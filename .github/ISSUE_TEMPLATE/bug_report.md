---
name: Bug report
about: Create a report to help us improve
title: '[BUG] '
labels: 'bug'
assignees: ''

---

**Describe the bug**
A clear and concise description of what the bug is.

**To Reproduce**
Steps to reproduce the behavior:
1. Create tracer with '...'
2. Start spans with '...'
3. Perform operations '...'
4. See error

**Code Example**
```go
// Minimal code example that reproduces the issue
package main

import (
    "context"
    "github.com/zoobzio/tracez"
)

func main() {
    // Your code here
    tracer := tracez.New("my-service")
    defer tracer.Close()
    // ...
}
```

**Expected behavior**
A clear and concise description of what you expected to happen.

**Actual behavior**
What actually happened, including any error messages, stack traces, or incorrect tracing behavior.

**Environment:**
 - OS: [e.g. macOS, Linux, Windows]
 - Architecture: [e.g. amd64, arm64]
 - Go version: [e.g. 1.21.0]
 - tracez version: [e.g. v1.0.0]
 - Concurrency level: [e.g. single-threaded, 10 goroutines, high contention]

**Tracing Information (if applicable)**
 - Number of concurrent goroutines creating spans
 - Span creation frequency (e.g. 1000/sec per goroutine)
 - Are you experiencing data corruption or lost spans?
 - Any collector buffer overflows?
 - Context propagation issues?

**Additional context**
Add any other context about the problem here, including:
- Performance characteristics observed
- Memory usage patterns
- Any custom collectors or configurations
- Related issues or similar problems