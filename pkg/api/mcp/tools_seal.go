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

func (r *ToolRegistry) registerSealTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_seal_status",
		Description: "Get the current seal status of the database including initialization state and threshold information",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_seal_init",
		Description: "Initialize the seal system with Shamir secret sharing parameters for first-time database setup",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Minimum number of shares needed to unseal",
					"minimum":     1,
				},
				"total_shares": map[string]interface{}{
					"type":        "integer",
					"description": "Total number of shares to generate",
					"minimum":     1,
				},
				"recovery_keys_enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable recovery key generation",
				},
				"recovery_threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Minimum recovery keys needed",
					"minimum":     0,
				},
				"recovery_total_keys": map[string]interface{}{
					"type":        "integer",
					"description": "Total recovery keys to generate",
					"minimum":     0,
				},
			},
			"required": []string{"threshold", "total_shares"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_unseal",
		Description: "Provide a key share to unseal the database (must be called threshold times with different shares)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"share": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded Shamir share",
				},
				"node_id": map[string]interface{}{
					"type":        "integer",
					"description": "Node ID providing the share (for auto mode)",
				},
				"signature": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded signature authenticating the share",
				},
			},
			"required": []string{"share"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_seal",
		Description: "Seal the database immediately, clearing the master key from memory and making data inaccessible",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_seal_rekey",
		Description: "Rotate the master key by generating new Shamir shares while maintaining data access",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"new_threshold": map[string]interface{}{
					"type":        "integer",
					"description": "New minimum number of shares needed",
					"minimum":     1,
				},
				"new_total_shares": map[string]interface{}{
					"type":        "integer",
					"description": "New total number of shares to generate",
					"minimum":     1,
				},
			},
			"required": []string{"new_threshold", "new_total_shares"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_seal_generate_recovery_key",
		Description: "Generate recovery keys for emergency access if seal shares are lost",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"recovery_threshold": map[string]interface{}{
					"type":        "integer",
					"description": "Minimum recovery keys needed",
					"minimum":     1,
				},
				"recovery_total_keys": map[string]interface{}{
					"type":        "integer",
					"description": "Total recovery keys to generate",
					"minimum":     1,
				},
			},
			"required": []string{"recovery_threshold", "recovery_total_keys"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_seal_delete_recovery_key",
		Description: "Remove recovery keys from the system",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"confirm": map[string]interface{}{
					"type":        "boolean",
					"description": "Must be true to proceed with deletion",
				},
			},
			"required": []string{"confirm"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_seal_generate_root_token",
		Description: "Generate a new root token for admin access using key shares or recovery keys",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"strategy": map[string]interface{}{
					"type":        "string",
					"description": "Generation strategy: 'recovery_keys' or 'shamir_shares'",
					"enum":        []string{"recovery_keys", "shamir_shares"},
				},
				"shares": map[string]interface{}{
					"type":        "array",
					"description": "Base64-encoded shares to use",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
			},
			"required": []string{"strategy"},
		},
	})
}

func (r *ToolRegistry) executeSealStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sealSvc, ok := r.service.(interface {
		Status(context.Context) (*service.SealStatusResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("seal")
	}

	resp, err := sealSvc.Status(ctx)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSealInit(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sealSvc, ok := r.service.(interface {
		Initialize(context.Context, *service.SealInitRequest) (*service.SealInitResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("seal")
	}

	req := &service.SealInitRequest{
		Threshold:           int(getUint64(args, "threshold")),    //nolint:gosec // G115: Threshold is small value (typically < 10)
		TotalShares:         int(getUint64(args, "total_shares")), //nolint:gosec // G115: small value
		RecoveryKeysEnabled: getBool(args, "recovery_keys_enabled"),
		RecoveryThreshold:   int(getUint64(args, "recovery_threshold")),  //nolint:gosec // G115: small value
		RecoveryTotalKeys:   int(getUint64(args, "recovery_total_keys")), //nolint:gosec // G115: small value
	}

	resp, err := sealSvc.Initialize(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeUnseal(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sealSvc, ok := r.service.(interface {
		Unseal(context.Context, *service.UnsealRequest) (*service.UnsealResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("seal")
	}

	req := &service.UnsealRequest{
		Share:     []byte(getString(args, "share")),
		NodeID:    getUint64(args, "node_id"),
		Signature: []byte(getString(args, "signature")),
	}

	resp, err := sealSvc.Unseal(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSeal(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sealSvc, ok := r.service.(interface {
		Seal(context.Context) error
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("seal")
	}

	err := sealSvc.Seal(ctx)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"sealed": true,
		"status": "Database sealed successfully",
	}, nil
}

func (r *ToolRegistry) executeSealRekey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sealSvc, ok := r.service.(interface {
		Rekey(context.Context, *service.RekeyRequest) (*service.RekeyResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("seal")
	}

	req := &service.RekeyRequest{
		//nolint:gosec // G115: Threshold and shares are small values (typically < 100)
		NewThreshold: int(getUint64(args, "new_threshold")),
		//nolint:gosec // G115: Threshold and shares are small values (typically < 100)
		NewTotalShares: int(getUint64(args, "new_total_shares")),
	}

	resp, err := sealSvc.Rekey(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSealGenerateRecoveryKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sealSvc, ok := r.service.(interface {
		GenerateRecoveryKey(context.Context, *service.GenerateRecoveryKeyRequest) (*service.GenerateRecoveryKeyResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("seal")
	}

	req := &service.GenerateRecoveryKeyRequest{
		//nolint:gosec // G115: Recovery key count is small value (typically < 100)
		RecoveryThreshold: int(getUint64(args, "recovery_threshold")),
		//nolint:gosec // G115: Recovery key count is small value (typically < 100)
		RecoveryTotalKeys: int(getUint64(args, "recovery_total_keys")),
	}

	resp, err := sealSvc.GenerateRecoveryKey(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSealDeleteRecoveryKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sealSvc, ok := r.service.(interface {
		DeleteRecoveryKey(context.Context, *service.DeleteRecoveryKeyRequest) (*service.DeleteRecoveryKeyResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("seal")
	}

	req := &service.DeleteRecoveryKeyRequest{
		Confirm: getBool(args, "confirm"),
	}

	resp, err := sealSvc.DeleteRecoveryKey(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}

func (r *ToolRegistry) executeSealGenerateRootToken(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sealSvc, ok := r.service.(interface {
		GenerateRootToken(context.Context, *service.GenerateRootTokenRequest) (*service.GenerateRootTokenResponse, error)
	})
	if !ok {
		return nil, api.NewServiceUnavailableError("seal")
	}

	var shares [][]byte
	if sharesRaw, ok := args["shares"].([]interface{}); ok {
		shares = make([][]byte, len(sharesRaw))
		for i, s := range sharesRaw {
			if str, ok := s.(string); ok {
				shares[i] = []byte(str)
			}
		}
	}

	req := &service.GenerateRootTokenRequest{
		Strategy: getString(args, "strategy"),
		Shares:   shares,
	}

	resp, err := sealSvc.GenerateRootToken(ctx, req)
	if err != nil {
		return nil, err
	}
	return marshalResult(resp)
}
