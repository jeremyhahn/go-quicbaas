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

package apimodule

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/role"
	"github.com/jeremyhahn/go-qrdb/pkg/user"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// TestModuleAuthFallback_EndToEnd is the guard test for the P1.3
// ModuleAuthFallback adoption: it wires go-qrdb's REAL AuthMiddleware +
// RBACAuthorizer + RESTRouteRegistry against the quicbaas module's
// AuthenticateHTTP (module_http.go), exactly as go-qrdb's unified server
// wires them (pkg/api/rest/server.go's routeFn wrapper +
// RESTRouteRegistry.SetFallback), and proves the full contract in one
// place:
//
//   - a USER token (subject resolves to a qrdb user record) is authorized
//     from store-resolved roles via qrdb's own finalizeIdentity enrichment;
//   - an APP token (client-credential grant, no qrdb user record) is
//     authorized purely from its ScopePermissions (the scp-derived
//     resource:action set AuthenticateHTTP projects) — exactly what the
//     token was issued for;
//   - an APP token whose scp does NOT cover the route's permission is
//     denied 403;
//   - a garbage/unrecognized bearer token is denied 401.
//
// No fake stands in for qrdb's authorization decision: rbac.New and
// middleware.New are the real production types from go-qrdb@v0.5.6-alpha,
// driven by minimal role/user service doubles (only ResolvePermissions /
// GetUserByUsername are ever exercised on this path).
func TestModuleAuthFallback_EndToEnd(t *testing.T) {
	const (
		routeMethod  = "GET"
		routePattern = "/api/v1/kms/keys"
		routePerm    = "keys:get"
	)

	roles := &fakeRoleServicer{perms: map[string][]permission.Permission{
		"viewer": {"keys:get"},
	}}
	users := &fakeUserServicer{users: map[string]*user.User{
		"tenant-a/alice": {Username: "alice", TenantID: "tenant-a", RoleIDs: []string{"viewer"}, Enabled: true},
	}}

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	restSrv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restapi.DefaultConfig(), log)
	if err != nil {
		t.Fatal(err)
	}
	restSrv.SetModuleTokenAuthorizer(&fakeTokenAuthorizer{tokens: map[string]fakeToken{
		"user-token":      {subject: "alice", tenant: "tenant-a"}, // no scp; resolves via the user store
		"app-token-right": {subject: "svc-app", tenant: "tenant-a", scopes: []string{"keys:get"}},
		"app-token-wrong": {subject: "svc-app", tenant: "tenant-a", scopes: []string{"keys:list"}},
	}})

	m, err := NewModule(
		func() service.QuicBaaS { return nil }, // never invoked: this test never dispatches a route body
		log,
		WithRESTServer(func() *restapi.Server { return restSrv }),
	)
	if err != nil {
		t.Fatal(err)
	}

	// Wire go-qrdb's REAL authorization pipeline exactly as
	// pkg/api/rest/server.go does for a ModuleAuthFallback-implementing
	// module: authenticate (the sole authenticator always fails here,
	// modeling a caller with no mTLS client certificate, so every request
	// forces the fallback path), authorize via RBAC, with the module's
	// AuthenticateHTTP attached as the route's fallback.
	authorizer := rbac.New(roles)
	am := middleware.New(authorizer,
		middleware.WithAuthenticator(neverAuthenticates{}),
		middleware.WithUserService(users),
	)
	routes := middleware.NewRESTRouteRegistry()
	routes.Register(routeMethod, routePattern, permission.Permission(routePerm))
	routes.SetFallback(routeMethod, routePattern, m)

	handler := am.RESTMiddleware(routes, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"user token: store-resolved role admits", "user-token", http.StatusOK},
		{"app token: right scope admits (scp-only, no qrdb user)", "app-token-right", http.StatusOK},
		{"app token: wrong scope denies", "app-token-wrong", http.StatusForbidden},
		{"garbage token denies", "not-a-real-token", http.StatusUnauthorized},
		{"no token denies", "", http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(routeMethod, routePattern, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}

// fakeToken is a token registry entry for fakeTokenAuthorizer.
type fakeToken struct {
	subject string
	tenant  string
	scopes  []string
}

// fakeTokenAuthorizer is a restapi.ModuleTokenAuthorizer test double that
// authenticates a known set of tokens and projects each one's configured
// Subject/TenantID/Scopes, exactly as the real platformtoken.Validator's
// Validate does after offline signature/DPoP/liveness verification.
type fakeTokenAuthorizer struct {
	tokens map[string]fakeToken
}

func (f *fakeTokenAuthorizer) Validate(_ context.Context, token, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	tok, ok := f.tokens[token]
	if !ok {
		return nil, &platformtoken.InvalidTokenError{Detail: "signature verification failed"}
	}
	return &platformtoken.AuthorizedContext{Subject: tok.subject, Username: tok.subject, TenantID: tok.tenant, Scopes: tok.scopes}, nil
}

func (f *fakeTokenAuthorizer) Authorize(ctx context.Context, token, requiredPerm, dpop, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	ac, err := f.Validate(ctx, token, dpop, htm, htu)
	if err != nil {
		return nil, err
	}
	if err := ac.RequireScope(requiredPerm); err != nil {
		return nil, err
	}
	return ac, nil
}

// fakeRoleServicer implements role.RoleServicer via a nil embed, overriding
// only ResolvePermissions — the single method the RBAC authorizer's
// permission-resolution path calls.
type fakeRoleServicer struct {
	role.RoleServicer
	perms map[string][]permission.Permission
}

func (f *fakeRoleServicer) ResolvePermissions(_ context.Context, roleIDs []string) ([]permission.Permission, error) {
	var out []permission.Permission
	for _, id := range roleIDs {
		out = append(out, f.perms[id]...)
	}
	return out, nil
}

// fakeUserServicer implements user.UserServicer via a nil embed, overriding
// only GetUserByUsername — the single method finalizeIdentity's
// store-resolved-role enrichment calls.
type fakeUserServicer struct {
	user.UserServicer
	users map[string]*user.User
}

func (f *fakeUserServicer) GetUserByUsername(_ context.Context, tenantID, username string) (*user.User, error) {
	u, ok := f.users[tenantID+"/"+username]
	if !ok {
		return nil, &user.UserNotFoundError{Lookup: tenantID + "/" + username}
	}
	return u, nil
}

// neverAuthenticates is a qrdbauth.Authenticator that always fails, forcing
// every request in this test through the module fallback path — the same
// posture a real deployment has for a caller presenting no mTLS client
// certificate.
type neverAuthenticates struct{}

var _ qrdbauth.Authenticator = neverAuthenticates{}

func (neverAuthenticates) Authenticate(context.Context) (*qrdbauth.Identity, error) {
	return nil, qrdbauth.ErrUnauthenticated
}

func (neverAuthenticates) Name() string { return "never" }
