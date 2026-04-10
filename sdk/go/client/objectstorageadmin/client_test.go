// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package objectstorageadmin

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

// validResponseMock returns a mock transport that returns appropriate valid responses.
func validResponseMock() *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			switch method {
			case "CreateBackend":
				return &CreateBackendResponse{Name: "test-backend", CreatedAt: 1234567890}, nil
			case "UpdateBackend":
				return &UpdateBackendResponse{Name: "test-backend", UpdatedAt: 1234567890}, nil
			case "DeleteBackend":
				return &DeleteBackendResponse{Name: "test-backend", Deleted: true}, nil
			case "SetBackendStatus":
				return &SetBackendStatusResponse{Name: "test-backend", Enabled: true, UpdatedAt: 1234567890}, nil
			case "GetStorageStats":
				return &GetStorageStatsResponse{TotalBytes: 1000, TotalObjects: 10}, nil
			case "RunGarbageCollection":
				return &RunGarbageCollectionResponse{OperationID: "gc-123", StartedAt: 1234567890}, nil
			case "GetGarbageCollectionProgress":
				return &GetGarbageCollectionProgressResponse{OperationID: "gc-123", Status: GCStatusRunning, PercentComplete: 50.0}, nil
			case "RepairObject":
				return &RepairObjectResponse{Key: "test:key", Success: true, RepairedLocations: []string{"backend-1"}}, nil
			case "CompactBackend":
				return &CompactBackendResponse{Name: "test-backend", OperationID: "compact-123", StartedAt: 1234567890}, nil
			default:
				return nil, nil
			}
		},
	}
}

// =============================================================================
// NewClient Tests
// =============================================================================

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

// =============================================================================
// CreateBackend Tests
// =============================================================================

func TestClient_CreateBackend_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if service != serviceName {
				t.Errorf("expected service %q, got %q", serviceName, service)
			}
			if method != "CreateBackend" {
				t.Errorf("expected method %q, got %q", "CreateBackend", method)
			}
			return &CreateBackendResponse{
				Name:      "s3-backend",
				CreatedAt: 1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.CreateBackend(ctx, &CreateBackendRequest{
		Name:    "s3-backend",
		Type:    BackendTypeS3,
		Tier:    StorageTierHot,
		Config:  map[string]string{"bucket": "my-bucket", "region": "us-east-1"},
		Enabled: true,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Name != "s3-backend" {
		t.Errorf("expected name %q, got %q", "s3-backend", resp.Name)
	}
}

func TestClient_CreateBackend_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *CreateBackendRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &CreateBackendRequest{Type: BackendTypeS3, Tier: StorageTierHot, Config: map[string]string{"k": "v"}},
			wantErr: true,
		},
		{
			name:    "unspecified type",
			req:     &CreateBackendRequest{Name: "test", Tier: StorageTierHot, Config: map[string]string{"k": "v"}},
			wantErr: true,
		},
		{
			name:    "unspecified tier",
			req:     &CreateBackendRequest{Name: "test", Type: BackendTypeS3, Config: map[string]string{"k": "v"}},
			wantErr: true,
		},
		{
			name:    "nil config",
			req:     &CreateBackendRequest{Name: "test", Type: BackendTypeS3, Tier: StorageTierHot},
			wantErr: true,
		},
		{
			name:    "empty config",
			req:     &CreateBackendRequest{Name: "test", Type: BackendTypeS3, Tier: StorageTierHot, Config: map[string]string{}},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &CreateBackendRequest{Name: "test", Type: BackendTypeS3, Tier: StorageTierHot, Config: map[string]string{"k": "v"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.CreateBackend(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateBackend() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_CreateBackend_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("transport", "connection failed")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.CreateBackend(ctx, &CreateBackendRequest{
		Name:   "test",
		Type:   BackendTypeS3,
		Tier:   StorageTierHot,
		Config: map[string]string{"k": "v"},
	})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_CreateBackend_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.CreateBackend(ctx, &CreateBackendRequest{
		Name:   "test",
		Type:   BackendTypeS3,
		Tier:   StorageTierHot,
		Config: map[string]string{"k": "v"},
	})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

// =============================================================================
// UpdateBackend Tests
// =============================================================================

func TestClient_UpdateBackend_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &UpdateBackendResponse{
				Name:      "s3-backend",
				UpdatedAt: 1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.UpdateBackend(ctx, &UpdateBackendRequest{
		Name:   "s3-backend",
		Config: map[string]string{"region": "us-west-2"},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Name != "s3-backend" {
		t.Errorf("expected name %q, got %q", "s3-backend", resp.Name)
	}
}

func TestClient_UpdateBackend_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *UpdateBackendRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &UpdateBackendRequest{Config: map[string]string{"k": "v"}},
			wantErr: true,
		},
		{
			name:    "no update fields",
			req:     &UpdateBackendRequest{Name: "test"},
			wantErr: true,
		},
		{
			name:    "valid request with config",
			req:     &UpdateBackendRequest{Name: "test", Config: map[string]string{"k": "v"}},
			wantErr: false,
		},
		{
			name:    "valid request with tier",
			req:     &UpdateBackendRequest{Name: "test", Tier: StorageTierWarm},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.UpdateBackend(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("UpdateBackend() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_UpdateBackend_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.UpdateBackend(ctx, &UpdateBackendRequest{
		Name:   "test",
		Config: map[string]string{"k": "v"},
	})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

// =============================================================================
// DeleteBackend Tests
// =============================================================================

func TestClient_DeleteBackend_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &DeleteBackendResponse{
				Name:            "old-backend",
				Deleted:         true,
				MigratedObjects: 100,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.DeleteBackend(ctx, &DeleteBackendRequest{
		Name:      "old-backend",
		MigrateTo: "new-backend",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Deleted {
		t.Error("expected Deleted to be true")
	}
	if resp.MigratedObjects != 100 {
		t.Errorf("expected 100 migrated objects, got %d", resp.MigratedObjects)
	}
}

func TestClient_DeleteBackend_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *DeleteBackendRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &DeleteBackendRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &DeleteBackendRequest{Name: "test"},
			wantErr: false,
		},
		{
			name:    "valid request with migrate_to",
			req:     &DeleteBackendRequest{Name: "test", MigrateTo: "other"},
			wantErr: false,
		},
		{
			name:    "valid request with force",
			req:     &DeleteBackendRequest{Name: "test", Force: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.DeleteBackend(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteBackend() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_DeleteBackend_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.DeleteBackend(ctx, &DeleteBackendRequest{Name: "test"})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

// =============================================================================
// SetBackendStatus Tests
// =============================================================================

func TestClient_SetBackendStatus_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SetBackendStatusResponse{
				Name:      "backend",
				Enabled:   false,
				UpdatedAt: 1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.SetBackendStatus(ctx, &SetBackendStatusRequest{
		Name:    "backend",
		Enabled: false,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Enabled {
		t.Error("expected Enabled to be false")
	}
}

func TestClient_SetBackendStatus_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SetBackendStatusRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &SetBackendStatusRequest{Enabled: true},
			wantErr: true,
		},
		{
			name:    "valid request enabled",
			req:     &SetBackendStatusRequest{Name: "test", Enabled: true},
			wantErr: false,
		},
		{
			name:    "valid request disabled",
			req:     &SetBackendStatusRequest{Name: "test", Enabled: false},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.SetBackendStatus(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetBackendStatus() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_SetBackendStatus_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.SetBackendStatus(ctx, &SetBackendStatusRequest{Name: "test", Enabled: true})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

// =============================================================================
// GetStorageStats Tests
// =============================================================================

func TestClient_GetStorageStats_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetStorageStatsResponse{
				TotalBytes:      1073741824,
				TotalObjects:    1000,
				TotalBackends:   3,
				HealthyBackends: 3,
				CalculatedAt:    1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetStorageStats(ctx, &GetStorageStatsRequest{
		IncludeBackends: true,
		IncludeTenants:  true,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.TotalBytes != 1073741824 {
		t.Errorf("expected total bytes 1073741824, got %d", resp.TotalBytes)
	}
	if resp.TotalObjects != 1000 {
		t.Errorf("expected total objects 1000, got %d", resp.TotalObjects)
	}
}

func TestClient_GetStorageStats_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetStorageStatsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid empty request",
			req:     &GetStorageStatsRequest{},
			wantErr: false,
		},
		{
			name:    "valid request with flags",
			req:     &GetStorageStatsRequest{IncludeBackends: true, IncludeTenants: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetStorageStats(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetStorageStats() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_GetStorageStats_NilResponseReturnsEmpty(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetStorageStats(ctx, &GetStorageStatsRequest{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.BackendStats == nil {
		t.Error("expected non-nil BackendStats slice")
	}
	if resp.TenantStats == nil {
		t.Error("expected non-nil TenantStats slice")
	}
}

// =============================================================================
// RunGarbageCollection Tests
// =============================================================================

func TestClient_RunGarbageCollection_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RunGarbageCollectionResponse{
				OperationID: "gc-operation-123",
				StartedAt:   1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.RunGarbageCollection(ctx, &RunGarbageCollectionRequest{
		Backends:   []string{"backend-1", "backend-2"},
		DryRun:     false,
		Aggressive: true,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.OperationID != "gc-operation-123" {
		t.Errorf("expected operation ID %q, got %q", "gc-operation-123", resp.OperationID)
	}
}

func TestClient_RunGarbageCollection_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RunGarbageCollectionRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid empty request (all backends)",
			req:     &RunGarbageCollectionRequest{},
			wantErr: false,
		},
		{
			name:    "valid request with backends",
			req:     &RunGarbageCollectionRequest{Backends: []string{"backend-1"}},
			wantErr: false,
		},
		{
			name:    "valid request with dry run",
			req:     &RunGarbageCollectionRequest{DryRun: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RunGarbageCollection(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RunGarbageCollection() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_RunGarbageCollection_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.RunGarbageCollection(ctx, &RunGarbageCollectionRequest{})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

// =============================================================================
// GetGarbageCollectionProgress Tests
// =============================================================================

func TestClient_GetGarbageCollectionProgress_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetGarbageCollectionProgressResponse{
				OperationID:     "gc-operation-123",
				Status:          GCStatusRunning,
				ObjectsScanned:  5000,
				ObjectsDeleted:  100,
				BytesReclaimed:  1048576,
				PercentComplete: 50.0,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetGarbageCollectionProgress(ctx, &GetGarbageCollectionProgressRequest{
		OperationID: "gc-operation-123",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Status != GCStatusRunning {
		t.Errorf("expected status %q, got %q", GCStatusRunning, resp.Status)
	}
	if resp.PercentComplete != 50.0 {
		t.Errorf("expected 50.0%% complete, got %v%%", resp.PercentComplete)
	}
}

func TestClient_GetGarbageCollectionProgress_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetGarbageCollectionProgressRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty operation ID",
			req:     &GetGarbageCollectionProgressRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetGarbageCollectionProgressRequest{OperationID: "gc-123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetGarbageCollectionProgress(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetGarbageCollectionProgress() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_GetGarbageCollectionProgress_NotFound(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetGarbageCollectionProgress(ctx, &GetGarbageCollectionProgressRequest{OperationID: "nonexistent"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

// =============================================================================
// RepairObject Tests
// =============================================================================

func TestClient_RepairObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RepairObjectResponse{
				Key:               "test:object",
				Success:           true,
				RepairedLocations: []string{"backend-1", "backend-2"},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.RepairObject(ctx, &RepairObjectRequest{
		Key:            "test:object",
		SourceLocation: "backend-primary",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Success {
		t.Error("expected Success to be true")
	}
	if len(resp.RepairedLocations) != 2 {
		t.Errorf("expected 2 repaired locations, got %d", len(resp.RepairedLocations))
	}
}

func TestClient_RepairObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RepairObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &RepairObjectRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &RepairObjectRequest{Key: "test:key"},
			wantErr: false,
		},
		{
			name:    "valid request with source location",
			req:     &RepairObjectRequest{Key: "test:key", SourceLocation: "backend-1"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RepairObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RepairObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_RepairObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.RepairObject(ctx, &RepairObjectRequest{Key: "test:key"})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_RepairObject_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewNotFound("storage", "object not found")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.RepairObject(ctx, &RepairObjectRequest{Key: "nonexistent"})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

// =============================================================================
// CompactBackend Tests
// =============================================================================

func TestClient_CompactBackend_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &CompactBackendResponse{
				Name:        "pebble-backend",
				OperationID: "compact-456",
				StartedAt:   1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.CompactBackend(ctx, &CompactBackendRequest{
		Name:       "pebble-backend",
		Aggressive: true,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Name != "pebble-backend" {
		t.Errorf("expected name %q, got %q", "pebble-backend", resp.Name)
	}
	if resp.OperationID != "compact-456" {
		t.Errorf("expected operation ID %q, got %q", "compact-456", resp.OperationID)
	}
}

func TestClient_CompactBackend_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *CompactBackendRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &CompactBackendRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &CompactBackendRequest{Name: "test"},
			wantErr: false,
		},
		{
			name:    "valid request with aggressive",
			req:     &CompactBackendRequest{Name: "test", Aggressive: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.CompactBackend(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("CompactBackend() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_CompactBackend_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.CompactBackend(ctx, &CompactBackendRequest{Name: "test"})
	if err == nil {
		t.Error("expected error for nil response")
	}
}

func TestClient_CompactBackend_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewNotFound("storage", "backend not found")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.CompactBackend(ctx, &CompactBackendRequest{Name: "nonexistent"})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

// =============================================================================
// Type Tests
// =============================================================================

func TestBackendType_String(t *testing.T) {
	tests := []struct {
		bt       BackendType
		expected string
	}{
		{BackendTypeUnspecified, "UNSPECIFIED"},
		{BackendTypeLocal, "LOCAL"},
		{BackendTypeS3, "S3"},
		{BackendTypeGCS, "GCS"},
		{BackendTypeAzure, "AZURE"},
		{BackendTypeMinio, "MINIO"},
		{BackendTypePebble, "PEBBLE"},
		{BackendTypeRaft, "RAFT"},
		{BackendType(99), "UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.bt.String(); got != tt.expected {
				t.Errorf("BackendType.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestStorageTier_String(t *testing.T) {
	tests := []struct {
		st       StorageTier
		expected string
	}{
		{StorageTierUnspecified, "UNSPECIFIED"},
		{StorageTierHot, "HOT"},
		{StorageTierWarm, "WARM"},
		{StorageTierCold, "COLD"},
		{StorageTierArchive, "ARCHIVE"},
		{StorageTier(99), "UNSPECIFIED"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.st.String(); got != tt.expected {
				t.Errorf("StorageTier.String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// =============================================================================
// Integration-style Tests (mocked)
// =============================================================================

func TestClient_BackendLifecycle(t *testing.T) {
	callOrder := make([]string, 0)
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			callOrder = append(callOrder, method)
			switch method {
			case "CreateBackend":
				return &CreateBackendResponse{Name: "test-backend", CreatedAt: 1}, nil
			case "SetBackendStatus":
				return &SetBackendStatusResponse{Name: "test-backend", Enabled: false, UpdatedAt: 2}, nil
			case "UpdateBackend":
				return &UpdateBackendResponse{Name: "test-backend", UpdatedAt: 3}, nil
			case "DeleteBackend":
				return &DeleteBackendResponse{Name: "test-backend", Deleted: true}, nil
			default:
				return nil, nil
			}
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	// Create backend
	_, err := client.CreateBackend(ctx, &CreateBackendRequest{
		Name:   "test-backend",
		Type:   BackendTypeS3,
		Tier:   StorageTierHot,
		Config: map[string]string{"bucket": "test"},
	})
	if err != nil {
		t.Fatalf("CreateBackend failed: %v", err)
	}

	// Disable backend
	_, err = client.SetBackendStatus(ctx, &SetBackendStatusRequest{
		Name:    "test-backend",
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("SetBackendStatus failed: %v", err)
	}

	// Update backend
	_, err = client.UpdateBackend(ctx, &UpdateBackendRequest{
		Name:   "test-backend",
		Config: map[string]string{"region": "us-west-2"},
	})
	if err != nil {
		t.Fatalf("UpdateBackend failed: %v", err)
	}

	// Delete backend
	_, err = client.DeleteBackend(ctx, &DeleteBackendRequest{
		Name: "test-backend",
	})
	if err != nil {
		t.Fatalf("DeleteBackend failed: %v", err)
	}

	// Verify call order
	expectedOrder := []string{"CreateBackend", "SetBackendStatus", "UpdateBackend", "DeleteBackend"}
	if len(callOrder) != len(expectedOrder) {
		t.Fatalf("expected %d calls, got %d", len(expectedOrder), len(callOrder))
	}
	for i, expected := range expectedOrder {
		if callOrder[i] != expected {
			t.Errorf("call %d: expected %q, got %q", i, expected, callOrder[i])
		}
	}
}

func TestClient_GarbageCollectionWorkflow(t *testing.T) {
	gcProgress := 0.0
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			switch method {
			case "RunGarbageCollection":
				return &RunGarbageCollectionResponse{OperationID: "gc-workflow", StartedAt: 1}, nil
			case "GetGarbageCollectionProgress":
				gcProgress += 25.0
				status := GCStatusRunning
				if gcProgress >= 100 {
					status = GCStatusCompleted
				}
				return &GetGarbageCollectionProgressResponse{
					OperationID:     "gc-workflow",
					Status:          status,
					PercentComplete: gcProgress,
				}, nil
			default:
				return nil, nil
			}
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	// Start GC
	gcResp, err := client.RunGarbageCollection(ctx, &RunGarbageCollectionRequest{})
	if err != nil {
		t.Fatalf("RunGarbageCollection failed: %v", err)
	}
	if gcResp.OperationID != "gc-workflow" {
		t.Errorf("expected operation ID %q, got %q", "gc-workflow", gcResp.OperationID)
	}

	// Poll for progress until complete
	for i := 0; i < 5; i++ {
		progressResp, err := client.GetGarbageCollectionProgress(ctx, &GetGarbageCollectionProgressRequest{
			OperationID: gcResp.OperationID,
		})
		if err != nil {
			t.Fatalf("GetGarbageCollectionProgress failed: %v", err)
		}
		if progressResp.Status == GCStatusCompleted {
			break
		}
		if i == 4 {
			t.Error("GC did not complete in expected iterations")
		}
	}
}
