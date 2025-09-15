# Sampling Patterns

Problem-focused solutions for reducing trace volume while preserving critical observability.

## Problem: High-Volume Services Overwhelming Trace Storage

**Symptoms**: Trace collectors constantly dropping spans, export systems overloaded, storage costs escalating.

**Solution**: Intelligent sampling strategies to reduce volume while maintaining coverage.

## Random Sampling

**When to use**: Need uniform sampling across all operations to reduce overall volume by a fixed percentage.

```go
import (
    "math/rand"
    "net/http"
    "strconv"
    
    "github.com/zoobzio/tracez"
)

func RandomSamplingMiddleware(tracer *tracez.Tracer, sampleRate float64) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Sample decision at request entry
            if rand.Float64() < sampleRate {
                // Trace this request
                ctx, span := tracer.StartSpan(r.Context(), "http.request")
                span.SetTag("http.method", r.Method)
                span.SetTag("http.path", r.URL.Path)
                span.SetTag("sampling.strategy", "random")
                span.SetTag("sampling.rate", fmt.Sprintf("%.2f", sampleRate))
                span.SetTag("sampled", "true")
                defer span.Finish()
                
                next.ServeHTTP(w, r.WithContext(ctx))
            } else {
                // Skip tracing entirely
                next.ServeHTTP(w, r)
            }
        })
    }
}

// Usage examples
func setupRandomSampling() {
    tracer := tracez.New("web-service")
    
    // Sample 10% of requests uniformly
    middleware := RandomSamplingMiddleware(tracer, 0.1)
    
    mux := http.NewServeMux()
    mux.HandleFunc("/", handler)
    
    // Apply sampling middleware
    http.ListenAndServe(":8080", middleware(mux))
}
```

**Analysis:**
```go
func analyzeRandomSampling(spans []tracez.Span, expectedRate float64) {
    sampled := 0
    total := 0
    
    for _, span := range spans {
        if span.Name == "http.request" {
            total++
            if span.Tags["sampled"] == "true" {
                sampled++
            }
        }
    }
    
    actualRate := float64(sampled) / float64(total)
    fmt.Printf("Random sampling analysis:\n")
    fmt.Printf("  Expected rate: %.1f%%\n", expectedRate*100)
    fmt.Printf("  Actual rate: %.1f%%\n", actualRate*100)
    fmt.Printf("  Sampled spans: %d/%d\n", sampled, total)
}
```

**Trade-offs:**
- ✅ Simple to implement and understand
- ✅ Uniform distribution over time
- ✅ Predictable volume reduction
- ❌ May miss critical error conditions
- ❌ No consideration of operation importance

## Rate-Limited Sampling

**When to use**: Need consistent trace throughput regardless of request volume spikes.

```go
import "golang.org/x/time/rate"

type RateLimitedSampler struct {
    tracer  *tracez.Tracer
    limiter *rate.Limiter
    name    string
}

func NewRateLimitedSampler(tracer *tracez.Tracer, tracesPerSecond int, name string) *RateLimitedSampler {
    return &RateLimitedSampler{
        tracer:  tracer,
        limiter: rate.NewLimiter(rate.Limit(tracesPerSecond), tracesPerSecond*2), // 2x burst
        name:    name,
    }
}

func (rls *RateLimitedSampler) ShouldSample() bool {
    return rls.limiter.Allow()
}

func (rls *RateLimitedSampler) StartSpan(ctx context.Context, operation string) (context.Context, *tracez.ActiveSpan) {
    if rls.ShouldSample() {
        ctx, span := rls.tracer.StartSpan(ctx, operation)
        span.SetTag("sampling.strategy", "rate_limited")
        span.SetTag("sampling.limiter", rls.name)
        span.SetTag("sampled", "true")
        return ctx, span
    }
    
    // Return original context without span
    return ctx, nil
}

// Usage in HTTP middleware
func RateLimitedSamplingMiddleware(sampler *RateLimitedSampler) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := sampler.StartSpan(r.Context(), "http.request")
            
            if span != nil {
                // Request is being traced
                span.SetTag("http.method", r.Method)
                span.SetTag("http.path", r.URL.Path)
                defer span.Finish()
                next.ServeHTTP(w, r.WithContext(ctx))
            } else {
                // Request not traced
                next.ServeHTTP(w, r)
            }
        })
    }
}

// Production setup
func setupRateLimitedSampling() {
    tracer := tracez.New("api-service")
    
    // Allow maximum 100 traces per second
    sampler := NewRateLimitedSampler(tracer, 100, "http-requests")
    middleware := RateLimitedSamplingMiddleware(sampler)
    
    // Multiple rate limiters for different operations
    dbSampler := NewRateLimitedSampler(tracer, 50, "database-ops")
    apiSampler := NewRateLimitedSampler(tracer, 25, "external-apis")
}
```

**Benefits:**
- ✅ Consistent trace volume under load spikes
- ✅ Prevents trace system overload
- ✅ Configurable per operation type
- ❌ May miss events during high-traffic periods

## Priority-Based Sampling

**When to use**: Different operations have different importance levels for observability.

```go
type PrioritySampler struct {
    tracer      *tracez.Tracer
    priorities  map[string]float64 // Operation -> sample rate
    defaultRate float64
    mu          sync.RWMutex
}

func NewPrioritySampler(tracer *tracez.Tracer, defaultRate float64) *PrioritySampler {
    return &PrioritySampler{
        tracer:      tracer,
        priorities:  make(map[string]float64),
        defaultRate: defaultRate,
    }
}

func (ps *PrioritySampler) SetPriority(operation string, sampleRate float64) {
    ps.mu.Lock()
    defer ps.mu.Unlock()
    ps.priorities[operation] = sampleRate
}

func (ps *PrioritySampler) GetSampleRate(operation string) float64 {
    ps.mu.RLock()
    defer ps.mu.RUnlock()
    
    if rate, exists := ps.priorities[operation]; exists {
        return rate
    }
    return ps.defaultRate
}

func (ps *PrioritySampler) StartSpan(ctx context.Context, operation string) (context.Context, *tracez.ActiveSpan) {
    sampleRate := ps.GetSampleRate(operation)
    
    if rand.Float64() < sampleRate {
        ctx, span := ps.tracer.StartSpan(ctx, operation)
        span.SetTag("sampling.strategy", "priority_based")
        span.SetTag("sampling.rate", fmt.Sprintf("%.2f", sampleRate))
        span.SetTag("operation.priority", ps.getPriorityLevel(sampleRate))
        return ctx, span
    }
    
    return ctx, nil
}

func (ps *PrioritySampler) getPriorityLevel(rate float64) string {
    switch {
    case rate >= 1.0:
        return "critical"
    case rate >= 0.5:
        return "high"  
    case rate >= 0.1:
        return "medium"
    default:
        return "low"
    }
}

// Production configuration
func setupPrioritySampling() {
    tracer := tracez.New("payment-service")
    sampler := NewPrioritySampler(tracer, 0.01) // 1% default
    
    // Critical operations - trace everything
    sampler.SetPriority("payment.process", 1.0)
    sampler.SetPriority("fraud.check", 1.0)
    sampler.SetPriority("auth.login", 1.0)
    
    // Important operations - high sampling
    sampler.SetPriority("user.create", 0.5)
    sampler.SetPriority("order.create", 0.5)
    
    // Normal operations - medium sampling  
    sampler.SetPriority("user.profile", 0.1)
    sampler.SetPriority("product.search", 0.05)
    
    // Low priority - minimal sampling
    sampler.SetPriority("health.check", 0.0)
    sampler.SetPriority("metrics.collect", 0.0)
}
```

## Error-Biased Sampling

**When to use**: Want to ensure all errors are traced while sampling successful operations.

```go
type ErrorBiasedSampler struct {
    tracer      *tracez.Tracer
    successRate float64 // Sample rate for successful operations
}

func NewErrorBiasedSampler(tracer *tracez.Tracer, successRate float64) *ErrorBiasedSampler {
    return &ErrorBiasedSampler{
        tracer:      tracer,
        successRate: successRate,
    }
}

// Custom span wrapper that decides whether to emit based on final state
type ConditionalSpan struct {
    span       *tracez.ActiveSpan
    sampler    *ErrorBiasedSampler
    shouldEmit bool
    hasError   bool
}

func (ebs *ErrorBiasedSampler) StartSpan(ctx context.Context, operation string) (context.Context, *ConditionalSpan) {
    ctx, span := ebs.tracer.StartSpan(ctx, operation)
    
    // Pre-decide for successful operations
    shouldEmit := rand.Float64() < ebs.successRate
    
    conditional := &ConditionalSpan{
        span:       span,
        sampler:    ebs,
        shouldEmit: shouldEmit,
        hasError:   false,
    }
    
    span.SetTag("sampling.strategy", "error_biased")
    
    return ctx, conditional
}

func (cs *ConditionalSpan) SetTag(key, value string) {
    cs.span.SetTag(key, value)
    
    // If we see an error, always emit this span
    if key == "error" && value != "" {
        cs.hasError = true
        cs.shouldEmit = true
        cs.span.SetTag("sampling.reason", "error_detected")
    }
}

func (cs *ConditionalSpan) GetTag(key string) (string, bool) {
    return cs.span.GetTag(key)
}

func (cs *ConditionalSpan) Finish() {
    // Tag the final decision
    if cs.hasError {
        cs.span.SetTag("sampling.decision", "emit_error")
    } else if cs.shouldEmit {
        cs.span.SetTag("sampling.decision", "emit_success")
        cs.span.SetTag("sampling.success_rate", fmt.Sprintf("%.2f", cs.sampler.successRate))
    } else {
        cs.span.SetTag("sampling.decision", "drop_success")
        // Don't call span.Finish() - let it be garbage collected
        return
    }
    
    cs.span.Finish()
}

// HTTP middleware with error bias
func ErrorBiasedMiddleware(sampler *ErrorBiasedSampler) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := sampler.StartSpan(r.Context(), "http.request")
            span.SetTag("http.method", r.Method)
            span.SetTag("http.path", r.URL.Path)
            defer span.Finish()
            
            // Wrap response writer to detect errors
            wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}
            next.ServeHTTP(wrapped, r.WithContext(ctx))
            
            // Tag response status
            span.SetTag("http.status_code", strconv.Itoa(wrapped.statusCode))
            
            // Mark as error if status >= 400
            if wrapped.statusCode >= 400 {
                span.SetTag("error", fmt.Sprintf("HTTP %d", wrapped.statusCode))
            }
        })
    }
}
```

## Adaptive Sampling

**When to use**: Want automatic sampling adjustment based on system load.

```go
type AdaptiveSampler struct {
    tracer         *tracez.Tracer
    collector      *tracez.Collector
    baseSampleRate float64
    maxSampleRate  float64
    currentRate    float64
    lastCheck      time.Time
    adjustment     sync.RWMutex
}

func NewAdaptiveSampler(tracer *tracez.Tracer, collector *tracez.Collector) *AdaptiveSampler {
    return &AdaptiveSampler{
        tracer:         tracer,
        collector:      collector,
        baseSampleRate: 0.1,    // 10% baseline
        maxSampleRate:  1.0,    // 100% maximum
        currentRate:    0.1,    // Start at baseline
        lastCheck:      time.Now(),
    }
}

func (as *AdaptiveSampler) adjustSampleRate() {
    now := time.Now()
    if now.Sub(as.lastCheck) < 30*time.Second {
        return // Don't adjust too frequently
    }
    
    as.adjustment.Lock()
    defer as.adjustment.Unlock()
    
    // Check collector pressure
    droppedCount := as.collector.DroppedCount()
    
    if droppedCount > 0 {
        // System under pressure - reduce sampling
        as.currentRate = as.currentRate * 0.8 // Reduce by 20%
        if as.currentRate < 0.01 {
            as.currentRate = 0.01 // Minimum 1%
        }
        log.Printf("Reduced sampling rate to %.2f%% due to %d dropped spans", 
            as.currentRate*100, droppedCount)
    } else {
        // System healthy - cautiously increase sampling
        as.currentRate = as.currentRate * 1.1 // Increase by 10%
        if as.currentRate > as.maxSampleRate {
            as.currentRate = as.maxSampleRate
        }
    }
    
    as.lastCheck = now
}

func (as *AdaptiveSampler) StartSpan(ctx context.Context, operation string) (context.Context, *tracez.ActiveSpan) {
    as.adjustSampleRate()
    
    as.adjustment.RLock()
    currentRate := as.currentRate
    as.adjustment.RUnlock()
    
    if rand.Float64() < currentRate {
        ctx, span := as.tracer.StartSpan(ctx, operation)
        span.SetTag("sampling.strategy", "adaptive")
        span.SetTag("sampling.current_rate", fmt.Sprintf("%.3f", currentRate))
        return ctx, span
    }
    
    return ctx, nil
}

// Monitor and adjust sampling continuously
func (as *AdaptiveSampler) StartMonitoring() {
    ticker := time.NewTicker(30 * time.Second)
    go func() {
        defer ticker.Stop()
        for range ticker.C {
            as.adjustSampleRate()
        }
    }()
}
```

## Multi-Strategy Sampling

**When to use**: Combine multiple sampling strategies for comprehensive coverage.

```go
type MultiSampler struct {
    tracer    *tracez.Tracer
    strategies map[string]SamplingStrategy
}

type SamplingStrategy interface {
    ShouldSample(ctx context.Context, operation string) (bool, string) // bool + reason
}

func NewMultiSampler(tracer *tracez.Tracer) *MultiSampler {
    return &MultiSampler{
        tracer:     tracer,
        strategies: make(map[string]SamplingStrategy),
    }
}

func (ms *MultiSampler) AddStrategy(name string, strategy SamplingStrategy) {
    ms.strategies[name] = strategy
}

func (ms *MultiSampler) StartSpan(ctx context.Context, operation string) (context.Context, *tracez.ActiveSpan) {
    // Try strategies in priority order
    for name, strategy := range ms.strategies {
        if shouldSample, reason := strategy.ShouldSample(ctx, operation); shouldSample {
            ctx, span := ms.tracer.StartSpan(ctx, operation)
            span.SetTag("sampling.strategy", name)
            span.SetTag("sampling.reason", reason)
            return ctx, span
        }
    }
    
    // No strategy decided to sample
    return ctx, nil
}

// Production setup with multiple strategies
func setupMultiSampling() {
    tracer := tracez.New("production-service")
    multiSampler := NewMultiSampler(tracer)
    
    // Priority 1: Always sample errors (from headers or previous context)
    multiSampler.AddStrategy("error_guaranteed", &ErrorGuaranteedStrategy{})
    
    // Priority 2: Critical operations get high sampling
    multiSampler.AddStrategy("critical_ops", &PriorityStrategy{
        criticalOps: []string{"payment", "auth", "fraud"},
        rate: 1.0,
    })
    
    // Priority 3: Rate-limited sampling for everything else
    multiSampler.AddStrategy("rate_limited", &RateLimitStrategy{
        limiter: rate.NewLimiter(100, 200), // 100/sec with 200 burst
    })
    
    // Priority 4: Random fallback sampling
    multiSampler.AddStrategy("random_fallback", &RandomStrategy{
        rate: 0.01, // 1% fallback
    })
}
```

## Sampling Analysis

### Volume Reduction Analysis
```go
func analyzeSamplingEffectiveness(originalSpans, sampledSpans []tracez.Span) {
    reduction := float64(len(originalSpans)-len(sampledSpans)) / float64(len(originalSpans))
    
    fmt.Printf("Sampling Analysis:\n")
    fmt.Printf("  Original spans: %d\n", len(originalSpans))
    fmt.Printf("  Sampled spans: %d\n", len(sampledSpans))
    fmt.Printf("  Volume reduction: %.1f%%\n", reduction*100)
    
    // Analyze by strategy
    strategyBreakdown := make(map[string]int)
    for _, span := range sampledSpans {
        if strategy, ok := span.Tags["sampling.strategy"]; ok {
            strategyBreakdown[strategy]++
        }
    }
    
    fmt.Printf("\nSampling by strategy:\n")
    for strategy, count := range strategyBreakdown {
        pct := float64(count) / float64(len(sampledSpans)) * 100
        fmt.Printf("  %s: %d (%.1f%%)\n", strategy, count, pct)
    }
}
```

### Error Coverage Analysis
```go
func analyzeErrorCoverage(originalErrors, sampledErrors []tracez.Span) {
    errorCoverage := float64(len(sampledErrors)) / float64(len(originalErrors))
    
    fmt.Printf("Error Coverage Analysis:\n")
    fmt.Printf("  Total errors: %d\n", len(originalErrors))
    fmt.Printf("  Sampled errors: %d\n", len(sampledErrors))
    fmt.Printf("  Error coverage: %.1f%%\n", errorCoverage*100)
    
    if errorCoverage < 0.95 {
        fmt.Printf("  ⚠️  Error coverage below 95%% - consider error-biased sampling\n")
    }
}
```

## Best Practices

### Sampling Decision Hierarchy
1. **Always sample**: Errors, critical operations, debug requests
2. **High sampling**: Important business operations, authentication
3. **Medium sampling**: Regular API calls, database operations
4. **Low sampling**: Health checks, metrics collection, static assets
5. **Never sample**: Internal diagnostics, load balancer probes

### Configuration Management
```go
type SamplingConfig struct {
    Strategy      string             `json:"strategy"`
    DefaultRate   float64           `json:"default_rate"`
    Priorities    map[string]float64 `json:"priorities"`
    RateLimit     int               `json:"rate_limit"`
    ErrorBias     bool              `json:"error_bias"`
}

func loadSamplingConfig(configPath string) (*SamplingConfig, error) {
    // Load from file, environment, or config service
    // Allow hot reloading in production
}
```

### Monitoring Sampling Health
```go
func monitorSamplingHealth(sampler Sampler) {
    ticker := time.NewTicker(60 * time.Second)
    go func() {
        for range ticker.C {
            // Monitor sampling rates by operation
            // Alert on unexpected changes
            // Track error coverage percentage
        }
    }()
}
```

## Next Steps

- **[Production Guide](../guides/production.md)**: Deploy sampling in production
- **[Performance Guide](../guides/performance.md)**: Optimize sampling overhead
- **[HTTP Tracing Patterns](http-tracing.md)**: Web service sampling strategies
- **[Error Handling Patterns](error-handling.md)**: Ensure error visibility with sampling