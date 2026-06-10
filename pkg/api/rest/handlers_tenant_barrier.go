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

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Per-tenant barrier REST handlers
//
// These mirror the qrdb per-tenant barrier REST surface 1:1 and delegate to the
// broker's TenantBarrierServicer facade, which forwards to the embedded qrdb
// node. quicbaas owns no barrier logic. Authorization is enforced by the PEP
// (pepSpecialRules map these paths to the tenant_barrier resource, tenant-
// scoped); qrdb re-applies ScopeTenantOnly beneath. Share material is proxied
// straight through and never logged.
// =============================================================================

// tenantBarrierInitBody is the JSON payload for POST .../barrier/init.
type tenantBarrierInitBody struct {
	Passphrase string `json:"passphrase,omitempty"`
	Threshold  int    `json:"threshold,omitempty"`
	Shares     int    `json:"shares,omitempty"`
	Strategy   string `json:"strategy,omitempty"`
}

// tenantBarrierUnsealBody is the JSON payload for POST .../barrier/unseal.
// Key is base64-encoded on the wire by encoding/json.
type tenantBarrierUnsealBody struct {
	Key []byte `json:"key,omitempty"`
}

// tenantBarrierUnsealShareBody is the JSON payload for POST .../barrier/unseal-share.
type tenantBarrierUnsealShareBody struct {
	Share string `json:"share,omitempty"`
}

// requireTenantBarrierService returns the wired per-tenant barrier facade, or
// reports 503 and returns false when it is not yet available.
func (s *Server) requireTenantBarrierService(w http.ResponseWriter, r *http.Request) (service.TenantBarrierServicer, bool) {
	svc := s.tenantBarrierService()
	if svc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrBarrierServiceUnavailable)
		return nil, false
	}
	return svc, true
}

// HandleTenantBarrierInit handles POST /api/v1/tenants/{tenant_id}/barrier/init.
// For the Shamir strategy it returns 200 with the generated shares; for
// none/passphrase it returns 204 No Content.
func (s *Server) HandleTenantBarrierInit(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	var body tenantBarrierInitBody
	if !readBarrierJSON(w, r, &body) {
		return
	}

	result, err := svc.TenantBarrierInit(r.Context(), tenantID, body.Passphrase, body.Threshold, body.Shares, body.Strategy)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Shamir initialization returns shares that the caller must capture exactly
	// once; surface them in a 200 body. Non-Shamir strategies carry no share
	// material, so they keep the 204 No Content response.
	if result != nil && len(result.Shares) > 0 {
		RespondJSON(w, r, http.StatusOK, result)
		return
	}
	RespondNoContent(w, r)
}

// HandleTenantBarrierUnseal handles POST /api/v1/tenants/{tenant_id}/barrier/unseal.
func (s *Server) HandleTenantBarrierUnseal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	var body tenantBarrierUnsealBody
	if !readBarrierJSON(w, r, &body) {
		return
	}

	if err := svc.TenantBarrierUnseal(r.Context(), tenantID, body.Key); err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondNoContent(w, r)
}

// HandleTenantBarrierUnsealShare handles POST
// /api/v1/tenants/{tenant_id}/barrier/unseal-share — submits one Shamir share
// toward the per-tenant quorum and returns the quorum progress.
func (s *Server) HandleTenantBarrierUnsealShare(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	var body tenantBarrierUnsealShareBody
	if !readBarrierJSON(w, r, &body) {
		return
	}
	if body.Share == "" {
		RespondBadRequest(w, r, "share is required")
		return
	}

	progress, err := svc.TenantBarrierUnsealShare(r.Context(), tenantID, body.Share)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, progress)
}

// HandleTenantBarrierSeal handles POST /api/v1/tenants/{tenant_id}/barrier/seal.
func (s *Server) HandleTenantBarrierSeal(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	if err := svc.TenantBarrierSeal(r.Context(), tenantID); err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondNoContent(w, r)
}

// HandleTenantBarrierStatus handles GET /api/v1/tenants/{tenant_id}/barrier/status.
func (s *Server) HandleTenantBarrierStatus(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.requireTenantBarrierService(w, r)
	if !ok {
		return
	}
	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	status, err := svc.TenantBarrierStatus(r.Context(), tenantID)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, status)
}
