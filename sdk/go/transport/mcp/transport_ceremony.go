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

package mcp

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// CeremonyClient implementation against the quicbaas MCP server.
//
// The qrdb SDK transport.CeremonyClient interface defines the canonical
// initialization-ceremony wire contract. The quicbaas MCP server exposes the
// same surface under the quicbaas_ceremony_* tool namespace (see
// pkg/api/mcp/tools_ceremony on the daemon side). This file makes
// *Transport implement qrdbtransport.CeremonyClient so consumers can talk to
// the quicbaas MCP listener with the same client type they use against
// go-qrdb.
// =============================================================================

// Compile-time interface assertion.
var _ qrdbtransport.CeremonyClient = (*Transport)(nil)

// CeremonyInitialize starts the initialization ceremony by calling the
// qrdb_ceremony_initialize MCP tool. The officer list and SANs are sent
// as JSON arrays so the daemon's typed argument parser can decode them
// without splitting strings.
func (t *Transport) CeremonyInitialize(ctx context.Context, req *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonyInitialize", "request is required")
	}
	if err := t.requireTLSForSecret(req.SOPin != "" || req.UserPin != "" || len(req.Secret) > 0); err != nil {
		return nil, err
	}
	args := map[string]interface{}{
		"so_pin":    req.SOPin,
		"user_pin":  req.UserPin,
		"threshold": req.Threshold,
	}
	if req.CredentialSealStrategy != "" {
		args["credential_seal_strategy"] = req.CredentialSealStrategy
	}
	if req.Policy != "" {
		args["policy"] = req.Policy
	}
	if len(req.Officers) > 0 {
		// Each officer goes in go-qrdb's own JSON shape, so role, the
		// share-envelope key, factors and the designated factor all arrive.
		var officers []interface{}
		if err := remarshalBarrier(req.Officers, &officers); err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, "mcp.CeremonyInitialize", "failed to encode officers")
		}
		args["officers"] = officers
	}
	if req.Hostname != "" {
		args["hostname"] = req.Hostname
	}
	if len(req.SANs) > 0 {
		args["sans"] = stringsToInterfaces(req.SANs)
	}
	if req.AdminCSRPEM != "" {
		args["admin_csr_pem"] = req.AdminCSRPEM
	}
	if req.AdminCertPEM != "" {
		args["admin_cert_pem"] = req.AdminCertPEM
	}
	if req.AdminCertPoolPEM != "" {
		args["admin_cert_pool_pem"] = req.AdminCertPoolPEM
	}
	if req.AdminGenerate {
		args["admin_generate"] = true
	}
	if len(req.Secret) > 0 {
		secret, err := secretArg("secret", req.Secret)
		if err != nil {
			return nil, err
		}
		args["secret"] = secret
	}

	raw, err := t.callBarrier(ctx, "qrdb_ceremony_initialize", args)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CeremonyInitializeResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyInitialize", "failed to decode response")
	}
	return &resp, nil
}

// CeremonyStatus returns the current ceremony state by calling the
// qrdb_ceremony_status MCP tool.
func (t *Transport) CeremonyStatus(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error) {
	raw, err := t.callBarrier(ctx, "qrdb_ceremony_status", nil)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CeremonyStatusResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyStatus", "failed to decode response")
	}
	return &resp, nil
}

// CeremonySignCSR signs a CSR during the ceremony by calling the
// qrdb_ceremony_sign_csr MCP tool.
func (t *Transport) CeremonySignCSR(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonySignCSR", "request is required")
	}
	if err := t.requireTLSForSecret(req.SOPin != ""); err != nil {
		return nil, err
	}
	raw, err := t.callBarrier(ctx, "qrdb_ceremony_sign_csr", map[string]interface{}{
		"username": req.Username,
		"so_pin":   req.SOPin,
		"csr_pem":  req.CSRPEM,
		"role":     req.Role,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.SignCSRInitResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonySignCSR", "failed to decode response")
	}
	return &resp, nil
}
