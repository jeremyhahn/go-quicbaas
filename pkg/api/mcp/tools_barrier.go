//go:build mcp

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
	"strings"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Barrier MCP tools — quicbaas_barrier_* surface.
//
// Mirrors the REST handlers in pkg/api/rest/handlers_barrier.go so the MCP
// transport exposes an identical barrier-lifecycle contract:
//
//   qrdb_barrier_status
//   qrdb_barrier_initialize
//   qrdb_barrier_unseal
//   qrdb_barrier_seal
//   qrdb_barrier_shamir_initialize
//   qrdb_barrier_shamir_unseal_share
//   qrdb_barrier_shamir_unseal_shares
//   qrdb_barrier_rekey
//   qrdb_barrier_recovery_generate
//   qrdb_barrier_recovery_recover
//   qrdb_barrier_recovery_delete
//   qrdb_barrier_recovery_status
//   qrdb_barrier_root_token
//
// All tools delegate to a BarrierServicer installed via
// ToolRegistry.SetBarrierService. When the service is unwired, every tool
// returns BarrierServiceUnavailableError so MCP clients see a typed
// "service unavailable" condition rather than a nil-pointer panic.
// =============================================================================

// barrierServiceUnavailableQBError is the canonical QBError returned when the
// barrier surface is not wired. Returning a QBError with the ErrUnavailable
// code lets pkg/api/mcp/server.go's mapErrorToJSONRPCError classify the
// condition through its explicit-code branch (ServiceUnavailable) instead of
// the generic "Tool execution failed" default, which previously surfaced to
// CLI callers as a misleading internal error.
var barrierServiceUnavailableQBError = &qberrors.QBError{
	Code: qberrors.ErrUnavailable,
	Op:   "mcp.barrier",
	Err:  &BarrierServiceUnavailableError{},
}

// BarrierServiceUnavailableError is returned by every barrier tool when the
// BarrierServicer has not been wired into the ToolRegistry. This mirrors the
// REST 503-on-nil-facade behaviour so clients of either transport can rely on
// the same typed unavailability signal during early boot or test setups. It is
// always wrapped in barrierServiceUnavailableQBError before being returned so
// the MCP error mapper classifies it as ServiceUnavailable.
type BarrierServiceUnavailableError struct{}

// Error implements the error interface. The message contains "unavailable" so
// that even when the value is unwrapped from its QBError envelope the MCP
// mapper's message-pattern fallback still classifies it correctly.
func (e *BarrierServiceUnavailableError) Error() string {
	return "barrier service unavailable: not configured"
}

// BarrierShareRequiredError is returned when a Shamir share submission tool
// is called without the required share value.
type BarrierShareRequiredError struct{}

// Error implements the error interface.
func (e *BarrierShareRequiredError) Error() string {
	return "share is required"
}

// BarrierSharesRequiredError is returned when a Shamir bulk-unseal or
// root-token generation tool is called without any share values.
type BarrierSharesRequiredError struct{}

// Error implements the error interface.
func (e *BarrierSharesRequiredError) Error() string {
	return "shares is required"
}

// BarrierRecoveryKeysRequiredError is returned when the recovery-recover tool
// is called without any recovery key values.
type BarrierRecoveryKeysRequiredError struct{}

// Error implements the error interface.
func (e *BarrierRecoveryKeysRequiredError) Error() string {
	return "keys is required"
}

// BarrierThresholdInvalidError is returned when a Shamir threshold/total
// parameter pair fails validation (zero, negative, or threshold > total).
type BarrierThresholdInvalidError struct {
	Reason string
}

// Error implements the error interface.
func (e *BarrierThresholdInvalidError) Error() string {
	return "invalid threshold/total: " + e.Reason
}

// registerBarrierTools adds every quicbaas_barrier_* tool definition to the
// registry. The tools are dispatched via isBarrierTool / executeBarrierTool.
func (r *ToolRegistry) registerBarrierTools() {
	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_status",
		Description: "Return the current barrier status including seal state, sealing strategy, and hardware backing indicator.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_initialize",
		Description: "Initialize the cryptographic barrier with a single-passphrase secret.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret": map[string]interface{}{
					"type":        "string",
					"description": "Passphrase used to seal the barrier on first initialisation.",
				},
			},
			"required": []string{"secret"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_unseal",
		Description: "Unseal the barrier with the single-passphrase secret that initialised it.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret": map[string]interface{}{
					"type":        "string",
					"description": "Passphrase used to unseal the barrier.",
				},
			},
			"required": []string{"secret"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_seal",
		Description: "Seal the barrier, transitioning it back to the sealed state and clearing the root key from memory.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_shamir_initialize",
		Description: "Initialise the barrier with Shamir secret sharing, returning the generated key shares.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"secret": map[string]interface{}{
					"type":        "string",
					"description": "Optional master passphrase. Most strategies generate one when omitted.",
				},
				"threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Minimum number of shares required to reconstruct the root key.",
					"minimum":     1,
				},
				"total_shares": map[string]interface{}{
					"type":        "integer",
					"description": "Total number of Shamir shares to generate.",
					"minimum":     1,
				},
			},
			"required": []string{"threshold", "total_shares"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_shamir_unseal_share",
		Description: "Submit a single Shamir share toward the unseal quorum threshold.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"share": map[string]interface{}{
					"type":        "string",
					"description": "A single Shamir share value.",
				},
			},
			"required": []string{"share"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_shamir_unseal_shares",
		Description: "Unseal the barrier in a single call using a complete set of Shamir shares.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shares": map[string]interface{}{
					"type":        "array",
					"description": "Complete set of Shamir shares to unseal the barrier.",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"shares"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_rekey",
		Description: "Generate new Shamir shares for the existing root key with the supplied threshold and total parameters.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"threshold": map[string]interface{}{
					"type":        "integer",
					"description": "New minimum number of shares required for reconstruction.",
					"minimum":     1,
				},
				"total": map[string]interface{}{
					"type":        "integer",
					"description": "New total number of shares to generate.",
					"minimum":     1,
				},
			},
			"required": []string{"threshold", "total"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_recovery_generate",
		Description: "Generate recovery key shares from the root key for disaster-recovery scenarios.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Minimum number of recovery keys required for recovery.",
					"minimum":     1,
				},
				"total": map[string]interface{}{
					"type":        "integer",
					"description": "Total number of recovery keys to generate.",
					"minimum":     1,
				},
			},
			"required": []string{"threshold", "total"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_recovery_recover",
		Description: "Unseal the barrier using a complete set of recovery keys.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"keys": map[string]interface{}{
					"type":        "array",
					"description": "Complete set of recovery key values.",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"keys"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_recovery_delete",
		Description: "Delete all recovery-key metadata from storage.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_recovery_status",
		Description: "Report whether recovery-key metadata is currently persisted.",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_root_token",
		Description: "Mint a new root token by proving knowledge of the master key with Shamir shares.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"shares": map[string]interface{}{
					"type":        "array",
					"description": "Complete set of Shamir shares used to authorise the operation.",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"shares"},
		},
	})
}

// barrierToolNames enumerates every tool name owned by the barrier surface.
// Membership is used by isBarrierTool to route dispatch from the registry's
// default branch into executeBarrierTool, avoiding a giant per-name switch in
// the parent dispatcher.
var barrierToolNames = map[string]struct{}{
	"qrdb_barrier_status":               {},
	"qrdb_barrier_initialize":           {},
	"qrdb_barrier_unseal":               {},
	"qrdb_barrier_seal":                 {},
	"qrdb_barrier_shamir_initialize":    {},
	"qrdb_barrier_shamir_unseal_share":  {},
	"qrdb_barrier_shamir_unseal_shares": {},
	"qrdb_barrier_rekey":                {},
	"qrdb_barrier_recovery_generate":    {},
	"qrdb_barrier_recovery_recover":     {},
	"qrdb_barrier_recovery_delete":      {},
	"qrdb_barrier_recovery_status":      {},
	"qrdb_barrier_root_token":           {},
}

// isBarrierTool reports whether the given tool name belongs to the barrier
// surface. Called from ToolRegistry.ExecuteTool's default branch.
func isBarrierTool(name string) bool {
	_, ok := barrierToolNames[name]
	return ok
}

// barrierExecutor is the signature each per-tool handler implements.
type barrierExecutor func(context.Context, service.BarrierServicer, map[string]interface{}) (interface{}, error)

// barrierExecutors is the map-based dispatch table for barrier tools. Lookup
// is O(1) and keeps executeBarrierTool free of a 17-arm switch.
var barrierExecutors = map[string]barrierExecutor{
	"qrdb_barrier_status":               executeBarrierStatus,
	"qrdb_barrier_initialize":           executeBarrierInitialize,
	"qrdb_barrier_unseal":               executeBarrierUnseal,
	"qrdb_barrier_seal":                 executeBarrierSeal,
	"qrdb_barrier_shamir_initialize":    executeBarrierShamirInitialize,
	"qrdb_barrier_shamir_unseal_share":  executeBarrierShamirUnsealShare,
	"qrdb_barrier_shamir_unseal_shares": executeBarrierShamirUnsealShares,
	"qrdb_barrier_rekey":                executeBarrierRekey,
	"qrdb_barrier_recovery_generate":    executeBarrierRecoveryGenerate,
	"qrdb_barrier_recovery_recover":     executeBarrierRecoveryRecover,
	"qrdb_barrier_recovery_delete":      executeBarrierRecoveryDelete,
	"qrdb_barrier_recovery_status":      executeBarrierRecoveryStatus,
	"qrdb_barrier_root_token":           executeBarrierRootToken,
}

// executeBarrierTool dispatches a barrier-surface call to the appropriate
// executor. Returns barrierServiceUnavailableQBError when the registry has no
// BarrierServicer wired, mirroring the REST 503-on-nil semantics and ensuring
// the MCP error mapper classifies it as ServiceUnavailable.
func (r *ToolRegistry) executeBarrierTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	svc := r.barrierSvc
	if svc == nil {
		return nil, barrierServiceUnavailableQBError
	}
	executor, ok := barrierExecutors[name]
	if !ok {
		// Should not happen — isBarrierTool gates entry. Defensive return.
		return nil, barrierServiceUnavailableQBError
	}
	return executor(ctx, svc, args)
}

// ---------------------------------------------------------------------------
// Per-tool executors
// ---------------------------------------------------------------------------

// executeBarrierStatus implements qrdb_barrier_status.
func executeBarrierStatus(ctx context.Context, svc service.BarrierServicer, _ map[string]interface{}) (interface{}, error) {
	info, err := svc.BarrierStatus(ctx)
	if err != nil {
		return nil, err
	}
	return barrierStatusToMap(info), nil
}

// executeBarrierInitialize implements qrdb_barrier_initialize.
func executeBarrierInitialize(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	// The secret may be empty for the "none" barrier strategy (passthrough).
	// Strategy-appropriate validation is the backend's responsibility, matching
	// the gRPC/REST/QUIC transports which pass the secret through unvalidated.
	secret := getString(args, "secret")
	if err := svc.BarrierInitialize(ctx, secret); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "initialized"}, nil
}

// executeBarrierUnseal implements qrdb_barrier_unseal.
func executeBarrierUnseal(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	secret := getString(args, "secret")
	if secret == "" {
		return nil, &BarrierThresholdInvalidError{Reason: "secret is required"}
	}
	if err := svc.BarrierUnseal(ctx, secret); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "unsealed"}, nil
}

// executeBarrierSeal implements qrdb_barrier_seal.
func executeBarrierSeal(ctx context.Context, svc service.BarrierServicer, _ map[string]interface{}) (interface{}, error) {
	if err := svc.BarrierSeal(ctx); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "sealed"}, nil
}

// executeBarrierShamirInitialize implements qrdb_barrier_shamir_initialize.
func executeBarrierShamirInitialize(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	secret := getString(args, "secret")
	threshold := getInt(args, "threshold")
	total := getInt(args, "total_shares")
	if err := validateBarrierThreshold(threshold, total); err != nil {
		return nil, err
	}
	info, err := svc.BarrierInitializeShamir(ctx, secret, threshold, total)
	if err != nil {
		return nil, err
	}
	return shamirInitToMap(info), nil
}

// executeBarrierShamirUnsealShare implements qrdb_barrier_shamir_unseal_share.
func executeBarrierShamirUnsealShare(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	share := getString(args, "share")
	if share == "" {
		return nil, &BarrierShareRequiredError{}
	}
	progress, err := svc.BarrierUnsealWithShare(ctx, share)
	if err != nil {
		return nil, err
	}
	return unsealShareProgressToMap(progress), nil
}

// executeBarrierShamirUnsealShares implements
// qrdb_barrier_shamir_unseal_shares. Accepts either a string slice or a
// comma-separated string in the "shares" argument to mirror the qrdb-style
// MCP input shape.
func executeBarrierShamirUnsealShares(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	shares := parseBarrierShareSlice(args, "shares")
	if len(shares) == 0 {
		return nil, &BarrierSharesRequiredError{}
	}
	if err := svc.BarrierUnsealWithShares(ctx, shares); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "unsealed"}, nil
}

// executeBarrierRekey implements qrdb_barrier_rekey.
func executeBarrierRekey(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	threshold := getInt(args, "threshold")
	total := getInt(args, "total")
	if err := validateBarrierThreshold(threshold, total); err != nil {
		return nil, err
	}
	info, err := svc.BarrierRekey(ctx, threshold, total)
	if err != nil {
		return nil, err
	}
	return shamirInitToMap(info), nil
}

// executeBarrierRecoveryGenerate implements qrdb_barrier_recovery_generate.
func executeBarrierRecoveryGenerate(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	threshold := getInt(args, "threshold")
	total := getInt(args, "total")
	if err := validateBarrierThreshold(threshold, total); err != nil {
		return nil, err
	}
	info, err := svc.BarrierGenerateRecoveryKeys(ctx, threshold, total)
	if err != nil {
		return nil, err
	}
	return recoveryKeysToMap(info), nil
}

// executeBarrierRecoveryRecover implements qrdb_barrier_recovery_recover.
func executeBarrierRecoveryRecover(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	keys := parseBarrierShareSlice(args, "keys")
	if len(keys) == 0 {
		return nil, &BarrierRecoveryKeysRequiredError{}
	}
	if err := svc.BarrierRecoverWithKeys(ctx, keys); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "recovered"}, nil
}

// executeBarrierRecoveryDelete implements qrdb_barrier_recovery_delete.
func executeBarrierRecoveryDelete(ctx context.Context, svc service.BarrierServicer, _ map[string]interface{}) (interface{}, error) {
	if err := svc.BarrierDeleteRecoveryKeys(ctx); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "deleted"}, nil
}

// executeBarrierRecoveryStatus implements qrdb_barrier_recovery_status.
func executeBarrierRecoveryStatus(ctx context.Context, svc service.BarrierServicer, _ map[string]interface{}) (interface{}, error) {
	hasKeys, err := svc.BarrierHasRecoveryKeys(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"has_recovery_keys": hasKeys}, nil
}

// executeBarrierRootToken implements qrdb_barrier_root_token.
func executeBarrierRootToken(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	shares := parseBarrierShareSlice(args, "shares")
	if len(shares) == 0 {
		return nil, &BarrierSharesRequiredError{}
	}
	info, err := svc.BarrierGenerateRootToken(ctx, shares)
	if err != nil {
		return nil, err
	}
	return rootTokenToMap(info), nil
}

// ---------------------------------------------------------------------------
// Helpers — argument parsing and response mapping
// ---------------------------------------------------------------------------

// validateBarrierThreshold enforces the threshold <= total invariant shared
// by the shamir-initialize, rekey, and recovery-generate tools.
func validateBarrierThreshold(threshold, total int) error {
	if threshold <= 0 {
		return &BarrierThresholdInvalidError{Reason: "threshold must be greater than 0"}
	}
	if total <= 0 {
		return &BarrierThresholdInvalidError{Reason: "total must be greater than 0"}
	}
	if threshold > total {
		return &BarrierThresholdInvalidError{Reason: "threshold cannot exceed total"}
	}
	return nil
}

// parseBarrierShareSlice extracts a list of share/key strings from the args
// map under the supplied key. The MCP transport may deliver these as either
// a JSON array (preferred) or a comma-separated string; both are accepted so
// the same tool surface is usable from CLI shells and structured callers.
func parseBarrierShareSlice(args map[string]interface{}, key string) []string {
	if values := getStringSlice(args, key); len(values) > 0 {
		out := make([]string, 0, len(values))
		for _, v := range values {
			trimmed := strings.TrimSpace(v)
			if trimmed != "" {
				out = append(out, trimmed)
			}
		}
		return out
	}
	raw := getString(args, key)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// barrierStatusToMap renders a BarrierStatusInfo as the JSON wire shape
// shared with handlers_barrier.go.
func barrierStatusToMap(info *service.BarrierStatusInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"sealed":          info.Sealed,
		"strategy":        info.Strategy,
		"hardware_backed": info.HardwareBacked,
	}
	if !info.InitializedAt.IsZero() {
		resp["initialized_at"] = info.InitializedAt.UTC().Format(time.RFC3339)
	}
	return resp
}

// shamirInitToMap renders a ShamirInitInfo as the JSON wire shape shared with
// handlers_barrier.go.
func shamirInitToMap(info *service.ShamirInitInfo) map[string]interface{} {
	return map[string]interface{}{
		"shares":       info.Shares,
		"threshold":    info.Threshold,
		"total_shares": info.TotalShares,
	}
}

// unsealShareProgressToMap renders an UnsealShareProgress as the JSON wire
// shape shared with handlers_barrier.go.
func unsealShareProgressToMap(p *service.UnsealShareProgress) map[string]interface{} {
	return map[string]interface{}{
		"required":  p.Required,
		"submitted": p.Submitted,
		"complete":  p.Complete,
	}
}

// recoveryKeysToMap renders a RecoveryKeysInfo as the JSON wire shape shared
// with handlers_barrier.go.
func recoveryKeysToMap(info *service.RecoveryKeysInfo) map[string]interface{} {
	return map[string]interface{}{
		"keys":      info.Keys,
		"threshold": info.Threshold,
		"total":     info.Total,
	}
}

// rootTokenToMap renders a RootTokenInfo as the JSON wire shape shared with
// handlers_barrier.go.
func rootTokenToMap(info *service.RootTokenInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"token": info.Token,
	}
	if !info.CreatedAt.IsZero() {
		resp["created_at"] = info.CreatedAt.UTC().Format(time.RFC3339)
	}
	return resp
}
