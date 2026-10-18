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

package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/embedding"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/ollama"
)

var testStamp = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// errProviderDown is the error the provider doubles return when told to fail.
type errProviderDown struct{}

func (errProviderDown) Error() string { return "provider down" }

// fakeOllama is an in-memory OllamaService that answers deterministically.
// The embedded interface is never called: every method the client reaches is
// implemented below.
type fakeOllama struct {
	service.OllamaService
	fail      bool
	available bool
}

func (f *fakeOllama) Embed(_ context.Context, req *service.OllamaEmbedRequest) (*service.OllamaEmbedResponse, error) {
	return f.EmbedWithModel(context.Background(), &service.OllamaEmbedWithModelRequest{Text: req.Text, Model: "nomic-embed-text"})
}

func (f *fakeOllama) EmbedWithModel(_ context.Context, req *service.OllamaEmbedWithModelRequest) (*service.OllamaEmbedResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	return &service.OllamaEmbedResponse{
		Embedding:      []float32{float32(len(req.Text)), 1},
		Model:          req.Model,
		Dimensions:     2,
		ProcessingTime: time.Millisecond,
		Timestamp:      testStamp,
	}, nil
}

func (f *fakeOllama) EmbedBatch(_ context.Context, req *service.OllamaEmbedBatchRequest) (*service.OllamaEmbedBatchResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	out := make([][]float32, 0, len(req.Texts))
	for _, t := range req.Texts {
		out = append(out, []float32{float32(len(t)), 1})
	}
	return &service.OllamaEmbedBatchResponse{
		Embeddings: out, Model: req.Model, Dimensions: 2, Count: len(out), Timestamp: testStamp,
	}, nil
}

func (f *fakeOllama) ListModels(context.Context, *service.OllamaListModelsRequest) (*service.OllamaListModelsResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	return &service.OllamaListModelsResponse{
		Models: []service.OllamaModelInfo{{Name: "nomic-embed-text", Size: 274, Digest: "sha256:abc", ModifiedAt: testStamp}},
		Count:  1, Timestamp: testStamp,
	}, nil
}

func (f *fakeOllama) GetModelInfo(_ context.Context, req *service.OllamaGetModelInfoRequest) (*service.OllamaGetModelInfoResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	return &service.OllamaGetModelInfoResponse{
		Model: req.Model,
		Details: service.OllamaModelDetails{
			License: "Apache-2.0", Modelfile: "FROM x", Format: "gguf",
			Family: "nomic", ParameterSize: "137M", QuantizationLevel: "F16",
		},
		Timestamp: testStamp,
	}, nil
}

func (f *fakeOllama) Health(context.Context, *service.OllamaHealthRequest) (*service.OllamaHealthResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	return &service.OllamaHealthResponse{
		Available: f.available, Version: "0.5.0", BaseURL: "http://ollama.test:11434",
		LastChecked: testStamp, Timestamp: testStamp,
	}, nil
}

// fakeEmbedding is an in-memory EmbeddingService that answers deterministically.
type fakeEmbedding struct {
	service.EmbeddingService
	fail    bool
	cleared bool
}

func (f *fakeEmbedding) GenerateEmbedding(_ context.Context, req *service.GenerateEmbeddingRequest) (*service.GenerateEmbeddingResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	return &service.GenerateEmbeddingResponse{
		Embedding: []float32{float32(len(req.Text))},
		Metadata: &service.EmbeddingMetadata{
			Model: "m", Provider: req.Provider, Dimensions: 1, Source: req.Source,
			InputType: req.InputType, Tags: req.Metadata,
		},
		Timestamp: testStamp,
	}, nil
}

func (f *fakeEmbedding) BatchGenerateEmbeddings(_ context.Context, req *service.BatchGenerateEmbeddingsRequest) (*service.BatchGenerateEmbeddingsResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	results := make([]*service.BatchEmbeddingResult, 0, len(req.Items))
	for _, it := range req.Items {
		results = append(results, &service.BatchEmbeddingResult{
			ID: it.ID, Embedding: []float32{float32(len(it.Text))},
			Metadata: &service.EmbeddingMetadata{Provider: req.Provider, InputType: it.InputType},
		})
	}
	return &service.BatchGenerateEmbeddingsResponse{
		Results: results, SuccessCount: len(results), TotalTokens: 7, Timestamp: testStamp,
	}, nil
}

func (f *fakeEmbedding) provider() *service.ProviderInfo {
	return &service.ProviderInfo{
		Name: "ollama", Type: service.EmbeddingProvider("ollama"), Model: "m", Dimensions: 768,
		Status: service.EmbeddingStatus("available"), IsDefault: true, SuccessCount: 3,
	}
}

func (f *fakeEmbedding) ListProviders(_ context.Context, req *service.ListProvidersRequest) (*service.ListProvidersResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	providers := []*service.ProviderInfo{f.provider()}
	if req.IncludeUnavailable {
		providers = append(providers, &service.ProviderInfo{Name: "voyage", Status: service.EmbeddingStatus("unavailable")})
	}
	return &service.ListProvidersResponse{Providers: providers, DefaultProvider: "ollama", Timestamp: testStamp}, nil
}

func (f *fakeEmbedding) GetProviderInfo(_ context.Context, req *service.GetProviderInfoRequest) (*service.GetProviderInfoResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	info := f.provider()
	info.Type = req.Provider
	return &service.GetProviderInfoResponse{Info: info, Timestamp: testStamp}, nil
}

func (f *fakeEmbedding) SetDefaultProvider(_ context.Context, req *service.SetDefaultProviderRequest) (*service.SetDefaultProviderResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	return &service.SetDefaultProviderResponse{
		Provider: req.Provider, PreviousProvider: service.EmbeddingProvider("ollama"), Timestamp: testStamp,
	}, nil
}

func (f *fakeEmbedding) GetCacheStats(context.Context, *service.GetEmbeddingCacheStatsRequest) (*service.GetEmbeddingCacheStatsResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	return &service.GetEmbeddingCacheStatsResponse{
		Enabled: true, Size: 4, Capacity: 100, Hits: 3, Misses: 1, HitRate: 0.75, Timestamp: testStamp,
	}, nil
}

func (f *fakeEmbedding) ClearCache(_ context.Context, req *service.ClearEmbeddingCacheRequest) (*service.ClearEmbeddingCacheResponse, error) {
	if f.fail {
		return nil, errProviderDown{}
	}
	f.cleared = req.All
	return &service.ClearEmbeddingCacheResponse{EntriesCleared: 4, Provider: req.Provider, Timestamp: testStamp}, nil
}

// aiMock is a QuicBaaS double whose Ollama and Embedding providers are set
// per test; a nil field is the "not configured" facade.
type aiMock struct {
	*mockQuicBaaS
	ollama    service.OllamaService
	embedding service.EmbeddingService
}

func (m *aiMock) Ollama() service.OllamaService       { return m.ollama }
func (m *aiMock) Embedding() service.EmbeddingService { return m.embedding }

func newAITransport(t *testing.T, o service.OllamaService, e service.EmbeddingService) *Transport {
	t.Helper()
	tr, err := New(&aiMock{mockQuicBaaS: &mockQuicBaaS{}, ollama: o, embedding: e})
	require.NoError(t, err)
	return tr
}

func TestEmbeddedOllama_EveryMethod(t *testing.T) {
	ctx := context.Background()
	c := ollama.NewClient(newAITransport(t, &fakeOllama{available: true}, nil))

	emb, err := c.Embed(ctx, &ollama.EmbedRequest{Text: "abc"})
	require.NoError(t, err)
	assert.Equal(t, []float32{3, 1}, emb.Embedding)
	assert.Equal(t, "nomic-embed-text", emb.Model)
	assert.Equal(t, 2, emb.Dimensions)
	assert.Equal(t, time.Millisecond, emb.ProcessingTime)
	assert.Equal(t, testStamp, emb.Timestamp)

	emb, err = c.EmbedWithModel(ctx, &ollama.EmbedWithModelRequest{Text: "abcd", Model: "mxbai"})
	require.NoError(t, err)
	assert.Equal(t, "mxbai", emb.Model)
	assert.Equal(t, []float32{4, 1}, emb.Embedding)

	batch, err := c.EmbedBatch(ctx, &ollama.EmbedBatchRequest{Texts: []string{"a", "bb"}, Model: "mxbai"})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 1}, {2, 1}}, batch.Embeddings)
	assert.Equal(t, 2, batch.Count)
	assert.Equal(t, "mxbai", batch.Model)

	models, err := c.ListModels(ctx, &ollama.ListModelsRequest{})
	require.NoError(t, err)
	require.Len(t, models.Models, 1)
	assert.Equal(t, ollama.ModelInfo{Name: "nomic-embed-text", Size: 274, Digest: "sha256:abc", ModifiedAt: testStamp}, models.Models[0])
	assert.Equal(t, 1, models.Count)

	info, err := c.GetModelInfo(ctx, &ollama.GetModelInfoRequest{Model: "nomic-embed-text"})
	require.NoError(t, err)
	assert.Equal(t, "nomic-embed-text", info.Model)
	assert.Equal(t, ollama.ModelDetails{
		License: "Apache-2.0", Modelfile: "FROM x", Format: "gguf",
		Family: "nomic", ParameterSize: "137M", QuantizationLevel: "F16",
	}, info.Details)

	health, err := c.Health(ctx, &ollama.HealthRequest{})
	require.NoError(t, err)
	assert.True(t, health.Available)
	assert.Equal(t, "0.5.0", health.Version)
	assert.Equal(t, "http://ollama.test:11434", health.BaseURL)
	assert.Equal(t, testStamp, health.LastChecked)

	assert.True(t, c.IsAvailable())
}

func TestEmbeddedOllama_IsAvailableFollowsHealth(t *testing.T) {
	c := ollama.NewClient(newAITransport(t, &fakeOllama{available: false}, nil))
	assert.False(t, c.IsAvailable())

	c = ollama.NewClient(newAITransport(t, &fakeOllama{fail: true}, nil))
	assert.False(t, c.IsAvailable())
}

func TestEmbeddedOllama_ProviderErrorPropagates(t *testing.T) {
	ctx := context.Background()
	c := ollama.NewClient(newAITransport(t, &fakeOllama{fail: true}, nil))

	_, err := c.Embed(ctx, &ollama.EmbedRequest{Text: "abc"})
	assert.ErrorAs(t, err, &errProviderDown{})
	_, err = c.EmbedBatch(ctx, &ollama.EmbedBatchRequest{Texts: []string{"a"}})
	assert.ErrorAs(t, err, &errProviderDown{})
	_, err = c.ListModels(ctx, &ollama.ListModelsRequest{})
	assert.ErrorAs(t, err, &errProviderDown{})
	_, err = c.GetModelInfo(ctx, &ollama.GetModelInfoRequest{Model: "m"})
	assert.ErrorAs(t, err, &errProviderDown{})
	_, err = c.Health(ctx, &ollama.HealthRequest{})
	assert.ErrorAs(t, err, &errProviderDown{})
}

func TestEmbeddedOllama_ServiceNotConfigured(t *testing.T) {
	tr := newAITransport(t, nil, nil)
	for _, method := range ollamaMethods {
		_, err := tr.Request(context.Background(), ollamaServiceName, method, &ollama.HealthRequest{})
		var notCfg *ServiceNotConfiguredError
		require.ErrorAs(t, err, &notCfg, method)
		assert.Equal(t, "Ollama", notCfg.Service)
	}
}

func TestEmbeddedOllama_InvalidRequestType(t *testing.T) {
	tr := newAITransport(t, &fakeOllama{}, nil)
	_, err := tr.Request(context.Background(), ollamaServiceName, "Embed", &ollama.HealthRequest{})
	var typeErr *InvalidRequestTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "*ollama.EmbedRequest", typeErr.ExpectedType)

	_, err = tr.Request(context.Background(), ollamaServiceName, "GetModelInfo", (*ollama.GetModelInfoRequest)(nil))
	require.ErrorAs(t, err, &typeErr)
}

func TestEmbeddedEmbedding_EveryMethod(t *testing.T) {
	ctx := context.Background()
	fake := &fakeEmbedding{}
	c := embedding.NewClient(newAITransport(t, nil, fake))

	gen, err := c.GenerateEmbedding(ctx, &embedding.GenerateEmbeddingRequest{
		Text: "abc", Provider: embedding.ProviderOllama, InputType: embedding.InputTypeQuery,
		Source: "doc.md", Metadata: map[string]string{"k": "v"},
	})
	require.NoError(t, err)
	assert.Equal(t, []float32{3}, gen.Embedding)
	require.NotNil(t, gen.Metadata)
	assert.Equal(t, embedding.ProviderOllama, gen.Metadata.Provider)
	assert.Equal(t, embedding.InputTypeQuery, gen.Metadata.InputType)
	assert.Equal(t, "doc.md", gen.Metadata.Source)
	assert.Equal(t, map[string]string{"k": "v"}, gen.Metadata.Tags)
	assert.Equal(t, testStamp, gen.Timestamp)

	batch, err := c.BatchGenerateEmbeddings(ctx, &embedding.BatchGenerateEmbeddingsRequest{
		Provider: embedding.ProviderOllama,
		Items: []*embedding.BatchEmbeddingItem{
			{ID: "a", Text: "x", InputType: embedding.InputTypeDocument},
			{ID: "b", Text: "yy"},
		},
	})
	require.NoError(t, err)
	require.Len(t, batch.Results, 2)
	assert.Equal(t, "b", batch.Results[1].ID)
	assert.Equal(t, []float32{2}, batch.Results[1].Embedding)
	assert.Equal(t, embedding.InputTypeDocument, batch.Results[0].Metadata.InputType)
	assert.Equal(t, 2, batch.SuccessCount)
	assert.Equal(t, 7, batch.TotalTokens)

	list, err := c.ListProviders(ctx, &embedding.ListProvidersRequest{IncludeUnavailable: true})
	require.NoError(t, err)
	require.Len(t, list.Providers, 2)
	assert.Equal(t, "ollama", list.DefaultProvider)
	assert.Equal(t, embedding.ProviderOllama, list.Providers[0].Type)
	assert.Equal(t, 768, list.Providers[0].Dimensions)
	assert.True(t, list.Providers[0].IsDefault)
	assert.Equal(t, uint64(3), list.Providers[0].SuccessCount)

	info, err := c.GetProviderInfo(ctx, &embedding.GetProviderInfoRequest{Provider: embedding.ProviderOllama})
	require.NoError(t, err)
	require.NotNil(t, info.Info)
	assert.Equal(t, embedding.ProviderOllama, info.Info.Type)

	set, err := c.SetDefaultProvider(ctx, &embedding.SetDefaultProviderRequest{Provider: embedding.ProviderVoyage})
	require.NoError(t, err)
	assert.Equal(t, embedding.ProviderVoyage, set.Provider)
	assert.Equal(t, embedding.ProviderOllama, set.PreviousProvider)

	stats, err := c.GetCacheStats(ctx, &embedding.GetEmbeddingCacheStatsRequest{})
	require.NoError(t, err)
	assert.True(t, stats.Enabled)
	assert.Equal(t, 4, stats.Size)
	assert.Equal(t, 100, stats.Capacity)
	assert.InDelta(t, 0.75, stats.HitRate, 1e-9)

	cleared, err := c.ClearCache(ctx, &embedding.ClearEmbeddingCacheRequest{All: true})
	require.NoError(t, err)
	assert.Equal(t, 4, cleared.EntriesCleared)
	assert.True(t, fake.cleared)
}

func TestEmbeddedEmbedding_ProviderErrorPropagates(t *testing.T) {
	ctx := context.Background()
	c := embedding.NewClient(newAITransport(t, nil, &fakeEmbedding{fail: true}))

	_, err := c.GenerateEmbedding(ctx, &embedding.GenerateEmbeddingRequest{Text: "abc"})
	assert.ErrorAs(t, err, &errProviderDown{})
	_, err = c.ListProviders(ctx, &embedding.ListProvidersRequest{})
	assert.ErrorAs(t, err, &errProviderDown{})
	_, err = c.GetCacheStats(ctx, &embedding.GetEmbeddingCacheStatsRequest{})
	assert.ErrorAs(t, err, &errProviderDown{})
	_, err = c.ClearCache(ctx, &embedding.ClearEmbeddingCacheRequest{All: true})
	assert.ErrorAs(t, err, &errProviderDown{})
}

func TestEmbeddedEmbedding_ServiceNotConfigured(t *testing.T) {
	tr := newAITransport(t, &fakeOllama{}, nil)
	for _, method := range embeddingMethods {
		_, err := tr.Request(context.Background(), embeddingServiceName, method, &embedding.ListProvidersRequest{})
		var notCfg *ServiceNotConfiguredError
		require.ErrorAs(t, err, &notCfg, method)
		assert.Equal(t, "Embedding", notCfg.Service)
	}
}

func TestEmbeddedEmbedding_InvalidRequestType(t *testing.T) {
	tr := newAITransport(t, nil, &fakeEmbedding{})
	_, err := tr.Request(context.Background(), embeddingServiceName, "GenerateEmbedding", &embedding.ListProvidersRequest{})
	var typeErr *InvalidRequestTypeError
	require.ErrorAs(t, err, &typeErr)
	assert.Equal(t, "*embedding.GenerateEmbeddingRequest", typeErr.ExpectedType)
}

// ollamaMethods and embeddingMethods are every verb the SDK clients send
// (sdk/go/client/ollama and sdk/go/client/embedding).
var (
	ollamaMethods = []string{
		"Embed", "EmbedWithModel", "EmbedBatch", "ListModels", "GetModelInfo", "Health", "IsAvailable",
	}
	embeddingMethods = []string{
		"GenerateEmbedding", "BatchGenerateEmbeddings", "ListProviders", "GetProviderInfo",
		"SetDefaultProvider", "GetCacheStats", "ClearCache",
	}
)

func TestEmbeddedOllamaEmbedding_EveryClientMethodIsRegistered(t *testing.T) {
	for _, m := range ollamaMethods {
		_, ok := unaryHandlers[ollamaServiceName+"/"+m]
		assert.True(t, ok, "%s/%s not registered", ollamaServiceName, m)
	}
	for _, m := range embeddingMethods {
		_, ok := unaryHandlers[embeddingServiceName+"/"+m]
		assert.True(t, ok, "%s/%s not registered", embeddingServiceName, m)
	}
}
