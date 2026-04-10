//go:build vector

package server

import (
	"testing"

	qrdbvector "github.com/jeremyhahn/go-qrdb/pkg/storage/vector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ---------------------------------------------------------------------------
// convertScanOptions
// ---------------------------------------------------------------------------

func TestConvertScanOptions_Nil(t *testing.T) {
	t.Run("returns nil for nil input", func(t *testing.T) {
		result := convertScanOptions(nil)
		assert.Nil(t, result)
	})
}

func TestConvertScanOptions_WithValues(t *testing.T) {
	t.Run("converts limit and offset", func(t *testing.T) {
		opts := &interfaces.VectorScanOptions{
			Limit:  50,
			Offset: 10,
		}
		result := convertScanOptions(opts)
		require.NotNil(t, result)
		assert.Equal(t, 50, result.Limit)
		assert.Equal(t, 10, result.Offset)
	})
}

// ---------------------------------------------------------------------------
// convertVectorValue
// ---------------------------------------------------------------------------

func TestConvertVectorValue_Nil(t *testing.T) {
	t.Run("returns nil for nil input", func(t *testing.T) {
		result := convertVectorValue(nil)
		assert.Nil(t, result)
	})
}

func TestConvertVectorValue_WithData(t *testing.T) {
	t.Run("converts all fields", func(t *testing.T) {
		vec := &qrdbvector.VectorValue{
			ID:        42,
			Embedding: []float32{1.0, 2.0, 3.0},
			Metadata:  map[string]string{"key": "value"},
			Version:   5,
			CreatedAt: 1000,
			UpdatedAt: 2000,
		}
		result := convertVectorValue(vec)
		require.NotNil(t, result)
		assert.Equal(t, uint64(42), result.ID)
		assert.Equal(t, []float32{1.0, 2.0, 3.0}, result.Embedding)
		assert.Equal(t, "value", result.Metadata["key"])
		assert.Equal(t, uint64(5), result.Version)
		assert.Equal(t, int64(1000), result.CreatedAt)
		assert.Equal(t, int64(2000), result.UpdatedAt)
	})
}

// ---------------------------------------------------------------------------
// convertVectorValues
// ---------------------------------------------------------------------------

func TestConvertVectorValues(t *testing.T) {
	t.Run("converts slice of vector values", func(t *testing.T) {
		vecs := []*qrdbvector.VectorValue{
			{ID: 1, Embedding: []float32{1.0}},
			{ID: 2, Embedding: []float32{2.0}},
			nil,
		}
		result := convertVectorValues(vecs)
		require.Len(t, result, 3)
		assert.Equal(t, uint64(1), result[0].ID)
		assert.Equal(t, uint64(2), result[1].ID)
		assert.Nil(t, result[2])
	})

	t.Run("empty slice returns empty", func(t *testing.T) {
		result := convertVectorValues([]*qrdbvector.VectorValue{})
		assert.Empty(t, result)
	})
}

// ---------------------------------------------------------------------------
// convertNamespaceInfo
// ---------------------------------------------------------------------------

func TestConvertNamespaceInfo(t *testing.T) {
	t.Run("converts all fields", func(t *testing.T) {
		info := &qrdbvector.NamespaceInfo{
			Name:           "test-ns",
			Dimension:      128,
			VectorCount:    1000,
			DistanceMetric: "cosine",
			IndexType:      "hnsw",
			IndexConfig:    map[string]string{"m": "16"},
			Metadata:       map[string]string{"env": "test"},
			TenantID:       "tenant-1",
			StorageBytes:   4096,
			CreatedAt:      1000,
			UpdatedAt:      2000,
		}
		result := convertNamespaceInfo(info)
		assert.Equal(t, "test-ns", result.Name)
		assert.Equal(t, uint32(128), result.Dimension)
		assert.Equal(t, uint64(1000), result.VectorCount)
		assert.Equal(t, "cosine", result.DistanceMetric)
		assert.Equal(t, "hnsw", result.IndexType)
		assert.Equal(t, "tenant-1", result.TenantID)
		assert.Equal(t, uint64(4096), result.StorageBytes)
		assert.Equal(t, int64(1000), result.CreatedAt)
		assert.Equal(t, int64(2000), result.UpdatedAt)
	})
}
