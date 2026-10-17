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

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// =============================================================================
// StartReencryption Validation Tests
// =============================================================================

func TestKMSService_StartReencryption_NilRequest(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.StartReencryption(systemTenantCtx(), nil)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request is nil")
}

func TestKMSService_StartReencryption_MissingFromVersion(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	req := &StartReencryptionRequest{FromVersion: 0, ToVersion: 2}
	resp, err := svc.StartReencryption(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "from_version is required")
}

func TestKMSService_StartReencryption_MissingToVersion(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	req := &StartReencryptionRequest{FromVersion: 1, ToVersion: 0}
	resp, err := svc.StartReencryption(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "to_version is required")
}

func TestKMSService_StartReencryption_InvalidVersionOrder(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	req := &StartReencryptionRequest{FromVersion: 3, ToVersion: 1}
	resp, err := svc.StartReencryption(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "to_version must be greater")
}

func TestKMSService_StartReencryption_AlreadyInProgress(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = true
	svc.reencryptionState.mu.Unlock()

	req := &StartReencryptionRequest{FromVersion: 1, ToVersion: 2}
	resp, err := svc.StartReencryption(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already in progress")
}

func TestKMSService_StartReencryption_ListKeysError(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return nil, errors.New("storage failure")
	}

	req := &StartReencryptionRequest{FromVersion: 1, ToVersion: 2}
	resp, err := svc.StartReencryption(systemTenantCtx(), req)
	assert.Nil(t, resp)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list keys")
}

func TestKMSService_StartReencryption_WithKeys(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return []interface{}{
			&kms.KeyMetadata{KeyID: "key-1", State: kms.KeyStateEnabled},
			&kms.KeyMetadata{KeyID: "key-2", State: kms.KeyStateEnabled},
		}, nil
	}

	req := &StartReencryptionRequest{
		FromVersion: 1,
		ToVersion:   2,
		BatchSize:   10,
		Concurrency: 2,
	}
	resp, err := svc.StartReencryption(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Started)
	assert.NotEmpty(t, resp.OperationID)
	assert.Equal(t, int64(2), resp.TotalEntities)
}

func TestKMSService_StartReencryption_DefaultBatchSizeAndConcurrency(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	mockMgr.listKeysFn = func(ctx context.Context, prefix string) ([]interface{}, error) {
		return []interface{}{}, nil
	}

	req := &StartReencryptionRequest{
		FromVersion: 1,
		ToVersion:   2,
		BatchSize:   0,
		Concurrency: 0,
	}
	resp, err := svc.StartReencryption(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Started)
}

// =============================================================================
// runReencryption Tests
// =============================================================================

func TestKMSService_RunReencryption_CompletesSuccessfully(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = true
	svc.reencryptionState.status = "running"
	svc.reencryptionState.operationID = "test-op"
	svc.reencryptionState.totalKeys = 3
	svc.reencryptionState.processedKeys.Store(0)
	svc.reencryptionState.failedKeys.Store(0)
	svc.reencryptionState.mu.Unlock()

	keys := []interface{}{
		&kms.KeyMetadata{KeyID: "k1"},
		&kms.KeyMetadata{KeyID: "k2"},
		&kms.KeyMetadata{KeyID: "k3"},
	}
	req := &StartReencryptionRequest{BatchSize: 2}

	svc.runReencryption(systemTenantCtx(), req, keys)

	svc.reencryptionState.mu.RLock()
	defer svc.reencryptionState.mu.RUnlock()
	assert.Equal(t, "completed", svc.reencryptionState.status)
	assert.False(t, svc.reencryptionState.inProgress)
	assert.Equal(t, uint64(3), svc.reencryptionState.processedKeys.Load())
}

func TestKMSService_RunReencryption_Cancelled(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = true
	svc.reencryptionState.status = "running"
	svc.reencryptionState.operationID = "cancel-op"
	svc.reencryptionState.totalKeys = 5
	svc.reencryptionState.processedKeys.Store(0)
	svc.reencryptionState.failedKeys.Store(0)
	svc.reencryptionState.mu.Unlock()

	keys := make([]interface{}, 100)
	for i := range keys {
		keys[i] = &kms.KeyMetadata{KeyID: "key"}
	}

	ctx, cancel := context.WithCancel(systemTenantCtx())
	cancel()

	req := &StartReencryptionRequest{BatchSize: 10}
	svc.runReencryption(ctx, req, keys)

	svc.reencryptionState.mu.RLock()
	defer svc.reencryptionState.mu.RUnlock()
	assert.Equal(t, "cancelled", svc.reencryptionState.status)
	assert.False(t, svc.reencryptionState.inProgress)
}

func TestKMSService_RunReencryption_WithFailedKeys(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = true
	svc.reencryptionState.status = "running"
	svc.reencryptionState.totalKeys = 2
	svc.reencryptionState.processedKeys.Store(0)
	svc.reencryptionState.failedKeys.Store(1)
	svc.reencryptionState.mu.Unlock()

	keys := []interface{}{&kms.KeyMetadata{KeyID: "k1"}, &kms.KeyMetadata{KeyID: "k2"}}
	req := &StartReencryptionRequest{BatchSize: 100}

	svc.runReencryption(systemTenantCtx(), req, keys)

	svc.reencryptionState.mu.RLock()
	defer svc.reencryptionState.mu.RUnlock()
	assert.Equal(t, "completed_with_errors", svc.reencryptionState.status)
}

func TestKMSService_RunReencryption_DefaultBatchSize(t *testing.T) {
	mockMgr := newMockKMSManager()
	svc := newKMSService(mockMgr, nil)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = true
	svc.reencryptionState.status = "running"
	svc.reencryptionState.processedKeys.Store(0)
	svc.reencryptionState.failedKeys.Store(0)
	svc.reencryptionState.mu.Unlock()

	keys := []interface{}{&kms.KeyMetadata{KeyID: "k1"}}
	req := &StartReencryptionRequest{BatchSize: 0}

	svc.runReencryption(systemTenantCtx(), req, keys)

	assert.Equal(t, uint64(1), svc.reencryptionState.processedKeys.Load())
}

// =============================================================================
// GetReencryptionProgress Tests
// =============================================================================

func TestKMSService_GetReencryptionProgress_Idle(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.GetReencryptionProgress(systemTenantCtx(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotNil(t, resp.Progress)
	assert.Equal(t, "idle", resp.Progress.Status)
	assert.False(t, resp.Progress.InProgress)
	assert.Equal(t, float64(0), resp.Progress.PercentComplete)
}

func TestKMSService_GetReencryptionProgress_InProgress(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	startedAt := time.Now().Add(-5 * time.Minute)
	estimatedCompletion := time.Now().Add(5 * time.Minute)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = true
	svc.reencryptionState.status = "running"
	svc.reencryptionState.totalKeys = 100
	svc.reencryptionState.processedKeys.Store(50)
	svc.reencryptionState.failedKeys.Store(2)
	svc.reencryptionState.startedAt = startedAt
	svc.reencryptionState.estimatedCompletion = estimatedCompletion
	svc.reencryptionState.operationID = "progress-op"
	svc.reencryptionState.mu.Unlock()

	resp, err := svc.GetReencryptionProgress(systemTenantCtx(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "running", resp.Progress.Status)
	assert.True(t, resp.Progress.InProgress)
	assert.Equal(t, uint64(100), resp.Progress.TotalKeys)
	assert.Equal(t, uint64(50), resp.Progress.ProcessedKeys)
	assert.Equal(t, uint64(2), resp.Progress.FailedKeys)
	assert.Equal(t, float64(50), resp.Progress.PercentComplete)
	assert.NotNil(t, resp.Progress.StartedAt)
	assert.NotNil(t, resp.Progress.EstimatedCompletion)
}

func TestKMSService_GetReencryptionProgress_CompletedNoStartTime(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = false
	svc.reencryptionState.status = "completed"
	svc.reencryptionState.totalKeys = 50
	svc.reencryptionState.processedKeys.Store(50)
	svc.reencryptionState.failedKeys.Store(0)
	svc.reencryptionState.mu.Unlock()

	resp, err := svc.GetReencryptionProgress(systemTenantCtx(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "completed", resp.Progress.Status)
	assert.Nil(t, resp.Progress.StartedAt)
	assert.Nil(t, resp.Progress.EstimatedCompletion)
}

func TestKMSService_GetReencryptionProgress_ZeroTotalKeys(t *testing.T) {
	mockMgr := newMockKMSManager()
	svc := newKMSService(mockMgr, nil)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.status = "running"
	svc.reencryptionState.totalKeys = 0
	svc.reencryptionState.processedKeys.Store(0)
	svc.reencryptionState.mu.Unlock()

	resp, err := svc.GetReencryptionProgress(systemTenantCtx(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, float64(0), resp.Progress.PercentComplete)
}

// =============================================================================
// CancelReencryption Tests
// =============================================================================

func TestKMSService_CancelReencryption_SuccessWithCancelFunc(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	cancelled := false
	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = true
	svc.reencryptionState.cancelFunc = func() { cancelled = true }
	svc.reencryptionState.mu.Unlock()

	err := svc.CancelReencryption(systemTenantCtx())
	require.NoError(t, err)
	assert.True(t, cancelled)
}

func TestKMSService_CancelReencryption_InProgressNilCancelFunc(t *testing.T) {
	mockMgr := newMockKMSManager()
	svc := newKMSService(mockMgr, nil)

	svc.reencryptionState.mu.Lock()
	svc.reencryptionState.inProgress = true
	svc.reencryptionState.cancelFunc = nil
	svc.reencryptionState.mu.Unlock()

	err := svc.CancelReencryption(systemTenantCtx())
	require.NoError(t, err)
}

// =============================================================================
// ListDEKs Tests
// =============================================================================

func TestKMSService_ListDEKs_NilRequest(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.ListDEKs(systemTenantCtx(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, len(resp.DEKs))
}

func TestKMSService_ListDEKs_WithPrefix(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	svc.dekStore.set("app/db", &dekEntry{
		metadata: &DEKMetadata{
			Path:      "app/db",
			Algorithm: KeyAlgAES256GCM,
			Version:   1,
			Status:    string(DEKStatusActive),
			CreatedAt: time.Now(),
		},
		wrappedKey: []byte("wrapped-key-1"),
	})
	svc.dekStore.set("app/cache", &dekEntry{
		metadata: &DEKMetadata{
			Path:      "app/cache",
			Algorithm: KeyAlgAES256GCM,
			Version:   1,
			Status:    string(DEKStatusActive),
			CreatedAt: time.Now(),
		},
		wrappedKey: []byte("wrapped-key-2"),
	})

	req := &ListDEKsRequest{Prefix: "app/", Limit: 10}
	resp, err := svc.ListDEKs(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, len(resp.DEKs))
}

func TestKMSService_ListDEKs_WithLimitTruncation(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	for i := 0; i < 5; i++ {
		path := "dek/" + string(rune('a'+i))
		svc.dekStore.set(path, &dekEntry{
			metadata: &DEKMetadata{
				Path:      path,
				Algorithm: KeyAlgAES256GCM,
				Version:   1,
				Status:    string(DEKStatusActive),
				CreatedAt: time.Now(),
			},
			wrappedKey: []byte("key"),
		})
	}

	req := &ListDEKsRequest{Prefix: "dek/", Limit: 3}
	resp, err := svc.ListDEKs(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.DEKs, 3)
	assert.True(t, resp.HasMore)
	assert.NotEmpty(t, resp.NextCursor)
}

func TestKMSService_ListDEKs_DefaultLimit(t *testing.T) {
	mockMgr := newMockKMSManager()
	svc := newKMSService(mockMgr, nil)

	req := &ListDEKsRequest{Prefix: "", Limit: 0}
	resp, err := svc.ListDEKs(systemTenantCtx(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.HasMore)
}

// =============================================================================
// BackendStatus Tests
// =============================================================================

func TestKMSService_BackendStatus_HealthyBackend(t *testing.T) {
	mockMgr := newMockKMSManager()
	logger := newMockLogger()
	svc := newKMSService(mockMgr, logger)

	resp, err := svc.BackendStatus(systemTenantCtx())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "healthy", resp.Status)
}

func TestKMSService_BackendStatus_NilManager(t *testing.T) {
	svc := &kmsServiceImpl{
		reencryptionState: &reencryptionState{status: "idle"},
		dekStore:          newDEKStore(),
		safetyTracker:     NewAEADSafetyTracker(),
	}

	resp, err := svc.BackendStatus(systemTenantCtx())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "not_initialized", resp.Status)
}
