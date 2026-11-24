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

//go:build !integration

package service

import (
	"bytes"
	"context"
	stderrors "errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// runGCOperation Deep Path Tests
// =============================================================================

// gcFixture wires a backend storage and catalog entries for runGCOperation
// tests: ListByStore lists every entry key in catalogMgr.
func gcFixture(t *testing.T, backends ...string) (*objectStorageAdminService, *adminMockCatalogManager, map[string]*objstoreMockStorage) {
	t.Helper()
	svc, catalogMgr, backendMgr := createTestAdminService(t)
	stores := make(map[string]*objstoreMockStorage, len(backends))
	for _, name := range backends {
		st := newObjstoreMockStorage()
		stores[name] = st
		backendMgr.AddBackend(name, st, &catalog.BackendConfig{Type: "memory"})
	}
	catalogMgr.listByStoreFn = func(ctx context.Context, store string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
		t.Errorf("GC must not use the primary-store index")
		return &catalog.ListResult{Keys: []string{}}, nil
	}
	catalogMgr.listByPrefixFn = func(ctx context.Context, prefix string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
		catalogMgr.mu.RLock()
		defer catalogMgr.mu.RUnlock()
		keys := make([]string, 0, len(catalogMgr.entries))
		for key := range catalogMgr.entries {
			keys = append(keys, key)
		}
		return &catalog.ListResult{Keys: keys}, nil
	}
	return svc, catalogMgr, stores
}

// gcPutObject stores an object of size bytes on st, dated at.
func gcPutObject(t *testing.T, st *objstoreMockStorage, ref string, size int, at time.Time) {
	t.Helper()
	require.NoError(t, st.PutWithContext(context.Background(), ref, bytes.NewReader(make([]byte, size))))
	st.setLastModified(ref, at)
}

// gcCatalogRef records a catalog entry whose primary location is ref on store.
func gcCatalogRef(catalogMgr *adminMockCatalogManager, key, store, ref string) *catalog.CatalogEntry {
	entry := catalog.NewCatalogEntry(key, 1, nil)
	entry.PrimaryStore = catalog.StoreLocation{Store: store, ObjectRef: ref}
	catalogMgr.mu.Lock()
	catalogMgr.entries[key] = entry
	catalogMgr.mu.Unlock()
	return entry
}

func gcOp(id string, backends ...string) *GCOperation {
	return &GCOperation{
		ID:        id,
		Backends:  backends,
		Status:    GCStatusRunning,
		StartedAt: time.Now().UnixNano(),
	}
}

func gcHas(st *objstoreMockStorage, ref string) bool {
	ok, _ := st.Exists(context.Background(), ref)
	return ok
}

func TestRunGCOperation_CompletesSuccessfully(t *testing.T) {
	svc, catalogMgr, stores := gcFixture(t, "backend-1", "backend-2")
	old := time.Now().Add(-2 * time.Hour)
	gcPutObject(t, stores["backend-1"], "ref-1", 10, old)
	gcPutObject(t, stores["backend-2"], "ref-2", 10, old)
	gcCatalogRef(catalogMgr, "a.txt", "backend-1", "ref-1")
	gcCatalogRef(catalogMgr, "b.txt", "backend-2", "ref-2")

	op := gcOp("gc-test-001", "backend-1", "backend-2")
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	assert.Equal(t, GCStatusCompleted, op.Status)
	assert.NotZero(t, op.CompletedAt)
	assert.Equal(t, int64(2), op.ObjectsScanned) // one listed object per backend
	assert.Equal(t, int64(0), op.ObjectsDeleted) // both are referenced
	assert.Empty(t, op.Error)
	assert.True(t, gcHas(stores["backend-1"], "ref-1"))
	assert.True(t, gcHas(stores["backend-2"], "ref-2"))
}

func TestRunGCOperation_ContextCancelled(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)

	backendMgr.AddBackend("backend-1", nil, &catalog.BackendConfig{Type: "memory"})

	// Cancel context before running
	ctx, cancel := context.WithCancel(tenantctx.WithSystem(context.Background()))
	cancel()

	op := &GCOperation{
		ID:        "gc-cancel-001",
		Backends:  []string{"backend-1"},
		Status:    GCStatusRunning,
		StartedAt: 1000000,
	}

	svc.runGCOperation(ctx, op)

	assert.Equal(t, GCStatusFailed, op.Status)
	assert.NotZero(t, op.CompletedAt)
	assert.Contains(t, op.Error, "context canceled")
}

func TestRunGCOperation_BackendListErrorFailsTheOperation(t *testing.T) {
	svc, _, stores := gcFixture(t, "backend-1", "backend-2")
	stores["backend-1"].listWithContextFn = func(ctx context.Context, prefix string) ([]string, error) {
		return nil, stderrors.New("list unavailable")
	}
	gcPutObject(t, stores["backend-2"], "orphan", 10, time.Now().Add(-2*time.Hour))

	op := gcOp("gc-partial-001", "backend-1", "backend-2")
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	// A backend that cannot be listed fails the operation: it never reports
	// a completed collection it did not do.
	assert.Equal(t, GCStatusFailed, op.Status)
	assert.NotZero(t, op.CompletedAt)
	assert.Contains(t, op.Error, "backend-1")
	assert.Contains(t, op.Error, "list unavailable")
	assert.True(t, gcHas(stores["backend-2"], "orphan"))
}

func TestRunGCOperation_MissingBackendFailsTheOperation(t *testing.T) {
	svc, _, _ := gcFixture(t)

	op := gcOp("gc-missing-001", "nope")
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	assert.Equal(t, GCStatusFailed, op.Status)
	assert.Contains(t, op.Error, "nope")
	assert.NotZero(t, op.CompletedAt)
}

func TestRunGCOperation_DeletesOnlyOldOrphans(t *testing.T) {
	svc, catalogMgr, stores := gcFixture(t, "backend-1")
	st := stores["backend-1"]
	gcPutObject(t, st, "ref-live", 10, time.Now().Add(-3*time.Hour))
	gcPutObject(t, st, "ref-old-orphan", 25, time.Now().Add(-3*time.Hour))
	gcPutObject(t, st, "ref-young-orphan", 40, time.Now().Add(-10*time.Minute))
	gcCatalogRef(catalogMgr, "live.txt", "backend-1", "ref-live")

	op := gcOp("gc-orphans-001", "backend-1")
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	require.Equal(t, GCStatusCompleted, op.Status, op.Error)
	assert.Equal(t, int64(3), op.ObjectsScanned)
	assert.Equal(t, int64(1), op.ObjectsDeleted)
	assert.Equal(t, int64(25), op.BytesReclaimed)
	assert.False(t, gcHas(st, "ref-old-orphan"))
	assert.True(t, gcHas(st, "ref-live"))
	assert.True(t, gcHas(st, "ref-young-orphan"))
}

func TestRunGCOperation_DryRunDeletesNothing(t *testing.T) {
	svc, catalogMgr, stores := gcFixture(t, "backend-1")
	st := stores["backend-1"]
	gcPutObject(t, st, "ref-live", 10, time.Now().Add(-3*time.Hour))
	gcPutObject(t, st, "ref-old-orphan", 25, time.Now().Add(-3*time.Hour))
	gcPutObject(t, st, "ref-young-orphan", 40, time.Now().Add(-10*time.Minute))
	gcCatalogRef(catalogMgr, "live.txt", "backend-1", "ref-live")

	op := gcOp("gc-dry-001", "backend-1")
	op.DryRun = true
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	require.Equal(t, GCStatusCompleted, op.Status, op.Error)
	assert.Equal(t, int64(3), op.ObjectsScanned)
	assert.Equal(t, int64(1), op.ObjectsDeleted)
	assert.Equal(t, int64(25), op.BytesReclaimed)
	assert.True(t, gcHas(st, "ref-live"))
	assert.True(t, gcHas(st, "ref-old-orphan"))
	assert.True(t, gcHas(st, "ref-young-orphan"))
}

func TestRunGCOperation_AggressiveUsesTheShortGrace(t *testing.T) {
	svc, _, stores := gcFixture(t, "backend-1")
	st := stores["backend-1"]
	gcPutObject(t, st, "ref-ten-minutes", 10, time.Now().Add(-10*time.Minute))
	gcPutObject(t, st, "ref-one-minute", 10, time.Now().Add(-time.Minute))

	op := gcOp("gc-aggr-001", "backend-1")
	op.Aggressive = true
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	require.Equal(t, GCStatusCompleted, op.Status, op.Error)
	assert.Equal(t, int64(1), op.ObjectsDeleted)
	assert.False(t, gcHas(st, "ref-ten-minutes"))
	assert.True(t, gcHas(st, "ref-one-minute"))

	// The same ten-minute-old orphan survives a normal run.
	gcPutObject(t, st, "ref-ten-minutes", 10, time.Now().Add(-10*time.Minute))
	op2 := gcOp("gc-aggr-002", "backend-1")
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op2)
	require.Equal(t, GCStatusCompleted, op2.Status, op2.Error)
	assert.Equal(t, int64(0), op2.ObjectsDeleted)
	assert.True(t, gcHas(st, "ref-ten-minutes"))
}

func TestRunGCOperation_UndatableOrphanIsKept(t *testing.T) {
	svc, _, stores := gcFixture(t, "backend-1")
	st := stores["backend-1"]
	// No LastModified: the mock reports a zero time.
	require.NoError(t, st.PutWithContext(context.Background(), "ref-undated", bytes.NewReader([]byte("x"))))

	op := gcOp("gc-undated-001", "backend-1")
	op.Aggressive = true
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	require.Equal(t, GCStatusCompleted, op.Status, op.Error)
	assert.Equal(t, int64(1), op.ObjectsScanned)
	assert.Equal(t, int64(0), op.ObjectsDeleted)
	assert.True(t, gcHas(st, "ref-undated"))
}

func TestRunGCOperation_ReplicaLocationIsReferenced(t *testing.T) {
	svc, catalogMgr, stores := gcFixture(t, "backend-1", "backend-2")
	old := time.Now().Add(-3 * time.Hour)
	gcPutObject(t, stores["backend-1"], "ref-primary", 10, old)
	gcPutObject(t, stores["backend-2"], "ref-replica", 10, old)
	entry := gcCatalogRef(catalogMgr, "replicated.txt", "backend-1", "ref-primary")
	entry.Replicas = []catalog.StoreLocation{{Store: "backend-2", ObjectRef: "ref-replica"}}

	op := gcOp("gc-replica-001", "backend-1", "backend-2")
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	require.Equal(t, GCStatusCompleted, op.Status, op.Error)
	assert.Equal(t, int64(0), op.ObjectsDeleted)
	assert.True(t, gcHas(stores["backend-1"], "ref-primary"))
	assert.True(t, gcHas(stores["backend-2"], "ref-replica"))
}

// gcRealCatalogRun stores one entry in a real Pebble catalog with its primary
// copy on backend-1 and, through addCopy, a second copy "ref-copy" on
// backend-2, then runs a deleting collection on backend-2 and returns the
// operation and the backend-2 store.
func gcRealCatalogRun(t *testing.T, opID string, addCopy func(entry *catalog.CatalogEntry)) (*GCOperation, *objstoreMockStorage) {
	t.Helper()
	storage, err := catalog.NewPebbleStorage(&catalog.PebbleStorageConfig{
		Path:       filepath.Join(t.TempDir(), "catalog"),
		SyncWrites: false,
	})
	require.NoError(t, err)
	realCatalog, err := catalog.NewCatalogManager(&catalog.ManagerConfig{Storage: storage})
	require.NoError(t, err)
	t.Cleanup(func() { _ = realCatalog.Close() })

	backendMgr := newAdminMockBackendManager()
	primary, secondary := newObjstoreMockStorage(), newObjstoreMockStorage()
	backendMgr.AddBackend("backend-1", primary, &catalog.BackendConfig{Type: "memory"})
	backendMgr.AddBackend("backend-2", secondary, &catalog.BackendConfig{Type: "memory"})
	svcIface, err := NewObjectStorageAdminService(realCatalog, backendMgr, nil, newObjstoreMockLogger())
	require.NoError(t, err)
	svc := svcIface.(*objectStorageAdminService)

	old := time.Now().Add(-3 * time.Hour)
	gcPutObject(t, primary, "ref-primary", 10, old)
	gcPutObject(t, secondary, "ref-copy", 10, old)

	entry := catalog.NewCatalogEntry("ns-gc:dir/copied.txt", 10, []byte("checksum"))
	entry.PrimaryStore = catalog.StoreLocation{Store: "backend-1", ObjectRef: "ref-primary", Status: catalog.LocationStatusActive}
	entry.TenantID = "tenant-1"
	addCopy(entry)
	require.NoError(t, realCatalog.Put(context.Background(), entry))

	op := gcOp(opID, "backend-2")
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)
	return op, secondary
}

func TestRunGCOperation_ReplicaOnTheRealCatalogIsReferenced(t *testing.T) {
	op, secondary := gcRealCatalogRun(t, "gc-real-replica", func(entry *catalog.CatalogEntry) {
		entry.Replicas = []catalog.StoreLocation{{Store: "backend-2", ObjectRef: "ref-copy", Status: catalog.LocationStatusActive}}
	})

	require.Equal(t, GCStatusCompleted, op.Status, op.Error)
	assert.Equal(t, int64(0), op.ObjectsDeleted)
	assert.True(t, gcHas(secondary, "ref-copy"))
}

func TestRunGCOperation_ArchiveOnTheRealCatalogIsReferenced(t *testing.T) {
	op, secondary := gcRealCatalogRun(t, "gc-real-archive", func(entry *catalog.CatalogEntry) {
		entry.ArchiveStore = &catalog.StoreLocation{Store: "backend-2", ObjectRef: "ref-copy", Status: catalog.LocationStatusActive}
	})

	require.Equal(t, GCStatusCompleted, op.Status, op.Error)
	assert.Equal(t, int64(0), op.ObjectsDeleted)
	assert.True(t, gcHas(secondary, "ref-copy"))
}

func TestRunGCOperation_CatalogPagesAreFollowed(t *testing.T) {
	svc, catalogMgr, stores := gcFixture(t, "backend-1")
	st := stores["backend-1"]
	old := time.Now().Add(-3 * time.Hour)
	gcPutObject(t, st, "ref-page-1", 10, old)
	gcPutObject(t, st, "ref-page-2", 10, old)
	gcCatalogRef(catalogMgr, "p1.txt", "backend-1", "ref-page-1")
	gcCatalogRef(catalogMgr, "p2.txt", "backend-1", "ref-page-2")
	catalogMgr.listByPrefixFn = func(ctx context.Context, prefix string, opts *catalog.ListOptions) (*catalog.ListResult, error) {
		if opts.Cursor == "" {
			return &catalog.ListResult{Keys: []string{"p1.txt"}, HasMore: true, NextCursor: "next"}, nil
		}
		return &catalog.ListResult{Keys: []string{"p2.txt"}}, nil
	}

	op := gcOp("gc-pages-001", "backend-1")
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	require.Equal(t, GCStatusCompleted, op.Status, op.Error)
	assert.Equal(t, int64(0), op.ObjectsDeleted)
	assert.True(t, gcHas(st, "ref-page-2"))
}

func TestRunGCOperation_EmptyBackendList(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	op := &GCOperation{
		ID:        "gc-empty-001",
		Backends:  []string{},
		Status:    GCStatusRunning,
		StartedAt: 1000000,
	}

	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	assert.Equal(t, GCStatusCompleted, op.Status)
	assert.Equal(t, int64(0), op.ObjectsScanned)
}

func TestCompactBackend_UnsupportedBackendIsTypedError(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)
	backendMgr.AddBackend("test-backend", nil, &catalog.BackendConfig{Type: "local", Enabled: true})

	resp, err := svc.CompactBackend(tenantctx.WithSystem(context.Background()), &CompactBackendRequest{
		Name:       "test-backend",
		Aggressive: true,
	})
	require.Error(t, err)
	assert.Nil(t, resp)

	var unsupported *api.CompactionUnsupportedError
	require.ErrorAs(t, err, &unsupported)
	assert.Equal(t, "test-backend", unsupported.Backend)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrUnimplemented, qbErr.Code)
}

func TestCompactBackend_UnknownBackendIsNotFound(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	resp, err := svc.CompactBackend(tenantctx.WithSystem(context.Background()), &CompactBackendRequest{Name: "missing"})
	require.Error(t, err)
	assert.Nil(t, resp)

	var unsupported *api.CompactionUnsupportedError
	assert.False(t, stderrors.As(err, &unsupported))

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrNotFound, qbErr.Code)
}

// =============================================================================
// CompactBackend Tests
// =============================================================================

// =============================================================================
// GetGarbageCollectionProgress Deep Path Tests
// =============================================================================

func TestGetGarbageCollectionProgress_FoundCompletedOperation(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	// Add a completed GC operation
	svc.mu.Lock()
	svc.gcOperations["gc-completed"] = &GCOperation{
		ID:             "gc-completed",
		Backends:       []string{"backend-1"},
		Status:         GCStatusCompleted,
		ObjectsScanned: 100,
		ObjectsDeleted: 5,
		BytesReclaimed: 1024,
		StartedAt:      1000000,
		CompletedAt:    2000000,
	}
	svc.mu.Unlock()

	resp, err := svc.GetGarbageCollectionProgress(tenantctx.WithSystem(context.Background()), &GetGarbageCollectionProgressRequest{
		OperationID: "gc-completed",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "gc-completed", resp.OperationID)
	assert.Equal(t, GCStatusCompleted, resp.Status)
	assert.Equal(t, int64(100), resp.ObjectsScanned)
	assert.Equal(t, int64(5), resp.ObjectsDeleted)
	assert.Equal(t, int64(1024), resp.BytesReclaimed)
}

func TestGetGarbageCollectionProgress_NotFoundOperation(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	resp, err := svc.GetGarbageCollectionProgress(tenantctx.WithSystem(context.Background()), &GetGarbageCollectionProgressRequest{
		OperationID: "nonexistent",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestGetGarbageCollectionProgress_RunningOperation(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	// Add a running GC operation
	svc.mu.Lock()
	svc.gcOperations["gc-running"] = &GCOperation{
		ID:             "gc-running",
		Backends:       []string{"backend-1", "backend-2"},
		Status:         GCStatusRunning,
		ObjectsScanned: 50,
		StartedAt:      1000000,
	}
	svc.mu.Unlock()

	resp, err := svc.GetGarbageCollectionProgress(tenantctx.WithSystem(context.Background()), &GetGarbageCollectionProgressRequest{
		OperationID: "gc-running",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, GCStatusRunning, resp.Status)
	assert.Equal(t, int64(50), resp.ObjectsScanned)
}

func TestGetGarbageCollectionProgress_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	resp, err := svc.GetGarbageCollectionProgress(tenantctx.WithSystem(context.Background()), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}

// TestGetGarbageCollectionProgress_ConcurrentWithRunIsRaceFree reads the
// progress of an operation while runGCOperation updates it in its own
// goroutine. Run with -race: every read of the operation's fields must happen
// under the service lock.
func TestGetGarbageCollectionProgress_ConcurrentWithRunIsRaceFree(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)
	backendMgr.AddBackend("backend-1", nil, &catalog.BackendConfig{Type: "memory"})
	backendMgr.AddBackend("backend-2", nil, &catalog.BackendConfig{Type: "memory"})

	catalogMgr.getStoreUsageFn = func(ctx context.Context, store string) (*catalog.StoreUsage, error) {
		time.Sleep(time.Millisecond)
		return &catalog.StoreUsage{Store: store, ObjectCount: 7, TotalSize: 70}, nil
	}

	op := &GCOperation{
		ID:        "gc-race-001",
		Backends:  []string{"backend-1", "backend-2"},
		Status:    GCStatusRunning,
		StartedAt: time.Now().UnixNano(),
	}
	svc.mu.Lock()
	svc.gcOperations[op.ID] = op
	svc.mu.Unlock()

	go svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	ctx := tenantctx.WithSystem(context.Background())
	deadline := time.Now().Add(5 * time.Second)
	var wg sync.WaitGroup
	finals := make([]GCStatus, 4)
	errs := make([]error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for time.Now().Before(deadline) {
				resp, err := svc.GetGarbageCollectionProgress(ctx, &GetGarbageCollectionProgressRequest{OperationID: op.ID})
				if err != nil {
					errs[i] = err
					return
				}
				finals[i] = resp.Status
				if resp.Status != GCStatusRunning {
					return
				}
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < 4; i++ {
		require.NoError(t, errs[i])
		assert.Contains(t, []GCStatus{GCStatusCompleted, GCStatusFailed}, finals[i],
			"reader %d did not see a terminal status within 5s", i)
	}
}

func TestGetGarbageCollectionProgress_OtherTenantGetsNotFound(t *testing.T) {
	svc, _, _ := createTestAdminService(t)
	svc.mu.Lock()
	svc.gcOperations["gc-tenant-a"] = &GCOperation{
		ID:             "gc-tenant-a",
		TenantID:       "tenant-a",
		Status:         GCStatusCompleted,
		ObjectsScanned: 3,
		StartedAt:      1000000,
		CompletedAt:    2000000,
	}
	svc.mu.Unlock()
	req := &GetGarbageCollectionProgressRequest{OperationID: "gc-tenant-a"}

	resp, err := svc.GetGarbageCollectionProgress(tenantctx.WithTenant(context.Background(), "tenant-b"), req)
	require.Error(t, err)
	assert.Nil(t, resp)
	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrNotFound, qbErr.Code)

	// The not-found matches an unknown ID exactly.
	_, unknownErr := svc.GetGarbageCollectionProgress(tenantctx.WithTenant(context.Background(), "tenant-b"),
		&GetGarbageCollectionProgressRequest{OperationID: "gc-unknown"})
	var unknownQB *dberrors.QBError
	require.ErrorAs(t, unknownErr, &unknownQB)
	assert.Equal(t, unknownQB.Code, qbErr.Code)

	resp, err = svc.GetGarbageCollectionProgress(tenantctx.WithTenant(context.Background(), "tenant-a"), req)
	require.NoError(t, err)
	assert.Equal(t, "gc-tenant-a", resp.OperationID)
	assert.Equal(t, int64(3), resp.ObjectsScanned)

	resp, err = svc.GetGarbageCollectionProgress(tenantctx.WithSystem(context.Background()), req)
	require.NoError(t, err)
	assert.Equal(t, "gc-tenant-a", resp.OperationID)
}

func TestRunGarbageCollection_TenantCallerIsRefused(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	resp, err := svc.RunGarbageCollection(tenantctx.WithTenant(context.Background(), "tenant-a"),
		&RunGarbageCollectionRequest{Backends: []string{}, DryRun: true})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrCrossTenantAccess, qbErr.Code)

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	assert.Empty(t, svc.gcOperations)
}

func TestRunGarbageCollection_DeletingRunIsRefused(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	resp, err := svc.RunGarbageCollection(tenantctx.WithSystem(context.Background()),
		&RunGarbageCollectionRequest{Backends: []string{}, DryRun: false})
	require.Error(t, err)
	assert.Nil(t, resp)

	var unsupported *api.GarbageCollectionDeleteUnsupportedError
	require.ErrorAs(t, err, &unsupported)

	var qbErr *dberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, dberrors.ErrUnimplemented, qbErr.Code)

	svc.mu.RLock()
	defer svc.mu.RUnlock()
	assert.Empty(t, svc.gcOperations)
}

func TestRunGarbageCollection_DryRunStarts(t *testing.T) {
	svc, _, stores := gcFixture(t, "backend-1")
	st := stores["backend-1"]
	gcPutObject(t, st, "ref-old-orphan", 10, time.Now().Add(-3*time.Hour))
	ctx := tenantctx.WithSystem(context.Background())

	resp, err := svc.RunGarbageCollection(ctx, &RunGarbageCollectionRequest{Backends: []string{"backend-1"}, DryRun: true})
	require.NoError(t, err)
	require.NotNil(t, resp)

	var progress *GetGarbageCollectionProgressResponse
	deadline := time.Now().Add(5 * time.Second)
	for {
		progress, err = svc.GetGarbageCollectionProgress(ctx, &GetGarbageCollectionProgressRequest{OperationID: resp.OperationID})
		require.NoError(t, err)
		if progress.Status != GCStatusRunning {
			break
		}
		require.True(t, time.Now().Before(deadline), "garbage collection did not finish within 5s")
		time.Sleep(10 * time.Millisecond)
	}

	require.Equal(t, GCStatusCompleted, progress.Status, progress.Error)
	assert.Equal(t, int64(1), progress.ObjectsDeleted)
	assert.True(t, gcHas(st, "ref-old-orphan"))
}
