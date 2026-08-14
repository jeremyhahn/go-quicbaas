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
	"net/http"
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
			clientIP := getClientIP(r)

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

// rateLimiter implements a token bucket rate limiter per client IP.
type rateLimiter struct {
	rate    int                     // requests per second
	buckets map[string]*tokenBucket // client IP -> bucket
	mu      sync.RWMutex
	cleanup *time.Ticker
}

// tokenBucket implements the token bucket algorithm.
type tokenBucket struct {
	tokens    float64
	capacity  float64
	rate      float64
	lastCheck time.Time
	mu        sync.Mutex
}

// newRateLimiter creates a new rate limiter.
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

// Allow checks if a request from the given client IP is allowed.
func (rl *rateLimiter) Allow(clientIP string) bool {
	rl.mu.RLock()
	bucket, exists := rl.buckets[clientIP]
	rl.mu.RUnlock()

	if !exists {
		rl.mu.Lock()
		// Double-check after acquiring write lock
		bucket, exists = rl.buckets[clientIP]
		if !exists {
			bucket = &tokenBucket{
				tokens:    float64(rl.rate),
				capacity:  float64(rl.rate),
				rate:      float64(rl.rate),
				lastCheck: time.Now(),
			}
			rl.buckets[clientIP] = bucket
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

	// Fall back to RemoteAddr
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx > 0 {
		return r.RemoteAddr[:idx]
	}

	return r.RemoteAddr
}
