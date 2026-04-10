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

// Package secrets provides secret storage and retrieval functionality for QuicBaaS.
package secrets

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// TestMockKMSManager_AllMethods exercises all mock methods to ensure coverage.
func TestMockKMSManager_AllMethods(t *testing.T) {
	ctx := context.Background()
	kms := newTestMockKMSManager()

	// Test GenerateKey
	result, err := kms.GenerateKey(ctx, &xkmssdk.KeyAttributes{CN: "test-key"})
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test ImportKey
	result, err = kms.ImportKey(ctx, "test-key", []byte("material"), "AES-256")
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test GetKey
	result, err = kms.GetKey(ctx, "test-key")
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test ListKeys
	keys, err := kms.ListKeys(ctx, "prefix")
	assert.NoError(t, err)
	assert.Nil(t, keys)

	// Test RotateKey
	result, err = kms.RotateKey(ctx, "test-key")
	assert.NoError(t, err)
	assert.Nil(t, result)

	// Test RevokeKey
	err = kms.RevokeKey(ctx, "test-key")
	assert.NoError(t, err)

	// Test DeleteKey
	err = kms.DeleteKey(ctx, "test-key")
	assert.NoError(t, err)

	// Test EnableKey
	err = kms.EnableKey(ctx, "test-key")
	assert.NoError(t, err)

	// Test DisableKey
	err = kms.DisableKey(ctx, "test-key")
	assert.NoError(t, err)

	// Test Sign
	sig, err := kms.Sign(ctx, "test-key", []byte("digest"), "RSA-SHA256")
	assert.NoError(t, err)
	assert.Nil(t, sig)

	// Test Verify
	valid, err := kms.Verify(ctx, "test-key", []byte("digest"), []byte("sig"), "RSA-SHA256")
	assert.NoError(t, err)
	assert.False(t, valid)

	// Test GetBackendType
	backendType := kms.GetBackendType()
	assert.Equal(t, config.BackendTypeSoftware, backendType)

	// Test IsAvailable
	available := kms.IsAvailable(ctx)
	assert.True(t, available)

	// Test Close
	err = kms.Close()
	assert.NoError(t, err)

	// Test Encrypt (default implementation)
	ciphertext, err := kms.Encrypt(ctx, "test-key", []byte("plaintext"), []byte("aad"))
	assert.NoError(t, err)
	assert.NotNil(t, ciphertext)
}

// TestMockKVStore_Scan exercises the Scan method.
func TestMockKVStore_Scan(t *testing.T) {
	ctx := context.Background()
	store := newTestMockKVStore()

	// Add some data
	_ = store.Put(ctx, "prefix/key1", []byte("value1"))
	_ = store.Put(ctx, "prefix/key2", []byte("value2"))

	// Test Scan with default implementation
	err := store.Scan(ctx, "prefix", func(key string, value []byte) error {
		return nil
	})
	assert.NoError(t, err)

	// Test Delete
	err = store.Delete(ctx, "prefix/key1")
	assert.NoError(t, err)
}

// TestMockSecretsEncryptor_ValidateKeyIdentifier exercises the ValidateKeyIdentifier method.
func TestMockSecretsEncryptor_ValidateKeyIdentifier(t *testing.T) {
	ctx := context.Background()
	encryptor := newTestMockSecretsEncryptor()

	// Test with key not found
	err := encryptor.ValidateKeyIdentifier(ctx, "unknown-key")
	assert.Error(t, err)

	// Add a key and test again
	encryptor.AddKey("known-key", []byte("keydata"))
	err = encryptor.ValidateKeyIdentifier(ctx, "known-key")
	assert.NoError(t, err)
}

// TestMockLogger_AllFormats exercises all logger format methods.
func TestMockLogger_AllFormats(t *testing.T) {
	logger := newTestMockLogger()

	// Test all format methods
	logger.Debugf("test %s", "debug")
	logger.Infof("test %s", "info")
	logger.Warnf("test %s", "warn")
	logger.Errorf("test %s", "error")

	// These will panic/exit in real usage, but our mock just logs
	// We can't test Fatal/Fatalf as they call os.Exit
}
