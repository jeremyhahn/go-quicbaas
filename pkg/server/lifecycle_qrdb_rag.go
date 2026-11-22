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
	return newQRDBRAGServiceOver(emb, vec, store, store)
}

// newQRDBRAGServiceOver builds go-qrdb's RAG service from its pipeline
// stores; store is the service's document store, kv the pipeline's.
func newQRDBRAGServiceOver(emb qrdbrag.EmbedProvider, vec qrdbrag.VectorStore, kv qrdbrag.KVStore, store *qrdbservice.ScopedRAGStore) qrdbservice.RAGServicer {
	return qrdbservice.NewRAGService(
		&qrdbRAGIngestor{in: qrdbrag.NewIngestor(emb, vec, kv, qrdbRAGMaxDocSize)},
		&qrdbRAGRetriever{r: qrdbrag.NewRetriever(emb, vec, kv)},
		store,
	)
}

// qrdbRAGIngestor adapts go-qrdb's rag.Ingestor to the service layer's
// RAGIngestor.
type qrdbRAGIngestor struct {
	in *qrdbrag.Ingestor
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
	resp, err := a.in.Ingest(ctx, ragReq)
	if err != nil {
		return nil, err
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
