package phantomlatency

import (
	"context"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
)

// TestDebugRetry helps debug the retry behavior
func TestDebugRetry(t *testing.T) {
	clock := clockz.NewFakeClock()
	sdk := NewStreamPaySDK(clock)
	
	// Force immediate rate limiting
	sdk.rateLimitAfter = 0

	ctx := context.Background()
	request := map[string]string{
		"customer_id": "test",
		"amount":      "100",
		"currency":    "USD",
		"order_id":    "debug_001",
	}

	done := make(chan error, 1)
	go func() {
		_, err := sdk.Execute(ctx, request)
		done <- err
	}()

	// Let it start
	time.Sleep(10 * time.Millisecond)
	
	// Step through each retry manually (0-indexed like the code)
	for attempt := 0; attempt < 5; attempt++ {
		t.Logf("Advancing through attempt %d", attempt)
		
		// Connection pool timeout (if needed)
		clock.Advance(50 * time.Millisecond)
		clock.BlockUntilReady()
		
		// Network latency for this attempt  
		clock.Advance(62 * time.Millisecond)
		clock.BlockUntilReady()
		
		// Check if done
		select {
		case err := <-done:
			t.Logf("SDK finished after %d attempts with error: %v", attempt+1, err)
			
			stats := sdk.Stats()
			t.Logf("SDK Stats: %+v", stats)
			return
		default:
			// Not done, should backoff (except on last attempt)
			if attempt < 4 {
				backoffTime := 100 * time.Millisecond
				for i := 0; i < attempt; i++ {
					backoffTime *= 2
				}
				t.Logf("Backing off for %v", backoffTime)
				clock.Advance(backoffTime)
				clock.BlockUntilReady()
			}
		}
	}
	
	// After all attempts, give a bit more time for cleanup
	clock.Advance(10 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Final check
	select {
	case err := <-done:
		t.Logf("SDK finished with error: %v", err)
		stats := sdk.Stats()
		t.Logf("SDK Stats: %+v", stats)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SDK never completed")
	}
}