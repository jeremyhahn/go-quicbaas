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
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewOpenAIProvider(t *testing.T) {
	tests := []struct {
		name       string
		config     Config
		wantErr    bool
		wantModel  string
		wantDims   int
		wantTokens int
	}{
		{
			name: "default_openai_config",
			config: Config{
				Provider: "openai",
				APIURL:   "https://api.openai.com",
				APIKey:   "sk-test-key",
			},
			wantErr:    false,
			wantModel:  ModelTextEmbedding3Small,
			wantDims:   1536,
			wantTokens: 8191,
		},
		{
			name: "openai_large_model",
			config: Config{
				Provider: "openai",
				APIURL:   "https://api.openai.com",
				APIKey:   "sk-test-key",
				Model:    ModelTextEmbedding3Large,
			},
			wantErr:    false,
			wantModel:  ModelTextEmbedding3Large,
			wantDims:   3072,
			wantTokens: 8191,
		},
		{
			name: "ollama_config",
			config: Config{
				Provider: "openai",
				APIURL:   "http://localhost:11434",
				Model:    "nomic-embed-text",
			},
			wantErr:    false,
			wantModel:  "nomic-embed-text",
			wantDims:   768,
			wantTokens: 8192,
		},
		{
			name: "lmstudio_config",
			config: Config{
				Provider: "openai",
				APIURL:   "http://localhost:1234",
				Model:    "nomic-ai/nomic-embed-text-v1.5-GGUF",
			},
			wantErr:    false,
			wantModel:  "nomic-ai/nomic-embed-text-v1.5-GGUF",
			wantDims:   768,
			wantTokens: 8192,
		},
		{
			name: "custom_dimensions_override",
			config: Config{
				Provider:   "openai",
				APIURL:     "http://localhost:11434",
				Model:      "custom-model",
				Dimensions: 512,
			},
			wantErr:    false,
			wantModel:  "custom-model",
			wantDims:   512,
			wantTokens: 8192,
		},
		{
			name: "auto_detect_dimensions",
			config: Config{
				Provider: "openai",
				APIURL:   "http://localhost:11434",
				Model:    "unknown-model",
			},
			wantErr:    false,
			wantModel:  "unknown-model",
			wantDims:   0, // Will auto-detect on first request
			wantTokens: 8192,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewOpenAIProvider(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewOpenAIProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			defer provider.Close()

			if provider.model != tt.wantModel {
				t.Errorf("model = %v, want %v", provider.model, tt.wantModel)
			}
			if provider.Dimensions() != tt.wantDims {
				t.Errorf("dimensions = %v, want %v", provider.Dimensions(), tt.wantDims)
			}
			if provider.MaxTokens() != tt.wantTokens {
				t.Errorf("maxTokens = %v, want %v", provider.MaxTokens(), tt.wantTokens)
			}
			if provider.Name() != "openai" {
				t.Errorf("name = %v, want openai", provider.Name())
			}
		})
	}
}

func TestOpenAIProvider_Embed(t *testing.T) {
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
			text: "test text",
			serverResponse: openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{
						Object:    "embedding",
						Embedding: make([]float32, 1536),
						Index:     0,
					},
				},
				Model: ModelTextEmbedding3Small,
				Usage: openaiUsage{
					PromptTokens: 2,
					TotalTokens:  2,
				},
			},
			statusCode: http.StatusOK,
			wantErr:    false,
			wantDims:   1536,
		},
		{
			name:       "empty_text",
			text:       "",
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name: "unauthorized",
			text: "test text",
			serverResponse: openaiResponse{
				Object: "error",
			},
			statusCode: http.StatusUnauthorized,
			wantErr:    true,
		},
		{
			name: "model_not_found",
			text: "test text",
			serverResponse: openaiResponse{
				Object: "error",
			},
			statusCode: http.StatusNotFound,
			wantErr:    true,
		},
		{
			name: "rate_limit_exceeded",
			text: "test text",
			serverResponse: openaiResponse{
				Object: "error",
			},
			statusCode: http.StatusTooManyRequests,
			wantErr:    true,
		},
		{
			name: "server_error",
			text: "test text",
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
				// Verify request
				if r.Method != http.MethodPost {
					t.Errorf("expected POST request, got %s", r.Method)
				}
				if !strings.HasSuffix(r.URL.Path, "/v1/embeddings") {
					t.Errorf("expected /v1/embeddings endpoint, got %s", r.URL.Path)
				}

				// Parse request
				var req openaiRequest
				if err := json.NewDecoder(r.Body).Decode(&req); err != nil && tt.statusCode == http.StatusOK {
					t.Errorf("failed to decode request: %v", err)
				}

				// Send response
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					json.NewEncoder(w).Encode(tt.serverResponse)
				} else {
					errResp := openaiError{}
					errResp.Error.Message = "test error"
					json.NewEncoder(w).Encode(errResp)
				}
			}))
			defer server.Close()

			// Create provider
			config := Config{
				Provider:   "openai",
				APIURL:     server.URL,
				APIKey:     "test-key",
				Model:      ModelTextEmbedding3Small,
				Dimensions: 1536,
				Timeout:    5,
			}
			provider, err := NewOpenAIProvider(config)
			if err != nil {
				t.Fatalf("NewOpenAIProvider() error = %v", err)
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

func TestOpenAIProvider_EmbedBatch(t *testing.T) {
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
					{
						Object:    "embedding",
						Embedding: make([]float32, 768),
						Index:     0,
					},
					{
						Object:    "embedding",
						Embedding: make([]float32, 768),
						Index:     1,
					},
					{
						Object:    "embedding",
						Embedding: make([]float32, 768),
						Index:     2,
					},
				},
				Model: "nomic-embed-text",
				Usage: openaiUsage{
					PromptTokens: 6,
					TotalTokens:  6,
				},
			},
			statusCode: http.StatusOK,
			wantErr:    false,
			wantCount:  3,
		},
		{
			name:       "empty_texts",
			texts:      []string{},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:       "text_with_empty_element",
			texts:      []string{"text1", "", "text3"},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:  "missing_embedding_in_response",
			texts: []string{"text1", "text2"},
			serverResponse: openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{
						Object:    "embedding",
						Embedding: make([]float32, 768),
						Index:     0,
					},
					// Missing index 1
				},
				Model: "nomic-embed-text",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
		{
			name:  "invalid_index_in_response",
			texts: []string{"text1", "text2"},
			serverResponse: openaiResponse{
				Object: "list",
				Data: []openaiEmbedding{
					{
						Object:    "embedding",
						Embedding: make([]float32, 768),
						Index:     5, // Invalid index
					},
				},
				Model: "nomic-embed-text",
			},
			statusCode: http.StatusOK,
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test server
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.statusCode)
				if tt.statusCode == http.StatusOK {
					json.NewEncoder(w).Encode(tt.serverResponse)
				} else {
					errResp := openaiError{}
					errResp.Error.Message = "test error"
					json.NewEncoder(w).Encode(errResp)
				}
			}))
			defer server.Close()

			// Create provider
			config := Config{
				Provider: "openai",
				APIURL:   server.URL,
				APIKey:   "test-key",
				Model:    "nomic-embed-text",
				Timeout:  5,
			}
			provider, err := NewOpenAIProvider(config)
			if err != nil {
				t.Fatalf("NewOpenAIProvider() error = %v", err)
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

func TestOpenAIProvider_AutoDetectDimensions(t *testing.T) {
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	// Create provider with no dimensions specified
	config := Config{
		Provider:   "openai",
		APIURL:     server.URL,
		Model:      "custom-model",
		Dimensions: 0, // Auto-detect
		Timeout:    5,
	}
	provider, err := NewOpenAIProvider(config)
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
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

func TestOpenAIProvider_DimensionsMismatch(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		dims := 512
		if callCount > 1 {
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider: "openai",
		APIURL:   server.URL,
		Model:    "custom-model",
		Timeout:  5,
	}
	provider, err := NewOpenAIProvider(config)
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
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

func TestOpenAIProvider_RetryLogic(t *testing.T) {
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
		{
			name:          "no_retry_on_client_error",
			statusCodes:   []int{http.StatusBadRequest},
			expectRetries: 0,
			wantErr:       true,
		},
		{
			name:          "retry_on_rate_limit",
			statusCodes:   []int{http.StatusTooManyRequests, http.StatusOK},
			expectRetries: 1,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			callCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				statusCode := tt.statusCodes[callCount]
				if callCount < len(tt.statusCodes)-1 {
					callCount++
				}

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
					json.NewEncoder(w).Encode(resp)
				} else {
					errResp := openaiError{}
					errResp.Error.Message = "test error"
					json.NewEncoder(w).Encode(errResp)
				}
			}))
			defer server.Close()

			config := Config{
				Provider:       "openai",
				APIURL:         server.URL,
				Model:          "test-model",
				Timeout:        5,
				MaxRetries:     3,
				RetryBackoffMS: 10, // Short backoff for testing
			}
			provider, err := NewOpenAIProvider(config)
			if err != nil {
				t.Fatalf("NewOpenAIProvider() error = %v", err)
			}
			defer provider.Close()

			ctx := context.Background()
			_, err = provider.Embed(ctx, "test text")

			if (err != nil) != tt.wantErr {
				t.Errorf("Embed() error = %v, wantErr %v", err, tt.wantErr)
			}

			if callCount != tt.expectRetries {
				t.Errorf("retry count = %d, want %d", callCount, tt.expectRetries)
			}
		})
	}
}

func TestOpenAIProvider_ContextCancellation(t *testing.T) {
	// Create a server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider: "openai",
		APIURL:   server.URL,
		Model:    "test-model",
		Timeout:  5,
	}
	provider, err := NewOpenAIProvider(config)
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	defer provider.Close()

	// Create context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err = provider.Embed(ctx, "test text")
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
}

func TestOpenAIProvider_NoAuthorizationHeader(t *testing.T) {
	// Test that no Authorization header is sent when APIKey is empty
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider: "openai",
		APIURL:   server.URL,
		APIKey:   "", // No API key
		Model:    "test-model",
		Timeout:  5,
	}
	provider, err := NewOpenAIProvider(config)
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	_, err = provider.Embed(ctx, "test text")
	if err != nil {
		t.Errorf("Embed() error = %v", err)
	}
}

func TestOpenAIProvider_AuthorizationHeader(t *testing.T) {
	// Test that Authorization header is sent when APIKey is provided
	expectedKey := "sk-test-key-12345"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		expectedHeader := fmt.Sprintf("Bearer %s", expectedKey)
		if authHeader != expectedHeader {
			t.Errorf("expected Authorization: %s, got %s", expectedHeader, authHeader)
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider: "openai",
		APIURL:   server.URL,
		APIKey:   expectedKey,
		Model:    "test-model",
		Timeout:  5,
	}
	provider, err := NewOpenAIProvider(config)
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	_, err = provider.Embed(ctx, "test text")
	if err != nil {
		t.Errorf("Embed() error = %v", err)
	}
}

func TestOpenAIProvider_Close(t *testing.T) {
	config := Config{
		Provider: "openai",
		APIURL:   "https://api.openai.com",
		APIKey:   "test-key",
		Model:    ModelTextEmbedding3Small,
		Timeout:  5,
	}
	provider, err := NewOpenAIProvider(config)
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
	}

	err = provider.Close()
	if err != nil {
		t.Errorf("Close() error = %v", err)
	}
}

// TestOpenAIProvider_EmptyEmbeddingInResponse tests handling of empty embeddings
func TestOpenAIProvider_EmptyEmbeddingInResponse(t *testing.T) {
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
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider: "openai",
		APIURL:   server.URL,
		Model:    "test-model",
		Timeout:  5,
	}
	provider, err := NewOpenAIProvider(config)
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
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

// TestOpenAIProvider_NoEmbeddingsInResponse tests handling of no embeddings in response
func TestOpenAIProvider_NoEmbeddingsInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := openaiResponse{
			Object: "list",
			Data:   []openaiEmbedding{}, // No embeddings
			Model:  "test-model",
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		Provider: "openai",
		APIURL:   server.URL,
		Model:    "test-model",
		Timeout:  5,
	}
	provider, err := NewOpenAIProvider(config)
	if err != nil {
		t.Fatalf("NewOpenAIProvider() error = %v", err)
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
