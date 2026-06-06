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
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// embeddingService core coverage.
//
// The richer embedding suites are gated behind the `vector` build tag, so under
// the default tag set the entire embeddingService sat at 0%. This file is
// untagged and ships a self-contained EmbeddingManager test double so the
// service's generate/batch/provider/cache verbs are exercised in every build.
// =============================================================================

// svcEmbProvider is a minimal interfaces.EmbeddingProvider used by the manager
// double below.
type svcEmbProvider struct {
	dims   int
	maxTok int
	name   string
}

func (p *svcEmbProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2}, nil
}

func (p *svcEmbProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

func (p *svcEmbProvider) Dimensions() int { return p.dims }
func (p *svcEmbProvider) MaxTokens() int  { return p.maxTok }
func (p *svcEmbProvider) Name() string    { return p.name }
func (p *svcEmbProvider) Close() error    { return nil }

// svcEmbManager is a configurable interfaces.EmbeddingManager test double.
type svcEmbManager struct {
	embedErr      error
	embedBatchErr error
	dims          int
	providers     map[string]interfaces.EmbeddingProvider
	stats         interfaces.EmbeddingStats
}

func newSvcEmbManager() *svcEmbManager {
	return &svcEmbManager{
		dims:      2,
		providers: map[string]interfaces.EmbeddingProvider{"openai": &svcEmbProvider{dims: 2, maxTok: 8192, name: "openai"}},
		stats: interfaces.EmbeddingStats{
			ProvidersRegistered: 1,
			ProviderNames:       []string{"openai"},
			DefaultProvider:     "openai",
			CacheEnabled:        true,
		},
	}
}

func (m *svcEmbManager) Embed(_ context.Context, _ string) ([]float32, error) {
	if m.embedErr != nil {
		return nil, m.embedErr
	}
	return []float32{0.1, 0.2}, nil
}

func (m *svcEmbManager) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	if m.embedBatchErr != nil {
		return nil, m.embedBatchErr
	}
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

func (m *svcEmbManager) Dimensions() (int, error) { return m.dims, nil }

func (m *svcEmbManager) GetProvider(name string) (interfaces.EmbeddingProvider, error) {
	if p, ok := m.providers[name]; ok {
		return p, nil
	}
	return nil, embedding.ErrProviderNotFound
}

func (m *svcEmbManager) GetStats() interfaces.EmbeddingStats { return m.stats }

func newSvcEmbeddingService(m interfaces.EmbeddingManager) EmbeddingService {
	return NewEmbeddingService(m, nil)
}

func TestEmbeddingService_GenerateEmbedding(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "hello world"})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Embedding, 2)
		assert.Equal(t, 2, resp.Metadata.Dimensions)
		assert.Greater(t, resp.Metadata.Tokens, 0)
	})

	t.Run("nil request", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.GenerateEmbedding(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty text", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: ""})
		require.Error(t, err)
	})

	t.Run("manager error mapped", func(t *testing.T) {
		mgr := newSvcEmbManager()
		mgr.embedErr = embedding.ErrProviderUnavailable
		svc := newSvcEmbeddingService(mgr)
		_, err := svc.GenerateEmbedding(ctx, &GenerateEmbeddingRequest{Text: "x", Provider: EmbeddingProvider("openai")})
		require.Error(t, err)
	})
}

func TestEmbeddingService_BatchGenerateEmbeddings(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.BatchGenerateEmbeddings(ctx, &BatchGenerateEmbeddingsRequest{
			Items: []*BatchEmbeddingItem{
				{ID: "1", Text: "alpha"},
				{ID: "2", Text: "beta"},
			},
		})
		require.NoError(t, err)
		assert.Equal(t, 2, resp.SuccessCount)
		assert.Equal(t, 0, resp.ErrorCount)
		assert.Len(t, resp.Results, 2)
	})

	t.Run("nil request", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.BatchGenerateEmbeddings(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty items", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.BatchGenerateEmbeddings(ctx, &BatchGenerateEmbeddingsRequest{})
		require.Error(t, err)
	})

	t.Run("item missing id", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.BatchGenerateEmbeddings(ctx, &BatchGenerateEmbeddingsRequest{
			Items: []*BatchEmbeddingItem{{Text: "no id"}},
		})
		require.Error(t, err)
	})

	t.Run("manager error mapped", func(t *testing.T) {
		mgr := newSvcEmbManager()
		mgr.embedBatchErr = embedding.ErrTokenLimitExceeded
		svc := newSvcEmbeddingService(mgr)
		_, err := svc.BatchGenerateEmbeddings(ctx, &BatchGenerateEmbeddingsRequest{
			Items: []*BatchEmbeddingItem{{ID: "1", Text: "x"}},
		})
		require.Error(t, err)
	})
}

func TestEmbeddingService_Providers(t *testing.T) {
	ctx := context.Background()

	t.Run("ListProviders success", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.ListProviders(ctx, &ListProvidersRequest{})
		require.NoError(t, err)
		assert.Len(t, resp.Providers, 1)
		assert.Equal(t, "openai", resp.DefaultProvider)
	})

	t.Run("ListProviders nil request defaulted", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.ListProviders(ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, resp)
	})

	t.Run("ListProviders includes unavailable", func(t *testing.T) {
		mgr := newSvcEmbManager()
		// Advertise a provider name that GetProvider cannot resolve.
		mgr.stats.ProviderNames = []string{"openai", "ghost"}
		svc := newSvcEmbeddingService(mgr)

		resp, err := svc.ListProviders(ctx, &ListProvidersRequest{IncludeUnavailable: true})
		require.NoError(t, err)
		assert.Len(t, resp.Providers, 2)

		// Without IncludeUnavailable the ghost provider is skipped.
		resp2, err := svc.ListProviders(ctx, &ListProvidersRequest{})
		require.NoError(t, err)
		assert.Len(t, resp2.Providers, 1)
	})

	t.Run("GetProviderInfo success", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.GetProviderInfo(ctx, &GetProviderInfoRequest{Provider: EmbeddingProvider("openai")})
		require.NoError(t, err)
		assert.Equal(t, 2, resp.Info.Dimensions)
		assert.Equal(t, 8192, resp.Info.MaxTokens)
	})

	t.Run("GetProviderInfo nil request", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.GetProviderInfo(ctx, nil)
		require.Error(t, err)
	})

	t.Run("GetProviderInfo empty provider", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.GetProviderInfo(ctx, &GetProviderInfoRequest{})
		require.Error(t, err)
	})

	t.Run("GetProviderInfo not found", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.GetProviderInfo(ctx, &GetProviderInfoRequest{Provider: EmbeddingProvider("ghost")})
		require.Error(t, err)
	})

	t.Run("SetDefaultProvider success", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: EmbeddingProvider("openai")})
		require.NoError(t, err)
		assert.Equal(t, EmbeddingProvider("openai"), resp.Provider)
	})

	t.Run("SetDefaultProvider nil request", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.SetDefaultProvider(ctx, nil)
		require.Error(t, err)
	})

	t.Run("SetDefaultProvider empty provider", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{})
		require.Error(t, err)
	})

	t.Run("SetDefaultProvider not found", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.SetDefaultProvider(ctx, &SetDefaultProviderRequest{Provider: EmbeddingProvider("ghost")})
		require.Error(t, err)
	})
}

func TestEmbeddingService_Cache(t *testing.T) {
	ctx := context.Background()

	t.Run("GetCacheStats with activity", func(t *testing.T) {
		mgr := newSvcEmbManager()
		mgr.stats.CacheHits = 6
		mgr.stats.CacheMisses = 2
		mgr.stats.CacheSize = 4
		svc := newSvcEmbeddingService(mgr)
		resp, err := svc.GetCacheStats(ctx, &GetEmbeddingCacheStatsRequest{})
		require.NoError(t, err)
		assert.InDelta(t, 0.75, resp.HitRate, 0.0001)
		assert.Equal(t, 4, resp.Size)
	})

	t.Run("GetCacheStats no activity hit-rate zero", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.GetCacheStats(ctx, nil)
		require.NoError(t, err)
		assert.Equal(t, 0.0, resp.HitRate)
	})

	t.Run("ClearCache success", func(t *testing.T) {
		mgr := newSvcEmbManager()
		mgr.stats.CacheSize = 5
		svc := newSvcEmbeddingService(mgr)
		resp, err := svc.ClearCache(ctx, &ClearEmbeddingCacheRequest{All: true})
		require.NoError(t, err)
		assert.Equal(t, 5, resp.EntriesCleared)
	})

	t.Run("ClearCache nil request defaulted", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.ClearCache(ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, resp)
	})
}

func TestEmbeddingService_SetProviderStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("enable then disable", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		resp, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "openai", Enabled: false})
		require.NoError(t, err)
		assert.False(t, resp.Enabled)

		resp, err = svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "openai", Enabled: true})
		require.NoError(t, err)
		assert.True(t, resp.Enabled)
	})

	t.Run("nil request", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.SetProviderStatus(ctx, nil)
		require.Error(t, err)
	})

	t.Run("empty name", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{})
		require.Error(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		svc := newSvcEmbeddingService(newSvcEmbManager())
		_, err := svc.SetProviderStatus(ctx, &SetProviderStatusRequest{Name: "ghost", Enabled: true})
		require.Error(t, err)
	})
}

func TestEstimateTokens_Core(t *testing.T) {
	assert.Equal(t, 0, estimateTokens(""))
	assert.Equal(t, 1, estimateTokens("abcd"))
	assert.Equal(t, 2, estimateTokens("abcde"))
}
