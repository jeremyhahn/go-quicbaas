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

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
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

// TestQRDBEmbedding_ProviderRefusesAnotherModel checks that a quicbaas node
// serves a provider with its configured model only: a go-qrdb embedding
// request naming another model is refused and never reaches the provider,
// while the configured model is served.
func TestQRDBEmbedding_ProviderRefusesAnotherModel(t *testing.T) {
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
	_, err = svc.Embed(ctx, &qrdbservice.EmbedRequest{Texts: []string{"hi"}, Model: large})
	require.Error(t, err, "Embed with a model other than the configured one must be refused")
	_, err = svc.BatchEmbed(ctx, &qrdbservice.BatchEmbedRequest{Texts: []string{"hi"}, Model: large})
	require.Error(t, err, "BatchEmbed with a model other than the configured one must be refused")
	assert.NotContains(t, recorded(), large, "the provider must not be called with a model other than the configured one")

	resp, err := svc.Embed(ctx, &qrdbservice.EmbedRequest{Texts: []string{"hi"}, Model: "m"})
	require.NoError(t, err)
	assert.Equal(t, []string{"m"}, recorded())
	assert.Equal(t, "m", resp.Model)
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

var _ embedding.Provider = (*singleModelProvider)(nil)
