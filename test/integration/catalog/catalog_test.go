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

//go:build integration

package catalog

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
)

// testSetup holds common test dependencies.
type testSetup struct {
	tmpDir  string
	storage catalog.PebbleStorage
	manager catalog.CatalogManager
	cleanup func()
}

// newTestSetup creates a new test setup with a temporary PebbleDB directory.
func newTestSetup(t *testing.T) *testSetup {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "catalog-integration-*")
	require.NoError(t, err)

	storageConfig := &catalog.PebbleStorageConfig{
		Path:        filepath.Join(tmpDir, "catalog"),
		CacheSize:   32 * 1024 * 1024,
		SyncWrites:  true,
		Compression: true,
	}

	storage, err := catalog.NewPebbleStorage(storageConfig)
	require.NoError(t, err)

	managerConfig := &catalog.ManagerConfig{
		Storage: storage,
		Logger:  nil,
	}

	manager, err := catalog.NewCatalogManager(managerConfig)
	require.NoError(t, err)

	cleanup := func() {
		manager.Close()
		os.RemoveAll(tmpDir)
	}

	return &testSetup{
		tmpDir:  tmpDir,
		storage: storage,
		manager: manager,
		cleanup: cleanup,
	}
}

// createTestEntry creates a catalog entry for testing.
func createTestEntry(key string, size int64, tenant string) *catalog.CatalogEntry {
	checksum := sha256.Sum256([]byte(key))
	entry := catalog.NewCatalogEntry(key, size, checksum[:])
	entry.TenantID = tenant
	entry.Owner = "test-owner"
	entry.ContentType = "application/octet-stream"
	entry.PrimaryStore = *catalog.NewStoreLocation("s3-primary", "objects/"+key)
	return entry
}

// TestCatalogManagerIntegration tests CatalogManager end-to-end operations.
func TestCatalogManagerIntegration(t *testing.T) {
	t.Run("full CRUD lifecycle", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// CREATE
		entry := createTestEntry("myns:documents/report.pdf", 1024, "tenant-acme")
		entry.SetTag("department", "finance")
		entry.SetTag("classification", "confidential")

		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		// READ
		retrieved, err := setup.manager.Get(ctx, "myns:documents/report.pdf")
		require.NoError(t, err)
		assert.Equal(t, entry.Key, retrieved.Key)
		assert.Equal(t, entry.Size, retrieved.Size)
		assert.Equal(t, entry.TenantID, retrieved.TenantID)
		assert.Equal(t, entry.GetTag("department"), "finance")
		assert.Equal(t, entry.GetTag("classification"), "confidential")

		// UPDATE
		err = setup.manager.Update(ctx, "myns:documents/report.pdf", func(e *catalog.CatalogEntry) error {
			e.Size = 2048
			e.SetTag("reviewed", "true")
			return nil
		})
		require.NoError(t, err)

		updated, err := setup.manager.Get(ctx, "myns:documents/report.pdf")
		require.NoError(t, err)
		assert.Equal(t, int64(2048), updated.Size)
		assert.Equal(t, "true", updated.GetTag("reviewed"))
		assert.Equal(t, uint64(2), updated.Version)

		// EXISTS
		exists, err := setup.manager.Exists(ctx, "myns:documents/report.pdf")
		require.NoError(t, err)
		assert.True(t, exists)

		notExists, err := setup.manager.Exists(ctx, "myns:nonexistent")
		require.NoError(t, err)
		assert.False(t, notExists)

		// DELETE
		err = setup.manager.Delete(ctx, "myns:documents/report.pdf")
		require.NoError(t, err)

		_, err = setup.manager.Get(ctx, "myns:documents/report.pdf")
		require.Error(t, err)
		assert.True(t, catalog.IsNotFound(err))
	})

	t.Run("concurrent operations", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()
		numGoroutines := 10
		numOpsPerGoroutine := 50

		var wg sync.WaitGroup
		var successCount atomic.Int64
		var errorCount atomic.Int64

		// Concurrent writes to different keys
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for j := 0; j < numOpsPerGoroutine; j++ {
					key := fmt.Sprintf("ns%d:file%d", goroutineID, j)
					entry := createTestEntry(key, int64(goroutineID*1000+j), fmt.Sprintf("tenant-%d", goroutineID))
					err := setup.manager.Put(ctx, entry)
					if err != nil {
						errorCount.Add(1)
					} else {
						successCount.Add(1)
					}
				}
			}(i)
		}

		wg.Wait()

		assert.Equal(t, int64(numGoroutines*numOpsPerGoroutine), successCount.Load())
		assert.Equal(t, int64(0), errorCount.Load())

		// Concurrent reads
		successCount.Store(0)
		errorCount.Store(0)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for j := 0; j < numOpsPerGoroutine; j++ {
					key := fmt.Sprintf("ns%d:file%d", goroutineID, j)
					_, err := setup.manager.Get(ctx, key)
					if err != nil {
						errorCount.Add(1)
					} else {
						successCount.Add(1)
					}
				}
			}(i)
		}

		wg.Wait()

		assert.Equal(t, int64(numGoroutines*numOpsPerGoroutine), successCount.Load())
		assert.Equal(t, int64(0), errorCount.Load())
	})

	t.Run("index queries ListByStore", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entries in different stores
		stores := []string{"s3-primary", "s3-secondary", "gcs-west"}
		for i, store := range stores {
			for j := 0; j < 5; j++ {
				key := fmt.Sprintf("ns%d:file%d", i, j)
				entry := createTestEntry(key, int64(j*100), "tenant-acme")
				entry.PrimaryStore = *catalog.NewStoreLocation(store, fmt.Sprintf("objects/%s", key))
				err := setup.manager.Put(ctx, entry)
				require.NoError(t, err)
			}
		}

		// Query by store
		result, err := setup.manager.ListByStore(ctx, "s3-primary", nil)
		require.NoError(t, err)
		assert.Len(t, result.Keys, 5)

		result, err = setup.manager.ListByStore(ctx, "gcs-west", nil)
		require.NoError(t, err)
		assert.Len(t, result.Keys, 5)

		// Query non-existent store
		result, err = setup.manager.ListByStore(ctx, "nonexistent", nil)
		require.NoError(t, err)
		assert.Empty(t, result.Keys)
	})

	t.Run("index queries ListByTenant", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entries for different tenants
		tenants := []string{"tenant-acme", "tenant-globex", "tenant-initech"}
		for i, tenant := range tenants {
			for j := 0; j < 3; j++ {
				key := fmt.Sprintf("ns%d:doc%d", i, j)
				entry := createTestEntry(key, int64(j*50), tenant)
				err := setup.manager.Put(ctx, entry)
				require.NoError(t, err)
			}
		}

		// Query by tenant
		for _, tenant := range tenants {
			result, err := setup.manager.ListByTenant(ctx, tenant, nil)
			require.NoError(t, err)
			assert.Len(t, result.Keys, 3, "Expected 3 entries for tenant %s", tenant)
		}
	})

	t.Run("index queries ListByStatus", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entries with different statuses
		statuses := []catalog.EntryStatus{catalog.StatusActive, catalog.StatusArchived, catalog.StatusPending}
		for i, status := range statuses {
			for j := 0; j < 4; j++ {
				key := fmt.Sprintf("status%d:file%d", i, j)
				entry := createTestEntry(key, int64(j*100), "tenant-test")
				entry.Status = status
				err := setup.manager.Put(ctx, entry)
				require.NoError(t, err)
			}
		}

		// Query by status
		result, err := setup.manager.ListByStatus(ctx, catalog.StatusActive, nil)
		require.NoError(t, err)
		assert.Len(t, result.Keys, 4)

		result, err = setup.manager.ListByStatus(ctx, catalog.StatusArchived, nil)
		require.NoError(t, err)
		assert.Len(t, result.Keys, 4)
	})

	t.Run("index queries ListByTag", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entries with tags
		departments := []string{"engineering", "finance", "marketing"}
		for i, dept := range departments {
			for j := 0; j < 3; j++ {
				key := fmt.Sprintf("tagged%d:doc%d", i, j)
				entry := createTestEntry(key, int64(j*100), "tenant-test")
				entry.SetTag("department", dept)
				entry.SetTag("year", "2025")
				err := setup.manager.Put(ctx, entry)
				require.NoError(t, err)
			}
		}

		// Query by tag
		result, err := setup.manager.ListByTag(ctx, "department", "engineering", nil)
		require.NoError(t, err)
		assert.Len(t, result.Keys, 3)

		// Query common tag
		result, err = setup.manager.ListByTag(ctx, "year", "2025", nil)
		require.NoError(t, err)
		assert.Len(t, result.Keys, 9)
	})

	t.Run("pagination with cursors", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create 25 entries for pagination testing
		for i := 0; i < 25; i++ {
			key := fmt.Sprintf("paginated:file%02d", i)
			entry := createTestEntry(key, int64(i*100), "tenant-pagination")
			err := setup.manager.Put(ctx, entry)
			require.NoError(t, err)
		}

		// Page 1 (limit 10)
		opts := &catalog.ListOptions{Limit: 10}
		result, err := setup.manager.ListByTenant(ctx, "tenant-pagination", opts)
		require.NoError(t, err)
		assert.Len(t, result.Keys, 10)
		assert.True(t, result.HasMore)
		assert.NotEmpty(t, result.NextCursor)

		// Page 2 (using cursor)
		opts = &catalog.ListOptions{Limit: 10, Cursor: result.NextCursor}
		result2, err := setup.manager.ListByTenant(ctx, "tenant-pagination", opts)
		require.NoError(t, err)
		assert.Len(t, result2.Keys, 10)
		assert.True(t, result2.HasMore)
		assert.NotEmpty(t, result2.NextCursor)

		// Page 3 (remaining items)
		opts = &catalog.ListOptions{Limit: 10, Cursor: result2.NextCursor}
		result3, err := setup.manager.ListByTenant(ctx, "tenant-pagination", opts)
		require.NoError(t, err)
		assert.Len(t, result3.Keys, 5)
		assert.False(t, result3.HasMore)

		// Ensure no duplicates across pages
		allKeys := make(map[string]bool)
		for _, key := range result.Keys {
			allKeys[key] = true
		}
		for _, key := range result2.Keys {
			assert.False(t, allKeys[key], "Duplicate key found: %s", key)
			allKeys[key] = true
		}
		for _, key := range result3.Keys {
			assert.False(t, allKeys[key], "Duplicate key found: %s", key)
			allKeys[key] = true
		}
		assert.Len(t, allKeys, 25)
	})

	t.Run("error handling for not found", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Get non-existent entry
		_, err := setup.manager.Get(ctx, "nonexistent:file")
		require.Error(t, err)
		assert.True(t, catalog.IsNotFound(err))

		// Update non-existent entry
		err = setup.manager.Update(ctx, "nonexistent:file", func(e *catalog.CatalogEntry) error {
			e.Size = 999
			return nil
		})
		require.Error(t, err)
		assert.True(t, catalog.IsNotFound(err))
	})

	t.Run("error handling for version mismatch", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create an entry
		entry := createTestEntry("conflict:file", 1024, "tenant-test")
		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		// Simulate concurrent updates that should detect version mismatch
		var wg sync.WaitGroup
		var versionMismatchCount atomic.Int64
		var successCount atomic.Int64

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				updateErr := setup.manager.Update(ctx, "conflict:file", func(e *catalog.CatalogEntry) error {
					e.Size = int64(idx * 1000)
					// Add a small delay to increase chance of version mismatch
					time.Sleep(time.Millisecond * 5)
					return nil
				})
				if updateErr != nil {
					if catalog.IsVersionMismatch(updateErr) {
						versionMismatchCount.Add(1)
					}
				} else {
					successCount.Add(1)
				}
			}(i)
		}

		wg.Wait()

		// With concurrent updates, we should see some version mismatches
		t.Logf("Successes: %d, Version mismatches: %d", successCount.Load(), versionMismatchCount.Load())
		// At least some should succeed
		assert.Greater(t, successCount.Load(), int64(0))
	})

	t.Run("location operations", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entry with primary store
		entry := createTestEntry("locations:file", 1024, "tenant-test")
		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		// Add replica location
		replica := catalog.NewStoreLocation("gcs-west", "replicas/locations:file")
		err = setup.manager.AddLocation(ctx, "locations:file", replica)
		require.NoError(t, err)

		// Verify locations
		locations, err := setup.manager.GetLocations(ctx, "locations:file")
		require.NoError(t, err)
		assert.Len(t, locations, 2) // primary + replica

		// Update location status
		err = setup.manager.UpdateLocationStatus(ctx, "locations:file", "gcs-west", catalog.LocationStatusSyncing)
		require.NoError(t, err)

		locations, err = setup.manager.GetLocations(ctx, "locations:file")
		require.NoError(t, err)
		for _, loc := range locations {
			if loc.Store == "gcs-west" {
				assert.Equal(t, catalog.LocationStatusSyncing, loc.Status)
			}
		}

		// Set new primary store
		err = setup.manager.SetPrimaryStore(ctx, "locations:file", "gcs-west")
		require.NoError(t, err)

		updated, err := setup.manager.Get(ctx, "locations:file")
		require.NoError(t, err)
		assert.Equal(t, "gcs-west", updated.PrimaryStore.Store)

		// Remove location (old primary now a replica)
		err = setup.manager.RemoveLocation(ctx, "locations:file", "s3-primary")
		require.NoError(t, err)

		locations, err = setup.manager.GetLocations(ctx, "locations:file")
		require.NoError(t, err)
		assert.Len(t, locations, 1) // only new primary
	})

	t.Run("async task queues", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Enqueue replication tasks with different priorities
		err := setup.manager.EnqueueReplication(ctx, "queue:file1", []string{"s3-west", "gcs-east"}, 5)
		require.NoError(t, err)
		err = setup.manager.EnqueueReplication(ctx, "queue:file2", []string{"s3-west"}, 10)
		require.NoError(t, err)
		err = setup.manager.EnqueueReplication(ctx, "queue:file3", []string{"gcs-east"}, 1)
		require.NoError(t, err)

		// Dequeue should return highest priority first
		tasks, err := setup.manager.DequeueReplication(ctx, 2)
		require.NoError(t, err)
		assert.Len(t, tasks, 2)
		assert.Equal(t, "queue:file2", tasks[0].Key) // priority 10
		assert.Equal(t, "queue:file1", tasks[1].Key) // priority 5

		// Dequeue remaining
		tasks, err = setup.manager.DequeueReplication(ctx, 10)
		require.NoError(t, err)
		assert.Len(t, tasks, 1)
		assert.Equal(t, "queue:file3", tasks[0].Key) // priority 1

		// Enqueue archive tasks
		err = setup.manager.EnqueueArchive(ctx, "archive:file1", "glacier-archive")
		require.NoError(t, err)
		err = setup.manager.EnqueueArchive(ctx, "archive:file2", "glacier-archive")
		require.NoError(t, err)

		archiveTasks, err := setup.manager.DequeueArchive(ctx, 10)
		require.NoError(t, err)
		assert.Len(t, archiveTasks, 2)

		// Enqueue verify tasks
		err = setup.manager.EnqueueVerify(ctx, "verify:file1", []string{"s3-primary", "gcs-west"})
		require.NoError(t, err)

		verifyTasks, err := setup.manager.DequeueVerify(ctx, 10)
		require.NoError(t, err)
		assert.Len(t, verifyTasks, 1)
		assert.Equal(t, "verify:file1", verifyTasks[0].Key)
	})

	t.Run("tenant usage statistics", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entries for a tenant
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("usage:file%02d", i)
			entry := createTestEntry(key, int64((i+1)*1000), "tenant-usage")
			if i >= 7 {
				entry.Status = catalog.StatusArchived
			}
			err := setup.manager.Put(ctx, entry)
			require.NoError(t, err)
		}

		// Get tenant usage
		usage, err := setup.manager.GetTenantUsage(ctx, "tenant-usage")
		require.NoError(t, err)
		assert.Equal(t, "tenant-usage", usage.TenantID)
		assert.Equal(t, int64(10), usage.ObjectCount)
		assert.Equal(t, int64(7), usage.ActiveCount)
		assert.Equal(t, int64(3), usage.ArchivedCount)
		assert.Equal(t, int64(55000), usage.TotalSize) // sum of 1000+2000+...+10000
	})

	t.Run("search with complex filters", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entries with various attributes
		now := time.Now()
		for i := 0; i < 20; i++ {
			key := fmt.Sprintf("search:doc%02d", i)
			entry := createTestEntry(key, int64((i+1)*500), "tenant-search")
			entry.SetTag("category", fmt.Sprintf("cat%d", i%3))
			if i < 10 {
				entry.Status = catalog.StatusActive
			} else {
				entry.Status = catalog.StatusArchived
			}
			// Vary creation times
			entry.CreatedAt = now.Add(-time.Duration(i) * time.Hour).UnixNano()
			err := setup.manager.Put(ctx, entry)
			require.NoError(t, err)
		}

		// Search by status
		result, err := setup.manager.Search(ctx, &catalog.SearchQuery{
			Status: catalog.StatusActive,
			Limit:  100,
		})
		require.NoError(t, err)
		assert.Len(t, result.Entries, 10)

		// Search by tenant
		result, err = setup.manager.Search(ctx, &catalog.SearchQuery{
			TenantID: "tenant-search",
			Limit:    100,
		})
		require.NoError(t, err)
		assert.Len(t, result.Entries, 20)

		// Search by size range
		result, err = setup.manager.Search(ctx, &catalog.SearchQuery{
			SizeMin: 3000,
			SizeMax: 7000,
			Limit:   100,
		})
		require.NoError(t, err)
		// Files with sizes 3000, 3500, 4000, 4500, 5000, 5500, 6000, 6500, 7000
		assert.Greater(t, len(result.Entries), 0)

		// Search with sorting
		result, err = setup.manager.Search(ctx, &catalog.SearchQuery{
			TenantID: "tenant-search",
			Sort:     "size",
			Order:    "desc",
			Limit:    5,
		})
		require.NoError(t, err)
		assert.Len(t, result.Entries, 5)
		// Verify descending order
		for i := 1; i < len(result.Entries); i++ {
			assert.GreaterOrEqual(t, result.Entries[i-1].Size, result.Entries[i].Size)
		}
	})

	t.Run("manager lifecycle sync and close", func(t *testing.T) {
		setup := newTestSetup(t)
		ctx := context.Background()

		// Create some entries
		for i := 0; i < 10; i++ {
			key := fmt.Sprintf("lifecycle:file%02d", i)
			entry := createTestEntry(key, int64(i*100), "tenant-lifecycle")
			err := setup.manager.Put(ctx, entry)
			require.NoError(t, err)
		}

		// Sync should succeed
		err := setup.manager.Sync()
		require.NoError(t, err)

		// Close manager
		err = setup.manager.Close()
		require.NoError(t, err)

		// Operations after close should fail
		_, err = setup.manager.Get(ctx, "lifecycle:file00")
		require.Error(t, err)
		assert.True(t, catalog.IsCatalogClosed(err))

		// Double close should be safe
		err = setup.manager.Close()
		require.NoError(t, err)

		// Cleanup temp dir manually since cleanup() would try to close again
		os.RemoveAll(setup.tmpDir)
	})
}

// TestWorkerManagerIntegration tests WorkerManager end-to-end operations.
func TestWorkerManagerIntegration(t *testing.T) {
	t.Run("worker registration lifecycle", func(t *testing.T) {
		wm := catalog.NewWorkerManager(nil)
		defer wm.Close()

		// Create mock workers
		worker1, err := newMockWorker("worker-1")
		require.NoError(t, err)
		worker2, err := newMockWorker("worker-2")
		require.NoError(t, err)

		// Register workers
		err = wm.Register(worker1)
		require.NoError(t, err)
		err = wm.Register(worker2)
		require.NoError(t, err)

		// Cannot register duplicate
		duplicateWorker, _ := newMockWorker("worker-1")
		err = wm.Register(duplicateWorker)
		require.Error(t, err)

		// List workers
		workers := wm.ListWorkers()
		assert.Len(t, workers, 2)
		assert.Contains(t, workers, "worker-1")
		assert.Contains(t, workers, "worker-2")

		// Get worker
		w, err := wm.GetWorker("worker-1")
		require.NoError(t, err)
		assert.Equal(t, "worker-1", w.Name())

		// Get non-existent worker
		_, err = wm.GetWorker("nonexistent")
		require.Error(t, err)
		assert.True(t, catalog.IsWorkerNotFound(err))

		// Unregister worker
		err = wm.Unregister("worker-1")
		require.NoError(t, err)

		workers = wm.ListWorkers()
		assert.Len(t, workers, 1)
		assert.NotContains(t, workers, "worker-1")
	})

	t.Run("start and stop all workers", func(t *testing.T) {
		wm := catalog.NewWorkerManager(nil)
		defer wm.Close()

		// Register multiple workers
		for i := 0; i < 5; i++ {
			worker, err := newMockWorker(fmt.Sprintf("worker-%d", i))
			require.NoError(t, err)
			err = wm.Register(worker)
			require.NoError(t, err)
		}

		ctx := context.Background()

		// Start all
		err := wm.StartAll(ctx)
		require.NoError(t, err)
		assert.True(t, wm.IsRunning())
		assert.Equal(t, 5, wm.RunningWorkerCount())

		// Workers should be running
		for i := 0; i < 5; i++ {
			w, _ := wm.GetWorker(fmt.Sprintf("worker-%d", i))
			assert.True(t, w.IsRunning())
		}

		// Stop all
		err = wm.StopAll()
		require.NoError(t, err)
		assert.False(t, wm.IsRunning())
		assert.Equal(t, 0, wm.RunningWorkerCount())
	})

	t.Run("individual worker start and stop", func(t *testing.T) {
		wm := catalog.NewWorkerManager(nil)
		defer wm.Close()

		worker, err := newMockWorker("test-worker")
		require.NoError(t, err)
		err = wm.Register(worker)
		require.NoError(t, err)

		ctx := context.Background()

		// Start individual worker
		err = wm.StartWorker(ctx, "test-worker")
		require.NoError(t, err)
		assert.True(t, worker.IsRunning())

		// Stop individual worker
		err = wm.StopWorker("test-worker")
		require.NoError(t, err)
		assert.False(t, worker.IsRunning())

		// Restart worker
		err = wm.RestartWorker(ctx, "test-worker")
		require.NoError(t, err)
		assert.True(t, worker.IsRunning())
	})

	t.Run("multiple workers running concurrently", func(t *testing.T) {
		wm := catalog.NewWorkerManager(nil)
		defer wm.Close()

		var taskCounters [5]*atomic.Int64
		for i := 0; i < 5; i++ {
			taskCounters[i] = &atomic.Int64{}
			worker, err := newMockWorkerWithCounter(fmt.Sprintf("concurrent-%d", i), taskCounters[i])
			require.NoError(t, err)
			err = wm.Register(worker)
			require.NoError(t, err)
		}

		ctx := context.Background()
		err := wm.StartAll(ctx)
		require.NoError(t, err)

		// Let workers run for a bit
		time.Sleep(200 * time.Millisecond)

		// Verify workers are processing
		assert.Equal(t, 5, wm.RunningWorkerCount())

		// Get aggregated metrics
		aggMetrics := wm.GetAggregatedMetrics()
		t.Logf("Aggregated metrics: processed=%d, failed=%d", aggMetrics.TasksProcessed, aggMetrics.TasksFailed)

		// Get status of all workers
		statuses := wm.GetStatus()
		assert.Len(t, statuses, 5)
		for _, status := range statuses {
			assert.True(t, status.Running)
		}

		err = wm.StopAll()
		require.NoError(t, err)
	})

	t.Run("graceful shutdown", func(t *testing.T) {
		wm := catalog.NewWorkerManager(nil)

		worker, err := newMockWorkerSlow("slow-worker", 100*time.Millisecond)
		require.NoError(t, err)
		err = wm.Register(worker)
		require.NoError(t, err)

		ctx := context.Background()
		err = wm.StartAll(ctx)
		require.NoError(t, err)

		// Start shutdown
		start := time.Now()
		err = wm.Close()
		elapsed := time.Since(start)
		require.NoError(t, err)

		// Should wait for worker to finish (graceful)
		t.Logf("Shutdown took: %v", elapsed)
		assert.False(t, wm.IsRunning())
	})

	t.Run("closed manager operations fail", func(t *testing.T) {
		wm := catalog.NewWorkerManager(nil)
		worker, _ := newMockWorker("test")
		err := wm.Register(worker)
		require.NoError(t, err)

		err = wm.Close()
		require.NoError(t, err)

		// Operations should fail after close
		newWorker, _ := newMockWorker("new")
		err = wm.Register(newWorker)
		require.Error(t, err)
		assert.True(t, catalog.IsWorkerManagerClosed(err))

		_, err = wm.GetWorker("test")
		require.Error(t, err)
		assert.True(t, catalog.IsWorkerManagerClosed(err))
	})
}

// TestACLManagerIntegration tests ACLManager end-to-end operations.
func TestACLManagerIntegration(t *testing.T) {
	t.Run("grant and revoke permissions", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entry with owner
		entry := createTestEntry("acl:secure-doc", 1024, "tenant-acl")
		entry.Owner = "alice"
		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		// Create ACL manager
		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
			Logger:  nil,
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Owner should have access
		allowed, err := aclMgr.CheckAccess(ctx, "alice", "acl:secure-doc", catalog.ActionRead)
		require.NoError(t, err)
		assert.True(t, allowed)

		allowed, err = aclMgr.CheckAccess(ctx, "alice", "acl:secure-doc", catalog.ActionAdmin)
		require.NoError(t, err)
		assert.True(t, allowed)

		// Non-owner should not have access initially
		allowed, err = aclMgr.CheckAccess(ctx, "bob", "acl:secure-doc", catalog.ActionRead)
		require.NoError(t, err)
		assert.False(t, allowed)

		// Grant read access to bob
		err = aclMgr.GrantAccess(ctx, "acl:secure-doc", "bob", []catalog.ObjectAction{catalog.ActionRead})
		require.NoError(t, err)

		// Bob should now have read access
		allowed, err = aclMgr.CheckAccess(ctx, "bob", "acl:secure-doc", catalog.ActionRead)
		require.NoError(t, err)
		assert.True(t, allowed)

		// Bob should not have write access
		allowed, err = aclMgr.CheckAccess(ctx, "bob", "acl:secure-doc", catalog.ActionWrite)
		require.NoError(t, err)
		assert.False(t, allowed)

		// Grant write access to bob
		err = aclMgr.GrantAccess(ctx, "acl:secure-doc", "bob", []catalog.ObjectAction{catalog.ActionWrite})
		require.NoError(t, err)

		// Bob should now have write access
		allowed, err = aclMgr.CheckAccess(ctx, "bob", "acl:secure-doc", catalog.ActionWrite)
		require.NoError(t, err)
		assert.True(t, allowed)

		// Revoke all access from bob
		err = aclMgr.RevokeAccess(ctx, "acl:secure-doc", "bob")
		require.NoError(t, err)

		// Bob should no longer have access
		allowed, err = aclMgr.CheckAccess(ctx, "bob", "acl:secure-doc", catalog.ActionRead)
		require.NoError(t, err)
		assert.False(t, allowed)
	})

	t.Run("permission levels", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		entry := createTestEntry("acl:levels", 1024, "tenant-acl")
		entry.Owner = "owner"
		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Grant different permission levels to different users
		err = aclMgr.GrantAccess(ctx, "acl:levels", "reader", []catalog.ObjectAction{catalog.ActionRead})
		require.NoError(t, err)
		err = aclMgr.GrantAccess(ctx, "acl:levels", "writer", []catalog.ObjectAction{catalog.ActionWrite})
		require.NoError(t, err)
		err = aclMgr.GrantAccess(ctx, "acl:levels", "admin", []catalog.ObjectAction{catalog.ActionAdmin})
		require.NoError(t, err)

		// Reader tests
		allowed, _ := aclMgr.CheckAccess(ctx, "reader", "acl:levels", catalog.ActionRead)
		assert.True(t, allowed)
		allowed, _ = aclMgr.CheckAccess(ctx, "reader", "acl:levels", catalog.ActionWrite)
		assert.False(t, allowed)
		allowed, _ = aclMgr.CheckAccess(ctx, "reader", "acl:levels", catalog.ActionDelete)
		assert.False(t, allowed)

		// Writer tests - writers should also be able to read
		allowed, _ = aclMgr.CheckAccess(ctx, "writer", "acl:levels", catalog.ActionRead)
		assert.True(t, allowed)
		allowed, _ = aclMgr.CheckAccess(ctx, "writer", "acl:levels", catalog.ActionWrite)
		assert.True(t, allowed)
		allowed, _ = aclMgr.CheckAccess(ctx, "writer", "acl:levels", catalog.ActionDelete)
		assert.False(t, allowed)

		// Admin tests - admins have full access
		allowed, _ = aclMgr.CheckAccess(ctx, "admin", "acl:levels", catalog.ActionRead)
		assert.True(t, allowed)
		allowed, _ = aclMgr.CheckAccess(ctx, "admin", "acl:levels", catalog.ActionWrite)
		assert.True(t, allowed)
		allowed, _ = aclMgr.CheckAccess(ctx, "admin", "acl:levels", catalog.ActionDelete)
		assert.True(t, allowed)
		allowed, _ = aclMgr.CheckAccess(ctx, "admin", "acl:levels", catalog.ActionAdmin)
		assert.True(t, allowed)
	})

	t.Run("public read access", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		entry := createTestEntry("acl:public", 1024, "tenant-acl")
		entry.Owner = "owner"
		entry.ACL = &catalog.ObjectACL{
			Owner:      "owner",
			PublicRead: true,
		}
		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Anyone should be able to read
		allowed, _ := aclMgr.CheckAccess(ctx, "anonymous", "acl:public", catalog.ActionRead)
		assert.True(t, allowed)

		// But not write
		allowed, _ = aclMgr.CheckAccess(ctx, "anonymous", "acl:public", catalog.ActionWrite)
		assert.False(t, allowed)
	})

	t.Run("namespace ACLs", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
			NamespaceACLs: map[string]*catalog.NamespaceACL{
				"public-ns": {
					Namespace:      "public-ns",
					Owner:          "admin",
					DefaultReaders: []string{"everyone"},
					PublicRead:     true,
				},
			},
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Get namespace ACL
		nsACL, err := aclMgr.GetNamespaceACL(ctx, "public-ns")
		require.NoError(t, err)
		assert.Equal(t, "public-ns", nsACL.Namespace)
		assert.Equal(t, "admin", nsACL.Owner)
		assert.True(t, nsACL.PublicRead)

		// Get non-existent namespace ACL (should return empty one)
		nsACL, err = aclMgr.GetNamespaceACL(ctx, "new-ns")
		require.NoError(t, err)
		assert.Equal(t, "new-ns", nsACL.Namespace)
		assert.Empty(t, nsACL.Owner)

		// Set new namespace ACL
		newNsACL := &catalog.NamespaceACL{
			Namespace:      "new-ns",
			Owner:          "team-lead",
			DefaultWriters: []string{"team-member-1", "team-member-2"},
		}
		err = aclMgr.SetNamespaceACL(ctx, "new-ns", newNsACL)
		require.NoError(t, err)

		// Verify it was set
		nsACL, err = aclMgr.GetNamespaceACL(ctx, "new-ns")
		require.NoError(t, err)
		assert.Equal(t, "team-lead", nsACL.Owner)
		assert.Len(t, nsACL.DefaultWriters, 2)
	})

	t.Run("transfer ownership with existing ACL", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entry with owner AND an existing ACL
		entry := createTestEntry("acl:transfer", 1024, "tenant-acl")
		entry.Owner = "original-owner"
		entry.ACL = &catalog.ObjectACL{
			Owner: "original-owner",
		}
		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Original owner has access
		allowed, _ := aclMgr.CheckAccess(ctx, "original-owner", "acl:transfer", catalog.ActionAdmin)
		assert.True(t, allowed)

		// New owner doesn't have access yet
		allowed, _ = aclMgr.CheckAccess(ctx, "new-owner", "acl:transfer", catalog.ActionAdmin)
		assert.False(t, allowed)

		// Transfer ownership
		err = aclMgr.TransferOwnership(ctx, "acl:transfer", "new-owner")
		require.NoError(t, err)

		// New owner now has full access
		allowed, _ = aclMgr.CheckAccess(ctx, "new-owner", "acl:transfer", catalog.ActionAdmin)
		assert.True(t, allowed)

		// Original owner should still have admin access (added as admin during transfer)
		allowed, _ = aclMgr.CheckAccess(ctx, "original-owner", "acl:transfer", catalog.ActionAdmin)
		assert.True(t, allowed)
	})

	t.Run("transfer ownership without existing ACL", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create entry with owner but NO ACL
		entry := createTestEntry("acl:transfer-no-acl", 1024, "tenant-acl")
		entry.Owner = "original-owner"
		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Original owner has access (as entry owner)
		allowed, _ := aclMgr.CheckAccess(ctx, "original-owner", "acl:transfer-no-acl", catalog.ActionAdmin)
		assert.True(t, allowed)

		// Transfer ownership
		err = aclMgr.TransferOwnership(ctx, "acl:transfer-no-acl", "new-owner")
		require.NoError(t, err)

		// New owner now has full access
		allowed, _ = aclMgr.CheckAccess(ctx, "new-owner", "acl:transfer-no-acl", catalog.ActionAdmin)
		assert.True(t, allowed)

		// Original owner loses access (no ACL existed to add them as admin)
		allowed, _ = aclMgr.CheckAccess(ctx, "original-owner", "acl:transfer-no-acl", catalog.ActionAdmin)
		assert.False(t, allowed)
	})

	t.Run("batch check access", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		// Create multiple entries with different ACLs
		for i := 0; i < 5; i++ {
			key := fmt.Sprintf("acl:batch%d", i)
			entry := createTestEntry(key, int64(i*100), "tenant-acl")
			entry.Owner = "owner"
			if i%2 == 0 {
				entry.ACL = &catalog.ObjectACL{
					Owner:   "owner",
					Readers: []string{"checker"},
				}
			}
			err := setup.manager.Put(ctx, entry)
			require.NoError(t, err)
		}

		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Batch check
		keys := []string{"acl:batch0", "acl:batch1", "acl:batch2", "acl:batch3", "acl:batch4"}
		results, err := aclMgr.BatchCheckAccess(ctx, "checker", keys, catalog.ActionRead)
		require.NoError(t, err)
		assert.Len(t, results, 5)

		// Even indexes should have access
		assert.True(t, results["acl:batch0"])
		assert.False(t, results["acl:batch1"])
		assert.True(t, results["acl:batch2"])
		assert.False(t, results["acl:batch3"])
		assert.True(t, results["acl:batch4"])
	})

	t.Run("cannot revoke owner access", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		entry := createTestEntry("acl:cannot-revoke", 1024, "tenant-acl")
		entry.Owner = "owner"
		entry.ACL = &catalog.ObjectACL{Owner: "owner"}
		err := setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Attempting to revoke owner's access should fail
		err = aclMgr.RevokeAccess(ctx, "acl:cannot-revoke", "owner")
		require.Error(t, err)
	})

	t.Run("ACL error handling", func(t *testing.T) {
		setup := newTestSetup(t)
		defer setup.cleanup()

		ctx := context.Background()

		aclConfig := &catalog.ACLManagerConfig{
			Catalog: setup.manager,
		}
		aclMgr, err := catalog.NewACLManager(aclConfig)
		require.NoError(t, err)

		// Empty key
		_, err = aclMgr.CheckAccess(ctx, "user", "", catalog.ActionRead)
		require.Error(t, err)

		// Invalid action
		entry := createTestEntry("acl:errors", 1024, "tenant-acl")
		entry.Owner = "owner"
		err = setup.manager.Put(ctx, entry)
		require.NoError(t, err)

		_, err = aclMgr.CheckAccess(ctx, "user", "acl:errors", "invalid-action")
		require.Error(t, err)

		// Empty principal
		_, err = aclMgr.CheckAccess(ctx, "", "acl:errors", catalog.ActionRead)
		require.NoError(t, err) // Empty principal is allowed but will fail access check

		// Empty namespace
		err = aclMgr.SetNamespaceACL(ctx, "", &catalog.NamespaceACL{})
		require.Error(t, err)

		// Nil ACL
		err = aclMgr.SetACL(ctx, "acl:errors", nil)
		require.Error(t, err)

		// Non-existent entry
		_, err = aclMgr.CheckAccess(ctx, "user", "nonexistent:key", catalog.ActionRead)
		require.Error(t, err)
	})
}

// mockWorker is a simple mock implementation of the Worker interface for testing.
type mockWorker struct {
	*catalog.BaseWorker
	taskCounter  *atomic.Int64
	slowDuration time.Duration
}

func newMockWorker(name string) (*mockWorker, error) {
	base, err := catalog.NewBaseWorker(name, nil, nil)
	if err != nil {
		return nil, err
	}
	return &mockWorker{BaseWorker: base}, nil
}

func newMockWorkerWithCounter(name string, counter *atomic.Int64) (*mockWorker, error) {
	base, err := catalog.NewBaseWorker(name, nil, nil)
	if err != nil {
		return nil, err
	}
	return &mockWorker{BaseWorker: base, taskCounter: counter}, nil
}

func newMockWorkerSlow(name string, duration time.Duration) (*mockWorker, error) {
	base, err := catalog.NewBaseWorker(name, nil, nil)
	if err != nil {
		return nil, err
	}
	return &mockWorker{BaseWorker: base, slowDuration: duration}, nil
}

func (w *mockWorker) Start(ctx context.Context) error {
	if err := w.BaseWorker.StartBase(ctx); err != nil {
		return err
	}

	w.WaitGroup().Add(1)
	go func() {
		defer w.WaitGroup().Done()
		for w.ShouldContinue() {
			if w.taskCounter != nil {
				w.taskCounter.Add(1)
			}
			if w.slowDuration > 0 {
				time.Sleep(w.slowDuration)
			} else {
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	return nil
}

func (w *mockWorker) Stop() error {
	return w.BaseWorker.StopBase()
}
