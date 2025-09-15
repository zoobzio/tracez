# Tracez Reliability Test Report

**Generated:** 2025-09-15 12:44:54  
**Level:** basic  
**Duration:** 30s  
**Max Goroutines:** 100  
**Memory Limit:** 512MB  
**Failure Threshold:** 0.05  

## Test Configuration

The reliability tests were executed using the CI-Safe, Stress-Capable pattern:

### Test Categories

1. **Collector Saturation** - Verify collector remains stable under extreme span ingestion
2. **Span Memory Pressure** - Test span operations under memory constraints  
3. **Tracer Lifecycle** - Verify tracer initialization, operation, and cleanup
4. **Context Cascade** - Test trace context propagation under extreme conditions
5. **Span Hierarchy Corruption** - Verify parent-child relationships under stress

### Test Levels

- **Basic (basic)**: CI-safe validation tests
- **Stress**: Production-level stress testing

## Results

### Test Output

```
{"Time":"2025-09-15T12:44:54.661687105-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/concurrent_tags","Output":"=== RUN   TestSpanMemoryPressure/concurrent_tags\n"}
{"Time":"2025-09-15T12:44:54.718797174-07:00","Action":"run","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup"}
{"Time":"2025-09-15T12:44:54.718815874-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup","Output":"=== RUN   TestSpanMemoryPressure/span_cleanup\n"}
{"Time":"2025-09-15T12:44:54.727915645-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup","Output":"    span_memory_pressure_test.go:219: Memory usage:\n"}
{"Time":"2025-09-15T12:44:54.727923525-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup","Output":"    span_memory_pressure_test.go:220:   Initial: 2236416 bytes\n"}
{"Time":"2025-09-15T12:44:54.727932335-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup","Output":"    span_memory_pressure_test.go:221:   After spans: 3563520 bytes\n"}
{"Time":"2025-09-15T12:44:54.727934635-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup","Output":"    span_memory_pressure_test.go:222:   After cleanup: 3547136 bytes\n"}
{"Time":"2025-09-15T12:44:54.727937195-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup","Output":"    span_memory_pressure_test.go:229: Reliability Issue: High memory growth 58.6% (potential memory pressure)\n"}
{"Time":"2025-09-15T12:44:54.727948815-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure","Output":"--- PASS: TestSpanMemoryPressure (0.07s)\n"}
{"Time":"2025-09-15T12:44:54.727954055-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion","Output":"    --- PASS: TestSpanMemoryPressure/tag_expansion (0.01s)\n"}
{"Time":"2025-09-15T12:44:54.727957005-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion/small","Output":"        --- PASS: TestSpanMemoryPressure/tag_expansion/small (0.00s)\n"}
{"Time":"2025-09-15T12:44:54.727959845-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion/small","Elapsed":0}
{"Time":"2025-09-15T12:44:54.727965995-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion/medium","Output":"        --- PASS: TestSpanMemoryPressure/tag_expansion/medium (0.00s)\n"}
{"Time":"2025-09-15T12:44:54.727968475-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion/medium","Elapsed":0}
{"Time":"2025-09-15T12:44:54.727970555-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion/large","Output":"        --- PASS: TestSpanMemoryPressure/tag_expansion/large (0.00s)\n"}
{"Time":"2025-09-15T12:44:54.727972985-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion/large","Elapsed":0}
{"Time":"2025-09-15T12:44:54.727974845-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion/extreme","Output":"        --- PASS: TestSpanMemoryPressure/tag_expansion/extreme (0.00s)\n"}
{"Time":"2025-09-15T12:44:54.727977235-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion/extreme","Elapsed":0}
{"Time":"2025-09-15T12:44:54.727978935-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/tag_expansion","Elapsed":0.01}
{"Time":"2025-09-15T12:44:54.727981875-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/concurrent_tags","Output":"    --- PASS: TestSpanMemoryPressure/concurrent_tags (0.06s)\n"}
{"Time":"2025-09-15T12:44:54.727984245-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/concurrent_tags","Elapsed":0.06}
{"Time":"2025-09-15T12:44:54.727989895-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup","Output":"    --- PASS: TestSpanMemoryPressure/span_cleanup (0.01s)\n"}
{"Time":"2025-09-15T12:44:54.727993985-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure/span_cleanup","Elapsed":0.01}
{"Time":"2025-09-15T12:44:54.727999335-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestSpanMemoryPressure","Elapsed":0.07}
{"Time":"2025-09-15T12:44:54.728001905-07:00","Action":"run","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle"}
{"Time":"2025-09-15T12:44:54.728003865-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle","Output":"=== RUN   TestTracerLifecycle\n"}
{"Time":"2025-09-15T12:44:54.728006065-07:00","Action":"run","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/startup_shutdown"}
{"Time":"2025-09-15T12:44:54.728007905-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/startup_shutdown","Output":"=== RUN   TestTracerLifecycle/startup_shutdown\n"}
{"Time":"2025-09-15T12:44:54.738414939-07:00","Action":"run","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management"}
{"Time":"2025-09-15T12:44:54.738423659-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management","Output":"=== RUN   TestTracerLifecycle/collector_management\n"}
{"Time":"2025-09-15T12:44:54.789194564-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management","Output":"    tracer_lifecycle_test.go:112: Collector 0: 100 buffered, 0 dropped\n"}
{"Time":"2025-09-15T12:44:54.789204154-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management","Output":"    tracer_lifecycle_test.go:112: Collector 1: 100 buffered, 0 dropped\n"}
{"Time":"2025-09-15T12:44:54.789207694-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management","Output":"    tracer_lifecycle_test.go:112: Collector 2: 100 buffered, 0 dropped\n"}
{"Time":"2025-09-15T12:44:54.789212134-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management","Output":"    tracer_lifecycle_test.go:112: Collector 3: 100 buffered, 0 dropped\n"}
{"Time":"2025-09-15T12:44:54.789215294-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management","Output":"    tracer_lifecycle_test.go:112: Collector 4: 100 buffered, 0 dropped\n"}
{"Time":"2025-09-15T12:44:54.789236264-07:00","Action":"run","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/id_pool_behavior"}
{"Time":"2025-09-15T12:44:54.789238924-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/id_pool_behavior","Output":"=== RUN   TestTracerLifecycle/id_pool_behavior\n"}
{"Time":"2025-09-15T12:44:54.790198566-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/id_pool_behavior","Output":"    tracer_lifecycle_test.go:164: Generated 1000 unique trace IDs and 1000 unique span IDs\n"}
{"Time":"2025-09-15T12:44:54.794727877-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/id_pool_behavior","Output":"    tracer_lifecycle_test.go:203: No ID collisions detected in concurrent generation of 1200 IDs\n"}
{"Time":"2025-09-15T12:44:54.794757247-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle","Output":"--- PASS: TestTracerLifecycle (0.07s)\n"}
{"Time":"2025-09-15T12:44:54.794761627-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/startup_shutdown","Output":"    --- PASS: TestTracerLifecycle/startup_shutdown (0.01s)\n"}
{"Time":"2025-09-15T12:44:54.794765747-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/startup_shutdown","Elapsed":0.01}
{"Time":"2025-09-15T12:44:54.794771707-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management","Output":"    --- PASS: TestTracerLifecycle/collector_management (0.05s)\n"}
{"Time":"2025-09-15T12:44:54.794774737-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/collector_management","Elapsed":0.05}
{"Time":"2025-09-15T12:44:54.794777647-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/id_pool_behavior","Output":"    --- PASS: TestTracerLifecycle/id_pool_behavior (0.01s)\n"}
{"Time":"2025-09-15T12:44:54.794780437-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle/id_pool_behavior","Elapsed":0.01}
{"Time":"2025-09-15T12:44:54.794782937-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Test":"TestTracerLifecycle","Elapsed":0.07}
{"Time":"2025-09-15T12:44:54.794785887-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Output":"PASS\n"}
{"Time":"2025-09-15T12:44:54.795444448-07:00","Action":"output","Package":"github.com/zoobzio/tracez/testing/reliability","Output":"ok  \tgithub.com/zoobzio/tracez/testing/reliability\t1.305s\n"}
{"Time":"2025-09-15T12:44:54.795852299-07:00","Action":"pass","Package":"github.com/zoobzio/tracez/testing/reliability","Elapsed":1.305}
```
