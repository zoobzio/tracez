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

// BenchmarkSpanCreationRate measures raw span creation throughput.
// This validates the 240k+ spans/sec claim under ideal conditions.
func BenchmarkSpanCreationRate(b *testing.B) {
	tracer := tracez.New("rate-test")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	start := time.Now()

	for i := 0; i < b.N; i++ {
		_, span := tracer.StartSpan(ctx, "rate-span")
		span.Finish()
	}

	elapsed := time.Since(start)
	rate := float64(b.N) / elapsed.Seconds()
	b.ReportMetric(rate, "spans/sec")
}

// BenchmarkSpanCreationRateParallel measures parallel span creation throughput.
// Real systems create spans from multiple goroutines simultaneously  .
func BenchmarkSpanCreationRateParallel(b *testing.B) {
	tracer := tracez.New("parallel-rate-test")
	defer tracer.Close()

	ctx := context.Background()
	var counter int64

	b.ResetTimer()
	start := time.Now()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "parallel-rate-span")
			span.Finish()
			atomic.AddInt64(&counter, 1)
		}
	})

	elapsed := time.Since(start)
	rate := float64(counter) / elapsed.Seconds()
	b.ReportMetric(rate, "spans/sec")
}

// BenchmarkIDGeneration measures ID generation performance.
// ID generation can be a bottleneck in high-throughput systems.
func BenchmarkIDGeneration(b *testing.B) {
	tracer := tracez.New("id-test")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Creating a span triggers ID generation.
		_, span := tracer.StartSpan(ctx, "id-span")
		_ = span // Prevent optimization.
	}
}

// BenchmarkIDGenerationParallel tests concurrent ID generation.
// Crypto/rand must handle concurrent access safely.
func BenchmarkIDGenerationParallel(b *testing.B) {
	tracer := tracez.New("parallel-id-test")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "parallel-id-span")
			_ = span // Prevent optimization.
		}
	})
}

// BenchmarkContextPropagation measures context operations cost.
// Context propagation is critical path for span hierarchies.
func BenchmarkContextPropagation(b *testing.B) {
	tracer := tracez.New("context-test")
	defer tracer.Close()

	ctx := context.Background()
	parentCtx, parentSpan := tracer.StartSpan(ctx, "parent")
	defer parentSpan.Finish()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, childSpan := tracer.StartSpan(parentCtx, "child")
		childSpan.Finish()
	}
}

// BenchmarkTagOperations measures tag performance across sizes.
func BenchmarkTagOperations(b *testing.B) {
	tagCounts := []int{1, 5, 10, 20}

	for _, count := range tagCounts {
		b.Run(fmt.Sprintf("tags-%d", count), func(b *testing.B) {
			tracer := tracez.New("tag-test")
			defer tracer.Close()

			ctx := context.Background()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				_, span := tracer.StartSpan(ctx, "tagged-span")

				for j := 0; j < count; j++ {
					span.SetTag(fmt.Sprintf("key_%d", j), fmt.Sprintf("value_%d", j))
				}

				span.Finish()
			}
		})
	}
}

// BenchmarkTagOperationsParallel measures concurrent tag safety overhead.
func BenchmarkTagOperationsParallel(b *testing.B) {
	tracer := tracez.New("parallel-tag-test")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "parallel-tagged-span")

			// Multiple tag operations per span (realistic usage).
			span.SetTag("service.name", "api-gateway")
			span.SetTag("user.id", "12345")
			span.SetTag("request.id", "req-67890")
			span.SetTag("operation.type", "read")
			span.SetTag("status", "success")

			span.Finish()
		}
	})
}

// BenchmarkSpanHierarchy measures nested span creation cost.
func BenchmarkSpanHierarchy(b *testing.B) {
	depths := []int{1, 3, 5, 10}

	for _, depth := range depths {
		b.Run(fmt.Sprintf("depth-%d", depth), func(b *testing.B) {
			tracer := tracez.New("hierarchy-test")
			defer tracer.Close()

			ctx := context.Background()

			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				spans := make([]*tracez.ActiveSpan, depth)
				contexts := make([]context.Context, depth)
				contexts[0] = ctx

				// Create hierarchy.
				for j := 0; j < depth; j++ {
					var span *tracez.ActiveSpan
					contexts[j], span = tracer.StartSpan(contexts[j], fmt.Sprintf("level-%d", j))
					spans[j] = span
					if j < depth-1 {
						contexts[j+1] = contexts[j]
					}
				}

				// Finish in reverse order.
				for j := depth - 1; j >= 0; j-- {
					spans[j].Finish()
				}
			}
		})
	}
}

// BenchmarkCollectorThroughput measures collector processing speed.
func BenchmarkCollectorThroughput(b *testing.B) {
	bufferSizes := []int{100, 1000, 10000}

	for _, bufSize := range bufferSizes {
		b.Run(fmt.Sprintf("buffer-%d", bufSize), func(b *testing.B) {
			collector := tracez.NewCollector("throughput-test", bufSize)
			defer collector.Reset()

			span := &tracez.Span{
				TraceID:   "throughput-trace",
				SpanID:    "throughput-span",
				Name:      "throughput-operation",
				StartTime: time.Now(),
				EndTime:   time.Now().Add(time.Microsecond),
				Duration:  time.Microsecond,
			}

			b.ResetTimer()
			start := time.Now()

			for i := 0; i < b.N; i++ {
				collector.Collect(span)
			}

			// Wait for background processing.
			time.Sleep(100 * time.Millisecond)
			elapsed := time.Since(start)

			rate := float64(b.N) / elapsed.Seconds()
			b.ReportMetric(rate, "spans/sec")
		})
	}
}

// BenchmarkFullPipelineThroughput measures end-to-end performance.
// From span creation through collection - this is the real-world metric.
func BenchmarkFullPipelineThroughput(b *testing.B) {
	tracer := tracez.New("pipeline-test")
	collector := tracez.NewCollector("pipeline-collector", 10000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	ctx := context.Background()
	var processed int64

	b.ResetTimer()
	start := time.Now()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "pipeline-span")
			span.SetTag("benchmark", "pipeline")
			span.Finish()
			atomic.AddInt64(&processed, 1)
		}
	})

	// Wait for collection processing.
	time.Sleep(200 * time.Millisecond)
	elapsed := time.Since(start)

	rate := float64(processed) / elapsed.Seconds()
	b.ReportMetric(rate, "spans/sec")
	b.ReportMetric(float64(collector.DroppedCount()), "dropped")
}

// BenchmarkCPUContention measures performance under CPU stress.
func BenchmarkCPUContention(b *testing.B) {
	tracer := tracez.New("cpu-contention-test")
	defer tracer.Close()

	ctx := context.Background()

	// Start CPU-intensive background work.
	stop := make(chan struct{})
	var wg sync.WaitGroup

	numCPUWorkers := runtime.GOMAXPROCS(0)
	for i := 0; i < numCPUWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					// CPU-intensive work.
					sum := 0
					for j := 0; j < 1000; j++ {
						sum += j * j
					}
					runtime.Gosched()
				}
			}
		}()
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, span := tracer.StartSpan(ctx, "cpu-contention-span")
		span.SetTag("cpu.contention", "high")
		span.Finish()
	}

	close(stop)
	wg.Wait()
}

// BenchmarkMemoryPressure measures performance under memory pressure.
func BenchmarkMemoryPressure(b *testing.B) {
	tracer := tracez.New("memory-pressure-test")
	defer tracer.Close()

	ctx := context.Background()

	// Create memory pressure (allocate significant portion of available memory).
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)

	// Allocate ~50% of available memory as ballast.
	// Check for overflow before conversion.
	maxInt := uint64(^uint(0) >> 1)
	if memStats.Sys > maxInt {
		b.Skip("Memory allocation too large for int conversion")
	}
	ballastSize := int(memStats.Sys / 2) //nolint:gosec // Overflow checked above
	ballast := make([]byte, ballastSize)
	defer func() { ballast = nil }() // Ensure cleanup.

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, span := tracer.StartSpan(ctx, "memory-pressure-span")
		span.SetTag("memory.pressure", "high")
		span.Finish()

		// Prevent ballast optimization.
		if i%1000 == 0 {
			ballast[0] = byte(i % 255)
		}
	}
}

// BenchmarkGoroutineScaling measures performance with increasing goroutines.
func BenchmarkGoroutineScaling(b *testing.B) {
	goroutineCounts := []int{1, 10, 100, 1000}

	for _, count := range goroutineCounts {
		b.Run(fmt.Sprintf("goroutines-%d", count), func(b *testing.B) {
			tracer := tracez.New("scaling-test")
			defer tracer.Close()

			ctx := context.Background()

			spansPerGoroutine := b.N / count
			if spansPerGoroutine == 0 {
				spansPerGoroutine = 1
			}

			var wg sync.WaitGroup
			var totalProcessed int64

			b.ResetTimer()
			start := time.Now()

			for i := 0; i < count; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for j := 0; j < spansPerGoroutine; j++ {
						_, span := tracer.StartSpan(ctx, fmt.Sprintf("scaling-span-%d", id))
						span.SetTag("goroutine.id", fmt.Sprintf("%d", id))
						span.Finish()
						atomic.AddInt64(&totalProcessed, 1)
					}
				}(i)
			}

			wg.Wait()
			elapsed := time.Since(start)

			rate := float64(totalProcessed) / elapsed.Seconds()
			b.ReportMetric(rate, "spans/sec")
			b.ReportMetric(float64(count), "goroutines")
		})
	}
}

// BenchmarkLockContention measures mutex overhead in ActiveSpan.
func BenchmarkLockContention(b *testing.B) {
	tracer := tracez.New("lock-test")
	defer tracer.Close()

	ctx := context.Background()
	_, span := tracer.StartSpan(ctx, "contended-span")
	defer span.Finish()

	b.ResetTimer()

	// Heavy contention on the same span's mutex.
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			span.SetTag("contention", "test")
			value, _ := span.GetTag("contention")
			_ = value // Prevent optimization.
		}
	})
}
