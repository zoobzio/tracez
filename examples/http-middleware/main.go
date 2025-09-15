package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/zoobzio/tracez"
)

// Local key constants for this example.
const HTTPRequestKey = "http.request"

// RateLimitMiddleware enforces per-user rate limits.
func RateLimitMiddleware(tracer *tracez.Tracer, requestsPerSecond int) func(http.Handler) http.Handler {
	// Simple in-memory rate limiter
	userRequests := make(map[string]*atomic.Int32)

	// Reset counters every second
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		for range ticker.C {
			userRequests = make(map[string]*atomic.Int32)
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.StartSpan(r.Context(), "ratelimit.check")
			defer span.Finish()

			// Extract user ID from auth header (simplified)
			userID := r.Header.Get("X-User-ID")
			if userID == "" {
				userID = "anonymous"
			}
			span.SetTag("user.id", userID)

			// Get or create counter for this user
			if _, exists := userRequests[userID]; !exists {
				userRequests[userID] = &atomic.Int32{}
			}

			// Check rate limit
			count := userRequests[userID].Add(1)
			span.SetTag("ratelimit.count", fmt.Sprintf("%d", count))
			span.SetTag("ratelimit.limit", fmt.Sprintf("%d", requestsPerSecond))

			if count > int32(requestsPerSecond) {
				span.SetTag("ratelimit.exceeded", "true")
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RetryMiddleware automatically retries failed requests.
func RetryMiddleware(tracer *tracez.Tracer, maxRetries int) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.StartSpan(r.Context(), "retry.handler")
			defer span.Finish()

			var lastStatus int
			var attempts int

			// Try up to maxRetries times
			for attempts = 0; attempts <= maxRetries; attempts++ {
				// Create recorder to capture response
				recorder := &responseRecorder{
					ResponseWriter: w,
					statusCode:     200,
					body:           []byte{},
				}

				// Create attempt span
				_, attemptSpan := tracer.StartSpan(ctx, "retry.attempt")
				attemptSpan.SetTag("retry.attempt", fmt.Sprintf("%d", attempts+1))

				// Call next handler
				next.ServeHTTP(recorder, r.WithContext(ctx))
				lastStatus = recorder.statusCode
				attemptSpan.SetTag("http.status_code", fmt.Sprintf("%d", lastStatus))
				attemptSpan.Finish()

				// Check if we should retry
				if lastStatus < 500 {
					// Success or client error - don't retry
					span.SetTag("retry.total_attempts", fmt.Sprintf("%d", attempts+1))
					span.SetTag("retry.final_status", fmt.Sprintf("%d", lastStatus))

					// Write the successful response
					for key, values := range recorder.Header() {
						for _, value := range values {
							w.Header().Add(key, value)
						}
					}
					w.WriteHeader(lastStatus)
					w.Write(recorder.body)
					return
				}

				// Server error - retry with backoff
				if attempts < maxRetries {
					backoff := time.Duration(100*(1<<uint(attempts))) * time.Millisecond
					span.SetTag(fmt.Sprintf("retry.backoff_%d", attempts+1), backoff.String())
					time.Sleep(backoff)
				}
			}

			// All retries exhausted
			span.SetTag("retry.exhausted", "true")
			span.SetTag("retry.total_attempts", fmt.Sprintf("%d", attempts))
			span.SetTag("retry.final_status", fmt.Sprintf("%d", lastStatus))
			http.Error(w, "Service unavailable after retries", http.StatusServiceUnavailable)
		})
	}
}

// TracingMiddleware adds tracing to HTTP requests.
func TracingMiddleware(tracer *tracez.Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.StartSpan(r.Context(), HTTPRequestKey)
			span.SetTag("http.method", r.Method)
			span.SetTag("http.path", r.URL.Path)
			span.SetTag("http.remote_addr", r.RemoteAddr)
			defer span.Finish()

			wrapped := &responseRecorder{
				ResponseWriter: w,
				statusCode:     200,
				body:           []byte{},
			}

			next.ServeHTTP(wrapped, r.WithContext(ctx))

			span.SetTag("http.status_code", fmt.Sprintf("%d", wrapped.statusCode))

			// Actually write the response
			for key, values := range wrapped.Header() {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.WriteHeader(wrapped.statusCode)
			w.Write(wrapped.body)
		})
	}
}

// API handler that sometimes fails.
func handleAPI(tracer *tracez.Tracer) http.HandlerFunc {
	requestCount := &atomic.Int32{}

	return func(w http.ResponseWriter, r *http.Request) {
		ctx, span := tracer.StartSpan(r.Context(), "handler.api")
		defer span.Finish()

		// Track request number
		reqNum := requestCount.Add(1)
		span.SetTag("request.number", fmt.Sprintf("%d", reqNum))

		// Simulate occasional backend failures (20% chance)
		if rand.Float32() < 0.2 {
			span.SetTag("backend.error", "connection_timeout")
			http.Error(w, "Backend connection timeout", http.StatusBadGateway)
			return
		}

		// Simulate API processing
		_, processSpan := tracer.StartSpan(ctx, "api.process")
		processSpan.SetTag("api.operation", "fetch_data")
		time.Sleep(50 * time.Millisecond)
		processSpan.Finish()

		// Return success
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status": "success",
			"data":   "API response data",
		})
	}
}

// responseRecorder captures the response for retry logic.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

func main() {
	// Setup tracer
	tracer := tracez.New("api-service")
	defer tracer.Close()

	collector := tracez.NewCollector("traces", 1000)
	tracer.AddCollector("collector", collector)

	// Start trace reporter
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			spans := collector.Export()
			if len(spans) > 0 {
				fmt.Printf("\n=== Trace Report (%d spans) ===\n", len(spans))

				// Group by trace
				traces := make(map[string][]tracez.Span)
				for i := range spans {
					span := &spans[i]
					traces[span.TraceID] = append(traces[span.TraceID], *span)
				}

				// Analyze each trace
				for traceID, traceSpans := range traces {
					fmt.Printf("\nTrace: %s\n", traceID[:8])

					// Count key events
					var rateLimitExceeded bool
					var retryAttempts int
					var finalStatus string

					for _, span := range traceSpans {
						if span.Name == "ratelimit.check" && span.Tags["ratelimit.exceeded"] == "true" {
							rateLimitExceeded = true
						}
						if span.Name == "retry.attempt" {
							retryAttempts++
						}
						if span.Name == "http.request" {
							finalStatus = span.Tags["http.status_code"]
						}
					}

					// Show the story
					if rateLimitExceeded && retryAttempts > 1 {
						fmt.Printf("  🔥 PROBLEM DETECTED: Retry consumed rate limit!\n")
						fmt.Printf("     - Retries attempted: %d\n", retryAttempts)
						fmt.Printf("     - Each retry counted against rate limit\n")
						fmt.Printf("     - User blocked with 429 despite original 502\n")
						fmt.Printf("     - Final status: %s\n", finalStatus)
					} else if retryAttempts > 1 {
						fmt.Printf("  ✓ Request succeeded after %d attempts\n", retryAttempts)
						fmt.Printf("    Final status: %s\n", finalStatus)
					} else if rateLimitExceeded {
						fmt.Printf("  ⚠️ Rate limit exceeded (legitimate)\n")
					} else {
						fmt.Printf("  ✓ Request succeeded immediately\n")
					}

					// Show span hierarchy
					fmt.Printf("\n  Span hierarchy:\n")
					for _, span := range traceSpans {
						indent := "    "
						if span.ParentID != "" {
							indent = "      "
						}
						fmt.Printf("%s[%s] %v", indent, span.Name, span.Duration)

						// Show important tags
						if span.Name == "ratelimit.check" {
							fmt.Printf(" (count: %s/%s)",
								span.Tags["ratelimit.count"],
								span.Tags["ratelimit.limit"])
						}
						if span.Name == "retry.attempt" {
							fmt.Printf(" (attempt: %s, status: %s)",
								span.Tags["retry.attempt"],
								span.Tags["http.status_code"])
						}
						fmt.Println()
					}
				}
			}
		}
	}()

	// Setup handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/api/data", handleAPI(tracer))

	// PROBLEM: Retry BEFORE rate limit = retries consume quota!
	problemHandler := TracingMiddleware(tracer)(
		RetryMiddleware(tracer, 3)( // Retry happens first
			RateLimitMiddleware(tracer, 5)( // Each retry counts!
				mux,
			),
		),
	)

	// SOLUTION: Rate limit BEFORE retry = retries don't consume quota
	solutionHandler := TracingMiddleware(tracer)(
		RateLimitMiddleware(tracer, 5)( // Rate limit happens first
			RetryMiddleware(tracer, 3)( // Only retry if under limit
				mux,
			),
		),
	)

	// Run both configurations
	go func() {
		fmt.Println("PROBLEM server (retry before rate limit) on :8080")
		http.ListenAndServe(":8080", problemHandler)
	}()

	fmt.Println("SOLUTION server (rate limit before retry) on :8081")
	fmt.Println()
	fmt.Println("Demonstrating the middleware ordering problem:")
	fmt.Println()
	fmt.Println("PROBLEM (port 8080): Retry middleware runs BEFORE rate limiter")
	fmt.Println("  Each retry attempt counts against rate limit!")
	fmt.Println("  Users get 429 (rate limited) instead of 503 (unavailable)")
	fmt.Println()
	fmt.Println("SOLUTION (port 8081): Rate limiter runs BEFORE retry middleware")
	fmt.Println("  Retries only happen if user is under rate limit")
	fmt.Println("  Users get appropriate error codes")
	fmt.Println()
	fmt.Println("Try these commands to see the difference:")
	fmt.Println()
	fmt.Println("  # Problem server - watch retries consume rate limit:")
	fmt.Println("  for i in {1..10}; do")
	fmt.Println("    curl -H 'X-User-ID: alice' http://localhost:8080/api/data")
	fmt.Println("  done")
	fmt.Println()
	fmt.Println("  # Solution server - rate limit protects from retry storms:")
	fmt.Println("  for i in {1..10}; do")
	fmt.Println("    curl -H 'X-User-ID: bob' http://localhost:8081/api/data")
	fmt.Println("  done")
	fmt.Println()
	fmt.Println("Watch the trace output to see how middleware ordering changes behavior!")

	http.ListenAndServe(":8081", solutionHandler)
}
