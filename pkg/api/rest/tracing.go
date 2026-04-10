//go:build rest && otel

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
	"net/http"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware creates a span per HTTP request when tracing is enabled.
func TracingMiddleware() func(http.Handler) http.Handler {
	if os.Getenv("QUICBAAS_TRACING_ENABLED") != "1" {
		return func(next http.Handler) http.Handler { return next }
	}
	tracer := otel.Tracer("quicbaas/rest")
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Start(r.Context(), "http.request", trace.WithAttributes(
				attribute.String("http.method", r.Method),
				attribute.String("http.route", r.URL.Path),
			))
			defer span.End()

			next.ServeHTTP(w, r.WithContext(ctx))

			shard := w.Header().Get("X-QuicBaaS-Shard")
			if shard != "" {
				span.SetAttributes(attribute.String("quicbaas.shard_id", shard))
			}
		})
	}
}
