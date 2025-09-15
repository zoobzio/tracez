# Tracez Reliability Test Suite

A comprehensive reliability testing framework for the tracez distributed tracing library, implementing graduated destruction patterns for deterministic failure discovery.

## Overview

The reliability test suite validates tracez behavior under extreme conditions, stress scenarios, and failure modes. Following the CI-Safe, Stress-Capable pattern, tests adapt their intensity based on environment configuration.

## Test Categories

### 1. Collector Saturation Tests (`collector_saturation_test.go`)

Tests collector stability under extreme span ingestion:
- **Basic Level**: Backpressure validation, buffer growth verification, export operations under load
- **Stress Level**: Extreme ingestion rates, sustained pressure testing, cascade saturation across multiple collectors

**Key Scenarios:**
- Channel saturation triggering backpressure protection
- Buffer expansion under graduated load increases
- Export operations that don't interfere with collection
- Multi-collector coordination under stress

### 2. Span Memory Pressure Tests (`span_memory_pressure_test.go`)

Validates span operations under memory constraints:
- **Basic Level**: Tag map expansion, concurrent tag operations, span cleanup verification
- **Stress Level**: Massive tag loads, memory fragmentation detection, GC pressure testing

**Key Scenarios:**
- Tag map growth from 10 to 5000+ tags per span
- Concurrent tag modifications across multiple goroutines
- Memory cleanup verification after span completion
- Detection of memory fragmentation patterns

### 3. Tracer Lifecycle Tests (`tracer_lifecycle_test.go`)

Verifies tracer initialization, operation, and cleanup:
- **Basic Level**: Startup/shutdown cycles, collector management, ID pool behavior
- **Stress Level**: Rapid cycling, resource exhaustion, concurrent lifecycle operations

**Key Scenarios:**
- Clean tracer startup and shutdown sequences
- Collector registration and management within tracers
- ID pool efficiency and uniqueness under load
- Resource cleanup verification preventing goroutine leaks

### 4. Context Cascade Tests (`context_cascade_test.go`)

Tests trace context propagation under extreme conditions:
- **Basic Level**: Deep nesting validation, concurrent propagation, context corruption handling
- **Stress Level**: Extreme depth testing, massive fanout patterns, context operation storms

**Key Scenarios:**
- Deep call stack context propagation (100+ levels)
- Concurrent context propagation across multiple goroutines
- Graceful handling of corrupted or invalid contexts
- Wide span tree creation and management

### 5. Span Hierarchy Corruption Tests (`span_hierarchy_corruption_test.go`)

Validates parent-child relationships under stress:
- **Basic Level**: Orphaned span handling, hierarchy validation, concurrent hierarchy operations
- **Stress Level**: Massive hierarchies, corruption resilience, hierarchy operation storms

**Key Scenarios:**
- Orphaned span creation and management
- Parent-child relationship validation in complex trees
- Concurrent hierarchy building across goroutines
- System resilience against hierarchy corruption

## Usage

### Quick Start

```bash
# CI-safe basic validation
./run_reliability_tests.sh basic

# Comprehensive stress testing
./run_reliability_tests.sh stress

# Custom stress test configuration
./run_reliability_tests.sh stress -d 5m -g 500 -v
```

### Environment Configuration

Set `TRACEZ_RELIABILITY_LEVEL` to control test intensity:

```bash
# Basic reliability testing (CI-friendly)
export TRACEZ_RELIABILITY_LEVEL=basic
go test ./testing/reliability/...

# Stress testing (manual/scheduled)
export TRACEZ_RELIABILITY_LEVEL=stress
go test ./testing/reliability/...

# Skip reliability tests (default)
unset TRACEZ_RELIABILITY_LEVEL
go test ./testing/reliability/...  # Will skip all tests
```

### Configuration Options

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `TRACEZ_RELIABILITY_LEVEL` | "" | Test level: `basic`, `stress`, or unset to skip |
| `TRACEZ_RELIABILITY_DURATION` | "30s" | Duration for stress tests |
| `TRACEZ_RELIABILITY_MAX_GOROUTINES` | "100" | Maximum concurrent goroutines |
| `TRACEZ_RELIABILITY_MAX_MEMORY_MB` | "512" | Memory limit for tests |
| `TRACEZ_RELIABILITY_FAILURE_THRESHOLD` | "0.05" | Acceptable failure rate (0.0-1.0) |
| `TRACEZ_RELIABILITY_VERBOSE` | "false" | Enable verbose output |
| `TRACEZ_RELIABILITY_OUTPUT_DIR` | "./reliability-results" | Output directory for results |

## Test Methodology

### Graduated Destruction

Tests employ graduated destruction patterns to systematically discover failure points:

1. **Baseline Establishment**: Normal operation verification
2. **Gradual Pressure Increase**: Progressive load increases
3. **Breaking Point Discovery**: Systematic failure point identification
4. **Recovery Validation**: System recovery capability verification
5. **Edge Case Exploration**: Boundary condition testing

### Example: Collector Saturation Progression

```go
// Phase 1: Baseline (10 spans)
// Phase 2: Light load (100 spans)  
// Phase 3: Heavy load (1000 spans)
// Phase 4: Extreme load (10000+ spans)
// Phase 5: Recovery verification
```

### Failure Classification

Tests categorize failures by severity and impact:

- **Catastrophic**: System crash, panic, deadlock
- **Degradation**: Performance loss, resource leaks
- **Data Loss**: Span loss, corruption, inconsistency
- **Recovery**: Inability to recover from failure states

## CI Integration

### GitHub Actions Example

```yaml
name: Reliability Tests

on:
  push:
    branches: [ main ]
  pull_request:
    branches: [ main ]
  schedule:
    - cron: '0 2 * * *'  # Nightly stress tests

jobs:
  basic-reliability:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v3
        with:
          go-version: '1.21'
      - name: Run Basic Reliability Tests
        run: |
          cd testing/reliability
          ./run_reliability_tests.sh basic
        env:
          TRACEZ_RELIABILITY_LEVEL: basic

  stress-reliability:
    runs-on: ubuntu-latest
    if: github.event_name == 'schedule'
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v3
        with:
          go-version: '1.21'
      - name: Run Stress Tests
        run: |
          cd testing/reliability
          ./run_reliability_tests.sh stress -d 10m
        env:
          TRACEZ_RELIABILITY_LEVEL: stress
          TRACEZ_RELIABILITY_DURATION: 10m
```

## Performance Benchmarks

### Expected Performance Baselines

| Test Category | Basic Level | Stress Level |
|---------------|-------------|--------------|
| Collector Throughput | >10K spans/sec | >50K spans/sec |
| Tag Operations | >1K ops/sec | >10K ops/sec |
| Context Propagation | >5K ops/sec | >25K ops/sec |
| Hierarchy Creation | >1K spans/sec | >5K spans/sec |
| Memory Growth | <20% | <50% |

### Failure Thresholds

- **Memory Growth**: <50% over baseline
- **Performance Degradation**: <50% of baseline performance
- **Data Loss**: <5% span loss under extreme load
- **Recovery Time**: <100ms for system recovery

## Troubleshooting

### Common Issues

**Test Timeouts**
```bash
# Reduce test duration
export TRACEZ_RELIABILITY_DURATION=10s
```

**Memory Limitations**
```bash
# Reduce memory limit
export TRACEZ_RELIABILITY_MAX_MEMORY_MB=256
```

**High Failure Rates**
```bash
# Increase failure threshold temporarily
export TRACEZ_RELIABILITY_FAILURE_THRESHOLD=0.10
```

### Debug Mode

Enable verbose output for detailed test information:

```bash
export TRACEZ_RELIABILITY_VERBOSE=true
./run_reliability_tests.sh stress -v
```

## Extending Tests

### Adding New Test Categories

1. Create new test file: `new_category_test.go`
2. Follow the pattern:
   ```go
   func TestNewCategory(t *testing.T) {
       config := getReliabilityConfig()
       
       switch config.Level {
       case "basic":
           t.Run("basic_test", testBasicScenario)
       case "stress":
           t.Run("stress_test", testStressScenario)
       default:
           t.Skip("TRACEZ_RELIABILITY_LEVEL not set")
       }
   }
   ```

### Adding New Scenarios

1. Implement graduated destruction pattern
2. Include baseline, pressure, and recovery phases
3. Add comprehensive logging and metrics
4. Validate against performance thresholds

## Output and Reporting

### Test Results Structure

```
reliability-results/
├── reliability-test-results.txt    # Raw test output
├── reliability-test-results.json   # JSON formatted results
└── reliability-report.md           # Formatted report
```

### Report Sections

- **Configuration Summary**: Test parameters and environment
- **Performance Metrics**: Throughput, latency, resource usage
- **Failure Analysis**: Categorized failures and root causes
- **Recommendations**: System improvements and optimizations

## Architecture Integration

The reliability test suite integrates with tracez architecture:

- **Collector Testing**: Validates backpressure and buffer management
- **Tracer Testing**: Verifies lifecycle and resource management
- **Span Testing**: Confirms thread-safety and memory efficiency
- **Context Testing**: Ensures propagation integrity
- **Integration Testing**: End-to-end scenario validation

## Maintenance

### Regular Tasks

1. **Baseline Updates**: Update performance baselines quarterly
2. **Threshold Tuning**: Adjust failure thresholds based on historical data
3. **Scenario Expansion**: Add new failure scenarios based on production issues
4. **Documentation Updates**: Keep test documentation current with code changes

### Performance Monitoring

Track key metrics over time:
- Test execution duration trends
- Memory usage patterns
- Failure rate evolution
- Performance baseline drift

The reliability test suite ensures tracez remains stable and performant under all conditions, from normal operation to extreme stress scenarios.