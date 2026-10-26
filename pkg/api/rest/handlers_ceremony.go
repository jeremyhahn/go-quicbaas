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

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

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

// signCSRRequest is the JSON payload for POST /api/v1/init/sign-csr.
type signCSRRequest struct {
	Username string `json:"username"`
	SOPin    string `json:"so_pin"`
	CSRPEM   string `json:"csr_pem"`
	Role     string `json:"role"`
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
	addCeremonyCredentials(resp, info)
	return resp
}

// addCeremonyCredentials adds the issued holder certificates and the
// bootstrap admin credential to resp, under go-qrdb's JSON names. qrdb returns
// a generated admin key exactly once, so dropping it here would lose it.
func addCeremonyCredentials(resp map[string]interface{}, info *service.CeremonyInitInfo) {
	if len(info.Holders) > 0 {
		resp["holders"] = info.Holders
	}
	if len(info.AdminCertPEM) > 0 {
		resp["admin_cert_pem"] = string(info.AdminCertPEM)
	}
	if len(info.AdminKeyPEM) > 0 {
		resp["admin_key_pem"] = info.AdminKeyPEM
	}
	if info.AdminCredentialSource != "" {
		resp["admin_credential_source"] = info.AdminCredentialSource
	}
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

	// The payload is go-qrdb's own request type, so every officer and policy
	// field reaches qrdb without narrowing.
	var req transport.CeremonyInitializeRequest
	defer wipeCredential(&req.Secret)()
	if !readBarrierJSON(w, r, &req) {
		return
	}

	secret, err := service.NewCeremonySecret(r.Context(), req.Secret)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	if secret != nil {
		defer secret.Destroy()
	}

	info, err := s.ceremonySvc.CeremonyInitialize(holderContext(r), &service.CeremonyInitRequest{
		SOPin:                  req.SOPin,
		UserPin:                req.UserPin,
		CredentialSealStrategy: req.CredentialSealStrategy,
		Threshold:              req.Threshold,
		Policy:                 req.Policy,
		Officers:               req.Officers,
		Hostname:               req.Hostname,
		SANs:                   req.SANs,
		AdminCSRPEM:            req.AdminCSRPEM,
		AdminCertPEM:           req.AdminCertPEM,
		AdminCertPoolPEM:       req.AdminCertPoolPEM,
		AdminGenerate:          req.AdminGenerate,
		Secret:                 secret,
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

	info, err := s.ceremonySvc.CeremonyStatus(holderContext(r))
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, ceremonyStatusToResponse(info))
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

	result, err := s.ceremonySvc.CeremonySignCSR(holderContext(r), req.Username, req.SOPin, req.CSRPEM, req.Role)
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
