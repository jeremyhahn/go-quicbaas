# Security Hardening — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/security-hardening/`.

QuicBaaS includes comprehensive security middleware for the REST API, implementing industry best practices for HTTP security headers, CORS, rate limiting, and request handling.

## Security Headers Middleware

The `SecurityHeadersMiddleware` automatically adds security headers to all HTTP responses to protect against common web vulnerabilities.

### Enabled Headers

| Header | Value | Purpose |
|--------|-------|---------|
| Strict-Transport-Security | max-age=31536000; includeSubDomains | Force HTTPS for 1 year |
| X-Frame-Options | DENY | Prevent clickjacking |
| X-Content-Type-Options | nosniff | Prevent MIME sniffing |
| X-XSS-Protection | 1; mode=block | Enable browser XSS filter |
| Referrer-Policy | strict-origin-when-cross-origin | Control referrer information |
| Content-Security-Policy | default-src 'self' | Restrict content sources |
| Permissions-Policy | geolocation=(), microphone=(), camera=() | Disable sensitive browser features |

### Configuration

Security headers are enabled by default. No configuration required.

### Usage

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"

// Apply security headers middleware
handler := rest.SecurityHeadersMiddleware()(yourHandler)
```

## HSTS (HTTP Strict Transport Security)

Forces browsers to use HTTPS for all future requests.

```
Strict-Transport-Security: max-age=31536000; includeSubDomains
```

- **max-age=31536000**: Cache HSTS policy for 1 year
- **includeSubDomains**: Apply to all subdomains

### Considerations

- Only enable when HTTPS is fully configured
- Consider HSTS preloading for production deployments
- Test thoroughly before enabling on production

## Clickjacking Protection

Prevents the page from being embedded in iframes.

```
X-Frame-Options: DENY
```

### Options

| Value | Description |
|-------|-------------|
| DENY | Never allow framing |
| SAMEORIGIN | Allow framing by same origin |
| ALLOW-FROM uri | Allow framing by specific origin (deprecated) |

QuicBaaS uses `DENY` as the API should never be framed.

## MIME Sniffing Protection

Prevents browsers from interpreting files as different MIME types.

```
X-Content-Type-Options: nosniff
```

This prevents:
- XSS attacks via malicious file uploads
- Drive-by downloads
- Content-type confusion attacks

## XSS Protection

Enables the browser's built-in XSS filter.

```
X-XSS-Protection: 1; mode=block
```

- **1**: Enable XSS filter
- **mode=block**: Block the page if XSS is detected

Note: Modern browsers use CSP instead, but this provides defense-in-depth.

## Referrer Policy

Controls how much referrer information is sent with requests.

```
Referrer-Policy: strict-origin-when-cross-origin
```

| Policy | Behavior |
|--------|----------|
| strict-origin-when-cross-origin | Full URL to same origin, origin only cross-origin |
| no-referrer | Never send referrer |
| origin | Send origin only |
| same-origin | Full URL to same origin, nothing cross-origin |

## Content Security Policy

Restricts which resources can be loaded.

```
Content-Security-Policy: default-src 'self'
```

This prevents:
- Cross-site scripting (XSS)
- Data injection attacks
- Malicious resource loading

For API-only endpoints, the restrictive `default-src 'self'` is appropriate.

## Permissions Policy

Disables sensitive browser features.

```
Permissions-Policy: geolocation=(), microphone=(), camera=()
```

Disabled features:
- Geolocation access
- Microphone access
- Camera access

## CORS Middleware

Controls cross-origin resource sharing.

### Configuration

```go
// Allow specific origins
handler := rest.CORSMiddleware([]string{
    "https://app.example.com",
    "https://admin.example.com",
})(yourHandler)

// Allow all origins (not recommended for production)
handler := rest.CORSMiddleware(nil)(yourHandler)
```

### Headers Set

```
Access-Control-Allow-Origin: https://app.example.com
Access-Control-Allow-Methods: GET, POST, PUT, DELETE, OPTIONS
Access-Control-Allow-Headers: Content-Type, Authorization, X-Request-ID
Access-Control-Max-Age: 3600
```

### Preflight Handling

OPTIONS requests are handled automatically with 204 No Content response.

## Rate Limiting Middleware

A token bucket per source IP. It is **on by default** at 1000 requests per
second per source, with a burst of the same size (go-quicbaas#158).

### Configuration

```yaml
server:
  rest:
    rate_limit: 1000   # requests per second per source IP
```

| `server.rest.rate_limit` | Effect |
|---|---|
| omitted or `0` | Default limit: `rest.DefaultRateLimit` (1000) |
| positive | That many requests per second, with a burst of the same size |
| `-1` | Opt-out: the global limiter is off |
| below `-1` | Rejected at config validation |

In Go, `rest.Config.RateLimit` uses the same values. Zero resolves to
`rest.DefaultRateLimit`, and `rest.RateLimitDisabled` (`-1`) turns the
limiter off. `Config.EffectiveRateLimit` returns the rate that applies.

### How the default was chosen

The default comes from measuring the Go SDK against the REST router.
`NewClient` sends no request, and each SDK call sends exactly one, so an
interactive client sends a few requests per user action. On loopback, one
sequential client reached about 5,600 requests per second (about 0.18 ms per
request). Over a network a sequential client is limited by the round trip, so
at a round trip of 1 ms or more it cannot exceed 1000 requests per second.
The default therefore never throttles a single sequential client over a real
network. It does throttle one source that sends many requests at once: eight
goroutines sharing one client reached about 31,000 requests per second.

### Behavior

- Keyed on the resolved client address (see [Client IP Detection](#client-ip-detection)).
  IPv6 sources are grouped by /64.
- The bucket map is bounded at 100,000 sources. New sources beyond the cap
  share one overflow bucket. Idle buckets are cleaned up.
- Returns 429 Too Many Requests when the limit is exceeded. The Go SDK reports
  this as `CodeRateLimited` and does not retry.
- `/oauth2/token`, `/oauth2/revoke` and `/oauth2/introspect` keep their own
  stricter per-source limiter, keyed on the transport peer, whatever the
  global setting. It is defense in depth for the client-authentication KDF.
- Behind a load balancer, list it in `server.trusted_proxies`. If it is not
  listed, every request appears to come from the balancer, and all clients
  share one bucket.

### Middleware use

```go
// 100 requests per second per IP
handler := rest.RateLimitMiddleware(100)(yourHandler)

// A rate of 0 or less passes every request through
handler := rest.RateLimitMiddleware(0)(yourHandler)
```

### Response on Rate Limit

```json
{
  "error": "rate limit exceeded: 100 requests per 1s",
  "timestamp": "2026-10-12T12:00:00Z",
  "request_id": "…",
  "retryable": false
}
```

## Request ID Middleware

Adds unique request IDs for tracing and correlation.

### Configuration

```go
handler := rest.RequestIDMiddleware()(yourHandler)
```

### Behavior

1. Checks for `X-Request-ID` header in request
2. If not present, generates new UUID
3. Adds `X-Request-ID` to response headers
4. Stores request ID in context

### Usage

```go
requestID := rest.GetRequestID(r.Context())
```

## Timeout Middleware

Enforces request processing timeouts.

### Configuration

```go
// 30 second timeout
handler := rest.TimeoutMiddleware(30 * time.Second)(yourHandler)
```

### Behavior

- Applies timeout if context doesn't have deadline
- Returns 408 Request Timeout on expiry

## Recovery Middleware

Recovers from panics in handlers.

### Configuration

```go
handler := rest.RecoveryMiddleware(logger)(yourHandler)
```

### Behavior

- Catches panics in handlers
- Logs error with stack trace
- Returns 500 Internal Server Error

## Logging Middleware

Logs all HTTP requests and responses.

### Configuration

```go
handler := rest.LoggingMiddleware(logger)(yourHandler)
```

### Logged Information

**Request:**
- Method
- Path
- Remote address
- Request ID

**Response:**
- Status code
- Duration
- Bytes written
- Shard ID (if applicable)

## Metrics Middleware

Collects Prometheus metrics for HTTP requests.

### Configuration

```go
handler := rest.MetricsMiddleware()(yourHandler)
```

### Metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| quicbaas_http_requests_total | Counter | method, path, status | Total HTTP requests |
| quicbaas_http_request_duration_seconds | Histogram | method, path | Request duration |
| quicbaas_http_requests_in_flight | Gauge | - | Active requests |

## Middleware Chain

Apply middleware in the correct order:

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"

// Build middleware chain (order matters!)
handler := yourHandler

// Recovery should be outermost to catch all panics
handler = rest.RecoveryMiddleware(logger)(handler)

// Request ID early for logging correlation
handler = rest.RequestIDMiddleware()(handler)

// Logging after request ID
handler = rest.LoggingMiddleware(logger)(handler)

// Metrics after logging
handler = rest.MetricsMiddleware()(handler)

// Timeout before business logic
handler = rest.TimeoutMiddleware(30 * time.Second)(handler)

// Rate limiting before expensive operations
handler = rest.RateLimitMiddleware(100)(handler)

// CORS before security headers
handler = rest.CORSMiddleware(allowedOrigins)(handler)

// Security headers last (applied to all responses)
handler = rest.SecurityHeadersMiddleware()(handler)
```

## Client IP Detection

`ClientIPMiddleware` resolves the client address once per request. Logging,
tracing, audit records and the rate limiter all use that one value.

- By default it is the transport peer (`RemoteAddr`). `X-Forwarded-For` and
  `X-Real-IP` are ignored, so a caller cannot choose its own address.
- When the peer is in `server.trusted_proxies` (CIDRs or bare addresses),
  the client those headers name is used instead (go-quicbaas#156).

## Best Practices

### Production Configuration

```yaml
server:
  rest:
    enabled: true
    port: 8080
    timeout: 30s
    rate_limit: 1000         # requests per second per IP; -1 disables
    cors:
      enabled: true
      origins:
        - "https://app.example.com"
    security_headers: true    # always enabled
```

### Testing

```bash
# Verify security headers
curl -I https://api.example.com/health

# Expected headers:
# Strict-Transport-Security: max-age=31536000; includeSubDomains
# X-Frame-Options: DENY
# X-Content-Type-Options: nosniff
# X-XSS-Protection: 1; mode=block
# Referrer-Policy: strict-origin-when-cross-origin
# Content-Security-Policy: default-src 'self'
# Permissions-Policy: geolocation=(), microphone=(), camera=()
```

### Security Scanning

Run security scanners to verify headers:

```bash
# Mozilla Observatory
curl -X POST "https://http-observatory.security.mozilla.org/api/v1/analyze?host=api.example.com"

# Security Headers
curl "https://securityheaders.com/?q=https://api.example.com"
```

## See Also

- Security README - Security overview
- TLS Setup - HTTPS configuration
- Authentication - JWT authentication
- RBAC - Authorization
