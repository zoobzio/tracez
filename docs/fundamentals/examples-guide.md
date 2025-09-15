# Examples Guide

How to run, understand, and modify the working examples in tracez.

## Overview

The `examples/` directory contains three complete, runnable examples that demonstrate real-world tracing patterns:

- **[http-middleware/](../../examples/http-middleware/)**: Complete web server with tracing middleware
- **[database-patterns/](../../examples/database-patterns/)**: Database tracing and N+1 query detection  
- **[worker-pool/](../../examples/worker-pool/)**: Concurrent processing with span correlation

Each example includes:
- Working Go code you can run immediately
- Tests demonstrating the tracing output
- Comments explaining why specific patterns are used

## Running the Examples

### Prerequisites
```bash
cd /path/to/tracez
go mod download  # Ensure dependencies are available
```

### HTTP Middleware Example
```bash
cd examples/http-middleware
go run main.go
```

In another terminal:
```bash
# Test basic endpoint
curl http://localhost:8080/

# Test endpoint that triggers database calls
curl http://localhost:8080/users

# Test error handling
curl http://localhost:8080/error
```

**What you'll see**: Trace output for each HTTP request, showing how requests flow through middleware, handlers, and simulated database calls.

### Database Patterns Example
```bash
cd examples/database-patterns  
go run main.go
```

**What you'll see**: Side-by-side comparison of traced vs untraced database operations, demonstrating how tracing reveals N+1 query problems.

### Worker Pool Example
```bash
cd examples/worker-pool
go run main.go
```

**What you'll see**: Concurrent job processing with spans that show which worker handled each job and how long each job took.

## Understanding the Output

### Reading Trace Output
When you run the examples, you'll see output like:
```
Collected 3 spans:
  http.request (45.2ms) tags=map[http.method:GET http.path:/ http.status_code:200]
  ├─ auth.verify (2.1ms) tags=map[user.authenticated:true]
  └─ db.query (15.7ms) tags=map[db.table:users query.type:SELECT]
```

This shows:
- **Hierarchy**: Parent-child relationships (indented)
- **Duration**: How long each operation took  
- **Tags**: Metadata attached to each span
- **Names**: What each operation represents

### Trace Trees
Complex requests create tree structures:
```
http.request (100ms)
├─ middleware.auth (5ms)
├─ handler.users (90ms)
│  ├─ db.get-user (20ms)
│  ├─ db.get-posts (30ms) 
│  └─ render.template (25ms)
└─ middleware.logging (2ms)
```

## Key Patterns Demonstrated

### 1. HTTP Middleware Pattern
From `examples/http-middleware/main.go`:

```go
func TracingMiddleware(tracer *tracez.Tracer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create root span for request
            ctx, span := tracer.StartSpan(r.Context(), "http.request")
            span.SetTag("http.method", r.Method)
            span.SetTag("http.path", r.URL.Path)
            defer span.Finish()
            
            // Pass traced context to handlers
            next.ServeHTTP(wrapped, r.WithContext(ctx))
            
            // Tag response details
            span.SetTag("http.status_code", fmt.Sprintf("%d", wrapped.statusCode))
        })
    }
}
```

**Key insights**:
- Middleware creates root span for each request
- Context propagates automatically to handlers
- Response details tagged after request completes

### 2. Database Tracing Pattern
From `examples/database-patterns/main.go`:

```go
func traceQuery(ctx context.Context, tracer *tracez.Tracer, query string) {
    _, span := tracer.StartSpan(ctx, "db.query")
    span.SetTag("db.statement", query)
    span.SetTag("db.table", extractTable(query))
    defer span.Finish()
    
    // Simulate database work
    time.Sleep(time.Duration(rand.Intn(20)) * time.Millisecond)
}
```

**Key insights**:
- Each query gets its own span
- SQL statement and table name tagged
- Duration reveals slow queries automatically

### 3. Worker Pool Pattern  
From `examples/worker-pool/main.go`:

```go
func worker(id int, jobs <-chan Job, tracer *tracez.Tracer) {
    for job := range jobs {
        // Create span for this job
        ctx, span := tracer.StartSpan(context.Background(), "job.process")
        span.SetTag("worker.id", fmt.Sprintf("%d", id))
        span.SetTag("job.id", job.ID)
        span.SetTag("job.type", job.Type)
        
        // Process job with traced context
        processJob(ctx, job, tracer)
        span.Finish()
    }
}
```

**Key insights**:
- Each job gets independent trace
- Worker ID tagged for debugging
- Job details preserved in tags

## Modifying the Examples

### Adding Custom Tags
```go
// Add business-specific metadata
span.SetTag("user.role", userRole)
span.SetTag("feature.flag", featureEnabled)
span.SetTag("cache.hit", cacheResult)
```

### Creating Child Spans
```go
func processUser(ctx context.Context, tracer *tracez.Tracer, userID string) {
    // Child span automatically links to parent in ctx
    _, span := tracer.StartSpan(ctx, "user.process")
    span.SetTag("user.id", userID)
    defer span.Finish()
    
    // Further child spans
    validateUser(ctx, tracer, userID)
    updateDatabase(ctx, tracer, userID)
}
```

### Error Handling
```go
_, span := tracer.StartSpan(ctx, "operation")
defer span.Finish()

if err := riskyOperation(); err != nil {
    span.SetTag("error", err.Error())
    span.SetTag("error.type", "validation")
    return err
}
```

## Testing Your Changes

Each example includes tests:

```bash
cd examples/http-middleware
go test -v  # See trace output in test logs

cd examples/database-patterns  
go test -v  # Verify tracing doesn't affect behavior

cd examples/worker-pool
go test -v  # Check concurrent span creation
```

The tests validate that:
- Tracing doesn't change application behavior
- Spans are created with correct hierarchy
- Tags contain expected values
- No goroutine leaks or data races

## Performance Considerations

### What the Examples Show

**Minimal overhead**: Examples demonstrate that tracing adds:
- ~344 bytes per span
- ~8 allocations per span
- Microsecond-level overhead

**Backpressure protection**: When collectors are full:
- Application stays fast (spans dropped, not blocked)
- Monitor with `collector.DroppedCount()`

**Thread safety**: All examples use concurrent patterns:
- Multiple goroutines creating spans
- Simultaneous tag operations
- Concurrent collector exports

## Common Modifications

### Changing Collector Buffer Size
```go
// Small buffer for testing
collector := tracez.NewCollector("debug", 10)

// Large buffer for production  
collector := tracez.NewCollector("production", 10000)
```

### Adding Multiple Collectors
```go
// Console collector for development
debugCollector := tracez.NewCollector("debug", 100)
tracer.AddCollector("debug", debugCollector)

// Production collector with larger buffer
prodCollector := tracez.NewCollector("production", 5000)
tracer.AddCollector("production", prodCollector)

// Export from specific collector
debugSpans := debugCollector.Export()
```

### Custom Export Logic
```go
// Periodic export example
go func() {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            spans := collector.Export()
            if len(spans) > 0 {
                sendToMonitoring(spans)
            }
        }
    }
}()
```

## Next Steps

After understanding the examples:

- **[Instrumentation Guide](../guides/instrumentation.md)**: Apply these patterns to your own services
- **[HTTP Tracing Patterns](../patterns/http-tracing.md)**: Advanced web service tracing
- **[Database Tracing Patterns](../patterns/database-tracing.md)**: Query optimization and debugging
- **[Production Guide](../guides/production.md)**: Deploy tracing in production environments

## Troubleshooting Examples

### Example Won't Run
```bash
# Check Go version
go version  # Requires 1.21+

# Verify module
go mod tidy
go mod download

# Run with verbose output
go run -v main.go
```

### No Trace Output
1. Check collector buffer size (may be too small)
2. Verify `span.Finish()` is called
3. Ensure `collector.Export()` is called after spans finish
4. Add brief `time.Sleep()` before export to allow collection

### Spans Not Linked
1. Verify you're using context from `StartSpan()`
2. Check that child spans receive parent's context
3. Confirm context propagates through function calls

The examples are designed to be modified and extended. Start with the pattern closest to your use case and adapt it to your specific needs.