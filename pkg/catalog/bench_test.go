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
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/require"
)

// benchmarkSetup creates a temporary PebbleDB storage for benchmarks.
func benchmarkSetup(b *testing.B) (PebbleStorage, func()) {
	b.Helper()

	tmpDir, err := os.MkdirTemp("", "catalog-bench-*")
	require.NoError(b, err)

	storage, err := NewPebbleStorage(&PebbleStorageConfig{
		Path:       filepath.Join(tmpDir, "catalog"),
		SyncWrites: false, // Disable sync for benchmark performance
	})
	require.NoError(b, err)

	cleanup := func() {
		storage.Close()
		os.RemoveAll(tmpDir)
	}
	b.Cleanup(cleanup)

	return storage, cleanup
}

// createBenchEntry creates a catalog entry with realistic data for benchmarking.
func createBenchEntry(key string, dataSize int) *CatalogEntry {
	// Create realistic checksum
	data := make([]byte, dataSize)
	for i := range data {
		data[i] = byte(i % 256)
	}
	checksum := sha256.Sum256(data)

	now := time.Now().UnixNano()
	entry := &CatalogEntry{
		Key:          key,
		ObjectID:     uint64(time.Now().UnixNano()),
		Checksum:     checksum[:],
		Size:         int64(dataSize),
		ContentType:  "application/octet-stream",
		PrimaryStore: *NewStoreLocation("s3-primary", fmt.Sprintf("objects/%s", key)),
		TenantID:     "tenant-acme",
		ShardID:      1,
		Owner:        "user@example.com",
		CreatedAt:    now,
		ModifiedAt:   now,
		LastAccessAt: now,
		Status:       StatusActive,
		Version:      1,
		Tags: map[string]string{
			"env":     "production",
			"region":  "us-west-2",
			"tier":    "hot",
			"project": "benchmark",
		},
		Encryption: &ObjectEncryption{
			Enabled:   true,
			KeyID:     "kms/keys/data-key-1",
			Algorithm: "AES-256-GCM",
		},
	}

	entry.PrimaryStore.StoredSize = int64(dataSize)
	entry.PrimaryStore.Compressed = true

	return entry
}

// =============================================================================
// CatalogManager Benchmarks
// =============================================================================

// BenchmarkCatalogManager_Put benchmarks single put operations.
func BenchmarkCatalogManager_Put(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		entry := createBenchEntry(fmt.Sprintf("ns%d:path/to/object-%d", i%10, i), 1024)
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCatalogManager_Put_LargeData benchmarks put operations with larger data size.
func BenchmarkCatalogManager_Put_LargeData(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		entry := createBenchEntry(fmt.Sprintf("ns%d:path/to/large-object-%d", i%10, i), 10*1024) // 10KB
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCatalogManager_Get benchmarks single get operations.
func BenchmarkCatalogManager_Get(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries
	numEntries := 1000
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:objects/entry-%d", i), 1024)
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench:objects/entry-%d", i%numEntries)
		_, err := mgr.Get(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCatalogManager_Put_Parallel benchmarks parallel put operations.
func BenchmarkCatalogManager_Put_Parallel(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()
	var counter atomic.Uint64

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := counter.Add(1)
			entry := createBenchEntry(fmt.Sprintf("ns%d:path/parallel/object-%d", id%10, id), 1024)
			if err := mgr.Put(ctx, entry); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCatalogManager_Get_Parallel benchmarks parallel get operations.
func BenchmarkCatalogManager_Get_Parallel(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries
	numEntries := 1000
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:parallel/entry-%d", i), 1024)
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	var counter atomic.Uint64

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := counter.Add(1)
			key := fmt.Sprintf("bench:parallel/entry-%d", id%uint64(numEntries))
			_, err := mgr.Get(ctx, key)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkCatalogManager_Exists benchmarks existence checks.
func BenchmarkCatalogManager_Exists(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries
	numEntries := 1000
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:exists/entry-%d", i), 1024)
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench:exists/entry-%d", i%numEntries)
		_, err := mgr.Exists(ctx, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCatalogManager_Delete benchmarks delete operations.
func BenchmarkCatalogManager_Delete(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries to delete
	for i := 0; i < b.N; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:delete/entry-%d", i), 1024)
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench:delete/entry-%d", i)
		if err := mgr.Delete(ctx, key); err != nil {
			b.Fatal(err)
		}
	}
}

// =============================================================================
// Index Manager Benchmarks
// =============================================================================

// BenchmarkIndexManager_WriteIndexes benchmarks index write performance.
func BenchmarkIndexManager_WriteIndexes(b *testing.B) {
	tmpDir, err := os.MkdirTemp("", "index-bench-*")
	require.NoError(b, err)
	defer os.RemoveAll(tmpDir)

	db, err := pebble.Open(filepath.Join(tmpDir, "index"), &pebble.Options{})
	require.NoError(b, err)
	defer db.Close()

	indexMgr, err := NewIndexManager(db)
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:index/entry-%d", i), 1024)
		batch := db.NewBatch()
		if err := indexMgr.WriteIndexes(batch, entry); err != nil {
			batch.Close()
			b.Fatal(err)
		}
		if err := batch.Commit(pebble.NoSync); err != nil {
			batch.Close()
			b.Fatal(err)
		}
		batch.Close()
	}
}

// BenchmarkIndexManager_ListByStore benchmarks listing entries by store.
func BenchmarkIndexManager_ListByStore(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries across multiple stores
	numEntries := 1000
	stores := []string{"s3-primary", "s3-replica", "gcs-backup"}
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:store/entry-%d", i), 1024)
		entry.PrimaryStore.Store = stores[i%len(stores)]
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	indexMgr := storage.GetPebbleIndexManager()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		store := stores[i%len(stores)]
		_, err := indexMgr.ListByStore(ctx, store, &ListOptions{Limit: 100})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIndexManager_ListByTenant benchmarks listing entries by tenant.
func BenchmarkIndexManager_ListByTenant(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries across multiple tenants
	numEntries := 1000
	tenants := []string{"tenant-acme", "tenant-beta", "tenant-gamma"}
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:tenant/entry-%d", i), 1024)
		entry.TenantID = tenants[i%len(tenants)]
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	indexMgr := storage.GetPebbleIndexManager()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		tenant := tenants[i%len(tenants)]
		_, err := indexMgr.ListByTenant(ctx, tenant, &ListOptions{Limit: 100})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIndexManager_ListByStatus benchmarks listing entries by status.
func BenchmarkIndexManager_ListByStatus(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries with different statuses
	numEntries := 1000
	statuses := []EntryStatus{StatusActive, StatusPending, StatusArchived}
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:status/entry-%d", i), 1024)
		entry.Status = statuses[i%len(statuses)]
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	indexMgr := storage.GetPebbleIndexManager()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		status := statuses[i%len(statuses)]
		_, err := indexMgr.ListByStatus(ctx, status, &ListOptions{Limit: 100})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIndexManager_ListByTag benchmarks listing entries by tag.
func BenchmarkIndexManager_ListByTag(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries with different tags
	numEntries := 1000
	envs := []string{"production", "staging", "development"}
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:tag/entry-%d", i), 1024)
		entry.Tags["env"] = envs[i%len(envs)]
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	indexMgr := storage.GetPebbleIndexManager()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		env := envs[i%len(envs)]
		_, err := indexMgr.ListByTag(ctx, "env", env, &ListOptions{Limit: 100})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkIndexManager_CountByStore benchmarks counting entries by store.
func BenchmarkIndexManager_CountByStore(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()

	// Pre-populate with entries
	numEntries := 500
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:count/entry-%d", i), 1024)
		if err := mgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	indexMgr := storage.GetPebbleIndexManager()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := indexMgr.CountByStore(ctx, "s3-primary")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// =============================================================================
// Key Parsing Benchmarks
// =============================================================================

// BenchmarkParseKey benchmarks key parsing performance.
func BenchmarkParseKey(b *testing.B) {
	keys := []string{
		"ns:simple",
		"tenant-acme:documents/reports/2024/q1.pdf",
		"backups:daily/2024-01-15/database.tar.gz",
		"prod:services/api/config/main.yaml",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := keys[i%len(keys)]
		_, err := ParseKey(key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParseKey_Long benchmarks parsing long keys.
func BenchmarkParseKey_Long(b *testing.B) {
	// Create a long but valid key
	namespace := "tenant-very-long-namespace-name"
	path := "very/long/path/with/many/segments/to/simulate/realistic/usage/patterns/in/production/systems/file.dat"
	key := namespace + ":" + path

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := ParseKey(key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkObjectKey_CatalogKey benchmarks catalog key generation.
func BenchmarkObjectKey_CatalogKey(b *testing.B) {
	objKey := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "documents/reports/2024/q1.pdf",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = objKey.CatalogKey()
	}
}

// BenchmarkObjectKey_IndexKey benchmarks index key generation.
func BenchmarkObjectKey_IndexKey(b *testing.B) {
	objKey := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "documents/reports/2024/q1.pdf",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = objKey.StoreIndexKey("s3-primary")
	}
}

// BenchmarkObjectKey_TagIndexKey benchmarks tag index key generation.
func BenchmarkObjectKey_TagIndexKey(b *testing.B) {
	objKey := &ObjectKey{
		Namespace: "tenant-acme",
		Path:      "documents/reports/2024/q1.pdf",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = objKey.TagIndexKey("env", "production")
	}
}

// BenchmarkIsValidNamespace benchmarks namespace validation.
func BenchmarkIsValidNamespace(b *testing.B) {
	namespaces := []string{
		"simple",
		"tenant-acme",
		"ns_with_underscore",
		"mixed-ns_123",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ns := namespaces[i%len(namespaces)]
		_ = IsValidNamespace(ns)
	}
}

// BenchmarkIsValidPath benchmarks path validation.
func BenchmarkIsValidPath(b *testing.B) {
	paths := []string{
		"simple.txt",
		"documents/reports/2024/q1.pdf",
		"very/long/path/with/many/segments/file.dat",
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		path := paths[i%len(paths)]
		_ = IsValidPath(path)
	}
}

// =============================================================================
// ACL Manager Benchmarks
// =============================================================================

// BenchmarkACLManager_CheckAccess benchmarks ACL access checks.
func BenchmarkACLManager_CheckAccess(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	catalogMgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer catalogMgr.Close()

	aclMgr, err := NewACLManager(&ACLManagerConfig{
		Catalog: catalogMgr,
	})
	require.NoError(b, err)

	ctx := context.Background()

	// Pre-populate with entries with ACLs
	numEntries := 100
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:acl/entry-%d", i), 1024)
		entry.Owner = "owner@example.com"
		entry.ACL = &ObjectACL{
			Owner:   "owner@example.com",
			Readers: []string{"reader1@example.com", "reader2@example.com"},
			Writers: []string{"writer@example.com"},
			Admins:  []string{"admin@example.com"},
		}
		if err := catalogMgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	principals := []string{"owner@example.com", "reader1@example.com", "writer@example.com", "unknown@example.com"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench:acl/entry-%d", i%numEntries)
		principal := principals[i%len(principals)]
		_, err := aclMgr.CheckAccess(ctx, principal, key, ActionRead)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkACLManager_CheckAccess_Parallel benchmarks parallel ACL access checks.
func BenchmarkACLManager_CheckAccess_Parallel(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	catalogMgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer catalogMgr.Close()

	aclMgr, err := NewACLManager(&ACLManagerConfig{
		Catalog: catalogMgr,
	})
	require.NoError(b, err)

	ctx := context.Background()

	// Pre-populate with entries with ACLs
	numEntries := 100
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:acl-parallel/entry-%d", i), 1024)
		entry.Owner = "owner@example.com"
		entry.ACL = &ObjectACL{
			Owner:   "owner@example.com",
			Readers: []string{"reader1@example.com", "reader2@example.com"},
			Writers: []string{"writer@example.com"},
			Admins:  []string{"admin@example.com"},
		}
		if err := catalogMgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	principals := []string{"owner@example.com", "reader1@example.com", "writer@example.com", "unknown@example.com"}
	var counter atomic.Uint64

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			id := counter.Add(1)
			key := fmt.Sprintf("bench:acl-parallel/entry-%d", id%uint64(numEntries))
			principal := principals[id%uint64(len(principals))]
			_, err := aclMgr.CheckAccess(ctx, principal, key, ActionRead)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkACLManager_Grant benchmarks granting access.
func BenchmarkACLManager_Grant(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	catalogMgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer catalogMgr.Close()

	aclMgr, err := NewACLManager(&ACLManagerConfig{
		Catalog: catalogMgr,
	})
	require.NoError(b, err)

	ctx := context.Background()

	// Pre-populate with entries
	numEntries := 100
	for i := 0; i < numEntries; i++ {
		entry := createBenchEntry(fmt.Sprintf("bench:grant/entry-%d", i), 1024)
		entry.Owner = "owner@example.com"
		if err := catalogMgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("bench:grant/entry-%d", i%numEntries)
		principal := fmt.Sprintf("user%d@example.com", i)
		err := aclMgr.GrantAccess(ctx, key, principal, []ObjectAction{ActionRead, ActionWrite})
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkACLManager_BatchCheckAccess benchmarks batch access checks.
func BenchmarkACLManager_BatchCheckAccess(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	catalogMgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer catalogMgr.Close()

	aclMgr, err := NewACLManager(&ACLManagerConfig{
		Catalog: catalogMgr,
	})
	require.NoError(b, err)

	ctx := context.Background()

	// Pre-populate with entries with ACLs
	numEntries := 100
	keys := make([]string, numEntries)
	for i := 0; i < numEntries; i++ {
		key := fmt.Sprintf("bench:batch-acl/entry-%d", i)
		keys[i] = key
		entry := createBenchEntry(key, 1024)
		entry.Owner = "owner@example.com"
		entry.ACL = &ObjectACL{
			Owner:   "owner@example.com",
			Readers: []string{"reader@example.com"},
		}
		if err := catalogMgr.Put(ctx, entry); err != nil {
			b.Fatal(err)
		}
	}

	// Create batch of 10 keys to check
	batchSize := 10
	batchKeys := keys[:batchSize]

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := aclMgr.BatchCheckAccess(ctx, "reader@example.com", batchKeys, ActionRead)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// =============================================================================
// Entry Encoding Benchmarks
// =============================================================================

// BenchmarkCatalogEntry_Encode benchmarks entry encoding.
func BenchmarkCatalogEntry_Encode(b *testing.B) {
	entry := createBenchEntry("bench:encode/object", 1024)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := entry.Encode()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCatalogEntry_Decode benchmarks entry decoding.
func BenchmarkCatalogEntry_Decode(b *testing.B) {
	entry := createBenchEntry("bench:decode/object", 1024)
	data, err := entry.Encode()
	require.NoError(b, err)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := DecodeCatalogEntry(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCatalogEntry_Clone benchmarks entry cloning.
func BenchmarkCatalogEntry_Clone(b *testing.B) {
	entry := createBenchEntry("bench:clone/object", 1024)
	entry.Replicas = []StoreLocation{
		*NewStoreLocation("s3-replica-1", "replica-1"),
		*NewStoreLocation("s3-replica-2", "replica-2"),
	}
	entry.ACL = &ObjectACL{
		Owner:   "owner@example.com",
		Readers: []string{"reader1@example.com", "reader2@example.com"},
		Writers: []string{"writer@example.com"},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = entry.Clone()
	}
}

// =============================================================================
// Scalability Benchmarks
// =============================================================================

// BenchmarkCatalogManager_Scalability tests performance with increasing entry counts.
func BenchmarkCatalogManager_Scalability(b *testing.B) {
	entryCounts := []int{100, 1000, 10000}

	for _, count := range entryCounts {
		b.Run(fmt.Sprintf("Entries-%d", count), func(b *testing.B) {
			storage, _ := benchmarkSetup(b)
			mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
			require.NoError(b, err)
			defer mgr.Close()

			ctx := context.Background()

			// Pre-populate with entries
			for i := 0; i < count; i++ {
				entry := createBenchEntry(fmt.Sprintf("scale:entries/entry-%d", i), 1024)
				if err := mgr.Put(ctx, entry); err != nil {
					b.Fatal(err)
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("scale:entries/entry-%d", i%count)
				_, err := mgr.Get(ctx, key)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkIndexManager_Scalability tests index listing with increasing entry counts.
func BenchmarkIndexManager_Scalability(b *testing.B) {
	entryCounts := []int{100, 1000, 10000}

	for _, count := range entryCounts {
		b.Run(fmt.Sprintf("Entries-%d", count), func(b *testing.B) {
			storage, _ := benchmarkSetup(b)
			mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
			require.NoError(b, err)
			defer mgr.Close()

			ctx := context.Background()

			// Pre-populate with entries for single tenant
			for i := 0; i < count; i++ {
				entry := createBenchEntry(fmt.Sprintf("scale:index/entry-%d", i), 1024)
				entry.TenantID = "scale-tenant"
				if err := mgr.Put(ctx, entry); err != nil {
					b.Fatal(err)
				}
			}

			indexMgr := storage.GetPebbleIndexManager()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := indexMgr.ListByTenant(ctx, "scale-tenant", &ListOptions{Limit: 100})
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// =============================================================================
// Queue Operations Benchmarks
// =============================================================================

// BenchmarkCatalogManager_EnqueueReplication benchmarks enqueuing replication tasks.
func BenchmarkCatalogManager_EnqueueReplication(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()
	targets := []string{"s3-replica-1", "s3-replica-2"}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := fmt.Sprintf("queue:replication/entry-%d", i)
		err := mgr.EnqueueReplication(ctx, key, targets, i%10)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCatalogManager_DequeueReplication benchmarks dequeuing replication tasks.
func BenchmarkCatalogManager_DequeueReplication(b *testing.B) {
	storage, _ := benchmarkSetup(b)
	mgr, err := NewCatalogManager(&ManagerConfig{Storage: storage})
	require.NoError(b, err)
	defer mgr.Close()

	ctx := context.Background()
	targets := []string{"s3-replica-1", "s3-replica-2"}

	// Pre-populate the queue
	numTasks := 10000
	for i := 0; i < numTasks; i++ {
		key := fmt.Sprintf("queue:dequeue/entry-%d", i)
		if err := mgr.EnqueueReplication(ctx, key, targets, i%10); err != nil {
			b.Fatal(err)
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, err := mgr.DequeueReplication(ctx, 10)
		if err != nil {
			b.Fatal(err)
		}

		// Re-populate to keep the benchmark consistent
		if i%100 == 0 {
			for j := 0; j < 100; j++ {
				key := fmt.Sprintf("queue:refill/entry-%d-%d", i, j)
				_ = mgr.EnqueueReplication(ctx, key, targets, j%10)
			}
		}
	}
}
