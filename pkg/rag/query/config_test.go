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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 10000, cfg.CacheSize)
	assert.Equal(t, 5*time.Minute, cfg.CacheTTL)
	assert.Equal(t, 10, cfg.DefaultTopK)
	assert.Equal(t, float32(0.7), cfg.DefaultMinScore)
	assert.Equal(t, 20, cfg.DefaultNProbe)
	assert.Equal(t, true, cfg.EnableCache)
	assert.Equal(t, 30*time.Second, cfg.QueryTimeout)
	assert.Equal(t, 1*time.Minute, cfg.CleanupInterval)
}

func TestConfig_Validate_Success(t *testing.T) {
	cfg := DefaultConfig()
	err := cfg.Validate()
	assert.NoError(t, err)
}

func TestConfig_Validate_InvalidCacheSize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CacheSize = -1

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cache size")
}

func TestConfig_Validate_InvalidCacheTTL(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CacheTTL = 0

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cache TTL")
}

func TestConfig_Validate_InvalidTopK(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DefaultTopK = 0

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "TopK")
}

func TestConfig_Validate_InvalidMinScore_Negative(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DefaultMinScore = -0.1

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "min score")
}

func TestConfig_Validate_InvalidMinScore_TooHigh(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DefaultMinScore = 1.1

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "min score")
}

func TestConfig_Validate_InvalidNProbe(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DefaultNProbe = 0

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "NProbe")
}

func TestConfig_Validate_InvalidQueryTimeout(t *testing.T) {
	cfg := DefaultConfig()
	cfg.QueryTimeout = 0

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "query timeout")
}

func TestConfig_Validate_InvalidCleanupInterval(t *testing.T) {
	cfg := DefaultConfig()
	cfg.CleanupInterval = 0

	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cleanup interval")
}

func TestConfig_WithCacheSize(t *testing.T) {
	cfg := DefaultConfig().WithCacheSize(5000)

	assert.Equal(t, 5000, cfg.CacheSize)
	assert.NoError(t, cfg.Validate())
}

func TestConfig_WithCacheTTL(t *testing.T) {
	cfg := DefaultConfig().WithCacheTTL(10 * time.Minute)

	assert.Equal(t, 10*time.Minute, cfg.CacheTTL)
	assert.NoError(t, cfg.Validate())
}

func TestConfig_WithDefaults(t *testing.T) {
	cfg := DefaultConfig().WithDefaults(20, 0.8, 30)

	assert.Equal(t, 20, cfg.DefaultTopK)
	assert.Equal(t, float32(0.8), cfg.DefaultMinScore)
	assert.Equal(t, 30, cfg.DefaultNProbe)
	assert.NoError(t, cfg.Validate())
}

func TestConfig_WithoutCache(t *testing.T) {
	cfg := DefaultConfig().WithoutCache()

	assert.False(t, cfg.EnableCache)
	assert.NoError(t, cfg.Validate())
}

func TestConfig_Chaining(t *testing.T) {
	cfg := DefaultConfig().
		WithCacheSize(5000).
		WithCacheTTL(10*time.Minute).
		WithDefaults(20, 0.8, 30)

	assert.Equal(t, 5000, cfg.CacheSize)
	assert.Equal(t, 10*time.Minute, cfg.CacheTTL)
	assert.Equal(t, 20, cfg.DefaultTopK)
	assert.Equal(t, float32(0.8), cfg.DefaultMinScore)
	assert.Equal(t, 30, cfg.DefaultNProbe)
	assert.NoError(t, cfg.Validate())
}

func TestConfig_CustomConfiguration(t *testing.T) {
	cfg := Config{
		CacheSize:       1000,
		CacheTTL:        2 * time.Minute,
		DefaultTopK:     5,
		DefaultMinScore: 0.6,
		DefaultNProbe:   10,
		EnableCache:     true,
		QueryTimeout:    15 * time.Second,
		CleanupInterval: 30 * time.Second,
	}

	err := cfg.Validate()
	require.NoError(t, err)

	assert.Equal(t, 1000, cfg.CacheSize)
	assert.Equal(t, 2*time.Minute, cfg.CacheTTL)
	assert.Equal(t, 5, cfg.DefaultTopK)
	assert.Equal(t, float32(0.6), cfg.DefaultMinScore)
	assert.Equal(t, 10, cfg.DefaultNProbe)
	assert.True(t, cfg.EnableCache)
	assert.Equal(t, 15*time.Second, cfg.QueryTimeout)
	assert.Equal(t, 30*time.Second, cfg.CleanupInterval)
}

func TestConfig_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{
			name: "min score at 0",
			cfg: Config{
				CacheSize:       100,
				CacheTTL:        1 * time.Minute,
				DefaultTopK:     10,
				DefaultMinScore: 0.0,
				DefaultNProbe:   10,
				EnableCache:     true,
				QueryTimeout:    30 * time.Second,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "min score at 1",
			cfg: Config{
				CacheSize:       100,
				CacheTTL:        1 * time.Minute,
				DefaultTopK:     10,
				DefaultMinScore: 1.0,
				DefaultNProbe:   10,
				EnableCache:     true,
				QueryTimeout:    30 * time.Second,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "cache size 0",
			cfg: Config{
				CacheSize:       0,
				CacheTTL:        1 * time.Minute,
				DefaultTopK:     10,
				DefaultMinScore: 0.7,
				DefaultNProbe:   10,
				EnableCache:     false,
				QueryTimeout:    30 * time.Second,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "very large cache size",
			cfg: Config{
				CacheSize:       1000000,
				CacheTTL:        1 * time.Minute,
				DefaultTopK:     10,
				DefaultMinScore: 0.7,
				DefaultNProbe:   10,
				EnableCache:     true,
				QueryTimeout:    30 * time.Second,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: false,
		},
		{
			name: "very small ttl",
			cfg: Config{
				CacheSize:       100,
				CacheTTL:        1 * time.Millisecond,
				DefaultTopK:     10,
				DefaultMinScore: 0.7,
				DefaultNProbe:   10,
				EnableCache:     true,
				QueryTimeout:    30 * time.Second,
				CleanupInterval: 1 * time.Minute,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
