# tracez Documentation

Minimal distributed tracing for Go applications - when OpenTelemetry feels like overkill.

## Quick Navigation

Choose your path based on what you need:

### 🚀 New to Tracing?
Start with **[fundamentals/getting-started.md](fundamentals/getting-started.md)** - Get your first trace running in 5 minutes.

### 🔧 Adding Tracing to Your App?
Go to **[guides/instrumentation.md](guides/instrumentation.md)** - Step-by-step instrumentation for common services.

### 🐛 Debugging a Specific Problem?
Check **[patterns/](patterns/)** for targeted solutions:
- [HTTP service issues](patterns/http-tracing.md)
- [Database performance problems](patterns/database-tracing.md) 
- [Error debugging](patterns/error-handling.md)
- [High volume tracing](patterns/sampling.md)

### 📖 Need API Details?
Reference **[reference/api.md](reference/api.md)** for complete function signatures and parameters.

## Learning Path

1. **[Fundamentals](fundamentals/)** - Core concepts and basic setup
   - [Core Concepts](fundamentals/concepts.md) - Spans, traces, context
   - [Getting Started](fundamentals/getting-started.md) - Your first trace
   - [Examples Guide](fundamentals/examples-guide.md) - How to run and understand examples

2. **[Guides](guides/)** - Practical instrumentation
   - [Instrumentation](guides/instrumentation.md) - Adding tracing to services
   - [Context Propagation](guides/context-propagation.md) - Goroutines and service calls
   - [Performance](guides/performance.md) - Optimization patterns
   - [Production](guides/production.md) - Deployment and operations

3. **[Patterns](patterns/)** - Problem-focused solutions
   - [HTTP Tracing](patterns/http-tracing.md) - Web service patterns
   - [Database Tracing](patterns/database-tracing.md) - Query optimization and N+1 detection
   - [Error Handling](patterns/error-handling.md) - Error visibility patterns
   - [Sampling](patterns/sampling.md) - Volume reduction strategies

4. **[Reference](reference/)** - Complete technical documentation
   - [API Reference](reference/api.md) - Complete function documentation
   - [Architecture](reference/architecture.md) - Internal design and data flow
   - [Troubleshooting](reference/troubleshooting.md) - Error resolution guide

## Examples

Working examples are in the [examples/](../examples/) directory:
- [HTTP Middleware](../examples/http-middleware/) - Complete web server with tracing
- [Database Patterns](../examples/database-patterns/) - Query tracing and N+1 detection
- [Worker Pool](../examples/worker-pool/) - Concurrent processing with tracing

All examples include runnable code and demonstrate real-world usage patterns.