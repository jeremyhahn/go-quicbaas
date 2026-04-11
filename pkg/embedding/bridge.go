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

	qembed "github.com/jeremyhahn/go-qrdb/pkg/embedding"
)

// providerBridgeIn adapts a qembed.Provider (batch-first, with model param)
// to the go-quicbaas Provider interface (single-text, no model param).
// It is used when a go-qrdb provider is retrieved from the Manager.
type providerBridgeIn struct {
	inner qembed.Provider
	model string // default model used when Embed is called
}

func (p *providerBridgeIn) Embed(ctx context.Context, text string) ([]float32, error) {
	result, err := p.inner.Embed(ctx, []string{text}, p.model)
	if err != nil {
		return nil, err
	}
	if len(result.Embeddings) == 0 {
		return nil, ErrInvalidInput
	}
	return result.Embeddings[0], nil
}

func (p *providerBridgeIn) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result, err := p.inner.Embed(ctx, texts, p.model)
	if err != nil {
		return nil, err
	}
	return result.Embeddings, nil
}

func (p *providerBridgeIn) Dimensions() int  { return p.inner.Dimensions() }
func (p *providerBridgeIn) MaxTokens() int   { return 0 }
func (p *providerBridgeIn) Name() string     { return p.inner.Name() }
func (p *providerBridgeIn) Close() error     { return p.inner.Close() }

// providerBridgeOut adapts a go-quicbaas Provider (single-text, no model param)
// to the qembed.Provider interface (batch-first, with model param).
// It is used when a go-quicbaas Provider is registered with the Manager.
type providerBridgeOut struct {
	inner Provider
}

func (p *providerBridgeOut) Embed(ctx context.Context, texts []string, _ string) (*qembed.EmbeddingResult, error) {
	// For a single text use Embed (preserves test hooks keyed on embedFunc).
	// For multiple texts use EmbedBatch (preserves test hooks keyed on embedBatchFunc).
	if len(texts) == 1 {
		emb, err := p.inner.Embed(ctx, texts[0])
		if err != nil {
			return nil, err
		}
		return &qembed.EmbeddingResult{
			Embeddings: [][]float32{emb},
			Model:      p.inner.Name(),
		}, nil
	}

	embeddings, err := p.inner.EmbedBatch(ctx, texts)
	if err != nil {
		return nil, err
	}
	return &qembed.EmbeddingResult{
		Embeddings: embeddings,
		Model:      p.inner.Name(),
	}, nil
}

func (p *providerBridgeOut) Dimensions() int                                        { return p.inner.Dimensions() }
func (p *providerBridgeOut) Name() string                                           { return p.inner.Name() }
func (p *providerBridgeOut) Models(_ context.Context) ([]qembed.ModelInfo, error)   { return nil, nil }
func (p *providerBridgeOut) Close() error                                           { return p.inner.Close() }
