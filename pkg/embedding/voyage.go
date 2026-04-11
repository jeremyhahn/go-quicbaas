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

// VoyageProvider implements Provider for Voyage AI by delegating to
// qrdbsdk.VoyageEmbeddingProvider.
//
// Compile-time assertion that VoyageProvider satisfies Provider.
var _ Provider = (*VoyageProvider)(nil)

// VoyageProvider wraps qrdbsdk.VoyageEmbeddingProvider with the single-text Embed API.
type VoyageProvider struct {
	inner *qrdbsdk.VoyageEmbeddingProvider
}

// NewVoyageProvider creates a new Voyage AI embedding provider.
func NewVoyageProvider(config Config) (*VoyageProvider, error) {
	inner, err := qrdbsdk.NewVoyageEmbeddingProvider(config)
	if err != nil {
		return nil, err
	}
	return &VoyageProvider{inner: inner}, nil
}

// Embed generates an embedding for a single text.
func (p *VoyageProvider) Embed(ctx context.Context, text string) ([]float32, error) {
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
func (p *VoyageProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result, err := p.inner.Embed(ctx, texts, "")
	if err != nil {
		return nil, err
	}
	return result.Embeddings, nil
}

// Dimensions returns the embedding vector dimensionality.
func (p *VoyageProvider) Dimensions() int { return p.inner.Dimensions() }

// MaxTokens returns the maximum tokens for the configured Voyage model.
func (p *VoyageProvider) MaxTokens() int { return 32000 }

// Name returns "voyage".
func (p *VoyageProvider) Name() string { return "voyage" }

// Close releases resources held by the underlying HTTP client.
func (p *VoyageProvider) Close() error { return p.inner.Close() }
