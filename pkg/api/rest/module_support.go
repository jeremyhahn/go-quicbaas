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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
)

// BindRoute binds a RouteSpec's handler on s with the middleware profile its
// RouteGroup carries on the legacy router — the per-group request body limit
// — plus the global per-client-IP rate limit, but WITHOUT a per-route RBAC decorator: on the unified API server,
// go-qrdb's own middleware authenticates AND authorizes every non-public
// route before this handler ever runs — its own mTLS chain first, then, for
// a quicbaas platform-token caller qrdb's own authenticators do not
// understand, the module's AuthenticateHTTP fallback (pkg/apimodule
// module_http.go, backed by Server.AuthenticateHTTP below) — and the ONE
// qrdb authorizer renders the decision at the registration site
// (pkg/apimodule RegisterHTTP). Re-running spec's Resource:Action here would
// double-authorize the call against a different decision point.
func (s *Server) BindRoute(spec RouteSpec) http.Handler {
	var h http.Handler = spec.Handler(s)

	switch spec.Group {
	case GroupAPI:
		h = BodyLimitMiddleware(s.config.MaxBodySize)(h)
	case GroupAPIUpload:
		h = BodyLimitMiddleware(s.config.MaxUploadBodySize)(h)
	}

	h = s.projectQRDBIdentity(h)

	// The global limiter: the SAME instance setupRouter installs, so a
	// client's budget spans core and module routes rather than doubling
	// (#158). The unified server does not run setupRouter's chain, so the
	// client address is resolved here too, honouring TrustedProxies the
	// way the core routes do (#156); the limiter keys its buckets on it.
	if s.rateLimit != nil {
		h = s.rateLimit(h)
	}
	return ClientIPMiddleware(s.clientIP)(h)
}

// RouteEnabled reports whether the route's backing subsystem is configured
// on s.
func (s *Server) RouteEnabled(spec RouteSpec) bool {
	return spec.Enabled == nil || spec.Enabled(s)
}

// projectQRDBIdentity projects a qrdb identity already authenticated AND
// authorized by go-qrdb's unified-server REST middleware into the quicbaas
// principal contract (interfaces.Subject, and — via injectPrincipal —
// GetUserID/GetUserRoles/GetUsername and the tenant context) so every
// handler observes the SAME caller qrdb already admitted, regardless of
// whether qrdb resolved the identity through its own mTLS chain or through
// the module's AuthenticateHTTP fallback (a quicbaas platform-token
// caller).
//
// A request reaching BindRoute WITHOUT a qrdb identity in context is a
// RouteAuthPublic route: go-qrdb attaches a module's fallback only to the
// non-public routes it registers via Handle (server.go's routeFn wrapper),
// never to HandlePublic routes, so a public route's identity is always nil
// here. Such a request passes through with no principal — the historical
// anonymous contract those surfaces already had.
//
// The AuthorizedContext injectPrincipal optionally carries is always nil
// here: qrdb's own authorization decision already ran (or, for
// RouteAuthPublic, none was required), so the small set of handlers that
// consult AuthorizedContextFromRequest as a TenantID/Subject fallback
// (auth_support.go) see the SAME nil they already tolerate for an upstream
// mTLS-authenticated qrdb identity today — this is not a new code path,
// only the single path every caller now takes.
func (s *Server) projectQRDBIdentity(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity := qrdbsdk.IdentityFromContext(r.Context())
		if identity == nil {
			next.ServeHTTP(w, r)
			return
		}
		// The identity came from qrdbsdk.IdentityFromContext — qrdb already
		// verified it — so the projected principal is authenticated.
		// Setting the flag matches the gRPC authz interceptor
		// (contextWithAuthorizedSubject) and the local-token middleware,
		// and is required by every handler that gates on
		// subject.Authenticated (requireSubject, and the impersonation
		// actor resolution in AdminManager.StartImpersonation — #383).
		// auth.SubjectFromIdentity also records whether the subject names a
		// user ID or a username, which an mTLS caller and a bearer caller
		// cannot otherwise be told apart by.
		subject := auth.SubjectFromIdentity(identity)
		if !s.admitTenant(w, r, subject.TenantID) {
			return
		}
		next.ServeHTTP(w, r.WithContext(injectPrincipal(r.Context(), subject, nil)))
	})
}
