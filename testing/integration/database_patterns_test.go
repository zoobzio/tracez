package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// MockDatabase simulates database operations with tracing.
type MockDatabase struct {
	name           string
	tracer         *tracez.Tracer
	connectionPool int
	activeConns    int
	mu             sync.Mutex
	slowQueryTime  time.Duration
}

// NewMockDatabase creates a simulated database.
func NewMockDatabase(name string, tracer *tracez.Tracer, poolSize int) *MockDatabase {
	return &MockDatabase{
		name:           name,
		tracer:         tracer,
		connectionPool: poolSize,
		slowQueryTime:  50 * time.Millisecond,
	}
}

// Query executes a simulated query with tracing.
func (db *MockDatabase) Query(ctx context.Context, query string, duration time.Duration) error {
	// Check connection pool.
	db.mu.Lock()
	if db.activeConns >= db.connectionPool {
		db.mu.Unlock()
		return fmt.Errorf("connection pool exhausted")
	}
	db.activeConns++
	db.mu.Unlock()

	defer func() {
		db.mu.Lock()
		db.activeConns--
		db.mu.Unlock()
	}()

	// Start query span.
	_, span := db.tracer.StartSpan(ctx, fmt.Sprintf("%s.query", db.name))
	defer span.Finish()

	span.SetTag("db.type", "sql")
	span.SetTag("db.instance", db.name)
	span.SetTag("db.statement", query)

	// Mark slow queries.
	if duration > db.slowQueryTime {
		span.SetTag("slow_query", "true")
		span.SetTag("query_time_ms", fmt.Sprintf("%.2f", duration.Seconds()*1000))
	}

	// Simulate query execution.
	time.Sleep(duration)

	return nil
}

// Transaction executes operations in a transaction.
func (db *MockDatabase) Transaction(ctx context.Context, operations func(context.Context) error) error {
	// Start transaction span.
	ctx, txSpan := db.tracer.StartSpan(ctx, fmt.Sprintf("%s.transaction", db.name))
	defer txSpan.Finish()

	txSpan.SetTag("db.type", "sql")
	txSpan.SetTag("db.instance", db.name)
	txSpan.SetTag("transaction", "true")

	// Begin transaction.
	_, beginSpan := db.tracer.StartSpan(ctx, "BEGIN")
	time.Sleep(2 * time.Millisecond)
	beginSpan.Finish()

	// Execute operations.
	err := operations(ctx)

	if err != nil {
		// Rollback on error.
		_, rollbackSpan := db.tracer.StartSpan(ctx, "ROLLBACK")
		rollbackSpan.SetTag("reason", err.Error())
		time.Sleep(5 * time.Millisecond)
		rollbackSpan.Finish()

		txSpan.SetTag("status", "rolled_back")
		return err
	}

	// Commit on success.
	_, commitSpan := db.tracer.StartSpan(ctx, "COMMIT")
	time.Sleep(10 * time.Millisecond)
	commitSpan.Finish()

	txSpan.SetTag("status", "committed")
	return nil
}

// TestConnectionPoolExhaustion demonstrates database connection pool saturation.
func TestConnectionPoolExhaustion(t *testing.T) {
	tracer := tracez.New("db-pool-test")
	collector := NewMockCollector(t, "pool", 1000)
	tracer.AddCollector("test", collector.Collector)
	defer tracer.Close()

	// Create database with small pool.
	db := NewMockDatabase("postgres", tracer, 3)

	ctx := context.Background()

	// Launch concurrent queries exceeding pool size.
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			queryCtx, querySpan := tracer.StartSpan(ctx, fmt.Sprintf("query-%d", id))
			querySpan.SetTag("query_id", fmt.Sprintf("%d", id))

			// Try to execute query.
			err := db.Query(queryCtx, "SELECT * FROM users", 20*time.Millisecond)
			if err != nil {
				querySpan.SetTag("error", "true")
				querySpan.SetTag("error.message", err.Error())
				errors <- err
			} else {
				querySpan.SetTag("success", "true")
			}

			querySpan.Finish()
		}(i)
	}

	wg.Wait()
	close(errors)

	// Count pool exhaustion errors.
	exhaustionCount := 0
	for err := range errors {
		if err.Error() == "connection pool exhausted" {
			exhaustionCount++
		}
	}

	// Should have some exhaustion due to pool limit.
	if exhaustionCount == 0 {
		t.Error("No connection pool exhaustion detected despite overload")
	}

	// Verify traces show the issue.
	spans := collector.Export()
	poolErrors := 0
	for _, span := range spans {
		if span.Tags["error.message"] == "connection pool exhausted" {
			poolErrors++
		}
	}

	if poolErrors != exhaustionCount {
		t.Errorf("Trace pool errors (%d) don't match actual errors (%d)",
			poolErrors, exhaustionCount)
	}

	t.Logf("Connection pool test: %d queries, %d exhaustion errors", 10, exhaustionCount)
}

// TestDistributedTransaction demonstrates 2PC across multiple databases.
func TestDistributedTransaction(t *testing.T) {
	tracer := tracez.New("2pc-coordinator")
	collector := NewMockCollector(t, "2pc", 1000)
	tracer.AddCollector("test", collector.Collector)
	defer tracer.Close()

	// Create multiple databases.
	userDB := NewMockDatabase("user-db", tracer, 10)
	orderDB := NewMockDatabase("order-db", tracer, 10)
	inventoryDB := NewMockDatabase("inventory-db", tracer, 10)

	ctx := context.Background()

	// Start distributed transaction.
	ctx, dtxSpan := tracer.StartSpan(ctx, "distributed-transaction")
	dtxSpan.SetTag("transaction_id", "dtx-789")
	dtxSpan.SetTag("protocol", "2PC")

	// Phase 1: Prepare.
	_, prepareSpan := tracer.StartSpan(ctx, "prepare-phase")
	prepareSpan.SetTag("phase", "prepare")

	prepareResults := make(map[string]bool)
	var prepareMu sync.Mutex
	var prepareWg sync.WaitGroup

	// Prepare each participant.
	participants := []struct {
		name string
		db   *MockDatabase
	}{
		{"user", userDB},
		{"order", orderDB},
		{"inventory", inventoryDB},
	}

	for _, p := range participants {
		prepareWg.Add(1)
		go func(name string, db *MockDatabase) {
			defer prepareWg.Done()

			_, pSpan := tracer.StartSpan(ctx, fmt.Sprintf("prepare-%s", name))
			pSpan.SetTag("participant", name)

			// Simulate prepare.
			err := db.Query(ctx, "PREPARE TRANSACTION", 10*time.Millisecond)

			prepareMu.Lock()
			prepareResults[name] = err == nil
			prepareMu.Unlock()

			if err == nil {
				pSpan.SetTag("vote", "commit")
			} else {
				pSpan.SetTag("vote", "abort")
			}
			pSpan.Finish()
		}(p.name, p.db)
	}

	prepareWg.Wait()
	prepareSpan.Finish()

	// Check prepare results.
	allPrepared := true
	for _, prepared := range prepareResults {
		if !prepared {
			allPrepared = false
			break
		}
	}

	// Phase 2: Commit or Abort.
	_, decisionSpan := tracer.StartSpan(ctx, "decision-phase")
	if allPrepared {
		decisionSpan.SetTag("decision", "commit")
		decisionSpan.SetTag("phase", "commit")

		// Commit all participants.
		var commitWg sync.WaitGroup
		for _, p := range participants {
			commitWg.Add(1)
			go func(name string, db *MockDatabase) {
				defer commitWg.Done()

				_, cSpan := tracer.StartSpan(ctx, fmt.Sprintf("commit-%s", name))
				cSpan.SetTag("participant", name)
				db.Query(ctx, "COMMIT PREPARED", 15*time.Millisecond)
				cSpan.Finish()
			}(p.name, p.db)
		}
		commitWg.Wait()

	} else {
		decisionSpan.SetTag("decision", "abort")
		decisionSpan.SetTag("phase", "abort")

		// Abort all participants.
		var abortWg sync.WaitGroup
		for _, p := range participants {
			abortWg.Add(1)
			go func(name string, db *MockDatabase) {
				defer abortWg.Done()

				_, aSpan := tracer.StartSpan(ctx, fmt.Sprintf("abort-%s", name))
				aSpan.SetTag("participant", name)
				db.Query(ctx, "ROLLBACK PREPARED", 5*time.Millisecond)
				aSpan.Finish()
			}(p.name, p.db)
		}
		abortWg.Wait()
	}
	decisionSpan.Finish()

	dtxSpan.SetTag("outcome", fmt.Sprintf("%v", allPrepared))
	dtxSpan.Finish()

	// Verify 2PC protocol in traces.
	spans := collector.Export()
	analyzer := NewTraceAnalyzer(spans)

	// Check prepare phase.
	preparePhaseSpans := analyzer.GetSpansByName("prepare-phase")
	if len(preparePhaseSpans) != 1 {
		t.Error("Missing prepare phase span")
	}

	// Check all participants voted.
	for _, p := range participants {
		prepareSpans := analyzer.GetSpansByName(fmt.Sprintf("prepare-%s", p.name))
		if len(prepareSpans) != 1 {
			t.Errorf("Missing prepare span for %s", p.name)
		}
	}

	// Check decision phase.
	decisionPhaseSpans := analyzer.GetSpansByName("decision-phase")
	if len(decisionPhaseSpans) != 1 {
		t.Error("Missing decision phase span")
	}

	t.Logf("2PC transaction completed with outcome: %v", allPrepared)
}

// TestQueryOptimization shows query plan changes based on statistics.
func TestQueryOptimization(t *testing.T) {
	tracer := tracez.New("query-optimizer")
	collector := NewMockCollector(t, "optimizer", 1000)
	tracer.AddCollector("test", collector.Collector)
	defer tracer.Close()

	db := NewMockDatabase("analytics-db", tracer, 10)
	ctx := context.Background()

	// Simulate query with different execution plans.
	queries := []struct {
		query       string
		plan        string
		duration    time.Duration
		indexUsed   bool
		rowsScanned int
	}{
		{
			query:       "SELECT * FROM orders WHERE status = 'pending'",
			plan:        "SeqScan",
			duration:    100 * time.Millisecond,
			indexUsed:   false,
			rowsScanned: 100000,
		},
		{
			query:       "SELECT * FROM orders WHERE status = 'pending'",
			plan:        "IndexScan",
			duration:    10 * time.Millisecond,
			indexUsed:   true,
			rowsScanned: 500,
		},
		{
			query:       "SELECT * FROM orders WHERE created_at > NOW() - INTERVAL '1 day'",
			plan:        "BitmapHeapScan",
			duration:    25 * time.Millisecond,
			indexUsed:   true,
			rowsScanned: 5000,
		},
	}

	for i, q := range queries {
		// Execute query with plan details.
		_, querySpan := tracer.StartSpan(ctx, fmt.Sprintf("query-%d", i))
		querySpan.SetTag("query", q.query)
		querySpan.SetTag("execution_plan", q.plan)
		querySpan.SetTag("index_used", fmt.Sprintf("%v", q.indexUsed))
		querySpan.SetTag("rows_scanned", fmt.Sprintf("%d", q.rowsScanned))

		// Explain plan.
		_, explainSpan := tracer.StartSpan(ctx, "EXPLAIN")
		explainSpan.SetTag("plan_type", q.plan)
		explainSpan.SetTag("estimated_cost", fmt.Sprintf("%.2f", float64(q.rowsScanned)/1000))
		time.Sleep(2 * time.Millisecond)
		explainSpan.Finish()

		// Execute query.
		db.Query(ctx, q.query, q.duration)

		// Calculate efficiency.
		efficiency := "poor"
		if q.indexUsed && q.rowsScanned < 1000 {
			efficiency = "excellent"
		} else if q.indexUsed {
			efficiency = "good"
		}
		querySpan.SetTag("efficiency", efficiency)

		querySpan.Finish()

		// Small gap between queries.
		time.Sleep(5 * time.Millisecond)
	}

	// Analyze query performance.
	spans := collector.Export()

	// Find slow queries.
	slowQueries := 0
	optimizedQueries := 0

	for _, span := range spans {
		if span.Tags["slow_query"] == "true" {
			slowQueries++
		}
		if span.Tags["execution_plan"] == "IndexScan" {
			optimizedQueries++
		}
	}

	// Verify we detected optimization.
	if slowQueries == 0 {
		t.Error("No slow queries detected")
	}
	if optimizedQueries == 0 {
		t.Error("No optimized queries detected")
	}

	t.Logf("Query optimization: %d slow, %d optimized", slowQueries, optimizedQueries)
}

// TestDatabaseReplication demonstrates primary-replica lag monitoring.
func TestDatabaseReplication(t *testing.T) {
	tracer := tracez.New("replication-monitor")
	collector := NewMockCollector(t, "replication", 1000)
	tracer.AddCollector("test", collector.Collector)
	defer tracer.Close()

	primary := NewMockDatabase("primary", tracer, 10)
	replica1 := NewMockDatabase("replica-1", tracer, 10)
	replica2 := NewMockDatabase("replica-2", tracer, 10)

	ctx := context.Background()

	// Simulate write to primary with replication.
	ctx, writeSpan := tracer.StartSpan(ctx, "write-operation")
	writeSpan.SetTag("operation", "INSERT")
	writeSpan.SetTag("table", "events")

	// Write to primary.
	err := primary.Transaction(ctx, func(txCtx context.Context) error {
		return primary.Query(txCtx, "INSERT INTO events", 15*time.Millisecond)
	})

	if err != nil {
		t.Fatalf("Primary write failed: %v", err)
	}

	writeTime := time.Now()
	writeSpan.SetTag("write_time", writeTime.Format(time.RFC3339))
	writeSpan.Finish()

	// Simulate replication lag.
	replicationLags := []struct {
		replica *MockDatabase
		lag     time.Duration
	}{
		{replica1, 50 * time.Millisecond},
		{replica2, 150 * time.Millisecond},
	}

	var wg sync.WaitGroup
	for _, r := range replicationLags {
		wg.Add(1)
		go func(replica *MockDatabase, lag time.Duration) {
			defer wg.Done()

			// Wait for replication lag.
			time.Sleep(lag)

			// Replica receives write.
			replicaCtx, replicaSpan := tracer.StartSpan(ctx, fmt.Sprintf("%s-replication", replica.name))
			replicaSpan.SetTag("role", "replica")
			replicaSpan.SetTag("lag_ms", fmt.Sprintf("%.2f", lag.Seconds()*1000))

			// Apply replicated write.
			replica.Query(replicaCtx, "APPLY REPLICATED WRITE", 10*time.Millisecond)

			// Check if lag is acceptable.
			if lag > 100*time.Millisecond {
				replicaSpan.SetTag("lag_warning", "high")
			}

			replicaSpan.Finish()
		}(r.replica, r.lag)
	}

	wg.Wait()

	// Read from replicas with consistency checks.
	for i, r := range replicationLags {
		readCtx, readSpan := tracer.StartSpan(ctx, fmt.Sprintf("read-replica-%d", i+1))
		readSpan.SetTag("replica", r.replica.name)
		readSpan.SetTag("expected_lag", fmt.Sprintf("%.2f", r.lag.Seconds()*1000))

		// Attempt read.
		err := r.replica.Query(readCtx, "SELECT * FROM events", 5*time.Millisecond)
		if err != nil {
			readSpan.SetTag("error", err.Error())
		}

		// Check if data is stale.
		if r.lag > 100*time.Millisecond {
			readSpan.SetTag("consistency", "eventual")
			readSpan.SetTag("stale_read_risk", "true")
		} else {
			readSpan.SetTag("consistency", "near-realtime")
		}

		readSpan.Finish()
	}

	// Verify replication monitoring.
	spans := collector.Export()

	highLagCount := 0
	replicationSpans := 0

	for _, span := range spans {
		if span.Tags["lag_warning"] == "high" {
			highLagCount++
		}
		if span.Tags["role"] == "replica" {
			replicationSpans++
		}
	}

	if replicationSpans != 2 {
		t.Errorf("Expected 2 replication spans, got %d", replicationSpans)
	}

	if highLagCount == 0 {
		t.Error("No high lag warnings despite 150ms lag")
	}

	t.Logf("Replication test: %d replicas, %d with high lag", replicationSpans, highLagCount)
}

// TestCachingStrategy demonstrates cache hits/misses with database fallback.
func TestCachingStrategy(t *testing.T) {
	tracer := tracez.New("cache-layer")
	collector := NewMockCollector(t, "cache", 1000)
	tracer.AddCollector("test", collector.Collector)
	defer tracer.Close()

	db := NewMockDatabase("postgres", tracer, 10)

	// Simple cache implementation.
	cache := &struct {
		data map[string]string
		mu   sync.RWMutex
	}{
		data: make(map[string]string),
	}

	ctx := context.Background()

	// Function to get data with cache.
	getData := func(key string) (string, error) {
		getCtx, getSpan := tracer.StartSpan(ctx, "get-data")
		defer getSpan.Finish()
		getSpan.SetTag("key", key)

		// Try cache first.
		_, cacheSpan := tracer.StartSpan(getCtx, "cache-lookup")
		cache.mu.RLock()
		value, found := cache.data[key]
		cache.mu.RUnlock()

		if found {
			cacheSpan.SetTag("result", "hit")
			cacheSpan.SetTag("cached", "true")
			cacheSpan.Finish()
			getSpan.SetTag("source", "cache")
			return value, nil
		}

		cacheSpan.SetTag("result", "miss")
		cacheSpan.Finish()

		// Cache miss - query database.
		_, dbSpan := tracer.StartSpan(getCtx, "db-fallback")
		dbSpan.SetTag("reason", "cache-miss")

		err := db.Query(getCtx, fmt.Sprintf("SELECT * FROM data WHERE key='%s'", key), 30*time.Millisecond)
		if err != nil {
			dbSpan.SetTag("error", err.Error())
			dbSpan.Finish()
			return "", err
		}

		// Simulate getting value.
		value = fmt.Sprintf("value-%s", key)
		dbSpan.Finish()

		// Update cache.
		_, updateSpan := tracer.StartSpan(getCtx, "cache-update")
		cache.mu.Lock()
		cache.data[key] = value
		cache.mu.Unlock()
		updateSpan.SetTag("action", "set")
		updateSpan.Finish()

		getSpan.SetTag("source", "database")
		return value, nil
	}

	// Test cache behavior with multiple requests.
	keys := []string{"user-1", "user-2", "user-1", "user-3", "user-2", "user-1"}

	for _, key := range keys {
		_, err := getData(key)
		if err != nil {
			t.Errorf("Failed to get data for %s: %v", key, err)
		}

		// Small delay between requests.
		time.Sleep(5 * time.Millisecond)
	}

	// Analyze cache performance.
	spans := collector.Export()

	hits := 0
	misses := 0
	dbQueries := 0

	for _, span := range spans {
		switch span.Name {
		case "cache-lookup":
			if span.Tags["result"] == "hit" {
				hits++
			} else if span.Tags["result"] == "miss" {
				misses++
			}
		case "db-fallback":
			dbQueries++
		}
	}

	// Verify cache is working.
	if hits == 0 {
		t.Error("No cache hits detected")
	}

	if dbQueries >= len(keys) {
		t.Error("Too many DB queries - cache not working")
	}

	// Calculate hit rate.
	totalLookups := hits + misses
	hitRate := float64(hits) / float64(totalLookups) * 100

	t.Logf("Cache performance: %.1f%% hit rate (%d hits, %d misses, %d DB queries)",
		hitRate, hits, misses, dbQueries)
}
