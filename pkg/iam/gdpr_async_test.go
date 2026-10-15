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

package iam

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// completionWriteError is the storage failure injected on the write that
// records a request as completed.
type completionWriteError struct{}

func (completionWriteError) Error() string { return "injected completion write failure" }

// completionFailingExportStorage refuses the write that marks an export
// completed, which GenerateExport cannot record on the request itself.
type completionFailingExportStorage struct {
	DataExportStorage
}

func (s *completionFailingExportStorage) UpdateExportRequest(ctx context.Context, req *DataExportRequest) error {
	if req.Status == DataExportStatusCompleted {
		return completionWriteError{}
	}
	return s.DataExportStorage.UpdateExportRequest(ctx, req)
}

// completionFailingDeletionStorage refuses the write that marks a deletion
// completed, which ProcessDeletion cannot record on the request itself.
type completionFailingDeletionStorage struct {
	DeletionStorage
}

func (s *completionFailingDeletionStorage) UpdateDeletionRequest(ctx context.Context, req *DataDeletionRequest) error {
	if req.Status == DeletionStatusCompleted {
		return completionWriteError{}
	}
	return s.DeletionStorage.UpdateDeletionRequest(ctx, req)
}

func newCompletionFailingGDPRManager(t *testing.T) *GDPRManager {
	t.Helper()

	manager, err := NewGDPRManager(GDPRManagerDependencies{
		ConsentStorage:    NewMemoryConsentStorage(),
		DataExportStorage: &completionFailingExportStorage{DataExportStorage: NewMemoryDataExportStorage()},
		DeletionStorage:   &completionFailingDeletionStorage{DeletionStorage: NewMemoryDeletionStorage()},
		UserStorage:       NewMemoryStorage(),
		SessionStorage:    NewMemorySessionStorage(),
	})
	require.NoError(t, err)
	return manager
}

func TestProcessExportAsync_UnrecordedErrorMarksExportFailed(t *testing.T) {
	manager := newCompletionFailingGDPRManager(t)
	ctx := context.Background()
	createTestUser(t, manager.deps.UserStorage, "tenant-async-export", 51)

	request, err := manager.deps.DataExportStorage.CreateExportRequest(ctx, CreateDataExportOptions{
		UserID: 51, TenantID: "tenant-async-export", RequestedBy: "51", Format: DataExportFormatJSON,
	})
	require.NoError(t, err)

	manager.processExportAsync(request.ID)

	updated, err := manager.GetExportStatus(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, DataExportStatusFailed, updated.Status)
	assert.Contains(t, updated.ErrorMessage, "injected completion write failure")
	assert.NotNil(t, updated.CompletedAt)
}

func TestMarkExportFailed_KeepsFinalStatus(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	request, err := manager.deps.DataExportStorage.CreateExportRequest(ctx, CreateDataExportOptions{
		UserID: 52, TenantID: "tenant-final-export", RequestedBy: "52", Format: DataExportFormatJSON,
	})
	require.NoError(t, err)
	request.Status = DataExportStatusCompleted
	require.NoError(t, manager.deps.DataExportStorage.UpdateExportRequest(ctx, request))

	manager.markExportFailed(request.ID, "must not overwrite")

	updated, err := manager.GetExportStatus(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, DataExportStatusCompleted, updated.Status)
	assert.Empty(t, updated.ErrorMessage)
}

func TestProcessDeletionAsync_UnrecordedErrorMarksDeletionFailed(t *testing.T) {
	manager := newCompletionFailingGDPRManager(t)
	ctx := context.Background()
	createTestUser(t, manager.deps.UserStorage, "tenant-async-deletion", 53)

	request, err := manager.deps.DeletionStorage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		UserID: 53, TenantID: "tenant-async-deletion", RequestedBy: "53",
	})
	require.NoError(t, err)

	manager.processDeletionAsync(request.ID)

	updated, err := manager.deps.DeletionStorage.GetDeletionRequest(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, DeletionStatusFailed, updated.Status)
	assert.Contains(t, updated.ErrorMessage, "injected completion write failure")
	assert.NotNil(t, updated.CompletedAt)
}

func TestMarkDeletionFailed_KeepsCancelledStatus(t *testing.T) {
	manager, _ := createTestGDPRManager(t)
	ctx := context.Background()

	request, err := manager.deps.DeletionStorage.CreateDeletionRequest(ctx, CreateDeletionRequestOptions{
		UserID: 54, TenantID: "tenant-cancelled-deletion", RequestedBy: "54",
	})
	require.NoError(t, err)
	request.Status = DeletionStatusCancelled
	require.NoError(t, manager.deps.DeletionStorage.UpdateDeletionRequest(ctx, request))

	manager.markDeletionFailed(request.ID, "must not overwrite")

	updated, err := manager.deps.DeletionStorage.GetDeletionRequest(ctx, request.ID)
	require.NoError(t, err)
	assert.Equal(t, DeletionStatusCancelled, updated.Status)
	assert.Empty(t, updated.ErrorMessage)
}
