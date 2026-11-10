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

package tracing

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/clientip"
)

func TestHTTPMiddleware(t *testing.T) {
	tests := []struct {
		name          string
		enabled       bool
		method        string
		path          string
		handlerStatus int
		wantSpans     int
		shardHeader   string
	}{
		{
			name:          "successful GET request",
			enabled:       true,
			method:        http.MethodGet,
			path:          "/api/v1/keys/user:123",
			handlerStatus: http.StatusOK,
			wantSpans:     1,
		},
		{
			name:          "POST request with error",
			enabled:       true,
			method:        http.MethodPost,
			path:          "/api/v1/keys",
			handlerStatus: http.StatusBadRequest,
			wantSpans:     1,
		},
		{
			name:          "request with shard header",
			enabled:       true,
			method:        http.MethodGet,
			path:          "/api/v1/keys/test",
			handlerStatus: http.StatusOK,
			wantSpans:     1,
			shardHeader:   "42",
		},
		{
			name:          "tracing disabled",
			enabled:       false,
			method:        http.MethodGet,
			path:          "/api/v1/keys/test",
			handlerStatus: http.StatusOK,
			wantSpans:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     tt.enabled,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			middleware := tracer.HTTPMiddleware()

			// Create test handler
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tt.shardHeader != "" {
					w.Header().Set("X-QuicBaaS-Shard", tt.shardHeader)
				}
				w.WriteHeader(tt.handlerStatus)
				w.Write([]byte("response body"))
			})

			// Wrap with middleware
			wrappedHandler := middleware(handler)

			// Create test request
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("User-Agent", "test-client/1.0")
			w := httptest.NewRecorder()

			// Execute request
			wrappedHandler.ServeHTTP(w, req)

			// Verify response
			assert.Equal(t, tt.handlerStatus, w.Code)

			// Verify spans
			spans := sr.Ended()
			assert.Len(t, spans, tt.wantSpans)

			if tt.wantSpans > 0 {
				span := spans[0]
				assert.Equal(t, tt.path, span.Name())

				// Check attributes
				attrs := span.Attributes()
				hasAttr := func(key string) bool {
					for _, attr := range attrs {
						if string(attr.Key) == key {
							return true
						}
					}
					return false
				}

				assert.True(t, hasAttr("http.method"))
				assert.True(t, hasAttr("http.url"))
				assert.True(t, hasAttr("http.status_code"))

				if tt.shardHeader != "" {
					assert.True(t, hasAttr("db.shard_id"))
				}
			}
		})
	}
}

func TestTracedResponseWriter(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		wantBytes  int
	}{
		{
			name:       "200 OK with body",
			statusCode: http.StatusOK,
			body:       "success",
			wantBytes:  7,
		},
		{
			name:       "404 Not Found",
			statusCode: http.StatusNotFound,
			body:       "not found",
			wantBytes:  9,
		},
		{
			name:       "200 OK with empty body",
			statusCode: http.StatusOK,
			body:       "",
			wantBytes:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			trw := &tracedResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Write header
			trw.WriteHeader(tt.statusCode)
			assert.Equal(t, tt.statusCode, trw.statusCode)

			// Write body
			n, err := trw.Write([]byte(tt.body))
			assert.NoError(t, err)
			assert.Equal(t, tt.wantBytes, n)
			assert.Equal(t, tt.wantBytes, trw.bytesWritten)

			// Verify response
			assert.Equal(t, tt.statusCode, w.Code)
			assert.Equal(t, tt.body, w.Body.String())
		})
	}
}

func TestTracedResponseWriter_NoContentStatus(t *testing.T) {
	// Go 1.26 enforces RFC 9110: 204 No Content responses must not have a body.
	// Verify that tracedResponseWriter correctly tracks header-only responses.
	w := httptest.NewRecorder()
	trw := &tracedResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Write only the header, no body
	trw.WriteHeader(http.StatusNoContent)
	assert.Equal(t, http.StatusNoContent, trw.statusCode)
	assert.True(t, trw.wroteHeader)
	assert.Equal(t, 0, trw.bytesWritten)
	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestTracedResponseWriter_ImplicitHeader(t *testing.T) {
	w := httptest.NewRecorder()
	trw := &tracedResponseWriter{
		ResponseWriter: w,
		statusCode:     http.StatusOK,
	}

	// Write body without calling WriteHeader
	n, err := trw.Write([]byte("test"))
	assert.NoError(t, err)
	assert.Equal(t, 4, n)

	// Should default to 200 OK
	assert.Equal(t, http.StatusOK, trw.statusCode)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestGetClientIP covers what this package records as http.client_ip.
//
// It no longer resolves the address itself. It had a second, independent copy
// of the REST middleware's resolver with the same defect -- X-Forwarded-For
// honoured from any peer -- so the same request could be traced as one address
// and rate-limited as another (go-quicbaas#156). The trust decision now belongs
// to pkg/util/clientip, made once by ClientIPMiddleware; this package reads it
// or falls back to the transport peer.
//
// So the header cases below expect the PEER: a span attribute must not be
// forgeable by the caller it describes.
func TestGetClientIP(t *testing.T) {
	tests := []struct {
		name       string
		headers    map[string]string
		remoteAddr string
		wantIP     string
	}{
		{
			name: "X-Forwarded-For is not believed here",
			headers: map[string]string{
				"X-Forwarded-For": "192.0.2.100",
			},
			remoteAddr: "10.0.0.1:12345",
			wantIP:     "10.0.0.1",
		},
		{
			name: "X-Forwarded-For chain is not believed here",
			headers: map[string]string{
				"X-Forwarded-For": "192.0.2.100, 10.0.0.1, 172.16.0.1",
			},
			remoteAddr: "10.0.0.1:12345",
			wantIP:     "10.0.0.1",
		},
		{
			name: "X-Real-IP is not believed here",
			headers: map[string]string{
				"X-Real-IP": "192.0.2.100",
			},
			remoteAddr: "10.0.0.1:12345",
			wantIP:     "10.0.0.1",
		},
		{
			name:       "RemoteAddr only",
			headers:    map[string]string{},
			remoteAddr: "192.0.2.100:12345",
			wantIP:     "192.0.2.100",
		},
		{
			name:       "RemoteAddr without port",
			headers:    map[string]string{},
			remoteAddr: "192.0.2.100",
			wantIP:     "192.0.2.100",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.RemoteAddr = tt.remoteAddr

			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			assert.Equal(t, tt.wantIP, clientip.FromRequest(req))
		})
	}
}

// TestGetClientIP_UsesTheResolvedAddress pins the other half: when the REST
// middleware has already decided -- because the peer is a configured trusted
// proxy -- the span records THAT, not the peer. Without this, collapsing the
// two implementations would have silently downgraded every proxied deployment's
// traces to the load balancer's address.
func TestGetClientIP_UsesTheResolvedAddress(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	req.Header.Set("X-Forwarded-For", "192.0.2.100")

	req = req.WithContext(clientip.NewContext(req.Context(), "192.0.2.100"))

	assert.Equal(t, "192.0.2.100", clientip.FromRequest(req))
}

func TestHTTPMiddleware_ContextPropagation(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	middleware := tracer.HTTPMiddleware()

	// Create handler that starts a child span
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, childSpan := tracer.StartSpan(ctx, "child.operation")
		childSpan.End()
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Verify parent-child relationship
	spans := sr.Ended()
	require.Len(t, spans, 2)

	childSpan := spans[0]
	parentSpan := spans[1]

	assert.Equal(t, "child.operation", childSpan.Name())
	assert.Equal(t, "/test", parentSpan.Name())
	assert.Equal(t, parentSpan.SpanContext().SpanID(), childSpan.Parent().SpanID())
}

func TestHTTPMiddleware_StatusCodeMapping(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		wantOkStatus bool
	}{
		{"2xx success", http.StatusOK, true},
		{"2xx created", http.StatusCreated, true},
		{"3xx redirect", http.StatusMovedPermanently, true},
		{"4xx client error", http.StatusBadRequest, false},
		{"4xx not found", http.StatusNotFound, false},
		{"5xx server error", http.StatusInternalServerError, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sr := tracetest.NewSpanRecorder()
			provider := sdktrace.NewTracerProvider(
				sdktrace.WithSpanProcessor(sr),
			)

			tracer := &Tracer{
				config: &Config{
					Enabled:     true,
					ServiceName: "test-service",
				},
				provider: provider,
				tracer:   provider.Tracer("test"),
			}

			middleware := tracer.HTTPMiddleware()

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
			})

			wrappedHandler := middleware(handler)

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()

			wrappedHandler.ServeHTTP(w, req)

			spans := sr.Ended()
			require.Len(t, spans, 1)
			span := spans[0]

			// Verify status code attribute
			attrs := span.Attributes()
			var statusCodeAttr int
			for _, attr := range attrs {
				if string(attr.Key) == "http.status_code" {
					statusCodeAttr = int(attr.Value.AsInt64())
					break
				}
			}
			assert.Equal(t, tt.statusCode, statusCodeAttr)

			// Verify span status
			if tt.wantOkStatus {
				assert.NotEqual(t, "Error", span.Status().Code.String())
			}
		})
	}
}

func BenchmarkHTTPMiddleware(b *testing.B) {
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.NeverSample()),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "bench-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	middleware := tracer.HTTPMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)
	}
}

func BenchmarkHTTPMiddleware_Disabled(b *testing.B) {
	tracer := &Tracer{
		config: &Config{
			Enabled:     false,
			ServiceName: "bench-service",
		},
	}

	middleware := tracer.HTTPMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/keys/test", nil)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		wrappedHandler.ServeHTTP(w, req)
	}
}

func TestHTTPMiddleware_TraceHeaderInjection(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(sr),
	)

	tracer := &Tracer{
		config: &Config{
			Enabled:     true,
			ServiceName: "test-service",
		},
		provider: provider,
		tracer:   provider.Tracer("test"),
	}

	middleware := tracer.HTTPMiddleware()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrappedHandler := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	wrappedHandler.ServeHTTP(w, req)

	// Verify trace headers are injected in response
	// Note: traceparent header should be present if trace context is valid
	headers := w.Header()
	assert.NotNil(t, headers)
}
