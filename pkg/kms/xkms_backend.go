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
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
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

	// registry is the durable key registry (guarded by mu): owner records map
	// a stored encoded name to the tenant key ID it was created for, and
	// version records hold each key's highest rotated version. With no
	// registry (unit tests), encoded names resolve by decoding alone and
	// versions are inferred from the contiguous stored run keyID_v2..vN.
	registry interfaces.KVStore
}

// SetKeyRegistry sets the durable key registry that records key owners and
// rotated versions.
func (kb *XKMSBackend) SetKeyRegistry(kv interfaces.KVStore) {
	kb.mu.Lock()
	kb.registry = kv
	kb.mu.Unlock()
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
		// A backend that reports attestation has stated what it does, and it is
		// neither signing nor encryption (AVR): it offers no algorithm to route
		// on. The defaults are only for a backend whose capabilities say nothing.
		if caps.Attestation {
			return []string{}
		}
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

	if err := kb.refuseVersionName(ctx, attrs.CN); err != nil {
		return nil, fmt.Errorf("%w: key ID %q names a version of an existing key", ErrInvalidKeyID, attrs.CN)
	}

	if err := kb.refuseHidingName(ctx, attrs.CN); err != nil {
		return nil, err
	}

	// go-xkms validates key names more strictly than quicbaas key IDs (a
	// tenant-scoped ID carries "/"), so the backend sees the encoded name
	// while the caller's attrs and the metadata keep the quicbaas ID.
	keyID := attrs.CN
	name, err := xkmsKeyName(keyID)
	if err != nil {
		return nil, err
	}
	if err := kb.refuseCollidingName(ctx, keyID, name); err != nil {
		return nil, err
	}
	backendAttrs := *attrs
	backendAttrs.CN = name

	// Ensure StoreType matches backend
	if backendAttrs.StoreType == "" {
		backendAttrs.StoreType = kctypes.StoreType(string(kb.backendType))
	}
	attrs = &backendAttrs

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

	// A key whose owner is not recorded would not resolve: remove it.
	if err := kb.recordOwner(ctx, keyID, name); err != nil {
		_ = deleteXKMSKey(ctx, attrs)
		return nil, err
	}

	// Build metadata
	now := time.Now()
	algorithm := kb.attrsToAlgorithmString(attrs)
	meta := &KeyMetadata{
		KeyID:      keyID,
		Name:       keyID,
		Algorithm:  algorithm,
		State:      KeyStateEnabled,
		Version:    1,
		Exportable: attrs.Exportable,
		Created:    now,
		Updated:    now,
	}

	// Cache metadata
	kb.mu.Lock()
	kb.metadata[keyID] = meta
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

	if err := kb.refuseVersionName(ctx, keyID); err != nil {
		return nil, fmt.Errorf("%w: key ID %q names a version of an existing key", ErrInvalidKeyID, keyID)
	}

	if err := kb.refuseHidingName(ctx, keyID); err != nil {
		return nil, err
	}

	if len(material) == 0 {
		return nil, ErrKeyMaterialEmpty
	}

	// Build key attributes for the imported key
	attrs, err := kb.algorithmToKeyAttributes(keyID, algorithm)
	if err != nil {
		return nil, err
	}

	// Symmetric and asymmetric import are ONE operation. The algorithm rides on
	// attrs, and go-xkms interprets the material accordingly: raw bytes for a
	// symmetric key, PKCS#8 DER for an asymmetric one, which the backend parses
	// and checks against attrs before storing.
	//
	// Asymmetric material was refused here, under a comment claiming the parsing
	// would have to be written. It was already written one layer down, so the
	// refusal withheld a capability we owned — and the CLI's asymmetric import
	// path terminated against it.
	kind := "symmetric"
	if !kb.isSymmetricAlgorithm(algorithm) {
		kind = "asymmetric"
	}

	if err := kb.refuseCollidingName(ctx, keyID, attrs.CN); err != nil {
		return nil, err
	}

	if err := xkms.ImportKeyMaterial(ctx, string(kb.backendType), attrs, material); err != nil {
		return nil, api.NewKeyImportError("XKMSBackend.ImportKey", kind, err)
	}

	// A key whose owner is not recorded would not resolve: remove it.
	if err := kb.recordOwner(ctx, keyID, attrs.CN); err != nil {
		_ = deleteXKMSKey(ctx, attrs)
		return nil, err
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

	if err := kb.refuseVersionName(ctx, keyID); err != nil {
		return nil, err
	}

	// Check cache first
	kb.mu.RLock()
	if meta, ok := kb.metadata[keyID]; ok {
		kb.mu.RUnlock()
		return meta, nil
	}
	kb.mu.RUnlock()

	name, err := xkmsKeyName(keyID)
	if err != nil {
		return nil, err
	}

	// An encoded name resolves to keyID only through keyID's owner record.
	if err := kb.refuseUnownedName(ctx, keyID, name); err != nil {
		return nil, err
	}

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
		kid := fmt.Sprintf("%s:signing:%s:%s", kb.backendType, sf.format, name)
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
			kid := fmt.Sprintf("%s:encryption:%s:%s", kb.backendType, sf.format, name)
			if _, err := xkms.GetSymmetricKey(ctx, kid); err == nil {
				foundAlgorithm = sf.algo
				break
			}
		}
	}

	// If still not found, try legacy lookup methods
	if foundAlgorithm == "" {
		attrs := &kctypes.KeyAttributes{
			CN:        name,
			StoreType: kctypes.StoreType(string(kb.backendType)),
		}

		// Try to get signer with just CN
		if _, err := xkms.Signer(ctx, attrs); err != nil {
			// Try symmetric key by ID
			if _, err := xkms.GetSymmetricKey(ctx, name); err != nil {
				return nil, ErrKeyNotFound
			}
			foundAlgorithm = "AES-256-GCM" // Default for symmetric
		} else {
			foundAlgorithm = "RSA-4096" // Default for signing
		}
	}

	// The current version is the one recorded at rotation (with a registry),
	// else the last of the contiguous rotated versions keyID_v2, keyID_v3, ...
	// that go-xkms stores: an ordinary key such as report_v2026 is not a
	// version of report. A failed read leaves the version unknown, so nothing
	// is cached and the caller sees the error.
	version, err := kb.storedVersion(ctx, keyID)
	if err != nil {
		return nil, err
	}

	// Key exists but we don't have metadata - create minimal metadata
	meta := &KeyMetadata{
		KeyID:     keyID,
		Algorithm: foundAlgorithm,
		State:     KeyStateEnabled,
		Version:   version,
		Created:   time.Now(),
		Updated:   time.Now(),
	}

	kb.mu.Lock()
	kb.metadata[keyID] = meta
	kb.mu.Unlock()

	return meta, nil
}

// CurrentVersion returns keyID's current version as recorded in durable
// storage (the key registry's version record, else the rotated versions
// go-xkms stores), not this node's cached metadata, which another node's
// rotation does not invalidate. The cached metadata is brought up to date
// with it. A key that does not exist is ErrKeyNotFound.
func (kb *XKMSBackend) CurrentVersion(ctx context.Context, keyID string) (int, error) {
	if _, err := kb.GetKeyMetadata(ctx, keyID); err != nil {
		return 0, err
	}
	version, err := kb.storedVersion(ctx, keyID)
	if err != nil {
		return 0, err
	}
	// A key's versions only grow. The durable read above ran without kb.mu,
	// so a version lower than the cached one is a read taken before a
	// rotation this backend has since recorded: it neither lowers the cache
	// nor is returned, or the rotation would be rolled back.
	kb.mu.Lock()
	defer kb.mu.Unlock()
	if meta, ok := kb.metadata[keyID]; ok {
		if meta.Version > version {
			return meta.Version, nil
		}
		if meta.Version < version {
			refreshed := *meta
			refreshed.Version = version
			refreshed.Updated = time.Now()
			kb.metadata[keyID] = &refreshed
		}
	}
	return version, nil
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
	keys, err := listXKMSKeys(ctx, string(kb.backendType))
	if err != nil {
		// Log but don't fail - we'll try the cache
		kb.logger.WithField("error", err.Error()).Warn("failed to list keys from xkms, using cache")
	}

	// Add keys from xkms
	for _, attrs := range keys {
		keyID := quicbaasKeyIDFromXKMSName(attrs.CN)

		// Filter by prefix
		if prefix != "" && !strings.HasPrefix(keyID, prefix) {
			continue
		}

		// Check cache for metadata
		kb.mu.RLock()
		meta, ok := kb.metadata[keyID]
		kb.mu.RUnlock()

		if !ok {
			// Create minimal metadata
			meta = &KeyMetadata{
				KeyID:   keyID,
				State:   KeyStateEnabled,
				Version: 1,
			}
		}

		result = append(result, meta)
		seen[keyID] = true

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

// RotateKey rotates a key to a new version, creating versioned keys
// ("<id>_v<n>") while preserving old versions for decryption. This predates
// go-xkms's own key versions (its RotateKey now keeps earlier versions too,
// as "<id>--v<n>" with a version record); the two schemes are independent.
func (kb *XKMSBackend) RotateKey(ctx context.Context, keyID string) (*KeyMetadata, error) {
	if !kb.IsAvailable(ctx) {
		return nil, ErrBackendNotReady
	}

	if err := kb.refuseVersionName(ctx, keyID); err != nil {
		return nil, err
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

	// A key already holding the new version's name, or the next one, would be
	// overwritten or adopted into the rotated run: refuse the rotation.
	stored, err := kb.listStoredKeyIDs(ctx, keyID+"_v")
	if err != nil {
		return nil, err
	}
	for _, taken := range []int{newVersion, newVersion + 1} {
		if _, ok := stored[kb.buildVersionedKeyID(keyID, taken)]; ok {
			return nil, ErrVersionNameTaken
		}
	}

	// Build attributes for the new versioned key
	versionedKeyID := kb.buildVersionedKeyID(keyID, newVersion)
	attrs, err := kb.algorithmToKeyAttributes(versionedKeyID, meta.Algorithm)
	if err != nil {
		return nil, err
	}

	// Generate new key with versioned ID
	// This backend's own version scheme (see the method comment).
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

	// Record the new version. An unrecorded version is not one of keyID's
	// versions, so on failure it is removed and the rotation fails.
	if reg := kb.keyRegistry(); reg != nil {
		if err := reg.Put(ctx, kb.versionRecordKey(keyID), []byte(strconv.Itoa(newVersion))); err != nil {
			_ = deleteXKMSKey(ctx, attrs)
			return nil, fmt.Errorf("xkms key registry: record version %d of %q: %w", newVersion, keyID, err)
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

// versionNameBase returns the base key ID of a name of the form
// <base>_v<digits> (the name buildVersionedKeyID gives version N of key
// <base>), and false for any other ID.
func versionNameBase(keyID string) (string, bool) {
	idx := strings.LastIndex(keyID, "_v")
	if idx <= 0 {
		return "", false
	}
	digits := keyID[idx+2:]
	if digits == "" {
		return "", false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return "", false
		}
	}
	base := keyID[:idx]
	if strings.HasSuffix(base, "/") {
		return "", false
	}
	return base, true
}

// refuseVersionName returns ErrKeyNotFound when keyID names a version of an
// existing key (<base>_v<digits> with <base> a key in the same scope). A
// version is stored by go-xkms under that name, but it is never a public key
// ID: callers address the parent key, so policy is always checked against
// the parent key ID. An ID of that form whose base does not exist is an
// ordinary key.
func (kb *XKMSBackend) refuseVersionName(ctx context.Context, keyID string) error {
	base, ok := versionNameBase(keyID)
	if !ok {
		return nil
	}
	n, err := strconv.Atoi(keyID[len(base)+len("_v"):])
	if err != nil || n < 2 || keyID != kb.buildVersionedKeyID(base, n) {
		// Rotation writes no such name, so keyID is an ordinary key.
		return nil
	}
	_, err = kb.GetKeyMetadata(ctx, base)
	switch {
	case err == nil:
	case stderrors.Is(err, ErrKeyNotFound):
		// The base is not a key, so keyID is an ordinary key.
		return nil
	default:
		// Fail closed: the lookup could not tell whether base is a key.
		return err
	}
	// Only base's versions base_v2..base_vk recorded at rotation (with a
	// registry) or the contiguous stored run (without one) are rotation
	// output; any other name is an ordinary key.
	current, err := kb.storedVersion(ctx, base)
	if err != nil {
		return err
	}
	if n <= current {
		return ErrKeyNotFound
	}
	return nil
}

// highestContiguousVersion returns the last version of keyID's contiguous
// rotated run keyID_v2, keyID_v3, ... in stored, or 1 when keyID_v2 is not
// stored. Walking up from 2 rather than taking the largest suffix keeps an
// ordinary key such as report_v2026 out of report's versions.
func (kb *XKMSBackend) highestContiguousVersion(stored map[string]struct{}, keyID string) int {
	version := 1
	for {
		if _, ok := stored[kb.buildVersionedKeyID(keyID, version+1)]; !ok {
			return version
		}
		version++
	}
}

// keyRegistry returns the durable key registry, or nil when none is set.
func (kb *XKMSBackend) keyRegistry() interfaces.KVStore {
	kb.mu.RLock()
	defer kb.mu.RUnlock()
	return kb.registry
}

// ownerRecordKey is the registry key mapping a stored xkms name to the
// quicbaas key ID it was created for.
func (kb *XKMSBackend) ownerRecordKey(name string) string {
	return fmt.Sprintf("kms:xkms:owner:%s:%s", kb.backendType, name)
}

// versionRecordKey is the registry key holding keyID's highest rotated
// version, in decimal.
func (kb *XKMSBackend) versionRecordKey(keyID string) string {
	return fmt.Sprintf("kms:xkms:version:%s:%s", kb.backendType, keyID)
}

// readRegistryRecord reads a registry record; ok is false when it is absent.
func readRegistryRecord(ctx context.Context, reg interfaces.KVStore, key string) (string, bool, error) {
	data, err := reg.Get(ctx, key)
	if err != nil {
		if qberrors.IsNotFound(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("xkms key registry: read %q: %w", key, err)
	}
	if len(data) == 0 {
		return "", false, nil
	}
	return string(data), true, nil
}

// refuseUnownedName fails with ErrKeyNotFound when, with a registry set,
// keyID is stored under an encoded name whose owner record does not name
// keyID: such a stored key (a system key that happens to carry the encoded
// name) is not keyID's.
func (kb *XKMSBackend) refuseUnownedName(ctx context.Context, keyID, name string) error {
	reg := kb.keyRegistry()
	if reg == nil || name == keyID {
		return nil
	}
	owner, ok, err := readRegistryRecord(ctx, reg, kb.ownerRecordKey(name))
	if err != nil {
		return err
	}
	if !ok || owner != keyID {
		return ErrKeyNotFound
	}
	return nil
}

// refuseCollidingName refuses, with a registry set, to create or import
// keyID when its encoded name is already stored without an owner record for
// keyID: the stored key belongs to someone else.
func (kb *XKMSBackend) refuseCollidingName(ctx context.Context, keyID, name string) error {
	reg := kb.keyRegistry()
	if reg == nil || name == keyID {
		return nil
	}
	stored, err := kb.listStoredKeyIDs(ctx, keyID)
	if err != nil {
		return err
	}
	if _, ok := stored[keyID]; !ok {
		return nil
	}
	owner, ok, err := readRegistryRecord(ctx, reg, kb.ownerRecordKey(name))
	if err != nil {
		return err
	}
	if !ok || owner != keyID {
		return fmt.Errorf("%w: key ID %q: its stored name %q is held by another key",
			kctypes.ErrBackendFileAlreadyExists, keyID, name)
	}
	return nil
}

// recordOwner writes, with a registry set, the owner record of keyID's
// encoded name. A "/"-free ID is stored under its own name and has none.
func (kb *XKMSBackend) recordOwner(ctx context.Context, keyID, name string) error {
	reg := kb.keyRegistry()
	if reg == nil || name == keyID {
		return nil
	}
	if err := reg.Put(ctx, kb.ownerRecordKey(name), []byte(keyID)); err != nil {
		return fmt.Errorf("xkms key registry: record owner of %q: %w", keyID, err)
	}
	return nil
}

// storedVersion is keyID's highest version held in go-xkms. With a registry
// it is the version recorded at rotation (1 when none is recorded), so a
// stored name that only looks like a version is never adopted. Without one,
// it is the contiguous stored run keyID_v2..vN.
func (kb *XKMSBackend) storedVersion(ctx context.Context, keyID string) (int, error) {
	if reg := kb.keyRegistry(); reg != nil {
		n, ok, err := kb.readVersionRecord(ctx, reg, keyID)
		if err != nil {
			return 0, err
		}
		if !ok {
			return kb.migrateVersionRecord(ctx, reg, keyID)
		}
		return n, nil
	}
	stored, err := kb.listStoredKeyIDs(ctx, keyID+"_v")
	if err != nil {
		return 0, err
	}
	return kb.highestContiguousVersion(stored, keyID), nil
}

// migrateVersionRecord gives a stored key that has no version record one.
//
// Rotation records every version it writes, so a stored key without a record
// was never rotated under the registry: it predates it. Its pre-registry
// keyID_vN names cannot be told apart from independent keys created before
// the registry under such a name, so none is adopted: the key records version
// 1 and every such name stays an ordinary key. A rotation under the registry
// then refuses with ErrVersionNameTaken while keyID_v2 exists, rather than
// silently taking over an existing key. A key that is not stored reports
// version 1 and no record is written for it.
func (kb *XKMSBackend) migrateVersionRecord(ctx context.Context, reg interfaces.KVStore, keyID string) (int, error) {
	stored, err := kb.listStoredKeyIDs(ctx, keyID)
	if err != nil {
		return 0, err
	}
	if _, ok := stored[keyID]; !ok {
		return 1, nil
	}
	version := 1
	recordKey := kb.versionRecordKey(keyID)
	value := []byte(strconv.Itoa(version))
	guarded, hasGuarded := reg.(interface {
		PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error)
	})
	// A rotation on another node may record a version between the listing
	// above and this write: the migrated record never overwrites one. A
	// store with a guarded insert makes the absence check and the write one
	// step; a lost insert re-reads the record that won.
	if hasGuarded {
		stored, err := guarded.PutIfAbsent(ctx, recordKey, value)
		if err != nil {
			return 0, fmt.Errorf("xkms key registry: record migrated version %d of %q: %w", version, keyID, err)
		}
		if stored {
			return version, nil
		}
		n, found, err := kb.readVersionRecord(ctx, reg, keyID)
		if err != nil {
			return 0, err
		}
		if !found {
			return 0, fmt.Errorf("xkms key registry: version record of %q vanished after a lost insert", keyID)
		}
		return n, nil
	}
	// A store without a guarded insert can only check for a record first.
	n, found, err := kb.readVersionRecord(ctx, reg, keyID)
	if err != nil {
		return 0, err
	}
	if found {
		return n, nil
	}
	if err := reg.Put(ctx, recordKey, value); err != nil {
		return 0, fmt.Errorf("xkms key registry: record migrated version %d of %q: %w", version, keyID, err)
	}
	return version, nil
}

// readVersionRecord reads keyID's durable version record, reporting whether
// one exists. A record that is not a positive integer is an error.
func (kb *XKMSBackend) readVersionRecord(ctx context.Context, reg interfaces.KVStore, keyID string) (int, bool, error) {
	v, ok, err := readRegistryRecord(ctx, reg, kb.versionRecordKey(keyID))
	if err != nil || !ok {
		return 0, false, err
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, false, fmt.Errorf("xkms key registry: version record of %q is not a positive integer: %q", keyID, v)
	}
	return n, true, nil
}

// refuseHidingName refuses to create keyID when an ordinary key already
// exists whose name refuseVersionName would read as a version of keyID: once
// keyID exists, that key would be unreachable. When keyID itself already
// exists, the caller's own "already exists" result stands and nothing is
// checked here.
func (kb *XKMSBackend) refuseHidingName(ctx context.Context, keyID string) error {
	if _, err := kb.GetKeyMetadata(ctx, keyID); err == nil {
		return nil
	}
	id, err := kb.conflictingVersionKeys(ctx, keyID)
	if err != nil {
		return err
	}
	if id != "" {
		return fmt.Errorf("%w: key ID %q would hide existing key %q", ErrInvalidKeyID, keyID, id)
	}
	return nil
}

// conflictingVersionKeys returns the first existing key whose name reads as a
// version of keyID (keyID_v<digits>), or "" when there is none. A list error
// is returned, so the caller fails closed.
func (kb *XKMSBackend) conflictingVersionKeys(ctx context.Context, keyID string) (string, error) {
	// The stored listing, not ListKeys: ListKeys falls back to the cache when go-xkms cannot list, and a restarted node's cache misses keys that exist.
	ids, err := kb.listStoredKeyIDs(ctx, keyID+"_v")
	if err != nil {
		return "", err
	}
	conflicts := make([]string, 0, len(ids))
	for id := range ids {
		if base, ok := versionNameBase(id); ok && base == keyID {
			conflicts = append(conflicts, id)
		}
	}
	if len(conflicts) == 0 {
		return "", nil
	}
	sort.Strings(conflicts)
	return conflicts[0], nil
}

// listXKMSKeys is go-xkms's key listing; a package variable so tests can make it fail.
var listXKMSKeys = xkms.ListKeys

// deleteXKMSKey is go-xkms's key deletion; a package variable so tests can make it fail.
var deleteXKMSKey = xkms.DeleteKey

// listStoredKeyIDs returns the QuicBaaS key IDs go-xkms stores under this backend whose ID starts with prefix.
// It never consults the metadata cache, and returns go-xkms's listing error unchanged.
func (kb *XKMSBackend) listStoredKeyIDs(ctx context.Context, prefix string) (map[string]struct{}, error) {
	keys, err := listXKMSKeys(ctx, string(kb.backendType))
	if err != nil {
		return nil, err
	}
	ids := make(map[string]struct{}, len(keys))
	for _, attrs := range keys {
		if attrs == nil {
			continue
		}
		// Filter on the decoded ID: a tenant-scoped name is stored hex-encoded.
		keyID := quicbaasKeyIDFromXKMSName(attrs.CN)
		if prefix != "" && !strings.HasPrefix(keyID, prefix) {
			continue
		}
		ids[keyID] = struct{}{}
	}
	return ids, nil
}

// DisableKey temporarily disables a key.
func (kb *XKMSBackend) DisableKey(ctx context.Context, keyID string) error {
	if !kb.IsAvailable(ctx) {
		return ErrBackendNotReady
	}

	if err := kb.refuseVersionName(ctx, keyID); err != nil {
		return err
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

	if err := kb.refuseVersionName(ctx, keyID); err != nil {
		return err
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

	if err := kb.refuseVersionName(ctx, keyID); err != nil {
		return err
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
			if stderrors.Is(err, ErrKeyNotFound) {
				// The material may be gone with the registry cleanup
				// of an earlier deletion left undone: finish it.
				return kb.resumeRegistryCleanup(ctx, keyID)
			}
			return ErrKeyNotFound
		}
	}

	name, err := xkmsKeyName(keyID)
	if err != nil {
		return err
	}

	// Delete every rotated version first. A version left behind would
	// outlive its base and, once the base is gone, refuseVersionName would
	// no longer hide it: it would read as an ordinary key without a policy.
	// The versions are v2..vk, k recorded at rotation (with a registry) or
	// else the contiguous run read from go-xkms's stored listing; the cached
	// meta.Version is not used, since after a restart the uncached metadata
	// may not know every version. A <keyID>_v<N> outside them (an independent key, or a version stranded by an
	// out-of-band gap) is kept as an ordinary key. The run is deleted
	// highest first, so a partial failure leaves a contiguous v2..vj that
	// GetKeyMetadata's version walk and a retry still read.
	algorithm := ""
	if meta != nil {
		algorithm = meta.Algorithm
	}
	current, err := kb.storedVersion(ctx, keyID)
	if err != nil {
		return err
	}
	for n := current; n > 1; n-- {
		versionedID := kb.buildVersionedKeyID(keyID, n)
		var versionAttrs *kctypes.KeyAttributes
		if algorithm != "" {
			versionAttrs, err = kb.algorithmToKeyAttributes(versionedID, algorithm)
			if err != nil {
				return err
			}
		} else {
			versionName, err := xkmsKeyName(versionedID)
			if err != nil {
				return err
			}
			versionAttrs = &kctypes.KeyAttributes{
				CN:                 versionName,
				StoreType:          kctypes.StoreType(string(kb.backendType)),
				KeyType:            kctypes.KeyTypeEncryption,
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			}
		}
		if err := deleteXKMSKey(ctx, versionAttrs); err != nil {
			// The go-xkms backend reports a missing key with its own
			// sentinel, which pkg/xkms passes through untranslated.
			if stderrors.Is(err, xkms.ErrKeyNotFound) || stderrors.Is(err, kctypes.ErrBackendKeyNotFound) {
				continue
			}
			return api.NewKeyDeletionError("XKMSBackend.DeleteKey", err)
		}
	}

	// Build key attributes for deletion
	// Use DeleteKey(attrs) instead of DeleteKeyByID(kid) to avoid
	// key ID format validation issues with algorithms like ChaCha20-Poly1305
	var attrs *kctypes.KeyAttributes
	if meta != nil && meta.Algorithm != "" {
		attrs, err = kb.algorithmToKeyAttributes(keyID, meta.Algorithm)
		if err != nil {
			return err
		}
	} else {
		// Default to AES-256-GCM if we don't have algorithm info
		attrs = &kctypes.KeyAttributes{
			CN:                 name,
			StoreType:          kctypes.StoreType(string(kb.backendType)),
			KeyType:            kctypes.KeyTypeEncryption,
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		}
	}

	// Delete from go-xkms using attributes. A base already gone is an earlier
	// deletion whose registry cleanup failed (or another node's): its
	// cleanup resumes.
	if err := deleteXKMSKey(ctx, attrs); err != nil {
		if stderrors.Is(err, xkms.ErrKeyNotFound) {
			return kb.resumeRegistryCleanup(ctx, keyID)
		}
		// Try with default format as fallback
		defaultAttrs := &kctypes.KeyAttributes{
			CN:                 name,
			StoreType:          kctypes.StoreType(string(kb.backendType)),
			KeyType:            kctypes.KeyTypeEncryption,
			SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
		}
		if err2 := deleteXKMSKey(ctx, defaultAttrs); err2 != nil {
			if stderrors.Is(err2, xkms.ErrKeyNotFound) {
				return kb.resumeRegistryCleanup(ctx, keyID)
			}
			return api.NewKeyDeletionError("XKMSBackend.DeleteKey", err)
		}
	}

	kb.dropCachedMetadata(keyID)
	return kb.deleteRegistryRecords(ctx, keyID, name)
}

// dropCachedMetadata removes keyID's cached metadata. Once its key material is
// gone the cache must not outlive it, whether or not the registry cleanup that
// follows succeeds: a key recreated under the ID starts from version 1.
func (kb *XKMSBackend) dropCachedMetadata(keyID string) {
	kb.mu.Lock()
	delete(kb.metadata, keyID)
	kb.mu.Unlock()
}

// deleteRegistryRecords drops a deleted key's registry records, version first,
// so a failure leaves at worst an owner record for a missing key. A failure is
// returned; a retried DeleteKey resumes it (resumeRegistryCleanup).
func (kb *XKMSBackend) deleteRegistryRecords(ctx context.Context, keyID, name string) error {
	reg := kb.keyRegistry()
	if reg == nil {
		return nil
	}
	if err := reg.Delete(ctx, kb.versionRecordKey(keyID)); err != nil && !qberrors.IsNotFound(err) {
		return fmt.Errorf("xkms key registry: delete version record of %q: %w", keyID, err)
	}
	if name != keyID {
		if err := reg.Delete(ctx, kb.ownerRecordKey(name)); err != nil && !qberrors.IsNotFound(err) {
			return fmt.Errorf("xkms key registry: delete owner record of %q: %w", keyID, err)
		}
	}
	return nil
}

// resumeRegistryCleanup completes the deletion of a key whose base key material
// is already gone: an earlier DeleteKey deleted the material and then failed to
// drop its registry records. With records left it drops the cache and the
// records, and the deletion succeeds; with none the key does not exist.
func (kb *XKMSBackend) resumeRegistryCleanup(ctx context.Context, keyID string) error {
	reg := kb.keyRegistry()
	if reg == nil {
		kb.dropCachedMetadata(keyID)
		return ErrKeyNotFound
	}
	name, err := xkmsKeyName(keyID)
	if err != nil {
		return err
	}
	_, leftover, err := readRegistryRecord(ctx, reg, kb.versionRecordKey(keyID))
	if err != nil {
		return err
	}
	if !leftover && name != keyID {
		_, leftover, err = readRegistryRecord(ctx, reg, kb.ownerRecordKey(name))
		if err != nil {
			return err
		}
	}
	kb.dropCachedMetadata(keyID)
	if !leftover {
		return ErrKeyNotFound
	}
	return kb.deleteRegistryRecords(ctx, keyID, name)
}

// versionKeyDeleteAttributes builds the go-xkms attributes DeleteKey uses to
// delete a rotated version key: the key's algorithm when it is known, the
// default AES-256-GCM attributes otherwise.
// formatKeyID returns the key ID as stored.
// Keys are stored using attrs.CN during generation, so we should use the same ID for lookup.
func (kb *XKMSBackend) formatKeyID(keyID string) (string, error) {
	// The go-xkms key name for keyID: it matches the attrs.CN used during
	// key generation. Do NOT add any other prefix, or key lookup fails.
	return xkmsKeyName(keyID)
}

// xkmsEncodedKeyPrefix marks a go-xkms key name that hex-encodes a quicbaas
// key ID go-xkms would refuse as a name (a tenant-scoped ID carries "/").
const xkmsEncodedKeyPrefix = "qbt."

// xkmsMaxKeyNameLen is go-xkms's limit on a key name (ValidateKeyID).
const xkmsMaxKeyNameLen = 255

// xkmsKeyName maps a quicbaas key ID to the go-xkms key name it is stored
// under. A key ID without "/" is its own name, legacy names starting with
// "qbt." included, except that one which is "qbt." + lowercase hex decoding
// to a string with "/" is refused: it would alias that encoded tenant key. A
// key ID with "/" becomes "qbt." + the lowercase hex of the ID; one whose
// encoding exceeds go-xkms's 255-byte limit is refused.
func xkmsKeyName(keyID string) (string, error) {
	if !strings.Contains(keyID, "/") {
		// A "/"-free ID is stored under its own name, including a legacy
		// name that starts with the prefix, unless the name would read back
		// as an encoded tenant-scoped ID (it would alias that key).
		if _, aliases := decodeXKMSKeyName(keyID); aliases {
			return "", fmt.Errorf("%w: key ID %q is the encoded name of a tenant-scoped key ID",
				ErrInvalidKeyID, keyID)
		}
		return keyID, nil
	}
	name := xkmsEncodedKeyPrefix + hex.EncodeToString([]byte(keyID))
	if len(name) > xkmsMaxKeyNameLen {
		return "", fmt.Errorf("%w: key ID %q is too long for the xkms backend (%d-byte encoded name, max %d)",
			ErrInvalidKeyID, keyID, len(name), xkmsMaxKeyNameLen)
	}
	return name, nil
}

// decodeXKMSKeyName decodes a go-xkms key name that xkmsKeyName encoded from
// a tenant-scoped ID: the prefix followed by lowercase hex that decodes to a
// string containing "/". It returns false for every other name.
func decodeXKMSKeyName(name string) (string, bool) {
	encoded, ok := strings.CutPrefix(name, xkmsEncodedKeyPrefix)
	if !ok || encoded == "" || encoded != strings.ToLower(encoded) {
		return "", false
	}
	raw, err := hex.DecodeString(encoded)
	if err != nil || !strings.Contains(string(raw), "/") {
		return "", false
	}
	return string(raw), true
}

// quicbaasKeyIDFromXKMSName reverses xkmsKeyName for a name go-xkms lists.
// Only a name that is "qbt." + lowercase hex decoding to a string with "/"
// is decoded; every other name, a legacy "qbt." name included, is returned
// unchanged.
func quicbaasKeyIDFromXKMSName(name string) string {
	if keyID, ok := decodeXKMSKeyName(name); ok {
		return keyID
	}
	return name
}

// buildFullKeyID constructs the full key ID format expected by go-xkms for crypto operations.
// Format: "backend:type:algo:keyname" (e.g., "software:encryption:aes256-gcm:my-key").
func (kb *XKMSBackend) buildFullKeyID(keyID string, algorithm string) (string, error) {
	name, err := xkmsKeyName(keyID)
	if err != nil {
		return "", err
	}
	// Map algorithm to go-xkms format
	algoFormat := kb.algorithmToKeyIDFormat(algorithm)
	return fmt.Sprintf("%s:encryption:%s:%s", kb.backendType, algoFormat, name), nil
}

// buildSigningKeyID constructs the full key ID format for signing operations.
// Format: "backend:signing:algo:keyname" (e.g., "software:signing:rsa:my-key").
func (kb *XKMSBackend) buildSigningKeyID(keyID string, algorithm string) (string, error) {
	name, err := xkmsKeyName(keyID)
	if err != nil {
		return "", err
	}
	// Map algorithm to go-xkms signing format
	algoFormat := kb.signingAlgorithmToKeyIDFormat(algorithm)
	return fmt.Sprintf("%s:signing:%s:%s", kb.backendType, algoFormat, name), nil
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

	// GetKeyMetadata returns the shared cached pointer, which RotateKey
	// mutates under kb.mu. Copy the version and algorithm once under the
	// read lock so the key chosen and the version serialized into the blob
	// always agree.
	kb.mu.RLock()
	version := meta.Version
	algorithm := meta.Algorithm
	kb.mu.RUnlock()

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
	kid, err := kb.buildFullKeyID(actualKeyID, algorithm)
	if err != nil {
		return nil, err
	}

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
	encrypted.Metadata[keyVersionMetadataKey] = []byte(fmt.Sprintf("%d", version))

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

	// Read key version from encrypted data metadata. A blob without one
	// (legacy data) is version 1; one that is not a positive integer is refused,
	// so no relabelled version can select a key outside keyID's versions.
	parsedVersion, err := encryptedKeyVersion(encrypted)
	if err != nil {
		return nil, api.NewCiphertextDeserializationError("XKMSBackend.Decrypt", err)
	}
	version := conv.ClampUint64ToInt(parsedVersion)

	// GetKeyMetadata returns the shared cached pointer, which RotateKey
	// mutates under kb.mu: copy the version and algorithm once under the
	// read lock, as Encrypt does.
	kb.mu.RLock()
	cachedVersion := meta.Version
	algorithm := meta.Algorithm
	kb.mu.RUnlock()

	if version > cachedVersion {
		// Above the cached version: accept it only inside keyID's stored
		// versions (this node's cache may be stale), never an independent
		// key that happens to be named keyID_v<N>.
		current, err := kb.storedVersion(ctx, keyID)
		if err != nil {
			return nil, api.NewKeyMetadataRetrievalError("XKMSBackend.Decrypt", err)
		}
		if version > current {
			return nil, api.NewCiphertextDeserializationError("XKMSBackend.Decrypt", ErrInvalidCiphertextKeyVersion)
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
	kid, err := kb.buildFullKeyID(actualKeyID, algorithm)
	if err != nil {
		return nil, err
	}

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
	kid, err := kb.buildSigningKeyID(keyID, meta.Algorithm)
	if err != nil {
		return nil, err
	}

	// Get the signer directly from go-xkms
	// This avoids the xkms.Sign() helper which always hashes the input,
	// causing double-hashing when digest is already a hash (e.g., from x509.CreateCertificate)
	signer, err := xkms.SignerByID(ctx, kid)
	if err != nil {
		return nil, api.NewSignerRetrievalError("XKMSBackend.Sign", err)
	}

	// Map algorithm to hash for SignerOpts
	hash := kb.algorithmToHash(algorithm)

	// Ed25519 signs the message itself, and crypto/ed25519 REFUSES any
	// SignerOpts whose hash is not crypto.Hash(0). The key decides this, not
	// the caller's requested digest algorithm — which is why it is read from
	// the key's own metadata rather than from the algorithm argument.
	if kb.isEd25519Algorithm(meta.Algorithm) {
		hash = crypto.Hash(0)
	}

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
	kid, err := kb.buildSigningKeyID(keyID, meta.Algorithm)
	if err != nil {
		return false, err
	}

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
	kid, err := kb.buildSigningKeyID(keyID, meta.Algorithm)
	if err != nil {
		return nil, err
	}

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
func (kb *XKMSBackend) algorithmToKeyAttributes(keyID, algorithm string) (*kctypes.KeyAttributes, error) {
	name, err := xkmsKeyName(keyID)
	if err != nil {
		return nil, err
	}
	attrs := &kctypes.KeyAttributes{
		CN:        name,
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

	return attrs, nil
}

// isEd25519Algorithm reports whether the algorithm names an Ed25519 key.
//
// Ed25519 is the only algorithm here that signs a message rather than a
// digest, so it is the only one that needs identifying by name at signing time.
func (kb *XKMSBackend) isEd25519Algorithm(algorithm string) bool {
	return strings.Contains(strings.ToUpper(algorithm), "ED25519")
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
