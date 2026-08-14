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
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// This file holds the ROUTE-TREE GUARD: the structural test that retires the
// "mounted but unmapped" class of authorization bug.
//
// Under the production posture (SecurityConfig.RequireAuthz defaults to true)
// the REST gate is AuthorizeViaValidator, which is default-deny for any path
// mapRequestToPermission does not resolve (authz_validator.go). A surface can
// therefore be mounted, documented, SDK-wired and completely unreachable — every
// request answering 403 "Authorization denied" — purely because nobody added a
// pepMapping entry. That failure is invisible to per-handler tests, which call
// the handler directly and never traverse the gate.
//
// The guard walks the ACTUAL chi route tree of a fully constructed server and
// asserts that every mounted route either resolves to a qrdb-canonical
// permission or is named in an explicit allowlist. Adding an unmapped route is
// therefore a build-breaking act, and exempting one is a deliberate, reviewed
// edit to the allowlist below.

// pepGuardPlaceholder replaces a chi path parameter ("{id}") or wildcard ("*")
// when a route template is concretized into a request path. It is deliberately
// an ordinary segment so it exercises the same item-vs-collection logic a real
// request would.
const pepGuardPlaceholder = "guard"

// preAuthRoutePrefixes are the mounted routes that MUST resolve without a
// platform token. Two distinct reasons appear here and both are load-bearing:
//
//	(1) PRE-AUTH BY DESIGN — the endpoint is how a caller obtains a token, or is
//	    an unauthenticated liveness/discovery surface. These are the same paths
//	    publicEndpoints (authz_middleware.go) and defaultDenyPublicPrefixes
//	    (authz_default_deny.go) admit.
//
//	(2) MOUNTED OUTSIDE THE PLATFORM-TOKEN GATE — the route lives at the router
//	    root and useAPIAuthzMiddleware is never installed on it because the
//	    surface carries its OWN authentication protocol (RFC 8555 JWS for ACME,
//	    RFC 6749/9449 client authentication for /oauth2, mTLS handshake identity
//	    for agent registration). A pepMapping entry would be meaningless: the
//	    validator gate never runs on these paths.
//
// Anything not listed here is a protected surface and MUST map.
var preAuthRoutePrefixes = []string{
	// (1) Unauthenticated probes + build info.
	"/api/v1/health",
	"/api/v1/version",
	"/api/v1/metrics",
	"/metrics",

	// (1) Token-minting ceremonies and token lifecycle. The caller holds no
	// bearer until these succeed; /auth/validate and /auth/logout authenticate
	// via the token carried IN the request body.
	"/api/v1/auth",

	// (1) Invite activation — the one-time invite token in the URL IS the
	// authorization, and activation is what enrolls the first factor.
	"/api/v1/iam/invites",

	// (2) Agent enrollment bootstrap: the daemon authenticates with its mTLS
	// client certificate on the TLS handshake and the handler mints credentials.
	"/api/v1/agents/register",

	// (2) OIDC / OAuth2 discovery, token, introspection and revocation.
	// Root-mounted, outside every platform-token gate; each endpoint performs
	// its own RFC-defined client authentication.
	//
	// NOTE the two exceptions under this prefix, both of which nonetheless
	// resolve through mapRequestToPermission so the guard would pass without
	// this allowlist entry:
	//   - POST /oauth2/register (RFC 7591) is NOT pre-auth. It carries
	//     useAPIAuthzMiddleware and maps to tenants:create; the presented
	//     platform token is the initial access token.
	//   - GET/PUT/DELETE /oauth2/register/{client_id} (RFC 7592) ARE pre-auth,
	//     declared in preAuthRouteTemplates, and gated by the per-client
	//     registration access token instead.
	"/.well-known",
	"/oauth2",

	// (2) RFC 8555 ACME server. Root-mounted per the ACME URL convention and
	// outside every platform-token gate: ACME clients authenticate with a JWS
	// signed by the account key (plus nonce replay protection and optional
	// External Account Binding), never with a platform bearer token. The
	// QuicBaaS ACME CONTROL plane is a different surface (/api/v1/acme) and is
	// mapped.
	"/acme",

	// (1) Human-facing UI, API documentation and the root redirect.
	"/ui",
	"/docs",
}

// pepGuardExemptExactPaths are individual concretized paths that are pre-auth
// but whose prefix cannot be exempted wholesale.
var pepGuardExemptExactPaths = map[string]bool{
	// The root redirect to /docs/.
	"/": true,
}

// isPEPGuardExempt reports whether a concretized request path is allowlisted.
func isPEPGuardExempt(path string) bool {
	if pepGuardExemptExactPaths[path] {
		return true
	}
	// The tenant-scoped WebAuthn LOGIN ceremony carries a variable tenant
	// segment that a literal prefix cannot express; the production gates consult
	// the same predicate.
	if isPreAuthRoute(path) {
		return true
	}
	for _, prefix := range preAuthRoutePrefixes {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

// concretizeRoute turns a chi route template into a request path by replacing
// every parameter segment ("{id}") and wildcard ("*") with a literal segment.
func concretizeRoute(route string) string {
	route = strings.TrimSuffix(route, "/")
	if route == "" {
		return "/"
	}
	segments := strings.Split(route, "/")
	for i, segment := range segments {
		if strings.HasPrefix(segment, "{") || segment == "*" {
			segments[i] = pepGuardPlaceholder
		}
	}
	return strings.Join(segments, "/")
}

// walkMountedRoutes returns every (method, concretized path) pair mounted on a
// fully constructed REST server, sorted for deterministic failure output.
func walkMountedRoutes(t *testing.T) [][2]string {
	t.Helper()

	s, _ := newTestServer(t)
	var routes [][2]string
	err := chi.Walk(s.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, [2]string{method, concretizeRoute(route)})
		return nil
	})
	if err != nil {
		t.Fatalf("chi.Walk: %v", err)
	}
	if len(routes) == 0 {
		t.Fatal("chi.Walk returned no routes; the guard would vacuously pass")
	}
	sort.Slice(routes, func(i, j int) bool {
		if routes[i][1] != routes[j][1] {
			return routes[i][1] < routes[j][1]
		}
		return routes[i][0] < routes[j][0]
	})
	return routes
}

// TestPEPMappingCoversEveryMountedRoute is the guard. It walks the real chi
// route tree and fails when a mounted, non-allowlisted route does not resolve
// through mapRequestToPermission — i.e. when the production validator gate
// would answer 403 "Authorization denied" for every request to it.
//
// To exempt a route, add it to preAuthRoutePrefixes above WITH the reason it is
// reachable without a platform token. Do not silence this test any other way.
func TestPEPMappingCoversEveryMountedRoute(t *testing.T) {
	var unmapped []string
	for _, route := range walkMountedRoutes(t) {
		method, path := route[0], route[1]
		if isPEPGuardExempt(path) {
			continue
		}
		// CONNECT and TRACE have no qrdb action and are intentionally unmapped
		// (actionForMethod fails closed for them). chi registers them only on
		// the catch-all UI mount, which is already exempt, so reaching one here
		// means a protected surface registered a method with no canonical verb.
		resource, action, ok := mapRequestToPermission(path, method)
		if !ok {
			unmapped = append(unmapped, method+" "+path)
			continue
		}
		if resource == "" || action == "" {
			unmapped = append(unmapped, method+" "+path+" (empty resource/action)")
		}
	}
	if len(unmapped) > 0 {
		t.Fatalf("%d mounted route(s) have no PEP permission mapping and are "+
			"therefore hard-403 under the production RequireAuthz posture:\n  %s\n"+
			"Add a pepMapping / pepSpecialRules entry, or (if the route is "+
			"genuinely pre-auth) add it to preAuthRoutePrefixes with a reason.",
			len(unmapped), strings.Join(unmapped, "\n  "))
	}
}

// TestPEPGuardAllowlistIsNotOverbroad asserts the guard's own allowlist cannot
// silently swallow a protected surface: no allowlist prefix may cover a path
// that the PEP mapping deliberately protects. Without this the guard could be
// "fixed" by widening the allowlist to /api/v1.
func TestPEPGuardAllowlistIsNotOverbroad(t *testing.T) {
	protected := []string{
		"/api/v1/kms/keys",
		"/api/v1/secrets/db",
		"/api/v1/secrets-versions/db",
		"/api/v1/trust/cross-signed-roots",
		"/api/v1/acme/issue",
		"/api/v1/xkms/keys",
		"/api/v1/seal/status",
		"/api/v1/batch/put",
		"/api/v1/query/users/email",
		"/api/v1/barrier/status",
		"/api/v1/tenants",
		"/v1/credentials/strategy",
	}
	for _, path := range protected {
		if isPEPGuardExempt(path) {
			t.Errorf("guard allowlist exempts protected path %q", path)
		}
	}
}

// TestPEPGuardConcretizeRoute covers the route-template concretization, both the
// nominal parameter/wildcard substitution and the degenerate inputs chi can
// produce for a root mount.
func TestPEPGuardConcretizeRoute(t *testing.T) {
	cases := []struct{ route, want string }{
		{"/api/v1/kms/keys/{name}/rotate", "/api/v1/kms/keys/guard/rotate"},
		{"/api/v1/secrets-versions/*", "/api/v1/secrets-versions/guard"},
		{"/api/v1/kms/keys", "/api/v1/kms/keys"},
		{"/v1/credentials/{name}/versions/{version}", "/v1/credentials/guard/versions/guard"},
		{"/docs/", "/docs"},
		{"/", "/"},
		{"", "/"},
	}
	for _, tc := range cases {
		if got := concretizeRoute(tc.route); got != tc.want {
			t.Errorf("concretizeRoute(%q) = %q, want %q", tc.route, got, tc.want)
		}
	}
}
