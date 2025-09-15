# tracez

[![CI Status](https://github.com/zoobzio/tracez/workflows/CI/badge.svg)](https://github.com/zoobzio/tracez/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/zoobzio/tracez/graph/badge.svg?branch=main)](https://codecov.io/gh/zoobzio/tracez)
[![Go Report Card](https://goreportcard.com/badge/github.com/zoobzio/tracez)](https://goreportcard.com/report/github.com/zoobzio/tracez)
[![CodeQL](https://github.com/zoobzio/tracez/workflows/CodeQL/badge.svg)](https://github.com/zoobzio/tracez/security/code-scanning)
[![Go Reference](https://pkg.go.dev/badge/github.com/zoobzio/tracez.svg)](https://pkg.go.dev/github.com/zoobzio/tracez)
[![License](https://img.shields.io/github/license/zoobzio/tracez)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/zoobzio/tracez)](go.mod)
[![Release](https://img.shields.io/github/v/release/zoobzio/tracez)](https://github.com/zoobzio/tracez/releases)

A minimal, primitive distributed tracing library for Go applications.

## Overview

tracez focuses on span collection and export without the complexity of full OpenTelemetry. It's designed for systems that need basic distributed tracing with predictable performance and resource usage.

**When to use tracez:**
- OpenTelemetry feels like overkill for your needs
- You want predictable performance (1.84M+ spans/sec single-threaded, 3.92M+ parallel)  
- Zero external dependencies matter
- You need thread-safety without complexity
- Backpressure protection is essential

**When to use OpenTelemetry instead:**
- You need vendor-specific integrations
- Automatic instrumentation is required
- Standards compliance is critical

### Key Features

- **Minimal Dependencies**: Standard library only
- **Thread-Safe**: Concurrent operations across goroutines
- **Backpressure Protection**: Spans dropped when buffers full (prevents OOM)
- **Resource Management**: Proper cleanup and shutdown
- **High Performance**: 1.84M spans/sec single-threaded, 3.92M spans/sec parallel (measured with race detection enabled)
- **Context Propagation**: Automatic parent-child span relationships
- **Memory Efficient**: Buffer shrinking and bounded growth

## Quick Start

```go
package main

import (
    "context"
    "github.com/zoobzio/tracez"
)

func main() {
    // Create tracer
    tracer := tracez.New("my-service")
    defer tracer.Close()
    
    // Add collector
    collector := tracez.NewCollector("console", 100)
    tracer.AddCollector("console", collector)
    
    // Create spans
    ctx, span := tracer.StartSpan(context.Background(), "operation")
    span.SetTag("user.id", "123")
    defer span.Finish()
    
    // Child spans automatically inherit context
    childCtx, childSpan := tracer.StartSpan(ctx, "database-query")
    childSpan.SetTag("table", "users")
    defer childSpan.Finish()
    
    // Export completed spans
    spans := collector.Export()
    // ... process spans
}
```

## Core Components

### Tracer
Manages span lifecycle and collection. Thread-safe for concurrent use.

```go
tracer := tracez.New("service-name")
ctx, span := tracer.StartSpan(context.Background(), "operation-name")
```

### Span & ActiveSpan
- `Span`: Immutable span data structure
- `ActiveSpan`: Thread-safe wrapper for ongoing spans

```go
span.SetTag("key", "value")      // Thread-safe
span.SetTag("status", "success") // Concurrent access safe
span.Finish()                    // Safe to call multiple times
```

### Collector
Buffers completed spans for batch export with backpressure protection.

```go
collector := tracez.NewCollector("exporter-name", bufferSize)
tracer.AddCollector("exporter", collector)

// Export spans (returns deep copy)
spans := collector.Export()

// Monitor drops
dropped := collector.DroppedCount()
```

## Thread Safety

| Component | Thread Safety |
|-----------|---------------|
| `Tracer` | ✅ Safe for concurrent use |
| `Collector` | ✅ Safe for concurrent use |
| `ActiveSpan` | ✅ SetTag/GetTag operations safe |
| `Span` (raw) | ❌ Not thread-safe |

## Performance Characteristics

- **Span Creation**: 1.84M spans/sec single-threaded, 3.92M spans/sec parallel (measured with race detection)
- **Memory Overhead**: ~344 bytes per span base + tag data (~20 bytes per tag)
- **Allocation Pattern**: 344 B/op base + tag data, 8 allocs/op minimum per span
- **Backpressure**: Drops spans when buffer full (configurable)

## Documentation

### Learn tracez
- **[Getting Started](docs/getting-started.md)** - 5-minute tutorial to your first working trace
- **[API Reference](docs/api-reference.md)** - Complete function documentation with examples
- **[Cookbook](docs/cookbook.md)** - Common patterns and best practices
- **[Troubleshooting](docs/troubleshooting.md)** - When things go wrong

### Working Examples
- **[HTTP Middleware](examples/http-middleware/)** - Trace web requests and middleware chains
- **[Database Patterns](examples/database-patterns/)** - Detect N+1 queries and optimize performance
- **[Worker Pool](examples/worker-pool/)** - Context propagation across goroutines

## Architecture

tracez follows the principle of **visible complexity** - all behavior is explicit and testable:

- **No Hidden Magic**: No reflection, code generation, or framework abstractions
- **Predictable Performance**: Linear scaling, bounded memory usage
- **Verifiable Behavior**: Every code path has corresponding unit tests
- **Simple Primitives**: Components compose without hidden dependencies

### Memory Management

- Collectors automatically shrink buffers after large exports
- Backpressure prevents unbounded memory growth
- Deep copying prevents data sharing between exports
- Clean shutdown prevents goroutine leaks

### Error Handling

- Non-blocking operations (spans dropped instead of blocking)
- Graceful degradation under high load
- Explicit error returns (no silent failures)
- Timeout protection for shutdown operations

## Installation

```bash
go get github.com/zoobzio/tracez
```

**Requirements:**
- Go 1.21 or later
- No external dependencies

## Testing

```bash
# Run unit tests with race detection
make test

# Check test coverage (95.9%)
make coverage

# Run linting
make lint

# Full CI pipeline
make check
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing requirements, and code standards.

Quick checklist:
1. Fork and create feature branch
2. Write tests (>95% coverage required)
3. Ensure `make ci` passes
4. Update documentation if needed
5. Submit pull request

## License

MIT License - see LICENSE file for details.

## Design Principles

tracez is built following these principles:

1. **Simplicity Over Features**: Minimal API surface
2. **Performance Over Convenience**: Predictable resource usage
3. **Explicitness Over Magic**: All behavior is visible
4. **Composition Over Inheritance**: Build complex behavior from simple parts
5. **Testing Over Documentation**: Behavior verified by tests

This makes tracez suitable for systems where predictability and resource efficiency matter more than feature completeness.