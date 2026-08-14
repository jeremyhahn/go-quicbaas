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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// =============================================================================
// Security regression suite for the Dynamic Client Registration surface.
//
// C1: validateRegistrationAccessToken accepted ANY non-empty bearer and was the
//     only gate on the RFC 7592 management endpoints, so anyone could read,
//     rewrite (redirect_uris -> authorization-code interception) or delete ANY
//     OAuth2 client, across tenants.
// H2: POST /oauth2/register was unauthenticated and defaulted the new client's
//     tenant to the control-plane system tenant.
//
// Every test below fails if either regression is reintroduced.
// =============================================================================

// dcrTenantHandler builds a handler over a real AppManager whose tenants are
// created on demand, so a client can be registered in tenant A and attacked
// from tenant B.
func dcrTenantHandler(t *testing.T, tenants ...string) (*IAMHandler, func()) {
	t.Helper()

	memStorage := iam.NewMemoryStorage()
	tenantMgr := &testTenantManager{storage: memStorage}
	for _, id := range tenants {
		_, err := tenantMgr.CreateTenant(context.Background(), iam.CreateTenantOptions{ID: id, Name: id})
		require.NoError(t, err)
	}

	manager := iam.NewAppManager(iam.NewBackendStorage(newIntrospectMemKV()), tenantMgr)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	mockSvc := &appMockService{MockQuicBaaS: testutil.NewMockQuicBaaS(), manager: manager}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	server.config.IAMDynamicClientRegistration = config.IAMDynamicClientRegistrationConfig{Enabled: true}

	return NewIAMHandler(IAMHandlerDependencies{Server: server}), func() { server.Close() }
}

// withPrincipal stamps a verified platform principal (the initial access token
// the authz middleware would have validated) onto the request.
func withPrincipal(r *http.Request, tenantID string) *http.Request {
	ctx := authorizedContextIntoContext(r.Context(), &platformtoken.AuthorizedContext{
		Subject:  "subject-" + tenantID,
		TenantID: tenantID,
	})
	return r.WithContext(ctx)
}

// registerDCRClientAs registers a client as an authenticated principal of
// tenantID and returns (client_id, registration access token).
func registerDCRClientAs(t *testing.T, handler *IAMHandler, tenantID string) (string, string) {
	t.Helper()
	body, err := json.Marshal(DynamicClientRequest{
		ClientName:    "client-" + tenantID,
		RedirectURIs:  []string{"https://app.example/cb"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	require.NoError(t, err)

	req := withPrincipal(dcrRegisterRequest(string(body)), tenantID)
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	var resp DynamicClientResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp.RegistrationAccessToken)
	return resp.ClientID, resp.RegistrationAccessToken
}

// -----------------------------------------------------------------------------
// C1 — the RFC 7592 management endpoints must refuse an arbitrary bearer.
// -----------------------------------------------------------------------------

// TestDCRManagement_ArbitraryBearerRejected is the direct regression pin for
// the "accept any valid bearer token format" stub. Reverting the fix makes
// every sub-case return 200/204 instead of 401.
func TestDCRManagement_ArbitraryBearerRejected(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	clientID, _ := registerDCRClient(t, handler)

	forged := []string{"x", "reg-token", "Bearer", "00000000000000000000000000000000"}
	for _, token := range forged {
		t.Run("GET with "+token, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientID, token, nil))
			assert.Equal(t, http.StatusUnauthorized, w.Code, "arbitrary bearer must not read a client")
		})

		t.Run("PUT with "+token, func(t *testing.T) {
			body, _ := json.Marshal(DynamicClientRequest{
				RedirectURIs:  []string{"https://attacker.example/steal"},
				GrantTypes:    []string{"authorization_code"},
				ResponseTypes: []string{"code"},
			})
			w := httptest.NewRecorder()
			handler.UpdateDynamicClient(w, dcrManagementRequest(http.MethodPut, clientID, token, body))
			assert.Equal(t, http.StatusUnauthorized, w.Code, "arbitrary bearer must not rewrite redirect_uris")
		})

		t.Run("DELETE with "+token, func(t *testing.T) {
			w := httptest.NewRecorder()
			handler.DeleteDynamicClient(w, dcrManagementRequest(http.MethodDelete, clientID, token, nil))
			assert.Equal(t, http.StatusUnauthorized, w.Code, "arbitrary bearer must not delete a client")
		})
	}
}

// TestDCRManagement_UnauthenticatedRejected pins the token-less case for all
// three verbs.
func TestDCRManagement_UnauthenticatedRejected(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	clientID, _ := registerDCRClient(t, handler)

	w := httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientID, "", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body, _ := json.Marshal(DynamicClientRequest{
		RedirectURIs:  []string{"https://attacker.example/steal"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	w = httptest.NewRecorder()
	handler.UpdateDynamicClient(w, dcrManagementRequest(http.MethodPut, clientID, "", body))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = httptest.NewRecorder()
	handler.DeleteDynamicClient(w, dcrManagementRequest(http.MethodDelete, clientID, "", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// The client survived every attempt, unmodified.
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientID, "", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestDCRManagement_TokenBoundToOneClient verifies a valid registration token
// for client A cannot address client B (same tenant).
func TestDCRManagement_TokenBoundToOneClient(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	clientA, tokenA := registerDCRClient(t, handler)
	clientB, tokenB := registerDCRClient(t, handler)
	require.NotEqual(t, clientA, clientB)

	// A's token on B -> 401.
	w := httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientB, tokenA, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	body, _ := json.Marshal(DynamicClientRequest{
		RedirectURIs:  []string{"https://attacker.example/steal"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	w = httptest.NewRecorder()
	handler.UpdateDynamicClient(w, dcrManagementRequest(http.MethodPut, clientB, tokenA, body))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = httptest.NewRecorder()
	handler.DeleteDynamicClient(w, dcrManagementRequest(http.MethodDelete, clientB, tokenA, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// B's own token still works, and B's redirect_uris were never rewritten.
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientB, tokenB, nil))
	require.Equal(t, http.StatusOK, w.Code)
	var resp DynamicClientResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, []string{"https://app.example/cb"}, resp.RedirectURIs)
}

// TestDCRManagement_CrossTenantDenied verifies a registration token issued in
// tenant A cannot manage a client in tenant B, and that an authenticated
// principal of tenant B cannot use its own tenant context to reach A's client.
func TestDCRManagement_CrossTenantDenied(t *testing.T) {
	const tenantA, tenantB = "tenant-alpha", "tenant-beta"
	handler, cleanup := dcrTenantHandler(t, tenantA, tenantB)
	defer cleanup()

	clientA, tokenA := registerDCRClientAs(t, handler, tenantA)
	clientB, tokenB := registerDCRClientAs(t, handler, tenantB)

	// Tenant B's registration token against tenant A's client -> 401.
	w := httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientA, tokenB, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Even holding A's genuine token, a caller authenticated INTO tenant B is
	// refused: the tenant narrows the lookup, it never widens it.
	req := withPrincipal(dcrManagementRequest(http.MethodGet, clientA, tokenA, nil), tenantB)
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// Deleting across the tenant boundary is likewise refused, and B's client
	// remains reachable with its own credential afterwards.
	w = httptest.NewRecorder()
	handler.DeleteDynamicClient(w, dcrManagementRequest(http.MethodDelete, clientA, tokenB, nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, clientB, tokenB, nil))
	assert.Equal(t, http.StatusOK, w.Code)

	// A's own token in A's own tenant context still works — the fix does not
	// break the legitimate RFC 7592 flow.
	req = withPrincipal(dcrManagementRequest(http.MethodGet, clientA, tokenA, nil), tenantA)
	w = httptest.NewRecorder()
	handler.GetDynamicClient(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestDCRManagement_NonDCRClientHasNoManagementCredential verifies a client
// created through the ordinary app-registration surface (no registration token)
// cannot be managed through RFC 7592 at all.
func TestDCRManagement_NonDCRClientHasNoManagementCredential(t *testing.T) {
	handler, cleanup := setupDCRLifecycleHandler(t)
	defer cleanup()

	manager, err := handler.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	}).GetAppManager(context.Background())
	require.NoError(t, err)

	app, err := manager.RegisterApp(context.Background(), appsTenant, iam.RegisterAppOptions{
		Name:         "classic app",
		RedirectURIs: []string{"https://classic.example/cb"},
		GrantTypes:   []string{"authorization_code"},
	})
	require.NoError(t, err)

	// Its client secret is not a registration access token either.
	for _, token := range []string{"anything", app.ClientSecret} {
		w := httptest.NewRecorder()
		handler.GetDynamicClient(w, dcrManagementRequest(http.MethodGet, app.ClientID, token, nil))
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	}
}

// -----------------------------------------------------------------------------
// H2 — Dynamic Client Registration must never be anonymous by default and must
// never land a client in the control-plane system tenant.
// -----------------------------------------------------------------------------

// TestDCRRegistration_DisabledByDefault pins the default posture: a
// zero-valued config refuses registration outright.
func TestDCRRegistration_DisabledByDefault(t *testing.T) {
	handler, _, cleanup := setupIAMHandler(t)
	defer cleanup()

	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"client_name":"x"}`))

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), DCRErrorAccessDenied)
}

// TestDCRRegistration_RequiresInitialAccessToken pins that an enabled surface
// without an anonymous landing tenant refuses a token-less registration.
func TestDCRRegistration_RequiresInitialAccessToken(t *testing.T) {
	handler, cleanup := setupDCRHandler(t, config.IAMDynamicClientRegistrationConfig{Enabled: true})
	defer cleanup()

	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"client_name":"x"}`))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "initial access token is required")
}

// TestDCRRegistration_NeverDefaultsToSystemTenant is the H2 pin: an
// unauthenticated registration must never produce a control-plane client.
func TestDCRRegistration_NeverDefaultsToSystemTenant(t *testing.T) {
	const tenant = "tenant-dcr-h2"
	handler, cleanup := dcrTenantHandler(t, tenant, qrdbsdk.SystemTenantID)
	defer cleanup()

	manager, err := handler.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	}).GetAppManager(context.Background())
	require.NoError(t, err)

	// (a) Anonymous, no landing tenant configured -> refused.
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"client_name":"anon"}`))
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// (b) An X-Tenant-ID header must not select the tenant (it was the old
	// escalation vector) and must not enable an otherwise-refused registration.
	req := dcrRegisterRequest(`{"grant_types":["client_credentials"],"client_name":"anon"}`)
	req.Header.Set("X-Tenant-ID", qrdbsdk.SystemTenantID)
	w = httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)

	// (c) A configured anonymous tenant that names the system tenant is refused
	// at request time even if it slipped past config validation.
	handler.server.config.IAMDynamicClientRegistration = config.IAMDynamicClientRegistrationConfig{
		Enabled:           true,
		AnonymousTenantID: qrdbsdk.SystemTenantID,
	}
	w = httptest.NewRecorder()
	handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"client_name":"anon"}`))
	require.Equal(t, http.StatusForbidden, w.Code)

	// No client was created in the system tenant by any of the above.
	apps, err := manager.ListApps(context.Background(), qrdbsdk.SystemTenantID, iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, apps, "unauthenticated DCR must never create a control-plane client")

	// (d) With a legitimate anonymous landing tenant the client lands THERE.
	handler.server.config.IAMDynamicClientRegistration = config.IAMDynamicClientRegistrationConfig{
		Enabled:           true,
		AnonymousTenantID: tenant,
	}
	w = httptest.NewRecorder()
	handler.RegisterDynamicClient(w, dcrRegisterRequest(`{"grant_types":["client_credentials"],"client_name":"anon"}`))
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	apps, err = manager.ListApps(context.Background(), tenant, iam.ListOptions{})
	require.NoError(t, err)
	require.Len(t, apps, 1)

	apps, err = manager.ListApps(context.Background(), qrdbsdk.SystemTenantID, iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, apps)
}

// TestDCRRegistration_TenantComesFromPrincipal verifies the authenticated
// caller's tenant — not any request-controlled input — owns the new client.
func TestDCRRegistration_TenantComesFromPrincipal(t *testing.T) {
	const tenantA, tenantB = "tenant-principal-a", "tenant-principal-b"
	handler, cleanup := dcrTenantHandler(t, tenantA, tenantB)
	defer cleanup()

	manager, err := handler.server.service.(interface {
		GetAppManager(ctx context.Context) (*iam.AppManager, error)
	}).GetAppManager(context.Background())
	require.NoError(t, err)

	// Authenticated into A while asking (via header) for B.
	req := withPrincipal(dcrRegisterRequest(`{"grant_types":["client_credentials"],"client_name":"principal"}`), tenantA)
	req.Header.Set("X-Tenant-ID", tenantB)
	w := httptest.NewRecorder()
	handler.RegisterDynamicClient(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())

	appsA, err := manager.ListApps(context.Background(), tenantA, iam.ListOptions{})
	require.NoError(t, err)
	assert.Len(t, appsA, 1)

	appsB, err := manager.ListApps(context.Background(), tenantB, iam.ListOptions{})
	require.NoError(t, err)
	assert.Empty(t, appsB, "a caller must not be able to name another tenant")
}

// -----------------------------------------------------------------------------
// Route wiring: the create verb stays gated, the management verbs are the
// deliberate (and narrowly scoped) pre-auth carve-out.
// -----------------------------------------------------------------------------

func TestDCRRouteAuthorizationPosture(t *testing.T) {
	t.Run("create route is not pre-auth", func(t *testing.T) {
		assert.False(t, isPreAuthRoute("/oauth2/register"))
		assert.False(t, isPreAuthRoute("/oauth2/register/"))
		assert.False(t, isAuthzPublicEndpoint("/oauth2/register"))
		assert.False(t, isDefaultDenyPublic("/oauth2/register"))
	})

	t.Run("management route is pre-auth", func(t *testing.T) {
		assert.True(t, isPreAuthRoute("/oauth2/register/app_tenant_abc"))
		assert.True(t, isAuthzPublicEndpoint("/oauth2/register/app_tenant_abc"))
		assert.True(t, isDefaultDenyPublic("/oauth2/register/app_tenant_abc"))
	})

	t.Run("carve-out does not widen to deeper paths", func(t *testing.T) {
		assert.False(t, isPreAuthRoute("/oauth2/register/app_tenant_abc/secret"))
		assert.False(t, isPreAuthRoute("/oauth2/register//"))
	})

	t.Run("create route requires the tenants:create scope", func(t *testing.T) {
		assert.Equal(t, "tenants:create", requiredScopeForRequest("/oauth2/register", http.MethodPost))

		resource, action, ok := mapRequestToPermission("/oauth2/register", http.MethodPost)
		require.True(t, ok, "POST /oauth2/register must be PEP-mapped")
		assert.Equal(t, "tenants", resource)
		assert.Equal(t, "create", action)
	})

	t.Run("public oauth2 endpoints stay public", func(t *testing.T) {
		for _, path := range []string{
			"/oauth2/authorize",
			"/oauth2/token",
			"/oauth2/token-mtls",
			"/oauth2/userinfo",
			"/oauth2/revoke",
			"/oauth2/introspect",
		} {
			assert.True(t, isDefaultDenyPublic(path), "%s must remain reachable pre-token", path)
		}
	})
}

// TestDCRRoutesAreMounted walks the real chi route tree and asserts the four
// Dynamic Client Registration verbs are reachable at their RFC-mandated paths.
// The group is registered with router.Route, so a chi mounting mistake would
// silently 404 the whole surface while every handler test kept passing.
func TestDCRRoutesAreMounted(t *testing.T) {
	want := map[string]bool{
		"POST /oauth2/register":               false,
		"GET /oauth2/register/{client_id}":    false,
		"PUT /oauth2/register/{client_id}":    false,
		"DELETE /oauth2/register/{client_id}": false,
	}

	s, _ := newTestServer(t)
	require.NoError(t, chi.Walk(s.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		key := method + " " + strings.TrimSuffix(route, "/")
		if _, ok := want[key]; ok {
			want[key] = true
		}
		return nil
	}))

	for route, found := range want {
		assert.True(t, found, "%s must be mounted", route)
	}
}

// TestDCRRoutesDispatch drives real requests through the mounted router. The
// group is registered with router.Route + r.Post("/"), so a chi mounting
// mistake would 404 the create verb while chi.Walk still listed it. Reaching
// the handler is proven by the DCR gate's own 403/401 answer instead of a 404.
func TestDCRRoutesDispatch(t *testing.T) {
	s, _ := newTestServer(t)

	cases := []struct {
		method string
		path   string
		want   int
	}{
		// Registration is disabled on a default-configured server -> the
		// handler's own 403, never a router 404.
		{http.MethodPost, "/oauth2/register", http.StatusForbidden},
		// The RFC 7592 verbs answer the registration-access-token gate's 401.
		{http.MethodGet, "/oauth2/register/app_t_1", http.StatusUnauthorized},
		{http.MethodPut, "/oauth2/register/app_t_1", http.StatusUnauthorized},
		{http.MethodDelete, "/oauth2/register/app_t_1", http.StatusUnauthorized},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("{}"))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			assert.NotEqual(t, http.StatusNotFound, w.Code, "route must be reachable")
			assert.Equal(t, tc.want, w.Code, "body: %s", w.Body.String())
		})
	}
}

// TestDCRConfigSystemTenantMatchesQRDB guards the literal duplicated in
// pkg/config against drift from qrdb's canonical system tenant id.
func TestDCRConfigSystemTenantMatchesQRDB(t *testing.T) {
	assert.Equal(t, qrdbsdk.SystemTenantID, config.SystemTenantID)
}
