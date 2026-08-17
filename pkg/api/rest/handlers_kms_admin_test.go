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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// kmsAdminReq builds a chi request against a /api/v1/kms/keys/{name}/<suffix>
// endpoint with the URL parameter populated.
func kmsAdminReq(method, keyName, suffix string, body interface{}) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/api/v1/kms/keys/"+keyName+suffix, &buf)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", keyName)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

// =============================================================================
// HandleRevokeKey
// =============================================================================

func TestHandleRevokeKey_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("RevokeKey", mock.Anything, mock.MatchedBy(func(r *service.RevokeKeyRequest) bool {
		return r.KeyID == "k1" && r.Reason == "compromised"
	})).Return(&service.RevokeKeyResponse{
		KeyID:     "k1",
		Status:    service.KeyStatusCompromised,
		Timestamp: time.Now(),
	}, nil).Once()

	w := httptest.NewRecorder()
	server.HandleRevokeKey(w, kmsAdminReq(http.MethodPost, "k1", "/revoke",
		RevokeKeyRESTRequest{Reason: "compromised"}))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp RevokeKeyRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "k1", resp.KeyID)
	assert.Equal(t, string(service.KeyStatusCompromised), resp.Status)
}

func TestHandleRevokeKey_EmptyBodyRevokesWithoutReason(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("RevokeKey", mock.Anything, mock.MatchedBy(func(r *service.RevokeKeyRequest) bool {
		return r.KeyID == "k1" && r.Reason == ""
	})).Return(&service.RevokeKeyResponse{
		KeyID: "k1", Status: service.KeyStatusDisabled, Timestamp: time.Now(),
	}, nil).Once()

	w := httptest.NewRecorder()
	server.HandleRevokeKey(w, kmsAdminReq(http.MethodPost, "k1", "/revoke", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestHandleRevokeKey_MissingName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleRevokeKey(w, kmsAdminReq(http.MethodPost, "", "/revoke", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRevokeKey_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/kms/keys/k1/revoke",
		bytes.NewReader([]byte("{not-json")))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "k1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	server.HandleRevokeKey(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRevokeKey_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("RevokeKey", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrInternal, "kms", "boom")).Once()

	w := httptest.NewRecorder()
	server.HandleRevokeKey(w, kmsAdminReq(http.MethodPost, "k1", "/revoke", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// HandleGetKeyMetadata
// =============================================================================

func TestHandleGetKeyMetadata_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	now := time.Now()
	mockService.On("GetKeyMetadata", mock.Anything, mock.MatchedBy(func(r *service.GetKeyMetadataRequest) bool {
		return r.KeyID == "k1"
	})).Return(&service.GetKeyMetadataResponse{
		Metadata: &service.KeyMetadata{
			KeyID:     "k1",
			Name:      "my-key",
			Algorithm: "AES-256-GCM",
			Status:    service.KeyStatusEnabled,
			Version:   2,
			CreatedAt: now,
			UpdatedAt: now,
			Tags:      map[string]string{"env": "test"},
		},
		Timestamp: now,
	}, nil).Once()

	w := httptest.NewRecorder()
	server.HandleGetKeyMetadata(w, kmsAdminReq(http.MethodGet, "k1", "/metadata", nil))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp GetKeyMetadataRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Metadata)
	assert.Equal(t, "k1", resp.Metadata.KeyID)
	assert.Equal(t, "my-key", resp.Metadata.Name)
	assert.Equal(t, 2, resp.Metadata.Version)
}

func TestHandleGetKeyMetadata_MissingName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleGetKeyMetadata(w, kmsAdminReq(http.MethodGet, "", "/metadata", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetKeyMetadata_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetKeyMetadata", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrNotFound, "kms", "no such key")).Once()

	w := httptest.NewRecorder()
	server.HandleGetKeyMetadata(w, kmsAdminReq(http.MethodGet, "missing", "/metadata", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// =============================================================================
// HandleUpdateKeyMetadata
// =============================================================================

func TestHandleUpdateKeyMetadata_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	now := time.Now()
	newName := "renamed"
	mockService.On("UpdateKeyMetadata", mock.Anything, mock.MatchedBy(func(r *service.UpdateKeyMetadataRequest) bool {
		return r.KeyID == "k1" && r.Name != nil && *r.Name == "renamed" &&
			r.Description == nil && len(r.RemoveTags) == 1
	})).Return(&service.UpdateKeyMetadataResponse{
		Metadata: &service.KeyMetadata{
			KeyID: "k1", Name: newName, Algorithm: "AES-256-GCM",
			Status: service.KeyStatusEnabled, CreatedAt: now, UpdatedAt: now,
		},
		Timestamp: now,
	}, nil).Once()

	w := httptest.NewRecorder()
	server.HandleUpdateKeyMetadata(w, kmsAdminReq(http.MethodPut, "k1", "/metadata",
		UpdateKeyMetadataRESTRequest{Name: &newName, RemoveTags: []string{"stale"}}))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp UpdateKeyMetadataRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Metadata)
	assert.Equal(t, "renamed", resp.Metadata.Name)
}

func TestHandleUpdateKeyMetadata_MissingName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleUpdateKeyMetadata(w, kmsAdminReq(http.MethodPut, "", "/metadata",
		UpdateKeyMetadataRESTRequest{}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateKeyMetadata_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/k1/metadata",
		bytes.NewReader([]byte("{not-json")))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "k1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	server.HandleUpdateKeyMetadata(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleUpdateKeyMetadata_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("UpdateKeyMetadata", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrInternal, "kms", "boom")).Once()

	w := httptest.NewRecorder()
	server.HandleUpdateKeyMetadata(w, kmsAdminReq(http.MethodPut, "k1", "/metadata",
		UpdateKeyMetadataRESTRequest{}))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// HandleGetPublicKey
// =============================================================================

func TestHandleGetPublicKey_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	der := []byte{0x30, 0x82, 0x01, 0x22}
	mockService.On("GetPublicKey", mock.Anything, mock.MatchedBy(func(r *service.GetPublicKeyRequest) bool {
		return r.KeyID == "k1" && r.Version == 0
	})).Return(&service.GetPublicKeyResponse{
		KeyID:     "k1",
		PublicKey: der,
		Algorithm: "RSA-2048",
		Version:   3,
		Timestamp: time.Now(),
	}, nil).Once()

	w := httptest.NewRecorder()
	server.HandleGetPublicKey(w, kmsAdminReq(http.MethodGet, "k1", "/public", nil))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp GetPublicKeyRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "k1", resp.KeyID)
	assert.Equal(t, der, resp.PublicKey)
	assert.Equal(t, "RSA-2048", resp.Algorithm)
	assert.Equal(t, uint64(3), resp.Version)
}

func TestHandleGetPublicKey_VersionParam(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetPublicKey", mock.Anything, mock.MatchedBy(func(r *service.GetPublicKeyRequest) bool {
		return r.KeyID == "k1" && r.Version == 2
	})).Return(&service.GetPublicKeyResponse{
		KeyID: "k1", PublicKey: []byte{0x01}, Algorithm: "ECDSA-P256", Version: 2, Timestamp: time.Now(),
	}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/k1/public?version=2", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "k1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	server.HandleGetPublicKey(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestHandleGetPublicKey_InvalidVersion(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/keys/k1/public?version=abc", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "k1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	w := httptest.NewRecorder()
	server.HandleGetPublicKey(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetPublicKey_MissingName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleGetPublicKey(w, kmsAdminReq(http.MethodGet, "", "/public", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetPublicKey_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetPublicKey", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrNotFound, "kms", "no such key")).Once()

	w := httptest.NewRecorder()
	server.HandleGetPublicKey(w, kmsAdminReq(http.MethodGet, "missing", "/public", nil))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// =============================================================================
// HandleGetSupportedAlgorithms
// =============================================================================

func TestHandleGetSupportedAlgorithms_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetSupportedAlgorithms", mock.Anything).Return(
		[]string{"AES-256-GCM", "RSA-2048"}, nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/algorithms", nil)
	w := httptest.NewRecorder()
	server.HandleGetSupportedAlgorithms(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp SupportedAlgorithmsRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, []string{"AES-256-GCM", "RSA-2048"}, resp.Algorithms)
}

func TestHandleGetSupportedAlgorithms_NilBecomesEmpty(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetSupportedAlgorithms", mock.Anything).Return(
		([]string)(nil), nil).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/algorithms", nil)
	w := httptest.NewRecorder()
	server.HandleGetSupportedAlgorithms(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp SupportedAlgorithmsRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotNil(t, resp.Algorithms)
	assert.Empty(t, resp.Algorithms)
}

func TestHandleGetSupportedAlgorithms_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetSupportedAlgorithms", mock.Anything).Return(
		([]string)(nil), qberrors.New(qberrors.ErrInternal, "kms", "boom")).Once()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/kms/algorithms", nil)
	w := httptest.NewRecorder()
	server.HandleGetSupportedAlgorithms(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// =============================================================================
// PEP mapping for the new KMS admin routes
// =============================================================================

// TestMapRequestToPermission_KMSAdminRoutes asserts the new REST routes resolve
// to the same qrdb permissions as the gRPC canon (authz_mapping.go).
func TestMapRequestToPermission_KMSAdminRoutes(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		method       string
		wantResource string
		wantAction   string
	}{
		{"revoke key", "/api/v1/kms/keys/k1/revoke", http.MethodPost, "keys", "delete"},
		{"update key metadata", "/api/v1/kms/keys/k1/metadata", http.MethodPut, "keys", "rotate"},
		{"get key metadata", "/api/v1/kms/keys/k1/metadata", http.MethodGet, "keys", "get"},
		{"get public key", "/api/v1/kms/keys/k1/public", http.MethodGet, "keys", "get"},
		{"supported algorithms", "/api/v1/kms/algorithms", http.MethodGet, "keys", "list"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource, action, ok := routePermissionForPath(tt.path, tt.method)
			require.True(t, ok, "expected mapping for %s %s", tt.method, tt.path)
			assert.Equal(t, tt.wantResource, resource)
			assert.Equal(t, tt.wantAction, action)
		})
	}
}
