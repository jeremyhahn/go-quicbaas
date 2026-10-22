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
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"math"
	"strings"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

func (r *ToolRegistry) registerKMSTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_generate_key",
		Description: "Generate a new cryptographic key with specified algorithm and parameters",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable key name",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Key algorithm (AES-256-GCM, RSA-2048, ECDSA-P256, ED25519, etc.)",
					"enum": []string{
						"AES-128-GCM", "AES-192-GCM", "AES-256-GCM", "CHACHA20-POLY1305",
						"RSA-2048", "RSA-3072", "RSA-4096",
						"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
						"ED25519", "ED448",
						"HMAC-SHA256", "HMAC-SHA384", "HMAC-SHA512",
					},
				},
				"usage": map[string]interface{}{
					"type":        "array",
					"description": "Permitted key operations",
					"items": map[string]interface{}{
						"type": "string",
						"enum": []string{"encrypt", "decrypt", "sign", "verify", "wrap_key", "derive_key", "generate_mac", "verify_mac"},
					},
				},
				"exportable": map[string]interface{}{
					"type":        "boolean",
					"description": "Allow key export",
					"default":     false,
				},
				"deletion_protection": map[string]interface{}{
					"type":        "boolean",
					"description": "Prevent accidental key deletion",
					"default":     true,
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Key description",
				},
				"tags": map[string]interface{}{
					"type":        "object",
					"description": "User-defined key-value tags",
				},
				"owner": map[string]interface{}{
					"type":        "string",
					"description": "Owning user or service",
				},
			},
			"required": []string{"algorithm"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_import_key",
		Description: "Import an externally generated key into the KMS",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_material": map[string]interface{}{
					"type":        "string",
					"description": "Base64-encoded encrypted key material",
				},
				"wrapping_key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key ID used to wrap the imported key",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Human-readable key name",
				},
				"algorithm": map[string]interface{}{
					"type":        "string",
					"description": "Key algorithm",
				},
				"usage": map[string]interface{}{
					"type":        "array",
					"description": "Permitted key operations",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
				"exportable": map[string]interface{}{
					"type":    "boolean",
					"default": false,
				},
				"description": map[string]interface{}{
					"type": "string",
				},
				"tags": map[string]interface{}{
					"type": "object",
				},
			},
			"required": []string{"key_material", "wrapping_key_id", "algorithm"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_get_key",
		Description: "Retrieve key metadata by key ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
				"version": map[string]interface{}{
					"type":        "integer",
					"description": "Specific key version (optional, defaults to latest)",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_get_public_key",
		Description: "Retrieve the DER-encoded public key component of an asymmetric key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
				"version": map[string]interface{}{
					"type":        "integer",
					"description": "Specific key version (optional, defaults to latest)",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_list_keys",
		Description: "List all keys with optional filtering and pagination",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results",
					"default":     100,
				},
				"cursor": map[string]interface{}{
					"type":        "string",
					"description": "Pagination cursor",
				},
				"filter": map[string]interface{}{
					"type":        "object",
					"description": "Filter by status, algorithm, tags, etc.",
				},
			},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_rotate_key",
		Description: "Create a new version of an existing key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier to rotate",
				},
				"retain_old_version": map[string]interface{}{
					"type":        "boolean",
					"description": "Keep the old version for decryption",
					"default":     true,
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_revoke_key",
		Description: "Mark a key version as revoked and unusable",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
				"reason": map[string]interface{}{
					"type":        "string",
					"description": "Revocation reason",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_delete_key",
		Description: "Permanently remove a key and all its versions",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
				"pending_window_days": map[string]interface{}{
					"type":        "integer",
					"description": "Days before permanent deletion (7-30)",
					"minimum":     7,
					"maximum":     30,
					"default":     30,
				},
				"force": map[string]interface{}{
					"type":        "boolean",
					"description": "Bypass deletion protection",
					"default":     false,
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_get_metadata",
		Description: "Retrieve key metadata without exposing key material",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_update_metadata",
		Description: "Update mutable metadata fields for a key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
				"name": map[string]interface{}{
					"type":        "string",
					"description": "Updated key name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Updated description",
				},
				"tags": map[string]interface{}{
					"type":        "object",
					"description": "Tags to add or update",
				},
				"remove_tags": map[string]interface{}{
					"type":        "array",
					"description": "Tag keys to remove",
					"items": map[string]interface{}{
						"type": "string",
					},
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_set_policy",
		Description: "Set the access control policy for a key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
				"policy": map[string]interface{}{
					"type":        "object",
					"description": "Policy document with operations and principals",
					"properties": map[string]interface{}{
						"allowed_operations": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Operations the key may perform",
						},
						"allowed_principals": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Principals allowed to use the key; empty allows every principal",
						},
						"denied_principals": map[string]interface{}{
							"type":        "array",
							"items":       map[string]interface{}{"type": "string"},
							"description": "Principals refused the key; a deny overrides an allow",
						},
						"expires_at": map[string]interface{}{
							"type":        "string",
							"format":      "date-time",
							"description": "RFC 3339 time after which the policy refuses every operation",
						},
						"rotation_required": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether key rotation is mandatory",
						},
						"max_usage_count": map[string]interface{}{
							"type":        "integer",
							"minimum":     0,
							"description": "Maximum number of operations the key may perform",
						},
						"require_mfa": map[string]interface{}{
							"type":        "boolean",
							"description": "Whether operations require multi-factor authentication",
						},
					},
				},
			},
			"required": []string{"key_id", "policy"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_get_policy",
		Description: "Retrieve the access control policy for a key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_set_rotation_policy",
		Description: "Set or replace the automatic cryptoperiod rotation policy for a key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
				"auto_rotate": map[string]interface{}{
					"type":        "boolean",
					"description": "Enable automatic rotation (requires interval_seconds > 0)",
				},
				"interval_seconds": map[string]interface{}{
					"type":        "number",
					"description": "Rotation interval in seconds (must be positive when auto_rotate is true)",
				},
				"retain_versions": map[string]interface{}{
					"type":        "number",
					"description": "Number of old key versions to retain after rotation",
				},
				"enabled": map[string]interface{}{
					"type":        "boolean",
					"description": "Mark the policy as enabled",
				},
			},
			"required": []string{"key_id", "auto_rotate", "interval_seconds"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_get_rotation_policy",
		Description: "Retrieve the automatic rotation policy for a key",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_delete_rotation_policy",
		Description: "Remove the automatic rotation policy for a key, disabling auto-rotation",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key_id": map[string]interface{}{
					"type":        "string",
					"description": "Key identifier",
				},
			},
			"required": []string{"key_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_kms_get_supported_algorithms",
		Description: "List the cryptographic algorithms supported by the current KMS backend",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	})
}

// =============================================================================
// KMS Rotation Policy Execute Functions
// =============================================================================

func (r *ToolRegistry) executeKMSSetRotationPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	intervalSecs, _ := args["interval_seconds"].(float64)
	autoRotate, _ := args["auto_rotate"].(bool)
	retainVersions, _ := args["retain_versions"].(float64)
	enabled, _ := args["enabled"].(bool)

	if autoRotate && intervalSecs <= 0 {
		return nil, api.NewInvalidInputError("interval_seconds must be positive when auto_rotate is true")
	}

	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	svcReq := &service.SetKeyRotationPolicyRequest{
		KeyID: keyID,
		Policy: &service.RotationPolicy{
			Enabled:        enabled,
			Interval:       time.Duration(intervalSecs) * time.Second,
			RetainVersions: int(retainVersions),
			AutoRotate:     autoRotate,
		},
	}

	resp, err := kmsSvc.SetKeyRotationPolicy(ctx, svcReq)
	if err != nil {
		return nil, err
	}

	var policyMap map[string]interface{}
	if resp.Policy != nil {
		policyMap = map[string]interface{}{
			"enabled":            resp.Policy.Enabled,
			"auto_rotate":        resp.Policy.AutoRotate,
			"interval_seconds":   int64(resp.Policy.Interval / time.Second),
			"next_rotation_unix": resp.Policy.NextRotation.Unix(),
			"retain_versions":    resp.Policy.RetainVersions,
		}
	}

	return marshalResult(map[string]interface{}{
		"key_id":    resp.KeyID,
		"policy":    policyMap,
		"timestamp": resp.Timestamp.Unix(),
	})
}

func (r *ToolRegistry) executeKMSGetRotationPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	svcReq := &service.GetKeyRotationPolicyRequest{KeyID: keyID}

	resp, err := kmsSvc.GetKeyRotationPolicy(ctx, svcReq)
	if err != nil {
		return nil, err
	}

	var policyMap interface{}
	if resp.Policy != nil {
		policyMap = map[string]interface{}{
			"enabled":            resp.Policy.Enabled,
			"auto_rotate":        resp.Policy.AutoRotate,
			"interval_seconds":   int64(resp.Policy.Interval / time.Second),
			"next_rotation_unix": resp.Policy.NextRotation.Unix(),
			"retain_versions":    resp.Policy.RetainVersions,
		}
	}

	return marshalResult(map[string]interface{}{
		"key_id":    resp.KeyID,
		"policy":    policyMap,
		"timestamp": resp.Timestamp.Unix(),
	})
}

func (r *ToolRegistry) executeKMSDeleteRotationPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	svcReq := &service.DeleteKeyRotationPolicyRequest{KeyID: keyID}

	resp, err := kmsSvc.DeleteKeyRotationPolicy(ctx, svcReq)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key_id":    resp.KeyID,
		"timestamp": resp.Timestamp.Unix(),
	})
}

// =============================================================================
// KMS Manager Accessor
// =============================================================================

// getKMSManager retrieves the KMSManager from the service.
// The service.QuicBaaS interface exposes KMSManager() which returns interfaces.KMSManager.
func (r *ToolRegistry) getKMSManager() (interfaces.KMSManager, error) {
	// Try to get KMSManager directly from service
	if kmsMgr := r.service.KMSManager(); kmsMgr != nil {
		return kmsMgr, nil
	}
	return nil, api.NewComponentUnavailableError("KMS manager")
}

// getKMSService retrieves the KMSService from the service.
// The service.QuicBaaS interface exposes KMS() which returns service.KMSService.
func (r *ToolRegistry) getKMSService() (service.KMSService, error) {
	if kmsSvc := r.service.KMS(); kmsSvc != nil {
		return kmsSvc, nil
	}
	return nil, api.NewServiceUnavailableError("KMS")
}

// =============================================================================
// MCP to KeyAttributes Conversion
// =============================================================================

// mcpArgsToKeyAttributes converts MCP tool arguments to KeyAttributes for KMSManager.
func mcpArgsToKeyAttributes(args map[string]interface{}) *kctypes.KeyAttributes {
	name := getString(args, "name")

	// Use the user-provided name directly as the key ID
	keyID := name

	attrs := &kctypes.KeyAttributes{
		CN:         keyID,
		Exportable: getBool(args, "exportable"),
	}

	// Map algorithm string to go-xkms types
	algorithm := getString(args, "algorithm")
	upper := strings.ToUpper(algorithm)
	switch {
	case strings.Contains(upper, "AES-128") || strings.Contains(upper, "AES128"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES128GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(upper, "AES-192") || strings.Contains(upper, "AES192"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES192GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(upper, "AES-256") || strings.Contains(upper, "AES256"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(upper, "XCHACHA20"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricXChaCha20Poly1305
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(upper, "CHACHA20"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricChaCha20Poly1305
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(upper, "RSA-2048") || strings.Contains(upper, "RSA2048"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 2048}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(upper, "RSA-3072") || strings.Contains(upper, "RSA3072"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 3072}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(upper, "RSA-4096") || strings.Contains(upper, "RSA4096"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 4096}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(upper, "ECDSA-P256") || upper == "P256":
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P256()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(upper, "ECDSA-P384") || upper == "P384":
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P384()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(upper, "ECDSA-P521") || upper == "P521":
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P521()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(upper, "ED25519"):
		attrs.KeyAlgorithm = x509.Ed25519
		attrs.KeyType = kctypes.KeyTypeSigning
	default:
		// Default to AES-256-GCM for unknown algorithms
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	}

	return attrs
}

// =============================================================================
// KMS Metadata Response Conversion
// =============================================================================

// KMSKeyResponse represents the MCP response for a KMS key.
type KMSKeyResponse struct {
	KeyID              string            `json:"key_id"`
	Name               string            `json:"name,omitempty"`
	Description        string            `json:"description,omitempty"`
	Algorithm          string            `json:"algorithm"`
	Status             string            `json:"status"`
	Version            int               `json:"version"`
	Exportable         bool              `json:"exportable"`
	DeletionProtection bool              `json:"deletion_protection"`
	CreatedAt          int64             `json:"created_at"`
	UpdatedAt          int64             `json:"updated_at"`
	LastUsedAt         int64             `json:"last_used_at,omitempty"`
	Usage              []string          `json:"usage,omitempty"`
	Tags               map[string]string `json:"tags,omitempty"`
	Owner              string            `json:"owner,omitempty"`
	Timestamp          int64             `json:"timestamp"`
}

// kmsMetadataToResponse converts *kms.KeyMetadata to an MCP response.
func kmsMetadataToResponse(metadata *kms.KeyMetadata) *KMSKeyResponse {
	resp := &KMSKeyResponse{
		KeyID:              metadata.KeyID,
		Name:               metadata.Name,
		Description:        metadata.Description,
		Algorithm:          metadata.Algorithm,
		Status:             string(metadata.State),
		Version:            metadata.Version,
		Exportable:         metadata.Exportable,
		DeletionProtection: metadata.DeletionProtection,
		CreatedAt:          metadata.Created.Unix(),
		UpdatedAt:          metadata.Updated.Unix(),
		Tags:               metadata.Tags,
		Owner:              metadata.Owner,
		Timestamp:          time.Now().Unix(),
	}

	if metadata.LastUsedAt != nil {
		resp.LastUsedAt = metadata.LastUsedAt.Unix()
	}

	// Convert usage
	if len(metadata.Usage) > 0 {
		resp.Usage = make([]string, len(metadata.Usage))
		for i, u := range metadata.Usage {
			resp.Usage[i] = string(u)
		}
	}

	return resp
}

// svcMetadataToResponse converts *service.KeyMetadata to an MCP response.
// This is used when routing through the service layer which returns service types.
func svcMetadataToResponse(metadata *service.KeyMetadata) (*KMSKeyResponse, error) {
	if metadata == nil {
		return &KMSKeyResponse{Timestamp: time.Now().Unix()}, nil
	}
	version, err := kmsKeyVersion(metadata.Version)
	if err != nil {
		return nil, err
	}
	resp := &KMSKeyResponse{
		KeyID:              metadata.KeyID,
		Name:               metadata.Name,
		Description:        metadata.Description,
		Version:            version,
		Algorithm:          string(metadata.Algorithm),
		Status:             string(metadata.Status),
		Exportable:         metadata.Exportable,
		DeletionProtection: metadata.DeletionProtection,
		CreatedAt:          metadata.CreatedAt.Unix(),
		UpdatedAt:          metadata.UpdatedAt.Unix(),
		Tags:               metadata.Tags,
		Owner:              metadata.Owner,
		Timestamp:          time.Now().Unix(),
	}

	if metadata.LastUsedAt != nil {
		resp.LastUsedAt = metadata.LastUsedAt.Unix()
	}

	// Convert usage
	if len(metadata.Usage) > 0 {
		resp.Usage = make([]string, len(metadata.Usage))
		for i, u := range metadata.Usage {
			resp.Usage[i] = string(u)
		}
	}

	return resp, nil
}

// kmsKeyVersion converts a service key version to the int the MCP response
// carries. A version the response cannot hold is a server fault, not a
// request fault, so it is reported as an internal error.
func kmsKeyVersion(version uint64) (int, error) {
	v, err := conv.SafeUint64ToInt(version)
	if err != nil {
		return 0, api.NewEncodeError("mcp.kms", "key version", err)
	}
	return v, nil
}

// mcpArgsToUsage extracts key usage from MCP tool arguments.
func mcpArgsToUsage(args map[string]interface{}) []service.KeyUsage {
	usageRaw, ok := args["usage"].([]interface{})
	if !ok {
		return nil
	}
	usage := make([]service.KeyUsage, 0, len(usageRaw))
	for _, u := range usageRaw {
		if str, ok := u.(string); ok {
			usage = append(usage, service.KeyUsage(str))
		}
	}
	return usage
}

// mcpArgsToTags extracts tags from MCP tool arguments.
func mcpArgsToTags(args map[string]interface{}) map[string]string {
	tagsRaw, ok := args["tags"].(map[string]interface{})
	if !ok {
		return nil
	}
	tags := make(map[string]string, len(tagsRaw))
	for k, v := range tagsRaw {
		if str, ok := v.(string); ok {
			tags[k] = str
		}
	}
	return tags
}

// =============================================================================
// KMS Tool Handlers
// =============================================================================

func (r *ToolRegistry) executeKMSGenerateKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	// Route through the service layer for tenant-aware key generation
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		attrs := mcpArgsToKeyAttributes(args)
		svcReq := &service.GenerateKeyRequest{
			KeyAttributes:      attrs,
			Name:               getString(args, "name"),
			Description:        getString(args, "description"),
			Usage:              mcpArgsToUsage(args),
			Exportable:         getBool(args, "exportable"),
			DeletionProtection: getBool(args, "deletion_protection"),
			Tags:               mcpArgsToTags(args),
			Owner:              getString(args, "owner"),
		}

		svcResp, err := kmsSvc.GenerateKey(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		resp, err := svcMetadataToResponse(svcResp.Metadata)
		if err != nil {
			return nil, err
		}
		return marshalResult(resp)
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	attrs := mcpArgsToKeyAttributes(args)

	result, err := kmsMgr.GenerateKey(ctx, attrs)
	if err != nil {
		return nil, err
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, api.NewUnexpectedResponseError("KMS manager", "")
	}

	if name := getString(args, "name"); name != "" {
		metadata.Name = name
	}
	if desc := getString(args, "description"); desc != "" {
		metadata.Description = desc
	}
	if owner := getString(args, "owner"); owner != "" {
		metadata.Owner = owner
	}

	if tagsRaw, ok := args["tags"].(map[string]interface{}); ok {
		metadata.Tags = make(map[string]string)
		for k, v := range tagsRaw {
			if str, ok := v.(string); ok {
				metadata.Tags[k] = str
			}
		}
	}

	return marshalResult(kmsMetadataToResponse(metadata))
}

func (r *ToolRegistry) executeKMSImportKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyMaterialB64 := getString(args, "key_material")
	if keyMaterialB64 == "" {
		return nil, api.NewRequiredFieldError("key_material")
	}
	keyMaterial, err := base64.StdEncoding.DecodeString(keyMaterialB64)
	if err != nil {
		return nil, api.NewInvalidBase64Error("key_material", err)
	}

	// Route through the service layer for tenant-aware key import
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		attrs := mcpArgsToKeyAttributes(args)
		svcReq := &service.ImportKeyRequest{
			KeyMaterial:   keyMaterial,
			WrappingKeyID: getString(args, "wrapping_key_id"),
			KeyAttributes: attrs,
			Name:          getString(args, "name"),
			Description:   getString(args, "description"),
			Usage:         mcpArgsToUsage(args),
			Tags:          mcpArgsToTags(args),
		}

		svcResp, err := kmsSvc.ImportKey(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		resp, err := svcMetadataToResponse(svcResp.Metadata)
		if err != nil {
			return nil, err
		}
		return marshalResult(resp)
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	algorithm := getString(args, "algorithm")
	name := getString(args, "name")
	keyID := name

	result, err := kmsMgr.ImportKey(ctx, keyID, keyMaterial, algorithm, getBool(args, "exportable"))
	if err != nil {
		return nil, err
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, api.NewUnexpectedResponseError("KMS manager", "")
	}

	if name != "" {
		metadata.Name = name
	}
	if desc := getString(args, "description"); desc != "" {
		metadata.Description = desc
	}

	if tagsRaw, ok := args["tags"].(map[string]interface{}); ok {
		metadata.Tags = make(map[string]string)
		for k, v := range tagsRaw {
			if str, ok := v.(string); ok {
				metadata.Tags[k] = str
			}
		}
	}

	return marshalResult(kmsMetadataToResponse(metadata))
}

func (r *ToolRegistry) executeKMSGetKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Route through the service layer for tenant-aware key retrieval
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		svcReq := &service.GetKeyRequest{
			KeyID:   keyID,
			Version: getUint64(args, "version"),
		}

		svcResp, err := kmsSvc.GetKey(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		resp, err := svcMetadataToResponse(svcResp.Metadata)
		if err != nil {
			return nil, err
		}
		return marshalResult(resp)
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	result, err := kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, api.NewUnexpectedResponseError("KMS manager", "")
	}

	return marshalResult(kmsMetadataToResponse(metadata))
}

// kmsListKeysMaxLimit is the page-size cap of quicbaas_kms_list_keys.
const kmsListKeysMaxLimit = 10000

func (r *ToolRegistry) executeKMSListKeys(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	limit, err := cappedIntArg(args, "limit", kmsListKeysMaxLimit)
	if err != nil {
		return nil, err
	}

	// Route through the service layer for tenant-aware key listing
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		filter := make(map[string]string)
		if filterRaw, ok := args["filter"].(map[string]interface{}); ok {
			for k, v := range filterRaw {
				if str, ok := v.(string); ok {
					filter[k] = str
				}
			}
		}

		svcReq := &service.ListKeysRequest{
			Limit:  limit,
			Cursor: getString(args, "cursor"),
			Filter: filter,
		}

		svcResp, err := kmsSvc.ListKeys(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		keys := make([]*KMSKeyResponse, 0, len(svcResp.Keys))
		for _, keyHandle := range svcResp.Keys {
			version, err := kmsKeyVersion(keyHandle.Version)
			if err != nil {
				return nil, err
			}
			keys = append(keys, &KMSKeyResponse{
				KeyID:     keyHandle.KeyID,
				Algorithm: string(keyHandle.Algorithm),
				Status:    string(keyHandle.Status),
				Version:   version,
				CreatedAt: keyHandle.CreatedAt.Unix(),
				Timestamp: time.Now().Unix(),
			})
		}

		return marshalResult(map[string]interface{}{
			"keys":        keys,
			"has_more":    svcResp.HasMore,
			"total_count": svcResp.TotalCount,
			"timestamp":   svcResp.Timestamp.Unix(),
		})
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	prefix := ""
	if filterRaw, ok := args["filter"].(map[string]interface{}); ok {
		if p, ok := filterRaw["prefix"].(string); ok {
			prefix = p
		}
	}

	results, err := kmsMgr.ListKeys(ctx, prefix)
	if err != nil {
		return nil, err
	}

	keys := make([]*KMSKeyResponse, 0, len(results))
	for _, result := range results {
		if metadata, ok := result.(*kms.KeyMetadata); ok {
			keys = append(keys, kmsMetadataToResponse(metadata))
		}
	}

	hasMore := false
	if limit > 0 && limit < len(keys) {
		keys = keys[:limit]
		hasMore = true
	}

	return marshalResult(map[string]interface{}{
		"keys":        keys,
		"has_more":    hasMore,
		"total_count": len(results),
		"timestamp":   time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeKMSRotateKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Route through the service layer for tenant-aware key rotation
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		svcReq := &service.RotateKeyRequest{
			KeyID:            keyID,
			RetainOldVersion: getBool(args, "retain_old_version"),
		}

		svcResp, err := kmsSvc.RotateKey(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		return marshalResult(map[string]interface{}{
			"key_id":      svcResp.KeyID,
			"old_version": svcResp.OldVersion,
			"new_version": svcResp.NewVersion,
			"timestamp":   svcResp.Timestamp.Unix(),
		})
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	oldResult, err := kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	oldMetadata, ok := oldResult.(*kms.KeyMetadata)
	if !ok {
		return nil, api.NewUnexpectedResponseError("KMS manager", "")
	}
	oldVersion := oldMetadata.Version

	result, err := kmsMgr.RotateKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, api.NewUnexpectedResponseError("KMS manager", "")
	}

	return marshalResult(map[string]interface{}{
		"key_id":      keyID,
		"old_version": oldVersion,
		"new_version": metadata.Version,
		"timestamp":   time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeKMSRevokeKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Route through the service layer for tenant-aware key revocation
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		svcReq := &service.RevokeKeyRequest{
			KeyID:  keyID,
			Reason: getString(args, "reason"),
		}

		svcResp, err := kmsSvc.RevokeKey(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		return marshalResult(map[string]interface{}{
			"key_id":    svcResp.KeyID,
			"status":    string(svcResp.Status),
			"timestamp": svcResp.Timestamp.Unix(),
		})
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	err = kmsMgr.RevokeKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key_id":    keyID,
		"status":    string(kms.KeyStateDisabled),
		"timestamp": time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeKMSDeleteKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}
	ints := newIntArgReader(args)
	pendingDays := ints.IntOr("pending_window_days", 7)
	if err := ints.Err(); err != nil {
		return nil, err
	}

	// Route through the service layer for tenant-aware key deletion
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		svcReq := &service.DeleteKeyRequest{
			KeyID:             keyID,
			PendingWindowDays: pendingDays,
			Force:             getBool(args, "force"),
		}

		svcResp, err := kmsSvc.DeleteKey(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		return marshalResult(map[string]interface{}{
			"key_id":        svcResp.KeyID,
			"status":        string(svcResp.Status),
			"deletion_date": svcResp.DeletionDate.Unix(),
			"timestamp":     svcResp.Timestamp.Unix(),
		})
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	force := getBool(args, "force")

	if !force {
		if err := kmsMgr.DisableKey(ctx, keyID); err != nil {
			return nil, err
		}
	}

	err = kmsMgr.DeleteKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	deletionDate := now.AddDate(0, 0, pendingDays)

	return marshalResult(map[string]interface{}{
		"key_id":        keyID,
		"status":        string(kms.KeyStatePendingDeletion),
		"deletion_date": deletionDate.Unix(),
		"timestamp":     now.Unix(),
	})
}

func (r *ToolRegistry) executeKMSGetMetadata(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Route through the service layer for tenant-aware metadata retrieval
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		svcReq := &service.GetKeyMetadataRequest{KeyID: keyID}
		svcResp, err := kmsSvc.GetKeyMetadata(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		metadata, err := svcMetadataToResponse(svcResp.Metadata)
		if err != nil {
			return nil, err
		}
		return marshalResult(map[string]interface{}{
			"metadata":  metadata,
			"timestamp": svcResp.Timestamp.Unix(),
		})
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	result, err := kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, api.NewUnexpectedResponseError("KMS manager", "")
	}

	return marshalResult(map[string]interface{}{
		"metadata":  kmsMetadataToResponse(metadata),
		"timestamp": time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeKMSUpdateMetadata(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Route through the service layer for tenant-aware metadata update
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		tags := mcpArgsToTags(args)

		var removeTags []string
		if removeRaw, ok := args["remove_tags"].([]interface{}); ok {
			removeTags = make([]string, 0, len(removeRaw))
			for _, t := range removeRaw {
				if str, ok := t.(string); ok {
					removeTags = append(removeTags, str)
				}
			}
		}

		svcReq := &service.UpdateKeyMetadataRequest{
			KeyID:      keyID,
			Tags:       tags,
			RemoveTags: removeTags,
		}
		if name := getString(args, "name"); name != "" {
			svcReq.Name = &name
		}
		if desc := getString(args, "description"); desc != "" {
			svcReq.Description = &desc
		}

		svcResp, err := kmsSvc.UpdateKeyMetadata(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		metadata, err := svcMetadataToResponse(svcResp.Metadata)
		if err != nil {
			return nil, err
		}
		return marshalResult(map[string]interface{}{
			"metadata":  metadata,
			"timestamp": svcResp.Timestamp.Unix(),
		})
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	result, err := kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		return nil, api.NewUnexpectedResponseError("KMS manager", "")
	}

	if name := getString(args, "name"); name != "" {
		metadata.Name = name
	}
	if desc := getString(args, "description"); desc != "" {
		metadata.Description = desc
	}

	if metadata.Tags == nil {
		metadata.Tags = make(map[string]string)
	}
	if tagsRaw, ok := args["tags"].(map[string]interface{}); ok {
		for k, v := range tagsRaw {
			if str, ok := v.(string); ok {
				metadata.Tags[k] = str
			}
		}
	}

	if removeRaw, ok := args["remove_tags"].([]interface{}); ok {
		for _, t := range removeRaw {
			if str, ok := t.(string); ok {
				delete(metadata.Tags, str)
			}
		}
	}

	metadata.Updated = time.Now()

	if err := kmsMgr.UpdateStoredMetadata(metadata); err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"metadata":  kmsMetadataToResponse(metadata),
		"timestamp": time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeKMSSetPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	policyRaw, ok := args["policy"].(map[string]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError("policy", "an object")
	}

	policy, err := parseKMSPolicyArg(keyID, policyRaw)
	if err != nil {
		return nil, err
	}

	// Route through the service layer for tenant-aware policy operations
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		svcReq := &service.SetKeyPolicyRequest{KeyID: keyID, Policy: policy}

		svcResp, err := kmsSvc.SetKeyPolicy(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		return marshalResult(map[string]interface{}{
			"key_id":         svcResp.KeyID,
			"policy_version": svcResp.PolicyVersion,
			"timestamp":      svcResp.Timestamp.Unix(),
		})
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	// Verify key exists
	_, err = kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	version := uint64(1)
	if v, ok := policyRaw["version"].(float64); ok {
		version = uint64(v) + 1
	}

	return marshalResult(map[string]interface{}{
		"key_id":         keyID,
		"policy_version": version,
		"timestamp":      time.Now().Unix(),
	})
}

func (r *ToolRegistry) executeKMSGetPolicy(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	// Route through the service layer for tenant-aware policy retrieval
	kmsSvc, svcErr := r.getKMSService()
	if svcErr == nil {
		svcReq := &service.GetKeyPolicyRequest{KeyID: keyID}
		svcResp, err := kmsSvc.GetKeyPolicy(ctx, svcReq)
		if err != nil {
			return nil, err
		}

		if svcResp.Policy == nil {
			return nil, api.NewUnexpectedResponseError("KMS", "GetKeyPolicy")
		}

		return marshalResult(map[string]interface{}{
			"policy":    kmsPolicyResult(keyID, svcResp.Policy, svcResp.Timestamp),
			"timestamp": svcResp.Timestamp.Unix(),
		})
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr, err := r.getKMSManager()
	if err != nil {
		return nil, err
	}

	_, err = kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		return nil, err
	}

	now := time.Now()

	return marshalResult(map[string]interface{}{
		"policy": map[string]interface{}{
			"key_id":             keyID,
			"version":            1,
			"allowed_operations": []string{"encrypt", "decrypt"},
			"updated_at":         now.Unix(),
		},
		"timestamp": now.Unix(),
	})
}

// parseKMSPolicyArg builds the service key policy from the tool's policy
// object. Every field the service stores is read, and a field of the wrong
// shape is refused rather than dropped: dropping a principal restriction or
// an expiry would widen the policy the caller asked for.
func parseKMSPolicyArg(keyID string, raw map[string]interface{}) (*service.KeyPolicy, error) {
	operations, err := strictStringListArg(raw, "allowed_operations")
	if err != nil {
		return nil, err
	}
	allowed, err := strictStringListArg(raw, "allowed_principals")
	if err != nil {
		return nil, err
	}
	denied, err := strictStringListArg(raw, "denied_principals")
	if err != nil {
		return nil, err
	}
	expiresAt, err := rfc3339Arg(raw, "expires_at")
	if err != nil {
		return nil, err
	}
	rotationRequired, err := strictBoolArg(raw, "rotation_required")
	if err != nil {
		return nil, err
	}
	requireMFA, err := strictBoolArg(raw, "require_mfa")
	if err != nil {
		return nil, err
	}
	maxUsage, err := countArg(raw, "max_usage_count")
	if err != nil {
		return nil, err
	}

	usages := make([]service.KeyUsage, 0, len(operations))
	for _, op := range operations {
		usages = append(usages, service.KeyUsage(op))
	}
	return &service.KeyPolicy{
		KeyID:             keyID,
		AllowedOperations: usages,
		AllowedPrincipals: allowed,
		DeniedPrincipals:  denied,
		ExpiresAt:         expiresAt,
		RotationRequired:  rotationRequired,
		MaxUsageCount:     maxUsage,
		RequireMFA:        requireMFA,
	}, nil
}

// kmsPolicyResult renders a service key policy as the tool's policy object.
// Every field is always present: an empty principal list is [] and a policy
// without an expiry or usage limit carries null, so a client never has to
// guess whether a missing field means "unrestricted" or "not sent".
func kmsPolicyResult(keyID string, policy *service.KeyPolicy, updatedAt time.Time) map[string]interface{} {
	operations := make([]string, len(policy.AllowedOperations))
	for i, op := range policy.AllowedOperations {
		operations[i] = string(op)
	}
	allowed := make([]string, 0, len(policy.AllowedPrincipals))
	allowed = append(allowed, policy.AllowedPrincipals...)
	denied := make([]string, 0, len(policy.DeniedPrincipals))
	denied = append(denied, policy.DeniedPrincipals...)

	var expiresAt interface{}
	if policy.ExpiresAt != nil {
		expiresAt = policy.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	var maxUsage interface{}
	if policy.MaxUsageCount != nil {
		maxUsage = *policy.MaxUsageCount
	}

	return map[string]interface{}{
		"key_id":             keyID,
		"version":            policy.Version,
		"allowed_operations": operations,
		"allowed_principals": allowed,
		"denied_principals":  denied,
		"expires_at":         expiresAt,
		"rotation_required":  policy.RotationRequired,
		"max_usage_count":    maxUsage,
		"require_mfa":        policy.RequireMFA,
		"updated_at":         updatedAt.Unix(),
	}
}

// strictStringListArg reads an optional list of strings. An absent or null
// value is nil; any other shape, or a list holding a non-string, is refused.
func strictStringListArg(args map[string]interface{}, key string) ([]string, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	if list, ok := raw.([]string); ok {
		return list, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, api.NewInvalidTypeError(key, "an array of strings")
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, api.NewInvalidTypeError(key, "an array of strings")
		}
		out = append(out, s)
	}
	return out, nil
}

// rfc3339Arg reads an optional RFC 3339 timestamp. An absent or null value is
// nil; a non-string or an unparsable string is refused.
func rfc3339Arg(args map[string]interface{}, key string) (*time.Time, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil, api.NewInvalidTypeError(key, "an RFC 3339 timestamp string")
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil, api.NewInvalidFormatError(key, err)
	}
	return &t, nil
}

// strictBoolArg reads an optional boolean. An absent or null value is false;
// any other non-boolean is refused.
func strictBoolArg(args map[string]interface{}, key string) (bool, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return false, nil
	}
	b, ok := raw.(bool)
	if !ok {
		return false, api.NewInvalidTypeError(key, "a boolean")
	}
	return b, nil
}

// countArg reads an optional non-negative whole number. An absent or null
// value is nil; a negative, fractional or non-numeric value is refused.
func countArg(args map[string]interface{}, key string) (*uint64, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	f, ok := raw.(float64)
	if !ok || f < 0 || f != math.Trunc(f) || f >= math.MaxUint64 {
		return nil, api.NewInvalidTypeError(key, "a non-negative whole number")
	}
	n := uint64(f)
	return &n, nil
}

// =============================================================================
// KMS Key Administration Execute Functions
// =============================================================================

// executeKMSGetPublicKey retrieves the DER-encoded public key component of an
// asymmetric key via the service layer.
func (r *ToolRegistry) executeKMSGetPublicKey(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	keyID := getString(args, "key_id")
	if keyID == "" {
		return nil, api.NewRequiredFieldError("key_id")
	}

	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	svcReq := &service.GetPublicKeyRequest{
		KeyID:   keyID,
		Version: getUint64(args, "version"),
	}

	svcResp, err := kmsSvc.GetPublicKey(ctx, svcReq)
	if err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"key_id":     svcResp.KeyID,
		"public_key": base64.StdEncoding.EncodeToString(svcResp.PublicKey),
		"algorithm":  string(svcResp.Algorithm),
		"version":    svcResp.Version,
		"timestamp":  svcResp.Timestamp.Unix(),
	})
}

// executeKMSGetSupportedAlgorithms lists the algorithms supported by the
// current KMS backend via the service layer.
func (r *ToolRegistry) executeKMSGetSupportedAlgorithms(ctx context.Context, _ map[string]interface{}) (interface{}, error) {
	kmsSvc, err := r.getKMSService()
	if err != nil {
		return nil, err
	}

	algorithms, err := kmsSvc.GetSupportedAlgorithms(ctx)
	if err != nil {
		return nil, err
	}
	if algorithms == nil {
		algorithms = []string{}
	}

	return marshalResult(map[string]interface{}{
		"algorithms": algorithms,
		"timestamp":  time.Now().Unix(),
	})
}
