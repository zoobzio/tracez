package integration

import (
	"context"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// TestSpanBufferGrowth verifies memory management with large then small batches.
func TestSpanBufferGrowth(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 20000) // Large buffer.
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Phase 1: Generate many spans.
	largeBatch := 10000
	for i := 0; i < largeBatch; i++ {
		_, span := tracer.StartSpan(context.Background(), "large-batch")
		span.SetTag("phase", "1")
		span.SetTag("index", fmt.Sprintf("%d", i))

		// Add some data to make spans non-trivial.
		span.SetTag("data", fmt.Sprintf("some-data-%d", i))
		span.SetTag("timestamp", fmt.Sprintf("%d", time.Now().UnixNano()))

		span.Finish()
	}

	// Let collector process.
	time.Sleep(200 * time.Millisecond)

	// Export large batch.
	phase1Spans := collector.Export()
	phase1Count := len(phase1Spans)

	t.Logf("Phase 1: Generated %d, Exported %d spans", largeBatch, phase1Count)

	// Verify reasonable collection.
	if phase1Count < largeBatch/2 {
		t.Errorf("Too few spans collected: %d of %d", phase1Count, largeBatch)
	}

	// Force GC to reclaim memory.
	runtime.GC()

	// Phase 2: Generate small batch.
	smallBatch := 10
	for i := 0; i < smallBatch; i++ {
		_, span := tracer.StartSpan(context.Background(), "small-batch")
		span.SetTag("phase", "2")
		span.SetTag("index", fmt.Sprintf("%d", i))
		span.Finish()
	}

	// Let collector process.
	time.Sleep(50 * time.Millisecond)

	// Export small batch.
	phase2Spans := collector.Export()
	phase2Count := len(phase2Spans)

	t.Logf("Phase 2: Generated %d, Exported %d spans", smallBatch, phase2Count)

	// Should collect all from small batch.
	if phase2Count != smallBatch {
		t.Errorf("Phase 2 expected %d spans, got %d", smallBatch, phase2Count)
	}

	// Verify data integrity.
	for _, span := range phase2Spans {
		if span.Tags["phase"] != "2" {
			t.Error("Phase 2 span has wrong phase tag")
		}
		if _, hasIndex := span.Tags["index"]; !hasIndex {
			t.Error("Phase 2 span missing index")
		}
	}
}

// TestTagMapDeepCopy verifies exported spans are immutable.
func TestTagMapDeepCopy(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create span with tags.
	_, span := tracer.StartSpan(context.Background(), "test-span")

	// Original tags.
	originalTags := map[string]string{
		"string": "value",
		"number": "42",
		"float":  "3.14",
		"bool":   "true",
		"slice":  "[a,b,c]",
		"map":    "{x:1,y:2}",
	}

	for k, v := range originalTags {
		span.SetTag(k, v)
	}

	span.Finish()

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Export spans.
	export1 := collector.Export()
	if len(export1) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(export1))
	}

	// Keep reference to first export.
	firstExport := export1[0]
	firstTags := firstExport.Tags

	// Modify the span again (though it's finished, test defensive copying).
	span.SetTag("string", "modified")
	span.SetTag("new-tag", "should-not-appear")

	// Create new span to ensure collector still works.
	_, span2 := tracer.StartSpan(context.Background(), "second-span")
	span2.SetTag("separate", "true")
	span2.Finish()

	time.Sleep(50 * time.Millisecond)

	// Export again.
	_ = collector.Export()

	// First export's tags should be unchanged.
	if firstTags["string"] != "value" {
		t.Error("First export's tags were modified")
	}

	if _, exists := firstTags["new-tag"]; exists {
		t.Error("First export got new tag after export")
	}

	// Verify deep copy by checking complex types.
	if slice := firstTags["slice"]; slice != "[a,b,c]" {
		t.Error("Slice data corrupted")
	}

	if mapData := firstTags["map"]; mapData != "{x:1,y:2}" {
		t.Error("Map data corrupted")
	}
}

// TestNilContextHandling verifies graceful handling of nil context.
func TestNilContextHandling(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Try with nil context.
	var nilCtx context.Context

	// Should not panic.
	ctx, span := tracer.StartSpan(nilCtx, "nil-context-span")

	// Should get valid span.
	if span == nil {
		t.Fatal("Got nil span from nil context")
	}

	// Should have valid IDs.
	if span.TraceID() == "" {
		t.Error("Span from nil context has empty TraceID")
	}
	if span.SpanID() == "" {
		t.Error("Span from nil context has empty SpanID")
	}

	// Should be able to set tags.
	span.SetTag("created-from", "nil-context")

	// Should be able to create child.
	_, childSpan := tracer.StartSpan(ctx, "child-of-nil-context")
	if childSpan == nil {
		t.Fatal("Got nil child span")
	}

	// Child should be properly linked.
	if childSpan.TraceID() != span.TraceID() {
		t.Error("Child has different TraceID")
	}

	childSpan.Finish()
	span.Finish()

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Verify spans collected.
	spans := collector.Export()
	if len(spans) != 2 {
		t.Fatalf("Expected 2 spans, got %d", len(spans))
	}

	// Verify parent-child relationship.
	var parent, child tracez.Span
	for _, s := range spans {
		if s.Name == "nil-context-span" {
			parent = s
		} else if s.Name == "child-of-nil-context" {
			child = s
		}
	}

	if child.ParentID != parent.SpanID {
		t.Error("Child not properly linked to parent created from nil context")
	}
}

// TestMemoryPressureGracefulDegradation simulates high memory usage.
func TestMemoryPressureGracefulDegradation(t *testing.T) {
	// Skip in short mode as this test is intensive.
	if testing.Short() {
		t.Skip("Skipping memory pressure test in short mode")
	}

	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Allocate large amount of memory to simulate pressure.
	// This is just for testing - allocate 100MB.
	largeData := make([][]byte, 100)
	for i := range largeData {
		largeData[i] = make([]byte, 1024*1024) // 1MB each.
	}

	// Force GC to recognize memory pressure.
	runtime.GC()

	// Now try to create many spans under memory pressure.
	generated := 0
	dropped := 0

	for i := 0; i < 1000; i++ {
		_, span := tracer.StartSpan(context.Background(), "memory-pressure-span")

		if span != nil {
			span.SetTag("index", fmt.Sprintf("%d", i))
			span.SetTag("memory-test", "true")

			// Add some data to increase memory usage.
			span.SetTag("data", fmt.Sprintf("test-data-%d", i))

			span.Finish()
			generated++
		} else {
			dropped++
		}
	}

	// Let collector process.
	time.Sleep(100 * time.Millisecond)

	// Check results.
	collectedSpans := collector.Export()
	droppedByCollector := collector.DroppedCount()

	t.Logf("Under memory pressure: generated=%d, collected=%d, dropped=%d, collector-dropped=%d",
		generated, len(collectedSpans), dropped, droppedByCollector)

	// System should handle gracefully - either by dropping or collecting.
	// but not crashing or hanging.
	if generated == 0 {
		t.Error("No spans could be generated under memory pressure")
	}

	// Free memory.
	runtime.KeepAlive(largeData) // Prevent optimizing away the allocation
	largeData = nil              //nolint:ineffassign,wastedassign // Required for memory pressure test
	runtime.GC()

	// Verify system recovers after memory pressure relieved.
	_, recoverySpan := tracer.StartSpan(context.Background(), "recovery-span")
	if recoverySpan == nil {
		t.Error("Cannot create spans after memory pressure relieved")
	}
	recoverySpan.SetTag("post-pressure", "true")
	recoverySpan.Finish()

	time.Sleep(50 * time.Millisecond)

	// Should be able to collect the recovery span.
	finalSpans := collector.Export()
	foundRecovery := false
	for _, span := range finalSpans {
		if span.Name == "recovery-span" {
			foundRecovery = true
			break
		}
	}

	if !foundRecovery {
		t.Error("Recovery span not collected after memory pressure relieved")
	}
}

// TestTagCardinalityExplosion tests behavior with excessive tag keys.
func TestTagCardinalityExplosion(t *testing.T) {
	// Skip in short mode as this test is intensive.
	if testing.Short() {
		t.Skip("Skipping tag cardinality test in short mode")
	}

	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create span with huge number of unique tags.
	_, span := tracer.StartSpan(context.Background(), "high-cardinality-span")

	// Add 10,000 unique tags.
	tagCount := 10000
	for i := 0; i < tagCount; i++ {
		key := fmt.Sprintf("tag-%d", i)
		value := fmt.Sprintf("value-%d", i)
		span.SetTag(key, value)
	}

	// Measure memory before finishing.
	var memStatsBefore runtime.MemStats
	runtime.ReadMemStats(&memStatsBefore)

	span.Finish()

	// Let collector process.
	time.Sleep(100 * time.Millisecond)

	// Measure memory after.
	var memStatsAfter runtime.MemStats
	runtime.ReadMemStats(&memStatsAfter)

	// Calculate memory growth.
	memGrowth := memStatsAfter.Alloc - memStatsBefore.Alloc
	t.Logf("Memory growth with %d tags: %d bytes", tagCount, memGrowth)

	// Export and verify.
	spans := collector.Export()

	if len(spans) != 1 {
		t.Fatalf("Expected 1 span, got %d", len(spans))
	}

	exportedSpan := spans[0]

	// Check that tags were preserved (or reasonably handled).
	t.Logf("Exported span has %d tags", len(exportedSpan.Tags))

	// System should handle this without crashing.
	// Exact behavior (all tags kept vs some dropped) is implementation-specific.
	if len(exportedSpan.Tags) == 0 {
		t.Error("No tags preserved in high-cardinality span")
	}

	// Verify system still functional.
	_, testSpan := tracer.StartSpan(context.Background(), "post-cardinality-test")
	testSpan.SetTag("normal", "true")
	testSpan.Finish()

	time.Sleep(50 * time.Millisecond)

	postTestSpans := collector.Export()
	if len(postTestSpans) != 1 {
		t.Error("System not functional after high cardinality test")
	}
}

// TestPanicRecovery verifies system recovers from panics in user code.
func TestPanicRecovery(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Function that panics during span creation.
	panickyOperation := func(ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				t.Logf("Recovered from panic: %v", r)
			}
		}()

		_, span := tracer.StartSpan(ctx, "panicky-span")
		span.SetTag("before-panic", "true")

		// Simulate panic in user code.
		panic("simulated panic")

		// This won't execute.
		span.SetTag("after-panic", "true")
		span.Finish()
	}

	// Run panicky operation.
	ctx := context.Background()
	panickyOperation(ctx)

	// System should still be functional.
	_, normalSpan := tracer.StartSpan(context.Background(), "normal-span")
	normalSpan.SetTag("post-panic", "true")
	normalSpan.Finish()

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Check collected spans.
	spans := collector.Export()

	// Should have the normal span at least.
	foundNormal := false
	for _, span := range spans {
		if span.Name == "normal-span" {
			foundNormal = true
			if span.Tags["post-panic"] != "true" {
				t.Error("Normal span missing expected tag")
			}
		}
	}

	if !foundNormal {
		t.Error("System not functional after panic recovery")
	}

	// The panicky span might or might not be collected.
	// depending on when the panic occurred.
	for _, span := range spans {
		if span.Name == "panicky-span" {
			t.Log("Panicky span was collected despite panic")
			// This is OK - just means span was sent before panic.
		}
	}
}

// TestCollectorResetCleanup verifies Reset() properly cleans up resources.
func TestCollectorResetCleanup(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Generate initial spans.
	for i := 0; i < 100; i++ {
		_, span := tracer.StartSpan(context.Background(), "pre-reset")
		span.SetTag("batch", "first")
		span.SetTag("index", fmt.Sprintf("%d", i))
		span.Finish()
	}

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Check initial state.
	initialSpans := collector.Export()
	initialCount := len(initialSpans)
	initialDropped := collector.DroppedCount()

	t.Logf("Before reset: %d spans, %d dropped", initialCount, initialDropped)

	// Reset.
	collector.Reset()

	// Immediately check state.
	afterResetSpans := collector.Export()
	afterResetDropped := collector.DroppedCount()

	if len(afterResetSpans) != 0 {
		t.Errorf("Spans not cleared after reset: %d remaining", len(afterResetSpans))
	}

	if afterResetDropped != 0 {
		t.Errorf("Dropped count not reset: %d", afterResetDropped)
	}

	// Generate new spans.
	for i := 0; i < 50; i++ {
		_, span := tracer.StartSpan(context.Background(), "post-reset")
		span.SetTag("batch", "second")
		span.SetTag("index", fmt.Sprintf("%d", i))
		span.Finish()
	}

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Check new spans collected properly.
	newSpans := collector.Export()

	if len(newSpans) == 0 {
		t.Error("No spans collected after reset")
	}

	// All new spans should be from second batch.
	for _, span := range newSpans {
		if span.Tags["batch"] != "second" {
			t.Error("Found old span after reset")
		}
	}

	t.Logf("After reset and new generation: %d spans", len(newSpans))
}
