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
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fleetCertificateRequest is the body of POST /api/v1/fleet/certificates.
type fleetCertificateRequest struct {
	TenantID     string `json:"tenant_id,omitempty"`
	CSRPEM       []byte `json:"csr_pem"`
	ValidityDays int    `json:"validity_days,omitempty"`
}

// fleetCertificateResponse carries only public material: the key stayed
// with whoever built the CSR.
type fleetCertificateResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	CAPEM          []byte `json:"ca_pem"`
}

// HandleSignFleetNodeCSR signs a fleet node's CSR as an enrollable agent
// certificate. It is the issuance route for `quicbaas fleet deploy` run off
// the server host: the deploy holds the agent key and sends only the CSR.
//
// Unlike /api/v1/xkms/ca/sign, the certificate carries the agent SPIFFE kind
// under the caller's tenant and is recorded for fleet enrolment, so the route
// requires orchestrator:agent_register — the permission enrolment itself
// needs — and signs only for the caller's own tenant.
func (s *Server) HandleSignFleetNodeCSR(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, defaultRegisterRequestMaxBytes))
	if err != nil {
		respondFleetEnrollError(w, r, http.StatusBadRequest, "invalid_request", "request body too large or unreadable")
		return
	}
	var req fleetCertificateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		respondFleetEnrollError(w, r, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	tenantID, err := tenantctx.Target(r.Context(), "rest.SignFleetNodeCSR", req.TenantID)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	ca := s.fleetCertificateAuthority()
	records := s.fleetNodeCertRecordsOrNil()
	if ca == nil || records == nil {
		respondFleetEnrollError(w, r, http.StatusServiceUnavailable, "temporarily_unavailable", "fleet certificate authority not available")
		return
	}
	issuer, err := service.NewXKMSFleetNodeCertIssuer(ca, records)
	if err != nil {
		RespondInternalError(w, r, err)
		return
	}

	signed, err := issuer.SignFleetNodeCSR(r.Context(), &service.FleetNodeCSRRequest{
		TenantID:          tenantID,
		CSRPEM:            req.CSRPEM,
		ValidityDays:      req.ValidityDays,
		SPIFFETrustDomain: s.config.SPIFFETrustDomain,
	})
	if err != nil {
		respondFleetCSRError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, &fleetCertificateResponse{
		CertificatePEM: signed.CertPEM,
		CAPEM:          signed.CAPEM,
	})
}

// respondFleetCSRError maps a SignFleetNodeCSR refusal: a bad CSR is the
// caller's (400), a server with no trust domain cannot mint an agent
// certificate (503), and anything else is the CA's (500).
func respondFleetCSRError(w http.ResponseWriter, r *http.Request, err error) {
	var fieldErr *service.FleetNodeCertRequestFieldError
	var noTrustDomain *service.FleetNodeCertNoTrustDomainError
	var csrErr *service.FleetNodeCSRInvalidError
	switch {
	case errors.As(err, &fieldErr), errors.As(err, &csrErr):
		respondFleetEnrollError(w, r, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.As(err, &noTrustDomain):
		respondFleetEnrollError(w, r, http.StatusServiceUnavailable, "temporarily_unavailable", err.Error())
	default:
		RespondInternalError(w, r, err)
	}
}
