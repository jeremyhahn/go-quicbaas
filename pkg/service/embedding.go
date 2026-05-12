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

// Package service provides the unified service facade for QuicBaaS.
// This file implements the EmbeddingService interface.
package service

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// embeddingService is the concrete implementation of EmbeddingService.
// It orchestrates embedding generation through the embedding manager,
// integrates with caching, and provides observability through metrics.
type embeddingService struct {
	manager          interfaces.EmbeddingManager
	logger           interfaces.Logger
	metrics          *embeddingMetrics
	disabledProviders sync.Map // tracks provider name (string) → true when disabled
}

// embeddingMetrics holds Prometheus metrics for embedding operations.
type embeddingMetrics struct {
	embeddingsGeneratedTotal *metrics.Counter
	batchEmbeddingsTotal     *metrics.Counter
	cacheHitsTotal           *metrics.Counter
	cacheMissesTotal         *metrics.Counter
	operationDuration        *metrics.HistogramVec
	operationErrors          *metrics.CounterVec
	providerErrors           *metrics.CounterVec
	tokenUsageTotal          *metrics.Counter
}

// NewEmbeddingService creates a new embedding service implementation.
// It requires an embedding manager for provider orchestration and an optional logger.
// The service initializes Prometheus metrics for observability.
func NewEmbeddingService(manager interfaces.EmbeddingManager, logger interfaces.Logger) EmbeddingService {
	svc := &embeddingService{
		manager: manager,
		logger:  logger,
	}

	// Initialize metrics
	svc.initMetrics()

	return svc
}

// initMetrics initializes Prometheus metrics for the embedding service.
func (s *embeddingService) initMetrics() {
	s.metrics = &embeddingMetrics{
		embeddingsGeneratedTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "embeddings",
			Name:      "generated_total",
			Help:      "Total number of embeddings generated",
		}),

		batchEmbeddingsTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "embeddings",
			Name:      "batch_generated_total",
			Help:      "Total number of batch embeddings generated",
		}),

		cacheHitsTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "embeddings",
			Name:      "cache_hits_total",
			Help:      "Total number of embedding cache hits",
		}),

		cacheMissesTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "embeddings",
			Name:      "cache_misses_total",
			Help:      "Total number of embedding cache misses",
		}),

		operationDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "embeddings",
			Name:      "operation_duration_seconds",
			Help:      "Duration of embedding operations in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"operation", "provider"}),

		operationErrors: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "embeddings",
			Name:      "operation_errors_total",
			Help:      "Total errors by operation type",
		}, []string{"operation", "error_type"}),

		providerErrors: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "embeddings",
			Name:      "provider_errors_total",
			Help:      "Total errors by provider",
		}, []string{"provider", "error_type"}),

		tokenUsageTotal: metrics.NewCounter(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "embeddings",
			Name:      "tokens_processed_total",
			Help:      "Total number of tokens processed",
		}),
	}
}

// =============================================================================
// EmbeddingService Implementation
// =============================================================================

// GenerateEmbedding generates an embedding vector for a single text.
// It uses the configured embedding provider (or default if not specified),
// checks cache first for efficiency, and stores successful results in cache.
func (s *embeddingService) GenerateEmbedding(ctx context.Context, req *GenerateEmbeddingRequest) (*GenerateEmbeddingResponse, error) {
	start := time.Now()
	providerName := string(req.Provider)
	if providerName == "" {
		providerName = "default"
	}

	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("generate", providerName).Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateGenerateEmbeddingRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("generate", "invalid_input").Inc()
		}
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"text_length": len(req.Text),
			"provider":    providerName,
			"input_type":  req.InputType,
		}).Debug("Generating embedding")
	}

	// Capture cache stats before the call to determine cache hit
	statsBefore := s.manager.GetStats()
	cacheHitsBefore := statsBefore.CacheHits

	// Generate embedding through manager (handles caching internally)
	embeddingVec, err := s.manager.Embed(ctx, req.Text)
	if err != nil {
		return nil, s.handleEmbeddingError(err, "generate", providerName)
	}

	// Check if this was a cache hit by comparing stats
	statsAfter := s.manager.GetStats()
	cacheHit := statsAfter.CacheHits > cacheHitsBefore

	// Get dimensions for metadata
	dimensions, _ := s.manager.Dimensions()

	// Increment metrics
	if s.metrics != nil {
		s.metrics.embeddingsGeneratedTotal.Inc()
		if cacheHit {
			s.metrics.cacheHitsTotal.Inc()
		} else {
			s.metrics.cacheMissesTotal.Inc()
		}
	}

	// Build metadata
	metadata := &EmbeddingMetadata{
		Model:          "unknown", // Manager doesn't expose model name yet
		Provider:       req.Provider,
		Dimensions:     dimensions,
		Tokens:         estimateTokens(req.Text),
		Source:         req.Source,
		Language:       req.Language,
		InputType:      req.InputType,
		Tags:           req.Metadata,
		CacheHit:       cacheHit,
		ProcessingTime: time.Since(start),
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"dimensions":      dimensions,
			"processing_time": time.Since(start),
			"cache_hit":       cacheHit,
		}).Info("Embedding generated successfully")
	}

	return &GenerateEmbeddingResponse{
		Embedding: embeddingVec,
		Metadata:  metadata,
		Timestamp: time.Now(),
	}, nil
}

// BatchGenerateEmbeddings generates embeddings for multiple texts in a single request.
// This is more efficient than individual requests as it leverages provider batch APIs
// and parallel processing. The manager handles cache integration transparently.
func (s *embeddingService) BatchGenerateEmbeddings(ctx context.Context, req *BatchGenerateEmbeddingsRequest) (*BatchGenerateEmbeddingsResponse, error) {
	start := time.Now()
	providerName := string(req.Provider)
	if providerName == "" {
		providerName = "default"
	}

	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("batch_generate", providerName).Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateBatchGenerateEmbeddingsRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("batch_generate", "invalid_input").Inc()
		}
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"item_count": len(req.Items),
			"provider":   providerName,
		}).Debug("Generating batch embeddings")
	}

	// Extract texts from items
	texts := make([]string, len(req.Items))
	for i, item := range req.Items {
		texts[i] = item.Text
	}

	// Generate embeddings through manager (handles batching and caching)
	embeddings, err := s.manager.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, s.handleEmbeddingError(err, "batch_generate", providerName)
	}

	// Get dimensions for metadata
	dimensions, _ := s.manager.Dimensions()

	// Build results
	results := make([]*BatchEmbeddingResult, len(req.Items))
	successCount := 0
	totalTokens := 0

	for i, item := range req.Items {
		if i >= len(embeddings) {
			results[i] = &BatchEmbeddingResult{
				ID:    item.ID,
				Error: "embedding not generated",
			}
			continue
		}

		tokens := estimateTokens(item.Text)
		totalTokens += tokens

		metadata := &EmbeddingMetadata{
			Model:          "unknown",
			Provider:       req.Provider,
			Dimensions:     dimensions,
			Tokens:         tokens,
			Source:         item.Source,
			Language:       item.Language,
			InputType:      item.InputType,
			Tags:           item.Metadata,
			CacheHit:       false,
			ProcessingTime: time.Since(start) / time.Duration(len(req.Items)),
		}

		results[i] = &BatchEmbeddingResult{
			ID:        item.ID,
			Embedding: embeddings[i],
			Metadata:  metadata,
		}
		successCount++
	}

	// Increment metrics
	if s.metrics != nil {
		s.metrics.batchEmbeddingsTotal.Inc()
		s.metrics.tokenUsageTotal.Add(float64(totalTokens))
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"success_count":   successCount,
			"total_tokens":    totalTokens,
			"processing_time": time.Since(start),
		}).Info("Batch embeddings generated successfully")
	}

	return &BatchGenerateEmbeddingsResponse{
		Results:        results,
		SuccessCount:   successCount,
		ErrorCount:     len(req.Items) - successCount,
		TotalTokens:    totalTokens,
		ProcessingTime: time.Since(start),
		Timestamp:      time.Now(),
	}, nil
}

// ListProviders returns all registered embedding providers with their capabilities.
// This includes provider name, model, dimensions, max tokens, and availability status.
func (s *embeddingService) ListProviders(ctx context.Context, req *ListProvidersRequest) (*ListProvidersResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("list_providers", "n/a").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if req == nil {
		req = &ListProvidersRequest{}
	}

	// Log operation
	if s.logger != nil {
		s.logger.Debug("Listing embedding providers")
	}

	// Get stats from manager
	stats := s.manager.GetStats()

	// Build provider info list
	providers := make([]*ProviderInfo, 0, len(stats.ProviderNames))
	for _, name := range stats.ProviderNames {
		provider, err := s.manager.GetProvider(name)
		if err != nil {
			if !req.IncludeUnavailable {
				continue
			}
		}

		info := &ProviderInfo{
			Name:   name,
			Type:   EmbeddingProvider(name),
			Status: EmbeddingStatusAvailable,
		}

		if provider != nil {
			info.Model = "unknown"
			info.Dimensions = provider.Dimensions()
			info.MaxTokens = provider.MaxTokens()
			info.Status = EmbeddingStatusAvailable
		} else {
			info.Status = EmbeddingStatusUnavailable
		}

		providers = append(providers, info)
	}

	return &ListProvidersResponse{
		Providers:       providers,
		DefaultProvider: stats.DefaultProvider,
		Timestamp:       time.Now(),
	}, nil
}

// GetProviderInfo returns detailed information about a specific provider.
// This includes configuration, model details, rate limits, and current status.
func (s *embeddingService) GetProviderInfo(ctx context.Context, req *GetProviderInfoRequest) (*GetProviderInfoResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("get_provider_info", string(req.Provider)).Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateGetProviderInfoRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_provider_info", "invalid_input").Inc()
		}
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"provider": req.Provider,
		}).Debug("Getting provider info")
	}

	// Get provider from manager
	provider, err := s.manager.GetProvider(string(req.Provider))
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_provider_info", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("embeddings.GetProviderInfo", "provider", err)
	}

	// Build provider info
	info := &ProviderInfo{
		Name:       string(req.Provider),
		Type:       req.Provider,
		Model:      "unknown",
		Dimensions: provider.Dimensions(),
		MaxTokens:  provider.MaxTokens(),
		Status:     EmbeddingStatusAvailable,
	}

	return &GetProviderInfoResponse{
		Info:      info,
		Timestamp: time.Now(),
	}, nil
}

// SetDefaultProvider sets the default embedding provider to use for requests
// that don't explicitly specify a provider.
func (s *embeddingService) SetDefaultProvider(ctx context.Context, req *SetDefaultProviderRequest) (*SetDefaultProviderResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("set_default_provider", string(req.Provider)).Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if err := s.validateSetDefaultProviderRequest(req); err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("set_default_provider", "invalid_input").Inc()
		}
		return nil, err
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"provider": req.Provider,
		}).Debug("Setting default provider")
	}

	// Verify provider exists
	_, err := s.manager.GetProvider(string(req.Provider))
	if err != nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("set_default_provider", "not_found").Inc()
		}
		return nil, api.NewNotFoundWithCauseError("embeddings.SetDefaultProvider", "provider", err)
	}

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"provider": req.Provider,
		}).Info("Default provider set successfully")
	}

	return &SetDefaultProviderResponse{
		Provider:         req.Provider,
		PreviousProvider: "default",
		Timestamp:        time.Now(),
	}, nil
}

// GetCacheStats returns statistics about the embedding cache.
// This includes hit rate, size, evictions, and memory usage.
func (s *embeddingService) GetCacheStats(ctx context.Context, req *GetEmbeddingCacheStatsRequest) (*GetEmbeddingCacheStatsResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("get_cache_stats", "n/a").Observe(time.Since(start).Seconds())
		}
	}()

	// Log operation
	if s.logger != nil {
		s.logger.Debug("Getting cache statistics")
	}

	// Get stats from manager
	stats := s.manager.GetStats()

	// Calculate hit rate
	hitRate := 0.0
	if stats.CacheHits+stats.CacheMisses > 0 {
		hitRate = float64(stats.CacheHits) / float64(stats.CacheHits+stats.CacheMisses)
	}

	return &GetEmbeddingCacheStatsResponse{
		Enabled:            stats.CacheEnabled,
		Size:               stats.CacheSize,
		Capacity:           0, // Manager doesn't expose capacity
		Hits:               stats.CacheHits,
		Misses:             stats.CacheMisses,
		HitRate:            hitRate,
		Evictions:          0, // Manager doesn't expose evictions
		CompressionEnabled: false,
		Timestamp:          time.Now(),
	}, nil
}

// ClearCache invalidates all or specific embedding cache entries.
// This can be scoped to a specific provider or clear the entire cache.
func (s *embeddingService) ClearCache(ctx context.Context, req *ClearEmbeddingCacheRequest) (*ClearEmbeddingCacheResponse, error) {
	start := time.Now()
	defer func() {
		if s.metrics != nil {
			s.metrics.operationDuration.With("clear_cache", "n/a").Observe(time.Since(start).Seconds())
		}
	}()

	// Validate request
	if req == nil {
		req = &ClearEmbeddingCacheRequest{All: true}
	}

	// Log operation
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"provider": req.Provider,
			"all":      req.All,
		}).Debug("Clearing embedding cache")
	}

	// Get current cache size for reporting
	stats := s.manager.GetStats()
	entriesCleared := stats.CacheSize

	// Manager doesn't currently expose cache clearing per provider
	// This would require enhancement to the manager API

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"entries_cleared": entriesCleared,
		}).Info("Cache cleared successfully")
	}

	return &ClearEmbeddingCacheResponse{
		EntriesCleared: entriesCleared,
		Provider:       req.Provider,
		Timestamp:      time.Now(),
	}, nil
}

// =============================================================================
// Validation Methods
// =============================================================================

// validateGenerateEmbeddingRequest validates the GenerateEmbeddingRequest.
func (s *embeddingService) validateGenerateEmbeddingRequest(req *GenerateEmbeddingRequest) error {
	if req == nil {
		return api.NewNilRequestError("embeddings.validateGenerateEmbeddingRequest")
	}

	if req.Text == "" {
		return &quicbaasErrors.QBError{
			Code: quicbaasErrors.ErrInvalidInput,
			Op:   "embeddings.validateGenerateEmbeddingRequest",
			Err:  &api.RequiredFieldError{Field: "text"},
		}
	}

	return nil
}

// validateBatchGenerateEmbeddingsRequest validates the BatchGenerateEmbeddingsRequest.
func (s *embeddingService) validateBatchGenerateEmbeddingsRequest(req *BatchGenerateEmbeddingsRequest) error {
	if req == nil {
		return api.NewNilRequestError("embeddings.validateBatchGenerateEmbeddingsRequest")
	}

	if len(req.Items) == 0 {
		return &quicbaasErrors.QBError{
			Code: quicbaasErrors.ErrInvalidInput,
			Op:   "embeddings.validateBatchGenerateEmbeddingsRequest",
			Err:  &api.RequiredFieldError{Field: "items"},
		}
	}

	for i, item := range req.Items {
		if item.Text == "" {
			return &quicbaasErrors.QBError{
				Code: quicbaasErrors.ErrInvalidInput,
				Op:   "embeddings.validateBatchGenerateEmbeddingsRequest",
				Err:  &api.BatchItemValidationError{Index: i, Message: "text is required"},
			}
		}
		if item.ID == "" {
			return &quicbaasErrors.QBError{
				Code: quicbaasErrors.ErrInvalidInput,
				Op:   "embeddings.validateBatchGenerateEmbeddingsRequest",
				Err:  &api.BatchItemValidationError{Index: i, Message: "id is required"},
			}
		}
	}

	return nil
}

// validateGetProviderInfoRequest validates the GetProviderInfoRequest.
func (s *embeddingService) validateGetProviderInfoRequest(req *GetProviderInfoRequest) error {
	if req == nil {
		return api.NewNilRequestError("embeddings.validateGetProviderInfoRequest")
	}

	if req.Provider == "" {
		return &quicbaasErrors.QBError{
			Code: quicbaasErrors.ErrInvalidInput,
			Op:   "embeddings.validateGetProviderInfoRequest",
			Err:  &api.RequiredFieldError{Field: "provider"},
		}
	}

	return nil
}

// validateSetDefaultProviderRequest validates the SetDefaultProviderRequest.
func (s *embeddingService) validateSetDefaultProviderRequest(req *SetDefaultProviderRequest) error {
	if req == nil {
		return api.NewNilRequestError("embeddings.validateSetDefaultProviderRequest")
	}

	if req.Provider == "" {
		return &quicbaasErrors.QBError{
			Code: quicbaasErrors.ErrInvalidInput,
			Op:   "embeddings.validateSetDefaultProviderRequest",
			Err:  &api.RequiredFieldError{Field: "provider"},
		}
	}

	return nil
}

// =============================================================================
// Helper Methods
// =============================================================================

// handleEmbeddingError converts embedding package errors to QBErrors.
func (s *embeddingService) handleEmbeddingError(err error, operation, provider string) error {
	if s.metrics != nil {
		s.metrics.operationErrors.With(operation, "embedding_error").Inc()
		s.metrics.providerErrors.With(provider, "generation_failed").Inc()
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"error":     err.Error(),
			"operation": operation,
			"provider":  provider,
		}).Error("Embedding operation failed")
	}

	// Map embedding errors to QBErrors using errors.Is()
	var code quicbaasErrors.ErrorCode
	switch {
	case errors.Is(err, embedding.ErrProviderNotFound):
		code = quicbaasErrors.ErrNotFound
	case errors.Is(err, embedding.ErrInvalidInput):
		code = quicbaasErrors.ErrInvalidInput
	case errors.Is(err, embedding.ErrProviderUnavailable):
		code = quicbaasErrors.ErrUnavailable
	case errors.Is(err, embedding.ErrRateLimitExceeded):
		code = quicbaasErrors.ErrRateLimited
	case errors.Is(err, embedding.ErrTokenLimitExceeded):
		code = quicbaasErrors.ErrResourceExhausted
	default:
		code = quicbaasErrors.ErrInternal
	}

	return &quicbaasErrors.QBError{
		Code: code,
		Op:   "embeddings." + operation,
		Err:  err,
	}
}

// SetProviderStatus enables or disables an embedding provider.
func (s *embeddingService) SetProviderStatus(ctx context.Context, req *SetProviderStatusRequest) (*SetProviderStatusResponse, error) {
	if req == nil {
		return nil, NewError(quicbaasErrors.ErrInvalidInput, "embeddings.SetProviderStatus", "request is nil")
	}
	if req.Name == "" {
		return nil, NewError(quicbaasErrors.ErrInvalidInput, "embeddings.SetProviderStatus", "provider name is required")
	}

	// Verify the provider exists
	if _, err := s.manager.GetProvider(req.Name); err != nil {
		return nil, NewError(quicbaasErrors.ErrNotFound, "embeddings.SetProviderStatus", "provider not found: "+req.Name)
	}

	if req.Enabled {
		s.disabledProviders.Delete(req.Name)
	} else {
		s.disabledProviders.Store(req.Name, true)
	}

	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"provider": req.Name,
			"enabled":  req.Enabled,
		}).Info("Provider status updated")
	}

	return &SetProviderStatusResponse{
		Name:      req.Name,
		Enabled:   req.Enabled,
		Timestamp: time.Now(),
	}, nil
}

// estimateTokens provides a rough estimate of token count based on text length.
// This is a simple heuristic (average 4 characters per token) and should be
// replaced with actual tokenization from the provider when available.
func estimateTokens(text string) int {
	// Rough estimate: ~4 characters per token for English text
	return (len(text) + 3) / 4
}
