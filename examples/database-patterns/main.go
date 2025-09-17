package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/zoobzio/tracez"
)

// Local key constants for this example.
const (
	DBQueryKey       = "db.query"
	DBBatchKey       = "db.batch"
	DBTransactionKey = "db.transaction"
)

// Database operation constants.
const (
	// Static keys for data fetching operations.
	FetchPostsKey    tracez.Key = "fetch.posts"    // Static key for all post fetches.
	FetchCommentsKey tracez.Key = "fetch.comments" // Static key for all comment fetches.
	// Database transaction constant to avoid repeated strings.
	DBTransactionName = "db.transaction"
)

// MockDB simulates a database with tracing.
type MockDB struct {
	tracer        *tracez.Tracer
	slowQueries   []string
	queryCount    int
	realisticMode bool // When true, simulates realistic query times
}

// NewMockDB creates a traced database client.
func NewMockDB(tracer *tracez.Tracer) *MockDB {
	return &MockDB{
		tracer:        tracer,
		slowQueries:   make([]string, 0),
		realisticMode: false,
	}
}

// NewRealisticMockDB creates a database client with realistic timing.
func NewRealisticMockDB(tracer *tracez.Tracer) *MockDB {
	return &MockDB{
		tracer:        tracer,
		slowQueries:   make([]string, 0),
		realisticMode: true,
	}
}

// Query executes a traced database query.
func (db *MockDB) Query(ctx context.Context, query string, args ...interface{}) ([]map[string]interface{}, error) {
	_, span := db.tracer.StartSpan(ctx, DBQueryKey)
	span.SetTag("db.statement", query)
	span.SetTag("db.args_count", fmt.Sprintf("%d", len(args)))
	defer span.Finish()

	db.queryCount++

	// Simulate query execution time based on query type.
	var duration time.Duration
	if db.realisticMode {
		// Realistic production database latencies
		switch {
		case strings.Contains(query, "JOIN"):
			duration = 150 * time.Millisecond // Complex JOIN
		case strings.Contains(query, "WHERE user_id =") || strings.Contains(query, "WHERE post_id ="):
			duration = 8 * time.Millisecond // Single row lookup (actually indexed in production)
		case strings.Contains(query, "WHERE id ="):
			duration = 2 * time.Millisecond // Indexed primary key lookup
		case strings.Contains(query, "SELECT * FROM users"):
			duration = 25 * time.Millisecond // Full table scan of small users table
		default:
			duration = 5 * time.Millisecond
		}
	} else {
		// Test mode timing
		switch {
		case strings.Contains(query, "JOIN"):
			duration = 20 * time.Millisecond
		case strings.Contains(query, "WHERE id ="):
			duration = 2 * time.Millisecond
		case strings.Contains(query, "COUNT(*)"):
			duration = 15 * time.Millisecond
		default:
			duration = 5 * time.Millisecond
		}
	}

	time.Sleep(duration)

	// Track slow queries.
	slowThreshold := 10 * time.Millisecond
	if db.realisticMode {
		slowThreshold = 50 * time.Millisecond // Production threshold
	}
	if duration > slowThreshold {
		db.slowQueries = append(db.slowQueries, query)
		span.SetTag("db.slow_query", "true")
	}

	// Mock results based on query type.
	if strings.Contains(query, "SELECT * FROM users") {
		// Return realistic number of users for a small team
		users := make([]map[string]interface{}, 50)
		for i := 0; i < 50; i++ {
			users[i] = map[string]interface{}{
				"id":    i + 1,
				"name":  fmt.Sprintf("user_%d", i+1),
				"email": fmt.Sprintf("user%d@company.com", i+1),
			}
		}
		return users, nil
	} else if strings.Contains(query, "posts WHERE user_id") {
		// Each user has 2-5 posts
		postCount := 3 + (db.queryCount % 3) // Vary between 3-5 posts
		posts := make([]map[string]interface{}, postCount)
		for i := 0; i < postCount; i++ {
			posts[i] = map[string]interface{}{
				"id":      db.queryCount*10 + i,
				"title":   fmt.Sprintf("Post %d", i+1),
				"user_id": args[0],
			}
		}
		return posts, nil
	} else if strings.Contains(query, "comments WHERE post_id") {
		// Each post has 0-3 comments
		commentCount := db.queryCount % 4 // 0-3 comments
		comments := make([]map[string]interface{}, commentCount)
		for i := 0; i < commentCount; i++ {
			comments[i] = map[string]interface{}{
				"id":      db.queryCount*100 + i,
				"text":    fmt.Sprintf("Comment %d", i+1),
				"post_id": args[0],
			}
		}
		return comments, nil
	}

	// Default mock results
	return []map[string]interface{}{
		{"id": 1, "name": "result1"},
		{"id": 2, "name": "result2"},
	}, nil
}

// UserService demonstrates N+1 query problem.
type UserService struct {
	db     *MockDB
	tracer *tracez.Tracer
}

func NewUserService(db *MockDB, tracer *tracez.Tracer) *UserService {
	return &UserService{db: db, tracer: tracer}
}

// GetUsersWithPosts - BAD: N+1 query pattern.
func (s *UserService) GetUsersWithPosts(ctx context.Context) error {
	ctx, span := s.tracer.StartSpan(ctx, "service.getUsersWithPosts")
	defer span.Finish()

	// First query: get all users.
	users, _ := s.db.Query(ctx, "SELECT * FROM users") //nolint:errcheck // Demo code focuses on tracing patterns
	span.SetTag("users.count", fmt.Sprintf("%d", len(users)))

	// N+1 Problem: For each user, query their posts.
	for i, user := range users {
		userID, ok := user["id"].(int)
		if !ok {
			continue
		}

		// This creates N additional queries!.
		// Use dynamic key to track each user's post fetch separately.
		fetchPostsKey := fmt.Sprintf("fetch.posts.%d", i)
		ctx2, postSpan := s.tracer.StartSpan(ctx, fetchPostsKey)
		posts, _ := s.db.Query(ctx2, "SELECT * FROM posts WHERE user_id = ?", userID) //nolint:errcheck // Demo code focuses on tracing patterns
		postSpan.SetTag("posts.count", fmt.Sprintf("%d", len(posts)))
		postSpan.SetTag("user.id", fmt.Sprintf("%d", userID))
		postSpan.SetTag("user.index", fmt.Sprintf("%d", i))
		postSpan.Finish()

		// For each post, get comments (even worse!).
		for j, post := range posts {
			postID, ok := post["id"].(int)
			if !ok {
				continue
			}

			// Use dynamic key to track each post's comment fetch separately.
			fetchCommentsKey := fmt.Sprintf("fetch.comments.%d", j)
			ctx3, commentSpan := s.tracer.StartSpan(ctx2, fetchCommentsKey)
			comments, _ := s.db.Query(ctx3, "SELECT * FROM comments WHERE post_id = ?", postID) //nolint:errcheck // Demo code focuses on tracing patterns
			commentSpan.SetTag("comments.count", fmt.Sprintf("%d", len(comments)))
			commentSpan.SetTag("post.id", fmt.Sprintf("%d", postID))
			commentSpan.SetTag("post.index", fmt.Sprintf("%d", j))
			commentSpan.Finish()
		}
	}

	return nil
}

// GetUsersWithPostsOptimized - GOOD: Batch query pattern.
func (s *UserService) GetUsersWithPostsOptimized(ctx context.Context) error {
	ctx, span := s.tracer.StartSpan(ctx, "service.getUsersWithPostsOptimized")
	defer span.Finish()

	// Single query with JOIN.
	_, joinSpan := s.tracer.StartSpan(ctx, DBBatchKey)
	result, _ := s.db.Query(ctx, //nolint:errcheck // Demo code focuses on tracing patterns
		"SELECT users.*, posts.*, comments.* FROM users "+
			"LEFT JOIN posts ON users.id = posts.user_id "+
			"LEFT JOIN comments ON posts.id = comments.post_id")
	joinSpan.SetTag("query.type", "batch_join")
	joinSpan.SetTag("rows.returned", fmt.Sprintf("%d", len(result)))
	joinSpan.Finish()

	return nil
}

// OrderService demonstrates various query patterns.
type OrderService struct {
	db     *MockDB
	tracer *tracez.Tracer
}

func NewOrderService(db *MockDB, tracer *tracez.Tracer) *OrderService {
	return &OrderService{db: db, tracer: tracer}
}

// ProcessOrder shows transaction-like pattern.
func (s *OrderService) ProcessOrder(ctx context.Context, orderID int) error {
	ctx, span := s.tracer.StartSpan(ctx, "service.processOrder")
	span.SetTag("order.id", fmt.Sprintf("%d", orderID))
	defer span.Finish()

	// Start transaction span.
	txCtx, txSpan := s.tracer.StartSpan(ctx, DBTransactionKey)
	defer txSpan.Finish()

	// Multiple queries in transaction.
	_, _ = s.db.Query(txCtx, "SELECT * FROM orders WHERE id = ?", orderID)                                   //nolint:errcheck // Demo code focuses on tracing patterns
	_, _ = s.db.Query(txCtx, "UPDATE orders SET status = ? WHERE id = ?", "processing", orderID)             //nolint:errcheck // Demo code focuses on tracing patterns
	_, _ = s.db.Query(txCtx, "INSERT INTO order_events (order_id, event) VALUES (?, ?)", orderID, "started") //nolint:errcheck // Demo code focuses on tracing patterns

	// Check inventory (could be optimized).
	items, _ := s.db.Query(txCtx, "SELECT * FROM order_items WHERE order_id = ?", orderID) //nolint:errcheck // Demo code focuses on tracing patterns
	for _, item := range items {
		itemID, ok := item["id"].(int)
		if !ok {
			continue
		}
		_, _ = s.db.Query(txCtx, "SELECT stock FROM inventory WHERE item_id = ?", itemID) //nolint:errcheck // Demo code focuses on tracing patterns
	}

	txSpan.SetTag("queries.in_transaction", fmt.Sprintf("%d", 4+len(items)))

	return nil
}

// AnalyzePatterns detects common database anti-patterns.
func AnalyzePatterns(spans []tracez.Span) {
	fmt.Println("\n=== Database Query Pattern Analysis ===")

	// Count queries by type.
	queryCount := 0
	slowQueries := 0
	queriesByTable := make(map[string]int)

	// Detect N+1 patterns.
	sequentialQueries := make(map[string]int)

	for i := range spans {
		span := &spans[i]
		if strings.HasPrefix(span.Name, "db.") {
			queryCount++

			if slow, ok := span.Tags["db.slow_query"]; ok && slow == "true" {
				slowQueries++
			}

			if stmt, ok := span.Tags["db.statement"]; ok {
				// Extract table name (simplified).
				if strings.Contains(stmt, "users") {
					queriesByTable["users"]++
				}
				if strings.Contains(stmt, "posts") {
					queriesByTable["posts"]++
				}
				if strings.Contains(stmt, "comments") {
					queriesByTable["comments"]++
				}
				if strings.Contains(stmt, "orders") {
					queriesByTable["orders"]++
				}

				// Detect sequential similar queries (N+1 indicator).
				if strings.Contains(stmt, "WHERE") && strings.Contains(stmt, "= ?") {
					baseQuery := strings.Split(stmt, "WHERE")[0]
					sequentialQueries[baseQuery]++
				}
			}
		}

		// Check for N+1 pattern in span names.
		if strings.HasPrefix(span.Name, "fetch.") && i > 0 {
			// Multiple fetch operations indicate N+1.
			if spans[i-1].Name == span.Name || strings.HasPrefix(spans[i-1].Name, "fetch.") {
				// Sequential fetches detected - analysis done above in sequentialQueries.
				sequentialQueries[span.Name]++
			}
		}
	}

	fmt.Printf("\nQuery Statistics:\n")
	fmt.Printf("  Total queries: %d\n", queryCount)
	fmt.Printf("  Slow queries (>10ms): %d\n", slowQueries)

	fmt.Printf("\nQueries by table:\n")
	for table, count := range queriesByTable {
		fmt.Printf("  %s: %d queries\n", table, count)
	}

	// Detect N+1 problems.
	fmt.Printf("\nPotential N+1 Problems Detected:\n")
	for query, count := range sequentialQueries {
		if count > 2 {
			fmt.Printf("  Query pattern '%s' executed %d times (possible N+1)\n",
				strings.TrimSpace(query), count)
		}
	}

	// Analyze query timing.
	var totalDBTime time.Duration
	for i := range spans {
		span := &spans[i]
		if strings.HasPrefix(span.Name, "db.") {
			totalDBTime += span.Duration
		}
	}
	fmt.Printf("\nTiming Analysis:\n")
	fmt.Printf("  Total time in database: %v\n", totalDBTime)
	fmt.Printf("  Average query time: %v\n", totalDBTime/time.Duration(queryCount))

	// Find transaction patterns.
	transactionCount := 0
	for i := range spans {
		span := &spans[i]
		if span.Name == DBTransactionName {
			transactionCount++
			if queriesInTx, ok := span.Tags["queries.in_transaction"]; ok {
				fmt.Printf("\nTransaction with %s queries detected\n", queriesInTx)
			}
		}
	}

	// Recommendations.
	fmt.Printf("\nRecommendations:\n")
	if slowQueries > queryCount/4 {
		fmt.Println("  - High proportion of slow queries. Consider query optimization.")
	}
	for query, count := range sequentialQueries {
		if count > 2 {
			fmt.Printf("  - Consider batching '%s' queries to avoid N+1 problem\n",
				strings.TrimSpace(query))
		}
	}
	if transactionCount > 0 && queryCount/transactionCount > 10 {
		fmt.Println("  - Consider using more transactions to ensure consistency")
	}
}

// SimulateRealisticScenario demonstrates the N+1 problem discovery story.
func SimulateRealisticScenario() {
	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("REALISTIC N+1 SCENARIO: Team Dashboard Loading Problem")
	fmt.Println(strings.Repeat("=", 70))
	fmt.Println("\nStory: Our team dashboard was 'working' but users complained it was slow.")
	fmt.Println("It loads 50 team members with their recent posts and comments.")
	fmt.Println("\nLet's see what tracez reveals...")

	// Setup tracer
	tracer := tracez.New("team-dashboard")
	defer tracer.Close()

	collector := tracez.NewCollector("dashboard-traces", 5000)
	tracer.AddCollector("collector", collector)

	// Create realistic database
	db := NewRealisticMockDB(tracer)
	userService := NewUserService(db, tracer)

	ctx := context.Background()

	// BEFORE: The slow N+1 version that "worked"
	fmt.Println("\n--- BEFORE: Original Dashboard Load ---")
	fmt.Println("Loading team dashboard the 'simple' way...")
	start := time.Now()

	ctx1, dashboardSpan := tracer.StartSpan(ctx, "dashboard.load")
	err := userService.GetUsersWithPosts(ctx1)
	dashboardSpan.Finish()

	loadTime := time.Since(start)

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("\nDashboard loaded in: %v\n", loadTime)
	fmt.Printf("Total queries executed: %d\n", db.queryCount)

	// Analyze what happened
	time.Sleep(50 * time.Millisecond)
	beforeSpans := collector.Export()

	// Count the damage
	var totalDBTime time.Duration
	queryTypes := make(map[string]int)
	for i := range beforeSpans {
		span := &beforeSpans[i]
		if strings.HasPrefix(span.Name, "db.") {
			totalDBTime += span.Duration
			if stmt, ok := span.Tags["db.statement"]; ok {
				if strings.Contains(stmt, "SELECT * FROM users") {
					queryTypes["users"]++
				} else if strings.Contains(stmt, "posts WHERE user_id") {
					queryTypes["posts"]++
				} else if strings.Contains(stmt, "comments WHERE post_id") {
					queryTypes["comments"]++
				}
			}
		}
	}

	fmt.Printf("\nTracez Analysis Reveals:\n")
	fmt.Printf("  - 1 query to fetch users\n")
	fmt.Printf("  - %d queries to fetch posts (one per user!)\n", queryTypes["posts"])
	fmt.Printf("  - %d queries to fetch comments (one per post!)\n", queryTypes["comments"])
	fmt.Printf("  - Total time in database: %v\n", totalDBTime)
	fmt.Printf("  - Database time as %% of total: %.1f%%\n", float64(totalDBTime)/float64(loadTime)*100)

	// Reset for the optimized version
	db.queryCount = 0
	collector = tracez.NewCollector("dashboard-traces-optimized", 5000)
	tracer.AddCollector("collector-optimized", collector)

	// AFTER: The optimized version
	fmt.Println("\n--- AFTER: Optimized Dashboard Load ---")
	fmt.Println("Loading dashboard with single JOIN query...")
	start = time.Now()

	ctx2, optimizedSpan := tracer.StartSpan(ctx, "dashboard.load.optimized")
	err = userService.GetUsersWithPostsOptimized(ctx2)
	optimizedSpan.Finish()

	optimizedTime := time.Since(start)

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("\nDashboard loaded in: %v\n", optimizedTime)
	fmt.Printf("Total queries executed: %d\n", db.queryCount)

	// Show the improvement
	time.Sleep(50 * time.Millisecond)
	afterSpans := collector.Export()

	var optimizedDBTime time.Duration
	for i := range afterSpans {
		span := &afterSpans[i]
		if strings.HasPrefix(span.Name, "db.") {
			optimizedDBTime += span.Duration
		}
	}

	fmt.Printf("\nOptimization Results:\n")
	fmt.Printf("  - Load time: %v → %v (%.1fx faster!)\n",
		loadTime, optimizedTime, float64(loadTime)/float64(optimizedTime))
	fmt.Printf("  - Query count: ~250 → 1\n")
	fmt.Printf("  - Database time: %v → %v\n", totalDBTime, optimizedDBTime)
	fmt.Printf("  - User experience: Sluggish → Snappy!\n")

	fmt.Println("\n" + strings.Repeat("=", 70))
	fmt.Println("LESSON: What seemed like a 'working' implementation was actually")
	fmt.Println("making 250+ database queries! Tracez made the problem visible.")
	fmt.Println(strings.Repeat("=", 70))
}

func main() {
	// First run the realistic scenario
	SimulateRealisticScenario()

	fmt.Println("\n\n" + strings.Repeat("=", 70))
	fmt.Println("TECHNICAL DEMONSTRATION: Database Pattern Detection")
	fmt.Println(strings.Repeat("=", 70))

	// Setup tracer for technical demo
	tracer := tracez.New("database-service")
	defer tracer.Close()

	collector := tracez.NewCollector("db-traces", 1000)
	tracer.AddCollector("collector", collector)

	// Create services for technical demo
	db := NewMockDB(tracer)
	userService := NewUserService(db, tracer)
	orderService := NewOrderService(db, tracer)

	ctx := context.Background()

	// Demonstrate N+1 problem.
	fmt.Println("Executing N+1 query pattern (BAD)...")
	ctx1, badSpan := tracer.StartSpan(ctx, "example.n_plus_one")
	if err := userService.GetUsersWithPosts(ctx1); err != nil {
		fmt.Printf("N+1 pattern error: %v\n", err)
	}
	badSpan.Finish()

	time.Sleep(10 * time.Millisecond)

	// Demonstrate optimized version.
	fmt.Println("Executing optimized query pattern (GOOD)...")
	ctx2, goodSpan := tracer.StartSpan(ctx, "example.optimized")
	if err := userService.GetUsersWithPostsOptimized(ctx2); err != nil {
		fmt.Printf("Optimized pattern error: %v\n", err)
	}
	goodSpan.Finish()

	time.Sleep(10 * time.Millisecond)

	// Demonstrate transaction pattern.
	fmt.Println("Processing order with transaction...")
	ctx3, orderSpan := tracer.StartSpan(ctx, "example.transaction")
	if err := orderService.ProcessOrder(ctx3, 123); err != nil {
		fmt.Printf("Transaction pattern error: %v\n", err)
	}
	orderSpan.Finish()

	time.Sleep(10 * time.Millisecond)

	// Export and analyze.
	spans := collector.Export()

	// Basic stats.
	fmt.Printf("\nTotal spans collected: %d\n", len(spans))
	fmt.Printf("Total queries executed: %d\n", db.queryCount)
	fmt.Printf("Slow queries detected: %d\n", len(db.slowQueries))

	// Pattern analysis.
	AnalyzePatterns(spans)

	// Show trace hierarchy for N+1 vs Optimized.
	fmt.Println("\n=== Trace Comparison ===")

	// Find the trace IDs for comparison.
	var badTraceID, goodTraceID string
	for i := range spans {
		span := &spans[i]
		if span.Name == "example.n_plus_one" {
			badTraceID = span.TraceID
		}
		if span.Name == "example.optimized" {
			goodTraceID = span.TraceID
		}
	}

	fmt.Println("\nN+1 Pattern Trace:")
	for i := range spans {
		span := &spans[i]
		if span.TraceID == badTraceID {
			indent := strings.Repeat("  ", countDepth(spans, *span))
			fmt.Printf("%s%s (%v)\n", indent, span.Name, span.Duration)
		}
	}

	fmt.Println("\nOptimized Pattern Trace:")
	for i := range spans {
		span := &spans[i]
		if span.TraceID == goodTraceID {
			indent := strings.Repeat("  ", countDepth(spans, *span))
			fmt.Printf("%s%s (%v)\n", indent, span.Name, span.Duration)
		}
	}
}

func countDepth(spans []tracez.Span, target tracez.Span) int {
	depth := 0
	current := target
	for current.ParentID != "" {
		for i := range spans {
			s := &spans[i]
			if s.SpanID == current.ParentID {
				depth++
				current = *s
				break
			}
		}
		if depth > 10 {
			break
		}
	}
	return depth
}
