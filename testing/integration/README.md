# Integration Test Scenarios: Real-World Pattern Validation

This directory contains integration tests that validate tracez behavior in realistic distributed system scenarios. Each test represents patterns that actually occur in production environments.

## Test Organization

### Core Infrastructure Tests

#### [Context Propagation](context_propagation_test.go)
**Validates:** Cross-goroutine trace context handling  
**Scenarios:**
- `TestCrossGoroutineContextPropagation` - Parent-child relationships across goroutine boundaries
- `TestContextCancellationDuringTracing` - Graceful handling of cancelled contexts
- `TestContextDeadlineExceeded` - Timeout behavior during span operations
- `TestNestedContextPropagation` - Deep nesting with context inheritance
- `TestContextValuePropagation` - Custom context values preserved through traces

**Why this matters:** Context propagation failures cause broken trace trees and lost correlation.

#### [Memory and Recovery](memory_and_recovery_test.go)  
**Validates:** Graceful degradation under stress conditions  
**Scenarios:**
- `TestSpanBufferGrowth` - Buffer expansion patterns under load
- `TestTagMapDeepCopy` - Memory safety in tag operations
- `TestNilContextHandling` - Defensive programming against nil contexts
- `TestMemoryPressureGracefulDegradation` - Behavior when memory is constrained
- `TestTagCardinalityExplosion` - Protection against unbounded tag growth
- `TestPanicRecovery` - Recovery from panics in user code
- `TestCollectorResetCleanup` - Proper resource cleanup

**Why this matters:** Production systems face memory pressure, bad data, and unexpected panics.

### Real-World Application Patterns

#### [HTTP Middleware Chain](real_world_patterns_test.go)
**Validates:** Complex middleware interaction patterns  
**Scenarios:**
- `TestHTTPMiddlewareChain` - Request flow through 5+ middleware components
- `TestDatabaseTransactionPattern` - Nested database operations with rollback
- `TestWorkerPoolPattern` - Producer-consumer patterns with backpressure
- `TestCircuitBreakerIntegration` - Failure detection and circuit breaker interaction
- `TestAsyncProcessingPattern` - Background job processing with correlation

**Why this matters:** These patterns represent 80% of real-world tracez usage.

#### [API Gateway Patterns](api_patterns_test.go)
**Validates:** API infrastructure tracing  
**Scenarios:**
- `TestAPIGatewayRouting` - Request routing with dynamic backends
- `TestRateLimiting` - Rate limit enforcement and bypass scenarios
- `TestGraphQLResolver` - Complex query resolution tracing
- `TestWebSocketConnection` - Long-lived connection tracing
- `TestRetryWithBackoff` - Retry logic with exponential backoff

**Why this matters:** API gateways are critical choke points requiring detailed observability.

### Distributed System Patterns

#### [Service Mesh Communication](service_mesh_test.go)
**Validates:** Inter-service communication patterns  
**Scenarios:**
- `TestServiceMeshCommunication` - Multi-service request flow
- `TestDistributedCircuitBreaker` - Cross-service failure detection
- `TestSagaPattern` - Distributed transaction rollback scenarios
- `TestEventSourcing` - Event-driven architecture tracing
- `TestGracefulDegradation` - Partial failure handling

**Why this matters:** Service mesh environments create complex trace propagation challenges.

#### [Database Integration](database_patterns_test.go)  
**Validates:** Database access pattern tracing  
**Scenarios:**
- `TestConnectionPoolExhaustion` - Pool exhaustion detection and recovery
- `TestDistributedTransaction` - Two-phase commit tracing
- `TestQueryOptimization` - N+1 query detection and batch optimization
- `TestDatabaseReplication` - Read/write split pattern tracing
- `TestCachingStrategy` - Cache hit/miss patterns with fallback

**Why this matters:** Database interactions are the most common source of performance issues.

### Observability and Reliability

#### [Observability Patterns](observability_patterns_test.go)
**Validates:** Advanced observability features  
**Scenarios:**
- `TestSamplingStrategies` - Head-based and tail-based sampling
- `TestTraceAggregation` - Span aggregation and metric generation
- `TestDistributedTracing` - Cross-service trace correlation
- `TestCorrelationIDs` - Request ID propagation and correlation
- `TestLatencyPercentiles` - P50, P95, P99 calculation accuracy
- `TestErrorRateAnalysis` - Error pattern detection and alerting
- `TestHealthcheckIntegration` - Health check correlation with traces

**Why this matters:** Observability features must work correctly under production conditions.

#### [Collector Backpressure](collector_backpressure_test.go)
**Validates:** System behavior under sustained load  
**Scenarios:**
- `TestChannelSaturation` - Buffer exhaustion and backpressure handling
- `TestCollectorShutdownUnderLoad` - Graceful shutdown with pending spans
- `TestMultipleCollectorsCompetition` - Resource contention between collectors
- `TestCollectorResetUnderLoad` - Reset behavior during active collection
- `TestExportLatencyImpact` - Slow export impact on collection
- `TestBufferGrowthPattern` - Dynamic buffer sizing under varying load

**Why this matters:** Collectors must remain stable during traffic spikes and export delays.

### Performance and Optimization

#### [Optimization Validation](optimization_validation_test.go)
**Validates:** Performance optimization effectiveness  
**Scenarios:**
- `TestIDPoolIntegration` - ID pool allocation efficiency
- `TestContextBundlingPropagation` - Context bundling memory reduction
- `TestBufferOptimizationUnderLoad` - Optimized buffer management
- `TestOptimizationBackwardCompatibility` - Optimization compatibility
- `TestIDPoolResourceCleanup` - Resource cleanup in ID pools
- `TestConcurrentPoolsAndContext` - Concurrent access to optimized structures

**Why this matters:** Optimizations must work correctly without breaking existing behavior.

#### [Parent-Child Relationships](parent_child_test.go)
**Validates:** Span hierarchy correctness  
**Scenarios:**
- `TestDeepNestingChain` - Deep parent-child chains (10+ levels)
- `TestSiblingSpanOrdering` - Sibling span temporal ordering
- `TestOrphanSpanHandling` - Spans with missing parents
- `TestComplexFamilyTree` - Complex trees with multiple branches
- `TestSpanTimestampIntegrity` - Timestamp consistency in hierarchies

**Why this matters:** Incorrect span relationships break distributed trace visualization.

## Test Helper Framework

### MockCollector
Synchronous collector for predictable testing:
```go
collector := NewMockCollector(t, "test-collector", 1000)
// Enables deterministic span ordering and verification
```

### MockService
Simulated service for integration testing:
```go
service := NewMockService("user-service", tracer)
service.SimulateDelay(100 * time.Millisecond)
service.SimulateFailure(0.1) // 10% failure rate
```

### Verification Helpers
Automated span tree validation:
```go
VerifySpanHierarchy(t, spans, expectedStructure)
VerifySpanTiming(t, spans, maxDuration)
VerifyTraceCompleteness(t, spans, expectedSpanCount)
```

## Running Integration Tests

### Complete Suite
```bash
# All integration tests (takes ~30 seconds)
go test -v ./testing/integration

# Specific test file
go test -v -run TestServiceMesh ./testing/integration

# Specific test case
go test -v -run TestAPIGatewayRouting ./testing/integration
```

### Continuous Integration
Integration tests run automatically:
- On every commit (core infrastructure tests)
- On pull requests (full integration suite)  
- Daily (extended scenario testing)
- Before releases (comprehensive validation)

## Test Patterns and Guidelines

### 1. Realistic Load Simulation
Tests use realistic concurrency and data volumes:
```go
// Not: 1 request to test basic functionality
// But: 100 concurrent requests over 10 seconds
```

### 2. Failure Injection
Every test includes failure scenarios:
```go
// Test normal case AND failure case
service.SimulateFailure(0.1) // 10% failure rate
```

### 3. Timing Validation  
Tests verify temporal relationships:
```go
// Child spans must start after parent
// Sibling spans should not overlap inappropriately
// Total trace time should be reasonable
```

### 4. Resource Cleanup
Every test cleans up resources:
```go
defer tracer.Shutdown()
defer collector.Close()
// Prevent test pollution and resource leaks
```

## Integration Test Philosophy

These tests answer the question: **"Does tracez work correctly in the real world?"**

- **Unit tests** verify individual function behavior
- **Integration tests** verify component interaction behavior  
- **Benchmark tests** verify performance claims
- **Integration tests** verify real-world usage patterns

Integration tests are the bridge between isolated unit tests and actual production usage. They catch problems that only emerge when components work together under realistic conditions.

## Contributing Integration Tests

When adding new integration tests:

1. **Base on real scenarios** - Use patterns from actual production systems
2. **Include failure cases** - Test what happens when things go wrong
3. **Validate relationships** - Verify span hierarchies and timing
4. **Use realistic scale** - Test with meaningful concurrency and data volumes
5. **Clean up resources** - Prevent test pollution and resource leaks

Integration tests are the final verification that tracez delivers on its promises in real-world environments.