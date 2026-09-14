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
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestRoutes_SecretsVaultCarriesTenantScopedPermissions is the authorization
// regression for go-quicbaas#534.
//
// The tenant secret vault was authorized with go-qrdb's credentials:* family,
// which guards a DIFFERENT store: qrdb's named-credential store has no tenant
// namespacing, so qrdb registers every credentials:* permission ScopeCross and
// its RBAC authorizer denies them to any identity outside the system tenant
// (go-qrdb pkg/permission/constants.go, pkg/auth/rbac/rbac.go). Every route
// below writes or reads under the CALLER's tenant, so no tenant identity could
// reach any of them and the grant sat on the tenant Admin role unusable.
//
// The family that describes these routes is secrets:read / secrets:write /
// secrets:delete / secrets:list — declared ScopeTenantOnly by qrdb for exactly
// this consumer (pkg/permission/broker_scope.go) and already on the built-in
// Admin role (pkg/permission/builtin_role_grants.go).
func TestRoutes_SecretsVaultCarriesTenantScopedPermissions(t *testing.T) {
	want := map[string]struct{ resource, action string }{
		"GET /api/v1/secrets/":           {"secrets", "list"},
		"GET /api/v1/secrets/*":          {"secrets", "read"},
		"POST /api/v1/secrets/*":         {"secrets", "write"},
		"PUT /api/v1/secrets/*":          {"secrets", "write"},
		"DELETE /api/v1/secrets/*":       {"secrets", "delete"},
		"GET /api/v1/secrets-prefix/*":   {"secrets", "read"},
		"GET /api/v1/secrets-versions/*": {"secrets", "read"},
		"GET /api/v1/secrets-watch/*":    {"secrets", "read"},
	}

	seen := map[string]struct{}{}
	for _, spec := range Routes() {
		key := spec.Method + " " + spec.Pattern
		exp, ok := want[key]
		if !ok {
			continue
		}
		seen[key] = struct{}{}
		assert.Equal(t, RouteAuthPermission, spec.Auth, "%s must stay permission-gated", key)
		assert.Equal(t, exp.resource, spec.Resource,
			"%s: the tenant secret vault must not be authorized with the system-only credentials family", key)
		assert.Equal(t, exp.action, spec.Action, "%s action", key)
	}

	for key := range want {
		_, ok := seen[key]
		require.True(t, ok, "route %s vanished from the table; the mapping guard is no longer guarding it", key)
	}
}

// TestHandleStoreSecret_ForwardsRequestedTenant pins that the handler hands the
// body's tenant_id to the service rather than dropping it. The service is the
// layer that reconciles it with the caller's tenant
// (pkg/service/secrets_tenant.go secretsScopedTenant); if the handler stopped
// forwarding it, that reconciliation would never run.
func TestHandleStoreSecret_ForwardsRequestedTenant(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	const path = "sdlc/forge/go-truststrap/token"
	mockService.On("StoreSecret", mock.Anything, mock.MatchedBy(func(req *service.StoreSecretRequest) bool {
		return req.Name == path && req.TenantID == "automatethethings"
	})).Return(&service.StoreSecretResponse{SecretID: path, Version: 1}, nil).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+path,
		strings.NewReader(`{"data":"dmFsdWU=","tenant_id":"automatethethings"}`))
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", path)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	mockService.AssertExpectations(t)
}

// TestHandleStoreSecret_CrossTenantRefusalIsLegible pins how the new refusal
// reaches the caller: 403 with the reason, not a sanitized body. Before
// go-quicbaas#534 the cross-tenant code fell through handleServiceError's
// default arm, which describes SERVER faults — it replaced the reason with a
// generic sentence and logged the caller's own mistake as a service failure.
func TestHandleStoreSecret_CrossTenantRefusalIsLegible(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	const path = "sdlc/forge/go-truststrap/token"
	mockService.On("StoreSecret", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
		Code: errors.ErrCrossTenantAccess,
		Op:   "secrets.StoreSecret",
		Err:  service.NewError(errors.ErrCrossTenantAccess, "secrets.StoreSecret", "not the caller's tenant"),
	}).Once()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/secrets/"+path,
		strings.NewReader(`{"data":"dmFsdWU=","tenant_id":"someone-else"}`))
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", path)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.HandleStoreSecret(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "not the caller's tenant",
		"the refusal must say which request was refused, not a generic server sentence")
	mockService.AssertExpectations(t)
}
