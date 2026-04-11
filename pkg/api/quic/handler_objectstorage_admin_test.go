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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock ObjectStorageAdminService
// =============================================================================

type mockOSAdminService struct {
	createBackendFn    func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error)
	updateBackendFn    func(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error)
	deleteBackendFn    func(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error)
	setBackendStatusFn func(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error)
	getStorageStatsFn  func(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error)
	runGCFn            func(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error)
	getGCProgressFn    func(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error)
	repairObjectFn     func(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error)
	compactBackendFn   func(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error)
}

func (m *mockOSAdminService) CreateBackend(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
	if m.createBackendFn != nil {
		return m.createBackendFn(ctx, req)
	}
	return &service.CreateBackendResponse{Name: req.Name, CreatedAt: time.Now().UnixNano()}, nil
}

func (m *mockOSAdminService) UpdateBackend(ctx context.Context, req *service.UpdateBackendRequest) (*service.UpdateBackendResponse, error) {
	if m.updateBackendFn != nil {
		return m.updateBackendFn(ctx, req)
	}
	return &service.UpdateBackendResponse{Name: req.Name, UpdatedAt: time.Now().UnixNano()}, nil
}

func (m *mockOSAdminService) DeleteBackend(ctx context.Context, req *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
	if m.deleteBackendFn != nil {
		return m.deleteBackendFn(ctx, req)
	}
	return &service.DeleteBackendResponse{Name: req.Name, Deleted: true}, nil
}

func (m *mockOSAdminService) SetBackendStatus(ctx context.Context, req *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
	if m.setBackendStatusFn != nil {
		return m.setBackendStatusFn(ctx, req)
	}
	return &service.SetBackendStatusResponse{Name: req.Name}, nil
}

func (m *mockOSAdminService) GetStorageStats(ctx context.Context, req *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
	if m.getStorageStatsFn != nil {
		return m.getStorageStatsFn(ctx, req)
	}
	return &service.GetStorageStatsResponse{}, nil
}

func (m *mockOSAdminService) RunGarbageCollection(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
	if m.runGCFn != nil {
		return m.runGCFn(ctx, req)
	}
	return &service.RunGarbageCollectionResponse{OperationID: "gc-1"}, nil
}

func (m *mockOSAdminService) GetGarbageCollectionProgress(ctx context.Context, req *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
	if m.getGCProgressFn != nil {
		return m.getGCProgressFn(ctx, req)
	}
	return &service.GetGarbageCollectionProgressResponse{OperationID: req.OperationID}, nil
}

func (m *mockOSAdminService) RepairObject(ctx context.Context, req *service.RepairObjectRequest) (*service.RepairObjectResponse, error) {
	if m.repairObjectFn != nil {
		return m.repairObjectFn(ctx, req)
	}
	return &service.RepairObjectResponse{Key: req.Key, Success: true}, nil
}

func (m *mockOSAdminService) CompactBackend(ctx context.Context, req *service.CompactBackendRequest) (*service.CompactBackendResponse, error) {
	if m.compactBackendFn != nil {
		return m.compactBackendFn(ctx, req)
	}
	return &service.CompactBackendResponse{Name: req.Name, OperationID: "compact-1"}, nil
}

// mockServiceWithOSAdmin embeds mockService and returns an OS admin service.
type mockServiceWithOSAdmin struct {
	mockService
	osAdminSvc service.ObjectStorageAdminService
}

func (m *mockServiceWithOSAdmin) ObjectStorageAdminService() service.ObjectStorageAdminService {
	return m.osAdminSvc
}

func createHandlerWithOSAdmin(adminSvc service.ObjectStorageAdminService) *Handler {
	svc := &mockServiceWithOSAdmin{osAdminSvc: adminSvc}
	return NewHandler(svc, nil, nil, nil, nil, createTestLogger())
}

// =============================================================================
// OS Admin Dispatch Tests
// =============================================================================

func TestOSAdminDispatch_MissingSubOpcode(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	ctx := context.Background()

	resp := h.handleOSAdminDispatch(ctx, &Request{Payload: []byte{}})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for missing sub-opcode")
	}
}

func TestOSAdminDispatch_UnknownSubOpcode(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	ctx := context.Background()

	resp := h.handleOSAdminDispatch(ctx, &Request{Payload: []byte{0xFF}})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for unknown sub-opcode")
	}
}

func TestOSAdminDispatch_AllSubOpcodes_Success(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	ctx := context.Background()

	tests := []struct {
		name      string
		subOpcode byte
		payload   interface{}
	}{
		{"Create", SubOpOSAdminCreate, &service.CreateBackendRequest{Name: "b1"}},
		{"Update", SubOpOSAdminUpdate, &service.UpdateBackendRequest{Name: "b1"}},
		{"Delete", SubOpOSAdminDelete, &service.DeleteBackendRequest{Name: "b1"}},
		{"SetStatus", SubOpOSAdminSetStatus, &service.SetBackendStatusRequest{Name: "b1"}},
		{"GetStats", SubOpOSAdminGetStats, &service.GetStorageStatsRequest{}},
		{"RunGC", SubOpOSAdminRunGC, &service.RunGarbageCollectionRequest{}},
		{"GetGC", SubOpOSAdminGetGC, &service.GetGarbageCollectionProgressRequest{OperationID: "gc-1"}},
		{"Repair", SubOpOSAdminRepair, &service.RepairObjectRequest{Key: "obj-1"}},
		{"Compact", SubOpOSAdminCompact, &service.CompactBackendRequest{Name: "b1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payloadBytes, err := EncodePayload(tt.payload)
			if err != nil {
				t.Fatalf("Failed to encode: %v", err)
			}
			fullPayload := append([]byte{tt.subOpcode}, payloadBytes...)
			resp := h.handleOSAdminDispatch(ctx, &Request{Payload: fullPayload})
			if resp.Status != StatusOK {
				t.Fatalf("Expected StatusOK for %s, got %d", tt.name, resp.Status)
			}
		})
	}
}

// =============================================================================
// OS Admin Handler - Service Unavailable Tests
// =============================================================================

func TestOSAdminCreateBackend_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.CreateBackendRequest{Name: "b1"})
	resp := h.handleOSAdminCreateBackend(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

func TestOSAdminUpdateBackend_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.UpdateBackendRequest{Name: "b1"})
	resp := h.handleOSAdminUpdateBackend(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

func TestOSAdminDeleteBackend_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.DeleteBackendRequest{Name: "b1"})
	resp := h.handleOSAdminDeleteBackend(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

func TestOSAdminSetBackendStatus_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.SetBackendStatusRequest{Name: "b1"})
	resp := h.handleOSAdminSetBackendStatus(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

func TestOSAdminGetStorageStats_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.GetStorageStatsRequest{})
	resp := h.handleOSAdminGetStorageStats(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

func TestOSAdminRunGC_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.RunGarbageCollectionRequest{})
	resp := h.handleOSAdminRunGC(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

func TestOSAdminGetGCProgress_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.GetGarbageCollectionProgressRequest{OperationID: "gc-1"})
	resp := h.handleOSAdminGetGCProgress(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

func TestOSAdminRepairObject_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.RepairObjectRequest{Key: "obj-1"})
	resp := h.handleOSAdminRepairObject(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

func TestOSAdminCompactBackend_ServiceUnavailable(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&service.CompactBackendRequest{Name: "b1"})
	resp := h.handleOSAdminCompactBackend(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error when admin service is nil")
	}
}

// =============================================================================
// OS Admin Handler - Decode Error Tests
// =============================================================================

func TestOSAdminCreateBackend_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	ctx := context.Background()

	resp := h.handleOSAdminCreateBackend(ctx, &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestOSAdminUpdateBackend_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	ctx := context.Background()

	resp := h.handleOSAdminUpdateBackend(ctx, &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

func TestOSAdminDeleteBackend_DecodeError(t *testing.T) {
	h := createHandlerWithOSAdmin(&mockOSAdminService{})
	ctx := context.Background()

	resp := h.handleOSAdminDeleteBackend(ctx, &Request{Payload: []byte("{bad")})
	if resp.Status == StatusOK {
		t.Fatal("Expected error for invalid payload")
	}
}

// =============================================================================
// OS Admin Handler - Service Error Tests
// =============================================================================

func TestOSAdminCreateBackend_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		createBackendFn: func(ctx context.Context, req *service.CreateBackendRequest) (*service.CreateBackendResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrAlreadyExists, Op: "test", Err: &errors.MessageError{Message: "exists"}}
		},
	}
	h := createHandlerWithOSAdmin(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.CreateBackendRequest{Name: "b1"})
	resp := h.handleOSAdminCreateBackend(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}

func TestOSAdminRunGC_ServiceError(t *testing.T) {
	svc := &mockOSAdminService{
		runGCFn: func(ctx context.Context, req *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "test", Err: &errors.MessageError{Message: "gc failed"}}
		},
	}
	h := createHandlerWithOSAdmin(svc)
	ctx := context.Background()

	payload, _ := EncodePayload(&service.RunGarbageCollectionRequest{})
	resp := h.handleOSAdminRunGC(ctx, &Request{Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("Expected error from service")
	}
}
