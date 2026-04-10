//go:build vector

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

package embedding_test

import (
	"context"
	"fmt"
	"log"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

// Example demonstrates using OpenAIProvider with OpenAI API
func ExampleNewOpenAIProvider_openai() {
	config := embedding.Config{
		Provider:   "openai",
		APIURL:     "https://api.openai.com",
		APIKey:     "sk-your-api-key-here",
		Model:      embedding.ModelTextEmbedding3Small,
		Dimensions: 1536,
		Timeout:    30,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	if err != nil {
		log.Fatalf("Failed to create OpenAI provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	text := "This is a sample text for embedding"

	emb, err := provider.Embed(ctx, text)
	if err != nil {
		log.Fatalf("Failed to generate embedding: %v", err)
	}

	fmt.Printf("Generated embedding with %d dimensions\n", len(emb))
	fmt.Printf("Provider: %s\n", provider.Name())
	fmt.Printf("Max tokens: %d\n", provider.MaxTokens())
}

// Example demonstrates using OpenAIProvider with Ollama (OpenAI-compatible API)
func ExampleNewOpenAIProvider_ollama() {
	config := embedding.Config{
		Provider: "openai",
		APIURL:   "http://localhost:11434",
		APIKey:   "", // No API key needed for Ollama
		Model:    "nomic-embed-text",
		Timeout:  30,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	if err != nil {
		log.Fatalf("Failed to create Ollama provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	// Batch embedding example
	texts := []string{
		"First document",
		"Second document",
		"Third document",
	}

	embeddings, err := provider.EmbedBatch(ctx, texts)
	if err != nil {
		log.Fatalf("Failed to generate batch embeddings: %v", err)
	}

	fmt.Printf("Generated %d embeddings\n", len(embeddings))
	fmt.Printf("Each embedding has %d dimensions\n", provider.Dimensions())
}

// Example demonstrates using OpenAIProvider with LMStudio
func ExampleNewOpenAIProvider_lmstudio() {
	config := embedding.Config{
		Provider: "openai",
		APIURL:   "http://localhost:1234",
		APIKey:   "", // API key is optional for LMStudio
		Model:    "nomic-ai/nomic-embed-text-v1.5-GGUF",
		Timeout:  30,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	if err != nil {
		log.Fatalf("Failed to create LMStudio provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	text := "Sample text for LMStudio embedding"

	emb, err := provider.Embed(ctx, text)
	if err != nil {
		log.Fatalf("Failed to generate embedding: %v", err)
	}

	fmt.Printf("Generated embedding with %d dimensions using LMStudio\n", len(emb))
}

// Example demonstrates auto-detection of embedding dimensions
func ExampleOpenAIProvider_autoDimensions() {
	config := embedding.Config{
		Provider:   "openai",
		APIURL:     "http://localhost:11434",
		Model:      "custom-model",
		Dimensions: 0, // Auto-detect dimensions
		Timeout:    30,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Dimensions will be auto-detected on first embedding
	fmt.Printf("Initial dimensions: %d (will auto-detect)\n", provider.Dimensions())

	ctx := context.Background()
	text := "Sample text"

	emb, err := provider.Embed(ctx, text)
	if err != nil {
		log.Fatalf("Failed to generate embedding: %v", err)
	}

	fmt.Printf("Auto-detected dimensions: %d\n", len(emb))
	fmt.Printf("Provider dimensions: %d\n", provider.Dimensions())
}

// Example demonstrates retry behavior with exponential backoff
func ExampleOpenAIProvider_retryLogic() {
	config := embedding.Config{
		Provider:       "openai",
		APIURL:         "https://api.openai.com",
		APIKey:         "sk-your-api-key-here",
		Model:          embedding.ModelTextEmbedding3Small,
		MaxRetries:     3,
		RetryBackoffMS: 1000, // 1s, 2s, 4s backoff
		Timeout:        30,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	text := "Sample text"

	// Provider will automatically retry on rate limits or server errors
	emb, err := provider.Embed(ctx, text)
	if err != nil {
		log.Fatalf("Failed after retries: %v", err)
	}

	fmt.Printf("Successfully generated embedding with %d dimensions\n", len(emb))
}

// Example demonstrates using different OpenAI models
func ExampleOpenAIProvider_models() {
	models := []struct {
		name       string
		dimensions int
		maxTokens  int
	}{
		{embedding.ModelTextEmbedding3Small, 1536, 8191},
		{embedding.ModelTextEmbedding3Large, 3072, 8191},
		{embedding.ModelTextEmbeddingAda002, 1536, 8191},
	}

	for _, model := range models {
		config := embedding.Config{
			Provider:   "openai",
			APIURL:     "https://api.openai.com",
			APIKey:     "sk-your-api-key-here",
			Model:      model.name,
			Dimensions: model.dimensions,
			Timeout:    30,
		}

		provider, err := embedding.NewOpenAIProvider(config)
		if err != nil {
			log.Fatalf("Failed to create provider for model %s: %v", model.name, err)
		}

		fmt.Printf("Model: %s, Dimensions: %d, MaxTokens: %d\n",
			model.name, provider.Dimensions(), provider.MaxTokens())

		provider.Close()
	}
}

// Example demonstrates context cancellation
func ExampleOpenAIProvider_contextCancellation() {
	config := embedding.Config{
		Provider: "openai",
		APIURL:   "http://localhost:11434",
		Model:    "nomic-embed-text",
		Timeout:  30,
	}

	provider, err := embedding.NewOpenAIProvider(config)
	if err != nil {
		log.Fatalf("Failed to create provider: %v", err)
	}
	defer provider.Close()

	// Create cancellable context
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after some time or condition
	go func() {
		// Simulate cancellation
		cancel()
	}()

	text := "Sample text"
	_, err = provider.Embed(ctx, text)
	if err != nil {
		fmt.Printf("Embedding cancelled: %v\n", err)
	}
}
