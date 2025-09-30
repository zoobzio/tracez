package phantomlatency

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zoobzio/clockz"
	"github.com/zoobzio/tracez"
)

// StreamPaySDK simulates a third-party payment SDK with hidden retry logic.
// This represents vendor code that lives in vendor/ and can't be modified.
// StreamPay always returns HTTP 200 OK but includes an 'error' field in the
// JSON response body - a common but problematic anti-pattern where errors
// are encoded in successful HTTP responses instead of using proper status codes.
type StreamPaySDK struct {
	// Config buried in SDK initialization
	maxRetries     int
	initialBackoff time.Duration
	maxBackoff     time.Duration

	// Internal state (invisible to metrics)
	requestCount  atomic.Int64
	rateLimitHit  atomic.Int64
	retryCount    atomic.Int64
	backoffTime   atomic.Int64

	// Rate limiting simulation
	mu               sync.Mutex
	windowStart      time.Time
	windowRequests   int
	maxRequestsPerMin int
	
	// Connection pool simulation
	connectionPool   chan struct{}
	poolExhausted    atomic.Int64

	// Failure injection
	transientErrorRate float64
	rateLimitAfter     int

	// Clock for testing
	clock clockz.Clock
	
	// Tracer for instrumentation (if provided)
	tracer *tracez.Tracer
}

// StreamPayResponse represents the API response.
// Even errors return HTTP 200 with error details in body.
type StreamPayResponse struct {
	Success bool              `json:"success"`
	Error   *StreamPayError   `json:"error,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
}

type StreamPayError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// NewStreamPaySDK creates a new SDK instance with default production config.
// This simulates how third-party SDKs often have buried configuration.
func NewStreamPaySDK(clock clockz.Clock) *StreamPaySDK {
	return &StreamPaySDK{
		maxRetries:         5,  // Hardcoded in SDK
		initialBackoff:     100 * time.Millisecond,
		maxBackoff:         10 * time.Second,
		maxRequestsPerMin:  1000,
		connectionPool:     make(chan struct{}, 100), // Default pool size
		transientErrorRate: 0.001, // 0.1% transient errors
		rateLimitAfter:     1000,  // Rate limit after 1000 requests/min
		clock:              clock,
		windowStart:        clock.Now(),
	}
}

// EnableTracing adds distributed tracing to the SDK
func (sdk *StreamPaySDK) EnableTracing(tracer *tracez.Tracer) {
	sdk.tracer = tracer
}

// SimulateLoadScenario configures the SDK to simulate flash sale conditions
func (sdk *StreamPaySDK) SimulateLoadScenario() {
	sdk.transientErrorRate = 0.02  // 2% errors under load
	sdk.rateLimitAfter = 10        // Rate limit after 10 requests/min - triggers during concurrent load
	// Pool stays at 100, causing exhaustion under load
}

// Execute performs the payment request with hidden retry logic.
// This is the black box that metrics can't see inside.
func (sdk *StreamPaySDK) Execute(ctx context.Context, request map[string]string) (*StreamPayResponse, error) {
	var span *tracez.ActiveSpan
	if sdk.tracer != nil {
		ctx, span = sdk.tracer.StartSpan(ctx, "streampay_sdk.execute")
		defer span.Finish()
	}

	attempts := 0
	backoff := sdk.initialBackoff

	for attempts < sdk.maxRetries {
		var attemptSpan *tracez.ActiveSpan
		attemptCtx := ctx
		if sdk.tracer != nil {
			attemptCtx, attemptSpan = sdk.tracer.StartSpan(ctx, tracez.Key(fmt.Sprintf("attempt_%d", attempts+1)))
		}
		
		// Try to get connection from pool
		connStart := sdk.clock.Now()
		select {
		case sdk.connectionPool <- struct{}{}:
			if attemptSpan != nil {
				attemptSpan.SetTag("connection_acquired", fmt.Sprintf("%dms", sdk.clock.Since(connStart).Milliseconds()))
				attemptSpan.SetTag("pool_exhausted", "false")
			}
		case <-sdk.clock.After(50 * time.Millisecond):
			// Pool exhausted, but got connection eventually
			sdk.poolExhausted.Add(1)
			if attemptSpan != nil {
				attemptSpan.SetTag("connection_wait", fmt.Sprintf("%dms", sdk.clock.Since(connStart).Milliseconds()))
				attemptSpan.SetTag("pool_exhausted", "true")
			}
		}

		// Make the actual request
		resp, err := sdk.doRequest(attemptCtx, request)
		
		// Return connection to pool
		select {
		case <-sdk.connectionPool:
		default:
		}

		// Handle response - this is where the anti-pattern lives
		if err == nil && resp != nil {
			// Check for errors in successful HTTP response
			if resp.Error != nil {
				if resp.Error.Code == "RATE_LIMIT" {
					sdk.rateLimitHit.Add(1)
					sdk.retryCount.Add(1)
					
					if attemptSpan != nil {
						attemptSpan.SetTag("rate_limit_hit", "true")
						attemptSpan.SetTag("backoff_ms", fmt.Sprintf("%d", backoff.Milliseconds()))
						attemptSpan.Finish()
					}
					
					// Record backoff without actually waiting
					sdk.backoffTime.Add(backoff.Nanoseconds())
					backoff = sdk.exponentialBackoff(backoff)
					attempts++
					continue
				}
				
				if resp.Error.Retryable {
					sdk.retryCount.Add(1)
					
					if attemptSpan != nil {
						attemptSpan.SetTag("transient_error", resp.Error.Code)
						attemptSpan.SetTag("backoff_ms", fmt.Sprintf("%d", backoff.Milliseconds()))
						attemptSpan.Finish()
					}
					
					// Record backoff without actually waiting
					sdk.backoffTime.Add(backoff.Nanoseconds())
					backoff = sdk.exponentialBackoff(backoff)
					attempts++
					continue
				}
			}
			
			// Success or non-retryable error
			if attemptSpan != nil {
				attemptSpan.SetTag("request_complete", "true")
				attemptSpan.SetTag("success", fmt.Sprintf("%v", resp.Success))
				attemptSpan.SetTag("attempts", fmt.Sprintf("%d", attempts+1))
				attemptSpan.Finish()
			}
			return resp, nil
		}

		// Connection error - also retry
		if err != nil {
			sdk.retryCount.Add(1)
			
			if attemptSpan != nil {
				attemptSpan.SetTag("connection_error", err.Error())
				attemptSpan.SetTag("backoff_ms", fmt.Sprintf("%d", backoff.Milliseconds()))
				attemptSpan.Finish()
			}
			
			// Record backoff without actually waiting
			sdk.backoffTime.Add(backoff.Nanoseconds())
			backoff = sdk.exponentialBackoff(backoff)
			attempts++
			continue
		}

		if attemptSpan != nil {
			attemptSpan.Finish()
		}
	}

	if span != nil {
		span.SetTag("max_retries_exceeded", "true")
		span.SetTag("attempts", fmt.Sprintf("%d", attempts))
		span.SetTag("total_backoff_ms", fmt.Sprintf("%d", sdk.backoffTime.Load()/1_000_000))
	}
	
	return nil, fmt.Errorf("max retries exceeded after %d attempts", attempts)
}

// doRequest simulates the actual HTTP request to StreamPay
func (sdk *StreamPaySDK) doRequest(ctx context.Context, request map[string]string) (*StreamPayResponse, error) {
	var span *tracez.ActiveSpan
	if sdk.tracer != nil {
		ctx, span = sdk.tracer.StartSpan(ctx, "http_post")
		defer span.Finish()
	}

	sdk.requestCount.Add(1)

	// Check rate limiting
	sdk.mu.Lock()
	now := sdk.clock.Now()
	if now.Sub(sdk.windowStart) > time.Minute {
		// Reset window
		sdk.windowStart = now
		sdk.windowRequests = 0
	}
	sdk.windowRequests++
	isRateLimited := sdk.windowRequests > sdk.rateLimitAfter
	sdk.mu.Unlock()

	// Skip network latency simulation - trace tags show the issue

	// Return rate limit error as HTTP 200 (the anti-pattern)
	if isRateLimited {
		if span != nil {
			span.SetTag("status", "200")
			span.SetTag("body.error", "RATE_LIMIT")
		}
		
		return &StreamPayResponse{
			Success: false,
			Error: &StreamPayError{
				Code:      "RATE_LIMIT",
				Message:   "Rate limit exceeded",
				Retryable: true,
			},
		}, nil
	}

	// Random transient errors (also HTTP 200)
	if sdk.clock.Now().UnixNano()%1000 < int64(sdk.transientErrorRate*1000) {
		if span != nil {
			span.SetTag("status", "200")
			span.SetTag("body.error", "TRANSIENT_ERROR")
		}
		
		return &StreamPayResponse{
			Success: false,
			Error: &StreamPayError{
				Code:      "TRANSIENT_ERROR",
				Message:   "Temporary failure, please retry",
				Retryable: true,
			},
		}, nil
	}

	// Success
	if span != nil {
		span.SetTag("status", "200")
		span.SetTag("success", "true")
	}
	
	return &StreamPayResponse{
		Success: true,
		Data: map[string]string{
			"transaction_id": fmt.Sprintf("txn_%d", sdk.clock.Now().UnixNano()),
			"status":         "completed",
		},
	}, nil
}

func (sdk *StreamPaySDK) exponentialBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > sdk.maxBackoff {
		return sdk.maxBackoff
	}
	return next
}

// Stats returns internal SDK statistics (not available in production)
func (sdk *StreamPaySDK) Stats() map[string]int64 {
	return map[string]int64{
		"total_requests":   sdk.requestCount.Load(),
		"rate_limits_hit":  sdk.rateLimitHit.Load(),
		"total_retries":    sdk.retryCount.Load(),
		"backoff_time_ms":  sdk.backoffTime.Load() / 1_000_000,
		"pool_exhaustions": sdk.poolExhausted.Load(),
	}
}

// PaymentService represents our application's payment processing layer.
// This is well-instrumented but blind to SDK internals.
type PaymentService struct {
	sdk     *StreamPaySDK
	metrics *MetricsCollector
	clock   clockz.Clock
	tracer  *tracez.Tracer
}

// PaymentRequest represents a payment request from our API
type PaymentRequest struct {
	CustomerID string
	Amount     int64
	Currency   string
	OrderID    string
}

// MetricsCollector simulates our application metrics
type MetricsCollector struct {
	mu              sync.Mutex
	latencies       []time.Duration
	successCount    int64
	errorCount      int64
	timeoutCount    int64
}

func NewMetricsCollector() *MetricsCollector {
	return &MetricsCollector{
		latencies: make([]time.Duration, 0, 10000),
	}
}

func (m *MetricsCollector) RecordLatency(d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.latencies = append(m.latencies, d)
}

func (m *MetricsCollector) RecordSuccess() {
	atomic.AddInt64(&m.successCount, 1)
}

func (m *MetricsCollector) RecordError() {
	atomic.AddInt64(&m.errorCount, 1)
}

func (m *MetricsCollector) RecordTimeout() {
	atomic.AddInt64(&m.timeoutCount, 1)
}

func (m *MetricsCollector) P99Latency() time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if len(m.latencies) == 0 {
		return 0
	}
	
	// Simple P99 calculation
	sorted := make([]time.Duration, len(m.latencies))
	copy(sorted, m.latencies)
	
	// Bubble sort for simplicity (fine for small datasets)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	
	idx := int(math.Ceil(float64(len(sorted)) * 0.99)) - 1
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func (m *MetricsCollector) Stats() map[string]interface{} {
	return map[string]interface{}{
		"p99_latency_ms": m.P99Latency().Milliseconds(),
		"success_count":  atomic.LoadInt64(&m.successCount),
		"error_count":    atomic.LoadInt64(&m.errorCount),
		"timeout_count":  atomic.LoadInt64(&m.timeoutCount),
		"error_rate":     float64(atomic.LoadInt64(&m.errorCount)) / float64(atomic.LoadInt64(&m.successCount) + atomic.LoadInt64(&m.errorCount)),
	}
}

// NewPaymentService creates a new payment service
func NewPaymentService(clock clockz.Clock) *PaymentService {
	return &PaymentService{
		sdk:     NewStreamPaySDK(clock),
		metrics: NewMetricsCollector(),
		clock:   clock,
	}
}

// EnableTracing adds distributed tracing to the payment service
func (s *PaymentService) EnableTracing(tracer *tracez.Tracer) {
	s.tracer = tracer
	s.sdk.EnableTracing(tracer)
}

// ProcessPayment handles a payment request - this is what we measure
func (s *PaymentService) ProcessPayment(ctx context.Context, req PaymentRequest) error {
	// This is what metrics see
	start := s.clock.Now()
	defer func() {
		latency := s.clock.Since(start)
		s.metrics.RecordLatency(latency)
	}()

	var span *tracez.ActiveSpan
	if s.tracer != nil {
		ctx, span = s.tracer.StartSpan(ctx, "process_payment")
		span.SetTag("payment_id", req.OrderID)
		span.SetTag("customer", req.CustomerID)
		defer span.Finish()
	}

	// Build request for SDK
	sdkRequest := map[string]string{
		"customer_id": req.CustomerID,
		"amount":      fmt.Sprintf("%d", req.Amount),
		"currency":    req.Currency,
		"order_id":    req.OrderID,
	}

	// SDK does its magic (retries, backoffs, etc) - all invisible
	resp, err := s.sdk.Execute(ctx, sdkRequest)
	if err != nil {
		s.metrics.RecordError()
		if span != nil {
			span.SetTag("error", err.Error())
		}
		return fmt.Errorf("payment failed: %w", err)
	}

	if !resp.Success {
		s.metrics.RecordError()
		err := fmt.Errorf("payment rejected: %s", resp.Error.Message)
		if span != nil {
			span.SetTag("error", err.Error())
		}
		return err
	}

	s.metrics.RecordSuccess()
	if span != nil {
		span.SetTag("payment_completed", "true")
		span.SetTag("transaction_id", resp.Data["transaction_id"])
	}
	
	return nil
}

// ProcessPaymentWithTimeout simulates customer experience with browser timeout
func (s *PaymentService) ProcessPaymentWithTimeout(ctx context.Context, req PaymentRequest, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- s.ProcessPayment(ctx, req)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		s.metrics.RecordTimeout()
		return fmt.Errorf("payment timeout after %v", timeout)
	}
}

// GetServiceMetrics returns what DevOps sees in dashboards
func (s *PaymentService) GetServiceMetrics() string {
	stats := s.metrics.Stats()
	data, _ := json.MarshalIndent(stats, "", "  ")
	return string(data)
}

// GetSDKStats returns hidden SDK behavior (not available in production)
func (s *PaymentService) GetSDKStats() string {
	stats := s.sdk.Stats()
	data, _ := json.MarshalIndent(stats, "", "  ")
	return string(data)
}