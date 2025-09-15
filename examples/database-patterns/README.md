# Database Patterns Example: The N+1 Query Problem

## The Story

**Friday, 3:47 PM:** "The dashboard is slow again," Sarah from Product messaged me on Slack. Our team dashboard - a simple page showing 50 team members with their recent posts and comments - was taking 2-3 seconds to load. Not terrible, but annoying enough that people were complaining.

**My first thought:** "It's probably the database server. Let me check the metrics."

CPU usage: 12%. Memory: fine. Network: normal. The database server was practically sleeping.

**My second thought:** "Must be the application server then."

But the app server was also fine. No memory leaks, no CPU spikes. The code was simple - fetch users, fetch their posts, fetch comments. What could go wrong?

## The Investigation

I added tracez to see what was actually happening:

```go
tracer := tracez.New("team-dashboard")
collector := tracez.NewCollector("dashboard-traces", 5000)
tracer.AddCollector("collector", collector)
```

Then I ran the dashboard load with tracing enabled.

## The Shocking Discovery

```bash
$ go run main.go

REALISTIC N+1 SCENARIO: Team Dashboard Loading Problem
======================================================================

Story: Our team dashboard was 'working' but users complained it was slow.
It loads 50 team members with their recent posts and comments.

Let's see what tracez reveals...

--- BEFORE: Original Dashboard Load ---
Loading team dashboard the 'simple' way...

Dashboard loaded in: 2.48s
Total queries executed: 301

Tracez Analysis Reveals:
  - 1 query to fetch users
  - 50 queries to fetch posts (one per user!)
  - 250 queries to fetch comments (one per post!)
  - Total time in database: 2.47s
  - Database time as % of total: 99.9%
```

**301 queries!** For 50 users! 

The "simple" code that "worked fine" was actually:
1. Fetching all users (1 query)
2. For each user, fetching their posts (50 queries) 
3. For each post, fetching its comments (250 queries)

This is the classic N+1 query problem. It worked fine in development with 5 test users. But with 50 real users? Death by a thousand cuts.

## The Fix

Replace the nested loops with a single JOIN query:

```go
// BEFORE: N+1 Problem
users, _ := db.Query(ctx, "SELECT * FROM users")
for _, user := range users {
    posts, _ := db.Query(ctx, "SELECT * FROM posts WHERE user_id = ?", user.ID)
    for _, post := range posts {
        comments, _ := db.Query(ctx, "SELECT * FROM comments WHERE post_id = ?", post.ID)
    }
}

// AFTER: Single JOIN
result, _ := db.Query(ctx, 
    "SELECT users.*, posts.*, comments.* FROM users " +
    "LEFT JOIN posts ON users.id = posts.user_id " +
    "LEFT JOIN comments ON posts.id = comments.post_id")
```

## The Result

```bash
--- AFTER: Optimized Dashboard Load ---
Loading dashboard with single JOIN query...

Dashboard loaded in: 150ms
Total queries executed: 1

Optimization Results:
  - Load time: 2.48s → 150ms (16.5x faster!)
  - Query count: 301 → 1
  - Database time: 2.47s → 150ms
  - User experience: Sluggish → Snappy!
```

## Running the Example

```bash
# Run the full demonstration
go run main.go

# Run the tests to verify pattern detection
go test -v
```

## Key Lessons

1. **"It works" != "It works well"** - The N+1 version technically worked, but with terrible performance.

2. **Small data hides big problems** - With 5 test users, the N+1 pattern added 100ms. With 50 real users, it added 2 seconds.

3. **Observability reveals truth** - Without tracez, we would have kept blaming the database server or network.

4. **The fix is often simple** - Once we saw the problem, the solution was obvious: use a JOIN.

## The Pattern Detection

This example includes pattern analysis that automatically detects N+1 problems:

```go
func AnalyzePatterns(spans []tracez.Span) {
    // Detects sequential similar queries
    // Counts queries by table
    // Calculates total database time
    // Provides specific recommendations
}
```

When tracez detects multiple similar queries in sequence, it flags them as potential N+1 problems:

```
Potential N+1 Problems Detected:
  Query pattern 'SELECT * FROM posts' executed 50 times (possible N+1)
  Query pattern 'SELECT * FROM comments' executed 200 times (possible N+1)

Recommendations:
  - Consider batching 'SELECT * FROM posts' queries to avoid N+1 problem
  - Consider batching 'SELECT * FROM comments' queries to avoid N+1 problem
```

## Technical Details

The example demonstrates several database patterns:

- **N+1 Query Pattern**: The anti-pattern where 1 query spawns N additional queries
- **Batch Query Pattern**: Using JOINs to fetch related data in a single query  
- **Transaction Pattern**: Multiple queries wrapped in a transaction
- **Slow Query Detection**: Automatic flagging of queries over threshold

Each pattern is traced with specific metadata:
- Query statements
- Execution time
- Row counts
- Parent-child relationships
- Transaction boundaries

## Try It Yourself

1. Run the example to see the N+1 problem in action
2. Modify the user count to see how it scales (hint: it gets worse)
3. Experiment with the slow query threshold
4. Add your own query patterns to test

The dashboard that was frustrating our team every day? It's now instant. And all it took was making the invisible visible with tracez.