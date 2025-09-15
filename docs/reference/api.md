# API Reference

Complete reference for all public functions and types in tracez.

## Core Types

### Tracer

The main entry point for creating and managing spans.

#### `tracez.New(serviceName string) *Tracer`

Creates a new tracer for the specified service.

```go
tracer := tracez.New("user-service")
defer tracer.Close()
```

**Thread Safety:** ✅ Safe for concurrent use

#### `(t *Tracer) StartSpan(ctx context.Context, operation string) (context.Context, *ActiveSpan)`

Creates a new span. If context contains a parent span, the new span becomes its child.

```go
// Root span (no parent)
ctx, span := tracer.StartSpan(context.Background(), "process-payment")
defer span.Finish()

// Child span (automatically linked)  
childCtx, childSpan := tracer.StartSpan(ctx, "validate-card")
defer childSpan.Finish()
```

**Returns:**
- `context.Context`: New context containing the span
- `*ActiveSpan`: Thread-safe wrapper for span operations

**Thread Safety:** ✅ Safe for concurrent use

#### `(t *Tracer) AddCollector(name string, collector *Collector)`

Registers a collector to receive finished spans.

```go
collector := tracez.NewCollector("main", 1000)
tracer.AddCollector("main", collector)
```

**Thread Safety:** ✅ Safe for concurrent use

#### `(t *Tracer) Close()`

Stops the tracer and releases resources. Call during application shutdown.

```go
defer tracer.Close()
```

### ActiveSpan

Thread-safe wrapper for span operations while the span is active.

#### `(a *ActiveSpan) SetTag(key, value string)`

Adds metadata to the span.

```go
span.SetTag("user.id", userID)
span.SetTag("http.status_code", "200")
span.SetTag("error", "validation failed")
```

**Thread Safety:** ✅ Safe for concurrent use

#### `(a *ActiveSpan) GetTag(key string) (string, bool)`

Retrieves tag value from the span.

```go
if userID, ok := span.GetTag("user.id"); ok {
    // Use userID
}
```

**Thread Safety:** ✅ Safe for concurrent use

#### `(a *ActiveSpan) Finish()`

Completes the span and sends it to all registered collectors.

```go
defer span.Finish() // Always call, typically with defer
```

**Thread Safety:** ✅ Safe for concurrent use  
**Critical:** Must be called exactly once per span

### Collector

Buffers completed spans for batch export.

#### `tracez.NewCollector(name string, bufferSize int) *Collector`

Creates a new collector with specified buffer size.

```go
// Small buffer for low-volume services
collector := tracez.NewCollector("debug", 100)

// Large buffer for high-volume services  
collector := tracez.NewCollector("production", 10000)
```

**Parameters:**
- `bufferSize`: Maximum spans to buffer before dropping

#### `(c *Collector) Export() []Span`

Returns all buffered spans and clears the buffer.

```go
spans := collector.Export()
for _, span := range spans {
    fmt.Printf("%s took %v\n", span.Name, span.Duration)
}

// Send to monitoring system
sendToJaeger(spans)
```

**Thread Safety:** ✅ Safe for concurrent use  
**Behavior:** Atomic operation - either get all spans or none

#### `(c *Collector) DroppedCount() int64`

Returns count of spans dropped due to buffer overflow.

```go
if dropped := collector.DroppedCount(); dropped > 0 {
    log.Printf("Warning: %d spans dropped", dropped)
}
```

**Thread Safety:** ✅ Safe for concurrent use

### Span (Read-Only)

Immutable span data returned by `Export()`.

```go
type Span struct {
    TraceID    string            // Unique trace identifier
    SpanID     string            // Unique span identifier  
    ParentID   string            // Parent span ID (empty for root)
    Name       string            // Operation name
    StartTime  time.Time         // When span started
    Duration   time.Duration     // How long operation took
    Tags       map[string]string // Metadata key-value pairs
}
```

**Usage:**
```go
spans := collector.Export()
for _, span := range spans {
    fmt.Printf("Operation: %s\n", span.Name)
    fmt.Printf("Duration: %v\n", span.Duration)
    fmt.Printf("Trace ID: %s\n", span.TraceID)
    
    if errorMsg, hasError := span.Tags["error"]; hasError {
        fmt.Printf("Error: %s\n", errorMsg)
    }
}
```

## Context Functions

### `tracez.GetSpan(ctx context.Context) *Span`

Retrieves the current span from context (read-only).

```go
if span := tracez.GetSpan(ctx); span != nil {
    fmt.Printf("Current operation: %s", span.Name)
    fmt.Printf("Trace ID: %s", span.TraceID)
}
```

**Returns:** `nil` if no span in context

### `tracez.GetTracer(ctx context.Context) *Tracer`

Retrieves the tracer from context.

```go
if tracer := tracez.GetTracer(ctx); tracer != nil {
    _, childSpan := tracer.StartSpan(ctx, "child-operation")
    defer childSpan.Finish()
}
```

**Returns:** `nil` if no tracer in context

## Usage Patterns

### Basic Request Tracing

```go
func handleRequest(w http.ResponseWriter, r *http.Request) {
    // Create root span
    ctx, span := tracer.StartSpan(r.Context(), "http.request")
    span.SetTag("http.method", r.Method)
    span.SetTag("http.path", r.URL.Path)
    defer span.Finish()
    
    // Business logic with child spans
    result, err := processRequest(ctx)
    if err != nil {
        span.SetTag("error", err.Error())
        http.Error(w, err.Error(), 500)
        return
    }
    
    json.NewEncoder(w).Encode(result)
}

func processRequest(ctx context.Context) (*Result, error) {
    _, span := tracer.StartSpan(ctx, "business.process")
    defer span.Finish()
    
    // Processing logic here
    return &Result{}, nil
}
```

### Database Query Tracing

```go
func getUserByID(ctx context.Context, userID string) (*User, error) {
    _, span := tracer.StartSpan(ctx, "db.get-user")
    span.SetTag("user.id", userID)
    span.SetTag("db.table", "users")
    defer span.Finish()
    
    query := "SELECT * FROM users WHERE id = ?"
    span.SetTag("db.statement", query)
    
    start := time.Now()
    row := db.QueryRowContext(ctx, query, userID)
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", 
        time.Since(start).Seconds()*1000))
    
    var user User
    if err := row.Scan(&user.ID, &user.Name); err != nil {
        span.SetTag("error", err.Error())
        return nil, err
    }
    
    return &user, nil
}
```

### Error Handling

```go
func processPayment(ctx context.Context, payment Payment) error {
    _, span := tracer.StartSpan(ctx, "payment.process")
    span.SetTag("amount", fmt.Sprintf("%.2f", payment.Amount))
    defer span.Finish()
    
    if err := validatePayment(payment); err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "validation")
        return err
    }
    
    if err := chargeCard(ctx, payment); err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "external_service")
        return err
    }
    
    span.SetTag("result", "success")
    return nil
}
```

### Periodic Export

```go
func setupExport(collector *tracez.Collector) {
    ticker := time.NewTicker(30 * time.Second)
    go func() {
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
}
```

## Performance Characteristics

### Span Creation
- **Throughput:** 1.84M spans/sec single-threaded, 3.92M spans/sec parallel
- **Allocation:** ~344 bytes per span + ~20 bytes per tag
- **Thread Safety:** Lock-free span creation, mutex-protected tag operations

### Collector Buffering
- **Non-blocking:** Full collectors drop spans instead of blocking
- **Memory:** Buffer size × average span size (typically 400-500 bytes)
- **Export:** O(1) buffer swap, O(n) span copying

### Key Takeaways

1. **Always call `span.Finish()`** - Use `defer span.Finish()` immediately after `StartSpan()`
2. **Use returned context** - Pass context from `StartSpan()` to child operations
3. **Size collectors appropriately** - Monitor `DroppedCount()` to detect buffer overflow
4. **Export regularly** - Balance between batch efficiency and memory usage
5. **Tag strategically** - Include debugging-relevant metadata, avoid high-cardinality values

## Next Steps

- **[Instrumentation Guide](../guides/instrumentation.md)**: Learn how to add tracing to your services
- **[HTTP Tracing Patterns](../patterns/http-tracing.md)**: Web service specific examples
- **[Database Tracing Patterns](../patterns/database-tracing.md)**: Query optimization techniques
- **[Troubleshooting Guide](troubleshooting.md)**: Debug common issues