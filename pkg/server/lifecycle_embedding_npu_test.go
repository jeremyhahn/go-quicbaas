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

// Scoped to the build where the real NPU provider exists. Under `vector` alone,
// stub.go supplies a stub whose constructor returns ErrVectorNotEnabled, so
// there would be nothing here to assert.
//go:build vector && qrdb_vector

package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/embedding"
)

// ollamaManagerConfig returns a config the qrdb manager accepts, so a manager
// exists to register other providers against.
//
// go-qrdb's validateManagerConfig allowlists only openai/ollama/lmstudio/voyage,
// so "npu" cannot yet be named as the manager's *default* provider -- see
// go-qrdb#528. It registers alongside the default instead, which is the intended
// arrangement regardless: Ollama stays the default until the owner switches it.
func ollamaManagerConfig() embedding.Config {
	return embedding.Config{
		Provider:       "ollama",
		APIURL:         "http://127.0.0.1:1",
		Model:          "nomic-embed-text",
		Dimensions:     768,
		Timeout:        1,
		MaxBatchSize:   128,
		CacheSize:      1000,
		CacheTTL:       3600,
		MaxRetries:     1,
		RetryBackoffMS: 100,
	}
}

// TestEmbeddingProviderFactoriesCoverEveryProvider guards the dispatch map
// against a provider that exists but was never wired in.
func TestEmbeddingProviderFactoriesCoverEveryProvider(t *testing.T) {
	for _, provider := range []string{
		"ollama",
		"openai",
		"voyage",
		"lmstudio",
		embedding.NPUProviderName,
	} {
		factory, ok := embeddingProviderFactories[provider]
		require.True(t, ok, "provider %q has no factory registered", provider)
		require.NotNil(t, factory, "provider %q has a nil factory", provider)
	}
}

// TestRegisterEmbeddingProviders_NPU registers the NPU provider against a
// manager. Construction makes no request to the sidecar, so the sidecar does not
// have to be running for the daemon to start.
func TestRegisterEmbeddingProviders_NPU(t *testing.T) {
	srv := withTempServer(t)

	mgr, err := embedding.NewManager(ollamaManagerConfig())
	require.NoError(t, err)

	npuCfg := ollamaManagerConfig()
	npuCfg.Provider = embedding.NPUProviderName
	npuCfg.APIURL = "http://127.0.0.1:8077"
	npuCfg.Model = "all-MiniLM-L6-v2"
	npuCfg.Dimensions = 384

	require.NoError(t, srv.registerEmbeddingProviders(mgr, &npuCfg),
		"the NPU provider must register even with the sidecar down")

	provider, err := mgr.GetProvider(embedding.NPUProviderName)
	require.NoError(t, err)
	require.Equal(t, embedding.NPUProviderName, provider.Name())
	require.Equal(t, 384, provider.Dimensions())
	// 512 is all-MiniLM-L6-v2's max_position_embeddings, not an NPU limit.
	require.Equal(t, 512, provider.MaxTokens())
}

// TestRegisterEmbeddingProviders_UnknownProvider covers the dispatch miss.
func TestRegisterEmbeddingProviders_UnknownProvider(t *testing.T) {
	srv := withTempServer(t)

	mgr, err := embedding.NewManager(ollamaManagerConfig())
	require.NoError(t, err)

	cfg := ollamaManagerConfig()
	cfg.Provider = "does-not-exist"

	err = srv.registerEmbeddingProviders(mgr, &cfg)
	require.Error(t, err)

	var unknown *api.UnknownProviderError
	require.True(t, errors.As(err, &unknown),
		"an unrecognised provider must surface UnknownProviderError, got %T", err)
	require.Equal(t, "does-not-exist", unknown.Provider)
}

// TestNewNPUEmbeddingProviderMapsConfig checks the config translation, since a
// dropped field would silently point the daemon at the wrong endpoint or index
// vectors of the wrong width.
func TestNewNPUEmbeddingProviderMapsConfig(t *testing.T) {
	cfg := embedding.Config{
		Provider:     embedding.NPUProviderName,
		APIURL:       "http://npu.internal:9000",
		Model:        "all-MiniLM-L6-v2",
		Dimensions:   384,
		Timeout:      45,
		MaxBatchSize: 16,
	}

	provider, err := newNPUEmbeddingProvider(context.Background(), &cfg)
	require.NoError(t, err)

	npu, ok := provider.(*embedding.NPUProvider)
	require.True(t, ok, "factory must return an *embedding.NPUProvider, got %T", provider)

	require.Equal(t, "http://npu.internal:9000", npu.BaseURL())
	require.Equal(t, 384, npu.Dimensions())
	require.Equal(t, 512, npu.MaxTokens())
	require.NoError(t, npu.Close())
}

// TestNewNPUEmbeddingProviderAppliesDefaults confirms an empty config yields the
// provider's own defaults rather than empty values.
// TestNewNPUEmbeddingProviderRefusesMissingEndpoint proves the server does not
// fall back to a hardcoded sidecar address when api_url is unset.
func TestNewNPUEmbeddingProviderRefusesMissingEndpoint(t *testing.T) {
	cfg := embedding.Config{Provider: embedding.NPUProviderName}

	provider, err := newNPUEmbeddingProvider(context.Background(), &cfg)
	if err == nil {
		npu, _ := provider.(*embedding.NPUProvider)
		if npu != nil {
			t.Cleanup(func() { _ = npu.Close() })
			t.Fatalf("newNPUEmbeddingProvider without api_url used %q, want a refusal", npu.BaseURL())
		}
	}
	require.ErrorIs(t, err, embedding.ErrNPUEndpointRequired)
}

func TestNewNPUEmbeddingProviderAppliesDefaults(t *testing.T) {
	cfg := embedding.Config{Provider: embedding.NPUProviderName, APIURL: "http://npu.test:8077"}

	provider, err := newNPUEmbeddingProvider(context.Background(), &cfg)
	require.NoError(t, err)

	npu, ok := provider.(*embedding.NPUProvider)
	require.True(t, ok)

	require.Equal(t, "http://npu.test:8077", npu.BaseURL())
	require.Equal(t, embedding.DefaultNPUDimensions, npu.Dimensions())
	require.Equal(t, embedding.DefaultNPUMaxTokens, npu.MaxTokens())
	require.NoError(t, npu.Close())
}

// TestNPUProviderReportsUnknownDeviceBeforeUse documents that the device is not
// probed at construction: the daemon must start whether or not the sidecar is up.
func TestNPUProviderReportsUnknownDeviceBeforeUse(t *testing.T) {
	cfg := embedding.DefaultNPUConfig()
	cfg.BaseURL = "http://npu.test:8077"
	cfg.Timeout = time.Second

	provider, err := embedding.NewNPUProvider(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, provider.Close()) })

	require.Equal(t, "unknown", provider.LastDevice(),
		"the device must not be claimed before a request has confirmed it")
}

// -----------------------------------------------------------------------------
// Additional providers: the path by which the NPU actually gets registered
// -----------------------------------------------------------------------------

// Only the manager's configured provider used to be registered, and go-qrdb
// refuses "npu" as that configured provider -- so the NPU provider existed in the
// binary and was unreachable at runtime, with per-request selection and
// SetDefaultProvider both failing with provider-not-found. These tests hold the
// `rag.embedding.additional` path to registering it.

func TestBuildAdditionalEmbeddingConfigsMapsEveryField(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Provider = "ollama"
	srv.config.RAG.Embedding.Additional = []config.EmbeddingConfig{
		{
			Provider:   embedding.NPUProviderName,
			APIURL:     "http://gpu-host.internal:8077",
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
			Timeout:    45,
		},
	}

	extras := srv.buildAdditionalEmbeddingConfigs()
	require.Len(t, extras, 1)

	got := extras[0]
	require.Equal(t, embedding.NPUProviderName, got.Provider)
	require.Equal(t, "http://gpu-host.internal:8077", got.APIURL)
	require.Equal(t, "all-MiniLM-L6-v2", got.Model)
	require.Equal(t, 384, got.Dimensions)
	require.Equal(t, 45, got.Timeout)
}

func TestBuildAdditionalEmbeddingConfigsSkipsNamelessEntries(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Additional = []config.EmbeddingConfig{
		{APIURL: "http://nowhere:1"},
		{Provider: embedding.NPUProviderName},
	}

	extras := srv.buildAdditionalEmbeddingConfigs()
	require.Len(t, extras, 1, "an entry with no provider name must be skipped, not registered")
	require.Equal(t, embedding.NPUProviderName, extras[0].Provider)
}

func TestBuildAdditionalEmbeddingConfigsIgnoredWhenRAGDisabled(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = false
	srv.config.RAG.Embedding.Additional = []config.EmbeddingConfig{
		{Provider: embedding.NPUProviderName},
	}

	require.Empty(t, srv.buildAdditionalEmbeddingConfigs())
}

// TestRegisterAdditionalEmbeddingProvidersRegistersTheNPU is the end of the
// chain: after startup, GetProvider("npu") must resolve.
func TestRegisterAdditionalEmbeddingProvidersRegistersTheNPU(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Provider = "ollama"
	srv.config.RAG.Embedding.Additional = []config.EmbeddingConfig{
		{
			Provider:   embedding.NPUProviderName,
			APIURL:     "http://127.0.0.1:8077",
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
		},
	}

	mgr, err := embedding.NewManager(ollamaManagerConfig())
	require.NoError(t, err)

	srv.registerAdditionalEmbeddingProviders(mgr)

	provider, err := mgr.GetProvider(embedding.NPUProviderName)
	require.NoError(t, err, "the NPU provider must be resolvable after startup")
	require.Equal(t, embedding.NPUProviderName, provider.Name())
	require.Equal(t, 384, provider.Dimensions())
}

// TestRegisterAdditionalEmbeddingProvidersSurvivesABadEntry keeps one broken
// extra provider from taking down the embedding subsystem.
func TestRegisterAdditionalEmbeddingProvidersSurvivesABadEntry(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Additional = []config.EmbeddingConfig{
		{Provider: "this-provider-does-not-exist"},
		{
			Provider:   embedding.NPUProviderName,
			APIURL:     "http://127.0.0.1:8077",
			Dimensions: 384,
		},
	}

	mgr, err := embedding.NewManager(ollamaManagerConfig())
	require.NoError(t, err)

	// Must not panic, and must keep going past the unknown provider.
	srv.registerAdditionalEmbeddingProviders(mgr)

	_, err = mgr.GetProvider(embedding.NPUProviderName)
	require.NoError(t, err, "a bad earlier entry must not prevent later ones registering")
}

// -----------------------------------------------------------------------------
// The real startup path
// -----------------------------------------------------------------------------

// TestStartEmbeddingRegistersTheNPUThroughTheRealPath exercises startEmbedding
// itself rather than the registration helper.
//
// That distinction is the whole point: the previous tests called
// registerEmbeddingProviders directly and so could not have caught the defect
// where startup registered only the configured default and the NPU was never
// reachable in production. This test goes through the function the daemon
// actually calls.
func TestStartEmbeddingRegistersTheNPUThroughTheRealPath(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding = config.EmbeddingConfig{
		Provider:   "ollama",
		APIURL:     "http://127.0.0.1:11434",
		Model:      "nomic-embed-text",
		Dimensions: 768,
		Timeout:    5,
		Additional: []config.EmbeddingConfig{{
			Provider:   embedding.NPUProviderName,
			APIURL:     "http://127.0.0.1:8077",
			Model:      "all-MiniLM-L6-v2",
			Dimensions: 384,
			Timeout:    5,
		}},
	}

	require.NoError(t, srv.startEmbedding())
	t.Cleanup(func() { require.NoError(t, srv.stopEmbedding()) })

	mgr := srv.GetEmbeddingManager()
	require.NotNil(t, mgr, "startEmbedding must leave a manager behind")

	// The configured default.
	ollama, err := mgr.GetProvider("ollama")
	require.NoError(t, err)
	require.Equal(t, "ollama", ollama.Name())

	// And the NPU, which is the thing that was previously unreachable.
	npu, err := mgr.GetProvider(embedding.NPUProviderName)
	require.NoError(t, err, "the NPU provider must be registered by the real startup path")
	require.Equal(t, embedding.NPUProviderName, npu.Name())
	require.Equal(t, 384, npu.Dimensions())
	require.Equal(t, 512, npu.MaxTokens())
}

// TestStartEmbeddingWithoutAdditionalLeavesOnlyTheDefault confirms the extra
// registration is opt-in and changes nothing when unused.
func TestStartEmbeddingWithoutAdditionalLeavesOnlyTheDefault(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding = config.EmbeddingConfig{
		Provider:   "ollama",
		APIURL:     "http://127.0.0.1:11434",
		Model:      "nomic-embed-text",
		Dimensions: 768,
		Timeout:    5,
	}

	require.NoError(t, srv.startEmbedding())
	t.Cleanup(func() { require.NoError(t, srv.stopEmbedding()) })

	mgr := srv.GetEmbeddingManager()
	require.NotNil(t, mgr)

	_, err := mgr.GetProvider(embedding.NPUProviderName)
	require.Error(t, err, "the NPU must not appear unless it was configured")
}

// TestStartEmbeddingRefusesNPUAsTheConfiguredDefault holds the preflight that
// turns a silent outage into a named one. go-qrdb rejects "npu" as a manager
// default, and startEmbedding treats manager failures as "embedding is
// optional", so without the preflight this config disabled embeddings while
// looking like an unreachable backend.
func TestStartEmbeddingRefusesNPUAsTheConfiguredDefault(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding = config.EmbeddingConfig{
		Provider:   embedding.NPUProviderName,
		APIURL:     "http://127.0.0.1:8077",
		Model:      "all-MiniLM-L6-v2",
		Dimensions: 384,
		Timeout:    5,
	}

	// Startup still succeeds -- embedding is optional -- but no manager is left.
	require.NoError(t, srv.startEmbedding())
	require.Nil(t, srv.GetEmbeddingManager(),
		"naming npu as the configured default must not half-initialise the subsystem")
}

// -----------------------------------------------------------------------------
// Omitted-dimension defaults
//
// An additional entry only has to name what differs from the norm, so most
// entries omit `dimensions`. What gets filled in then decides whether the
// provider works at all: the NPU cross-checks the sidecar's declared width
// against its own configured width and refuses every response that disagrees.
// A single global default is therefore wrong -- 768 suits the text providers,
// whose width follows the chosen model, but guarantees total failure for the
// NPU, whose model is fixed at 384.
// -----------------------------------------------------------------------------

// TestBuildAdditionalEmbeddingConfigsDefaultsNPUToItsNativeWidth is the
// regression test for the defect where an omitted width became 768 for every
// provider alike. 768 is not a width all-MiniLM-L6-v2 can ever produce, so the
// old default did not merely pick a suboptimal number -- it configured a
// provider that could not answer a single request.
func TestBuildAdditionalEmbeddingConfigsDefaultsNPUToItsNativeWidth(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Additional = []config.EmbeddingConfig{
		{Provider: embedding.NPUProviderName},
	}

	extras := srv.buildAdditionalEmbeddingConfigs()
	require.Len(t, extras, 1)
	require.Equal(t, 120, extras[0].Timeout, "an unset timeout must get a default")
	require.Equal(t, embedding.DefaultNPUDimensions, extras[0].Dimensions,
		"an omitted NPU width must become the model's own 384, not the generic 768")
}

// TestBuildAdditionalEmbeddingConfigsDefaultsTextProvidersTo768 pins the other
// half of the rule, so the fix for the NPU does not quietly change the width
// every other provider has always been given.
func TestBuildAdditionalEmbeddingConfigsDefaultsTextProvidersTo768(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Additional = []config.EmbeddingConfig{
		{Provider: "ollama", APIURL: "http://127.0.0.1:11434"},
		{Provider: "lmstudio", APIURL: "http://127.0.0.1:1234"},
		{Provider: "openai", APIKey: "k"},
		{Provider: "voyage", APIKey: "k"},
		{Provider: "some-provider-we-have-never-heard-of"},
	}

	extras := srv.buildAdditionalEmbeddingConfigs()
	require.Len(t, extras, 5)
	for _, extra := range extras {
		require.Equal(t, fallbackEmbeddingDimensions, extra.Dimensions,
			"provider %q has no fixed native width, so it keeps the generic default", extra.Provider)
	}
}

// TestBuildAdditionalEmbeddingConfigsHonoursAnExplicitWidth checks that the
// per-provider default only ever fills a gap. An operator who states a width
// has to get it, including for the NPU and including when it happens to equal
// the value the old bug produced.
func TestBuildAdditionalEmbeddingConfigsHonoursAnExplicitWidth(t *testing.T) {
	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding.Additional = []config.EmbeddingConfig{
		{Provider: embedding.NPUProviderName, Dimensions: 768},
		{Provider: "ollama", APIURL: "http://127.0.0.1:11434", Dimensions: 384},
	}

	extras := srv.buildAdditionalEmbeddingConfigs()
	require.Len(t, extras, 2)
	require.Equal(t, 768, extras[0].Dimensions, "an explicit NPU width must override its native default")
	require.Equal(t, 384, extras[1].Dimensions, "an explicit Ollama width must override the generic default")
}

// TestStartEmbeddingServesAnNPUEntryThatOmitsItsWidth is the test that would
// actually have caught this in production.
//
// Holding the right integer in a config struct is not the property that
// matters; answering a request is. This drives the daemon's own startEmbedding
// against a sidecar double that behaves exactly as the real one does -- it
// declares 384 and returns 384-wide vectors -- and asserts an embedding comes
// back. Under the old default the provider registered perfectly happily and
// then failed here with NPUDimensionsMismatchError, which is why a
// configuration-shaped assertion alone is not enough.
func TestStartEmbeddingServesAnNPUEntryThatOmitsItsWidth(t *testing.T) {
	var gotTexts []string
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"texts"`
			Model string   `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		gotTexts = req.Texts

		vectors := make([][]float32, len(req.Texts))
		for i := range vectors {
			vectors[i] = make([]float32, embedding.DefaultNPUDimensions)
			vectors[i][0] = 1
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
			"embeddings": vectors,
			"model":      "all-MiniLM-L6-v2",
			"dimensions": embedding.DefaultNPUDimensions,
			"device":     "NPU",
		}))
	}))
	defer sidecar.Close()

	srv := withTempServer(t)
	srv.config.RAG.Enabled = true
	srv.config.RAG.Embedding = config.EmbeddingConfig{
		Provider:   "ollama",
		APIURL:     "http://127.0.0.1:11434",
		Model:      "nomic-embed-text",
		Dimensions: 768,
		// Deliberately no Dimensions on the extra entry: that omission is the
		// whole subject of the test.
		Additional: []config.EmbeddingConfig{
			{
				Provider: embedding.NPUProviderName,
				APIURL:   sidecar.URL,
				Model:    "all-MiniLM-L6-v2",
			},
		},
	}

	require.NoError(t, srv.startEmbedding())

	provider, err := srv.embeddingManager.GetProvider(embedding.NPUProviderName)
	require.NoError(t, err, "the NPU provider must be registered by the real startup path")
	require.Equal(t, embedding.DefaultNPUDimensions, provider.Dimensions())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	vector, err := provider.Embed(ctx, "the padding is invariant")
	require.NoError(t, err, "an NPU entry that omits its width must still serve")
	require.Len(t, vector, embedding.DefaultNPUDimensions)
	require.Equal(t, []string{"the padding is invariant"}, gotTexts)
}
