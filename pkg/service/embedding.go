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
	"sync/atomic"
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
	manager           interfaces.EmbeddingManager
	logger            interfaces.Logger
	metrics           *embeddingMetrics
	disabledProviders sync.Map // tracks provider name (string) → true when disabled

	// disabledCount is the number of entries in disabledProviders. sync.Map has
	// no length, and the disabled set is empty in the overwhelmingly common
	// case, so this lets the embed path skip resolving the manager's default
	// name entirely rather than paying a GetStats call per request.
	disabledCount atomic.Int64

	// defaultProvider overrides the manager's configured default when set by
	// SetDefaultProvider. Held atomically because it is read on every embed
	// call and written rarely. Nil or empty means "use the manager's default".
	//
	// This exists because the manager's own default is fixed at construction
	// from configuration, and go-qrdb validates that name against a closed
	// allowlist -- so a provider registered afterwards could otherwise never
	// become the effective default. See go-qrdb#528.
	defaultProvider atomic.Pointer[EmbeddingProvider]
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
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("generate", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("embeddings.GenerateEmbedding")
	}
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

	// Generate the embedding, honouring an explicitly requested provider.
	embeddingVec, routedDimensions, err := s.embedOne(ctx, req.Provider, req.Text)
	if err != nil {
		return nil, s.handleEmbeddingError(err, "generate", providerName)
	}

	// Check if this was a cache hit by comparing stats
	statsAfter := s.manager.GetStats()
	cacheHit := statsAfter.CacheHits > cacheHitsBefore

	// Get dimensions for metadata. A routed request reports the dimensions of
	// the provider that actually served it, not the default provider's.
	dimensions := routedDimensions
	if dimensions <= 0 {
		dimensions, _ = s.manager.Dimensions()
	}

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
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("batch_generate", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("embeddings.BatchGenerateEmbeddings")
	}
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

	// Generate the embeddings, honouring an explicitly requested provider.
	embeddings, routedDimensions, err := s.embedMany(ctx, req.Provider, texts)
	if err != nil {
		return nil, s.handleEmbeddingError(err, "batch_generate", providerName)
	}

	// Get dimensions for metadata. A routed request reports the dimensions of
	// the provider that actually served it, not the default provider's.
	dimensions := routedDimensions
	if dimensions <= 0 {
		dimensions, _ = s.manager.Dimensions()
	}

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
		DefaultProvider: s.reportedDefaultProvider(stats.DefaultProvider),
		Timestamp:       time.Now(),
	}, nil
}

// GetProviderInfo returns detailed information about a specific provider.
// This includes configuration, model details, rate limits, and current status.
func (s *embeddingService) GetProviderInfo(ctx context.Context, req *GetProviderInfoRequest) (*GetProviderInfoResponse, error) {
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("get_provider_info", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("embeddings.GetProviderInfo")
	}
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
	if req == nil {
		if s.metrics != nil {
			s.metrics.operationErrors.With("set_default_provider", "invalid_input").Inc()
		}
		return nil, api.NewNilRequestError("embeddings.SetDefaultProvider")
	}
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

	// Refuse to make a disabled provider the default; it would be excluded from
	// routing the moment it was selected.
	if s.providerDisabled(string(req.Provider)) {
		if s.metrics != nil {
			s.metrics.operationErrors.With("set_default_provider", "disabled").Inc()
		}
		return nil, &ProviderDisabledError{Provider: string(req.Provider)}
	}

	// Record the change. Previously this returned success without mutating
	// anything, so the endpoint reported a switch that never happened.
	previous := s.effectiveDefaultProvider()
	selected := req.Provider
	s.defaultProvider.Store(&selected)

	// Log success
	if s.logger != nil {
		s.logger.WithFields(map[string]interface{}{
			"provider": req.Provider,
			"previous": string(previous),
		}).Info("Default provider set successfully")
	}

	if previous == "" {
		// No override was in force, so the manager's configured default was
		// serving. "default" is the established sentinel for that; naming it
		// would mean an extra manager round-trip on a write path.
		previous = "default"
	}

	return &SetDefaultProviderResponse{
		Provider:         req.Provider,
		PreviousProvider: previous,
		Timestamp:        time.Now(),
	}, nil
}

// reportedDefaultProvider returns the default a caller should be told about:
// the service-level override when one is in force, else the manager's own.
func (s *embeddingService) reportedDefaultProvider(managerDefault string) string {
	if override := s.effectiveDefaultProvider(); override != "" {
		return string(override)
	}
	return managerDefault
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
// ProviderDisabledError indicates a request named a provider that an operator
// has disabled through SetProviderStatus.
//
// It unwraps to embedding.ErrProviderUnavailable so that callers matching the
// general embedding error contract, and handleEmbeddingError's mapping to
// ErrUnavailable, both keep working.
type ProviderDisabledError struct {
	// Provider is the disabled provider that was requested.
	Provider string
}

// Error implements the error interface.
func (e *ProviderDisabledError) Error() string {
	return "embeddings: provider is disabled: " + e.Provider
}

// Unwrap returns the shared unavailable sentinel for errors.Is support.
func (e *ProviderDisabledError) Unwrap() error {
	return embedding.ErrProviderUnavailable
}

// providerDisabled reports whether an operator has disabled the named provider.
//
// SetProviderStatus records the disabled set, and the service contract says
// disabled providers are "excluded from request routing and fallback chains" --
// so this is consulted before a request is routed. Without it, disabling a
// provider whose backend is down would silently do nothing, which is the first
// thing an operator reaches for during an outage.
func (s *embeddingService) providerDisabled(name string) bool {
	_, disabled := s.disabledProviders.Load(name)
	return disabled
}

// managerDefaultDisabled reports whether the manager's own configured default
// has been disabled.
//
// An unnamed request routes through the manager's cached default path, which
// never names a provider -- so without this check, disabling the default
// provider would record the intent and change nothing, and default traffic would
// keep hitting the backend an operator was trying to take out of service during
// an outage. That is the failure mode the disabled set exists to prevent.
//
// The disabled set is empty in the common case, so the counter short-circuits
// before any manager round-trip.
func (s *embeddingService) managerDefaultDisabled() (string, bool) {
	if s.disabledCount.Load() == 0 {
		return "", false
	}

	name := s.manager.GetStats().DefaultProvider
	if name == "" {
		// The manager does not report a default name, so there is nothing to
		// match against the disabled set.
		return "", false
	}
	return name, s.providerDisabled(name)
}

// resolveProvider returns the provider a request should run against: the one it
// named, else the service-level default set by SetDefaultProvider, else empty
// to mean the manager's configured default.
func (s *embeddingService) resolveProvider(requested EmbeddingProvider) EmbeddingProvider {
	if requested != "" {
		return requested
	}
	return s.effectiveDefaultProvider()
}

// effectiveDefaultProvider returns the service-level default override, or the
// empty string when none has been set.
func (s *embeddingService) effectiveDefaultProvider() EmbeddingProvider {
	if p := s.defaultProvider.Load(); p != nil {
		return *p
	}
	return ""
}

// embedOne generates a single embedding, routing to an explicitly requested
// provider when the request names one.
//
// GenerateEmbeddingRequest.Provider is a documented part of the public API --
// "optionally specifies which provider to use (uses default if empty)" -- and
// was previously recorded for metrics but never acted on, so every request ran
// the manager's configured default. That made any registered non-default
// provider unreachable through this service.
//
// The returned dimension count is the serving provider's, so metadata cannot
// claim the default provider's width for a vector produced by another.
// It is zero when the manager's default served the request, in which case the
// caller falls back to the manager's own reported dimensions.
//
// Routing directly to a provider bypasses the manager's cache, because the
// cache belongs to the manager and is keyed for its default provider. A routed
// request is therefore always a cache miss, which the reported metadata
// reflects honestly.
func (s *embeddingService) embedOne(
	ctx context.Context,
	provider EmbeddingProvider,
	text string,
) ([]float32, int, error) {
	provider = s.resolveProvider(provider)
	if provider == "" {
		if name, disabled := s.managerDefaultDisabled(); disabled {
			return nil, 0, &ProviderDisabledError{Provider: name}
		}
		vec, err := s.manager.Embed(ctx, text)
		return vec, 0, err
	}

	if s.providerDisabled(string(provider)) {
		return nil, 0, &ProviderDisabledError{Provider: string(provider)}
	}

	target, err := s.manager.GetProvider(string(provider))
	if err != nil {
		return nil, 0, err
	}
	if target == nil {
		return nil, 0, embedding.ErrProviderNotFound
	}

	vec, err := target.Embed(ctx, text)
	if err != nil {
		return nil, 0, err
	}
	return vec, target.Dimensions(), nil
}

// embedMany is the batch counterpart of embedOne and follows the same routing
// and cache rules.
func (s *embeddingService) embedMany(
	ctx context.Context,
	provider EmbeddingProvider,
	texts []string,
) ([][]float32, int, error) {
	provider = s.resolveProvider(provider)
	if provider == "" {
		if name, disabled := s.managerDefaultDisabled(); disabled {
			return nil, 0, &ProviderDisabledError{Provider: name}
		}
		vecs, err := s.manager.EmbedBatch(ctx, texts)
		return vecs, 0, err
	}

	if s.providerDisabled(string(provider)) {
		return nil, 0, &ProviderDisabledError{Provider: string(provider)}
	}

	target, err := s.manager.GetProvider(string(provider))
	if err != nil {
		return nil, 0, err
	}
	if target == nil {
		return nil, 0, embedding.ErrProviderNotFound
	}

	vecs, err := target.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, 0, err
	}
	return vecs, target.Dimensions(), nil
}

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
		if _, existed := s.disabledProviders.LoadAndDelete(req.Name); existed {
			s.disabledCount.Add(-1)
		}
	} else {
		if _, existed := s.disabledProviders.LoadOrStore(req.Name, true); !existed {
			s.disabledCount.Add(1)
		}
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
