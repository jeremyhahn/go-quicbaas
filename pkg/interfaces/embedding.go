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

package interfaces

import (
	"context"
)

// EmbeddingStats holds statistics about the embedding manager.
// This mirrors embedding.Stats to avoid circular dependencies between packages.
type EmbeddingStats struct {
	ProvidersRegistered int
	ProviderNames       []string
	DefaultProvider     string
	CacheEnabled        bool
	CacheSize           int
	CacheHits           uint64
	CacheMisses         uint64
	CacheHitRate        float64
}

// EmbeddingProvider defines the interface for embedding providers.
// This mirrors embedding.Provider to avoid circular dependencies.
type EmbeddingProvider interface {
	// Embed generates an embedding for a single text.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts in a single request.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensionality of embeddings produced by this provider.
	Dimensions() int

	// MaxTokens returns the maximum number of tokens this provider can handle.
	MaxTokens() int

	// Name returns the name of this provider.
	Name() string

	// Close closes the provider and releases resources.
	Close() error
}

// EmbeddingManager defines the interface for embedding management operations.
// This interface abstracts the concrete embedding.Manager type to enable
// dependency injection and testing in the service layer.
//
// The interface is designed to avoid circular imports by using EmbeddingStats
// (defined in this package) instead of embedding.Stats. Implementations should
// convert between the two types as needed.
type EmbeddingManager interface {
	// Embed generates an embedding for a single text using the configured provider.
	// It checks cache first and stores results in cache if enabled.
	Embed(ctx context.Context, text string) ([]float32, error)

	// EmbedBatch generates embeddings for multiple texts.
	// It leverages caching and provider batch APIs for efficiency.
	EmbedBatch(ctx context.Context, texts []string) ([][]float32, error)

	// Dimensions returns the dimensions of embeddings from the current provider.
	Dimensions() (int, error)

	// GetProvider returns a provider by name.
	// The returned provider implements EmbeddingProvider.
	GetProvider(name string) (EmbeddingProvider, error)

	// GetStats returns statistics about the embedding manager.
	GetStats() EmbeddingStats
}
