# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `SpanHandler` type for callback-based span processing
- `OnSpanComplete(handler SpanHandler) uint64` - register synchronous handler
- `OnSpanCompleteAsync(handler SpanHandler) uint64` - register asynchronous handler  
- `RemoveHandler(id uint64)` - remove handler by ID
- `SetPanicHook(hook func(uint64, interface{}))` - handle handler panics
- `EnableWorkerPool(workers, queueSize int) error` - bounded async execution
- `DroppedSpans() uint64` - monitor dropped spans due to full worker queue

### Changed
- **BREAKING**: Replaced Collector abstraction with callback-based API
  - Zero memory overhead when no handlers are registered
  - Direct handler invocation instead of channel-based collection
  - Handlers receive immutable span copies by value
  - Migration example:
    ```go
    // Before
    collector := tracez.NewCollector("spans", 1000)
    tracer.AddCollector("spans", collector)
    spans := collector.Export()
    
    // After
    var spans []tracez.Span
    tracer.OnSpanComplete(func(span tracez.Span) {
        spans = append(spans, span)
    })
    ```

### Removed
- **BREAKING**: `Collector` type and all associated methods
- **BREAKING**: `AddCollector()` method from Tracer
- **BREAKING**: `Reset()` method from Tracer
- `collector.go` and `collector_test.go` files (implementation detail)

## [1.0.0] - 2024-09-13

### Added
- Initial release of tracez
- Core tracing functionality with Tracer, Span, and Collector
- Thread-safe operations across all components
- Context propagation for parent-child span relationships
- Backpressure protection to prevent OOM conditions
- High-performance span creation (1.84M spans/sec single-threaded, 3.92M spans/sec parallel)
- Memory-efficient design with buffer shrinking and bounded growth
- Comprehensive documentation including getting started, API reference, cookbook, and troubleshooting
- Working examples for HTTP middleware, database patterns, and worker pools
- Complete test suite with >95% coverage
- Performance benchmarks and profiling tools
- Integration and end-to-end tests
- CI/CD pipeline with quality gates

### Key Features
- **Minimal Dependencies**: Standard library only
- **Thread-Safe**: Concurrent operations across goroutines
- **Backpressure Protection**: Spans dropped when buffers full
- **Resource Management**: Proper cleanup and shutdown
- **High Performance**: Measured performance characteristics
- **Context Propagation**: Automatic parent-child relationships
- **Memory Efficient**: Predictable allocation patterns

### Performance Characteristics
- **Span Creation**: 1.84M spans/sec single-threaded, 3.92M spans/sec parallel (with race detection)
- **Memory Overhead**: ~344 bytes per span base + tag data
- **Allocation Pattern**: 344 B/op base + tag data, 8 allocs/op minimum per span
- **Test Coverage**: 95.9%

### Design Principles
1. **Simplicity Over Features**: Minimal API surface
2. **Performance Over Convenience**: Predictable resource usage  
3. **Explicitness Over Magic**: All behavior is visible
4. **Composition Over Inheritance**: Build complex behavior from simple parts
5. **Testing Over Documentation**: Behavior verified by tests

---

## Release Notes Format

### For Maintainers

When creating a new release, follow this checklist:

1. **Update Version Numbers**
   - Update version in relevant files
   - Ensure go.mod reflects correct version

2. **Document Changes**
   - Move items from [Unreleased] to new version section
   - Use semantic versioning for version numbers
   - Include date in ISO format (YYYY-MM-DD)

3. **Categorize Changes**
   - **Added**: New features
   - **Changed**: Changes in existing functionality  
   - **Deprecated**: Soon-to-be removed features
   - **Removed**: Removed features
   - **Fixed**: Bug fixes
   - **Security**: Security fixes

4. **Performance Impact**
   - Include benchmark comparisons for performance changes
   - Document any memory allocation pattern changes
   - Note any breaking changes to performance characteristics

5. **Migration Notes**
   - Include migration instructions for breaking changes
   - Provide code examples for API changes
   - Document deprecated feature replacements

### Version Compatibility

- **Patch releases** (1.0.x): Bug fixes, no API changes
- **Minor releases** (1.x.0): New features, backward compatible
- **Major releases** (x.0.0): Breaking changes (used sparingly)

### Breaking Changes

When introducing breaking changes:
1. Document the change clearly in the changelog
2. Provide migration examples
3. Explain the rationale for the change
4. Include timeline for deprecation if applicable

Example breaking change entry:
```markdown
### Changed
- **BREAKING**: Collector.Export() now returns deep copy instead of reference
- **Migration**: No code changes needed, but spans are now immutable after export
- **Rationale**: Prevents data races and ensures export consistency
```