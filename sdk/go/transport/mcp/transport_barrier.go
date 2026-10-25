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
	if err := t.requireTLSForSecret(len(req.Secret) > 0); err != nil {
		return err
	}
	secret, err := secretArg("secret", req.Secret)
	if err != nil {
		return err
	}
	if _, err := t.callBarrier(ctx, "qrdb_barrier_initialize", map[string]interface{}{
		"secret": secret,
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
	if err := t.requireTLSForSecret(len(req.Secret) > 0); err != nil {
		return err
	}
	secret, err := secretArg("secret", req.Secret)
	if err != nil {
		return err
	}
	if _, err := t.callBarrier(ctx, "qrdb_barrier_unseal", map[string]interface{}{
		"secret": secret,
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
		if err := t.requireTLSForSecret(len(pin) > 0); err != nil {
			return err
		}
		soPIN, err := secretArg("so_pin", pin)
		if err != nil {
			return err
		}
		args = map[string]interface{}{"so_pin": soPIN}
	}
	if _, err := t.callBarrier(ctx, "qrdb_barrier_seal", args); err != nil {
		return err
	}
	return nil
}

// requireTLSForSecret refuses to send a secret to an endpoint that is not
// https://, so no barrier credential ever crosses a plaintext connection.
// Every MCP verb that carries one (an so_pin argument, an unseal or
// initialize secret, a passphrase or a decrypted key share) calls it with
// hasSecret true before the request is built; a call that carries none
// passes false and may use http://. The refusal is the typed
// *qrdbtransport.SOPINRequiresTLSError and never contains the secret.
func (t *Transport) requireTLSForSecret(hasSecret bool) error {
	if !hasSecret || strings.HasPrefix(t.endpoint, "https://") {
		return nil
	}
	return &qrdbtransport.SOPINRequiresTLSError{Transport: "mcp"}
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
	// The SO PIN comes from the request or, when it carries none, from the
	// PIN bound by qrdbtransport.WithSOPIN, as on qrdb's own transports.
	soPIN, err := qrdbtransport.ResolveSOPIN(ctx, "BarrierInitializeShamir", req.SOPin)
	if err != nil {
		return nil, err
	}
	if err := t.requireTLSForSecret(soPIN != "" || req.UserPin != ""); err != nil {
		return nil, err
	}
	args := map[string]interface{}{
		"threshold":    req.Threshold,
		"total_shares": req.TotalShares,
	}
	putOptional(args, "policy", req.Policy)
	putOptional(args, "so_pin", soPIN)
	putOptional(args, "user_pin", req.UserPin)
	raw, err := t.callBarrier(ctx, "qrdb_barrier_shamir_initialize", args)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierInitializeShamirResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierInitializeShamir", "failed to decode response")
	}
	return &resp, nil
}

// BarrierGenerateRecoveryKeys generates recovery shares for disaster recovery
// by calling the qrdb_barrier_recovery_generate MCP tool.
func (t *Transport) BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierGenerateRecoveryKeys", "request is required")
	}
	soPIN, err := qrdbtransport.ResolveSOPIN(ctx, "BarrierGenerateRecoveryKeys", req.SOPin)
	if err != nil {
		return nil, err
	}
	if err := t.requireTLSForSecret(soPIN != ""); err != nil {
		return nil, err
	}
	args := map[string]interface{}{"threshold": req.Threshold}
	putOptional(args, "policy", req.Policy)
	putOptional(args, "so_pin", soPIN)
	raw, err := t.callBarrier(ctx, "qrdb_barrier_recovery_generate", args)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierRecoveryKeysResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierGenerateRecoveryKeys", "failed to decode response")
	}
	return &resp, nil
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

// OfficerRegister registers a Security Officer for the system barrier by
// calling the qrdb_barrier_officer_register MCP tool. The signed certificate
// in the response must be delivered to the officer out of band.
func (t *Transport) OfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.OfficerRegister", "request is required")
	}
	var resp qrdbtransport.OfficerRegisterResponse
	if err := t.callBarrierInto(ctx, "OfficerRegister", "qrdb_barrier_officer_register", req, &resp); err != nil {
		return nil, err
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

// ShareClaimBegin issues a challenge nonce to the named Security Officer by
// calling the qrdb_barrier_share_claim_begin MCP tool.
func (t *Transport) ShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.ShareClaimBegin", "request is required")
	}
	var resp qrdbtransport.ShareClaimBeginResponse
	if err := t.callBarrierInto(ctx, "ShareClaimBegin", "qrdb_barrier_share_claim_begin", req, &resp); err != nil {
		return nil, err
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
	var resp qrdbtransport.ShareClaimCompleteResponse
	if err := t.callBarrierInto(ctx, "ShareClaimComplete", "qrdb_barrier_share_claim_complete", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RecoveryOfficerRegister registers a Security Officer for recovery key
// distribution by calling the qrdb_barrier_recovery_officer_register MCP tool.
func (t *Transport) RecoveryOfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.RecoveryOfficerRegister", "request is required")
	}
	var resp qrdbtransport.OfficerRegisterResponse
	if err := t.callBarrierInto(ctx, "RecoveryOfficerRegister", "qrdb_barrier_recovery_officer_register", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RecoveryShareClaimBegin issues a challenge nonce for a recovery officer by
// calling the qrdb_barrier_recovery_share_claim_begin MCP tool.
func (t *Transport) RecoveryShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.RecoveryShareClaimBegin", "request is required")
	}
	var resp qrdbtransport.ShareClaimBeginResponse
	if err := t.callBarrierInto(ctx, "RecoveryShareClaimBegin", "qrdb_barrier_recovery_share_claim_begin", req, &resp); err != nil {
		return nil, err
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
	var resp qrdbtransport.ShareClaimCompleteResponse
	if err := t.callBarrierInto(ctx, "RecoveryShareClaimComplete", "qrdb_barrier_recovery_share_claim_complete", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierClusterUnseal reports the cluster-wide unseal state by calling the
// qrdb_barrier_cluster_unseal MCP tool.
func (t *Transport) BarrierClusterUnseal(ctx context.Context) (*qrdbtransport.ClusterSeal, error) {
	var resp qrdbtransport.ClusterSeal
	if err := t.callBarrierInto(ctx, "BarrierClusterUnseal", "qrdb_barrier_cluster_unseal", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierQuorumBegin opens a quorum round by calling the
// qrdb_barrier_quorum_begin MCP tool.
func (t *Transport) BarrierQuorumBegin(ctx context.Context, req *qrdbtransport.QuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	var resp qrdbtransport.QuorumChallenge
	if err := t.callBarrierInto(ctx, "BarrierQuorumBegin", "qrdb_barrier_quorum_begin", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierQuorumSubmit submits a holder's share toward a quorum round by
// calling the qrdb_barrier_quorum_submit MCP tool.
func (t *Transport) BarrierQuorumSubmit(ctx context.Context, req *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
	if req != nil {
		if err := t.requireTLSForSecret(len(req.Share) > 0); err != nil {
			return nil, err
		}
	}
	var resp qrdbtransport.QuorumOutcome
	if err := t.callBarrierInto(ctx, "BarrierQuorumSubmit", "qrdb_barrier_quorum_submit", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierRekeyStart opens a rekey round by calling the
// qrdb_barrier_rekey_start MCP tool.
func (t *Transport) BarrierRekeyStart(ctx context.Context, req *qrdbtransport.RekeyStartRequest) (*qrdbtransport.QuorumRound, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierRekeyStart", "request is required")
	}
	soPIN, err := qrdbtransport.ResolveSOPIN(ctx, "BarrierRekeyStart", req.SOPin)
	if err != nil {
		return nil, err
	}
	if err := t.requireTLSForSecret(soPIN != ""); err != nil {
		return nil, err
	}
	resolved := *req
	resolved.SOPin = soPIN
	var resp qrdbtransport.QuorumRound
	if err := t.callBarrierInto(ctx, "BarrierRekeyStart", "qrdb_barrier_rekey_start", &resolved, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierOfficerRevoke revokes a holder through a quorum-gated rekey by
// calling the qrdb_barrier_officer_revoke MCP tool.
func (t *Transport) BarrierOfficerRevoke(ctx context.Context, req *qrdbtransport.OfficerRevokeRequest) (*qrdbtransport.QuorumRound, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierOfficerRevoke", "request is required")
	}
	soPIN, err := qrdbtransport.ResolveSOPIN(ctx, "BarrierOfficerRevoke", req.SOPin)
	if err != nil {
		return nil, err
	}
	if err := t.requireTLSForSecret(soPIN != ""); err != nil {
		return nil, err
	}
	resolved := *req
	resolved.SOPin = soPIN
	var resp qrdbtransport.QuorumRound
	if err := t.callBarrierInto(ctx, "BarrierOfficerRevoke", "qrdb_barrier_officer_revoke", &resolved, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// BarrierEnableEncryption migrates an unencrypted barrier to a keyed strategy
// through qrdb's qrdb_barrier_enable_encryption tool. An empty TenantID targets
// the system barrier. The migration runs asynchronously on the node; poll
// BarrierStatus for completion.
func (t *Transport) BarrierEnableEncryption(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("mcp.BarrierEnableEncryption", "request is required")
	}
	if err := t.requireTLSForSecret(len(req.Passphrase) > 0); err != nil {
		return nil, err
	}
	passphrase, err := secretArg("passphrase", req.Passphrase)
	if err != nil {
		return nil, err
	}
	tool := "qrdb_barrier_enable_encryption"
	args := map[string]interface{}{
		"target_strategy":  req.Strategy,
		"mechanism":        req.Mechanism,
		"availability":     req.Availability,
		"passphrase":       passphrase,
		"shamir_threshold": req.ShamirThreshold,
		"shamir_total":     req.ShamirTotal,
	}
	// The system tool ignores tenant_id, so a tenant request must go to the
	// tenant tool or it would migrate the system barrier.
	if req.TenantID != "" {
		tool = "qrdb_tenant_barrier_enable_encryption"
		args["tenant_id"] = req.TenantID
	}
	raw, err := t.callBarrier(ctx, tool, args)
	if err != nil {
		return nil, err
	}
	var resp qrdbtransport.BarrierEnableEncryptionResponse
	if err := remarshalBarrier(raw, &resp); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.BarrierEnableEncryption", "failed to decode response")
	}
	return &resp, nil
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

// callBarrierInto maps req onto the tool's arguments through its JSON
// shape, calls tool and decodes the result into dest.
func (t *Transport) callBarrierInto(ctx context.Context, method, tool string, req, dest interface{}) error {
	var args map[string]interface{}
	if req != nil {
		if err := remarshalBarrier(req, &args); err != nil {
			return errors.Wrap(err, errors.CodeInvalidInput, "mcp."+method, "failed to encode request")
		}
	}
	raw, err := t.callBarrier(ctx, tool, args)
	if err != nil {
		return err
	}
	if err := remarshalBarrier(raw, dest); err != nil {
		return errors.Wrap(err, errors.CodeInternal, "mcp."+method, "failed to decode response")
	}
	return nil
}

// putOptional sets args[key] only when value is non-empty, so absent
// optional fields stay off the wire.
func putOptional(args map[string]interface{}, key, value string) {
	if value != "" {
		args[key] = value
	}
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
