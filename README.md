# tracez

[![CI Status](https://github.com/zoobzio/tracez/workflows/CI/badge.svg)](https://github.com/zoobzio/tracez/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/zoobzio/tracez/graph/badge.svg?branch=main)](https://codecov.io/gh/zoobzio/tracez)
[![Go Report Card](https://goreportcard.com/badge/github.com/zoobzio/tracez)](https://goreportcard.com/report/github.com/zoobzio/tracez)
[![CodeQL](https://github.com/zoobzio/tracez/workflows/CodeQL/badge.svg)](https://github.com/zoobzio/tracez/security/code-scanning)
[![Go Reference](https://pkg.go.dev/badge/github.com/zoobzio/tracez.svg)](https://pkg.go.dev/github.com/zoobzio/tracez)
[![License](https://img.shields.io/github/license/zoobzio/tracez)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zoobzio/tracez)](go.mod)
[![Release](https://img.shields.io/github/v/release/zoobzio/tracez)](https://github.com/zoobzio/tracez/releases)

A minimal span collection library for Go applications - a building block for observability systems.

## What tracez Actually Is

tracez collects spans within your Go application for local performance analysis or export to APM systems. It's a **primitive** - the foundation you build observability on, not a complete tracing solution.

**What span collection enables:**
- Feed APM systems (Datadog, New Relic, Jaeger) with performance data
- Local performance analysis during development
- Identify slow operations and bottlenecks
- Understand code execution paths
- Measure resource usage patterns

**tracez is NOT:**
- Distributed tracing (no cross-service correlation)
- An APM system (no UI, no analysis tools)
- A metrics system (spans only, not counters/gauges)
- A logging framework (structured performance data only)

## When to Use tracez

**Use tracez when building:**
- Custom APM integrations
- Performance monitoring tools
- Development profiling utilities
- Lightweight observability for libraries
- Systems where you control the entire span pipeline

**Use OpenTelemetry instead when:**
- You need actual distributed tracing across services
- Vendor-specific integrations are required
- Automatic instrumentation is needed
- Standards compliance (W3C Trace Context) is critical
- Cross-process correlation is required
- You want a complete solution, not a building block

## Core Features

- **Minimal Dependencies**: Standard library only
- **Thread-Safe**: Safe concurrent operations across goroutines
- **Backpressure Protection**: Drops spans when buffers full (prevents OOM)
- **High Performance**: 1.84M spans/sec single-threaded, 3.92M spans/sec parallel
- **Context Propagation**: Parent-child relationships within your process
- **Memory Efficient**: Bounded growth with automatic buffer shrinking

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "github.com/zoobzio/tracez"
)

func main() {
    // Create tracer for your library/component
    tracer := tracez.New("auth-component")  // Component name, not service
    defer tracer.Close()
    
    // Add collector with buffer size
    collector := tracez.NewCollector("apm-exporter", 100)
    tracer.AddCollector("apm-exporter", collector)
    
    // Collect performance data
    ctx, span := tracer.StartSpan(context.Background(), "validate-token")
    span.SetTag("token.type", "jwt")
    defer span.Finish()
    
    // Child spans track nested operations
    childCtx, childSpan := tracer.StartSpan(ctx, "database-lookup")
    childSpan.SetTag("query", "SELECT * FROM users WHERE token = ?")
    defer childSpan.Finish()
    
    // Export spans for processing
    spans := collector.Export()
    
    // Feed to your APM system
    for _, span := range spans {
        // Send to Datadog, New Relic, Jaeger, etc.
        sendToAPM(span)
    }
}

func sendToAPM(span tracez.Span) {
    // Your APM integration logic
    // Convert span to vendor format
    // Batch and send to APM endpoint
}
```

## Building Observability Systems

tracez provides primitives. You build the system:

### Example: Local Development Profiler

```go
// Collect spans during test runs
collector := tracez.NewCollector("profiler", 1000)
tracer.AddCollector("profiler", collector)

// Run your code...

// Analyze performance locally
spans := collector.Export()
analyzer := NewPerformanceAnalyzer(spans)
slowOps := analyzer.FindSlowOperations(100 * time.Millisecond)
fmt.Printf("Found %d slow operations\n", len(slowOps))
```

### Example: Production APM Integration

```go
// Batch spans for APM export
type APMExporter struct {
    collector *tracez.Collector
    client    *http.Client
    endpoint  string
}

func (e *APMExporter) Run(ctx context.Context) {
    ticker := time.NewTicker(10 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            spans := e.collector.Export()
            if len(spans) > 0 {
                e.sendBatch(spans)
            }
        case <-ctx.Done():
            return
        }
    }
}
```

## Components

### Tracer
Manages span lifecycle within your Go application. One per library/component.

```go
tracer := tracez.New("component-name")  // Not service name
ctx, span := tracer.StartSpan(context.Background(), "operation")
```

### Span & ActiveSpan
- `Span`: Immutable completed span data
- `ActiveSpan`: Thread-safe wrapper for spans being recorded

```go
span.SetTag("cache.hit", "true")     // Thread-safe
span.SetTag("cache.key", key)        // Concurrent safe
span.Finish()                         // Idempotent
```

### Collector
Buffers spans for batch processing. Foundation for exporters.

```go
collector := tracez.NewCollector("exporter-name", bufferSize)
tracer.AddCollector("exporter", collector)

// Get spans for processing (returns copy)
spans := collector.Export()

// Monitor health
dropped := collector.DroppedCount()
if dropped > 0 {
    log.Printf("Warning: dropped %d spans\n", dropped)
}
```

## Performance Characteristics

Measured with race detection enabled:

| Operation | Throughput | Memory | Allocations |
|-----------|------------|--------|-------------|
| Span Creation | 1.84M/sec (single) | 344 B/op | 8 allocs |
| Span Creation | 3.92M/sec (parallel) | 344 B/op | 8 allocs |
| Tag Addition | - | ~20 B/tag | 1 alloc |
| Export (1000 spans) | 485K/sec | Deep copy | Batch alloc |

Backpressure: Automatically drops spans when buffer full (configurable).

## Documentation

### Learn the Primitives
- **[Getting Started](docs/getting-started.md)** - Build your first span collector
- **[API Reference](docs/api-reference.md)** - Complete component documentation
- **[Cookbook](docs/cookbook.md)** - Integration patterns and examples
- **[Troubleshooting](docs/troubleshooting.md)** - Common issues and solutions

### Integration Examples
- **[APM Export](examples/apm-export/)** - Send spans to Datadog/New Relic
- **[Performance Analysis](examples/performance-analysis/)** - Local profiling tools
- **[HTTP Middleware](examples/http-middleware/)** - Web request instrumentation
- **[Database Monitoring](examples/database-monitoring/)** - Query performance tracking

## Architecture Principles

tracez follows **visible complexity** - no hidden behavior:

- **No Magic**: No reflection, code generation, or hidden abstractions
- **Predictable**: Linear performance, bounded memory
- **Testable**: Every path has unit tests
- **Composable**: Simple primitives build complex systems

### Memory Management

- Automatic buffer shrinking after exports
- Bounded growth with backpressure
- Deep copies prevent reference leaks
- Clean shutdown without goroutine leaks

### Thread Safety

| Component | Safety | Notes |
|-----------|--------|-------|
| `Tracer` | ✅ Safe | Concurrent span creation |
| `Collector` | ✅ Safe | Concurrent collection/export |
| `ActiveSpan` | ✅ Safe | Concurrent tag operations |
| `Span` (exported) | ❌ Immutable | Read-only after export |

## Installation

```bash
go get github.com/zoobzio/tracez
```

**Requirements:**
- Go 1.21 or later
- No external dependencies

## Testing

```bash
# Run tests with race detection
make test

# Coverage report (95.9%)
make coverage

# Linting
make lint

# Full CI suite
make check
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

Quick start:
1. Fork repository
2. Create feature branch
3. Write tests (maintain >95% coverage)
4. Run `make ci`
5. Submit pull request

## License

MIT License - see LICENSE file.

## Design Philosophy

tracez is a primitive, not a platform:

1. **Primitives Over Frameworks**: Building blocks, not solutions
2. **Explicit Over Automatic**: You control what happens
3. **Performance Over Features**: Predictable resource usage
4. **Visibility Over Convenience**: See how everything works
5. **Composition Over Configuration**: Build what you need

This makes tracez ideal when you need to build custom observability solutions or integrate with specific APM systems without framework overhead.