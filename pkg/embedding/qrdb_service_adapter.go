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

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// qrdbServiceAdapter serves a native go-qrdb embedding provider as go-qrdb's
// service-layer EmbeddingProvider: it passes the per-call model through and
// reports the native model list.
type qrdbServiceAdapter struct {
	inner qrdbsdk.EmbeddingProvider
}

var _ qrdbservice.EmbeddingProvider = (*qrdbServiceAdapter)(nil)

var (
	_ QRDBServiceProvider = (*OpenAIProvider)(nil)
	_ QRDBServiceProvider = (*VoyageProvider)(nil)
	_ QRDBServiceProvider = (*OllamaProvider)(nil)
	_ QRDBServiceProvider = (*LMStudioProvider)(nil)
)

// Embed embeds texts with the native provider; an empty model selects the
// provider's configured model.
func (a *qrdbServiceAdapter) Embed(ctx context.Context, texts []string, model string) (*qrdbservice.EmbeddingResult, error) {
	res, err := a.inner.Embed(ctx, texts, model)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &qrdbservice.EmbeddingResult{}, nil
	}
	return &qrdbservice.EmbeddingResult{Embeddings: res.Embeddings, Model: res.Model}, nil
}

// Models reports the models the native provider lists.
func (a *qrdbServiceAdapter) Models(ctx context.Context) ([]qrdbservice.ModelInfo, error) {
	native, err := a.inner.Models(ctx)
	if err != nil {
		return nil, err
	}
	models := make([]qrdbservice.ModelInfo, 0, len(native))
	for _, m := range native {
		models = append(models, qrdbservice.ModelInfo{Name: m.Name, Dimensions: m.Dimensions, MaxTokens: m.MaxTokens})
	}
	return models, nil
}

// Close is a no-op: the embedding manager owns the wrapped provider.
func (a *qrdbServiceAdapter) Close() error {
	return nil
}

// QRDBServiceProvider returns the native OpenAI provider as go-qrdb's
// service-layer EmbeddingProvider.
func (p *OpenAIProvider) QRDBServiceProvider() qrdbservice.EmbeddingProvider {
	return &qrdbServiceAdapter{inner: p.inner}
}

// QRDBServiceProvider returns the native Voyage provider as go-qrdb's
// service-layer EmbeddingProvider.
func (p *VoyageProvider) QRDBServiceProvider() qrdbservice.EmbeddingProvider {
	return &qrdbServiceAdapter{inner: p.inner}
}

// QRDBServiceProvider returns the native Ollama provider as go-qrdb's
// service-layer EmbeddingProvider.
func (p *OllamaProvider) QRDBServiceProvider() qrdbservice.EmbeddingProvider {
	return &qrdbServiceAdapter{inner: p.inner}
}

// QRDBServiceProvider returns the native LM Studio provider as go-qrdb's
// service-layer EmbeddingProvider.
func (p *LMStudioProvider) QRDBServiceProvider() qrdbservice.EmbeddingProvider {
	return &qrdbServiceAdapter{inner: p.inner}
}
