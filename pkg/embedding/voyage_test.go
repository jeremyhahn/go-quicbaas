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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestNewVoyageProvider tests provider initialization
func TestNewVoyageProvider(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
		errType error
	}{
		{
			name: "valid config with voyage-code-2",
			config: Config{
				APIKey:         "test-api-key",
				Model:          "voyage-code-2",
				Timeout:        30,
				MaxRetries:     3,
				RetryBackoffMS: 1000,
			},
			wantErr: false,
		},
		{
			name: "valid config with voyage-2",
			config: Config{
				APIKey: "test-api-key",
				Model:  "voyage-2",
			},
			wantErr: false,
		},
		{
			name: "valid config with voyage-large-2",
			config: Config{
				APIKey: "test-api-key",
				Model:  "voyage-large-2",
			},
			wantErr: false,
		},
		{
			name: "default model when not specified",
			config: Config{
				APIKey: "test-api-key",
			},
			wantErr: false,
		},
		{
			name: "missing API key",
			config: Config{
				Model: "voyage-code-2",
			},
			wantErr: true,
			errType: ErrInvalidCredentials,
		},
		{
			name: "invalid model",
			config: Config{
				APIKey: "test-api-key",
				Model:  "invalid-model",
			},
			wantErr: true,
			errType: ErrModelNotFound,
		},
		{
			name: "custom dimensions override",
			config: Config{
				APIKey:     "test-api-key",
				Model:      "voyage-code-2",
				Dimensions: 768,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewVoyageProvider(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Errorf("NewVoyageProvider() expected error, got nil")
					return
				}
				if tt.errType != nil && !strings.Contains(err.Error(), tt.errType.Error()) {
					t.Errorf("NewVoyageProvider() error = %v, want error type %v", err, tt.errType)
				}
				return
			}

			if err != nil {
				t.Errorf("NewVoyageProvider() unexpected error = %v", err)
				return
			}

			if provider == nil {
				t.Error("NewVoyageProvider() returned nil provider")
				return
			}

			// Verify provider properties
			if provider.Name() != "voyage" {
				t.Errorf("provider.Name() = %v, want 'voyage'", provider.Name())
			}

			// Verify model-specific properties
			if tt.config.Model == "" {
				if provider.model != "voyage-code-2" {
					t.Errorf("default model = %v, want 'voyage-code-2'", provider.model)
				}
			}

			// Verify custom dimensions
			if tt.config.Dimensions > 0 {
				if provider.Dimensions() != tt.config.Dimensions {
					t.Errorf("provider.Dimensions() = %v, want %v", provider.Dimensions(), tt.config.Dimensions)
				}
			}

			// Clean up
			provider.Close()
		})
	}
}

// TestVoyageProviderModelSpecs tests model specifications
func TestVoyageProviderModelSpecs(t *testing.T) {
	tests := []struct {
		name           string
		model          string
		wantDimensions int
		wantMaxTokens  int
	}{
		{
			name:           "voyage-code-2",
			model:          "voyage-code-2",
			wantDimensions: 1536,
			wantMaxTokens:  32000,
		},
		{
			name:           "voyage-2",
			model:          "voyage-2",
			wantDimensions: 1024,
			wantMaxTokens:  16000,
		},
		{
			name:           "voyage-large-2",
			model:          "voyage-large-2",
			wantDimensions: 1536,
			wantMaxTokens:  16000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := Config{
				APIKey: "test-api-key",
				Model:  tt.model,
			}

			provider, err := NewVoyageProvider(config)
			if err != nil {
				t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
			}
			defer provider.Close()

			if provider.Dimensions() != tt.wantDimensions {
				t.Errorf("provider.Dimensions() = %v, want %v", provider.Dimensions(), tt.wantDimensions)
			}

			if provider.MaxTokens() != tt.wantMaxTokens {
				t.Errorf("provider.MaxTokens() = %v, want %v", provider.MaxTokens(), tt.wantMaxTokens)
			}
		})
	}
}

// TestVoyageProviderEmbed tests single text embedding
func TestVoyageProviderEmbed(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}

		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			t.Errorf("invalid Authorization header: %s", r.Header.Get("Authorization"))
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("invalid Content-Type header: %s", r.Header.Get("Content-Type"))
		}

		// Parse request body
		var req voyageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Send response
		resp := voyageResponse{
			Object: "list",
			Model:  "voyage-code-2",
			Data: []voyageEmbedding{
				{
					Object:    "embedding",
					Embedding: make([]float32, 1536),
					Index:     0,
				},
			},
			Usage: voyageUsage{
				TotalTokens: 10,
			},
		}

		// Fill with test data
		for i := range resp.Data[0].Embedding {
			resp.Data[0].Embedding[i] = float32(i) * 0.001
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		APIKey: "test-api-key",
		APIURL: server.URL,
		Model:  "voyage-code-2",
	}

	provider, err := NewVoyageProvider(config)
	if err != nil {
		t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	embedding, err := provider.Embed(ctx, "test text")
	if err != nil {
		t.Fatalf("Embed() unexpected error = %v", err)
	}

	if len(embedding) != 1536 {
		t.Errorf("embedding length = %v, want 1536", len(embedding))
	}

	// Verify embedding values
	if embedding[0] != 0.0 {
		t.Errorf("embedding[0] = %v, want 0.0", embedding[0])
	}
	if embedding[1] != 0.001 {
		t.Errorf("embedding[1] = %v, want 0.001", embedding[1])
	}
}

// TestVoyageProviderEmbedErrors tests error handling for single embedding
func TestVoyageProviderEmbedErrors(t *testing.T) {
	config := Config{
		APIKey: "test-api-key",
		APIURL: "http://localhost:9999", // Non-existent server
		Model:  "voyage-code-2",
	}

	provider, err := NewVoyageProvider(config)
	if err != nil {
		t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	// Test empty text
	_, err = provider.Embed(ctx, "")
	if err == nil {
		t.Error("Embed() expected error for empty text, got nil")
	}
	if !strings.Contains(err.Error(), ErrInvalidInput.Error()) {
		t.Errorf("Embed() error = %v, want ErrInvalidInput", err)
	}
}

// TestVoyageProviderEmbedBatch tests batch embedding
func TestVoyageProviderEmbedBatch(t *testing.T) {
	// Mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req voyageRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		// Create response with embeddings for each input
		resp := voyageResponse{
			Object: "list",
			Model:  "voyage-code-2",
			Data:   make([]voyageEmbedding, len(req.Input)),
			Usage: voyageUsage{
				TotalTokens: len(req.Input) * 10,
			},
		}

		for i := range req.Input {
			resp.Data[i] = voyageEmbedding{
				Object:    "embedding",
				Embedding: make([]float32, 1536),
				Index:     i,
			}
			// Fill with unique test data per embedding
			for j := range resp.Data[i].Embedding {
				resp.Data[i].Embedding[j] = float32(i*1000+j) * 0.001
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		APIKey: "test-api-key",
		APIURL: server.URL,
		Model:  "voyage-code-2",
	}

	provider, err := NewVoyageProvider(config)
	if err != nil {
		t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	texts := []string{"text1", "text2", "text3"}
	embeddings, err := provider.EmbedBatch(ctx, texts)
	if err != nil {
		t.Fatalf("EmbedBatch() unexpected error = %v", err)
	}

	if len(embeddings) != 3 {
		t.Errorf("embeddings count = %v, want 3", len(embeddings))
	}

	for i, emb := range embeddings {
		if len(emb) != 1536 {
			t.Errorf("embedding[%d] length = %v, want 1536", i, len(emb))
		}
		// Verify unique values per embedding
		expectedFirst := float32(i*1000) * 0.001
		if emb[0] != expectedFirst {
			t.Errorf("embedding[%d][0] = %v, want %v", i, emb[0], expectedFirst)
		}
	}
}

// TestVoyageProviderEmbedBatchErrors tests error handling for batch embedding
func TestVoyageProviderEmbedBatchErrors(t *testing.T) {
	config := Config{
		APIKey: "test-api-key",
		APIURL: "http://localhost:9999",
		Model:  "voyage-code-2",
	}

	provider, err := NewVoyageProvider(config)
	if err != nil {
		t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	tests := []struct {
		name    string
		texts   []string
		wantErr string
	}{
		{
			name:    "empty texts slice",
			texts:   []string{},
			wantErr: ErrInvalidInput.Error(),
		},
		{
			name:    "empty text in slice",
			texts:   []string{"text1", "", "text3"},
			wantErr: ErrInvalidInput.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := provider.EmbedBatch(ctx, tt.texts)
			if err == nil {
				t.Error("EmbedBatch() expected error, got nil")
				return
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("EmbedBatch() error = %v, want error containing %v", err, tt.wantErr)
			}
		})
	}
}

// TestVoyageProviderHTTPErrors tests HTTP error handling
func TestVoyageProviderHTTPErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		wantErr    error
	}{
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			wantErr:    ErrInvalidCredentials,
		},
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
			wantErr:    ErrInvalidInput,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
			wantErr:    ErrModelNotFound,
		},
		{
			name:       "rate limit",
			statusCode: http.StatusTooManyRequests,
			wantErr:    ErrRateLimitExceeded,
		},
		{
			name:       "internal server error",
			statusCode: http.StatusInternalServerError,
			wantErr:    ErrProviderUnavailable,
		},
		{
			name:       "bad gateway",
			statusCode: http.StatusBadGateway,
			wantErr:    ErrProviderUnavailable,
		},
		{
			name:       "service unavailable",
			statusCode: http.StatusServiceUnavailable,
			wantErr:    ErrProviderUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				errResp := voyageError{}
				errResp.Error.Message = "test error message"
				errResp.Error.Type = "test_error"

				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				json.NewEncoder(w).Encode(errResp)
			}))
			defer server.Close()

			config := Config{
				APIKey:     "test-api-key",
				APIURL:     server.URL,
				Model:      "voyage-code-2",
				MaxRetries: 0, // Disable retries for error testing
			}

			provider, err := NewVoyageProvider(config)
			if err != nil {
				t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
			}
			defer provider.Close()

			ctx := context.Background()
			_, err = provider.Embed(ctx, "test text")
			if err == nil {
				t.Error("Embed() expected error, got nil")
				return
			}

			if !strings.Contains(err.Error(), tt.wantErr.Error()) {
				t.Errorf("Embed() error = %v, want error containing %v", err, tt.wantErr)
			}
		})
	}
}

// TestVoyageProviderRetry tests retry logic with exponential backoff
func TestVoyageProviderRetry(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			// Fail first 2 attempts with 5xx error
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		// Succeed on 3rd attempt
		resp := voyageResponse{
			Object: "list",
			Model:  "voyage-code-2",
			Data: []voyageEmbedding{
				{
					Object:    "embedding",
					Embedding: make([]float32, 1536),
					Index:     0,
				},
			},
			Usage: voyageUsage{
				TotalTokens: 10,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	config := Config{
		APIKey:         "test-api-key",
		APIURL:         server.URL,
		Model:          "voyage-code-2",
		MaxRetries:     3,
		RetryBackoffMS: 100, // Short backoff for testing
	}

	provider, err := NewVoyageProvider(config)
	if err != nil {
		t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	startTime := time.Now()
	embedding, err := provider.Embed(ctx, "test text")
	elapsed := time.Since(startTime)

	if err != nil {
		t.Fatalf("Embed() unexpected error = %v", err)
	}

	if embedding == nil {
		t.Error("Embed() returned nil embedding")
	}

	if attempts != 3 {
		t.Errorf("attempts = %v, want 3", attempts)
	}

	// Verify backoff timing (should have 2 backoffs: 100ms and 200ms)
	minExpected := 300 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("elapsed time = %v, want at least %v", elapsed, minExpected)
	}
}

// TestVoyageProviderRetryExhausted tests retry exhaustion
func TestVoyageProviderRetryExhausted(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	config := Config{
		APIKey:         "test-api-key",
		APIURL:         server.URL,
		Model:          "voyage-code-2",
		MaxRetries:     2,
		RetryBackoffMS: 10,
	}

	provider, err := NewVoyageProvider(config)
	if err != nil {
		t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	_, err = provider.Embed(ctx, "test text")
	if err == nil {
		t.Error("Embed() expected error after retry exhaustion, got nil")
	}

	// Should attempt: initial + maxRetries = 1 + 2 = 3
	expectedAttempts := 3
	if attempts != expectedAttempts {
		t.Errorf("attempts = %v, want %v", attempts, expectedAttempts)
	}

	if !strings.Contains(err.Error(), "failed after") {
		t.Errorf("error should mention retry count: %v", err)
	}
}

// TestVoyageProviderContextCancellation tests context cancellation
func TestVoyageProviderContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Delay to allow context cancellation
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := Config{
		APIKey: "test-api-key",
		APIURL: server.URL,
		Model:  "voyage-code-2",
	}

	provider, err := NewVoyageProvider(config)
	if err != nil {
		t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
	}
	defer provider.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = provider.Embed(ctx, "test text")
	if err == nil {
		t.Error("Embed() expected context cancellation error, got nil")
	}

	if !strings.Contains(err.Error(), "context") && !strings.Contains(err.Error(), "deadline") {
		t.Errorf("expected context/deadline error, got: %v", err)
	}
}

// TestVoyageProviderClose tests cleanup
func TestVoyageProviderClose(t *testing.T) {
	config := Config{
		APIKey: "test-api-key",
		Model:  "voyage-code-2",
	}

	provider, err := NewVoyageProvider(config)
	if err != nil {
		t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
	}

	err = provider.Close()
	if err != nil {
		t.Errorf("Close() unexpected error = %v", err)
	}
}

// TestVoyageProviderNoRetryOnClientErrors tests that client errors are not retried
func TestVoyageProviderNoRetryOnClientErrors(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
	}{
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
		},
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
		},
		{
			name:       "not found",
			statusCode: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts++
				w.WriteHeader(tt.statusCode)
				json.NewEncoder(w).Encode(voyageError{})
			}))
			defer server.Close()

			config := Config{
				APIKey:         "test-api-key",
				APIURL:         server.URL,
				Model:          "voyage-code-2",
				MaxRetries:     3,
				RetryBackoffMS: 10,
			}

			provider, err := NewVoyageProvider(config)
			if err != nil {
				t.Fatalf("NewVoyageProvider() unexpected error = %v", err)
			}
			defer provider.Close()

			ctx := context.Background()
			_, err = provider.Embed(ctx, "test text")
			if err == nil {
				t.Error("Embed() expected error, got nil")
			}

			// Should only attempt once, no retries
			if attempts != 1 {
				t.Errorf("attempts = %v, want 1 (no retries on client errors)", attempts)
			}
		})
	}
}
