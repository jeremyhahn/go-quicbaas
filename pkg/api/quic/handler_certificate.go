//go:build quic

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

package quic

import (
	"context"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Certificate Operations Handlers
// =============================================================================

// handleCertIssue handles certificate issuance requests.
func (h *Handler) handleCertIssue(ctx context.Context, req *Request) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var issueReq service.IssueCertificateRequest
	if err := DecodePayload(req.Payload, &issueReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := certService.IssueCertificate(ctx, &issueReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleCertSignCSR handles certificate signing request (CSR) operations.
func (h *Handler) handleCertSignCSR(ctx context.Context, req *Request) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var signReq service.SignCSRRequest
	if err := DecodePayload(req.Payload, &signReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := certService.SignCSR(ctx, &signReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleCertRevoke handles certificate revocation requests.
func (h *Handler) handleCertRevoke(ctx context.Context, req *Request) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var revokeReq service.RevokeCertificateRequest
	if err := DecodePayload(req.Payload, &revokeReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := certService.RevokeCertificate(ctx, &revokeReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleCertVerify handles certificate verification requests.
func (h *Handler) handleCertVerify(ctx context.Context, req *Request) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var verifyReq service.VerifyCertificateRequest
	if err := DecodePayload(req.Payload, &verifyReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := certService.VerifyCertificate(ctx, &verifyReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleCertGetCA handles requests to retrieve the CA certificate.
func (h *Handler) handleCertGetCA(ctx context.Context, req *Request) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var caReq service.GetCACertificateRequest
	if err := DecodePayload(req.Payload, &caReq); err != nil {
		return h.errorResponse(err)
	}

	resp, err := certService.GetCACertificate(ctx, &caReq)
	if err != nil {
		return h.errorResponse(err)
	}

	payload, err := EncodePayload(resp)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleCertGetTLSConfig handles requests to retrieve TLS configuration.
// Note: The TLS config response contains a *tls.Config which is not serializable,
// so we return a response indicating that TLS config should be retrieved through
// the direct service layer or by requesting a new certificate.
func (h *Handler) handleCertGetTLSConfig(ctx context.Context, req *Request) *Response {
	certService := h.service.Certificate()
	if certService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("certificate"))
	}

	var tlsReq service.GetTLSConfigRequest
	if err := DecodePayload(req.Payload, &tlsReq); err != nil {
		return h.errorResponse(err)
	}

	// Note: GetTLSConfig returns a *tls.Config which cannot be serialized.
	// We validate the request and return success if the service can generate a config.
	_, err := certService.GetTLSConfig(ctx, &tlsReq)
	if err != nil {
		return h.errorResponse(err)
	}

	// Return a success response indicating TLS config was generated successfully.
	// The actual config must be retrieved through the service layer directly.
	response := struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{
		Success: true,
		Message: "TLS configuration generated successfully. Use service layer to retrieve the actual *tls.Config.",
	}

	payload, err := EncodePayload(response)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}
