package main

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

func TestWorkerPoolCreation(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	pool := NewWorkerPool(3, tracer)
	if pool.workers != 3 {
		t.Errorf("Expected 3 workers, got %d", pool.workers)
	}
	if pool.tracer != tracer {
		t.Error("Tracer not set correctly")
	}
	if pool.ctx == nil {
		t.Error("Context not initialized")
	}
	if pool.cancel == nil {
		t.Error("Cancel func not initialized")
	}

	pool.Stop()
}

func TestWorkerPoolJobProcessing(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)

	pool := NewWorkerPool(2, tracer)
	pool.Start()

	// Submit jobs.
	jobs := []Job{
		{ID: 1, Type: "cache-read", Data: "test1"},
		{ID: 2, Type: "database", Data: "test2"},
		{ID: 3, Type: "vendor-api", Data: "test3"},
	}

	for _, job := range jobs {
		pool.Submit(job)
	}

	// Collect results.
	results := make([]Result, 0, len(jobs))
	done := make(chan bool)
	go func() {
		for i := 0; i < len(jobs); i++ {
			select {
			case result := <-pool.Results():
				results = append(results, result)
			case <-time.After(3 * time.Second):
				t.Error("Timeout waiting for results")
				done <- false
				return
			}
		}
		done <- true
	}()

	success := <-done
	if !success {
		t.Fatal("Failed to collect results")
	}

	pool.Stop()

	// Verify results.
	if len(results) != len(jobs) {
		t.Errorf("Expected %d results, got %d", len(jobs), len(results))
	}

	// Verify all job IDs present in results.
	for _, job := range jobs {
		found := false
		for _, result := range results {
			if result.JobID == job.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Job %d not found in results", job.ID)
		}
	}

	// Check traces.
	time.Sleep(50 * time.Millisecond)
	spans := collector.Export()
	if len(spans) == 0 {
		t.Error("No spans collected")
	}

	// Verify worker spans exist.
	workerSpans := 0
	for _, span := range spans {
		if strings.HasPrefix(span.Name, "worker-") {
			workerSpans++
		}
	}
	if workerSpans != len(jobs) {
		t.Errorf("Expected %d worker spans, got %d", len(jobs), workerSpans)
	}
}

func TestJobTypeProcessing(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	pool := NewWorkerPool(1, tracer)

	tests := []struct {
		jobType      string
		expectedSpan string
	}{
		{"cache-read", "cache.read"},
		{"database", "database.query"},
		{"vendor-api", "vendor.api"},
	}

	for _, tt := range tests {
		t.Run(tt.jobType, func(t *testing.T) {
			collector.Reset()

			ctx, rootSpan := tracer.StartSpan(context.Background(), "test-root")
			job := Job{ID: 1, Type: tt.jobType, Data: "test"}

			result := pool.processJob(ctx, job)
			rootSpan.Finish()

			if result == "" {
				t.Error("Empty result returned")
			}

			time.Sleep(50 * time.Millisecond)
			spans := collector.Export()

			// Find expected span.
			found := false
			for _, span := range spans {
				if span.Name == tt.expectedSpan {
					found = true
					break
				}
			}

			if !found {
				t.Errorf("Expected span %s not found", tt.expectedSpan)
			}
		})
	}
}

func TestConcurrentWorkers(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)

	pool := NewWorkerPool(3, tracer)
	pool.Start()

	// Submit many jobs to ensure concurrent processing.
	numJobs := 9
	for i := 0; i < numJobs; i++ {
		pool.Submit(Job{
			ID:   i,
			Type: "cache-read",
			Data: fmt.Sprintf("job-%d", i),
		})
	}

	// Drain results to prevent blocking.
	done := make(chan bool)
	go func() {
		for i := 0; i < numJobs; i++ {
			select {
			case <-pool.Results():
				// Got result.
			case <-time.After(500 * time.Millisecond):
				// Timeout occurred.
			}
		}
		done <- true
	}()

	// Wait for processing.
	<-done
	pool.Stop()

	spans := collector.Export()

	// Check that multiple workers were used.
	workerIDs := make(map[string]bool)
	for _, span := range spans {
		if workerID, ok := span.Tags["worker.id"]; ok {
			workerIDs[workerID] = true
		}
	}

	if len(workerIDs) < 2 {
		t.Errorf("Expected at least 2 workers to be used, got %d", len(workerIDs))
	}

	// Check for concurrent execution.
	overlapping := 0
	for i := 0; i < len(spans)-1; i++ {
		for j := i + 1; j < len(spans); j++ {
			if spans[i].StartTime.Before(spans[j].EndTime) &&
				spans[j].StartTime.Before(spans[i].EndTime) {
				overlapping++
			}
		}
	}

	if overlapping == 0 {
		t.Error("No overlapping spans found, workers not running concurrently")
	}
}

func TestWorkerPoolShutdown(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	pool := NewWorkerPool(2, tracer)
	pool.Start()

	// Submit a job.
	pool.Submit(Job{ID: 1, Type: "cache-read", Data: "test"})

	// Stop should close channels and wait for workers.
	done := make(chan bool)
	go func() {
		pool.Stop()
		done <- true
	}()

	select {
	case <-done:
		// Success.
	case <-time.After(1 * time.Second):
		t.Error("Shutdown timed out")
	}

	// Verify channels are closed.
	select {
	case _, ok := <-pool.jobs:
		if ok {
			t.Error("Jobs channel not closed")
		}
	default:
		// Channel might be empty but closed.
	}
}

func TestCacheReadSpans(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)

	pool := NewWorkerPool(1, tracer)
	ctx := context.Background()

	job := Job{ID: 1, Type: "cache-read", Data: "test"}
	result := pool.processCacheRead(ctx, job)

	if !strings.Contains(result, "Cache hit for job 1") {
		t.Errorf("Unexpected result: %s", result)
	}

	time.Sleep(50 * time.Millisecond)
	spans := collector.Export()

	// Check for cache read span.
	hasCache := false
	for _, span := range spans {
		if span.Name == "cache.read" {
			hasCache = true
			if span.Tags["cache.key"] != "test" {
				t.Error("Cache key not set correctly")
			}
		}
	}

	if !hasCache {
		t.Error("Cache read span not found")
	}
}

func TestThreadSafety(_ *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	pool := NewWorkerPool(5, tracer)
	pool.Start()

	// Concurrent job submission.
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			pool.Submit(Job{
				ID:   id,
				Type: "cache-read",
				Data: fmt.Sprintf("concurrent-%d", id),
			})
		}(i)
	}

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	pool.Stop()

	// If we get here without panic, thread safety is good.
}

// TestBottleneckDetection verifies tracez can detect the blocking pattern
func TestBottleneckDetection(t *testing.T) {
	tracer := tracez.New("test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)

	pool := NewWorkerPool(2, tracer)
	pool.Start()

	// Submit mix of fast and slow jobs
	jobs := []Job{
		{ID: 1, Type: "cache-read", Data: "fast1"},
		{ID: 2, Type: "vendor-api", Data: "slow1"},
		{ID: 3, Type: "cache-read", Data: "fast2"},
		{ID: 4, Type: "cache-read", Data: "fast3"},
	}

	for _, job := range jobs {
		pool.Submit(job)
	}

	// Slow consumer to trigger blocking
	resultCount := 0
	go func() {
		for range pool.Results() {
			resultCount++
			time.Sleep(100 * time.Millisecond) // Slow consumer
			if resultCount >= len(jobs) {
				break
			}
		}
	}()

	time.Sleep(1 * time.Second)
	pool.Stop()

	// Check for blocked sends in traces
	spans := collector.Export()
	var blockedSends int
	for _, span := range spans {
		if span.Name == "result.send" && span.Tags["channel.blocked"] == "true" {
			blockedSends++
		}
	}

	if blockedSends == 0 {
		t.Log("No blocked sends detected - consumer might be fast enough")
	} else {
		t.Logf("Detected %d blocked result sends - bottleneck confirmed!", blockedSends)
	}
}
