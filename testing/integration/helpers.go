package integration

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zoobzio/tracez"
)

// Package-level random source for test variability removed - unused.

// MockCollector wraps a real collector with test utilities.
// Provides synchronous collection and verification helpers.
//
//nolint:govet // Field alignment optimized for test helper readability
type MockCollector struct {
	exported []tracez.Span
	*tracez.Collector
	t  *testing.T
	mu sync.Mutex
}

// NewMockCollector creates a collector for testing.
func NewMockCollector(t *testing.T, name string, bufferSize int) *MockCollector {
	collector := tracez.NewCollector(name, bufferSize)
	collector.SetSyncMode(true) // Enable synchronous collection for testing.
	return &MockCollector{
		Collector: collector,
		t:         t,
		exported:  make([]tracez.Span, 0),
	}
}

// Export returns collected spans and clears the buffer.
func (m *MockCollector) Export() []tracez.Span {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get spans from underlying collector.
	spans := m.Collector.Export()
	m.exported = append(m.exported, spans...)
	return spans
}

// GetAll returns all exported spans without clearing.
func (m *MockCollector) GetAll() []tracez.Span {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Get current spans without clearing (peek into collector state).
	// This is a workaround since Export() clears the buffer.
	// We'll export, save them, then return all.
	current := m.Collector.Export()
	if len(current) > 0 {
		m.exported = append(m.exported, current...)
	}

	// Return copy of all exported spans.
	all := make([]tracez.Span, len(m.exported))
	copy(all, m.exported)
	return all
}

// WaitForSpans waits for expected number of spans with timeout.
func (m *MockCollector) WaitForSpans(expected int, timeout time.Duration) []tracez.Span {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()

	for time.Now().Before(deadline) {
		spans := m.Export()
		if len(spans) >= expected {
			return spans[:expected]
		}
		<-ticker.C
	}

	// Timeout - return what we have.
	spans := m.Export()
	m.t.Errorf("Timeout waiting for spans: expected %d, got %d", expected, len(spans))
	return spans
}

// AssertSpanCount verifies exact span count.
func (m *MockCollector) AssertSpanCount(expected int) {
	spans := m.Export()
	if len(spans) != expected {
		m.t.Errorf("Expected %d spans, got %d", expected, len(spans))
	}
}

// AssertSpanNamed checks if a span with given name exists.
func (m *MockCollector) AssertSpanNamed(name string) *tracez.Span {
	spans := m.GetAll()
	for i := range spans {
		span := &spans[i]
		if span.Name == name {
			return span
		}
	}
	m.t.Errorf("Span named '%s' not found", name)
	return nil
}

// AssertParentChild verifies parent-child relationship.
func (m *MockCollector) AssertParentChild(parentName, childName string) {
	spans := m.GetAll()
	var parent, child *tracez.Span

	for i := range spans {
		if spans[i].Name == parentName {
			parent = &spans[i]
		}
		if spans[i].Name == childName {
			child = &spans[i]
		}
	}

	if parent == nil {
		m.t.Errorf("Parent span '%s' not found", parentName)
		return
	}
	if child == nil {
		m.t.Errorf("Child span '%s' not found", childName)
		return
	}

	if child.ParentID != parent.SpanID {
		m.t.Errorf("Parent-child relationship broken: %s is not parent of %s. Child ParentID=%s, Parent SpanID=%s",
			parentName, childName, child.ParentID, parent.SpanID)
	}
	if child.TraceID != parent.TraceID {
		m.t.Errorf("Trace ID mismatch: parent=%s, child=%s", parent.TraceID, child.TraceID)
	}
}

// SpanTree represents a hierarchical view of spans.
type SpanTree struct {
	Span     tracez.Span
	Children []*SpanTree
}

// BuildSpanTree constructs a tree from flat span list.
func BuildSpanTree(spans []tracez.Span) []*SpanTree {
	nodeMap := make(map[string]*SpanTree)
	roots := make([]*SpanTree, 0)

	// Create nodes.
	for i := range spans {
		span := spans[i]
		nodeMap[span.SpanID] = &SpanTree{
			Span:     span,
			Children: make([]*SpanTree, 0),
		}
	}

	// Build relationships.
	for i := range spans {
		span := spans[i]
		node := nodeMap[span.SpanID]
		if span.ParentID == "" {
			roots = append(roots, node)
		} else if parent, exists := nodeMap[span.ParentID]; exists {
			parent.Children = append(parent.Children, node)
		}
	}

	return roots
}

// PrintSpanTree formats span tree for debugging.
func PrintSpanTree(trees []*SpanTree) string {
	var sb strings.Builder
	for _, tree := range trees {
		printTreeNode(&sb, tree, 0)
	}
	return sb.String()
}

func printTreeNode(sb *strings.Builder, node *SpanTree, depth int) {
	indent := strings.Repeat("  ", depth)
	fmt.Fprintf(sb, "%s%s (%.2fms)\n",
		indent, node.Span.Name, node.Span.Duration.Seconds()*1000)
	for _, child := range node.Children {
		printTreeNode(sb, child, depth+1)
	}
}

// TestScenario represents a reusable test case.
type TestScenario struct {
	Setup   func() (*tracez.Tracer, *MockCollector)
	Execute func(context.Context, *tracez.Tracer)
	Verify  func(*testing.T, []tracez.Span)
	Name    string
}

// Run executes the test scenario.
func (s *TestScenario) Run(t *testing.T) {
	t.Run(s.Name, func(t *testing.T) {
		// Setup.
		tracer, collector := s.Setup()
		if tracer == nil || collector == nil {
			// Use defaults if not provided.
			tracer = tracez.New("test-service")
			collector = NewMockCollector(t, "test", 1000)
			tracer.AddCollector("test", collector.Collector)
		}
		defer tracer.Close()

		// Execute.
		ctx := context.Background()
		s.Execute(ctx, tracer)

		// Wait for collection.
		time.Sleep(50 * time.Millisecond)

		// Verify.
		spans := collector.Export()
		s.Verify(t, spans)
	})
}

// MockService simulates an external service for integration testing.
type MockService struct {
	tracer       *tracez.Tracer
	name         string
	latency      time.Duration
	mu           sync.Mutex
	requestCount int
	failureRate  float32
}

// NewMockService creates a simulated service.
func NewMockService(name string, tracer *tracez.Tracer) *MockService {
	return &MockService{
		name:    name,
		latency: 10 * time.Millisecond,
		tracer:  tracer,
	}
}

// SetLatency configures response time.
func (m *MockService) SetLatency(d time.Duration) {
	m.mu.Lock()
	m.latency = d
	m.mu.Unlock()
}

// SetFailureRate configures error probability (0.0-1.0).
func (m *MockService) SetFailureRate(rate float32) {
	m.mu.Lock()
	m.failureRate = rate
	m.mu.Unlock()
}

// Call simulates a service call with tracing.
func (m *MockService) Call(ctx context.Context, operation string) error {
	m.mu.Lock()
	m.requestCount++
	count := m.requestCount
	latency := m.latency
	shouldFail := rand.Float32() < m.failureRate
	m.mu.Unlock()

	// Start span for service call.
	_, span := m.tracer.StartSpan(ctx, fmt.Sprintf("%s.%s", m.name, operation))
	defer span.Finish()

	span.SetTag("service", m.name)
	span.SetTag("operation", operation)
	span.SetTag("request_id", fmt.Sprintf("%d", count))

	// Simulate processing.
	time.Sleep(latency)

	if shouldFail {
		span.SetTag("error", "true")
		span.SetTag("error.message", "simulated failure")
		return fmt.Errorf("%s: simulated failure", m.name)
	}

	span.SetTag("success", "true")
	return nil
}

// SpanMatcher provides fluent assertions for spans.
type SpanMatcher struct {
	t    *testing.T
	span *tracez.Span
}

// NewSpanMatcher creates a matcher for span assertions.
func NewSpanMatcher(t *testing.T, span *tracez.Span) *SpanMatcher {
	return &SpanMatcher{t: t, span: span}
}

// HasTag verifies tag exists with value.
func (m *SpanMatcher) HasTag(key, value string) *SpanMatcher {
	if m.span == nil {
		return m
	}
	if actual, exists := m.span.Tags[key]; !exists {
		m.t.Errorf("Span %s missing tag '%s'", m.span.Name, key)
	} else if actual != value {
		m.t.Errorf("Span %s tag '%s': expected '%s', got '%s'",
			m.span.Name, key, value, actual)
	}
	return m
}

// HasParent verifies parent relationship.
func (m *SpanMatcher) HasParent(parentID string) *SpanMatcher {
	if m.span == nil {
		return m
	}
	if m.span.ParentID != parentID {
		m.t.Errorf("Span %s wrong parent: expected %s, got %s",
			m.span.Name, parentID, m.span.ParentID)
	}
	return m
}

// DurationBetween verifies duration is in range.
func (m *SpanMatcher) DurationBetween(minDur, maxDur time.Duration) *SpanMatcher {
	if m.span == nil {
		return m
	}
	if m.span.Duration < minDur || m.span.Duration > maxDur {
		m.t.Errorf("Span %s duration %v not in range [%v, %v]",
			m.span.Name, m.span.Duration, minDur, maxDur)
	}
	return m
}

// TraceAnalyzer provides trace-level assertions.
type TraceAnalyzer struct {
	spans  []tracez.Span
	byID   map[string]tracez.Span
	byName map[string][]tracez.Span
	trees  []*SpanTree
}

// NewTraceAnalyzer creates an analyzer for a set of spans.
func NewTraceAnalyzer(spans []tracez.Span) *TraceAnalyzer {
	a := &TraceAnalyzer{
		spans:  spans,
		byID:   make(map[string]tracez.Span),
		byName: make(map[string][]tracez.Span),
	}

	for i := range spans {
		span := spans[i]
		a.byID[span.SpanID] = span
		a.byName[span.Name] = append(a.byName[span.Name], span)
	}

	a.trees = BuildSpanTree(spans)
	return a
}

// GetSpan retrieves span by ID.
func (a *TraceAnalyzer) GetSpan(spanID string) (tracez.Span, bool) {
	span, exists := a.byID[spanID]
	return span, exists
}

// GetSpansByName retrieves all spans with given name.
func (a *TraceAnalyzer) GetSpansByName(name string) []tracez.Span {
	return a.byName[name]
}

// CountSpans returns total span count.
func (a *TraceAnalyzer) CountSpans() int {
	return len(a.spans)
}

// CountTrees returns number of root spans.
func (a *TraceAnalyzer) CountTrees() int {
	return len(a.trees)
}

// VerifyChain checks if spans form a valid parent-child chain.
func (a *TraceAnalyzer) VerifyChain(names ...string) error {
	if len(names) < 2 {
		return fmt.Errorf("chain requires at least 2 spans")
	}

	var prevSpan *tracez.Span
	for i, name := range names {
		spans := a.GetSpansByName(name)
		if len(spans) == 0 {
			return fmt.Errorf("span '%s' not found", name)
		}

		// For simplicity, use first match.
		span := spans[0]

		if i > 0 && prevSpan != nil {
			if span.ParentID != prevSpan.SpanID {
				return fmt.Errorf("broken chain: %s is not child of %s", name, names[i-1])
			}
		}

		prevSpan = &span
	}

	return nil
}

// GetCriticalPath returns the longest duration path through the trace.
func (a *TraceAnalyzer) GetCriticalPath() []tracez.Span {
	if len(a.trees) == 0 {
		return nil
	}

	// Find path with maximum total duration.
	var maxPath []tracez.Span
	var maxDuration time.Duration

	for _, tree := range a.trees {
		path := a.findLongestPath(tree)
		duration := a.pathDuration(path)
		if duration > maxDuration {
			maxDuration = duration
			maxPath = path
		}
	}

	return maxPath
}

func (a *TraceAnalyzer) findLongestPath(node *SpanTree) []tracez.Span {
	path := []tracez.Span{node.Span}

	if len(node.Children) == 0 {
		return path
	}

	// Find child with longest path.
	var longestChild []tracez.Span
	var longestDuration time.Duration

	for _, child := range node.Children {
		childPath := a.findLongestPath(child)
		duration := a.pathDuration(childPath)
		if duration > longestDuration {
			longestDuration = duration
			longestChild = childPath
		}
	}

	return append(path, longestChild...)
}

func (*TraceAnalyzer) pathDuration(path []tracez.Span) time.Duration {
	var total time.Duration
	for i := range path {
		total += path[i].Duration
	}
	return total
}
