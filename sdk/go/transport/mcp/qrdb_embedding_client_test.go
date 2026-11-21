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

package mcp

import (
	"context"
	"testing"
	"time"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestQRDBEmbeddingClient_SendsGoQRDBArgsAndDecodesResults checks the
// argument names the client sends and that it decodes go-qrdb's PascalCase
// tool results, including the batch tool's TotalUsage.
func TestQRDBEmbeddingClient_SendsGoQRDBArgsAndDecodesResults(t *testing.T) {
	args := map[string]map[string]any{}
	record := func(name, body string) mcpserver.ToolHandlerFunc {
		return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			args[name] = req.GetArguments()
			return mcpgo.NewToolResultText(body), nil
		}
	}
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	srv.AddTool(mcpgo.NewTool("qrdb_embed_text"), record("qrdb_embed_text",
		`{"Embeddings":[[0.5]],"Model":"e","Cached":true,"Usage":{"PromptTokens":1,"TotalTokens":2}}`))
	srv.AddTool(mcpgo.NewTool("qrdb_batch_embed"), record("qrdb_batch_embed",
		`{"Embeddings":[[1],[2]],"Model":"b","TotalUsage":{"PromptTokens":3,"TotalTokens":4}}`))
	srv.AddTool(mcpgo.NewTool("qrdb_list_embedding_models"), record("qrdb_list_embedding_models",
		`{"Models":[{"Name":"x","Dimensions":3,"MaxTokens":9}]}`))
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })
	ec := tr.QRDBEmbedding()
	ctx := context.Background()

	er, err := ec.Embed(ctx, &qrdbtransport.EmbedRequest{Texts: []string{"a"}, Model: "e", Provider: "p"})
	require.NoError(t, err)
	assert.Equal(t, &qrdbtransport.EmbedResponse{
		Embeddings: [][]float32{{0.5}}, Model: "e", Cached: true,
		Usage: &qrdbtransport.UsageInfo{PromptTokens: 1, TotalTokens: 2},
	}, er)
	assert.Equal(t, map[string]any{"texts": []any{"a"}, "model": "e", "provider": "p"}, args["qrdb_embed_text"])

	br, err := ec.BatchEmbed(ctx, &qrdbtransport.BatchEmbedRequest{Texts: []string{"a", "b"}, BatchSize: 2})
	require.NoError(t, err)
	assert.Equal(t, &qrdbtransport.BatchEmbedResponse{
		Embeddings: [][]float32{{1}, {2}}, Model: "b",
		Usage: &qrdbtransport.UsageInfo{PromptTokens: 3, TotalTokens: 4},
	}, br)
	assert.Equal(t, map[string]any{"texts": []any{"a", "b"}, "batch_size": float64(2)}, args["qrdb_batch_embed"])

	lr, err := ec.ListEmbeddingModels(ctx, "p")
	require.NoError(t, err)
	assert.Equal(t, []qrdbtransport.EmbeddingModelInfo{{Name: "x", Dimensions: 3, MaxTokens: 9}}, lr.Models)
	assert.Equal(t, map[string]any{"provider": "p"}, args["qrdb_list_embedding_models"])

	require.NoError(t, tr.Close())
	_, err = ec.Embed(ctx, &qrdbtransport.EmbedRequest{Texts: []string{"a"}})
	assert.Error(t, err)
}
