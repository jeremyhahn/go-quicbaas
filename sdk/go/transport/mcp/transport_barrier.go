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
	"encoding/json"
	"strings"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// =============================================================================
// BarrierClient implementation against the quicbaas MCP server.
//
// The qrdb SDK transport.BarrierClient interface defines the canonical
// barrier wire contract. The quicbaas MCP server exposes the same surface
// under the quicbaas_barrier_* tool namespace (see pkg/api/mcp/tools_barrier
// on the daemon side). This file makes *Transport implement
// qrdbtransport.BarrierClient so consumers can talk to the quicbaas MCP
// listener with the same client type they use against go-qrdb.
//
// Every method:
//   1. Short-circuits with errors.NewConnection when the transport is closed.
//   2. Calls the matching quicbaas_barrier_* MCP tool via callTool.
//   3. Decodes the (already-parsed) JSON content back into the canonical
//      qrdb response type via a round-trip through json.Marshal/Unmarshal.
// =============================================================================

// Compile-time interface assertion.
var _ qrdbtransport.BarrierClient = (*Transport)(nil)

// BarrierInitialize initialises the barrier with a single-passphrase secret
// by calling the qrdb_barrier_initialize MCP tool.
func (t *Transport) BarrierInitialize(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("mcp.BarrierInitialize", "request is required")
	}
	if _, err := t.callBarrier(ctx, "qrdb_barrier_initialize", map[string]interface{}{
		"secret": secretArg(req.Secret),
	}); err != nil {
		return err
	}
	return nil
}

// BarrierUnseal unseals the barrier with the master passphrase by calling
// the qrdb_barrier_unseal MCP tool.
func (t *Transport) BarrierUnseal(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error {
	if req == nil {
		return errors.NewInvalidInput("mcp.BarrierUnseal", "request is required")
	}
	if _, err := t.callBarrier(ctx, "qrdb_barrier_unseal", map[string]interface{}{
		"secret": secretArg(req.Secret),
	}); err != nil {
		return err
	}
	return nil
}

// BarrierSeal transitions the barrier back to the sealed state by calling
// the qrdb_barrier_seal MCP tool.
func (t *Transport) BarrierSeal(ctx context.Context) error {
	// The SO PIN travels as the tool's so_pin argument when the caller bound
	// one with qrdbsdk.WithSOPIN; the server verifies it.
	pin, ok, err := qrdbtransport.SOPINFromContext(ctx)
	if err != nil {
		return err
	}
	var args map[string]interface{}
	if ok {
		defer secure.Wipe(pin)
		if !strings.HasPrefix(t.endpoint, "https://") {
			return &qrdbtransport.SOPINRequiresTLSError{Transport: "mcp"}
		}
		args = map[string]interface{}{"so_pin": string(pin)}
	}
	if _, err := t.callBarrier(ctx, "qrdb_barrier_seal", args); err != nil {
		return err
	}
	return nil
}

// BarrierStatus returns the current barrier state by calling the
// qrdb_barrier_status MCP tool.
func (t *Transport) BarrierStatus(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
	raw, err := t.callBarrier(ctx, "qrdb_barrier_status", nil)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierStatusResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierStatus", "failed to decode status response")
	}
	return &resp, nil
}

// BarrierInitializeShamir initialises the barrier with Shamir secret sharing
// by calling the qrdb_barrier_shamir_initialize MCP tool.
func (t *Transport) BarrierInitializeShamir(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierInitializeShamir", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_shamir_initialize", map[string]interface{}{
		"secret":       secretArg(req.Secret),
		"threshold":    req.Threshold,
		"total_shares": req.TotalShares,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierInitializeShamirResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierInitializeShamir", "failed to decode response")
	}
	return &resp, nil
}

// BarrierUnsealWithShare submits a single Shamir share toward the unseal
// quorum by calling the qrdb_barrier_shamir_unseal_share MCP tool.
func (t *Transport) BarrierUnsealWithShare(ctx context.Context, req *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierUnsealWithShare", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_shamir_unseal_share", map[string]interface{}{
		"share": req.Share,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierUnsealShareResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierUnsealWithShare", "failed to decode response")
	}
	return &resp, nil
}

// BarrierUnsealWithShares unseals the barrier in a single call with a complete
// set of Shamir shares by calling the qrdb_barrier_shamir_unseal_shares
// MCP tool. The shares are sent as a JSON array argument so the server's
// strongly-typed parser does not have to split a comma-separated string.
func (t *Transport) BarrierUnsealWithShares(ctx context.Context, req *qrdbtransport.BarrierUnsealSharesRequest) error {
	if req == nil {
		return errors.NewInvalidInput("mcp.BarrierUnsealWithShares", "request is required")
	}
	if _, err := t.callBarrier(ctx, "qrdb_barrier_shamir_unseal_shares", map[string]interface{}{
		"shares": stringsToInterfaces(req.Shares),
	}); err != nil {
		return err
	}
	return nil
}

// BarrierRekey generates new Shamir shares for the existing root key by
// calling the qrdb_barrier_rekey MCP tool.
func (t *Transport) BarrierRekey(ctx context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierRekey", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_rekey", map[string]interface{}{
		"threshold": req.Threshold,
		"total":     req.Total,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierRekeyResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierRekey", "failed to decode response")
	}
	return &resp, nil
}

// BarrierGenerateRecoveryKeys generates recovery shares for disaster recovery
// by calling the qrdb_barrier_recovery_generate MCP tool.
func (t *Transport) BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierGenerateRecoveryKeys", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_recovery_generate", map[string]interface{}{
		"threshold": req.Threshold,
		"total":     req.Total,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierRecoveryKeysResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierGenerateRecoveryKeys", "failed to decode response")
	}
	return &resp, nil
}

// BarrierRecoverWithKeys unseals the barrier using recovery shares by calling
// the qrdb_barrier_recovery_recover MCP tool.
func (t *Transport) BarrierRecoverWithKeys(ctx context.Context, req *qrdbtransport.BarrierRecoverWithKeysRequest) error {
	if req == nil {
		return errors.NewInvalidInput("mcp.BarrierRecoverWithKeys", "request is required")
	}
	if _, err := t.callBarrier(ctx, "qrdb_barrier_recovery_recover", map[string]interface{}{
		"keys": stringsToInterfaces(req.Keys),
	}); err != nil {
		return err
	}
	return nil
}

// BarrierDeleteRecoveryKeys removes recovery-key metadata by calling the
// qrdb_barrier_recovery_delete MCP tool.
func (t *Transport) BarrierDeleteRecoveryKeys(ctx context.Context) error {
	if _, err := t.callBarrier(ctx, "qrdb_barrier_recovery_delete", nil); err != nil {
		return err
	}
	return nil
}

// BarrierHasRecoveryKeys reports whether recovery-key metadata is currently
// stored by calling the qrdb_barrier_recovery_status MCP tool.
func (t *Transport) BarrierHasRecoveryKeys(ctx context.Context) (*qrdbtransport.BarrierHasRecoveryKeysResponse, error) {
	raw, err := t.callBarrier(ctx, "qrdb_barrier_recovery_status", nil)
	if err != nil {
		return nil, err
	}
	var wire struct {
		HasRecoveryKeys bool `json:"has_recovery_keys"`
	}
	if err := remarshalBarrier(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierHasRecoveryKeys", "failed to decode response")
	}
	return &qrdbtransport.BarrierHasRecoveryKeysResponse{HasKeys: wire.HasRecoveryKeys}, nil
}

// BarrierGenerateRootToken mints a new root token using Shamir shares by
// calling the qrdb_barrier_root_token MCP tool.
func (t *Transport) BarrierGenerateRootToken(ctx context.Context, req *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierGenerateRootToken", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_root_token", map[string]interface{}{
		"shares": stringsToInterfaces(req.Shares),
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierRootTokenResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierGenerateRootToken", "failed to decode response")
	}
	return &resp, nil
}

// OfficerRegister registers a Security Officer for the system barrier by
// calling the qrdb_barrier_officer_register MCP tool. The signed certificate
// in the response must be delivered to the officer out of band.
func (t *Transport) OfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.OfficerRegister", "request is required")
	}
	args := map[string]interface{}{
		"username": req.Username,
		"csr_pem":  string(req.CSRPEM),
	}
	if req.AuthenticatorType != "" {
		args["authenticator_type"] = req.AuthenticatorType
	}
	if req.AuthenticatorRef != "" {
		args["authenticator_ref"] = req.AuthenticatorRef
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_officer_register", args)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.OfficerRegisterResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.OfficerRegister", "failed to decode response")
	}
	return &resp, nil
}

// OfficerList lists all registered Security Officers for the system barrier
// by calling the qrdb_barrier_officer_list MCP tool.
func (t *Transport) OfficerList(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	raw, err := t.callBarrier(ctx, "qrdb_barrier_officer_list", nil)
	if err != nil {
		return nil, err
	}
	var wire struct {
		Officers []*qrdbtransport.OfficerStatus `json:"officers"`
	}
	if err := remarshalBarrier(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.OfficerList", "failed to decode response")
	}
	return wire.Officers, nil
}

// OfficerClaimStatus returns per-officer claim status (no share data) by
// calling the qrdb_barrier_officer_claim_status MCP tool.
func (t *Transport) OfficerClaimStatus(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	raw, err := t.callBarrier(ctx, "qrdb_barrier_officer_claim_status", nil)
	if err != nil {
		return nil, err
	}
	var wire struct {
		Officers []*qrdbtransport.OfficerStatus `json:"officers"`
	}
	if err := remarshalBarrier(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.OfficerClaimStatus", "failed to decode response")
	}
	return wire.Officers, nil
}

// ShareClaimBegin issues a challenge nonce to the named Security Officer by
// calling the qrdb_barrier_share_claim_begin MCP tool.
func (t *Transport) ShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.ShareClaimBegin", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_share_claim_begin", map[string]interface{}{
		"username": req.Username,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.ShareClaimBeginResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ShareClaimBegin", "failed to decode response")
	}
	return &resp, nil
}

// ShareClaimComplete verifies the officer's nonce signature and returns the
// share bytes exactly once by calling the qrdb_barrier_share_claim_complete
// MCP tool. The caller must wipe ShareBytes after use.
func (t *Transport) ShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.ShareClaimComplete", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_share_claim_complete", map[string]interface{}{
		"username":  req.Username,
		"nonce_hex": req.NonceHex,
		"signature": req.Signature,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.ShareClaimCompleteResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ShareClaimComplete", "failed to decode response")
	}
	return &resp, nil
}

// ShamirSealVote submits a Shamir share as a seal vote for a Shamir-protected
// system barrier by calling the qrdb_barrier_shamir_seal_vote MCP tool.
func (t *Transport) ShamirSealVote(ctx context.Context, req *qrdbtransport.ShamirSealVoteRequest) (*qrdbtransport.ShamirSealVoteResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.ShamirSealVote", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_shamir_seal_vote", map[string]interface{}{
		"share": req.Share,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.ShamirSealVoteResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.ShamirSealVote", "failed to decode response")
	}
	return &resp, nil
}

// RecoveryOfficerRegister registers a Security Officer for recovery key
// distribution by calling the qrdb_barrier_recovery_officer_register MCP tool.
func (t *Transport) RecoveryOfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.RecoveryOfficerRegister", "request is required")
	}
	args := map[string]interface{}{
		"username": req.Username,
		"csr_pem":  string(req.CSRPEM),
	}
	if req.AuthenticatorType != "" {
		args["authenticator_type"] = req.AuthenticatorType
	}
	if req.AuthenticatorRef != "" {
		args["authenticator_ref"] = req.AuthenticatorRef
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_recovery_officer_register", args)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.OfficerRegisterResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.RecoveryOfficerRegister", "failed to decode response")
	}
	return &resp, nil
}

// RecoveryShareClaimBegin issues a challenge nonce for a recovery officer by
// calling the qrdb_barrier_recovery_share_claim_begin MCP tool.
func (t *Transport) RecoveryShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.RecoveryShareClaimBegin", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_recovery_share_claim_begin", map[string]interface{}{
		"username": req.Username,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.ShareClaimBeginResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.RecoveryShareClaimBegin", "failed to decode response")
	}
	return &resp, nil
}

// RecoveryShareClaimComplete verifies the recovery officer's nonce signature
// and returns the recovery share bytes exactly once by calling the
// qrdb_barrier_recovery_share_claim_complete MCP tool.
func (t *Transport) RecoveryShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.RecoveryShareClaimComplete", "request is required")
	}
	raw, err := t.callBarrier(ctx, "qrdb_barrier_recovery_share_claim_complete", map[string]interface{}{
		"username":  req.Username,
		"nonce_hex": req.NonceHex,
		"signature": req.Signature,
	})
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.ShareClaimCompleteResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.RecoveryShareClaimComplete", "failed to decode response")
	}
	return &resp, nil
}

// BarrierEnableEncryption is not supported on the MCP transport; enable
// encryption via the REST or gRPC (unix) transports which have direct access
// to the barrier key material.
func (t *Transport) BarrierEnableEncryption(_ context.Context, _ *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
	return nil, &qrdbtransport.NotImplementedError{Method: "BarrierEnableEncryption"}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// callBarrier is a thin wrapper around Transport.callTool that performs the
// closed-transport precondition check inline. It returns the (already JSON-
// parsed) tool result for the caller to round-trip into a typed response, or
// the conventional SDK QBError on failure.
func (t *Transport) callBarrier(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if t.closed.Load() {
		return nil, errors.NewConnection("mcp."+name, "transport is closed")
	}
	if err := t.ensureInitialized(ctx); err != nil {
		return nil, err
	}
	// Barrier tools are gated by the daemon's authorization layer; only the
	// public health-style tools skip authentication. Reuse the standard
	// ensureAuthenticated flow so credentials are forwarded when configured.
	if !t.isPublicTool(name) {
		if err := t.ensureAuthenticated(ctx); err != nil {
			return nil, err
		}
	}
	return t.callTool(ctx, name, args)
}

// remarshalBarrier copies the JSON shape from src (already-decoded interface{}
// from callTool) into the typed pointer dst via a JSON round-trip. This keeps
// the transport client free of bespoke per-field conversion logic.
func remarshalBarrier(src interface{}, dst interface{}) error {
	if src == nil {
		return nil
	}
	data, err := json.Marshal(src)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// stringsToInterfaces widens a []string to []interface{} for JSON-friendly
// transport. Returning []interface{} avoids accidental joins on the wire and
// matches the MCP tool schema, which accepts arrays of strings.
func stringsToInterfaces(in []string) []interface{} {
	if len(in) == 0 {
		return []interface{}{}
	}
	out := make([]interface{}, len(in))
	for i, s := range in {
		out[i] = s
	}
	return out
}
