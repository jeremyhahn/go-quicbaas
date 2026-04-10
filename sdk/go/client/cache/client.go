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

package cache

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Cache"
)

// Client provides cache management operations.
type Client interface {
	// GetCacheStats returns current cache statistics.
	GetCacheStats(ctx context.Context, req *GetCacheStatsRequest) (*GetCacheStatsResponse, error)

	// ClearCache invalidates all or specific cache entries.
	ClearCache(ctx context.Context, req *ClearCacheRequest) (*ClearCacheResponse, error)

	// GetCacheConfig returns the current cache configuration.
	GetCacheConfig(ctx context.Context, req *GetCacheConfigRequest) (*GetCacheConfigResponse, error)

	// UpdateCacheConfig updates the cache configuration.
	UpdateCacheConfig(ctx context.Context, req *UpdateCacheConfigRequest) (*UpdateCacheConfigResponse, error)
}

// GetCacheStatsRequest is a request to retrieve cache statistics.
type GetCacheStatsRequest struct {
	// EntityType optionally filters stats to a specific entity type.
	EntityType string
}

// GetCacheStatsResponse contains cache statistics.
type GetCacheStatsResponse struct {
	// HitRate is the cache hit ratio (0.0 to 1.0).
	HitRate float64

	// Size is the current number of entries in the cache.
	Size uint64

	// MaxSize is the maximum number of entries the cache can hold.
	MaxSize uint64

	// Evictions is the total number of entries evicted.
	Evictions uint64

	// Hits is the total number of cache hits.
	Hits uint64

	// Misses is the total number of cache misses.
	Misses uint64
}

// ClearCacheRequest is a request to clear cache entries.
type ClearCacheRequest struct {
	// EntityType optionally clears only entries for a specific entity type.
	EntityType string

	// All clears all cache entries when true.
	All bool
}

// ClearCacheResponse contains the result of a cache clear operation.
type ClearCacheResponse struct {
	// EntriesCleared is the number of entries removed from the cache.
	EntriesCleared uint64
}

// GetCacheConfigRequest is a request to retrieve cache configuration.
type GetCacheConfigRequest struct{}

// GetCacheConfigResponse contains the current cache configuration.
type GetCacheConfigResponse struct {
	// L1Size is the maximum number of entries in the L1 cache.
	L1Size int

	// L1TTL is the time-to-live for L1 cache entries.
	L1TTL time.Duration

	// CleanupInterval is the interval between cache cleanup cycles.
	CleanupInterval time.Duration

	// Enabled indicates whether caching is enabled.
	Enabled bool
}

// UpdateCacheConfigRequest is a request to update cache configuration.
type UpdateCacheConfigRequest struct {
	// L1Size sets the maximum number of entries in the L1 cache.
	// Only updated if non-nil.
	L1Size *int

	// L1TTL sets the time-to-live for L1 cache entries.
	// Only updated if non-nil.
	L1TTL *time.Duration

	// CleanupInterval sets the interval between cache cleanup cycles.
	// Only updated if non-nil.
	CleanupInterval *time.Duration
}

// UpdateCacheConfigResponse contains the result of a configuration update.
type UpdateCacheConfigResponse struct {
	// Updated indicates whether the configuration was successfully updated.
	Updated bool

	// NewConfig contains the new cache configuration after the update.
	NewConfig *GetCacheConfigResponse
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new Cache client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// GetCacheStats returns current cache statistics.
func (c *client) GetCacheStats(ctx context.Context, req *GetCacheStatsRequest) (*GetCacheStatsResponse, error) {
	if err := validateGetCacheStatsRequest(ctx, req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetCacheStats", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("cache.GetCacheStats", "nil response from server")
	}

	statsResp, ok := resp.(*GetCacheStatsResponse)
	if !ok {
		return nil, errors.NewInternal("cache.GetCacheStats", "invalid response type")
	}

	return statsResp, nil
}

// ClearCache invalidates all or specific cache entries.
func (c *client) ClearCache(ctx context.Context, req *ClearCacheRequest) (*ClearCacheResponse, error) {
	if err := validateClearCacheRequest(ctx, req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ClearCache", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("cache.ClearCache", "nil response from server")
	}

	clearResp, ok := resp.(*ClearCacheResponse)
	if !ok {
		return nil, errors.NewInternal("cache.ClearCache", "invalid response type")
	}

	return clearResp, nil
}

// GetCacheConfig returns the current cache configuration.
func (c *client) GetCacheConfig(ctx context.Context, req *GetCacheConfigRequest) (*GetCacheConfigResponse, error) {
	if err := validateGetCacheConfigRequest(ctx, req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetCacheConfig", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("cache.GetCacheConfig", "nil response from server")
	}

	configResp, ok := resp.(*GetCacheConfigResponse)
	if !ok {
		return nil, errors.NewInternal("cache.GetCacheConfig", "invalid response type")
	}

	return configResp, nil
}

// UpdateCacheConfig updates the cache configuration.
func (c *client) UpdateCacheConfig(ctx context.Context, req *UpdateCacheConfigRequest) (*UpdateCacheConfigResponse, error) {
	if err := validateUpdateCacheConfigRequest(ctx, req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpdateCacheConfig", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("cache.UpdateCacheConfig", "nil response from server")
	}

	updateResp, ok := resp.(*UpdateCacheConfigResponse)
	if !ok {
		return nil, errors.NewInternal("cache.UpdateCacheConfig", "invalid response type")
	}

	return updateResp, nil
}

// Validation functions

func validateGetCacheStatsRequest(ctx context.Context, req *GetCacheStatsRequest) error {
	if ctx == nil {
		return errors.NewInvalidInput("cache.GetCacheStats", "context is required")
	}
	if req == nil {
		return errors.NewInvalidInput("cache.GetCacheStats", "request is required")
	}
	return nil
}

func validateClearCacheRequest(ctx context.Context, req *ClearCacheRequest) error {
	if ctx == nil {
		return errors.NewInvalidInput("cache.ClearCache", "context is required")
	}
	if req == nil {
		return errors.NewInvalidInput("cache.ClearCache", "request is required")
	}
	// Validate that at least one of EntityType or All is specified
	if req.EntityType == "" && !req.All {
		return errors.NewInvalidInput("cache.ClearCache", "either entity type or all flag must be specified")
	}
	return nil
}

func validateGetCacheConfigRequest(ctx context.Context, req *GetCacheConfigRequest) error {
	if ctx == nil {
		return errors.NewInvalidInput("cache.GetCacheConfig", "context is required")
	}
	if req == nil {
		return errors.NewInvalidInput("cache.GetCacheConfig", "request is required")
	}
	return nil
}

func validateUpdateCacheConfigRequest(ctx context.Context, req *UpdateCacheConfigRequest) error {
	if ctx == nil {
		return errors.NewInvalidInput("cache.UpdateCacheConfig", "context is required")
	}
	if req == nil {
		return errors.NewInvalidInput("cache.UpdateCacheConfig", "request is required")
	}
	// Validate that at least one field is being updated
	if req.L1Size == nil && req.L1TTL == nil && req.CleanupInterval == nil {
		return errors.NewInvalidInput("cache.UpdateCacheConfig", "at least one configuration field must be specified")
	}
	// Validate L1Size if provided
	if req.L1Size != nil && *req.L1Size < 0 {
		return errors.NewInvalidInput("cache.UpdateCacheConfig", "L1 size cannot be negative")
	}
	// Validate L1TTL if provided
	if req.L1TTL != nil && *req.L1TTL < 0 {
		return errors.NewInvalidInput("cache.UpdateCacheConfig", "L1 TTL cannot be negative")
	}
	// Validate CleanupInterval if provided
	if req.CleanupInterval != nil && *req.CleanupInterval < 0 {
		return errors.NewInvalidInput("cache.UpdateCacheConfig", "cleanup interval cannot be negative")
	}
	return nil
}
