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

package embedding

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Embedding"
)

// Provider specifies the embedding provider type.
type Provider string

const (
	// ProviderVoyage uses Voyage AI embedding models.
	ProviderVoyage Provider = "voyage"

	// ProviderOpenAI uses OpenAI embedding models.
	ProviderOpenAI Provider = "openai"

	// ProviderOllama uses local Ollama embedding models.
	ProviderOllama Provider = "ollama"

	// ProviderLMStudio uses local LM Studio embedding models.
	ProviderLMStudio Provider = "lmstudio"
)

// InputType specifies the type of text being embedded.
type InputType string

const (
	// InputTypeDocument indicates document/long-form text.
	InputTypeDocument InputType = "document"

	// InputTypeQuery indicates search query text.
	InputTypeQuery InputType = "query"

	// InputTypeCode indicates source code.
	InputTypeCode InputType = "code"
)

// Status indicates provider availability status.
type Status string

const (
	// StatusAvailable indicates the provider is ready.
	StatusAvailable Status = "available"

	// StatusUnavailable indicates the provider is down.
	StatusUnavailable Status = "unavailable"

	// StatusRateLimited indicates the provider hit rate limits.
	StatusRateLimited Status = "rate_limited"

	// StatusError indicates the provider has errors.
	StatusError Status = "error"
)

// Client provides embedding generation operations.
type Client interface {
	// GenerateEmbedding generates an embedding vector for a single text.
	GenerateEmbedding(ctx context.Context, req *GenerateEmbeddingRequest) (*GenerateEmbeddingResponse, error)

	// BatchGenerateEmbeddings generates embeddings for multiple texts in a single request.
	BatchGenerateEmbeddings(ctx context.Context, req *BatchGenerateEmbeddingsRequest) (*BatchGenerateEmbeddingsResponse, error)

	// ListProviders returns all registered embedding providers with their capabilities.
	ListProviders(ctx context.Context, req *ListProvidersRequest) (*ListProvidersResponse, error)

	// GetProviderInfo returns detailed information about a specific provider.
	GetProviderInfo(ctx context.Context, req *GetProviderInfoRequest) (*GetProviderInfoResponse, error)

	// SetDefaultProvider sets the default embedding provider.
	SetDefaultProvider(ctx context.Context, req *SetDefaultProviderRequest) (*SetDefaultProviderResponse, error)

	// GetCacheStats returns statistics about the embedding cache.
	GetCacheStats(ctx context.Context, req *GetEmbeddingCacheStatsRequest) (*GetEmbeddingCacheStatsResponse, error)

	// ClearCache invalidates all or specific embedding cache entries.
	ClearCache(ctx context.Context, req *ClearEmbeddingCacheRequest) (*ClearEmbeddingCacheResponse, error)
}

// Metadata contains information about an embedding request.
type Metadata struct {
	// Model is the embedding model name
	Model string `json:"model"`

	// Provider is the provider that generated the embedding
	Provider Provider `json:"provider"`

	// Dimensions is the embedding vector dimensions
	Dimensions int `json:"dimensions"`

	// Tokens is the number of tokens processed
	Tokens int `json:"tokens"`

	// Source is the source identifier (file path, URL, etc.)
	Source string `json:"source"`

	// Language is the programming or natural language
	Language string `json:"language"`

	// InputType specifies the type of input text
	InputType InputType `json:"input_type"`

	// Tags are custom user-defined tags
	Tags map[string]string `json:"tags"`

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
	Type Provider `json:"type"`

	// Model is the model name being used
	Model string `json:"model"`

	// Dimensions is the embedding vector dimensions
	Dimensions int `json:"dimensions"`

	// MaxTokens is the maximum number of tokens per request
	MaxTokens int `json:"max_tokens"`

	// MaxBatchSize is the maximum batch size supported
	MaxBatchSize int `json:"max_batch_size"`

	// Status is the current provider status
	Status Status `json:"status"`

	// APIURL is the provider API endpoint
	APIURL string `json:"api_url"`

	// RateLimitPerMinute is the rate limit (0 = unlimited)
	RateLimitPerMinute int `json:"rate_limit_per_minute"`

	// Timeout is the request timeout duration
	Timeout time.Duration `json:"timeout"`

	// IsDefault indicates if this is the default provider
	IsDefault bool `json:"is_default"`

	// LastError contains the last error message if any
	LastError string `json:"last_error"`

	// LastErrorTime is when the last error occurred
	LastErrorTime *time.Time `json:"last_error_time"`

	// SuccessCount is the total successful requests
	SuccessCount uint64 `json:"success_count"`

	// ErrorCount is the total failed requests
	ErrorCount uint64 `json:"error_count"`

	// AvgLatency is the average request latency
	AvgLatency time.Duration `json:"avg_latency"`
}

// GenerateEmbeddingRequest requests embedding generation for a single text.
type GenerateEmbeddingRequest struct {
	// Text is the input text to embed
	Text string `json:"text"`

	// Provider optionally specifies which provider to use (uses default if empty)
	Provider Provider `json:"provider"`

	// InputType specifies the type of input text
	InputType InputType `json:"input_type"`

	// Metadata contains optional metadata to attach
	Metadata map[string]string `json:"metadata"`

	// Source is the source identifier
	Source string `json:"source"`

	// Language is the programming or natural language
	Language string `json:"language"`

	// CacheBypass forces generation even if cached
	CacheBypass bool `json:"cache_bypass"`
}

// GenerateEmbeddingResponse returns the generated embedding.
type GenerateEmbeddingResponse struct {
	// Embedding is the embedding vector
	Embedding []float32 `json:"embedding"`

	// Metadata contains embedding metadata
	Metadata *Metadata `json:"metadata"`

	// Timestamp is when the embedding was generated
	Timestamp time.Time `json:"timestamp"`
}

// BatchEmbeddingItem represents a single item in a batch embedding request.
type BatchEmbeddingItem struct {
	// ID is a client-provided identifier for this item
	ID string `json:"id"`

	// Text is the input text to embed
	Text string `json:"text"`

	// InputType specifies the type of input text
	InputType InputType `json:"input_type"`

	// Metadata contains optional metadata
	Metadata map[string]string `json:"metadata"`

	// Source is the source identifier
	Source string `json:"source"`

	// Language is the programming or natural language
	Language string `json:"language"`
}

// BatchGenerateEmbeddingsRequest requests batch embedding generation.
type BatchGenerateEmbeddingsRequest struct {
	// Items is the list of texts to embed
	Items []*BatchEmbeddingItem `json:"items"`

	// Provider optionally specifies which provider to use
	Provider Provider `json:"provider"`

	// CacheBypass forces generation even if cached
	CacheBypass bool `json:"cache_bypass"`
}

// BatchEmbeddingResult represents the result for one batch item.
type BatchEmbeddingResult struct {
	// ID is the client-provided identifier
	ID string `json:"id"`

	// Embedding is the embedding vector
	Embedding []float32 `json:"embedding"`

	// Metadata contains embedding metadata
	Metadata *Metadata `json:"metadata"`

	// Error contains any error message for this item
	Error string `json:"error"`
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

// ListProvidersRequest requests a list of available providers.
type ListProvidersRequest struct {
	// IncludeUnavailable includes providers that are currently unavailable
	IncludeUnavailable bool `json:"include_unavailable"`
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
	Provider Provider `json:"provider"`
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
	Provider Provider `json:"provider"`
}

// SetDefaultProviderResponse confirms the default provider change.
type SetDefaultProviderResponse struct {
	// Provider is the new default provider
	Provider Provider `json:"provider"`

	// PreviousProvider is the previous default provider
	PreviousProvider Provider `json:"previous_provider"`

	// Timestamp is when the change was made
	Timestamp time.Time `json:"timestamp"`
}

// GetEmbeddingCacheStatsRequest requests cache statistics.
type GetEmbeddingCacheStatsRequest struct{}

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
	MemoryBytes uint64 `json:"memory_bytes"`

	// CompressionEnabled indicates if compression is enabled
	CompressionEnabled bool `json:"compression_enabled"`

	// CompressionRatio is the average compression ratio
	CompressionRatio float64 `json:"compression_ratio"`

	// Timestamp is when stats were collected
	Timestamp time.Time `json:"timestamp"`
}

// ClearEmbeddingCacheRequest requests cache clearing.
type ClearEmbeddingCacheRequest struct {
	// Provider optionally clears cache only for a specific provider
	Provider Provider `json:"provider"`

	// All clears the entire cache regardless of provider
	All bool `json:"all"`
}

// ClearEmbeddingCacheResponse confirms cache clearing.
type ClearEmbeddingCacheResponse struct {
	// EntriesCleared is the number of cache entries removed
	EntriesCleared int `json:"entries_cleared"`

	// Provider is the provider whose cache was cleared (if specified)
	Provider Provider `json:"provider"`

	// Timestamp is when the cache was cleared
	Timestamp time.Time `json:"timestamp"`
}

// client is the embedding client.
type client struct {
	transport transport.Transport
}

// NewClient creates a new embedding client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// GenerateEmbedding generates an embedding vector for a single text.
func (c *client) GenerateEmbedding(ctx context.Context, req *GenerateEmbeddingRequest) (*GenerateEmbeddingResponse, error) {
	if err := validateGenerateEmbeddingRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("embedding.GenerateEmbedding", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GenerateEmbedding", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("embedding.GenerateEmbedding", "nil response from server")
	}

	embeddingResp, ok := resp.(*GenerateEmbeddingResponse)
	if !ok {
		return nil, errors.NewInternal("embedding.GenerateEmbedding", "invalid response type")
	}

	return embeddingResp, nil
}

// BatchGenerateEmbeddings generates embeddings for multiple texts in a single request.
func (c *client) BatchGenerateEmbeddings(ctx context.Context, req *BatchGenerateEmbeddingsRequest) (*BatchGenerateEmbeddingsResponse, error) {
	if err := validateBatchGenerateEmbeddingsRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("embedding.BatchGenerateEmbeddings", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "BatchGenerateEmbeddings", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("embedding.BatchGenerateEmbeddings", "nil response from server")
	}

	batchResp, ok := resp.(*BatchGenerateEmbeddingsResponse)
	if !ok {
		return nil, errors.NewInternal("embedding.BatchGenerateEmbeddings", "invalid response type")
	}

	return batchResp, nil
}

// ListProviders returns all registered embedding providers with their capabilities.
func (c *client) ListProviders(ctx context.Context, req *ListProvidersRequest) (*ListProvidersResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("embedding.ListProviders", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("embedding.ListProviders", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ListProviders", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &ListProvidersResponse{Providers: []*ProviderInfo{}}, nil
	}

	listResp, ok := resp.(*ListProvidersResponse)
	if !ok {
		return nil, errors.NewInternal("embedding.ListProviders", "invalid response type")
	}

	return listResp, nil
}

// GetProviderInfo returns detailed information about a specific provider.
func (c *client) GetProviderInfo(ctx context.Context, req *GetProviderInfoRequest) (*GetProviderInfoResponse, error) {
	if err := validateGetProviderInfoRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("embedding.GetProviderInfo", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetProviderInfo", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("embedding.GetProviderInfo", "nil response from server")
	}

	infoResp, ok := resp.(*GetProviderInfoResponse)
	if !ok {
		return nil, errors.NewInternal("embedding.GetProviderInfo", "invalid response type")
	}

	return infoResp, nil
}

// SetDefaultProvider sets the default embedding provider.
func (c *client) SetDefaultProvider(ctx context.Context, req *SetDefaultProviderRequest) (*SetDefaultProviderResponse, error) {
	if err := validateSetDefaultProviderRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("embedding.SetDefaultProvider", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "SetDefaultProvider", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("embedding.SetDefaultProvider", "nil response from server")
	}

	setResp, ok := resp.(*SetDefaultProviderResponse)
	if !ok {
		return nil, errors.NewInternal("embedding.SetDefaultProvider", "invalid response type")
	}

	return setResp, nil
}

// GetCacheStats returns statistics about the embedding cache.
func (c *client) GetCacheStats(ctx context.Context, req *GetEmbeddingCacheStatsRequest) (*GetEmbeddingCacheStatsResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("embedding.GetCacheStats", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("embedding.GetCacheStats", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetCacheStats", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("embedding.GetCacheStats", "nil response from server")
	}

	statsResp, ok := resp.(*GetEmbeddingCacheStatsResponse)
	if !ok {
		return nil, errors.NewInternal("embedding.GetCacheStats", "invalid response type")
	}

	return statsResp, nil
}

// ClearCache invalidates all or specific embedding cache entries.
func (c *client) ClearCache(ctx context.Context, req *ClearEmbeddingCacheRequest) (*ClearEmbeddingCacheResponse, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("embedding.ClearCache", "request is required")
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("embedding.ClearCache", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "ClearCache", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("embedding.ClearCache", "nil response from server")
	}

	clearResp, ok := resp.(*ClearEmbeddingCacheResponse)
	if !ok {
		return nil, errors.NewInternal("embedding.ClearCache", "invalid response type")
	}

	return clearResp, nil
}

// Validation functions

func validateGenerateEmbeddingRequest(req *GenerateEmbeddingRequest) error {
	if req == nil {
		return errors.NewInvalidInput("embedding.GenerateEmbedding", "request is required")
	}
	if req.Text == "" {
		return errors.NewInvalidInput("embedding.GenerateEmbedding", "text is required")
	}
	return nil
}

func validateBatchGenerateEmbeddingsRequest(req *BatchGenerateEmbeddingsRequest) error {
	if req == nil {
		return errors.NewInvalidInput("embedding.BatchGenerateEmbeddings", "request is required")
	}
	if len(req.Items) == 0 {
		return errors.NewInvalidInput("embedding.BatchGenerateEmbeddings", "items are required")
	}
	for i, item := range req.Items {
		if item == nil {
			return errors.NewInvalidInput("embedding.BatchGenerateEmbeddings", "item at index is nil").
				WithContext("index", i)
		}
		if item.ID == "" {
			return errors.NewInvalidInput("embedding.BatchGenerateEmbeddings", "item ID is required").
				WithContext("index", i)
		}
		if item.Text == "" {
			return errors.NewInvalidInput("embedding.BatchGenerateEmbeddings", "item text is required").
				WithContext("index", i).
				WithContext("id", item.ID)
		}
	}
	return nil
}

func validateGetProviderInfoRequest(req *GetProviderInfoRequest) error {
	if req == nil {
		return errors.NewInvalidInput("embedding.GetProviderInfo", "request is required")
	}
	if req.Provider == "" {
		return errors.NewInvalidInput("embedding.GetProviderInfo", "provider is required")
	}
	return nil
}

func validateSetDefaultProviderRequest(req *SetDefaultProviderRequest) error {
	if req == nil {
		return errors.NewInvalidInput("embedding.SetDefaultProvider", "request is required")
	}
	if req.Provider == "" {
		return errors.NewInvalidInput("embedding.SetDefaultProvider", "provider is required")
	}
	return nil
}
