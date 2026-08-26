//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

package rest

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// contextKey is the type for context keys to avoid collisions.
type contextKey string

const (
	// requestIDKey is the context key for request ID.
	requestIDKey contextKey = "request_id"

	// startTimeKey is the context key for request start time.
	startTimeKey contextKey = "start_time"
)

// DefaultMaxBodySize is the default maximum request body size (10 MB).
// This protects against memory exhaustion from oversized request payloads.
const DefaultMaxBodySize int64 = 10 << 20 // 10 MB

// DefaultMaxUploadBodySize is the default maximum request body size for
// upload-heavy endpoints such as object storage and backup (100 MB).
const DefaultMaxUploadBodySize int64 = 100 << 20 // 100 MB

// MaxOAuth2FormBytes bounds the request body of the form-encoded OAuth2
// endpoints. The largest legitimate field is a JWT client assertion or a
// refresh token, so 1 MB sits far above any real request while keeping
// ParseForm from buffering an attacker-supplied body. Those endpoints are
// public and are mounted both on the legacy router (iam.go) and through the
// module route specs (routes_iam.go), only one of which installs
// BodyLimitMiddleware, so each handler applies this limit itself.
const MaxOAuth2FormBytes int64 = 1 << 20 // 1 MB

// Security header constants.
const (
	// HeaderStrictTransportSecurity is the HSTS header name.
	HeaderStrictTransportSecurity = "Strict-Transport-Security"

	// HeaderXFrameOptions is the X-Frame-Options header name.
	HeaderXFrameOptions = "X-Frame-Options"

	// HeaderXContentTypeOptions is the X-Content-Type-Options header name.
	HeaderXContentTypeOptions = "X-Content-Type-Options"

	// HeaderXXSSProtection is the X-XSS-Protection header name.
	HeaderXXSSProtection = "X-XSS-Protection"

	// HeaderReferrerPolicy is the Referrer-Policy header name.
	HeaderReferrerPolicy = "Referrer-Policy"

	// HeaderContentSecurityPolicy is the Content-Security-Policy header name.
	HeaderContentSecurityPolicy = "Content-Security-Policy"

	// HeaderPermissionsPolicy is the Permissions-Policy header name.
	HeaderPermissionsPolicy = "Permissions-Policy"
)

// Middleware metrics.
var (
	httpRequestsTotal    *metrics.CounterVec
	httpRequestDuration  *metrics.HistogramVec
	httpRequestsInFlight *metrics.Gauge
)

// initMiddlewareMetrics initializes middleware metrics once.
func initMiddlewareMetrics() {
	httpRequestsTotal = metrics.NewCounterVec(metrics.CounterOpts{
		Namespace: "quicbaas",
		Subsystem: "http",
		Name:      "requests_total",
		Help:      "Total number of HTTP requests",
	}, []string{"method", "path", "status"})

	httpRequestDuration = metrics.NewHistogramVec(metrics.HistogramOpts{
		Namespace: "quicbaas",
		Subsystem: "http",
		Name:      "request_duration_seconds",
		Help:      "HTTP request duration in seconds",
		Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
	}, []string{"method", "path"})

	httpRequestsInFlight = metrics.NewGauge(metrics.GaugeOpts{
		Namespace: "quicbaas",
		Subsystem: "http",
		Name:      "requests_in_flight",
		Help:      "Number of HTTP requests currently being processed",
	})
}

// SecurityHeadersMiddleware adds security headers to all HTTP responses.
// This middleware sets standard security headers to protect against common
// web vulnerabilities including clickjacking, XSS, MIME sniffing, and more.
func SecurityHeadersMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// HSTS - Force HTTPS for 1 year, including subdomains.
			// Per RFC 6797 §7.2, an HSTS host MUST NOT include the STS
			// header field in responses conveyed over non-secure transport.
			// r.TLS is non-nil only when the request arrived over TLS, so
			// the header is emitted exclusively for secure connections.
			if r.TLS != nil {
				w.Header().Set(HeaderStrictTransportSecurity, "max-age=31536000; includeSubDomains")
			}

			// Prevent clickjacking by denying frame embedding
			w.Header().Set(HeaderXFrameOptions, "DENY")

			// Prevent MIME type sniffing
			w.Header().Set(HeaderXContentTypeOptions, "nosniff")

			// Enable XSS filter in browsers (legacy, but still useful)
			w.Header().Set(HeaderXXSSProtection, "1; mode=block")

			// Control referrer information sent with requests
			w.Header().Set(HeaderReferrerPolicy, "strict-origin-when-cross-origin")

			// Restrict content sources to same origin
			w.Header().Set(HeaderContentSecurityPolicy, "default-src 'self'")

			// Disable sensitive browser features
			w.Header().Set(HeaderPermissionsPolicy, "geolocation=(), microphone=(), camera=()")

			next.ServeHTTP(w, r)
		})
	}
}

// LoggingMiddleware logs HTTP requests and responses.
func LoggingMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Get request ID from context
			requestID := GetRequestID(r.Context())

			// Log request with strongly-typed slog attributes
			log.InfoAttrs("HTTP request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote", r.RemoteAddr),
				slog.String("request_id", requestID),
			)

			// Wrap response writer to capture status code
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Process request
			start := time.Now()
			next.ServeHTTP(wrapped, r)
			duration := time.Since(start)

			// Log response with strongly-typed slog attributes
			shard := wrapped.Header().Get("X-QuicBaaS-Shard")
			log.InfoAttrs("HTTP response",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("remote", r.RemoteAddr),
				slog.String("request_id", requestID),
				slog.String("shard_id", shard),
				slog.Int("status", wrapped.statusCode),
				slog.Duration("duration", duration),
				slog.Int("bytes", wrapped.bytesWritten),
			)
		})
	}
}

// routePattern extracts the chi route pattern from the request context.
// After next.ServeHTTP returns, chi has matched the route and populated
// the RouteContext with the matched pattern (e.g., "/api/v1/entities/{type}/{id}").
// This prevents unbounded metric cardinality from unique URL paths.
func routePattern(r *http.Request) string {
	rctx := chi.RouteContext(r.Context())
	if rctx != nil {
		if pattern := rctx.RoutePattern(); pattern != "" {
			return pattern
		}
	}
	// Fallback for unmatched routes (404s, etc.)
	return "unmatched"
}

// MetricsMiddleware collects Prometheus metrics for HTTP requests.
// It uses chi's RouteContext to record the matched route pattern instead of
// the raw URL path, preventing unbounded cardinality from dynamic path segments.
func MetricsMiddleware() func(http.Handler) http.Handler {
	// Initialize metrics on first use
	if httpRequestsTotal == nil {
		initMiddlewareMetrics()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Track in-flight requests
			httpRequestsInFlight.Inc()
			defer httpRequestsInFlight.Dec()

			// Wrap response writer to capture status
			wrapped := &responseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Process request
			start := time.Now()
			next.ServeHTTP(wrapped, r)
			duration := time.Since(start).Seconds()

			// Use route pattern instead of raw path to bound cardinality.
			// After next.ServeHTTP returns, chi has resolved the route.
			pattern := routePattern(r)

			// Record metrics with bounded cardinality labels
			httpRequestsTotal.With(
				r.Method,
				pattern,
				strconv.Itoa(wrapped.statusCode),
			).Inc()

			httpRequestDuration.With(
				r.Method,
				pattern,
			).Observe(duration)
		})
	}
}

// CORSMiddleware adds CORS headers to responses.
// When allowedOrigins is empty or nil, all cross-origin requests are denied
// (no CORS headers are set). This is a secure default that requires explicit
// configuration to enable cross-origin access.
func CORSMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	if len(allowedOrigins) == 0 {
		slog.Warn("CORS middleware configured with no allowed origins; all cross-origin requests will be denied")
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is in the allowed list
			allowed := false
			if origin != "" {
				for _, allowedOrigin := range allowedOrigins {
					if origin == allowedOrigin {
						allowed = true
						break
					}
				}
			}

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
				w.Header().Set("Access-Control-Max-Age", "3600")
			}

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RecoveryMiddleware recovers from panics and returns 500 error.
func RecoveryMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					// Log panic with stack trace
					log.WithFields(map[string]interface{}{
						"error":      err,
						"stack":      string(debug.Stack()),
						"request_id": GetRequestID(r.Context()),
					}).Error("Panic recovered in HTTP handler")

					// Return 500 error
					RespondError(w, r, http.StatusInternalServerError, &InternalServerError{
						Message: "Internal server error",
					})
				}
			}()

			next.ServeHTTP(w, r)
		})
	}
}

// TimeoutMiddleware adds a timeout to request processing.
//
// It wraps http.TimeoutHandler, which buffers the inner handler's response and
// writes 503 Service Unavailable with a JSON body if the deadline fires before
// the handler completes. Per RFC 7231 §6.5.7, status 408 Request Timeout is
// reserved for "the server did not receive a complete request message within
// the time that it was prepared to wait" — i.e. the client stalled mid-request.
// A server-side handler exceeding its own processing deadline is correctly
// modeled as 503 (the canonical http.TimeoutHandler behavior), not 408.
//
// This middleware does NOT rewrite the status code emitted by the inner
// handler. Handler-emitted 503 responses (e.g. "barrier sealed", "no leader
// available") pass through unchanged so callers can distinguish them and the
// canonical envelopes (Retry-After, typed error codes) reach the wire intact.
func TimeoutMiddleware(timeout time.Duration) func(http.Handler) http.Handler {
	timeoutMessage := `{"error":"request timeout after ` + timeout.String() + `","retryable":true}`
	return func(next http.Handler) http.Handler {
		timed := http.TimeoutHandler(next, timeout, timeoutMessage)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Streaming responses are long-lived by construction: an SSE
			// follow lasts as long as the agent turn it is reporting on,
			// which is unbounded. http.TimeoutHandler also buffers the
			// response, which would defeat incremental delivery even below
			// the deadline. Such requests bypass the deadline entirely;
			// their lifetime is governed by the client disconnecting or the
			// handler completing.
			if isStreamingRequest(r) {
				next.ServeHTTP(w, r)
				return
			}
			timed.ServeHTTP(w, r)
		})
	}
}

// isStreamingRequest reports whether the request asks for an event stream.
//
// Detection is by request shape, not by a route table that would have to be
// kept in sync as streaming endpoints are added:
//
//   - Accept: text/event-stream is the canonical SSE signal, sent by browser
//     EventSource and by the SDK's SSE reader.
//   - follow=1 marks a tail on the session-events endpoint, which streams SSE
//     regardless of the Accept header. Without this a caller that asks for
//     `Accept: application/json&follow=1` would have its unbounded stream
//     buffered and cut by the request deadline.
func isStreamingRequest(r *http.Request) bool {
	if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
		return true
	}
	switch r.URL.Query().Get("follow") {
	case "1", "true":
		return true
	}
	return false
}

// RequestIDMiddleware generates and adds a request ID to the context.
func RequestIDMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if request ID already exists in header
			requestID := r.Header.Get("X-Request-ID")
			if requestID == "" {
				// Generate new UUID
				requestID = uuid.New().String()
			}

			// Add request ID to response header
			w.Header().Set("X-Request-ID", requestID)

			// Add request ID to context
			ctx := context.WithValue(r.Context(), requestIDKey, requestID)
			ctx = context.WithValue(ctx, startTimeKey, time.Now())

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// BodyLimitMiddleware wraps r.Body with http.MaxBytesReader to enforce a
// maximum request body size. When the limit is exceeded, the reader returns
// an error and subsequent io.ReadAll / json.Decoder calls fail. The middleware
// detects this via http.MaxBytesError and responds with 413 Payload Too Large.
//
// The limit parameter is in bytes. Pass 0 or a negative value to disable the
// limit (passthrough).
func BodyLimitMiddleware(limit int64) func(http.Handler) http.Handler {
	if limit <= 0 {
		// Body limiting disabled, pass through
		return func(next http.Handler) http.Handler {
			return next
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip body limiting for requests without a body
			if r.Body == nil || r.Body == http.NoBody {
				next.ServeHTTP(w, r)
				return
			}

			// Wrap the body with MaxBytesReader to enforce the size limit.
			// MaxBytesReader returns an error on Read when the limit is exceeded,
			// which downstream handlers detect when calling io.ReadAll or json.Decode.
			r.Body = http.MaxBytesReader(w, r.Body, limit)

			next.ServeHTTP(w, r)
		})
	}
}

// IsMaxBytesError checks whether an error is caused by http.MaxBytesReader
// exceeding its configured limit. Use this in handlers to detect oversized
// request bodies and return an appropriate 413 response.
func IsMaxBytesError(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.As(err, &maxBytesErr)
}

// RateLimitMiddleware implements token bucket rate limiting per client IP.
func RateLimitMiddleware(requestsPerSecond int) func(http.Handler) http.Handler {
	if requestsPerSecond <= 0 {
		// Rate limiting disabled, pass through
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				next.ServeHTTP(w, r)
			})
		}
	}

	limiter := newRateLimiter(requestsPerSecond)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP from request
			// limiterKey collapses an IPv6 source to its /64 so one
			// allocation cannot mint an unbounded number of buckets, and so
			// the limit applies to IPv6 clients at all. It does NOT make this
			// key trustworthy: getClientIP still honours X-Forwarded-For with
			// no trusted-proxy allowlist, so this bound is memory-only until
			// #156 lands the trusted-proxy keying.
			clientIP := limiterKey(getClientIP(r))

			// Check if request is allowed
			if !limiter.Allow(clientIP) {
				RespondError(w, r, http.StatusTooManyRequests, &RateLimitError{
					Limit:  requestsPerSecond,
					Period: "1s",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code and bytes written.
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

// WriteHeader captures the status code.
func (rw *responseWriter) WriteHeader(statusCode int) {
	rw.statusCode = statusCode
	rw.ResponseWriter.WriteHeader(statusCode)
}

// Write captures bytes written.
func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytesWritten += n
	return n, err
}

// GetRequestID retrieves the request ID from context.
func GetRequestID(ctx context.Context) string {
	if requestID, ok := ctx.Value(requestIDKey).(string); ok {
		return requestID
	}
	return ""
}

// GetStartTime retrieves the request start time from context.
func GetStartTime(ctx context.Context) time.Time {
	if startTime, ok := ctx.Value(startTimeKey).(time.Time); ok {
		return startTime
	}
	return time.Now()
}

// Custom error types for middleware

// InternalServerError represents a 500 error.
type InternalServerError struct {
	Message string
}

func (e *InternalServerError) Error() string {
	return e.Message
}

// RequestTimeoutError represents a 408 error.
type RequestTimeoutError struct {
	Timeout time.Duration
}

func (e *RequestTimeoutError) Error() string {
	return "request timeout after " + e.Timeout.String()
}

// RateLimitError represents a 429 error.
type RateLimitError struct {
	Limit  int
	Period string
}

func (e *RateLimitError) Error() string {
	return "rate limit exceeded: " + strconv.Itoa(e.Limit) + " requests per " + e.Period
}

// PayloadTooLargeError represents a 413 Payload Too Large error.
// It is returned when a request body exceeds the configured maximum size.
type PayloadTooLargeError struct {
	MaxBytes int64
}

func (e *PayloadTooLargeError) Error() string {
	return "request body too large: maximum size is " + strconv.FormatInt(e.MaxBytes, 10) + " bytes"
}

// maxRateLimiterBuckets caps how many per-source buckets a BOUNDED
// rateLimiter will hold. Beyond it, un-tracked sources share one overflow
// bucket.
//
// The map is otherwise unbounded, and nothing throttles its growth: a NEW key
// is always admitted because it is created with a full bucket, so the limiter
// constrains reuse but never creation. Entries therefore accrue at the request
// rate, and cleanupLoop evicts nothing for 10 minutes — measured at ~140 bytes
// per entry, 10k req/s from distinct sources reaches ~840 MB before the first
// eviction is considered. A single IPv6 /64 supplies 2^64 addresses, so this
// needs no botnet (#167).
//
// 100,000 entries is ~12 MB, a reasonable ceiling for a server process and far
// above any plausible count of concurrently-active legitimate sources.
const maxRateLimiterBuckets = 100000

// rateLimiter implements a token bucket rate limiter per client IP.
//
// The bucket map is bounded only when maxBuckets is set, and that is
// deliberately opt-in rather than the default. **A cap must never be applied to
// a limiter whose key an attacker can forge**, because the two combine into a
// denial of service that neither has alone:
//
//	forgeable key + no cap  -> attacker grows the map (memory exhaustion)
//	forgeable key + a cap   -> attacker fills the cap with minted keys, then
//	                           drains the shared overflow, and every legitimate
//	                           source not already in the map is served from
//	                           that empty bucket. One peer throttles everyone.
//
// So bounding is safe only where the key is the transport peer. Today that is
// oauth2ClientAuthLimiter (remoteAddrKey). RateLimitMiddleware keys on
// getClientIP, which honours X-Forwarded-For with no trusted-proxy allowlist,
// so it stays UNBOUNDED until #156 makes its key trustworthy — the bound and
// the trustworthy keying have to land together, and #156 is where they do.
type rateLimiter struct {
	rate    int                     // requests per second
	buckets map[string]*tokenBucket // client IP -> bucket
	mu      sync.RWMutex
	cleanup *time.Ticker

	// maxBuckets bounds len(buckets); zero means unbounded.
	maxBuckets int

	// overflow serves every source that arrives once buckets is full, and is
	// nil on an unbounded limiter. See Allow for why the alternatives
	// (evicting, or refusing outright) are both worse.
	overflow *tokenBucket
}

// tokenBucket implements the token bucket algorithm.
type tokenBucket struct {
	tokens    float64
	capacity  float64
	rate      float64
	lastCheck time.Time
	mu        sync.Mutex
}

// newRateLimiter creates an UNBOUNDED rate limiter: one bucket per key, for as
// many keys as arrive. Correct for a limiter keyed on something a caller can
// forge — see the rateLimiter doc comment for why adding a cap there would be
// a denial of service rather than a fix.
func newRateLimiter(requestsPerSecond int) *rateLimiter {
	rl := &rateLimiter{
		rate:    requestsPerSecond,
		buckets: make(map[string]*tokenBucket),
		cleanup: time.NewTicker(5 * time.Minute),
	}

	// Start cleanup goroutine to remove old buckets
	go rl.cleanupLoop()

	return rl
}

// newBoundedRateLimiter creates a rate limiter that tracks at most maxBuckets
// sources, serving the rest from one shared overflow bucket.
//
// ONLY for a limiter keyed on the transport peer. Passing a forgeable key here
// hands an attacker a service-wide throttle: they mint keys to fill the cap,
// then drain the overflow that every subsequent legitimate source is served
// from.
func newBoundedRateLimiter(requestsPerSecond, maxBuckets int) *rateLimiter {
	rl := newRateLimiter(requestsPerSecond)
	rl.maxBuckets = maxBuckets
	rl.overflow = &tokenBucket{
		tokens:    float64(requestsPerSecond),
		capacity:  float64(requestsPerSecond),
		rate:      float64(requestsPerSecond),
		lastCheck: time.Now(),
	}
	return rl
}

// Allow checks if a request from the given client IP is allowed.
//
// On a BOUNDED limiter, once the map holds maxBuckets sources a source without
// a bucket is served from the shared overflow bucket rather than being given
// one of its own. The two obvious alternatives are both worse:
//
//   - Evicting to make room (LRU or otherwise) turns eviction into the bypass.
//     An attacker whose bucket is evicted gets a FRESH, FULL bucket on the next
//     request, so spending the map is how you reset your own limit — and an
//     attacker who can drive eviction can flush a victim's bucket deliberately.
//   - Refusing at the cap hands the attacker a global denial of service for the
//     price of filling a map: every legitimate newcomer is locked out.
//
// Sharing degrades newcomers during a flood without ever admitting one for
// free, which is the safe direction. cleanupLoop still drains idle entries, so
// capacity returns on its own once a flood stops.
func (rl *rateLimiter) Allow(clientIP string) bool {
	rl.mu.RLock()
	bucket, exists := rl.buckets[clientIP]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		// Double-check after acquiring write lock
		bucket, exists = rl.buckets[clientIP]
		if !exists {
			if rl.maxBuckets > 0 && len(rl.buckets) >= rl.maxBuckets {
				bucket = rl.overflow
			} else {
				bucket = &tokenBucket{
					tokens:    float64(rl.rate),
					capacity:  float64(rl.rate),
					rate:      float64(rl.rate),
					lastCheck: time.Now(),
				}
				rl.buckets[clientIP] = bucket
			}
		}
		rl.mu.Unlock()
	}

	return bucket.consume()
}

// consume attempts to consume one token from the bucket.
func (tb *tokenBucket) consume() bool {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(tb.lastCheck).Seconds()
	tb.lastCheck = now

	// Refill tokens based on elapsed time
	tb.tokens += elapsed * tb.rate
	if tb.tokens > tb.capacity {
		tb.tokens = tb.capacity
	}

	// Try to consume one token
	if tb.tokens >= 1.0 {
		tb.tokens -= 1.0
		return true
	}

	return false
}

// cleanupLoop periodically removes inactive buckets.
func (rl *rateLimiter) cleanupLoop() {
	for range rl.cleanup.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, bucket := range rl.buckets {
			bucket.mu.Lock()
			if now.Sub(bucket.lastCheck) > 10*time.Minute {
				delete(rl.buckets, ip)
			}
			bucket.mu.Unlock()
		}
		rl.mu.Unlock()
	}
}

// getClientIP extracts the client IP from the request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header first (for proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP in the list
		if idx := strings.Index(xff, ","); idx > 0 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr.
	//
	// SplitHostPort, not a split on the last colon. net/http writes an IPv6
	// peer as "[2001:db8::1]:1234", and the naive form returned
	// "[2001:db8::1]" — brackets included, which no IP parser accepts — while
	// a bare "2001:db8::1" was mangled into "2001:db8:" by splitting on its
	// own last colon. Both values then flowed into the rate limiter as keys
	// and into audit records as addresses, where a mangled address is worse
	// than an absent one.
	// The empty-host guard keeps degenerate inputs behaving as they did: ":"
	// splits successfully into empty host and empty port, and turning that
	// into "" rather than ":" would be a gratuitous change to a value that is
	// meaningless either way.
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && host != "" {
		return host
	}

	// No port to split, or nothing useful in it: return the address as it is.
	// A bracket-less multi-colon value such as "::1:8080" is a valid IPv6
	// address in its own right, so it is left whole rather than truncated at
	// its last colon — net/http brackets a real IPv6 peer, so an unbracketed
	// one is an address, not an address and a port.
	return r.RemoteAddr
}

// oauth2ClientAuthRateLimit is the per-source-IP request rate allowed across
// the OAuth2 client-authenticating endpoints, and the burst (the bucket's
// capacity equals its rate).
//
// Three endpoints qualify — POST /oauth2/token, /oauth2/revoke and
// /oauth2/introspect. All three are RouteAuthPublic, and on all three the
// caller's "client authentication" IS a call to ValidateClientSecret, so since
// #37 every attempt costs one PBKDF2 derivation whether or not the client
// exists — measured at ~308ms on a 32-core host, so roughly 100 req/s saturates
// every core. Without a bound, one unauthenticated sender is a CPU-exhaustion
// amplifier on any of them; the enumeration oracle that derivation closes would
// have been traded for a denial of service (#118 F1).
//
// 10/s caps a single source at ~3.1 core-seconds per second — about 3 of 32
// cores — so saturating the host now takes ~11 distinct sources rather than
// one. It is orders of magnitude above legitimate traffic: client_credentials
// fetches are periodic, governed by token TTL, so even a thousand clients
// behind one NAT gateway refreshing on a 15-minute TTL is ~1.1 req/s.
const oauth2ClientAuthRateLimit = 10

// oauth2ClientAuthLimiter is the single limiter shared by all three endpoints
// and by every registration path that serves them.
//
// ONE bucket per source across the trio, not one per route: the resource being
// protected is total KDF CPU per source, not per-route fairness. Three separate
// buckets would triple a single sender's effective budget for the same amount
// of derivation work, which is the thing the bound exists to cap.
//
// The registration paths are exactly two, and they do not overlap:
//
//   - the hand-written chi registrations in IAMHandler.RegisterRoutes (iam.go),
//     which is what the legacy in-process listener serves. Server.registerRoutes
//     binds only GroupAPI, GroupAPIUpload and the three qrdb-wire groups — it
//     never binds GroupOAuth2Core — so the spec-level wrapper does NOT reach
//     this path and it carries its own;
//   - the route table's GroupOAuth2Core specs (routes_iam.go), bound by
//     apimodule.RegisterHTTP through ModuleRoutes and Server.BindRoute, which
//     is what the unified API server serves.
//
// It is created on first use rather than at package init so that a binary
// importing this package without serving these endpoints does not carry the
// cleanup goroutine and its ticker.
var oauth2ClientAuthLimiter = sync.OnceValue(func() *rateLimiter {
	// Bounded, and safe to bound: these endpoints key on remoteAddrKey, the
	// transport peer, which a caller cannot forge. RateLimitMiddleware is NOT
	// bounded because its key comes from getClientIP and is forgeable — see
	// the rateLimiter doc comment.
	return newBoundedRateLimiter(oauth2ClientAuthRateLimit, maxRateLimiterBuckets)
})

// remoteAddrKey identifies a client by its transport peer address ONLY.
//
// It deliberately does NOT use getClientIP, which honours X-Forwarded-For and
// X-Real-IP with no trusted-proxy allowlist. Keying a limiter on a header the
// caller controls is not a limit at all: a fresh forged value yields a fresh
// bucket, initialised full, so every request is admitted — and the bucket map
// grows unboundedly while that happens. On an unauthenticated endpoint that is
// a bypass of exactly the control this exists to provide.
//
// The cost is that behind a reverse proxy every request shares the proxy's
// address and the limit becomes effectively global. That is the safe direction
// to be wrong in: over-limiting is visible and diagnosable, under-limiting is
// the vulnerability shipped. Restoring per-client granularity behind a proxy
// needs a trusted-proxy configuration, tracked separately.
func remoteAddrKey(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return limiterKey(host)
	}
	return limiterKey(r.RemoteAddr)
}

// limiterKey reduces a source address to the unit a rate limit should apply
// to: an IPv6 address collapses to its /64, everything else is returned as-is.
//
// Two reasons, and the second matters more than the memory one:
//
//   - A /64 is the smallest routine IPv6 allocation, so it is 2^64 addresses
//     that belong to ONE subscriber. Keying on the full address lets a single
//     allocation mint an unbounded number of buckets (#167).
//   - Without this the rate limit is effectively IPv4-only. An IPv6 client
//     evades it entirely by rotating within its own prefix — every request
//     from a fresh address, every fresh address a full bucket. Aggregation is
//     what makes the limit apply to IPv6 clients at all.
//
// The cost is that unrelated clients sharing a prefix share an allowance, so
// the limiter over-limits rather than under-limits. That is the same trade
// already recorded for remoteAddrKey above, and the same safe direction:
// over-limiting is visible and diagnosable, under-limiting is the
// vulnerability shipped.
//
// A value that does not parse as an IP is returned unchanged. getClientIP can
// hand over arbitrary header content (#156), and inventing structure for it
// here would only obscure that.
func limiterKey(addr string) string {
	// X-Forwarded-For legitimately carries bracketed IPv6 ("[2001:db8::1]"),
	// and getClientIP returns header content verbatim. Without this, adding
	// brackets would yield a different bucket from the bare address — the
	// bracketing itself would be the bypass this function exists to close.
	parsed := strings.TrimSuffix(strings.TrimPrefix(addr, "["), "]")

	ip, err := netip.ParseAddr(parsed)
	if err != nil || !ip.Is6() || ip.Is4In6() {
		return addr
	}
	prefix, err := ip.Prefix(64)
	if err != nil {
		return addr
	}
	return prefix.String()
}

// RateLimitOAuth2ClientAuth bounds an unauthenticated OAuth2 endpoint whose
// client authentication runs the PBKDF2 client-secret verification, per source
// IP and against a bucket shared with its sibling endpoints.
//
// Every path that serves /oauth2/token, /oauth2/revoke or /oauth2/introspect
// must wrap its handler with this; a path that does not is an unbounded route
// to the same KDF. A new OAuth2 endpoint that reaches ValidateClientSecret
// belongs behind this wrapper too — the deciding question is whether an
// unauthenticated caller can make it derive, not which verb it is.
//
// The two serving paths wrap in different places (see oauth2ClientAuthLimiter)
// and each request therefore passes through exactly one wrapper. Wrapping the
// same request twice would spend two tokens per attempt and halve the effective
// limit, so a new serving path adds a wrapper only where none already applies.
func RateLimitOAuth2ClientAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !oauth2ClientAuthLimiter().Allow(remoteAddrKey(r)) {
			RespondError(w, r, http.StatusTooManyRequests, &RateLimitError{
				Limit:  oauth2ClientAuthRateLimit,
				Period: "1s",
			})
			return
		}
		next(w, r)
	}
}
