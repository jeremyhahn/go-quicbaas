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

package quicbaas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// serveQRDBEmbeddingMCP starts an MCP endpoint that refuses every request
// without "Authorization: Bearer <token>" and answers go-qrdb's three
// embedding tools in the shapes go-qrdb's MCP server emits. It returns the
// endpoint URL and the names of the tools called.
func serveQRDBEmbeddingMCP(t *testing.T, token string) (string, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var called []string
	answer := func(name string, v any) mcpserver.ToolHandlerFunc {
		return func(_ context.Context, _ mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			mu.Lock()
			called = append(called, name)
			mu.Unlock()
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			return mcpgo.NewToolResultText(string(b)), nil
		}
	}
	srv := mcpserver.NewMCPServer("qrdb-embedding", "1.0.0")
	srv.AddTool(mcpgo.NewTool("qrdb_embed_text"), answer("qrdb_embed_text", map[string]any{
		"Embeddings": [][]float32{{1, 2}}, "Model": "m", "Usage": map[string]any{"PromptTokens": 3, "TotalTokens": 4},
	}))
	srv.AddTool(mcpgo.NewTool("qrdb_batch_embed"), answer("qrdb_batch_embed", map[string]any{
		"Embeddings": [][]float32{{1, 2}, {3, 4}}, "Model": "m", "TotalUsage": map[string]any{"PromptTokens": 5, "TotalTokens": 6},
	}))
	srv.AddTool(mcpgo.NewTool("qrdb_list_embedding_models"), answer("qrdb_list_embedding_models", map[string]any{
		"Models": []map[string]any{{"Name": "m", "Dimensions": 2, "MaxTokens": 8}},
	}))
	h := mcpserver.NewStreamableHTTPServer(srv)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(ts.Close)
	return ts.URL, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), called...)
	}
}

func TestQRDBEmbedding_MCPSendsBearer(t *testing.T) {
	url, called := serveQRDBEmbeddingMCP(t, "t")

	c, err := NewClient(url, WithMCP(), WithAuthToken("t"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ec, err := c.AI().QRDBEmbedding()
	require.NoError(t, err)
	ctx := context.Background()

	resp, err := ec.Embed(ctx, &QRDBEmbedRequest{Texts: []string{"hi"}})
	require.NoError(t, err)
	assert.Equal(t, [][]float32{{1, 2}}, resp.Embeddings)
	assert.Equal(t, "m", resp.Model)
	require.NotNil(t, resp.Usage)
	assert.Equal(t, 4, resp.Usage.TotalTokens)

	batch, err := ec.BatchEmbed(ctx, &QRDBBatchEmbedRequest{Texts: []string{"a", "b"}})
	require.NoError(t, err)
	assert.Len(t, batch.Embeddings, 2)
	require.NotNil(t, batch.Usage)
	assert.Equal(t, 6, batch.Usage.TotalTokens)

	models, err := ec.ListEmbeddingModels(ctx, "")
	require.NoError(t, err)
	require.Len(t, models.Models, 1)
	assert.Equal(t, 2, models.Models[0].Dimensions)

	assert.Equal(t, []string{"qrdb_embed_text", "qrdb_batch_embed", "qrdb_list_embedding_models"}, called())
}
