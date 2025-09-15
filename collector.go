package tracez

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector buffers completed spans for batch export.
// Safe for concurrent use by multiple goroutines.
//
//nolint:govet // Field alignment optimized for readability over memory efficiency
type Collector struct {
	spans        []Span
	spansCh      chan Span
	stopCh       chan struct{}
	done         chan struct{}
	droppedCount atomic.Int64
	name         string
	mu           sync.Mutex
	closed       atomic.Bool // Track if collector is closed.
	syncMode     bool        // Bypass channel for synchronous collection.
}

// NewCollector creates a new collector with the specified name and buffer size.
func NewCollector(name string, bufferSize int) *Collector {
	c := &Collector{
		name:    name,
		spans:   make([]Span, 0, 8), // Start with small capacity.
		spansCh: make(chan Span, bufferSize),
		stopCh:  make(chan struct{}),
		done:    make(chan struct{}),
	}
	go c.start()
	return c
}

// start runs the collector's main loop, receiving spans from the channel.
func (c *Collector) start() {
	defer close(c.done)

	for {
		select {
		case <-c.stopCh:
			// Drain remaining spans before shutdown.
			for {
				select {
				case span := <-c.spansCh:
					c.bufferSpanUnsafe(&span)
				default:
					return // Clean shutdown.
				}
			}
		case span := <-c.spansCh:
			c.bufferSpanUnsafe(&span)
		}
	}
}

// close shuts down the collector gracefully.
func (c *Collector) close() {
	c.closed.Store(true)
	close(c.stopCh)
	select {
	case <-c.done:
		// Clean shutdown completed.
	case <-time.After(100 * time.Millisecond):
		// Timeout - log warning but continue.
		// In production, you might want to log this.
	}
}

// Collect attempts to buffer a span with backpressure protection.
// If the internal channel is full, the span is dropped and the drop counter is incremented.
// In sync mode, spans are collected directly for deterministic testing.
func (c *Collector) Collect(span *Span) {
	// Nil check to prevent panic in calling goroutine.
	if span == nil {
		// Drop nil spans to prevent system crash.
		c.droppedCount.Add(1)
		return
	}

	// Create a deep copy to prevent modifications after collection.
	spanCopy := *span
	if span.Tags != nil {
		spanCopy.Tags = make(map[Tag]string, len(span.Tags))
		for k, v := range span.Tags {
			spanCopy.Tags[k] = v
		}
	}

	if c.syncMode {
		// Direct synchronous collection for tests.
		if c.closed.Load() {
			// Collector is closed - drop span.
			c.droppedCount.Add(1)
			return
		}
		c.bufferSpanSafe(&spanCopy)
		return
	}

	select {
	case c.spansCh <- spanCopy:
		// Successfully queued.
	default:
		// Channel full - drop span to prevent blocking.
		c.droppedCount.Add(1)
	}
}

// bufferSpanUnsafe adds a span to the internal buffer.
// Must be called from the collector goroutine only.
func (c *Collector) bufferSpanUnsafe(span *Span) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if buffer needs to grow - optimized growth strategy.
	if len(c.spans) >= cap(c.spans) {
		// More aggressive growth for better performance under load.
		currentCap := cap(c.spans)
		var newCap int
		if currentCap < 1024 {
			// Double capacity for small buffers.
			newCap = currentCap * 2
		} else {
			// Grow by 50% for large buffers to avoid excessive memory usage.
			newCap = currentCap + currentCap/2
		}
		if newCap < 32 {
			newCap = 32
		}
		newSlice := make([]Span, len(c.spans), newCap)
		copy(newSlice, c.spans)
		c.spans = newSlice
	}
	c.spans = append(c.spans, *span)
}

// bufferSpanSafe adds a span to the internal buffer with proper locking.
// Safe to call from any goroutine, used by sync mode.
func (c *Collector) bufferSpanSafe(span *Span) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if buffer needs to grow - optimized growth strategy.
	if len(c.spans) >= cap(c.spans) {
		// More aggressive growth for better performance under load.
		currentCap := cap(c.spans)
		var newCap int
		if currentCap < 1024 {
			// Double capacity for small buffers.
			newCap = currentCap * 2
		} else {
			// Grow by 50% for large buffers to avoid excessive memory usage.
			newCap = currentCap + currentCap/2
		}
		if newCap < 32 {
			newCap = 32
		}
		newSlice := make([]Span, len(c.spans), newCap)
		copy(newSlice, c.spans)
		c.spans = newSlice
	}
	c.spans = append(c.spans, *span)
}

// Export returns a copy of all buffered spans and clears the internal buffer.
// The returned slice is safe to modify without affecting the collector.
func (c *Collector) Export() []Span {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.spans) == 0 {
		return nil
	}

	// Create a deep copy of the spans.
	result := make([]Span, len(c.spans))
	for i := range c.spans {
		result[i] = c.spans[i]
		// Deep copy the Tags map to prevent sharing.
		if c.spans[i].Tags != nil {
			result[i].Tags = make(map[string]string)
			for k, v := range c.spans[i].Tags {
				result[i].Tags[k] = v
			}
		}
	}

	// Optimized buffer management after export.
	// More conservative shrinking to avoid allocation churn.
	if cap(c.spans) > 256 && len(c.spans) < cap(c.spans)/8 {
		// Only shrink if buffer is very oversized to avoid allocation churn.
		newCap := cap(c.spans) / 4
		if newCap < 32 {
			newCap = 32
		}
		c.spans = make([]Span, 0, newCap)
	} else {
		c.spans = c.spans[:0] // Keep capacity, reset length.
	}

	return result
}

// Count returns the current number of buffered spans.
func (c *Collector) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.spans)
}

// DroppedCount returns the total number of spans dropped due to backpressure.
func (c *Collector) DroppedCount() int64 {
	return c.droppedCount.Load()
}

// SetSyncMode enables synchronous collection for testing.
// When enabled, spans are collected directly without using the channel.
// This makes tests deterministic by eliminating async behavior.
func (c *Collector) SetSyncMode(sync bool) {
	c.syncMode = sync
}

// Reset clears all buffered spans and resets the drop counter.
// Does not affect the running goroutine - use close() for that.
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.spans = c.spans[:0]
	c.droppedCount.Store(0)
}
