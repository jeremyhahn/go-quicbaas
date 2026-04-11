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

package kms

import (
	"context"
	"crypto/elliptic"
	"crypto/x509"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// =============================================================================
// Test Helpers
// =============================================================================

// mockLogger implements interfaces.Logger for testing.
type mockLogger struct{}

func (m *mockLogger) Debug(msg string, args ...any)                                           {}
func (m *mockLogger) Debugf(format string, args ...interface{})                  {}
func (m *mockLogger) Info(msg string, args ...any)                                            {}
func (m *mockLogger) Infof(format string, args ...interface{})                   {}
func (m *mockLogger) Warn(msg string, args ...any)                                            {}
func (m *mockLogger) Warnf(format string, args ...interface{})                   {}
func (m *mockLogger) Error(msg string, args ...any)                                           {}
func (m *mockLogger) Errorf(format string, args ...interface{})                  {}
func (m *mockLogger) Fatal(msg string)                                           {}
func (m *mockLogger) Fatalf(format string, args ...interface{})                  {}
func (m *mockLogger) WithField(key string, value interface{}) interfaces.Logger  { return m }
func (m *mockLogger) WithFields(fields map[string]interface{}) interfaces.Logger { return m }
func (m *mockLogger) WithError(err error) interfaces.Logger                      { return m }

// setupTestXKMS initializes the xkms for testing.
// Returns a cleanup function that should be deferred.
func setupTestXKMS(t *testing.T) func() {
	t.Helper()
	xkms.Reset()
	return func() {
		xkms.Reset()
	}
}

// =============================================================================
// NewXKMSBackend Tests
// =============================================================================

func TestNewXKMSBackend_NilConfig(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend, err := NewXKMSBackend(nil)
	assert.Error(t, err)
	assert.Nil(t, backend)
	assert.Contains(t, err.Error(), "config cannot be nil")
}

func TestNewXKMSBackend_XKMSNotInitialized(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	cfg := &XKMSBackendConfig{
		BackendType: config.BackendTypeSoftware,
		Logger:      &mockLogger{},
	}

	backend, err := NewXKMSBackend(cfg)
	assert.Error(t, err)
	assert.Nil(t, backend)
	assert.ErrorIs(t, err, xkms.ErrNotInitialized)
}

func TestNewXKMSBackend_DefaultBackendType(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	// For this test we need xkms initialized, but we cannot easily
	// do that without a real backend. Instead, we test that the code path
	// correctly sets default backend type by checking the config handling.

	// Since xkms is not initialized, this will fail at the IsInitialized check
	cfg := &XKMSBackendConfig{
		BackendType: "", // Empty - should default to software
		Logger:      &mockLogger{},
	}

	backend, err := NewXKMSBackend(cfg)
	assert.Error(t, err) // Fails because xkms not initialized
	assert.Nil(t, backend)
	assert.ErrorIs(t, err, xkms.ErrNotInitialized)
}

// =============================================================================
// Type Tests
// =============================================================================

func TestXKMSBackend_Type(t *testing.T) {
	// Create a backend directly for type testing (bypass initialization)
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	assert.Equal(t, config.BackendTypeSoftware, backend.Type())
}

func TestXKMSBackend_Type_TPM2(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeTPM2,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	assert.Equal(t, config.BackendTypeTPM2, backend.Type())
}

func TestXKMSBackend_Type_PKCS11(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypePKCS11,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	assert.Equal(t, config.BackendTypePKCS11, backend.Type())
}

// =============================================================================
// IsAvailable Tests
// =============================================================================

func TestXKMSBackend_IsAvailable_NotInitialized(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	assert.False(t, backend.IsAvailable(ctx))
}

// =============================================================================
// SupportedAlgorithms Tests
// =============================================================================

func TestXKMSBackend_SupportedAlgorithms_NotInitialized(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	algs := backend.SupportedAlgorithms(ctx)
	// When xkms is not initialized or capabilities cannot be queried,
	// the backend should return default algorithms based on backend type.
	// This ensures the service can always report supported algorithms.
	assert.NotEmpty(t, algs, "Should return default algorithms when capabilities unavailable")
	// Verify ChaCha20 algorithms are included for software backend
	assert.Contains(t, algs, AlgChaCha20Poly1305)
	assert.Contains(t, algs, AlgXChaCha20Poly1305)
}

// =============================================================================
// CreateKey Tests
// =============================================================================

func TestXKMSBackend_CreateKey_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "test-key",
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}

	meta, err := backend.CreateKey(ctx, attrs)
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

func TestXKMSBackend_CreateKey_NilAttrs(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	// Create a backend that appears available by mocking IsAvailable behavior
	// We'll use a direct test on the validation logic
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	// Since IsAvailable returns false, we get ErrBackendNotReady
	ctx := context.Background()
	meta, err := backend.CreateKey(ctx, nil)
	assert.Error(t, err)
	assert.Nil(t, meta)
	// Backend not ready error comes first
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

func TestXKMSBackend_CreateKey_EmptyCN(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	attrs := &xkmssdk.KeyAttributes{
		CN:                 "", // Empty
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}

	meta, err := backend.CreateKey(ctx, attrs)
	assert.Error(t, err)
	assert.Nil(t, meta)
	// Backend not ready error comes first
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// ImportKey Tests
// =============================================================================

func TestXKMSBackend_ImportKey_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	material := []byte("test-key-material-32-bytes-long!")

	meta, err := backend.ImportKey(ctx, "test-key", material, AlgAES256GCM, nil)
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

func TestXKMSBackend_ImportKey_EmptyKeyID(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	material := []byte("test-key-material-32-bytes-long!")

	meta, err := backend.ImportKey(ctx, "", material, AlgAES256GCM, nil)
	assert.Error(t, err)
	assert.Nil(t, meta)
	// Backend not ready error comes first
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

func TestXKMSBackend_ImportKey_EmptyMaterial(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()

	meta, err := backend.ImportKey(ctx, "test-key", nil, AlgAES256GCM, nil)
	assert.Error(t, err)
	assert.Nil(t, meta)
	// Backend not ready error comes first
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// GetKeyMetadata Tests
// =============================================================================

func TestXKMSBackend_GetKeyMetadata_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	meta, err := backend.GetKeyMetadata(ctx, "test-key")
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

func TestXKMSBackend_GetKeyMetadata_FromCache(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	// Pre-populate the cache
	now := time.Now()
	cachedMeta := &KeyMetadata{
		KeyID:     "test-key",
		Name:      "test-key",
		Algorithm: AlgAES256GCM,
		State:     KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
	}

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    map[string]*KeyMetadata{"test-key": cachedMeta},
	}

	ctx := context.Background()
	// This will fail because xkms is not initialized
	meta, err := backend.GetKeyMetadata(ctx, "test-key")
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// ListKeys Tests
// =============================================================================

func TestXKMSBackend_ListKeys_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	keys, cursor, err := backend.ListKeys(ctx, "", 0)
	assert.Error(t, err)
	assert.Nil(t, keys)
	assert.Empty(t, cursor)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// RotateKey Tests
// =============================================================================

func TestXKMSBackend_RotateKey_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	meta, err := backend.RotateKey(ctx, "test-key")
	assert.Error(t, err)
	assert.Nil(t, meta)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// DisableKey Tests
// =============================================================================

func TestXKMSBackend_DisableKey_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	err := backend.DisableKey(ctx, "test-key")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// EnableKey Tests
// =============================================================================

func TestXKMSBackend_EnableKey_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	err := backend.EnableKey(ctx, "test-key")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// DeleteKey Tests
// =============================================================================

func TestXKMSBackend_DeleteKey_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	err := backend.DeleteKey(ctx, "test-key")
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// Encrypt Tests
// =============================================================================

func TestXKMSBackend_Encrypt_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	ciphertext, err := backend.Encrypt(ctx, "test-key", []byte("plaintext"), nil)
	assert.Error(t, err)
	assert.Nil(t, ciphertext)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// Decrypt Tests
// =============================================================================

func TestXKMSBackend_Decrypt_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	plaintext, err := backend.Decrypt(ctx, "test-key", []byte("ciphertext"), nil)
	assert.Error(t, err)
	assert.Nil(t, plaintext)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// Sign Tests
// =============================================================================

func TestXKMSBackend_Sign_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	digest := []byte("test-digest-data")
	sig, err := backend.Sign(ctx, "test-key", digest, "SHA256")
	assert.Error(t, err)
	assert.Nil(t, sig)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// Verify Tests
// =============================================================================

func TestXKMSBackend_Verify_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	digest := []byte("test-digest-data")
	signature := []byte("test-signature")
	valid, err := backend.Verify(ctx, "test-key", digest, signature, "SHA256")
	assert.Error(t, err)
	assert.False(t, valid)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// GenerateDataKey Tests
// =============================================================================

func TestXKMSBackend_GenerateDataKey_NotAvailable(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	ctx := context.Background()
	plaintext, wrapped, err := backend.GenerateDataKey(ctx, "test-kek", 32)
	assert.Error(t, err)
	assert.Nil(t, plaintext)
	assert.Nil(t, wrapped)
	assert.ErrorIs(t, err, ErrBackendNotReady)
}

// =============================================================================
// Close Tests
// =============================================================================

func TestXKMSBackend_Close(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	err := backend.Close()
	assert.NoError(t, err)
}

func TestXKMSBackend_Close_MultipleCallsIdempotent(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	// Multiple closes should be safe
	err := backend.Close()
	assert.NoError(t, err)

	err = backend.Close()
	assert.NoError(t, err)
}

// =============================================================================
// Helper Function Tests: formatKeyID
// =============================================================================

func TestXKMSBackend_formatKeyID_WithPrefix(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	// formatKeyID now returns key ID as-is (no transformation)
	result := backend.formatKeyID("software:encryption:aes256-gcm:my-key")
	assert.Equal(t, "software:encryption:aes256-gcm:my-key", result)
}

func TestXKMSBackend_formatKeyID_WithoutPrefix(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	// formatKeyID now returns key ID as-is (matching how keys are stored with attrs.CN)
	result := backend.formatKeyID("my-key")
	assert.Equal(t, "my-key", result)
}

func TestXKMSBackend_formatKeyID_TPM2Backend(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeTPM2,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	// formatKeyID returns key ID as-is regardless of backend type
	result := backend.formatKeyID("my-key")
	assert.Equal(t, "my-key", result)
}

func TestXKMSBackend_formatKeyID_PKCS11Backend(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypePKCS11,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	// formatKeyID returns key ID as-is regardless of backend type
	result := backend.formatKeyID("my-key")
	assert.Equal(t, "my-key", result)
}

// Helper Function Tests: buildFullKeyID
// =============================================================================

func TestXKMSBackend_buildFullKeyID_AES256GCM(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildFullKeyID("my-key", "AES-256-GCM")
	assert.Equal(t, "software:encryption:aes256-gcm:my-key", result)
}

func TestXKMSBackend_buildFullKeyID_AES128GCM(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildFullKeyID("my-key", "AES-128-GCM")
	assert.Equal(t, "software:encryption:aes128-gcm:my-key", result)
}

func TestXKMSBackend_buildFullKeyID_ChaCha20(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildFullKeyID("my-key", "CHACHA20-POLY1305")
	assert.Equal(t, "software:encryption:chacha20-poly1305:my-key", result)
}

func TestXKMSBackend_buildFullKeyID_TPM2Backend(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeTPM2,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildFullKeyID("my-key", "AES-256-GCM")
	assert.Equal(t, "tpm2:encryption:aes256-gcm:my-key", result)
}

// =============================================================================
// Helper Function Tests: buildSigningKeyID
// =============================================================================

func TestXKMSBackend_buildSigningKeyID_RSA(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildSigningKeyID("my-key", "RSA-2048")
	assert.Equal(t, "software:signing:rsa:my-key", result)
}

func TestXKMSBackend_buildSigningKeyID_RSA4096(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildSigningKeyID("my-key", "RSA-4096")
	assert.Equal(t, "software:signing:rsa:my-key", result)
}

func TestXKMSBackend_buildSigningKeyID_ECDSA(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildSigningKeyID("my-key", "ECDSA-P256")
	assert.Equal(t, "software:signing:ecdsa-p256:my-key", result)
}

func TestXKMSBackend_buildSigningKeyID_ECDSA_P384(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildSigningKeyID("my-key", "ECDSA-P384")
	assert.Equal(t, "software:signing:ecdsa-p384:my-key", result)
}

func TestXKMSBackend_buildSigningKeyID_Ed25519(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildSigningKeyID("my-key", "ED25519")
	assert.Equal(t, "software:signing:ed25519:my-key", result)
}

func TestXKMSBackend_buildSigningKeyID_TPM2Backend(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeTPM2,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildSigningKeyID("my-key", "RSA-2048")
	assert.Equal(t, "tpm2:signing:rsa:my-key", result)
}

func TestXKMSBackend_buildSigningKeyID_PKCS11Backend(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypePKCS11,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	result := backend.buildSigningKeyID("my-key", "ECDSA-P256")
	assert.Equal(t, "pkcs11:signing:ecdsa-p256:my-key", result)
}

// =============================================================================
// Helper Function Tests: signingAlgorithmToKeyIDFormat
// =============================================================================

func TestXKMSBackend_signingAlgorithmToKeyIDFormat_RSA(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []string{
		"RSA",
		"RSA-2048",
		"RSA-4096",
		"rsa-3072",
		"RSA2048",
	}

	for _, tc := range testCases {
		t.Run(tc, func(t *testing.T) {
			result := backend.signingAlgorithmToKeyIDFormat(tc)
			assert.Equal(t, "rsa", result)
		})
	}
}

func TestXKMSBackend_signingAlgorithmToKeyIDFormat_ECDSA(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		input    string
		expected string
	}{
		// P-256 variants
		{"ECDSA-P256", "ecdsa-p256"},
		{"ecdsa-p256", "ecdsa-p256"},
		{"P-256", "ecdsa-p256"},
		{"P256", "ecdsa-p256"},
		// P-384 variants
		{"ECDSA-P384", "ecdsa-p384"},
		{"P-384", "ecdsa-p384"},
		{"P384", "ecdsa-p384"},
		// P-521 variants
		{"ECDSA-P521", "ecdsa-p521"},
		{"P-521", "ecdsa-p521"},
		{"P521", "ecdsa-p521"},
		// Generic ECDSA (no curve) defaults to P-256
		{"ECDSA", "ecdsa-p256"},
		{"EC", "ecdsa-p256"},
		{"EC-P256", "ecdsa-p256"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := backend.signingAlgorithmToKeyIDFormat(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestXKMSBackend_signingAlgorithmToKeyIDFormat_Ed25519(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []string{
		"ED25519",
		"ed25519",
		"Ed25519",
	}

	for _, tc := range testCases {
		t.Run(tc, func(t *testing.T) {
			result := backend.signingAlgorithmToKeyIDFormat(tc)
			assert.Equal(t, "ed25519", result)
		})
	}
}

func TestXKMSBackend_signingAlgorithmToKeyIDFormat_Unknown(t *testing.T) {
	backend := &XKMSBackend{}

	// Unknown algorithms default to RSA
	result := backend.signingAlgorithmToKeyIDFormat("unknown-algorithm")
	assert.Equal(t, "rsa", result)
}

// =============================================================================
// Helper Function Tests: algorithmToKeyAttributes
// =============================================================================

func TestXKMSBackend_algorithmToKeyAttributes_AES256GCM(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", AlgAES256GCM)
	require.NotNil(t, attrs)
	assert.Equal(t, "test-key", attrs.CN)
	assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
}

func TestXKMSBackend_algorithmToKeyAttributes_AES192GCM(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "AES-192-GCM")
	require.NotNil(t, attrs)
	assert.Equal(t, xkmssdk.SymmetricAES192GCM, attrs.SymmetricAlgorithm)
}

func TestXKMSBackend_algorithmToKeyAttributes_AES128GCM(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "AES-128-GCM")
	require.NotNil(t, attrs)
	assert.Equal(t, xkmssdk.SymmetricAES128GCM, attrs.SymmetricAlgorithm)
}

func TestXKMSBackend_algorithmToKeyAttributes_ChaCha20(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "ChaCha20-Poly1305")
	require.NotNil(t, attrs)
	assert.Equal(t, xkmssdk.SymmetricChaCha20Poly1305, attrs.SymmetricAlgorithm)
}

func TestXKMSBackend_algorithmToKeyAttributes_XChaCha20(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "XChaCha20-Poly1305")
	require.NotNil(t, attrs)
	assert.Equal(t, xkmssdk.SymmetricXChaCha20Poly1305, attrs.SymmetricAlgorithm)
}

func TestXKMSBackend_algorithmToKeyAttributes_RSA4096(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "RSA-4096")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
	assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
	require.NotNil(t, attrs.RSAAttributes)
	assert.Equal(t, 4096, attrs.RSAAttributes.KeySize)
}

func TestXKMSBackend_algorithmToKeyAttributes_RSA3072(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "RSA-3072")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
	require.NotNil(t, attrs.RSAAttributes)
	assert.Equal(t, 3072, attrs.RSAAttributes.KeySize)
}

func TestXKMSBackend_algorithmToKeyAttributes_RSA2048(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "RSA-2048")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
	require.NotNil(t, attrs.RSAAttributes)
	assert.Equal(t, 2048, attrs.RSAAttributes.KeySize)
}

func TestXKMSBackend_algorithmToKeyAttributes_RSADefault(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "RSA")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
	require.NotNil(t, attrs.RSAAttributes)
	assert.Equal(t, 4096, attrs.RSAAttributes.KeySize) // Default to 4096
}

func TestXKMSBackend_algorithmToKeyAttributes_ECDSAP256(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "ECDSA-P256")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
	assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
	require.NotNil(t, attrs.ECCAttributes)
	assert.Equal(t, elliptic.P256(), attrs.ECCAttributes.Curve)
}

func TestXKMSBackend_algorithmToKeyAttributes_ECDSAP384(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "ECDSA-P384")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
	require.NotNil(t, attrs.ECCAttributes)
	assert.Equal(t, elliptic.P384(), attrs.ECCAttributes.Curve)
}

func TestXKMSBackend_algorithmToKeyAttributes_ECDSAP521(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "ECDSA-P521")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
	require.NotNil(t, attrs.ECCAttributes)
	assert.Equal(t, elliptic.P521(), attrs.ECCAttributes.Curve)
}

func TestXKMSBackend_algorithmToKeyAttributes_ECDSADefault(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "ECDSA")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
	require.NotNil(t, attrs.ECCAttributes)
	assert.Equal(t, elliptic.P256(), attrs.ECCAttributes.Curve) // Default to P-256
}

func TestXKMSBackend_algorithmToKeyAttributes_Ed25519(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "ED25519")
	require.NotNil(t, attrs)
	assert.Equal(t, x509.Ed25519, attrs.KeyAlgorithm)
	assert.Equal(t, xkmssdk.KeyTypeSigning, attrs.KeyType)
}

func TestXKMSBackend_algorithmToKeyAttributes_Default(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", "unknown-algorithm")
	require.NotNil(t, attrs)
	// Default to AES-256-GCM
	assert.Equal(t, xkmssdk.SymmetricAES256GCM, attrs.SymmetricAlgorithm)
}

// =============================================================================
// Helper Function Tests: isSymmetricAlgorithm
// =============================================================================

func TestXKMSBackend_isSymmetricAlgorithm_AES(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		algorithm string
		expected  bool
	}{
		{"AES-256-GCM", true},
		{"AES-128-GCM", true},
		{"AES-192-GCM", true},
		{"aes-256-gcm", true},
		{"AES256-GCM", true},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			result := backend.isSymmetricAlgorithm(tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestXKMSBackend_isSymmetricAlgorithm_ChaCha(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		algorithm string
		expected  bool
	}{
		{"CHACHA20-POLY1305", true},
		{"ChaCha20-Poly1305", true},
		{"chacha20-poly1305", true},
		{"XCHACHA20-POLY1305", true},
		{"XChaCha20-Poly1305", true},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			result := backend.isSymmetricAlgorithm(tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestXKMSBackend_isSymmetricAlgorithm_Asymmetric(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		algorithm string
		expected  bool
	}{
		{"RSA-4096", false},
		{"RSA-2048", false},
		{"ECDSA-P256", false},
		{"ECDSA-P384", false},
		{"ED25519", false},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			result := backend.isSymmetricAlgorithm(tc.algorithm)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Helper Function Tests: attrsToAlgorithmString
// =============================================================================

func TestXKMSBackend_attrsToAlgorithmString_SymmetricAlgorithms(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		name     string
		attrs    *xkmssdk.KeyAttributes
		expected string
	}{
		{
			name:     "AES-128-GCM",
			attrs:    &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES128GCM},
			expected: AlgAES128GCM,
		},
		{
			name:     "AES-192-GCM",
			attrs:    &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES192GCM},
			expected: AlgAES192GCM,
		},
		{
			name:     "AES-256-GCM",
			attrs:    &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
			expected: AlgAES256GCM,
		},
		{
			name:     "ChaCha20-Poly1305",
			attrs:    &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricChaCha20Poly1305},
			expected: AlgChaCha20Poly1305,
		},
		{
			name:     "XChaCha20-Poly1305",
			attrs:    &xkmssdk.KeyAttributes{SymmetricAlgorithm: xkmssdk.SymmetricXChaCha20Poly1305},
			expected: AlgXChaCha20Poly1305,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := backend.attrsToAlgorithmString(tc.attrs)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestXKMSBackend_attrsToAlgorithmString_RSA(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		name     string
		attrs    *xkmssdk.KeyAttributes
		expected string
	}{
		{
			name: "RSA-2048",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  x509.RSA,
				RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 2048},
			},
			expected: AlgRSA2048,
		},
		{
			name: "RSA-3072",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  x509.RSA,
				RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 3072},
			},
			expected: AlgRSA3072,
		},
		{
			name: "RSA-4096",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  x509.RSA,
				RSAAttributes: &xkmssdk.RSAAttributes{KeySize: 4096},
			},
			expected: AlgRSA4096,
		},
		{
			name: "RSA-default",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.RSA,
				// No RSAAttributes - should default to 4096
			},
			expected: AlgRSA4096,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := backend.attrsToAlgorithmString(tc.attrs)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestXKMSBackend_attrsToAlgorithmString_ECDSA(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		name     string
		attrs    *xkmssdk.KeyAttributes
		expected string
	}{
		{
			name: "ECDSA-P256",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  x509.ECDSA,
				ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
			},
			expected: AlgECDSAP256,
		},
		{
			name: "ECDSA-P384",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  x509.ECDSA,
				ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P384()},
			},
			expected: AlgECDSAP384,
		},
		{
			name: "ECDSA-P521",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm:  x509.ECDSA,
				ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P521()},
			},
			expected: AlgECDSAP521,
		},
		{
			name: "ECDSA-default",
			attrs: &xkmssdk.KeyAttributes{
				KeyAlgorithm: x509.ECDSA,
				// No ECCAttributes - should default to P256
			},
			expected: AlgECDSAP256,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := backend.attrsToAlgorithmString(tc.attrs)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestXKMSBackend_attrsToAlgorithmString_Ed25519(t *testing.T) {
	backend := &XKMSBackend{}

	attrs := &xkmssdk.KeyAttributes{
		KeyAlgorithm: x509.Ed25519,
	}

	result := backend.attrsToAlgorithmString(attrs)
	assert.Equal(t, AlgEd25519, result)
}

func TestXKMSBackend_attrsToAlgorithmString_Default(t *testing.T) {
	backend := &XKMSBackend{}

	// Empty attributes should default to AES-256-GCM
	attrs := &xkmssdk.KeyAttributes{}

	result := backend.attrsToAlgorithmString(attrs)
	assert.Equal(t, AlgAES256GCM, result)
}

// =============================================================================
// Helper Function Tests: normalizeAlgorithmForXKMS
// =============================================================================

func TestXKMSBackend_normalizeAlgorithmForXKMS(t *testing.T) {
	backend := &XKMSBackend{}

	// The function removes hyphens between letters and digits.
	// "AES-256-GCM" -> 's' (letter) - '2' (digit) = remove hyphen -> "aes256-gcm"
	// "RSA-4096" -> 'a' (letter) - '4' (digit) = remove hyphen -> "rsa4096"
	// "ChaCha20-Poly1305" -> '0' (digit) - 'p' (letter) = keep hyphen -> "chacha20-poly1305"
	// "ECDSA-P256" -> 'a' (letter) - 'p' (letter) = keep hyphen -> "ecdsa-p256"
	testCases := []struct {
		input    string
		expected string
	}{
		{"AES-256-GCM", "aes256-gcm"},
		{"AES-128-GCM", "aes128-gcm"},
		{"RSA-4096", "rsa4096"},
		{"RSA-2048", "rsa2048"},
		{"ChaCha20-Poly1305", "chacha20-poly1305"},
		{"ECDSA-P256", "ecdsa-p256"},
		{"ED25519", "ed25519"},
	}

	for _, tc := range testCases {
		t.Run(tc.input, func(t *testing.T) {
			result := backend.normalizeAlgorithmForXKMS(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestXKMSBackend_normalizeAlgorithmForXKMS_AlreadyLowercase(t *testing.T) {
	backend := &XKMSBackend{}

	result := backend.normalizeAlgorithmForXKMS("aes256-gcm")
	assert.Equal(t, "aes256-gcm", result)
}

func TestXKMSBackend_normalizeAlgorithmForXKMS_EmptyString(t *testing.T) {
	backend := &XKMSBackend{}

	result := backend.normalizeAlgorithmForXKMS("")
	assert.Equal(t, "", result)
}

// =============================================================================
// Helper Function Tests: algorithmToHash
// =============================================================================

func TestXKMSBackend_algorithmToHash(t *testing.T) {
	backend := &XKMSBackend{}

	testCases := []struct {
		algorithm    string
		expectedName string // Using name comparison since crypto.Hash is internal
	}{
		{"SHA-256", "SHA-256"},
		{"SHA256", "SHA-256"},
		{"SHA-384", "SHA-384"},
		{"SHA384", "SHA-384"},
		{"SHA-512", "SHA-512"},
		{"SHA512", "SHA-512"},
		{"SHA-1", "SHA-1"},
		{"SHA1", "SHA-1"},
		{"unknown", "SHA-256"}, // Default
		{"", "SHA-256"},        // Default
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			result := backend.algorithmToHash(tc.algorithm)
			assert.Equal(t, tc.expectedName, result.String())
		})
	}
}

// =============================================================================
// Helper Function Tests: serializeEncryptedData / deserializeEncryptedData
// =============================================================================

func TestXKMSBackend_serializeDeserializeEncryptedData(t *testing.T) {
	backend := &XKMSBackend{}

	original := &xkmssdk.EncryptedData{
		Ciphertext: []byte("encrypted-data"),
		Nonce:      []byte("nonce-value"),
		Tag:        []byte("auth-tag"),
	}

	// Serialize
	serialized, err := backend.serializeEncryptedData(original)
	require.NoError(t, err)
	require.NotEmpty(t, serialized)

	// Deserialize
	deserialized, err := backend.deserializeEncryptedData(serialized)
	require.NoError(t, err)
	require.NotNil(t, deserialized)

	// Verify
	assert.Equal(t, original.Ciphertext, deserialized.Ciphertext)
	assert.Equal(t, original.Nonce, deserialized.Nonce)
	assert.Equal(t, original.Tag, deserialized.Tag)
}

func TestXKMSBackend_deserializeEncryptedData_InvalidJSON(t *testing.T) {
	backend := &XKMSBackend{}

	result, err := backend.deserializeEncryptedData([]byte("invalid-json"))
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestXKMSBackend_deserializeEncryptedData_EmptyData(t *testing.T) {
	backend := &XKMSBackend{}

	result, err := backend.deserializeEncryptedData([]byte(""))
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestXKMSBackend_deserializeEncryptedData_NullJSON(t *testing.T) {
	backend := &XKMSBackend{}

	// When JSON is "null", json.Unmarshal succeeds and returns
	// a pointer to an empty struct (not nil)
	result, err := backend.deserializeEncryptedData([]byte("null"))
	assert.NoError(t, err)
	// Result is a pointer to empty struct, not nil
	require.NotNil(t, result)
	assert.Empty(t, result.Ciphertext)
	assert.Empty(t, result.Nonce)
	assert.Empty(t, result.Tag)
}

func TestXKMSBackend_deserializeEncryptedData_ValidEmptyObject(t *testing.T) {
	backend := &XKMSBackend{}

	result, err := backend.deserializeEncryptedData([]byte("{}"))
	assert.NoError(t, err)
	require.NotNil(t, result)
	assert.Empty(t, result.Ciphertext)
	assert.Empty(t, result.Nonce)
	assert.Empty(t, result.Tag)
}

// =============================================================================
// Backend Interface Compliance Test
// =============================================================================

func TestXKMSBackend_ImplementsBackend(t *testing.T) {
	// Verify that XKMSBackend implements the Backend interface
	var _ Backend = (*XKMSBackend)(nil)
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestXKMSBackend_ConcurrentMetadataAccess(t *testing.T) {
	cleanup := setupTestXKMS(t)
	defer cleanup()

	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
		logger:      &mockLogger{},
		metadata:    make(map[string]*KeyMetadata),
	}

	// Pre-populate some metadata
	now := time.Now()
	for i := 0; i < 10; i++ {
		keyID := "key-" + string(rune('A'+i))
		backend.metadata[keyID] = &KeyMetadata{
			KeyID:   keyID,
			State:   KeyStateEnabled,
			Version: 1,
			Created: now,
			Updated: now,
		}
	}

	// Run concurrent operations on metadata cache
	done := make(chan bool)
	ctx := context.Background()

	// Reader goroutines
	for i := 0; i < 5; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				// These will fail due to xkms not being initialized,
				// but they should not cause race conditions
				_, _ = backend.GetKeyMetadata(ctx, "key-A")
				_ = backend.DisableKey(ctx, "key-B")
				_ = backend.EnableKey(ctx, "key-C")
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}
}

// =============================================================================
// Error Message Tests
// =============================================================================

func TestErrBackendNotReady(t *testing.T) {
	assert.Equal(t, "backend not ready", ErrBackendNotReady.Error())
}

// =============================================================================
// Edge Case Tests for Algorithm Mapping
// =============================================================================

func TestXKMSBackend_algorithmToKeyAttributes_CaseInsensitive(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	testCases := []struct {
		algorithm string
		expected  xkmssdk.SymmetricAlgorithm
	}{
		{"aes-256-gcm", xkmssdk.SymmetricAES256GCM},
		{"AES-256-GCM", xkmssdk.SymmetricAES256GCM},
		{"Aes-256-Gcm", xkmssdk.SymmetricAES256GCM},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			attrs := backend.algorithmToKeyAttributes("test", tc.algorithm)
			assert.Equal(t, tc.expected, attrs.SymmetricAlgorithm)
		})
	}
}

func TestXKMSBackend_algorithmToKeyAttributes_AlternateFormats(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	// Test alternate algorithm name formats
	testCases := []struct {
		algorithm string
		checkRSA  bool
		rsaSize   int
	}{
		{"RSA4096", true, 4096},
		{"RSA-4096", true, 4096},
		{"rsa-4096", true, 4096},
		{"RSA3072", true, 3072},
		{"RSA2048", true, 2048},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			attrs := backend.algorithmToKeyAttributes("test", tc.algorithm)
			if tc.checkRSA {
				assert.Equal(t, x509.RSA, attrs.KeyAlgorithm)
				require.NotNil(t, attrs.RSAAttributes)
				assert.Equal(t, tc.rsaSize, attrs.RSAAttributes.KeySize)
			}
		})
	}
}

func TestXKMSBackend_algorithmToKeyAttributes_ECAlternateFormats(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	testCases := []struct {
		algorithm string
		curve     elliptic.Curve
	}{
		{"P-256", elliptic.P256()},
		{"P256", elliptic.P256()},
		{"P-384", elliptic.P384()},
		{"P384", elliptic.P384()},
		{"P-521", elliptic.P521()},
		{"P521", elliptic.P521()},
		{"EC-P256", elliptic.P256()},
	}

	for _, tc := range testCases {
		t.Run(tc.algorithm, func(t *testing.T) {
			attrs := backend.algorithmToKeyAttributes("test", tc.algorithm)
			assert.Equal(t, x509.ECDSA, attrs.KeyAlgorithm)
			require.NotNil(t, attrs.ECCAttributes)
			assert.Equal(t, tc.curve, attrs.ECCAttributes.Curve)
		})
	}
}

// =============================================================================
// StoreType Propagation Test
// =============================================================================

func TestXKMSBackend_algorithmToKeyAttributes_StoreType(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeTPM2,
	}

	attrs := backend.algorithmToKeyAttributes("test-key", AlgAES256GCM)
	assert.Equal(t, xkmssdk.StoreType("tpm2"), attrs.StoreType)
}

func TestXKMSBackend_algorithmToKeyAttributes_KeyType(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	// Symmetric key should have encryption key type
	symAttrs := backend.algorithmToKeyAttributes("test-key", AlgAES256GCM)
	assert.Equal(t, xkmssdk.KeyTypeEncryption, symAttrs.KeyType)

	// Asymmetric signing key should have signing key type
	rsaAttrs := backend.algorithmToKeyAttributes("test-key", AlgRSA4096)
	assert.Equal(t, xkmssdk.KeyTypeSigning, rsaAttrs.KeyType)
}

// =============================================================================
// buildVersionedKeyID Tests
// =============================================================================

func TestXKMSBackend_buildVersionedKeyID(t *testing.T) {
	backend := &XKMSBackend{
		backendType: config.BackendTypeSoftware,
	}

	testCases := []struct {
		keyID    string
		version  int
		expected string
	}{
		{"my-key", 1, "my-key_v1"},
		{"my-key", 2, "my-key_v2"},
		{"test-path-key", 10, "test-path-key_v10"},
		{"", 1, "_v1"},
		{"key-with-special-chars", 5, "key-with-special-chars_v5"},
	}

	for _, tc := range testCases {
		t.Run(tc.expected, func(t *testing.T) {
			result := backend.buildVersionedKeyID(tc.keyID, tc.version)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// =============================================================================
// Key Version Metadata Tests
// =============================================================================

func TestXKMSBackend_keyVersionMetadataKey(t *testing.T) {
	// Verify the metadata key is short but meaningful
	assert.Equal(t, "kv", keyVersionMetadataKey)
}
