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

// Package service provides unit tests for Secrets service encryption options.
package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockSecretsManagerWithOptions extends mockSecretsManager to support StoreWithOptions.
type mockSecretsManagerWithOptions struct {
	*mockSecretsManager
	lastStoreOptions *interfaces.StoreOptions
}

// StoreWithOptions implements the new interface method.
func (m *mockSecretsManagerWithOptions) StoreWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *interfaces.StoreOptions) error {
	m.lastStoreOptions = opts
	// Also store the options in metadata for verification
	if opts != nil {
		if metadata == nil {
			metadata = make(map[string]string)
		}
		if opts.TenantID != "" {
			metadata["encryption_tenant_id"] = opts.TenantID
		}
		if opts.KeyIdentifier != "" {
			metadata["encryption_key_id"] = opts.KeyIdentifier
		}
		if opts.Algorithm != "" {
			metadata["encryption_algorithm"] = opts.Algorithm
		}
	}
	return m.Store(ctx, path, data, metadata)
}

// TestStoreSecretWithTenantID verifies secrets can be stored with tenant-specific encryption.
func TestStoreSecretWithTenantID(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	svc := newSecretsService(mockMgr, nil)

	req := &StoreSecretRequest{
		Name:     "test-secret",
		Data:     []byte("secret-data"),
		TenantID: "tenant-123",
	}

	ctx := context.Background()
	resp, err := svc.StoreSecret(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-secret", resp.SecretID)

	// Verify StoreWithOptions was called with correct tenant ID
	require.NotNil(t, mockMgr.lastStoreOptions)
	assert.Equal(t, "tenant-123", mockMgr.lastStoreOptions.TenantID)

	// Verify metadata includes tenant info
	secret, ok := mockMgr.secrets["test-secret"]
	require.True(t, ok)
	assert.Equal(t, "tenant-123", secret.metadata["encryption_tenant_id"])
}

// TestStoreSecretWithKeyID verifies secrets can be stored with a custom key ID.
func TestStoreSecretWithKeyID(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	svc := newSecretsService(mockMgr, nil)

	req := &StoreSecretRequest{
		Name:  "test-secret",
		Data:  []byte("secret-data"),
		KeyID: "custom-key-456",
	}

	ctx := context.Background()
	resp, err := svc.StoreSecret(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify StoreWithOptions was called with correct key ID
	require.NotNil(t, mockMgr.lastStoreOptions)
	assert.Equal(t, "custom-key-456", mockMgr.lastStoreOptions.KeyIdentifier)

	// Verify metadata includes key ID
	secret, ok := mockMgr.secrets["test-secret"]
	require.True(t, ok)
	assert.Equal(t, "custom-key-456", secret.metadata["encryption_key_id"])
}

// TestStoreSecretWithAlgorithm verifies secrets can be stored with a custom encryption algorithm.
func TestStoreSecretWithAlgorithm(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	svc := newSecretsService(mockMgr, nil)

	req := &StoreSecretRequest{
		Name:      "test-secret",
		Data:      []byte("secret-data"),
		Algorithm: "CHACHA20-POLY1305",
	}

	ctx := context.Background()
	resp, err := svc.StoreSecret(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify StoreWithOptions was called with correct algorithm
	require.NotNil(t, mockMgr.lastStoreOptions)
	assert.Equal(t, "CHACHA20-POLY1305", mockMgr.lastStoreOptions.Algorithm)

	// Verify metadata includes algorithm
	secret, ok := mockMgr.secrets["test-secret"]
	require.True(t, ok)
	assert.Equal(t, "CHACHA20-POLY1305", secret.metadata["encryption_algorithm"])
}

// TestStoreSecretWithAllOptions verifies secrets can be stored with all encryption options.
func TestStoreSecretWithAllOptions(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	svc := newSecretsService(mockMgr, nil)

	req := &StoreSecretRequest{
		Name:      "test-secret",
		Data:      []byte("secret-data"),
		TenantID:  "tenant-789",
		KeyID:     "key-abc",
		Algorithm: "AES-256-GCM",
	}

	ctx := context.Background()
	resp, err := svc.StoreSecret(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify all options were set
	require.NotNil(t, mockMgr.lastStoreOptions)
	assert.Equal(t, "tenant-789", mockMgr.lastStoreOptions.TenantID)
	assert.Equal(t, "key-abc", mockMgr.lastStoreOptions.KeyIdentifier)
	assert.Equal(t, "AES-256-GCM", mockMgr.lastStoreOptions.Algorithm)

	// Verify all metadata was stored
	secret, ok := mockMgr.secrets["test-secret"]
	require.True(t, ok)
	assert.Equal(t, "tenant-789", secret.metadata["encryption_tenant_id"])
	assert.Equal(t, "key-abc", secret.metadata["encryption_key_id"])
	assert.Equal(t, "AES-256-GCM", secret.metadata["encryption_algorithm"])
}

// TestStoreSecretWithoutOptions verifies backward compatibility when no options are provided.
func TestStoreSecretWithoutOptions(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	svc := newSecretsService(mockMgr, nil)

	req := &StoreSecretRequest{
		Name: "test-secret",
		Data: []byte("secret-data"),
		// No TenantID, KeyID, or Algorithm
	}

	ctx := context.Background()
	resp, err := svc.StoreSecret(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Verify StoreWithOptions was NOT called (lastStoreOptions should be nil)
	assert.Nil(t, mockMgr.lastStoreOptions)

	// Verify secret was stored via regular Store method
	secret, ok := mockMgr.secrets["test-secret"]
	require.True(t, ok)
	assert.NotNil(t, secret.data)
}

// TestGetSecretReturnsAlgorithm verifies GetSecret returns the encryption algorithm.
func TestGetSecretReturnsAlgorithm(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	// Store a secret with algorithm
	metadata := map[string]string{
		"encryption_algorithm": "AES-256-GCM",
		"encryption_key_id":    "key-123",
	}
	mockMgr.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{
			"data": []byte("secret-data"),
		},
		metadata: metadata,
	}

	svc := newSecretsService(mockMgr, nil)

	req := &GetSecretRequest{
		SecretID: "test-secret",
	}

	ctx := context.Background()
	resp, err := svc.GetSecret(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "secret-data", string(resp.Data))
	assert.Equal(t, "AES-256-GCM", resp.Algorithm)
	assert.Equal(t, "key-123", resp.KeyIdentifier)
}

// TestGetSecretReturnsKeyIdentifierFromTenantID verifies fallback to tenant ID.
func TestGetSecretReturnsKeyIdentifierFromTenantID(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	// Store a secret with tenant ID but no explicit key ID
	metadata := map[string]string{
		"encryption_algorithm": "CHACHA20-POLY1305",
		"encryption_tenant_id": "tenant-456",
	}
	mockMgr.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{
			"data": []byte("secret-data"),
		},
		metadata: metadata,
	}

	svc := newSecretsService(mockMgr, nil)

	req := &GetSecretRequest{
		SecretID: "test-secret",
	}

	ctx := context.Background()
	resp, err := svc.GetSecret(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "CHACHA20-POLY1305", resp.Algorithm)
	// Should fall back to tenant ID when key ID is not set
	assert.Equal(t, "tenant-456", resp.KeyIdentifier)
}

// TestGetSecretWithoutEncryptionInfo verifies backward compatibility.
func TestGetSecretWithoutEncryptionInfo(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	// Store a secret without encryption metadata
	mockMgr.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		data: map[string][]byte{
			"data": []byte("secret-data"),
		},
		metadata: map[string]string{
			"description": "A test secret",
		},
	}

	svc := newSecretsService(mockMgr, nil)

	req := &GetSecretRequest{
		SecretID: "test-secret",
	}

	ctx := context.Background()
	resp, err := svc.GetSecret(ctx, req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "secret-data", string(resp.Data))
	// Algorithm and KeyIdentifier should be empty strings
	assert.Empty(t, resp.Algorithm)
	assert.Empty(t, resp.KeyIdentifier)
}

// TestStoreSecretRequestValidation verifies request validation still works.
func TestStoreSecretRequestValidation(t *testing.T) {
	mockMgr := &mockSecretsManagerWithOptions{
		mockSecretsManager: newMockSecretsManager(),
	}

	svc := newSecretsService(mockMgr, nil)

	tests := []struct {
		name    string
		req     *StoreSecretRequest
		wantErr bool
	}{
		{
			name: "valid request with options",
			req: &StoreSecretRequest{
				Name:     "test",
				Data:     []byte("data"),
				TenantID: "tenant-1",
			},
			wantErr: false,
		},
		{
			name: "missing name and secret_id",
			req: &StoreSecretRequest{
				Data: []byte("data"),
			},
			wantErr: true,
		},
		{
			name: "missing data",
			req: &StoreSecretRequest{
				Name: "test",
				Data: []byte{},
			},
			wantErr: true,
		},
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.StoreSecret(ctx, tt.req)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
