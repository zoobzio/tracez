# HTTP Tracing Patterns

Problem-focused solutions for tracing HTTP services and debugging web application issues.

## Problem: Slow HTTP Responses

**Symptoms**: Users complain about slow page loads, but you don't know where time is spent.

**Solution**: Comprehensive HTTP request tracing with timing breakdown.

```go
func TracingMiddleware(tracer *tracez.Tracer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create root span for request
            ctx, span := tracer.StartSpan(r.Context(), "http.request")
            
            // Request metadata
            span.SetTag("http.method", r.Method)
            span.SetTag("http.path", r.URL.Path)
            span.SetTag("http.user_agent", r.UserAgent())
            span.SetTag("http.content_length", strconv.FormatInt(r.ContentLength, 10))
            
            // Extract user context for debugging
            if userID := r.Header.Get("X-User-ID"); userID != "" {
                span.SetTag("user.id", userID)
            }
            
            defer span.Finish()
            
            // Wrap response writer to capture metrics
            wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}
            start := time.Now()
            
            // Process request
            next.ServeHTTP(wrapped, r.WithContext(ctx))
            
            // Tag response details
            duration := time.Since(start)
            span.SetTag("http.status_code", strconv.Itoa(wrapped.statusCode))
            span.SetTag("http.response_time_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
            span.SetTag("http.response_size", strconv.FormatInt(wrapped.bytesWritten, 10))
            
            // Flag slow requests for investigation
            if duration > 100*time.Millisecond {
                span.SetTag("performance.slow", "true")
                span.SetTag("performance.threshold_ms", "100")
            }
            
            // Flag errors for filtering
            if wrapped.statusCode >= 400 {
                span.SetTag("error.http", "true")
                if wrapped.statusCode >= 500 {
                    span.SetTag("error.severity", "high")
                } else {
                    span.SetTag("error.severity", "medium")
                }
            }
        })
    }
}

type responseWriter struct {
    http.ResponseWriter
    statusCode   int
    bytesWritten int64
}

func (w *responseWriter) WriteHeader(code int) {
    w.statusCode = code
    w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
    n, err := w.ResponseWriter.Write(b)
    w.bytesWritten += int64(n)
    return n, err
}
```

**What the trace reveals**:
```
http.request (250ms) - SLOW REQUEST DETECTED
├─ middleware.auth (5ms)
├─ handler.dashboard (240ms) - Main bottleneck
│  ├─ db.get-user (15ms)
│  ├─ db.get-posts (200ms) - Problem found!
│  └─ template.render (20ms)
└─ middleware.logging (2ms)
```

## Problem: Authentication Failures

**Symptoms**: Users getting 401 errors, but auth logic is complex and hard to debug.

**Solution**: Detailed authentication tracing with failure reasons.

```go
func AuthMiddleware(tracer *tracez.Tracer, authService AuthService) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := tracer.StartSpan(r.Context(), "auth.verify")
            span.SetTag("auth.method", "bearer_token")
            defer span.Finish()
            
            // Extract token
            authHeader := r.Header.Get("Authorization")
            if authHeader == "" {
                span.SetTag("auth.result", "failed")
                span.SetTag("auth.reason", "no_auth_header")
                span.SetTag("auth.required_header", "Authorization")
                http.Error(w, "Authentication required", 401)
                return
            }
            
            // Parse token
            token := strings.TrimPrefix(authHeader, "Bearer ")
            if token == authHeader {
                span.SetTag("auth.result", "failed") 
                span.SetTag("auth.reason", "invalid_format")
                span.SetTag("auth.expected_format", "Bearer <token>")
                http.Error(w, "Invalid auth format", 401)
                return
            }
            
            span.SetTag("auth.token_length", strconv.Itoa(len(token)))
            
            // Validate token
            user, err := authService.ValidateToken(ctx, tracer, token)
            if err != nil {
                span.SetTag("auth.result", "failed")
                span.SetTag("auth.error", err.Error())
                
                // Categorize auth errors for analysis
                switch {
                case isExpiredToken(err):
                    span.SetTag("auth.reason", "token_expired")
                case isInvalidToken(err):
                    span.SetTag("auth.reason", "token_invalid")
                case isRevokedToken(err):
                    span.SetTag("auth.reason", "token_revoked")
                default:
                    span.SetTag("auth.reason", "service_error")
                }
                
                http.Error(w, "Authentication failed", 401)
                return
            }
            
            // Success
            span.SetTag("auth.result", "success")
            span.SetTag("auth.user.id", user.ID)
            span.SetTag("auth.user.role", user.Role)
            span.SetTag("auth.token.type", user.TokenType)
            
            // Add user context to request
            ctx = context.WithValue(ctx, "user", user)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func (as *AuthService) ValidateToken(ctx context.Context, tracer *tracez.Tracer, token string) (*User, error) {
    _, span := tracer.StartSpan(ctx, "auth.token-validation")
    defer span.Finish()
    
    // Parse JWT
    claims, err := as.parseJWT(token)
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "jwt_parse")
        return nil, err
    }
    
    span.SetTag("jwt.subject", claims.Subject)
    span.SetTag("jwt.issued_at", claims.IssuedAt.Format(time.RFC3339))
    span.SetTag("jwt.expires_at", claims.ExpiresAt.Format(time.RFC3339))
    
    // Check expiration
    if time.Now().After(claims.ExpiresAt) {
        span.SetTag("error", "token expired")
        span.SetTag("error.type", "token_expired")
        span.SetTag("expired_seconds_ago", strconv.FormatInt(int64(time.Since(claims.ExpiresAt).Seconds()), 10))
        return nil, ErrTokenExpired
    }
    
    // Validate against database
    user, err := as.getUserByID(ctx, tracer, claims.Subject)
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "user_lookup")
        return nil, err
    }
    
    // Check if user is active
    if !user.Active {
        span.SetTag("error", "user inactive")
        span.SetTag("error.type", "user_inactive")
        span.SetTag("user.status", "inactive")
        return nil, ErrUserInactive
    }
    
    return user, nil
}
```

**Authentication trace analysis**:
```go
func analyzeAuthFailures(spans []tracez.Span) {
    failures := make(map[string]int)
    
    for _, span := range spans {
        if span.Name == "auth.verify" {
            if result, ok := span.Tags["auth.result"]; ok && result == "failed" {
                if reason, ok := span.Tags["auth.reason"]; ok {
                    failures[reason]++
                }
            }
        }
    }
    
    fmt.Printf("Auth failure analysis:\n")
    for reason, count := range failures {
        fmt.Printf("  %s: %d failures\n", reason, count)
    }
}
```

## Problem: Error Rate Spikes

**Symptoms**: Error monitoring shows increased 500 errors, need to identify root cause.

**Solution**: Structured error tracing with categorization and context.

```go
func ErrorTrackingMiddleware(tracer *tracez.Tracer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := tracer.StartSpan(r.Context(), "http.request")
            defer span.Finish()
            
            // Wrap writer to capture errors
            wrapped := &errorCapturingWriter{ResponseWriter: w, statusCode: 200}
            
            // Recover from panics
            defer func() {
                if recover := recover(); recover != nil {
                    span.SetTag("error.panic", "true")
                    span.SetTag("error", fmt.Sprintf("Panic: %v", recover))
                    span.SetTag("error.type", "panic")
                    span.SetTag("error.severity", "critical")
                    http.Error(wrapped, "Internal server error", 500)
                }
            }()
            
            next.ServeHTTP(wrapped, r.WithContext(ctx))
            
            // Analyze errors
            if wrapped.statusCode >= 400 {
                span.SetTag("error.http", "true")
                span.SetTag("http.status_code", strconv.Itoa(wrapped.statusCode))
                
                switch {
                case wrapped.statusCode >= 500:
                    span.SetTag("error.category", "server_error")
                    span.SetTag("error.severity", "high")
                case wrapped.statusCode >= 400:
                    span.SetTag("error.category", "client_error") 
                    span.SetTag("error.severity", "medium")
                }
                
                // Add request context for debugging
                span.SetTag("request.path", r.URL.Path)
                span.SetTag("request.method", r.Method)
                
                if userID := r.Header.Get("X-User-ID"); userID != "" {
                    span.SetTag("user.id", userID)
                }
            }
        })
    }
}

type errorCapturingWriter struct {
    http.ResponseWriter
    statusCode int
}

func (w *errorCapturingWriter) WriteHeader(code int) {
    w.statusCode = code
    w.ResponseWriter.WriteHeader(code)
}
```

## Problem: API Rate Limiting Issues

**Symptoms**: Clients getting 429 errors, need to understand usage patterns.

**Solution**: Request rate and quota tracking with client identification.

```go
func RateLimitingMiddleware(tracer *tracez.Tracer, rateLimiter RateLimiter) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := tracer.StartSpan(r.Context(), "rate_limit.check")
            defer span.Finish()
            
            // Identify client
            clientID := extractClientID(r)
            span.SetTag("client.id", clientID)
            span.SetTag("client.ip", r.RemoteAddr)
            
            // Check rate limit
            allowed, quota, remaining, resetTime := rateLimiter.Allow(clientID)
            
            span.SetTag("rate_limit.quota", strconv.Itoa(quota))
            span.SetTag("rate_limit.remaining", strconv.Itoa(remaining))
            span.SetTag("rate_limit.reset_time", resetTime.Format(time.RFC3339))
            span.SetTag("rate_limit.allowed", strconv.FormatBool(allowed))
            
            if !allowed {
                span.SetTag("rate_limit.exceeded", "true")
                span.SetTag("error", "rate limit exceeded")
                span.SetTag("error.type", "rate_limit")
                
                // Add rate limit headers
                w.Header().Set("X-RateLimit-Limit", strconv.Itoa(quota))
                w.Header().Set("X-RateLimit-Remaining", "0")
                w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
                
                http.Error(w, "Rate limit exceeded", 429)
                return
            }
            
            // Add rate limit headers for successful requests
            w.Header().Set("X-RateLimit-Limit", strconv.Itoa(quota))
            w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
            w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))
            
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func extractClientID(r *http.Request) string {
    // Priority order for client identification
    if apiKey := r.Header.Get("X-API-Key"); apiKey != "" {
        return "api:" + apiKey
    }
    if userID := r.Header.Get("X-User-ID"); userID != "" {
        return "user:" + userID
    }
    // Fall back to IP address
    return "ip:" + r.RemoteAddr
}
```

## Problem: Load Balancer Health Check Noise

**Symptoms**: Health check requests creating unnecessary trace volume.

**Solution**: Selective tracing with health check filtering.

```go
func SelectiveTracingMiddleware(tracer *tracez.Tracer) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Skip tracing for health checks
            if isHealthCheck(r) {
                next.ServeHTTP(w, r)
                return
            }
            
            // Skip tracing for static assets (optional)
            if isStaticAsset(r) {
                next.ServeHTTP(w, r)
                return
            }
            
            // Full tracing for business requests
            ctx, span := tracer.StartSpan(r.Context(), "http.request")
            span.SetTag("http.method", r.Method)
            span.SetTag("http.path", r.URL.Path)
            span.SetTag("request.type", "business")
            defer span.Finish()
            
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func isHealthCheck(r *http.Request) bool {
    path := r.URL.Path
    return path == "/health" || 
           path == "/healthz" || 
           path == "/ping" ||
           strings.HasPrefix(path, "/_health")
}

func isStaticAsset(r *http.Request) bool {
    path := r.URL.Path
    return strings.HasPrefix(path, "/static/") ||
           strings.HasPrefix(path, "/assets/") ||
           strings.HasSuffix(path, ".css") ||
           strings.HasSuffix(path, ".js") ||
           strings.HasSuffix(path, ".png") ||
           strings.HasSuffix(path, ".jpg")
}
```

## Problem: CORS and Preflight Request Issues

**Symptoms**: CORS failures in browser, need to debug preflight handling.

**Solution**: Detailed CORS request tracing.

```go
func CORSMiddleware(tracer *tracez.Tracer, allowedOrigins []string) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            ctx, span := tracer.StartSpan(r.Context(), "cors.check")
            defer span.Finish()
            
            origin := r.Header.Get("Origin")
            span.SetTag("cors.origin", origin)
            span.SetTag("cors.method", r.Method)
            
            // Check if this is a preflight request
            if r.Method == "OPTIONS" {
                span.SetTag("cors.preflight", "true")
                requestMethod := r.Header.Get("Access-Control-Request-Method")
                requestHeaders := r.Header.Get("Access-Control-Request-Headers")
                
                span.SetTag("cors.requested_method", requestMethod)
                span.SetTag("cors.requested_headers", requestHeaders)
                
                // Validate origin
                if origin != "" && isAllowedOrigin(origin, allowedOrigins) {
                    span.SetTag("cors.origin_allowed", "true")
                    
                    w.Header().Set("Access-Control-Allow-Origin", origin)
                    w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
                    w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
                    w.Header().Set("Access-Control-Max-Age", "3600")
                    
                    w.WriteHeader(204)
                    return
                } else {
                    span.SetTag("cors.origin_allowed", "false")
                    span.SetTag("cors.error", "origin not allowed")
                    span.SetTag("cors.allowed_origins", strings.Join(allowedOrigins, ","))
                    
                    http.Error(w, "CORS: origin not allowed", 403)
                    return
                }
            }
            
            // Regular request - add CORS headers if origin is allowed
            if origin != "" {
                if isAllowedOrigin(origin, allowedOrigins) {
                    span.SetTag("cors.origin_allowed", "true")
                    w.Header().Set("Access-Control-Allow-Origin", origin)
                } else {
                    span.SetTag("cors.origin_allowed", "false")
                    span.SetTag("cors.warning", "origin not allowed for regular request")
                }
            }
            
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func isAllowedOrigin(origin string, allowedOrigins []string) bool {
    for _, allowed := range allowedOrigins {
        if origin == allowed || allowed == "*" {
            return true
        }
    }
    return false
}
```

## Trace Analysis Patterns

### Finding Slow Endpoints
```go
func findSlowEndpoints(spans []tracez.Span, thresholdMs float64) {
    slowEndpoints := make(map[string][]float64)
    
    for _, span := range spans {
        if span.Name == "http.request" {
            if durationStr, ok := span.Tags["http.response_time_ms"]; ok {
                if duration, err := strconv.ParseFloat(durationStr, 64); err == nil {
                    if duration > thresholdMs {
                        path := span.Tags["http.path"]
                        slowEndpoints[path] = append(slowEndpoints[path], duration)
                    }
                }
            }
        }
    }
    
    fmt.Printf("Slow endpoints (>%.0fms):\n", thresholdMs)
    for path, durations := range slowEndpoints {
        avg := average(durations)
        fmt.Printf("  %s: %d occurrences, avg %.1fms\n", path, len(durations), avg)
    }
}
```

### Error Pattern Analysis
```go
func analyzeErrorPatterns(spans []tracez.Span) {
    errorsByEndpoint := make(map[string]map[int]int)
    
    for _, span := range spans {
        if span.Name == "http.request" {
            if errorStr, hasError := span.Tags["error.http"]; hasError && errorStr == "true" {
                path := span.Tags["http.path"]
                statusCode, _ := strconv.Atoi(span.Tags["http.status_code"])
                
                if errorsByEndpoint[path] == nil {
                    errorsByEndpoint[path] = make(map[int]int)
                }
                errorsByEndpoint[path][statusCode]++
            }
        }
    }
    
    fmt.Printf("Error patterns by endpoint:\n")
    for path, errors := range errorsByEndpoint {
        fmt.Printf("  %s:\n", path)
        for statusCode, count := range errors {
            fmt.Printf("    %d: %d occurrences\n", statusCode, count)
        }
    }
}
```

## Next Steps

- **[Database Tracing Patterns](database-tracing.md)**: Debug database performance issues
- **[Error Handling Patterns](error-handling.md)**: Comprehensive error tracing
- **[Production Guide](../guides/production.md)**: Deploy HTTP tracing in production
- **[API Reference](../reference/api.md)**: Complete tracez API documentation