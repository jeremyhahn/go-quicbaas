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

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// canonicalIAMUtilityExpectations is the locked route -> permission table for
// the root-mounted /api/v1/iam utility group (iam.go). Keys are
// "METHOD path" with chi path parameters substituted (substitutePathParams).
// Every protected route registered in the group MUST appear here; the
// route-walk test below fails when a new route is added without a conscious
// PEP mapping + expectation entry.
var canonicalIAMUtilityExpectations = map[string]string{
	// Pending self-registration review.
	"POST /api/v1/iam/users/x/approve": "users:enable",
	"POST /api/v1/iam/users/x/reject":  "users:disable",

	// OAuth2 client-credential validation (apps live under the tenants surface).
	"POST /api/v1/iam/validate-client": "tenants:read",

	// PEP access checks.
	"POST /api/v1/iam/check":       "permissions:resolve",
	"POST /api/v1/iam/check/batch": "permissions:resolve",
	"POST /api/v1/iam/batch-check": "permissions:resolve",

	// Audit log.
	"GET /api/v1/iam/audit":   "audit:query",
	"GET /api/v1/iam/audit/x": "audit:read",
	// No audit:delete is registered in qrdb yet; the handler additionally
	// enforces the fine-grained audit:delete AdminManager check.
	"DELETE /api/v1/iam/audit": "audit:query",

	// Login-session management (no session:* family in the qrdb registry;
	// sessions are state on the caller's user record).
	"GET /api/v1/iam/sessions":                "users:read",
	"GET /api/v1/iam/sessions/x":              "users:read",
	"DELETE /api/v1/iam/sessions":             "users:update",
	"DELETE /api/v1/iam/sessions/x":           "users:update",
	"POST /api/v1/iam/sessions/revoke-others": "users:update",
}

// iamUtilityPublicRoutes are routes inside the /api/v1/iam group that are
// intentionally public: they bypass the authz gate via isAuthzPublicEndpoint
// so the login UI works before a token exists.
var iamUtilityPublicRoutes = map[string]bool{
	"GET /api/v1/iam/tenants/public": true,
}

// TestIAMUtilityPEPMapping locks the qrdb-canonical permission for every
// endpoint in the /api/v1/iam utility group (table-driven against
// mapRequestToPermission directly).
func TestIAMUtilityPEPMapping(t *testing.T) {
	for key, wantPerm := range canonicalIAMUtilityExpectations {
		parts := strings.SplitN(key, " ", 2)
		method, path := parts[0], parts[1]
		t.Run(key, func(t *testing.T) {
			resource, action, ok := mapRequestToPermission(path, method)
			if !ok {
				t.Fatalf("mapRequestToPermission(%q, %q) ok=false; want %q", path, method, wantPerm)
			}
			if got := resource + ":" + action; got != wantPerm {
				t.Errorf("mapRequestToPermission(%q, %q) = %q, want %q", path, method, got, wantPerm)
			}
		})
	}
}

// TestIAMUtilityPEPMapping_CoarseFallback verifies that an unanticipated
// future sub-path under /api/v1/iam degrades to the mapped tenants-family
// fallback (same posture as the defaultDenyScopeTable) instead of being
// unmapped, and that an unknown HTTP verb still fails closed.
func TestIAMUtilityPEPMapping_CoarseFallback(t *testing.T) {
	resource, action, ok := mapRequestToPermission("/api/v1/iam/future-surface", http.MethodGet)
	if !ok {
		t.Fatal("expected coarse fallback mapping for unanticipated /api/v1/iam sub-path")
	}
	if got := resource + ":" + action; got != "tenants:read" {
		t.Errorf("coarse fallback = %q, want tenants:read", got)
	}

	if _, _, ok := mapRequestToPermission("/api/v1/iam/sessions", "TRACE"); ok {
		t.Error("unknown HTTP verb must remain unmapped (fail closed)")
	}
}

// TestIAMUtilityPublicTenantListing_Bypass verifies the login UI tenant
// selector endpoint is on the authz public allowlist: the middleware bypasses
// it BEFORE the PEP mapping is consulted, so wrapping the whole /api/v1/iam
// group does not break token-less login flows.
func TestIAMUtilityPublicTenantListing_Bypass(t *testing.T) {
	if !isAuthzPublicEndpoint("/api/v1/iam/tenants/public") {
		t.Fatal("/api/v1/iam/tenants/public must be an authz public endpoint")
	}
	// The invite-activation surface is a separate unwrapped mount, but guard
	// against a future registration moving it under the gated group without
	// an allowlist entry.
	if isAuthzPublicEndpoint("/api/v1/iam/sessions") {
		t.Fatal("/api/v1/iam/sessions must NOT be public")
	}
}

// TestIAMUtilityRoutes_EveryRouteHasMapping walks the live router and asserts
// every registered /api/v1/iam route is either declared public or carries
// EXACTLY the canonical PEP mapping. A new route added to the group without a
// conscious mapping + expectation entry fails here (the same guard class as
// TestBarrierRoutes_EveryRouteHasGranularMapping).
func TestIAMUtilityRoutes_EveryRouteHasMapping(t *testing.T) {
	s, _ := newTestServer(t)

	walkErr := chi.Walk(s.router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		concrPath := substitutePathParams(strings.TrimSuffix(route, "/"))

		// Only audit the /api/v1/iam mount.
		if concrPath != "/api/v1/iam" && !strings.HasPrefix(concrPath, "/api/v1/iam/") {
			return nil
		}
		// The invite-activation surface is intentionally a separate PUBLIC
		// mount (pre-auth bootstrap), and the xkms WebAuthn ceremony mount
		// authenticates via the ceremony itself; both are outside the gated
		// utility group.
		if strings.HasPrefix(concrPath, "/api/v1/iam/invites") ||
			strings.Contains(concrPath, "/webauthn") {
			return nil
		}

		key := method + " " + concrPath
		t.Run(key, func(t *testing.T) {
			if iamUtilityPublicRoutes[key] {
				if !isAuthzPublicEndpoint(concrPath) {
					t.Fatalf("route %s is declared public but is not on the authz public allowlist", key)
				}
				return
			}

			wantPerm, known := canonicalIAMUtilityExpectations[key]
			if !known {
				t.Fatalf("route %s has no canonical PEP expectation; add a mapping in authz_pep_mapping.go and an entry to canonicalIAMUtilityExpectations", key)
			}
			resource, action, ok := mapRequestToPermission(concrPath, method)
			if !ok {
				t.Fatalf("mapRequestToPermission(%q, %q) ok=false; route is unmapped and would be default-denied", concrPath, method)
			}
			if got := resource + ":" + action; got != wantPerm {
				t.Errorf("mapRequestToPermission(%q, %q) = %q, want %q", concrPath, method, got, wantPerm)
			}
		})
		return nil
	})
	if walkErr != nil {
		t.Fatalf("chi.Walk error: %v", walkErr)
	}
}

// Under the validator posture (RequireAuthz=true) the root-mounted
// /api/v1/iam utility group is fail-closed through the SAME
// AuthorizeViaValidator gate as the /api/v1 group and /api/v1/tenants: a
// token-less request is 401, a scope denial surfaces as 403 with the mapped
// qrdb-canonical permission consulted, a sufficient scope passes the
// middleware, and the public tenant listing bypasses the gate entirely.
func TestServer_ValidatorEnforcement_IAMUtilityGroup(t *testing.T) {
	t.Run("token-less request is 401", func(t *testing.T) {
		s, _ := newTestServer(t)
		s.SetValidatorAuthzEnforcement(&recordingValidatorAuthorizer{})

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/sessions", nil)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 for token-less request, got %d", w.Code)
		}
	})

	t.Run("scope denial is 403 with canonical permission", func(t *testing.T) {
		s, _ := newTestServer(t)
		authorizer := &recordingValidatorAuthorizer{retErr: &platformtoken.ScopeDeniedError{Required: "audit:query"}}
		s.SetValidatorAuthzEnforcement(authorizer)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=t1", nil)
		req.Header.Set("Authorization", "Bearer tok")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected 403, got %d", w.Code)
		}
		if !authorizer.called {
			t.Fatal("validator authorizer must be consulted for the IAM utility group")
		}
		if authorizer.gotPerm != "audit:query" {
			t.Errorf("requiredPerm = %q, want audit:query", authorizer.gotPerm)
		}
	})

	t.Run("sufficient scope passes middleware", func(t *testing.T) {
		s, _ := newTestServer(t)
		authorizer := &recordingValidatorAuthorizer{
			ret: &platformtoken.AuthorizedContext{
				Subject:  "admin",
				Username: "admin",
				TenantID: "t1",
				Roles:    []string{"admin"},
				Scopes:   []string{"audit:query"},
			},
		}
		s.SetValidatorAuthzEnforcement(authorizer)

		// The audit list handler responds 400/500 (missing params / mock
		// service) but never 401/403, proving the request cleared the gate.
		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/audit?tenant_id=t1", nil)
		req.Header.Set("Authorization", "Bearer tok")
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
			t.Fatalf("middleware blocked an authorized request: %d", w.Code)
		}
		if authorizer.gotPerm != "audit:query" {
			t.Errorf("requiredPerm = %q, want audit:query", authorizer.gotPerm)
		}
	})

	t.Run("public tenant listing bypasses the gate", func(t *testing.T) {
		s, _ := newTestServer(t)
		authorizer := &recordingValidatorAuthorizer{retErr: &platformtoken.ScopeDeniedError{Required: "tenants:read"}}
		s.SetValidatorAuthzEnforcement(authorizer)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/iam/tenants/public", nil)
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, req)

		if w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden {
			t.Fatalf("public tenant listing must not require a token: %d", w.Code)
		}
		if authorizer.called {
			t.Fatal("validator authorizer must NOT be consulted for the public tenant listing")
		}
	})
}

// Under the binding posture (RequireAuthz=false) the /api/v1/iam utility
// group receives the SAME EnforceTokenBinding chain as /api/v1/tenants: a
// DPoP cnf-bound token presented without its proof is rejected 401, and valid
// presentations have their principal injected before the route layer.
func TestServer_TokenBindingEnforcement_IAMUtilityGroup(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetTokenBindingEnforcement(bindingValidator{})

	tests := []struct {
		name        string
		path        string
		authz       string
		dpop        string
		wantBlocked bool
	}{
		{"bound token without proof rejected on sessions list", "/api/v1/iam/sessions", "Bearer bound", "", true},
		{"bound token without proof rejected on audit list", "/api/v1/iam/audit", "Bearer bound", "", true},
		// Pass cases use the audit list: its handler responds 400 (missing
		// tenant_id) rather than 401, proving the middleware passed the
		// request through (the sessions handler 401s on its own without a
		// principal, which would mask the middleware verdict).
		{"bound token with proof passes middleware", "/api/v1/iam/audit", "Bearer bound", "proof", false},
		{"plain bearer passes middleware", "/api/v1/iam/audit", "Bearer human", "", false},
		{"token-less passes through (binding-only, not an auth gate)", "/api/v1/iam/audit", "", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.authz != "" {
				req.Header.Set("Authorization", tc.authz)
			}
			if tc.dpop != "" {
				req.Header.Set("DPoP", tc.dpop)
			}
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			if tc.wantBlocked {
				if w.Code != http.StatusUnauthorized {
					t.Fatalf("expected 401 on %s, got %d", tc.path, w.Code)
				}
			} else if w.Code == http.StatusUnauthorized {
				t.Fatalf("middleware blocked a request it should have passed on %s: %d", tc.path, w.Code)
			}
		})
	}
}
