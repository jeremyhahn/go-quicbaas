// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package query

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
			return &QueryResponse{Results: []QueryResult{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *QueryRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty entity type",
			req:     &QueryRequest{Index: "email", Operator: OpEqual},
			wantErr: true,
		},
		{
			name:    "empty index",
			req:     &QueryRequest{EntityType: "users", Operator: OpEqual},
			wantErr: true,
		},
		{
			name:    "invalid operator",
			req:     &QueryRequest{EntityType: "users", Index: "email", Operator: Operator(99)},
			wantErr: true,
		},
		{
			name:    "negative limit",
			req:     &QueryRequest{EntityType: "users", Index: "email", Operator: OpEqual, Limit: -1},
			wantErr: true,
		},
		{
			name:    "valid equal query",
			req:     &QueryRequest{EntityType: "users", Index: "email", Value: "test@example.com", Operator: OpEqual},
			wantErr: false,
		},
		{
			name:    "valid prefix query",
			req:     &QueryRequest{EntityType: "users", Index: "name", Value: "Ali", Operator: OpPrefix},
			wantErr: false,
		},
		{
			name:    "valid range query",
			req:     &QueryRequest{EntityType: "users", Index: "age", Value: []int{18, 65}, Operator: OpRange},
			wantErr: false,
		},
		{
			name:    "valid with limit",
			req:     &QueryRequest{EntityType: "users", Index: "email", Operator: OpEqual, Limit: 100},
			wantErr: false,
		},
		{
			name:    "valid with cursor",
			req:     &QueryRequest{EntityType: "users", Index: "email", Operator: OpEqual, Cursor: "abc123"},
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
		name        string
		ctx         context.Context
		transport   *mockTransport
		wantErr     bool
		errCode     errors.ErrorCode
		resultCount int
	}{
		{
			name: "successful query with results",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Execute" {
						t.Errorf("expected method %q, got %q", "Execute", method)
					}
					return &QueryResponse{
						Results: []QueryResult{
							{Key: "user-1", Value: []byte(`{"name":"Alice"}`)},
							{Key: "user-2", Value: []byte(`{"name":"Bob"}`)},
						},
						NextCursor: "cursor123",
						HasMore:    true,
					}, nil
				},
			},
			wantErr:     false,
			resultCount: 2,
		},
		{
			name: "empty results with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:     false,
			resultCount: 0,
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

	validReq := &QueryRequest{
		EntityType: "users",
		Index:      "email",
		Value:      "test@example.com",
		Operator:   OpEqual,
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
			if !tt.wantErr && len(resp.Results) != tt.resultCount {
				t.Errorf("expected %d results, got %d", tt.resultCount, len(resp.Results))
			}
		})
	}
}

func TestOperator_String(t *testing.T) {
	tests := []struct {
		op       Operator
		expected string
	}{
		{OpEqual, "EQUAL"},
		{OpNotEqual, "NOT_EQUAL"},
		{OpLessThan, "LESS_THAN"},
		{OpLessThanOrEqual, "LESS_THAN_OR_EQUAL"},
		{OpGreaterThan, "GREATER_THAN"},
		{OpGreaterThanOrEqual, "GREATER_THAN_OR_EQUAL"},
		{OpPrefix, "PREFIX"},
		{OpRange, "RANGE"},
		{Operator(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.op.String(); got != tt.expected {
				t.Errorf("String() = %q, expected %q", got, tt.expected)
			}
		})
	}
}

func TestQueryResponse_Pagination(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			queryReq := req.(*QueryRequest)
			if queryReq.Cursor == "" {
				// First page
				return &QueryResponse{
					Results: []QueryResult{
						{Key: "user-1", Value: []byte("Alice")},
						{Key: "user-2", Value: []byte("Bob")},
					},
					NextCursor: "page2",
					HasMore:    true,
				}, nil
			}
			// Second page
			return &QueryResponse{
				Results: []QueryResult{
					{Key: "user-3", Value: []byte("Carol")},
				},
				NextCursor: "",
				HasMore:    false,
			}, nil
		},
	}

	client := NewClient(mockT)
	ctx := context.Background()

	// First page
	resp, err := client.Execute(ctx, &QueryRequest{
		EntityType: "users",
		Index:      "active",
		Value:      true,
		Operator:   OpEqual,
		Limit:      2,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != 2 {
		t.Errorf("expected 2 results, got %d", len(resp.Results))
	}
	if !resp.HasMore {
		t.Error("expected HasMore to be true")
	}
	if resp.NextCursor != "page2" {
		t.Errorf("expected NextCursor='page2', got %q", resp.NextCursor)
	}

	// Second page
	resp, err = client.Execute(ctx, &QueryRequest{
		EntityType: "users",
		Index:      "active",
		Value:      true,
		Operator:   OpEqual,
		Limit:      2,
		Cursor:     resp.NextCursor,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Errorf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.HasMore {
		t.Error("expected HasMore to be false")
	}
	if resp.NextCursor != "" {
		t.Errorf("expected empty NextCursor, got %q", resp.NextCursor)
	}
}

func TestQueryResult_Values(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &QueryResponse{
				Results: []QueryResult{
					{Key: "users/alice", Value: []byte(`{"id":"alice","name":"Alice","email":"alice@example.com"}`)},
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.Execute(context.Background(), &QueryRequest{
		EntityType: "users",
		Index:      "email",
		Value:      "alice@example.com",
		Operator:   OpEqual,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}

	result := resp.Results[0]
	if result.Key != "users/alice" {
		t.Errorf("expected key 'users/alice', got %q", result.Key)
	}
	if len(result.Value) == 0 {
		t.Error("expected non-empty value")
	}
}
