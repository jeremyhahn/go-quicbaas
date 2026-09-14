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
	"strconv"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/clientip"
)

// HTTPMiddleware returns HTTP middleware that traces requests.
func (t *Tracer) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if tracing is disabled
			if !t.IsEnabled() {
				next.ServeHTTP(w, r)
				return
			}

			// Extract trace context from HTTP headers
			ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Start span
			spanCtx, span := t.tracer.Start(ctx, r.URL.Path,
				trace.WithSpanKind(trace.SpanKindServer),
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.String()),
					attribute.String("http.scheme", r.URL.Scheme),
					attribute.String("http.host", r.Host),
					attribute.String("http.target", r.URL.Path),
					attribute.String("http.user_agent", r.UserAgent()),
					attribute.String("http.client_ip", clientip.FromRequest(r)),
					AttrNetworkProtocol.String("http"),
				),
			)
			defer span.End()

			// Wrap response writer to capture status code
			wrapped := &tracedResponseWriter{
				ResponseWriter: w,
				statusCode:     http.StatusOK,
			}

			// Process request with traced context
			next.ServeHTTP(wrapped, r.WithContext(spanCtx))

			// Add response attributes
			span.SetAttributes(
				attribute.Int("http.status_code", wrapped.statusCode),
				attribute.Int64("http.response_content_length", int64(wrapped.bytesWritten)),
			)

			// Set span status based on HTTP status code
			if wrapped.statusCode >= 400 {
				span.SetStatus(codes.Error, http.StatusText(wrapped.statusCode))
			} else {
				span.SetStatus(codes.Ok, "")
			}

			// Check for shard header and add as attribute
			if shard := wrapped.Header().Get("X-QuicBaaS-Shard"); shard != "" {
				if shardID, err := strconv.ParseUint(shard, 10, 64); err == nil {
					span.SetAttributes(AttrDBShardID.Int64(conv.ClampUint64ToInt64(shardID)))
				}
			}

			// Inject trace context into response headers
			propagation.TraceContext{}.Inject(spanCtx, propagation.HeaderCarrier(wrapped.Header()))
		})
	}
}

// tracedResponseWriter wraps http.ResponseWriter to capture status code and bytes.
type tracedResponseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
	wroteHeader  bool
}

// WriteHeader captures the status code.
func (trw *tracedResponseWriter) WriteHeader(statusCode int) {
	if !trw.wroteHeader {
		trw.statusCode = statusCode
		trw.ResponseWriter.WriteHeader(statusCode)
		trw.wroteHeader = true
	}
}

// Write captures bytes written and ensures header is written.
func (trw *tracedResponseWriter) Write(b []byte) (int, error) {
	if !trw.wroteHeader {
		trw.WriteHeader(http.StatusOK)
	}
	n, err := trw.ResponseWriter.Write(b)
	trw.bytesWritten += n
	return n, err
}
