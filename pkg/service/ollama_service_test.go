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

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOllamaService_NilConfig(t *testing.T) {
	_, err := NewOllamaService(nil)
	if err != ErrOllamaNotConfigured {
		t.Errorf("expected ErrOllamaNotConfigured, got %v", err)
	}
}

func TestNewOllamaService_Disabled(t *testing.T) {
	cfg := &config.OllamaConfig{
		Enabled: false,
	}

	_, err := NewOllamaService(cfg)
	if err != ErrOllamaDisabled {
		t.Errorf("expected ErrOllamaDisabled, got %v", err)
	}
}

func TestNewOllamaService_InvalidConfig(t *testing.T) {
	cfg := &config.OllamaConfig{
		Enabled: true,
		BaseURL: "", // Invalid - empty when enabled
	}

	_, err := NewOllamaService(cfg)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestNewOllamaService_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if svc.DefaultModel() != "nomic-embed-text" {
		t.Errorf("expected default model 'nomic-embed-text', got %s", svc.DefaultModel())
	}

	if svc.BaseURL() != server.URL {
		t.Errorf("expected base URL %s, got %s", server.URL, svc.BaseURL())
	}
}

func TestOllamaService_Embed_NilRequest(t *testing.T) {
	svc := createTestOllamaService(t)
	_, err := svc.Embed(context.Background(), nil)
	if err != ErrInvalidRequest {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOllamaService_Embed_Success(t *testing.T) {
	expectedEmbedding := []float32{0.1, 0.2, 0.3}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			resp := map[string]interface{}{
				"embedding": expectedEmbedding,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := createTestOllamaServiceWithURL(t, server.URL)

	resp, err := svc.Embed(context.Background(), &OllamaEmbedRequest{
		Text: "test text",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(resp.Embedding) != len(expectedEmbedding) {
		t.Errorf("expected %d dimensions, got %d", len(expectedEmbedding), len(resp.Embedding))
	}

	if resp.Dimensions != len(expectedEmbedding) {
		t.Errorf("expected dimensions %d, got %d", len(expectedEmbedding), resp.Dimensions)
	}

	if resp.Model != "nomic-embed-text" {
		t.Errorf("expected model 'nomic-embed-text', got %s", resp.Model)
	}
}

func TestOllamaService_EmbedWithModel_NilRequest(t *testing.T) {
	svc := createTestOllamaService(t)
	_, err := svc.EmbedWithModel(context.Background(), nil)
	if err != ErrInvalidRequest {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOllamaService_EmbedWithModel_Success(t *testing.T) {
	expectedEmbedding := []float32{0.4, 0.5, 0.6}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			resp := map[string]interface{}{
				"embedding": expectedEmbedding,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := createTestOllamaServiceWithURL(t, server.URL)

	resp, err := svc.EmbedWithModel(context.Background(), &OllamaEmbedWithModelRequest{
		Text:  "test text",
		Model: "custom-model",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Model != "custom-model" {
		t.Errorf("expected model 'custom-model', got %s", resp.Model)
	}
}

func TestOllamaService_EmbedBatch_NilRequest(t *testing.T) {
	svc := createTestOllamaService(t)
	_, err := svc.EmbedBatch(context.Background(), nil)
	if err != ErrInvalidRequest {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOllamaService_EmbedBatch_Success(t *testing.T) {
	expectedEmbedding := []float32{0.1, 0.2, 0.3}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			resp := map[string]interface{}{
				"embedding": expectedEmbedding,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := createTestOllamaServiceWithURL(t, server.URL)

	resp, err := svc.EmbedBatch(context.Background(), &OllamaEmbedBatchRequest{
		Texts: []string{"text1", "text2"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Count != 2 {
		t.Errorf("expected count 2, got %d", resp.Count)
	}

	if len(resp.Embeddings) != 2 {
		t.Errorf("expected 2 embeddings, got %d", len(resp.Embeddings))
	}
}

func TestOllamaService_EmbedBatch_WithModel(t *testing.T) {
	expectedEmbedding := []float32{0.1, 0.2, 0.3}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embeddings" {
			resp := map[string]interface{}{
				"embedding": expectedEmbedding,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := createTestOllamaServiceWithURL(t, server.URL)

	resp, err := svc.EmbedBatch(context.Background(), &OllamaEmbedBatchRequest{
		Texts: []string{"text1"},
		Model: "custom-model",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Model != "custom-model" {
		t.Errorf("expected model 'custom-model', got %s", resp.Model)
	}
}

func TestOllamaService_ListModels_Success(t *testing.T) {
	models := []map[string]interface{}{
		{"name": "nomic-embed-text", "size": 1000000, "digest": "sha256:abc"},
		{"name": "llama3.2", "size": 5000000, "digest": "sha256:def"},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/tags" {
			resp := map[string]interface{}{"models": models}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := createTestOllamaServiceWithURL(t, server.URL)

	resp, err := svc.ListModels(context.Background(), &OllamaListModelsRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Count != 2 {
		t.Errorf("expected count 2, got %d", resp.Count)
	}

	if len(resp.Models) != 2 {
		t.Errorf("expected 2 models, got %d", len(resp.Models))
	}

	if resp.Models[0].Name != "nomic-embed-text" {
		t.Errorf("expected first model 'nomic-embed-text', got %s", resp.Models[0].Name)
	}
}

func TestOllamaService_GetModelInfo_NilRequest(t *testing.T) {
	svc := createTestOllamaService(t)
	_, err := svc.GetModelInfo(context.Background(), nil)
	if err != ErrInvalidRequest {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOllamaService_GetModelInfo_EmptyModel(t *testing.T) {
	svc := createTestOllamaService(t)
	_, err := svc.GetModelInfo(context.Background(), &OllamaGetModelInfoRequest{Model: ""})
	if err != ErrInvalidRequest {
		t.Errorf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestOllamaService_GetModelInfo_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/show" {
			resp := map[string]interface{}{
				"license":   "MIT",
				"modelfile": "FROM nomic-embed-text",
				"details": map[string]interface{}{
					"format":             "gguf",
					"family":             "nomic",
					"parameter_size":     "137M",
					"quantization_level": "Q4_0",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := createTestOllamaServiceWithURL(t, server.URL)

	resp, err := svc.GetModelInfo(context.Background(), &OllamaGetModelInfoRequest{
		Model: "nomic-embed-text",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Model != "nomic-embed-text" {
		t.Errorf("expected model 'nomic-embed-text', got %s", resp.Model)
	}

	if resp.Details.License != "MIT" {
		t.Errorf("expected license 'MIT', got %s", resp.Details.License)
	}

	if resp.Details.Family != "nomic" {
		t.Errorf("expected family 'nomic', got %s", resp.Details.Family)
	}
}

func TestOllamaService_Health_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/version" {
			resp := map[string]interface{}{"version": "0.1.0"}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	svc := createTestOllamaServiceWithURL(t, server.URL)

	resp, err := svc.Health(context.Background(), &OllamaHealthRequest{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if !resp.Available {
		t.Error("expected available to be true")
	}

	if resp.Version != "0.1.0" {
		t.Errorf("expected version '0.1.0', got %s", resp.Version)
	}
}

func TestOllamaService_Health_Unavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	resp, _ := svc.Health(context.Background(), &OllamaHealthRequest{})

	// Should always return a response, even when unhealthy
	if resp == nil {
		t.Fatal("expected response even when unavailable")
	}

	if resp.Available {
		t.Error("expected available to be false")
	}
}

func TestOllamaService_IsAvailable(t *testing.T) {
	svc := createTestOllamaService(t)

	// Initially not available (no health check performed)
	if svc.IsAvailable() {
		t.Error("expected initially not available")
	}
}

func TestOllamaServiceError(t *testing.T) {
	err := &ollamaServiceError{message: "test error"}
	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got %s", err.Error())
	}
}

// Helper functions

func createTestOllamaService(t *testing.T) OllamaService {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)
	return createTestOllamaServiceWithURL(t, server.URL)
}

func createTestOllamaServiceWithURL(t *testing.T, url string) OllamaService {
	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               url,
		DefaultEmbeddingModel: "nomic-embed-text",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg)
	if err != nil {
		t.Fatalf("failed to create ollama service: %v", err)
	}
	return svc
}

// TestOllamaServiceErrors tests Ollama service error types.
func TestOllamaServiceErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "ErrOllamaNotConfigured",
			err:      ErrOllamaNotConfigured,
			expected: "ollama not configured",
		},
		{
			name:     "ErrOllamaDisabled",
			err:      ErrOllamaDisabled,
			expected: "ollama integration disabled",
		},
		{
			name:     "ErrInvalidRequest",
			err:      ErrInvalidRequest,
			expected: "invalid request",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("expected error message '%s', got '%s'", tt.expected, tt.err.Error())
			}
		})
	}
}

// TestOllamaServiceErrorType tests the ollamaServiceError type.
func TestOllamaServiceErrorType(t *testing.T) {
	err := &ollamaServiceError{message: "custom error message"}
	if err.Error() != "custom error message" {
		t.Errorf("expected 'custom error message', got '%s'", err.Error())
	}
}

// TestOllamaRequestTypes tests Ollama request type structures.
func TestOllamaRequestTypes(t *testing.T) {
	t.Run("OllamaEmbedRequest", func(t *testing.T) {
		req := &OllamaEmbedRequest{Text: "test text"}
		if req.Text != "test text" {
			t.Errorf("expected Text 'test text', got '%s'", req.Text)
		}
	})

	t.Run("OllamaEmbedWithModelRequest", func(t *testing.T) {
		req := &OllamaEmbedWithModelRequest{
			Model: "nomic-embed-text",
			Text:  "test text",
		}
		if req.Model != "nomic-embed-text" {
			t.Errorf("expected Model 'nomic-embed-text', got '%s'", req.Model)
		}
		if req.Text != "test text" {
			t.Errorf("expected Text 'test text', got '%s'", req.Text)
		}
	})

	t.Run("OllamaEmbedBatchRequest", func(t *testing.T) {
		req := &OllamaEmbedBatchRequest{
			Texts: []string{"text1", "text2", "text3"},
			Model: "nomic-embed-text",
		}
		if len(req.Texts) != 3 {
			t.Errorf("expected 3 texts, got %d", len(req.Texts))
		}
		if req.Model != "nomic-embed-text" {
			t.Errorf("expected Model 'nomic-embed-text', got '%s'", req.Model)
		}
	})

	t.Run("OllamaListModelsRequest", func(t *testing.T) {
		req := &OllamaListModelsRequest{}
		if req == nil {
			t.Error("expected non-nil request")
		}
	})

	t.Run("OllamaGetModelInfoRequest", func(t *testing.T) {
		req := &OllamaGetModelInfoRequest{Model: "llama2"}
		if req.Model != "llama2" {
			t.Errorf("expected Model 'llama2', got '%s'", req.Model)
		}
	})

	t.Run("OllamaHealthRequest", func(t *testing.T) {
		req := &OllamaHealthRequest{}
		if req == nil {
			t.Error("expected non-nil request")
		}
	})
}

// TestOllamaResponseTypes tests Ollama response type structures.
func TestOllamaResponseTypes(t *testing.T) {
	t.Run("OllamaEmbedResponse", func(t *testing.T) {
		resp := &OllamaEmbedResponse{
			Embedding:  []float32{0.1, 0.2, 0.3},
			Model:      "nomic-embed-text",
			Dimensions: 3,
		}
		if len(resp.Embedding) != 3 {
			t.Errorf("expected 3 dimensions, got %d", len(resp.Embedding))
		}
		if resp.Model != "nomic-embed-text" {
			t.Errorf("expected Model 'nomic-embed-text', got '%s'", resp.Model)
		}
		if resp.Dimensions != 3 {
			t.Errorf("expected Dimensions 3, got %d", resp.Dimensions)
		}
	})

	t.Run("OllamaEmbedBatchResponse", func(t *testing.T) {
		resp := &OllamaEmbedBatchResponse{
			Embeddings: [][]float32{
				{0.1, 0.2, 0.3},
				{0.4, 0.5, 0.6},
			},
			Model:      "nomic-embed-text",
			Dimensions: 3,
			Count:      2,
		}
		if resp.Count != 2 {
			t.Errorf("expected Count 2, got %d", resp.Count)
		}
		if len(resp.Embeddings) != 2 {
			t.Errorf("expected 2 embeddings, got %d", len(resp.Embeddings))
		}
	})

	t.Run("OllamaListModelsResponse", func(t *testing.T) {
		resp := &OllamaListModelsResponse{
			Models: []OllamaModelInfo{
				{Name: "llama2", Size: 3800000000},
				{Name: "codellama", Size: 4200000000},
			},
			Count: 2,
		}
		if resp.Count != 2 {
			t.Errorf("expected Count 2, got %d", resp.Count)
		}
		if len(resp.Models) != 2 {
			t.Errorf("expected 2 models, got %d", len(resp.Models))
		}
	})

	t.Run("OllamaGetModelInfoResponse", func(t *testing.T) {
		resp := &OllamaGetModelInfoResponse{
			Model: "llama2",
			Details: OllamaModelDetails{
				Family:        "llama",
				ParameterSize: "7B",
				Format:        "gguf",
			},
		}
		if resp.Model != "llama2" {
			t.Errorf("expected Model 'llama2', got '%s'", resp.Model)
		}
		if resp.Details.Family != "llama" {
			t.Errorf("expected Family 'llama', got '%s'", resp.Details.Family)
		}
	})

	t.Run("OllamaHealthResponse", func(t *testing.T) {
		resp := &OllamaHealthResponse{
			Available: true,
			Version:   "0.1.0",
			BaseURL:   "http://localhost:11434",
		}
		if !resp.Available {
			t.Error("expected Available to be true")
		}
		if resp.Version != "0.1.0" {
			t.Errorf("expected Version '0.1.0', got '%s'", resp.Version)
		}
	})
}

// TestOllamaModelInfo tests OllamaModelInfo struct.
func TestOllamaModelInfo(t *testing.T) {
	info := OllamaModelInfo{
		Name:   "nomic-embed-text",
		Size:   274000000,
		Digest: "abc123",
	}

	if info.Name != "nomic-embed-text" {
		t.Errorf("expected Name 'nomic-embed-text', got '%s'", info.Name)
	}
	if info.Size != 274000000 {
		t.Errorf("expected Size 274000000, got %d", info.Size)
	}
	if info.Digest != "abc123" {
		t.Errorf("expected Digest 'abc123', got '%s'", info.Digest)
	}
}

// TestOllamaModelDetails tests OllamaModelDetails struct.
func TestOllamaModelDetails(t *testing.T) {
	details := OllamaModelDetails{
		License:           "MIT",
		Modelfile:         "FROM llama2",
		Format:            "gguf",
		Family:            "llama",
		ParameterSize:     "7B",
		QuantizationLevel: "Q4_0",
	}

	if details.License != "MIT" {
		t.Errorf("expected License 'MIT', got '%s'", details.License)
	}
	if details.Format != "gguf" {
		t.Errorf("expected Format 'gguf', got '%s'", details.Format)
	}
	if details.QuantizationLevel != "Q4_0" {
		t.Errorf("expected QuantizationLevel 'Q4_0', got '%s'", details.QuantizationLevel)
	}
}

func createTestOllamaHTTPServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/embeddings":
			resp := map[string]interface{}{
				"embedding": []float32{0.1, 0.2, 0.3},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		case "/api/tags":
			resp := map[string]interface{}{
				"models": []interface{}{},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		case "/api/version":
			resp := map[string]interface{}{
				"version": "0.1.0-test",
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
}

func TestWithOllamaClient_SetsCustomClient(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	clientCfg := ollama.ClientConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	}
	client := ollama.NewClient(clientCfg)

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg, WithOllamaClient(client))
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.Equal(t, server.URL, svc.BaseURL())
}

func TestWithOllamaClient_NilFallsBackToDefault(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg, WithOllamaClient(nil))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestWithOllamaEmbeddingService_SetsCustomService(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	clientCfg := ollama.ClientConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	}
	client := ollama.NewClient(clientCfg)
	embeddingSvc := ollama.NewEmbeddingService(client, "custom-model")

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg, WithOllamaEmbeddingService(embeddingSvc))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestWithOllamaEmbeddingService_NilFallsBackToDefault(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg, WithOllamaEmbeddingService(nil))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestWithOllamaModelService_SetsCustomService(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	clientCfg := ollama.ClientConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	}
	client := ollama.NewClient(clientCfg)
	modelSvc := ollama.NewModelService(client)

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg, WithOllamaModelService(modelSvc))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestWithOllamaModelService_NilFallsBackToDefault(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg, WithOllamaModelService(nil))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestWithOllamaHealthChecker_SetsCustomChecker(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	clientCfg := ollama.ClientConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	}
	client := ollama.NewClient(clientCfg)
	health := ollama.NewHealthChecker(client)

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg, WithOllamaHealthChecker(health))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestWithOllamaHealthChecker_NilFallsBackToDefault(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg, WithOllamaHealthChecker(nil))
	require.NoError(t, err)
	require.NotNil(t, svc)
}

func TestNewDefaultOllamaService_CreatesServiceWithDefaults(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewDefaultOllamaService(cfg)
	require.NoError(t, err)
	require.NotNil(t, svc)
	assert.Equal(t, server.URL, svc.BaseURL())
	assert.Equal(t, "test-model", svc.DefaultModel())
}

func TestNewDefaultOllamaService_NilConfigReturnsError(t *testing.T) {
	svc, err := NewDefaultOllamaService(nil)
	require.Error(t, err)
	assert.Nil(t, svc)
}

func TestOllamaService_AllCustomOptions(t *testing.T) {
	server := createTestOllamaHTTPServer()
	defer server.Close()

	clientCfg := ollama.ClientConfig{
		BaseURL:    server.URL,
		Timeout:    5 * time.Second,
		MaxRetries: 0,
	}
	client := ollama.NewClient(clientCfg)

	cfg := &config.OllamaConfig{
		Enabled:               true,
		BaseURL:               server.URL,
		DefaultEmbeddingModel: "test-model",
		Timeout:               5 * time.Second,
		MaxRetries:            0,
	}

	svc, err := NewOllamaService(cfg,
		WithOllamaClient(client),
		WithOllamaEmbeddingService(ollama.NewEmbeddingService(client, "custom")),
		WithOllamaModelService(ollama.NewModelService(client)),
		WithOllamaHealthChecker(ollama.NewHealthChecker(client)),
	)
	require.NoError(t, err)
	require.NotNil(t, svc)

	// Perform a health check first to update the cached availability status.
	// IsAvailable() returns the last cached status, which starts as false.
	healthResp, err := svc.Health(context.Background(), &OllamaHealthRequest{})
	require.NoError(t, err)
	require.NotNil(t, healthResp)
	assert.True(t, healthResp.Available)

	// Now IsAvailable should reflect the successful health check
	available := svc.IsAvailable()
	assert.True(t, available)
}
