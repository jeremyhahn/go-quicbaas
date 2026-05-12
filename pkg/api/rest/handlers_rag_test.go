//go:build rest && rag

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
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// mockVectorServiceForRAG implements service.VectorService for RAG testing.
type mockVectorServiceForRAG struct {
	mock.Mock
}

func (m *mockVectorServiceForRAG) CreateNamespace(ctx context.Context, req *service.CreateNamespaceRequest) (*service.CreateNamespaceResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CreateNamespaceResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) DeleteNamespace(ctx context.Context, req *service.DeleteNamespaceRequest) (*service.DeleteNamespaceResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteNamespaceResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) ListNamespaces(ctx context.Context, req *service.ListNamespacesRequest) (*service.ListNamespacesResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListNamespacesResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) GetNamespaceInfo(ctx context.Context, req *service.GetNamespaceInfoRequest) (*service.GetNamespaceInfoResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetNamespaceInfoResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) UpsertVector(ctx context.Context, req *service.UpsertVectorRequest) (*service.UpsertVectorResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.UpsertVectorResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) GetVector(ctx context.Context, req *service.GetVectorRequest) (*service.GetVectorResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetVectorResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) DeleteVector(ctx context.Context, req *service.DeleteVectorRequest) (*service.DeleteVectorResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.DeleteVectorResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) BatchUpsert(ctx context.Context, req *service.BatchUpsertRequest) (*service.BatchUpsertResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchUpsertResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) SearchSimilar(ctx context.Context, req *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SearchSimilarResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) SearchByMetadata(ctx context.Context, req *service.SearchByMetadataRequest) (*service.SearchByMetadataResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SearchByMetadataResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) HybridSearch(ctx context.Context, req *service.HybridSearchRequest) (*service.HybridSearchResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.HybridSearchResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) RebuildIndex(ctx context.Context, req *service.RebuildIndexRequest) (*service.RebuildIndexResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.RebuildIndexResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) GetRebuildProgress(ctx context.Context, req *service.GetRebuildProgressRequest) (*service.GetRebuildProgressResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetRebuildProgressResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) GetIndexStats(ctx context.Context, req *service.GetIndexStatsRequest) (*service.GetIndexStatsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetIndexStatsResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) CompactNamespace(ctx context.Context, req *service.CompactNamespaceRequest) (*service.CompactNamespaceResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.CompactNamespaceResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) GetCompactionProgress(ctx context.Context, req *service.GetCompactionProgressRequest) (*service.GetCompactionProgressResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.GetCompactionProgressResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) BatchDeleteVectors(ctx context.Context, req *service.BatchDeleteVectorsRequest) (*service.BatchDeleteVectorsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.BatchDeleteVectorsResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) ListVectors(ctx context.Context, req *service.ListVectorsRequest) (*service.ListVectorsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.ListVectorsResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) UpdateMetadata(ctx context.Context, req *service.UpdateVectorMetadataRequest) (*service.UpdateVectorMetadataResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.UpdateVectorMetadataResponse), args.Error(1)
}

func (m *mockVectorServiceForRAG) SearchByID(ctx context.Context, req *service.SearchByIDRequest) (*service.SearchByIDResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.SearchByIDResponse), args.Error(1)
}

// mockOllamaServiceForRAG implements service.OllamaService for RAG testing.
type mockOllamaServiceForRAG struct {
	mock.Mock
	baseURL      string
	defaultModel string
	isAvailable  bool
}

func (m *mockOllamaServiceForRAG) Embed(ctx context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaEmbedResponse), args.Error(1)
}

func (m *mockOllamaServiceForRAG) EmbedWithModel(ctx context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaEmbedResponse), args.Error(1)
}

func (m *mockOllamaServiceForRAG) EmbedBatch(ctx context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaEmbedBatchResponse), args.Error(1)
}

func (m *mockOllamaServiceForRAG) ListModels(ctx context.Context, req *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaListModelsResponse), args.Error(1)
}

func (m *mockOllamaServiceForRAG) GetModelInfo(ctx context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaGetModelInfoResponse), args.Error(1)
}

func (m *mockOllamaServiceForRAG) Health(ctx context.Context, req *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
	args := m.Called(ctx, req)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*service.OllamaHealthResponse), args.Error(1)
}

func (m *mockOllamaServiceForRAG) IsAvailable() bool {
	return m.isAvailable
}

func (m *mockOllamaServiceForRAG) DefaultModel() string {
	if m.defaultModel != "" {
		return m.defaultModel
	}
	return "nomic-embed-text"
}

func (m *mockOllamaServiceForRAG) BaseURL() string {
	if m.baseURL != "" {
		return m.baseURL
	}
	return "http://localhost:11434"
}

func TestHandleRAGQuery_Success(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	// Set up vector service mock
	vectorSvc := &mockVectorServiceForRAG{}
	vectorSvc.On("SearchSimilar", mock.Anything, mock.Anything).Return(&service.SearchSimilarResponse{
		Results: []*service.VectorSearchResult{
			{
				ID:    1,
				Score: 0.95,
				Metadata: map[string]interface{}{
					"content":    "func main() { fmt.Println(\"hello\") }",
					"file_path":  "main.go",
					"file_name":  "main.go",
					"language":   "go",
					"start_line": 1,
					"end_line":   3,
				},
			},
		},
		Timestamp:     time.Now().UnixNano(),
		QueryDuration: 50000,
	}, nil)

	// Set up ollama service mock
	ollamaSvc := &mockOllamaServiceForRAG{isAvailable: true}
	ollamaSvc.On("Embed", mock.Anything, mock.Anything).Return(&service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3},
		Model:          "nomic-embed-text",
		Dimensions:     3,
		ProcessingTime: 100 * time.Millisecond,
	}, nil)

	mockDB.VectorService = vectorSvc
	mockDB.OllamaService = ollamaSvc

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:     "How does error handling work?",
		Namespace: "codebase",
		TopK:      5,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp RAGQueryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ResultCount == 0 {
		t.Error("expected at least one result")
	}
}

func TestHandleRAGQuery_MissingQuery(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:     "",
		Namespace: "codebase",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleRAGQuery_MissingNamespace(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:     "test query",
		Namespace: "",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleRAGQuery_OllamaNotConfigured(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()
	// OllamaService is nil by default

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:     "test query",
		Namespace: "codebase",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

// mockQuicBaaSWithNilVector returns nil from Vector() to test not-configured scenario.
type mockQuicBaaSWithNilVector struct {
	*testutil.MockQuicBaaS
	ollamaSvc service.OllamaService
}

func (m *mockQuicBaaSWithNilVector) Vector() service.VectorService {
	return nil
}

func (m *mockQuicBaaSWithNilVector) Ollama() service.OllamaService {
	return m.ollamaSvc
}

func TestHandleRAGQuery_VectorNotConfigured(t *testing.T) {
	baseMockDB := testutil.NewMockQuicBaaS()

	// Set up ollama but not vector
	ollamaSvc := &mockOllamaServiceForRAG{isAvailable: true}
	ollamaSvc.On("Embed", mock.Anything, mock.Anything).Return(&service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3},
		Model:          "nomic-embed-text",
		Dimensions:     3,
		ProcessingTime: 100 * time.Millisecond,
	}, nil)

	mockDB := &mockQuicBaaSWithNilVector{
		MockQuicBaaS: baseMockDB,
		ollamaSvc:    ollamaSvc,
	}

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:     "test query",
		Namespace: "codebase",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected status 503, got %d", w.Code)
	}
}

func TestHandleRAGQuery_EmbeddingError(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	vectorSvc := &mockVectorServiceForRAG{}
	ollamaSvc := &mockOllamaServiceForRAG{isAvailable: true}
	ollamaSvc.On("Embed", mock.Anything, mock.Anything).Return(nil, errors.New("embedding generation failed"))

	mockDB.VectorService = vectorSvc
	mockDB.OllamaService = ollamaSvc

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:     "test query",
		Namespace: "codebase",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandleRAGQuery_SearchError(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	vectorSvc := &mockVectorServiceForRAG{}
	vectorSvc.On("SearchSimilar", mock.Anything, mock.Anything).Return(nil, errors.New("search failed"))

	ollamaSvc := &mockOllamaServiceForRAG{isAvailable: true}
	ollamaSvc.On("Embed", mock.Anything, mock.Anything).Return(&service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3},
		Model:          "nomic-embed-text",
		Dimensions:     3,
		ProcessingTime: 100 * time.Millisecond,
	}, nil)

	mockDB.VectorService = vectorSvc
	mockDB.OllamaService = ollamaSvc

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:     "test query",
		Namespace: "codebase",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", w.Code)
	}
}

func TestHandleRAGQuery_WithCitations(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	vectorSvc := &mockVectorServiceForRAG{}
	vectorSvc.On("SearchSimilar", mock.Anything, mock.Anything).Return(&service.SearchSimilarResponse{
		Results: []*service.VectorSearchResult{
			{
				ID:    1,
				Score: 0.95,
				Metadata: map[string]interface{}{
					"content":    "func main() {}",
					"file_path":  "main.go",
					"start_line": 1,
					"end_line":   3,
				},
			},
		},
		Timestamp:     time.Now().UnixNano(),
		QueryDuration: 50000,
	}, nil)

	ollamaSvc := &mockOllamaServiceForRAG{isAvailable: true}
	ollamaSvc.On("Embed", mock.Anything, mock.Anything).Return(&service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3},
		Model:          "nomic-embed-text",
		Dimensions:     3,
		ProcessingTime: 100 * time.Millisecond,
	}, nil)

	mockDB.VectorService = vectorSvc
	mockDB.OllamaService = ollamaSvc

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:            "test query",
		Namespace:        "codebase",
		IncludeCitations: true,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp RAGQueryResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Citations should be present
	if len(resp.Citations) == 0 {
		t.Error("expected citations to be included")
	}
}

func TestHandleRAGQuery_WithModel(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	vectorSvc := &mockVectorServiceForRAG{}
	vectorSvc.On("SearchSimilar", mock.Anything, mock.Anything).Return(&service.SearchSimilarResponse{
		Results: []*service.VectorSearchResult{
			{
				ID:    1,
				Score: 0.95,
				Metadata: map[string]interface{}{
					"content": "func main() {}",
				},
			},
		},
		Timestamp:     time.Now().UnixNano(),
		QueryDuration: 50000,
	}, nil)

	ollamaSvc := &mockOllamaServiceForRAG{isAvailable: true}
	ollamaSvc.On("EmbedWithModel", mock.Anything, mock.MatchedBy(func(req *service.OllamaEmbedWithModelRequest) bool {
		return req.Model == "mxbai-embed-large"
	})).Return(&service.OllamaEmbedResponse{
		Embedding:      []float32{0.1, 0.2, 0.3},
		Model:          "mxbai-embed-large",
		Dimensions:     3,
		ProcessingTime: 100 * time.Millisecond,
	}, nil)

	mockDB.VectorService = vectorSvc
	mockDB.OllamaService = ollamaSvc

	s := &Server{service: mockDB}

	body := RAGQueryRequest{
		Query:     "test query",
		Namespace: "codebase",
		Model:     "mxbai-embed-large",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/query", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGQuery(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleRAGBuildContext_Success(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	s := &Server{service: mockDB}

	body := RAGBuildContextRequest{
		Results: []RAGResultInput{
			{
				Content: "func main() { fmt.Println(\"hello\") }",
				Score:   0.95,
				Metadata: map[string]string{
					"file_path":  "main.go",
					"file_name":  "main.go",
					"language":   "go",
					"start_line": "1",
					"end_line":   "3",
				},
			},
		},
		MaxTokens: 2000,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/context", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGBuildContext(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp RAGBuildContextResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ResultCount != 1 {
		t.Errorf("expected result_count 1, got %d", resp.ResultCount)
	}

	if resp.Context == "" {
		t.Error("expected context to be non-empty")
	}
}

func TestHandleRAGBuildContext_EmptyResults(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	s := &Server{service: mockDB}

	body := RAGBuildContextRequest{
		Results: []RAGResultInput{},
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/context", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGBuildContext(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleRAGBuildContext_InvalidJSON(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	s := &Server{service: mockDB}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/context", bytes.NewReader([]byte("invalid json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGBuildContext(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestHandleRAGBuildContext_WithCitations(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	s := &Server{service: mockDB}

	body := RAGBuildContextRequest{
		Results: []RAGResultInput{
			{
				Content: "func main() { fmt.Println(\"hello\") }",
				Score:   0.95,
				Metadata: map[string]string{
					"file_path":  "main.go",
					"start_line": "1",
					"end_line":   "3",
				},
			},
		},
		IncludeCitations: true,
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/context", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGBuildContext(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp RAGBuildContextResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(resp.Citations) == 0 {
		t.Error("expected citations to be included")
	}
}

func TestHandleRAGBuildContext_MultipleResults(t *testing.T) {
	mockDB := testutil.NewMockQuicBaaS()

	s := &Server{service: mockDB}

	body := RAGBuildContextRequest{
		Results: []RAGResultInput{
			{
				Content: "func main() {}",
				Score:   0.95,
				Metadata: map[string]string{
					"file_path": "main.go",
				},
			},
			{
				Content: "func helper() {}",
				Score:   0.85,
				Metadata: map[string]string{
					"file_path": "utils.go",
				},
			},
			{
				Content: "func test() {}",
				Score:   0.75,
				Metadata: map[string]string{
					"file_path": "test.go",
				},
			},
		},
		MaxTokens: 4000,
		Template:  "markdown",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/context", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.HandleRAGBuildContext(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp RAGBuildContextResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.ResultCount != 3 {
		t.Errorf("expected result_count 3, got %d", resp.ResultCount)
	}
}

func TestHandleRAGBuildContext_TruncationStrategies(t *testing.T) {
	testCases := []struct {
		name     string
		strategy string
	}{
		{"smart", "smart"},
		{"hard", "hard"},
		{"none", "none"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockDB := testutil.NewMockQuicBaaS()

			s := &Server{service: mockDB}

			body := RAGBuildContextRequest{
				Results: []RAGResultInput{
					{
						Content: "func main() {}",
						Score:   0.95,
					},
				},
				TruncationStrategy: tc.strategy,
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/context", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			s.HandleRAGBuildContext(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestHandleRAGBuildContext_Templates(t *testing.T) {
	testCases := []struct {
		name     string
		template string
	}{
		{"markdown", "markdown"},
		{"json", "json"},
		{"xml", "xml"},
		{"plain", "plain"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mockDB := testutil.NewMockQuicBaaS()

			s := &Server{service: mockDB}

			body := RAGBuildContextRequest{
				Results: []RAGResultInput{
					{
						Content: "func main() {}",
						Score:   0.95,
					},
				},
				Template: tc.template,
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest(http.MethodPost, "/api/v1/rag/context", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			s.HandleRAGBuildContext(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestGetStringFromInterfaceMap(t *testing.T) {
	t.Run("nil map returns empty string", func(t *testing.T) {
		result := getStringFromInterfaceMap(nil, "key")
		assert.Equal(t, "", result)
	})

	t.Run("key not found returns empty string", func(t *testing.T) {
		m := map[string]interface{}{
			"other": "value",
		}
		result := getStringFromInterfaceMap(m, "key")
		assert.Equal(t, "", result)
	})

	t.Run("key with string value returns value", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "value",
		}
		result := getStringFromInterfaceMap(m, "key")
		assert.Equal(t, "value", result)
	})

	t.Run("key with non-string value returns empty string", func(t *testing.T) {
		m := map[string]interface{}{
			"key": 12345,
		}
		result := getStringFromInterfaceMap(m, "key")
		assert.Equal(t, "", result)
	})

	t.Run("key with nil value returns empty string", func(t *testing.T) {
		m := map[string]interface{}{
			"key": nil,
		}
		result := getStringFromInterfaceMap(m, "key")
		assert.Equal(t, "", result)
	})

	t.Run("empty string key", func(t *testing.T) {
		m := map[string]interface{}{
			"": "empty key value",
		}
		result := getStringFromInterfaceMap(m, "")
		assert.Equal(t, "empty key value", result)
	})
}

func TestGetIntFromInterfaceMap(t *testing.T) {
	t.Run("nil map returns 0", func(t *testing.T) {
		result := getIntFromInterfaceMap(nil, "key")
		assert.Equal(t, 0, result)
	})

	t.Run("key not found returns 0", func(t *testing.T) {
		m := map[string]interface{}{
			"other": 42,
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 0, result)
	})

	t.Run("key with int value returns value", func(t *testing.T) {
		m := map[string]interface{}{
			"key": 42,
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 42, result)
	})

	t.Run("key with int64 value returns value", func(t *testing.T) {
		m := map[string]interface{}{
			"key": int64(100),
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 100, result)
	})

	t.Run("key with float64 value returns int", func(t *testing.T) {
		m := map[string]interface{}{
			"key": 42.7,
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 42, result)
	})

	t.Run("key with string numeric value parses to int", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "123",
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 123, result)
	})

	t.Run("key with non-numeric string returns 0", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "not a number",
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 0, result)
	})

	t.Run("key with other type returns 0", func(t *testing.T) {
		m := map[string]interface{}{
			"key": []int{1, 2, 3},
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 0, result)
	})

	t.Run("key with nil value returns 0", func(t *testing.T) {
		m := map[string]interface{}{
			"key": nil,
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 0, result)
	})

	t.Run("negative int value", func(t *testing.T) {
		m := map[string]interface{}{
			"key": -42,
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, -42, result)
	})

	t.Run("zero value", func(t *testing.T) {
		m := map[string]interface{}{
			"key": 0,
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 0, result)
	})

	t.Run("large int64 value", func(t *testing.T) {
		m := map[string]interface{}{
			"key": int64(9223372036854775807),
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.NotEqual(t, 0, result) // Will be truncated but non-zero
	})

	t.Run("string with leading zeros", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "007",
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, 7, result)
	})

	t.Run("string with negative number", func(t *testing.T) {
		m := map[string]interface{}{
			"key": "-99",
		}
		result := getIntFromInterfaceMap(m, "key")
		assert.Equal(t, -99, result)
	})
}

// =============================================================================
// RAG Document CRUD Tests
// =============================================================================

func TestHandleRAGListDocuments(t *testing.T) {
	t.Run("list documents successfully", func(t *testing.T) {
		mockDB := testutil.NewMockQuicBaaS()
		vectorSvc := &mockVectorServiceForRAG{}
		vectorSvc.On("ListVectors", mock.Anything, mock.Anything).Return(&service.ListVectorsResponse{
			Vectors: []*service.VectorData{
				{
					ID:        1,
					Metadata:  map[string]interface{}{"content": "doc content"},
					UpdatedAt: time.Now().UnixNano(),
				},
			},
			NextCursor: "",
			TotalCount: 1,
		}, nil)
		mockDB.VectorService = vectorSvc

		s := &Server{service: mockDB}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rag/documents?namespace=test", nil)
		w := httptest.NewRecorder()
		s.HandleRAGListDocuments(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp RAGListDocumentsResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)
		assert.Len(t, resp.Documents, 1)
		assert.Equal(t, uint64(1), resp.Documents[0].ID)
	})

	t.Run("vector service not configured returns 503", func(t *testing.T) {
		baseMockDB := testutil.NewMockQuicBaaS()
		mockDB := &mockQuicBaaSWithNilVector{MockQuicBaaS: baseMockDB}
		s := &Server{service: mockDB}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rag/documents", nil)
		w := httptest.NewRecorder()
		s.HandleRAGListDocuments(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

func TestHandleRAGGetDocument(t *testing.T) {
	t.Run("get document by id successfully", func(t *testing.T) {
		mockDB := testutil.NewMockQuicBaaS()
		vectorSvc := &mockVectorServiceForRAG{}
		vectorSvc.On("GetVector", mock.Anything, mock.MatchedBy(func(req *service.GetVectorRequest) bool {
			return req.ID == 42 && req.Namespace == "default"
		})).Return(&service.GetVectorResponse{
			ID:        42,
			Metadata:  map[string]interface{}{"content": "my document"},
			Found:     true,
			UpdatedAt: time.Now().UnixNano(),
		}, nil)
		mockDB.VectorService = vectorSvc

		s := &Server{service: mockDB}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "42")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rag/documents/42", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		s.HandleRAGGetDocument(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp RAGDocumentResponse
		err := json.NewDecoder(w.Body).Decode(&resp)
		assert.NoError(t, err)
		assert.Equal(t, uint64(42), resp.ID)
		assert.Equal(t, "my document", resp.Content)
	})

	t.Run("invalid document id returns 400", func(t *testing.T) {
		mockDB := testutil.NewMockQuicBaaS()
		s := &Server{service: mockDB}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "not-a-number")
		req := httptest.NewRequest(http.MethodGet, "/api/v1/rag/documents/not-a-number", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		s.HandleRAGGetDocument(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestHandleRAGDeleteDocument(t *testing.T) {
	t.Run("delete document successfully", func(t *testing.T) {
		mockDB := testutil.NewMockQuicBaaS()
		vectorSvc := &mockVectorServiceForRAG{}
		vectorSvc.On("DeleteVector", mock.Anything, mock.MatchedBy(func(req *service.DeleteVectorRequest) bool {
			return req.ID == 7
		})).Return(&service.DeleteVectorResponse{Success: true}, nil)
		mockDB.VectorService = vectorSvc

		s := &Server{service: mockDB}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "7")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/rag/documents/7", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		s.HandleRAGDeleteDocument(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("invalid id returns 400", func(t *testing.T) {
		mockDB := testutil.NewMockQuicBaaS()
		s := &Server{service: mockDB}

		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", "bad")
		req := httptest.NewRequest(http.MethodDelete, "/api/v1/rag/documents/bad", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		w := httptest.NewRecorder()

		s.HandleRAGDeleteDocument(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
