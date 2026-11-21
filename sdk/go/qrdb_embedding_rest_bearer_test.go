package quicbaas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQRDBEmbedding_REST_SendsPlatformBearer checks that AI().QRDBEmbedding()
// over REST carries the platform bearer set by WithAuthToken to go-qrdb's
// embedding route, so the route's permission check sees the caller.
func TestQRDBEmbedding_REST_SendsPlatformBearer(t *testing.T) {
	var (
		mu   sync.Mutex
		auth string
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		auth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&qrdbtransport.EmbedResponse{Embeddings: [][]float32{{0.5}}, Model: "m"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := NewClient(srv.URL, WithREST(), WithAuthToken("t-2506"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ec, err := c.AI().QRDBEmbedding()
	require.NoError(t, err)
	_, err = ec.Embed(context.Background(), &QRDBEmbedRequest{Texts: []string{"hi"}})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "Bearer t-2506", auth)
}
