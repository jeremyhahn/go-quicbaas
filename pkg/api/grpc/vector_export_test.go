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

package grpc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestSerializeExportBatch_JSON(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors: []VectorExportEntry{
			{
				ID:       "vec-1",
				Vector:   []float32{1.0, 2.0, 3.0},
				Metadata: map[string]string{"key": "value"},
				Version:  1,
			},
		},
		Namespace: "test-ns",
		BatchNum:  1,
		Total:     1,
	}

	data, err := serializeExportBatch(batch, "json", "none")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	assert.Contains(t, string(data), "vec-1")
	assert.Contains(t, string(data), "test-ns")
}

func TestSerializeExportBatch_JSONWithGzip(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors: []VectorExportEntry{
			{
				ID:     "vec-1",
				Vector: []float32{1.0, 2.0, 3.0},
			},
		},
		Namespace: "test-ns",
		BatchNum:  1,
		Total:     1,
	}

	data, err := serializeExportBatch(batch, "json", "gzip")
	require.NoError(t, err)
	assert.NotEmpty(t, data)
	// Gzip magic number
	assert.Equal(t, byte(0x1f), data[0])
	assert.Equal(t, byte(0x8b), data[1])
}

func TestDeserializeExportBatch_JSON(t *testing.T) {
	original := &VectorExportBatch{
		Vectors: []VectorExportEntry{
			{
				ID:       "vec-1",
				Vector:   []float32{1.0, 2.0, 3.0},
				Metadata: map[string]string{"key": "value"},
				Version:  1,
			},
		},
		Namespace: "test-ns",
		BatchNum:  1,
		Total:     1,
	}

	// Serialize
	data, err := serializeExportBatch(original, "json", "none")
	require.NoError(t, err)

	// Deserialize
	result, err := deserializeExportBatch(data, "json", "none")
	require.NoError(t, err)
	assert.Equal(t, original.Namespace, result.Namespace)
	assert.Equal(t, original.BatchNum, result.BatchNum)
	assert.Len(t, result.Vectors, 1)
	assert.Equal(t, original.Vectors[0].ID, result.Vectors[0].ID)
}

func TestDeserializeExportBatch_JSONWithGzip(t *testing.T) {
	original := &VectorExportBatch{
		Vectors: []VectorExportEntry{
			{
				ID:     "vec-1",
				Vector: []float32{1.0, 2.0, 3.0},
			},
		},
		Namespace: "test-ns",
		BatchNum:  1,
		Total:     1,
	}

	// Serialize with gzip
	data, err := serializeExportBatch(original, "json", "gzip")
	require.NoError(t, err)

	// Deserialize with gzip
	result, err := deserializeExportBatch(data, "json", "gzip")
	require.NoError(t, err)
	assert.Equal(t, original.Namespace, result.Namespace)
	assert.Len(t, result.Vectors, 1)
}

func TestCalculateChecksum(t *testing.T) {
	data := []byte("test data for checksum")
	checksum := calculateChecksum(data)
	assert.NotZero(t, checksum)

	// Same data should produce same checksum
	checksum2 := calculateChecksum(data)
	assert.Equal(t, checksum, checksum2)

	// Different data should produce different checksum
	checksum3 := calculateChecksum([]byte("different data"))
	assert.NotEqual(t, checksum, checksum3)
}

func TestVectorDataToExportEntries(t *testing.T) {
	vectors := []*service.VectorData{
		{
			ID:        123,
			Vector:    []float32{1.0, 2.0, 3.0},
			Metadata:  map[string]interface{}{"key": "value"},
			Version:   1,
			CreatedAt: 1000000,
			UpdatedAt: 2000000,
		},
		{
			ID:        456,
			Vector:    []float32{4.0, 5.0, 6.0},
			Metadata:  map[string]interface{}{"int_key": 42},
			Version:   2,
			CreatedAt: 3000000,
			UpdatedAt: 4000000,
		},
	}

	entries := vectorDataToExportEntries(vectors)
	require.Len(t, entries, 2)

	// First entry
	assert.Equal(t, "123", entries[0].ID)
	assert.Equal(t, []float32{1.0, 2.0, 3.0}, entries[0].Vector)
	assert.Equal(t, "value", entries[0].Metadata["key"])
	assert.Equal(t, uint64(1), entries[0].Version)
	assert.Equal(t, int64(1000000), entries[0].CreatedAt)
	assert.Equal(t, int64(2000000), entries[0].UpdatedAt)

	// Second entry with int metadata converted to string
	assert.Equal(t, "456", entries[1].ID)
	assert.Equal(t, "42", entries[1].Metadata["int_key"])
}

func TestExportEntriesToVectorData(t *testing.T) {
	entries := []VectorExportEntry{
		{
			ID:       "123",
			Vector:   []float32{1.0, 2.0, 3.0},
			Metadata: map[string]string{"key": "value"},
		},
		{
			ID:       "my-vector-id",
			Vector:   []float32{4.0, 5.0, 6.0},
			Metadata: map[string]string{"another": "meta"},
		},
	}

	vectors := exportEntriesToVectorData(entries)
	require.Len(t, vectors, 2)

	// Numeric ID
	assert.Equal(t, uint64(123), vectors[0].ID)
	assert.Equal(t, []float32{1.0, 2.0, 3.0}, vectors[0].Vector)
	assert.Equal(t, "value", vectors[0].Metadata["key"])

	// Non-numeric ID gets hashed
	assert.NotZero(t, vectors[1].ID)
	assert.Equal(t, []float32{4.0, 5.0, 6.0}, vectors[1].Vector)
	assert.Equal(t, "meta", vectors[1].Metadata["another"])
}

func TestRoundTripSerialization(t *testing.T) {
	// Test full round-trip: VectorData -> Export -> Serialize -> Deserialize -> VectorData
	original := []*service.VectorData{
		{
			ID:       100,
			Vector:   []float32{0.1, 0.2, 0.3, 0.4},
			Metadata: map[string]interface{}{"category": "test", "count": 5},
			Version:  10,
		},
	}

	// Convert to export entries
	entries := vectorDataToExportEntries(original)

	// Create batch
	batch := &VectorExportBatch{
		Vectors:   entries,
		Namespace: "round-trip-test",
		BatchNum:  1,
		Total:     1,
	}

	// Serialize
	data, err := serializeExportBatch(batch, "json", "none")
	require.NoError(t, err)

	// Deserialize
	restored, err := deserializeExportBatch(data, "json", "none")
	require.NoError(t, err)

	// Convert back to VectorData
	restoredVectors := exportEntriesToVectorData(restored.Vectors)
	require.Len(t, restoredVectors, 1)

	// Verify data preserved
	assert.Equal(t, original[0].ID, restoredVectors[0].ID)
	assert.Equal(t, original[0].Vector, restoredVectors[0].Vector)
}

func TestDeserializeExportBatch_InvalidJSON(t *testing.T) {
	_, err := deserializeExportBatch([]byte("invalid json{"), "json", "none")
	assert.Error(t, err)
}

func TestDeserializeExportBatch_InvalidGzip(t *testing.T) {
	_, err := deserializeExportBatch([]byte("not gzip data"), "json", "gzip")
	assert.Error(t, err)
}

func TestSerializeExportBatch_DefaultFormat(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors:   []VectorExportEntry{},
		Namespace: "test",
		BatchNum:  1,
	}

	// Empty format defaults to JSON
	data, err := serializeExportBatch(batch, "", "none")
	require.NoError(t, err)
	assert.Contains(t, string(data), "namespace")

	// Unknown format defaults to JSON
	data2, err := serializeExportBatch(batch, "unknown", "none")
	require.NoError(t, err)
	assert.Contains(t, string(data2), "namespace")
}

func TestVectorDataToExportEntries_EmptyMetadata(t *testing.T) {
	vectors := []*service.VectorData{
		{
			ID:       1,
			Vector:   []float32{1.0},
			Metadata: nil,
		},
	}

	entries := vectorDataToExportEntries(vectors)
	require.Len(t, entries, 1)
	assert.Empty(t, entries[0].Metadata)
}

func TestExportEntriesToVectorData_EmptyMetadata(t *testing.T) {
	entries := []VectorExportEntry{
		{
			ID:       "1",
			Vector:   []float32{1.0},
			Metadata: nil,
		},
	}

	vectors := exportEntriesToVectorData(entries)
	require.Len(t, vectors, 1)
	assert.NotNil(t, vectors[0].Metadata) // Empty map, not nil
	assert.Empty(t, vectors[0].Metadata)
}

// =============================================================================
// Additional Tests for Full Coverage
// =============================================================================

func TestSerializeExportBatch_EmptyCompression(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors:   []VectorExportEntry{},
		Namespace: "test",
		BatchNum:  1,
	}

	// Empty compression defaults to no compression
	data, err := serializeExportBatch(batch, "json", "")
	require.NoError(t, err)
	assert.Contains(t, string(data), "namespace")
}

func TestSerializeExportBatch_UnknownCompression(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors:   []VectorExportEntry{},
		Namespace: "test",
		BatchNum:  1,
	}

	// Unknown compression defaults to no compression
	data, err := serializeExportBatch(batch, "json", "unknown")
	require.NoError(t, err)
	assert.Contains(t, string(data), "namespace")
}

func TestDeserializeExportBatch_EmptyCompression(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors:   []VectorExportEntry{},
		Namespace: "test",
		BatchNum:  1,
	}

	// Serialize without compression
	data, err := serializeExportBatch(batch, "json", "none")
	require.NoError(t, err)

	// Deserialize with empty compression
	result, err := deserializeExportBatch(data, "json", "")
	require.NoError(t, err)
	assert.Equal(t, "test", result.Namespace)
}

func TestDeserializeExportBatch_UnknownFormat(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors:   []VectorExportEntry{},
		Namespace: "test",
		BatchNum:  1,
	}

	// Serialize with JSON
	data, err := serializeExportBatch(batch, "json", "none")
	require.NoError(t, err)

	// Deserialize with unknown format (defaults to JSON)
	result, err := deserializeExportBatch(data, "unknown", "none")
	require.NoError(t, err)
	assert.Equal(t, "test", result.Namespace)
}

func TestDeserializeExportBatch_EmptyFormat(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors:   []VectorExportEntry{},
		Namespace: "test",
		BatchNum:  1,
	}

	// Serialize with JSON
	data, err := serializeExportBatch(batch, "json", "none")
	require.NoError(t, err)

	// Deserialize with empty format (defaults to JSON)
	result, err := deserializeExportBatch(data, "", "none")
	require.NoError(t, err)
	assert.Equal(t, "test", result.Namespace)
}
