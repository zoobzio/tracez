package main

import (
	"context"
	"testing"
	"time"
)

// Testing both approaches: contextBundle vs separate keys

type contextKey string

const (
	bundleKey contextKey = "bundle"
	tracerKey contextKey = "tracer"
	spanKey   contextKey = "span"
)

// Simplified types for benchmarking
type Tracer struct {
	serviceName string
}

type Span struct {
	TraceID   string
	SpanID    string
	ParentID  string
	Name      string
	StartTime time.Time
}

type contextBundle struct {
	tracer *Tracer
	span   *Span
}

// Approach 1: Using contextBundle (current implementation)
func BenchmarkContextBundle_Store(b *testing.B) {
	ctx := context.Background()
	tracer := &Tracer{serviceName: "test"}
	span := &Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "operation",
		StartTime: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bundle := &contextBundle{tracer: tracer, span: span}
		_ = context.WithValue(ctx, bundleKey, bundle)
	}
}

func BenchmarkContextBundle_Retrieve(b *testing.B) {
	tracer := &Tracer{serviceName: "test"}
	span := &Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "operation",
		StartTime: time.Now(),
	}
	bundle := &contextBundle{tracer: tracer, span: span}
	ctx := context.WithValue(context.Background(), bundleKey, bundle)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if bundle, ok := ctx.Value(bundleKey).(*contextBundle); ok {
			_ = bundle.span
			_ = bundle.tracer
		}
	}
}

// Approach 2: Using separate context keys (proposed simplification)
func BenchmarkSeparateKeys_Store(b *testing.B) {
	ctx := context.Background()
	tracer := &Tracer{serviceName: "test"}
	span := &Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "operation",
		StartTime: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.WithValue(ctx, tracerKey, tracer)
		_ = context.WithValue(ctx, spanKey, span)
	}
}

func BenchmarkSeparateKeys_Retrieve(b *testing.B) {
	tracer := &Tracer{serviceName: "test"}
	span := &Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "operation",
		StartTime: time.Now(),
	}
	ctx := context.WithValue(context.Background(), tracerKey, tracer)
	ctx = context.WithValue(ctx, spanKey, span)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctx.Value(spanKey).(*Span)
		_ = ctx.Value(tracerKey).(*Tracer)
	}
}

// Real-world scenario: Nested spans (deep context chain)
func BenchmarkNestedSpans_Bundle(b *testing.B) {
	tracer := &Tracer{serviceName: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()

		// Create 10 nested spans
		for j := 0; j < 10; j++ {
			span := &Span{
				TraceID:   "trace-123",
				SpanID:    "span-" + string(rune(j)),
				Name:      "operation",
				StartTime: time.Now(),
			}
			bundle := &contextBundle{tracer: tracer, span: span}
			ctx = context.WithValue(ctx, bundleKey, bundle)
		}

		// Retrieve final span
		if bundle, ok := ctx.Value(bundleKey).(*contextBundle); ok {
			_ = bundle.span
		}
	}
}

func BenchmarkNestedSpans_Separate(b *testing.B) {
	tracer := &Tracer{serviceName: "test"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx := context.Background()
		ctx = context.WithValue(ctx, tracerKey, tracer)

		// Create 10 nested spans
		for j := 0; j < 10; j++ {
			span := &Span{
				TraceID:   "trace-123",
				SpanID:    "span-" + string(rune(j)),
				Name:      "operation",
				StartTime: time.Now(),
			}
			ctx = context.WithValue(ctx, spanKey, span)
		}

		// Retrieve final span
		_ = ctx.Value(spanKey).(*Span)
	}
}

// Memory allocation comparison
func BenchmarkAllocation_Bundle(b *testing.B) {
	tracer := &Tracer{serviceName: "test"}
	span := &Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "operation",
		StartTime: time.Now(),
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		bundle := &contextBundle{tracer: tracer, span: span}
		ctx = context.WithValue(ctx, bundleKey, bundle)

		// Simulate retrieval
		if bundle, ok := ctx.Value(bundleKey).(*contextBundle); ok {
			_ = bundle.span
		}
	}
}

func BenchmarkAllocation_Separate(b *testing.B) {
	tracer := &Tracer{serviceName: "test"}
	span := &Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "operation",
		StartTime: time.Now(),
	}
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ctx = context.WithValue(ctx, tracerKey, tracer)
		ctx = context.WithValue(ctx, spanKey, span)

		// Simulate retrieval
		_ = ctx.Value(spanKey).(*Span)
	}
}

// Concurrent access patterns
func BenchmarkConcurrentAccess_Bundle(b *testing.B) {
	tracer := &Tracer{serviceName: "test"}
	span := &Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "operation",
		StartTime: time.Now(),
	}
	bundle := &contextBundle{tracer: tracer, span: span}
	ctx := context.WithValue(context.Background(), bundleKey, bundle)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if bundle, ok := ctx.Value(bundleKey).(*contextBundle); ok {
				_ = bundle.span
			}
		}
	})
}

func BenchmarkConcurrentAccess_Separate(b *testing.B) {
	tracer := &Tracer{serviceName: "test"}
	span := &Span{
		TraceID:   "trace-123",
		SpanID:    "span-456",
		Name:      "operation",
		StartTime: time.Now(),
	}
	ctx := context.WithValue(context.Background(), tracerKey, tracer)
	ctx = context.WithValue(ctx, spanKey, span)

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = ctx.Value(spanKey).(*Span)
		}
	})
}
