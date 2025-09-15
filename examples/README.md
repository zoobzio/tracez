# Tracez Examples: Real Problems, Real Solutions

This directory contains three real-world troubleshooting scenarios that demonstrate how distributed tracing with tracez reveals problems that traditional monitoring misses.

## The Collection

### 1. [Database Patterns: The N+1 Query Problem](database-patterns/)
**The Story:** Team dashboard loads in 2.5 seconds despite "working fine" in development.  
**The Discovery:** 301 database queries for 50 users - classic N+1 problem hidden by small test data.  
**The Fix:** Single JOIN query reduces load time to 150ms (16.5x faster).  
**Key Learning:** "It works" != "It works well" - scale reveals hidden inefficiencies.

### 2. [HTTP Middleware: The Mystery of the 503s](http-middleware/)
**The Story:** Random 503 errors for "some users sometimes" with no clear pattern.  
**The Discovery:** Retry middleware consuming rate limit quota - retries triggering false rate limits.  
**The Fix:** Swap middleware order - rate limit before retry, not after.  
**Key Learning:** Middleware ordering matters - same components, different sequence, vastly different behavior.

### 3. [Worker Pool: The Hidden Bottleneck](worker-pool/)
**The Story:** Worker pool handles 1000 req/sec in staging, 12 req/sec in production.  
**The Discovery:** Workers blocked on channel sends, not processing - fast jobs stuck behind slow ones.  
**The Fix:** Larger result buffer prevents head-of-line blocking.  
**Key Learning:** Workers can be "busy" doing nothing - blocked operations look like work to standard metrics.

## Why These Examples Matter

Traditional monitoring (CPU, memory, logs) showed nothing wrong in all three cases:

- **Database example**: Database CPU at 12%, no memory issues, queries individually fast
- **Middleware example**: No errors in logs, HTTP status codes mixed symptoms with causes  
- **Worker pool example**: CPU active, memory fine, workers appeared "busy"

**Distributed tracing revealed the truth** by showing relationships between operations that metrics and logs couldn't capture.

## Running the Examples

Each example is self-contained with its own `go.mod` and can be run independently:

```bash
# Database patterns
cd database-patterns && go run main.go

# HTTP middleware 
cd http-middleware && go run main.go

# Worker pool
cd worker-pool && go run main.go
```

All examples include:
- **Runnable demonstrations** - See the problem and solution in action
- **Test suites** - Verify pattern detection and fixes
- **Performance analysis** - Before/after metrics with tracez insights
- **Real failure scenarios** - Based on actual production incidents

## What You'll Learn

1. **Hidden relationships** - How seemingly independent components interact
2. **Scale effects** - Problems that only appear under realistic load
3. **Metric limitations** - Why CPU/memory/logs miss the real issues
4. **Trace-driven debugging** - Using spans to reveal bottlenecks
5. **Performance optimization** - Data-driven improvements vs guesswork

## The Pattern

Each example follows the same investigation pattern:

1. **Symptoms observed** - User reports, basic metrics
2. **Traditional debugging fails** - Standard approaches miss the issue  
3. **Tracez reveals truth** - Spans show what really happens
4. **Root cause identified** - Clear understanding of the problem
5. **Fix implemented** - Targeted solution based on evidence
6. **Results validated** - Performance improvement measured

This pattern reflects real troubleshooting workflows where distributed tracing makes the invisible visible.

## Beyond the Examples

These scenarios represent common patterns in distributed systems:

- **Database patterns**: Query optimization, connection pooling, transaction boundaries
- **Middleware patterns**: Request flow, error handling, ordering dependencies
- **Concurrency patterns**: Channel blocking, resource contention, backpressure

Use these as templates for instrumenting your own systems and identifying similar issues before they impact users.