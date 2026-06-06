//go:build vector

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

// This file exercises the embeddingService facade against a mocked
// interfaces.EmbeddingManager. Unlike embedding_service_test.go (which needs
// the real embedding.Manager and therefore the qrdb_vector build tag), these
// tests run under the plain `vector` tag because they never touch
// embedding.NewManager — the service layer only depends on the
// interfaces.EmbeddingManager abstraction, which the package's
// mockEmbeddingManager already satisfies.

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	quicbaasErrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// stubEmbeddingProvider is a minimal interfaces.EmbeddingProvider used to
// populate the mock manager's provider registry.
type stubEmbeddingProvider struct {
	name       string
	dimensions int
	maxTokens  int
}

func (p *stubEmbeddingProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	return []float32{0.1, 0.2}, nil
}

func (p *stubEmbeddingProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0.1, 0.2}
	}
	return out, nil
}

func (p *stubEmbeddingProvider) Dimensions() int { return p.dimensions }
func (p *stubEmbeddingProvider) MaxTokens() int  { return p.maxTokens }
func (p *stubEmbeddingProvider) Name() string    { return p.name }
func (p *stubEmbeddingProvider) Close() error    { return nil }

var _ interfaces.EmbeddingProvider = (*stubEmbeddingProvider)(nil)

func newMockServiceWithManager(mgr interfaces.EmbeddingManager) EmbeddingService {
	return NewEmbeddingService(mgr, nil)
}

// =============================================================================
// BatchGenerateEmbeddings
// =============================================================================

func TestEmbeddingServiceMock_BatchGenerateEmbeddings_Success(t *testing.T) {
	mgr := newMockEmbeddingManager()
	svc := newMockServiceWithManager(mgr)

	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "1", Text: "alpha"},
			{ID: "2", Text: "beta gamma delta"},
		},
		Provider: "openai",
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.SuccessCount)
	assert.Equal(t, 0, resp.ErrorCount)
	assert.Greater(t, resp.TotalTokens, 0)
	require.Len(t, resp.Results, 2)
	assert.Equal(t, "1", resp.Results[0].ID)
	assert.NotEmpty(t, resp.Results[0].Embedding)
}

func TestEmbeddingServiceMock_BatchGenerateEmbeddings_ValidationErrors(t *testing.T) {
	svc := newMockServiceWithManager(newMockEmbeddingManager())

	tests := []struct {
		name string
		req  *BatchGenerateEmbeddingsRequest
	}{
		{name: "empty items", req: &BatchGenerateEmbeddingsRequest{Items: nil}},
		{
			name: "item missing text",
			req:  &BatchGenerateEmbeddingsRequest{Items: []*BatchEmbeddingItem{{ID: "1", Text: ""}}},
		},
		{
			name: "item missing id",
			req:  &BatchGenerateEmbeddingsRequest{Items: []*BatchEmbeddingItem{{ID: "", Text: "x"}}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := svc.BatchGenerateEmbeddings(context.Background(), tc.req)
			require.Error(t, err)
			assert.Nil(t, resp)
		})
	}
}

// TestEmbeddingServiceMock_BatchGenerateEmbeddings_NilRequest verifies a nil
// request returns a validation error instead of panicking (the method guards
// nil before dereferencing req.Provider).
func TestEmbeddingServiceMock_BatchGenerateEmbeddings_NilRequest(t *testing.T) {
	svc := newMockServiceWithManager(newMockEmbeddingManager())
	resp, err := svc.BatchGenerateEmbeddings(context.Background(), nil)
	assert.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingServiceMock_BatchGenerateEmbeddings_ManagerErrorMapped(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.embedBatchFunc = func(_ context.Context, _ []string) ([][]float32, error) {
		return nil, embedding.ErrRateLimitExceeded
	}
	svc := newMockServiceWithManager(mgr)

	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{{ID: "1", Text: "alpha"}},
	}

	resp, err := svc.BatchGenerateEmbeddings(context.Background(), req)
	require.Error(t, err)
	assert.Nil(t, resp)

	// handleEmbeddingError must map the sentinel onto the rate-limited code.
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrRateLimited, qbErr.Code)
}

func TestEmbeddingServiceMock_BatchGenerateEmbeddings_TokenLimitMapped(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.embedBatchFunc = func(_ context.Context, _ []string) ([][]float32, error) {
		return nil, embedding.ErrTokenLimitExceeded
	}
	svc := newMockServiceWithManager(mgr)

	_, err := svc.BatchGenerateEmbeddings(context.Background(), &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{{ID: "1", Text: "alpha"}},
	})
	require.Error(t, err)
	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrResourceExhausted, qbErr.Code)
}

// =============================================================================
// ListProviders
// =============================================================================

func TestEmbeddingServiceMock_ListProviders_Success(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.registeredProviders["openai"] = &stubEmbeddingProvider{name: "openai", dimensions: 1536, maxTokens: 8192}
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{
			ProviderNames:   []string{"openai"},
			DefaultProvider: "openai",
		}
	}
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.ListProviders(context.Background(), &ListProvidersRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Len(t, resp.Providers, 1)
	assert.Equal(t, "openai", resp.Providers[0].Name)
	assert.Equal(t, 1536, resp.Providers[0].Dimensions)
	assert.Equal(t, "openai", resp.DefaultProvider)
}

func TestEmbeddingServiceMock_ListProviders_NilRequest(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{ProviderNames: nil}
	}
	svc := newMockServiceWithManager(mgr)

	// A nil request must be defaulted rather than panicking.
	resp, err := svc.ListProviders(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Empty(t, resp.Providers)
}

func TestEmbeddingServiceMock_ListProviders_UnavailableSkippedAndIncluded(t *testing.T) {
	mgr := newMockEmbeddingManager()
	// "ghost" is named in stats but not registered: GetProvider returns an error.
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{ProviderNames: []string{"ghost"}}
	}

	// Without IncludeUnavailable the missing provider is skipped.
	svc := newMockServiceWithManager(mgr)
	resp, err := svc.ListProviders(context.Background(), &ListProvidersRequest{IncludeUnavailable: false})
	require.NoError(t, err)
	assert.Empty(t, resp.Providers)

	// With IncludeUnavailable it is reported with an unavailable status.
	resp, err = svc.ListProviders(context.Background(), &ListProvidersRequest{IncludeUnavailable: true})
	require.NoError(t, err)
	require.Len(t, resp.Providers, 1)
	assert.Equal(t, EmbeddingStatusUnavailable, resp.Providers[0].Status)
}

// =============================================================================
// GetProviderInfo
// =============================================================================

func TestEmbeddingServiceMock_GetProviderInfo_Success(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.registeredProviders["openai"] = &stubEmbeddingProvider{name: "openai", dimensions: 768, maxTokens: 512}
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.GetProviderInfo(context.Background(), &GetProviderInfoRequest{Provider: "openai"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 768, resp.Info.Dimensions)
	assert.Equal(t, 512, resp.Info.MaxTokens)
}

func TestEmbeddingServiceMock_GetProviderInfo_ValidationError(t *testing.T) {
	svc := newMockServiceWithManager(newMockEmbeddingManager())

	resp, err := svc.GetProviderInfo(context.Background(), &GetProviderInfoRequest{Provider: ""})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingServiceMock_GetProviderInfo_NotFound(t *testing.T) {
	svc := newMockServiceWithManager(newMockEmbeddingManager())

	resp, err := svc.GetProviderInfo(context.Background(), &GetProviderInfoRequest{Provider: "missing"})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// SetDefaultProvider
// =============================================================================

func TestEmbeddingServiceMock_SetDefaultProvider_Success(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.registeredProviders["openai"] = &stubEmbeddingProvider{name: "openai"}
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.SetDefaultProvider(context.Background(), &SetDefaultProviderRequest{Provider: "openai"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, EmbeddingProvider("openai"), resp.Provider)
}

func TestEmbeddingServiceMock_SetDefaultProvider_ValidationError(t *testing.T) {
	svc := newMockServiceWithManager(newMockEmbeddingManager())

	resp, err := svc.SetDefaultProvider(context.Background(), &SetDefaultProviderRequest{Provider: ""})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingServiceMock_SetDefaultProvider_NotFound(t *testing.T) {
	svc := newMockServiceWithManager(newMockEmbeddingManager())

	resp, err := svc.SetDefaultProvider(context.Background(), &SetDefaultProviderRequest{Provider: "missing"})
	require.Error(t, err)
	assert.Nil(t, resp)
}

// =============================================================================
// GetCacheStats
// =============================================================================

func TestEmbeddingServiceMock_GetCacheStats_HitRate(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{
			CacheEnabled: true,
			CacheSize:    10,
			CacheHits:    3,
			CacheMisses:  1,
		}
	}
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.GetCacheStats(context.Background(), &GetEmbeddingCacheStatsRequest{})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Enabled)
	assert.Equal(t, 10, resp.Size)
	assert.InDelta(t, 0.75, resp.HitRate, 0.0001)
}

func TestEmbeddingServiceMock_GetCacheStats_NoActivityHitRateZero(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{CacheEnabled: false}
	}
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.GetCacheStats(context.Background(), &GetEmbeddingCacheStatsRequest{})
	require.NoError(t, err)
	// With zero hits and misses the hit rate must not divide by zero.
	assert.Equal(t, 0.0, resp.HitRate)
}

// =============================================================================
// ClearCache
// =============================================================================

func TestEmbeddingServiceMock_ClearCache_Success(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{CacheSize: 5}
	}
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.ClearCache(context.Background(), &ClearEmbeddingCacheRequest{All: true})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 5, resp.EntriesCleared)
}

func TestEmbeddingServiceMock_ClearCache_NilRequestDefaulted(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{CacheSize: 2}
	}
	svc := newMockServiceWithManager(mgr)

	// A nil request must default to All=true rather than panicking.
	resp, err := svc.ClearCache(context.Background(), nil)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 2, resp.EntriesCleared)
}

// =============================================================================
// SetProviderStatus
// =============================================================================

func TestEmbeddingServiceMock_SetProviderStatus_EnableDisable(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.registeredProviders["openai"] = &stubEmbeddingProvider{name: "openai"}
	svc := newMockServiceWithManager(mgr)

	// Disable then re-enable to cover both store/delete branches.
	resp, err := svc.SetProviderStatus(context.Background(), &SetProviderStatusRequest{Name: "openai", Enabled: false})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.False(t, resp.Enabled)

	resp, err = svc.SetProviderStatus(context.Background(), &SetProviderStatusRequest{Name: "openai", Enabled: true})
	require.NoError(t, err)
	assert.True(t, resp.Enabled)
}

func TestEmbeddingServiceMock_SetProviderStatus_Errors(t *testing.T) {
	svc := newMockServiceWithManager(newMockEmbeddingManager())

	// Nil request.
	_, err := svc.SetProviderStatus(context.Background(), nil)
	require.Error(t, err)

	// Missing name.
	_, err = svc.SetProviderStatus(context.Background(), &SetProviderStatusRequest{Name: ""})
	require.Error(t, err)

	// Unknown provider.
	_, err = svc.SetProviderStatus(context.Background(), &SetProviderStatusRequest{Name: "missing", Enabled: true})
	require.Error(t, err)
}

// =============================================================================
// GenerateEmbedding
// =============================================================================

func TestEmbeddingServiceMock_GenerateEmbedding_Success(t *testing.T) {
	mgr := newMockEmbeddingManager()
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{
		Text:     "hello world",
		Provider: "openai",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.Embedding)
	require.NotNil(t, resp.Metadata)
	assert.Greater(t, resp.Metadata.Tokens, 0)
	assert.False(t, resp.Metadata.CacheHit)
}

func TestEmbeddingServiceMock_GenerateEmbedding_CacheHitDetected(t *testing.T) {
	mgr := newMockEmbeddingManager()
	// Report a higher CacheHits count on the second GetStats call so the
	// service classifies the result as a cache hit.
	calls := 0
	mgr.getStatsFunc = func() interfaces.EmbeddingStats {
		calls++
		return interfaces.EmbeddingStats{CacheHits: uint64(calls)}
	}
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{Text: "cached"})
	require.NoError(t, err)
	require.NotNil(t, resp.Metadata)
	assert.True(t, resp.Metadata.CacheHit)
}

func TestEmbeddingServiceMock_GenerateEmbedding_ValidationError(t *testing.T) {
	svc := newMockServiceWithManager(newMockEmbeddingManager())

	resp, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{Text: ""})
	require.Error(t, err)
	assert.Nil(t, resp)
}

func TestEmbeddingServiceMock_GenerateEmbedding_ManagerErrorMapped(t *testing.T) {
	mgr := newMockEmbeddingManager()
	mgr.embedFunc = func(_ context.Context, _ string) ([]float32, error) {
		return nil, embedding.ErrProviderUnavailable
	}
	svc := newMockServiceWithManager(mgr)

	resp, err := svc.GenerateEmbedding(context.Background(), &GenerateEmbeddingRequest{Text: "x"})
	require.Error(t, err)
	assert.Nil(t, resp)

	var qbErr *quicbaasErrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, quicbaasErrors.ErrUnavailable, qbErr.Code)
}

// =============================================================================
// EmbeddingManagerAdapter tenant-context guard
//
// Embed and EmbedBatch enforce the multi-tenant context guard before touching
// the wrapped manager, so the rejection path is exercisable with a bare
// adapter (no real embedding.Manager required).
// =============================================================================

func TestEmbeddingManagerAdapter_Embed_RejectsInvalidTenant(t *testing.T) {
	adapter := &EmbeddingManagerAdapter{}
	// A tenant ID longer than 128 characters fails validateEmbeddingTenantID
	// before the manager is dereferenced.
	ctx := iam.ContextWithTenant(context.Background(), strings.Repeat("a", 129))

	vec, err := adapter.Embed(ctx, "text")
	require.Error(t, err)
	assert.Nil(t, vec)
}

func TestEmbeddingManagerAdapter_EmbedBatch_RejectsInvalidTenant(t *testing.T) {
	adapter := &EmbeddingManagerAdapter{}
	ctx := iam.ContextWithTenant(context.Background(), strings.Repeat("a", 129))

	vecs, err := adapter.EmbedBatch(ctx, []string{"a", "b"})
	require.Error(t, err)
	assert.Nil(t, vecs)
}
