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

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// mockEmbeddingManager implements interfaces.EmbeddingManager for testing.
type mockEmbeddingManager struct {
	embedFunc           func(ctx context.Context, text string) ([]float32, error)
	embedBatchFunc      func(ctx context.Context, texts []string) ([][]float32, error)
	dimensionsFunc      func() (int, error)
	getProviderFunc     func(name string) (interfaces.EmbeddingProvider, error)
	getStatsFunc        func() interfaces.EmbeddingStats
	closeCalled         bool
	registeredProviders map[string]interfaces.EmbeddingProvider
}

func newMockEmbeddingManager() *mockEmbeddingManager {
	return &mockEmbeddingManager{
		registeredProviders: make(map[string]interfaces.EmbeddingProvider),
	}
}

func (m *mockEmbeddingManager) Embed(ctx context.Context, text string) ([]float32, error) {
	if m.embedFunc != nil {
		return m.embedFunc(ctx, text)
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

func (m *mockEmbeddingManager) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if m.embedBatchFunc != nil {
		return m.embedBatchFunc(ctx, texts)
	}
	results := make([][]float32, len(texts))
	for i := range texts {
		results[i] = []float32{0.1, 0.2, 0.3}
	}
	return results, nil
}

func (m *mockEmbeddingManager) Dimensions() (int, error) {
	if m.dimensionsFunc != nil {
		return m.dimensionsFunc()
	}
	return 1536, nil
}

func (m *mockEmbeddingManager) GetProvider(name string) (interfaces.EmbeddingProvider, error) {
	if m.getProviderFunc != nil {
		return m.getProviderFunc(name)
	}
	if provider, ok := m.registeredProviders[name]; ok {
		return provider, nil
	}
	return nil, embedding.ErrProviderNotFound
}

func (m *mockEmbeddingManager) GetStats() interfaces.EmbeddingStats {
	if m.getStatsFunc != nil {
		return m.getStatsFunc()
	}
	return interfaces.EmbeddingStats{
		ProvidersRegistered: len(m.registeredProviders),
		ProviderNames:       []string{"openai"},
		CacheEnabled:        true,
		CacheSize:           0,
		CacheHits:           0,
		CacheMisses:         0,
		CacheHitRate:        0.0,
	}
}

// =============================================================================
// ServiceBuilder WithEmbeddingManager Tests
// =============================================================================

func TestServiceBuilder_WithEmbeddingManager_SetsManager(t *testing.T) {
	mockMgr := newMockEmbeddingManager()
	builder := NewServiceBuilder().WithEmbeddingManager(mockMgr)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.Equal(t, mockMgr, sb.embeddingManager)
}

func TestServiceBuilder_WithEmbeddingManager_ReturnsBuilder(t *testing.T) {
	mockMgr := newMockEmbeddingManager()
	builder := NewServiceBuilder()
	result := builder.WithEmbeddingManager(mockMgr)

	assert.Same(t, builder, result, "WithEmbeddingManager should return the same builder for chaining")
}

func TestServiceBuilder_WithEmbeddingManager_NilManager(t *testing.T) {
	builder := NewServiceBuilder().WithEmbeddingManager(nil)

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.Nil(t, sb.embeddingManager)
}

func TestServiceBuilder_WithEmbeddingManager_ChainedWithOtherMethods(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockMgr := newMockEmbeddingManager()

	builder := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithEmbeddingManager(mockMgr).
		WithConfig(&ServiceConfig{
			NodeID:  1,
			Version: "1.0.0",
		})

	sb, ok := builder.(*serviceBuilder)
	require.True(t, ok)
	assert.Equal(t, mockRaft, sb.raftNode)
	assert.Equal(t, mockMgr, sb.embeddingManager)
	assert.NotNil(t, sb.config)
}

// =============================================================================
// QuicBaaS Embedding Service Initialization Tests
// =============================================================================

func TestQuicBaaS_Embedding_ReturnsNilWithoutManager(t *testing.T) {
	mockRaft := &mockRaftNode{}
	svc := createTestServiceForEmbedding(t, mockRaft, nil)
	defer svc.Close()

	embeddingSvc := svc.Embedding()
	assert.Nil(t, embeddingSvc, "Embedding() should return nil when no manager is provided")
}

func TestQuicBaaS_Embedding_ReturnsServiceWithManager(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockMgr := newMockEmbeddingManager()

	svc := createTestServiceForEmbedding(t, mockRaft, mockMgr)
	defer svc.Close()

	embeddingSvc := svc.Embedding()
	assert.NotNil(t, embeddingSvc, "Embedding() should return a service when manager is provided")
}

func TestQuicBaaS_Embedding_ServiceIsUsable(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockMgr := newMockEmbeddingManager()
	mockMgr.embedFunc = func(ctx context.Context, text string) ([]float32, error) {
		return []float32{0.5, 0.6, 0.7}, nil
	}
	mockMgr.getStatsFunc = func() interfaces.EmbeddingStats {
		return interfaces.EmbeddingStats{
			ProvidersRegistered: 1,
			ProviderNames:       []string{"openai"},
			CacheEnabled:        true,
			CacheSize:           0,
			CacheHits:           0,
			CacheMisses:         0,
			CacheHitRate:        0.0,
		}
	}

	svc := createTestServiceForEmbedding(t, mockRaft, mockMgr)
	defer svc.Close()

	embeddingSvc := svc.Embedding()
	require.NotNil(t, embeddingSvc)

	// Test that the service works
	req := &GenerateEmbeddingRequest{
		Text: "Test embedding",
	}

	resp, err := embeddingSvc.GenerateEmbedding(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []float32{0.5, 0.6, 0.7}, resp.Embedding)
}

func TestQuicBaaS_Build_WithEmbeddingManager_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}
	mockMgr := newMockEmbeddingManager()

	builder := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithEmbeddingManager(mockMgr).
		WithConfig(&ServiceConfig{
			NodeID:        1,
			Version:       "1.0.0",
			EnableAuth:    false,
			EnableMetrics: false,
		})

	svc, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	assert.NotNil(t, svc.Embedding())
}

func TestQuicBaaS_Build_WithoutEmbeddingManager_Success(t *testing.T) {
	mockRaft := &mockRaftNode{}

	builder := NewServiceBuilder().
		WithRaftNode(mockRaft).
		WithConfig(&ServiceConfig{
			NodeID:        1,
			Version:       "1.0.0",
			EnableAuth:    false,
			EnableMetrics: false,
		})

	svc, err := builder.Build()
	require.NoError(t, err)
	require.NotNil(t, svc)
	defer svc.Close()

	assert.Nil(t, svc.Embedding())
}

// =============================================================================
// Test Helpers
// =============================================================================

// createTestServiceForEmbedding creates a test service with the given embedding manager.
func createTestServiceForEmbedding(t *testing.T, raftNode interfaces.RaftNode, embeddingMgr interfaces.EmbeddingManager) QuicBaaS {
	builder := &serviceBuilder{
		config: &ServiceConfig{
			NodeID:                1,
			Version:               "1.0.0",
			EnableAuth:            false,
			EnableMetrics:         false,
			EnableTracing:         false,
			RequestTimeout:        5 * time.Second,
			MaxConcurrentRequests: 100,
		},
		raftNode:         raftNode,
		embeddingManager: embeddingMgr,
	}

	svc, err := newQuicBaaS(builder)
	require.NoError(t, err)
	require.NotNil(t, svc)

	return svc
}
