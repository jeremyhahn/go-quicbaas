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

// Package kms provides key management services for QuicBaaS.
package kms

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strings"
	"sync"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// ErrBackendNotReady indicates the backend is not ready for operations.
var ErrBackendNotReady = stderrors.New("backend not ready")

// XKMSBackend adapts the QuicBaaS xkms package to the kms.Backend interface.
// This allows the KMS Manager to use go-xkms for all cryptographic operations.
//
// All key operations are delegated to the go-xkms service which may use:
//   - Software backend (PKCS#8 files)
//   - TPM2 backend (hardware security)
//   - PKCS#11 backend (HSM/smart cards)
//   - Cloud KMS (AWS, Azure, GCP)
//   - HashiCorp Vault
type XKMSBackend struct {
	backendType config.BackendType
	logger      interfaces.Logger
	metadata    map[string]*KeyMetadata // cached metadata
	mu          sync.RWMutex
}

// XKMSBackendConfig configures the XKMSBackend.
type XKMSBackendConfig struct {
	// BackendType is the type of go-xkms backend to use.
	// Use config.BackendType constants (e.g., config.BackendTypeSoftware).
	BackendType config.BackendType

	// Logger for operation logging.
	Logger interfaces.Logger
}

// NewXKMSBackend creates a new backend that wraps go-xkms.
// Requires xkms.Initialize() to be called first.
func NewXKMSBackend(cfg *XKMSBackendConfig) (*XKMSBackend, error) {
	if cfg == nil {
		return nil, ErrConfigNil
	}
	if !xkms.IsInitialized() {
		return nil, xkms.ErrNotInitialized
	}

	backendType := cfg.BackendType
	if backendType == "" {
		backendType = config.BackendTypeSoftware
	}

	return &XKMSBackend{
		backendType: backendType,
		logger:      cfg.Logger,
		metadata:    make(map[string]*KeyMetadata),
	}, nil
}

// Type returns the backend type identifier.
func (kb *XKMSBackend) Type() config.BackendType {
	return kb.backendType
}

// IsAvailable checks if the backend is available for operations.
func (kb *XKMSBackend) IsAvailable(ctx context.Context) bool {
	return xkms.IsInitialized()
}

// SupportedAlgorithms returns the list of cryptographic algorithms supported by this backend.
// This queries the underlying go-xkms backend's Capabilities to determine what's supported.
func (kb *XKMSBackend) SupportedAlgorithms(ctx context.Context) []string {
	// Get capabilities from go-xkms
	caps, err := xkms.GetBackendCapabilities(string(kb.backendType))
	if err != nil {
		// If we can't get capabilities, use defaults based on backend type
		return kb.defaultAlgorithms()
	}

	var algorithms []string
	isHardware := caps.IsHardwareBacked()
	supportsSymmetric := caps.SupportsSymmetricEncryption()
	supportsSign := caps.SupportsSign()

	// If capabilities report no support for basic operations, use defaults
	// This handles cases where go-xkms doesn't recognize the backend type
	if !supportsSymmetric && !supportsSign {
		return kb.defaultAlgorithms()
	}

	// Add symmetric algorithms if supported
	if supportsSymmetric {
		algorithms = append(algorithms, AlgAES128GCM, AlgAES192GCM, AlgAES256GCM)
		// ChaCha20-Poly1305 and XChaCha20-Poly1305 are only available in software backend (not hardware-backed)
		if !isHardware {
			algorithms = append(algorithms, AlgChaCha20Poly1305, AlgXChaCha20Poly1305)
		}
	}

	// Add signing algorithms if supported
	if supportsSign {
		algorithms = append(algorithms,
			AlgRSA2048, AlgRSA3072, AlgRSA4096,
			AlgECDSAP256, AlgECDSAP384, AlgECDSAP521,
			AlgEd25519,
			AlgHMACSHA256, AlgHMACSHA384, AlgHMACSHA512,
		)
		// Ed448, X25519, X448 only available in software backend (not hardware-backed)
		if !isHardware {
			algorithms = append(algorithms, AlgEd448, AlgX25519, AlgX448)
		}
	}

	return algorithms
}

// defaultAlgorithms returns the default algorithms for this backend type.
// Used as a fallback when capabilities cannot be determined.
func (kb *XKMSBackend) defaultAlgorithms() []string {
	// Software backends support all algorithms
	if kb.backendType == config.BackendTypeSoftware {
		return []string{
			// Symmetric
			AlgAES128GCM, AlgAES192GCM, AlgAES256GCM,
			AlgChaCha20Poly1305, AlgXChaCha20Poly1305,
			// Asymmetric
			AlgRSA2048, AlgRSA3072, AlgRSA4096,
			AlgECDSAP256, AlgECDSAP384, AlgECDSAP521,
			AlgEd25519, AlgEd448,
			// Key Exchange
			AlgX25519, AlgX448,
			// HMAC
			AlgHMACSHA256, AlgHMACSHA384, AlgHMACSHA512,
		}
	}

	// Hardware backends support a subset of algorithms
	return []string{
		AlgAES128GCM, AlgAES192GCM, AlgAES256GCM,
		AlgRSA2048, AlgRSA3072, AlgRSA4096,
		AlgECDSAP256, AlgECDSAP384, AlgECDSAP521,
		AlgEd25519,
		AlgHMACSHA256, AlgHMACSHA384, AlgHMACSHA512,
	}
}

// CreateKey creates a new cryptographic key.
func (kb *XKMSBackend) CreateKey(ctx context.Context, attrs *kctypes.KeyAttributes) (*KeyMetadata, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	if attrs == nil {
		return nil, ErrKeyAttributesNil
	}

	if attrs.CN == "" {
		return nil, ErrKeyCNEmpty
	}

	// Ensure StoreType matches backend
	if attrs.StoreType == "" {
		attrs.StoreType = kctypes.StoreType(string(kb.backendType))
	}

	// Determine if symmetric or asymmetric based on attrs
	isSymmetric := attrs.SymmetricAlgorithm != ""

	// Generate key using go-xkms
	if isSymmetric {
		// Generate symmetric key
		_, err := xkms.GenerateSymmetricKey(ctx, string(kb.backendType), attrs)
		if err != nil {
			return nil, api.NewSymmetricKeyGenerationError("XKMSBackend.CreateKey", err)
		}
	} else {
		// Generate asymmetric key
		_, err := xkms.GenerateKeyWithBackend(ctx, string(kb.backendType), attrs)
		if err != nil {
			return nil, api.NewAsymmetricKeyGenerationError("XKMSBackend.CreateKey", err)
		}
	}

	// Build metadata
	now := time.Now()
	algorithm := kb.attrsToAlgorithmString(attrs)
	meta := &KeyMetadata{
		KeyID:      attrs.CN,
		Name:       attrs.CN,
		Algorithm:  algorithm,
		State:      KeyStateEnabled,
		Version:    1,
		Exportable: attrs.Exportable,
		Created:    now,
		Updated:    now,
	}

	// Cache metadata
	kb.mu.Lock()
	kb.metadata[attrs.CN] = meta
	kb.mu.Unlock()

	return meta, nil
}

// ImportKey imports external key material.
func (kb *XKMSBackend) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string, options map[string]interface{}) (*KeyMetadata, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	if keyID == "" {
		return nil, ErrKeyIDEmpty
	}

	if len(material) == 0 {
		return nil, ErrKeyMaterialEmpty
	}

	// Build key attributes for the imported key
	attrs := kb.algorithmToKeyAttributes(keyID, algorithm)

	// For symmetric keys, use the xkms import functionality
	if kb.isSymmetricAlgorithm(algorithm) {
		// Import the symmetric key material
		err := xkms.ImportSymmetricKey(ctx, string(kb.backendType), attrs, material)
		if err != nil {
			return nil, api.NewKeyImportError("XKMSBackend.ImportKey", "symmetric", err)
		}
	} else {
		// For asymmetric keys, we'd need to parse and import the key material
		// This is more complex and depends on the key format (PEM, DER, etc.)
		return nil, ErrAsymmetricImportUnsupported
	}

	// Build metadata
	now := time.Now()
	meta := &KeyMetadata{
		KeyID:     keyID,
		Algorithm: algorithm,
		State:     KeyStateEnabled,
		Version:   1,
		Created:   now,
		Updated:   now,
	}

	// Extract options if provided
	if options != nil {
		if desc, ok := options["description"].(string); ok {
			meta.Description = desc
		}
		if exp, ok := options["exportable"].(bool); ok {
			meta.Exportable = exp
		}
	}

	// Cache metadata
	kb.mu.Lock()
	kb.metadata[keyID] = meta
	kb.mu.Unlock()

	return meta, nil
}

// GetKeyMetadata retrieves metadata for a key.
func (kb *XKMSBackend) GetKeyMetadata(ctx context.Context, keyID string) (*KeyMetadata, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	// Check cache first
	kb.mu.RLock()
	if meta, ok := kb.metadata[keyID]; ok {
		kb.mu.RUnlock()
		return meta, nil
	}
	kb.mu.RUnlock()

	// Try to verify key exists in xkms using multiple lookup strategies
	// Since we don't know the algorithm, try common signing key formats first
	var foundAlgorithm string

	// Try signing key formats: RSA, ECDSA (P-256, P-384, P-521), Ed25519
	signingFormats := []struct {
		algo   string
		format string
	}{
		{"RSA-4096", "rsa"},
		{"RSA-2048", "rsa"},
		{"ECDSA-P256", "ecdsa-p256"},
		{"ECDSA-P384", "ecdsa-p384"},
		{"ECDSA-P521", "ecdsa-p521"},
		{"ED25519", "ed25519"},
	}

	for _, sf := range signingFormats {
		kid := fmt.Sprintf("%s:signing:%s:%s", kb.backendType, sf.format, keyID)
		if _, err := xkms.SignerByID(ctx, kid); err == nil {
			foundAlgorithm = sf.algo
			break
		}
	}

	// If not found as signing key, try symmetric key formats
	if foundAlgorithm == "" {
		symmetricFormats := []struct {
			algo   string
			format string
		}{
			{"AES-256-GCM", "aes256-gcm"},
			{"AES-128-GCM", "aes128-gcm"},
			{"CHACHA20-POLY1305", "chacha20-poly1305"},
		}

		for _, sf := range symmetricFormats {
			kid := fmt.Sprintf("%s:encryption:%s:%s", kb.backendType, sf.format, keyID)
			if _, err := xkms.GetSymmetricKey(ctx, kid); err == nil {
				foundAlgorithm = sf.algo
				break
			}
		}
	}

	// If still not found, try legacy lookup methods
	if foundAlgorithm == "" {
		attrs := &kctypes.KeyAttributes{
			CN:        keyID,
			StoreType: kctypes.StoreType(string(kb.backendType)),
		}

		// Try to get signer with just CN
		if _, err := xkms.Signer(ctx, attrs); err != nil {
			// Try symmetric key by ID
			if _, err := xkms.GetSymmetricKey(ctx, keyID); err != nil {
				return nil, ErrKeyNotFound
			}
			foundAlgorithm = "AES-256-GCM" // Default for symmetric
		} else {
			foundAlgorithm = "RSA-4096" // Default for signing
		}
	}

	// Key exists but we don't have metadata - create minimal metadata
	meta := &KeyMetadata{
		KeyID:     keyID,
		Algorithm: foundAlgorithm,
		State:     KeyStateEnabled,
		Version:   1,
		Created:   time.Now(),
		Updated:   time.Now(),
	}

	kb.mu.Lock()
	kb.metadata[keyID] = meta
	kb.mu.Unlock()

	return meta, nil
}

// ListKeys lists keys matching the prefix.
func (kb *XKMSBackend) ListKeys(ctx context.Context, prefix string, limit int) ([]*KeyMetadata, string, error) {
	if !kb.IsAvailable(ctx) {
		return nil, "", ErrBackendNotReady
	}

	// Track seen keys to avoid duplicates
	seen := make(map[string]bool)
	var result []*KeyMetadata

	// Get keys from go-xkms
	keys, err := xkms.ListKeys(ctx, string(kb.backendType))
	if err != nil {
		// Log but don't fail - we'll try the cache
		kb.logger.WithField("error", err.Error()).Warn("failed to list keys from xkms, using cache")
	}

	// Add keys from xkms
	for _, attrs := range keys {
		// Filter by prefix
		if prefix != "" && !strings.HasPrefix(attrs.CN, prefix) {
			continue
		}

		// Check cache for metadata
		kb.mu.RLock()
		meta, ok := kb.metadata[attrs.CN]
		kb.mu.RUnlock()

		if !ok {
			// Create minimal metadata
			meta = &KeyMetadata{
				KeyID:   attrs.CN,
				State:   KeyStateEnabled,
				Version: 1,
			}
		}

		result = append(result, meta)
		seen[attrs.CN] = true

		// Apply limit
		if limit > 0 && len(result) >= limit {
			return result, "", nil
		}
	}

	// Also add keys from in-memory cache that weren't in xkms list
	// This handles cases where xkms.ListKeys doesn't return all keys
	kb.mu.RLock()
	for keyID, meta := range kb.metadata {
		if seen[keyID] {
			continue
		}
		// Filter by prefix
		if prefix != "" && !strings.HasPrefix(keyID, prefix) {
			continue
		}
		result = append(result, meta)
		// Apply limit
		if limit > 0 && len(result) >= limit {
			kb.mu.RUnlock()
			return result, "", nil
		}
	}
	kb.mu.RUnlock()

	// No pagination cursor for now
	return result, "", nil
}

// RotateKey rotates a key to a new version.
// Unlike go-xkms's RotateKey (which destroys the old key), this implementation
// creates versioned keys (e.g., "my-key:v2") while preserving old versions for decryption.
func (kb *XKMSBackend) RotateKey(ctx context.Context, keyID string) (*KeyMetadata, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	// Get key metadata to retrieve the algorithm (with proper locking)
	kb.mu.RLock()
	meta, exists := kb.metadata[keyID]
	kb.mu.RUnlock()

	if !exists {
		return nil, ErrKeyNotFound
	}

	// Calculate new version
	oldVersion := meta.Version
	newVersion := oldVersion + 1

	// Build attributes for the new versioned key
	versionedKeyID := kb.buildVersionedKeyID(keyID, newVersion)
	attrs := kb.algorithmToKeyAttributes(versionedKeyID, meta.Algorithm)

	// Generate new key with versioned ID
	// Note: We don't use go-xkms's RotateKey because it destroys the old key
	if kb.isSymmetricAlgorithm(meta.Algorithm) {
		_, err := xkms.GenerateSymmetricKey(ctx, string(kb.backendType), attrs)
		if err != nil {
			return nil, api.NewKeyVersionGenerationError("XKMSBackend.RotateKey", err)
		}
	} else {
		_, err := xkms.GenerateKeyWithBackend(ctx, string(kb.backendType), attrs)
		if err != nil {
			return nil, api.NewKeyVersionGenerationError("XKMSBackend.RotateKey", err)
		}
	}

	// Update cached metadata
	kb.mu.Lock()
	defer kb.mu.Unlock()

	now := time.Now()
	// Re-fetch metadata in case it was updated between RUnlock and Lock
	meta, exists = kb.metadata[keyID]
	if !exists {
		meta = &KeyMetadata{
			KeyID:   keyID,
			State:   KeyStateEnabled,
			Version: 1,
			Created: now,
		}
	}

	meta.Version = newVersion
	meta.Updated = now

	kb.metadata[keyID] = meta

	return meta, nil
}

// buildVersionedKeyID creates a versioned key identifier.
// Format: "keyID_vN" where N is the version number.
// Note: We use "_" as separator because go-xkms validation only allows
// alphanumeric characters plus - and _ in key names.
func (kb *XKMSBackend) buildVersionedKeyID(keyID string, version int) string {
	return fmt.Sprintf("%s_v%d", keyID, version)
}

// DisableKey temporarily disables a key.
func (kb *XKMSBackend) DisableKey(ctx context.Context, keyID string) error {
	if !kb.IsAvailable(ctx) {
		return ErrBackendNotReady
	}

	// Update cached metadata - go-xkms doesn't have native disable
	kb.mu.Lock()
	defer kb.mu.Unlock()

	meta, ok := kb.metadata[keyID]
	if !ok {
		return ErrKeyNotFound
	}

	meta.State = KeyStateDisabled
	meta.Updated = time.Now()

	return nil
}

// EnableKey enables a disabled key.
func (kb *XKMSBackend) EnableKey(ctx context.Context, keyID string) error {
	if !kb.IsAvailable(ctx) {
		return ErrBackendNotReady
	}

	// Update cached metadata - go-xkms doesn't have native enable
	kb.mu.Lock()
	defer kb.mu.Unlock()

	meta, ok := kb.metadata[keyID]
	if !ok {
		return ErrKeyNotFound
	}

	meta.State = KeyStateEnabled
	meta.Updated = time.Now()

	return nil
}

// DeleteKey deletes a key.
func (kb *XKMSBackend) DeleteKey(ctx context.Context, keyID string) error {
	if !kb.IsAvailable(ctx) {
		return ErrBackendNotReady
	}

	// First check if key exists in our metadata cache
	kb.mu.RLock()
	meta, exists := kb.metadata[keyID]
	kb.mu.RUnlock()

	if !exists {
		// Key not in cache - try to see if it exists via xkms
		// by attempting to get metadata (which tries to access the key)
		var err error
		meta, err = kb.GetKeyMetadata(ctx, keyID)
		if err != nil {
			return ErrKeyNotFound
		}
	}

	// Build key attributes for deletion
	// Use DeleteKey(attrs) instead of DeleteKeyByID(kid) to avoid
	// key ID format validation issues with algorithms like ChaCha20-Poly1305
	var attrs *kctypes.KeyAttributes
	if meta != nil && meta.Algorithm != "" {
		attrs = kb.algorithmToKeyAttributes(keyID, meta.Algorithm)
	} else {
		// Default to AES-256-GCM if we don't have algorithm info
		attrs = &kctypes.KeyAttributes{
			CN:                 keyID,
			StoreType:          kctypes.StoreType(string(kb.backendType)),
			KeyType:            kctypes.KeyTypeEncryption,
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		}
	}

	// Delete from go-xkms using attributes
	if err := xkms.DeleteKey(ctx, attrs); err != nil {
		if stderrors.Is(err, xkms.ErrKeyNotFound) {
			return ErrKeyNotFound
		}
		// Try with default format as fallback
		defaultAttrs := &kctypes.KeyAttributes{
			CN:                 keyID,
			StoreType:          kctypes.StoreType(string(kb.backendType)),
			KeyType:            kctypes.KeyTypeEncryption,
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		}
		if err2 := xkms.DeleteKey(ctx, defaultAttrs); err2 != nil {
			if stderrors.Is(err2, xkms.ErrKeyNotFound) {
				return ErrKeyNotFound
			}
			return api.NewKeyDeletionError("XKMSBackend.DeleteKey", err)
		}
	}

	// Remove from cache
	kb.mu.Lock()
	delete(kb.metadata, keyID)
	kb.mu.Unlock()

	return nil
}

// formatKeyID returns the key ID as stored.
// Keys are stored using attrs.CN during generation, so we should use the same ID for lookup.
func (kb *XKMSBackend) formatKeyID(keyID string) string {
	// Use keyID directly - it matches the attrs.CN used during key generation.
	// Do NOT add any prefixes as that would cause key lookup to fail.
	return keyID
}

// buildFullKeyID constructs the full key ID format expected by go-xkms for crypto operations.
// Format: "backend:type:algo:keyname" (e.g., "software:encryption:aes256-gcm:my-key").
func (kb *XKMSBackend) buildFullKeyID(keyID string, algorithm string) string {
	// Map algorithm to go-xkms format
	algoFormat := kb.algorithmToKeyIDFormat(algorithm)
	return fmt.Sprintf("%s:encryption:%s:%s", kb.backendType, algoFormat, keyID)
}

// buildSigningKeyID constructs the full key ID format for signing operations.
// Format: "backend:signing:algo:keyname" (e.g., "software:signing:rsa:my-key").
func (kb *XKMSBackend) buildSigningKeyID(keyID string, algorithm string) string {
	// Map algorithm to go-xkms signing format
	algoFormat := kb.signingAlgorithmToKeyIDFormat(algorithm)
	return fmt.Sprintf("%s:signing:%s:%s", kb.backendType, algoFormat, keyID)
}

// signingAlgorithmToKeyIDFormat converts signing algorithm strings to go-xkms key ID format.
// Preserves ECDSA curve information (e.g., "ecdsa-p256") so the signer can determine the curve.
func (kb *XKMSBackend) signingAlgorithmToKeyIDFormat(algorithm string) string {
	alg := strings.ToUpper(algorithm)
	switch {
	case strings.Contains(alg, "RSA"):
		return "rsa"
	case strings.Contains(alg, "P-521") || strings.Contains(alg, "P521"):
		return "ecdsa-p521"
	case strings.Contains(alg, "P-384") || strings.Contains(alg, "P384"):
		return "ecdsa-p384"
	case strings.Contains(alg, "P-256") || strings.Contains(alg, "P256"):
		return "ecdsa-p256"
	case strings.Contains(alg, "ECDSA") || strings.Contains(alg, "EC"):
		// Default ECDSA to P-256 if no specific curve mentioned
		return "ecdsa-p256"
	case strings.Contains(alg, "ED25519"):
		return "ed25519"
	default:
		// Default to RSA for unknown signing algorithms
		return "rsa"
	}
}

// algorithmToKeyIDFormat converts our algorithm string to go-xkms key ID format.
func (kb *XKMSBackend) algorithmToKeyIDFormat(algorithm string) string {
	switch strings.ToUpper(algorithm) {
	case "AES-128-GCM", "AES128-GCM", "AES128GCM":
		return "aes128-gcm"
	case "AES-192-GCM", "AES192-GCM", "AES192GCM":
		return "aes192-gcm"
	case "AES-256-GCM", "AES256-GCM", "AES256GCM":
		return "aes256-gcm"
	case "CHACHA20-POLY1305", "CHACHA20POLY1305":
		return "chacha20-poly1305"
	case "XCHACHA20-POLY1305", "XCHACHA20POLY1305":
		return "xchacha20-poly1305"
	default:
		// Default to AES-256-GCM
		return "aes256-gcm"
	}
}

// keyVersionMetadataKey is the metadata key used to store key version in encrypted data.
const keyVersionMetadataKey = "kv" // "kv" = key version

// Encrypt encrypts plaintext using the specified key.
// The current key version is stored in the encrypted data's metadata to ensure
// decryption uses the correct key version after key rotation.
func (kb *XKMSBackend) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	// Get key metadata to determine algorithm and current version
	meta, err := kb.GetKeyMetadata(ctx, keyID)
	if err != nil {
		return nil, api.NewKeyMetadataRetrievalError("XKMSBackend.Encrypt", err)
	}

	// Determine which key to use based on version
	// Version 1 uses the base key (backward compatible)
	// Version 2+ uses versioned keys (keyID:v2, keyID:v3, etc.)
	var actualKeyID string
	if meta.Version == 1 {
		actualKeyID = keyID
	} else {
		actualKeyID = kb.buildVersionedKeyID(keyID, meta.Version)
	}

	// Build the full key ID with algorithm info for go-xkms
	// Format: "backend:type:algo:keyname"
	kid := kb.buildFullKeyID(actualKeyID, meta.Algorithm)

	// Use go-xkms encrypt
	opts := &kctypes.EncryptOptions{
		AdditionalData: aad,
	}

	encrypted, err := xkms.Encrypt(ctx, kid, plaintext, opts)
	if err != nil {
		return nil, api.NewEncryptionOperationError("XKMSBackend.Encrypt", err)
	}

	// Store key version in metadata for decryption after rotation
	if encrypted.Metadata == nil {
		encrypted.Metadata = make(map[string][]byte)
	}
	encrypted.Metadata[keyVersionMetadataKey] = []byte(fmt.Sprintf("%d", meta.Version))

	// Serialize encrypted data for storage
	return kb.serializeEncryptedData(encrypted)
}

// Decrypt decrypts ciphertext using the specified key.
// The key version is read from the encrypted data's metadata to ensure
// the correct key version is used after key rotation.
func (kb *XKMSBackend) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	// Deserialize encrypted data
	encrypted, err := kb.deserializeEncryptedData(ciphertext)
	if err != nil {
		return nil, api.NewCiphertextDeserializationError("XKMSBackend.Decrypt", err)
	}

	// Get key metadata to determine algorithm
	meta, err := kb.GetKeyMetadata(ctx, keyID)
	if err != nil {
		return nil, api.NewKeyMetadataRetrievalError("XKMSBackend.Decrypt", err)
	}

	// Read key version from encrypted data metadata
	// If not present (legacy data), assume version 1
	version := 1
	if encrypted.Metadata != nil {
		if versionBytes, ok := encrypted.Metadata[keyVersionMetadataKey]; ok {
			if _, err := fmt.Sscanf(string(versionBytes), "%d", &version); err != nil {
				// If we can't parse the version, fall back to version 1
				version = 1
			}
		}
	}

	// Determine which key to use based on version
	// Version 1 uses the base key (backward compatible)
	// Version 2+ uses versioned keys (keyID:v2, keyID:v3, etc.)
	var actualKeyID string
	if version == 1 {
		actualKeyID = keyID
	} else {
		actualKeyID = kb.buildVersionedKeyID(keyID, version)
	}

	// Build the full key ID with algorithm info for go-xkms
	// Format: "backend:type:algo:keyname"
	kid := kb.buildFullKeyID(actualKeyID, meta.Algorithm)

	// Use go-xkms decrypt
	opts := &kctypes.DecryptOptions{
		AdditionalData: aad,
	}

	plaintext, err := xkms.Decrypt(ctx, kid, encrypted, opts)
	if err != nil {
		return nil, api.NewDecryptionOperationError("XKMSBackend.Decrypt", err)
	}

	return plaintext, nil
}

// Sign creates a digital signature.
// The digest parameter is expected to be a pre-hashed value.
func (kb *XKMSBackend) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	// Get key metadata to determine the signing algorithm
	meta, err := kb.GetKeyMetadata(ctx, keyID)
	if err != nil {
		return nil, api.NewKeyMetadataRetrievalError("XKMSBackend.Sign", err)
	}

	// Build the full key ID with backend and algorithm info for go-xkms
	// Format: "backend:signing:algo:keyname"
	kid := kb.buildSigningKeyID(keyID, meta.Algorithm)

	// Get the signer directly from go-xkms
	// This avoids the xkms.Sign() helper which always hashes the input,
	// causing double-hashing when digest is already a hash (e.g., from x509.CreateCertificate)
	signer, err := xkms.SignerByID(ctx, kid)
	if err != nil {
		return nil, api.NewSignerRetrievalError("XKMSBackend.Sign", err)
	}

	// Map algorithm to hash for SignerOpts
	hash := kb.algorithmToHash(algorithm)

	// Pass the pre-hashed digest directly to the signer.
	// The signer.Sign() interface expects a digest (pre-hashed data), not raw data.
	sig, err := signer.Sign(rand.Reader, digest, hash)
	if err != nil {
		return nil, api.NewSigningOperationError("XKMSBackend.Sign", err)
	}

	return sig, nil
}

// Verify verifies a digital signature.
// The digest parameter is expected to be a pre-hashed value.
func (kb *XKMSBackend) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	if !kb.IsAvailable(ctx) {
		return false, ErrBackendNotReady
	}

	// Get key metadata to determine the signing algorithm
	meta, err := kb.GetKeyMetadata(ctx, keyID)
	if err != nil {
		return false, api.NewKeyMetadataRetrievalError("XKMSBackend.Verify", err)
	}

	// Build the full key ID with backend and algorithm info for go-xkms
	// Format: "backend:signing:algo:keyname"
	kid := kb.buildSigningKeyID(keyID, meta.Algorithm)

	// Get the signer directly from go-xkms to extract the public key
	// This avoids the xkms.Verify() helper which always hashes the input,
	// causing double-hashing when digest is already a hash
	signer, err := xkms.SignerByID(ctx, kid)
	if err != nil {
		return false, api.NewSignerRetrievalError("XKMSBackend.Verify", err)
	}

	// Get the public key from the signer
	pubKey := signer.Public()

	// Verify the signature directly based on key type
	// The digest is already hashed, so we verify directly without re-hashing
	switch pk := pubKey.(type) {
	case *ecdsa.PublicKey:
		return ecdsa.VerifyASN1(pk, digest, signature), nil
	case *rsa.PublicKey:
		hash := kb.algorithmToHash(algorithm)
		err := rsa.VerifyPKCS1v15(pk, hash, digest, signature)
		return err == nil, nil
	case ed25519.PublicKey:
		// Ed25519 uses the full message, not a digest, but we pass through
		// what we have since the caller should have handled this appropriately
		return ed25519.Verify(pk, digest, signature), nil
	default:
		return false, api.NewUnsupportedPublicKeyTypeError("XKMSBackend.Verify", fmt.Sprintf("%T", pubKey))
	}
}

// GenerateDataKey generates a new data encryption key (DEK).
func (kb *XKMSBackend) GenerateDataKey(ctx context.Context, keyID string, keyBytes int) (plaintext, wrapped []byte, err error) {
	if !kb.IsAvailable(ctx) {
		return nil, nil, ErrBackendNotReady
	}

	// Generate random plaintext DEK using xkms RNG
	plaintext, err = xkms.Rand(keyBytes)
	if err != nil {
		return nil, nil, api.NewRandomKeyGenerationError("XKMSBackend.GenerateDataKey", err)
	}

	// Encrypt (wrap) the DEK using the KEK
	wrapped, err = kb.Encrypt(ctx, keyID, plaintext, nil)
	if err != nil {
		return nil, nil, api.NewDEKWrappingError("XKMSBackend.GenerateDataKey", err)
	}

	return plaintext, wrapped, nil
}

// ExportPublicKey exports the DER-encoded public key for an asymmetric key.
func (kb *XKMSBackend) ExportPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	// Build the full key ID for go-xkms
	meta, err := kb.GetKeyMetadata(ctx, keyID)
	if err != nil {
		return nil, api.NewKeyMetadataRetrievalError("XKMSBackend.ExportPublicKey", err)
	}

	// Build the signing key ID format
	kid := kb.buildSigningKeyID(keyID, meta.Algorithm)

	// Get the signer which has access to the public key
	signer, err := xkms.SignerByID(ctx, kid)
	if err != nil {
		return nil, api.NewSignerRetrievalError("XKMSBackend.ExportPublicKey", err)
	}

	// Get the public key from the signer
	publicKey := signer.Public()
	if publicKey == nil {
		return nil, api.NewNoPublicComponentError("XKMSBackend.ExportPublicKey")
	}

	// Marshal the public key to DER format
	derBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return nil, api.NewPublicKeyMarshalError("XKMSBackend.ExportPublicKey", err)
	}

	return derBytes, nil
}

// Close releases resources held by the backend.
func (kb *XKMSBackend) Close() error {
	// No resources to release - xkms lifecycle is managed globally
	return nil
}

// algorithmToKeyAttributes converts an algorithm string to KeyAttributes.
func (kb *XKMSBackend) algorithmToKeyAttributes(keyID, algorithm string) *kctypes.KeyAttributes {
	attrs := &kctypes.KeyAttributes{
		CN:        keyID,
		StoreType: kctypes.StoreType(string(kb.backendType)),
		KeyType:   kctypes.KeyTypeEncryption,
	}

	alg := strings.ToUpper(algorithm)

	// Map algorithm to go-xkms attributes
	switch {
	case strings.HasPrefix(alg, "AES-256") || alg == "AES256-GCM" || alg == "AES-256-GCM":
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
	case strings.HasPrefix(alg, "AES-192"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES192GCM
	case strings.HasPrefix(alg, "AES-128"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES128GCM
	case strings.HasPrefix(alg, "XCHACHA20"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricXChaCha20Poly1305
	case strings.HasPrefix(alg, "CHACHA20"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricChaCha20Poly1305
	case strings.Contains(alg, "RSA-4096") || strings.Contains(alg, "RSA4096"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.KeyType = kctypes.KeyTypeSigning
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 4096}
	case strings.Contains(alg, "RSA-3072") || strings.Contains(alg, "RSA3072"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.KeyType = kctypes.KeyTypeSigning
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 3072}
	case strings.Contains(alg, "RSA-2048") || strings.Contains(alg, "RSA2048"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.KeyType = kctypes.KeyTypeSigning
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 2048}
	case strings.HasPrefix(alg, "RSA"):
		// Default RSA to 4096 bits
		attrs.KeyAlgorithm = x509.RSA
		attrs.KeyType = kctypes.KeyTypeSigning
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 4096}
	case strings.Contains(alg, "P-521") || strings.Contains(alg, "P521"):
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.KeyType = kctypes.KeyTypeSigning
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P521()}
	case strings.Contains(alg, "P-384") || strings.Contains(alg, "P384"):
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.KeyType = kctypes.KeyTypeSigning
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P384()}
	case strings.Contains(alg, "P-256") || strings.Contains(alg, "P256"):
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.KeyType = kctypes.KeyTypeSigning
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P256()}
	case strings.HasPrefix(alg, "ECDSA") || strings.HasPrefix(alg, "EC"):
		// Default ECDSA to P-256
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.KeyType = kctypes.KeyTypeSigning
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P256()}
	case strings.Contains(alg, "ED25519"):
		attrs.KeyAlgorithm = x509.Ed25519
		attrs.KeyType = kctypes.KeyTypeSigning
	default:
		// Default to AES-256-GCM for symmetric
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
	}

	return attrs
}

// isSymmetricAlgorithm checks if the algorithm is for symmetric encryption.
func (kb *XKMSBackend) isSymmetricAlgorithm(algorithm string) bool {
	alg := strings.ToUpper(algorithm)
	return strings.HasPrefix(alg, "AES") || strings.HasPrefix(alg, "CHACHA") || strings.HasPrefix(alg, "XCHACHA")
}

// attrsToAlgorithmString converts KeyAttributes to a human-readable algorithm string.
func (kb *XKMSBackend) attrsToAlgorithmString(attrs *kctypes.KeyAttributes) string {
	// Check symmetric algorithms first
	switch attrs.SymmetricAlgorithm {
	case kctypes.SymmetricAES128GCM:
		return AlgAES128GCM
	case kctypes.SymmetricAES192GCM:
		return AlgAES192GCM
	case kctypes.SymmetricAES256GCM:
		return AlgAES256GCM
	case kctypes.SymmetricChaCha20Poly1305:
		return AlgChaCha20Poly1305
	case kctypes.SymmetricXChaCha20Poly1305:
		return AlgXChaCha20Poly1305
	}

	// Check asymmetric algorithms
	switch attrs.KeyAlgorithm {
	case x509.RSA:
		if attrs.RSAAttributes != nil {
			switch attrs.RSAAttributes.KeySize {
			case 2048:
				return AlgRSA2048
			case 3072:
				return AlgRSA3072
			case 4096:
				return AlgRSA4096
			}
		}
		return AlgRSA4096 // default
	case x509.ECDSA:
		if attrs.ECCAttributes != nil && attrs.ECCAttributes.Curve != nil {
			switch attrs.ECCAttributes.Curve.Params().BitSize {
			case 256:
				return AlgECDSAP256
			case 384:
				return AlgECDSAP384
			case 521:
				return AlgECDSAP521
			}
		}
		return AlgECDSAP256 // default
	case x509.Ed25519:
		return AlgEd25519
	}

	// Default
	return AlgAES256GCM
}

// normalizeAlgorithmForXKMS converts algorithm names to the format expected by go-xkms.
// go-xkms expects lowercase algorithm names without hyphens between letters and numbers.
// Examples: "AES-256-GCM" -> "aes256-gcm", "RSA-4096" -> "rsa-4096", "ChaCha20-Poly1305" -> "chacha20-poly1305".
func (kb *XKMSBackend) normalizeAlgorithmForXKMS(algorithm string) string {
	alg := strings.ToLower(algorithm)
	// Remove hyphen between letters and numbers (e.g., "aes-256" -> "aes256")
	// but keep hyphens between words (e.g., "chacha20-poly1305")
	result := strings.Builder{}
	for i := 0; i < len(alg); i++ {
		ch := alg[i]
		if ch == '-' && i > 0 && i < len(alg)-1 {
			prevChar := alg[i-1]
			nextChar := alg[i+1]
			// Skip hyphen if it's between a letter and a digit
			if (prevChar >= 'a' && prevChar <= 'z') && (nextChar >= '0' && nextChar <= '9') {
				continue
			}
			// Skip hyphen if it's between a digit and a letter that starts "gcm", "cbc", etc.
			if (prevChar >= '0' && prevChar <= '9') && (nextChar >= 'a' && nextChar <= 'z') {
				// Keep if this is word separator like "256-gcm" -> "256-gcm"
				result.WriteByte(ch)
				continue
			}
		}
		result.WriteByte(ch)
	}
	return result.String()
}

// algorithmToHash maps algorithm strings to crypto.Hash values.
func (kb *XKMSBackend) algorithmToHash(algorithm string) crypto.Hash {
	alg := strings.ToUpper(algorithm)
	switch {
	case strings.Contains(alg, "SHA-256") || strings.Contains(alg, "SHA256"):
		return crypto.SHA256
	case strings.Contains(alg, "SHA-384") || strings.Contains(alg, "SHA384"):
		return crypto.SHA384
	case strings.Contains(alg, "SHA-512") || strings.Contains(alg, "SHA512"):
		return crypto.SHA512
	case strings.Contains(alg, "SHA-1") || strings.Contains(alg, "SHA1"):
		return crypto.SHA1
	default:
		return crypto.SHA256
	}
}

// serializeEncryptedData converts EncryptedData to bytes for storage.
func (kb *XKMSBackend) serializeEncryptedData(data *kctypes.EncryptedData) ([]byte, error) {
	return json.Marshal(data)
}

// deserializeEncryptedData converts bytes back to EncryptedData.
func (kb *XKMSBackend) deserializeEncryptedData(data []byte) (*kctypes.EncryptedData, error) {
	var encrypted kctypes.EncryptedData
	if err := json.Unmarshal(data, &encrypted); err != nil {
		return nil, err
	}
	return &encrypted, nil
}

// Ensure XKMSBackend implements Backend.
var _ Backend = (*XKMSBackend)(nil)
