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
	"time"

	qembed "github.com/jeremyhahn/go-qrdb/pkg/embedding"
)

// Default constants re-exported from go-qrdb for backward compatibility.
const (
	DefaultOllamaURL        = qembed.DefaultOllamaBaseURL
	DefaultOllamaTimeout    = qembed.DefaultOllamaTimeout
	DefaultOllamaMaxRetries = 3
	DefaultOllamaRetryBackoff = time.Second
)

// OllamaConfig holds configuration for the Ollama embedding provider.
// It is the same shape as qembed.OllamaConfig and is re-exported here for
// callers that construct providers directly without importing go-qrdb.
type OllamaConfig = qembed.OllamaConfig

// DefaultOllamaConfig returns the default Ollama configuration.
func DefaultOllamaConfig() OllamaConfig {
	return qembed.DefaultOllamaConfig()
}

// OllamaProvider implements Provider by delegating to qembed.OllamaProvider.
//
// Compile-time assertion that OllamaProvider satisfies Provider.
var _ Provider = (*OllamaProvider)(nil)

// OllamaProvider is a thin wrapper around qembed.OllamaProvider that
// exposes the single-text Embed API used in go-quicbaas.
type OllamaProvider struct {
	inner *qembed.OllamaProvider
}

// NewOllamaProvider creates a new OllamaProvider.
func NewOllamaProvider(config OllamaConfig) (*OllamaProvider, error) {
	inner, err := qembed.NewOllamaProvider(config)
	if err != nil {
		return nil, err
	}
	return &OllamaProvider{inner: inner}, nil
}

// Embed generates an embedding for a single text.
func (p *OllamaProvider) Embed(ctx context.Context, text string) ([]float32, error) {
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
func (p *OllamaProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	result, err := p.inner.Embed(ctx, texts, "")
	if err != nil {
		return nil, err
	}
	return result.Embeddings, nil
}

// Dimensions returns the embedding vector dimensionality.
func (p *OllamaProvider) Dimensions() int { return p.inner.Dimensions() }

// MaxTokens returns 8192 (standard Ollama embedding model limit).
func (p *OllamaProvider) MaxTokens() int { return 8192 }

// Name returns "ollama".
func (p *OllamaProvider) Name() string { return "ollama" }

// Close releases resources held by the underlying HTTP client.
func (p *OllamaProvider) Close() error { return p.inner.Close() }
