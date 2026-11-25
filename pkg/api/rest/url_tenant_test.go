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
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// namedTenant is the tenant a request names in the URL.
const namedTenant = "acme"

// systemTenantID returns the tenant a system caller is bound to.
func systemTenantID(t testing.TB) string {
	t.Helper()
	tenantID, err := tenantctx.FromContext(tenantctx.WithSystem(context.Background()), "rest.IAM")
	require.NoError(t, err)
	return tenantID
}

// urlTenantRequest is a request for a tenant-scoped IAM route that names
// tenantID in its URL, made by the caller ctx carries.
func urlTenantRequest(ctx context.Context, tenantID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+tenantID+"/roles", nil)
	return req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
}

// TestURLTenant_OwnTenantGetsNoSecondDecision proves a caller acting in its
// own tenant behaves as before: no route permission, identity or authorizer
// is needed, and no decision is rendered.
func TestURLTenant_OwnTenantGetsNoSecondDecision(t *testing.T) {
	for _, tc := range []struct {
		name   string
		caller context.Context
		named  string
	}{
		{
			name:   "a tenant caller names its own tenant",
			caller: tenantctx.WithTenant(context.Background(), namedTenant),
			named:  namedTenant,
		},
		{
			name:   "a system caller names the system tenant",
			caller: tenantctx.WithSystem(context.Background()),
			named:  systemTenantID(t),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("no authorizer wired", func(t *testing.T) {
				var s *Server
				req := urlTenantRequest(tc.caller, tc.named)
				w := httptest.NewRecorder()

				_, tenantID, ok := s.urlTenant(w, req, "rest.IAM", "tenant_id")

				require.True(t, ok, "body: %s", w.Body.String())
				assert.Equal(t, tc.named, tenantID)
			})

			t.Run("a wired authorizer is not consulted", func(t *testing.T) {
				var s Server
				authz := &scopeAuthorizerStub{err: errors.New("must not be consulted")}
				s.SetBarrierScopeAuthorizer(authz)
				req := urlTenantRequest(tc.caller, tc.named)
				w := httptest.NewRecorder()

				_, tenantID, ok := s.urlTenant(w, req, "rest.IAM", "tenant_id")

				require.True(t, ok, "body: %s", w.Body.String())
				assert.Equal(t, tc.named, tenantID)
				assert.Empty(t, authz.made())
			})
		})
	}
}

// TestURLTenant_TenantCallerNamingAnotherTenantIsStillRefused proves a tenant
// caller naming another tenant is refused by tenantctx.ContextForTarget
// before any authorizer is asked, whatever it allows.
func TestURLTenant_TenantCallerNamingAnotherTenantIsStillRefused(t *testing.T) {
	var s Server
	authz := &scopeAuthorizerStub{}
	s.SetBarrierScopeAuthorizer(authz)
	ctx := tenantctx.WithTenant(context.Background(), "globex")
	ctx = qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{Subject: "operator", TenantID: "globex", Roles: []string{"admin"}})
	req := urlTenantRequest(ctx, namedTenant)
	w := httptest.NewRecorder()

	_, _, ok := s.urlTenant(w, req, "rest.IAM", "tenant_id")

	require.False(t, ok)
	assert.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
	assert.Empty(t, authz.made(), "a tenant caller's cross-tenant request must not reach the authorizer")
}
