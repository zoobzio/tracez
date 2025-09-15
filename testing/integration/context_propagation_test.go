package integration

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// TestCrossGoroutineContextPropagation verifies parent-child relationships.
// across goroutine boundaries. Critical for distributed tracing.
func TestCrossGoroutineContextPropagation(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Start parent span.
	ctx, parentSpan := tracer.StartSpan(context.Background(), "parent-operation")
	parentTraceID := parentSpan.TraceID()
	parentSpanID := parentSpan.SpanID()

	// Track goroutines for leak detection.
	before := runtime.NumGoroutine()

	// Spawn multiple goroutines with child spans.
	var wg sync.WaitGroup
	childCount := 10

	for i := 0; i < childCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			// Create child span in goroutine.
			_, childSpan := tracer.StartSpan(ctx, "child-operation")
			childSpan.SetTag("goroutine.index", fmt.Sprintf("%d", idx))

			// Simulate work.
			time.Sleep(10 * time.Millisecond)

			childSpan.Finish()
		}(i)
	}

	wg.Wait()
	parentSpan.Finish()

	// Let collector process.
	time.Sleep(50 * time.Millisecond)

	// Verify no goroutine leak.
	after := runtime.NumGoroutine()
	if after > before {
		t.Errorf("Goroutine leak detected: %d -> %d", before, after)
	}

	// Export and verify spans.
	spans := collector.Export()

	// Should have parent + children.
	if len(spans) != childCount+1 {
		t.Fatalf("Expected %d spans, got %d", childCount+1, len(spans))
	}

	// Verify all spans share same TraceID.
	for _, span := range spans {
		if span.TraceID != parentTraceID {
			t.Errorf("Span %s has wrong TraceID: expected %s, got %s",
				span.Name, parentTraceID, span.TraceID)
		}
	}

	// Verify parent-child relationships.
	childrenFound := 0
	for _, span := range spans {
		if span.Name == "child-operation" {
			if span.ParentID != parentSpanID {
				t.Errorf("Child span has wrong ParentID: expected %s, got %s",
					parentSpanID, span.ParentID)
			}
			childrenFound++
		}
	}

	if childrenFound != childCount {
		t.Errorf("Expected %d child spans, found %d", childCount, childrenFound)
	}
}

// TestContextCancellationDuringTracing verifies graceful handling.
// when context is canceled mid-operation.
func TestContextCancellationDuringTracing(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create cancellable context.
	ctx, cancel := context.WithCancel(context.Background())

	// Start spans before cancellation.
	ctx, span1 := tracer.StartSpan(ctx, "operation-1")
	span1.SetTag("status", "started")

	ctx, span2 := tracer.StartSpan(ctx, "operation-2")
	span2.SetTag("status", "started")

	// Finish first span.
	span1.Finish()

	// Cancel context.
	cancel()

	// Try to create span after cancellation.
	_, span3 := tracer.StartSpan(ctx, "operation-3")
	if span3 != nil {
		span3.SetTag("status", "after-cancel")
		span3.Finish()
	}

	// Finish span started before cancellation.
	span2.Finish()

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Verify spans created before cancellation were collected.
	spans := collector.Export()

	foundSpan1 := false
	foundSpan2 := false
	foundSpan3 := false

	for _, span := range spans {
		switch span.Name {
		case "operation-1":
			foundSpan1 = true
			if span.Tags["status"] != "started" {
				t.Error("Span 1 status incorrect")
			}
		case "operation-2":
			foundSpan2 = true
			if span.Tags["status"] != "started" {
				t.Error("Span 2 status incorrect")
			}
		case "operation-3":
			foundSpan3 = true
		}
	}

	if !foundSpan1 {
		t.Error("Span created before cancellation not collected (span1)")
	}
	if !foundSpan2 {
		t.Error("Span created before cancellation not collected (span2)")
	}

	// Span 3 behavior depends on implementation.
	// It's OK if it was created or not, just no panic.
	t.Logf("Span after cancellation created: %v", foundSpan3)
}

// TestContextDeadlineExceeded verifies partial span collection.
// when deadline is exceeded during nested operations.
func TestContextDeadlineExceeded(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Short deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Start operation.
	ctx, rootSpan := tracer.StartSpan(ctx, "root-operation")

	// Create some nested spans quickly.
	ctx, span1 := tracer.StartSpan(ctx, "quick-op-1")
	span1.SetTag("completed", "true")
	span1.Finish()

	ctx, span2 := tracer.StartSpan(ctx, "quick-op-2")
	span2.SetTag("completed", "true")
	span2.Finish()

	// Wait for deadline.
	<-ctx.Done()

	// Try operations after deadline.
	_, span3 := tracer.StartSpan(ctx, "after-deadline")
	if span3 != nil {
		span3.SetTag("completed", "false")
		span3.Finish()
	}

	rootSpan.SetTag("deadline", "exceeded")
	rootSpan.Finish()

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Verify partial collection.
	spans := collector.Export()

	// Should have at least the spans created before deadline.
	if len(spans) < 2 {
		t.Errorf("Expected at least 2 spans, got %d", len(spans))
	}

	// Verify no corruption - all collected spans should be valid.
	for _, span := range spans {
		if span.TraceID == "" {
			t.Error("Span has empty TraceID")
		}
		if span.SpanID == "" {
			t.Error("Span has empty SpanID")
		}
		if span.Name == "" {
			t.Error("Span has empty Name")
		}
		if span.StartTime.IsZero() {
			t.Error("Span has zero StartTime")
		}
		if span.EndTime.IsZero() {
			t.Error("Span has zero EndTime")
		}
	}

	// Check that quick operations were collected.
	foundQuick1 := false
	foundQuick2 := false
	for _, span := range spans {
		if span.Name == "quick-op-1" {
			foundQuick1 = true
		}
		if span.Name == "quick-op-2" {
			foundQuick2 = true
		}
	}

	if !foundQuick1 || !foundQuick2 {
		t.Error("Quick operations completed before deadline not collected")
	}
}

// TestNestedContextPropagation verifies deep nesting maintains correct relationships.
func TestNestedContextPropagation(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create deep nested structure.
	ctx := context.Background()
	var spans []*tracez.ActiveSpan
	nestingDepth := 10

	// Create nested spans.
	for i := 0; i < nestingDepth; i++ {
		var span *tracez.ActiveSpan
		ctx, span = tracer.StartSpan(ctx, "level-operation")
		span.SetTag("level", fmt.Sprintf("%d", i))
		spans = append(spans, span)
	}

	// Finish in reverse order (innermost first).
	for i := len(spans) - 1; i >= 0; i-- {
		spans[i].Finish()
	}

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Export and verify.
	exported := collector.Export()

	if len(exported) != nestingDepth {
		t.Fatalf("Expected %d spans, got %d", nestingDepth, len(exported))
	}

	// All should share same TraceID.
	traceID := exported[0].TraceID
	for _, span := range exported {
		if span.TraceID != traceID {
			t.Error("TraceID not consistent across nested spans")
		}
	}

	// Build parent-child map.
	childToParent := make(map[string]string)
	spansByID := make(map[string]tracez.Span)

	for _, span := range exported {
		spansByID[span.SpanID] = span
		if span.ParentID != "" {
			childToParent[span.SpanID] = span.ParentID
		}
	}

	// Verify chain integrity.
	for childID, parentID := range childToParent {
		if _, exists := spansByID[parentID]; !exists && parentID != "" {
			t.Errorf("Child %s references non-existent parent %s", childID, parentID)
		}
	}
}

// TestContextValuePropagation verifies context values are preserved.
func TestContextValuePropagation(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Custom context key.
	type contextKey string
	const requestIDKey contextKey = "request-id"

	// Add value to context.
	ctx := context.WithValue(context.Background(), requestIDKey, "req-123")

	// Start span with context containing value.
	ctx, span1 := tracer.StartSpan(ctx, "operation-1")

	// Verify context value is preserved.
	if val := ctx.Value(requestIDKey); val != "req-123" {
		t.Error("Context value lost after StartSpan")
	}

	// Create nested span.
	ctx, span2 := tracer.StartSpan(ctx, "operation-2")

	// Value should still be there.
	if val := ctx.Value(requestIDKey); val != "req-123" {
		t.Error("Context value lost in nested span")
	}

	span2.Finish()
	span1.Finish()

	// Verify spans were collected properly.
	time.Sleep(50 * time.Millisecond)
	spans := collector.Export()

	if len(spans) != 2 {
		t.Errorf("Expected 2 spans, got %d", len(spans))
	}
}
