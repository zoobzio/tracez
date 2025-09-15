package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

func TestRateLimitMiddleware(t *testing.T) {
	tracer := tracez.New("test-service")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	// Create rate limited handler
	handler := RateLimitMiddleware(tracer, 2)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	// Test under limit
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", http.NoBody)
		req.Header.Set("X-User-ID", "test-user")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("Request %d: expected 200, got %d", i+1, w.Code)
		}
	}

	// Test over limit
	req := httptest.NewRequest("GET", "/test", http.NoBody)
	req.Header.Set("X-User-ID", "test-user")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("Over limit request: expected 429, got %d", w.Code)
	}

	// Wait for spans and verify
	time.Sleep(10 * time.Millisecond)
	spans := collector.Export()

	// Find rate limit spans
	var exceededFound bool
	for _, span := range spans {
		if span.Name == "ratelimit.check" {
			if span.Tags["ratelimit.exceeded"] == "true" {
				exceededFound = true
			}
		}
	}

	if !exceededFound {
		t.Error("Expected to find span with rate limit exceeded")
	}
}

func TestRetryMiddleware(t *testing.T) {
	tracer := tracez.New("test-service")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 200)
	tracer.AddCollector("collector", collector)

	tests := []struct {
		name           string
		statusCode     int
		expectedStatus int
		expectRetries  bool
	}{
		{
			name:           "success no retry",
			statusCode:     http.StatusOK,
			expectedStatus: http.StatusOK,
			expectRetries:  false,
		},
		{
			name:           "client error no retry",
			statusCode:     http.StatusBadRequest,
			expectedStatus: http.StatusBadRequest,
			expectRetries:  false,
		},
		{
			name:           "server error with retry",
			statusCode:     http.StatusBadGateway,
			expectedStatus: http.StatusServiceUnavailable,
			expectRetries:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector.Reset()

			// Create handler that returns specific status
			handler := RetryMiddleware(tracer, 2)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(tt.statusCode)
				}),
			)

			req := httptest.NewRequest("GET", "/test", http.NoBody)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			// Check response status
			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			// Wait for spans
			time.Sleep(50 * time.Millisecond)
			spans := collector.Export()

			// Count retry attempts
			retryAttempts := 0
			for _, span := range spans {
				if span.Name == "retry.attempt" {
					retryAttempts++
				}
			}

			if tt.expectRetries && retryAttempts <= 1 {
				t.Errorf("Expected retries but got %d attempts", retryAttempts)
			}
			if !tt.expectRetries && retryAttempts > 1 {
				t.Errorf("Expected no retries but got %d attempts", retryAttempts)
			}
		})
	}
}

func TestMiddlewareOrdering(t *testing.T) {
	tracer := tracez.New("test-service")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 500)
	tracer.AddCollector("collector", collector)

	// Simulate a failing backend
	failingBackend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})

	// PROBLEM: Retry before rate limit
	problemHandler := TracingMiddleware(tracer)(
		RetryMiddleware(tracer, 3)(
			RateLimitMiddleware(tracer, 5)(
				failingBackend,
			),
		),
	)

	// SOLUTION: Rate limit before retry
	solutionHandler := TracingMiddleware(tracer)(
		RateLimitMiddleware(tracer, 5)(
			RetryMiddleware(tracer, 3)(
				failingBackend,
			),
		),
	)

	t.Run("problem ordering", func(t *testing.T) {
		collector.Reset()

		// Make multiple requests with problem ordering
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", http.NoBody)
			req.Header.Set("X-User-ID", "problem-user")
			w := httptest.NewRecorder()
			problemHandler.ServeHTTP(w, req)

			// First request should get 503 (retry exhausted)
			// Second might get 429 if retries consumed rate limit
			if w.Code != http.StatusServiceUnavailable && w.Code != http.StatusTooManyRequests {
				t.Errorf("Request %d: unexpected status %d", i+1, w.Code)
			}
		}

		// Wait and check spans
		time.Sleep(100 * time.Millisecond)
		spans := collector.Export()

		// Count rate limit checks
		rateLimitChecks := 0
		for _, span := range spans {
			if span.Name == "ratelimit.check" {
				rateLimitChecks++
			}
		}

		// With problem ordering, each retry triggers a rate limit check
		// So we should see many rate limit checks
		if rateLimitChecks < 4 {
			t.Errorf("Expected multiple rate limit checks due to retries, got %d", rateLimitChecks)
		}
	})

	t.Run("solution ordering", func(t *testing.T) {
		collector.Reset()

		// Wait for rate limit reset
		time.Sleep(1100 * time.Millisecond)

		// Make multiple requests with solution ordering
		for i := 0; i < 2; i++ {
			req := httptest.NewRequest("GET", "/test", http.NoBody)
			req.Header.Set("X-User-ID", "solution-user")
			w := httptest.NewRecorder()
			solutionHandler.ServeHTTP(w, req)

			// Should consistently get 503 (retry exhausted)
			if w.Code != http.StatusServiceUnavailable {
				t.Errorf("Request %d: expected 503, got %d", i+1, w.Code)
			}
		}

		// Wait and check spans
		time.Sleep(100 * time.Millisecond)
		spans := collector.Export()

		// Count rate limit checks per request
		rateLimitChecks := 0
		for _, span := range spans {
			if span.Name == "ratelimit.check" {
				rateLimitChecks++
			}
		}

		// With solution ordering, rate limit is checked once per request
		// (before retries), so we should see exactly 2 checks for 2 requests
		if rateLimitChecks != 2 {
			t.Errorf("Expected 2 rate limit checks (one per request), got %d", rateLimitChecks)
		}
	})
}

func TestTracingMiddleware(t *testing.T) {
	tracer := tracez.New("test-service")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	handler := TracingMiddleware(tracer)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Verify context has span
			if tracez.GetSpan(r.Context()) == nil {
				t.Error("Context missing span")
			}
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Wait for span collection
	time.Sleep(10 * time.Millisecond)

	// Verify span created
	spans := collector.Export()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name != "http.request" {
		t.Errorf("Expected span name 'http.request', got %s", span.Name)
	}

	// Check tags
	expectedTags := map[string]string{
		"http.method":      "GET",
		"http.path":        "/test",
		"http.status_code": "200",
	}

	for key, expected := range expectedTags {
		if actual := span.Tags[key]; actual != expected {
			t.Errorf("Tag %s: expected %s, got %s", key, expected, actual)
		}
	}
}

func TestAPIHandler(t *testing.T) {
	tracer := tracez.New("test-service")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	handler := handleAPI(tracer)

	// Make multiple requests to get both success and failure
	successCount := 0
	failureCount := 0

	for i := 0; i < 20; i++ {
		collector.Reset()

		req := httptest.NewRequest("GET", "/api/data", http.NoBody)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			successCount++
		} else if w.Code == http.StatusBadGateway {
			failureCount++
		} else {
			t.Errorf("Unexpected status code: %d", w.Code)
		}

		// Check spans
		time.Sleep(10 * time.Millisecond)
		spans := collector.Export()

		// Should have handler.api span
		hasHandlerSpan := false
		for _, span := range spans {
			if span.Name == "handler.api" {
				hasHandlerSpan = true
				// Check for request number tag
				if span.Tags["request.number"] == "" {
					t.Error("Missing request.number tag")
				}
			}
		}

		if !hasHandlerSpan {
			t.Error("Missing handler.api span")
		}
	}

	// Should have some successes and some failures (20% failure rate)
	if successCount == 0 {
		t.Error("Expected some successful requests")
	}
	if failureCount == 0 {
		t.Error("Expected some failed requests")
	}
}

func TestResponseRecorder(t *testing.T) {
	w := httptest.NewRecorder()
	recorder := &responseRecorder{
		ResponseWriter: w,
		statusCode:     200,
		body:           []byte{},
	}

	// Test WriteHeader
	recorder.WriteHeader(404)
	if recorder.statusCode != 404 {
		t.Errorf("Expected status 404, got %d", recorder.statusCode)
	}

	// Test Write
	testData := []byte("test response")
	n, err := recorder.Write(testData)
	if err != nil {
		t.Errorf("Write error: %v", err)
	}
	if n != len(testData) {
		t.Errorf("Expected to write %d bytes, wrote %d", len(testData), n)
	}
	if string(recorder.body) != "test response" {
		t.Errorf("Expected body 'test response', got '%s'", string(recorder.body))
	}
}
