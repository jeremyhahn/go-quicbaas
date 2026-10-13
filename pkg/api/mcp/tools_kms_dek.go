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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerDEKTools registers Data Encryption Key management MCP tools.
// DEKs provide hierarchical encryption key management for multi-tenant isolation.
func (r *ToolRegistry) registerDEKTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_create",
		Description: "Create a new Data Encryption Key (DEK) at the specified path for hierarchical encryption",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "Hierarchical path for the DEK (e.g., '/', 'tenant-123/', 'tenant-123/prod/app/')",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Encryption algorithm (default: AES-256-GCM)",
					"enum":        []string{"AES-128-GCM", "AES-256-GCM", "CHACHA20-POLY1305"},
					"default":     "AES-256-GCM",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable description of the DEK purpose",
				},
				"max_operations": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum encrypt operations before rotation required (0 = unlimited, NIST recommends 2^32)",
				},
				"max_bytes": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum bytes encrypted before rotation required (0 = unlimited)",
				},
				"warning_percent": map[string]interface{}{
					"type":        "integer",
					"description": "Percent of limit at which to warn (default: 80)",
					"minimum":     1,
					"maximum":     100,
					"default":     80,
				},
			},
			"required": []string{"path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_get",
		Description: "Retrieve DEK metadata by path (never exposes key material)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path (e.g., '/', 'tenant-123/', 'tenant-123/prod/')",
				},
			},
			"required": []string{"path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_list",
		Description: "List all DEKs with optional path prefix filtering",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"prefix": map[string]interface{}{
					"type":        "string",
					"description": "Path prefix filter (e.g., 'tenant-123/' lists DEKs for that tenant)",
				},
				"recursive": map[string]interface{}{
					"type":        "boolean",
					"description": "Include nested DEKs in results",
					"default":     false,
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     100,
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_rotate",
		Description: "Rotate a DEK by generating a new version and scheduling re-encryption",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path to rotate",
				},
				"background": map[string]interface{}{
					"type":        "boolean",
					"description": "Run re-encryption in background (default: true)",
					"default":     true,
				},
			},
			"required": []string{"path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_delete",
		Description: "Delete a DEK (destructive operation, requires --force)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path to delete",
				},
				"force": map[string]interface{}{
					"type":        "boolean",
					"description": "Required confirmation for destructive operation",
					"default":     false,
				},
				"migrate_to": map[string]interface{}{
					"type":        "string",
					"description": "Migrate data to another DEK before deletion",
				},
			},
			"required": []string{"path", "force"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_status",
		Description: "Get detailed DEK status including AEAD usage, version, and re-encryption progress",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path",
				},
			},
			"required": []string{"path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_set_default",
		Description: "Set the default DEK for new keys/data at this path level",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path to set as default",
				},
			},
			"required": []string{"path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_set_safety",
		Description: "Set or update AEAD safety configuration for a DEK",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path",
				},
				"max_operations": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum encrypt operations (0 = unlimited)",
				},
				"max_bytes": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum bytes encrypted (0 = unlimited)",
				},
				"warning_percent": map[string]interface{}{
					"type":        "integer",
					"description": "Percent of limit at which to warn",
					"minimum":     1,
					"maximum":     100,
				},
			},
			"required": []string{"path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_clear_safety",
		Description: "Clear per-DEK AEAD safety config, reverting to server defaults",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path",
				},
			},
			"required": []string{"path"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_start_reencryption",
		Description: "Start background re-encryption of data with a new DEK version after rotation",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path (optional, empty for cluster-wide re-encryption)",
				},
				"from_version": map[string]interface{}{
					"type":        "integer",
					"description": "Source DEK version to re-encrypt from",
				},
				"to_version": map[string]interface{}{
					"type":        "integer",
					"description": "Target DEK version to re-encrypt to",
				},
				"batch_size": map[string]interface{}{
					"type":        "integer",
					"description": "Number of entities to process per batch (default: 100)",
					"default":     100,
				},
				"concurrency": map[string]interface{}{
					"type":        "integer",
					"description": "Number of parallel re-encryption workers (default: 4)",
					"default":     4,
				},
				"throttle_rate": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum entities per second, 0 = unlimited (default: 0)",
					"default":     0,
				},
				"force": map[string]interface{}{
					"type":        "boolean",
					"description": "Start even if re-encryption is already in progress",
					"default":     false,
				},
			},
			"required": []string{"from_version", "to_version"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_dek_get_reencryption_progress",
		Description: "Get progress of an ongoing or completed re-encryption operation",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{
					"type":        "string",
					"description": "DEK path (optional)",
				},
				"operation_id": map[string]interface{}{
					"type":        "string",
					"description": "Specific operation ID to check (optional)",
				},
				"verbose": map[string]interface{}{
					"type":        "boolean",
					"description": "Include detailed per-node progress information",
					"default":     false,
				},
			},
		},
	})
}

// =============================================================================
// DEK Service Accessor
// =============================================================================

// getDEKService retrieves the KMSService for DEK re-encryption operations.
func (r *ToolRegistry) getDEKService() (service.KMSService, error) {
	return r.getKMSService()
}

// =============================================================================
// DEK Tool Handlers
// =============================================================================

func (r *ToolRegistry) executeDEKCreate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		CreateDEK(context.Context, *service.CreateDEKRequest) (*service.CreateDEKResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	ints := newIntArgReader(args)
	req := &service.CreateDEKRequest{
		Path:           getString(args, "path"),
		Algorithm:      service.KeyAlgorithm(getString(args, "algorithm")),
		Description:    getString(args, "description"),
		MaxOperations:  ints.Uint64("max_operations"),
		MaxBytes:       ints.Uint64("max_bytes"),
		WarningPercent: ints.Int("warning_percent"),
	}
	if err := ints.Err(); err != nil {
		return nil, err
	}

	resp, err := kmsSvc.CreateDEK(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		GetDEK(context.Context, *service.GetDEKRequest) (*service.GetDEKResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	req := &service.GetDEKRequest{
		Path: getString(args, "path"),
	}

	resp, err := kmsSvc.GetDEK(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

// dekListMaxLimit is the page-size cap of quicbaas_dek_list.
const dekListMaxLimit = 10000

func (r *ToolRegistry) executeDEKList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		ListDEKs(context.Context, *service.ListDEKsRequest) (*service.ListDEKsResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	limit, err := cappedIntArg(args, "limit", dekListMaxLimit)
	if err != nil {
		return nil, err
	}

	req := &service.ListDEKsRequest{
		Prefix:    getString(args, "prefix"),
		Recursive: getBool(args, "recursive"),
		Limit:     limit,
	}

	resp, err := kmsSvc.ListDEKs(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKRotate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		RotateDEK(context.Context, *service.RotateDEKRequest) (*service.RotateDEKResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	req := &service.RotateDEKRequest{
		Path:       getString(args, "path"),
		Background: getBool(args, "background"),
	}

	resp, err := kmsSvc.RotateDEK(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKDelete(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		DeleteDEK(context.Context, *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	req := &service.DeleteDEKRequest{
		Path:      getString(args, "path"),
		Force:     getBool(args, "force"),
		MigrateTo: getString(args, "migrate_to"),
	}

	resp, err := kmsSvc.DeleteDEK(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		GetDEKStatus(context.Context, *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	req := &service.GetDEKStatusRequest{
		Path: getString(args, "path"),
	}

	resp, err := kmsSvc.GetDEKStatus(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKSetDefault(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		SetDefaultDEK(context.Context, *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	req := &service.SetDefaultDEKRequest{
		Path: getString(args, "path"),
	}

	resp, err := kmsSvc.SetDefaultDEK(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKSetSafety(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		SetDEKSafety(context.Context, *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	ints := newIntArgReader(args)
	req := &service.SetDEKSafetyRequest{
		Path:           getString(args, "path"),
		MaxOperations:  ints.Uint64("max_operations"),
		MaxBytes:       ints.Uint64("max_bytes"),
		WarningPercent: ints.Int("warning_percent"),
	}
	if err := ints.Err(); err != nil {
		return nil, err
	}

	resp, err := kmsSvc.SetDEKSafety(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKClearSafety(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	kmsSvc, ok := baseSvc.(interface {
		ClearDEKSafety(context.Context, *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("KMS DEK")
	}

	req := &service.ClearDEKSafetyRequest{
		Path: getString(args, "path"),
	}

	resp, err := kmsSvc.ClearDEKSafety(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKStartReencryption(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	dekSvc, err := r.getDEKService()
	if err != nil {
		return nil, err
	}

	ints := newIntArgReader(args)
	req := &service.StartReencryptionRequest{
		Path:         getString(args, "path"),
		FromVersion:  ints.Uint64("from_version"),
		ToVersion:    ints.Uint64("to_version"),
		BatchSize:    ints.IntOr("batch_size", 100),
		Concurrency:  ints.IntOr("concurrency", 4),
		ThrottleRate: ints.Int("throttle_rate"),
		Force:        getBool(args, "force"),
	}
	if err := ints.Err(); err != nil {
		return nil, err
	}

	resp, err := dekSvc.StartReencryption(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeDEKGetReencryptionProgress(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Get KMS service first, then check if it implements DEK methods
	baseSvc, err := r.getKMSService()
	if err != nil {
		return nil, api.NewServiceUnavailableError("KMS")
	}

	dekSvc, ok := baseSvc.(interface {
		GetReencryptionProgress(context.Context, *service.GetReencryptionProgressRequest) (*service.GetReencryptionProgressResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("DEK")
	}

	req := &service.GetReencryptionProgressRequest{
		Path:        getString(args, "path"),
		OperationID: getString(args, "operation_id"),
		Verbose:     getBool(args, "verbose"),
	}

	resp, err := dekSvc.GetReencryptionProgress(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}
