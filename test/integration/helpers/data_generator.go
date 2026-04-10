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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

var entityIDCounter uint64

// TestEntity represents a test entity with routing information.
type TestEntity struct {
	EntityType string
	Key        string
	Data       []byte
	ShardID    uint64
	Checksum   string
}

// TestDataPattern defines different data generation patterns.
type TestDataPattern int

const (
	// PatternUniform generates data uniformly distributed across shards.
	PatternUniform TestDataPattern = iota

	// PatternSkewed generates data skewed towards specific shards.
	PatternSkewed

	// PatternLargeObjects generates entities with large data payloads.
	PatternLargeObjects

	// PatternSmallObjects generates entities with small data payloads.
	PatternSmallObjects
)

// DataGeneratorOptions configures test data generation.
type DataGeneratorOptions struct {
	Count      int
	EntityType string
	Pattern    TestDataPattern
	ShardCount int
	MinSize    int // bytes
	MaxSize    int // bytes
	KeyPrefix  string
}

// DefaultDataGeneratorOptions returns default options for data generation.
func DefaultDataGeneratorOptions() *DataGeneratorOptions {
	return &DataGeneratorOptions{
		Count:      100,
		EntityType: "test_entity",
		Pattern:    PatternUniform,
		ShardCount: 3,
		MinSize:    100,
		MaxSize:    1000,
		KeyPrefix:  "test",
	}
}

// GenerateTestEntities generates test entities based on the specified pattern.
func GenerateTestEntities(opts *DataGeneratorOptions) []TestEntity {
	if opts == nil {
		opts = DefaultDataGeneratorOptions()
	}

	entities := make([]TestEntity, opts.Count)
	now := time.Now().UnixNano()

	for i := 0; i < opts.Count; i++ {
		entityID := atomic.AddUint64(&entityIDCounter, 1)
		key := fmt.Sprintf("%s_%d_%d", opts.KeyPrefix, now, entityID)

		// Generate data based on pattern
		var data []byte
		var shardID uint64

		switch opts.Pattern {
		case PatternUniform:
			data, shardID = generateUniformData(i, opts)
		case PatternSkewed:
			data, shardID = generateSkewedData(i, opts)
		case PatternLargeObjects:
			data, shardID = generateLargeObjectData(i, opts)
		case PatternSmallObjects:
			data, shardID = generateSmallObjectData(i, opts)
		default:
			data, shardID = generateUniformData(i, opts)
		}

		// Compute checksum
		hash := sha256.Sum256(data)
		checksum := hex.EncodeToString(hash[:])

		entities[i] = TestEntity{
			EntityType: opts.EntityType,
			Key:        key,
			Data:       data,
			ShardID:    shardID,
			Checksum:   checksum,
		}
	}

	return entities
}

// generateUniformData creates data uniformly distributed across shards.
func generateUniformData(index int, opts *DataGeneratorOptions) ([]byte, uint64) {
	shardID := uint64((index % opts.ShardCount) + 1)

	payload := map[string]interface{}{
		"id":        index,
		"shard":     shardID,
		"pattern":   "uniform",
		"timestamp": time.Now().Unix(),
		"data":      generateRandomString(opts.MinSize),
	}

	data, _ := json.Marshal(payload)
	return data, shardID
}

// generateSkewedData creates data skewed towards lower shard IDs.
func generateSkewedData(index int, opts *DataGeneratorOptions) ([]byte, uint64) {
	// 70% goes to shard 1, 20% to shard 2, 10% to shard 3
	var shardID uint64
	mod := index % 10
	if mod < 7 {
		shardID = 1
	} else if mod < 9 {
		shardID = 2
	} else {
		shardID = 3
	}

	payload := map[string]interface{}{
		"id":        index,
		"shard":     shardID,
		"pattern":   "skewed",
		"timestamp": time.Now().Unix(),
		"data":      generateRandomString(opts.MinSize),
	}

	data, _ := json.Marshal(payload)
	return data, shardID
}

// generateLargeObjectData creates entities with large payloads.
func generateLargeObjectData(index int, opts *DataGeneratorOptions) ([]byte, uint64) {
	shardID := uint64((index % opts.ShardCount) + 1)

	// Generate 10KB - 100KB objects
	size := 10240 + (index % 90000)

	payload := map[string]interface{}{
		"id":        index,
		"shard":     shardID,
		"pattern":   "large",
		"timestamp": time.Now().Unix(),
		"data":      generateRandomString(size),
	}

	data, _ := json.Marshal(payload)
	return data, shardID
}

// generateSmallObjectData creates entities with small payloads.
func generateSmallObjectData(index int, opts *DataGeneratorOptions) ([]byte, uint64) {
	shardID := uint64((index % opts.ShardCount) + 1)

	// Generate 50-200 byte objects
	size := 50 + (index % 150)

	payload := map[string]interface{}{
		"id":        index,
		"shard":     shardID,
		"pattern":   "small",
		"timestamp": time.Now().Unix(),
		"data":      generateRandomString(size),
	}

	data, _ := json.Marshal(payload)
	return data, shardID
}

// generateRandomString generates a pseudo-random string of specified length.
func generateRandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[i%len(charset)]
	}
	return string(b)
}

// WriteTestData writes test entities to the cluster and returns them.
func WriteTestData(t *testing.T, client quicbaas.Client, entities []TestEntity) []TestEntity {
	t.Helper()

	t.Logf("Writing %d test entities...", len(entities))
	startTime := time.Now()

	for i, entity := range entities {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := client.KV().Put(ctx, &service.PutRequest{
			EntityType:  entity.EntityType,
			ID:          id.HashString(entity.Key),
			Data:        entity.Data,
			Consistency: service.ConsistencyLevelLinearizable,
		})
		cancel()

		require.NoError(t, err, "Failed to write entity %d (%s)", i, entity.Key)

		// Log progress every 50 entities
		if (i+1)%50 == 0 {
			t.Logf("Wrote %d/%d entities...", i+1, len(entities))
		}
	}

	duration := time.Since(startTime)
	t.Logf("Wrote %d entities in %v (%.2f ops/sec)",
		len(entities), duration, float64(len(entities))/duration.Seconds())

	return entities
}

// ReadTestData reads test entities from the cluster and verifies checksums.
func ReadTestData(t *testing.T, client quicbaas.Client, entities []TestEntity) {
	t.Helper()

	t.Logf("Reading and verifying %d test entities...", len(entities))
	startTime := time.Now()

	for i, entity := range entities {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		resp, err := client.KV().Get(ctx, &service.GetRequest{
			EntityType:  entity.EntityType,
			ID:          id.HashString(entity.Key),
			Consistency: service.ConsistencyLevelLinearizable,
		})
		cancel()

		require.NoError(t, err, "Failed to read entity %d (%s)", i, entity.Key)
		require.True(t, resp.Found, "Entity %d (%s) not found", i, entity.Key)

		// Verify checksum
		hash := sha256.Sum256(resp.Data)
		actualChecksum := hex.EncodeToString(hash[:])
		require.Equal(t, entity.Checksum, actualChecksum,
			"Checksum mismatch for entity %d (%s)", i, entity.Key)

		// Log progress every 50 entities
		if (i+1)%50 == 0 {
			t.Logf("Read %d/%d entities...", i+1, len(entities))
		}
	}

	duration := time.Since(startTime)
	t.Logf("Read and verified %d entities in %v (%.2f ops/sec)",
		len(entities), duration, float64(len(entities))/duration.Seconds())
}

// ComputeChecksums computes SHA256 checksums for all entities.
func ComputeChecksums(entities []TestEntity) map[string]string {
	checksums := make(map[string]string, len(entities))

	for _, entity := range entities {
		key := fmt.Sprintf("%s:%s", entity.EntityType, entity.Key)
		checksums[key] = entity.Checksum
	}

	return checksums
}

// VerifyChecksums verifies that entity checksums match expected values.
func VerifyChecksums(t *testing.T, client quicbaas.Client, entities []TestEntity, expectedChecksums map[string]string) {
	t.Helper()

	t.Logf("Verifying checksums for %d entities...", len(entities))

	mismatches := 0
	for _, entity := range entities {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		resp, err := client.KV().Get(ctx, &service.GetRequest{
			EntityType:  entity.EntityType,
			ID:          id.HashString(entity.Key),
			Consistency: service.ConsistencyLevelLinearizable,
		})
		cancel()

		require.NoError(t, err, "Failed to read entity %s", entity.Key)
		require.True(t, resp.Found, "Entity %s not found", entity.Key)

		// Compute actual checksum
		hash := sha256.Sum256(resp.Data)
		actualChecksum := hex.EncodeToString(hash[:])

		// Get expected checksum
		key := fmt.Sprintf("%s:%s", entity.EntityType, entity.Key)
		expectedChecksum, ok := expectedChecksums[key]
		require.True(t, ok, "No expected checksum for entity %s", entity.Key)

		if actualChecksum != expectedChecksum {
			mismatches++
			t.Errorf("Checksum mismatch for entity %s: expected %s, got %s",
				entity.Key, expectedChecksum, actualChecksum)
		}
	}

	if mismatches == 0 {
		t.Logf("All checksums verified successfully")
	} else {
		t.Errorf("Found %d checksum mismatches", mismatches)
	}
}
