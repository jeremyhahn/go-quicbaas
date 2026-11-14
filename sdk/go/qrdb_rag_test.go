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
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The re-exports are go-qrdb's own types, not copies: a value of one is a
// value of the other (SPEC row rag, #1672).
var (
	_ qrdbtransport.RAGClient                  = QRDBRAGClient(nil)
	_ *qrdbtransport.RAGIngestRequest          = (*QRDBRAGIngestRequest)(nil)
	_ *qrdbtransport.RAGIngestResponse         = (*QRDBRAGIngestResponse)(nil)
	_ *qrdbtransport.RAGQueryRequest           = (*QRDBRAGQueryRequest)(nil)
	_ *qrdbtransport.RAGQueryResult            = (*QRDBRAGQueryResult)(nil)
	_ *qrdbtransport.RAGQueryResponse          = (*QRDBRAGQueryResponse)(nil)
	_ *qrdbtransport.RAGDeleteDocumentRequest  = (*QRDBRAGDeleteDocumentRequest)(nil)
	_ *qrdbtransport.RAGDeleteDocumentResponse = (*QRDBRAGDeleteDocumentResponse)(nil)
	_ *qrdbtransport.RAGListDocumentsRequest   = (*QRDBRAGListDocumentsRequest)(nil)
	_ *qrdbtransport.RAGDocumentInfo           = (*QRDBRAGDocumentInfo)(nil)
	_ *qrdbtransport.RAGListDocumentsResponse  = (*QRDBRAGListDocumentsResponse)(nil)
)

func TestQRDBRAG_RESTClientCarriesGoQRDBRAG(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	rc, err := QRDBRAG(c)
	require.NoError(t, err)
	require.NotNil(t, rc)

	// It is go-qrdb's RAG service, not QuicBaaS's own pipeline (Client.RAG).
	_, own := rc.(RAGClient)
	assert.False(t, own, "QRDBRAG must not hand back QuicBaaS's own RAG client")
}

func TestQRDBRAG_PropagatesQRDBInitError(t *testing.T) {
	c, err := NewClient("localhost:8080", WithREST())
	require.NoError(t, err)

	// TransportEmbedded without WithEmbeddedQRDB cannot build the qrdb
	// sub-client; QRDBRAG must say so rather than return a nil client.
	cc := c.(*client)
	cc.qrdb = qrdbState{}
	cc.opts.transportType = TransportEmbedded
	cc.opts.embeddedQRDB = nil

	rc, err := QRDBRAG(c)
	require.Error(t, err)
	assert.Nil(t, rc)
}

// otherClient is a Client that is not the SDK's own, as a caller's wrapper
// or mock would be.
type otherClient struct{ Client }

func TestQRDBRAG_ForeignClientIsAnError(t *testing.T) {
	rc, err := QRDBRAG(otherClient{})
	require.Error(t, err)
	assert.Nil(t, rc)
}
