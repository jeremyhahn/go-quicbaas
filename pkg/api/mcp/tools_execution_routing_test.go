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
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// Test KMS tool execution error paths (service not available)
func TestKMSToolsServiceNotAvailable(t *testing.T) {
	svc := &mockService{} // Mock doesn't implement KMS interfaces
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "generate key - service not available",
			toolName: "quicbaas_kms_generate_key",
			args: map[string]interface{}{
				"algorithm": "AES-256-GCM",
				"name":      "test-key",
			},
		},
		{
			name:     "import key - service not available",
			toolName: "quicbaas_kms_import_key",
			args: map[string]interface{}{
				"key_material":    "base64data",
				"wrapping_key_id": "key-1",
				"algorithm":       "AES-256-GCM",
			},
		},
		{
			name:     "get key - service not available",
			toolName: "quicbaas_kms_get_key",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "list keys - service not available",
			toolName: "quicbaas_kms_list_keys",
			args:     map[string]interface{}{},
		},
		{
			name:     "rotate key - service not available",
			toolName: "quicbaas_kms_rotate_key",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "revoke key - service not available",
			toolName: "quicbaas_kms_revoke_key",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "delete key - service not available",
			toolName: "quicbaas_kms_delete_key",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "get metadata - service not available",
			toolName: "quicbaas_kms_get_metadata",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
		{
			name:     "update metadata - service not available",
			toolName: "quicbaas_kms_update_metadata",
			args: map[string]interface{}{
				"key_id":      "key-1",
				"description": "updated",
			},
		},
		{
			name:     "set policy - service not available",
			toolName: "quicbaas_kms_set_policy",
			args: map[string]interface{}{
				"key_id": "key-1",
				"policy": map[string]interface{}{
					"version": "1.0",
				},
			},
		},
		{
			name:     "get policy - service not available",
			toolName: "quicbaas_kms_get_policy",
			args: map[string]interface{}{
				"key_id": "key-1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not available")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// Test Secrets tool execution error paths (service not available)
func TestSecretsToolsServiceNotAvailable(t *testing.T) {
	svc := &mockService{} // Mock doesn't implement Secrets interfaces
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "store secret - service not available",
			toolName: "quicbaas_secrets_store",
			args: map[string]interface{}{
				"path": "/secret/test",
				"data": map[string]interface{}{
					"key": "value",
				},
			},
		},
		{
			name:     "get secret - service not available",
			toolName: "quicbaas_secrets_get",
			args: map[string]interface{}{
				"path": "/secret/test",
			},
		},
		{
			name:     "list secrets - service not available",
			toolName: "quicbaas_secrets_list",
			args: map[string]interface{}{
				"path": "/secret",
			},
		},
		{
			name:     "delete secret - service not available",
			toolName: "quicbaas_secrets_delete",
			args: map[string]interface{}{
				"path": "/secret/test",
			},
		},
		{
			name:     "list versions - service not available",
			toolName: "quicbaas_secrets_versions",
			args: map[string]interface{}{
				"path": "/secret/test",
			},
		},
		{
			name:     "rotate secret - service not available",
			toolName: "quicbaas_secrets_rotate",
			args: map[string]interface{}{
				"path": "/secret/test",
			},
		},
		{
			name:     "set rotation policy - service not available",
			toolName: "quicbaas_secrets_set_rotation_policy",
			args: map[string]interface{}{
				"path":            "/secret/test",
				"rotation_period": float64(86400),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not available")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// Test Crypto tool execution error paths (service not available)
func TestCryptoToolsServiceNotAvailable(t *testing.T) {
	svc := &mockService{} // Mock doesn't implement Crypto interfaces
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name     string
		toolName string
		args     map[string]interface{}
	}{
		{
			name:     "encrypt - service not available",
			toolName: "quicbaas_crypto_encrypt",
			args: map[string]interface{}{
				"key_name":  "crypto-key",
				"plaintext": "SGVsbG8gV29ybGQ=",
			},
		},
		{
			name:     "decrypt - service not available",
			toolName: "quicbaas_crypto_decrypt",
			args: map[string]interface{}{
				"key_name":   "crypto-key",
				"ciphertext": "encrypted-data",
			},
		},
		{
			name:     "rewrap - service not available",
			toolName: "quicbaas_crypto_rewrap",
			args: map[string]interface{}{
				"key_name":   "crypto-key",
				"ciphertext": "encrypted-data",
			},
		},
		{
			name:     "sign - service not available",
			toolName: "quicbaas_crypto_sign",
			args: map[string]interface{}{
				"key_name": "crypto-key",
				"input":    "data-to-sign",
			},
		},
		{
			name:     "verify - service not available",
			toolName: "quicbaas_crypto_verify",
			args: map[string]interface{}{
				"key_name":  "crypto-key",
				"input":     "data-to-verify",
				"signature": "signature-value",
			},
		},
		{
			name:     "hmac - service not available",
			toolName: "quicbaas_crypto_hmac",
			args: map[string]interface{}{
				"key_name": "crypto-key",
				"input":    "data-to-hmac",
			},
		},
		{
			name:     "random - service not available",
			toolName: "quicbaas_crypto_random",
			args: map[string]interface{}{
				"bytes": float64(32),
			},
		},
		{
			name:     "hash - service not available",
			toolName: "quicbaas_crypto_hash",
			args: map[string]interface{}{
				"input": "data-to-hash",
			},
		},
		{
			name:     "datakey - service not available",
			toolName: "quicbaas_crypto_datakey",
			args: map[string]interface{}{
				"key_name": "crypto-key",
			},
		},
		{
			name:     "datakey wrapped - service not available",
			toolName: "quicbaas_crypto_datakey_wrapped",
			args: map[string]interface{}{
				"key_name": "crypto-key",
			},
		},
		{
			name:     "encrypt batch - service not available",
			toolName: "quicbaas_crypto_encrypt_batch",
			args: map[string]interface{}{
				"key_name": "crypto-key",
				"items": []interface{}{
					map[string]interface{}{"plaintext": "SGVsbG8="},
				},
			},
		},
		{
			name:     "decrypt batch - service not available",
			toolName: "quicbaas_crypto_decrypt_batch",
			args: map[string]interface{}{
				"key_name": "crypto-key",
				"items": []interface{}{
					map[string]interface{}{"ciphertext": "encrypted"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.toolName, tt.args)
			require.Error(t, err, "should error when service not available")
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "not available")
		})
	}
}

// Test edge cases in parameter extraction
func TestToolParameterExtraction(t *testing.T) {
	t.Run("KMS generate key with all parameters", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"algorithm":           "AES-256-GCM",
			"name":                "test-key",
			"description":         "test description",
			"exportable":          true,
			"deletion_protection": false,
			"usage":               []interface{}{"encrypt", "decrypt"},
			"tags": map[string]interface{}{
				"env":  "test",
				"team": "security",
			},
			"owner": "test-user",
		}

		// This will fail because service doesn't implement the interface,
		// but it will test the parameter extraction code
		_, err := registry.ExecuteTool(ctx, "quicbaas_kms_generate_key", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("KMS import key with all parameters", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_material":    "base64data",
			"wrapping_key_id": "wrapping-key-1",
			"algorithm":       "RSA-2048",
			"name":            "imported-key",
			"description":     "imported from external system",
			"exportable":      false,
			"usage":           []interface{}{"sign", "verify"},
			"tags": map[string]interface{}{
				"source": "external",
			},
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_kms_import_key", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("Secrets store with complex data", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"path": "/secret/app/db",
			"data": map[string]interface{}{
				"username": "admin",
				"password": "secret123",
				"host":     "db.example.com",
				"port":     float64(5432),
			},
			"version": float64(1),
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_secrets_store", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("Crypto encrypt with context", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_name":  "crypto-key",
			"plaintext": "SGVsbG8gV29ybGQ=",
			"context":   "dXNlci1pZDoxMjM=",
			"nonce":     "random-nonce",
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("KMS set policy with complex policy", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_id": "key-1",
			"policy": map[string]interface{}{
				"version": "1.0",
				"statements": []interface{}{
					map[string]interface{}{
						"effect":    "allow",
						"actions":   []interface{}{"encrypt", "decrypt"},
						"resources": []interface{}{"*"},
					},
				},
			},
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_kms_set_policy", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})

	t.Run("Crypto batch operations with multiple items", func(t *testing.T) {
		svc := &mockService{}
		log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
		registry := NewToolRegistry(svc, log)
		ctx := context.Background()

		args := map[string]interface{}{
			"key_name": "crypto-key",
			"items": []interface{}{
				map[string]interface{}{
					"plaintext": "SGVsbG8=",
					"context":   "Y3R4MQ==",
				},
				map[string]interface{}{
					"plaintext": "V29ybGQ=",
					"context":   "Y3R4Mg==",
				},
			},
		}

		_, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", args)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not available")
	})
}

func TestToolRegistryExecuteToolRouting(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	// Test all KMS operation routes
	kmsTools := []string{
		"quicbaas_kms_generate_key",
		"quicbaas_kms_import_key",
		"quicbaas_kms_get_key",
		"quicbaas_kms_list_keys",
		"quicbaas_kms_rotate_key",
		"quicbaas_kms_revoke_key",
		"quicbaas_kms_delete_key",
		"quicbaas_kms_get_metadata",
		"quicbaas_kms_update_metadata",
		"quicbaas_kms_set_policy",
		"quicbaas_kms_get_policy",
		"quicbaas_kms_encrypt",
		"quicbaas_kms_decrypt",
		"quicbaas_kms_backend_test",
		"quicbaas_kms_key_versions",
		"quicbaas_kms_key_status_update",
		"quicbaas_kms_backend_status",
	}

	for _, tool := range kmsTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			// KMS tools require KMS service
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}

	// Test DEK operation routes
	dekTools := []string{
		"quicbaas_dek_create",
		"quicbaas_dek_get",
		"quicbaas_dek_list",
		"quicbaas_dek_rotate",
		"quicbaas_dek_delete",
		"quicbaas_dek_status",
		"quicbaas_dek_set_default",
		"quicbaas_dek_set_safety",
		"quicbaas_dek_clear_safety",
		"quicbaas_dek_start_reencryption",
		"quicbaas_dek_get_reencryption_progress",
	}

	for _, tool := range dekTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}

	// Test secrets operation routes
	secretsTools := []string{
		"quicbaas_secrets_store",
		"quicbaas_secrets_get",
		"quicbaas_secrets_list",
		"quicbaas_secrets_delete",
		"quicbaas_secrets_versions",
		"quicbaas_secrets_rollback",
		"quicbaas_secrets_rotate",
		"quicbaas_secrets_set_rotation_policy",
		"quicbaas_secrets_get_rotation_policy",
		"quicbaas_secrets_get_prefix",
		"quicbaas_secrets_watch",
	}

	for _, tool := range secretsTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}

	// Test crypto operation routes
	cryptoTools := []string{
		"quicbaas_crypto_encrypt",
		"quicbaas_crypto_decrypt",
		"quicbaas_crypto_rewrap",
		"quicbaas_crypto_sign",
		"quicbaas_crypto_verify",
		"quicbaas_crypto_hmac",
		"quicbaas_crypto_verify_hmac",
		"quicbaas_crypto_hash",
		"quicbaas_crypto_random",
		"quicbaas_crypto_datakey",
		"quicbaas_crypto_datakey_wrapped",
		"quicbaas_crypto_encrypt_batch",
		"quicbaas_crypto_decrypt_batch",
	}

	for _, tool := range cryptoTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// TestObjectStorageToolRouting tests object storage tool execution routing.
func TestObjectStorageToolRouting(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	// Return nil for ObjectStorageService to trigger "not available" errors
	mockSvc.On("ObjectStorageService").Return(nil)

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	objectStorageTools := []string{
		"quicbaas_object_put",
		"quicbaas_object_get",
		"quicbaas_object_delete",
		"quicbaas_object_head",
		"quicbaas_object_list",
		"quicbaas_object_list_by_tag",
		"quicbaas_object_list_by_tenant",
		"quicbaas_object_copy",
		"quicbaas_object_replicate",
		"quicbaas_object_migrate",
		"quicbaas_object_archive",
		"quicbaas_object_restore",
		"quicbaas_object_verify",
		"quicbaas_object_set_policy",
		"quicbaas_object_get_policy",
		"quicbaas_object_list_backends",
		"quicbaas_object_backend_health",
		"quicbaas_object_tenant_usage",
		"quicbaas_object_tenant_quotas",
	}

	for _, tool := range objectStorageTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// TestAlgorithmToKeyAttributes tests the algorithmToKeyAttributes function.
func TestAlgorithmToKeyAttributes(t *testing.T) {
	testCases := []struct {
		algorithm             string
		expectedSymmetricAlgo xkmssdk.SymmetricAlgorithm
		description           string
	}{
		{"AES-256-GCM", xkmssdk.SymmetricAES256GCM, "AES-256-GCM format"},
		{"AES256GCM", xkmssdk.SymmetricAES256GCM, "AES256GCM format"},
		{"AES-192-GCM", xkmssdk.SymmetricAES192GCM, "AES-192-GCM format"},
		{"AES192GCM", xkmssdk.SymmetricAES192GCM, "AES192GCM format"},
		{"AES-128-GCM", xkmssdk.SymmetricAES128GCM, "AES-128-GCM format"},
		{"AES128GCM", xkmssdk.SymmetricAES128GCM, "AES128GCM format"},
		{"CHACHA20-POLY1305", xkmssdk.SymmetricChaCha20Poly1305, "CHACHA20-POLY1305 format"},
		{"ChaCha20-Poly1305", xkmssdk.SymmetricChaCha20Poly1305, "ChaCha20-Poly1305 format"},
		{"XCHACHA20-POLY1305", xkmssdk.SymmetricXChaCha20Poly1305, "XCHACHA20-POLY1305 format"},
		{"XChaCha20-Poly1305", xkmssdk.SymmetricXChaCha20Poly1305, "XChaCha20-Poly1305 format"},
		{"unknown", xkmssdk.SymmetricAES256GCM, "unknown algorithm defaults to AES-256-GCM"},
		{"", xkmssdk.SymmetricAES256GCM, "empty algorithm defaults to AES-256-GCM"},
	}

	for _, tc := range testCases {
		t.Run(tc.description, func(t *testing.T) {
			attrs := algorithmToKeyAttributes(tc.algorithm)
			assert.NotNil(t, attrs)
			assert.Equal(t, "data-key", attrs.CN)
			assert.Equal(t, tc.expectedSymmetricAlgo, attrs.SymmetricAlgorithm)
		})
	}
}

// TestVectorToolRouting tests vector tool execution routing.
func TestVectorToolRouting(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	vectorTools := []string{
		"quicbaas_vector_search",
		"qrdb_vector_upsert",
		"qrdb_vector_delete",
		"qrdb_vector_get",
		"quicbaas_vector_search_hybrid",
		"quicbaas_vector_create_index",
		"quicbaas_vector_delete_index",
		"quicbaas_vector_list_indexes",
		"quicbaas_vector_describe_index",
		"qrdb_vector_update_metadata",
	}

	for _, tool := range vectorTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

// TestTimeseriesToolRouting tests timeseries tool execution routing.
func TestTimeseriesToolRouting(t *testing.T) {
	mockSvc := &MockQuicBaaS{}
	log, _ := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test",
	})

	registry := NewToolRegistry(mockSvc, log)
	ctx := context.Background()

	timeseriesTools := []string{
		"quicbaas_timeseries_write",
		"quicbaas_timeseries_query",
		"quicbaas_timeseries_delete",
		"quicbaas_timeseries_aggregate",
		"quicbaas_timeseries_create_retention_policy",
		"quicbaas_timeseries_get_retention_policy",
		"quicbaas_timeseries_delete_retention_policy",
		"quicbaas_timeseries_compact",
		"quicbaas_timeseries_get_stats",
	}

	for _, tool := range timeseriesTools {
		t.Run(tool, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tool, map[string]interface{}{})
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}
