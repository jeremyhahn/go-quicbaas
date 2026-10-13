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
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteResponseBody_WritesBody(t *testing.T) {
	rec := httptest.NewRecorder()

	writeResponseBody(rec, httptest.NewRequest(http.MethodGet, "/", nil), []byte("payload"))

	assert.Equal(t, "payload", rec.Body.String())
}

func TestWriteResponseBody_LogsWriteFailure(t *testing.T) {
	var logs bytes.Buffer
	t.Cleanup(swapDefaultSlog(&logs))
	w := &failingResponseWriter{header: http.Header{}, err: errors.New("client went away")}

	writeResponseBody(w, httptest.NewRequest(http.MethodGet, "/", nil), []byte("payload"))

	assert.Contains(t, logs.String(), "response body write failed")
	assert.Contains(t, logs.String(), "client went away")
}
