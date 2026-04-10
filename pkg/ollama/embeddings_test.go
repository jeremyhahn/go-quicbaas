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

package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestNewEmbeddingService(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewEmbeddingService(client, "nomic-embed-text")

	if service.Model() != "nomic-embed-text" {
		t.Errorf("expected model 'nomic-embed-text', got %s", service.Model())
	}
}

func TestEmbeddingService_SetModel(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewEmbeddingService(client, "model1")

	service.SetModel("model2")
	if service.Model() != "model2" {
		t.Errorf("expected model 'model2', got %s", service.Model())
	}
}

func TestEmbeddingService_Embed_Success(t *testing.T) {
	expectedEmbedding := []float32{0.1, 0.2, 0.3, 0.4, 0.5}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Errorf("expected path /api/embeddings, got %s", r.URL.Path)
		}

		var req EmbedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}

		if req.Model != "nomic-embed-text" {
			t.Errorf("expected model 'nomic-embed-text', got %s", req.Model)
		}

		if req.Prompt != "test text" {
			t.Errorf("expected prompt 'test text', got %s", req.Prompt)
		}

		resp := EmbedResponse{Embedding: expectedEmbedding}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewEmbeddingService(client, "nomic-embed-text")

	embedding, err := service.Embed(context.Background(), "test text")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(embedding) != len(expectedEmbedding) {
		t.Fatalf("expected %d dimensions, got %d", len(expectedEmbedding), len(embedding))
	}

	for i, v := range embedding {
		if v != expectedEmbedding[i] {
			t.Errorf("expected embedding[%d] = %f, got %f", i, expectedEmbedding[i], v)
		}
	}
}

func TestEmbeddingService_Embed_EmptyText(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewEmbeddingService(client, "nomic-embed-text")

	_, err := service.Embed(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty text")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestEmbeddingService_EmbedWithModel_EmptyModel(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewEmbeddingService(client, "nomic-embed-text")

	_, err := service.EmbedWithModel(context.Background(), "", "text")
	if err == nil {
		t.Fatal("expected error for empty model")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestEmbeddingService_Embed_EmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EmbedResponse{Embedding: []float32{}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewEmbeddingService(client, "nomic-embed-text")

	_, err := service.Embed(context.Background(), "test text")
	if err == nil {
		t.Fatal("expected error for empty response")
	}

	if !errors.Is(err, ErrEmptyResponse) {
		t.Errorf("expected ErrEmptyResponse, got %v", err)
	}
}

func TestEmbeddingService_EmbedBatch_Success(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		resp := EmbedResponse{Embedding: []float32{0.1, 0.2, 0.3}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewEmbeddingService(client, "nomic-embed-text")

	texts := []string{"text1", "text2", "text3"}
	embeddings, err := service.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(embeddings) != len(texts) {
		t.Fatalf("expected %d embeddings, got %d", len(texts), len(embeddings))
	}

	// For 3 texts, should use sequential processing
	if callCount.Load() != 3 {
		t.Errorf("expected 3 calls for sequential processing, got %d", callCount.Load())
	}
}

func TestEmbeddingService_EmbedBatch_EmptyTexts(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewEmbeddingService(client, "nomic-embed-text")

	_, err := service.EmbedBatch(context.Background(), []string{})
	if err == nil {
		t.Fatal("expected error for empty texts")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestEmbeddingService_EmbedBatch_EmptyTextInSlice(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewEmbeddingService(client, "nomic-embed-text")

	texts := []string{"valid", "", "also valid"}
	_, err := service.EmbedBatch(context.Background(), texts)
	if err == nil {
		t.Fatal("expected error for empty text in slice")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestEmbeddingService_EmbedBatchWithModel_EmptyModel(t *testing.T) {
	client := NewClient(DefaultClientConfig())
	service := NewEmbeddingService(client, "nomic-embed-text")

	_, err := service.EmbedBatchWithModel(context.Background(), "", []string{"text"})
	if err == nil {
		t.Fatal("expected error for empty model")
	}

	if !errors.Is(err, ErrInvalidInput) {
		t.Errorf("expected ErrInvalidInput, got %v", err)
	}
}

func TestEmbeddingService_EmbedBatch_Concurrent(t *testing.T) {
	var callCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		resp := EmbedResponse{Embedding: []float32{0.1, 0.2, 0.3}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewEmbeddingService(client, "nomic-embed-text")

	// More than 3 texts triggers concurrent processing
	texts := []string{"text1", "text2", "text3", "text4", "text5"}
	embeddings, err := service.EmbedBatch(context.Background(), texts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(embeddings) != len(texts) {
		t.Fatalf("expected %d embeddings, got %d", len(texts), len(embeddings))
	}

	if callCount.Load() != 5 {
		t.Errorf("expected 5 calls, got %d", callCount.Load())
	}
}

func TestEmbeddingService_EmbedBatch_ConcurrentError(t *testing.T) {
	// All requests fail to ensure we get an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		BaseURL:    server.URL,
		MaxRetries: 0, // No retries
	})
	service := NewEmbeddingService(client, "nomic-embed-text")

	texts := []string{"text1", "text2", "text3", "text4", "text5"}
	_, err := service.EmbedBatch(context.Background(), texts)
	if err == nil {
		t.Fatal("expected error from batch processing")
	}
}

func TestEmbeddingService_EmbedBatch_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EmbedResponse{Embedding: []float32{0.1, 0.2, 0.3}}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(ClientConfig{BaseURL: server.URL})
	service := NewEmbeddingService(client, "nomic-embed-text")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	texts := []string{"text1", "text2", "text3", "text4", "text5"}
	_, err := service.EmbedBatch(ctx, texts)
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
}
