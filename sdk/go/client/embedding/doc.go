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

// Package embedding provides the embedding generation client for the QuicBaaS SDK.
//
// The embedding client supports generating semantic embeddings using multiple
// providers (Voyage AI, OpenAI, Ollama, LM Studio) with intelligent caching,
// batch processing, and provider fallback capabilities.
//
// # Generating Single Embeddings
//
//	client := embedding.NewClient(transport)
//
//	// Generate an embedding for text
//	resp, err := client.GenerateEmbedding(ctx, &embedding.GenerateEmbeddingRequest{
//	    Text:      "Introduction to machine learning",
//	    Provider:  embedding.ProviderVoyage,
//	    InputType: embedding.InputTypeDocument,
//	    Metadata: map[string]string{
//	        "source": "tutorial.md",
//	    },
//	})
//	fmt.Printf("Embedding dimensions: %d\n", len(resp.Embedding))
//
// # Batch Embedding Generation
//
//	// Generate embeddings for multiple texts efficiently
//	resp, err := client.BatchGenerateEmbeddings(ctx, &embedding.BatchGenerateEmbeddingsRequest{
//	    Items: []*embedding.BatchEmbeddingItem{
//	        {ID: "doc-1", Text: "First document"},
//	        {ID: "doc-2", Text: "Second document"},
//	    },
//	    Provider: embedding.ProviderOpenAI,
//	})
//	for _, result := range resp.Results {
//	    fmt.Printf("ID: %s, Dimensions: %d\n", result.ID, len(result.Embedding))
//	}
//
// # Provider Management
//
//	// List available providers
//	providers, err := client.ListProviders(ctx, &embedding.ListProvidersRequest{})
//	for _, p := range providers.Providers {
//	    fmt.Printf("Provider: %s, Model: %s, Dimensions: %d\n",
//	        p.Name, p.Model, p.Dimensions)
//	}
//
//	// Set default provider
//	_, err = client.SetDefaultProvider(ctx, &embedding.SetDefaultProviderRequest{
//	    Provider: embedding.ProviderOllama,
//	})
//
// # Cache Management
//
//	// Get cache statistics
//	stats, err := client.GetCacheStats(ctx, &embedding.GetEmbeddingCacheStatsRequest{})
//	fmt.Printf("Cache hit rate: %.2f%%\n", stats.HitRate*100)
//
//	// Clear cache for a specific provider
//	_, err = client.ClearCache(ctx, &embedding.ClearEmbeddingCacheRequest{
//	    Provider: embedding.ProviderVoyage,
//	})
//
// # Supported Providers
//
// The embedding client supports the following providers:
//   - Voyage AI (ProviderVoyage): High-quality embeddings for semantic search
//   - OpenAI (ProviderOpenAI): OpenAI's text embedding models
//   - Ollama (ProviderOllama): Local embedding models via Ollama
//   - LM Studio (ProviderLMStudio): Local embedding models via LM Studio
//
// # Input Types
//
// Specify the input type to optimize embedding quality:
//   - InputTypeDocument: Long-form document text
//   - InputTypeQuery: Search query text
//   - InputTypeCode: Source code
package embedding
