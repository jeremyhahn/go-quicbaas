// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package objectstorage

import (
	"context"
	stderrors "errors"
	"io"
	"strconv"
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

// mockStream is a mock stream for testing streaming operations.
type mockStream struct {
	sendFn      func(msg interface{}) error
	recvFn      func() (interface{}, error)
	closeSendFn func() error
	closeFn     func() error
	recvIndex   int
}

func (m *mockStream) Send(msg interface{}) error {
	if m.sendFn != nil {
		return m.sendFn(msg)
	}
	return nil
}

func (m *mockStream) Recv() (interface{}, error) {
	if m.recvFn != nil {
		m.recvIndex++
		return m.recvFn()
	}
	return nil, nil
}

func (m *mockStream) CloseSend() error {
	if m.closeSendFn != nil {
		return m.closeSendFn()
	}
	return nil
}

func (m *mockStream) Close() error {
	if m.closeFn != nil {
		return m.closeFn()
	}
	return nil
}

// validResponseMock returns a mock transport that returns appropriate valid responses
func validResponseMock() *mockTransport {
	return &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			switch method {
			case "PutObject":
				return &PutObjectResponse{Key: "k", ObjectID: 1, Size: 1}, nil
			case "GetObject":
				return &GetObjectResponse{Key: "k", Value: []byte("v")}, nil
			case "DeleteObject":
				return &DeleteObjectResponse{Key: "k", Success: true}, nil
			case "HeadObject":
				return &HeadObjectResponse{Key: "k", Exists: true}, nil
			case "ListObjects":
				return &ListObjectsResponse{Objects: []*ObjectSummary{}}, nil
			case "ListByTag":
				return &ListByTagResponse{Objects: []*ObjectSummary{}}, nil
			case "ListByTenant":
				return &ListByTenantResponse{Objects: []*ObjectSummary{}}, nil
			case "CopyObject":
				return &CopyObjectResponse{Key: "k", ObjectID: 1}, nil
			case "ReplicateObject":
				return &ReplicateObjectResponse{Key: "k", Status: "completed"}, nil
			case "MigrateObject":
				return &MigrateObjectResponse{Key: "k", Success: true}, nil
			case "ArchiveObject":
				return &ArchiveObjectResponse{Key: "k", Success: true}, nil
			case "RestoreObject":
				return &RestoreObjectResponse{Key: "k", Success: true}, nil
			case "VerifyObject":
				return &VerifyObjectResponse{Key: "k", Valid: true}, nil
			case "SetObjectPolicy":
				return &SetObjectPolicyResponse{Key: "k", Success: true}, nil
			case "GetObjectPolicy":
				return &GetObjectPolicyResponse{Key: "k"}, nil
			case "ListBackends":
				return &ListBackendsResponse{Backends: []*BackendInfo{}}, nil
			case "GetBackendHealth":
				return &GetBackendHealthResponse{Name: "n", Healthy: true}, nil
			case "GetTenantUsage":
				return &GetTenantUsageResponse{TenantID: "t"}, nil
			case "GetTenantQuotas":
				return &GetTenantQuotasResponse{TenantID: "t"}, nil
			case "SetTenantQuotas":
				return &SetTenantQuotasResponse{TenantID: "t", UpdatedAt: 1234567890}, nil
			case "BatchPutObjects":
				return &BatchPutObjectsResponse{Results: []*PutObjectResponse{}, Succeeded: 1}, nil
			case "BatchGetObjects":
				return &BatchGetObjectsResponse{Objects: []*GetObjectResponse{}}, nil
			case "BatchDeleteObjects":
				return &BatchDeleteObjectsResponse{DeletedKeys: []string{"k"}}, nil
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
// PutObject Tests
// =============================================================================

func TestClient_PutObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			if service != serviceName {
				t.Errorf("expected service %q, got %q", serviceName, service)
			}
			if method != "PutObject" {
				t.Errorf("expected method %q, got %q", "PutObject", method)
			}
			return &PutObjectResponse{
				Key:       "test:key",
				ObjectID:  1,
				Checksum:  []byte("checksum"),
				Size:      100,
				Backend:   "primary",
				Version:   1,
				CreatedAt: 1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.PutObject(ctx, &PutObjectRequest{
		Key:         "test:key",
		Value:       []byte("test value"),
		ContentType: "text/plain",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Key != "test:key" {
		t.Errorf("expected key %q, got %q", "test:key", resp.Key)
	}
	if resp.Size != 100 {
		t.Errorf("expected size 100, got %d", resp.Size)
	}
}

func TestClient_PutObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *PutObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &PutObjectRequest{Value: []byte("value")},
			wantErr: true,
		},
		{
			name:    "empty value",
			req:     &PutObjectRequest{Key: "test:key"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &PutObjectRequest{Key: "test:key", Value: []byte("value")},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.PutObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("PutObject() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && !errors.IsInvalidInput(err) {
				t.Errorf("expected InvalidInput error, got %v", err)
			}
		})
	}
}

func TestClient_PutObject_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("transport", "connection failed")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.PutObject(ctx, &PutObjectRequest{Key: "test:key", Value: []byte("value")})
	if err == nil {
		t.Error("expected error")
	}
	if !errors.IsUnavailable(err) {
		t.Errorf("expected Unavailable error, got %v", err)
	}
}

func TestClient_PutObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.PutObject(ctx, &PutObjectRequest{Key: "test:key", Value: []byte("value")})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

// =============================================================================
// GetObject Tests
// =============================================================================

func TestClient_GetObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetObjectResponse{
				Key:         "test:key",
				Value:       []byte("test value"),
				ContentType: "text/plain",
				Size:        10,
				Version:     1,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetObject(ctx, &GetObjectRequest{Key: "test:key"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if string(resp.Value) != "test value" {
		t.Errorf("expected value %q, got %q", "test value", string(resp.Value))
	}
}

func TestClient_GetObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &GetObjectRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetObjectRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_GetObject_NotFound(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetObject(ctx, &GetObjectRequest{Key: "nonexistent"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

// =============================================================================
// DeleteObject Tests
// =============================================================================

func TestClient_DeleteObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &DeleteObjectResponse{
				Key:       "test:key",
				Success:   true,
				Size:      100,
				Timestamp: 1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.DeleteObject(ctx, &DeleteObjectRequest{Key: "test:key"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Success {
		t.Error("expected success to be true")
	}
}

func TestClient_DeleteObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *DeleteObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &DeleteObjectRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &DeleteObjectRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.DeleteObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("DeleteObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_DeleteObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.DeleteObject(ctx, &DeleteObjectRequest{Key: "test:key"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

// =============================================================================
// HeadObject Tests
// =============================================================================

func TestClient_HeadObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &HeadObjectResponse{
				Key:         "test:key",
				Exists:      true,
				ContentType: "text/plain",
				Size:        100,
				Version:     1,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.HeadObject(ctx, &HeadObjectRequest{Key: "test:key"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Exists {
		t.Error("expected exists to be true")
	}
}

func TestClient_HeadObject_NotExists(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.HeadObject(ctx, &HeadObjectRequest{Key: "nonexistent"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Exists {
		t.Error("expected exists to be false")
	}
}

func TestClient_HeadObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *HeadObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &HeadObjectRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &HeadObjectRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.HeadObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("HeadObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ListObjects Tests
// =============================================================================

func TestClient_ListObjects_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListObjectsResponse{
				Objects: []*ObjectSummary{
					{Key: "test:key1", Size: 100},
					{Key: "test:key2", Size: 200},
				},
				HasMore:    true,
				NextCursor: "cursor123",
				TotalCount: 2,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListObjects(ctx, &ListObjectsRequest{Prefix: "test:"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Objects) != 2 {
		t.Errorf("expected 2 objects, got %d", len(resp.Objects))
	}
}

func TestClient_ListObjects_EmptyResult(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListObjects(ctx, &ListObjectsRequest{})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Objects) != 0 {
		t.Errorf("expected 0 objects, got %d", len(resp.Objects))
	}
}

func TestClient_ListObjects_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		_, err := client.ListObjects(ctx, nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput error, got %v", err)
		}
	})

	t.Run("valid request", func(t *testing.T) {
		_, err := client.ListObjects(ctx, &ListObjectsRequest{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// =============================================================================
// ListByTag Tests
// =============================================================================

func TestClient_ListByTag_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListByTagResponse{
				Objects: []*ObjectSummary{
					{Key: "test:key1", Tags: map[string]string{"env": "prod"}},
				},
				TotalCount: 1,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListByTag(ctx, &ListByTagRequest{TagName: "env", TagValue: "prod"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Objects) != 1 {
		t.Errorf("expected 1 object, got %d", len(resp.Objects))
	}
}

func TestClient_ListByTag_EmptyResult(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListByTag(ctx, &ListByTagRequest{TagName: "env"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Objects) != 0 {
		t.Errorf("expected 0 objects, got %d", len(resp.Objects))
	}
}

func TestClient_ListByTag_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ListByTagRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty tag name",
			req:     &ListByTagRequest{TagValue: "value"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &ListByTagRequest{TagName: "env"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ListByTag(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListByTag() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ListByTenant Tests
// =============================================================================

func TestClient_ListByTenant_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListByTenantResponse{
				Objects: []*ObjectSummary{
					{Key: "test:key1"},
				},
				TotalCount: 1,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListByTenant(ctx, &ListByTenantRequest{TenantID: "tenant-123"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestClient_ListByTenant_EmptyResult(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListByTenant(ctx, &ListByTenantRequest{TenantID: "tenant-123"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Objects) != 0 {
		t.Errorf("expected 0 objects, got %d", len(resp.Objects))
	}
}

func TestClient_ListByTenant_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ListByTenantRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty tenant ID",
			req:     &ListByTenantRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &ListByTenantRequest{TenantID: "tenant-123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ListByTenant(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListByTenant() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// CopyObject Tests
// =============================================================================

func TestClient_CopyObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &CopyObjectResponse{
				Key:      "dest:key",
				ObjectID: 2,
				Size:     100,
				Version:  1,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.CopyObject(ctx, &CopyObjectRequest{
		SourceKey:      "src:key",
		DestinationKey: "dest:key",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Key != "dest:key" {
		t.Errorf("expected key %q, got %q", "dest:key", resp.Key)
	}
}

func TestClient_CopyObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.CopyObject(ctx, &CopyObjectRequest{SourceKey: "s", DestinationKey: "d"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_CopyObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *CopyObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty source key",
			req:     &CopyObjectRequest{DestinationKey: "dest:key"},
			wantErr: true,
		},
		{
			name:    "empty destination key",
			req:     &CopyObjectRequest{SourceKey: "src:key"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &CopyObjectRequest{SourceKey: "src:key", DestinationKey: "dest:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.CopyObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("CopyObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ReplicateObject Tests
// =============================================================================

func TestClient_ReplicateObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ReplicateObjectResponse{
				Key:    "test:key",
				Status: "completed",
				Results: map[string]*ReplicationResult{
					"backend-us": {Target: "backend-us", Success: true, Duration: 100},
					"backend-eu": {Target: "backend-eu", Success: true, Duration: 150},
				},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ReplicateObject(ctx, &ReplicateObjectRequest{
		Key:     "test:key",
		Targets: []string{"backend-us", "backend-eu"},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Status != "completed" {
		t.Errorf("expected status %q, got %q", "completed", resp.Status)
	}
	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
}

func TestClient_ReplicateObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.ReplicateObject(ctx, &ReplicateObjectRequest{Key: "k", Targets: []string{"t"}})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_ReplicateObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ReplicateObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &ReplicateObjectRequest{Targets: []string{"backend"}},
			wantErr: true,
		},
		{
			name:    "empty targets",
			req:     &ReplicateObjectRequest{Key: "test:key", Targets: []string{}},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &ReplicateObjectRequest{Key: "test:key", Targets: []string{"backend"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ReplicateObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ReplicateObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// MigrateObject Tests
// =============================================================================

func TestClient_MigrateObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &MigrateObjectResponse{
				Key:               "test:key",
				SourceBackend:     "old-backend",
				TargetBackend:     "new-backend",
				Success:           true,
				DeletedFromSource: true,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.MigrateObject(ctx, &MigrateObjectRequest{
		Key:              "test:key",
		TargetBackend:    "new-backend",
		DeleteFromSource: true,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Success {
		t.Error("expected success to be true")
	}
}

func TestClient_MigrateObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.MigrateObject(ctx, &MigrateObjectRequest{Key: "k", TargetBackend: "t"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_MigrateObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *MigrateObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &MigrateObjectRequest{TargetBackend: "backend"},
			wantErr: true,
		},
		{
			name:    "empty target backend",
			req:     &MigrateObjectRequest{Key: "test:key"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &MigrateObjectRequest{Key: "test:key", TargetBackend: "backend"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.MigrateObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("MigrateObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ArchiveObject Tests
// =============================================================================

func TestClient_ArchiveObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ArchiveObjectResponse{
				Key:                "test:key",
				ArchiveStore:       "cold-storage",
				Success:            true,
				DeletedFromPrimary: true,
				ArchivedAt:         1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ArchiveObject(ctx, &ArchiveObjectRequest{
		Key:               "test:key",
		TargetStore:       "cold-storage",
		DeleteFromPrimary: true,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Success {
		t.Error("expected success to be true")
	}
}

func TestClient_ArchiveObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.ArchiveObject(ctx, &ArchiveObjectRequest{Key: "k"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_ArchiveObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ArchiveObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &ArchiveObjectRequest{TargetStore: "cold"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &ArchiveObjectRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ArchiveObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ArchiveObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// RestoreObject Tests
// =============================================================================

func TestClient_RestoreObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &RestoreObjectResponse{
				Key:        "test:key",
				Backend:    "hot-storage",
				Success:    true,
				RestoredAt: 1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.RestoreObject(ctx, &RestoreObjectRequest{
		Key:           "test:key",
		TargetBackend: "hot-storage",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Success {
		t.Error("expected success to be true")
	}
}

func TestClient_RestoreObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.RestoreObject(ctx, &RestoreObjectRequest{Key: "k"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_RestoreObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *RestoreObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &RestoreObjectRequest{TargetBackend: "hot"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &RestoreObjectRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.RestoreObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("RestoreObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// VerifyObject Tests
// =============================================================================

func TestClient_VerifyObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &VerifyObjectResponse{
				Key:   "test:key",
				Valid: true,
				Results: map[string]*VerificationResult{
					"primary":   {Store: "primary", Valid: true, ChecksumMatch: true, SizeMatch: true},
					"secondary": {Store: "secondary", Valid: true, ChecksumMatch: true, SizeMatch: true},
				},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.VerifyObject(ctx, &VerifyObjectRequest{Key: "test:key"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Valid {
		t.Error("expected valid to be true")
	}
}

func TestClient_VerifyObject_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.VerifyObject(ctx, &VerifyObjectRequest{Key: "k"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_VerifyObject_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *VerifyObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &VerifyObjectRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &VerifyObjectRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.VerifyObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("VerifyObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// SetObjectPolicy Tests
// =============================================================================

func TestClient_SetObjectPolicy_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SetObjectPolicyResponse{
				Key:     "test:key",
				Success: true,
				Version: 2,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.SetObjectPolicy(ctx, &SetObjectPolicyRequest{
		Key: "test:key",
		ReplicationPolicy: &ReplicationPolicy{
			Enabled:     true,
			MinReplicas: 3,
		},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Success {
		t.Error("expected success to be true")
	}
}

func TestClient_SetObjectPolicy_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.SetObjectPolicy(ctx, &SetObjectPolicyRequest{Key: "k"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_SetObjectPolicy_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SetObjectPolicyRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &SetObjectPolicyRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &SetObjectPolicyRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.SetObjectPolicy(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetObjectPolicy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// GetObjectPolicy Tests
// =============================================================================

func TestClient_GetObjectPolicy_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetObjectPolicyResponse{
				Key: "test:key",
				ReplicationPolicy: &ReplicationPolicy{
					Enabled:     true,
					MinReplicas: 3,
				},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetObjectPolicy(ctx, &GetObjectPolicyRequest{Key: "test:key"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.ReplicationPolicy == nil {
		t.Error("expected replication policy to be set")
	}
}

func TestClient_GetObjectPolicy_NotFound(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetObjectPolicy(ctx, &GetObjectPolicyRequest{Key: "nonexistent"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestClient_GetObjectPolicy_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetObjectPolicyRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &GetObjectPolicyRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetObjectPolicyRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetObjectPolicy(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetObjectPolicy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// ListBackends Tests
// =============================================================================

func TestClient_ListBackends_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListBackendsResponse{
				Backends: []*BackendInfo{
					{Name: "primary", Type: "s3", Enabled: true, Default: true},
					{Name: "secondary", Type: "gcs", Enabled: true, Default: false},
				},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListBackends(ctx, &ListBackendsRequest{IncludeHealth: true})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Backends) != 2 {
		t.Errorf("expected 2 backends, got %d", len(resp.Backends))
	}
}

func TestClient_ListBackends_Empty(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.ListBackends(ctx, &ListBackendsRequest{})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Backends) != 0 {
		t.Errorf("expected 0 backends, got %d", len(resp.Backends))
	}
}

func TestClient_ListBackends_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	t.Run("nil request", func(t *testing.T) {
		_, err := client.ListBackends(ctx, nil)
		if err == nil {
			t.Error("expected error for nil request")
		}
		if !errors.IsInvalidInput(err) {
			t.Errorf("expected InvalidInput error, got %v", err)
		}
	})

	t.Run("valid request", func(t *testing.T) {
		_, err := client.ListBackends(ctx, &ListBackendsRequest{})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

// =============================================================================
// GetBackendHealth Tests
// =============================================================================

func TestClient_GetBackendHealth_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetBackendHealthResponse{
				Name:    "primary",
				Healthy: true,
				Latency: 5,
				Status:  "operational",
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetBackendHealth(ctx, &GetBackendHealthRequest{Name: "primary"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if !resp.Healthy {
		t.Error("expected healthy to be true")
	}
}

func TestClient_GetBackendHealth_NotFound(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetBackendHealth(ctx, &GetBackendHealthRequest{Name: "nonexistent"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestClient_GetBackendHealth_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetBackendHealthRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty name",
			req:     &GetBackendHealthRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetBackendHealthRequest{Name: "primary"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetBackendHealth(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetBackendHealth() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// GetTenantUsage Tests
// =============================================================================

func TestClient_GetTenantUsage_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetTenantUsageResponse{
				TenantID:      "tenant-123",
				ObjectCount:   1000,
				TotalSize:     1024 * 1024 * 100,
				ActiveCount:   900,
				ArchivedCount: 100,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetTenantUsage(ctx, &GetTenantUsageRequest{TenantID: "tenant-123"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.ObjectCount != 1000 {
		t.Errorf("expected object count 1000, got %d", resp.ObjectCount)
	}
}

func TestClient_GetTenantUsage_NotFound(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetTenantUsage(ctx, &GetTenantUsageRequest{TenantID: "nonexistent"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestClient_GetTenantUsage_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetTenantUsageRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty tenant ID",
			req:     &GetTenantUsageRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetTenantUsageRequest{TenantID: "tenant-123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetTenantUsage(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTenantUsage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// GetTenantQuotas Tests
// =============================================================================

func TestClient_GetTenantQuotas_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetTenantQuotasResponse{
				TenantID:         "tenant-123",
				MaxStorageBytes:  1024 * 1024 * 1024,
				MaxObjectSize:    1024 * 1024 * 100,
				MaxObjectCount:   10000,
				UsedStorageBytes: 1024 * 1024 * 100,
				UsedObjectCount:  1000,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.GetTenantQuotas(ctx, &GetTenantQuotasRequest{TenantID: "tenant-123"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.MaxObjectCount != 10000 {
		t.Errorf("expected max object count 10000, got %d", resp.MaxObjectCount)
	}
}

func TestClient_GetTenantQuotas_NotFound(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.GetTenantQuotas(ctx, &GetTenantQuotasRequest{TenantID: "nonexistent"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	if !errors.IsNotFound(err) {
		t.Errorf("expected NotFound error, got %v", err)
	}
}

func TestClient_GetTenantQuotas_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetTenantQuotasRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty tenant ID",
			req:     &GetTenantQuotasRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetTenantQuotasRequest{TenantID: "tenant-123"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetTenantQuotas(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetTenantQuotas() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// SetTenantQuotas Tests
// =============================================================================

func TestClient_SetTenantQuotas_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SetTenantQuotasResponse{
				TenantID:  "tenant-123",
				UpdatedAt: 1234567890,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.SetTenantQuotas(ctx, &SetTenantQuotasRequest{
		TenantID:        "tenant-123",
		MaxStorageBytes: 1024 * 1024 * 1024,
		MaxObjectSize:   1024 * 1024 * 100,
		MaxObjectCount:  10000,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.TenantID != "tenant-123" {
		t.Errorf("expected tenant ID %q, got %q", "tenant-123", resp.TenantID)
	}
}

func TestClient_SetTenantQuotas_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.SetTenantQuotas(ctx, &SetTenantQuotasRequest{TenantID: "t"})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_SetTenantQuotas_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SetTenantQuotasRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty tenant ID",
			req:     &SetTenantQuotasRequest{},
			wantErr: true,
		},
		{
			name:    "negative max storage bytes",
			req:     &SetTenantQuotasRequest{TenantID: "t", MaxStorageBytes: -1},
			wantErr: true,
		},
		{
			name:    "negative max object size",
			req:     &SetTenantQuotasRequest{TenantID: "t", MaxObjectSize: -1},
			wantErr: true,
		},
		{
			name:    "negative max object count",
			req:     &SetTenantQuotasRequest{TenantID: "t", MaxObjectCount: -1},
			wantErr: true,
		},
		{
			name:    "valid request with zero values (unlimited)",
			req:     &SetTenantQuotasRequest{TenantID: "tenant-123"},
			wantErr: false,
		},
		{
			name:    "valid request with quotas",
			req:     &SetTenantQuotasRequest{TenantID: "tenant-123", MaxStorageBytes: 1024, MaxObjectSize: 512, MaxObjectCount: 100},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.SetTenantQuotas(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetTenantQuotas() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// =============================================================================
// BatchPutObjects Tests
// =============================================================================

func TestClient_BatchPutObjects_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BatchPutObjectsResponse{
				Results: []*PutObjectResponse{
					{Key: "test:key1", ObjectID: 1, Size: 100},
					{Key: "test:key2", ObjectID: 2, Size: 200},
				},
				Succeeded: 2,
				Failed:    0,
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.BatchPutObjects(ctx, &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{
			{Key: "test:key1", Value: []byte("value1")},
			{Key: "test:key2", Value: []byte("value2")},
		},
		Atomic: true,
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Succeeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", resp.Succeeded)
	}
}

func TestClient_BatchPutObjects_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.BatchPutObjects(ctx, &BatchPutObjectsRequest{
		Objects: []*PutObjectRequest{{Key: "k", Value: []byte("v")}},
	})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_BatchPutObjects_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *BatchPutObjectsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty objects",
			req:     &BatchPutObjectsRequest{Objects: []*PutObjectRequest{}},
			wantErr: true,
		},
		{
			name:    "nil object in list",
			req:     &BatchPutObjectsRequest{Objects: []*PutObjectRequest{nil}},
			wantErr: true,
		},
		{
			name: "object with empty key",
			req: &BatchPutObjectsRequest{
				Objects: []*PutObjectRequest{{Key: "", Value: []byte("v")}},
			},
			wantErr: true,
		},
		{
			name: "object with empty value",
			req: &BatchPutObjectsRequest{
				Objects: []*PutObjectRequest{{Key: "k", Value: nil}},
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &BatchPutObjectsRequest{
				Objects: []*PutObjectRequest{{Key: "k", Value: []byte("v")}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.BatchPutObjects(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("BatchPutObjects() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_BatchPutObjects_TooManyObjects(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	objects := make([]*PutObjectRequest, 101)
	for i := range objects {
		objects[i] = &PutObjectRequest{Key: "k" + strconv.Itoa(i), Value: []byte("v")}
	}

	_, err := client.BatchPutObjects(ctx, &BatchPutObjectsRequest{Objects: objects})
	if err == nil {
		t.Error("expected error for too many objects")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

// =============================================================================
// BatchGetObjects Tests
// =============================================================================

func TestClient_BatchGetObjects_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BatchGetObjectsResponse{
				Objects: []*GetObjectResponse{
					{Key: "test:key1", Value: []byte("value1")},
					{Key: "test:key2", Value: []byte("value2")},
				},
				NotFound: []string{},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.BatchGetObjects(ctx, &BatchGetObjectsRequest{
		Keys: []string{"test:key1", "test:key2"},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Objects) != 2 {
		t.Errorf("expected 2 objects, got %d", len(resp.Objects))
	}
}

func TestClient_BatchGetObjects_EmptyResult(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.BatchGetObjects(ctx, &BatchGetObjectsRequest{Keys: []string{"k"}})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.Objects) != 0 {
		t.Errorf("expected 0 objects, got %d", len(resp.Objects))
	}
}

func TestClient_BatchGetObjects_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *BatchGetObjectsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty keys",
			req:     &BatchGetObjectsRequest{Keys: []string{}},
			wantErr: true,
		},
		{
			name:    "empty key in list",
			req:     &BatchGetObjectsRequest{Keys: []string{"k1", "", "k2"}},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &BatchGetObjectsRequest{Keys: []string{"k1", "k2"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.BatchGetObjects(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("BatchGetObjects() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_BatchGetObjects_TooManyKeys(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	keys := make([]string, 101)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}

	_, err := client.BatchGetObjects(ctx, &BatchGetObjectsRequest{Keys: keys})
	if err == nil {
		t.Error("expected error for too many keys")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

// =============================================================================
// BatchDeleteObjects Tests
// =============================================================================

func TestClient_BatchDeleteObjects_Success(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BatchDeleteObjectsResponse{
				DeletedKeys: []string{"test:key1", "test:key2"},
				FailedKeys:  []string{},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	resp, err := client.BatchDeleteObjects(ctx, &BatchDeleteObjectsRequest{
		Keys: []string{"test:key1", "test:key2"},
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if len(resp.DeletedKeys) != 2 {
		t.Errorf("expected 2 deleted keys, got %d", len(resp.DeletedKeys))
	}
}

func TestClient_BatchDeleteObjects_NilResponse(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.BatchDeleteObjects(ctx, &BatchDeleteObjectsRequest{Keys: []string{"k"}})
	if err == nil {
		t.Error("expected error for nil response")
	}
	code := errors.Code(err)
	if code != errors.CodeInternal {
		t.Errorf("expected Internal error code, got %v", code)
	}
}

func TestClient_BatchDeleteObjects_Validation(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *BatchDeleteObjectsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty keys",
			req:     &BatchDeleteObjectsRequest{Keys: []string{}},
			wantErr: true,
		},
		{
			name:    "empty key in list",
			req:     &BatchDeleteObjectsRequest{Keys: []string{"k1", "", "k2"}},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &BatchDeleteObjectsRequest{Keys: []string{"k1", "k2"}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.BatchDeleteObjects(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("BatchDeleteObjects() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_BatchDeleteObjects_TooManyKeys(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	keys := make([]string, 1001)
	for i := range keys {
		keys[i] = "k" + strconv.Itoa(i)
	}

	_, err := client.BatchDeleteObjects(ctx, &BatchDeleteObjectsRequest{Keys: keys})
	if err == nil {
		t.Error("expected error for too many keys")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("expected InvalidInput error, got %v", err)
	}
}

// =============================================================================
// StreamPutObject Tests
// =============================================================================

func TestClient_StreamPutObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				sendFn: func(msg interface{}) error {
					return nil
				},
				recvFn: func() (interface{}, error) {
					return &PutObjectResponse{Key: "test:key", ObjectID: 1, Size: 100}, nil
				},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	writer, err := client.StreamPutObject(ctx, StreamPutOptions{
		Key:         "test:key",
		ContentType: "text/plain",
	})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if writer == nil {
		t.Fatal("expected non-nil writer")
	}
}

func TestClient_StreamPutObject_ValidationError(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		opts    StreamPutOptions
		wantErr bool
	}{
		{
			name:    "empty key",
			opts:    StreamPutOptions{},
			wantErr: true,
		},
		{
			name:    "valid options",
			opts:    StreamPutOptions{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.StreamPutObject(ctx, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Errorf("StreamPutObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_StreamPutObject_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return nil, errors.NewConnection("transport", "connection failed")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.StreamPutObject(ctx, StreamPutOptions{Key: "test:key"})
	if err == nil {
		t.Error("expected error")
	}
}

// =============================================================================
// StreamGetObject Tests
// =============================================================================

func TestClient_StreamGetObject_Success(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return &mockStream{
				recvFn: func() (interface{}, error) {
					return &ObjectChunk{
						Data:      []byte("test data"),
						Offset:    0,
						IsLast:    true,
						TotalSize: 9,
					}, nil
				},
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	reader, err := client.StreamGetObject(ctx, &GetObjectRequest{Key: "test:key"})

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if reader == nil {
		t.Fatal("expected non-nil reader")
	}
}

func TestClient_StreamGetObject_ValidationError(t *testing.T) {
	client := NewClient(validResponseMock())
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetObjectRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty key",
			req:     &GetObjectRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetObjectRequest{Key: "test:key"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.StreamGetObject(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("StreamGetObject() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_StreamGetObject_TransportError(t *testing.T) {
	mockT := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return nil, errors.NewConnection("transport", "connection failed")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	_, err := client.StreamGetObject(ctx, &GetObjectRequest{Key: "test:key"})
	if err == nil {
		t.Error("expected error")
	}
}

// =============================================================================
// StreamPutWriter Tests
// =============================================================================

func TestStreamPutWriter_Write(t *testing.T) {
	sendCount := 0
	stream := &mockStream{
		sendFn: func(msg interface{}) error {
			sendCount++
			return nil
		},
	}

	writer := &streamPutWriter{
		stream:    stream,
		opts:      StreamPutOptions{Key: "test:key"},
		chunkSize: 10,
	}

	n, err := writer.Write([]byte("hello world"))
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 11 {
		t.Errorf("expected 11 bytes written, got %d", n)
	}
	// Should have sent metadata chunk + 2 data chunks
	if sendCount != 3 {
		t.Errorf("expected 3 sends, got %d", sendCount)
	}
}

func TestStreamPutWriter_WriteClosed(t *testing.T) {
	writer := &streamPutWriter{
		stream: &mockStream{},
		opts:   StreamPutOptions{Key: "test:key"},
		closed: true,
	}

	_, err := writer.Write([]byte("hello"))
	if err == nil {
		t.Error("expected error for closed writer")
	}
}

func TestStreamPutWriter_WriteAborted(t *testing.T) {
	writer := &streamPutWriter{
		stream:  &mockStream{},
		opts:    StreamPutOptions{Key: "test:key"},
		aborted: true,
	}

	_, err := writer.Write([]byte("hello"))
	if err == nil {
		t.Error("expected error for aborted writer")
	}
}

func TestStreamPutWriter_Close(t *testing.T) {
	stream := &mockStream{
		sendFn: func(msg interface{}) error {
			return nil
		},
		closeSendFn: func() error {
			return nil
		},
		recvFn: func() (interface{}, error) {
			return &PutObjectResponse{Key: "test:key", ObjectID: 1, Size: 5}, nil
		},
	}

	writer := &streamPutWriter{
		stream:       stream,
		opts:         StreamPutOptions{Key: "test:key"},
		chunkSize:    64 * 1024,
		metadataSent: true,
	}

	resp, err := writer.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Key != "test:key" {
		t.Errorf("expected key %q, got %q", "test:key", resp.Key)
	}
}

func TestStreamPutWriter_CloseTwice(t *testing.T) {
	writer := &streamPutWriter{
		stream: &mockStream{},
		opts:   StreamPutOptions{Key: "test:key"},
		closed: true,
	}

	_, err := writer.Close()
	if err == nil {
		t.Error("expected error for closing twice")
	}
}

func TestStreamPutWriter_Abort(t *testing.T) {
	closeCalled := false
	stream := &mockStream{
		closeFn: func() error {
			closeCalled = true
			return nil
		},
	}

	writer := &streamPutWriter{
		stream: stream,
		opts:   StreamPutOptions{Key: "test:key"},
	}

	err := writer.Abort()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !closeCalled {
		t.Error("expected stream to be closed")
	}
	if !writer.aborted {
		t.Error("expected writer to be marked as aborted")
	}
}

func TestStreamPutWriter_BytesWritten(t *testing.T) {
	writer := &streamPutWriter{
		bytesWritten: 100,
	}

	if writer.BytesWritten() != 100 {
		t.Errorf("expected 100, got %d", writer.BytesWritten())
	}
}

// =============================================================================
// StreamGetReader Tests
// =============================================================================

func TestStreamGetReader_Read(t *testing.T) {
	stream := &mockStream{
		recvFn: func() (interface{}, error) {
			return &ObjectChunk{
				Data:      []byte("hello"),
				Offset:    0,
				IsLast:    false,
				TotalSize: 5,
			}, nil
		},
	}

	reader := &streamGetReader{
		stream: stream,
		req:    &GetObjectRequest{Key: "test:key"},
	}

	buf := make([]byte, 10)
	n, err := reader.Read(buf)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes read, got %d", n)
	}
	if string(buf[:n]) != "hello" {
		t.Errorf("expected %q, got %q", "hello", string(buf[:n]))
	}
}

func TestStreamGetReader_ReadClosed(t *testing.T) {
	reader := &streamGetReader{
		stream: &mockStream{},
		req:    &GetObjectRequest{Key: "test:key"},
		closed: true,
	}

	buf := make([]byte, 10)
	_, err := reader.Read(buf)
	if err == nil {
		t.Error("expected error for closed reader")
	}
}

func TestStreamGetReader_ReadFromBuffer(t *testing.T) {
	reader := &streamGetReader{
		stream: &mockStream{},
		req:    &GetObjectRequest{Key: "test:key"},
		buffer: []byte("buffered data"),
	}

	buf := make([]byte, 5)
	n, err := reader.Read(buf)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes read, got %d", n)
	}
	if string(buf[:n]) != "buffe" {
		t.Errorf("expected %q, got %q", "buffe", string(buf[:n]))
	}
}

func TestStreamGetReader_Close(t *testing.T) {
	closeCalled := false
	stream := &mockStream{
		closeFn: func() error {
			closeCalled = true
			return nil
		},
	}

	reader := &streamGetReader{
		stream: stream,
		req:    &GetObjectRequest{Key: "test:key"},
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !closeCalled {
		t.Error("expected stream to be closed")
	}
	if !reader.closed {
		t.Error("expected reader to be marked as closed")
	}
}

func TestStreamGetReader_CloseTwice(t *testing.T) {
	reader := &streamGetReader{
		stream: &mockStream{},
		req:    &GetObjectRequest{Key: "test:key"},
		closed: true,
	}

	err := reader.Close()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStreamGetReader_TotalSize(t *testing.T) {
	reader := &streamGetReader{
		totalSize: 1000,
	}

	if reader.TotalSize() != 1000 {
		t.Errorf("expected 1000, got %d", reader.TotalSize())
	}
}

func TestStreamGetReader_BytesRead(t *testing.T) {
	reader := &streamGetReader{
		bytesRead: 500,
	}

	if reader.BytesRead() != 500 {
		t.Errorf("expected 500, got %d", reader.BytesRead())
	}
}

// errStreamBroken is a mid-stream transport failure that is not io.EOF.
var errStreamBroken = stderrors.New("stream broken")

// chunkStream returns a mock stream that delivers chunks in order and then
// returns endErr from every further Recv.
func chunkStream(endErr error, chunks ...*ObjectChunk) *mockStream {
	next := 0
	return &mockStream{
		recvFn: func() (interface{}, error) {
			if next >= len(chunks) {
				return nil, endErr
			}
			chunk := chunks[next]
			next++
			return chunk, nil
		},
	}
}

func newChunkReader(stream transport.Stream) *streamGetReader {
	return &streamGetReader{
		stream: stream,
		req:    &GetObjectRequest{Key: "test:key"},
	}
}

func TestStreamGetReader_ReadAll_LastChunkWithoutData(t *testing.T) {
	stream := chunkStream(errStreamBroken,
		&ObjectChunk{Data: []byte("hello "), TotalSize: 11},
		&ObjectChunk{Data: []byte("world"), Offset: 6},
		&ObjectChunk{IsLast: true},
	)

	got, err := io.ReadAll(newChunkReader(stream))
	if err != nil {
		t.Fatalf("io.ReadAll: unexpected error: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", string(got))
	}
}

func TestStreamGetReader_ReadAll_LastChunkWithData(t *testing.T) {
	stream := chunkStream(errStreamBroken,
		&ObjectChunk{Data: []byte("hello "), TotalSize: 11},
		&ObjectChunk{Data: []byte("world"), Offset: 6, IsLast: true},
	)

	got, err := io.ReadAll(newChunkReader(stream))
	if err != nil {
		t.Fatalf("io.ReadAll: unexpected error: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", string(got))
	}
}

func TestStreamGetReader_ReadAll_RecvEOF(t *testing.T) {
	stream := chunkStream(io.EOF,
		&ObjectChunk{Data: []byte("hello "), TotalSize: 11},
		&ObjectChunk{Data: []byte("world"), Offset: 6},
	)

	got, err := io.ReadAll(newChunkReader(stream))
	if err != nil {
		t.Fatalf("io.ReadAll: unexpected error: %v", err)
	}
	if string(got) != "hello world" {
		t.Errorf("expected %q, got %q", "hello world", string(got))
	}
}

func TestStreamGetReader_ReadAll_RecvFailure(t *testing.T) {
	stream := chunkStream(errStreamBroken,
		&ObjectChunk{Data: []byte("hello "), TotalSize: 11},
	)

	got, err := io.ReadAll(newChunkReader(stream))
	if err == nil {
		t.Fatal("io.ReadAll: expected an error for a mid-stream failure")
	}
	if stderrors.Is(err, io.EOF) {
		t.Errorf("a mid-stream failure must not be io.EOF: %v", err)
	}
	if !stderrors.Is(err, errStreamBroken) {
		t.Errorf("expected the transport error to be wrapped, got %v", err)
	}
	var qbErr *errors.QBError
	if !stderrors.As(err, &qbErr) || qbErr.Code != errors.CodeConnection {
		t.Errorf("expected a CodeConnection QBError, got %v", err)
	}
	if string(got) != "hello " {
		t.Errorf("expected %q before the failure, got %q", "hello ", string(got))
	}
}

func TestStreamGetReader_ReadAfterEOF(t *testing.T) {
	stream := chunkStream(errStreamBroken,
		&ObjectChunk{Data: []byte("hi"), TotalSize: 2, IsLast: true},
	)
	reader := newChunkReader(stream)

	buf := make([]byte, 1)
	for _, want := range []string{"h", "i"} {
		n, err := reader.Read(buf)
		if err != nil {
			t.Fatalf("Read: unexpected error: %v", err)
		}
		if string(buf[:n]) != want {
			t.Fatalf("expected %q, got %q", want, string(buf[:n]))
		}
	}

	for i := 0; i < 2; i++ {
		n, err := reader.Read(buf)
		if n != 0 || err != io.EOF {
			t.Errorf("Read %d after end: expected 0, io.EOF; got %d, %v", i+1, n, err)
		}
	}
	if stream.recvIndex != 1 {
		t.Errorf("expected no Recv after the last chunk, got %d receives", stream.recvIndex)
	}
}

// =============================================================================
// Invalid Response Type Tests
// =============================================================================

func TestClient_InvalidResponseType(t *testing.T) {
	// Test that all methods handle invalid response types correctly
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "invalid response type", nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "PutObject",
			fn: func() error {
				_, err := client.PutObject(ctx, &PutObjectRequest{Key: "k", Value: []byte("v")})
				return err
			},
		},
		{
			name: "GetObject",
			fn: func() error {
				_, err := client.GetObject(ctx, &GetObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "DeleteObject",
			fn: func() error {
				_, err := client.DeleteObject(ctx, &DeleteObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "HeadObject",
			fn: func() error {
				_, err := client.HeadObject(ctx, &HeadObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "ListObjects",
			fn: func() error {
				_, err := client.ListObjects(ctx, &ListObjectsRequest{})
				return err
			},
		},
		{
			name: "ListByTag",
			fn: func() error {
				_, err := client.ListByTag(ctx, &ListByTagRequest{TagName: "t"})
				return err
			},
		},
		{
			name: "ListByTenant",
			fn: func() error {
				_, err := client.ListByTenant(ctx, &ListByTenantRequest{TenantID: "t"})
				return err
			},
		},
		{
			name: "CopyObject",
			fn: func() error {
				_, err := client.CopyObject(ctx, &CopyObjectRequest{SourceKey: "s", DestinationKey: "d"})
				return err
			},
		},
		{
			name: "ReplicateObject",
			fn: func() error {
				_, err := client.ReplicateObject(ctx, &ReplicateObjectRequest{Key: "k", Targets: []string{"t"}})
				return err
			},
		},
		{
			name: "MigrateObject",
			fn: func() error {
				_, err := client.MigrateObject(ctx, &MigrateObjectRequest{Key: "k", TargetBackend: "t"})
				return err
			},
		},
		{
			name: "ArchiveObject",
			fn: func() error {
				_, err := client.ArchiveObject(ctx, &ArchiveObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "RestoreObject",
			fn: func() error {
				_, err := client.RestoreObject(ctx, &RestoreObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "VerifyObject",
			fn: func() error {
				_, err := client.VerifyObject(ctx, &VerifyObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "SetObjectPolicy",
			fn: func() error {
				_, err := client.SetObjectPolicy(ctx, &SetObjectPolicyRequest{Key: "k"})
				return err
			},
		},
		{
			name: "GetObjectPolicy",
			fn: func() error {
				_, err := client.GetObjectPolicy(ctx, &GetObjectPolicyRequest{Key: "k"})
				return err
			},
		},
		{
			name: "ListBackends",
			fn: func() error {
				_, err := client.ListBackends(ctx, &ListBackendsRequest{})
				return err
			},
		},
		{
			name: "GetBackendHealth",
			fn: func() error {
				_, err := client.GetBackendHealth(ctx, &GetBackendHealthRequest{Name: "n"})
				return err
			},
		},
		{
			name: "GetTenantUsage",
			fn: func() error {
				_, err := client.GetTenantUsage(ctx, &GetTenantUsageRequest{TenantID: "t"})
				return err
			},
		},
		{
			name: "GetTenantQuotas",
			fn: func() error {
				_, err := client.GetTenantQuotas(ctx, &GetTenantQuotasRequest{TenantID: "t"})
				return err
			},
		},
		{
			name: "SetTenantQuotas",
			fn: func() error {
				_, err := client.SetTenantQuotas(ctx, &SetTenantQuotasRequest{TenantID: "t"})
				return err
			},
		},
		{
			name: "BatchPutObjects",
			fn: func() error {
				_, err := client.BatchPutObjects(ctx, &BatchPutObjectsRequest{Objects: []*PutObjectRequest{{Key: "k", Value: []byte("v")}}})
				return err
			},
		},
		{
			name: "BatchGetObjects",
			fn: func() error {
				_, err := client.BatchGetObjects(ctx, &BatchGetObjectsRequest{Keys: []string{"k"}})
				return err
			},
		},
		{
			name: "BatchDeleteObjects",
			fn: func() error {
				_, err := client.BatchDeleteObjects(ctx, &BatchDeleteObjectsRequest{Keys: []string{"k"}})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err == nil {
				t.Error("expected error for invalid response type")
			}
			code := errors.Code(err)
			if code != errors.CodeInternal {
				t.Errorf("expected Internal error code, got %v", code)
			}
		})
	}
}

// =============================================================================
// Transport Error Tests
// =============================================================================

func TestClient_TransportErrors(t *testing.T) {
	// Test that all methods handle transport errors correctly
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewUnavailable("transport", "connection failed")
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name string
		fn   func() error
	}{
		{
			name: "GetObject",
			fn: func() error {
				_, err := client.GetObject(ctx, &GetObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "DeleteObject",
			fn: func() error {
				_, err := client.DeleteObject(ctx, &DeleteObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "HeadObject",
			fn: func() error {
				_, err := client.HeadObject(ctx, &HeadObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "ListObjects",
			fn: func() error {
				_, err := client.ListObjects(ctx, &ListObjectsRequest{})
				return err
			},
		},
		{
			name: "ListByTag",
			fn: func() error {
				_, err := client.ListByTag(ctx, &ListByTagRequest{TagName: "t"})
				return err
			},
		},
		{
			name: "ListByTenant",
			fn: func() error {
				_, err := client.ListByTenant(ctx, &ListByTenantRequest{TenantID: "t"})
				return err
			},
		},
		{
			name: "CopyObject",
			fn: func() error {
				_, err := client.CopyObject(ctx, &CopyObjectRequest{SourceKey: "s", DestinationKey: "d"})
				return err
			},
		},
		{
			name: "ReplicateObject",
			fn: func() error {
				_, err := client.ReplicateObject(ctx, &ReplicateObjectRequest{Key: "k", Targets: []string{"t"}})
				return err
			},
		},
		{
			name: "MigrateObject",
			fn: func() error {
				_, err := client.MigrateObject(ctx, &MigrateObjectRequest{Key: "k", TargetBackend: "t"})
				return err
			},
		},
		{
			name: "ArchiveObject",
			fn: func() error {
				_, err := client.ArchiveObject(ctx, &ArchiveObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "RestoreObject",
			fn: func() error {
				_, err := client.RestoreObject(ctx, &RestoreObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "VerifyObject",
			fn: func() error {
				_, err := client.VerifyObject(ctx, &VerifyObjectRequest{Key: "k"})
				return err
			},
		},
		{
			name: "SetObjectPolicy",
			fn: func() error {
				_, err := client.SetObjectPolicy(ctx, &SetObjectPolicyRequest{Key: "k"})
				return err
			},
		},
		{
			name: "GetObjectPolicy",
			fn: func() error {
				_, err := client.GetObjectPolicy(ctx, &GetObjectPolicyRequest{Key: "k"})
				return err
			},
		},
		{
			name: "ListBackends",
			fn: func() error {
				_, err := client.ListBackends(ctx, &ListBackendsRequest{})
				return err
			},
		},
		{
			name: "GetBackendHealth",
			fn: func() error {
				_, err := client.GetBackendHealth(ctx, &GetBackendHealthRequest{Name: "n"})
				return err
			},
		},
		{
			name: "GetTenantUsage",
			fn: func() error {
				_, err := client.GetTenantUsage(ctx, &GetTenantUsageRequest{TenantID: "t"})
				return err
			},
		},
		{
			name: "GetTenantQuotas",
			fn: func() error {
				_, err := client.GetTenantQuotas(ctx, &GetTenantQuotasRequest{TenantID: "t"})
				return err
			},
		},
		{
			name: "SetTenantQuotas",
			fn: func() error {
				_, err := client.SetTenantQuotas(ctx, &SetTenantQuotasRequest{TenantID: "t"})
				return err
			},
		},
		{
			name: "BatchPutObjects",
			fn: func() error {
				_, err := client.BatchPutObjects(ctx, &BatchPutObjectsRequest{Objects: []*PutObjectRequest{{Key: "k", Value: []byte("v")}}})
				return err
			},
		},
		{
			name: "BatchGetObjects",
			fn: func() error {
				_, err := client.BatchGetObjects(ctx, &BatchGetObjectsRequest{Keys: []string{"k"}})
				return err
			},
		},
		{
			name: "BatchDeleteObjects",
			fn: func() error {
				_, err := client.BatchDeleteObjects(ctx, &BatchDeleteObjectsRequest{Keys: []string{"k"}})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err == nil {
				t.Error("expected transport error")
			}
			if !errors.IsUnavailable(err) {
				t.Errorf("expected Unavailable error, got %v", err)
			}
		})
	}
}
