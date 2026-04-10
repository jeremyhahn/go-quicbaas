//go:build rag

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

package interfaces

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/ollama"
)

// Compile-time interface satisfaction checks.
var (
	_ OllamaClient           = (*ollama.Client)(nil)
	_ OllamaEmbeddingService = (*ollama.EmbeddingService)(nil)
	_ OllamaModelService     = (*ollama.ModelService)(nil)
	_ OllamaHealthChecker    = (*ollama.HealthChecker)(nil)
)

// OllamaClient provides HTTP communication with the Ollama API.
// This interface defines the minimal contract needed by dependent services.
type OllamaClient interface {
	// BaseURL returns the configured base URL.
	BaseURL() string

	// Close closes the client and releases resources.
	Close() error
}

// OllamaEmbeddingService generates embeddings using the Ollama API.
// This interface defines the embedding operations used by the service layer.
type OllamaEmbeddingService interface {
	// Model returns the configured embedding model.
	Model() string

	// SetModel changes the embedding model.
	SetModel(model string)

	// Embed generates an embedding for a single text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedWithModel generates an embedding for a single text using a specific model.
	EmbedWithModel(ctx context.Context, model, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// EmbedBatchWithModel generates embeddings for multiple texts using a specific model.
	EmbedBatchWithModel(ctx context.Context, model string, texts []string) ([][]float32, error)
}

// OllamaModelService provides model information from the Ollama API.
// This interface defines the model operations used by the service layer.
type OllamaModelService interface {
	// List returns all available models from Ollama.
	List(ctx context.Context) ([]ollama.ModelInfo, error)

	// Show returns detailed information about a model.
	Show(ctx context.Context, model string) (*ollama.ModelDetails, error)

	// IsAvailable checks if a model exists locally.
	IsAvailable(ctx context.Context, model string) (bool, error)

	// GetModelInfo returns information about a specific model from the list.
	GetModelInfo(ctx context.Context, model string) (*ollama.ModelInfo, error)
}

// OllamaHealthChecker provides health check functionality for the Ollama API.
// This interface defines the health check operations used by the service layer.
type OllamaHealthChecker interface {
	// Check performs a health check against the Ollama server.
	Check(ctx context.Context) (*ollama.HealthStatus, error)

	// IsAvailable returns the last known availability status without making a request.
	IsAvailable() bool

	// LastStatus returns the last known health status without making a request.
	LastStatus() *ollama.HealthStatus
}
