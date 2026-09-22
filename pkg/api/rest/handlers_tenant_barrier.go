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
	stderrors "errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

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

// tenantBarrierEnableEncryptionBody is the JSON payload for
// POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption.
type tenantBarrierEnableEncryptionBody struct {
	// TargetStrategy is the keyed strategy to migrate to ("passphrase" or
	// "shamir"). Required.
	TargetStrategy string `json:"target_strategy"`

	// Mechanism selects the physical re-encryption approach ("swap" or
	// "inplace"). Defaults to "swap" when empty.
	Mechanism string `json:"mechanism,omitempty"`

	// Availability controls write availability during migration ("online"
	// or "quiesced"). Defaults to "online" when empty.
	Availability string `json:"availability,omitempty"`

	// Passphrase is the new passphrase for a passphrase-target migration.
	Passphrase string `json:"passphrase,omitempty"`

	// ShamirThreshold / ShamirTotal configure M-of-N for a Shamir-target
	// migration.
	ShamirThreshold int `json:"shamir_threshold,omitempty"`
	ShamirTotal     int `json:"shamir_total,omitempty"`
}

// handleTenantBarrierError maps per-tenant barrier service errors to their
// canonical REST status. The qrdb error canon maps
// tenant.ErrBarrierNotEnabled (per-tenant barrier verb on a node whose seal
// subsystem is disabled) to 422 Unprocessable Entity; without this check the
// raw sentinel would fall through handleServiceError to a generic 500. The
// error is passed to RespondError verbatim so the canonical message is
// preserved on the wire and the response is marked non-retryable (generic
// errors serialize with retryable=false). Every other error keeps the shared
// handleServiceError classification.
func (s *Server) handleTenantBarrierError(w http.ResponseWriter, r *http.Request, err error) {
	if stderrors.Is(err, qrdbsdk.ErrTenantBarrierNotEnabled) {
		RespondError(w, r, http.StatusUnprocessableEntity, err)
		return
	}
	s.handleServiceError(w, r, err)
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
// For the Shamir strategy it returns 200 with share-free metadata (threshold,
// total shares, sealed-share count); officers claim their shares individually
// via the share-claim flow. For none/passphrase it returns 204 No Content.
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

	passphrase := secretBytes(body.Passphrase)
	defer secure.Wipe(passphrase)
	result, err := svc.TenantBarrierInit(r.Context(), tenantID, passphrase, body.Threshold, body.Shares, body.Strategy)
	if err != nil {
		s.handleTenantBarrierError(w, r, err)
		return
	}

	// Share-free contract: Shamir initialization returns only metadata
	// (threshold, total shares, sealed-share count) in a 200 body; share bytes
	// are sealed to officer enclaves and claimed via the share-claim flow.
	// Non-Shamir strategies carry no share metadata, so they keep the 204 No
	// Content response.
	if result != nil && result.TotalShares > 0 {
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
		s.handleTenantBarrierError(w, r, err)
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
		s.handleTenantBarrierError(w, r, err)
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
		s.handleTenantBarrierError(w, r, err)
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
		s.handleTenantBarrierError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, status)
}

// HandleTenantBarrierEnableEncryption handles
// POST /api/v1/tenants/{tenant_id}/barrier/enable-encryption.
//
// It migrates a specific tenant's dedicated barrier from the none (unencrypted)
// strategy to a keyed strategy in place. The operation is driven through the
// embedded qrdb node (same EncryptionMigrator as the system barrier handler).
func (s *Server) HandleTenantBarrierEnableEncryption(w http.ResponseWriter, r *http.Request) {
	barrierSvc := s.barrierService()
	if barrierSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrBarrierServiceUnavailable)
		return
	}

	tenantID := chi.URLParam(r, "tenant_id")
	if tenantID == "" {
		RespondBadRequest(w, r, "tenant ID is required")
		return
	}

	var body tenantBarrierEnableEncryptionBody
	if !readBarrierJSON(w, r, &body) {
		return
	}
	if body.TargetStrategy == "" {
		RespondBadRequest(w, r, "target_strategy is required")
		return
	}

	mech := body.Mechanism
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := body.Availability
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}

	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(body.TargetStrategy),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: body.ShamirThreshold,
		ShamirTotal:     body.ShamirTotal,
	}
	if body.Passphrase != "" {
		passphrase := secretBytes(body.Passphrase)
		defer secure.Wipe(passphrase)
		creds, credErr := qrdbsdk.CredentialsFor(passphrase)
		if credErr != nil {
			s.handleServiceError(w, r, credErr)
			return
		}
		defer creds.Secret.Destroy()
		opts.Credentials = creds
	}

	if err := barrierSvc.EnableEncryption(r.Context(), tenantID, opts); err != nil {
		s.handleTenantBarrierError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
