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
	"encoding/base64"
	"time"

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
	args := map[string]interface{}{
		"so_pin":    req.SOPin,
		"user_pin":  req.UserPin,
		"threshold": req.Threshold,
	}
	if req.CredentialSealStrategy != "" {
		args["credential_seal_strategy"] = req.CredentialSealStrategy
	}
	if len(req.Officers) > 0 {
		officers := make([]interface{}, len(req.Officers))
		for i, o := range req.Officers {
			officers[i] = map[string]interface{}{
				"username": o.Username,
				"csr_pem":  o.CSRPEM,
			}
		}
		args["officers"] = officers
	}
	if req.Hostname != "" {
		args["hostname"] = req.Hostname
	}
	if len(req.SANs) > 0 {
		args["sans"] = stringsToInterfaces(req.SANs)
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

// CeremonyClaimCertBegin starts a certificate-claim challenge for the named
// officer by calling the qrdb_ceremony_claim_cert_begin MCP tool.
func (t *Transport) CeremonyClaimCertBegin(ctx context.Context, req *qrdbtransport.ClaimCertBeginRequest) (*qrdbtransport.ClaimCertBeginResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonyClaimCertBegin", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_ceremony_claim_cert_begin", map[string]interface{}{
		"username": req.Username,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.ClaimCertBeginResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyClaimCertBegin", "failed to decode response")
	}
	return &resp, nil
}

// CeremonyClaimCertComplete completes a certificate claim by submitting the
// signed nonce by calling the qrdb_ceremony_claim_cert_complete MCP tool.
func (t *Transport) CeremonyClaimCertComplete(ctx context.Context, req *qrdbtransport.ClaimCertCompleteRequest) (*qrdbtransport.ClaimCertCompleteResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonyClaimCertComplete", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_ceremony_claim_cert_complete", map[string]interface{}{
		"username":  req.Username,
		"nonce_hex": req.NonceHex,
		"signature": req.Signature,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.ClaimCertCompleteResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyClaimCertComplete", "failed to decode response")
	}
	return &resp, nil
}

// CeremonyClaimShare retrieves the Shamir share assigned to the given officer
// by calling the qrdb_ceremony_claim_share MCP tool. The server returns a
// "share" field which is mapped onto the canonical ShareJSON response field.
func (t *Transport) CeremonyClaimShare(ctx context.Context, req *qrdbtransport.ClaimShareRequest) (*qrdbtransport.ClaimShareResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonyClaimShare", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_ceremony_claim_share", map[string]interface{}{
		"username": req.Username,
	})
	if err != nil {
		return nil, err
	}
	var wire struct {
		Share string `json:"share"`
	}
	if err := remarshalBarrier(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyClaimShare", "failed to decode response")
	}
	return &qrdbtransport.ClaimShareResponse{ShareJSON: wire.Share}, nil
}

// CeremonySignCSR signs a CSR during the ceremony by calling the
// qrdb_ceremony_sign_csr MCP tool.
func (t *Transport) CeremonySignCSR(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonySignCSR", "request is required")
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

// CeremonyClaimShareBegin issues a factor challenge for an enrolled custodian
// to claim their Shamir share by calling the qrdb_ceremony_claim_share_begin
// MCP tool. The daemon returns the challenge nonce base64-encoded; it is
// decoded back into raw bytes for the canonical response.
func (t *Transport) CeremonyClaimShareBegin(ctx context.Context, req *qrdbtransport.CeremonyClaimShareBeginRequest) (*qrdbtransport.CeremonyClaimShareBeginResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonyClaimShareBegin", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_ceremony_claim_share_begin", map[string]interface{}{
		"custodian_id": req.CustodianID,
	})
	if err != nil {
		return nil, err
	}
	var wire struct {
		CustodianID string `json:"custodian_id"`
		Nonce       string `json:"nonce"`
		ExpiresAt   string `json:"expires_at"`
	}
	if err := remarshalBarrier(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyClaimShareBegin", "failed to decode response")
	}
	nonce, err := base64.StdEncoding.DecodeString(wire.Nonce)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyClaimShareBegin", "failed to decode nonce")
	}
	resp := &qrdbtransport.CeremonyClaimShareBeginResponse{
		CustodianID: wire.CustodianID,
		Nonce:       nonce,
	}
	if wire.ExpiresAt != "" {
		ts, perr := time.Parse(time.RFC3339, wire.ExpiresAt)
		if perr != nil {
			return nil, errors.Wrap(perr, errors.CodeInternal, "mcp.CeremonyClaimShareBegin", "failed to parse expires_at")
		}
		resp.ExpiresAt = ts
	}
	return resp, nil
}

// CeremonyClaimShareComplete verifies the custodian's factor response and
// returns the custodian metadata confirming share delivery by calling the
// qrdb_ceremony_claim_share_complete MCP tool. The factor response is sent
// base64-encoded so binary proof material survives the JSON tool boundary.
func (t *Transport) CeremonyClaimShareComplete(ctx context.Context, req *qrdbtransport.CeremonyClaimShareCompleteRequest) (*qrdbtransport.CeremonyCustodianInfo, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonyClaimShareComplete", "request is required")
	}
	args := map[string]interface{}{
		"custodian_id": req.CustodianID,
		"challenge_id": req.ChallengeID,
	}
	if len(req.FactorResponse) > 0 {
		args["factor_response"] = base64.StdEncoding.EncodeToString(req.FactorResponse)
	}
	raw, err := t.callBarrier(ctx, "qrdb_ceremony_claim_share_complete", args)
	if err != nil {
		return nil, err
	}
	var info qrdbtransport.CeremonyCustodianInfo
	if err := remarshalBarrier(raw, &info); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyClaimShareComplete", "failed to decode response")
	}
	return &info, nil
}

// CeremonySubmitUnsealShare submits a custodian's plaintext Shamir share with
// factor proof toward the unseal quorum by calling the
// qrdb_ceremony_submit_unseal_share MCP tool. Both the share bytes and the
// factor response are base64-encoded for transport across the JSON tool args.
func (t *Transport) CeremonySubmitUnsealShare(ctx context.Context, req *qrdbtransport.CeremonySubmitUnsealShareRequest) (*qrdbtransport.CeremonySubmitUnsealShareResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonySubmitUnsealShare", "request is required")
	}
	args := map[string]interface{}{
		"custodian_id": req.CustodianID,
		"challenge_id": req.ChallengeID,
		"share":        base64.StdEncoding.EncodeToString(req.ShareBytes),
	}
	if len(req.FactorResponse) > 0 {
		args["factor_response"] = base64.StdEncoding.EncodeToString(req.FactorResponse)
	}
	raw, err := t.callBarrier(ctx, "qrdb_ceremony_submit_unseal_share", args)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.CeremonySubmitUnsealShareResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonySubmitUnsealShare", "failed to decode response")
	}
	return &resp, nil
}

// CeremonyEnrollExternalOfficer enrols an officer whose key material lives
// outside the daemon by calling the
// qrdb_ceremony_enroll_external_officer MCP tool.
func (t *Transport) CeremonyEnrollExternalOfficer(ctx context.Context, req *qrdbtransport.EnrollExternalOfficerRequest) (*qrdbtransport.EnrollExternalOfficerResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.CeremonyEnrollExternalOfficer", "request is required")
	}
	args := map[string]interface{}{
		"username":    req.Username,
		"share_index": req.ShareIndex,
		"auth_factor": req.AuthFactor,
	}
	if req.CertPEM != "" {
		args["cert_pem"] = req.CertPEM
	}
	if req.PubKeyPEM != "" {
		args["pubkey_pem"] = req.PubKeyPEM
	}
	if len(req.ShareBytes) > 0 {
		// MCP tool ingests share_bytes as a string; the daemon converts
		// back to bytes server-side.
		args["share_bytes"] = string(req.ShareBytes)
	}

	raw, err := t.callBarrier(ctx, "qrdb_ceremony_enroll_external_officer", args)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.EnrollExternalOfficerResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.CeremonyEnrollExternalOfficer", "failed to decode response")
	}
	return &resp, nil
}
