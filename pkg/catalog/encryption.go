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
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"

	xkms "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// Encryption error codes.
const (
	// ErrCodeEncryptionFailed indicates encryption failed.
	ErrCodeEncryptionFailed ErrorCode = "ENCRYPTION_FAILED"

	// ErrCodeDecryptionFailed indicates decryption failed.
	ErrCodeDecryptionFailed ErrorCode = "DECRYPTION_FAILED"

	// ErrCodeKeyNotFound indicates the encryption key was not found.
	ErrCodeKeyNotFound ErrorCode = "KEY_NOT_FOUND"

	// ErrCodeInvalidNonce indicates an invalid nonce was provided.
	ErrCodeInvalidNonce ErrorCode = "INVALID_NONCE"

	// ErrCodeInvalidKeySize indicates an invalid key size was provided.
	ErrCodeInvalidKeySize ErrorCode = "INVALID_KEY_SIZE"

	// ErrCodeInvalidAlgorithm indicates an unsupported algorithm was specified.
	ErrCodeInvalidAlgorithm ErrorCode = "INVALID_ALGORITHM"
)

// Sentinel errors for encryption operations.
var (
	// ErrKMSServiceNil indicates a nil KMS service was provided.
	ErrKMSServiceNil = errors.New("KMS service cannot be nil")

	// ErrConfigResolverNil indicates a nil config resolver was provided.
	ErrConfigResolverNil = errors.New("config resolver cannot be nil")

	// ErrDataEmpty indicates empty data was provided for encryption/decryption.
	ErrDataEmpty = errors.New("data cannot be empty")

	// ErrEncryptionInfoNil indicates nil encryption info was provided for decryption.
	ErrEncryptionInfoNil = errors.New("encryption info cannot be nil for encrypted data")
)

// NewEncryptionError creates a new error for encryption failure.
func NewEncryptionError(operation string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeEncryptionFailed,
		Message: fmt.Sprintf("encryption failed: %s", operation),
		Cause:   cause,
	}
}

// NewDecryptionError creates a new error for decryption failure.
func NewDecryptionError(operation string, cause error) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeDecryptionFailed,
		Message: fmt.Sprintf("decryption failed: %s", operation),
		Cause:   cause,
	}
}

// NewKeyNotFoundError creates a new error for missing encryption key.
func NewKeyNotFoundError(keyID string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeKeyNotFound,
		Message: fmt.Sprintf("encryption key not found: %s", keyID),
	}
}

// NewInvalidNonceError creates a new error for invalid nonce.
func NewInvalidNonceError(expected, actual int) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeInvalidNonce,
		Message: fmt.Sprintf("invalid nonce size: expected %d, got %d", expected, actual),
	}
}

// NewInvalidKeySizeError creates a new error for invalid key size.
func NewInvalidKeySizeError(algorithm string, expected, actual int) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeInvalidKeySize,
		Message: fmt.Sprintf("invalid key size for %s: expected %d, got %d", algorithm, expected, actual),
	}
}

// NewInvalidAlgorithmError creates a new error for unsupported algorithm.
func NewInvalidAlgorithmError(algorithm string) *CatalogError {
	return &CatalogError{
		Code:    ErrCodeInvalidAlgorithm,
		Message: fmt.Sprintf("unsupported encryption algorithm: %s", algorithm),
	}
}

// IsEncryptionError returns true if the error is an encryption-related error.
func IsEncryptionError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		switch ce.Code {
		case ErrCodeEncryptionFailed, ErrCodeDecryptionFailed,
			ErrCodeKeyNotFound, ErrCodeInvalidNonce,
			ErrCodeInvalidKeySize, ErrCodeInvalidAlgorithm:
			return true
		}
	}
	return false
}

// IsDecryptionError returns true if the error is a decryption error.
func IsDecryptionError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeDecryptionFailed
	}
	return false
}

// IsKeyNotFoundError returns true if the error is a key not found error.
func IsKeyNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var ce *CatalogError
	if errors.As(err, &ce) {
		return ce.Code == ErrCodeKeyNotFound
	}
	return false
}

// DEKStatus represents the status of a Data Encryption Key.
type DEKStatus string

const (
	// DEKStatusActive indicates the DEK is active and usable.
	DEKStatusActive DEKStatus = "active"

	// DEKStatusDisabled indicates the DEK is disabled.
	DEKStatusDisabled DEKStatus = "disabled"

	// DEKStatusRotating indicates the DEK is being rotated.
	DEKStatusRotating DEKStatus = "rotating"
)

// DEKMetadata contains metadata about a Data Encryption Key.
type DEKMetadata struct {
	// Path is the unique identifier for the DEK.
	Path string

	// Version is the current version of the DEK.
	Version uint64

	// Status is the current status of the DEK.
	Status DEKStatus

	// IsDefault indicates if this is the default DEK.
	IsDefault bool

	// CreatedAt is when the DEK was created.
	CreatedAt time.Time

	// RotatedAt is when the DEK was last rotated.
	RotatedAt time.Time
}

// GetDEKRequest represents a request to retrieve a DEK.
type GetDEKRequest struct {
	// Path is the DEK path/identifier (the KEK used for wrapping).
	Path string

	// Version optionally specifies a specific version.
	Version uint64

	// WrappedKey is the wrapped (encrypted) DEK to unwrap during decryption.
	// When nil, the provider generates a new DEK and wraps it with the KEK
	// identified by Path (envelope encryption).
	WrappedKey []byte
}

// GetDEKResponse represents the response from a DEK retrieval.
type GetDEKResponse struct {
	// DEK is the DEK metadata.
	DEK *DEKMetadata

	// KeyMaterial is the plaintext key material for encryption/decryption.
	// This is the unwrapped DEK bytes used directly as the AES key.
	KeyMaterial *secure.Buffer

	// WrappedKey is the wrapped (encrypted) DEK for persistent storage.
	// Store this alongside ciphertext so the DEK can be unwrapped later.
	WrappedKey []byte

	// Timestamp is when the request was processed.
	Timestamp time.Time
}

// DEKProvider provides access to Data Encryption Keys.
// This is a minimal interface to avoid import cycles with the service package.
type DEKProvider interface {
	// GetDEK retrieves a data encryption key.
	GetDEK(ctx context.Context, req *GetDEKRequest) (*GetDEKResponse, error)
}

// EncryptionResult contains the result of an encryption operation.
type EncryptionResult struct {
	// Ciphertext is the encrypted data.
	Ciphertext []byte

	// Encryption contains the encryption metadata for storage.
	Encryption *LocationEncryption
}

// EncryptionManager handles object encryption and decryption operations.
type EncryptionManager interface {
	// Encrypt encrypts data using the resolved encryption config.
	Encrypt(ctx context.Context, data []byte, cfg *config.ObjectStoreEncryption) (*EncryptionResult, error)

	// Decrypt decrypts data using location encryption info.
	Decrypt(ctx context.Context, data []byte, enc *LocationEncryption) ([]byte, error)

	// ResolveEncryptionConfig resolves the effective encryption config.
	ResolveEncryptionConfig(tenantID string, shardID uint64, objectOverride *ObjectEncryption) (*config.ObjectStoreEncryption, error)

	// ShouldEncrypt returns whether encryption should be applied.
	ShouldEncrypt(cfg *config.ObjectStoreEncryption) bool

	// Reencrypt re-encrypts data with a new key.
	Reencrypt(ctx context.Context, data []byte, oldEnc *LocationEncryption, newKeyID string) (*EncryptionResult, error)

	// ValidateKey validates that a key exists and is usable.
	ValidateKey(ctx context.Context, keyID string) error
}

// encryptionManager implements EncryptionManager.
type encryptionManager struct {
	dekProvider    DEKProvider
	configResolver config.ObjectStoreConfigResolver
}

// NewEncryptionManager creates a new encryption manager.
func NewEncryptionManager(dek DEKProvider, resolver config.ObjectStoreConfigResolver) (EncryptionManager, error) {
	if dek == nil {
		return nil, ErrKMSServiceNil
	}
	if resolver == nil {
		return nil, ErrConfigResolverNil
	}

	return &encryptionManager{
		dekProvider:    dek,
		configResolver: resolver,
	}, nil
}

// Encrypt encrypts data using AES-256-GCM.
func (m *encryptionManager) Encrypt(ctx context.Context, data []byte, cfg *config.ObjectStoreEncryption) (*EncryptionResult, error) {
	// No encryption requested
	if cfg == nil || !cfg.Enabled {
		return &EncryptionResult{
			Ciphertext: data,
			Encryption: nil,
		}, nil
	}

	if len(data) == 0 {
		return nil, NewEncryptionError("empty data", ErrDataEmpty)
	}

	if cfg.DefaultKey == "" {
		return nil, NewEncryptionError("no key configured", ErrKeyIDEmpty)
	}

	// Get encryption key material from DEK provider
	keyData, err := m.dekProvider.GetDEK(ctx, &GetDEKRequest{Path: cfg.DefaultKey})
	if err != nil {
		return nil, NewEncryptionError("get key from KMS", err)
	}
	if keyData != nil && keyData.KeyMaterial != nil {
		defer keyData.KeyMaterial.Destroy()
	}

	// Determine key material from DEK response
	keyMaterial, err := m.extractKeyMaterial(keyData)
	if err != nil {
		return nil, err
	}

	// Perform encryption based on algorithm
	algorithm := cfg.Algorithm
	if algorithm == "" {
		algorithm = config.DefaultEncryptionAlgo
	}

	var ciphertext []byte
	var nonce []byte
	if err := keyMaterial.WithBytes(func(key []byte) error {
		var encryptErr error
		ciphertext, nonce, encryptErr = m.encryptWithAlgorithm(data, key, algorithm)
		return encryptErr
	}); err != nil {
		return nil, err
	}

	return &EncryptionResult{
		Ciphertext: ciphertext,
		Encryption: &LocationEncryption{
			KeyID:      cfg.DefaultKey,
			Algorithm:  algorithm,
			Nonce:      nonce,
			WrappedKey: keyData.WrappedKey,
		},
	}, nil
}

// Decrypt decrypts data using the stored encryption info.
func (m *encryptionManager) Decrypt(ctx context.Context, data []byte, enc *LocationEncryption) ([]byte, error) {
	// Not encrypted
	if enc == nil {
		return data, nil
	}

	if len(data) == 0 {
		return nil, NewDecryptionError("empty data", ErrDataEmpty)
	}

	if enc.KeyID == "" {
		return nil, NewDecryptionError("no key ID", ErrKeyIDEmpty)
	}

	// Get decryption key material from DEK provider by unwrapping the stored DEK
	keyData, err := m.dekProvider.GetDEK(ctx, &GetDEKRequest{
		Path:       enc.KeyID,
		WrappedKey: enc.WrappedKey,
	})
	if err != nil {
		return nil, NewDecryptionError("get key from KMS", err)
	}
	if keyData != nil && keyData.KeyMaterial != nil {
		defer keyData.KeyMaterial.Destroy()
	}

	// Extract key material
	keyMaterial, err := m.extractKeyMaterial(keyData)
	if err != nil {
		return nil, NewDecryptionError("extract key material", err)
	}

	// Perform decryption
	var plaintext []byte
	if err := keyMaterial.WithBytes(func(key []byte) error {
		var decryptErr error
		plaintext, decryptErr = m.decryptWithAlgorithm(data, key, enc.Algorithm, enc.Nonce)
		return decryptErr
	}); err != nil {
		return nil, err
	}

	return plaintext, nil
}

// ResolveEncryptionConfig determines which encryption config to use.
func (m *encryptionManager) ResolveEncryptionConfig(tenantID string, shardID uint64, objectOverride *ObjectEncryption) (*config.ObjectStoreEncryption, error) {
	// Object override takes precedence
	if objectOverride != nil && objectOverride.Enabled {
		return &config.ObjectStoreEncryption{
			Enabled:    true,
			Enforce:    true,
			DefaultKey: objectOverride.KeyID,
			Algorithm:  objectOverride.Algorithm,
		}, nil
	}

	// Get resolved config from the config resolver
	cfg, err := m.configResolver.ResolveEncryption(tenantID, shardID)
	if err != nil {
		return nil, NewEncryptionError("resolve config", err)
	}

	return cfg, nil
}

// ShouldEncrypt returns whether encryption should be applied.
func (m *encryptionManager) ShouldEncrypt(cfg *config.ObjectStoreEncryption) bool {
	return cfg != nil && cfg.Enabled && cfg.Enforce
}

// Reencrypt re-encrypts data with a new key.
func (m *encryptionManager) Reencrypt(ctx context.Context, data []byte, oldEnc *LocationEncryption, newKeyID string) (*EncryptionResult, error) {
	if len(data) == 0 {
		return nil, NewEncryptionError("reencrypt empty data", ErrDataEmpty)
	}
	if newKeyID == "" {
		return nil, NewEncryptionError("reencrypt no new key", ErrKeyIDEmpty)
	}

	// Decrypt with old key
	plaintext, err := m.Decrypt(ctx, data, oldEnc)
	if err != nil {
		return nil, NewEncryptionError("reencrypt decrypt", err)
	}

	// Determine algorithm (use old algorithm if available, otherwise default)
	algorithm := config.DefaultEncryptionAlgo
	if oldEnc != nil && oldEnc.Algorithm != "" {
		algorithm = oldEnc.Algorithm
	}

	// Encrypt with new key
	newCfg := &config.ObjectStoreEncryption{
		Enabled:    true,
		Enforce:    true,
		DefaultKey: newKeyID,
		Algorithm:  algorithm,
	}

	return m.Encrypt(ctx, plaintext, newCfg)
}

// ValidateKey validates that a key exists and is usable.
func (m *encryptionManager) ValidateKey(ctx context.Context, keyID string) error {
	if keyID == "" {
		return NewKeyNotFoundError("")
	}

	keyData, err := m.dekProvider.GetDEK(ctx, &GetDEKRequest{Path: keyID})
	if err != nil {
		return NewKeyNotFoundError(keyID)
	}
	if keyData != nil && keyData.KeyMaterial != nil {
		keyData.KeyMaterial.Destroy()
	}

	return nil
}

// extractKeyMaterial extracts the key material from a DEK response.
func (m *encryptionManager) extractKeyMaterial(dekResp *GetDEKResponse) (*secure.Buffer, error) {
	if dekResp == nil || dekResp.DEK == nil {
		return nil, NewEncryptionError("DEK response is nil", ErrNilDEKResponse)
	}

	if dekResp.KeyMaterial == nil || dekResp.KeyMaterial.Size() == 0 {
		return nil, NewEncryptionError("empty key material", ErrEmptyKeyMaterial)
	}

	return dekResp.KeyMaterial, nil
}

// encryptWithAlgorithm encrypts data with the specified algorithm.
func (m *encryptionManager) encryptWithAlgorithm(plaintext, key []byte, algorithm string) ([]byte, []byte, error) {
	switch algorithm {
	case "AES-256-GCM", "AES256GCM", "aes-256-gcm":
		return m.encryptAES256GCM(plaintext, key)
	case "AES-128-GCM", "AES128GCM", "aes-128-gcm":
		// For AES-128, use only first 16 bytes of key
		if len(key) > 16 {
			key = key[:16]
		}
		return m.encryptAES128GCM(plaintext, key)
	default:
		// Default to AES-256-GCM
		return m.encryptAES256GCM(plaintext, key)
	}
}

// encryptAES256GCM encrypts data using AES-256-GCM via the xkms aesgcm package.
func (m *encryptionManager) encryptAES256GCM(plaintext, key []byte) ([]byte, []byte, error) {
	if len(key) != xkms.AESGCMKeySize {
		return nil, nil, NewInvalidKeySizeError("AES-256-GCM", xkms.AESGCMKeySize, len(key))
	}

	// xkms AESGCMEncrypt returns [nonce:12][ciphertext+tag]
	sealed, err := xkms.AESGCMEncrypt(key, plaintext)
	if err != nil {
		return nil, nil, NewEncryptionError("AES-256-GCM encrypt", err)
	}

	// Split into nonce and ciphertext for separate storage
	nonce := sealed[:xkms.AESGCMNonceSize]
	ciphertext := sealed[xkms.AESGCMNonceSize:]

	return ciphertext, nonce, nil
}

// encryptAES128GCM encrypts data using AES-128-GCM with raw crypto primitives.
// The xkms aesgcm package only supports AES-256, so AES-128 uses standard library.
func (m *encryptionManager) encryptAES128GCM(plaintext, key []byte) ([]byte, []byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, NewEncryptionError("create cipher", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, NewEncryptionError("create GCM", err)
	}

	// Generate random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, NewEncryptionError("generate nonce", err)
	}

	// Encrypt the data
	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)

	return ciphertext, nonce, nil
}

// decryptWithAlgorithm decrypts data with the specified algorithm.
func (m *encryptionManager) decryptWithAlgorithm(ciphertext, key []byte, algorithm string, nonce []byte) ([]byte, error) {
	switch algorithm {
	case "AES-256-GCM", "AES256GCM", "aes-256-gcm":
		return m.decryptAES256GCM(ciphertext, key, nonce)
	case "AES-128-GCM", "AES128GCM", "aes-128-gcm":
		// For AES-128, use only first 16 bytes of key
		if len(key) > 16 {
			key = key[:16]
		}
		return m.decryptAES128GCM(ciphertext, key, nonce)
	default:
		// Default to AES-256-GCM
		return m.decryptAES256GCM(ciphertext, key, nonce)
	}
}

// decryptAES256GCM decrypts data using AES-256-GCM via the xkms aesgcm package.
func (m *encryptionManager) decryptAES256GCM(ciphertext, key, nonce []byte) ([]byte, error) {
	if len(key) != xkms.AESGCMKeySize {
		return nil, NewInvalidKeySizeError("AES-256-GCM", xkms.AESGCMKeySize, len(key))
	}

	if len(nonce) != xkms.AESGCMNonceSize {
		return nil, NewInvalidNonceError(xkms.AESGCMNonceSize, len(nonce))
	}

	// Reconstruct the wire format expected by xkms AESGCMDecrypt: [nonce][ciphertext+tag]
	sealed := make([]byte, len(nonce)+len(ciphertext))
	copy(sealed, nonce)
	copy(sealed[len(nonce):], ciphertext)

	plaintext, err := xkms.AESGCMDecrypt(key, sealed)
	if err != nil {
		return nil, NewDecryptionError("decrypt", err)
	}

	return plaintext, nil
}

// decryptAES128GCM decrypts data using AES-128-GCM with raw crypto primitives.
// The xkms aesgcm package only supports AES-256, so AES-128 uses standard library.
func (m *encryptionManager) decryptAES128GCM(ciphertext, key, nonce []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, NewDecryptionError("create cipher", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, NewDecryptionError("create GCM", err)
	}

	if len(nonce) != gcm.NonceSize() {
		return nil, NewInvalidNonceError(gcm.NonceSize(), len(nonce))
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, NewDecryptionError("decrypt", err)
	}

	return plaintext, nil
}
