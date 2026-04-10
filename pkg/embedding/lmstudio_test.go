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

func TestNewLMStudioProvider(t *testing.T) {
	tests := []struct {
		name       string
		config     LMStudioConfig
		wantModel  string
		wantDims   int
		wantTokens int
	}{
		{
			name:       "default_config",
			config:     LMStudioConfig{},
			wantModel:  "nomic-ai/nomic-embed-text-v1.5-GGUF",
			wantDims:   768,
			wantTokens: 8192,
		},
		{
			name: "custom_nomic_model",
			config: LMStudioConfig{
				Model: "nomic-ai/nomic-embed-text-v1.5-GGUF",
			},
			wantModel:  "nomic-ai/nomic-embed-text-v1.5-GGUF",
			wantDims:   768,
			wantTokens: 8192,
		},
		{
			name: "bge_small_model",
			config: LMStudioConfig{
				Model: "BAAI/bge-small-en-v1.5-GGUF",
			},
			wantModel:  "BAAI/bge-small-en-v1.5-GGUF",
			wantDims:   384,
			wantTokens: 8192,
		},
		{
			name: "bge_large_model",
			config: LMStudioConfig{
				Model: "BAAI/bge-large-en-v1.5-GGUF",
			},
			wantModel:  "BAAI/bge-large-en-v1.5-GGUF",
			wantDims:   1024,
			wantTokens: 8192,
		},
		{
			name: "mxbai_model",
			config: LMStudioConfig{
				Model: "mixedbread-ai/mxbai-embed-large-v1-GGUF",
			},
			wantModel:  "mixedbread-ai/mxbai-embed-large-v1-GGUF",
			wantDims:   1024,
			wantTokens: 8192,
		},
		{
			name: "minilm_model",
			config: LMStudioConfig{
				Model: "sentence-transformers/all-MiniLM-L6-v2-GGUF",
			},
			wantModel:  "sentence-transformers/all-MiniLM-L6-v2-GGUF",
			wantDims:   384,
			wantTokens: 8192,
		},
		{
			name: "custom_dimensions_override",
			config: LMStudioConfig{
				Model:      "nomic-ai/nomic-embed-text-v1.5-GGUF",
				Dimensions: 512,
			},
			wantModel:  "nomic-ai/nomic-embed-text-v1.5-GGUF",
			wantDims:   512,
			wantTokens: 8192,
		},
		{
			name: "unknown_model_auto_detect",
			config: LMStudioConfig{
				Model: "custom-unknown-model",
			},
			wantModel:  "custom-unknown-model",
			wantDims:   0, // Will auto-detect on first request
			wantTokens: 8192,
		},
		{
			name: "custom_base_url",
			config: LMStudioConfig{
				BaseURL: "http://custom-host:1234",
				Model:   "nomic-ai/nomic-embed-text-v1.5-GGUF",
			},
			wantModel:  "nomic-ai/nomic-embed-text-v1.5-GGUF",
			wantDims:   768,
			wantTokens: 8192,
		},
		{
			name: "custom_timeout_and_retries",
			config: LMStudioConfig{
				Model:          "nomic-ai/nomic-embed-text-v1.5-GGUF",
				Timeout:        120,
				MaxRetries:     5,
				RetryBackoffMS: 2000,
			},
			wantModel:  "nomic-ai/nomic-embed-text-v1.5-GGUF",
			wantDims:   768,
			wantTokens: 8192,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewLMStudioProvider(tt.config)
			if err != nil {
				t.Fatalf("NewLMStudioProvider() error = %v", err)
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
			if provider.Name() != "lmstudio" {
				t.Errorf("Name() = %v, want lmstudio", provider.Name())
			}
		})
	}
}

func TestLMStudioProvider_DefaultConfig(t *testing.T) {
	config := DefaultLMStudioConfig()

	if config.BaseURL != DefaultLMStudioURL {
		t.Errorf("BaseURL = %v, want %v", config.BaseURL, DefaultLMStudioURL)
	}
	if config.Model != "nomic-ai/nomic-embed-text-v1.5-GGUF" {
		t.Errorf("Model = %v, want nomic-ai/nomic-embed-text-v1.5-GGUF", config.Model)
	}
	if config.Dimensions != 0 {
		t.Errorf("Dimensions = %v, want 0 (auto-detect)", config.Dimensions)
	}
	if config.Timeout != int(DefaultLMStudioTimeout.Seconds()) {
		t.Errorf("Timeout = %v, want %v", config.Timeout, int(DefaultLMStudioTimeout.Seconds()))
	}
	if config.MaxRetries != DefaultLMStudioMaxRetries {
		t.Errorf("MaxRetries = %v, want %v", config.MaxRetries, DefaultLMStudioMaxRetries)
	}
	if config.RetryBackoffMS != DefaultLMStudioRetryBackoffMS {
		t.Errorf("RetryBackoffMS = %v, want %v", config.RetryBackoffMS, DefaultLMStudioRetryBackoffMS)
	}
}

func TestLMStudioProvider_Embed(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		serverResponse openaiResponse
		statusCode     int
		wantErr        bool
		wantDims       int
	}{
		{
			name: "successful_embedding",
			text: "test text for embedding",
			serverResponse: openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{
						Object:    "embedding",
						Embedding: make([]float32, 768),
						Index:     0,
					},
				},
				Model: "nomic-ai/nomic-embed-text-v1.5-GGUF",
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
			name:       "server_error",
			text:       "test text",
			statusCode: http.StatusInternalServerError,
			wantErr:    true,
		},
		{
			name:       "model_not_found",
			text:       "test text",
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server (OpenAI-compatible API)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Verify request format
				if r.Method != http.MethodPost {
					t.Errorf("expected POST request, got %s", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/v1/embeddings") {
					t.Errorf("expected /v1/embeddings endpoint, got %s", r.URL.Path)
				}

				// Verify NO Authorization header (LMStudio doesn't need it)
				authHeader := r.Header.Get("Authorization")
				if authHeader != "" {
					t.Errorf("expected no Authorization header, got %s", authHeader)
				}

				// Parse request
				var req openaiRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil && tt.statusCode == http.StatusOK {
					t.Errorf("failed to decode request: %v", err)
				}

				// Send response
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					if err := json.NewEncoder(w).Encode(tt.serverResponse); err != nil {
						t.Errorf("failed to encode response: %v", err)
					}
				} else {
					errResp := openaiError{}
					errResp.Error.Message = "test error"
					if err := json.NewEncoder(w).Encode(errResp); err != nil {
						t.Errorf("failed to encode error response: %v", err)
					}
				}
			}))
			defer server.Close()

			// Create provider
			config := LMStudioConfig{
				BaseURL:        server.URL,
				Model:          "nomic-ai/nomic-embed-text-v1.5-GGUF",
				Timeout:        5,
				MaxRetries:     0,
				RetryBackoffMS: 10,
			}
			provider, err := NewLMStudioProvider(config)
			if err != nil {
				t.Fatalf("NewLMStudioProvider() error = %v", err)
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

func TestLMStudioProvider_EmbedBatch(t *testing.T) {
	tests := []struct {
		name           string
		texts          []string
		serverResponse openaiResponse
		statusCode     int
		wantErr        bool
		wantCount      int
	}{
		{
			name:  "successful_batch",
			texts: []string{"text1", "text2", "text3"},
			serverResponse: openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{Object: "embedding", Embedding: make([]float32, 768), Index: 0},
					{Object: "embedding", Embedding: make([]float32, 768), Index: 1},
					{Object: "embedding", Embedding: make([]float32, 768), Index: 2},
				},
				Model: "nomic-ai/nomic-embed-text-v1.5-GGUF",
			},
			statusCode: http.StatusOK,
			wantErr:    false,
			wantCount:  3,
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
			name:  "server_error",
			texts: []string{"text1", "text2"},
			serverResponse: openaiResponse{
				Object: "error",
			},
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
					if err := json.NewEncoder(w).Encode(tt.serverResponse); err != nil {
						t.Errorf("failed to encode response: %v", err)
					}
				} else {
					errResp := openaiError{}
					errResp.Error.Message = "test error"
					if err := json.NewEncoder(w).Encode(errResp); err != nil {
						t.Errorf("failed to encode error response: %v", err)
					}
				}
			}))
			defer server.Close()

			// Create provider
			config := LMStudioConfig{
				BaseURL:        server.URL,
				Model:          "nomic-ai/nomic-embed-text-v1.5-GGUF",
				Timeout:        5,
				MaxRetries:     0,
				RetryBackoffMS: 10,
			}
			provider, err := NewLMStudioProvider(config)
			if err != nil {
				t.Fatalf("NewLMStudioProvider() error = %v", err)
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

func TestLMStudioProvider_AutoDetectDimensions(t *testing.T) {
	// Create test server that returns 512-dimensional embeddings
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			Object: "list",
			Data: []openaiEmbedding{
				{
					Object:    "embedding",
					Embedding: make([]float32, 512),
					Index:     0,
				},
			},
			Model: "custom-model",
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	// Create provider with unknown model (no pre-configured dimensions)
	config := LMStudioConfig{
		BaseURL:        server.URL,
		Model:          "custom-unknown-model",
		Timeout:        5,
		MaxRetries:     0,
		RetryBackoffMS: 10,
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
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
}

func TestLMStudioProvider_DimensionsMismatch(t *testing.T) {
	var callCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&callCount, 1)
		dims := 512
		if count > 1 {
			dims = 768 // Return different dimensions on second call
		}
		resp := openaiResponse{
			Object: "list",
			Data: []openaiEmbedding{
				{
					Object:    "embedding",
					Embedding: make([]float32, dims),
					Index:     0,
				},
			},
			Model: "custom-model",
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	config := LMStudioConfig{
		BaseURL:        server.URL,
		Model:          "custom-model",
		Timeout:        5,
		MaxRetries:     0,
		RetryBackoffMS: 10,
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
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
	if !strings.Contains(err.Error(), "dimensions mismatch") {
		t.Errorf("expected 'dimensions mismatch' error, got: %v", err)
	}
}

func TestLMStudioProvider_RetryLogic(t *testing.T) {
	tests := []struct {
		name          string
		statusCodes   []int
		expectRetries int
		wantErr       bool
	}{
		{
			name:          "success_on_first_try",
			statusCodes:   []int{http.StatusOK},
			expectRetries: 0,
			wantErr:       false,
		},
		{
			name:          "success_after_retry",
			statusCodes:   []int{http.StatusInternalServerError, http.StatusOK},
			expectRetries: 1,
			wantErr:       false,
		},
		{
			name:          "fail_after_max_retries",
			statusCodes:   []int{http.StatusInternalServerError, http.StatusInternalServerError, http.StatusInternalServerError, http.StatusInternalServerError},
			expectRetries: 3,
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var callCount int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count := atomic.AddInt32(&callCount, 1)
				idx := int(count) - 1
				if idx >= len(tt.statusCodes) {
					idx = len(tt.statusCodes) - 1
				}
				statusCode := tt.statusCodes[idx]

				w.WriteHeader(statusCode)
				if statusCode == http.StatusOK {
					resp := openaiResponse{
						Object: "list",
						Data: []openaiEmbedding{
							{
								Object:    "embedding",
								Embedding: make([]float32, 768),
								Index:     0,
							},
						},
						Model: "test-model",
					}
					if err := json.NewEncoder(w).Encode(resp); err != nil {
						t.Errorf("failed to encode response: %v", err)
					}
				} else {
					errResp := openaiError{}
					errResp.Error.Message = "test error"
					if err := json.NewEncoder(w).Encode(errResp); err != nil {
						t.Errorf("failed to encode error response: %v", err)
					}
				}
			}))
			defer server.Close()

			config := LMStudioConfig{
				BaseURL:        server.URL,
				Model:          "test-model",
				Timeout:        5,
				MaxRetries:     3,
				RetryBackoffMS: 10, // Short backoff for testing
			}
			provider, err := NewLMStudioProvider(config)
			if err != nil {
				t.Fatalf("NewLMStudioProvider() error = %v", err)
			}
			defer provider.Close()

			ctx := context.Background()
			_, err = provider.Embed(ctx, "test text")

			if (err != nil) != tt.wantErr {
				t.Errorf("Embed() error = %v, wantErr %v", err, tt.wantErr)
			}

			actualRetries := int(atomic.LoadInt32(&callCount)) - 1
			if actualRetries != tt.expectRetries {
				t.Errorf("retry count = %d, want %d", actualRetries, tt.expectRetries)
			}
		})
	}
}

func TestLMStudioProvider_ContextCancellation(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		resp := openaiResponse{
			Object: "list",
			Data: []openaiEmbedding{
				{
					Object:    "embedding",
					Embedding: make([]float32, 768),
					Index:     0,
				},
			},
			Model: "test-model",
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			// Expected when context is canceled
			return
		}
	}))
	defer server.Close()

	config := LMStudioConfig{
		BaseURL:        server.URL,
		Model:          "test-model",
		Timeout:        30,
		MaxRetries:     0,
		RetryBackoffMS: 10,
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
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

func TestLMStudioProvider_Close(t *testing.T) {
	config := LMStudioConfig{
		BaseURL: "http://localhost:1234",
		Model:   "nomic-ai/nomic-embed-text-v1.5-GGUF",
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}

	err = provider.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

func TestLMStudioProvider_BaseURL(t *testing.T) {
	config := LMStudioConfig{
		BaseURL: "http://custom-host:5678",
		Model:   "nomic-ai/nomic-embed-text-v1.5-GGUF",
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}
	defer provider.Close()

	if provider.BaseURL() != "http://custom-host:5678" {
		t.Errorf("BaseURL() = %v, want http://custom-host:5678", provider.BaseURL())
	}
}

func TestLMStudioProvider_NoAuthorizationHeader(t *testing.T) {
	// Test that no Authorization header is sent (LMStudio doesn't need it)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader != "" {
			t.Errorf("expected no Authorization header, got %s", authHeader)
		}

		resp := openaiResponse{
			Object: "list",
			Data: []openaiEmbedding{
				{
					Object:    "embedding",
					Embedding: make([]float32, 768),
					Index:     0,
				},
			},
			Model: "test-model",
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	config := LMStudioConfig{
		BaseURL:        server.URL,
		Model:          "nomic-ai/nomic-embed-text-v1.5-GGUF",
		Timeout:        5,
		MaxRetries:     0,
		RetryBackoffMS: 10,
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	_, err = provider.Embed(ctx, "test text")
	if err != nil {
		t.Errorf("Embed() error = %v", err)
	}
}

func TestLMStudioProvider_DelegationToOpenAI(t *testing.T) {
	// Test that LMStudioProvider properly delegates to OpenAIProvider
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it uses OpenAI-compatible endpoint
		if !strings.HasSuffix(r.URL.Path, "/v1/embeddings") {
			t.Errorf("expected OpenAI-compatible endpoint, got %s", r.URL.Path)
		}

		// Verify request body follows OpenAI format
		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
		}

		if len(req.Input) == 0 {
			t.Error("expected non-empty input array")
		}

		resp := openaiResponse{
			Object: "list",
			Data: []openaiEmbedding{
				{
					Object:    "embedding",
					Embedding: make([]float32, 768),
					Index:     0,
				},
			},
			Model: req.Model,
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	config := LMStudioConfig{
		BaseURL:        server.URL,
		Model:          "test-model",
		Timeout:        5,
		MaxRetries:     0,
		RetryBackoffMS: 10,
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	embedding, err := provider.Embed(ctx, "test text")
	if err != nil {
		t.Errorf("Embed() error = %v", err)
	}

	if len(embedding) != 768 {
		t.Errorf("embedding dimensions = %d, want 768", len(embedding))
	}
}

// Test typed errors

func TestLMStudioInitError(t *testing.T) {
	tests := []struct {
		name     string
		err      *LMStudioInitError
		expected string
	}{
		{
			name:     "with_cause",
			err:      &LMStudioInitError{Cause: errors.New("connection failed")},
			expected: "failed to initialize LMStudio provider: connection failed",
		},
		{
			name:     "without_cause",
			err:      &LMStudioInitError{},
			expected: "failed to initialize LMStudio provider",
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

func TestLMStudioError(t *testing.T) {
	tests := []struct {
		name     string
		err      *LMStudioError
		expected string
	}{
		{
			name:     "with_cause",
			err:      &LMStudioError{Message: "something went wrong", Cause: errors.New("underlying error")},
			expected: "lmstudio: something went wrong",
		},
		{
			name:     "without_cause",
			err:      &LMStudioError{Message: "generic error"},
			expected: "lmstudio: generic error",
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

func TestLMStudioProvider_KnownModels(t *testing.T) {
	// Test that all known models have proper specifications
	expectedModels := map[string]lmstudioModelSpec{
		"nomic-ai/nomic-embed-text-v1.5-GGUF":          {dimensions: 768, maxTokens: 8192},
		"nomic-embed-text-v1.5-GGUF":                   {dimensions: 768, maxTokens: 8192},
		"BAAI/bge-small-en-v1.5-GGUF":                  {dimensions: 384, maxTokens: 8192},
		"BAAI/bge-base-en-v1.5-GGUF":                   {dimensions: 768, maxTokens: 8192},
		"BAAI/bge-large-en-v1.5-GGUF":                  {dimensions: 1024, maxTokens: 8192},
		"sentence-transformers/all-MiniLM-L6-v2-GGUF":  {dimensions: 384, maxTokens: 8192},
		"sentence-transformers/all-MiniLM-L12-v2-GGUF": {dimensions: 384, maxTokens: 8192},
		"Xenova/all-MiniLM-L6-v2-onnx":                 {dimensions: 384, maxTokens: 8192},
		"mixedbread-ai/mxbai-embed-large-v1-GGUF":      {dimensions: 1024, maxTokens: 8192},
		"intfloat/e5-small-v2-GGUF":                    {dimensions: 384, maxTokens: 8192},
		"intfloat/e5-base-v2-GGUF":                     {dimensions: 768, maxTokens: 8192},
		"intfloat/e5-large-v2-GGUF":                    {dimensions: 1024, maxTokens: 8192},
		"Snowflake/snowflake-arctic-embed-m-v1.5-GGUF": {dimensions: 768, maxTokens: 8192},
		"Snowflake/snowflake-arctic-embed-l-v2.0-GGUF": {dimensions: 1024, maxTokens: 8192},
	}

	for model, expected := range expectedModels {
		t.Run(model, func(t *testing.T) {
			spec, ok := lmstudioModels[model]
			if !ok {
				t.Errorf("model %s not found in lmstudioModels", model)
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

func TestLMStudioProvider_EmptyEmbeddingInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			Object: "list",
			Data: []openaiEmbedding{
				{
					Object:    "embedding",
					Embedding: []float32{}, // Empty embedding
					Index:     0,
				},
			},
			Model: "test-model",
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	config := LMStudioConfig{
		BaseURL:        server.URL,
		Model:          "test-model",
		Timeout:        5,
		MaxRetries:     0,
		RetryBackoffMS: 10,
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	_, err = provider.Embed(ctx, "test text")
	if err == nil {
		t.Error("expected error for empty embedding, got nil")
	}
	if !strings.Contains(err.Error(), "empty embedding") {
		t.Errorf("expected 'empty embedding' error, got: %v", err)
	}
}

func TestLMStudioProvider_NoEmbeddingsInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			Object: "list",
			Data:   []openaiEmbedding{}, // No embeddings
			Model:  "test-model",
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))
	defer server.Close()

	config := LMStudioConfig{
		BaseURL:        server.URL,
		Model:          "test-model",
		Timeout:        5,
		MaxRetries:     0,
		RetryBackoffMS: 10,
	}
	provider, err := NewLMStudioProvider(config)
	if err != nil {
		t.Fatalf("NewLMStudioProvider() error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	_, err = provider.Embed(ctx, "test text")
	if err == nil {
		t.Error("expected error for no embeddings, got nil")
	}
	if !strings.Contains(err.Error(), "no embeddings") {
		t.Errorf("expected 'no embeddings' error, got: %v", err)
	}
}
