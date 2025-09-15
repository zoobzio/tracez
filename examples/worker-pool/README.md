# Worker Pool Example: The Hidden Bottleneck

## The Mystery

Production worker pool handled 1000 req/sec on staging. In production? 12 req/sec.

Same code. Same workers. What happened?

## The Symptoms

- Jobs queued for seconds
- Workers seemed "busy" (CPU metrics showed activity)
- Debug logs showed workers processing
- CPU available, memory fine
- Made no sense

## What We Tried (And Failed)

1. **Added more workers** - No improvement
2. **Increased job buffer** - Still slow
3. **Added logging** - Showed workers were "working"
4. **Profiled CPU** - Not the bottleneck
5. **Checked database** - Queries were fast

## The Revelation: Tracez

Run the example and watch tracez reveal the truth:

```bash
go run main.go
```

## What Tracez Found

### Discovery #1: Result Channel Blocking
```
Found 15 blocked result sends!
  - Worker blocked for 43ms sending result
  - Worker blocked for 49ms sending result
  - Worker blocked for 99ms sending result
```

Workers weren't processing - they were stuck trying to send results!

### Discovery #2: Operation Disparity
```
Cache reads: avg 2ms (12 calls)
Vendor APIs: avg 268ms (4 calls)
→ Vendor APIs are 105x slower!
```

One slow vendor API could block all workers.

### Discovery #3: Head-of-Line Blocking

Fast cache reads (2ms) got stuck behind slow vendor APIs (268ms). Workers couldn't take new jobs while blocked on the results channel.

## The Root Cause Chain

1. **Vendor API degradation** - Some calls took 2 seconds (vs normal 100ms)
2. **Small results buffer** - Only 4 slots, filled quickly
3. **Slow result consumer** - Processing results at 50ms each
4. **Channel blocking** - Workers wait to deliver results
5. **Cascade failure** - Fast jobs stuck behind slow ones

## The Fix

```go
// BEFORE: Small buffer causes blocking
results: make(chan Result, workers)

// AFTER: Larger buffer OR non-blocking send
results: make(chan Result, workers*100)

// OR: Non-blocking send pattern
select {
case p.results <- result:
    // Sent successfully
default:
    // Buffer result for retry
    p.buffered = append(p.buffered, result)
}
```

## Lessons Learned

1. **Workers can be "busy" doing nothing** - Blocked on channels looks like work
2. **Fast and slow don't mix** - One slow operation blocks everything
3. **Buffers matter** - Too small = hidden serialization
4. **Standard metrics lie** - CPU/memory fine, but throughput dead
5. **Distributed tracing reveals truth** - See exactly where time goes

## Try It Yourself

1. Run with current settings - see the blocking
2. Change results buffer to `workers*100` - problem disappears
3. Add priority queues - fast jobs bypass slow ones
4. Implement timeout handling - prevent vendor API from killing throughput

Without tracez, this bug lived in production for months. With tracez, found and fixed in minutes.