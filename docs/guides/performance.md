# Performance Guide

Optimization techniques and performance characteristics for tracez in production environments.

## Performance Characteristics

### Benchmarked Performance
- **Single-threaded**: 1.84M spans/sec
- **Parallel**: 3.92M spans/sec (with race detection)
- **Memory per span**: ~344 bytes base + ~20 bytes per tag
- **Allocations per span**: ~8 allocations
- **Thread safety**: All operations safe for concurrent use

## Hot Path Optimization

### Span Creation Performance

```go
// ✓ Optimized - minimal allocations
func OptimizedHandler(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.StartSpan(r.Context(), "http.request")
    defer span.Finish()
    
    // Set essential tags only
    span.SetTag("method", r.Method)
    span.SetTag("path", r.URL.Path)
    
    // Business logic
    processRequest(ctx)
}

// ✗ Unoptimized - excessive allocations
func UnoptimizedHandler(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.StartSpan(r.Context(), "http.request")
    defer span.Finish()
    
    // Too many tags create allocation pressure
    span.SetTag("method", r.Method)
    span.SetTag("path", r.URL.Path)
    span.SetTag("user_agent", r.UserAgent())
    span.SetTag("remote_addr", r.RemoteAddr)
    span.SetTag("content_length", strconv.FormatInt(r.ContentLength, 10))
    span.SetTag("host", r.Host)
    span.SetTag("proto", r.Proto)
    span.SetTag("request_uri", r.RequestURI)
    // ... more tags = more allocations
    
    processRequest(ctx)
}
```

### Tag Optimization

```go
// ✓ Optimized - reuse string constants
const (
    MethodTag = "http.method"
    PathTag   = "http.path"
    StatusTag = "http.status"
)

func optimizedTagging(span *tracez.ActiveSpan, r *http.Request) {
    span.SetTag(MethodTag, r.Method)      // Reused constants
    span.SetTag(PathTag, r.URL.Path)
    span.SetTag(StatusTag, "200")
}

// ✗ Unoptimized - string allocation on each call
func unoptimizedTagging(span *tracez.ActiveSpan, r *http.Request) {
    span.SetTag("http.method", r.Method)  // New string each time
    span.SetTag("http.path", r.URL.Path)
    span.SetTag("http.status", "200")
}
```

### Conditional Tracing

```go
// ✓ Optimized - skip expensive operations when not tracing
func ConditionalTracing(ctx context.Context, data []byte) {
    tracer := tracez.GetTracer(ctx)
    if tracer == nil {
        // No tracer - skip all tracing work
        processData(data)
        return
    }
    
    _, span := tracer.StartSpan(ctx, "data.process")
    defer span.Finish()
    
    // Only do expensive tagging when tracing
    if len(data) > 1024 {
        span.SetTag("data.large", "true")
        span.SetTag("data.size", strconv.Itoa(len(data)))
    }
    
    processData(data)
}
```

## Memory Optimization

### Buffer Sizing Strategy

```go
// Memory-optimized collector sizing
func OptimizeCollectorSizing(spanRate int, exportInterval time.Duration) int {
    // Calculate buffer size with safety margin
    spansPerSecond := float64(spanRate)
    exportSeconds := exportInterval.Seconds()
    safetyFactor := 1.5 // 50% safety margin
    
    bufferSize := int(spansPerSecond * exportSeconds * safetyFactor)
    
    // Minimum and maximum bounds
    if bufferSize < 100 {
        bufferSize = 100
    }
    if bufferSize > 50000 {
        bufferSize = 50000 // Prevent excessive memory usage
    }
    
    return bufferSize
}

// Production setup with optimized sizing
func setupOptimizedCollectors(tracer *tracez.Tracer) {
    // High-frequency, small buffer for errors (immediate export)
    errorCollector := tracez.NewCollector("errors", OptimizeCollectorSizing(10, 5*time.Second))
    tracer.AddCollector("errors", errorCollector)
    
    // Medium-frequency for HTTP requests
    httpCollector := tracez.NewCollector("http", OptimizeCollectorSizing(1000, 15*time.Second))
    tracer.AddCollector("http", httpCollector)
    
    // Low-frequency, large buffer for background jobs
    jobCollector := tracez.NewCollector("jobs", OptimizeCollectorSizing(50, 60*time.Second))
    tracer.AddCollector("jobs", jobCollector)
}
```

### Tag Value Optimization

```go
// ✓ Optimized - limit tag value sizes
func SetOptimizedTag(span *tracez.ActiveSpan, key, value string) {
    const maxTagLength = 1000
    
    if len(value) > maxTagLength {
        value = value[:maxTagLength] + "...[truncated]"
    }
    
    span.SetTag(key, value)
}

// ✓ Optimized - structured tag values
func SetStructuredTags(span *tracez.ActiveSpan, err error) {
    span.SetTag("error", err.Error())
    span.SetTag("error.type", categorizeError(err))
    
    // Don't include full stack traces in tags
    // Send stack traces to logs instead
    log.Printf("Error with stack: %+v", err)
}

// ✗ Unoptimized - huge tag values
func SetUnoptimizedTags(span *tracez.ActiveSpan, err error) {
    span.SetTag("error", err.Error())
    span.SetTag("stack_trace", debug.Stack()) // Potentially huge!
}
```

### Span Lifecycle Optimization

```go
// ✓ Optimized - immediate span finishing
func ProcessItem(ctx context.Context, item Item) {
    _, span := tracer.StartSpan(ctx, "item.process")
    
    // Set tags early
    span.SetTag("item.id", item.ID)
    span.SetTag("item.type", item.Type)
    
    // Process item
    result := item.Process()
    
    // Finish span immediately after processing
    span.SetTag("result", result.String())
    span.Finish()
    
    // Don't defer - finish as soon as possible
    // Any post-processing doesn't need to be traced
    postProcess(result)
}

// ✗ Unoptimized - long-lived spans
func ProcessItemUnoptimized(ctx context.Context, item Item) {
    _, span := tracer.StartSpan(ctx, "item.process")
    defer span.Finish() // Span lives for entire function
    
    result := item.Process()
    span.SetTag("result", result.String())
    
    // Long post-processing keeps span alive
    time.Sleep(1 * time.Second)
    postProcess(result)
}
```

## High-Throughput Optimization

### Batch Export Optimization

```go
type OptimizedExporter struct {
    collectors []tracez.Collector
    batchSize  int
    client     *http.Client
}

func NewOptimizedExporter(batchSize int) *OptimizedExporter {
    return &OptimizedExporter{
        batchSize: batchSize,
        client: &http.Client{
            Transport: &http.Transport{
                MaxIdleConns:        100,
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
            },
            Timeout: 30 * time.Second,
        },
    }
}

func (oe *OptimizedExporter) Export() {
    // Collect from all collectors in parallel
    spanChannels := make([]<-chan []tracez.Span, len(oe.collectors))
    for i, collector := range oe.collectors {
        ch := make(chan []tracez.Span, 1)
        spanChannels[i] = ch
        
        go func(c tracez.Collector, ch chan<- []tracez.Span) {
            spans := c.Export()
            ch <- spans
            close(ch)
        }(collector, ch)
    }
    
    // Gather all spans
    var allSpans []tracez.Span
    for _, ch := range spanChannels {
        spans := <-ch
        allSpans = append(allSpans, spans...)
    }
    
    // Export in batches
    for i := 0; i < len(allSpans); i += oe.batchSize {
        end := i + oe.batchSize
        if end > len(allSpans) {
            end = len(allSpans)
        }
        
        batch := allSpans[i:end]
        go oe.exportBatch(batch) // Parallel batch export
    }
}

func (oe *OptimizedExporter) exportBatch(spans []tracez.Span) {
    // Optimized serialization
    var buf bytes.Buffer
    encoder := json.NewEncoder(&buf)
    encoder.SetEscapeHTML(false) // Faster encoding
    
    if err := encoder.Encode(spans); err != nil {
        log.Printf("Encoding error: %v", err)
        return
    }
    
    // Send to monitoring system
    req, _ := http.NewRequest("POST", "https://monitoring.example.com/traces", &buf)
    req.Header.Set("Content-Type", "application/json")
    req.Header.Set("Content-Encoding", "gzip")
    
    resp, err := oe.client.Do(req)
    if err != nil {
        log.Printf("Export error: %v", err)
        return
    }
    resp.Body.Close()
}
```

### Concurrent Span Processing

```go
// ✓ Optimized - minimal contention
func ProcessConcurrentRequests(requests []Request) {
    // Pre-allocate workers to avoid goroutine creation overhead
    const numWorkers = 10
    reqChan := make(chan Request, len(requests))
    
    // Start workers
    var wg sync.WaitGroup
    for i := 0; i < numWorkers; i++ {
        wg.Add(1)
        go func() {
            defer wg.Done()
            for req := range reqChan {
                // Each worker creates independent spans
                ctx, span := tracer.StartSpan(context.Background(), "request.process")
                span.SetTag("request.id", req.ID)
                
                processRequest(ctx, req)
                span.Finish()
            }
        }()
    }
    
    // Send work to workers
    for _, req := range requests {
        reqChan <- req
    }
    close(reqChan)
    
    wg.Wait()
}
```

## Profiling and Monitoring

### Performance Monitoring

```go
type TracingMetrics struct {
    SpansCreated     int64
    SpansFinished    int64
    SpansDropped     int64
    TagsSet          int64
    ExportDuration   time.Duration
    CollectorPressure map[string]int64
}

func MonitorTracingPerformance(collectors map[string]*tracez.Collector) {
    ticker := time.NewTicker(60 * time.Second)
    go func() {
        defer ticker.Stop()
        
        for range ticker.C {
            metrics := collectMetrics(collectors)
            logMetrics(metrics)
            
            // Alert on performance issues
            if metrics.SpansDropped > 1000 {
                log.Printf("HIGH DROP RATE: %d spans dropped in last minute", metrics.SpansDropped)
            }
            
            for name, pressure := range metrics.CollectorPressure {
                if pressure > 80 { // > 80% full
                    log.Printf("COLLECTOR PRESSURE: %s is %d%% full", name, pressure)
                }
            }
        }
    }()
}

func collectMetrics(collectors map[string]*tracez.Collector) TracingMetrics {
    var metrics TracingMetrics
    metrics.CollectorPressure = make(map[string]int64)
    
    for name, collector := range collectors {
        dropped := collector.DroppedCount()
        metrics.SpansDropped += dropped
        
        // Estimate pressure (would need to expose buffer stats)
        // metrics.CollectorPressure[name] = collector.PressurePercent()
    }
    
    return metrics
}
```

### CPU Profiling Integration

```go
func EnableProfilingInProduction() {
    if os.Getenv("ENABLE_PROFILING") == "true" {
        go func() {
            log.Println("Starting profiling server on :6060")
            log.Println(http.ListenAndServe(":6060", nil))
        }()
    }
}

// Profile span creation performance
func BenchmarkSpanCreation(b *testing.B) {
    tracer := tracez.New("benchmark")
    collector := tracez.NewCollector("bench", 10000)
    tracer.AddCollector("bench", collector)
    
    b.ResetTimer()
    b.ReportAllocs()
    
    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            ctx, span := tracer.StartSpan(context.Background(), "benchmark")
            span.SetTag("iteration", "test")
            span.Finish()
        }
    })
    
    // Report custom metrics
    spans := collector.Export()
    b.ReportMetric(float64(len(spans)), "spans/exported")
}
```

## Production Performance Tuning

### Environment-Based Configuration

```go
func ConfigureForEnvironment(env string) TracingConfig {
    switch env {
    case "production":
        return TracingConfig{
            BufferSize:     15000,
            ExportInterval: 30 * time.Second,
            MaxTagSize:     500,
            SampleRate:     0.1, // 10% sampling
        }
    case "staging":
        return TracingConfig{
            BufferSize:     5000,
            ExportInterval: 15 * time.Second,
            MaxTagSize:     1000,
            SampleRate:     0.5, // 50% sampling
        }
    case "development":
        return TracingConfig{
            BufferSize:     1000,
            ExportInterval: 5 * time.Second,
            MaxTagSize:     2000,
            SampleRate:     1.0, // 100% tracing
        }
    default:
        return TracingConfig{
            BufferSize:     1000,
            ExportInterval: 10 * time.Second,
            MaxTagSize:     1000,
            SampleRate:     0.1,
        }
    }
}
```

### Resource Limit Enforcement

```go
type ResourceLimits struct {
    MaxMemoryMB     int
    MaxGoroutines   int
    MaxSpansPerSec  int
}

func EnforceResourceLimits(limits ResourceLimits) {
    // Memory monitoring
    go func() {
        ticker := time.NewTicker(30 * time.Second)
        defer ticker.Stop()
        
        for range ticker.C {
            var m runtime.MemStats
            runtime.ReadMemStats(&m)
            
            memMB := int(m.Alloc / 1024 / 1024)
            if memMB > limits.MaxMemoryMB {
                log.Printf("MEMORY LIMIT EXCEEDED: %dMB > %dMB", memMB, limits.MaxMemoryMB)
                // Trigger garbage collection
                runtime.GC()
            }
        }
    }()
    
    // Goroutine monitoring
    go func() {
        ticker := time.NewTicker(60 * time.Second)
        defer ticker.Stop()
        
        for range ticker.C {
            goroutines := runtime.NumGoroutine()
            if goroutines > limits.MaxGoroutines {
                log.Printf("GOROUTINE LIMIT EXCEEDED: %d > %d", goroutines, limits.MaxGoroutines)
            }
        }
    }()
}
```

## Performance Best Practices

### DO's
1. **Size buffers appropriately** - Match buffer size to export frequency and span rate
2. **Limit tag sizes** - Truncate large values to prevent memory bloat
3. **Use span hierarchies** - Child spans are more efficient than independent spans
4. **Export asynchronously** - Never block request processing for export
5. **Monitor drop rates** - Alert when collectors drop spans
6. **Profile in production** - Use pprof to identify bottlenecks

### DON'Ts
1. **Don't over-instrument** - Trace meaningful operations only
2. **Don't create spans in tight loops** - Batch operations instead
3. **Don't use huge tag values** - Limit to 1KB per tag
4. **Don't defer spans unnecessarily** - Finish spans as soon as possible
5. **Don't ignore memory pressure** - Monitor and adjust buffer sizes
6. **Don't export synchronously** - Always export in background

## Performance Testing

```go
func TestPerformanceUnderLoad(t *testing.T) {
    tracer := tracez.New("load-test")
    collector := tracez.NewCollector("load", 50000)
    tracer.AddCollector("load", collector)
    
    const (
        numGoroutines = 100
        spansPerGoroutine = 1000
        totalSpans = numGoroutines * spansPerGoroutine
    )
    
    start := time.Now()
    var wg sync.WaitGroup
    
    for i := 0; i < numGoroutines; i++ {
        wg.Add(1)
        go func(goroutineID int) {
            defer wg.Done()
            
            for j := 0; j < spansPerGoroutine; j++ {
                ctx, span := tracer.StartSpan(context.Background(), "load-test")
                span.SetTag("goroutine", strconv.Itoa(goroutineID))
                span.SetTag("iteration", strconv.Itoa(j))
                span.Finish()
            }
        }(i)
    }
    
    wg.Wait()
    duration := time.Since(start)
    
    // Allow collection to complete
    time.Sleep(100 * time.Millisecond)
    
    spans := collector.Export()
    dropped := collector.DroppedCount()
    
    spansPerSecond := float64(totalSpans) / duration.Seconds()
    
    t.Logf("Performance Results:")
    t.Logf("  Total spans: %d", totalSpans)
    t.Logf("  Collected spans: %d", len(spans))
    t.Logf("  Dropped spans: %d", dropped)
    t.Logf("  Duration: %v", duration)
    t.Logf("  Spans/second: %.0f", spansPerSecond)
    
    // Performance assertions
    assert.True(t, spansPerSecond > 100000, "Should process >100K spans/sec")
    assert.True(t, dropped < totalSpans/10, "Drop rate should be <10%")
}
```

## Next Steps

- **[Production Guide](production.md)**: Deploy performance-optimized tracing
- **[Sampling Patterns](../patterns/sampling.md)**: Reduce volume with intelligent sampling
- **[Context Propagation](context-propagation.md)**: Optimize context flow
- **[Troubleshooting Guide](../reference/troubleshooting.md)**: Debug performance issues