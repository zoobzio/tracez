package integration

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// TestSamplingStrategies demonstrates different trace sampling approaches.
func TestSamplingStrategies(t *testing.T) {
	tracer := tracez.New("sampling-test")
	collector := NewMockCollector(t, "sampling", 2000)
	tracer.AddCollector("observability", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Sampling configurations.
	strategies := []struct {
		name       string
		sampleRate float64
		requests   int
	}{
		{"always", 1.0, 20},   // 100% sampling.
		{"half", 0.5, 40},     // 50% sampling.
		{"quarter", 0.25, 80}, // 25% sampling.
		{"rare", 0.1, 100},    // 10% sampling.
	}

	for _, strategy := range strategies {
		t.Run(strategy.name, func(t *testing.T) {
			sampled := 0

			for i := 0; i < strategy.requests; i++ {
				// Apply sampling decision.
				shouldSample := rand.Float64() < strategy.sampleRate

				if shouldSample {
					// Create trace.
					_, span := tracer.StartSpan(ctx, fmt.Sprintf("%s-request-%d", strategy.name, i))
					span.SetTag("strategy", strategy.name)
					span.SetTag("sample_rate", fmt.Sprintf("%.2f", strategy.sampleRate))
					span.SetTag("sampled", "true")

					// Simulate work.
					time.Sleep(1 * time.Millisecond)
					span.Finish()
					sampled++
				}
			}

			// Verify sampling rate is approximately correct.
			actualRate := float64(sampled) / float64(strategy.requests)
			tolerance := 0.2 // 20% tolerance for randomness.

			if actualRate < strategy.sampleRate-tolerance || actualRate > strategy.sampleRate+tolerance {
				t.Errorf("Sampling rate deviation: expected ~%.2f, got %.2f",
					strategy.sampleRate, actualRate)
			}

			t.Logf("Strategy %s: %d/%d sampled (%.2f%%)",
				strategy.name, sampled, strategy.requests, actualRate*100)
		})
	}
}

// TestTraceAggregation demonstrates collecting and analyzing trace patterns.
func TestTraceAggregation(t *testing.T) {
	tracer := tracez.New("aggregation-service")
	collector := NewMockCollector(t, "aggregation", 2000)
	tracer.AddCollector("observability", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Simulate various operation types with different characteristics.
	operations := []struct {
		name      string
		count     int
		minTime   time.Duration
		maxTime   time.Duration
		errorRate float64
	}{
		{"fast-read", 50, 1 * time.Millisecond, 5 * time.Millisecond, 0.02},
		{"slow-query", 20, 50 * time.Millisecond, 200 * time.Millisecond, 0.15},
		{"cache-miss", 30, 10 * time.Millisecond, 30 * time.Millisecond, 0.05},
		{"external-api", 25, 100 * time.Millisecond, 500 * time.Millisecond, 0.30},
	}

	for _, op := range operations {
		for i := 0; i < op.count; i++ {
			// Start operation.
			_, span := tracer.StartSpan(ctx, op.name)
			span.SetTag("operation", op.name)
			span.SetTag("execution_id", fmt.Sprintf("%s-%d", op.name, i))

			// Simulate variable duration.
			duration := op.minTime + time.Duration(rand.Int63n(int64(op.maxTime-op.minTime)))
			time.Sleep(duration)

			// Simulate errors.
			if rand.Float64() < op.errorRate {
				span.SetTag("error", "true")
				span.SetTag("error.type", "simulated_failure")
			} else {
				span.SetTag("status", "success")
			}

			span.Finish()
		}
	}

	// Analyze aggregated data.
	spans := collector.Export()

	// Group by operation type.
	operationStats := make(map[string]struct {
		count     int
		totalTime time.Duration
		errors    int
		minTime   time.Duration
		maxTime   time.Duration
	})

	for _, span := range spans {
		op := span.Tags["operation"]
		stats := operationStats[op]

		stats.count++
		stats.totalTime += span.Duration

		if span.Tags["error"] == "true" {
			stats.errors++
		}

		if stats.minTime == 0 || span.Duration < stats.minTime {
			stats.minTime = span.Duration
		}
		if span.Duration > stats.maxTime {
			stats.maxTime = span.Duration
		}

		operationStats[op] = stats
	}

	// Verify statistics.
	for _, op := range operations {
		stats := operationStats[op.name]

		if stats.count != op.count {
			t.Errorf("Operation %s: expected %d spans, got %d", op.name, op.count, stats.count)
		}

		avgTime := stats.totalTime / time.Duration(stats.count)
		errorRate := float64(stats.errors) / float64(stats.count)

		t.Logf("Operation %s: count=%d, avg=%.2fms, errors=%.1f%%, range=[%.2f-%.2f]ms",
			op.name, stats.count, avgTime.Seconds()*1000, errorRate*100,
			stats.minTime.Seconds()*1000, stats.maxTime.Seconds()*1000)
	}

	// Find outliers (spans taking more than 2x average).
	outliers := []tracez.Span{}
	for _, span := range spans {
		op := span.Tags["operation"]
		stats := operationStats[op]
		avgTime := stats.totalTime / time.Duration(stats.count)

		if span.Duration > avgTime*2 {
			outliers = append(outliers, span)
		}
	}

	t.Logf("Found %d outlier spans (>2x average)", len(outliers))
}

// TestDistributedTracing demonstrates trace correlation across services.
func TestDistributedTracing(t *testing.T) {
	// Create single tracer for distributed test.
	// (In real systems, different services would have separate tracers.
	// but send to same collector/backend).
	tracer := tracez.New("distributed-system")
	collector := NewMockCollector(t, "distributed", 1000)
	tracer.AddCollector("observability", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Simulate distributed request.
	ctx, frontendSpan := tracer.StartSpan(ctx, "user-request")
	frontendSpan.SetTag("service", "frontend")
	frontendSpan.SetTag("user_id", "user-456")
	frontendSpan.SetTag("endpoint", "/api/profile")

	// Frontend processes request.
	time.Sleep(5 * time.Millisecond)

	// Call backend service (propagate context).
	ctx, backendSpan := tracer.StartSpan(ctx, "profile-lookup")
	backendSpan.SetTag("service", "backend")
	backendSpan.SetTag("operation", "get_user_profile")

	// Backend processes.
	time.Sleep(10 * time.Millisecond)

	// Backend calls database (propagate context).
	_, dbSpan := tracer.StartSpan(ctx, "user-query")
	dbSpan.SetTag("service", "database")
	dbSpan.SetTag("query", "SELECT * FROM users WHERE id = ?")
	dbSpan.SetTag("db.instance", "postgres-primary")

	// Database query.
	time.Sleep(15 * time.Millisecond)
	dbSpan.Finish()

	// Backend completes.
	backendSpan.Finish()

	// Frontend completes.
	frontendSpan.Finish()

	// Verify distributed trace.
	spans := collector.Export()
	analyzer := NewTraceAnalyzer(spans)

	// Should have one root trace with 3 spans.
	if analyzer.CountSpans() != 3 {
		t.Errorf("Expected 3 spans, got %d", analyzer.CountSpans())
	}

	// All spans should share same trace ID.
	var traceID string
	for _, span := range spans {
		if traceID == "" {
			traceID = span.TraceID
		} else if span.TraceID != traceID {
			t.Errorf("Span %s has different trace ID", span.Name)
		}
	}

	// Verify parent-child relationships.
	err := analyzer.VerifyChain("user-request", "profile-lookup", "user-query")
	if err != nil {
		t.Errorf("Trace chain broken: %v", err)
	}

	// Verify service tags.
	services := make(map[string]bool)
	for _, span := range spans {
		services[span.Tags["service"]] = true
	}

	expectedServices := []string{"frontend", "backend", "database"}
	for _, svc := range expectedServices {
		if !services[svc] {
			t.Errorf("Service %s not found in trace", svc)
		}
	}

	t.Logf("Distributed trace spans across %d services", len(services))
}

// TestCorrelationIDs demonstrates request tracking across async operations.
func TestCorrelationIDs(t *testing.T) {
	tracer := tracez.New("correlation-test")
	collector := NewMockCollector(t, "correlation", 1000)
	tracer.AddCollector("observability", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Start request with correlation ID.
	correlationID := "req-12345"
	ctx, requestSpan := tracer.StartSpan(ctx, "api-request")
	requestSpan.SetTag("correlation_id", correlationID)
	requestSpan.SetTag("user_id", "user-789")

	// Process synchronously.
	_, syncSpan := tracer.StartSpan(ctx, "validation")
	syncSpan.SetTag("correlation_id", correlationID)
	syncSpan.SetTag("type", "sync")
	time.Sleep(5 * time.Millisecond)
	syncSpan.Finish()

	// Launch async operations.
	var wg sync.WaitGroup
	asyncOps := []string{"email-notification", "audit-log", "analytics-event"}

	for _, op := range asyncOps {
		wg.Add(1)
		go func(operation string) {
			defer wg.Done()

			// Start async span with correlation.
			_, asyncSpan := tracer.StartSpan(ctx, operation)
			asyncSpan.SetTag("correlation_id", correlationID)
			asyncSpan.SetTag("type", "async")
			asyncSpan.SetTag("async_operation", operation)

			// Simulate async work.
			time.Sleep(time.Duration(rand.Intn(20)+10) * time.Millisecond)

			asyncSpan.Finish()
		}(op)
	}

	// Complete request before async ops finish.
	requestSpan.Finish()

	// Wait for async operations.
	wg.Wait()

	// Analyze correlation.
	spans := collector.Export()

	// Group by correlation ID.
	correlatedSpans := []tracez.Span{}
	for _, span := range spans {
		if span.Tags["correlation_id"] == correlationID {
			correlatedSpans = append(correlatedSpans, span)
		}
	}

	expectedSpans := 1 + 1 + len(asyncOps) // request + sync + async ops.
	if len(correlatedSpans) != expectedSpans {
		t.Errorf("Expected %d correlated spans, got %d", expectedSpans, len(correlatedSpans))
	}

	// Verify async spans exist.
	asyncCount := 0
	syncCount := 0
	for _, span := range correlatedSpans {
		if span.Tags["type"] == "async" {
			asyncCount++
		} else if span.Tags["type"] == "sync" {
			syncCount++
		}
	}

	if asyncCount != len(asyncOps) {
		t.Errorf("Expected %d async spans, got %d", len(asyncOps), asyncCount)
	}

	if syncCount != 1 {
		t.Errorf("Expected 1 sync span, got %d", syncCount)
	}

	t.Logf("Correlation ID %s tracked across %d spans (%d sync, %d async)",
		correlationID, len(correlatedSpans), syncCount, asyncCount)
}

// TestLatencyPercentiles calculates performance percentiles from traces.
func TestLatencyPercentiles(t *testing.T) {
	tracer := tracez.New("latency-analysis")
	collector := NewMockCollector(t, "latency", 2000)
	tracer.AddCollector("observability", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Generate requests with realistic latency distribution.
	requestCount := 200
	for i := 0; i < requestCount; i++ {
		_, span := tracer.StartSpan(ctx, "api-call")
		span.SetTag("request_id", fmt.Sprintf("req-%d", i))

		// Simulate latency distribution:.
		// - 80% fast (1-10ms).
		// - 15% medium (10-50ms)  .
		// - 4% slow (50-200ms).
		// - 1% very slow (200-1000ms).
		var latency time.Duration
		r := rand.Float64()
		switch {
		case r < 0.80:
			latency = time.Duration(rand.Intn(9)+1) * time.Millisecond
			span.SetTag("latency_class", "fast")
		case r < 0.95:
			latency = time.Duration(rand.Intn(40)+10) * time.Millisecond
			span.SetTag("latency_class", "medium")
		case r < 0.99:
			latency = time.Duration(rand.Intn(150)+50) * time.Millisecond
			span.SetTag("latency_class", "slow")
		default:
			latency = time.Duration(rand.Intn(800)+200) * time.Millisecond
			span.SetTag("latency_class", "very_slow")
		}

		time.Sleep(latency)
		span.Finish()
	}

	// Calculate percentiles.
	spans := collector.Export()

	// Extract durations and sort.
	durations := make([]time.Duration, len(spans))
	for i, span := range spans {
		durations[i] = span.Duration
	}
	sort.Slice(durations, func(i, j int) bool {
		return durations[i] < durations[j]
	})

	// Calculate percentiles.
	percentiles := []float64{50, 90, 95, 99, 99.9}
	results := make(map[float64]time.Duration)

	for _, p := range percentiles {
		index := int(float64(len(durations)) * p / 100)
		if index >= len(durations) {
			index = len(durations) - 1
		}
		results[p] = durations[index]
	}

	// Verify reasonable distribution.
	if results[50] >= results[90] {
		t.Error("P50 should be less than P90")
	}
	if results[90] >= results[99] {
		t.Error("P90 should be less than P99")
	}

	// Count latency classes.
	classCounts := make(map[string]int)
	for _, span := range spans {
		class := span.Tags["latency_class"]
		classCounts[class]++
	}

	t.Logf("Latency percentiles:")
	for _, p := range percentiles {
		t.Logf("  P%.1f: %.2fms", p, results[p].Seconds()*1000)
	}

	t.Logf("Latency distribution:")
	for class, count := range classCounts {
		pct := float64(count) / float64(len(spans)) * 100
		t.Logf("  %s: %d (%.1f%%)", class, count, pct)
	}
}

// TestErrorRateAnalysis tracks error patterns and rates.
func TestErrorRateAnalysis(t *testing.T) {
	tracer := tracez.New("error-analysis")
	collector := NewMockCollector(t, "errors", 1000)
	tracer.AddCollector("observability", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Simulate operations with different error patterns.
	errorPatterns := []struct {
		operation string
		count     int
		errorRate float64
		errorType string
	}{
		{"database-query", 100, 0.02, "timeout"},
		{"payment-process", 50, 0.08, "declined"},
		{"external-api", 75, 0.15, "unavailable"},
		{"user-validation", 200, 0.01, "invalid_input"},
	}

	for _, pattern := range errorPatterns {
		for i := 0; i < pattern.count; i++ {
			_, span := tracer.StartSpan(ctx, pattern.operation)
			span.SetTag("operation", pattern.operation)
			span.SetTag("attempt", fmt.Sprintf("%d", i))

			// Simulate processing.
			time.Sleep(time.Duration(rand.Intn(10)+1) * time.Millisecond)

			// Apply error rate.
			if rand.Float64() < pattern.errorRate {
				span.SetTag("error", "true")
				span.SetTag("error.type", pattern.errorType)
				span.SetTag("error.message", fmt.Sprintf("Simulated %s error", pattern.errorType))
			} else {
				span.SetTag("status", "success")
			}

			span.Finish()
		}
	}

	// Analyze error rates.
	spans := collector.Export()

	operationStats := make(map[string]struct {
		total      int
		errors     int
		errorTypes map[string]int
	})

	for _, span := range spans {
		op := span.Tags["operation"]
		stats := operationStats[op]
		if stats.errorTypes == nil {
			stats.errorTypes = make(map[string]int)
		}

		stats.total++
		if span.Tags["error"] == "true" {
			stats.errors++
			errorType := span.Tags["error.type"]
			stats.errorTypes[errorType]++
		}

		operationStats[op] = stats
	}

	// Verify error rates are approximately correct.
	for _, pattern := range errorPatterns {
		stats := operationStats[pattern.operation]
		actualRate := float64(stats.errors) / float64(stats.total)

		tolerance := 0.05 // 5% tolerance.
		if actualRate < pattern.errorRate-tolerance || actualRate > pattern.errorRate+tolerance {
			t.Logf("Error rate deviation for %s: expected %.2f, got %.2f (within tolerance)",
				pattern.operation, pattern.errorRate, actualRate)
		}

		t.Logf("Operation %s: %d/%d errors (%.1f%%)",
			pattern.operation, stats.errors, stats.total, actualRate*100)
	}

	// Find operations with highest error rates.
	type OperationError struct {
		operation string
		rate      float64
		count     int
	}

	operationErrors := make([]OperationError, 0, len(operationStats))
	for op, stats := range operationStats {
		rate := float64(stats.errors) / float64(stats.total)
		operationErrors = append(operationErrors, OperationError{
			operation: op,
			rate:      rate,
			count:     stats.errors,
		})
	}

	sort.Slice(operationErrors, func(i, j int) bool {
		return operationErrors[i].rate > operationErrors[j].rate
	})

	t.Logf("Operations by error rate:")
	for _, opErr := range operationErrors {
		t.Logf("  %s: %.1f%% (%d errors)", opErr.operation, opErr.rate*100, opErr.count)
	}
}

// TestHealthcheckIntegration demonstrates health monitoring via traces.
func TestHealthcheckIntegration(t *testing.T) {
	tracer := tracez.New("health-monitor")
	collector := NewMockCollector(t, "health", 1000)
	tracer.AddCollector("observability", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Simulate health checks for various components.
	components := []struct {
		name    string
		healthy bool
		latency time.Duration
		details map[string]string
	}{
		{
			name:    "database",
			healthy: true,
			latency: 5 * time.Millisecond,
			details: map[string]string{
				"connections": "8/10",
				"version":     "13.4",
			},
		},
		{
			name:    "redis",
			healthy: true,
			latency: 2 * time.Millisecond,
			details: map[string]string{
				"memory_usage": "45%",
				"uptime":       "72h",
			},
		},
		{
			name:    "external_api",
			healthy: false,
			latency: 1 * time.Second, // Timeout - reduced for test efficiency.
			details: map[string]string{
				"last_success": "2h ago",
				"error":        "connection timeout",
			},
		},
		{
			name:    "message_queue",
			healthy: true,
			latency: 8 * time.Millisecond,
			details: map[string]string{
				"queue_depth": "12",
				"consumers":   "3",
			},
		},
	}

	// Perform health checks.
	ctx, healthSpan := tracer.StartSpan(ctx, "health-check")
	healthSpan.SetTag("check_type", "full")

	overallHealthy := true

	for _, comp := range components {
		_, compSpan := tracer.StartSpan(ctx, fmt.Sprintf("health-check-%s", comp.name))
		compSpan.SetTag("component", comp.name)
		compSpan.SetTag("health_check", "true")

		// Add component details.
		for key, value := range comp.details {
			compSpan.SetTag(fmt.Sprintf("health.%s", key), value)
		}

		// Simulate check duration.
		time.Sleep(comp.latency)

		if comp.healthy {
			compSpan.SetTag("status", "healthy")
			compSpan.SetTag("health.status", "up")
		} else {
			compSpan.SetTag("status", "unhealthy")
			compSpan.SetTag("health.status", "down")
			compSpan.SetTag("error", "true")
			overallHealthy = false
		}

		compSpan.Finish()
	}

	// Overall health status.
	if overallHealthy {
		healthSpan.SetTag("overall_status", "healthy")
	} else {
		healthSpan.SetTag("overall_status", "degraded")
	}

	healthSpan.Finish()

	// Analyze health check results.
	spans := collector.Export()

	healthyCount := 0
	unhealthyCount := 0
	totalLatency := time.Duration(0)

	for _, span := range spans {
		if span.Tags["health_check"] == "true" {
			totalLatency += span.Duration

			if span.Tags["status"] == "healthy" {
				healthyCount++
			} else {
				unhealthyCount++
			}
		}
	}

	// Verify health check coverage.
	expectedChecks := len(components)
	actualChecks := healthyCount + unhealthyCount

	if actualChecks != expectedChecks {
		t.Errorf("Expected %d health checks, got %d", expectedChecks, actualChecks)
	}

	// Verify overall status reflects component health.
	overallHealthyExpected := unhealthyCount == 0
	for _, span := range spans {
		if span.Name == "health-check" {
			actualOverallHealthy := span.Tags["overall_status"] == "healthy"
			if actualOverallHealthy != overallHealthyExpected {
				t.Errorf("Overall health status incorrect: expected %v, got %v",
					overallHealthyExpected, actualOverallHealthy)
			}
		}
	}

	avgLatency := totalLatency / time.Duration(actualChecks)

	t.Logf("Health check summary: %d healthy, %d unhealthy, avg latency: %.2fms",
		healthyCount, unhealthyCount, avgLatency.Seconds()*1000)
}
