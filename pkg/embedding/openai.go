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

package embedding

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// OpenAIProvider implements Provider for OpenAI-compatible embedding APIs
// by delegating to qrdbsdk.OpenAIEmbeddingProvider.
//
// Compile-time assertion that OpenAIProvider satisfies Provider.
var _ Provider = (*OpenAIProvider)(nil)

// OpenAIProvider wraps qrdbsdk.OpenAIEmbeddingProvider with the single-text Embed API.
type OpenAIProvider struct {
	inner *qrdbsdk.OpenAIEmbeddingProvider
}

// NewOpenAIProvider creates a new OpenAI-compatible embedding provider.
func NewOpenAIProvider(config Config) (*OpenAIProvider, error) {
	inner, err := qrdbsdk.NewOpenAIEmbeddingProvider(config)
	if err != nil {
		return nil, err
	}
	return &OpenAIProvider{inner: inner}, nil
}

// Embed generates an embedding for a single text.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
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
func (p *OpenAIProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result, err := p.inner.Embed(ctx, texts, "")
	if err != nil {
		return nil, err
	}
	return result.Embeddings, nil
}

// Dimensions returns the embedding vector dimensionality.
func (p *OpenAIProvider) Dimensions() int { return p.inner.Dimensions() }

// MaxTokens returns 8191 (standard OpenAI embedding model limit).
func (p *OpenAIProvider) MaxTokens() int { return 8191 }

// Name returns "openai".
func (p *OpenAIProvider) Name() string { return "openai" }

// Close releases resources held by the underlying HTTP client.
func (p *OpenAIProvider) Close() error { return p.inner.Close() }
