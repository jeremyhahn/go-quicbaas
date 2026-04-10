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

// Package service provides high-level service types for embedding operations.
// This file defines the EmbeddingService interface and associated request/response types.
package service

import (
	"context"
	"time"
)

// =============================================================================
// Embedding Service Interface
// =============================================================================

// EmbeddingService provides semantic embedding generation and management.
// It supports multiple embedding providers (Voyage AI, OpenAI, Ollama, LM Studio)
// with intelligent caching, batch processing, and provider fallback capabilities.
type EmbeddingService interface {
	// GenerateEmbedding generates an embedding vector for a single text.
	// The text is processed by the configured embedding provider and the result
	// is cached for subsequent requests.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Generate embedding request with text and optional metadata
	// Returns:
	//   - GenerateEmbeddingResponse: Contains embedding vector and metadata
	//   - error: ErrProviderUnavailable, ErrTokenLimitExceeded, ErrTimeout
	GenerateEmbedding(ctx context.Context, req *GenerateEmbeddingRequest) (*GenerateEmbeddingResponse, error)

	// BatchGenerateEmbeddings generates embeddings for multiple texts in a single request.
	// This is more efficient than individual requests as it leverages provider batch APIs
	// and parallel processing. Cached embeddings are returned immediately while uncached
	// texts are batched together for efficient processing.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Batch generate request with multiple texts
	// Returns:
	//   - BatchGenerateEmbeddingsResponse: Contains embedding vectors for all texts
	//   - error: ErrProviderUnavailable, ErrTokenLimitExceeded, ErrTimeout
	BatchGenerateEmbeddings(ctx context.Context, req *BatchGenerateEmbeddingsRequest) (*BatchGenerateEmbeddingsResponse, error)

	// ListProviders returns all registered embedding providers with their capabilities.
	// This includes provider name, model, dimensions, max tokens, and availability status.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: List providers request with optional filters
	// Returns:
	//   - ListProvidersResponse: Contains list of available providers
	//   - error: ErrTimeout
	ListProviders(ctx context.Context, req *ListProvidersRequest) (*ListProvidersResponse, error)

	// GetProviderInfo returns detailed information about a specific provider.
	// This includes configuration, model details, rate limits, and current status.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get provider info request with provider name
	// Returns:
	//   - GetProviderInfoResponse: Contains detailed provider information
	//   - error: ErrProviderNotFound, ErrTimeout
	GetProviderInfo(ctx context.Context, req *GetProviderInfoRequest) (*GetProviderInfoResponse, error)

	// SetDefaultProvider sets the default embedding provider to use for requests
	// that don't explicitly specify a provider.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Set default provider request with provider name
	// Returns:
	//   - SetDefaultProviderResponse: Contains confirmation and updated provider
	//   - error: ErrProviderNotFound, ErrTimeout
	SetDefaultProvider(ctx context.Context, req *SetDefaultProviderRequest) (*SetDefaultProviderResponse, error)

	// GetCacheStats returns statistics about the embedding cache.
	// This includes hit rate, size, evictions, and memory usage.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Get cache stats request
	// Returns:
	//   - GetEmbeddingCacheStatsResponse: Contains cache statistics
	//   - error: ErrTimeout
	GetCacheStats(ctx context.Context, req *GetEmbeddingCacheStatsRequest) (*GetEmbeddingCacheStatsResponse, error)

	// ClearCache invalidates all or specific embedding cache entries.
	// This can be scoped to a specific provider or clear the entire cache.
	// Parameters:
	//   - ctx: Request context with timeout and metadata
	//   - req: Clear cache request with optional provider filter
	// Returns:
	//   - ClearEmbeddingCacheResponse: Contains number of entries cleared
	//   - error: ErrTimeout
	ClearCache(ctx context.Context, req *ClearEmbeddingCacheRequest) (*ClearEmbeddingCacheResponse, error)
}

// =============================================================================
// Common Types and Enums
// =============================================================================

// EmbeddingProvider specifies the embedding provider type.
type EmbeddingProvider string

const (
	// EmbeddingProviderVoyage uses Voyage AI embedding models.
	EmbeddingProviderVoyage EmbeddingProvider = "voyage"

	// EmbeddingProviderOpenAI uses OpenAI embedding models.
	EmbeddingProviderOpenAI EmbeddingProvider = "openai"

	// EmbeddingProviderOllama uses local Ollama embedding models.
	EmbeddingProviderOllama EmbeddingProvider = "ollama"

	// EmbeddingProviderLMStudio uses local LM Studio embedding models.
	EmbeddingProviderLMStudio EmbeddingProvider = "lmstudio"
)

// EmbeddingInputType specifies the type of text being embedded.
type EmbeddingInputType string

const (
	// EmbeddingInputTypeDocument indicates document/long-form text.
	EmbeddingInputTypeDocument EmbeddingInputType = "document"

	// EmbeddingInputTypeQuery indicates search query text.
	EmbeddingInputTypeQuery EmbeddingInputType = "query"

	// EmbeddingInputTypeCode indicates source code.
	EmbeddingInputTypeCode EmbeddingInputType = "code"
)

// EmbeddingStatus indicates provider availability status.
type EmbeddingStatus string

const (
	// EmbeddingStatusAvailable indicates the provider is ready.
	EmbeddingStatusAvailable EmbeddingStatus = "available"

	// EmbeddingStatusUnavailable indicates the provider is down.
	EmbeddingStatusUnavailable EmbeddingStatus = "unavailable"

	// EmbeddingStatusRateLimited indicates the provider hit rate limits.
	EmbeddingStatusRateLimited EmbeddingStatus = "rate_limited"

	// EmbeddingStatusError indicates the provider has errors.
	EmbeddingStatusError EmbeddingStatus = "error"
)

// =============================================================================
// Embedding Metadata and Configuration
// =============================================================================

// EmbeddingMetadata contains information about an embedding request.
type EmbeddingMetadata struct {
	// Model is the embedding model name
	Model string `json:"model"`

	// Provider is the provider that generated the embedding
	Provider EmbeddingProvider `json:"provider"`

	// Dimensions is the embedding vector dimensions
	Dimensions int `json:"dimensions"`

	// Tokens is the number of tokens processed
	Tokens int `json:"tokens"`

	// Source is the source identifier (file path, URL, etc.)
	Source string `json:"source,omitempty"`

	// Language is the programming or natural language
	Language string `json:"language,omitempty"`

	// InputType specifies the type of input text
	InputType EmbeddingInputType `json:"input_type,omitempty"`

	// Tags are custom user-defined tags
	Tags map[string]string `json:"tags,omitempty"`

	// CacheHit indicates if this result was served from cache
	CacheHit bool `json:"cache_hit"`

	// ProcessingTime is the time taken to generate the embedding
	ProcessingTime time.Duration `json:"processing_time"`
}

// ProviderInfo contains detailed information about an embedding provider.
type ProviderInfo struct {
	// Name is the provider name
	Name string `json:"name"`

	// Type is the provider type
	Type EmbeddingProvider `json:"type"`

	// Model is the model name being used
	Model string `json:"model"`

	// Dimensions is the embedding vector dimensions
	Dimensions int `json:"dimensions"`

	// MaxTokens is the maximum number of tokens per request
	MaxTokens int `json:"max_tokens"`

	// MaxBatchSize is the maximum batch size supported
	MaxBatchSize int `json:"max_batch_size"`

	// Status is the current provider status
	Status EmbeddingStatus `json:"status"`

	// APIURL is the provider API endpoint
	APIURL string `json:"api_url"`

	// RateLimitPerMinute is the rate limit (0 = unlimited)
	RateLimitPerMinute int `json:"rate_limit_per_minute,omitempty"`

	// Timeout is the request timeout duration
	Timeout time.Duration `json:"timeout"`

	// IsDefault indicates if this is the default provider
	IsDefault bool `json:"is_default"`

	// LastError contains the last error message if any
	LastError string `json:"last_error,omitempty"`

	// LastErrorTime is when the last error occurred
	LastErrorTime *time.Time `json:"last_error_time,omitempty"`

	// SuccessCount is the total successful requests
	SuccessCount uint64 `json:"success_count"`

	// ErrorCount is the total failed requests
	ErrorCount uint64 `json:"error_count"`

	// AvgLatency is the average request latency
	AvgLatency time.Duration `json:"avg_latency"`
}

// EmbeddingCacheConfig contains cache configuration settings.
type EmbeddingCacheConfig struct {
	// Enabled indicates if caching is enabled
	Enabled bool `json:"enabled"`

	// Size is the maximum number of cached embeddings
	Size int `json:"size"`

	// TTL is the cache entry time-to-live in seconds
	TTL int `json:"ttl_seconds"`

	// CompressionEnabled indicates if cache entries are compressed
	CompressionEnabled bool `json:"compression_enabled"`
}

// =============================================================================
// Generate Embedding Request/Response
// =============================================================================

// GenerateEmbeddingRequest requests embedding generation for a single text.
type GenerateEmbeddingRequest struct {
	// Text is the input text to embed
	Text string `json:"text"`

	// Provider optionally specifies which provider to use (uses default if empty)
	Provider EmbeddingProvider `json:"provider,omitempty"`

	// InputType specifies the type of input text
	InputType EmbeddingInputType `json:"input_type,omitempty"`

	// Metadata contains optional metadata to attach
	Metadata map[string]string `json:"metadata,omitempty"`

	// Source is the source identifier
	Source string `json:"source,omitempty"`

	// Language is the programming or natural language
	Language string `json:"language,omitempty"`

	// CacheBypass forces generation even if cached
	CacheBypass bool `json:"cache_bypass,omitempty"`
}

// GenerateEmbeddingResponse returns the generated embedding.
type GenerateEmbeddingResponse struct {
	// Embedding is the embedding vector
	Embedding []float32 `json:"embedding"`

	// Metadata contains embedding metadata
	Metadata *EmbeddingMetadata `json:"metadata"`

	// Timestamp is when the embedding was generated
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Batch Generate Embeddings Request/Response
// =============================================================================

// BatchEmbeddingItem represents a single item in a batch embedding request.
type BatchEmbeddingItem struct {
	// ID is a client-provided identifier for this item
	ID string `json:"id"`

	// Text is the input text to embed
	Text string `json:"text"`

	// InputType specifies the type of input text
	InputType EmbeddingInputType `json:"input_type,omitempty"`

	// Metadata contains optional metadata
	Metadata map[string]string `json:"metadata,omitempty"`

	// Source is the source identifier
	Source string `json:"source,omitempty"`

	// Language is the programming or natural language
	Language string `json:"language,omitempty"`
}

// BatchGenerateEmbeddingsRequest requests batch embedding generation.
type BatchGenerateEmbeddingsRequest struct {
	// Items is the list of texts to embed
	Items []*BatchEmbeddingItem `json:"items"`

	// Provider optionally specifies which provider to use
	Provider EmbeddingProvider `json:"provider,omitempty"`

	// CacheBypass forces generation even if cached
	CacheBypass bool `json:"cache_bypass,omitempty"`
}

// BatchEmbeddingResult represents the result for one batch item.
type BatchEmbeddingResult struct {
	// ID is the client-provided identifier
	ID string `json:"id"`

	// Embedding is the embedding vector
	Embedding []float32 `json:"embedding"`

	// Metadata contains embedding metadata
	Metadata *EmbeddingMetadata `json:"metadata"`

	// Error contains any error message for this item
	Error string `json:"error,omitempty"`
}

// BatchGenerateEmbeddingsResponse returns batch embedding results.
type BatchGenerateEmbeddingsResponse struct {
	// Results contains per-item results in the same order as the request
	Results []*BatchEmbeddingResult `json:"results"`

	// SuccessCount is the number of successful embeddings
	SuccessCount int `json:"success_count"`

	// ErrorCount is the number of failed embeddings
	ErrorCount int `json:"error_count"`

	// TotalTokens is the total tokens processed
	TotalTokens int `json:"total_tokens"`

	// ProcessingTime is the total processing time
	ProcessingTime time.Duration `json:"processing_time"`

	// Timestamp is when the batch was processed
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Provider Management Request/Response
// =============================================================================

// ListProvidersRequest requests a list of available providers.
type ListProvidersRequest struct {
	// IncludeUnavailable includes providers that are currently unavailable
	IncludeUnavailable bool `json:"include_unavailable,omitempty"`
}

// ListProvidersResponse returns the list of providers.
type ListProvidersResponse struct {
	// Providers is the list of available providers
	Providers []*ProviderInfo `json:"providers"`

	// DefaultProvider is the name of the default provider
	DefaultProvider string `json:"default_provider"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// GetProviderInfoRequest requests information about a specific provider.
type GetProviderInfoRequest struct {
	// Provider is the provider name
	Provider EmbeddingProvider `json:"provider"`
}

// GetProviderInfoResponse returns detailed provider information.
type GetProviderInfoResponse struct {
	// Info contains the provider information
	Info *ProviderInfo `json:"info"`

	// Timestamp is when the info was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// SetDefaultProviderRequest sets the default provider.
type SetDefaultProviderRequest struct {
	// Provider is the provider to set as default
	Provider EmbeddingProvider `json:"provider"`
}

// SetDefaultProviderResponse confirms the default provider change.
type SetDefaultProviderResponse struct {
	// Provider is the new default provider
	Provider EmbeddingProvider `json:"provider"`

	// PreviousProvider is the previous default provider
	PreviousProvider EmbeddingProvider `json:"previous_provider"`

	// Timestamp is when the change was made
	Timestamp time.Time `json:"timestamp"`
}

// =============================================================================
// Cache Management Request/Response
// =============================================================================

// GetEmbeddingCacheStatsRequest requests cache statistics.
type GetEmbeddingCacheStatsRequest struct {
	// No parameters needed
}

// GetEmbeddingCacheStatsResponse returns cache statistics.
type GetEmbeddingCacheStatsResponse struct {
	// Enabled indicates if caching is enabled
	Enabled bool `json:"enabled"`

	// Size is the current number of cached embeddings
	Size int `json:"size"`

	// Capacity is the maximum cache capacity
	Capacity int `json:"capacity"`

	// Hits is the total cache hits
	Hits uint64 `json:"hits"`

	// Misses is the total cache misses
	Misses uint64 `json:"misses"`

	// HitRate is the cache hit rate (0.0 to 1.0)
	HitRate float64 `json:"hit_rate"`

	// Evictions is the total number of evictions
	Evictions uint64 `json:"evictions"`

	// MemoryBytes is the approximate memory used by cache
	MemoryBytes uint64 `json:"memory_bytes,omitempty"`

	// CompressionEnabled indicates if compression is enabled
	CompressionEnabled bool `json:"compression_enabled"`

	// CompressionRatio is the average compression ratio
	CompressionRatio float64 `json:"compression_ratio,omitempty"`

	// Timestamp is when stats were collected
	Timestamp time.Time `json:"timestamp"`
}

// ClearEmbeddingCacheRequest requests cache clearing.
type ClearEmbeddingCacheRequest struct {
	// Provider optionally clears cache only for a specific provider
	Provider EmbeddingProvider `json:"provider,omitempty"`

	// All clears the entire cache regardless of provider
	All bool `json:"all,omitempty"`
}

// ClearEmbeddingCacheResponse confirms cache clearing.
type ClearEmbeddingCacheResponse struct {
	// EntriesCleared is the number of cache entries removed
	EntriesCleared int `json:"entries_cleared"`

	// Provider is the provider whose cache was cleared (if specified)
	Provider EmbeddingProvider `json:"provider,omitempty"`

	// Timestamp is when the cache was cleared
	Timestamp time.Time `json:"timestamp"`
}
