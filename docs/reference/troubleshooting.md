# Troubleshooting Guide

Solutions to common tracez issues with exact error messages and fixes.

## Spans Not Appearing

### Symptom: `collector.Export()` returns empty slice

**Cause 1: Spans not finished**
```go
// ✗ Problem: span.Finish() never called
ctx, span := tracer.StartSpan(ctx, "operation")
// Missing: defer span.Finish()
return result
```

**Solution:**
```go
// ✓ Fix: Always call Finish()
ctx, span := tracer.StartSpan(ctx, "operation")
defer span.Finish()  // This line was missing
return result
```

**Cause 2: Export called before spans finish**
```go
// ✗ Problem: Export called immediately
ctx, span := tracer.StartSpan(ctx, "operation")
spans := collector.Export()  // Empty - span not finished yet
span.Finish()
```

**Solution:**
```go
// ✓ Fix: Export after spans complete
ctx, span := tracer.StartSpan(ctx, "operation")
span.Finish()
time.Sleep(10 * time.Millisecond)  // Allow collection
spans := collector.Export()
```

**Cause 3: Collector not registered**
```go
// ✗ Problem: collector created but not registered
collector := tracez.NewCollector("debug", 100)
// Missing: tracer.AddCollector("debug", collector)

ctx, span := tracer.StartSpan(ctx, "operation")
span.Finish()
```

**Solution:**
```go
// ✓ Fix: Register collector with tracer
collector := tracez.NewCollector("debug", 100)
tracer.AddCollector("debug", collector)  // This line was missing
```

## Spans Not Linked (No Parent-Child Relationship)

### Symptom: All spans have empty ParentID

**Cause: Not using context from StartSpan**
```go
// ✗ Problem: Using original context for child
ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")
defer parentSpan.Finish()

// Wrong: using ctx instead of the context returned from StartSpan
_, childSpan := tracer.StartSpan(context.Background(), "child")  
defer childSpan.Finish()
```

**Solution:**
```go
// ✓ Fix: Use context returned from StartSpan
ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")
defer parentSpan.Finish()

// Correct: use ctx which contains the parent span
_, childSpan := tracer.StartSpan(ctx, "child")
defer childSpan.Finish()
```

**Verification:**
```go
spans := collector.Export()
for _, span := range spans {
    if span.Name == "child" {
        if span.ParentID == "" {
            fmt.Println("❌ Child span has no parent")
        } else {
            fmt.Printf("✅ Child span parent: %s\n", span.ParentID)
        }
    }
}
```

## High Memory Usage

### Symptom: Application memory grows continuously

**Cause 1: Buffer sizes too large**
```go
// ✗ Problem: Oversized buffers
collector := tracez.NewCollector("main", 1000000)  // 1M spans!
// At ~400 bytes per span = ~400MB just for buffer
```

**Solution:**
```go
// ✓ Fix: Right-size buffers based on export frequency
// 1000 spans/sec × 30 sec export interval × 1.5 safety = 45K buffer
collector := tracez.NewCollector("main", 45000)
```

**Cause 2: Infrequent exports**
```go
// ✗ Problem: Rare exports let buffers fill
time.Sleep(300 * time.Second)  // 5 minutes between exports
spans := collector.Export()
```

**Solution:**
```go
// ✓ Fix: Export more frequently
ticker := time.NewTicker(30 * time.Second)  // 30 second exports
go func() {
    for range ticker.C {
        spans := collector.Export()
        if len(spans) > 0 {
            processSpans(spans)
        }
    }
}()
```

**Cause 3: Large tag values**
```go
// ✗ Problem: Huge tag values
span.SetTag("request.body", string(largeRequestBody))  // 10MB tag!
span.SetTag("sql.result", string(allRows))  // Entire result set
```

**Solution:**
```go
// ✓ Fix: Limit tag sizes
func setLimitedTag(span *tracez.ActiveSpan, key, value string) {
    if len(value) > 1000 {
        value = value[:1000] + "...[truncated]"
    }
    span.SetTag(key, value)
}

// Usage
setLimitedTag(span, "request.body", string(requestBody))
```

## Dropped Spans

### Symptom: `collector.DroppedCount() > 0`

**Cause: Buffer overflow**
```go
collector := tracez.NewCollector("small", 100)  // Too small
// Generate 1000 spans
for i := 0; i < 1000; i++ {
    _, span := tracer.StartSpan(ctx, "operation")
    span.Finish()
}

dropped := collector.DroppedCount()
fmt.Printf("Dropped: %d spans\n", dropped)  // Dropped: 900 spans
```

**Solution 1: Increase buffer size**
```go
collector := tracez.NewCollector("larger", 2000)  // Bigger buffer
```

**Solution 2: Export more frequently**
```go
// Before: export every 60 seconds
ticker := time.NewTicker(60 * time.Second)

// After: export every 15 seconds
ticker := time.NewTicker(15 * time.Second)
```

**Monitoring drops:**
```go
func monitorDrops(collector *tracez.Collector) {
    ticker := time.NewTicker(30 * time.Second)
    go func() {
        for range ticker.C {
            drops := collector.DroppedCount()
            if drops > 0 {
                log.Printf("WARNING: %d spans dropped - increase buffer or export frequency", drops)
            }
        }
    }()
}
```

## Performance Issues

### Symptom: Application latency increases with tracing

**Cause 1: Synchronous export in request path**
```go
// ✗ Problem: Export blocks request handling
func handleRequest(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.StartSpan(r.Context(), "http.request")
    defer span.Finish()
    
    // Process request
    result := processRequest(ctx)
    
    // BAD: Synchronous export in request handler
    spans := collector.Export()
    sendToMonitoring(spans)  // Blocks request!
    
    json.NewEncoder(w).Encode(result)
}
```

**Solution:**
```go
// ✓ Fix: Asynchronous export
func handleRequest(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.StartSpan(r.Context(), "http.request")
    defer span.Finish()
    
    result := processRequest(ctx)
    
    // No export in request path - handled by background goroutine
    json.NewEncoder(w).Encode(result)
}

// Background export
func setupAsyncExport(collector *tracez.Collector) {
    ticker := time.NewTicker(15 * time.Second)
    go func() {
        for range ticker.C {
            spans := collector.Export()
            go sendToMonitoring(spans)  // Async send
        }
    }()
}
```

**Cause 2: Too many spans**
```go
// ✗ Problem: Over-instrumentation
func processString(s string) string {
    for i, char := range s {
        // Creating span for every character!
        _, span := tracer.StartSpan(ctx, fmt.Sprintf("char-%d", i))
        processChar(char)
        span.Finish()
    }
    return s
}
```

**Solution:**
```go
// ✓ Fix: Trace meaningful operations only
func processString(s string) string {
    _, span := tracer.StartSpan(ctx, "string.process")
    span.SetTag("string.length", strconv.Itoa(len(s)))
    defer span.Finish()
    
    // Process without individual spans
    for _, char := range s {
        processChar(char)
    }
    return s
}
```

## Context Issues

### Symptom: `panic: context deadline exceeded` or similar

**Cause: Context misuse with long-running spans**
```go
// ✗ Problem: Timeout context with long span
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

_, span := tracer.StartSpan(ctx, "long-operation")
defer span.Finish()

time.Sleep(10 * time.Second)  // Exceeds context timeout!
```

**Solution 1: Separate contexts**
```go
// ✓ Fix: Use separate context for tracing
timeoutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

// Create span with background context
traceCtx, span := tracer.StartSpan(context.Background(), "long-operation")
defer span.Finish()

// Use timeout context for actual operation
select {
case result := <-doWork(timeoutCtx):
    span.SetTag("result", "success")
case <-timeoutCtx.Done():
    span.SetTag("error", "timeout")
}
```

**Solution 2: Shorter span lifetimes**
```go
// ✓ Fix: Multiple shorter spans instead of one long span
func longProcess(ctx context.Context) {
    for i := 0; i < 10; i++ {
        _, span := tracer.StartSpan(ctx, fmt.Sprintf("step-%d", i))
        doStep(ctx, i)
        span.Finish()  // Finish each step span
    }
}
```

## Integration Issues

### Symptom: Spans missing in distributed traces

**Cause: Trace context not propagated across services**
```go
// ✗ Problem: New request doesn't carry trace context
func callExternalService(ctx context.Context, url string) error {
    // Missing: extract trace context from ctx
    req, _ := http.NewRequest("GET", url, nil)
    
    resp, err := http.DefaultClient.Do(req)  // No trace context!
    return err
}
```

**Solution:**
```go
// ✓ Fix: Add trace headers to outbound requests
func callExternalService(ctx context.Context, url string) error {
    req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
    
    // Extract trace context and add headers
    if span := tracez.GetSpan(ctx); span != nil {
        req.Header.Set("X-Trace-ID", span.TraceID)
        req.Header.Set("X-Parent-Span-ID", span.SpanID)
    }
    
    resp, err := http.DefaultClient.Do(req)
    return err
}

// In receiving service
func handleInbound(w http.ResponseWriter, r *http.Request) {
    traceID := r.Header.Get("X-Trace-ID")
    parentSpanID := r.Header.Get("X-Parent-Span-ID")
    
    // Create span with inherited trace context
    ctx, span := tracer.StartSpanWithParent(r.Context(), "inbound.request", traceID, parentSpanID)
    defer span.Finish()
}
```

## Testing Issues

### Symptom: Tests fail with race conditions

**Cause: Concurrent span access without proper synchronization**
```go
// ✗ Problem: Race condition in test
func TestConcurrentSpans(t *testing.T) {
    var wg sync.WaitGroup
    
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            _, span := tracer.StartSpan(ctx, "test")
            span.SetTag("iteration", strconv.Itoa(i))  // Race condition!
            span.Finish()
        }()
    }
    
    wg.Wait()
}
```

**Solution:**
```go
// ✓ Fix: Each goroutine uses its own span
func TestConcurrentSpans(t *testing.T) {
    var wg sync.WaitGroup
    
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(iteration int) {  // Capture iteration value
            defer wg.Done()
            _, span := tracer.StartSpan(ctx, "test")
            span.SetTag("iteration", strconv.Itoa(iteration))
            span.Finish()
        }(i)
    }
    
    wg.Wait()
}
```

**Test span verification:**
```go
func TestSpanCreation(t *testing.T) {
    collector := tracez.NewCollector("test", 100)
    tracer.AddCollector("test", collector)
    
    // Create test span
    _, span := tracer.StartSpan(context.Background(), "test-operation")
    span.SetTag("test.key", "test.value")
    span.Finish()
    
    // Wait for collection
    time.Sleep(10 * time.Millisecond)
    
    // Verify span
    spans := collector.Export()
    require.Len(t, spans, 1)
    assert.Equal(t, "test-operation", spans[0].Name)
    assert.Equal(t, "test.value", spans[0].Tags["test.key"])
}
```

## Debugging Checklist

When tracing isn't working:

1. **✅ Span lifecycle**
   - [ ] `StartSpan()` called
   - [ ] `Finish()` called with `defer`
   - [ ] Export called after spans complete

2. **✅ Collector setup**
   - [ ] Collector created with appropriate buffer size
   - [ ] Collector registered with `tracer.AddCollector()`
   - [ ] Export called on correct collector

3. **✅ Context propagation**
   - [ ] Using context returned from `StartSpan()`
   - [ ] Context passed to child operations
   - [ ] No context deadline issues

4. **✅ Memory management**
   - [ ] Buffer sizes appropriate for span volume
   - [ ] Export frequency matches span rate
   - [ ] Tag values reasonable size

5. **✅ Performance**
   - [ ] Export happens asynchronously
   - [ ] Not over-instrumenting
   - [ ] Monitor `DroppedCount()` for buffer overflow

## Getting Help

### Enable Debug Logging
```go
func debugCollector(collector *tracez.Collector) {
    ticker := time.NewTicker(5 * time.Second)
    go func() {
        for range ticker.C {
            spans := collector.Export()
            log.Printf("DEBUG: Exported %d spans", len(spans))
            for _, span := range spans {
                log.Printf("  %s (%v) - %+v", span.Name, span.Duration, span.Tags)
            }
        }
    }()
}
```

### Minimal Reproduction
```go
func main() {
    tracer := tracez.New("debug-service")
    defer tracer.Close()
    
    collector := tracez.NewCollector("debug", 10)
    tracer.AddCollector("debug", collector)
    
    _, span := tracer.StartSpan(context.Background(), "test")
    span.SetTag("debug", "true")
    span.Finish()
    
    time.Sleep(10 * time.Millisecond)
    
    spans := collector.Export()
    fmt.Printf("Got %d spans\n", len(spans))
    for _, s := range spans {
        fmt.Printf("  %s: %+v\n", s.Name, s.Tags)
    }
}
```

If issues persist after following this guide, create a minimal reproduction case and check the project's issue tracker.