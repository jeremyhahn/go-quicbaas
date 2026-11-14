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
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ========================================
// RAG Route Registration
// ========================================

// ========================================
// Request/Response Types
// ========================================

// RAGQueryRequest is the request body for RAG query.
type RAGQueryRequest struct {
	Query            string  `json:"query"`
	Namespace        string  `json:"namespace"`
	TopK             int     `json:"top_k,omitempty"`
	MaxTokens        int     `json:"max_tokens,omitempty"`
	MinScore         float32 `json:"min_score,omitempty"`
	Template         string  `json:"template,omitempty"`
	IncludeCitations bool    `json:"include_citations,omitempty"`
	Model            string  `json:"model,omitempty"`
}

// RAGQueryResponse is the response for RAG query.
type RAGQueryResponse struct {
	Context     string        `json:"context"`
	Tokens      int           `json:"tokens"`
	Truncated   bool          `json:"truncated"`
	ResultCount int           `json:"result_count"`
	Citations   []RAGCitation `json:"citations,omitempty"`
}

// RAGCitation represents a source citation.
type RAGCitation struct {
	Index     int     `json:"index"`
	FilePath  string  `json:"file_path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float32 `json:"score"`
}

// RAGBuildContextRequest is the request body for building context.
type RAGBuildContextRequest struct {
	Results            []RAGResultInput `json:"results"`
	MaxTokens          int              `json:"max_tokens,omitempty"`
	Template           string           `json:"template,omitempty"`
	IncludeCitations   bool             `json:"include_citations,omitempty"`
	TruncationStrategy string           `json:"truncation_strategy,omitempty"`
}

// RAGResultInput represents a single result for context building.
type RAGResultInput struct {
	Content  string            `json:"content"`
	Score    float32           `json:"score"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// RAGBuildContextResponse is the response for context building.
type RAGBuildContextResponse struct {
	Context     string        `json:"context"`
	Tokens      int           `json:"tokens"`
	Truncated   bool          `json:"truncated"`
	ResultCount int           `json:"result_count"`
	Citations   []RAGCitation `json:"citations,omitempty"`
}

// ========================================
// RAG Query Handler
// ========================================

// HandleRAGQuery performs an all-in-one RAG query: embed, search, build context.
func (s *Server) HandleRAGQuery(w http.ResponseWriter, r *http.Request) {
	var req RAGQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// The one RAG query every transport serves (#1611).
	out, err := ragservice.Query(r.Context(), s.service, ragservice.QueryRequest{
		Query:            req.Query,
		Namespace:        req.Namespace,
		TopK:             req.TopK,
		MaxTokens:        req.MaxTokens,
		MinScore:         req.MinScore,
		Template:         req.Template,
		IncludeCitations: req.IncludeCitations,
		Model:            req.Model,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, RAGQueryResponse{
		Context:     out.Context,
		Tokens:      out.Tokens,
		Truncated:   out.Truncated,
		ResultCount: out.ResultCount,
		Citations:   restCitations(out.Citations),
	})
}

// restCitations renders the RAG service's citations in the REST shape.
func restCitations(in []ragservice.Citation) []RAGCitation {
	if len(in) == 0 {
		return nil
	}
	out := make([]RAGCitation, len(in))
	for i, c := range in {
		out[i] = RAGCitation{
			Index:     c.Index,
			FilePath:  c.FilePath,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Score:     c.Score,
		}
	}
	return out
}

// ========================================
// RAG Build Context Handler
// ========================================

// HandleRAGBuildContext builds context from user-provided results.
func (s *Server) HandleRAGBuildContext(w http.ResponseWriter, r *http.Request) {
	var req RAGBuildContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	in := make([]ragservice.ResultInput, len(req.Results))
	for i, res := range req.Results {
		in[i] = ragservice.ResultInput{Content: res.Content, Score: res.Score, Metadata: res.Metadata}
	}
	// The one context assembly every transport serves (#1611).
	out, err := ragservice.BuildContext(r.Context(), ragservice.BuildContextRequest{
		Results:            in,
		MaxTokens:          req.MaxTokens,
		Template:           req.Template,
		IncludeCitations:   req.IncludeCitations,
		TruncationStrategy: req.TruncationStrategy,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}
	RespondJSON(w, r, http.StatusOK, RAGBuildContextResponse{
		Context:     out.Context,
		Tokens:      out.Tokens,
		Truncated:   out.Truncated,
		ResultCount: out.ResultCount,
		Citations:   restCitations(out.Citations),
	})
}

// ========================================
// Helper Functions
// ========================================

func getStringFromInterfaceMap(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntFromInterfaceMap(m map[string]interface{}, key string) int {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case int:
			return val
		case int64:
			return int(val)
		case float64:
			return int(val)
		case string:
			if i, err := strconv.Atoi(val); err == nil {
				return i
			}
		}
	}
	return 0
}

// ========================================
// RAG Document CRUD Handlers
// ========================================

// RAGDocumentResponse represents a RAG document (vector with content metadata).
type RAGDocumentResponse struct {
	ID        uint64                 `json:"id"`
	Namespace string                 `json:"namespace"`
	Content   string                 `json:"content,omitempty"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp int64                  `json:"timestamp"`
}

// RAGListDocumentsResponse contains a page of RAG documents.
type RAGListDocumentsResponse struct {
	Documents  []*RAGDocumentResponse `json:"documents"`
	NextCursor string                 `json:"next_cursor,omitempty"`
	Total      uint64                 `json:"total"`
}

// HandleRAGListDocuments handles GET /v1/rag/documents?namespace=X - List documents in a namespace.
func (s *Server) HandleRAGListDocuments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	limit := 100
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}
	cursor := r.URL.Query().Get("cursor")

	vectorSvc := s.service.Vector()
	if vectorSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "vector service not configured",
		})
		return
	}

	resp, err := vectorSvc.ListVectors(ctx, &service.ListVectorsRequest{
		Namespace:       namespace,
		Limit:           limit,
		Cursor:          cursor,
		IncludeMetadata: true,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	docs := make([]*RAGDocumentResponse, 0, len(resp.Vectors))
	for _, v := range resp.Vectors {
		content := getStringFromInterfaceMap(v.Metadata, "content")
		docs = append(docs, &RAGDocumentResponse{
			ID:        v.ID,
			Namespace: namespace,
			Content:   content,
			Metadata:  v.Metadata,
			Timestamp: v.UpdatedAt,
		})
	}

	RespondJSON(w, r, http.StatusOK, &RAGListDocumentsResponse{
		Documents:  docs,
		NextCursor: resp.NextCursor,
		Total:      resp.TotalCount,
	})
}

// HandleRAGGetDocument handles GET /v1/rag/documents/{id} - Get a document by ID.
func (s *Server) HandleRAGGetDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		RespondBadRequest(w, r, "document ID is required")
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "document ID must be a positive integer")
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	vectorSvc := s.service.Vector()
	if vectorSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "vector service not configured",
		})
		return
	}

	resp, err := vectorSvc.GetVector(ctx, &service.GetVectorRequest{
		Namespace:       namespace,
		ID:              id,
		IncludeMetadata: true,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	doc := &RAGDocumentResponse{
		ID:        resp.ID,
		Namespace: namespace,
		Content:   getStringFromInterfaceMap(resp.Metadata, "content"),
		Metadata:  resp.Metadata,
		Timestamp: resp.UpdatedAt,
	}

	RespondJSON(w, r, http.StatusOK, doc)
}

// HandleRAGDeleteDocument handles DELETE /v1/rag/documents/{id} - Delete a document by ID.
func (s *Server) HandleRAGDeleteDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		RespondBadRequest(w, r, "document ID is required")
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "document ID must be a positive integer")
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	vectorSvc := s.service.Vector()
	if vectorSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "vector service not configured",
		})
		return
	}

	if _, err := vectorSvc.DeleteVector(ctx, &service.DeleteVectorRequest{
		Namespace: namespace,
		ID:        id,
	}); err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"id":      id,
		"deleted": true,
	})
}

// RAGReindexRequest is the optional body for reindex.
type RAGReindexRequest struct {
	// Content overrides the stored content for reindexing (optional)
	Content string `json:"content,omitempty"`
}

// HandleRAGReindexDocument handles POST /v1/rag/documents/{id}/reindex - Re-embed a document.
func (s *Server) HandleRAGReindexDocument(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		RespondBadRequest(w, r, "document ID is required")
		return
	}

	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil {
		RespondBadRequest(w, r, "document ID must be a positive integer")
		return
	}

	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	var body RAGReindexRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	vectorSvc := s.service.Vector()
	if vectorSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "vector service not configured",
		})
		return
	}

	// Fetch existing document
	existing, err := vectorSvc.GetVector(ctx, &service.GetVectorRequest{
		Namespace:       namespace,
		ID:              id,
		IncludeMetadata: true,
		IncludeVector:   false,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	content := body.Content
	if content == "" {
		content = getStringFromInterfaceMap(existing.Metadata, "content")
	}
	if content == "" {
		RespondBadRequest(w, r, "document has no content to reindex")
		return
	}

	// Generate new embedding
	embeddingSvc := s.service.Embedding()
	if embeddingSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "embedding service not configured",
		})
		return
	}

	embedResp, err := embeddingSvc.GenerateEmbedding(ctx, &service.GenerateEmbeddingRequest{
		Text: content,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Update metadata with new content if overridden
	meta := existing.Metadata
	if meta == nil {
		meta = make(map[string]interface{})
	}
	if body.Content != "" {
		meta["content"] = body.Content
	}

	// Re-upsert with fresh embedding
	upsertResp, err := vectorSvc.UpsertVector(ctx, &service.UpsertVectorRequest{
		Namespace: namespace,
		ID:        id,
		Vector:    embedResp.Embedding,
		Metadata:  meta,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, map[string]interface{}{
		"id":        id,
		"namespace": namespace,
		"reindexed": upsertResp.Success,
		"version":   upsertResp.Version,
	})
}

// ragRouteSpecs contributes the RAG surface to the route table (routes.go).
// It exists in both rag build-tag variants so the table always matches the
// compiled handler set: this variant serves the real handlers.
func ragRouteSpecs() []RouteSpec {
	return []RouteSpec{
		{Method: "POST", Pattern: "/api/v1/rag/context", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRAGBuildContext }},
		{Method: "GET", Pattern: "/api/v1/rag/documents", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRAGListDocuments }},
		{Method: "DELETE", Pattern: "/api/v1/rag/documents/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRAGDeleteDocument }},
		{Method: "GET", Pattern: "/api/v1/rag/documents/{id}", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRAGGetDocument }},
		{Method: "POST", Pattern: "/api/v1/rag/documents/{id}/reindex", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRAGReindexDocument }},
		{Method: "POST", Pattern: "/api/v1/rag/query", Group: GroupAPI, Handler: func(s *Server) http.HandlerFunc { return s.HandleRAGQuery }},
	}
}
