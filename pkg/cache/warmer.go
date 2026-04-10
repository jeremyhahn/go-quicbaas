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
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// StorageReader defines the interface for reading from storage during cache warming.
type StorageReader interface {
	// NewIterator creates a new iterator with the given options.
	NewIterator(opts *storage.IteratorOptions) storage.Iterator

	// Get retrieves the value for the given key.
	Get(key []byte) ([]byte, error)
}

// WarmingConfig configures the cache warming behavior.
type WarmingConfig struct {
	Enabled            bool     // Enable cache warming on startup
	Prefixes           []string // List of key prefixes to warm
	BatchSize          int      // Number of keys to warm per batch
	RateLimitPerSecond int      // Maximum keys per second (0 = unlimited)
	MaxKeys            int      // Maximum total keys to warm (0 = unlimited)
}

// WarmingStatus represents the current state of cache warming.
type WarmingStatus struct {
	State         string // "idle", "warming", "completed", "failed"
	StartedAt     time.Time
	CompletedAt   time.Time
	KeysWarmed    int64
	KeysFailed    int64
	CurrentPrefix string
	Error         string
}

// Copy creates a deep copy of the warming status.
func (s *WarmingStatus) Copy() *WarmingStatus {
	return &WarmingStatus{
		State:         s.State,
		StartedAt:     s.StartedAt,
		CompletedAt:   s.CompletedAt,
		KeysWarmed:    s.KeysWarmed,
		KeysFailed:    s.KeysFailed,
		CurrentPrefix: s.CurrentPrefix,
		Error:         s.Error,
	}
}

// CacheWarmer pre-populates the cache from storage.
type CacheWarmer struct {
	cache    *CacheManager
	storage  StorageReader
	config   WarmingConfig
	logger   interfaces.Logger
	status   *WarmingStatus
	statusMu sync.RWMutex
	stopCh   chan struct{}
	stopped  atomic.Bool
}

// NewCacheWarmer creates a new cache warmer instance.
func NewCacheWarmer(
	cache *CacheManager,
	storage StorageReader,
	config WarmingConfig,
	logger interfaces.Logger,
) (*CacheWarmer, error) {
	if cache == nil {
		return nil, errors.InvalidInput("cache.NewCacheWarmer", "cache cannot be nil")
	}
	if storage == nil {
		return nil, errors.InvalidInput("cache.NewCacheWarmer", "storage cannot be nil")
	}
	if logger == nil {
		return nil, errors.InvalidInput("cache.NewCacheWarmer", "logger cannot be nil")
	}

	// Apply defaults and validate config
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.RateLimitPerSecond < 0 {
		return nil, errors.InvalidInput("cache.NewCacheWarmer", "RateLimitPerSecond cannot be negative")
	}
	if config.MaxKeys < 0 {
		return nil, errors.InvalidInput("cache.NewCacheWarmer", "MaxKeys cannot be negative")
	}

	warmer := &CacheWarmer{
		cache:   cache,
		storage: storage,
		config:  config,
		logger:  logger,
		status: &WarmingStatus{
			State: "idle",
		},
		stopCh: make(chan struct{}),
	}

	return warmer, nil
}

// WarmPrefix warms the cache for a single prefix pattern.
func (w *CacheWarmer) WarmPrefix(ctx context.Context, prefix string) (*WarmingStatus, error) {
	if prefix == "" {
		return nil, errors.InvalidInput("cache.WarmPrefix", "prefix cannot be empty")
	}

	// Check if already stopped
	if w.stopped.Load() {
		return nil, errors.Internal("cache.WarmPrefix", "cache warmer is stopped")
	}

	// Initialize status
	w.updateStatus(func(s *WarmingStatus) {
		s.State = "warming"
		s.StartedAt = time.Now()
		s.CompletedAt = time.Time{}
		s.KeysWarmed = 0
		s.KeysFailed = 0
		s.CurrentPrefix = prefix
		s.Error = ""
	})

	// Perform warming
	err := w.warmPrefixInternal(ctx, prefix)

	// Update final status
	w.updateStatus(func(s *WarmingStatus) {
		s.CompletedAt = time.Now()
		if err != nil {
			s.State = "failed"
			s.Error = err.Error()
		} else {
			s.State = "completed"
		}
	})

	return w.GetStatus(), err
}

// WarmPrefixes warms the cache for multiple prefixes sequentially.
func (w *CacheWarmer) WarmPrefixes(ctx context.Context, prefixes []string) (*WarmingStatus, error) {
	if len(prefixes) == 0 {
		return nil, errors.InvalidInput("cache.WarmPrefixes", "prefixes cannot be empty")
	}

	// Check if already stopped
	if w.stopped.Load() {
		return nil, errors.Internal("cache.WarmPrefixes", "cache warmer is stopped")
	}

	// Initialize status
	w.updateStatus(func(s *WarmingStatus) {
		s.State = "warming"
		s.StartedAt = time.Now()
		s.CompletedAt = time.Time{}
		s.KeysWarmed = 0
		s.KeysFailed = 0
		s.Error = ""
	})

	var lastErr error

	// Warm each prefix sequentially
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}

		// Update current prefix
		w.updateStatus(func(s *WarmingStatus) {
			s.CurrentPrefix = prefix
		})

		// Warm this prefix
		if err := w.warmPrefixInternal(ctx, prefix); err != nil {
			w.logger.WithError(err).
				WithField("prefix", prefix).
				Warn("Failed to warm prefix")
			lastErr = err
		}

		// Check if we should stop
		select {
		case <-ctx.Done():
			lastErr = ctx.Err()
			goto done
		case <-w.stopCh:
			lastErr = errors.Internal("cache.WarmPrefixes", "cache warmer stopped")
			goto done
		default:
		}
	}

done:
	// Update final status
	w.updateStatus(func(s *WarmingStatus) {
		s.CompletedAt = time.Now()
		s.CurrentPrefix = ""
		if lastErr != nil {
			s.State = "failed"
			s.Error = lastErr.Error()
		} else {
			s.State = "completed"
		}
	})

	return w.GetStatus(), lastErr
}

// GetStatus returns a copy of the current warming status.
func (w *CacheWarmer) GetStatus() *WarmingStatus {
	w.statusMu.RLock()
	defer w.statusMu.RUnlock()
	return w.status.Copy()
}

// Stop gracefully stops the warming operation.
func (w *CacheWarmer) Stop() {
	if w.stopped.CompareAndSwap(false, true) {
		close(w.stopCh)

		w.updateStatus(func(s *WarmingStatus) {
			if s.State == "warming" {
				s.State = "failed"
				s.CompletedAt = time.Now()
				s.Error = "cache warmer stopped"
			}
		})

		w.logger.Info("Cache warmer stopped")
	}
}

// warmPrefixInternal performs the actual warming for a single prefix.
func (w *CacheWarmer) warmPrefixInternal(ctx context.Context, prefix string) error {
	// Create iterator options for this prefix
	opts := &storage.IteratorOptions{
		LowerBound: []byte(prefix),
		UpperBound: incrementByteSlice([]byte(prefix)),
	}

	iter := w.storage.NewIterator(opts)
	defer iter.Close()

	// Setup rate limiting if configured
	var rateLimiter *time.Ticker
	var rateLimitCh <-chan time.Time

	if w.config.RateLimitPerSecond > 0 {
		// Calculate delay per batch
		delay := time.Second * time.Duration(w.config.BatchSize) / time.Duration(w.config.RateLimitPerSecond)
		rateLimiter = time.NewTicker(delay)
		rateLimitCh = rateLimiter.C
		defer rateLimiter.Stop()
	}

	batchCount := 0
	totalProcessed := int64(0)

	// Iterate through all keys with the prefix
	for iter.SeekToFirst(); iter.Valid(); iter.Next() {
		// Check for context cancellation or stop signal
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-w.stopCh:
			return errors.Internal("cache.warmPrefixInternal", "cache warmer stopped")
		default:
		}

		// Check max keys limit
		if w.config.MaxKeys > 0 {
			currentTotal := w.getKeysWarmed() + w.getKeysFailed()
			if currentTotal >= int64(w.config.MaxKeys) {
				w.logger.WithField("max_keys", w.config.MaxKeys).
					Debug("Reached max keys limit")
				return nil
			}
		}

		// Get key and value
		key := iter.Key()
		if key == nil {
			continue
		}

		// Make a copy of the key since iterator reuses the buffer
		keyCopy := make([]byte, len(key))
		copy(keyCopy, key)

		// Get value from storage
		value, err := w.storage.Get(keyCopy)
		if err != nil {
			w.incrementKeysFailed()
			w.logger.WithError(err).
				WithField("key", string(keyCopy)).
				Debug("Failed to get value from storage")
			continue
		}

		// Set in cache
		if err := w.cache.Set(ctx, string(keyCopy), value); err != nil {
			w.incrementKeysFailed()
			w.logger.WithError(err).
				WithField("key", string(keyCopy)).
				Debug("Failed to set value in cache")
			continue
		}

		w.incrementKeysWarmed()
		batchCount++
		totalProcessed++

		// Apply rate limiting at batch boundaries
		if rateLimiter != nil && batchCount >= w.config.BatchSize {
			select {
			case <-rateLimitCh:
				batchCount = 0
			case <-ctx.Done():
				return ctx.Err()
			case <-w.stopCh:
				return errors.Internal("cache.warmPrefixInternal", "cache warmer stopped")
			}
		}
	}

	// Check for iterator errors
	if err := iter.Error(); err != nil {
		return err
	}

	w.logger.WithFields(map[string]interface{}{
		"prefix":         prefix,
		"keys_warmed":    totalProcessed,
		"keys_failed":    w.getKeysFailed(),
		"total_warmed":   w.getKeysWarmed(),
		"total_failures": w.getKeysFailed(),
	}).Info("Cache warming completed for prefix")

	return nil
}

// updateStatus safely updates the warming status.
func (w *CacheWarmer) updateStatus(fn func(*WarmingStatus)) {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	fn(w.status)
}

// incrementKeysWarmed atomically increments the keys warmed counter.
func (w *CacheWarmer) incrementKeysWarmed() {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	w.status.KeysWarmed++
}

// incrementKeysFailed atomically increments the keys failed counter.
func (w *CacheWarmer) incrementKeysFailed() {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	w.status.KeysFailed++
}

// getKeysWarmed safely reads the keys warmed counter.
func (w *CacheWarmer) getKeysWarmed() int64 {
	w.statusMu.RLock()
	defer w.statusMu.RUnlock()
	return w.status.KeysWarmed
}

// getKeysFailed safely reads the keys failed counter.
func (w *CacheWarmer) getKeysFailed() int64 {
	w.statusMu.RLock()
	defer w.statusMu.RUnlock()
	return w.status.KeysFailed
}

// incrementByteSlice increments a byte slice by one for upper bound calculation.
// Returns nil if the slice cannot be incremented (all 0xFF bytes).
func incrementByteSlice(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}

	// Make a copy to avoid modifying the input
	result := make([]byte, len(b))
	copy(result, b)

	// Increment from right to left
	for i := len(result) - 1; i >= 0; i-- {
		if result[i] < 0xFF {
			result[i]++
			return result
		}
		result[i] = 0
	}

	// All bytes were 0xFF, append a 0x00
	return append(result, 0x00)
}
