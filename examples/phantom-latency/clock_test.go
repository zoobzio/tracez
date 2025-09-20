package phantomlatency

import (
	"context"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
)

// TestFakeClockBasics verifies fake clock works correctly
func TestFakeClockBasics(t *testing.T) {
	clock := clockz.NewFakeClock()
	
	// Basic sleep test
	start := clock.Now()
	go func() {
		clock.Sleep(100 * time.Millisecond)
	}()
	
	// Advance time
	clock.Advance(100 * time.Millisecond)
	clock.BlockUntilReady()
	
	elapsed := clock.Since(start)
	if elapsed != 100*time.Millisecond {
		t.Fatalf("Expected 100ms, got %v", elapsed)
	}
}

// TestSDKWithFakeClock tests basic SDK operation
func TestSDKWithFakeClock(t *testing.T) {
	clock := clockz.NewFakeClock()
	sdk := NewStreamPaySDK(clock)
	
	ctx := context.Background()
	request := map[string]string{
		"customer_id": "test",
		"amount":      "100",
		"currency":    "USD",
		"order_id":    "test_001",
	}

	done := make(chan struct{})
	var resp *StreamPayResponse
	var err error
	
	go func() {
		defer close(done)
		resp, err = sdk.Execute(ctx, request)
	}()

	// Let the request start
	time.Sleep(10 * time.Millisecond)
	
	// Advance through the network latency
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()

	// Wait for completion
	select {
	case <-done:
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		if resp == nil || !resp.Success {
			t.Fatal("Expected successful response")
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Request timed out")
	}
}