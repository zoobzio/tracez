package benchmarks

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// BenchmarkTracerSpanCreation measures the core span creation performance.
// This is the fundamental operation that claims 240k+ spans/sec.
func BenchmarkTracerSpanCreation(b *testing.B) {
	tracer := tracez.New("benchmark-service")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, span := tracer.StartSpan(ctx, "benchmark-operation")
		span.Finish()
	}
}

// BenchmarkTracerSpanCreationParallel tests concurrent span creation.
// Real systems create spans from multiple goroutines.
func BenchmarkTracerSpanCreationParallel(b *testing.B) {
	tracer := tracez.New("benchmark-service")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "parallel-operation")
			span.Finish()
		}
	})
}

// BenchmarkTracerSpanWithTags measures performance impact of adding tags.
// Tags are common in real tracing scenarios.
func BenchmarkTracerSpanWithTags(b *testing.B) {
	tracer := tracez.New("benchmark-service")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, span := tracer.StartSpan(ctx, "tagged-operation")
		span.SetTag("user.id", "12345")
		span.SetTag("request.id", "req-67890")
		span.SetTag("service.version", "1.2.3")
		span.Finish()
	}
}

// BenchmarkTracerSpanWithTagsParallel tests concurrent tag operations.
// Tags must be thread-safe.
func BenchmarkTracerSpanWithTagsParallel(b *testing.B) {
	tracer := tracez.New("benchmark-service")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "parallel-tagged-operation")
			span.SetTag("user.id", "12345")
			span.SetTag("request.id", "req-67890")
			span.SetTag("service.version", "1.2.3")
			span.Finish()
		}
	})
}

// BenchmarkTracerNestedSpans measures hierarchical span performance.
// Nested spans are common in real applications.
func BenchmarkTracerNestedSpans(b *testing.B) {
	tracer := tracez.New("benchmark-service")
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		rootCtx, rootSpan := tracer.StartSpan(ctx, "root-operation")

		childCtx, childSpan := tracer.StartSpan(rootCtx, "child-operation")
		childSpan.SetTag("child.index", "1")

		_, grandchildSpan := tracer.StartSpan(childCtx, "grandchild-operation")
		grandchildSpan.SetTag("depth", "2")

		grandchildSpan.Finish()
		childSpan.Finish()
		rootSpan.Finish()
	}
}

// BenchmarkCollectorCollection measures collector performance without export.
func BenchmarkCollectorCollection(b *testing.B) {
	collector := tracez.NewCollector("benchmark-collector", 10000)
	defer collector.Reset()

	span := &tracez.Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "benchmark-span",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Millisecond),
		Duration:  time.Millisecond,
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		collector.Collect(span)
	}

	// Background processing completion not needed for benchmark
}

// BenchmarkCollectorCollectionParallel tests concurrent collection.
func BenchmarkCollectorCollectionParallel(b *testing.B) {
	collector := tracez.NewCollector("parallel-collector", 10000)
	defer collector.Reset()

	span := &tracez.Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "parallel-span",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Millisecond),
		Duration:  time.Millisecond,
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			collector.Collect(span)
		}
	})

	// Background processing completion not needed for benchmark
}

// BenchmarkCollectorExport measures export performance with various sizes.
func BenchmarkCollectorExport(b *testing.B) {
	sizes := []int{1, 10, 100, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size-%d", size), func(b *testing.B) {
			collector := tracez.NewCollector("export-collector", size*2)
			defer collector.Reset()

			// Pre-populate collector.
			for i := 0; i < size; i++ {
				span := &tracez.Span{
					TraceID:   fmt.Sprintf("trace-%d", i),
					SpanID:    fmt.Sprintf("span-%d", i),
					Name:      "export-span",
					StartTime: time.Now(),
					EndTime:   time.Now().Add(time.Millisecond),
					Duration:  time.Millisecond,
					Tags:      map[string]string{"index": fmt.Sprintf("%d", i)},
				}
				collector.Collect(span)
			}

			// Background processing wait removed for benchmark accuracy

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				spans := collector.Export()
				if len(spans) == 0 {
					// Re-populate for next iteration.
					for j := 0; j < size; j++ {
						span := &tracez.Span{
							TraceID:   fmt.Sprintf("trace-%d-%d", i, j),
							SpanID:    fmt.Sprintf("span-%d-%d", i, j),
							Name:      "export-span",
							StartTime: time.Now(),
							EndTime:   time.Now().Add(time.Millisecond),
							Duration:  time.Millisecond,
							Tags:      map[string]string{"iter": fmt.Sprintf("%d", i)},
						}
						collector.Collect(span)
					}
					// Re-population delay removed for benchmark accuracy
				}
			}
		})
	}
}

// BenchmarkMemoryUsageUnderLoad measures memory allocation patterns under sustained load.
func BenchmarkMemoryUsageUnderLoad(b *testing.B) {
	tracer := tracez.New("load-test")
	collector := tracez.NewCollector("load-collector", 10000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	ctx := context.Background()

	// Force GC and get baseline.
	runtime.GC()
	var startStats runtime.MemStats
	runtime.ReadMemStats(&startStats)

	b.ResetTimer()
	b.ReportAllocs()

	// Simulate realistic workload.
	for i := 0; i < b.N; i++ {
		// Create spans with realistic nesting and tags.
		rootCtx, rootSpan := tracer.StartSpan(ctx, "http-request")
		rootSpan.SetTag("http.method", "POST")
		rootSpan.SetTag("http.path", "/api/users")

		dbCtx, dbSpan := tracer.StartSpan(rootCtx, "db.query")
		dbSpan.SetTag("db.table", "users")
		dbSpan.SetTag("db.operation", "INSERT")

		_, cacheSpan := tracer.StartSpan(dbCtx, "cache.invalidate")
		cacheSpan.SetTag("cache.key", "users:*")
		cacheSpan.Finish()

		dbSpan.Finish()
		rootSpan.Finish()

		// Periodic export to prevent unbounded growth.
		if i%1000 == 0 {
			collector.Export()
		}
	}

	b.StopTimer()
	runtime.GC()
	var endStats runtime.MemStats
	runtime.ReadMemStats(&endStats)

	allocatedMB := float64(endStats.TotalAlloc-startStats.TotalAlloc) / 1024 / 1024
	b.ReportMetric(allocatedMB, "MB-allocated")
	b.ReportMetric(float64(endStats.NumGC-startStats.NumGC), "gc-cycles")
}

// BenchmarkBackpressureBehavior tests behavior when collector buffers fill.
func BenchmarkBackpressureBehavior(b *testing.B) {
	// Small buffer to trigger backpressure quickly.
	collector := tracez.NewCollector("backpressure-test", 10)
	defer collector.Reset()

	span := &tracez.Span{
		TraceID:   "overload-trace",
		SpanID:    "overload-span",
		Name:      "overload-operation",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Microsecond * 100),
		Duration:  time.Microsecond * 100,
	}

	b.ResetTimer()
	b.ReportAllocs()

	var dropped int64
	for i := 0; i < b.N; i++ {
		initialDropped := collector.DroppedCount()
		collector.Collect(span)
		if collector.DroppedCount() > initialDropped {
			dropped++
		}
	}

	// Report metrics.
	dropRate := float64(dropped) / float64(b.N) * 100
	b.ReportMetric(dropRate, "drop-rate-%")
	b.ReportMetric(float64(collector.DroppedCount()), "total-dropped")
}

// BenchmarkConcurrentCollectors tests performance with multiple collectors.
func BenchmarkConcurrentCollectors(b *testing.B) {
	tracer := tracez.New("multi-collector-test")
	defer tracer.Close()

	// Add multiple collectors as realistic systems might have.
	collectors := make([]*tracez.Collector, 3)
	for i := 0; i < 3; i++ {
		collectors[i] = tracez.NewCollector(fmt.Sprintf("collector-%d", i), 1000)
		tracer.AddCollector(fmt.Sprintf("collector-%d", i), collectors[i])
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "multi-collector-span")
			span.SetTag("collector.count", "3")
			span.Finish()
		}
	})
}

// BenchmarkRealWorldScenario simulates a realistic HTTP request trace.
func BenchmarkRealWorldScenario(b *testing.B) {
	tracer := tracez.New("web-service")
	collector := tracez.NewCollector("http-collector", 5000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// HTTP request span.
		reqCtx, reqSpan := tracer.StartSpan(ctx, "http.request")
		reqSpan.SetTag("http.method", "GET")
		reqSpan.SetTag("http.path", "/api/orders/12345")
		reqSpan.SetTag("user.id", "user-98765")

		// Auth middleware span.
		authCtx, authSpan := tracer.StartSpan(reqCtx, "auth.validate")
		authSpan.SetTag("auth.method", "jwt")
		// Auth simulation delay removed for benchmark accuracy
		authSpan.Finish()

		// Database query span.
		dbCtx, dbSpan := tracer.StartSpan(authCtx, "db.query")
		dbSpan.SetTag("db.table", "orders")
		dbSpan.SetTag("db.operation", "SELECT")
		dbSpan.SetTag("db.rows", "1")
		// DB simulation delay removed for benchmark accuracy
		dbSpan.Finish()

		// External API call.
		apiCtx, apiSpan := tracer.StartSpan(dbCtx, "external.payment_service")
		apiSpan.SetTag("api.endpoint", "GET /payments/12345")
		apiSpan.SetTag("api.timeout", "5s")
		time.Sleep(time.Microsecond * 10) // Simulate network time.
		apiSpan.Finish()

		// Cache operation.
		_, cacheSpan := tracer.StartSpan(apiCtx, "cache.set")
		cacheSpan.SetTag("cache.key", "order:12345")
		cacheSpan.SetTag("cache.ttl", "300")
		time.Sleep(time.Microsecond) // Simulate cache time.
		cacheSpan.Finish()

		reqSpan.Finish()

		// Periodic export to simulate real system behavior.
		if i%100 == 0 {
			collector.Export()
		}
	}
}
