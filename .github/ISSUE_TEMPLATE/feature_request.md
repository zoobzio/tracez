---
name: Feature request
about: Suggest an idea for this project
title: '[FEATURE] '
labels: 'enhancement'
assignees: ''

---

**Is your feature request related to a problem? Please describe.**
A clear and concise description of what the problem is. Ex. I'm always frustrated when [...]

**Describe the solution you'd like**
A clear and concise description of what you want to happen.

**Describe alternatives you've considered**
A clear and concise description of any alternative solutions or features you've considered.

**Use case**
Describe your specific use case:
- What type of traces are you trying to collect?
- What's your expected span volume/frequency?
- Do you need specific performance characteristics?
- Any concurrency requirements?
- Any specific context propagation needs?

**Proposed API (if applicable)**
```go
// Example of how you'd like the API to work
package main

import (
    "context"
    "github.com/zoobzio/tracez"
)

func main() {
    // Your proposed API usage
    tracer := tracez.New("my-service")
    defer tracer.Close()
    // ...
}
```

**Performance considerations**
- Expected span throughput requirements
- Memory usage constraints
- Collector buffering requirements
- Thread-safety needs
- Context propagation overhead

**Compatibility**
- Should this be backward compatible?
- Any breaking changes acceptable?
- Integration with existing tracing systems?
- OpenTelemetry compatibility needs?

**Additional context**
Add any other context, screenshots, or examples about the feature request here.

**Related issues**
Reference any related issues or discussions.