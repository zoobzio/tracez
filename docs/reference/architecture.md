# Architecture Reference

Deep dive into tracez internal design, data flow, and implementation details.

## Component Overview

```
Application Code
       ↓
    Tracer (StartSpan/Finish)
       ↓
   ActiveSpan (SetTag/GetTag)
       ↓
    Span (immutable)
       ↓
    Collectors (buffering)
       ↓
    Export (batch)
       ↓
  External Systems
```

## Data Flow

### 1. Span Creation
```go
// Application calls StartSpan
ctx, span := tracer.StartSpan(context.Background(), "operation")

// Internal flow:
// 1. Generate unique SpanID and TraceID (or inherit TraceID from parent)
// 2. Create internalSpan with timestamps
// 3. Store span in context for child span linking
// 4. Return ActiveSpan wrapper for thread-safe operations
```

### 2. Span Modification  
```go
// Application modifies span
span.SetTag("user.id", "123")

// Internal flow:
// 1. ActiveSpan.SetTag() acquires mutex
// 2. Writes to internal tags map
// 3. Releases mutex
// Thread-safe for concurrent modifications
```

### 3. Span Completion
```go
// Application finishes span
span.Finish()

// Internal flow:
// 1. Calculate final duration
// 2. Convert ActiveSpan to immutable Span
// 3. Send to all registered collectors via channels (non-blocking)
// 4. ActiveSpan becomes invalid for further operations
```

### 4. Span Collection
```go
// Collectors receive spans asynchronously
for span := range collector.spansCh {
    if buffer_not_full {
        buffer.append(span)
    } else {
        dropCount++  // Drop span, don't block
    }
}
```

### 5. Span Export
```go
// Application exports spans
spans := collector.Export()

// Internal flow:
// 1. Atomic buffer swap (O(1) operation)
// 2. Copy spans to returned slice
// 3. Clear internal buffer
// 4. Return spans to application
```

## Thread Safety Design

### Layered Concurrency Control

#### Tracer Level
```go
type Tracer struct {
    mu          sync.RWMutex     // Protects collectors map
    collectors  map[string]*Collector
    serviceName string           // Immutable after creation
}
```

**Guarantees:**
- Multiple goroutines can call `StartSpan()` simultaneously
- Collector registration/removal protected by RWMutex
- Service name immutable - no synchronization needed

#### ActiveSpan Level  
```go
type ActiveSpan struct {
    mu   sync.Mutex              // Protects tags map
    span *internalSpan
}
```

**Guarantees:**
- `SetTag()` and `GetTag()` are mutex-protected
- Multiple goroutines can modify tags concurrently
- `Finish()` can be called safely from any goroutine

#### Collector Level
```go
type Collector struct {
    spansCh      chan Span       // Buffered channel
    mu           sync.Mutex      // Protects spans slice during export
    spans        []Span
    droppedCount atomic.Int64    // Lock-free counter
}
```

**Guarantees:**
- Channel provides lock-free span collection in hot path
- Export operations are mutex-protected
- Dropped count uses atomic operations
- No locks during span ingestion

## Context Propagation Mechanism

### Parent-Child Linking
```go
// Span storage in context
type spanKey struct{}

func withSpan(ctx context.Context, span *internalSpan) context.Context {
    return context.WithValue(ctx, spanKey{}, span)
}

func spanFromContext(ctx context.Context) *internalSpan {
    if span, ok := ctx.Value(spanKey{}).(*internalSpan); ok {
        return span
    }
    return nil
}
```

### ID Inheritance Rules
1. **Root span**: Generates new random TraceID and SpanID
2. **Child span**: Inherits TraceID from parent, generates new SpanID
3. **Parent relationship**: Child's ParentID = Parent's SpanID
4. **Context requirement**: Child spans MUST use parent's context

### Example Hierarchy
```go
// Root span
TraceID: abc123, SpanID: span001, ParentID: ""

// Child span  
TraceID: abc123, SpanID: span002, ParentID: span001

// Grandchild span
TraceID: abc123, SpanID: span003, ParentID: span002
```

## Memory Management

### Span Lifecycle
```
ActiveSpan (mutable)  →  Span (immutable)  →  Export  →  GC
    ↓                       ↓                   ↓
 Tags Map              Tags Map Copy      External System
(mutex protected)      (read-only)       (application owned)
```

### Memory Allocation Patterns
```go
// Span creation (~344 bytes base)
span := &internalSpan{
    TraceID:   generateID(),     // 32 bytes (hex string)
    SpanID:    generateID(),     // 32 bytes  
    ParentID:  parentID,         // 32 bytes
    Name:      operation,        // Variable (typically 20-50 bytes)
    StartTime: time.Now(),       // 24 bytes
    Tags:      make(map[string]string), // ~48 bytes base + content
}

// Tag storage (~20 bytes per tag)
span.Tags[key] = value  // Key + value + map overhead
```

### Buffer Management Strategy
```go
// Collector buffer grows and shrinks automatically
func (c *Collector) Export() []Span {
    spans := make([]Span, len(c.spans))
    copy(spans, c.spans)
    
    // Smart memory management
    if len(c.spans) > 1024 {
        c.spans = c.spans[:0]  // Reset length, keep capacity
    } else {
        c.spans = nil  // Let GC reclaim memory
        c.spans = make([]Span, 0, 8)  // Fresh slice
    }
    
    return spans
}
```

## Backpressure Protection

### Non-Blocking Collection
```go
func (c *Collector) collectSpan(span Span) {
    select {
    case c.spansCh <- span:
        // Span accepted
    default:
        // Channel full, drop span immediately
        atomic.AddInt64(&c.droppedCount, 1)
    }
}
```

### Benefits of Drop Strategy
1. **Performance Protection**: Never blocks application threads
2. **Graceful Degradation**: Loses observability but preserves functionality  
3. **Observable Backpressure**: `DroppedCount()` shows buffer pressure
4. **Configurable**: Buffer sizes tunable per deployment

### Alternative Strategies Considered and Rejected

#### Blocking Collection (Rejected)
```go
// REJECTED: Would block application
c.spansCh <- span  // Blocks when channel full
```
**Problems**: Deadlocks, performance degradation, unpredictable latency

#### Overflow to Disk (Rejected)  
**Problems**: I/O overhead, disk space management, complexity

#### Dynamic Buffer Growth (Rejected)
**Problems**: OOM risk, unpredictable memory usage

## ID Generation

### Cryptographically Secure Random IDs
```go
func generateID() string {
    bytes := make([]byte, 16)
    if _, err := rand.Read(bytes); err != nil {
        // Fallback to time-based ID
        return fmt.Sprintf("%d-%d", time.Now().UnixNano(), rand.Int63())
    }
    return hex.EncodeToString(bytes)
}
```

### ID Properties
- **Uniqueness**: 128-bit random values, collision probability ~2^-64
- **Format**: 32-character hex strings for readability
- **Fallback**: Time-based IDs if crypto/rand fails
- **Thread Safety**: crypto/rand is thread-safe

## Error Handling Philosophy

### Fail-Safe Design Principles

#### Never Crash the Application
```go
func (t *Tracer) StartSpan(ctx context.Context, operation string) (context.Context, *ActiveSpan) {
    // Always return valid objects, even on internal failures
    span := &internalSpan{
        TraceID:   generateIDWithFallback(),
        SpanID:    generateIDWithFallback(),
        Name:      operation,
        StartTime: time.Now(),
        Tags:      make(map[string]string),
    }
    
    return withSpan(ctx, span), &ActiveSpan{span: span}
}
```

#### Degraded Service Over No Service
- **ID generation fails** → Time-based fallback IDs
- **Collector buffer full** → Drop spans, continue operating
- **Export fails** → Log error, continue collecting
- **Memory pressure** → Shrink buffers, continue operating

### Error Visibility
```go
// Errors are observable but non-fatal
func (c *Collector) DroppedCount() int64 {
    return atomic.LoadInt64(&c.droppedCount)
}

// Applications can monitor health
if dropped := collector.DroppedCount(); dropped > threshold {
    log.Printf("High span drop rate: %d", dropped)
    // Increase buffer size or export frequency
}
```

## Performance Characteristics

### Benchmarked Performance
- **Single-threaded**: 1.84M spans/sec
- **Parallel**: 3.92M spans/sec  
- **Memory per span**: ~344 bytes + ~20 bytes per tag
- **Allocations per span**: ~8 allocations

### Hot Path Optimization
1. **Lock-free span creation**: No synchronization during StartSpan()
2. **Channel-based collection**: Non-blocking span ingestion
3. **Atomic counters**: Lock-free dropped count tracking
4. **Buffer reuse**: Minimize GC pressure in collectors

### Cold Path Operations
- **Collector registration**: Mutex-protected (rare operation)
- **Export**: Mutex-protected (periodic operation)
- **Tag operations**: Mutex-protected (span lifetime only)

## Design Trade-offs

### Chosen: Simplicity Over Features
- **No sampling**: Application-level concern
- **No trace context propagation**: Standard Go context
- **No span relationships beyond parent-child**: Keeps model simple
- **No automatic instrumentation**: Explicit, predictable behavior

### Chosen: Performance Over Complete Observability  
- **Drop spans when full**: Protects application performance
- **Fixed buffer sizes**: Predictable memory usage
- **Minimal span data**: Essential information only

### Chosen: Thread Safety Over Speed
- **Mutex-protected tags**: Allows concurrent modifications
- **Atomic operations**: Thread-safe counters
- **Immutable spans**: Safe to share after export

## Integration Points

### Context Integration
```go
// Leverages Go's context.Context for span propagation
// No custom context types or global state
ctx = context.WithValue(ctx, spanKey{}, span)
```

### Standard Library Compatibility
```go
// Works with any code using context.Context
func httpHandler(w http.ResponseWriter, r *http.Request) {
    ctx, span := tracer.StartSpan(r.Context(), "handler")
    defer span.Finish()
    
    // Context flows through standard library
    row := db.QueryRowContext(ctx, query)
}
```

### External System Integration
```go
// Span data designed for easy serialization
type Span struct {
    TraceID    string            // JSON-friendly
    SpanID     string            // No binary data
    ParentID   string
    Name       string
    StartTime  time.Time         // Standard time format
    Duration   time.Duration     // Standard duration
    Tags       map[string]string // Simple key-value pairs
}
```

## Scalability Considerations

### Horizontal Scaling
- **Per-service tracers**: Each service instance has independent tracer
- **Trace ID correlation**: Same trace ID across service boundaries
- **Stateless design**: No shared state between tracers

### Vertical Scaling  
- **Multiple collectors**: Shard span collection by type
- **Buffer size scaling**: Larger buffers for higher throughput
- **Export frequency tuning**: Balance latency vs efficiency

### Resource Management
- **Bounded memory**: Fixed buffer sizes prevent OOM
- **Bounded CPU**: Non-blocking design prevents CPU starvation  
- **Bounded I/O**: Export is application-controlled

This architecture prioritizes application performance and reliability over complete trace capture, making it suitable for production environments where observability should never impact service availability.