package tracez

import (
	"sync"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	collector := NewCollector("test-collector", 100)
	defer collector.close()

	// Name method removed - users track their own collector names.
	if collector == nil {
		t.Error("Expected collector to be created")
	}

	if collector.Count() != 0 {
		t.Errorf("Expected 0 spans initially, got %d", collector.Count())
	}

	if collector.DroppedCount() != 0 {
		t.Errorf("Expected 0 dropped spans initially, got %d", collector.DroppedCount())
	}
}

func TestCollectorBasicCollection(t *testing.T) {
	collector := NewCollector("test", 10)
	collector.SetSyncMode(true) // Enable sync for deterministic testing.
	defer collector.close()

	span := Span{
		SpanID:  "test-span-1",
		TraceID: "test-trace-1",
		Name:    "test-operation",
	}

	collector.Collect(&span)

	// No sleep needed - synchronous.
	if collector.Count() != 1 {
		t.Errorf("Expected 1 span, got %d", collector.Count())
	}

	spans := collector.Export()
	if len(spans) != 1 {
		t.Errorf("Expected 1 exported span, got %d", len(spans))
	}

	if spans[0].SpanID != "test-span-1" {
		t.Errorf("Expected span ID 'test-span-1', got %s", spans[0].SpanID)
	}

	// After export, collector should be empty.
	if collector.Count() != 0 {
		t.Errorf("Expected 0 spans after export, got %d", collector.Count())
	}
}

func TestCollectorBackpressure(t *testing.T) {
	// Small buffer to trigger backpressure quickly.
	collector := NewCollector("test", 2)
	defer collector.close()

	// Fill the channel beyond capacity.
	for i := 0; i < 10; i++ {
		span := Span{
			SpanID:  "test-span",
			TraceID: "test-trace",
			Name:    "test-operation",
		}
		collector.Collect(&span)
	}

	// Give time for async processing and dropping.
	time.Sleep(50 * time.Millisecond)

	droppedCount := collector.DroppedCount()
	if droppedCount == 0 {
		t.Error("Expected some spans to be dropped due to backpressure")
	}

	t.Logf("Dropped %d spans due to backpressure (expected behavior)", droppedCount)
}

func TestCollectorBufferGrowth(t *testing.T) {
	collector := NewCollector("test", 100)
	collector.SetSyncMode(true) // Enable sync for deterministic testing.
	defer collector.close()

	// Add many spans to trigger buffer growth.
	numSpans := 50
	for i := 0; i < numSpans; i++ {
		span := Span{
			SpanID:  "test-span",
			TraceID: "test-trace",
			Name:    "test-operation",
		}
		collector.Collect(&span)
	}

	// No sleep needed - synchronous.
	if collector.Count() != numSpans {
		t.Errorf("Expected %d spans, got %d", numSpans, collector.Count())
	}

	spans := collector.Export()
	if len(spans) != numSpans {
		t.Errorf("Expected %d exported spans, got %d", numSpans, len(spans))
	}
}

func TestCollectorMemoryShrink(t *testing.T) {
	collector := NewCollector("test", 1000)
	collector.SetSyncMode(true) // Enable sync for deterministic testing.
	defer collector.close()

	// Fill with many spans.
	numSpans := 100
	for i := 0; i < numSpans; i++ {
		span := Span{
			SpanID:  "test-span",
			TraceID: "test-trace",
			Name:    "test-operation",
		}
		collector.Collect(&span)
	}

	// No sleep needed - synchronous.
	// Export to trigger potential shrinking.
	spans := collector.Export()
	if len(spans) != numSpans {
		t.Errorf("Expected %d spans in export, got %d", numSpans, len(spans))
	}

	// Buffer should now be empty.
	if collector.Count() != 0 {
		t.Errorf("Expected 0 spans after export, got %d", collector.Count())
	}

	// Add a small number of spans.
	for i := 0; i < 5; i++ {
		span := Span{
			SpanID:  "small-span",
			TraceID: "small-trace",
			Name:    "small-operation",
		}
		collector.Collect(&span)
	}

	// No sleep needed - synchronous.
	if collector.Count() != 5 {
		t.Errorf("Expected 5 spans after small batch, got %d", collector.Count())
	}
}

func TestCollectorExportCopy(t *testing.T) {
	collector := NewCollector("test", 10)
	collector.SetSyncMode(true) // Enable sync for deterministic testing.
	defer collector.close()

	originalSpan := Span{
		SpanID:  "original",
		TraceID: "trace",
		Name:    "operation",
		Tags:    map[string]string{"key": "value"},
	}

	collector.Collect(&originalSpan)
	// No sleep needed - synchronous.

	exported := collector.Export()
	if len(exported) != 1 {
		t.Fatalf("Expected 1 exported span, got %d", len(exported))
	}

	// Modify the exported span.
	exported[0].Tags["key"] = "modified"
	exported[0].SpanID = "modified"

	// Add the same span again and export.
	collector.Collect(&originalSpan)
	// No sleep needed - synchronous.

	exported2 := collector.Export()
	if len(exported2) != 1 {
		t.Fatalf("Expected 1 exported span in second export, got %d", len(exported2))
	}

	// Original values should be preserved.
	if exported2[0].SpanID != "original" {
		t.Errorf("Expected SpanID 'original', got %s", exported2[0].SpanID)
	}

	if exported2[0].Tags["key"] != "value" {
		t.Errorf("Expected tag value 'value', got %s", exported2[0].Tags["key"])
	}
}

func TestCollectorReset(t *testing.T) {
	collector := NewCollector("test", 10)
	collector.SetSyncMode(true) // Enable sync for deterministic testing.
	defer collector.close()

	// Add some spans.
	for i := 0; i < 5; i++ {
		span := Span{SpanID: "span", TraceID: "trace", Name: "op"}
		collector.Collect(&span)
	}

	// No sleep needed - synchronous.
	if collector.Count() != 5 {
		t.Errorf("Expected 5 spans before reset, got %d", collector.Count())
	}

	// Set some dropped count.
	collector.droppedCount.Store(10)

	collector.Reset()

	if collector.Count() != 0 {
		t.Errorf("Expected 0 spans after reset, got %d", collector.Count())
	}

	if collector.DroppedCount() != 0 {
		t.Errorf("Expected 0 dropped count after reset, got %d", collector.DroppedCount())
	}
}

func TestCollectorShutdown(t *testing.T) {
	collector := NewCollector("test", 10)
	collector.SetSyncMode(true) // Enable sync for deterministic testing.

	// Add some spans.
	for i := 0; i < 3; i++ {
		span := Span{SpanID: "span", TraceID: "trace", Name: "op"}
		collector.Collect(&span)
	}

	// No sleep needed - synchronous.

	// Close should shut down gracefully.
	collector.close()

	// Should still be able to export what was collected.
	spans := collector.Export()
	if len(spans) != 3 {
		t.Errorf("Expected 3 spans after shutdown, got %d", len(spans))
	}

	// New spans should be dropped (channel closed).
	newSpan := Span{SpanID: "new", TraceID: "trace", Name: "op"}
	collector.Collect(&newSpan)

	// Should not increase count - sync mode bypasses channel.
	// No sleep needed - synchronous.
	if collector.Count() != 0 { // Already exported above.
		t.Errorf("Expected 0 spans after adding to closed collector, got %d", collector.Count())
	}
}

func TestCollectorConcurrentCollection(t *testing.T) {
	collector := NewCollector("test", 100)
	defer collector.close()

	var wg sync.WaitGroup
	numGoroutines := 50
	spansPerGoroutine := 10

	// Concurrent span collection.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < spansPerGoroutine; j++ {
				span := Span{
					SpanID:  "span",
					TraceID: "trace",
					Name:    "operation",
				}
				collector.Collect(&span)
			}
		}(i)
	}

	wg.Wait()

	// Give time for all spans to be processed by async goroutine.
	time.Sleep(100 * time.Millisecond)

	expectedTotal := numGoroutines * spansPerGoroutine
	actualCount := collector.Count()
	droppedCount := collector.DroppedCount()
	totalProcessed := int(droppedCount) + actualCount

	if totalProcessed != expectedTotal {
		t.Errorf("Expected %d total spans (collected + dropped), got %d (collected: %d, dropped: %d)",
			expectedTotal, totalProcessed, actualCount, droppedCount)
	}
}

func TestCollectorConcurrentExport(t *testing.T) {
	collector := NewCollector("test", 100)
	defer collector.close()

	// Add some initial spans.
	for i := 0; i < 20; i++ {
		span := Span{SpanID: "span", TraceID: "trace", Name: "op"}
		collector.Collect(&span)
	}

	// Give time for async processing.
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	var exportResults [][]Span
	var mu sync.Mutex

	// Concurrent exports.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result := collector.Export()

			mu.Lock()
			exportResults = append(exportResults, result)
			mu.Unlock()
		}()
	}

	wg.Wait()

	// Only one export should get the spans, others should be empty.
	var totalExported int
	var nonEmptyExports int

	for _, result := range exportResults {
		totalExported += len(result)
		if len(result) > 0 {
			nonEmptyExports++
		}
	}

	if nonEmptyExports > 1 {
		t.Errorf("Expected at most 1 non-empty export, got %d", nonEmptyExports)
	}

	if totalExported > 20 {
		t.Errorf("Expected at most 20 total exported spans, got %d", totalExported)
	}
}

func TestSetSyncMode(t *testing.T) {
	collector := NewCollector("test", 10)
	defer collector.close()

	// Test async mode (default).
	span1 := Span{SpanID: "async-span", TraceID: "trace", Name: "op"}
	collector.Collect(&span1)

	// Give time for async processing.
	time.Sleep(10 * time.Millisecond)

	if collector.Count() != 1 {
		t.Errorf("Expected 1 span in async mode, got %d", collector.Count())
	}

	// Clear the collector.
	collector.Export()

	// Enable sync mode.
	collector.SetSyncMode(true)

	// Test sync mode - no sleep needed.
	span2 := Span{SpanID: "sync-span", TraceID: "trace", Name: "op"}
	collector.Collect(&span2)

	// Should be immediately available.
	if collector.Count() != 1 {
		t.Errorf("Expected 1 span in sync mode (immediate), got %d", collector.Count())
	}

	// Verify span content.
	spans := collector.Export()
	if len(spans) != 1 {
		t.Fatalf("Expected 1 exported span, got %d", len(spans))
	}

	if spans[0].SpanID != "sync-span" {
		t.Errorf("Expected span ID 'sync-span', got %s", spans[0].SpanID)
	}

	// Test disabling sync mode.
	collector.SetSyncMode(false)

	// Back to async mode.
	span3 := Span{SpanID: "async-again", TraceID: "trace", Name: "op"}
	collector.Collect(&span3)

	// Give time for async processing.
	time.Sleep(10 * time.Millisecond)

	if collector.Count() != 1 {
		t.Errorf("Expected 1 span after disabling sync mode, got %d", collector.Count())
	}
}

// TestOptimizedCollectorBuffering tests improved buffer management.
func TestOptimizedCollectorBuffering(t *testing.T) {
	// Test with default buffer size (should be 1000 now).
	collector := NewCollector("optimized-test", 0)
	collector.SetSyncMode(true) // Enable sync mode for deterministic testing.
	defer collector.close()

	// Should start with optimized initial capacity (32).
	// We can't directly test this, but we can test growth behavior.

	// Generate many spans to test growth.
	spans := make([]Span, 100)
	for i := range spans {
		spans[i] = Span{
			SpanID:  "test-span",
			TraceID: "test-trace",
			Name:    "buffer-test",
		}
		collector.Collect(&spans[i])
	}

	// Wait for processing.
	time.Sleep(20 * time.Millisecond)

	// Should have collected all spans.
	count := collector.Count()
	if count != 100 {
		t.Errorf("Expected 100 spans collected, got %d", count)
	}

	// Export and verify shrinking doesn't happen too aggressively.
	exported := collector.Export()
	if len(exported) != 100 {
		t.Errorf("Expected 100 spans exported, got %d", len(exported))
	}

	// Add a few more spans.
	for i := 0; i < 5; i++ {
		span := Span{SpanID: "small-batch", TraceID: "test", Name: "small"}
		collector.Collect(&span)
	}

	// Should still work efficiently.
	if collector.Count() != 5 {
		t.Errorf("Expected 5 spans after export, got %d", collector.Count())
	}
}
