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
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// removedPublicTenantListingPath is the anonymous tenant-enumeration endpoint
// that used to serve every tenant's id + name to any caller that loaded the
// login page. It was deleted outright: the login UI takes the organization id
// as free text instead. This test is the regression guard against it (or any
// equivalent) coming back.
//
// The former companion tests that scanned the per-request authz allowlists
// (isAuthzPublicEndpoint / isDefaultDenyPublic / isPreAuthRoute) for the path
// no longer apply: those allowlists were deleted with the legacy per-request
// middleware chain (see the provenance comment in
// routes_permissions_generated.go). The equivalent guard now is structural —
// a route absent from Routes() can never acquire a frozen permission entry
// (applyFrozenPermissions panics on a rawRouteSpecs entry with none), and
// TestRoutes_SurfaceGuard / TestRoutes_FrozenPermissionsGolden (routes_test.go)
// pin the table's exact size and contents, so a reintroduced route changes
// both counts and fails loudly.
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

	// Also confirm the module dispatch surface (Routes(), consumed by
	// go-qrdb's per-route registration) never carries the path either.
	for _, spec := range Routes() {
		if strings.TrimSuffix(spec.Pattern, "/") == removedPublicTenantListingPath {
			t.Errorf("%s %s is still in the module route table; anonymous tenant enumeration must stay deleted",
				spec.Method, spec.Pattern)
		}
	}
}
