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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/catalog"
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
	svc.runGCOperation(context.Background(), op)

	assert.Equal(t, GCStatusCompleted, op.Status)
	assert.NotZero(t, op.CompletedAt)
	assert.Equal(t, int64(100), op.ObjectsScanned) // 50 per backend
	assert.Empty(t, op.Error)
}

func TestRunGCOperation_ContextCancelled(t *testing.T) {
	svc, _, backendMgr := createTestAdminService(t)

	backendMgr.AddBackend("backend-1", nil, &catalog.BackendConfig{Type: "memory"})

	// Cancel context before running
	ctx, cancel := context.WithCancel(context.Background())
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

	svc.runGCOperation(context.Background(), op)

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

	svc.runGCOperation(context.Background(), op)

	assert.Equal(t, GCStatusCompleted, op.Status)
	assert.Equal(t, int64(0), op.ObjectsScanned)
}

// =============================================================================
// runCompaction Deep Path Tests
// =============================================================================

func TestRunCompaction_CompletesSuccessfully(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	op := &CompactOperation{
		ID:        "compact-test-001",
		Backend:   "test-backend",
		Status:    "running",
		StartedAt: 1000000,
	}

	svc.runCompaction(context.Background(), op)

	assert.Equal(t, "completed", op.Status)
	assert.NotZero(t, op.CompletedAt)
	assert.Empty(t, op.Error)
}

func TestRunCompaction_AggressiveMode(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	op := &CompactOperation{
		ID:         "compact-aggressive-001",
		Backend:    "test-backend",
		Aggressive: true,
		Status:     "running",
		StartedAt:  1000000,
	}

	svc.runCompaction(context.Background(), op)

	assert.Equal(t, "completed", op.Status)
	assert.NotZero(t, op.CompletedAt)
}

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

	resp, err := svc.GetGarbageCollectionProgress(context.Background(), &GetGarbageCollectionProgressRequest{
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

	resp, err := svc.GetGarbageCollectionProgress(context.Background(), &GetGarbageCollectionProgressRequest{
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

	resp, err := svc.GetGarbageCollectionProgress(context.Background(), &GetGarbageCollectionProgressRequest{
		OperationID: "gc-running",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, GCStatusRunning, resp.Status)
	assert.Equal(t, int64(50), resp.ObjectsScanned)
}

func TestGetGarbageCollectionProgress_NilRequest(t *testing.T) {
	svc, _, _ := createTestAdminService(t)

	resp, err := svc.GetGarbageCollectionProgress(context.Background(), nil)
	require.Error(t, err)
	assert.Nil(t, resp)
}
