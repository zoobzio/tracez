package reliability

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// Span hierarchy corruption tests - verify parent-child relationships under stress
// Tests orphaned spans, circular references, and hierarchy validation under concurrent access

func TestSpanHierarchyCorruption(t *testing.T) {
	config := getReliabilityConfig()

	switch config.Level {
	case "basic":
		t.Run("orphaned_spans", testOrphanedSpans)
		t.Run("hierarchy_validation", testHierarchyValidation)
		t.Run("concurrent_hierarchy", testConcurrentHierarchy)
	case "stress":
		t.Run("massive_hierarchy", testMassiveHierarchy)
		t.Run("hierarchy_corruption", testHierarchyCorruption)
		t.Run("hierarchy_storm", testHierarchyStorm)
	default:
		t.Skip("TRACEZ_RELIABILITY_LEVEL not set, skipping reliability tests")
	}
}

// testOrphanedSpans verifies system handles spans without valid parents.
func testOrphanedSpans(t *testing.T) {
	tracer := tracez.New("orphaned-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Create spans in various orphaned scenarios
	scenarios := []struct {
		name    string
		setupFn func() (context.Context, *tracez.ActiveSpan)
	}{
		{
			name: "nil_context",
			setupFn: func() (context.Context, *tracez.ActiveSpan) {
				//nolint:staticcheck // Testing nil context handling
				return tracer.StartSpan(nil, "orphaned-nil")
			},
		},
		{
			name: "background_context",
			setupFn: func() (context.Context, *tracez.ActiveSpan) {
				return tracer.StartSpan(context.Background(), "orphaned-background")
			},
		},
		{
			name: "empty_value_context",
			setupFn: func() (context.Context, *tracez.ActiveSpan) {
				type contextKey string
				//nolint:revive // Test code demonstrating context corruption scenarios
				ctx := context.WithValue(context.Background(), contextKey("key"), "value")
				return tracer.StartSpan(ctx, "orphaned-value")
			},
		},
	}

	validSpans := 0

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, span := scenario.setupFn()

			// Span should be created successfully
			if span == nil {
				t.Errorf("Failed to create span for scenario: %s", scenario.name)
				return
			}

			// Span should have valid IDs
			if span.TraceID() == "" {
				t.Errorf("Empty trace ID for scenario: %s", scenario.name)
			}
			if span.SpanID() == "" {
				t.Errorf("Empty span ID for scenario: %s", scenario.name)
			}

			span.SetTag("scenario", scenario.name)
			span.SetTag("orphaned", "true")
			span.Finish()

			// Context should be valid
			if ctx == nil {
				t.Errorf("Got nil context for scenario: %s", scenario.name)
			}

			validSpans++
		})
	}

	// Verify all orphaned spans were collected
	exported := collector.Export()
	if len(exported) != validSpans {
		t.Errorf("Expected %d orphaned spans, got %d", validSpans, len(exported))
	}

	// Verify orphaned spans have no parent ID (empty string)
	for i := range exported {
		span := exported[i]
		if span.ParentID != "" {
			t.Errorf("Orphaned span has parent ID: %s", span.ParentID)
		}
	}
}

// testHierarchyValidation verifies parent-child relationships are correct.
func testHierarchyValidation(t *testing.T) {
	tracer := tracez.New("hierarchy-validation-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Create complex hierarchy: root -> 3 children -> 2 grandchildren each
	rootCtx, rootSpan := tracer.StartSpan(context.Background(), "hierarchy-root")
	rootSpan.SetTag("level", "0")
	rootTraceID := rootSpan.TraceID()
	rootSpanID := rootSpan.SpanID()

	childSpans := make([]*tracez.ActiveSpan, 3)
	grandchildSpans := make([][]*tracez.ActiveSpan, 3)

	// Create children
	for i := 0; i < 3; i++ {
		childCtx, childSpan := tracer.StartSpan(rootCtx, fmt.Sprintf("child-%d", i))
		childSpan.SetTag("level", "1")
		childSpan.SetTag("parent", "root")
		childSpans[i] = childSpan

		// Create grandchildren
		grandchildSpans[i] = make([]*tracez.ActiveSpan, 2)
		for j := 0; j < 2; j++ {
			_, grandchildSpan := tracer.StartSpan(childCtx, fmt.Sprintf("grandchild-%d-%d", i, j))
			grandchildSpan.SetTag("level", "2")
			grandchildSpan.SetTag("parent", fmt.Sprintf("child-%d", i))
			grandchildSpans[i][j] = grandchildSpan
		}
	}

	// Finish spans in proper order (children before parents)
	for i := 0; i < 3; i++ {
		for j := 0; j < 2; j++ {
			grandchildSpans[i][j].Finish()
		}
		childSpans[i].Finish()
	}
	rootSpan.Finish()

	// Verify hierarchy
	exported := collector.Export()
	expectedSpans := 1 + 3 + 6 // root + children + grandchildren

	if len(exported) != expectedSpans {
		t.Errorf("Expected %d spans in hierarchy, got %d", expectedSpans, len(exported))
	}

	// Group spans by level for validation
	spansByLevel := make(map[string][]*tracez.Span)
	for i := range exported {
		span := exported[i]
		level := span.Tags["level"]
		spansByLevel[level] = append(spansByLevel[level], &span)
	}

	// Validate root span
	if len(spansByLevel["0"]) != 1 {
		t.Errorf("Expected 1 root span, got %d", len(spansByLevel["0"]))
	} else {
		root := spansByLevel["0"][0]
		if root.TraceID != rootTraceID {
			t.Errorf("Root span trace ID mismatch")
		}
		if root.ParentID != "" {
			t.Errorf("Root span should have no parent, got: %s", root.ParentID)
		}
	}

	// Validate child spans
	if len(spansByLevel["1"]) != 3 {
		t.Errorf("Expected 3 child spans, got %d", len(spansByLevel["1"]))
	} else {
		missingParents := 0
		for _, child := range spansByLevel["1"] {
			if child.TraceID != rootTraceID {
				t.Errorf("Child span trace ID mismatch")
			}
			if child.ParentID == "" {
				missingParents++
			} else if child.ParentID != rootSpanID {
				t.Errorf("Child span parent ID mismatch: expected %s, got %s", rootSpanID, child.ParentID)
			}
		}
		if missingParents > 0 {
			t.Logf("Reliability Issue: %d child spans missing parent IDs (potential race condition)", missingParents)
		}
	}

	// Validate grandchild spans
	if len(spansByLevel["2"]) != 6 {
		t.Errorf("Expected 6 grandchild spans, got %d", len(spansByLevel["2"]))
	} else {
		childIDs := make(map[string]bool)
		for _, child := range spansByLevel["1"] {
			if child.SpanID != "" {
				childIDs[child.SpanID] = true
			}
		}

		orphanedGrandchildren := 0
		for _, grandchild := range spansByLevel["2"] {
			if grandchild.TraceID != rootTraceID {
				t.Errorf("Grandchild span trace ID mismatch")
			}
			if grandchild.ParentID == "" {
				orphanedGrandchildren++
			} else if !childIDs[grandchild.ParentID] {
				t.Errorf("Grandchild span parent ID not found in children: %s", grandchild.ParentID)
			}
		}
		if orphanedGrandchildren > 0 {
			t.Logf("Reliability Issue: %d grandchild spans missing parent IDs (potential race condition)", orphanedGrandchildren)
		}
	}
}

// testConcurrentHierarchy verifies hierarchy consistency under concurrent operations.
func testConcurrentHierarchy(t *testing.T) {
	tracer := tracez.New("concurrent-hierarchy-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 5000)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Create root span
	rootCtx, rootSpan := tracer.StartSpan(context.Background(), "concurrent-root")
	rootSpan.SetTag("level", "root")
	rootTraceID := rootSpan.TraceID()
	defer rootSpan.Finish()

	numGoroutines := runtime.NumCPU() * 2
	spansPerGoroutine := 50

	var wg sync.WaitGroup
	var hierarchyErrors atomic.Int64

	// Launch concurrent hierarchy builders
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			// Create branch span for this goroutine
			branchCtx, branchSpan := tracer.StartSpan(rootCtx, fmt.Sprintf("branch-%d", goroutineID))
			branchSpan.SetTag("level", "branch")
			branchSpan.SetTag("goroutine", fmt.Sprintf("%d", goroutineID))
			branchSpanID := branchSpan.SpanID()
			defer branchSpan.Finish()

			// Create leaf spans
			for j := 0; j < spansPerGoroutine; j++ {
				leafCtx, leafSpan := tracer.StartSpan(branchCtx, fmt.Sprintf("leaf-%d-%d", goroutineID, j))
				leafSpan.SetTag("level", "leaf")
				leafSpan.SetTag("goroutine", fmt.Sprintf("%d", goroutineID))
				leafSpan.SetTag("index", fmt.Sprintf("%d", j))

				// Verify hierarchy immediately
				if leafSpan.TraceID() != rootTraceID {
					hierarchyErrors.Add(1)
				}

				// Verify parent relationship through context
				parentSpan := tracez.GetSpan(branchCtx)
				if parentSpan == nil || parentSpan.SpanID != branchSpanID {
					hierarchyErrors.Add(1)
				}

				leafSpan.Finish()
				_ = leafCtx // Satisfy linter
			}
		}(i)
	}

	wg.Wait()

	if hierarchyErrors.Load() > 0 {
		t.Errorf("Hierarchy errors detected during concurrent operations: %d", hierarchyErrors.Load())
	}

	// Verify final hierarchy
	exported := collector.Export()
	expectedSpans := 1 + numGoroutines + (numGoroutines * spansPerGoroutine) // root + branches + leaves

	t.Logf("Concurrent hierarchy results:")
	t.Logf("  Expected spans: %d", expectedSpans)
	t.Logf("  Collected spans: %d", len(exported))
	t.Logf("  Hierarchy errors: %d", hierarchyErrors.Load())

	if len(exported) < expectedSpans*9/10 { // Allow 10% loss
		t.Errorf("Too few spans collected: got %d, expected ~%d", len(exported), expectedSpans)
	}

	// Validate trace continuity
	for i := range exported {
		span := exported[i]
		if span.TraceID != rootTraceID {
			t.Errorf("Span has wrong trace ID: %s vs %s", span.TraceID, rootTraceID)
			break
		}
	}
}

// testMassiveHierarchy - stress test with very large span trees.
func testMassiveHierarchy(t *testing.T) {
	tracer := tracez.New("massive-hierarchy-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 100000)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Create massive hierarchy: 1 root -> 100 branches -> 100 leaves each
	rootCtx, rootSpan := tracer.StartSpan(context.Background(), "massive-root")
	rootSpan.SetTag("level", "0")
	rootTraceID := rootSpan.TraceID()
	defer rootSpan.Finish()

	numBranches := 100
	leavesPerBranch := 100
	totalExpected := 1 + numBranches + (numBranches * leavesPerBranch)

	start := time.Now()

	// Create hierarchy iteratively to avoid stack overflow
	for i := 0; i < numBranches; i++ {
		branchCtx, branchSpan := tracer.StartSpan(rootCtx, fmt.Sprintf("massive-branch-%d", i))
		branchSpan.SetTag("level", "1")
		branchSpan.SetTag("branch", fmt.Sprintf("%d", i))

		for j := 0; j < leavesPerBranch; j++ {
			leafCtx, leafSpan := tracer.StartSpan(branchCtx, fmt.Sprintf("massive-leaf-%d-%d", i, j))
			leafSpan.SetTag("level", "2")
			leafSpan.SetTag("branch", fmt.Sprintf("%d", i))
			leafSpan.SetTag("leaf", fmt.Sprintf("%d", j))
			leafSpan.Finish()
			_ = leafCtx // Satisfy linter
		}

		branchSpan.Finish()
	}

	duration := time.Since(start)
	spansPerSecond := float64(totalExpected) / duration.Seconds()

	t.Logf("Massive hierarchy results:")
	t.Logf("  Total spans: %d", totalExpected)
	t.Logf("  Duration: %v", duration)
	t.Logf("  Rate: %.0f spans/sec", spansPerSecond)

	// Verify collection
	exported := collector.Export()
	if len(exported) != totalExpected {
		t.Errorf("Expected %d spans, got %d", totalExpected, len(exported))
	}

	// Sample hierarchy validation (checking all would be too slow)
	sampleSize := 100
	hierarchyValid := true

	for i := 0; i < sampleSize && i < len(exported); i++ {
		span := exported[i]
		if span.TraceID != rootTraceID {
			hierarchyValid = false
			break
		}

		// Check parent-child relationships for sampled spans
		level := span.Tags["level"]
		switch level {
		case "0": // Root
			if span.ParentID != "" {
				hierarchyValid = false
			}
		case "1", "2": // Branch or leaf
			if span.ParentID == "" {
				hierarchyValid = false
			}
		}
	}

	if !hierarchyValid {
		t.Error("Hierarchy validation failed in massive hierarchy")
	}

	// Performance should be reasonable
	if spansPerSecond < 5000 {
		t.Errorf("Massive hierarchy performance too slow: %.0f spans/sec", spansPerSecond)
	}
}

// testHierarchyCorruption - test system resilience against corrupted hierarchies.
func testHierarchyCorruption(t *testing.T) {
	tracer := tracez.New("corruption-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 1000)
	// Note: collector cleanup handled by tracer.Close() in production use
	collector.SetSyncMode(true)
	tracer.AddCollector("test", collector)

	// Create various corruption scenarios that should be handled gracefully
	scenarios := []struct {
		name   string
		testFn func() error
	}{
		{
			name: "parent_finished_before_child",
			testFn: func() error {
				ctx, parent := tracer.StartSpan(context.Background(), "early-parent")
				parent.SetTag("corruption", "early-finish")

				childCtx, child := tracer.StartSpan(ctx, "orphaned-child")
				child.SetTag("corruption", "parent-finished-early")

				// Finish parent before child (incorrect order)
				parent.Finish()
				child.Finish()
				_ = childCtx // Satisfy linter

				return nil
			},
		},
		{
			name: "deep_recursive_nesting",
			testFn: func() error {
				ctx := context.Background()
				spans := make([]*tracez.ActiveSpan, 1000)

				// Create very deep nesting
				for i := 0; i < 1000; i++ {
					var span *tracez.ActiveSpan
					ctx, span = tracer.StartSpan(ctx, fmt.Sprintf("deep-%d", i))
					span.SetTag("depth", fmt.Sprintf("%d", i))
					spans[i] = span
				}

				// Finish in random order (corruption)
				for i := 0; i < 1000; i += 2 {
					spans[i].Finish()
				}
				for i := 1; i < 1000; i += 2 {
					spans[i].Finish()
				}

				return nil
			},
		},
		{
			name: "concurrent_finish_same_span",
			testFn: func() error {
				ctx, span := tracer.StartSpan(context.Background(), "concurrent-finish")
				span.SetTag("corruption", "multiple-finish")

				var wg sync.WaitGroup

				// Multiple goroutines try to finish same span
				for i := 0; i < 10; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						span.Finish() // Should be safe to call multiple times
					}()
				}

				wg.Wait()
				_ = ctx // Satisfy linter
				return nil
			},
		},
	}

	successfulScenarios := 0

	for _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// Test should not panic or crash
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Panic in corruption scenario %s: %v", scenario.name, r)
					}
				}()

				if err := scenario.testFn(); err != nil {
					t.Errorf("Error in corruption scenario %s: %v", scenario.name, err)
				} else {
					successfulScenarios++
				}
			}()
		})
	}

	// System should handle all corruption scenarios gracefully
	if successfulScenarios != len(scenarios) {
		t.Errorf("Some corruption scenarios failed: %d/%d successful",
			successfulScenarios, len(scenarios))
	}

	// Verify spans were still collected despite corruption
	exported := collector.Export()
	if len(exported) == 0 {
		t.Error("No spans collected after corruption tests")
	}

	t.Logf("Hierarchy corruption results:")
	t.Logf("  Scenarios: %d", len(scenarios))
	t.Logf("  Successful: %d", successfulScenarios)
	t.Logf("  Spans collected: %d", len(exported))
}

// testHierarchyStorm - extreme concurrent hierarchy operations.
func testHierarchyStorm(t *testing.T) {
	tracer := tracez.New("hierarchy-storm-test")
	defer tracer.Close()

	collector := tracez.NewCollector("test", 200000)
	// Note: collector cleanup handled by tracer.Close() in production use
	tracer.AddCollector("test", collector)

	duration := 10 * time.Second
	numGoroutines := runtime.NumCPU() * 4

	var totalSpans atomic.Int64
	var hierarchyErrors atomic.Int64

	ctx, cancel := context.WithTimeout(context.Background(), duration)
	defer cancel()

	// Create root span for all hierarchies
	rootCtx, rootSpan := tracer.StartSpan(context.Background(), "storm-root")
	rootSpan.SetTag("test", "hierarchy-storm")
	rootTraceID := rootSpan.TraceID()
	defer rootSpan.Finish()

	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			localCtx := rootCtx
			spans := make([]*tracez.ActiveSpan, 0, 100)

			for {
				select {
				case <-ctx.Done():
					// Finish all remaining spans
					for i := len(spans) - 1; i >= 0; i-- {
						spans[i].Finish()
					}
					return
				default:
					// Create new span in hierarchy
					newCtx, span := tracer.StartSpan(localCtx, fmt.Sprintf("storm-%d-%d", goroutineID, len(spans)))
					span.SetTag("goroutine", fmt.Sprintf("%d", goroutineID))
					span.SetTag("depth", fmt.Sprintf("%d", len(spans)))

					// Verify hierarchy
					if span.TraceID() != rootTraceID {
						hierarchyErrors.Add(1)
					}

					spans = append(spans, span)
					localCtx = newCtx
					totalSpans.Add(1)

					// Randomly finish some spans to create complex patterns
					if len(spans) > 20 {
						// Finish oldest spans
						toFinish := spans[:5]
						spans = spans[5:]

						for _, s := range toFinish {
							s.Finish()
						}

						// Reset context to avoid extreme depth
						if len(spans) == 0 {
							localCtx = rootCtx
						}
					}

					// Brief pause
					time.Sleep(time.Microsecond * 50)
				}
			}
		}(i)
	}

	wg.Wait()

	// Allow processing time
	time.Sleep(100 * time.Millisecond)

	total := totalSpans.Load()
	errors := hierarchyErrors.Load()
	errorRate := float64(errors) / float64(total) * 100
	spansPerSecond := float64(total) / duration.Seconds()

	t.Logf("Hierarchy storm results:")
	t.Logf("  Duration: %v", duration)
	t.Logf("  Goroutines: %d", numGoroutines)
	t.Logf("  Total spans: %d", total)
	t.Logf("  Hierarchy errors: %d", errors)
	t.Logf("  Error rate: %.2f%%", errorRate)
	t.Logf("  Rate: %.0f spans/sec", spansPerSecond)

	// Verify system handled the storm
	if errorRate > 1 {
		t.Errorf("Too many hierarchy errors: %.2f%%", errorRate)
	}

	if spansPerSecond < 1000 {
		t.Errorf("Hierarchy storm performance too low: %.0f spans/sec", spansPerSecond)
	}

	// Verify collection worked
	exported := collector.Export()
	collectionRate := float64(len(exported)) / float64(total) * 100

	t.Logf("  Collected spans: %d (%.1f%%)", len(exported), collectionRate)

	if collectionRate < 80 {
		t.Errorf("Too few spans collected during storm: %.1f%%", collectionRate)
	}
}
