// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package batch

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

func TestClient_Execute_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BatchResponse{Revision: 1, Results: []BatchResult{{Success: true}}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *BatchRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty operations",
			req:     &BatchRequest{Operations: []BatchOperation{}},
			wantErr: true,
		},
		{
			name: "missing entity type",
			req: &BatchRequest{
				Operations: []BatchOperation{
					{Type: OpPut, Key: "key", Value: []byte("value")},
				},
			},
			wantErr: true,
		},
		{
			name: "missing key",
			req: &BatchRequest{
				Operations: []BatchOperation{
					{Type: OpPut, EntityType: "users", Value: []byte("value")},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid operation type",
			req: &BatchRequest{
				Operations: []BatchOperation{
					{Type: OpType(99), EntityType: "users", Key: "key"},
				},
			},
			wantErr: true,
		},
		{
			name: "valid put operation",
			req: &BatchRequest{
				Operations: []BatchOperation{
					{Type: OpPut, EntityType: "users", Key: "key", Value: []byte("value")},
				},
			},
			wantErr: false,
		},
		{
			name: "valid delete operation",
			req: &BatchRequest{
				Operations: []BatchOperation{
					{Type: OpDelete, EntityType: "users", Key: "key"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid multiple operations",
			req: &BatchRequest{
				Operations: []BatchOperation{
					{Type: OpPut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
					{Type: OpPut, EntityType: "users", Key: "user-2", Value: []byte("Bob")},
					{Type: OpDelete, EntityType: "users", Key: "user-old"},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Execute(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Execute(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful batch",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Execute" {
						t.Errorf("expected method %q, got %q", "Execute", method)
					}
					batchReq, ok := req.(*BatchRequest)
					if !ok {
						t.Errorf("expected BatchRequest, got %T", req)
					}
					results := make([]BatchResult, len(batchReq.Operations))
					for i := range results {
						results[i] = BatchResult{Success: true}
					}
					return &BatchResponse{Revision: 100, Results: results}, nil
				},
			},
			wantErr: false,
		},
		{
			name:      "nil context",
			ctx:       nil,
			transport: &mockTransport{},
			wantErr:   true,
			errCode:   errors.CodeInvalidInput,
		},
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "server unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
		{
			name: "nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
		{
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &BatchRequest{
		Operations: []BatchOperation{
			{Type: OpPut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Execute(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp == nil {
				t.Error("expected non-nil response")
			}
		})
	}
}

func TestOpType_String(t *testing.T) {
	tests := []struct {
		opType   OpType
		expected string
	}{
		{OpPut, "PUT"},
		{OpDelete, "DELETE"},
		{OpType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.opType.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestBatchResponse_Results(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BatchResponse{
				Revision: 42,
				Results: []BatchResult{
					{Success: true},
					{Success: true},
					{Success: false, Error: "key already exists"},
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.Execute(context.Background(), &BatchRequest{
		Operations: []BatchOperation{
			{Type: OpPut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
			{Type: OpPut, EntityType: "users", Key: "user-2", Value: []byte("Bob")},
			{Type: OpPut, EntityType: "users", Key: "user-3", Value: []byte("Carol")},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Revision != 42 {
		t.Errorf("expected revision 42, got %d", resp.Revision)
	}

	if len(resp.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(resp.Results))
	}

	successCount := 0
	for _, r := range resp.Results {
		if r.Success {
			successCount++
		}
	}
	if successCount != 2 {
		t.Errorf("expected 2 successes, got %d", successCount)
	}

	if resp.Results[2].Error != "key already exists" {
		t.Errorf("expected error message 'key already exists', got %q", resp.Results[2].Error)
	}
}

func TestValidateBatchOperation_ErrorContext(t *testing.T) {
	client := NewClient(&mockTransport{})
	ctx := context.Background()

	// Test that error context includes operation index
	req := &BatchRequest{
		Operations: []BatchOperation{
			{Type: OpPut, EntityType: "users", Key: "valid-key", Value: []byte("value")},
			{Type: OpPut, EntityType: "", Key: "key", Value: []byte("value")}, // Invalid at index 1
		},
	}

	_, err := client.Execute(ctx, req)
	if err == nil {
		t.Fatal("expected error for invalid operation")
	}

	dragonErr, ok := err.(*errors.DragonError)
	if !ok {
		t.Fatalf("expected DragonError, got %T", err)
	}

	if dragonErr.Context == nil {
		t.Fatal("expected error context to be set")
	}

	if dragonErr.Context["operation_index"] != 1 {
		t.Errorf("expected operation_index=1, got %v", dragonErr.Context["operation_index"])
	}
}
