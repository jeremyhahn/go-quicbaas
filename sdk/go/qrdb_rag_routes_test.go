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
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQRDBRAG_RESTCallsGoQRDBRoutes pins the routes QRDBRAG's REST client
// calls: go-qrdb's /v1/rag/*, not the QuicBaaS node's /api/v1/rag/*. A
// QuicBaaS node serves none of them (docs/rag/api.md, "go-qrdb's RAG
// service"), so QRDBRAG needs a go-qrdb node's address (SPEC row rag, #1672).
func TestQRDBRAG_RESTCallsGoQRDBRoutes(t *testing.T) {
	var (
		mu  sync.Mutex
		got []string
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/rag") { // not the connect-time /health
			mu.Lock()
			got = append(got, r.Method+" "+r.URL.Path)
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	t.Cleanup(srv.Close)

	roots := x509.NewCertPool()
	roots.AddCert(srv.Certificate())
	c, err := NewClient(strings.TrimPrefix(srv.URL, "https://"), WithREST(),
		WithTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}))
	require.NoError(t, err)
	rc, err := QRDBRAG(c)
	require.NoError(t, err)

	ctx := context.Background()
	_, _ = rc.RAGIngestDocument(ctx, &QRDBRAGIngestRequest{})
	_, _ = rc.RAGQuery(ctx, &QRDBRAGQueryRequest{})
	_, _ = rc.RAGDeleteDocument(ctx, &QRDBRAGDeleteDocumentRequest{})
	_, _ = rc.RAGListDocuments(ctx, &QRDBRAGListDocumentsRequest{})

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{
		"POST /v1/rag/ingest",
		"POST /v1/rag/query",
		"POST /v1/rag/documents/delete",
		"GET /v1/rag/documents",
	}, got)
}
