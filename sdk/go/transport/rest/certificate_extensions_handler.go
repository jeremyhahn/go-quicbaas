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

// certificate_extensions_handler.go routes the three verbs behind
// Security().CertificateExt() — CrossSign, Chain and IssueDeviceIdentity — to
// their quicbaasd REST endpoints and decodes the responses into the leaf
// client/certificate types the SDK type-asserts on.
//
// These verbs cannot use the generic dispatch fallback: it derives the
// endpoint by lowercasing the method name (producing "/crosssign") and returns
// an untyped body the SDK cannot assert on.

package rest

import (
	"context"
	"net/http"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// Wire shapes (mirror pkg/api/rest/handlers_certificate_extensions.go)
// =============================================================================

type crossSignRESTWireRequest struct {
	ForeignRootPEM []byte `json:"foreign_root_pem"`
}

type crossSignRESTWireResponse struct {
	CrossSignedPEM []byte `json:"cross_signed_pem"`
}

type certificateChainRESTWireRequest struct {
	CertificatePEM []byte `json:"certificate_pem"`
}

type certificateChainRESTWireResponse struct {
	ChainPEM [][]byte `json:"chain_pem"`
}

type deviceIdentityRESTWireRequest struct {
	Type           string `json:"type"`
	DeviceID       string `json:"device_id"`
	CSRPEM         []byte `json:"csr_pem,omitempty"`
	EKPub          []byte `json:"ek_pub,omitempty"`
	AKPub          []byte `json:"ak_pub,omitempty"`
	QualifyingData []byte `json:"qualifying_data,omitempty"`
	ValidityDays   int    `json:"validity_days,omitempty"`
	TenantID       string `json:"tenant_id,omitempty"`
}

type deviceIdentityRESTWireResponse struct {
	CertificatePEM []byte    `json:"certificate_pem"`
	ChainPEM       []byte    `json:"chain_pem"`
	SerialNumber   string    `json:"serial_number"`
	NotBefore      time.Time `json:"not_before"`
	NotAfter       time.Time `json:"not_after"`
}

// =============================================================================
// Handlers
// =============================================================================

func (h *certificateRESTHandler) handleCrossSign(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.CrossSignRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.CrossSign", "request is required")
	}
	if len(r.ForeignRootPEM) == 0 {
		return nil, errors.NewInvalidInput("rest.certificate.CrossSign", "foreign root PEM is required")
	}

	wireReq := crossSignRESTWireRequest{ForeignRootPEM: r.ForeignRootPEM}

	var wire crossSignRESTWireResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/certificate/cross-sign", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &certificate.CrossSignResponse{CrossSignedPEM: wire.CrossSignedPEM}, nil
}

func (h *certificateRESTHandler) handleChain(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.ChainRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.Chain", "request is required")
	}
	if len(r.CertificatePEM) == 0 {
		return nil, errors.NewInvalidInput("rest.certificate.Chain", "certificate PEM is required")
	}

	wireReq := certificateChainRESTWireRequest{CertificatePEM: r.CertificatePEM}

	var wire certificateChainRESTWireResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/certificate/chain", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &certificate.ChainResponse{ChainPEM: wire.ChainPEM}, nil
}

func (h *certificateRESTHandler) handleIssueDeviceIdentity(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*certificate.DeviceIdentityRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.certificate.IssueDeviceIdentity", "request is required")
	}
	if r.Type == "" {
		return nil, errors.NewInvalidInput("rest.certificate.IssueDeviceIdentity", "identity type is required")
	}
	if r.DeviceID == "" {
		return nil, errors.NewInvalidInput("rest.certificate.IssueDeviceIdentity", "device ID is required")
	}

	wireReq := deviceIdentityRESTWireRequest{
		Type:           string(r.Type),
		DeviceID:       r.DeviceID,
		CSRPEM:         r.CSRPEM,
		EKPub:          r.EKPub,
		AKPub:          r.AKPub,
		QualifyingData: r.QualifyingData,
		ValidityDays:   r.ValidityDays,
		TenantID:       r.TenantID,
	}

	var wire deviceIdentityRESTWireResponse
	if err := h.transport.doJSONRequest(ctx, http.MethodPost,
		"/api/v1/certificate/device-identity", &wireReq, &wire); err != nil {
		return nil, err
	}
	return &certificate.DeviceIdentityResponse{
		CertificatePEM: wire.CertificatePEM,
		ChainPEM:       wire.ChainPEM,
		SerialNumber:   wire.SerialNumber,
		NotBefore:      wire.NotBefore,
		NotAfter:       wire.NotAfter,
	}, nil
}
