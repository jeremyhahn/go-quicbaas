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
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestManager creates a test manager with a temporary PebbleDB.
func setupTestManager(t *testing.T) (CatalogManager, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "catalog-manager-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "testdb")
	storage, err := NewPebbleStorage(&PebbleStorageConfig{
		Path:       dbPath,
		SyncWrites: false, // Faster for tests
	})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create storage: %v", err)
	}

	manager, err := NewCatalogManager(&ManagerConfig{
		Storage: storage,
	})
	if err != nil {
		storage.Close()
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to create manager: %v", err)
	}

	cleanup := func() {
		manager.Close()
		os.RemoveAll(tmpDir)
	}

	return manager, cleanup
}

// createTestEntry creates a test catalog entry.
func createTestEntry(key string) *CatalogEntry {
	entry := NewCatalogEntry(key, 1024, []byte("test-checksum-12345"))
	entry.PrimaryStore = StoreLocation{
		Store:     "s3-primary",
		ObjectRef: "bucket/path/" + key,
		Status:    LocationStatusActive,
	}
	entry.TenantID = "tenant-1"
	return entry
}

func TestNewCatalogManager(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		manager, cleanup := setupTestManager(t)
		defer cleanup()

		if manager == nil {
			t.Fatal("expected manager to be non-nil")
		}
	})

	t.Run("with nil config", func(t *testing.T) {
		_, err := NewCatalogManager(nil)
		if err != ErrConfigNil {
			t.Errorf("expected ErrConfigNil, got %v", err)
		}
	})

	t.Run("with nil storage", func(t *testing.T) {
		_, err := NewCatalogManager(&ManagerConfig{
			Storage: nil,
		})
		if err != ErrStorageNil {
			t.Errorf("expected ErrStorageNil, got %v", err)
		}
	})
}

func TestManagerPut(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("valid entry", func(t *testing.T) {
		entry := createTestEntry("ns1:path/to/object.txt")

		err := manager.Put(ctx, entry)
		if err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		// Verify it was stored
		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.Key != entry.Key {
			t.Errorf("key mismatch: expected %s, got %s", entry.Key, retrieved.Key)
		}
	})

	t.Run("nil entry", func(t *testing.T) {
		err := manager.Put(ctx, nil)
		if err != ErrEntryNil {
			t.Errorf("expected ErrEntryNil, got %v", err)
		}
	})

	t.Run("invalid entry - empty key", func(t *testing.T) {
		entry := &CatalogEntry{}
		err := manager.Put(ctx, entry)
		if err == nil {
			t.Error("expected error for invalid entry")
		}
	})
}

func TestManagerGet(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("existing entry", func(t *testing.T) {
		entry := createTestEntry("ns1:get-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.Key != entry.Key {
			t.Errorf("key mismatch: expected %s, got %s", entry.Key, retrieved.Key)
		}
		if retrieved.Size != entry.Size {
			t.Errorf("size mismatch: expected %d, got %d", entry.Size, retrieved.Size)
		}
	})

	t.Run("non-existent entry", func(t *testing.T) {
		_, err := manager.Get(ctx, "ns1:non-existent.txt")
		if !IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("empty key", func(t *testing.T) {
		_, err := manager.Get(ctx, "")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestManagerDelete(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("existing entry", func(t *testing.T) {
		entry := createTestEntry("ns1:delete-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.Delete(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		// Verify it was deleted
		_, err = manager.Get(ctx, entry.Key)
		if !IsNotFound(err) {
			t.Errorf("expected not found error after delete, got %v", err)
		}
	})

	t.Run("non-existent entry", func(t *testing.T) {
		err := manager.Delete(ctx, "ns1:non-existent-delete.txt")
		// Should not error for non-existent
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("empty key", func(t *testing.T) {
		err := manager.Delete(ctx, "")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestManagerUpdate(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("successful update", func(t *testing.T) {
		entry := createTestEntry("ns1:update-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		originalVersion := entry.Version

		err := manager.Update(ctx, entry.Key, func(e *CatalogEntry) error {
			e.Size = 2048
			e.SetTag("updated", "true")
			return nil
		})
		if err != nil {
			t.Fatalf("Update failed: %v", err)
		}

		// Verify updates
		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.Size != 2048 {
			t.Errorf("size not updated: expected 2048, got %d", retrieved.Size)
		}
		if retrieved.GetTag("updated") != "true" {
			t.Error("tag not updated")
		}
		if retrieved.Version != originalVersion+1 {
			t.Errorf("version not incremented: expected %d, got %d", originalVersion+1, retrieved.Version)
		}
	})

	t.Run("update non-existent entry", func(t *testing.T) {
		err := manager.Update(ctx, "ns1:non-existent-update.txt", func(e *CatalogEntry) error {
			return nil
		})
		if !IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("nil updater", func(t *testing.T) {
		entry := createTestEntry("ns1:update-nil-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.Update(ctx, entry.Key, nil)
		if err != ErrUpdaterNil {
			t.Errorf("expected ErrUpdaterNil, got %v", err)
		}
	})

	t.Run("empty key", func(t *testing.T) {
		err := manager.Update(ctx, "", func(e *CatalogEntry) error {
			return nil
		})
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestManagerExists(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("existing entry", func(t *testing.T) {
		entry := createTestEntry("ns1:exists-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		exists, err := manager.Exists(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if !exists {
			t.Error("expected entry to exist")
		}
	})

	t.Run("non-existent entry", func(t *testing.T) {
		exists, err := manager.Exists(ctx, "ns1:non-existent-exists.txt")
		if err != nil {
			t.Fatalf("Exists failed: %v", err)
		}
		if exists {
			t.Error("expected entry to not exist")
		}
	})

	t.Run("empty key", func(t *testing.T) {
		_, err := manager.Exists(ctx, "")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestManagerAddLocation(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("add new location", func(t *testing.T) {
		entry := createTestEntry("ns1:add-location-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		newLoc := &StoreLocation{
			Store:     "s3-replica",
			ObjectRef: "replica-bucket/path/add-location-test.txt",
			Status:    LocationStatusActive,
		}

		err := manager.AddLocation(ctx, entry.Key, newLoc)
		if err != nil {
			t.Fatalf("AddLocation failed: %v", err)
		}

		// Verify location was added
		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if len(retrieved.Replicas) != 1 {
			t.Errorf("expected 1 replica, got %d", len(retrieved.Replicas))
		}
		if retrieved.Replicas[0].Store != "s3-replica" {
			t.Errorf("replica store mismatch: expected s3-replica, got %s", retrieved.Replicas[0].Store)
		}
	})

	t.Run("add duplicate location", func(t *testing.T) {
		entry := createTestEntry("ns1:add-dup-location-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		loc := &StoreLocation{
			Store:     "s3-dup",
			ObjectRef: "bucket/dup-test.txt",
			Status:    LocationStatusActive,
		}

		// Add first time
		if err := manager.AddLocation(ctx, entry.Key, loc); err != nil {
			t.Fatalf("First AddLocation failed: %v", err)
		}

		// Add second time (should be no-op)
		if err := manager.AddLocation(ctx, entry.Key, loc); err != nil {
			t.Fatalf("Second AddLocation failed: %v", err)
		}

		// Verify only one replica
		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if len(retrieved.Replicas) != 1 {
			t.Errorf("expected 1 replica, got %d", len(retrieved.Replicas))
		}
	})

	t.Run("nil location", func(t *testing.T) {
		entry := createTestEntry("ns1:add-nil-loc-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.AddLocation(ctx, entry.Key, nil)
		if err != ErrLocationNil {
			t.Errorf("expected ErrLocationNil, got %v", err)
		}
	})
}

func TestManagerRemoveLocation(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("remove replica", func(t *testing.T) {
		entry := createTestEntry("ns1:remove-location-test.txt")
		entry.Replicas = []StoreLocation{
			{Store: "s3-replica", ObjectRef: "bucket/replica.txt", Status: LocationStatusActive},
		}
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.RemoveLocation(ctx, entry.Key, "s3-replica")
		if err != nil {
			t.Fatalf("RemoveLocation failed: %v", err)
		}

		// Verify location was removed
		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if len(retrieved.Replicas) != 0 {
			t.Errorf("expected 0 replicas, got %d", len(retrieved.Replicas))
		}
	})

	t.Run("cannot remove primary", func(t *testing.T) {
		entry := createTestEntry("ns1:remove-primary-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.RemoveLocation(ctx, entry.Key, entry.PrimaryStore.Store)
		if err != ErrCannotRemovePrimary {
			t.Errorf("expected ErrCannotRemovePrimary, got %v", err)
		}
	})

	t.Run("empty store name", func(t *testing.T) {
		entry := createTestEntry("ns1:remove-empty-store-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.RemoveLocation(ctx, entry.Key, "")
		if err != ErrStoreNameEmpty {
			t.Errorf("expected ErrStoreNameEmpty, got %v", err)
		}
	})
}

func TestManagerSetPrimaryStore(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("promote replica to primary", func(t *testing.T) {
		entry := createTestEntry("ns1:set-primary-test.txt")
		entry.Replicas = []StoreLocation{
			{Store: "s3-replica", ObjectRef: "bucket/replica.txt", Status: LocationStatusActive},
		}
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		originalPrimary := entry.PrimaryStore.Store

		err := manager.SetPrimaryStore(ctx, entry.Key, "s3-replica")
		if err != nil {
			t.Fatalf("SetPrimaryStore failed: %v", err)
		}

		// Verify the switch
		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.PrimaryStore.Store != "s3-replica" {
			t.Errorf("primary not switched: expected s3-replica, got %s", retrieved.PrimaryStore.Store)
		}
		if len(retrieved.Replicas) != 1 || retrieved.Replicas[0].Store != originalPrimary {
			t.Error("old primary not demoted to replica")
		}
	})

	t.Run("set same primary", func(t *testing.T) {
		entry := createTestEntry("ns1:set-same-primary-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.SetPrimaryStore(ctx, entry.Key, entry.PrimaryStore.Store)
		if err != nil {
			t.Fatalf("SetPrimaryStore failed: %v", err)
		}
	})

	t.Run("store not found", func(t *testing.T) {
		entry := createTestEntry("ns1:set-primary-not-found-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.SetPrimaryStore(ctx, entry.Key, "non-existent-store")
		if err == nil {
			t.Error("expected error for non-existent store")
		}
	})
}

func TestManagerGetLocations(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("get all locations", func(t *testing.T) {
		entry := createTestEntry("ns1:get-locations-test.txt")
		entry.Replicas = []StoreLocation{
			{Store: "s3-replica1", ObjectRef: "bucket/replica1.txt", Status: LocationStatusActive},
			{Store: "s3-replica2", ObjectRef: "bucket/replica2.txt", Status: LocationStatusSyncing},
		}
		entry.ArchiveStore = &StoreLocation{
			Store: "glacier", ObjectRef: "archive/test.txt", Status: LocationStatusActive,
		}
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		locations, err := manager.GetLocations(ctx, entry.Key)
		if err != nil {
			t.Fatalf("GetLocations failed: %v", err)
		}

		// Should have primary + 2 replicas + archive = 4
		if len(locations) != 4 {
			t.Errorf("expected 4 locations, got %d", len(locations))
		}
	})

	t.Run("non-existent entry", func(t *testing.T) {
		_, err := manager.GetLocations(ctx, "ns1:non-existent-locations.txt")
		if !IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

func TestManagerUpdateLocationStatus(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("update primary status", func(t *testing.T) {
		entry := createTestEntry("ns1:update-status-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.UpdateLocationStatus(ctx, entry.Key, entry.PrimaryStore.Store, LocationStatusSyncing)
		if err != nil {
			t.Fatalf("UpdateLocationStatus failed: %v", err)
		}

		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.PrimaryStore.Status != LocationStatusSyncing {
			t.Errorf("status not updated: expected syncing, got %s", retrieved.PrimaryStore.Status)
		}
	})

	t.Run("update replica status", func(t *testing.T) {
		entry := createTestEntry("ns1:update-replica-status-test.txt")
		entry.Replicas = []StoreLocation{
			{Store: "s3-replica", ObjectRef: "bucket/replica.txt", Status: LocationStatusActive},
		}
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.UpdateLocationStatus(ctx, entry.Key, "s3-replica", LocationStatusFailed)
		if err != nil {
			t.Fatalf("UpdateLocationStatus failed: %v", err)
		}

		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.Replicas[0].Status != LocationStatusFailed {
			t.Errorf("status not updated: expected failed, got %s", retrieved.Replicas[0].Status)
		}
	})

	t.Run("invalid status", func(t *testing.T) {
		entry := createTestEntry("ns1:invalid-status-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.UpdateLocationStatus(ctx, entry.Key, entry.PrimaryStore.Store, LocationStatus("invalid"))
		if err != ErrInvalidLocationStatus {
			t.Errorf("expected ErrInvalidLocationStatus, got %v", err)
		}
	})

	t.Run("store not found", func(t *testing.T) {
		entry := createTestEntry("ns1:status-store-not-found-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.UpdateLocationStatus(ctx, entry.Key, "non-existent-store", LocationStatusActive)
		if err == nil {
			t.Error("expected error for non-existent store")
		}
	})
}

func TestManagerListByStore(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("list entries in store", func(t *testing.T) {
		// Create entries with same primary store
		for i := 0; i < 3; i++ {
			entry := createTestEntry("ns1:list-store-" + string(rune('a'+i)) + ".txt")
			entry.PrimaryStore.Store = "s3-list-test"
			if err := manager.Put(ctx, entry); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		result, err := manager.ListByStore(ctx, "s3-list-test", nil)
		if err != nil {
			t.Fatalf("ListByStore failed: %v", err)
		}

		if len(result.Keys) != 3 {
			t.Errorf("expected 3 keys, got %d", len(result.Keys))
		}
	})

	t.Run("empty store name", func(t *testing.T) {
		_, err := manager.ListByStore(ctx, "", nil)
		if err != ErrStoreNameEmpty {
			t.Errorf("expected ErrStoreNameEmpty, got %v", err)
		}
	})
}

func TestManagerListByTenant(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("list entries by tenant", func(t *testing.T) {
		// Create entries for same tenant
		for i := 0; i < 3; i++ {
			entry := createTestEntry("ns1:list-tenant-" + string(rune('a'+i)) + ".txt")
			entry.TenantID = "tenant-list-test"
			if err := manager.Put(ctx, entry); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		result, err := manager.ListByTenant(ctx, "tenant-list-test", nil)
		if err != nil {
			t.Fatalf("ListByTenant failed: %v", err)
		}

		if len(result.Keys) != 3 {
			t.Errorf("expected 3 keys, got %d", len(result.Keys))
		}
	})

	t.Run("empty tenant", func(t *testing.T) {
		_, err := manager.ListByTenant(ctx, "", nil)
		if err != ErrTenantEmpty {
			t.Errorf("expected ErrTenantEmpty, got %v", err)
		}
	})
}

func TestManagerListByStatus(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("list by status", func(t *testing.T) {
		// Create entries with different statuses
		for i := 0; i < 2; i++ {
			entry := createTestEntry("ns1:list-status-active-" + string(rune('a'+i)) + ".txt")
			entry.Status = StatusActive
			if err := manager.Put(ctx, entry); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		result, err := manager.ListByStatus(ctx, StatusActive, nil)
		if err != nil {
			t.Fatalf("ListByStatus failed: %v", err)
		}

		if len(result.Keys) < 2 {
			t.Errorf("expected at least 2 keys, got %d", len(result.Keys))
		}
	})

	t.Run("invalid status", func(t *testing.T) {
		_, err := manager.ListByStatus(ctx, EntryStatus("invalid"), nil)
		if err != ErrInvalidStatus {
			t.Errorf("expected ErrInvalidStatus, got %v", err)
		}
	})
}

func TestManagerListByTag(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("list by tag", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			entry := createTestEntry("ns1:list-tag-" + string(rune('a'+i)) + ".txt")
			entry.SetTag("env", "test")
			if err := manager.Put(ctx, entry); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		result, err := manager.ListByTag(ctx, "env", "test", nil)
		if err != nil {
			t.Fatalf("ListByTag failed: %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(result.Keys))
		}
	})

	t.Run("empty tag", func(t *testing.T) {
		_, err := manager.ListByTag(ctx, "", "value", nil)
		if err != ErrTagEmpty {
			t.Errorf("expected ErrTagEmpty, got %v", err)
		}
	})
}

func TestManagerListByEncryptionKey(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("list by encryption key", func(t *testing.T) {
		for i := 0; i < 2; i++ {
			entry := createTestEntry("ns1:list-enc-" + string(rune('a'+i)) + ".txt")
			entry.Encryption = &ObjectEncryption{
				Enabled:   true,
				KeyID:     "kms/test-key-123",
				Algorithm: "AES-256-GCM",
			}
			if err := manager.Put(ctx, entry); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		result, err := manager.ListByEncryptionKey(ctx, "kms/test-key-123", nil)
		if err != nil {
			t.Fatalf("ListByEncryptionKey failed: %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(result.Keys))
		}
	})

	t.Run("empty key ID", func(t *testing.T) {
		_, err := manager.ListByEncryptionKey(ctx, "", nil)
		if err != ErrKeyIDEmpty {
			t.Errorf("expected ErrKeyIDEmpty, got %v", err)
		}
	})
}

func TestManagerSearch(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("search with filters", func(t *testing.T) {
		now := time.Now().UnixNano()

		// Create test entries
		entry1 := createTestEntry("ns1:search-a.txt")
		entry1.Size = 1000
		entry1.TenantID = "tenant-search"
		entry1.SetTag("type", "document")
		if err := manager.Put(ctx, entry1); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		entry2 := createTestEntry("ns1:search-b.txt")
		entry2.Size = 5000
		entry2.TenantID = "tenant-search"
		entry2.SetTag("type", "image")
		if err := manager.Put(ctx, entry2); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		// Search by tenant
		result, err := manager.Search(ctx, &SearchQuery{
			TenantID: "tenant-search",
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) != 2 {
			t.Errorf("expected 2 entries, got %d", len(result.Entries))
		}

		// Search by size range
		result, err = manager.Search(ctx, &SearchQuery{
			SizeMin: 3000,
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) != 1 {
			t.Errorf("expected 1 entry, got %d", len(result.Entries))
		}

		// Search by creation time
		result, err = manager.Search(ctx, &SearchQuery{
			CreatedAfter: now - time.Hour.Nanoseconds(),
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) < 2 {
			t.Errorf("expected at least 2 entries, got %d", len(result.Entries))
		}
	})

	t.Run("search with sorting", func(t *testing.T) {
		// Create entries with different sizes
		for i := 0; i < 3; i++ {
			entry := createTestEntry("ns1:sort-test-" + string(rune('a'+i)) + ".txt")
			entry.TenantID = "tenant-sort"
			entry.Size = int64((3 - i) * 1000) // 3000, 2000, 1000
			if err := manager.Put(ctx, entry); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		result, err := manager.Search(ctx, &SearchQuery{
			TenantID: "tenant-sort",
			Sort:     "size",
			Order:    "asc",
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(result.Entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(result.Entries))
		}

		// Verify ascending order
		for i := 1; i < len(result.Entries); i++ {
			if result.Entries[i].Size < result.Entries[i-1].Size {
				t.Error("entries not sorted in ascending order by size")
			}
		}
	})

	t.Run("search with nil query", func(t *testing.T) {
		result, err := manager.Search(ctx, nil)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if result == nil {
			t.Error("expected non-nil result")
		}
	})
}

func TestManagerEnqueueReplication(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("enqueue and dequeue", func(t *testing.T) {
		err := manager.EnqueueReplication(ctx, "ns1:repl-test.txt", []string{"s3-replica"}, 5)
		if err != nil {
			t.Fatalf("EnqueueReplication failed: %v", err)
		}

		tasks, err := manager.DequeueReplication(ctx, 10)
		if err != nil {
			t.Fatalf("DequeueReplication failed: %v", err)
		}

		if len(tasks) != 1 {
			t.Errorf("expected 1 task, got %d", len(tasks))
		}
		if tasks[0].Key != "ns1:repl-test.txt" {
			t.Errorf("key mismatch: expected ns1:repl-test.txt, got %s", tasks[0].Key)
		}
	})

	t.Run("priority ordering", func(t *testing.T) {
		// Enqueue with different priorities
		if err := manager.EnqueueReplication(ctx, "ns1:low-priority.txt", []string{"s3"}, 1); err != nil {
			t.Fatalf("EnqueueReplication failed: %v", err)
		}
		if err := manager.EnqueueReplication(ctx, "ns1:high-priority.txt", []string{"s3"}, 10); err != nil {
			t.Fatalf("EnqueueReplication failed: %v", err)
		}

		tasks, err := manager.DequeueReplication(ctx, 10)
		if err != nil {
			t.Fatalf("DequeueReplication failed: %v", err)
		}

		if len(tasks) < 2 {
			t.Fatalf("expected at least 2 tasks, got %d", len(tasks))
		}

		// High priority should be first
		if tasks[0].Key != "ns1:high-priority.txt" {
			t.Errorf("expected high priority first, got %s", tasks[0].Key)
		}
	})

	t.Run("empty targets", func(t *testing.T) {
		err := manager.EnqueueReplication(ctx, "ns1:empty-targets.txt", []string{}, 5)
		if err != ErrTargetsEmpty {
			t.Errorf("expected ErrTargetsEmpty, got %v", err)
		}
	})

	t.Run("empty key", func(t *testing.T) {
		err := manager.EnqueueReplication(ctx, "", []string{"s3"}, 5)
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestManagerEnqueueArchive(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("enqueue and dequeue", func(t *testing.T) {
		err := manager.EnqueueArchive(ctx, "ns1:archive-test.txt", "glacier")
		if err != nil {
			t.Fatalf("EnqueueArchive failed: %v", err)
		}

		tasks, err := manager.DequeueArchive(ctx, 10)
		if err != nil {
			t.Fatalf("DequeueArchive failed: %v", err)
		}

		if len(tasks) != 1 {
			t.Errorf("expected 1 task, got %d", len(tasks))
		}
		if tasks[0].Key != "ns1:archive-test.txt" {
			t.Errorf("key mismatch: expected ns1:archive-test.txt, got %s", tasks[0].Key)
		}
		if tasks[0].TargetStore != "glacier" {
			t.Errorf("target store mismatch: expected glacier, got %s", tasks[0].TargetStore)
		}
	})

	t.Run("empty target store", func(t *testing.T) {
		err := manager.EnqueueArchive(ctx, "ns1:no-target.txt", "")
		if err != ErrStoreNameEmpty {
			t.Errorf("expected ErrStoreNameEmpty, got %v", err)
		}
	})

	t.Run("empty key", func(t *testing.T) {
		err := manager.EnqueueArchive(ctx, "", "glacier")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestManagerEnqueueVerify(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("enqueue verify", func(t *testing.T) {
		err := manager.EnqueueVerify(ctx, "ns1:verify-test.txt", []string{"s3-primary", "s3-replica"})
		if err != nil {
			t.Fatalf("EnqueueVerify failed: %v", err)
		}
	})

	t.Run("empty key", func(t *testing.T) {
		err := manager.EnqueueVerify(ctx, "", []string{"s3"})
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestManagerGetTenantUsage(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("get usage", func(t *testing.T) {
		// Create entries for tenant
		entry1 := createTestEntry("ns1:usage-a.txt")
		entry1.TenantID = "tenant-usage-test"
		entry1.Size = 1000
		entry1.Status = StatusActive
		if err := manager.Put(ctx, entry1); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		entry2 := createTestEntry("ns1:usage-b.txt")
		entry2.TenantID = "tenant-usage-test"
		entry2.Size = 2000
		entry2.Status = StatusArchived
		if err := manager.Put(ctx, entry2); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		usage, err := manager.GetTenantUsage(ctx, "tenant-usage-test")
		if err != nil {
			t.Fatalf("GetTenantUsage failed: %v", err)
		}

		if usage.ObjectCount != 2 {
			t.Errorf("expected 2 objects, got %d", usage.ObjectCount)
		}
		if usage.TotalSize != 3000 {
			t.Errorf("expected total size 3000, got %d", usage.TotalSize)
		}
		if usage.ActiveCount != 1 {
			t.Errorf("expected 1 active, got %d", usage.ActiveCount)
		}
		if usage.ArchivedCount != 1 {
			t.Errorf("expected 1 archived, got %d", usage.ArchivedCount)
		}
	})

	t.Run("empty tenant", func(t *testing.T) {
		_, err := manager.GetTenantUsage(ctx, "")
		if err != ErrTenantEmpty {
			t.Errorf("expected ErrTenantEmpty, got %v", err)
		}
	})
}

func TestManagerGetStoreUsage(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("get usage", func(t *testing.T) {
		entry := createTestEntry("ns1:store-usage.txt")
		entry.PrimaryStore = StoreLocation{
			Store:      "s3-store-usage-test",
			ObjectRef:  "bucket/test.txt",
			Status:     LocationStatusActive,
			StoredSize: 1500,
		}
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		usage, err := manager.GetStoreUsage(ctx, "s3-store-usage-test")
		if err != nil {
			t.Fatalf("GetStoreUsage failed: %v", err)
		}

		if usage.ObjectCount != 1 {
			t.Errorf("expected 1 object, got %d", usage.ObjectCount)
		}
		if usage.TotalSize != 1500 {
			t.Errorf("expected total size 1500, got %d", usage.TotalSize)
		}
		if usage.HealthyCount != 1 {
			t.Errorf("expected 1 healthy, got %d", usage.HealthyCount)
		}
	})

	t.Run("empty store", func(t *testing.T) {
		_, err := manager.GetStoreUsage(ctx, "")
		if err != ErrStoreNameEmpty {
			t.Errorf("expected ErrStoreNameEmpty, got %v", err)
		}
	})
}

func TestManagerUpdateAccessTime(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("update access time", func(t *testing.T) {
		entry := createTestEntry("ns1:access-time-test.txt")
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		originalAccessTime := entry.LastAccessAt

		// Wait a bit to ensure time difference
		time.Sleep(10 * time.Millisecond)

		err := manager.UpdateAccessTime(ctx, entry.Key)
		if err != nil {
			t.Fatalf("UpdateAccessTime failed: %v", err)
		}

		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}

		if retrieved.LastAccessAt <= originalAccessTime {
			t.Error("access time not updated")
		}
	})

	t.Run("empty key", func(t *testing.T) {
		err := manager.UpdateAccessTime(ctx, "")
		if err == nil {
			t.Error("expected error for empty key")
		}
	})
}

func TestManagerClose(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	// Close the manager
	if err := manager.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Operations should fail after close
	_, err := manager.Get(ctx, "ns1:test.txt")
	if !IsCatalogClosed(err) {
		t.Errorf("expected catalog closed error, got %v", err)
	}

	// Second close should be no-op
	if err := manager.Close(); err != nil {
		t.Errorf("second Close should not error: %v", err)
	}
}

func TestManagerSync(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()

	err := manager.Sync()
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
}

func TestManagerListByPrefix(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("list by prefix", func(t *testing.T) {
		// Create entries with same prefix
		for i := 0; i < 3; i++ {
			entry := createTestEntry("ns-prefix:dir/file-" + string(rune('a'+i)) + ".txt")
			if err := manager.Put(ctx, entry); err != nil {
				t.Fatalf("Put failed: %v", err)
			}
		}

		result, err := manager.ListByPrefix(ctx, "ns-prefix:dir/", nil)
		if err != nil {
			t.Fatalf("ListByPrefix failed: %v", err)
		}

		if len(result.Keys) != 3 {
			t.Errorf("expected 3 keys, got %d", len(result.Keys))
		}
	})

	t.Run("list with limit", func(t *testing.T) {
		result, err := manager.ListByPrefix(ctx, "ns-prefix:", &ListOptions{Limit: 2})
		if err != nil {
			t.Fatalf("ListByPrefix failed: %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(result.Keys))
		}
		if !result.HasMore {
			t.Error("expected HasMore to be true")
		}
	})
}

// =============================================================================
// Consolidated from manager_coverage_test.go
// =============================================================================

// TestManagerSearchAllFilters tests search with all filter types.
func TestManagerSearchAllFilters(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	// Create entries with various attributes
	entry1 := createTestEntry("ns1:search-all-a.txt")
	entry1.Size = 1000
	entry1.TenantID = "tenant-all"
	entry1.Status = StatusActive
	entry1.SetTag("type", "document")
	entry1.SetTag("env", "prod")
	if err := manager.Put(ctx, entry1); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	entry2 := createTestEntry("ns1:search-all-b.txt")
	entry2.Size = 5000
	entry2.TenantID = "tenant-all"
	entry2.Status = StatusActive
	entry2.SetTag("type", "image")
	entry2.SetTag("env", "prod")
	if err := manager.Put(ctx, entry2); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	entry3 := createTestEntry("ns1:search-all-c.txt")
	entry3.Size = 2000
	entry3.TenantID = "tenant-other"
	entry3.Status = StatusArchived
	if err := manager.Put(ctx, entry3); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	t.Run("filter by status", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			Status: StatusActive,
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) < 2 {
			t.Errorf("expected at least 2 active entries, got %d", len(result.Entries))
		}
	})

	t.Run("filter by size max", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			SizeMax: 1500,
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		for _, e := range result.Entries {
			if e.Size > 1500 {
				t.Errorf("entry size %d exceeds max 1500", e.Size)
			}
		}
	})

	t.Run("filter by created before", func(t *testing.T) {
		future := time.Now().Add(time.Hour).UnixNano()
		result, err := manager.Search(ctx, &SearchQuery{
			CreatedBefore: future,
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) < 3 {
			t.Errorf("expected at least 3 entries created before future, got %d", len(result.Entries))
		}
	})

	t.Run("filter by modified after", func(t *testing.T) {
		past := time.Now().Add(-time.Hour).UnixNano()
		result, err := manager.Search(ctx, &SearchQuery{
			ModifiedAfter: past,
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) < 3 {
			t.Errorf("expected entries modified after past, got %d", len(result.Entries))
		}
	})

	t.Run("filter by modified before", func(t *testing.T) {
		future := time.Now().Add(time.Hour).UnixNano()
		result, err := manager.Search(ctx, &SearchQuery{
			ModifiedBefore: future,
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) < 3 {
			t.Errorf("expected entries modified before future, got %d", len(result.Entries))
		}
	})

	t.Run("filter by tags", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			Tags: map[string]string{
				"env":  "prod",
				"type": "document",
			},
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) != 1 {
			t.Errorf("expected 1 entry with both tags, got %d", len(result.Entries))
		}
	})

	t.Run("filter by tags no match", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			Tags: map[string]string{
				"nonexistent": "tag",
			},
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) != 0 {
			t.Errorf("expected 0 entries, got %d", len(result.Entries))
		}
	})

	t.Run("filter by prefix", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			Prefix: "ns1:search-all",
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) < 3 {
			t.Errorf("expected at least 3 entries with prefix, got %d", len(result.Entries))
		}
	})
}

// TestManagerSearchSorting tests all sort fields.
func TestManagerSearchSorting(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	now := time.Now()

	// Create entries with different attributes
	for i := 0; i < 3; i++ {
		entry := createTestEntry("ns-sort:file-" + string(rune('a'+i)) + ".txt")
		entry.TenantID = "tenant-sort-test"
		entry.Size = int64((i + 1) * 1000) // 1000, 2000, 3000
		entry.CreatedAt = now.Add(-time.Duration(3-i) * time.Hour).UnixNano()
		entry.ModifiedAt = now.Add(-time.Duration(i+1) * time.Hour).UnixNano()
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	t.Run("sort by created_at asc", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			TenantID: "tenant-sort-test",
			Sort:     "created_at",
			Order:    "asc",
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		for i := 1; i < len(result.Entries); i++ {
			if result.Entries[i].CreatedAt < result.Entries[i-1].CreatedAt {
				t.Error("entries not sorted by created_at ascending")
			}
		}
	})

	t.Run("sort by modified_at desc", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			TenantID: "tenant-sort-test",
			Sort:     "modified_at",
			Order:    "desc",
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		for i := 1; i < len(result.Entries); i++ {
			if result.Entries[i].ModifiedAt > result.Entries[i-1].ModifiedAt {
				t.Error("entries not sorted by modified_at descending")
			}
		}
	})

	t.Run("sort by key", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			TenantID: "tenant-sort-test",
			Sort:     "key",
			Order:    "asc",
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		for i := 1; i < len(result.Entries); i++ {
			if result.Entries[i].Key < result.Entries[i-1].Key {
				t.Error("entries not sorted by key ascending")
			}
		}
	})

	t.Run("sort by unknown field uses key", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			TenantID: "tenant-sort-test",
			Sort:     "unknown_field",
			Order:    "asc",
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		// Should default to sorting by key
		for i := 1; i < len(result.Entries); i++ {
			if result.Entries[i].Key < result.Entries[i-1].Key {
				t.Error("entries not sorted by key (default)")
			}
		}
	})
}

// TestManagerSearchPagination tests search cursor pagination.
func TestManagerSearchPagination(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	// Create entries for pagination test
	for i := 0; i < 5; i++ {
		entry := createTestEntry("ns-page:file-" + string(rune('a'+i)) + ".txt")
		entry.TenantID = "tenant-page"
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}
	}

	t.Run("paginate with cursor", func(t *testing.T) {
		// Get first page
		result1, err := manager.Search(ctx, &SearchQuery{
			TenantID: "tenant-page",
			Limit:    2,
		})
		if err != nil {
			t.Fatalf("Search page 1 failed: %v", err)
		}
		if len(result1.Entries) != 2 {
			t.Errorf("expected 2 entries on page 1, got %d", len(result1.Entries))
		}
		if !result1.HasMore {
			t.Error("expected HasMore to be true for page 1")
		}

		// Get second page using cursor
		if result1.NextCursor != "" {
			result2, err := manager.Search(ctx, &SearchQuery{
				TenantID: "tenant-page",
				Limit:    2,
				Cursor:   result1.NextCursor,
			})
			if err != nil {
				t.Fatalf("Search page 2 failed: %v", err)
			}
			if len(result2.Entries) == 0 {
				t.Error("expected entries on page 2")
			}
		}
	})

	t.Run("search with no results returns empty", func(t *testing.T) {
		result, err := manager.Search(ctx, &SearchQuery{
			TenantID: "non-existent-tenant",
		})
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}
		if len(result.Entries) != 0 {
			t.Errorf("expected 0 entries, got %d", len(result.Entries))
		}
		if result.HasMore {
			t.Error("expected HasMore to be false")
		}
	})
}

// TestManagerUpdateLocationStatusArchive tests updating archive store status.
func TestManagerUpdateLocationStatusArchive(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("update archive store status", func(t *testing.T) {
		entry := createTestEntry("ns1:archive-status-test.txt")
		entry.ArchiveStore = &StoreLocation{
			Store:     "glacier",
			ObjectRef: "archive/test.txt",
			Status:    LocationStatusSyncing,
		}
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.UpdateLocationStatus(ctx, entry.Key, "glacier", LocationStatusActive)
		if err != nil {
			t.Fatalf("UpdateLocationStatus failed: %v", err)
		}

		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.ArchiveStore.Status != LocationStatusActive {
			t.Errorf("archive status not updated: expected active, got %s", retrieved.ArchiveStore.Status)
		}
		if retrieved.ArchiveStore.SyncedAt == 0 {
			t.Error("SyncedAt should be updated when status becomes active")
		}
	})

	t.Run("update replica to active updates synced time", func(t *testing.T) {
		entry := createTestEntry("ns1:replica-sync-time-test.txt")
		entry.Replicas = []StoreLocation{
			{Store: "s3-replica", ObjectRef: "bucket/replica.txt", Status: LocationStatusSyncing},
		}
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.UpdateLocationStatus(ctx, entry.Key, "s3-replica", LocationStatusActive)
		if err != nil {
			t.Fatalf("UpdateLocationStatus failed: %v", err)
		}

		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.Replicas[0].SyncedAt == 0 {
			t.Error("SyncedAt should be updated when replica status becomes active")
		}
	})
}

// TestManagerRemoveArchiveLocation tests removing an archive store location.
func TestManagerRemoveArchiveLocation(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("remove archive location", func(t *testing.T) {
		entry := createTestEntry("ns1:remove-archive-test.txt")
		entry.ArchiveStore = &StoreLocation{
			Store:     "glacier",
			ObjectRef: "archive/test.txt",
			Status:    LocationStatusActive,
		}
		if err := manager.Put(ctx, entry); err != nil {
			t.Fatalf("Put failed: %v", err)
		}

		err := manager.RemoveLocation(ctx, entry.Key, "glacier")
		if err != nil {
			t.Fatalf("RemoveLocation failed: %v", err)
		}

		retrieved, err := manager.Get(ctx, entry.Key)
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if retrieved.ArchiveStore != nil {
			t.Error("expected archive store to be removed")
		}
	})
}

// TestManagerUpdateReturnsError tests update when updater returns error.
func TestManagerUpdateReturnsError(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	entry := createTestEntry("ns1:update-error-test.txt")
	if err := manager.Put(ctx, entry); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	expectedErr := errors.New("update failed")
	err := manager.Update(ctx, entry.Key, func(e *CatalogEntry) error {
		return expectedErr
	})
	if err != expectedErr {
		t.Errorf("expected error from updater, got %v", err)
	}
}

// TestManagerAddLocationInvalidLocation tests adding invalid location.
func TestManagerAddLocationInvalidLocation(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	entry := createTestEntry("ns1:add-invalid-loc-test.txt")
	if err := manager.Put(ctx, entry); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	t.Run("empty store name", func(t *testing.T) {
		loc := &StoreLocation{
			Store:     "",
			ObjectRef: "bucket/test.txt",
			Status:    LocationStatusActive,
		}
		err := manager.AddLocation(ctx, entry.Key, loc)
		if err == nil {
			t.Error("expected error for invalid location")
		}
	})

	t.Run("empty object ref", func(t *testing.T) {
		loc := &StoreLocation{
			Store:     "s3",
			ObjectRef: "",
			Status:    LocationStatusActive,
		}
		err := manager.AddLocation(ctx, entry.Key, loc)
		if err == nil {
			t.Error("expected error for invalid location")
		}
	})

	t.Run("add location to non-existent entry", func(t *testing.T) {
		loc := &StoreLocation{
			Store:     "s3-new",
			ObjectRef: "bucket/new.txt",
			Status:    LocationStatusActive,
		}
		err := manager.AddLocation(ctx, "ns1:non-existent-for-location.txt", loc)
		if !IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})
}

// TestManagerGetStoreUsageFailed tests store usage with failed locations.
func TestManagerGetStoreUsageFailed(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	entry := createTestEntry("ns1:store-failed-usage.txt")
	entry.PrimaryStore = StoreLocation{
		Store:      "s3-failed-test",
		ObjectRef:  "bucket/failed.txt",
		Status:     LocationStatusFailed,
		StoredSize: 2000,
	}
	if err := manager.Put(ctx, entry); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	usage, err := manager.GetStoreUsage(ctx, "s3-failed-test")
	if err != nil {
		t.Fatalf("GetStoreUsage failed: %v", err)
	}

	if usage.FailedCount != 1 {
		t.Errorf("expected 1 failed, got %d", usage.FailedCount)
	}
	if usage.HealthyCount != 0 {
		t.Errorf("expected 0 healthy, got %d", usage.HealthyCount)
	}
}

// TestManagerClosedOperations tests all operations fail after close.
func TestManagerClosedOperations(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	ctx := context.Background()

	if err := manager.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	cleanup()

	entry := createTestEntry("ns1:closed-test.txt")
	loc := &StoreLocation{Store: "s3", ObjectRef: "test.txt", Status: LocationStatusActive}

	tests := []struct {
		name string
		fn   func() error
	}{
		{"Put", func() error { return manager.Put(ctx, entry) }},
		{"Delete", func() error { return manager.Delete(ctx, "ns1:test.txt") }},
		{"Update", func() error { return manager.Update(ctx, "ns1:test.txt", func(e *CatalogEntry) error { return nil }) }},
		{"AddLocation", func() error { return manager.AddLocation(ctx, "ns1:test.txt", loc) }},
		{"RemoveLocation", func() error { return manager.RemoveLocation(ctx, "ns1:test.txt", "s3") }},
		{"SetPrimaryStore", func() error { return manager.SetPrimaryStore(ctx, "ns1:test.txt", "s3") }},
		{"UpdateLocationStatus", func() error {
			return manager.UpdateLocationStatus(ctx, "ns1:test.txt", "s3", LocationStatusActive)
		}},
		{"EnqueueReplication", func() error {
			return manager.EnqueueReplication(ctx, "ns1:test.txt", []string{"s3"}, 5)
		}},
		{"EnqueueArchive", func() error { return manager.EnqueueArchive(ctx, "ns1:test.txt", "glacier") }},
		{"EnqueueVerify", func() error { return manager.EnqueueVerify(ctx, "ns1:test.txt", []string{"s3"}) }},
		{"UpdateAccessTime", func() error { return manager.UpdateAccessTime(ctx, "ns1:test.txt") }},
		{"Sync", func() error { return manager.Sync() }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if !IsCatalogClosed(err) {
				t.Errorf("expected catalog closed error, got %v", err)
			}
		})
	}

	t.Run("Get", func(t *testing.T) {
		_, err := manager.Get(ctx, "ns1:test.txt")
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		_, err := manager.Exists(ctx, "ns1:test.txt")
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("GetLocations", func(t *testing.T) {
		_, err := manager.GetLocations(ctx, "ns1:test.txt")
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("ListByStore", func(t *testing.T) {
		_, err := manager.ListByStore(ctx, "s3", nil)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("ListByTenant", func(t *testing.T) {
		_, err := manager.ListByTenant(ctx, "tenant", nil)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("ListByPrefix", func(t *testing.T) {
		_, err := manager.ListByPrefix(ctx, "ns:", nil)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("ListByStatus", func(t *testing.T) {
		_, err := manager.ListByStatus(ctx, StatusActive, nil)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("ListByTag", func(t *testing.T) {
		_, err := manager.ListByTag(ctx, "tag", "value", nil)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("ListByEncryptionKey", func(t *testing.T) {
		_, err := manager.ListByEncryptionKey(ctx, "key-id", nil)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("Search", func(t *testing.T) {
		_, err := manager.Search(ctx, nil)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("DequeueReplication", func(t *testing.T) {
		_, err := manager.DequeueReplication(ctx, 10)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("DequeueArchive", func(t *testing.T) {
		_, err := manager.DequeueArchive(ctx, 10)
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("GetTenantUsage", func(t *testing.T) {
		_, err := manager.GetTenantUsage(ctx, "tenant")
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})

	t.Run("GetStoreUsage", func(t *testing.T) {
		_, err := manager.GetStoreUsage(ctx, "s3")
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})
}

// TestManagerDequeueEmptyQueue tests dequeueing from empty queues.
func TestManagerDequeueEmptyQueue(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	t.Run("dequeue replication from empty", func(t *testing.T) {
		tasks, err := manager.DequeueReplication(ctx, 10)
		if err != nil {
			t.Fatalf("DequeueReplication failed: %v", err)
		}
		if len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d", len(tasks))
		}
	})

	t.Run("dequeue archive from empty", func(t *testing.T) {
		tasks, err := manager.DequeueArchive(ctx, 10)
		if err != nil {
			t.Fatalf("DequeueArchive failed: %v", err)
		}
		if len(tasks) != 0 {
			t.Errorf("expected 0 tasks, got %d", len(tasks))
		}
	})

	t.Run("dequeue with zero limit", func(t *testing.T) {
		// First add some tasks
		if err := manager.EnqueueReplication(ctx, "ns:test.txt", []string{"s3"}, 5); err != nil {
			t.Fatalf("EnqueueReplication failed: %v", err)
		}

		tasks, err := manager.DequeueReplication(ctx, 0)
		if err != nil {
			t.Fatalf("DequeueReplication failed: %v", err)
		}
		// Limit 0 should dequeue all
		if len(tasks) != 1 {
			t.Errorf("expected 1 task with limit 0, got %d", len(tasks))
		}
	})
}

// TestManagerUpdatePrimarySyncTime tests that primary status update sets sync time.
func TestManagerUpdatePrimarySyncTime(t *testing.T) {
	manager, cleanup := setupTestManager(t)
	defer cleanup()
	ctx := context.Background()

	entry := createTestEntry("ns1:primary-sync-time-test.txt")
	entry.PrimaryStore.SyncedAt = 0 // Reset sync time
	if err := manager.Put(ctx, entry); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Update to syncing first
	if err := manager.UpdateLocationStatus(ctx, entry.Key, entry.PrimaryStore.Store, LocationStatusSyncing); err != nil {
		t.Fatalf("UpdateLocationStatus to syncing failed: %v", err)
	}

	// Update to active
	if err := manager.UpdateLocationStatus(ctx, entry.Key, entry.PrimaryStore.Store, LocationStatusActive); err != nil {
		t.Fatalf("UpdateLocationStatus to active failed: %v", err)
	}

	retrieved, err := manager.Get(ctx, entry.Key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if retrieved.PrimaryStore.SyncedAt == 0 {
		t.Error("expected SyncedAt to be updated when status becomes active")
	}
}
