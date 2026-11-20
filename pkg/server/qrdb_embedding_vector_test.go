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

//go:build vector && qrdb_vector

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	qrdbembedding "github.com/jeremyhahn/go-qrdb/pkg/embedding"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

// TestUnifiedModuleServerOptions_ComposesQRDBEmbedding checks that the node's
// server options create go-qrdb's embedding service once and reuse it.
func TestUnifiedModuleServerOptions_ComposesQRDBEmbedding(t *testing.T) {
	srv := newModuleHostTestServer(t)

	first, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	svc := srv.GetQRDBEmbeddingServicer()
	require.NotNil(t, svc)

	second, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	assert.Same(t, svc, srv.GetQRDBEmbeddingServicer())
	assert.Equal(t, len(first), len(second))
}

// TestQRDBEmbedding_ProviderServesGoQRDBEmbed checks that a provider the
// quicbaas embedding manager registers also serves go-qrdb's embedding service.
func TestQRDBEmbedding_ProviderServesGoQRDBEmbed(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{{"id": "m", "object": "model"}}})
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/embeddings") {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Input json.RawMessage `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var inputs []string
		if err := json.Unmarshal(body.Input, &inputs); err != nil {
			var one string
			if err := json.Unmarshal(body.Input, &one); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			inputs = []string{one}
		}
		type item struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		data := make([]item, len(inputs))
		for i := range inputs {
			data[i] = item{Embedding: []float32{0.1, 0.2, 0.3}, Index: i}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "model": "m"})
	}))
	t.Cleanup(stub.Close)

	srv := newModuleHostTestServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Provider = "openai"
	srv.config.RAG.Embedding.APIURL = stub.URL
	srv.config.RAG.Embedding.APIKey = "k"
	srv.config.RAG.Embedding.Model = "m"
	srv.config.RAG.Embedding.Dimensions = 3

	_, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	require.NoError(t, srv.startEmbedding())
	t.Cleanup(func() { _ = srv.stopEmbedding() })

	svc := srv.GetQRDBEmbeddingServicer()
	require.NotNil(t, svc)

	ctx := context.Background()
	resp, err := svc.Embed(ctx, &qrdbservice.EmbedRequest{Texts: []string{"hi"}})
	require.NoError(t, err)
	require.Len(t, resp.Embeddings, 1)
	require.Len(t, resp.Embeddings[0], 3)

	models, err := svc.ListModels(ctx, &qrdbservice.ListEmbeddingModelsRequest{Provider: "openai"})
	require.NoError(t, err)
	require.Len(t, models.Models, 1)
	assert.Equal(t, "m", models.Models[0].Name)
}

// TestQRDBEmbedding_ProviderHonoursRequestedModel checks that a go-qrdb
// embedding request naming a model reaches the provider with that model, not
// the configured default.
func TestQRDBEmbedding_ProviderHonoursRequestedModel(t *testing.T) {
	var (
		mu     sync.Mutex
		models []string
	)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/models") {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": []map[string]any{{"id": "m", "object": "model"}}})
			return
		}
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/embeddings") {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Input json.RawMessage `json:"input"`
			Model string          `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		mu.Lock()
		models = append(models, body.Model)
		mu.Unlock()
		var inputs []string
		if err := json.Unmarshal(body.Input, &inputs); err != nil {
			var one string
			if err := json.Unmarshal(body.Input, &one); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			inputs = []string{one}
		}
		type item struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		}
		data := make([]item, len(inputs))
		for i := range inputs {
			data[i] = item{Embedding: []float32{0.1, 0.2, 0.3}, Index: i}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "model": body.Model})
	}))
	t.Cleanup(stub.Close)
	recorded := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), models...)
	}

	srv := newModuleHostTestServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Provider = "openai"
	srv.config.RAG.Embedding.APIURL = stub.URL
	srv.config.RAG.Embedding.APIKey = "k"
	srv.config.RAG.Embedding.Model = "m"
	srv.config.RAG.Embedding.Dimensions = 3

	_, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	require.NoError(t, srv.startEmbedding())
	t.Cleanup(func() { _ = srv.stopEmbedding() })

	svc := srv.GetQRDBEmbeddingServicer()
	require.NotNil(t, svc)

	ctx := context.Background()
	const large = "text-embedding-3-large"
	resp, err := svc.Embed(ctx, &qrdbservice.EmbedRequest{Texts: []string{"hi"}, Model: large})
	require.NoError(t, err)
	assert.Equal(t, []string{large}, recorded())
	assert.Equal(t, large, resp.Model)

	batch, err := svc.BatchEmbed(ctx, &qrdbservice.BatchEmbedRequest{Texts: []string{"hi"}, Model: large})
	require.NoError(t, err)
	assert.Equal(t, []string{large, large}, recorded())
	assert.Equal(t, large, batch.Model)
}

// singleModelProvider is an embedding.Provider without per-call model
// support; it counts the calls it serves.
type singleModelProvider struct {
	calls int
}

func (p *singleModelProvider) Embed(_ context.Context, _ string) ([]float32, error) {
	p.calls++
	return []float32{1}, nil
}

func (p *singleModelProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	p.calls++
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1}
	}
	return out, nil
}

func (p *singleModelProvider) Dimensions() int { return 1 }
func (p *singleModelProvider) MaxTokens() int  { return 8 }
func (p *singleModelProvider) Name() string    { return "single" }
func (p *singleModelProvider) Close() error    { return nil }

// TestQRDBEmbeddingProvider_ModelWithoutPerCallSupport checks that a provider
// serving one model refuses a request for another model instead of answering
// with the default model's vectors.
func TestQRDBEmbeddingProvider_ModelWithoutPerCallSupport(t *testing.T) {
	fake := &singleModelProvider{}
	a := &qrdbEmbeddingProvider{p: fake, model: "m"}
	ctx := context.Background()
	texts := []string{"hi"}

	for _, model := range []string{"", "m"} {
		res, err := a.Embed(ctx, texts, model)
		require.NoError(t, err, "model %q", model)
		assert.Equal(t, "m", res.Model)
	}
	require.Equal(t, 2, fake.calls)

	res, err := a.Embed(ctx, texts, "other")
	require.Error(t, err)
	assert.Nil(t, res)
	var invalid *api.InvalidInputError
	require.True(t, errors.As(err, &invalid), "want *api.InvalidInputError, got %T", err)
	assert.Equal(t, 2, fake.calls, "the provider must not be called for another model")
}

// upstreamEmbedder is a go-qrdb embedding provider double: it answers with
// fixed vectors, the model it was asked for (or its default), a fixed token
// usage, and a fixed model list.
type upstreamEmbedder struct {
	usage  qrdbembedding.UsageInfo
	models []qrdbsdk.EmbeddingModelInfo
}

func (u *upstreamEmbedder) Embed(_ context.Context, texts []string, model string) (*qrdbsdk.EmbeddingResult, error) {
	if model == "" {
		model = "m"
	}
	vecs := make([][]float32, len(texts))
	for i := range texts {
		vecs[i] = []float32{1, 2, 3}
	}
	usage := u.usage
	return &qrdbsdk.EmbeddingResult{Embeddings: vecs, Model: model, Usage: &usage}, nil
}

func (u *upstreamEmbedder) Models(_ context.Context) ([]qrdbsdk.EmbeddingModelInfo, error) {
	return u.models, nil
}

func (u *upstreamEmbedder) Dimensions() int { return 3 }
func (u *upstreamEmbedder) Name() string    { return "upstream" }
func (u *upstreamEmbedder) Close() error    { return nil }

// upstreamBackedProvider is a quicbaas provider that wraps a go-qrdb provider,
// as the openai, ollama, lmstudio and voyage wrappers do.
type upstreamBackedProvider struct {
	singleModelProvider
	up *upstreamEmbedder
}

func (p *upstreamBackedProvider) QRDBProvider() qrdbsdk.EmbeddingProvider { return p.up }

var _ embedding.QRDBUpstream = (*upstreamBackedProvider)(nil)

func TestQRDBEmbeddingProvider_CarriesUsage(t *testing.T) {
	up := &upstreamEmbedder{usage: qrdbembedding.UsageInfo{PromptTokens: 7, TotalTokens: 9}}
	a := &qrdbEmbeddingProvider{p: &upstreamBackedProvider{up: up}, model: "m"}
	ctx := context.Background()

	for _, model := range []string{"", "m", "other"} {
		res, err := a.Embed(ctx, []string{"a", "b"}, model)
		require.NoError(t, err, "model %q", model)
		require.Len(t, res.Embeddings, 2)
		require.NotNil(t, res.Usage, "model %q: the upstream's usage must be carried", model)
		assert.Equal(t, 7, res.Usage.PromptTokens, "model %q", model)
		assert.Equal(t, 9, res.Usage.TotalTokens, "model %q", model)
		if model == "other" {
			assert.Equal(t, "other", res.Model)
		} else {
			assert.Equal(t, "m", res.Model)
		}
	}
}

func TestQRDBEmbeddingProvider_ListsUpstreamModels(t *testing.T) {
	up := &upstreamEmbedder{models: []qrdbsdk.EmbeddingModelInfo{
		{Name: "small", Dimensions: 384, MaxTokens: 512},
		{Name: "large", Dimensions: 1024, MaxTokens: 8192},
	}}
	a := &qrdbEmbeddingProvider{p: &upstreamBackedProvider{up: up}, model: "m"}

	models, err := a.Models(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []qrdbservice.ModelInfo{
		{Name: "small", Dimensions: 384, MaxTokens: 512},
		{Name: "large", Dimensions: 1024, MaxTokens: 8192},
	}, models)
}

var _ embedding.Provider = (*singleModelProvider)(nil)
