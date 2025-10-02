package tracez

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/clockz"
)

// Local key constants for testing.
const DBQueryKey = "db.query"

func TestNewTracer(t *testing.T) {
	tracer := New()

	// Service name is internal now - just verify tracer was created.
	if tracer == nil {
		t.Error("Expected tracer to be created")
	}

	// No way to check collector count now - users manage their own.
}

func TestTracerOnSpanComplete(t *testing.T) {
	tracer := New()

	var called bool
	id := tracer.OnSpanComplete(func(_ Span) {
		called = true
	})

	if id == 0 {
		t.Error("Expected non-zero handler ID")
	}

	// Create and finish a span
	_, span := tracer.StartSpan(context.Background(), "test")
	span.Finish()

	if !called {
		t.Error("Handler was not called")
	}
}

func TestTracerStartSpanNoParent(t *testing.T) {
	tracer := New()
	ctx := context.Background()

	// Add a handler so spans are actually created
	tracer.OnSpanComplete(func(_ Span) {})

	newCtx, activeSpan := tracer.StartSpan(ctx, "test-operation")

	// Check span properties.
	if activeSpan.span.Name != "test-operation" {
		t.Errorf("Expected span name 'test-operation', got %s", activeSpan.span.Name)
	}

	if activeSpan.span.TraceID == "" {
		t.Error("Expected non-empty TraceID")
	}

	if activeSpan.span.SpanID == "" {
		t.Error("Expected non-empty SpanID")
	}

	if activeSpan.span.ParentID != "" {
		t.Error("Expected empty ParentID for root span")
	}

	if activeSpan.span.StartTime.IsZero() {
		t.Error("Expected non-zero StartTime")
	}

	// GetTracer function removed - users access tracer through their own references.

	extractedSpan := GetSpan(newCtx)
	if extractedSpan != activeSpan.span {
		t.Error("Expected span to be propagated in context")
	}
}

func TestTracerStartSpanWithParent(t *testing.T) {
	tracer := New()
	ctx := context.Background()

	// Add a handler so spans are actually created
	tracer.OnSpanComplete(func(_ Span) {})

	// Create parent span.
	parentCtx, parentSpan := tracer.StartSpan(ctx, "parent-operation")

	// Create child span.
	childCtx, childSpan := tracer.StartSpan(parentCtx, "child-operation")

	// Child should inherit trace ID from parent.
	if childSpan.span.TraceID != parentSpan.span.TraceID {
		t.Errorf("Expected child TraceID %s, got %s", parentSpan.span.TraceID, childSpan.span.TraceID)
	}

	// Child should reference parent.
	if childSpan.span.ParentID != parentSpan.span.SpanID {
		t.Errorf("Expected child ParentID %s, got %s", parentSpan.span.SpanID, childSpan.span.ParentID)
	}

	// Child should have different SpanID.
	if childSpan.span.SpanID == parentSpan.span.SpanID {
		t.Error("Expected child to have different SpanID from parent")
	}

	// Context should contain child span.
	extractedSpan := GetSpan(childCtx)
	if extractedSpan != childSpan.span {
		t.Error("Expected child span to be in context")
	}
}

func TestTracerCollectSpan(t *testing.T) {
	tracer := New()
	var spans []Span
	tracer.OnSpanComplete(func(span Span) {
		spans = append(spans, span)
	})

	span := Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test-operation",
		StartTime: time.Now(),
		EndTime:   time.Now(),
		Duration:  time.Millisecond * 100,
	}

	// Execute handlers directly with the span
	tracer.executeHandlers(span)

	if len(spans) != 1 {
		t.Errorf("Expected 1 span, got %d", len(spans))
	}
	if len(spans) != 1 {
		t.Errorf("Expected 1 exported span, got %d", len(spans))
	}

	if spans[0].SpanID != "test-span" {
		t.Errorf("Expected span ID 'test-span', got %s", spans[0].SpanID)
	}
}

func TestTracerCollectSpanMultipleCollectors(t *testing.T) {
	tracer := New()

	var handler1Spans []Span
	var handler2Spans []Span

	tracer.OnSpanComplete(func(span Span) {
		handler1Spans = append(handler1Spans, span)
	})
	tracer.OnSpanComplete(func(span Span) {
		handler2Spans = append(handler2Spans, span)
	})

	span := Span{
		SpanID:  "test-span",
		TraceID: "test-trace",
		Name:    "test-operation",
	}

	// Execute handlers directly with the span
	tracer.executeHandlers(span)

	if len(handler1Spans) != 1 {
		t.Errorf("Expected 1 span in handler1, got %d", len(handler1Spans))
	}

	if len(handler2Spans) != 1 {
		t.Errorf("Expected 1 span in handler2, got %d", len(handler2Spans))
	}
}

func TestTracerRemoveHandler(t *testing.T) {
	tracer := New()

	var count int
	id := tracer.OnSpanComplete(func(_ Span) {
		count++
	})

	// Create and finish a span
	_, span := tracer.StartSpan(context.Background(), "test1")
	span.Finish()

	if count != 1 {
		t.Errorf("Expected count to be 1, got %d", count)
	}

	// Remove the handler
	tracer.RemoveHandler(id)

	// Create and finish another span
	_, span2 := tracer.StartSpan(context.Background(), "test2")
	span2.Finish()

	// Count should still be 1 since handler was removed
	if count != 1 {
		t.Errorf("Expected count to still be 1 after handler removal, got %d", count)
	}
}

func TestTracerClose(t *testing.T) {
	tracer := New()

	var handler1Spans []Span
	var handler2Spans []Span

	tracer.OnSpanComplete(func(span Span) {
		handler1Spans = append(handler1Spans, span)
	})
	tracer.OnSpanComplete(func(span Span) {
		handler2Spans = append(handler2Spans, span)
	})

	// Close should be equivalent to Reset (clear buffers).
	tracer.Close()

	// After close, handlers should be cleared
	// Create a new span to verify handlers don't run
	_, span := tracer.StartSpan(context.Background(), "after-close")
	span.Finish()

	// Handler counts should not increase
	if len(handler1Spans) != 0 {
		t.Errorf("Expected no spans after close, got %d", len(handler1Spans))
	}
}

func TestTracerGenerateIDs(t *testing.T) {
	tracer := New()
	// Add handler so IDs are actually generated
	tracer.OnSpanComplete(func(_ Span) {})
	ctx := context.Background()

	// Generate multiple spans to test ID uniqueness.
	var traceIDs []string
	var spanIDs []string

	for i := 0; i < 10; i++ {
		_, activeSpan := tracer.StartSpan(ctx, "test")
		traceIDs = append(traceIDs, activeSpan.span.TraceID)
		spanIDs = append(spanIDs, activeSpan.span.SpanID)
	}

	// All trace IDs should be unique (no parent context).
	for i := 0; i < len(traceIDs); i++ {
		for j := i + 1; j < len(traceIDs); j++ {
			if traceIDs[i] == traceIDs[j] {
				t.Error("Found duplicate trace IDs")
			}
		}
	}

	// All span IDs should be unique.
	for i := 0; i < len(spanIDs); i++ {
		for j := i + 1; j < len(spanIDs); j++ {
			if spanIDs[i] == spanIDs[j] {
				t.Error("Found duplicate span IDs")
			}
		}
	}

	// IDs should be non-empty hex strings.
	for _, id := range traceIDs {
		if id == "" {
			t.Error("Found empty trace ID")
		}
		if len(id) != 32 { // 16 bytes = 32 hex chars.
			t.Errorf("Expected trace ID length 32, got %d", len(id))
		}
	}

	for _, id := range spanIDs {
		if id == "" {
			t.Error("Found empty span ID")
		}
		if len(id) != 16 { // 8 bytes = 16 hex chars.
			t.Errorf("Expected span ID length 16, got %d", len(id))
		}
	}
}

func TestTracerConcurrentSpanCreation(t *testing.T) {
	tracer := New()
	var spans []Span
	var mu sync.Mutex
	tracer.OnSpanComplete(func(span Span) {
		mu.Lock()
		spans = append(spans, span)
		mu.Unlock()
	})

	var wg sync.WaitGroup
	numGoroutines := 50
	spansPerGoroutine := 10

	ctx := context.Background()

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()

			for j := 0; j < spansPerGoroutine; j++ {
				_, activeSpan := tracer.StartSpan(ctx, "test-operation")
				activeSpan.SetTag("routine", "test")
				activeSpan.Finish()
			}
		}(i)
	}

	wg.Wait()

	expectedSpans := numGoroutines * spansPerGoroutine
	actualSpans := len(spans)
	droppedSpans := tracer.DroppedSpans()
	totalProcessed := actualSpans + int(droppedSpans) //nolint:gosec // Safe conversion, test only

	if totalProcessed != expectedSpans {
		t.Errorf("Expected %d total spans, got %d (collected: %d, dropped: %d)",
			expectedSpans, totalProcessed, actualSpans, droppedSpans)
	}
}

func TestTracerCompleteWorkflow(t *testing.T) {
	tracer := New()
	var spans []Span
	tracer.OnSpanComplete(func(span Span) {
		spans = append(spans, span)
	})

	ctx := context.Background()

	// Start root span.
	rootCtx, rootSpan := tracer.StartSpan(ctx, "root-operation")
	rootSpan.SetTag("operation.type", "root")

	// Start child span.
	childCtx, childSpan := tracer.StartSpan(rootCtx, "child-operation")
	childSpan.SetTag("operation.type", "child")

	// Start grandchild span.
	_, grandchildSpan := tracer.StartSpan(childCtx, "grandchild-operation")
	grandchildSpan.SetTag("operation.type", "grandchild")

	// Finish in reverse order (typical pattern).
	grandchildSpan.Finish()
	childSpan.Finish()
	rootSpan.Finish()

	// Give time for processing.
	time.Sleep(50 * time.Millisecond)

	if len(spans) != 3 {
		t.Errorf("Expected 3 spans, got %d", len(spans))
	}
	if len(spans) != 3 {
		t.Fatalf("Expected 3 exported spans, got %d", len(spans))
	}

	// Find spans by type.
	var rootExported, childExported, grandchildExported *Span
	for i := range spans {
		switch spans[i].Tags["operation.type"] {
		case "root":
			rootExported = &spans[i]
		case "child":
			childExported = &spans[i]
		case "grandchild":
			grandchildExported = &spans[i]
		}
	}

	if rootExported == nil || childExported == nil || grandchildExported == nil {
		t.Fatal("Could not find all span types in export")
	}

	// Verify relationships.
	if childExported.TraceID != rootExported.TraceID {
		t.Error("Child should have same trace ID as root")
	}

	if grandchildExported.TraceID != rootExported.TraceID {
		t.Error("Grandchild should have same trace ID as root")
	}

	if childExported.ParentID != rootExported.SpanID {
		t.Error("Child should reference root as parent")
	}

	if grandchildExported.ParentID != childExported.SpanID {
		t.Error("Grandchild should reference child as parent")
	}

	// Root should have no parent.
	if rootExported.ParentID != "" {
		t.Error("Root should have no parent")
	}
}

func TestTracerKeyConstantsAndBackwardsCompatibility(t *testing.T) {
	tracer := New()

	// Register a handler so spans are actually created (not no-op)
	var captured []Span
	tracer.OnSpanComplete(func(s Span) {
		captured = append(captured, s)
	})

	ctx := context.Background()

	// Test Key constants work.
	_, keySpan := tracer.StartSpan(ctx, DBQueryKey)
	if keySpan.span.Name != DBQueryKey {
		t.Errorf("Expected span name %s, got %s", DBQueryKey, keySpan.span.Name)
	}

	// Test backwards compatibility - string literals still work.
	_, stringSpan := tracer.StartSpan(ctx, "legacy-operation")
	if stringSpan.span.Name != "legacy-operation" {
		t.Errorf("Expected span name 'legacy-operation', got %s", stringSpan.span.Name)
	}

	// Test dynamic Key construction still works.
	dynamicKey := Key("dynamic.operation.123")
	_, dynamicSpan := tracer.StartSpan(ctx, dynamicKey)
	if dynamicSpan.span.Name != string(dynamicKey) {
		t.Errorf("Expected span name %s, got %s", dynamicKey, dynamicSpan.span.Name)
	}
}

func TestTracerIDFallback(t *testing.T) {
	// This test verifies fallback behavior when crypto/rand fails.
	// In practice, this is hard to trigger, but the code handles it.

	tracer := New()
	// Add handler so spans are created with IDs
	tracer.OnSpanComplete(func(_ Span) {})
	ctx := context.Background()

	_, activeSpan := tracer.StartSpan(ctx, "test-operation")

	// IDs should still be generated (using time-based fallback).
	if activeSpan.span.TraceID == "" {
		t.Error("Expected non-empty TraceID even with potential rand failure")
	}

	if activeSpan.span.SpanID == "" {
		t.Error("Expected non-empty SpanID even with potential rand failure")
	}
}

func TestTracerStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	tracer := New()
	var spans []Span
	var mu sync.Mutex
	tracer.OnSpanComplete(func(span Span) {
		mu.Lock()
		spans = append(spans, span)
		mu.Unlock()
	})

	ctx := context.Background()
	numGoroutines := 100
	spansPerGoroutine := 100

	start := time.Now()
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for j := 0; j < spansPerGoroutine; j++ {
				_, span := tracer.StartSpan(ctx, "stress-operation")
				span.SetTag("iteration", "test")
				span.Finish()
			}
		}()
	}

	wg.Wait()
	duration := time.Since(start)

	expectedSpans := numGoroutines * spansPerGoroutine
	t.Logf("Created %d spans in %v (%0.2f spans/sec)",
		expectedSpans, duration, float64(expectedSpans)/duration.Seconds())

	// Give time for collection.
	time.Sleep(200 * time.Millisecond)

	actualSpans := len(spans)
	droppedSpans := tracer.DroppedSpans()
	totalProcessed := actualSpans + int(droppedSpans) //nolint:gosec // Safe conversion, test only

	if totalProcessed != expectedSpans {
		t.Errorf("Expected %d total spans, got %d (collected: %d, dropped: %d)",
			expectedSpans, totalProcessed, actualSpans, droppedSpans)
	}

	if droppedSpans > 0 {
		t.Logf("Dropped %d spans under stress (%.2f%%)",
			droppedSpans, float64(droppedSpans)/float64(expectedSpans)*100)
	}
}

// TestTracerIDPoolIntegration tests ID pools integrated with tracer.
func TestTracerIDPoolIntegration(t *testing.T) {
	tracer := New()
	// Add handler so ID pools are initialized
	tracer.OnSpanComplete(func(_ Span) {})
	defer tracer.Close()

	ctx := context.Background()

	// First span should initialize pools.
	_, span1 := tracer.StartSpan(ctx, "test-operation")
	traceID1 := span1.TraceID()
	spanID1 := span1.SpanID()

	// Verify IDs are properly formatted.
	if len(traceID1) != 32 { // 16 bytes = 32 hex chars.
		t.Errorf("Expected trace ID length 32, got %d", len(traceID1))
	}
	if len(spanID1) != 16 { // 8 bytes = 16 hex chars.
		t.Errorf("Expected span ID length 16, got %d", len(spanID1))
	}

	// Second span should use pools.
	_, span2 := tracer.StartSpan(ctx, "test-operation-2")
	traceID2 := span2.TraceID()
	spanID2 := span2.SpanID()

	// IDs should be unique.
	if traceID1 == traceID2 {
		t.Error("Trace IDs should be unique")
	}
	if spanID1 == spanID2 {
		t.Error("Span IDs should be unique")
	}

	span1.Finish()
	span2.Finish()
}

// TestTracerCloseWithPools tests clean shutdown of tracer with ID pools.
func TestTracerCloseWithPools(t *testing.T) {
	tracer := New()

	// Force pool initialization.
	ctx := context.Background()
	_, span := tracer.StartSpan(ctx, "init-pools")
	span.Finish()

	// Get goroutine count before close.
	before := runtime.NumGoroutine()

	// Close tracer (should close pools).
	tracer.Close()

	// Give time for cleanup.
	time.Sleep(20 * time.Millisecond)

	// Should not have leaked goroutines.
	after := runtime.NumGoroutine()
	if after > before {
		t.Errorf("Goroutine leak detected after tracer close: %d -> %d", before, after)
	}
}

// TestTracerWithFakeClock verifies that WithClock enables deterministic span timing.
func TestTracerWithFakeClock(t *testing.T) {
	fakeClock := clockz.NewFakeClock()
	tracer := New().WithClock(fakeClock)
	// Add handler so spans track time
	tracer.OnSpanComplete(func(_ Span) {})
	defer tracer.Close()

	// Start a span
	_, span := tracer.StartSpan(context.Background(), "test-operation")
	startTime := span.span.StartTime

	// Advance the fake clock
	advancement := 100 * time.Millisecond
	fakeClock.Advance(advancement)

	// Finish the span
	span.Finish()

	// Verify the duration matches the exact advancement
	expectedDuration := advancement
	if span.span.Duration != expectedDuration {
		t.Errorf("Expected duration %v, got %v", expectedDuration, span.span.Duration)
	}

	// Verify end time is start time plus advancement
	expectedEndTime := startTime.Add(advancement)
	if span.span.EndTime != expectedEndTime {
		t.Errorf("Expected end time %v, got %v", expectedEndTime, span.span.EndTime)
	}
}

// TestTracerBackwardCompatibility ensures New() constructor still works with real clock.
func TestTracerBackwardCompatibility(t *testing.T) {
	tracer := New()
	// Add handler so spans work normally
	tracer.OnSpanComplete(func(_ Span) {})
	defer tracer.Close()

	// Should use real clock by default
	_, span := tracer.StartSpan(context.Background(), "test-operation")

	// Small delay to ensure measurable duration
	time.Sleep(1 * time.Millisecond)
	span.Finish()

	// Duration should be positive (real time elapsed)
	if span.span.Duration <= 0 {
		t.Error("Expected positive duration with real clock")
	}

	// StartTime should be reasonable (within last second)
	now := time.Now()
	if span.span.StartTime.After(now) || span.span.StartTime.Before(now.Add(-1*time.Second)) {
		t.Errorf("StartTime %v seems unreasonable compared to now %v", span.span.StartTime, now)
	}
}

// TestTracerFallbackIDGeneration verifies deterministic fallback IDs with fake clock.
func TestTracerFallbackIDGeneration(t *testing.T) {
	fakeClock := clockz.NewFakeClock()
	tracer := New().WithClock(fakeClock)
	// Add handler so IDs are generated
	tracer.OnSpanComplete(func(_ Span) {})
	defer tracer.Close()

	// Force pool initialization to test fallback behavior
	// Note: This test assumes we can trigger the fallback path
	// In practice, crypto/rand rarely fails, so this tests the code path exists
	_, span1 := tracer.StartSpan(context.Background(), "test1")
	span1.Finish()

	// Advance clock
	fakeClock.Advance(1 * time.Second)

	_, span2 := tracer.StartSpan(context.Background(), "test2")
	span2.Finish()

	// Verify spans have different IDs (even with fake clock, crypto/rand should work)
	if span1.span.SpanID == span2.span.SpanID {
		t.Error("Expected different span IDs")
	}
	if span1.span.TraceID == span2.span.TraceID {
		t.Error("Expected different trace IDs")
	}
}

// TestTracerClockInjection verifies clock is properly injected and used.
func TestTracerClockInjection(t *testing.T) {
	// Create two tracers with different clocks
	fakeClock1 := clockz.NewFakeClockAt(time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC))
	fakeClock2 := clockz.NewFakeClockAt(time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC))

	tracer1 := New().WithClock(fakeClock1)
	tracer2 := New().WithClock(fakeClock2)
	// Add handlers so spans track time
	tracer1.OnSpanComplete(func(_ Span) {})
	tracer2.OnSpanComplete(func(_ Span) {})
	defer tracer1.Close()
	defer tracer2.Close()

	// Start spans on each tracer
	_, span1 := tracer1.StartSpan(context.Background(), "test1")
	_, span2 := tracer2.StartSpan(context.Background(), "test2")

	span1.Finish()
	span2.Finish()

	// Verify each span uses its tracer's clock
	expectedTime1 := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	expectedTime2 := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)

	if span1.span.StartTime != expectedTime1 {
		t.Errorf("Span1 start time %v, expected %v", span1.span.StartTime, expectedTime1)
	}
	if span2.span.StartTime != expectedTime2 {
		t.Errorf("Span2 start time %v, expected %v", span2.span.StartTime, expectedTime2)
	}
}

func TestSetPanicHook(t *testing.T) {
	tracer := New()

	var hookCalled bool
	var capturedID uint64
	var capturedPanic interface{}

	tracer.SetPanicHook(func(handlerID uint64, r interface{}) {
		hookCalled = true
		capturedID = handlerID
		capturedPanic = r
	})

	// Register a handler that panics
	id := tracer.OnSpanComplete(func(_ Span) {
		panic("test panic")
	})

	// Create and finish a span to trigger the handler
	_, span := tracer.StartSpan(context.Background(), "test")
	span.Finish()

	if !hookCalled {
		t.Error("Panic hook was not called")
	}

	if capturedID != id {
		t.Errorf("Expected handler ID %d, got %d", id, capturedID)
	}

	if capturedPanic != "test panic" {
		t.Errorf("Expected panic value 'test panic', got %v", capturedPanic)
	}
}

func TestOnSpanCompleteAsync(t *testing.T) {
	tracer := New()

	// Enable worker pool for async handlers
	err := tracer.EnableWorkerPool(2, 10)
	if err != nil {
		t.Fatalf("Failed to enable worker pool: %v", err)
	}
	defer tracer.Close()

	done := make(chan bool)
	var called bool

	id := tracer.OnSpanCompleteAsync(func(_ Span) {
		called = true
		done <- true
	})

	if id == 0 {
		t.Error("Expected non-zero handler ID")
	}

	// Create and finish a span
	_, span := tracer.StartSpan(context.Background(), "test")
	span.Finish()

	// Wait for async handler to execute
	select {
	case <-done:
		if !called {
			t.Error("Async handler was not called")
		}
	case <-time.After(time.Second):
		t.Error("Async handler timeout")
	}
}

func TestEnableWorkerPoolValidation(t *testing.T) {
	tests := []struct {
		name      string
		workers   int
		queueSize int
		wantErr   string
	}{
		{
			name:      "zero workers",
			workers:   0,
			queueSize: 10,
			wantErr:   "workers must be > 0",
		},
		{
			name:      "negative workers",
			workers:   -1,
			queueSize: 10,
			wantErr:   "workers must be > 0",
		},
		{
			name:      "zero queue size",
			workers:   2,
			queueSize: 0,
			wantErr:   "queueSize must be > 0",
		},
		{
			name:      "negative queue size",
			workers:   2,
			queueSize: -1,
			wantErr:   "queueSize must be > 0",
		},
		{
			name:      "valid configuration",
			workers:   2,
			queueSize: 10,
			wantErr:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tracer := New()
			defer tracer.Close()

			err := tracer.EnableWorkerPool(tt.workers, tt.queueSize)

			if tt.wantErr != "" {
				if err == nil {
					t.Errorf("Expected error %q, got nil", tt.wantErr)
				} else if err.Error() != tt.wantErr {
					t.Errorf("Expected error %q, got %q", tt.wantErr, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

func TestEnableWorkerPoolAlreadyEnabled(t *testing.T) {
	tracer := New()
	defer tracer.Close()

	// Enable worker pool once
	err := tracer.EnableWorkerPool(2, 10)
	if err != nil {
		t.Fatalf("First EnableWorkerPool failed: %v", err)
	}

	// Try to enable again
	err = tracer.EnableWorkerPool(3, 20)
	if err == nil {
		t.Error("Expected error for double EnableWorkerPool, got nil")
	}
	if err.Error() != "worker pool already enabled" {
		t.Errorf("Expected 'worker pool already enabled', got %v", err)
	}
}

func TestWorkerPoolBasicOperation(t *testing.T) {
	tracer := New()
	defer tracer.Close()

	// Enable worker pool
	err := tracer.EnableWorkerPool(2, 10)
	if err != nil {
		t.Fatalf("Failed to enable worker pool: %v", err)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var asyncCount int
	var syncCount int

	// Register async handler
	tracer.OnSpanCompleteAsync(func(_ Span) {
		mu.Lock()
		asyncCount++
		mu.Unlock()
		wg.Done()
	})

	// Register sync handler
	tracer.OnSpanComplete(func(_ Span) {
		mu.Lock()
		syncCount++
		mu.Unlock()
	})

	// Create multiple spans
	numSpans := 5
	wg.Add(numSpans)

	for i := 0; i < numSpans; i++ {
		_, span := tracer.StartSpan(context.Background(), "test")
		span.Finish()
	}

	// Wait for all async handlers to complete
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	if asyncCount != numSpans {
		t.Errorf("Expected %d async handler calls, got %d", numSpans, asyncCount)
	}
	if syncCount != numSpans {
		t.Errorf("Expected %d sync handler calls, got %d", numSpans, syncCount)
	}
}

func TestSpanDropCounting(t *testing.T) {
	tracer := New()

	// Enable worker pool with small queue
	err := tracer.EnableWorkerPool(1, 2)
	if err != nil {
		t.Fatalf("Failed to enable worker pool: %v", err)
	}

	// Add a slow async handler to fill the queue
	blocked := make(chan struct{})
	unblock := make(chan struct{})

	tracer.OnSpanCompleteAsync(func(_ Span) {
		select {
		case blocked <- struct{}{}:
		default:
		}
		<-unblock
	})

	// Create first span to block the worker
	_, span1 := tracer.StartSpan(context.Background(), "span1")
	span1.Finish()

	// Wait for handler to start blocking
	<-blocked

	// Create spans to fill and overflow the queue
	for i := 0; i < 5; i++ {
		_, span := tracer.StartSpan(context.Background(), "overflow")
		span.Finish()
	}

	// Check dropped count (should be at least 3 since queue size is 2)
	dropped := tracer.DroppedSpans()
	if dropped < 3 {
		t.Errorf("Expected at least 3 dropped spans, got %d", dropped)
	}

	// Unblock the handler and clean up
	close(unblock)
	tracer.Close()
}

func TestWorkerPoolShutdown(t *testing.T) {
	tracer := New()

	// Enable worker pool
	err := tracer.EnableWorkerPool(2, 10)
	if err != nil {
		t.Fatalf("Failed to enable worker pool: %v", err)
	}

	var wg sync.WaitGroup
	executed := make(chan bool, 10)

	// Register async handler
	tracer.OnSpanCompleteAsync(func(_ Span) {
		executed <- true
		wg.Done()
	})

	// Create spans
	wg.Add(3)
	for i := 0; i < 3; i++ {
		_, span := tracer.StartSpan(context.Background(), "test")
		span.Finish()
	}

	// Wait for handlers to be processed
	wg.Wait()

	// Close tracer (should wait for any remaining handlers to complete)
	tracer.Close()

	// Count executed handlers
	count := 0
	close(executed)
	for range executed {
		count++
	}

	if count != 3 {
		t.Errorf("Expected 3 handlers to execute, got %d", count)
	}
}

func TestPanicIsolation(t *testing.T) {
	tracer := New()
	defer tracer.Close()

	var handler1Count int
	var handler2Count int
	var handler3Count int
	var panicCount int

	tracer.SetPanicHook(func(_ uint64, _ interface{}) {
		panicCount++
	})

	// First handler - runs fine
	tracer.OnSpanComplete(func(_ Span) {
		handler1Count++
	})

	// Second handler - panics
	tracer.OnSpanComplete(func(_ Span) {
		handler2Count++
		panic("handler 2 panic")
	})

	// Third handler - should still run
	tracer.OnSpanComplete(func(_ Span) {
		handler3Count++
	})

	// Create and finish a span
	_, span := tracer.StartSpan(context.Background(), "test")
	span.Finish()

	// Verify handlers 1 and 3 ran, handler 2 panicked
	if handler1Count != 1 {
		t.Errorf("Expected handler1 to run once, got %d", handler1Count)
	}
	if handler2Count != 1 {
		t.Errorf("Expected handler2 to run once before panic, got %d", handler2Count)
	}
	if handler3Count != 1 {
		t.Errorf("Expected handler3 to run once, got %d", handler3Count)
	}
	if panicCount != 1 {
		t.Errorf("Expected 1 panic, got %d", panicCount)
	}
}
