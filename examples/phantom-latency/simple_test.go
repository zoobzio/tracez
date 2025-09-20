package phantomlatency

import (
	"context"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
)

// TestSimpleMetrics tests the basic phantom latency issue
func TestSimpleMetrics(t *testing.T) {
	clock := clockz.NewFakeClock()
	service := NewPaymentService(clock)
	
	// Don't simulate load conditions for simple test
	// service.sdk.SimulateLoadScenario()

	ctx := context.Background()
	req := PaymentRequest{
		CustomerID: "test",
		Amount:     100,
		Currency:   "USD",
		OrderID:    "test_001",
	}

	start := clock.Now()
	
	// Run payment in goroutine
	done := make(chan error, 1)
	go func() {
		done <- service.ProcessPayment(ctx, req)
	}()

	// Let the request start
	time.Sleep(10 * time.Millisecond)
	
	// Advance through the expected operations
	// Network latency: 62ms
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()

	// Wait for completion
	select {
	case err := <-done:
		if err != nil {
			t.Logf("Payment failed: %v", err)
		}
		duration := clock.Since(start)
		t.Logf("Payment took: %v", duration)
		
		// Get metrics
		metrics := service.metrics.Stats()
		metricsLatency := time.Duration(metrics["p99_latency_ms"].(int64)) * time.Millisecond
		t.Logf("Metrics show: %v", metricsLatency)
		
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Payment timed out")
	}
}