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
	"errors"
	"strings"
	"sync"
	"testing"

	qrdbrag "github.com/jeremyhahn/go-qrdb/pkg/rag"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUnifiedModuleServerOptions_ComposesQRDBRAG checks that the node
// composes go-qrdb's RAG service over its go-qrdb embedding service, once.
func TestUnifiedModuleServerOptions_ComposesQRDBRAG(t *testing.T) {
	srv := newModuleHostTestServer(t)

	first, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	require.NotNil(t, srv.GetQRDBEmbeddingServicer())
	svc := srv.GetQRDBRAGServicer()
	require.NotNil(t, svc)

	second, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	assert.Same(t, svc, srv.GetQRDBRAGServicer())
	assert.Equal(t, len(first), len(second))
}

// TestQRDBRAG_NilWithoutEmbedding checks that no go-qrdb RAG service is
// composed without a go-qrdb embedding service.
func TestQRDBRAG_NilWithoutEmbedding(t *testing.T) {
	srv := &Server{}
	assert.Nil(t, srv.qrdbRAGServerOptions())
	assert.Nil(t, srv.GetQRDBRAGServicer())
}

// TestQRDBRAG_UnboundRefuses checks that the composed service refuses a
// query before the go-qrdb server binds its registry, rather than panicking.
func TestQRDBRAG_UnboundRefuses(t *testing.T) {
	srv := newModuleHostTestServer(t)
	_, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	svc := srv.GetQRDBRAGServicer()
	require.NotNil(t, svc)

	require.NotPanics(t, func() {
		_, err = svc.Query(context.Background(), &qrdbservice.RAGQueryRequest{Query: "q", Namespace: "docs"})
	})
	assert.Error(t, err)
}

// TestQRDBRAG_AdaptersRoundTrip drives go-qrdb's RAG service, built from the
// node's ingestor and retriever adapters, over in-memory pipeline stores: a
// document ingested with a chunk configuration comes back from a query, and
// the model, provider, top-k and filter reach the pipeline.
func TestQRDBRAG_AdaptersRoundTrip(t *testing.T) {
	emb := &fakeRAGEmbedder{}
	vec := &fakeRAGVectors{vecs: map[uint64]fakeRAGVector{}}
	kv := &fakeRAGKV{m: map[string][]byte{}}
	svc := newQRDBRAGServiceOver(emb, vec, kv, qrdbservice.NewScopedRAGStore(qrdbservice.NewRAGBinding()), vec)
	ctx := context.Background()

	text := "The first paragraph is about apples and orchards.\n\nThe second paragraph is about rivers and boats."
	ing, err := svc.IngestDocument(ctx, &qrdbservice.IngestDocumentRequest{
		DocumentID: "doc-1",
		Text:       text,
		Namespace:  "docs",
		Model:      "m-1",
		Provider:   "p-1",
		Metadata:   map[string]string{"lang": "en"},
		ChunkConfig: &qrdbservice.ChunkConfig{
			Strategy:  "paragraph",
			ChunkSize: 60,
			MinSize:   1,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "doc-1", ing.DocumentID)
	assert.Equal(t, "docs", ing.Namespace)
	assert.Equal(t, 2, ing.ChunkCount, "the paragraph chunk configuration reaches the pipeline")
	assert.Len(t, vec.vecs, ing.ChunkCount, "the response reports the chunks the pipeline stored")

	resp, err := svc.Query(ctx, &qrdbservice.RAGQueryRequest{
		Query:     "apples",
		Namespace: "docs",
		TopK:      2,
		Model:     "m-2",
		Provider:  "p-2",
		Filter:    map[string]string{"lang": "en"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Results)
	var texts []string
	for _, r := range resp.Results {
		assert.Equal(t, "doc-1", r.DocumentID)
		texts = append(texts, r.ChunkText)
	}
	assert.Contains(t, strings.Join(texts, "\n"), "apples and orchards")

	emb.mu.Lock()
	assert.Contains(t, emb.calls, [2]string{"m-1", "p-1"}, "the ingest model and provider reach the embedder")
	assert.Contains(t, emb.calls, [2]string{"m-2", "p-2"}, "the query model and provider reach the embedder")
	emb.mu.Unlock()
	assert.Equal(t, 2, vec.lastK, "top-k reaches the vector search")
	assert.Equal(t, "en", vec.lastFilter["lang"], "the filter reaches the vector search")
}

// qrdbRAGTestStores builds the wrapped go-qrdb RAG service over fresh fakes.
func qrdbRAGTestStores() (qrdbservice.RAGServicer, *fakeRAGVectors, *fakeRAGKV) {
	vec := &fakeRAGVectors{vecs: map[uint64]fakeRAGVector{}}
	kv := &fakeRAGKV{m: map[string][]byte{}}
	svc := newQRDBRAGServiceOver(&fakeRAGEmbedder{}, vec, kv, qrdbservice.NewScopedRAGStore(qrdbservice.NewRAGBinding()), vec)
	return svc, vec, kv
}

// qrdbRAGTestIngest ingests text as doc-1 in namespace docs, one chunk per
// paragraph.
func qrdbRAGTestIngest(t *testing.T, svc qrdbservice.RAGServicer, text string) *qrdbservice.IngestDocumentResponse {
	t.Helper()
	ing, err := svc.IngestDocument(context.Background(), &qrdbservice.IngestDocumentRequest{
		DocumentID:  "doc-1",
		Text:        text,
		Namespace:   "docs",
		ChunkConfig: &qrdbservice.ChunkConfig{Strategy: "paragraph", ChunkSize: 60, MinSize: 1},
	})
	require.NoError(t, err)
	return ing
}

// qrdbRAGTestKeys lists the fake KV's keys under prefix.
func qrdbRAGTestKeys(kv *fakeRAGKV, prefix string) []string {
	kv.mu.Lock()
	defer kv.mu.Unlock()
	var keys []string
	for k := range kv.m {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys
}

// qrdbRAGTestVectorIDs lists the fake vector store's IDs.
func qrdbRAGTestVectorIDs(vec *fakeRAGVectors) []uint64 {
	vec.mu.Lock()
	defer vec.mu.Unlock()
	var ids []uint64
	for id := range vec.vecs {
		ids = append(ids, id)
	}
	return ids
}

const qrdbRAGTwoParagraphs = "The first paragraph is about apples and orchards.\n\nThe second paragraph is about rivers and boats."

// TestQRDBRAG_MirrorsMatchGoQRDBIngest pins the node's copies of go-qrdb's
// unexported chunk key, metadata key and vector ID formats to what go-qrdb's
// Ingestor writes; it fails if go-qrdb changes a format.
func TestQRDBRAG_MirrorsMatchGoQRDBIngest(t *testing.T) {
	svc, vec, kv := qrdbRAGTestStores()
	ing := qrdbRAGTestIngest(t, svc, qrdbRAGTwoParagraphs)
	require.Equal(t, 2, ing.ChunkCount)

	assert.ElementsMatch(t,
		[]uint64{qrdbRAGChunkVectorID("docs", "doc-1", 0), qrdbRAGChunkVectorID("docs", "doc-1", 1)},
		qrdbRAGTestVectorIDs(vec))
	keys := qrdbRAGTestKeys(kv, "")
	assert.Contains(t, keys, qrdbRAGChunkKey("docs", "doc-1", 0))
	assert.Contains(t, keys, qrdbRAGChunkKey("docs", "doc-1", 1))
	assert.Contains(t, keys, qrdbRAGDocMetaKey("docs", "doc-1"))
}

// TestQRDBRAG_DeleteRemovesVectorsAndChunks pins that a delete removes the
// document's vectors as well as its chunk entries and metadata.
func TestQRDBRAG_DeleteRemovesVectorsAndChunks(t *testing.T) {
	svc, vec, kv := qrdbRAGTestStores()
	ctx := context.Background()
	qrdbRAGTestIngest(t, svc, qrdbRAGTwoParagraphs)

	del, err := svc.DeleteDocument(ctx, &qrdbservice.DeleteRAGDocumentRequest{DocumentID: "doc-1", Namespace: "docs"})
	require.NoError(t, err)
	assert.Equal(t, "doc-1", del.DocumentID)
	assert.Equal(t, 2, del.ChunksRemoved)
	assert.Empty(t, qrdbRAGTestVectorIDs(vec))
	assert.Empty(t, qrdbRAGTestKeys(kv, "rag:docs:doc-1:"))

	resp, err := svc.Query(ctx, &qrdbservice.RAGQueryRequest{Query: "apples", Namespace: "docs", TopK: 5})
	require.NoError(t, err)
	assert.Empty(t, resp.Results)
}

// TestQRDBRAG_DeleteSurfacesStorageFailure pins that a failed chunk delete
// is reported, leaves the document's metadata for a retry, and that the
// retry finishes the delete.
func TestQRDBRAG_DeleteSurfacesStorageFailure(t *testing.T) {
	svc, vec, kv := qrdbRAGTestStores()
	ctx := context.Background()
	qrdbRAGTestIngest(t, svc, qrdbRAGTwoParagraphs)

	kv.mu.Lock()
	kv.failDelete = map[string]bool{qrdbRAGChunkKey("docs", "doc-1", 1): true}
	kv.mu.Unlock()
	_, err := svc.DeleteDocument(ctx, &qrdbservice.DeleteRAGDocumentRequest{DocumentID: "doc-1", Namespace: "docs"})
	require.Error(t, err)
	var delErr *qrdbservice.RAGDeleteError
	require.True(t, errors.As(err, &delErr), "got %T: %v", err, err)
	assert.Contains(t, qrdbRAGTestKeys(kv, ""), qrdbRAGDocMetaKey("docs", "doc-1"), "the metadata stays for a retry")

	kv.mu.Lock()
	kv.failDelete = nil
	kv.mu.Unlock()
	del, err := svc.DeleteDocument(ctx, &qrdbservice.DeleteRAGDocumentRequest{DocumentID: "doc-1", Namespace: "docs"})
	require.NoError(t, err)
	assert.Equal(t, 2, del.ChunksRemoved)
	assert.Empty(t, qrdbRAGTestVectorIDs(vec))
	assert.Empty(t, qrdbRAGTestKeys(kv, ""))
}

// TestQRDBRAG_DeleteUnknownDocumentIsNotFound pins the typed not-found error.
func TestQRDBRAG_DeleteUnknownDocumentIsNotFound(t *testing.T) {
	svc, _, _ := qrdbRAGTestStores()
	_, err := svc.DeleteDocument(context.Background(), &qrdbservice.DeleteRAGDocumentRequest{DocumentID: "nope", Namespace: "docs"})
	var nf *qrdbservice.RAGDocumentNotFoundError
	require.True(t, errors.As(err, &nf), "got %T: %v", err, err)
	assert.Equal(t, "nope", nf.ID)
}

// TestQRDBRAG_ShrinkingReingestDropsStaleChunks pins that re-ingesting a
// document as fewer chunks removes the old ordinals, so a query no longer
// returns replaced text and a later delete leaves nothing behind.
func TestQRDBRAG_ShrinkingReingestDropsStaleChunks(t *testing.T) {
	svc, vec, kv := qrdbRAGTestStores()
	ctx := context.Background()
	ing := qrdbRAGTestIngest(t, svc, "Old apples paragraph.\n\nOld rivers paragraph.\n\nOld mountains paragraph.")
	require.Equal(t, 3, ing.ChunkCount)
	ing = qrdbRAGTestIngest(t, svc, "New orchards paragraph.")
	require.Equal(t, 1, ing.ChunkCount)

	assert.Equal(t, []uint64{qrdbRAGChunkVectorID("docs", "doc-1", 0)}, qrdbRAGTestVectorIDs(vec))
	keys := qrdbRAGTestKeys(kv, "")
	assert.NotContains(t, keys, qrdbRAGChunkKey("docs", "doc-1", 1))
	assert.NotContains(t, keys, qrdbRAGChunkKey("docs", "doc-1", 2))

	resp, err := svc.Query(ctx, &qrdbservice.RAGQueryRequest{Query: "paragraph", Namespace: "docs", TopK: 5})
	require.NoError(t, err)
	require.Len(t, resp.Results, 1)
	assert.Contains(t, resp.Results[0].ChunkText, "New orchards")

	_, err = svc.DeleteDocument(ctx, &qrdbservice.DeleteRAGDocumentRequest{DocumentID: "doc-1", Namespace: "docs"})
	require.NoError(t, err)
	assert.Empty(t, qrdbRAGTestVectorIDs(vec))
	assert.Empty(t, qrdbRAGTestKeys(kv, ""))
}

// TestQRDBRAG_WrapperIsBound pins that the node's RAG service still
// implements RAGRegistryBinder, which go-qrdb's server requires to bind it.
func TestQRDBRAG_WrapperIsBound(t *testing.T) {
	srv := newModuleHostTestServer(t)
	_, err := srv.unifiedModuleServerOptions(nil, false)
	require.NoError(t, err)
	svc := srv.GetQRDBRAGServicer()
	require.NotNil(t, svc)
	_, ok := svc.(qrdbservice.RAGRegistryBinder)
	assert.True(t, ok, "%T does not implement RAGRegistryBinder", svc)
}

// fakeRAGEmbedder embeds every text as a fixed 3-d vector and records the
// model and provider of each call.
type fakeRAGEmbedder struct {
	mu    sync.Mutex
	calls [][2]string
}

var _ qrdbrag.EmbedProvider = (*fakeRAGEmbedder)(nil)

func (f *fakeRAGEmbedder) embed(texts []string, model, provider string) [][]float32 {
	f.mu.Lock()
	f.calls = append(f.calls, [2]string{model, provider})
	f.mu.Unlock()
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{1, 0, 0}
	}
	return out
}

func (f *fakeRAGEmbedder) Embed(_ context.Context, req *qrdbrag.EmbedRequest) (*qrdbrag.EmbedResponse, error) {
	return &qrdbrag.EmbedResponse{Embeddings: f.embed(req.Texts, req.Model, req.Provider)}, nil
}

func (f *fakeRAGEmbedder) BatchEmbed(_ context.Context, req *qrdbrag.BatchEmbedRequest) (*qrdbrag.BatchEmbedResponse, error) {
	return &qrdbrag.BatchEmbedResponse{Embeddings: f.embed(req.Texts, req.Model, req.Provider)}, nil
}

type fakeRAGVector struct {
	vec  []float32
	meta map[string]interface{}
}

// fakeRAGVectors is an in-memory vector store whose search returns every
// stored vector at distance 0 and records the last k and filter.
type fakeRAGVectors struct {
	mu         sync.Mutex
	vecs       map[uint64]fakeRAGVector
	lastK      int
	lastFilter map[string]interface{}
}

var _ qrdbrag.VectorStore = (*fakeRAGVectors)(nil)

func (f *fakeRAGVectors) EnsureNamespace(context.Context, string, int, string) error { return nil }

// DeleteVectors implements qrdbRAGVectorDeleter; a missing ID is not an error.
func (f *fakeRAGVectors) DeleteVectors(_ context.Context, _ string, ids []uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, id := range ids {
		delete(f.vecs, id)
	}
	return nil
}

func (f *fakeRAGVectors) UpsertVector(_ context.Context, req *qrdbrag.UpsertVecRequest) (*qrdbrag.UpsertVecResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.vecs[req.ID] = fakeRAGVector{vec: req.Vector, meta: req.Metadata}
	return &qrdbrag.UpsertVecResponse{Success: true}, nil
}

func (f *fakeRAGVectors) SearchSimilar(_ context.Context, req *qrdbrag.SearchVecRequest) (*qrdbrag.SearchVecResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastK = req.K
	f.lastFilter = req.MetadataFilter
	resp := &qrdbrag.SearchVecResponse{}
	for id, v := range f.vecs {
		resp.Results = append(resp.Results, &qrdbrag.VecSearchResult{ID: id, Metadata: v.meta})
	}
	return resp, nil
}

// fakeRAGKV is an in-memory KV store.
type fakeRAGKV struct {
	mu sync.Mutex
	m  map[string][]byte
	// failDelete makes Delete fail for these keys.
	failDelete map[string]bool
}

var _ qrdbrag.KVStore = (*fakeRAGKV)(nil)

func (f *fakeRAGKV) Get(_ context.Context, key string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.m[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (f *fakeRAGKV) Put(_ context.Context, key string, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.m[key] = append([]byte(nil), value...)
	return nil
}

func (f *fakeRAGKV) Delete(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failDelete[key] {
		return errors.New("fake kv: delete refused")
	}
	delete(f.m, key)
	return nil
}

func (f *fakeRAGKV) Scan(_ context.Context, prefix string) (map[string][]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string][]byte{}
	for k, v := range f.m {
		if strings.HasPrefix(k, prefix) {
			out[k] = v
		}
	}
	return out, nil
}
