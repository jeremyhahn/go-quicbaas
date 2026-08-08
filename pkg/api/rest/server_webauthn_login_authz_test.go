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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/sealedwindow"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// =============================================================================
// Router-level pre-auth contract for the tenant-scoped WebAuthn LOGIN ceremony.
//
// Regression guard for the production outage where the root-mounted
// /api/v1/tenants group gained useAPIAuthzMiddleware (the tenant-barrier
// middleware-layering fix) and thereby demanded a bearer token on
// POST /api/v1/tenants/{tenant}/webauthn/login/begin — the very call a browser
// makes BEFORE it holds any token. Browser sign-in became impossible.
//
// Contract (identical under BOTH enforcement postures):
//
//	login/begin  + login/finish, no token  -> admitted (reaches the handler)
//	register/begin, register/finish, no token -> 401 (still gated)
//	every other tenant route, no token     -> 401 (still gated)
//
// The tests drive the REAL router built by setupRouter: a predicate unit test
// cannot catch a routing/middleware-layering hole.
// =============================================================================

// preAuthLoginPaths are the login-ceremony routes on the legacy
// /api/v1/tenants mount — the surface the TypeScript SDK and the SPA login
// view call, and the one the production outage broke. This mount is always
// registered, so an admitted request must reach its handler.
var preAuthLoginPaths = []string{
	"/api/v1/tenants/system/webauthn/login/begin",
	"/api/v1/tenants/system/webauthn/login/finish",
}

// canonicalXKMSLoginPaths are the same ceremony on the canonical xkms-native
// mount, which the Go SDK REST transport calls. That mount is registered only
// when the xkms-backed WebAuthn manager resolves (MountXKMSWebAuthnRoutes is a
// silent no-op otherwise), so on a mock-service router these paths reach the
// router's 404. That still proves the authorization contract: the gate admitted
// the request instead of short-circuiting it with a 401.
var canonicalXKMSLoginPaths = []string{
	"/api/v1/iam/system/webauthn/login/begin",
	"/api/v1/iam/system/webauthn/login/finish",
}

// gatedTenantSiblingPaths are sibling routes under the SAME chi groups that must
// remain gated. register/{begin,finish} enroll a NEW authenticator against a
// caller-supplied user_id, so a public mount would be an account-takeover
// primitive; the remaining entries are ordinary tenant administration.
var gatedTenantSiblingPaths = []struct {
	method string
	path   string
}{
	{http.MethodPost, "/api/v1/tenants/system/webauthn/register/begin"},
	{http.MethodPost, "/api/v1/tenants/system/webauthn/register/finish"},
	{http.MethodGet, "/api/v1/tenants/system/users"},
	{http.MethodGet, "/api/v1/tenants"},
	{http.MethodGet, "/api/v1/tenants/system/barrier/status"},
}

// loginBody is a well-formed ceremony body so an admitted request travels past
// the handler's JSON parsing and a 400 can never be mistaken for a bypass.
const loginBody = `{"email":"admin@example.com","credential":{}}`

// serveWebAuthn issues method+path against the real router with an optional
// bearer and returns the recorder.
func serveWebAuthn(s *Server, method, path, bearer string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(loginBody))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	rr := httptest.NewRecorder()
	s.router.ServeHTTP(rr, req)
	return rr
}

// assertGateAdmitted asserts the authorization layer did not deny the request.
func assertGateAdmitted(t *testing.T, rr *httptest.ResponseRecorder, path string) {
	t.Helper()
	assert.NotEqual(t, http.StatusUnauthorized, rr.Code, "%s must not be 401", path)
	assert.NotEqual(t, http.StatusForbidden, rr.Code, "%s must not be 403", path)
}

// assertReachedHandler additionally asserts the request was routed to a real
// endpoint, so a 404 can never be mistaken for a successful bypass.
func assertReachedHandler(t *testing.T, rr *httptest.ResponseRecorder, path string) {
	t.Helper()
	assertGateAdmitted(t, rr, path)
	assert.NotEqual(t, http.StatusNotFound, rr.Code, "%s must be a routed endpoint", path)
	assert.NotEqual(t, http.StatusMethodNotAllowed, rr.Code, "%s must accept POST", path)
}

// --- Validator enforcement (RequireAuthz=true, the e2e daemon posture) --------

// TestRouter_WebAuthnLogin_PreAuthUnderValidatorEnforcement pins that the login
// ceremony bypasses the validator gate entirely (the authorizer is never even
// consulted) while its enrollment siblings still 401 without a token.
func TestRouter_WebAuthnLogin_PreAuthUnderValidatorEnforcement(t *testing.T) {
	for _, path := range preAuthLoginPaths {
		t.Run("public "+path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "unused"},
			}
			s, _ := newTestServer(t)
			s.SetValidatorAuthzEnforcement(authorizer)

			rr := serveWebAuthn(s, http.MethodPost, path, "")

			assertReachedHandler(t, rr, path)
			assert.False(t, authorizer.called,
				"a pre-auth login route must never reach the validator")
		})
	}

	for _, path := range canonicalXKMSLoginPaths {
		t.Run("public "+path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "unused"},
			}
			s, _ := newTestServer(t)
			s.SetValidatorAuthzEnforcement(authorizer)

			rr := serveWebAuthn(s, http.MethodPost, path, "")

			assertGateAdmitted(t, rr, path)
			assert.False(t, authorizer.called,
				"a pre-auth login route must never reach the validator")
		})
	}

	for _, sibling := range gatedTenantSiblingPaths {
		t.Run("gated "+sibling.path, func(t *testing.T) {
			authorizer := &recordingValidatorAuthorizer{
				ret: &platformtoken.AuthorizedContext{Subject: "unused"},
			}
			s, _ := newTestServer(t)
			s.SetValidatorAuthzEnforcement(authorizer)

			rr := serveWebAuthn(s, sibling.method, sibling.path, "")

			assert.Equal(t, http.StatusUnauthorized, rr.Code,
				"%s must still require a bearer token", sibling.path)
		})
	}
}

// TestRouter_WebAuthnRegister_StillAuthorizedWithToken proves the enrollment
// sibling is not merely 401-on-missing-header but genuinely flows through the
// validator gate with the tenant-surface permission when a token IS presented.
func TestRouter_WebAuthnRegister_StillAuthorizedWithToken(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{
		ret: &platformtoken.AuthorizedContext{Subject: "admin", TenantID: "system"},
	}
	s, _ := newTestServer(t)
	s.SetValidatorAuthzEnforcement(authorizer)

	rr := serveWebAuthn(s, http.MethodPost,
		"/api/v1/tenants/system/webauthn/register/begin", "tok")

	require.True(t, authorizer.called, "validator must gate the enrollment route")
	assert.Equal(t, "tenants:create", authorizer.gotPerm)
	assert.NotEqual(t, http.StatusUnauthorized, rr.Code)
}

// TestRouter_WebAuthnRegister_ScopeDeniedForbidden pins the deny half of the
// enrollment route's gate: a token without the required scope is 403, never
// silently admitted.
func TestRouter_WebAuthnRegister_ScopeDeniedForbidden(t *testing.T) {
	authorizer := &recordingValidatorAuthorizer{
		retErr: &platformtoken.ScopeDeniedError{Required: "tenants:create"},
	}
	s, _ := newTestServer(t)
	s.SetValidatorAuthzEnforcement(authorizer)

	rr := serveWebAuthn(s, http.MethodPost,
		"/api/v1/tenants/system/webauthn/register/begin", "tok")

	assert.Equal(t, http.StatusForbidden, rr.Code)
}

// --- Default-deny enforcement -------------------------------------------------

// newDefaultDenyRouterServer builds a real REST server under the default-deny
// posture with a validator that ALWAYS fails, so any route that reaches token
// validation is provably denied and only a true public bypass can be admitted.
func newDefaultDenyRouterServer(t *testing.T) *Server {
	t.Helper()
	s, _ := newTestServer(t)
	s.SetDefaultDenyEnforcement(&mockDPoPValidator{err: &InvalidTokenResultError{}})
	return s
}

// TestRouter_WebAuthnLogin_PreAuthUnderDefaultDeny pins the same pre-auth
// contract under the top-router default-deny gate, which covers the
// root-mounted groups a per-group gate would miss.
func TestRouter_WebAuthnLogin_PreAuthUnderDefaultDeny(t *testing.T) {
	for _, path := range preAuthLoginPaths {
		t.Run("public "+path, func(t *testing.T) {
			s := newDefaultDenyRouterServer(t)

			rr := serveWebAuthn(s, http.MethodPost, path, "")

			assertReachedHandler(t, rr, path)
		})
	}

	for _, path := range canonicalXKMSLoginPaths {
		t.Run("public "+path, func(t *testing.T) {
			s := newDefaultDenyRouterServer(t)

			rr := serveWebAuthn(s, http.MethodPost, path, "")

			assertGateAdmitted(t, rr, path)
		})
	}

	for _, sibling := range gatedTenantSiblingPaths {
		t.Run("gated "+sibling.path, func(t *testing.T) {
			s := newDefaultDenyRouterServer(t)

			rr := serveWebAuthn(s, sibling.method, sibling.path, "")

			assert.Equal(t, http.StatusUnauthorized, rr.Code,
				"%s must still require a bearer token", sibling.path)
		})
	}
}

// TestRouter_WebAuthnLogin_PreAuthIgnoresBadToken proves the bypass is
// path-based, not token-based: a login request carrying a garbage bearer is
// still admitted rather than 401'd by the failing validator.
func TestRouter_WebAuthnLogin_PreAuthIgnoresBadToken(t *testing.T) {
	s := newDefaultDenyRouterServer(t)

	rr := serveWebAuthn(s, http.MethodPost,
		"/api/v1/tenants/system/webauthn/login/begin", "garbage")

	assertReachedHandler(t, rr, "/api/v1/tenants/system/webauthn/login/begin")
}

// --- Sealed-window semantics --------------------------------------------------

// TestRouter_WebAuthnLogin_SealedWindowUnaffected pins that making the login
// ceremony public in the AUTHZ layer does not punch a hole in the sealed-window
// DENY gate, which runs strictly ahead of every authz gate (setupRouter). The
// ceremony is not on the canonical sealed allowlist, so while the barrier is
// sealed it must still be rejected with qrdb's 503 envelope, and it must become
// reachable again the moment the barrier unseals.
func TestRouter_WebAuthnLogin_SealedWindowUnaffected(t *testing.T) {
	probe := &restTogglableProbe{sealedState: true}
	authorizer := &recordingValidatorAuthorizer{
		ret: &platformtoken.AuthorizedContext{Subject: "unused"},
	}
	s, _ := newTestServer(t)
	s.SetValidatorAuthzEnforcement(authorizer)
	s.SetSealedWindowGate(probe, sealedwindow.RESTAllowlist())

	const path = "/api/v1/tenants/system/webauthn/login/begin"

	rr := serveWebAuthn(s, http.MethodPost, path, "")
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code,
		"sealed: the login ceremony is not on the sealed allowlist")

	probe.sealedState = false
	rr = serveWebAuthn(s, http.MethodPost, path, "")
	assertReachedHandler(t, rr, path)
}

// --- Predicate contract -------------------------------------------------------

// TestPreAuthRoutePredicates asserts both public predicates agree, and that
// matching is whole-path and segment-exact so no neighbouring route is widened.
func TestPreAuthRoutePredicates(t *testing.T) {
	cases := []struct {
		name string
		path string
		want bool
	}{
		// Legacy tenant mount.
		{"tenants login begin", "/api/v1/tenants/system/webauthn/login/begin", true},
		{"tenants login finish", "/api/v1/tenants/system/webauthn/login/finish", true},
		{"tenants login begin other tenant", "/api/v1/tenants/acme-corp/webauthn/login/begin", true},
		{"tenants login begin trailing slash", "/api/v1/tenants/system/webauthn/login/begin/", true},
		// Canonical xkms mount.
		{"iam login begin", "/api/v1/iam/system/webauthn/login/begin", true},
		{"iam login finish", "/api/v1/iam/system/webauthn/login/finish", true},

		// Enrollment ceremonies stay gated on both mounts.
		{"tenants register begin gated", "/api/v1/tenants/system/webauthn/register/begin", false},
		{"tenants register finish gated", "/api/v1/tenants/system/webauthn/register/finish", false},
		{"iam registration begin gated", "/api/v1/iam/system/webauthn/registration/begin", false},
		{"iam registration status gated", "/api/v1/iam/system/webauthn/registration/status", false},

		// No prefix widening in either direction.
		{"parent webauthn group gated", "/api/v1/tenants/system/webauthn", false},
		{"parent login group gated", "/api/v1/tenants/system/webauthn/login", false},
		{"deeper path gated", "/api/v1/tenants/system/webauthn/login/begin/extra", false},
		{"tenant admin gated", "/api/v1/tenants/system/users", false},
		{"tenant barrier gated", "/api/v1/tenants/system/barrier/unseal", false},

		// Malformed paths never match.
		{"empty tenant segment", "/api/v1/tenants//webauthn/login/begin", false},
		{"missing tenant segment", "/api/v1/tenants/webauthn/login/begin", false},
		{"relative path", "api/v1/tenants/system/webauthn/login/begin", false},
		{"double leading slash", "//api/v1/tenants/system/webauthn/login/begin", false},
		{"root", "/", false},
		{"empty", "", false},
		{"wrong group", "/api/v1/projects/system/webauthn/login/begin", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, isPreAuthRoute(tc.path), "isPreAuthRoute")
			if tc.want {
				// Both gates must agree for a pre-auth route; the non-pre-auth
				// cases may legitimately be public for other reasons (e.g. the
				// /api/v1/iam invite prefixes), so only the positive direction
				// is a cross-predicate invariant.
				assert.True(t, isAuthzPublicEndpoint(tc.path), "isAuthzPublicEndpoint")
				assert.True(t, isDefaultDenyPublic(tc.path), "isDefaultDenyPublic")
			}
		})
	}
}

// TestPreAuthRoutePredicates_GatedSiblingsNotPublic pins that the enrollment
// and administrative siblings are not public under EITHER predicate — the exact
// over-exemption this change must avoid.
func TestPreAuthRoutePredicates_GatedSiblingsNotPublic(t *testing.T) {
	for _, sibling := range gatedTenantSiblingPaths {
		t.Run(sibling.path, func(t *testing.T) {
			assert.False(t, isAuthzPublicEndpoint(sibling.path), "isAuthzPublicEndpoint")
			assert.False(t, isDefaultDenyPublic(sibling.path), "isDefaultDenyPublic")
		})
	}
}
