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

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
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

// qrdbDEKVerb is one tenant DEK call; secret marks the data verbs that carry
// key-protected data and so take the secret-hop guard.
type qrdbDEKVerb struct {
	name   string
	secret bool
	call   func(context.Context, *QRDBDEKClient) error
}

func qrdbDEKVerbs() []qrdbDEKVerb {
	return []qrdbDEKVerb{
		{"TenantDEKStatus", false, func(ctx context.Context, c *QRDBDEKClient) error {
			_, err := c.TenantDEKStatus(ctx, &qrdbtransport.TenantDEKRequest{TenantID: "t1"})
			return err
		}},
		{"TenantDEKRotate", false, func(ctx context.Context, c *QRDBDEKClient) error {
			_, err := c.TenantDEKRotate(ctx, &qrdbtransport.TenantDEKRequest{})
			return err
		}},
		{"TenantDEKRetire", false, func(ctx context.Context, c *QRDBDEKClient) error {
			_, err := c.TenantDEKRetire(ctx, &qrdbtransport.TenantDEKRetireRequest{Version: 1})
			return err
		}},
		{"TenantDEKEncrypt", true, func(ctx context.Context, c *QRDBDEKClient) error {
			_, err := c.TenantDEKEncrypt(ctx, &qrdbtransport.TenantDEKEncryptRequest{Plaintext: []byte("hi"), AAD: []byte("a")})
			return err
		}},
		{"TenantDEKDecrypt", true, func(ctx context.Context, c *QRDBDEKClient) error {
			_, err := c.TenantDEKDecrypt(ctx, &qrdbtransport.TenantDEKDecryptRequest{Ciphertext: []byte("ct"), AAD: []byte("a")})
			return err
		}},
		{"TenantDEKRewrap", true, func(ctx context.Context, c *QRDBDEKClient) error {
			_, err := c.TenantDEKRewrap(ctx, &qrdbtransport.TenantDEKRewrapRequest{Ciphertext: []byte("ct"), AAD: []byte("a")})
			return err
		}},
	}
}

// qrdbDEKPlainTransports builds a client on the plain http address addr, and
// one with a supplied http.Client, each with opts.
func qrdbDEKPlainTransports(t *testing.T, addr string, opts ...Option) map[string]*Transport {
	t.Helper()
	built, err := New(&transport.Config{Address: addr, MaxRetries: 0}, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	supplied, err := New(&transport.Config{Address: addr, MaxRetries: 0},
		append([]Option{WithHTTPClient(&http.Client{Timeout: 5 * time.Second})}, opts...)...)
	if err != nil {
		t.Fatalf("New WithHTTPClient: %v", err)
	}
	t.Cleanup(func() { _ = built.Close(); _ = supplied.Close() })
	return map[string]*Transport{"built": built, "supplied": supplied}
}

// qrdbDEKRefusal checks err is a non-retryable CodeForbidden refusal.
func qrdbDEKRefusal(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("got nil error, want a refusal")
	}
	if got := errors.Code(err); got != errors.CodeForbidden {
		t.Errorf("code = %v, want CodeForbidden: %v", got, err)
	}
	if errors.IsRetryable(err) {
		t.Errorf("refusal is retryable: %v", err)
	}
}

func TestQRDBDEKClient_SecretVerbsRefusePlaintext(t *testing.T) {
	var hits atomic.Int32
	plain := countingServer(&hits)
	t.Cleanup(plain.Close)

	for trName, tr := range qrdbDEKPlainTransports(t, plain.URL) {
		for _, v := range qrdbDEKVerbs() {
			if !v.secret {
				continue
			}
			t.Run(trName+"/"+v.name, func(t *testing.T) {
				err := v.call(context.Background(), tr.QRDBDEK())
				qrdbDEKRefusal(t, err)
				if err != nil && !strings.Contains(err.Error(), v.name) {
					t.Errorf("err = %v, want it to name %s", err, v.name)
				}
			})
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("plaintext server received %d requests, want 0", n)
	}
}

func TestQRDBDEKClient_BearerNeverSentOverPlaintext(t *testing.T) {
	var hits, authHits atomic.Int32
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		hits.Add(1)
		if r.Header.Get("Authorization") != "" {
			authHits.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"tenant_id":"t1","current_version":2}`)
	}))
	t.Cleanup(plain.Close)

	t.Run("bearer", func(t *testing.T) {
		for trName, tr := range qrdbDEKPlainTransports(t, plain.URL, WithAuthToken("tok-test")) {
			for _, v := range qrdbDEKVerbs() {
				t.Run(trName+"/"+v.name, func(t *testing.T) {
					err := v.call(context.Background(), tr.QRDBDEK())
					qrdbDEKRefusal(t, err)
					// A data verb may be stopped first by the secret-hop
					// guard; any other verb must hit ErrPlaintextBearer.
					if !v.secret && !stderrors.Is(err, ErrPlaintextBearer) {
						t.Errorf("err = %v, want ErrPlaintextBearer", err)
					}
				})
			}
		}
		if n := hits.Load(); n != 0 {
			t.Fatalf("plaintext server received %d requests (%d with Authorization), want 0", n, authHits.Load())
		}
	})

	t.Run("no bearer", func(t *testing.T) {
		for trName, tr := range qrdbDEKPlainTransports(t, plain.URL) {
			for _, v := range qrdbDEKVerbs() {
				if v.secret {
					continue
				}
				t.Run(trName+"/"+v.name, func(t *testing.T) {
					if err := v.call(context.Background(), tr.QRDBDEK()); err != nil {
						t.Fatalf("%s without a bearer over http: %v", v.name, err)
					}
				})
			}
		}
		if n := authHits.Load(); n != 0 {
			t.Errorf("server saw %d requests with Authorization, want 0", n)
		}
	})
}

// qrdbDEKRedirectOrigin is a TLS server that answers every /v1/dek/ route
// with a 307 to target(srv)+path+suffix and serves the rest through next.
func qrdbDEKRedirectOrigin(t *testing.T, target func(*httptest.Server) string, suffix string, next http.Handler) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/v1/dek/") && (suffix == "" || !strings.HasSuffix(r.URL.Path, suffix)) {
			http.Redirect(w, r, target(srv)+r.URL.Path+suffix, http.StatusTemporaryRedirect)
			return
		}
		next.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestQRDBDEKClient_RefusesRedirects(t *testing.T) {
	rows := []struct {
		name string
		// follow reports whether a non-secret verb follows the redirect.
		follow bool
		origin func(t *testing.T, saw *atomic.Bool) *httptest.Server
	}{
		{"https to http downgrade", false, func(t *testing.T, saw *atomic.Bool) *httptest.Server {
			plain := httptest.NewServer(tenantRedirectTarget(saw))
			t.Cleanup(plain.Close)
			return qrdbDEKRedirectOrigin(t, func(*httptest.Server) string { return plain.URL }, "", http.NotFoundHandler())
		}},
		{"https to another host", false, func(t *testing.T, saw *atomic.Bool) *httptest.Server {
			other := httptest.NewTLSServer(tenantRedirectTarget(saw))
			t.Cleanup(other.Close)
			return qrdbDEKRedirectOrigin(t, func(*httptest.Server) string { return other.URL }, "", http.NotFoundHandler())
		}},
		{"same-host https", true, func(t *testing.T, saw *atomic.Bool) *httptest.Server {
			return qrdbDEKRedirectOrigin(t, func(s *httptest.Server) string { return s.URL }, "-moved", tenantRedirectTarget(saw))
		}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			var saw atomic.Bool
			origin := row.origin(t, &saw)
			for trName, tr := range tenantRedirectTransports(t, origin) {
				for _, v := range qrdbDEKVerbs() {
					t.Run(trName+"/"+v.name, func(t *testing.T) {
						saw.Store(false)
						err := v.call(context.Background(), tr.QRDBDEK())
						if row.follow && !v.secret {
							if err != nil {
								t.Fatalf("same-host redirect: %v", err)
							}
							if !saw.Load() {
								t.Fatalf("same-host target got no bearer")
							}
							return
						}
						qrdbDEKRefusal(t, err)
						if !v.secret && !stderrors.Is(err, ErrInsecureRedirect) {
							t.Errorf("err = %v, want ErrInsecureRedirect", err)
						}
						if saw.Load() {
							t.Errorf("redirect target received the bearer")
						}
					})
				}
			}
		})
	}
}

func TestQRDBDEKClient_HTTPSRoundTrip(t *testing.T) {
	var badAuth atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok-2558" {
			badAuth.Add(1)
		}
		var in struct {
			Plaintext  []byte `json:"plaintext"`
			Ciphertext []byte `json:"ciphertext"`
		}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&in)
		}
		var out any
		switch r.URL.Path {
		case "/v1/dek/status", "/v1/dek/rotate", "/v1/dek/retire":
			out = &qrdbtransport.TenantDEKStatusResponse{TenantID: "t1", CurrentVersion: 2}
		case "/v1/dek/encrypt":
			out = &qrdbtransport.TenantDEKEncryptResponse{Ciphertext: append([]byte("ct:"), in.Plaintext...), Version: 2}
		case "/v1/dek/decrypt":
			out = &qrdbtransport.TenantDEKDecryptResponse{Plaintext: bytes.TrimPrefix(in.Ciphertext, []byte("ct:")), Version: 2}
		case "/v1/dek/rewrap":
			out = &qrdbtransport.TenantDEKRewrapResponse{Ciphertext: in.Ciphertext, FromVersion: 1, ToVersion: 2}
		default:
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)

	for trName, tr := range tenantRedirectTransports(t, srv) {
		t.Run(trName, func(t *testing.T) {
			qrdbDEKRoundTrip(t, tr.QRDBDEK())
		})
	}
	if n := badAuth.Load(); n != 0 {
		t.Errorf("%d requests lacked the bearer", n)
	}
}

// qrdbDEKRoundTrip runs the six verbs against TestQRDBDEKClient_HTTPSRoundTrip's
// server and checks every response body.
func qrdbDEKRoundTrip(t *testing.T, c *QRDBDEKClient) {
	t.Helper()
	ctx := context.Background()
	for _, call := range []func() (*qrdbtransport.TenantDEKStatusResponse, error){
		func() (*qrdbtransport.TenantDEKStatusResponse, error) {
			return c.TenantDEKStatus(ctx, &qrdbtransport.TenantDEKRequest{TenantID: "t1"})
		},
		func() (*qrdbtransport.TenantDEKStatusResponse, error) {
			return c.TenantDEKRotate(ctx, &qrdbtransport.TenantDEKRequest{})
		},
		func() (*qrdbtransport.TenantDEKStatusResponse, error) {
			return c.TenantDEKRetire(ctx, &qrdbtransport.TenantDEKRetireRequest{Version: 1})
		},
	} {
		st, err := call()
		if err != nil {
			t.Fatalf("status verb over https: %v", err)
		}
		if st.CurrentVersion != 2 {
			t.Errorf("CurrentVersion = %d, want 2", st.CurrentVersion)
		}
	}

	enc, err := c.TenantDEKEncrypt(ctx, &qrdbtransport.TenantDEKEncryptRequest{Plaintext: []byte("hi"), AAD: []byte("a")})
	if err != nil {
		t.Fatalf("TenantDEKEncrypt over https: %v", err)
	}
	if string(enc.Ciphertext) != "ct:hi" || enc.Version != 2 {
		t.Errorf("encrypt = %q v%d, want \"ct:hi\" v2", enc.Ciphertext, enc.Version)
	}
	dec, err := c.TenantDEKDecrypt(ctx, &qrdbtransport.TenantDEKDecryptRequest{Ciphertext: enc.Ciphertext, AAD: []byte("a")})
	if err != nil {
		t.Fatalf("TenantDEKDecrypt over https: %v", err)
	}
	if string(dec.Plaintext) != "hi" || dec.Version != 2 {
		t.Errorf("decrypt = %q v%d, want \"hi\" v2", dec.Plaintext, dec.Version)
	}
	rw, err := c.TenantDEKRewrap(ctx, &qrdbtransport.TenantDEKRewrapRequest{Ciphertext: enc.Ciphertext, AAD: []byte("a")})
	if err != nil {
		t.Fatalf("TenantDEKRewrap over https: %v", err)
	}
	if string(rw.Ciphertext) != "ct:hi" || rw.FromVersion != 1 || rw.ToVersion != 2 {
		t.Errorf("rewrap = %q v%d->v%d, want \"ct:hi\" v1->v2", rw.Ciphertext, rw.FromVersion, rw.ToVersion)
	}
}
