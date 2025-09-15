# Error Handling Patterns

Problem-focused solutions for making failures visible and debuggable through comprehensive error tracing.

## Problem: Production Errors Without Root Cause

**Symptoms**: Errors show up in logs but you can't determine what caused them or which request they came from.

**Solution**: Structured error tracing with categorization and context.

```go
func ProcessPayment(ctx context.Context, tracer *tracez.Tracer, payment Payment) error {
    _, span := tracer.StartSpan(ctx, "payment.process")
    span.SetTag("payment.amount", fmt.Sprintf("%.2f", payment.Amount))
    span.SetTag("payment.method", payment.Method)
    span.SetTag("customer.id", payment.CustomerID)
    defer span.Finish()
    
    // Validate payment
    if err := validatePayment(ctx, tracer, payment); err != nil {
        // Structured error tagging
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "validation_failed")
        span.SetTag("error.stage", "validation")
        span.SetTag("error.field", extractFieldFromError(err))
        span.SetTag("retry.recommended", "false")
        return err
    }
    
    // Process payment
    result, err := chargeCard(ctx, tracer, payment)
    if err != nil {
        span.SetTag("error", err.Error())
        
        // Different error types need different handling
        if isDeclinedError(err) {
            span.SetTag("error.type", "payment_declined")
            span.SetTag("error.stage", "charging")
            span.SetTag("retry.recommended", "false")
            span.SetTag("error.decline_reason", extractDeclineReason(err))
        } else if isTimeoutError(err) {
            span.SetTag("error.type", "timeout")
            span.SetTag("error.stage", "charging")
            span.SetTag("retry.recommended", "true")
            span.SetTag("error.timeout_ms", extractTimeoutDuration(err))
        } else {
            span.SetTag("error.type", "service_error")
            span.SetTag("error.stage", "charging")
            span.SetTag("retry.recommended", "true")
        }
        
        return err
    }
    
    // Success case
    span.SetTag("transaction.id", result.TransactionID)
    span.SetTag("result", "success")
    return nil
}

func validatePayment(ctx context.Context, tracer *tracez.Tracer, payment Payment) error {
    _, span := tracer.StartSpan(ctx, "payment.validate")
    defer span.Finish()
    
    // Check amount
    if payment.Amount <= 0 {
        err := errors.New("amount must be positive")
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "validation")
        span.SetTag("validation.field", "amount")
        span.SetTag("validation.value", fmt.Sprintf("%.2f", payment.Amount))
        span.SetTag("validation.constraint", "must be > 0")
        return err
    }
    
    // Check payment method
    if payment.Method == "" {
        err := errors.New("payment method required")
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "validation")
        span.SetTag("validation.field", "method")
        span.SetTag("validation.constraint", "must be non-empty")
        return err
    }
    
    // Check customer ID
    if payment.CustomerID == "" {
        err := errors.New("customer ID required")
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "validation")
        span.SetTag("validation.field", "customer.id")
        return err
    }
    
    span.SetTag("validation.result", "passed")
    span.SetTag("validation.checks", "3")
    return nil
}
```

## Problem: External Service Failures

**Symptoms**: Third-party API calls fail but you don't know if it's network, timeout, or service errors.

**Solution**: Comprehensive external service tracing.

```go
func CallExternalAPI(ctx context.Context, tracer *tracez.Tracer, endpoint string, payload interface{}) (*APIResponse, error) {
    _, span := tracer.StartSpan(ctx, "external.api")
    span.SetTag("api.service", extractServiceName(endpoint))
    span.SetTag("api.endpoint", endpoint)
    span.SetTag("api.method", "POST")
    defer span.Finish()
    
    // Prepare request
    client := &http.Client{
        Timeout: 30 * time.Second,
        Transport: &http.Transport{
            MaxIdleConns:       10,
            IdleConnTimeout:    30 * time.Second,
            DisableCompression: true,
        },
    }
    
    start := time.Now()
    resp, err := client.Post(endpoint, "application/json", payloadReader(payload))
    duration := time.Since(start)
    
    span.SetTag("api.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        // Network or transport errors
        span.SetTag("error", err.Error())
        
        if isTimeoutError(err) {
            span.SetTag("error.type", "timeout")
            span.SetTag("error.category", "network")
            span.SetTag("retry.recommended", "true")
            span.SetTag("retry.delay.ms", "1000")
        } else if isConnectionError(err) {
            span.SetTag("error.type", "connection_failed")
            span.SetTag("error.category", "network")
            span.SetTag("retry.recommended", "true")
            span.SetTag("retry.delay.ms", "5000")
        } else if isDNSError(err) {
            span.SetTag("error.type", "dns_resolution")
            span.SetTag("error.category", "network")
            span.SetTag("retry.recommended", "false")
        } else {
            span.SetTag("error.type", "network_unknown")
            span.SetTag("error.category", "network")
            span.SetTag("retry.recommended", "false")
        }
        
        return nil, err
    }
    defer resp.Body.Close()
    
    span.SetTag("api.status_code", strconv.Itoa(resp.StatusCode))
    
    // Read response body for error analysis
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        span.SetTag("error", "failed to read response body")
        span.SetTag("error.type", "response_read")
        return nil, err
    }
    
    if resp.StatusCode >= 400 {
        // HTTP-level errors
        span.SetTag("error", string(body))
        span.SetTag("error.category", "http")
        
        switch {
        case resp.StatusCode >= 500:
            span.SetTag("error.type", "server_error")
            span.SetTag("retry.recommended", "true")
            span.SetTag("retry.delay.ms", "2000")
        case resp.StatusCode == 429:
            span.SetTag("error.type", "rate_limited")
            span.SetTag("retry.recommended", "true")
            span.SetTag("retry.delay.ms", extractRetryAfter(resp))
        case resp.StatusCode >= 400:
            span.SetTag("error.type", "client_error")
            span.SetTag("retry.recommended", "false")
        }
        
        return nil, fmt.Errorf("API call failed with status %d", resp.StatusCode)
    }
    
    // Success case
    var response APIResponse
    if err := json.Unmarshal(body, &response); err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "json_decode")
        span.SetTag("error.category", "response_format")
        span.SetTag("response.size_bytes", strconv.Itoa(len(body)))
        return nil, err
    }
    
    span.SetTag("api.result", "success")
    span.SetTag("response.size_bytes", strconv.Itoa(len(body)))
    return &response, nil
}

// Error classification helpers
func isTimeoutError(err error) bool {
    if netErr, ok := err.(net.Error); ok {
        return netErr.Timeout()
    }
    return strings.Contains(err.Error(), "timeout")
}

func isConnectionError(err error) bool {
    return strings.Contains(err.Error(), "connection refused") ||
           strings.Contains(err.Error(), "connection reset")
}

func isDNSError(err error) bool {
    return strings.Contains(err.Error(), "no such host") ||
           strings.Contains(err.Error(), "dns")
}

func extractRetryAfter(resp *http.Response) string {
    if retry := resp.Header.Get("Retry-After"); retry != "" {
        if seconds, err := strconv.Atoi(retry); err == nil {
            return strconv.Itoa(seconds * 1000) // Convert to milliseconds
        }
    }
    return "60000" // Default 60 seconds
}
```

## Problem: Database Error Debugging

**Symptoms**: Database operations fail with cryptic error messages, hard to determine root cause.

**Solution**: Database-specific error tracing and categorization.

```go
func ExecuteQuery(ctx context.Context, tracer *tracez.Tracer, query string, args ...interface{}) (*sql.Rows, error) {
    _, span := tracer.StartSpan(ctx, "db.query")
    defer span.Finish()
    
    // Pre-execution metadata
    span.SetTag("db.statement", query)
    span.SetTag("db.args_count", strconv.Itoa(len(args)))
    span.SetTag("db.operation", extractSQLOperation(query))
    span.SetTag("db.table", extractTableName(query))
    
    start := time.Now()
    rows, err := db.QueryContext(ctx, query, args...)
    duration := time.Since(start)
    
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        
        // Categorize database errors
        switch {
        case isSyntaxError(err):
            span.SetTag("error.type", "syntax_error")
            span.SetTag("error.category", "query")
            span.SetTag("retry.recommended", "false")
            span.SetTag("error.severity", "high")
            
        case isConnectionError(err):
            span.SetTag("error.type", "connection_error")
            span.SetTag("error.category", "network")
            span.SetTag("retry.recommended", "true")
            span.SetTag("error.severity", "medium")
            
        case isDeadlockError(err):
            span.SetTag("error.type", "deadlock")
            span.SetTag("error.category", "concurrency")
            span.SetTag("retry.recommended", "true")
            span.SetTag("error.severity", "medium")
            span.SetTag("retry.delay.ms", "100") // Short delay for deadlock retry
            
        case isTimeoutError(err):
            span.SetTag("error.type", "timeout")
            span.SetTag("error.category", "performance")
            span.SetTag("retry.recommended", "true")
            span.SetTag("error.severity", "medium")
            
        case isConstraintViolation(err):
            span.SetTag("error.type", "constraint_violation")
            span.SetTag("error.category", "data")
            span.SetTag("retry.recommended", "false")
            span.SetTag("error.severity", "low")
            span.SetTag("constraint.type", extractConstraintType(err))
            
        case isPermissionError(err):
            span.SetTag("error.type", "permission_denied")
            span.SetTag("error.category", "security")
            span.SetTag("retry.recommended", "false")
            span.SetTag("error.severity", "high")
            
        default:
            span.SetTag("error.type", "database_unknown")
            span.SetTag("error.category", "database")
            span.SetTag("retry.recommended", "false")
            span.SetTag("error.severity", "medium")
        }
        
        return nil, err
    }
    
    return rows, nil
}

// Database error classification
func isSyntaxError(err error) bool {
    errStr := strings.ToLower(err.Error())
    return strings.Contains(errStr, "syntax error") ||
           strings.Contains(errStr, "near")
}

func isDeadlockError(err error) bool {
    errStr := strings.ToLower(err.Error())
    return strings.Contains(errStr, "deadlock") ||
           strings.Contains(errStr, "lock wait timeout")
}

func isConstraintViolation(err error) bool {
    errStr := strings.ToLower(err.Error())
    return strings.Contains(errStr, "constraint") ||
           strings.Contains(errStr, "duplicate") ||
           strings.Contains(errStr, "foreign key")
}

func extractConstraintType(err error) string {
    errStr := strings.ToLower(err.Error())
    if strings.Contains(errStr, "primary key") {
        return "primary.key"
    } else if strings.Contains(errStr, "foreign key") {
        return "foreign.key"
    } else if strings.Contains(errStr, "unique") {
        return "unique"
    } else if strings.Contains(errStr, "check") {
        return "check"
    }
    return "unknown"
}
```

## Problem: Panic Recovery and Tracing

**Symptoms**: Panics occur but you lose trace context when they happen.

**Solution**: Panic recovery with error tracing.

```go
func SafeHandler(tracer *tracez.Tracer, handler func(ctx context.Context) error) func(ctx context.Context) error {
    return func(ctx context.Context) (err error) {
        _, span := tracer.StartSpan(ctx, "safe.handler")
        defer span.Finish()
        
        // Recover from panics
        defer func() {
            if r := recover(); r != nil {
                // Capture panic details
                span.SetTag("error.panic", "true")
                span.SetTag("error", fmt.Sprintf("Panic: %v", r))
                span.SetTag("error.type", "panic")
                span.SetTag("error.severity", "critical")
                
                // Capture stack trace
                buf := make([]byte, 4096)
                stackSize := runtime.Stack(buf, false)
                stack := string(buf[:stackSize])
                span.SetTag("error.stack_trace", stack)
                
                // Convert panic to error
                err = fmt.Errorf("panic recovered: %v", r)
            }
        }()
        
        // Execute handler
        err = handler(ctx)
        if err != nil {
            span.SetTag("error", err.Error())
            span.SetTag("error.type", "handler_error")
        }
        
        return err
    }
}

// Usage example
func riskyOperation(ctx context.Context) error {
    tracer := tracez.GetTracer(ctx)
    
    safeFunc := SafeHandler(tracer, func(ctx context.Context) error {
        // This could panic
        result := 10 / getRiskyValue()
        fmt.Printf("Result: %d\n", result)
        return nil
    })
    
    return safeFunc(ctx)
}
```

## Error Analysis Patterns

### Comprehensive Error Analysis
```go
func analyzeErrors(spans []tracez.Span) {
    errorsByType := make(map[string]int)
    errorsByCategory := make(map[string]int)
    errorsBySeverity := make(map[string]int)
    retryableErrors := 0
    totalErrors := 0
    
    for _, span := range spans {
        if errorMsg, hasError := span.Tags["error"]; hasError && errorMsg != "" {
            totalErrors++
            
            if errorType, ok := span.Tags["error.type"]; ok {
                errorsByType[errorType]++
            }
            
            if category, ok := span.Tags["error.category"]; ok {
                errorsByCategory[category]++
            }
            
            if severity, ok := span.Tags["error.severity"]; ok {
                errorsBySeverity[severity]++
            }
            
            if retry, ok := span.Tags["retry.recommended"]; ok && retry == "true" {
                retryableErrors++
            }
        }
    }
    
    fmt.Printf("Error Analysis Summary:\n")
    fmt.Printf("  Total errors: %d\n", totalErrors)
    fmt.Printf("  Retryable errors: %d (%.1f%%)\n", retryableErrors, 
        float64(retryableErrors)/float64(totalErrors)*100)
    
    fmt.Printf("\nErrors by type:\n")
    for errType, count := range errorsByType {
        pct := float64(count) / float64(totalErrors) * 100
        fmt.Printf("  %s: %d (%.1f%%)\n", errType, count, pct)
    }
    
    fmt.Printf("\nErrors by category:\n")
    for category, count := range errorsByCategory {
        pct := float64(count) / float64(totalErrors) * 100
        fmt.Printf("  %s: %d (%.1f%%)\n", category, count, pct)
    }
    
    fmt.Printf("\nErrors by severity:\n")
    for severity, count := range errorsBySeverity {
        pct := float64(count) / float64(totalErrors) * 100
        fmt.Printf("  %s: %d (%.1f%%)\n", severity, count, pct)
    }
}
```

### Error Correlation Analysis
```go
func findErrorPatterns(spans []tracez.Span) {
    // Group spans by trace ID
    traceErrors := make(map[string][]tracez.Span)
    
    for _, span := range spans {
        if _, hasError := span.Tags["error"]; hasError {
            traceErrors[span.TraceID] = append(traceErrors[span.TraceID], span)
        }
    }
    
    // Analyze error cascades
    fmt.Printf("Error correlation analysis:\n")
    for traceID, errorSpans := range traceErrors {
        if len(errorSpans) > 1 {
            fmt.Printf("  Trace %s: %d errors\n", traceID[:8], len(errorSpans))
            for _, span := range errorSpans {
                fmt.Printf("    %s: %s\n", span.Name, span.Tags["error.type"])
            }
        }
    }
}
```

## Best Practices

### Consistent Error Tagging
```go
// Standard error tags to use across your application
const (
    ErrorTag      = "error"           // Error message
    ErrorTypeTag  = "error.type"      // Error category
    ErrorStageTag = "error.stage"     // Where error occurred  
    RetrySafeTag  = "retry.recommended" // Boolean
    SeverityTag   = "error.severity"  // "low", "medium", "high", "critical"
)

func tagError(span *tracez.ActiveSpan, err error, errorType, stage string, retrySafe bool) {
    span.SetTag(ErrorTag, err.Error())
    span.SetTag(ErrorTypeTag, errorType)
    span.SetTag(ErrorStageTag, stage)
    span.SetTag(RetrySafeTag, strconv.FormatBool(retrySafe))
    
    // Auto-determine severity based on type
    severity := "medium" // default
    switch errorType {
    case "panic", "security", "data_corruption":
        severity = "critical"
    case "timeout", "service_unavailable", "rate_limited":
        severity = "high"
    case "validation", "not_found", "permission_denied":
        severity = "low"
    }
    span.SetTag(SeverityTag, severity)
}
```

### Error Monitoring Integration
```go
func monitorErrors(spans []tracez.Span) {
    for _, span := range spans {
        if errorMsg, hasError := span.Tags["error"]; hasError {
            severity := span.Tags["error.severity"]
            
            // Send to monitoring system based on severity
            switch severity {
            case "critical":
                sendAlert("CRITICAL", span.Name, errorMsg)
            case "high":
                sendWarning("HIGH", span.Name, errorMsg)
            case "medium", "low":
                logError(span.Name, errorMsg)
            }
        }
    }
}
```

## Next Steps

- **[HTTP Tracing Patterns](http-tracing.md)**: Web-specific error handling
- **[Database Tracing Patterns](database-tracing.md)**: Database error analysis
- **[Production Guide](../guides/production.md)**: Error monitoring in production
- **[Troubleshooting Guide](../reference/troubleshooting.md)**: Common error resolution