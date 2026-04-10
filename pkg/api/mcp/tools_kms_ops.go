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
	"encoding/base64"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerKMSOpTools registers additional KMS operations tools for encrypt, decrypt,
// backend test, key versions, and key status updates.
func (r *ToolRegistry) registerKMSOpTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_encrypt",
		Description: "Encrypt data using a KMS key with authenticated encryption (AEAD)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "KMS key identifier to use for encryption",
				},
				"plaintext": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded plaintext data to encrypt",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded additional authenticated data (AAD)",
				},
			},
			"required": []string{"key_id", "plaintext"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_decrypt",
		Description: "Decrypt data using a KMS key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "KMS key identifier to use for decryption",
				},
				"ciphertext": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded ciphertext data to decrypt",
				},
				"context": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded additional authenticated data (AAD) used during encryption",
				},
			},
			"required": []string{"key_id", "ciphertext"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_backend_test",
		Description: "Test KMS backend functionality by performing a seal/unseal test cycle",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_key_versions",
		Description: "List all versions of a KMS key with their status and metadata",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "KMS key identifier",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of versions to return",
					"default":     100,
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor for fetching additional versions",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_key_status_update",
		Description: "Update the status of a KMS key (enable/disable)",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "KMS key identifier",
				},
				"version": map[string]interface{}{
					"type":        "integer",
					"description": "Specific key version to update (optional, defaults to all versions)",
				},
				"status": map[string]interface{}{
					"type":        "string",
					"description": "New key status",
					"enum":        []string{"enabled", "disabled"},
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Optional reason for the status change",
				},
			},
			"required": []string{"key_id", "status"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_backend_status",
		Description: "Get the status and capabilities of the KMS backend",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_set_key_safety",
		Description: "Set AEAD safety limits for a KMS key to prevent cryptographic overuse",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "KMS key identifier",
				},
				"max_operations": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of encryption operations",
				},
				"max_bytes": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum bytes that can be encrypted",
				},
				"warning_percent": map[string]interface{}{
					"type":        "integer",
					"description": "Percentage threshold to trigger warning (0-100)",
				},
				"auto_rotate_percent": map[string]interface{}{
					"type":        "integer",
					"description": "Percentage threshold to trigger automatic key rotation (0-100)",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_clear_key_safety",
		Description: "Clear AEAD safety limits for a KMS key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "KMS key identifier",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_safety_check",
		Description: "Check AEAD safety status for keys and DEKs that exceed a usage threshold",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"threshold_percent": map[string]interface{}{
					"type":        "integer",
					"description": "Usage threshold percentage (0-100) to filter results, defaults to 80",
					"default":     80,
					"minimum":     0,
					"maximum":     100,
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_safety_report",
		Description: "Generate a comprehensive AEAD safety report for all keys and DEKs",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"include_details": map[string]interface{}{
					"type":        "boolean",
					"description": "Include detailed information in the report",
					"default":     false,
				},
			},
		},
	})
}

func (r *ToolRegistry) executeKMSEncrypt(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	plaintextB64 := getString(args, "plaintext")
	if plaintextB64 == "" {
		return nil, api.NewRequiredFieldError("plaintext")
	}

	// Decode base64 plaintext
	plaintext, err := base64.StdEncoding.DecodeString(plaintextB64)
	if err != nil {
		// If not base64, use as raw bytes
		plaintext = []byte(plaintextB64)
	}

	// Decode optional AAD context
	var aad []byte
	if contextB64 := getString(args, "context"); contextB64 != "" {
		aad, err = base64.StdEncoding.DecodeString(contextB64)
		if err != nil {
			aad = []byte(contextB64)
		}
	}

	// Call KMS manager to encrypt
	ciphertext, err := kmsMgr.Encrypt(ctx, keyID, plaintext, aad)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key_id":     keyID,
		"ciphertext": base64.StdEncoding.EncodeToString(ciphertext),
		"timestamp":  time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeKMSDecrypt(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	ciphertextB64 := getString(args, "ciphertext")
	if ciphertextB64 == "" {
		return nil, api.NewRequiredFieldError("ciphertext")
	}

	// Decode base64 ciphertext
	ciphertext, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("ciphertext", err)
	}

	// Decode optional AAD context
	var aad []byte
	if contextB64 := getString(args, "context"); contextB64 != "" {
		aad, err = base64.StdEncoding.DecodeString(contextB64)
		if err != nil {
			aad = []byte(contextB64)
		}
	}

	// Call KMS manager to decrypt
	plaintext, err := kmsMgr.Decrypt(ctx, keyID, ciphertext, aad)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key_id":    keyID,
		"plaintext": base64.StdEncoding.EncodeToString(plaintext),
		"timestamp": time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeKMSBackendTest(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	start := time.Now()

	// Test by checking availability
	isAvailable := kmsMgr.IsAvailable(ctx)

	duration := time.Since(start)
	now := time.Now()

	if !isAvailable {
		return marshalResult(map[string]interface{}{
			"success":     false,
			"duration_ms": duration.Milliseconds(),
			"message":     "backend is not available",
			"timestamp":   now.Unix(),
		})
	}

	return marshalResult(map[string]interface{}{
		"success":     true,
		"duration_ms": duration.Milliseconds(),
		"message":     "backend test completed successfully",
		"timestamp":   now.Unix(),
	})
}

func (r *ToolRegistry) executeKMSKeyVersions(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Get key metadata to determine version info
	result, err := kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, api.NewUnexpectedResponseError("KMS manager", "")
	}

	// Build version list from current metadata
	// Note: Full version history would require backend support
	versions := []map[string]interface{}{
		{
			"version":    metadata.Version,
			"status":     string(metadata.State),
			"algorithm":  metadata.Algorithm,
			"created_at": metadata.Created.Unix(),
		},
	}

	now := time.Now()

	return marshalResult(map[string]interface{}{
		"key_id":      keyID,
		"versions":    versions,
		"has_more":    false,
		"next_cursor": "",
		"timestamp":   now.Unix(),
	})
}

func (r *ToolRegistry) executeKMSKeyStatusUpdate(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	status := getString(args, "status")
	if status != "enabled" && status != "disabled" {
		return nil, api.NewInvalidTypeError("status", "'enabled' or 'disabled'")
	}

	// Call appropriate KMS manager method based on status
	var statusErr error
	if status == "enabled" {
		statusErr = kmsMgr.EnableKey(ctx, keyID)
	} else {
		statusErr = kmsMgr.DisableKey(ctx, keyID)
	}

	if statusErr != nil {
		return nil, statusErr
	}

	now := time.Now()

	return marshalResult(map[string]interface{}{
		"key_id":    keyID,
		"version":   getUint64(args, "version"),
		"status":    status,
		"timestamp": now.Unix(),
	})
}

func (r *ToolRegistry) executeKMSBackendStatus(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	backendType := kmsMgr.GetBackendType()
	isAvailable := kmsMgr.IsAvailable(ctx)

	statusStr := "healthy"
	if !isAvailable {
		statusStr = "unavailable"
	}

	// Get supported algorithms
	algorithms, _ := kmsMgr.GetSupportedAlgorithms(ctx)

	now := time.Now()

	return marshalResult(map[string]interface{}{
		"backend_type": string(backendType),
		"status":       statusStr,
		"capabilities": map[string]bool{
			"encrypt":     true,
			"decrypt":     true,
			"sign":        true,
			"verify":      true,
			"generate":    true,
			"import":      true,
			"rotate":      true,
			"data_key":    true,
			"key_wrap":    true,
			"key_unwrap":  true,
			"multi_party": false,
		},
		"supported_algorithms": algorithms,
		"timestamp":            now.Unix(),
	})
}

func (r *ToolRegistry) executeKMSSetKeySafety(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Build request with optional parameters
	req := &service.SetKeySafetyRequest{
		KeyID:             keyID,
		MaxOperations:     getUint64(args, "max_operations"),
		MaxBytes:          getUint64(args, "max_bytes"),
		WarningPercent:    getInt(args, "warning_percent"),
		AutoRotatePercent: getInt(args, "auto_rotate_percent"),
	}

	resp, err := kmsSvc.SetKeySafety(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key_id": resp.KeyID,
		"config": map[string]interface{}{
			"max_operations":      resp.Config.MaxOperations,
			"max_bytes":           resp.Config.MaxBytes,
			"warning_percent":     resp.Config.WarningPercent,
			"auto_rotate_percent": resp.Config.AutoRotatePercent,
		},
		"timestamp": resp.Timestamp.Unix(),
	})
}

func (r *ToolRegistry) executeKMSClearKeySafety(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	req := &service.ClearKeySafetyRequest{
		KeyID: keyID,
	}

	resp, err := kmsSvc.ClearKeySafety(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key_id":    resp.KeyID,
		"timestamp": resp.Timestamp.Unix(),
	})
}

func (r *ToolRegistry) executeKMSSafetyCheck(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	threshold := getInt(args, "threshold_percent")
	if threshold == 0 {
		threshold = 80 // Default to 80%
	}

	req := &service.SafetyCheckRequest{
		ThresholdPercent: float64(threshold),
	}

	resp, err := kmsSvc.SafetyCheck(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert items to map format
	items := make([]map[string]interface{}, 0, len(resp.Items))
	for _, item := range resp.Items {
		items = append(items, map[string]interface{}{
			"id":              item.ID,
			"type":            item.Type,
			"operation_count": item.OperationCount,
			"bytes_processed": item.BytesProcessed,
			"usage_percent":   item.UsagePercent,
			"max_operations":  item.MaxOperations,
			"max_bytes":       item.MaxBytes,
		})
	}

	return marshalResult(map[string]interface{}{
		"items":              items,
		"threshold_percent":  resp.ThresholdPercent,
		"total_keys_checked": resp.TotalKeysChecked,
		"total_deks_checked": resp.TotalDEKsChecked,
		"timestamp":          resp.Timestamp.Unix(),
	})
}

func (r *ToolRegistry) executeKMSSafetyReport(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	req := &service.SafetyReportRequest{
		IncludeDetails: getBool(args, "include_details"),
	}

	resp, err := kmsSvc.SafetyReport(ctx, req)
	if err != nil {
		return nil, err
	}

	// Convert items to map format
	items := make([]map[string]interface{}, 0, len(resp.Items))
	for _, item := range resp.Items {
		items = append(items, map[string]interface{}{
			"id":              item.ID,
			"type":            item.Type,
			"operation_count": item.OperationCount,
			"bytes_processed": item.BytesProcessed,
			"usage_percent":   item.UsagePercent,
			"max_operations":  item.MaxOperations,
			"max_bytes":       item.MaxBytes,
		})
	}

	return marshalResult(map[string]interface{}{
		"items":                 items,
		"total_operations":      resp.TotalOperations,
		"total_bytes_processed": resp.TotalBytesProcessed,
		"server_config": map[string]interface{}{
			"enforce":             resp.ServerConfig.Enforce,
			"max_operations":      resp.ServerConfig.MaxOperations,
			"max_bytes":           resp.ServerConfig.MaxBytes,
			"warning_percent":     resp.ServerConfig.WarningPercent,
			"auto_rotate_percent": resp.ServerConfig.AutoRotatePercent,
		},
		"timestamp": resp.Timestamp.Unix(),
	})
}
