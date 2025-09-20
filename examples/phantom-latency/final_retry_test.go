package phantomlatency

import (
	"context"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
)

// TestPhantomLatencyWithRetries demonstrates the full phantom latency pattern
func TestPhantomLatencyWithRetries(t *testing.T) {
	clock := clockz.NewFakeClock()
	service := NewPaymentService(clock)
	
	// Force rate limiting to trigger retries
	service.sdk.rateLimitAfter = 0

	ctx := context.Background()
	req := PaymentRequest{
		CustomerID: "megamart",
		Amount:     9999,
		Currency:   "USD",
		OrderID:    "phantom_001",
	}

	start := clock.Now()
	
	done := make(chan error, 1)
	go func() {
		done <- service.ProcessPayment(ctx, req)
	}()

	// Let request start
	time.Sleep(10 * time.Millisecond)
	
	// Advance through all retry attempts
	// The SDK will do 5 attempts (attempts 0-4) with exponential backoff
	totalAdvanced := time.Duration(0)
	
	for attempt := 0; attempt < 5; attempt++ {
		// Network latency for this attempt
		clock.Advance(62 * time.Millisecond)
		totalAdvanced += 62 * time.Millisecond
		clock.BlockUntilReady()
		
		// If not the last attempt, add backoff time
		if attempt < 4 {
			backoff := 100 * time.Millisecond
			for i := 0; i < attempt; i++ {
				backoff *= 2
			}
			clock.Advance(backoff)
			totalAdvanced += backoff
			clock.BlockUntilReady()
		}
	}
	
	// Wait for completion
	select {
	case err := <-done:
		actualDuration := clock.Since(start)
		
		if err == nil {
			t.Fatal("Expected payment to fail after max retries")
		}
		
		// Get metrics
		metrics := service.metrics.Stats()
		metricsLatency := time.Duration(metrics["p99_latency_ms"].(int64)) * time.Millisecond
		
		// Get SDK stats
		sdkStats := service.sdk.Stats()
		
		t.Logf("\n=== PHANTOM LATENCY REVEALED ===")
		t.Logf("What metrics show: %v", metricsLatency)
		t.Logf("What customer experienced: %v", actualDuration)
		t.Logf("Time we advanced: %v", totalAdvanced)
		t.Logf("")
		t.Logf("Hidden SDK behavior:")
		t.Logf("  - Total requests made: %d", sdkStats["total_requests"])
		t.Logf("  - Rate limits hit: %d", sdkStats["rate_limits_hit"])
		t.Logf("  - Total retries: %d", sdkStats["total_retries"])
		t.Logf("  - Time spent in backoff: %dms", sdkStats["backoff_time_ms"])
		
		// The phantom latency: metrics show ~62ms but reality is much worse
		if actualDuration > 1*time.Second && metricsLatency < 100*time.Millisecond {
			t.Logf("\n🔥 PHANTOM LATENCY DETECTED!")
			t.Logf("   Metrics mislead by %.1fx", float64(actualDuration)/float64(metricsLatency))
		}
		
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Payment should have completed by now")
	}
}