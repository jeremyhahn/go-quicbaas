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
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Barrier REST handlers — /api/v1/barrier surface.
//
// Mirrors the go-qrdb /v1/barrier surface (handlers_barrier.go in
// pkg/api/rest there) so QuicBaaS exposes an identical contract. Each
// handler short-circuits to 503 Service Unavailable when the barrier
// facade is not yet wired (early-boot or test setups).
// =============================================================================

// ---------------------------------------------------------------------------
// REST request types (local, unexported — JSON shape only).
// ---------------------------------------------------------------------------

// barrierInitializeRequest is the JSON payload for POST /api/v1/barrier/initialize.
type barrierInitializeRequest struct {
	Secret string `json:"secret"`
}

// barrierUnsealRequest is the JSON payload for POST /api/v1/barrier/unseal.
type barrierUnsealRequest struct {
	Secret string `json:"secret"`
}

// barrierShamirInitializeRequest is the JSON payload for
// POST /api/v1/barrier/shamir/initialize.
type barrierShamirInitializeRequest struct {
	Secret      string `json:"secret"`
	Threshold   int    `json:"threshold"`
	TotalShares int    `json:"total_shares"`
}

// barrierShamirUnsealShareRequest is the JSON payload for
// POST /api/v1/barrier/shamir/unseal-share.
type barrierShamirUnsealShareRequest struct {
	Share string `json:"share"`
}

// barrierShamirUnsealSharesRequest is the JSON payload for
// POST /api/v1/barrier/shamir/unseal-shares.
type barrierShamirUnsealSharesRequest struct {
	Shares []string `json:"shares"`
}

// barrierRekeyRequest is the JSON payload for POST /api/v1/barrier/rekey.
type barrierRekeyRequest struct {
	Threshold int `json:"threshold"`
	Total     int `json:"total"`
}

// barrierRecoveryGenerateRequest is the JSON payload for
// POST /api/v1/barrier/recovery/generate.
type barrierRecoveryGenerateRequest struct {
	Threshold int `json:"threshold"`
	Total     int `json:"total"`
}

// barrierRecoveryRecoverRequest is the JSON payload for
// POST /api/v1/barrier/recovery/recover.
type barrierRecoveryRecoverRequest struct {
	Keys []string `json:"keys"`
}

// barrierRootTokenRequest is the JSON payload for
// POST /api/v1/barrier/root-token.
type barrierRootTokenRequest struct {
	Shares []string `json:"shares"`
}

// barrierEnableEncryptionRequest is the JSON payload for
// POST /api/v1/barrier/enable-encryption.
type barrierEnableEncryptionRequest struct {
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
	// Ignored for Shamir targets.
	Passphrase string `json:"passphrase,omitempty"`

	// ShamirThreshold / ShamirTotal configure the M-of-N scheme for a
	// Shamir-target migration. Ignored for passphrase targets.
	ShamirThreshold int `json:"shamir_threshold,omitempty"`
	ShamirTotal     int `json:"shamir_total,omitempty"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requireBarrierService writes a 503 Service Unavailable response when the
// barrier facade is unwired. Returns true when the service is present.
func (s *Server) requireBarrierService(w http.ResponseWriter, r *http.Request) bool {
	if s.barrierService() == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrBarrierServiceUnavailable)
		return false
	}
	return true
}

// readBarrierJSON decodes a JSON request body into the supplied destination.
// It returns false (after writing a 400 response) if the body cannot be
// decoded or read. EOF is treated as a valid empty body for endpoints with
// optional payloads — callers can supply a nil dst when no body is expected.
func readBarrierJSON(w http.ResponseWriter, r *http.Request, dst interface{}) bool {
	if dst == nil {
		return true
	}
	body, err := io.ReadAll(r.Body)
	if err != nil && !errors.Is(err, io.EOF) {
		RespondBadRequest(w, r, "failed to read request body")
		return false
	}
	defer r.Body.Close()

	if len(body) == 0 {
		return true
	}
	if err := json.Unmarshal(body, dst); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return false
	}
	return true
}

// barrierStatusToResponse maps a BarrierStatusInfo to its JSON wire shape.
func barrierStatusToResponse(info *service.BarrierStatusInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"sealed":          info.Sealed,
		"strategy":        info.Strategy,
		"hardware_backed": info.HardwareBacked,
	}
	if !info.InitializedAt.IsZero() {
		resp["initialized_at"] = info.InitializedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// shamirInitToResponse maps a ShamirInitInfo to its JSON wire shape. Share
// material is never present; officers claim shares via the qrdb share-claim
// flow.
func shamirInitToResponse(info *service.ShamirInitInfo) map[string]interface{} {
	return map[string]interface{}{
		"threshold":    info.Threshold,
		"total_shares": info.TotalShares,
		"share_count":  info.ShareCount,
	}
}

// unsealShareProgressToResponse maps an UnsealShareProgress to its JSON shape.
func unsealShareProgressToResponse(p *service.UnsealShareProgress) map[string]interface{} {
	return map[string]interface{}{
		"required":  p.Required,
		"submitted": p.Submitted,
		"complete":  p.Complete,
	}
}

// recoveryKeysToResponse maps a RecoveryKeysInfo to its JSON shape. Recovery
// share material is never present; officers claim shares via the qrdb
// recovery share-claim flow.
func recoveryKeysToResponse(info *service.RecoveryKeysInfo) map[string]interface{} {
	return map[string]interface{}{
		"threshold":   info.Threshold,
		"total":       info.Total,
		"share_count": info.ShareCount,
	}
}

// rootTokenToResponse maps a RootTokenInfo to its JSON shape.
func rootTokenToResponse(info *service.RootTokenInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"token": info.Token,
	}
	if !info.CreatedAt.IsZero() {
		resp["created_at"] = info.CreatedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// HandleBarrierStatus handles GET /api/v1/barrier/status.
func (s *Server) HandleBarrierStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	info, err := s.barrierSvc.BarrierStatus(r.Context())
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, barrierStatusToResponse(info))
}

// HandleBarrierInitialize handles POST /api/v1/barrier/initialize.
func (s *Server) HandleBarrierInitialize(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierInitializeRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}

	if err := s.barrierSvc.BarrierInitialize(r.Context(), req.Secret); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierUnseal handles POST /api/v1/barrier/unseal.
func (s *Server) HandleBarrierUnseal(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierUnsealRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}

	if err := s.barrierSvc.BarrierUnseal(r.Context(), req.Secret); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierSeal handles POST /api/v1/barrier/seal.
func (s *Server) HandleBarrierSeal(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	if err := s.barrierSvc.BarrierSeal(r.Context()); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierShamirInitialize handles POST /api/v1/barrier/shamir/initialize.
func (s *Server) HandleBarrierShamirInitialize(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierShamirInitializeRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Threshold <= 0 {
		RespondBadRequest(w, r, "threshold must be greater than 0")
		return
	}
	if req.TotalShares <= 0 {
		RespondBadRequest(w, r, "total_shares must be greater than 0")
		return
	}
	if req.Threshold > req.TotalShares {
		RespondBadRequest(w, r, "threshold cannot exceed total_shares")
		return
	}

	info, err := s.barrierSvc.BarrierInitializeShamir(r.Context(), req.Secret, req.Threshold, req.TotalShares)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, shamirInitToResponse(info))
}

// HandleBarrierShamirUnsealShare handles POST /api/v1/barrier/shamir/unseal-share.
func (s *Server) HandleBarrierShamirUnsealShare(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierShamirUnsealShareRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Share == "" {
		RespondBadRequest(w, r, "share is required")
		return
	}

	progress, err := s.barrierSvc.BarrierUnsealWithShare(r.Context(), req.Share)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, unsealShareProgressToResponse(progress))
}

// HandleBarrierShamirUnsealShares handles POST /api/v1/barrier/shamir/unseal-shares.
func (s *Server) HandleBarrierShamirUnsealShares(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierShamirUnsealSharesRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if len(req.Shares) == 0 {
		RespondBadRequest(w, r, "shares is required")
		return
	}

	if err := s.barrierSvc.BarrierUnsealWithShares(r.Context(), req.Shares); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierRekey handles POST /api/v1/barrier/rekey.
func (s *Server) HandleBarrierRekey(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierRekeyRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Threshold <= 0 {
		RespondBadRequest(w, r, "threshold must be greater than 0")
		return
	}
	if req.Total <= 0 {
		RespondBadRequest(w, r, "total must be greater than 0")
		return
	}
	if req.Threshold > req.Total {
		RespondBadRequest(w, r, "threshold cannot exceed total")
		return
	}

	info, err := s.barrierSvc.BarrierRekey(r.Context(), req.Threshold, req.Total)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, shamirInitToResponse(info))
}

// HandleBarrierRecoveryGenerate handles POST /api/v1/barrier/recovery/generate.
func (s *Server) HandleBarrierRecoveryGenerate(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierRecoveryGenerateRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Threshold <= 0 {
		RespondBadRequest(w, r, "threshold must be greater than 0")
		return
	}
	if req.Total <= 0 {
		RespondBadRequest(w, r, "total must be greater than 0")
		return
	}
	if req.Threshold > req.Total {
		RespondBadRequest(w, r, "threshold cannot exceed total")
		return
	}

	info, err := s.barrierSvc.BarrierGenerateRecoveryKeys(r.Context(), req.Threshold, req.Total)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, recoveryKeysToResponse(info))
}

// HandleBarrierRecoveryRecover handles POST /api/v1/barrier/recovery/recover.
func (s *Server) HandleBarrierRecoveryRecover(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierRecoveryRecoverRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if len(req.Keys) == 0 {
		RespondBadRequest(w, r, "keys is required")
		return
	}

	if err := s.barrierSvc.BarrierRecoverWithKeys(r.Context(), req.Keys); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierRecoveryDelete handles DELETE /api/v1/barrier/recovery.
func (s *Server) HandleBarrierRecoveryDelete(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	if err := s.barrierSvc.BarrierDeleteRecoveryKeys(r.Context()); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// HandleBarrierRecoveryStatus handles GET /api/v1/barrier/recovery/status.
func (s *Server) HandleBarrierRecoveryStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	hasKeys, err := s.barrierSvc.BarrierHasRecoveryKeys(r.Context())
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"has_recovery_keys": hasKeys,
	})
}

// HandleBarrierRootToken handles POST /api/v1/barrier/root-token.
func (s *Server) HandleBarrierRootToken(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierRootTokenRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if len(req.Shares) == 0 {
		RespondBadRequest(w, r, "shares is required")
		return
	}

	info, err := s.barrierSvc.BarrierGenerateRootToken(r.Context(), req.Shares)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, rootTokenToResponse(info))
}

// HandleBarrierEnableEncryption handles
// POST /api/v1/barrier/enable-encryption.
//
// It migrates the system barrier from the none (unencrypted) strategy to a
// keyed strategy in place. The operation is driven through the embedded qrdb
// node's EncryptionMigrator (not the transport BarrierClient, which stubs
// this operation). Callers must supply the target strategy, mechanism, and
// availability; a passphrase or Shamir parameters are required for the
// respective target strategy.
func (s *Server) HandleBarrierEnableEncryption(w http.ResponseWriter, r *http.Request) {
	if !s.requireBarrierService(w, r) {
		return
	}

	var req barrierEnableEncryptionRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.TargetStrategy == "" {
		RespondBadRequest(w, r, "target_strategy is required")
		return
	}

	mech := req.Mechanism
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := req.Availability
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}

	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(req.TargetStrategy),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: req.ShamirThreshold,
		ShamirTotal:     req.ShamirTotal,
	}
	if req.Passphrase != "" {
		opts.Credentials = qrdbsdk.Credentials{Secret: []byte(req.Passphrase)}
	}

	if err := s.barrierSvc.EnableEncryption(r.Context(), "", opts); err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
