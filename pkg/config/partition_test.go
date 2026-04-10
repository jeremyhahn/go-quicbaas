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

package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPartitionConfig(t *testing.T) {
	t.Run("returns non-nil config with provided data dir", func(t *testing.T) {
		cfg := DefaultPartitionConfig("/var/lib/quicbaas/partition1")
		require.NotNil(t, cfg)
		assert.Equal(t, "/var/lib/quicbaas/partition1", cfg.DataDir)
	})

	t.Run("WAL dir defaults to empty string", func(t *testing.T) {
		cfg := DefaultPartitionConfig("/data/partition")
		assert.Equal(t, "", cfg.WALDir, "WAL dir should default to empty (will use DataDir/wal)")
	})

	t.Run("cache size defaults to zero", func(t *testing.T) {
		cfg := DefaultPartitionConfig("/data/partition")
		assert.Equal(t, int64(0), cfg.CacheSize, "cache size should default to 0 (use default)")
	})
}

func TestPartitionConfig_CustomValues(t *testing.T) {
	t.Run("custom WAL directory", func(t *testing.T) {
		cfg := &PartitionConfig{
			DataDir: "/data/partition1",
			WALDir:  "/ssd/wal/partition1",
		}
		assert.Equal(t, "/data/partition1", cfg.DataDir)
		assert.Equal(t, "/ssd/wal/partition1", cfg.WALDir)
	})

	t.Run("custom cache size", func(t *testing.T) {
		cfg := &PartitionConfig{
			DataDir:   "/data/partition1",
			CacheSize: 512 * 1024 * 1024, // 512MB
		}
		assert.Equal(t, int64(512*1024*1024), cfg.CacheSize)
	})

	t.Run("all fields set", func(t *testing.T) {
		cfg := &PartitionConfig{
			DataDir:   "/mnt/nvme/partition-hot",
			WALDir:    "/mnt/nvme-fast/wal",
			CacheSize: 1024 * 1024 * 1024, // 1GB
		}
		assert.Equal(t, "/mnt/nvme/partition-hot", cfg.DataDir)
		assert.Equal(t, "/mnt/nvme-fast/wal", cfg.WALDir)
		assert.Equal(t, int64(1024*1024*1024), cfg.CacheSize)
	})
}

func TestPartitionsConfig(t *testing.T) {
	t.Run("empty partitions map", func(t *testing.T) {
		partitions := make(PartitionsConfig)
		assert.Empty(t, partitions)
	})

	t.Run("single partition", func(t *testing.T) {
		partitions := PartitionsConfig{
			"default": DefaultPartitionConfig("/data/default"),
		}
		require.Len(t, partitions, 1)
		require.NotNil(t, partitions["default"])
		assert.Equal(t, "/data/default", partitions["default"].DataDir)
	})

	t.Run("multiple partitions", func(t *testing.T) {
		partitions := PartitionsConfig{
			"hot": &PartitionConfig{
				DataDir:   "/nvme/hot",
				CacheSize: 2 * 1024 * 1024 * 1024, // 2GB cache for hot partition
			},
			"warm": &PartitionConfig{
				DataDir:   "/ssd/warm",
				CacheSize: 512 * 1024 * 1024, // 512MB cache for warm
			},
			"cold": &PartitionConfig{
				DataDir:   "/hdd/cold",
				CacheSize: 64 * 1024 * 1024, // 64MB cache for cold
			},
		}
		require.Len(t, partitions, 3)

		assert.Equal(t, "/nvme/hot", partitions["hot"].DataDir)
		assert.Equal(t, int64(2*1024*1024*1024), partitions["hot"].CacheSize)

		assert.Equal(t, "/ssd/warm", partitions["warm"].DataDir)
		assert.Equal(t, int64(512*1024*1024), partitions["warm"].CacheSize)

		assert.Equal(t, "/hdd/cold", partitions["cold"].DataDir)
		assert.Equal(t, int64(64*1024*1024), partitions["cold"].CacheSize)
	})

	t.Run("partition lookup by name", func(t *testing.T) {
		partitions := PartitionsConfig{
			"primary":   DefaultPartitionConfig("/data/primary"),
			"secondary": DefaultPartitionConfig("/data/secondary"),
		}

		primary, ok := partitions["primary"]
		require.True(t, ok)
		assert.Equal(t, "/data/primary", primary.DataDir)

		secondary, ok := partitions["secondary"]
		require.True(t, ok)
		assert.Equal(t, "/data/secondary", secondary.DataDir)

		_, ok = partitions["nonexistent"]
		assert.False(t, ok)
	})

	t.Run("nil partition in map", func(t *testing.T) {
		partitions := PartitionsConfig{
			"valid":   DefaultPartitionConfig("/data/valid"),
			"invalid": nil,
		}
		require.Len(t, partitions, 2)
		assert.NotNil(t, partitions["valid"])
		assert.Nil(t, partitions["invalid"])
	})
}

func TestPartitionConfig_ZeroValue(t *testing.T) {
	t.Run("zero value struct has empty strings and zero cache", func(t *testing.T) {
		var cfg PartitionConfig
		assert.Equal(t, "", cfg.DataDir)
		assert.Equal(t, "", cfg.WALDir)
		assert.Equal(t, int64(0), cfg.CacheSize)
	})
}
