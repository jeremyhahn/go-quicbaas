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

// Package mcp implements Ollama tool definitions for the Model Context Protocol.
package mcp

// Ollama tool definitions for embedding and model operations (5 tools).
// Each tool follows the MCP specification with JSON schema validation.

var (
	// OllamaEmbedTool generates an embedding for a single text using the default model.
	OllamaEmbedTool = Tool{
		Name:        "quicbaas_ollama_embed",
		Description: "Generate an embedding vector for text using the default Ollama model",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"text": {
					Type:        "string",
					Description: "The text to generate an embedding for",
				},
				"model": {
					Type:        "string",
					Description: "Optional model name (uses default if not specified)",
				},
			},
			Required: []string{"text"},
		},
	}

	// OllamaEmbedBatchTool generates embeddings for multiple texts.
	OllamaEmbedBatchTool = Tool{
		Name:        "quicbaas_ollama_embed_batch",
		Description: "Generate embedding vectors for multiple texts in a single request",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"texts": {
					Type:        "string",
					Description: "JSON array of texts to generate embeddings for",
				},
				"model": {
					Type:        "string",
					Description: "Optional model name (uses default if not specified)",
				},
			},
			Required: []string{"texts"},
		},
	}

	// OllamaModelListTool lists all available models from Ollama.
	OllamaModelListTool = Tool{
		Name:        "quicbaas_ollama_model_list",
		Description: "List all available models from Ollama",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}

	// OllamaModelInfoTool gets detailed information about a specific model.
	OllamaModelInfoTool = Tool{
		Name:        "quicbaas_ollama_model_info",
		Description: "Get detailed information about a specific Ollama model",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"model": {
					Type:        "string",
					Description: "Name of the model to get information for",
				},
			},
			Required: []string{"model"},
		},
	}

	// OllamaHealthTool checks Ollama connectivity and availability.
	OllamaHealthTool = Tool{
		Name:        "quicbaas_ollama_health",
		Description: "Check Ollama server connectivity and availability",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
			Required:   []string{},
		},
	}
)

// getAllOllamaTools returns all Ollama tools (5 tools).
func getAllOllamaTools() []Tool {
	return []Tool{
		OllamaEmbedTool,
		OllamaEmbedBatchTool,
		OllamaModelListTool,
		OllamaModelInfoTool,
		OllamaHealthTool,
	}
}
