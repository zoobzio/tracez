# Database Tracing Patterns

Problem-focused solutions for database performance issues, query optimization, and debugging database-related bottlenecks.

## Problem: N+1 Query Performance Issues

**Symptoms**: Application is slow, but individual queries look fast. Database CPU is high with many small queries.

**Solution**: Trace all queries to reveal N+1 patterns.

### Before: Invisible N+1 Problem
```go
func GetUsersWithPosts(ctx context.Context) ([]User, error) {
    // This looks innocent but creates N+1 queries
    users, err := db.Query("SELECT * FROM users")
    if err != nil {
        return nil, err
    }
    
    for i, user := range users {
        // Each iteration = another query! (N queries for N users)
        posts, err := db.Query("SELECT * FROM posts WHERE user_id = ?", user.ID)
        if err != nil {
            return nil, err
        }
        users[i].Posts = posts
    }
    
    return users, nil
}
```

### After: N+1 Problem Visible Through Tracing
```go
func GetUsersWithPosts(ctx context.Context, tracer *tracez.Tracer) ([]User, error) {
    // Service-level span to see total impact
    _, serviceSpan := tracer.StartSpan(ctx, "service.getUsersWithPosts")
    defer serviceSpan.Finish()
    
    // Query 1: Get users (the "1" in N+1)
    users, err := tracedQuery(ctx, tracer, "SELECT * FROM users", "users")
    if err != nil {
        serviceSpan.SetTag("error", err.Error())
        return nil, err
    }
    serviceSpan.SetTag("users.count", strconv.Itoa(len(users)))
    
    // Query 2+N: Get posts for each user (the "N" in N+1)
    for i, user := range users {
        posts, err := tracedQuery(ctx, tracer, 
            "SELECT * FROM posts WHERE user_id = ?", 
            "posts", user.ID)
        if err != nil {
            serviceSpan.SetTag("error", err.Error())
            return nil, err
        }
        
        // Individual span to see per-user impact
        _, userSpan := tracer.StartSpan(ctx, fmt.Sprintf("user.%d.posts", i))
        userSpan.SetTag("user.id", user.ID)
        userSpan.SetTag("posts.count", strconv.Itoa(len(posts)))
        userSpan.Finish()
        
        users[i].Posts = posts
    }
    
    return users, nil
}

func tracedQuery(ctx context.Context, tracer *tracez.Tracer, query string, table string, args ...interface{}) ([]Row, error) {
    _, span := tracer.StartSpan(ctx, "db.query")
    span.SetTag("db.statement", query)
    span.SetTag("db.table", table)
    span.SetTag("db.args_count", strconv.Itoa(len(args)))
    defer span.Finish()
    
    start := time.Now()
    results, err := db.Query(query, args...)
    duration := time.Since(start)
    
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "database")
        return nil, err
    }
    
    span.SetTag("db.rows_returned", strconv.Itoa(len(results)))
    
    // Flag slow queries
    if duration > 50*time.Millisecond {
        span.SetTag("performance.slow", "true")
    }
    
    return results, nil
}
```

### Optimized: Single Query Solution
```go
func GetUsersWithPostsOptimized(ctx context.Context, tracer *tracez.Tracer) ([]User, error) {
    _, span := tracer.StartSpan(ctx, "service.getUsersWithPostsOptimized")
    defer span.Finish()
    
    // Single JOIN query eliminates N+1 problem
    query := `
        SELECT users.id, users.name, users.email,
               posts.id, posts.title, posts.content
        FROM users 
        LEFT JOIN posts ON users.id = posts.user_id
        ORDER BY users.id, posts.id`
    
    results, err := tracedQuery(ctx, tracer, query, "users_posts_join")
    if err != nil {
        span.SetTag("error", err.Error())
        return nil, err
    }
    
    // Process results into nested structure
    users := groupResultsIntoUsers(results)
    
    span.SetTag("query.type", "batch_join")
    span.SetTag("optimization", "eliminated_n_plus_one")
    span.SetTag("users.count", strconv.Itoa(len(users)))
    
    return users, nil
}
```

**Trace comparison shows the dramatic difference**:
```
N+1 Pattern (BAD):
  service.getUsersWithPosts (245ms)
  ├─ db.query "SELECT * FROM users" (5ms, 100 rows)
  ├─ user.0.posts (2ms)
  │  └─ db.query "SELECT * FROM posts WHERE user_id = 1" (2ms, 3 rows)
  ├─ user.1.posts (2ms)
  │  └─ db.query "SELECT * FROM posts WHERE user_id = 2" (2ms, 1 row)
  └─ ... (98 more queries, 238ms total)

Optimized Pattern (GOOD):
  service.getUsersWithPostsOptimized (18ms)
  └─ db.query "SELECT users.*, posts.* FROM users LEFT JOIN..." (18ms, 150 rows)
```

## Problem: Slow Query Detection

**Symptoms**: Some requests are randomly slow, need to identify which queries are the bottleneck.

**Solution**: Automatic slow query flagging with detailed analysis.

```go
func DatabaseMiddleware(tracer *tracez.Tracer, db *sql.DB, slowThresholdMs int) *TracedDB {
    return &TracedDB{
        db:               db,
        tracer:          tracer,
        slowThresholdMs: slowThresholdMs,
    }
}

type TracedDB struct {
    db              *sql.DB
    tracer          *tracez.Tracer
    slowThresholdMs int
}

func (tdb *TracedDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
    _, span := tdb.tracer.StartSpan(ctx, "db.query")
    defer span.Finish()
    
    // Pre-execution metadata
    span.SetTag("db.statement", query)
    span.SetTag("db.args_count", strconv.Itoa(len(args)))
    span.SetTag("db.operation", extractOperation(query))
    span.SetTag("db.table", extractTableName(query))
    
    // Execute with timing
    start := time.Now()
    rows, err := tdb.db.QueryContext(ctx, query, args...)
    duration := time.Since(start)
    
    durationMs := duration.Seconds() * 1000
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", durationMs))
    
    // Performance analysis
    if durationMs > float64(tdb.slowThresholdMs) {
        span.SetTag("performance.slow", "true")
        span.SetTag("performance.threshold_ms", strconv.Itoa(tdb.slowThresholdMs))
        
        // Additional slow query metadata
        if durationMs > 1000 { // > 1 second
            span.SetTag("performance.severity", "critical")
        } else if durationMs > 500 { // > 500ms
            span.SetTag("performance.severity", "high")
        } else {
            span.SetTag("performance.severity", "medium")
        }
    }
    
    // Error handling
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", categorizeDBError(err))
        return nil, err
    }
    
    return rows, nil
}

func (tdb *TracedDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
    _, span := tdb.tracer.StartSpan(ctx, "db.exec")
    defer span.Finish()
    
    span.SetTag("db.statement", query)
    span.SetTag("db.operation", extractOperation(query))
    span.SetTag("db.table", extractTableName(query))
    
    start := time.Now()
    result, err := tdb.db.ExecContext(ctx, query, args...)
    duration := time.Since(start)
    
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", categorizeDBError(err))
        return nil, err
    }
    
    // Success metrics
    if rowsAffected, err := result.RowsAffected(); err == nil {
        span.SetTag("db.rows_affected", strconv.FormatInt(rowsAffected, 10))
        
        // Flag operations affecting many rows
        if rowsAffected > 1000 {
            span.SetTag("db.bulk_operation", "true")
        }
    }
    
    return result, nil
}

func extractOperation(query string) string {
    query = strings.TrimSpace(strings.ToUpper(query))
    if strings.HasPrefix(query, "SELECT") {
        return "SELECT"
    } else if strings.HasPrefix(query, "INSERT") {
        return "INSERT"
    } else if strings.HasPrefix(query, "UPDATE") {
        return "UPDATE"
    } else if strings.HasPrefix(query, "DELETE") {
        return "DELETE"
    }
    return "UNKNOWN"
}

func extractTableName(query string) string {
    // Simple table extraction - could be enhanced with SQL parser
    query = strings.ToLower(strings.TrimSpace(query))
    
    if strings.HasPrefix(query, "select") {
        if fromIndex := strings.Index(query, " from "); fromIndex != -1 {
            remaining := query[fromIndex+6:]
            parts := strings.Fields(remaining)
            if len(parts) > 0 {
                return strings.Trim(parts[0], "`\"[]")
            }
        }
    } else if strings.HasPrefix(query, "insert into") {
        parts := strings.Fields(query)
        if len(parts) >= 3 {
            return strings.Trim(parts[2], "`\"[]")
        }
    } else if strings.HasPrefix(query, "update") {
        parts := strings.Fields(query)
        if len(parts) >= 2 {
            return strings.Trim(parts[1], "`\"[]")
        }
    }
    
    return "unknown"
}

func categorizeDBError(err error) string {
    errStr := strings.ToLower(err.Error())
    
    switch {
    case strings.Contains(errStr, "connection"):
        return "connection_error"
    case strings.Contains(errStr, "timeout"):
        return "timeout"
    case strings.Contains(errStr, "duplicate"):
        return "duplicate_key"
    case strings.Contains(errStr, "constraint"):
        return "constraint_violation"
    case strings.Contains(errStr, "syntax"):
        return "syntax_error"
    case strings.Contains(errStr, "permission"):
        return "permission_denied"
    default:
        return "database_error"
    }
}
```

## Problem: Transaction Deadlock Debugging

**Symptoms**: Occasional deadlocks causing transaction failures, need to understand lock patterns.

**Solution**: Transaction-level tracing with lock analysis.

```go
func (tdb *TracedDB) BeginTx(ctx context.Context, opts *sql.TxOptions) (*TracedTx, error) {
    _, span := tdb.tracer.StartSpan(ctx, "db.transaction")
    
    span.SetTag("tx.isolation_level", getTxIsolationLevel(opts))
    span.SetTag("tx.read_only", strconv.FormatBool(opts != nil && opts.ReadOnly))
    
    tx, err := tdb.db.BeginTx(ctx, opts)
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", "tx_begin_failed")
        span.Finish()
        return nil, err
    }
    
    return &TracedTx{
        tx:      tx,
        span:    span,
        tracer:  tdb.tracer,
        queries: 0,
    }, nil
}

type TracedTx struct {
    tx      *sql.Tx
    span    *tracez.ActiveSpan
    tracer  *tracez.Tracer
    queries int
}

func (ttx *TracedTx) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
    ttx.queries++
    
    _, span := ttx.tracer.StartSpan(ctx, "db.tx-query")
    span.SetTag("tx.query_number", strconv.Itoa(ttx.queries))
    span.SetTag("db.statement", query)
    defer span.Finish()
    
    start := time.Now()
    rows, err := ttx.tx.QueryContext(ctx, query, args...)
    duration := time.Since(start)
    
    span.SetTag("db.duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        span.SetTag("error", err.Error())
        span.SetTag("error.type", categorizeDBError(err))
        
        // Check for deadlock
        if isDeadlock(err) {
            span.SetTag("deadlock.detected", "true")
            ttx.span.SetTag("tx.deadlock", "true")
            ttx.span.SetTag("tx.deadlock_query", strconv.Itoa(ttx.queries))
        }
    }
    
    return rows, err
}

func (ttx *TracedTx) Commit() error {
    defer ttx.span.Finish()
    
    ttx.span.SetTag("tx.queries_executed", strconv.Itoa(ttx.queries))
    ttx.span.SetTag("tx.result", "commit")
    
    start := time.Now()
    err := ttx.tx.Commit()
    duration := time.Since(start)
    
    ttx.span.SetTag("tx.commit_duration_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
    
    if err != nil {
        ttx.span.SetTag("error", err.Error())
        ttx.span.SetTag("error.type", "commit_failed")
    }
    
    return err
}

func (ttx *TracedTx) Rollback() error {
    defer ttx.span.Finish()
    
    ttx.span.SetTag("tx.queries_executed", strconv.Itoa(ttx.queries))
    ttx.span.SetTag("tx.result", "rollback")
    
    err := ttx.tx.Rollback()
    if err != nil {
        ttx.span.SetTag("error", err.Error())
        ttx.span.SetTag("error.type", "rollback_failed")
    }
    
    return err
}

func isDeadlock(err error) bool {
    errStr := strings.ToLower(err.Error())
    return strings.Contains(errStr, "deadlock") || 
           strings.Contains(errStr, "lock wait timeout")
}

func getTxIsolationLevel(opts *sql.TxOptions) string {
    if opts == nil {
        return "default"
    }
    
    switch opts.Isolation {
    case sql.LevelDefault:
        return "default"
    case sql.LevelReadUncommitted:
        return "read_uncommitted"
    case sql.LevelReadCommitted:
        return "read_committed"
    case sql.LevelWriteCommitted:
        return "write_committed"
    case sql.LevelRepeatableRead:
        return "repeatable_read"
    case sql.LevelSnapshot:
        return "snapshot"
    case sql.LevelSerializable:
        return "serializable"
    case sql.LevelLinearizable:
        return "linearizable"
    default:
        return "unknown"
    }
}
```

## Problem: Connection Pool Exhaustion

**Symptoms**: Application hangs waiting for database connections, need to understand connection usage.

**Solution**: Connection pool monitoring with acquisition tracking.

```go
func NewConnectionTrackedDB(tracer *tracez.Tracer, db *sql.DB) *ConnectionTrackedDB {
    return &ConnectionTrackedDB{
        TracedDB: DatabaseMiddleware(tracer, db, 100),
        tracer:   tracer,
    }
}

type ConnectionTrackedDB struct {
    *TracedDB
    tracer *tracez.Tracer
}

func (ctdb *ConnectionTrackedDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
    // Track connection acquisition
    _, span := ctdb.tracer.StartSpan(ctx, "db.connection-acquire")
    defer span.Finish()
    
    // Get pool stats before operation
    stats := ctdb.db.Stats()
    span.SetTag("pool.open_connections", strconv.Itoa(stats.OpenConnections))
    span.SetTag("pool.in_use", strconv.Itoa(stats.InUse))
    span.SetTag("pool.idle", strconv.Itoa(stats.Idle))
    span.SetTag("pool.wait_count", strconv.FormatInt(stats.WaitCount, 10))
    span.SetTag("pool.wait_duration_ms", fmt.Sprintf("%.2f", stats.WaitDuration.Seconds()*1000))
    
    // Flag connection pool pressure
    if stats.InUse > stats.OpenConnections-5 { // Less than 5 connections available
        span.SetTag("pool.pressure", "high")
    }
    
    if stats.WaitCount > 0 {
        span.SetTag("pool.contention", "true")
    }
    
    start := time.Now()
    rows, err := ctdb.TracedDB.QueryContext(ctx, query, args...)
    acquisitionTime := time.Since(start)
    
    span.SetTag("connection.acquisition_ms", fmt.Sprintf("%.2f", acquisitionTime.Seconds()*1000))
    
    // Flag slow connection acquisition (indicates pool exhaustion)
    if acquisitionTime > 10*time.Millisecond {
        span.SetTag("connection.slow_acquire", "true")
    }
    
    return rows, err
}
```

## Analysis Patterns

### N+1 Query Detection
```go
func detectNPlusOne(spans []tracez.Span) []NPlusOnePattern {
    var patterns []NPlusOnePattern
    
    for _, span := range spans {
        if span.Name == "service.getUsersWithPosts" { // Or similar service operations
            childQueries := getChildSpans(spans, span.SpanID)
            
            dbQueries := 0
            for _, child := range childQueries {
                if child.Name == "db.query" {
                    dbQueries++
                }
            }
            
            // N+1 pattern: Initial query + N subsequent queries
            if dbQueries > 10 { // Threshold for concern
                patterns = append(patterns, NPlusOnePattern{
                    ServiceSpan: span,
                    QueryCount:  dbQueries,
                    Duration:    span.Duration,
                })
            }
        }
    }
    
    return patterns
}

type NPlusOnePattern struct {
    ServiceSpan tracez.Span
    QueryCount  int
    Duration    time.Duration
}
```

### Slow Query Analysis
```go
func analyzeSlowQueries(spans []tracez.Span, thresholdMs float64) {
    slowQueries := make(map[string][]QueryMetric)
    
    for _, span := range spans {
        if span.Name == "db.query" {
            if durationStr, ok := span.Tags["db.duration_ms"]; ok {
                if duration, err := strconv.ParseFloat(durationStr, 64); err == nil && duration > thresholdMs {
                    table := span.Tags["db.table"]
                    operation := span.Tags["db.operation"]
                    key := fmt.Sprintf("%s.%s", table, operation)
                    
                    slowQueries[key] = append(slowQueries[key], QueryMetric{
                        Duration:  duration,
                        Statement: span.Tags["db.statement"],
                    })
                }
            }
        }
    }
    
    fmt.Printf("Slow queries (>%.0fms):\n", thresholdMs)
    for queryType, metrics := range slowQueries {
        avgDuration := calculateAverage(metrics)
        maxDuration := calculateMax(metrics)
        
        fmt.Printf("  %s: %d occurrences, avg %.1fms, max %.1fms\n", 
            queryType, len(metrics), avgDuration, maxDuration)
        
        // Show worst query
        if len(metrics) > 0 {
            worst := findSlowest(metrics)
            fmt.Printf("    Slowest: %.1fms - %s\n", worst.Duration, worst.Statement[:100])
        }
    }
}

type QueryMetric struct {
    Duration  float64
    Statement string
}
```

## Working Example

The [database patterns example](../../examples/database-patterns/) demonstrates:
- N+1 query detection with before/after comparisons
- Automatic slow query flagging
- Transaction tracing with deadlock detection  
- Connection pool monitoring

Run it to see these patterns in action:
```bash
cd examples/database-patterns
go run main.go
```

## Next Steps

- **[Error Handling Patterns](error-handling.md)**: Comprehensive error tracing strategies
- **[Performance Guide](../guides/performance.md)**: Optimize tracing overhead
- **[Production Guide](../guides/production.md)**: Deploy database tracing in production
- **[Troubleshooting Guide](../reference/troubleshooting.md)**: Debug common database tracing issues