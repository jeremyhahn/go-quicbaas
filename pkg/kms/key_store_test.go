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

package kms

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Test Helpers
// =============================================================================

func createTestMetadata(keyID string) *KeyMetadata {
	now := time.Now()
	return &KeyMetadata{
		KeyID:              keyID,
		Name:               "test-key",
		Description:        "Test key for unit tests",
		Algorithm:          AlgAES256GCM,
		State:              KeyStateEnabled,
		Usage:              []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt},
		Version:            1,
		Exportable:         false,
		DeletionProtection: false,
		Created:            now,
		Updated:            now,
		Tags: map[string]string{
			"environment": "test",
			"service":     "kms",
		},
		Owner: "test-owner",
	}
}

func createTestMetadataWithRotation(keyID string, rotationTime time.Time) *KeyMetadata {
	metadata := createTestMetadata(keyID)
	metadata.RotationPolicy = &RotationPolicy{
		Enabled:        true,
		Interval:       24 * time.Hour,
		NextRotation:   rotationTime,
		RetainVersions: 5,
		AutoRotate:     true,
	}
	return metadata
}

// =============================================================================
// NewKeyStore Tests
// =============================================================================

func TestNewKeyStore(t *testing.T) {
	t.Run("creates non-nil store", func(t *testing.T) {
		store := NewKeyStore()
		assert.NotNil(t, store, "NewKeyStore should return non-nil store")
		assert.NotNil(t, store.keys, "keys map should be initialized")
		assert.NotNil(t, store.versionCounter, "versionCounter map should be initialized")
		assert.NotNil(t, store.usageCounter, "usageCounter map should be initialized")
	})

	t.Run("store is empty initially", func(t *testing.T) {
		store := NewKeyStore()
		assert.Empty(t, store.keys, "new store should have no keys")
		assert.Empty(t, store.versionCounter, "new store should have no version counters")
		assert.Empty(t, store.usageCounter, "new store should have no usage counters")
	})
}

// =============================================================================
// Put/Get Tests
// =============================================================================

func TestKeyStore_Put(t *testing.T) {
	tests := []struct {
		name      string
		metadata  *KeyMetadata
		wantError bool
		errorMsg  string
	}{
		{
			name:      "valid metadata",
			metadata:  createTestMetadata("key-1"),
			wantError: false,
		},
		{
			name:      "nil metadata",
			metadata:  nil,
			wantError: true,
			errorMsg:  "metadata cannot be nil",
		},
		{
			name: "empty keyID",
			metadata: &KeyMetadata{
				Name:      "test",
				Algorithm: AlgAES256GCM,
			},
			wantError: true,
			errorMsg:  "keyID cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewKeyStore()
			err := store.Put(tt.metadata)

			if tt.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errorMsg)
			} else {
				require.NoError(t, err)
				// Verify the key was stored
				stored, err := store.Get(tt.metadata.KeyID)
				require.NoError(t, err)
				assert.Equal(t, tt.metadata.KeyID, stored.KeyID)
			}
		})
	}
}

func TestKeyStore_PutAndGet(t *testing.T) {
	t.Run("put and get key", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Equal(t, metadata.KeyID, retrieved.KeyID)
		assert.Equal(t, metadata.Name, retrieved.Name)
		assert.Equal(t, metadata.Algorithm, retrieved.Algorithm)
		assert.Equal(t, metadata.State, retrieved.State)
		assert.Equal(t, metadata.Version, retrieved.Version)
	})

	t.Run("get non-existent key returns error", func(t *testing.T) {
		store := NewKeyStore()

		retrieved, err := store.Get("non-existent")
		require.Error(t, err)
		assert.Nil(t, retrieved)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("get with empty keyID returns error", func(t *testing.T) {
		store := NewKeyStore()

		retrieved, err := store.Get("")
		require.Error(t, err)
		assert.Nil(t, retrieved)
		assert.Contains(t, err.Error(), "keyID cannot be empty")
	})

	t.Run("update existing key", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		// Initial put
		err := store.Put(metadata)
		require.NoError(t, err)

		// Update metadata
		metadata.Name = "updated-name"
		metadata.State = KeyStateDisabled
		err = store.Put(metadata)
		require.NoError(t, err)

		// Verify update
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Equal(t, "updated-name", retrieved.Name)
		assert.Equal(t, KeyStateDisabled, retrieved.State)
	})
}

func TestKeyStore_DeepCopyOnPut(t *testing.T) {
	t.Run("mutation does not affect stored copy", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		originalName := metadata.Name

		err := store.Put(metadata)
		require.NoError(t, err)

		// Mutate original metadata
		metadata.Name = "mutated-name"
		metadata.State = KeyStateDisabled
		metadata.Tags["new-tag"] = "new-value"

		// Verify stored copy is unchanged
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Equal(t, originalName, retrieved.Name)
		assert.Equal(t, KeyStateEnabled, retrieved.State)
		assert.NotContains(t, retrieved.Tags, "new-tag")
	})
}

func TestKeyStore_DeepCopyOnGet(t *testing.T) {
	t.Run("mutation does not affect stored copy", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		originalName := metadata.Name

		err := store.Put(metadata)
		require.NoError(t, err)

		// Get and mutate
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		retrieved.Name = "mutated-name"
		retrieved.State = KeyStateDisabled
		retrieved.Tags["new-tag"] = "new-value"

		// Verify stored copy is unchanged
		retrieved2, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Equal(t, originalName, retrieved2.Name)
		assert.Equal(t, KeyStateEnabled, retrieved2.State)
		assert.NotContains(t, retrieved2.Tags, "new-tag")
	})
}

// =============================================================================
// Delete Tests
// =============================================================================

func TestKeyStore_Delete(t *testing.T) {
	t.Run("delete existing key", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		err = store.Delete("key-1")
		require.NoError(t, err)

		// Verify key is deleted
		_, err = store.Get("key-1")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("delete non-existent key returns error", func(t *testing.T) {
		store := NewKeyStore()

		err := store.Delete("non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("delete with empty keyID returns error", func(t *testing.T) {
		store := NewKeyStore()

		err := store.Delete("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyID cannot be empty")
	})

	t.Run("deleted key cannot be retrieved", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		// Increment version and usage to ensure counters are cleaned up
		_, err = store.IncrementVersion("key-1")
		require.NoError(t, err)
		err = store.IncrementUsage("key-1")
		require.NoError(t, err)

		err = store.Delete("key-1")
		require.NoError(t, err)

		// Verify key is gone
		_, err = store.Get("key-1")
		require.Error(t, err)

		// Verify counters are cleaned up
		assert.NotContains(t, store.versionCounter, "key-1")
		assert.NotContains(t, store.usageCounter, "key-1")
	})
}

// =============================================================================
// List Tests
// =============================================================================

func TestKeyStore_List(t *testing.T) {
	t.Run("list with empty store", func(t *testing.T) {
		store := NewKeyStore()
		results := store.List("", 0)
		assert.Empty(t, results)
	})

	t.Run("list with multiple keys", func(t *testing.T) {
		store := NewKeyStore()

		// Add multiple keys
		for i := 1; i <= 5; i++ {
			metadata := createTestMetadata(string(rune('0' + i)))
			err := store.Put(metadata)
			require.NoError(t, err)
		}

		results := store.List("", 0)
		assert.Len(t, results, 5)
	})

	t.Run("list with prefix filter", func(t *testing.T) {
		store := NewKeyStore()

		// Add keys with different prefixes
		prefixes := []string{"app-key-1", "app-key-2", "db-key-1", "db-key-2", "cache-key-1"}
		for _, prefix := range prefixes {
			metadata := createTestMetadata(prefix)
			err := store.Put(metadata)
			require.NoError(t, err)
		}

		// Filter by "app-" prefix
		results := store.List("app-", 0)
		assert.Len(t, results, 2)
		for _, r := range results {
			assert.Contains(t, r.KeyID, "app-")
		}

		// Filter by "db-" prefix
		results = store.List("db-", 0)
		assert.Len(t, results, 2)
		for _, r := range results {
			assert.Contains(t, r.KeyID, "db-")
		}
	})

	t.Run("list with limit", func(t *testing.T) {
		store := NewKeyStore()

		// Add multiple keys
		for i := 1; i <= 10; i++ {
			metadata := createTestMetadata(string(rune('0' + i)))
			err := store.Put(metadata)
			require.NoError(t, err)
		}

		// List with limit
		results := store.List("", 5)
		assert.Len(t, results, 5)

		// List with limit larger than total
		results = store.List("", 20)
		assert.Len(t, results, 10)
	})

	t.Run("results are sorted", func(t *testing.T) {
		store := NewKeyStore()

		// Add keys in random order
		keys := []string{"key-3", "key-1", "key-5", "key-2", "key-4"}
		for _, key := range keys {
			metadata := createTestMetadata(key)
			err := store.Put(metadata)
			require.NoError(t, err)
		}

		results := store.List("", 0)
		assert.Len(t, results, 5)

		// Verify sorted order
		for i := 0; i < len(results)-1; i++ {
			assert.True(t, results[i].KeyID < results[i+1].KeyID,
				"results should be sorted: %s < %s", results[i].KeyID, results[i+1].KeyID)
		}
	})
}

// =============================================================================
// Query Tests - GetByState
// =============================================================================

func TestKeyStore_GetByState(t *testing.T) {
	tests := []struct {
		name      string
		setupKeys []struct {
			keyID  string
			status KeyState
		}
		queryState    KeyState
		expectedCount int
	}{
		{
			name: "filter by enabled",
			setupKeys: []struct {
				keyID  string
				status KeyState
			}{
				{"key-1", KeyStateEnabled},
				{"key-2", KeyStateEnabled},
				{"key-3", KeyStateDisabled},
			},
			queryState:    KeyStateEnabled,
			expectedCount: 2,
		},
		{
			name: "filter by disabled",
			setupKeys: []struct {
				keyID  string
				status KeyState
			}{
				{"key-1", KeyStateEnabled},
				{"key-2", KeyStateDisabled},
				{"key-3", KeyStateDisabled},
			},
			queryState:    KeyStateDisabled,
			expectedCount: 2,
		},
		{
			name: "filter by pending deletion",
			setupKeys: []struct {
				keyID  string
				status KeyState
			}{
				{"key-1", KeyStateEnabled},
				{"key-2", KeyStatePendingDeletion},
				{"key-3", KeyStatePendingDeletion},
			},
			queryState:    KeyStatePendingDeletion,
			expectedCount: 2,
		},
		{
			name: "no matches",
			setupKeys: []struct {
				keyID  string
				status KeyState
			}{
				{"key-1", KeyStateEnabled},
				{"key-2", KeyStateEnabled},
			},
			queryState:    KeyStateDisabled,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewKeyStore()

			// Setup keys
			for _, sk := range tt.setupKeys {
				metadata := createTestMetadata(sk.keyID)
				metadata.State = sk.status
				err := store.Put(metadata)
				require.NoError(t, err)
			}

			// Query by state
			results := store.GetByState(tt.queryState)
			assert.Len(t, results, tt.expectedCount)

			// Verify all results have correct state
			for _, r := range results {
				assert.Equal(t, tt.queryState, r.State)
			}
		})
	}
}

// =============================================================================
// Query Tests - GetExpiring
// =============================================================================

func TestKeyStore_GetExpiring(t *testing.T) {
	now := time.Now()

	t.Run("keys expiring within duration", func(t *testing.T) {
		store := NewKeyStore()

		// Key expiring in 1 hour
		metadata1 := createTestMetadataWithRotation("key-1", now.Add(1*time.Hour))
		err := store.Put(metadata1)
		require.NoError(t, err)

		// Key expiring in 23 hours
		metadata2 := createTestMetadataWithRotation("key-2", now.Add(23*time.Hour))
		err = store.Put(metadata2)
		require.NoError(t, err)

		// Key expiring in 25 hours (outside 24h window)
		metadata3 := createTestMetadataWithRotation("key-3", now.Add(25*time.Hour))
		err = store.Put(metadata3)
		require.NoError(t, err)

		// Query keys expiring within 24 hours
		results := store.GetExpiring(24 * time.Hour)
		assert.Len(t, results, 2)
		assert.Equal(t, "key-1", results[0].KeyID)
		assert.Equal(t, "key-2", results[1].KeyID)
	})

	t.Run("exclude keys without rotation policy", func(t *testing.T) {
		store := NewKeyStore()

		// Key with rotation policy
		metadata1 := createTestMetadataWithRotation("key-1", now.Add(1*time.Hour))
		err := store.Put(metadata1)
		require.NoError(t, err)

		// Key without rotation policy
		metadata2 := createTestMetadata("key-2")
		err = store.Put(metadata2)
		require.NoError(t, err)

		results := store.GetExpiring(24 * time.Hour)
		assert.Len(t, results, 1)
		assert.Equal(t, "key-1", results[0].KeyID)
	})

	t.Run("exclude keys with zero next rotation", func(t *testing.T) {
		store := NewKeyStore()

		// Key with zero next rotation
		metadata := createTestMetadata("key-1")
		metadata.RotationPolicy = &RotationPolicy{
			Enabled:      true,
			Interval:     24 * time.Hour,
			NextRotation: time.Time{}, // Zero value
		}
		err := store.Put(metadata)
		require.NoError(t, err)

		results := store.GetExpiring(24 * time.Hour)
		assert.Empty(t, results)
	})

	t.Run("results sorted by next rotation time", func(t *testing.T) {
		store := NewKeyStore()

		// Add keys in reverse order
		metadata3 := createTestMetadataWithRotation("key-3", now.Add(3*time.Hour))
		err := store.Put(metadata3)
		require.NoError(t, err)

		metadata1 := createTestMetadataWithRotation("key-1", now.Add(1*time.Hour))
		err = store.Put(metadata1)
		require.NoError(t, err)

		metadata2 := createTestMetadataWithRotation("key-2", now.Add(2*time.Hour))
		err = store.Put(metadata2)
		require.NoError(t, err)

		results := store.GetExpiring(24 * time.Hour)
		assert.Len(t, results, 3)
		assert.Equal(t, "key-1", results[0].KeyID)
		assert.Equal(t, "key-2", results[1].KeyID)
		assert.Equal(t, "key-3", results[2].KeyID)
	})
}

// =============================================================================
// Query Tests - GetPendingDeletion
// =============================================================================

func TestKeyStore_GetPendingDeletion(t *testing.T) {
	t.Run("returns only pending deletion keys", func(t *testing.T) {
		store := NewKeyStore()

		// Add keys with different states
		states := []struct {
			keyID  string
			status KeyState
		}{
			{"key-1", KeyStateEnabled},
			{"key-2", KeyStatePendingDeletion},
			{"key-3", KeyStateDisabled},
			{"key-4", KeyStatePendingDeletion},
		}

		for _, s := range states {
			metadata := createTestMetadata(s.keyID)
			metadata.State = s.status
			err := store.Put(metadata)
			require.NoError(t, err)
		}

		results := store.GetPendingDeletion()
		assert.Len(t, results, 2)

		for _, r := range results {
			assert.Equal(t, KeyStatePendingDeletion, r.State)
		}
	})
}

// =============================================================================
// Version Tracking Tests
// =============================================================================

func TestKeyStore_IncrementVersion(t *testing.T) {
	t.Run("increment increases version", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Version = 1

		err := store.Put(metadata)
		require.NoError(t, err)

		// Increment version
		newVersion, err := store.IncrementVersion("key-1")
		require.NoError(t, err)
		assert.Equal(t, 2, newVersion)

		// Verify metadata is updated
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Equal(t, 2, retrieved.Version)
	})

	t.Run("increment on non-existent key errors", func(t *testing.T) {
		store := NewKeyStore()

		_, err := store.IncrementVersion("non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("increment with empty keyID errors", func(t *testing.T) {
		store := NewKeyStore()

		_, err := store.IncrementVersion("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyID cannot be empty")
	})

	t.Run("multiple increments", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Version = 1

		err := store.Put(metadata)
		require.NoError(t, err)

		// Increment multiple times
		for i := 2; i <= 5; i++ {
			version, err := store.IncrementVersion("key-1")
			require.NoError(t, err)
			assert.Equal(t, i, version)
		}

		// Verify final version
		version, err := store.GetVersion("key-1")
		require.NoError(t, err)
		assert.Equal(t, 5, version)
	})

	t.Run("increment initializes missing counter", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Version = 5

		err := store.Put(metadata)
		require.NoError(t, err)

		// Manually remove version counter to simulate missing counter
		delete(store.versionCounter, "key-1")

		// Increment should initialize from metadata version
		newVersion, err := store.IncrementVersion("key-1")
		require.NoError(t, err)
		assert.Equal(t, 6, newVersion)
	})
}

func TestKeyStore_GetVersion(t *testing.T) {
	t.Run("returns current version", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Version = 42

		err := store.Put(metadata)
		require.NoError(t, err)

		version, err := store.GetVersion("key-1")
		require.NoError(t, err)
		assert.Equal(t, 42, version)
	})

	t.Run("get version on non-existent key errors", func(t *testing.T) {
		store := NewKeyStore()

		_, err := store.GetVersion("non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("get version with empty keyID errors", func(t *testing.T) {
		store := NewKeyStore()

		_, err := store.GetVersion("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyID cannot be empty")
	})
}

// =============================================================================
// Usage Tracking Tests
// =============================================================================

func TestKeyStore_IncrementUsage(t *testing.T) {
	t.Run("increment increases count", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		// Increment usage
		err = store.IncrementUsage("key-1")
		require.NoError(t, err)

		// Verify usage count
		usage, err := store.GetUsage("key-1")
		require.NoError(t, err)
		assert.Equal(t, uint64(1), usage)
	})

	t.Run("increment on non-existent key errors", func(t *testing.T) {
		store := NewKeyStore()

		err := store.IncrementUsage("non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("increment with empty keyID errors", func(t *testing.T) {
		store := NewKeyStore()

		err := store.IncrementUsage("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyID cannot be empty")
	})

	t.Run("multiple increments", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		// Increment multiple times
		for i := 0; i < 100; i++ {
			err := store.IncrementUsage("key-1")
			require.NoError(t, err)
		}

		// Verify final usage count
		usage, err := store.GetUsage("key-1")
		require.NoError(t, err)
		assert.Equal(t, uint64(100), usage)
	})

	t.Run("increment initializes missing counter", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		// Manually remove usage counter to simulate missing counter
		delete(store.usageCounter, "key-1")

		// Increment should initialize counter
		err = store.IncrementUsage("key-1")
		require.NoError(t, err)

		usage, err := store.GetUsage("key-1")
		require.NoError(t, err)
		assert.Equal(t, uint64(1), usage)
	})
}

func TestKeyStore_GetUsage(t *testing.T) {
	t.Run("returns current count", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		// Initial usage should be 0
		usage, err := store.GetUsage("key-1")
		require.NoError(t, err)
		assert.Equal(t, uint64(0), usage)

		// Increment and verify
		err = store.IncrementUsage("key-1")
		require.NoError(t, err)

		usage, err = store.GetUsage("key-1")
		require.NoError(t, err)
		assert.Equal(t, uint64(1), usage)
	})

	t.Run("get usage on non-existent key errors", func(t *testing.T) {
		store := NewKeyStore()

		_, err := store.GetUsage("non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("get usage with empty keyID errors", func(t *testing.T) {
		store := NewKeyStore()

		_, err := store.GetUsage("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyID cannot be empty")
	})
}

// =============================================================================
// State Management Tests
// =============================================================================

func TestKeyStore_SetState(t *testing.T) {
	t.Run("set state changes state", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.State = KeyStateEnabled

		err := store.Put(metadata)
		require.NoError(t, err)

		// Change state to disabled
		err = store.SetState("key-1", KeyStateDisabled)
		require.NoError(t, err)

		// Verify state changed
		state, err := store.GetState("key-1")
		require.NoError(t, err)
		assert.Equal(t, KeyStateDisabled, state)
	})

	t.Run("set state updates UpdatedAt", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		originalTime := metadata.Updated

		err := store.Put(metadata)
		require.NoError(t, err)

		// Wait a bit to ensure time difference
		time.Sleep(10 * time.Millisecond)

		// Change state
		err = store.SetState("key-1", KeyStateDisabled)
		require.NoError(t, err)

		// Verify UpdatedAt changed
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.True(t, retrieved.Updated.After(originalTime))
	})

	t.Run("set state on non-existent key errors", func(t *testing.T) {
		store := NewKeyStore()

		err := store.SetState("non-existent", KeyStateDisabled)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("set state with empty keyID errors", func(t *testing.T) {
		store := NewKeyStore()

		err := store.SetState("", KeyStateDisabled)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyID cannot be empty")
	})
}

func TestKeyStore_GetState(t *testing.T) {
	t.Run("returns current state", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.State = KeyStateEnabled

		err := store.Put(metadata)
		require.NoError(t, err)

		state, err := store.GetState("key-1")
		require.NoError(t, err)
		assert.Equal(t, KeyStateEnabled, state)
	})

	t.Run("get state on non-existent key errors", func(t *testing.T) {
		store := NewKeyStore()

		_, err := store.GetState("non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key not found")
	})

	t.Run("get state with empty keyID errors", func(t *testing.T) {
		store := NewKeyStore()

		_, err := store.GetState("")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "keyID cannot be empty")
	})
}

// =============================================================================
// copyMetadata Tests - Edge Cases
// =============================================================================

func TestKeyStore_CopyMetadata(t *testing.T) {
	t.Run("nil metadata returns nil", func(t *testing.T) {
		store := NewKeyStore()
		result := store.copyMetadata(nil)
		assert.Nil(t, result)
	})

	t.Run("metadata with nil usage slice", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Usage = nil

		err := store.Put(metadata)
		require.NoError(t, err)

		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Nil(t, retrieved.Usage)
	})

	t.Run("metadata with nil tags map", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Tags = nil

		err := store.Put(metadata)
		require.NoError(t, err)

		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Nil(t, retrieved.Tags)
	})

	t.Run("metadata with nil LastUsedAt", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.LastUsedAt = nil

		err := store.Put(metadata)
		require.NoError(t, err)

		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Nil(t, retrieved.LastUsedAt)
	})

	t.Run("metadata with nil RotationPolicy", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.RotationPolicy = nil

		err := store.Put(metadata)
		require.NoError(t, err)

		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Nil(t, retrieved.RotationPolicy)
	})

	t.Run("metadata with all nil optional fields", func(t *testing.T) {
		store := NewKeyStore()
		metadata := &KeyMetadata{
			KeyID:          "key-1",
			Name:           "test",
			Algorithm:      AlgAES256GCM,
			State:          KeyStateEnabled,
			Version:        1,
			Created:        time.Now(),
			Updated:        time.Now(),
			Usage:          nil,
			Tags:           nil,
			LastUsedAt:     nil,
			RotationPolicy: nil,
		}

		err := store.Put(metadata)
		require.NoError(t, err)

		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.NotNil(t, retrieved)
		assert.Nil(t, retrieved.Usage)
		assert.Nil(t, retrieved.Tags)
		assert.Nil(t, retrieved.LastUsedAt)
		assert.Nil(t, retrieved.RotationPolicy)
	})
}

// =============================================================================
// Concurrency Tests
// =============================================================================

func TestKeyStore_ConcurrentPut(t *testing.T) {
	t.Run("concurrent put operations", func(t *testing.T) {
		store := NewKeyStore()
		numGoroutines := 100
		var wg sync.WaitGroup

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				metadata := createTestMetadata(string(rune('0' + id)))
				err := store.Put(metadata)
				assert.NoError(t, err)
			}(i)
		}

		wg.Wait()

		// Verify all keys were stored
		results := store.List("", 0)
		assert.Len(t, results, numGoroutines)
	})
}

func TestKeyStore_ConcurrentGet(t *testing.T) {
	t.Run("concurrent get operations", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		numGoroutines := 100
		var wg sync.WaitGroup

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				retrieved, err := store.Get("key-1")
				assert.NoError(t, err)
				assert.NotNil(t, retrieved)
				assert.Equal(t, "key-1", retrieved.KeyID)
			}()
		}

		wg.Wait()
	})
}

func TestKeyStore_ConcurrentIncrementUsage(t *testing.T) {
	t.Run("concurrent increment usage", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")

		err := store.Put(metadata)
		require.NoError(t, err)

		numGoroutines := 100
		incrementsPerGoroutine := 10
		var wg sync.WaitGroup

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				for j := 0; j < incrementsPerGoroutine; j++ {
					err := store.IncrementUsage("key-1")
					assert.NoError(t, err)
				}
			}()
		}

		wg.Wait()

		// Verify final count
		usage, err := store.GetUsage("key-1")
		require.NoError(t, err)
		expected := uint64(numGoroutines * incrementsPerGoroutine)
		assert.Equal(t, expected, usage)
	})
}

func TestKeyStore_ConcurrentIncrementVersion(t *testing.T) {
	t.Run("concurrent increment version", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Version = 1

		err := store.Put(metadata)
		require.NoError(t, err)

		numGoroutines := 100
		var wg sync.WaitGroup

		wg.Add(numGoroutines)
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				_, err := store.IncrementVersion("key-1")
				assert.NoError(t, err)
			}()
		}

		wg.Wait()

		// Verify final version
		version, err := store.GetVersion("key-1")
		require.NoError(t, err)
		expected := 1 + numGoroutines
		assert.Equal(t, expected, version)
	})
}

func TestKeyStore_ConcurrentMixedOperations(t *testing.T) {
	t.Run("concurrent mixed operations", func(t *testing.T) {
		store := NewKeyStore()

		// Pre-populate with some keys
		for i := 0; i < 10; i++ {
			metadata := createTestMetadata(string(rune('0' + i)))
			err := store.Put(metadata)
			require.NoError(t, err)
		}

		numGoroutines := 50
		var wg sync.WaitGroup

		wg.Add(numGoroutines * 4) // 4 operation types

		// Concurrent reads
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				results := store.List("", 0)
				assert.NotNil(t, results)
			}()
		}

		// Concurrent writes
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				metadata := createTestMetadata(string(rune('a' + id)))
				err := store.Put(metadata)
				assert.NoError(t, err)
			}(i)
		}

		// Concurrent increments
		for i := 0; i < numGoroutines; i++ {
			go func() {
				defer wg.Done()
				// Increment first key
				_ = store.IncrementUsage(string(rune('0')))
			}()
		}

		// Concurrent state changes
		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()
				// Toggle state on different keys
				keyID := string(rune('0' + (id % 10)))
				if id%2 == 0 {
					_ = store.SetState(keyID, KeyStateDisabled)
				} else {
					_ = store.SetState(keyID, KeyStateEnabled)
				}
			}(i)
		}

		wg.Wait()

		// Verify store is still consistent
		results := store.List("", 0)
		assert.NotEmpty(t, results)
	})
}

// =============================================================================
// Deep Copy Tests
// =============================================================================

func TestKeyStore_DeepCopySlices(t *testing.T) {
	t.Run("usage slice is deep copied", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Usage = []KeyUsage{KeyUsageEncrypt, KeyUsageDecrypt}

		err := store.Put(metadata)
		require.NoError(t, err)

		// Mutate original
		metadata.Usage[0] = KeyUsageSign

		// Verify stored copy is unchanged
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Equal(t, KeyUsageEncrypt, retrieved.Usage[0])
	})
}

func TestKeyStore_DeepCopyMaps(t *testing.T) {
	t.Run("tags map is deep copied", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		metadata.Tags = map[string]string{
			"env": "test",
		}

		err := store.Put(metadata)
		require.NoError(t, err)

		// Mutate original
		metadata.Tags["env"] = "prod"
		metadata.Tags["new"] = "value"

		// Verify stored copy is unchanged
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.Equal(t, "test", retrieved.Tags["env"])
		assert.NotContains(t, retrieved.Tags, "new")
	})
}

func TestKeyStore_DeepCopyPointers(t *testing.T) {
	t.Run("LastUsedAt pointer is deep copied", func(t *testing.T) {
		store := NewKeyStore()
		metadata := createTestMetadata("key-1")
		now := time.Now()
		metadata.LastUsedAt = &now

		err := store.Put(metadata)
		require.NoError(t, err)

		// Mutate original pointer
		newTime := now.Add(1 * time.Hour)
		metadata.LastUsedAt = &newTime

		// Verify stored copy is unchanged
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.True(t, retrieved.LastUsedAt.Equal(now))
	})

	t.Run("RotationPolicy is deep copied", func(t *testing.T) {
		store := NewKeyStore()
		rotationTime := time.Now().Add(24 * time.Hour)
		metadata := createTestMetadataWithRotation("key-1", rotationTime)

		err := store.Put(metadata)
		require.NoError(t, err)

		// Mutate original
		metadata.RotationPolicy.Enabled = false
		metadata.RotationPolicy.Interval = 48 * time.Hour

		// Verify stored copy is unchanged
		retrieved, err := store.Get("key-1")
		require.NoError(t, err)
		assert.True(t, retrieved.RotationPolicy.Enabled)
		assert.Equal(t, 24*time.Hour, retrieved.RotationPolicy.Interval)
	})
}
