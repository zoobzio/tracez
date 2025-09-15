# Core Concepts

Understanding the fundamental building blocks of tracez tracing.

## What is Distributed Tracing?

Distributed tracing helps you understand how requests flow through your system. Instead of guessing where time is spent or where errors occur, you get precise visibility into:

- Which services a request touched
- How long each operation took
- Where errors occurred and why
- Which operations happened in parallel vs sequence

## The Core Components

### Spans
A **span** represents a single operation in your system:

```go
_, span := tracer.StartSpan(ctx, "database.query")
span.SetTag("query", "SELECT * FROM users WHERE id = ?")
span.SetTag("user.id", "123")
defer span.Finish()
// ... do database work ...
```

Every span has:
- **Name**: What operation this represents (`"database.query"`)
- **Duration**: How long it took from start to finish
- **Tags**: Key-value metadata you attach (`"user.id": "123"`)
- **Timestamps**: Precise start and end times
- **IDs**: Unique identifiers for correlation

### Traces
A **trace** is a collection of related spans that represent a single request:

```
Trace ID: abc123 (complete user request)
├─ http.request (50ms) - Root span
│  ├─ auth.verify (5ms) - Authentication check
│  ├─ db.user-lookup (15ms) - Database query
│  └─ notification.send (20ms) - Send email
```

All spans in a trace share the same **Trace ID**, making it easy to find all work done for a single request.

### Context Propagation
Go's `context.Context` automatically links spans together:

```go
// Parent span
ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")
defer parentSpan.Finish()

// Child span - automatically linked because we use parent's context
childCtx, childSpan := tracer.StartSpan(ctx, "child")
defer childSpan.Finish()

// Result: child.ParentID == parent.SpanID
```

**Critical Rule**: Always use the context returned by `StartSpan()` for child operations.

## Data Flow Through tracez

```
Application Code
       ↓
    Tracer (StartSpan/Finish)
       ↓
   ActiveSpan (SetTag/GetTag)
       ↓
    Span (immutable)
       ↓
    Collectors (buffering)
       ↓
    Export (batch)
       ↓
  External Systems
```

### Step-by-Step Flow
1. **Application** calls `tracer.StartSpan(ctx, operation)`
2. **Tracer** creates internal span with IDs and timestamps
3. **ActiveSpan** wrapper provides thread-safe tag operations
4. **Context** carries span information to child operations
5. **Finish()** converts ActiveSpan to immutable Span
6. **Collectors** receive finished spans via channels (non-blocking)
7. **Export()** returns buffered spans and clears buffer
8. **External systems** process exported spans

## Thread Safety

### All Operations Are Safe
```go
// Multiple goroutines can do this simultaneously:
go func() {
    _, span := tracer.StartSpan(ctx, "operation-a")
    span.SetTag("worker", "1")
    defer span.Finish()
}()

go func() {
    _, span := tracer.StartSpan(ctx, "operation-b") 
    span.SetTag("worker", "2")
    defer span.Finish()
}()
```

### Safety Guarantees
- **Tracer**: Multiple goroutines can call `StartSpan()` simultaneously
- **ActiveSpan**: `SetTag()` and `GetTag()` are mutex-protected
- **Collector**: Channel-based collection with no locks in hot path
- **Export**: Atomic operations prevent data races

## Context Storage and Retrieval

### How Spans Travel in Context
```go
type spanKey struct{}

// When you call StartSpan(), tracez stores the span in context:
func withSpan(ctx context.Context, span *internalSpan) context.Context {
    return context.WithValue(ctx, spanKey{}, span)
}

// When creating child spans, tracez retrieves the parent:
func spanFromContext(ctx context.Context) *internalSpan {
    if span, ok := ctx.Value(spanKey{}).(*internalSpan); ok {
        return span
    }
    return nil  // No parent span
}
```

This automatic linking is why you must use the context from `StartSpan()`:

```go
// ✓ Correct - spans will be linked
ctx, parent := tracer.StartSpan(context.Background(), "parent")
_, child := tracer.StartSpan(ctx, "child")  // Uses parent's context

// ✗ Wrong - spans will NOT be linked
ctx, parent := tracer.StartSpan(context.Background(), "parent")
_, child := tracer.StartSpan(context.Background(), "child")  // Fresh context!
```

## ID Generation and Inheritance

### Trace ID Lifecycle
1. **Root span**: Generates new random trace ID
2. **Child spans**: Inherit parent's trace ID
3. **Trace ID**: Remains constant throughout entire request
4. **Cross-service**: Same trace ID enables correlation across services

### Span ID Hierarchy
```go
// Root span
TraceID: abc123, SpanID: span001, ParentID: nil

// Child span
TraceID: abc123, SpanID: span002, ParentID: span001

// Grandchild span  
TraceID: abc123, SpanID: span003, ParentID: span002
```

## Memory Management

### Span Lifecycle
```
ActiveSpan (mutable)  →  Span (immutable)  →  Export  →  GC
    ↓                       ↓                   ↓
 Tags Map              Tags Map Copy      External System
```

1. **ActiveSpan**: Mutable during operation, allows tag modification
2. **Finish()**: Converts to immutable Span, copies tag map
3. **Collection**: Span sent to collectors via non-blocking channel
4. **Export**: Spans returned to application, collector buffer cleared
5. **GC**: Spans eligible for garbage collection after export

### Backpressure Protection
```go
collector := tracez.NewCollector("name", 1000)  // 1000 span buffer

// When buffer is full:
// - New spans are dropped (not blocked)
// - Application performance unaffected
// - Use collector.DroppedCount() to monitor
```

## Best Practices for Concepts

### Span Naming
```go
// ✓ Good - Describes the operation
"http.request"
"db.user-lookup" 
"payment.charge"

// ✗ Bad - Too generic or includes variables
"function"
"query-user-123"  // Don't put IDs in names
"important_operation"
```

### Tag Usage
```go
// ✓ Good - Structured metadata
span.SetTag("http.method", "POST")
span.SetTag("http.status_code", "200")
span.SetTag("user.id", userID)
span.SetTag("error", err.Error())

// ✗ Bad - Unstructured or redundant
span.SetTag("info", "This is a POST request that returned 200")
span.SetTag("span_name", "http.request")  // Redundant
```

### Context Discipline
```go
// ✓ Always use the context from StartSpan
ctx, span := tracer.StartSpan(parentCtx, "operation")
result := processData(ctx)  // Pass traced context
callService(ctx)            // Pass traced context

// ✓ Extract context in functions
func processData(ctx context.Context) Result {
    _, span := tracer.StartSpan(ctx, "data.process")  // Child span
    defer span.Finish()
    // ...
}
```

## Next Steps

Now that you understand the core concepts:

- **[Getting Started](getting-started.md)**: See these concepts in working code
- **[Examples Guide](examples-guide.md)**: Learn from realistic examples
- **[Instrumentation Guide](../guides/instrumentation.md)**: Apply concepts to your services