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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// liftTenant is the tenant the system admin names in the URL.
const liftTenant = "acme"

// systemTenantID returns the tenant a system caller is bound to.
func systemTenantID(t testing.TB) string {
	t.Helper()
	tenantID, err := tenantctx.FromContext(tenantctx.WithSystem(context.Background()), "rest.IAM")
	require.NoError(t, err)
	return tenantID
}

// systemAdminIdentity is the authenticated identity go-qrdb's middleware
// places in the request context for the system admin.
func systemAdminIdentity(t testing.TB) *qrdbsdk.Identity {
	t.Helper()
	return &qrdbsdk.Identity{Subject: "admin", TenantID: systemTenantID(t), Roles: []string{"admin"}}
}

// systemAdminLiftRequest returns r as the unified server hands a system
// admin's request to a handler: with the authenticated identity go-qrdb's
// middleware placed in the context, and the permission BindRoute carries for
// the route. A test whose system caller names another tenant also wires an
// authorizer on its server (SetBarrierScopeAuthorizer).
func systemAdminLiftRequest(t testing.TB, r *http.Request, resource, action string) *http.Request {
	t.Helper()
	ctx := qrdbsdk.WithIdentity(r.Context(), systemAdminIdentity(t))
	return r.WithContext(withRoutePermission(ctx, resource, action))
}

// urlTenantRequest is a request for a tenant-scoped IAM route that names
// tenantID in its URL, made by the caller ctx carries.
func urlTenantRequest(ctx context.Context, tenantID string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenantID)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/"+tenantID+"/roles", nil)
	return req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
}

// identityRecordingAuthorizer allows every decision and records the identity
// it was asked about.
type identityRecordingAuthorizer struct {
	identities []*qrdbsdk.Identity
}

func (a *identityRecordingAuthorizer) Authorize(_ context.Context, identity *qrdbsdk.Identity, _, _ string) error {
	a.identities = append(a.identities, identity)
	return nil
}

// mountAppDeleteRoute mounts the PRODUCTION OAuth application delete route
// through BindRoute, as the unified server does, over an application manager
// that holds one application registered in liftTenant. It returns the router,
// the server, the route's spec and a probe reporting whether the application
// is still stored.
func mountAppDeleteRoute(t *testing.T) (http.Handler, *Server, RouteSpec, string, func() bool) {
	t.Helper()

	server, mockService, _, _, cleanup := setupIAMTestServer(t)
	t.Cleanup(cleanup)

	manager := iam.NewAppManager(iam.NewBackendStorage(newIntrospectMemKV()), nil)
	server.service = &appMockService{MockQuicBaaS: mockService.MockQuicBaaS, manager: manager}

	tenantCtx := tenantctx.WithTenant(context.Background(), liftTenant)
	app, err := manager.RegisterApp(tenantCtx, liftTenant, iam.RegisterAppOptions{
		Name:         "acme-console",
		RedirectURIs: []string{"https://acme.example/callback"},
	})
	require.NoError(t, err)

	const pattern = "/api/v1/tenants/{tenant_id}/apps/{client_id}"
	r := chi.NewRouter()
	var mounted RouteSpec
	for _, spec := range Routes() {
		if spec.Method != http.MethodDelete || spec.Pattern != pattern {
			continue
		}
		r.Method(spec.Method, spec.Pattern, server.BindRoute(spec))
		mounted = spec
	}
	require.Equal(t, pattern, mounted.Pattern, "expected the application delete route in the production table")
	require.NotEmpty(t, mounted.Resource, "the application delete route carries no permission")
	require.NotEmpty(t, mounted.Action, "the application delete route carries no permission")

	stored := func() bool {
		_, err := manager.GetAppInTenant(tenantCtx, liftTenant, app.ClientID)
		return err == nil
	}
	require.True(t, stored(), "the application was not stored")
	return r, server, mounted, app.ClientID, stored
}

// TestTenantLift_AppDeleteIsAuthorizedInTheNamedTenant drives the review's
// case end to end at the HTTP level: a system admin deletes an OAuth
// application of another tenant. go-qrdb authorized the route against the
// caller's own tenant; the URL's tenant decides once more before the handler
// reaches storage, so its deny policies apply (go-quicbaas#2212).
func TestTenantLift_AppDeleteIsAuthorizedInTheNamedTenant(t *testing.T) {
	const denyText = "denied by policy acme-no-app-delete"

	for _, tc := range []struct {
		name string
		// authz is the wired authorizer; nil leaves none wired.
		authz         *scopeAuthorizerStub
		wantStatus    int
		wantStored    bool
		wantDecisions int
	}{
		{
			name:          "the named tenant denies",
			authz:         &scopeAuthorizerStub{err: errors.New(denyText)},
			wantStatus:    http.StatusForbidden,
			wantStored:    true,
			wantDecisions: 1,
		},
		{
			name:          "the named tenant allows",
			authz:         &scopeAuthorizerStub{},
			wantStatus:    http.StatusNoContent,
			wantStored:    false,
			wantDecisions: 1,
		},
		{
			name:       "no authorizer is wired",
			wantStatus: http.StatusServiceUnavailable,
			wantStored: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router, server, spec, clientID, stored := mountAppDeleteRoute(t)
			if tc.authz != nil {
				server.SetBarrierScopeAuthorizer(tc.authz)
			}

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/"+liftTenant+"/apps/"+clientID, nil)
			req = req.WithContext(qrdbsdk.WithIdentity(tenantctx.WithSystem(req.Context()), systemAdminIdentity(t)))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, tc.wantStatus, rec.Code, "body: %s", rec.Body.String())
			assert.Equal(t, tc.wantStored, stored(), "the stored application")
			assert.NotContains(t, rec.Body.String(), denyText,
				"the authorizer's error text must not reach the caller")
			if tc.authz == nil {
				return
			}
			decisions := tc.authz.made()
			require.Len(t, decisions, tc.wantDecisions)
			assert.Equal(t, scopeDecision{tenant: liftTenant, resource: spec.Resource, action: spec.Action}, decisions[0],
				"the route's own permission, with the URL's tenant as the target")
		})
	}
}

// TestURLTenant_SystemCallerIsAuthorizedInTheNamedTenant proves the second
// decision is rendered for the request's own identity and the route's own
// permission, with only the target tenant changed.
func TestURLTenant_SystemCallerIsAuthorizedInTheNamedTenant(t *testing.T) {
	identity := systemAdminIdentity(t)
	req := urlTenantRequest(tenantctx.WithSystem(context.Background()), liftTenant)
	req = req.WithContext(withRoutePermission(qrdbsdk.WithIdentity(req.Context(), identity), "tenants", "read"))

	t.Run("the decision names the URL's tenant and the route's permission", func(t *testing.T) {
		var s Server
		authz := &scopeAuthorizerStub{}
		s.SetBarrierScopeAuthorizer(authz)
		r := req.Clone(req.Context())
		w := httptest.NewRecorder()

		ctx, tenantID, ok := s.urlTenant(w, r, "rest.IAM", "tenant_id")

		require.True(t, ok, "body: %s", w.Body.String())
		assert.Equal(t, liftTenant, tenantID)
		resolved, err := tenantctx.FromContext(ctx, "rest.IAM")
		require.NoError(t, err)
		assert.Equal(t, liftTenant, resolved)
		assert.Equal(t, []scopeDecision{{tenant: liftTenant, resource: "tenants", action: "read"}}, authz.made())
	})

	t.Run("the decision is for the request's own identity", func(t *testing.T) {
		var s Server
		authz := &identityRecordingAuthorizer{}
		s.SetBarrierScopeAuthorizer(authz)
		r := req.Clone(req.Context())

		_, _, ok := s.urlTenant(httptest.NewRecorder(), r, "rest.IAM", "tenant_id")

		require.True(t, ok)
		require.Len(t, authz.identities, 1)
		assert.Same(t, identity, authz.identities[0])
		assert.Equal(t, systemTenantID(t), authz.identities[0].TenantID, "the identity was rebound")
	})
}

// TestURLTenant_CrossTenantLiftFailsClosed covers everything that cannot be
// decided: each refuses the request, and urlTenant leaves the request bound
// to its unresolved caller.
func TestURLTenant_CrossTenantLiftFailsClosed(t *testing.T) {
	const denyText = "denied by policy acme-no-role-read"

	for _, tc := range []struct {
		name string
		// authz is the wired authorizer; nil leaves none wired.
		authz         *scopeAuthorizerStub
		noIdentity    bool
		noPermission  bool
		wantStatus    int
		wantDecisions int
	}{
		{
			name:         "no route permission in the context",
			authz:        &scopeAuthorizerStub{},
			noPermission: true,
			wantStatus:   http.StatusForbidden,
		},
		{
			name:       "no authorizer wired",
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "no authenticated identity",
			authz:      &scopeAuthorizerStub{},
			noIdentity: true,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "the named tenant denies",
			authz:         &scopeAuthorizerStub{err: errors.New(denyText)},
			wantStatus:    http.StatusForbidden,
			wantDecisions: 1,
		},
		{
			name:          "the authorizer is unavailable",
			authz:         &scopeAuthorizerStub{err: &BarrierScopeAuthorizerUnavailableError{Reason: "not ready"}},
			wantStatus:    http.StatusServiceUnavailable,
			wantDecisions: 1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s Server
			if tc.authz != nil {
				s.SetBarrierScopeAuthorizer(tc.authz)
			}
			ctx := tenantctx.WithSystem(context.Background())
			if !tc.noIdentity {
				ctx = qrdbsdk.WithIdentity(ctx, systemAdminIdentity(t))
			}
			if !tc.noPermission {
				ctx = withRoutePermission(ctx, "tenants", "read")
			}
			req := urlTenantRequest(ctx, liftTenant)
			w := httptest.NewRecorder()

			resolved, tenantID, ok := s.urlTenant(w, req, "rest.IAM", "tenant_id")

			require.False(t, ok)
			assert.Nil(t, resolved)
			assert.Empty(t, tenantID)
			assert.Equal(t, tc.wantStatus, w.Code, "body: %s", w.Body.String())
			assert.NotContains(t, w.Body.String(), denyText,
				"the authorizer's error text must not reach the caller")
			bound, err := tenantctx.FromContext(req.Context(), "rest.IAM")
			require.NoError(t, err)
			assert.Equal(t, systemTenantID(t), bound, "a refused request must keep its unresolved caller")
			if tc.authz != nil {
				assert.Len(t, tc.authz.made(), tc.wantDecisions)
			}
		})
	}
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
			caller: tenantctx.WithTenant(context.Background(), liftTenant),
			named:  liftTenant,
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

// TestURLTenant_TenantCallerNamingAnotherTenantIsStillRefused proves the
// second decision widens nothing: a tenant caller is refused by
// tenantctx.AdminTarget before any authorizer is asked, whatever it allows.
func TestURLTenant_TenantCallerNamingAnotherTenantIsStillRefused(t *testing.T) {
	var s Server
	authz := &scopeAuthorizerStub{}
	s.SetBarrierScopeAuthorizer(authz)
	ctx := tenantctx.WithTenant(context.Background(), "globex")
	ctx = qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{Subject: "operator", TenantID: "globex", Roles: []string{"admin"}})
	req := urlTenantRequest(withRoutePermission(ctx, "tenants", "read"), liftTenant)
	w := httptest.NewRecorder()

	_, _, ok := s.urlTenant(w, req, "rest.IAM", "tenant_id")

	require.False(t, ok)
	assert.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
	assert.Empty(t, authz.made(), "a tenant caller's cross-tenant request must not reach the authorizer")
}
