//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Operations Management - Decode Error Tests
// =============================================================================

func TestHandleMigrateData_DecodeError(t *testing.T) {
	svc := &mockService{
		migrateDataFn: func(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
			return &service.MigrateDataResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	resp := handler.handleMigrateData(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleMigrateData_ServiceError(t *testing.T) {
	svc := &mockService{
		migrateDataFn: func(ctx context.Context, req *service.MigrateDataRequest) (*service.MigrateDataResponse, error) {
			return nil, errors.New("migration failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.MigrateDataRequest{})
	resp := handler.handleMigrateData(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleGetMigrationProgress_DecodeError(t *testing.T) {
	svc := &mockService{
		getMigrationProgressFn: func(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
			return &service.GetMigrationProgressResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	resp := handler.handleGetMigrationProgress(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleGetMigrationProgress_ServiceError(t *testing.T) {
	svc := &mockService{
		getMigrationProgressFn: func(ctx context.Context, req *service.GetMigrationProgressRequest) (*service.GetMigrationProgressResponse, error) {
			return nil, errors.New("progress check failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetMigrationProgressRequest{})
	resp := handler.handleGetMigrationProgress(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleReplaceNode_DecodeError(t *testing.T) {
	svc := &mockService{
		replaceNodeFn: func(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
			return &service.ReplaceNodeResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	resp := handler.handleReplaceNode(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleReplaceNode_ServiceError(t *testing.T) {
	svc := &mockService{
		replaceNodeFn: func(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
			return nil, errors.New("replace failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ReplaceNodeRequest{})
	resp := handler.handleReplaceNode(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleUpgradeNode_DecodeError(t *testing.T) {
	svc := &mockService{
		upgradeNodeFn: func(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
			return &service.UpgradeNodeResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	resp := handler.handleUpgradeNode(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleUpgradeNode_ServiceError(t *testing.T) {
	svc := &mockService{
		upgradeNodeFn: func(ctx context.Context, req *service.UpgradeNodeRequest) (*service.UpgradeNodeResponse, error) {
			return nil, errors.New("upgrade failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.UpgradeNodeRequest{})
	resp := handler.handleUpgradeNode(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleGetUpgradeStatus_DecodeError(t *testing.T) {
	svc := &mockService{
		getUpgradeStatusFn: func(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
			return &service.GetUpgradeStatusResponse{}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	resp := handler.handleGetUpgradeStatus(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleGetUpgradeStatus_ServiceError(t *testing.T) {
	svc := &mockService{
		getUpgradeStatusFn: func(ctx context.Context, req *service.GetUpgradeStatusRequest) (*service.GetUpgradeStatusResponse, error) {
			return nil, errors.New("status check failed")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.GetUpgradeStatusRequest{})
	resp := handler.handleGetUpgradeStatus(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// OS Admin Handler - Missing Decode and Service Error Tests
// =============================================================================

func TestOSAdminSetBackendStatus_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	resp := h.handleOSAdminSetBackendStatus(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("expected error for invalid payload")
	}
}

func TestOSAdminSetBackendStatus_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		setBackendStatusFn: func(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
			return nil, errors.New("status update failed")
		},
	}
	h := createHandlerWithOSAdmin(svc)
	payload, _ := EncodePayload(&service.SetBackendStatusRequest{Name: "b1"})
	resp := h.handleOSAdminSetBackendStatus(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error from service")
	}
}

func TestOSAdminGetStorageStats_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	resp := h.handleOSAdminGetStorageStats(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("expected error for invalid payload")
	}
}

func TestOSAdminGetStorageStats_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		getStorageStatsFn: func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
			return nil, errors.New("stats failed")
		},
	}
	h := createHandlerWithOSAdmin(svc)
	payload, _ := EncodePayload(&service.GetStorageStatsRequest{})
	resp := h.handleOSAdminGetStorageStats(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error from service")
	}
}

func TestOSAdminGetGCProgress_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	resp := h.handleOSAdminGetGCProgress(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("expected error for invalid payload")
	}
}

func TestOSAdminGetGCProgress_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		getGCProgressFn: func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
			return nil, errors.New("gc progress failed")
		},
	}
	h := createHandlerWithOSAdmin(svc)
	payload, _ := EncodePayload(&service.GetGarbageCollectionProgressRequest{OperationID: "gc-1"})
	resp := h.handleOSAdminGetGCProgress(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error from service")
	}
}

func TestOSAdminRepairObject_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	resp := h.handleOSAdminRepairObject(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("expected error for invalid payload")
	}
}

func TestOSAdminRepairObject_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		repairObjectFn: func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
			return nil, errors.New("repair failed")
		},
	}
	h := createHandlerWithOSAdmin(svc)
	payload, _ := EncodePayload(&service.RepairObjectRequest{Key: "obj-1"})
	resp := h.handleOSAdminRepairObject(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error from service")
	}
}

func TestOSAdminCompactBackend_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	resp := h.handleOSAdminCompactBackend(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("expected error for invalid payload")
	}
}

func TestOSAdminCompactBackend_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		compactBackendFn: func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
			return nil, errors.New("compaction failed")
		},
	}
	h := createHandlerWithOSAdmin(svc)
	payload, _ := EncodePayload(&service.CompactBackendRequest{Name: "b1"})
	resp := h.handleOSAdminCompactBackend(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error from service")
	}
}

func TestOSAdminUpdateBackend_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		updateBackendFn: func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
			return nil, errors.New("update failed")
		},
	}
	h := createHandlerWithOSAdmin(svc)
	payload, _ := EncodePayload(&service.UpdateBackendRequest{Name: "b1"})
	resp := h.handleOSAdminUpdateBackend(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error from service")
	}
}

func TestOSAdminDeleteBackend_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		deleteBackendFn: func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
			return nil, errors.New("delete failed")
		},
	}
	h := createHandlerWithOSAdmin(svc)
	payload, _ := EncodePayload(&service.DeleteBackendRequest{Name: "b1"})
	resp := h.handleOSAdminDeleteBackend(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error from service")
	}
}

func TestOSAdminRunGC_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	resp := h.handleOSAdminRunGC(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("expected error for invalid payload")
	}
}

// =============================================================================
// Cluster Management - Additional Branch Tests
// =============================================================================

func TestHandleRebalanceStatus_EmptyPayload(t *testing.T) {
	handler := createTestHandler()
	resp := handler.handleRebalanceStatus(context.Background(), &Request{Payload: []byte("{}")})
	// Empty operation_id should return default not_running status
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK for empty operation_id, got %d", resp.Status)
	}
}

func TestHandleRebalanceStatus_WithOperationID(t *testing.T) {
	svc := &mockService{
		getRebalanceProgressFn: func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
			return &service.GetRebalanceProgressResponse{
				OperationID: req.OperationID,
				Status:      "in_progress",
			}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(map[string]string{"operation_id": "op-123"})
	resp := handler.handleRebalanceStatus(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleRebalanceStatus_InvalidJSON(t *testing.T) {
	handler := createTestHandler()
	resp := handler.handleRebalanceStatus(context.Background(), &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid JSON")
	}
}

func TestHandleRebalanceStatus_ServiceError(t *testing.T) {
	svc := &mockService{
		getRebalanceProgressFn: func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
			return nil, errors.New("service error")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(map[string]string{"operation_id": "op-123"})
	resp := handler.handleRebalanceStatus(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleNodeReplaceRequest_NilService(t *testing.T) {
	handler := NewHandler(nil, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ReplaceNodeRequest{})
	resp := handler.handleNodeReplaceRequest(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for nil service")
	}
}

// =============================================================================
// Scheduler Handler - Missing Error Tests
// =============================================================================

func TestHandleSchedulerUpdateJob_DecodeError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload := append([]byte{SubOpSchedulerUpdateJob}, []byte("{bad")...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleSchedulerDeleteJob_DecodeError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload := append([]byte{SubOpSchedulerDeleteJob}, []byte("{bad")...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleSchedulerListJobs_DecodeError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload := append([]byte{SubOpSchedulerListJobs}, []byte("{bad")...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleSchedulerTriggerJob_DecodeError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload := append([]byte{SubOpSchedulerTriggerJob}, []byte("{bad")...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleSchedulerPauseJob_DecodeError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload := append([]byte{SubOpSchedulerPauseJob}, []byte("{bad")...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleSchedulerResumeJob_DecodeError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload := append([]byte{SubOpSchedulerResumeJob}, []byte("{bad")...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleSchedulerCancelJob_DecodeError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload := append([]byte{SubOpSchedulerCancelJob}, []byte("{bad")...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleSchedulerGetJobHistory_DecodeError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload := append([]byte{SubOpSchedulerGetJobHistory}, []byte("{bad")...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error for invalid payload")
	}
}

func TestHandleSchedulerPauseJob_ServiceError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{
			pauseJobFn: func(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
				return nil, errors.New("pause failed")
			},
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	jobPayload, _ := EncodePayload(&service.PauseSchedulerJobRequest{JobID: 1})
	payload := append([]byte{SubOpSchedulerPauseJob}, jobPayload...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleSchedulerResumeJob_ServiceError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{
			resumeJobFn: func(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
				return nil, errors.New("resume failed")
			},
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	jobPayload, _ := EncodePayload(&service.ResumeSchedulerJobRequest{JobID: 1})
	payload := append([]byte{SubOpSchedulerResumeJob}, jobPayload...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

func TestHandleSchedulerCancelJob_ServiceError(t *testing.T) {
	svc := &mockServiceWithScheduler{
		schedulerSvc: &mockSchedulerService{
			cancelJobFn: func(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
				return nil, errors.New("cancel failed")
			},
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	jobPayload, _ := EncodePayload(&service.CancelSchedulerJobRequest{JobID: 1})
	payload := append([]byte{SubOpSchedulerCancelJob}, jobPayload...)
	resp := handler.handleSchedulerDispatch(context.Background(), &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Cluster Management - Additional Key Variant and NotFound Tests
// =============================================================================

// TestHandleRebalanceStatus_OperationIDPascalCase verifies that
// handleRebalanceStatus recognizes the PascalCase "OperationID" key variant.
func TestHandleRebalanceStatus_OperationIDPascalCase(t *testing.T) {
	svc := &mockService{
		getRebalanceProgressFn: func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
			return &service.GetRebalanceProgressResponse{
				OperationID: req.OperationID,
				Status:      "completed",
			}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(map[string]string{"OperationID": "op-pascal"})
	resp := handler.handleRebalanceStatus(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK for PascalCase OperationID, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// TestHandleRebalanceStatus_JobIDVariant verifies that handleRebalanceStatus
// recognizes the "JobID" key variant.
func TestHandleRebalanceStatus_JobIDVariant(t *testing.T) {
	svc := &mockService{
		getRebalanceProgressFn: func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
			return &service.GetRebalanceProgressResponse{
				OperationID: req.OperationID,
				Status:      "in_progress",
			}, nil
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(map[string]string{"JobID": "job-123"})
	resp := handler.handleRebalanceStatus(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK for JobID variant, got %d: %s", resp.Status, string(resp.Payload))
	}
}

// TestHandleRebalanceStatus_NotFoundError verifies that handleRebalanceStatus
// returns a not_running response for a NotFound error from the service.
func TestHandleRebalanceStatus_NotFoundError(t *testing.T) {
	svc := &mockService{
		getRebalanceProgressFn: func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
			return nil, dragonerrors.NotFound("test", "operation")
		},
	}
	handler := NewHandler(svc, nil, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(map[string]string{"operation_id": "nonexistent"})
	resp := handler.handleRebalanceStatus(context.Background(), &Request{Payload: payload})
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK with not_running for NotFound, got %d", resp.Status)
	}
}
