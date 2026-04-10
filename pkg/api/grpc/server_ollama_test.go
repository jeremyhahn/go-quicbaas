//go:build rag

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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"errors"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockOllamaService implements service.OllamaService for testing.
type mockOllamaService struct {
	embedFunc          func(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error)
	embedWithModelFunc func(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error)
	embedBatchFunc     func(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error)
	listModelsFunc     func(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error)
	getModelInfoFunc   func(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error)
	healthFunc         func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error)
	isAvailableFunc    func() bool
	defaultModelFunc   func() string
	baseURLFunc        func() string
}

func (m *mockOllamaService) Embed(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, req)
	}
	return &service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3},
		Model:          "nomic-embed-text",
		Dimensions:     3,
		ProcessingTime: 100 * time.Millisecond,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockOllamaService) EmbedWithModel(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
	if m.embedWithModelFunc != nil {
		return m.embedWithModelFunc(ctx, req)
	}
	return &service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3},
		Model:          req.Model,
		Dimensions:     3,
		ProcessingTime: 100 * time.Millisecond,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockOllamaService) EmbedBatch(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
	if m.embedBatchFunc != nil {
		return m.embedBatchFunc(ctx, req)
	}
	embeddings := make([][]float32, len(req.Texts))
	for i := range req.Texts {
		embeddings[i] = []float32{0.1, 0.2, 0.3}
	}
	model := req.Model
	if model == "" {
		model = "nomic-embed-text"
	}
	return &service.OllamaEmbedBatchResponse{
		Embeddings:     embeddings,
		Model:          model,
		Dimensions:     3,
		Count:          len(req.Texts),
		ProcessingTime: 200 * time.Millisecond,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockOllamaService) ListModels(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
	if m.listModelsFunc != nil {
		return m.listModelsFunc(ctx, req)
	}
	return &service.OllamaListModelsResponse{
		Models: []service.OllamaModelInfo{
			{
				Name:       "nomic-embed-text",
				Size:       274000000,
				Digest:     "sha256:abc123",
				ModifiedAt: time.Now(),
			},
		},
		Count:     1,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) GetModelInfo(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
	if m.getModelInfoFunc != nil {
		return m.getModelInfoFunc(ctx, req)
	}
	return &service.OllamaGetModelInfoResponse{
		Model: req.Model,
		Details: service.OllamaModelDetails{
			License:           "Apache-2.0",
			Format:            "gguf",
			Family:            "nomic",
			ParameterSize:     "137M",
			QuantizationLevel: "Q4_0",
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) Health(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
	if m.healthFunc != nil {
		return m.healthFunc(ctx, req)
	}
	return &service.OllamaHealthResponse{
		Available:   true,
		Version:     "0.1.42",
		BaseURL:     "http://localhost:11434",
		LastChecked: time.Now(),
		Timestamp:   time.Now(),
	}, nil
}

func (m *mockOllamaService) IsAvailable() bool {
	if m.isAvailableFunc != nil {
		return m.isAvailableFunc()
	}
	return true
}

func (m *mockOllamaService) DefaultModel() string {
	if m.defaultModelFunc != nil {
		return m.defaultModelFunc()
	}
	return "nomic-embed-text"
}

func (m *mockOllamaService) BaseURL() string {
	if m.baseURLFunc != nil {
		return m.baseURLFunc()
	}
	return "http://localhost:11434"
}

func newTestOllamaServer(t *testing.T) (*OllamaServer, *mockOllamaService) {
	t.Helper()
	logConfig := logger.LogConfig{Level: "error", Format: "json", Output: "stdout"}
	log, err := logger.New(logConfig)
	require.NoError(t, err)
	mockSvc := &mockOllamaService{}
	server := NewOllamaServer(mockSvc, log)
	return server, mockSvc
}

// =============================================================================
// Health Tests
// =============================================================================

func TestOllamaServer_Health_Success(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.Health(ctx, &pb.OllamaHealthRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Available)
	assert.Equal(t, "0.1.42", resp.Version)
	assert.Equal(t, "http://localhost:11434", resp.BaseUrl)
	assert.NotNil(t, resp.LastChecked)
}

func TestOllamaServer_Health_Unavailable(t *testing.T) {
	server, mockSvc := newTestOllamaServer(t)
	ctx := context.Background()

	mockSvc.healthFunc = func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
		return &service.OllamaHealthResponse{
			Available:   false,
			Error:       "connection refused",
			BaseURL:     "http://localhost:11434",
			LastChecked: time.Now(),
			Timestamp:   time.Now(),
		}, nil
	}

	resp, err := server.Health(ctx, &pb.OllamaHealthRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Available)
	assert.Equal(t, "connection refused", resp.Error)
}

// =============================================================================
// ListModels Tests
// =============================================================================

func TestOllamaServer_ListModels_Success(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.ListModels(ctx, &pb.OllamaListModelsRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Models, 1)
	assert.Equal(t, "nomic-embed-text", resp.Models[0].Name)
	assert.Equal(t, int64(274000000), resp.Models[0].Size)
	assert.Equal(t, int32(1), resp.Count)
}

func TestOllamaServer_ListModels_Empty(t *testing.T) {
	server, mockSvc := newTestOllamaServer(t)
	ctx := context.Background()

	mockSvc.listModelsFunc = func(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
		return &service.OllamaListModelsResponse{
			Models:    []service.OllamaModelInfo{},
			Count:     0,
			Timestamp: time.Now(),
		}, nil
	}

	resp, err := server.ListModels(ctx, &pb.OllamaListModelsRequest{})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Models, 0)
	assert.Equal(t, int32(0), resp.Count)
}

// =============================================================================
// GetModelInfo Tests
// =============================================================================

func TestOllamaServer_GetModelInfo_Success(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.GetModelInfo(ctx, &pb.OllamaGetModelInfoRequest{
		Model: "nomic-embed-text",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "nomic-embed-text", resp.Model)
	assert.NotNil(t, resp.Details)
	assert.Equal(t, "Apache-2.0", resp.Details.License)
	assert.Equal(t, "gguf", resp.Details.Format)
	assert.Equal(t, "nomic", resp.Details.Family)
	assert.Equal(t, "137M", resp.Details.ParameterSize)
	assert.Equal(t, "Q4_0", resp.Details.QuantizationLevel)
}

func TestOllamaServer_GetModelInfo_EmptyModel(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.GetModelInfo(ctx, &pb.OllamaGetModelInfoRequest{
		Model: "",
	})

	require.Error(t, err)
	require.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "model name is required")
}

// =============================================================================
// Embed Tests
// =============================================================================

func TestOllamaServer_Embed_Success(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.Embed(ctx, &pb.OllamaEmbedRequest{
		Text: "Hello, world!",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Embedding, 3)
	assert.Equal(t, "nomic-embed-text", resp.Model)
	assert.Equal(t, int32(3), resp.Dimensions)
	assert.NotNil(t, resp.ProcessingTime)
}

func TestOllamaServer_Embed_EmptyText(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.Embed(ctx, &pb.OllamaEmbedRequest{
		Text: "",
	})

	require.Error(t, err)
	require.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "text is required")
}

// =============================================================================
// EmbedWithModel Tests
// =============================================================================

func TestOllamaServer_EmbedWithModel_Success(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.EmbedWithModel(ctx, &pb.OllamaEmbedWithModelRequest{
		Text:  "Hello, world!",
		Model: "mxbai-embed-large",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Embedding, 3)
	assert.Equal(t, "mxbai-embed-large", resp.Model)
	assert.Equal(t, int32(3), resp.Dimensions)
}

func TestOllamaServer_EmbedWithModel_EmptyText(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.EmbedWithModel(ctx, &pb.OllamaEmbedWithModelRequest{
		Text:  "",
		Model: "mxbai-embed-large",
	})

	require.Error(t, err)
	require.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "text is required")
}

func TestOllamaServer_EmbedWithModel_EmptyModel(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.EmbedWithModel(ctx, &pb.OllamaEmbedWithModelRequest{
		Text:  "Hello, world!",
		Model: "",
	})

	require.Error(t, err)
	require.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "model is required")
}

// =============================================================================
// EmbedBatch Tests
// =============================================================================

func TestOllamaServer_EmbedBatch_Success(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.EmbedBatch(ctx, &pb.OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World", "Test"},
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Embeddings, 3)
	assert.Equal(t, "nomic-embed-text", resp.Model)
	assert.Equal(t, int32(3), resp.Dimensions)
	assert.Equal(t, int32(3), resp.Count)
}

func TestOllamaServer_EmbedBatch_WithModel(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.EmbedBatch(ctx, &pb.OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World"},
		Model: "mxbai-embed-large",
	})

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Len(t, resp.Embeddings, 2)
	assert.Equal(t, "mxbai-embed-large", resp.Model)
}

func TestOllamaServer_EmbedBatch_EmptyTexts(t *testing.T) {
	server, _ := newTestOllamaServer(t)
	ctx := context.Background()

	resp, err := server.EmbedBatch(ctx, &pb.OllamaEmbedBatchRequest{
		Texts: []string{},
	})

	require.Error(t, err)
	require.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "texts are required")
}

// =============================================================================
// Interface Compliance Tests
// =============================================================================

func TestOllamaServer_ImplementsInterface(t *testing.T) {
	var _ pb.OllamaServiceServer = (*OllamaServer)(nil)
}

func TestNewOllamaServer(t *testing.T) {
	logConfig := logger.LogConfig{Level: "error", Format: "json", Output: "stdout"}
	log, err := logger.New(logConfig)
	require.NoError(t, err)
	mockSvc := &mockOllamaService{}

	server := NewOllamaServer(mockSvc, log)

	require.NotNil(t, server)
	assert.NotNil(t, server.ollamaSvc)
	assert.NotNil(t, server.logger)
}

// =============================================================================
// convertOllamaError Tests
// =============================================================================

func TestConvertOllamaError_NilError(t *testing.T) {
	err := convertOllamaError(nil)
	assert.Nil(t, err)
}

// --- Merged from coverage_91_target_ollama_test.go ---

// =============================================================================
// Ollama service error paths
// Covers the if err != nil return paths for each Ollama RPC method.
// =============================================================================

func TestOllamaServer_ListModels_ServiceError(t *testing.T) {
	server, mockSvc := newTestOllamaServer(t)
	mockSvc.listModelsFunc = func(_ context.Context, _ *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
		return nil, errors.New("list models failed")
	}

	_, err := server.ListModels(context.Background(), &pb.OllamaListModelsRequest{})
	require.Error(t, err)
}

func TestOllamaServer_GetModelInfo_ServiceError(t *testing.T) {
	server, mockSvc := newTestOllamaServer(t)
	mockSvc.getModelInfoFunc = func(_ context.Context, _ *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
		return nil, errors.New("get model info failed")
	}

	_, err := server.GetModelInfo(context.Background(), &pb.OllamaGetModelInfoRequest{Model: "test-model"})
	require.Error(t, err)
}

func TestOllamaServer_Embed_ServiceError(t *testing.T) {
	server, mockSvc := newTestOllamaServer(t)
	mockSvc.embedFunc = func(_ context.Context, _ *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
		return nil, errors.New("embed failed")
	}

	_, err := server.Embed(context.Background(), &pb.OllamaEmbedRequest{Text: "hello"})
	require.Error(t, err)
}

func TestOllamaServer_EmbedWithModel_ServiceError(t *testing.T) {
	server, mockSvc := newTestOllamaServer(t)
	mockSvc.embedWithModelFunc = func(_ context.Context, _ *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
		return nil, errors.New("embed with model failed")
	}

	_, err := server.EmbedWithModel(context.Background(), &pb.OllamaEmbedWithModelRequest{
		Text:  "hello",
		Model: "test-model",
	})
	require.Error(t, err)
}

func TestOllamaServer_EmbedBatch_ServiceError(t *testing.T) {
	server, mockSvc := newTestOllamaServer(t)
	mockSvc.embedBatchFunc = func(_ context.Context, _ *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
		return nil, errors.New("embed batch failed")
	}

	_, err := server.EmbedBatch(context.Background(), &pb.OllamaEmbedBatchRequest{
		Texts: []string{"hello", "world"},
	})
	require.Error(t, err)
}

func TestOllamaServer_Health_ServiceError(t *testing.T) {
	server, mockSvc := newTestOllamaServer(t)
	mockSvc.healthFunc = func(_ context.Context, _ *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
		return nil, errors.New("health check failed")
	}

	_, err := server.Health(context.Background(), &pb.OllamaHealthRequest{})
	require.Error(t, err)
}
