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

//go:build qrdb_vector

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

// newQRDBRAGTestTransport starts an mcp-go server with the given tools and
// returns an MCP transport connected to it.
func newQRDBRAGTestTransport(t *testing.T, tools map[string]mcpserver.ToolHandlerFunc) *Transport {
	t.Helper()
	srv := mcpserver.NewMCPServer("TestServer", "1.0.0")
	for name, h := range tools {
		srv.AddTool(mcpgo.NewTool(name), h)
	}
	httpSrv := mcpserver.NewTestStreamableHTTPServer(srv)
	t.Cleanup(httpSrv.Close)

	tr, err := New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// TestQRDBRAGClient_SendsGoQRDBArgsAndDecodesResults checks the argument
// names and encodings the client sends (metadata and filters as JSON
// strings) and that it decodes go-qrdb's PascalCase tool results.
func TestQRDBRAGClient_SendsGoQRDBArgsAndDecodesResults(t *testing.T) {
	args := map[string]map[string]any{}
	record := func(name, body string) mcpserver.ToolHandlerFunc {
		return func(_ context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			args[name] = req.GetArguments()
			return mcpgo.NewToolResultText(body), nil
		}
	}
	tr := newQRDBRAGTestTransport(t, map[string]mcpserver.ToolHandlerFunc{
		"qrdb_rag_ingest": record("qrdb_rag_ingest",
			`{"DocumentID":"d","ChunkCount":2,"Namespace":"ns"}`),
		"qrdb_rag_query": record("qrdb_rag_query",
			`{"Results":[{"ChunkText":"c","Score":0.5,"DocumentID":"d","ChunkIndex":1,"Metadata":{"k":"v"}}]}`),
		"qrdb_rag_delete_document": record("qrdb_rag_delete_document",
			`{"DocumentID":"d","ChunksRemoved":2}`),
		"qrdb_rag_list_documents": record("qrdb_rag_list_documents",
			`{"Documents":[{"ID":"d","Namespace":"ns","ChunkCount":2}],"Total":1}`),
	})
	rc := tr.QRDBRAG()
	ctx := context.Background()

	ir, err := rc.RAGIngestDocument(ctx, &qrdbtransport.RAGIngestRequest{
		DocumentID: "d", Text: "t", Namespace: "ns", Model: "m", ChunkSize: 100,
		Metadata: map[string]string{"k": "v"},
	})
	require.NoError(t, err)
	assert.Equal(t, &qrdbtransport.RAGIngestResponse{DocumentID: "d", ChunkCount: 2, Namespace: "ns"}, ir)
	assert.Equal(t, map[string]any{
		"document_id": "d", "text": "t", "namespace": "ns", "model": "m",
		"chunk_size": float64(100), "metadata": `{"k":"v"}`,
	}, args["qrdb_rag_ingest"])

	qr, err := rc.RAGQuery(ctx, &qrdbtransport.RAGQueryRequest{
		Query: "q", Namespace: "ns", TopK: 3, Filter: map[string]string{"k": "v"},
	})
	require.NoError(t, err)
	assert.Equal(t, &qrdbtransport.RAGQueryResponse{Results: []*qrdbtransport.RAGQueryResult{{
		ChunkText: "c", Score: 0.5, DocumentID: "d", ChunkIndex: 1, Metadata: map[string]string{"k": "v"},
	}}}, qr)
	assert.Equal(t, map[string]any{
		"query": "q", "namespace": "ns", "top_k": float64(3), "filter": `{"k":"v"}`,
	}, args["qrdb_rag_query"])

	dr, err := rc.RAGDeleteDocument(ctx, &qrdbtransport.RAGDeleteDocumentRequest{DocumentID: "d", Namespace: "ns"})
	require.NoError(t, err)
	assert.Equal(t, &qrdbtransport.RAGDeleteDocumentResponse{DocumentID: "d", ChunksRemoved: 2}, dr)
	assert.Equal(t, map[string]any{"document_id": "d", "namespace": "ns"}, args["qrdb_rag_delete_document"])

	lr, err := rc.RAGListDocuments(ctx, &qrdbtransport.RAGListDocumentsRequest{Namespace: "ns", Limit: 5})
	require.NoError(t, err)
	assert.Equal(t, &qrdbtransport.RAGListDocumentsResponse{
		Documents: []*qrdbtransport.RAGDocumentInfo{{ID: "d", Namespace: "ns", ChunkCount: 2}},
		Total:     1,
	}, lr)
	assert.Equal(t, map[string]any{"namespace": "ns", "limit": float64(5)}, args["qrdb_rag_list_documents"])

	_, err = rc.RAGListDocuments(ctx, &qrdbtransport.RAGListDocumentsRequest{})
	require.NoError(t, err)
	assert.Empty(t, args["qrdb_rag_list_documents"])

	require.NoError(t, tr.Close())
	_, err = rc.RAGQuery(ctx, &qrdbtransport.RAGQueryRequest{Query: "q"})
	assert.Error(t, err)
}

// TestQRDBRAGClient_ToolErrorSurfaces checks that a go-qrdb tool error comes
// back as an error, not as an empty response.
func TestQRDBRAGClient_ToolErrorSurfaces(t *testing.T) {
	tr := newQRDBRAGTestTransport(t, map[string]mcpserver.ToolHandlerFunc{
		"qrdb_rag_query": func(context.Context, mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			return mcpgo.NewToolResultError("denied"), nil
		},
	})
	resp, err := tr.QRDBRAG().RAGQuery(context.Background(), &qrdbtransport.RAGQueryRequest{Query: "q", Namespace: "ns"})
	assert.Error(t, err)
	assert.Nil(t, resp)
}

// TestQRDBRAGClient_TransportServesContract checks that the MCP transport
// serves go-qrdb's RAG service through the method the SDK root asserts.
func TestQRDBRAGClient_TransportServesContract(t *testing.T) {
	var _ interface {
		QRDBRAG() qrdbtransport.RAGClient
	} = (*Transport)(nil)

	tr, err := New(&transport.Config{Address: "http://127.0.0.1:1", Timeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })
	assert.NotNil(t, tr.QRDBRAG())
}
