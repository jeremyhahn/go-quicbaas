//go:build rest

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// handlers_service_error_notfound_test.go covers the ErrNotFound mapping of
// handleServiceError: the response body must carry the full typed error
// string (op + code + message, e.g. "iam.role: not_found: role not found")
// instead of the information-free generic "resource not found: entity", so
// REST clients — including the SDK REST transport and the CLI — can tell
// WHICH resource was missing, at parity with the gRPC transport.

package rest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// A not-found service error must surface its full typed message in the
// response body, not a generic placeholder.
func TestHandleServiceError_NotFoundCarriesMessage(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/api/v1/iam/users", nil)
	server.handleServiceError(w, r, qberrors.New(qberrors.ErrNotFound, "iam.role", "role not found"))

	assert.Equal(t, http.StatusNotFound, w.Code)

	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body.Error, "iam.role", "body must identify the failing operation")
	assert.Contains(t, body.Error, "not_found", "body must carry the typed error code")
	assert.Contains(t, body.Error, "role not found", "body must carry the service message")
	assert.NotContains(t, body.Error, "resource not found: entity",
		"the information-free generic body must not be produced for typed errors")
}

// The not-found mapping must not regress the HTTP status for a bare typed
// not-found error without an operation.
func TestHandleServiceError_NotFoundStatusPreserved(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	server.handleServiceError(w, r, qberrors.New(qberrors.ErrNotFound, "", "gone"))

	assert.Equal(t, http.StatusNotFound, w.Code)

	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Contains(t, body.Error, "gone")
}
