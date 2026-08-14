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

// certificate_extensions_handler.go transforms the CrossSign, Chain and
// IssueDeviceIdentity requests and responses behind
// Security().CertificateExt() for the MCP transport.
//
// The quicbaasd MCP tool schema is JSON with string-typed properties, so PEM
// and DER arguments travel base64-encoded and the server returns the PEM text
// directly.

package mcp

import (
	"encoding/base64"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// CrossSign
// =============================================================================

func (h *certificateHandler) transformCrossSignRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*certificate.CrossSignRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.certificate.CrossSign",
			"invalid request type, expected *certificate.CrossSignRequest")
	}
	if len(r.ForeignRootPEM) == 0 {
		return nil, errors.NewInvalidInput("mcp.certificate.CrossSign",
			"foreign root PEM is required")
	}
	return map[string]interface{}{
		"foreign_root_pem": base64.StdEncoding.EncodeToString(r.ForeignRootPEM),
	}, nil
}

func (h *certificateHandler) transformCrossSignResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.CrossSign")
	if err != nil {
		return nil, err
	}
	return &certificate.CrossSignResponse{
		CrossSignedPEM: []byte(getResultString(m, "cross_signed_pem")),
	}, nil
}

// =============================================================================
// Chain
// =============================================================================

func (h *certificateHandler) transformChainRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*certificate.ChainRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.certificate.Chain",
			"invalid request type, expected *certificate.ChainRequest")
	}
	if len(r.CertificatePEM) == 0 {
		return nil, errors.NewInvalidInput("mcp.certificate.Chain",
			"certificate PEM is required")
	}
	return map[string]interface{}{
		"certificate_pem": base64.StdEncoding.EncodeToString(r.CertificatePEM),
	}, nil
}

func (h *certificateHandler) transformChainResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.Chain")
	if err != nil {
		return nil, err
	}

	raw, _ := m["chain_pem"].([]interface{})
	chain := make([][]byte, 0, len(raw))
	for _, elem := range raw {
		if s, ok := elem.(string); ok {
			chain = append(chain, []byte(s))
		}
	}
	return &certificate.ChainResponse{ChainPEM: chain}, nil
}

// =============================================================================
// IssueDeviceIdentity
// =============================================================================

func (h *certificateHandler) transformIssueDeviceIdentityRequest(req interface{}) (map[string]interface{}, error) {
	r, ok := req.(*certificate.DeviceIdentityRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("mcp.certificate.IssueDeviceIdentity",
			"invalid request type, expected *certificate.DeviceIdentityRequest")
	}
	if r.Type == "" {
		return nil, errors.NewInvalidInput("mcp.certificate.IssueDeviceIdentity",
			"identity type is required")
	}
	if r.DeviceID == "" {
		return nil, errors.NewInvalidInput("mcp.certificate.IssueDeviceIdentity",
			"device ID is required")
	}

	args := map[string]interface{}{
		"type":      string(r.Type),
		"device_id": r.DeviceID,
	}
	for key, value := range map[string][]byte{
		"csr_pem":         r.CSRPEM,
		"ek_pub":          r.EKPub,
		"ak_pub":          r.AKPub,
		"qualifying_data": r.QualifyingData,
	} {
		if len(value) > 0 {
			args[key] = base64.StdEncoding.EncodeToString(value)
		}
	}
	if r.ValidityDays > 0 {
		args["validity_days"] = r.ValidityDays
	}
	if r.TenantID != "" {
		args["tenant_id"] = r.TenantID
	}
	return args, nil
}

func (h *certificateHandler) transformIssueDeviceIdentityResponse(resp interface{}) (interface{}, error) {
	m, err := asResultMap(resp, "mcp.certificate.IssueDeviceIdentity")
	if err != nil {
		return nil, err
	}
	return &certificate.DeviceIdentityResponse{
		CertificatePEM: []byte(getResultString(m, "certificate_pem")),
		ChainPEM:       []byte(getResultString(m, "chain_pem")),
		SerialNumber:   getResultString(m, "serial_number"),
		NotBefore:      getResultTime(m, "not_before"),
		NotAfter:       getResultTime(m, "not_after"),
	}, nil
}
