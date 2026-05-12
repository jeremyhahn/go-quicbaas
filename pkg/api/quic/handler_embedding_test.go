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
// Mock EmbeddingService Implementation
// =============================================================================

// mockEmbeddingService provides a configurable mock implementation of EmbeddingService.
type mockEmbeddingService struct {
	generateEmbeddingFn      func(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error)
	batchGenerateEmbeddingFn func(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error)
	listProvidersFn          func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error)
	getProviderInfoFn        func(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error)
	setDefaultProviderFn     func(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error)
	getCacheStatsFn          func(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error)
	clearCacheFn             func(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error)
}

func (m *mockEmbeddingService) GenerateEmbedding(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
	if m.generateEmbeddingFn != nil {
		return m.generateEmbeddingFn(ctx, req)
	}
	return &service.GenerateEmbeddingResponse{
		Embedding: []float32{0.1, 0.2, 0.3},
		Metadata: &service.EmbeddingMetadata{
			Model:      "test-model",
			Provider:   service.EmbeddingProviderOllama,
			Dimensions: 3,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockEmbeddingService) BatchGenerateEmbeddings(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
	if m.batchGenerateEmbeddingFn != nil {
		return m.batchGenerateEmbeddingFn(ctx, req)
	}
	results := make([]*service.BatchEmbeddingResult, len(req.Items))
	for i, item := range req.Items {
		results[i] = &service.BatchEmbeddingResult{
			ID:        item.ID,
			Embedding: []float32{0.1, 0.2, 0.3},
			Metadata: &service.EmbeddingMetadata{
				Model:      "test-model",
				Provider:   service.EmbeddingProviderOllama,
				Dimensions: 3,
			},
		}
	}
	return &service.BatchGenerateEmbeddingsResponse{
		Results:        results,
		SuccessCount:   len(req.Items),
		ErrorCount:     0,
		ProcessingTime: time.Millisecond * 100,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockEmbeddingService) ListProviders(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
	if m.listProvidersFn != nil {
		return m.listProvidersFn(ctx, req)
	}
	return &service.ListProvidersResponse{
		Providers: []*service.ProviderInfo{
			{
				Name:       "ollama",
				Type:       service.EmbeddingProviderOllama,
				Model:      "nomic-embed-text",
				Dimensions: 768,
				Status:     service.EmbeddingStatusAvailable,
			},
		},
		DefaultProvider: "ollama",
		Timestamp:       time.Now(),
	}, nil
}

func (m *mockEmbeddingService) GetProviderInfo(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
	if m.getProviderInfoFn != nil {
		return m.getProviderInfoFn(ctx, req)
	}
	return &service.GetProviderInfoResponse{
		Info: &service.ProviderInfo{
			Name:       string(req.Provider),
			Type:       req.Provider,
			Model:      "nomic-embed-text",
			Dimensions: 768,
			Status:     service.EmbeddingStatusAvailable,
		},
		Timestamp: time.Now(),
	}, nil
}

func (m *mockEmbeddingService) SetDefaultProvider(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
	if m.setDefaultProviderFn != nil {
		return m.setDefaultProviderFn(ctx, req)
	}
	return &service.SetDefaultProviderResponse{
		Provider:         req.Provider,
		PreviousProvider: service.EmbeddingProviderOllama,
		Timestamp:        time.Now(),
	}, nil
}

func (m *mockEmbeddingService) GetCacheStats(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
	if m.getCacheStatsFn != nil {
		return m.getCacheStatsFn(ctx, req)
	}
	return &service.GetEmbeddingCacheStatsResponse{
		Enabled:   true,
		Size:      100,
		Capacity:  1000,
		Hits:      50,
		Misses:    20,
		HitRate:   0.714,
		Evictions: 5,
		Timestamp: time.Now(),
	}, nil
}

func (m *mockEmbeddingService) ClearCache(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
	if m.clearCacheFn != nil {
		return m.clearCacheFn(ctx, req)
	}
	return &service.ClearEmbeddingCacheResponse{
		EntriesCleared: 100,
		Provider:       req.Provider,
		Timestamp:      time.Now(),
	}, nil
}

func (m *mockEmbeddingService) SetProviderStatus(_ context.Context, req *service.SetProviderStatusRequest) (*service.SetProviderStatusResponse, error) {
	return &service.SetProviderStatusResponse{Name: req.Name, Enabled: req.Enabled, Timestamp: time.Now()}, nil
}

// =============================================================================
// Mock Service Wrapper for Embedding Tests
// =============================================================================

// mockEmbeddingEnabledService extends mockService to return a configurable EmbeddingService.
type mockEmbeddingEnabledService struct {
	mockService
	embeddingSvc service.EmbeddingService
}

func (m *mockEmbeddingEnabledService) Embedding() service.EmbeddingService {
	return m.embeddingSvc
}

// errorEmbeddingService always returns errors for all operations.
type errorEmbeddingService struct{}

func (e *errorEmbeddingService) GenerateEmbedding(ctx context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
	return nil, errors.New("generate embedding error")
}

func (e *errorEmbeddingService) BatchGenerateEmbeddings(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
	return nil, errors.New("batch generate embeddings error")
}

func (e *errorEmbeddingService) ListProviders(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
	return nil, errors.New("list providers error")
}

func (e *errorEmbeddingService) GetProviderInfo(ctx context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
	return nil, errors.New("get provider info error")
}

func (e *errorEmbeddingService) SetDefaultProvider(ctx context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
	return nil, errors.New("set default provider error")
}

func (e *errorEmbeddingService) GetCacheStats(ctx context.Context, req *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
	return nil, errors.New("get cache stats error")
}

func (e *errorEmbeddingService) ClearCache(ctx context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
	return nil, errors.New("clear cache error")
}

func (e *errorEmbeddingService) SetProviderStatus(_ context.Context, _ *service.SetProviderStatusRequest) (*service.SetProviderStatusResponse, error) {
	return nil, errors.New("set provider status error")
}

// =============================================================================
// Helper Functions
// =============================================================================

func createEmbeddingTestLogger() *logger.Logger {
	cfg := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "quic-embedding-test",
	}
	log, err := logger.New(cfg)
	if err != nil {
		panic(err)
	}
	return log
}

func createEmbeddingTestHandler(embeddingSvc service.EmbeddingService) *Handler {
	svc := &mockEmbeddingEnabledService{
		embeddingSvc: embeddingSvc,
	}
	return NewHandler(svc, nil, nil, nil, nil, createEmbeddingTestLogger())
}

// =============================================================================
// handleEmbeddingGenerate Tests
// =============================================================================

func TestHandleEmbeddingGenerate_Success(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, err := EncodePayload(&service.GenerateEmbeddingRequest{
		Text:     "Hello, world!",
		Provider: service.EmbeddingProviderOllama,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpEmbeddingGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var genResp service.GenerateEmbeddingResponse
	if err := DecodePayload(resp.Payload, &genResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(genResp.Embedding) == 0 {
		t.Error("expected non-empty embedding")
	}
}

func TestHandleEmbeddingGenerate_NilService(t *testing.T) {
	handler := createEmbeddingTestHandler(nil)

	payload, _ := EncodePayload(&service.GenerateEmbeddingRequest{
		Text: "Hello, world!",
	})

	req := &Request{
		Opcode:  OpEmbeddingGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil embedding service")
	}
}

func TestHandleEmbeddingGenerate_DecodeError(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	req := &Request{
		Opcode:  OpEmbeddingGenerate,
		Payload: []byte("invalid json payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleEmbeddingGenerate_ServiceError(t *testing.T) {
	embeddingSvc := &errorEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.GenerateEmbeddingRequest{
		Text: "Hello, world!",
	})

	req := &Request{
		Opcode:  OpEmbeddingGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleEmbeddingBatchGenerate Tests
// =============================================================================

func TestHandleEmbeddingBatchGenerate_Success(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, err := EncodePayload(&service.BatchGenerateEmbeddingsRequest{
		Items: []*service.BatchEmbeddingItem{
			{ID: "1", Text: "First text"},
			{ID: "2", Text: "Second text"},
			{ID: "3", Text: "Third text"},
		},
		Provider: service.EmbeddingProviderOllama,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpEmbeddingBatchGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var batchResp service.BatchGenerateEmbeddingsResponse
	if err := DecodePayload(resp.Payload, &batchResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(batchResp.Results) != 3 {
		t.Errorf("expected 3 results, got %d", len(batchResp.Results))
	}
}

func TestHandleEmbeddingBatchGenerate_NilService(t *testing.T) {
	handler := createEmbeddingTestHandler(nil)

	payload, _ := EncodePayload(&service.BatchGenerateEmbeddingsRequest{
		Items: []*service.BatchEmbeddingItem{
			{ID: "1", Text: "Test"},
		},
	})

	req := &Request{
		Opcode:  OpEmbeddingBatchGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil embedding service")
	}
}

func TestHandleEmbeddingBatchGenerate_DecodeError(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	req := &Request{
		Opcode:  OpEmbeddingBatchGenerate,
		Payload: []byte("not valid json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleEmbeddingBatchGenerate_ServiceError(t *testing.T) {
	embeddingSvc := &errorEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.BatchGenerateEmbeddingsRequest{
		Items: []*service.BatchEmbeddingItem{
			{ID: "1", Text: "Test"},
		},
	})

	req := &Request{
		Opcode:  OpEmbeddingBatchGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleEmbeddingListProviders Tests
// =============================================================================

func TestHandleEmbeddingListProviders_Success(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, err := EncodePayload(&service.ListProvidersRequest{
		IncludeUnavailable: true,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpEmbeddingListProviders,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var listResp service.ListProvidersResponse
	if err := DecodePayload(resp.Payload, &listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(listResp.Providers) == 0 {
		t.Error("expected at least one provider")
	}
}

func TestHandleEmbeddingListProviders_NilService(t *testing.T) {
	handler := createEmbeddingTestHandler(nil)

	payload, _ := EncodePayload(&service.ListProvidersRequest{})

	req := &Request{
		Opcode:  OpEmbeddingListProviders,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil embedding service")
	}
}

func TestHandleEmbeddingListProviders_DecodeError(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	req := &Request{
		Opcode:  OpEmbeddingListProviders,
		Payload: []byte("corrupted data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleEmbeddingListProviders_ServiceError(t *testing.T) {
	embeddingSvc := &errorEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.ListProvidersRequest{})

	req := &Request{
		Opcode:  OpEmbeddingListProviders,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleEmbeddingGetProvider Tests
// =============================================================================

func TestHandleEmbeddingGetProvider_Success(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, err := EncodePayload(&service.GetProviderInfoRequest{
		Provider: service.EmbeddingProviderOllama,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpEmbeddingGetProvider,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var infoResp service.GetProviderInfoResponse
	if err := DecodePayload(resp.Payload, &infoResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if infoResp.Info == nil {
		t.Error("expected non-nil provider info")
	}
}

func TestHandleEmbeddingGetProvider_NilService(t *testing.T) {
	handler := createEmbeddingTestHandler(nil)

	payload, _ := EncodePayload(&service.GetProviderInfoRequest{
		Provider: service.EmbeddingProviderOllama,
	})

	req := &Request{
		Opcode:  OpEmbeddingGetProvider,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil embedding service")
	}
}

func TestHandleEmbeddingGetProvider_DecodeError(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	req := &Request{
		Opcode:  OpEmbeddingGetProvider,
		Payload: []byte("garbage payload"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleEmbeddingGetProvider_ServiceError(t *testing.T) {
	embeddingSvc := &errorEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.GetProviderInfoRequest{
		Provider: service.EmbeddingProviderOllama,
	})

	req := &Request{
		Opcode:  OpEmbeddingGetProvider,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleEmbeddingSetDefault Tests
// =============================================================================

func TestHandleEmbeddingSetDefault_Success(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, err := EncodePayload(&service.SetDefaultProviderRequest{
		Provider: service.EmbeddingProviderOpenAI,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpEmbeddingSetDefault,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var setResp service.SetDefaultProviderResponse
	if err := DecodePayload(resp.Payload, &setResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if setResp.Provider != service.EmbeddingProviderOpenAI {
		t.Errorf("expected provider %s, got %s", service.EmbeddingProviderOpenAI, setResp.Provider)
	}
}

func TestHandleEmbeddingSetDefault_NilService(t *testing.T) {
	handler := createEmbeddingTestHandler(nil)

	payload, _ := EncodePayload(&service.SetDefaultProviderRequest{
		Provider: service.EmbeddingProviderOpenAI,
	})

	req := &Request{
		Opcode:  OpEmbeddingSetDefault,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil embedding service")
	}
}

func TestHandleEmbeddingSetDefault_DecodeError(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	req := &Request{
		Opcode:  OpEmbeddingSetDefault,
		Payload: []byte("malformed json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleEmbeddingSetDefault_ServiceError(t *testing.T) {
	embeddingSvc := &errorEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.SetDefaultProviderRequest{
		Provider: service.EmbeddingProviderOpenAI,
	})

	req := &Request{
		Opcode:  OpEmbeddingSetDefault,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleEmbeddingGetCacheStats Tests
// =============================================================================

func TestHandleEmbeddingGetCacheStats_Success(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, err := EncodePayload(&service.GetEmbeddingCacheStatsRequest{})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpEmbeddingGetCacheStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var statsResp service.GetEmbeddingCacheStatsResponse
	if err := DecodePayload(resp.Payload, &statsResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !statsResp.Enabled {
		t.Error("expected cache to be enabled")
	}
	if statsResp.Size == 0 {
		t.Error("expected non-zero cache size")
	}
}

func TestHandleEmbeddingGetCacheStats_NilService(t *testing.T) {
	handler := createEmbeddingTestHandler(nil)

	payload, _ := EncodePayload(&service.GetEmbeddingCacheStatsRequest{})

	req := &Request{
		Opcode:  OpEmbeddingGetCacheStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil embedding service")
	}
}

func TestHandleEmbeddingGetCacheStats_DecodeError(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	req := &Request{
		Opcode:  OpEmbeddingGetCacheStats,
		Payload: []byte("not json"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleEmbeddingGetCacheStats_ServiceError(t *testing.T) {
	embeddingSvc := &errorEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.GetEmbeddingCacheStatsRequest{})

	req := &Request{
		Opcode:  OpEmbeddingGetCacheStats,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// handleEmbeddingClearCache Tests
// =============================================================================

func TestHandleEmbeddingClearCache_Success(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, err := EncodePayload(&service.ClearEmbeddingCacheRequest{
		All: true,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpEmbeddingClearCache,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}

	var clearResp service.ClearEmbeddingCacheResponse
	if err := DecodePayload(resp.Payload, &clearResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if clearResp.EntriesCleared == 0 {
		t.Error("expected non-zero entries cleared")
	}
}

func TestHandleEmbeddingClearCache_NilService(t *testing.T) {
	handler := createEmbeddingTestHandler(nil)

	payload, _ := EncodePayload(&service.ClearEmbeddingCacheRequest{
		All: true,
	})

	req := &Request{
		Opcode:  OpEmbeddingClearCache,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with nil embedding service")
	}
}

func TestHandleEmbeddingClearCache_DecodeError(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	req := &Request{
		Opcode:  OpEmbeddingClearCache,
		Payload: []byte("invalid data"),
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error with invalid payload")
	}
}

func TestHandleEmbeddingClearCache_ServiceError(t *testing.T) {
	embeddingSvc := &errorEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.ClearEmbeddingCacheRequest{
		All: true,
	})

	req := &Request{
		Opcode:  OpEmbeddingClearCache,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status == StatusOK {
		t.Error("expected error from service")
	}
}

// =============================================================================
// Additional Edge Case Tests
// =============================================================================

func TestHandleEmbeddingClearCache_ProviderSpecific(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, err := EncodePayload(&service.ClearEmbeddingCacheRequest{
		Provider: service.EmbeddingProviderOllama,
		All:      false,
	})
	if err != nil {
		t.Fatalf("failed to encode payload: %v", err)
	}

	req := &Request{
		Opcode:  OpEmbeddingClearCache,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleEmbeddingBatchGenerate_EmptyBatch(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{
		batchGenerateEmbeddingFn: func(ctx context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
			return &service.BatchGenerateEmbeddingsResponse{
				Results:      []*service.BatchEmbeddingResult{},
				SuccessCount: 0,
				ErrorCount:   0,
				Timestamp:    time.Now(),
			}, nil
		},
	}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.BatchGenerateEmbeddingsRequest{
		Items: []*service.BatchEmbeddingItem{},
	})

	req := &Request{
		Opcode:  OpEmbeddingBatchGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK for empty batch, got %d", resp.Status)
	}
}

func TestHandleEmbeddingGenerate_WithMetadata(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.GenerateEmbeddingRequest{
		Text:      "Test text with metadata",
		Provider:  service.EmbeddingProviderOllama,
		InputType: service.EmbeddingInputTypeDocument,
		Metadata: map[string]string{
			"source":   "test",
			"language": "en",
		},
		Source:   "unit_test",
		Language: "en",
	})

	req := &Request{
		Opcode:  OpEmbeddingGenerate,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d: %s", resp.Status, string(resp.Payload))
	}
}

func TestHandleEmbeddingListProviders_IncludeUnavailable(t *testing.T) {
	embeddingSvc := &mockEmbeddingService{
		listProvidersFn: func(ctx context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
			providers := []*service.ProviderInfo{
				{Name: "ollama", Status: service.EmbeddingStatusAvailable},
			}
			if req.IncludeUnavailable {
				providers = append(providers, &service.ProviderInfo{
					Name:   "openai",
					Status: service.EmbeddingStatusUnavailable,
				})
			}
			return &service.ListProvidersResponse{
				Providers:       providers,
				DefaultProvider: "ollama",
				Timestamp:       time.Now(),
			}, nil
		},
	}
	handler := createEmbeddingTestHandler(embeddingSvc)

	payload, _ := EncodePayload(&service.ListProvidersRequest{
		IncludeUnavailable: true,
	})

	req := &Request{
		Opcode:  OpEmbeddingListProviders,
		Payload: payload,
	}

	resp := handler.HandleRequest(context.Background(), req)
	if resp.Status != StatusOK {
		t.Errorf("expected StatusOK, got %d", resp.Status)
	}

	var listResp service.ListProvidersResponse
	if err := DecodePayload(resp.Payload, &listResp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(listResp.Providers) != 2 {
		t.Errorf("expected 2 providers when including unavailable, got %d", len(listResp.Providers))
	}
}

// =============================================================================
// Table-Driven Tests for All Embedding Operations
// =============================================================================

func TestEmbeddingHandlers_AllOperations_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{
			name:   "EmbeddingGenerate",
			opcode: OpEmbeddingGenerate,
			payload: &service.GenerateEmbeddingRequest{
				Text:     "test text",
				Provider: service.EmbeddingProviderOllama,
			},
		},
		{
			name:   "EmbeddingBatchGenerate",
			opcode: OpEmbeddingBatchGenerate,
			payload: &service.BatchGenerateEmbeddingsRequest{
				Items: []*service.BatchEmbeddingItem{
					{ID: "1", Text: "test"},
				},
			},
		},
		{
			name:    "EmbeddingListProviders",
			opcode:  OpEmbeddingListProviders,
			payload: &service.ListProvidersRequest{},
		},
		{
			name:   "EmbeddingGetProvider",
			opcode: OpEmbeddingGetProvider,
			payload: &service.GetProviderInfoRequest{
				Provider: service.EmbeddingProviderOllama,
			},
		},
		{
			name:   "EmbeddingSetDefault",
			opcode: OpEmbeddingSetDefault,
			payload: &service.SetDefaultProviderRequest{
				Provider: service.EmbeddingProviderOllama,
			},
		},
		{
			name:    "EmbeddingGetCacheStats",
			opcode:  OpEmbeddingGetCacheStats,
			payload: &service.GetEmbeddingCacheStatsRequest{},
		},
		{
			name:   "EmbeddingClearCache",
			opcode: OpEmbeddingClearCache,
			payload: &service.ClearEmbeddingCacheRequest{
				All: true,
			},
		},
	}

	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
				t.Errorf("expected StatusOK for %s, got %d: %s", tt.name, resp.Status, string(resp.Payload))
			}
		})
	}
}

func TestEmbeddingHandlers_NilService_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		opcode  byte
		payload interface{}
	}{
		{"EmbeddingGenerate", OpEmbeddingGenerate, &service.GenerateEmbeddingRequest{Text: "test"}},
		{"EmbeddingBatchGenerate", OpEmbeddingBatchGenerate, &service.BatchGenerateEmbeddingsRequest{Items: []*service.BatchEmbeddingItem{{ID: "1", Text: "test"}}}},
		{"EmbeddingListProviders", OpEmbeddingListProviders, &service.ListProvidersRequest{}},
		{"EmbeddingGetProvider", OpEmbeddingGetProvider, &service.GetProviderInfoRequest{Provider: "ollama"}},
		{"EmbeddingSetDefault", OpEmbeddingSetDefault, &service.SetDefaultProviderRequest{Provider: "ollama"}},
		{"EmbeddingGetCacheStats", OpEmbeddingGetCacheStats, &service.GetEmbeddingCacheStatsRequest{}},
		{"EmbeddingClearCache", OpEmbeddingClearCache, &service.ClearEmbeddingCacheRequest{All: true}},
	}

	handler := createEmbeddingTestHandler(nil)

	for _, tt := range tests {
		t.Run(tt.name+"_NilService", func(t *testing.T) {
			payload, _ := EncodePayload(tt.payload)
			req := &Request{
				Opcode:  tt.opcode,
				Payload: payload,
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected error for %s with nil service", tt.name)
			}
		})
	}
}

func TestEmbeddingHandlers_DecodeError_TableDriven(t *testing.T) {
	tests := []struct {
		name   string
		opcode byte
	}{
		{"EmbeddingGenerate", OpEmbeddingGenerate},
		{"EmbeddingBatchGenerate", OpEmbeddingBatchGenerate},
		{"EmbeddingListProviders", OpEmbeddingListProviders},
		{"EmbeddingGetProvider", OpEmbeddingGetProvider},
		{"EmbeddingSetDefault", OpEmbeddingSetDefault},
		{"EmbeddingGetCacheStats", OpEmbeddingGetCacheStats},
		{"EmbeddingClearCache", OpEmbeddingClearCache},
	}

	embeddingSvc := &mockEmbeddingService{}
	handler := createEmbeddingTestHandler(embeddingSvc)

	for _, tt := range tests {
		t.Run(tt.name+"_DecodeError", func(t *testing.T) {
			req := &Request{
				Opcode:  tt.opcode,
				Payload: []byte("this is not valid json"),
			}

			resp := handler.HandleRequest(context.Background(), req)
			if resp.Status == StatusOK {
				t.Errorf("expected decode error for %s", tt.name)
			}
		})
	}
}
