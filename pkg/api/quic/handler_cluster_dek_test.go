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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock ClusterDEK Service for Testing
// =============================================================================

// mockClusterDEKService implements the service.ClusterDEKService interface for testing.
type mockClusterDEKService struct {
	initializeClusterDEKFn  func(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error)
	getClusterDEKStatusFn   func(ctx context.Context) (*service.ClusterDEKStatusResponse, error)
	rotateClusterDEKFn      func(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error)
	syncNodeDEKFn           func(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error)
	getNodeDEKStatusFn      func(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error)
	startReencryptionFn     func(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error)
	getReencryptionStatusFn func(ctx context.Context) (*service.ReencryptionStatusResponse, error)
	cancelReencryptionFn    func(ctx context.Context) error
	recoverDEKFn            func(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error)
}

func (m *mockClusterDEKService) InitializeClusterDEK(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error) {
	if m.initializeClusterDEKFn != nil {
		return m.initializeClusterDEKFn(ctx, req)
	}
	return &service.InitClusterDEKResponse{
		Version:     1,
		CreatedAt:   time.Now(),
		NodesSynced: 3,
		Algorithm:   req.Algorithm,
		KeySize:     req.KeySize,
	}, nil
}

func (m *mockClusterDEKService) GetClusterDEKStatus(ctx context.Context) (*service.ClusterDEKStatusResponse, error) {
	if m.getClusterDEKStatusFn != nil {
		return m.getClusterDEKStatusFn(ctx)
	}
	return &service.ClusterDEKStatusResponse{
		Initialized:            true,
		CurrentVersion:         1,
		CreatedAt:              time.Now(),
		NodesSynced:            3,
		TotalNodes:             3,
		Algorithm:              "AES-256-GCM",
		KeySize:                256,
		ReencryptionInProgress: false,
	}, nil
}

func (m *mockClusterDEKService) RotateClusterDEK(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error) {
	if m.rotateClusterDEKFn != nil {
		return m.rotateClusterDEKFn(ctx, req)
	}
	return &service.RotateClusterDEKResponse{
		NewVersion:          2,
		OldVersion:          1,
		RotatedAt:           time.Now(),
		RotationID:          "rot-1-2",
		NodesSynced:         3,
		ReencryptionStarted: req.AutoStartReencryption,
	}, nil
}

func (m *mockClusterDEKService) SyncNodeDEK(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error) {
	if m.syncNodeDEKFn != nil {
		return m.syncNodeDEKFn(ctx, req)
	}
	return &service.SyncNodeDEKResponse{
		NodeID:         req.NodeID,
		VersionsSynced: []uint64{1, 2},
		CurrentVersion: 2,
		SyncedAt:       time.Now(),
	}, nil
}

func (m *mockClusterDEKService) GetNodeDEKStatus(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error) {
	if m.getNodeDEKStatusFn != nil {
		return m.getNodeDEKStatusFn(ctx, req)
	}
	return &service.GetNodeDEKStatusResponse{
		NodeID:            req.NodeID,
		NodeName:          "node-1",
		CurrentVersion:    2,
		LastSyncedAt:      time.Now(),
		Status:            service.NodeDEKSynced,
		VersionsAvailable: []uint64{1, 2},
	}, nil
}

func (m *mockClusterDEKService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	if m.startReencryptionFn != nil {
		return m.startReencryptionFn(ctx, req)
	}
	return &service.StartReencryptionResponse{
		OperationID:         "reenc-1-2-123456",
		Started:             true,
		StartedAt:           time.Now(),
		TotalEntities:       10000,
		EstimatedCompletion: time.Now().Add(1 * time.Hour),
	}, nil
}

func (m *mockClusterDEKService) GetReencryptionStatus(ctx context.Context) (*service.ReencryptionStatusResponse, error) {
	if m.getReencryptionStatusFn != nil {
		return m.getReencryptionStatusFn(ctx)
	}
	return &service.ReencryptionStatusResponse{
		OperationID:       "reenc-1-2-123456",
		State:             service.ReencryptionRunning,
		StartedAt:         time.Now().Add(-30 * time.Minute),
		Progress:          50.0,
		TotalEntities:     10000,
		ProcessedEntities: 5000,
		FailedEntities:    0,
		ThroughputRate:    166.7,
	}, nil
}

func (m *mockClusterDEKService) CancelReencryption(ctx context.Context) error {
	if m.cancelReencryptionFn != nil {
		return m.cancelReencryptionFn(ctx)
	}
	return nil
}

func (m *mockClusterDEKService) RecoverDEK(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error) {
	if m.recoverDEKFn != nil {
		return m.recoverDEKFn(ctx, req)
	}
	return &service.RecoverDEKResponse{
		Success:          true,
		RecoveredVersion: 2,
		RecoveredAt:      time.Now(),
		NodesSynced:      3,
		AuditID:          "recovery-2-123456",
	}, nil
}

// errorClusterDEKService always returns errors for testing error paths.
type errorClusterDEKService struct{}

func (e *errorClusterDEKService) InitializeClusterDEK(ctx context.Context, req *service.InitClusterDEKRequest) (*service.InitClusterDEKResponse, error) {
	return nil, errors.New("initialization failed")
}

func (e *errorClusterDEKService) GetClusterDEKStatus(ctx context.Context) (*service.ClusterDEKStatusResponse, error) {
	return nil, errors.New("status retrieval failed")
}

func (e *errorClusterDEKService) RotateClusterDEK(ctx context.Context, req *service.RotateClusterDEKRequest) (*service.RotateClusterDEKResponse, error) {
	return nil, errors.New("rotation failed")
}

func (e *errorClusterDEKService) SyncNodeDEK(ctx context.Context, req *service.SyncNodeDEKRequest) (*service.SyncNodeDEKResponse, error) {
	return nil, errors.New("sync failed")
}

func (e *errorClusterDEKService) GetNodeDEKStatus(ctx context.Context, req *service.GetNodeDEKStatusRequest) (*service.GetNodeDEKStatusResponse, error) {
	return nil, errors.New("node status retrieval failed")
}

func (e *errorClusterDEKService) StartReencryption(ctx context.Context, req *service.StartReencryptionRequest) (*service.StartReencryptionResponse, error) {
	return nil, errors.New("reencryption start failed")
}

func (e *errorClusterDEKService) GetReencryptionStatus(ctx context.Context) (*service.ReencryptionStatusResponse, error) {
	return nil, errors.New("reencryption status retrieval failed")
}

func (e *errorClusterDEKService) CancelReencryption(ctx context.Context) error {
	return errors.New("cancel reencryption failed")
}

func (e *errorClusterDEKService) RecoverDEK(ctx context.Context, req *service.RecoverDEKRequest) (*service.RecoverDEKResponse, error) {
	return nil, errors.New("recovery failed")
}

// =============================================================================
// Helper Functions
// =============================================================================

func createClusterDEKTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-cluster-dek-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createClusterDEKTestHandler(clusterDEKSvc service.ClusterDEKService) *Handler {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createClusterDEKTestLogger())
	if clusterDEKSvc != nil {
		handler.clusterDEKService = clusterDEKSvc
	}
	return handler
}

// =============================================================================
// handleClusterDEKInit Tests
// =============================================================================

func TestHandleClusterDEKInit_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.InitClusterDEKRequest{
		Algorithm: "AES-256-GCM",
		KeySize:   256,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKInit,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKInit_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	payload, _ := EncodePayload(&service.InitClusterDEKRequest{
		Algorithm: "AES-256-GCM",
		KeySize:   256,
	})
	req := &Request{
		Opcode:  OpClusterDEKInit,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKInit_DecodeError(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKInit,
		Payload: []byte("invalid payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleClusterDEKInit_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, _ := EncodePayload(&service.InitClusterDEKRequest{
		Algorithm: "AES-256-GCM",
		KeySize:   256,
	})
	req := &Request{
		Opcode:  OpClusterDEKInit,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleClusterDEKStatus Tests
// =============================================================================

func TestHandleClusterDEKStatus_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKStatus_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	req := &Request{
		Opcode:  OpClusterDEKStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKStatus_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleClusterDEKRotate Tests
// =============================================================================

func TestHandleClusterDEKRotate_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.RotateClusterDEKRequest{
		Reason:                "scheduled rotation",
		AutoStartReencryption: true,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKRotate_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	payload, _ := EncodePayload(&service.RotateClusterDEKRequest{
		Reason: "scheduled rotation",
	})
	req := &Request{
		Opcode:  OpClusterDEKRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKRotate_DecodeError(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKRotate,
		Payload: []byte("invalid rotate payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleClusterDEKRotate_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, _ := EncodePayload(&service.RotateClusterDEKRequest{
		Reason: "scheduled rotation",
	})
	req := &Request{
		Opcode:  OpClusterDEKRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleClusterDEKSyncNode Tests
// =============================================================================

func TestHandleClusterDEKSyncNode_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.SyncNodeDEKRequest{
		NodeID:         1,
		CurrentVersion: 1,
		ForceResync:    false,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKSyncNode,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKSyncNode_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	payload, _ := EncodePayload(&service.SyncNodeDEKRequest{
		NodeID: 1,
	})
	req := &Request{
		Opcode:  OpClusterDEKSyncNode,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKSyncNode_DecodeError(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKSyncNode,
		Payload: []byte("invalid sync payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleClusterDEKSyncNode_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, _ := EncodePayload(&service.SyncNodeDEKRequest{
		NodeID: 1,
	})
	req := &Request{
		Opcode:  OpClusterDEKSyncNode,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleClusterDEKNodeStatus Tests
// =============================================================================

func TestHandleClusterDEKNodeStatus_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.GetNodeDEKStatusRequest{
		NodeID: 1,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKNodeStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKNodeStatus_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	payload, _ := EncodePayload(&service.GetNodeDEKStatusRequest{
		NodeID: 1,
	})
	req := &Request{
		Opcode:  OpClusterDEKNodeStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKNodeStatus_DecodeError(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKNodeStatus,
		Payload: []byte("invalid status payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleClusterDEKNodeStatus_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, _ := EncodePayload(&service.GetNodeDEKStatusRequest{
		NodeID: 1,
	})
	req := &Request{
		Opcode:  OpClusterDEKNodeStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleClusterDEKStartReencrypt Tests
// =============================================================================

func TestHandleClusterDEKStartReencrypt_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.StartReencryptionRequest{
		FromVersion: 1,
		ToVersion:   2,
		BatchSize:   100,
		Concurrency: 4,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKStartReencrypt,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKStartReencrypt_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	payload, _ := EncodePayload(&service.StartReencryptionRequest{
		FromVersion: 1,
		ToVersion:   2,
	})
	req := &Request{
		Opcode:  OpClusterDEKStartReencrypt,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKStartReencrypt_DecodeError(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKStartReencrypt,
		Payload: []byte("invalid reencrypt payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleClusterDEKStartReencrypt_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, _ := EncodePayload(&service.StartReencryptionRequest{
		FromVersion: 1,
		ToVersion:   2,
	})
	req := &Request{
		Opcode:  OpClusterDEKStartReencrypt,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleClusterDEKReencryptStatus Tests
// =============================================================================

func TestHandleClusterDEKReencryptStatus_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKReencryptStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKReencryptStatus_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	req := &Request{
		Opcode:  OpClusterDEKReencryptStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKReencryptStatus_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKReencryptStatus,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleClusterDEKCancelReencrypt Tests
// =============================================================================

func TestHandleClusterDEKCancelReencrypt_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKCancelReencrypt,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKCancelReencrypt_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	req := &Request{
		Opcode:  OpClusterDEKCancelReencrypt,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKCancelReencrypt_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKCancelReencrypt,
		Payload: []byte{},
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleClusterDEKRecover Tests
// =============================================================================

func TestHandleClusterDEKRecover_Success(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.RecoverDEKRequest{
		RecoveryMethod: service.RecoveryMethodBackup,
		BackupLocation: "/backup/dek/v2",
		Reason:         "disaster recovery test",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKRecover,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKRecover_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	payload, _ := EncodePayload(&service.RecoverDEKRequest{
		RecoveryMethod: service.RecoveryMethodBackup,
		BackupLocation: "/backup/dek/v2",
		Reason:         "disaster recovery",
	})
	req := &Request{
		Opcode:  OpClusterDEKRecover,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil cluster DEK service")
	}
}

func TestHandleClusterDEKRecover_DecodeError(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKRecover,
		Payload: []byte("invalid recover payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleClusterDEKRecover_ServiceError(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, _ := EncodePayload(&service.RecoverDEKRequest{
		RecoveryMethod: service.RecoveryMethodBackup,
		BackupLocation: "/backup/dek/v2",
		Reason:         "disaster recovery",
	})
	req := &Request{
		Opcode:  OpClusterDEKRecover,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Table-Driven Tests for All ClusterDEK Operations
// =============================================================================

func TestHandleClusterDEKOperations_AllOpcodes(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	tests := []struct {
		name       string
		opcode     byte
		payload    interface{}
		hasPayload bool
	}{
		{
			name:   "ClusterDEKInit",
			opcode: OpClusterDEKInit,
			payload: &service.InitClusterDEKRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
			},
			hasPayload: true,
		},
		{
			name:       "ClusterDEKStatus",
			opcode:     OpClusterDEKStatus,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:   "ClusterDEKRotate",
			opcode: OpClusterDEKRotate,
			payload: &service.RotateClusterDEKRequest{
				Reason:                "scheduled rotation",
				AutoStartReencryption: true,
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKSyncNode",
			opcode: OpClusterDEKSyncNode,
			payload: &service.SyncNodeDEKRequest{
				NodeID:         1,
				CurrentVersion: 1,
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKNodeStatus",
			opcode: OpClusterDEKNodeStatus,
			payload: &service.GetNodeDEKStatusRequest{
				NodeID: 1,
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKStartReencrypt",
			opcode: OpClusterDEKStartReencrypt,
			payload: &service.StartReencryptionRequest{
				FromVersion: 1,
				ToVersion:   2,
				BatchSize:   100,
			},
			hasPayload: true,
		},
		{
			name:       "ClusterDEKReencryptStatus",
			opcode:     OpClusterDEKReencryptStatus,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:       "ClusterDEKCancelReencrypt",
			opcode:     OpClusterDEKCancelReencrypt,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:   "ClusterDEKRecover",
			opcode: OpClusterDEKRecover,
			payload: &service.RecoverDEKRequest{
				RecoveryMethod: service.RecoveryMethodBackup,
				BackupLocation: "/backup/dek/v2",
				Reason:         "disaster recovery test",
			},
			hasPayload: true,
		},
	}

	// Verify we have all 9 operations
	if len(tests) != 9 {
		t.Errorf("expected 9 test cases, got %d", len(tests))
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var payload []byte
			if tt.hasPayload {
				var err error
				payload, err = EncodePayload(tt.payload)
				if err != nil {
					t.Fatalf("failed to encode payload: %v", err)
				}
			} else {
				payload = []byte{}
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK for %s, got %d", tt.name, resp.Status)
			}
		})
	}
}

// =============================================================================
// Table-Driven Tests for Nil Service Errors
// =============================================================================

func TestHandleClusterDEKOperations_NilService(t *testing.T) {
	handler := createClusterDEKTestHandler(nil)

	tests := []struct {
		name       string
		opcode     byte
		payload    interface{}
		hasPayload bool
	}{
		{
			name:   "ClusterDEKInit",
			opcode: OpClusterDEKInit,
			payload: &service.InitClusterDEKRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
			},
			hasPayload: true,
		},
		{
			name:       "ClusterDEKStatus",
			opcode:     OpClusterDEKStatus,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:   "ClusterDEKRotate",
			opcode: OpClusterDEKRotate,
			payload: &service.RotateClusterDEKRequest{
				Reason: "test",
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKSyncNode",
			opcode: OpClusterDEKSyncNode,
			payload: &service.SyncNodeDEKRequest{
				NodeID: 1,
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKNodeStatus",
			opcode: OpClusterDEKNodeStatus,
			payload: &service.GetNodeDEKStatusRequest{
				NodeID: 1,
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKStartReencrypt",
			opcode: OpClusterDEKStartReencrypt,
			payload: &service.StartReencryptionRequest{
				FromVersion: 1,
				ToVersion:   2,
			},
			hasPayload: true,
		},
		{
			name:       "ClusterDEKReencryptStatus",
			opcode:     OpClusterDEKReencryptStatus,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:       "ClusterDEKCancelReencrypt",
			opcode:     OpClusterDEKCancelReencrypt,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:   "ClusterDEKRecover",
			opcode: OpClusterDEKRecover,
			payload: &service.RecoverDEKRequest{
				RecoveryMethod: service.RecoveryMethodBackup,
				BackupLocation: "/backup",
				Reason:         "test",
			},
			hasPayload: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_NilService", func(t *testing.T) {
			var payload []byte
			if tt.hasPayload {
				var err error
				payload, err = EncodePayload(tt.payload)
				if err != nil {
					t.Fatalf("failed to encode payload: %v", err)
				}
			} else {
				payload = []byte{}
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status == StatusOK {
				t.Errorf("expected error for %s with nil service, got StatusOK", tt.name)
			}
		})
	}
}

// =============================================================================
// Table-Driven Tests for Service Errors
// =============================================================================

func TestHandleClusterDEKOperations_ServiceErrors(t *testing.T) {
	clusterDEKSvc := &errorClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	tests := []struct {
		name       string
		opcode     byte
		payload    interface{}
		hasPayload bool
	}{
		{
			name:   "ClusterDEKInit",
			opcode: OpClusterDEKInit,
			payload: &service.InitClusterDEKRequest{
				Algorithm: "AES-256-GCM",
				KeySize:   256,
			},
			hasPayload: true,
		},
		{
			name:       "ClusterDEKStatus",
			opcode:     OpClusterDEKStatus,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:   "ClusterDEKRotate",
			opcode: OpClusterDEKRotate,
			payload: &service.RotateClusterDEKRequest{
				Reason: "test",
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKSyncNode",
			opcode: OpClusterDEKSyncNode,
			payload: &service.SyncNodeDEKRequest{
				NodeID: 1,
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKNodeStatus",
			opcode: OpClusterDEKNodeStatus,
			payload: &service.GetNodeDEKStatusRequest{
				NodeID: 1,
			},
			hasPayload: true,
		},
		{
			name:   "ClusterDEKStartReencrypt",
			opcode: OpClusterDEKStartReencrypt,
			payload: &service.StartReencryptionRequest{
				FromVersion: 1,
				ToVersion:   2,
			},
			hasPayload: true,
		},
		{
			name:       "ClusterDEKReencryptStatus",
			opcode:     OpClusterDEKReencryptStatus,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:       "ClusterDEKCancelReencrypt",
			opcode:     OpClusterDEKCancelReencrypt,
			payload:    nil,
			hasPayload: false,
		},
		{
			name:   "ClusterDEKRecover",
			opcode: OpClusterDEKRecover,
			payload: &service.RecoverDEKRequest{
				RecoveryMethod: service.RecoveryMethodBackup,
				BackupLocation: "/backup",
				Reason:         "test",
			},
			hasPayload: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_ServiceError", func(t *testing.T) {
			var payload []byte
			if tt.hasPayload {
				var err error
				payload, err = EncodePayload(tt.payload)
				if err != nil {
					t.Fatalf("failed to encode payload: %v", err)
				}
			} else {
				payload = []byte{}
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status == StatusOK {
				t.Errorf("expected error for %s with error service, got StatusOK", tt.name)
			}
		})
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestHandleClusterDEKInit_WithRotationPolicy(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	rotationInterval := 24 * time.Hour
	payload, err := EncodePayload(&service.InitClusterDEKRequest{
		Algorithm: "AES-256-GCM",
		KeySize:   256,
		Metadata: map[string]string{
			"environment": "production",
		},
		RotationPolicy: &service.DEKRotationPolicy{
			Enabled:          true,
			RotationInterval: rotationInterval,
			AutoReencrypt:    true,
		},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKInit,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKRotate_ForceRotation(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.RotateClusterDEKRequest{
		Reason:        "security incident",
		ForceRotation: true,
		Metadata: map[string]string{
			"incident_id": "SEC-2024-001",
		},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKRotate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKSyncNode_ForceResync(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.SyncNodeDEKRequest{
		NodeID:         1,
		CurrentVersion: 2,
		ForceResync:    true,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKSyncNode,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKRecover_RecoveryKeys(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.RecoverDEKRequest{
		RecoveryMethod: service.RecoveryMethodKeys,
		RecoveryKeys: [][]byte{
			[]byte("recovery-key-1"),
			[]byte("recovery-key-2"),
			[]byte("recovery-key-3"),
		},
		Reason: "disaster recovery with keys",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKRecover,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKRecover_ManualKey(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.RecoverDEKRequest{
		RecoveryMethod: service.RecoveryMethodManual,
		ManualKey:      []byte("manual-dek-key-32-bytes-long!!!"),
		Reason:         "break-glass emergency recovery",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKRecover,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleClusterDEKStartReencrypt_WithThrottle(t *testing.T) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, err := EncodePayload(&service.StartReencryptionRequest{
		FromVersion:  1,
		ToVersion:    2,
		BatchSize:    50,
		Concurrency:  2,
		ThrottleRate: 1000,
		Force:        false,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpClusterDEKStartReencrypt,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

// =============================================================================
// Benchmark Tests
// =============================================================================

func BenchmarkHandler_HandleClusterDEKInit(b *testing.B) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, _ := EncodePayload(&service.InitClusterDEKRequest{
		Algorithm: "AES-256-GCM",
		KeySize:   256,
	})
	req := &Request{
		Opcode:  OpClusterDEKInit,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleClusterDEKStatus(b *testing.B) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKStatus,
		Payload: []byte{},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleClusterDEKRotate(b *testing.B) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	payload, _ := EncodePayload(&service.RotateClusterDEKRequest{
		Reason:                "benchmark rotation",
		AutoStartReencryption: false,
	})
	req := &Request{
		Opcode:  OpClusterDEKRotate,
		Payload: payload,
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}

func BenchmarkHandler_HandleClusterDEKReencryptStatus(b *testing.B) {
	clusterDEKSvc := &mockClusterDEKService{}
	handler := createClusterDEKTestHandler(clusterDEKSvc)

	req := &Request{
		Opcode:  OpClusterDEKReencryptStatus,
		Payload: []byte{},
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handler.HandleRequest(ctx, req)
	}
}
