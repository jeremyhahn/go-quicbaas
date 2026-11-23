//go:build qrdb_vector

package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestQRDBRAGClient_MethodPathAndBearer proves each RAG call reaches
// go-qrdb's route with the right method, path, query string and body, carries
// the platform bearer, and decodes the reply.
func TestQRDBRAGClient_MethodPathAndBearer(t *testing.T) {
	ctx := context.Background()
	const token = "tok-rag"
	meta := map[string]string{"source": "manual", "lang": "en"}

	type wantBody struct {
		DocumentID string            `json:"document_id"`
		Namespace  string            `json:"namespace"`
		Query      string            `json:"query"`
		TopK       int               `json:"top_k"`
		Metadata   map[string]string `json:"metadata"`
		Filter     map[string]string `json:"filter"`
	}

	ingestReply := &qrdbtransport.RAGIngestResponse{DocumentID: "doc-1", ChunkCount: 2, Namespace: "ns"}
	queryReply := &qrdbtransport.RAGQueryResponse{Results: []*qrdbtransport.RAGQueryResult{
		{ChunkText: "hello", Score: 0.5, DocumentID: "doc-1", ChunkIndex: 1, Metadata: map[string]string{"k": "v"}},
	}}
	deleteReply := &qrdbtransport.RAGDeleteDocumentResponse{DocumentID: "doc-1", ChunksRemoved: 2}
	listReply := &qrdbtransport.RAGListDocumentsResponse{
		Documents: []*qrdbtransport.RAGDocumentInfo{{ID: "doc-1", Namespace: "ns", ChunkCount: 2}},
		Total:     1,
	}

	cases := []struct {
		name     string
		method   string
		path     string
		rawQuery string
		reply    any
		body     *wantBody
		call     func(c qrdbtransport.RAGClient) (any, error)
	}{
		{
			name:   "Ingest",
			method: http.MethodPost,
			path:   "/v1/rag/ingest",
			reply:  ingestReply,
			body:   &wantBody{DocumentID: "doc-1", Namespace: "ns", Metadata: meta},
			call: func(c qrdbtransport.RAGClient) (any, error) {
				return c.RAGIngestDocument(ctx, &qrdbtransport.RAGIngestRequest{
					DocumentID: "doc-1", Text: "hello world", Namespace: "ns", Metadata: meta,
				})
			},
		},
		{
			name:   "Query",
			method: http.MethodPost,
			path:   "/v1/rag/query",
			reply:  queryReply,
			body:   &wantBody{Namespace: "ns", Query: "hello", TopK: 3, Filter: meta},
			call: func(c qrdbtransport.RAGClient) (any, error) {
				return c.RAGQuery(ctx, &qrdbtransport.RAGQueryRequest{
					Query: "hello", Namespace: "ns", TopK: 3, Filter: meta,
				})
			},
		},
		{
			name:   "Delete",
			method: http.MethodPost,
			path:   "/v1/rag/documents/delete",
			reply:  deleteReply,
			body:   &wantBody{DocumentID: "doc-1", Namespace: "ns"},
			call: func(c qrdbtransport.RAGClient) (any, error) {
				return c.RAGDeleteDocument(ctx, &qrdbtransport.RAGDeleteDocumentRequest{
					DocumentID: "doc-1", Namespace: "ns",
				})
			},
		},
		{
			name:     "List",
			method:   http.MethodGet,
			path:     "/v1/rag/documents",
			rawQuery: "limit=5&namespace=ns&offset=10",
			reply:    listReply,
			call: func(c qrdbtransport.RAGClient) (any, error) {
				return c.RAGListDocuments(ctx, &qrdbtransport.RAGListDocumentsRequest{
					Namespace: "ns", Limit: 5, Offset: 10,
				})
			},
		},
		{
			name:   "ListZeroRequest",
			method: http.MethodGet,
			path:   "/v1/rag/documents",
			reply:  listReply,
			call: func(c qrdbtransport.RAGClient) (any, error) {
				return c.RAGListDocuments(ctx, &qrdbtransport.RAGListDocumentsRequest{})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu                          sync.Mutex
				hits                        int
				method, path, query, bearer string
				raw                         []byte
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				mu.Lock()
				hits++
				method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
				bearer = r.Header.Get("Authorization")
				raw = b
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(tc.reply)
			}))
			t.Cleanup(srv.Close)

			tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0}, WithAuthToken(token))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			got, err := tc.call(tr.QRDBRAG())
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !reflect.DeepEqual(got, tc.reply) {
				t.Errorf("response: got %+v, want %+v", got, tc.reply)
			}

			mu.Lock()
			defer mu.Unlock()
			if hits != 1 {
				t.Fatalf("requests reaching the server: got %d, want 1", hits)
			}
			if method != tc.method {
				t.Errorf("method: got %s, want %s", method, tc.method)
			}
			if path != tc.path {
				t.Errorf("path: got %s, want %s", path, tc.path)
			}
			if query != tc.rawQuery {
				t.Errorf("query: got %q, want %q", query, tc.rawQuery)
			}
			if want := "Bearer " + token; bearer != want {
				t.Errorf("Authorization: got %q, want %q", bearer, want)
			}
			if tc.body != nil {
				var gotBody wantBody
				if err := json.Unmarshal(raw, &gotBody); err != nil {
					t.Fatalf("request body %q: %v", raw, err)
				}
				if !reflect.DeepEqual(&gotBody, tc.body) {
					t.Errorf("request body: got %+v, want %+v", gotBody, *tc.body)
				}
			}
		})
	}
}

// TestQRDBRAGClient_EmptyResponseIsAnError proves an empty 200 reply is
// reported, not decoded as an empty result.
func TestQRDBRAGClient_EmptyResponseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.QRDBRAG().RAGQuery(context.Background(), &qrdbtransport.RAGQueryRequest{Query: "q", Namespace: "ns"}); err == nil {
		t.Fatal("RAGQuery: got nil error for an empty response, want an error")
	}
}

// TestQRDBRAGClient_ServerErrorSurfaces proves a server refusal reaches the
// caller as an error with no response.
func TestQRDBRAGClient_ServerErrorSurfaces(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"forbidden"}`))
	}))
	t.Cleanup(srv.Close)

	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0}, WithAuthToken("tok-rag"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	resp, err := tr.QRDBRAG().RAGIngestDocument(context.Background(), &qrdbtransport.RAGIngestRequest{
		DocumentID: "doc-1", Text: "t", Namespace: "ns",
	})
	if err == nil {
		t.Fatal("RAGIngestDocument: got nil error for a 403, want an error")
	}
	if resp != nil {
		t.Errorf("RAGIngestDocument: got response %+v with the error, want nil", resp)
	}
}

// TestQRDBRAGClient_QUICInheritsAdapter proves the REST transport exposes
// the method the QUIC transport inherits through embedding.
func TestQRDBRAGClient_QUICInheritsAdapter(t *testing.T) {
	var _ interface {
		QRDBRAG() qrdbtransport.RAGClient
	} = (*Transport)(nil)

	tr, err := New(&transport.Config{Address: "http://127.0.0.1:1", MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if tr.QRDBRAG() == nil {
		t.Fatal("QRDBRAG: got nil client")
	}
}
