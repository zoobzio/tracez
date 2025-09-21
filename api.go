// Package tracez provides a minimal, primitive distributed tracing library.
//
// tracez focuses on span creation and processing without the complexity of
// full OpenTelemetry. It's designed for systems that need basic distributed
// tracing with predictable performance and zero memory overhead when unused.
//
// Core Components:
//   - Tracer: Manages span lifecycle and handlers.
//   - Span: Represents a single unit of work.
//   - ActiveSpan: Thread-safe wrapper for ongoing spans.
//   - SpanHandler: Callback function invoked when spans complete.
//
// Basic Usage:
//
//	tracer := tracez.New()
//	defer tracer.Close()
//
//	// Register a handler for completed spans.
//	tracer.OnSpanComplete(func(span Span) {
//	    log.Printf("%s: %v", span.Name, span.Duration)
//	})
//
//	// Start a new span.
//	ctx, span := tracer.StartSpan(ctx, "operation-name")
//	defer span.Finish()
//
//	// Add metadata.
//	span.SetTag("user.id", "123")
//
//	// Pass context to child operations.
//	childCtx, childSpan := tracer.StartSpan(ctx, "child-operation")
//	defer childSpan.Finish()
//
// Thread Safety:
//
// Tracer is safe for concurrent use by multiple goroutines.
// Handler registration/removal is thread-safe.
// ActiveSpan SetTag/GetTag operations are safe for concurrent use.
// Handlers receive immutable span copies, safe for any use.
//
// Context Propagation:
//
// Spans are automatically linked via context.Context. Child spans
// inherit their parent's TraceID and reference the parent's SpanID.
//
// Memory Management:
//
// Zero memory overhead when no handlers are registered.
// Handlers receive span copies by value to prevent data races.
// Optional worker pool for bounded async handler execution.
//
// Resource Cleanup:
//
// Call tracer.Close() to properly shut down worker pools and handlers.
package tracez

// Key represents a span operation name.
type Key = string

// Tag represents a span tag key.
type Tag = string
