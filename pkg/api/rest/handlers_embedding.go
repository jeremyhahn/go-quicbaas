//go:build rest

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

package rest

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ========================================
// Embedding Route Registration
// ========================================

// registerEmbeddingRoutes registers all Embedding-related routes.
func registerEmbeddingRoutes(s *Server, r chi.Router) {
	r.Route("/embedding", func(r chi.Router) {
		// Embedding generation
		r.Post("/generate", s.HandleEmbeddingGenerate)
		r.Post("/batch", s.HandleEmbeddingBatchGenerate)

		// Provider management
		r.Get("/providers", s.HandleEmbeddingListProviders)
		r.Get("/providers/{provider}", s.HandleEmbeddingGetProviderInfo)
		r.Put("/providers/default", s.HandleEmbeddingSetDefaultProvider)

		// Cache management
		r.Get("/cache/stats", s.HandleEmbeddingGetCacheStats)
		r.Delete("/cache", s.HandleEmbeddingClearCache)
	})
}

// ========================================
// Request/Response Types
// ========================================

// EmbeddingGenerateRequest is the request body for generating a single embedding.
type EmbeddingGenerateRequest struct {
	Text        string                     `json:"text"`
	Provider    service.EmbeddingProvider  `json:"provider,omitempty"`
	InputType   service.EmbeddingInputType `json:"input_type,omitempty"`
	Source      string                     `json:"source,omitempty"`
	Language    string                     `json:"language,omitempty"`
	Metadata    map[string]string          `json:"metadata,omitempty"`
	CacheBypass bool                       `json:"cache_bypass,omitempty"`
}

// EmbeddingGenerateResponse is the response for embedding generation.
type EmbeddingGenerateResponse struct {
	Embedding      []float32                 `json:"embedding"`
	Model          string                    `json:"model"`
	Provider       service.EmbeddingProvider `json:"provider"`
	Dimensions     int                       `json:"dimensions"`
	Tokens         int                       `json:"tokens"`
	CacheHit       bool                      `json:"cache_hit"`
	ProcessingTime string                    `json:"processing_time"`
	Timestamp      time.Time                 `json:"timestamp"`
}

// EmbeddingBatchItem represents a single item in a batch embedding request.
type EmbeddingBatchItem struct {
	ID        string                     `json:"id"`
	Text      string                     `json:"text"`
	InputType service.EmbeddingInputType `json:"input_type,omitempty"`
	Source    string                     `json:"source,omitempty"`
	Language  string                     `json:"language,omitempty"`
	Metadata  map[string]string          `json:"metadata,omitempty"`
}

// EmbeddingBatchGenerateRequest is the request body for batch embedding generation.
type EmbeddingBatchGenerateRequest struct {
	Items       []*EmbeddingBatchItem     `json:"items"`
	Provider    service.EmbeddingProvider `json:"provider,omitempty"`
	CacheBypass bool                      `json:"cache_bypass,omitempty"`
}

// EmbeddingBatchResult represents the result for one batch item.
type EmbeddingBatchResult struct {
	ID             string                    `json:"id"`
	Embedding      []float32                 `json:"embedding"`
	Model          string                    `json:"model"`
	Provider       service.EmbeddingProvider `json:"provider"`
	Dimensions     int                       `json:"dimensions"`
	Tokens         int                       `json:"tokens"`
	CacheHit       bool                      `json:"cache_hit"`
	ProcessingTime string                    `json:"processing_time"`
	Error          string                    `json:"error,omitempty"`
}

// EmbeddingBatchGenerateResponse is the response for batch embedding generation.
type EmbeddingBatchGenerateResponse struct {
	Results        []*EmbeddingBatchResult `json:"results"`
	SuccessCount   int                     `json:"success_count"`
	ErrorCount     int                     `json:"error_count"`
	TotalTokens    int                     `json:"total_tokens"`
	ProcessingTime string                  `json:"processing_time"`
	Timestamp      time.Time               `json:"timestamp"`
}

// EmbeddingProviderInfoResponse is the response for provider information.
type EmbeddingProviderInfoResponse struct {
	Name               string                    `json:"name"`
	Type               service.EmbeddingProvider `json:"type"`
	Model              string                    `json:"model"`
	Dimensions         int                       `json:"dimensions"`
	MaxTokens          int                       `json:"max_tokens"`
	MaxBatchSize       int                       `json:"max_batch_size"`
	Status             service.EmbeddingStatus   `json:"status"`
	APIURL             string                    `json:"api_url"`
	RateLimitPerMinute int                       `json:"rate_limit_per_minute,omitempty"`
	Timeout            string                    `json:"timeout"`
	IsDefault          bool                      `json:"is_default"`
	LastError          string                    `json:"last_error,omitempty"`
	LastErrorTime      *time.Time                `json:"last_error_time,omitempty"`
	SuccessCount       uint64                    `json:"success_count"`
	ErrorCount         uint64                    `json:"error_count"`
	AvgLatency         string                    `json:"avg_latency"`
	Timestamp          time.Time                 `json:"timestamp"`
}

// EmbeddingListProvidersResponse is the response for listing providers.
type EmbeddingListProvidersResponse struct {
	Providers       []*EmbeddingProviderInfoResponse `json:"providers"`
	DefaultProvider string                           `json:"default_provider"`
	Timestamp       time.Time                        `json:"timestamp"`
}

// EmbeddingSetDefaultProviderRequest is the request to set the default provider.
type EmbeddingSetDefaultProviderRequest struct {
	Provider service.EmbeddingProvider `json:"provider"`
}

// EmbeddingSetDefaultProviderResponse is the response after setting default provider.
type EmbeddingSetDefaultProviderResponse struct {
	Provider         service.EmbeddingProvider `json:"provider"`
	PreviousProvider service.EmbeddingProvider `json:"previous_provider"`
	Timestamp        time.Time                 `json:"timestamp"`
}

// EmbeddingCacheStatsResponse is the response for cache statistics.
type EmbeddingCacheStatsResponse struct {
	Enabled            bool      `json:"enabled"`
	Size               int       `json:"size"`
	Capacity           int       `json:"capacity"`
	Hits               uint64    `json:"hits"`
	Misses             uint64    `json:"misses"`
	HitRate            float64   `json:"hit_rate"`
	Evictions          uint64    `json:"evictions"`
	MemoryBytes        uint64    `json:"memory_bytes,omitempty"`
	CompressionEnabled bool      `json:"compression_enabled"`
	CompressionRatio   float64   `json:"compression_ratio,omitempty"`
	Timestamp          time.Time `json:"timestamp"`
}

// EmbeddingClearCacheRequest is the request to clear the cache.
type EmbeddingClearCacheRequest struct {
	Provider service.EmbeddingProvider `json:"provider,omitempty"`
	All      bool                      `json:"all,omitempty"`
}

// EmbeddingClearCacheResponse is the response after clearing the cache.
type EmbeddingClearCacheResponse struct {
	EntriesCleared int                       `json:"entries_cleared"`
	Provider       service.EmbeddingProvider `json:"provider,omitempty"`
	Timestamp      time.Time                 `json:"timestamp"`
}

// ========================================
// Embedding Generation Handlers
// ========================================

// HandleEmbeddingGenerate generates an embedding for a single text.
func (s *Server) HandleEmbeddingGenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req EmbeddingGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Text == "" {
		RespondBadRequest(w, r, "text is required")
		return
	}

	embeddingSvc := s.service.Embedding()
	if embeddingSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "embedding service not configured",
		})
		return
	}

	// Build service request
	svcReq := &service.GenerateEmbeddingRequest{
		Text:        req.Text,
		Provider:    req.Provider,
		InputType:   req.InputType,
		Source:      req.Source,
		Language:    req.Language,
		Metadata:    req.Metadata,
		CacheBypass: req.CacheBypass,
	}

	// Call service
	svcResp, err := embeddingSvc.GenerateEmbedding(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Build response
	resp := EmbeddingGenerateResponse{
		Embedding:      svcResp.Embedding,
		Model:          svcResp.Metadata.Model,
		Provider:       svcResp.Metadata.Provider,
		Dimensions:     svcResp.Metadata.Dimensions,
		Tokens:         svcResp.Metadata.Tokens,
		CacheHit:       svcResp.Metadata.CacheHit,
		ProcessingTime: svcResp.Metadata.ProcessingTime.String(),
		Timestamp:      svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleEmbeddingBatchGenerate generates embeddings for multiple texts.
func (s *Server) HandleEmbeddingBatchGenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req EmbeddingBatchGenerateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if len(req.Items) == 0 {
		RespondBadRequest(w, r, "items is required and cannot be empty")
		return
	}

	embeddingSvc := s.service.Embedding()
	if embeddingSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "embedding service not configured",
		})
		return
	}

	// Convert request items to service items
	svcItems := make([]*service.BatchEmbeddingItem, len(req.Items))
	for i, item := range req.Items {
		if item.Text == "" {
			RespondBadRequest(w, r, "text is required for each item")
			return
		}
		svcItems[i] = &service.BatchEmbeddingItem{
			ID:        item.ID,
			Text:      item.Text,
			InputType: item.InputType,
			Source:    item.Source,
			Language:  item.Language,
			Metadata:  item.Metadata,
		}
	}

	// Build service request
	svcReq := &service.BatchGenerateEmbeddingsRequest{
		Items:       svcItems,
		Provider:    req.Provider,
		CacheBypass: req.CacheBypass,
	}

	// Call service
	svcResp, err := embeddingSvc.BatchGenerateEmbeddings(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert service results to response
	results := make([]*EmbeddingBatchResult, len(svcResp.Results))
	for i, result := range svcResp.Results {
		r := &EmbeddingBatchResult{
			ID:        result.ID,
			Embedding: result.Embedding,
			Error:     result.Error,
		}
		if result.Metadata != nil {
			r.Model = result.Metadata.Model
			r.Provider = result.Metadata.Provider
			r.Dimensions = result.Metadata.Dimensions
			r.Tokens = result.Metadata.Tokens
			r.CacheHit = result.Metadata.CacheHit
			r.ProcessingTime = result.Metadata.ProcessingTime.String()
		}
		results[i] = r
	}

	// Build response
	resp := EmbeddingBatchGenerateResponse{
		Results:        results,
		SuccessCount:   svcResp.SuccessCount,
		ErrorCount:     svcResp.ErrorCount,
		TotalTokens:    svcResp.TotalTokens,
		ProcessingTime: svcResp.ProcessingTime.String(),
		Timestamp:      svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Provider Management Handlers
// ========================================

// HandleEmbeddingListProviders returns all registered embedding providers.
func (s *Server) HandleEmbeddingListProviders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	embeddingSvc := s.service.Embedding()
	if embeddingSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "embedding service not configured",
		})
		return
	}

	// Parse query parameters
	includeUnavailable := r.URL.Query().Get("include_unavailable") == "true"

	// Build service request
	svcReq := &service.ListProvidersRequest{
		IncludeUnavailable: includeUnavailable,
	}

	// Call service
	svcResp, err := embeddingSvc.ListProviders(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert service response to REST response
	providers := make([]*EmbeddingProviderInfoResponse, len(svcResp.Providers))
	for i, p := range svcResp.Providers {
		providers[i] = convertProviderInfoToResponse(p)
	}

	resp := EmbeddingListProvidersResponse{
		Providers:       providers,
		DefaultProvider: svcResp.DefaultProvider,
		Timestamp:       svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleEmbeddingGetProviderInfo returns detailed information about a specific provider.
func (s *Server) HandleEmbeddingGetProviderInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	provider := chi.URLParam(r, "provider")
	if provider == "" {
		RespondBadRequest(w, r, "provider is required")
		return
	}

	embeddingSvc := s.service.Embedding()
	if embeddingSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "embedding service not configured",
		})
		return
	}

	// Build service request
	svcReq := &service.GetProviderInfoRequest{
		Provider: service.EmbeddingProvider(provider),
	}

	// Call service
	svcResp, err := embeddingSvc.GetProviderInfo(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := convertProviderInfoToResponse(svcResp.Info)
	resp.Timestamp = svcResp.Timestamp

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleEmbeddingSetDefaultProvider sets the default embedding provider.
func (s *Server) HandleEmbeddingSetDefaultProvider(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Parse request body
	var req EmbeddingSetDefaultProviderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Validate request
	if req.Provider == "" {
		RespondBadRequest(w, r, "provider is required")
		return
	}

	embeddingSvc := s.service.Embedding()
	if embeddingSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "embedding service not configured",
		})
		return
	}

	// Build service request
	svcReq := &service.SetDefaultProviderRequest{
		Provider: req.Provider,
	}

	// Call service
	svcResp, err := embeddingSvc.SetDefaultProvider(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := EmbeddingSetDefaultProviderResponse{
		Provider:         svcResp.Provider,
		PreviousProvider: svcResp.PreviousProvider,
		Timestamp:        svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Cache Management Handlers
// ========================================

// HandleEmbeddingGetCacheStats returns statistics about the embedding cache.
func (s *Server) HandleEmbeddingGetCacheStats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	embeddingSvc := s.service.Embedding()
	if embeddingSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "embedding service not configured",
		})
		return
	}

	// Build service request
	svcReq := &service.GetEmbeddingCacheStatsRequest{}

	// Call service
	svcResp, err := embeddingSvc.GetCacheStats(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := EmbeddingCacheStatsResponse{
		Enabled:            svcResp.Enabled,
		Size:               svcResp.Size,
		Capacity:           svcResp.Capacity,
		Hits:               svcResp.Hits,
		Misses:             svcResp.Misses,
		HitRate:            svcResp.HitRate,
		Evictions:          svcResp.Evictions,
		MemoryBytes:        svcResp.MemoryBytes,
		CompressionEnabled: svcResp.CompressionEnabled,
		CompressionRatio:   svcResp.CompressionRatio,
		Timestamp:          svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleEmbeddingClearCache clears the embedding cache.
func (s *Server) HandleEmbeddingClearCache(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	embeddingSvc := s.service.Embedding()
	if embeddingSvc == nil {
		RespondError(w, r, http.StatusServiceUnavailable, &ValidationError{
			Message: "embedding service not configured",
		})
		return
	}

	// Parse request body (optional)
	var req EmbeddingClearCacheRequest
	if r.Body != nil && r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	// Build service request
	svcReq := &service.ClearEmbeddingCacheRequest{
		Provider: req.Provider,
		All:      req.All,
	}

	// Call service
	svcResp, err := embeddingSvc.ClearCache(ctx, svcReq)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	resp := EmbeddingClearCacheResponse{
		EntriesCleared: svcResp.EntriesCleared,
		Provider:       svcResp.Provider,
		Timestamp:      svcResp.Timestamp,
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// ========================================
// Helper Functions
// ========================================

// convertProviderInfoToResponse converts service ProviderInfo to REST response.
func convertProviderInfoToResponse(p *service.ProviderInfo) *EmbeddingProviderInfoResponse {
	if p == nil {
		return nil
	}
	return &EmbeddingProviderInfoResponse{
		Name:               p.Name,
		Type:               p.Type,
		Model:              p.Model,
		Dimensions:         p.Dimensions,
		MaxTokens:          p.MaxTokens,
		MaxBatchSize:       p.MaxBatchSize,
		Status:             p.Status,
		APIURL:             p.APIURL,
		RateLimitPerMinute: p.RateLimitPerMinute,
		Timeout:            p.Timeout.String(),
		IsDefault:          p.IsDefault,
		LastError:          p.LastError,
		LastErrorTime:      p.LastErrorTime,
		SuccessCount:       p.SuccessCount,
		ErrorCount:         p.ErrorCount,
		AvgLatency:         p.AvgLatency.String(),
	}
}
