//go:build qrdb_vector

// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// go-qrdb's RAG service, re-exported (SPEC row rag, #1672). go-qrdb builds its
// RAG client only under the qrdb_vector tag, so this file does too.
//
// RAGClient (Client.RAG) remains QuicBaaS's own RAG pipeline, the parallel
// implementation the SPEC keeps until the owner retires it; QRDBRAGClient is
// go-qrdb's: ingest, query, delete and list over the qrdb sub-client's
// transport.

// QRDBRAGClient is a re-export of the qrdb RAGClient interface.
type QRDBRAGClient = qrdbtransport.RAGClient

// QRDBRAGIngestRequest is a re-export of the qrdb RAGIngestRequest type.
type QRDBRAGIngestRequest = qrdbtransport.RAGIngestRequest

// QRDBRAGIngestResponse is a re-export of the qrdb RAGIngestResponse type.
type QRDBRAGIngestResponse = qrdbtransport.RAGIngestResponse

// QRDBRAGQueryRequest is a re-export of the qrdb RAGQueryRequest type.
type QRDBRAGQueryRequest = qrdbtransport.RAGQueryRequest

// QRDBRAGQueryResult is a re-export of the qrdb RAGQueryResult type.
type QRDBRAGQueryResult = qrdbtransport.RAGQueryResult

// QRDBRAGQueryResponse is a re-export of the qrdb RAGQueryResponse type.
type QRDBRAGQueryResponse = qrdbtransport.RAGQueryResponse

// QRDBRAGDeleteDocumentRequest is a re-export of the qrdb RAGDeleteDocumentRequest type.
type QRDBRAGDeleteDocumentRequest = qrdbtransport.RAGDeleteDocumentRequest

// QRDBRAGDeleteDocumentResponse is a re-export of the qrdb RAGDeleteDocumentResponse type.
type QRDBRAGDeleteDocumentResponse = qrdbtransport.RAGDeleteDocumentResponse

// QRDBRAGListDocumentsRequest is a re-export of the qrdb RAGListDocumentsRequest type.
type QRDBRAGListDocumentsRequest = qrdbtransport.RAGListDocumentsRequest

// QRDBRAGDocumentInfo is a re-export of the qrdb RAGDocumentInfo type.
type QRDBRAGDocumentInfo = qrdbtransport.RAGDocumentInfo

// QRDBRAGListDocumentsResponse is a re-export of the qrdb RAGListDocumentsResponse type.
type QRDBRAGListDocumentsResponse = qrdbtransport.RAGListDocumentsResponse

// qrdbRAGProvider is the client method QRDBRAG reaches. It is not on the
// Client interface because that interface builds without qrdb_vector.
type qrdbRAGProvider interface {
	QRDBRAG() (QRDBRAGClient, error)
}

// QRDBRAG returns go-qrdb's RAG client for c, carried by the qrdb sub-client
// on the same transport and address as c. It fails when the qrdb sub-client
// cannot be built, or when its transport does not implement go-qrdb's RAG
// service.
func QRDBRAG(c Client) (QRDBRAGClient, error) {
	p, ok := c.(qrdbRAGProvider)
	if !ok {
		return nil, sdkerrors.NewInvalidInput("QRDBRAG", "the client does not carry a qrdb sub-client")
	}
	return p.QRDBRAG()
}

// QRDBRAG returns go-qrdb's RAG client over the qrdb sub-client's transport.
func (c *client) QRDBRAG() (QRDBRAGClient, error) {
	qc, err := c.qrdbClient()
	if err != nil {
		return nil, err
	}
	rc := qc.RAG()
	if rc == nil {
		return nil, sdkerrors.NewInvalidInput("QRDBRAG", "the qrdb transport does not implement go-qrdb's RAG service")
	}
	return rc, nil
}
