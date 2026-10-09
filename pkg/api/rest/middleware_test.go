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
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

func TestSecurityHeadersMiddleware(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Plain-HTTP request (r.TLS == nil): per RFC 6797 §7.2 the HSTS header
	// MUST NOT be emitted over non-secure transport.
	assert.Empty(t, w.Header().Get(HeaderStrictTransportSecurity))

	// All other security headers are set unconditionally.
	assert.Equal(t, "DENY", w.Header().Get(HeaderXFrameOptions))
	assert.Equal(t, "nosniff", w.Header().Get(HeaderXContentTypeOptions))
	assert.Equal(t, "1; mode=block", w.Header().Get(HeaderXXSSProtection))
	assert.Equal(t, "strict-origin-when-cross-origin", w.Header().Get(HeaderReferrerPolicy))
	assert.Equal(t, "default-src 'self'", w.Header().Get(HeaderContentSecurityPolicy))
	assert.Equal(t, "geolocation=(), microphone=(), camera=()", w.Header().Get(HeaderPermissionsPolicy))
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestSecurityHeadersMiddlewareHSTSOmittedOverPlainHTTP verifies that a
// request arriving over plain HTTP (r.TLS == nil) does not receive the
// Strict-Transport-Security header, per RFC 6797 §7.2.
func TestSecurityHeadersMiddlewareHSTSOmittedOverPlainHTTP(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.TLS = nil // explicit: non-secure transport
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	assert.Empty(t, w.Header().Get(HeaderStrictTransportSecurity),
		"HSTS header must not be emitted over plain HTTP")
	// Other security headers remain present.
	assert.Equal(t, "DENY", w.Header().Get(HeaderXFrameOptions))
}

// TestSecurityHeadersMiddlewareHSTSPresentOverTLS verifies that a request
// arriving over TLS (r.TLS != nil) receives the Strict-Transport-Security
// header with its canonical value.
func TestSecurityHeadersMiddlewareHSTSPresentOverTLS(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Simulate a request received over a completed TLS handshake.
	req.TLS = &tls.ConnectionState{HandshakeComplete: true}
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	assert.Equal(t, "max-age=31536000; includeSubDomains",
		w.Header().Get(HeaderStrictTransportSecurity),
		"HSTS header must be emitted over TLS with its canonical value")
	// Other security headers remain present.
	assert.Equal(t, "DENY", w.Header().Get(HeaderXFrameOptions))
}

func TestSecurityHeadersMiddlewareWithDifferentMethods(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	tests := []struct {
		name   string
		method string
	}{
		{
			name:   "GET request",
			method: http.MethodGet,
		},
		{
			name:   "POST request",
			method: http.MethodPost,
		},
		{
			name:   "PUT request",
			method: http.MethodPut,
		},
		{
			name:   "DELETE request",
			method: http.MethodDelete,
		},
		{
			name:   "OPTIONS request",
			method: http.MethodOptions,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrappedHandler := middleware(handler)

			req := httptest.NewRequest(tt.method, "/test", nil)
			w := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(w, req)

			// All methods should have security headers. HSTS is omitted on
			// plain HTTP (r.TLS == nil) per RFC 6797 §7.2; the remaining
			// headers are set unconditionally.
			assert.Empty(t, w.Header().Get(HeaderStrictTransportSecurity))
			assert.NotEmpty(t, w.Header().Get(HeaderXFrameOptions))
			assert.NotEmpty(t, w.Header().Get(HeaderXContentTypeOptions))
			assert.NotEmpty(t, w.Header().Get(HeaderXXSSProtection))
			assert.NotEmpty(t, w.Header().Get(HeaderReferrerPolicy))
			assert.NotEmpty(t, w.Header().Get(HeaderContentSecurityPolicy))
			assert.NotEmpty(t, w.Header().Get(HeaderPermissionsPolicy))
		})
	}
}

func TestSecurityHeadersMiddlewarePreservesOtherHeaders(t *testing.T) {
	middleware := SecurityHeadersMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Custom-Header", "custom-value")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Security headers should be set
	assert.Equal(t, "DENY", w.Header().Get(HeaderXFrameOptions))

	// Custom headers from handler should also be preserved
	assert.Equal(t, "custom-value", w.Header().Get("X-Custom-Header"))
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))
}

func TestSecurityHeadersMiddlewareChaining(t *testing.T) {
	// Test that security headers middleware works correctly when chained
	securityMiddleware := SecurityHeadersMiddleware()
	requestIDMiddleware := RequestIDMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Chain middlewares: RequestID -> Security -> Handler
	wrappedHandler := requestIDMiddleware(securityMiddleware(handler))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Both middleware should have added their headers
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
	assert.Equal(t, "DENY", w.Header().Get(HeaderXFrameOptions))
	assert.Equal(t, "nosniff", w.Header().Get(HeaderXContentTypeOptions))
}

func TestRecoveryMiddleware(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedStatus int
		shouldPanic    bool
	}{
		{
			name: "normal execution",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			expectedStatus: http.StatusOK,
			shouldPanic:    false,
		},
		{
			name: "panic with string",
			handler: func(w http.ResponseWriter, r *http.Request) {
				panic("something went wrong")
			},
			expectedStatus: http.StatusInternalServerError,
			shouldPanic:    true,
		},
		{
			name: "panic with error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				panic(&errors.MessageError{Message: "error occurred"})
			},
			expectedStatus: http.StatusInternalServerError,
			shouldPanic:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := RecoveryMiddleware(log)
			handler := middleware(tt.handler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
			w := httptest.NewRecorder()

			handler.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestTimeoutMiddleware(t *testing.T) {
	tests := []struct {
		name           string
		timeout        time.Duration
		expectedStatus int
		shouldTimeout  bool
	}{
		{
			name:           "completes before timeout",
			timeout:        100 * time.Millisecond,
			expectedStatus: http.StatusOK,
			shouldTimeout:  false,
		},
		{
			// Per RFC 7231 §6.5.7, server-side handler timeouts are 503
			// (the canonical http.TimeoutHandler behavior). 408 is reserved
			// for client-side request stalls.
			name:           "times out",
			timeout:        50 * time.Millisecond,
			expectedStatus: http.StatusServiceUnavailable,
			shouldTimeout:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := TimeoutMiddleware(tt.timeout)

			handlerStarted := make(chan struct{})
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(handlerStarted)
				if tt.shouldTimeout {
					<-r.Context().Done()
					return
				}
				w.WriteHeader(http.StatusOK)
			})

			wrappedHandler := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
			w := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(w, req)
			<-handlerStarted

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestCORSMiddlewareVariations(t *testing.T) {
	tests := []struct {
		name           string
		allowedOrigins []string
		requestOrigin  string
		expectCORS     bool
		expectedOrigin string
	}{
		{
			name:           "allowed origin",
			allowedOrigins: []string{"https://example.com"},
			requestOrigin:  "https://example.com",
			expectCORS:     true,
			expectedOrigin: "https://example.com",
		},
		{
			name:           "disallowed origin",
			allowedOrigins: []string{"https://example.com"},
			requestOrigin:  "https://evil.com",
			expectCORS:     false,
			expectedOrigin: "",
		},
		{
			name:           "empty origins denies cross-origin",
			allowedOrigins: []string{},
			requestOrigin:  "https://anysite.com",
			expectCORS:     false,
			expectedOrigin: "",
		},
		{
			name:           "no origin header with empty config",
			allowedOrigins: []string{},
			requestOrigin:  "",
			expectCORS:     false,
			expectedOrigin: "",
		},
		{
			name:           "multiple allowed origins",
			allowedOrigins: []string{"https://example.com", "https://test.com"},
			requestOrigin:  "https://test.com",
			expectCORS:     true,
			expectedOrigin: "https://test.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			middleware := CORSMiddleware(tt.allowedOrigins)

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			wrappedHandler := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.requestOrigin != "" {
				req.Header.Set("Origin", tt.requestOrigin)
			}
			w := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(w, req)

			if tt.expectCORS {
				assert.Equal(t, tt.expectedOrigin, w.Header().Get("Access-Control-Allow-Origin"))
				assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Methods"))
				assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Headers"))
			} else {
				assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
			}
		})
	}
}

func TestRateLimitMiddleware(t *testing.T) {
	// Note: RateLimitMiddleware is currently a placeholder
	middleware := RateLimitMiddleware(100)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Should pass through for now
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGetRequestID(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		expected string
	}{
		{
			name:     "with request ID",
			ctx:      context.WithValue(context.Background(), requestIDKey, "test-request-123"),
			expected: "test-request-123",
		},
		{
			name:     "without request ID",
			ctx:      context.Background(),
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestID := GetRequestID(tt.ctx)
			assert.Equal(t, tt.expected, requestID)
		})
	}
}

func TestGetStartTime(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		checkTime bool
	}{
		{
			name:      "with start time",
			ctx:       context.WithValue(context.Background(), startTimeKey, time.Now()),
			checkTime: true,
		},
		{
			name:      "without start time",
			ctx:       context.Background(),
			checkTime: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startTime := GetStartTime(tt.ctx)
			assert.NotZero(t, startTime)

			if tt.checkTime {
				// Should be close to now
				assert.True(t, time.Since(startTime) < 1*time.Second)
			}
		})
	}
}

func TestLoggingMiddleware(t *testing.T) {
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})

	middleware := LoggingMiddleware(log)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("test response"))
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "test response", w.Body.String())
}

func TestMetricsMiddlewareMetrics(t *testing.T) {
	// Initialize metrics
	if httpRequestsTotal == nil {
		initMiddlewareMetrics()
	}

	middleware := MetricsMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	// Make several requests
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// Metrics should have been recorded
	// Note: We can't easily verify counter values without exposing them,
	// but we can verify the middleware executes successfully
}

func TestRoutePatternWithChiContext(t *testing.T) {
	t.Run("returns route pattern from chi context", func(t *testing.T) {
		// Set up a chi router with a parameterized route
		r := chi.NewRouter()

		var capturedPattern string
		r.Get("/api/v1/entities/{type}/{id}", func(w http.ResponseWriter, req *http.Request) {
			capturedPattern = routePattern(req)
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/entities/users/12345", nil)
		w := httptest.NewRecorder()

		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "/api/v1/entities/{type}/{id}", capturedPattern,
			"should return the route template, not the actual path with values")
	})

	t.Run("returns unmatched for requests without chi context", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/some/path", nil)
		pattern := routePattern(req)
		assert.Equal(t, "unmatched", pattern,
			"should return 'unmatched' when no chi RouteContext is present")
	})
}

func TestMetricsMiddlewareUsesRoutePattern(t *testing.T) {
	// Verify that the metrics middleware uses route patterns (bounded cardinality)
	// rather than raw URL paths (unbounded cardinality) when used with chi router.
	if httpRequestsTotal == nil {
		initMiddlewareMetrics()
	}

	r := chi.NewRouter()
	r.Use(MetricsMiddleware())

	r.Get("/api/v1/entities/{type}/{id}", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Send requests with different path parameters -- these should all map
	// to the same route pattern, not create separate metric series.
	paths := []string{
		"/api/v1/entities/users/1",
		"/api/v1/entities/users/2",
		"/api/v1/entities/orders/100",
	}

	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// The test validates that the middleware executes without error when chi
	// provides route context. The actual cardinality reduction is validated by
	// the integration test TestMetricsCardinality.
}

func TestRequestIDMiddlewareGeneration(t *testing.T) {
	middleware := RequestIDMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request ID is in context
		requestID := GetRequestID(r.Context())
		assert.NotEmpty(t, requestID)

		// Verify start time is in context
		startTime := GetStartTime(r.Context())
		assert.NotZero(t, startTime)

		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	tests := []struct {
		name              string
		existingRequestID string
	}{
		{
			name:              "generate new request ID",
			existingRequestID: "",
		},
		{
			name:              "use existing request ID",
			existingRequestID: "existing-request-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			if tt.existingRequestID != "" {
				req.Header.Set("X-Request-ID", tt.existingRequestID)
			}
			w := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(w, req)

			// Verify request ID is in response header
			responseID := w.Header().Get("X-Request-ID")
			assert.NotEmpty(t, responseID)

			if tt.existingRequestID != "" {
				assert.Equal(t, tt.existingRequestID, responseID)
			}
		})
	}
}

func TestResponseWriterInterface(t *testing.T) {
	baseWriter := httptest.NewRecorder()
	rw := &responseWriter{
		ResponseWriter: baseWriter,
		statusCode:     http.StatusOK,
	}

	// Test multiple writes
	data1 := []byte("Hello ")
	data2 := []byte("World!")

	n1, err1 := rw.Write(data1)
	assert.NoError(t, err1)
	assert.Equal(t, len(data1), n1)

	n2, err2 := rw.Write(data2)
	assert.NoError(t, err2)
	assert.Equal(t, len(data2), n2)

	assert.Equal(t, len(data1)+len(data2), rw.bytesWritten)

	// Test WriteHeader
	rw.WriteHeader(http.StatusCreated)
	assert.Equal(t, http.StatusCreated, rw.statusCode)
}

func TestMiddlewareErrorTypes(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name: "internal server error",
			err: &InternalServerError{
				Message: "database error",
			},
			expected: "database error",
		},
		{
			name: "request timeout error",
			err: &RequestTimeoutError{
				Timeout: 30 * time.Second,
			},
			expected: "request timeout after 30s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.err.Error())
		})
	}
}

func BenchmarkSecurityHeadersMiddleware(b *testing.B) {
	middleware := SecurityHeadersMiddleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := middleware(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)
	}
}

func BenchmarkRequestIDMiddleware(b *testing.B) {
	middleware := RequestIDMiddleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := middleware(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)
	}
}

func BenchmarkLoggingMiddleware(b *testing.B) {
	log, _ := logger.New(logger.LogConfig{
		Level:  "error",
		Format: "json",
		Output: "stdout",
	})

	middleware := LoggingMiddleware(log)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := middleware(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req = req.WithContext(context.WithValue(req.Context(), requestIDKey, "test-123"))
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)
	}
}

func BenchmarkMetricsMiddleware(b *testing.B) {
	if httpRequestsTotal == nil {
		initMiddlewareMetrics()
	}

	middleware := MetricsMiddleware()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrappedHandler := middleware(handler)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)
	}
}

func TestRateLimitMiddlewareDisabledZero(t *testing.T) {
	// Test with rate limit disabled (0)
	middleware := RateLimitMiddleware(0)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRateLimitMiddlewareNegativeValue(t *testing.T) {
	middleware := RateLimitMiddleware(-1)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCORSMiddlewareOptionsPreflightRequest(t *testing.T) {
	middleware := CORSMiddleware([]string{"https://example.com"})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// OPTIONS should return 204 No Content
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.NotEmpty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestGetClientIPWithVariousHeaders(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		expected   string
	}{
		{
			name: "X-Forwarded-For with multiple IPs",
			headers: map[string]string{
				"X-Forwarded-For": "192.168.1.1, 10.0.0.1, 172.16.0.1",
			},
			remoteAddr: "127.0.0.1:8080",
			expected:   "192.168.1.1",
		},
		{
			name: "X-Forwarded-For single IP",
			headers: map[string]string{
				"X-Forwarded-For": "192.168.1.100",
			},
			remoteAddr: "127.0.0.1:8080",
			expected:   "192.168.1.100",
		},
		{
			name: "X-Real-IP header",
			headers: map[string]string{
				"X-Real-IP": "10.10.10.10",
			},
			remoteAddr: "127.0.0.1:8080",
			expected:   "10.10.10.10",
		},
		{
			name:       "RemoteAddr with port",
			headers:    map[string]string{},
			remoteAddr: "192.168.2.1:54321",
			expected:   "192.168.2.1",
		},
		{
			name:       "RemoteAddr without port",
			headers:    map[string]string{},
			remoteAddr: "192.168.3.1",
			expected:   "192.168.3.1",
		},
		{
			name:       "IPv6 address",
			headers:    map[string]string{},
			remoteAddr: "[::1]:8080",
			// Bare, not "[::1]". net/http brackets an IPv6 peer; the brackets
			// are transport syntax, not part of the address, and the old
			// expectation kept them -- so this value reached the rate limiter
			// as a key no IP parser accepts, and audit records as a
			// non-address.
			expected: "::1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			req.RemoteAddr = tt.remoteAddr

			// See resolvedViaOwnPeer: this table is about header parsing, not
			// about which peers are believed.
			assert.Equal(t, tt.expected, resolvedViaOwnPeer(t, req))
		})
	}
}

func TestRateLimiterAllowAndDenyBehavior(t *testing.T) {
	// Create a rate limiter with 2 requests per second
	rl := newRateLimiter(2)

	// First two requests should be allowed
	assert.True(t, rl.Allow("client1"))
	assert.True(t, rl.Allow("client1"))

	// Third request should be denied (bucket empty)
	assert.False(t, rl.Allow("client1"))

	// Different client should be allowed
	assert.True(t, rl.Allow("client2"))
}

func TestTokenBucketConsumeLogic(t *testing.T) {
	tb := &tokenBucket{
		tokens:   2.0,
		capacity: 5.0,
		rate:     1.0,
	}

	// Should consume successfully
	assert.True(t, tb.consume())
	assert.True(t, tb.consume())

	// Should fail when no tokens
	tb.tokens = 0.5
	assert.False(t, tb.consume())
}

func TestRateLimitErrorMessageFormatting(t *testing.T) {
	err := &RateLimitError{
		Limit:  100,
		Period: "1s",
	}
	assert.Equal(t, "rate limit exceeded: 100 requests per 1s", err.Error())
}

// TestTimeoutMiddleware_HandlerEmitted503PassesThrough is the regression guard
// for the bug where TimeoutMiddleware globally rewrote every 503 (including
// handler-emitted ones such as "barrier sealed" or "no leader available") to
// 408. After the fix, handler-emitted status codes pass through unchanged.
func TestTimeoutMiddleware_HandlerEmitted503PassesThrough(t *testing.T) {
	middleware := TimeoutMiddleware(1 * time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"barrier sealed","code":"SEALED"}`))
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"handler-emitted 503 must reach the wire intact (not rewritten to 408)")
	assert.Equal(t, "5", w.Header().Get("Retry-After"))
	assert.Contains(t, w.Body.String(), `"barrier sealed"`)
	assert.Contains(t, w.Body.String(), `"SEALED"`)
}

// TestTimeoutMiddleware_HandlerEmitted408PassesThrough confirms that handlers
// which intentionally emit 408 (e.g. ctx.Done() branches mapping to a typed
// RequestTimeoutError) are also unmolested by the middleware.
func TestTimeoutMiddleware_HandlerEmitted408PassesThrough(t *testing.T) {
	middleware := TimeoutMiddleware(1 * time.Second)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestTimeout)
		_, _ = w.Write([]byte(`{"error":"client stalled"}`))
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestTimeout, w.Code)
	assert.Contains(t, w.Body.String(), "client stalled")
}

// TestTimeoutMiddleware_TimeoutFires503 confirms that when http.TimeoutHandler
// fires (handler exceeded its deadline), it emits 503 — the canonical RFC 7231
// behavior. 408 is reserved for client-side request stalls.
func TestTimeoutMiddleware_TimeoutFires503(t *testing.T) {
	middleware := TimeoutMiddleware(20 * time.Millisecond)

	done := make(chan struct{})
	defer close(done)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-done:
		}
	})

	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"server-side handler timeout must surface as 503 per RFC 7231 §6.5.7")
	assert.Contains(t, w.Body.String(), "request timeout after")
}

// TestSealedWindowMiddleware_Returns503NotRewritten is the regression guard
// for the sealed-window contract: a middleware in the chain that returns 503
// (e.g. SealedWindowMiddleware emitting `503 barrier sealed`) must reach the
// wire intact when stacked with TimeoutMiddleware. This pins the canonical
// envelope agreed in the sealed-window review.
func TestSealedWindowMiddleware_Returns503NotRewritten(t *testing.T) {
	// Simulate a sealed-window middleware that short-circuits with 503.
	sealedWindowMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"barrier sealed","code":"SEALED"}`))
		})
	}

	timeoutMW := TimeoutMiddleware(1 * time.Second)

	// Inner handler should never be reached.
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("inner handler must not be invoked while sealed")
	})

	// Compose: timeout wraps sealedWindow wraps inner.
	chain := timeoutMW(sealedWindowMW(inner))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kv/foo", nil)
	w := httptest.NewRecorder()
	chain.ServeHTTP(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code,
		"sealed-window 503 must reach the wire intact through TimeoutMiddleware")
	assert.Equal(t, "5", w.Header().Get("Retry-After"))
	assert.Contains(t, w.Body.String(), `"barrier sealed"`)
	assert.Contains(t, w.Body.String(), `"SEALED"`)
}

func TestInternalServerErrorMessageOutput(t *testing.T) {
	err := &InternalServerError{Message: "database connection failed"}
	assert.Equal(t, "database connection failed", err.Error())
}

func TestRequestTimeoutErrorMessageOutput(t *testing.T) {
	tests := []struct {
		name     string
		timeout  time.Duration
		expected string
	}{
		{
			name:     "5 seconds",
			timeout:  5 * time.Second,
			expected: "request timeout after 5s",
		},
		{
			name:     "100 milliseconds",
			timeout:  100 * time.Millisecond,
			expected: "request timeout after 100ms",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &RequestTimeoutError{Timeout: tt.timeout}
			assert.Equal(t, tt.expected, err.Error())
		})
	}
}

func TestCORSMiddlewareDisallowedOrigin(t *testing.T) {
	middleware := CORSMiddleware([]string{"https://allowed.com"})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Origin", "https://notallowed.com")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORSMiddlewareOptionsWithDisallowedOrigin(t *testing.T) {
	middleware := CORSMiddleware([]string{"https://allowed.com"})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodOptions, "/test", nil)
	req.Header.Set("Origin", "https://notallowed.com")
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// OPTIONS still returns 204 but no CORS headers
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestRateLimiterDoubleCheckLocking(t *testing.T) {
	rl := newRateLimiter(10)

	// First call creates the bucket
	rl.Allow("test-client")

	// Second call uses existing bucket
	rl.Allow("test-client")

	// Verify bucket exists
	rl.mu.RLock()
	_, exists := rl.buckets["test-client"]
	rl.mu.RUnlock()

	assert.True(t, exists)
}

func TestTokenBucketRefill(t *testing.T) {
	now := time.Now()
	tb := &tokenBucket{
		tokens:    0.0,
		capacity:  5.0,
		rate:      10.0, // 10 tokens per second
		lastCheck: now.Add(-1 * time.Second),
	}

	// After 1 second with rate of 10/sec, should have ~10 tokens (capped at capacity)
	result := tb.consume()
	assert.True(t, result, "Should have tokens after refill")
	assert.LessOrEqual(t, tb.tokens, tb.capacity)
}

func TestRateLimiterAllowNewClient(t *testing.T) {
	t.Run("allows first request from new client", func(t *testing.T) {
		rl := newRateLimiter(10)
		defer rl.cleanup.Stop()

		allowed := rl.Allow("192.168.1.1")
		assert.True(t, allowed)
	})

	t.Run("allows multiple requests up to rate limit", func(t *testing.T) {
		rl := newRateLimiter(5)
		defer rl.cleanup.Stop()

		// First 5 requests should be allowed
		for i := 0; i < 5; i++ {
			allowed := rl.Allow("192.168.1.2")
			assert.True(t, allowed, "request %d should be allowed", i+1)
		}

		// 6th request should be denied (no time to refill)
		allowed := rl.Allow("192.168.1.2")
		assert.False(t, allowed)
	})

	t.Run("different clients have independent rate limits", func(t *testing.T) {
		rl := newRateLimiter(2)
		defer rl.cleanup.Stop()

		// Client 1 uses their quota
		assert.True(t, rl.Allow("client1"))
		assert.True(t, rl.Allow("client1"))
		assert.False(t, rl.Allow("client1"))

		// Client 2 should still have their quota
		assert.True(t, rl.Allow("client2"))
		assert.True(t, rl.Allow("client2"))
	})
}

func TestTokenBucketConsumeViaRateLimiter(t *testing.T) {
	t.Run("consume returns true when tokens available", func(t *testing.T) {
		// Use the rate limiter to properly initialize the bucket
		rl := newRateLimiter(5)
		defer rl.cleanup.Stop()

		// First call creates bucket with tokens = capacity
		allowed := rl.Allow("test-client")
		assert.True(t, allowed)

		// Check remaining tokens via subsequent allows
		for i := 0; i < 4; i++ {
			assert.True(t, rl.Allow("test-client"))
		}

		// Should be denied now
		assert.False(t, rl.Allow("test-client"))
	})

	t.Run("consume returns false when no tokens", func(t *testing.T) {
		rl := newRateLimiter(1)
		defer rl.cleanup.Stop()

		// Use the only token
		assert.True(t, rl.Allow("test-client"))

		// Should be denied
		assert.False(t, rl.Allow("test-client"))
	})

	t.Run("tokens cap at capacity", func(t *testing.T) {
		rl := newRateLimiter(3)
		defer rl.cleanup.Stop()

		// Use all tokens
		assert.True(t, rl.Allow("test-client"))
		assert.True(t, rl.Allow("test-client"))
		assert.True(t, rl.Allow("test-client"))
		assert.False(t, rl.Allow("test-client"))
	})
}

// TestGetClientIP covers header parsing FROM A TRUSTED PROXY. The subtests
// below used to call getClientIP on a request from an arbitrary peer and assert
// the header won; that is the #156 defect, and it is now covered from the other
// side in trusted_proxy_test.go. The parsing itself -- first of the chain,
// X-Real-IP fallback, precedence between the two -- is still real logic, so it
// is exercised the way a deployment reaches it: through a configured proxy.
func TestGetClientIP(t *testing.T) {
	t.Run("extracts from X-Forwarded-For", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = trustedPeer
		req.Header.Set("X-Forwarded-For", "203.0.113.195")

		assert.Equal(t, "203.0.113.195", resolvedVia(t, []string{trustedProxyCIDR}, req))
	})

	t.Run("extracts first IP from X-Forwarded-For chain", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = trustedPeer
		req.Header.Set("X-Forwarded-For", "203.0.113.195, 70.41.3.18, 150.172.238.178")

		assert.Equal(t, "203.0.113.195", resolvedVia(t, []string{trustedProxyCIDR}, req))
	})

	t.Run("extracts from X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = trustedPeer
		req.Header.Set("X-Real-IP", "192.168.1.100")

		assert.Equal(t, "192.168.1.100", resolvedVia(t, []string{trustedProxyCIDR}, req))
	})

	t.Run("prefers X-Forwarded-For over X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = trustedPeer
		req.Header.Set("X-Forwarded-For", "10.0.0.1")
		req.Header.Set("X-Real-IP", "10.0.0.2")

		assert.Equal(t, "10.0.0.1", resolvedVia(t, []string{trustedProxyCIDR}, req))
	})

	t.Run("falls back to RemoteAddr", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "127.0.0.1:12345"

		ip := getClientIP(req)
		assert.Equal(t, "127.0.0.1", ip)
	})

	t.Run("handles RemoteAddr without port", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = "127.0.0.1"

		ip := getClientIP(req)
		assert.Equal(t, "127.0.0.1", ip)
	})

	t.Run("trims whitespace from headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = trustedPeer
		req.Header.Set("X-Forwarded-For", "  10.0.0.1  ")

		assert.Equal(t, "10.0.0.1", resolvedVia(t, []string{trustedProxyCIDR}, req))
	})

	t.Run("trims whitespace from X-Real-IP", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.RemoteAddr = trustedPeer
		req.Header.Set("X-Real-IP", "  192.168.1.1  ")

		assert.Equal(t, "192.168.1.1", resolvedVia(t, []string{trustedProxyCIDR}, req))
	})
}

func TestRateLimitMiddlewareDisabled(t *testing.T) {
	t.Run("disabled with zero rate", func(t *testing.T) {
		middleware := RateLimitMiddleware(0)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := middleware(handler)

		for i := 0; i < 100; i++ {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			wrappedHandler.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		}
	})

	t.Run("disabled with negative rate", func(t *testing.T) {
		middleware := RateLimitMiddleware(-1)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestRateLimitErrorType(t *testing.T) {
	err := &RateLimitError{
		Limit:  100,
		Period: "1s",
	}

	assert.Equal(t, "rate limit exceeded: 100 requests per 1s", err.Error())
}

func TestTokenBucketDirectConsume(t *testing.T) {
	t.Run("consume with proper initialization", func(t *testing.T) {
		now := time.Now()
		bucket := &tokenBucket{
			tokens:    5.0,
			capacity:  10.0,
			rate:      1.0,
			lastCheck: now,
		}

		result := bucket.consume()
		assert.True(t, result)
		// Tokens should be reduced (might have small refill from elapsed time)
		assert.Less(t, bucket.tokens, 5.0)
	})

	t.Run("consume when bucket is nearly empty", func(t *testing.T) {
		now := time.Now()
		bucket := &tokenBucket{
			tokens:    0.5,
			capacity:  10.0,
			rate:      0.0, // No refill
			lastCheck: now,
		}

		result := bucket.consume()
		assert.False(t, result)
	})

	t.Run("tokens refill based on elapsed time", func(t *testing.T) {
		// Set lastCheck to 2 seconds ago
		bucket := &tokenBucket{
			tokens:    0.0,
			capacity:  10.0,
			rate:      5.0, // 5 tokens per second
			lastCheck: time.Now().Add(-2 * time.Second),
		}

		// Should have refilled ~10 tokens (2s * 5/s)
		result := bucket.consume()
		assert.True(t, result)
	})

	t.Run("tokens cap at capacity during refill", func(t *testing.T) {
		// Set lastCheck to long time ago with high rate
		bucket := &tokenBucket{
			tokens:    0.0,
			capacity:  10.0,
			rate:      1000.0, // Very high rate
			lastCheck: time.Now().Add(-1 * time.Second),
		}

		bucket.consume()

		// Should be capped at capacity - 1 (after consuming)
		assert.LessOrEqual(t, bucket.tokens, bucket.capacity)
	})
}

func TestCORSMiddlewarePreflightRequest(t *testing.T) {
	t.Run("preflight returns 204 with allowed origin", func(t *testing.T) {
		middleware := CORSMiddleware([]string{"https://example.com"})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "https://example.com")
		w := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Equal(t, "https://example.com", w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("preflight with disallowed origin returns no CORS headers", func(t *testing.T) {
		middleware := CORSMiddleware([]string{"https://example.com"})

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		req.Header.Set("Origin", "https://evil.com")
		w := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})

	t.Run("preflight with nil origins denies cross-origin", func(t *testing.T) {
		middleware := CORSMiddleware(nil)

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		wrappedHandler := middleware(handler)

		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		w := httptest.NewRecorder()

		wrappedHandler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
	})
}

func TestRateLimitError_Error(t *testing.T) {
	err := &RateLimitError{
		Limit:  100,
		Period: "1s",
	}

	errMsg := err.Error()
	assert.Contains(t, errMsg, "rate limit exceeded")
	assert.Contains(t, errMsg, "100")
	assert.Contains(t, errMsg, "1s")
}

// Header parsing, reached from a configured proxy — see TestGetClientIP.
func TestGetClientIP_XForwardedFor(t *testing.T) {
	// Test X-Forwarded-For with single IP
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.RemoteAddr = trustedPeer
	r.Header.Set("X-Forwarded-For", "192.168.1.1")
	assert.Equal(t, "192.168.1.1", resolvedVia(t, []string{trustedProxyCIDR}, r))

	// Test X-Forwarded-For with multiple IPs
	r = httptest.NewRequest(http.MethodGet, "/test", nil)
	r.RemoteAddr = trustedPeer
	r.Header.Set("X-Forwarded-For", "192.168.1.1, 10.0.0.1, 172.16.0.1")
	assert.Equal(t, "192.168.1.1", resolvedVia(t, []string{trustedProxyCIDR}, r))
}

func TestGetClientIP_XRealIP(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.RemoteAddr = trustedPeer
	r.Header.Set("X-Real-IP", "192.168.1.2")
	assert.Equal(t, "192.168.1.2", resolvedVia(t, []string{trustedProxyCIDR}, r))
}

func TestGetClientIP_RemoteAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.RemoteAddr = "192.168.1.3:54321"
	ip := getClientIP(r)
	assert.Equal(t, "192.168.1.3", ip)
}

func TestGetClientIP_RemoteAddrNoPort(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	r.RemoteAddr = "192.168.1.4"
	ip := getClientIP(r)
	assert.Equal(t, "192.168.1.4", ip)
}
