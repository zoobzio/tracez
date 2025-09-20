package phantomlatency

import (
	"context"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
)

// TestRetryDemonstration shows how fake clocks reveal hidden retry patterns
func TestRetryDemonstration(t *testing.T) {
	clock := clockz.NewFakeClock()
	service := NewPaymentService(clock)
	
	// Force rate limiting immediately
	service.sdk.rateLimitAfter = 0  // Rate limit on first request

	ctx := context.Background()
	req := PaymentRequest{
		CustomerID: "test",
		Amount:     100,
		Currency:   "USD",
		OrderID:    "retry_test",
	}

	start := clock.Now()
	
	done := make(chan error, 1)
	go func() {
		done <- service.ProcessPayment(ctx, req)
	}()
	
	// Let request start
	time.Sleep(10 * time.Millisecond)
	
	// Advance through multiple retry attempts
	// Attempt 1: 62ms network + rate limit response
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Backoff: 100ms
	clock.Advance(100 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Attempt 2: 62ms network + rate limit response
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Backoff: 200ms (exponential)
	clock.Advance(200 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Attempt 3: 62ms network + rate limit response
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Backoff: 400ms
	clock.Advance(400 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Attempt 4: 62ms network + rate limit response
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Backoff: 800ms
	clock.Advance(800 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Attempt 5: 62ms network + rate limit response
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Should fail after max retries
	select {
	case err := <-done:
		duration := clock.Since(start)
		t.Logf("Payment completed in: %v", duration)
		
		if err == nil {
			t.Fatal("Expected payment to fail after retries")
		}
		
		// Get metrics vs reality
		metrics := service.metrics.Stats()
		metricsLatency := time.Duration(metrics["p99_latency_ms"].(int64)) * time.Millisecond
		
		// Get SDK stats
		sdkStats := service.sdk.Stats()
		
		t.Logf("\n=== THE PHANTOM LATENCY REVEALED ===")
		t.Logf("What metrics show: %v", metricsLatency)
		t.Logf("What customer experienced: %v", duration)
		t.Logf("Hidden SDK behavior:")
		t.Logf("  - Total requests made: %d", sdkStats["total_requests"])
		t.Logf("  - Rate limits hit: %d", sdkStats["rate_limits_hit"])
		t.Logf("  - Total retries: %d", sdkStats["total_retries"])
		t.Logf("  - Time spent in backoff: %dms", sdkStats["backoff_time_ms"])
		
		// Show the dramatic difference
		if duration > 1*time.Second && metricsLatency < 100*time.Millisecond {
			t.Logf("\n🔥 SMOKING GUN: Metrics show %v but took %v!", metricsLatency, duration)
		}
		
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Payment should have completed by now")
	}
}