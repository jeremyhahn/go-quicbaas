//go:build quic && rag

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

package quic

import (
	"context"
	"fmt"
	"strconv"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	ragctx "github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// RAG Request/Response Types
// =============================================================================

// RAGQueryRequest is the request body for RAG query operations.
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

// RAGQueryResponse is the response for RAG query operations.
type RAGQueryResponse struct {
	Context     string        `json:"context"`
	Tokens      int           `json:"tokens"`
	Truncated   bool          `json:"truncated"`
	ResultCount int           `json:"result_count"`
	Citations   []RAGCitation `json:"citations,omitempty"`
}

// RAGCitation represents a source citation in RAG responses.
type RAGCitation struct {
	Index     int     `json:"index"`
	FilePath  string  `json:"file_path"`
	StartLine int     `json:"start_line"`
	EndLine   int     `json:"end_line"`
	Score     float32 `json:"score"`
}

// RAGBuildContextRequest is the request body for building context from results.
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

// RAGBuildContextResponse is the response for context building operations.
type RAGBuildContextResponse struct {
	Context     string        `json:"context"`
	Tokens      int           `json:"tokens"`
	Truncated   bool          `json:"truncated"`
	ResultCount int           `json:"result_count"`
	Citations   []RAGCitation `json:"citations,omitempty"`
}

// =============================================================================
// RAG Operations Handlers
// =============================================================================

// handleRAGQuery handles RAG query requests.
// This orchestrates: embedding generation -> vector search -> context building.
func (h *Handler) handleRAGQuery(ctx context.Context, req *Request) *Response {
	// Check Ollama service availability
	ollamaService := h.service.Ollama()
	if ollamaService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Ollama"))
	}

	// Check Vector service availability
	vectorService := h.service.Vector()
	if vectorService == nil {
		return h.errorResponse(api.NewServiceUnavailableError("Vector"))
	}

	// Decode request
	var ragReq RAGQueryRequest
	if err := DecodePayload(req.Payload, &ragReq); err != nil {
		return h.errorResponse(err)
	}

	// Validate required fields
	if ragReq.Query == "" {
		return h.errorResponse(api.NewRequiredFieldError("query"))
	}
	if ragReq.Namespace == "" {
		return h.errorResponse(api.NewRequiredFieldError("namespace"))
	}

	// Apply defaults
	topK := ragReq.TopK
	if topK <= 0 {
		topK = 10
	}
	maxTokens := ragReq.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	minScore := ragReq.MinScore
	if minScore <= 0 {
		minScore = 0.7
	}
	template := parseTemplate(ragReq.Template)

	// Step 1: Generate embedding for the query
	var embedding []float32
	if ragReq.Model != "" {
		resp, err := ollamaService.EmbedWithModel(ctx, &service.OllamaEmbedWithModelRequest{
			Text:  ragReq.Query,
			Model: ragReq.Model,
		})
		if err != nil {
			return h.errorResponse(err)
		}
		embedding = resp.Embedding
	} else {
		resp, err := ollamaService.Embed(ctx, &service.OllamaEmbedRequest{
			Text: ragReq.Query,
		})
		if err != nil {
			return h.errorResponse(err)
		}
		embedding = resp.Embedding
	}

	// Step 2: Search for similar vectors
	searchResp, err := vectorService.SearchSimilar(ctx, &service.SearchSimilarRequest{
		Namespace:       ragReq.Namespace,
		QueryVector:     embedding,
		K:               topK,
		MinScore:        minScore,
		IncludeMetadata: true,
		IncludeVectors:  false,
	})
	if err != nil {
		return h.errorResponse(err)
	}

	// Step 3: Convert search results to RAG results
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

	// Step 4: Build context using RAG context builder
	config := ragctx.Config{
		DefaultMaxTokens:   maxTokens,
		DefaultTemplate:    template,
		IncludeCitations:   ragReq.IncludeCitations,
		TruncationStrategy: ragctx.TruncationSmart,
		MaxResultsPerChunk: 20,
		TokenMultiplier:    1.3,
	}
	builder := ragctx.NewBuilderWithConfig(config)

	builtContext, err := builder.BuildWithTemplate(results, template, maxTokens)
	if err != nil {
		return h.errorResponse(err)
	}

	// Build response
	response := RAGQueryResponse{
		Context:     builtContext.Content,
		Tokens:      builtContext.Tokens,
		Truncated:   builtContext.Truncated,
		ResultCount: len(results),
	}

	if ragReq.IncludeCitations && len(builtContext.Citations) > 0 {
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
		response.Citations = citations
	}

	payload, err := EncodePayload(response)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// handleRAGBuildContext handles RAG context building from user-provided results.
func (h *Handler) handleRAGBuildContext(ctx context.Context, req *Request) *Response {
	// Decode request
	var buildReq RAGBuildContextRequest
	if err := DecodePayload(req.Payload, &buildReq); err != nil {
		return h.errorResponse(err)
	}

	// Validate required fields
	if len(buildReq.Results) == 0 {
		return h.errorResponse(api.NewEmptyCollectionError("results"))
	}

	// Apply defaults
	maxTokens := buildReq.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 4000
	}
	template := parseTemplate(buildReq.Template)
	truncationStrategy := parseTruncationStrategy(buildReq.TruncationStrategy)

	// Convert input results to RAG results
	results := make([]ragctx.Result, len(buildReq.Results))
	for i, result := range buildReq.Results {
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

	// Build context using RAG context builder
	config := ragctx.Config{
		DefaultMaxTokens:   maxTokens,
		DefaultTemplate:    template,
		IncludeCitations:   buildReq.IncludeCitations,
		TruncationStrategy: truncationStrategy,
		MaxResultsPerChunk: 20,
		TokenMultiplier:    1.3,
	}
	builder := ragctx.NewBuilderWithConfig(config)

	builtContext, err := builder.BuildWithTemplate(results, template, maxTokens)
	if err != nil {
		return h.errorResponse(err)
	}

	// Build response
	response := RAGBuildContextResponse{
		Context:     builtContext.Content,
		Tokens:      builtContext.Tokens,
		Truncated:   builtContext.Truncated,
		ResultCount: len(results),
	}

	if buildReq.IncludeCitations && len(builtContext.Citations) > 0 {
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
		response.Citations = citations
	}

	payload, err := EncodePayload(response)
	if err != nil {
		return h.errorResponse(err)
	}

	return &Response{Status: StatusOK, Payload: payload}
}

// =============================================================================
// Helper Functions
// =============================================================================

// parseTemplate converts a string template name to a ragctx.Template.
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

// parseTruncationStrategy converts a string strategy name to a ragctx.TruncationStrategy.
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

// getStringFromInterfaceMap extracts a string value from a map[string]interface{}.
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

// getIntFromInterfaceMap extracts an int value from a map[string]interface{}.
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

// getIntFromStringMetadata extracts an int value from a map[string]string.
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
