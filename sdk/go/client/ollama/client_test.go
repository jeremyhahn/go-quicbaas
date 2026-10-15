// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

package ollama

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
// Embed Tests
// =============================================================================

func TestClient_Embed_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &EmbedResponse{
				Embedding:  []float32{0.1, 0.2, 0.3},
				Model:      "nomic-embed-text",
				Dimensions: 3,
				Timestamp:  time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *EmbedRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty text",
			req:     &EmbedRequest{Text: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &EmbedRequest{Text: "Hello, world!"},
			wantErr: false,
		},
		{
			name:    "valid request with long text",
			req:     &EmbedRequest{Text: "This is a longer text that should still work fine for embedding generation."},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Embed(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Embed() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Embed(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful embed",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if service != serviceName {
						t.Errorf("expected service %q, got %q", serviceName, service)
					}
					if method != "Embed" {
						t.Errorf("expected method %q, got %q", "Embed", method)
					}
					return &EmbedResponse{
						Embedding:      []float32{0.1, 0.2, 0.3},
						Model:          "nomic-embed-text",
						Dimensions:     3,
						ProcessingTime: 100 * time.Millisecond,
						Timestamp:      time.Now(),
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
					return nil, errors.NewUnavailable("test", "service unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &EmbedRequest{Text: "Hello, world!"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Embed(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Embed() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// EmbedWithModel Tests
// =============================================================================

func TestClient_EmbedWithModel_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &EmbedResponse{
				Embedding:  []float32{0.1, 0.2, 0.3},
				Model:      "nomic-embed-text",
				Dimensions: 3,
				Timestamp:  time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *EmbedWithModelRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty text",
			req:     &EmbedWithModelRequest{Text: "", Model: "nomic-embed-text"},
			wantErr: true,
		},
		{
			name:    "empty model",
			req:     &EmbedWithModelRequest{Text: "Hello", Model: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &EmbedWithModelRequest{Text: "Hello, world!", Model: "nomic-embed-text"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.EmbedWithModel(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("EmbedWithModel() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_EmbedWithModel(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful embed with model",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "EmbedWithModel" {
						t.Errorf("expected method %q, got %q", "EmbedWithModel", method)
					}
					embedReq := req.(*EmbedWithModelRequest)
					return &EmbedResponse{
						Embedding:      []float32{0.1, 0.2, 0.3, 0.4},
						Model:          embedReq.Model,
						Dimensions:     4,
						ProcessingTime: 50 * time.Millisecond,
						Timestamp:      time.Now(),
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
			name: "model not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "model not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	validReq := &EmbedWithModelRequest{Text: "Hello, world!", Model: "nomic-embed-text"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.EmbedWithModel(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("EmbedWithModel() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// EmbedBatch Tests
// =============================================================================

func TestClient_EmbedBatch_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &EmbedBatchResponse{
				Embeddings: [][]float32{{0.1, 0.2}, {0.3, 0.4}},
				Model:      "nomic-embed-text",
				Dimensions: 2,
				Count:      2,
				Timestamp:  time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *EmbedBatchRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty texts",
			req:     &EmbedBatchRequest{Texts: []string{}},
			wantErr: true,
		},
		{
			name:    "nil texts",
			req:     &EmbedBatchRequest{Texts: nil},
			wantErr: true,
		},
		{
			name:    "empty text in batch",
			req:     &EmbedBatchRequest{Texts: []string{"Hello", "", "World"}},
			wantErr: true,
		},
		{
			name:    "valid request without model",
			req:     &EmbedBatchRequest{Texts: []string{"Hello", "World"}},
			wantErr: false,
		},
		{
			name:    "valid request with model",
			req:     &EmbedBatchRequest{Texts: []string{"Hello", "World"}, Model: "nomic-embed-text"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.EmbedBatch(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("EmbedBatch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_EmbedBatch(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		wantCount int
	}{
		{
			name: "successful batch embed",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "EmbedBatch" {
						t.Errorf("expected method %q, got %q", "EmbedBatch", method)
					}
					batchReq := req.(*EmbedBatchRequest)
					embeddings := make([][]float32, len(batchReq.Texts))
					for i := range embeddings {
						embeddings[i] = []float32{0.1, 0.2, 0.3}
					}
					return &EmbedBatchResponse{
						Embeddings:     embeddings,
						Model:          "nomic-embed-text",
						Dimensions:     3,
						Count:          len(embeddings),
						ProcessingTime: 200 * time.Millisecond,
						Timestamp:      time.Now(),
					}, nil
				},
			},
			wantErr:   false,
			wantCount: 3,
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
					return &EmbedResponse{}, nil // wrong type
				},
			},
			wantErr: true,
			errCode: errors.CodeInternal,
		},
	}

	validReq := &EmbedBatchRequest{Texts: []string{"Hello", "World", "Test"}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.EmbedBatch(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("EmbedBatch() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr {
				if resp == nil {
					t.Error("expected non-nil response")
				} else if resp.Count != tt.wantCount {
					t.Errorf("expected count %d, got %d", tt.wantCount, resp.Count)
				}
			}
		})
	}
}

// =============================================================================
// ListModels Tests
// =============================================================================

func TestClient_ListModels_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListModelsResponse{
				Models:    []ModelInfo{},
				Count:     0,
				Timestamp: time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *ListModelsRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid empty request",
			req:     &ListModelsRequest{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.ListModels(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListModels() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_ListModels(t *testing.T) {
	tests := []struct {
		name       string
		ctx        context.Context
		transport  *mockTransport
		wantErr    bool
		errCode    errors.ErrorCode
		modelCount int
	}{
		{
			name: "successful list with models",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "ListModels" {
						t.Errorf("expected method %q, got %q", "ListModels", method)
					}
					return &ListModelsResponse{
						Models: []ModelInfo{
							{Name: "nomic-embed-text", Size: 1000000, Digest: "abc123", ModifiedAt: time.Now()},
							{Name: "llama2", Size: 5000000, Digest: "def456", ModifiedAt: time.Now()},
						},
						Count:     2,
						Timestamp: time.Now(),
					}, nil
				},
			},
			wantErr:    false,
			modelCount: 2,
		},
		{
			name: "empty list with nil response",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:    false,
			modelCount: 0,
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
		{
			name: "transport error",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "service unavailable")
				},
			},
			wantErr: true,
			errCode: errors.CodeUnavailable,
		},
	}

	validReq := &ListModelsRequest{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.ListModels(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListModels() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr && len(resp.Models) != tt.modelCount {
				t.Errorf("expected %d models, got %d", tt.modelCount, len(resp.Models))
			}
		})
	}
}

// =============================================================================
// GetModelInfo Tests
// =============================================================================

func TestClient_GetModelInfo_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetModelInfoResponse{
				Model:     "nomic-embed-text",
				Details:   ModelDetails{Family: "nomic"},
				Timestamp: time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *GetModelInfoRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "empty model",
			req:     &GetModelInfoRequest{Model: ""},
			wantErr: true,
		},
		{
			name:    "valid request",
			req:     &GetModelInfoRequest{Model: "nomic-embed-text"},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.GetModelInfo(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetModelInfo() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_GetModelInfo(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
	}{
		{
			name: "successful get model info",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "GetModelInfo" {
						t.Errorf("expected method %q, got %q", "GetModelInfo", method)
					}
					infoReq := req.(*GetModelInfoRequest)
					return &GetModelInfoResponse{
						Model: infoReq.Model,
						Details: ModelDetails{
							License:           "Apache-2.0",
							Format:            "gguf",
							Family:            "nomic",
							ParameterSize:     "137M",
							QuantizationLevel: "Q4_0",
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
			name: "model not found",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewNotFound("test", "model not found")
				},
			},
			wantErr: true,
			errCode: errors.CodeNotFound,
		},
	}

	validReq := &GetModelInfoRequest{Model: "nomic-embed-text"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.GetModelInfo(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetModelInfo() error = %v, wantErr %v", err, tt.wantErr)
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

// =============================================================================
// Health Tests
// =============================================================================

func TestClient_Health_Validation(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &HealthResponse{
				Available: true,
				Version:   "0.1.0",
				Timestamp: time.Now(),
			}, nil
		},
	}
	client := NewClient(mockT)
	ctx := context.Background()

	tests := []struct {
		name    string
		req     *HealthRequest
		wantErr bool
	}{
		{
			name:    "nil request",
			req:     nil,
			wantErr: true,
		},
		{
			name:    "valid empty request",
			req:     &HealthRequest{},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := client.Health(ctx, tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("Health() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if !errors.IsInvalidInput(err) {
					t.Errorf("expected InvalidInput error, got %v", err)
				}
			}
		})
	}
}

func TestClient_Health(t *testing.T) {
	tests := []struct {
		name      string
		ctx       context.Context
		transport *mockTransport
		wantErr   bool
		errCode   errors.ErrorCode
		available bool
	}{
		{
			name: "successful health check - available",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "Health" {
						t.Errorf("expected method %q, got %q", "Health", method)
					}
					return &HealthResponse{
						Available:   true,
						Version:     "0.1.0",
						BaseURL:     "http://localhost:11434",
						LastChecked: time.Now(),
						Timestamp:   time.Now(),
					}, nil
				},
			},
			wantErr:   false,
			available: true,
		},
		{
			name: "successful health check - unavailable",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &HealthResponse{
						Available: false,
						Error:     "connection refused",
						Timestamp: time.Now(),
					}, nil
				},
			},
			wantErr:   false,
			available: false,
		},
		{
			name: "nil response returns unavailable",
			ctx:  context.Background(),
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			wantErr:   false,
			available: false,
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

	validReq := &HealthRequest{}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			resp, err := client.Health(tt.ctx, validReq)
			if (err != nil) != tt.wantErr {
				t.Errorf("Health() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil {
				if errors.Code(err) != tt.errCode {
					t.Errorf("expected error code %v, got %v", tt.errCode, errors.Code(err))
				}
			}
			if !tt.wantErr {
				if resp == nil {
					t.Error("expected non-nil response")
				} else if resp.Available != tt.available {
					t.Errorf("expected available=%v, got %v", tt.available, resp.Available)
				}
			}
		})
	}
}

// =============================================================================
// IsAvailable Tests
// =============================================================================

func TestClient_IsAvailable(t *testing.T) {
	tests := []struct {
		name      string
		transport *mockTransport
		want      bool
	}{
		{
			name: "available",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					if method != "IsAvailable" {
						t.Errorf("expected method %q, got %q", "IsAvailable", method)
					}
					return &HealthResponse{Available: true, Version: "0.5.1"}, nil
				},
			},
			want: true,
		},
		{
			name: "unavailable",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &HealthResponse{Available: false, Error: "connection refused"}, nil
				},
			},
			want: false,
		},
		{
			// The MCP transport serves IsAvailable from the health tool.
			name: "available via health response",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &HealthResponse{Available: true, Version: "0.5.1"}, nil
				},
			},
			want: true,
		},
		{
			name: "unavailable via health response",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return &HealthResponse{Available: false, Error: "connection refused"}, nil
				},
			},
			want: false,
		},
		{
			name: "transport error",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, errors.NewUnavailable("test", "connection failed")
				},
			},
			want: false,
		},
		{
			name: "nil response",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
			want: false,
		},
		{
			name: "invalid response type",
			transport: &mockTransport{
				requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
					return "invalid", nil
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := NewClient(tt.transport)
			got := client.IsAvailable()
			if got != tt.want {
				t.Errorf("IsAvailable() = %v, want %v", got, tt.want)
			}
		})
	}
}

// =============================================================================
// Response Field Tests
// =============================================================================

func TestEmbedResponse_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &EmbedResponse{
				Embedding:      []float32{0.1, 0.2, 0.3, 0.4, 0.5},
				Model:          "nomic-embed-text",
				Dimensions:     5,
				ProcessingTime: 150 * time.Millisecond,
				Timestamp:      time.Now(),
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.Embed(context.Background(), &EmbedRequest{Text: "Test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Embedding) != 5 {
		t.Errorf("expected 5 dimensions, got %d", len(resp.Embedding))
	}
	if resp.Model != "nomic-embed-text" {
		t.Errorf("expected model 'nomic-embed-text', got %q", resp.Model)
	}
	if resp.Dimensions != 5 {
		t.Errorf("expected dimensions=5, got %d", resp.Dimensions)
	}
	if resp.ProcessingTime != 150*time.Millisecond {
		t.Errorf("expected processing time 150ms, got %v", resp.ProcessingTime)
	}
	if resp.Timestamp.IsZero() {
		t.Error("expected non-zero timestamp")
	}
}

func TestModelInfo_Fields(t *testing.T) {
	now := time.Now()
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListModelsResponse{
				Models: []ModelInfo{
					{
						Name:       "nomic-embed-text",
						Size:       274530336,
						Digest:     "0a109f422b47e3a30ba2b10eca18548e944e8a23073ee3f3e947efcf3c45e59f",
						ModifiedAt: now,
					},
				},
				Count:     1,
				Timestamp: now,
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.ListModels(context.Background(), &ListModelsRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(resp.Models))
	}

	model := resp.Models[0]
	if model.Name != "nomic-embed-text" {
		t.Errorf("expected name='nomic-embed-text', got %q", model.Name)
	}
	if model.Size != 274530336 {
		t.Errorf("expected size=274530336, got %d", model.Size)
	}
	if model.Digest == "" {
		t.Error("expected non-empty digest")
	}
	if model.ModifiedAt.IsZero() {
		t.Error("expected non-zero modified at")
	}
}

func TestModelDetails_Fields(t *testing.T) {
	mockT := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &GetModelInfoResponse{
				Model: "nomic-embed-text",
				Details: ModelDetails{
					License:           "Apache-2.0",
					Modelfile:         "FROM nomic-embed-text",
					Format:            "gguf",
					Family:            "nomic",
					ParameterSize:     "137M",
					QuantizationLevel: "Q4_0",
				},
				Timestamp: time.Now(),
			}, nil
		},
	}

	client := NewClient(mockT)
	resp, err := client.GetModelInfo(context.Background(), &GetModelInfoRequest{Model: "nomic-embed-text"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Model != "nomic-embed-text" {
		t.Errorf("expected model='nomic-embed-text', got %q", resp.Model)
	}
	if resp.Details.License != "Apache-2.0" {
		t.Errorf("expected license='Apache-2.0', got %q", resp.Details.License)
	}
	if resp.Details.Format != "gguf" {
		t.Errorf("expected format='gguf', got %q", resp.Details.Format)
	}
	if resp.Details.Family != "nomic" {
		t.Errorf("expected family='nomic', got %q", resp.Details.Family)
	}
	if resp.Details.ParameterSize != "137M" {
		t.Errorf("expected parameter_size='137M', got %q", resp.Details.ParameterSize)
	}
	if resp.Details.QuantizationLevel != "Q4_0" {
		t.Errorf("expected quantization_level='Q4_0', got %q", resp.Details.QuantizationLevel)
	}
}

// =============================================================================
// intToString Tests
// =============================================================================

func TestIntToString(t *testing.T) {
	tests := []struct {
		input int
		want  string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
		{-1, "-1"},
		{-123, "-123"},
		{1000000, "1000000"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := intToString(tt.input)
			if got != tt.want {
				t.Errorf("intToString(%d) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
