package integration

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// TestHTTPMiddlewareChain simulates 5-layer middleware with request/response flow.
func TestHTTPMiddlewareChain(t *testing.T) {
	tracer := tracez.New("http-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Middleware layers.
	middlewares := []string{"auth", "ratelimit", "cors", "logging", "metrics"}

	// Create middleware chain.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Extract context from request.
		ctx := r.Context()

		// Business logic span.
		_, bizSpan := tracer.StartSpan(ctx, "business-logic")
		bizSpan.SetTag("endpoint", r.URL.Path)
		bizSpan.SetTag("method", r.Method)
		time.Sleep(5 * time.Millisecond) // Simulate work.
		bizSpan.Finish()

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Build middleware chain.
	var wrappedHandler http.Handler = handler

	// Apply middlewares in reverse order (so they execute in correct order).
	for i := len(middlewares) - 1; i >= 0; i-- {
		middleware := middlewares[i]
		previousHandler := wrappedHandler

		wrappedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Start middleware span.
			ctx, span := tracer.StartSpan(r.Context(), fmt.Sprintf("middleware.%s", middleware))
			span.SetTag("layer", middleware)
			span.SetTag("path", r.URL.Path)

			// Pass context through request.
			r = r.WithContext(ctx)

			// Call next handler.
			previousHandler.ServeHTTP(w, r)

			// Finish middleware span.
			span.Finish()
		})
	}

	// Create test request.
	req := httptest.NewRequest("GET", "/api/users", http.NoBody)

	// Start root span for request.
	ctx, rootSpan := tracer.StartSpan(context.Background(), "http.request")
	rootSpan.SetTag("url", req.URL.String())
	req = req.WithContext(ctx)

	// Execute request.
	recorder := httptest.NewRecorder()
	wrappedHandler.ServeHTTP(recorder, req)

	rootSpan.SetTag("status", fmt.Sprintf("%d", recorder.Code))
	rootSpan.Finish()

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Export and verify.
	spans := collector.Export()

	// Should have: root + 5 middlewares + business logic = 7 spans.
	if len(spans) != 7 {
		t.Fatalf("Expected 7 spans, got %d", len(spans))
	}

	// Build span map.
	spansByName := make(map[string]tracez.Span)
	for _, span := range spans {
		spansByName[span.Name] = span
	}

	// Verify all spans present.
	expectedSpans := []string{
		"http.request",
		"middleware.auth",
		"middleware.ratelimit",
		"middleware.cors",
		"middleware.logging",
		"middleware.metrics",
		"business-logic",
	}

	for _, expected := range expectedSpans {
		if _, exists := spansByName[expected]; !exists {
			t.Errorf("Missing expected span: %s", expected)
		}
	}

	// Verify nesting order.
	// auth should be child of root.
	authSpan := spansByName["middleware.auth"]
	rootSpan2 := spansByName["http.request"]
	if authSpan.ParentID != rootSpan2.SpanID {
		t.Error("Auth middleware not child of root")
	}

	// Business logic should be deepest.
	bizLogic := spansByName["business-logic"]
	if bizLogic.ParentID == "" {
		t.Error("Business logic has no parent")
	}

	// All spans should share same TraceID.
	traceID := rootSpan.TraceID()
	for _, span := range spans {
		if span.TraceID != traceID {
			t.Errorf("Span %s has wrong TraceID", span.Name)
		}
	}
}

// TestDatabaseTransactionPattern simulates transaction with queries and rollback.
func TestDatabaseTransactionPattern(t *testing.T) {
	tracer := tracez.New("db-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Simulate database transaction.
	ctx, txSpan := tracer.StartSpan(context.Background(), "db.transaction")
	txSpan.SetTag("isolation", "read-committed")

	// BEGIN.
	_, beginSpan := tracer.StartSpan(ctx, "db.begin")
	beginSpan.SetTag("query", "BEGIN")
	time.Sleep(2 * time.Millisecond)
	beginSpan.Finish()

	// Multiple queries.
	queries := []struct {
		name  string
		query string
		err   bool
	}{
		{"db.select", "SELECT * FROM users WHERE id = ?", false},
		{"db.update", "UPDATE users SET status = ? WHERE id = ?", false},
		{"db.select", "SELECT COUNT(*) FROM orders WHERE user_id = ?", false},
		{"db.insert", "INSERT INTO audit_log (user_id, action) VALUES (?, ?)", false},
		{"db.update", "UPDATE inventory SET count = count - ? WHERE id = ?", true}, // This fails.
	}

	var failed bool
	for i, q := range queries {
		_, querySpan := tracer.StartSpan(ctx, q.name)
		querySpan.SetTag("query", q.query)
		querySpan.SetTag("index", fmt.Sprintf("%d", i))

		time.Sleep(5 * time.Millisecond) // Simulate query time.

		if q.err {
			querySpan.SetTag("error", "true")
			querySpan.SetTag("error.message", "constraint violation")
			failed = true
		} else {
			querySpan.SetTag("rows_affected", fmt.Sprintf("%d", i+1))
		}

		querySpan.Finish()

		if failed {
			break // Stop on error.
		}
	}

	// ROLLBACK or COMMIT.
	if failed {
		_, rollbackSpan := tracer.StartSpan(ctx, "db.rollback")
		rollbackSpan.SetTag("query", "ROLLBACK")
		rollbackSpan.SetTag("reason", "query_failure")
		time.Sleep(2 * time.Millisecond)
		rollbackSpan.Finish()

		txSpan.SetTag("status", "rolled_back")
	} else {
		_, commitSpan := tracer.StartSpan(ctx, "db.commit")
		commitSpan.SetTag("query", "COMMIT")
		time.Sleep(2 * time.Millisecond)
		commitSpan.Finish()

		txSpan.SetTag("status", "committed")
	}

	txSpan.Finish()

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Export and verify.
	spans := collector.Export()

	// Should have transaction + begin + queries + rollback.
	if len(spans) < 7 { // At minimum.
		t.Fatalf("Expected at least 7 spans, got %d", len(spans))
	}

	// Find transaction span.
	var transactionSpan tracez.Span
	for _, span := range spans {
		if span.Name == "db.transaction" {
			transactionSpan = span
			break
		}
	}

	if transactionSpan.SpanID == "" {
		t.Fatal("Transaction span not found")
	}

	// Verify all query spans are children of transaction.
	for _, span := range spans {
		if span.Name != "db.transaction" {
			if span.ParentID != transactionSpan.SpanID {
				t.Errorf("Span %s not child of transaction", span.Name)
			}
		}
	}

	// Verify rollback occurred.
	foundRollback := false
	for _, span := range spans {
		if span.Name == "db.rollback" {
			foundRollback = true
			if span.Tags["reason"] != "query_failure" {
				t.Error("Rollback reason not set correctly")
			}
		}
	}

	if !foundRollback {
		t.Error("Rollback span not found")
	}

	// Verify error was recorded.
	foundError := false
	for _, span := range spans {
		if errTag, exists := span.Tags["error"]; exists && errTag == "true" {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Error("Error not recorded in spans")
	}
}

// TestWorkerPoolPattern simulates worker pool processing jobs.
func TestWorkerPoolPattern(t *testing.T) {
	tracer := tracez.New("worker-service")
	collector := tracez.NewCollector("test", 2000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Worker pool configuration.
	workerCount := 5
	jobCount := 20

	// Job queue.
	jobs := make(chan int, jobCount)
	results := make(chan string, jobCount)

	// Start supervisor span.
	ctx, supervisorSpan := tracer.StartSpan(context.Background(), "worker-pool.supervisor")
	supervisorSpan.SetTag("workers", fmt.Sprintf("%d", workerCount))
	supervisorSpan.SetTag("jobs", fmt.Sprintf("%d", jobCount))

	// Start workers.
	var wg sync.WaitGroup
	for w := 0; w < workerCount; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Worker span.
			workerCtx, workerSpan := tracer.StartSpan(ctx, fmt.Sprintf("worker-%d", workerID))
			workerSpan.SetTag("worker.id", fmt.Sprintf("%d", workerID))
			defer workerSpan.Finish()

			// Process jobs.
			for jobID := range jobs {
				// Job processing span.
				_, jobSpan := tracer.StartSpan(workerCtx, "process-job")
				jobSpan.SetTag("job.id", fmt.Sprintf("%d", jobID))
				jobSpan.SetTag("worker.id", fmt.Sprintf("%d", workerID))

				// Simulate work.
				time.Sleep(10 * time.Millisecond)

				// Different job types.
				jobType := "standard"
				if jobID%5 == 0 {
					jobType = "priority"
				} else if jobID%3 == 0 {
					jobType = "batch"
				}
				jobSpan.SetTag("job.type", jobType)

				jobSpan.Finish()

				results <- fmt.Sprintf("job-%d-done-by-worker-%d", jobID, workerID)
			}
		}(w)
	}

	// Submit jobs.
	for j := 0; j < jobCount; j++ {
		jobs <- j
	}
	close(jobs)

	// Wait for completion.
	wg.Wait()
	close(results)

	// Collect results.
	resultCount := 0
	for range results {
		resultCount++
	}

	supervisorSpan.SetTag("completed", fmt.Sprintf("%d", resultCount))
	supervisorSpan.Finish()

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Export and verify.
	spans := collector.Export()

	// Should have: supervisor + workers + jobs.
	expectedMinSpans := 1 + workerCount + jobCount
	if len(spans) < expectedMinSpans {
		t.Fatalf("Expected at least %d spans, got %d", expectedMinSpans, len(spans))
	}

	// Verify worker attribution.
	workerJobs := make(map[string]int) // workerID -> job count.
	for _, span := range spans {
		if span.Name == "process-job" {
			if workerID, ok := span.Tags["worker.id"]; ok {
				workerJobs[workerID]++
			}
		}
	}

	// Each worker should have processed at least one job.
	if len(workerJobs) != workerCount {
		t.Errorf("Expected %d workers to process jobs, got %d", workerCount, len(workerJobs))
	}

	// Total jobs processed should equal jobCount.
	totalProcessed := 0
	for _, count := range workerJobs {
		totalProcessed += count
	}
	if totalProcessed != jobCount {
		t.Errorf("Expected %d jobs processed, got %d", jobCount, totalProcessed)
	}

	// Verify all spans share same TraceID.
	traceID := supervisorSpan.TraceID()
	for _, span := range spans {
		if span.TraceID != traceID {
			t.Errorf("Span %s has different TraceID", span.Name)
		}
	}

	// Verify no span mixing between workers.
	for _, span := range spans {
		if span.Name == "process-job" {
			// Job should have consistent worker ID.
			workerID := span.Tags["worker.id"]

			// Find parent worker span.
			for _, parentSpan := range spans {
				if parentSpan.SpanID == span.ParentID {
					expectedName := fmt.Sprintf("worker-%s", workerID)
					if parentSpan.Name != expectedName {
						t.Errorf("Job from worker %s has wrong parent: %s",
							workerID, parentSpan.Name)
					}
					break
				}
			}
		}
	}
}

// TestCircuitBreakerIntegration simulates circuit breaker state transitions.
func TestCircuitBreakerIntegration(t *testing.T) {
	tracer := tracez.New("circuit-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Circuit breaker states.
	type CircuitState string
	const (
		StateClosed   CircuitState = "closed"
		StateOpen     CircuitState = "open"
		StateHalfOpen CircuitState = "half-open"
	)

	// Simulate service with circuit breaker.
	var (
		state        = StateClosed
		failureCount = 0
		threshold    = 3
	)

	// Make requests.
	for i := 0; i < 10; i++ {
		ctx, requestSpan := tracer.StartSpan(context.Background(), "service.request")
		requestSpan.SetTag("request.id", fmt.Sprintf("%d", i))
		requestSpan.SetTag("circuit.state", string(state))

		// Check circuit state.
		if state == StateOpen {
			// Fast fail.
			_, fallbackSpan := tracer.StartSpan(ctx, "circuit.fallback")
			fallbackSpan.SetTag("reason", "circuit_open")
			fallbackSpan.Finish()

			requestSpan.SetTag("handled_by", "fallback")
			requestSpan.Finish()

			// Try half-open after some requests.
			if i > 6 {
				state = StateHalfOpen
			}
			continue
		}

		// Try actual service call.
		_, callSpan := tracer.StartSpan(ctx, "service.call")

		// Simulate failures for first few requests.
		success := i >= 5 || state == StateHalfOpen

		if !success {
			callSpan.SetTag("error", "true")
			callSpan.SetTag("error.type", "timeout")
			failureCount++

			if failureCount >= threshold && state == StateClosed {
				state = StateOpen

				_, transitionSpan := tracer.StartSpan(ctx, "circuit.transition")
				transitionSpan.SetTag("from", string(StateClosed))
				transitionSpan.SetTag("to", string(StateOpen))
				transitionSpan.SetTag("trigger", "threshold_exceeded")
				transitionSpan.Finish()
			}
		} else {
			callSpan.SetTag("success", "true")

			if state == StateHalfOpen {
				state = StateClosed
				failureCount = 0

				_, transitionSpan := tracer.StartSpan(ctx, "circuit.transition")
				transitionSpan.SetTag("from", string(StateHalfOpen))
				transitionSpan.SetTag("to", string(StateClosed))
				transitionSpan.SetTag("trigger", "success_in_half_open")
				transitionSpan.Finish()
			}
		}

		callSpan.Finish()
		requestSpan.SetTag("handled_by", "service")
		requestSpan.Finish()

		// Small delay between requests.
		time.Sleep(5 * time.Millisecond)
	}

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Export and verify.
	spans := collector.Export()

	// Verify state transitions recorded.
	transitions := make([]tracez.Span, 0)
	for _, span := range spans {
		if span.Name == "circuit.transition" {
			transitions = append(transitions, span)
		}
	}

	if len(transitions) < 2 {
		t.Errorf("Expected at least 2 transitions, got %d", len(transitions))
	}

	// Verify fallback used when circuit open.
	fallbackCount := 0
	for _, span := range spans {
		if span.Name == "circuit.fallback" {
			fallbackCount++
		}
	}

	if fallbackCount == 0 {
		t.Error("No fallback spans found")
	}

	// Verify circuit states recorded in requests.
	statesFound := make(map[string]bool)
	for _, span := range spans {
		if span.Name == "service.request" {
			if state, ok := span.Tags["circuit.state"]; ok {
				statesFound[state] = true
			}
		}
	}

	if !statesFound[string(StateClosed)] {
		t.Error("Closed state not found in spans")
	}
	if !statesFound[string(StateOpen)] {
		t.Error("Open state not found in spans")
	}
}

// TestAsyncProcessingPattern simulates async job processing with callbacks.
func TestAsyncProcessingPattern(t *testing.T) {
	tracer := tracez.New("async-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Async job processor.
	type Job struct {
		ID       int
		TraceCtx context.Context
		Result   chan string
	}

	jobQueue := make(chan Job, 10)

	// Start async processor.
	go func() {
		for job := range jobQueue {
			// Continue trace from job context.
			_, processSpan := tracer.StartSpan(job.TraceCtx, "async.process")
			processSpan.SetTag("job.id", fmt.Sprintf("%d", job.ID))

			// Simulate processing.
			time.Sleep(20 * time.Millisecond)

			// Send result.
			job.Result <- fmt.Sprintf("processed-%d", job.ID)

			processSpan.Finish()
		}
	}()

	// Submit jobs.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)

		// Start trace for each job submission.
		ctx, submitSpan := tracer.StartSpan(context.Background(), "job.submit")
		submitSpan.SetTag("job.id", fmt.Sprintf("%d", i))

		resultChan := make(chan string, 1)
		job := Job{
			ID:       i,
			TraceCtx: ctx,
			Result:   resultChan,
		}

		jobQueue <- job
		submitSpan.Finish()

		// Wait for result in separate goroutine.
		go func(id int, results chan string, traceCtx context.Context) {
			defer wg.Done()

			result := <-results

			// Record callback.
			_, callbackSpan := tracer.StartSpan(traceCtx, "job.callback")
			callbackSpan.SetTag("job.id", fmt.Sprintf("%d", id))
			callbackSpan.SetTag("result", result)
			callbackSpan.Finish()
		}(i, resultChan, ctx)
	}

	wg.Wait()
	close(jobQueue)

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Export and verify.
	spans := collector.Export()

	// Each job should have: submit + process + callback.
	jobSpans := make(map[string][]string)
	for _, span := range spans {
		if jobID, ok := span.Tags["job.id"]; ok {
			jobSpans[jobID] = append(jobSpans[jobID], span.Name)
		}
	}

	for jobID, spanNames := range jobSpans {
		if len(spanNames) != 3 {
			t.Errorf("Job %s has %d spans, expected 3", jobID, len(spanNames))
		}

		// Verify all three types present.
		hasSubmit, hasProcess, hasCallback := false, false, false
		for _, name := range spanNames {
			switch name {
			case "job.submit":
				hasSubmit = true
			case "async.process":
				hasProcess = true
			case "job.callback":
				hasCallback = true
			}
		}

		if !hasSubmit || !hasProcess || !hasCallback {
			t.Errorf("Job %s missing spans: submit=%v, process=%v, callback=%v",
				jobID, hasSubmit, hasProcess, hasCallback)
		}
	}

	// Verify trace continuity across async boundaries.
	// Each job should have consistent TraceID across all its spans.
	for jobID := range jobSpans {
		var traceID string
		for _, span := range spans {
			if id, ok := span.Tags["job.id"]; ok && id == jobID {
				if traceID == "" {
					traceID = span.TraceID
				} else if span.TraceID != traceID {
					t.Errorf("Job %s has inconsistent TraceID", jobID)
				}
			}
		}
	}
}
