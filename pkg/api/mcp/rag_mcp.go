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
	"strconv"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/rag/ragservice"
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
	req := ragservice.QueryRequest{IncludeCitations: true}
	req.Query, _ = args["query"].(string)
	req.Namespace, _ = args["namespace"].(string)
	if v, ok := args["top_k"].(float64); ok {
		req.TopK = int(v)
	}
	if v, ok := args["max_tokens"].(float64); ok {
		req.MaxTokens = int(v)
	}
	if v, ok := args["min_score"].(string); ok {
		if f, err := strconv.ParseFloat(v, 32); err == nil {
			req.MinScore = float32(f)
			req.MinScoreSet = true
		}
	}
	req.Template, _ = args["template"].(string)
	if v, ok := args["include_citations"].(string); ok {
		req.IncludeCitations = v == "true"
	}
	req.Model, _ = args["model"].(string)

	// The one RAG query every transport serves (#1611).
	out, err := ragservice.Query(ctx, r.service, req)
	if err != nil {
		return nil, err
	}
	return mcpRAGResponse(out), nil
}

// mcpRAGResponse renders the RAG service's response as the MCP tool result.
func mcpRAGResponse(out *ragservice.Response) map[string]interface{} {
	response := map[string]interface{}{
		"context":      out.Context,
		"tokens":       out.Tokens,
		"truncated":    out.Truncated,
		"result_count": out.ResultCount,
	}
	if len(out.Citations) > 0 {
		citations := make([]map[string]interface{}, len(out.Citations))
		for i, c := range out.Citations {
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
	return response
}

// handleRAGBuildContext handles the quicbaas_rag_build_context tool.
// This tool assembles context from user-provided results.
func (r *ToolRegistry) handleRAGBuildContext(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	resultsJSON, ok := args["results"].(string)
	if !ok || resultsJSON == "" {
		return nil, api.NewRequiredFieldError("results")
	}
	var inputResults []struct {
		Content  string            `json:"content"`
		Score    float32           `json:"score"`
		Metadata map[string]string `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(resultsJSON), &inputResults); err != nil {
		return nil, api.NewInvalidInputCauseError("invalid results JSON", err)
	}

	req := ragservice.BuildContextRequest{IncludeCitations: true}
	req.Results = make([]ragservice.ResultInput, len(inputResults))
	for i, in := range inputResults {
		req.Results[i] = ragservice.ResultInput{Content: in.Content, Score: in.Score, Metadata: in.Metadata}
	}
	if v, ok := args["max_tokens"].(float64); ok {
		req.MaxTokens = int(v)
	}
	req.Template, _ = args["template"].(string)
	if v, ok := args["include_citations"].(string); ok {
		req.IncludeCitations = v == "true"
	}
	req.TruncationStrategy, _ = args["truncation_strategy"].(string)

	// The one context assembly every transport serves (#1611).
	out, err := ragservice.BuildContext(ctx, req)
	if err != nil {
		return nil, err
	}
	return mcpRAGResponse(out), nil
}

// Helper functions

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
