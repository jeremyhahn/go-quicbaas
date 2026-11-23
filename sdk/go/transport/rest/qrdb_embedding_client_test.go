package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestQRDBEmbeddingClient_MethodPathAndBearer checks that each embedding verb
// reaches go-qrdb's route with the right method, path and query, and carries
// the platform bearer the transport was built with.
func TestQRDBEmbeddingClient_MethodPathAndBearer(t *testing.T) {
	ctx := context.Background()
	const token = "tok-embed"

	cases := []struct {
		name     string
		method   string
		path     string
		rawQuery string
		reply    any
		call     func(c *QRDBEmbeddingClient) error
	}{
		{
			name:   "Embed",
			method: http.MethodPost,
			path:   "/v1/embeddings",
			reply:  &qrdbtransport.EmbedResponse{Embeddings: [][]float32{{1}}, Model: "m"},
			call: func(c *QRDBEmbeddingClient) error {
				_, err := c.Embed(ctx, &qrdbtransport.EmbedRequest{Texts: []string{"a"}})
				return err
			},
		},
		{
			name:   "BatchEmbed",
			method: http.MethodPost,
			path:   "/v1/embeddings/batch",
			reply:  &qrdbtransport.BatchEmbedResponse{},
			call: func(c *QRDBEmbeddingClient) error {
				_, err := c.BatchEmbed(ctx, &qrdbtransport.BatchEmbedRequest{Texts: []string{"a"}})
				return err
			},
		},
		{
			name:     "ListEmbeddingModels",
			method:   http.MethodGet,
			path:     "/v1/embedding/models",
			rawQuery: "provider=ollama",
			reply:    &qrdbtransport.ListEmbeddingModelsResponse{},
			call: func(c *QRDBEmbeddingClient) error {
				_, err := c.ListEmbeddingModels(ctx, "ollama")
				return err
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu                          sync.Mutex
				hits                        int
				method, path, query, bearer string
			)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				hits++
				method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
				bearer = r.Header.Get("Authorization")
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

			if err := tc.call(tr.QRDBEmbedding()); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
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
		})
	}
}

// TestQRDBEmbeddingClient_EmptyResponseIsAnError checks that an empty 200 body
// is refused rather than decoded into a zero response.
func TestQRDBEmbeddingClient_EmptyResponseIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.QRDBEmbedding().Embed(context.Background(), &qrdbtransport.EmbedRequest{Texts: []string{"a"}}); err == nil {
		t.Fatal("Embed: got nil error for an empty response, want an error")
	}
}
