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

// handlers_certificate_extensions.go exposes the three certificate-extension
// verbs — CrossSign, Chain and IssueDeviceIdentity — over REST. They back the
// SDK's Security().CertificateExt() surface and delegate to the same
// CertificateService the rest of /api/v1/certificate uses.

package rest

import (
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Request/Response types
// =============================================================================

// CrossSignRESTRequest is the JSON body for POST /api/v1/certificate/cross-sign.
type CrossSignRESTRequest struct {
	ForeignRootPEM []byte `json:"foreign_root_pem"`
}

// CrossSignRESTResponse is the JSON body returned by cross-signing.
type CrossSignRESTResponse struct {
	CrossSignedPEM []byte `json:"cross_signed_pem"`
	Timestamp      int64  `json:"timestamp"`
}

// CertificateChainRESTRequest is the JSON body for
// POST /api/v1/certificate/chain.
type CertificateChainRESTRequest struct {
	CertificatePEM []byte `json:"certificate_pem"`
}

// CertificateChainRESTResponse carries the resolved issuer chain, ordered
// issuer-first and root-last.
type CertificateChainRESTResponse struct {
	ChainPEM  [][]byte `json:"chain_pem"`
	Timestamp int64    `json:"timestamp"`
}

// DeviceIdentityRESTRequest is the JSON body for
// POST /api/v1/certificate/device-identity.
type DeviceIdentityRESTRequest struct {
	Type           string `json:"type"`
	DeviceID       string `json:"device_id"`
	CSRPEM         []byte `json:"csr_pem,omitempty"`
	EKPub          []byte `json:"ek_pub,omitempty"`
	AKPub          []byte `json:"ak_pub,omitempty"`
	QualifyingData []byte `json:"qualifying_data,omitempty"`
	ValidityDays   int    `json:"validity_days,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
}

// DeviceIdentityRESTResponse is the issued device identity certificate.
type DeviceIdentityRESTResponse struct {
	CertificatePEM []byte    `json:"certificate_pem"`
	ChainPEM       []byte    `json:"chain_pem"`
	SerialNumber   string    `json:"serial_number"`
	NotBefore      time.Time `json:"not_before"`
	NotAfter       time.Time `json:"not_after"`
	Timestamp      int64     `json:"timestamp"`
}

// =============================================================================
// Handlers
// =============================================================================

// HandleCertificateCrossSign handles POST /api/v1/certificate/cross-sign —
// cross-signs an external root certificate with the local CA.
func (s *Server) HandleCertificateCrossSign(w http.ResponseWriter, r *http.Request) {
	var req CrossSignRESTRequest
	if !decodeCertExtBody(w, r, &req) {
		return
	}
	if len(req.ForeignRootPEM) == 0 {
		RespondBadRequest(w, r, "foreign_root_pem is required")
		return
	}

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcResp, err := certService.CrossSign(r.Context(), &service.CrossSignRequest{
		ForeignRootPEM: req.ForeignRootPEM,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &CrossSignRESTResponse{
		CrossSignedPEM: svcResp.CrossSignedPEM,
		Timestamp:      time.Now().Unix(),
	})
}

// HandleCertificateChain handles POST /api/v1/certificate/chain — resolves the
// issuer chain for a certificate by walking its AIA extension.
func (s *Server) HandleCertificateChain(w http.ResponseWriter, r *http.Request) {
	var req CertificateChainRESTRequest
	if !decodeCertExtBody(w, r, &req) {
		return
	}
	if len(req.CertificatePEM) == 0 {
		RespondBadRequest(w, r, "certificate_pem is required")
		return
	}

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcResp, err := certService.Chain(r.Context(), &service.CertificateChainRequest{
		CertificatePEM: req.CertificatePEM,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &CertificateChainRESTResponse{
		ChainPEM:  svcResp.ChainPEM,
		Timestamp: time.Now().Unix(),
	})
}

// HandleCertificateDeviceIdentity handles
// POST /api/v1/certificate/device-identity — issues a TCG device identity
// certificate (EK / AK / IDevID).
func (s *Server) HandleCertificateDeviceIdentity(w http.ResponseWriter, r *http.Request) {
	var req DeviceIdentityRESTRequest
	if !decodeCertExtBody(w, r, &req) {
		return
	}
	if req.Type == "" {
		RespondBadRequest(w, r, "type is required")
		return
	}
	if req.DeviceID == "" {
		RespondBadRequest(w, r, "device_id is required")
		return
	}

	certService := s.service.Certificate()
	if certService == nil {
		RespondError(w, r, http.StatusServiceUnavailable, ErrCertificateServiceUnavailable)
		return
	}

	svcResp, err := certService.IssueDeviceIdentity(r.Context(), &service.IssueDeviceIdentityRequest{
		Type:           req.Type,
		DeviceID:       req.DeviceID,
		CSRPEM:         req.CSRPEM,
		EKPub:          req.EKPub,
		AKPub:          req.AKPub,
		QualifyingData: req.QualifyingData,
		ValidityDays:   req.ValidityDays,
		TenantID:       req.TenantID,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, &DeviceIdentityRESTResponse{
		CertificatePEM: svcResp.CertificatePEM,
		ChainPEM:       svcResp.ChainPEM,
		SerialNumber:   svcResp.SerialNumber,
		NotBefore:      svcResp.NotBefore,
		NotAfter:       svcResp.NotAfter,
		Timestamp:      time.Now().Unix(),
	})
}

// decodeCertExtBody reads and JSON-decodes the request body into out,
// responding with 400 and returning false when the body is unreadable or
// malformed.
func decodeCertExtBody(w http.ResponseWriter, r *http.Request, out interface{}) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return false
	}
	defer r.Body.Close()

	if err := json.Unmarshal(body, out); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return false
	}
	return true
}
