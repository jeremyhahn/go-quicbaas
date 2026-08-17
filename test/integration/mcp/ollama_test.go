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

//go:build integration && ollama

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// testTimeout and httpClient are package-level: the bespoke MCP JSON-RPC
// listener's integration suite (authorization_test.go) that used to define
// them here was retired when quicbaas adopted go-qrdb's unified MCP server;
// this Ollama-embedding suite has no dependency on that listener, so it
// keeps its own minimal HTTP client.
var (
	testTimeout = 60 * time.Second
	httpClient  = helpers.NewHTTPClient(testTimeout)
)

var ollamaURL = getOllamaEnv("OLLAMA_URL", "http://ollama:11434")

func getOllamaEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// TestOllamaAvailability verifies Ollama is accessible for embedding tests.
func TestOllamaAvailability(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	tagsURL := ollamaURL + "/api/tags"

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	require.NoError(t, err)

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "Ollama service must be accessible for integration tests")
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode, "Ollama should be accessible")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	assert.Contains(t, string(body), "all-minilm", "all-minilm model should be available in Ollama")
}

// TestMCPEmbeddingWithOllama tests MCP embedding operations using Ollama.
func TestMCPEmbeddingWithOllama(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	tagsURL := ollamaURL + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, tagsURL, nil)
	require.NoError(t, err, "failed to create request for Ollama tags endpoint")

	resp, err := httpClient.Do(req)
	require.NoError(t, err, "Ollama service must be accessible for embedding tests")
	require.Equal(t, http.StatusOK, resp.StatusCode, "Ollama must return 200 OK for tags endpoint")
	resp.Body.Close()

	t.Run("generate_embedding_via_ollama", func(t *testing.T) {
		embedURL := ollamaURL + "/api/embeddings"

		embedReq := map[string]interface{}{
			"model":  "all-minilm",
			"prompt": "This is a test document for embedding",
		}

		reqBody, err := json.Marshal(embedReq)
		require.NoError(t, err)

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, embedURL, bytes.NewReader(reqBody))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode, "Ollama embedding should succeed")

		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)

		var embedResp map[string]interface{}
		err = json.Unmarshal(body, &embedResp)
		require.NoError(t, err)

		embedding, ok := embedResp["embedding"]
		assert.True(t, ok, "Response should include 'embedding' field")

		embeddingSlice, ok := embedding.([]interface{})
		assert.True(t, ok, "Embedding should be an array")
		assert.Equal(t, 384, len(embeddingSlice), "all-minilm produces 384-dimensional embeddings")
	})
}
