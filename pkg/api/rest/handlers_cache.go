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

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// CacheStatsResponse represents the REST API response for cache statistics.
type CacheStatsResponse struct {
	Enabled     bool    `json:"enabled"`
	L1Size      int64   `json:"l1_size"`
	L1Capacity  int64   `json:"l1_capacity"`
	L1Hits      int64   `json:"l1_hits"`
	L1Misses    int64   `json:"l1_misses"`
	L1HitRate   float64 `json:"l1_hit_rate"`
	L1Evictions int64   `json:"l1_evictions"`
}

// ClearCacheResponse represents the REST API response for clearing cache.
type ClearCacheResponse struct {
	Success        bool  `json:"success"`
	EntriesCleared int32 `json:"entries_cleared"`
}

// CacheConfigResponse represents the REST API response for cache configuration.
type CacheConfigResponse struct {
	Enabled                bool  `json:"enabled"`
	L1Size                 int32 `json:"l1_size"`
	L1TTLSeconds           int64 `json:"l1_ttl_seconds"`
	CleanupIntervalSeconds int64 `json:"cleanup_interval_seconds"`
}

// UpdateCacheConfigRequest represents the REST API request for updating cache configuration.
type UpdateCacheConfigRequest struct {
	L1Size       *int32 `json:"l1_size,omitempty"`
	L1TTLSeconds *int64 `json:"l1_ttl_seconds,omitempty"`
}

// UpdateCacheConfigResponse represents the REST API response for updating cache configuration.
type UpdateCacheConfigResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// registerCacheRoutes registers cache-related routes.
func registerCacheRoutes(s *Server, r interface {
	Get(pattern string, h http.HandlerFunc)
	Post(pattern string, h http.HandlerFunc)
	Put(pattern string, h http.HandlerFunc)
}) {
	r.Get("/cache/stats", s.HandleGetCacheStats)
	r.Post("/cache/clear", s.HandleClearCache)
	r.Get("/cache/config", s.HandleGetCacheConfig)
	r.Put("/cache/config", s.HandleUpdateCacheConfig)
}

// HandleGetCacheStats returns cache statistics.
// GET /api/v1/cache/stats.
func (s *Server) HandleGetCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Call service layer
	resp, err := s.service.GetCacheStats(ctx, &service.GetCacheStatsRequest{})
	if err != nil {
		s.logger.Errorf("Failed to get cache stats: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	restResp := &CacheStatsResponse{
		Enabled:     true,
		L1Size:      int64(resp.Size),
		L1Capacity:  int64(resp.MaxSize),
		L1Hits:      int64(resp.Hits),
		L1Misses:    int64(resp.Misses),
		L1HitRate:   resp.HitRate,
		L1Evictions: int64(resp.Evictions),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}

// HandleClearCache clears the cache.
// POST /api/v1/cache/clear.
func (s *Server) HandleClearCache(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Call service layer
	resp, err := s.service.ClearCache(ctx, &service.ClearCacheRequest{All: true})
	if err != nil {
		s.logger.Errorf("Failed to clear cache: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	restResp := &ClearCacheResponse{
		Success:        true,
		EntriesCleared: int32(resp.EntriesCleared),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}

// HandleGetCacheConfig returns cache configuration.
// GET /api/v1/cache/config.
func (s *Server) HandleGetCacheConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Call service layer
	resp, err := s.service.GetCacheConfig(ctx, &service.GetCacheConfigRequest{})
	if err != nil {
		s.logger.Errorf("Failed to get cache config: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert to REST response
	restResp := &CacheConfigResponse{
		Enabled:                resp.Enabled,
		L1Size:                 int32(resp.L1Size),
		L1TTLSeconds:           int64(resp.L1TTL.Seconds()),
		CleanupIntervalSeconds: int64(resp.CleanupInterval.Seconds()),
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}

// HandleUpdateCacheConfig updates cache configuration.
// PUT /api/v1/cache/config.
func (s *Server) HandleUpdateCacheConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()

	// Parse request
	var req UpdateCacheConfigRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Build service request
	serviceReq := &service.UpdateCacheConfigRequest{}

	if req.L1Size != nil {
		size := int(*req.L1Size)
		serviceReq.L1Size = &size
	}

	if req.L1TTLSeconds != nil {
		ttl := time.Duration(*req.L1TTLSeconds) * time.Second
		serviceReq.L1TTL = &ttl
	}

	// Call service layer
	resp, err := s.service.UpdateCacheConfig(ctx, serviceReq)
	if err != nil {
		s.logger.Errorf("Failed to update cache config: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	msg := "Cache configuration unchanged"
	if resp.Updated {
		msg = "Cache configuration updated successfully"
	}

	// Convert to REST response
	restResp := &UpdateCacheConfigResponse{
		Success: resp.Updated,
		Message: msg,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(restResp)
}
