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

// Package mcp implements Embedding tool definitions for the Model Context Protocol.
package mcp

// Embedding tool definitions for semantic embedding operations (7 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (
	// EmbeddingGenerateTool generates an embedding for a single text.
	EmbeddingGenerateTool = Tool{
		Name:        "quicbaas_embedding_generate",
		Description: "Generate an embedding vector for a single text using the configured provider",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"text": {
					Type:        "string",
					Description: "The text to generate an embedding for",
				},
				"provider": {
					Type:        "string",
					Description: "Optional provider name (voyage, openai, ollama, lmstudio). Uses default if not specified",
				},
				"input_type": {
					Type:        "string",
					Description: "Type of input text: document, query, or code",
					Enum:        []string{"document", "query", "code"},
				},
				"source": {
					Type:        "string",
					Description: "Optional source identifier (file path, URL, etc.)",
				},
				"language": {
					Type:        "string",
					Description: "Optional programming or natural language",
				},
				"cache_bypass": {
					Type:        "string",
					Description: "Set to 'true' to bypass cache and force regeneration",
				},
			},
			Required: []string{"text"},
		},
	}

	// EmbeddingBatchTool generates embeddings for multiple texts.
	EmbeddingBatchTool = Tool{
		Name:        "quicbaas_embedding_batch",
		Description: "Generate embedding vectors for multiple texts in a single request",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"items": {
					Type:        "string",
					Description: "JSON array of items to embed. Each item should have 'id' and 'text' fields, with optional 'input_type', 'source', and 'language'",
				},
				"provider": {
					Type:        "string",
					Description: "Optional provider name (voyage, openai, ollama, lmstudio). Uses default if not specified",
				},
				"cache_bypass": {
					Type:        "string",
					Description: "Set to 'true' to bypass cache and force regeneration",
				},
			},
			Required: []string{"items"},
		},
	}

	// EmbeddingListProvidersTool lists all available embedding providers.
	EmbeddingListProvidersTool = Tool{
		Name:        "quicbaas_embedding_list_providers",
		Description: "List all registered embedding providers with their capabilities and status",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"include_unavailable": {
					Type:        "string",
					Description: "Set to 'true' to include providers that are currently unavailable",
				},
			},
			Required: []string{},
		},
	}

	// EmbeddingGetProviderTool gets detailed information about a specific provider.
	EmbeddingGetProviderTool = Tool{
		Name:        "quicbaas_embedding_get_provider",
		Description: "Get detailed information about a specific embedding provider",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"provider": {
					Type:        "string",
					Description: "Provider name to get information for (voyage, openai, ollama, lmstudio)",
				},
			},
			Required: []string{"provider"},
		},
	}

	// EmbeddingSetDefaultTool sets the default embedding provider.
	EmbeddingSetDefaultTool = Tool{
		Name:        "quicbaas_embedding_set_default",
		Description: "Set the default embedding provider to use for requests that don't specify a provider",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"provider": {
					Type:        "string",
					Description: "Provider name to set as default (voyage, openai, ollama, lmstudio)",
				},
			},
			Required: []string{"provider"},
		},
	}

	// EmbeddingCacheStatsTool gets embedding cache statistics.
	EmbeddingCacheStatsTool = Tool{
		Name:        "quicbaas_embedding_cache_stats",
		Description: "Get statistics about the embedding cache including hit rate, size, and memory usage",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}

	// EmbeddingCacheClearTool clears the embedding cache.
	EmbeddingCacheClearTool = Tool{
		Name:        "quicbaas_embedding_cache_clear",
		Description: "Clear embedding cache entries. Can clear all entries or only those for a specific provider",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"provider": {
					Type:        "string",
					Description: "Optional provider name to clear cache only for that provider",
				},
				"all": {
					Type:        "string",
					Description: "Set to 'true' to clear the entire cache regardless of provider",
				},
			},
			Required: []string{},
		},
	}
)

// getAllEmbeddingTools returns all Embedding tools (7 tools).
func getAllEmbeddingTools() []Tool {
	return []Tool{
		EmbeddingGenerateTool,
		EmbeddingBatchTool,
		EmbeddingListProvidersTool,
		EmbeddingGetProviderTool,
		EmbeddingSetDefaultTool,
		EmbeddingCacheStatsTool,
		EmbeddingCacheClearTool,
	}
}
