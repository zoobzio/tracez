package integration

import (
	"context"
	"sync"
	"testing"

	"github.com/zoobzio/tracez"
)

// TestOptimizationRaceConditions tests concurrent access with optimizations.
func TestOptimizationRaceConditions(_ *testing.T) {
	tracer := tracez.New("race-test-service")
	defer tracer.Close()

	var wg sync.WaitGroup
	numGoroutines := 20
	spansPerGoroutine := 50

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			ctx := context.Background()

			for j := 0; j < spansPerGoroutine; j++ {
				// Create nested spans to test both ID pools and context bundling.
				ctx1, span1 := tracer.StartSpan(ctx, "parent")
				_, span2 := tracer.StartSpan(ctx1, "child")

				// Add some tag operations.
				span1.SetTag("routine", "test")
				span2.SetTag("iteration", "test")

				span2.Finish()
				span1.Finish()
			}
		}(i)
	}

	wg.Wait()

	// If we get here without race detection errors, the optimizations are thread-safe.
}
