package benchmarks

import (
	"context"
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// BenchmarkWebServerScenario simulates a realistic web server workload.
func BenchmarkWebServerScenario(b *testing.B) {
	tracer := tracez.New("web-server")
	collector := tracez.NewCollector("http-traces", 5000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Simulate different request types with different complexity.
	requestTypes := []struct {
		name     string
		weight   int // How often this request type occurs.
		dbCalls  int // Number of DB calls.
		apiCalls int // Number of external API calls.
	}{
		{"GET /health", 30, 0, 0},  // Simple health check.
		{"GET /users", 25, 1, 0},   // Single DB query.
		{"POST /users", 15, 3, 1},  // Create user: validation, insert, notification.
		{"GET /orders", 20, 2, 0},  // List orders with pagination.
		{"POST /orders", 10, 5, 2}, // Complex order creation.
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Select request type based on weights.
		reqType := requestTypes[weightedSelect(requestTypes, rand.Intn(100))]

		// HTTP request span.
		reqCtx, reqSpan := tracer.StartSpan(ctx, "http.request")
		reqSpan.SetTag("http.method", reqType.name[:3])
		reqSpan.SetTag("http.path", reqType.name[4:])
		reqSpan.SetTag("user.id", fmt.Sprintf("user-%d", rand.Intn(10000)))

		// Auth middleware (always present).
		authCtx, authSpan := tracer.StartSpan(reqCtx, "auth.validate")
		authSpan.SetTag("auth.method", "jwt")
		time.Sleep(time.Nanosecond * 50) // Minimal auth time.
		authSpan.Finish()

		// Database calls.
		for j := 0; j < reqType.dbCalls; j++ {
			_, dbSpan := tracer.StartSpan(authCtx, "db.query")
			dbSpan.SetTag("db.table", []string{"users", "orders", "products"}[rand.Intn(3)])
			dbSpan.SetTag("db.operation", []string{"SELECT", "INSERT", "UPDATE"}[rand.Intn(3)])
			time.Sleep(time.Nanosecond * time.Duration(100+rand.Intn(500))) // DB latency.
			dbSpan.Finish()
		}

		// External API calls.
		for j := 0; j < reqType.apiCalls; j++ {
			_, apiSpan := tracer.StartSpan(authCtx, "external.api")
			apiSpan.SetTag("api.service", []string{"payment", "notification", "inventory"}[rand.Intn(3)])
			time.Sleep(time.Nanosecond * time.Duration(200+rand.Intn(1000))) // API latency.
			apiSpan.Finish()
		}

		reqSpan.SetTag("http.status", "200")
		reqSpan.Finish()

		// Periodic export to simulate real system.
		if i%500 == 0 {
			collector.Export()
		}
	}
}

// BenchmarkMicroserviceScenario simulates distributed microservice calls.
func BenchmarkMicroserviceScenario(b *testing.B) {
	tracer := tracez.New("api-gateway")
	collector := tracez.NewCollector("microservice-traces", 10000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// API Gateway receives request.
		gatewayCtx, gatewaySpan := tracer.StartSpan(ctx, "gateway.request")
		gatewaySpan.SetTag("service.name", "api-gateway")
		gatewaySpan.SetTag("trace.id", fmt.Sprintf("trace-%d", i))

		// Auth service call.
		authCtx, authSpan := tracer.StartSpan(gatewayCtx, "service.auth")
		authSpan.SetTag("service.name", "auth-service")
		authSpan.SetTag("operation", "validate_token")
		time.Sleep(time.Nanosecond * time.Duration(50+rand.Intn(100)))
		authSpan.Finish()

		// User service call.
		userCtx, userSpan := tracer.StartSpan(authCtx, "service.user")
		userSpan.SetTag("service.name", "user-service")
		userSpan.SetTag("operation", "get_profile")

		// User service makes its own DB call.
		_, userDBSpan := tracer.StartSpan(userCtx, "db.user")
		userDBSpan.SetTag("db.table", "users")
		userDBSpan.SetTag("db.query", "SELECT")
		time.Sleep(time.Nanosecond * time.Duration(80+rand.Intn(200)))
		userDBSpan.Finish()

		userSpan.Finish()

		// Order service call (parallel to user service in real scenario).
		orderCtx, orderSpan := tracer.StartSpan(authCtx, "service.order")
		orderSpan.SetTag("service.name", "order-service")
		orderSpan.SetTag("operation", "list_orders")

		// Order service database calls.
		_, orderDBSpan := tracer.StartSpan(orderCtx, "db.orders")
		orderDBSpan.SetTag("db.table", "orders")
		orderDBSpan.SetTag("db.query", "SELECT")
		time.Sleep(time.Nanosecond * time.Duration(120+rand.Intn(300)))
		orderDBSpan.Finish()

		orderSpan.Finish()

		// Response aggregation.
		gatewaySpan.SetTag("response.services", "3")
		gatewaySpan.SetTag("response.status", "success")
		gatewaySpan.Finish()

		// Export periodically.
		if i%1000 == 0 {
			collector.Export()
		}
	}
}

// BenchmarkDatabaseQueryScenario simulates database-heavy workloads.
func BenchmarkDatabaseQueryScenario(b *testing.B) {
	tracer := tracez.New("database-service")
	collector := tracez.NewCollector("db-traces", 5000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Query patterns based on real applications.
	queryPatterns := []struct {
		name     string
		queries  int
		hasIndex bool
	}{
		{"simple_select", 1, true},
		{"join_query", 1, true},
		{"aggregation", 1, false},
		{"n_plus_one", 10, true}, // Anti-pattern.
		{"batch_insert", 1, true},
		{"complex_report", 5, false},
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		pattern := queryPatterns[rand.Intn(len(queryPatterns))]

		// Request span.
		reqCtx, reqSpan := tracer.StartSpan(ctx, "db.request")
		reqSpan.SetTag("pattern", pattern.name)
		reqSpan.SetTag("query.count", fmt.Sprintf("%d", pattern.queries))

		// Transaction span.
		txCtx, txSpan := tracer.StartSpan(reqCtx, "db.transaction")
		txSpan.SetTag("isolation", "read_committed")

		for j := 0; j < pattern.queries; j++ {
			_, querySpan := tracer.StartSpan(txCtx, "db.query")
			querySpan.SetTag("query.index", fmt.Sprintf("%d", j))
			querySpan.SetTag("query.indexed", fmt.Sprintf("%v", pattern.hasIndex))

			// Simulate query execution time based on whether it's indexed.
			var queryTime time.Duration
			if pattern.hasIndex {
				queryTime = time.Nanosecond * time.Duration(100+rand.Intn(200))
			} else {
				queryTime = time.Nanosecond * time.Duration(500+rand.Intn(1000))
			}
			time.Sleep(queryTime)

			if !pattern.hasIndex {
				querySpan.SetTag("query.slow", "true")
			}

			querySpan.Finish()
		}

		txSpan.SetTag("queries.executed", fmt.Sprintf("%d", pattern.queries))
		txSpan.Finish()
		reqSpan.Finish()

		// Export periodically.
		if i%200 == 0 {
			collector.Export()
		}
	}
}

// BenchmarkWorkerPoolScenario simulates background job processing.
func BenchmarkWorkerPoolScenario(b *testing.B) {
	tracer := tracez.New("worker-pool")
	collector := tracez.NewCollector("worker-traces", 5000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Job types with different processing characteristics.
	jobTypes := []struct {
		name         string
		cpuIntensive bool
		ioIntensive  bool
		duration     time.Duration
	}{
		{"image_resize", true, false, time.Nanosecond * 1000},
		{"email_send", false, true, time.Nanosecond * 500},
		{"data_export", false, true, time.Nanosecond * 2000},
		{"webhook_call", false, true, time.Nanosecond * 800},
		{"cache_warm", false, false, time.Nanosecond * 200},
	}

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	// Simulate multiple workers processing jobs.
	var wg sync.WaitGroup
	var processed int64
	numWorkers := 4
	jobsPerWorker := b.N / numWorkers

	for workerID := 0; workerID < numWorkers; workerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < jobsPerWorker; j++ {
				jobType := jobTypes[rand.Intn(len(jobTypes))]

				// Worker span.
				workerCtx, workerSpan := tracer.StartSpan(ctx, "worker.job")
				workerSpan.SetTag("worker.id", fmt.Sprintf("%d", id))
				workerSpan.SetTag("job.type", jobType.name)
				workerSpan.SetTag("job.cpu_intensive", fmt.Sprintf("%v", jobType.cpuIntensive))
				workerSpan.SetTag("job.io_intensive", fmt.Sprintf("%v", jobType.ioIntensive))

				// Job processing steps.
				if jobType.cpuIntensive {
					_, cpuSpan := tracer.StartSpan(workerCtx, "job.cpu_process")
					cpuSpan.SetTag("cpu.cores", fmt.Sprintf("%d", runtime.GOMAXPROCS(0)))
					time.Sleep(jobType.duration)
					cpuSpan.Finish()
				}

				if jobType.ioIntensive {
					_, ioSpan := tracer.StartSpan(workerCtx, "job.io_process")
					ioSpan.SetTag("io.type", "network")
					time.Sleep(jobType.duration)
					ioSpan.Finish()
				}

				// Completion tracking.
				_, trackSpan := tracer.StartSpan(workerCtx, "job.tracking")
				trackSpan.SetTag("status", "completed")
				time.Sleep(time.Nanosecond * 50)
				trackSpan.Finish()

				workerSpan.SetTag("job.status", "completed")
				workerSpan.Finish()

				atomic.AddInt64(&processed, 1)
			}
		}(workerID)
	}

	wg.Wait()

	// Final export.
	collector.Export()
	b.ReportMetric(float64(processed), "jobs-processed")
}

// BenchmarkStreamingScenario simulates real-time data streaming.
func BenchmarkStreamingScenario(b *testing.B) {
	tracer := tracez.New("streaming-service")
	collector := tracez.NewCollector("stream-traces", 10000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	// Simulate streaming events.
	for i := 0; i < b.N; i++ {
		// Event ingestion.
		eventCtx, eventSpan := tracer.StartSpan(ctx, "stream.event")
		eventSpan.SetTag("event.id", fmt.Sprintf("evt-%d", i))
		eventSpan.SetTag("event.type", []string{"user_action", "system_metric", "error"}[rand.Intn(3)])

		// Validation.
		validCtx, validSpan := tracer.StartSpan(eventCtx, "stream.validate")
		validSpan.SetTag("validation.rules", "3")
		time.Sleep(time.Nanosecond * 20)
		validSpan.Finish()

		// Enrichment.
		enrichCtx, enrichSpan := tracer.StartSpan(validCtx, "stream.enrich")
		enrichSpan.SetTag("enrichment.sources", "2")
		time.Sleep(time.Nanosecond * 50)
		enrichSpan.Finish()

		// Multiple downstream processors.
		for procID := 0; procID < 3; procID++ {
			_, procSpan := tracer.StartSpan(enrichCtx, fmt.Sprintf("stream.process_%d", procID))
			procSpan.SetTag("processor.id", fmt.Sprintf("%d", procID))
			procSpan.SetTag("processor.type", []string{"analytics", "alerting", "storage"}[procID])
			time.Sleep(time.Nanosecond * time.Duration(30+rand.Intn(70)))
			procSpan.Finish()
		}

		eventSpan.SetTag("processors.count", "3")
		eventSpan.SetTag("event.status", "processed")
		eventSpan.Finish()

		// High-frequency export for streaming.
		if i%100 == 0 {
			collector.Export()
		}
	}
}

// BenchmarkErrorScenario tests tracing behavior under error conditions.
func BenchmarkErrorScenario(b *testing.B) {
	tracer := tracez.New("error-service")
	collector := tracez.NewCollector("error-traces", 3000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Request that will encounter errors.
		reqCtx, reqSpan := tracer.StartSpan(ctx, "error.request")
		reqSpan.SetTag("request.id", fmt.Sprintf("req-%d", i))

		// Introduce errors randomly.
		errorRate := 20 // 20% error rate.
		willError := rand.Intn(100) < errorRate

		if willError {
			// Service error.
			errCtx, errSpan := tracer.StartSpan(reqCtx, "service.operation")
			errSpan.SetTag("error.type", []string{"timeout", "connection", "validation"}[rand.Intn(3)])
			errSpan.SetTag("error.code", []string{"500", "502", "400"}[rand.Intn(3)])
			errSpan.SetTag("error.message", "Operation failed")
			time.Sleep(time.Nanosecond * time.Duration(100+rand.Intn(500))) // Error delays.
			errSpan.Finish()

			// Retry logic.
			_, retrySpan := tracer.StartSpan(errCtx, "retry.operation")
			retrySpan.SetTag("retry.attempt", "1")

			// Some retries succeed.
			if rand.Intn(2) == 0 {
				retrySpan.SetTag("retry.result", "success")
				time.Sleep(time.Nanosecond * 200)
			} else {
				retrySpan.SetTag("retry.result", "failed")
				retrySpan.SetTag("error.final", "true")
				time.Sleep(time.Nanosecond * 50)
			}
			retrySpan.Finish()

			reqSpan.SetTag("request.status", "error")
		} else {
			// Successful operation.
			_, successSpan := tracer.StartSpan(reqCtx, "service.operation")
			successSpan.SetTag("operation.result", "success")
			time.Sleep(time.Nanosecond * time.Duration(50+rand.Intn(150)))
			successSpan.Finish()

			reqSpan.SetTag("request.status", "success")
		}

		reqSpan.Finish()

		// Export periodically.
		if i%300 == 0 {
			collector.Export()
		}
	}
}

// BenchmarkHighCardinalityScenario tests performance with many unique tag values.
func BenchmarkHighCardinalityScenario(b *testing.B) {
	tracer := tracez.New("high-cardinality-service")
	collector := tracez.NewCollector("cardinality-traces", 5000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, span := tracer.StartSpan(ctx, "high.cardinality")

		// High cardinality tags (common anti-pattern).
		span.SetTag("user.id", fmt.Sprintf("user-%d", rand.Intn(100000)))
		span.SetTag("session.id", fmt.Sprintf("sess-%d", rand.Intn(50000)))
		span.SetTag("request.id", fmt.Sprintf("req-%d", i))
		span.SetTag("timestamp", fmt.Sprintf("%d", time.Now().UnixNano()))
		span.SetTag("random.value", fmt.Sprintf("%d", rand.Intn(1000000)))

		// Some lower cardinality tags mixed in.
		span.SetTag("service.version", []string{"1.0.0", "1.1.0", "1.2.0"}[rand.Intn(3)])
		span.SetTag("environment", []string{"prod", "staging", "dev"}[rand.Intn(3)])
		span.SetTag("region", []string{"us-east", "us-west", "eu-west"}[rand.Intn(3)])

		span.Finish()

		// More frequent exports due to memory concerns.
		if i%100 == 0 {
			collector.Export()
		}
	}
}

// Helper function for weighted selection.
func weightedSelect(options []struct {
	name     string
	weight   int
	dbCalls  int
	apiCalls int
}, value int) int {
	cumulative := 0
	for i, option := range options {
		cumulative += option.weight
		if value < cumulative {
			return i
		}
	}
	return len(options) - 1
}
