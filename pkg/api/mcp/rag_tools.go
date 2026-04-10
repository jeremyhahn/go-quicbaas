//go:build mcp

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

// Package mcp implements RAG tool definitions for the Model Context Protocol.
package mcp

// RAG tool definitions for context building and query operations (2 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (
	// RAGQueryTool performs an all-in-one RAG query: embed → search → assemble context.
	// This is the recommended tool for most RAG use cases.
	RAGQueryTool = Tool{
		Name:        "quicbaas_rag_query",
		Description: "All-in-one RAG query: generates embedding, searches vectors, and assembles context for LLM consumption",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"query": {
					Type:        "string",
					Description: "The query text to search for",
				},
				"namespace": {
					Type:        "string",
					Description: "Vector namespace to search in",
				},
				"top_k": {
					Type:        "integer",
					Description: "Number of results to return (default: 10)",
				},
				"max_tokens": {
					Type:        "integer",
					Description: "Maximum tokens for assembled context (default: 4000)",
				},
				"min_score": {
					Type:        "string",
					Description: "Minimum similarity score 0.0-1.0 (default: 0.7)",
				},
				"template": {
					Type:        "string",
					Enum:        []string{"markdown", "json", "xml", "plain"},
					Description: "Output format template (default: markdown)",
				},
				"include_citations": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include source citations in context (default: true)",
				},
				"model": {
					Type:        "string",
					Description: "Embedding model to use (uses default if not specified)",
				},
			},
			Required: []string{"query", "namespace"},
		},
	}

	// RAGBuildContextTool assembles context from user-provided chunks.
	// Use this for custom search workflows where you handle the search step.
	RAGBuildContextTool = Tool{
		Name:        "quicbaas_rag_build_context",
		Description: "Assemble LLM context from user-provided search results or chunks",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"results": {
					Type:        "string",
					Description: "JSON array of result objects with content, metadata, and scores",
				},
				"max_tokens": {
					Type:        "integer",
					Description: "Maximum tokens for assembled context (default: 4000)",
				},
				"template": {
					Type:        "string",
					Enum:        []string{"markdown", "json", "xml", "plain"},
					Description: "Output format template (default: markdown)",
				},
				"include_citations": {
					Type:        "string",
					Enum:        []string{"true", "false"},
					Description: "Include source citations in context (default: true)",
				},
				"truncation_strategy": {
					Type:        "string",
					Enum:        []string{"smart", "hard", "none"},
					Description: "How to handle context overflow (default: smart)",
				},
			},
			Required: []string{"results"},
		},
	}
)

// getAllRAGTools returns all RAG tools (2 tools).
func getAllRAGTools() []Tool {
	return []Tool{
		RAGQueryTool,
		RAGBuildContextTool,
	}
}
