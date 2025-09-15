package reliability

import (
	"context"
	"fmt"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// Collector saturation tests - verify collector remains stable under extreme span ingestion
// Environment: TRACEZ_RELIABILITY_LEVEL controls test intensity
//   basic: CI-safe collector validation
//   stress: Production-level collector stress testing

func TestCollectorSaturation(t *testing.T) {
	config := getReliabilityConfig()
	
	switch config.Level {
	case "basic":
		t.Run("basic_backpressure", testBasicBackpressure)
		t.Run("buffer_growth", testBufferGrowth)
		t.Run("export_under_load", testExportUnderLoad)
	case "stress":
		t.Run("extreme_ingestion", testExtremeIngestion)
		t.Run("sustained_pressure", testSustainedPressure)
		t.Run("cascade_saturation", testCascadeSaturation)
	default:
		t.Skip("TRACEZ_RELIABILITY_LEVEL not set, skipping reliability tests")
	}
}

// testBasicBackpressure verifies collector drops spans when channel is full
func testBasicBackpressure(t *testing.T) {
	const bufferSize = 10
	collector := tracez.NewCollector("test", bufferSize)
	// Note: collector cleanup handled by tracer.Close() in production use
	
	// Fill the channel beyond capacity
	spans := make([]*tracez.Span, bufferSize*2)
	for i := range spans {
		spans[i] = &tracez.Span{
			TraceID:   fmt.Sprintf("trace-%d", i),
			SpanID:    fmt.Sprintf("span-%d", i),
			Name:      "test",
			StartTime: time.Now(),
		}
	}
	
	// Submit all spans rapidly to trigger backpressure
	for _, span := range spans {
		collector.Collect(span)
	}
	
	// Allow time for processing
	time.Sleep(10 * time.Millisecond)
	
	// Verify some spans were dropped
	if collector.DroppedCount() == 0 {
		t.Error("Expected some spans to be dropped due to backpressure")
	}
	
	// Verify collector continues operating
	testSpan := &tracez.Span{
		TraceID:   "recovery-trace",
		SpanID:    "recovery-span", 
		Name:      "recovery",
		StartTime: time.Now(),
	}
	collector.Collect(testSpan)
	
	// System should recover and accept new spans
	exported := collector.Export()
	if len(exported) == 0 {
		t.Error("Collector should continue operating after backpressure")
	}
}

// testBufferGrowth verifies buffer expansion under graduated load
func testBufferGrowth(t *testing.T) {
	collector := tracez.NewCollector("buffer-test", 1000)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true) // Deterministic testing
	
	// Track buffer behavior through growth phases
	phases := []struct {
		spanCount int
		name      string
	}{
		{32, "initial"},
		{128, "first_growth"},
		{512, "moderate_growth"},
		{2048, "large_growth"},
	}
	
	for _, phase := range phases {
		t.Run(phase.name, func(t *testing.T) {
			// Submit spans for this phase
			for i := 0; i < phase.spanCount; i++ {
				span := &tracez.Span{
					TraceID:   fmt.Sprintf("growth-%s-%d", phase.name, i),
					SpanID:    fmt.Sprintf("span-%d", i),
					Name:      "growth-test",
					StartTime: time.Now(),
				}
				collector.Collect(span)
			}
			
			// Verify collector handles the load
			count := collector.Count()
			if count != phase.spanCount {
				t.Errorf("Expected %d spans, got %d", phase.spanCount, count)
			}
			
			// Export to reset for next phase
			exported := collector.Export()
			if len(exported) != phase.spanCount {
				t.Errorf("Export returned %d spans, expected %d", len(exported), phase.spanCount)
			}
		})
	}
}

// testExportUnderLoad verifies export operations don't interfere with collection
func testExportUnderLoad(t *testing.T) {
	collector := tracez.NewCollector("export-test", 100)
	// Note: collector cleanup handled by tracer.Close() in production use
	
	var wg sync.WaitGroup
	var exportCount atomic.Int64
	var spanCount atomic.Int64
	
	// Start continuous span submission
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			span := &tracez.Span{
				TraceID:   fmt.Sprintf("export-trace-%d", i),
				SpanID:    fmt.Sprintf("span-%d", i),
				Name:      "export-test",
				StartTime: time.Now(),
			}
			collector.Collect(span)
			spanCount.Add(1)
			time.Sleep(time.Microsecond * 100)
		}
	}()
	
	// Start continuous exports
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			exported := collector.Export()
			exportCount.Add(int64(len(exported)))
			time.Sleep(time.Millisecond * 5)
		}
	}()
	
	wg.Wait()
	
	// Final export to get remaining spans
	final := collector.Export()
	exportCount.Add(int64(len(final)))
	
	// Account for dropped spans
	totalProcessed := exportCount.Load() + collector.DroppedCount()
	
	// Verify most spans were processed
	if totalProcessed < spanCount.Load()/2 {
		t.Errorf("Too many spans lost: submitted %d, processed %d", spanCount.Load(), totalProcessed)
	}
}

// testExtremeIngestion - stress test with high concurrent span volume
func testExtremeIngestion(t *testing.T) {
	collector := tracez.NewCollector("extreme-test", 10000)
	// Note: collector cleanup handled by tracer.Close() in production use
	
	numGoroutines := runtime.NumCPU() * 4
	spansPerGoroutine := 5000
	
	var wg sync.WaitGroup
	start := time.Now()
	
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < spansPerGoroutine; j++ {
				span := &tracez.Span{
					TraceID:   fmt.Sprintf("extreme-%d-%d", goroutineID, j),
					SpanID:    fmt.Sprintf("span-%d-%d", goroutineID, j),
					Name:      "extreme-ingestion",
					StartTime: time.Now(),
				}
				collector.Collect(span)
			}
		}(i)
	}
	
	wg.Wait()
	duration := time.Since(start)
	
	// Calculate metrics
	totalSpans := int64(numGoroutines * spansPerGoroutine)
	processed := int64(collector.Count()) + collector.DroppedCount()
	throughput := float64(processed) / duration.Seconds()
	
	t.Logf("Extreme ingestion results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Total spans: %d", totalSpans)
	t.Logf("  Processed: %d", processed)
	t.Logf("  Dropped: %d", collector.DroppedCount())
	t.Logf("  Throughput: %.0f spans/sec", throughput)
	
	// Verify system didn't collapse
	if processed < totalSpans/10 {
		t.Errorf("System processed too few spans: %d/%d", processed, totalSpans)
	}
}

// testSustainedPressure - long-running stress to detect memory leaks
func testSustainedPressure(t *testing.T) {
	collector := tracez.NewCollector("sustained-test", 1000)
	// Note: collector cleanup handled by tracer.Close() in production use
	
	duration := 30 * time.Second
	exportInterval := 100 * time.Millisecond
	
	var totalExported atomic.Int64
	var memStats runtime.MemStats
	runtime.ReadMemStats(&memStats)
	initialMem := memStats.HeapInuse
	
	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()
	
	var wg sync.WaitGroup
	
	// Span generation
	wg.Add(1)
	go func() {
		defer wg.Done()
		counter := 0
		ticker := time.NewTicker(time.Microsecond * 500)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				span := &tracez.Span{
					TraceID:   fmt.Sprintf("sustained-%d", counter),
					SpanID:    fmt.Sprintf("span-%d", counter),
					Name:      "sustained-pressure",
					StartTime: time.Now(),
				}
				collector.Collect(span)
				counter++
			}
		}
	}()
	
	// Regular exports
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(exportInterval)
		defer ticker.Stop()
		
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				exported := collector.Export()
				totalExported.Add(int64(len(exported)))
			}
		}
	}()
	
	wg.Wait()
	
	// Final metrics
	runtime.ReadMemStats(&memStats)
	finalMem := memStats.HeapInuse
	memGrowth := float64(finalMem-initialMem) / float64(initialMem) * 100
	
	t.Logf("Sustained pressure results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Total exported: %d", totalExported.Load())
	t.Logf("  Final dropped: %d", collector.DroppedCount())
	t.Logf("  Memory growth: %.1f%%", memGrowth)
	
	// Verify no excessive memory growth (allow 50% growth)
	if memGrowth > 50 {
		t.Errorf("Excessive memory growth: %.1f%%", memGrowth)
	}
}

// testCascadeSaturation - multiple collectors under coordinated stress
func testCascadeSaturation(t *testing.T) {
	numCollectors := 5
	collectors := make([]*tracez.Collector, numCollectors)
	
	for i := 0; i < numCollectors; i++ {
		collectors[i] = tracez.NewCollector(fmt.Sprintf("cascade-%d", i), 500)
		// Note: collector cleanup handled by tracer.Close() in production use
	}
	
	tracer := tracez.New("cascade-test")
	defer tracer.Close()
	
	// Register all collectors
	for i, collector := range collectors {
		tracer.AddCollector(fmt.Sprintf("collector-%d", i), collector)
	}
	
	// Generate spans that will be distributed to all collectors
	numSpans := 10000
	var wg sync.WaitGroup
	
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numSpans; i++ {
			ctx, span := tracer.StartSpan(context.Background(), "cascade-operation")
			span.SetTag("iteration", strconv.Itoa(i))
			span.Finish()
			_ = ctx // Satisfy linter
		}
	}()
	
	wg.Wait()
	
	// Allow processing time
	time.Sleep(100 * time.Millisecond)
	
	// Verify all collectors received spans
	totalCollected := 0
	totalDropped := int64(0)
	
	for i, collector := range collectors {
		count := collector.Count()
		dropped := collector.DroppedCount()
		totalCollected += count
		totalDropped += dropped
		
		t.Logf("Collector %d: %d collected, %d dropped", i, count, dropped)
		
		if count == 0 && dropped == 0 {
			t.Errorf("Collector %d received no spans", i)
		}
	}
	
	t.Logf("Cascade totals: %d collected, %d dropped", totalCollected, totalDropped)
	
	// Verify system handled the cascade load
	expectedTotal := int64(numSpans * numCollectors)
	actualTotal := int64(totalCollected) + totalDropped
	
	if actualTotal < expectedTotal/2 {
		t.Errorf("Too much data lost in cascade: expected ~%d, got %d", expectedTotal, actualTotal)
	}
}