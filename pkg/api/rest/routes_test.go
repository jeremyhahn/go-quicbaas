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
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// baseRouteTableSize is the number of routes the table carries independent of
// build-tag-conditional contributions: the 455 surfaces converted from the
// historical setupRouter registrations, the cluster CA secret-delivery route,
// and the 160 IAM/OIDC + ACME directory + docs + UI + metrics-scrape +
// debug-mount surfaces contributed by routes_iam.go (Wave A of the unified-
// API-server cutover: these surfaces gained module registration in addition
// to their existing legacy-listener registration, see routes_iam.go's
// package doc comment), MINUS the 16 qrdb-wire proxy routes removed once
// go-qrdb's unified REST server started serving /v1/credentials (8),
// /users/by-username (1), and /v1/users/factors/* (7) natively on the same
// listener. GroupBarrierWire (14) / GroupCeremonyWire (8) /
// GroupInitWire (1) stay in the table: they back the standalone
// sealed-window REST listener (pkg/server/sealed_window_rest.go), which
// exists before go-qrdb's unified server does. Plus 1 for the restored RFC
// 9728 protected-resource metadata route (/.well-known/oauth-protected-
// resource, iam_protected_resource_metadata.go), the MCP client-auth
// discovery plane recovered as a module public route (unified-API-server
// login-plane plan, P6b). Plus 1 for POST /api/v1/iam/audit
// (RecordAuditEvent, iam_audit.go), the remote audit-emission endpoint
// closing the last hole in the unified audit plane (P5.3). MINUS 3 for the
// P3.3 de-collision: refresh/logout/validate/login-piv (4) dropped from
// apiAuthPublicRouteSpecs (a composed node's xkms module now owns those
// /api/v1/auth/* paths as part of its own session-login plane), PLUS 1 for
// login-piv's new IAM-namespaced home (POST /api/v1/iam/login/piv,
// iamManagedRouteSpecs) -- net -3. The rag build tag contributes
// ragRouteSpecs() on top. NOTE: this count is Routes(), the full table
// (legacy listener + module surface); ModuleRoutes() (routes.go), which
// additionally excludes /api/v1/barrier/* (14) and /api/v1/init/* (9) from
// the module-only surface, is 23 SMALLER and has its own guard
// (TestModuleRoutes_ExcludesBarrierAndInit below).
const baseRouteTableSize = 599

// TestRoutes_SurfaceGuard asserts the table's structural invariants: exact
// size, absolute patterns, no duplicate method+pattern, and a handler binder
// on every entry.
func TestRoutes_SurfaceGuard(t *testing.T) {
	specs := Routes()

	want := baseRouteTableSize + len(ragRouteSpecs())
	if len(specs) != want {
		t.Errorf("route table size: got %d, want %d", len(specs), want)
	}

	seen := map[string]struct{}{}
	for _, spec := range specs {
		if spec.Method == "" {
			t.Errorf("route %q has empty method", spec.Pattern)
		}
		if !strings.HasPrefix(spec.Pattern, "/") {
			t.Errorf("route pattern not absolute: %q", spec.Pattern)
		}
		if spec.Handler == nil {
			t.Errorf("route %s %s has no handler binder", spec.Method, spec.Pattern)
		}
		key := spec.Method + " " + spec.Pattern
		if _, dup := seen[key]; dup {
			t.Errorf("duplicate route: %s", key)
		}
		seen[key] = struct{}{}

		if mount, wire := routeGroupMounts[spec.Group]; wire {
			if !strings.HasPrefix(spec.Pattern, mount) {
				t.Errorf("route %s %s not under its group mount %s", spec.Method, spec.Pattern, mount)
			}
		} else if !strings.HasPrefix(spec.Pattern, "/api/v1/") {
			t.Errorf("route %s %s in an /api/v1 group but not under /api/v1", spec.Method, spec.Pattern)
		}
	}
}

// TestRoutes_PublicRoutes pins the exact public sub-surface: only the health
// probes and the version endpoint serve unauthenticated. Anything else
// showing up here is a pre-auth exposure regression.
func TestRoutes_PublicRoutes(t *testing.T) {
	want := map[string]struct{}{
		"GET /api/v1/health":       {},
		"GET /api/v1/health/live":  {},
		"GET /api/v1/health/ready": {},
		"GET /api/v1/version":      {},

		// OIDC/OAuth2 discovery (GroupOIDCDiscovery) + core token endpoints
		// (GroupOAuth2Core): public by protocol, unwrapped on the legacy
		// listener (routes_iam.go oidcDiscoveryRouteSpecs / oauth2CoreRouteSpecs).
		"GET /.well-known/openid-configuration":       {},
		"GET /.well-known/oauth-authorization-server": {},
		"GET /.well-known/oauth-protected-resource":   {},
		"GET /.well-known/jwks.json":                  {},
		"GET /oauth2/authorize":                       {},
		"POST /oauth2/token":                          {},
		"POST /oauth2/token-mtls":                     {},
		"GET /oauth2/userinfo":                        {},
		"POST /oauth2/revoke":                         {},
		"POST /oauth2/introspect":                     {},

		// RFC 7592 dynamic client management: pre-auth via the per-client
		// registration access token (GroupOAuth2Register, PEP-derived).
		"GET /oauth2/register/{client_id}":    {},
		"PUT /oauth2/register/{client_id}":    {},
		"DELETE /oauth2/register/{client_id}": {},

		// /api/v1/auth login/self-registration ceremonies, federated
		// login/callback, and mTLS agent-registration bootstrap
		// (GroupAPIAuthPublic): unwrapped on the legacy listener. The token-
		// lifecycle verbs (refresh/logout/validate) and login/piv were
		// removed from here by P3.3 (apiAuthPublicRouteSpecs no longer
		// carries them -- a composed node's xkms module owns those
		// /api/v1/auth/* paths; the legacy listener alone keeps serving them
		// via iam.go's independent hand-mount, which this module-surface
		// table does not feed).
		"POST /api/v1/auth/login/begin":                                   {},
		"POST /api/v1/auth/login/finish":                                  {},
		"POST /api/v1/auth/register/begin":                                {},
		"POST /api/v1/auth/register/finish":                               {},
		"GET /api/v1/auth/providers/{tenant_id}/{provider_name}/login":    {},
		"GET /api/v1/auth/providers/{tenant_id}/{provider_name}/callback": {},
		"POST /api/v1/agents/register":                                    {},

		// Invite activation + WebAuthn login ceremony (GroupIAM, PEP-derived
		// via preAuthRouteTemplates). login/piv (P3.3) is quicbaas's own
		// OIDC-session PIV login, moved here from /api/v1/auth/login/piv to
		// clear the collision with xkms's module.
		"GET /api/v1/iam/{tenant_id}/invites/{token}":                        {},
		"POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/begin":  {},
		"POST /api/v1/iam/{tenant_id}/invites/{token}/activate/fido2/finish": {},
		"POST /api/v1/iam/{tenant_id}/invites/{token}/activate/piv":          {},
		"POST /api/v1/iam/login/piv":                                         {},
		"POST /api/v1/iam/{tenant_id}/webauthn/login/begin":                  {},
		"POST /api/v1/iam/{tenant_id}/webauthn/login/finish":                 {},

		// Raw RFC 8555 ACME directory (GroupACMEDirectory): JWS-self-
		// authenticating per the ACME protocol, unwrapped on the legacy
		// listener.
		"GET /acme/directory":            {},
		"HEAD /acme/directory":           {},
		"GET /acme/new-nonce":            {},
		"HEAD /acme/new-nonce":           {},
		"POST /acme/new-account":         {},
		"POST /acme/account/{id}":        {},
		"POST /acme/new-order":           {},
		"POST /acme/order/{id}":          {},
		"POST /acme/order/{id}/finalize": {},
		"POST /acme/orders/{id}":         {},
		"POST /acme/authz/{id}":          {},
		"POST /acme/challenge/{id}":      {},
		"POST /acme/cert/{id}":           {},
		"POST /acme/revoke-cert":         {},
		"POST /acme/key-change":          {},
		"GET /acme/ca-bundle":            {},

		// Docs (GroupDocs) + embedded UI (GroupUIMount): unwrapped on the
		// legacy listener.
		"GET /docs":              {},
		"GET /docs/":             {},
		"GET /docs/openapi.yaml": {},
		"GET /ui":                {},
		"GET /ui/*":              {},
	}
	got := map[string]struct{}{}
	for _, spec := range Routes() {
		if spec.Auth == RouteAuthPublic {
			got[spec.Method+" "+spec.Pattern] = struct{}{}
		}
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("expected public route missing: %s", key)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected public route: %s", key)
		}
	}
}

// TestRoutes_SessionRoutes pins the identity-gated (mapped-permission-less)
// sub-surface. A route falling out of the PEP mapping lands here and is
// served identity-gated on the unified server — this pin makes any such
// regression explicit.
func TestRoutes_SessionRoutes(t *testing.T) {
	want := map[string]struct{}{
		"GET /api/v1/metrics/performance": {},
		// Prometheus scrape mount (GroupMetricsScrape): no PEP entry maps
		// "/api/v1/metrics" specifically, so it derives to the same
		// identity-gated posture as its "performance" sibling above (see
		// routes_iam.go metricsScrapeRouteSpecs).
		"GET /api/v1/metrics/": {},
	}
	got := map[string]struct{}{}
	for _, spec := range Routes() {
		if spec.Auth == RouteAuthSession {
			got[spec.Method+" "+spec.Pattern] = struct{}{}
		}
	}
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("expected session route missing: %s", key)
		}
	}
	for key := range got {
		if _, ok := want[key]; !ok {
			t.Errorf("unexpected session (unmapped) route: %s", key)
		}
	}
}

// routeAuthLabel renders a RouteAuth as the golden file's stable label.
func routeAuthLabel(a RouteAuth) string {
	switch a {
	case RouteAuthPublic:
		return "public"
	case RouteAuthSession:
		return "session"
	case RouteAuthPermission:
		return "permission"
	default:
		return "unknown"
	}
}

// formatRoutePermissionLine renders one Routes() entry in the golden file's
// stable, greppable format: "METHOD PATTERN CLASS RESOURCE:ACTION". RESOURCE
// and ACTION are "-" for public/session routes, which carry none.
func formatRoutePermissionLine(spec RouteSpec) string {
	resource, action := spec.Resource, spec.Action
	if resource == "" {
		resource = "-"
	}
	if action == "" {
		action = "-"
	}
	return fmt.Sprintf("%s %s %s %s:%s", spec.Method, spec.Pattern, routeAuthLabel(spec.Auth), resource, action)
}

// TestRoutes_FrozenPermissionsGolden pins Routes()'s FULL, EXACT
// authorization surface — every route's method, pattern, auth class, and
// (for permission routes) resource:action — against
// testdata/route_permissions.golden. This is the frozen counterpart to the
// live PEP mapper: it does not call authz_pep_mapping.go at all, so it keeps
// passing even after the mapper is deleted at cutover, and any route added,
// renamed, or re-permissioned without regenerating
// routes_permissions_generated.go (`go generate ./pkg/api/rest`) and
// updating the golden file fails here.
func TestRoutes_FrozenPermissionsGolden(t *testing.T) {
	var got []string
	for _, spec := range Routes() {
		got = append(got, formatRoutePermissionLine(spec))
	}
	sort.Strings(got)

	raw, err := os.ReadFile("testdata/route_permissions.golden")
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n")
	sort.Strings(want)

	if len(got) != len(want) {
		t.Errorf("route permission surface size: got %d, want %d (regenerate testdata/route_permissions.golden)",
			len(got), len(want))
	}
	wantSet := make(map[string]struct{}, len(want))
	for _, w := range want {
		wantSet[w] = struct{}{}
	}
	gotSet := make(map[string]struct{}, len(got))
	for _, g := range got {
		gotSet[g] = struct{}{}
	}
	for g := range gotSet {
		if _, ok := wantSet[g]; !ok {
			t.Errorf("route permission not in golden surface: %s", g)
		}
	}
	for w := range wantSet {
		if _, ok := gotSet[w]; !ok {
			t.Errorf("golden route permission missing from Routes(): %s", w)
		}
	}
}

// TestModuleRoutes_ExcludesBarrierAndInit proves ModuleRoutes() (the table
// pkg/apimodule module_http.go's RegisterHTTP consumes) drops every
// /api/v1/barrier/* and /api/v1/init/* entry that Routes() carries, while
// leaving every other route untouched. P3.3: on a composed node the qrdb
// node's own native barrier/init planes (or the xkms ServiceModule) already
// own that surface, so quicbaas's own module registrations for it would
// either collide at module-construction time or shadow the node's real
// state with quicbaas's own pre-unseal, node-less handlers.
func TestModuleRoutes_ExcludesBarrierAndInit(t *testing.T) {
	all := Routes()
	module := ModuleRoutes()

	var excludedFromAll int
	for _, spec := range all {
		if strings.HasPrefix(spec.Pattern, "/api/v1/barrier/") || strings.HasPrefix(spec.Pattern, "/api/v1/init/") {
			excludedFromAll++
		}
	}
	if excludedFromAll == 0 {
		t.Fatal("sanity: Routes() must still carry /api/v1/barrier|init/* routes")
	}
	if got, want := len(all)-len(module), excludedFromAll; got != want {
		t.Errorf("ModuleRoutes() size delta: got %d fewer than Routes(), want %d", got, want)
	}

	moduleSet := make(map[string]struct{}, len(module))
	for _, spec := range module {
		moduleSet[spec.Method+" "+spec.Pattern] = struct{}{}
	}
	for _, spec := range all {
		key := spec.Method + " " + spec.Pattern
		excluded := strings.HasPrefix(spec.Pattern, "/api/v1/barrier/") || strings.HasPrefix(spec.Pattern, "/api/v1/init/")
		_, present := moduleSet[key]
		if excluded && present {
			t.Errorf("ModuleRoutes() must NOT carry %s (barrier/init are module-excluded)", key)
		}
		if !excluded && !present {
			t.Errorf("ModuleRoutes() dropped non-excluded route %s", key)
		}
	}
}

// TestLegacyListener_StillServesBarrierAndInit is the item-3 acceptance
// proof: the standalone in-process listener (registerRoutes, server.go),
// which is unaffected by ModuleRoutes filtering the module-only surface
// (pkg/apimodule), still mounts every /api/v1/barrier/* and /api/v1/init/*
// route from the SAME GroupAPI table registerRoutes has always consumed.
// This is the sealed-window boot path's dependency: it must keep working on
// a standalone (non-composed) node even after P3.3 excludes those paths
// from the unified-server module surface.
func TestLegacyListener_StillServesBarrierAndInit(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatal(err)
	}
	srv, err := NewServer(testutil.NewMockQuicBaaS(), DefaultConfig(), log)
	if err != nil {
		t.Fatal(err)
	}

	served := map[string]struct{}{}
	walkErr := chi.Walk(srv.router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		served[method+" "+route] = struct{}{}
		return nil
	})
	if walkErr != nil {
		t.Fatal(walkErr)
	}

	var checked int
	for _, spec := range Routes() {
		if !strings.HasPrefix(spec.Pattern, "/api/v1/barrier/") && !strings.HasPrefix(spec.Pattern, "/api/v1/init/") {
			continue
		}
		checked++
		if _, ok := served[spec.Method+" "+spec.Pattern]; !ok {
			t.Errorf("legacy listener no longer serves %s %s", spec.Method, spec.Pattern)
		}
	}
	if checked == 0 {
		t.Fatal("sanity: expected at least one /api/v1/barrier|init/* route in Routes()")
	}
}
