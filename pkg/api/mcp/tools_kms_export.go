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

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerKMSExportTools registers MCP tools for KMS key export and unwrap operations.
func (r *ToolRegistry) registerKMSExportTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_export_key",
		Description: "Export a key wrapped with another key for secure transport to another system",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier to export",
				},
				"version": map[string]interface{}{
					"type":        "integer",
					"description": "Specific key version to export (optional, defaults to latest)",
				},
				"wrapping_key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier used to wrap the exported key material",
				},
				"wrapping_algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Algorithm for wrapping the key (AES-KW, AES-KWP, AES-GCM, RSA-OAEP-SHA256, etc.)",
					"enum": []string{
						"AES-KW", "AES-KWP", "AES-GCM",
						"RSA-OAEP-SHA256", "RSA-OAEP-SHA384", "RSA-OAEP-SHA512",
						"ECDH-ES+A256KW",
					},
					"default": "AES-GCM",
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Export format (raw, jwk, pem)",
					"enum":        []string{"raw", "jwk", "pem"},
					"default":     "raw",
				},
			},
			"required": []string{"key_id", "wrapping_key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_unwrap_key",
		Description: "Unwrap (import) a previously exported key into the KMS",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Identifier for the new key after unwrapping",
				},
				"wrapped_key": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded wrapped key material",
				},
				"wrapping_key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier used to unwrap the key material",
				},
				"wrapping_algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Algorithm used for wrapping (must match export)",
					"enum": []string{
						"AES-KW", "AES-KWP", "AES-GCM",
						"RSA-OAEP-SHA256", "RSA-OAEP-SHA384", "RSA-OAEP-SHA512",
						"ECDH-ES+A256KW",
					},
					"default": "AES-GCM",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Algorithm of the unwrapped key",
					"enum": []string{
						"AES-128-GCM", "AES-192-GCM", "AES-256-GCM",
						"CHACHA20-POLY1305", "XCHACHA20-POLY1305",
						"RSA-2048", "RSA-3072", "RSA-4096",
						"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
						"ED25519", "ED448",
						"HMAC-SHA256", "HMAC-SHA384", "HMAC-SHA512",
					},
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable name for the unwrapped key",
				},
				"source_key_id": map[string]interface{}{
					"type":        "string",
					"description": "Original key ID that was exported (required for AAD verification)",
				},
				"source_version": map[string]interface{}{
					"type":        "integer",
					"description": "Version of the source key that was exported (required for AAD verification)",
				},
				"exportable": map[string]interface{}{
					"type":        "boolean",
					"description": "Whether the unwrapped key can be exported again (default false)",
				},
			},
			"required": []string{"key_id", "wrapped_key", "wrapping_key_id", "algorithm"},
		},
	})
}

// executeKMSExportKey exports a key wrapped with another key.
func (r *ToolRegistry) executeKMSExportKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	wrappingKeyID := getString(args, "wrapping_key_id")
	if wrappingKeyID == "" {
		return nil, api.NewRequiredFieldError("wrapping_key_id")
	}

	wrappingAlgorithm := getString(args, "wrapping_algorithm")
	if wrappingAlgorithm == "" {
		wrappingAlgorithm = "AES-GCM"
	}

	format := getString(args, "format")
	if format == "" {
		format = "raw"
	}

	req := &service.ExportKeyRequest{
		KeyID:             keyID,
		Version:           getUint64(args, "version"),
		WrappingKeyID:     wrappingKeyID,
		WrappingAlgorithm: wrappingAlgorithm,
		Format:            format,
	}

	resp, err := kmsSvc.ExportKey(ctx, req)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key_id":             resp.KeyID,
		"version":            resp.Version,
		"wrapped_key":        base64.StdEncoding.EncodeToString(resp.WrappedKey),
		"wrapping_key_id":    resp.WrappingKeyID,
		"wrapping_algorithm": resp.WrappingAlgorithm,
		"format":             resp.Format,
		"timestamp":          time.Now().Unix(),
	})
}

// executeKMSUnwrapKey unwraps a previously exported key and imports it into the KMS.
func (r *ToolRegistry) executeKMSUnwrapKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	wrappedKeyB64 := getString(args, "wrapped_key")
	if wrappedKeyB64 == "" {
		return nil, api.NewRequiredFieldError("wrapped_key")
	}

	wrappedKey, err := base64.StdEncoding.DecodeString(wrappedKeyB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("wrapped_key", err)
	}

	wrappingKeyID := getString(args, "wrapping_key_id")
	if wrappingKeyID == "" {
		return nil, api.NewRequiredFieldError("wrapping_key_id")
	}

	algorithm := getString(args, "algorithm")
	if algorithm == "" {
		return nil, api.NewRequiredFieldError("algorithm")
	}

	wrappingAlgorithm := getString(args, "wrapping_algorithm")
	if wrappingAlgorithm == "" {
		wrappingAlgorithm = "AES-GCM"
	}

	req := &service.UnwrapKeyRequest{
		KeyID:             keyID,
		WrappedKey:        wrappedKey,
		WrappingKeyID:     wrappingKeyID,
		WrappingAlgorithm: wrappingAlgorithm,
		Algorithm:         algorithm,
		Name:              getString(args, "name"),
		SourceKeyID:       getString(args, "source_key_id"),
		SourceVersion:     getUint64(args, "source_version"),
		KeyAttributes:     &kctypes.KeyAttributes{Exportable: getBool(args, "exportable")},
	}

	resp, err := kmsSvc.UnwrapKey(ctx, req)
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"key_id":    resp.KeyID,
		"version":   resp.Version,
		"algorithm": string(resp.Algorithm),
		"status":    string(resp.Status),
		"timestamp": time.Now().Unix(),
	}

	if resp.Key != nil {
		result["key"] = map[string]interface{}{
			"key_id":     resp.Key.KeyID,
			"version":    resp.Key.Version,
			"algorithm":  string(resp.Key.Algorithm),
			"status":     string(resp.Key.Status),
			"created_at": resp.Key.CreatedAt.Unix(),
		}
	}

	return marshalResult(result)
}
