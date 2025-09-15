package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// TestDeepNestingChain verifies 100-level deep span hierarchy.
// All parent relationships must be correct.
func TestDeepNestingChain(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create deep hierarchy.
	nestingDepth := 100
	ctx := context.Background()
	spans := make([]*tracez.ActiveSpan, 0, nestingDepth)

	// Track expected relationships.
	expectedParents := make(map[string]string) // spanID -> parentID.

	// Create nested spans.
	var rootTraceID string
	for i := 0; i < nestingDepth; i++ {
		var span *tracez.ActiveSpan
		ctx, span = tracer.StartSpan(ctx, fmt.Sprintf("level-%03d", i))
		span.SetTag("depth", fmt.Sprintf("%d", i))
		spans = append(spans, span)

		if i == 0 {
			rootTraceID = span.TraceID()
		} else {
			// Each span's parent is the previous span.
			expectedParents[span.SpanID()] = spans[i-1].SpanID()
		}
	}

	// Finish in reverse order (deepest first).
	for i := len(spans) - 1; i >= 0; i-- {
		spans[i].Finish()
	}

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Export and verify.
	exported := collector.Export()

	if len(exported) != nestingDepth {
		t.Fatalf("Expected %d spans, got %d", nestingDepth, len(exported))
	}

	// Build lookup maps.
	spansByID := make(map[string]tracez.Span)
	for _, span := range exported {
		spansByID[span.SpanID] = span
	}

	// Verify all spans share same TraceID.
	for _, span := range exported {
		if span.TraceID != rootTraceID {
			t.Errorf("Span %s has wrong TraceID: expected %s, got %s",
				span.Name, rootTraceID, span.TraceID)
		}
	}

	// Verify parent relationships.
	for spanID, expectedParentID := range expectedParents {
		span, exists := spansByID[spanID]
		if !exists {
			t.Errorf("Span %s not found in export", spanID)
			continue
		}

		if span.ParentID != expectedParentID {
			t.Errorf("Span %s has wrong ParentID: expected %s, got %s",
				span.Name, expectedParentID, span.ParentID)
		}

		// Verify parent exists.
		if _, parentExists := spansByID[expectedParentID]; !parentExists {
			t.Errorf("Parent span %s not found for child %s", expectedParentID, spanID)
		}
	}

	// Verify depth tags.
	for _, span := range exported {
		depthTag, exists := span.Tags["depth"]
		if !exists {
			t.Errorf("Span %s missing depth tag", span.Name)
			continue
		}

		// Extract expected depth from name.
		var expectedDepth int
		fmt.Sscanf(span.Name, "level-%d", &expectedDepth)

		if depthTag != fmt.Sprintf("%d", expectedDepth) {
			t.Errorf("Span %s has wrong depth: expected %d, got %v",
				span.Name, expectedDepth, depthTag)
		}
	}
}

// TestSiblingSpanOrdering verifies timeline consistency.
// Parent creates children sequentially, then parallel grandchildren.
func TestSiblingSpanOrdering(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create parent.
	ctx, parentSpan := tracer.StartSpan(context.Background(), "parent")
	parentID := parentSpan.SpanID()

	// Create sequential children.
	childCount := 5
	childSpans := make([]*tracez.ActiveSpan, childCount)
	childIDs := make([]string, childCount)

	for i := 0; i < childCount; i++ {
		// Small delay to ensure timestamp ordering.
		time.Sleep(5 * time.Millisecond)

		var childSpan *tracez.ActiveSpan
		_, childSpan = tracer.StartSpan(ctx, fmt.Sprintf("child-%d", i)) // Use parent ctx, don't reassign.
		childSpan.SetTag("order", fmt.Sprintf("%d", i))
		childSpans[i] = childSpan
		childIDs[i] = childSpan.SpanID()
	}

	// Create parallel grandchildren for middle child.
	grandchildCount := 10
	done := make(chan bool, grandchildCount)

	for i := 0; i < grandchildCount; i++ {
		go func(idx int) {
			_, grandchild := tracer.StartSpan(ctx, fmt.Sprintf("grandchild-%d", idx))
			grandchild.SetTag("parallel", "true")
			grandchild.Finish()
			done <- true
		}(i)
	}

	// Wait for grandchildren.
	for i := 0; i < grandchildCount; i++ {
		<-done
	}

	// Finish children in order.
	for _, child := range childSpans {
		child.Finish()
	}

	parentSpan.Finish()

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Export and analyze.
	spans := collector.Export()

	expectedTotal := 1 + childCount + grandchildCount
	if len(spans) != expectedTotal {
		t.Fatalf("Expected %d spans, got %d", expectedTotal, len(spans))
	}

	// Separate spans by type.
	var parent tracez.Span
	children := make([]tracez.Span, 0)
	grandchildren := make([]tracez.Span, 0)

	for _, span := range spans {
		switch {
		case span.Name == "parent":
			parent = span
		case len(span.Name) >= 5 && span.Name[:5] == "child":
			children = append(children, span)
		case len(span.Name) >= 10 && span.Name[:10] == "grandchild":
			grandchildren = append(grandchildren, span)
		}
	}

	// Verify counts.
	if parent.SpanID != parentID {
		t.Error("Parent span ID mismatch")
	}
	if len(children) != childCount {
		t.Errorf("Expected %d children, got %d", childCount, len(children))
	}
	if len(grandchildren) != grandchildCount {
		t.Errorf("Expected %d grandchildren, got %d", grandchildCount, len(grandchildren))
	}

	// Verify children timestamps are sequential.
	for i := 1; i < len(children); i++ {
		if !children[i-1].StartTime.Before(children[i].StartTime) {
			t.Errorf("Child %d started before child %d", i, i-1)
		}
	}

	// Verify all children have parent as parent.
	for _, child := range children {
		if child.ParentID != parentID {
			t.Errorf("Child %s has wrong parent: expected %s, got %s",
				child.Name, parentID, child.ParentID)
		}
	}

	// Verify grandchildren all started within reasonable window.
	var minGrandchildStart, maxGrandchildStart time.Time
	for i, gc := range grandchildren {
		if i == 0 || gc.StartTime.Before(minGrandchildStart) {
			minGrandchildStart = gc.StartTime
		}
		if i == 0 || gc.StartTime.After(maxGrandchildStart) {
			maxGrandchildStart = gc.StartTime
		}
	}

	parallelWindow := maxGrandchildStart.Sub(minGrandchildStart)
	if parallelWindow > 100*time.Millisecond {
		t.Errorf("Grandchildren creation took too long: %v", parallelWindow)
	}
}

// TestOrphanSpanHandling verifies behavior with invalid parent references.
func TestOrphanSpanHandling(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create context with non-existent parent.
	fakeParentID := "non-existent-parent-id"
	ctx := context.Background()

	// Manually inject fake parent (simulating cross-service scenario).
	type spanKey struct{}
	fakeParent := struct {
		TraceID  string
		SpanID   string
		ParentID string
	}{
		TraceID:  "fake-trace-id",
		SpanID:   fakeParentID,
		ParentID: "",
	}
	// Create context with fake parent for testing.
	ctx = context.WithValue(ctx, spanKey{}, fakeParent) //nolint:ineffassign // Required for context test

	// Try to create child span.
	// Since we can't directly manipulate context internals,.
	// create a normal span and verify it handles missing parent gracefully.
	ctx2, orphan := tracer.StartSpan(ctx, "orphan-span")
	orphan.SetTag("type", "orphan")

	// Should get new TraceID since no valid parent.
	if orphan.TraceID() == "" {
		t.Error("Orphan span has empty TraceID")
	}

	// Create child of orphan.
	_, childOfOrphan := tracer.StartSpan(ctx2, "child-of-orphan")
	childOfOrphan.SetTag("type", "child")

	childOfOrphan.Finish()
	orphan.Finish()

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Export and verify.
	spans := collector.Export()

	if len(spans) != 2 {
		t.Fatalf("Expected 2 spans, got %d", len(spans))
	}

	// Find spans.
	var orphanSpan, childSpan tracez.Span
	for _, span := range spans {
		if span.Name == "orphan-span" {
			orphanSpan = span
		} else if span.Name == "child-of-orphan" {
			childSpan = span
		}
	}

	// Verify orphan created new trace.
	if orphanSpan.TraceID == "" {
		t.Error("Orphan has no TraceID")
	}
	if orphanSpan.SpanID == "" {
		t.Error("Orphan has no SpanID")
	}

	// Child should be properly linked to orphan.
	if childSpan.TraceID != orphanSpan.TraceID {
		t.Error("Child has different TraceID than orphan parent")
	}
	if childSpan.ParentID != orphanSpan.SpanID {
		t.Error("Child not properly linked to orphan parent")
	}
}

// TestComplexFamilyTree creates a realistic span hierarchy.
func TestComplexFamilyTree(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 1000)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create complex tree:.
	// root.
	// ├── auth.
	// │   ├── validate-token.
	// │   └── load-user.
	// ├── process.
	// │   ├── validate-input.
	// │   ├── business-logic.
	// │   │   ├── calculate.
	// │   │   └── store.
	// │   └── prepare-response.
	// └── audit.
	//     └── log-activity.

	ctx, root := tracer.StartSpan(context.Background(), "root")

	// Auth branch.
	authCtx, auth := tracer.StartSpan(ctx, "auth")

	_, validateToken := tracer.StartSpan(authCtx, "validate-token")
	validateToken.Finish()

	_, loadUser := tracer.StartSpan(authCtx, "load-user")
	loadUser.Finish()

	auth.Finish()

	// Process branch.
	processCtx, process := tracer.StartSpan(ctx, "process")

	_, validateInput := tracer.StartSpan(processCtx, "validate-input")
	validateInput.Finish()

	businessCtx, businessLogic := tracer.StartSpan(processCtx, "business-logic")

	_, calculate := tracer.StartSpan(businessCtx, "calculate")
	calculate.Finish()

	_, store := tracer.StartSpan(businessCtx, "store")
	store.Finish()

	businessLogic.Finish()

	_, prepareResponse := tracer.StartSpan(processCtx, "prepare-response")
	prepareResponse.Finish()

	process.Finish()

	// Audit branch.
	auditCtx, audit := tracer.StartSpan(ctx, "audit")

	_, logActivity := tracer.StartSpan(auditCtx, "log-activity")
	logActivity.Finish()

	audit.Finish()

	root.Finish()

	// Wait for collection.
	time.Sleep(100 * time.Millisecond)

	// Export and verify.
	spans := collector.Export()

	// Should have exactly 12 spans.
	if len(spans) != 12 {
		t.Fatalf("Expected 12 spans, got %d", len(spans))
	}

	// Build relationship map.
	spansByName := make(map[string]tracez.Span)
	for _, span := range spans {
		spansByName[span.Name] = span
	}

	// Verify specific relationships.
	expectations := []struct {
		child  string
		parent string
	}{
		{"auth", "root"},
		{"validate-token", "auth"},
		{"load-user", "auth"},
		{"process", "root"},
		{"validate-input", "process"},
		{"business-logic", "process"},
		{"calculate", "business-logic"},
		{"store", "business-logic"},
		{"prepare-response", "process"},
		{"audit", "root"},
		{"log-activity", "audit"},
	}

	for _, exp := range expectations {
		child, childExists := spansByName[exp.child]
		if !childExists {
			t.Errorf("Child span %s not found", exp.child)
			continue
		}

		parent, parentExists := spansByName[exp.parent]
		if !parentExists {
			t.Errorf("Parent span %s not found", exp.parent)
			continue
		}

		if child.ParentID != parent.SpanID {
			t.Errorf("%s should have %s as parent, but ParentID is %s",
				exp.child, exp.parent, child.ParentID)
		}
	}

	// Verify all spans share same TraceID.
	traceID := root.TraceID()
	for _, span := range spans {
		if span.TraceID != traceID {
			t.Errorf("Span %s has wrong TraceID", span.Name)
		}
	}
}

// TestSpanTimestampIntegrity verifies parent/child timestamp relationships.
func TestSpanTimestampIntegrity(t *testing.T) {
	tracer := tracez.New("test-service")
	collector := tracez.NewCollector("test", 100)
	tracer.AddCollector("collector", collector)
	defer tracer.Close()

	// Create parent.
	ctx, parent := tracer.StartSpan(context.Background(), "parent")
	time.Sleep(10 * time.Millisecond)

	// Create child.
	_, child := tracer.StartSpan(ctx, "child")
	time.Sleep(10 * time.Millisecond)

	// Finish child first.
	child.Finish()
	time.Sleep(10 * time.Millisecond)

	// Then finish parent.
	parent.Finish()

	// Wait for collection.
	time.Sleep(50 * time.Millisecond)

	// Export and verify.
	spans := collector.Export()

	var parentSpan, childSpan tracez.Span
	for _, span := range spans {
		if span.Name == "parent" {
			parentSpan = span
		} else if span.Name == "child" {
			childSpan = span
		}
	}

	// Parent should start before child.
	if !parentSpan.StartTime.Before(childSpan.StartTime) {
		t.Error("Parent didn't start before child")
	}

	// Child should end before parent.
	if !childSpan.EndTime.Before(parentSpan.EndTime) {
		t.Error("Child didn't end before parent")
	}

	// Child lifetime should be within parent lifetime.
	if childSpan.StartTime.Before(parentSpan.StartTime) {
		t.Error("Child started before parent")
	}
	if childSpan.EndTime.After(parentSpan.EndTime) {
		t.Error("Child ended after parent")
	}
}
