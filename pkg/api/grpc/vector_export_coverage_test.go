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
	"bytes"
	"compress/gzip"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeserializeExportBatch_TruncatedGzip tests deserialization with a truncated
// gzip payload that decompresses partially but fails on ReadAll.
func TestDeserializeExportBatch_TruncatedGzip(t *testing.T) {
	t.Run("truncated_gzip_returns_error", func(t *testing.T) {
		// Create a valid gzip stream with valid JSON, then truncate it
		var buf bytes.Buffer
		gz := gzip.NewWriter(&buf)
		_, err := gz.Write([]byte(`{"vectors":[],"namespace":"ns","batch_num":1,"total":0}`))
		require.NoError(t, err)
		err = gz.Close()
		require.NoError(t, err)

		fullData := buf.Bytes()

		// Truncate to half - this preserves valid gzip header but corrupts the stream
		truncated := fullData[:len(fullData)/2]

		_, err = deserializeExportBatch(truncated, "json", "gzip")
		require.Error(t, err)
	})
}

// TestSerializeExportBatch_DefaultFormatFallback tests that an unrecognized format
// falls back to JSON serialization.
func TestSerializeExportBatch_DefaultFormatFallback(t *testing.T) {
	batch := &VectorExportBatch{
		Vectors:   []VectorExportEntry{{ID: "v1", Vector: []float32{1.0}}},
		Namespace: "ns",
		BatchNum:  1,
		Total:     1,
	}

	t.Run("unknown_format_uses_json", func(t *testing.T) {
		data, err := serializeExportBatch(batch, "protobuf", "none")
		require.NoError(t, err)
		assert.Contains(t, string(data), "v1")
	})
}

// TestDeserializeExportBatch_DefaultFormatFallback_BadJSON tests the default format
// branch with invalid JSON to trigger the error path.
func TestDeserializeExportBatch_DefaultFormatFallback_BadJSON(t *testing.T) {
	t.Run("default_format_with_invalid_json", func(t *testing.T) {
		_, err := deserializeExportBatch([]byte("{invalid json"), "protobuf", "none")
		require.Error(t, err)
	})
}

// TestExportEntriesToVectorData_ReverseConversion tests the reverse conversion
// from export entries back to VectorData format.
func TestExportEntriesToVectorData_ReverseConversion(t *testing.T) {
	t.Run("converts_entries_with_metadata", func(t *testing.T) {
		entries := []VectorExportEntry{
			{
				ID:       "123",
				Vector:   []float32{1.0, 2.0},
				Metadata: map[string]string{"key": "value"},
			},
			{
				ID:     "456",
				Vector: []float32{3.0, 4.0},
			},
		}

		result := exportEntriesToVectorData(entries)
		require.Len(t, result, 2)
		assert.Equal(t, uint64(123), result[0].ID)
		assert.Equal(t, []float32{1.0, 2.0}, result[0].Vector)
		assert.Equal(t, "value", result[0].Metadata["key"])

		assert.Equal(t, uint64(456), result[1].ID)
		assert.Empty(t, result[1].Metadata)
	})

	t.Run("handles_non_numeric_id_by_hashing", func(t *testing.T) {
		entries := []VectorExportEntry{
			{ID: "not-a-number", Vector: []float32{1.0}},
		}

		result := exportEntriesToVectorData(entries)
		require.Len(t, result, 1)
		// parseVectorID hashes non-numeric strings to a deterministic uint64
		assert.NotEqual(t, uint64(0), result[0].ID, "non-numeric IDs should be hashed to a non-zero value")
	})
}
