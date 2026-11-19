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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
