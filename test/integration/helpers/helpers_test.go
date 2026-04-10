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

//go:build integration

package helpers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerateTestEntities verifies test data generation.
func TestGenerateTestEntities(t *testing.T) {
	t.Run("Uniform", func(t *testing.T) {
		opts := &DataGeneratorOptions{
			Count:      10,
			EntityType: "test",
			Pattern:    PatternUniform,
			ShardCount: 3,
			MinSize:    100,
			MaxSize:    1000,
			KeyPrefix:  "test",
		}

		entities := GenerateTestEntities(opts)

		require.Len(t, entities, 10, "Should generate correct number of entities")

		// Verify all entities have required fields
		for i, entity := range entities {
			assert.NotEmpty(t, entity.Key, "Entity %d should have a key", i)
			assert.NotEmpty(t, entity.Data, "Entity %d should have data", i)
			assert.NotEmpty(t, entity.Checksum, "Entity %d should have checksum", i)
			assert.Equal(t, opts.EntityType, entity.EntityType, "Entity %d should have correct type", i)

			// Verify shard ID is in valid range
			assert.GreaterOrEqual(t, entity.ShardID, uint64(1), "Entity %d shard ID too low", i)
			assert.LessOrEqual(t, entity.ShardID, uint64(opts.ShardCount), "Entity %d shard ID too high", i)
		}
	})

	t.Run("Skewed", func(t *testing.T) {
		opts := &DataGeneratorOptions{
			Count:      100,
			EntityType: "test",
			Pattern:    PatternSkewed,
			ShardCount: 3,
			MinSize:    100,
			MaxSize:    1000,
			KeyPrefix:  "test",
		}

		entities := GenerateTestEntities(opts)

		// Count entities per shard
		shardCounts := make(map[uint64]int)
		for _, entity := range entities {
			shardCounts[entity.ShardID]++
		}

		// Verify skew (shard 1 should have most entities)
		assert.Greater(t, shardCounts[1], shardCounts[2], "Shard 1 should have more than shard 2")
		assert.Greater(t, shardCounts[2], shardCounts[3], "Shard 2 should have more than shard 3")

		t.Logf("Shard distribution (skewed): 1=%d, 2=%d, 3=%d",
			shardCounts[1], shardCounts[2], shardCounts[3])
	})

	t.Run("LargeObjects", func(t *testing.T) {
		opts := &DataGeneratorOptions{
			Count:      5,
			EntityType: "test",
			Pattern:    PatternLargeObjects,
			ShardCount: 3,
			KeyPrefix:  "test",
		}

		entities := GenerateTestEntities(opts)

		// Verify entities are large
		for i, entity := range entities {
			assert.Greater(t, len(entity.Data), 10000, "Entity %d should be large", i)
		}
	})

	t.Run("SmallObjects", func(t *testing.T) {
		opts := &DataGeneratorOptions{
			Count:      10,
			EntityType: "test",
			Pattern:    PatternSmallObjects,
			ShardCount: 3,
			KeyPrefix:  "test",
		}

		entities := GenerateTestEntities(opts)

		// Verify entities are small
		for i, entity := range entities {
			assert.Less(t, len(entity.Data), 500, "Entity %d should be small", i)
		}
	})
}

// TestComputeChecksums verifies checksum computation.
func TestComputeChecksums(t *testing.T) {
	opts := DefaultDataGeneratorOptions()
	opts.Count = 5
	entities := GenerateTestEntities(opts)

	checksums := ComputeChecksums(entities)

	require.Len(t, checksums, 5, "Should compute checksum for all entities")

	// Verify checksums are valid hex strings
	for key, checksum := range checksums {
		assert.Len(t, checksum, 64, "Checksum for %s should be 64 hex chars (SHA256)", key)
		assert.Regexp(t, "^[0-9a-f]+$", checksum, "Checksum for %s should be hex", key)
	}

	// Verify checksums match entity checksums
	for _, entity := range entities {
		key := entity.EntityType + ":" + entity.Key
		assert.Equal(t, entity.Checksum, checksums[key], "Checksum mismatch for %s", key)
	}
}

// TestDefaultDataGeneratorOptions verifies default options.
func TestDefaultDataGeneratorOptions(t *testing.T) {
	opts := DefaultDataGeneratorOptions()

	assert.Equal(t, 100, opts.Count)
	assert.Equal(t, "test_entity", opts.EntityType)
	assert.Equal(t, PatternUniform, opts.Pattern)
	assert.Equal(t, 3, opts.ShardCount)
	assert.Equal(t, 100, opts.MinSize)
	assert.Equal(t, 1000, opts.MaxSize)
	assert.Equal(t, "test", opts.KeyPrefix)
}

// TestClusterBuilderDefaults verifies cluster builder defaults.
func TestClusterBuilderDefaults(t *testing.T) {
	builder := NewClusterBuilder()

	assert.NotNil(t, builder)
	assert.Len(t, builder.endpoints, 3, "Should have 3 default endpoints")
	assert.Equal(t, "test-node1:50051", builder.endpoints[0])
	assert.Equal(t, "test-node2:50051", builder.endpoints[1])
	assert.Equal(t, "test-node3:50051", builder.endpoints[2])
}

// TestClusterBuilderFluent verifies fluent API.
func TestClusterBuilderFluent(t *testing.T) {
	builder := NewClusterBuilder().
		WithEndpoints([]string{"localhost:50051"}).
		WithTimeout(60 * time.Second).
		WithRetries(5)

	assert.Len(t, builder.endpoints, 1)
	assert.Equal(t, "localhost:50051", builder.endpoints[0])
	assert.Equal(t, 60*time.Second, builder.timeout)
	assert.Equal(t, 5, builder.retries)
}
