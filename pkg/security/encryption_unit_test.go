//go:build !integration

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

package security

import (
	"context"
	"crypto/rand"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateNonceSuccess(t *testing.T) {
	// Test successful nonce generation
	nonce, err := generateNonce()
	require.NoError(t, err)
	assert.Len(t, nonce, NonceSize)

	// Verify nonce is random (two calls should produce different results)
	nonce2, err := generateNonce()
	require.NoError(t, err)
	assert.NotEqual(t, nonce, nonce2)
}

func TestGenerateNonceReadError(t *testing.T) {
	// Save original rand.Reader
	originalReader := rand.Reader
	defer func() { rand.Reader = originalReader }()

	// Replace with failing reader
	rand.Reader = &failingReader{}

	nonce, err := generateNonce()
	assert.Error(t, err)
	assert.Nil(t, nonce)
}

// failingReader always returns an error.
type failingReader struct{}

func (f *failingReader) Read(p []byte) (n int, err error) {
	return 0, io.ErrUnexpectedEOF
}

func TestKeyring_AlgorithmToSymmetricAllVariants(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{name: "AES-256-GCM uppercase", algorithm: AlgorithmAES256GCM},
		{name: "aes256-gcm lowercase", algorithm: "aes256-gcm"},
		{name: "AES256-GCM mixed", algorithm: "AES256-GCM"},
		{name: "AES-192-GCM uppercase", algorithm: "AES-192-GCM"},
		{name: "aes192-gcm lowercase", algorithm: "aes192-gcm"},
		{name: "AES-128-GCM uppercase", algorithm: "AES-128-GCM"},
		{name: "aes128-gcm lowercase", algorithm: "aes128-gcm"},
		{name: "default fallback", algorithm: "unknown"},
	}

	log := createTestLogger(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &EncryptionConfig{
				Algorithm:   tt.algorithm,
				KeyProvider: "local",
			}

			kr := &Keyring{
				config: cfg,
				logger: log,
			}

			// Just verify it doesn't panic and returns a value
			result := kr.algorithmToSymmetric()
			assert.NotNil(t, result)
		})
	}
}

func TestKeyring_AlgorithmToKeySizeAllSizes(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		expected  int
	}{
		{name: "AES-256-GCM", algorithm: AlgorithmAES256GCM, expected: 256},
		{name: "aes256-gcm", algorithm: "aes256-gcm", expected: 256},
		{name: "AES256-GCM", algorithm: "AES256-GCM", expected: 256},
		{name: "AES-192-GCM", algorithm: "AES-192-GCM", expected: 192},
		{name: "aes192-gcm", algorithm: "aes192-gcm", expected: 192},
		{name: "AES-128-GCM", algorithm: "AES-128-GCM", expected: 128},
		{name: "aes128-gcm", algorithm: "aes128-gcm", expected: 128},
		{name: "default fallback", algorithm: "unknown", expected: 256},
		{name: "empty string", algorithm: "", expected: 256},
	}

	log := createTestLogger(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &EncryptionConfig{
				Algorithm:   tt.algorithm,
				KeyProvider: "local",
			}

			kr := &Keyring{
				config: cfg,
				logger: log,
			}

			result := kr.algorithmToKeySize()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestKeyring_IsValidAlgorithmCheck(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		valid     bool
	}{
		{name: "AES-256-GCM valid", algorithm: AlgorithmAES256GCM, valid: true},
		{name: "aes256-gcm valid", algorithm: "aes256-gcm", valid: true},
		{name: "AES256-GCM valid", algorithm: "AES256-GCM", valid: true},
		{name: "AES-192-GCM valid", algorithm: "AES-192-GCM", valid: true},
		{name: "aes192-gcm valid", algorithm: "aes192-gcm", valid: true},
		{name: "AES-128-GCM valid", algorithm: "AES-128-GCM", valid: true},
		{name: "aes128-gcm valid", algorithm: "aes128-gcm", valid: true},
		{name: "empty string valid", algorithm: "", valid: true},
		{name: "ChaCha20 invalid", algorithm: AlgorithmChaCha20Poly, valid: false},
		{name: "unknown invalid", algorithm: "unknown", valid: false},
	}

	log := createTestLogger(t)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &EncryptionConfig{
				Algorithm:   tt.algorithm,
				KeyProvider: "local",
			}

			kr := &Keyring{
				config: cfg,
				logger: log,
			}

			result := kr.isValidAlgorithm()
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestInitializeWithVaultPlaceholder(t *testing.T) {
	cfg := &EncryptionConfig{
		VaultAddr:  "http://vault.example.com",
		VaultToken: "test-token",
	}

	log := createTestLogger(t)

	// Should return error indicating it requires external package
	err := InitializeWithVault(cfg, log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "vault backend requires explicit initialization")
}

func TestInitializeWithAWSKMSPlaceholder(t *testing.T) {
	cfg := &EncryptionConfig{
		KMSKeyID:  "test-key-id",
		KMSRegion: "us-west-2",
	}

	log := createTestLogger(t)

	// Should return error indicating it requires external package
	err := InitializeWithAWSKMS(cfg, log)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "awskms backend requires explicit initialization")
}

func TestKeyring_InitializeXKMSVaultProvider(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "vault",
		VaultAddr:   "http://vault.example.com",
		VaultToken:  "test-token",
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config: cfg,
		logger: log,
	}

	err := kr.initializeXKMS()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "InitializeWithVault")
}

func TestKeyring_InitializeXKMSAWSKMS(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "awskms",
		KMSKeyID:    "test-key-id",
		KMSRegion:   "us-west-2",
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config: cfg,
		logger: log,
	}

	err := kr.initializeXKMS()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "InitializeWithAWSKMS")
}

func TestKeyring_InitializeXKMSAzureKV(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider:   "azurekv",
		AzureVaultURL: "https://test.vault.azure.net",
		AzureKeyName:  "test-key",
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config: cfg,
		logger: log,
	}

	err := kr.initializeXKMS()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "InitializeWithAzureKV")
}

func TestKeyring_InitializeXKMSGCPKMS(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "gcpkms",
		GCPProject:  "test-project",
		GCPLocation: "us-central1",
		GCPKeyRing:  "test-keyring",
		GCPKeyName:  "test-key",
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config: cfg,
		logger: log,
	}

	err := kr.initializeXKMS()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "InitializeWithGCPKMS")
}

func TestKeyring_InitializeXKMSUnsupported(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "unsupported-provider",
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config: cfg,
		logger: log,
	}

	err := kr.initializeXKMS()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported key provider")
}

func TestKeyring_LoadOrGenerateKeyWithoutBackend(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config:  cfg,
		logger:  log,
		backend: nil, // No backend initialized
	}

	key, err := kr.loadOrGenerateKey(context.Background())
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Contains(t, err.Error(), "backend not initialized")
}

func TestKeyring_AutoRotateWithoutBackend(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
		RotateDays:  1,
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config:  cfg,
		logger:  log,
		backend: nil,
	}

	err := kr.autoRotate()
	assert.Error(t, err)
}

func TestNewKeyringUnsupportedAlgorithm(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmChaCha20Poly,
	}

	log := createTestLogger(t)

	kr, err := NewKeyring(cfg, log)
	assert.Error(t, err)
	assert.Nil(t, kr)
	assert.Contains(t, err.Error(), "unsupported algorithm")
}

func TestKeyring_EncryptWithoutEncrypter(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config:    cfg,
		logger:    log,
		encrypter: nil, // No encrypter
	}

	data, err := kr.Encrypt([]byte("test"))
	assert.Error(t, err)
	assert.Nil(t, data)
	assert.Contains(t, err.Error(), "no encrypter available")
}

func TestKeyring_DecryptWithoutEncrypter(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config:    cfg,
		logger:    log,
		encrypter: nil, // No encrypter
	}

	plaintext, err := kr.Decrypt(nil)
	assert.Error(t, err)
	assert.Nil(t, plaintext)
	assert.Contains(t, err.Error(), "no encrypter available")
}

func TestKeyring_RotateKeyWithoutBackend(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}

	log := createTestLogger(t)

	kr := &Keyring{
		config:  cfg,
		logger:  log,
		backend: nil, // No backend
	}

	key, err := kr.RotateKey(context.Background())
	assert.Error(t, err)
	assert.Nil(t, key)
	assert.Contains(t, err.Error(), "backend not initialized")
}

func TestEncryptionManager_DecryptCiphertextTooShort(t *testing.T) {
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}

	log := createTestLogger(t)
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)

	// Ciphertext too short (less than version + nonce + min tag)
	shortData := []byte{1, 2, 3}
	plaintext, err := em.Decrypt(shortData)
	assert.Error(t, err)
	assert.Nil(t, plaintext)
	assert.Contains(t, err.Error(), "ciphertext too short")
}

func TestEncryptionManager_DecryptWithInvalidKeyVersion(t *testing.T) {
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}

	log := createTestLogger(t)
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)

	// Create data with invalid key version
	invalidData := make([]byte, 1+NonceSize+20)
	invalidData[0] = 99 // Non-existent key version

	plaintext, err := em.Decrypt(invalidData)
	assert.Error(t, err)
	assert.Nil(t, plaintext)
	assert.Contains(t, err.Error(), "encryption key version")
}

func TestEncryptionManager_DecryptCiphertextTooShortForTag(t *testing.T) {
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}

	log := createTestLogger(t)
	em, err := NewEncryptionManager(cfg, log)
	require.NoError(t, err)

	// Create data that's long enough initially but too short for tag extraction
	invalidData := make([]byte, 1+NonceSize+5) // Too short for 16-byte tag
	invalidData[0] = 1                         // Valid version

	plaintext, err := em.Decrypt(invalidData)
	assert.Error(t, err)
	assert.Nil(t, plaintext)
	assert.Contains(t, err.Error(), "ciphertext too short")
}

func TestEncryptionManager_EncryptWithoutActiveKey(t *testing.T) {
	cfg := &EncryptionConfig{
		Enabled:     true,
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
	}

	log := createTestLogger(t)
	em := &EncryptionManager{
		config: cfg,
		logger: log,
		keyring: &Keyring{
			config:     cfg,
			logger:     log,
			currentKey: nil, // No active key
		},
	}

	ciphertext, err := em.Encrypt([]byte("test"))
	assert.Error(t, err)
	assert.Nil(t, ciphertext)
	assert.Contains(t, err.Error(), "no active encryption key")
}

func TestKeyring_CloseWithTimer(t *testing.T) {
	cfg := &EncryptionConfig{
		KeyProvider: "local",
		Algorithm:   AlgorithmAES256GCM,
		RotateDays:  90,
	}

	log := createTestLogger(t)
	kr, err := NewKeyring(cfg, log)
	require.NoError(t, err)

	// Close should stop the rotation timer
	kr.Close()

	// Verify timer is stopped (subsequent close shouldn't panic)
	kr.Close()
}
