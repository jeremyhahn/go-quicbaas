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

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// BatchOpType Tests
// =============================================================================

func TestBatchOpType_String(t *testing.T) {
	tests := []struct {
		name     string
		opType   BatchOpType
		expected string
	}{
		{
			name:     "BatchOpSet",
			opType:   BatchOpSet,
			expected: "Set",
		},
		{
			name:     "BatchOpDelete",
			opType:   BatchOpDelete,
			expected: "Delete",
		},
		{
			name:     "Unknown operation type",
			opType:   BatchOpType(99),
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.opType.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Default Options Tests
// =============================================================================

func TestDefaultWriteOptions(t *testing.T) {
	t.Run("returns valid write options with defaults", func(t *testing.T) {
		opts := DefaultWriteOptions()

		assert.NotNil(t, opts)
		assert.Equal(t, qrdbtransport.WriteModeSync, opts.Mode)
		assert.Equal(t, 5*time.Second, opts.Timeout)
		assert.True(t, opts.RetryOnFailure)
		assert.Equal(t, 3, opts.MaxRetries)
	})

	t.Run("returns new instance each time", func(t *testing.T) {
		opts1 := DefaultWriteOptions()
		opts2 := DefaultWriteOptions()

		assert.NotSame(t, opts1, opts2)

		// Modify one shouldn't affect the other
		opts1.MaxRetries = 10
		assert.Equal(t, 3, opts2.MaxRetries)
	})
}

func TestDefaultReadOptions(t *testing.T) {
	t.Run("returns valid read options with defaults", func(t *testing.T) {
		opts := DefaultReadOptions()

		assert.NotNil(t, opts)
		assert.Equal(t, qrdbtransport.ConsistencyLinearizable, opts.Consistency)
		assert.Equal(t, 5*time.Second, opts.Timeout)
		assert.False(t, opts.AllowStale)
		assert.True(t, opts.UseCache)
	})

	t.Run("returns new instance each time", func(t *testing.T) {
		opts1 := DefaultReadOptions()
		opts2 := DefaultReadOptions()

		assert.NotSame(t, opts1, opts2)

		// Modify one shouldn't affect the other
		opts1.AllowStale = true
		assert.False(t, opts2.AllowStale)
	})
}

func TestDefaultCacheConfig(t *testing.T) {
	t.Run("returns valid cache config with defaults", func(t *testing.T) {
		config := DefaultCacheConfig()

		assert.NotNil(t, config)
		assert.Equal(t, 1000, config.L1Size)
		assert.Equal(t, 5*time.Minute, config.L1TTL)
		assert.Equal(t, 1*time.Minute, config.CleanupInterval)
	})

	t.Run("returns new instance each time", func(t *testing.T) {
		config1 := DefaultCacheConfig()
		config2 := DefaultCacheConfig()

		assert.NotSame(t, config1, config2)

		// Modify one shouldn't affect the other
		config1.L1Size = 5000
		assert.Equal(t, 1000, config2.L1Size)
	})
}

// =============================================================================
// Options Validation Tests
// =============================================================================

func TestWriteOptions_Validation(t *testing.T) {
	t.Run("default options are valid", func(t *testing.T) {
		opts := DefaultWriteOptions()

		// Should have sensible defaults
		assert.True(t, opts.Timeout > 0)
		assert.True(t, opts.MaxRetries >= 0)
	})

	t.Run("custom options work correctly", func(t *testing.T) {
		opts := &WriteOptions{
			Mode:           qrdbtransport.WriteModeSync,
			Timeout:        10 * time.Second,
			RetryOnFailure: false,
			MaxRetries:     5,
		}

		assert.Equal(t, qrdbtransport.WriteModeSync, opts.Mode)
		assert.Equal(t, 10*time.Second, opts.Timeout)
		assert.False(t, opts.RetryOnFailure)
		assert.Equal(t, 5, opts.MaxRetries)
	})
}

func TestReadOptions_Validation(t *testing.T) {
	t.Run("default options are valid", func(t *testing.T) {
		opts := DefaultReadOptions()

		// Should have sensible defaults
		assert.True(t, opts.Timeout > 0)
	})

	t.Run("custom options work correctly", func(t *testing.T) {
		opts := &ReadOptions{
			Consistency: qrdbtransport.ConsistencyEventual,
			Timeout:     10 * time.Second,
			AllowStale:  true,
			UseCache:    false,
		}

		assert.Equal(t, qrdbtransport.ConsistencyEventual, opts.Consistency)
		assert.Equal(t, 10*time.Second, opts.Timeout)
		assert.True(t, opts.AllowStale)
		assert.False(t, opts.UseCache)
	})
}

func TestCacheConfig_Validation(t *testing.T) {
	t.Run("default config is valid", func(t *testing.T) {
		config := DefaultCacheConfig()

		// Should have sensible defaults
		assert.True(t, config.L1Size > 0)
		assert.True(t, config.L1TTL > 0)
		assert.True(t, config.CleanupInterval > 0)
	})

	t.Run("custom config works correctly", func(t *testing.T) {
		config := &CacheConfig{
			L1Size:          10000,
			L1TTL:           15 * time.Minute,
			CleanupInterval: 5 * time.Minute,
		}

		assert.Equal(t, 10000, config.L1Size)
		assert.Equal(t, 15*time.Minute, config.L1TTL)
		assert.Equal(t, 5*time.Minute, config.CleanupInterval)
	})
}
