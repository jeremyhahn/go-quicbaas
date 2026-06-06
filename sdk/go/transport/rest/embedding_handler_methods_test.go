// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
)

// TestEmbeddingHandlerGenerateAndList drives the non-GetProviderInfo dispatch
// path of handleRequest (generic request + convertResponse) for Generate and
// ListProviders.
func TestEmbeddingHandlerGenerateAndList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/embedding/generate":
			_, _ = w.Write([]byte(`{"embedding":[0.1,0.2,0.3],"model":"m1","timestamp":"2025-01-01T00:00:00Z"}`))
		case "/api/v1/embedding/providers":
			_, _ = w.Write([]byte(`{"providers":[{"name":"ollama"}],"default_provider":"ollama","timestamp":"2025-01-01T00:00:00Z"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("Generate", func(t *testing.T) {
		resp, err := tr.Request(ctx, "Embedding", "Generate",
			&embedding.GenerateEmbeddingRequest{Text: "hello"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out, ok := resp.(*embedding.GenerateEmbeddingResponse)
		if !ok {
			t.Fatalf("unexpected response type %T", resp)
		}
		if len(out.Embedding) != 3 {
			t.Errorf("expected 3-dim embedding, got %v", out.Embedding)
		}
	})

	t.Run("ListProviders", func(t *testing.T) {
		resp, err := tr.Request(ctx, "Embedding", "ListProviders",
			&embedding.ListProvidersRequest{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out, ok := resp.(*embedding.ListProvidersResponse)
		if !ok {
			t.Fatalf("unexpected response type %T", resp)
		}
		if out.DefaultProvider != "ollama" {
			t.Errorf("expected default provider ollama, got %q", out.DefaultProvider)
		}
	})
}

// TestEmbeddingHandlerGenerateServerError exercises the requestWithRetry error
// path in handleRequest.
func TestEmbeddingHandlerGenerateServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Embedding", "Generate",
		&embedding.GenerateEmbeddingRequest{Text: "hi"})
	if err == nil {
		t.Fatal("expected error from server 500")
	}
}

// TestEmbeddingHandlerBatchAndSetDefault exercises convertBatchGenerateEmbeddingsResponse,
// convertBatchEmbeddingResult and convertSetDefaultProviderResponse.
func TestEmbeddingHandlerBatchAndSetDefault(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/embedding/batch":
			_, _ = w.Write([]byte(`{"success_count":1,"error_count":0,"total_tokens":12,` +
				`"processing_time":"5ms","timestamp":"2025-01-01T00:00:00Z",` +
				`"results":[{"id":"a","embedding":[0.1,0.2],"model":"m","provider":"ollama",` +
				`"dimensions":2,"tokens":6,"cache_hit":true,"processing_time":"2ms"}]}`))
		case "/api/v1/embedding/providers/default":
			_, _ = w.Write([]byte(`{"provider":"ollama","previous_provider":"openai","timestamp":"2025-01-01T00:00:00Z"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("BatchGenerate", func(t *testing.T) {
		resp, err := tr.Request(ctx, "Embedding", "BatchGenerate",
			&embedding.BatchGenerateEmbeddingsRequest{
				Items: []*embedding.BatchEmbeddingItem{{ID: "a", Text: "hi"}}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		out, ok := resp.(*embedding.BatchGenerateEmbeddingsResponse)
		if !ok {
			t.Fatalf("unexpected type %T", resp)
		}
		if len(out.Results) != 1 || out.Results[0].ID != "a" || !out.Results[0].Metadata.CacheHit {
			t.Errorf("unexpected batch result: %+v", out.Results)
		}
	})

	t.Run("SetDefaultProvider", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Embedding", "SetDefaultProvider",
			&embedding.SetDefaultProviderRequest{Provider: embedding.Provider("ollama")}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
