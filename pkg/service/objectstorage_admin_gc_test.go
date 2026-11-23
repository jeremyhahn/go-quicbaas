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
	"context"
	stderrors "errors"
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

func TestRunGCOperation_CompletesSuccessfully(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)

	// Add backends
	backendMgr.AddBackend("backend-1", nil, &catalog.BackendConfig{Type: "memory"})
	backendMgr.AddBackend("backend-2", nil, &catalog.BackendConfig{Type: "memory"})

	// Set store usage
	catalogMgr.getStoreUsageFn = func(ctx context.Context, store string) (*catalog.StoreUsage, error) {
		return &catalog.StoreUsage{Store: store, ObjectCount: 50, TotalSize: 5000}, nil
	}

	op := &GCOperation{
		ID:        "gc-test-001",
		Backends:  []string{"backend-1", "backend-2"},
		Status:    GCStatusRunning,
		StartedAt: 1000000,
	}

	// Run synchronously
	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	assert.Equal(t, GCStatusCompleted, op.Status)
	assert.NotZero(t, op.CompletedAt)
	assert.Equal(t, int64(100), op.ObjectsScanned) // 50 per backend
	assert.Empty(t, op.Error)
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

func TestRunGCOperation_StoreUsageError(t *testing.T) {
	svc, catalogMgr, backendMgr := createTestAdminService(t)

	backendMgr.AddBackend("backend-1", nil, &catalog.BackendConfig{Type: "memory"})
	backendMgr.AddBackend("backend-2", nil, &catalog.BackendConfig{Type: "memory"})

	// First backend fails, second succeeds
	catalogMgr.getStoreUsageFn = func(ctx context.Context, store string) (*catalog.StoreUsage, error) {
		if store == "backend-1" {
			return nil, stderrors.New("store usage unavailable")
		}
		return &catalog.StoreUsage{Store: store, ObjectCount: 25, TotalSize: 2500}, nil
	}

	op := &GCOperation{
		ID:        "gc-partial-001",
		Backends:  []string{"backend-1", "backend-2"},
		Status:    GCStatusRunning,
		StartedAt: 1000000,
	}

	svc.runGCOperation(tenantctx.WithSystem(context.Background()), op)

	// Should complete despite one backend failing
	assert.Equal(t, GCStatusCompleted, op.Status)
	assert.Equal(t, int64(25), op.ObjectsScanned) // Only second backend counted
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
