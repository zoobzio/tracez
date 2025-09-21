package tracez

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestActiveSpanSetTag(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	activeSpan.SetTag("key1", "value1")
	activeSpan.SetTag("key2", "value2")

	if len(span.Tags) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(span.Tags))
	}

	if span.Tags["key1"] != "value1" {
		t.Errorf("Expected tag key1=value1, got %s", span.Tags["key1"])
	}

	if span.Tags["key2"] != "value2" {
		t.Errorf("Expected tag key2=value2, got %s", span.Tags["key2"])
	}
}

func TestActiveSpanGetTag(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
		Tags:      map[string]string{"existing": "value"},
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	// Test existing tag.
	value, ok := activeSpan.GetTag("existing")
	if !ok {
		t.Error("Expected to find existing tag")
	}
	if value != "value" {
		t.Errorf("Expected 'value', got %s", value)
	}

	// Test non-existing tag.
	_, ok = activeSpan.GetTag("missing")
	if ok {
		t.Error("Expected not to find missing tag")
	}

	// Test nil tags map.
	span.Tags = nil
	_, ok = activeSpan.GetTag("any")
	if ok {
		t.Error("Expected not to find any tag when map is nil")
	}
}

func TestActiveSpanSetIntTag(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	// Test setting positive integer
	activeSpan.SetIntTag("count", 42)
	if span.Tags["count"] != "42" {
		t.Errorf("Expected tag count=42, got %s", span.Tags["count"])
	}

	// Test setting negative integer
	activeSpan.SetIntTag("temperature", -15)
	if span.Tags["temperature"] != "-15" {
		t.Errorf("Expected tag temperature=-15, got %s", span.Tags["temperature"])
	}

	// Test setting zero
	activeSpan.SetIntTag("offset", 0)
	if span.Tags["offset"] != "0" {
		t.Errorf("Expected tag offset=0, got %s", span.Tags["offset"])
	}
}

func TestActiveSpanSetBoolTag(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	// Test setting true
	activeSpan.SetBoolTag("success", true)
	if span.Tags["success"] != "true" {
		t.Errorf("Expected tag success=true, got %s", span.Tags["success"])
	}

	// Test setting false
	activeSpan.SetBoolTag("error", false)
	if span.Tags["error"] != "false" {
		t.Errorf("Expected tag error=false, got %s", span.Tags["error"])
	}
}

func TestActiveSpanSetTagsAfterFinish(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	// Finish the span
	activeSpan.Finish()

	// Try to set tags after finish (should be no-ops)
	activeSpan.SetTag("string", "value")
	activeSpan.SetIntTag("int", 123)
	activeSpan.SetBoolTag("bool", true)

	// Tags should not have been added
	if _, ok := span.Tags["string"]; ok {
		t.Error("String tag should not have been added after finish")
	}
	if _, ok := span.Tags["int"]; ok {
		t.Error("Int tag should not have been added after finish")
	}
	if _, ok := span.Tags["bool"]; ok {
		t.Error("Bool tag should not have been added after finish")
	}
}

func TestConcurrentTagSetting(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	var wg sync.WaitGroup
	numGoroutines := 100

	// Test concurrent SetTag operations.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", n)
			value := fmt.Sprintf("value%d", n)
			activeSpan.SetTag(key, value)
		}(i)
	}

	wg.Wait()

	// Verify all tags were set correctly.
	if len(span.Tags) != numGoroutines {
		t.Errorf("Expected %d tags, got %d", numGoroutines, len(span.Tags))
	}

	for i := 0; i < numGoroutines; i++ {
		key := fmt.Sprintf("key%d", i)
		expectedValue := fmt.Sprintf("value%d", i)
		if actualValue, ok := span.Tags[key]; !ok {
			t.Errorf("Expected to find tag %s", key)
		} else if actualValue != expectedValue {
			t.Errorf("Expected %s=%s, got %s", key, expectedValue, actualValue)
		}
	}
}

func TestConcurrentTagGetting(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
		Tags:      make(map[string]string),
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	// Pre-populate with some tags.
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		span.Tags[key] = value
	}

	var wg sync.WaitGroup
	numGoroutines := 100

	// Test concurrent GetTag operations.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", n%50) // Cycle through existing keys.
			expectedValue := fmt.Sprintf("value%d", n%50)

			if value, ok := activeSpan.GetTag(key); !ok {
				t.Errorf("Expected to find tag %s", key)
			} else if value != expectedValue {
				t.Errorf("Expected %s, got %s for key %s", expectedValue, value, key)
			}
		}(i)
	}

	wg.Wait()
}

func TestConcurrentSetAndGet(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	var wg sync.WaitGroup
	numGoroutines := 50

	// Concurrent SetTag operations.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", n)
			value := fmt.Sprintf("value%d", n)
			activeSpan.SetTag(key, value)
		}(i)
	}

	// Concurrent GetTag operations.
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := fmt.Sprintf("key%d", n)
			// May or may not find the key depending on timing.
			activeSpan.GetTag(key)
		}(i)
	}

	wg.Wait()

	// Verify final state.
	if len(span.Tags) != numGoroutines {
		t.Errorf("Expected %d tags, got %d", numGoroutines, len(span.Tags))
	}
}

func TestActiveSpanFinish(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
	}

	tracer := New()
	// Register handler but we don't need to check the result
	tracer.OnSpanComplete(func(_ Span) {
		// Handler called when span finishes
	})

	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	// Finish should set end time and duration.
	activeSpan.Finish()

	if span.EndTime.IsZero() {
		t.Error("Expected EndTime to be set after Finish()")
	}

	if span.Duration == 0 {
		t.Error("Expected Duration to be set after Finish()")
	}

	// Second finish should be a no-op.
	endTime1 := span.EndTime
	duration1 := span.Duration
	time.Sleep(time.Millisecond)

	activeSpan.Finish()

	if !span.EndTime.Equal(endTime1) {
		t.Error("Expected EndTime to remain unchanged on second Finish()")
	}

	if span.Duration != duration1 {
		t.Error("Expected Duration to remain unchanged on second Finish()")
	}
}

func TestActiveSpanContext(t *testing.T) {
	span := &Span{
		SpanID:    "test-span",
		TraceID:   "test-trace",
		Name:      "test",
		StartTime: time.Now(),
	}

	tracer := New()
	activeSpan := &ActiveSpan{span: span, tracer: tracer}

	parentCtx := context.Background()
	ctx := activeSpan.Context(parentCtx)

	// GetTracer function removed - users access tracer through their own references.

	// Verify span is embedded.
	if extractedSpan := GetSpan(ctx); extractedSpan != span {
		t.Error("Expected to extract the same span from context")
	}
}

func TestGetSpanFromContext(t *testing.T) {
	// Test with span in context using proper API.
	tracer := New()
	ctx, activeSpan := tracer.StartSpan(context.Background(), "test-operation")

	extractedSpan := GetSpan(ctx)
	if extractedSpan != activeSpan.span {
		t.Error("Expected to extract the span from context")
	}

	// Test with no span in context.
	emptyCtx := context.Background()
	if extractedSpan := GetSpan(emptyCtx); extractedSpan != nil {
		t.Error("Expected nil span from empty context")
	}

	// Test with wrong type in context.
	// Use a string key (not our typed key) to ensure no collision.
	wrongCtx := context.WithValue(context.Background(), bundleKeyType("tracez"), "not-a-bundle")
	if extractedSpan := GetSpan(wrongCtx); extractedSpan != nil {
		t.Error("Expected nil span from context with wrong type")
	}
}

// TestGetTracerFromContext removed - GetTracer function no longer exists.

func TestContextKeySafety(t *testing.T) {
	// Test that our context keys don't collide with string keys.
	ctx := context.Background()

	// Set a string key with the same value (using custom type to avoid lint warnings).
	type testKey string
	ctx = context.WithValue(ctx, testKey("tracez"), "fake-bundle")

	// Set our real span using proper API.
	tracer := New()
	ctx, activeSpan := tracer.StartSpan(ctx, "test-operation")

	// Should extract the real span, not the fake one.
	if extractedSpan := GetSpan(ctx); extractedSpan != activeSpan.span {
		t.Error("Context key collision: extracted wrong span")
	}

	// String keys should still work alongside bundle.
	if value := ctx.Value(testKey("tracez")); value != "fake-bundle" {
		t.Error("String context key was affected by bundle key")
	}
}

// TestContextBundling tests the new context bundling approach.
func TestContextBundling(t *testing.T) {
	tracer := New()
	defer tracer.Close()

	ctx := context.Background()

	// Create span with new bundling approach.
	newCtx, span := tracer.StartSpan(ctx, "bundled-operation")

	// Should be able to extract span from context.
	extractedSpan := GetSpan(newCtx)
	if extractedSpan != span.span {
		t.Error("Failed to extract span from bundled context")
	}

	// Create child span using bundled context.
	_, childSpan := tracer.StartSpan(newCtx, "child-operation")

	// Should have correct parent relationship.
	if childSpan.span.TraceID != span.span.TraceID {
		t.Error("Child span should share trace ID with parent")
	}
	if childSpan.span.ParentID != span.span.SpanID {
		t.Error("Child span should reference parent span ID")
	}

	childSpan.Finish()
	span.Finish()
}

// TestBackwardCompatibilityContext tests that old context approach still works.
func TestBackwardCompatibilityContext(t *testing.T) {
	// This test ensures old code using ActiveSpan.Context() still works
	tracer := New()
	defer tracer.Close()

	ctx := context.Background()

	// Create span with tracer.
	_, span := tracer.StartSpan(ctx, "parent-operation")

	// Use old ActiveSpan.Context() method
	spanCtx := span.Context(ctx)

	// Should be able to extract using GetSpan.
	extractedSpan := GetSpan(spanCtx)
	if extractedSpan != span.span {
		t.Error("Failed to extract span from ActiveSpan.Context() result")
	}

	// Should work with child spans.
	_, childSpan := tracer.StartSpan(spanCtx, "child-operation")
	if childSpan.span.ParentID != span.span.SpanID {
		t.Error("Child span should reference parent via ActiveSpan.Context()")
	}

	childSpan.Finish()
	span.Finish()
}

func TestSetTagAfterFinish(t *testing.T) {
	tracer := New()
	defer tracer.Close()

	// Create a span
	_, activeSpan := tracer.StartSpan(context.Background(), "test-span")

	// Set a tag before finish
	activeSpan.SetTag("before", "value1")

	// Finish the span
	activeSpan.Finish()

	// Try to set a tag after finish
	activeSpan.SetTag("after", "value2")

	// Verify only the "before" tag exists
	if activeSpan.span.Tags["before"] != "value1" {
		t.Error("Expected 'before' tag to be set")
	}

	if _, exists := activeSpan.span.Tags["after"]; exists {
		t.Error("Tag should not be set after span is finished")
	}

	// Verify span is marked as finished
	if activeSpan.span.EndTime.IsZero() {
		t.Error("Span should have EndTime set after Finish()")
	}
}
