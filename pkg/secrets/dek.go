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

// Package secrets provides DEK (Data Encryption Key) management for secrets encryption.
package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	types "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// DEK errors.
var (
	// ErrDEKNotFound indicates the requested DEK does not exist.
	ErrDEKNotFound = errors.New("secrets: DEK not found")

	// ErrDEKVersionNotFound indicates the requested DEK version does not exist.
	ErrDEKVersionNotFound = errors.New("secrets: DEK version not found")

	// ErrDEKAlreadyExists indicates the DEK already exists.
	ErrDEKAlreadyExists = errors.New("secrets: DEK already exists")

	// ErrDEKInitializationFailed indicates DEK initialization failed.
	ErrDEKInitializationFailed = errors.New("secrets: DEK initialization failed")

	// ErrDEKUnwrapFailed indicates DEK unwrapping (decryption) failed.
	ErrDEKUnwrapFailed = errors.New("secrets: DEK unwrap failed")

	// ErrDEKRotationFailed indicates DEK rotation failed.
	ErrDEKRotationFailed = errors.New("secrets: DEK rotation failed")
)

const (
	// Storage paths for DEKs.
	dekStoragePrefix      = "_internal/secrets/dek/"
	globalDEKPath         = dekStoragePrefix + "global"
	tenantDEKPathPrefix   = dekStoragePrefix + "tenant/"
	databaseDEKPathPrefix = dekStoragePrefix + "database/"
	dekVersionsPrefix     = dekStoragePrefix + "versions/"
	dekCurrentVersionKey  = "/current"
	defaultDEKAlgorithm   = "AES-256-GCM"
	defaultDEKKeyBytes    = 32 // 256 bits
	defaultCacheTTL       = 5 * time.Minute
	defaultCacheMaxSize   = 100
	cacheCleanupInterval  = 1 * time.Minute
	defaultRetainVersions = 3 // Number of old DEK versions to retain
)

// DEKEntry represents a stored DEK entry.
type DEKEntry struct {
	DEKID         string    `json:"dekid"`
	WrappedKey    []byte    `json:"wrapped_key"` // DEK encrypted with master key
	Algorithm     string    `json:"algorithm"`   // AES-256-GCM default
	Version       uint64    `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	LastRotatedAt time.Time `json:"last_rotated_at"`
}

// DEKStatus contains status information about a DEK.
type DEKStatus struct {
	DEKID         DEKID     `json:"dekid"`
	Algorithm     string    `json:"algorithm"`
	Version       uint64    `json:"version"`
	CreatedAt     time.Time `json:"created_at"`
	LastRotatedAt time.Time `json:"last_rotated_at"`
}

// DEKVersionEntry represents a stored DEK version for multi-version storage.
type DEKVersionEntry struct {
	DEKID      string    `json:"dekid"`
	Version    uint64    `json:"version"`
	WrappedKey []byte    `json:"wrapped_key"`
	Algorithm  string    `json:"algorithm"`
	CreatedAt  time.Time `json:"created_at"`
}

// DEKVersionInfo contains metadata about a DEK version (without key material).
type DEKVersionInfo struct {
	Version   uint64    `json:"version"`
	Algorithm string    `json:"algorithm"`
	CreatedAt time.Time `json:"created_at"`
	IsCurrent bool      `json:"is_current"`
}

// dekCacheEntry represents a cached DEK entry.
type dekCacheEntry struct {
	keyMaterial []byte
	version     uint64
	expiry      time.Time
}

// dekCacheKey represents a composite cache key for versioned DEKs.
type dekCacheKey struct {
	dekid   DEKID
	version uint64 // 0 means "current version"
}

// dekCache provides in-memory caching for unwrapped DEKs.
type dekCache struct {
	entries        map[DEKID]*dekCacheEntry       // Current version cache
	versionedCache map[dekCacheKey]*dekCacheEntry // Versioned cache
	ttl            time.Duration
	maxSize        int
	stopChan       chan struct{}
}

// newDEKCache creates a new DEK cache.
func newDEKCache() *dekCache {
	cache := &dekCache{
		entries:        make(map[DEKID]*dekCacheEntry),
		versionedCache: make(map[dekCacheKey]*dekCacheEntry),
		ttl:            defaultCacheTTL,
		maxSize:        defaultCacheMaxSize,
		stopChan:       make(chan struct{}),
	}

	// Start cleanup goroutine
	go cache.cleanupExpired()

	return cache
}

// get retrieves a cached DEK entry if it exists and is not expired.
func (c *dekCache) get(dekid DEKID) ([]byte, uint64, bool) {
	entry, ok := c.entries[dekid]
	if !ok {
		return nil, 0, false
	}

	// Check expiration
	if time.Now().After(entry.expiry) {
		delete(c.entries, dekid)
		return nil, 0, false
	}

	// Return a copy to prevent modification
	keyMaterial := make([]byte, len(entry.keyMaterial))
	copy(keyMaterial, entry.keyMaterial)

	return keyMaterial, entry.version, true
}

// getVersion retrieves a specific version of a DEK from the versioned cache.
func (c *dekCache) getVersion(dekid DEKID, version uint64) ([]byte, bool) {
	key := dekCacheKey{dekid: dekid, version: version}
	entry, ok := c.versionedCache[key]
	if !ok {
		return nil, false
	}

	// Check expiration
	if time.Now().After(entry.expiry) {
		delete(c.versionedCache, key)
		return nil, false
	}

	// Return a copy to prevent modification
	keyMaterial := make([]byte, len(entry.keyMaterial))
	copy(keyMaterial, entry.keyMaterial)

	return keyMaterial, true
}

// set stores a DEK entry in the cache.
// Implements LRU eviction when cache is full.
func (c *dekCache) set(dekid DEKID, keyMaterial []byte, version uint64) {
	// Evict oldest entry if cache is full
	if len(c.entries) >= c.maxSize {
		var oldestDEKID DEKID
		var oldestTime time.Time

		for id, entry := range c.entries {
			if oldestTime.IsZero() || entry.expiry.Before(oldestTime) {
				oldestDEKID = id
				oldestTime = entry.expiry
			}
		}

		if oldestDEKID != "" {
			delete(c.entries, oldestDEKID)
		}
	}

	// Store a copy to prevent modification
	keyCopy := make([]byte, len(keyMaterial))
	copy(keyCopy, keyMaterial)

	c.entries[dekid] = &dekCacheEntry{
		keyMaterial: keyCopy,
		version:     version,
		expiry:      time.Now().Add(c.ttl),
	}
}

// setVersion stores a specific version of a DEK in the versioned cache.
func (c *dekCache) setVersion(dekid DEKID, version uint64, keyMaterial []byte) {
	// Evict oldest entry if versioned cache is full
	if len(c.versionedCache) >= c.maxSize {
		var oldestKey dekCacheKey
		var oldestTime time.Time

		for key, entry := range c.versionedCache {
			if oldestTime.IsZero() || entry.expiry.Before(oldestTime) {
				oldestKey = key
				oldestTime = entry.expiry
			}
		}

		if oldestKey.dekid != "" {
			delete(c.versionedCache, oldestKey)
		}
	}

	// Store a copy to prevent modification
	keyCopy := make([]byte, len(keyMaterial))
	copy(keyCopy, keyMaterial)

	key := dekCacheKey{dekid: dekid, version: version}
	c.versionedCache[key] = &dekCacheEntry{
		keyMaterial: keyCopy,
		version:     version,
		expiry:      time.Now().Add(c.ttl),
	}
}

// delete removes a DEK entry from the cache.
func (c *dekCache) delete(dekid DEKID) {
	delete(c.entries, dekid)
}

// deleteVersion removes a specific version of a DEK from the versioned cache.
func (c *dekCache) deleteVersion(dekid DEKID, version uint64) {
	key := dekCacheKey{dekid: dekid, version: version}
	delete(c.versionedCache, key)
}

// deleteAllVersions removes all versions of a DEK from the versioned cache.
func (c *dekCache) deleteAllVersions(dekid DEKID) {
	for key := range c.versionedCache {
		if key.dekid == dekid {
			delete(c.versionedCache, key)
		}
	}
}

// clear removes all entries from the cache.
func (c *dekCache) clear() {
	c.entries = make(map[DEKID]*dekCacheEntry)
	c.versionedCache = make(map[dekCacheKey]*dekCacheEntry)
}

// cleanupExpired periodically removes expired entries.
func (c *dekCache) cleanupExpired() {
	ticker := time.NewTicker(cacheCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			now := time.Now()
			// Cleanup current version cache
			for dekid, entry := range c.entries {
				if now.After(entry.expiry) {
					delete(c.entries, dekid)
				}
			}
			// Cleanup versioned cache
			for key, entry := range c.versionedCache {
				if now.After(entry.expiry) {
					delete(c.versionedCache, key)
				}
			}
		case <-c.stopChan:
			return
		}
	}
}

// stop stops the cleanup goroutine.
func (c *dekCache) stop() {
	close(c.stopChan)
}

// SecretsDEK manages Data Encryption Keys (DEKs) for secrets encryption.
type SecretsDEK struct {
	kms         interfaces.KMSManager
	store       interfaces.KVStore
	cache       *dekCache
	masterKeyID string
	logger      interfaces.Logger
	mu          sync.RWMutex
}

// NewSecretsDEK creates a new SecretsDEK instance.
func NewSecretsDEK(kms interfaces.KMSManager, store interfaces.KVStore, masterKeyID string, logger interfaces.Logger) *SecretsDEK {
	return &SecretsDEK{
		kms:         kms,
		store:       store,
		cache:       newDEKCache(),
		masterKeyID: masterKeyID,
		logger:      logger,
	}
}

// Initialize creates the global DEK if it doesn't exist.
// This method is idempotent - it will not fail if the DEK already exists.
func (d *SecretsDEK) Initialize(ctx context.Context, algorithm string) error {
	if algorithm == "" {
		algorithm = defaultDEKAlgorithm
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if global DEK already exists
	key := globalDEKPath
	existing, err := d.store.Get(ctx, key)
	if err == nil && existing != nil {
		// DEK already exists, this is OK (idempotent)
		d.logger.Debug("Global DEK already initialized")
		return nil
	}

	// Generate new DEK
	kekAttrs := &types.KeyAttributes{CN: d.masterKeyID}
	plaintext, wrapped, err := d.kms.GenerateDataKey(ctx, kekAttrs, defaultDEKKeyBytes)
	if err != nil {
		d.logger.WithError(err).Error("Failed to generate global DEK")
		return api.NewSentinelWrapError("secrets.dek.Initialize", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	// Clear plaintext from memory when done
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()

	// Create DEK entry
	now := time.Now()
	entry := &DEKEntry{
		DEKID:         string(DEKIDGlobal),
		WrappedKey:    wrapped,
		Algorithm:     algorithm,
		Version:       1,
		CreatedAt:     now,
		LastRotatedAt: now,
	}

	// Marshal and store
	data, err := json.Marshal(entry)
	if err != nil {
		d.logger.WithError(err).Error("Failed to marshal global DEK entry")
		return api.NewSentinelWrapError("secrets.dek.Initialize", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	if err := d.store.Put(ctx, key, data); err != nil {
		d.logger.WithError(err).Error("Failed to store global DEK")
		return api.NewSentinelWrapError("secrets.dek.Initialize", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	d.logger.Info("Global DEK initialized successfully")
	return nil
}

// GetKey retrieves key material for a DEKID (from cache or storage).
// Returns the unwrapped key material and version.
func (d *SecretsDEK) GetKey(ctx context.Context, dekid DEKID) ([]byte, uint64, error) {
	d.mu.RLock()

	// Check cache first
	if keyMaterial, version, found := d.cache.get(dekid); found {
		d.mu.RUnlock()
		return keyMaterial, version, nil
	}

	d.mu.RUnlock()

	// Not in cache, load from storage
	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check cache after acquiring write lock
	if keyMaterial, version, found := d.cache.get(dekid); found {
		return keyMaterial, version, nil
	}

	// Load from storage
	entry, err := d.loadDEKEntry(ctx, dekid)
	if err != nil {
		return nil, 0, err
	}

	// Unwrap the key
	plaintext, err := d.kms.Decrypt(ctx, d.masterKeyID, entry.WrappedKey, nil)
	if err != nil {
		d.logger.WithError(err).WithField("dekid", dekid).Error("Failed to unwrap DEK")
		return nil, 0, api.NewSentinelWrapError("secrets.dek.GetKey", dberrors.ErrDecryption, ErrDEKUnwrapFailed, err)
	}

	// Cache the unwrapped key
	d.cache.set(dekid, plaintext, entry.Version)

	// Return a copy
	keyMaterial := make([]byte, len(plaintext))
	copy(keyMaterial, plaintext)

	return keyMaterial, entry.Version, nil
}

// EnsureTenantDEK creates a tenant DEK if it doesn't exist, returns the DEKID.
// This method is idempotent - it will return the existing DEKID if it already exists.
func (d *SecretsDEK) EnsureTenantDEK(ctx context.Context, tenantID string, algorithm string) (DEKID, error) {
	// Validate tenant ID
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "", api.NewEmptyFieldError("secrets.dek.EnsureTenantDEK", "tenant ID")
	}

	if algorithm == "" {
		algorithm = defaultDEKAlgorithm
	}

	dekid := NewTenantDEKID(tenantID)

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if tenant DEK already exists
	key := tenantDEKPathPrefix + tenantID
	existing, err := d.store.Get(ctx, key)
	if err == nil && existing != nil {
		// DEK already exists, this is OK (idempotent)
		d.logger.WithField("tenant_id", tenantID).Debug("Tenant DEK already exists")
		return dekid, nil
	}

	// Generate new DEK
	kekAttrs := &types.KeyAttributes{CN: d.masterKeyID}
	plaintext, wrapped, err := d.kms.GenerateDataKey(ctx, kekAttrs, defaultDEKKeyBytes)
	if err != nil {
		d.logger.WithError(err).WithField("tenant_id", tenantID).Error("Failed to generate tenant DEK")
		return "", api.NewSentinelWrapError("secrets.dek.EnsureTenantDEK", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	// Clear plaintext from memory when done
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()

	// Create DEK entry
	now := time.Now()
	entry := &DEKEntry{
		DEKID:         string(dekid),
		WrappedKey:    wrapped,
		Algorithm:     algorithm,
		Version:       1,
		CreatedAt:     now,
		LastRotatedAt: now,
	}

	// Marshal and store
	data, err := json.Marshal(entry)
	if err != nil {
		d.logger.WithError(err).WithField("tenant_id", tenantID).Error("Failed to marshal tenant DEK entry")
		return "", api.NewSentinelWrapError("secrets.dek.EnsureTenantDEK", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	if err := d.store.Put(ctx, key, data); err != nil {
		d.logger.WithError(err).WithField("tenant_id", tenantID).Error("Failed to store tenant DEK")
		return "", api.NewSentinelWrapError("secrets.dek.EnsureTenantDEK", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	d.logger.WithField("tenant_id", tenantID).Info("Tenant DEK created successfully")
	return dekid, nil
}

// EnsureDatabaseDEK creates or retrieves the DEK for a tenant's database.
// This method is idempotent - it will return the existing DEK if it already exists.
func (d *SecretsDEK) EnsureDatabaseDEK(ctx context.Context, tenantID, dbName string) (DEKID, error) {
	// Validate tenant ID
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return "", api.NewEmptyFieldError("secrets.dek.EnsureDatabaseDEK", "tenant ID")
	}

	// Validate database name
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return "", api.NewEmptyFieldError("secrets.dek.EnsureDatabaseDEK", "database name")
	}

	dekid := DatabaseDEKID(tenantID, dbName)

	d.mu.Lock()
	defer d.mu.Unlock()

	// Check if database DEK already exists
	key := databaseDEKStoragePath(tenantID, dbName)
	existing, err := d.store.Get(ctx, key)
	if err == nil && existing != nil {
		// DEK already exists, this is OK (idempotent)
		d.logger.WithField("tenant_id", tenantID).WithField("db_name", dbName).
			Debug("Database DEK already exists")
		return dekid, nil
	}

	// Generate new DEK
	kekAttrs := &types.KeyAttributes{CN: d.masterKeyID}
	plaintext, wrapped, err := d.kms.GenerateDataKey(ctx, kekAttrs, defaultDEKKeyBytes)
	if err != nil {
		d.logger.WithError(err).WithField("tenant_id", tenantID).WithField("db_name", dbName).
			Error("Failed to generate database DEK")
		return "", api.NewSentinelWrapError("secrets.dek.EnsureDatabaseDEK", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	// Clear plaintext from memory when done
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()

	// Create DEK entry
	now := time.Now()
	entry := &DEKEntry{
		DEKID:         string(dekid),
		WrappedKey:    wrapped,
		Algorithm:     defaultDEKAlgorithm,
		Version:       1,
		CreatedAt:     now,
		LastRotatedAt: now,
	}

	// Marshal and store
	data, err := json.Marshal(entry)
	if err != nil {
		d.logger.WithError(err).WithField("tenant_id", tenantID).WithField("db_name", dbName).
			Error("Failed to marshal database DEK entry")
		return "", api.NewSentinelWrapError("secrets.dek.EnsureDatabaseDEK", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	if err := d.store.Put(ctx, key, data); err != nil {
		d.logger.WithError(err).WithField("tenant_id", tenantID).WithField("db_name", dbName).
			Error("Failed to store database DEK")
		return "", api.NewSentinelWrapError("secrets.dek.EnsureDatabaseDEK", dberrors.ErrEncryption, ErrDEKInitializationFailed, err)
	}

	d.logger.WithField("tenant_id", tenantID).WithField("db_name", dbName).
		Info("Database DEK created successfully")
	return dekid, nil
}

// DeleteDatabaseDEK removes the DEK for a tenant's database.
func (d *SecretsDEK) DeleteDatabaseDEK(ctx context.Context, tenantID, dbName string) error {
	// Validate tenant ID
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return api.NewEmptyFieldError("secrets.dek.DeleteDatabaseDEK", "tenant ID")
	}

	// Validate database name
	dbName = strings.TrimSpace(dbName)
	if dbName == "" {
		return api.NewEmptyFieldError("secrets.dek.DeleteDatabaseDEK", "database name")
	}

	dekid := DatabaseDEKID(tenantID, dbName)

	d.mu.Lock()
	defer d.mu.Unlock()

	// Delete from storage
	key := databaseDEKStoragePath(tenantID, dbName)
	if err := d.store.Delete(ctx, key); err != nil && !errors.Is(err, ErrSecretNotFound) {
		d.logger.WithError(err).WithField("tenant_id", tenantID).WithField("db_name", dbName).
			Error("Failed to delete database DEK")
		return api.NewDEKOperationError("secrets.dek.DeleteDatabaseDEK", "failed to delete database DEK", err)
	}

	// Clear cache entry
	d.cache.delete(dekid)

	d.logger.WithField("tenant_id", tenantID).WithField("db_name", dbName).
		Info("Database DEK deleted successfully")
	return nil
}

// databaseDEKStoragePath returns the storage path for a database DEK.
func databaseDEKStoragePath(tenantID, dbName string) string {
	return databaseDEKPathPrefix + tenantID + "/" + dbName
}

// GetStatus returns status information about a DEK.
func (d *SecretsDEK) GetStatus(ctx context.Context, dekid DEKID) (*DEKStatus, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entry, err := d.loadDEKEntry(ctx, dekid)
	if err != nil {
		return nil, err
	}

	return &DEKStatus{
		DEKID:         dekid,
		Algorithm:     entry.Algorithm,
		Version:       entry.Version,
		CreatedAt:     entry.CreatedAt,
		LastRotatedAt: entry.LastRotatedAt,
	}, nil
}

// Validate checks if a DEKID is valid and exists.
func (d *SecretsDEK) Validate(ctx context.Context, dekid DEKID) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	_, err := d.loadDEKEntry(ctx, dekid)
	return err
}

// Rotate creates a new version of the DEK.
func (d *SecretsDEK) Rotate(ctx context.Context, dekid DEKID) (uint64, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Load existing entry
	entry, err := d.loadDEKEntry(ctx, dekid)
	if err != nil {
		return 0, err
	}

	oldVersion := entry.Version

	// Store the old version at a versioned path for decryption of old data
	if err := d.storeVersionEntry(ctx, dekid, entry); err != nil {
		d.logger.WithError(err).WithField("dekid", dekid).WithField("version", oldVersion).
			Error("Failed to store old DEK version")
		return 0, api.NewSentinelWrapError("secrets.dek.Rotate", dberrors.ErrInternal, ErrDEKRotationFailed, err)
	}

	// Generate new DEK
	kekAttrs := &types.KeyAttributes{CN: d.masterKeyID}
	plaintext, wrapped, err := d.kms.GenerateDataKey(ctx, kekAttrs, defaultDEKKeyBytes)
	if err != nil {
		d.logger.WithError(err).WithField("dekid", dekid).Error("Failed to generate rotated DEK")
		return 0, api.NewSentinelWrapError("secrets.dek.Rotate", dberrors.ErrEncryption, ErrDEKRotationFailed, err)
	}

	// Clear plaintext from memory when done
	defer func() {
		for i := range plaintext {
			plaintext[i] = 0
		}
	}()

	// Update entry with new version
	newVersion := entry.Version + 1
	now := time.Now()
	entry.WrappedKey = wrapped
	entry.Version = newVersion
	entry.LastRotatedAt = now

	// Store updated current entry
	key := d.dekIDToStorageKey(dekid)
	data, err := json.Marshal(entry)
	if err != nil {
		d.logger.WithError(err).WithField("dekid", dekid).Error("Failed to marshal rotated DEK entry")
		return 0, api.NewSentinelWrapError("secrets.dek.Rotate", dberrors.ErrInternal, ErrDEKRotationFailed, err)
	}

	if err := d.store.Put(ctx, key, data); err != nil {
		d.logger.WithError(err).WithField("dekid", dekid).Error("Failed to store rotated DEK")
		return 0, api.NewSentinelWrapError("secrets.dek.Rotate", dberrors.ErrInternal, ErrDEKRotationFailed, err)
	}

	// Clear cache entry to force reload (don't clear versioned cache - old version still valid)
	d.cache.delete(dekid)

	d.logger.WithField("dekid", dekid).WithField("old_version", oldVersion).
		WithField("new_version", newVersion).Info("DEK rotated successfully")
	return newVersion, nil
}

// storeVersionEntry stores a DEK entry at a versioned path for historical access.
// Must be called with write lock held.
func (d *SecretsDEK) storeVersionEntry(ctx context.Context, dekid DEKID, entry *DEKEntry) error {
	versionEntry := &DEKVersionEntry{
		DEKID:      string(dekid),
		Version:    entry.Version,
		WrappedKey: entry.WrappedKey,
		Algorithm:  entry.Algorithm,
		CreatedAt:  entry.LastRotatedAt, // When this version was created
	}

	data, err := json.Marshal(versionEntry)
	if err != nil {
		return api.NewDEKOperationError("secrets.dek.storeVersionEntry", "failed to marshal version entry", err)
	}

	// Store at versioned path: _internal/secrets/dek/versions/{dekid}/v{version}
	versionPath := d.versionStoragePath(dekid, entry.Version)
	if err := d.store.Put(ctx, versionPath, data); err != nil {
		return api.NewDEKOperationError("secrets.dek.storeVersionEntry", "failed to store version entry", err)
	}

	return nil
}

// versionStoragePath returns the storage path for a specific DEK version.
func (d *SecretsDEK) versionStoragePath(dekid DEKID, version uint64) string {
	// Encode dekid to be safe for path usage (replace : with /)
	encodedDEKID := strings.ReplaceAll(string(dekid), ":", "/")
	return fmt.Sprintf("%s%s/v%d", dekVersionsPrefix, encodedDEKID, version)
}

// DeleteTenantDEK securely deletes a tenant's DEK.
func (d *SecretsDEK) DeleteTenantDEK(ctx context.Context, tenantID string) error {
	// Validate tenant ID
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return api.NewEmptyFieldError("secrets.dek.DeleteTenantDEK", "tenant ID")
	}

	dekid := NewTenantDEKID(tenantID)

	d.mu.Lock()
	defer d.mu.Unlock()

	// Delete from storage
	key := tenantDEKPathPrefix + tenantID
	if err := d.store.Delete(ctx, key); err != nil && !errors.Is(err, ErrSecretNotFound) {
		d.logger.WithError(err).WithField("tenant_id", tenantID).Error("Failed to delete tenant DEK")
		return api.NewDEKOperationError("secrets.dek.DeleteTenantDEK", "failed to delete tenant DEK", err)
	}

	// Clear cache entry
	d.cache.delete(dekid)

	d.logger.WithField("tenant_id", tenantID).Info("Tenant DEK deleted successfully")
	return nil
}

// Close releases resources.
func (d *SecretsDEK) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Stop cache cleanup goroutine
	d.cache.stop()

	// Clear current version cache and zero out key material
	for _, entry := range d.cache.entries {
		for i := range entry.keyMaterial {
			entry.keyMaterial[i] = 0
		}
	}

	// Clear versioned cache and zero out key material
	for _, entry := range d.cache.versionedCache {
		for i := range entry.keyMaterial {
			entry.keyMaterial[i] = 0
		}
	}

	d.cache.clear()

	d.logger.Info("SecretsDEK closed")
	return nil
}

// GetKeyVersion retrieves a specific version of a DEK.
// This is used for decrypting data encrypted with an older DEK version.
func (d *SecretsDEK) GetKeyVersion(ctx context.Context, dekid DEKID, version uint64) ([]byte, error) {
	d.mu.RLock()

	// Check versioned cache first
	if keyMaterial, found := d.cache.getVersion(dekid, version); found {
		d.mu.RUnlock()
		return keyMaterial, nil
	}

	d.mu.RUnlock()

	// Not in cache, load from storage
	d.mu.Lock()
	defer d.mu.Unlock()

	// Double-check cache after acquiring write lock
	if keyMaterial, found := d.cache.getVersion(dekid, version); found {
		return keyMaterial, nil
	}

	// Check if this is the current version
	currentEntry, err := d.loadDEKEntry(ctx, dekid)
	if err != nil {
		return nil, err
	}

	if currentEntry.Version == version {
		// It's the current version, unwrap and cache
		plaintext, err := d.kms.Decrypt(ctx, d.masterKeyID, currentEntry.WrappedKey, nil)
		if err != nil {
			d.logger.WithError(err).WithField("dekid", dekid).WithField("version", version).
				Error("Failed to unwrap current DEK version")
			return nil, api.NewSentinelWrapError("secrets.dek.GetKeyVersion", dberrors.ErrDecryption, ErrDEKUnwrapFailed, err)
		}

		d.cache.setVersion(dekid, version, plaintext)

		keyMaterial := make([]byte, len(plaintext))
		copy(keyMaterial, plaintext)
		return keyMaterial, nil
	}

	// Load from versioned storage
	versionEntry, err := d.loadVersionEntry(ctx, dekid, version)
	if err != nil {
		return nil, err
	}

	// Unwrap the key
	plaintext, err := d.kms.Decrypt(ctx, d.masterKeyID, versionEntry.WrappedKey, nil)
	if err != nil {
		d.logger.WithError(err).WithField("dekid", dekid).WithField("version", version).
			Error("Failed to unwrap DEK version")
		return nil, api.NewSentinelWrapError("secrets.dek.GetKeyVersion", dberrors.ErrDecryption, ErrDEKUnwrapFailed, err)
	}

	// Cache the unwrapped key
	d.cache.setVersion(dekid, version, plaintext)

	// Return a copy
	keyMaterial := make([]byte, len(plaintext))
	copy(keyMaterial, plaintext)

	return keyMaterial, nil
}

// loadVersionEntry loads a specific DEK version from storage.
// Must be called with at least read lock held.
func (d *SecretsDEK) loadVersionEntry(ctx context.Context, dekid DEKID, version uint64) (*DEKVersionEntry, error) {
	versionPath := d.versionStoragePath(dekid, version)

	data, err := d.store.Get(ctx, versionPath)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return nil, api.NewDEKVersionNotFoundError("secrets.dek.loadVersionEntry", string(dekid), version)
		}
		d.logger.WithError(err).WithField("dekid", dekid).WithField("version", version).
			Error("Failed to load DEK version entry")
		return nil, api.NewDEKOperationError("secrets.dek.loadVersionEntry", "failed to load DEK version", err)
	}

	var entry DEKVersionEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		d.logger.WithError(err).WithField("dekid", dekid).WithField("version", version).
			Error("Failed to unmarshal DEK version entry")
		return nil, api.NewDEKOperationError("secrets.dek.loadVersionEntry", "failed to unmarshal DEK version", err)
	}

	return &entry, nil
}

// ListVersions returns all available versions of a DEK.
func (d *SecretsDEK) ListVersions(ctx context.Context, dekid DEKID) ([]DEKVersionInfo, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	// Get current entry for the current version
	currentEntry, err := d.loadDEKEntry(ctx, dekid)
	if err != nil {
		return nil, err
	}

	versions := make([]DEKVersionInfo, 0)

	// Add current version
	versions = append(versions, DEKVersionInfo{
		Version:   currentEntry.Version,
		Algorithm: currentEntry.Algorithm,
		CreatedAt: currentEntry.LastRotatedAt,
		IsCurrent: true,
	})

	// Scan for historical versions
	encodedDEKID := strings.ReplaceAll(string(dekid), ":", "/")
	prefix := fmt.Sprintf("%s%s/v", dekVersionsPrefix, encodedDEKID)

	keys, err := d.store.List(ctx, prefix)
	if err != nil {
		// If List returns error, just return current version
		d.logger.WithError(err).WithField("dekid", dekid).Debug("Failed to list DEK versions")
		return versions, nil
	}

	for _, key := range keys {
		// Extract version number from key
		keyStr := string(key)
		if !strings.HasPrefix(keyStr, prefix) {
			continue
		}

		var versionNum uint64
		if _, err := fmt.Sscanf(keyStr[len(prefix):], "%d", &versionNum); err != nil {
			continue
		}

		// Skip current version (already added)
		if versionNum == currentEntry.Version {
			continue
		}

		// Load version entry for metadata
		versionEntry, err := d.loadVersionEntry(ctx, dekid, versionNum)
		if err != nil {
			d.logger.WithError(err).WithField("dekid", dekid).WithField("version", versionNum).
				Debug("Failed to load version entry during list")
			continue
		}

		versions = append(versions, DEKVersionInfo{
			Version:   versionEntry.Version,
			Algorithm: versionEntry.Algorithm,
			CreatedAt: versionEntry.CreatedAt,
			IsCurrent: false,
		})
	}

	return versions, nil
}

// CleanupOldVersions removes old DEK versions beyond the retention limit.
// retainCount specifies how many old versions to keep (in addition to current).
func (d *SecretsDEK) CleanupOldVersions(ctx context.Context, dekid DEKID, retainCount int) (int, error) {
	if retainCount < 0 {
		retainCount = defaultRetainVersions
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	// Get current version
	currentEntry, err := d.loadDEKEntry(ctx, dekid)
	if err != nil {
		return 0, err
	}

	// List all versions
	encodedDEKID := strings.ReplaceAll(string(dekid), ":", "/")
	prefix := fmt.Sprintf("%s%s/v", dekVersionsPrefix, encodedDEKID)

	keys, err := d.store.List(ctx, prefix)
	if err != nil {
		return 0, api.NewDEKOperationError("secrets.dek.CleanupOldVersions", "failed to list DEK versions", err)
	}

	// Collect all version numbers
	type versionInfo struct {
		version uint64
		key     string
	}
	allVersions := make([]versionInfo, 0)

	for _, key := range keys {
		keyStr := string(key)
		if !strings.HasPrefix(keyStr, prefix) {
			continue
		}

		var versionNum uint64
		if _, err := fmt.Sscanf(keyStr[len(prefix):], "%d", &versionNum); err != nil {
			continue
		}

		// Don't include current version in cleanup candidates
		if versionNum == currentEntry.Version {
			continue
		}

		allVersions = append(allVersions, versionInfo{version: versionNum, key: keyStr})
	}

	// Sort by version descending (newest first)
	for i := 0; i < len(allVersions); i++ {
		for j := i + 1; j < len(allVersions); j++ {
			if allVersions[j].version > allVersions[i].version {
				allVersions[i], allVersions[j] = allVersions[j], allVersions[i]
			}
		}
	}

	// Delete versions beyond retention limit
	deletedCount := 0
	for i := retainCount; i < len(allVersions); i++ {
		vi := allVersions[i]
		if err := d.store.Delete(ctx, vi.key); err != nil {
			d.logger.WithError(err).WithField("dekid", dekid).WithField("version", vi.version).
				Warn("Failed to delete old DEK version")
			continue
		}

		// Clear from cache
		d.cache.deleteVersion(dekid, vi.version)

		d.logger.WithField("dekid", dekid).WithField("version", vi.version).
			Debug("Deleted old DEK version")
		deletedCount++
	}

	if deletedCount > 0 {
		d.logger.WithField("dekid", dekid).WithField("deleted_count", deletedCount).
			Info("Cleaned up old DEK versions")
	}

	return deletedCount, nil
}

// loadDEKEntry loads a DEK entry from storage.
// Must be called with at least read lock held.
func (d *SecretsDEK) loadDEKEntry(ctx context.Context, dekid DEKID) (*DEKEntry, error) {
	key := d.dekIDToStorageKey(dekid)

	data, err := d.store.Get(ctx, key)
	if err != nil {
		if errors.Is(err, ErrSecretNotFound) {
			return nil, api.NewDEKNotFoundError("secrets.dek.loadDEKEntry", string(dekid))
		}
		d.logger.WithError(err).WithField("dekid", dekid).Error("Failed to load DEK entry")
		return nil, api.NewDEKOperationError("secrets.dek.loadDEKEntry", "failed to load DEK", err)
	}

	var entry DEKEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		d.logger.WithError(err).WithField("dekid", dekid).Error("Failed to unmarshal DEK entry")
		return nil, api.NewDEKOperationError("secrets.dek.loadDEKEntry", "failed to unmarshal DEK", err)
	}

	return &entry, nil
}

// dekIDToStorageKey converts a DEKID to a storage key.
func (d *SecretsDEK) dekIDToStorageKey(dekid DEKID) string {
	if IsGlobal(dekid) {
		return globalDEKPath
	}

	// Check for database DEKID first (more specific match)
	if tenantID, dbName, ok := GetDatabaseInfo(dekid); ok {
		return databaseDEKStoragePath(tenantID, dbName)
	}

	if tenantID, ok := GetTenantID(dekid); ok {
		return tenantDEKPathPrefix + tenantID
	}

	// Fallback (should not happen with valid DEKIDs)
	return dekStoragePrefix + string(dekid)
}
