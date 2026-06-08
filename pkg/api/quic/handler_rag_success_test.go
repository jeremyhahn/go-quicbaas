//go:build quic && rag

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package quic

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// mockRAGEnabledService wires both an Ollama and a Vector service so the full
// handleRAGQuery pipeline (embed -> vector search -> context build) can run.
type mockRAGEnabledService struct {
	mockService
	ollamaSvc service.OllamaService
	vectorSvc service.VectorService
}

func (m *mockRAGEnabledService) Ollama() service.OllamaService { return m.ollamaSvc }
func (m *mockRAGEnabledService) Vector() service.VectorService { return m.vectorSvc }

func newRAGHandler(ollama service.OllamaService, vector service.VectorService) *Handler {
	svc := &mockRAGEnabledService{ollamaSvc: ollama, vectorSvc: vector}
	return NewHandler(svc, nil, nil, nil, createTestLogger())
}

// TestRAGQuery_FullPipelineSuccess exercises the complete handleRAGQuery path:
// embedding generation via the default model, vector search, RAG context build,
// and response encoding.
func TestRAGQuery_FullPipelineSuccess(t *testing.T) {
	h := newRAGHandler(&mockOllamaService{}, &mockVectorService{})
	ctx := context.Background()

	payload, err := EncodePayload(&RAGQueryRequest{
		Query:            "how does the barrier seal work",
		Namespace:        "docs",
		IncludeCitations: true,
	})
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}

	resp := h.handleRAGQuery(ctx, &Request{Opcode: OpRAGQuery, Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("expected StatusOK, got 0x%02X (payload=%s)", resp.Status, string(resp.Payload))
	}
}

// TestRAGQuery_WithModelSuccess exercises the EmbedWithModel branch (Model set).
func TestRAGQuery_WithModelSuccess(t *testing.T) {
	h := newRAGHandler(&mockOllamaService{}, &mockVectorService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGQueryRequest{
		Query:     "explain shamir unseal",
		Namespace: "docs",
		Model:     "nomic-embed-text",
		TopK:      5,
		MaxTokens: 2000,
		MinScore:  0.5,
		Template:  "json",
	})

	resp := h.handleRAGQuery(ctx, &Request{Opcode: OpRAGQuery, Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("expected StatusOK, got 0x%02X", resp.Status)
	}
}

// TestRAGQuery_VectorUnavailable covers the Vector-service nil branch.
func TestRAGQuery_VectorUnavailable(t *testing.T) {
	h := newRAGHandler(&mockOllamaService{}, nil)
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGQueryRequest{Query: "q", Namespace: "n"})
	resp := h.handleRAGQuery(ctx, &Request{Opcode: OpRAGQuery, Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error when vector service is unavailable")
	}
}

// TestRAGQuery_MissingQuery covers the required-field validation for query.
func TestRAGQuery_MissingQuery(t *testing.T) {
	h := newRAGHandler(&mockOllamaService{}, &mockVectorService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGQueryRequest{Namespace: "docs"})
	resp := h.handleRAGQuery(ctx, &Request{Opcode: OpRAGQuery, Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error for missing query")
	}
}

// TestRAGQuery_MissingNamespace covers the required-field validation for namespace.
func TestRAGQuery_MissingNamespace(t *testing.T) {
	h := newRAGHandler(&mockOllamaService{}, &mockVectorService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGQueryRequest{Query: "q"})
	resp := h.handleRAGQuery(ctx, &Request{Opcode: OpRAGQuery, Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error for missing namespace")
	}
}

// TestRAGQuery_EmbedError covers the embedding failure branch.
func TestRAGQuery_EmbedError(t *testing.T) {
	ollama := &mockOllamaService{
		embedFn: func(context.Context, *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
			return nil, errors.New("embed failed")
		},
	}
	h := newRAGHandler(ollama, &mockVectorService{})
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGQueryRequest{Query: "q", Namespace: "n"})
	resp := h.handleRAGQuery(ctx, &Request{Opcode: OpRAGQuery, Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error when embedding fails")
	}
}

// TestRAGQuery_SearchError covers the vector-search failure branch.
func TestRAGQuery_SearchError(t *testing.T) {
	vector := &mockVectorService{
		searchSimilarFn: func(context.Context, *service.SearchSimilarRequest) (*service.SearchSimilarResponse, error) {
			return nil, errors.New("search failed")
		},
	}
	h := newRAGHandler(&mockOllamaService{}, vector)
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGQueryRequest{Query: "q", Namespace: "n"})
	resp := h.handleRAGQuery(ctx, &Request{Opcode: OpRAGQuery, Payload: payload})
	if resp.Status == StatusOK {
		t.Fatal("expected error when vector search fails")
	}
}

// TestRAGBuildContext_Success exercises the full handleRAGBuildContext success
// path including citation extraction and metadata parsing.
func TestRAGBuildContext_Success(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, err := EncodePayload(&RAGBuildContextRequest{
		Results: []RAGResultInput{
			{
				Content: "the barrier seals the data store",
				Score:   0.95,
				Metadata: map[string]string{
					"file_path":  "docs/barrier.md",
					"file_name":  "barrier.md",
					"language":   "markdown",
					"start_line": "10",
					"end_line":   "20",
				},
			},
			{
				Content: "shamir secret sharing splits the unseal key",
				Score:   0.88,
				Metadata: map[string]string{
					"file_path":  "docs/shamir.md",
					"start_line": "1",
					"end_line":   "5",
				},
			},
		},
		MaxTokens:          2000,
		Template:           "markdown",
		IncludeCitations:   true,
		TruncationStrategy: "smart",
	})
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}

	resp := h.handleRAGBuildContext(ctx, &Request{Opcode: OpRAGBuildContext, Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("expected StatusOK, got 0x%02X (payload=%s)", resp.Status, string(resp.Payload))
	}
}

// TestRAGBuildContext_HardTruncation covers the "hard" truncation-strategy branch.
func TestRAGBuildContext_HardTruncation(t *testing.T) {
	h := createTestHandler()
	ctx := context.Background()

	payload, _ := EncodePayload(&RAGBuildContextRequest{
		Results:            []RAGResultInput{{Content: "content", Score: 0.5}},
		TruncationStrategy: "hard",
		Template:           "plain",
	})

	resp := h.handleRAGBuildContext(ctx, &Request{Opcode: OpRAGBuildContext, Payload: payload})
	if resp.Status != StatusOK {
		t.Fatalf("expected StatusOK, got 0x%02X", resp.Status)
	}
}
