package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// unsealSecret is the passphrase the tests send; no refusal may echo it.
const unsealSecret = "unseal-passphrase-2554"

// trustServer returns a TLS config that trusts the httptest TLS server.
func trustServer(srv *httptest.Server) *tls.Config {
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
}

// countingServer counts every non-health request it receives.
func countingServer(hits *atomic.Int32) *httptest.Server {
	return httptest.NewServer(recordingHandler(hits))
}

// redirectingTLSServer answers from on a TLS server with a 307 to
// location(srv), and serves everything else (the health probe and a
// same-host target) through next.
func redirectingTLSServer(from string, location func(*httptest.Server) string, next http.Handler) *httptest.Server {
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == from {
			http.Redirect(w, r, location(srv), http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	}))
	return srv
}

// newTLSTransports builds the transport twice for srv: once with the client
// New builds, once WithHTTPClient as the QUIC transport supplies it.
func newTLSTransports(t *testing.T, srv *httptest.Server) map[string]*Transport {
	t.Helper()
	cfg := trustServer(srv)
	built, err := New(&transport.Config{Address: srv.URL, TLSConfig: cfg, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	supplied, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0},
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{TLSClientConfig: cfg}}))
	if err != nil {
		t.Fatalf("New WithHTTPClient: %v", err)
	}
	t.Cleanup(func() { _ = built.Close(); _ = supplied.Close() })
	return map[string]*Transport{"built": built, "supplied": supplied}
}

func requireRefusal(t *testing.T, err error, what string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: got nil error, want a refusal", what)
	}
	if strings.Contains(err.Error(), unsealSecret) {
		t.Fatalf("%s: the refusal names the secret: %v", what, err)
	}
	if got := errors.Code(err); got != errors.CodeForbidden {
		t.Fatalf("%s: code = %v, want CodeForbidden: %v", what, got, err)
	}
	if errors.IsRetryable(err) {
		t.Fatalf("%s: refusal is retryable: %v", what, err)
	}
}

// A secret-carrying barrier call refuses a redirect from the https origin to
// a plaintext http Location: the passphrase never reaches the http server.
func TestBarrierSecretHop_RefusesRedirectToHTTP(t *testing.T) {
	var plainHits atomic.Int32
	plain := countingServer(&plainHits)
	t.Cleanup(plain.Close)

	srv := redirectingTLSServer("/v1/barrier/unseal",
		func(*httptest.Server) string { return plain.URL + "/v1/barrier/unseal" },
		recordingHandler(new(atomic.Int32)))
	t.Cleanup(srv.Close)

	for name, tr := range newTLSTransports(t, srv) {
		t.Run(name, func(t *testing.T) {
			err := tr.Barrier().BarrierUnseal(context.Background(),
				&qrdbtransport.BarrierUnsealRequest{Secret: []byte(unsealSecret)})
			requireRefusal(t, err, "BarrierUnseal redirected to http")
			if !strings.Contains(err.Error(), "BarrierUnseal") {
				t.Fatalf("refusal does not name the verb: %v", err)
			}
		})
	}
	if n := plainHits.Load(); n != 0 {
		t.Fatalf("requests reaching the plaintext server: got %d, want 0", n)
	}
}

// A secret-carrying barrier call refuses a redirect even to the same https
// host: the redirect target receives nothing.
func TestBarrierSecretHop_RefusesSameHostRedirect(t *testing.T) {
	var targetHits atomic.Int32
	const target = "/v1/barrier/unseal-elsewhere"
	srv := redirectingTLSServer("/v1/barrier/unseal",
		func(s *httptest.Server) string { return s.URL + target },
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == target {
				targetHits.Add(1)
			}
			w.WriteHeader(http.StatusNoContent)
		}))
	t.Cleanup(srv.Close)

	for name, tr := range newTLSTransports(t, srv) {
		t.Run(name, func(t *testing.T) {
			err := tr.Barrier().BarrierUnseal(context.Background(),
				&qrdbtransport.BarrierUnsealRequest{Secret: []byte(unsealSecret)})
			requireRefusal(t, err, "BarrierUnseal redirected on the same host")
		})
	}
	if n := targetHits.Load(); n != 0 {
		t.Fatalf("requests reaching the redirect target: got %d, want 0", n)
	}
}

// A transport whose base URL is plaintext http refuses the secret-carrying
// calls before anything is sent.
func TestBarrierSecretHop_RefusesHTTPTarget(t *testing.T) {
	var hits atomic.Int32
	plain := countingServer(&hits)
	t.Cleanup(plain.Close)

	built, err := New(&transport.Config{Address: plain.URL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	supplied, err := New(&transport.Config{Address: plain.URL, MaxRetries: 0},
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}))
	if err != nil {
		t.Fatalf("New WithHTTPClient: %v", err)
	}
	t.Cleanup(func() { _ = built.Close(); _ = supplied.Close() })

	for name, tr := range map[string]*Transport{"built": built, "supplied": supplied} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			requireRefusal(t, tr.Barrier().BarrierUnseal(ctx,
				&qrdbtransport.BarrierUnsealRequest{Secret: []byte(unsealSecret)}), "BarrierUnseal over http")
			requireRefusal(t, tr.Barrier().BarrierUnsealWithShares(ctx,
				&qrdbtransport.BarrierUnsealSharesRequest{Shares: []string{unsealSecret}}), "BarrierUnsealWithShares over http")
		})
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("requests reaching the plaintext server: got %d, want 0", n)
	}
}

// An unmarked call is not refused by the guard: BarrierStatus still
// follows a same-host https redirect, as it did before.
func TestBarrierSecretHop_UnmarkedFollowsRedirect(t *testing.T) {
	var targetHits atomic.Int32
	const target = "/v1/barrier/status-elsewhere"
	srv := redirectingTLSServer("/v1/barrier/status",
		func(s *httptest.Server) string { return s.URL + target },
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == target {
				targetHits.Add(1)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sealed":true}`))
		}))
	t.Cleanup(srv.Close)

	for name, tr := range newTLSTransports(t, srv) {
		t.Run(name, func(t *testing.T) {
			status, err := tr.Barrier().BarrierStatus(context.Background())
			if err != nil {
				t.Fatalf("BarrierStatus through a same-host redirect: %v", err)
			}
			if !status.Sealed {
				t.Fatalf("BarrierStatus: got %+v, want the redirect target's sealed=true", status)
			}
		})
	}
	if n := targetHits.Load(); n != 2 {
		t.Fatalf("requests reaching the redirect target: got %d, want 2", n)
	}
}
