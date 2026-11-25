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

//go:build vector && qrdb_vector

package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	qrdbapimcp "github.com/jeremyhahn/go-qrdb/pkg/api/mcp"
	qrdbapirest "github.com/jeremyhahn/go-qrdb/pkg/api/rest"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	sdkmcp "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
	sdkrest "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/rest"
)

// qrdbRAGThreeParagraphs is three paragraphs, each shorter than the
// 60-byte chunk size, so the paragraph strategy keeps them apart.
const qrdbRAGThreeParagraphs = "Apples grow in the orchard.\n\nRivers carry the boats.\n\nMountains hold the snow."

// qrdbRAGResultTexts returns the chunk texts of a query response.
func qrdbRAGResultTexts(resp *qrdbtransport.RAGQueryResponse) []string {
	var texts []string
	for _, r := range resp.Results {
		texts = append(texts, r.ChunkText)
	}
	return texts
}

// qrdbRAGChunkKeys returns the chunk keys under prefix, without the
// document's ":meta" key.
func qrdbRAGChunkKeys(kv *fakeRAGKV, prefix string) []string {
	var chunks []string
	for _, k := range qrdbRAGTestKeys(kv, prefix) {
		if !strings.HasSuffix(k, ":meta") {
			chunks = append(chunks, k)
		}
	}
	return chunks
}

// qrdbRAGTestRegistry resolves every (tenant, database) to one KV
// servicer. The node binds its real registry; the test binds this one so
// go-qrdb's list reaches the same fake store the node's ingestor wrote.
type qrdbRAGTestRegistry struct {
	qrdbservice.Registry
	kv qrdbservice.KVServicer
}

func (r qrdbRAGTestRegistry) KVService(string, string) (qrdbservice.KVServicer, error) {
	return r.kv, nil
}

// qrdbRAGTestKVServicer serves the KV calls go-qrdb's scoped RAG store
// makes from the fake KV.
type qrdbRAGTestKVServicer struct {
	qrdbservice.KVServicer
	kv *fakeRAGKV
}

func (s qrdbRAGTestKVServicer) Get(ctx context.Context, key string) ([]byte, error) {
	return s.kv.Get(ctx, key)
}

func (s qrdbRAGTestKVServicer) Put(ctx context.Context, key string, value []byte) error {
	return s.kv.Put(ctx, key, value)
}

func (s qrdbRAGTestKVServicer) Delete(ctx context.Context, key string) error {
	return s.kv.Delete(ctx, key)
}

func (s qrdbRAGTestKVServicer) Scan(ctx context.Context, prefix string) (map[string][]byte, error) {
	return s.kv.Scan(ctx, prefix)
}

// qrdbRAGTestBindKV binds svc's scoped store to kv, for calls that carry
// no tenant (the go-qrdb servers here run without authentication).
func qrdbRAGTestBindKV(t *testing.T, svc qrdbservice.RAGServicer, kv *fakeRAGKV) {
	t.Helper()
	b, ok := svc.(qrdbservice.RAGRegistryBinder)
	require.True(t, ok, "the node's RAG service binds no registry")
	b.BindRegistry(qrdbRAGTestRegistry{kv: qrdbRAGTestKVServicer{kv: kv}}, qrdbservice.RAGUnauthenticatedTenant("tenant-rag"))
}

// TestQRDBRAG_SDKRESTThroughGoQRDBHandler drives the SDK's REST
// QRDBRAGClient through go-qrdb's own REST handler over the node's wrapped
// RAG service: ingest, query, a shrinking re-ingest, delete and list.
func TestQRDBRAG_SDKRESTThroughGoQRDBHandler(t *testing.T) {
	svc, vec, kv := qrdbRAGTestStores()
	qrdbRAGTestBindKV(t, svc, kv)
	srv := httptest.NewServer(qrdbapirest.NewServer(nil, nil, "", qrdbapirest.WithRAG(svc)).Handler())
	t.Cleanup(srv.Close)
	tr, err := sdkrest.New(&transport.Config{Address: srv.URL, MaxRetries: 0}, sdkrest.WithAuthToken("tok-rag"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })
	c := tr.QRDBRAG()
	ctx := context.Background()
	const docID = "doc 1/a"

	// ChunkMinSize 1 keeps the three short paragraphs apart, as go-qrdb's
	// default minimum does (the MCP test below records it).
	ing, err := c.RAGIngestDocument(ctx, &qrdbtransport.RAGIngestRequest{
		DocumentID: docID, Text: qrdbRAGThreeParagraphs, Namespace: "docs",
		ChunkStrategy: "paragraph", ChunkSize: 60, ChunkMinSize: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 3, ing.ChunkCount)

	q, err := c.RAGQuery(ctx, &qrdbtransport.RAGQueryRequest{Query: "Rivers", Namespace: "docs", TopK: 5})
	require.NoError(t, err)
	var found bool
	for _, r := range q.Results {
		if r.DocumentID == docID && strings.Contains(r.ChunkText, "Rivers carry the boats.") {
			found = true
		}
	}
	assert.True(t, found, "query results %v carry no paragraph-2 chunk of %q", qrdbRAGResultTexts(q), docID)

	// go-qrdb's REST handler starts from its default chunk config (overlap
	// 50, minimum 100) and overrides the fields the request sets. With the
	// fixed strategy, ChunkMinSize 1 cuts the text into 3 chunks and the
	// default minimum into 2. Only the minimum differs between these two
	// ingests, so the drop from 3 to 2 proves ChunkMinSize reached the
	// pipeline over REST, and the second, shrinking ingest drops chunk 2.
	ing, err = c.RAGIngestDocument(ctx, &qrdbtransport.RAGIngestRequest{
		DocumentID: docID, Text: qrdbRAGThreeParagraphs, Namespace: "docs",
		ChunkStrategy: "fixed", ChunkSize: 60, ChunkMinSize: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 3, ing.ChunkCount)
	ing, err = c.RAGIngestDocument(ctx, &qrdbtransport.RAGIngestRequest{
		DocumentID: docID, Text: qrdbRAGThreeParagraphs, Namespace: "docs",
		ChunkStrategy: "fixed", ChunkSize: 60,
	})
	require.NoError(t, err)
	require.Equal(t, 2, ing.ChunkCount)
	assert.ElementsMatch(t, []uint64{qrdbRAGChunkVectorID("docs", docID, 0), qrdbRAGChunkVectorID("docs", docID, 1)}, qrdbRAGTestVectorIDs(vec))
	assert.Len(t, qrdbRAGChunkKeys(kv, "rag:docs:"), 2)

	ing, err = c.RAGIngestDocument(ctx, &qrdbtransport.RAGIngestRequest{
		DocumentID: docID, Text: "Orchards replace everything.", Namespace: "docs",
		ChunkStrategy: "paragraph", ChunkSize: 60, ChunkMinSize: 1,
	})
	require.NoError(t, err)
	require.Equal(t, 1, ing.ChunkCount)
	assert.Equal(t, []uint64{qrdbRAGChunkVectorID("docs", docID, 0)}, qrdbRAGTestVectorIDs(vec))
	q, err = c.RAGQuery(ctx, &qrdbtransport.RAGQueryRequest{Query: "Rivers", Namespace: "docs", TopK: 5})
	require.NoError(t, err)
	for _, text := range qrdbRAGResultTexts(q) {
		assert.NotContains(t, text, "Rivers", "a stale chunk survived the shrinking re-ingest")
	}

	// The ID has a space and a slash: the delete only succeeds if the SDK's
	// query string reaches the real handler intact.
	del, err := c.RAGDeleteDocument(ctx, &qrdbtransport.RAGDeleteDocumentRequest{DocumentID: docID, Namespace: "docs"})
	require.NoError(t, err)
	assert.Equal(t, 1, del.ChunksRemoved)
	assert.Empty(t, qrdbRAGTestVectorIDs(vec))
	assert.Empty(t, qrdbRAGTestKeys(kv, "rag:docs:"))
	list, err := c.RAGListDocuments(ctx, &qrdbtransport.RAGListDocumentsRequest{Namespace: "docs"})
	require.NoError(t, err)
	assert.Empty(t, list.Documents)

	// A second delete finds nothing and must fail with the handler's 404,
	// not report 0 removed.
	_, err = c.RAGDeleteDocument(ctx, &qrdbtransport.RAGDeleteDocumentRequest{DocumentID: docID, Namespace: "docs"})
	require.Error(t, err)
	assert.True(t, sdkerrors.IsNotFound(err), "err = %v", err)
}

// TestQRDBRAG_SDKMCPThroughGoQRDBTools drives the SDK's MCP QRDBRAGClient
// through go-qrdb's own MCP RAG tools over the node's wrapped RAG service.
func TestQRDBRAG_SDKMCPThroughGoQRDBTools(t *testing.T) {
	svc, vec, kv := qrdbRAGTestStores()
	qrdbRAGTestBindKV(t, svc, kv)
	ms := qrdbapimcp.NewServer(nil, nil, "", qrdbapimcp.WithRAG(svc)).MCPServer()
	httpSrv := mcpserver.NewTestStreamableHTTPServer(ms)
	t.Cleanup(httpSrv.Close)
	tr, err := sdkmcp.New(&transport.Config{Address: httpSrv.URL, Timeout: 5 * time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })
	c := tr.QRDBRAG()
	ctx := context.Background()

	// go-qrdb's ingest tool parses no chunk_min_size, so the SDK refuses it
	// before any tool call.
	_, err = c.RAGIngestDocument(ctx, &qrdbtransport.RAGIngestRequest{
		DocumentID: "doc-1", Text: qrdbRAGThreeParagraphs, Namespace: "docs",
		ChunkStrategy: "paragraph", ChunkSize: 60, ChunkMinSize: 1,
	})
	var unsupported *sdkmcp.QRDBRAGUnsupportedFieldError
	require.True(t, errors.As(err, &unsupported), "err = %v", err)
	assert.Empty(t, qrdbRAGTestVectorIDs(vec))
	assert.Empty(t, qrdbRAGTestKeys(kv, "rag:"))

	// Over MCP the provider's default minimum chunk size decides the chunk
	// boundaries. The count the tool reports must match what was stored:
	// with go-qrdb v0.30.1-alpha's defaults the three paragraphs stay 3
	// chunks.
	ing, err := c.RAGIngestDocument(ctx, &qrdbtransport.RAGIngestRequest{
		DocumentID: "doc-1", Text: qrdbRAGThreeParagraphs, Namespace: "docs",
		ChunkStrategy: "paragraph", ChunkSize: 60,
	})
	require.NoError(t, err)
	require.Equal(t, 3, ing.ChunkCount)
	assert.Len(t, qrdbRAGTestVectorIDs(vec), ing.ChunkCount)
	assert.Len(t, qrdbRAGChunkKeys(kv, "rag:docs:doc-1:"), ing.ChunkCount)
	t.Logf("MCP ingest chunk count with the provider's default minimum: %d", ing.ChunkCount)

	q, err := c.RAGQuery(ctx, &qrdbtransport.RAGQueryRequest{Query: "Rivers", Namespace: "docs", TopK: 5})
	require.NoError(t, err)
	var found bool
	for _, r := range q.Results {
		if r.DocumentID == "doc-1" {
			found = true
		}
	}
	assert.True(t, found, "query results %v carry no chunk of doc-1", qrdbRAGResultTexts(q))

	del, err := c.RAGDeleteDocument(ctx, &qrdbtransport.RAGDeleteDocumentRequest{DocumentID: "doc-1", Namespace: "docs"})
	require.NoError(t, err)
	assert.Equal(t, ing.ChunkCount, del.ChunksRemoved)
	assert.Empty(t, qrdbRAGTestVectorIDs(vec))
	assert.Empty(t, qrdbRAGTestKeys(kv, "rag:docs:"))
	list, err := c.RAGListDocuments(ctx, &qrdbtransport.RAGListDocumentsRequest{Namespace: "docs"})
	require.NoError(t, err)
	assert.Empty(t, list.Documents)
}
