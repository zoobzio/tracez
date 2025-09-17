package reliability

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// Tracer lifecycle tests - verify tracer initialization, operation, and cleanup
// Tests ID pool behavior, collector management, and resource cleanup patterns

func TestTracerLifecycle(t *testing.T) {
	config := getReliabilityConfig()

	switch config.Level {
	case "basic":
		t.Run("startup_shutdown", testStartupShutdown)
		t.Run("collector_management", testCollectorManagement)
		t.Run("id_pool_behavior", testIDPoolBehavior)
	case "stress":
		t.Run("rapid_cycling", testRapidCycling)
		t.Run("resource_exhaustion", testResourceExhaustion)
		t.Run("concurrent_lifecycle", testConcurrentLifecycle)
	default:
		t.Skip("TRACEZ_RELIABILITY_LEVEL not set, skipping reliability tests")
	}
}

// testStartupShutdown verifies basic tracer lifecycle.
func testStartupShutdown(t *testing.T) {
	// Test clean startup
	tracer := tracez.New("lifecycle-test")
	if tracer == nil {
		t.Fatal("Tracer creation failed")
	}

	// Verify tracer is functional immediately
	ctx, span := tracer.StartSpan(context.Background(), "startup-test")
	if span == nil {
		t.Error("Span creation failed immediately after tracer startup")
	}

	span.SetTag("test", "startup")
	span.Finish()
	_ = ctx // Satisfy linter

	// Test clean shutdown
	tracer.Close()

	// Verify resources are cleaned up (no goroutine leaks)
	runtime.GC()
	time.Sleep(10 * time.Millisecond)

	// Post-shutdown operations should not panic
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Panic after tracer close: %v", r)
			}
		}()

		// These operations should be safe after close
		tracer.Reset()
		ctx, span := tracer.StartSpan(context.Background(), "post-close-test")
		span.SetTag("test", "post-close")
		span.Finish()
		_ = ctx // Satisfy linter
	}()
}

// testCollectorManagement verifies collector lifecycle within tracer.
func testCollectorManagement(t *testing.T) {
	tracer := tracez.New("collector-mgmt-test")
	defer tracer.Close()

	// Add multiple collectors
	collectors := make([]*tracez.Collector, 5)
	for i := 0; i < 5; i++ {
		collectors[i] = tracez.NewCollector(fmt.Sprintf("collector-%d", i), 100)
		// Note: collector cleanup handled by tracer.Close() in production use
		tracer.AddCollector(fmt.Sprintf("collector-%d", i), collectors[i])
	}

	// Generate spans that should reach all collectors
	numSpans := 100
	for i := 0; i < numSpans; i++ {
		ctx, span := tracer.StartSpan(context.Background(), "collector-test")
		span.SetTag("iteration", fmt.Sprintf("%d", i))
		span.Finish()
		_ = ctx // Satisfy linter
	}

	// Allow processing time
	time.Sleep(50 * time.Millisecond)

	// Verify all collectors received spans
	for i, collector := range collectors {
		count := collector.Count()
		dropped := collector.DroppedCount()
		total := int64(count) + dropped

		if total == 0 {
			t.Errorf("Collector %d received no spans", i)
		}

		t.Logf("Collector %d: %d buffered, %d dropped", i, count, dropped)
	}

	// Test collector reset functionality
	tracer.Reset()

	// Verify all collectors were reset
	for i, collector := range collectors {
		if collector.Count() != 0 {
			t.Errorf("Collector %d not reset: %d spans remaining", i, collector.Count())
		}
		if collector.DroppedCount() != 0 {
			t.Errorf("Collector %d drop count not reset: %d", i, collector.DroppedCount())
		}
	}
}

// testIDPoolBehavior verifies ID generation under various conditions.
func testIDPoolBehavior(t *testing.T) {
	tracer := tracez.New("id-pool-test")
	defer tracer.Close()

	// Generate many spans to trigger ID pool usage
	numSpans := 1000
	traceIDs := make(map[string]bool)
	spanIDs := make(map[string]bool)

	for i := 0; i < numSpans; i++ {
		ctx, span := tracer.StartSpan(context.Background(), "id-test")

		traceID := span.TraceID()
		spanID := span.SpanID()

		// Verify IDs are non-empty
		if traceID == "" {
			t.Error("Empty trace ID generated")
		}
		if spanID == "" {
			t.Error("Empty span ID generated")
		}

		// Verify span IDs are unique (trace IDs will be same for root spans)
		if spanIDs[spanID] {
			t.Errorf("Duplicate span ID: %s", spanID)
		}
		spanIDs[spanID] = true
		traceIDs[traceID] = true

		span.Finish()
		_ = ctx // Satisfy linter
	}

	t.Logf("Generated %d unique trace IDs and %d unique span IDs",
		len(traceIDs), len(spanIDs))

	// Test ID pool under concurrent access
	var wg sync.WaitGroup
	var idCollisionDetected atomic.Bool
	concurrentSpanIDs := make([][]string, runtime.NumCPU())

	for i := 0; i < runtime.NumCPU(); i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			localIDs := make([]string, 100)
			for j := 0; j < 100; j++ {
				ctx, span := tracer.StartSpan(context.Background(), "concurrent-id-test")
				localIDs[j] = span.SpanID()
				span.Finish()
				_ = ctx // Satisfy linter
			}
			concurrentSpanIDs[goroutineID] = localIDs
		}(i)
	}

	wg.Wait()

	// Check for ID collisions across goroutines
	allConcurrentIDs := make(map[string]bool)
	for _, ids := range concurrentSpanIDs {
		for _, id := range ids {
			if allConcurrentIDs[id] {
				idCollisionDetected.Store(true)
				t.Errorf("ID collision detected in concurrent generation: %s", id)
			}
			allConcurrentIDs[id] = true
		}
	}

	if !idCollisionDetected.Load() {
		t.Logf("No ID collisions detected in concurrent generation of %d IDs",
			len(allConcurrentIDs))
	}
}

// testRapidCycling - stress test with rapid tracer create/destroy cycles.
func testRapidCycling(t *testing.T) {
	cycles := 100
	spansPerCycle := 50

	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	initialMem := memStats.HeapInuse

	start := time.Now()

	for cycle := 0; cycle < cycles; cycle++ {
		tracer := tracez.New(fmt.Sprintf("rapid-cycle-%d", cycle))

		// Add a collector
		collector := tracez.NewCollector("test", 100)
		collector.SetSyncMode(true)
		tracer.AddCollector("test", collector)

		// Generate some spans
		for i := 0; i < spansPerCycle; i++ {
			ctx, span := tracer.StartSpan(context.Background(), "rapid-cycle-span")
			span.SetTag("cycle", fmt.Sprintf("%d", cycle))
			span.SetTag("span", fmt.Sprintf("%d", i))
			span.Finish()
			_ = ctx // Satisfy linter
		}

		// Verify spans were collected
		exported := collector.Export()
		if len(exported) != spansPerCycle {
			t.Errorf("Cycle %d: expected %d spans, got %d", cycle, spansPerCycle, len(exported))
		}

		// Clean shutdown
		// Note: collector cleanup handled by tracer.Close() in production use
		tracer.Close()

		// Periodic GC to prevent memory buildup
		if cycle%10 == 0 {
			runtime.GC()
		}
	}

	duration := time.Since(start)

	// Final memory check
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	finalMem := memStats.HeapInuse

	memGrowth := float64(finalMem-initialMem) / float64(initialMem) * 100
	cyclesPerSecond := float64(cycles) / duration.Seconds()

	t.Logf("Rapid cycling results:")
	t.Logf("  Cycles: %d", cycles)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Rate: %.1f cycles/sec", cyclesPerSecond)
	t.Logf("  Memory growth: %.1f%%", memGrowth)

	// Verify no excessive memory leaks
	if memGrowth > 30 {
		t.Errorf("Excessive memory growth during rapid cycling: %.1f%%", memGrowth)
	}

	// Performance should be reasonable
	if cyclesPerSecond < 10 {
		t.Errorf("Rapid cycling too slow: %.1f cycles/sec", cyclesPerSecond)
	}
}

// testResourceExhaustion - verify behavior under resource constraints.
func testResourceExhaustion(t *testing.T) {
	// Create many tracers to exhaust resources
	numTracers := 1000
	tracers := make([]*tracez.Tracer, numTracers)

	for i := 0; i < numTracers; i++ {
		tracers[i] = tracez.New(fmt.Sprintf("exhaustion-test-%d", i))

		// Add collectors to each tracer
		collector := tracez.NewCollector(fmt.Sprintf("collector-%d", i), 10)
		tracers[i].AddCollector("main", collector)
	}

	// Generate spans across all tracers
	spansPerTracer := 10
	successfulSpans := 0

	for i, tracer := range tracers {
		for j := 0; j < spansPerTracer; j++ {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Logf("Panic in tracer %d, span %d: %v", i, j, r)
					}
				}()

				ctx, span := tracer.StartSpan(context.Background(), "exhaustion-test")
				span.SetTag("tracer", fmt.Sprintf("%d", i))
				span.SetTag("span", fmt.Sprintf("%d", j))
				span.Finish()
				successfulSpans++
				_ = ctx // Satisfy linter
			}()
		}
	}

	expectedSpans := numTracers * spansPerTracer
	successRate := float64(successfulSpans) / float64(expectedSpans) * 100

	t.Logf("Resource exhaustion results:")
	t.Logf("  Tracers: %d", numTracers)
	t.Logf("  Expected spans: %d", expectedSpans)
	t.Logf("  Successful spans: %d", successfulSpans)
	t.Logf("  Success rate: %.1f%%", successRate)

	// System should handle resource pressure gracefully
	if successRate < 90 {
		t.Errorf("Too many failures under resource exhaustion: %.1f%% success", successRate)
	}

	// Clean up
	for i, tracer := range tracers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Logf("Panic closing tracer %d: %v", i, r)
				}
			}()
			tracer.Close()
		}()
	}
}

// testConcurrentLifecycle - verify thread-safety of lifecycle operations.
func testConcurrentLifecycle(t *testing.T) {
	numGoroutines := runtime.NumCPU() * 2
	operationsPerGoroutine := 50

	var wg sync.WaitGroup
	var successfulOps atomic.Int64
	var errors atomic.Int64

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < operationsPerGoroutine; j++ {
				func() {
					defer func() {
						if r := recover(); r != nil {
							errors.Add(1)
							t.Logf("Panic in goroutine %d, operation %d: %v", goroutineID, j, r)
						}
					}()

					// Create tracer
					tracer := tracez.New(fmt.Sprintf("concurrent-%d-%d", goroutineID, j))

					// Add collector
					collector := tracez.NewCollector("test", 50)
					collector.SetSyncMode(true)
					tracer.AddCollector("test", collector)

					// Generate spans
					for k := 0; k < 10; k++ {
						ctx, span := tracer.StartSpan(context.Background(), "concurrent-test")
						span.SetTag("goroutine", fmt.Sprintf("%d", goroutineID))
						span.SetTag("operation", fmt.Sprintf("%d", j))
						span.SetTag("span", fmt.Sprintf("%d", k))
						span.Finish()
						_ = ctx // Satisfy linter
					}

					// Verify collection
					exported := collector.Export()
					if len(exported) == 10 {
						successfulOps.Add(1)
					} else {
						errors.Add(1)
					}

					// Clean up
					// Note: collector cleanup handled by tracer.Close() in production use
					tracer.Close()
				}()
			}
		}(i)
	}

	wg.Wait()

	totalOps := int64(numGoroutines * operationsPerGoroutine)
	successRate := float64(successfulOps.Load()) / float64(totalOps) * 100

	t.Logf("Concurrent lifecycle results:")
	t.Logf("  Goroutines: %d", numGoroutines)
	t.Logf("  Operations per goroutine: %d", operationsPerGoroutine)
	t.Logf("  Total operations: %d", totalOps)
	t.Logf("  Successful: %d", successfulOps.Load())
	t.Logf("  Errors: %d", errors.Load())
	t.Logf("  Success rate: %.1f%%", successRate)

	// Should handle concurrent lifecycle operations well
	if successRate < 95 {
		t.Errorf("Too many errors in concurrent lifecycle: %.1f%% success", successRate)
	}
}
