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
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/rag"
)

func TestNewRAGHandler(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	assert.NotNil(t, handler)
	assert.Equal(t, tr, handler.transport)
}

func TestRAGHandler_HandleRequest_UnknownMethod(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.RAG", "UnknownMethod", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown method: UnknownMethod")
}

func TestRAGHandler_HandleRequest_Query(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		defer r.Body.Close()

		err = json.Unmarshal(body, &capturedBody)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"context":      "Here is the relevant context for your query.",
			"tokens":       float64(128),
			"truncated":    false,
			"result_count": float64(3),
			"citations": []interface{}{
				map[string]interface{}{
					"index":      float64(0),
					"file_path":  "/docs/guide.md",
					"start_line": float64(10),
					"end_line":   float64(25),
					"score":      float64(0.95),
				},
				map[string]interface{}{
					"index":      float64(1),
					"file_path":  "/docs/api.md",
					"start_line": float64(5),
					"end_line":   float64(15),
					"score":      float64(0.87),
				},
			},
		}
		err = json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	req := &rag.QueryRequest{
		Query:            "How do I configure backups?",
		Namespace:        "docs",
		TopK:             5,
		MaxTokens:        512,
		MinScore:         0.7,
		Template:         "markdown",
		IncludeCitations: true,
		Model:            "text-embedding-ada-002",
	}
	result, err := handler.handleRequest(ctx, "quicbaas.RAG", "Query", req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "/api/v1/rag/query", capturedPath)

	assert.Equal(t, "How do I configure backups?", capturedBody["query"])
	assert.Equal(t, "docs", capturedBody["namespace"])
	assert.Equal(t, float64(5), capturedBody["top_k"])
	assert.Equal(t, float64(512), capturedBody["max_tokens"])
	assert.Equal(t, true, capturedBody["include_citations"])
	assert.Equal(t, "markdown", capturedBody["template"])
	assert.Equal(t, "text-embedding-ada-002", capturedBody["model"])

	queryResp, ok := result.(*rag.QueryResponse)
	assert.True(t, ok)
	assert.Equal(t, "Here is the relevant context for your query.", queryResp.Context)
	assert.Equal(t, 128, queryResp.Tokens)
	assert.False(t, queryResp.Truncated)
	assert.Equal(t, 3, queryResp.ResultCount)
	assert.Len(t, queryResp.Citations, 2)

	assert.Equal(t, 0, queryResp.Citations[0].Index)
	assert.Equal(t, "/docs/guide.md", queryResp.Citations[0].FilePath)
	assert.Equal(t, 10, queryResp.Citations[0].StartLine)
	assert.Equal(t, 25, queryResp.Citations[0].EndLine)
	assert.Equal(t, float32(0.95), queryResp.Citations[0].Score)

	assert.Equal(t, 1, queryResp.Citations[1].Index)
	assert.Equal(t, "/docs/api.md", queryResp.Citations[1].FilePath)
	assert.Equal(t, float32(0.87), queryResp.Citations[1].Score)
}

func TestRAGHandler_HandleRequest_Query_InvalidType(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.RAG", "Query", "invalid-request-type")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestRAGHandler_HandleRequest_Query_NilRequest(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.RAG", "Query", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestRAGHandler_HandleRequest_Query_NoCitations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"context":      "Simple context response.",
			"tokens":       float64(32),
			"truncated":    false,
			"result_count": float64(1),
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	req := &rag.QueryRequest{
		Query:            "test query",
		Namespace:        "ns1",
		IncludeCitations: false,
	}
	result, err := handler.handleRequest(ctx, "quicbaas.RAG", "Query", req)
	assert.NoError(t, err)

	queryResp, ok := result.(*rag.QueryResponse)
	assert.True(t, ok)
	assert.Equal(t, "Simple context response.", queryResp.Context)
	assert.Equal(t, 32, queryResp.Tokens)
	assert.Nil(t, queryResp.Citations)
}

func TestRAGHandler_HandleRequest_BuildContext(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		defer r.Body.Close()

		err = json.Unmarshal(body, &capturedBody)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"context":      "Built context from provided results.",
			"tokens":       float64(256),
			"truncated":    true,
			"result_count": float64(2),
			"citations": []interface{}{
				map[string]interface{}{
					"index":      float64(0),
					"file_path":  "/src/main.go",
					"start_line": float64(1),
					"end_line":   float64(50),
					"score":      float64(0.92),
				},
			},
		}
		err = json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	req := &rag.BuildContextRequest{
		Results: []rag.ResultInput{
			{
				Content:  "First result content.",
				Score:    0.95,
				Metadata: map[string]string{"source": "file1.go"},
			},
			{
				Content:  "Second result content.",
				Score:    0.88,
				Metadata: map[string]string{"source": "file2.go"},
			},
		},
		MaxTokens:          1024,
		Template:           "json",
		IncludeCitations:   true,
		TruncationStrategy: "smart",
	}
	result, err := handler.handleRequest(ctx, "quicbaas.RAG", "BuildContext", req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "/api/v1/rag/context", capturedPath)

	assert.Equal(t, float64(1024), capturedBody["max_tokens"])
	assert.Equal(t, "json", capturedBody["template"])
	assert.Equal(t, true, capturedBody["include_citations"])
	assert.Equal(t, "smart", capturedBody["truncation_strategy"])

	results, ok := capturedBody["results"].([]interface{})
	assert.True(t, ok)
	assert.Len(t, results, 2)

	buildResp, ok := result.(*rag.BuildContextResponse)
	assert.True(t, ok)
	assert.Equal(t, "Built context from provided results.", buildResp.Context)
	assert.Equal(t, 256, buildResp.Tokens)
	assert.True(t, buildResp.Truncated)
	assert.Equal(t, 2, buildResp.ResultCount)
	assert.Len(t, buildResp.Citations, 1)
	assert.Equal(t, "/src/main.go", buildResp.Citations[0].FilePath)
	assert.Equal(t, float32(0.92), buildResp.Citations[0].Score)
}

func TestRAGHandler_HandleRequest_BuildContext_InvalidType(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.RAG", "BuildContext", "invalid-type")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestRAGHandler_HandleRequest_BuildContext_NilRequest(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.RAG", "BuildContext", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid request type")
}

func TestRAGHandler_HandleRequest_BuildContext_EmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"context":      "",
			"tokens":       float64(0),
			"truncated":    false,
			"result_count": float64(0),
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	req := &rag.BuildContextRequest{
		Results:   []rag.ResultInput{},
		MaxTokens: 512,
	}
	result, err := handler.handleRequest(ctx, "quicbaas.RAG", "BuildContext", req)
	assert.NoError(t, err)

	buildResp, ok := result.(*rag.BuildContextResponse)
	assert.True(t, ok)
	assert.Equal(t, "", buildResp.Context)
	assert.Equal(t, 0, buildResp.Tokens)
	assert.Equal(t, 0, buildResp.ResultCount)
}

func TestRAGHandler_HandleRequest_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte(`{"error": "embedding service unavailable", "code": "internal"}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	req := &rag.QueryRequest{
		Query:     "test query",
		Namespace: "ns1",
	}
	_, err := handler.handleRequest(ctx, "quicbaas.RAG", "Query", req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "embedding service unavailable")
}

func TestIsRAGService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{"fully qualified", "quicbaas.RAG", true},
		{"bare name", "RAG", true},
		{"non-RAG KV", "quicbaas.KV", false},
		{"non-RAG Vector", "quicbaas.Vector", false},
		{"empty string", "", false},
		{"partial match", "quicbaas.RAGPipeline", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRAGService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRAGHandler_HandleRequest_WithBasePath(t *testing.T) {
	var capturedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"context":      "context with base path",
			"tokens":       float64(10),
			"truncated":    false,
			"result_count": float64(1),
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "/quicbaas",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newRAGHandler(tr)

	ctx := context.Background()
	req := &rag.QueryRequest{
		Query:     "test",
		Namespace: "ns1",
	}
	_, err := handler.handleRequest(ctx, "quicbaas.RAG", "Query", req)
	assert.NoError(t, err)

	assert.Equal(t, "/quicbaas/api/v1/rag/query", capturedPath)
}

func TestRAGHandler_HandleRequest_TransportClosed(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	tr.closed.Store(true)
	handler := newRAGHandler(tr)

	ctx := context.Background()
	req := &rag.QueryRequest{
		Query:     "test",
		Namespace: "ns1",
	}
	_, err := handler.handleRequest(ctx, "quicbaas.RAG", "Query", req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transport is closed")
}
