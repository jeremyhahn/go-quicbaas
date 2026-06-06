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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Default constants for the LMStudio provider.
const (
	DefaultLMStudioURL            = qrdbsdk.DefaultLMStudioEmbeddingBaseURL
	DefaultLMStudioMaxRetries     = 3
	DefaultLMStudioRetryBackoffMS = 1000
)

// DefaultLMStudioTimeout is the default per-request HTTP timeout for LMStudio.
var DefaultLMStudioTimeout = qrdbsdk.DefaultLMStudioEmbeddingTimeout

// LMStudioConfig holds configuration for the LMStudio embedding provider.
type LMStudioConfig = qrdbsdk.LMStudioEmbeddingConfig

// DefaultLMStudioConfig returns the default LMStudio configuration.
func DefaultLMStudioConfig() LMStudioConfig {
	return qrdbsdk.DefaultLMStudioEmbeddingConfig()
}

// LMStudioProvider implements Provider for LMStudio by delegating to
// qrdbsdk.LMStudioEmbeddingProvider.
//
// Compile-time assertion that LMStudioProvider satisfies Provider.
var _ Provider = (*LMStudioProvider)(nil)

// LMStudioProvider wraps qrdbsdk.LMStudioEmbeddingProvider with the single-text Embed API.
type LMStudioProvider struct {
	inner *qrdbsdk.LMStudioEmbeddingProvider
}

// NewLMStudioProvider creates a new LMStudio embedding provider.
func NewLMStudioProvider(config LMStudioConfig) (*LMStudioProvider, error) {
	inner, err := qrdbsdk.NewLMStudioEmbeddingProvider(config)
	if err != nil {
		return nil, err
	}
	return &LMStudioProvider{inner: inner}, nil
}

// Embed generates an embedding for a single text.
func (p *LMStudioProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	result, err := p.inner.Embed(ctx, []string{text}, "")
	if err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 {
		return nil, ErrInvalidInput
	}
	return result.Embeddings[0], nil
}

// EmbedBatch generates embeddings for multiple texts.
func (p *LMStudioProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result, err := p.inner.Embed(ctx, texts, "")
	if err != nil {
		return nil, err
	}
	return result.Embeddings, nil
}

// Dimensions returns the embedding vector dimensionality.
func (p *LMStudioProvider) Dimensions() int { return p.inner.Dimensions() }

// MaxTokens returns 8192 (standard LMStudio GGUF model limit).
func (p *LMStudioProvider) MaxTokens() int { return 8192 }

// Name returns "lmstudio".
func (p *LMStudioProvider) Name() string { return "lmstudio" }

// Close releases resources held by the underlying HTTP client.
func (p *LMStudioProvider) Close() error { return p.inner.Close() }
