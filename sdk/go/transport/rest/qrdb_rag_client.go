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

//go:build qrdb_vector

package rest

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// qrdbRAGOp names the RAG adapter in transport errors.
const qrdbRAGOp = "rest.QRDBRAGClient"

// QRDBRAGClient calls go-qrdb's RAG routes through this transport, so the
// platform bearer and headers ride every call. go-qrdb's own REST sub-client
// sets no Authorization header.
type QRDBRAGClient struct {
	t *Transport
}

var _ qrdbtransport.RAGClient = (*QRDBRAGClient)(nil)

// QRDBRAG returns go-qrdb's RAG client bound to this transport. The QUIC
// transport embeds this transport and so inherits it.
func (t *Transport) QRDBRAG() qrdbtransport.RAGClient {
	return &QRDBRAGClient{t: t}
}

// RAGIngestDocument chunks, embeds and stores a document.
func (c *QRDBRAGClient) RAGIngestDocument(ctx context.Context, req *qrdbtransport.RAGIngestRequest) (*qrdbtransport.RAGIngestResponse, error) {
	if req == nil {
		req = &qrdbtransport.RAGIngestRequest{}
	}
	var resp qrdbtransport.RAGIngestResponse
	if err := doJSONResult(ctx, c.t, qrdbRAGOp, http.MethodPost, "/v1/rag/ingest", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RAGQuery retrieves the chunks most relevant to a query.
func (c *QRDBRAGClient) RAGQuery(ctx context.Context, req *qrdbtransport.RAGQueryRequest) (*qrdbtransport.RAGQueryResponse, error) {
	if req == nil {
		req = &qrdbtransport.RAGQueryRequest{}
	}
	var resp qrdbtransport.RAGQueryResponse
	if err := doJSONResult(ctx, c.t, qrdbRAGOp, http.MethodPost, "/v1/rag/query", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RAGDeleteDocument removes a document from a namespace. The pinned go-qrdb
// handler reads the document id and namespace only from the query string, so
// both go there, URL-encoded; the JSON body go-qrdb's own SDK sends is kept.
func (c *QRDBRAGClient) RAGDeleteDocument(ctx context.Context, req *qrdbtransport.RAGDeleteDocumentRequest) (*qrdbtransport.RAGDeleteDocumentResponse, error) {
	if req == nil {
		req = &qrdbtransport.RAGDeleteDocumentRequest{}
	}
	q := url.Values{}
	if req.DocumentID != "" {
		q.Set("id", req.DocumentID)
	}
	if req.Namespace != "" {
		q.Set("namespace", req.Namespace)
	}
	path := "/v1/rag/documents/delete"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp qrdbtransport.RAGDeleteDocumentResponse
	if err := doJSONResult(ctx, c.t, qrdbRAGOp, http.MethodPost, path, req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// RAGListDocuments lists the documents of a namespace, one page at a time.
func (c *QRDBRAGClient) RAGListDocuments(ctx context.Context, req *qrdbtransport.RAGListDocumentsRequest) (*qrdbtransport.RAGListDocumentsResponse, error) {
	if req == nil {
		req = &qrdbtransport.RAGListDocumentsRequest{}
	}
	q := url.Values{}
	if req.Namespace != "" {
		q.Set("namespace", req.Namespace)
	}
	if req.Limit != 0 {
		q.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Offset != 0 {
		q.Set("offset", strconv.Itoa(req.Offset))
	}
	path := "/v1/rag/documents"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var resp qrdbtransport.RAGListDocumentsResponse
	if err := doJSONResult(ctx, c.t, qrdbRAGOp, http.MethodGet, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
