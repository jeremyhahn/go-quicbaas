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
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble"
)

// testPebbleDB creates a temporary PebbleDB for testing.
func testPebbleDB(t *testing.T) (*pebble.DB, func()) {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "catalog-index-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open pebble: %v", err)
	}

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return db, cleanup
}

// testEntry creates a test catalog entry with common fields.
func testEntry(key, store, tenant string, status EntryStatus) *CatalogEntry {
	checksum := sha256.Sum256([]byte(key))
	entry := NewCatalogEntry(key, 1024, checksum[:])
	entry.PrimaryStore = StoreLocation{Store: store, ObjectRef: "/data/" + key}
	entry.TenantID = tenant
	entry.Status = status
	return entry
}

func TestNewIndexManager(t *testing.T) {
	t.Run("creates manager with valid db", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, err := NewIndexManager(db)
		if err != nil {
			t.Fatalf("NewIndexManager() error = %v", err)
		}
		if mgr == nil {
			t.Fatal("NewIndexManager() returned nil manager")
		}
	})

	t.Run("returns error with nil db", func(t *testing.T) {
		mgr, err := NewIndexManager(nil)
		if err == nil {
			t.Fatal("NewIndexManager(nil) expected error")
		}
		if mgr != nil {
			t.Fatal("NewIndexManager(nil) should return nil manager")
		}
		var ce *CatalogError
		if !errors.As(err, &ce) {
			t.Fatalf("expected CatalogError, got %T", err)
		}
		if ce.Code != ErrCodeInvalidEntry {
			t.Errorf("expected error code %s, got %s", ErrCodeInvalidEntry, ce.Code)
		}
	})
}

func TestIndexManager_WriteIndexes(t *testing.T) {
	t.Run("writes all indexes for complete entry", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		entry := testEntry("tenant-a:docs/file.txt", "s3-primary", "tenant-a", StatusActive)
		entry.Tags = map[string]string{"env": "production", "type": "document"}
		entry.Encryption = &ObjectEncryption{Enabled: true, KeyID: "key-123"}

		batch := db.NewBatch()
		defer batch.Close()

		err := mgr.WriteIndexes(batch, entry)
		if err != nil {
			t.Fatalf("WriteIndexes() error = %v", err)
		}

		if err := batch.Commit(pebble.NoSync); err != nil {
			t.Fatalf("batch.Commit() error = %v", err)
		}

		// Verify store index exists
		objKey, _ := ParseKey(entry.Key)
		storeIdxKey := objKey.StoreIndexKey("s3-primary")
		_, closer, err := db.Get(storeIdxKey)
		if err != nil {
			t.Errorf("store index not found: %v", err)
		} else {
			closer.Close()
		}

		// Verify tenant index exists
		tenantIdxKey := objKey.TenantIndexKey("tenant-a")
		_, closer, err = db.Get(tenantIdxKey)
		if err != nil {
			t.Errorf("tenant index not found: %v", err)
		} else {
			closer.Close()
		}

		// Verify status index exists
		statusIdxKey := objKey.StatusIndexKey("active")
		_, closer, err = db.Get(statusIdxKey)
		if err != nil {
			t.Errorf("status index not found: %v", err)
		} else {
			closer.Close()
		}

		// Verify checksum index exists
		checksumIdxKey := objKey.ChecksumIndexKey(entry.ChecksumHex())
		_, closer, err = db.Get(checksumIdxKey)
		if err != nil {
			t.Errorf("checksum index not found: %v", err)
		} else {
			closer.Close()
		}

		// Verify tag indexes exist
		tagIdxKey := objKey.TagIndexKey("env", "production")
		_, closer, err = db.Get(tagIdxKey)
		if err != nil {
			t.Errorf("tag index not found: %v", err)
		} else {
			closer.Close()
		}

		// Verify encryption index exists
		encIdxKey := objKey.EncryptionIndexKey("key-123")
		_, closer, err = db.Get(encIdxKey)
		if err != nil {
			t.Errorf("encryption index not found: %v", err)
		} else {
			closer.Close()
		}
	})

	t.Run("returns error with nil batch", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		entry := testEntry("ns:path", "s3", "tenant", StatusActive)

		err := mgr.WriteIndexes(nil, entry)
		if err == nil {
			t.Fatal("WriteIndexes(nil, entry) expected error")
		}
	})

	t.Run("returns error with nil entry", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		batch := db.NewBatch()
		defer batch.Close()

		err := mgr.WriteIndexes(batch, nil)
		if err == nil {
			t.Fatal("WriteIndexes(batch, nil) expected error")
		}
	})

	t.Run("returns error with invalid key", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		entry := &CatalogEntry{Key: "invalid-key-no-colon"}

		batch := db.NewBatch()
		defer batch.Close()

		err := mgr.WriteIndexes(batch, entry)
		if err == nil {
			t.Fatal("WriteIndexes() expected error for invalid key")
		}
	})

	t.Run("skips empty optional fields", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		entry := &CatalogEntry{
			Key:    "ns:path",
			Status: StatusPending,
		}

		batch := db.NewBatch()
		defer batch.Close()

		err := mgr.WriteIndexes(batch, entry)
		if err != nil {
			t.Fatalf("WriteIndexes() error = %v", err)
		}
	})
}

func TestIndexManager_DeleteIndexes(t *testing.T) {
	t.Run("deletes all indexes", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		entry := testEntry("tenant-b:files/data.json", "minio", "tenant-b", StatusActive)
		entry.Tags = map[string]string{"backup": "true"}

		// First write indexes
		batch := db.NewBatch()
		if err := mgr.WriteIndexes(batch, entry); err != nil {
			t.Fatalf("WriteIndexes() error = %v", err)
		}
		if err := batch.Commit(pebble.NoSync); err != nil {
			t.Fatalf("batch.Commit() error = %v", err)
		}
		batch.Close()

		// Verify indexes exist
		objKey, _ := ParseKey(entry.Key)
		storeIdxKey := objKey.StoreIndexKey("minio")
		_, closer, err := db.Get(storeIdxKey)
		if err != nil {
			t.Fatalf("store index should exist before delete: %v", err)
		}
		closer.Close()

		// Now delete indexes
		batch = db.NewBatch()
		if err := mgr.DeleteIndexes(batch, entry); err != nil {
			t.Fatalf("DeleteIndexes() error = %v", err)
		}
		if err := batch.Commit(pebble.NoSync); err != nil {
			t.Fatalf("batch.Commit() error = %v", err)
		}
		batch.Close()

		// Verify indexes are deleted
		_, _, err = db.Get(storeIdxKey)
		if err != pebble.ErrNotFound {
			t.Errorf("store index should be deleted, got error: %v", err)
		}
	})

	t.Run("returns error with nil batch", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		entry := testEntry("ns:path", "s3", "tenant", StatusActive)

		err := mgr.DeleteIndexes(nil, entry)
		if err == nil {
			t.Fatal("DeleteIndexes(nil, entry) expected error")
		}
	})

	t.Run("returns error with nil entry", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		batch := db.NewBatch()
		defer batch.Close()

		err := mgr.DeleteIndexes(batch, nil)
		if err == nil {
			t.Fatal("DeleteIndexes(batch, nil) expected error")
		}
	})
}

func TestIndexManager_UpdateIndexes(t *testing.T) {
	t.Run("updates indexes when entry changes", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)

		oldEntry := testEntry("ns:file.txt", "s3-old", "tenant-1", StatusActive)
		newEntry := testEntry("ns:file.txt", "s3-new", "tenant-1", StatusArchived)

		// Write initial indexes
		batch := db.NewBatch()
		if err := mgr.WriteIndexes(batch, oldEntry); err != nil {
			t.Fatalf("WriteIndexes() error = %v", err)
		}
		if err := batch.Commit(pebble.NoSync); err != nil {
			t.Fatalf("batch.Commit() error = %v", err)
		}
		batch.Close()

		// Update indexes
		batch = db.NewBatch()
		if err := mgr.UpdateIndexes(batch, oldEntry, newEntry); err != nil {
			t.Fatalf("UpdateIndexes() error = %v", err)
		}
		if err := batch.Commit(pebble.NoSync); err != nil {
			t.Fatalf("batch.Commit() error = %v", err)
		}
		batch.Close()

		// Verify old indexes are gone
		objKey, _ := ParseKey(oldEntry.Key)
		oldStoreIdx := objKey.StoreIndexKey("s3-old")
		_, _, err := db.Get(oldStoreIdx)
		if err != pebble.ErrNotFound {
			t.Errorf("old store index should be deleted")
		}

		// Verify new indexes exist
		newStoreIdx := objKey.StoreIndexKey("s3-new")
		_, closer, err := db.Get(newStoreIdx)
		if err != nil {
			t.Errorf("new store index should exist: %v", err)
		} else {
			closer.Close()
		}

		// Verify status index changed
		archivedIdx := objKey.StatusIndexKey("archived")
		_, closer, err = db.Get(archivedIdx)
		if err != nil {
			t.Errorf("archived status index should exist: %v", err)
		} else {
			closer.Close()
		}
	})

	t.Run("returns error with nil batch", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		old := testEntry("ns:path", "s3", "tenant", StatusActive)
		new := testEntry("ns:path", "s3", "tenant", StatusArchived)

		err := mgr.UpdateIndexes(nil, old, new)
		if err == nil {
			t.Fatal("UpdateIndexes(nil, old, new) expected error")
		}
	})

	t.Run("returns error with nil old entry", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		new := testEntry("ns:path", "s3", "tenant", StatusActive)

		batch := db.NewBatch()
		defer batch.Close()

		err := mgr.UpdateIndexes(batch, nil, new)
		if err == nil {
			t.Fatal("UpdateIndexes(batch, nil, new) expected error")
		}
	})

	t.Run("returns error with nil new entry", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		old := testEntry("ns:path", "s3", "tenant", StatusActive)

		batch := db.NewBatch()
		defer batch.Close()

		err := mgr.UpdateIndexes(batch, old, nil)
		if err == nil {
			t.Fatal("UpdateIndexes(batch, old, nil) expected error")
		}
	})
}

func TestIndexManager_ListByStore(t *testing.T) {
	t.Run("lists entries by store", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		// Create entries in different stores
		entries := []*CatalogEntry{
			testEntry("ns:file1.txt", "s3-primary", "tenant", StatusActive),
			testEntry("ns:file2.txt", "s3-primary", "tenant", StatusActive),
			testEntry("ns:file3.txt", "minio", "tenant", StatusActive),
		}

		for _, entry := range entries {
			batch := db.NewBatch()
			if err := mgr.WriteIndexes(batch, entry); err != nil {
				t.Fatalf("WriteIndexes() error = %v", err)
			}
			if err := batch.Commit(pebble.NoSync); err != nil {
				t.Fatalf("batch.Commit() error = %v", err)
			}
			batch.Close()
		}

		result, err := mgr.ListByStore(ctx, "s3-primary", nil)
		if err != nil {
			t.Fatalf("ListByStore() error = %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(result.Keys))
		}
	})

	t.Run("returns error for empty store", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.ListByStore(ctx, "", nil)
		if err == nil {
			t.Fatal("ListByStore() expected error for empty store")
		}
	})

	t.Run("respects pagination limit", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		// Create multiple entries
		for i := 0; i < 5; i++ {
			entry := testEntry("ns:file"+string(rune('a'+i))+".txt", "s3", "tenant", StatusActive)
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		result, err := mgr.ListByStore(ctx, "s3", &ListOptions{Limit: 2})
		if err != nil {
			t.Fatalf("ListByStore() error = %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys with limit, got %d", len(result.Keys))
		}

		if !result.HasMore {
			t.Error("expected HasMore to be true")
		}

		if result.NextCursor == "" {
			t.Error("expected NextCursor to be set")
		}
	})

	t.Run("supports cursor pagination", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		// Create entries
		for i := 0; i < 5; i++ {
			entry := testEntry("ns:file"+string(rune('a'+i))+".txt", "s3", "tenant", StatusActive)
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		// Get first page
		page1, err := mgr.ListByStore(ctx, "s3", &ListOptions{Limit: 2})
		if err != nil {
			t.Fatalf("ListByStore() page 1 error = %v", err)
		}

		// Get second page using cursor
		page2, err := mgr.ListByStore(ctx, "s3", &ListOptions{Limit: 2, Cursor: page1.NextCursor})
		if err != nil {
			t.Fatalf("ListByStore() page 2 error = %v", err)
		}

		// Verify no duplicates
		for _, k1 := range page1.Keys {
			for _, k2 := range page2.Keys {
				if k1 == k2 {
					t.Errorf("duplicate key across pages: %s", k1)
				}
			}
		}
	})

	t.Run("respects context cancellation", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		_, err := mgr.ListByStore(ctx, "s3", nil)
		if err == nil {
			t.Fatal("ListByStore() expected error for canceled context")
		}
	})

	t.Run("returns empty result for non-existent store", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		result, err := mgr.ListByStore(ctx, "non-existent", nil)
		if err != nil {
			t.Fatalf("ListByStore() error = %v", err)
		}

		if len(result.Keys) != 0 {
			t.Errorf("expected 0 keys, got %d", len(result.Keys))
		}
	})
}

func TestIndexManager_ListByTenant(t *testing.T) {
	t.Run("lists entries by tenant", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		entries := []*CatalogEntry{
			testEntry("ns:file1.txt", "s3", "acme-corp", StatusActive),
			testEntry("ns:file2.txt", "s3", "acme-corp", StatusActive),
			testEntry("ns:file3.txt", "s3", "other-tenant", StatusActive),
		}

		for _, entry := range entries {
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		result, err := mgr.ListByTenant(ctx, "acme-corp", nil)
		if err != nil {
			t.Fatalf("ListByTenant() error = %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(result.Keys))
		}
	})

	t.Run("returns error for empty tenant", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.ListByTenant(ctx, "", nil)
		if err == nil {
			t.Fatal("ListByTenant() expected error for empty tenant")
		}
	})
}

func TestIndexManager_ListByChecksum(t *testing.T) {
	t.Run("lists entries by checksum", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		checksum := sha256.Sum256([]byte("shared-content"))
		entry1 := testEntry("ns:file1.txt", "s3", "tenant", StatusActive)
		entry1.Checksum = checksum[:]

		entry2 := testEntry("ns:file2.txt", "s3", "tenant", StatusActive)
		entry2.Checksum = checksum[:] // Same checksum - deduplication scenario

		for _, entry := range []*CatalogEntry{entry1, entry2} {
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		result, err := mgr.ListByChecksum(ctx, checksum[:], nil)
		if err != nil {
			t.Fatalf("ListByChecksum() error = %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys with same checksum, got %d", len(result.Keys))
		}
	})

	t.Run("returns error for empty checksum", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.ListByChecksum(ctx, nil, nil)
		if err == nil {
			t.Fatal("ListByChecksum() expected error for empty checksum")
		}
	})
}

func TestIndexManager_ListByStatus(t *testing.T) {
	t.Run("lists entries by status", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		entries := []*CatalogEntry{
			testEntry("ns:active1.txt", "s3", "tenant", StatusActive),
			testEntry("ns:active2.txt", "s3", "tenant", StatusActive),
			testEntry("ns:archived.txt", "s3", "tenant", StatusArchived),
			testEntry("ns:deleted.txt", "s3", "tenant", StatusDeleted),
		}

		for _, entry := range entries {
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		result, err := mgr.ListByStatus(ctx, StatusActive, nil)
		if err != nil {
			t.Fatalf("ListByStatus() error = %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 active keys, got %d", len(result.Keys))
		}
	})

	t.Run("returns error for empty status", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.ListByStatus(ctx, "", nil)
		if err == nil {
			t.Fatal("ListByStatus() expected error for empty status")
		}
	})
}

func TestIndexManager_ListByTag(t *testing.T) {
	t.Run("lists entries by tag", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		entry1 := testEntry("ns:file1.txt", "s3", "tenant", StatusActive)
		entry1.Tags = map[string]string{"env": "prod", "team": "alpha"}

		entry2 := testEntry("ns:file2.txt", "s3", "tenant", StatusActive)
		entry2.Tags = map[string]string{"env": "prod"}

		entry3 := testEntry("ns:file3.txt", "s3", "tenant", StatusActive)
		entry3.Tags = map[string]string{"env": "staging"}

		for _, entry := range []*CatalogEntry{entry1, entry2, entry3} {
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		result, err := mgr.ListByTag(ctx, "env", "prod", nil)
		if err != nil {
			t.Fatalf("ListByTag() error = %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys with env=prod tag, got %d", len(result.Keys))
		}
	})

	t.Run("returns error for empty tag name", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.ListByTag(ctx, "", "value", nil)
		if err == nil {
			t.Fatal("ListByTag() expected error for empty tag name")
		}
	})
}

func TestIndexManager_ListByEncryptionKey(t *testing.T) {
	t.Run("lists entries by encryption key", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		entry1 := testEntry("ns:file1.txt", "s3", "tenant", StatusActive)
		entry1.Encryption = &ObjectEncryption{Enabled: true, KeyID: "kms-key-abc"}

		entry2 := testEntry("ns:file2.txt", "s3", "tenant", StatusActive)
		entry2.Encryption = &ObjectEncryption{Enabled: true, KeyID: "kms-key-abc"}

		entry3 := testEntry("ns:file3.txt", "s3", "tenant", StatusActive)
		entry3.Encryption = &ObjectEncryption{Enabled: true, KeyID: "kms-key-xyz"}

		for _, entry := range []*CatalogEntry{entry1, entry2, entry3} {
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		result, err := mgr.ListByEncryptionKey(ctx, "kms-key-abc", nil)
		if err != nil {
			t.Fatalf("ListByEncryptionKey() error = %v", err)
		}

		if len(result.Keys) != 2 {
			t.Errorf("expected 2 keys with same encryption key, got %d", len(result.Keys))
		}
	})

	t.Run("returns error for empty key ID", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.ListByEncryptionKey(ctx, "", nil)
		if err == nil {
			t.Fatal("ListByEncryptionKey() expected error for empty key ID")
		}
	})
}

func TestIndexManager_CountByStore(t *testing.T) {
	t.Run("counts entries by store", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		entries := []*CatalogEntry{
			testEntry("ns:file1.txt", "s3", "tenant", StatusActive),
			testEntry("ns:file2.txt", "s3", "tenant", StatusActive),
			testEntry("ns:file3.txt", "s3", "tenant", StatusActive),
			testEntry("ns:file4.txt", "minio", "tenant", StatusActive),
		}

		for _, entry := range entries {
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		count, err := mgr.CountByStore(ctx, "s3")
		if err != nil {
			t.Fatalf("CountByStore() error = %v", err)
		}

		if count != 3 {
			t.Errorf("expected count 3, got %d", count)
		}
	})

	t.Run("returns error for empty store", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.CountByStore(ctx, "")
		if err == nil {
			t.Fatal("CountByStore() expected error for empty store")
		}
	})

	t.Run("returns 0 for non-existent store", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		count, err := mgr.CountByStore(ctx, "non-existent")
		if err != nil {
			t.Fatalf("CountByStore() error = %v", err)
		}

		if count != 0 {
			t.Errorf("expected count 0, got %d", count)
		}
	})
}

func TestIndexManager_CountByTenant(t *testing.T) {
	t.Run("counts entries by tenant", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		entries := []*CatalogEntry{
			testEntry("ns:file1.txt", "s3", "tenant-a", StatusActive),
			testEntry("ns:file2.txt", "s3", "tenant-a", StatusActive),
			testEntry("ns:file3.txt", "s3", "tenant-b", StatusActive),
		}

		for _, entry := range entries {
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		count, err := mgr.CountByTenant(ctx, "tenant-a")
		if err != nil {
			t.Fatalf("CountByTenant() error = %v", err)
		}

		if count != 2 {
			t.Errorf("expected count 2, got %d", count)
		}
	})

	t.Run("returns error for empty tenant", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.CountByTenant(ctx, "")
		if err == nil {
			t.Fatal("CountByTenant() expected error for empty tenant")
		}
	})
}

func TestIndexManager_CountByStatus(t *testing.T) {
	t.Run("counts entries by status", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		entries := []*CatalogEntry{
			testEntry("ns:active1.txt", "s3", "tenant", StatusActive),
			testEntry("ns:active2.txt", "s3", "tenant", StatusActive),
			testEntry("ns:active3.txt", "s3", "tenant", StatusActive),
			testEntry("ns:archived.txt", "s3", "tenant", StatusArchived),
		}

		for _, entry := range entries {
			batch := db.NewBatch()
			mgr.WriteIndexes(batch, entry)
			batch.Commit(pebble.NoSync)
			batch.Close()
		}

		count, err := mgr.CountByStatus(ctx, StatusActive)
		if err != nil {
			t.Fatalf("CountByStatus() error = %v", err)
		}

		if count != 3 {
			t.Errorf("expected count 3, got %d", count)
		}
	})

	t.Run("returns error for empty status", func(t *testing.T) {
		db, cleanup := testPebbleDB(t)
		defer cleanup()

		mgr, _ := NewIndexManager(db)
		ctx := context.Background()

		_, err := mgr.CountByStatus(ctx, "")
		if err == nil {
			t.Fatal("CountByStatus() expected error for empty status")
		}
	})
}

func TestIndexPrefixUpperBound(t *testing.T) {
	tests := []struct {
		name     string
		prefix   []byte
		expected []byte
	}{
		{
			name:     "normal prefix",
			prefix:   []byte("idx:store:s3:"),
			expected: []byte("idx:store:s3;"), // : (0x3A) + 1 = ; (0x3B)
		},
		{
			name:     "empty prefix",
			prefix:   []byte{},
			expected: nil,
		},
		{
			name:     "single byte",
			prefix:   []byte{0x41}, // 'A'
			expected: []byte{0x42}, // 'B'
		},
		{
			name:     "all 0xFF",
			prefix:   []byte{0xFF, 0xFF, 0xFF},
			expected: nil,
		},
		{
			name:     "trailing 0xFF",
			prefix:   []byte{0x41, 0xFF, 0xFF},
			expected: []byte{0x42},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := indexPrefixUpperBound(tt.prefix)
			if string(result) != string(tt.expected) {
				t.Errorf("indexPrefixUpperBound(%v) = %v, want %v", tt.prefix, result, tt.expected)
			}
		})
	}
}

func TestListOptions_InvalidCursor(t *testing.T) {
	db, cleanup := testPebbleDB(t)
	defer cleanup()

	mgr, _ := NewIndexManager(db)
	ctx := context.Background()

	_, err := mgr.ListByStore(ctx, "s3", &ListOptions{Cursor: "not-valid-base64!@#$"})
	if err == nil {
		t.Fatal("ListByStore() expected error for invalid cursor")
	}

	var ce *CatalogError
	if !errors.As(err, &ce) {
		t.Fatalf("expected CatalogError, got %T", err)
	}
}
