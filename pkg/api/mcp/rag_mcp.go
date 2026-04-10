//go:build mcp && rag

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

// Package mcp implements RAG MCP integration handlers.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	ragctx "github.com/jeremyhahn/go-quicbaas/pkg/rag/context"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// registerRAGTools registers all RAG tool handlers on the ToolRegistry.
func (r *ToolRegistry) registerRAGTools() {
	ragTools := getAllRAGTools()
	for _, tool := range ragTools {
		r.addTool(ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: convertInputSchema(tool.InputSchema),
		})
	}
}

// executeRAGTool executes a RAG tool and returns the result.
func (r *ToolRegistry) executeRAGTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	switch name {
	case "quicbaas_rag_query":
		return r.handleRAGQuery(ctx, args)
	case "quicbaas_rag_build_context":
		return r.handleRAGBuildContext(ctx, args)
	default:
		return nil, api.NewUnknownToolError(name)
	}
}

// handleRAGQuery handles the quicbaas_rag_query tool.
// This is the all-in-one RAG tool: embed -> search -> assemble context.
func (r *ToolRegistry) handleRAGQuery(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return nil, api.NewRequiredFieldError("query")
	}

	namespace, ok := args["namespace"].(string)
	if !ok || namespace == "" {
		return nil, api.NewRequiredFieldError("namespace")
	}

	// Get optional parameters
	topK := 10
	if v, ok := args["top_k"].(float64); ok {
		topK = int(v)
	}

	maxTokens := 4000
	if v, ok := args["max_tokens"].(float64); ok {
		maxTokens = int(v)
	}

	minScore := float32(0.7)
	if v, ok := args["min_score"].(string); ok {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			minScore = float32(f)
		}
	}

	template := ragctx.TemplateMarkdown
	if v, ok := args["template"].(string); ok {
		switch v {
		case "json":
			template = ragctx.TemplateJSON
		case "xml":
			template = ragctx.TemplateXML
		case "plain":
			template = ragctx.TemplatePlain
		}
	}

	includeCitations := true
	if v, ok := args["include_citations"].(string); ok {
		includeCitations = v == "true"
	}

	model := ""
	if v, ok := args["model"].(string); ok {
		model = v
	}

	// Step 1: Generate embedding for the query
	ollamaService := r.service.Ollama()
	if ollamaService == nil {
		return nil, api.NewServiceNotConfiguredError("ollama")
	}

	var embedding []float32
	var err error

	if model != "" {
		resp, err := ollamaService.EmbedWithModel(ctx, &service.OllamaEmbedWithModelRequest{
			Text:  query,
			Model: model,
		})
		if err != nil {
			return nil, api.NewOperationFailedError("failed to generate query embedding", err)
		}
		embedding = resp.Embedding
	} else {
		resp, err := ollamaService.Embed(ctx, &service.OllamaEmbedRequest{
			Text: query,
		})
		if err != nil {
			return nil, api.NewOperationFailedError("failed to generate query embedding", err)
		}
		embedding = resp.Embedding
	}

	// Step 2: Search for similar vectors
	vectorService := r.service.Vector()
	if vectorService == nil {
		return nil, api.NewServiceNotConfiguredError("vector")
	}

	searchResp, err := vectorService.SearchSimilar(ctx, &service.SearchSimilarRequest{
		Namespace:       namespace,
		QueryVector:     embedding,
		K:               topK,
		MinScore:        minScore,
		IncludeMetadata: true,
		IncludeVectors:  false,
	})
	if err != nil {
		return nil, api.NewOperationFailedError("vector search", err)
	}

	// Step 3: Build context from search results
	results := make([]ragctx.Result, len(searchResp.Results))
	for i, r := range searchResp.Results {
		results[i] = ragctx.Result{
			ID:          fmt.Sprintf("%d", r.ID),
			Content:     getStringFromInterfaceMetadata(r.Metadata, "content"),
			VectorScore: r.Score,
			FusionScore: r.Score, // Use vector score as fusion score
			Metadata: ragctx.Metadata{
				FilePath:  getStringFromInterfaceMetadata(r.Metadata, "file_path"),
				FileName:  getStringFromInterfaceMetadata(r.Metadata, "file_name"),
				Language:  getStringFromInterfaceMetadata(r.Metadata, "language"),
				StartLine: getIntFromInterfaceMetadata(r.Metadata, "start_line"),
				EndLine:   getIntFromInterfaceMetadata(r.Metadata, "end_line"),
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
		return nil, api.NewOperationFailedError("context building", err)
	}

	// Build response
	response := map[string]interface{}{
		"context":      builtContext.Content,
		"tokens":       builtContext.Tokens,
		"truncated":    builtContext.Truncated,
		"result_count": len(results),
	}

	if includeCitations && len(builtContext.Citations) > 0 {
		citations := make([]map[string]interface{}, len(builtContext.Citations))
		for i, c := range builtContext.Citations {
			citations[i] = map[string]interface{}{
				"index":      c.Index,
				"file_path":  c.FilePath,
				"start_line": c.StartLine,
				"end_line":   c.EndLine,
				"score":      c.Score,
			}
		}
		response["citations"] = citations
	}

	return response, nil
}

// handleRAGBuildContext handles the quicbaas_rag_build_context tool.
// This tool assembles context from user-provided results.
func (r *ToolRegistry) handleRAGBuildContext(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	resultsJSON, ok := args["results"].(string)
	if !ok || resultsJSON == "" {
		return nil, api.NewRequiredFieldError("results")
	}

	// Parse results JSON
	var inputResults []struct {
		Content  string            `json:"content"`
		Score    float32           `json:"score"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(resultsJSON), &inputResults); err != nil {
		return nil, api.NewInvalidInputCauseError("invalid results JSON", err)
	}

	if len(inputResults) == 0 {
		return nil, api.NewEmptyCollectionError("results array")
	}

	// Get optional parameters
	maxTokens := 4000
	if v, ok := args["max_tokens"].(float64); ok {
		maxTokens = int(v)
	}

	template := ragctx.TemplateMarkdown
	if v, ok := args["template"].(string); ok {
		switch v {
		case "json":
			template = ragctx.TemplateJSON
		case "xml":
			template = ragctx.TemplateXML
		case "plain":
			template = ragctx.TemplatePlain
		}
	}

	includeCitations := true
	if v, ok := args["include_citations"].(string); ok {
		includeCitations = v == "true"
	}

	truncationStrategy := ragctx.TruncationSmart
	if v, ok := args["truncation_strategy"].(string); ok {
		switch v {
		case "hard":
			truncationStrategy = ragctx.TruncationHard
		case "none":
			truncationStrategy = ragctx.TruncationNone
		}
	}

	// Convert to RAG results
	results := make([]ragctx.Result, len(inputResults))
	for i, r := range inputResults {
		results[i] = ragctx.Result{
			ID:          fmt.Sprintf("result_%d", i),
			Content:     r.Content,
			FusionScore: r.Score,
			Metadata: ragctx.Metadata{
				FilePath:  r.Metadata["file_path"],
				FileName:  r.Metadata["file_name"],
				Language:  r.Metadata["language"],
				StartLine: getIntFromStringMap(r.Metadata, "start_line"),
				EndLine:   getIntFromStringMap(r.Metadata, "end_line"),
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
		return nil, api.NewOperationFailedError("context building", err)
	}

	// Build response
	response := map[string]interface{}{
		"context":      builtContext.Content,
		"tokens":       builtContext.Tokens,
		"truncated":    builtContext.Truncated,
		"result_count": len(results),
	}

	if includeCitations && len(builtContext.Citations) > 0 {
		citations := make([]map[string]interface{}, len(builtContext.Citations))
		for i, c := range builtContext.Citations {
			citations[i] = map[string]interface{}{
				"index":      c.Index,
				"file_path":  c.FilePath,
				"start_line": c.StartLine,
				"end_line":   c.EndLine,
				"score":      c.Score,
			}
		}
		response["citations"] = citations
	}

	return response, nil
}

// Helper functions

func getStringFromMetadata(metadata map[string]string, key string) string {
	if metadata == nil {
		return ""
	}
	return metadata[key]
}

func getIntFromMetadata(metadata map[string]string, key string) int {
	if metadata == nil {
		return 0
	}
	if v, ok := metadata[key]; ok {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return 0
}

func getIntFromStringMap(m map[string]string, key string) int {
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

func getStringFromInterfaceMetadata(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	if v, ok := metadata[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getIntFromInterfaceMetadata(metadata map[string]interface{}, key string) int {
	if metadata == nil {
		return 0
	}
	if v, ok := metadata[key]; ok {
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

// isRAGTool returns true if the tool name is a RAG tool.
func isRAGTool(name string) bool {
	switch name {
	case "quicbaas_rag_query",
		"quicbaas_rag_build_context":
		return true
	default:
		return false
	}
}
