// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package transaction

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
			return &TxnResponse{Succeeded: true, Revision: 1}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *TxnRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty request",
			req:     &TxnRequest{},
			wantErr: true,
		},
		{
			name: "compare with empty key",
			req: &TxnRequest{
				Compare: []Compare{
					{Key: "", Target: CompareValue, Result: CompareEqual},
				},
			},
			wantErr: true,
		},
		{
			name: "compare with invalid target",
			req: &TxnRequest{
				Compare: []Compare{
					{Key: "test", Target: CompareTarget(99), Result: CompareEqual},
				},
			},
			wantErr: true,
		},
		{
			name: "compare with invalid result",
			req: &TxnRequest{
				Compare: []Compare{
					{Key: "test", Target: CompareValue, Result: CompareResult(99)},
				},
			},
			wantErr: true,
		},
		{
			name: "success op with empty entity type",
			req: &TxnRequest{
				Success: []TxnOp{
					{Type: TxnOpPut, Key: "key", Value: []byte("value")},
				},
			},
			wantErr: true,
		},
		{
			name: "success op with empty key",
			req: &TxnRequest{
				Success: []TxnOp{
					{Type: TxnOpPut, EntityType: "users", Value: []byte("value")},
				},
			},
			wantErr: true,
		},
		{
			name: "success op with invalid type",
			req: &TxnRequest{
				Success: []TxnOp{
					{Type: TxnOpType(99), EntityType: "users", Key: "key"},
				},
			},
			wantErr: true,
		},
		{
			name: "failure op with empty entity type",
			req: &TxnRequest{
				Compare: []Compare{
					{Key: "test", Target: CompareValue, Result: CompareEqual},
				},
				Failure: []TxnOp{
					{Type: TxnOpGet, Key: "key"},
				},
			},
			wantErr: true,
		},
		{
			name: "valid compare only",
			req: &TxnRequest{
				Compare: []Compare{
					{Key: "test", Target: CompareVersion, Result: CompareEqual, Revision: 0},
				},
			},
			wantErr: false,
		},
		{
			name: "valid success only",
			req: &TxnRequest{
				Success: []TxnOp{
					{Type: TxnOpPut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
				},
			},
			wantErr: false,
		},
		{
			name: "valid full transaction",
			req: &TxnRequest{
				Compare: []Compare{
					{Key: "users/1", Target: CompareValue, Result: CompareEqual, Value: []byte("Alice")},
				},
				Success: []TxnOp{
					{Type: TxnOpPut, EntityType: "users", Key: "users/1", Value: []byte("Alice Updated")},
				},
				Failure: []TxnOp{
					{Type: TxnOpGet, EntityType: "users", Key: "users/1"},
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
		succeeded bool
	}{
		{
			name: "successful transaction",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Execute" {
						t.Errorf("expected method %q, got %q", "Execute", method)
					}
					return &TxnResponse{
						Succeeded: true,
						Revision:  100,
						Responses: []TxnOpResponse{
							{Type: TxnOpPut},
						},
					}, nil
				},
			},
			wantErr:   false,
			succeeded: true,
		},
		{
			name: "failed transaction (compare failed)",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &TxnResponse{
						Succeeded: false,
						Revision:  99,
						Responses: []TxnOpResponse{
							{Type: TxnOpGet, Value: []byte("current value")},
						},
					}, nil
				},
			},
			wantErr:   false,
			succeeded: false,
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

	validReq := &TxnRequest{
		Compare: []Compare{
			{Key: "test", Target: CompareVersion, Result: CompareEqual},
		},
		Success: []TxnOp{
			{Type: TxnOpPut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
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
			if !tt.wantErr && resp.Succeeded != tt.succeeded {
				t.Errorf("Execute() succeeded = %v, want %v", resp.Succeeded, tt.succeeded)
			}
		})
	}
}

func TestCompareTarget_String(t *testing.T) {
	tests := []struct {
		target   CompareTarget
		expected string
	}{
		{CompareVersion, "VERSION"},
		{CompareValue, "VALUE"},
		{CompareModRevision, "MOD_REVISION"},
		{CompareCreateRevision, "CREATE_REVISION"},
		{CompareTarget(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.target.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestCompareResult_String(t *testing.T) {
	tests := []struct {
		result   CompareResult
		expected string
	}{
		{CompareEqual, "EQUAL"},
		{CompareNotEqual, "NOT_EQUAL"},
		{CompareLess, "LESS"},
		{CompareGreater, "GREATER"},
		{CompareResult(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.result.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestTxnOpType_String(t *testing.T) {
	tests := []struct {
		opType   TxnOpType
		expected string
	}{
		{TxnOpPut, "PUT"},
		{TxnOpGet, "GET"},
		{TxnOpDelete, "DELETE"},
		{TxnOpType(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.opType.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestTxnResponse_WithResponses(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &TxnResponse{
				Succeeded: true,
				Revision:  42,
				Responses: []TxnOpResponse{
					{Type: TxnOpPut},
					{Type: TxnOpGet, Value: []byte("retrieved value")},
					{Type: TxnOpDelete},
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.Execute(context.Background(), &TxnRequest{
		Success: []TxnOp{
			{Type: TxnOpPut, EntityType: "users", Key: "user-1", Value: []byte("Alice")},
			{Type: TxnOpGet, EntityType: "users", Key: "user-2"},
			{Type: TxnOpDelete, EntityType: "users", Key: "user-3"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Succeeded {
		t.Error("expected transaction to succeed")
	}

	if resp.Revision != 42 {
		t.Errorf("expected revision 42, got %d", resp.Revision)
	}

	if len(resp.Responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(resp.Responses))
	}

	if resp.Responses[1].Type != TxnOpGet {
		t.Errorf("expected second response type GET, got %v", resp.Responses[1].Type)
	}

	if string(resp.Responses[1].Value) != "retrieved value" {
		t.Errorf("expected value 'retrieved value', got %q", string(resp.Responses[1].Value))
	}
}

func TestValidation_ErrorContext(t *testing.T) {
	client := NewClient(&mockTransport{})
	ctx := context.Background()

	t.Run("compare error context", func(t *testing.T) {
		req := &TxnRequest{
			Compare: []Compare{
				{Key: "valid-key", Target: CompareValue, Result: CompareEqual},
				{Key: "", Target: CompareValue, Result: CompareEqual}, // Invalid at index 1
			},
		}

		_, err := client.Execute(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid compare")
		}

		qbErr, ok := err.(*errors.QBError)
		if !ok {
			t.Fatalf("expected QBError, got %T", err)
		}

		if qbErr.Context == nil {
			t.Fatal("expected error context to be set")
		}

		if qbErr.Context["compare_index"] != 1 {
			t.Errorf("expected compare_index=1, got %v", qbErr.Context["compare_index"])
		}
	})

	t.Run("success operation error context", func(t *testing.T) {
		req := &TxnRequest{
			Success: []TxnOp{
				{Type: TxnOpPut, EntityType: "users", Key: "valid-key"},
				{Type: TxnOpPut, EntityType: "", Key: "key"}, // Invalid at index 1
			},
		}

		_, err := client.Execute(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid operation")
		}

		qbErr, ok := err.(*errors.QBError)
		if !ok {
			t.Fatalf("expected QBError, got %T", err)
		}

		if qbErr.Context["branch"] != "success" {
			t.Errorf("expected branch='success', got %v", qbErr.Context["branch"])
		}
		if qbErr.Context["operation_index"] != 1 {
			t.Errorf("expected operation_index=1, got %v", qbErr.Context["operation_index"])
		}
	})

	t.Run("failure operation error context", func(t *testing.T) {
		req := &TxnRequest{
			Compare: []Compare{
				{Key: "test", Target: CompareValue, Result: CompareEqual},
			},
			Failure: []TxnOp{
				{Type: TxnOpGet, EntityType: "", Key: "key"}, // Invalid at index 0
			},
		}

		_, err := client.Execute(ctx, req)
		if err == nil {
			t.Fatal("expected error for invalid operation")
		}

		qbErr, ok := err.(*errors.QBError)
		if !ok {
			t.Fatalf("expected QBError, got %T", err)
		}

		if qbErr.Context["branch"] != "failure" {
			t.Errorf("expected branch='failure', got %v", qbErr.Context["branch"])
		}
	})
}
