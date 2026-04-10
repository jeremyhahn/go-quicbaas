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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// TestHandleEnsureSecret_HappyPath exercises the REST-layer wiring for
// the Vault-style generate-on-first-access primitive using a mocked
// service. It asserts the request is decoded + forwarded correctly and
// the response JSON is emitted.
func TestHandleEnsureSecret_HappyPath(t *testing.T) {
	server, mockSvc, cleanup := setupMockServer(t)
	defer cleanup()

	path := "tenants/t1/nodes/n1/secrets/platform/so-pin"
	mockSvc.On("EnsureSecret", mock.Anything, mock.MatchedBy(func(req *service.EnsureSecretRequest) bool {
		return req.SecretID == path && req.Length == 8 && req.Encoding == secrets.EncodingNumeric
	})).Return(&service.EnsureSecretResponse{
		SecretID:  path,
		Data:      []byte("12345678"),
		Generated: true,
		Version:   1,
		Timestamp: time.Now(),
	}, nil).Once()

	body, err := json.Marshal(map[string]interface{}{
		"length":   8,
		"encoding": secrets.EncodingNumeric,
	})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+path+"/ensure", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", path+"/ensure")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleEnsureSecret(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var resp service.EnsureSecretResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, path, resp.SecretID)
	assert.True(t, resp.Generated)
	assert.Equal(t, []byte("12345678"), resp.Data)
	mockSvc.AssertExpectations(t)
}

// TestHandleEnsureSecret_InvalidSpec verifies the REST boundary
// rejects malformed SecretSpec values with 400 before hitting the
// service layer — the no-silent-fallback rule.
func TestHandleEnsureSecret_InvalidSpec(t *testing.T) {
	cases := []struct {
		name string
		body map[string]interface{}
	}{
		{"zero length", map[string]interface{}{"length": 0, "encoding": "base64"}},
		{"negative length", map[string]interface{}{"length": -1, "encoding": "base64"}},
		{"missing encoding", map[string]interface{}{"length": 16}},
		{"unknown encoding", map[string]interface{}{"length": 16, "encoding": "weird"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server, mockSvc, cleanup := setupMockServer(t)
			defer cleanup()

			body, _ := json.Marshal(tc.body)
			req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/p/ensure", bytes.NewReader(body))
			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", "p/ensure")
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
			w := httptest.NewRecorder()

			server.HandleEnsureSecret(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
			// Service layer must NOT be called for malformed input.
			mockSvc.AssertNotCalled(t, "EnsureSecret", mock.Anything, mock.Anything)
		})
	}
}

// TestHandleEnsureSecret_SpecMismatchHardError verifies a service-layer
// error (simulating a pre-existing secret with a different spec) is
// surfaced as an HTTP error rather than silently retried.
func TestHandleEnsureSecret_SpecMismatchHardError(t *testing.T) {
	server, mockSvc, cleanup := setupMockServer(t)
	defer cleanup()

	mockSvc.On("EnsureSecret", mock.Anything, mock.Anything).
		Return((*service.EnsureSecretResponse)(nil), secrets.ErrSecretSpecMismatch).Once()

	body, _ := json.Marshal(map[string]interface{}{
		"length":   16,
		"encoding": secrets.EncodingBase64,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/p/ensure", bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "p/ensure")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()

	server.HandleEnsureSecret(w, req)

	assert.GreaterOrEqual(t, w.Code, 400, "body=%s", w.Body.String())
	mockSvc.AssertExpectations(t)
}
