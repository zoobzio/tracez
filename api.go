// Package tracez provides a minimal, primitive distributed tracing library.
//
// tracez focuses on span collection and export without the complexity of.
// full OpenTelemetry. It's designed for systems that need basic distributed
// tracing with predictable performance and resource usage.
//
// Core Components:.
//   - Tracer: Manages span lifecycle and collection.
//   - Span: Represents a single unit of work.
//   - ActiveSpan: Thread-safe wrapper for ongoing spans.
//   - Collector: Buffers completed spans for export.
//
// Basic Usage:.
//
//	tracer := tracez.New()
//	defer tracer.Close()
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
// Thread Safety:.
//
// Tracer is safe for concurrent use by multiple goroutines.
// Collectors are safe for concurrent span buffering.
// ActiveSpan SetTag/GetTag operations are safe for concurrent use.
//
// Spans themselves are NOT thread-safe - do not modify the same.
// Span struct from multiple goroutines simultaneously.
//
// Context Propagation:.
//
// Spans are automatically linked via context.Context. Child spans
// inherit their parent's TraceID and reference the parent's SpanID.
//
// Memory Management:.
//
// Collectors automatically manage memory by shrinking buffers after.
// export operations. Under high load, spans may be dropped to prevent
// memory exhaustion - use Collector.DroppedCount() to monitor.
//
// Resource Cleanup:.
//
// Call tracer.Close() to properly shut down all background goroutines.
// Call tracer.Reset() to clear all collectors and spans.
package tracez

// Key represents a span operation name.
type Key = string

// Tag represents a span tag key.
type Tag = string
