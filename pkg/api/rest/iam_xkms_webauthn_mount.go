//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// MountXKMSWebAuthnRoutes registers xkms's native WebAuthn HTTP
// handlers under /api/v1/iam/{tenant_id}/webauthn. The handlers expose
// xkms's canonical surface — /registration/begin|finish|status,
// /login/begin|finish — which uses the X-Session-Id header for
// ceremony correlation and accepts email/display-name bodies.
//
// This is the single tenant-scoped ceremony surface: the legacy
// /api/v1/tenants/{tenant_id}/webauthn endpoints were deleted, and
// browser sign-in rides the unified login ceremony at
// /api/v1/auth/login/{begin,finish}.
//
// The mount is a ROOT chi.Route on the legacy in-process listener, which the
// unified-API-server architecture never starts a listener for
// (pkg/server/lifecycle_rest.go startRESTServer); it carries no
// authorization gate here. Live enforcement for the ENROLLMENT verbs —
// registration/begin, registration/finish — is the module-only GroupIAM
// RouteSpecs (routes_iam.go) served through go-qrdb's unified-server authorizer via BindRoute,
// frozen to their user_authfactors:add permission so an upstream-identity or
// local-platform-token caller without that grant cannot bind a new
// authenticator to another user's account.
//
// The LOGIN ceremony (login/begin, login/finish) is RouteAuthPublic in the
// frozen table: WebAuthn IS the authentication mechanism — the caller holds
// no token until FinishLogin mints one.
//
// ctx is the initialization context used for the WebAuthn manager lookup;
// callers must supply a context tied to server startup rather than
// context.Background(). The route mount is skipped silently when the
// xkms-backed manager is unavailable (the legacy handlers continue to work).
func (h *IAMHandler) MountXKMSWebAuthnRoutes(ctx context.Context, router chi.Router) {
	factory := h.resolveWebAuthnFactory(ctx)
	if factory == nil {
		return
	}
	dispatch := buildXKMSWebAuthnDispatcher(factory)
	router.Route("/api/v1/iam/{tenant_id}/webauthn", func(r chi.Router) {
		r.Post("/registration/begin", dispatch)
		r.Post("/registration/finish", dispatch)
		r.Get("/registration/status", dispatch)
		r.Post("/login/begin", dispatch)
		r.Post("/login/finish", dispatch)
	})
}

// resolveWebAuthnFactory pulls the xkms-backed factory out of the
// service layer when the WebAuthn manager is the multi-tenant
// XKMSWebAuthnManager. Returns nil otherwise (legacy code path or
// uninitialized). ctx is the initialization context used for the manager
// lookup; it should be a short-lived context tied to server startup, not
// context.Background().
func (h *IAMHandler) resolveWebAuthnFactory(ctx context.Context) *iam.WebAuthnServiceFactory {
	if h.server == nil || h.server.service == nil {
		return nil
	}
	getter, ok := h.server.service.(interface {
		GetWebAuthnManager(ctx context.Context) (WebAuthnManager, error)
	})
	if !ok {
		return nil
	}
	mgr, err := getter.GetWebAuthnManager(ctx)
	if err != nil || mgr == nil {
		return nil
	}
	xkmsMgr, ok := mgr.(*iam.XKMSWebAuthnManager)
	if !ok {
		return nil
	}
	return xkmsMgr.Factory()
}

// buildXKMSWebAuthnDispatcher returns an http.HandlerFunc that resolves
// the tenant id from the chi URL parameter, looks up the per-tenant
// xkms WebAuthn service, and dispatches the request to the matching
// xkms HTTP handler. The dispatcher is shared across all five
// ceremony routes; xkms's HandlerFunc internally routes by URL path.
//
// The url-strip approach: chi routes the request with the prefix
// "/api/v1/iam/{tenant_id}/webauthn" already stripped from the path
// pattern but kept in r.URL.Path. We therefore rewrite r.URL.Path to
// the suffix the xkms handler expects (e.g. "/registration/begin")
// before invoking xkms.Handler.HandlerFunc.
func buildXKMSWebAuthnDispatcher(factory *iam.WebAuthnServiceFactory) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := chi.URLParam(r, "tenant_id")
		if tenantID == "" {
			http.Error(w, "tenant_id is required", http.StatusBadRequest)
			return
		}
		svc, err := factory.ServiceFor(r.Context(), tenantID)
		if err != nil {
			http.Error(w, "tenant webauthn service unavailable", http.StatusServiceUnavailable)
			return
		}
		handler := xkmssdk.NewWebAuthnHTTPHandler(svc)

		// Compute the suffix the xkms HandlerFunc expects: strip the
		// "/api/v1/iam/{tenantID}/webauthn" prefix from r.URL.Path.
		prefix := "/api/v1/iam/" + tenantID + "/webauthn"
		stripped := r.URL.Path[len(prefix):]
		if stripped == "" {
			http.NotFound(w, r)
			return
		}

		// Clone the request with the rewritten path so the xkms
		// HandlerFunc routes correctly without mutating shared state.
		newURL := *r.URL
		newURL.Path = stripped
		r2 := r.Clone(r.Context())
		r2.URL = &newURL

		handler.HandlerFunc()(w, r2)
	}
}

// moduleWebAuthnHandler returns an http.HandlerFunc that resolves the
// xkms-native WebAuthn factory PER REQUEST (rather than once at mount time)
// and dispatches to buildXKMSWebAuthnDispatcher. Used by the unified-API-
// server module surface (routes_iam.go): unlike MountXKMSWebAuthnRoutes'
// legacy one-time resolution during the bounded server-construction window,
// this stays reachable even if the xkms-backed manager was not yet available
// when the module registered its routes, and degrades to 503 rather than
// being silently absent when it never becomes available.
func (s *Server) moduleWebAuthnHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		h := s.iamHTTP()
		if h == nil {
			http.Error(w, "iam is not configured", http.StatusServiceUnavailable)
			return
		}
		factory := h.resolveWebAuthnFactory(r.Context())
		if factory == nil {
			http.Error(w, "webauthn is not configured on this tenant", http.StatusServiceUnavailable)
			return
		}
		buildXKMSWebAuthnDispatcher(factory)(w, r)
	}
}
