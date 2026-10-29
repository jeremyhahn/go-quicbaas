// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// Package object composes the real object-storage stack (a PebbleDB catalog
// on disk, the backend manager with several backends, and the per-tenant
// config resolver) under ObjectStorageService, and checks that tenants land
// on their configured backends and the catalog records where each object
// lives. Single-backend round trips run in pkg/service.
package object

import (
	"context"
	"crypto/rand"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// objectStack is ObjectStorageService over real components, with the catalog
// kept for asserting where objects were recorded.
type objectStack struct {
	catalog catalog.CatalogManager
	objects service.ObjectStorageService
}

// newObjectStack builds the service over a PebbleDB catalog in a temp dir and
// four memory backends: "primary" (the default), "replica-west", "archive",
// and "tenant-premium". Tenant "tenant-premium" is configured onto the
// "tenant-premium" backend, tenant "tenant-basic" onto "primary".
func newObjectStack(t *testing.T) *objectStack {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "warn", Format: "json", Output: "stdout", Component: "objstore-integration"})
	require.NoError(t, err)
	logAdapter := logger.NewAdapter(log)

	catalogStorage, err := catalog.NewPebbleStorage(&catalog.PebbleStorageConfig{
		Path:        filepath.Join(t.TempDir(), "catalog"),
		CacheSize:   32 * 1024 * 1024,
		SyncWrites:  true,
		Compression: true,
	})
	require.NoError(t, err)

	catalogManager, err := catalog.NewCatalogManager(&catalog.ManagerConfig{Storage: catalogStorage, Logger: logAdapter})
	require.NoError(t, err)

	backendManager, err := catalog.NewBackendManager(catalog.DefaultBackendManagerConfig())
	require.NoError(t, err)
	backends := []struct {
		name   string
		tier   catalog.StorageTier
		region string
		dflt   bool
	}{
		{"primary", catalog.TierHot, "us-east-1", true},
		{"replica-west", catalog.TierWarm, "us-west-2", false},
		{"archive", catalog.TierCold, "us-east-1", false},
		{"tenant-premium", catalog.TierHot, "eu-west-1", false},
	}
	for _, b := range backends {
		require.NoError(t, backendManager.RegisterBackend(b.name, &catalog.BackendConfig{
			Type: "memory", Enabled: true, Default: b.dflt, Tier: b.tier, Region: b.region,
		}))
	}

	resolver := config.NewObjectStoreResolver(&config.ObjectStoreFullConfig{
		Defaults: config.ObjectStoreDefaults{
			PrimaryBackend: "primary",
			ValueThreshold: 1024,
			Compression:    "none",
			Replication: config.ObjectStoreReplication{
				Enabled:      true,
				MinReplicas:  1,
				TargetStores: []string{"replica-west"},
				Mode:         "async",
				Priority:     5,
			},
		},
		Tenants: map[string]*config.ObjectStoreTenantConfig{
			"tenant-premium": {
				PrimaryBackend:  "tenant-premium",
				AllowedBackends: []string{"tenant-premium", "replica-west"},
				Quotas:          config.ObjectStoreQuotas{MaxStorageBytes: 1 << 30, MaxObjectCount: 10000, MaxObjectSize: 100 << 20},
			},
			"tenant-basic": {
				PrimaryBackend:  "primary",
				AllowedBackends: []string{"primary"},
				Quotas:          config.ObjectStoreQuotas{MaxStorageBytes: 100 << 20, MaxObjectCount: 1000, MaxObjectSize: 10 << 20},
			},
		},
	}, nil)

	workerManager := catalog.NewWorkerManager(logAdapter)
	objects, err := service.NewObjectStorageService(catalogManager, backendManager, resolver, workerManager, logAdapter)
	require.NoError(t, err)

	t.Cleanup(func() {
		assert.NoError(t, workerManager.Close())
		assert.NoError(t, backendManager.Close())
		assert.NoError(t, catalogManager.Close())
	})
	return &objectStack{catalog: catalogManager, objects: objects}
}

func randomData(t *testing.T, size int) []byte {
	t.Helper()
	data := make([]byte, size)
	_, err := rand.Read(data)
	require.NoError(t, err)
	return data
}

func TestObjectStorage_CatalogTracksBackendLocations(t *testing.T) {
	stack := newObjectStack(t)
	systemCtx := tenantctx.WithSystem(context.Background())

	t.Run("default put lands on the primary backend", func(t *testing.T) {
		put, err := stack.objects.PutObject(systemCtx, &service.PutObjectRequest{
			Key:   "catalog-test:primary/file1.bin",
			Value: randomData(t, 2048),
		})
		require.NoError(t, err)
		assert.Equal(t, "primary", put.Backend)

		entry, err := stack.catalog.Get(systemCtx, "catalog-test:primary/file1.bin")
		require.NoError(t, err)
		assert.Equal(t, "primary", entry.PrimaryStore.Store)
		assert.Equal(t, catalog.LocationStatusActive, entry.PrimaryStore.Status)
		assert.NotEmpty(t, entry.PrimaryStore.ObjectRef)
		assert.Equal(t, int64(2048), entry.PrimaryStore.StoredSize)
		assert.Positive(t, entry.PrimaryStore.SyncedAt)
	})

	t.Run("tenant put lands on the tenant's configured backend", func(t *testing.T) {
		premiumCtx := tenantctx.WithTenant(context.Background(), "tenant-premium")
		put, err := stack.objects.PutObject(premiumCtx, &service.PutObjectRequest{
			Key:      "premium:data/important.bin",
			Value:    randomData(t, 1024),
			TenantID: "tenant-premium",
		})
		require.NoError(t, err)
		assert.Equal(t, "tenant-premium", put.Backend)

		entry, err := stack.catalog.Get(systemCtx, "premium:data/important.bin")
		require.NoError(t, err)
		assert.Equal(t, "tenant-premium", entry.PrimaryStore.Store)
		assert.Equal(t, "tenant-premium", entry.TenantID)
	})

	t.Run("explicit target backend overrides the default", func(t *testing.T) {
		put, err := stack.objects.PutObject(systemCtx, &service.PutObjectRequest{
			Key:           "explicit:archive/old-file.bin",
			Value:         randomData(t, 512),
			TargetBackend: "archive",
		})
		require.NoError(t, err)
		assert.Equal(t, "archive", put.Backend)

		entry, err := stack.catalog.Get(systemCtx, "explicit:archive/old-file.bin")
		require.NoError(t, err)
		assert.Equal(t, "archive", entry.PrimaryStore.Store)
	})
}

func TestObjectStorage_MultipleBackendsRoundTrip(t *testing.T) {
	stack := newObjectStack(t)
	systemCtx := tenantctx.WithSystem(context.Background())

	objects := []struct {
		key     string
		backend string
		data    []byte
	}{
		{"multi:primary/file1.txt", "primary", []byte("Primary backend data")},
		{"multi:archive/file2.txt", "archive", []byte("Archive backend data")},
		{"multi:replica/file3.txt", "replica-west", []byte("Replica backend data")},
	}
	for _, obj := range objects {
		put, err := stack.objects.PutObject(systemCtx, &service.PutObjectRequest{Key: obj.key, Value: obj.data, TargetBackend: obj.backend})
		require.NoError(t, err)
		assert.Equal(t, obj.backend, put.Backend)
	}

	// Each object reads back from its own backend, as the catalog records.
	for _, obj := range objects {
		got, err := stack.objects.GetObject(systemCtx, &service.GetObjectRequest{Key: obj.key})
		require.NoError(t, err)
		assert.Equal(t, obj.data, got.Value, "data for %s", obj.key)

		entry, err := stack.catalog.Get(systemCtx, obj.key)
		require.NoError(t, err)
		assert.Equal(t, obj.backend, entry.PrimaryStore.Store)
	}
}

func TestObjectStorage_TenantBackendsAndIsolation(t *testing.T) {
	stack := newObjectStack(t)
	systemCtx := tenantctx.WithSystem(context.Background())
	// Each tenant acts with its own tenant bound, as the transports bind it.
	premiumCtx := tenantctx.WithTenant(context.Background(), "tenant-premium")
	basicCtx := tenantctx.WithTenant(context.Background(), "tenant-basic")

	t.Run("each tenant's data is stored on its own backend", func(t *testing.T) {
		premium, err := stack.objects.PutObject(premiumCtx, &service.PutObjectRequest{
			Key:      "premium-data:secrets/api-keys.txt",
			Value:    []byte("Premium tenant confidential data"),
			TenantID: "tenant-premium",
		})
		require.NoError(t, err)
		assert.Equal(t, "tenant-premium", premium.Backend)

		basic, err := stack.objects.PutObject(basicCtx, &service.PutObjectRequest{
			Key:      "basic-data:files/readme.txt",
			Value:    []byte("Basic tenant data"),
			TenantID: "tenant-basic",
		})
		require.NoError(t, err)
		assert.Equal(t, "primary", basic.Backend)

		premiumEntry, err := stack.catalog.Get(systemCtx, "premium-data:secrets/api-keys.txt")
		require.NoError(t, err)
		assert.Equal(t, "tenant-premium", premiumEntry.PrimaryStore.Store)
		assert.Equal(t, "tenant-premium", premiumEntry.TenantID)

		basicEntry, err := stack.catalog.Get(systemCtx, "basic-data:files/readme.txt")
		require.NoError(t, err)
		assert.Equal(t, "primary", basicEntry.PrimaryStore.Store)
		assert.Equal(t, "tenant-basic", basicEntry.TenantID)
	})

	t.Run("another tenant cannot read the object", func(t *testing.T) {
		_, err := stack.objects.PutObject(premiumCtx, &service.PutObjectRequest{
			Key:      "private:tenant-a/secret.txt",
			Value:    []byte("Tenant A's private data"),
			TenantID: "tenant-premium",
		})
		require.NoError(t, err)

		_, err = stack.objects.GetObject(basicCtx, &service.GetObjectRequest{
			Key:      "private:tenant-a/secret.txt",
			TenantID: "tenant-basic",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cross-tenant")
	})
}
