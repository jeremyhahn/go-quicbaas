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
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	types "github.com/jeremyhahn/go-xkms/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Storage path constants for tenant DEKs.
const (
	tenantDEKCurrentPrefix  = "_internal/secrets/dek/tenant/"
	tenantDEKVersionsPrefix = "_internal/secrets/dek/versions/dek:tenant:"

	defaultTenantDEKAlgorithm = "AES-256-GCM"
	defaultTenantDEKKeyBytes  = 32 // 256 bits

	// TenantDEKMasterKeyID is the default KMS key CN used to wrap tenant DEKs.
	// This key-encryption key (KEK) must exist in the configured KMS backend.
	TenantDEKMasterKeyID = "quicbaas-tenant-dek-kek"
)

// Sentinel errors for tenant DEK operations.
var (
	// ErrStaleTenantDEK indicates that a persisted tenant DEK ciphertext
	// cannot be unwrapped with the currently configured KEK. This typically
	// means the KMS key-encryption key was rotated or regenerated while the
	// wrapped DEK on disk was created under a previous KEK. Operator action
	// is required to repair the state — either restore the prior KEK,
	// rewrap the DEK, or deprovision the tenant DEK so that a fresh one is
	// generated on next use.
	ErrStaleTenantDEK = errors.New("tenant DEK ciphertext cannot be unwrapped by the current KEK")
)

// StaleTenantDEKError is a typed error returned when a stored tenant DEK
// cannot be unwrapped by the active KEK. It carries enough context for
// operators to diagnose and repair the mismatch. The Cause field holds the
// underlying decryption error from the KMS backend for forensic logging.
type StaleTenantDEKError struct {
	TenantID string
	KEKID    string
	Cause    error
}

// Error implements the error interface. The message includes a repair hint
// so the operator receives actionable guidance rather than a raw crypto
// failure.
func (e *StaleTenantDEKError) Error() string {
	return fmt.Sprintf(
		"stale tenant DEK for %q: wrapped key on disk cannot be decrypted by KEK %q (cause: %v); repair by restoring the prior KEK, rewrapping the DEK, or deprovisioning the tenant DEK",
		e.TenantID, e.KEKID, e.Cause,
	)
}

// Unwrap returns the underlying cause so errors.Is / errors.As can walk the
// chain to the original KMS failure.
func (e *StaleTenantDEKError) Unwrap() error {
	return e.Cause
}

// Is reports whether the target matches this error. Matches ErrStaleTenantDEK
// so callers can assert with errors.Is(err, ErrStaleTenantDEK).
func (e *StaleTenantDEKError) Is(target error) bool {
	return target == ErrStaleTenantDEK
}

// aeadAuthFailureMarker is the canonical substring emitted by Go's
// crypto/cipher AEAD implementations when ciphertext authentication fails.
// Matching by substring is stable across Go versions (the error text is part
// of stdlib's public contract since at least Go 1.0). We cannot use errors.Is
// because stdlib's errOpen is unexported.
const aeadAuthFailureMarker = "message authentication failed"

// isAEADAuthFailure reports whether err is caused by an AEAD authentication
// tag mismatch. Such failures indicate ciphertext tampering, wrong key,
// wrong AAD, or wrong nonce — from the tenant DEK's perspective the
// overwhelming common cause is a KEK mismatch (rotated or regenerated KEK
// applied against a DEK wrapped under the prior KEK).
func isAEADAuthFailure(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), aeadAuthFailureMarker)
}

// dekEntry is the on-disk representation of a tenant DEK.
type dekEntry struct {
	DEKID         string    `json:"dek_id"`
	WrappedKey    []byte    `json:"wrapped_key"`
	Algorithm     string    `json:"algorithm"`
	Version       uint64    `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	LastRotatedAt time.Time `json:"last_rotated_at"`
}

// tenantDEKManager is the concrete implementation of TenantDEKManager.
// Storage is protected by mu; the cache uses sync.Map for lock-free reads.
type tenantDEKManager struct {
	kms         interfaces.KMSManager
	store       interfaces.KVStore
	cache       sync.Map // tenantID -> []byte (plaintext DEK copy)
	masterKeyID string
	algorithm   string
	keyBytes    int
	logger      interfaces.Logger
	mu          sync.RWMutex
}

// NewTenantDEKManager creates a TenantDEKManager backed by go-xkms and a KVStore.
// masterKeyID is the go-xkms key reference used to wrap/unwrap tenant DEKs.
func NewTenantDEKManager(kms interfaces.KMSManager, store interfaces.KVStore, masterKeyID string, logger interfaces.Logger) TenantDEKManager {
	return &tenantDEKManager{
		kms:         kms,
		store:       store,
		masterKeyID: masterKeyID,
		algorithm:   defaultTenantDEKAlgorithm,
		keyBytes:    defaultTenantDEKKeyBytes,
		logger:      logger,
	}
}

// currentStorageKey returns the storage path for the current DEK of a tenant.
func currentStorageKey(tenantID string) string {
	return tenantDEKCurrentPrefix + tenantID
}

// versionedStorageKey returns the storage path for a specific DEK version.
func versionedStorageKey(tenantID string, version uint64) string {
	return fmt.Sprintf("%s%s/v%d", tenantDEKVersionsPrefix, tenantID, version)
}

// GetOrCreateTenantDEK returns the current DEK for tenantID, generating one if
// it does not yet exist. The returned slice is a defensive copy; the caller may
// zero it after use.
func (m *tenantDEKManager) GetOrCreateTenantDEK(ctx context.Context, tenantID string) ([]byte, error) {
	// Fast path: cache hit.
	if cached, ok := m.cache.Load(tenantID); ok {
		src := cached.([]byte)
		out := make([]byte, len(src))
		copy(out, src)
		return out, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring the write lock.
	if cached, ok := m.cache.Load(tenantID); ok {
		src := cached.([]byte)
		out := make([]byte, len(src))
		copy(out, src)
		return out, nil
	}

	// Storage check. A missing entry manifests either as a not-found error
	// from the store or as a nil/empty byte slice — both paths fall through
	// to generateAndStore. This matters because the Decrypt call is an AEAD
	// operation and would fail with a misleading "message authentication
	// failed" if we handed it an empty ciphertext.
	key := currentStorageKey(tenantID)
	data, err := m.store.Get(ctx, key)
	if err == nil && len(data) > 0 {
		// Entry exists — unwrap and cache.
		return m.unwrapAndCache(ctx, tenantID, data)
	}

	// Generate a new DEK.
	return m.generateAndStore(ctx, tenantID, key)
}

// GetTenantDEK returns the current DEK for tenantID.
// Returns ErrTenantDEKNotFound if no DEK has been provisioned.
func (m *tenantDEKManager) GetTenantDEK(ctx context.Context, tenantID string) ([]byte, error) {
	// Cache hit.
	if cached, ok := m.cache.Load(tenantID); ok {
		src := cached.([]byte)
		out := make([]byte, len(src))
		copy(out, src)
		return out, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	key := currentStorageKey(tenantID)
	data, err := m.store.Get(ctx, key)
	if err != nil || data == nil {
		return nil, fmt.Errorf("security.tenantDEKManager.GetTenantDEK: %w", ErrTenantDEKNotFound)
	}

	return m.unwrapAndCache(ctx, tenantID, data)
}

// RotateTenantDEK archives the current DEK version and generates a new one.
func (m *tenantDEKManager) RotateTenantDEK(ctx context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load current entry.
	key := currentStorageKey(tenantID)
	data, err := m.store.Get(ctx, key)
	if err != nil || data == nil {
		return fmt.Errorf("security.tenantDEKManager.RotateTenantDEK: %w", ErrTenantDEKNotFound)
	}

	var entry dekEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return fmt.Errorf("security.tenantDEKManager.RotateTenantDEK: unmarshal: %w", err)
	}

	// Archive the current version.
	versionedKey := versionedStorageKey(tenantID, entry.Version)
	if storeErr := m.store.Put(ctx, versionedKey, data); storeErr != nil {
		return fmt.Errorf("security.tenantDEKManager.RotateTenantDEK: archive version %d: %w", entry.Version, storeErr)
	}

	// Generate replacement DEK.
	kekAttrs := &types.KeyAttributes{CN: m.masterKeyID}
	plaintext, wrapped, err := m.kms.GenerateDataKey(ctx, kekAttrs, m.keyBytes)
	if err != nil {
		return fmt.Errorf("security.tenantDEKManager.RotateTenantDEK: generate data key: %w", err)
	}
	defer zeroBytes(plaintext)

	now := time.Now()
	newEntry := dekEntry{
		DEKID:         "dek:tenant:" + tenantID,
		WrappedKey:    wrapped,
		Algorithm:     m.algorithm,
		Version:       entry.Version + 1,
		CreatedAt:     entry.CreatedAt,
		LastRotatedAt: now,
	}

	newData, err := json.Marshal(newEntry)
	if err != nil {
		return fmt.Errorf("security.tenantDEKManager.RotateTenantDEK: marshal: %w", err)
	}

	if storeErr := m.store.Put(ctx, key, newData); storeErr != nil {
		return fmt.Errorf("security.tenantDEKManager.RotateTenantDEK: store: %w", storeErr)
	}

	// Invalidate the cache; next GetTenantDEK will re-derive from storage.
	m.cache.Delete(tenantID)

	m.logger.WithFields(map[string]interface{}{
		"tenant_id":   tenantID,
		"old_version": entry.Version,
		"new_version": newEntry.Version,
	}).Info("tenant DEK rotated")
	return nil
}

// DeleteTenantDEK removes the current DEK for tenantID from storage and cache.
// The wrapped key is deleted from storage; no plaintext is ever persisted.
// This constitutes cryptographic shredding: without the wrapped key, the
// tenant's previously encrypted data is permanently inaccessible.
func (m *tenantDEKManager) DeleteTenantDEK(ctx context.Context, tenantID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := currentStorageKey(tenantID)
	if err := m.store.Delete(ctx, key); err != nil {
		// Treat not-found as success (idempotent delete).
		if !isNotFound(err) {
			return fmt.Errorf("security.tenantDEKManager.DeleteTenantDEK: %w", err)
		}
	}

	m.cache.Delete(tenantID)

	m.logger.WithField("tenant_id", tenantID).Info("tenant DEK deleted")
	return nil
}

// GetTenantDEKStatus returns metadata about a tenant's DEK without key material.
func (m *tenantDEKManager) GetTenantDEKStatus(ctx context.Context, tenantID string) (*TenantDEKStatus, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := currentStorageKey(tenantID)
	data, err := m.store.Get(ctx, key)
	if err != nil || data == nil {
		return nil, fmt.Errorf("security.tenantDEKManager.GetTenantDEKStatus: %w", ErrTenantDEKNotFound)
	}

	var entry dekEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("security.tenantDEKManager.GetTenantDEKStatus: unmarshal: %w", err)
	}

	return &TenantDEKStatus{
		TenantID:  tenantID,
		Version:   entry.Version,
		Algorithm: entry.Algorithm,
		KeyBytes:  m.keyBytes,
		CreatedAt: entry.CreatedAt,
		RotatedAt: entry.LastRotatedAt,
	}, nil
}

// ListTenantDEKs returns the tenant IDs for which a current DEK exists.
func (m *tenantDEKManager) ListTenantDEKs(ctx context.Context) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	keys, err := m.store.List(ctx, tenantDEKCurrentPrefix)
	if err != nil {
		return nil, fmt.Errorf("security.tenantDEKManager.ListTenantDEKs: %w", err)
	}

	tenantIDs := make([]string, 0, len(keys))
	for _, k := range keys {
		tenantID := strings.TrimPrefix(k, tenantDEKCurrentPrefix)
		if tenantID != "" {
			tenantIDs = append(tenantIDs, tenantID)
		}
	}
	return tenantIDs, nil
}

// Close zeros all plaintext DEKs held in the cache.
func (m *tenantDEKManager) Close() error {
	m.cache.Range(func(_, value any) bool {
		zeroBytes(value.([]byte))
		return true
	})
	m.cache = sync.Map{}
	return nil
}

// unwrapAndCache decrypts the wrapped key inside data and stores a copy in the
// cache. Must be called with at least a read lock already held.
// Returns a fresh defensive copy of the plaintext.
func (m *tenantDEKManager) unwrapAndCache(ctx context.Context, tenantID string, data []byte) ([]byte, error) {
	var entry dekEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, fmt.Errorf("security.tenantDEKManager: unmarshal entry for %q: %w", tenantID, err)
	}

	plaintext, err := m.kms.Decrypt(ctx, m.masterKeyID, entry.WrappedKey, nil)
	if err != nil {
		// An AEAD authentication failure almost always means the KEK has
		// changed since the DEK was wrapped (key store rebuilt, KEK rotated
		// without rewrap, or ciphertext corrupted). Surface it as a typed
		// error so callers and operators can branch on ErrStaleTenantDEK
		// rather than string-matching the underlying crypto message.
		if isAEADAuthFailure(err) {
			m.logger.WithFields(map[string]interface{}{
				"tenant_id":     tenantID,
				"master_key_id": m.masterKeyID,
				"dek_version":   entry.Version,
			}).Error("tenant DEK unwrap failed: KEK mismatch — wrapped DEK cannot be decrypted by current master key")
			return nil, &StaleTenantDEKError{
				TenantID: tenantID,
				KEKID:    m.masterKeyID,
				Cause:    err,
			}
		}
		return nil, fmt.Errorf("security.tenantDEKManager: unwrap DEK for %q: %w", tenantID, err)
	}

	// Store a copy in the cache; zero the local slice after the copy.
	cached := make([]byte, len(plaintext))
	copy(cached, plaintext)
	zeroBytes(plaintext)
	m.cache.Store(tenantID, cached)

	// Return a separate defensive copy to the caller.
	out := make([]byte, len(cached))
	copy(out, cached)
	return out, nil
}

// generateAndStore creates a new DEK via KMS, persists the wrapped form, caches
// the plaintext, and returns a defensive copy to the caller. Must be called
// with the write lock held.
func (m *tenantDEKManager) generateAndStore(ctx context.Context, tenantID, storageKey string) ([]byte, error) {
	kekAttrs := &types.KeyAttributes{CN: m.masterKeyID}
	plaintext, wrapped, err := m.kms.GenerateDataKey(ctx, kekAttrs, m.keyBytes)
	if err != nil {
		return nil, fmt.Errorf("security.tenantDEKManager: generate DEK for %q: %w", tenantID, err)
	}
	defer zeroBytes(plaintext)

	now := time.Now()
	entry := dekEntry{
		DEKID:         "dek:tenant:" + tenantID,
		WrappedKey:    wrapped,
		Algorithm:     m.algorithm,
		Version:       1,
		CreatedAt:     now,
		LastRotatedAt: now,
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return nil, fmt.Errorf("security.tenantDEKManager: marshal entry for %q: %w", tenantID, err)
	}

	if storeErr := m.store.Put(ctx, storageKey, data); storeErr != nil {
		return nil, fmt.Errorf("security.tenantDEKManager: store DEK for %q: %w", tenantID, storeErr)
	}

	// Cache a copy.
	cached := make([]byte, len(plaintext))
	copy(cached, plaintext)
	m.cache.Store(tenantID, cached)

	// Return a separate defensive copy to the caller.
	out := make([]byte, len(cached))
	copy(out, cached)

	m.logger.WithField("tenant_id", tenantID).Info("tenant DEK created")
	return out, nil
}

// zeroBytes overwrites b with zeroes to remove key material from memory.
func zeroBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// isNotFound returns true when err signals a missing key from any storage layer.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	var de *dberrors.QBError
	if errors.As(err, &de) {
		return de.Code == dberrors.ErrNotFound
	}
	return false
}

// ----------------------------------------------------------------------------
// tenantDEKProvisioner — iam.TenantDEKProvisioner adapter
// ----------------------------------------------------------------------------

// tenantDEKProvisioner adapts TenantDEKManager to the iam.TenantDEKProvisioner
// interface so it can be wired into iam.DefaultTenantManager.
type tenantDEKProvisioner struct {
	dekManager TenantDEKManager
}

// NewTenantDEKProvisioner wraps a TenantDEKManager as a TenantDEKProvisioner.
// The returned value satisfies the iam.TenantDEKProvisioner interface.
func NewTenantDEKProvisioner(dekManager TenantDEKManager) *tenantDEKProvisioner {
	return &tenantDEKProvisioner{dekManager: dekManager}
}

// ProvisionDEK creates a DEK for tenantID. Safe to call for existing tenants.
func (p *tenantDEKProvisioner) ProvisionDEK(ctx context.Context, tenantID string) error {
	_, err := p.dekManager.GetOrCreateTenantDEK(ctx, tenantID)
	return err
}

// DeprovisionDEK removes the DEK for tenantID. Safe to call for unknown tenants.
func (p *tenantDEKProvisioner) DeprovisionDEK(ctx context.Context, tenantID string) error {
	return p.dekManager.DeleteTenantDEK(ctx, tenantID)
}
