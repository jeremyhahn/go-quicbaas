// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build qrdb_vector

package quicbaas

import (
	"context"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"google.golang.org/grpc/metadata"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// The aliases below re-export go-qrdb's RAG service (ingest, query, delete
// and list documents) as the node serves it. They are built only under the
// qrdb_vector tag because go-qrdb builds its RAG client only there. Reach the
// service with QRDBRAG(c); Client.RAG() remains QuicBaaS's own RAG pipeline.

// QRDBRAGClient is a re-export of go-qrdb's RAGClient.
type QRDBRAGClient = qrdbtransport.RAGClient

// QRDBRAGIngestRequest is a re-export of go-qrdb's RAGIngestRequest.
type QRDBRAGIngestRequest = qrdbtransport.RAGIngestRequest

// QRDBRAGIngestResponse is a re-export of go-qrdb's RAGIngestResponse.
type QRDBRAGIngestResponse = qrdbtransport.RAGIngestResponse

// QRDBRAGQueryRequest is a re-export of go-qrdb's RAGQueryRequest.
type QRDBRAGQueryRequest = qrdbtransport.RAGQueryRequest

// QRDBRAGQueryResult is a re-export of go-qrdb's RAGQueryResult.
type QRDBRAGQueryResult = qrdbtransport.RAGQueryResult

// QRDBRAGQueryResponse is a re-export of go-qrdb's RAGQueryResponse.
type QRDBRAGQueryResponse = qrdbtransport.RAGQueryResponse

// QRDBRAGDeleteDocumentRequest is a re-export of go-qrdb's RAGDeleteDocumentRequest.
type QRDBRAGDeleteDocumentRequest = qrdbtransport.RAGDeleteDocumentRequest

// QRDBRAGDeleteDocumentResponse is a re-export of go-qrdb's RAGDeleteDocumentResponse.
type QRDBRAGDeleteDocumentResponse = qrdbtransport.RAGDeleteDocumentResponse

// QRDBRAGListDocumentsRequest is a re-export of go-qrdb's RAGListDocumentsRequest.
type QRDBRAGListDocumentsRequest = qrdbtransport.RAGListDocumentsRequest

// QRDBRAGDocumentInfo is a re-export of go-qrdb's RAGDocumentInfo.
type QRDBRAGDocumentInfo = qrdbtransport.RAGDocumentInfo

// QRDBRAGListDocumentsResponse is a re-export of go-qrdb's RAGListDocumentsResponse.
type QRDBRAGListDocumentsResponse = qrdbtransport.RAGListDocumentsResponse

// qrdbRAGTransport is a quicbaas transport that carries go-qrdb's RAG calls
// itself, with the platform credentials it was built with (REST, QUIC through
// its embedded REST transport, MCP).
type qrdbRAGTransport interface {
	QRDBRAG() qrdbtransport.RAGClient
}

// QRDBRAG returns go-qrdb's RAG service client for c, carrying the platform
// credentials c was built with. Over REST and QUIC it uses the SDK's REST
// transport, over MCP the SDK's MCP session, over gRPC and Unix go-qrdb's
// client with the WithAuthToken bearer as authorization metadata, and on the
// embedded transport the in-process RAG servicer. A transport without an
// authenticated RAG adapter is refused as invalid input.
func QRDBRAG(c Client) (QRDBRAGClient, error) {
	cc, ok := c.(*client)
	if !ok {
		return nil, sdkerrors.NewInvalidInput("QRDBRAG", "the client is not a quicbaas SDK client")
	}
	return cc.qrdbRAG()
}

// qrdbRAG dispatches go-qrdb's RAG service to the transport's own adapter
// when it has one, and otherwise to the qrdb sub-client on gRPC, Unix and
// embedded transports.
func (c *client) qrdbRAG() (QRDBRAGClient, error) {
	if c.qrdb.closed.Load() {
		return nil, errQRDBClientClosed()
	}
	embedded := c.opts != nil && c.opts.embeddedQRDB != nil
	if !embedded {
		if rt, ok := c.transport.(qrdbRAGTransport); ok {
			if rc := rt.QRDBRAG(); rc != nil {
				return rc, nil
			}
		}
		// Never fall through to go-qrdb's own REST or MCP sub-client: it
		// sends no platform bearer.
		if c.opts == nil || (c.opts.transportType != TransportGRPC &&
			c.opts.transportType != TransportUnix &&
			c.opts.transportType != TransportEmbedded) {
			return nil, sdkerrors.NewInvalidInput("QRDBRAG",
				"the selected transport has no authenticated go-qrdb RAG adapter")
		}
	}
	qc, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	if qc == nil {
		return nil, sdkerrors.NewUnavailable("QRDBRAG", "the qrdb client is not configured")
	}
	rc := qc.RAG()
	if rc == nil {
		return nil, sdkerrors.NewUnavailable("QRDBRAG", "the qrdb transport does not serve the RAG service")
	}
	if !embedded && c.opts.authToken != "" &&
		(c.opts.transportType == TransportGRPC || c.opts.transportType == TransportUnix) {
		return &qrdbBearerRAGClient{inner: rc, bearer: "Bearer " + c.opts.authToken}, nil
	}
	return rc, nil
}

// qrdbBearerRAGClient forwards to go-qrdb's RAG client and adds the platform
// bearer as outgoing gRPC authorization metadata on every call, the way the
// SDK's own gRPC transport does.
type qrdbBearerRAGClient struct {
	inner  QRDBRAGClient
	bearer string
}

var _ QRDBRAGClient = (*qrdbBearerRAGClient)(nil)

// RAGIngestDocument forwards to go-qrdb's RAGIngestDocument with the bearer attached.
func (b *qrdbBearerRAGClient) RAGIngestDocument(ctx context.Context, req *QRDBRAGIngestRequest) (*QRDBRAGIngestResponse, error) {
	return b.inner.RAGIngestDocument(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), req)
}

// RAGQuery forwards to go-qrdb's RAGQuery with the bearer attached.
func (b *qrdbBearerRAGClient) RAGQuery(ctx context.Context, req *QRDBRAGQueryRequest) (*QRDBRAGQueryResponse, error) {
	return b.inner.RAGQuery(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), req)
}

// RAGDeleteDocument forwards to go-qrdb's RAGDeleteDocument with the bearer attached.
func (b *qrdbBearerRAGClient) RAGDeleteDocument(ctx context.Context, req *QRDBRAGDeleteDocumentRequest) (*QRDBRAGDeleteDocumentResponse, error) {
	return b.inner.RAGDeleteDocument(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), req)
}

// RAGListDocuments forwards to go-qrdb's RAGListDocuments with the bearer attached.
func (b *qrdbBearerRAGClient) RAGListDocuments(ctx context.Context, req *QRDBRAGListDocumentsRequest) (*QRDBRAGListDocumentsResponse, error) {
	return b.inner.RAGListDocuments(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), req)
}
