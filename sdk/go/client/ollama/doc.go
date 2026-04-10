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

// Package ollama provides the Ollama embedding service client for the QuicBaaS SDK.
//
// The ollama client supports generating text embeddings using Ollama models
// for AI/ML applications, semantic search, and RAG (Retrieval-Augmented Generation).
//
// # Generating Embeddings
//
//	client := ollama.NewClient(transport)
//
//	// Generate embedding for a single text
//	resp, err := client.Embed(ctx, &ollama.EmbedRequest{
//	    Text: "Hello, world!",
//	})
//	// resp.Embedding contains the vector
//
// # Using Specific Models
//
//	// Generate embedding with a specific model
//	resp, err := client.EmbedWithModel(ctx, &ollama.EmbedWithModelRequest{
//	    Text:  "Hello, world!",
//	    Model: "nomic-embed-text",
//	})
//
// # Batch Embedding
//
//	// Generate embeddings for multiple texts
//	resp, err := client.EmbedBatch(ctx, &ollama.EmbedBatchRequest{
//	    Texts: []string{"Hello", "World", "Test"},
//	    Model: "nomic-embed-text", // optional, uses default if empty
//	})
//	for i, embedding := range resp.Embeddings {
//	    fmt.Printf("Text %d: %d dimensions\n", i, len(embedding))
//	}
//
// # Model Management
//
//	// List available models
//	models, err := client.ListModels(ctx, &ollama.ListModelsRequest{})
//	for _, model := range models.Models {
//	    fmt.Printf("Model: %s, Size: %d\n", model.Name, model.Size)
//	}
//
//	// Get detailed model information
//	info, err := client.GetModelInfo(ctx, &ollama.GetModelInfoRequest{
//	    Model: "nomic-embed-text",
//	})
//
// # Health Checking
//
//	// Check Ollama service health
//	health, err := client.Health(ctx, &ollama.HealthRequest{})
//	if health.Available {
//	    fmt.Printf("Ollama version: %s\n", health.Version)
//	}
//
// # Client Information
//
//	// Check if service is available
//	if client.IsAvailable() {
//	    // Service is ready
//	}
//
//	// Get default model name
//	model := client.DefaultModel()
//
//	// Get base URL
//	url := client.BaseURL()
package ollama
