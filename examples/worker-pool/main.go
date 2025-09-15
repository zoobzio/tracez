// THE MYSTERY: Black Friday morning. Payment service dying.
// Load balancer shows workers healthy. CPU at 40%. Memory fine.
// But checkout requests timing out. 12 req/sec instead of 1000+.
//
// TRIED EVERYTHING: Restarted service. Scaled workers 4x → 16x.
// Added connection pools. Tuned garbage collector. Zero improvement.
// Senior engineer on call. CTO on Slack. CEO asking questions.
//
// LOGS SHOWED: "Worker-3 processing job 47", "Worker-1 processing job 12"
// Workers looked busy. But WHERE were the completions?
//
// ROOT CAUSE: Payment gateway having "minor latency issues" (their words).
// Created results channel bottleneck that starved ALL workers.
// Fast cache reads trapped behind slow API calls. Classic head-of-line blocking.
// Only distributed tracing could see workers blocked on result delivery.

package main

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"time"

	"github.com/zoobzio/tracez"
)

// Job represents work to be done.
type Job struct {
	ID       int
	Type     string
	Priority string // "critical", "normal", "batch"
	Data     string
	Timeout  time.Duration
}

// Result carries job output with timing metadata.
type Result struct {
	JobID       int
	WorkerID    int
	Output      string
	QueueTime   time.Duration
	ProcessTime time.Duration
}

// WorkerPool manages concurrent workers with tracing.
type WorkerPool struct {
	tracer  *tracez.Tracer
	ctx     context.Context
	cancel  context.CancelFunc
	jobs    chan Job
	results chan Result
	wg      sync.WaitGroup
	workers int

	// Metrics for debugging
	submitted int
	processed int
	mu        sync.Mutex
}

// NewWorkerPool creates a traced worker pool.
func NewWorkerPool(workers int, tracer *tracez.Tracer) *WorkerPool {
	ctx, cancel := context.WithCancel(context.Background())
	return &WorkerPool{
		workers: workers,
		jobs:    make(chan Job, workers*10), // Large buffer to see queueing
		results: make(chan Result, workers), // Small buffer - THE BOTTLENECK
		tracer:  tracer,
		ctx:     ctx,
		cancel:  cancel,
	}
}

// Start launches worker goroutines.
func (p *WorkerPool) Start() {
	for i := 0; i < p.workers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// worker processes jobs with tracing.
func (p *WorkerPool) worker(id int) {
	defer p.wg.Done()

	for {
		select {
		case <-p.ctx.Done():
			return
		case job, ok := <-p.jobs:
			if !ok {
				return
			}

			queueStart := time.Now()

			// Start worker span
			workerKey := fmt.Sprintf("worker-%d", id)
			ctx, span := p.tracer.StartSpan(p.ctx, workerKey)
			span.SetTag("worker.id", fmt.Sprintf("%d", id))
			span.SetTag("job.id", fmt.Sprintf("%d", job.ID))
			span.SetTag("job.type", job.Type)
			span.SetTag("job.priority", job.Priority)

			// Process with timeout
			processStart := time.Now()
			output := p.processJob(ctx, job)
			processTime := time.Since(processStart)

			result := Result{
				JobID:       job.ID,
				WorkerID:    id,
				Output:      output,
				QueueTime:   processStart.Sub(queueStart),
				ProcessTime: processTime,
			}

			// THE PROBLEM: This blocks when results channel is full!
			// Worker can't take new jobs while waiting to deliver results.
			_, sendSpan := p.tracer.StartSpan(ctx, "result.send")
			sendSpan.SetTag("channel.blocked", "false")

			sendStart := time.Now()
			select {
			case p.results <- result:
				sendDuration := time.Since(sendStart)
				if sendDuration > 10*time.Millisecond {
					sendSpan.SetTag("channel.blocked", "true")
					sendSpan.SetTag("block.duration", sendDuration.String())
				}
				sendSpan.Finish()
			case <-p.ctx.Done():
				sendSpan.Finish()
				return
			}

			span.SetTag("job.status", "completed")
			span.SetTag("process.duration", processTime.String())
			span.Finish()

			p.mu.Lock()
			p.processed++
			p.mu.Unlock()
		}
	}

}

// processJob simulates different job types with realistic latencies.
func (p *WorkerPool) processJob(ctx context.Context, job Job) string {
	switch job.Type {
	case "cache-read":
		return p.processCacheRead(ctx, job)
	case "database":
		return p.processDatabase(ctx, job)
	case "vendor-api":
		return p.processVendorAPI(ctx, job)
	default:
		return fmt.Sprintf("Unknown job type: %s", job.Type)
	}

}

// processCacheRead - FAST: 1-5ms, should complete quickly
func (p *WorkerPool) processCacheRead(ctx context.Context, job Job) string {
	_, span := p.tracer.StartSpan(ctx, "cache.read")
	span.SetTag("cache.key", job.Data)
	defer span.Finish()

	// Fast operation
	time.Sleep(time.Duration(1+rand.Intn(4)) * time.Millisecond)
	return fmt.Sprintf("Cache hit for job %d: %s", job.ID, job.Data)
}

// processDatabase - MEDIUM: 10-50ms, typical database query
func (p *WorkerPool) processDatabase(ctx context.Context, job Job) string {
	_, span := p.tracer.StartSpan(ctx, "database.query")
	span.SetTag("query.type", "select")
	span.SetTag("table", job.Data)
	defer span.Finish()

	// Connection pool acquisition
	_, connSpan := p.tracer.StartSpan(ctx, "pool.acquire")
	time.Sleep(time.Duration(2+rand.Intn(5)) * time.Millisecond)
	connSpan.Finish()

	// Query execution
	_, querySpan := p.tracer.StartSpan(ctx, "query.execute")
	time.Sleep(time.Duration(10+rand.Intn(40)) * time.Millisecond)
	querySpan.Finish()

	return fmt.Sprintf("Database query %d completed: %s", job.ID, job.Data)
}

// processVendorAPI - SLOW: 100-2000ms, external vendor API (THE KILLER)
func (p *WorkerPool) processVendorAPI(ctx context.Context, job Job) string {
	_, span := p.tracer.StartSpan(ctx, "vendor.api")
	span.SetTag("api.endpoint", job.Data)
	span.SetTag("api.vendor", "payment-gateway")
	defer span.Finish()

	// DNS resolution
	_, dnsSpan := p.tracer.StartSpan(ctx, "dns.lookup")
	time.Sleep(time.Duration(5+rand.Intn(10)) * time.Millisecond)
	dnsSpan.Finish()

	// TLS handshake
	_, tlsSpan := p.tracer.StartSpan(ctx, "tls.handshake")
	time.Sleep(time.Duration(10+rand.Intn(20)) * time.Millisecond)
	tlsSpan.Finish()

	// API call - Sometimes VERY slow
	_, apiSpan := p.tracer.StartSpan(ctx, "api.call")

	// Simulate vendor having issues (20% chance of severe slowdown)
	if rand.Float32() < 0.2 {
		apiSpan.SetTag("api.degraded", "true")
		time.Sleep(time.Duration(500+rand.Intn(1500)) * time.Millisecond)
	} else {
		apiSpan.SetTag("api.degraded", "false")
		time.Sleep(time.Duration(100+rand.Intn(200)) * time.Millisecond)
	}
	apiSpan.Finish()

	return fmt.Sprintf("Vendor API %d response: %s", job.ID, job.Data)
}

// Submit adds a job to the queue.
func (p *WorkerPool) Submit(job Job) bool {
	p.mu.Lock()
	p.submitted++
	p.mu.Unlock()

	select {
	case p.jobs <- job:
		return true
	case <-time.After(100 * time.Millisecond):
		// Queue full!
		return false
	case <-p.ctx.Done():
		return false
	}

}

// Results returns the results channel.
func (p *WorkerPool) Results() <-chan Result {
	return p.results
}

// Stop gracefully shuts down the pool.
func (p *WorkerPool) Stop() {
	close(p.jobs)
	p.wg.Wait()
	p.cancel()
	close(p.results)
}

func main() {
	// Setup tracer.
	tracer := tracez.New("worker-pool")
	defer tracer.Close()

	collector := tracez.NewCollector("pool-traces", 5000)
	tracer.AddCollector("collector", collector)

	// Create worker pool with 4 workers.
	pool := NewWorkerPool(4, tracer)
	pool.Start()

	fmt.Println("=== BLACK FRIDAY MORNING SIMULATION ===")
	fmt.Println("🛒 E-commerce checkout flow under load")
	fmt.Println("⚡ Workers: 4 (same as production)")
	fmt.Println("📊 Expected: 1000+ req/sec | Actual: ???")
	fmt.Println("🔥 Payment gateway reporting 'minor latency issues'")
	fmt.Println()

	// BLACK FRIDAY CHECKOUT FLOW - exactly what kills production
	jobs := []Job{
		// 🚀 FAST LANE: User session validation (1-5ms each)
		{ID: 1, Type: "cache-read", Priority: "normal", Data: "user:premium_customer"},
		{ID: 2, Type: "cache-read", Priority: "normal", Data: "session:authenticated"},
		{ID: 3, Type: "cache-read", Priority: "normal", Data: "cart:items_valid"},

		// 💳 PAYMENT: First vendor API call sneaks in (100-2000ms)
		{ID: 4, Type: "vendor-api", Priority: "critical", Data: "/payment/authorize"},

		// 🏃‍♂️ MORE FAST OPERATIONS: Should complete instantly
		{ID: 5, Type: "cache-read", Priority: "normal", Data: "product:pricing"},
		{ID: 6, Type: "database", Priority: "normal", Data: "inventory_check"},
		{ID: 7, Type: "cache-read", Priority: "normal", Data: "promotion:black_friday"},

		// 🕵️‍♀️ FRAUD CHECK: Another slow API (security can't be skipped)
		{ID: 8, Type: "vendor-api", Priority: "critical", Data: "/fraud/check"},

		// ⚡ SHOULD BE INSTANT: But now queued behind slow APIs
		{ID: 9, Type: "cache-read", Priority: "normal", Data: "shipping:zones"},
		{ID: 10, Type: "cache-read", Priority: "normal", Data: "tax:rates"},
		{ID: 11, Type: "database", Priority: "normal", Data: "user_preferences"},
		{ID: 12, Type: "cache-read", Priority: "normal", Data: "recommendations"},

		// 📦 MORE SLOW APIS: Creating the deadly bottleneck
		{ID: 13, Type: "vendor-api", Priority: "critical", Data: "/shipping/calculate"},
		{ID: 14, Type: "vendor-api", Priority: "critical", Data: "/tax/calculate"},

		// 😱 TRAPPED FAST OPERATIONS: Customer sees "loading..." forever
		{ID: 15, Type: "cache-read", Priority: "normal", Data: "order:confirmation"},
		{ID: 16, Type: "cache-read", Priority: "normal", Data: "email:template"},
		{ID: 17, Type: "database", Priority: "normal", Data: "receipt:generate"},
		{ID: 18, Type: "cache-read", Priority: "normal", Data: "analytics:event"},
		{ID: 19, Type: "cache-read", Priority: "normal", Data: "notification:send"},
		{ID: 20, Type: "database", Priority: "normal", Data: "audit:log"},
	}

	// Submit jobs and track timing
	submitStart := time.Now()
	for _, job := range jobs {
		if pool.Submit(job) {
			fmt.Printf("[%3dms] Submitted job %2d (%s)\n",
				time.Since(submitStart).Milliseconds(), job.ID, job.Type)
		} else {
			fmt.Printf("[%3dms] FAILED to submit job %2d - Queue full!\n",
				time.Since(submitStart).Milliseconds(), job.ID)
		}
	}

	// Start result consumer - SIMULATING SLOW RESPONSE SERIALIZATION
	// (Real production: JSON marshal, HTTP response, logging, metrics)
	resultCount := 0
	done := make(chan bool)
	go func() {
		for result := range pool.Results() {
			resultCount++

			// THE HIDDEN KILLER: Slow result processing (database writes, metrics, etc)
			time.Sleep(50 * time.Millisecond)

			// Show the tragedy: fast operations waiting for slow results
			status := "✅"
			if result.ProcessTime < 10*time.Millisecond {
				status = "⚡" // Should have been instant
			} else if result.ProcessTime > 500*time.Millisecond {
				status = "🐌" // Slow vendor API
			}

			fmt.Printf("[%3dms] %s Result %2d from worker %d (queue: %dms, process: %dms): %s\n",
				time.Since(submitStart).Milliseconds(),
				status,
				result.JobID,
				result.WorkerID,
				result.QueueTime.Milliseconds(),
				result.ProcessTime.Milliseconds(),
				result.Output)

			if resultCount >= len(jobs) {
				done <- true
				return
			}
		}
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		fmt.Println("\nAll jobs completed")
	case <-time.After(10 * time.Second):
		fmt.Printf("\nTIMEOUT: Only %d/%d jobs completed in 10 seconds!\n", resultCount, len(jobs))
	}

	// Export and analyze traces - THE REVELATION
	spans := collector.Export()
	fmt.Printf("\n=== 🔍 FORENSIC ANALYSIS - THE SMOKING GUN ===\n")
	fmt.Printf("🔬 Distributed traces collected: %d spans\n", len(spans))
	fmt.Printf("🕵️‍♂️ Analyzing worker behavior patterns...\n")

	// Find blocked result sends
	var blockedSends []tracez.Span
	var vendorAPICalls []tracez.Span
	var cacheCalls []tracez.Span

	for i := range spans {
		span := &spans[i]

		// Find blocked channel sends
		if span.Name == "result.send" && span.Tags["channel.blocked"] == "true" {
			blockedSends = append(blockedSends, *span)
		}

		// Categorize operations
		if span.Name == "vendor.api" {
			vendorAPICalls = append(vendorAPICalls, *span)
		} else if span.Name == "cache.read" {
			cacheCalls = append(cacheCalls, *span)
		}
	}

	fmt.Printf("\n🚨 DISCOVERY #1: Worker Starvation Detected!\n")
	if len(blockedSends) > 0 {
		fmt.Printf("  💥 CRITICAL: Found %d blocked result channel sends!\n", len(blockedSends))
		for _, span := range blockedSends[:min(3, len(blockedSends))] {
			fmt.Printf("    🔒 Worker stuck for %s trying to deliver result\n", span.Tags["block.duration"])
		}
		fmt.Println("  💀 FATAL FLAW: Workers can't take new jobs while blocked!")
		fmt.Println("  🎯 THIS IS YOUR BLACK FRIDAY KILLER!")
	} else {
		fmt.Println("  ✅ No blocking detected (results channel flowing)")
	}

	fmt.Printf("\n⚡ DISCOVERY #2: Performance Cliff Identified!\n")
	if len(vendorAPICalls) > 0 && len(cacheCalls) > 0 {
		var vendorTotal, cacheTotal time.Duration
		for _, span := range vendorAPICalls {
			vendorTotal += span.Duration
		}
		for _, span := range cacheCalls {
			cacheTotal += span.Duration
		}

		vendorAvg := vendorTotal / time.Duration(len(vendorAPICalls))
		cacheAvg := cacheTotal / time.Duration(len(cacheCalls))

		fmt.Printf("  🏎️  Cache operations: %dms avg (%d calls) - FAST LANE\n", cacheAvg.Milliseconds(), len(cacheCalls))
		fmt.Printf("  🐌 Vendor API calls: %dms avg (%d calls) - DANGER ZONE\n", vendorAvg.Milliseconds(), len(vendorAPICalls))
		fmt.Printf("  💥 PERFORMANCE CLIFF: Vendor APIs are %dx slower!\n", vendorAvg/cacheAvg)
		fmt.Printf("  🔥 ONE slow API call blocks ENTIRE worker pool!\n")
	}

	// Find head-of-line blocking
	fmt.Printf("\n🔒 DISCOVERY #3: Head-of-Line Blocking Pattern!\n")
	workerLastSeen := make(map[string]time.Time)
	var maxGap time.Duration
	var stuckWorker string

	for i := range spans {
		span := &spans[i]
		if workerID, ok := span.Tags["worker.id"]; ok {
			if lastTime, exists := workerLastSeen[workerID]; exists {
				gap := span.StartTime.Sub(lastTime)
				if gap > maxGap {
					maxGap = gap
					stuckWorker = workerID
				}
			}
			workerLastSeen[workerID] = span.EndTime
		}
	}

	if maxGap > 100*time.Millisecond {
		fmt.Printf("  🚫 Worker-%s stuck for %dms between jobs!\n", stuckWorker, maxGap.Milliseconds())
		fmt.Printf("  💡 REVELATION: Worker wasn't processing - it was BLOCKED!\n")
		fmt.Printf("  🔗 Classic head-of-line blocking pattern identified!\n")
	} else {
		fmt.Printf("  ✅ Workers transitioning smoothly between jobs\n")
	}

	fmt.Printf("\n💀 BLACK FRIDAY DAMAGE REPORT\n")
	actualThroughput := float64(pool.processed) / time.Since(submitStart).Seconds()
	fmt.Printf("  📈 Expected throughput: 1000+ req/sec\n")
	fmt.Printf("  📉 Actual throughput:   %.1f req/sec\n", actualThroughput)
	fmt.Printf("  💸 Performance loss:    %.0f%% - CUSTOMERS ABANDONING CARTS!\n",
		(1000-actualThroughput)/1000*100)

	fmt.Printf("\n🔬 FORENSIC CONCLUSION - THE PERFECT STORM:\n")
	fmt.Println("  1️⃣ Payment gateway 'minor latency' = 10-100x slower response times")
	fmt.Println("  2️⃣ Small results channel (4 slots) becomes bottleneck instantly")
	fmt.Println("  3️⃣ Slow result processing (JSON, DB writes, metrics) backs up channel")
	fmt.Println("  4️⃣ Workers block delivering results → can't accept new work")
	fmt.Println("  5️⃣ Fast cache reads trapped behind slow API calls")
	fmt.Println("  6️⃣ Head-of-line blocking destroys entire system throughput")

	fmt.Printf("\n💊 IMMEDIATE FIXES (Choose ONE):\n")
	fmt.Println("  🚀 Option A: Increase results buffer: make(chan Result, workers*10)")
	fmt.Println("  🎯 Option B: Non-blocking send with select/default")
	fmt.Println("  🏃‍♂️ Option C: Separate fast/slow worker pools")
	fmt.Println("  🔄 Option D: Async result processing with queue")

	fmt.Printf("\n🛡️ LONG-TERM PROTECTION:\n")
	fmt.Println("  📊 Add distributed tracing to ALL production worker pools")
	fmt.Println("  ⏰ Monitor result channel blocking duration")
	fmt.Println("  🚨 Alert when workers idle >100ms between jobs")
	fmt.Println("  🔍 THIS PATTERN EXISTS IN YOUR CODE - CHECK NOW!")

	// Shutdown pool.
	pool.Stop()
	fmt.Printf("\nWorker pool shut down\n")
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
