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

// Package secrets provides internal secret storage for QuicBaaS configuration.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Well-known internal secret keys for common configuration secrets.
// These are logical path identifiers for secret lookups, not actual credentials.
//
// #nosec G101 -- These are constant names for internal secret paths, not hardcoded credentials
const (
	// Cache configuration secrets.
	InternalSecretRedisPassword = "cache/redis/password"
	InternalSecretRedisUsername = "cache/redis/username"
	InternalSecretRedisTLSCert  = "cache/redis/tls/cert"
	InternalSecretRedisTLSKey   = "cache/redis/tls/key"

	// SMTP configuration secrets.
	InternalSecretSMTPPassword = "smtp/password"
	InternalSecretSMTPUsername = "smtp/username"

	// Backup encryption secrets.
	InternalSecretBackupEncKey  = "backup/encryption_key"
	InternalSecretBackupHMACKey = "backup/hmac_key"

	// Database credentials for dynamic secrets.
	InternalSecretDBAdminPassword = "database/admin/password"
	InternalSecretDBRootPassword  = "database/root/password"

	// External service API keys.
	InternalSecretAPIKeyOpenAI = "external/openai/api_key"
	InternalSecretAPIKeyVoyage = "external/voyage/api_key"
	InternalSecretAPIKeyAWS    = "external/aws/secret_key"
	InternalSecretAPIKeyAzure  = "external/azure/client_secret"
	InternalSecretAPIKeyGCP    = "external/gcp/service_account_key"

	// TLS/mTLS certificates and keys.
	InternalSecretTLSServerKey  = "tls/server/key"
	InternalSecretTLSServerCert = "tls/server/cert"
	InternalSecretTLSClientKey  = "tls/client/key"
	InternalSecretTLSClientCert = "tls/client/cert"

	// JWT signing keys.
	InternalSecretJWTPrivateKey = "jwt/private_key"
	InternalSecretJWTPublicKey  = "jwt/public_key"
)

// Internal secrets storage path prefix.
const (
	internalSecretsPrefix   = "_internal/config/secrets/"
	internalSecretAlgorithm = "AES-256-GCM" // #nosec G101 -- false positive - algorithm name, not credential
)

// InternalSecretStore errors.
var (
	// ErrInternalSecretNotFound indicates the internal secret does not exist.
	ErrInternalSecretNotFound = errors.New("secrets: internal secret not found")

	// ErrInternalSecretAlreadyExists indicates the internal secret already exists.
	ErrInternalSecretAlreadyExists = errors.New("secrets: internal secret already exists")

	// ErrInternalSecretInvalidKey indicates the key is invalid or malformed.
	ErrInternalSecretInvalidKey = errors.New("secrets: invalid internal secret key")

	// ErrInternalSecretEmpty indicates the secret value is empty.
	ErrInternalSecretEmpty = errors.New("secrets: internal secret value cannot be empty")

	// ErrInternalSecretEncryptionFailed indicates encryption failed.
	ErrInternalSecretEncryptionFailed = errors.New("secrets: internal secret encryption failed")

	// ErrInternalSecretDecryptionFailed indicates decryption failed.
	ErrInternalSecretDecryptionFailed = errors.New("secrets: internal secret decryption failed")
)

// InternalSecretStore defines the interface for storing QuicBaaS's internal configuration secrets.
// All secrets are encrypted using the global DEK and stored at _internal/config/secrets/{key}.
type InternalSecretStore interface {
	// Store encrypts and stores an internal secret.
	// The value is encrypted using the global DEK with AES-256-GCM.
	Store(ctx context.Context, key string, value []byte) error

	// Get decrypts and retrieves an internal secret.
	// Returns ErrInternalSecretNotFound if the secret doesn't exist.
	Get(ctx context.Context, key string) ([]byte, error)

	// Delete removes an internal secret.
	// Returns ErrInternalSecretNotFound if the secret doesn't exist.
	Delete(ctx context.Context, key string) error

	// List returns all internal secret keys.
	// Returns an empty slice if no secrets exist.
	List(ctx context.Context) ([]string, error)

	// Exists checks if a secret exists.
	Exists(ctx context.Context, key string) (bool, error)

	// Close releases any resources held by the store.
	Close() error
}

// internalSecretEntry represents a stored internal secret with metadata.
type internalSecretEntry struct {
	// Key is the secret key (without path prefix).
	Key string `json:"key"`

	// EncryptedValue is the encrypted secret value.
	EncryptedValue []byte `json:"encrypted_value"`

	// Nonce used for AES-GCM encryption.
	Nonce []byte `json:"nonce"`

	// Tag for AES-GCM authentication.
	Tag []byte `json:"tag"`

	// Algorithm used for encryption.
	Algorithm string `json:"algorithm"`

	// DEKVersion is the version of the DEK used for encryption.
	DEKVersion uint64 `json:"dek_version"`

	// CreatedAt is when the secret was first created.
	CreatedAt time.Time `json:"created_at"`

	// UpdatedAt is when the secret was last updated.
	UpdatedAt time.Time `json:"updated_at"`
}

// internalSecretStoreImpl implements InternalSecretStore.
type internalSecretStoreImpl struct {
	dek    *SecretsDEK
	store  interfaces.KVStore
	logger interfaces.Logger
	mu     sync.RWMutex
}

// InternalSecretStoreOptions configures an InternalSecretStore instance.
type InternalSecretStoreOptions struct {
	// DEK manages the global DEK for encryption (required).
	DEK *SecretsDEK

	// Store is the underlying key-value storage (required).
	Store interfaces.KVStore

	// Logger for operation logging (optional).
	Logger interfaces.Logger
}

// NewInternalSecretStore creates a new InternalSecretStore instance.
func NewInternalSecretStore(opts *InternalSecretStoreOptions) (InternalSecretStore, error) {
	if opts == nil {
		return nil, ErrNilOptions
	}

	if opts.DEK == nil {
		return nil, api.NewNilDEKError("secrets.internalStore.New")
	}

	if opts.Store == nil {
		return nil, ErrNilStore
	}

	s := &internalSecretStoreImpl{
		dek:    opts.DEK,
		store:  opts.Store,
		logger: opts.Logger,
	}

	if s.logger != nil {
		s.logger.Info("Internal Secret Store initialized")
	}

	return s, nil
}

// Store encrypts and stores an internal secret.
func (s *internalSecretStoreImpl) Store(ctx context.Context, key string, value []byte) error {
	// Validate inputs
	if err := s.validateKey(key); err != nil {
		return err
	}

	if len(value) == 0 {
		return ErrInternalSecretEmpty
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Get global DEK
	dekKey, dekVersion, err := s.dek.GetKey(ctx, DEKIDGlobal)
	if err != nil {
		if s.logger != nil {
			s.logger.WithError(err).Error("Failed to get global DEK for internal secret encryption")
		}
		return api.NewSentinelWrapError("secrets.internalStore.Store", dberrors.ErrEncryption, ErrInternalSecretEncryptionFailed, err)
	}

	// Ensure DEK is cleared from memory after use
	defer func() {
		for i := range dekKey {
			dekKey[i] = 0
		}
	}()

	// Encrypt the secret value using AES-256-GCM
	nonce, ciphertext, tag, err := encryptAEAD(dekKey, value, []byte(key), internalSecretAlgorithm)
	if err != nil {
		if s.logger != nil {
			s.logger.WithError(err).WithField("key", key).Error("Failed to encrypt internal secret")
		}
		return api.NewSentinelWrapError("secrets.internalStore.Store", dberrors.ErrEncryption, ErrInternalSecretEncryptionFailed, err)
	}

	// Create entry with metadata
	now := time.Now().UTC()
	entry := &internalSecretEntry{
		Key:            key,
		EncryptedValue: ciphertext,
		Nonce:          nonce,
		Tag:            tag,
		Algorithm:      internalSecretAlgorithm,
		DEKVersion:     dekVersion,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	// Check if secret already exists to preserve CreatedAt
	existingData, err := s.store.Get(ctx, s.buildStorageKey(key))
	if err == nil && existingData != nil {
		var existing internalSecretEntry
		if err := json.Unmarshal(existingData, &existing); err == nil {
			entry.CreatedAt = existing.CreatedAt
		}
	}

	// Marshal entry
	data, err := json.Marshal(entry)
	if err != nil {
		if s.logger != nil {
			s.logger.WithError(err).WithField("key", key).Error("Failed to marshal internal secret entry")
		}
		return api.NewSentinelWrapError("secrets.internalStore.Store", dberrors.ErrEncryption, ErrInternalSecretEncryptionFailed, err)
	}

	// Store encrypted entry
	if err := s.store.Put(ctx, s.buildStorageKey(key), data); err != nil {
		if s.logger != nil {
			s.logger.WithError(err).WithField("key", key).Error("Failed to store internal secret")
		}
		return api.NewSecretStoreOperationError("secrets.internalStore.Store", "failed to store internal secret", err)
	}

	if s.logger != nil {
		s.logger.WithField("key", key).Info("Internal secret stored successfully")
	}

	return nil
}

// Get decrypts and retrieves an internal secret.
func (s *internalSecretStoreImpl) Get(ctx context.Context, key string) ([]byte, error) {
	// Validate key
	if err := s.validateKey(key); err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Load entry from storage
	data, err := s.store.Get(ctx, s.buildStorageKey(key))
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return nil, ErrInternalSecretNotFound
		}
		if s.logger != nil {
			s.logger.WithError(err).WithField("key", key).Error("Failed to load internal secret")
		}
		return nil, api.NewSecretStoreOperationError("secrets.internalStore.Get", "failed to load internal secret", err)
	}

	// Unmarshal entry
	var entry internalSecretEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		if s.logger != nil {
			s.logger.WithError(err).WithField("key", key).Error("Failed to unmarshal internal secret entry")
		}
		return nil, api.NewSentinelWrapError("secrets.internalStore.Get", dberrors.ErrDecryption, ErrInternalSecretDecryptionFailed, err)
	}

	// Get DEK (might be a specific version if rotated)
	var dekKey []byte
	if entry.DEKVersion == 0 {
		// Old format or current version
		dekKey, _, err = s.dek.GetKey(ctx, DEKIDGlobal)
		if err != nil {
			if s.logger != nil {
				s.logger.WithError(err).Error("Failed to get global DEK for internal secret decryption")
			}
			return nil, api.NewSentinelWrapError("secrets.internalStore.Get", dberrors.ErrDecryption, ErrInternalSecretDecryptionFailed, err)
		}
	} else {
		// Specific version
		dekKey, err = s.dek.GetKeyVersion(ctx, DEKIDGlobal, entry.DEKVersion)
		if err != nil {
			if s.logger != nil {
				s.logger.WithError(err).WithField("dek_version", entry.DEKVersion).
					Error("Failed to get specific DEK version for internal secret decryption")
			}
			return nil, api.NewSentinelWrapError("secrets.internalStore.Get", dberrors.ErrDecryption, ErrInternalSecretDecryptionFailed, err)
		}
	}

	// Ensure DEK is cleared from memory after use
	defer func() {
		for i := range dekKey {
			dekKey[i] = 0
		}
	}()

	// Decrypt the secret value
	plaintext, err := decryptAEAD(dekKey, entry.Nonce, entry.EncryptedValue, entry.Tag, []byte(key), entry.Algorithm)
	if err != nil {
		if s.logger != nil {
			s.logger.WithError(err).WithField("key", key).Error("Failed to decrypt internal secret")
		}
		return nil, api.NewSentinelWrapError("secrets.internalStore.Get", dberrors.ErrDecryption, ErrInternalSecretDecryptionFailed, err)
	}

	return plaintext, nil
}

// Delete removes an internal secret.
func (s *internalSecretStoreImpl) Delete(ctx context.Context, key string) error {
	// Validate key
	if err := s.validateKey(key); err != nil {
		return err
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if secret exists
	storageKey := s.buildStorageKey(key)
	existing, err := s.store.Get(ctx, storageKey)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return ErrInternalSecretNotFound
		}
		return api.NewSecretStoreOperationError("secrets.internalStore.Delete", "failed to check internal secret existence", err)
	}

	if existing == nil {
		return ErrInternalSecretNotFound
	}

	// Delete from storage
	if err := s.store.Delete(ctx, storageKey); err != nil {
		if s.logger != nil {
			s.logger.WithError(err).WithField("key", key).Error("Failed to delete internal secret")
		}
		return api.NewSecretStoreOperationError("secrets.internalStore.Delete", "failed to delete internal secret", err)
	}

	if s.logger != nil {
		s.logger.WithField("key", key).Info("Internal secret deleted successfully")
	}

	return nil
}

// List returns all internal secret keys.
func (s *internalSecretStoreImpl) List(ctx context.Context) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// List all keys with the internal secrets prefix
	keys, err := s.store.List(ctx, internalSecretsPrefix)
	if err != nil {
		if s.logger != nil {
			s.logger.WithError(err).Error("Failed to list internal secrets")
		}
		return nil, api.NewSecretStoreOperationError("secrets.internalStore.List", "failed to list internal secrets", err)
	}

	// Extract secret keys from storage keys
	secretKeys := make([]string, 0, len(keys))
	for _, storageKey := range keys {
		key := s.extractSecretKey(storageKey)
		if key != "" {
			secretKeys = append(secretKeys, key)
		}
	}

	return secretKeys, nil
}

// Exists checks if a secret exists.
func (s *internalSecretStoreImpl) Exists(ctx context.Context, key string) (bool, error) {
	// Validate key
	if err := s.validateKey(key); err != nil {
		return false, err
	}

	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if key exists in storage
	data, err := s.store.Get(ctx, s.buildStorageKey(key))
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return false, nil
		}
		return false, api.NewSecretStoreOperationError("secrets.internalStore.Exists", "failed to check internal secret existence", err)
	}

	return data != nil, nil
}

// Close releases any resources held by the store.
func (s *internalSecretStoreImpl) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.logger != nil {
		s.logger.Info("Internal Secret Store closed")
	}

	return nil
}

// validateKey validates that a secret key is valid.
func (s *internalSecretStoreImpl) validateKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return ErrInternalSecretInvalidKey
	}

	// Key should not contain internal path prefix
	if strings.HasPrefix(key, internalSecretsPrefix) {
		return api.NewPathTraversalError("secrets.internalStore.validateKey", ErrInternalSecretInvalidKey, "key should not contain path prefix")
	}

	// Key should not contain path traversal attempts
	if strings.Contains(key, "..") {
		return api.NewPathTraversalError("secrets.internalStore.validateKey", ErrInternalSecretInvalidKey, "key contains path traversal")
	}

	return nil
}

// buildStorageKey builds the full storage key for an internal secret.
func (s *internalSecretStoreImpl) buildStorageKey(key string) string {
	return internalSecretsPrefix + key
}

// extractSecretKey extracts the secret key from a storage key.
func (s *internalSecretStoreImpl) extractSecretKey(storageKey string) string {
	keyStr := storageKey
	if !strings.HasPrefix(keyStr, internalSecretsPrefix) {
		return ""
	}
	return strings.TrimPrefix(keyStr, internalSecretsPrefix)
}
