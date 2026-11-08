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
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openAIRequestBody mirrors the OpenAI-compatible /v1/embeddings request body so
// tests can introspect what the providers actually transmit.
type openAIRequestBody struct {
	Input          []string `json:"input"`
	Model          string   `json:"model"`
	EncodingFormat string   `json:"encoding_format"`
}

// openAIEmbeddingHandler builds an httptest server that speaks the
// OpenAI-compatible /v1/embeddings (and /v1/models) protocol. It returns a
// deterministic embedding of the requested width for each input text. The
// optional capture closure receives the parsed request and the bearer token so
// individual tests can assert on transport details.
func openAIEmbeddingHandler(t *testing.T, dims int, capture func(req openAIRequestBody, auth string)) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		var body openAIRequestBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if capture != nil {
			capture(body, r.Header.Get("Authorization"))
		}

		data := make([]map[string]any, len(body.Input))
		for i := range body.Input {
			vec := make([]float32, dims)
			for j := range vec {
				vec[j] = float32(i*dims + j)
			}
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": vec}
		}
		resp := map[string]any{
			"object": "list",
			"data":   data,
			"model":  body.Model,
			"usage":  map[string]int{"prompt_tokens": 3, "total_tokens": 3},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, _ *http.Request) {
		resp := map[string]any{
			"object": "list",
			"data": []map[string]string{
				{"id": "model-a", "object": "model"},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// statusServer returns an httptest server that always answers with the given
// status code and body. It is used to exercise provider error paths.
func statusServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// voyageHandler builds an httptest server speaking the Voyage AI protocol.
func voyageHandler(t *testing.T, dims int, capture func(auth string)) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capture != nil {
			capture(r.Header.Get("Authorization"))
		}
		var body struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))

		data := make([]map[string]any, len(body.Input))
		for i := range body.Input {
			vec := make([]float32, dims)
			for j := range vec {
				vec[j] = float32(j)
			}
			data[i] = map[string]any{"object": "embedding", "index": i, "embedding": vec}
		}
		resp := map[string]any{
			"object": "list",
			"data":   data,
			"model":  body.Model,
			"usage":  map[string]int{"total_tokens": 7},
		}
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(resp))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// The constructors below point each provider at url with retries off, so an
// error status from the test server fails on the first request.

func newOpenAITestProvider(t *testing.T, url string) Provider {
	t.Helper()
	p, err := NewOpenAIProvider(Config{APIURL: url, APIKey: "sk-test", Model: "custom-model", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func newOllamaTestProvider(t *testing.T, url string) Provider {
	t.Helper()
	cfg := DefaultOllamaConfig()
	cfg.BaseURL = url
	cfg.MaxRetries = 0
	p, err := NewOllamaProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func newLMStudioTestProvider(t *testing.T, url string) Provider {
	t.Helper()
	cfg := DefaultLMStudioConfig()
	cfg.BaseURL = url
	cfg.MaxRetries = 0
	p, err := NewLMStudioProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func newVoyageTestProvider(t *testing.T, url string) Provider {
	t.Helper()
	p, err := NewVoyageProvider(Config{APIKey: "voyage-key", APIURL: url, Model: "voyage-code-2", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// =============================================================================
// Construction and metadata
// =============================================================================

func TestProviders_Metadata(t *testing.T) {
	tests := []struct {
		name          string
		newProvider   func() (Provider, error)
		wantMaxTokens int
		wantDims      int
	}{
		{
			// text-embedding-3-small has a known dimension of 1536 pre-seeded.
			name: "openai",
			newProvider: func() (Provider, error) {
				return NewOpenAIProvider(Config{Model: ModelTextEmbedding3Small, MaxRetries: 0})
			},
			wantMaxTokens: 8191,
			wantDims:      1536,
		},
		{
			// nomic-embed-text is a known model pre-seeded to 768 dimensions.
			name:          "ollama",
			newProvider:   func() (Provider, error) { return NewOllamaProvider(DefaultOllamaConfig()) },
			wantMaxTokens: 8192,
			wantDims:      768,
		},
		{
			// nomic-ai/nomic-embed-text-v1.5-GGUF is pre-seeded to 768 dimensions.
			name:          "lmstudio",
			newProvider:   func() (Provider, error) { return NewLMStudioProvider(DefaultLMStudioConfig()) },
			wantMaxTokens: 8192,
			wantDims:      768,
		},
		{
			name: "voyage",
			newProvider: func() (Provider, error) {
				return NewVoyageProvider(Config{APIKey: "k", Model: "voyage-code-2", MaxRetries: 0})
			},
			wantMaxTokens: 32000,
			wantDims:      1536,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := tc.newProvider()
			require.NoError(t, err)
			t.Cleanup(func() { _ = p.Close() })

			assert.Equal(t, tc.name, p.Name())
			assert.Equal(t, tc.wantMaxTokens, p.MaxTokens())
			assert.Equal(t, tc.wantDims, p.Dimensions())
		})
	}
}

func TestNewVoyageProvider_MissingAPIKey(t *testing.T) {
	_, err := NewVoyageProvider(Config{Model: "voyage-code-2"})
	require.Error(t, err)
	assert.ErrorIs(t, err, qrdbsdk.ErrEmbeddingAPIKeyRequired)
}

func TestNewVoyageProvider_UnknownModel(t *testing.T) {
	_, err := NewVoyageProvider(Config{APIKey: "key", Model: "not-a-real-model"})
	require.Error(t, err)
	var mnf *qrdbsdk.EmbeddingModelNotFoundError
	require.True(t, errors.As(err, &mnf), "expected ModelNotFoundError, got %T", err)
}

// =============================================================================
// Embed and EmbedBatch
// =============================================================================

// TestOpenAICompatibleProviders_Embed checks that each provider speaking the
// OpenAI-compatible protocol sends the input texts, model and credentials, and
// returns one vector per text from both Embed and EmbedBatch.
func TestOpenAICompatibleProviders_Embed(t *testing.T) {
	tests := []struct {
		name        string
		newProvider func(t *testing.T, url string) Provider
		wantModel   string
		wantAuth    string
	}{
		{name: "openai", newProvider: newOpenAITestProvider, wantModel: "custom-model", wantAuth: "Bearer sk-test"},
		{name: "ollama", newProvider: newOllamaTestProvider, wantModel: DefaultOllamaConfig().Model},
		{name: "lmstudio", newProvider: newLMStudioTestProvider, wantModel: DefaultLMStudioConfig().Model},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotAuth string
			var gotBody openAIRequestBody
			srv := openAIEmbeddingHandler(t, 4, func(req openAIRequestBody, auth string) {
				gotBody = req
				gotAuth = auth
			})
			p := tc.newProvider(t, srv.URL)

			vec, err := p.Embed(context.Background(), "hello")
			require.NoError(t, err)
			assert.Len(t, vec, 4)
			assert.Equal(t, []string{"hello"}, gotBody.Input)
			assert.Equal(t, tc.wantModel, gotBody.Model)
			assert.Equal(t, tc.wantAuth, gotAuth)

			out, err := p.EmbedBatch(context.Background(), []string{"a", "b", "c"})
			require.NoError(t, err)
			require.Len(t, out, 3)
			assert.Len(t, out[2], 4)
			assert.Equal(t, []string{"a", "b", "c"}, gotBody.Input)
		})
	}
}

func TestVoyageProvider_Embed(t *testing.T) {
	var gotAuth string
	srv := voyageHandler(t, 8, func(auth string) { gotAuth = auth })
	p := newVoyageTestProvider(t, srv.URL)

	vec, err := p.Embed(context.Background(), "voyage text")
	require.NoError(t, err)
	assert.Len(t, vec, 8)
	assert.Equal(t, "Bearer voyage-key", gotAuth)

	out, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Len(t, out[1], 8)
}

// TestProviders_HTTPError checks that an error status from the embedding
// server reaches the caller of Embed and EmbedBatch as an EmbeddingHTTPError
// carrying that status. A 4xx status is permanent, so nothing is retried.
func TestProviders_HTTPError(t *testing.T) {
	const voyageBody = `{"error":{"message":"nope"}}`
	tests := []struct {
		name        string
		newProvider func(t *testing.T, url string) Provider
		batch       bool
		status      int
		body        string
	}{
		{name: "openai/Embed", newProvider: newOpenAITestProvider, status: http.StatusBadRequest, body: "boom"},
		{name: "openai/EmbedBatch", newProvider: newOpenAITestProvider, batch: true, status: http.StatusUnauthorized, body: "nope"},
		{name: "ollama/Embed", newProvider: newOllamaTestProvider, status: http.StatusForbidden, body: "denied"},
		{name: "ollama/EmbedBatch", newProvider: newOllamaTestProvider, batch: true, status: http.StatusBadRequest, body: "bad"},
		{name: "lmstudio/Embed", newProvider: newLMStudioTestProvider, status: http.StatusBadRequest, body: "bad"},
		{name: "lmstudio/EmbedBatch", newProvider: newLMStudioTestProvider, batch: true, status: http.StatusBadRequest, body: "bad"},
		{name: "voyage/Embed", newProvider: newVoyageTestProvider, status: http.StatusBadRequest, body: voyageBody},
		{name: "voyage/EmbedBatch", newProvider: newVoyageTestProvider, batch: true, status: http.StatusBadRequest, body: voyageBody},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := statusServer(t, tc.status, tc.body)
			p := tc.newProvider(t, srv.URL)

			var err error
			if tc.batch {
				_, err = p.EmbedBatch(context.Background(), []string{"a", "b"})
			} else {
				_, err = p.Embed(context.Background(), "hello")
			}
			require.Error(t, err)
			var httpErr *qrdbsdk.EmbeddingHTTPError
			require.True(t, errors.As(err, &httpErr), "expected EmbeddingHTTPError, got %T", err)
			assert.Equal(t, tc.status, httpErr.StatusCode)
		})
	}
}
