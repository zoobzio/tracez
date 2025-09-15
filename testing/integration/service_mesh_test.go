package integration

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// TestServiceMeshCommunication demonstrates tracing across multiple services.
// in a mesh topology where services call each other.
func TestServiceMeshCommunication(t *testing.T) {
	// Create shared tracer for all services.
	tracer := tracez.New("service-mesh")
	collector := NewMockCollector(t, "mesh", 1000)
	tracer.AddCollector("mesh", collector.Collector)
	defer tracer.Close()

	// Create mesh services.
	api := NewMockService("api-gateway", tracer)
	auth := NewMockService("auth-service", tracer)
	catalog := NewMockService("catalog-service", tracer)
	inventory := NewMockService("inventory-service", tracer)
	payment := NewMockService("payment-service", tracer)

	// Configure latencies to simulate realistic network.
	api.SetLatency(5 * time.Millisecond)
	auth.SetLatency(15 * time.Millisecond)
	catalog.SetLatency(20 * time.Millisecond)
	inventory.SetLatency(10 * time.Millisecond)
	payment.SetLatency(30 * time.Millisecond)

	// Simulate a distributed transaction.
	ctx := context.Background()
	ctx, rootSpan := tracer.StartSpan(ctx, "checkout-flow")
	rootSpan.SetTag("flow", "checkout")
	rootSpan.SetTag("user_id", "user-123")

	// API Gateway receives request.
	err := api.Call(ctx, "receive-request")
	if err != nil {
		t.Fatalf("API call failed: %v", err)
	}

	// Authenticate user.
	err = auth.Call(ctx, "verify-token")
	if err != nil {
		t.Fatalf("Auth call failed: %v", err)
	}

	// Fetch catalog items in parallel.
	var wg sync.WaitGroup
	itemIDs := []string{"item-1", "item-2", "item-3"}

	for _, itemID := range itemIDs {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()

			// Each item lookup creates its own span.
			itemCtx, itemSpan := tracer.StartSpan(ctx, fmt.Sprintf("fetch-item-%s", id))
			itemSpan.SetTag("item_id", id)

			// Catalog lookup.
			catalog.Call(itemCtx, "get-item-details")

			// Inventory check.
			inventory.Call(itemCtx, "check-availability")

			itemSpan.Finish()
		}(itemID)
	}

	wg.Wait()

	// Process payment.
	err = payment.Call(ctx, "process-payment")
	if err != nil {
		t.Fatalf("Payment call failed: %v", err)
	}

	rootSpan.Finish()

	// Wait for all spans to be collected.
	expectedSpans := 1 + 1 + 1 + (len(itemIDs) * 3) + 1 // root + api + auth + (items * 3 ops) + payment.
	spans := collector.WaitForSpans(expectedSpans, 200*time.Millisecond)

	// Analyze the trace.
	analyzer := NewTraceAnalyzer(spans)

	// Verify span count.
	if analyzer.CountSpans() < expectedSpans {
		t.Errorf("Expected at least %d spans, got %d", expectedSpans, analyzer.CountSpans())
	}

	// Verify all spans share same trace ID.
	var traceID string
	for _, span := range spans {
		if traceID == "" {
			traceID = span.TraceID
		} else if span.TraceID != traceID {
			t.Errorf("Span %s has different trace ID: %s", span.Name, span.TraceID)
		}
	}

	// Verify service calls exist.
	services := []string{
		"api-gateway.receive-request",
		"auth-service.verify-token",
		"catalog-service.get-item-details",
		"inventory-service.check-availability",
		"payment-service.process-payment",
	}

	for _, service := range services {
		if analyzer.GetSpansByName(service) == nil {
			t.Errorf("Service span '%s' not found", service)
		}
	}

	// Verify parallel item fetches have same parent.
	for _, itemID := range itemIDs {
		itemSpans := analyzer.GetSpansByName(fmt.Sprintf("fetch-item-%s", itemID))
		if len(itemSpans) == 0 {
			t.Errorf("Item span for %s not found", itemID)
			continue
		}

		// Should be child of root span.
		itemSpan := itemSpans[0]
		if itemSpan.ParentID != rootSpan.SpanID() {
			t.Errorf("Item span %s not child of root", itemID)
		}
	}

	// Print trace tree for debugging.
	t.Logf("Service Mesh Trace Tree:\n%s", PrintSpanTree(analyzer.trees))
}

// TestDistributedCircuitBreaker demonstrates tracing when services fail.
// and circuit breakers activate.
func TestDistributedCircuitBreaker(t *testing.T) {
	tracer := tracez.New("resilient-system")
	collector := NewMockCollector(t, "circuit", 1000)
	tracer.AddCollector("mesh", collector.Collector)
	defer tracer.Close()

	// Create services with failure scenarios.
	stable := NewMockService("stable-service", tracer)
	flaky := NewMockService("flaky-service", tracer)
	flaky.SetFailureRate(0.6) // 60% failure rate.

	// Circuit breaker state.
	type CircuitState int
	const (
		Closed CircuitState = iota
		Open
		HalfOpen
	)

	circuitState := Closed
	failureCount := 0
	successCount := 0
	const threshold = 3

	// Simulate requests with circuit breaker.
	ctx := context.Background()

	for i := 0; i < 10; i++ {
		// Start request span.
		requestCtx, requestSpan := tracer.StartSpan(ctx, fmt.Sprintf("request-%d", i))
		requestSpan.SetTag("request_id", fmt.Sprintf("%d", i))
		requestSpan.SetTag("circuit_state", fmt.Sprintf("%v", circuitState))

		// Always call stable service.
		stable.Call(requestCtx, "process")

		// Check circuit breaker.
		switch circuitState {
		case Closed:
			// Try calling flaky service.
			err := flaky.Call(requestCtx, "risky-operation")
			if err != nil {
				failureCount++
				requestSpan.SetTag("error", "true")
				requestSpan.SetTag("error.type", "service_failure")

				if failureCount >= threshold {
					circuitState = Open
					requestSpan.SetTag("circuit_action", "opened")
				}
			} else {
				successCount++
				requestSpan.SetTag("success", "true")
			}

		case Open:
			// Circuit is open, skip flaky service.
			requestSpan.SetTag("circuit_skip", "true")
			requestSpan.SetTag("fallback", "true")

			// After some requests, try half-open.
			if i > 6 {
				circuitState = HalfOpen
				requestSpan.SetTag("circuit_action", "half-open")
			}

		case HalfOpen:
			// Try one request.
			err := flaky.Call(requestCtx, "test-recovery")
			if err != nil {
				circuitState = Open
				failureCount = threshold
				requestSpan.SetTag("circuit_action", "re-opened")
				requestSpan.SetTag("error", "true")
			} else {
				circuitState = Closed
				failureCount = 0
				requestSpan.SetTag("circuit_action", "closed")
				requestSpan.SetTag("recovered", "true")
			}
		}

		requestSpan.Finish()

		// Small delay between requests.
		time.Sleep(5 * time.Millisecond)
	}

	// Analyze circuit breaker behavior.
	spans := collector.Export()

	// Count circuit breaker actions.
	openCount := 0
	skipCount := 0
	fallbackCount := 0

	for _, span := range spans {
		if span.Tags["circuit_action"] == "opened" {
			openCount++
		}
		if span.Tags["circuit_skip"] == "true" {
			skipCount++
		}
		if span.Tags["fallback"] == "true" {
			fallbackCount++
		}
	}

	// Verify circuit breaker activated.
	if openCount == 0 {
		t.Error("Circuit breaker never opened despite failures")
	}

	if skipCount == 0 {
		t.Error("No requests were skipped when circuit was open")
	}

	t.Logf("Circuit breaker stats: opened=%d, skipped=%d, fallback=%d",
		openCount, skipCount, fallbackCount)
}

// TestSagaPattern demonstrates distributed transaction with compensations.
func TestSagaPattern(t *testing.T) {
	tracer := tracez.New("saga-coordinator")
	collector := NewMockCollector(t, "saga", 1000)
	tracer.AddCollector("mesh", collector.Collector)
	defer tracer.Close()

	// Create services for saga steps.
	reservation := NewMockService("reservation-service", tracer)
	payment := NewMockService("payment-service", tracer)
	notification := NewMockService("notification-service", tracer)

	// Make payment fail to trigger compensation.
	payment.SetFailureRate(1.0)

	ctx := context.Background()

	// Start saga transaction.
	ctx, sagaSpan := tracer.StartSpan(ctx, "booking-saga")
	sagaSpan.SetTag("saga_id", "saga-123")
	sagaSpan.SetTag("type", "hotel-booking")

	// Track completed steps for compensation.
	completedSteps := []string{}

	// Step 1: Reserve room.
	_, step1Span := tracer.StartSpan(ctx, "saga-step-1-reserve")
	step1Span.SetTag("step", "reserve-room")
	err := reservation.Call(ctx, "reserve-room")
	if err == nil {
		completedSteps = append(completedSteps, "reservation")
		step1Span.SetTag("status", "success")
	} else {
		step1Span.SetTag("status", "failed")
	}
	step1Span.Finish()

	// Step 2: Process payment (will fail).
	_, step2Span := tracer.StartSpan(ctx, "saga-step-2-payment")
	step2Span.SetTag("step", "process-payment")
	err = payment.Call(ctx, "charge-card")
	if err == nil {
		_ = append(completedSteps, "payment") // Operation tracked in test
		step2Span.SetTag("status", "success")
	} else {
		step2Span.SetTag("status", "failed")
		step2Span.SetTag("error", err.Error())

		// Trigger compensation.
		_, compensateSpan := tracer.StartSpan(ctx, "saga-compensation")
		compensateSpan.SetTag("reason", "payment-failed")

		// Compensate in reverse order.
		for i := len(completedSteps) - 1; i >= 0; i-- {
			step := completedSteps[i]
			_, compSpan := tracer.StartSpan(ctx, fmt.Sprintf("compensate-%s", step))
			compSpan.SetTag("compensation", step)

			switch step {
			case "reservation":
				reservation.Call(ctx, "cancel-reservation")
			case "notification":
				notification.Call(ctx, "send-cancellation")
			}

			compSpan.Finish()
		}

		compensateSpan.Finish()
	}
	step2Span.Finish()

	// Step 3: Send notification (skipped due to failure).
	if err == nil {
		_, step3Span := tracer.StartSpan(ctx, "saga-step-3-notify")
		step3Span.SetTag("step", "send-confirmation")
		notification.Call(ctx, "send-email")
		step3Span.Finish()
	}

	sagaSpan.SetTag("outcome", "compensated")
	sagaSpan.Finish()

	// Verify saga execution.
	spans := collector.Export()
	analyzer := NewTraceAnalyzer(spans)

	// Check for compensation spans.
	compensationSpans := analyzer.GetSpansByName("saga-compensation")
	if len(compensationSpans) == 0 {
		t.Error("No compensation span found despite failure")
	}

	// Verify compensation executed.
	cancelSpans := analyzer.GetSpansByName("compensate-reservation")
	if len(cancelSpans) == 0 {
		t.Error("Reservation was not compensated")
	}

	// Verify step 3 was skipped.
	step3Spans := analyzer.GetSpansByName("saga-step-3-notify")
	if len(step3Spans) > 0 {
		t.Error("Step 3 should not execute after step 2 failure")
	}

	t.Logf("Saga execution with compensation:\n%s", PrintSpanTree(analyzer.trees))
}

// TestEventSourcing demonstrates CQRS/Event Sourcing patterns with tracing.
func TestEventSourcing(t *testing.T) {
	tracer := tracez.New("event-store")
	collector := NewMockCollector(t, "events", 2000)
	tracer.AddCollector("mesh", collector.Collector)
	defer tracer.Close()

	// Simulate event sourcing components.
	commandHandler := NewMockService("command-handler", tracer)
	eventStore := NewMockService("event-store", tracer)
	projections := NewMockService("projections", tracer)
	readModel := NewMockService("read-model", tracer)

	ctx := context.Background()

	// Process a command that generates multiple events.
	ctx, cmdSpan := tracer.StartSpan(ctx, "process-command")
	cmdSpan.SetTag("command_type", "CreateOrder")
	cmdSpan.SetTag("aggregate_id", "order-456")

	// Validate command.
	commandHandler.Call(ctx, "validate-command")

	// Generate events.
	events := []string{"OrderCreated", "InventoryReserved", "PaymentRequested"}

	for _, eventType := range events {
		// Store each event.
		_, eventSpan := tracer.StartSpan(ctx, "store-event")
		eventSpan.SetTag("event_type", eventType)
		eventSpan.SetTag("sequence", fmt.Sprintf("%d", len(events)))

		// Persist to event store.
		eventStore.Call(ctx, "append-event")

		// Update projections asynchronously.
		go func(evt string) {
			projCtx, projSpan := tracer.StartSpan(ctx, "update-projection")
			projSpan.SetTag("projection", evt)
			projSpan.SetTag("async", "true")

			projections.Call(projCtx, fmt.Sprintf("project-%s", evt))
			readModel.Call(projCtx, "update-view")

			projSpan.Finish()
		}(eventType)

		eventSpan.Finish()
	}

	cmdSpan.Finish()

	// Query read model.
	time.Sleep(50 * time.Millisecond) // Wait for projections.

	queryCtx, querySpan := tracer.StartSpan(ctx, "query-read-model")
	querySpan.SetTag("query_type", "GetOrderStatus")
	readModel.Call(queryCtx, "fetch-order-view")
	querySpan.Finish()

	// Analyze event flow.
	time.Sleep(100 * time.Millisecond) // Ensure async ops complete.
	spans := collector.Export()
	analyzer := NewTraceAnalyzer(spans)

	// Verify events were stored.
	for _, eventType := range events {
		found := false
		for _, span := range spans {
			if span.Tags["event_type"] == eventType {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Event %s not found in trace", eventType)
		}
	}

	// Verify projections were updated.
	projectionSpans := analyzer.GetSpansByName("update-projection")
	if len(projectionSpans) < len(events) {
		t.Errorf("Expected %d projections, found %d", len(events), len(projectionSpans))
	}

	// Check async processing.
	asyncCount := 0
	for _, span := range spans {
		if span.Tags["async"] == "true" {
			asyncCount++
		}
	}
	if asyncCount == 0 {
		t.Error("No async processing detected")
	}

	t.Logf("Event sourcing flow with %d events and %d async projections",
		len(events), asyncCount)
}

// TestGracefulDegradation shows how services degrade under load.
func TestGracefulDegradation(t *testing.T) {
	tracer := tracez.New("load-test")
	collector := NewMockCollector(t, "degradation", 2000)
	tracer.AddCollector("mesh", collector.Collector)
	defer tracer.Close()

	// Services with different priorities.
	critical := NewMockService("critical-service", tracer)
	important := NewMockService("important-service", tracer)
	optional := NewMockService("optional-service", tracer)

	// Simulate increasing load.
	loadLevels := []struct {
		name        string
		concurrent  int
		degradation string
	}{
		{"normal", 5, "none"},
		{"high", 20, "optional"},
		{"critical", 50, "important"},
	}

	ctx := context.Background()

	for _, level := range loadLevels {
		// Mark load level.
		levelCtx, levelSpan := tracer.StartSpan(ctx, fmt.Sprintf("load-%s", level.name))
		levelSpan.SetTag("load_level", level.name)
		levelSpan.SetTag("concurrent_requests", fmt.Sprintf("%d", level.concurrent))

		var wg sync.WaitGroup
		for i := 0; i < level.concurrent; i++ {
			wg.Add(1)
			go func(reqNum int) {
				defer wg.Done()

				reqCtx, reqSpan := tracer.StartSpan(levelCtx, fmt.Sprintf("request-%s-%d", level.name, reqNum))
				reqSpan.SetTag("degradation_mode", level.degradation)

				// Always serve critical.
				critical.Call(reqCtx, "critical-operation")

				// Conditionally serve others based on load.
				switch level.degradation {
				case "none":
					important.Call(reqCtx, "important-operation")
					optional.Call(reqCtx, "optional-operation")
				case "optional":
					important.Call(reqCtx, "important-operation")
					reqSpan.SetTag("skipped", "optional")
				case "important":
					reqSpan.SetTag("skipped", "important,optional")
				}

				reqSpan.Finish()
			}(i)
		}

		wg.Wait()
		levelSpan.Finish()

		// Brief pause between load levels.
		time.Sleep(10 * time.Millisecond)
	}

	// Analyze degradation behavior.
	spans := collector.Export()

	// Count operations at each level.
	normalOps := 0
	degradedOps := 0
	criticalOnlyOps := 0

	for _, span := range spans {
		if span.Tags["degradation_mode"] == "none" {
			normalOps++
		}
		if span.Tags["skipped"] == "optional" {
			degradedOps++
		}
		if span.Tags["skipped"] == "important,optional" {
			criticalOnlyOps++
		}
	}

	// Verify degradation happened.
	if normalOps == 0 {
		t.Error("No normal operations recorded")
	}
	if degradedOps == 0 {
		t.Error("No degraded operations recorded")
	}
	if criticalOnlyOps == 0 {
		t.Error("No critical-only operations recorded")
	}

	t.Logf("Degradation stats: normal=%d, degraded=%d, critical-only=%d",
		normalOps, degradedOps, criticalOnlyOps)
}
