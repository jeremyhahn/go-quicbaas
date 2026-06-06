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
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func ensureSecretReq(path, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+path+"/ensure", bytes.NewReader([]byte(body)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", path+"/ensure")
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleEnsureSecret_Validation(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// Missing path -> 400
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/x", bytes.NewReader([]byte(`{}`)))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "")
	r = r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
	server.HandleEnsureSecret(w, r)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid JSON -> 400
	w = httptest.NewRecorder()
	server.HandleEnsureSecret(w, ensureSecretReq("db/pw", "{bad"))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// Invalid spec (length 0) -> 400
	w = httptest.NewRecorder()
	server.HandleEnsureSecret(w, ensureSecretReq("db/pw", `{"length":0,"encoding":"base64"}`))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
