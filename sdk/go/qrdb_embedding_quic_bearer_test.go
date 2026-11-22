package quicbaas

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQRDBEmbedding_QUIC_SendsPlatformBearer checks that AI().QRDBEmbedding()
// over QUIC rides the client's HTTP/3 transport to a UDP-only listener and
// carries the platform bearer set by WithAuthToken on Embed, BatchEmbed and
// ListEmbeddingModels, so the routes' permission checks see the caller.
func TestQRDBEmbedding_QUIC_SendsPlatformBearer(t *testing.T) {
	var (
		mu     sync.Mutex
		auths  = map[string]string{}
		protos = map[string]int{}
	)
	record := func(name string, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		auths[name] = r.Header.Get("Authorization")
		protos[name] = r.ProtoMajor
	}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /v1/embeddings", func(w http.ResponseWriter, r *http.Request) {
		record("embed", r)
		writeJSON(w, &qrdbtransport.EmbedResponse{Embeddings: [][]float32{{0.5}}, Model: "m"})
	})
	mux.HandleFunc("POST /v1/embeddings/batch", func(w http.ResponseWriter, r *http.Request) {
		record("batch", r)
		writeJSON(w, &qrdbtransport.BatchEmbedResponse{Embeddings: [][]float32{{0.5}}, Model: "m"})
	})
	mux.HandleFunc("GET /v1/embedding/models", func(w http.ResponseWriter, r *http.Request) {
		record("models", r)
		writeJSON(w, &qrdbtransport.ListEmbeddingModelsResponse{
			Models: []qrdbtransport.EmbeddingModelInfo{{Name: "m", Dimensions: 1}},
		})
	})

	cert := h3BarrierTestCert(t)
	addr := startUDPOnlyHTTP3Server(t, cert, mux)

	roots := x509.NewCertPool()
	roots.AddCert(cert.Leaf)
	c, err := NewClient(addr,
		WithQUIC(),
		WithTLSConfig(&tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}),
		WithAuthToken("t-2506"),
		WithTimeout(5*time.Second),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	ec, err := c.AI().QRDBEmbedding()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err = ec.Embed(ctx, &QRDBEmbedRequest{Texts: []string{"hi"}})
	require.NoError(t, err)
	_, err = ec.BatchEmbed(ctx, &QRDBBatchEmbedRequest{Texts: []string{"hi"}})
	require.NoError(t, err)
	_, err = ec.ListEmbeddingModels(ctx, "")
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	for _, name := range []string{"embed", "batch", "models"} {
		assert.Equal(t, "Bearer t-2506", auths[name], "%s Authorization", name)
		assert.Equal(t, 3, protos[name], "%s request protocol major version", name)
	}
}
