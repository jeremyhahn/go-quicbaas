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

package catalog

import (
	"context"
	"crypto/sha256"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// mockDEKProvider implements DEKProvider for testing.
type mockDEKProvider struct {
	getDEKFunc       func(ctx context.Context, req *GetDEKRequest) (*GetDEKResponse, error)
	getDEKCallCount  int
	lastGetDEKPath   string
	dekMetadataStore map[string]*DEKMetadata
}

func newMockDEKProvider() *mockDEKProvider {
	return &mockDEKProvider{
		dekMetadataStore: make(map[string]*DEKMetadata),
	}
}

func (m *mockDEKProvider) GetDEK(ctx context.Context, req *GetDEKRequest) (*GetDEKResponse, error) {
	m.getDEKCallCount++
	m.lastGetDEKPath = req.Path

	if m.getDEKFunc != nil {
		return m.getDEKFunc(ctx, req)
	}

	// Derive deterministic key material from the path using SHA-256.
	// This ensures encrypt and decrypt use the same key for a given path.
	keyMaterial := mockDeriveKey(req.Path)
	wrappedKey := mockWrapKey(req.Path)

	// Default behavior: return a DEK with the path and key material
	if dek, ok := m.dekMetadataStore[req.Path]; ok {
		return &GetDEKResponse{
			DEK:         dek,
			KeyMaterial: keyMaterial,
			WrappedKey:  wrappedKey,
			Timestamp:   time.Now(),
		}, nil
	}

	return &GetDEKResponse{
		DEK: &DEKMetadata{
			Path:    req.Path,
			Version: 1,
			Status:  DEKStatusActive,
		},
		KeyMaterial: keyMaterial,
		WrappedKey:  wrappedKey,
		Timestamp:   time.Now(),
	}, nil
}

// mockDeriveKey derives a deterministic 32-byte key from a path for testing.
func mockDeriveKey(path string) []byte {
	h := sha256.Sum256([]byte("mock-dek-key:" + path))
	return h[:]
}

// mockWrapKey produces a deterministic wrapped key for testing.
func mockWrapKey(path string) []byte {
	h := sha256.Sum256([]byte("mock-wrapped-key:" + path))
	return h[:]
}

// mockConfigResolver implements config.ObjectStoreConfigResolver for testing.
type mockConfigResolver struct {
	encryptionConfig *config.ObjectStoreEncryption
	resolveError     error
}

func newMockConfigResolver() *mockConfigResolver {
	return &mockConfigResolver{
		encryptionConfig: &config.ObjectStoreEncryption{
			Enabled:    true,
			Enforce:    true,
			DefaultKey: "test/key/v1",
			Algorithm:  "AES-256-GCM",
		},
	}
}

func (m *mockConfigResolver) ResolveConfig(tenantID string, shardID uint64) (*config.ResolvedObjectStoreConfig, error) {
	return &config.ResolvedObjectStoreConfig{
		Encryption: m.encryptionConfig,
	}, nil
}

func (m *mockConfigResolver) ResolveBackend(tenantID string, shardID uint64) (string, error) {
	return "default", nil
}

func (m *mockConfigResolver) ResolveEncryption(tenantID string, shardID uint64) (*config.ObjectStoreEncryption, error) {
	if m.resolveError != nil {
		return nil, m.resolveError
	}
	return m.encryptionConfig, nil
}

func (m *mockConfigResolver) ResolveReplication(tenantID string, shardID uint64) (*config.ObjectStoreReplication, error) {
	return nil, nil
}

func (m *mockConfigResolver) CheckQuotas(tenantID string, objectSize int64, usage *config.TenantUsage) error {
	return nil
}

func (m *mockConfigResolver) GetTenantConfig(tenantID string) *config.ObjectStoreTenantConfig {
	return nil
}

func (m *mockConfigResolver) ListTenants() []string {
	return nil
}

func (m *mockConfigResolver) Reload(fullConfig *config.ObjectStoreFullConfig, nodeConfig *config.NodeObjectStorageConfig) error {
	return nil
}

func (m *mockConfigResolver) SetTenantQuotas(ctx context.Context, tenantID string, quotas *config.TenantQuotas) error {
	return nil
}

// TestNewEncryptionManager tests the constructor.
func TestNewEncryptionManager(t *testing.T) {
	t.Run("nil DEK provider returns error", func(t *testing.T) {
		resolver := newMockConfigResolver()
		_, err := NewEncryptionManager(nil, resolver)
		if err != ErrKMSServiceNil {
			t.Errorf("expected ErrKMSServiceNil, got %v", err)
		}
	})

	t.Run("nil config resolver returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		_, err := NewEncryptionManager(dek, nil)
		if err != ErrConfigResolverNil {
			t.Errorf("expected ErrConfigResolverNil, got %v", err)
		}
	})

	t.Run("valid parameters succeeds", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, err := NewEncryptionManager(dek, resolver)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if mgr == nil {
			t.Fatal("expected non-nil manager")
		}
	})
}

// TestEncryptionManager_Encrypt tests encryption operations.
func TestEncryptionManager_Encrypt(t *testing.T) {
	ctx := context.Background()

	t.Run("nil config returns unencrypted data", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		data := []byte("test data")
		result, err := mgr.Encrypt(ctx, data, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Encryption != nil {
			t.Error("expected nil encryption info for unencrypted data")
		}
		if string(result.Ciphertext) != "test data" {
			t.Error("expected data to be unchanged")
		}
	})

	t.Run("disabled encryption returns unencrypted data", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		data := []byte("test data")
		cfg := &config.ObjectStoreEncryption{
			Enabled: false,
		}
		result, err := mgr.Encrypt(ctx, data, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Encryption != nil {
			t.Error("expected nil encryption info")
		}
	})

	t.Run("empty data returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		cfg := &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "test/key",
		}
		_, err := mgr.Encrypt(ctx, []byte{}, cfg)
		if !IsEncryptionError(err) {
			t.Errorf("expected encryption error, got %v", err)
		}
	})

	t.Run("no key configured returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		cfg := &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "",
		}
		_, err := mgr.Encrypt(ctx, []byte("data"), cfg)
		if !IsEncryptionError(err) {
			t.Errorf("expected encryption error, got %v", err)
		}
	})

	t.Run("DEK provider error propagates", func(t *testing.T) {
		dek := newMockDEKProvider()
		dek.getDEKFunc = func(ctx context.Context, req *GetDEKRequest) (*GetDEKResponse, error) {
			return nil, errors.New("KMS unavailable")
		}
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		cfg := &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "test/key",
			Algorithm:  "AES-256-GCM",
		}
		_, err := mgr.Encrypt(ctx, []byte("data"), cfg)
		if !IsEncryptionError(err) {
			t.Errorf("expected encryption error, got %v", err)
		}
	})

	t.Run("successful encryption returns ciphertext and metadata", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		data := []byte("secret data for encryption test")
		cfg := &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "test/encryption/key/v1",
			Algorithm:  "AES-256-GCM",
		}

		result, err := mgr.Encrypt(ctx, data, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Encryption == nil {
			t.Fatal("expected non-nil encryption info")
		}
		if result.Encryption.KeyID != cfg.DefaultKey {
			t.Errorf("expected key ID %s, got %s", cfg.DefaultKey, result.Encryption.KeyID)
		}
		if result.Encryption.Algorithm != cfg.Algorithm {
			t.Errorf("expected algorithm %s, got %s", cfg.Algorithm, result.Encryption.Algorithm)
		}
		if len(result.Encryption.Nonce) == 0 {
			t.Error("expected non-empty nonce")
		}
		// Ciphertext should be different from plaintext
		if string(result.Ciphertext) == string(data) {
			t.Error("ciphertext should differ from plaintext")
		}
	})

	t.Run("encryption with default algorithm", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		data := []byte("test data")
		cfg := &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "test/key",
			Algorithm:  "", // Should default to AES-256-GCM
		}

		result, err := mgr.Encrypt(ctx, data, cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Encryption.Algorithm != config.DefaultEncryptionAlgo {
			t.Errorf("expected default algorithm %s, got %s", config.DefaultEncryptionAlgo, result.Encryption.Algorithm)
		}
	})
}

// TestEncryptionManager_Decrypt tests decryption operations.
func TestEncryptionManager_Decrypt(t *testing.T) {
	ctx := context.Background()

	t.Run("nil encryption info returns data unchanged", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		data := []byte("unencrypted data")
		result, err := mgr.Decrypt(ctx, data, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if string(result) != string(data) {
			t.Error("expected data to be unchanged")
		}
	})

	t.Run("empty data returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		enc := &LocationEncryption{
			KeyID:     "test/key",
			Algorithm: "AES-256-GCM",
			Nonce:     make([]byte, 12),
		}
		_, err := mgr.Decrypt(ctx, []byte{}, enc)
		if !IsDecryptionError(err) {
			t.Errorf("expected decryption error, got %v", err)
		}
	})

	t.Run("no key ID returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		enc := &LocationEncryption{
			KeyID:     "",
			Algorithm: "AES-256-GCM",
		}
		_, err := mgr.Decrypt(ctx, []byte("data"), enc)
		if !IsDecryptionError(err) {
			t.Errorf("expected decryption error, got %v", err)
		}
	})

	t.Run("encrypt and decrypt roundtrip", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		originalData := []byte("sensitive information that needs protection")
		cfg := &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "test/roundtrip/key",
			Algorithm:  "AES-256-GCM",
		}

		// Encrypt
		encResult, err := mgr.Encrypt(ctx, originalData, cfg)
		if err != nil {
			t.Fatalf("encrypt failed: %v", err)
		}

		// Decrypt
		decrypted, err := mgr.Decrypt(ctx, encResult.Ciphertext, encResult.Encryption)
		if err != nil {
			t.Fatalf("decrypt failed: %v", err)
		}

		if string(decrypted) != string(originalData) {
			t.Errorf("decrypted data does not match original: got %q, want %q", string(decrypted), string(originalData))
		}
	})

	t.Run("invalid nonce size returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		// Create valid ciphertext first
		cfg := &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "test/key",
			Algorithm:  "AES-256-GCM",
		}
		encResult, _ := mgr.Encrypt(ctx, []byte("test data"), cfg)

		// Modify nonce to invalid size
		enc := &LocationEncryption{
			KeyID:     encResult.Encryption.KeyID,
			Algorithm: encResult.Encryption.Algorithm,
			Nonce:     []byte{1, 2, 3}, // Invalid nonce size
		}

		_, err := mgr.Decrypt(ctx, encResult.Ciphertext, enc)
		if err == nil {
			t.Error("expected error for invalid nonce size")
		}
	})
}

// TestEncryptionManager_ResolveEncryptionConfig tests config resolution.
func TestEncryptionManager_ResolveEncryptionConfig(t *testing.T) {
	t.Run("object override takes precedence", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		resolver.encryptionConfig = &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "default/key",
			Algorithm:  "AES-256-GCM",
		}
		mgr, _ := NewEncryptionManager(dek, resolver)

		override := &ObjectEncryption{
			Enabled:   true,
			KeyID:     "object/specific/key",
			Algorithm: "AES-128-GCM",
		}

		cfg, err := mgr.ResolveEncryptionConfig("tenant1", 1, override)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DefaultKey != override.KeyID {
			t.Errorf("expected override key %s, got %s", override.KeyID, cfg.DefaultKey)
		}
		if cfg.Algorithm != override.Algorithm {
			t.Errorf("expected override algorithm %s, got %s", override.Algorithm, cfg.Algorithm)
		}
	})

	t.Run("nil override uses resolver config", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		resolver.encryptionConfig = &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "tenant/default/key",
			Algorithm:  "AES-256-GCM",
		}
		mgr, _ := NewEncryptionManager(dek, resolver)

		cfg, err := mgr.ResolveEncryptionConfig("tenant1", 1, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DefaultKey != resolver.encryptionConfig.DefaultKey {
			t.Errorf("expected resolver key %s, got %s", resolver.encryptionConfig.DefaultKey, cfg.DefaultKey)
		}
	})

	t.Run("disabled override uses resolver config", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		override := &ObjectEncryption{
			Enabled: false,
			KeyID:   "ignored/key",
		}

		cfg, err := mgr.ResolveEncryptionConfig("tenant1", 1, override)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.DefaultKey == override.KeyID {
			t.Error("disabled override key should be ignored")
		}
	})

	t.Run("resolver error propagates", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		resolver.resolveError = errors.New("config resolution failed")
		mgr, _ := NewEncryptionManager(dek, resolver)

		_, err := mgr.ResolveEncryptionConfig("tenant1", 1, nil)
		if !IsEncryptionError(err) {
			t.Errorf("expected encryption error, got %v", err)
		}
	})
}

// TestEncryptionManager_ShouldEncrypt tests the encryption decision logic.
func TestEncryptionManager_ShouldEncrypt(t *testing.T) {
	dek := newMockDEKProvider()
	resolver := newMockConfigResolver()
	mgr, _ := NewEncryptionManager(dek, resolver)

	t.Run("nil config returns false", func(t *testing.T) {
		if mgr.ShouldEncrypt(nil) {
			t.Error("expected false for nil config")
		}
	})

	t.Run("disabled returns false", func(t *testing.T) {
		cfg := &config.ObjectStoreEncryption{
			Enabled: false,
			Enforce: true,
		}
		if mgr.ShouldEncrypt(cfg) {
			t.Error("expected false when disabled")
		}
	})

	t.Run("not enforced returns false", func(t *testing.T) {
		cfg := &config.ObjectStoreEncryption{
			Enabled: true,
			Enforce: false,
		}
		if mgr.ShouldEncrypt(cfg) {
			t.Error("expected false when not enforced")
		}
	})

	t.Run("enabled and enforced returns true", func(t *testing.T) {
		cfg := &config.ObjectStoreEncryption{
			Enabled: true,
			Enforce: true,
		}
		if !mgr.ShouldEncrypt(cfg) {
			t.Error("expected true when enabled and enforced")
		}
	})
}

// TestEncryptionManager_Reencrypt tests re-encryption operations.
func TestEncryptionManager_Reencrypt(t *testing.T) {
	ctx := context.Background()

	t.Run("empty data returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		_, err := mgr.Reencrypt(ctx, []byte{}, nil, "new/key")
		if !IsEncryptionError(err) {
			t.Errorf("expected encryption error, got %v", err)
		}
	})

	t.Run("empty new key returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		_, err := mgr.Reencrypt(ctx, []byte("data"), nil, "")
		if !IsEncryptionError(err) {
			t.Errorf("expected encryption error, got %v", err)
		}
	})

	t.Run("successful reencrypt with same algorithm", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		// First encrypt with old key
		originalData := []byte("data to reencrypt")
		oldCfg := &config.ObjectStoreEncryption{
			Enabled:    true,
			DefaultKey: "old/key",
			Algorithm:  "AES-256-GCM",
		}
		encResult, err := mgr.Encrypt(ctx, originalData, oldCfg)
		if err != nil {
			t.Fatalf("initial encrypt failed: %v", err)
		}

		// Re-encrypt with new key
		newKeyID := "new/rotated/key"
		reencResult, err := mgr.Reencrypt(ctx, encResult.Ciphertext, encResult.Encryption, newKeyID)
		if err != nil {
			t.Fatalf("reencrypt failed: %v", err)
		}

		// Verify new encryption info
		if reencResult.Encryption.KeyID != newKeyID {
			t.Errorf("expected new key ID %s, got %s", newKeyID, reencResult.Encryption.KeyID)
		}

		// Verify we can decrypt with new key
		decrypted, err := mgr.Decrypt(ctx, reencResult.Ciphertext, reencResult.Encryption)
		if err != nil {
			t.Fatalf("decrypt after reencrypt failed: %v", err)
		}
		if string(decrypted) != string(originalData) {
			t.Errorf("decrypted data mismatch: got %q, want %q", string(decrypted), string(originalData))
		}
	})

	t.Run("reencrypt unencrypted data", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		data := []byte("previously unencrypted data")
		newKeyID := "new/key"

		result, err := mgr.Reencrypt(ctx, data, nil, newKeyID)
		if err != nil {
			t.Fatalf("reencrypt failed: %v", err)
		}
		if result.Encryption.KeyID != newKeyID {
			t.Errorf("expected key ID %s, got %s", newKeyID, result.Encryption.KeyID)
		}
	})
}

// TestEncryptionManager_ValidateKey tests key validation.
func TestEncryptionManager_ValidateKey(t *testing.T) {
	ctx := context.Background()

	t.Run("empty key ID returns error", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		err := mgr.ValidateKey(ctx, "")
		if !IsKeyNotFoundError(err) {
			t.Errorf("expected key not found error, got %v", err)
		}
	})

	t.Run("DEK provider error returns key not found", func(t *testing.T) {
		dek := newMockDEKProvider()
		dek.getDEKFunc = func(ctx context.Context, req *GetDEKRequest) (*GetDEKResponse, error) {
			return nil, errors.New("key not found")
		}
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		err := mgr.ValidateKey(ctx, "nonexistent/key")
		if !IsKeyNotFoundError(err) {
			t.Errorf("expected key not found error, got %v", err)
		}
	})

	t.Run("valid key returns nil", func(t *testing.T) {
		dek := newMockDEKProvider()
		resolver := newMockConfigResolver()
		mgr, _ := NewEncryptionManager(dek, resolver)

		err := mgr.ValidateKey(ctx, "valid/key")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// TestEncryptionErrors tests error type functions.
func TestEncryptionErrors(t *testing.T) {
	t.Run("NewEncryptionError creates correct error", func(t *testing.T) {
		err := NewEncryptionError("test op", errors.New("cause"))
		if err.Code != ErrCodeEncryptionFailed {
			t.Errorf("expected code %s, got %s", ErrCodeEncryptionFailed, err.Code)
		}
		if err.Cause == nil || err.Cause.Error() != "cause" {
			t.Error("cause not preserved")
		}
	})

	t.Run("NewDecryptionError creates correct error", func(t *testing.T) {
		err := NewDecryptionError("test op", errors.New("cause"))
		if err.Code != ErrCodeDecryptionFailed {
			t.Errorf("expected code %s, got %s", ErrCodeDecryptionFailed, err.Code)
		}
	})

	t.Run("NewKeyNotFoundError creates correct error", func(t *testing.T) {
		err := NewKeyNotFoundError("missing/key")
		if err.Code != ErrCodeKeyNotFound {
			t.Errorf("expected code %s, got %s", ErrCodeKeyNotFound, err.Code)
		}
	})

	t.Run("NewInvalidNonceError creates correct error", func(t *testing.T) {
		err := NewInvalidNonceError(12, 8)
		if err.Code != ErrCodeInvalidNonce {
			t.Errorf("expected code %s, got %s", ErrCodeInvalidNonce, err.Code)
		}
	})

	t.Run("NewInvalidKeySizeError creates correct error", func(t *testing.T) {
		err := NewInvalidKeySizeError("AES-256-GCM", 32, 16)
		if err.Code != ErrCodeInvalidKeySize {
			t.Errorf("expected code %s, got %s", ErrCodeInvalidKeySize, err.Code)
		}
	})

	t.Run("NewInvalidAlgorithmError creates correct error", func(t *testing.T) {
		err := NewInvalidAlgorithmError("UNKNOWN")
		if err.Code != ErrCodeInvalidAlgorithm {
			t.Errorf("expected code %s, got %s", ErrCodeInvalidAlgorithm, err.Code)
		}
	})

	t.Run("IsEncryptionError detects encryption errors", func(t *testing.T) {
		if IsEncryptionError(nil) {
			t.Error("expected false for nil")
		}
		if IsEncryptionError(errors.New("random error")) {
			t.Error("expected false for non-catalog error")
		}
		if !IsEncryptionError(NewEncryptionError("op", nil)) {
			t.Error("expected true for encryption error")
		}
		if !IsEncryptionError(NewDecryptionError("op", nil)) {
			t.Error("expected true for decryption error")
		}
		if !IsEncryptionError(NewKeyNotFoundError("key")) {
			t.Error("expected true for key not found error")
		}
	})

	t.Run("IsDecryptionError detects decryption errors", func(t *testing.T) {
		if IsDecryptionError(nil) {
			t.Error("expected false for nil")
		}
		if IsDecryptionError(NewEncryptionError("op", nil)) {
			t.Error("expected false for encryption error")
		}
		if !IsDecryptionError(NewDecryptionError("op", nil)) {
			t.Error("expected true for decryption error")
		}
	})
}

// TestAES128GCMEncryption tests AES-128-GCM algorithm support.
func TestAES128GCMEncryption(t *testing.T) {
	ctx := context.Background()
	dek := newMockDEKProvider()
	resolver := newMockConfigResolver()
	mgr, _ := NewEncryptionManager(dek, resolver)

	data := []byte("test data for AES-128")
	cfg := &config.ObjectStoreEncryption{
		Enabled:    true,
		DefaultKey: "test/aes128/key",
		Algorithm:  "AES-128-GCM",
	}

	// Encrypt
	encResult, err := mgr.Encrypt(ctx, data, cfg)
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if encResult.Encryption.Algorithm != "AES-128-GCM" {
		t.Errorf("expected AES-128-GCM algorithm, got %s", encResult.Encryption.Algorithm)
	}

	// Decrypt
	decrypted, err := mgr.Decrypt(ctx, encResult.Ciphertext, encResult.Encryption)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if string(decrypted) != string(data) {
		t.Errorf("decrypted data mismatch: got %q, want %q", string(decrypted), string(data))
	}
}

// TestDEKTypes tests the DEK type definitions.
func TestDEKTypes(t *testing.T) {
	t.Run("DEKStatus constants", func(t *testing.T) {
		if DEKStatusActive != "active" {
			t.Errorf("expected 'active', got %s", DEKStatusActive)
		}
		if DEKStatusDisabled != "disabled" {
			t.Errorf("expected 'disabled', got %s", DEKStatusDisabled)
		}
		if DEKStatusRotating != "rotating" {
			t.Errorf("expected 'rotating', got %s", DEKStatusRotating)
		}
	})

	t.Run("DEKMetadata fields", func(t *testing.T) {
		now := time.Now()
		dek := &DEKMetadata{
			Path:      "test/path",
			Version:   1,
			Status:    DEKStatusActive,
			IsDefault: true,
			CreatedAt: now,
			RotatedAt: now,
		}
		if dek.Path != "test/path" {
			t.Error("path not set correctly")
		}
		if dek.Version != 1 {
			t.Error("version not set correctly")
		}
		if dek.Status != DEKStatusActive {
			t.Error("status not set correctly")
		}
	})
}
