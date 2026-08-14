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
	stderrors "errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Per-tenant barrier MCP tools — qrdb_tenant_barrier_* surface.
//
// Mirrors the REST handlers in pkg/api/rest/handlers_tenant_barrier.go so the
// MCP transport exposes an identical per-tenant barrier-lifecycle contract:
//
//   qrdb_tenant_barrier_init
//   qrdb_tenant_barrier_unseal
//   qrdb_tenant_barrier_unseal_share
//   qrdb_tenant_barrier_seal
//   qrdb_tenant_barrier_status
//
// All tools delegate to a TenantBarrierServicer installed via
// ToolRegistry.SetTenantBarrierService. When the service is unwired, every tool
// returns TenantBarrierServiceUnavailableError so MCP clients see a typed
// "service unavailable" condition rather than a nil-pointer panic. Share
// material is proxied straight through and never logged.
// =============================================================================

// errTenantBarrierServiceUnavailableQB is the canonical QBError returned when
// the per-tenant barrier surface is not wired.
var errTenantBarrierServiceUnavailableQB = &qberrors.QBError{
	Code: qberrors.ErrUnavailable,
	Op:   "mcp.tenant_barrier",
	Err:  &TenantBarrierServiceUnavailableError{},
}

// TenantBarrierServiceUnavailableError is returned by every per-tenant barrier
// tool when the TenantBarrierServicer has not been wired into the ToolRegistry.
// It mirrors BarrierServiceUnavailableError from tools_barrier.go so clients
// of either surface can rely on the same typed unavailability signal.
type TenantBarrierServiceUnavailableError struct{}

// Error implements the error interface.
func (e *TenantBarrierServiceUnavailableError) Error() string {
	return "tenant barrier service unavailable: not configured"
}

// TenantBarrierTenantIDRequiredError is returned when a tool is called without
// the required tenant_id argument.
type TenantBarrierTenantIDRequiredError struct{}

// Error implements the error interface.
func (e *TenantBarrierTenantIDRequiredError) Error() string {
	return "tenant_id is required"
}

// TenantBarrierShareRequiredError is returned when the unseal_share tool is
// called without the required share argument.
type TenantBarrierShareRequiredError struct{}

// Error implements the error interface.
func (e *TenantBarrierShareRequiredError) Error() string {
	return "share is required"
}

// registerTenantBarrierTools adds every qrdb_tenant_barrier_* tool definition
// to the registry. The tools are dispatched via isTenantBarrierTool /
// executeTenantBarrierTool.
func (r *ToolRegistry) registerTenantBarrierTools() {
	r.addTool(ToolDefinition{
		Name:        "qrdb_tenant_barrier_init",
		Description: "Initialize the per-tenant barrier with the selected sealing strategy (none, passphrase, or shamir). Share material is never returned (qrdb share-free contract); the response reports only threshold/total metadata.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "The tenant whose barrier to initialize.",
				},
				"strategy": map[string]interface{}{
					"type":        "string",
					"description": "Sealing strategy: none, passphrase, or shamir.",
				},
				"passphrase": map[string]interface{}{
					"type":        "string",
					"description": "Passphrase for the passphrase strategy. Omit for other strategies.",
				},
				"threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Shamir threshold (minimum shares required). Only for the shamir strategy.",
					"minimum":     1,
				},
				"shares": map[string]interface{}{
					"type":        "integer",
					"description": "Total number of Shamir shares to generate. Only for the shamir strategy.",
					"minimum":     1,
				},
			},
			"required": []string{"tenant_id", "strategy"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_tenant_barrier_unseal",
		Description: "Unseal the per-tenant barrier with a single key (passphrase strategy). The key must be base64-encoded.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "The tenant whose barrier to unseal.",
				},
				"key": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded unseal key.",
				},
			},
			"required": []string{"tenant_id", "key"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_tenant_barrier_unseal_share",
		Description: "Submit a single Shamir share toward the per-tenant barrier quorum threshold. Returns quorum progress including whether the barrier was unsealed.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "The tenant whose barrier to unseal.",
				},
				"share": map[string]interface{}{
					"type":        "string",
					"description": "A single Shamir share value.",
				},
			},
			"required": []string{"tenant_id", "share"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_tenant_barrier_seal",
		Description: "Seal the per-tenant barrier, transitioning it back to the sealed state.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "The tenant whose barrier to seal.",
				},
			},
			"required": []string{"tenant_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "qrdb_tenant_barrier_status",
		Description: "Report the per-tenant barrier state including sealed/initialized status, active strategy, and Shamir threshold/total when applicable.",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "The tenant whose barrier status to query.",
				},
			},
			"required": []string{"tenant_id"},
		},
	})
}

// tenantBarrierToolNames enumerates every tool name owned by the per-tenant
// barrier surface. Membership is used by isTenantBarrierTool to route dispatch.
var tenantBarrierToolNames = map[string]struct{}{
	"qrdb_tenant_barrier_init":         {},
	"qrdb_tenant_barrier_unseal":       {},
	"qrdb_tenant_barrier_unseal_share": {},
	"qrdb_tenant_barrier_seal":         {},
	"qrdb_tenant_barrier_status":       {},
}

// isTenantBarrierTool reports whether the given tool name belongs to the
// per-tenant barrier surface. Called from ToolRegistry.ExecuteTool's default
// branch.
func isTenantBarrierTool(name string) bool {
	_, ok := tenantBarrierToolNames[name]
	return ok
}

// tenantBarrierExecutor is the signature each per-tool handler implements.
type tenantBarrierExecutor func(context.Context, service.TenantBarrierServicer, map[string]interface{}) (interface{}, error)

// tenantBarrierExecutors is the map-based dispatch table for tenant barrier
// tools. Lookup is O(1) and keeps executeTenantBarrierTool free of a multi-arm
// switch.
var tenantBarrierExecutors = map[string]tenantBarrierExecutor{
	"qrdb_tenant_barrier_init":         executeTenantBarrierInit,
	"qrdb_tenant_barrier_unseal":       executeTenantBarrierUnseal,
	"qrdb_tenant_barrier_unseal_share": executeTenantBarrierUnsealShare,
	"qrdb_tenant_barrier_seal":         executeTenantBarrierSeal,
	"qrdb_tenant_barrier_status":       executeTenantBarrierStatus,
}

// executeTenantBarrierTool dispatches a per-tenant barrier call to the
// appropriate executor. Returns errTenantBarrierServiceUnavailableQB when the
// registry has no TenantBarrierServicer wired, mirroring the REST 503-on-nil
// semantics.
func (r *ToolRegistry) executeTenantBarrierTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	svc := r.tenantBarrierSvc
	if svc == nil {
		return nil, errTenantBarrierServiceUnavailableQB
	}
	executor, ok := tenantBarrierExecutors[name]
	if !ok {
		// Should not happen — isTenantBarrierTool gates entry. Defensive return.
		return nil, errTenantBarrierServiceUnavailableQB
	}
	result, err := executor(ctx, svc, args)
	if err != nil {
		return nil, mapTenantBarrierToolError(err)
	}
	return result, nil
}

// mapTenantBarrierToolError classifies qrdb sentinel errors into typed QBErrors
// so mapErrorToJSONRPCError emits the canonical JSON-RPC code. The qrdb error
// canon maps tenant.ErrBarrierNotEnabled (per-tenant barrier verb on a node
// whose seal subsystem is disabled) to the failed-precondition class — 422 on
// REST, codes.FailedPrecondition on gRPC — so the MCP transport surfaces the
// PreconditionFailed JSON-RPC code for the same condition. The original error
// is preserved as the QBError cause so the canonical message survives on the
// wire. Every other error passes through unchanged.
func mapTenantBarrierToolError(err error) error {
	if stderrors.Is(err, qrdbsdk.ErrTenantBarrierNotEnabled) {
		return &qberrors.QBError{
			Code: qberrors.ErrPreconditionFailed,
			Op:   "mcp.tenant_barrier",
			Err:  err,
		}
	}
	return err
}

// ---------------------------------------------------------------------------
// Per-tool executors
// ---------------------------------------------------------------------------

// executeTenantBarrierInit implements qrdb_tenant_barrier_init.
func executeTenantBarrierInit(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &TenantBarrierTenantIDRequiredError{}
	}

	result, err := svc.TenantBarrierInit(
		ctx,
		tenantID,
		getString(args, "passphrase"),
		getInt(args, "threshold"),
		getInt(args, "shares"),
		getString(args, "strategy"),
	)
	if err != nil {
		return nil, err
	}

	// Share-free contract: share bytes are never returned over the wire;
	// officers claim their shares individually via the share-claim flow.
	if result != nil && result.TotalShares > 0 {
		return map[string]interface{}{
			"tenant_id":    result.TenantID,
			"strategy":     result.Strategy,
			"threshold":    result.Threshold,
			"total_shares": result.TotalShares,
			"share_count":  result.ShareCount,
		}, nil
	}
	return map[string]interface{}{
		"status":    "initialized",
		"tenant_id": tenantID,
	}, nil
}

// executeTenantBarrierUnseal implements qrdb_tenant_barrier_unseal.
func executeTenantBarrierUnseal(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &TenantBarrierTenantIDRequiredError{}
	}

	keyStr := getString(args, "key")
	var key []byte
	if keyStr != "" {
		var err error
		key, err = base64.StdEncoding.DecodeString(keyStr)
		if err != nil {
			// Accept raw bytes if not valid base64, matching REST behaviour where
			// encoding/json decodes []byte as base64 but the caller may supply raw.
			key = []byte(keyStr)
		}
	}

	if err := svc.TenantBarrierUnseal(ctx, tenantID, key); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status":    "unsealed",
		"tenant_id": tenantID,
	}, nil
}

// executeTenantBarrierUnsealShare implements qrdb_tenant_barrier_unseal_share.
func executeTenantBarrierUnsealShare(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &TenantBarrierTenantIDRequiredError{}
	}
	share := getString(args, "share")
	if share == "" {
		return nil, &TenantBarrierShareRequiredError{}
	}

	progress, err := svc.TenantBarrierUnsealShare(ctx, tenantID, share)
	if err != nil {
		return nil, err
	}
	if progress == nil {
		return map[string]interface{}{
			"tenant_id": tenantID,
			"required":  0,
			"submitted": 0,
			"complete":  false,
			"sealed":    true,
		}, nil
	}
	return map[string]interface{}{
		"tenant_id": progress.TenantID,
		"required":  progress.Required,
		"submitted": progress.Submitted,
		"complete":  progress.Complete,
		"sealed":    progress.Sealed,
	}, nil
}

// executeTenantBarrierSeal implements qrdb_tenant_barrier_seal.
func executeTenantBarrierSeal(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &TenantBarrierTenantIDRequiredError{}
	}

	if err := svc.TenantBarrierSeal(ctx, tenantID); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"status":    "sealed",
		"tenant_id": tenantID,
	}, nil
}

// executeTenantBarrierStatus implements qrdb_tenant_barrier_status.
func executeTenantBarrierStatus(ctx context.Context, svc service.TenantBarrierServicer, args map[string]interface{}) (interface{}, error) {
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, &TenantBarrierTenantIDRequiredError{}
	}

	info, err := svc.TenantBarrierStatus(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if info == nil {
		return map[string]interface{}{
			"tenant_id": tenantID,
		}, nil
	}
	return map[string]interface{}{
		"tenant_id":        info.TenantID,
		"sealed":           info.Sealed,
		"initialized":      info.Initialized,
		"strategy":         info.Strategy,
		"hardware_backed":  info.HardwareBacked,
		"shamir_threshold": info.ShamirThreshold,
		"shamir_total":     info.ShamirTotal,
	}, nil
}
