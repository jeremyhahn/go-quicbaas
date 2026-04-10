//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Mock Ollama Service Implementations
// =============================================================================

// mockOllamaService is a configurable mock implementation of service.OllamaService.
type mockOllamaService struct {
	embedFn          func(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error)
	embedWithModelFn func(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error)
	embedBatchFn     func(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error)
	listModelsFn     func(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error)
	getModelInfoFn   func(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error)
	healthFn         func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error)
	isAvailableFn    func() bool
	defaultModelFn   func() string
	baseURLFn        func() string
}

func (m *mockOllamaService) Embed(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
	if m.embedFn != nil {
		return m.embedFn(ctx, req)
	}
	return &service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Model:          "nomic-embed-text",
		Dimensions:     5,
		ProcessingTime: 10 * time.Millisecond,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockOllamaService) EmbedWithModel(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
	if m.embedWithModelFn != nil {
		return m.embedWithModelFn(ctx, req)
	}
	return &service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Model:          req.Model,
		Dimensions:     5,
		ProcessingTime: 15 * time.Millisecond,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockOllamaService) EmbedBatch(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
	if m.embedBatchFn != nil {
		return m.embedBatchFn(ctx, req)
	}
	embeddings := make([][]float32, len(req.Texts))
	for i := range req.Texts {
		embeddings[i] = []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	}
	return &service.OllamaEmbedBatchResponse{
		Embeddings:     embeddings,
		Model:          "nomic-embed-text",
		Dimensions:     5,
		Count:          len(req.Texts),
		ProcessingTime: 50 * time.Millisecond,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockOllamaService) ListModels(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
	if m.listModelsFn != nil {
		return m.listModelsFn(ctx, req)
	}
	return &service.OllamaListModelsResponse{
		Models: []service.OllamaModelInfo{
			{Name: "nomic-embed-text", Size: 274000000, Digest: "abc123", ModifiedAt: time.Now()},
			{Name: "llama2:7b", Size: 3800000000, Digest: "def456", ModifiedAt: time.Now()},
		},
		Count:     2,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) GetModelInfo(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
	if m.getModelInfoFn != nil {
		return m.getModelInfoFn(ctx, req)
	}
	return &service.OllamaGetModelInfoResponse{
		Model: req.Model,
		Details: service.OllamaModelDetails{
			License:           "Apache-2.0",
			Modelfile:         "FROM nomic-embed-text",
			Format:            "gguf",
			Family:            "nomic",
			ParameterSize:     "137M",
			QuantizationLevel: "Q4_0",
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockOllamaService) Health(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
	if m.healthFn != nil {
		return m.healthFn(ctx, req)
	}
	return &service.OllamaHealthResponse{
		Available:   true,
		Version:     "0.1.29",
		BaseURL:     "http://localhost:11434",
		LastChecked: time.Now(),
		Timestamp:   time.Now(),
	}, nil
}

func (m *mockOllamaService) IsAvailable() bool {
	if m.isAvailableFn != nil {
		return m.isAvailableFn()
	}
	return true
}

func (m *mockOllamaService) DefaultModel() string {
	if m.defaultModelFn != nil {
		return m.defaultModelFn()
	}
	return "nomic-embed-text"
}

func (m *mockOllamaService) BaseURL() string {
	if m.baseURLFn != nil {
		return m.baseURLFn()
	}
	return "http://localhost:11434"
}

// errorOllamaService is a mock that returns errors for all operations.
type errorOllamaService struct {
	mockOllamaService
}

func newErrorOllamaService() *errorOllamaService {
	return &errorOllamaService{
		mockOllamaService: mockOllamaService{
			embedFn: func(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
				return nil, errors.New("embed error")
			},
			embedWithModelFn: func(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
				return nil, errors.New("embed with model error")
			},
			embedBatchFn: func(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
				return nil, errors.New("embed batch error")
			},
			listModelsFn: func(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
				return nil, errors.New("list models error")
			},
			getModelInfoFn: func(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
				return nil, errors.New("get model info error")
			},
			healthFn: func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
				return nil, errors.New("health error")
			},
		},
	}
}

// mockOllamaEnabledService wraps mockService to return a mock OllamaService.
type mockOllamaEnabledService struct {
	mockService
	ollamaSvc service.OllamaService
}

func (m *mockOllamaEnabledService) Ollama() service.OllamaService {
	return m.ollamaSvc
}

// =============================================================================
// Helper Functions
// =============================================================================

func createOllamaTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-ollama-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createOllamaHandler(ollamaSvc service.OllamaService) *Handler {
	svc := &mockOllamaEnabledService{
		ollamaSvc: ollamaSvc,
	}
	return NewHandler(svc, nil, nil, nil, nil, createOllamaTestLogger())
}

// =============================================================================
// handleOllamaEmbed Tests
// =============================================================================

func TestHandleOllamaEmbed_Success(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, err := EncodePayload(&service.OllamaEmbedRequest{
		Text: "Hello, world!",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOllamaEmbed,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var embedResp service.OllamaEmbedResponse
	if err := DecodePayload(resp.Payload, &embedResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(embedResp.Embedding) == 0 {
		t.Error("expected non-empty embedding")
	}
	if embedResp.Model == "" {
		t.Error("expected non-empty model name")
	}
}

func TestHandleOllamaEmbed_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createOllamaTestLogger())

	payload, _ := EncodePayload(&service.OllamaEmbedRequest{
		Text: "Hello, world!",
	})

	req := &Request{
		Opcode:  OpOllamaEmbed,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil Ollama service")
	}
}

func TestHandleOllamaEmbed_DecodeError(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	req := &Request{
		Opcode:  OpOllamaEmbed,
		Payload: []byte("invalid json payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOllamaEmbed_ServiceError(t *testing.T) {
	ollamaSvc := newErrorOllamaService()
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaEmbedRequest{
		Text: "Hello, world!",
	})

	req := &Request{
		Opcode:  OpOllamaEmbed,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOllamaEmbedWithModel Tests
// =============================================================================

func TestHandleOllamaEmbedWithModel_Success(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, err := EncodePayload(&service.OllamaEmbedWithModelRequest{
		Text:  "Hello, world!",
		Model: "nomic-embed-text",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOllamaEmbedWithModel,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var embedResp service.OllamaEmbedResponse
	if err := DecodePayload(resp.Payload, &embedResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(embedResp.Embedding) == 0 {
		t.Error("expected non-empty embedding")
	}
	if embedResp.Model != "nomic-embed-text" {
		t.Errorf("expected model 'nomic-embed-text', got '%s'", embedResp.Model)
	}
}

func TestHandleOllamaEmbedWithModel_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createOllamaTestLogger())

	payload, _ := EncodePayload(&service.OllamaEmbedWithModelRequest{
		Text:  "Hello, world!",
		Model: "nomic-embed-text",
	})

	req := &Request{
		Opcode:  OpOllamaEmbedWithModel,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil Ollama service")
	}
}

func TestHandleOllamaEmbedWithModel_DecodeError(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	req := &Request{
		Opcode:  OpOllamaEmbedWithModel,
		Payload: []byte("invalid msgpack data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOllamaEmbedWithModel_ServiceError(t *testing.T) {
	ollamaSvc := newErrorOllamaService()
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaEmbedWithModelRequest{
		Text:  "Hello, world!",
		Model: "nomic-embed-text",
	})

	req := &Request{
		Opcode:  OpOllamaEmbedWithModel,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOllamaEmbedBatch Tests
// =============================================================================

func TestHandleOllamaEmbedBatch_Success(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, err := EncodePayload(&service.OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World", "Testing"},
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOllamaEmbedBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var batchResp service.OllamaEmbedBatchResponse
	if err := DecodePayload(resp.Payload, &batchResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if batchResp.Count != 3 {
		t.Errorf("expected 3 embeddings, got %d", batchResp.Count)
	}
	if len(batchResp.Embeddings) != 3 {
		t.Errorf("expected 3 embeddings in slice, got %d", len(batchResp.Embeddings))
	}
}

func TestHandleOllamaEmbedBatch_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createOllamaTestLogger())

	payload, _ := EncodePayload(&service.OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World"},
	})

	req := &Request{
		Opcode:  OpOllamaEmbedBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil Ollama service")
	}
}

func TestHandleOllamaEmbedBatch_DecodeError(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	req := &Request{
		Opcode:  OpOllamaEmbedBatch,
		Payload: []byte("not valid msgpack"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOllamaEmbedBatch_ServiceError(t *testing.T) {
	ollamaSvc := newErrorOllamaService()
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World"},
	})

	req := &Request{
		Opcode:  OpOllamaEmbedBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOllamaListModels Tests
// =============================================================================

func TestHandleOllamaListModels_Success(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, err := EncodePayload(&service.OllamaListModelsRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOllamaListModels,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var listResp service.OllamaListModelsResponse
	if err := DecodePayload(resp.Payload, &listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if listResp.Count != 2 {
		t.Errorf("expected 2 models, got %d", listResp.Count)
	}
	if len(listResp.Models) != 2 {
		t.Errorf("expected 2 models in slice, got %d", len(listResp.Models))
	}
}

func TestHandleOllamaListModels_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createOllamaTestLogger())

	payload, _ := EncodePayload(&service.OllamaListModelsRequest{})

	req := &Request{
		Opcode:  OpOllamaListModels,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil Ollama service")
	}
}

func TestHandleOllamaListModels_DecodeError(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	req := &Request{
		Opcode:  OpOllamaListModels,
		Payload: []byte("garbage data here"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOllamaListModels_ServiceError(t *testing.T) {
	ollamaSvc := newErrorOllamaService()
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaListModelsRequest{})

	req := &Request{
		Opcode:  OpOllamaListModels,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOllamaGetModelInfo Tests
// =============================================================================

func TestHandleOllamaGetModelInfo_Success(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, err := EncodePayload(&service.OllamaGetModelInfoRequest{
		Model: "nomic-embed-text",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOllamaGetModelInfo,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var infoResp service.OllamaGetModelInfoResponse
	if err := DecodePayload(resp.Payload, &infoResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if infoResp.Model != "nomic-embed-text" {
		t.Errorf("expected model 'nomic-embed-text', got '%s'", infoResp.Model)
	}
	if infoResp.Details.Family == "" {
		t.Error("expected non-empty model family")
	}
}

func TestHandleOllamaGetModelInfo_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createOllamaTestLogger())

	payload, _ := EncodePayload(&service.OllamaGetModelInfoRequest{
		Model: "nomic-embed-text",
	})

	req := &Request{
		Opcode:  OpOllamaGetModelInfo,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil Ollama service")
	}
}

func TestHandleOllamaGetModelInfo_DecodeError(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	req := &Request{
		Opcode:  OpOllamaGetModelInfo,
		Payload: []byte("bad payload data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOllamaGetModelInfo_ServiceError(t *testing.T) {
	ollamaSvc := newErrorOllamaService()
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaGetModelInfoRequest{
		Model: "nomic-embed-text",
	})

	req := &Request{
		Opcode:  OpOllamaGetModelInfo,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleOllamaHealth Tests
// =============================================================================

func TestHandleOllamaHealth_Success(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, err := EncodePayload(&service.OllamaHealthRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOllamaHealth,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var healthResp service.OllamaHealthResponse
	if err := DecodePayload(resp.Payload, &healthResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !healthResp.Available {
		t.Error("expected Available to be true")
	}
	if healthResp.Version == "" {
		t.Error("expected non-empty version")
	}
	if healthResp.BaseURL == "" {
		t.Error("expected non-empty base URL")
	}
}

func TestHandleOllamaHealth_NilService(t *testing.T) {
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createOllamaTestLogger())

	payload, _ := EncodePayload(&service.OllamaHealthRequest{})

	req := &Request{
		Opcode:  OpOllamaHealth,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil Ollama service")
	}
}

func TestHandleOllamaHealth_DecodeError(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	req := &Request{
		Opcode:  OpOllamaHealth,
		Payload: []byte("invalid health payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleOllamaHealth_ServiceError(t *testing.T) {
	ollamaSvc := newErrorOllamaService()
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaHealthRequest{})

	req := &Request{
		Opcode:  OpOllamaHealth,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Table-Driven Tests for All Handlers
// =============================================================================

func TestOllamaHandlers_AllOperations_Success(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"OllamaEmbed", OpOllamaEmbed, &service.OllamaEmbedRequest{Text: "test"}},
		{"OllamaEmbedWithModel", OpOllamaEmbedWithModel, &service.OllamaEmbedWithModelRequest{Text: "test", Model: "nomic-embed-text"}},
		{"OllamaEmbedBatch", OpOllamaEmbedBatch, &service.OllamaEmbedBatchRequest{Texts: []string{"a", "b"}}},
		{"OllamaListModels", OpOllamaListModels, &service.OllamaListModelsRequest{}},
		{"OllamaGetModelInfo", OpOllamaGetModelInfo, &service.OllamaGetModelInfoRequest{Model: "nomic-embed-text"}},
		{"OllamaHealth", OpOllamaHealth, &service.OllamaHealthRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name+"_Success", func(t *testing.T) {
			payload, err := EncodePayload(tt.payload)
			if err != nil {
				t.Fatalf("failed to encode payload: %v", err)
			}

			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status != StatusOK {
				t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
			}
		})
	}
}

func TestOllamaHandlers_AllOperations_NilService(t *testing.T) {
	// Use base mockService which returns nil for Ollama()
	handler := NewHandler(&mockService{}, nil, nil, nil, nil, createOllamaTestLogger())

	tests := []struct {
		name   string
		opcode byte
	}{
		{"OllamaEmbed_NilService", OpOllamaEmbed},
		{"OllamaEmbedWithModel_NilService", OpOllamaEmbedWithModel},
		{"OllamaEmbedBatch_NilService", OpOllamaEmbedBatch},
		{"OllamaListModels_NilService", OpOllamaListModels},
		{"OllamaGetModelInfo_NilService", OpOllamaGetModelInfo},
		{"OllamaHealth_NilService", OpOllamaHealth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := EncodePayload(map[string]interface{}{"test": "data"})
			req := &Request{Opcode: tt.opcode, Payload: payload}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected error status when Ollama service is nil")
			}
		})
	}
}

func TestOllamaHandlers_AllOperations_DecodeError(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	tests := []struct {
		name   string
		opcode byte
	}{
		{"OllamaEmbed_DecodeError", OpOllamaEmbed},
		{"OllamaEmbedWithModel_DecodeError", OpOllamaEmbedWithModel},
		{"OllamaEmbedBatch_DecodeError", OpOllamaEmbedBatch},
		{"OllamaListModels_DecodeError", OpOllamaListModels},
		{"OllamaGetModelInfo_DecodeError", OpOllamaGetModelInfo},
		{"OllamaHealth_DecodeError", OpOllamaHealth},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &Request{
				Opcode:  tt.opcode,
				Payload: []byte("invalid msgpack data"),
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected error status for invalid payload")
			}
		})
	}
}

func TestOllamaHandlers_AllOperations_ServiceError(t *testing.T) {
	ollamaSvc := newErrorOllamaService()
	handler := createOllamaHandler(ollamaSvc)

	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"OllamaEmbed_ServiceError", OpOllamaEmbed, &service.OllamaEmbedRequest{Text: "test"}},
		{"OllamaEmbedWithModel_ServiceError", OpOllamaEmbedWithModel, &service.OllamaEmbedWithModelRequest{Text: "test", Model: "m"}},
		{"OllamaEmbedBatch_ServiceError", OpOllamaEmbedBatch, &service.OllamaEmbedBatchRequest{Texts: []string{"a"}}},
		{"OllamaListModels_ServiceError", OpOllamaListModels, &service.OllamaListModelsRequest{}},
		{"OllamaGetModelInfo_ServiceError", OpOllamaGetModelInfo, &service.OllamaGetModelInfoRequest{Model: "m"}},
		{"OllamaHealth_ServiceError", OpOllamaHealth, &service.OllamaHealthRequest{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, _ := EncodePayload(tt.payload)
			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected error status from service error")
			}
		})
	}
}

// =============================================================================
// Edge Case Tests
// =============================================================================

func TestHandleOllamaEmbed_EmptyText(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaEmbedRequest{
		Text: "",
	})

	req := &Request{
		Opcode:  OpOllamaEmbed,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	// Empty text is valid for the mock, service may return error in production
	if resp.Status != StatusOK {
		t.Logf("empty text handled as expected with status %d", resp.Status)
	}
}

func TestHandleOllamaEmbedBatch_EmptyTexts(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaEmbedBatchRequest{
		Texts: []string{},
	})

	req := &Request{
		Opcode:  OpOllamaEmbedBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Logf("empty texts list handled with status %d", resp.Status)
	}
}

func TestHandleOllamaEmbedBatch_WithModel(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	handler := createOllamaHandler(ollamaSvc)

	payload, err := EncodePayload(&service.OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World"},
		Model: "custom-model",
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpOllamaEmbedBatch,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}
}

func TestHandleOllamaHealth_NotAvailable(t *testing.T) {
	ollamaSvc := &mockOllamaService{
		healthFn: func(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
			return &service.OllamaHealthResponse{
				Available:   false,
				Version:     "",
				BaseURL:     "http://localhost:11434",
				Error:       "connection refused",
				LastChecked: time.Now(),
				Timestamp:   time.Now(),
			}, nil
		},
	}
	handler := createOllamaHandler(ollamaSvc)

	payload, _ := EncodePayload(&service.OllamaHealthRequest{})

	req := &Request{
		Opcode:  OpOllamaHealth,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK even for unavailable status, got %d", resp.Status)
	}

	var healthResp service.OllamaHealthResponse
	if err := DecodePayload(resp.Payload, &healthResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if healthResp.Available {
		t.Error("expected Available to be false")
	}
	if healthResp.Error == "" {
		t.Error("expected non-empty error message")
	}
}

// =============================================================================
// Mock Service Method Tests
// =============================================================================

func TestMockOllamaService_DefaultBehavior(t *testing.T) {
	svc := &mockOllamaService{}

	if !svc.IsAvailable() {
		t.Error("expected IsAvailable to return true by default")
	}

	if svc.DefaultModel() != "nomic-embed-text" {
		t.Errorf("expected default model 'nomic-embed-text', got '%s'", svc.DefaultModel())
	}

	if svc.BaseURL() != "http://localhost:11434" {
		t.Errorf("expected base URL 'http://localhost:11434', got '%s'", svc.BaseURL())
	}
}

func TestMockOllamaEnabledService_ReturnsOllamaService(t *testing.T) {
	ollamaSvc := &mockOllamaService{}
	svc := &mockOllamaEnabledService{
		ollamaSvc: ollamaSvc,
	}

	if svc.Ollama() == nil {
		t.Error("expected non-nil Ollama service")
	}

	if svc.Ollama() != ollamaSvc {
		t.Error("expected Ollama() to return the configured mock service")
	}
}
