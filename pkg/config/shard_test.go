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

func TestDefaultShardConfig(t *testing.T) {
	t.Run("returns non-nil config with provided values", func(t *testing.T) {
		cfg := DefaultShardConfig(1, "default")
		require.NotNil(t, cfg)
		assert.Equal(t, uint64(1), cfg.ID)
		assert.Equal(t, "default", cfg.Partition)
	})

	t.Run("uses default state machine", func(t *testing.T) {
		cfg := DefaultShardConfig(1, "primary")
		assert.Equal(t, DefaultStateMachine, cfg.StateMachine)
		assert.Equal(t, "fast", cfg.StateMachine)
	})

	t.Run("different shard IDs", func(t *testing.T) {
		cfg1 := DefaultShardConfig(1, "partition1")
		cfg2 := DefaultShardConfig(100, "partition2")
		cfg3 := DefaultShardConfig(uint64(1<<20), "partition3")

		assert.Equal(t, uint64(1), cfg1.ID)
		assert.Equal(t, uint64(100), cfg2.ID)
		assert.Equal(t, uint64(1<<20), cfg3.ID)
	})
}

func TestShardConfig_CustomValues(t *testing.T) {
	t.Run("custom state machine type", func(t *testing.T) {
		cfg := &ShardConfig{
			ID:           1,
			Partition:    "vectors",
			StateMachine: "vector",
		}
		assert.Equal(t, uint64(1), cfg.ID)
		assert.Equal(t, "vectors", cfg.Partition)
		assert.Equal(t, "vector", cfg.StateMachine)
	})

	t.Run("minimal state machine", func(t *testing.T) {
		cfg := &ShardConfig{
			ID:           2,
			Partition:    "counters",
			StateMachine: "minimal",
		}
		assert.Equal(t, "minimal", cfg.StateMachine)
	})

	t.Run("custom registered state machine", func(t *testing.T) {
		cfg := &ShardConfig{
			ID:           3,
			Partition:    "custom",
			StateMachine: "custom-timeseries",
		}
		assert.Equal(t, "custom-timeseries", cfg.StateMachine)
	})
}

func TestShardsConfig(t *testing.T) {
	t.Run("empty shards list", func(t *testing.T) {
		shards := make(ShardsConfig, 0)
		assert.Empty(t, shards)
	})

	t.Run("single shard", func(t *testing.T) {
		shards := ShardsConfig{
			DefaultShardConfig(1, "default"),
		}
		require.Len(t, shards, 1)
		assert.Equal(t, uint64(1), shards[0].ID)
		assert.Equal(t, "default", shards[0].Partition)
	})

	t.Run("multiple shards with different partitions", func(t *testing.T) {
		shards := ShardsConfig{
			&ShardConfig{ID: 1, Partition: "kv-primary", StateMachine: "fast"},
			&ShardConfig{ID: 2, Partition: "kv-primary", StateMachine: "fast"},
			&ShardConfig{ID: 3, Partition: "vectors", StateMachine: "vector"},
			&ShardConfig{ID: 4, Partition: "metadata", StateMachine: "minimal"},
		}
		require.Len(t, shards, 4)

		// Verify shard 1
		assert.Equal(t, uint64(1), shards[0].ID)
		assert.Equal(t, "kv-primary", shards[0].Partition)
		assert.Equal(t, "fast", shards[0].StateMachine)

		// Verify shard 3 (vector shard)
		assert.Equal(t, uint64(3), shards[2].ID)
		assert.Equal(t, "vectors", shards[2].Partition)
		assert.Equal(t, "vector", shards[2].StateMachine)

		// Verify shard 4 (metadata)
		assert.Equal(t, uint64(4), shards[3].ID)
		assert.Equal(t, "metadata", shards[3].Partition)
		assert.Equal(t, "minimal", shards[3].StateMachine)
	})

	t.Run("find shard by ID", func(t *testing.T) {
		shards := ShardsConfig{
			&ShardConfig{ID: 10, Partition: "p1"},
			&ShardConfig{ID: 20, Partition: "p2"},
			&ShardConfig{ID: 30, Partition: "p3"},
		}

		// Find shard with ID 20
		var found *ShardConfig
		for _, s := range shards {
			if s.ID == 20 {
				found = s
				break
			}
		}
		require.NotNil(t, found)
		assert.Equal(t, "p2", found.Partition)

		// Shard not found
		var notFound *ShardConfig
		for _, s := range shards {
			if s.ID == 99 {
				notFound = s
				break
			}
		}
		assert.Nil(t, notFound)
	})

	t.Run("multiple shards on same partition", func(t *testing.T) {
		shards := ShardsConfig{
			&ShardConfig{ID: 1, Partition: "shared"},
			&ShardConfig{ID: 2, Partition: "shared"},
			&ShardConfig{ID: 3, Partition: "shared"},
		}

		// All three shards share the same partition
		for _, s := range shards {
			assert.Equal(t, "shared", s.Partition)
		}
	})

	t.Run("nil shard in list", func(t *testing.T) {
		shards := ShardsConfig{
			&ShardConfig{ID: 1, Partition: "valid"},
			nil,
			&ShardConfig{ID: 3, Partition: "valid"},
		}
		require.Len(t, shards, 3)
		assert.NotNil(t, shards[0])
		assert.Nil(t, shards[1])
		assert.NotNil(t, shards[2])
	})
}

func TestShardConfig_ZeroValue(t *testing.T) {
	t.Run("zero value struct has zero ID and empty strings", func(t *testing.T) {
		var cfg ShardConfig
		assert.Equal(t, uint64(0), cfg.ID)
		assert.Equal(t, "", cfg.Partition)
		assert.Equal(t, "", cfg.StateMachine)
	})
}

func TestDefaultStateMachine(t *testing.T) {
	t.Run("constant is set to fast", func(t *testing.T) {
		assert.Equal(t, "fast", DefaultStateMachine)
	})
}

func TestShardConfig_IDValidation(t *testing.T) {
	t.Run("valid shard IDs are greater than zero", func(t *testing.T) {
		// This test documents the expected behavior: shard IDs must be > 0
		cfg := DefaultShardConfig(1, "test")
		assert.Greater(t, cfg.ID, uint64(0), "shard ID must be greater than 0")
	})

	t.Run("zero shard ID represents invalid config", func(t *testing.T) {
		// Zero ID is technically possible but represents an invalid shard
		cfg := &ShardConfig{
			ID:        0,
			Partition: "test",
		}
		assert.Equal(t, uint64(0), cfg.ID)
		// Note: Validation should be done by the validator, not the struct
	})
}
