# Security Policy

## Supported Versions

We actively support security updates for the following versions of tracez:

| Version | Supported          |
| ------- | ------------------ |
| 1.0.x   | :white_check_mark: |
| < 1.0   | :x:                |

## Reporting a Vulnerability

We take security seriously and appreciate responsible disclosure of security vulnerabilities.

### How to Report

**Please do NOT report security vulnerabilities through public GitHub issues.**

Instead, please report security vulnerabilities by:

1. **Email**: Send details to security@zoobzio.com
2. **Subject Line**: Include "[SECURITY] tracez vulnerability report"
3. **Content**: Include as much detail as possible (see template below)

### Report Template

```
Subject: [SECURITY] tracez vulnerability report

**Vulnerability Description:**
Brief description of the vulnerability

**Affected Components:**
- Component/function affected
- Version(s) affected

**Attack Vector:**
How the vulnerability can be exploited

**Impact Assessment:**
- Data exposure risk: None/Low/Medium/High/Critical  
- Service availability risk: None/Low/Medium/High/Critical
- System integrity risk: None/Low/Medium/High/Critical

**Proof of Concept:**
Step-by-step reproduction (if available)
Code example demonstrating the issue

**Suggested Fix:**
Your recommendation for addressing the issue (if available)

**Additional Context:**
Any other relevant information
```

### Response Timeline

- **Initial Response**: Within 48 hours
- **Vulnerability Assessment**: Within 1 week
- **Fix Development**: Target 2 weeks for critical, 4 weeks for others
- **Public Disclosure**: After fix is released and users have time to update

### Disclosure Policy

- We follow responsible disclosure practices
- Public disclosure occurs only after:
  1. Fix is developed and tested
  2. Fix is released in a new version
  3. Sufficient time for users to update (typically 2-4 weeks)
- Credit will be given to security researchers (if desired)

## Security Considerations for Users

### General Security

tracez is designed with security in mind:

- **No Network Communication**: tracez itself makes no network calls
- **Memory Safety**: Go's memory safety prevents buffer overflows
- **No Code Execution**: No dynamic code evaluation or execution
- **Standard Library Only**: No external dependencies that could introduce vulnerabilities

### Span Data Security

**Important**: tracez collects and processes span data that may contain sensitive information.

#### Data Handling Best Practices

1. **Sensitive Data in Tags**
   ```go
   // DON'T: Include sensitive data in tags
   span.SetTag("user.password", password)        // ❌ Never do this
   span.SetTag("api.key", apiKey)               // ❌ Never do this
   span.SetTag("credit.card", creditCardNumber) // ❌ Never do this
   
   // DO: Use safe identifiers or sanitized data
   span.SetTag("user.id", userID)               // ✅ Safe identifier
   span.SetTag("api.endpoint", "/api/users")    // ✅ Non-sensitive path
   span.SetTag("query.table", "users")          // ✅ Schema information
   ```

2. **Span Name Security**
   ```go
   // DON'T: Include sensitive data in span names
   tracer.StartSpan(ctx, "query-user-password-" + password) // ❌ Bad
   
   // DO: Use generic operation names
   tracer.StartSpan(ctx, "database.query")                  // ✅ Good
   tracer.StartSpan(ctx, "user.authenticate")               // ✅ Good
   ```

3. **Export Destination Security**
   - Ensure your export destination (logs, monitoring systems) is secure
   - Use encrypted transport for span data transmission
   - Implement proper access controls for trace data storage
   - Consider data retention policies for trace data

#### Memory Security

- **Buffer Cleanup**: tracez automatically cleans up span buffers
- **Deep Copying**: Exported spans are deep copies, preventing accidental data sharing
- **Bounded Memory**: Backpressure protection prevents unbounded memory growth

### Deployment Security

#### Production Recommendations

1. **Resource Limits**
   ```go
   // Set reasonable buffer limits to prevent memory exhaustion
   collector := tracez.NewCollector("exporter", 1000) // Reasonable limit
   ```

2. **Monitoring**
   - Monitor dropped span counts: `collector.DroppedCount()`
   - Set up alerts for unusual tracing overhead
   - Monitor memory usage of tracing components

3. **Error Handling**
   ```go
   // Don't expose internal errors that might leak information
   spans := collector.Export()
   if len(spans) == 0 {
       log.Info("No spans to export") // ✅ Safe logging
       // Don't log internal state details in production
   }
   ```

### Dependency Security

tracez has **zero external dependencies** beyond Go's standard library:
- No risk of vulnerable third-party packages
- No supply chain attack vectors through dependencies
- Easier security auditing with smaller codebase

### Thread Safety and Concurrency

All public APIs are thread-safe, preventing race conditions that could lead to:
- Data corruption
- Memory access violations  
- Unpredictable behavior in multi-threaded applications

## Security Auditing

### Self-Audit Checklist

If you're conducting a security audit of tracez in your organization:

#### Code Review Focus Areas

1. **Data Flow**: Trace span data from creation to export
2. **Memory Management**: Buffer handling and cleanup
3. **Concurrency**: Thread safety mechanisms
4. **Error Handling**: How failures are handled and logged
5. **Resource Limits**: Protection against resource exhaustion

#### Testing Focus Areas

1. **Race Conditions**: Run tests with `-race` flag
2. **Memory Leaks**: Long-running tests with memory profiling
3. **Resource Exhaustion**: High-load scenarios with limited resources
4. **Edge Cases**: Malformed inputs and unexpected conditions

### Third-Party Security Assessment

We welcome third-party security assessments. If you're conducting a professional security audit:

1. Contact us beforehand to coordinate
2. We can provide additional technical context
3. We're happy to discuss findings and remediation
4. Results can be shared publicly if desired

## Incident Response

In the event of a confirmed security vulnerability:

### Immediate Response
1. Assess severity and impact
2. Develop and test fix
3. Prepare security advisory
4. Coordinate release timeline

### Communication
1. Notify affected users through GitHub Security Advisories
2. Update documentation with security recommendations
3. Provide clear upgrade instructions
4. Credit security researchers (with permission)

### Post-Incident
1. Review what allowed the vulnerability
2. Improve development processes if needed
3. Update security guidelines
4. Consider additional security measures

## Security Resources

### Reporting Channels
- **Email**: security@zoobzio.com  
- **PGP**: Available on request for sensitive communications

### Security Updates
- Security fixes are released as patch versions
- Critical security updates may be backported to older supported versions
- Security advisories published through GitHub Security Advisories

### Community
- Security discussions welcome in GitHub Discussions
- General security questions can be asked in GitHub Issues
- Follow repository for security update notifications

## Acknowledgments

We thank the security research community for helping keep tracez secure. Responsible disclosure helps protect all users of the library.

Special thanks to security researchers who have helped improve tracez security (list will be updated as reports are received and resolved).