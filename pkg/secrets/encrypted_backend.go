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
// All cryptographic operations use the SecretsEncryptor interface for unified
// encryption support (DEK-based and direct KMS).
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// secretsKeyPrefix is the storage prefix for all secrets.
const secretsKeyPrefix = "secrets/"

// DefaultAlgorithm is the default encryption algorithm for secrets.
const DefaultAlgorithm = "AES-256-GCM"

// SupportedAlgorithms lists the encryption algorithms supported for secrets.
var SupportedAlgorithms = []string{
	"AES-128-GCM",
	"AES-192-GCM",
	"AES-256-GCM",
	"CHACHA20-POLY1305",
	"XCHACHA20-POLY1305",
}

// EncryptedBackend provides encrypted secret storage using the SecretsEncryptor interface.
// All secret data is encrypted before storage and decrypted on retrieval.
// Uses interfaces.KVStore for underlying storage (memory, PebbleDB, or Raft).
type EncryptedBackend struct {
	// store is the underlying KV storage (memory, pebble, raft, etc.)
	store interfaces.KVStore

	// encryptor handles encryption/decryption operations
	encryptor interfaces.SecretsEncryptor

	// defaultKeyIdentifier is the key identifier used when none is specified (e.g., "dek:global")
	defaultKeyIdentifier string

	// defaultAlgorithm is the encryption algorithm when none is specified
	defaultAlgorithm string

	logger interfaces.Logger
	mu     sync.RWMutex
}

// EncryptedBackendOptions configures the encrypted backend.
type EncryptedBackendOptions struct {
	// Store is the underlying KV storage (required).
	// Can be a memory, PebbleDB, or Raft-backed store.
	Store interfaces.KVStore

	// Encryptor handles encryption/decryption operations (required).
	Encryptor interfaces.SecretsEncryptor

	// DefaultKeyIdentifier is the key identifier to use when none is specified.
	// If empty, "dek:global" will be used.
	// Can be a DEKID (e.g., "dek:global", "dek:tenant:acme-corp") or a KeyID.
	DefaultKeyIdentifier string

	// DefaultAlgorithm is the encryption algorithm when none is specified.
	// If empty, AES-256-GCM will be used.
	DefaultAlgorithm string

	// Logger for operation logging (optional).
	Logger interfaces.Logger
}

// StoreOptions provides flexible key selection for Store operations.
type StoreOptions struct {
	// TenantID specifies a tenant-specific DEK to use.
	// If set, uses "dek:tenant:{TenantID}" as the key identifier.
	TenantID string

	// KeyIdentifier specifies a custom key identifier to use.
	// Can be a DEKID or a KeyID in go-xkms format.
	// Takes precedence over TenantID.
	KeyIdentifier string

	// Algorithm specifies the encryption algorithm to use.
	// If empty, the backend's default algorithm is used.
	Algorithm string
}

// encryptedEntry stores encrypted secret data with metadata.
// This structure is persisted to storage as JSON.
type encryptedEntry struct {
	// KeyIdentifier is either a DEKID (e.g., "dek:global", "dek:tenant:{id}")
	// or a KeyID in go-xkms format for direct KMS key reference.
	KeyIdentifier string `json:"key_identifier"`

	// KeyVersion tracks the version of the key used for encryption.
	KeyVersion uint64 `json:"key_version"`

	// Algorithm is the encryption algorithm used
	Algorithm string `json:"algorithm"`

	// Nonce is the nonce/IV used for encryption
	Nonce []byte `json:"nonce"`

	// Ciphertext is the encrypted data
	Ciphertext []byte `json:"ciphertext"`

	// Tag is the authentication tag (for AEAD modes)
	Tag []byte `json:"tag"`

	// AAD is Additional Authenticated Data bound to the ciphertext
	AAD []byte `json:"aad"`

	// EncryptedAt records when the encryption occurred
	EncryptedAt time.Time `json:"encrypted_at"`

	// Metadata is unencrypted metadata about the secret
	Metadata map[string]string `json:"metadata"`

	// Version is the secret version number
	Version int `json:"version"`

	// Deleted indicates soft delete status
	Deleted bool `json:"deleted"`

	// CreatedAt is when this version was created
	CreatedAt time.Time `json:"created_at"`

	// CreatedBy is the authenticated principal that minted this version
	CreatedBy string `json:"created_by,omitempty"`

	// DeletedAt is when this version was deleted (if applicable)
	DeletedAt *time.Time `json:"deleted_at,omitempty"`

	// Legacy support: KeyID field for backward compatibility
	// If present and KeyIdentifier is empty, use KeyID as KeyIdentifier
	KeyID string `json:"key_id,omitempty"`
}

// secretVersions stores all versions of a secret.
type secretVersions struct {
	Versions []*encryptedEntry `json:"versions"`
	Deleted  bool              `json:"deleted"`
}

// NewEncryptedBackend creates a new encrypted secrets backend.
// The encryptor must be provided and non-nil.
func NewEncryptedBackend(opts *EncryptedBackendOptions) (*EncryptedBackend, error) {
	if opts == nil {
		return nil, ErrNilOptions
	}
	if opts.Store == nil {
		return nil, ErrNilStore
	}
	if opts.Encryptor == nil {
		return nil, ErrNilEncryptor
	}

	// Set defaults
	defaultKeyIdentifier := opts.DefaultKeyIdentifier
	if defaultKeyIdentifier == "" {
		defaultKeyIdentifier = string(DEKIDGlobal)
	}

	defaultAlgorithm := opts.DefaultAlgorithm
	if defaultAlgorithm == "" {
		defaultAlgorithm = DefaultAlgorithm
	}

	// Validate algorithm
	if !isValidAlgorithm(defaultAlgorithm) {
		return nil, ErrUnsupportedAlgorithm
	}

	eb := &EncryptedBackend{
		store:                opts.Store,
		encryptor:            opts.Encryptor,
		defaultKeyIdentifier: defaultKeyIdentifier,
		defaultAlgorithm:     defaultAlgorithm,
		logger:               opts.Logger,
	}

	if eb.logger != nil {
		eb.logger.WithFields(map[string]interface{}{
			"default_key_identifier": defaultKeyIdentifier,
			"default_algorithm":      defaultAlgorithm,
		}).Info("Encrypted secrets backend initialized with SecretsEncryptor")
	}

	return eb, nil
}

// isValidAlgorithm checks if the algorithm is supported.
func isValidAlgorithm(algorithm string) bool {
	for _, a := range SupportedAlgorithms {
		if strings.EqualFold(a, algorithm) {
			return true
		}
	}
	return false
}

// Store saves a secret at the specified path with encryption using the default key identifier.
func (eb *EncryptedBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	return eb.StoreWithOptions(ctx, path, data, metadata, nil)
}

// StoreWithKey saves a secret encrypted with the specified key identifier.
//
// Deprecated: Use StoreWithOptions for more flexibility.
func (eb *EncryptedBackend) StoreWithKey(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, keyIdentifier string) error {
	return eb.StoreWithOptions(ctx, path, data, metadata, &StoreOptions{
		KeyIdentifier: keyIdentifier,
	})
}

// StoreWithAlgorithm saves a secret encrypted with the specified key identifier and algorithm.
//
// Deprecated: Use StoreWithOptions for more flexibility.
func (eb *EncryptedBackend) StoreWithAlgorithm(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, keyIdentifier, algorithm string) error {
	return eb.StoreWithOptions(ctx, path, data, metadata, &StoreOptions{
		KeyIdentifier: keyIdentifier,
		Algorithm:     algorithm,
	})
}

// StoreWithOptions saves a secret encrypted with options for flexible key selection.
// If opts is nil or opts.KeyIdentifier is empty, uses the default key identifier.
// If opts.TenantID is set and KeyIdentifier is empty, uses "dek:tenant:{tenantID}".
func (eb *EncryptedBackend) StoreWithOptions(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *StoreOptions) error {
	_, err := eb.storeVersion(ctx, path, data, metadata, opts)
	return err
}

// StoreVersion saves a secret as Store does and returns the version it
// assigned.
func (eb *EncryptedBackend) StoreVersion(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) (int, error) {
	return eb.storeVersion(ctx, path, data, metadata, nil)
}

// storeVersion encrypts and saves a secret under opts and returns the
// version it assigned.
func (eb *EncryptedBackend) storeVersion(ctx context.Context, path string, data map[string][]byte, metadata map[string]string, opts *StoreOptions) (int, error) {
	if path == "" {
		return 0, ErrInvalidPath
	}
	if len(data) == 0 {
		return 0, ErrEmptyData
	}
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	// Determine key identifier
	keyIdentifier := eb.defaultKeyIdentifier
	algorithm := eb.defaultAlgorithm

	if opts != nil {
		// KeyIdentifier takes precedence
		if opts.KeyIdentifier != "" {
			keyIdentifier = opts.KeyIdentifier
		} else if opts.TenantID != "" {
			// Build tenant-specific DEKID
			keyIdentifier = string(NewTenantDEKID(opts.TenantID))
			if keyIdentifier == "" {
				return 0, ErrInvalidTenantID
			}
		}

		// Use custom algorithm if specified
		if opts.Algorithm != "" {
			algorithm = opts.Algorithm
		}
	}

	// Validate algorithm
	if !isValidAlgorithm(algorithm) {
		return 0, ErrUnsupportedAlgorithm
	}

	// Serialize the secret data for encryption
	plaintext, err := json.Marshal(data)
	if err != nil {
		return 0, ErrSerializationFailed
	}

	// Encrypt using SecretsEncryptor
	encryptOpts := &interfaces.EncryptOptions{
		Algorithm: algorithm,
		AAD:       []byte(path), // Use path as AAD for binding
	}
	blob, err := eb.encryptor.Encrypt(ctx, keyIdentifier, plaintext, encryptOpts)
	if err != nil {
		return 0, &SecretsSentinelCauseError{Sentinel: ErrEncryptionFailed, Cause: err}
	}

	// Load existing versions or create new
	versions, err := eb.loadVersions(ctx, path)
	if err != nil && !errors.Is(err, ErrSecretNotFound) {
		return 0, &SecretsCauseError{Message: "failed to load existing versions", Cause: err}
	}
	if versions == nil {
		versions = &secretVersions{
			Versions: make([]*encryptedEntry, 0, 1),
		}
	}

	// Create new encrypted entry with all fields from blob
	now := time.Now()
	newVersion := len(versions.Versions) + 1
	entry := &encryptedEntry{
		KeyIdentifier: blob.KeyIdentifier,
		KeyVersion:    blob.KeyVersion,
		Algorithm:     blob.Algorithm,
		Nonce:         blob.Nonce,
		Ciphertext:    blob.Ciphertext,
		Tag:           blob.Tag,
		AAD:           blob.AAD,
		EncryptedAt:   blob.EncryptedAt,
		Metadata:      metadata,
		Version:       newVersion,
		Deleted:       false,
		CreatedAt:     now,
		CreatedBy:     auth.PrincipalFromContext(ctx),
	}

	versions.Versions = append(versions.Versions, entry)
	versions.Deleted = false

	// Save to storage
	if err := eb.saveVersions(ctx, path, versions); err != nil {
		return 0, &SecretsCauseError{Message: "failed to save secret", Cause: err}
	}

	if eb.logger != nil {
		eb.logger.WithFields(map[string]interface{}{
			"path":           path,
			"key_identifier": blob.KeyIdentifier,
			"algorithm":      blob.Algorithm,
			"version":        newVersion,
		}).Info("Secret stored with SecretsEncryptor")
	}

	return newVersion, nil
}

// Get retrieves a secret from the specified path.
func (eb *EncryptedBackend) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, ErrInvalidPath
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	versions, err := eb.loadVersions(ctx, path)
	if err != nil {
		return nil, nil, err
	}

	if versions.Deleted || len(versions.Versions) == 0 {
		return nil, nil, ErrSecretDeleted
	}

	// Get latest version
	entry := versions.Versions[len(versions.Versions)-1]
	if entry.Deleted {
		return nil, nil, ErrSecretDeleted
	}

	// Decrypt the data
	data, err := eb.decryptEntry(ctx, entry)
	if err != nil {
		return nil, nil, &SecretsSentinelCauseError{Sentinel: ErrDecryptionFailed, Cause: err}
	}

	// Include version in metadata
	metadata := make(map[string]string)
	for k, v := range entry.Metadata {
		metadata[k] = v
	}
	metadata["version"] = fmt.Sprintf("%d", entry.Version)

	return data, metadata, nil
}

// GetVersion retrieves a specific version of a secret.
func (eb *EncryptedBackend) GetVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, ErrInvalidPath
	}
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	versions, err := eb.loadVersions(ctx, path)
	if err != nil {
		return nil, nil, err
	}

	if version < 1 || version > len(versions.Versions) {
		return nil, nil, ErrVersionNotFound
	}

	entry := versions.Versions[version-1]
	if entry.Deleted {
		return nil, nil, ErrVersionDestroyed
	}

	// Decrypt the data
	data, err := eb.decryptEntry(ctx, entry)
	if err != nil {
		return nil, nil, &SecretsSentinelCauseError{Sentinel: ErrDecryptionFailed, Cause: err}
	}

	// Include version in metadata
	metadata := make(map[string]string)
	for k, v := range entry.Metadata {
		metadata[k] = v
	}
	metadata["version"] = fmt.Sprintf("%d", entry.Version)

	return data, metadata, nil
}

// List returns all secret paths matching the prefix.
func (eb *EncryptedBackend) List(ctx context.Context, prefix string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	allKeys, err := eb.store.List(ctx, secretKey(prefix))
	if err != nil {
		return nil, err
	}

	// Filter out deleted secrets and strip prefix
	var paths []string
	for _, key := range allKeys {
		// Strip our internal prefix
		cleanPath := strings.TrimPrefix(key, "secrets/")

		// Check if deleted
		versions, err := eb.loadVersions(ctx, cleanPath)
		if err != nil {
			continue
		}
		if !versions.Deleted {
			paths = append(paths, cleanPath)
		}
	}

	return paths, nil
}

// Delete removes a secret (soft delete, keeps versions).
func (eb *EncryptedBackend) Delete(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidPath
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	versions, err := eb.loadVersions(ctx, path)
	if err != nil {
		return err
	}

	if versions.Deleted {
		return ErrSecretDeleted
	}

	now := time.Now()
	versions.Deleted = true

	// Mark latest version as deleted
	if len(versions.Versions) > 0 {
		latest := versions.Versions[len(versions.Versions)-1]
		latest.Deleted = true
		latest.DeletedAt = &now
	}

	return eb.saveVersions(ctx, path, versions)
}

// Destroy permanently removes all versions of a secret.
func (eb *EncryptedBackend) Destroy(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidPath
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	return eb.store.Delete(ctx, secretKey(path))
}

// ListVersions returns metadata for all versions of a secret.
func (eb *EncryptedBackend) ListVersions(ctx context.Context, path string) ([]*VersionMetadata, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	versions, err := eb.loadVersions(ctx, path)
	if err != nil {
		return nil, err
	}

	result := make([]*VersionMetadata, len(versions.Versions))
	for i, entry := range versions.Versions {
		result[i] = &VersionMetadata{
			Version:   entry.Version,
			CreatedAt: entry.CreatedAt,
			CreatedBy: entry.CreatedBy,
			Deleted:   entry.Deleted,
			DeletedAt: entry.DeletedAt,
		}
	}

	return result, nil
}

// Rollback restores a previous version as the current version.
func (eb *EncryptedBackend) Rollback(ctx context.Context, path string, version int) error {
	if path == "" {
		return ErrInvalidPath
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}

	eb.mu.Lock()
	defer eb.mu.Unlock()

	versions, err := eb.loadVersions(ctx, path)
	if err != nil {
		return err
	}

	if version < 1 || version > len(versions.Versions) {
		return ErrVersionNotFound
	}

	oldEntry := versions.Versions[version-1]
	if oldEntry.Deleted {
		return ErrVersionDestroyed
	}

	// Create a new version that copies the old entry's encryption fields
	now := time.Now()
	newVersion := len(versions.Versions) + 1
	newEntry := &encryptedEntry{
		KeyIdentifier: oldEntry.KeyIdentifier,
		KeyVersion:    oldEntry.KeyVersion,
		Algorithm:     oldEntry.Algorithm,
		Nonce:         oldEntry.Nonce,
		Ciphertext:    oldEntry.Ciphertext,
		Tag:           oldEntry.Tag,
		AAD:           oldEntry.AAD,
		EncryptedAt:   oldEntry.EncryptedAt, // Keep original encryption timestamp
		Metadata:      oldEntry.Metadata,
		Version:       newVersion,
		Deleted:       false,
		CreatedAt:     now,
		CreatedBy:     auth.PrincipalFromContext(ctx),
	}

	versions.Versions = append(versions.Versions, newEntry)
	versions.Deleted = false

	return eb.saveVersions(ctx, path, versions)
}

// Type returns the backend type identifier.
func (eb *EncryptedBackend) Type() string {
	return "encrypted"
}

// Close is a no-op since KVStore lifecycle is managed externally.
func (eb *EncryptedBackend) Close() error {
	return nil
}

// decryptEntry decrypts an encrypted entry using the SecretsEncryptor.
// Handles both new format (with EncryptedBlob fields) and legacy format (with KeyID).
func (eb *EncryptedBackend) decryptEntry(ctx context.Context, entry *encryptedEntry) (map[string][]byte, error) {
	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Handle backward compatibility: migrate old KeyID field to KeyIdentifier
	keyIdentifier := entry.KeyIdentifier
	if keyIdentifier == "" && entry.KeyID != "" {
		// Legacy format detected
		keyIdentifier = entry.KeyID
		if eb.logger != nil {
			eb.logger.WithFields(map[string]interface{}{
				"legacy_key_id": entry.KeyID,
			}).Warn("Detected legacy KeyID field, migrating to KeyIdentifier")
		}
	}

	if keyIdentifier == "" {
		return nil, ErrMissingKeyIdentifier
	}

	// Build EncryptedBlob structure for decryption
	blob := &interfaces.EncryptedBlob{
		KeyIdentifier: keyIdentifier,
		KeyVersion:    entry.KeyVersion,
		Algorithm:     entry.Algorithm,
		Nonce:         entry.Nonce,
		Ciphertext:    entry.Ciphertext,
		Tag:           entry.Tag,
		AAD:           entry.AAD,
		EncryptedAt:   entry.EncryptedAt,
	}

	// Decrypt using SecretsEncryptor
	plaintext, err := eb.encryptor.Decrypt(ctx, blob)
	if err != nil {
		return nil, &SecretsSentinelCauseError{Sentinel: ErrDecryptionFailed, Cause: err}
	}

	// Deserialize the secret data, WIPING the decrypted serialization buffer
	// either way (review 979): json.Unmarshal copies every value into fresh
	// allocations, so after it returns the plaintext buffer is a dangling
	// reversible copy of the whole record — exactly what the guarded-use
	// wipe upstream cannot reach.
	var data map[string][]byte
	err = unmarshalWiped(plaintext, &data)
	if err != nil {
		return nil, ErrDeserializationFailed
	}

	return data, nil
}

// unmarshalWiped decodes buf into dst and zeroes buf before returning,
// success or failure: the buffer is a this-call plaintext copy, and decoding
// duplicates whatever dst needs, so nothing may keep reading it afterwards.
func unmarshalWiped(buf []byte, dst any) error {
	defer func() {
		for i := range buf {
			buf[i] = 0
		}
	}()
	return json.Unmarshal(buf, dst)
}

// loadVersions loads all versions of a secret from storage.
func (eb *EncryptedBackend) loadVersions(ctx context.Context, path string) (*secretVersions, error) {
	data, err := eb.store.Get(ctx, secretKey(path))
	if err != nil {
		// ABSENCE AND OUTAGE STAY DISTINCT. The KVStore contract reports a
		// missing key as a QBError with ErrNotFound; only that is absence.
		// Every other failure — a Raft election, an I/O error, a timeout —
		// says nothing about whether the secret exists, and reporting it as
		// ErrSecretNotFound made every consumer that treats absence as
		// ordinary (the documented contract) fail open under an outage.
		var qbErr *dberrors.QBError
		if ok := dberrors.As(err, &qbErr); ok && qbErr.Code == dberrors.ErrNotFound {
			return nil, ErrSecretNotFound
		}
		return nil, &SecretsSentinelCauseError{Sentinel: ErrBackendUnavailable, Cause: err}
	}

	var versions secretVersions
	if err := json.Unmarshal(data, &versions); err != nil {
		return nil, ErrDeserializationFailed
	}

	return &versions, nil
}

// saveVersions saves all versions of a secret to storage.
func (eb *EncryptedBackend) saveVersions(ctx context.Context, path string, versions *secretVersions) error {
	data, err := json.Marshal(versions)
	if err != nil {
		return ErrSerializationFailed
	}

	return eb.store.Put(ctx, secretKey(path), data)
}

// secretKey builds the storage key for a secret path.
func secretKey(path string) string {
	return secretsKeyPrefix + path
}

// GetKeyIdentifier returns the key identifier used for a secret.
// This is useful for key rotation to know which key encrypted a secret.
func (eb *EncryptedBackend) GetKeyIdentifier(ctx context.Context, path string) (string, error) {
	if path == "" {
		return "", ErrInvalidPath
	}

	eb.mu.RLock()
	defer eb.mu.RUnlock()

	versions, err := eb.loadVersions(ctx, path)
	if err != nil {
		return "", err
	}

	if len(versions.Versions) == 0 {
		return "", ErrSecretNotFound
	}

	// Return key identifier of latest version
	latest := versions.Versions[len(versions.Versions)-1]
	keyIdentifier := latest.KeyIdentifier

	// Handle legacy format
	if keyIdentifier == "" && latest.KeyID != "" {
		keyIdentifier = latest.KeyID
	}

	return keyIdentifier, nil
}

// GetKeyID returns the key ID used for a secret.
//
// Deprecated: Use GetKeyIdentifier instead.
func (eb *EncryptedBackend) GetKeyID(ctx context.Context, path string) (string, error) {
	return eb.GetKeyIdentifier(ctx, path)
}

// ReencryptWithKey re-encrypts a secret with a different key identifier.
// This creates a new version with the same data but different encryption.
func (eb *EncryptedBackend) ReencryptWithKey(ctx context.Context, path string, newKeyIdentifier string) error {
	if path == "" {
		return ErrInvalidPath
	}
	if newKeyIdentifier == "" {
		return ErrMissingKeyIdentifier
	}

	// Get current data
	data, metadata, err := eb.Get(ctx, path)
	if err != nil {
		return err
	}

	// Store with new key identifier (creates new version)
	return eb.StoreWithOptions(ctx, path, data, metadata, &StoreOptions{
		KeyIdentifier: newKeyIdentifier,
	})
}
