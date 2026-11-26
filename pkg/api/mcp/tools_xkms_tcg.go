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

package mcp

import (
	"context"
	"encoding/base64"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// The xkms.ca.tcg.* tools serve go-xkms's TCGCAService (EK and AK
// certificates, TCG-CSR signing, device enrollment) over the same go-xkms CA
// adapter as the xkms.ca.* tools and the REST /api/v1/xkms/ca/tcg/* routes
// (#1330). Binary inputs are base64, as encoding/json encodes []byte.

// registerTCGCATools registers the xkms.ca.tcg.* tool definitions.
func (r *ToolRegistry) registerTCGCATools() {
	subject := func(binName, binDesc string) map[string]interface{} {
		return map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"common_name":  map[string]interface{}{"type": "string", "description": "Certificate common name"},
				"organization": map[string]interface{}{"type": "string", "description": "Certificate organization"},
				binName:        map[string]interface{}{"type": "string", "description": binDesc},
				"tenant_id":    map[string]interface{}{"type": "string", "description": "Tenant ID"},
			},
			"required": []string{"common_name", binName},
		}
	}
	r.addTool(ToolDefinition{
		Name:        "xkms.ca.tcg.issueEKCert",
		Description: "Issue a TPM Endorsement Key certificate",
		InputSchema: subject("ek_public_key", "Base64 DER (PKIX) EK public key"),
	})
	r.addTool(ToolDefinition{
		Name:        "xkms.ca.tcg.issueAKCert",
		Description: "Issue a TPM Attestation Key certificate",
		InputSchema: subject("public_key", "Base64 DER (PKIX) AK public key"),
	})
	r.addTool(ToolDefinition{
		Name:        "xkms.ca.tcg.signCSR",
		Description: "Sign a TCG-CSR-IDEVID and return the IAK and IDevID certificates",
		InputSchema: subject("tcg_csr", "Base64 packed TCG-CSR-IDEVID"),
	})
	r.addTool(ToolDefinition{
		Name:        "xkms.ca.tcg.enroll",
		Description: "Enroll a TPM device: IAK and IDevID certificates with the credential-activation challenge",
		InputSchema: subject("packed_csr", "Base64 packed TCG-CSR-IDEVID"),
	})
}

// tcgCA returns the wired CA's TCGCAService, or the typed
// operation-not-supported error when no CA is wired or it has no TCG verbs.
func (r *ToolRegistry) tcgCA(op string) (xkmstransport.TCGCAService, error) {
	ca, err := r.trustedCA(op)
	if err != nil {
		return nil, err
	}
	tcg, ok := ca.(xkmstransport.TCGCAService)
	if !ok {
		return nil, &XKMSOperationError{Operation: op, Cause: xkmssdk.ServiceErrOperationNotSupported}
	}
	return tcg, nil
}

// tcgArgs reads the arguments every xkms.ca.tcg.* tool shares: the common
// name and the named base64 binary input.
func tcgArgs(args map[string]interface{}, binName string) (string, []byte, error) {
	cn := getString(args, "common_name")
	if cn == "" {
		return "", nil, api.NewRequiredFieldError("common_name")
	}
	enc := getString(args, binName)
	if enc == "" {
		return "", nil, api.NewRequiredFieldError(binName)
	}
	bin, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", nil, api.NewValidationError(binName, "must be base64")
	}
	return cn, bin, nil
}

// handleTCGCAIssueEKCert issues an Endorsement Key certificate.
func (r *ToolRegistry) handleTCGCAIssueEKCert(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cn, pub, err := tcgArgs(args, "ek_public_key")
	if err != nil {
		return nil, err
	}
	tcg, err := r.tcgCA("ca.tcg.issueEKCert")
	if err != nil {
		return nil, err
	}
	return tcg.IssueEKCertificate(ctx, &xkmstransport.IssueEKCertificateRequest{
		CommonName: cn, Organization: getString(args, "organization"), EKPublicKey: pub, TenantID: getString(args, "tenant_id"),
	})
}

// handleTCGCAIssueAKCert issues an Attestation Key certificate.
func (r *ToolRegistry) handleTCGCAIssueAKCert(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cn, pub, err := tcgArgs(args, "public_key")
	if err != nil {
		return nil, err
	}
	tcg, err := r.tcgCA("ca.tcg.issueAKCert")
	if err != nil {
		return nil, err
	}
	return tcg.IssueAKCertificate(ctx, &xkmstransport.IssueAKCertificateRequest{
		CommonName: cn, Organization: getString(args, "organization"), PublicKey: pub, TenantID: getString(args, "tenant_id"),
	})
}

// handleTCGCASignCSR signs a packed TCG-CSR-IDEVID.
func (r *ToolRegistry) handleTCGCASignCSR(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cn, csr, err := tcgArgs(args, "tcg_csr")
	if err != nil {
		return nil, err
	}
	tcg, err := r.tcgCA("ca.tcg.signCSR")
	if err != nil {
		return nil, err
	}
	return tcg.SignTCGCSR(ctx, &xkmstransport.SignTCGCSRRequest{
		CommonName: cn, Organization: getString(args, "organization"), TCGCSR: csr, TenantID: getString(args, "tenant_id"),
	})
}

// handleTCGCAEnroll runs TCG device enrollment.
func (r *ToolRegistry) handleTCGCAEnroll(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	cn, packed, err := tcgArgs(args, "packed_csr")
	if err != nil {
		return nil, err
	}
	tcg, err := r.tcgCA("ca.tcg.enroll")
	if err != nil {
		return nil, err
	}
	return tcg.EnrollDevice(ctx, &xkmstransport.EnrollDeviceRequest{
		CommonName: cn, Organization: getString(args, "organization"), PackedCSR: packed, TenantID: getString(args, "tenant_id"),
	})
}
