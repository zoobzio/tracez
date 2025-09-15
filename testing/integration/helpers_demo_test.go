package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// TestHelpersDemo demonstrates all helper functions working together.
func TestHelpersDemo(t *testing.T) {
	// Test scenario demonstrates e-commerce checkout flow.
	scenario := TestScenario{
		Name: "E-commerce Checkout with Circuit Breaker",
		Setup: func() (*tracez.Tracer, *MockCollector) {
			tracer := tracez.New("ecommerce-system")
			collector := NewMockCollector(t, "checkout", 1000)
			tracer.AddCollector("mock", collector.Collector)
			return tracer, collector
		},
		Execute: func(ctx context.Context, tracer *tracez.Tracer) {
			// Create mock services.
			userService := NewMockService("user-service", tracer)
			inventoryService := NewMockService("inventory-service", tracer)
			paymentService := NewMockService("payment-service", tracer)
			shippingService := NewMockService("shipping-service", tracer)

			// Configure payment service to be flaky.
			paymentService.SetFailureRate(0.3)
			paymentService.SetLatency(50 * time.Millisecond)

			// Start checkout process.
			ctx, checkoutSpan := tracer.StartSpan(ctx, "checkout-process")
			checkoutSpan.SetTag("user_id", "user-12345")
			checkoutSpan.SetTag("cart_items", "3")
			checkoutSpan.SetTag("total", "$127.99")

			// Step 1: Validate user.
			userService.Call(ctx, "validate-user")

			// Step 2: Check inventory.
			inventoryService.Call(ctx, "reserve-items")

			// Step 3: Process payment with retry logic.
			var paymentErr error
			for attempt := 0; attempt < 3; attempt++ {
				_, attemptSpan := tracer.StartSpan(ctx, fmt.Sprintf("payment-attempt-%d", attempt+1))
				attemptSpan.SetTag("attempt", fmt.Sprintf("%d", attempt+1))

				paymentErr = paymentService.Call(ctx, "process-payment")
				if paymentErr == nil {
					attemptSpan.SetTag("result", "success")
					attemptSpan.Finish()
					break
				}

				attemptSpan.SetTag("result", "failed")
				attemptSpan.SetTag("error", paymentErr.Error())
				attemptSpan.Finish()

				// Exponential backoff.
				if attempt < 2 {
					backoffDelay := time.Duration(100*(attempt+1)) * time.Millisecond
					time.Sleep(backoffDelay)
				}
			}

			if paymentErr == nil {
				// Step 4: Arrange shipping (only if payment succeeded).
				shippingService.Call(ctx, "create-shipment")
				checkoutSpan.SetTag("status", "completed")
			} else {
				checkoutSpan.SetTag("status", "failed")
				checkoutSpan.SetTag("failure_reason", "payment_failed")
			}

			checkoutSpan.Finish()
		},
		Verify: func(t *testing.T, spans []tracez.Span) {
			// Use TraceAnalyzer to verify the flow.
			analyzer := NewTraceAnalyzer(spans)

			// Should have checkout root span.
			checkoutSpans := analyzer.GetSpansByName("checkout-process")
			if len(checkoutSpans) != 1 {
				t.Errorf("Expected 1 checkout span, got %d", len(checkoutSpans))
				return
			}

			checkout := checkoutSpans[0]

			// Use SpanMatcher for fluent assertions.
			checkoutMatcher := NewSpanMatcher(t, &checkout)
			checkoutMatcher.
				HasTag("user_id", "user-12345").
				HasTag("cart_items", "3").
				DurationBetween(50*time.Millisecond, 500*time.Millisecond)

			// Verify service calls exist.
			services := []string{
				"user-service.validate-user",
				"inventory-service.reserve-items",
				"payment-service.process-payment",
			}

			for _, service := range services {
				serviceSpans := analyzer.GetSpansByName(service)
				if len(serviceSpans) == 0 {
					t.Errorf("Service call '%s' not found", service)
				}
			}

			// Check for payment retry attempts.
			attemptCount := 0
			for i := 1; i <= 3; i++ {
				attemptSpans := analyzer.GetSpansByName(fmt.Sprintf("payment-attempt-%d", i))
				if len(attemptSpans) > 0 {
					attemptCount++
				}
			}

			if attemptCount == 0 {
				t.Error("No payment attempts found")
			}

			// Verify tree structure.
			if analyzer.CountTrees() != 1 {
				t.Errorf("Expected 1 trace tree, got %d", analyzer.CountTrees())
			}

			// Get critical path.
			criticalPath := analyzer.GetCriticalPath()
			if len(criticalPath) == 0 {
				t.Error("No critical path found")
			}

			t.Logf("Checkout flow analysis:")
			t.Logf("  Total spans: %d", analyzer.CountSpans())
			t.Logf("  Payment attempts: %d", attemptCount)
			t.Logf("  Critical path length: %d spans", len(criticalPath))
			t.Logf("  Checkout status: %s", checkout.Tags["status"])
		},
	}

	// Run the scenario.
	scenario.Run(t)
}

// TestMockServiceCapabilities demonstrates MockService features.
func TestMockServiceCapabilities(t *testing.T) {
	tracer := tracez.New("service-test")
	collector := NewMockCollector(t, "mock", 1000)
	tracer.AddCollector("mock", collector.Collector)
	defer tracer.Close()

	// Create a mock service.
	service := NewMockService("test-api", tracer)

	// Configure service behavior.
	service.SetLatency(25 * time.Millisecond)
	service.SetFailureRate(0.2) // 20% failure rate.

	ctx := context.Background()

	// Make multiple calls to test behavior.
	// With 20% failure rate, we need enough calls to ensure both success and failure.
	callCount := 50 // Increased for statistical reliability.
	successCount := 0
	errorCount := 0

	for i := 0; i < callCount; i++ {
		err := service.Call(ctx, fmt.Sprintf("operation-%d", i))
		if err == nil {
			successCount++
		} else {
			errorCount++
		}
	}

	// Verify behavior - with 50 calls and 20% failure rate:.
	// Expected ~40 success, ~10 failures.
	// Allow reasonable variance but detect complete failure.
	if successCount < 10 {
		t.Errorf("Too few successful calls: %d/%d (expected ~80%%)", successCount, callCount)
	}

	if errorCount < 2 {
		t.Errorf("Too few failed calls: %d/%d (expected ~20%%)", errorCount, callCount)
	}

	// Verify spans were created.
	spans := collector.Export()
	if len(spans) != callCount {
		t.Errorf("Expected %d spans, got %d", callCount, len(spans))
	}

	// Verify service tags.
	for _, span := range spans {
		if span.Tags["service"] != "test-api" {
			t.Errorf("Wrong service tag: expected 'test-api', got '%s'", span.Tags["service"])
		}

		// Check request ID increments.
		requestID := span.Tags["request_id"]
		if requestID == "" {
			t.Error("Missing request_id tag")
		}
	}

	t.Logf("Mock service test: %d calls, %d success, %d errors",
		callCount, successCount, errorCount)
}

// TestCollectorHelpers demonstrates MockCollector capabilities.
func TestCollectorHelpers(t *testing.T) {
	tracer := tracez.New("collector-test")
	collector := NewMockCollector(t, "helpers", 1000)
	tracer.AddCollector("mock", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Create some spans.
	_, span1 := tracer.StartSpan(ctx, "operation-1")
	span1.SetTag("priority", "high")
	span1.Finish()

	_, span2 := tracer.StartSpan(ctx, "operation-2")
	span2.SetTag("priority", "low")
	span2.Finish()

	// Test WaitForSpans.
	spans := collector.WaitForSpans(2, 100*time.Millisecond)
	if len(spans) != 2 {
		t.Errorf("WaitForSpans: expected 2 spans, got %d", len(spans))
	}

	// Test AssertSpanNamed.
	foundSpan := collector.AssertSpanNamed("operation-1")
	if foundSpan == nil {
		t.Error("AssertSpanNamed failed to find span")
	} else if foundSpan.Tags["priority"] != "high" {
		t.Error("Found span has wrong tag value")
	}

	// Create parent-child relationship.
	parentCtx, parentSpan := tracer.StartSpan(ctx, "parent-operation")
	_, childSpan := tracer.StartSpan(parentCtx, "child-operation")
	childSpan.Finish()
	parentSpan.Finish()

	// Give time for spans to be collected.
	time.Sleep(50 * time.Millisecond)

	// Test parent-child assertion.
	collector.AssertParentChild("parent-operation", "child-operation")

	t.Log("MockCollector helper tests passed")
}

// TestSpanTreeVisualization demonstrates tree building and printing.
func TestSpanTreeVisualization(t *testing.T) {
	tracer := tracez.New("tree-test")
	collector := NewMockCollector(t, "tree", 1000)
	tracer.AddCollector("mock", collector.Collector)
	defer tracer.Close()

	ctx := context.Background()

	// Create a complex tree structure.
	rootCtx, rootSpan := tracer.StartSpan(ctx, "web-request")
	rootSpan.SetTag("endpoint", "/api/orders")

	// Database operations.
	dbCtx, dbSpan := tracer.StartSpan(rootCtx, "database-transaction")

	_, querySpan1 := tracer.StartSpan(dbCtx, "query-users")
	time.Sleep(5 * time.Millisecond)
	querySpan1.Finish()

	_, querySpan2 := tracer.StartSpan(dbCtx, "query-orders")
	time.Sleep(8 * time.Millisecond)
	querySpan2.Finish()

	dbSpan.Finish()

	// External API call.
	_, apiSpan := tracer.StartSpan(rootCtx, "external-api-call")
	apiSpan.SetTag("service", "payment-gateway")
	time.Sleep(20 * time.Millisecond)
	apiSpan.Finish()

	rootSpan.Finish()

	// Build and visualize tree.
	spans := collector.Export()
	trees := BuildSpanTree(spans)

	if len(trees) != 1 {
		t.Errorf("Expected 1 tree root, got %d", len(trees))
	}

	treeStr := PrintSpanTree(trees)
	if treeStr == "" {
		t.Error("Tree string is empty")
	}

	// Tree should show hierarchy.
	expectedLines := []string{
		"web-request",
		"  database-transaction",
		"    query-users",
		"    query-orders",
		"  external-api-call",
	}

	for _, expectedLine := range expectedLines {
		if !containsLine(treeStr, expectedLine) {
			t.Errorf("Tree missing expected line: %s", expectedLine)
		}
	}

	t.Logf("Span tree visualization:\n%s", treeStr)
}

// Helper function to check if tree string contains expected line.
func containsLine(treeStr, expectedLine string) bool {
	lines := splitLines(treeStr)
	for _, line := range lines {
		// Remove timing info and compare structure.
		if containsStructure(line, expectedLine) {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func containsStructure(line, expected string) bool {
	// Simple check: does the line contain the expected span name with similar indentation?.
	if line == "" {
		return false
	}

	// Count leading spaces in both.
	lineSpaces := 0
	for _, r := range line {
		if r == ' ' {
			lineSpaces++
		} else {
			break
		}
	}

	expectedSpaces := 0
	for _, r := range expected {
		if r == ' ' {
			expectedSpaces++
		} else {
			break
		}
	}

	// Check if spaces match and span name appears.
	spanName := trimSpaces(expected[expectedSpaces:])
	return lineSpaces == expectedSpaces && containsSpanName(line[lineSpaces:], spanName)
}

func containsSpanName(line, spanName string) bool {
	// Look for span name in the line (before timing info).
	parenIndex := -1
	for i, r := range line {
		if r == '(' {
			parenIndex = i
			break
		}
	}

	searchIn := line
	if parenIndex >= 0 {
		searchIn = line[:parenIndex]
	}

	// Trim trailing spaces and check if it ends with span name.
	searchIn = trimSpaces(searchIn)
	return searchIn == spanName
}

func trimSpaces(s string) string {
	// Trim trailing spaces.
	end := len(s)
	for end > 0 && s[end-1] == ' ' {
		end--
	}
	return s[:end]
}
