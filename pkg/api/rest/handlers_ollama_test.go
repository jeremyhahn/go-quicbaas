//go:build rest

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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/mock"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// mockOllamaServiceForOllama implements service.OllamaService for testing.
type mockOllamaServiceForOllama struct {
	mock.Mock
	baseURL      string
	defaultModel string
	isAvailable  bool
}

func (m *mockOllamaServiceForOllama) Embed(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaEmbedResponse), args.Error(1)
}

func (m *mockOllamaServiceForOllama) EmbedWithModel(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaEmbedResponse), args.Error(1)
}

func (m *mockOllamaServiceForOllama) EmbedBatch(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaEmbedBatchResponse), args.Error(1)
}

func (m *mockOllamaServiceForOllama) ListModels(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaListModelsResponse), args.Error(1)
}

func (m *mockOllamaServiceForOllama) GetModelInfo(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaGetModelInfoResponse), args.Error(1)
}

func (m *mockOllamaServiceForOllama) Health(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaHealthResponse), args.Error(1)
}

func (m *mockOllamaServiceForOllama) IsAvailable() bool {
	return m.isAvailable
}

func (m *mockOllamaServiceForOllama) DefaultModel() string {
	if m.defaultModel != "" {
		return m.defaultModel
	}
	return "nomic-embed-text"
}

func (m *mockOllamaServiceForOllama) BaseURL() string {
	if m.baseURL != "" {
		return m.baseURL
	}
	return "http://localhost:11434"
}

func TestHandleOllamaHealth_Success(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("Health", mock.Anything, mock.Anything).Return(&service.OllamaHealthResponse{
		Available:   true,
		Version:     "0.1.0",
		BaseURL:     "http://localhost:11434",
		LastChecked: time.Now(),
	}, nil)

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/health", nil)
	w := httptest.NewRecorder()

	s.HandleOllamaHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp OllamaHealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if !resp.Available {
		t.Error("expected available to be true")
	}
}

func TestHandleOllamaHealth_NotConfigured(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()
	// OllamaService is nil by default

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/health", nil)
	w := httptest.NewRecorder()

	s.HandleOllamaHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp OllamaHealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Available {
		t.Error("expected available to be false")
	}

	if resp.Error != "ollama service not configured" {
		t.Errorf("expected error 'ollama service not configured', got '%s'", resp.Error)
	}
}

func TestHandleOllamaHealth_Error(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: false}
	ollamaMock.On("Health", mock.Anything, mock.Anything).Return(nil, errors.New("connection refused"))

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/health", nil)
	w := httptest.NewRecorder()

	s.HandleOllamaHealth(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp OllamaHealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Available {
		t.Error("expected available to be false")
	}

	if resp.Error == "" {
		t.Error("expected error message to be present")
	}
}

func TestHandleOllamaModelList_Success(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("ListModels", mock.Anything, mock.Anything).Return(&service.OllamaListModelsResponse{
		Models: []service.OllamaModelInfo{
			{
				Name:       "nomic-embed-text",
				Size:       274000000,
				Digest:     "sha256:abc123",
				ModifiedAt: time.Now(),
			},
		},
		Count: 1,
	}, nil)

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/models", nil)
	w := httptest.NewRecorder()

	s.HandleOllamaModelList(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp OllamaModelListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Count != 1 {
		t.Errorf("expected count 1, got %d", resp.Count)
	}
}

func TestHandleOllamaModelList_NotConfigured(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()
	// OllamaService is nil by default

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/models", nil)
	w := httptest.NewRecorder()

	s.HandleOllamaModelList(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleOllamaModelList_Error(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("ListModels", mock.Anything, mock.Anything).Return(nil, errors.New("failed to list models"))

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/models", nil)
	w := httptest.NewRecorder()

	s.HandleOllamaModelList(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandleOllamaModelInfo_Success(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("GetModelInfo", mock.Anything, mock.Anything).Return(&service.OllamaGetModelInfoResponse{
		Model: "nomic-embed-text",
		Details: service.OllamaModelDetails{
			License:           "Apache-2.0",
			Format:            "gguf",
			Family:            "nomic",
			ParameterSize:     "137M",
			QuantizationLevel: "Q4_0",
		},
	}, nil)

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	r := chi.NewRouter()
	r.Get("/api/v1/ollama/models/{model}", s.HandleOllamaModelInfo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/models/nomic-embed-text", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp OllamaModelDetailsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Model != "nomic-embed-text" {
		t.Errorf("expected model 'nomic-embed-text', got '%s'", resp.Model)
	}
}

func TestHandleOllamaModelInfo_NotConfigured(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()
	// OllamaService is nil by default

	s := &Server{service: mockDB}

	r := chi.NewRouter()
	r.Get("/api/v1/ollama/models/{model}", s.HandleOllamaModelInfo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/models/nomic-embed-text", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleOllamaModelInfo_Error(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("GetModelInfo", mock.Anything, mock.Anything).Return(nil, errors.New("model not found"))

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	r := chi.NewRouter()
	r.Get("/api/v1/ollama/models/{model}", s.HandleOllamaModelInfo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/ollama/models/nonexistent", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandleOllamaEmbed_Success(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("Embed", mock.Anything, mock.Anything).Return(&service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3},
		Model:          "nomic-embed-text",
		Dimensions:     3,
		ProcessingTime: 100 * time.Millisecond,
	}, nil)

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	body := OllamaEmbedRequest{
		Text: "Hello world",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp OllamaEmbedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Embedding) != 3 {
		t.Errorf("expected 3 embedding dimensions, got %d", len(resp.Embedding))
	}
}

func TestHandleOllamaEmbed_WithModel(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("EmbedWithModel", mock.Anything, mock.MatchedBy(func(req *service.OllamaEmbedWithModelRequest) bool {
		return req.Model == "mxbai-embed-large"
	})).Return(&service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3, 0.4, 0.5},
		Model:          "mxbai-embed-large",
		Dimensions:     5,
		ProcessingTime: 150 * time.Millisecond,
	}, nil)

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	body := OllamaEmbedRequest{
		Text:  "Hello world",
		Model: "mxbai-embed-large",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbed(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp OllamaEmbedResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Model != "mxbai-embed-large" {
		t.Errorf("expected model 'mxbai-embed-large', got '%s'", resp.Model)
	}
}

func TestHandleOllamaEmbed_MissingText(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	body := OllamaEmbedRequest{
		Text: "",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbed(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleOllamaEmbed_NotConfigured(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()
	// OllamaService is nil by default

	s := &Server{service: mockDB}

	body := OllamaEmbedRequest{
		Text: "Hello world",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbed(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleOllamaEmbed_Error(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("Embed", mock.Anything, mock.Anything).Return(nil, errors.New("embedding failed"))

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	body := OllamaEmbedRequest{
		Text: "Hello world",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbed(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandleOllamaEmbed_InvalidJSON(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbed(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleOllamaEmbedBatch_Success(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("EmbedBatch", mock.Anything, mock.Anything).Return(&service.OllamaEmbedBatchResponse{
		Embeddings: [][]float32{
			{0.1, 0.2, 0.3},
			{0.4, 0.5, 0.6},
		},
		Model:          "nomic-embed-text",
		Dimensions:     3,
		Count:          2,
		ProcessingTime: 200 * time.Millisecond,
	}, nil)

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	body := OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed/batch", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbedBatch(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp OllamaEmbedBatchResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Count != 2 {
		t.Errorf("expected count 2, got %d", resp.Count)
	}
}

func TestHandleOllamaEmbedBatch_EmptyTexts(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	body := OllamaEmbedBatchRequest{
		Texts: []string{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed/batch", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbedBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleOllamaEmbedBatch_NotConfigured(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()
	// OllamaService is nil by default

	s := &Server{service: mockDB}

	body := OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed/batch", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbedBatch(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleOllamaEmbedBatch_Error(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	ollamaMock.On("EmbedBatch", mock.Anything, mock.Anything).Return(nil, errors.New("batch embedding failed"))

	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	body := OllamaEmbedBatchRequest{
		Texts: []string{"Hello", "World"},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed/batch", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbedBatch(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandleOllamaEmbedBatch_InvalidJSON(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	ollamaMock := &mockOllamaServiceForOllama{isAvailable: true}
	mockDB.OllamaService = ollamaMock

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/ollama/embed/batch", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleOllamaEmbedBatch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}
