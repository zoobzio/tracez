# Context Propagation Guide

Comprehensive guide to ensuring trace context flows correctly through goroutines, service calls, and complex execution paths.

## The Context Propagation Rule

**Golden Rule**: Always use the context returned by `StartSpan()` for child operations.

```go
// ✓ Correct - child span automatically linked
ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")
defer parentSpan.Finish()

_, childSpan := tracer.StartSpan(ctx, "child") // Uses parent's context
defer childSpan.Finish()

// ✗ Wrong - spans will NOT be linked  
ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")
_, childSpan := tracer.StartSpan(context.Background(), "child") // Fresh context!
```

## Goroutine Context Propagation

### Problem: Context Lost Across Goroutine Boundaries

```go
// ✗ Wrong - trace context lost
func processJobs(ctx context.Context, jobs []Job) {
    for _, job := range jobs {
        go func(j Job) {
            // This goroutine has no trace context!
            processJob(j) // No tracing connection to parent
        }(job)
    }
}
```

### Solution 1: Explicit Context Passing

```go
// ✓ Correct - explicitly pass context
func processJobs(ctx context.Context, jobs []Job) {
    for _, job := range jobs {
        go func(ctx context.Context, j Job) { // Capture context
            // Create child span in goroutine
            _, span := tracer.StartSpan(ctx, "job.process")
            span.SetTag("job.id", j.ID)
            defer span.Finish()
            
            processJob(ctx, j) // Pass context to business logic
        }(ctx, job) // Pass context explicitly
    }
}
```

### Solution 2: Context in Data Structure

```go
type TracedJob struct {
    Job     Job
    Context context.Context
}

func processJobs(ctx context.Context, jobs []Job) {
    // Convert jobs to traced jobs
    tracedJobs := make([]TracedJob, len(jobs))
    for i, job := range jobs {
        tracedJobs[i] = TracedJob{
            Job:     job,
            Context: ctx, // Embed context
        }
    }
    
    // Process with context preserved
    for _, tracedJob := range tracedJobs {
        go func(tj TracedJob) {
            _, span := tracer.StartSpan(tj.Context, "job.process")
            span.SetTag("job.id", tj.Job.ID)
            defer span.Finish()
            
            processJob(tj.Context, tj.Job)
        }(tracedJob)
    }
}
```

### Solution 3: Worker Pool Pattern

```go
type Worker struct {
    id     int
    tracer *tracez.Tracer
    jobs   <-chan TracedJob
}

func (w *Worker) start() {
    for tracedJob := range w.jobs {
        // Each job maintains its trace context
        w.processJob(tracedJob.Context, tracedJob.Job)
    }
}

func (w *Worker) processJob(ctx context.Context, job Job) {
    _, span := w.tracer.StartSpan(ctx, "worker.process-job")
    span.SetTag("worker.id", strconv.Itoa(w.id))
    span.SetTag("job.id", job.ID)
    defer span.Finish()
    
    // Business logic with trace context
    if err := job.Execute(ctx); err != nil {
        span.SetTag("error", err.Error())
    }
}

// Setup worker pool
func setupWorkerPool(ctx context.Context, tracer *tracez.Tracer) chan<- TracedJob {
    jobs := make(chan TracedJob, 100)
    
    // Start workers
    for i := 0; i < 5; i++ {
        worker := &Worker{
            id:     i,
            tracer: tracer,
            jobs:   jobs,
        }
        go worker.start()
    }
    
    return jobs
}
```

## HTTP Service Context Propagation

### Inbound Request Tracing

```go
func TracingMiddleware(tracer *tracez.Tracer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Check for distributed trace headers
            traceID := r.Header.Get("X-Trace-ID")
            parentSpanID := r.Header.Get("X-Parent-Span-ID")
            
            var ctx context.Context
            var span *tracez.ActiveSpan
            
            if traceID != "" && parentSpanID != "" {
                // Continue existing trace
                ctx, span = tracer.StartSpanWithTrace(r.Context(), "http.request", traceID, parentSpanID)
                span.SetTag("trace.inherited", "true")
            } else {
                // Start new trace
                ctx, span = tracer.StartSpan(r.Context(), "http.request")
                span.SetTag("trace.root", "true")
            }
            
            span.SetTag("http.method", r.Method)
            span.SetTag("http.path", r.URL.Path)
            defer span.Finish()
            
            // Pass traced context to handler
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

### Outbound Request Tracing

```go
func TracedHTTPClient(tracer *tracez.Tracer) *http.Client {
    transport := &TracedTransport{
        tracer:    tracer,
        transport: http.DefaultTransport,
    }
    
    return &http.Client{
        Transport: transport,
        Timeout:   30 * time.Second,
    }
}

type TracedTransport struct {
    tracer    *tracez.Tracer
    transport http.RoundTripper
}

func (tt *TracedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    ctx := req.Context()
    _, span := tt.tracer.StartSpan(ctx, "http.client")
    span.SetTag("http.method", req.Method)
    span.SetTag("http.url", req.URL.String())
    defer span.Finish()
    
    // Add trace context to outbound headers
    if currentSpan := tracez.GetSpan(ctx); currentSpan != nil {
        req.Header.Set("X-Trace-ID", currentSpan.TraceID)
        req.Header.Set("X-Parent-Span-ID", currentSpan.SpanID)
    }
    
    // Execute request
    start := time.Now()
    resp, err := tt.transport.RoundTrip(req)
    duration := time.Since(start)
    
    span.SetTag("http.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        return nil, err
    }
    
    span.SetTag("http.status_code", strconv.Itoa(resp.StatusCode))
    if resp.StatusCode >= 400 {
        span.SetTag("error.http", "true")
    }
    
    return resp, nil
}

// Usage example
func callExternalAPI(ctx context.Context, url string) (*APIResponse, error) {
    client := TracedHTTPClient(tracer)
    
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        return nil, err
    }
    
    resp, err := client.Do(req) // Automatically traced
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()
    
    var apiResp APIResponse
    return &apiResp, json.NewDecoder(resp.Body).Decode(&apiResp)
}
```

## W3C Trace Context Standard

For interoperability with other tracing systems, implement W3C Trace Context headers alongside or instead of custom headers.

### W3C Headers Specification

```go
const (
    TraceParentHeader = "traceparent"  // Required: trace-id, parent-id, flags
    TraceStateHeader  = "tracestate"   // Optional: vendor-specific data
)

// W3C traceparent format: version-trace-id-parent-id-trace-flags
// Example: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
```

### W3C-Compatible Middleware

```go
func W3CTracingMiddleware(tracer *tracez.Tracer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            var ctx context.Context
            var span *tracez.ActiveSpan
            
            // Parse W3C traceparent header
            traceparent := r.Header.Get("traceparent")
            if traceContext := parseW3CTraceParent(traceparent); traceContext != nil {
                // Continue existing trace with W3C context
                ctx, span = tracer.StartSpanWithTrace(
                    r.Context(), 
                    "http.request", 
                    traceContext.TraceID, 
                    traceContext.ParentID,
                )
                span.SetTag("trace.w3c.inherited", "true")
                span.SetTag("trace.w3c.flags", traceContext.Flags)
            } else {
                // Check fallback custom headers
                traceID := r.Header.Get("X-Trace-ID")
                parentSpanID := r.Header.Get("X-Parent-Span-ID")
                
                if traceID != "" && parentSpanID != "" {
                    ctx, span = tracer.StartSpanWithTrace(r.Context(), "http.request", traceID, parentSpanID)
                    span.SetTag("trace.custom.inherited", "true")
                } else {
                    // Start new trace
                    ctx, span = tracer.StartSpan(r.Context(), "http.request")
                    span.SetTag("trace.root", "true")
                }
            }
            
            span.SetTag("http.method", r.Method)
            span.SetTag("http.path", r.URL.Path)
            defer span.Finish()
            
            // Pass traced context to handler
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// W3CTraceContext represents parsed W3C trace context
type W3CTraceContext struct {
    Version  string
    TraceID  string
    ParentID string
    Flags    string
}

// parseW3CTraceParent parses W3C traceparent header
// Format: version-trace-id-parent-id-trace-flags
func parseW3CTraceParent(traceparent string) *W3CTraceContext {
    if traceparent == "" {
        return nil
    }
    
    parts := strings.Split(traceparent, "-")
    if len(parts) != 4 {
        return nil
    }
    
    // Validate format
    if len(parts[0]) != 2 || len(parts[1]) != 32 || len(parts[2]) != 16 || len(parts[3]) != 2 {
        return nil
    }
    
    return &W3CTraceContext{
        Version:  parts[0],
        TraceID:  parts[1],
        ParentID: parts[2],
        Flags:    parts[3],
    }
}
```

### W3C-Compatible Outbound Client

```go
type W3CTracedTransport struct {
    tracer    *tracez.Tracer
    transport http.RoundTripper
}

func (wtt *W3CTracedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
    ctx := req.Context()
    _, span := wtt.tracer.StartSpan(ctx, "http.client")
    span.SetTag("http.method", req.Method)
    span.SetTag("http.url", req.URL.String())
    defer span.Finish()
    
    // Add W3C trace context to outbound headers
    if currentSpan := tracez.GetSpan(ctx); currentSpan != nil {
        // W3C traceparent format: version-traceid-spanid-flags
        traceparent := fmt.Sprintf("00-%s-%s-01", 
            currentSpan.TraceID, 
            currentSpan.SpanID,
        )
        req.Header.Set("traceparent", traceparent)
        
        // Also add custom headers for backward compatibility
        req.Header.Set("X-Trace-ID", currentSpan.TraceID)
        req.Header.Set("X-Parent-Span-ID", currentSpan.SpanID)
    }
    
    // Execute request with timing
    start := time.Now()
    resp, err := wtt.transport.RoundTrip(req)
    duration := time.Since(start)
    
    span.SetTag("http.duration.ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "network")
        return nil, err
    }
    
    span.SetTag("http.status.code", strconv.Itoa(resp.StatusCode))
    if resp.StatusCode >= 400 {
        span.SetTag("error.http", "true")
        span.SetTag("error.type", "http")
    }
    
    return resp, nil
}

// Create W3C-compatible HTTP client
func NewW3CClient(tracer *tracez.Tracer) *http.Client {
    return &http.Client{
        Transport: &W3CTracedTransport{
            tracer:    tracer,
            transport: http.DefaultTransport,
        },
        Timeout: 30 * time.Second,
    }
}
```

### Complete Service-to-Service Example

```go
// Service A: Making outbound call with W3C propagation
func CallServiceB(ctx context.Context, tracer *tracez.Tracer, userID string) (*UserProfile, error) {
    _, span := tracer.StartSpan(ctx, "service.call.user.profile")
    span.SetTag("service.target", "profile-service")
    span.SetTag("user.id", userID)
    defer span.Finish()
    
    client := NewW3CClient(tracer)
    
    // Create request with traced context
    url := fmt.Sprintf("https://profile-service/users/%s", userID)
    req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "request_creation")
        return nil, err
    }
    
    // W3C headers automatically added by transport
    resp, err := client.Do(req)
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "network")
        return nil, err
    }
    defer resp.Body.Close()
    
    if resp.StatusCode != 200 {
        err := fmt.Errorf("profile service returned %d", resp.StatusCode)
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "http")
        return nil, err
    }
    
    var profile UserProfile
    if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "json_decode")
        return nil, err
    }
    
    span.SetTag("profile.loaded", "true")
    span.SetTag("profile.has.avatar", strconv.FormatBool(profile.Avatar != ""))
    return &profile, nil
}

// Service B: Receiving call with W3C context
func ProfileServiceHandler(tracer *tracez.Tracer) http.Handler {
    mux := http.NewServeMux()
    
    // Apply W3C tracing middleware
    tracedMux := W3CTracingMiddleware(tracer)(mux)
    
    mux.HandleFunc("/users/", func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context() // Already contains trace context from middleware
        userID := strings.TrimPrefix(r.URL.Path, "/users/")
        
        _, span := tracer.StartSpan(ctx, "handler.get.user.profile")
        span.SetTag("user.id", userID)
        span.SetTag("request.path", r.URL.Path)
        defer span.Finish()
        
        // Simulate database lookup with context propagation
        profile, err := getUserProfile(ctx, tracer, userID)
        if err != nil {
            span.SetTag("error", err.Error())
            span.SetTag("error.type", "database")
            http.Error(w, "Profile not found", 404)
            return
        }
        
        span.SetTag("profile.found", "true")
        w.Header().Set("Content-Type", "application/json")
        json.NewEncoder(w).Encode(profile)
    })
    
    return tracedMux
}

func getUserProfile(ctx context.Context, tracer *tracez.Tracer, userID string) (*UserProfile, error) {
    _, span := tracer.StartSpan(ctx, "db.query.user.profile")
    span.SetTag("user.id", userID)
    span.SetTag("db.table", "users")
    defer span.Finish()
    
    // Simulate database query (would use actual DB with context)
    profile := &UserProfile{
        ID:       userID,
        Name:     "John Doe",
        Email:    "john@example.com",
        Avatar:   "https://avatars.example.com/" + userID,
    }
    
    span.SetTag("db.rows.returned", "1")
    return profile, nil
}

type UserProfile struct {
    ID     string `json:"id"`
    Name   string `json:"name"`
    Email  string `json:"email"`
    Avatar string `json:"avatar"`
}
```

### Trace Context Validation

```go
// validateTraceContext ensures proper propagation across services
func validateTraceContext(ctx context.Context, expectedTraceID string) bool {
    span := tracez.GetSpan(ctx)
    if span == nil {
        return false
    }
    
    return span.TraceID == expectedTraceID
}

// Example: End-to-end trace validation
func TestServiceIntegration(t *testing.T) {
    tracer := tracez.New("integration-test")
    
    // Start root trace in service A
    ctx, rootSpan := tracer.StartSpan(context.Background(), "integration.test")
    rootTraceID := rootSpan.TraceID
    defer rootSpan.Finish()
    
    // Call service B
    profile, err := CallServiceB(ctx, tracer, "user123")
    if err != nil {
        t.Fatalf("Service call failed: %v", err)
    }
    
    // Verify trace context was maintained
    if !validateTraceContext(ctx, rootTraceID) {
        t.Error("Trace context not properly propagated")
    }
    
    // Verify profile was retrieved
    if profile.ID != "user123" {
        t.Error("Incorrect profile returned")
    }
}
```

## Database Context Propagation

### SQL Database Tracing

```go
type TracedDB struct {
    db     *sql.DB
    tracer *tracez.Tracer
}

func NewTracedDB(db *sql.DB, tracer *tracez.Tracer) *TracedDB {
    return &TracedDB{db: db, tracer: tracer}
}

func (tdb *TracedDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
    _, span := tdb.tracer.StartSpan(ctx, "db.query")
    defer span.Finish()
    
    span.SetTag("db.statement", query)
    span.SetTag("db.operation", extractSQLOperation(query))
    span.SetTag("db.args_count", strconv.Itoa(len(args)))
    
    start := time.Now()
    rows, err := tdb.db.QueryContext(ctx, query, args...)
    duration := time.Since(start)
    
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
    }
    
    return rows, err
}

func (tdb *TracedDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
    _, span := tdb.tracer.StartSpan(ctx, "db.exec")
    defer span.Finish()
    
    span.SetTag("db.statement", query)
    span.SetTag("db.operation", extractSQLOperation(query))
    
    result, err := tdb.db.ExecContext(ctx, query, args...)
    if err != nil {
        span.SetTag("error", err.Error())
        return nil, err
    }
    
    if rowsAffected, err := result.RowsAffected(); err == nil {
        span.SetTag("db.rows_affected", strconv.FormatInt(rowsAffected, 10))
    }
    
    return result, nil
}

// Usage in business logic
func getUserByID(ctx context.Context, tdb *TracedDB, userID string) (*User, error) {
    // Context automatically flows to database layer
    rows, err := tdb.QueryContext(ctx, "SELECT * FROM users WHERE id = ?", userID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    
    // Process rows...
    return user, nil
}
```

## Context Propagation Patterns

### Request Scoped Services

```go
type RequestServices struct {
    tracer   *tracez.Tracer
    db       *TracedDB
    client   *http.Client
    ctx      context.Context
}

func NewRequestServices(ctx context.Context, tracer *tracez.Tracer) *RequestServices {
    return &RequestServices{
        tracer: tracer,
        db:     NewTracedDB(globalDB, tracer),
        client: TracedHTTPClient(tracer),
        ctx:    ctx, // Request context with trace
    }
}

func (rs *RequestServices) GetUser(userID string) (*User, error) {
    // All operations use the request context automatically
    _, span := rs.tracer.StartSpan(rs.ctx, "service.get-user")
    span.SetTag("user.id", userID)
    defer span.Finish()
    
    // Database call inherits context
    user, err := rs.db.QueryContext(rs.ctx, "SELECT * FROM users WHERE id = ?", userID)
    if err != nil {
        span.SetTag("error", err.Error())
        return nil, err
    }
    
    // External API call inherits context
    profile, err := rs.fetchUserProfile(userID)
    if err != nil {
        span.SetTag("profile.error", err.Error())
        // Continue without profile
    } else {
        user.Profile = profile
    }
    
    return user, nil
}

func (rs *RequestServices) fetchUserProfile(userID string) (*Profile, error) {
    url := fmt.Sprintf("https://profile-service/users/%s", userID)
    req, _ := http.NewRequestWithContext(rs.ctx, "GET", url, nil)
    
    resp, err := rs.client.Do(req) // Traced automatically
    // ... handle response
}
```

### Channel-Based Context Propagation

```go
type Message struct {
    ID      string
    Payload []byte
    Context context.Context // Embed context in message
}

func MessageProducer(ctx context.Context, tracer *tracez.Tracer, messages chan<- Message) {
    for i := 0; i < 100; i++ {
        // Create span for each message
        msgCtx, span := tracer.StartSpan(ctx, "message.produce")
        span.SetTag("message.id", strconv.Itoa(i))
        span.Finish() // Producer span ends here
        
        // Send message with context
        messages <- Message{
            ID:      strconv.Itoa(i),
            Payload: []byte(fmt.Sprintf("Message %d", i)),
            Context: msgCtx, // Context flows with message
        }
    }
}

func MessageConsumer(tracer *tracez.Tracer, messages <-chan Message) {
    for msg := range messages {
        // Use context from message
        _, span := tracer.StartSpan(msg.Context, "message.consume")
        span.SetTag("message.id", msg.ID)
        
        // Process message with inherited context
        if err := processMessage(msg.Context, msg.Payload); err != nil {
            span.SetTag("error", err.Error())
        }
        
        span.Finish()
    }
}

func processMessage(ctx context.Context, payload []byte) error {
    // This function can create child spans using the context
    _, span := tracer.StartSpan(ctx, "message.process")
    defer span.Finish()
    
    // Business logic here
    return nil
}
```

## Common Context Propagation Mistakes

### Mistake 1: Context Lost in Goroutines
```go
// ✗ Wrong
func processItems(ctx context.Context, items []Item) {
    for _, item := range items {
        go func(i Item) {
            // Context lost! This goroutine has no trace context
            process(i)
        }(item)
    }
}

// ✓ Correct  
func processItems(ctx context.Context, items []Item) {
    for _, item := range items {
        go func(ctx context.Context, i Item) {
            // Context preserved
            _, span := tracer.StartSpan(ctx, "item.process")
            defer span.Finish()
            process(ctx, i)
        }(ctx, item)
    }
}
```

### Mistake 2: Using Background Context
```go
// ✗ Wrong - breaks trace chain
func childOperation(parentCtx context.Context) {
    _, span := tracer.StartSpan(context.Background(), "child") // Lost parent!
    defer span.Finish()
}

// ✓ Correct - maintains trace chain
func childOperation(parentCtx context.Context) {
    _, span := tracer.StartSpan(parentCtx, "child") // Preserves parent
    defer span.Finish()
}
```

### Mistake 3: Not Propagating to Third-Party Libraries
```go
// ✗ Wrong - context not passed to HTTP client
func callAPI() {
    resp, err := http.Get("https://api.example.com/data")
    // No trace context in outbound call
}

// ✓ Correct - context flows to HTTP client
func callAPI(ctx context.Context) {
    req, _ := http.NewRequestWithContext(ctx, "GET", "https://api.example.com/data", nil)
    client := TracedHTTPClient(tracer)
    resp, err := client.Do(req) // Traced with context
}
```

## Testing Context Propagation

### Unit Test for Context Flow
```go
func TestContextPropagation(t *testing.T) {
    tracer := tracez.New("test")
    collector := tracez.NewCollector("test", 100)
    tracer.AddCollector("test", collector)
    
    // Create parent span
    ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")
    parentSpan.SetTag("test", "parent")
    
    // Create child span
    _, childSpan := tracer.StartSpan(ctx, "child")
    childSpan.SetTag("test", "child")
    
    // Finish spans
    childSpan.Finish()
    parentSpan.Finish()
    
    // Wait for collection
    time.Sleep(10 * time.Millisecond)
    
    // Verify hierarchy
    spans := collector.Export()
    require.Len(t, spans, 2)
    
    // Find parent and child
    var parent, child tracez.Span
    for _, span := range spans {
        if span.Name == "parent" {
            parent = span
        } else if span.Name == "child" {
            child = span
        }
    }
    
    // Verify relationship
    assert.Equal(t, parent.TraceID, child.TraceID) // Same trace
    assert.Equal(t, parent.SpanID, child.ParentID) // Child's parent is parent span
    assert.Empty(t, parent.ParentID) // Parent is root
}
```

### Integration Test for Service Calls
```go
func TestServiceCallPropagation(t *testing.T) {
    // Setup test server
    testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Verify headers were propagated
        assert.NotEmpty(t, r.Header.Get("X-Trace-ID"))
        assert.NotEmpty(t, r.Header.Get("X-Parent-Span-ID"))
        w.WriteHeader(200)
    }))
    defer testServer.Close()
    
    // Make traced call
    tracer := tracez.New("test")
    ctx, span := tracer.StartSpan(context.Background(), "test-call")
    defer span.Finish()
    
    client := TracedHTTPClient(tracer)
    req, _ := http.NewRequestWithContext(ctx, "GET", testServer.URL, nil)
    resp, err := client.Do(req)
    
    assert.NoError(t, err)
    assert.Equal(t, 200, resp.StatusCode)
}
```

## Next Steps

- **[Performance Guide](performance.md)**: Optimize context propagation performance
- **[Production Guide](production.md)**: Deploy context propagation patterns
- **[HTTP Tracing Patterns](../patterns/http-tracing.md)**: Web service context patterns
- **[Troubleshooting Guide](../reference/troubleshooting.md)**: Debug context propagation issues