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
	"net/http"
	"strings"
	"sync/atomic"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
)

// identityGatedPermission is go-qrdb's identity-gated sentinel:
// authentication is required but authorization is deferred to the handler.
const identityGatedPermission = qrdbsdk.PermissionIdentityGated

// RegisterHTTP registers the full quicbaas REST surface (pkg/api/rest
// Routes(), the same table the legacy router serves) onto the unified
// server's permission-recording registrar:
//
//   - public routes register via HandlePublic;
//   - identity-gated routes (no mapped PEP permission) register under the
//     identity-gated sentinel (auth required, authorization deferred);
//   - permission routes register under their qrdb-canonical
//     resource:action, enforced by qrdb's authorization layer.
//
// Handlers late-bind through the REST resolver exactly like the gRPC
// surface: until the REST wiring composes, every route answers 503. A route
// whose backing subsystem is not configured on the resolved server answers
// 501. Routes that appear more than once in the table (availability
// variants) register once, choosing the enabled variant at request time.
//
// Trailing-slash collection routes additionally register a bare alias
// (e.g. /api/v1/parameters next to /api/v1/parameters/): the legacy chi
// mounts matched the bare path by dispatching it as "/", and the unified
// server's flat registrar needs the alias spelled out to preserve that
// contract.
func (m *Module) RegisterHTTP(r *qrdbsdk.ModuleHTTPRegistrar) {
	if m.resolveREST == nil {
		return
	}

	type routeKey struct{ method, pattern string }
	variants := map[routeKey][]restapi.RouteSpec{}
	var order []routeKey
	// ModuleRoutes (not Routes) -- /api/v1/barrier/* and /api/v1/init/* are
	// excluded from the module surface (see routes.go moduleExcludedPathPrefixes):
	// on a composed node the qrdb node / xkms module already own that surface.
	for _, spec := range restapi.ModuleRoutes() {
		key := routeKey{spec.Method, spec.Pattern}
		if _, seen := variants[key]; !seen {
			order = append(order, key)
		}
		variants[key] = append(variants[key], spec)
	}

	register := func(method, pattern string, lead restapi.RouteSpec, h http.HandlerFunc) {
		switch lead.Auth {
		case restapi.RouteAuthPublic:
			r.HandlePublic(method, pattern, h)
		case restapi.RouteAuthSession:
			r.Handle(method, pattern, identityGatedPermission, h)
		default:
			r.Handle(method, pattern,
				qrdbsdk.Permission(lead.Resource+":"+lead.Action), h)
		}
	}

	for _, key := range order {
		specs := variants[key]
		lead := specs[0]
		h := m.lazyHTTPHandler(specs)
		register(key.method, key.pattern, lead, h)

		// Bare alias for trailing-slash collection routes, unless the
		// table carries a distinct bare route.
		if bare := strings.TrimSuffix(key.pattern, "/"); bare != key.pattern && bare != "" {
			if _, taken := variants[routeKey{key.method, bare}]; !taken {
				register(key.method, bare, lead, h)
			}
		}
	}
}

// Compile-time proof the module implements qrdb's authentication fallback:
// the unified server consults it when its own authenticators produce no
// identity for a request on one of the module's non-public routes.
var _ qrdbsdk.ModuleAuthFallback = (*Module)(nil)

// AuthenticateHTTP authenticates a request with quicbaas's platform-token
// (OIDC JWT/DPoP) session-vehicle chain (Server.AuthenticateHTTP,
// pkg/api/rest/auth_support.go). go-qrdb's REST middleware invokes it only
// after its own authenticators found no identity; the returned identity is
// finalized and authorized by qrdb's single enrichment + authorizer path.
// While the REST engine is still composing, authentication fails closed.
func (m *Module) AuthenticateHTTP(r *http.Request) (*qrdbsdk.Identity, error) {
	if m.resolveREST == nil {
		return nil, &SurfaceComposingError{Surface: "authentication"}
	}
	srv := m.resolveREST()
	if srv == nil {
		return nil, &SurfaceComposingError{Surface: "authentication"}
	}
	return srv.AuthenticateHTTP(r)
}

// SurfaceComposingError reports a module surface consulted before the
// backing engine finished composing. Authentication fails closed on it.
type SurfaceComposingError struct {
	// Surface names the consulted surface.
	Surface string
}

// Error describes the composing state.
func (e *SurfaceComposingError) Error() string {
	return "quicbaas module: " + e.Surface + " is not yet available (server still composing)"
}

// lazyHTTPHandler returns a handler that resolves the REST surface on first
// use, memoizing the bound handler chain (group middleware + handler) once
// the enabled variant is known. Lock-free: concurrent first calls may both
// bind, and the CompareAndSwap keeps exactly one.
func (m *Module) lazyHTTPHandler(specs []restapi.RouteSpec) http.HandlerFunc {
	var bound atomic.Pointer[http.Handler]
	return func(w http.ResponseWriter, req *http.Request) {
		if h := bound.Load(); h != nil {
			(*h).ServeHTTP(w, req)
			return
		}
		srv := m.resolveREST()
		if srv == nil {
			http.Error(w, "quicbaas rest surface is not yet available (server still composing)",
				http.StatusServiceUnavailable)
			return
		}
		var chosen *restapi.RouteSpec
		for i := range specs {
			if srv.RouteEnabled(specs[i]) {
				chosen = &specs[i]
				break
			}
		}
		if chosen == nil {
			http.Error(w, "quicbaas subsystem for this route is not configured on this server",
				http.StatusNotImplemented)
			return
		}
		h := srv.BindRoute(*chosen)
		if !bound.CompareAndSwap(nil, &h) {
			h = *bound.Load()
		}
		h.ServeHTTP(w, req)
	}
}
