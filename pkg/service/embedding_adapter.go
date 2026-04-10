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

	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// EmbeddingManagerAdapter wraps embedding.Manager to implement interfaces.EmbeddingManager.
// This adapter converts between embedding package types and interface package types
// to enable dependency injection without circular imports.
type EmbeddingManagerAdapter struct {
	manager *embedding.Manager
}

// NewEmbeddingManagerAdapter creates a new adapter wrapping the given Manager.
func NewEmbeddingManagerAdapter(manager *embedding.Manager) *EmbeddingManagerAdapter {
	return &EmbeddingManagerAdapter{manager: manager}
}

// Embed generates an embedding for a single text using the configured provider.
func (a *EmbeddingManagerAdapter) Embed(ctx context.Context, text string) ([]float32, error) {
	if _, err := prepareEmbeddingTenantContext(ctx); err != nil {
		return nil, err
	}
	return a.manager.Embed(ctx, text)
}

// EmbedBatch generates embeddings for multiple texts.
func (a *EmbeddingManagerAdapter) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	if _, err := prepareEmbeddingTenantContext(ctx); err != nil {
		return nil, err
	}
	return a.manager.EmbedBatch(ctx, texts)
}

// Dimensions returns the dimensions of embeddings from the current provider.
func (a *EmbeddingManagerAdapter) Dimensions() (int, error) {
	return a.manager.Dimensions()
}

// GetProvider returns a provider by name, wrapped as interfaces.EmbeddingProvider.
func (a *EmbeddingManagerAdapter) GetProvider(name string) (interfaces.EmbeddingProvider, error) {
	provider, err := a.manager.GetProvider(name)
	if err != nil {
		return nil, err
	}
	// embedding.Provider implements the same interface as interfaces.EmbeddingProvider
	return provider, nil
}

// GetStats returns statistics about the embedding manager.
func (a *EmbeddingManagerAdapter) GetStats() interfaces.EmbeddingStats {
	stats := a.manager.GetStats()
	return interfaces.EmbeddingStats{
		ProvidersRegistered: stats.ProvidersRegistered,
		ProviderNames:       stats.ProviderNames,
		DefaultProvider:     stats.DefaultProvider,
		CacheEnabled:        stats.CacheEnabled,
		CacheSize:           stats.CacheSize,
		CacheHits:           stats.CacheHits,
		CacheMisses:         stats.CacheMisses,
		CacheHitRate:        stats.CacheHitRate,
	}
}

// Unwrap returns the underlying Manager for cases where direct access is needed.
func (a *EmbeddingManagerAdapter) Unwrap() *embedding.Manager {
	return a.manager
}

// Compile-time check that EmbeddingManagerAdapter implements interfaces.EmbeddingManager.
var _ interfaces.EmbeddingManager = (*EmbeddingManagerAdapter)(nil)
