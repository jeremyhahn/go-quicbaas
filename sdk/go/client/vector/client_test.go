// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package vector

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

func TestClient_Upsert_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &UpsertResponse{ID: "doc-1"}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *UpsertRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "nil vector",
			req:     &UpsertRequest{},
			wantErr: true,
		},
		{
			name: "empty namespace",
			req: &UpsertRequest{Vector: &VectorEntity{
				ID:     "doc-1",
				Vector: []float32{0.1, 0.2},
			}},
			wantErr: true,
		},
		{
			name: "empty ID",
			req: &UpsertRequest{Vector: &VectorEntity{
				Namespace: "docs",
				Vector:    []float32{0.1, 0.2},
			}},
			wantErr: true,
		},
		{
			name: "empty vector data",
			req: &UpsertRequest{Vector: &VectorEntity{
				Namespace: "docs",
				ID:        "doc-1",
			}},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &UpsertRequest{Vector: &VectorEntity{
				Namespace: "docs",
				ID:        "doc-1",
				Vector:    []float32{0.1, 0.2, 0.3},
			}},
			wantErr: false,
		},
		{
			name: "valid with metadata",
			req: &UpsertRequest{Vector: &VectorEntity{
				Namespace: "docs",
				ID:        "doc-1",
				Vector:    []float32{0.1, 0.2, 0.3},
				Metadata:  map[string]string{"title": "Test"},
			}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Upsert(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Upsert() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Upsert(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful upsert",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Upsert" {
						t.Errorf("expected method %q, got %q", "Upsert", method)
					}
					upsertReq := req.(*UpsertRequest)
					return &UpsertResponse{ID: upsertReq.Vector.ID}, nil
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

	validReq := &UpsertRequest{
		Vector: &VectorEntity{
			Namespace: "docs",
			ID:        "doc-1",
			Vector:    []float32{0.1, 0.2, 0.3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Upsert(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Upsert() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_SearchSimilar_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SearchSimilarResponse{Results: []*SearchResult{}}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SearchSimilarRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty namespace",
			req:     &SearchSimilarRequest{QueryVector: []float32{0.1, 0.2}, K: 10},
			wantErr: true,
		},
		{
			name:    "empty query vector",
			req:     &SearchSimilarRequest{Namespace: "docs", K: 10},
			wantErr: true,
		},
		{
			name:    "zero K",
			req:     &SearchSimilarRequest{Namespace: "docs", QueryVector: []float32{0.1, 0.2}, K: 0},
			wantErr: true,
		},
		{
			name:    "negative K",
			req:     &SearchSimilarRequest{Namespace: "docs", QueryVector: []float32{0.1, 0.2}, K: -1},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &SearchSimilarRequest{Namespace: "docs", QueryVector: []float32{0.1, 0.2}, K: 10},
			wantErr: false,
		},
		{
			name: "valid with min score",
			req: &SearchSimilarRequest{
				Namespace:   "docs",
				QueryVector: []float32{0.1, 0.2},
				K:           5,
				MinScore:    0.5,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.SearchSimilar(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SearchSimilar() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_SearchSimilar(t *testing.T) {
	tests := []struct {
		name        string
		ctx         context.Context
		transport   *mockTransport
		wantErr     bool
		errCode     errors.ErrorCode
		resultCount int
	}{
		{
			name: "successful search with results",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "SearchSimilar" {
						t.Errorf("expected method %q, got %q", "SearchSimilar", method)
					}
					return &SearchSimilarResponse{
						Results: []*SearchResult{
							{Vector: &VectorEntity{ID: "doc-1", Vector: []float32{0.1, 0.2}}, Score: 0.95},
							{Vector: &VectorEntity{ID: "doc-2", Vector: []float32{0.3, 0.4}}, Score: 0.85},
						},
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
			name: "invalid response type",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &SearchSimilarRequest{Namespace: "docs", QueryVector: []float32{0.1, 0.2}, K: 10}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.SearchSimilar(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("SearchSimilar() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_Delete_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &DeleteResponse{}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *DeleteRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty namespace",
			req:     &DeleteRequest{ID: "doc-1"},
			wantErr: true,
		},
		{
			name:    "empty ID",
			req:     &DeleteRequest{Namespace: "docs"},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &DeleteRequest{Namespace: "docs", ID: "doc-1"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Delete(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_Delete(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful delete",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Delete" {
						t.Errorf("expected method %q, got %q", "Delete", method)
					}
					return &DeleteResponse{Deleted: true}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "successful delete with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
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
			name: "not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "vector not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
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

	validReq := &DeleteRequest{Namespace: "docs", ID: "doc-1"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			_, err := client.Delete(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Delete() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
		})
	}
}

func TestSearchResult_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SearchSimilarResponse{
				Results: []*SearchResult{
					{
						Vector: &VectorEntity{
							ID:     "doc-1",
							Vector: []float32{0.1, 0.2, 0.3},
							Metadata: map[string]string{
								"title":    "Test Document",
								"category": "tech",
							},
						},
						Score: 0.95,
					},
				},
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.SearchSimilar(context.Background(), &SearchSimilarRequest{
		Namespace:   "docs",
		QueryVector: []float32{0.1, 0.2, 0.3},
		K:           10,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}

	result := resp.Results[0]
	if result.Vector.ID != "doc-1" {
		t.Errorf("expected ID='doc-1', got %q", result.Vector.ID)
	}
	if result.Score != 0.95 {
		t.Errorf("expected Score=0.95, got %f", result.Score)
	}
	if len(result.Vector.Vector) != 3 {
		t.Errorf("expected 3-dimensional vector, got %d", len(result.Vector.Vector))
	}
	if result.Vector.Metadata["title"] != "Test Document" {
		t.Errorf("expected title='Test Document', got %v", result.Vector.Metadata["title"])
	}
}
