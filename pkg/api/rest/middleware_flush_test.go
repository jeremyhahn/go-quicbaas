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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// flushCounter is an http.ResponseWriter whose Flush call increments a counter,
// so we can prove the wrapper forwards the flush to the underlying writer.
type flushCounter struct {
	http.ResponseWriter
	flushes int
}

func (f *flushCounter) Flush() { f.flushes++ }

// TestResponseWriterUnwrap verifies the logging wrapper exposes its underlying
// writer so http.NewResponseController can reach optional interfaces. Without
// Unwrap, streaming handlers behind the logging middleware fail the Flusher
// lookup and return a 500.
func TestResponseWriterUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	wrapped := &responseWriter{ResponseWriter: rec, statusCode: http.StatusOK}

	require.Equal(t, http.ResponseWriter(rec), wrapped.Unwrap())

	// http.NewResponseController must reach the recorder's Flusher through Unwrap.
	rc := http.NewResponseController(wrapped)
	assert.NoError(t, rc.Flush(), "ResponseController.Flush must succeed through Unwrap")
}

// TestResponseWriterFlushForwards verifies the wrapper's own Flush forwards to a
// flushable underlying writer.
func TestResponseWriterFlushForwards(t *testing.T) {
	fc := &flushCounter{ResponseWriter: httptest.NewRecorder()}
	wrapped := &responseWriter{ResponseWriter: fc, statusCode: http.StatusOK}

	wrapped.Flush()
	wrapped.Flush()

	assert.Equal(t, 2, fc.flushes, "wrapper Flush must forward to the underlying flusher")
}

// TestResponseWriterFlushNonFlushableNoPanic verifies Flush is a safe no-op when
// the underlying writer cannot flush (best-effort contract).
func TestResponseWriterFlushNonFlushableNoPanic(t *testing.T) {
	wrapped := &responseWriter{ResponseWriter: newNonFlushResponseWriter(), statusCode: http.StatusOK}
	assert.NotPanics(t, wrapped.Flush, "Flush on a non-flushable writer must be a no-op")
}
