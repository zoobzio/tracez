package reliability

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
	"github.com/zoobzio/tracez"
)

// Context cascade tests - verify trace context propagation under extreme conditions
// Tests deep nesting, concurrent propagation, and context corruption scenarios

func TestContextCascade(t *testing.T) {
	config := getReliabilityConfig()

	switch config.Level {
	case "basic":
		t.Run("deep_nesting", testDeepNesting)
		t.Run("concurrent_propagation", testConcurrentPropagation)
		t.Run("context_corruption", testContextCorruption)
	case "stress":
		t.Run("extreme_depth", testExtremeDepth)
		t.Run("massive_fanout", testMassiveFanout)
		t.Run("context_storm", testContextStorm)
	default:
		t.Skip("TRACEZ_RELIABILITY_LEVEL not set, skipping reliability tests")
	}
}

// testDeepNesting verifies trace propagation through deep call stacks.
func testDeepNesting(t *testing.T) {
	fakeClock := clockz.NewFakeClock()
	tracer := tracez.New("deep-nesting-test").WithClock(fakeClock)
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000).WithClock(fakeClock)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	maxDepth := 100

	var recursiveSpan func(ctx context.Context, depth int) context.Context
	recursiveSpan = func(ctx context.Context, depth int) context.Context {
		if depth <= 0 {
			return ctx
		}

		newCtx, span := tracer.StartSpan(ctx, fmt.Sprintf("depth-%d", depth))
		span.SetTag("depth", fmt.Sprintf("%d", depth))
		span.SetTag("operation", "recursive-nesting")

		// Recurse deeper
		finalCtx := recursiveSpan(newCtx, depth-1)

		span.Finish()
		return finalCtx
	}

	// Start the recursive chain
	rootCtx, rootSpan := tracer.StartSpan(context.Background(), "root")
	rootSpan.SetTag("operation", "root")

	finalCtx := recursiveSpan(rootCtx, maxDepth)
	rootSpan.Finish()

	// Verify final context still has trace information
	finalSpan := tracez.GetSpan(finalCtx)
	if finalSpan == nil {
		t.Error("Lost trace context at maximum depth")
	} else {
		t.Logf("Final span at depth %d: TraceID=%s", maxDepth, finalSpan.TraceID)
	}

	// With sync mode, collection is immediate - no waiting needed

	// Verify all spans were collected
	exported := collector.Export()
	expectedSpans := maxDepth + 1 // recursive spans + root

	if len(exported) != expectedSpans {
		t.Errorf("Expected %d spans from deep nesting, got %d", expectedSpans, len(exported))
	}

	// Verify trace continuity - all spans should have same trace ID
	if len(exported) > 0 {
		rootTraceID := exported[0].TraceID
		for i := range exported {
			if exported[i].TraceID != rootTraceID {
				t.Errorf("Span %d has different trace ID: expected %s, got %s",
					i, rootTraceID, exported[i].TraceID)
			}
		}
	}
}

// testConcurrentPropagation verifies context propagation across goroutines.
func testConcurrentPropagation(t *testing.T) {
	fakeClock := clockz.NewFakeClock()
	tracer := tracez.New("concurrent-propagation-test").WithClock(fakeClock)
	defer tracer.Close()

	collector := tracez.NewCollector("test", 5000).WithClock(fakeClock)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Start root span
	rootCtx, rootSpan := tracer.StartSpan(context.Background(), "concurrent-root")
	rootSpan.SetTag("operation", "concurrent-test")
	defer rootSpan.Finish()

	numGoroutines := runtime.NumCPU() * 4
	spansPerGoroutine := 50

	var wg sync.WaitGroup
	var processedSpans atomic.Int64

	// Launch concurrent workers that propagate context
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Each goroutine creates its own span from root context
			workerCtx, workerSpan := tracer.StartSpan(rootCtx, fmt.Sprintf("worker-%d", goroutineID))
			workerSpan.SetTag("goroutine", fmt.Sprintf("%d", goroutineID))
			defer workerSpan.Finish()

			// Create child spans
			for j := 0; j < spansPerGoroutine; j++ {
				childCtx, childSpan := tracer.StartSpan(workerCtx, fmt.Sprintf("child-%d-%d", goroutineID, j))
				childSpan.SetTag("goroutine", fmt.Sprintf("%d", goroutineID))
				childSpan.SetTag("child", fmt.Sprintf("%d", j))

				// Verify context propagation
				currentSpan := tracez.GetSpan(childCtx)
				if currentSpan == nil {
					t.Errorf("Lost context in goroutine %d, child %d", goroutineID, j)
				} else if currentSpan.TraceID != rootSpan.TraceID() {
					t.Errorf("Trace ID mismatch in goroutine %d, child %d", goroutineID, j)
				}

				childSpan.Finish()
				processedSpans.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// With sync mode, collection is immediate - no waiting needed

	// Verify span collection
	exported := collector.Export()
	expectedSpans := int64(1 + numGoroutines + numGoroutines*spansPerGoroutine) // root + workers + children

	t.Logf("Concurrent propagation results:")
	t.Logf("  Goroutines: %d", numGoroutines)
	t.Logf("  Expected spans: %d", expectedSpans)
	t.Logf("  Collected spans: %d", len(exported))
	t.Logf("  Processed spans: %d", processedSpans.Load())

	if int64(len(exported)) < expectedSpans*8/10 { // Allow 20% loss due to timing
		t.Errorf("Too few spans collected: got %d, expected ~%d", len(exported), expectedSpans)
	}

	// Verify trace continuity
	rootTraceID := rootSpan.TraceID()
	discontinuity := 0
	for i := range exported {
		if exported[i].TraceID != rootTraceID {
			discontinuity++
		}
	}

	if discontinuity > 0 {
		t.Errorf("Trace discontinuity detected: %d spans with wrong trace ID", discontinuity)
	}
}

// testContextCorruption verifies system handles corrupted contexts gracefully.
func testContextCorruption(t *testing.T) {
	fakeClock := clockz.NewFakeClock()
	tracer := tracez.New("corruption-test").WithClock(fakeClock)
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000).WithClock(fakeClock)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Test various corruption scenarios
	scenarios := []struct {
		name    string
		setupFn func() context.Context
	}{
		{
			name: "nil_context",
			setupFn: func() context.Context {
				return nil
			},
		},
		{
			name:    "empty_context",
			setupFn: context.Background,
		},
		{
			name: "canceled_context",
			setupFn: func() context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
		{
			name: "timeout_context",
			setupFn: func() context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
				time.Sleep(time.Millisecond) // Ensure timeout
				defer cancel()
				return ctx
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			corruptCtx := scenario.setupFn()

			// System should handle corrupted context gracefully
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Panic with %s: %v", scenario.name, r)
					}
				}()

				ctx, span := tracer.StartSpan(corruptCtx, fmt.Sprintf("corruption-test-%s", scenario.name))
				span.SetTag("scenario", scenario.name)
				span.SetTag("corruption", "handled")
				span.Finish()

				// Verify we got a valid context back
				if ctx == nil {
					t.Errorf("Got nil context from corrupted input: %s", scenario.name)
				}

				// Verify span is accessible from returned context
				retrievedSpan := tracez.GetSpan(ctx)
				if retrievedSpan == nil {
					t.Errorf("Cannot retrieve span from context: %s", scenario.name)
				}
			}()
		})
	}

	// Verify all corruption scenarios were handled
	exported := collector.Export()
	if len(exported) != len(scenarios) {
		t.Errorf("Expected %d spans from corruption tests, got %d", len(scenarios), len(exported))
	}
}

// testExtremeDepth - stress test with very deep nesting.
func testExtremeDepth(t *testing.T) {
	fakeClock := clockz.NewFakeClock()
	tracer := tracez.New("extreme-depth-test").WithClock(fakeClock)
	defer tracer.Close()

	collector := tracez.NewCollector("test", 10000).WithClock(fakeClock)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	extremeDepth := 5000

	// Track memory usage during deep nesting
	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	initialMem := memStats.HeapInuse

	start := time.Now()

	// Iterative approach to avoid stack overflow
	ctx := context.Background()
	spans := make([]*tracez.ActiveSpan, extremeDepth)

	// Create nested spans iteratively
	for i := 0; i < extremeDepth; i++ {
		var span *tracez.ActiveSpan
		ctx, span = tracer.StartSpan(ctx, fmt.Sprintf("extreme-depth-%d", i))
		span.SetTag("depth", fmt.Sprintf("%d", i))
		spans[i] = span
	}

	// Verify context integrity at extreme depth
	finalSpan := tracez.GetSpan(ctx)
	if finalSpan == nil {
		t.Error("Lost context at extreme depth")
	}

	// Finish spans in reverse order (LIFO)
	for i := extremeDepth - 1; i >= 0; i-- {
		spans[i].Finish()
	}

	duration := time.Since(start)

	// Memory check
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	finalMem := memStats.HeapInuse
	memGrowth := float64(finalMem-initialMem) / float64(initialMem) * 100

	// Performance metrics
	spansPerSecond := float64(extremeDepth) / duration.Seconds()

	t.Logf("Extreme depth results:")
	t.Logf("  Depth: %d spans", extremeDepth)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Rate: %.0f spans/sec", spansPerSecond)
	t.Logf("  Memory growth: %.1f%%", memGrowth)

	// With sync mode, collection is immediate - no waiting needed
	exported := collector.Export()
	if len(exported) != extremeDepth {
		t.Errorf("Expected %d spans from extreme depth, got %d", extremeDepth, len(exported))
	}

	// Performance should be reasonable
	if spansPerSecond < 1000 {
		t.Errorf("Extreme depth performance too slow: %.0f spans/sec", spansPerSecond)
	}

	// Memory usage should be controlled
	if memGrowth > 100 {
		t.Errorf("Excessive memory growth: %.1f%%", memGrowth)
	}
}

// testMassiveFanout - stress test with wide span trees.
func testMassiveFanout(t *testing.T) {
	fakeClock := clockz.NewFakeClock()
	tracer := tracez.New("massive-fanout-test").WithClock(fakeClock)
	defer tracer.Close()

	collector := tracez.NewCollector("test", 20000).WithClock(fakeClock)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Create wide span tree: root -> level1 (100 spans) -> level2 (10 spans each)
	rootCtx, rootSpan := tracer.StartSpan(context.Background(), "fanout-root")
	rootSpan.SetTag("level", "0")
	defer rootSpan.Finish()

	level1Count := 100
	level2CountPerLevel1 := 50
	totalExpected := 1 + level1Count + (level1Count * level2CountPerLevel1)

	var wg sync.WaitGroup
	var createdSpans atomic.Int64

	start := time.Now()

	// Create level 1 spans concurrently
	for i := 0; i < level1Count; i++ {
		wg.Add(1)
		go func(level1ID int) {
			defer wg.Done()

			// Create level 1 span
			level1Ctx, level1Span := tracer.StartSpan(rootCtx, fmt.Sprintf("level1-%d", level1ID))
			level1Span.SetTag("level", "1")
			level1Span.SetTag("id", fmt.Sprintf("%d", level1ID))
			defer level1Span.Finish()
			createdSpans.Add(1)

			// Create level 2 spans
			for j := 0; j < level2CountPerLevel1; j++ {
				level2Ctx, level2Span := tracer.StartSpan(level1Ctx, fmt.Sprintf("level2-%d-%d", level1ID, j))
				level2Span.SetTag("level", "2")
				level2Span.SetTag("parent", fmt.Sprintf("%d", level1ID))
				level2Span.SetTag("id", fmt.Sprintf("%d", j))
				level2Span.Finish()
				createdSpans.Add(1)
				_ = level2Ctx // Satisfy linter
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	// Performance metrics
	actualCreated := createdSpans.Load()
	spansPerSecond := float64(actualCreated) / duration.Seconds()

	t.Logf("Massive fanout results:")
	t.Logf("  Expected spans: %d", totalExpected)
	t.Logf("  Created spans: %d", actualCreated)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Rate: %.0f spans/sec", spansPerSecond)

	// With sync mode, collection is immediate - no waiting needed

	// Verify collection
	exported := collector.Export()
	t.Logf("  Collected spans: %d", len(exported))

	if int64(len(exported)) < actualCreated*8/10 { // Allow 20% loss
		t.Errorf("Too few spans collected from fanout: got %d, expected ~%d",
			len(exported), actualCreated)
	}

	// Verify trace continuity
	rootTraceID := rootSpan.TraceID()
	for i := range exported {
		if exported[i].TraceID != rootTraceID {
			t.Errorf("Trace discontinuity in fanout: span %s has wrong trace ID", exported[i].SpanID)
			break
		}
	}
}

// testContextStorm - extreme concurrent context operations.
func testContextStorm(t *testing.T) {
	tracer := tracez.New("context-storm-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 50000)
	// Note: collector cleanup handled by tracer.Close() in production use
	tracer.AddCollector("test", collector)

	duration := 10 * time.Second
	numGoroutines := runtime.NumCPU() * 8

	var totalOperations atomic.Int64
	var successfulOperations atomic.Int64
	var errors atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			localCtx := ctx
			depth := 0
			rand := rand.New(rand.NewSource(time.Now().UnixNano() + int64(goroutineID)))

			for {
				select {
				case <-ctx.Done():
					return
				default:
					func() {
						defer func() {
							if r := recover(); r != nil {
								errors.Add(1)
							}
						}()

						totalOperations.Add(1)

						// Random context operations
						switch rand.Intn(4) {
						case 0: // Start new span
							newCtx, span := tracer.StartSpan(localCtx, fmt.Sprintf("storm-%d-%d", goroutineID, depth))
							span.SetTag("goroutine", fmt.Sprintf("%d", goroutineID))
							span.SetTag("depth", fmt.Sprintf("%d", depth))
							localCtx = newCtx
							depth++
							span.Finish()
							successfulOperations.Add(1)

						case 1: // Get span from context
							span := tracez.GetSpan(localCtx)
							if span != nil {
								successfulOperations.Add(1)
							}

						case 2: // Create child context
							if depth > 0 {
								newCtx, span := tracer.StartSpan(localCtx, fmt.Sprintf("child-%d-%d", goroutineID, depth))
								span.SetTag("type", "child")
								span.Finish()
								localCtx = newCtx
								successfulOperations.Add(1)
							}

						case 3: // Reset to background (simulate new request)
							if depth > 5 {
								localCtx = ctx
								depth = 0
							}
							successfulOperations.Add(1)
						}
					}()

					// Brief pause to prevent CPU saturation
					time.Sleep(time.Microsecond * 10)
				}
			}
		}(i)
	}

	wg.Wait()

	total := totalOperations.Load()
	successful := successfulOperations.Load()
	failed := errors.Load()
	successRate := float64(successful) / float64(total) * 100
	opsPerSecond := float64(total) / duration.Seconds()

	t.Logf("Context storm results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Goroutines: %d", numGoroutines)
	t.Logf("  Total operations: %d", total)
	t.Logf("  Successful: %d", successful)
	t.Logf("  Errors: %d", failed)
	t.Logf("  Success rate: %.1f%%", successRate)
	t.Logf("  Rate: %.0f ops/sec", opsPerSecond)

	// System should handle the storm reasonably well
	if successRate < 90 {
		t.Errorf("Too many failures in context storm: %.1f%% success", successRate)
	}

	if opsPerSecond < 1000 {
		t.Errorf("Context storm performance too low: %.0f ops/sec", opsPerSecond)
	}
}
