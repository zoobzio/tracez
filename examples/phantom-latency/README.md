# The Phantom Latency

## Monday Morning, October 17, 2024

**10:47 AM, PayFlow War Room**

The CEO's voice cuts through the speaker phone: "MegaMart is threatening to terminate our contract. Their flash sale is failing. Fix this. Now."

Every dashboard shows green. API latency: 87ms p99. Error rate: 0.02%. CPU and memory: normal. Database queries: fast.

But customers are seeing 14-second checkout times. Support is drowning in complaints. Videos show payment spinners timing out.

"It must be their network," someone suggests. But MegaMart sends HAR files. Browser DevTools screenshots. The requests are taking 14 seconds on their end. Our metrics say 87 milliseconds.

Four hours into the investigation, a junior engineer asks: "What about the StreamPay SDK?"

"It's battle-tested. In production three years."

"But do we measure it?"

Silence.

## The Discovery

Deep in `vendor/streampay-sdk-v3/`, last modified in 2021, we find the smoking gun:

```go
func (c *Client) Execute(req *Request) (*Response, error) {
    attempts := 0
    backoff := 100 * time.Millisecond
    maxAttempts := c.config.MaxRetries // Default: 5
    
    for attempts < maxAttempts {
        resp, err := c.doRequest(req)
        
        // StreamPay returns errors as HTTP 200 with error body
        if err == nil && resp.StatusCode == 200 {
            if resp.Body.Error != nil {
                if resp.Body.Error.Code == "RATE_LIMIT" {
                    time.Sleep(backoff)
                    backoff = min(backoff * 2, 10 * time.Second)
                    attempts++
                    continue
                }
            }
        }
        // ... more retry logic
    }
}
```

The SDK retries failed requests with exponential backoff. During flash sales, we hit StreamPay's rate limits. Each payment attempt triggers 5 retries: 100ms + 200ms + 400ms + 800ms + 1600ms = 3.1 seconds of backoff alone. Plus the actual request times.

Our metrics measure our service layer - 87ms to call the SDK. The SDK takes 10-15 seconds internally. We never see it.

## The Pattern

**What metrics showed:**
```
PaymentService.ProcessPayment: 87ms [SUCCESS]
```

**What was actually happening:**
```
PaymentService.ProcessPayment [13.7s total]
└── StreamPaySDK.Execute [13.7s hidden]
    ├── Attempt 1: 62ms (RATE_LIMIT) + 100ms backoff
    ├── Attempt 2: 62ms (RATE_LIMIT) + 200ms backoff
    ├── Attempt 3: 62ms (RATE_LIMIT) + 400ms backoff
    ├── Attempt 4: 62ms (RATE_LIMIT) + 800ms backoff
    └── Attempt 5: 62ms (SUCCESS) + 1600ms backoff
```

## The Lesson

Third-party SDKs are black boxes. They live in vendor directories, untouchable and uninstrumented. Their retry loops, backoff strategies, and error handling are invisible to your metrics.

This example demonstrates how tracez reveals what metrics miss - the hidden behavior deep in third-party code that can destroy your SLA while your dashboards show everything is fine.

## Running the Example

```bash
go test -v
```

Watch how:
1. Service metrics show sub-100ms latency
2. Customer reality is 10+ seconds
3. tracez reveals the hidden retry loops
4. The cascade effect gets worse under load

The vendor SDK we trusted for 3 years was gaslighting our metrics the entire time.