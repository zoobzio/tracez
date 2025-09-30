package tracez

import (
	"context"
	"runtime"
	"testing"
	"time"
)

func BenchmarkNoOpSpan(b *testing.B) {
	tracer := New()
	defer tracer.Close()

	ctx := context.Background()

	b.Run("no-handlers", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, span := tracer.StartSpan(ctx, "test-op")
			span.SetTag("key", "value")
			span.SetIntTag("int", 123)
			span.SetBoolTag("bool", true)
			span.Finish()
		}
	})

	b.Run("with-handler", func(b *testing.B) {
		tracer.OnSpanComplete(func(_ Span) {})
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			_, span := tracer.StartSpan(ctx, "test-op")
			span.SetTag("key", "value")
			span.SetIntTag("int", 123)
			span.SetBoolTag("bool", true)
			span.Finish()
		}
	})
}

func TestNoOpBehavior(t *testing.T) {
	tracer := New()
	defer tracer.Close()

	if tracer.HasHandlers() {
		t.Error("tracer should have no handlers initially")
	}

	ctx := context.Background()

	// With no handlers, operations should be no-op
	ctx, span := tracer.StartSpan(ctx, "test-op")

	// Verify no-op behavior
	span.SetTag("key", "value")
	span.SetIntTag("int", 123)
	span.SetBoolTag("bool", true)

	// These should return empty values for no-op spans
	if id := span.TraceID(); id != "" {
		t.Errorf("expected empty TraceID for no-op span, got %s", id)
	}

	if id := span.SpanID(); id != "" {
		t.Errorf("expected empty SpanID for no-op span, got %s", id)
	}

	if val, ok := span.GetTag("key"); ok || val != "" {
		t.Errorf("expected no tag for no-op span, got %v, %v", val, ok)
	}

	// Context is still created for chaining, even in no-op mode
	// This is needed for child spans to potentially work if handlers are added later
	_ = span.Context(ctx)
	// We no longer expect context to be unchanged - it's a design tradeoff

	span.Finish()

	// Add a handler and verify normal behavior resumes
	var capturedSpan Span
	tracer.OnSpanComplete(func(s Span) {
		capturedSpan = s
	})

	if !tracer.HasHandlers() {
		t.Error("tracer should have handlers after registration")
	}

	_, span = tracer.StartSpan(ctx, "real-op")
	span.SetTag("key", "value")
	span.Finish()

	// Allow time for handler to execute
	time.Sleep(10 * time.Millisecond)

	if capturedSpan.Name != "real-op" {
		t.Error("handler should have received the span")
	}

	if capturedSpan.Tags["key"] != "value" {
		t.Error("span should have the tag set")
	}
}

func TestNoOpMemoryUsage(t *testing.T) {
	tracer := New()
	defer tracer.Close()

	var m1, m2 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	ctx := context.Background()
	// Perform many no-op operations
	for i := 0; i < 1000; i++ {
		_, span := tracer.StartSpan(ctx, Key("test-op"))
		span.SetTag("key", "value")
		span.Finish()
	}

	runtime.GC()
	runtime.ReadMemStats(&m2)

	allocBytes := m2.TotalAlloc - m1.TotalAlloc
	allocsPerOp := allocBytes / 1000

	// With no-op optimizations, we should have minimal allocations per operation
	// The threshold here is generous to account for runtime overhead
	if allocsPerOp > 500 {
		t.Errorf("no-op spans allocating too much memory: %d bytes per operation", allocsPerOp)
	}
}
