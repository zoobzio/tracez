# Instrumentation Guide

Step-by-step guide to adding tracez tracing to your Go services.

## Decision Tree: What Should I Trace?

Start here to determine your tracing strategy:

### High-Value Operations (Always Trace)
- **HTTP requests**: Every request should create a root span
- **Database queries**: Individual queries and transactions  
- **External API calls**: Service-to-service communication
- **Long-running operations**: Anything > 10ms typically
- **Error-prone operations**: Authentication, validation, parsing

### Medium-Value Operations (Context-Dependent)  
- **Business logic functions**: When they contain complexity
- **Cache operations**: If cache performance matters
- **File I/O**: Reading configuration, processing files
- **Queue operations**: Message publishing/consuming

### Low-Value Operations (Usually Skip)
- **Pure functions**: Math, string manipulation, formatting
- **Simple getters/setters**: Property access
- **Constants and lookups**: Static data access
- **Logging statements**: Already instrumented

## Basic Service Instrumentation

### Step 1: Initialize Tracer

```go
package main

import (
    "context"
    "log"
    "net/http"
    
    "github.com/zoobzio/tracez"
)

var tracer *tracez.Tracer

func init() {
    // Initialize tracer for your service
    tracer = tracez.New("user-service")
    
    // Add collector with appropriate buffer size
    collector := tracez.NewCollector("main", 1000)
    tracer.AddCollector("main", collector)
    
    // Export spans periodically (production setup)
    go exportPeriodically(collector)
}

func exportPeriodically(collector *tracez.Collector) {
    ticker := time.NewTicker(5 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            spans := collector.Export()
            if len(spans) > 0 {
                // Send to your monitoring system
                log.Printf("Exported %d spans", len(spans))
                processSpans(spans)
            }
        }
    }
}
```

### Step 2: HTTP Middleware (Essential)

Every web service needs HTTP request tracing:

```go
func TracingMiddleware(tracer *tracez.Tracer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create root span for this request
            ctx, span := tracer.StartSpan(r.Context(), "http.request")
            span.SetTag("http.method", r.Method)
            span.SetTag("http.path", r.URL.Path)
            span.SetTag("http.user_agent", r.UserAgent())
            defer span.Finish()
            
            // Wrap response writer to capture status
            wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}
            
            // Pass traced context to handlers
            next.ServeHTTP(wrapped, r.WithContext(ctx))
            
            // Tag response details
            span.SetTag("http.status_code", strconv.Itoa(wrapped.statusCode))
            
            // Tag errors for filtering
            if wrapped.statusCode >= 400 {
                span.SetTag("error.http", "true")
            }
        })
    }
}

type responseWriter struct {
    http.ResponseWriter
    statusCode int
}

func (w *responseWriter) WriteHeader(code int) {
    w.statusCode = code
    w.ResponseWriter.WriteHeader(code)
}

// Apply middleware to your server
func main() {
    mux := http.NewServeMux()
    
    // Add handlers
    mux.HandleFunc("/users", handleUsers)
    mux.HandleFunc("/orders", handleOrders)
    
    // Apply tracing middleware
    tracedMux := TracingMiddleware(tracer)(mux)
    
    log.Fatal(http.ListenAndServe(":8080", tracedMux))
}
```

### Step 3: Handler Instrumentation

Trace business logic within your handlers:

```go
func handleUsers(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context() // Already has trace context from middleware
    
    // Parse request
    userID := r.URL.Query().Get("id")
    if userID == "" {
        http.Error(w, "user_id required", 400)
        return
    }
    
    // Trace business operation
    user, err := getUserWithProfile(ctx, userID)
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }
    
    // Return response
    json.NewEncoder(w).Encode(user)
}

func getUserWithProfile(ctx context.Context, userID string) (*User, error) {
    // Create span for business operation
    _, span := tracer.StartSpan(ctx, "user.get-with-profile")
    span.SetTag("user.id", userID)
    defer span.Finish()
    
    // Get user from database (traced)
    user, err := getUserFromDB(ctx, userID)
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "database")
        return nil, err
    }
    
    // Get profile from external service (traced)  
    profile, err := getProfileFromAPI(ctx, userID)
    if err != nil {
        // Log error but continue - profile is optional
        span.SetTag("profile.error", err.Error())
        span.SetTag("profile.available", "false")
    } else {
        span.SetTag("profile.available", "true")
        user.Profile = profile
    }
    
    return user, nil
}
```

## Database Instrumentation

### Step 4: Database Query Tracing

Wrap your database operations to trace queries:

```go
func getUserFromDB(ctx context.Context, userID string) (*User, error) {
    _, span := tracer.StartSpan(ctx, "db.get-user")
    span.SetTag("db.table", "users")
    span.SetTag("db.operation", "SELECT")
    span.SetTag("user.id", userID)
    defer span.Finish()
    
    query := "SELECT id, name, email FROM users WHERE id = ?"
    span.SetTag("db.statement", query)
    
    start := time.Now()
    row := db.QueryRowContext(ctx, query, userID)
    
    var user User
    err := row.Scan(&user.ID, &user.Name, &user.Email)
    duration := time.Since(start)
    
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        if err == sql.ErrNoRows {
            span.SetTag("error.type", "not_found")
        } else {
            span.SetTag("error.type", "database")
        }
        return nil, err
    }
    
    return &user, nil
}

// Generic query tracer for reuse
func traceQuery(ctx context.Context, name, query string, args ...interface{}) *tracez.ActiveSpan {
    _, span := tracer.StartSpan(ctx, name)
    span.SetTag("db.statement", query)
    span.SetTag("db.args_count", strconv.Itoa(len(args)))
    return span
}

func executeTracedQuery(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
    span := traceQuery(ctx, "db.exec", query, args...)
    defer span.Finish()
    
    start := time.Now()
    result, err := db.ExecContext(ctx, query, args...)
    duration := time.Since(start)
    
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        return nil, err
    }
    
    if rows, err := result.RowsAffected(); err == nil {
        span.SetTag("db.rows_affected", strconv.FormatInt(rows, 10))
    }
    
    return result, nil
}
```

## External Service Integration

### Step 5: HTTP Client Tracing

Trace calls to external APIs:

```go
func getProfileFromAPI(ctx context.Context, userID string) (*Profile, error) {
    _, span := tracer.StartSpan(ctx, "external.profile-service")
    span.SetTag("api.service", "profile-service")
    span.SetTag("api.method", "GET")
    span.SetTag("user.id", userID)
    defer span.Finish()
    
    // Create request with timeout
    url := fmt.Sprintf("http://profile-service/users/%s/profile", userID)
    span.SetTag("api.url", url)
    
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "request_creation")
        return nil, err
    }
    
    // Add trace headers for distributed tracing
    addTraceHeaders(req, ctx)
    
    client := &http.Client{Timeout: 10 * time.Second}
    
    start := time.Now()
    resp, err := client.Do(req)
    duration := time.Since(start)
    
    span.SetTag("api.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        if isTimeout(err) {
            span.SetTag("error.type", "timeout")
        } else {
            span.SetTag("error.type", "network")
        }
        return nil, err
    }
    defer resp.Body.Close()
    
    span.SetTag("api.status_code", strconv.Itoa(resp.StatusCode))
    
    if resp.StatusCode >= 400 {
        span.SetTag("error.http", "true")
        span.SetTag("error.type", "http_error")
        return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
    }
    
    var profile Profile
    if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "decode")
        return nil, err
    }
    
    return &profile, nil
}

// Add trace context to outbound requests
func addTraceHeaders(req *http.Request, ctx context.Context) {
    // Extract trace info from context
    if span := tracez.SpanFromContext(ctx); span != nil {
        req.Header.Set("X-Trace-ID", span.TraceID)
        req.Header.Set("X-Parent-Span-ID", span.SpanID)
    }
}

func isTimeout(err error) bool {
    if netErr, ok := err.(net.Error); ok {
        return netErr.Timeout()
    }
    return false
}
```

## Background Job Processing

### Step 6: Worker Pool Tracing

Trace background jobs and maintain context across goroutines:

```go
type TracedJob struct {
    Job Job
    Ctx context.Context
}

func processJobs(jobs <-chan Job) {
    tracedJobs := make(chan TracedJob, 100)
    
    // Convert jobs to traced jobs
    go func() {
        for job := range jobs {
            // Create trace for job processing
            ctx, span := tracer.StartSpan(context.Background(), "job.queued")
            span.SetTag("job.id", job.ID)
            span.SetTag("job.type", job.Type)
            span.Finish() // Queued span completed
            
            tracedJobs <- TracedJob{Job: job, Ctx: ctx}
        }
    }()
    
    // Start worker pool
    for i := 0; i < 10; i++ {
        go worker(i, tracedJobs)
    }
}

func worker(workerID int, jobs <-chan TracedJob) {
    for tracedJob := range jobs {
        // Process job with inherited context
        processJob(tracedJob.Ctx, workerID, tracedJob.Job)
    }
}

func processJob(ctx context.Context, workerID int, job Job) {
    _, span := tracer.StartSpan(ctx, "job.process")
    span.SetTag("job.id", job.ID)
    span.SetTag("job.type", job.Type)
    span.SetTag("worker.id", strconv.Itoa(workerID))
    defer span.Finish()
    
    start := time.Now()
    
    switch job.Type {
    case "email":
        err := sendEmail(ctx, job.Data)
        if err != nil {
            span.SetTag("error", err.Error())
            span.SetTag("error.type", "email_failed")
        }
    case "analytics":
        err := processAnalytics(ctx, job.Data)
        if err != nil {
            span.SetTag("error", err.Error())
            span.SetTag("error.type", "analytics_failed")
        }
    default:
        span.SetTag("error", "unknown job type")
        span.SetTag("error.type", "unsupported_job")
    }
    
    duration := time.Since(start)
    span.SetTag("job.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
}
```

## Error Handling Patterns

### Step 7: Structured Error Tracing

Tag errors consistently for analysis:

```go
func processPayment(ctx context.Context, payment Payment) error {
    _, span := tracer.StartSpan(ctx, "payment.process")
    span.SetTag("payment.amount", fmt.Sprintf("%.2f", payment.Amount))
    span.SetTag("payment.method", payment.Method)
    defer span.Finish()
    
    // Validation
    if err := validatePayment(payment); err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "validation")
        span.SetTag("error.field", extractFieldFromError(err))
        return err
    }
    
    // External service call
    result, err := chargeCard(ctx, payment)
    if err != nil {
        span.SetTag("error", err.Error())
        if isRetryableError(err) {
            span.SetTag("error.type", "retryable")
            span.SetTag("retry.recommended", "true")
        } else {
            span.SetTag("error.type", "permanent")
            span.SetTag("retry.recommended", "false")
        }
        return err
    }
    
    span.SetTag("transaction.id", result.TransactionID)
    span.SetTag("result", "success")
    return nil
}

// Consistent error categorization
func tagError(span *tracez.ActiveSpan, err error, category string) {
    span.SetTag("error", err.Error())
    span.SetTag("error.type", category)
    span.SetTag("error.timestamp", time.Now().UTC().Format(time.RFC3339))
}
```

## Production Considerations

### Performance Impact
- **Minimal overhead**: ~344 bytes per span, ~8 allocations
- **Non-blocking**: Full collectors drop spans, don't block
- **Thread-safe**: All operations safe for concurrent use

### Buffer Sizing
```go
// Development: Small buffer for immediate feedback
collector := tracez.NewCollector("dev", 100)

// Production: Larger buffer for batch efficiency
collector := tracez.NewCollector("prod", 10000)

// High-throughput: Multiple collectors
mainCollector := tracez.NewCollector("main", 10000)
errorCollector := tracez.NewCollector("errors", 1000)  // Smaller, more frequent export
```

### Monitoring Collector Health
```go
func monitorCollectors() {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            if dropped := collector.DroppedCount(); dropped > 0 {
                log.Printf("WARNING: Dropped %d spans due to buffer overflow", dropped)
                // Consider increasing buffer size or export frequency
            }
        }
    }
}
```

## Next Steps

After instrumenting your service:

1. **[Context Propagation Guide](context-propagation.md)**: Ensure traces flow correctly across goroutines
2. **[Performance Guide](performance.md)**: Optimize tracing overhead for production  
3. **[Production Guide](production.md)**: Deploy and monitor tracing in production
4. **[HTTP Tracing Patterns](../patterns/http-tracing.md)**: Advanced web service patterns
5. **[Database Tracing Patterns](../patterns/database-tracing.md)**: Query optimization techniques

## Common Instrumentation Mistakes

### Mistake 1: Not Using Context from StartSpan
```go
// ✗ Wrong - spans won't be linked
ctx, span := tracer.StartSpan(r.Context(), "parent")
_, childSpan := tracer.StartSpan(context.Background(), "child")

// ✓ Correct - child links to parent  
ctx, span := tracer.StartSpan(r.Context(), "parent")
_, childSpan := tracer.StartSpan(ctx, "child")
```

### Mistake 2: Over-Instrumenting
```go
// ✗ Wrong - too granular, no value
_, span := tracer.StartSpan(ctx, "format.string")
result := fmt.Sprintf("Hello %s", name)
span.Finish()

// ✓ Correct - trace meaningful operations
_, span := tracer.StartSpan(ctx, "user.authenticate")
user, err := validateCredentials(ctx, credentials)
span.Finish()
```

### Mistake 3: Missing Error Context
```go
// ✗ Wrong - error without context
if err != nil {
    span.SetTag("error", err.Error())
    return err
}

// ✓ Correct - structured error tagging
if err != nil {
    span.SetTag("error", err.Error())
    span.SetTag("error.type", "validation")
    span.SetTag("error.field", "email")
    return err
}
```

Start with HTTP middleware and database tracing - these provide the highest value with minimal effort.