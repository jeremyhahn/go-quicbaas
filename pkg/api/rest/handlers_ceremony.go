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
	"net/http"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Ceremony REST handlers — /api/v1/init surface.
//
// The QuicBaaS daemon exposes the ceremony lifecycle (M-of-N officer
// enrollment, certificate claims, share distribution, CSR signing) under
// /api/v1/init/* to match the qrdb sealed-window allowlist exactly.
// =============================================================================

// ---------------------------------------------------------------------------
// REST request types (local, unexported — JSON shape only).
// ---------------------------------------------------------------------------

// ceremonyOfficer describes a single officer participating in the ceremony.
type ceremonyOfficer struct {
	Username string `json:"username"`
	CSRPEM   string `json:"csr_pem"`
}

// ceremonyInitializeRequest is the JSON payload for POST /api/v1/init/initialize.
type ceremonyInitializeRequest struct {
	SOPin                  string            `json:"so_pin"`
	UserPin                string            `json:"user_pin"`
	CredentialSealStrategy string            `json:"credential_seal_strategy,omitempty"`
	Threshold              int               `json:"threshold"`
	Officers               []ceremonyOfficer `json:"officers,omitempty"`
	Hostname               string            `json:"hostname,omitempty"`
	SANs                   []string          `json:"sans,omitempty"`
}

// claimCertBeginRequest is the JSON payload for
// POST /api/v1/init/claim-cert/begin.
type claimCertBeginRequest struct {
	Username string `json:"username"`
}

// claimCertCompleteRequest is the JSON payload for
// POST /api/v1/init/claim-cert/complete.
type claimCertCompleteRequest struct {
	Username  string `json:"username"`
	NonceHex  string `json:"nonce_hex"`
	Signature string `json:"signature"` // base64-encoded
}

// claimShareRequest is the JSON payload for POST /api/v1/init/claim-share.
type claimShareRequest struct {
	Username string `json:"username"`
}

// signCSRRequest is the JSON payload for POST /api/v1/init/sign-csr.
type signCSRRequest struct {
	Username string `json:"username"`
	SOPin    string `json:"so_pin"`
	CSRPEM   string `json:"csr_pem"`
	Role     string `json:"role"`
}

// enrollExternalOfficerRequest is the JSON payload for
// POST /api/v1/init/enroll-external-officer.
type enrollExternalOfficerRequest struct {
	Username   string `json:"username"`
	CertPEM    string `json:"cert_pem,omitempty"`
	PubKeyPEM  string `json:"pubkey_pem,omitempty"`
	ShareIndex int    `json:"share_index"`
	AuthFactor string `json:"auth_factor"`
	ShareBytes []byte `json:"share_bytes,omitempty"`
}

// enrollExternalOfficerResponse is the JSON response for
// POST /api/v1/init/enroll-external-officer.
type enrollExternalOfficerResponse struct {
	SealedShare string `json:"sealed_share,omitempty"`
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// requireCeremonyService writes a 503 Service Unavailable response when the
// ceremony facade is unwired. Returns true when the service is present.
func (s *Server) requireCeremonyService(w http.ResponseWriter, r *http.Request) bool {
	if s.ceremonySvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCeremonyServiceUnavailable)
		return false
	}
	return true
}

// ceremonyInitToResponse maps a CeremonyInitInfo to its JSON wire shape.
func ceremonyInitToResponse(info *service.CeremonyInitInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"spki_pin":     info.SPKIPin,
		"ca_cert_pem":  string(info.CACertPEM),
		"tls_cert_pem": string(info.TLSCertPEM),
		"state":        info.State,
		"share_count":  info.ShareCount,
	}
	if len(info.SOCertPEM) > 0 {
		resp["so_cert_pem"] = string(info.SOCertPEM)
	}
	if len(info.SOKeyPEM) > 0 {
		resp["so_key_pem"] = string(info.SOKeyPEM)
	}
	return resp
}

// ceremonyStatusToResponse maps a CeremonyStatusInfo to its JSON shape.
func ceremonyStatusToResponse(info *service.CeremonyStatusInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"state": info.State,
	}
	if info.SPKIPin != "" {
		resp["spki_pin"] = info.SPKIPin
	}
	return resp
}

// ceremonyChallengeToResponse maps a CeremonyChallenge to its JSON shape.
func ceremonyChallengeToResponse(c *service.CeremonyChallenge) map[string]interface{} {
	resp := map[string]interface{}{
		"nonce_hex": c.NonceHex,
	}
	if !c.ExpiresAt.IsZero() {
		resp["expires_at"] = c.ExpiresAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// ceremonyClaimCertToResponse maps a CeremonyClaimCertResult to its JSON shape.
func ceremonyClaimCertToResponse(r *service.CeremonyClaimCertResult) map[string]interface{} {
	return map[string]interface{}{
		"cert_pem":    string(r.CertPEM),
		"ca_cert_pem": string(r.CACertPEM),
	}
}

// ceremonySignCSRToResponse maps a CeremonySignCSRResult to its JSON shape.
func ceremonySignCSRToResponse(r *service.CeremonySignCSRResult) map[string]interface{} {
	return map[string]interface{}{
		"cert_pem": string(r.CertPEM),
	}
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

// HandleCeremonyInitialize handles POST /api/v1/init/initialize.
func (s *Server) HandleCeremonyInitialize(w http.ResponseWriter, r *http.Request) {
	if !s.requireCeremonyService(w, r) {
		return
	}

	var req ceremonyInitializeRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}

	officers := make([]service.CeremonyOfficer, 0, len(req.Officers))
	for _, o := range req.Officers {
		officers = append(officers, service.CeremonyOfficer{
			Username: o.Username,
			CSRPEM:   o.CSRPEM,
		})
	}

	info, err := s.ceremonySvc.CeremonyInitialize(r.Context(), &service.CeremonyInitRequest{
		SOPin:                  req.SOPin,
		UserPin:                req.UserPin,
		CredentialSealStrategy: req.CredentialSealStrategy,
		Threshold:              req.Threshold,
		Officers:               officers,
		Hostname:               req.Hostname,
		SANs:                   req.SANs,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, ceremonyInitToResponse(info))
}

// HandleCeremonyStatus handles GET /api/v1/init/status.
func (s *Server) HandleCeremonyStatus(w http.ResponseWriter, r *http.Request) {
	if !s.requireCeremonyService(w, r) {
		return
	}

	info, err := s.ceremonySvc.CeremonyStatus(r.Context())
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, ceremonyStatusToResponse(info))
}

// HandleCeremonyClaimCertBegin handles POST /api/v1/init/claim-cert/begin.
func (s *Server) HandleCeremonyClaimCertBegin(w http.ResponseWriter, r *http.Request) {
	if !s.requireCeremonyService(w, r) {
		return
	}

	var req claimCertBeginRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Username == "" {
		RespondBadRequest(w, r, "username is required")
		return
	}

	challenge, err := s.ceremonySvc.CeremonyBeginClaimCert(r.Context(), req.Username)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, ceremonyChallengeToResponse(challenge))
}

// HandleCeremonyClaimCertComplete handles POST /api/v1/init/claim-cert/complete.
func (s *Server) HandleCeremonyClaimCertComplete(w http.ResponseWriter, r *http.Request) {
	if !s.requireCeremonyService(w, r) {
		return
	}

	var req claimCertCompleteRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Username == "" {
		RespondBadRequest(w, r, "username is required")
		return
	}
	if req.NonceHex == "" {
		RespondBadRequest(w, r, "nonce_hex is required")
		return
	}
	if req.Signature == "" {
		RespondBadRequest(w, r, "signature is required")
		return
	}

	result, err := s.ceremonySvc.CeremonyCompleteClaimCert(r.Context(), req.Username, req.NonceHex, req.Signature)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, ceremonyClaimCertToResponse(result))
}

// HandleCeremonyClaimShare handles POST /api/v1/init/claim-share.
func (s *Server) HandleCeremonyClaimShare(w http.ResponseWriter, r *http.Request) {
	if !s.requireCeremonyService(w, r) {
		return
	}

	var req claimShareRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Username == "" {
		RespondBadRequest(w, r, "username is required")
		return
	}

	share, err := s.ceremonySvc.CeremonyClaimShare(r.Context(), req.Username)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"share": share,
	})
}

// HandleCeremonySignCSR handles POST /api/v1/init/sign-csr.
func (s *Server) HandleCeremonySignCSR(w http.ResponseWriter, r *http.Request) {
	if !s.requireCeremonyService(w, r) {
		return
	}

	var req signCSRRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Username == "" {
		RespondBadRequest(w, r, "username is required")
		return
	}
	if req.CSRPEM == "" {
		RespondBadRequest(w, r, "csr_pem is required")
		return
	}

	result, err := s.ceremonySvc.CeremonySignCSR(r.Context(), req.Username, req.SOPin, req.CSRPEM, req.Role)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, ceremonySignCSRToResponse(result))
}

// HandleCeremonyListOfficers handles GET /api/v1/init/officers.
//
// Returns the persistent ceremony officer registry shape:
//
//	{ "officers": [ ... ] }
//
// While the registry is empty (no officers enrolled yet) the array is
// returned empty rather than a 404 — the UI lists the empty roster as
// part of its first render and a 404 surfaces as a red error banner.
//
// When the ceremony service is unwired the canonical 503 + typed-error
// envelope is returned via requireCeremonyService so callers can
// distinguish "no officers yet" from "ceremony subsystem offline".
func (s *Server) HandleCeremonyListOfficers(w http.ResponseWriter, r *http.Request) {
	if !s.requireCeremonyService(w, r) {
		return
	}

	// The ceremony service does not (yet) expose a typed ListOfficers
	// method on its facade. Until the persistent ceremony-registry
	// surface lands, return an empty roster — the front-end's
	// CeremonyOfficerList component renders the empty-state placeholder
	// in this case rather than an error banner.
	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"officers": []interface{}{},
	})
}

// HandleCeremonyEnrollExternalOfficer handles
// POST /api/v1/init/enroll-external-officer.
func (s *Server) HandleCeremonyEnrollExternalOfficer(w http.ResponseWriter, r *http.Request) {
	if !s.requireCeremonyService(w, r) {
		return
	}

	var req enrollExternalOfficerRequest
	if !readBarrierJSON(w, r, &req) {
		return
	}
	if req.Username == "" {
		RespondBadRequest(w, r, "username is required")
		return
	}
	if req.CertPEM == "" && req.PubKeyPEM == "" {
		RespondBadRequest(w, r, "exactly one of cert_pem or pubkey_pem is required")
		return
	}
	if req.CertPEM != "" && req.PubKeyPEM != "" {
		RespondBadRequest(w, r, "cert_pem and pubkey_pem are mutually exclusive")
		return
	}
	if req.AuthFactor == "" {
		RespondBadRequest(w, r, "auth_factor is required")
		return
	}

	resp, err := s.ceremonySvc.CeremonyEnrollExternalOfficer(r.Context(), &service.CeremonyEnrollExternalOfficerRequest{
		Username:   req.Username,
		CertPEM:    req.CertPEM,
		PubKeyPEM:  req.PubKeyPEM,
		ShareIndex: req.ShareIndex,
		AuthFactor: req.AuthFactor,
		ShareBytes: req.ShareBytes,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, enrollExternalOfficerResponse{SealedShare: resp.SealedShare})
}
