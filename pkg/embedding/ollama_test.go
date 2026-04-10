//go:build vector

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

package embedding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ollamaEmbedRequest mirrors the Ollama embeddings request format.
type ollamaEmbedRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// ollamaEmbedResponse mirrors the Ollama embeddings response format.
type ollamaEmbedResponse struct {
	Embedding []float32 `json:"embedding"`
}

// ollamaErrorResponse mirrors the Ollama error response format.
type ollamaErrorResponse struct {
	Error string `json:"error"`
}

func TestNewOllamaProvider(t *testing.T) {
	tests := []struct {
		name       string
		config     OllamaConfig
		wantModel  string
		wantDims   int
		wantTokens int
	}{
		{
			name:       "default_config",
			config:     OllamaConfig{},
			wantModel:  "nomic-embed-text",
			wantDims:   768,
			wantTokens: 8192,
		},
		{
			name: "custom_model_nomic",
			config: OllamaConfig{
				Model: "nomic-embed-text",
			},
			wantModel:  "nomic-embed-text",
			wantDims:   768,
			wantTokens: 8192,
		},
		{
			name: "custom_model_mxbai",
			config: OllamaConfig{
				Model: "mxbai-embed-large",
			},
			wantModel:  "mxbai-embed-large",
			wantDims:   1024,
			wantTokens: 8192,
		},
		{
			name: "custom_model_all_minilm",
			config: OllamaConfig{
				Model: "all-minilm",
			},
			wantModel:  "all-minilm",
			wantDims:   384,
			wantTokens: 8192,
		},
		{
			name: "custom_model_bge_large",
			config: OllamaConfig{
				Model: "bge-large-en-v1.5",
			},
			wantModel:  "bge-large-en-v1.5",
			wantDims:   1024,
			wantTokens: 8192,
		},
		{
			name: "custom_dimensions_override",
			config: OllamaConfig{
				Model:      "nomic-embed-text",
				Dimensions: 512,
			},
			wantModel:  "nomic-embed-text",
			wantDims:   512,
			wantTokens: 8192,
		},
		{
			name: "unknown_model_auto_detect",
			config: OllamaConfig{
				Model: "custom-unknown-model",
			},
			wantModel:  "custom-unknown-model",
			wantDims:   0, // Will auto-detect on first request
			wantTokens: 8192,
		},
		{
			name: "custom_base_url",
			config: OllamaConfig{
				BaseURL: "http://custom-host:11434",
				Model:   "nomic-embed-text",
			},
			wantModel:  "nomic-embed-text",
			wantDims:   768,
			wantTokens: 8192,
		},
		{
			name: "custom_timeout_and_retries",
			config: OllamaConfig{
				Model:        "nomic-embed-text",
				Timeout:      60 * time.Second,
				MaxRetries:   5,
				RetryBackoff: 2 * time.Second,
			},
			wantModel:  "nomic-embed-text",
			wantDims:   768,
			wantTokens: 8192,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOllamaProvider(tt.config)
			if err != nil {
				t.Fatalf("NewOllamaProvider() error = %v", err)
			}
			defer provider.Close()

			if provider.Model() != tt.wantModel {
				t.Errorf("Model() = %v, want %v", provider.Model(), tt.wantModel)
			}
			if provider.Dimensions() != tt.wantDims {
				t.Errorf("Dimensions() = %v, want %v", provider.Dimensions(), tt.wantDims)
			}
			if provider.MaxTokens() != tt.wantTokens {
				t.Errorf("MaxTokens() = %v, want %v", provider.MaxTokens(), tt.wantTokens)
			}
			if provider.Name() != "ollama" {
				t.Errorf("Name() = %v, want ollama", provider.Name())
			}
		})
	}
}

func TestOllamaProvider_DefaultConfig(t *testing.T) {
	config := DefaultOllamaConfig()

	if config.BaseURL != DefaultOllamaURL {
		t.Errorf("BaseURL = %v, want %v", config.BaseURL, DefaultOllamaURL)
	}
	if config.Model != "nomic-embed-text" {
		t.Errorf("Model = %v, want nomic-embed-text", config.Model)
	}
	if config.Dimensions != 0 {
		t.Errorf("Dimensions = %v, want 0 (auto-detect)", config.Dimensions)
	}
	if config.Timeout != DefaultOllamaTimeout {
		t.Errorf("Timeout = %v, want %v", config.Timeout, DefaultOllamaTimeout)
	}
	if config.MaxRetries != DefaultOllamaMaxRetries {
		t.Errorf("MaxRetries = %v, want %v", config.MaxRetries, DefaultOllamaMaxRetries)
	}
	if config.RetryBackoff != DefaultOllamaRetryBackoff {
		t.Errorf("RetryBackoff = %v, want %v", config.RetryBackoff, DefaultOllamaRetryBackoff)
	}
}

func TestOllamaProvider_Embed(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		serverResponse ollamaEmbedResponse
		statusCode     int
		wantErr        bool
		errType        error
		wantDims       int
	}{
		{
			name: "successful_embedding",
			text: "test text for embedding",
			serverResponse: ollamaEmbedResponse{
				Embedding: make([]float32, 768),
			},
			statusCode: http.StatusOK,
			wantErr:    false,
			wantDims:   768,
		},
		{
			name:       "empty_text_error",
			text:       "",
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name: "empty_embedding_error",
			text: "test text",
			serverResponse: ollamaEmbedResponse{
				Embedding: []float32{},
			},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "model_not_found_error",
			text:       "test text",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name:       "bad_request_error",
			text:       "test text",
			statusCode: http.StatusBadRequest,
			wantErr:    true,
		},
		{
			name:       "server_error",
			text:       "test text",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request
				if r.Method != http.MethodPost {
					t.Errorf("expected POST request, got %s", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/api/embeddings") {
					t.Errorf("expected /api/embeddings endpoint, got %s", r.URL.Path)
				}

				// Parse request
				var req ollamaEmbedRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil && tt.statusCode == http.StatusOK {
					t.Errorf("failed to decode request: %v", err)
				}

				// Send response
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					if err := json.NewEncoder(w).Encode(tt.serverResponse); err != nil {
						t.Errorf("failed to encode response: %v", err)
					}
				} else if tt.statusCode == http.StatusNotFound {
					resp := ollamaErrorResponse{Error: "model 'test-model' not found"}
					if err := json.NewEncoder(w).Encode(resp); err != nil {
						t.Errorf("failed to encode error response: %v", err)
					}
				} else {
					resp := ollamaErrorResponse{Error: "test error"}
					if err := json.NewEncoder(w).Encode(resp); err != nil {
						t.Errorf("failed to encode error response: %v", err)
					}
				}
			}))
			defer server.Close()

			// Create provider
			config := OllamaConfig{
				BaseURL:      server.URL,
				Model:        "nomic-embed-text",
				Timeout:      5 * time.Second,
				MaxRetries:   0, // No retries for testing
				RetryBackoff: 10 * time.Millisecond,
			}
			provider, err := NewOllamaProvider(config)
			if err != nil {
				t.Fatalf("NewOllamaProvider() error = %v", err)
			}
			defer provider.Close()

			// Call Embed
			ctx := context.Background()
			embedding, err := provider.Embed(ctx, tt.text)

			if (err != nil) != tt.wantErr {
				t.Errorf("Embed() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(embedding) != tt.wantDims {
					t.Errorf("embedding dimensions = %d, want %d", len(embedding), tt.wantDims)
				}
			}
		})
	}
}

func TestOllamaProvider_EmbedBatch(t *testing.T) {
	tests := []struct {
		name       string
		texts      []string
		dimensions int
		statusCode int
		wantErr    bool
		wantCount  int
	}{
		{
			name:       "successful_batch",
			texts:      []string{"text1", "text2", "text3"},
			dimensions: 768,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantCount:  3,
		},
		{
			name:       "single_text_batch",
			texts:      []string{"single text"},
			dimensions: 768,
			statusCode: http.StatusOK,
			wantErr:    false,
			wantCount:  1,
		},
		{
			name:       "empty_texts_error",
			texts:      []string{},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "text_with_empty_element_error",
			texts:      []string{"text1", "", "text3"},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "all_empty_texts_error",
			texts:      []string{"", "", ""},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "server_error",
			texts:      []string{"text1", "text2"},
			dimensions: 768,
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					resp := ollamaEmbedResponse{
						Embedding: make([]float32, tt.dimensions),
					}
					if err := json.NewEncoder(w).Encode(resp); err != nil {
						t.Errorf("failed to encode response: %v", err)
					}
				} else {
					resp := ollamaErrorResponse{Error: "test error"}
					if err := json.NewEncoder(w).Encode(resp); err != nil {
						t.Errorf("failed to encode error response: %v", err)
					}
				}
			}))
			defer server.Close()

			// Create provider
			config := OllamaConfig{
				BaseURL:      server.URL,
				Model:        "nomic-embed-text",
				Timeout:      5 * time.Second,
				MaxRetries:   0,
				RetryBackoff: 10 * time.Millisecond,
			}
			provider, err := NewOllamaProvider(config)
			if err != nil {
				t.Fatalf("NewOllamaProvider() error = %v", err)
			}
			defer provider.Close()

			// Call EmbedBatch
			ctx := context.Background()
			embeddings, err := provider.EmbedBatch(ctx, tt.texts)

			if (err != nil) != tt.wantErr {
				t.Errorf("EmbedBatch() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(embeddings) != tt.wantCount {
					t.Errorf("embeddings count = %d, want %d", len(embeddings), tt.wantCount)
				}
			}
		})
	}
}

func TestOllamaProvider_AutoDetectDimensions(t *testing.T) {
	// Create test server that returns 512-dimensional embeddings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaEmbedResponse{
			Embedding: make([]float32, 512),
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// Create provider with unknown model (no pre-configured dimensions)
	config := OllamaConfig{
		BaseURL:      server.URL,
		Model:        "custom-unknown-model",
		Timeout:      5 * time.Second,
		MaxRetries:   0,
		RetryBackoff: 10 * time.Millisecond,
	}
	provider, err := NewOllamaProvider(config)
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}
	defer provider.Close()

	// Dimensions should be 0 initially
	if provider.Dimensions() != 0 {
		t.Errorf("initial dimensions = %d, want 0", provider.Dimensions())
	}

	// Make first request to auto-detect dimensions
	ctx := context.Background()
	embedding, err := provider.Embed(ctx, "test text")
	if err != nil {
		t.Fatalf("Embed() error = %v", err)
	}

	// Dimensions should now be 512
	if provider.Dimensions() != 512 {
		t.Errorf("auto-detected dimensions = %d, want 512", provider.Dimensions())
	}

	if len(embedding) != 512 {
		t.Errorf("embedding length = %d, want 512", len(embedding))
	}

	// Second request should succeed with same dimensions
	embedding2, err := provider.Embed(ctx, "test text 2")
	if err != nil {
		t.Fatalf("second Embed() error = %v", err)
	}

	if len(embedding2) != 512 {
		t.Errorf("second embedding length = %d, want 512", len(embedding2))
	}
}

func TestOllamaProvider_DimensionsMismatch(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		dims := 512
		if count > 1 {
			dims = 768 // Return different dimensions on second call
		}
		resp := ollamaEmbedResponse{
			Embedding: make([]float32, dims),
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	config := OllamaConfig{
		BaseURL:      server.URL,
		Model:        "custom-model",
		Timeout:      5 * time.Second,
		MaxRetries:   0,
		RetryBackoff: 10 * time.Millisecond,
	}
	provider, err := NewOllamaProvider(config)
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	// First call succeeds and sets dimensions to 512
	_, err = provider.Embed(ctx, "test1")
	if err != nil {
		t.Fatalf("first Embed() error = %v", err)
	}

	// Second call should fail due to dimension mismatch
	_, err = provider.Embed(ctx, "test2")
	if err == nil {
		t.Error("expected dimension mismatch error, got nil")
	}

	var dimErr *DimensionsMismatchError
	if !errors.As(err, &dimErr) {
		t.Errorf("expected DimensionsMismatchError, got: %T - %v", err, err)
	}
}

func TestOllamaProvider_ContextCancellation(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		resp := ollamaEmbedResponse{
			Embedding: make([]float32, 768),
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			// Expected when context is canceled
			return
		}
	}))
	defer server.Close()

	config := OllamaConfig{
		BaseURL:      server.URL,
		Model:        "nomic-embed-text",
		Timeout:      5 * time.Second,
		MaxRetries:   0,
		RetryBackoff: 10 * time.Millisecond,
	}
	provider, err := NewOllamaProvider(config)
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}
	defer provider.Close()

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = provider.Embed(ctx, "test text")
	if err == nil {
		t.Error("expected context cancellation/timeout error, got nil")
	}
}

func TestOllamaProvider_Close(t *testing.T) {
	config := OllamaConfig{
		BaseURL: "http://localhost:11434",
		Model:   "nomic-embed-text",
	}
	provider, err := NewOllamaProvider(config)
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}

	err = provider.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestOllamaProvider_BaseURL(t *testing.T) {
	config := OllamaConfig{
		BaseURL: "http://custom-host:12345",
		Model:   "nomic-embed-text",
	}
	provider, err := NewOllamaProvider(config)
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}
	defer provider.Close()

	if provider.BaseURL() != "http://custom-host:12345" {
		t.Errorf("BaseURL() = %v, want http://custom-host:12345", provider.BaseURL())
	}
}

func TestOllamaProvider_LargeBatch(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := ollamaEmbedResponse{
			Embedding: make([]float32, 768),
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	config := OllamaConfig{
		BaseURL:      server.URL,
		Model:        "nomic-embed-text",
		Timeout:      30 * time.Second,
		MaxRetries:   0,
		RetryBackoff: 10 * time.Millisecond,
	}
	provider, err := NewOllamaProvider(config)
	if err != nil {
		t.Fatalf("NewOllamaProvider() error = %v", err)
	}
	defer provider.Close()

	// Create a larger batch (>3 texts triggers concurrent processing)
	texts := make([]string, 10)
	for i := range texts {
		texts[i] = "test text " + itoa(i)
	}

	ctx := context.Background()
	embeddings, err := provider.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}

	if len(embeddings) != 10 {
		t.Errorf("embeddings count = %d, want 10", len(embeddings))
	}

	for i, emb := range embeddings {
		if len(emb) != 768 {
			t.Errorf("embedding[%d] dimensions = %d, want 768", i, len(emb))
		}
	}
}

// Test typed errors

func TestInvalidInputError(t *testing.T) {
	err := &InvalidInputError{Message: "test message"}
	expected := "invalid input: test message"
	if err.Error() != expected {
		t.Errorf("Error() = %v, want %v", err.Error(), expected)
	}
}

func TestEmptyResponseError(t *testing.T) {
	err := &EmptyResponseError{Message: "no data returned"}
	expected := "empty response: no data returned"
	if err.Error() != expected {
		t.Errorf("Error() = %v, want %v", err.Error(), expected)
	}
}

func TestDimensionsMismatchError(t *testing.T) {
	err := &DimensionsMismatchError{Expected: 768, Received: 512}
	expected := "dimensions mismatch: expected 768, got 512"
	if err.Error() != expected {
		t.Errorf("Error() = %v, want %v", err.Error(), expected)
	}
}

func TestProviderUnavailableError(t *testing.T) {
	tests := []struct {
		name     string
		err      *ProviderUnavailableError
		expected string
	}{
		{
			name:     "with_cause",
			err:      &ProviderUnavailableError{Provider: "ollama", Cause: errors.New("connection refused")},
			expected: "ollama provider unavailable: connection refused",
		},
		{
			name:     "without_cause",
			err:      &ProviderUnavailableError{Provider: "ollama"},
			expected: "ollama provider unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("Error() = %v, want %v", tt.err.Error(), tt.expected)
			}
			if tt.err.Cause != nil && tt.err.Unwrap() != tt.err.Cause {
				t.Errorf("Unwrap() = %v, want %v", tt.err.Unwrap(), tt.err.Cause)
			}
		})
	}
}

func TestModelNotFoundError(t *testing.T) {
	tests := []struct {
		name     string
		err      *ModelNotFoundError
		expected string
	}{
		{
			name:     "with_cause",
			err:      &ModelNotFoundError{Model: "test-model", Cause: errors.New("not found")},
			expected: "model not found: test-model: not found",
		},
		{
			name:     "without_cause",
			err:      &ModelNotFoundError{Model: "test-model"},
			expected: "model not found: test-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("Error() = %v, want %v", tt.err.Error(), tt.expected)
			}
			if tt.err.Cause != nil && tt.err.Unwrap() != tt.err.Cause {
				t.Errorf("Unwrap() = %v, want %v", tt.err.Unwrap(), tt.err.Cause)
			}
		})
	}
}

func TestTimeoutError(t *testing.T) {
	tests := []struct {
		name     string
		err      *TimeoutError
		expected string
	}{
		{
			name:     "with_cause",
			err:      &TimeoutError{Cause: errors.New("deadline exceeded")},
			expected: "request timeout: deadline exceeded",
		},
		{
			name:     "without_cause",
			err:      &TimeoutError{},
			expected: "request timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("Error() = %v, want %v", tt.err.Error(), tt.expected)
			}
			if tt.err.Cause != nil && tt.err.Unwrap() != tt.err.Cause {
				t.Errorf("Unwrap() = %v, want %v", tt.err.Unwrap(), tt.err.Cause)
			}
		})
	}
}

func TestContextCanceledError(t *testing.T) {
	tests := []struct {
		name     string
		err      *ContextCanceledError
		expected string
	}{
		{
			name:     "with_cause",
			err:      &ContextCanceledError{Cause: context.Canceled},
			expected: "context canceled: context canceled",
		},
		{
			name:     "without_cause",
			err:      &ContextCanceledError{},
			expected: "context canceled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Error() != tt.expected {
				t.Errorf("Error() = %v, want %v", tt.err.Error(), tt.expected)
			}
			if tt.err.Cause != nil && tt.err.Unwrap() != tt.err.Cause {
				t.Errorf("Unwrap() = %v, want %v", tt.err.Unwrap(), tt.err.Cause)
			}
		})
	}
}

func TestProviderError(t *testing.T) {
	err := &ProviderError{Provider: "ollama", Message: "something went wrong"}
	expected := "ollama error: something went wrong"
	if err.Error() != expected {
		t.Errorf("Error() = %v, want %v", err.Error(), expected)
	}
}

func TestItoa(t *testing.T) {
	tests := []struct {
		input    int
		expected string
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
		t.Run(tt.expected, func(t *testing.T) {
			result := itoa(tt.input)
			if result != tt.expected {
				t.Errorf("itoa(%d) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestOllamaProvider_KnownModels(t *testing.T) {
	// Test that all known models have proper specifications
	expectedModels := map[string]ollamaModelSpec{
		"nomic-embed-text":       {dimensions: 768, maxTokens: 8192},
		"mxbai-embed-large":      {dimensions: 1024, maxTokens: 8192},
		"all-minilm":             {dimensions: 384, maxTokens: 8192},
		"all-minilm-l6-v2":       {dimensions: 384, maxTokens: 8192},
		"all-minilm-l12-v2":      {dimensions: 384, maxTokens: 8192},
		"bge-small-en-v1.5":      {dimensions: 384, maxTokens: 8192},
		"bge-base-en-v1.5":       {dimensions: 768, maxTokens: 8192},
		"bge-large-en-v1.5":      {dimensions: 1024, maxTokens: 8192},
		"bge-m3":                 {dimensions: 1024, maxTokens: 8192},
		"snowflake-arctic-embed": {dimensions: 1024, maxTokens: 8192},
		"e5-small-v2":            {dimensions: 384, maxTokens: 8192},
		"e5-base-v2":             {dimensions: 768, maxTokens: 8192},
		"e5-large-v2":            {dimensions: 1024, maxTokens: 8192},
	}

	for model, expected := range expectedModels {
		t.Run(model, func(t *testing.T) {
			spec, ok := ollamaModels[model]
			if !ok {
				t.Errorf("model %s not found in ollamaModels", model)
				return
			}
			if spec.dimensions != expected.dimensions {
				t.Errorf("model %s dimensions = %d, want %d", model, spec.dimensions, expected.dimensions)
			}
			if spec.maxTokens != expected.maxTokens {
				t.Errorf("model %s maxTokens = %d, want %d", model, spec.maxTokens, expected.maxTokens)
			}
		})
	}
}
