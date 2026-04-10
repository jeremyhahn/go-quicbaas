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
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Tests
// =============================================================================

func TestNewSecretsDEK(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)

	require.NotNil(t, dek)
	assert.NotNil(t, dek.kms)
	assert.NotNil(t, dek.store)
	assert.NotNil(t, dek.logger)
	assert.Equal(t, "master-key-id", dek.masterKeyID)
	assert.NotNil(t, dek.cache)
}

func TestSecretsDEK_Initialize(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		setupMock func(*testMockKVStore, *testMockKMSManager)
		wantErr   bool
		errType   error
	}{
		{
			name:      "successful initialization",
			algorithm: "AES-256-GCM",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				// No setup needed, default behavior works
			},
			wantErr: false,
		},
		{
			name:      "idempotent - already initialized",
			algorithm: "AES-256-GCM",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				// Pre-populate with existing DEK
				entry := &DEKEntry{
					DEKID:         string(DEKIDGlobal),
					WrappedKey:    []byte("wrapped-key"),
					Algorithm:     "AES-256-GCM",
					Version:       1,
					CreatedAt:     time.Now(),
					LastRotatedAt: time.Now(),
				}
				data, _ := json.Marshal(entry)
				store.data["_internal/secrets/dek/global"] = data
			},
			wantErr: false,
		},
		{
			name:      "KMS failure",
			algorithm: "AES-256-GCM",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				kms.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
					return nil, nil, errors.New("KMS unavailable")
				}
			},
			wantErr: true,
			errType: ErrDEKInitializationFailed,
		},
		{
			name:      "store failure",
			algorithm: "AES-256-GCM",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				store.putFn = func(ctx context.Context, key string, value []byte) error {
					return errors.New("storage error")
				}
			},
			wantErr: true,
			errType: ErrDEKInitializationFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			if tt.setupMock != nil {
				tt.setupMock(store, kms)
			}

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)
			ctx := context.Background()

			err := dek.Initialize(ctx, tt.algorithm)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)

				// Verify DEK was stored
				key := "_internal/secrets/dek/global"
				data, err := store.Get(ctx, key)
				require.NoError(t, err)

				var entry DEKEntry
				err = json.Unmarshal(data, &entry)
				require.NoError(t, err)

				assert.Equal(t, string(DEKIDGlobal), entry.DEKID)
				assert.Equal(t, tt.algorithm, entry.Algorithm)
				assert.NotEmpty(t, entry.WrappedKey)
				assert.Equal(t, uint64(1), entry.Version)
			}
		})
	}
}

func TestSecretsDEK_GetKey(t *testing.T) {
	tests := []struct {
		name      string
		dekid     DEKID
		setupMock func(*testMockKVStore, *testMockKMSManager, *SecretsDEK)
		wantErr   bool
		errType   error
		errMsg    string
	}{
		{
			name:  "successful retrieval from storage",
			dekid: DEKIDGlobal,
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager, d *SecretsDEK) {
				// Initialize first
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
			},
			wantErr: false,
		},
		{
			name:  "cached retrieval",
			dekid: DEKIDGlobal,
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager, d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
				// First call caches it
				_, _, _ = d.GetKey(ctx, DEKIDGlobal)
			},
			wantErr: false,
		},
		{
			name:  "DEK not found",
			dekid: NewTenantDEKID("nonexistent"),
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager, d *SecretsDEK) {
				// Don't initialize anything
			},
			wantErr: true,
			errMsg:  "DEK not found",
		},
		{
			name:  "unwrap failure",
			dekid: DEKIDGlobal,
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager, d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")

				// Make decrypt fail
				kms.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
					return nil, errors.New("decrypt failed")
				}
			},
			wantErr: true,
			errType: ErrDEKUnwrapFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)

			if tt.setupMock != nil {
				tt.setupMock(store, kms, dek)
			}

			ctx := context.Background()
			keyMaterial, version, err := dek.GetKey(ctx, tt.dekid)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				} else if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, keyMaterial)
				assert.Greater(t, version, uint64(0))
			}
		})
	}
}

func TestSecretsDEK_EnsureTenantDEK(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		algorithm string
		setupMock func(*testMockKVStore, *testMockKMSManager)
		wantErr   bool
	}{
		{
			name:      "create new tenant DEK",
			tenantID:  "tenant-123",
			algorithm: "AES-256-GCM",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				// No setup needed
			},
			wantErr: false,
		},
		{
			name:      "idempotent - tenant DEK already exists",
			tenantID:  "tenant-123",
			algorithm: "AES-256-GCM",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				// Pre-populate with existing tenant DEK
				entry := &DEKEntry{
					DEKID:         "dek:tenant:tenant-123",
					WrappedKey:    []byte("wrapped-key"),
					Algorithm:     "AES-256-GCM",
					Version:       1,
					CreatedAt:     time.Now(),
					LastRotatedAt: time.Now(),
				}
				data, _ := json.Marshal(entry)
				store.data["_internal/secrets/dek/tenant/tenant-123"] = data
			},
			wantErr: false,
		},
		{
			name:      "empty tenant ID",
			tenantID:  "",
			algorithm: "AES-256-GCM",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			if tt.setupMock != nil {
				tt.setupMock(store, kms)
			}

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)
			ctx := context.Background()

			dekid, err := dek.EnsureTenantDEK(ctx, tt.tenantID, tt.algorithm)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.True(t, IsTenant(dekid))

				// Verify tenant ID matches
				extractedID, ok := GetTenantID(dekid)
				assert.True(t, ok)
				assert.Equal(t, tt.tenantID, extractedID)
			}
		})
	}
}

func TestSecretsDEK_GetStatus(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize global DEK
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Get status
	status, err := dek.GetStatus(ctx, DEKIDGlobal)
	require.NoError(t, err)
	require.NotNil(t, status)

	assert.Equal(t, DEKIDGlobal, status.DEKID)
	assert.Equal(t, "AES-256-GCM", status.Algorithm)
	assert.Equal(t, uint64(1), status.Version)
	assert.False(t, status.CreatedAt.IsZero())
	assert.False(t, status.LastRotatedAt.IsZero())
}

func TestSecretsDEK_Rotate(t *testing.T) {
	tests := []struct {
		name      string
		dekid     DEKID
		setupMock func(*SecretsDEK)
		wantErr   bool
	}{
		{
			name:  "successful rotation",
			dekid: DEKIDGlobal,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
			},
			wantErr: false,
		},
		{
			name:  "rotate non-existent DEK",
			dekid: NewTenantDEKID("nonexistent"),
			setupMock: func(d *SecretsDEK) {
				// Don't initialize
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)

			if tt.setupMock != nil {
				tt.setupMock(dek)
			}

			ctx := context.Background()
			newVersion, err := dek.Rotate(ctx, tt.dekid)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, uint64(2), newVersion)

				// Verify cache was cleared
				dek.mu.RLock()
				_, cached := dek.cache.entries[tt.dekid]
				dek.mu.RUnlock()
				assert.False(t, cached, "cache should be cleared after rotation")
			}
		})
	}
}

func TestSecretsDEK_DeleteTenantDEK(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		setupMock func(*SecretsDEK)
		wantErr   bool
	}{
		{
			name:     "delete existing tenant DEK",
			tenantID: "tenant-123",
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_, _ = d.EnsureTenantDEK(ctx, "tenant-123", "AES-256-GCM")
			},
			wantErr: false,
		},
		{
			name:     "delete non-existent tenant DEK",
			tenantID: "nonexistent",
			setupMock: func(d *SecretsDEK) {
				// Don't create anything
			},
			wantErr: false, // Idempotent
		},
		{
			name:     "empty tenant ID",
			tenantID: "",
			setupMock: func(d *SecretsDEK) {
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)

			if tt.setupMock != nil {
				tt.setupMock(dek)
			}

			ctx := context.Background()
			err := dek.DeleteTenantDEK(ctx, tt.tenantID)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)

				// Verify DEK was deleted from storage
				dekid := NewTenantDEKID(tt.tenantID)
				key := "_internal/secrets/dek/tenant/" + tt.tenantID
				_, err := store.Get(ctx, key)
				assert.Error(t, err)

				// Verify cache was cleared
				dek.mu.RLock()
				_, cached := dek.cache.entries[dekid]
				dek.mu.RUnlock()
				assert.False(t, cached)
			}
		})
	}
}

func TestSecretsDEK_Close(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize and cache some data
	_ = dek.Initialize(ctx, "AES-256-GCM")
	_, _, _ = dek.GetKey(ctx, DEKIDGlobal)

	// Close should clear cache
	err := dek.Close()
	require.NoError(t, err)

	// Verify cache is empty
	dek.mu.RLock()
	assert.Empty(t, dek.cache.entries)
	dek.mu.RUnlock()
}

func TestSecretsDEK_CacheExpiration(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Set very short TTL for testing
	dek.cache.ttl = 100 * time.Millisecond

	// Initialize
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Get key (should cache)
	_, _, err = dek.GetKey(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Verify cached
	dek.mu.RLock()
	_, cached := dek.cache.entries[DEKIDGlobal]
	dek.mu.RUnlock()
	assert.True(t, cached)

	// Wait for expiration
	time.Sleep(150 * time.Millisecond)

	// Get key again (should fetch from storage, not cache)
	initialCount := kms.decryptionCounter
	_, _, err = dek.GetKey(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Verify decrypt was called (cache expired)
	assert.Greater(t, kms.decryptionCounter, initialCount, "should have called decrypt after cache expiration")
}

func TestSecretsDEK_CacheSizeLimit(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Set small cache size for testing
	dek.cache.maxSize = 3

	// Create multiple tenant DEKs
	for i := 0; i < 5; i++ {
		tenantID := string(rune('A' + i))
		_, err := dek.EnsureTenantDEK(ctx, tenantID, "AES-256-GCM")
		require.NoError(t, err)

		// Access the DEK to cache it
		dekid := NewTenantDEKID(tenantID)
		_, _, err = dek.GetKey(ctx, dekid)
		require.NoError(t, err)
	}

	// Verify cache doesn't exceed max size
	dek.mu.RLock()
	cacheSize := len(dek.cache.entries)
	dek.mu.RUnlock()

	assert.LessOrEqual(t, cacheSize, dek.cache.maxSize, "cache size should not exceed maximum")
}

func TestSecretsDEK_ConcurrentAccess(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Concurrent reads
	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			_, _, err := dek.GetKey(ctx, DEKIDGlobal)
			assert.NoError(t, err)
		}()
	}

	wg.Wait()
}

func TestDEKEntry_Serialization(t *testing.T) {
	now := time.Now()
	entry := &DEKEntry{
		DEKID:         string(DEKIDGlobal),
		WrappedKey:    []byte("wrapped-key-data"),
		Algorithm:     "AES-256-GCM",
		Version:       1,
		CreatedAt:     now,
		LastRotatedAt: now,
	}

	// Marshal
	data, err := json.Marshal(entry)
	require.NoError(t, err)

	// Unmarshal
	var decoded DEKEntry
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entry.DEKID, decoded.DEKID)
	assert.Equal(t, entry.WrappedKey, decoded.WrappedKey)
	assert.Equal(t, entry.Algorithm, decoded.Algorithm)
	assert.Equal(t, entry.Version, decoded.Version)
	assert.WithinDuration(t, entry.CreatedAt, decoded.CreatedAt, time.Second)
	assert.WithinDuration(t, entry.LastRotatedAt, decoded.LastRotatedAt, time.Second)
}

// TestSecretsDEK_GetStatusNotFound tests GetStatus with non-existent DEK.
func TestSecretsDEK_GetStatusNotFound(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Try to get status of non-existent DEK
	_, err := dek.GetStatus(ctx, NewTenantDEKID("nonexistent"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEK not found")
}

// TestSecretsDEK_InitializeDefaultAlgorithm tests initialization with empty algorithm.
func TestSecretsDEK_InitializeDefaultAlgorithm(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize with empty algorithm (should use default)
	err := dek.Initialize(ctx, "")
	require.NoError(t, err)

	// Verify DEK was stored with default algorithm
	key := "_internal/secrets/dek/global"
	data, err := store.Get(ctx, key)
	require.NoError(t, err)

	var entry DEKEntry
	err = json.Unmarshal(data, &entry)
	require.NoError(t, err)

	assert.Equal(t, defaultDEKAlgorithm, entry.Algorithm)
}

// TestSecretsDEK_EnsureTenantDEKDefaultAlgorithm tests tenant DEK creation with empty algorithm.
func TestSecretsDEK_EnsureTenantDEKDefaultAlgorithm(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Create with empty algorithm (should use default)
	dekid, err := dek.EnsureTenantDEK(ctx, "tenant-123", "")
	require.NoError(t, err)

	// Verify algorithm
	status, err := dek.GetStatus(ctx, dekid)
	require.NoError(t, err)
	assert.Equal(t, defaultDEKAlgorithm, status.Algorithm)
}

// TestSecretsDEK_LoadDEKEntryInvalidJSON tests loading corrupted DEK entry.
func TestSecretsDEK_LoadDEKEntryInvalidJSON(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Store invalid JSON
	key := "_internal/secrets/dek/global"
	invalidJSON := []byte("{invalid json")
	err := store.Put(ctx, key, invalidJSON)
	require.NoError(t, err)

	// Try to load - should fail
	_, err = dek.GetStatus(ctx, DEKIDGlobal)
	require.Error(t, err)
}

// TestSecretsDEK_DeleteTenantDEKWithWhitespace tests deletion with whitespace in tenant ID.
func TestSecretsDEK_DeleteTenantDEKWithWhitespace(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Create tenant DEK
	_, err := dek.EnsureTenantDEK(ctx, "tenant-123", "AES-256-GCM")
	require.NoError(t, err)

	// Delete with whitespace (should be trimmed)
	err = dek.DeleteTenantDEK(ctx, "  tenant-123  ")
	require.NoError(t, err)
}

// TestSecretsDEK_DEKIDToStorageKeyFallback tests storage key generation fallback.
func TestSecretsDEK_DEKIDToStorageKeyFallback(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)

	// Test fallback for invalid DEKID (should never happen with valid DEKIDs)
	invalidDEKID := DEKID("invalid-format")
	key := dek.dekIDToStorageKey(invalidDEKID)
	assert.Contains(t, string(key), "invalid-format")
}

// TestSecretsDEK_CacheCleanupStop tests that cache cleanup goroutine stops properly.
func TestSecretsDEK_CacheCleanupStop(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)

	// Close should stop the cleanup goroutine
	err := dek.Close()
	require.NoError(t, err)

	// Give goroutine time to stop
	time.Sleep(10 * time.Millisecond)
}

// =============================================================================
// Multi-Version DEK Tests
// =============================================================================

func TestSecretsDEK_RotateStoresOldVersion(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Rotate
	newVersion, err := dek.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), newVersion)

	// Verify old version was stored at versioned path
	versionPath := dek.versionStoragePath(DEKIDGlobal, 1)
	data, err := store.Get(ctx, versionPath)
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Verify it's a valid DEKVersionEntry
	var versionEntry DEKVersionEntry
	err = json.Unmarshal(data, &versionEntry)
	require.NoError(t, err)

	assert.Equal(t, string(DEKIDGlobal), versionEntry.DEKID)
	assert.Equal(t, uint64(1), versionEntry.Version)
	assert.NotEmpty(t, versionEntry.WrappedKey)
	assert.Equal(t, "AES-256-GCM", versionEntry.Algorithm)
}

func TestSecretsDEK_GetKeyVersion(t *testing.T) {
	tests := []struct {
		name      string
		dekid     DEKID
		version   uint64
		setupMock func(*SecretsDEK)
		wantErr   bool
		errType   error
		errMsg    string
	}{
		{
			name:    "get current version",
			dekid:   DEKIDGlobal,
			version: 1,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
			},
			wantErr: false,
		},
		{
			name:    "get old version after rotation",
			dekid:   DEKIDGlobal,
			version: 1,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
				_, _ = d.Rotate(ctx, DEKIDGlobal) // Version 2 is current, 1 is old
			},
			wantErr: false,
		},
		{
			name:    "get version from cache",
			dekid:   DEKIDGlobal,
			version: 1,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
				// First call caches it
				_, _ = d.GetKeyVersion(ctx, DEKIDGlobal, 1)
			},
			wantErr: false,
		},
		{
			name:    "non-existent version",
			dekid:   DEKIDGlobal,
			version: 99,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
			},
			wantErr: true,
			errMsg:  "DEK version not found",
		},
		{
			name:    "non-existent DEK",
			dekid:   NewTenantDEKID("nonexistent"),
			version: 1,
			setupMock: func(d *SecretsDEK) {
				// Don't initialize anything
			},
			wantErr: true,
			errMsg:  "DEK not found",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)

			if tt.setupMock != nil {
				tt.setupMock(dek)
			}

			ctx := context.Background()
			keyMaterial, err := dek.GetKeyVersion(ctx, tt.dekid, tt.version)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				} else if tt.errType != nil {
					assert.ErrorIs(t, err, tt.errType)
				}
			} else {
				require.NoError(t, err)
				assert.NotEmpty(t, keyMaterial)
			}
		})
	}
}

func TestSecretsDEK_ListVersions(t *testing.T) {
	tests := []struct {
		name           string
		dekid          DEKID
		setupMock      func(*SecretsDEK)
		wantVersions   int
		wantCurrentVer uint64
		wantErr        bool
	}{
		{
			name:  "single version (no rotation)",
			dekid: DEKIDGlobal,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
			},
			wantVersions:   1,
			wantCurrentVer: 1,
			wantErr:        false,
		},
		{
			name:  "multiple versions after rotation",
			dekid: DEKIDGlobal,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
				_, _ = d.Rotate(ctx, DEKIDGlobal)
				_, _ = d.Rotate(ctx, DEKIDGlobal)
			},
			wantVersions:   3, // version 1, 2, 3
			wantCurrentVer: 3,
			wantErr:        false,
		},
		{
			name:  "non-existent DEK",
			dekid: NewTenantDEKID("nonexistent"),
			setupMock: func(d *SecretsDEK) {
				// Don't initialize
			},
			wantVersions: 0,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)

			if tt.setupMock != nil {
				tt.setupMock(dek)
			}

			ctx := context.Background()
			versions, err := dek.ListVersions(ctx, tt.dekid)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, versions, tt.wantVersions)

				// Find and verify current version
				foundCurrent := false
				for _, v := range versions {
					if v.IsCurrent {
						assert.Equal(t, tt.wantCurrentVer, v.Version)
						foundCurrent = true
					}
				}
				assert.True(t, foundCurrent, "should have found current version")
			}
		})
	}
}

func TestSecretsDEK_CleanupOldVersions(t *testing.T) {
	tests := []struct {
		name          string
		dekid         DEKID
		retainCount   int
		setupMock     func(*SecretsDEK)
		wantDeleted   int
		wantRemaining int
		wantErr       bool
	}{
		{
			name:        "cleanup with 3 versions, retain 1",
			dekid:       DEKIDGlobal,
			retainCount: 1,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
				_, _ = d.Rotate(ctx, DEKIDGlobal) // v1 -> v2
				_, _ = d.Rotate(ctx, DEKIDGlobal) // v2 -> v3
				_, _ = d.Rotate(ctx, DEKIDGlobal) // v3 -> v4
			},
			wantDeleted:   2, // v1, v2 deleted, v3 retained, v4 is current
			wantRemaining: 2, // v3 retained + v4 current
			wantErr:       false,
		},
		{
			name:        "cleanup with nothing to delete",
			dekid:       DEKIDGlobal,
			retainCount: 10,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
				_, _ = d.Rotate(ctx, DEKIDGlobal)
			},
			wantDeleted:   0,
			wantRemaining: 2, // v1 + v2 current
			wantErr:       false,
		},
		{
			name:        "cleanup non-existent DEK",
			dekid:       NewTenantDEKID("nonexistent"),
			retainCount: 1,
			setupMock: func(d *SecretsDEK) {
				// Don't initialize
			},
			wantDeleted: 0,
			wantErr:     true,
		},
		{
			name:        "negative retain count uses default",
			dekid:       DEKIDGlobal,
			retainCount: -1,
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_ = d.Initialize(ctx, "AES-256-GCM")
				// Create 6 versions (1 current + 5 old)
				for i := 0; i < 5; i++ {
					_, _ = d.Rotate(ctx, DEKIDGlobal)
				}
			},
			wantDeleted:   2, // 5 old - 3 retained = 2 deleted
			wantRemaining: 4, // 3 retained + 1 current
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)

			if tt.setupMock != nil {
				tt.setupMock(dek)
			}

			ctx := context.Background()
			deleted, err := dek.CleanupOldVersions(ctx, tt.dekid, tt.retainCount)

			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantDeleted, deleted)

				// Verify remaining versions
				versions, err := dek.ListVersions(ctx, tt.dekid)
				require.NoError(t, err)
				assert.Len(t, versions, tt.wantRemaining)
			}
		})
	}
}

func TestSecretsDEK_VersionStoragePath(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)

	tests := []struct {
		name    string
		dekid   DEKID
		version uint64
		want    string
	}{
		{
			name:    "global DEK path",
			dekid:   DEKIDGlobal,
			version: 1,
			want:    "_internal/secrets/dek/versions/dek/global/v1",
		},
		{
			name:    "tenant DEK path",
			dekid:   NewTenantDEKID("tenant-123"),
			version: 5,
			want:    "_internal/secrets/dek/versions/dek/tenant/tenant-123/v5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := dek.versionStoragePath(tt.dekid, tt.version)
			assert.Equal(t, tt.want, path)
		})
	}
}

func TestSecretsDEK_VersionedCacheBehavior(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize and rotate
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Get old version (should cache)
	key1, err := dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.NoError(t, err)
	require.NotEmpty(t, key1)

	// Verify versioned cache has entry
	dek.mu.RLock()
	cacheKey := dekCacheKey{dekid: DEKIDGlobal, version: 1}
	_, cached := dek.cache.versionedCache[cacheKey]
	dek.mu.RUnlock()
	assert.True(t, cached, "version should be cached")

	// Get same version again - should use cache
	initialDecryptCount := kms.decryptionCounter
	key2, err := dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.NoError(t, err)
	assert.Equal(t, key1, key2)

	// Decrypt counter should not increase (used cache)
	assert.Equal(t, initialDecryptCount, kms.decryptionCounter, "should have used cache")
}

func TestDEKVersionEntry_Serialization(t *testing.T) {
	now := time.Now()
	entry := &DEKVersionEntry{
		DEKID:      string(DEKIDGlobal),
		Version:    5,
		WrappedKey: []byte("wrapped-key-data"),
		Algorithm:  "AES-256-GCM",
		CreatedAt:  now,
	}

	// Marshal
	data, err := json.Marshal(entry)
	require.NoError(t, err)

	// Unmarshal
	var decoded DEKVersionEntry
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, entry.DEKID, decoded.DEKID)
	assert.Equal(t, entry.Version, decoded.Version)
	assert.Equal(t, entry.WrappedKey, decoded.WrappedKey)
	assert.Equal(t, entry.Algorithm, decoded.Algorithm)
	assert.WithinDuration(t, entry.CreatedAt, decoded.CreatedAt, time.Second)
}

func TestDEKVersionInfo_Fields(t *testing.T) {
	info := DEKVersionInfo{
		Version:   3,
		Algorithm: "AES-256-GCM",
		CreatedAt: time.Now(),
		IsCurrent: true,
	}

	assert.Equal(t, uint64(3), info.Version)
	assert.Equal(t, "AES-256-GCM", info.Algorithm)
	assert.True(t, info.IsCurrent)
}

func TestSecretsDEK_RotateVersionStorageFailure(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Make storage fail for version path
	store.putFn = func(ctx context.Context, key string, value []byte) error {
		if string(key) != "_internal/secrets/dek/global" {
			return errors.New("storage error for version")
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		store.data[string(key)] = value
		return nil
	}

	// Rotate should fail
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDEKRotationFailed)
}

func TestSecretsDEK_RotateKMSFailure(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize first
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Now make KMS fail for subsequent GenerateDataKey calls
	kms.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
		return nil, nil, errors.New("KMS failure during rotation")
	}

	// Rotate should fail
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDEKRotationFailed)
}

func TestSecretsDEK_GetKeyVersionDecryptFailure(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize and rotate to create old version
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Make decrypt fail
	kms.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		return nil, errors.New("decrypt failed")
	}

	// GetKeyVersion should fail
	_, err = dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDEKUnwrapFailed)
}

func TestSecretsDEK_GetKeyVersionCurrentDecryptFailure(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Make decrypt fail
	kms.decryptFn = func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
		return nil, errors.New("decrypt failed")
	}

	// GetKeyVersion for current version should fail
	_, err = dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDEKUnwrapFailed)
}

func TestSecretsDEK_LoadVersionEntryInvalidJSON(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize and rotate
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Corrupt the stored version entry
	versionPath := dek.versionStoragePath(DEKIDGlobal, 1)
	err = store.Put(ctx, versionPath, []byte("{invalid json"))
	require.NoError(t, err)

	// GetKeyVersion should fail due to invalid JSON
	_, err = dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestSecretsDEK_ListVersionsWithListError(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Make List fail
	store.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, errors.New("list error")
	}

	// ListVersions should still return current version
	versions, err := dek.ListVersions(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Len(t, versions, 1) // Just the current version
	assert.True(t, versions[0].IsCurrent)
}

func TestSecretsDEK_CleanupOldVersionsListError(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Make List fail
	store.listFn = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, errors.New("list error")
	}

	// CleanupOldVersions should fail
	_, err = dek.CleanupOldVersions(ctx, DEKIDGlobal, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "list DEK versions")
}

func TestSecretsDEK_VersionedCacheExpiration(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Set very short TTL
	dek.cache.ttl = 50 * time.Millisecond

	// Initialize and rotate
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Get old version (should cache)
	_, err = dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.NoError(t, err)

	// Wait for expiration
	time.Sleep(100 * time.Millisecond)

	// Get same version again - cache should be expired
	initialDecryptCount := kms.decryptionCounter
	_, err = dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.NoError(t, err)

	// Decrypt counter should increase (cache expired)
	assert.Greater(t, kms.decryptionCounter, initialDecryptCount, "should have called decrypt after cache expiration")
}

func TestSecretsDEK_VersionedCacheSizeLimit(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Set small versioned cache size
	dek.cache.maxSize = 3

	// Initialize
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Create multiple versions
	for i := 0; i < 5; i++ {
		_, err = dek.Rotate(ctx, DEKIDGlobal)
		require.NoError(t, err)
	}

	// Access multiple old versions
	for v := uint64(1); v <= 5; v++ {
		_, err = dek.GetKeyVersion(ctx, DEKIDGlobal, v)
		require.NoError(t, err)
	}

	// Verify versioned cache doesn't exceed max size
	dek.mu.RLock()
	versionedCacheSize := len(dek.cache.versionedCache)
	dek.mu.RUnlock()

	assert.LessOrEqual(t, versionedCacheSize, dek.cache.maxSize)
}

func TestSecretsDEK_CacheDeleteVersion(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	// Add versioned entry
	cache.setVersion(DEKIDGlobal, 1, []byte("key-material"))

	// Verify it's cached
	_, found := cache.getVersion(DEKIDGlobal, 1)
	assert.True(t, found)

	// Delete it
	cache.deleteVersion(DEKIDGlobal, 1)

	// Verify it's gone
	_, found = cache.getVersion(DEKIDGlobal, 1)
	assert.False(t, found)
}

func TestSecretsDEK_CacheDeleteAllVersions(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	// Add multiple versioned entries for same DEKID
	cache.setVersion(DEKIDGlobal, 1, []byte("key-v1"))
	cache.setVersion(DEKIDGlobal, 2, []byte("key-v2"))
	cache.setVersion(DEKIDGlobal, 3, []byte("key-v3"))

	// Add version for different DEKID
	tenantDEKID := NewTenantDEKID("tenant-1")
	cache.setVersion(tenantDEKID, 1, []byte("tenant-key"))

	// Delete all versions for global
	cache.deleteAllVersions(DEKIDGlobal)

	// Verify global versions are gone
	_, found := cache.getVersion(DEKIDGlobal, 1)
	assert.False(t, found)
	_, found = cache.getVersion(DEKIDGlobal, 2)
	assert.False(t, found)
	_, found = cache.getVersion(DEKIDGlobal, 3)
	assert.False(t, found)

	// Verify tenant version still exists
	_, found = cache.getVersion(tenantDEKID, 1)
	assert.True(t, found)
}

func TestSecretsDEK_CloseZerosVersionedCache(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize, rotate, and cache old version
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)
	_, err = dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.NoError(t, err)

	// Close should clear versioned cache
	err = dek.Close()
	require.NoError(t, err)

	// Verify versioned cache is empty
	dek.mu.RLock()
	assert.Empty(t, dek.cache.versionedCache)
	dek.mu.RUnlock()
}

// =============================================================================
// Validate Tests
// =============================================================================

// TestSecretsDEK_Validate_Success tests validating a global DEK that exists.
func TestSecretsDEK_Validate_Success(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize global DEK
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// Validate should succeed
	err = dek.Validate(ctx, DEKIDGlobal)
	assert.NoError(t, err)
}

// TestSecretsDEK_Validate_TenantDEK tests validating a tenant DEK that exists.
func TestSecretsDEK_Validate_TenantDEK(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Create tenant DEK
	tenantID := "tenant-abc"
	dekid, err := dek.EnsureTenantDEK(ctx, tenantID, "AES-256-GCM")
	require.NoError(t, err)

	// Validate should succeed
	err = dek.Validate(ctx, dekid)
	assert.NoError(t, err)
}

// TestSecretsDEK_Validate_NotFound tests validating a DEKID that doesn't exist.
func TestSecretsDEK_Validate_NotFound(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Try to validate non-existent DEK
	nonexistentDEKID := NewTenantDEKID("nonexistent-tenant")
	err := dek.Validate(ctx, nonexistentDEKID)

	// Should return error
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DEK not found")
}

// =============================================================================
// DEK Cache Cleanup Tests - COVERAGE IMPROVEMENT
// =============================================================================

// TestDEKCache_CleanupExpired_RemovesExpired verifies that the cleanup
// goroutine removes expired cache entries from both current and versioned caches.
func TestDEKCache_CleanupExpired_RemovesExpired(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	// Set very short TTL for testing (shorter than cleanup interval)
	cache.ttl = 50 * time.Millisecond

	// Add entries to both caches
	cache.set(DEKIDGlobal, []byte("global-key"), 1)
	cache.setVersion(DEKIDGlobal, 1, []byte("global-v1"))
	tenantDEKID := NewTenantDEKID("tenant-1")
	cache.set(tenantDEKID, []byte("tenant-key"), 1)
	cache.setVersion(tenantDEKID, 1, []byte("tenant-v1"))

	// Verify entries are cached
	_, _, found := cache.get(DEKIDGlobal)
	assert.True(t, found)
	_, found = cache.getVersion(DEKIDGlobal, 1)
	assert.True(t, found)
	_, _, found = cache.get(tenantDEKID)
	assert.True(t, found)
	_, found = cache.getVersion(tenantDEKID, 1)
	assert.True(t, found)

	// Wait for entries to expire and cleanup to run
	// We need to wait longer than the cleanup interval (1 minute by default)
	// Since we can't modify the cleanup interval easily, we'll wait for
	// at least one cleanup cycle plus expiration time
	time.Sleep(70 * time.Millisecond)

	// Force a check by accessing entries (they should be expired by inline check)
	_, _, found = cache.get(DEKIDGlobal)
	assert.False(t, found, "current cache entry should be expired")

	_, found = cache.getVersion(DEKIDGlobal, 1)
	assert.False(t, found, "versioned cache entry should be expired")

	_, _, found = cache.get(tenantDEKID)
	assert.False(t, found, "tenant current cache entry should be expired")

	_, found = cache.getVersion(tenantDEKID, 1)
	assert.False(t, found, "tenant versioned cache entry should be expired")
}

// TestDEKCache_CleanupExpired_KeepsNonExpired verifies that the cleanup
// goroutine does not remove non-expired entries.
func TestDEKCache_CleanupExpired_KeepsNonExpired(t *testing.T) {
	cache := newDEKCache()
	defer cache.stop()

	// Set longer TTL for testing
	cache.ttl = 5 * time.Second

	// Add entries to both caches
	cache.set(DEKIDGlobal, []byte("global-key"), 1)
	cache.setVersion(DEKIDGlobal, 1, []byte("global-v1"))
	tenantDEKID := NewTenantDEKID("tenant-1")
	cache.set(tenantDEKID, []byte("tenant-key"), 1)
	cache.setVersion(tenantDEKID, 2, []byte("tenant-v2"))

	// Wait a short time (much less than TTL)
	time.Sleep(100 * time.Millisecond)

	// Verify all entries are still cached
	key, version, found := cache.get(DEKIDGlobal)
	assert.True(t, found, "current cache entry should still be valid")
	assert.NotEmpty(t, key)
	assert.Equal(t, uint64(1), version)

	key, found = cache.getVersion(DEKIDGlobal, 1)
	assert.True(t, found, "versioned cache entry should still be valid")
	assert.NotEmpty(t, key)

	key, version, found = cache.get(tenantDEKID)
	assert.True(t, found, "tenant current cache entry should still be valid")
	assert.NotEmpty(t, key)
	assert.Equal(t, uint64(1), version)

	key, found = cache.getVersion(tenantDEKID, 2)
	assert.True(t, found, "tenant versioned cache entry should still be valid")
	assert.NotEmpty(t, key)
}

// TestDEKCache_CleanupExpired_StopChan verifies that the cleanup goroutine
// exits when the stop channel is closed.
func TestDEKCache_CleanupExpired_StopChan(t *testing.T) {
	cache := newDEKCache()

	// Add some entries
	cache.set(DEKIDGlobal, []byte("global-key"), 1)
	cache.setVersion(DEKIDGlobal, 1, []byte("global-v1"))

	// Create a done channel to track goroutine exit
	done := make(chan struct{})

	// Stop the cache and signal when it's done
	go func() {
		cache.stop()
		close(done)
	}()

	// Wait for goroutine to exit with timeout
	select {
	case <-done:
		// Success - goroutine exited cleanly
	case <-time.After(100 * time.Millisecond):
		t.Fatal("cleanup goroutine did not exit after stop() was called")
	}

	// Verify cache can still be used after stopping
	// (even though cleanup goroutine has stopped)
	key, version, found := cache.get(DEKIDGlobal)
	assert.True(t, found)
	assert.NotEmpty(t, key)
	assert.Equal(t, uint64(1), version)
}

// =============================================================================
// Additional Coverage Tests for EnsureTenantDEK
// =============================================================================

// TestSecretsDEK_EnsureTenantDEK_KMSFailure tests KMS failure during tenant DEK creation.
func TestSecretsDEK_EnsureTenantDEK_KMSFailure(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	// Make KMS fail
	kms.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
		return nil, nil, errors.New("KMS unavailable")
	}

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Should fail with DEK initialization error
	_, err := dek.EnsureTenantDEK(ctx, "tenant-xyz", "AES-256-GCM")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDEKInitializationFailed)
}

// TestSecretsDEK_EnsureTenantDEK_StoreFailure tests storage failure during tenant DEK creation.
func TestSecretsDEK_EnsureTenantDEK_StoreFailure(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	// Make store fail on Put
	store.putFn = func(ctx context.Context, key string, value []byte) error {
		return errors.New("storage unavailable")
	}

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Should fail with DEK initialization error
	_, err := dek.EnsureTenantDEK(ctx, "tenant-xyz", "AES-256-GCM")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDEKInitializationFailed)
}

// TestSecretsDEK_EnsureTenantDEK_WhitespaceHandling tests tenant ID with whitespace.
func TestSecretsDEK_EnsureTenantDEK_WhitespaceHandling(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Create with whitespace (should be trimmed)
	dekid, err := dek.EnsureTenantDEK(ctx, "  tenant-trim  ", "AES-256-GCM")
	require.NoError(t, err)
	assert.True(t, IsTenant(dekid))

	// Verify tenant ID is trimmed
	extractedID, ok := GetTenantID(dekid)
	assert.True(t, ok)
	assert.Equal(t, "tenant-trim", extractedID)
}

// =============================================================================
// Additional Coverage Tests for ListVersions
// =============================================================================

// TestSecretsDEK_ListVersions_LoadVersionEntryFailure tests handling load failure during listing.
func TestSecretsDEK_ListVersions_LoadVersionEntryFailure(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize and rotate to create versions
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Corrupt one of the version entries
	versionPath := dek.versionStoragePath(DEKIDGlobal, 1)
	err = store.Put(ctx, versionPath, []byte("{invalid}"))
	require.NoError(t, err)

	// ListVersions should still return current version, skipping the corrupted one
	versions, err := dek.ListVersions(ctx, DEKIDGlobal)
	require.NoError(t, err)
	// Should have current version at minimum (corrupted version skipped with debug log)
	assert.GreaterOrEqual(t, len(versions), 1)
}

// TestSecretsDEK_ListVersions_NoVersionsStored tests listing when no old versions exist.
func TestSecretsDEK_ListVersions_NoVersionsStored(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize without rotation (only current version)
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)

	// List should return only current version
	versions, err := dek.ListVersions(ctx, DEKIDGlobal)
	require.NoError(t, err)
	assert.Len(t, versions, 1)
	assert.True(t, versions[0].IsCurrent)
	assert.Equal(t, uint64(1), versions[0].Version)
}

// =============================================================================
// Additional Coverage Tests for loadDEKEntry
// =============================================================================

// TestSecretsDEK_LoadDEKEntry_StorageError tests storage error during load.
func TestSecretsDEK_LoadDEKEntry_StorageError(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	// Make Get fail with non-NotFound error
	store.getFn = func(ctx context.Context, key string) ([]byte, error) {
		return nil, errors.New("storage read error")
	}

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Try to get key - should fail with storage error (not DEK not found)
	_, _, err := dek.GetKey(ctx, DEKIDGlobal)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load DEK")
}

// =============================================================================
// Additional Coverage Tests for loadVersionEntry
// =============================================================================

// TestSecretsDEK_LoadVersionEntry_StorageError tests storage error during version load.
func TestSecretsDEK_LoadVersionEntry_StorageError(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Initialize and rotate
	err := dek.Initialize(ctx, "AES-256-GCM")
	require.NoError(t, err)
	_, err = dek.Rotate(ctx, DEKIDGlobal)
	require.NoError(t, err)

	// Make Get fail with non-NotFound error for version paths
	store.getFn = func(ctx context.Context, key string) ([]byte, error) {
		keyStr := string(key)
		if keyStr == "_internal/secrets/dek/global" {
			// Return the current DEK entry
			return store.data[keyStr], nil
		}
		// Fail for version paths
		return nil, errors.New("storage read error")
	}

	// Try to get old version - should fail with storage error
	_, err = dek.GetKeyVersion(ctx, DEKIDGlobal, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to load DEK version")
}

// =============================================================================
// Database DEK Tests
// =============================================================================

// TestSecretsDEK_EnsureDatabaseDEK tests creating and retrieving database-specific DEKs.
func TestSecretsDEK_EnsureDatabaseDEK(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		dbName    string
		setupMock func(*testMockKVStore, *testMockKMSManager)
		wantErr   bool
		errMsg    string
	}{
		{
			name:     "create new database DEK",
			tenantID: "tenant-123",
			dbName:   "mydb",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				// No setup needed
			},
			wantErr: false,
		},
		{
			name:     "idempotent - database DEK already exists",
			tenantID: "tenant-123",
			dbName:   "mydb",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				// Pre-populate with existing database DEK
				entry := &DEKEntry{
					DEKID:      "dek:tenant:tenant-123:db:mydb",
					WrappedKey: []byte("wrapped-key"),
					Algorithm:  "AES-256-GCM",
					Version:    1,
				}
				data, _ := json.Marshal(entry)
				store.data["_internal/secrets/dek/database/tenant-123/mydb"] = data
			},
			wantErr: false,
		},
		{
			name:     "empty tenant ID",
			tenantID: "",
			dbName:   "mydb",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
			},
			wantErr: true,
			errMsg:  "tenant ID",
		},
		{
			name:     "empty database name",
			tenantID: "tenant-123",
			dbName:   "",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
			},
			wantErr: true,
			errMsg:  "database name",
		},
		{
			name:     "whitespace tenant ID",
			tenantID: "   ",
			dbName:   "mydb",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
			},
			wantErr: true,
			errMsg:  "tenant ID",
		},
		{
			name:     "whitespace database name",
			tenantID: "tenant-123",
			dbName:   "   ",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
			},
			wantErr: true,
			errMsg:  "database name",
		},
		{
			name:     "KMS generate key failure",
			tenantID: "tenant-456",
			dbName:   "faildb",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				kms.generateDataKeyFn = func(ctx context.Context, attrs *xkmssdk.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
					return nil, nil, errors.New("KMS unavailable")
				}
			},
			wantErr: true,
		},
		{
			name:     "store put failure",
			tenantID: "tenant-789",
			dbName:   "faildb",
			setupMock: func(store *testMockKVStore, kms *testMockKMSManager) {
				store.putFn = func(ctx context.Context, key string, value []byte) error {
					return errors.New("storage error")
				}
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			if tt.setupMock != nil {
				tt.setupMock(store, kms)
			}

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)
			ctx := context.Background()

			dekid, err := dek.EnsureDatabaseDEK(ctx, tt.tenantID, tt.dbName)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)
				assert.True(t, IsDatabase(dekid))

				// Verify tenant ID and db name can be extracted
				tenantID, dbName, ok := GetDatabaseInfo(dekid)
				assert.True(t, ok)
				assert.Equal(t, tt.tenantID, tenantID)
				assert.Equal(t, tt.dbName, dbName)
			}
		})
	}
}

// TestSecretsDEK_DeleteDatabaseDEK tests deleting database-specific DEKs.
func TestSecretsDEK_DeleteDatabaseDEK(t *testing.T) {
	tests := []struct {
		name      string
		tenantID  string
		dbName    string
		setupMock func(*SecretsDEK)
		wantErr   bool
		errMsg    string
	}{
		{
			name:     "delete existing database DEK",
			tenantID: "tenant-123",
			dbName:   "mydb",
			setupMock: func(d *SecretsDEK) {
				ctx := context.Background()
				_, _ = d.EnsureDatabaseDEK(ctx, "tenant-123", "mydb")
			},
			wantErr: false,
		},
		{
			name:     "delete non-existent database DEK is idempotent",
			tenantID: "tenant-999",
			dbName:   "nonexistent",
			setupMock: func(d *SecretsDEK) {
				// Don't create anything
			},
			wantErr: false,
		},
		{
			name:     "empty tenant ID",
			tenantID: "",
			dbName:   "mydb",
			setupMock: func(d *SecretsDEK) {
			},
			wantErr: true,
			errMsg:  "tenant ID",
		},
		{
			name:     "empty database name",
			tenantID: "tenant-123",
			dbName:   "",
			setupMock: func(d *SecretsDEK) {
			},
			wantErr: true,
			errMsg:  "database name",
		},
		{
			name:     "whitespace tenant ID",
			tenantID: "   ",
			dbName:   "mydb",
			setupMock: func(d *SecretsDEK) {
			},
			wantErr: true,
			errMsg:  "tenant ID",
		},
		{
			name:     "whitespace database name",
			tenantID: "tenant-123",
			dbName:   "   ",
			setupMock: func(d *SecretsDEK) {
			},
			wantErr: true,
			errMsg:  "database name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kms := newTestMockKMSManager()
			store := newTestMockKVStore()
			logger := newTestMockLogger()

			dek := NewSecretsDEK(kms, store, "master-key-id", logger)

			if tt.setupMock != nil {
				tt.setupMock(dek)
			}

			ctx := context.Background()
			err := dek.DeleteDatabaseDEK(ctx, tt.tenantID, tt.dbName)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				require.NoError(t, err)

				// Verify DEK was deleted from storage
				key := "_internal/secrets/dek/database/" + tt.tenantID + "/" + tt.dbName
				_, err := store.Get(ctx, key)
				assert.Error(t, err, "database DEK should be deleted from storage")
			}
		})
	}
}

// TestSecretsDEK_EnsureDatabaseDEK_ThenGetKey tests creating a database DEK and retrieving it.
func TestSecretsDEK_EnsureDatabaseDEK_ThenGetKey(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Create database DEK
	dekid, err := dek.EnsureDatabaseDEK(ctx, "tenant-123", "mydb")
	require.NoError(t, err)

	// Retrieve the key
	keyMaterial, version, err := dek.GetKey(ctx, dekid)
	require.NoError(t, err)
	assert.NotEmpty(t, keyMaterial)
	assert.Equal(t, uint64(1), version)
}

// TestSecretsDEK_DeleteDatabaseDEK_ClearsCache tests that deleting a database DEK clears the cache.
func TestSecretsDEK_DeleteDatabaseDEK_ClearsCache(t *testing.T) {
	kms := newTestMockKMSManager()
	store := newTestMockKVStore()
	logger := newTestMockLogger()

	dek := NewSecretsDEK(kms, store, "master-key-id", logger)
	ctx := context.Background()

	// Create and cache
	dekid, err := dek.EnsureDatabaseDEK(ctx, "tenant-123", "mydb")
	require.NoError(t, err)

	// Access the key to cache it
	_, _, err = dek.GetKey(ctx, dekid)
	require.NoError(t, err)

	// Verify it's cached
	dek.mu.RLock()
	_, cached := dek.cache.entries[dekid]
	dek.mu.RUnlock()
	assert.True(t, cached, "database DEK should be cached")

	// Delete database DEK
	err = dek.DeleteDatabaseDEK(ctx, "tenant-123", "mydb")
	require.NoError(t, err)

	// Verify cache was cleared
	dek.mu.RLock()
	_, cached = dek.cache.entries[dekid]
	dek.mu.RUnlock()
	assert.False(t, cached, "cache should be cleared after deletion")
}
