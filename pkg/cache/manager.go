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

// Package cache provides an in-memory LRU caching system for QuicBaaS.
// It implements L1 (in-memory LRU) cache with optional L2 (distributed) cache support.
package cache

import (
	"context"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Entry represents a cached value with metadata.
type Entry struct {
	Value     []byte
	ExpiresAt time.Time
	CachedAt  time.Time
}

// IsExpired checks if the entry has expired based on TTL.
func (e *Entry) IsExpired() bool {
	if e.ExpiresAt.IsZero() {
		return false
	}
	return time.Now().After(e.ExpiresAt)
}

// CacheManager implements a multi-tier caching system with L1 (in-memory LRU)
// and optional L2 (distributed) cache support.
type CacheManager struct {
	l1Cache       *LRUCache
	l2Cache       L2Cache
	config        Config
	metrics       *Metrics
	logger        interfaces.Logger
	mu            sync.RWMutex
	stopCh        chan struct{}
	cleanupDone   chan struct{}
	cleanupTicker *time.Ticker
}

// Config defines the cache manager configuration.
type Config struct {
	// L1 cache configuration
	L1Size          int           // Maximum number of entries in L1 cache
	L1TTL           time.Duration // Time-to-live for L1 entries (0 = no expiration)
	CleanupInterval time.Duration // Interval for cleaning expired entries

	// L2 cache configuration
	L2Cache     L2Cache       // Optional L2 cache implementation
	L2TTL       time.Duration // Time-to-live for L2 entries (0 = no expiration)
	WritePolicy WritePolicy   // Write policy for L2 cache
}

// DefaultConfig returns the default cache configuration.
func DefaultConfig() Config {
	return Config{
		L1Size:          1000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: 1 * time.Minute,
		L2TTL:           30 * time.Minute,
		WritePolicy:     WriteThroughSync,
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.L1Size <= 0 {
		return errors.InvalidInput("cache.Config.Validate", "L1 cache size must be positive")
	}
	if c.L1TTL < 0 {
		return errors.InvalidInput("cache.Config.Validate", "L1 TTL cannot be negative")
	}
	if c.L2TTL < 0 {
		return errors.InvalidInput("cache.Config.Validate", "L2 TTL cannot be negative")
	}
	if c.CleanupInterval <= 0 {
		c.CleanupInterval = 1 * time.Minute
	}
	if c.L2Cache != nil && !c.WritePolicy.IsValid() {
		return errors.InvalidInput("cache.Config.Validate", "write policy not implemented")
	}
	return nil
}

// NewManager creates a new cache manager with the given configuration.
func NewManager(config Config, log interfaces.Logger) (*CacheManager, error) {
	if err := config.Validate(); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInvalidInput, "cache.NewManager", "invalid cache config")
	}

	if log == nil {
		return nil, errors.InvalidInput("cache.NewManager", "logger cannot be nil")
	}

	l1Cache := NewLRUCache(config.L1Size)

	cm := &CacheManager{
		l1Cache:     l1Cache,
		l2Cache:     config.L2Cache,
		config:      config,
		metrics:     NewMetrics(),
		logger:      log,
		stopCh:      make(chan struct{}),
		cleanupDone: make(chan struct{}),
	}

	// Setup WriteBack eviction callback if configured
	if config.L2Cache != nil && config.WritePolicy == WriteBack {
		l1Cache.SetEvictCallback(cm.onL1Eviction)
	}

	// Start cleanup goroutine for expired entries. Without an L1 TTL there is
	// no background work, so the manager is born already done.
	if config.L1TTL > 0 {
		go cm.cleanupExpiredEntries()
	} else {
		close(cm.cleanupDone)
	}

	// Verify L2 cache connection if configured
	if cm.l2Cache != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cm.l2Cache.Ping(ctx); err != nil {
			log.WithError(err).Warn("L2 cache ping failed, continuing with L1 only")
		} else {
			log.Info("L2 cache connected successfully")
		}
	}

	return cm, nil
}

// onL1Eviction is called when an entry is evicted from L1 cache.
// This callback is invoked synchronously under lock, so it delegates
// the L2 write to a background goroutine to avoid blocking.
func (cm *CacheManager) onL1Eviction(key string, entry *Entry) {
	if cm.l2Cache == nil {
		return
	}

	// Get L2 TTL while we can safely read config
	cm.mu.RLock()
	l2TTL := cm.config.L2TTL
	cm.mu.RUnlock()

	// Make a copy of the value to avoid race conditions
	valueCopy := make([]byte, len(entry.Value))
	copy(valueCopy, entry.Value)

	// Delegate to background goroutine to avoid blocking eviction
	go cm.writeBackToL2(key, valueCopy, l2TTL)
}

// writeBackToL2 asynchronously writes an evicted entry to L2 cache.
func (cm *CacheManager) writeBackToL2(key string, value []byte, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := cm.l2Cache.Set(ctx, key, value, ttl); err != nil {
		cm.incrementMetric("l2_errors")
		cm.incrementMetric("writeback_failures")
		cm.logger.WithError(err).WithField("key", key).Warn("WriteBack to L2 cache failed")
	} else {
		cm.incrementMetric("l2_sets")
		cm.incrementMetric("writeback_success")
		cm.logger.WithField("key", key).Debug("WriteBack to L2 cache succeeded")
	}
}

// Get retrieves a value from the cache, checking L1 first, then L2.
// If found in L2, the value is populated back into L1 (cache warming).
func (cm *CacheManager) Get(ctx context.Context, key string) ([]byte, error) {
	if key == "" {
		return nil, errors.InvalidInput("cache.Get", "key cannot be empty")
	}

	// Try L1 cache first
	if entry, ok := cm.l1Cache.Get(key); ok {
		if !entry.IsExpired() {
			cm.incrementMetric("l1_hits")
			cm.logger.WithField("key", key).Debug("L1 cache hit")
			return entry.Value, nil
		}
		// Remove expired entry
		cm.l1Cache.Remove(key)
		cm.incrementMetric("l1_expired")
	}

	cm.incrementMetric("l1_misses")

	// Try L2 cache if available
	if cm.l2Cache != nil {
		value, err := cm.l2Cache.Get(ctx, key)
		if err == nil {
			cm.incrementMetric("l2_hits")
			cm.logger.WithField("key", key).Debug("L2 cache hit, warming L1")

			// Warm L1 cache with L2 value
			cm.setL1(key, value)
			return value, nil
		}

		// Only increment miss if it's a NotFound error
		if errors.IsNotFound(err) {
			cm.incrementMetric("l2_misses")
		} else {
			// Log and track other L2 errors
			cm.incrementMetric("l2_errors")
			cm.logger.WithError(err).WithField("key", key).Warn("L2 cache error")
		}
	}

	cm.incrementMetric("cache_misses")
	return nil, errors.NotFound("cache.Get", key)
}

// Set stores a value in the cache using the configured write policy.
// Writes always go to L1. L2 writes depend on the WritePolicy:
//   - WriteThroughSync: Synchronous write to L2
//   - WriteThroughAsync: Asynchronous write to L2 (fire-and-forget)
//   - WriteBack: L2 write happens only on L1 eviction
func (cm *CacheManager) Set(ctx context.Context, key string, value []byte) error {
	if key == "" {
		return errors.InvalidInput("cache.Set", "key cannot be empty")
	}
	if value == nil {
		return errors.InvalidInput("cache.Set", "value cannot be nil")
	}

	// Always write to L1
	cm.setL1(key, value)
	cm.incrementMetric("l1_sets")

	// Write to L2 if configured and policy is write-through
	if cm.l2Cache != nil {
		cm.mu.RLock()
		policy := cm.config.WritePolicy
		l2TTL := cm.config.L2TTL
		cm.mu.RUnlock()

		if policy.IsWriteThrough() {
			if policy.IsAsync() {
				// Async write - fire and forget
				go cm.setL2Async(context.WithoutCancel(ctx), key, value, l2TTL)
			} else {
				// Sync write - block until complete
				if err := cm.setL2(ctx, key, value, l2TTL); err != nil {
					// Log error but don't fail the operation (graceful degradation)
					cm.incrementMetric("l2_errors")
					cm.logger.WithError(err).WithField("key", key).Warn("Failed to write to L2 cache")
				} else {
					cm.incrementMetric("l2_sets")
				}
			}
		}
		// For WriteBack policy, we do nothing here - L2 write happens on eviction
	}

	return nil
}

// Delete removes a value from both L1 and L2 caches.
func (cm *CacheManager) Delete(ctx context.Context, key string) error {
	if key == "" {
		return errors.InvalidInput("cache.Delete", "key cannot be empty")
	}

	// Remove from L1
	cm.l1Cache.Remove(key)
	cm.incrementMetric("l1_deletes")

	// Remove from L2 if configured
	if cm.l2Cache != nil {
		if err := cm.l2Cache.Delete(ctx, key); err != nil {
			cm.incrementMetric("l2_errors")
			cm.logger.WithError(err).WithField("key", key).Warn("Failed to delete from L2 cache")
		} else {
			cm.incrementMetric("l2_deletes")
		}
	}

	return nil
}

// Clear removes all entries from the cache.
func (cm *CacheManager) Clear() {
	cm.l1Cache.Clear()
	cm.incrementMetric("cache_clears")
	cm.logger.Info("Cache cleared")
}

// GetMetrics returns the current cache metrics.
func (cm *CacheManager) GetMetrics() *Metrics {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	return cm.metrics.Copy()
}

// Stats returns cache statistics.
func (cm *CacheManager) Stats() Stats {
	stats := Stats{
		Enabled:     true,
		L1Size:      cm.l1Cache.Len(),
		L1Capacity:  cm.l1Cache.Cap(),
		L1Evictions: cm.l1Cache.EvictionCount(),
		L2Enabled:   cm.l2Cache != nil,
	}

	metrics := cm.GetMetrics()
	stats.L1Hits = metrics.L1Hits()
	stats.L1Misses = metrics.L1Misses()
	stats.L2Hits = metrics.L2Hits()
	stats.L2Misses = metrics.L2Misses()
	stats.L2Errors = metrics.Get("l2_errors")
	stats.L2Sets = metrics.Get("l2_sets")
	stats.WritebackSuccess = metrics.Get("writeback_success")
	stats.WritebackFailure = metrics.Get("writeback_failures")

	// Calculate L1 hit rate
	if stats.L1Hits+stats.L1Misses > 0 {
		stats.L1HitRate = float64(stats.L1Hits) / float64(stats.L1Hits+stats.L1Misses)
	}

	// Calculate L2 hit rate (only if L2 is enabled)
	if stats.L2Enabled && (stats.L2Hits+stats.L2Misses > 0) {
		stats.L2HitRate = float64(stats.L2Hits) / float64(stats.L2Hits+stats.L2Misses)
	}

	return stats
}

// GetConfig returns a copy of the current cache configuration.
func (cm *CacheManager) GetConfig() *Config {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Return a copy to prevent external modification
	return &Config{
		L1Size:          cm.config.L1Size,
		L1TTL:           cm.config.L1TTL,
		CleanupInterval: cm.config.CleanupInterval,
		L2Cache:         cm.config.L2Cache,
		L2TTL:           cm.config.L2TTL,
		WritePolicy:     cm.config.WritePolicy,
	}
}

// UpdateConfig validates and applies a new cache configuration.
// If L1Size changed, the cache is resized accordingly.
// If CleanupInterval changed, the cleanup ticker is restarted.
// If WritePolicy changed to/from WriteBack, the eviction callback is updated.
func (cm *CacheManager) UpdateConfig(config *Config) error {
	if config == nil {
		return errors.InvalidInput("cache.UpdateConfig", "config cannot be nil")
	}

	// Validate new config
	if err := config.Validate(); err != nil {
		return errors.Wrapf(err, errors.ErrInvalidInput, "cache.UpdateConfig", "invalid cache config")
	}

	cm.mu.Lock()
	defer cm.mu.Unlock()

	oldConfig := cm.config

	// Check if L1 size changed
	if config.L1Size != oldConfig.L1Size {
		cm.l1Cache.Resize(config.L1Size)
		cm.logger.WithFields(map[string]interface{}{
			"old_size": oldConfig.L1Size,
			"new_size": config.L1Size,
		}).Info("Cache L1 size updated")
	}

	// Check if WritePolicy changed and update eviction callback
	if config.WritePolicy != oldConfig.WritePolicy {
		if config.L2Cache != nil && config.WritePolicy == WriteBack {
			cm.l1Cache.SetEvictCallback(cm.onL1Eviction)
		} else {
			cm.l1Cache.SetEvictCallback(nil)
		}
		cm.logger.WithFields(map[string]interface{}{
			"old_policy": oldConfig.WritePolicy.String(),
			"new_policy": config.WritePolicy.String(),
		}).Info("Cache write policy updated")
	}

	// Check if cleanup interval changed
	needsCleanupRestart := config.CleanupInterval != oldConfig.CleanupInterval
	needsTTLRestart := config.L1TTL != oldConfig.L1TTL

	// Update config
	cm.config = *config

	// Restart cleanup goroutine if interval or TTL changed
	if needsCleanupRestart || needsTTLRestart {
		if err := cm.restartCleanupTicker(); err != nil {
			cm.logger.WithError(err).Warn("Failed to restart cleanup ticker")
		}
		cm.logger.WithFields(map[string]interface{}{
			"cleanup_interval": config.CleanupInterval,
			"ttl":              config.L1TTL,
		}).Info("Cache cleanup configuration updated")
	}

	return nil
}

// restartCleanupTicker stops and restarts the cleanup ticker with new interval.
// Must be called with lock held.
func (cm *CacheManager) restartCleanupTicker() error {
	// Stop existing ticker if running
	if cm.cleanupTicker != nil {
		cm.cleanupTicker.Stop()
	}

	// Create new ticker with updated interval
	if cm.config.L1TTL > 0 && cm.config.CleanupInterval > 0 {
		cm.cleanupTicker = time.NewTicker(cm.config.CleanupInterval)
	}

	return nil
}

// Done returns a channel that is closed once the manager's background
// cleanup goroutine has exited. It is closed from the start when the manager
// runs none (a zero L1 TTL), and otherwise only by that goroutine's own exit,
// which Close waits for. An open Done therefore means the cleanup goroutine
// is live; a closed one means the manager holds no goroutine.
func (cm *CacheManager) Done() <-chan struct{} {
	return cm.cleanupDone
}

// Close closes the cache manager and releases resources.
func (cm *CacheManager) Close() error {
	close(cm.stopCh)
	<-cm.cleanupDone

	cm.mu.Lock()
	if cm.cleanupTicker != nil {
		cm.cleanupTicker.Stop()
	}
	cm.mu.Unlock()

	// Close L2 cache if configured
	if cm.l2Cache != nil {
		if err := cm.l2Cache.Close(); err != nil {
			cm.logger.WithError(err).Warn("Failed to close L2 cache")
			return err
		}
	}

	cm.logger.Info("Cache manager closed")
	return nil
}

// setL1 stores a value in L1 cache with TTL.
func (cm *CacheManager) setL1(key string, value []byte) {
	entry := &Entry{
		Value:    value,
		CachedAt: time.Now(),
	}

	cm.mu.RLock()
	ttl := cm.config.L1TTL
	cm.mu.RUnlock()

	if ttl > 0 {
		entry.ExpiresAt = time.Now().Add(ttl)
	}

	cm.l1Cache.Add(key, entry)
}

// setL2 synchronously writes to L2 cache.
func (cm *CacheManager) setL2(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	return cm.l2Cache.Set(ctx, key, value, ttl)
}

// setL2Async asynchronously writes to L2 cache (fire-and-forget).
func (cm *CacheManager) setL2Async(parentCtx context.Context, key string, value []byte, ttl time.Duration) {
	ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)
	defer cancel()

	if err := cm.l2Cache.Set(ctx, key, value, ttl); err != nil {
		cm.incrementMetric("l2_errors")
		cm.logger.WithError(err).WithField("key", key).Warn("Async L2 cache write failed")
	} else {
		cm.incrementMetric("l2_sets")
	}
}

// cleanupExpiredEntries periodically removes expired entries from L1 cache.
func (cm *CacheManager) cleanupExpiredEntries() {
	defer close(cm.cleanupDone)

	// Get initial interval
	cm.mu.RLock()
	ticker := time.NewTicker(cm.config.CleanupInterval)
	cm.cleanupTicker = ticker
	cm.mu.RUnlock()

	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cm.performCleanup()

			// Check if we need to update ticker interval
			cm.mu.RLock()
			currentTicker := cm.cleanupTicker
			cm.mu.RUnlock()

			if currentTicker != ticker {
				// Ticker was replaced, stop old one and use new one
				ticker.Stop()
				ticker = currentTicker
			}

		case <-cm.stopCh:
			return
		}
	}
}

// performCleanup removes expired entries from L1 cache.
func (cm *CacheManager) performCleanup() {
	expiredCount := 0
	keys := cm.l1Cache.Keys()

	for _, key := range keys {
		if entry, ok := cm.l1Cache.Get(key); ok {
			if entry.IsExpired() {
				cm.l1Cache.Remove(key)
				expiredCount++
			}
		}
	}

	if expiredCount > 0 {
		cm.logger.WithField("count", expiredCount).Debug("Cleaned up expired cache entries")
		cm.metrics.Add("l1_expired", int64(expiredCount))
	}
}

// incrementMetric safely increments a metric counter.
func (cm *CacheManager) incrementMetric(name string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.metrics.Increment(name)
}

// Stats represents cache statistics.
type Stats struct {
	Enabled          bool
	L1Size           int
	L1Capacity       int
	L1Hits           int64
	L1Misses         int64
	L1HitRate        float64
	L1Evictions      int64
	L2Enabled        bool
	L2Hits           int64
	L2Misses         int64
	L2HitRate        float64
	L2Errors         int64
	L2Sets           int64
	WritebackSuccess int64
	WritebackFailure int64
}
