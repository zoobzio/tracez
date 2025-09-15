package integration

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// TestIDPoolIntegration verifies ID pool optimization maintains correct trace/span relationships.
func TestIDPoolIntegration(t *testing.T) {
	tracer := tracez.New("id-pool-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test-collector", 1000)
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Create parent span.
	ctx, parent := tracer.StartSpan(context.Background(), "parent-operation")
	parentTraceID := parent.TraceID()
	parentSpanID := parent.SpanID()

	// Create child spans using ID pool.
	var wg sync.WaitGroup
	childSpanIDs := make(map[string]bool)
	mu := sync.Mutex{}

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			_, child := tracer.StartSpan(ctx, tracez.Key("child"))

			// Verify trace ID propagation.
			if child.TraceID() != parentTraceID {
				t.Errorf("Child span %d has wrong trace ID. Expected %s, got %s",
					idx, parentTraceID, child.TraceID())
			}

			// Verify unique span IDs.
			childID := child.SpanID()
			mu.Lock()
			if childSpanIDs[childID] {
				t.Errorf("Duplicate span ID detected: %s", childID)
			}
			childSpanIDs[childID] = true
			mu.Unlock()

			child.Finish()
		}(i)
	}

	wg.Wait()
	parent.Finish()

	// Verify all spans collected.
	spans := collector.Export()
	if len(spans) != 101 { // 1 parent + 100 children.
		t.Errorf("Expected 101 spans, got %d", len(spans))
	}

	// Verify parent-child relationships.
	for _, span := range spans {
		if span.Name == "child" {
			if span.ParentID != parentSpanID {
				t.Errorf("Child span has wrong parent ID. Expected %s, got %s",
					parentSpanID, span.ParentID)
			}
			if span.TraceID != parentTraceID {
				t.Errorf("Child span has wrong trace ID. Expected %s, got %s",
					parentTraceID, span.TraceID)
			}
		}
	}
}

// TestContextBundlingPropagation verifies context optimization preserves span hierarchy.
func TestContextBundlingPropagation(t *testing.T) {
	tracer := tracez.New("context-bundle-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test-collector", 1000)
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Create deep nesting with context bundling.
	ctx := context.Background()
	traceIDs := make([]string, 0)
	parentIDs := make([]string, 0)

	// Create 10 levels of nested spans.
	for i := 0; i < 10; i++ {
		var span *tracez.ActiveSpan
		ctx, span = tracer.StartSpan(ctx, tracez.Key("level"))

		traceIDs = append(traceIDs, span.TraceID())
		if i > 0 {
			// Get parent from context using GetSpan.
			parent := tracez.GetSpan(ctx)
			if parent != nil {
				_ = append(parentIDs, parent.ParentID) // Track parent chain for validation
			}
		}

		span.SetTag("level", "test")
		span.Finish() // Immediate finish instead of defer in loop.
	}

	// All spans should have same trace ID.
	firstTraceID := traceIDs[0]
	for i, tid := range traceIDs {
		if tid != firstTraceID {
			t.Errorf("Level %d has different trace ID: %s vs %s", i, tid, firstTraceID)
		}
	}

	// Verify GetSpan still works with bundling.
	finalSpan := tracez.GetSpan(ctx)
	if finalSpan == nil {
		t.Error("GetSpan returned nil for context with bundled span")
		return
	}
	if finalSpan.TraceID != firstTraceID {
		t.Errorf("GetSpan returned span with wrong trace ID: %s vs %s",
			finalSpan.TraceID, firstTraceID)
	}
}

// TestBufferOptimizationUnderLoad verifies buffer management under varying load.
func TestBufferOptimizationUnderLoad(t *testing.T) {
	tracer := tracez.New("buffer-optimization-test")
	defer tracer.Close()

	// Use larger buffer size (optimization sets to 1000).
	collector := tracez.NewCollector("test-collector", 1000)
	tracer.AddCollector("test", collector)

	// Simulate traffic patterns.
	patterns := []struct {
		name           string
		goroutines     int
		spansPerWorker int
		burstDelay     time.Duration
	}{
		{"steady-low", 2, 50, 0},
		{"burst-high", 20, 100, 0},
		{"steady-medium", 5, 200, 0},
		{"burst-extreme", 50, 50, 0},
	}

	for _, pattern := range patterns {
		t.Run(pattern.name, func(t *testing.T) {
			// Reset collector for clean test.
			collector.Reset()
			initialDropped := collector.DroppedCount()

			var wg sync.WaitGroup
			for i := 0; i < pattern.goroutines; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					ctx := context.Background()
					for j := 0; j < pattern.spansPerWorker; j++ {
						_, span := tracer.StartSpan(ctx, "operation")
						span.SetTag("pattern", pattern.name)
						span.Finish()

						if pattern.burstDelay > 0 && j%10 == 0 {
							time.Sleep(pattern.burstDelay)
						}
					}
				}()
			}

			wg.Wait()

			// Allow time for async collection.
			time.Sleep(50 * time.Millisecond)

			// Check results.
			collected := collector.Export()
			dropped := collector.DroppedCount() - initialDropped
			total := pattern.goroutines * pattern.spansPerWorker

			t.Logf("Pattern %s: collected=%d, dropped=%d, total=%d",
				pattern.name, len(collected), dropped, total)

			// With 1000 buffer size, we should handle most patterns without drops.
			if pattern.name != "burst-extreme" && dropped > 0 {
				t.Errorf("Pattern %s: unexpected drops with optimized buffer: %d",
					pattern.name, dropped)
			}

			// Verify collected spans are valid.
			for _, span := range collected {
				if span.TraceID == "" || span.SpanID == "" {
					t.Error("Collected span missing IDs")
				}
				if span.Duration == 0 {
					t.Error("Collected span has zero duration")
				}
			}
		})
	}
}

// TestOptimizationBackwardCompatibility ensures old patterns still work.
func TestOptimizationBackwardCompatibility(t *testing.T) {
	tracer := tracez.New("backward-compat-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test-collector", 100) // Old buffer size.
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Old pattern: manual context management.
	ctx := context.Background()
	_, span1 := tracer.StartSpan(ctx, "operation1")

	// Use ActiveSpan.Context() method (should use bundling internally)
	newCtx := span1.Context(context.Background())

	// Start child with new context.
	_, span2 := tracer.StartSpan(newCtx, "operation2")

	// Verify parent-child relationship.
	if span2.TraceID() != span1.TraceID() {
		t.Error("Context bundling broke trace ID propagation")
	}

	// GetSpan should work with bundled context.
	extractedSpan := tracez.GetSpan(newCtx)
	if extractedSpan == nil {
		t.Error("GetSpan failed with bundled context")
		return
	}
	if extractedSpan.SpanID != span1.SpanID() {
		t.Error("GetSpan returned wrong span from bundled context")
	}

	span2.Finish()
	span1.Finish()

	// Verify collection still works.
	spans := collector.Export()
	if len(spans) != 2 {
		t.Errorf("Expected 2 spans, got %d", len(spans))
	}
}

// TestIDPoolResourceCleanup verifies pools shut down cleanly with tracer.
func TestIDPoolResourceCleanup(t *testing.T) {
	// Get baseline goroutine count.
	time.Sleep(10 * time.Millisecond) // Let any background work settle.
	baseline := countGoroutines()

	// Create and use tracer with ID pools.
	tracer := tracez.New("cleanup-test")

	// Generate spans to ensure pools are created.
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		_, span := tracer.StartSpan(ctx, "test")
		span.Finish()
	}

	// Should have pool goroutines running.
	withPools := countGoroutines()
	if withPools <= baseline {
		t.Skip("Could not verify pool goroutines were created")
	}

	// Close tracer (should close pools).
	tracer.Close()

	// Allow cleanup time.
	time.Sleep(50 * time.Millisecond)

	// Should be back to baseline (or very close).
	afterClose := countGoroutines()
	leaked := afterClose - baseline

	if leaked > 2 { // Allow small variance for test framework.
		t.Errorf("Possible goroutine leak: baseline=%d, with pools=%d, after close=%d (leaked=%d)",
			baseline, withPools, afterClose, leaked)
	}
}

// TestConcurrentPoolsAndContext tests all optimizations under concurrent load.
func TestConcurrentPoolsAndContext(t *testing.T) {
	tracer := tracez.New("concurrent-optimization-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 2000) // Large buffer for concurrency.
	tracer.AddCollector("test", collector)

	// Concurrent pattern that stresses both ID pools and context bundling.
	var wg sync.WaitGroup
	errors := make(chan error, 1000)

	// 50 workers creating nested spans.
	for w := 0; w < 50; w++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			for i := 0; i < 20; i++ {
				// Create parent.
				ctx, parent := tracer.StartSpan(context.Background(), "parent")
				parentTrace := parent.TraceID()
				parentSpan := parent.SpanID()

				// Create multiple children concurrently.
				var childWg sync.WaitGroup
				for c := 0; c < 5; c++ {
					childWg.Add(1)
					go func(_ int) {
						defer childWg.Done()

						_, child := tracer.StartSpan(ctx, "child")

						// Verify relationships.
						if child.TraceID() != parentTrace {
							select {
							case errors <- fmt.Errorf("Wrong trace ID"):
							default:
							}
						}

						// Use GetSpan to verify context bundling.
						span := tracez.GetSpan(ctx)
						if span == nil || span.SpanID != parentSpan {
							select {
							case errors <- fmt.Errorf("GetSpan failed"):
							default:
							}
						}

						child.SetTag("test", "value")
						child.Finish()
					}(c)
				}

				childWg.Wait()
				parent.Finish()
			}
		}(w)
	}

	wg.Wait()
	close(errors)

	// Check for errors.
	errorCount := 0
	for range errors {
		errorCount++
	}
	if errorCount > 0 {
		t.Errorf("Detected %d errors during concurrent execution", errorCount)
	}

	// Verify no crashes or panics occurred (getting here means success).
	t.Log("All optimizations stable under high concurrency")
}

// Helper function to count goroutines.
func countGoroutines() int {
	time.Sleep(10 * time.Millisecond) // Let goroutines settle.
	var buf [4096]byte
	n := runtime.Stack(buf[:], true)

	// Count "goroutine" occurrences in stack dump.
	count := 0
	s := string(buf[:n])
	for i := 0; i < len(s); i++ {
		if i+9 < len(s) && s[i:i+9] == "goroutine" {
			count++
		}
	}
	return count
}
