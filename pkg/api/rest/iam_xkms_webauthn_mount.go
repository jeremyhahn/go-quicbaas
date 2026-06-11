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
// This complements the legacy /api/v1/tenants/{tenant_id}/webauthn
// endpoints (handled by IAMHandler) which were wired in pre-Phase-5
// quicbaas. Both paths route through the same per-tenant xkms service;
// the new path is the recommended canonical surface for tenant-scoped
// FIDO2 ceremonies.
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
