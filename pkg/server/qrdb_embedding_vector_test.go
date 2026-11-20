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

	"github.com/jeremyhahn/go-qrdb/pkg/api/errclass"
	qrdbgrpc "github.com/jeremyhahn/go-qrdb/pkg/api/grpc"
	qrdbmcp "github.com/jeremyhahn/go-qrdb/pkg/api/mcp"
	mcpwire "github.com/jeremyhahn/go-qrdb/pkg/api/mcp/wire"
	qrdbquic "github.com/jeremyhahn/go-qrdb/pkg/api/quic"
	quicwire "github.com/jeremyhahn/go-qrdb/pkg/api/quic/wire"
	qrdbrest "github.com/jeremyhahn/go-qrdb/pkg/api/rest"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
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
// newRecordingEmbeddingStub starts an OpenAI-compatible embeddings stub that
// answers three-dimensional vectors and records the model of every
// embeddings request; recorded returns the models in request order.
func newRecordingEmbeddingStub(t *testing.T) (stub *httptest.Server, recorded func() []string) {
	t.Helper()
	var (
		mu     sync.Mutex
		models []string
	)
	stub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	recorded = func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), models...)
	}
	return stub, recorded
}

// newOpenAIEmbeddingNode returns a module-host test server whose primary
// embedding provider is openai with model "m" served by stub, with go-qrdb's
// embedding service composed and the embedding manager started.
func newOpenAIEmbeddingNode(t *testing.T, stub *httptest.Server, additional ...config.EmbeddingConfig) qrdbservice.EmbeddingServicer {
	t.Helper()
	srv := newModuleHostTestServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Provider = "openai"
	srv.config.RAG.Embedding.APIURL = stub.URL
	srv.config.RAG.Embedding.APIKey = "k"
	srv.config.RAG.Embedding.Model = "m"
	srv.config.RAG.Embedding.Dimensions = 3
	srv.config.RAG.Embedding.Additional = additional

	_, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	require.NoError(t, srv.startEmbedding())
	t.Cleanup(func() { _ = srv.stopEmbedding() })

	svc := srv.GetQRDBEmbeddingServicer()
	require.NotNil(t, svc)
	return svc
}

func TestQRDBEmbedding_ProviderRefusesAnotherModel(t *testing.T) {
	stub, recorded := newRecordingEmbeddingStub(t)

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

// TestEffectiveEmbeddingModel checks that a provider configured without a
// model is served with the model its constructor applies.
func TestEffectiveEmbeddingModel(t *testing.T) {
	cases := []struct {
		cfg  embedding.Config
		want string
	}{
		{embedding.Config{Provider: "ollama"}, "nomic-embed-text"},
		{embedding.Config{Provider: "lmstudio"}, "nomic-ai/nomic-embed-text-v1.5-GGUF"},
		{embedding.Config{Provider: "openai"}, "text-embedding-3-small"},
		{embedding.Config{Provider: "voyage"}, "voyage-code-2"},
		{embedding.Config{Provider: "npu"}, "all-MiniLM-L6-v2"},
		{embedding.Config{Provider: "openai", Model: "m"}, "m"},
		{embedding.Config{Provider: "unknown"}, ""},
	}
	for _, tc := range cases {
		cfg := tc.cfg
		assert.Equal(t, tc.want, effectiveEmbeddingModel(&cfg), "provider %q model %q", cfg.Provider, cfg.Model)
	}
}

// TestQRDBEmbedding_AdditionalProviderServesItsDefaultModel checks that an
// additional provider configured without a model is served by go-qrdb's
// embedding service with the model its constructor applies.
func TestQRDBEmbedding_AdditionalProviderServesItsDefaultModel(t *testing.T) {
	const lmstudioModel = "nomic-ai/nomic-embed-text-v1.5-GGUF"
	stub, recorded := newRecordingEmbeddingStub(t)
	svc := newOpenAIEmbeddingNode(t, stub, config.EmbeddingConfig{Provider: "lmstudio", APIURL: stub.URL, Dimensions: 3})

	ctx := context.Background()
	resp, err := svc.Embed(ctx, &qrdbservice.EmbedRequest{Texts: []string{"hi"}, Provider: "lmstudio"})
	require.NoError(t, err)
	assert.Equal(t, lmstudioModel, resp.Model)

	resp, err = svc.Embed(ctx, &qrdbservice.EmbedRequest{Texts: []string{"hello"}, Provider: "lmstudio", Model: lmstudioModel})
	require.NoError(t, err, "a request naming the provider's effective model must be served")
	assert.Equal(t, lmstudioModel, resp.Model)

	models, err := svc.ListModels(ctx, &qrdbservice.ListEmbeddingModelsRequest{Provider: "lmstudio"})
	require.NoError(t, err)
	require.Len(t, models.Models, 1)
	assert.Equal(t, lmstudioModel, models.Models[0].Name)

	assert.Contains(t, recorded(), lmstudioModel, "the provider must be called with its effective model")
}

// TestQRDBEmbedding_AnotherModelIsInvalidArgumentOnEveryTransport checks that
// the refusal of another model carries go-qrdb's InvalidArgument class, so
// every native transport answers it as an invalid argument, and still
// unwraps to an *api.InvalidInputError.
func TestQRDBEmbedding_AnotherModelIsInvalidArgumentOnEveryTransport(t *testing.T) {
	stub, _ := newRecordingEmbeddingStub(t)
	svc := newOpenAIEmbeddingNode(t, stub)

	ctx := context.Background()
	const large = "text-embedding-3-large"
	_, embedErr := svc.Embed(ctx, &qrdbservice.EmbedRequest{Texts: []string{"hi"}, Model: large})
	_, batchErr := svc.BatchEmbed(ctx, &qrdbservice.BatchEmbedRequest{Texts: []string{"hi"}, Model: large})

	for name, err := range map[string]error{"Embed": embedErr, "BatchEmbed": batchErr} {
		require.Error(t, err, name)
		assert.Equal(t, errclass.InvalidArgument, errclass.Classify(err), name)
		assert.Equal(t, http.StatusBadRequest, qrdbrest.ServiceErrorStatus(err), name)
		assert.Equal(t, codes.InvalidArgument, qrdbgrpc.ServiceErrorCode(err), name)
		assert.Equal(t, quicwire.StatusInvalidArgument, qrdbquic.ServiceErrorStatus(err), name)
		assert.Equal(t, mcpwire.CodeInvalidArgument, qrdbmcp.ServiceErrorCode(err), name)
		var invalid *api.InvalidInputError
		assert.True(t, errors.As(err, &invalid), "%s: want *api.InvalidInputError in the chain, got %T", name, err)
	}
}

var _ embedding.Provider = (*singleModelProvider)(nil)
