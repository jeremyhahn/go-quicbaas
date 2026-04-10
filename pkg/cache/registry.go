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

// Package cache provides entity-aware caching with per-entity configuration.
package cache

import (
	"context"
	"fmt"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// EntityCache represents a cache instance for a specific entity type.
type EntityCache struct {
	entityType string
	manager    *CacheManager
	config     config.EntityCacheConfig
	enabled    bool
}

// CacheRegistry manages entity-specific cache instances based on configuration.
type CacheRegistry struct {
	config      *config.EnhancedCacheConfig
	caches      map[string]*EntityCache
	logger      interfaces.Logger
	mu          sync.RWMutex
	globalCache *CacheManager // Fallback cache for unconfigured entities
	l2Cache     L2Cache       // Shared L2 cache for all entity caches
}

// NewCacheRegistry creates a new cache registry from the given configuration.
// The l2Cache parameter is optional and can be nil if L2 caching is not desired.
func NewCacheRegistry(cfg *config.EnhancedCacheConfig, logger interfaces.Logger, l2Cache L2Cache) (*CacheRegistry, error) {
	if cfg == nil {
		return nil, errors.InvalidInput("cache.NewCacheRegistry", "config cannot be nil")
	}

	if logger == nil {
		return nil, errors.InvalidInput("cache.NewCacheRegistry", "logger cannot be nil")
	}

	if err := cfg.Validate(); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInvalidInput, "cache.NewCacheRegistry", "invalid cache config")
	}

	registry := &CacheRegistry{
		config:  cfg,
		caches:  make(map[string]*EntityCache),
		logger:  logger,
		l2Cache: l2Cache,
	}

	// Create a global fallback cache for entities without specific configuration
	if cfg.Enabled {
		globalCacheConfig := Config{
			L1Size:          cfg.MaxSize,
			L1TTL:           cfg.DefaultTTL,
			CleanupInterval: cfg.CleanupInterval,
			L2Cache:         l2Cache,
			L2TTL:           cfg.L2TTL,
			WritePolicy:     WriteThroughSync,
		}

		globalCache, err := NewManager(globalCacheConfig, logger)
		if err != nil {
			return nil, errors.Wrapf(err, errors.ErrInternal, "cache.NewCacheRegistry", "failed to create global cache")
		}
		registry.globalCache = globalCache
	}

	// Initialize entity-specific caches
	for entityType, entityCfg := range cfg.Entities {
		if err := registry.registerEntityCache(entityType, entityCfg); err != nil {
			// Log error but continue with other entities
			logger.WithError(err).
				WithField("entity_type", entityType).
				Error("Failed to register entity cache")
		}
	}

	logger.WithField("entity_count", len(registry.caches)).
		Info("Cache registry initialized")

	return registry, nil
}

// registerEntityCache creates and registers a cache for a specific entity type.
// This method does NOT acquire locks and should only be called from NewCacheRegistry.
func (r *CacheRegistry) registerEntityCache(entityType string, cfg config.EntityCacheConfig) error {
	// Merge with defaults
	merged := r.config.GetEntityConfig(entityType)

	// Determine write policy
	writePolicy := WriteThroughSync
	if merged.WriteThrough != nil && !*merged.WriteThrough {
		// If write-through is explicitly disabled, we don't use L2
		// (future: could use WriteBack policy here)
		writePolicy = WriteThroughSync
	}

	// Create cache configuration
	cacheConfig := Config{
		L1Size:          merged.MaxSize,
		L1TTL:           merged.TTL,
		CleanupInterval: r.config.CleanupInterval,
		L2Cache:         r.l2Cache,
		L2TTL:           r.config.L2TTL,
		WritePolicy:     writePolicy,
	}

	// If MaxSize is 0, use proportional allocation
	if cacheConfig.L1Size == 0 {
		cacheConfig.L1Size = r.config.GetEntityMaxSize(entityType)
	}

	// If write-through is disabled, don't use L2 for this entity
	if merged.WriteThrough != nil && !*merged.WriteThrough {
		cacheConfig.L2Cache = nil
	}

	// Create cache manager for this entity
	var manager *CacheManager
	var err error

	if merged.Enabled {
		manager, err = NewManager(cacheConfig, r.logger)
		if err != nil {
			return errors.Wrapf(err, errors.ErrInternal, "cache.registerEntityCache", "failed to create cache manager for %s", entityType)
		}
	}

	entityCache := &EntityCache{
		entityType: entityType,
		manager:    manager,
		config:     merged,
		enabled:    merged.Enabled,
	}

	r.caches[entityType] = entityCache

	r.logger.WithFields(map[string]interface{}{
		"entity_type": entityType,
		"enabled":     merged.Enabled,
		"ttl":         merged.TTL,
		"max_size":    cacheConfig.L1Size,
		"l2_enabled":  cacheConfig.L2Cache != nil,
	}).Debug("Registered entity cache")

	return nil
}

// GetEntityCache returns the cache instance for a specific entity type.
// If the entity doesn't have a specific cache, returns the global cache.
func (r *CacheRegistry) GetEntityCache(entityType string) *EntityCache {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if cache, exists := r.caches[entityType]; exists {
		return cache
	}

	// Return a pseudo entity cache that uses the global cache
	return &EntityCache{
		entityType: entityType,
		manager:    r.globalCache,
		config:     r.config.GetEntityConfig(entityType),
		enabled:    r.config.Enabled,
	}
}

// IsCacheEnabled returns whether caching is enabled for a specific entity type.
func (r *CacheRegistry) IsCacheEnabled(entityType string) bool {
	return r.config.IsEntityCacheEnabled(entityType)
}

// Get retrieves a value from the cache for a specific entity type.
func (r *CacheRegistry) Get(ctx context.Context, entityType, key string) ([]byte, error) {
	entityCache := r.GetEntityCache(entityType)
	return entityCache.Get(ctx, key)
}

// Set stores a value in the cache for a specific entity type.
func (r *CacheRegistry) Set(ctx context.Context, entityType, key string, value []byte) error {
	entityCache := r.GetEntityCache(entityType)
	return entityCache.Set(ctx, key, value)
}

// Delete removes a value from the cache for a specific entity type.
func (r *CacheRegistry) Delete(ctx context.Context, entityType, key string) error {
	entityCache := r.GetEntityCache(entityType)
	return entityCache.Delete(ctx, key)
}

// Clear clears all caches in the registry.
func (r *CacheRegistry) Clear() {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, cache := range r.caches {
		if cache.enabled && cache.manager != nil {
			cache.manager.Clear()
		}
	}

	if r.globalCache != nil {
		r.globalCache.Clear()
	}

	r.logger.Info("All caches cleared")
}

// GetStats returns statistics for a specific entity type.
func (r *CacheRegistry) GetStats(entityType string) Stats {
	entityCache := r.GetEntityCache(entityType)
	return entityCache.Stats()
}

// GetAllStats returns statistics for all entity types.
func (r *CacheRegistry) GetAllStats() map[string]Stats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := make(map[string]Stats)

	// Include all configured entities, both enabled and disabled
	for entityType, cache := range r.caches {
		stats[entityType] = cache.Stats()
	}

	// Add global cache stats
	if r.globalCache != nil {
		stats["_global"] = r.globalCache.Stats()
	}

	return stats
}

// Close closes all caches in the registry.
func (r *CacheRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var lastErr error

	for entityType, cache := range r.caches {
		if cache.enabled && cache.manager != nil {
			if err := cache.manager.Close(); err != nil {
				r.logger.WithError(err).
					WithField("entity_type", entityType).
					Error("Failed to close entity cache")
				lastErr = err
			}
		}
	}

	if r.globalCache != nil {
		if err := r.globalCache.Close(); err != nil {
			r.logger.WithError(err).Error("Failed to close global cache")
			lastErr = err
		}
	}

	// Close shared L2 cache (if any)
	// Note: We only close L2 if it was created by the registry
	// If it was passed in, the caller is responsible for closing it

	r.logger.Info("Cache registry closed")
	return lastErr
}

// UpdateEntityConfig dynamically updates the configuration for an entity type.
// This will recreate the cache with the new configuration.
func (r *CacheRegistry) UpdateEntityConfig(entityType string, cfg config.EntityCacheConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Close existing cache if it exists
	if existingCache, exists := r.caches[entityType]; exists {
		if existingCache.enabled && existingCache.manager != nil {
			if err := existingCache.manager.Close(); err != nil {
				r.logger.WithError(err).
					WithField("entity_type", entityType).
					Warn("Failed to close existing cache during update")
			}
		}
		delete(r.caches, entityType)
	}

	// Update config
	r.config.Entities[entityType] = cfg

	// Register new cache
	return r.registerEntityCache(entityType, cfg)
}

// EntityCache methods

// Get retrieves a value from the entity cache.
func (ec *EntityCache) Get(ctx context.Context, key string) ([]byte, error) {
	if !ec.enabled || ec.manager == nil {
		return nil, errors.NotFound("cache.EntityCache.Get", "cache disabled for entity: "+ec.entityType)
	}

	return ec.manager.Get(ctx, key)
}

// Set stores a value in the entity cache.
func (ec *EntityCache) Set(ctx context.Context, key string, value []byte) error {
	if !ec.enabled || ec.manager == nil {
		return nil // Silently skip if cache is disabled
	}

	return ec.manager.Set(ctx, key, value)
}

// Delete removes a value from the entity cache.
func (ec *EntityCache) Delete(ctx context.Context, key string) error {
	if !ec.enabled || ec.manager == nil {
		return nil // Silently skip if cache is disabled
	}

	return ec.manager.Delete(ctx, key)
}

// Stats returns statistics for this entity cache.
func (ec *EntityCache) Stats() Stats {
	if !ec.enabled || ec.manager == nil {
		return Stats{Enabled: false}
	}

	return ec.manager.Stats()
}

// IsEnabled returns whether this entity cache is enabled.
func (ec *EntityCache) IsEnabled() bool {
	return ec.enabled
}

// GetConfig returns the configuration for this entity cache.
func (ec *EntityCache) GetConfig() config.EntityCacheConfig {
	return ec.config
}

// GetEntityType returns the entity type for this cache.
func (ec *EntityCache) GetEntityType() string {
	return ec.entityType
}

// Helper functions for common cache operations with entity types

// BuildEntityKey creates a cache key with entity type prefix.
// Format: "entity:{entityType}:{id}".
func BuildEntityKey(entityType, id string) string {
	return fmt.Sprintf("entity:%s:%s", entityType, id)
}

// BuildEntityPrefixKey creates a cache key for scanning/listing entity types.
// Format: "entity:{entityType}:".
func BuildEntityPrefixKey(entityType string) string {
	return fmt.Sprintf("entity:%s:", entityType)
}

// ParseEntityKey extracts the entity type and ID from a cache key.
// Returns empty strings if the key format is invalid.
func ParseEntityKey(key string) (entityType, id string) {
	// Simple parsing - could be enhanced with more robust parsing
	const prefix = "entity:"
	if len(key) <= len(prefix) {
		return "", ""
	}

	if key[:len(prefix)] != prefix {
		return "", ""
	}

	remainder := key[len(prefix):]

	// Find the second colon
	for i, c := range remainder {
		if c == ':' {
			return remainder[:i], remainder[i+1:]
		}
	}

	return "", ""
}
