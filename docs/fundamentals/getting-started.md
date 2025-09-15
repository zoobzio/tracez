# Get Your First Trace in 5 Minutes

tracez is a minimal distributed tracing library that gets out of your way. Perfect when OpenTelemetry feels like overkill, but you still need to understand what's happening across your services.

## Installation

```bash
go get github.com/zoobzio/tracez
```

Requires Go 1.21+ and has zero external dependencies.

## Basic Usage - 30 Lines

Let's create your first trace to see how spans work:

```go
package main

import (
    "context"
    "fmt"
    "time"
    
    "github.com/zoobzio/tracez"
)

func main() {
    // Create tracer for your service
    tracer := tracez.New("my-service")
    defer tracer.Close()
    
    // Add collector to buffer spans
    collector := tracez.NewCollector("console", 100)
    tracer.AddCollector("console", collector)
    
    // Create a root span
    ctx, span := tracer.StartSpan(context.Background(), "process-order")
    span.SetTag("order.id", "123")
    span.SetTag("user.id", "alice")
    
    // Child span automatically inherits trace context
    processPayment(ctx, tracer)
    
    // Finish root span explicitly before export
    span.Finish()
    
    // Brief pause to ensure spans are collected
    time.Sleep(10 * time.Millisecond)
    
    // Export completed spans to see what happened
    spans := collector.Export()
    fmt.Printf("Collected %d spans:\n", len(spans))
    for _, s := range spans {
        fmt.Printf("  %s (%v) tags=%v\n", s.Name, s.Duration, s.Tags)
    }
}

func processPayment(ctx context.Context, tracer *tracez.Tracer) {
    // This span will be a child of "process-order"
    _, span := tracer.StartSpan(ctx, "payment.charge")
    span.SetTag("amount", "99.99")
    defer span.Finish()
    
    // Simulate payment processing
    time.Sleep(10 * time.Millisecond)
}
```

Save this as `main.go` and run:

```bash
go mod init trace-demo
go get github.com/zoobzio/tracez
go run main.go
```

## What You'll See

```
Collected 2 spans:
  process-order (12.345ms) tags=map[order.id:123 user.id:alice]
  payment.charge (10.123ms) tags=map[amount:99.99]
```

Each span shows:
- **Name**: What operation this represents
- **Duration**: How long it took from start to finish
- **Tags**: Key-value metadata you attached

## What Just Happened

1. **Tracer Created**: `tracez.New("my-service")` creates a thread-safe tracer for your service
2. **Collector Added**: Buffers completed spans until you export them
3. **Root Span**: `StartSpan()` with no parent creates a new trace
4. **Child Span**: Calling `StartSpan()` with the parent context automatically links spans
5. **Context Propagation**: The child span knows it belongs to the same trace
6. **Export**: `collector.Export()` returns all collected spans and clears the buffer

## Key Concepts

### Context is Everything
```go
// Parent span
ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")
defer parentSpan.Finish()

// Child span - MUST use the context from parent
childCtx, childSpan := tracer.StartSpan(ctx, "child")  // ctx from parent!
defer childSpan.Finish()
```

**Critical**: Always use the context returned by `StartSpan()` for child operations. This is how tracez knows to link spans together.

### Thread Safety
All operations are thread-safe:
- Multiple goroutines can call `tracer.StartSpan()` concurrently
- `span.SetTag()` and `span.GetTag()` are safe for concurrent use
- `collector.Export()` can be called while spans are being created

### Backpressure Protection
Collectors have fixed buffer sizes. When full:
- New spans are dropped (not blocked)
- Your application stays fast
- Use `collector.DroppedCount()` to monitor drops

## Real-World Example

Here's a more realistic HTTP handler pattern:

```go
func handleOrder(w http.ResponseWriter, r *http.Request) {
    // Extract tracer from your middleware (see http-middleware example)
    tracer := tracez.GetTracer(r.Context())
    if tracer == nil {
        // No tracing configured, handle normally
        http.Error(w, "Service unavailable", 500)
        return
    }
    
    ctx, span := tracer.StartSpan(r.Context(), "handler.create-order")
    span.SetTag("http.method", r.Method)
    span.SetTag("http.path", r.URL.Path)
    defer span.Finish()
    
    // Database operation with its own span
    orderID, err := createOrderInDB(ctx, tracer)
    if err != nil {
        span.SetTag("error", err.Error())
        http.Error(w, "Database error", 500)
        return
    }
    span.SetTag("order.created", orderID)
    
    // Send notification (another service call)
    notifyUser(ctx, tracer, orderID)
    
    w.WriteHeader(201)
    fmt.Fprintf(w, `{"order_id": "%s"}`, orderID)
}

func createOrderInDB(ctx context.Context, tracer *tracez.Tracer) (string, error) {
    _, span := tracer.StartSpan(ctx, "db.create-order")
    span.SetTag("db.table", "orders")
    defer span.Finish()
    
    // Simulate database work
    time.Sleep(5 * time.Millisecond)
    return "order-123", nil
}

func notifyUser(ctx context.Context, tracer *tracez.Tracer, orderID string) {
    _, span := tracer.StartSpan(ctx, "notification.send")
    span.SetTag("notification.type", "order_created") 
    span.SetTag("order.id", orderID)
    defer span.Finish()
    
    // Simulate notification service call
    time.Sleep(20 * time.Millisecond)
}
```

This creates a trace tree:
```
handler.create-order (30ms)
├─ db.create-order (5ms)
└─ notification.send (20ms)
```

## Next Steps

### Learn More Concepts
- **[Core Concepts](concepts.md)**: Deep dive into spans, traces, and context
- **[Examples Guide](examples-guide.md)**: How to run and understand the working examples

### Start Instrumenting
- **[Instrumentation Guide](../guides/instrumentation.md)**: Step-by-step service instrumentation
- **[HTTP Tracing Patterns](../patterns/http-tracing.md)**: Web service specific solutions

### Working Examples
- **[HTTP Middleware](../../examples/http-middleware/)**: Complete web server with tracing
- **[Database Patterns](../../examples/database-patterns/)**: Query tracing and N+1 detection
- **[Worker Pool](../../examples/worker-pool/)**: Concurrent processing with tracing

## Why tracez?

**Simple**: 4 core types, predictable behavior, no magic
**Fast**: 1.84M spans/sec single-threaded, 3.92M spans/sec parallel (measured with race detection)
**Safe**: Thread-safe, backpressure protection prevents OOM, reasonable allocation patterns (344 B/op, 8 allocs/op)
**Transparent**: All behavior is explicit and testable

When you need basic distributed tracing without OpenTelemetry's complexity, tracez provides just enough observability without getting in your way.