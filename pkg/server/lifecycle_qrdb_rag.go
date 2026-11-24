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
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"sync/atomic"

	qrdbrag "github.com/jeremyhahn/go-qrdb/pkg/rag"
	"github.com/jeremyhahn/go-qrdb/pkg/rag/chunker"
	qrdbserver "github.com/jeremyhahn/go-qrdb/pkg/server"
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// qrdbRAGMaxDocSize is the largest document go-qrdb's RAG ingestor accepts;
// 0 is unlimited, go-qrdb's own default.
const qrdbRAGMaxDocSize = 0

// qrdbRAGServerOptions composes go-qrdb's RAG service into the node's server
// options, over the node's go-qrdb embedding service. The pipeline resolves
// its vector and KV stores through a RAGBinding the go-qrdb server binds to
// the node's per-(tenant, database) registry at start, so every document a
// caller ingests or queries is in the caller's own database. The service is
// created once. It returns nil when no go-qrdb embedding service is composed,
// so it must run after qrdbEmbeddingServerOptions.
func (s *Server) qrdbRAGServerOptions() []qrdbsdk.ServerOption {
	if s.qrdbEmbeddingSvc == nil {
		return nil
	}
	if s.qrdbRAGSvc == nil {
		s.qrdbRAGSvc = newQRDBRAGService(qrdbservice.NewEmbedProviderAdapter(s.qrdbEmbeddingSvc), qrdbservice.NewRAGBinding())
	}
	return []qrdbsdk.ServerOption{qrdbserver.WithRAG(s.qrdbRAGSvc)}
}

// newQRDBRAGService builds go-qrdb's RAG service over emb, with its vector
// and KV stores scoped through binding.
func newQRDBRAGService(emb qrdbrag.EmbedProvider, binding *qrdbservice.RAGBinding) qrdbservice.RAGServicer {
	vec := qrdbservice.NewScopedVectorStoreAdapter(binding)
	store := qrdbservice.NewScopedRAGStore(binding)
	return newQRDBRAGServiceOver(emb, vec, store, store, qrdbRAGScopedVectorDeleter{binding: binding})
}

// newQRDBRAGServiceOver builds go-qrdb's RAG service from its pipeline
// stores; store is the service's document store, kv the pipeline's, and
// vectors removes the pipeline's chunk vectors. The result wraps go-qrdb's
// service with a complete delete, and its ingestor removes the chunks a
// shrinking re-ingest leaves behind.
func newQRDBRAGServiceOver(emb qrdbrag.EmbedProvider, vec qrdbrag.VectorStore, kv qrdbrag.KVStore, store *qrdbservice.ScopedRAGStore, vectors qrdbRAGVectorDeleter) qrdbservice.RAGServicer {
	return &qrdbRAGService{
		RAGServicer: qrdbservice.NewRAGService(
			&qrdbRAGIngestor{in: qrdbrag.NewIngestor(emb, vec, kv, qrdbRAGMaxDocSize), kv: kv, vectors: vectors},
			&qrdbRAGRetriever{r: qrdbrag.NewRetriever(emb, vec, kv)},
			store,
		),
		kv:      kv,
		vectors: vectors,
	}
}

// qrdbRAGChunkKey mirrors go-qrdb's unexported rag.chunkKVKey (and
// service.ragChunkKey): the KV key of chunk i of a document.
func qrdbRAGChunkKey(namespace, docID string, i int) string {
	return "rag:" + namespace + ":" + docID + ":" + strconv.Itoa(i)
}

// qrdbRAGDocMetaKey mirrors go-qrdb's unexported rag.docMetaKVKey (and
// service.ragDocMetaKey): the KV key of a document's metadata.
func qrdbRAGDocMetaKey(namespace, docID string) string {
	return "rag:" + namespace + ":" + docID + ":meta"
}

// qrdbRAGChunkVectorID mirrors go-qrdb's unexported rag.chunkVectorID: the
// vector ID of chunk i of a document, FNV-64a over the namespace, the
// document ID and the ordinal, NUL-separated.
func qrdbRAGChunkVectorID(namespace, docID string, i int) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(namespace))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(docID))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strconv.Itoa(i)))
	return h.Sum64()
}

// qrdbRAGStoredChunkCount reads a document's ChunkCount from its metadata.
// found is false when the metadata cannot be read or is empty; err reports
// metadata that is present but cannot be decoded.
func qrdbRAGStoredChunkCount(ctx context.Context, kv qrdbrag.KVStore, namespace, docID string) (count int, found bool, err error) {
	b, getErr := kv.Get(ctx, qrdbRAGDocMetaKey(namespace, docID))
	if getErr != nil || len(b) == 0 {
		return 0, false, nil
	}
	var meta struct {
		ChunkCount int `json:"ChunkCount"`
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return 0, true, err
	}
	return meta.ChunkCount, true, nil
}

// qrdbRAGRemoveChunks removes the vectors, then the chunk KV entries, of a
// document's ordinals in [from, to). The first failure is returned.
func qrdbRAGRemoveChunks(ctx context.Context, kv qrdbrag.KVStore, vectors qrdbRAGVectorDeleter, namespace, docID string, from, to int) error {
	if from >= to {
		return nil
	}
	ids := make([]uint64, 0, to-from)
	for i := from; i < to; i++ {
		ids = append(ids, qrdbRAGChunkVectorID(namespace, docID, i))
	}
	if err := vectors.DeleteVectors(ctx, namespace, ids); err != nil {
		return err
	}
	for i := from; i < to; i++ {
		if err := kv.Delete(ctx, qrdbRAGChunkKey(namespace, docID, i)); err != nil {
			return fmt.Errorf("delete chunk %d: %w", i, err)
		}
	}
	return nil
}

// qrdbRAGVectorDeleter removes a document's chunk vectors. A vector that is
// already gone is not a failure, so a retry after a partial delete finishes.
type qrdbRAGVectorDeleter interface {
	DeleteVectors(ctx context.Context, namespace string, ids []uint64) error
}

// qrdbRAGVectorDeleteError reports chunk vectors the vector service could
// not remove.
type qrdbRAGVectorDeleteError struct {
	Namespace string
	FailedIDs []uint64
	Cause     error
}

func (e *qrdbRAGVectorDeleteError) Error() string {
	return fmt.Sprintf("delete %d RAG chunk vector(s) in namespace %q: %v", len(e.FailedIDs), e.Namespace, e.Cause)
}

func (e *qrdbRAGVectorDeleteError) Unwrap() error { return e.Cause }

// qrdbRAGScopedVectorDeleter removes chunk vectors through the vector
// service the RAG binding resolves for the caller's database.
type qrdbRAGScopedVectorDeleter struct {
	binding *qrdbservice.RAGBinding
}

var _ qrdbRAGVectorDeleter = qrdbRAGScopedVectorDeleter{}

// DeleteVectors batch-deletes ids. The batch reports a failed ID without its
// cause, so each failed ID is deleted again on its own: an ID the service
// reports as not found (or whose namespace is gone) is already removed.
// ShardID stays unset, as go-qrdb's VectorStoreAdapter.UpsertVector leaves it.
func (d qrdbRAGScopedVectorDeleter) DeleteVectors(ctx context.Context, namespace string, ids []uint64) error {
	if len(ids) == 0 {
		return nil
	}
	svc, err := d.binding.Vector(ctx)
	if err != nil {
		return err
	}
	resp, err := svc.BatchDeleteVectors(ctx, &qrdbservice.BatchDeleteVectorsRequest{Namespace: namespace, IDs: ids})
	if err != nil {
		if qrdbRAGVectorGone(err) {
			return nil
		}
		return err
	}
	if resp == nil || len(resp.FailedIDs) == 0 {
		return nil
	}
	var failed []uint64
	var cause error
	for _, id := range resp.FailedIDs {
		_, delErr := svc.DeleteVector(ctx, &qrdbservice.DeleteVectorRequest{Namespace: namespace, ID: id})
		if delErr == nil || qrdbRAGVectorGone(delErr) {
			continue
		}
		failed = append(failed, id)
		if cause == nil {
			cause = delErr
		}
	}
	if len(failed) == 0 {
		return nil
	}
	return &qrdbRAGVectorDeleteError{Namespace: namespace, FailedIDs: failed, Cause: cause}
}

// qrdbRAGVectorGone reports whether err says the vector, or its namespace,
// no longer exists.
func qrdbRAGVectorGone(err error) bool {
	var vecErr *qrdbservice.VectorNotFoundError
	var nsErr *qrdbservice.NamespaceNotFoundError
	return errors.As(err, &vecErr) || errors.As(err, &nsErr)
}

// qrdbRAGService wraps go-qrdb's RAG service. go-qrdb v0.30.1-alpha's
// DeleteDocument removes neither the chunk vectors nor reports a storage
// failure; the wrapper's DeleteDocument does both. Everything else is
// go-qrdb's.
type qrdbRAGService struct {
	qrdbservice.RAGServicer // go-qrdb's service: Ingest, Query, List, Close
	kv                      qrdbrag.KVStore
	vectors                 qrdbRAGVectorDeleter
	closed                  atomic.Bool
}

var (
	_ qrdbservice.RAGServicer       = (*qrdbRAGService)(nil)
	_ qrdbservice.RAGRegistryBinder = (*qrdbRAGService)(nil)
)

// BindRegistry forwards to go-qrdb's service, which binds the registry the
// stores resolve through. go-qrdb's server binds a RAG service only when it
// implements RAGRegistryBinder, so the wrapper must.
func (s *qrdbRAGService) BindRegistry(reg qrdbservice.Registry, opts ...qrdbservice.RAGBindOption) {
	if b, ok := s.RAGServicer.(qrdbservice.RAGRegistryBinder); ok {
		b.BindRegistry(reg, opts...)
	}
}

// Close closes go-qrdb's service and the wrapper's delete.
func (s *qrdbRAGService) Close() error {
	s.closed.Store(true)
	return s.RAGServicer.Close()
}

// DeleteDocument removes a document's chunk vectors, then its chunk entries,
// then its metadata. The metadata goes last, so a failure at any step leaves
// the document listed and a retry finishes the delete. Every failure is
// returned as a RAGDeleteError.
func (s *qrdbRAGService) DeleteDocument(ctx context.Context, req *qrdbservice.DeleteRAGDocumentRequest) (*qrdbservice.DeleteRAGDocumentResponse, error) {
	if s.closed.Load() {
		return nil, &qrdbservice.RAGServiceClosedError{}
	}
	if req == nil {
		req = &qrdbservice.DeleteRAGDocumentRequest{}
	}
	count, found, err := qrdbRAGStoredChunkCount(ctx, s.kv, req.Namespace, req.DocumentID)
	if !found {
		return nil, &qrdbservice.RAGDocumentNotFoundError{ID: req.DocumentID, Namespace: req.Namespace}
	}
	if err != nil {
		return nil, &qrdbservice.RAGDeleteError{ID: req.DocumentID, Cause: err}
	}
	if err := qrdbRAGRemoveChunks(ctx, s.kv, s.vectors, req.Namespace, req.DocumentID, 0, count); err != nil {
		return nil, &qrdbservice.RAGDeleteError{ID: req.DocumentID, Cause: err}
	}
	if err := s.kv.Delete(ctx, qrdbRAGDocMetaKey(req.Namespace, req.DocumentID)); err != nil {
		return nil, &qrdbservice.RAGDeleteError{ID: req.DocumentID, Cause: err}
	}
	return &qrdbservice.DeleteRAGDocumentResponse{DocumentID: req.DocumentID, ChunksRemoved: count}, nil
}

// qrdbRAGIngestor adapts go-qrdb's rag.Ingestor to the service layer's
// RAGIngestor. go-qrdb's Ingestor overwrites a document's chunks and count
// but keeps ordinals past the new count; the adapter removes them.
type qrdbRAGIngestor struct {
	in      *qrdbrag.Ingestor
	kv      qrdbrag.KVStore
	vectors qrdbRAGVectorDeleter
}

var _ qrdbservice.RAGIngestor = (*qrdbRAGIngestor)(nil)

// Ingest maps the service request onto the pipeline's, chunk configuration
// included, and the pipeline's response back.
func (a *qrdbRAGIngestor) Ingest(ctx context.Context, req *qrdbservice.IngestDocumentRequest) (*qrdbservice.RAGIngestResult, error) {
	ragReq := &qrdbrag.IngestRequest{
		Document: &qrdbrag.Document{
			ID:       req.DocumentID,
			Text:     req.Text,
			Metadata: req.Metadata,
		},
		Namespace: req.Namespace,
		Model:     req.Model,
		Provider:  req.Provider,
	}
	if req.ChunkConfig != nil {
		ragReq.ChunkConfig = &chunker.Config{
			Strategy:  req.ChunkConfig.Strategy,
			ChunkSize: req.ChunkConfig.ChunkSize,
			Overlap:   req.ChunkConfig.Overlap,
			MinSize:   req.ChunkConfig.MinSize,
		}
	}
	// Missing or unreadable metadata is a new document: nothing to replace.
	oldCount, _, metaErr := qrdbRAGStoredChunkCount(ctx, a.kv, req.Namespace, req.DocumentID)
	if metaErr != nil {
		oldCount = 0
	}
	resp, err := a.in.Ingest(ctx, ragReq)
	if err != nil {
		return nil, err
	}
	// The new chunks and count are written; drop the old ordinals past them.
	if err := qrdbRAGRemoveChunks(ctx, a.kv, a.vectors, req.Namespace, req.DocumentID, resp.ChunkCount, oldCount); err != nil {
		return nil, fmt.Errorf("document %q re-ingested, but its stale chunks were not removed: %w", req.DocumentID, err)
	}
	return &qrdbservice.RAGIngestResult{
		DocumentID: resp.DocumentID,
		ChunkCount: resp.ChunkCount,
		Namespace:  resp.Namespace,
	}, nil
}

// qrdbRAGRetriever adapts go-qrdb's rag.Retriever to the service layer's
// RAGRetriever.
type qrdbRAGRetriever struct {
	r *qrdbrag.Retriever
}

var _ qrdbservice.RAGRetriever = (*qrdbRAGRetriever)(nil)

// Retrieve maps the service query onto the pipeline's and each retrieved
// passage back.
func (a *qrdbRAGRetriever) Retrieve(ctx context.Context, req *qrdbservice.RAGQueryRequest) (*qrdbservice.RAGRetrieveResult, error) {
	resp, err := a.r.Retrieve(ctx, &qrdbrag.RetrieveRequest{
		Query:     req.Query,
		Namespace: req.Namespace,
		TopK:      req.TopK,
		Model:     req.Model,
		Provider:  req.Provider,
		Filter:    req.Filter,
	})
	if err != nil {
		return nil, err
	}
	results := make([]*qrdbservice.RAGRetrievalResult, len(resp.Results))
	for i, r := range resp.Results {
		results[i] = &qrdbservice.RAGRetrievalResult{
			ChunkText:  r.ChunkText,
			Score:      r.Score,
			DocumentID: r.DocumentID,
			ChunkIndex: r.ChunkIndex,
			Metadata:   r.Metadata,
		}
	}
	return &qrdbservice.RAGRetrieveResult{Results: results}, nil
}
