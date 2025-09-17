package reliability

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// Span memory pressure tests - verify span operations under memory constraints
// Tests tag map expansion, concurrent modifications, and memory cleanup patterns

func TestSpanMemoryPressure(t *testing.T) {
	config := getReliabilityConfig()

	switch config.Level {
	case "basic":
		t.Run("tag_expansion", testTagExpansion)
		t.Run("concurrent_tags", testConcurrentTags)
		t.Run("span_cleanup", testSpanCleanup)
	case "stress":
		t.Run("massive_tag_load", testMassiveTagLoad)
		t.Run("memory_fragmentation", testMemoryFragmentation)
		t.Run("gc_pressure", testGCPressure)
	default:
		t.Skip("TRACEZ_RELIABILITY_LEVEL not set, skipping reliability tests")
	}
}

// testTagExpansion verifies span tag maps handle growth correctly.
func testTagExpansion(t *testing.T) {
	tracer := tracez.New("tag-expansion-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Test progressive tag expansion
	phases := []struct {
		tagCount int
		name     string
	}{
		{10, "small"},
		{100, "medium"},
		{1000, "large"},
		{5000, "extreme"},
	}

	for _, phase := range phases {
		t.Run(phase.name, func(t *testing.T) {
			ctx, span := tracer.StartSpan(context.Background(), "tag-expansion")

			// Add many tags to trigger map growth
			for i := 0; i < phase.tagCount; i++ {
				key := fmt.Sprintf("tag_%04d", i)
				value := fmt.Sprintf("value_%04d_%s", i, strings.Repeat("x", 50))
				span.SetTag(key, value)
			}

			// Verify tags are accessible
			midKey := fmt.Sprintf("tag_%04d", phase.tagCount/2)
			if value, ok := span.GetTag(midKey); !ok {
				t.Errorf("Tag %s not found", midKey)
			} else if !strings.Contains(value, "value_") {
				t.Errorf("Tag %s has wrong value: %s", midKey, value)
			}

			span.Finish()
			_ = ctx // Satisfy linter

			// Verify span was collected with all tags
			exported := collector.Export()
			if len(exported) != 1 {
				t.Fatalf("Expected 1 span, got %d", len(exported))
			}

			if len(exported[0].Tags) != phase.tagCount {
				t.Errorf("Expected %d tags, got %d", phase.tagCount, len(exported[0].Tags))
			}
		})
	}
}

// testConcurrentTags verifies thread-safety under concurrent tag operations.
func testConcurrentTags(t *testing.T) {
	tracer := tracez.New("concurrent-tags-test")
	defer tracer.Close()

	ctx, span := tracer.StartSpan(context.Background(), "concurrent-tags")
	defer span.Finish()
	_ = ctx // Satisfy linter

	numGoroutines := runtime.NumCPU() * 2
	tagsPerGoroutine := 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines)

	// Launch concurrent tag setters
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < tagsPerGoroutine; j++ {
				key := fmt.Sprintf("goroutine_%d_tag_%d", goroutineID, j)
				value := fmt.Sprintf("value_%d_%d", goroutineID, j)
				span.SetTag(key, value)

				// Verify we can read what we wrote
				if readValue, ok := span.GetTag(key); !ok {
					errors <- fmt.Errorf("tag %s not found", key)
					return
				} else if readValue != value {
					errors <- fmt.Errorf("tag %s: expected %s, got %s", key, value, readValue)
					return
				}
			}
		}(i)
	}

	// Launch concurrent tag readers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			for j := 0; j < tagsPerGoroutine*2; j++ {
				// Read random existing tags
				goroutineID := j % numGoroutines
				tagID := j % tagsPerGoroutine
				key := fmt.Sprintf("goroutine_%d_tag_%d", goroutineID, tagID)

				// Tag might not exist yet, that's okay
				span.GetTag(key)
				time.Sleep(time.Microsecond * 10)
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for race condition errors
	for err := range errors {
		t.Error(err)
	}

	// Verify final tag count
	expectedTags := numGoroutines * tagsPerGoroutine
	actualTags := 0

	for i := 0; i < numGoroutines; i++ {
		for j := 0; j < tagsPerGoroutine; j++ {
			key := fmt.Sprintf("goroutine_%d_tag_%d", i, j)
			if _, ok := span.GetTag(key); ok {
				actualTags++
			}
		}
	}

	if actualTags != expectedTags {
		t.Errorf("Expected %d tags, found %d", expectedTags, actualTags)
	}
}

// testSpanCleanup verifies spans are properly cleaned up after finishing.
func testSpanCleanup(t *testing.T) {
	tracer := tracez.New("cleanup-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	initialMem := memStats.HeapInuse

	// Create and finish many spans
	numSpans := 1000
	for i := 0; i < numSpans; i++ {
		ctx, span := tracer.StartSpan(context.Background(), "cleanup-test")

		// Add some tags to increase memory usage
		for j := 0; j < 10; j++ {
			span.SetTag(fmt.Sprintf("tag_%d", j), fmt.Sprintf("value_%d_%d", i, j))
		}

		span.Finish()
		_ = ctx // Satisfy linter
	}

	// Force GC and measure memory
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	afterSpansMem := memStats.HeapInuse

	// Export spans to clear collector buffers
	exported := collector.Export()
	if len(exported) != numSpans {
		t.Errorf("Expected %d spans, got %d", numSpans, len(exported))
	}

	// Force another GC and measure
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	finalMem := memStats.HeapInuse

	t.Logf("Memory usage:")
	t.Logf("  Initial: %d bytes", initialMem)
	t.Logf("  After spans: %d bytes", afterSpansMem)
	t.Logf("  After cleanup: %d bytes", finalMem)

	// Memory should return close to initial levels
	memGrowth := float64(finalMem-initialMem) / float64(initialMem) * 100
	if memGrowth > 100 {
		t.Errorf("Excessive memory growth: %.1f%% growth", memGrowth)
	} else if memGrowth > 50 {
		t.Logf("Reliability Issue: High memory growth %.1f%% (potential memory pressure)", memGrowth)
	}
}

// testMassiveTagLoad - stress test with extreme tag volumes.
func testMassiveTagLoad(t *testing.T) {
	tracer := tracez.New("massive-tag-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	ctx, span := tracer.StartSpan(context.Background(), "massive-tags")
	defer span.Finish()
	_ = ctx // Satisfy linter

	// Add massive number of tags with large values
	numTags := 50000
	largeValue := strings.Repeat("x", 1000) // 1KB per tag value

	start := time.Now()

	for i := 0; i < numTags; i++ {
		key := fmt.Sprintf("massive_tag_%06d", i)
		value := fmt.Sprintf("%s_%06d", largeValue, i)
		span.SetTag(key, value)

		// Periodic verification to ensure system remains responsive
		if i%5000 == 0 {
			if _, ok := span.GetTag(key); !ok {
				t.Errorf("Tag %s not found during massive load", key)
			}
		}
	}

	duration := time.Since(start)

	// Verify performance didn't degrade catastrophically
	tagsPerSecond := float64(numTags) / duration.Seconds()
	if tagsPerSecond < 1000 {
		t.Errorf("Tag operations too slow: %.0f tags/sec", tagsPerSecond)
	}

	// Verify all tags are accessible
	midKey := fmt.Sprintf("massive_tag_%06d", numTags/2)
	if _, ok := span.GetTag(midKey); !ok {
		t.Errorf("Middle tag %s not found", midKey)
	}

	t.Logf("Massive tag load results:")
	t.Logf("  Tags: %d", numTags)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Rate: %.0f tags/sec", tagsPerSecond)
}

// testMemoryFragmentation - detect memory fragmentation issues.
func testMemoryFragmentation(t *testing.T) {
	tracer := tracez.New("fragmentation-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 10000)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	var memStats runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	initialMem := memStats.HeapInuse

	// Create spans with varying tag sizes to cause fragmentation
	numRounds := 100
	spansPerRound := 100

	for round := 0; round < numRounds; round++ {
		spans := make([]*tracez.ActiveSpan, spansPerRound)

		// Create spans with different tag patterns
		for i := 0; i < spansPerRound; i++ {
			ctx, span := tracer.StartSpan(context.Background(), "fragmentation-test")
			spans[i] = span

			// Varying tag sizes to create fragmentation
			tagCount := (i%10 + 1) * 10  // 10-100 tags
			valueSize := (i%5 + 1) * 100 // 100-500 char values

			for j := 0; j < tagCount; j++ {
				key := fmt.Sprintf("frag_%d_%d", i, j)
				value := strings.Repeat("f", valueSize)
				span.SetTag(key, value)
			}
			_ = ctx // Satisfy linter
		}

		// Finish half the spans
		for i := 0; i < spansPerRound/2; i++ {
			spans[i].Finish()
		}

		// Force GC to cleanup finished spans
		runtime.GC()

		// Finish remaining spans
		for i := spansPerRound / 2; i < spansPerRound; i++ {
			spans[i].Finish()
		}

		// Export to clear collector buffer
		collector.Export()
	}

	// Final memory check
	runtime.GC()
	runtime.ReadMemStats(&memStats)
	finalMem := memStats.HeapInuse

	fragmentation := float64(finalMem-initialMem) / float64(initialMem) * 100

	t.Logf("Fragmentation test results:")
	t.Logf("  Initial memory: %d bytes", initialMem)
	t.Logf("  Final memory: %d bytes", finalMem)
	t.Logf("  Growth: %.1f%%", fragmentation)

	// Should not have excessive fragmentation
	if fragmentation > 50 {
		t.Errorf("Excessive memory fragmentation: %.1f%%", fragmentation)
	}
}

// testGCPressure - verify system behavior under GC pressure.
func testGCPressure(t *testing.T) {
	tracer := tracez.New("gc-pressure-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	// Note: collector cleanup handled by tracer.Close() in production use
	tracer.AddCollector("test", collector)

	// Create memory pressure to trigger frequent GC
	duration := 10 * time.Second
	done := make(chan bool)

	var allocatedSpans int64
	var finishedSpans int64

	// Memory pressure generator
	go func() {
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()

		spans := make([]*tracez.ActiveSpan, 0, 100)

		for {
			select {
			case <-done:
				// Finish all remaining spans
				for _, span := range spans {
					span.Finish()
					finishedSpans++
				}
				return
			case <-ticker.C:
				// Create new spans
				for i := 0; i < 10; i++ {
					ctx, span := tracer.StartSpan(context.Background(), "gc-pressure")

					// Add tags to increase memory usage
					for j := 0; j < 20; j++ {
						span.SetTag(fmt.Sprintf("gc_tag_%d", j), fmt.Sprintf("value_%d", j))
					}

					spans = append(spans, span)
					allocatedSpans++
					_ = ctx // Satisfy linter
				}

				// Randomly finish some spans
				if len(spans) > 50 {
					toFinish := spans[:25]
					spans = spans[25:]

					for _, span := range toFinish {
						span.Finish()
						finishedSpans++
					}
				}
			}
		}
	}()

	// Monitor GC behavior
	var initialGC runtime.MemStats
	runtime.ReadMemStats(&initialGC)

	time.Sleep(duration)
	close(done)
	time.Sleep(100 * time.Millisecond) // Allow cleanup

	var finalGC runtime.MemStats
	runtime.ReadMemStats(&finalGC)

	gcRuns := finalGC.NumGC - initialGC.NumGC
	gcTime := finalGC.PauseTotalNs - initialGC.PauseTotalNs

	t.Logf("GC pressure test results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Allocated spans: %d", allocatedSpans)
	t.Logf("  Finished spans: %d", finishedSpans)
	t.Logf("  GC runs: %d", gcRuns)
	//nolint:gosec // Safe conversion - gcTime is from runtime.MemStats
	t.Logf("  GC time: %v", time.Duration(gcTime))

	// Verify system remained functional under GC pressure
	if finishedSpans < allocatedSpans/2 {
		t.Errorf("Too few spans finished under GC pressure: %d/%d", finishedSpans, allocatedSpans)
	}

	// System should still be responsive
	ctx, testSpan := tracer.StartSpan(context.Background(), "post-gc-test")
	testSpan.SetTag("test", "post-gc")
	testSpan.Finish()
	_ = ctx // Satisfy linter

	// Verify the test span was processed
	exported := collector.Export()
	found := false
	for i := range exported {
		span := exported[i]
		if span.Name == "post-gc-test" {
			found = true
			break
		}
	}

	if !found {
		t.Error("System not responsive after GC pressure test")
	}
}
