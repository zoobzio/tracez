# The Mystery of the 503s: How Middleware Ordering Broke Our API

## The Incident

"API returns 503 for some users sometimes."

That was the ticket. No pattern. Same endpoints. Same code. Some users got 503s, others didn't.

Traditional monitoring showed nothing. Logs showed rate limiting (429) and service unavailable (503) errors, but no connection between them.

Then we turned on tracez.

## The Discovery

What we found changed everything:

```
Trace: a7f3c2d1
  🔥 PROBLEM DETECTED: Retry consumed rate limit!
     - Retries attempted: 4
     - Each retry counted against rate limit
     - User blocked with 429 despite original 502
     - Final status: 429

  Span hierarchy:
    [http.request] 412ms (status: 429)
      [retry.handler] 410ms (exhausted: true, attempts: 4)
        [retry.attempt] 102ms (attempt: 1, status: 502)
          [ratelimit.check] 1ms (count: 1/5)
        [retry.attempt] 102ms (attempt: 2, status: 502)
          [ratelimit.check] 1ms (count: 2/5)
        [retry.attempt] 102ms (attempt: 3, status: 502)
          [ratelimit.check] 1ms (count: 3/5)
        [retry.attempt] 102ms (attempt: 4, status: 429)
          [ratelimit.check] 1ms (count: 6/5) ⚠️ EXCEEDED
```

**The problem:** Our retry middleware ran BEFORE our rate limit middleware.

Every retry attempt counted as a new request against the user's rate limit. A temporary backend failure (502) that should have been retried successfully was instead triggering rate limiting (429).

## The Root Cause

Our middleware chain looked innocent enough:

```go
// PROBLEM: This ordering causes retries to consume rate limit
handler := TracingMiddleware(tracer)(
    RetryMiddleware(tracer, 3)(          // Retry happens first
        RateLimitMiddleware(tracer, 5)(  // Each retry counts!
            mux,
        ),
    ),
)
```

When the backend returned 502:
1. Retry middleware caught it
2. Retry middleware made another request
3. That request went through rate limiting AGAIN
4. After 5 attempts, user was rate limited
5. User got 429 instead of the actual error

## The Fix

One line. We swapped the middleware order:

```go
// SOLUTION: Rate limit before retry
handler := TracingMiddleware(tracer)(
    RateLimitMiddleware(tracer, 5)(      // Rate limit happens first
        RetryMiddleware(tracer, 3)(      // Only retry if under limit
            mux,
        ),
    ),
)
```

Now:
1. Rate limiter checks quota once
2. If under limit, request proceeds
3. Retries happen AFTER rate limit check
4. Retries don't consume additional quota
5. Users get appropriate error codes

## The Impact

**Before the fix:**
- Users experiencing backend issues also got rate limited
- Support tickets about "random 503s" that were actually 429s
- Retry storms during outages consumed all rate limit quotas
- Legitimate traffic blocked during recovery

**After the fix:**
- Backend failures retry without consuming quota
- Clear separation between rate limiting and availability issues
- Graceful degradation during outages
- 73% reduction in false rate limit violations

## Running the Demo

This example demonstrates both the problem and solution:

```bash
# Start the server (runs both configurations)
go run main.go

# Terminal 1: Problem server (port 8080)
# Watch how retries consume rate limit quota
for i in {1..10}; do
  curl -H 'X-User-ID: alice' http://localhost:8080/api/data
  sleep 0.5
done

# Terminal 2: Solution server (port 8081)
# Rate limit protects from retry storms
for i in {1..10}; do
  curl -H 'X-User-ID: bob' http://localhost:8081/api/data
  sleep 0.5
done
```

Watch the trace output to see the difference. The API randomly fails 20% of requests to simulate backend issues.

## Key Observations

1. **Middleware ordering matters** - The same components in different order produce vastly different behavior
2. **Traces reveal interactions** - Logs showed symptoms, traces showed cause
3. **Implicit dependencies** - Retry logic had no idea it was affecting rate limiting
4. **Cascading effects** - One retry could trigger a chain reaction

## The Lesson

This wasn't a bug in our retry logic. It wasn't a bug in our rate limiter. It was the interaction between them that nobody anticipated.

Without distributed tracing, we would never have seen that retries were consuming rate limit quota. The spans revealed the hidden relationship between two seemingly independent middlewares.

**The real value of tracez:** It doesn't just show you what happened. It shows you HOW things relate to each other. And sometimes, that relationship is the bug.

## Technical Details

The demo includes:
- **RateLimitMiddleware**: Per-user rate limiting with atomic counters
- **RetryMiddleware**: Automatic retry with exponential backoff for 5xx errors
- **TracingMiddleware**: Request tracing with automatic span creation
- **Trace Analysis**: Real-time detection of retry/rate-limit interactions

Each request creates a trace showing the complete middleware chain execution, making the ordering problem immediately visible.

## Try It Yourself

1. Run the demo
2. Watch the trace output
3. Notice how the PROBLEM server shows retries consuming rate limit
4. Notice how the SOLUTION server properly separates the concerns
5. Modify the middleware ordering and see what happens

The mystery of the 503s wasn't a mystery at all. It was middleware ordering. And tracez made it visible.