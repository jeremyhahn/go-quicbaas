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

import "net/http"

// The logging/request-ID middleware wraps every response in *responseWriter to
// capture the status code and byte count. That wrapper embeds http.ResponseWriter
// for the common methods, but the embedded interface does NOT promote optional
// behavioral interfaces like http.Flusher — so a wrapped writer fails a plain
// `w.(http.Flusher)` type assertion even when the underlying connection can
// flush. Streaming handlers (SSE session-events tail, NDJSON inference/object
// transfer/secrets) all type-assert http.Flusher; without these forwarders the
// session-events SSE path returns HTTP 500 ("streaming unsupported on this
// connection") before the text/event-stream headers are written.
//
// Implementing Flush forwards the call when the underlying writer supports it,
// and Unwrap lets http.NewResponseController reach through the wrapper. Together
// they make the capture wrapper transparent to streaming, which is the standard
// net/http (Go 1.20+) contract for response-writer middleware.

// Flush forwards a flush to the underlying ResponseWriter when it implements
// http.Flusher. It is a no-op for non-flushable writers so callers can treat
// flushing as best-effort without a panic.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap returns the wrapped ResponseWriter so http.NewResponseController can
// access optional interfaces (Flusher, Hijacker, etc.) the capture wrapper does
// not itself re-declare. This is the idiomatic Go 1.20+ unwrap contract.
func (rw *responseWriter) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}
