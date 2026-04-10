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
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	ragctx "github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ========================================
// RAG Route Registration
// ========================================

// registerRAGRoutes registers all RAG-related routes.
func registerRAGRoutes(s *Server, r chi.Router) {
	r.Route("/rag", func(r chi.Router) {
		// All-in-one RAG query
		r.Post("/query", s.HandleRAGQuery)

		// Context building from user-provided results
		r.Post("/context", s.HandleRAGBuildContext)
	})
}

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
	ctx := r.Context()

	// Parse request body
	var req RAGQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Query == "" {
		RespondBadRequest(w, r, "query is required")
		return
	}
	if req.Namespace == "" {
		RespondBadRequest(w, r, "namespace is required")
		return
	}

	// Set defaults
	topK := req.TopK
	if topK <= 0 {
		topK = 10
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	minScore := req.MinScore
	if minScore <= 0 {
		minScore = 0.7
	}
	template := parseTemplate(req.Template)
	includeCitations := req.IncludeCitations

	// Step 1: Generate embedding for the query
	ollamaSvc := s.service.Ollama()
	if ollamaSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "ollama service not configured",
		})
		return
	}

	var embedding []float32
	var err error

	if req.Model != "" {
		resp, embedErr := ollamaSvc.EmbedWithModel(ctx, &service.OllamaEmbedWithModelRequest{
			Text:  req.Query,
			Model: req.Model,
		})
		if embedErr != nil {
			s.handleServiceError(w, r, embedErr)
			return
		}
		embedding = resp.Embedding
	} else {
		resp, embedErr := ollamaSvc.Embed(ctx, &service.OllamaEmbedRequest{
			Text: req.Query,
		})
		if embedErr != nil {
			s.handleServiceError(w, r, embedErr)
			return
		}
		embedding = resp.Embedding
	}

	// Step 2: Search for similar vectors
	vectorSvc := s.service.Vector()
	if vectorSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "vector service not configured",
		})
		return
	}

	searchResp, err := vectorSvc.SearchSimilar(ctx, &service.SearchSimilarRequest{
		Namespace:       req.Namespace,
		QueryVector:     embedding,
		K:               topK,
		MinScore:        minScore,
		IncludeMetadata: true,
		IncludeVectors:  false,
	})
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Step 3: Build context from search results
	results := make([]ragctx.Result, len(searchResp.Results))
	for i, result := range searchResp.Results {
		results[i] = ragctx.Result{
			ID:          fmt.Sprintf("%d", result.ID),
			Content:     getStringFromInterfaceMap(result.Metadata, "content"),
			VectorScore: result.Score,
			FusionScore: result.Score,
			Metadata: ragctx.Metadata{
				FilePath:  getStringFromInterfaceMap(result.Metadata, "file_path"),
				FileName:  getStringFromInterfaceMap(result.Metadata, "file_name"),
				Language:  getStringFromInterfaceMap(result.Metadata, "language"),
				StartLine: getIntFromInterfaceMap(result.Metadata, "start_line"),
				EndLine:   getIntFromInterfaceMap(result.Metadata, "end_line"),
			},
		}
	}

	// Build context
	config := ragctx.Config{
		DefaultMaxTokens:   maxTokens,
		DefaultTemplate:    template,
		IncludeCitations:   includeCitations,
		TruncationStrategy: ragctx.TruncationSmart,
		MaxResultsPerChunk: 20,
		TokenMultiplier:    1.3,
	}
	builder := ragctx.NewBuilderWithConfig(config)

	builtContext, err := builder.BuildWithTemplate(results, template, maxTokens)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := RAGQueryResponse{
		Context:     builtContext.Content,
		Tokens:      builtContext.Tokens,
		Truncated:   builtContext.Truncated,
		ResultCount: len(results),
	}

	if includeCitations && len(builtContext.Citations) > 0 {
		citations := make([]RAGCitation, len(builtContext.Citations))
		for i, c := range builtContext.Citations {
			citations[i] = RAGCitation{
				Index:     c.Index,
				FilePath:  c.FilePath,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				Score:     c.Score,
			}
		}
		resp.Citations = citations
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// RAG Build Context Handler
// ========================================

// HandleRAGBuildContext builds context from user-provided results.
func (s *Server) HandleRAGBuildContext(w http.ResponseWriter, r *http.Request) {
	// Parse request body
	var req RAGBuildContextRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if len(req.Results) == 0 {
		RespondBadRequest(w, r, "results is required and cannot be empty")
		return
	}

	// Set defaults
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	template := parseTemplate(req.Template)
	truncationStrategy := parseTruncationStrategy(req.TruncationStrategy)
	includeCitations := req.IncludeCitations

	// Convert to RAG results
	results := make([]ragctx.Result, len(req.Results))
	for i, result := range req.Results {
		results[i] = ragctx.Result{
			ID:          fmt.Sprintf("result_%d", i),
			Content:     result.Content,
			FusionScore: result.Score,
			Metadata: ragctx.Metadata{
				FilePath:  result.Metadata["file_path"],
				FileName:  result.Metadata["file_name"],
				Language:  result.Metadata["language"],
				StartLine: getIntFromStringMetadata(result.Metadata, "start_line"),
				EndLine:   getIntFromStringMetadata(result.Metadata, "end_line"),
			},
		}
	}

	// Build context
	config := ragctx.Config{
		DefaultMaxTokens:   maxTokens,
		DefaultTemplate:    template,
		IncludeCitations:   includeCitations,
		TruncationStrategy: truncationStrategy,
		MaxResultsPerChunk: 20,
		TokenMultiplier:    1.3,
	}
	builder := ragctx.NewBuilderWithConfig(config)

	builtContext, err := builder.BuildWithTemplate(results, template, maxTokens)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := RAGBuildContextResponse{
		Context:     builtContext.Content,
		Tokens:      builtContext.Tokens,
		Truncated:   builtContext.Truncated,
		ResultCount: len(results),
	}

	if includeCitations && len(builtContext.Citations) > 0 {
		citations := make([]RAGCitation, len(builtContext.Citations))
		for i, c := range builtContext.Citations {
			citations[i] = RAGCitation{
				Index:     c.Index,
				FilePath:  c.FilePath,
				StartLine: c.StartLine,
				EndLine:   c.EndLine,
				Score:     c.Score,
			}
		}
		resp.Citations = citations
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Helper Functions
// ========================================

func parseTemplate(s string) ragctx.Template {
	switch s {
	case "json":
		return ragctx.TemplateJSON
	case "xml":
		return ragctx.TemplateXML
	case "plain":
		return ragctx.TemplatePlain
	default:
		return ragctx.TemplateMarkdown
	}
}

func parseTruncationStrategy(s string) ragctx.TruncationStrategy {
	switch s {
	case "hard":
		return ragctx.TruncationHard
	case "none":
		return ragctx.TruncationNone
	default:
		return ragctx.TruncationSmart
	}
}

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

func getIntFromStringMetadata(m map[string]string, key string) int {
	if m == nil {
		return 0
	}
	if v, ok := m[key]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}
