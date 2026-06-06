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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func kmsRotationReq(method, keyName string, body interface{}) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "/api/v1/kms/keys/"+keyName+"/rotation-policy", &buf)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", keyName)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleSetKeyRotationPolicy_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SetKeyRotationPolicy", mock.Anything, mock.MatchedBy(func(r *service.SetKeyRotationPolicyRequest) bool {
		return r.KeyID == "k1" && r.Policy != nil && r.Policy.Enabled
	})).Return(&service.SetKeyRotationPolicyResponse{
		KeyID: "k1",
		Policy: &service.RotationPolicy{
			Enabled: true, Interval: 24 * time.Hour, RetainVersions: 3, AutoRotate: true,
			NextRotation: time.Now().Add(24 * time.Hour),
		},
		Timestamp: time.Now(),
	}, nil).Once()

	w := httptest.NewRecorder()
	server.HandleSetKeyRotationPolicy(w, kmsRotationReq(http.MethodPut, "k1",
		RotationPolicyRESTRequest{Enabled: true, IntervalSeconds: 86400, RetainVersions: 3, AutoRotate: true}))

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp SetKeyRotationPolicyRESTResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "k1", resp.KeyID)
	require.NotNil(t, resp.Policy)
	assert.Equal(t, int64(86400), resp.Policy.IntervalSeconds)
}

func TestHandleSetKeyRotationPolicy_MissingName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleSetKeyRotationPolicy(w, kmsRotationReq(http.MethodPut, "",
		RotationPolicyRESTRequest{Enabled: true}))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleSetKeyRotationPolicy_InvalidJSON(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/kms/keys/k1/rotation-policy",
		bytes.NewReader([]byte("{not-json")))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "k1")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	server.HandleSetKeyRotationPolicy(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleSetKeyRotationPolicy_ServiceError(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("SetKeyRotationPolicy", mock.Anything, mock.Anything).Return(nil,
		qberrors.New(qberrors.ErrInternal, "kms", "boom")).Once()

	w := httptest.NewRecorder()
	server.HandleSetKeyRotationPolicy(w, kmsRotationReq(http.MethodPut, "k1",
		RotationPolicyRESTRequest{Enabled: true, IntervalSeconds: 100}))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetKeyRotationPolicy_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("GetKeyRotationPolicy", mock.Anything, mock.MatchedBy(func(r *service.GetKeyRotationPolicyRequest) bool {
		return r.KeyID == "k1"
	})).Return(&service.GetKeyRotationPolicyResponse{
		KeyID:     "k1",
		Policy:    &service.RotationPolicy{Enabled: true, Interval: time.Hour, RetainVersions: 2},
		Timestamp: time.Now(),
	}, nil).Once()

	w := httptest.NewRecorder()
	server.HandleGetKeyRotationPolicy(w, kmsRotationReq(http.MethodGet, "k1", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestHandleGetKeyRotationPolicy_MissingName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleGetKeyRotationPolicy(w, kmsRotationReq(http.MethodGet, "", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleDeleteKeyRotationPolicy_Success(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	mockService.On("DeleteKeyRotationPolicy", mock.Anything, mock.Anything).Return(
		&service.DeleteKeyRotationPolicyResponse{KeyID: "k1", Timestamp: time.Now()}, nil).Once()

	w := httptest.NewRecorder()
	server.HandleDeleteKeyRotationPolicy(w, kmsRotationReq(http.MethodDelete, "k1", nil))
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
}

func TestHandleDeleteKeyRotationPolicy_MissingName(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	server.HandleDeleteKeyRotationPolicy(w, kmsRotationReq(http.MethodDelete, "", nil))
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// serviceToRESTRotationPolicy converts a service policy (including nil).
func TestServiceToRESTRotationPolicy(t *testing.T) {
	assert.Nil(t, serviceToRESTRotationPolicy(nil))

	next := time.Now().Add(time.Hour)
	out := serviceToRESTRotationPolicy(&service.RotationPolicy{
		Enabled: true, Interval: 2 * time.Hour, RetainVersions: 5, AutoRotate: true, NextRotation: next,
	})
	require.NotNil(t, out)
	assert.Equal(t, int64(7200), out.IntervalSeconds)
	assert.Equal(t, 5, out.RetainVersions)
	assert.Equal(t, next.Unix(), out.NextRotationUnix)
}
