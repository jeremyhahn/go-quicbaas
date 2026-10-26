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
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"

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
//   qrdb_barrier_recovery_generate
//   qrdb_barrier_recovery_delete
//   qrdb_barrier_recovery_status
//
// plus the share-holder tools in tools_barrier_holders.go. Shares are never
// typed in: holders claim them and submit through an authenticated quorum.
//
// All tools delegate to a BarrierServicer installed via
// ToolRegistry.SetBarrierService. When the service is unwired, every tool
// returns BarrierServiceUnavailableError so MCP clients see a typed
// "service unavailable" condition rather than a nil-pointer panic.
// =============================================================================

// mcpTenantGovernancer applies engine-side quota and entitlement limits to
// tenants via the iam.DefaultTenantManager delegate. Defined locally to avoid
// an import cycle with pkg/iam from the mcp package.
type mcpTenantGovernancer interface {
	UpdateEngineQuotas(ctx context.Context, id string, quotas qrdbsdk.Quotas) error
	UpdateEngineEntitlements(ctx context.Context, id string, entitlements qrdbsdk.Entitlements) error
}

// errBarrierServiceUnavailableQB is the canonical QBError returned when the
// barrier surface is not wired. Returning a QBError with the ErrUnavailable
// code lets pkg/api/mcp/server.go's mapErrorToJSONRPCError classify the
// condition through its explicit-code branch (ServiceUnavailable) instead of
// the generic "Tool execution failed" default, which previously surfaced to
// CLI callers as a misleading internal error.
var errBarrierServiceUnavailableQB = &qberrors.QBError{
	Code: qberrors.ErrUnavailable,
	Op:   "mcp.barrier",
	Err:  &BarrierServiceUnavailableError{},
}

// BarrierServiceUnavailableError is returned by every barrier tool when the
// BarrierServicer has not been wired into the ToolRegistry. This mirrors the
// REST 503-on-nil-facade behaviour so clients of either transport can rely on
// the same typed unavailability signal during early boot or test setups. It is
// always wrapped in errBarrierServiceUnavailableQB before being returned so
// the MCP error mapper classifies it as ServiceUnavailable.
type BarrierServiceUnavailableError struct{}

// Error implements the error interface. The message contains "unavailable" so
// that even when the value is unwrapped from its QBError envelope the MCP
// mapper's message-pattern fallback still classifies it correctly.
func (e *BarrierServiceUnavailableError) Error() string {
	return "barrier service unavailable: not configured"
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

// BarrierTargetStrategyRequiredError is returned when enable-encryption is
// called without the required target_strategy argument.
type BarrierTargetStrategyRequiredError struct{}

// Error implements the error interface.
func (e *BarrierTargetStrategyRequiredError) Error() string {
	return "target_strategy is required"
}

// BarrierTenantIDRequiredError is returned when a barrier tool that targets
// a specific tenant is called without the required tenant_id argument. It is
// the barrier-surface analogue of TenantBarrierTenantIDRequiredError; both
// convey the same constraint but from different tool families.
type BarrierTenantIDRequiredError struct{}

// Error implements the error interface.
func (e *BarrierTenantIDRequiredError) Error() string {
	return "tenant_id is required"
}

// GovernanceServiceUnavailableError is returned by the governance tools when the
// mcpTenantGovernancer has not been wired into the ToolRegistry. This mirrors
// the REST 503-on-nil-facade behaviour.
type GovernanceServiceUnavailableError struct{}

// Error implements the error interface.
func (e *GovernanceServiceUnavailableError) Error() string {
	return "governance service unavailable: not configured"
}

// errGovernanceServiceUnavailableQB is the canonical QBError returned when
// the governance surface is not wired.
var errGovernanceServiceUnavailableQB = &qberrors.QBError{
	Code: qberrors.ErrUnavailable,
	Op:   "mcp.governance",
	Err:  &GovernanceServiceUnavailableError{},
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
			"type": "object",
			"properties": map[string]interface{}{
				"so_pin": map[string]interface{}{
					"type":        "string",
					"description": "Security Officer PIN. Required when a PIN backend with an SO PIN is configured.",
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_shamir_initialize",
		Description: "Initialise the barrier with Shamir secret sharing. Share material is never returned (qrdb share-free contract); holders claim their shares through the identity-gated claim tools.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
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
				"policy":   map[string]interface{}{"type": "string", "description": argHolderPolicy.description},
				"so_pin":   map[string]interface{}{"type": "string", "description": argHolderSOPin.description},
				"user_pin": map[string]interface{}{"type": "string", "description": "User PIN verified against the node's PIN backend."},
			},
			"required": []string{"threshold", "total_shares"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_barrier_recovery_generate",
		Description: "Generate the recovery key's share set for disaster recovery. Share material is never returned; recovery holders claim their shares through the identity-gated claim tools.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Minimum number of recovery shares required for recovery.",
					"minimum":     1,
				},
				"policy": map[string]interface{}{"type": "string", "description": argHolderPolicy.description},
				"so_pin": map[string]interface{}{"type": "string", "description": argHolderSOPin.description},
			},
			"required": []string{"threshold"},
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
		Name:        "qrdb_barrier_enable_encryption",
		Description: "Migrate the system barrier from the none strategy to a keyed at-rest encryption strategy. This is a one-way, in-place migration that requires the embedded qrdb node.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"target_strategy": map[string]interface{}{
					"type":        "string",
					"description": "Target sealing strategy: passphrase or shamir.",
				},
				"mechanism": map[string]interface{}{
					"type":        "string",
					"description": "Migration mechanism: swap (default) or copy.",
				},
				"availability": map[string]interface{}{
					"type":        "string",
					"description": "Migration availability: online (default) or offline.",
				},
				"passphrase": map[string]interface{}{
					"type":        "string",
					"description": "Passphrase for the passphrase strategy. Required when target_strategy is passphrase.",
				},
				"shamir_threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Shamir threshold (minimum shares required). Required when target_strategy is shamir.",
					"minimum":     1,
				},
				"shamir_total": map[string]interface{}{
					"type":        "integer",
					"description": "Total number of Shamir shares to generate. Required when target_strategy is shamir.",
					"minimum":     1,
				},
			},
			"required": []string{"target_strategy"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_tenant_barrier_enable_encryption",
		Description: "Migrate a tenant barrier from the none strategy to a keyed at-rest encryption strategy. This is a one-way, in-place migration that requires the embedded qrdb node.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "The tenant whose barrier to migrate.",
				},
				"target_strategy": map[string]interface{}{
					"type":        "string",
					"description": "Target sealing strategy: passphrase or shamir.",
				},
				"mechanism": map[string]interface{}{
					"type":        "string",
					"description": "Migration mechanism: swap (default) or copy.",
				},
				"availability": map[string]interface{}{
					"type":        "string",
					"description": "Migration availability: online (default) or offline.",
				},
				"passphrase": map[string]interface{}{
					"type":        "string",
					"description": "Passphrase for the passphrase strategy. Required when target_strategy is passphrase.",
				},
				"shamir_threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Shamir threshold (minimum shares required). Required when target_strategy is shamir.",
					"minimum":     1,
				},
				"shamir_total": map[string]interface{}{
					"type":        "integer",
					"description": "Total number of Shamir shares to generate. Required when target_strategy is shamir.",
					"minimum":     1,
				},
			},
			"required": []string{"tenant_id", "target_strategy"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_tenant_update_quotas",
		Description: "Update the engine-level numeric resource limits for a tenant. A limit value of -1 means unlimited; 0 means use the system default.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "The tenant whose quotas to update.",
				},
				"max_databases": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of databases. -1 = unlimited; 0 = system default.",
				},
				"max_raft_groups": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of dedicated Raft groups. -1 = unlimited; 0 = system default.",
				},
				"max_replicas_per_group": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum replication factor per Raft group. -1 = unlimited; 0 = system default.",
				},
				"max_total_replicas": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum aggregate replicas across all Raft groups. -1 = unlimited; 0 = system default.",
				},
				"max_collections": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of collections. -1 = unlimited; 0 = system default.",
				},
				"max_keys": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of keys. -1 = unlimited; 0 = system default.",
				},
				"max_storage_bytes": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum total storage in bytes. -1 = unlimited; 0 = system default.",
				},
				"max_database_size_bytes": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum size of any single database in bytes. -1 = unlimited; 0 = system default.",
				},
				"max_users": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of users. -1 = unlimited; 0 = system default.",
				},
				"max_roles": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of roles. -1 = unlimited; 0 = system default.",
				},
				"max_certificates": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of TLS certificates. -1 = unlimited; 0 = system default.",
				},
				"max_backups": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of backups. -1 = unlimited; 0 = system default.",
				},
				"max_vector_dims": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of vector dimensions. -1 = unlimited; 0 = system default.",
				},
				"max_ts_metrics": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of time-series metrics. -1 = unlimited; 0 = system default.",
				},
				"max_dedicated_wal_groups": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of dedicated WAL groups. -1 = unlimited; 0 = system default.",
				},
				"system_barrier_quota": map[string]interface{}{
					"type":        "integer",
					"description": "System-barrier usage quota in bytes. 0 = disabled; -1 = unlimited.",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_tenant_update_entitlements",
		Description: "Update the engine-level feature entitlements for a tenant, controlling which isolation modes, barrier strategies, and optional features the tenant may use.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "The tenant whose entitlements to update.",
				},
				"allowed_isolation_modes": map[string]interface{}{
					"type":        "array",
					"description": "Isolation modes the tenant may use (e.g. barrier, volume, raft). Empty means all modes are permitted.",
					"items":       map[string]interface{}{"type": "string"},
				},
				"allow_dedicated_raft": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the tenant may provision dedicated Raft-backed databases.",
				},
				"allow_volume_storage": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the tenant may provision databases on a dedicated volume.",
				},
				"allow_system_managed_barrier": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the tenant may opt into the system-managed barrier.",
				},
				"allow_dedicated_wal": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the tenant may opt a dedicated-raft database into a per-tenant-key WAL.",
				},
				"allowed_barrier_modes": map[string]interface{}{
					"type":        "array",
					"description": "Barrier sealing strategies available to the tenant for its dedicated barrier. Empty means software strategies are permitted.",
					"items":       map[string]interface{}{"type": "string"},
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	registerHolderTools(r, barrierHolderTools)
}

// barrierToolNames enumerates every tool name owned by the barrier surface.
// Membership is used by isBarrierTool to route dispatch from the registry's
// default branch into executeBarrierTool, avoiding a giant per-name switch in
// the parent dispatcher.
var barrierToolNames = map[string]struct{}{
	"qrdb_barrier_status":                   {},
	"qrdb_barrier_initialize":               {},
	"qrdb_barrier_unseal":                   {},
	"qrdb_barrier_seal":                     {},
	"qrdb_barrier_shamir_initialize":        {},
	"qrdb_barrier_recovery_generate":        {},
	"qrdb_barrier_recovery_delete":          {},
	"qrdb_barrier_recovery_status":          {},
	"qrdb_barrier_enable_encryption":        {},
	"qrdb_tenant_barrier_enable_encryption": {},
	"qrdb_tenant_update_quotas":             {},
	"qrdb_tenant_update_entitlements":       {},
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
	"qrdb_barrier_status":                   executeBarrierStatus,
	"qrdb_barrier_initialize":               executeBarrierInitialize,
	"qrdb_barrier_unseal":                   executeBarrierUnseal,
	"qrdb_barrier_seal":                     executeBarrierSeal,
	"qrdb_barrier_shamir_initialize":        executeBarrierShamirInitialize,
	"qrdb_barrier_recovery_generate":        executeBarrierRecoveryGenerate,
	"qrdb_barrier_recovery_delete":          executeBarrierRecoveryDelete,
	"qrdb_barrier_recovery_status":          executeBarrierRecoveryStatus,
	"qrdb_barrier_enable_encryption":        executeBarrierEnableEncryption,
	"qrdb_tenant_barrier_enable_encryption": executeBarrierTenantEnableEncryption,
}

// governanceExecutor is the function signature for tools that require the
// mcpTenantGovernancer rather than the BarrierServicer.
type governanceExecutor func(context.Context, mcpTenantGovernancer, map[string]interface{}) (interface{}, error)

// governanceExecutors is the dispatch table for the two tenant-governance tools.
var governanceExecutors = map[string]governanceExecutor{
	"qrdb_tenant_update_quotas":       executeUpdateTenantQuotas,
	"qrdb_tenant_update_entitlements": executeUpdateTenantEntitlements,
}

// executeBarrierTool dispatches a barrier-surface call to the appropriate
// executor. Governance tools (quota/entitlement updates) require the
// mcpTenantGovernancer and are dispatched from the governanceExecutors table;
// all other barrier tools require the BarrierServicer. Returns the relevant
// unavailable error when the required service is not wired.
func (r *ToolRegistry) executeBarrierTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	// Governance tools take a different service than the BarrierServicer.
	if govExec, ok := governanceExecutors[name]; ok {
		gov := r.tenantGovernance
		if gov == nil {
			return nil, errGovernanceServiceUnavailableQB
		}
		return govExec(ctx, gov, args)
	}

	svc := r.barrierSvc
	if svc == nil {
		return nil, errBarrierServiceUnavailableQB
	}
	executor, ok := barrierExecutors[name]
	if !ok {
		// Should not happen — isBarrierTool gates entry. Defensive return.
		return nil, errBarrierServiceUnavailableQB
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
	secret := credentialArg(args, "secret")
	defer secure.Wipe(secret)
	if err := svc.BarrierInitialize(ctx, secret); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "initialized"}, nil
}

// executeBarrierUnseal implements qrdb_barrier_unseal.
func executeBarrierUnseal(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	secret := credentialArg(args, "secret")
	defer secure.Wipe(secret)
	if len(secret) == 0 {
		return nil, &BarrierThresholdInvalidError{Reason: "secret is required"}
	}
	if err := svc.BarrierUnseal(ctx, secret); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "unsealed"}, nil
}

// executeBarrierSeal implements qrdb_barrier_seal.
func executeBarrierSeal(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	soPIN := []byte(getString(args, "so_pin"))
	defer secure.Wipe(soPIN)
	if err := svc.BarrierSeal(ctx, &service.BarrierSealRequest{SOPIN: soPIN}); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "sealed"}, nil
}

// executeBarrierShamirInitialize implements qrdb_barrier_shamir_initialize.
func executeBarrierShamirInitialize(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	threshold := getInt(args, "threshold")
	total := getInt(args, "total_shares")
	if err := validateBarrierThreshold(threshold, total); err != nil {
		return nil, err
	}
	return holderVerb(ctx, args, svc.BarrierInitializeShamir)
}

// executeBarrierRecoveryGenerate implements qrdb_barrier_recovery_generate.
func executeBarrierRecoveryGenerate(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	if getInt(args, "threshold") <= 0 {
		return nil, &BarrierThresholdInvalidError{Reason: "threshold must be greater than 0"}
	}
	return holderVerb(ctx, args, svc.BarrierGenerateRecoveryKeys)
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

// executeBarrierEnableEncryption implements qrdb_barrier_enable_encryption.
// It migrates the SYSTEM barrier from none-strategy to a keyed at-rest
// encryption strategy in place.
func executeBarrierEnableEncryption(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	targetStrategy := getString(args, "target_strategy")
	if targetStrategy == "" {
		return nil, &BarrierTargetStrategyRequiredError{}
	}
	opts, err := buildMigrationOptions(ctx, targetStrategy, args)
	if err != nil {
		return nil, err
	}
	// tenantID="" routes to the system barrier scope inside EnableEncryption.
	if err := enableEncryptionAndDestroy(ctx, svc, "", opts); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "encryption enabled"}, nil
}

// executeBarrierTenantEnableEncryption implements
// qrdb_tenant_barrier_enable_encryption. It migrates a TENANT barrier from
// none-strategy to a keyed at-rest encryption strategy in place.
func executeBarrierTenantEnableEncryption(ctx context.Context, svc service.BarrierServicer, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &BarrierTenantIDRequiredError{}
	}
	targetStrategy := getString(args, "target_strategy")
	if targetStrategy == "" {
		return nil, &BarrierTargetStrategyRequiredError{}
	}
	opts, err := buildMigrationOptions(ctx, targetStrategy, args)
	if err != nil {
		return nil, err
	}
	if err := enableEncryptionAndDestroy(ctx, svc, tenantID, opts); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status":    "encryption enabled",
		"tenant_id": tenantID,
	}, nil
}

// enableEncryptionAndDestroy runs the barrier migration and then destroys
// the protected-memory secret opts carries. The migration error wins; a
// destroy failure is returned when the migration itself succeeded, because
// a secret that could not be destroyed is still resident.
func enableEncryptionAndDestroy(ctx context.Context, svc service.BarrierServicer, tenantID string, opts qrdbsdk.MigrationOptions) error {
	err := svc.EnableEncryption(ctx, tenantID, opts)
	if opts.Credentials.Secret != nil {
		if destroyErr := opts.Credentials.Secret.Destroy(); destroyErr != nil && err == nil {
			return destroyErr
		}
	}
	return err
}

// buildMigrationOptions constructs a qrdbsdk.MigrationOptions from the tool
// args map, applying defaults for mechanism and availability when omitted.
func buildMigrationOptions(ctx context.Context, targetStrategy string, args map[string]interface{}) (qrdbsdk.MigrationOptions, error) {
	mech := getString(args, "mechanism")
	if mech == "" {
		mech = string(qrdbsdk.MechanismSwap)
	}
	avail := getString(args, "availability")
	if avail == "" {
		avail = string(qrdbsdk.AvailabilityOnline)
	}
	opts := qrdbsdk.MigrationOptions{
		TargetStrategy:  qrdbsdk.StrategyID(targetStrategy),
		Mechanism:       qrdbsdk.MigrationMechanism(mech),
		Availability:    qrdbsdk.MigrationAvailability(avail),
		ShamirThreshold: getInt(args, "shamir_threshold"),
		ShamirTotal:     getInt(args, "shamir_total"),
	}
	if passphrase := credentialArg(args, "passphrase"); len(passphrase) > 0 {
		defer secure.Wipe(passphrase)
		creds, err := qrdbsdk.CredentialsFor(ctx, passphrase)
		if err != nil {
			return qrdbsdk.MigrationOptions{}, err
		}
		// Ownership passes to the caller, which destroys it once the
		// migration it was built for has returned.
		opts.Credentials = creds
	}
	return opts, nil
}

// executeUpdateTenantQuotas implements qrdb_tenant_update_quotas. It maps the
// args to qrdbsdk.Quotas and delegates to the governance manager.
func executeUpdateTenantQuotas(ctx context.Context, gov mcpTenantGovernancer, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &BarrierTenantIDRequiredError{}
	}
	quotas := qrdbsdk.Quotas{
		MaxDatabases:          int64(getInt(args, "max_databases")),
		MaxRaftGroups:         int64(getInt(args, "max_raft_groups")),
		MaxReplicasPerGroup:   int64(getInt(args, "max_replicas_per_group")),
		MaxTotalReplicas:      int64(getInt(args, "max_total_replicas")),
		MaxCollections:        int64(getInt(args, "max_collections")),
		MaxKeys:               int64(getInt(args, "max_keys")),
		MaxStorageBytes:       int64(getInt(args, "max_storage_bytes")),
		MaxDatabaseSizeBytes:  int64(getInt(args, "max_database_size_bytes")),
		MaxUsers:              int64(getInt(args, "max_users")),
		MaxRoles:              int64(getInt(args, "max_roles")),
		MaxCertificates:       int64(getInt(args, "max_certificates")),
		MaxBackups:            int64(getInt(args, "max_backups")),
		MaxVectorDims:         int64(getInt(args, "max_vector_dims")),
		MaxTSMetrics:          int64(getInt(args, "max_ts_metrics")),
		MaxDedicatedWALGroups: int64(getInt(args, "max_dedicated_wal_groups")),
		SystemBarrierQuota:    int64(getInt(args, "system_barrier_quota")),
	}
	if err := gov.UpdateEngineQuotas(ctx, tenantID, quotas); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "ok"}, nil
}

// executeUpdateTenantEntitlements implements qrdb_tenant_update_entitlements.
// It maps the args to qrdbsdk.Entitlements and delegates to the governance manager.
func executeUpdateTenantEntitlements(ctx context.Context, gov mcpTenantGovernancer, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &BarrierTenantIDRequiredError{}
	}
	entitlements := qrdbsdk.Entitlements{
		AllowedIsolationModes:     getStringSlice(args, "allowed_isolation_modes"),
		AllowDedicatedRaft:        getBool(args, "allow_dedicated_raft"),
		AllowVolumeStorage:        getBool(args, "allow_volume_storage"),
		AllowSystemManagedBarrier: getBool(args, "allow_system_managed_barrier"),
		AllowDedicatedWAL:         getBool(args, "allow_dedicated_wal"),
		AllowedBarrierModes:       getStringSlice(args, "allowed_barrier_modes"),
	}
	if err := gov.UpdateEngineEntitlements(ctx, tenantID, entitlements); err != nil {
		return nil, err
	}
	return map[string]interface{}{"status": "ok"}, nil
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

// barrierTimeFormat is the ONE rendering every barrier timestamp uses on
// every transport. It is RFC3339Nano, not RFC3339, because go-qrdb's own
// qrdb.v1 gRPC barrier surface — which quicbaas's unix and gRPC transports
// reach through the platform-shard servicer adapter — already renders
// InitializedAt at nanosecond precision. Rendering the same instant at
// second precision here made `barrier status` answer
// "2026-08-25T04:40:05Z" over REST/MCP and "2026-08-25T04:40:05.727835519Z"
// over unix/gRPC for one barrier: not a contradiction, but not
// byte-comparable either, and go-quicbaas#405 requires an operator
// diffing two transports to get the same answer.
const barrierTimeFormat = time.RFC3339Nano

// barrierStatusToMap renders a BarrierStatusInfo as the JSON wire shape
// shared with handlers_barrier.go.
func barrierStatusToMap(info *service.BarrierStatusInfo) map[string]interface{} {
	resp := map[string]interface{}{
		"sealed":          info.Sealed,
		"initialized":     info.Initialized,
		"strategy":        info.Strategy,
		"hardware_backed": info.HardwareBacked,
	}
	if !info.InitializedAt.IsZero() {
		resp["initialized_at"] = info.InitializedAt.UTC().Format(barrierTimeFormat)
	}
	return resp
}

// credentialArg reads the barrier credential argument name and copies it into
// a slice the caller wipes after use. It is the one place MCP turns a
// credential string into bytes. MCP is go-qrdb's documented exception to the
// wipeable-credential contract: a tool argument is a JSON string by definition
// of the protocol, so the plaintext necessarily exists as an unwipeable Go
// string before this point. Converting narrows what follows; for MCP alone the
// residue is not removable. Every other transport carries the credential as
// bytes end to end.
func credentialArg(args map[string]interface{}, name string) []byte {
	s := getString(args, name)
	if s == "" {
		return nil
	}
	return []byte(s)
}
