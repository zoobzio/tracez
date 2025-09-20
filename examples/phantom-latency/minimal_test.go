package phantomlatency

import (
	"context"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
)

// TestMinimalSDK tests the simplest possible SDK interaction
func TestMinimalSDK(t *testing.T) {
	clock := clockz.NewFakeClock()
	sdk := NewStreamPaySDK(clock)
	
	// Don't force rate limiting - use default config
	// sdk.rateLimitAfter = 1000  // This is already the default

	ctx := context.Background()
	request := map[string]string{
		"customer_id": "test",
		"amount":      "100",
		"currency":    "USD",
		"order_id":    "minimal_001",
	}

	done := make(chan error, 1)
	go func() {
		_, err := sdk.Execute(ctx, request)
		done <- err
	}()

	// Let it start
	time.Sleep(10 * time.Millisecond)
	
	// Advance only the network latency (62ms)
	clock.Advance(62 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Should complete successfully
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}
		t.Logf("SDK completed successfully")
		
		stats := sdk.Stats()
		t.Logf("SDK Stats: %+v", stats)
		
	case <-time.After(100 * time.Millisecond):
		t.Fatal("SDK took too long")
	}
}