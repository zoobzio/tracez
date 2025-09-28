package tracez

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zoobzio/clockz"
)

// contextBundle holds both tracer and span to reduce context allocations.
type contextBundle struct {
	tracer *Tracer
	span   *Span
}

// SpanHandler is called when a span completes.
type SpanHandler func(span Span)

type handlerEntry struct {
	handler SpanHandler
	id      uint64
	async   bool
}

// Tracer manages span lifecycle and collection.
// Safe for concurrent use by multiple goroutines.
//
//nolint:govet // Field order optimized for functionality over memory
type Tracer struct {
	handlers     []handlerEntry
	panicHook    func(handlerID uint64, r interface{})
	workers      *workerPool
	traceIDPool  *IDPool
	spanIDPool   *IDPool
	clock        clockz.Clock
	handlersLock sync.RWMutex
	idPoolOnce   sync.Once
	nextID       atomic.Uint64
	droppedSpans atomic.Uint64
}

// New creates a new tracer.
// Uses the real clock for production behavior.
func New() *Tracer {
	return &Tracer{
		handlers: make([]handlerEntry, 0),
		clock:    clockz.RealClock,
	}
}

// WithClock returns a new tracer with the specified clock.
// Enables clock injection for deterministic testing.
func (*Tracer) WithClock(clock clockz.Clock) *Tracer {
	return &Tracer{
		handlers: make([]handlerEntry, 0),
		clock:    clock,
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

// OnSpanComplete registers a synchronous handler called when spans complete.
func (t *Tracer) OnSpanComplete(handler SpanHandler) uint64 {
	return t.registerHandler(handler, false)
}

// OnSpanCompleteAsync registers an asynchronous handler called when spans complete.
func (t *Tracer) OnSpanCompleteAsync(handler SpanHandler) uint64 {
	return t.registerHandler(handler, true)
}

func (t *Tracer) registerHandler(handler SpanHandler, async bool) uint64 {
	if handler == nil {
		return 0
	}

	id := t.nextID.Add(1)

	t.handlersLock.Lock()
	defer t.handlersLock.Unlock()

	t.handlers = append(t.handlers, handlerEntry{
		id:      id,
		handler: handler,
		async:   async,
	})

	return id
}

// RemoveHandler removes a handler by ID.
func (t *Tracer) RemoveHandler(id uint64) {
	t.handlersLock.Lock()
	defer t.handlersLock.Unlock()

	// Preserve order
	for i, h := range t.handlers {
		if h.id == id {
			copy(t.handlers[i:], t.handlers[i+1:])
			t.handlers = t.handlers[:len(t.handlers)-1]
			return
		}
	}
}

// SetPanicHook sets a function to be called when a handler panics.
func (t *Tracer) SetPanicHook(hook func(handlerID uint64, r interface{})) {
	t.panicHook = hook
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
		Name:      string(operation),
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

// executeHandlers calls all registered handlers with the completed span.
func (t *Tracer) executeHandlers(span Span) {
	t.handlersLock.RLock()
	if len(t.handlers) == 0 {
		t.handlersLock.RUnlock()
		return
	}

	handlers := make([]handlerEntry, len(t.handlers))
	copy(handlers, t.handlers)
	t.handlersLock.RUnlock()

	for _, h := range handlers {
		if h.async {
			// Make a copy of h for closure
			entry := h
			if t.workers != nil {
				t.workers.submit(func() {
					t.safeCall(entry, span)
				})
			} else {
				go t.safeCall(entry, span)
			}
		} else {
			t.safeCall(h, span)
		}
	}
}

func (t *Tracer) safeCall(entry handlerEntry, span Span) {
	defer func() {
		if r := recover(); r != nil {
			if t.panicHook != nil {
				t.panicHook(entry.id, r)
			}
		}
	}()
	entry.handler(span)
}

// EnableWorkerPool creates a bounded worker pool for async handlers.
func (t *Tracer) EnableWorkerPool(workers, queueSize int) error {
	if t.workers != nil {
		return errors.New("worker pool already enabled")
	}
	if workers <= 0 {
		return errors.New("workers must be > 0")
	}
	if queueSize <= 0 {
		return errors.New("queueSize must be > 0")
	}

	t.workers = &workerPool{
		tasks:   make(chan func(), queueSize),
		stop:    make(chan struct{}),
		dropped: &t.droppedSpans,
	}

	t.workers.wg.Add(workers)
	for i := 0; i < workers; i++ {
		go t.workers.run()
	}

	return nil
}

// DroppedSpans returns the number of spans dropped due to full worker queue.
func (t *Tracer) DroppedSpans() uint64 {
	return t.droppedSpans.Load()
}

// Close shuts down the tracer gracefully and cleans up resources.
// This should be called when the tracer is no longer needed.
func (t *Tracer) Close() {
	// Stop new handler executions
	t.handlersLock.Lock()
	t.handlers = nil
	t.handlersLock.Unlock()

	// Wait for in-flight async tasks
	if t.workers != nil {
		t.workers.shutdown()
		t.workers = nil
	}

	// Close ID pools
	if t.traceIDPool != nil {
		t.traceIDPool.Close()
	}
	if t.spanIDPool != nil {
		t.spanIDPool.Close()
	}
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

// workerPool manages a fixed number of workers for processing async handlers.
//
//nolint:govet // Field order optimized for functionality over memory
type workerPool struct {
	tasks   chan func()
	stop    chan struct{}
	dropped *atomic.Uint64
	wg      sync.WaitGroup
}

func (w *workerPool) run() {
	defer w.wg.Done()
	for {
		select {
		case task := <-w.tasks:
			task()
		case <-w.stop:
			return
		}
	}
}

func (w *workerPool) submit(task func()) {
	select {
	case w.tasks <- task:
	default:
		w.dropped.Add(1)
	}
}

func (w *workerPool) shutdown() {
	close(w.stop)
	w.wg.Wait()
}
