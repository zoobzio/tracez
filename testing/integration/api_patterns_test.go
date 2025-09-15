package integration

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// MockAPIGateway simulates an API gateway with routing and middleware.
type MockAPIGateway struct {
	tracer     *tracez.Tracer
	routes     map[string]*MockService
	middleware []string
	mu         sync.RWMutex
}

// NewMockAPIGateway creates a simulated API gateway.
func NewMockAPIGateway(tracer *tracez.Tracer) *MockAPIGateway {
	return &MockAPIGateway{
		tracer:     tracer,
		routes:     make(map[string]*MockService),
		middleware: []string{"auth", "rate-limit", "cors", "logging"},
	}
}

// RegisterRoute adds a service for a route pattern.
func (g *MockAPIGateway) RegisterRoute(pattern string, service *MockService) {
	g.mu.Lock()
	g.routes[pattern] = service
	g.mu.Unlock()
}

// HandleRequest processes a request through middleware and routing.
func (g *MockAPIGateway) HandleRequest(ctx context.Context, method, path string, headers map[string]string) error {
	// Start request span.
	ctx, reqSpan := g.tracer.StartSpan(ctx, "api-request")
	defer reqSpan.Finish()

	reqSpan.SetTag("http.method", method)
	reqSpan.SetTag("http.path", path)
	reqSpan.SetTag("http.url", fmt.Sprintf("https://api.example.com%s", path))

	// Process through middleware chain.
	for _, mw := range g.middleware {
		_, mwSpan := g.tracer.StartSpan(ctx, fmt.Sprintf("middleware.%s", mw))
		mwSpan.SetTag("middleware", mw)

		// Simulate middleware processing.
		switch mw {
		case "auth":
			if token, exists := headers["Authorization"]; exists {
				mwSpan.SetTag("auth.token", token[:10]+"...")
				mwSpan.SetTag("auth.user", "user-123")
			}
		case "rate-limit":
			mwSpan.SetTag("rate.limit", "1000")
			mwSpan.SetTag("rate.remaining", fmt.Sprintf("%d", rand.Intn(1000)))
		case "cors":
			mwSpan.SetTag("cors.origin", headers["Origin"])
		}

		time.Sleep(2 * time.Millisecond)
		mwSpan.Finish()
	}

	// Route to service.
	g.mu.RLock()
	service, exists := g.routes[path]
	g.mu.RUnlock()

	if !exists {
		reqSpan.SetTag("http.status_code", "404")
		return fmt.Errorf("route not found: %s", path)
	}

	// Call backend service.
	err := service.Call(ctx, method)
	if err != nil {
		reqSpan.SetTag("http.status_code", "500")
		reqSpan.SetTag("error", "true")
		return err
	}

	reqSpan.SetTag("http.status_code", "200")
	return nil
}

// TestAPIGatewayRouting demonstrates request routing through an API gateway.
func TestAPIGatewayRouting(t *testing.T) {
	tracer := tracez.New("api-gateway")
	collector := NewMockCollector(t, "gateway", 1000)
	tracer.AddCollector("api", collector.Collector)
	defer tracer.Close()

	// Create gateway and services.
	gateway := NewMockAPIGateway(tracer)
	userService := NewMockService("user-service", tracer)
	orderService := NewMockService("order-service", tracer)
	productService := NewMockService("product-service", tracer)

	// Register routes.
	gateway.RegisterRoute("/api/users", userService)
	gateway.RegisterRoute("/api/orders", orderService)
	gateway.RegisterRoute("/api/products", productService)

	ctx := context.Background()

	// Simulate various API requests.
	requests := []struct {
		method  string
		path    string
		headers map[string]string
	}{
		{
			method: "GET",
			path:   "/api/users",
			headers: map[string]string{
				"Authorization": "Bearer token123456789",
				"Origin":        "https://frontend.example.com",
			},
		},
		{
			method: "POST",
			path:   "/api/orders",
			headers: map[string]string{
				"Authorization": "Bearer token987654321",
				"Content-Type":  "application/json",
			},
		},
		{
			method: "GET",
			path:   "/api/products",
			headers: map[string]string{
				"Authorization": "Bearer tokenABCDEFGHI",
			},
		},
		{
			method: "GET",
			path:   "/api/unknown", // 404 route.
			headers: map[string]string{
				"Authorization": "Bearer tokenXYZ",
			},
		},
	}

	for _, req := range requests {
		err := gateway.HandleRequest(ctx, req.method, req.path, req.headers)
		if err != nil && req.path != "/api/unknown" {
			t.Errorf("Request failed: %s %s - %v", req.method, req.path, err)
		}
	}

	// Analyze gateway behavior.
	spans := collector.Export()
	analyzer := NewTraceAnalyzer(spans)

	// Verify middleware executed for each request.
	authSpans := analyzer.GetSpansByName("middleware.auth")
	if len(authSpans) != len(requests) {
		t.Errorf("Expected %d auth middleware spans, got %d", len(requests), len(authSpans))
	}

	// Check 404 handling.
	notFoundCount := 0
	for _, span := range spans {
		if span.Tags["http.status_code"] == "404" {
			notFoundCount++
		}
	}
	if notFoundCount != 1 {
		t.Errorf("Expected 1 404 response, got %d", notFoundCount)
	}

	t.Logf("API Gateway processed %d requests with %d 404s", len(requests), notFoundCount)
}

// TestRateLimiting demonstrates rate limiting with backpressure.
func TestRateLimiting(t *testing.T) {
	tracer := tracez.New("rate-limiter")
	collector := NewMockCollector(t, "ratelimit", 2000)
	tracer.AddCollector("api", collector.Collector)
	defer tracer.Close()

	// Rate limiter implementation.
	type RateLimiter struct {
		limit    int
		window   time.Duration
		requests map[string][]time.Time
		mu       sync.Mutex
	}

	limiter := &RateLimiter{
		limit:    5,
		window:   100 * time.Millisecond,
		requests: make(map[string][]time.Time),
	}

	checkLimit := func(ctx context.Context, clientID string) bool {
		_, span := tracer.StartSpan(ctx, "rate-limit-check")
		defer span.Finish()

		span.SetTag("client_id", clientID)
		span.SetTag("limit", fmt.Sprintf("%d", limiter.limit))
		span.SetTag("window_ms", fmt.Sprintf("%d", limiter.window.Milliseconds()))

		limiter.mu.Lock()
		defer limiter.mu.Unlock()

		now := time.Now()
		cutoff := now.Add(-limiter.window)

		// Clean old requests.
		validRequests := []time.Time{}
		for _, reqTime := range limiter.requests[clientID] {
			if reqTime.After(cutoff) {
				validRequests = append(validRequests, reqTime)
			}
		}

		// Check limit.
		if len(validRequests) >= limiter.limit {
			span.SetTag("result", "rate_limited")
			span.SetTag("current_count", fmt.Sprintf("%d", len(validRequests)))
			return false
		}

		// Add current request.
		limiter.requests[clientID] = append(validRequests, now)
		span.SetTag("result", "allowed")
		span.SetTag("current_count", fmt.Sprintf("%d", len(validRequests)+1))
		return true
	}

	ctx := context.Background()

	// Simulate burst of requests from multiple clients.
	clients := []string{"client-A", "client-B"}
	var wg sync.WaitGroup

	results := struct {
		allowed map[string]int
		limited map[string]int
		mu      sync.Mutex
	}{
		allowed: make(map[string]int),
		limited: make(map[string]int),
	}

	for _, clientID := range clients {
		wg.Add(1)
		go func(client string) {
			defer wg.Done()

			// Send burst of requests.
			for i := 0; i < 10; i++ {
				reqCtx, reqSpan := tracer.StartSpan(ctx, fmt.Sprintf("request-%s-%d", client, i))
				reqSpan.SetTag("client", client)
				reqSpan.SetTag("request_num", fmt.Sprintf("%d", i))

				if checkLimit(reqCtx, client) {
					reqSpan.SetTag("status", "processed")
					results.mu.Lock()
					results.allowed[client]++
					results.mu.Unlock()

					// Simulate processing.
					time.Sleep(5 * time.Millisecond)
				} else {
					reqSpan.SetTag("status", "rate_limited")
					reqSpan.SetTag("http.status_code", "429")
					results.mu.Lock()
					results.limited[client]++
					results.mu.Unlock()
				}

				reqSpan.Finish()

				// Small delay between requests.
				time.Sleep(2 * time.Millisecond)
			}
		}(clientID)
	}

	wg.Wait()

	// Verify rate limiting worked.
	for _, client := range clients {
		if results.limited[client] == 0 {
			t.Errorf("Client %s was never rate limited", client)
		}
		if results.allowed[client] == 0 {
			t.Errorf("Client %s had no allowed requests", client)
		}

		t.Logf("Client %s: %d allowed, %d rate limited",
			client, results.allowed[client], results.limited[client])
	}

	// Check traces for 429 responses.
	spans := collector.Export()
	rateLimitedCount := 0
	for _, span := range spans {
		if span.Tags["http.status_code"] == "429" {
			rateLimitedCount++
		}
	}

	if rateLimitedCount == 0 {
		t.Error("No 429 status codes in traces")
	}
}

// TestGraphQLResolver demonstrates tracing GraphQL query resolution.
func TestGraphQLResolver(t *testing.T) {
	tracer := tracez.New("graphql-server")
	collector := NewMockCollector(t, "graphql", 1000)
	tracer.AddCollector("api", collector.Collector)
	defer tracer.Close()

	// Mock data sources.
	userDB := NewMockService("user-db", tracer)
	postDB := NewMockService("post-db", tracer)
	commentDB := NewMockService("comment-db", tracer)

	ctx := context.Background()

	// Simulate GraphQL query resolution.
	// Query: { user(id: "123") { name, posts { title, comments { text } } } }.

	ctx, querySpan := tracer.StartSpan(ctx, "graphql.query")
	querySpan.SetTag("operation", "query")
	querySpan.SetTag("query", "user.posts.comments")

	// Resolve user.
	_, userSpan := tracer.StartSpan(ctx, "resolve.user")
	userSpan.SetTag("field", "user")
	userSpan.SetTag("args.id", "123")
	userDB.Call(ctx, "fetch-user")
	userSpan.Finish()

	// Resolve posts for user (N+1 problem demonstration).
	postIDs := []string{"post-1", "post-2", "post-3"}

	_, postsSpan := tracer.StartSpan(ctx, "resolve.posts")
	postsSpan.SetTag("field", "posts")
	postsSpan.SetTag("parent.type", "User")

	for _, postID := range postIDs {
		// Individual query for each post (N+1 problem).
		_, postSpan := tracer.StartSpan(ctx, fmt.Sprintf("fetch.post.%s", postID))
		postSpan.SetTag("post_id", postID)
		postSpan.SetTag("n_plus_one", "true")
		postDB.Call(ctx, "fetch-post")
		postSpan.Finish()

		// Resolve comments for each post (nested N+1).
		commentIDs := []string{"c1", "c2"}
		for _, commentID := range commentIDs {
			_, commentSpan := tracer.StartSpan(ctx, fmt.Sprintf("fetch.comment.%s", commentID))
			commentSpan.SetTag("comment_id", commentID)
			commentSpan.SetTag("post_id", postID)
			commentSpan.SetTag("n_plus_one", "true")
			commentDB.Call(ctx, "fetch-comment")
			commentSpan.Finish()
		}
	}

	postsSpan.Finish()

	// Calculate total DB calls.
	totalDBCalls := 1 + len(postIDs) + (len(postIDs) * 2) // user + posts + comments.
	querySpan.SetTag("db_calls", fmt.Sprintf("%d", totalDBCalls))
	querySpan.SetTag("has_n_plus_one", "true")
	querySpan.Finish()

	// Simulate optimized query with DataLoader pattern.
	ctx, optimizedSpan := tracer.StartSpan(ctx, "graphql.query.optimized")
	optimizedSpan.SetTag("operation", "query")
	optimizedSpan.SetTag("optimization", "dataloader")

	// Batch fetch users.
	_, batchUserSpan := tracer.StartSpan(ctx, "batch.users")
	batchUserSpan.SetTag("batch_size", "1")
	userDB.Call(ctx, "batch-fetch-users")
	batchUserSpan.Finish()

	// Batch fetch posts.
	_, batchPostsSpan := tracer.StartSpan(ctx, "batch.posts")
	batchPostsSpan.SetTag("batch_size", fmt.Sprintf("%d", len(postIDs)))
	postDB.Call(ctx, "batch-fetch-posts")
	batchPostsSpan.Finish()

	// Batch fetch comments.
	_, batchCommentsSpan := tracer.StartSpan(ctx, "batch.comments")
	batchCommentsSpan.SetTag("batch_size", fmt.Sprintf("%d", len(postIDs)*2))
	commentDB.Call(ctx, "batch-fetch-comments")
	batchCommentsSpan.Finish()

	optimizedDBCalls := 3 // One batch query per type.
	optimizedSpan.SetTag("db_calls", fmt.Sprintf("%d", optimizedDBCalls))
	optimizedSpan.SetTag("has_n_plus_one", "false")
	optimizedSpan.Finish()

	// Analyze performance difference.
	spans := collector.Export()

	nPlusOneCount := 0
	batchCount := 0

	for _, span := range spans {
		if span.Tags["n_plus_one"] == "true" {
			nPlusOneCount++
		}
		if span.Name[:6] == "batch." {
			batchCount++
		}
	}

	// Verify N+1 problem was demonstrated.
	if nPlusOneCount == 0 {
		t.Error("N+1 problem not demonstrated")
	}

	// Verify optimization worked.
	if batchCount != 3 {
		t.Errorf("Expected 3 batch queries, got %d", batchCount)
	}

	t.Logf("GraphQL performance: N+1 queries=%d, Optimized batch queries=%d",
		totalDBCalls, optimizedDBCalls)
}

// TestWebSocketConnection demonstrates WebSocket message tracing.
func TestWebSocketConnection(t *testing.T) {
	tracer := tracez.New("websocket-server")
	collector := NewMockCollector(t, "websocket", 1000)
	tracer.AddCollector("api", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Simulate WebSocket connection lifecycle.
	ctx, connSpan := tracer.StartSpan(ctx, "websocket.connection")
	connID := "ws-conn-789"
	connSpan.SetTag("connection_id", connID)
	connSpan.SetTag("protocol", "ws")

	// Handshake.
	_, handshakeSpan := tracer.StartSpan(ctx, "ws.handshake")
	handshakeSpan.SetTag("upgrade", "websocket")
	handshakeSpan.SetTag("sec-websocket-version", "13")
	time.Sleep(5 * time.Millisecond)
	handshakeSpan.Finish()

	// Message exchange.
	messages := []struct {
		direction string
		msgType   string
		payload   string
	}{
		{"client->server", "subscribe", `{"channel":"updates"}`},
		{"server->client", "ack", `{"subscribed":true}`},
		{"server->client", "data", `{"update":1}`},
		{"client->server", "ping", `{"timestamp":123}`},
		{"server->client", "pong", `{"timestamp":123}`},
		{"server->client", "data", `{"update":2}`},
		{"client->server", "unsubscribe", `{"channel":"updates"}`},
	}

	for i, msg := range messages {
		_, msgSpan := tracer.StartSpan(ctx, fmt.Sprintf("ws.message.%d", i))
		msgSpan.SetTag("direction", msg.direction)
		msgSpan.SetTag("message.type", msg.msgType)
		msgSpan.SetTag("message.size", fmt.Sprintf("%d", len(msg.payload)))

		// Simulate processing time.
		processingTime := 2 * time.Millisecond
		if msg.msgType == "data" {
			processingTime = 10 * time.Millisecond // Data messages take longer.
		}
		time.Sleep(processingTime)

		msgSpan.Finish()
	}

	// Connection close.
	_, closeSpan := tracer.StartSpan(ctx, "ws.close")
	closeSpan.SetTag("close_code", "1000")
	closeSpan.SetTag("close_reason", "Normal Closure")
	time.Sleep(2 * time.Millisecond)
	closeSpan.Finish()

	connSpan.SetTag("message_count", fmt.Sprintf("%d", len(messages)))
	connSpan.SetTag("status", "closed")
	connSpan.Finish()

	// Verify WebSocket traces.
	spans := collector.Export()
	analyzer := NewTraceAnalyzer(spans)

	// Check handshake.
	handshakeSpans := analyzer.GetSpansByName("ws.handshake")
	if len(handshakeSpans) != 1 {
		t.Error("WebSocket handshake not traced")
	}

	// Count message types.
	pingPongCount := 0
	dataCount := 0

	for _, span := range spans {
		msgType := span.Tags["message.type"]
		if msgType == "ping" || msgType == "pong" {
			pingPongCount++
		}
		if msgType == "data" {
			dataCount++
		}
	}

	if pingPongCount != 2 {
		t.Errorf("Expected 2 ping/pong messages, got %d", pingPongCount)
	}

	if dataCount != 2 {
		t.Errorf("Expected 2 data messages, got %d", dataCount)
	}

	t.Logf("WebSocket session: %d total messages (%d data, %d ping/pong)",
		len(messages), dataCount, pingPongCount)
}

// TestRetryWithBackoff demonstrates exponential backoff retry strategy.
func TestRetryWithBackoff(t *testing.T) {
	tracer := tracez.New("retry-client")
	collector := NewMockCollector(t, "retry", 1000)
	tracer.AddCollector("api", collector.Collector)
	defer tracer.Close()

	// Create flaky service that succeeds after N attempts.
	flakyService := NewMockService("flaky-api", tracer)
	attemptCount := 0
	successAfter := 3

	ctx := context.Background()

	// Retry configuration.
	maxRetries := 5
	baseDelay := 10 * time.Millisecond

	// Execute with retry.
	ctx, opSpan := tracer.StartSpan(ctx, "operation-with-retry")
	opSpan.SetTag("max_retries", fmt.Sprintf("%d", maxRetries))
	opSpan.SetTag("backoff", "exponential")

	var lastErr error
	success := false

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Calculate backoff delay.
		delay := time.Duration(0)
		if attempt > 0 {
			delay = baseDelay * time.Duration(1<<(attempt-1)) // Exponential: 10ms, 20ms, 40ms, 80ms...
		}

		if delay > 0 {
			_, delaySpan := tracer.StartSpan(ctx, "backoff-delay")
			delaySpan.SetTag("attempt", fmt.Sprintf("%d", attempt))
			delaySpan.SetTag("delay_ms", fmt.Sprintf("%d", delay.Milliseconds()))
			time.Sleep(delay)
			delaySpan.Finish()
		}

		// Attempt operation.
		_, attemptSpan := tracer.StartSpan(ctx, fmt.Sprintf("attempt-%d", attempt))
		attemptSpan.SetTag("attempt_number", fmt.Sprintf("%d", attempt))

		// Simulate failure until successAfter attempts.
		attemptCount++
		if attemptCount <= successAfter {
			flakyService.SetFailureRate(1.0) // Force failure.
		} else {
			flakyService.SetFailureRate(0.0) // Allow success.
		}

		err := flakyService.Call(ctx, "api-call")

		if err != nil {
			attemptSpan.SetTag("result", "failed")
			attemptSpan.SetTag("error", err.Error())
			lastErr = err

			// Determine if we should retry.
			if attempt < maxRetries {
				attemptSpan.SetTag("will_retry", "true")
				attemptSpan.SetTag("next_delay_ms", fmt.Sprintf("%d", (baseDelay*time.Duration(1<<attempt)).Milliseconds()))
			} else {
				attemptSpan.SetTag("will_retry", "false")
				attemptSpan.SetTag("reason", "max_retries_exceeded")
			}
		} else {
			attemptSpan.SetTag("result", "success")
			success = true
		}

		attemptSpan.Finish()

		if success {
			break
		}
	}

	if success {
		opSpan.SetTag("outcome", "success")
		opSpan.SetTag("total_attempts", fmt.Sprintf("%d", attemptCount))
	} else {
		opSpan.SetTag("outcome", "failed")
		opSpan.SetTag("final_error", lastErr.Error())
	}

	opSpan.Finish()

	// Analyze retry behavior.
	spans := collector.Export()

	// Count attempts and delays.
	attempts := 0
	delays := 0
	totalDelayMs := int64(0)

	for _, span := range spans {
		if span.Name[:8] == "attempt-" {
			attempts++
		}
		if span.Name == "backoff-delay" {
			delays++
			if delayMs := span.Tags["delay_ms"]; delayMs != "" {
				var ms int64
				fmt.Sscanf(delayMs, "%d", &ms)
				totalDelayMs += ms
			}
		}
	}

	// Verify retry behavior.
	if attempts <= 1 {
		t.Error("No retries detected")
	}

	if delays == 0 {
		t.Error("No backoff delays detected")
	}

	if !success {
		t.Error("Operation should have succeeded after retries")
	}

	t.Logf("Retry stats: %d attempts, %d delays, %dms total delay",
		attempts, delays, totalDelayMs)
}
