package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

func TestMockDB(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	db := NewMockDB(tracer)
	ctx := context.Background()

	// Test basic query.
	results, err := db.Query(ctx, "SELECT * FROM users")
	if err != nil {
		t.Errorf("Query failed: %v", err)
	}
	if len(results) != 50 {
		t.Errorf("Expected 50 results, got %d", len(results))
	}

	// Test query count.
	if db.queryCount != 1 {
		t.Errorf("Expected query count 1, got %d", db.queryCount)
	}

	// Test slow query detection.
	db.Query(ctx, "SELECT * FROM users JOIN posts ON users.id = posts.user_id")
	if len(db.slowQueries) != 1 {
		t.Errorf("Expected 1 slow query, got %d", len(db.slowQueries))
	}

	// Check spans.
	time.Sleep(10 * time.Millisecond)
	spans := collector.Export()

	dbSpans := 0
	for _, span := range spans {
		if span.Name == "db.query" {
			dbSpans++
		}
	}
	if dbSpans != 2 {
		t.Errorf("Expected 2 db.query spans, got %d", dbSpans)
	}
}

func TestNPlusOnePattern(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)

	db := NewMockDB(tracer)
	service := NewUserService(db, tracer)

	ctx := context.Background()

	// Execute N+1 pattern.
	err := service.GetUsersWithPosts(ctx)
	if err != nil {
		t.Errorf("GetUsersWithPosts failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	spans := collector.Export()

	// Count queries.
	queryCount := 0
	fetchPostsCount := 0
	fetchCommentsCount := 0

	for _, span := range spans {
		t.Logf("Span: %s (parent: %s)", span.Name, span.ParentID)
		if span.Name == "db.query" {
			queryCount++
		}
		if strings.HasPrefix(span.Name, "fetch.posts.") {
			fetchPostsCount++
		}
		if strings.HasPrefix(span.Name, "fetch.comments.") {
			fetchCommentsCount++
		}
	}

	// Should have 1 initial query + N post queries + M comment queries.
	if queryCount < 3 {
		t.Errorf("Expected at least 3 queries for N+1 pattern, got %d", queryCount)
	}

	if fetchPostsCount < 2 {
		t.Errorf("Expected at least 2 fetch.posts spans, got %d", fetchPostsCount)
	}

	if fetchCommentsCount < 2 {
		t.Errorf("Expected at least 2 fetch.comments spans, got %d", fetchCommentsCount)
	}
}

func TestOptimizedPattern(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	db := NewMockDB(tracer)
	service := NewUserService(db, tracer)

	ctx := context.Background()

	// Execute optimized pattern.
	err := service.GetUsersWithPostsOptimized(ctx)
	if err != nil {
		t.Errorf("GetUsersWithPostsOptimized failed: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	spans := collector.Export()

	// Should have exactly 1 query (the JOIN).
	queryCount := 0
	batchQueryFound := false

	for _, span := range spans {
		if span.Name == "db.query" {
			queryCount++
			if stmt, ok := span.Tags["db.statement"]; ok {
				if strings.Contains(stmt, "JOIN") {
					batchQueryFound = true
				}
			}
		}
		if span.Name == "db.batch_query" {
			if span.Tags["query.type"] != "batch_join" {
				t.Error("Expected batch_join query type")
			}
		}
	}

	if queryCount != 1 {
		t.Errorf("Expected 1 query for optimized pattern, got %d", queryCount)
	}

	if !batchQueryFound {
		t.Error("Expected JOIN query not found")
	}
}

func TestTransactionPattern(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	db := NewMockDB(tracer)
	service := NewOrderService(db, tracer)

	ctx := context.Background()

	// Process order.
	err := service.ProcessOrder(ctx, 456)
	if err != nil {
		t.Errorf("ProcessOrder failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	spans := collector.Export()

	// Find transaction span.
	var txSpan *tracez.Span
	for i := range spans {
		if spans[i].Name == "db.transaction" {
			txSpan = &spans[i]
			break
		}
	}

	if txSpan == nil {
		t.Fatal("Transaction span not found")
	}

	// Check transaction metadata.
	if txSpan.Tags["queries.in_transaction"] == "" {
		t.Error("Transaction should track number of queries")
	}

	// Count queries within transaction.
	txQueryCount := 0
	for _, span := range spans {
		if span.Name == "db.query" && span.ParentID != "" {
			// Check if query is within transaction.
			parent := findSpanByID(spans, span.ParentID)
			if parent != nil && parent.Name == "db.transaction" {
				txQueryCount++
			}
		}
	}

	if txQueryCount < 4 {
		t.Errorf("Expected at least 4 queries in transaction, got %d", txQueryCount)
	}
}

func TestSlowQueryDetection(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	db := NewMockDB(tracer)
	ctx := context.Background()

	// Execute different query types.
	queries := []struct {
		sql      string
		expected bool // expect slow query.
	}{
		{"SELECT * FROM users WHERE id = ?", false},
		{"SELECT COUNT(*) FROM orders", true},
		{"SELECT * FROM users JOIN posts ON users.id = posts.user_id", true},
		{"SELECT name FROM products", false},
	}

	for _, q := range queries {
		db.Query(ctx, q.sql, 1)
	}

	time.Sleep(100 * time.Millisecond)
	spans := collector.Export()

	// Count slow queries in spans.
	slowCount := 0
	for _, span := range spans {
		if slow, ok := span.Tags["db.slow_query"]; ok && slow == "true" {
			slowCount++
		}
	}

	expectedSlowCount := 0
	for _, q := range queries {
		if q.expected {
			expectedSlowCount++
		}
	}

	if slowCount != expectedSlowCount {
		t.Errorf("Expected %d slow queries, got %d", expectedSlowCount, slowCount)
	}
}

func TestPatternAnalysis(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)

	db := NewMockDB(tracer)
	ctx := context.Background()

	// Create N+1 pattern.
	for i := 0; i < 5; i++ {
		db.Query(ctx, "SELECT * FROM posts WHERE user_id = ?", i)
	}

	// Create normal queries.
	db.Query(ctx, "SELECT * FROM users")
	db.Query(ctx, "SELECT COUNT(*) FROM orders")

	time.Sleep(50 * time.Millisecond)
	spans := collector.Export()

	// The AnalyzePatterns function should detect the N+1.
	// We can't easily test console output, but we can verify the data exists.

	sequentialCount := 0
	for _, span := range spans {
		if stmt, ok := span.Tags["db.statement"]; ok {
			if strings.Contains(stmt, "posts WHERE user_id = ?") {
				sequentialCount++
			}
		}
	}

	if sequentialCount != 5 {
		t.Errorf("Expected 5 sequential similar queries, got %d", sequentialCount)
	}
}

func TestQueryTiming(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	db := NewMockDB(tracer)
	ctx := context.Background()

	// Execute queries with known timing.
	db.Query(ctx, "SELECT * FROM users WHERE id = ?", 1)                        // Fast: 2ms.
	db.Query(ctx, "SELECT * FROM users JOIN posts ON users.id = posts.user_id") // Slow: 20ms.

	time.Sleep(50 * time.Millisecond)
	spans := collector.Export()

	for _, span := range spans {
		if span.Name == "db.query" {
			stmt := span.Tags["db.statement"]
			if strings.Contains(stmt, "JOIN") {
				if span.Duration < 15*time.Millisecond {
					t.Error("JOIN query should be slow (>15ms)")
				}
			} else if strings.Contains(stmt, "WHERE id =") {
				if span.Duration > 10*time.Millisecond {
					t.Errorf("Simple WHERE query should be fast (<10ms), got %v", span.Duration)
				}
			}
		}
	}
}

// Helper function.
func findSpanByID(spans []tracez.Span, id string) *tracez.Span {
	for i := range spans {
		if spans[i].SpanID == id {
			return &spans[i]
		}
	}
	return nil
}
