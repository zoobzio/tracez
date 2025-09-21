package phantomlatency

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
	"github.com/zoobzio/tracez"
)

// TestMetricsVsReality demonstrates the phantom latency problem.
// Metrics show 87ms, customers experience 10+ seconds.
func TestMetricsVsReality(t *testing.T) {
	clock := clockz.NewFakeClock()
	service := NewPaymentService(clock)
	
	// Simulate flash sale conditions
	service.sdk.SimulateLoadScenario()

	ctx := context.Background()
	req := PaymentRequest{
		CustomerID: "megamart",
		Amount:     9999,
		Currency:   "USD",
		OrderID:    "flash_sale_001",
	}

	// Process payment
	start := clock.Now()
	
	done := make(chan error, 1)
	go func() {
		done <- service.ProcessPayment(ctx, req)
	}()
	
	// Let request start
	time.Sleep(10 * time.Millisecond)
	
	// Advance through network latency
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Wait for completion
	var err error
	select {
	case err = <-done:
		// Continue with test
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Payment timed out")
	}
	
	actualDuration := clock.Since(start)

	if err != nil {
		t.Logf("Payment failed: %v", err)
	}

	// What metrics show
	metrics := service.metrics.Stats()
	metricsLatency := time.Duration(metrics["p99_latency_ms"].(int64)) * time.Millisecond

	// What actually happened
	sdkStats := service.sdk.Stats()
	
	t.Logf("\n=== THE PHANTOM LATENCY ===")
	t.Logf("What metrics show: %v", metricsLatency)
	t.Logf("What customer experienced: %v", actualDuration)
	t.Logf("Hidden SDK behavior:")
	t.Logf("  - Total requests made: %d", sdkStats["total_requests"])
	t.Logf("  - Rate limits hit: %d", sdkStats["rate_limits_hit"])
	t.Logf("  - Total retries: %d", sdkStats["total_retries"])
	t.Logf("  - Time spent in backoff: %dms", sdkStats["backoff_time_ms"])

	// The metrics lie
	if metricsLatency < 100*time.Millisecond && actualDuration > 500*time.Millisecond {
		t.Logf("\n⚠️  METRICS ARE LYING: Shows %v but actually took %v", 
			metricsLatency, actualDuration)
	}
}

// TestFlashSaleScenario simulates the MegaMart incident
func TestFlashSaleScenario(t *testing.T) {
	clock := clockz.NewFakeClock()
	service := NewPaymentService(clock)
	service.sdk.SimulateLoadScenario()

	// Warm up - establish baseline
	for i := 0; i < 100; i++ {
		ctx := context.Background()
		req := PaymentRequest{
			CustomerID: "warmup",
			Amount:     100,
			Currency:   "USD",
			OrderID:    fmt.Sprintf("warmup_%d", i),
		}
		service.ProcessPayment(ctx, req)
	}
	clock.BlockUntilReady() // Ensure warmup completes

	t.Logf("\n=== FLASH SALE STARTS ===")
	t.Logf("Time: Monday, October 17, 2024, 12:30 AM")
	t.Logf("Event: MegaMart flash sale goes live")

	// Simulate concurrent flash sale traffic
	var wg sync.WaitGroup
	var mu sync.Mutex
	successCount := 0
	timeoutCount := 0
	customerExperiences := make([]time.Duration, 0, 100)
	
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(customerNum int) {
			defer wg.Done()
			
			ctx := context.Background()
			req := PaymentRequest{
				CustomerID: "megamart",
				Amount:     9999,
				Currency:   "USD",
				OrderID:    fmt.Sprintf("flash_%d", customerNum),
			}
			
			// Customer timeout is 10 seconds
			start := clock.Now()
			err := service.ProcessPaymentWithTimeout(ctx, req, 10*time.Second)
			duration := clock.Since(start)
			
			mu.Lock()
			customerExperiences = append(customerExperiences, duration)
			
			if err != nil {
				if strings.Contains(err.Error(), "timeout") {
					timeoutCount++
				}
			} else {
				successCount++
			}
			mu.Unlock()
		}(i)
		
		// Stagger requests slightly
		clock.Advance(50 * time.Millisecond)
	}
	
	wg.Wait()
	clock.BlockUntilReady() // Ensure all goroutines complete

	// Calculate customer experience
	var totalCustomerTime time.Duration
	var maxCustomerTime time.Duration
	for _, d := range customerExperiences {
		totalCustomerTime += d
		if d > maxCustomerTime {
			maxCustomerTime = d
		}
	}
	avgCustomerTime := totalCustomerTime / time.Duration(len(customerExperiences))

	t.Logf("\n=== WAR ROOM DASHBOARD (What DevOps Sees) ===")
	t.Logf("Time: 10:00 AM")
	metrics := service.metrics.Stats()
	t.Logf("P99 Latency: %dms ✅", metrics["p99_latency_ms"])
	t.Logf("Error Rate: %.2f%% ✅", metrics["error_rate"].(float64)*100)
	t.Logf("Success Count: %d", metrics["success_count"])
	t.Logf("Status: ALL SYSTEMS OPERATIONAL")

	t.Logf("\n=== CUSTOMER REALITY (What MegaMart Experiences) ===")
	t.Logf("Successful Checkouts: %d/%d (%.1f%%)", 
		successCount, 20, float64(successCount)/20*100)
	t.Logf("Timeouts: %d/%d (%.1f%%)", 
		timeoutCount, 20, float64(timeoutCount)/20*100)
	t.Logf("Average Checkout Time: %v", avgCustomerTime)
	t.Logf("Worst Checkout Time: %v", maxCustomerTime)
	t.Logf("Customer Complaints: FLOODING SUPPORT")

	t.Logf("\n=== THE HIDDEN TRUTH (SDK Internals) ===")
	sdkStats := service.sdk.Stats()
	t.Logf("SDK made %d actual requests", sdkStats["total_requests"])
	t.Logf("Hit rate limit %d times", sdkStats["rate_limits_hit"])
	t.Logf("Performed %d retries", sdkStats["total_retries"])
	t.Logf("Spent %dms in backoff sleep", sdkStats["backoff_time_ms"])
	t.Logf("Connection pool exhausted %d times", sdkStats["pool_exhaustions"])

	if timeoutCount > 0 {
		t.Logf("\n🔥 CUSTOMER IMPACT: %.1f%% of payments timing out!", 
			float64(timeoutCount)/20*100)
		t.Logf("💰 REVENUE LOSS: ~$%.0f per minute at this rate",
			float64(timeoutCount)/20 * 2000000 / 60)
	}
}

// TestWithTracez shows how distributed tracing reveals the truth
func TestWithTracez(t *testing.T) {
	clock := clockz.NewFakeClock()
	service := NewPaymentService(clock)
	service.sdk.SimulateLoadScenario()

	// Create a tracer with collector
	tracer := tracez.New().WithClock(clock)
	var spans []tracez.Span
	tracer.OnSpanComplete(func(span tracez.Span) {
		spans = append(spans, span)
	})
	service.EnableTracing(tracer)

	ctx := context.Background()
	
	req := PaymentRequest{
		CustomerID: "megamart",
		Amount:     9999,
		Currency:   "USD",
		OrderID:    "trace_test_001",
	}

	// Trigger rate limiting
	for i := 0; i < 10; i++ {
		service.ProcessPayment(ctx, PaymentRequest{
			CustomerID: "trigger_limit",
			Amount:     100,
			Currency:   "USD",
			OrderID:    fmt.Sprintf("trigger_%d", i),
		})
	}

	// Now process our traced payment
	start := clock.Now()
	err := service.ProcessPayment(ctx, req)
	clock.BlockUntilReady() // Ensure all async operations complete
	duration := clock.Since(start)

	if err != nil {
		t.Logf("Payment failed: %v", err)
	}

	// Export spans to see what happened
	// spans already captured by handler

	t.Logf("\n=== WITHOUT TRACEZ (Current Reality) ===")
	t.Logf("Payment: %v [SUCCESS]", service.metrics.P99Latency())

	t.Logf("\n=== WITH TRACEZ (The Truth) ===")
	t.Logf("Total time: %v", duration)
	printTrace(t, spans, "")

	sdkStats := service.sdk.Stats()
	hiddenTime := time.Duration(sdkStats["backoff_time_ms"]) * time.Millisecond
	t.Logf("\n💡 SMOKING GUN: %v of hidden retry backoff time!", hiddenTime)
	t.Logf("📊 Metrics show: %v", service.metrics.P99Latency())
	t.Logf("⏱️  Reality: %v", duration)
}

// TestSDKRetryBehavior isolates and demonstrates the SDK's retry pattern
func TestSDKRetryBehavior(t *testing.T) {
	clock := clockz.NewFakeClock()
	sdk := NewStreamPaySDK(clock)
	
	// Force rate limiting from the start
	sdk.rateLimitAfter = 0

	ctx := context.Background()
	request := map[string]string{
		"customer_id": "test",
		"amount":      "100",
		"currency":    "USD",
		"order_id":    "test_001",
	}

	start := clock.Now()
	resp, err := sdk.Execute(ctx, request)
	clock.BlockUntilReady() // Ensure retries complete
	duration := clock.Since(start)

	t.Logf("\n=== SDK RETRY CASCADE ===")
	
	if err != nil {
		t.Logf("Request failed after %v: %v", duration, err)
	} else if resp != nil && resp.Success {
		t.Logf("Request succeeded after %v", duration)
	}

	stats := sdk.Stats()
	t.Logf("\nRetry Pattern:")
	t.Logf("  Attempt 1: 62ms request + 100ms backoff")
	t.Logf("  Attempt 2: 62ms request + 200ms backoff")
	t.Logf("  Attempt 3: 62ms request + 400ms backoff")
	t.Logf("  Attempt 4: 62ms request + 800ms backoff")
	t.Logf("  Attempt 5: 62ms request + 1600ms backoff")
	t.Logf("\nTotal: %d requests, %dms in backoff", 
		stats["total_requests"], stats["backoff_time_ms"])
	t.Logf("Expected minimum time: 3410ms")
	t.Logf("Actual time: %v", duration)
}

// TestTheWarRoom recreates the investigation timeline
func TestTheWarRoom(t *testing.T) {
	t.Logf("\n=== THE WAR ROOM INVESTIGATION ===")
	t.Logf("Date: Monday, October 17, 2024")
	t.Logf("Location: PayFlow HQ, War Room")
	
	clock := clockz.NewFakeClock()
	service := NewPaymentService(clock)
	service.sdk.SimulateLoadScenario()

	t.Logf("\n--- HOUR 1: Check the Dashboards (10:00 AM) ---")
	// Process some requests
	for i := 0; i < 50; i++ {
		ctx := context.Background()
		service.ProcessPayment(ctx, PaymentRequest{
			CustomerID: "investigation",
			Amount:     100,
			Currency:   "USD",
			OrderID:    fmt.Sprintf("investigate_%d", i),
		})
	}
	clock.BlockUntilReady() // Ensure all requests complete
	
	metrics := service.metrics.Stats()
	t.Logf("API Latency P99: %dms ✅", metrics["p99_latency_ms"])
	t.Logf("Error Rate: %.2f%% ✅", metrics["error_rate"].(float64)*100)
	t.Logf("Conclusion: \"Problem is on customer side\"")

	t.Logf("\n--- HOUR 2: Customer Evidence (11:00 AM) ---")
	ctx := context.Background()
	req := PaymentRequest{
		CustomerID: "megamart",
		Amount:     9999,
		Currency:   "USD",
		OrderID:    "evidence_001",
	}
	start := clock.Now()
	service.ProcessPayment(ctx, req)
	clock.BlockUntilReady() // Ensure payment completes
	actualTime := clock.Since(start)
	
	t.Logf("Customer HAR file shows: %v request time", actualTime)
	t.Logf("Our metrics show: %v", service.metrics.P99Latency())
	t.Logf("Conclusion: \"Maybe network issues?\"")

	t.Logf("\n--- HOUR 3: Add More Logging (12:00 PM) ---")
	t.Logf("Added logging to service layer...")
	t.Logf("Service.ProcessPayment: start=%v", clock.Now())
	t.Logf("Service.ProcessPayment: calling SDK...")
	t.Logf("Service.ProcessPayment: SDK returned in %v", service.metrics.P99Latency())
	t.Logf("Conclusion: \"Ghost in the machine\"")

	t.Logf("\n--- HOUR 4: The Revelation (1:00 PM) ---")
	t.Logf("Junior: \"What about the SDK?\"")
	t.Logf("Senior: \"It's battle-tested\"")
	t.Logf("Junior: \"But do we measure it?\"")
	t.Logf("Senior: \"...\"")
	
	t.Logf("\nChecking vendor/streampay-sdk-v3/client.go...")
	t.Logf("Found: Retry loop with exponential backoff")
	t.Logf("Found: Default MaxRetries = 5")
	t.Logf("Found: HTTP 200 with error body triggers retry")
	
	sdkStats := service.sdk.Stats()
	t.Logf("\n🔍 THE SMOKING GUN:")
	t.Logf("  SDK made %d requests for %d payments", 
		sdkStats["total_requests"], metrics["success_count"])
	t.Logf("  Time hidden in backoff: %dms", sdkStats["backoff_time_ms"])
	t.Logf("  Rate limits hit: %d times", sdkStats["rate_limits_hit"])
	
	t.Logf("\n💡 CONCLUSION: \"The vendor SDK we trusted for 3 years")
	t.Logf("              was gaslighting our metrics the entire time.\"")
}

// printTrace recursively prints a trace tree
func printTrace(t *testing.T, spans []tracez.Span, indent string) {
	// Find root spans
	roots := findRootSpans(spans)
	for _, root := range roots {
		printSpan(t, root, spans, indent)
	}
}

func findRootSpans(spans []tracez.Span) []tracez.Span {
	var roots []tracez.Span
	for _, span := range spans {
		if span.ParentID == "" {
			roots = append(roots, span)
		}
	}
	return roots
}

func printSpan(t *testing.T, span tracez.Span, allSpans []tracez.Span, indent string) {
	// Print current span
	t.Logf("%s%s [%v]", indent, span.Name, span.Duration)
	
	// Print tags
	for k, v := range span.Tags {
		t.Logf("%s  %s: %s", indent, k, v)
	}
	
	// Find and print children
	children := findChildren(span, allSpans)
	for _, child := range children {
		printSpan(t, child, allSpans, indent+"  ")
	}
}

func findChildren(parent tracez.Span, allSpans []tracez.Span) []tracez.Span {
	var children []tracez.Span
	for _, span := range allSpans {
		if span.ParentID == parent.SpanID {
			children = append(children, span)
		}
	}
	return children
}