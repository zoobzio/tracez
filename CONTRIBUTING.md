# Contributing to tracez

Thank you for your interest in contributing! This document provides everything you need to contribute effectively.

## Development Setup

### Prerequisites
- Go 1.21 or later
- `golangci-lint` for code quality checks
- `make` for build automation

### Quick Start
```bash
# Clone the repository
git clone https://github.com/zoobzio/tracez
cd tracez

# Install development tools
make install-tools

# Run tests to verify setup
make test

# Check code quality
make lint

# Run the complete CI pipeline
make ci
```

## Project Structure

```
tracez/
├── *.go                 # Core library implementation
├── *_test.go           # Unit tests (>95% coverage required)
├── docs/               # User documentation
│   ├── getting-started.md
│   ├── api-reference.md
│   ├── cookbook.md
│   └── troubleshooting.md
├── examples/           # Working examples with tests
│   ├── database-patterns/
│   ├── http-middleware/
│   └── worker-pool/
├── testing/            # Test organization
│   ├── benchmarks/     # Performance benchmarks
│   ├── e2e/           # End-to-end tests
│   └── integration/   # Integration tests
└── Makefile           # Build automation
```

## Build Commands

The Makefile provides all necessary build and test commands:

### Basic Testing
```bash
make test         # Unit tests with race detection
make test-short   # Quick unit tests (short mode)
make bench        # Run benchmarks
make coverage     # Generate HTML coverage report
```

### Code Quality
```bash
make lint         # Run golangci-lint
make fmt          # Format source code
make vet          # Run go vet
make check        # Quick verification (test + lint)
```

### Complete Validation
```bash
make ci           # Full CI pipeline
make all          # Run all quality checks
make release-check # Pre-release verification
```

### Specialized Testing
```bash
make integration  # Integration tests
make e2e          # End-to-end tests  
make benchmarks   # Performance benchmarks
make stress       # Stress tests
make race         # Race condition tests
```

### Development Helpers
```bash
make examples     # Run all examples
make doc          # Start godoc server
make watch        # Watch for changes (requires fswatch)
make profile      # Generate performance profiles
```

## Testing Requirements

### Unit Tests
All code must have comprehensive unit tests:
- **Coverage**: Minimum 95% (currently 95.9%)
- **Race Detection**: All tests run with `-race` flag
- **Concurrency**: Test concurrent operations explicitly
- **Error Cases**: Test failure modes and edge cases

Example test structure:
```go
func TestTracerStartSpan(t *testing.T) {
    tracer := tracez.New("test-service")
    defer tracer.Close()
    
    ctx, span := tracer.StartSpan(context.Background(), "test-operation")
    defer span.Finish()
    
    // Verify span properties
    assert.Equal(t, "test-operation", span.Name)
    assert.NotEmpty(t, span.SpanID)
    
    // Verify context contains span
    extractedSpan := tracez.GetSpan(ctx)
    assert.NotNil(t, extractedSpan)
}
```

### Benchmarks
Performance-sensitive code must include benchmarks:
```go
func BenchmarkSpanCreation(b *testing.B) {
    tracer := tracez.New("benchmark")
    defer tracer.Close()
    
    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        _, span := tracer.StartSpan(context.Background(), "benchmark-span")
        span.Finish()
    }
}
```

### Examples
All examples must:
- Have corresponding tests (`main_test.go`)
- Compile and run without errors
- Demonstrate realistic usage patterns
- Include explanatory comments

## Code Standards

### API Design Principles
1. **Simplicity**: Minimal API surface area
2. **Safety**: Thread-safe by default
3. **Performance**: Reasonable allocation patterns (344 B/op, 8 allocs/op measured)
4. **Explicitness**: No hidden magic or reflection
5. **Composability**: Simple parts combine for complex behavior

### Go Style Guidelines
- Follow [Effective Go](https://golang.org/doc/effective_go.html)
- Use `golangci-lint` configuration (see `.golangci.yml`)
- Format with `gofmt` (enforced by CI)
- Document all public APIs with examples

### Error Handling
```go
// Preferred: Explicit error returns
func (t *Tracer) AddCollector(collector *Collector) error {
    if collector == nil {
        return fmt.Errorf("collector cannot be nil")
    }
    // ...
    return nil
}

// Avoid: Panics in library code
func (t *Tracer) AddCollector(collector *Collector) {
    if collector == nil {
        panic("collector cannot be nil") // ← Don't do this
    }
}
```

### Concurrency
- All public APIs must be thread-safe
- Document thread safety guarantees
- Test concurrent usage patterns
- Use minimal locking (prefer atomic operations)

### Memory Management
- Prevent memory leaks in long-running applications
- Implement proper cleanup (Close methods)
- Use bounded buffers with backpressure protection
- Test memory usage patterns

## Documentation Standards

### Godoc Comments
All public APIs require comprehensive documentation:
```go
// StartSpan creates a new span and returns it wrapped in an ActiveSpan.
// If the context contains an existing span, the new span will be its child.
//
// The returned context contains both the span and tracer for easy propagation
// to child operations. Always call Finish() on the returned span to prevent
// resource leaks.
//
// Example:
//   ctx, span := tracer.StartSpan(context.Background(), "operation-name")
//   defer span.Finish()
//   
//   // Use ctx for child operations
//   childCtx, childSpan := tracer.StartSpan(ctx, "child-operation")
//   defer childSpan.Finish()
func (t *Tracer) StartSpan(ctx context.Context, name string) (context.Context, *ActiveSpan)
```

### Markdown Documentation
- Keep user documentation current with code changes
- Test all code examples in documentation
- Update troubleshooting guide with real issues
- Include performance characteristics

## Contribution Workflow

### 1. Before You Start
- Check existing issues and PRs to avoid duplication
- For large changes, create an issue first to discuss the approach
- Fork the repository and create a feature branch

### 2. Development Process
```bash
# Create feature branch
git checkout -b feature/your-feature-name

# Make changes with tests
# ... develop and test ...

# Verify everything works
make all

# Commit with descriptive messages
git commit -m "Add feature: brief description

Longer explanation of what this change does and why it's needed.
Includes any breaking changes or migration notes."
```

### 3. Code Review Requirements
Your PR must:
- [ ] Pass all CI checks (`make ci`)
- [ ] Include comprehensive tests
- [ ] Update documentation if needed
- [ ] Follow code style guidelines
- [ ] Not break existing APIs (unless clearly needed)
- [ ] Include benchmark results for performance changes

### 4. Merge Process
- All PRs require review from project maintainers
- CI must pass (tests, linting, coverage)
- Squash commits for clean git history
- Update CHANGELOG.md for user-facing changes

## Performance Standards

tracez is a performance-focused library. All changes must maintain:

### Benchmarked Performance
Current performance targets:
- **Span Creation**: 1.84M spans/sec single-threaded, 3.92M spans/sec parallel (measured with race detection)
- **Memory Overhead**: ~344 bytes per span + tag data (measured)
- **Reasonable Allocations**: 344 B/op, 8 allocs/op (measured and predictable)

### Running Benchmarks
```bash
# Run all benchmarks
make benchmarks

# Compare before/after performance
go test -bench=. -count=5 -benchmem ./... > before.txt
# ... make changes ...
go test -bench=. -count=5 -benchmem ./... > after.txt

# Use benchcmp to compare
benchcmp before.txt after.txt
```

### Memory Profiling
```bash
# Generate memory profiles
make profile

# Analyze profiles
go tool pprof cpu.prof
go tool pprof mem.prof
```

## Release Process

### Version Compatibility
- **Patch releases** (1.0.x): Bug fixes, no API changes
- **Minor releases** (1.x.0): New features, backward compatible
- **Major releases** (x.0.0): Breaking changes (rare)

### Pre-Release Checklist
```bash
# Complete validation
make release-check

# Update version numbers and changelog
# Tag and create release
```

## Getting Help

### Questions and Discussion
- Create GitHub issues for bugs and feature requests
- Use GitHub Discussions for questions and ideas
- Check existing documentation first

### Response Time
- Bug reports: Within 2 business days
- Feature requests: Within 1 week  
- PRs: Within 1 week for initial review

## Code of Conduct

We follow the [Go Community Code of Conduct](https://golang.org/conduct). 

Key principles:
- Be respectful and inclusive
- Focus on technical merit
- Assume good intentions
- Help others learn and contribute

## Recognition

Contributors are recognized in:
- CHANGELOG.md for significant contributions
- GitHub contributor list
- Annual contributor highlights

Thank you for contributing to tracez! Every improvement helps make distributed tracing more accessible and reliable for the Go community.