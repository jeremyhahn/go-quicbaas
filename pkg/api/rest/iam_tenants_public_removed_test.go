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

// removedPublicTenantListingPath is the anonymous tenant-enumeration endpoint
// that used to serve every tenant's id + name to any caller that loaded the
// login page. It was deleted outright: the login UI takes the organization id
// as free text instead. These tests are the regression guard against it (or any
// equivalent) coming back.
const removedPublicTenantListingPath = "/api/v1/iam/tenants/public"

// TestPublicTenantListing_RouteDeleted proves the route is no longer registered
// on the router at all — a walk of every live chi route must not name it.
func TestPublicTenantListing_RouteDeleted(t *testing.T) {
	s, _ := newTestServer(t)

	walkErr := chi.Walk(s.router, func(method, route string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if strings.TrimSuffix(route, "/") == removedPublicTenantListingPath {
			t.Errorf("%s %s is still registered; anonymous tenant enumeration must stay deleted", method, route)
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("chi.Walk error: %v", walkErr)
	}
}

// TestPublicTenantListing_NotOnAnyAllowlist proves no pre-auth allowlist still
// admits the path. The two literal slices and the tenant-scoped route templates
// are the complete set of ways a path can bypass an authz gate, so scanning all
// three is a total guard rather than a spot check.
func TestPublicTenantListing_NotOnAnyAllowlist(t *testing.T) {
	if isAuthzPublicEndpoint(removedPublicTenantListingPath) {
		t.Errorf("%s is still on the per-group authz allowlist", removedPublicTenantListingPath)
	}
	if isDefaultDenyPublic(removedPublicTenantListingPath) {
		t.Errorf("%s is still on the default-deny allowlist", removedPublicTenantListingPath)
	}
	if isPreAuthRoute(removedPublicTenantListingPath) {
		t.Errorf("%s is still matched by a pre-auth route template", removedPublicTenantListingPath)
	}

	// Scan the raw allowlist tables too: a shorter prefix entry (say
	// "/api/v1/iam/tenants") would re-open the hole without the predicates above
	// naming the exact path.
	for _, entry := range publicEndpoints {
		if strings.Contains(entry, "tenants") {
			t.Errorf("publicEndpoints entry %q re-opens a pre-auth tenant surface", entry)
		}
	}
	for _, entry := range defaultDenyPublicPrefixes {
		if strings.Contains(entry, "tenants/public") {
			t.Errorf("defaultDenyPublicPrefixes entry %q re-opens anonymous tenant enumeration", entry)
		}
	}
	for _, tmpl := range preAuthRouteTemplates {
		if strings.HasSuffix(tmpl, "/tenants/public") {
			t.Errorf("preAuthRouteTemplates entry %q re-opens anonymous tenant enumeration", tmpl)
		}
	}
}

// TestPublicTenantListing_AnonymousRequestNeverServesTenants drives the real
// router under all three enforcement postures. The status differs by posture
// (an ungated server 404s, a gated one 401s before routing), but the invariant
// the security fix depends on is the same everywhere: an anonymous caller never
// receives 200 and never receives a tenant list.
func TestPublicTenantListing_AnonymousRequestNeverServesTenants(t *testing.T) {
	tests := []struct {
		name       string
		enforce    func(*Server)
		wantStatus int
	}{
		{
			name:       "no enforcement posture",
			enforce:    func(*Server) {},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "validator enforcement posture",
			enforce: func(s *Server) {
				s.SetValidatorAuthzEnforcement(&recordingValidatorAuthorizer{
					retErr: &platformtoken.ScopeDeniedError{Required: "tenants:list"},
				})
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "default-deny posture",
			enforce: func(s *Server) {
				s.SetDefaultDenyEnforcement(&mockDPoPValidator{err: &InvalidTokenResultError{}})
			},
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(t)
			tc.enforce(s)

			req := httptest.NewRequest(http.MethodGet, removedPublicTenantListingPath, nil)
			w := httptest.NewRecorder()
			s.router.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d (body: %s)", w.Code, tc.wantStatus, w.Body.String())
			}
			if w.Code == http.StatusOK {
				t.Fatalf("anonymous caller was served the deleted tenant listing: %s", w.Body.String())
			}
			if strings.Contains(w.Body.String(), `"tenants"`) {
				t.Fatalf("response body leaked a tenant list: %s", w.Body.String())
			}
		})
	}
}

// TestPublicTenantListing_BearerTokenDoesNotResurrectIt proves the deletion is
// unconditional rather than merely un-allowlisted: even a fully authorized
// caller gets nothing, because no handler exists behind the path.
func TestPublicTenantListing_BearerTokenDoesNotResurrectIt(t *testing.T) {
	s, _ := newTestServer(t)
	s.SetValidatorAuthzEnforcement(&recordingValidatorAuthorizer{
		ret: &platformtoken.AuthorizedContext{
			Subject:  "admin",
			Username: "admin",
			TenantID: "system",
			Roles:    []string{"admin"},
			Scopes:   []string{"tenants:list"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, removedPublicTenantListingPath, nil)
	req.Header.Set("Authorization", "Bearer tok")
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (body: %s)", w.Code, w.Body.String())
	}
}
