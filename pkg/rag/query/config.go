//go:build rag

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

package query

import (
	"time"
)

// Config configures the query handler.
type Config struct {
	// CacheSize is the maximum number of cached queries (default: 10000).
	CacheSize int

	// CacheTTL is the cache entry time-to-live (default: 5 minutes).
	CacheTTL time.Duration

	// DefaultTopK is the default number of results (default: 10).
	DefaultTopK int

	// DefaultMinScore is the default minimum score (default: 0.7).
	DefaultMinScore float32

	// DefaultNProbe is the default IVF nprobe (default: 20).
	DefaultNProbe int

	// EnableCache enables query caching (default: true).
	EnableCache bool

	// QueryTimeout is the maximum query duration (default: 30s).
	QueryTimeout time.Duration

	// CleanupInterval is the cache cleanup interval (default: 1 minute).
	CleanupInterval time.Duration
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		CacheSize:       10000,
		CacheTTL:        5 * time.Minute,
		DefaultTopK:     10,
		DefaultMinScore: 0.7,
		DefaultNProbe:   20,
		EnableCache:     true,
		QueryTimeout:    30 * time.Second,
		CleanupInterval: 1 * time.Minute,
	}
}

// Validate validates the configuration.
func (c Config) Validate() error {
	if c.CacheSize < 0 {
		return ErrInvalidCacheSize
	}

	if c.CacheTTL <= 0 {
		return ErrInvalidCacheTTL
	}

	if c.DefaultTopK <= 0 {
		return ErrInvalidDefaultTopK
	}

	if c.DefaultMinScore < 0 || c.DefaultMinScore > 1 {
		return ErrInvalidDefaultMinScore
	}

	if c.DefaultNProbe <= 0 {
		return ErrInvalidDefaultNProbe
	}

	if c.QueryTimeout <= 0 {
		return ErrInvalidQueryTimeout
	}

	if c.CleanupInterval <= 0 {
		return ErrInvalidCleanupInterval
	}

	return nil
}

// WithCacheSize sets the cache size.
func (c Config) WithCacheSize(size int) Config {
	c.CacheSize = size
	return c
}

// WithCacheTTL sets the cache TTL.
func (c Config) WithCacheTTL(ttl time.Duration) Config {
	c.CacheTTL = ttl
	return c
}

// WithDefaults sets default query options.
func (c Config) WithDefaults(topK int, minScore float32, nprobe int) Config {
	c.DefaultTopK = topK
	c.DefaultMinScore = minScore
	c.DefaultNProbe = nprobe
	return c
}

// WithoutCache disables caching.
func (c Config) WithoutCache() Config {
	c.EnableCache = false
	return c
}
