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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// TestKMSToolsNotAvailable tests KMS tools when service doesn't implement them.
func TestKMSToolsNotAvailable(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	svc := &mockService{}
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{"generate key", "quicbaas_kms_generate_key", map[string]interface{}{"algorithm": "AES-256-GCM"}},
		{"import key", "quicbaas_kms_import_key", map[string]interface{}{"key_material": "data", "wrapping_key_id": "w1", "algorithm": "AES-256-GCM"}},
		{"get key", "quicbaas_kms_get_key", map[string]interface{}{"key_id": "k1"}},
		{"list keys", "quicbaas_kms_list_keys", map[string]interface{}{}},
		{"list keys large limit", "quicbaas_kms_list_keys", map[string]interface{}{"limit": float64(20000)}},
		{"rotate key", "quicbaas_kms_rotate_key", map[string]interface{}{"key_id": "k1"}},
		{"revoke key", "quicbaas_kms_revoke_key", map[string]interface{}{"key_id": "k1"}},
		{"delete key", "quicbaas_kms_delete_key", map[string]interface{}{"key_id": "k1"}},
		{"get metadata", "quicbaas_kms_get_metadata", map[string]interface{}{"key_id": "k1"}},
		{"update metadata", "quicbaas_kms_update_metadata", map[string]interface{}{"key_id": "k1"}},
		{"set policy", "quicbaas_kms_set_policy", map[string]interface{}{"key_id": "k1", "policy": map[string]interface{}{}}},
		{"get policy", "quicbaas_kms_get_policy", map[string]interface{}{"key_id": "k1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.tool, tt.args)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "KMS manager not available")
		})
	}
}

// TestSecretsToolsNotAvailable tests Secrets tools when service doesn't implement them.
func TestSecretsToolsNotAvailable(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	svc := &mockService{}
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{"store", "quicbaas_secrets_store", map[string]interface{}{"name": "s1", "data": "d"}},
		{"store with metadata", "quicbaas_secrets_store", map[string]interface{}{"name": "s1", "data": "d", "metadata": map[string]interface{}{"k": "v"}}},
		{"store with tags", "quicbaas_secrets_store", map[string]interface{}{"name": "s1", "data": "d", "tags": map[string]interface{}{"t": "v"}}},
		{"get", "quicbaas_secrets_get", map[string]interface{}{"secret_id": "s1"}},
		{"list", "quicbaas_secrets_list", map[string]interface{}{}},
		{"list with filter", "quicbaas_secrets_list", map[string]interface{}{"filter": map[string]interface{}{"env": "prod"}}},
		{"delete", "quicbaas_secrets_delete", map[string]interface{}{"secret_id": "s1"}},
		{"versions", "quicbaas_secrets_versions", map[string]interface{}{"secret_id": "s1"}},
		{"rollback", "quicbaas_secrets_rollback", map[string]interface{}{"secret_id": "s1", "target_version": float64(1)}},
		{"rotate", "quicbaas_secrets_rotate", map[string]interface{}{"secret_id": "s1", "new_data": "d2"}},
		{"set policy", "quicbaas_secrets_set_rotation_policy", map[string]interface{}{"secret_id": "s1", "policy": map[string]interface{}{}}},
		{"get policy", "quicbaas_secrets_get_rotation_policy", map[string]interface{}{"secret_id": "s1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.tool, tt.args)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "service not available")
		})
	}
}

// TestCryptoToolsNotAvailable tests Crypto tools when service doesn't implement them.
func TestCryptoToolsNotAvailable(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	svc := &mockService{}
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	tests := []struct {
		name string
		tool string
		args map[string]interface{}
	}{
		{"encrypt", "quicbaas_crypto_encrypt", map[string]interface{}{"key_id": "k1", "plaintext": "data"}},
		{"decrypt", "quicbaas_crypto_decrypt", map[string]interface{}{"key_id": "k1", "ciphertext": "enc"}},
		{"rewrap", "quicbaas_crypto_rewrap", map[string]interface{}{"key_id": "k1", "ciphertext": "enc"}},
		{"sign", "quicbaas_crypto_sign", map[string]interface{}{"key_id": "k1", "message": "msg"}},
		{"verify", "quicbaas_crypto_verify", map[string]interface{}{"key_id": "k1", "message": "msg", "signature": "sig"}},
		{"hmac", "quicbaas_crypto_hmac", map[string]interface{}{"key_id": "k1", "message": "msg"}},
		{"verify hmac", "quicbaas_crypto_verify_hmac", map[string]interface{}{"key_id": "k1", "message": "msg", "hmac": "mac"}},
		{"hash", "quicbaas_crypto_hash", map[string]interface{}{"data": "d", "algorithm": "SHA256"}},
		{"random", "quicbaas_crypto_random", map[string]interface{}{"num_bytes": float64(32)}},
		{"datakey", "quicbaas_crypto_datakey", map[string]interface{}{"key_id": "k1"}},
		{"datakey with alg", "quicbaas_crypto_datakey", map[string]interface{}{"key_id": "k1", "algorithm": "AES-128-GCM"}},
		{"wrapped datakey", "quicbaas_crypto_datakey_wrapped", map[string]interface{}{"key_id": "k1"}},
		{"wrapped datakey alg", "quicbaas_crypto_datakey_wrapped", map[string]interface{}{"key_id": "k1", "algorithm": "AES-128-GCM"}},
		{"encrypt batch", "quicbaas_crypto_encrypt_batch", map[string]interface{}{"key_id": "k1", "items": []interface{}{map[string]interface{}{"plaintext": "d"}}}},
		{"decrypt batch", "quicbaas_crypto_decrypt_batch", map[string]interface{}{"key_id": "k1", "items": []interface{}{map[string]interface{}{"ciphertext": "e"}}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := registry.ExecuteTool(ctx, tt.tool, tt.args)
			assert.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), "service not available")
		})
	}
}

// TestCryptoBatchErrors tests batch operation error cases.
func TestCryptoBatchErrors(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	svc := &mockService{}
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	t.Run("encrypt batch invalid items type", func(t *testing.T) {
		result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", map[string]interface{}{
			"key_id": "k1",
			"items":  "not-array",
		})
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("encrypt batch invalid item type", func(t *testing.T) {
		result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_encrypt_batch", map[string]interface{}{
			"key_id": "k1",
			"items":  []interface{}{"not-object"},
		})
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("decrypt batch invalid items type", func(t *testing.T) {
		result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt_batch", map[string]interface{}{
			"key_id": "k1",
			"items":  123,
		})
		assert.Error(t, err)
		assert.Nil(t, result)
	})

	t.Run("decrypt batch invalid item type", func(t *testing.T) {
		result, err := registry.ExecuteTool(ctx, "quicbaas_crypto_decrypt_batch", map[string]interface{}{
			"key_id": "k1",
			"items":  []interface{}{true},
		})
		assert.Error(t, err)
		assert.Nil(t, result)
	})
}

// TestKMSToolsWithVariations tests KMS tools with different argument combinations.
func TestKMSToolsWithVariations(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	require.NoError(t, err)

	svc := &mockService{}
	registry := NewToolRegistry(svc, log)
	ctx := context.Background()

	t.Run("generate key with all options", func(t *testing.T) {
		result, err := registry.ExecuteTool(ctx, "quicbaas_kms_generate_key", map[string]interface{}{
			"algorithm":           "AES-256-GCM",
			"name":                "test",
			"usage":               []interface{}{"encrypt", "decrypt"},
			"tags":                map[string]interface{}{"env": "test"},
			"exportable":          true,
			"deletion_protection": true,
			"description":         "test key",
			"owner":               "user",
		})
		assert.Error(t, err) // Service not available
		assert.Nil(t, result)
	})

	t.Run("import key with all options", func(t *testing.T) {
		result, err := registry.ExecuteTool(ctx, "quicbaas_kms_import_key", map[string]interface{}{
			"key_material":    "data",
			"wrapping_key_id": "wrap",
			"algorithm":       "AES-256-GCM",
			"usage":           []interface{}{"encrypt"},
			"tags":            map[string]interface{}{"type": "imported"},
			"name":            "imported",
			"description":     "desc",
			"exportable":      false,
		})
		assert.Error(t, err) // Service not available
		assert.Nil(t, result)
	})

	t.Run("update metadata with remove tags", func(t *testing.T) {
		result, err := registry.ExecuteTool(ctx, "quicbaas_kms_update_metadata", map[string]interface{}{
			"key_id":      "k1",
			"tags":        map[string]interface{}{"new": "tag"},
			"remove_tags": []interface{}{"old"},
		})
		assert.Error(t, err) // Service not available
		assert.Nil(t, result)
	})
}
