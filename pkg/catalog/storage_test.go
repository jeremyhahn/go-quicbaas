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
	"strconv"
	"testing"
	"time"
)

func createTestPebbleStorage(t *testing.T) PebbleStorage {
	t.Helper()
	tmpDir := t.TempDir()
	cfg := &PebbleStorageConfig{
		Path:       tmpDir,
		SyncWrites: false,
	}
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		t.Fatalf("failed to create storage: %v", err)
	}
	t.Cleanup(func() {
		storage.Close()
	})
	return storage
}

func createPebbleTestEntry(key string) *CatalogEntry {
	now := time.Now().UnixNano()
	return &CatalogEntry{
		Key:          key,
		TenantID:     "tenant1",
		Size:         1024,
		Checksum:     []byte{0x01, 0x02, 0x03, 0x04},
		Status:       StatusActive,
		Tags:         map[string]string{"env": "test"},
		CreatedAt:    now,
		ModifiedAt:   now,
		LastAccessAt: now,
		Version:      1,
		PrimaryStore: StoreLocation{
			Store:     "primary-s3",
			ObjectRef: "/data/obj1",
			Status:    LocationStatusActive,
			SyncedAt:  now,
		},
		Encryption: &ObjectEncryption{
			Enabled: true,
			KeyID:   "key-001",
		},
	}
}

func TestNewPebbleStorage(t *testing.T) {
	t.Run("with valid config", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &PebbleStorageConfig{Path: tmpDir}
		storage, err := NewPebbleStorage(cfg)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		defer storage.Close()

		if storage.DB() == nil {
			t.Error("expected DB to be non-nil")
		}
		if storage.GetPebbleIndexManager() == nil {
			t.Error("expected IndexManager to be non-nil")
		}
	})

	t.Run("with nil config uses defaults", func(t *testing.T) {
		_, err := NewPebbleStorage(nil)
		if err == nil {
			t.Fatal("expected error for nil config path")
		}
	})

	t.Run("with empty path returns error", func(t *testing.T) {
		cfg := &PebbleStorageConfig{Path: ""}
		_, err := NewPebbleStorage(cfg)
		if err == nil {
			t.Fatal("expected error for empty path")
		}
	})
}

func TestPebbleStorageConfigValidate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &PebbleStorageConfig{Path: "/tmp/test"}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("empty path returns error", func(t *testing.T) {
		cfg := &PebbleStorageConfig{}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected error for empty path")
		}
	})

	t.Run("negative cache size returns error", func(t *testing.T) {
		cfg := &PebbleStorageConfig{Path: "/tmp/test", CacheSize: -1}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected error for negative cache size")
		}
	})

	t.Run("negative write buffer size returns error", func(t *testing.T) {
		cfg := &PebbleStorageConfig{Path: "/tmp/test", WriteBufferSize: -1}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected error for negative write buffer size")
		}
	})

	t.Run("negative max open files returns error", func(t *testing.T) {
		cfg := &PebbleStorageConfig{Path: "/tmp/test", MaxOpenFiles: -1}
		if err := cfg.Validate(); err == nil {
			t.Fatal("expected error for negative max open files")
		}
	})
}

func TestPebbleStorageConfigWithDefaults(t *testing.T) {
	cfg := &PebbleStorageConfig{Path: "/tmp/test"}
	withDefaults := cfg.WithDefaults()

	defaults := DefaultPebbleStorageConfig()
	if withDefaults.CacheSize != defaults.CacheSize {
		t.Errorf("expected CacheSize %d, got %d", defaults.CacheSize, withDefaults.CacheSize)
	}
	if withDefaults.WriteBufferSize != defaults.WriteBufferSize {
		t.Errorf("expected WriteBufferSize %d, got %d", defaults.WriteBufferSize, withDefaults.WriteBufferSize)
	}
	if withDefaults.MaxOpenFiles != defaults.MaxOpenFiles {
		t.Errorf("expected MaxOpenFiles %d, got %d", defaults.MaxOpenFiles, withDefaults.MaxOpenFiles)
	}
}

func TestPebbleStoragePutGetEntry(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("put and get entry", func(t *testing.T) {
		entry := createPebbleTestEntry("ns1:path/to/object")
		if err := storage.PutEntry(ctx, entry); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		retrieved, err := storage.GetEntry(ctx, entry.Key)
		if err != nil {
			t.Fatalf("failed to get entry: %v", err)
		}

		if retrieved.Key != entry.Key {
			t.Errorf("expected key %s, got %s", entry.Key, retrieved.Key)
		}
		if retrieved.TenantID != entry.TenantID {
			t.Errorf("expected tenant %s, got %s", entry.TenantID, retrieved.TenantID)
		}
		if retrieved.Size != entry.Size {
			t.Errorf("expected size %d, got %d", entry.Size, retrieved.Size)
		}
	})

	t.Run("get non-existent entry returns not found", func(t *testing.T) {
		_, err := storage.GetEntry(ctx, "ns1:nonexistent")
		if err == nil {
			t.Fatal("expected error for non-existent entry")
		}
		if !IsNotFound(err) {
			t.Errorf("expected not found error, got %v", err)
		}
	})

	t.Run("put nil entry returns error", func(t *testing.T) {
		if err := storage.PutEntry(ctx, nil); err == nil {
			t.Fatal("expected error for nil entry")
		}
	})

	t.Run("put entry with empty key returns error", func(t *testing.T) {
		entry := createPebbleTestEntry("")
		if err := storage.PutEntry(ctx, entry); err == nil {
			t.Fatal("expected error for empty key")
		}
	})
}

func TestPebbleStorageDeleteEntry(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("delete existing entry", func(t *testing.T) {
		entry := createPebbleTestEntry("ns1:to-delete")
		if err := storage.PutEntry(ctx, entry); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		if err := storage.DeleteEntry(ctx, entry.Key); err != nil {
			t.Fatalf("failed to delete entry: %v", err)
		}

		_, err := storage.GetEntry(ctx, entry.Key)
		if !IsNotFound(err) {
			t.Error("expected entry to be deleted")
		}
	})

	t.Run("delete non-existent entry is no-op", func(t *testing.T) {
		if err := storage.DeleteEntry(ctx, "ns1:nonexistent"); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestPebbleStorageBatchOperations(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("put and get multiple entries", func(t *testing.T) {
		entries := []*CatalogEntry{
			createPebbleTestEntry("ns1:batch/obj1"),
			createPebbleTestEntry("ns1:batch/obj2"),
			createPebbleTestEntry("ns1:batch/obj3"),
		}

		if err := storage.PutEntries(ctx, entries); err != nil {
			t.Fatalf("failed to put entries: %v", err)
		}

		keys := []string{entries[0].Key, entries[1].Key, entries[2].Key}
		retrieved, err := storage.GetEntries(ctx, keys)
		if err != nil {
			t.Fatalf("failed to get entries: %v", err)
		}
		if len(retrieved) != 3 {
			t.Errorf("expected 3 entries, got %d", len(retrieved))
		}
	})

	t.Run("get entries with some not found", func(t *testing.T) {
		entry := createPebbleTestEntry("ns1:batch/exists")
		if err := storage.PutEntry(ctx, entry); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		keys := []string{entry.Key, "ns1:batch/missing"}
		retrieved, err := storage.GetEntries(ctx, keys)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(retrieved) != 1 {
			t.Errorf("expected 1 entry, got %d", len(retrieved))
		}
	})

	t.Run("delete multiple entries", func(t *testing.T) {
		entries := []*CatalogEntry{
			createPebbleTestEntry("ns1:todelete/obj1"),
			createPebbleTestEntry("ns1:todelete/obj2"),
		}
		if err := storage.PutEntries(ctx, entries); err != nil {
			t.Fatalf("failed to put entries: %v", err)
		}

		keys := []string{entries[0].Key, entries[1].Key}
		if err := storage.DeleteEntries(ctx, keys); err != nil {
			t.Fatalf("failed to delete entries: %v", err)
		}

		retrieved, err := storage.GetEntries(ctx, keys)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(retrieved) != 0 {
			t.Errorf("expected 0 entries, got %d", len(retrieved))
		}
	})

	t.Run("put empty entries is no-op", func(t *testing.T) {
		if err := storage.PutEntries(ctx, nil); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if err := storage.PutEntries(ctx, []*CatalogEntry{}); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("delete empty keys is no-op", func(t *testing.T) {
		if err := storage.DeleteEntries(ctx, nil); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if err := storage.DeleteEntries(ctx, []string{}); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}

func TestPebbleStorageIterate(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("iterate all entries", func(t *testing.T) {
		entries := []*CatalogEntry{
			createPebbleTestEntry("ns1:iter/obj1"),
			createPebbleTestEntry("ns1:iter/obj2"),
			createPebbleTestEntry("ns1:iter/obj3"),
		}
		if err := storage.PutEntries(ctx, entries); err != nil {
			t.Fatalf("failed to put entries: %v", err)
		}

		var count int
		err := storage.Iterate(ctx, "", func(e *CatalogEntry) bool {
			count++
			return true
		})
		if err != nil {
			t.Fatalf("iterate failed: %v", err)
		}
		if count < 3 {
			t.Errorf("expected at least 3 entries, got %d", count)
		}
	})

	t.Run("iterate with prefix", func(t *testing.T) {
		if err := storage.PutEntry(ctx, createPebbleTestEntry("ns1:prefix/a")); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}
		if err := storage.PutEntry(ctx, createPebbleTestEntry("ns1:prefix/b")); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}
		if err := storage.PutEntry(ctx, createPebbleTestEntry("ns1:other/c")); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		var count int
		err := storage.Iterate(ctx, "ns1:prefix/", func(e *CatalogEntry) bool {
			count++
			return true
		})
		if err != nil {
			t.Fatalf("iterate failed: %v", err)
		}
		if count != 2 {
			t.Errorf("expected 2 entries with prefix, got %d", count)
		}
	})

	t.Run("iterate stops when callback returns false", func(t *testing.T) {
		if err := storage.PutEntry(ctx, createPebbleTestEntry("ns1:stop/a")); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}
		if err := storage.PutEntry(ctx, createPebbleTestEntry("ns1:stop/b")); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}
		if err := storage.PutEntry(ctx, createPebbleTestEntry("ns1:stop/c")); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		var count int
		err := storage.Iterate(ctx, "ns1:stop/", func(e *CatalogEntry) bool {
			count++
			return count < 2
		})
		if err != nil {
			t.Fatalf("iterate failed: %v", err)
		}
		if count != 2 {
			t.Errorf("expected iteration to stop after 2, got %d", count)
		}
	})
}

func TestPebbleStorageIterateKeys(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("iterate keys", func(t *testing.T) {
		if err := storage.PutEntry(ctx, createPebbleTestEntry("ns1:keys/a")); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}
		if err := storage.PutEntry(ctx, createPebbleTestEntry("ns1:keys/b")); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		var keys []string
		err := storage.IterateKeys(ctx, "ns1:keys/", func(key string) bool {
			keys = append(keys, key)
			return true
		})
		if err != nil {
			t.Fatalf("iterate keys failed: %v", err)
		}
		if len(keys) != 2 {
			t.Errorf("expected 2 keys, got %d", len(keys))
		}
	})
}

func TestPebbleStorageSync(t *testing.T) {
	storage := createTestPebbleStorage(t)

	t.Run("sync succeeds", func(t *testing.T) {
		if err := storage.Sync(); err != nil {
			t.Fatalf("sync failed: %v", err)
		}
	})
}

func TestPebbleStorageClose(t *testing.T) {
	t.Run("close storage", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &PebbleStorageConfig{Path: tmpDir}
		storage, err := NewPebbleStorage(cfg)
		if err != nil {
			t.Fatalf("failed to create storage: %v", err)
		}

		if err := storage.Close(); err != nil {
			t.Fatalf("failed to close storage: %v", err)
		}

		// Second close should be no-op
		if err := storage.Close(); err != nil {
			t.Fatalf("second close should be no-op, got %v", err)
		}
	})

	t.Run("operations fail after close", func(t *testing.T) {
		tmpDir := t.TempDir()
		cfg := &PebbleStorageConfig{Path: tmpDir}
		storage, err := NewPebbleStorage(cfg)
		if err != nil {
			t.Fatalf("failed to create storage: %v", err)
		}
		storage.Close()

		ctx := context.Background()
		_, err = storage.GetEntry(ctx, "ns1:test")
		if !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}

		entry := createPebbleTestEntry("ns1:test")
		if err := storage.PutEntry(ctx, entry); !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}

		if err := storage.DeleteEntry(ctx, "ns1:test"); !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}

		if err := storage.Sync(); !IsCatalogClosed(err) {
			t.Errorf("expected catalog closed error, got %v", err)
		}
	})
}

func TestPebbleStorageContextCancellation(t *testing.T) {
	storage := createTestPebbleStorage(t)

	t.Run("cancelled context returns error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := storage.GetEntry(ctx, "ns1:test")
		if err == nil {
			t.Fatal("expected error for cancelled context")
		}

		entry := createPebbleTestEntry("ns1:test")
		if err := storage.PutEntry(ctx, entry); err == nil {
			t.Fatal("expected error for cancelled context")
		}

		if err := storage.DeleteEntry(ctx, "ns1:test"); err == nil {
			t.Fatal("expected error for cancelled context")
		}

		if err := storage.Iterate(ctx, "", func(e *CatalogEntry) bool { return true }); err == nil {
			t.Fatal("expected error for cancelled context")
		}

		if err := storage.IterateKeys(ctx, "", func(k string) bool { return true }); err == nil {
			t.Fatal("expected error for cancelled context")
		}
	})
}

func TestPebbleStorageIndexIntegration(t *testing.T) {
	storage := createTestPebbleStorage(t)
	ctx := context.Background()

	t.Run("entry with indexes is created correctly", func(t *testing.T) {
		entry := createPebbleTestEntry("ns1:indexed/obj1")
		entry.TenantID = "test-tenant"
		entry.Tags = map[string]string{"env": "prod", "team": "backend"}

		if err := storage.PutEntry(ctx, entry); err != nil {
			t.Fatalf("failed to put entry: %v", err)
		}

		// Verify the entry was stored
		retrieved, err := storage.GetEntry(ctx, entry.Key)
		if err != nil {
			t.Fatalf("failed to get entry: %v", err)
		}
		if retrieved.TenantID != entry.TenantID {
			t.Errorf("expected tenant %s, got %s", entry.TenantID, retrieved.TenantID)
		}

		// Verify indexes exist via IndexManager
		indexMgr := storage.GetPebbleIndexManager()
		result, err := indexMgr.ListByTenant(ctx, "test-tenant", nil)
		if err != nil {
			t.Fatalf("failed to list by tenant: %v", err)
		}
		if len(result.Keys) == 0 {
			t.Error("expected to find entry via tenant index")
		}
	})
}

func TestDefaultPebbleStorageConfig(t *testing.T) {
	cfg := DefaultPebbleStorageConfig()

	if cfg.CacheSize <= 0 {
		t.Error("expected positive cache size")
	}
	if cfg.WriteBufferSize <= 0 {
		t.Error("expected positive write buffer size")
	}
	if cfg.MaxOpenFiles <= 0 {
		t.Error("expected positive max open files")
	}
	if !cfg.Compression {
		t.Error("expected compression to be enabled by default")
	}
	if !cfg.SyncWrites {
		t.Error("expected sync writes to be enabled by default")
	}
}

func BenchmarkPebbleStoragePutEntry(b *testing.B) {
	tmpDir := b.TempDir()
	cfg := &PebbleStorageConfig{
		Path:       tmpDir,
		SyncWrites: false,
	}
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		b.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "ns1:bench/obj" + strconv.Itoa(i)
		entry := createPebbleTestEntry(key)
		if err := storage.PutEntry(ctx, entry); err != nil {
			b.Fatalf("failed to put entry: %v", err)
		}
	}
}

func BenchmarkPebbleStorageGetEntry(b *testing.B) {
	tmpDir := b.TempDir()
	cfg := &PebbleStorageConfig{
		Path:       tmpDir,
		SyncWrites: false,
	}
	storage, err := NewPebbleStorage(cfg)
	if err != nil {
		b.Fatalf("failed to create storage: %v", err)
	}
	defer storage.Close()

	ctx := context.Background()

	// Pre-populate with entries
	for i := 0; i < 1000; i++ {
		key := "ns1:bench/obj" + strconv.Itoa(i)
		entry := createPebbleTestEntry(key)
		if err := storage.PutEntry(ctx, entry); err != nil {
			b.Fatalf("failed to put entry: %v", err)
		}
	}

	key := "ns1:bench/obj" + strconv.Itoa(500)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := storage.GetEntry(ctx, key); err != nil {
			b.Fatalf("failed to get entry: %v", err)
		}
	}
}
