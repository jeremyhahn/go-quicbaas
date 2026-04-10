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

// Package service provides high-level service types for Ollama operations.
// This file defines the OllamaService interface and associated request/response types.
package service

import (
	"context"
	"time"
)

// =============================================================================
// Ollama Service Interface
// =============================================================================

// OllamaService provides access to Ollama API operations.
// QuicBaaS uses Ollama as an external service for embedding generation
// and model management. Container orchestration and attestation are
// handled by go-trusted-platform.
type OllamaService interface {
	// Embed generates an embedding for a single text using the default model.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Embed request with text
	// Returns:
	//   - OllamaEmbedResponse: Contains embedding vector
	//   - error: ErrOllamaUnavailable, ErrInvalidInput, ErrTimeout
	Embed(ctx context.Context, req *OllamaEmbedRequest) (*OllamaEmbedResponse, error)

	// EmbedWithModel generates an embedding using a specific model.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Embed request with text and model
	// Returns:
	//   - OllamaEmbedResponse: Contains embedding vector
	//   - error: ErrOllamaUnavailable, ErrModelNotFound, ErrInvalidInput, ErrTimeout
	EmbedWithModel(ctx context.Context, req *OllamaEmbedWithModelRequest) (*OllamaEmbedResponse, error)

	// EmbedBatch generates embeddings for multiple texts.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Batch embed request with texts
	// Returns:
	//   - OllamaEmbedBatchResponse: Contains embedding vectors for all texts
	//   - error: ErrOllamaUnavailable, ErrInvalidInput, ErrTimeout
	EmbedBatch(ctx context.Context, req *OllamaEmbedBatchRequest) (*OllamaEmbedBatchResponse, error)

	// ListModels returns all available models from Ollama.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List models request
	// Returns:
	//   - OllamaListModelsResponse: Contains list of available models
	//   - error: ErrOllamaUnavailable, ErrTimeout
	ListModels(ctx context.Context, req *OllamaListModelsRequest) (*OllamaListModelsResponse, error)

	// GetModelInfo returns detailed information about a specific model.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get model info request with model name
	// Returns:
	//   - OllamaGetModelInfoResponse: Contains model details
	//   - error: ErrOllamaUnavailable, ErrModelNotFound, ErrTimeout
	GetModelInfo(ctx context.Context, req *OllamaGetModelInfoRequest) (*OllamaGetModelInfoResponse, error)

	// Health checks Ollama connectivity and availability.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Health request
	// Returns:
	//   - OllamaHealthResponse: Contains availability status and version
	//   - error: None (always returns response with status)
	Health(ctx context.Context, req *OllamaHealthRequest) (*OllamaHealthResponse, error)

	// IsAvailable returns true if Ollama is available and responding.
	IsAvailable() bool

	// DefaultModel returns the configured default embedding model.
	DefaultModel() string

	// BaseURL returns the configured Ollama base URL.
	BaseURL() string
}

// =============================================================================
// Request/Response Types
// =============================================================================

// OllamaEmbedRequest requests embedding generation for a single text.
type OllamaEmbedRequest struct {
	// Text is the input text to embed
	Text string `json:"text"`
}

// OllamaEmbedWithModelRequest requests embedding generation with a specific model.
type OllamaEmbedWithModelRequest struct {
	// Text is the input text to embed
	Text string `json:"text"`

	// Model is the embedding model to use
	Model string `json:"model"`
}

// OllamaEmbedResponse returns the generated embedding.
type OllamaEmbedResponse struct {
	// Embedding is the embedding vector
	Embedding []float32 `json:"embedding"`

	// Model is the model used
	Model string `json:"model"`

	// Dimensions is the embedding vector dimensions
	Dimensions int `json:"dimensions"`

	// ProcessingTime is the time taken to generate the embedding
	ProcessingTime time.Duration `json:"processing_time"`

	// Timestamp is when the embedding was generated
	Timestamp time.Time `json:"timestamp"`
}

// OllamaEmbedBatchRequest requests batch embedding generation.
type OllamaEmbedBatchRequest struct {
	// Texts is the list of input texts to embed
	Texts []string `json:"texts"`

	// Model optionally specifies which model to use (uses default if empty)
	Model string `json:"model,omitempty"`
}

// OllamaEmbedBatchResponse returns batch embedding results.
type OllamaEmbedBatchResponse struct {
	// Embeddings is the list of embedding vectors (same order as input)
	Embeddings [][]float32 `json:"embeddings"`

	// Model is the model used
	Model string `json:"model"`

	// Dimensions is the embedding vector dimensions
	Dimensions int `json:"dimensions"`

	// Count is the number of embeddings generated
	Count int `json:"count"`

	// ProcessingTime is the total processing time
	ProcessingTime time.Duration `json:"processing_time"`

	// Timestamp is when the batch was processed
	Timestamp time.Time `json:"timestamp"`
}

// OllamaListModelsRequest requests a list of available models.
type OllamaListModelsRequest struct {
	// No parameters needed
}

// OllamaModelInfo contains information about an Ollama model.
type OllamaModelInfo struct {
	// Name is the model name
	Name string `json:"name"`

	// Size is the model size in bytes
	Size int64 `json:"size"`

	// Digest is the model digest/hash
	Digest string `json:"digest"`

	// ModifiedAt is when the model was last modified
	ModifiedAt time.Time `json:"modified_at"`
}

// OllamaListModelsResponse returns the list of available models.
type OllamaListModelsResponse struct {
	// Models is the list of available models
	Models []OllamaModelInfo `json:"models"`

	// Count is the number of models
	Count int `json:"count"`

	// Timestamp is when the list was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// OllamaGetModelInfoRequest requests detailed model information.
type OllamaGetModelInfoRequest struct {
	// Model is the model name
	Model string `json:"model"`
}

// OllamaModelDetails contains detailed model information.
type OllamaModelDetails struct {
	// License is the model license
	License string `json:"license"`

	// Modelfile is the model file contents
	Modelfile string `json:"modelfile"`

	// Format is the model format (e.g., "gguf")
	Format string `json:"format"`

	// Family is the model family (e.g., "nomic", "llama")
	Family string `json:"family"`

	// ParameterSize is the model parameter size (e.g., "137M")
	ParameterSize string `json:"parameter_size"`

	// QuantizationLevel is the quantization level (e.g., "Q4_0")
	QuantizationLevel string `json:"quantization_level"`
}

// OllamaGetModelInfoResponse returns detailed model information.
type OllamaGetModelInfoResponse struct {
	// Model is the model name
	Model string `json:"model"`

	// Details contains the model details
	Details OllamaModelDetails `json:"details"`

	// Timestamp is when the info was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// OllamaHealthRequest requests Ollama health status.
type OllamaHealthRequest struct {
	// No parameters needed
}

// OllamaHealthResponse returns Ollama health status.
type OllamaHealthResponse struct {
	// Available indicates if Ollama is available
	Available bool `json:"available"`

	// Version is the Ollama server version
	Version string `json:"version"`

	// BaseURL is the Ollama API URL
	BaseURL string `json:"base_url"`

	// Error contains any error message
	Error string `json:"error,omitempty"`

	// LastChecked is when the health was last checked
	LastChecked time.Time `json:"last_checked"`

	// Timestamp is when this response was generated
	Timestamp time.Time `json:"timestamp"`
}
