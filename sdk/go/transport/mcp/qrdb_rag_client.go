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
	"encoding/json"
	"fmt"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// QRDBRAGClient calls go-qrdb's RAG tools over this transport's MCP session,
// so every call carries the platform credentials the transport was built
// with.
type QRDBRAGClient struct {
	t *Transport
}

var _ qrdbtransport.RAGClient = (*QRDBRAGClient)(nil)

// QRDBRAG returns a go-qrdb RAG client bound to this transport.
func (t *Transport) QRDBRAG() qrdbtransport.RAGClient {
	return &QRDBRAGClient{t: t}
}

// call runs one go-qrdb tool the way the embedding client does.
func (c *QRDBRAGClient) call(ctx context.Context, op, tool string, args map[string]interface{}, out interface{}) error {
	return (&QRDBEmbeddingClient{t: c.t}).call(ctx, op, tool, args, out)
}

// encodeStringMap encodes a non-empty map as the JSON string go-qrdb's RAG
// tools take for metadata and filters.
// QRDBRAGUnsupportedFieldError reports a request field the go-qrdb MCP tool
// cannot carry. The client refuses the request before any tool call rather
// than drop the field; the gRPC, REST and embedded transports carry it.
type QRDBRAGUnsupportedFieldError struct {
	Op    string
	Field string
}

// Error implements error.
func (e *QRDBRAGUnsupportedFieldError) Error() string {
	return fmt.Sprintf("%s: %s is not supported by go-qrdb's qrdb_rag_ingest tool; use the gRPC, REST or embedded transport", e.Op, e.Field)
}

func encodeStringMap(args map[string]interface{}, key, op, what string, m map[string]string) error {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("%s: encode %s: %w", op, what, err)
	}
	args[key] = string(b)
	return nil
}

// RAGIngestDocument chunks, embeds and stores a document with go-qrdb's
// qrdb_rag_ingest tool.
func (c *QRDBRAGClient) RAGIngestDocument(ctx context.Context, req *qrdbtransport.RAGIngestRequest) (*qrdbtransport.RAGIngestResponse, error) {
	const op = "mcp.QRDBRAG.IngestDocument"
	if req == nil {
		req = &qrdbtransport.RAGIngestRequest{}
	}
	// go-qrdb's qrdb_rag_ingest tool parses no chunk_min_size, so a
	// forwarded value would be dropped silently. Refuse it instead.
	if req.ChunkMinSize != 0 {
		return nil, &QRDBRAGUnsupportedFieldError{Op: op, Field: "chunk_min_size"}
	}
	args := map[string]interface{}{
		"document_id": req.DocumentID,
		"text":        req.Text,
		"namespace":   req.Namespace,
	}
	if req.Model != "" {
		args["model"] = req.Model
	}
	if req.Provider != "" {
		args["provider"] = req.Provider
	}
	if req.ChunkStrategy != "" {
		args["chunk_strategy"] = req.ChunkStrategy
	}
	if req.ChunkSize != 0 {
		args["chunk_size"] = req.ChunkSize
	}
	if req.ChunkOverlap != 0 {
		args["chunk_overlap"] = req.ChunkOverlap
	}
	if err := encodeStringMap(args, "metadata", op, "metadata", req.Metadata); err != nil {
		return nil, err
	}
	var wire struct {
		DocumentID string
		ChunkCount int
		Namespace  string
	}
	if err := c.call(ctx, op, "qrdb_rag_ingest", args, &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.RAGIngestResponse{
		DocumentID: wire.DocumentID,
		ChunkCount: wire.ChunkCount,
		Namespace:  wire.Namespace,
	}, nil
}

// RAGQuery retrieves the chunks most similar to a query with go-qrdb's
// qrdb_rag_query tool.
func (c *QRDBRAGClient) RAGQuery(ctx context.Context, req *qrdbtransport.RAGQueryRequest) (*qrdbtransport.RAGQueryResponse, error) {
	const op = "mcp.QRDBRAG.Query"
	if req == nil {
		req = &qrdbtransport.RAGQueryRequest{}
	}
	args := map[string]interface{}{
		"query":     req.Query,
		"namespace": req.Namespace,
	}
	if req.TopK != 0 {
		args["top_k"] = req.TopK
	}
	if req.Model != "" {
		args["model"] = req.Model
	}
	if req.Provider != "" {
		args["provider"] = req.Provider
	}
	if err := encodeStringMap(args, "filter", op, "filter", req.Filter); err != nil {
		return nil, err
	}
	var wire struct {
		Results []struct {
			ChunkText  string
			Score      float32
			DocumentID string
			ChunkIndex int
			Metadata   map[string]string
		}
	}
	if err := c.call(ctx, op, "qrdb_rag_query", args, &wire); err != nil {
		return nil, err
	}
	out := &qrdbtransport.RAGQueryResponse{Results: make([]*qrdbtransport.RAGQueryResult, 0, len(wire.Results))}
	for _, r := range wire.Results {
		out.Results = append(out.Results, &qrdbtransport.RAGQueryResult{
			ChunkText:  r.ChunkText,
			Score:      r.Score,
			DocumentID: r.DocumentID,
			ChunkIndex: r.ChunkIndex,
			Metadata:   r.Metadata,
		})
	}
	return out, nil
}

// RAGDeleteDocument removes a document with go-qrdb's
// qrdb_rag_delete_document tool.
func (c *QRDBRAGClient) RAGDeleteDocument(ctx context.Context, req *qrdbtransport.RAGDeleteDocumentRequest) (*qrdbtransport.RAGDeleteDocumentResponse, error) {
	const op = "mcp.QRDBRAG.DeleteDocument"
	if req == nil {
		req = &qrdbtransport.RAGDeleteDocumentRequest{}
	}
	args := map[string]interface{}{
		"document_id": req.DocumentID,
		"namespace":   req.Namespace,
	}
	var wire struct {
		DocumentID    string
		ChunksRemoved int
	}
	if err := c.call(ctx, op, "qrdb_rag_delete_document", args, &wire); err != nil {
		return nil, err
	}
	return &qrdbtransport.RAGDeleteDocumentResponse{
		DocumentID:    wire.DocumentID,
		ChunksRemoved: wire.ChunksRemoved,
	}, nil
}

// RAGListDocuments lists ingested documents with go-qrdb's
// qrdb_rag_list_documents tool.
func (c *QRDBRAGClient) RAGListDocuments(ctx context.Context, req *qrdbtransport.RAGListDocumentsRequest) (*qrdbtransport.RAGListDocumentsResponse, error) {
	const op = "mcp.QRDBRAG.ListDocuments"
	if req == nil {
		req = &qrdbtransport.RAGListDocumentsRequest{}
	}
	args := map[string]interface{}{}
	if req.Namespace != "" {
		args["namespace"] = req.Namespace
	}
	if req.Limit != 0 {
		args["limit"] = req.Limit
	}
	if req.Offset != 0 {
		args["offset"] = req.Offset
	}
	var wire struct {
		Documents []struct {
			ID         string
			Namespace  string
			ChunkCount int
			Metadata   map[string]string
		}
		Total int
	}
	if err := c.call(ctx, op, "qrdb_rag_list_documents", args, &wire); err != nil {
		return nil, err
	}
	out := &qrdbtransport.RAGListDocumentsResponse{
		Documents: make([]*qrdbtransport.RAGDocumentInfo, 0, len(wire.Documents)),
		Total:     wire.Total,
	}
	for _, d := range wire.Documents {
		out.Documents = append(out.Documents, &qrdbtransport.RAGDocumentInfo{
			ID:         d.ID,
			Namespace:  d.Namespace,
			ChunkCount: d.ChunkCount,
			Metadata:   d.Metadata,
		})
	}
	return out, nil
}
