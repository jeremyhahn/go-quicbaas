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
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	qrdbauth "github.com/jeremyhahn/go-qrdb/pkg/auth"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/user"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// go-quicbaas#256 — the daemon must accept the bearer it just minted.
//
// The regression this file pins was invisible to every existing test because
// no test crossed mint -> validate at the layer that broke. The handler
// (IAMHandler.ValidateToken) was never wrong; the ROUTE was absent. P3.3
// dropped the token-lifecycle verbs from the module surface on the reasoning
// that a composed node's xkms module owns /api/v1/auth/*, and #221 relocated
// only refresh. xkms registers GET /api/v1/auth/validate, so the SPA's POST
// matched no registered method+pattern at all — and go-qrdb's auth middleware
// wraps the WHOLE router (pkg/api/rest/server.go), running before chi routes.
// With no registration there is no module auth fallback to consult, so mTLS
// authentication ran against a browser that has no client certificate and the
// operator's freshly minted bearer came back
//
//	401 {"error":"qrdb/auth: no_peer_cert: no client certificate in TLS connection"}
//
// The test therefore drives go-qrdb's REAL middleware over the registry built
// from the module's OWN RegisterHTTP output: what a composed node registers is
// exactly what is exercised here. A route that stops being registered fails
// this test the way it failed the operator.
// =============================================================================

// noPeerCertificate is the mTLS authenticator's verdict for a caller that
// presented no client certificate — a browser. It replaces neverAuthenticates
// (module_auth_fallback_e2e_test.go) so the unregistered-route case reproduces
// the exact error body captured in the browser on the burn-in node, not a
// generic "unauthenticated".
type noPeerCertificate struct{}

var _ qrdbauth.Authenticator = noPeerCertificate{}

func (noPeerCertificate) Authenticate(context.Context) (*qrdbauth.Identity, error) {
	return nil, qrdbauth.ErrNoPeerCertificate
}

func (noPeerCertificate) Name() string { return "no-peer-cert" }

// registryFromModule registers the module's real HTTP surface into a real
// go-qrdb RESTRouteRegistry using the same rules the unified server applies
// (pkg/api/rest/server.go NewServer): public routes short-circuit
// authentication, and every non-public route gets the module's own
// AuthenticateHTTP attached as its authentication fallback.
func registryFromModule(m *Module) *middleware.RESTRouteRegistry {
	routes := middleware.NewRESTRouteRegistry()
	reg := qrdbsdk.NewModuleHTTPRegistrar(chi.NewRouter(),
		func(method, pattern string, perm qrdbsdk.Permission, _ http.HandlerFunc) {
			routes.Register(method, pattern, permission.Permission(perm))
			routes.SetFallback(method, pattern, m)
		},
		func(method, pattern string, _ http.HandlerFunc) {
			routes.RegisterPublic(method, pattern)
		})
	m.RegisterHTTP(reg)
	return routes
}

// TestModuleSessionVerbs_MintedBearerAccepted is the #256 guard. It proves
// the four token-lifecycle verbs the SPA calls with a session bearer are
// reachable on a composed node with that bearer and no client certificate,
// and — the asymmetric half — that the middleware still rejects a bearer this
// daemon did not mint and still refuses the abandoned /api/v1/auth/validate
// path. A test that only asserted 200s would pass against a router that
// authenticated nothing.
func TestModuleSessionVerbs_MintedBearerAccepted(t *testing.T) {
	const mintedBearer = "bearer-this-daemon-minted"

	roles := &fakeRoleServicer{perms: map[string][]permission.Permission{
		"viewer": {"keys:get"},
	}}
	users := &fakeUserServicer{users: map[string]*user.User{
		"tenant-a/alice": {Username: "alice", TenantID: "tenant-a", RoleIDs: []string{"viewer"}, Enabled: true},
	}}

	log := newTestLogger(t)
	restSrv := newModuleRESTServer(t)
	// The login ceremony's product: a bearer whose subject resolves to a real
	// user record, exactly what LoginService hands the SPA at login/finish.
	restSrv.SetModuleTokenAuthorizer(&fakeTokenAuthorizer{tokens: map[string]fakeToken{
		mintedBearer: {subject: "alice", tenant: "tenant-a"},
	}})

	m, err := NewModule(
		func() service.QuicBaaS { return nil },
		log,
		WithRESTServer(func() *restapi.Server { return restSrv }),
	)
	if err != nil {
		t.Fatal(err)
	}

	routes := registryFromModule(m)
	am := middleware.New(rbac.New(roles),
		middleware.WithAuthenticator(noPeerCertificate{}),
		middleware.WithUserService(users),
	)
	handler := am.RESTMiddleware(routes, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name        string
		method      string
		path        string
		bearer      string
		want        int
		wantInBody  string
		description string
	}{
		{
			name: "validate accepts the bearer this daemon minted", method: "POST",
			path: "/api/v1/iam/validate", bearer: mintedBearer, want: http.StatusOK,
			description: "the #256 regression: this answered 401 no_peer_cert",
		},
		{
			name: "logout accepts the bearer this daemon minted", method: "POST",
			path: "/api/v1/iam/logout", bearer: mintedBearer, want: http.StatusOK,
			description: "revoking a quicbaas session must reach quicbaas's own store",
		},
		{
			name: "refresh stays public (no bearer exists at renewal time)", method: "POST",
			path: "/api/v1/iam/refresh", bearer: "", want: http.StatusOK,
			description: "go-quicbaas#221; the opaque credential is in the body",
		},
		{
			name: "validate rejects a bearer this daemon did not mint", method: "POST",
			path: "/api/v1/iam/validate", bearer: "forged", want: http.StatusUnauthorized,
			description: "asymmetry: the route is gated, not merely reachable",
		},
		{
			name: "validate rejects a request carrying no bearer", method: "POST",
			path: "/api/v1/iam/validate", bearer: "", want: http.StatusUnauthorized,
			description: "asymmetry: no credential is not an anonymous pass",
		},
		{
			name: "logout rejects a bearer this daemon did not mint", method: "POST",
			path: "/api/v1/iam/logout", bearer: "forged", want: http.StatusUnauthorized,
			description: "asymmetry: revocation is gated too",
		},
		{
			name: "the abandoned /api/v1/auth/validate path is still unreachable", method: "POST",
			path: "/api/v1/auth/validate", bearer: mintedBearer,
			want: http.StatusUnauthorized, wantInBody: "no_peer_cert",
			description: "the exact 401 the operator saw; the SDK must not go back to this path",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.bearer != "" {
				req.Header.Set("Authorization", "Bearer "+tc.bearer)
			}
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Errorf("%s %s: status = %d, want %d (%s; body: %s)",
					tc.method, tc.path, w.Code, tc.want, tc.description, w.Body.String())
			}
			if tc.wantInBody != "" && !strings.Contains(w.Body.String(), tc.wantInBody) {
				t.Errorf("%s %s: body = %s, want it to contain %q",
					tc.method, tc.path, w.Body.String(), tc.wantInBody)
			}
		})
	}
}
