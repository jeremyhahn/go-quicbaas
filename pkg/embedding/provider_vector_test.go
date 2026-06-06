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

// =============================================================================
// OpenAIProvider
// =============================================================================

func TestNewOpenAIProvider_Success(t *testing.T) {
	srv := openAIEmbeddingHandler(t, 4, nil)
	p, err := NewOpenAIProvider(Config{
		APIURL:     srv.URL,
		Model:      ModelTextEmbedding3Small,
		MaxRetries: 0,
	})
	require.NoError(t, err)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close() })

	assert.Equal(t, "openai", p.Name())
	assert.Equal(t, 8191, p.MaxTokens())
	// text-embedding-3-small has a known dimension of 1536 pre-seeded.
	assert.Equal(t, 1536, p.Dimensions())
}

func TestOpenAIProvider_Embed_Success(t *testing.T) {
	var gotAuth string
	var gotBody openAIRequestBody
	srv := openAIEmbeddingHandler(t, 4, func(req openAIRequestBody, auth string) {
		gotBody = req
		gotAuth = auth
	})
	p, err := NewOpenAIProvider(Config{APIURL: srv.URL, APIKey: "sk-test", Model: "custom-model", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	vec, err := p.Embed(context.Background(), "hello")
	require.NoError(t, err)
	require.Len(t, vec, 4)
	assert.Equal(t, []string{"hello"}, gotBody.Input)
	assert.Equal(t, "custom-model", gotBody.Model)
	assert.Equal(t, "Bearer sk-test", gotAuth)
}

func TestOpenAIProvider_EmbedBatch_Success(t *testing.T) {
	srv := openAIEmbeddingHandler(t, 3, nil)
	p, err := NewOpenAIProvider(Config{APIURL: srv.URL, Model: "custom-model", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	out, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Len(t, out[0], 3)
	assert.Len(t, out[1], 3)
}

func TestOpenAIProvider_Embed_HTTPError(t *testing.T) {
	// 400 is a permanent client error so the client does not retry (fast path).
	srv := statusServer(t, http.StatusBadRequest, "boom")
	p, err := NewOpenAIProvider(Config{APIURL: srv.URL, Model: "custom-model", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Embed(context.Background(), "hello")
	require.Error(t, err)
	var httpErr *qrdbsdk.EmbeddingHTTPError
	require.True(t, errors.As(err, &httpErr), "expected EmbeddingHTTPError, got %T", err)
	assert.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
}

func TestOpenAIProvider_EmbedBatch_HTTPError(t *testing.T) {
	srv := statusServer(t, http.StatusUnauthorized, "nope")
	p, err := NewOpenAIProvider(Config{APIURL: srv.URL, Model: "custom-model", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.Error(t, err)
	var httpErr *qrdbsdk.EmbeddingHTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusUnauthorized, httpErr.StatusCode)
}

// =============================================================================
// OllamaProvider
// =============================================================================

func TestNewOllamaProvider_Defaults(t *testing.T) {
	p, err := NewOllamaProvider(DefaultOllamaConfig())
	require.NoError(t, err)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close() })

	assert.Equal(t, "ollama", p.Name())
	assert.Equal(t, 8192, p.MaxTokens())
	// nomic-embed-text is a known model pre-seeded to 768 dimensions.
	assert.Equal(t, 768, p.Dimensions())
}

func TestOllamaProvider_Embed_Success(t *testing.T) {
	srv := openAIEmbeddingHandler(t, 5, nil)
	cfg := DefaultOllamaConfig()
	cfg.BaseURL = srv.URL
	cfg.MaxRetries = 0
	p, err := NewOllamaProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	vec, err := p.Embed(context.Background(), "hi")
	require.NoError(t, err)
	assert.Len(t, vec, 5)
}

func TestOllamaProvider_EmbedBatch_Success(t *testing.T) {
	srv := openAIEmbeddingHandler(t, 2, nil)
	cfg := DefaultOllamaConfig()
	cfg.BaseURL = srv.URL
	cfg.MaxRetries = 0
	p, err := NewOllamaProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	out, err := p.EmbedBatch(context.Background(), []string{"x", "y", "z"})
	require.NoError(t, err)
	assert.Len(t, out, 3)
}

func TestOllamaProvider_Embed_HTTPError(t *testing.T) {
	srv := statusServer(t, http.StatusForbidden, "denied")
	cfg := DefaultOllamaConfig()
	cfg.BaseURL = srv.URL
	cfg.MaxRetries = 0
	p, err := NewOllamaProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Embed(context.Background(), "hi")
	require.Error(t, err)
	var httpErr *qrdbsdk.EmbeddingHTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusForbidden, httpErr.StatusCode)
}

// =============================================================================
// LMStudioProvider
// =============================================================================

func TestNewLMStudioProvider_Defaults(t *testing.T) {
	p, err := NewLMStudioProvider(DefaultLMStudioConfig())
	require.NoError(t, err)
	require.NotNil(t, p)
	t.Cleanup(func() { _ = p.Close() })

	assert.Equal(t, "lmstudio", p.Name())
	assert.Equal(t, 8192, p.MaxTokens())
	// nomic-ai/nomic-embed-text-v1.5-GGUF is pre-seeded to 768 dimensions.
	assert.Equal(t, 768, p.Dimensions())
}

func TestLMStudioProvider_Embed_Success(t *testing.T) {
	srv := openAIEmbeddingHandler(t, 6, nil)
	cfg := DefaultLMStudioConfig()
	cfg.BaseURL = srv.URL
	cfg.MaxRetries = 0
	p, err := NewLMStudioProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	vec, err := p.Embed(context.Background(), "lmstudio")
	require.NoError(t, err)
	assert.Len(t, vec, 6)
}

func TestLMStudioProvider_EmbedBatch_Success(t *testing.T) {
	srv := openAIEmbeddingHandler(t, 2, nil)
	cfg := DefaultLMStudioConfig()
	cfg.BaseURL = srv.URL
	cfg.MaxRetries = 0
	p, err := NewLMStudioProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	out, err := p.EmbedBatch(context.Background(), []string{"p", "q"})
	require.NoError(t, err)
	assert.Len(t, out, 2)
}

func TestLMStudioProvider_Embed_HTTPError(t *testing.T) {
	srv := statusServer(t, http.StatusBadRequest, "bad")
	cfg := DefaultLMStudioConfig()
	cfg.BaseURL = srv.URL
	cfg.MaxRetries = 0
	p, err := NewLMStudioProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Embed(context.Background(), "bad")
	require.Error(t, err)
	var httpErr *qrdbsdk.EmbeddingHTTPError
	require.True(t, errors.As(err, &httpErr))
}

// =============================================================================
// VoyageProvider
// =============================================================================

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

func TestVoyageProvider_Embed_Success(t *testing.T) {
	var gotAuth string
	srv := voyageHandler(t, 8, func(auth string) { gotAuth = auth })
	p, err := NewVoyageProvider(Config{
		APIKey:     "voyage-key",
		APIURL:     srv.URL,
		Model:      "voyage-code-2",
		MaxRetries: 0,
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	assert.Equal(t, "voyage", p.Name())
	assert.Equal(t, 32000, p.MaxTokens())
	assert.Equal(t, 1536, p.Dimensions())

	vec, err := p.Embed(context.Background(), "voyage text")
	require.NoError(t, err)
	assert.Len(t, vec, 8)
	assert.Equal(t, "Bearer voyage-key", gotAuth)
}

func TestVoyageProvider_EmbedBatch_Success(t *testing.T) {
	srv := voyageHandler(t, 4, nil)
	p, err := NewVoyageProvider(Config{APIKey: "k", APIURL: srv.URL, Model: "voyage-2", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	out, err := p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Len(t, out[0], 4)
}

func TestVoyageProvider_Embed_HTTPError(t *testing.T) {
	// 400 is permanent for Voyage so no retry/backoff is performed.
	srv := statusServer(t, http.StatusBadRequest, `{"error":{"message":"nope"}}`)
	p, err := NewVoyageProvider(Config{APIKey: "k", APIURL: srv.URL, Model: "voyage-code-2", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.Embed(context.Background(), "x")
	require.Error(t, err)
	var httpErr *qrdbsdk.EmbeddingHTTPError
	require.True(t, errors.As(err, &httpErr))
	assert.Equal(t, http.StatusBadRequest, httpErr.StatusCode)
}

// =============================================================================
// EmbedBatch HTTP error paths (exercise the batch error branch in each wrapper)
// =============================================================================

func TestOllamaProvider_EmbedBatch_HTTPError(t *testing.T) {
	srv := statusServer(t, http.StatusBadRequest, "bad")
	cfg := DefaultOllamaConfig()
	cfg.BaseURL = srv.URL
	cfg.MaxRetries = 0
	p, err := NewOllamaProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.Error(t, err)
	var httpErr *qrdbsdk.EmbeddingHTTPError
	require.True(t, errors.As(err, &httpErr))
}

func TestLMStudioProvider_EmbedBatch_HTTPError(t *testing.T) {
	srv := statusServer(t, http.StatusBadRequest, "bad")
	cfg := DefaultLMStudioConfig()
	cfg.BaseURL = srv.URL
	cfg.MaxRetries = 0
	p, err := NewLMStudioProvider(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.Error(t, err)
	var httpErr *qrdbsdk.EmbeddingHTTPError
	require.True(t, errors.As(err, &httpErr))
}

func TestVoyageProvider_EmbedBatch_HTTPError(t *testing.T) {
	srv := statusServer(t, http.StatusBadRequest, `{"error":{"message":"nope"}}`)
	p, err := NewVoyageProvider(Config{APIKey: "k", APIURL: srv.URL, Model: "voyage-code-2", MaxRetries: 0})
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	_, err = p.EmbedBatch(context.Background(), []string{"a", "b"})
	require.Error(t, err)
	var httpErr *qrdbsdk.EmbeddingHTTPError
	require.True(t, errors.As(err, &httpErr))
}
