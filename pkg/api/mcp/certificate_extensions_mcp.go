//go:build mcp

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

// certificate_extensions_mcp.go exposes the three certificate-extension verbs
// behind Security().CertificateExt() — CrossSign, Chain and
// IssueDeviceIdentity — as MCP tools.
//
// PEM and DER arguments are carried as base64 strings because the MCP tool
// argument schema is JSON with string-typed properties throughout.

package mcp

import (
	"context"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Tool definitions
// =============================================================================

var (
	// CertificateCrossSignTool cross-signs an external root certificate.
	CertificateCrossSignTool = Tool{
		Name: "quicbaas_certificate_cross_sign",
		Description: "Cross-sign an external root certificate with the local CA. " +
			"Produces a cross-certificate so relying parties anchored on the local " +
			"CA can validate chains rooted at the foreign root.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"foreign_root_pem": {
					Type:        "string",
					Description: "Base64-encoded PEM of the external root certificate",
				},
			},
			Required: []string{"foreign_root_pem"},
		},
	}

	// CertificateChainTool resolves a certificate's issuer chain.
	CertificateChainTool = Tool{
		Name: "quicbaas_certificate_chain",
		Description: "Resolve the issuer chain for a certificate by walking its " +
			"Authority Information Access extension. Returns the chain ordered " +
			"issuer-first and root-last, excluding the leaf.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"certificate_pem": {
					Type:        "string",
					Description: "Base64-encoded PEM of the certificate to resolve",
				},
			},
			Required: []string{"certificate_pem"},
		},
	}

	// CertificateDeviceIdentityTool issues a TCG device identity certificate.
	CertificateDeviceIdentityTool = Tool{
		Name: "quicbaas_certificate_issue_device_identity",
		Description: "Issue a TCG device identity certificate: an Endorsement Key " +
			"(ek), an Attestation Key (ak), or an IEEE 802.1AR IDevID (idevid).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"type": {
					Type:        "string",
					Description: "Device identity flavor",
					Enum:        []string{"ek", "ak", "idevid"},
				},
				"device_id": {
					Type:        "string",
					Description: "Unique device identifier; becomes the subject CN and TCG PermanentIdentifier",
				},
				"csr_pem": {
					Type:        "string",
					Description: "Base64-encoded TCG-CSR-IDEVID (required for type=idevid)",
				},
				"ek_pub": {
					Type:        "string",
					Description: "Base64-encoded EK public key, PEM or DER PKIX (required for type=ek)",
				},
				"ak_pub": {
					Type:        "string",
					Description: "Base64-encoded AK public key, PEM or DER PKIX (required for type=ak)",
				},
				"qualifying_data": {
					Type:        "string",
					Description: "Base64-encoded qualifying data binding the certificate to a TPM quote",
				},
				"validity_days": {
					Type:        "string",
					Description: "Override the profile default validity period, in days",
				},
				"tenant_id": {
					Type:        "string",
					Description: "Tenant the issuance is scoped to",
				},
			},
			Required: []string{"type", "device_id"},
		},
	}
)

// getAllCertificateExtensionTools returns the certificate extension tools.
func getAllCertificateExtensionTools() []Tool {
	return []Tool{
		CertificateCrossSignTool,
		CertificateChainTool,
		CertificateDeviceIdentityTool,
	}
}

// isCertificateExtensionTool reports whether name is a certificate extension
// tool.
func isCertificateExtensionTool(name string) bool {
	switch name {
	case "quicbaas_certificate_cross_sign",
		"quicbaas_certificate_chain",
		"quicbaas_certificate_issue_device_identity":
		return true
	default:
		return false
	}
}

// =============================================================================
// Handlers
// =============================================================================

// handleCertificateCrossSign handles the quicbaas_certificate_cross_sign tool.
func (r *ToolRegistry) handleCertificateCrossSign(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	foreignRoot, err := decodeBase64Arg(args, "foreign_root_pem")
	if err != nil {
		return nil, err
	}
	if len(foreignRoot) == 0 {
		return nil, api.NewRequiredFieldError("foreign_root_pem")
	}

	resp, err := svc.CrossSign(ctx, &service.CrossSignRequest{ForeignRootPEM: foreignRoot})
	if err != nil {
		return nil, api.NewOperationFailedError("cross-sign certificate", err)
	}

	return map[string]interface{}{
		"cross_signed_pem": string(resp.CrossSignedPEM),
	}, nil
}

// handleCertificateChain handles the quicbaas_certificate_chain tool.
func (r *ToolRegistry) handleCertificateChain(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	certPEM, err := decodeBase64Arg(args, "certificate_pem")
	if err != nil {
		return nil, err
	}
	if len(certPEM) == 0 {
		return nil, api.NewRequiredFieldError("certificate_pem")
	}

	resp, err := svc.Chain(ctx, &service.CertificateChainRequest{CertificatePEM: certPEM})
	if err != nil {
		return nil, api.NewOperationFailedError("resolve certificate chain", err)
	}

	chain := make([]string, 0, len(resp.ChainPEM))
	for _, elem := range resp.ChainPEM {
		chain = append(chain, string(elem))
	}
	return map[string]interface{}{"chain_pem": chain}, nil
}

// handleCertificateDeviceIdentity handles the
// quicbaas_certificate_issue_device_identity tool.
func (r *ToolRegistry) handleCertificateDeviceIdentity(ctx context.Context, svc service.CertificateService, args map[string]interface{}) (interface{}, error) {
	identityType := getString(args, "type")
	if identityType == "" {
		return nil, api.NewRequiredFieldError("type")
	}
	deviceID := getString(args, "device_id")
	if deviceID == "" {
		return nil, api.NewRequiredFieldError("device_id")
	}

	req := &service.IssueDeviceIdentityRequest{
		Type:     identityType,
		DeviceID: deviceID,
		TenantID: getString(args, "tenant_id"),
	}

	for field, dst := range map[string]*[]byte{
		"csr_pem":         &req.CSRPEM,
		"ek_pub":          &req.EKPub,
		"ak_pub":          &req.AKPub,
		"qualifying_data": &req.QualifyingData,
	} {
		decoded, err := decodeBase64Arg(args, field)
		if err != nil {
			return nil, err
		}
		*dst = decoded
	}

	if days := getInt(args, "validity_days"); days > 0 {
		req.ValidityDays = days
	}

	resp, err := svc.IssueDeviceIdentity(ctx, req)
	if err != nil {
		return nil, api.NewOperationFailedError("issue device identity certificate", err)
	}

	return map[string]interface{}{
		"certificate_pem": string(resp.CertificatePEM),
		"chain_pem":       string(resp.ChainPEM),
		"serial_number":   resp.SerialNumber,
		"not_before":      resp.NotBefore,
		"not_after":       resp.NotAfter,
	}, nil
}
