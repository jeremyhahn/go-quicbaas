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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// EmbeddingManagerAdapter Tests
// =============================================================================

func TestNewEmbeddingManagerAdapter_NilManager(t *testing.T) {
	// Creating adapter with nil manager should not panic
	adapter := NewEmbeddingManagerAdapter(nil)
	require.NotNil(t, adapter)
	assert.Nil(t, adapter.manager)
}

func TestNewEmbeddingManagerAdapter_WithManager(t *testing.T) {
	// We can't test with a real embedding.Manager without setting up the whole
	// embedding infrastructure, but we can verify the adapter is created correctly.
	adapter := NewEmbeddingManagerAdapter(nil)
	require.NotNil(t, adapter)
}

func TestEmbeddingManagerAdapter_InterfaceCompliance(t *testing.T) {
	// Verify the adapter implements the EmbeddingManager interface
	var _ interfaces.EmbeddingManager = (*EmbeddingManagerAdapter)(nil)
}

// =============================================================================
// EmbeddingStats Type Tests
// =============================================================================

func TestEmbeddingStats_StructFieldsFull(t *testing.T) {
	stats := interfaces.EmbeddingStats{
		ProvidersRegistered: 3,
		ProviderNames:       []string{"openai", "ollama", "local"},
		CacheEnabled:        true,
		CacheSize:           500,
		CacheHits:           400,
		CacheMisses:         100,
		CacheHitRate:        0.8,
	}

	assert.Equal(t, 3, stats.ProvidersRegistered)
	assert.Equal(t, []string{"openai", "ollama", "local"}, stats.ProviderNames)
	assert.True(t, stats.CacheEnabled)
	assert.Equal(t, 500, stats.CacheSize)
	assert.Equal(t, uint64(400), stats.CacheHits)
	assert.Equal(t, uint64(100), stats.CacheMisses)
	assert.Equal(t, 0.8, stats.CacheHitRate)
}

func TestEmbeddingStats_ZeroValuesFull(t *testing.T) {
	stats := interfaces.EmbeddingStats{}

	assert.Equal(t, 0, stats.ProvidersRegistered)
	assert.Nil(t, stats.ProviderNames)
	assert.False(t, stats.CacheEnabled)
	assert.Equal(t, 0, stats.CacheSize)
	assert.Equal(t, uint64(0), stats.CacheHits)
	assert.Equal(t, uint64(0), stats.CacheMisses)
	assert.Equal(t, 0.0, stats.CacheHitRate)
}

// =============================================================================
// Embedding Service Types Tests
// =============================================================================

func TestGenerateEmbeddingRequest_FullFields(t *testing.T) {
	req := &GenerateEmbeddingRequest{
		Text:      "test embedding text",
		Provider:  EmbeddingProviderOpenAI,
		InputType: EmbeddingInputTypeDocument,
	}

	assert.Equal(t, "test embedding text", req.Text)
	assert.Equal(t, EmbeddingProviderOpenAI, req.Provider)
	assert.Equal(t, EmbeddingInputTypeDocument, req.InputType)
}

func TestGenerateEmbeddingRequest_EmptyFieldsFull(t *testing.T) {
	req := &GenerateEmbeddingRequest{}

	assert.Equal(t, "", req.Text)
	assert.Equal(t, EmbeddingProvider(""), req.Provider)
	assert.Equal(t, EmbeddingInputType(""), req.InputType)
}

func TestBatchEmbeddingItem_FullFields(t *testing.T) {
	item := &BatchEmbeddingItem{
		ID:   "item-1",
		Text: "batch item text",
	}

	assert.Equal(t, "item-1", item.ID)
	assert.Equal(t, "batch item text", item.Text)
}

func TestBatchGenerateEmbeddingsRequest_FullFields(t *testing.T) {
	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{
			{ID: "1", Text: "text 1"},
			{ID: "2", Text: "text 2"},
		},
		Provider: EmbeddingProviderOllama,
	}

	assert.Len(t, req.Items, 2)
	assert.Equal(t, EmbeddingProviderOllama, req.Provider)
	assert.Equal(t, "1", req.Items[0].ID)
	assert.Equal(t, "text 2", req.Items[1].Text)
}

func TestBatchGenerateEmbeddingsRequest_EmptyItemsFull(t *testing.T) {
	req := &BatchGenerateEmbeddingsRequest{
		Items: []*BatchEmbeddingItem{},
	}

	assert.Empty(t, req.Items)
}

func TestListProvidersRequest_FullFields(t *testing.T) {
	req := &ListProvidersRequest{
		IncludeUnavailable: true,
	}

	assert.True(t, req.IncludeUnavailable)
}

func TestListProvidersRequest_DefaultValuesFull(t *testing.T) {
	req := &ListProvidersRequest{}

	assert.False(t, req.IncludeUnavailable)
}

func TestGetProviderInfoRequest_FullFields(t *testing.T) {
	req := &GetProviderInfoRequest{
		Provider: "openai",
	}

	assert.Equal(t, EmbeddingProvider("openai"), req.Provider)
}

func TestSetDefaultProviderRequest_FullFields(t *testing.T) {
	req := &SetDefaultProviderRequest{
		Provider: "ollama",
	}

	assert.Equal(t, EmbeddingProvider("ollama"), req.Provider)
}

func TestGetEmbeddingCacheStatsRequest_EmptyStruct(t *testing.T) {
	req := &GetEmbeddingCacheStatsRequest{}
	// This struct is empty, just verify it exists
	assert.NotNil(t, req)
}

func TestClearEmbeddingCacheRequest_FullFields(t *testing.T) {
	req := &ClearEmbeddingCacheRequest{
		Provider: "openai",
		All:      true,
	}

	assert.Equal(t, EmbeddingProvider("openai"), req.Provider)
	assert.True(t, req.All)
}

func TestClearEmbeddingCacheRequest_ClearSpecificProviderFull(t *testing.T) {
	req := &ClearEmbeddingCacheRequest{
		Provider: "openai",
		All:      false,
	}

	assert.Equal(t, EmbeddingProvider("openai"), req.Provider)
	assert.False(t, req.All)
}

// =============================================================================
// Response Types Tests
// =============================================================================

func TestGenerateEmbeddingResponse_FullFields(t *testing.T) {
	embedding := []float32{0.1, 0.2, 0.3}
	metadata := &EmbeddingMetadata{
		Dimensions: 3,
		Tokens:     10,
		Model:      "test-model",
		CacheHit:   true,
	}

	resp := &GenerateEmbeddingResponse{
		Embedding: embedding,
		Metadata:  metadata,
	}

	assert.Equal(t, embedding, resp.Embedding)
	assert.Equal(t, 3, resp.Metadata.Dimensions)
	assert.Equal(t, 10, resp.Metadata.Tokens)
	assert.Equal(t, "test-model", resp.Metadata.Model)
	assert.True(t, resp.Metadata.CacheHit)
}

func TestBatchEmbeddingResult_FullFields(t *testing.T) {
	result := &BatchEmbeddingResult{
		ID:        "result-1",
		Embedding: []float32{0.1, 0.2},
		Error:     "",
	}

	assert.Equal(t, "result-1", result.ID)
	assert.Equal(t, []float32{0.1, 0.2}, result.Embedding)
	assert.Empty(t, result.Error)
}

func TestBatchEmbeddingResult_WithErrorFull(t *testing.T) {
	result := &BatchEmbeddingResult{
		ID:        "result-error",
		Embedding: nil,
		Error:     "processing failed",
	}

	assert.Equal(t, "result-error", result.ID)
	assert.Nil(t, result.Embedding)
	assert.Equal(t, "processing failed", result.Error)
}

func TestBatchGenerateEmbeddingsResponse_FullFields(t *testing.T) {
	resp := &BatchGenerateEmbeddingsResponse{
		Results: []*BatchEmbeddingResult{
			{ID: "1", Embedding: []float32{0.1}},
			{ID: "2", Embedding: []float32{0.2}},
		},
		SuccessCount:   2,
		ErrorCount:     0,
		TotalTokens:    10,
		ProcessingTime: time.Second,
	}

	assert.Len(t, resp.Results, 2)
	assert.Equal(t, 2, resp.SuccessCount)
	assert.Equal(t, 0, resp.ErrorCount)
	assert.Equal(t, 10, resp.TotalTokens)
	assert.Equal(t, time.Second, resp.ProcessingTime)
}

func TestProviderInfo_FullFields(t *testing.T) {
	info := &ProviderInfo{
		Name:       "openai",
		Dimensions: 1536,
		MaxTokens:  8192,
		Status:     EmbeddingStatusAvailable,
	}

	assert.Equal(t, "openai", info.Name)
	assert.Equal(t, 1536, info.Dimensions)
	assert.Equal(t, 8192, info.MaxTokens)
	assert.Equal(t, EmbeddingStatusAvailable, info.Status)
}

func TestListProvidersResponse_FullFields(t *testing.T) {
	resp := &ListProvidersResponse{
		Providers: []*ProviderInfo{
			{Name: "openai", Status: EmbeddingStatusAvailable},
			{Name: "ollama", Status: EmbeddingStatusUnavailable},
		},
	}

	assert.Len(t, resp.Providers, 2)
	assert.Equal(t, "openai", resp.Providers[0].Name)
	assert.Equal(t, "ollama", resp.Providers[1].Name)
}

func TestGetProviderInfoResponse_FullFields(t *testing.T) {
	resp := &GetProviderInfoResponse{
		Info: &ProviderInfo{
			Name:       "local",
			Dimensions: 384,
		},
	}

	assert.NotNil(t, resp.Info)
	assert.Equal(t, "local", resp.Info.Name)
}

func TestSetDefaultProviderResponse_FullFields(t *testing.T) {
	resp := &SetDefaultProviderResponse{
		Provider: "new-default",
	}

	assert.Equal(t, EmbeddingProvider("new-default"), resp.Provider)
}

func TestGetEmbeddingCacheStatsResponse_FullFields(t *testing.T) {
	resp := &GetEmbeddingCacheStatsResponse{
		Enabled:            true,
		Size:               1000,
		Capacity:           5000,
		Hits:               800,
		Misses:             200,
		HitRate:            0.8,
		Evictions:          50,
		CompressionEnabled: true,
	}

	assert.True(t, resp.Enabled)
	assert.Equal(t, 1000, resp.Size)
	assert.Equal(t, 5000, resp.Capacity)
	assert.Equal(t, uint64(800), resp.Hits)
	assert.Equal(t, uint64(200), resp.Misses)
	assert.Equal(t, 0.8, resp.HitRate)
	assert.Equal(t, uint64(50), resp.Evictions)
	assert.True(t, resp.CompressionEnabled)
}

func TestClearEmbeddingCacheResponse_FullFields(t *testing.T) {
	resp := &ClearEmbeddingCacheResponse{
		EntriesCleared: 100,
		Provider:       "openai",
	}

	assert.Equal(t, 100, resp.EntriesCleared)
	assert.Equal(t, EmbeddingProvider("openai"), resp.Provider)
}

// =============================================================================
// Embedding Constants Tests
// =============================================================================

func TestEmbeddingProviderConstantsFull(t *testing.T) {
	// Verify provider constants are defined
	assert.Equal(t, EmbeddingProvider("openai"), EmbeddingProviderOpenAI)
	assert.Equal(t, EmbeddingProvider("ollama"), EmbeddingProviderOllama)
}

func TestEmbeddingStatusConstantsFull(t *testing.T) {
	// Verify status constants are defined
	assert.Equal(t, EmbeddingStatus("available"), EmbeddingStatusAvailable)
	assert.Equal(t, EmbeddingStatus("unavailable"), EmbeddingStatusUnavailable)
}

func TestEmbeddingInputTypeConstantsFull(t *testing.T) {
	// Verify input type constants are defined
	assert.Equal(t, EmbeddingInputType("document"), EmbeddingInputTypeDocument)
	assert.Equal(t, EmbeddingInputType("query"), EmbeddingInputTypeQuery)
	assert.Equal(t, EmbeddingInputType("code"), EmbeddingInputTypeCode)
}

// =============================================================================
// EmbeddingMetadata Full Struct Tests
// =============================================================================

func TestEmbeddingMetadata_AllFields(t *testing.T) {
	metadata := &EmbeddingMetadata{
		Model:      "text-embedding-3-small",
		Provider:   EmbeddingProviderOpenAI,
		Dimensions: 1536,
		Tokens:     100,
		Source:     "document.txt",
		Language:   "en",
		InputType:  EmbeddingInputTypeDocument,
		CacheHit:   false,
	}

	assert.Equal(t, "text-embedding-3-small", metadata.Model)
	assert.Equal(t, EmbeddingProviderOpenAI, metadata.Provider)
	assert.Equal(t, 1536, metadata.Dimensions)
	assert.Equal(t, 100, metadata.Tokens)
	assert.Equal(t, "document.txt", metadata.Source)
	assert.Equal(t, "en", metadata.Language)
	assert.Equal(t, EmbeddingInputTypeDocument, metadata.InputType)
	assert.False(t, metadata.CacheHit)
}

func TestEmbeddingMetadata_MinimalFields(t *testing.T) {
	metadata := &EmbeddingMetadata{
		Dimensions: 384,
	}

	assert.Equal(t, 384, metadata.Dimensions)
	assert.Empty(t, metadata.Model)
	assert.Empty(t, metadata.Provider)
}

func TestEmbeddingMetadata_WithCacheHit(t *testing.T) {
	metadata := &EmbeddingMetadata{
		Dimensions: 768,
		CacheHit:   true,
	}

	assert.True(t, metadata.CacheHit)
	assert.Equal(t, 768, metadata.Dimensions)
}
