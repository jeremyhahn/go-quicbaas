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

package quic

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
)

func TestIsEmbeddingService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{"fully qualified service", "quicbaas.Embedding", true},
		{"bare service name", "Embedding", true},
		{"non-embedding service KV", "quicbaas.KV", false},
		{"non-embedding service Vector", "quicbaas.Vector", false},
		{"empty service", "", false},
		{"partial match prefix", "quicbaas.EmbeddingExtended", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isEmbeddingService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertEmbeddingResponse_GenerateEmbedding(t *testing.T) {
	t.Run("converts generate embedding response", func(t *testing.T) {
		data := map[string]interface{}{
			"embedding": []interface{}{float64(0.1), float64(0.2), float64(0.3)},
		}

		result, err := convertEmbeddingResponse("GenerateEmbedding", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.GenerateEmbeddingResponse)
		require.True(t, ok, "expected *embedding.GenerateEmbeddingResponse")
		require.Len(t, resp.Embedding, 3)
		assert.InDelta(t, float32(0.1), resp.Embedding[0], 0.001)
		assert.InDelta(t, float32(0.2), resp.Embedding[1], 0.001)
		assert.InDelta(t, float32(0.3), resp.Embedding[2], 0.001)
	})

	t.Run("returns error on invalid data", func(t *testing.T) {
		data := map[string]interface{}{
			"embedding": "not-an-array",
		}

		_, err := convertEmbeddingResponse("GenerateEmbedding", data)
		require.Error(t, err)
	})
}

func TestConvertEmbeddingResponse_BatchGenerateEmbeddings(t *testing.T) {
	t.Run("converts batch generate embeddings response", func(t *testing.T) {
		data := map[string]interface{}{
			"success_count": float64(2),
			"error_count":   float64(0),
			"total_tokens":  float64(150),
		}

		result, err := convertEmbeddingResponse("BatchGenerateEmbeddings", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.BatchGenerateEmbeddingsResponse)
		require.True(t, ok, "expected *embedding.BatchGenerateEmbeddingsResponse")
		assert.Equal(t, 2, resp.SuccessCount)
		assert.Equal(t, 0, resp.ErrorCount)
		assert.Equal(t, 150, resp.TotalTokens)
	})

	t.Run("returns error on invalid data", func(t *testing.T) {
		data := map[string]interface{}{
			"success_count": "not-a-number",
		}

		_, err := convertEmbeddingResponse("BatchGenerateEmbeddings", data)
		require.Error(t, err)
	})
}

func TestConvertEmbeddingResponse_ListProviders(t *testing.T) {
	t.Run("converts list providers response", func(t *testing.T) {
		data := map[string]interface{}{
			"default_provider": "voyage",
		}

		result, err := convertEmbeddingResponse("ListProviders", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.ListProvidersResponse)
		require.True(t, ok, "expected *embedding.ListProvidersResponse")
		assert.Equal(t, "voyage", resp.DefaultProvider)
	})

	t.Run("returns error on invalid data", func(t *testing.T) {
		data := map[string]interface{}{
			"providers": "not-an-array",
		}

		_, err := convertEmbeddingResponse("ListProviders", data)
		require.Error(t, err)
	})
}

func TestConvertEmbeddingResponse_GetProviderInfo(t *testing.T) {
	t.Run("converts get provider info response", func(t *testing.T) {
		data := map[string]interface{}{}

		result, err := convertEmbeddingResponse("GetProviderInfo", data)
		require.NoError(t, err)

		_, ok := result.(*embedding.GetProviderInfoResponse)
		require.True(t, ok, "expected *embedding.GetProviderInfoResponse")
	})

	t.Run("returns error on invalid data", func(t *testing.T) {
		data := map[string]interface{}{
			"info": "not-an-object",
		}

		_, err := convertEmbeddingResponse("GetProviderInfo", data)
		require.Error(t, err)
	})
}

func TestConvertEmbeddingResponse_SetDefaultProvider(t *testing.T) {
	t.Run("converts set default provider response", func(t *testing.T) {
		data := map[string]interface{}{
			"provider":          "openai",
			"previous_provider": "voyage",
		}

		result, err := convertEmbeddingResponse("SetDefaultProvider", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.SetDefaultProviderResponse)
		require.True(t, ok, "expected *embedding.SetDefaultProviderResponse")
		assert.Equal(t, embedding.Provider("openai"), resp.Provider)
		assert.Equal(t, embedding.Provider("voyage"), resp.PreviousProvider)
	})

	t.Run("returns error on invalid data", func(t *testing.T) {
		data := map[string]interface{}{
			"provider": float64(123),
		}

		_, err := convertEmbeddingResponse("SetDefaultProvider", data)
		require.Error(t, err)
	})
}

func TestConvertEmbeddingResponse_GetCacheStats(t *testing.T) {
	t.Run("converts get cache stats response", func(t *testing.T) {
		data := map[string]interface{}{
			"enabled":  true,
			"size":     float64(100),
			"capacity": float64(1000),
			"hits":     float64(50),
			"misses":   float64(25),
			"hit_rate": float64(0.667),
		}

		result, err := convertEmbeddingResponse("GetCacheStats", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.GetEmbeddingCacheStatsResponse)
		require.True(t, ok, "expected *embedding.GetEmbeddingCacheStatsResponse")
		assert.True(t, resp.Enabled)
		assert.Equal(t, 100, resp.Size)
		assert.Equal(t, 1000, resp.Capacity)
		assert.Equal(t, uint64(50), resp.Hits)
		assert.Equal(t, uint64(25), resp.Misses)
		assert.InDelta(t, 0.667, resp.HitRate, 0.001)
	})

	t.Run("returns error on invalid data", func(t *testing.T) {
		data := map[string]interface{}{
			"enabled": "not-a-bool",
		}

		_, err := convertEmbeddingResponse("GetCacheStats", data)
		require.Error(t, err)
	})
}

func TestConvertEmbeddingResponse_ClearCache(t *testing.T) {
	t.Run("converts clear cache response", func(t *testing.T) {
		data := map[string]interface{}{
			"entries_cleared": float64(42),
			"provider":        "voyage",
		}

		result, err := convertEmbeddingResponse("ClearCache", data)
		require.NoError(t, err)

		resp, ok := result.(*embedding.ClearEmbeddingCacheResponse)
		require.True(t, ok, "expected *embedding.ClearEmbeddingCacheResponse")
		assert.Equal(t, 42, resp.EntriesCleared)
		assert.Equal(t, embedding.Provider("voyage"), resp.Provider)
	})

	t.Run("returns error on invalid data", func(t *testing.T) {
		data := map[string]interface{}{
			"entries_cleared": "not-a-number",
		}

		_, err := convertEmbeddingResponse("ClearCache", data)
		require.Error(t, err)
	})
}

func TestConvertEmbeddingResponse_UnknownMethod(t *testing.T) {
	t.Run("unknown method returns raw map", func(t *testing.T) {
		data := map[string]interface{}{
			"some_key": "some_value",
		}

		result, err := convertEmbeddingResponse("UnknownMethod", data)
		require.NoError(t, err)

		mapResult, ok := result.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "some_value", mapResult["some_key"])
	})
}

func TestConvertEmbeddingResponse_NilData(t *testing.T) {
	t.Run("nil data returns nil", func(t *testing.T) {
		result, err := convertEmbeddingResponse("GenerateEmbedding", nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestEmbeddingOpcodes_MatchServerProtocol(t *testing.T) {
	t.Run("embedding opcodes match server-side values", func(t *testing.T) {
		assert.Equal(t, byte(0x4D), OpEmbeddingGenerate)
		assert.Equal(t, byte(0x4E), OpEmbeddingBatchGenerate)
		assert.Equal(t, byte(0x4F), OpEmbeddingListProviders)
		assert.Equal(t, byte(0x55), OpEmbeddingGetProvider)
		assert.Equal(t, byte(0x56), OpEmbeddingSetDefault)
		assert.Equal(t, byte(0x57), OpEmbeddingGetCacheStats)
		assert.Equal(t, byte(0x58), OpEmbeddingClearCache)
	})
}

func TestEmbeddingOpcodes_NoConflicts(t *testing.T) {
	t.Run("embedding opcodes do not conflict with cluster opcodes", func(t *testing.T) {
		assert.NotEqual(t, OpEmbeddingGenerate, OpClusterStatus)
		assert.NotEqual(t, OpEmbeddingGenerate, OpNodeReplaceRequest)
	})

	t.Run("embedding opcodes do not conflict with backup opcodes", func(t *testing.T) {
		assert.NotEqual(t, OpEmbeddingGetProvider, OpBackupCreate)
		assert.NotEqual(t, OpEmbeddingSetDefault, OpBackupList)
		assert.NotEqual(t, OpEmbeddingGetCacheStats, OpBackupRestore)
		assert.NotEqual(t, OpEmbeddingClearCache, OpBackupDelete)
	})
}

func TestEmbeddingMethodToOpcode(t *testing.T) {
	t.Run("new format methods resolve to correct opcodes", func(t *testing.T) {
		tests := []struct {
			service  string
			method   string
			expected byte
		}{
			{"quicbaas.Embedding", "GenerateEmbedding", OpEmbeddingGenerate},
			{"quicbaas.Embedding", "BatchGenerateEmbeddings", OpEmbeddingBatchGenerate},
			{"quicbaas.Embedding", "ListProviders", OpEmbeddingListProviders},
			{"quicbaas.Embedding", "GetProviderInfo", OpEmbeddingGetProvider},
			{"quicbaas.Embedding", "SetDefaultProvider", OpEmbeddingSetDefault},
			{"quicbaas.Embedding", "GetCacheStats", OpEmbeddingGetCacheStats},
			{"quicbaas.Embedding", "ClearCache", OpEmbeddingClearCache},
		}

		for _, tt := range tests {
			t.Run(tt.method, func(t *testing.T) {
				opcode, ok := MethodToOpcode(tt.service, tt.method)
				require.True(t, ok, "method should be found: %s/%s", tt.service, tt.method)
				assert.Equal(t, tt.expected, opcode)
			})
		}
	})

	t.Run("legacy format methods resolve to correct opcodes", func(t *testing.T) {
		tests := []struct {
			service  string
			method   string
			expected byte
		}{
			{"Embedding", "Generate", OpEmbeddingGenerate},
			{"Embedding", "BatchGenerate", OpEmbeddingBatchGenerate},
			{"Embedding", "ListProviders", OpEmbeddingListProviders},
			{"Embedding", "GetProvider", OpEmbeddingGetProvider},
			{"Embedding", "SetDefault", OpEmbeddingSetDefault},
			{"Embedding", "GetCacheStats", OpEmbeddingGetCacheStats},
			{"Embedding", "ClearCache", OpEmbeddingClearCache},
		}

		for _, tt := range tests {
			t.Run(tt.method, func(t *testing.T) {
				opcode, ok := MethodToOpcode(tt.service, tt.method)
				require.True(t, ok, "method should be found: %s/%s", tt.service, tt.method)
				assert.Equal(t, tt.expected, opcode)
			})
		}
	})

	t.Run("unknown embedding method returns false", func(t *testing.T) {
		_, ok := MethodToOpcode("quicbaas.Embedding", "UnknownMethod")
		assert.False(t, ok)
	})
}

func TestEmbeddingOpcodeToMethod(t *testing.T) {
	t.Run("embedding opcodes resolve to correct methods", func(t *testing.T) {
		tests := []struct {
			opcode   byte
			expected string
		}{
			{OpEmbeddingGenerate, "quicbaas.Embedding/GenerateEmbedding"},
			{OpEmbeddingBatchGenerate, "quicbaas.Embedding/BatchGenerateEmbeddings"},
			{OpEmbeddingListProviders, "quicbaas.Embedding/ListProviders"},
			{OpEmbeddingGetProvider, "quicbaas.Embedding/GetProviderInfo"},
			{OpEmbeddingSetDefault, "quicbaas.Embedding/SetDefaultProvider"},
			{OpEmbeddingGetCacheStats, "quicbaas.Embedding/GetCacheStats"},
			{OpEmbeddingClearCache, "quicbaas.Embedding/ClearCache"},
		}

		for _, tt := range tests {
			t.Run(tt.expected, func(t *testing.T) {
				method, ok := OpcodeToMethod(tt.opcode)
				require.True(t, ok, "opcode should be found: 0x%02X", tt.opcode)
				assert.Equal(t, tt.expected, method)
			})
		}
	})
}
