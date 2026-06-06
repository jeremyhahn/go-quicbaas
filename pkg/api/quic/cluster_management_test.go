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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// TestListNodes - Tests the ListNodes cluster operation
// =============================================================================

func TestHandleListNodes_Success(t *testing.T) {
	// Create mock service that returns node list via cluster status
	mockSvc := &mockService{
		clusterStatusFn: func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
			return &service.ClusterStatusResponse{
				ClusterID: "test-cluster",
				Nodes: []*service.NodeInfo{
					{NodeID: 1, Address: "node1:8080", IsLeader: true},
					{NodeID: 2, Address: "node2:8080", IsLeader: false},
					{NodeID: 3, Address: "node3:8080", IsLeader: false},
				},
			}, nil
		},
	}

	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	// Create request
	payload, err := EncodePayload(&service.ClusterStatusRequest{IncludeDetails: true})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpListNodes,
		Payload: payload,
	}

	// Execute handler
	resp := handler.HandleRequest(context.Background(), req)

	// Verify response
	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X, payload: %s", resp.Status, string(resp.Payload))
	}

	// Decode and verify response structure
	type ListNodesResponse struct {
		Nodes      []*service.NodeInfo
		TotalCount uint64
	}
	var listResp ListNodesResponse
	if err := DecodePayload(resp.Payload, &listResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if listResp.TotalCount != 3 {
		t.Errorf("Expected 3 nodes, got %d", listResp.TotalCount)
	}

	if len(listResp.Nodes) != 3 {
		t.Errorf("Expected 3 nodes in list, got %d", len(listResp.Nodes))
	}
}

func TestHandleListNodes_ServiceError(t *testing.T) {
	expectedErr := errors.New("cluster unavailable")
	mockSvc := &mockService{
		clusterStatusFn: func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
			return nil, expectedErr
		},
	}

	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.ClusterStatusRequest{})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpListNodes,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("Expected error status, got OK")
	}
}

func TestHandleListNodes_InvalidPayload(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpListNodes,
		Payload: []byte("invalid msgpack data"),
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("Expected error status for invalid payload, got OK")
	}
}

// =============================================================================
// TestRebalanceStatus - Tests the RebalanceStatus operation
// =============================================================================

func TestHandleRebalanceStatus_Success(t *testing.T) {
	mockSvc := &mockService{
		getRebalanceProgressFn: func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
			return &service.GetRebalanceProgressResponse{
				OperationID: req.OperationID,
				Status:      "in_progress",
				Progress:    42.5,
			}, nil
		},
	}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	type RebalanceStatusRequest struct {
		JobID string
	}

	payload, err := EncodePayload(&RebalanceStatusRequest{JobID: "job-123"})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpRebalanceStatus,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X, payload: %s", resp.Status, string(resp.Payload))
	}

	var statusResp service.GetRebalanceProgressResponse
	if err := DecodePayload(resp.Payload, &statusResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if statusResp.OperationID != "job-123" {
		t.Errorf("Expected OperationID 'job-123', got '%s'", statusResp.OperationID)
	}

	if statusResp.Status == "" {
		t.Error("Expected non-empty status")
	}
}

func TestHandleRebalanceStatus_InvalidPayload(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpRebalanceStatus,
		Payload: []byte("corrupt data"),
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("Expected error status for invalid payload, got OK")
	}
}

// =============================================================================
// TestRebalancePlan - Tests the RebalancePlan operation
// =============================================================================

func TestHandleRebalancePlan_Success(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	type RebalancePlanRequest struct {
		TargetShardCount uint64
		Strategy         string
	}

	payload, err := EncodePayload(&RebalancePlanRequest{
		TargetShardCount: 10,
		Strategy:         "consistent-hash",
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpRebalancePlan,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X, payload: %s", resp.Status, string(resp.Payload))
	}

	type ShardMove struct {
		ShardID    uint64
		FromNodeID uint64
		ToNodeID   uint64
		DataSize   uint64
	}

	type RebalancePlanResponse struct {
		Strategy        string
		Moves           []*ShardMove
		EstimatedTime   int64
		ExpectedBalance float64
	}

	var planResp RebalancePlanResponse
	if err := DecodePayload(resp.Payload, &planResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if planResp.Strategy != "consistent-hash" {
		t.Errorf("Expected strategy 'consistent-hash', got '%s'", planResp.Strategy)
	}

	if planResp.Moves == nil {
		t.Error("Expected non-nil Moves slice")
	}
}

func TestHandleRebalancePlan_DifferentStrategies(t *testing.T) {
	testCases := []struct {
		name     string
		strategy string
	}{
		{"Uniform", "uniform"},
		{"ConsistentHash", "consistent-hash"},
		{"MinimumMovement", "minimum-movement"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockSvc := &mockService{}
			handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

			type RebalancePlanRequest struct {
				TargetShardCount uint64
				Strategy         string
			}

			payload, err := EncodePayload(&RebalancePlanRequest{
				TargetShardCount: 8,
				Strategy:         tc.strategy,
			})
			if err != nil {
				t.Fatalf("Failed to encode payload: %v", err)
			}

			req := &Request{
				Opcode:  OpRebalancePlan,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)

			if resp.Status != StatusOK {
				t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
			}
		})
	}
}

func TestHandleRebalancePlan_InvalidPayload(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpRebalancePlan,
		Payload: []byte{0xFF, 0xFE, 0xFD}, // Invalid msgpack
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("Expected error status for invalid payload, got OK")
	}
}

// =============================================================================
// TestCancelMigration - Tests the CancelMigration operation
// =============================================================================

func TestHandleCancelMigration_Success(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	type CancelMigrationRequest struct {
		MigrationID string
	}

	payload, err := EncodePayload(&CancelMigrationRequest{MigrationID: "migration-456"})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpCancelMigration,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X, payload: %s", resp.Status, string(resp.Payload))
	}

	type CancelMigrationResponse struct {
		MigrationID string
		Cancelled   bool
		Message     string
	}

	var cancelResp CancelMigrationResponse
	if err := DecodePayload(resp.Payload, &cancelResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if cancelResp.MigrationID != "migration-456" {
		t.Errorf("Expected MigrationID 'migration-456', got '%s'", cancelResp.MigrationID)
	}

	if !cancelResp.Cancelled {
		t.Error("Expected Cancelled to be true")
	}

	if cancelResp.Message == "" {
		t.Error("Expected non-empty message")
	}
}

func TestHandleCancelMigration_InvalidPayload(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpCancelMigration,
		Payload: []byte("not valid msgpack"),
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("Expected error status for invalid payload, got OK")
	}
}

func TestHandleCancelMigration_EmptyMigrationID(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	type CancelMigrationRequest struct {
		MigrationID string
	}

	payload, err := EncodePayload(&CancelMigrationRequest{MigrationID: ""})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpCancelMigration,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)

	// Should still succeed but with empty ID
	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X", resp.Status)
	}
}

// =============================================================================
// TestNodeReplaceRequest - Tests the NodeReplaceRequest operation
// =============================================================================

func TestHandleNodeReplaceRequest_Success(t *testing.T) {
	mockSvc := &mockService{
		replaceNodeFn: func(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
			return &service.ReplaceNodeResponse{
				JobID:  "replace-job-789",
				Status: "started",
			}, nil
		},
	}

	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.ReplaceNodeRequest{
		ShardID:    1,
		OldNodeID:  2,
		NewNodeID:  5,
		NewAddress: "node5:8080",
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpNodeReplaceRequest,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status != StatusOK {
		t.Errorf("Expected status OK, got: 0x%02X, payload: %s", resp.Status, string(resp.Payload))
	}

	var replaceResp service.ReplaceNodeResponse
	if err := DecodePayload(resp.Payload, &replaceResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if replaceResp.JobID != "replace-job-789" {
		t.Errorf("Expected JobID 'replace-job-789', got '%s'", replaceResp.JobID)
	}

	if replaceResp.Status != "started" {
		t.Errorf("Expected status 'started', got '%s'", replaceResp.Status)
	}
}

func TestHandleNodeReplaceRequest_ServiceError(t *testing.T) {
	expectedErr := errors.New("node not found")
	mockSvc := &mockService{
		replaceNodeFn: func(ctx context.Context, req *service.ReplaceNodeRequest) (*service.ReplaceNodeResponse, error) {
			return nil, expectedErr
		},
	}

	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	payload, err := EncodePayload(&service.ReplaceNodeRequest{
		ShardID:    1,
		OldNodeID:  999,
		NewNodeID:  5,
		NewAddress: "node5:8080",
	})
	if err != nil {
		t.Fatalf("Failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpNodeReplaceRequest,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("Expected error status, got OK")
	}
}

func TestHandleNodeReplaceRequest_InvalidPayload(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	req := &Request{
		Opcode:  OpNodeReplaceRequest,
		Payload: []byte{0x00, 0x01, 0x02}, // Invalid msgpack
	}

	resp := handler.HandleRequest(context.Background(), req)

	if resp.Status == StatusOK {
		t.Error("Expected error status for invalid payload, got OK")
	}
}

// =============================================================================
// Integration Tests - Testing multiple operations in sequence
// =============================================================================

func TestClusterManagementOperationsIntegration(t *testing.T) {
	// Test a typical cluster management workflow
	mockSvc := &mockService{
		clusterStatusFn: func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
			return &service.ClusterStatusResponse{
				ClusterID: "test-cluster",
				Nodes: []*service.NodeInfo{
					{NodeID: 1, Address: "node1:8080"},
					{NodeID: 2, Address: "node2:8080"},
				},
			}, nil
		},
		rebalanceClusterFn: func(ctx context.Context, req *service.RebalanceClusterRequest) (*service.RebalanceClusterResponse, error) {
			return &service.RebalanceClusterResponse{
				JobID:    "rebalance-job-1",
				Progress: 0.0,
			}, nil
		},
		getRebalanceProgressFn: func(ctx context.Context, req *service.GetRebalanceProgressRequest) (*service.GetRebalanceProgressResponse, error) {
			return &service.GetRebalanceProgressResponse{
				OperationID: req.OperationID,
				Status:      "in_progress",
				Progress:    10.0,
			}, nil
		},
	}

	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())
	ctx := context.Background()

	// Step 1: List nodes
	listPayload, _ := EncodePayload(&service.ClusterStatusRequest{})
	listResp := handler.HandleRequest(ctx, &Request{
		Opcode:  OpListNodes,
		Payload: listPayload,
	})

	if listResp.Status != StatusOK {
		t.Errorf("Step 1 (ListNodes) failed: 0x%02X", listResp.Status)
	}

	// Step 2: Start rebalance
	rebalancePayload, _ := EncodePayload(&service.RebalanceClusterRequest{
		Strategy: "uniform",
	})
	rebalanceResp := handler.HandleRequest(ctx, &Request{
		Opcode:  OpRebalanceCluster,
		Payload: rebalancePayload,
	})

	if rebalanceResp.Status != StatusOK {
		t.Errorf("Step 2 (RebalanceCluster) failed: 0x%02X", rebalanceResp.Status)
	}

	// Step 3: Check rebalance status
	type RebalanceStatusRequest struct {
		JobID string
	}
	statusPayload, _ := EncodePayload(&RebalanceStatusRequest{JobID: "rebalance-job-1"})
	statusResp := handler.HandleRequest(ctx, &Request{
		Opcode:  OpRebalanceStatus,
		Payload: statusPayload,
	})

	if statusResp.Status != StatusOK {
		t.Errorf("Step 3 (RebalanceStatus) failed: 0x%02X", statusResp.Status)
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestClusterManagementOperations_EmptyCluster(t *testing.T) {
	mockSvc := &mockService{
		clusterStatusFn: func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
			return &service.ClusterStatusResponse{
				ClusterID: "empty-cluster",
				Nodes:     []*service.NodeInfo{},
			}, nil
		},
	}

	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	payload, _ := EncodePayload(&service.ClusterStatusRequest{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpListNodes,
		Payload: payload,
	})

	if resp.Status != StatusOK {
		t.Errorf("Expected OK for empty cluster, got: 0x%02X", resp.Status)
	}

	type ListNodesResponse struct {
		Nodes      []*service.NodeInfo
		TotalCount uint64
	}
	var listResp ListNodesResponse
	if err := DecodePayload(resp.Payload, &listResp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if listResp.TotalCount != 0 {
		t.Errorf("Expected 0 nodes, got %d", listResp.TotalCount)
	}
}

func TestClusterManagementOperations_NilPayload(t *testing.T) {
	mockSvc := &mockService{}
	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	opcodes := []byte{OpListNodes, OpRebalanceStatus, OpRebalancePlan, OpCancelMigration, OpNodeReplaceRequest}

	for _, opcode := range opcodes {
		t.Run(OpcodeToString(opcode), func(t *testing.T) {
			req := &Request{
				Opcode:  opcode,
				Payload: nil,
			}

			resp := handler.HandleRequest(context.Background(), req)

			// All handlers should handle nil payload gracefully
			// They either succeed with defaults or return an appropriate error
			if resp == nil {
				t.Error("Expected non-nil response")
			}
		})
	}
}

// =============================================================================
// Performance and Concurrency Tests
// =============================================================================

func TestClusterManagementOperations_Concurrent(t *testing.T) {
	mockSvc := &mockService{
		clusterStatusFn: func(ctx context.Context, req *service.ClusterStatusRequest) (*service.ClusterStatusResponse, error) {
			return &service.ClusterStatusResponse{
				ClusterID: "test-cluster",
				Nodes:     []*service.NodeInfo{{NodeID: 1, Address: "node1:8080"}},
			}, nil
		},
	}

	handler := NewHandler(mockSvc, nil, nil, nil, createTestLogger())

	// Run multiple operations concurrently
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func() {
			payload, _ := EncodePayload(&service.ClusterStatusRequest{})
			resp := handler.HandleRequest(context.Background(), &Request{
				Opcode:  OpListNodes,
				Payload: payload,
			})

			if resp.Status != StatusOK {
				t.Errorf("Concurrent request failed: 0x%02X", resp.Status)
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
}
