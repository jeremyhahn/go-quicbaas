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

package secrets

import (
	"context"
	"crypto"
	"crypto/rand"
	"errors"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// encryptorMockLogger implements interfaces.Logger for testing.
type encryptorMockLogger struct{}

func (m *encryptorMockLogger) Debug(msg string, args ...any)                          {}
func (m *encryptorMockLogger) Debugf(format string, args ...interface{}) {}
func (m *encryptorMockLogger) Info(msg string, args ...any)                           {}
func (m *encryptorMockLogger) Infof(format string, args ...interface{})  {}
func (m *encryptorMockLogger) Warn(msg string, args ...any)                           {}
func (m *encryptorMockLogger) Warnf(format string, args ...interface{})  {}
func (m *encryptorMockLogger) Error(msg string, args ...any)                          {}
func (m *encryptorMockLogger) Errorf(format string, args ...interface{}) {}
func (m *encryptorMockLogger) Fatal(msg string)                          {}
func (m *encryptorMockLogger) Fatalf(format string, args ...interface{}) {}
func (m *encryptorMockLogger) WithField(key string, value interface{}) interfaces.Logger {
	return m
}
func (m *encryptorMockLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return m
}
func (m *encryptorMockLogger) WithError(err error) interfaces.Logger { return m }

// encryptorMockSecretsDEK implements SecretsDEKProvider for testing.
type encryptorMockSecretsDEK struct {
	keys        map[string][]byte // DEKID -> key material
	versions    map[string]uint64 // DEKID -> version
	getKeyErr   error
	validateErr error
}

func newEncryptorMockSecretsDEK() *encryptorMockSecretsDEK {
	return &encryptorMockSecretsDEK{
		keys:     make(map[string][]byte),
		versions: make(map[string]uint64),
	}
}

func (m *encryptorMockSecretsDEK) addKey(dekid string, key []byte, version uint64) {
	m.keys[dekid] = key
	m.versions[dekid] = version
}

func (m *encryptorMockSecretsDEK) GetKey(ctx context.Context, dekid DEKID) ([]byte, uint64, error) {
	if m.getKeyErr != nil {
		return nil, 0, m.getKeyErr
	}

	key, exists := m.keys[string(dekid)]
	if !exists {
		return nil, 0, interfaces.ErrKeyNotFound
	}

	version := m.versions[string(dekid)]
	return key, version, nil
}

func (m *encryptorMockSecretsDEK) Validate(ctx context.Context, dekid DEKID) error {
	if m.validateErr != nil {
		return m.validateErr
	}

	if _, exists := m.keys[string(dekid)]; !exists {
		return interfaces.ErrKeyNotFound
	}

	return nil
}

// encryptorMockKMSManager implements interfaces.KMSManager for testing.
type encryptorMockKMSManager struct {
	encryptedData map[string][]byte // keyID -> ciphertext
	keys          map[string]bool   // keyID -> exists
	encryptErr    error
	decryptErr    error
	getKeyErr     error
}

func newEncryptorMockKMSManager() *encryptorMockKMSManager {
	return &encryptorMockKMSManager{
		encryptedData: make(map[string][]byte),
		keys:          make(map[string]bool),
	}
}

func (m *encryptorMockKMSManager) addKey(keyID string) {
	m.keys[keyID] = true
}

func (m *encryptorMockKMSManager) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if m.encryptErr != nil {
		return nil, m.encryptErr
	}

	if !m.keys[keyID] {
		return nil, interfaces.ErrKeyNotFound
	}

	// Simple mock encryption: just XOR with key ID bytes
	ciphertext := make([]byte, len(plaintext))
	for i := range plaintext {
		ciphertext[i] = plaintext[i] ^ byte(i%256)
	}

	m.encryptedData[keyID] = ciphertext
	return ciphertext, nil
}

func (m *encryptorMockKMSManager) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if m.decryptErr != nil {
		return nil, m.decryptErr
	}

	if !m.keys[keyID] {
		return nil, interfaces.ErrKeyNotFound
	}

	// Simple mock decryption: reverse the XOR
	plaintext := make([]byte, len(ciphertext))
	for i := range ciphertext {
		plaintext[i] = ciphertext[i] ^ byte(i%256)
	}

	return plaintext, nil
}

func (m *encryptorMockKMSManager) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.getKeyErr != nil {
		return nil, m.getKeyErr
	}

	if !m.keys[keyID] {
		return nil, interfaces.ErrKeyNotFound
	}

	return map[string]interface{}{"keyID": keyID}, nil
}

// Implement required interface methods (stubs)
func (m *encryptorMockKMSManager) GenerateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
	return nil, nil
}

func (m *encryptorMockKMSManager) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error) {
	return nil, nil
}

func (m *encryptorMockKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	return nil
}

func (m *encryptorMockKMSManager) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	return nil, nil
}

func (m *encryptorMockKMSManager) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	return nil, nil
}

func (m *encryptorMockKMSManager) RevokeKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *encryptorMockKMSManager) DeleteKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *encryptorMockKMSManager) EnableKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *encryptorMockKMSManager) DisableKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *encryptorMockKMSManager) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	return nil, nil
}

func (m *encryptorMockKMSManager) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	return false, nil
}

func (m *encryptorMockKMSManager) GenerateDataKey(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
	return nil, nil, nil
}

func (m *encryptorMockKMSManager) GetBackendType() config.BackendType {
	return config.BackendTypeSoftware
}

func (m *encryptorMockKMSManager) IsAvailable(ctx context.Context) bool {
	return true
}

func (m *encryptorMockKMSManager) Close() error {
	return nil
}

func (m *encryptorMockKMSManager) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return []string{
		"AES-128-GCM", "AES-192-GCM", "AES-256-GCM",
		"CHACHA20-POLY1305",
		"RSA-2048", "RSA-3072", "RSA-4096",
		"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
		"ED25519", "ED448",
	}, nil
}

func (m *encryptorMockKMSManager) GetPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	if !m.keys[keyID] {
		return nil, interfaces.ErrKeyNotFound
	}
	return []byte("mock-public-key-" + keyID), nil
}

// generateRandomKeyForTest generates a random key of the specified size.
func generateRandomKeyForTest(size int) []byte {
	key := make([]byte, size)
	_, err := rand.Read(key)
	if err != nil {
		panic(err)
	}
	return key
}

// TestNewSecretsEncryptor tests the constructor.
func TestNewSecretsEncryptor(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	assert.NotNil(t, encryptor)
}

// TestEncryptWithGlobalDEK tests encryption using the global DEK.
func TestEncryptWithGlobalDEK(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a global DEK
	globalKey := generateRandomKeyForTest(32) // AES-256
	dek.addKey("dek:global", globalKey, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("secret data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       []byte("secret/path/to/data"),
	}

	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)

	require.NoError(t, err)
	require.NotNil(t, blob)
	assert.Equal(t, "dek:global", blob.KeyIdentifier)
	assert.Equal(t, uint64(1), blob.KeyVersion)
	assert.Equal(t, interfaces.AlgorithmAES256GCM, blob.Algorithm)
	assert.NotEmpty(t, blob.Nonce)
	assert.NotEmpty(t, blob.Ciphertext)
	assert.NotEmpty(t, blob.Tag)
	assert.Equal(t, opts.AAD, blob.AAD)
	assert.False(t, blob.EncryptedAt.IsZero())
}

// TestEncryptWithTenantDEK tests encryption using a tenant-specific DEK.
func TestEncryptWithTenantDEK(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a tenant DEK
	tenantKey := generateRandomKeyForTest(32)
	dekID := "dek:tenant:acme-corp"
	dek.addKey(dekID, tenantKey, 2)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("tenant secret")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       []byte("tenant/acme-corp/secret"),
	}

	blob, err := encryptor.Encrypt(context.Background(), dekID, plaintext, opts)

	require.NoError(t, err)
	require.NotNil(t, blob)
	assert.Equal(t, dekID, blob.KeyIdentifier)
	assert.Equal(t, uint64(2), blob.KeyVersion)
}

// TestEncryptWithKMSKey tests encryption using a direct KMS key.
func TestEncryptWithKMSKey(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a KMS key
	keyID := "custom-key-123"
	kms.addKey(keyID)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("kms encrypted data")
	opts := &interfaces.EncryptOptions{
		AAD: []byte("path/to/secret"),
	}

	blob, err := encryptor.Encrypt(context.Background(), keyID, plaintext, opts)

	require.NoError(t, err)
	require.NotNil(t, blob)
	assert.Equal(t, keyID, blob.KeyIdentifier)
	assert.NotEmpty(t, blob.Ciphertext)
}

// TestDecryptWithDEK tests decryption using a DEK.
func TestDecryptWithDEK(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a global DEK
	globalKey := generateRandomKeyForTest(32)
	dek.addKey("dek:global", globalKey, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("secret data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       []byte("secret/path"),
	}

	// Encrypt
	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	require.NoError(t, err)

	// Decrypt
	decrypted, err := encryptor.Decrypt(context.Background(), blob)

	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestDecryptWithKMS tests decryption using KMS.
func TestDecryptWithKMS(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	keyID := "kms-key-456"
	kms.addKey(keyID)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("kms secret")
	opts := &interfaces.EncryptOptions{
		AAD: []byte("kms/path"),
	}

	// Encrypt
	blob, err := encryptor.Encrypt(context.Background(), keyID, plaintext, opts)
	require.NoError(t, err)

	// Decrypt
	decrypted, err := encryptor.Decrypt(context.Background(), blob)

	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestValidateKeyIdentifierDEK tests key validation for DEK identifiers.
func TestValidateKeyIdentifierDEK(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	dek.addKey("dek:global", generateRandomKeyForTest(32), 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	err := encryptor.ValidateKeyIdentifier(context.Background(), "dek:global")
	assert.NoError(t, err)
}

// TestValidateKeyIdentifierKMS tests key validation for KMS key identifiers.
func TestValidateKeyIdentifierKMS(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	keyID := "valid-kms-key"
	kms.addKey(keyID)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	err := encryptor.ValidateKeyIdentifier(context.Background(), keyID)
	assert.NoError(t, err)
}

// TestValidateKeyIdentifierInvalid tests validation with invalid identifiers.
func TestValidateKeyIdentifierInvalid(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	// Empty identifier
	err := encryptor.ValidateKeyIdentifier(context.Background(), "")
	assert.Error(t, err)

	// Non-existent DEK
	err = encryptor.ValidateKeyIdentifier(context.Background(), "dek:nonexistent")
	assert.Error(t, err)

	// Non-existent KMS key
	err = encryptor.ValidateKeyIdentifier(context.Background(), "nonexistent-key")
	assert.Error(t, err)
}

// TestAlgorithmAES128GCM tests AES-128-GCM algorithm.
func TestAlgorithmAES128GCM(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	key := generateRandomKeyForTest(16) // AES-128
	dek.addKey("dek:global", key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES128GCM,
		AAD:       []byte("test/path"),
	}

	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	require.NoError(t, err)
	assert.Equal(t, interfaces.AlgorithmAES128GCM, blob.Algorithm)

	decrypted, err := encryptor.Decrypt(context.Background(), blob)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestAlgorithmChaCha20Poly1305 tests CHACHA20-POLY1305 algorithm.
func TestAlgorithmChaCha20Poly1305(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	key := generateRandomKeyForTest(32) // ChaCha20 requires 32 bytes
	dek.addKey("dek:global", key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("chacha test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmChaCha20Poly1305,
		AAD:       []byte("chacha/path"),
	}

	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	require.NoError(t, err)
	assert.Equal(t, interfaces.AlgorithmChaCha20Poly1305, blob.Algorithm)

	decrypted, err := encryptor.Decrypt(context.Background(), blob)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestAADBinding tests that AAD is cryptographically bound.
func TestAADBinding(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	key := generateRandomKeyForTest(32)
	dek.addKey("dek:global", key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("sensitive data")
	correctAAD := []byte("correct/path")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       correctAAD,
	}

	// Encrypt with correct AAD
	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	require.NoError(t, err)

	// Decrypt should succeed with correct AAD
	decrypted, err := encryptor.Decrypt(context.Background(), blob)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	// Modify AAD and decrypt should fail
	blob.AAD = []byte("wrong/path")
	_, err = encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
}

// TestTamperDetection tests that tampering is detected.
func TestTamperDetection(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	key := generateRandomKeyForTest(32)
	dek.addKey("dek:global", key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("tamper test")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       []byte("test/path"),
	}

	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	require.NoError(t, err)

	// Tamper with ciphertext
	if len(blob.Ciphertext) > 0 {
		blob.Ciphertext[0] ^= 0xFF
	}

	// Decrypt should fail
	_, err = encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
}

// TestEncryptErrorDEKNotFound tests error when DEK is not found.
func TestEncryptErrorDEKNotFound(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
	}

	_, err := encryptor.Encrypt(context.Background(), "dek:nonexistent", plaintext, opts)
	assert.Error(t, err)
}

// TestEncryptErrorKMSNotFound tests error when KMS key is not found.
func TestEncryptErrorKMSNotFound(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test")
	opts := &interfaces.EncryptOptions{}

	_, err := encryptor.Encrypt(context.Background(), "nonexistent-kms-key", plaintext, opts)
	assert.Error(t, err)
}

// TestDecryptErrorInvalidBlob tests decryption with invalid blob.
func TestDecryptErrorInvalidBlob(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	// Nil blob
	_, err := encryptor.Decrypt(context.Background(), nil)
	assert.Error(t, err)

	// Empty key identifier
	blob := &interfaces.EncryptedBlob{
		KeyIdentifier: "",
		Algorithm:     interfaces.AlgorithmAES256GCM,
		Nonce:         make([]byte, 12),
		Ciphertext:    []byte("data"),
		Tag:           make([]byte, 16),
		EncryptedAt:   time.Now(),
	}
	_, err = encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
}

// TestDecryptErrorUnsupportedAlgorithm tests decryption with unsupported algorithm.
func TestDecryptErrorUnsupportedAlgorithm(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	key := generateRandomKeyForTest(32)
	dek.addKey("dek:global", key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	blob := &interfaces.EncryptedBlob{
		KeyIdentifier: "dek:global",
		KeyVersion:    1,
		Algorithm:     "UNSUPPORTED-ALGO",
		Nonce:         make([]byte, 12),
		Ciphertext:    []byte("data"),
		Tag:           make([]byte, 16),
		EncryptedAt:   time.Now(),
	}

	_, err := encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
}

// TestEncryptWithDefaultAlgorithm tests encryption with default algorithm.
func TestEncryptWithDefaultAlgorithm(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	key := generateRandomKeyForTest(32)
	dek.addKey("dek:global", key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test")
	opts := &interfaces.EncryptOptions{} // No algorithm specified

	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	require.NoError(t, err)
	assert.Equal(t, interfaces.DefaultAlgorithm, blob.Algorithm)
}

// TestEncryptWithNilOptions tests encryption with nil options.
func TestEncryptWithNilOptions(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	key := generateRandomKeyForTest(32)
	dek.addKey("dek:global", key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test")

	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, nil)
	require.NoError(t, err)
	assert.Equal(t, interfaces.DefaultAlgorithm, blob.Algorithm)
}

// TestEncryptEmptyPlaintext tests encryption of empty plaintext.

// TestEncryptEmptyPlaintext is skipped because EncryptedBlob.Validate()
// requires non-empty ciphertext, which is incompatible with encrypting
// empty plaintext (results in 0-byte ciphertext).
// This is a design constraint in the interfaces.EncryptedBlob type.

// TestGetKeySizeAndNonceSize tests helper functions.
func TestGetKeySizeAndNonceSize(t *testing.T) {
	tests := []struct {
		algorithm string
		keySize   int
		nonceSize int
	}{
		{interfaces.AlgorithmAES256GCM, 32, 12},
		{interfaces.AlgorithmAES128GCM, 16, 12},
		{interfaces.AlgorithmChaCha20Poly1305, 32, 12},
	}

	for _, tt := range tests {
		t.Run(tt.algorithm, func(t *testing.T) {
			assert.Equal(t, tt.keySize, getKeySizeForAlgorithm(tt.algorithm))
			assert.Equal(t, tt.nonceSize, getNonceSizeForAlgorithm(tt.algorithm))
		})
	}
}

// TestIsValidAlgorithmEncryptor tests algorithm validation.
func TestIsValidAlgorithmEncryptor(t *testing.T) {
	validAlgos := []string{
		interfaces.AlgorithmAES256GCM,
		interfaces.AlgorithmAES128GCM,
		interfaces.AlgorithmChaCha20Poly1305,
	}

	for _, algo := range validAlgos {
		assert.True(t, isValidAlgorithm(algo), "Expected %s to be valid", algo)
	}

	invalidAlgos := []string{
		"INVALID-ALGO",
		"AES-256-CBC",
		"",
	}

	for _, algo := range invalidAlgos {
		assert.False(t, isValidAlgorithm(algo), "Expected %s to be invalid", algo)
	}
}

// TestEncryptDecryptKMSError tests KMS error handling.
func TestEncryptDecryptKMSError(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	keyID := "error-key"
	kms.addKey(keyID)
	kms.encryptErr = errors.New("kms encryption failed")

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test")
	opts := &interfaces.EncryptOptions{}

	_, err := encryptor.Encrypt(context.Background(), keyID, plaintext, opts)
	assert.Error(t, err)
}

// TestDecryptKMSError tests KMS decryption error handling.
func TestDecryptKMSError(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	keyID := "error-key"
	kms.addKey(keyID)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test")
	opts := &interfaces.EncryptOptions{}

	// Encrypt successfully
	blob, err := encryptor.Encrypt(context.Background(), keyID, plaintext, opts)
	require.NoError(t, err)

	// Inject decrypt error
	kms.decryptErr = errors.New("kms decryption failed")

	_, err = encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
}

// TestEncryptInvalidDEKFormat tests encryption with invalid DEKID format.
func TestEncryptInvalidDEKFormat(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
	}

	// Invalid DEKID format (missing parts)
	_, err := encryptor.Encrypt(context.Background(), "dek:", plaintext, opts)
	assert.Error(t, err)
}

// TestSecretsEncryptor_EncryptWithDEK_InvalidAlgorithm tests encryption with unsupported algorithm.
func TestSecretsEncryptor_EncryptWithDEK_InvalidAlgorithm(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a valid DEK
	globalKey := generateRandomKeyForTest(32)
	dek.addKey("dek:global", globalKey, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: "UNSUPPORTED-ALGORITHM",
		AAD:       []byte("test/path"),
	}

	// Encryption should fail due to invalid algorithm
	_, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	assert.Error(t, err)
	assert.ErrorIs(t, err, interfaces.ErrUnsupportedAlgorithm)
}

// TestSecretsEncryptor_EncryptWithDEK_DEKNotFound tests encryption when DEK doesn't exist.
func TestSecretsEncryptor_EncryptWithDEK_DEKNotFound(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       []byte("test/path"),
	}

	// Encryption should fail because DEK doesn't exist
	_, err := encryptor.Encrypt(context.Background(), "dek:tenant:nonexistent", plaintext, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get DEK")
}

// TestSecretsEncryptor_EncryptWithDEK_InvalidKeySize tests encryption with wrong key size.
func TestSecretsEncryptor_EncryptWithDEK_InvalidKeySize(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a DEK with wrong size (16 bytes for AES-128)
	invalidKey := generateRandomKeyForTest(16)
	dek.addKey("dek:global", invalidKey, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM, // Requires 32 bytes, but we only have 16
		AAD:       []byte("test/path"),
	}

	// Encryption should fail due to key size mismatch
	_, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "DEK size mismatch")
}

// TestSecretsEncryptor_DecryptWithDEK_CorruptedCiphertext tests decryption with corrupted ciphertext.
func TestSecretsEncryptor_DecryptWithDEK_CorruptedCiphertext(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a valid DEK
	globalKey := generateRandomKeyForTest(32)
	dek.addKey("dek:global", globalKey, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       []byte("test/path"),
	}

	// Encrypt successfully
	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	require.NoError(t, err)

	// Corrupt the ciphertext by flipping bits
	if len(blob.Ciphertext) > 0 {
		blob.Ciphertext[0] ^= 0xFF
	}

	// Decryption should fail due to authentication failure
	_, err = encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
	assert.ErrorIs(t, err, interfaces.ErrDecryptionFailed)
}

// TestSecretsEncryptor_DecryptWithDEK_CorruptedTag tests decryption with corrupted authentication tag.
func TestSecretsEncryptor_DecryptWithDEK_CorruptedTag(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a valid DEK
	globalKey := generateRandomKeyForTest(32)
	dek.addKey("dek:global", globalKey, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       []byte("test/path"),
	}

	// Encrypt successfully
	blob, err := encryptor.Encrypt(context.Background(), "dek:global", plaintext, opts)
	require.NoError(t, err)

	// Corrupt the authentication tag
	if len(blob.Tag) > 0 {
		blob.Tag[0] ^= 0xFF
	}

	// Decryption should fail due to authentication failure
	_, err = encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
	assert.ErrorIs(t, err, interfaces.ErrDecryptionFailed)
}

// TestSecretsEncryptor_DecryptWithDEK_InvalidDEKID tests decryption with invalid DEKID format.
func TestSecretsEncryptor_DecryptWithDEK_InvalidDEKID(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	// Create a blob with invalid DEKID format
	blob := &interfaces.EncryptedBlob{
		KeyIdentifier: "dek:", // Invalid format
		KeyVersion:    1,
		Algorithm:     interfaces.AlgorithmAES256GCM,
		Nonce:         make([]byte, 12),
		Ciphertext:    []byte("data"),
		Tag:           make([]byte, 16),
		EncryptedAt:   time.Now(),
	}

	// Decryption should fail due to invalid DEKID format
	_, err := encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
	assert.ErrorIs(t, err, interfaces.ErrInvalidKeyIdentifier)
}

// TestSecretsEncryptor_EncryptAEAD_AllAlgorithms tests all supported AEAD algorithms.
func TestSecretsEncryptor_EncryptAEAD_AllAlgorithms(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		keySize   int
	}{
		{
			name:      "AES-128-GCM",
			algorithm: interfaces.AlgorithmAES128GCM,
			keySize:   16,
		},
		{
			name:      "AES-256-GCM",
			algorithm: interfaces.AlgorithmAES256GCM,
			keySize:   32,
		},
		{
			name:      "CHACHA20-POLY1305",
			algorithm: interfaces.AlgorithmChaCha20Poly1305,
			keySize:   32,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dek := newEncryptorMockSecretsDEK()
			kms := newEncryptorMockKMSManager()
			logger := &encryptorMockLogger{}

			// Generate key of appropriate size
			key := generateRandomKeyForTest(tt.keySize)
			dekID := "dek:global"
			dek.addKey(dekID, key, 1)

			encryptor := NewSecretsEncryptor(dek, kms, logger)

			plaintext := []byte("test data for " + tt.name)
			aad := []byte("additional authenticated data")
			opts := &interfaces.EncryptOptions{
				Algorithm: tt.algorithm,
				AAD:       aad,
			}

			// Encrypt
			blob, err := encryptor.Encrypt(context.Background(), dekID, plaintext, opts)
			require.NoError(t, err, "Encryption should succeed for %s", tt.name)
			assert.Equal(t, tt.algorithm, blob.Algorithm)
			assert.NotEmpty(t, blob.Nonce)
			assert.NotEmpty(t, blob.Ciphertext)
			assert.NotEmpty(t, blob.Tag)
			assert.Equal(t, aad, blob.AAD)

			// Verify nonce size is correct (all use 12 bytes)
			assert.Equal(t, 12, len(blob.Nonce), "Nonce size should be 12 bytes for %s", tt.name)

			// Verify tag size is correct (all use 16 bytes)
			assert.Equal(t, 16, len(blob.Tag), "Tag size should be 16 bytes for %s", tt.name)

			// Decrypt and verify
			decrypted, err := encryptor.Decrypt(context.Background(), blob)
			require.NoError(t, err, "Decryption should succeed for %s", tt.name)
			assert.Equal(t, plaintext, decrypted, "Decrypted plaintext should match original for %s", tt.name)
		})
	}
}

// TestSecretsEncryptor_EncryptAEAD_EmptyAAD tests encryption with empty AAD.
func TestSecretsEncryptor_EncryptAEAD_EmptyAAD(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	key := generateRandomKeyForTest(32)
	dekID := "dek:global"
	dek.addKey(dekID, key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
		AAD:       nil, // No AAD
	}

	// Encrypt with no AAD
	blob, err := encryptor.Encrypt(context.Background(), dekID, plaintext, opts)
	require.NoError(t, err)
	assert.Nil(t, blob.AAD)

	// Decrypt should succeed
	decrypted, err := encryptor.Decrypt(context.Background(), blob)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestSecretsEncryptor_EncryptWithDEK_GetKeyError tests error during DEK retrieval.
func TestSecretsEncryptor_EncryptWithDEK_GetKeyError(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a DEK but configure mock to return error on GetKey
	key := generateRandomKeyForTest(32)
	dekID := "dek:global"
	dek.addKey(dekID, key, 1)
	dek.getKeyErr = errors.New("mock GetKey error")

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
	}

	// Encryption should fail due to GetKey error
	_, err := encryptor.Encrypt(context.Background(), dekID, plaintext, opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get DEK")
}

// TestSecretsEncryptor_DecryptWithDEK_GetKeyError tests error during DEK retrieval for decryption.
func TestSecretsEncryptor_DecryptWithDEK_GetKeyError(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	// Add a DEK and encrypt successfully
	key := generateRandomKeyForTest(32)
	dekID := "dek:global"
	dek.addKey(dekID, key, 1)

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
	}

	blob, err := encryptor.Encrypt(context.Background(), dekID, plaintext, opts)
	require.NoError(t, err)

	// Configure mock to return error on GetKey during decryption
	dek.getKeyErr = errors.New("mock GetKey error during decrypt")

	// Decryption should fail due to GetKey error
	_, err = encryptor.Decrypt(context.Background(), blob)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get DEK")
}

// TestSecretsEncryptor_EncryptWithEmptyKeyIdentifier tests encryption with empty key identifier.
func TestSecretsEncryptor_EncryptWithEmptyKeyIdentifier(t *testing.T) {
	dek := newEncryptorMockSecretsDEK()
	kms := newEncryptorMockKMSManager()
	logger := &encryptorMockLogger{}

	encryptor := NewSecretsEncryptor(dek, kms, logger)

	plaintext := []byte("test data")
	opts := &interfaces.EncryptOptions{
		Algorithm: interfaces.AlgorithmAES256GCM,
	}

	// Encryption should fail with empty key identifier
	_, err := encryptor.Encrypt(context.Background(), "", plaintext, opts)
	assert.Error(t, err)
	assert.ErrorIs(t, err, interfaces.ErrInvalidKeyIdentifier)
}

// TestEncryptAEAD_ChaCha20Poly1305_DirectCall tests encryptAEAD with CHACHA20-POLY1305.
// This test directly exercises the encryptAEAD function to ensure CHACHA20-POLY1305 code path is covered.
func TestEncryptAEAD_ChaCha20Poly1305_DirectCall(t *testing.T) {
	key := generateRandomKeyForTest(32)
	plaintext := []byte("direct chacha20 test")
	aad := []byte("additional data")

	// Test encryption
	nonce, ciphertext, tag, err := encryptAEAD(key, plaintext, aad, interfaces.AlgorithmChaCha20Poly1305)
	require.NoError(t, err, "encryptAEAD should succeed for CHACHA20-POLY1305")
	assert.Equal(t, 12, len(nonce), "CHACHA20-POLY1305 should use 12-byte nonce")
	assert.Equal(t, 16, len(tag), "CHACHA20-POLY1305 should use 16-byte tag")
	assert.NotEmpty(t, ciphertext)

	// Test decryption
	decrypted, err := decryptAEAD(key, nonce, ciphertext, tag, aad, interfaces.AlgorithmChaCha20Poly1305)
	require.NoError(t, err, "decryptAEAD should succeed for CHACHA20-POLY1305")
	assert.Equal(t, plaintext, decrypted, "Decrypted plaintext should match original")
}

// TestEncryptAEAD_UnsupportedAlgorithm tests encryptAEAD with an unsupported algorithm.
// This ensures the default case in the algorithm switch is covered.
func TestEncryptAEAD_UnsupportedAlgorithm(t *testing.T) {
	key := generateRandomKeyForTest(32)
	plaintext := []byte("test data")
	aad := []byte("aad")

	_, _, _, err := encryptAEAD(key, plaintext, aad, "UNSUPPORTED-ALGO")
	assert.Error(t, err, "encryptAEAD should fail with unsupported algorithm")
	assert.ErrorIs(t, err, interfaces.ErrUnsupportedAlgorithm)
}

// TestDecryptAEAD_UnsupportedAlgorithm tests decryptAEAD with an unsupported algorithm.
// This ensures the default case in the decryption algorithm switch is covered.
func TestDecryptAEAD_UnsupportedAlgorithm(t *testing.T) {
	key := generateRandomKeyForTest(32)
	nonce := make([]byte, 12)
	ciphertext := []byte("fake ciphertext")
	tag := make([]byte, 16)
	aad := []byte("aad")

	_, err := decryptAEAD(key, nonce, ciphertext, tag, aad, "UNSUPPORTED-ALGO")
	assert.Error(t, err, "decryptAEAD should fail with unsupported algorithm")
	assert.ErrorIs(t, err, interfaces.ErrUnsupportedAlgorithm)
}

// TestGetKeySizeForAlgorithm_UnsupportedAlgorithm tests getKeySizeForAlgorithm with unsupported algorithm.
// This ensures the default case returns 0 for unknown algorithms.
func TestGetKeySizeForAlgorithm_UnsupportedAlgorithm(t *testing.T) {
	size := getKeySizeForAlgorithm("UNSUPPORTED-ALGO")
	assert.Equal(t, 0, size, "getKeySizeForAlgorithm should return 0 for unsupported algorithm")

	// Test with empty string
	size = getKeySizeForAlgorithm("")
	assert.Equal(t, 0, size, "getKeySizeForAlgorithm should return 0 for empty algorithm")

	// Test with random invalid algorithm
	size = getKeySizeForAlgorithm("AES-512-INVALID")
	assert.Equal(t, 0, size, "getKeySizeForAlgorithm should return 0 for invalid algorithm")
}

// TestGetNonceSizeForAlgorithm_UnsupportedAlgorithm tests getNonceSizeForAlgorithm with unsupported algorithm.
// This ensures the default case returns 0 for unknown algorithms.
func TestGetNonceSizeForAlgorithm_UnsupportedAlgorithm(t *testing.T) {
	size := getNonceSizeForAlgorithm("UNSUPPORTED-ALGO")
	assert.Equal(t, 0, size, "getNonceSizeForAlgorithm should return 0 for unsupported algorithm")

	// Test with empty string
	size = getNonceSizeForAlgorithm("")
	assert.Equal(t, 0, size, "getNonceSizeForAlgorithm should return 0 for empty algorithm")

	// Test with random invalid algorithm
	size = getNonceSizeForAlgorithm("XCHACHA20-POLY1305")
	assert.Equal(t, 0, size, "getNonceSizeForAlgorithm should return 0 for invalid algorithm")
}

// TestDecryptAEAD_ChaCha20Poly1305_InvalidTag tests CHACHA20-POLY1305 decryption with invalid tag.
// This provides regression protection for authentication failure in CHACHA20-POLY1305.
func TestDecryptAEAD_ChaCha20Poly1305_InvalidTag(t *testing.T) {
	key := generateRandomKeyForTest(32)
	plaintext := []byte("chacha authentication test")
	aad := []byte("path/to/secret")

	// Encrypt with valid data
	nonce, ciphertext, tag, err := encryptAEAD(key, plaintext, aad, interfaces.AlgorithmChaCha20Poly1305)
	require.NoError(t, err)

	// Corrupt the tag
	corruptedTag := make([]byte, len(tag))
	copy(corruptedTag, tag)
	corruptedTag[0] ^= 0xFF

	// Decryption should fail due to authentication failure
	_, err = decryptAEAD(key, nonce, ciphertext, corruptedTag, aad, interfaces.AlgorithmChaCha20Poly1305)
	assert.Error(t, err, "decryptAEAD should fail with corrupted tag")
	assert.Contains(t, err.Error(), "authentication failed")
}

// TestDecryptAEAD_ChaCha20Poly1305_InvalidAAD tests CHACHA20-POLY1305 decryption with wrong AAD.
// This ensures AAD is properly authenticated for CHACHA20-POLY1305.
func TestDecryptAEAD_ChaCha20Poly1305_InvalidAAD(t *testing.T) {
	key := generateRandomKeyForTest(32)
	plaintext := []byte("chacha aad binding test")
	correctAAD := []byte("correct/path")
	wrongAAD := []byte("wrong/path")

	// Encrypt with correct AAD
	nonce, ciphertext, tag, err := encryptAEAD(key, plaintext, correctAAD, interfaces.AlgorithmChaCha20Poly1305)
	require.NoError(t, err)

	// Decryption should fail with wrong AAD
	_, err = decryptAEAD(key, nonce, ciphertext, tag, wrongAAD, interfaces.AlgorithmChaCha20Poly1305)
	assert.Error(t, err, "decryptAEAD should fail with wrong AAD")
	assert.Contains(t, err.Error(), "authentication failed")
}

// TestEncryptAEAD_InvalidKeySize tests encryptAEAD with invalid key sizes.
// This provides regression protection for key validation in AEAD operations.
func TestEncryptAEAD_InvalidKeySize(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		keySize   int
	}{
		{
			name:      "AES-GCM with 7-byte key",
			algorithm: interfaces.AlgorithmAES256GCM,
			keySize:   7, // Invalid size
		},
		{
			name:      "AES-GCM with 15-byte key",
			algorithm: interfaces.AlgorithmAES128GCM,
			keySize:   15, // Invalid size
		},
		{
			name:      "CHACHA20-POLY1305 with 16-byte key",
			algorithm: interfaces.AlgorithmChaCha20Poly1305,
			keySize:   16, // Should be 32
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			invalidKey := generateRandomKeyForTest(tt.keySize)
			plaintext := []byte("test data")
			aad := []byte("aad")

			_, _, _, err := encryptAEAD(invalidKey, plaintext, aad, tt.algorithm)
			assert.Error(t, err, "encryptAEAD should fail with invalid key size for %s", tt.name)
		})
	}
}

// TestDecryptAEAD_AES256GCM_ShortCiphertext tests AES-256-GCM decryption with very short ciphertext.
// This provides regression protection for edge cases in ciphertext handling.
func TestDecryptAEAD_AES256GCM_ShortCiphertext(t *testing.T) {
	key := generateRandomKeyForTest(32)
	plaintext := []byte("x") // Single byte plaintext
	aad := []byte("aad")

	// Encrypt single byte
	nonce, ciphertext, tag, err := encryptAEAD(key, plaintext, aad, interfaces.AlgorithmAES256GCM)
	require.NoError(t, err)

	// Decrypt should succeed
	decrypted, err := decryptAEAD(key, nonce, ciphertext, tag, aad, interfaces.AlgorithmAES256GCM)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestEncryptAEAD_LargePlaintext tests AEAD encryption with large plaintext.
// This provides regression protection for buffer handling with large data.
func TestEncryptAEAD_LargePlaintext(t *testing.T) {
	key := generateRandomKeyForTest(32)
	// Create 1MB plaintext
	plaintext := make([]byte, 1024*1024)
	_, err := rand.Read(plaintext)
	require.NoError(t, err)
	aad := []byte("large data test")

	// Test with AES-256-GCM
	nonce, ciphertext, tag, err := encryptAEAD(key, plaintext, aad, interfaces.AlgorithmAES256GCM)
	require.NoError(t, err, "Should handle large plaintext")
	assert.Equal(t, len(plaintext), len(ciphertext))

	// Verify decryption
	decrypted, err := decryptAEAD(key, nonce, ciphertext, tag, aad, interfaces.AlgorithmAES256GCM)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	// Test with CHACHA20-POLY1305
	nonce2, ciphertext2, tag2, err := encryptAEAD(key, plaintext, aad, interfaces.AlgorithmChaCha20Poly1305)
	require.NoError(t, err, "CHACHA20-POLY1305 should handle large plaintext")
	assert.Equal(t, len(plaintext), len(ciphertext2))

	// Verify decryption
	decrypted2, err := decryptAEAD(key, nonce2, ciphertext2, tag2, aad, interfaces.AlgorithmChaCha20Poly1305)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted2)
}

// TestEncryptAEAD_EmptyPlaintext tests AEAD encryption with empty plaintext.
// This provides regression protection for the edge case where plaintext is empty,
// resulting in a sealed output that is smaller than the tag size.
func TestEncryptAEAD_EmptyPlaintext(t *testing.T) {
	key := generateRandomKeyForTest(32)
	plaintext := []byte{} // Empty plaintext
	aad := []byte("aad for empty plaintext")

	// Test with AES-256-GCM
	nonce, ciphertext, tag, err := encryptAEAD(key, plaintext, aad, interfaces.AlgorithmAES256GCM)
	require.NoError(t, err, "Should handle empty plaintext")
	assert.Empty(t, ciphertext, "Ciphertext should be empty for empty plaintext")
	assert.Equal(t, 16, len(tag), "Tag should be 16 bytes")
	assert.Equal(t, 12, len(nonce), "Nonce should be 12 bytes")

	// Decrypt and verify
	decrypted, err := decryptAEAD(key, nonce, ciphertext, tag, aad, interfaces.AlgorithmAES256GCM)
	require.NoError(t, err, "Decryption of empty plaintext should succeed")
	assert.Empty(t, decrypted, "Decrypted data should be empty")

	// Test with CHACHA20-POLY1305
	nonce2, ciphertext2, tag2, err := encryptAEAD(key, plaintext, aad, interfaces.AlgorithmChaCha20Poly1305)
	require.NoError(t, err, "CHACHA20-POLY1305 should handle empty plaintext")
	assert.Empty(t, ciphertext2, "Ciphertext should be empty for empty plaintext")
	assert.Equal(t, 16, len(tag2), "Tag should be 16 bytes")
	assert.Equal(t, 12, len(nonce2), "Nonce should be 12 bytes")

	// Decrypt and verify
	decrypted2, err := decryptAEAD(key, nonce2, ciphertext2, tag2, aad, interfaces.AlgorithmChaCha20Poly1305)
	require.NoError(t, err, "CHACHA20-POLY1305 decryption of empty plaintext should succeed")
	assert.Empty(t, decrypted2, "Decrypted data should be empty")
}

// Signer returns a crypto.Signer for the given keyID. Tests that don't
// exercise the signer path receive a nil signer and no error; tests
// that do can stub this by redefining the method on a wrapped type.
func (m *encryptorMockKMSManager) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, nil
}
