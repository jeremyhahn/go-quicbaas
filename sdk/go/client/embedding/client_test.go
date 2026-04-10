// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package embedding

import (
	"context"
	"testing"
	"time"

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

func TestClient_GenerateEmbedding_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GenerateEmbeddingResponse{
				Embedding: []float32{0.1, 0.2, 0.3},
				Metadata:  &Metadata{Model: "test", Provider: ProviderVoyage, Dimensions: 3},
				Timestamp: time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GenerateEmbeddingRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty text",
			req:     &GenerateEmbeddingRequest{},
			wantErr: true,
		},
		{
			name:    "valid request with text only",
			req:     &GenerateEmbeddingRequest{Text: "test text"},
			wantErr: false,
		},
		{
			name: "valid request with all fields",
			req: &GenerateEmbeddingRequest{
				Text:      "test text",
				Provider:  ProviderVoyage,
				InputType: InputTypeDocument,
				Metadata:  map[string]string{"key": "value"},
				Source:    "test.md",
				Language:  "en",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GenerateEmbedding(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateEmbedding() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GenerateEmbedding(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful embedding generation",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "GenerateEmbedding" {
						t.Errorf("expected method %q, got %q", "GenerateEmbedding", method)
					}
					return &GenerateEmbeddingResponse{
						Embedding: []float32{0.1, 0.2, 0.3},
						Metadata:  &Metadata{Model: "voyage-2", Provider: ProviderVoyage, Dimensions: 3},
						Timestamp: time.Now(),
					}, nil
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
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "provider unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &GenerateEmbeddingRequest{Text: "test text"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GenerateEmbedding(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("GenerateEmbedding() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_BatchGenerateEmbeddings_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BatchGenerateEmbeddingsResponse{
				Results:      []*BatchEmbeddingResult{},
				SuccessCount: 0,
				Timestamp:    time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *BatchGenerateEmbeddingsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty items",
			req:     &BatchGenerateEmbeddingsRequest{Items: []*BatchEmbeddingItem{}},
			wantErr: true,
		},
		{
			name: "nil item in slice",
			req: &BatchGenerateEmbeddingsRequest{
				Items: []*BatchEmbeddingItem{nil},
			},
			wantErr: true,
		},
		{
			name: "item with empty ID",
			req: &BatchGenerateEmbeddingsRequest{
				Items: []*BatchEmbeddingItem{{Text: "text"}},
			},
			wantErr: true,
		},
		{
			name: "item with empty text",
			req: &BatchGenerateEmbeddingsRequest{
				Items: []*BatchEmbeddingItem{{ID: "id-1"}},
			},
			wantErr: true,
		},
		{
			name: "valid request",
			req: &BatchGenerateEmbeddingsRequest{
				Items: []*BatchEmbeddingItem{
					{ID: "id-1", Text: "text one"},
					{ID: "id-2", Text: "text two"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid request with optional fields",
			req: &BatchGenerateEmbeddingsRequest{
				Items: []*BatchEmbeddingItem{
					{ID: "id-1", Text: "text", InputType: InputTypeCode, Source: "main.go"},
				},
				Provider: ProviderOpenAI,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.BatchGenerateEmbeddings(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("BatchGenerateEmbeddings() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_BatchGenerateEmbeddings(t *testing.T) {
	tests := []struct {
		name         string
		ctx          context.Context
		transport    *mockTransport
		wantErr      bool
		errCode      errors.ErrorCode
		successCount int
	}{
		{
			name: "successful batch generation",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "BatchGenerateEmbeddings" {
						t.Errorf("expected method %q, got %q", "BatchGenerateEmbeddings", method)
					}
					return &BatchGenerateEmbeddingsResponse{
						Results: []*BatchEmbeddingResult{
							{ID: "id-1", Embedding: []float32{0.1, 0.2}},
							{ID: "id-2", Embedding: []float32{0.3, 0.4}},
						},
						SuccessCount: 2,
						Timestamp:    time.Now(),
					}, nil
				},
			},
			wantErr:      false,
			successCount: 2,
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
					return 123, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "id-1", Text: "text one"},
			{ID: "id-2", Text: "text two"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.BatchGenerateEmbeddings(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("BatchGenerateEmbeddings() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp.SuccessCount != tt.successCount {
				t.Errorf("expected success count %d, got %d", tt.successCount, resp.SuccessCount)
			}
		})
	}
}

func TestClient_ListProviders_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListProvidersResponse{
				Providers:       []*ProviderInfo{},
				DefaultProvider: "voyage",
				Timestamp:       time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ListProvidersRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &ListProvidersRequest{},
			wantErr: false,
		},
		{
			name:    "valid request with include unavailable",
			req:     &ListProvidersRequest{IncludeUnavailable: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ListProviders(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListProviders() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_ListProviders(t *testing.T) {
	tests := []struct {
		name          string
		ctx           context.Context
		transport     *mockTransport
		wantErr       bool
		errCode       errors.ErrorCode
		providerCount int
	}{
		{
			name: "successful list with providers",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ListProviders" {
						t.Errorf("expected method %q, got %q", "ListProviders", method)
					}
					return &ListProvidersResponse{
						Providers: []*ProviderInfo{
							{Name: "voyage", Type: ProviderVoyage, Status: StatusAvailable},
							{Name: "openai", Type: ProviderOpenAI, Status: StatusAvailable},
						},
						DefaultProvider: "voyage",
						Timestamp:       time.Now(),
					}, nil
				},
			},
			wantErr:       false,
			providerCount: 2,
		},
		{
			name: "nil response returns empty list",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:       false,
			providerCount: 0,
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
					return "invalid", nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ListProviders(tt.ctx, &ListProvidersRequest{})
			if (err != nil) != tt.wantErr {
				t.Errorf("ListProviders() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Providers) != tt.providerCount {
				t.Errorf("expected %d providers, got %d", tt.providerCount, len(resp.Providers))
			}
		})
	}
}

func TestClient_GetProviderInfo_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetProviderInfoResponse{
				Info:      &ProviderInfo{Name: "voyage", Type: ProviderVoyage},
				Timestamp: time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetProviderInfoRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty provider",
			req:     &GetProviderInfoRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetProviderInfoRequest{Provider: ProviderVoyage},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetProviderInfo(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetProviderInfo() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_GetProviderInfo(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get provider info",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetProviderInfo" {
						t.Errorf("expected method %q, got %q", "GetProviderInfo", method)
					}
					return &GetProviderInfoResponse{
						Info: &ProviderInfo{
							Name:       "voyage",
							Type:       ProviderVoyage,
							Model:      "voyage-2",
							Dimensions: 1024,
							Status:     StatusAvailable,
						},
						Timestamp: time.Now(),
					}, nil
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
			name: "provider not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "provider not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	validReq := &GetProviderInfoRequest{Provider: ProviderVoyage}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetProviderInfo(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetProviderInfo() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_SetDefaultProvider_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &SetDefaultProviderResponse{
				Provider:         ProviderOllama,
				PreviousProvider: ProviderVoyage,
				Timestamp:        time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *SetDefaultProviderRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty provider",
			req:     &SetDefaultProviderRequest{},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &SetDefaultProviderRequest{Provider: ProviderOllama},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.SetDefaultProvider(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetDefaultProvider() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_SetDefaultProvider(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful set default provider",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "SetDefaultProvider" {
						t.Errorf("expected method %q, got %q", "SetDefaultProvider", method)
					}
					return &SetDefaultProviderResponse{
						Provider:         ProviderOllama,
						PreviousProvider: ProviderVoyage,
						Timestamp:        time.Now(),
					}, nil
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
			name: "provider not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "provider not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	validReq := &SetDefaultProviderRequest{Provider: ProviderOllama}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.SetDefaultProvider(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetDefaultProvider() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_GetCacheStats_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetEmbeddingCacheStatsResponse{
				Enabled:   true,
				Size:      100,
				Capacity:  1000,
				HitRate:   0.85,
				Timestamp: time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetEmbeddingCacheStatsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetEmbeddingCacheStatsRequest{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetCacheStats(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCacheStats() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_GetCacheStats(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get cache stats",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetCacheStats" {
						t.Errorf("expected method %q, got %q", "GetCacheStats", method)
					}
					return &GetEmbeddingCacheStatsResponse{
						Enabled:   true,
						Size:      100,
						Capacity:  1000,
						Hits:      850,
						Misses:    150,
						HitRate:   0.85,
						Evictions: 50,
						Timestamp: time.Now(),
					}, nil
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
					return struct{}{}, nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetCacheStats(tt.ctx, &GetEmbeddingCacheStatsRequest{})
			if (err != nil) != tt.wantErr {
				t.Errorf("GetCacheStats() error = %v, wantErr %v", err, tt.wantErr)
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

func TestClient_ClearCache_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ClearEmbeddingCacheResponse{
				EntriesCleared: 100,
				Timestamp:      time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ClearEmbeddingCacheRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid request empty",
			req:     &ClearEmbeddingCacheRequest{},
			wantErr: false,
		},
		{
			name:    "valid request with provider",
			req:     &ClearEmbeddingCacheRequest{Provider: ProviderVoyage},
			wantErr: false,
		},
		{
			name:    "valid request with all flag",
			req:     &ClearEmbeddingCacheRequest{All: true},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ClearCache(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ClearCache() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestClient_ClearCache(t *testing.T) {
	tests := []struct {
		name           string
		ctx            context.Context
		transport      *mockTransport
		wantErr        bool
		errCode        errors.ErrorCode
		entriesCleared int
	}{
		{
			name: "successful clear cache",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ClearCache" {
						t.Errorf("expected method %q, got %q", "ClearCache", method)
					}
					return &ClearEmbeddingCacheResponse{
						EntriesCleared: 150,
						Timestamp:      time.Now(),
					}, nil
				},
			},
			wantErr:        false,
			entriesCleared: 150,
		},
		{
			name: "successful clear cache for specific provider",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					clearReq := req.(*ClearEmbeddingCacheRequest)
					return &ClearEmbeddingCacheResponse{
						EntriesCleared: 50,
						Provider:       clearReq.Provider,
						Timestamp:      time.Now(),
					}, nil
				},
			},
			wantErr:        false,
			entriesCleared: 50,
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
					return []byte("invalid"), nil
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ClearCache(tt.ctx, &ClearEmbeddingCacheRequest{All: true})
			if (err != nil) != tt.wantErr {
				t.Errorf("ClearCache() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && resp.EntriesCleared != tt.entriesCleared {
				t.Errorf("expected entries cleared %d, got %d", tt.entriesCleared, resp.EntriesCleared)
			}
		})
	}
}

func TestMetadata_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GenerateEmbeddingResponse{
				Embedding: []float32{0.1, 0.2, 0.3, 0.4},
				Metadata: &Metadata{
					Model:          "voyage-2",
					Provider:       ProviderVoyage,
					Dimensions:     4,
					Tokens:         15,
					Source:         "test.md",
					Language:       "en",
					InputType:      InputTypeDocument,
					Tags:           map[string]string{"category": "test"},
					CacheHit:       true,
					ProcessingTime: 50 * time.Millisecond,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{
		Text: "test text",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Embedding) != 4 {
		t.Errorf("expected 4-dimensional embedding, got %d", len(resp.Embedding))
	}

	m := resp.Metadata
	if m.Model != "voyage-2" {
		t.Errorf("expected model='voyage-2', got %q", m.Model)
	}
	if m.Provider != ProviderVoyage {
		t.Errorf("expected provider=voyage, got %q", m.Provider)
	}
	if m.Dimensions != 4 {
		t.Errorf("expected dimensions=4, got %d", m.Dimensions)
	}
	if m.Tokens != 15 {
		t.Errorf("expected tokens=15, got %d", m.Tokens)
	}
	if !m.CacheHit {
		t.Error("expected cache hit to be true")
	}
	if m.Tags["category"] != "test" {
		t.Errorf("expected tag category='test', got %v", m.Tags["category"])
	}
}

func TestProviderInfo_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			lastErr := time.Now().Add(-time.Hour)
			return &GetProviderInfoResponse{
				Info: &ProviderInfo{
					Name:               "voyage",
					Type:               ProviderVoyage,
					Model:              "voyage-2",
					Dimensions:         1024,
					MaxTokens:          8000,
					MaxBatchSize:       128,
					Status:             StatusAvailable,
					APIURL:             "https://api.voyageai.com",
					RateLimitPerMinute: 1000,
					Timeout:            30 * time.Second,
					IsDefault:          true,
					LastError:          "",
					LastErrorTime:      &lastErr,
					SuccessCount:       10000,
					ErrorCount:         5,
					AvgLatency:         100 * time.Millisecond,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.GetProviderInfo(context.Background(), &GetProviderInfoRequest{
		Provider: ProviderVoyage,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info := resp.Info
	if info.Name != "voyage" {
		t.Errorf("expected name='voyage', got %q", info.Name)
	}
	if info.Type != ProviderVoyage {
		t.Errorf("expected type=voyage, got %q", info.Type)
	}
	if info.Dimensions != 1024 {
		t.Errorf("expected dimensions=1024, got %d", info.Dimensions)
	}
	if info.MaxBatchSize != 128 {
		t.Errorf("expected max batch size=128, got %d", info.MaxBatchSize)
	}
	if info.Status != StatusAvailable {
		t.Errorf("expected status=available, got %q", info.Status)
	}
	if !info.IsDefault {
		t.Error("expected is default to be true")
	}
	if info.SuccessCount != 10000 {
		t.Errorf("expected success count=10000, got %d", info.SuccessCount)
	}
}

func TestBatchEmbeddingResult_WithError(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &BatchGenerateEmbeddingsResponse{
				Results: []*BatchEmbeddingResult{
					{ID: "id-1", Embedding: []float32{0.1, 0.2}},
					{ID: "id-2", Error: "token limit exceeded"},
				},
				SuccessCount:   1,
				ErrorCount:     1,
				TotalTokens:    100,
				ProcessingTime: 100 * time.Millisecond,
				Timestamp:      time.Now(),
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.BatchGenerateEmbeddings(context.Background(), &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "id-1", Text: "short text"},
			{ID: "id-2", Text: "very long text that exceeds token limit"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.SuccessCount != 1 {
		t.Errorf("expected success count=1, got %d", resp.SuccessCount)
	}
	if resp.ErrorCount != 1 {
		t.Errorf("expected error count=1, got %d", resp.ErrorCount)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if resp.Results[0].Error != "" {
		t.Errorf("expected first result to have no error, got %q", resp.Results[0].Error)
	}
	if resp.Results[1].Error == "" {
		t.Error("expected second result to have error")
	}
}

func TestCacheStats_AllFields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetEmbeddingCacheStatsResponse{
				Enabled:            true,
				Size:               500,
				Capacity:           1000,
				Hits:               4500,
				Misses:             500,
				HitRate:            0.9,
				Evictions:          100,
				MemoryBytes:        52428800,
				CompressionEnabled: true,
				CompressionRatio:   0.65,
				Timestamp:          time.Now(),
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.GetCacheStats(context.Background(), &GetEmbeddingCacheStatsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Enabled {
		t.Error("expected cache to be enabled")
	}
	if resp.Size != 500 {
		t.Errorf("expected size=500, got %d", resp.Size)
	}
	if resp.Capacity != 1000 {
		t.Errorf("expected capacity=1000, got %d", resp.Capacity)
	}
	if resp.HitRate != 0.9 {
		t.Errorf("expected hit rate=0.9, got %f", resp.HitRate)
	}
	if !resp.CompressionEnabled {
		t.Error("expected compression to be enabled")
	}
	if resp.MemoryBytes != 52428800 {
		t.Errorf("expected memory bytes=52428800, got %d", resp.MemoryBytes)
	}
}
