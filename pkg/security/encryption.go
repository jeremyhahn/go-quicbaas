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
	"crypto/rand"
	"fmt"
	"io"
	"sync"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// EncryptionManager manages encryption at rest for Pebble storage.
// It uses go-xkms's KeychainFacade for unified access to multiple key backends
// including local storage, HashiCorp Vault, AWS KMS, Azure Key Vault, GCP KMS, and more.
type EncryptionManager struct {
	keyring *Keyring
	config  *EncryptionConfig
	logger  *logger.Logger
	mu      sync.RWMutex
}

// EncryptionConfig configures encryption behavior.
type EncryptionConfig struct {
	Enabled     bool   `json:"enabled"`
	Algorithm   string `json:"algorithm"`    // AES-256-GCM, ChaCha20-Poly1305
	KeyFile     string `json:"key_file"`     // Path to key file (for local backend)
	KeyProvider string `json:"key_provider"` // local, vault, awskms, azurekv, gcpkms
	RotateDays  int    `json:"rotate_days"`  // Automatic rotation interval
	KeyID       string `json:"key_id"`       // Key identifier for the backend

	// KeyStorage is an optional external storage backend for encryption keys.
	// When set, this overrides the default in-memory storage for the local provider.
	// Accepts any go-objstore common.Storage implementation (S3, GCS, Azure, local, etc.)
	KeyStorage interface{} `json:"-"` // Not serialized

	// Vault configuration
	VaultAddr  string `json:"vault_addr"`  // HashiCorp Vault address
	VaultToken string `json:"vault_token"` // Vault token (or use VAULT_TOKEN env)

	// AWS KMS configuration
	KMSKeyID  string `json:"kms_key_id"` // AWS KMS key ID
	KMSRegion string `json:"kms_region"` // AWS region

	// Azure Key Vault configuration
	AzureVaultURL string `json:"azure_vault_url"` // Azure Key Vault URL
	AzureKeyName  string `json:"azure_key_name"`  // Azure key name

	// GCP KMS configuration
	GCPProject  string `json:"gcp_project"`  // GCP project ID
	GCPLocation string `json:"gcp_location"` // GCP location
	GCPKeyRing  string `json:"gcp_keyring"`  // GCP key ring name
	GCPKeyName  string `json:"gcp_key_name"` // GCP key name
}

// Keyring manages encryption keys using go-xkms backends.
// It provides a unified interface to multiple key backends with automatic
// rotation support and version management.
type Keyring struct {
	backend       xkmssdk.SymmetricKeyProvider // The go-xkms backend (AES, Vault, KMS, etc.)
	encrypter     xkmssdk.SymmetricEncrypter   // Current encrypter from go-xkms
	keyAttrs      *xkmssdk.KeyAttributes       // Key attributes
	currentKey    *EncryptionKey               // Metadata about current key
	keys          map[byte]*EncryptionKey      // Historical keys for decryption
	rotationTimer *time.Timer
	config        *EncryptionConfig
	logger        *logger.Logger
	mu            sync.RWMutex
}

// EncryptionKey represents an encryption key with metadata.
// The actual key material is managed by go-xkms backends.
type EncryptionKey struct {
	Version   byte      `json:"version"`
	KeyID     string    `json:"key_id"` // Reference to key in backend
	Algorithm string    `json:"algorithm"`
	CreatedAt time.Time `json:"created_at"`
	RotatedAt time.Time `json:"rotated_at,omitempty"`
	Active    bool      `json:"active"`
}

// Algorithm types.
const (
	AlgorithmAES256GCM    = "AES-256-GCM"
	AlgorithmChaCha20Poly = "CHACHA20-POLY1305"
	DefaultAlgorithm      = AlgorithmAES256GCM
	AESKeySize            = 32 // 256 bits
	NonceSize             = 12 // Standard GCM nonce size
)

// NewEncryptionManager creates a new encryption manager using go-xkms.
func NewEncryptionManager(cfg *EncryptionConfig, log *logger.Logger) (*EncryptionManager, error) {
	if cfg == nil {
		return nil, api.NewNilInputError("NewEncryptionManager", "encryption config")
	}

	// Set defaults
	if cfg.Algorithm == "" {
		cfg.Algorithm = DefaultAlgorithm
	}
	if cfg.RotateDays == 0 {
		cfg.RotateDays = 90 // Default 90-day rotation
	}
	if cfg.KeyProvider == "" {
		cfg.KeyProvider = "local"
	}
	if cfg.KeyID == "" {
		cfg.KeyID = "quicbaas-master-key"
	}

	em := &EncryptionManager{
		config: cfg,
		logger: log,
	}

	if !cfg.Enabled {
		log.Info("Encryption at rest is disabled")
		return em, nil
	}

	// Initialize keyring with go-xkms
	keyring, err := NewKeyring(cfg, log)
	if err != nil {
		return nil, api.NewCryptoError("NewEncryptionManager", "initialize keyring", dberrors.ErrEncryption, err)
	}
	em.keyring = keyring

	log.Infof("Encryption manager initialized: algorithm=%s provider=%s", cfg.Algorithm, cfg.KeyProvider)
	return em, nil
}

// Encrypt encrypts plaintext data before storage.
func (em *EncryptionManager) Encrypt(plaintext []byte) ([]byte, error) {
	if !em.config.Enabled {
		return plaintext, nil
	}

	em.mu.RLock()
	defer em.mu.RUnlock()

	// Get current encryption key metadata
	key := em.keyring.GetCurrentKey()
	if key == nil {
		return nil, api.NewNoActiveKeyError("EncryptionManager.Encrypt")
	}

	// Encrypt using go-xkms's SymmetricEncrypter
	encryptedData, err := em.keyring.Encrypt(plaintext)
	if err != nil {
		return nil, api.NewCryptoError("EncryptionManager.Encrypt", "encryption", dberrors.ErrEncryption, err)
	}

	// Format: [version:1][nonce:12][ciphertext+tag]
	result := make([]byte, 1+len(encryptedData.Nonce)+len(encryptedData.Ciphertext)+len(encryptedData.Tag))
	result[0] = key.Version
	offset := 1
	copy(result[offset:], encryptedData.Nonce)
	offset += len(encryptedData.Nonce)
	copy(result[offset:], encryptedData.Ciphertext)
	offset += len(encryptedData.Ciphertext)
	copy(result[offset:], encryptedData.Tag)

	return result, nil
}

// Decrypt decrypts ciphertext after retrieval.
func (em *EncryptionManager) Decrypt(ciphertext []byte) ([]byte, error) {
	if !em.config.Enabled {
		return ciphertext, nil
	}

	if len(ciphertext) < 1+NonceSize+16 { // version + nonce + min tag
		return nil, api.NewCiphertextTooShortError("EncryptionManager.Decrypt", "")
	}

	em.mu.RLock()
	defer em.mu.RUnlock()

	// Extract key version
	keyVersion := ciphertext[0]

	// Verify we have the key for this version
	key := em.keyring.GetKey(keyVersion)
	if key == nil {
		return nil, api.NewKeyVersionNotFoundError("EncryptionManager.Decrypt", keyVersion)
	}

	// Parse encrypted data format: [version:1][nonce:12][ciphertext][tag:16]
	nonce := ciphertext[1 : 1+NonceSize]
	// For GCM, the tag is appended to ciphertext - go-xkms expects them separate
	// Standard GCM tag is 16 bytes
	tagSize := 16
	dataEnd := len(ciphertext) - tagSize
	if dataEnd < 1+NonceSize {
		return nil, api.NewCiphertextTooShortError("EncryptionManager.Decrypt", "too short for tag extraction")
	}
	data := ciphertext[1+NonceSize : dataEnd]
	tag := ciphertext[dataEnd:]

	// Build EncryptedData for go-xkms
	encryptedData := &xkmssdk.EncryptedData{
		Ciphertext: data,
		Nonce:      nonce,
		Tag:        tag,
		Algorithm:  key.Algorithm,
	}

	// Decrypt using go-xkms
	plaintext, err := em.keyring.Decrypt(encryptedData)
	if err != nil {
		return nil, api.NewCryptoError("EncryptionManager.Decrypt", "decryption", dberrors.ErrDecryption, err)
	}

	return plaintext, nil
}

// RotateKeys initiates key rotation.
func (em *EncryptionManager) RotateKeys() error {
	if !em.config.Enabled {
		return nil
	}

	em.mu.Lock()
	defer em.mu.Unlock()

	em.logger.Info("Starting encryption key rotation")

	oldKey := em.keyring.GetCurrentKey()
	newKey, err := em.keyring.RotateKey()
	if err != nil {
		return api.NewCryptoError("EncryptionManager.RotateKeys", "key rotation", dberrors.ErrEncryption, err)
	}

	em.logger.Infof("Key rotation complete: old_version=%d new_version=%d",
		oldKey.Version, newKey.Version)

	return nil
}

// GetKeyVersion returns the current key version.
func (em *EncryptionManager) GetKeyVersion() byte {
	if !em.config.Enabled {
		return 0
	}

	em.mu.RLock()
	defer em.mu.RUnlock()

	key := em.keyring.GetCurrentKey()
	if key == nil {
		return 0
	}
	return key.Version
}

// NewKeyring creates a new keyring using go-xkms's KeychainFacade.
func NewKeyring(cfg *EncryptionConfig, log *logger.Logger) (*Keyring, error) {
	// Set defaults
	if cfg.KeyID == "" {
		cfg.KeyID = "quicbaas-master-key"
	}
	if cfg.Algorithm == "" {
		cfg.Algorithm = DefaultAlgorithm
	}

	kr := &Keyring{
		keys:   make(map[byte]*EncryptionKey),
		config: cfg,
		logger: log,
	}

	// Validate algorithm before initializing
	if !kr.isValidAlgorithm() {
		return nil, api.NewUnsupportedAlgorithmError("NewKeyring", cfg.Algorithm, nil)
	}

	// Initialize go-xkms with the appropriate backend
	if err := kr.initializeXKMS(); err != nil {
		return nil, err
	}

	// Load or generate initial key
	key, err := kr.loadOrGenerateKey()
	if err != nil {
		return nil, err
	}

	kr.keys[key.Version] = key
	kr.currentKey = key

	// Setup automatic rotation
	if cfg.RotateDays > 0 {
		duration := time.Duration(cfg.RotateDays) * 24 * time.Hour
		kr.rotationTimer = time.AfterFunc(duration, func() {
			if err := kr.autoRotate(); err != nil {
				log.WithError(err).Error("Automatic key rotation failed")
			}
		})
	}

	log.Infof("Keyring initialized with go-xkms: version=%d provider=%s", key.Version, cfg.KeyProvider)
	return kr, nil
}

// initializeXKMS sets up go-xkms with the configured backend.
func (kr *Keyring) initializeXKMS() error {
	switch kr.config.KeyProvider {
	case "local":
		// Determine which storage backend to use
		var keyStorage xkmssdk.StorageBackend
		var err error

		if kr.config.KeyStorage != nil {
			// Use external storage backend wrapped via ObjStoreAdapter
			// The KeyStorage field accepts common.Storage from go-objstore
			var ok bool
			keyStorage, ok = kr.config.KeyStorage.(xkmssdk.StorageBackend)
			if !ok {
				// Type assertion failed - fall back to default memory storage
				kr.logger.Warn("KeyStorage type assertion failed, using default memory backend")
				keyStorage, err = xkmssdk.NewMemoryBackend()
				if err != nil {
					return api.NewOperationFailedError("create memory storage", err)
				}
			}
		} else {
			// Use default in-memory storage
			keyStorage, err = xkmssdk.NewMemoryBackend()
			if err != nil {
				return api.NewOperationFailedError("create memory storage", err)
			}
		}

		aesBackend, err := xkmssdk.NewSymmetricBackend(&xkmssdk.SymmetricBackendConfig{
			KeyStorage: keyStorage,
		})
		if err != nil {
			return api.NewCryptoError("Keyring.initializeXKMS", "create AES backend", dberrors.ErrEncryption, err)
		}
		kr.backend = aesBackend

	case "vault":
		return api.NewInitializationRequiredError("Keyring.initializeXKMS", "vault", "InitializeWithVault()")

	case "awskms":
		return api.NewInitializationRequiredError("Keyring.initializeXKMS", "awskms", "InitializeWithAWSKMS()")

	case "azurekv":
		return api.NewInitializationRequiredError("Keyring.initializeXKMS", "azurekv", "InitializeWithAzureKV()")

	case "gcpkms":
		return api.NewInitializationRequiredError("Keyring.initializeXKMS", "gcpkms", "InitializeWithGCPKMS()")

	default:
		return api.NewUnsupportedProviderError("Keyring.initializeXKMS", kr.config.KeyProvider)
	}

	return nil
}

// InitializeWithVault initializes the keyring with HashiCorp Vault backend.
// This should be called before NewEncryptionManager when using Vault.
//
// NOTE: the Vault backend is currently only reachable via the xkms pkg/
// tree; github.com/jeremyhahn/go-xkms/sdk/go does not re-export
// vault.NewBackend or vault.Config. Applications that need the Vault
// backend must import pkg/backend/vault directly until an SDK alias is
// added.
//
// Example:
//
//	import "github.com/jeremyhahn/go-xkms/pkg/backend/vault"
//	vaultBackend, err := vault.NewBackend(&vault.Config{...})
//	kr.backend = vaultBackend
func InitializeWithVault(cfg *EncryptionConfig, log *logger.Logger) error {
	// Note: This requires the application to import the vault backend package
	return api.NewInitializationRequiredError("InitializeWithVault", "vault",
		"import github.com/jeremyhahn/go-xkms/pkg/backend/vault and call its initialization")
}

// InitializeWithAWSKMS initializes the keyring with AWS KMS backend.
// This should be called before NewEncryptionManager when using AWS KMS.
//
// NOTE: the AWS KMS backend is currently only reachable via the xkms
// pkg/ tree; github.com/jeremyhahn/go-xkms/sdk/go does not re-export
// awskms.NewBackend or awskms.Config. Applications that need the AWS
// KMS backend must import pkg/backend/awskms directly until an SDK
// alias is added.
//
// Example:
//
//	import "github.com/jeremyhahn/go-xkms/pkg/backend/awskms"
//	kmsBackend, err := awskms.NewBackend(&awskms.Config{...})
//	kr.backend = kmsBackend
func InitializeWithAWSKMS(cfg *EncryptionConfig, log *logger.Logger) error {
	// Note: This requires the application to import the AWS KMS backend package
	return api.NewInitializationRequiredError("InitializeWithAWSKMS", "awskms",
		"import github.com/jeremyhahn/go-xkms/pkg/backend/awskms and call its initialization")
}

// loadOrGenerateKey loads an existing key or generates a new one using go-xkms.
func (kr *Keyring) loadOrGenerateKey() (*EncryptionKey, error) {
	if kr.backend == nil {
		return nil, api.NewNotInitializedError("Keyring.loadOrGenerateKey", "backend")
	}

	// Build key attributes with required fields for go-xkms
	kr.keyAttrs = &xkmssdk.KeyAttributes{
		CN:                 kr.config.KeyID,
		KeyType:            xkmssdk.KeyTypeEncryption,
		StoreType:          xkmssdk.StoreSoftware, // Use software store for AES keys
		SymmetricAlgorithm: kr.algorithmToSymmetric(),
	}

	// Try to get existing key
	symKey, err := kr.backend.GetSymmetricKey(kr.keyAttrs)
	if err == nil && symKey != nil {
		// Key exists, get encrypter
		encrypter, err := kr.backend.SymmetricEncrypter(kr.keyAttrs)
		if err != nil {
			return nil, api.NewCryptoError("Keyring.loadOrGenerateKey", "get encrypter for existing key", dberrors.ErrEncryption, err)
		}
		kr.encrypter = encrypter

		return &EncryptionKey{
			Version:   1,
			KeyID:     kr.config.KeyID,
			Algorithm: kr.config.Algorithm,
			CreatedAt: time.Now(), // We don't have creation time stored
			Active:    true,
		}, nil
	}

	// Generate new key - we don't need the returned symKey
	_, err = kr.backend.GenerateSymmetricKey(kr.keyAttrs)
	if err != nil {
		return nil, api.NewCryptoError("Keyring.loadOrGenerateKey", "generate symmetric key", dberrors.ErrEncryption, err)
	}

	// Get encrypter for the new key
	encrypter, err := kr.backend.SymmetricEncrypter(kr.keyAttrs)
	if err != nil {
		return nil, api.NewCryptoError("Keyring.loadOrGenerateKey", "get encrypter for new key", dberrors.ErrEncryption, err)
	}
	kr.encrypter = encrypter

	return &EncryptionKey{
		Version:   1,
		KeyID:     kr.config.KeyID,
		Algorithm: kr.config.Algorithm,
		CreatedAt: time.Now(),
		Active:    true,
	}, nil
}

// algorithmToSymmetric converts the config algorithm to go-xkms's SymmetricAlgorithm.
func (kr *Keyring) algorithmToSymmetric() xkmssdk.SymmetricAlgorithm {
	switch kr.config.Algorithm {
	case AlgorithmAES256GCM, "aes256-gcm", "AES256-GCM":
		return xkmssdk.SymmetricAES256GCM
	case "AES-192-GCM", "aes192-gcm":
		return xkmssdk.SymmetricAES192GCM
	case "AES-128-GCM", "aes128-gcm":
		return xkmssdk.SymmetricAES128GCM
	default:
		return xkmssdk.SymmetricAES256GCM
	}
}

// isValidAlgorithm checks if the configured algorithm is supported.
func (kr *Keyring) isValidAlgorithm() bool {
	switch kr.config.Algorithm {
	case AlgorithmAES256GCM, "aes256-gcm", "AES256-GCM",
		"AES-192-GCM", "aes192-gcm",
		"AES-128-GCM", "aes128-gcm", "":
		return true
	default:
		return false
	}
}

// algorithmToKeySize returns the AES key size in bits for the configured algorithm.
// Used for key validation and testing.
//
//nolint:unused // Used in tests for key size validation
func (kr *Keyring) algorithmToKeySize() int {
	switch kr.config.Algorithm {
	case AlgorithmAES256GCM, "aes256-gcm", "AES256-GCM":
		return 256
	case "AES-192-GCM", "aes192-gcm":
		return 192
	case "AES-128-GCM", "aes128-gcm":
		return 128
	default:
		return 256 // Default to AES-256
	}
}

// Encrypt encrypts plaintext using the current key via go-xkms.
func (kr *Keyring) Encrypt(plaintext []byte) (*xkmssdk.EncryptedData, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	if kr.encrypter == nil {
		return nil, api.NewNoEncrypterError("Keyring.Encrypt")
	}

	return kr.encrypter.Encrypt(plaintext, nil)
}

// Decrypt decrypts ciphertext using go-xkms.
func (kr *Keyring) Decrypt(data *xkmssdk.EncryptedData) ([]byte, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	if kr.encrypter == nil {
		return nil, api.NewNoEncrypterError("Keyring.Decrypt")
	}

	return kr.encrypter.Decrypt(data, nil)
}

// GetCurrentKey returns the current active encryption key metadata.
func (kr *Keyring) GetCurrentKey() *EncryptionKey {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return kr.currentKey
}

// GetKey retrieves a key by version.
func (kr *Keyring) GetKey(version byte) *EncryptionKey {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return kr.keys[version]
}

// RotateKey generates a new encryption key and marks the old one inactive.
func (kr *Keyring) RotateKey() (*EncryptionKey, error) {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	if kr.backend == nil {
		return nil, api.NewNotInitializedError("Keyring.RotateKey", "backend")
	}

	// Mark current key as rotated
	if kr.currentKey != nil {
		kr.currentKey.Active = false
		kr.currentKey.RotatedAt = time.Now()
	}

	// Generate new key with incremented version
	newVersion := byte(1)
	if kr.currentKey != nil {
		newVersion = kr.currentKey.Version + 1
	}

	newKeyID := fmt.Sprintf("%s-v%d", kr.config.KeyID, newVersion)

	// Generate new key with required fields for go-xkms
	newAttrs := &xkmssdk.KeyAttributes{
		CN:                 newKeyID,
		KeyType:            xkmssdk.KeyTypeEncryption,
		StoreType:          xkmssdk.StoreSoftware, // Use software store for AES keys
		SymmetricAlgorithm: kr.algorithmToSymmetric(),
	}

	_, err := kr.backend.GenerateSymmetricKey(newAttrs)
	if err != nil {
		return nil, api.NewCryptoError("Keyring.RotateKey", "generate new key", dberrors.ErrEncryption, err)
	}

	// Get encrypter for new key
	encrypter, err := kr.backend.SymmetricEncrypter(newAttrs)
	if err != nil {
		return nil, api.NewCryptoError("Keyring.RotateKey", "get encrypter for new key", dberrors.ErrEncryption, err)
	}

	// Update keyring state
	kr.keyAttrs = newAttrs
	kr.encrypter = encrypter

	newKey := &EncryptionKey{
		Version:   newVersion,
		KeyID:     newKeyID,
		Algorithm: kr.config.Algorithm,
		CreatedAt: time.Now(),
		Active:    true,
	}

	kr.keys[newKey.Version] = newKey
	kr.currentKey = newKey

	return newKey, nil
}

// autoRotate performs automatic key rotation.
func (kr *Keyring) autoRotate() error {
	kr.logger.Info("Automatic key rotation triggered")

	if _, err := kr.RotateKey(); err != nil {
		return err
	}

	// Schedule next rotation
	duration := time.Duration(kr.config.RotateDays) * 24 * time.Hour
	kr.rotationTimer.Reset(duration)

	return nil
}

// Close stops the rotation timer and closes the backend.
func (kr *Keyring) Close() {
	kr.mu.Lock()
	defer kr.mu.Unlock()

	if kr.rotationTimer != nil {
		kr.rotationTimer.Stop()
	}

	// Close the backend if it implements io.Closer
	if closer, ok := kr.backend.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

// EncryptBatch encrypts multiple values efficiently.
func (em *EncryptionManager) EncryptBatch(plaintexts [][]byte) ([][]byte, error) {
	if !em.config.Enabled {
		return plaintexts, nil
	}

	ciphertexts := make([][]byte, len(plaintexts))
	for i, plaintext := range plaintexts {
		ciphertext, err := em.Encrypt(plaintext)
		if err != nil {
			return nil, api.NewBatchEncryptionError("EncryptionManager.EncryptBatch", "encrypt", i, dberrors.ErrEncryption, err)
		}
		ciphertexts[i] = ciphertext
	}

	return ciphertexts, nil
}

// DecryptBatch decrypts multiple values efficiently.
func (em *EncryptionManager) DecryptBatch(ciphertexts [][]byte) ([][]byte, error) {
	if !em.config.Enabled {
		return ciphertexts, nil
	}

	plaintexts := make([][]byte, len(ciphertexts))
	for i, ciphertext := range ciphertexts {
		plaintext, err := em.Decrypt(ciphertext)
		if err != nil {
			return nil, api.NewBatchEncryptionError("EncryptionManager.DecryptBatch", "decrypt", i, dberrors.ErrDecryption, err)
		}
		plaintexts[i] = plaintext
	}

	return plaintexts, nil
}

// GetKeyMetadata returns metadata for all keys (without key material).
func (kr *Keyring) GetKeyMetadata() []*EncryptionKey {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	keys := make([]*EncryptionKey, 0, len(kr.keys))
	for _, key := range kr.keys {
		// Create copy without actual key material (key material is in go-xkms)
		keys = append(keys, &EncryptionKey{
			Version:   key.Version,
			KeyID:     key.KeyID,
			Algorithm: key.Algorithm,
			CreatedAt: key.CreatedAt,
			RotatedAt: key.RotatedAt,
			Active:    key.Active,
		})
	}

	return keys
}

// generateNonce generates a cryptographically secure random nonce.
// Used for nonce generation in encryption operations.
//
//nolint:unused // Used in tests for nonce generation validation
func generateNonce() ([]byte, error) {
	nonce := make([]byte, NonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return nonce, nil
}
