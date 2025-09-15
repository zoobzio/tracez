package tracez

import (
	"context"
	"sync"
	"time"
)

// bundleKeyType is a private type for context keys to avoid collisions.
type bundleKeyType string

const (
	bundleKey bundleKeyType = "tracez"
)

// Span represents a single unit of work in a distributed trace.
// Spans are NOT thread-safe - do not modify from multiple goroutines.
//
//nolint:govet // Field alignment optimized for JSON serialization order
type Span struct {
	Tags      map[Tag]string `json:"tags,omitempty"`
	StartTime time.Time      `json:"start_time"`
	EndTime   time.Time      `json:"end_time,omitempty"`
	Duration  time.Duration  `json:"duration"`
	TraceID   string         `json:"trace_id"`
	SpanID    string         `json:"span_id"`
	ParentID  string         `json:"parent_id,omitempty"`
	Name      string         `json:"name"`
}

// ActiveSpan wraps a Span with thread-safe tag operations and lifecycle management.
// Safe for concurrent use by multiple goroutines.
type ActiveSpan struct {
	span   *Span
	tracer *Tracer
	mu     sync.Mutex // Protects Tags map from concurrent writes.
}

// SetTag adds a key-value pair to the span.
// Thread-safe for concurrent access.
// No-op if span is already finished.
func (a *ActiveSpan) SetTag(key Tag, value string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Don't modify finished spans.
	if !a.span.EndTime.IsZero() {
		return
	}

	if a.span.Tags == nil {
		a.span.Tags = make(map[Tag]string)
	}
	a.span.Tags[key] = value
}

// GetTag retrieves a tag value by key.
// Thread-safe for concurrent access.
func (a *ActiveSpan) GetTag(key Tag) (string, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.span.Tags == nil {
		return "", false
	}
	value, ok := a.span.Tags[key]
	return value, ok
}

// Finish completes the span and sends it to the tracer for collection.
// Safe to call multiple times - subsequent calls are no-ops.
func (a *ActiveSpan) Finish() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Prevent double-finishing.
	if !a.span.EndTime.IsZero() {
		return
	}

	a.span.EndTime = time.Now()
	a.span.Duration = a.span.EndTime.Sub(a.span.StartTime)

	// Send to tracer for collection.
	a.tracer.collectSpan(a.span)
}

// TraceID returns the trace ID of this span.
// Thread-safe for concurrent access.
func (a *ActiveSpan) TraceID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.span.TraceID
}

// SpanID returns the span ID of this span.
// Thread-safe for concurrent access.
func (a *ActiveSpan) SpanID() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.span.SpanID
}

// Context creates a new context with this span embedded.
// The returned context can be used to start child spans.
func (a *ActiveSpan) Context(parent context.Context) context.Context {
	// Use bundled approach for performance optimization.
	bundle := &contextBundle{tracer: a.tracer, span: a.span}
	return context.WithValue(parent, bundleKey, bundle)
}

// GetSpan extracts the current span from a context.
// Returns nil if no span is present.
func GetSpan(ctx context.Context) *Span {
	if ctx == nil {
		return nil
	}

	if bundle, ok := ctx.Value(bundleKey).(*contextBundle); ok {
		return bundle.span
	}

	return nil
}
