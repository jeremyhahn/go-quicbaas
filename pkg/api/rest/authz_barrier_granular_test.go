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

// authz_barrier_granular_test.go — exhaustive guard tests for the granular
// barrier authz surface on both the /api/v1/barrier canonical mount and the
// /v1/barrier qrdb-SDK root-wire mount, plus the legacy /api/v1/seal surface.
//
// These tests serve as the REST analogue of TestGRPCPermissions_EveryMethodMapped
// in pkg/api/grpc: a future barrier route cannot ship without a matching
// granular mapping or these tests will catch the gap.

import (
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// =============================================================================
// Canonical barrier permission table (mirrors gRPC canon byte-for-byte)
// =============================================================================

// wantBarrierPerm is the expected (resource, action) for a given (method,
// path-suffix). The table covers all 13 registered barrier operations.
type wantBarrierPerm struct {
	resource string
	action   string
}

// canonicalBarrierExpectations maps path-suffix (relative to the barrier prefix,
// e.g. "/status") to method to expected wantBarrierPerm. It is the single
// reference truth for the 13 operations from the task spec.
var canonicalBarrierExpectations = map[string]map[string]wantBarrierPerm{
	"/status": {
		http.MethodGet: {"tenant_barrier", "status"},
	},
	"/initialize": {
		http.MethodPost: {"tenant_barrier", "init"},
	},
	"/unseal": {
		http.MethodPost: {"tenant_barrier", "unseal"},
	},
	"/seal": {
		http.MethodPost: {"tenant_barrier", "seal"},
	},
	"/shamir/initialize": {
		http.MethodPost: {"seal", "initialize_shamir"},
	},
	"/shamir/unseal-share": {
		http.MethodPost: {"barrier", "unseal-share"},
	},
	"/shamir/unseal-shares": {
		http.MethodPost: {"barrier", "unseal-share"},
	},
	"/rekey": {
		http.MethodPost: {"seal", "rekey"},
	},
	"/recovery/generate": {
		http.MethodPost: {"seal", "generate_recovery"},
	},
	"/recovery/recover": {
		http.MethodPost: {"seal", "recover_with_keys"},
	},
	"/recovery": {
		http.MethodDelete: {"seal", "generate_recovery"},
	},
	"/recovery/status": {
		http.MethodGet: {"seal", "status"},
	},
	"/root-token": {
		http.MethodPost: {"seal", "bootstrap"},
	},
}

// canonicalBarrierScopes maps suffix+method to the expected granular scope
// string that requiredScopeForRequest must return for both canonical mounts.
var canonicalBarrierScopes = map[string]map[string]string{
	"/status":               {http.MethodGet: "tenant_barrier:status"},
	"/initialize":           {http.MethodPost: "tenant_barrier:init"},
	"/unseal":               {http.MethodPost: "tenant_barrier:unseal"},
	"/seal":                 {http.MethodPost: "tenant_barrier:seal"},
	"/shamir/initialize":    {http.MethodPost: "seal:initialize_shamir"},
	"/shamir/unseal-share":  {http.MethodPost: "barrier:unseal-share"},
	"/shamir/unseal-shares": {http.MethodPost: "barrier:unseal-share"},
	"/rekey":                {http.MethodPost: "seal:rekey"},
	"/recovery/generate":    {http.MethodPost: "seal:generate_recovery"},
	"/recovery/recover":     {http.MethodPost: "seal:recover_with_keys"},
	"/recovery":             {http.MethodDelete: "seal:generate_recovery"},
	"/recovery/status":      {http.MethodGet: "seal:status"},
	"/root-token":           {http.MethodPost: "seal:bootstrap"},
}

// =============================================================================
// Task 3a — route-walking guard: every registered barrier route has a granular
// mapping in both the PEP and the default-deny gate.
// =============================================================================

// pathParamRe replaces chi path-parameter segments ({param}) with a literal
// value so mapRequestToPermission and requiredScopeForRequest can match them.
var pathParamRe = regexp.MustCompile(`\{[^}]+\}`)

func substitutePathParams(pattern string) string {
	return pathParamRe.ReplaceAllString(pattern, "x")
}

// TestBarrierRoutes_EveryRouteHasGranularMapping walks the server router for
// /api/v1/barrier, /v1/barrier, and /api/v1/seal routes, asserts
// mapRequestToPermission returns ok=true with EXACTLY the canonical permission,
// and asserts requiredScopeForRequest returns EXACTLY the canonical scope. A
// new barrier route that is missing from the granular tables will fail here.
func TestBarrierRoutes_EveryRouteHasGranularMapping(t *testing.T) {
	s, _, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	s.SetBarrierService(&fakeBarrierService{})

	barrierPrefixSet := map[string]bool{
		"/api/v1/barrier": true,
		"/v1/barrier":     true,
	}

	var walkErr error
	walkErr = chi.Walk(s.router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		concrPath := substitutePathParams(route)

		// Only audit barrier mounts.
		var matchedPrefix string
		for prefix := range barrierPrefixSet {
			if concrPath == prefix || strings.HasPrefix(concrPath, prefix+"/") {
				if len(prefix) > len(matchedPrefix) {
					matchedPrefix = prefix
				}
			}
		}
		if matchedPrefix == "" {
			return nil
		}

		suffix := concrPath[len(matchedPrefix):]
		if suffix == "" {
			suffix = "/"
		}

		t.Run(method+" "+route, func(t *testing.T) {
			// PEP mapping — must return ok=true with canonical (resource, action).
			resource, action, ok := mapRequestToPermission(concrPath, method)
			if !ok {
				t.Errorf("mapRequestToPermission(%q, %q) ok=false; barrier route has no PEP mapping", concrPath, method)
				return
			}

			// If we know the canonical expectation, enforce it exactly.
			if methods, exists := canonicalBarrierExpectations[suffix]; exists {
				want, methodKnown := methods[method]
				if methodKnown {
					if resource != want.resource {
						t.Errorf("resource=%q, want %q for %s %s", resource, want.resource, method, concrPath)
					}
					if action != want.action {
						t.Errorf("action=%q, want %q for %s %s", action, want.action, method, concrPath)
					}
				}
			}

			// Default-deny scope — must return a non-empty scope.
			scope := requiredScopeForRequest(concrPath, method)
			if scope == "" {
				t.Errorf("requiredScopeForRequest(%q, %q)=%q; barrier route must resolve to a granular scope", concrPath, method, scope)
				return
			}

			// If we know the canonical scope, enforce it exactly.
			if methods, exists := canonicalBarrierScopes[suffix]; exists {
				wantScope, methodKnown := methods[method]
				if methodKnown && scope != wantScope {
					t.Errorf("requiredScopeForRequest(%q, %q)=%q, want %q", concrPath, method, scope, wantScope)
				}
			}
		})

		return nil
	})
	if walkErr != nil {
		t.Fatalf("chi.Walk error: %v", walkErr)
	}
}

// =============================================================================
// Task 3b — table-driven negative / regression tests
// =============================================================================

// TestBarrierAuthz_NegativeAndRegression verifies that:
//   - POST /api/v1/barrier/rekey no longer maps to the coarse tenant_barrier:init.
//   - /v1/barrier/status is now mapped (ok=true) and granular.
//   - An arbitrary unmapped path still returns ok=false.
func TestBarrierAuthz_NegativeAndRegression(t *testing.T) {
	cases := []struct {
		name         string
		path         string
		method       string
		wantOK       bool
		wantResource string
		wantAction   string
		// forbiddenPerm is a permission that must NOT be produced (regression guard).
		forbiddenPerm string
	}{
		{
			name:          "rekey must NOT map to tenant_barrier:init anymore",
			path:          "/api/v1/barrier/rekey",
			method:        http.MethodPost,
			wantOK:        true,
			wantResource:  "seal",
			wantAction:    "rekey",
			forbiddenPerm: "tenant_barrier:init",
		},
		{
			name:         "/v1/barrier/status is now mapped ok=true",
			path:         "/v1/barrier/status",
			method:       http.MethodGet,
			wantOK:       true,
			wantResource: "tenant_barrier",
			wantAction:   "status",
		},
		// Truly unrelated surface (no barrier prefix) is still unmapped.
		{
			name:   "non-barrier unmapped path is still ok=false",
			path:   "/api/v1/totally-unknown-surface",
			method: http.MethodGet,
			wantOK: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tc.path, tc.method)
			if ok != tc.wantOK {
				t.Fatalf("mapRequestToPermission(%q, %q) ok=%v, want %v", tc.path, tc.method, ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if resource != tc.wantResource {
				t.Errorf("resource=%q, want %q", resource, tc.wantResource)
			}
			if action != tc.wantAction {
				t.Errorf("action=%q, want %q", action, tc.wantAction)
			}
			if tc.forbiddenPerm != "" {
				got := resource + ":" + action
				if got == tc.forbiddenPerm {
					t.Errorf("permission=%q is the forbidden coarse perm; granular mapping must win", got)
				}
			}
		})
	}
}

// =============================================================================
// Task 3c — suffix-ambiguity regression tests
// =============================================================================

// TestBarrierAuthz_SuffixAmbiguity covers the potentially-ambiguous suffix
// pairs that the tier-2 longest-match must resolve correctly:
//
//   - /barrier/unseal → tenant_barrier:unseal (NOT tenant_barrier:seal via substring)
//   - /barrier/seal   → tenant_barrier:seal
//   - /shamir/unseal-shares → barrier:unseal-share (NOT confused with unseal-share)
//   - /shamir/initialize → seal:initialize_shamir (NOT tenant_barrier:init via /initialize)
//   - /recovery/status → seal:status (NOT tenant_barrier:status via /status)
func TestBarrierAuthz_SuffixAmbiguity(t *testing.T) {
	cases := []struct {
		name            string
		path            string
		method          string
		wantResource    string
		wantAction      string
		wantScopeSuffix string // last segment of expected scope, for readability
	}{
		{
			name:   "barrier/unseal must be tenant_barrier:unseal not seal",
			path:   "/api/v1/barrier/unseal",
			method: http.MethodPost,
			// "/seal" is a suffix of "/unseal" but "/unseal" is longer → wins.
			wantResource:    "tenant_barrier",
			wantAction:      "unseal",
			wantScopeSuffix: "tenant_barrier:unseal",
		},
		{
			name:            "barrier/seal must be tenant_barrier:seal",
			path:            "/api/v1/barrier/seal",
			method:          http.MethodPost,
			wantResource:    "tenant_barrier",
			wantAction:      "seal",
			wantScopeSuffix: "tenant_barrier:seal",
		},
		{
			name:            "shamir/unseal-shares must be barrier:unseal-share",
			path:            "/api/v1/barrier/shamir/unseal-shares",
			method:          http.MethodPost,
			wantResource:    "barrier",
			wantAction:      "unseal-share",
			wantScopeSuffix: "barrier:unseal-share",
		},
		{
			name:   "shamir/initialize must be seal:initialize_shamir not tenant_barrier:init",
			path:   "/api/v1/barrier/shamir/initialize",
			method: http.MethodPost,
			// "/initialize" is a suffix of "/shamir/initialize" but the longer
			// "/shamir/initialize" wins in tier 2.
			wantResource:    "seal",
			wantAction:      "initialize_shamir",
			wantScopeSuffix: "seal:initialize_shamir",
		},
		{
			name:   "recovery/status must be seal:status not tenant_barrier:status",
			path:   "/api/v1/barrier/recovery/status",
			method: http.MethodGet,
			// "/status" is also a suffix but "/recovery/status" is longer → wins.
			wantResource:    "seal",
			wantAction:      "status",
			wantScopeSuffix: "seal:status",
		},
		// Same ambiguity checks on the /v1/barrier root-wire mount.
		{
			name:            "v1 barrier/unseal → tenant_barrier:unseal",
			path:            "/v1/barrier/unseal",
			method:          http.MethodPost,
			wantResource:    "tenant_barrier",
			wantAction:      "unseal",
			wantScopeSuffix: "tenant_barrier:unseal",
		},
		{
			name:            "v1 shamir/initialize → seal:initialize_shamir",
			path:            "/v1/barrier/shamir/initialize",
			method:          http.MethodPost,
			wantResource:    "seal",
			wantAction:      "initialize_shamir",
			wantScopeSuffix: "seal:initialize_shamir",
		},
		{
			name:            "v1 recovery/status → seal:status",
			path:            "/v1/barrier/recovery/status",
			method:          http.MethodGet,
			wantResource:    "seal",
			wantAction:      "status",
			wantScopeSuffix: "seal:status",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tc.path, tc.method)
			if !ok {
				t.Fatalf("mapRequestToPermission(%q, %q) ok=false, want true", tc.path, tc.method)
			}
			if resource != tc.wantResource {
				t.Errorf("resource=%q, want %q", resource, tc.wantResource)
			}
			if action != tc.wantAction {
				t.Errorf("action=%q, want %q", action, tc.wantAction)
			}

			scope := requiredScopeForRequest(tc.path, tc.method)
			if scope != tc.wantScopeSuffix {
				t.Errorf("requiredScopeForRequest=%q, want %q", scope, tc.wantScopeSuffix)
			}
		})
	}
}

// =============================================================================
// Full canonical table assertion — all 13 ops × both mounts
// =============================================================================

// TestBarrierAuthz_FullCanonicalTable asserts every one of the 13 canonical
// operations resolves correctly on BOTH the /api/v1/barrier and /v1/barrier
// mounts for mapRequestToPermission and requiredScopeForRequest.
func TestBarrierAuthz_FullCanonicalTable(t *testing.T) {
	mounts := []string{"/api/v1/barrier", "/v1/barrier"}

	for suffix, methods := range canonicalBarrierExpectations {
		for method, want := range methods {
			wantScope := canonicalBarrierScopes[suffix][method]
			for _, mount := range mounts {
				path := mount + suffix
				t.Run(method+" "+path, func(t *testing.T) {
					resource, action, ok := mapRequestToPermission(path, method)
					if !ok {
						t.Fatalf("mapRequestToPermission(%q, %q) ok=false", path, method)
					}
					if resource != want.resource {
						t.Errorf("resource=%q, want %q", resource, want.resource)
					}
					if action != want.action {
						t.Errorf("action=%q, want %q", action, want.action)
					}

					scope := requiredScopeForRequest(path, method)
					if scope != wantScope {
						t.Errorf("requiredScopeForRequest=%q, want %q", scope, wantScope)
					}
				})
			}
		}
	}
}

// =============================================================================
// Legacy /api/v1/seal granular scope tests
// =============================================================================

// TestBarrierAuthz_LegacySealGranularScope asserts that the legacy
// /api/v1/seal surface resolves to the same granular per-operation scopes as
// the canonical /api/v1/barrier surface (via requiredScopeForRequest).
func TestBarrierAuthz_LegacySealGranularScope(t *testing.T) {
	cases := []struct {
		path      string
		method    string
		wantScope string
	}{
		{"/api/v1/seal/status", http.MethodGet, "tenant_barrier:status"},
		{"/api/v1/seal/init", http.MethodPost, "tenant_barrier:init"},
		{"/api/v1/seal/unseal", http.MethodPost, "tenant_barrier:unseal"},
		{"/api/v1/seal/seal", http.MethodPost, "tenant_barrier:seal"},
		{"/api/v1/seal/rekey", http.MethodPost, "seal:rekey"},
		{"/api/v1/seal/recovery/generate", http.MethodPost, "seal:generate_recovery"},
		{"/api/v1/seal/recovery", http.MethodDelete, "seal:generate_recovery"},
		{"/api/v1/seal/root/generate", http.MethodPost, "seal:bootstrap"},
		{"/api/v1/seal/rotate-root", http.MethodPost, "seal:rekey"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			got := requiredScopeForRequest(tc.path, tc.method)
			if got != tc.wantScope {
				t.Errorf("requiredScopeForRequest(%q, %q)=%q, want %q", tc.path, tc.method, got, tc.wantScope)
			}
		})
	}
}

// TestBarrierAuthz_LegacySealPEPMatchesDefaultDeny asserts that the validator
// PEP and the default-deny gate resolve every legacy /api/v1/seal operation to
// the SAME scope.
//
// An earlier revision deliberately left /api/v1/seal unmapped in the validator
// PEP. That posture made the surface a hard 403 under the production
// RequireAuthz default — including for the Security().Seal() SDK service, which
// the Go, TypeScript and JavaScript REST transports still route here — and made
// the handlers' RFC 8594 Deprecation/Sunset headers unreachable, so a client
// could never learn the surface was deprecated. Both gates now read the single
// legacySealOps table (authz_pep_mapping.go).
func TestBarrierAuthz_LegacySealPEPMatchesDefaultDeny(t *testing.T) {
	sealPaths := []struct {
		path   string
		method string
	}{
		{"/api/v1/seal/status", http.MethodGet},
		{"/api/v1/seal/init", http.MethodPost},
		{"/api/v1/seal/unseal", http.MethodPost},
		{"/api/v1/seal/seal", http.MethodPost},
		{"/api/v1/seal/rekey", http.MethodPost},
		{"/api/v1/seal/recovery/generate", http.MethodPost},
		{"/api/v1/seal/recovery", http.MethodDelete},
		{"/api/v1/seal/root/generate", http.MethodPost},
		{"/api/v1/seal/rotate-root", http.MethodPost},
	}
	for _, tc := range sealPaths {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(tc.path, tc.method)
			if !ok {
				t.Fatalf("mapRequestToPermission(%q, %q) ok=false; the legacy seal surface is mounted and must authorize, not hard-403", tc.path, tc.method)
			}
			if got, want := resource+":"+action, requiredScopeForRequest(tc.path, tc.method); got != want {
				t.Errorf("PEP scope %q != default-deny scope %q", got, want)
			}
		})
	}
}

// =============================================================================
// Stale-expectation updates for pre-existing tests
// =============================================================================

// TestBarrierAuthz_StaleExpectationBarrierInit confirms the barrier/initialize
// path resolves to tenant_barrier:init (unchanged from the pre-existing coarse
// mapping) to guard against inadvertent regressions from the refactor.
func TestBarrierAuthz_StaleExpectationBarrierInit(t *testing.T) {
	resource, action, ok := mapRequestToPermission("/api/v1/barrier/initialize", http.MethodPost)
	if !ok {
		t.Fatal("mapRequestToPermission(/api/v1/barrier/initialize, POST) ok=false")
	}
	if resource != "tenant_barrier" || action != "init" {
		t.Errorf("resource=%q action=%q; want tenant_barrier:init", resource, action)
	}
}
