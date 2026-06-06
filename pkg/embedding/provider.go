//go:build vector && qrdb_vector

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

package embedding

import (
	"context"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// OpenAI model name constants.
const (
	DefaultOpenAIURL         = "https://api.openai.com"
	DefaultOpenAITimeout     = 30 * time.Second
	ModelTextEmbedding3Small = "text-embedding-3-small"
	ModelTextEmbedding3Large = "text-embedding-3-large"
	ModelTextEmbeddingAda002 = "text-embedding-ada-002"
)

// Voyage API constants.
const (
	VoyageAPIURL = "https://api.voyageai.com/v1/embeddings"
)

// Provider defines the embedding provider interface used throughout go-quicbaas.
// It accepts a single text per Embed call (convenience wrapper) and provides
// batch support via EmbedBatch.
//
// The underlying implementations delegate to go-qrdb's batch-first Provider,
// which uses Embed(ctx, []string, model) *EmbeddingResult.
type Provider interface {
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

// Metadata contains information about the embedding request.
type Metadata struct {
	Model      string
	Dimensions int
	Tokens     int
	Source     string
	Language   string
	Tags       map[string]string
}

// Result represents an embedding result with metadata.
type Result struct {
	Embedding []float32
	Metadata  Metadata
}

// BatchResult represents a batch embedding result.
type BatchResult struct {
	Embeddings [][]float32
	Metadata   []Metadata
}

// Config holds the full configuration for the embedding subsystem.
// It is identical to qrdbsdk.EmbeddingConfig, re-exported here so callers within
// go-quicbaas do not need to import go-qrdb directly.
type Config = qrdbsdk.EmbeddingConfig

// DefaultConfig returns the default Config.
func DefaultConfig() Config {
	return qrdbsdk.DefaultEmbeddingConfig()
}

// Stats holds statistics about the embedding manager.
// It mirrors qrdbsdk.EmbeddingManagerStats with the same field names.
type Stats struct {
	ProvidersRegistered int
	ProviderNames       []string
	DefaultProvider     string
	CacheEnabled        bool
	CacheSize           int
	CacheHits           uint64
	CacheMisses         uint64
	CacheHitRate        float64
}

// statsFromManager converts qrdbsdk.EmbeddingManagerStats to Stats.
func statsFromManager(s qrdbsdk.EmbeddingManagerStats) Stats {
	return Stats{
		ProvidersRegistered: s.ProvidersRegistered,
		ProviderNames:       s.ProviderNames,
		DefaultProvider:     s.DefaultProvider,
		CacheEnabled:        s.CacheEnabled,
		CacheSize:           s.CacheSize,
		CacheHits:           s.CacheHits,
		CacheMisses:         s.CacheMisses,
		CacheHitRate:        s.CacheHitRate,
	}
}
