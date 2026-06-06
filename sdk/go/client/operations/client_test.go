// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package operations

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport is a mock transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) Conn() interface{} {
	return nil
}

func (m *mockTransport) Close() error {
	return nil
}

func (m *mockTransport) Healthy() bool {
	return true
}

// TestNewClient tests client creation.
func TestNewClient(t *testing.T) {
	t.Run("with valid transport", func(t *testing.T) {
		client := NewClient(&mockTransport{})
		if client == nil {
			t.Error("expected non-nil client")
		}
	})

	t.Run("with nil transport", func(t *testing.T) {
		client := NewClient(nil)
		if client != nil {
			t.Error("expected nil client for nil transport")
		}
	})
}

// TestClient_MigrateData_Success tests successful migration.
func TestClient_MigrateData_Success(t *testing.T) {
	expectedResp := &MigrateDataResponse{
		MigrationID:       "mig-123",
		Status:            "in_progress",
		EstimatedDuration: 3600,
	}

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if service != serviceName {
				t.Errorf("expected service %q, got %q", serviceName, service)
			}
			if method != "MigrateData" {
				t.Errorf("expected method %q, got %q", "MigrateData", method)
			}
			return expectedResp, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.MigrateData(ctx, &MigrateDataRequest{
		SourceShard: 1,
		TargetShard: 2,
		EntityTypes: []string{"users"},
		Concurrency: 4,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.MigrationID != expectedResp.MigrationID {
		t.Errorf("expected MigrationID %q, got %q", expectedResp.MigrationID, resp.MigrationID)
	}
	if resp.Status != expectedResp.Status {
		t.Errorf("expected Status %q, got %q", expectedResp.Status, resp.Status)
	}
}

// TestClient_MigrateData_Validation tests validation errors.
func TestClient_MigrateData_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &MigrateDataResponse{MigrationID: "test"}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *MigrateDataRequest
		wantErr bool
		errMsg  string
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
			errMsg:  "request is required",
		},
		{
			name:    "zero source shard",
			req:     &MigrateDataRequest{SourceShard: 0, TargetShard: 2},
			wantErr: true,
			errMsg:  "source shard is required",
		},
		{
			name:    "zero target shard",
			req:     &MigrateDataRequest{SourceShard: 1, TargetShard: 0},
			wantErr: true,
			errMsg:  "target shard is required",
		},
		{
			name:    "same source and target",
			req:     &MigrateDataRequest{SourceShard: 1, TargetShard: 1},
			wantErr: true,
			errMsg:  "source and target shards must be different",
		},
		{
			name:    "valid request",
			req:     &MigrateDataRequest{SourceShard: 1, TargetShard: 2},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.MigrateData(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("MigrateData() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

// TestClient_MigrateData_TransportError tests transport error handling.
func TestClient_MigrateData_TransportError(t *testing.T) {
	expectedErr := errors.NewUnavailable("transport", "connection failed")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, expectedErr
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.MigrateData(ctx, &MigrateDataRequest{SourceShard: 1, TargetShard: 2})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

// TestClient_MigrateData_NilResponse tests nil response handling.
func TestClient_MigrateData_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.MigrateData(ctx, &MigrateDataRequest{SourceShard: 1, TargetShard: 2})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
	if errors.Op(err) != "operations.MigrateData" {
		t.Errorf("expected operation operations.MigrateData, got %s", errors.Op(err))
	}
}

// TestClient_MigrateData_InvalidResponseType tests invalid response type handling.
func TestClient_MigrateData_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid type", nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.MigrateData(ctx, &MigrateDataRequest{SourceShard: 1, TargetShard: 2})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
}

// TestClient_GetMigrationProgress_Success tests successful progress retrieval.
func TestClient_GetMigrationProgress_Success(t *testing.T) {
	expectedResp := &GetMigrationProgressResponse{
		MigrationID:   "mig-123",
		Progress:      50.0,
		Status:        "in_progress",
		ItemsMigrated: 500,
		TotalItems:    1000,
		ETA:           1704067200,
	}

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "GetMigrationProgress" {
				t.Errorf("expected method %q, got %q", "GetMigrationProgress", method)
			}
			return expectedResp, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetMigrationProgress(ctx, &GetMigrationProgressRequest{MigrationID: "mig-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Progress != expectedResp.Progress {
		t.Errorf("expected Progress %f, got %f", expectedResp.Progress, resp.Progress)
	}
	if resp.ItemsMigrated != expectedResp.ItemsMigrated {
		t.Errorf("expected ItemsMigrated %d, got %d", expectedResp.ItemsMigrated, resp.ItemsMigrated)
	}
}

// TestClient_GetMigrationProgress_Validation tests validation errors.
func TestClient_GetMigrationProgress_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetMigrationProgressResponse{}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetMigrationProgressRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty migration ID",
			req:     &GetMigrationProgressRequest{MigrationID: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetMigrationProgressRequest{MigrationID: "mig-123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetMigrationProgress(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetMigrationProgress() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

// TestClient_GetMigrationProgress_TransportError tests transport error handling.
func TestClient_GetMigrationProgress_TransportError(t *testing.T) {
	expectedErr := errors.NewNotFound("transport", "migration not found")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, expectedErr
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetMigrationProgress(ctx, &GetMigrationProgressRequest{MigrationID: "mig-123"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

// TestClient_GetMigrationProgress_NilResponse tests nil response handling.
func TestClient_GetMigrationProgress_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetMigrationProgress(ctx, &GetMigrationProgressRequest{MigrationID: "mig-123"})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

// TestClient_ReplaceNode_Success tests successful node replacement.
func TestClient_ReplaceNode_Success(t *testing.T) {
	expectedResp := &ReplaceNodeResponse{
		JobID:  "job-456",
		Status: "started",
	}

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "ReplaceNode" {
				t.Errorf("expected method %q, got %q", "ReplaceNode", method)
			}
			return expectedResp, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ReplaceNode(ctx, &ReplaceNodeRequest{
		ShardID:    1,
		OldNodeID:  3,
		NewNodeID:  5,
		NewAddress: "node5.cluster.local:5001",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.JobID != expectedResp.JobID {
		t.Errorf("expected JobID %q, got %q", expectedResp.JobID, resp.JobID)
	}
}

// TestClient_ReplaceNode_Validation tests validation errors.
func TestClient_ReplaceNode_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ReplaceNodeResponse{JobID: "test"}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ReplaceNodeRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "zero shard ID",
			req:     &ReplaceNodeRequest{ShardID: 0, OldNodeID: 1, NewNodeID: 2, NewAddress: "addr"},
			wantErr: true,
		},
		{
			name:    "zero old node ID",
			req:     &ReplaceNodeRequest{ShardID: 1, OldNodeID: 0, NewNodeID: 2, NewAddress: "addr"},
			wantErr: true,
		},
		{
			name:    "zero new node ID",
			req:     &ReplaceNodeRequest{ShardID: 1, OldNodeID: 1, NewNodeID: 0, NewAddress: "addr"},
			wantErr: true,
		},
		{
			name:    "empty new address",
			req:     &ReplaceNodeRequest{ShardID: 1, OldNodeID: 1, NewNodeID: 2, NewAddress: ""},
			wantErr: true,
		},
		{
			name:    "same old and new node IDs",
			req:     &ReplaceNodeRequest{ShardID: 1, OldNodeID: 3, NewNodeID: 3, NewAddress: "addr"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &ReplaceNodeRequest{ShardID: 1, OldNodeID: 3, NewNodeID: 5, NewAddress: "node5:5001"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ReplaceNode(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReplaceNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

// TestClient_ReplaceNode_TransportError tests transport error handling.
func TestClient_ReplaceNode_TransportError(t *testing.T) {
	expectedErr := errors.NewUnavailable("transport", "connection refused")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, expectedErr
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.ReplaceNode(ctx, &ReplaceNodeRequest{
		ShardID:    1,
		OldNodeID:  3,
		NewNodeID:  5,
		NewAddress: "node5:5001",
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestClient_ReplaceNode_NilResponse tests nil response handling.
func TestClient_ReplaceNode_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.ReplaceNode(ctx, &ReplaceNodeRequest{
		ShardID:    1,
		OldNodeID:  3,
		NewNodeID:  5,
		NewAddress: "node5:5001",
	})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

// TestClient_UpgradeNode_Success tests successful node upgrade.
func TestClient_UpgradeNode_Success(t *testing.T) {
	expectedResp := &UpgradeNodeResponse{
		JobID:  "upgrade-789",
		Status: "preparing",
	}

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "UpgradeNode" {
				t.Errorf("expected method %q, got %q", "UpgradeNode", method)
			}
			return expectedResp, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.UpgradeNode(ctx, &UpgradeNodeRequest{
		NodeID:        1,
		TargetVersion: "2.0.0",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.JobID != expectedResp.JobID {
		t.Errorf("expected JobID %q, got %q", expectedResp.JobID, resp.JobID)
	}
	if resp.Status != expectedResp.Status {
		t.Errorf("expected Status %q, got %q", expectedResp.Status, resp.Status)
	}
}

// TestClient_UpgradeNode_Validation tests validation errors.
func TestClient_UpgradeNode_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &UpgradeNodeResponse{JobID: "test"}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *UpgradeNodeRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "zero node ID means all nodes (valid)",
			req:     &UpgradeNodeRequest{NodeID: 0, TargetVersion: "2.0.0"},
			wantErr: false,
		},
		{
			name:    "empty target version",
			req:     &UpgradeNodeRequest{NodeID: 1, TargetVersion: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &UpgradeNodeRequest{NodeID: 1, TargetVersion: "2.0.0"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.UpgradeNode(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpgradeNode() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

// TestClient_UpgradeNode_TransportError tests transport error handling.
func TestClient_UpgradeNode_TransportError(t *testing.T) {
	expectedErr := errors.NewTimeout("transport", "request timed out")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, expectedErr
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.UpgradeNode(ctx, &UpgradeNodeRequest{NodeID: 1, TargetVersion: "2.0.0"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsTimeout(err) {
		t.Errorf("expected Timeout error, got %v", err)
	}
}

// TestClient_UpgradeNode_NilResponse tests nil response handling.
func TestClient_UpgradeNode_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.UpgradeNode(ctx, &UpgradeNodeRequest{NodeID: 1, TargetVersion: "2.0.0"})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

// TestClient_GetUpgradeStatus_Success tests successful upgrade status retrieval.
func TestClient_GetUpgradeStatus_Success(t *testing.T) {
	expectedResp := &GetUpgradeStatusResponse{
		JobID:          "upgrade-789",
		Progress:       75.0,
		Status:         "upgrading",
		CurrentVersion: "1.5.0",
		TargetVersion:  "2.0.0",
	}

	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if method != "GetUpgradeStatus" {
				t.Errorf("expected method %q, got %q", "GetUpgradeStatus", method)
			}
			return expectedResp, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetUpgradeStatus(ctx, &GetUpgradeStatusRequest{JobID: "upgrade-789"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Progress != expectedResp.Progress {
		t.Errorf("expected Progress %f, got %f", expectedResp.Progress, resp.Progress)
	}
	if resp.CurrentVersion != expectedResp.CurrentVersion {
		t.Errorf("expected CurrentVersion %q, got %q", expectedResp.CurrentVersion, resp.CurrentVersion)
	}
	if resp.TargetVersion != expectedResp.TargetVersion {
		t.Errorf("expected TargetVersion %q, got %q", expectedResp.TargetVersion, resp.TargetVersion)
	}
}

// TestClient_GetUpgradeStatus_Validation tests validation errors.
func TestClient_GetUpgradeStatus_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetUpgradeStatusResponse{}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetUpgradeStatusRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty job ID",
			req:     &GetUpgradeStatusRequest{JobID: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetUpgradeStatusRequest{JobID: "upgrade-789"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetUpgradeStatus(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetUpgradeStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

// TestClient_GetUpgradeStatus_TransportError tests transport error handling.
func TestClient_GetUpgradeStatus_TransportError(t *testing.T) {
	expectedErr := errors.NewNotFound("transport", "upgrade job not found")
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, expectedErr
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetUpgradeStatus(ctx, &GetUpgradeStatusRequest{JobID: "upgrade-789"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

// TestClient_GetUpgradeStatus_NilResponse tests nil response handling.
func TestClient_GetUpgradeStatus_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetUpgradeStatus(ctx, &GetUpgradeStatusRequest{JobID: "upgrade-789"})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

// TestClient_GetUpgradeStatus_InvalidResponseType tests invalid response type handling.
func TestClient_GetUpgradeStatus_InvalidResponseType(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid type", nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetUpgradeStatus(ctx, &GetUpgradeStatusRequest{JobID: "upgrade-789"})
	if err == nil {
		t.Fatal("expected error for invalid response type")
	}
}

// TestValidationFunctions tests validation functions directly.
func TestValidationFunctions(t *testing.T) {
	t.Run("validateMigrateDataRequest", func(t *testing.T) {
		err := validateMigrateDataRequest(nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("validateGetMigrationProgressRequest", func(t *testing.T) {
		err := validateGetMigrationProgressRequest(nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("validateReplaceNodeRequest", func(t *testing.T) {
		err := validateReplaceNodeRequest(nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("validateUpgradeNodeRequest", func(t *testing.T) {
		err := validateUpgradeNodeRequest(nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})

	t.Run("validateGetUpgradeStatusRequest", func(t *testing.T) {
		err := validateGetUpgradeStatusRequest(nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
	})
}
