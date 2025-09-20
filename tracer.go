package tracez

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"runtime"
	"sync"
	"time"

	"github.com/zoobzio/clockz"
)

// contextBundle holds both tracer and span to reduce context allocations.
type contextBundle struct {
	tracer *Tracer
	span   *Span
}

// Tracer manages span lifecycle and collection.
// Safe for concurrent use by multiple goroutines.
type Tracer struct {
	collectors  map[string]*Collector
	traceIDPool *IDPool
	spanIDPool  *IDPool
	clock       clockz.Clock
	mu          sync.Mutex
	idPoolOnce  sync.Once
}

// New creates a new tracer.
// Uses the real clock for production behavior.
func New() *Tracer {
	return &Tracer{
		collectors: make(map[string]*Collector),
		clock:      clockz.RealClock,
	}
}

// WithClock returns a new tracer with the specified clock.
// Enables clock injection for deterministic testing.
func (*Tracer) WithClock(clock clockz.Clock) *Tracer {
	return &Tracer{
		collectors: make(map[string]*Collector),
		clock:      clock,
	}
}

// ensureIDPools initializes ID pools if not already created.
func (t *Tracer) ensureIDPools() {
	t.idPoolOnce.Do(func() {
		// Pool size based on number of CPUs for optimal contention balance.
		poolSize := runtime.NumCPU() * 100

		t.traceIDPool = NewIDPool(poolSize, func() string {
			bytes := make([]byte, 16)
			if _, err := rand.Read(bytes); err != nil {
				// Fallback to time-based ID if crypto/rand fails.
				return hex.EncodeToString([]byte(t.clock.Now().Format(time.RFC3339Nano)))
			}
			return hex.EncodeToString(bytes)
		})

		t.spanIDPool = NewIDPool(poolSize, func() string {
			bytes := make([]byte, 8)
			if _, err := rand.Read(bytes); err != nil {
				// Fallback to time-based ID if crypto/rand fails.
				return hex.EncodeToString([]byte(t.clock.Now().Format("15:04:05.000000")))
			}
			return hex.EncodeToString(bytes)
		})
	})
}

// AddCollector registers a new collector with the tracer.
// Users must track collector names themselves if needed.
func (t *Tracer) AddCollector(name string, collector *Collector) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.collectors[name] = collector
}

// StartSpan creates a new span and returns it wrapped in an ActiveSpan.
// If the context contains an existing span, the new span will be its child.
func (t *Tracer) StartSpan(ctx context.Context, operation Key) (context.Context, *ActiveSpan) {
	// Handle nil context by creating a new one.
	if ctx == nil {
		ctx = context.Background()
	}

	span := &Span{
		TraceID:   t.generateTraceID(ctx),
		SpanID:    t.generateSpanID(),
		Name:      operation,
		StartTime: t.clock.Now(),
	}

	// Link to parent span if present.
	if parentSpan := GetSpan(ctx); parentSpan != nil {
		span.TraceID = parentSpan.TraceID
		span.ParentID = parentSpan.SpanID
	}

	activeSpan := &ActiveSpan{
		span:   span,
		tracer: t,
	}

	// Create new context with bundled tracer and span (single allocation optimization).
	bundle := &contextBundle{tracer: t, span: span}
	newCtx := context.WithValue(ctx, bundleKey, bundle)

	return newCtx, activeSpan
}

// collectSpan distributes a completed span to all registered collectors.
func (t *Tracer) collectSpan(span *Span) {
	t.mu.Lock()
	collectors := make([]*Collector, 0, len(t.collectors))
	for _, collector := range t.collectors {
		collectors = append(collectors, collector)
	}
	t.mu.Unlock()

	// Send to collectors without holding the lock.
	for _, collector := range collectors {
		collector.Collect(span)
	}
}

// Reset clears all collectors' buffers without destroying them.
// The collectors remain registered and their goroutines continue running.
func (t *Tracer) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Reset all collectors (clear buffers and dropped counts).
	for _, collector := range t.collectors {
		collector.Reset()
	}
}

// Close shuts down all collectors gracefully and cleans up ID pools.
// This should be called when the tracer is no longer needed.
func (t *Tracer) Close() {
	// Close ID pools first to stop background goroutines.
	if t.traceIDPool != nil {
		t.traceIDPool.Close()
	}
	if t.spanIDPool != nil {
		t.spanIDPool.Close()
	}

	// Close all collectors to stop their goroutines.
	t.mu.Lock()
	for _, collector := range t.collectors {
		collector.close()
	}
	t.mu.Unlock()

	t.Reset()
}

// generateTraceID creates a new trace ID or returns the existing one from context.
func (t *Tracer) generateTraceID(ctx context.Context) string {
	if parentSpan := GetSpan(ctx); parentSpan != nil {
		return parentSpan.TraceID
	}

	// Use ID pool for performance optimization.
	t.ensureIDPools()
	return t.traceIDPool.Get()
}

// generateSpanID creates a new span ID using the optimized ID pool.
func (t *Tracer) generateSpanID() string {
	// Use ID pool for performance optimization.
	t.ensureIDPools()
	return t.spanIDPool.Get()
}
