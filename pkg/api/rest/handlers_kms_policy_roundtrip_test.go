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
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// policyStoreKMS backs SetKeyPolicy and GetKeyPolicy with one stored policy,
// stamping version and updated_at the way the KMS service does.
func policyStoreKMS() *mocks.MockKMSService {
	var stored *service.KeyPolicy
	return &mocks.MockKMSService{
		SetKeyPolicyFunc: func(ctx context.Context, req *service.SetKeyPolicyRequest) (*service.SetKeyPolicyResponse, error) {
			p := *req.Policy
			p.Version = req.Policy.Version + 1
			p.UpdatedAt = time.Now()
			stored = &p
			return &service.SetKeyPolicyResponse{KeyID: req.KeyID, PolicyVersion: p.Version, Timestamp: p.UpdatedAt}, nil
		},
		GetKeyPolicyFunc: func(ctx context.Context, req *service.GetKeyPolicyRequest) (*service.GetKeyPolicyResponse, error) {
			return &service.GetKeyPolicyResponse{Policy: stored, Timestamp: time.Now()}, nil
		},
	}
}

// doKMSPolicyRequest runs the policy handler for key "k1" and returns the recorder.
func doKMSPolicyRequest(t *testing.T, server *Server, method, body string) *httptest.ResponseRecorder {
	t.Helper()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("name", "k1")
	req := httptest.NewRequest(method, "/api/v1/kms/keys/k1/policy", strings.NewReader(body))
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	if method == http.MethodPut {
		server.HandleSetKeyPolicy(w, req)
	} else {
		server.HandleGetKeyPolicy(w, req)
	}
	return w
}

// getKMSPolicy GETs the stored policy and returns the raw body and its decoded
// policy object.
func getKMSPolicy(t *testing.T, server *Server) (string, map[string]json.RawMessage) {
	t.Helper()
	w := doKMSPolicyRequest(t, server, http.MethodGet, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body struct {
		Policy map[string]json.RawMessage `json:"policy"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return w.Body.String(), body.Policy
}

func TestKeyPolicyREST_GetBodyRoundTripsThroughPut(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()
	mockService.MockKMSService = policyStoreKMS()

	set := `{"policy":{"allowed_operations":["encrypt","decrypt"],` +
		`"allowed_principals":["app-a","app-b"],"denied_principals":["intruder"],` +
		`"expires_at":"2031-02-03T04:05:06.789Z","rotation_required":true,` +
		`"max_usage_count":500,"require_mfa":true}}`
	w := doKMSPolicyRequest(t, server, http.MethodPut, set)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	getBody, first := getKMSPolicy(t, server)
	assert.JSONEq(t, `"2031-02-03T04:05:06.789Z"`, string(first["expires_at"]))
	assert.JSONEq(t, `500`, string(first["max_usage_count"]))
	assert.JSONEq(t, `["app-a","app-b"]`, string(first["allowed_principals"]))
	assert.JSONEq(t, `["intruder"]`, string(first["denied_principals"]))

	w = doKMSPolicyRequest(t, server, http.MethodPut, getBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	_, second := getKMSPolicy(t, server)
	for _, field := range []string{
		"key_id", "allowed_operations", "allowed_principals", "denied_principals",
		"expires_at", "rotation_required", "max_usage_count", "require_mfa",
	} {
		assert.JSONEq(t, string(first[field]), string(second[field]), field)
	}
	assert.JSONEq(t, `1`, string(first["version"]))
	assert.JSONEq(t, `2`, string(second["version"]))
}

func TestKeyPolicyREST_UnrestrictedPolicyWritesEmptyListsAndNulls(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()
	mockService.MockKMSService = policyStoreKMS()

	w := doKMSPolicyRequest(t, server, http.MethodPut, `{"policy":{"allowed_operations":["sign"]}}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	getBody, policy := getKMSPolicy(t, server)
	assert.Equal(t, `[]`, string(policy["allowed_principals"]))
	assert.Equal(t, `[]`, string(policy["denied_principals"]))
	assert.Equal(t, `null`, string(policy["expires_at"]))
	assert.Equal(t, `null`, string(policy["max_usage_count"]))

	w = doKMSPolicyRequest(t, server, http.MethodPut, getBody)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, again := getKMSPolicy(t, server)
	assert.Equal(t, `null`, string(again["expires_at"]))
	assert.Equal(t, `null`, string(again["max_usage_count"]))
}

func TestKeyPolicyREST_PutRefusesMalformedPolicy(t *testing.T) {
	server, mockService, cleanup := setupMockServerWithFuncMock(t)
	defer cleanup()
	mockService.MockKMSService = policyStoreKMS()

	for name, body := range map[string]string{
		"unix expires_at":      `{"policy":{"expires_at":1800000000}}`,
		"non RFC 3339 expires": `{"policy":{"expires_at":"next tuesday"}}`,
		"string usage count":   `{"policy":{"max_usage_count":"ten"}}`,
		"principals not list":  `{"policy":{"allowed_principals":"app-a"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			w := doKMSPolicyRequest(t, server, http.MethodPut, body)
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}
}
