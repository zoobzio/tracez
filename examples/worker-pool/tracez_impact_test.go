package main

import (
	"context"
	"testing"

	"github.com/zoobzio/tracez"
)

// Test actual tracez API performance impact

func BenchmarkTracezStartSpan_Current(b *testing.B) {
	tracer := tracez.New("benchmark")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		newCtx, span := tracer.StartSpan(ctx, "operation")
		span.Finish()
		_ = newCtx
	}
}

func BenchmarkTracezGetSpan_Current(b *testing.B) {
	tracer := tracez.New("benchmark")
	ctx := context.Background()
	ctx, span := tracer.StartSpan(ctx, "operation")
	defer span.Finish()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = tracez.GetSpan(ctx)
	}
}

func BenchmarkTracezNestedSpans_Current(b *testing.B) {
	tracer := tracez.New("benchmark")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()

		// Parent span
		ctx, parent := tracer.StartSpan(ctx, "parent")

		// Child span
		ctx, child := tracer.StartSpan(ctx, "child")

		// Grandchild span
		_, grandchild := tracer.StartSpan(ctx, "grandchild")

		grandchild.Finish()
		child.Finish()
		parent.Finish()
	}
}

func BenchmarkTracezMiddleware_Current(b *testing.B) {
	tracer := tracez.New("benchmark")

	// Simulate HTTP middleware chain
	handler := func(ctx context.Context) {
		ctx, span := tracer.StartSpan(ctx, "handler")
		defer span.Finish()

		// Do some work
		span.SetTag("status", "200")
	}

	middleware1 := func(ctx context.Context, next func(context.Context)) {
		ctx, span := tracer.StartSpan(ctx, "middleware1")
		defer span.Finish()

		span.SetTag("auth", "true")
		next(ctx)
	}

	middleware2 := func(ctx context.Context, next func(context.Context)) {
		ctx, span := tracer.StartSpan(ctx, "middleware2")
		defer span.Finish()

		span.SetTag("rate_limit", "ok")
		next(ctx)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		ctx, root := tracer.StartSpan(ctx, "request")

		middleware1(ctx, func(ctx context.Context) {
			middleware2(ctx, handler)
		})

		root.Finish()
	}
}

func BenchmarkTracezHighContention_Current(b *testing.B) {
	tracer := tracez.New("benchmark")
	ctx := context.Background()
	ctx, root := tracer.StartSpan(ctx, "root")
	defer root.Finish()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, span := tracer.StartSpan(ctx, "parallel")
			span.SetTag("goroutine", "true")
			span.Finish()
		}
	})
}

// Memory profile for typical usage
func BenchmarkTracezMemoryProfile_Current(b *testing.B) {
	tracer := tracez.New("benchmark")
	collector := tracez.NewCollector("memory", 1000)
	tracer.AddCollector("memory", collector)

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		ctx := context.Background()

		// Typical request flow
		ctx, request := tracer.StartSpan(ctx, "request")
		request.SetTag("method", "GET")
		request.SetTag("path", "/api/users")

		// Database query
		ctx, query := tracer.StartSpan(ctx, "db.query")
		query.SetTag("query", "SELECT * FROM users")
		query.Finish()

		// Cache check
		_, cache := tracer.StartSpan(ctx, "cache.get")
		cache.SetTag("key", "users:all")
		cache.SetTag("hit", "false")
		cache.Finish()

		request.Finish()
	}
}
