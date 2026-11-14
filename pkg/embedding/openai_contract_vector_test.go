//go:build vector && qrdb_vector

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestOpenAIProvider returns an OpenAI provider pointed at an in-process
// OpenAI-compatible server that counts the embedding requests it serves.
// The server's URL is written into cfg so a manager built from it validates.
func newTestOpenAIProvider(t *testing.T, cfg *Config) (Provider, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	srv := openAIEmbeddingHandler(t, 4, func(openAIRequestBody, string) { requests.Add(1) })
	cfg.APIURL = srv.URL
	p, err := NewOpenAIProvider(*cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p, &requests
}

// openAIConfig is a Config for the OpenAI provider with retries off, so error
// paths return on the first failure.
func openAIConfig() Config {
	return Config{
		Provider:     "openai",
		Model:        "custom-model",
		Dimensions:   4,
		MaxBatchSize: 8,
		Timeout:      30,
		MaxRetries:   0,
	}
}

// newCachingManager registers an OpenAI provider on a manager with the
// embedding cache on and returns the server's request counter.
func newCachingManager(t *testing.T, compress bool) (*Manager, *atomic.Int64) {
	t.Helper()
	cfg := openAIConfig()
	cfg.CacheEnabled = true
	cfg.CacheSize = 100
	cfg.CacheTTL = 3600
	cfg.CacheCompress = compress
	p, requests := newTestOpenAIProvider(t, &cfg)

	m, err := NewManager(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })
	require.NoError(t, m.RegisterProvider("openai", p))
	return m, requests
}

func TestOpenAIProvider_EmbedBatch_EmptyBatchRefused(t *testing.T) {
	cfg := openAIConfig()
	p, requests := newTestOpenAIProvider(t, &cfg)

	// go-qrdb refuses the empty batch with its InvalidInputError, which the
	// SDK does not re-export, so only the refusal itself is asserted.
	_, err := p.EmbedBatch(context.Background(), []string{})
	require.Error(t, err)
	assert.Zero(t, requests.Load(), "an empty batch must be refused before any request")
}

func TestOpenAIProvider_EmbedBatch_SingleAndLargeBatches(t *testing.T) {
	const dims = 4
	large := make([]string, 20)
	for i := range large {
		large[i] = "document " + strconv.Itoa(i)
	}
	cases := map[string][]string{
		"single": {"single item"},
		"small": {
			"machine learning",
			"artificial intelligence",
			"deep learning",
			"neural networks",
			"natural language processing",
		},
		"large": large,
	}

	for name, texts := range cases {
		t.Run(name, func(t *testing.T) {
			var mu sync.Mutex
			var submitted [][]string
			srv := openAIEmbeddingHandler(t, dims, func(req openAIRequestBody, _ string) {
				mu.Lock()
				defer mu.Unlock()
				submitted = append(submitted, req.Input)
			})
			cfg := openAIConfig()
			cfg.APIURL = srv.URL
			cfg.MaxBatchSize = len(large)
			p, err := NewOpenAIProvider(cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = p.Close() })

			got, err := p.EmbedBatch(context.Background(), texts)
			require.NoError(t, err)

			mu.Lock()
			defer mu.Unlock()
			require.Equal(t, [][]string{texts}, submitted, "the server must receive exactly the submitted texts, in order")
			require.Len(t, got, len(texts))
			for i, vec := range got {
				assert.Equal(t, fixtureVector(i, dims), vec, "embedding %d", i)
			}
		})
	}
}

// fixtureVector is the vector openAIEmbeddingHandler returns for the input at
// index i of a request.
func fixtureVector(i, dims int) []float32 {
	vec := make([]float32, dims)
	for j := range vec {
		vec[j] = float32(i*dims + j)
	}
	return vec
}

func TestOpenAIProvider_Embed_Errors(t *testing.T) {
	tests := []struct {
		name string
		// setup returns the context to embed with; it may stop the server.
		setup func(t *testing.T, stopServer func()) context.Context
		// want is the error the call must wrap; nil accepts any error.
		want error
	}{
		{
			name: "canceled context",
			setup: func(t *testing.T, _ func()) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			want: context.Canceled,
		},
		{
			name: "expired deadline",
			setup: func(t *testing.T, _ func()) context.Context {
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)
				return ctx
			},
			want: context.DeadlineExceeded,
		},
		{
			name: "unreachable server",
			setup: func(t *testing.T, stopServer func()) context.Context {
				stopServer() // the listener is gone, so the dial is refused
				return context.Background()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := openAIEmbeddingHandler(t, 4, nil)
			cfg := openAIConfig()
			cfg.APIURL = srv.URL
			p, err := NewOpenAIProvider(cfg)
			require.NoError(t, err)
			t.Cleanup(func() { _ = p.Close() })
			ctx := tt.setup(t, srv.Close)

			_, err = p.Embed(ctx, "test")
			require.Error(t, err)
			if tt.want != nil {
				require.ErrorIs(t, err, tt.want)
			}
		})
	}
}

func TestManager_RegisterProvider_DuplicateRefused(t *testing.T) {
	cfg := openAIConfig()
	p, _ := newTestOpenAIProvider(t, &cfg)
	m, err := NewManager(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = m.Close() })

	require.NoError(t, m.RegisterProvider("openai", p))
	require.Error(t, m.RegisterProvider("openai", p), "a second registration under one name must be refused")
}

func TestManager_Embed_CacheHitSkipsProvider(t *testing.T) {
	m, requests := newCachingManager(t, false)
	ctx := context.Background()

	first, err := m.Embed(ctx, "stats test 1") // miss
	require.NoError(t, err)
	cached, err := m.Embed(ctx, "stats test 1") // hit
	require.NoError(t, err)
	_, err = m.Embed(ctx, "stats test 2") // miss
	require.NoError(t, err)

	assert.Equal(t, first, cached)
	assert.Equal(t, int64(2), requests.Load(), "the cache hit must not reach the provider")

	stats := m.GetStats()
	assert.True(t, stats.CacheEnabled)
	assert.Equal(t, uint64(1), stats.CacheHits)
	assert.Equal(t, uint64(2), stats.CacheMisses)
	assert.InDelta(t, 1.0/3.0, stats.CacheHitRate, 0.01)

	// EmbedBatch goes to the provider directly; a repeat returns the same vectors.
	texts := []string{"batch1", "batch2", "batch3"}
	batch, err := m.EmbedBatch(ctx, texts)
	require.NoError(t, err)
	require.Len(t, batch, len(texts))
	repeat, err := m.EmbedBatch(ctx, texts)
	require.NoError(t, err)
	assert.Equal(t, batch, repeat)
}

func TestManager_Embed_CacheCompressionIsLossless(t *testing.T) {
	for _, compress := range []bool{true, false} {
		t.Run("compress="+strconv.FormatBool(compress), func(t *testing.T) {
			m, requests := newCachingManager(t, compress)
			ctx := context.Background()

			first, err := m.Embed(ctx, "compression round-trip test")
			require.NoError(t, err)
			cached, err := m.Embed(ctx, "compression round-trip test")
			require.NoError(t, err)

			assert.Equal(t, first, cached, "the cached vector must match the computed one")
			assert.Equal(t, int64(1), requests.Load())
		})
	}
}
