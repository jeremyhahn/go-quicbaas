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

package object

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Test Setup - End-to-End Object Storage
// =============================================================================

// e2eTestSetup holds all components for end-to-end object storage testing.
type e2eTestSetup struct {
	tmpDir         string
	catalogStorage catalog.PebbleStorage
	catalogManager catalog.CatalogManager
	backendManager catalog.BackendManager
	configResolver config.ObjectStoreConfigResolver
	workerManager  *catalog.WorkerManager
	objectStorage  service.ObjectStorageService
	logger         *logger.Logger
	logAdapter     interfaces.Logger
	cleanup        func()
}

// newE2ETestSetup creates a complete end-to-end test setup with real backends.
func newE2ETestSetup(t *testing.T) *e2eTestSetup {
	t.Helper()

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "objstore-e2e-*")
	require.NoError(t, err)

	// Create logger
	logCfg := logger.LogConfig{
		Level:     "warn",
		Format:    "json",
		Output:    "stdout",
		Component: "objstore-e2e-test",
	}
	log, err := logger.New(logCfg)
	require.NoError(t, err)
	logAdapter := logger.NewAdapter(log)

	// Create catalog storage with PebbleDB
	catalogPath := filepath.Join(tmpDir, "catalog")
	catalogStorage, err := catalog.NewPebbleStorage(&catalog.PebbleStorageConfig{
		Path:        catalogPath,
		CacheSize:   32 * 1024 * 1024,
		SyncWrites:  true,
		Compression: true,
	})
	require.NoError(t, err)

	// Create catalog manager
	catalogManager, err := catalog.NewCatalogManager(&catalog.ManagerConfig{
		Storage: catalogStorage,
		Logger:  logAdapter,
	})
	require.NoError(t, err)

	// Create backend manager with multiple memory backends
	backendManager, err := catalog.NewBackendManager(catalog.DefaultBackendManagerConfig())
	require.NoError(t, err)

	// Register multiple backends for multi-backend testing
	// Primary (hot tier)
	err = backendManager.RegisterBackend("primary", &catalog.BackendConfig{
		Type:    "memory",
		Enabled: true,
		Default: true,
		Tier:    catalog.TierHot,
		Region:  "us-east-1",
	})
	require.NoError(t, err)

	// Replica (warm tier)
	err = backendManager.RegisterBackend("replica-west", &catalog.BackendConfig{
		Type:    "memory",
		Enabled: true,
		Default: false,
		Tier:    catalog.TierWarm,
		Region:  "us-west-2",
	})
	require.NoError(t, err)

	// Archive (cold tier)
	err = backendManager.RegisterBackend("archive", &catalog.BackendConfig{
		Type:    "memory",
		Enabled: true,
		Default: false,
		Tier:    catalog.TierCold,
		Region:  "us-east-1",
	})
	require.NoError(t, err)

	// Secondary primary for tenant isolation testing
	err = backendManager.RegisterBackend("tenant-premium", &catalog.BackendConfig{
		Type:    "memory",
		Enabled: true,
		Default: false,
		Tier:    catalog.TierHot,
		Region:  "eu-west-1",
	})
	require.NoError(t, err)

	// Create config with tenant-specific backends
	fullConfig := &config.ObjectStoreFullConfig{
		Defaults: config.ObjectStoreDefaults{
			PrimaryBackend: "primary",
			ValueThreshold: 1024,
			Compression:    "none",
			Replication: config.ObjectStoreReplication{
				Enabled:      true,
				MinReplicas:  1,
				TargetStores: []string{"replica-west"},
				Mode:         "async",
				Priority:     5, // Required: must be between 1-10
			},
		},
		Tenants: map[string]*config.ObjectStoreTenantConfig{
			"tenant-premium": {
				PrimaryBackend:  "tenant-premium",
				AllowedBackends: []string{"tenant-premium", "replica-west"},
				Quotas: config.ObjectStoreQuotas{
					MaxStorageBytes: 1024 * 1024 * 1024, // 1GB
					MaxObjectCount:  10000,
					MaxObjectSize:   100 * 1024 * 1024, // 100MB
				},
			},
			"tenant-basic": {
				PrimaryBackend:  "primary",
				AllowedBackends: []string{"primary"},
				Quotas: config.ObjectStoreQuotas{
					MaxStorageBytes: 100 * 1024 * 1024, // 100MB
					MaxObjectCount:  1000,
					MaxObjectSize:   10 * 1024 * 1024, // 10MB
				},
			},
		},
	}

	// Create config resolver
	configResolver := config.NewObjectStoreResolver(fullConfig, nil)

	// Create worker manager
	workerManager := catalog.NewWorkerManager(logAdapter)

	// Create object storage service - the main component under test
	objectStorage, err := service.NewObjectStorageService(
		catalogManager,
		backendManager,
		configResolver,
		workerManager,
		logAdapter,
	)
	require.NoError(t, err)

	cleanup := func() {
		// Note: ObjectStorageService doesn't have a Close method,
		// cleanup is handled by underlying components
		if workerManager != nil {
			workerManager.Close()
		}
		if backendManager != nil {
			backendManager.Close()
		}
		if catalogManager != nil {
			catalogManager.Close()
		}
		os.RemoveAll(tmpDir)
	}

	return &e2eTestSetup{
		tmpDir:         tmpDir,
		catalogStorage: catalogStorage,
		catalogManager: catalogManager,
		backendManager: backendManager,
		configResolver: configResolver,
		workerManager:  workerManager,
		objectStorage:  objectStorage,
		logger:         log,
		logAdapter:     logAdapter,
		cleanup:        cleanup,
	}
}

// generateRandomData generates random test data.
func generateRandomData(size int) []byte {
	data := make([]byte, size)
	rand.Read(data)
	return data
}

// =============================================================================
// End-to-End Object Storage Tests
// =============================================================================

func TestE2E_ObjectStoragePutAndGet(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("put and get object - verifies data stored in backend", func(t *testing.T) {
		// Generate test data
		testData := generateRandomData(4096)
		expectedChecksum := sha256.Sum256(testData)

		// Put object
		putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:         "test-ns:docs/report.pdf",
			Value:       testData,
			ContentType: "application/pdf",
			Tags:        map[string]string{"department": "engineering"},
		})
		require.NoError(t, err)
		require.NotNil(t, putResp)
		assert.Equal(t, "test-ns:docs/report.pdf", putResp.Key)
		assert.Equal(t, int64(4096), putResp.Size)
		assert.Equal(t, "primary", putResp.Backend)

		// Get object - verifies data is retrieved from backend
		getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
			Key: "test-ns:docs/report.pdf",
		})
		require.NoError(t, err)
		require.NotNil(t, getResp)

		// Verify data integrity
		assert.True(t, bytes.Equal(testData, getResp.Value), "retrieved data should match original")
		assert.Equal(t, "application/pdf", getResp.ContentType)
		assert.True(t, bytes.Equal(expectedChecksum[:], getResp.Checksum), "checksum should match")

		// Verify catalog entry exists with correct location
		entry, err := setup.catalogManager.Get(ctx, "test-ns:docs/report.pdf")
		require.NoError(t, err)
		assert.Equal(t, "primary", entry.PrimaryStore.Store)
		assert.Equal(t, catalog.LocationStatusActive, entry.PrimaryStore.Status)
		assert.NotEmpty(t, entry.PrimaryStore.ObjectRef)
	})

	t.Run("put object with explicit checksum verification", func(t *testing.T) {
		testData := []byte("Hello, World! This is a test document.")
		checksum := sha256.Sum256(testData)

		// Put with explicit checksum
		putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:         "checksum-ns:verified/doc.txt",
			Value:       testData,
			ContentType: "text/plain",
			Checksum:    checksum[:],
		})
		require.NoError(t, err)
		assert.Equal(t, int64(len(testData)), putResp.Size)

		// Retrieve and verify
		getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
			Key: "checksum-ns:verified/doc.txt",
		})
		require.NoError(t, err)
		assert.True(t, bytes.Equal(testData, getResp.Value))
		assert.True(t, bytes.Equal(checksum[:], getResp.Checksum))
	})

	t.Run("put with wrong checksum fails", func(t *testing.T) {
		testData := []byte("Test data")
		wrongChecksum := sha256.Sum256([]byte("wrong data"))

		_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:      "fail-ns:wrong-checksum.txt",
			Value:    testData,
			Checksum: wrongChecksum[:],
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "checksum")
	})
}

func TestE2E_CatalogTracksBackendLocations(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("catalog correctly tracks primary backend location", func(t *testing.T) {
		testData := generateRandomData(2048)

		// Put to default (primary) backend
		putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:   "catalog-test:primary/file1.bin",
			Value: testData,
		})
		require.NoError(t, err)
		assert.Equal(t, "primary", putResp.Backend)

		// Verify catalog entry
		entry, err := setup.catalogManager.Get(ctx, "catalog-test:primary/file1.bin")
		require.NoError(t, err)

		// Verify primary store tracking
		assert.Equal(t, "primary", entry.PrimaryStore.Store)
		assert.Equal(t, catalog.LocationStatusActive, entry.PrimaryStore.Status)
		assert.NotEmpty(t, entry.PrimaryStore.ObjectRef)
		assert.Equal(t, int64(2048), entry.PrimaryStore.StoredSize)
		assert.True(t, entry.PrimaryStore.SyncedAt > 0)
	})

	t.Run("catalog tracks tenant-specific backend", func(t *testing.T) {
		testData := generateRandomData(1024)

		// Put for premium tenant - should use tenant-premium backend
		putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:      "premium:data/important.bin",
			Value:    testData,
			TenantID: "tenant-premium",
		})
		require.NoError(t, err)
		assert.Equal(t, "tenant-premium", putResp.Backend)

		// Verify catalog entry tracks correct backend
		entry, err := setup.catalogManager.Get(ctx, "premium:data/important.bin")
		require.NoError(t, err)
		assert.Equal(t, "tenant-premium", entry.PrimaryStore.Store)
		assert.Equal(t, "tenant-premium", entry.TenantID)
	})

	t.Run("put to explicit backend overrides default", func(t *testing.T) {
		testData := generateRandomData(512)

		// Put to explicit archive backend
		putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:           "explicit:archive/old-file.bin",
			Value:         testData,
			TargetBackend: "archive",
		})
		require.NoError(t, err)
		assert.Equal(t, "archive", putResp.Backend)

		// Verify catalog
		entry, err := setup.catalogManager.Get(ctx, "explicit:archive/old-file.bin")
		require.NoError(t, err)
		assert.Equal(t, "archive", entry.PrimaryStore.Store)
	})
}

func TestE2E_MultipleBackendsAndRetrieval(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("objects stored in different backends are retrieved correctly", func(t *testing.T) {
		// Store objects in different backends
		objects := []struct {
			key     string
			backend string
			data    []byte
		}{
			{"multi:primary/file1.txt", "primary", []byte("Primary backend data")},
			{"multi:archive/file2.txt", "archive", []byte("Archive backend data")},
			{"multi:replica/file3.txt", "replica-west", []byte("Replica backend data")},
		}

		// Put objects to different backends
		for _, obj := range objects {
			putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
				Key:           obj.key,
				Value:         obj.data,
				TargetBackend: obj.backend,
			})
			require.NoError(t, err)
			assert.Equal(t, obj.backend, putResp.Backend)
		}

		// Retrieve each object and verify data
		for _, obj := range objects {
			getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
				Key: obj.key,
			})
			require.NoError(t, err)
			assert.True(t, bytes.Equal(obj.data, getResp.Value),
				"data mismatch for key %s", obj.key)

			// Verify catalog tracks correct backend
			entry, err := setup.catalogManager.Get(ctx, obj.key)
			require.NoError(t, err)
			assert.Equal(t, obj.backend, entry.PrimaryStore.Store)
		}
	})

	t.Run("backend data persists across retrievals", func(t *testing.T) {
		testData := generateRandomData(8192)
		key := "persist:data/large-file.bin"

		// Put object
		_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:   key,
			Value: testData,
		})
		require.NoError(t, err)

		// Retrieve multiple times
		for i := 0; i < 5; i++ {
			getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
				Key: key,
			})
			require.NoError(t, err)
			assert.True(t, bytes.Equal(testData, getResp.Value),
				"data should be consistent on retrieval %d", i+1)
		}
	})
}

func TestE2E_TenantIsolation(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("tenant data is stored in tenant-specific backend", func(t *testing.T) {
		premiumData := []byte("Premium tenant confidential data")
		basicData := []byte("Basic tenant data")

		// Premium tenant
		premiumResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:      "premium-data:secrets/api-keys.txt",
			Value:    premiumData,
			TenantID: "tenant-premium",
		})
		require.NoError(t, err)
		assert.Equal(t, "tenant-premium", premiumResp.Backend)

		// Basic tenant
		basicResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:      "basic-data:files/readme.txt",
			Value:    basicData,
			TenantID: "tenant-basic",
		})
		require.NoError(t, err)
		assert.Equal(t, "primary", basicResp.Backend) // basic uses primary

		// Verify each tenant's data is in correct backend
		premiumEntry, _ := setup.catalogManager.Get(ctx, "premium-data:secrets/api-keys.txt")
		assert.Equal(t, "tenant-premium", premiumEntry.PrimaryStore.Store)
		assert.Equal(t, "tenant-premium", premiumEntry.TenantID)

		basicEntry, _ := setup.catalogManager.Get(ctx, "basic-data:files/readme.txt")
		assert.Equal(t, "primary", basicEntry.PrimaryStore.Store)
		assert.Equal(t, "tenant-basic", basicEntry.TenantID)
	})

	t.Run("cross-tenant access is denied", func(t *testing.T) {
		testData := []byte("Tenant A's private data")

		// Put as tenant-premium
		_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:      "private:tenant-a/secret.txt",
			Value:    testData,
			TenantID: "tenant-premium",
		})
		require.NoError(t, err)

		// Try to get as different tenant
		_, err = setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
			Key:      "private:tenant-a/secret.txt",
			TenantID: "tenant-basic", // Different tenant
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cross-tenant")
	})
}

func TestE2E_DeleteObject(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("delete removes object from backend and catalog", func(t *testing.T) {
		testData := []byte("Data to be deleted")
		key := "delete-test:temp/file.txt"

		// Create object
		_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:   key,
			Value: testData,
		})
		require.NoError(t, err)

		// Verify it exists
		getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{Key: key})
		require.NoError(t, err)
		assert.NotNil(t, getResp)

		// Delete it
		_, err = setup.objectStorage.DeleteObject(ctx, &service.DeleteObjectRequest{Key: key})
		require.NoError(t, err)

		// Verify it's gone
		_, err = setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{Key: key})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")

		// Verify catalog entry is removed or marked deleted
		entry, err := setup.catalogManager.Get(ctx, key)
		if err == nil {
			// If entry still exists, it should be marked as deleted
			assert.Equal(t, catalog.StatusDeleted, entry.Status)
		}
	})
}

func TestE2E_ListObjects(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("list objects by prefix", func(t *testing.T) {
		// Create multiple objects with a common prefix
		for i := 0; i < 10; i++ {
			_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
				Key:   fmt.Sprintf("list-test:documents/doc%02d.txt", i),
				Value: []byte(fmt.Sprintf("Document %d", i)),
			})
			require.NoError(t, err)
		}

		// Create objects with different prefix
		for i := 0; i < 5; i++ {
			_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
				Key:   fmt.Sprintf("list-test:images/img%02d.png", i),
				Value: []byte(fmt.Sprintf("Image %d", i)),
			})
			require.NoError(t, err)
		}

		// List documents only
		listResp, err := setup.objectStorage.ListObjects(ctx, &service.ListObjectsRequest{
			Prefix: "list-test:documents/",
			Limit:  100,
		})
		require.NoError(t, err)
		assert.Len(t, listResp.Objects, 10)

		// List images only
		listResp, err = setup.objectStorage.ListObjects(ctx, &service.ListObjectsRequest{
			Prefix: "list-test:images/",
			Limit:  100,
		})
		require.NoError(t, err)
		assert.Len(t, listResp.Objects, 5)

		// List all with common parent prefix
		listResp, err = setup.objectStorage.ListObjects(ctx, &service.ListObjectsRequest{
			Prefix: "list-test:",
			Limit:  100,
		})
		require.NoError(t, err)
		assert.Len(t, listResp.Objects, 15)
	})

	t.Run("list with limit", func(t *testing.T) {
		// Create 15 objects
		for i := 0; i < 15; i++ {
			_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
				Key:   fmt.Sprintf("paginate:files/file%03d.txt", i),
				Value: []byte(fmt.Sprintf("File %d", i)),
			})
			require.NoError(t, err)
		}

		// Test limit functionality
		limited, err := setup.objectStorage.ListObjects(ctx, &service.ListObjectsRequest{
			Prefix: "paginate:files/",
			Limit:  5,
		})
		require.NoError(t, err)
		assert.Len(t, limited.Objects, 5)
		assert.True(t, limited.HasMore, "should indicate more objects exist")

		// Get all without limit
		all, err := setup.objectStorage.ListObjects(ctx, &service.ListObjectsRequest{
			Prefix: "paginate:files/",
			Limit:  100,
		})
		require.NoError(t, err)
		assert.Len(t, all.Objects, 15)
		assert.False(t, all.HasMore)
	})
}

func TestE2E_ConcurrentOperations(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("concurrent puts and gets", func(t *testing.T) {
		const numGoroutines = 20
		const opsPerGoroutine = 10

		var wg sync.WaitGroup
		errChan := make(chan error, numGoroutines*opsPerGoroutine*2)

		// Concurrent writers
		for g := 0; g < numGoroutines; g++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for i := 0; i < opsPerGoroutine; i++ {
					key := fmt.Sprintf("concurrent:g%d/file%d.txt", goroutineID, i)
					data := []byte(fmt.Sprintf("Goroutine %d, File %d", goroutineID, i))

					_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
						Key:   key,
						Value: data,
					})
					if err != nil {
						errChan <- fmt.Errorf("put error g%d/f%d: %w", goroutineID, i, err)
					}
				}
			}(g)
		}

		wg.Wait()
		close(errChan)

		// Check for errors
		var errors []error
		for err := range errChan {
			errors = append(errors, err)
		}
		assert.Empty(t, errors)

		// Verify all objects were created
		listResp, err := setup.objectStorage.ListObjects(ctx, &service.ListObjectsRequest{
			Prefix: "concurrent:",
			Limit:  1000,
		})
		require.NoError(t, err)
		assert.Equal(t, numGoroutines*opsPerGoroutine, len(listResp.Objects))

		// Concurrent readers - verify data integrity
		errChan = make(chan error, numGoroutines*opsPerGoroutine)
		for g := 0; g < numGoroutines; g++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()
				for i := 0; i < opsPerGoroutine; i++ {
					key := fmt.Sprintf("concurrent:g%d/file%d.txt", goroutineID, i)
					expectedData := []byte(fmt.Sprintf("Goroutine %d, File %d", goroutineID, i))

					getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
						Key: key,
					})
					if err != nil {
						errChan <- fmt.Errorf("get error g%d/f%d: %w", goroutineID, i, err)
						continue
					}
					if !bytes.Equal(expectedData, getResp.Value) {
						errChan <- fmt.Errorf("data mismatch g%d/f%d", goroutineID, i)
					}
				}
			}(g)
		}

		wg.Wait()
		close(errChan)

		for err := range errChan {
			errors = append(errors, err)
		}
		assert.Empty(t, errors)
	})
}

func TestE2E_LargeObjectHandling(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("store and retrieve large objects", func(t *testing.T) {
		sizes := []int{
			64 * 1024,       // 64KB
			256 * 1024,      // 256KB
			1024 * 1024,     // 1MB
			5 * 1024 * 1024, // 5MB
		}

		for _, size := range sizes {
			t.Run(fmt.Sprintf("size_%dKB", size/1024), func(t *testing.T) {
				data := generateRandomData(size)
				key := fmt.Sprintf("large:objects/%dkb.bin", size/1024)

				// Put
				start := time.Now()
				putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
					Key:   key,
					Value: data,
				})
				putDuration := time.Since(start)
				require.NoError(t, err)
				assert.Equal(t, int64(size), putResp.Size)
				t.Logf("Put %dKB in %v", size/1024, putDuration)

				// Get
				start = time.Now()
				getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
					Key: key,
				})
				getDuration := time.Since(start)
				require.NoError(t, err)
				t.Logf("Get %dKB in %v", size/1024, getDuration)

				// Verify data integrity
				assert.True(t, bytes.Equal(data, getResp.Value))
			})
		}
	})
}

func TestE2E_BackendHealthTracking(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("check backend health", func(t *testing.T) {
		// Check individual backend health
		health, err := setup.backendManager.CheckHealth(ctx, "primary")
		require.NoError(t, err)
		assert.True(t, health.Healthy)
		assert.NotEmpty(t, health.LastCheck)

		// Check all backends
		allHealth := setup.backendManager.CheckAllHealth(ctx)
		assert.Len(t, allHealth, 4) // primary, replica-west, archive, tenant-premium

		for name, h := range allHealth {
			assert.True(t, h.Healthy, "backend %s should be healthy", name)
		}
	})

	t.Run("list backends shows all registered", func(t *testing.T) {
		backends := setup.backendManager.ListBackends()
		assert.Contains(t, backends, "primary")
		assert.Contains(t, backends, "replica-west")
		assert.Contains(t, backends, "archive")
		assert.Contains(t, backends, "tenant-premium")
	})
}

func TestE2E_ObjectMetadata(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("metadata is preserved through put/get cycle", func(t *testing.T) {
		testData := []byte("Document with rich metadata")
		tags := map[string]string{
			"department":   "engineering",
			"project":      "quicbaas",
			"confidential": "true",
		}

		// Put with metadata
		putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:         "metadata:docs/project-plan.doc",
			Value:       testData,
			ContentType: "application/msword",
			Tags:        tags,
		})
		require.NoError(t, err)
		assert.NotNil(t, putResp)

		// Get and verify metadata
		getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
			Key: "metadata:docs/project-plan.doc",
		})
		require.NoError(t, err)
		assert.Equal(t, "application/msword", getResp.ContentType)

		// Verify catalog entry has tags
		entry, err := setup.catalogManager.Get(ctx, "metadata:docs/project-plan.doc")
		require.NoError(t, err)
		assert.Equal(t, "engineering", entry.Tags["department"])
		assert.Equal(t, "quicbaas", entry.Tags["project"])
		assert.Equal(t, "true", entry.Tags["confidential"])
	})
}

// =============================================================================
// Worker Integration Tests
// =============================================================================

func TestE2E_WorkerRegistrationAndLifecycle(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("register and run background workers", func(t *testing.T) {
		// Create workers
		replicationWorker, err := catalog.NewReplicationWorker(
			setup.catalogManager,
			setup.backendManager,
			catalog.DefaultWorkerConfig(),
			setup.logAdapter,
		)
		require.NoError(t, err)

		archiveWorker, err := catalog.NewArchiveWorker(
			setup.catalogManager,
			setup.backendManager,
			catalog.DefaultWorkerConfig(),
			setup.logAdapter,
		)
		require.NoError(t, err)

		verifyWorker, err := catalog.NewVerifyWorker(
			setup.catalogManager,
			setup.backendManager,
			catalog.DefaultWorkerConfig(),
			setup.logAdapter,
		)
		require.NoError(t, err)

		retentionWorker, err := catalog.NewRetentionWorker(
			setup.catalogManager,
			setup.backendManager,
			catalog.DefaultRetentionWorkerConfig(),
			setup.logAdapter,
		)
		require.NoError(t, err)

		// Register all workers
		require.NoError(t, setup.workerManager.Register(replicationWorker))
		require.NoError(t, setup.workerManager.Register(archiveWorker))
		require.NoError(t, setup.workerManager.Register(verifyWorker))
		require.NoError(t, setup.workerManager.Register(retentionWorker))

		// Start all workers
		require.NoError(t, setup.workerManager.StartAll(ctx))
		assert.True(t, setup.workerManager.IsRunning())
		assert.Equal(t, 4, setup.workerManager.RunningWorkerCount())

		// Let workers run briefly
		time.Sleep(100 * time.Millisecond)

		// Check worker status
		statuses := setup.workerManager.GetStatus()
		assert.Len(t, statuses, 4)
		for _, status := range statuses {
			assert.True(t, status.Running, "worker %s should be running", status.Name)
		}

		// Stop all workers
		require.NoError(t, setup.workerManager.StopAll())
		assert.False(t, setup.workerManager.IsRunning())
	})
}

// =============================================================================
// Edge Cases
// =============================================================================

func TestE2E_EdgeCases(t *testing.T) {
	setup := newE2ETestSetup(t)
	defer setup.cleanup()

	ctx := context.Background()

	t.Run("empty value object", func(t *testing.T) {
		_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:   "edge:empty/file.txt",
			Value: []byte{},
		})
		// Empty value should fail with clear error
		require.Error(t, err)
		assert.Contains(t, err.Error(), "value is required")
	})

	t.Run("get nonexistent object", func(t *testing.T) {
		_, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
			Key: "nonexistent:path/file.txt",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("special characters in key", func(t *testing.T) {
		testData := []byte("Data with special key")
		// Use allowed special characters: dashes and underscores
		key := "special:path_name/sub-dir/file_with-dashes_and_underscores.txt"

		putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:   key,
			Value: testData,
		})
		require.NoError(t, err)
		assert.Equal(t, key, putResp.Key)

		getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
			Key: key,
		})
		require.NoError(t, err)
		assert.True(t, bytes.Equal(testData, getResp.Value))
	})

	t.Run("overwrite existing object", func(t *testing.T) {
		key := "overwrite:docs/file.txt"
		originalData := []byte("Original content")
		newData := []byte("New content that replaces the original")

		// Create original
		_, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:   key,
			Value: originalData,
		})
		require.NoError(t, err)

		// Overwrite
		putResp, err := setup.objectStorage.PutObject(ctx, &service.PutObjectRequest{
			Key:   key,
			Value: newData,
		})
		require.NoError(t, err)
		assert.Equal(t, int64(len(newData)), putResp.Size)

		// Verify new content
		getResp, err := setup.objectStorage.GetObject(ctx, &service.GetObjectRequest{
			Key: key,
		})
		require.NoError(t, err)
		assert.True(t, bytes.Equal(newData, getResp.Value))
	})
}
