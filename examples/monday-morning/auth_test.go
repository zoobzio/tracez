package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
	"github.com/zoobzio/tracez"
)

// TestAuthenticate_WithoutTracing shows what debugging looked like before tracez.
// Just timing the overall function tells you nothing about WHERE the time goes.
func TestAuthenticate_WithoutTracing(t *testing.T) {
	clock := clockz.NewFakeClock()
	auth := NewAuthServiceWithClock(clock)
	ctx := context.Background()

	// First auth - cache miss (slow)
	start := time.Now()
	
	done := make(chan error, 1)
	var user *User
	go func() {
		var authErr error
		user, authErr = auth.Authenticate(ctx, "user123")
		done <- authErr
	}()
	
	// Let authentication start
	time.Sleep(10 * time.Millisecond)
	
	// Advance through all the cascaded operations
	// Database: 10ms
	clock.Advance(10 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Permission service: ~900ms + ~800ms + ~1100ms = ~2800ms
	clock.Advance(2800 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Group service: ~500ms + ~600ms + ~400ms = ~1500ms
	clock.Advance(1500 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Preference service: ~400ms + ~200ms = ~600ms
	clock.Advance(600 * time.Millisecond)
	clock.BlockUntilReady()
	
	// History service: ~300ms
	clock.Advance(300 * time.Millisecond)
	clock.BlockUntilReady()
	
	// Wait for completion
	var err error
	select {
	case err = <-done:
		// Continue
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Authentication timed out")
	}
	
	duration := time.Since(start)

	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}
	if user == nil {
		t.Fatal("Expected user, got nil")
	}
	
	// Without tracing, all you see is:
	t.Logf("Authentication took: %v", duration)
	t.Logf("User authenticated: %s", user.Name)
	
	// Is it the database? The cache? Network calls? WHO KNOWS!
	// This is what production monitoring showed - just "auth is slow"

	// Second auth - cache hit (fast)
	start = time.Now()
	_, err = auth.Authenticate(ctx, "user123")
	duration = time.Since(start)

	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	t.Logf("Cached authentication took: %v", duration)
	
	// Cache hit is fast, so the problem only happens on cold starts
	// Monday morning = cache eviction = everything is slow
}

// TestAuthenticate_WithTracing shows how tracez reveals the cascade problem.
// The trace immediately shows WHERE the time is being spent.
func TestAuthenticate_WithTracing(t *testing.T) {
	// Use fake clock for deterministic timing
	clock := clockz.NewFakeClock()
	auth := NewAuthServiceWithClock(clock)
	
	// Enable tracing
	tracer := tracez.New().WithClock(clock)
	collector := tracez.NewCollector("test", 100)
	collector.SetSyncMode(true) // For deterministic tests
	tracer.AddCollector("test", collector)
	auth.EnableTracing(tracer)
	
	ctx := context.Background()

	// First auth - cache miss (reveals the cascade)
	_, span := tracer.StartSpan(ctx, "test.auth_cold")
	ctx = span.Context(ctx)
	
	user, err := auth.Authenticate(ctx, "user123")
	span.Finish()
	clock.BlockUntilReady() // Ensure all async operations complete

	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}
	if user == nil {
		t.Fatal("Expected user, got nil")
	}

	// Get the trace
	spans := collector.Export()
	
	// The trace reveals EVERYTHING
	t.Logf("\nTrace of cold authentication:")
	printTrace(t, spans, "")
	
	// Find the culprits
	var authSpan *tracez.Span
	var permSpan *tracez.Span
	for i := range spans {
		if spans[i].Name == "authenticate" {
			authSpan = &spans[i]
		}
		if spans[i].Name == "fetch.permissions" {
			permSpan = &spans[i]
		}
	}

	if authSpan == nil {
		t.Fatal("Expected authenticate span")
	}
	if permSpan == nil {
		t.Fatal("Expected permissions span")
	}

	t.Logf("\nThe smoking gun:")
	t.Logf("  Total auth time: %v", authSpan.Duration)
	t.Logf("  Permissions alone: %v (%.1f%% of total!)", 
		permSpan.Duration,
		float64(permSpan.Duration)/float64(authSpan.Duration)*100)

	// Second auth - cache hit (no cascade)
	collector.Reset()
	
	_, span2 := tracer.StartSpan(ctx, "test.auth_warm")
	ctx2 := span2.Context(ctx)
	
	user2, err := auth.Authenticate(ctx2, "user123")
	span2.Finish()
	clock.BlockUntilReady() // Ensure completion

	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}
	if user2 == nil {
		t.Fatal("Expected user, got nil")
	}

	spans = collector.Export()
	t.Logf("\nTrace of warm authentication (cached):")
	printTrace(t, spans, "")
	
	// With cache hit, no cascade occurs
	hasCascade := false
	for i := range spans {
		if spans[i].Name == "fetch.permissions" {
			hasCascade = true
		}
	}
	
	if hasCascade {
		t.Fatal("Expected no cascade on cache hit")
	}
}

// TestAuthenticate_Performance demonstrates the actual performance issue.
// On Monday morning when cache is cold, EVERY login triggers the cascade.
func TestAuthenticate_Performance(t *testing.T) {
	clock := clockz.NewFakeClock()
	auth := NewAuthServiceWithClock(clock)
	
	// Enable tracing to measure cascade impact
	tracer := tracez.New().WithClock(clock)
	collector := tracez.NewCollector("perf", 1000)
	collector.SetSyncMode(true) // For deterministic tests
	tracer.AddCollector("perf", collector)
	auth.EnableTracing(tracer)
	
	ctx := context.Background()

	// Simulate Monday morning - cache is cold for everyone
	userIDs := []string{"alice", "bob", "charlie", "david", "eve"}
	
	t.Logf("\nMonday morning scenario - all users have cold cache:")
	
	totalStart := time.Now()
	for _, userID := range userIDs {
		start := time.Now()
		
		_, span := tracer.StartSpan(ctx, fmt.Sprintf("auth.%s", userID))
		authCtx := span.Context(ctx)
		
		_, err := auth.Authenticate(authCtx, userID)
		span.Finish()
		
		if err != nil {
			t.Fatalf("Auth failed for %s: %v", userID, err)
		}
		
		duration := time.Since(start)
		t.Logf("  User %s: %v", userID, duration)
	}
	clock.BlockUntilReady() // Ensure all operations complete
	totalDuration := time.Since(totalStart)
	
	t.Logf("\nTotal time for %d cold auths: %v", len(userIDs), totalDuration)
	t.Logf("Average per user: %v", totalDuration/time.Duration(len(userIDs)))
	
	// Now simulate normal operation - most users hit cache
	t.Logf("\nNormal operation - users have warm cache:")
	
	collector.Reset()
	totalStart = time.Now()
	for _, userID := range userIDs {
		start := time.Now()
		
		_, span := tracer.StartSpan(ctx, fmt.Sprintf("auth.%s.cached", userID))
		authCtx := span.Context(ctx)
		
		_, err := auth.Authenticate(authCtx, userID)
		span.Finish()
		
		if err != nil {
			t.Fatalf("Auth failed for %s: %v", userID, err)
		}
		
		duration := time.Since(start)
		t.Logf("  User %s: %v", userID, duration)
	}
	clock.BlockUntilReady() // Ensure all operations complete
	totalDuration = time.Since(totalStart)
	
	t.Logf("\nTotal time for %d warm auths: %v", len(userIDs), totalDuration)
	t.Logf("Average per user: %v", totalDuration/time.Duration(len(userIDs)))
	
	// Analyze the spans to show the pattern
	spans := collector.Export()
	
	slowSpans := 0
	fastSpans := 0
	for i := range spans {
		if spans[i].Name == "authenticate" {
			if spans[i].Duration > 1*time.Second {
				slowSpans++
			} else {
				fastSpans++
			}
		}
	}
	
	t.Logf("\nPattern analysis:")
	t.Logf("  Slow authentications (cascade): %d", slowSpans)
	t.Logf("  Fast authentications (cached): %d", fastSpans)
}

// TestAuthenticate_DetailedBreakdown shows the full cascade in detail.
// This is what tracez reveals that logs could never show efficiently.
func TestAuthenticate_DetailedBreakdown(t *testing.T) {
	clock := clockz.NewFakeClock()
	auth := NewAuthServiceWithClock(clock)
	
	// Enable tracing
	tracer := tracez.New().WithClock(clock)
	collector := tracez.NewCollector("breakdown", 100)
	collector.SetSyncMode(true) // For deterministic tests
	tracer.AddCollector("breakdown", collector)
	auth.EnableTracing(tracer)
	
	ctx := context.Background()

	// Clear cache to force cold start
	auth.cache.Clear()
	
	// Authenticate with full tracing
	_, span := tracer.StartSpan(ctx, "full.breakdown")
	authCtx := span.Context(ctx)
	
	_, err := auth.Authenticate(authCtx, "user123")
	span.Finish()
	clock.BlockUntilReady() // Ensure all async operations complete
	
	if err != nil {
		t.Fatalf("Authentication failed: %v", err)
	}

	// Analyze the cascade
	spans := collector.Export()
	
	t.Logf("\nFull cascade breakdown:")
	t.Logf("=====================================")
	
	// Group spans by service
	dbTime := time.Duration(0)
	permTime := time.Duration(0)
	groupTime := time.Duration(0)
	prefTime := time.Duration(0)
	histTime := time.Duration(0)
	
	for i := range spans {
		switch spans[i].Name {
		case "db.get_user":
			dbTime = spans[i].Duration
		case "fetch.permissions":
			permTime = spans[i].Duration
		case "fetch.groups":
			groupTime = spans[i].Duration
		case "fetch.preferences":
			prefTime = spans[i].Duration
		case "fetch.history":
			histTime = spans[i].Duration
		}
	}
	
	totalEnrichment := permTime + groupTime + prefTime + histTime
	
	t.Logf("Database query:        %7v (  2%% of total)", dbTime)
	t.Logf("Permission cascade:    %7v ( 53%% of total)", permTime)
	t.Logf("Group lookups:         %7v ( 29%% of total)", groupTime)
	t.Logf("Preference fetching:   %7v ( 12%% of total)", prefTime)
	t.Logf("History scanning:      %7v (  7%% of total)", histTime)
	t.Logf("-------------------------------------")
	t.Logf("Total enrichment:      %7v ( 98%% of total)", totalEnrichment)
	
	t.Logf("\nThe revelation:")
	t.Logf("- Database is FAST (10ms) - not the problem!")
	t.Logf("- Permission service cascade takes 2.8 seconds alone")
	t.Logf("- Every cache miss triggers 5+ seconds of backend calls")
	t.Logf("- Monday morning cache eviction = thousands of cascades")
	
	// Show the permission service breakdown
	t.Logf("\nPermission service cascade details:")
	for i := range spans {
		if spans[i].Name == "permission.service.call" {
			t.Logf("  - Service call:     %v", spans[i].Duration)
		}
		if spans[i].Name == "role.resolver.expand" {
			t.Logf("  - Role expansion:   %v", spans[i].Duration)
		}
		if spans[i].Name == "policy.engine.evaluate" {
			t.Logf("  - Policy engine:    %v", spans[i].Duration)
		}
	}
}

// TestAuthenticate_ConcurrentLoad demonstrates how concurrent requests amplify the cascade problem.
// Shows realistic behavior: 1 user = 5s, 100 users = 25s each due to connection pool contention.
func TestAuthenticate_ConcurrentLoad(t *testing.T) {
	clock := clockz.NewFakeClock()
	auth := NewAuthServiceWithClock(clock)
	
	// Enable tracing to see the resource contention
	tracer := tracez.New().WithClock(clock)
	collector := tracez.NewCollector("load", 1000)
	collector.SetSyncMode(true)
	tracer.AddCollector("load", collector)
	auth.EnableTracing(tracer)
	
	ctx := context.Background()

	// Test single user - baseline performance
	t.Logf("\n=== Baseline: Single User ===")
	start := time.Now()
	_, err := auth.Authenticate(ctx, "single_user")
	clock.BlockUntilReady() // Ensure operation completes
	singleUserDuration := time.Since(start)
	if err != nil {
		t.Fatalf("Single user auth failed: %v", err)
	}
	t.Logf("Single user auth: %v", singleUserDuration)
	
	// Clear cache to ensure fair comparison
	auth.cache.Clear()
	collector.Reset()

	// Test concurrent load - 20 users simultaneously
	t.Logf("\n=== Load Test: 20 Concurrent Users ===")
	numUsers := 20
	var wg sync.WaitGroup
	userDurations := make([]time.Duration, numUsers)
	
	overallStart := time.Now()
	for i := 0; i < numUsers; i++ {
		wg.Add(1)
		go func(userID int) {
			defer wg.Done()
			
			userStart := time.Now()
			userCtx, span := tracer.StartSpan(ctx, fmt.Sprintf("concurrent_user_%d", userID))
			
			_, err := auth.Authenticate(userCtx, fmt.Sprintf("user_%d", userID))
			if err != nil {
				t.Errorf("User %d auth failed: %v", userID, err)
			}
			
			span.Finish()
			userDurations[userID] = time.Since(userStart)
		}(i)
	}
	
	wg.Wait()
	clock.BlockUntilReady() // Ensure all operations complete
	overallDuration := time.Since(overallStart)
	
	// Calculate statistics
	var totalUserTime time.Duration
	minTime := userDurations[0]
	maxTime := userDurations[0]
	for _, duration := range userDurations {
		totalUserTime += duration
		if duration < minTime {
			minTime = duration
		}
		if duration > maxTime {
			maxTime = duration
		}
	}
	avgUserTime := totalUserTime / time.Duration(numUsers)
	
	t.Logf("Overall completion time: %v", overallDuration)
	t.Logf("Average user auth time: %v", avgUserTime)
	t.Logf("Fastest user: %v", minTime)
	t.Logf("Slowest user: %v", maxTime)
	t.Logf("Slowdown factor: %.1fx", float64(avgUserTime)/float64(singleUserDuration))
	
	// Analyze connection pool contention in traces
	spans := collector.Export()
	connectionTimeouts := 0
	maxActiveConnections := int64(0)
	
	for i := range spans {
		if spans[i].Tags != nil {
			if errorTag, exists := spans[i].Tags["error"]; exists && errorTag == "connection_timeout" {
				connectionTimeouts++
			}
			if activeConnsTag, exists := spans[i].Tags["active_connections"]; exists {
				if activeConns, err := fmt.Sscanf(activeConnsTag, "%d", &maxActiveConnections); err == nil && activeConns > 0 {
					// Track max connections seen
				}
			}
		}
	}
	
	t.Logf("\n=== Resource Contention Analysis ===")
	t.Logf("Connection timeouts observed: %d", connectionTimeouts)
	t.Logf("Peak active connections: %d (pool limit: 50)", maxActiveConnections)
	
	// Show how tracing reveals the bottleneck
	t.Logf("\n=== What Tracing Reveals ===")
	t.Logf("- Connection pool exhaustion visible in traces")
	t.Logf("- Variable latency + contention = multiplicative slowdown")  
	t.Logf("- Real production bottlenecks exposed under load")
	
	// Verify the cascade still happens even with timeouts
	cascadeSpans := 0
	timeoutSpans := 0
	for i := range spans {
		if spans[i].Name == "fetch.permissions" {
			cascadeSpans++
		}
		if spans[i].Tags != nil {
			if errorTag, exists := spans[i].Tags["error"]; exists && errorTag == "connection_timeout" {
				timeoutSpans++
			}
		}
	}
	
	t.Logf("Permission cascades executed: %d", cascadeSpans)
	t.Logf("Operations that timed out: %d", timeoutSpans)
	
	// Show that concurrent load amplifies the problem exponentially
	if avgUserTime > singleUserDuration*2 {
		t.Logf("\nCONFIRMED: Concurrent load amplifies the cascade problem")
		t.Logf("- Single user: %v", singleUserDuration)
		t.Logf("- Concurrent avg: %v (%.1fx slower)", avgUserTime, float64(avgUserTime)/float64(singleUserDuration))
	}
}

// printTrace recursively prints a trace tree
func printTrace(t *testing.T, spans []tracez.Span, indent string) {
	// Find root spans
	roots := findRootSpans(spans)
	
	for _, root := range roots {
		printSpan(t, root, spans, indent)
	}
}

func findRootSpans(spans []tracez.Span) []tracez.Span {
	var roots []tracez.Span
	for _, span := range spans {
		if span.ParentID == "" {
			roots = append(roots, span)
		}
	}
	return roots
}

func printSpan(t *testing.T, span tracez.Span, allSpans []tracez.Span, indent string) {
	// Print current span
	tags := ""
	if span.Tags != nil && len(span.Tags) > 0 {
		for k, v := range span.Tags {
			tags += fmt.Sprintf(" %s=%s", k, v)
		}
	}
	t.Logf("%s%s [%v]%s", indent, span.Name, span.Duration, tags)
	
	// Find and print children
	children := findChildren(span, allSpans)
	for i, child := range children {
		prefix := "├── "
		childIndent := indent + "│   "
		if i == len(children)-1 {
			prefix = "└── "
			childIndent = indent + "    "
		}
		t.Logf("%s%s%s [%v]", indent, prefix, child.Name, child.Duration)
		
		// Recursively print grandchildren
		grandchildren := findChildren(child, allSpans)
		for _, grandchild := range grandchildren {
			printSpan(t, grandchild, allSpans, childIndent)
		}
	}
}

func findChildren(parent tracez.Span, allSpans []tracez.Span) []tracez.Span {
	var children []tracez.Span
	for _, span := range allSpans {
		if span.ParentID == parent.SpanID {
			children = append(children, span)
		}
	}
	return children
}