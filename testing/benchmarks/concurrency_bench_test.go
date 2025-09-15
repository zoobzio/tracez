package benchmarks

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

// BenchmarkConcurrentSpanCreation tests thread safety under heavy concurrent load.
func BenchmarkConcurrentSpanCreation(b *testing.B) {
	concurrencyLevels := []int{1, 10, 50, 100, 500}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("concurrent-%d", concurrency), func(b *testing.B) {
			tracer := tracez.New("concurrent-test")
			defer tracer.Close()

			ctx := context.Background()
			spansPerWorker := b.N / concurrency
			if spansPerWorker == 0 {
				spansPerWorker = 1
			}

			var wg sync.WaitGroup
			var totalSpans int64

			b.ResetTimer()

			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for j := 0; j < spansPerWorker; j++ {
						_, span := tracer.StartSpan(ctx, fmt.Sprintf("worker-%d-span", workerID))
						span.SetTag("worker.id", fmt.Sprintf("%d", workerID))
						span.Finish()
						atomic.AddInt64(&totalSpans, 1)
					}
				}(i)
			}

			wg.Wait()
			b.ReportMetric(float64(totalSpans), "total-spans")
		})
	}
}

// BenchmarkCollectorConcurrency tests collector performance under concurrent load.
func BenchmarkCollectorConcurrency(b *testing.B) {
	concurrencyLevels := []int{1, 10, 50, 100}
	bufferSizes := []int{100, 1000}

	for _, concurrency := range concurrencyLevels {
		for _, bufSize := range bufferSizes {
			b.Run(fmt.Sprintf("workers-%d-buffer-%d", concurrency, bufSize), func(b *testing.B) {
				collector := tracez.NewCollector("concurrent-collector", bufSize)
				defer collector.Reset()

				spansPerWorker := b.N / concurrency
				if spansPerWorker == 0 {
					spansPerWorker = 1
				}

				var wg sync.WaitGroup

				b.ResetTimer()

				for i := 0; i < concurrency; i++ {
					wg.Add(1)
					go func(workerID int) {
						defer wg.Done()
						for j := 0; j < spansPerWorker; j++ {
							span := &tracez.Span{
								TraceID:   fmt.Sprintf("trace-%d-%d", workerID, j),
								SpanID:    fmt.Sprintf("span-%d-%d", workerID, j),
								Name:      "concurrent-operation",
								StartTime: time.Now(),
								EndTime:   time.Now().Add(time.Microsecond),
								Duration:  time.Microsecond,
								Tags: map[string]string{
									"worker.id": fmt.Sprintf("%d", workerID),
								},
							}
							collector.Collect(span)
						}
					}(i)
				}

				wg.Wait()

				// Wait for background processing.
				time.Sleep(100 * time.Millisecond)

				collected := collector.Count()
				dropped := collector.DroppedCount()
				b.ReportMetric(float64(collected), "collected")
				b.ReportMetric(float64(dropped), "dropped")
			})
		}
	}
}

// BenchmarkMultipleCollectorsConcurrency tests tracer with multiple collectors.
func BenchmarkMultipleCollectorsConcurrency(b *testing.B) {
	collectorCounts := []int{1, 3, 5, 10}

	for _, count := range collectorCounts {
		b.Run(fmt.Sprintf("collectors-%d", count), func(b *testing.B) {
			tracer := tracez.New("multi-collector-test")
			defer tracer.Close()

			// Create multiple collectors.
			collectors := make([]*tracez.Collector, count)
			for i := 0; i < count; i++ {
				collectors[i] = tracez.NewCollector(fmt.Sprintf("collector-%d", i), 1000)
				tracer.AddCollector(fmt.Sprintf("collector-%d", i), collectors[i])
			}

			ctx := context.Background()
			var processed int64

			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_, span := tracer.StartSpan(ctx, "multi-collector-span")
					span.SetTag("collector.count", fmt.Sprintf("%d", count))
					span.Finish()
					atomic.AddInt64(&processed, 1)
				}
			})

			// Wait for processing across all collectors.
			time.Sleep(200 * time.Millisecond)

			var totalCollected, totalDropped int64
			for _, collector := range collectors {
				totalCollected += int64(collector.Count())
				totalDropped += collector.DroppedCount()
			}

			b.ReportMetric(float64(totalCollected), "total-collected")
			b.ReportMetric(float64(totalDropped), "total-dropped")
			b.ReportMetric(float64(processed), "spans-created")
		})
	}
}

// BenchmarkSpanTagConcurrency tests concurrent tag operations on same span.
func BenchmarkSpanTagConcurrency(b *testing.B) {
	tracer := tracez.New("tag-concurrency-test")
	defer tracer.Close()

	ctx := context.Background()
	_, span := tracer.StartSpan(ctx, "contested-span")
	defer span.Finish()

	b.ResetTimer()

	// Multiple goroutines hammering the same span with tag operations.
	b.RunParallel(func(pb *testing.PB) {
		workerID := 0
		for pb.Next() {
			workerID++

			// Mix of SetTag and GetTag operations.
			span.SetTag(fmt.Sprintf("key-%d", workerID%10), fmt.Sprintf("value-%d", workerID))

			if workerID%2 == 0 {
				_, exists := span.GetTag(fmt.Sprintf("key-%d", (workerID-1)%10))
				_ = exists // Prevent optimization.
			}
		}
	})
}

// BenchmarkHierarchicalConcurrency tests concurrent hierarchical span creation.
func BenchmarkHierarchicalConcurrency(b *testing.B) {
	tracer := tracez.New("hierarchy-concurrency-test")
	defer tracer.Close()

	ctx := context.Background()
	rootCtx, rootSpan := tracer.StartSpan(ctx, "root-span")
	defer rootSpan.Finish()

	b.ResetTimer()

	// Multiple goroutines creating child spans from same parent.
	b.RunParallel(func(pb *testing.PB) {
		workerID := 0
		for pb.Next() {
			workerID++
			childCtx, childSpan := tracer.StartSpan(rootCtx, fmt.Sprintf("child-%d", workerID))
			childSpan.SetTag("parent.shared", "true")

			// Create grandchild spans.
			_, grandchildSpan := tracer.StartSpan(childCtx, fmt.Sprintf("grandchild-%d", workerID))
			grandchildSpan.SetTag("depth", "2")
			grandchildSpan.Finish()

			childSpan.Finish()
		}
	})
}

// BenchmarkCollectorExportConcurrency tests concurrent export operations.
func BenchmarkCollectorExportConcurrency(b *testing.B) {
	collector := tracez.NewCollector("export-test", 10000)
	defer collector.Reset()

	// Pre-populate collector.
	for i := 0; i < 5000; i++ {
		span := &tracez.Span{
			TraceID:   fmt.Sprintf("export-trace-%d", i),
			SpanID:    fmt.Sprintf("export-span-%d", i),
			Name:      "export-operation",
			StartTime: time.Now(),
			EndTime:   time.Now().Add(time.Microsecond),
			Duration:  time.Microsecond,
		}
		collector.Collect(span)
	}

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	var totalExported int64

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			spans := collector.Export()
			atomic.AddInt64(&totalExported, int64(len(spans)))
		}
	})

	b.ReportMetric(float64(totalExported), "total-exported")
}

// BenchmarkContextConcurrency tests concurrent context propagation.
func BenchmarkContextConcurrency(b *testing.B) {
	tracer := tracez.New("context-concurrency-test")
	defer tracer.Close()

	// Create multiple root contexts.
	numRoots := 10
	roots := make([]context.Context, numRoots)
	rootSpans := make([]*tracez.ActiveSpan, numRoots)

	for i := 0; i < numRoots; i++ {
		roots[i], rootSpans[i] = tracer.StartSpan(context.Background(), fmt.Sprintf("root-%d", i))
	}
	defer func() {
		for _, span := range rootSpans {
			span.Finish()
		}
	}()

	b.ResetTimer()

	// Concurrent span creation from different root contexts.
	b.RunParallel(func(pb *testing.PB) {
		workerID := 0
		for pb.Next() {
			rootCtx := roots[workerID%numRoots]
			_, childSpan := tracer.StartSpan(rootCtx, fmt.Sprintf("concurrent-child-%d", workerID))
			childSpan.SetTag("root.id", fmt.Sprintf("%d", workerID%numRoots))
			childSpan.Finish()
			workerID++
		}
	})
}

// BenchmarkRaceConditionStress heavily stresses race detection.
// This benchmark is specifically designed to catch race conditions.
func BenchmarkRaceConditionStress(b *testing.B) {
	tracer := tracez.New("race-stress-test")
	collector := tracez.NewCollector("race-collector", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	ctx := context.Background()

	// Shared span for maximum contention.
	sharedCtx, sharedSpan := tracer.StartSpan(ctx, "shared-span")
	defer sharedSpan.Finish()

	var operations int64

	b.ResetTimer()

	// Heavy concurrent operations on shared resources.
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			operation := atomic.AddInt64(&operations, 1)

			switch operation % 6 {
			case 0:
				// Create new span.
				_, span := tracer.StartSpan(ctx, "race-span")
				span.Finish()

			case 1:
				// Modify shared span tags.
				sharedSpan.SetTag("race", fmt.Sprintf("%d", operation))

			case 2:
				// Read shared span tags.
				_, exists := sharedSpan.GetTag("race")
				_ = exists

			case 3:
				// Create child of shared span.
				_, child := tracer.StartSpan(sharedCtx, "race-child")
				child.Finish()

			case 4:
				// Collector operations.
				span := &tracez.Span{
					TraceID: fmt.Sprintf("race-%d", operation),
					SpanID:  fmt.Sprintf("span-%d", operation),
					Name:    "race-operation",
				}
				collector.Collect(span)

			case 5:
				// Export (may conflict with collect).
				exported := collector.Export()
				_ = len(exported)
			}
		}
	})
}

// BenchmarkBackpressureConcurrency tests backpressure under concurrent load.
func BenchmarkBackpressureConcurrency(b *testing.B) {
	// Intentionally small buffer to trigger backpressure.
	collector := tracez.NewCollector("backpressure-test", 10)
	defer collector.Reset()

	concurrencyLevels := []int{10, 50, 100}

	for _, concurrency := range concurrencyLevels {
		b.Run(fmt.Sprintf("workers-%d", concurrency), func(b *testing.B) {
			spansPerWorker := b.N / concurrency
			if spansPerWorker == 0 {
				spansPerWorker = 1
			}

			var wg sync.WaitGroup
			var attempted int64

			b.ResetTimer()

			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func(workerID int) {
					defer wg.Done()
					for j := 0; j < spansPerWorker; j++ {
						span := &tracez.Span{
							TraceID: fmt.Sprintf("backpressure-%d-%d", workerID, j),
							SpanID:  fmt.Sprintf("span-%d-%d", workerID, j),
							Name:    "backpressure-operation",
						}
						collector.Collect(span)
						atomic.AddInt64(&attempted, 1)
					}
				}(i)
			}

			wg.Wait()
			time.Sleep(50 * time.Millisecond) // Let processing complete.

			dropped := collector.DroppedCount()
			collected := collector.Count()

			dropRate := float64(dropped) / float64(attempted) * 100
			b.ReportMetric(dropRate, "drop-rate-%")
			b.ReportMetric(float64(collected), "collected")
			b.ReportMetric(float64(attempted), "attempted")
		})
	}
}

// BenchmarkGoroutineLeakDetection tests proper resource cleanup.
func BenchmarkGoroutineLeakDetection(b *testing.B) {
	initialGoroutines := runtime.NumGoroutine()

	for i := 0; i < b.N; i++ {
		// Create and immediately close tracer/collector.
		tracer := tracez.New("leak-test")
		collector := tracez.NewCollector("leak-collector", 100)
		tracer.AddCollector("collector", collector)

		// Do some work.
		ctx := context.Background()
		_, span := tracer.StartSpan(ctx, "leak-span")
		span.SetTag("iteration", fmt.Sprintf("%d", i))
		span.Finish()

		// Clean shutdown.
		tracer.Close()

		// Periodic goroutine check.
		if i%100 == 0 {
			runtime.GC()
			time.Sleep(10 * time.Millisecond)
			currentGoroutines := runtime.NumGoroutine()

			if currentGoroutines > initialGoroutines+5 { // Allow some variance.
				b.Fatalf("Potential goroutine leak detected: started with %d, now have %d at iteration %d",
					initialGoroutines, currentGoroutines, i)
			}
		}
	}

	// Final check.
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	finalGoroutines := runtime.NumGoroutine()

	b.ReportMetric(float64(initialGoroutines), "initial-goroutines")
	b.ReportMetric(float64(finalGoroutines), "final-goroutines")
}

// BenchmarkChannelContention tests channel performance under load.
func BenchmarkChannelContention(b *testing.B) {
	channelSizes := []int{1, 10, 100, 1000}

	for _, size := range channelSizes {
		b.Run(fmt.Sprintf("channel-size-%d", size), func(b *testing.B) {
			collector := tracez.NewCollector("channel-test", size)
			defer collector.Reset()

			b.ResetTimer()

			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					span := &tracez.Span{
						TraceID: "channel-trace",
						SpanID:  "channel-span",
						Name:    "channel-operation",
					}
					collector.Collect(span)
				}
			})

			time.Sleep(100 * time.Millisecond)

			b.ReportMetric(float64(collector.Count()), "collected")
			b.ReportMetric(float64(collector.DroppedCount()), "dropped")
		})
	}
}
