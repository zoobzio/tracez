# Production Deployment Guide

Configuration patterns and sizing guidelines for production tracez deployments.

## Buffer Sizing

### Calculation Formula

Buffer size should accommodate expected span volume between exports:

```
buffer_size = spans_per_second × export_interval_seconds × safety_factor
```

**Recommended safety factor: 1.5-2.0** to handle traffic spikes.

### Examples

```go
// Low-volume service: 50 spans/sec, export every 30 seconds
bufferSize := 50 * 30 * 2 // = 3,000 spans
collector := tracez.NewCollector("low-volume", 3000)

// High-volume service: 1000 spans/sec, export every 10 seconds  
bufferSize := 1000 * 10 * 2 // = 20,000 spans
collector := tracez.NewCollector("high-volume", 20000)

// Microservice: 100 spans/sec, export every 60 seconds
bufferSize := 100 * 60 * 1.5 // = 9,000 spans  
collector := tracez.NewCollector("microservice", 9000)
```

### Buffer Size Guidelines

| Service Type | Spans/Second | Export Interval | Buffer Size |
|--------------|--------------|-----------------|-------------|
| Microservice | 50-200 | 30-60s | 3K-24K |
| Web API | 200-1000 | 10-30s | 4K-60K |
| High-Volume | 1000+ | 5-15s | 15K-45K |

## Memory Estimation

### Per-Span Memory Usage

```go
// Base span: ~344 bytes
// Each tag: ~20 bytes (key + value overhead)
// Example with 5 tags: 344 + (5 × 20) = 444 bytes
```

### Buffer Memory Calculation

```go
// Memory per collector = buffer_size × average_span_size
memoryMB := (bufferSize * averageSpanBytes) / (1024 * 1024)

// Example: 10,000 spans × 450 bytes = 4.3MB per collector
```

### Total Memory Planning

```go
// Application with 3 collectors
collector1 := tracez.NewCollector("http", 15000)    // 6.4MB
collector2 := tracez.NewCollector("db", 8000)       // 3.4MB  
collector3 := tracez.NewCollector("worker", 5000)   // 2.1MB
// Total tracing memory: ~12MB
```

## Export Strategies

### Periodic Export (Recommended)

```go
func setupPeriodicExport(collector *tracez.Collector, interval time.Duration) {
    ticker := time.NewTicker(interval)
    go func() {
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                spans := collector.Export()
                if len(spans) > 0 {
                    if err := sendToMonitoring(spans); err != nil {
                        log.Printf("Export failed: %v", err)
                        // Consider re-queuing or alerting
                    }
                }
            }
        }
    }()
}

// Setup for different service types
func setupProduction() {
    tracer := tracez.New("user-service")
    
    // HTTP requests - frequent export for immediate visibility
    httpCollector := tracez.NewCollector("http", 5000)
    tracer.AddCollector("http", httpCollector)
    setupPeriodicExport(httpCollector, 10*time.Second)
    
    // Database operations - less frequent, larger batches
    dbCollector := tracez.NewCollector("db", 10000)
    tracer.AddCollector("db", dbCollector)  
    setupPeriodicExport(dbCollector, 30*time.Second)
    
    // Background jobs - infrequent but complete
    jobCollector := tracez.NewCollector("jobs", 2000)
    tracer.AddCollector("jobs", jobCollector)
    setupPeriodicExport(jobCollector, 60*time.Second)
}
```

### Batch Size Optimization

```go
func sendToMonitoring(spans []tracez.Span) error {
    const maxBatchSize = 1000
    
    // Process in batches to avoid overwhelming monitoring system
    for i := 0; i < len(spans); i += maxBatchSize {
        end := i + maxBatchSize
        if end > len(spans) {
            end = len(spans)
        }
        
        batch := spans[i:end]
        if err := sendBatch(batch); err != nil {
            return fmt.Errorf("batch %d-%d failed: %w", i, end-1, err)
        }
        
        // Small delay between batches to avoid rate limiting
        if end < len(spans) {
            time.Sleep(100 * time.Millisecond)
        }
    }
    
    return nil
}
```

### Graceful Shutdown

```go
func gracefulShutdown(tracer *tracez.Tracer, collectors map[string]*tracez.Collector) {
    // Stop accepting new requests first
    log.Println("Stopping tracer...")
    tracer.Close()
    
    // Export remaining spans
    log.Println("Exporting remaining spans...")
    for name, collector := range collectors {
        spans := collector.Export()
        if len(spans) > 0 {
            log.Printf("Exporting %d spans from %s collector", len(spans), name)
            if err := sendToMonitoring(spans); err != nil {
                log.Printf("Warning: Failed to export %s spans: %v", name, err)
            }
        }
    }
    
    log.Println("Tracing shutdown complete")
}

// Integration with signal handling
func main() {
    tracer, collectors := setupProduction()
    
    // Graceful shutdown on SIGTERM/SIGINT
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
    
    go func() {
        <-sigCh
        gracefulShutdown(tracer, collectors)
        os.Exit(0)
    }()
    
    // Run your application
    runApplication()
}
```

## Monitoring Tracer Health

### Collector Metrics

```go
func monitorCollectors(collectors map[string]*tracez.Collector) {
    ticker := time.NewTicker(30 * time.Second)
    go func() {
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                for name, collector := range collectors {
                    dropped := collector.DroppedCount()
                    if dropped > 0 {
                        log.Printf("WARNING: %s collector dropped %d spans", name, dropped)
                        
                        // Alert if drop rate is high
                        if dropped > 1000 {
                            sendAlert(fmt.Sprintf("High span drop rate in %s: %d", name, dropped))
                        }
                    }
                }
            }
        }
    }()
}

func sendAlert(message string) {
    // Send to your alerting system
    log.Printf("ALERT: %s", message)
}
```

### Performance Metrics

```go
type TracingMetrics struct {
    SpansCreated  int64
    SpansExported int64
    SpansDropped  int64
    ExportErrors  int64
}

var metrics TracingMetrics

func trackSpanCreation() {
    atomic.AddInt64(&metrics.SpansCreated, 1)
}

func trackSpanExport(count int) {
    atomic.AddInt64(&metrics.SpansExported, int64(count))
}

func trackSpanDrop(count int64) {
    atomic.AddInt64(&metrics.SpansDropped, count)
}

func reportMetrics() {
    ticker := time.NewTicker(60 * time.Second)
    go func() {
        defer ticker.Stop()
        for {
            select {
            case <-ticker.C:
                created := atomic.LoadInt64(&metrics.SpansCreated)
                exported := atomic.LoadInt64(&metrics.SpansExported)
                dropped := atomic.LoadInt64(&metrics.SpansDropped)
                
                log.Printf("Tracing metrics: created=%d, exported=%d, dropped=%d", 
                    created, exported, dropped)
                
                // Send to metrics system
                sendMetrics(map[string]int64{
                    "spans.created":  created,
                    "spans.exported": exported,
                    "spans.dropped":  dropped,
                })
            }
        }
    }()
}
```

## Environment Configuration

### Environment-Based Setup

```go
func setupByEnvironment() (*tracez.Tracer, map[string]*tracez.Collector) {
    serviceName := os.Getenv("SERVICE_NAME")
    if serviceName == "" {
        serviceName = "unknown-service"
    }
    
    tracer := tracez.New(serviceName)
    collectors := make(map[string]*tracez.Collector)
    
    env := os.Getenv("ENVIRONMENT")
    
    switch env {
    case "production":
        collectors["http"] = tracez.NewCollector("http", 10000)
        collectors["db"] = tracez.NewCollector("db", 15000)  
        collectors["external"] = tracez.NewCollector("external", 5000)
        
        // Production export intervals
        setupPeriodicExport(collectors["http"], 15*time.Second)
        setupPeriodicExport(collectors["db"], 45*time.Second)
        setupPeriodicExport(collectors["external"], 30*time.Second)
        
    case "staging":
        collectors["main"] = tracez.NewCollector("main", 5000)
        setupPeriodicExport(collectors["main"], 30*time.Second)
        
    case "development":
        collectors["debug"] = tracez.NewCollector("debug", 100)
        // Immediate export for development
        setupPeriodicExport(collectors["debug"], 5*time.Second)
        
    default:
        log.Printf("Unknown environment '%s', using development config", env)
        collectors["debug"] = tracez.NewCollector("debug", 100)
        setupPeriodicExport(collectors["debug"], 5*time.Second)
    }
    
    for name, collector := range collectors {
        tracer.AddCollector(name, collector)
    }
    
    return tracer, collectors
}
```

### Configuration Validation

```go
func validateConfiguration(collectors map[string]*tracez.Collector) error {
    totalMemoryMB := 0.0
    
    for name, collector := range collectors {
        // Estimate memory usage (assuming 500 bytes per span average)
        bufferSize := collector.BufferSize() // Would need to expose this
        memoryMB := float64(bufferSize * 500) / (1024 * 1024)
        totalMemoryMB += memoryMB
        
        log.Printf("Collector %s: buffer=%d spans, memory=%.1fMB", 
            name, bufferSize, memoryMB)
    }
    
    log.Printf("Total tracing memory: %.1fMB", totalMemoryMB)
    
    // Warn if memory usage is high
    if totalMemoryMB > 100 {
        log.Printf("WARNING: High tracing memory usage (%.1fMB)", totalMemoryMB)
    }
    
    return nil
}
```

## Integration with Monitoring Systems

### OpenTelemetry Bridge

```go
func bridgeToOTel(spans []tracez.Span) {
    for _, span := range spans {
        // Convert tracez span to OpenTelemetry format
        otelSpan := convertToOTelSpan(span)
        // Send to OpenTelemetry collector
        sendToOTelCollector(otelSpan)
    }
}
```

### Jaeger Integration

```go
func sendToJaeger(spans []tracez.Span) error {
    jaegerSpans := make([]*jaeger.Span, 0, len(spans))
    
    for _, span := range spans {
        jaegerSpan := &jaeger.Span{
            TraceID:       convertTraceID(span.TraceID),
            SpanID:        convertSpanID(span.SpanID),
            ParentSpanID:  convertSpanID(span.ParentID),
            OperationName: span.Name,
            StartTime:     span.StartTime,
            Duration:      span.Duration,
            Tags:          convertTags(span.Tags),
        }
        jaegerSpans = append(jaegerSpans, jaegerSpan)
    }
    
    return jaegerClient.EmitBatch(jaegerSpans)
}
```

## Performance Tuning

### High-Throughput Optimization

```go
// Use multiple collectors to reduce contention
func setupHighThroughput() {
    tracer := tracez.New("high-throughput-service")
    
    // Shard collectors by operation type
    for i := 0; i < 4; i++ {
        collectorName := fmt.Sprintf("shard-%d", i)
        collector := tracez.NewCollector(collectorName, 25000)
        tracer.AddCollector(collectorName, collector)
        
        // Stagger export times to avoid all collectors exporting simultaneously
        exportDelay := time.Duration(i) * 2 * time.Second
        setupDelayedPeriodicExport(collector, 10*time.Second, exportDelay)
    }
}

func setupDelayedPeriodicExport(collector *tracez.Collector, interval, delay time.Duration) {
    go func() {
        time.Sleep(delay) // Initial delay
        
        ticker := time.NewTicker(interval)
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                spans := collector.Export()
                if len(spans) > 0 {
                    go sendToMonitoring(spans) // Async export
                }
            }
        }
    }()
}
```

### Memory Optimization

```go
// Reduce memory by limiting tag sizes
func sanitizeSpan(span *tracez.Span) {
    for key, value := range span.Tags {
        strValue := fmt.Sprintf("%v", value)
        
        // Truncate long values
        if len(strValue) > 1000 {
            span.Tags[key] = strValue[:1000] + "..."
        }
        
        // Remove empty values
        if strValue == "" {
            delete(span.Tags, key)
        }
    }
}
```

## Troubleshooting Production Issues

### Common Problems

1. **High memory usage**: Reduce buffer sizes or export more frequently
2. **Dropped spans**: Increase buffer size or reduce span creation rate
3. **Export failures**: Add retry logic and dead letter queues
4. **Performance impact**: Use sampling or selective tracing

### Debug Mode

```go
func enableDebugMode(tracer *tracez.Tracer) {
    debugCollector := tracez.NewCollector("debug", 1000)
    tracer.AddCollector("debug", debugCollector)
    
    // Export immediately for debugging
    go func() {
        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()
        
        for {
            select {
            case <-ticker.C:
                spans := debugCollector.Export()
                for _, span := range spans {
                    log.Printf("DEBUG SPAN: %s (%v) %+v", 
                        span.Name, span.Duration, span.Tags)
                }
            }
        }
    }()
}
```

## Next Steps

After deploying to production:

- **[Performance Guide](performance.md)**: Optimize tracing overhead
- **[Sampling Patterns](../patterns/sampling.md)**: Reduce trace volume
- **[Troubleshooting Guide](../reference/troubleshooting.md)**: Debug production issues
- **[Architecture Reference](../reference/architecture.md)**: Understand internal behavior