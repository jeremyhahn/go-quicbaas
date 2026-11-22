package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestBarrierClient_MethodPathAndBearer drives every barrier verb through the
// SDK's REST transport and checks that each reaches the go-qrdb REST route
// with the go-qrdb HTTP method and carries the platform bearer
// (go-quicbaas#2538).
func TestBarrierClient_MethodPathAndBearer(t *testing.T) {
	const token = "t-2538"
	ctx := context.Background()

	tests := []struct {
		name   string
		method string
		path   string
		call   func(b *BarrierClient) error
	}{
		{"BarrierInitialize", http.MethodPost, "/v1/barrier/initialize", func(b *BarrierClient) error {
			return b.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{})
		}},
		{"BarrierUnseal", http.MethodPost, "/v1/barrier/unseal", func(b *BarrierClient) error {
			return b.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{})
		}},
		{"BarrierSeal", http.MethodPost, "/v1/barrier/seal", func(b *BarrierClient) error {
			return b.BarrierSeal(ctx)
		}},
		{"BarrierStatus", http.MethodGet, "/v1/barrier/status", func(b *BarrierClient) error {
			_, err := b.BarrierStatus(ctx)
			return err
		}},
		{"BarrierInitializeShamir", http.MethodPost, "/v1/barrier/shamir/initialize", func(b *BarrierClient) error {
			_, err := b.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{})
			return err
		}},
		{"BarrierUnsealWithShare", http.MethodPost, "/v1/barrier/shamir/unseal-share", func(b *BarrierClient) error {
			_, err := b.BarrierUnsealWithShare(ctx, &qrdbtransport.BarrierUnsealShareRequest{})
			return err
		}},
		{"BarrierUnsealWithShares", http.MethodPost, "/v1/barrier/shamir/unseal-shares", func(b *BarrierClient) error {
			return b.BarrierUnsealWithShares(ctx, &qrdbtransport.BarrierUnsealSharesRequest{})
		}},
		{"BarrierRekey", http.MethodPost, "/v1/barrier/rekey", func(b *BarrierClient) error {
			_, err := b.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{})
			return err
		}},
		{"BarrierEnableEncryption", http.MethodPost, "/v1/barrier/enable-encryption", func(b *BarrierClient) error {
			_, err := b.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{})
			return err
		}},
		{"BarrierGenerateRecoveryKeys", http.MethodPost, "/v1/barrier/recovery/generate", func(b *BarrierClient) error {
			_, err := b.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{})
			return err
		}},
		{"BarrierRecoverWithKeys", http.MethodPost, "/v1/barrier/recovery/recover", func(b *BarrierClient) error {
			return b.BarrierRecoverWithKeys(ctx, &qrdbtransport.BarrierRecoverWithKeysRequest{})
		}},
		{"BarrierDeleteRecoveryKeys", http.MethodDelete, "/v1/barrier/recovery", func(b *BarrierClient) error {
			return b.BarrierDeleteRecoveryKeys(ctx)
		}},
		{"BarrierHasRecoveryKeys", http.MethodGet, "/v1/barrier/recovery/status", func(b *BarrierClient) error {
			_, err := b.BarrierHasRecoveryKeys(ctx)
			return err
		}},
		{"BarrierGenerateRootToken", http.MethodPost, "/v1/barrier/root-token", func(b *BarrierClient) error {
			_, err := b.BarrierGenerateRootToken(ctx, &qrdbtransport.BarrierGenerateRootTokenRequest{})
			return err
		}},
		{"OfficerRegister", http.MethodPost, "/v1/barrier/officers", func(b *BarrierClient) error {
			_, err := b.OfficerRegister(ctx, &qrdbtransport.OfficerRegisterRequest{Username: "so1"})
			return err
		}},
		{"OfficerList", http.MethodGet, "/v1/barrier/officers", func(b *BarrierClient) error {
			_, err := b.OfficerList(ctx)
			return err
		}},
		{"OfficerClaimStatus", http.MethodGet, "/v1/barrier/officers/claim-status", func(b *BarrierClient) error {
			_, err := b.OfficerClaimStatus(ctx)
			return err
		}},
		{"ShareClaimBegin", http.MethodPost, "/v1/barrier/officers/so1/claim/begin", func(b *BarrierClient) error {
			_, err := b.ShareClaimBegin(ctx, &qrdbtransport.ShareClaimBeginRequest{Username: "so1"})
			return err
		}},
		{"ShareClaimComplete", http.MethodPost, "/v1/barrier/officers/so1/claim/complete", func(b *BarrierClient) error {
			_, err := b.ShareClaimComplete(ctx, &qrdbtransport.ShareClaimCompleteRequest{Username: "so1"})
			return err
		}},
		{"ShamirSealVote", http.MethodPost, "/v1/barrier/shamir/seal-vote", func(b *BarrierClient) error {
			_, err := b.ShamirSealVote(ctx, &qrdbtransport.ShamirSealVoteRequest{})
			return err
		}},
		{"RecoveryOfficerRegister", http.MethodPost, "/v1/barrier/recovery/officers", func(b *BarrierClient) error {
			_, err := b.RecoveryOfficerRegister(ctx, &qrdbtransport.OfficerRegisterRequest{Username: "ro1"})
			return err
		}},
		{"RecoveryShareClaimBegin", http.MethodPost, "/v1/barrier/recovery/officers/ro1/claim/begin", func(b *BarrierClient) error {
			_, err := b.RecoveryShareClaimBegin(ctx, &qrdbtransport.ShareClaimBeginRequest{Username: "ro1"})
			return err
		}},
		{"RecoveryShareClaimComplete", http.MethodPost, "/v1/barrier/recovery/officers/ro1/claim/complete", func(b *BarrierClient) error {
			_, err := b.RecoveryShareClaimComplete(ctx, &qrdbtransport.ShareClaimCompleteRequest{Username: "ro1"})
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu                 sync.Mutex
				method, path, auth string
				hits               int
			)
			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/health" {
					w.WriteHeader(http.StatusOK)
					return
				}
				mu.Lock()
				method, path, auth = r.Method, r.URL.Path, r.Header.Get("Authorization")
				hits++
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			t.Cleanup(srv.Close)

			tr, err := New(&transport.Config{Address: srv.URL, TLSConfig: trustServer(srv), MaxRetries: 0}, WithAuthToken(token))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			if err := tc.call(tr.Barrier()); err != nil {
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
			if want := "Bearer " + token; auth != want {
				t.Errorf("Authorization: got %q, want %q", auth, want)
			}
		})
	}
}

// TestBarrierClient_EmptyResponseIsAnError checks that every barrier verb
// that returns a value refuses an empty 200 or 204 body instead of
// returning a zero value with no error (go-quicbaas#2538): a zero
// BarrierStatusResponse would report Sealed=false for a server that said
// nothing. A verb that returns only an error still accepts an empty body.
func TestBarrierClient_EmptyResponseIsAnError(t *testing.T) {
	ctx := context.Background()

	valued := []struct {
		name string
		call func(b *BarrierClient) error
	}{
		{"BarrierStatus", func(b *BarrierClient) error {
			_, err := b.BarrierStatus(ctx)
			return err
		}},
		{"BarrierInitializeShamir", func(b *BarrierClient) error {
			_, err := b.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{})
			return err
		}},
		{"BarrierUnsealWithShare", func(b *BarrierClient) error {
			_, err := b.BarrierUnsealWithShare(ctx, &qrdbtransport.BarrierUnsealShareRequest{})
			return err
		}},
		{"BarrierRekey", func(b *BarrierClient) error {
			_, err := b.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{})
			return err
		}},
		{"BarrierEnableEncryption", func(b *BarrierClient) error {
			_, err := b.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{})
			return err
		}},
		{"BarrierGenerateRecoveryKeys", func(b *BarrierClient) error {
			_, err := b.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{})
			return err
		}},
		{"BarrierHasRecoveryKeys", func(b *BarrierClient) error {
			_, err := b.BarrierHasRecoveryKeys(ctx)
			return err
		}},
		{"BarrierGenerateRootToken", func(b *BarrierClient) error {
			_, err := b.BarrierGenerateRootToken(ctx, &qrdbtransport.BarrierGenerateRootTokenRequest{})
			return err
		}},
		{"OfficerRegister", func(b *BarrierClient) error {
			_, err := b.OfficerRegister(ctx, &qrdbtransport.OfficerRegisterRequest{Username: "so1"})
			return err
		}},
		{"OfficerList", func(b *BarrierClient) error {
			_, err := b.OfficerList(ctx)
			return err
		}},
		{"OfficerClaimStatus", func(b *BarrierClient) error {
			_, err := b.OfficerClaimStatus(ctx)
			return err
		}},
		{"ShareClaimBegin", func(b *BarrierClient) error {
			_, err := b.ShareClaimBegin(ctx, &qrdbtransport.ShareClaimBeginRequest{Username: "so1"})
			return err
		}},
		{"ShareClaimComplete", func(b *BarrierClient) error {
			_, err := b.ShareClaimComplete(ctx, &qrdbtransport.ShareClaimCompleteRequest{Username: "so1"})
			return err
		}},
		{"ShamirSealVote", func(b *BarrierClient) error {
			_, err := b.ShamirSealVote(ctx, &qrdbtransport.ShamirSealVoteRequest{})
			return err
		}},
		{"RecoveryOfficerRegister", func(b *BarrierClient) error {
			_, err := b.RecoveryOfficerRegister(ctx, &qrdbtransport.OfficerRegisterRequest{Username: "ro1"})
			return err
		}},
		{"RecoveryShareClaimBegin", func(b *BarrierClient) error {
			_, err := b.RecoveryShareClaimBegin(ctx, &qrdbtransport.ShareClaimBeginRequest{Username: "ro1"})
			return err
		}},
		{"RecoveryShareClaimComplete", func(b *BarrierClient) error {
			_, err := b.RecoveryShareClaimComplete(ctx, &qrdbtransport.ShareClaimCompleteRequest{Username: "ro1"})
			return err
		}},
	}

	emptyBarrier := func(t *testing.T, status int) *BarrierClient {
		t.Helper()
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(status)
		}))
		t.Cleanup(srv.Close)

		tr, err := New(&transport.Config{Address: srv.URL, TLSConfig: trustServer(srv), MaxRetries: 0})
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		t.Cleanup(func() { _ = tr.Close() })
		return tr.Barrier()
	}

	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		for _, tc := range valued {
			t.Run(fmt.Sprintf("%s/%d", tc.name, status), func(t *testing.T) {
				if err := tc.call(emptyBarrier(t, status)); err == nil {
					t.Fatalf("%s on an empty %d body: got nil error, want an error", tc.name, status)
				}
			})
		}
	}

	t.Run("BarrierUnseal/204", func(t *testing.T) {
		if err := emptyBarrier(t, http.StatusNoContent).BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{}); err != nil {
			t.Fatalf("BarrierUnseal on an empty 204 body: %v", err)
		}
	})
}

// recordingHandler answers every barrier request with an empty JSON object
// and counts the requests that reached it (the /health probe is not counted).
func recordingHandler(hits *atomic.Int32) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sealed":false}`))
	}
}

// bindSOPIN returns a context carrying an SO PIN, released at test end.
func bindSOPIN(t *testing.T) context.Context {
	t.Helper()
	bound, release, err := qrdbtransport.WithSOPIN(context.Background(), []byte("so-pin-2538"))
	if err != nil {
		t.Fatalf("WithSOPIN: %v", err)
	}
	t.Cleanup(release)
	return bound
}

// A bound SO PIN never crosses plaintext http: the call is refused before
// anything is sent, as go-qrdb's own REST transport refuses it.
func TestBarrierClient_SOPINRequiresTLS(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(recordingHandler(&hits))
	t.Cleanup(srv.Close)

	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = tr.Barrier().BarrierSeal(bindSOPIN(t))
	var tlsErr *qrdbtransport.SOPINRequiresTLSError
	if !errors.As(err, &tlsErr) {
		t.Fatalf("BarrierSeal with a bound PIN over http: got %v, want *SOPINRequiresTLSError", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("requests reaching the plaintext server: got %d, want 0", n)
	}
}

// A redirect never carries a bound SO PIN to a host the caller did not name.
func TestBarrierClient_SOPINRefusesForeignRedirect(t *testing.T) {
	var hitsB atomic.Int32
	srvB := httptest.NewTLSServer(recordingHandler(&hitsB))
	t.Cleanup(srvB.Close)

	srvA := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, srvB.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srvA.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srvA.Certificate())
	pool.AddCert(srvB.Certificate())

	tr, err := New(&transport.Config{
		Address:    srvA.URL,
		TLSConfig:  &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = tr.Barrier().BarrierSeal(bindSOPIN(t))
	var hostErr *qrdbtransport.SOPINForeignHostError
	if !errors.As(err, &hostErr) {
		t.Fatalf("BarrierSeal with a bound PIN redirected to another host: got %v, want *SOPINForeignHostError", err)
	}
	if n := hitsB.Load(); n != 0 {
		t.Fatalf("requests reaching the redirect target: got %d, want 0", n)
	}
}

// requestFieldPINCalls lists every REST barrier call that carries an SO PIN
// in its request's so_pin field, each made with the PIN in that field only
// (no WithSOPIN binding).
var requestFieldPINCalls = []struct {
	name string
	call func(ctx context.Context, b *BarrierClient) error
}{
	{"BarrierInitialize", func(ctx context.Context, b *BarrierClient) error {
		return b.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{SOPin: "123456"})
	}},
	{"BarrierInitializeShamir", func(ctx context.Context, b *BarrierClient) error {
		_, err := b.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{SOPin: "123456"})
		return err
	}},
	{"BarrierRekey", func(ctx context.Context, b *BarrierClient) error {
		_, err := b.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{SOPin: "123456"})
		return err
	}},
	{"BarrierGenerateRecoveryKeys", func(ctx context.Context, b *BarrierClient) error {
		_, err := b.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{SOPin: "123456"})
		return err
	}},
}

// redirectAllTLSServer starts a TLS server that 307-redirects every request
// but /health to target, keeping the path.
func redirectAllTLSServer(t *testing.T, target string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, target+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newTLSTransport builds a REST transport to srv trusting the given servers'
// certificates.
func newTLSTransport(t *testing.T, srv *httptest.Server, trusted ...*httptest.Server) *Transport {
	t.Helper()
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	for _, s := range trusted {
		pool.AddCert(s.Certificate())
	}
	tr, err := New(&transport.Config{
		Address:    srv.URL,
		TLSConfig:  &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
		MaxRetries: 0,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

// TestBarrierClient_RequestFieldPINRequiresTLS: a PIN carried only in the
// request's SOPin field is refused over http://, as a bound one is.
func TestBarrierClient_RequestFieldPINRequiresTLS(t *testing.T) {
	for _, tc := range requestFieldPINCalls {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			srv := httptest.NewServer(recordingHandler(&hits))
			t.Cleanup(srv.Close)

			tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			err = tc.call(context.Background(), tr.Barrier())
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !errors.As(err, &tlsErr) {
				t.Fatalf("%s with a request-field PIN over http: got %v, want *SOPINRequiresTLSError", tc.name, err)
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("requests reaching the plaintext server: got %d, want 0", n)
			}
		})
	}
}

// TestBarrierClient_RequestFieldPINRefusesPlainRedirect: a request-field PIN
// is not carried across a 307 from the TLS server to a plaintext one.
func TestBarrierClient_RequestFieldPINRefusesPlainRedirect(t *testing.T) {
	for _, tc := range requestFieldPINCalls {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			plain := httptest.NewServer(recordingHandler(&hits))
			t.Cleanup(plain.Close)
			srv := redirectAllTLSServer(t, plain.URL)
			tr := newTLSTransport(t, srv)

			err := tc.call(context.Background(), tr.Barrier())
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !errors.As(err, &tlsErr) {
				t.Fatalf("%s with a request-field PIN redirected to http: got %v, want *SOPINRequiresTLSError", tc.name, err)
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("requests reaching the plaintext redirect target: got %d, want 0", n)
			}
		})
	}
}

// TestBarrierClient_RequestFieldPINRefusesForeignRedirect: a request-field
// PIN is not carried across a 307 to another TLS host.
func TestBarrierClient_RequestFieldPINRefusesForeignRedirect(t *testing.T) {
	for _, tc := range requestFieldPINCalls {
		t.Run(tc.name, func(t *testing.T) {
			var hits atomic.Int32
			other := httptest.NewTLSServer(recordingHandler(&hits))
			t.Cleanup(other.Close)
			srv := redirectAllTLSServer(t, other.URL)
			tr := newTLSTransport(t, srv, other)

			err := tc.call(context.Background(), tr.Barrier())
			var hostErr *qrdbtransport.SOPINForeignHostError
			if !errors.As(err, &hostErr) {
				t.Fatalf("%s with a request-field PIN redirected to another host: got %v, want *SOPINForeignHostError", tc.name, err)
			}
			if n := hits.Load(); n != 0 {
				t.Fatalf("requests reaching the redirect target: got %d, want 0", n)
			}
		})
	}
}

// A transport built WithHTTPClient guards a bound SO PIN too: the call over
// plaintext http is refused before anything is sent. The QUIC transport
// builds its REST client this way.
func TestBarrierClient_SOPINRequiresTLS_SuppliedClient(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(recordingHandler(&hits))
	t.Cleanup(srv.Close)

	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0},
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = tr.Barrier().BarrierSeal(bindSOPIN(t))
	var tlsErr *qrdbtransport.SOPINRequiresTLSError
	if !errors.As(err, &tlsErr) {
		t.Fatalf("BarrierSeal with a bound PIN over http on a supplied client: got %v, want *SOPINRequiresTLSError", err)
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("requests reaching the plaintext server: got %d, want 0", n)
	}
}

// A transport built WithHTTPClient never follows a redirect to another host
// with a bound SO PIN.
func TestBarrierClient_SOPINRefusesForeignRedirect_SuppliedClient(t *testing.T) {
	var hitsB atomic.Int32
	srvB := httptest.NewTLSServer(recordingHandler(&hitsB))
	t.Cleanup(srvB.Close)

	srvA := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Redirect(w, r, srvB.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srvA.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srvA.Certificate())
	pool.AddCert(srvB.Certificate())

	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	tr, err := New(&transport.Config{Address: srvA.URL, MaxRetries: 0}, WithHTTPClient(client))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = tr.Barrier().BarrierSeal(bindSOPIN(t))
	var hostErr *qrdbtransport.SOPINForeignHostError
	if !errors.As(err, &hostErr) {
		t.Fatalf("BarrierSeal with a bound PIN redirected to another host on a supplied client: got %v, want *SOPINForeignHostError", err)
	}
	if n := hitsB.Load(); n != 0 {
		t.Fatalf("requests reaching the redirect target: got %d, want 0", n)
	}
}

// A transport built WithHTTPClient sends a bound SO PIN in the
// X-QRDB-SO-PIN header to the host it was configured with.
func TestTransport_SOPINHeader_SuppliedClient(t *testing.T) {
	var (
		mu  sync.Mutex
		got string
	)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		mu.Lock()
		got = r.Header.Get(qrdbtransport.SOPINHeader)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	client := &http.Client{Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
	}}
	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0}, WithHTTPClient(client))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if err := tr.Barrier().BarrierSeal(bindSOPIN(t)); err != nil {
		t.Fatalf("BarrierSeal with a bound PIN to the configured host: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got != "so-pin-2538" {
		t.Fatalf("%s header: got %q, want the bound PIN", qrdbtransport.SOPINHeader, got)
	}
}

// New never mutates the caller's http.Client: the SO PIN guard goes on a
// copy, so the caller's Transport is the value it set, nil included.
func TestTransport_SuppliedClientNotMutated(t *testing.T) {
	custom := &http.Transport{}
	for _, tc := range []struct {
		name string
		rt   http.RoundTripper
	}{
		{"nil", nil},
		{"custom", custom},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: tc.rt}
			tr, err := New(&transport.Config{Address: "https://127.0.0.1:1", MaxRetries: 0}, WithHTTPClient(client))
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })
			if client.Transport != tc.rt {
				t.Fatalf("caller's client.Transport: got %T %v, want %v", client.Transport, client.Transport, tc.rt)
			}
		})
	}
}

// A call without a bound PIN passes through over plain http unchanged.
func TestBarrierClient_SOPINUnboundPassesThrough(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(recordingHandler(&hits))
	t.Cleanup(srv.Close)

	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if _, err := tr.Barrier().BarrierStatus(context.Background()); err != nil {
		t.Fatalf("BarrierStatus without a bound PIN over http: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("requests reaching the server: got %d, want 1", n)
	}
}

// wireCall is one request a wireServer received: its method, path and the
// JSON body decoded field by field, so a test asserts the exact wire value of
// each field (a PEM string, not its base64 form).
type wireCall struct {
	method string
	path   string
	body   map[string]any
}

// wireServer answers every non-health request with the fixed JSON reply and
// records what it received. useTLS serves HTTPS, which a bound SO PIN needs.
type wireServer struct {
	mu    sync.Mutex
	calls []wireCall
}

func (ws *wireServer) recorded() []wireCall {
	ws.mu.Lock()
	defer ws.mu.Unlock()
	return append([]wireCall(nil), ws.calls...)
}

func newWireServer(t *testing.T, useTLS bool, reply string) (*wireServer, *Transport) {
	t.Helper()
	ws := &wireServer{}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusOK)
			return
		}
		call := wireCall{method: r.Method, path: r.URL.Path}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &call.body); err != nil {
				t.Errorf("request body is not a JSON object: %v (%q)", err, raw)
			}
		}
		ws.mu.Lock()
		ws.calls = append(ws.calls, call)
		ws.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(reply))
	})

	cfg := &transport.Config{MaxRetries: 0}
	var srv *httptest.Server
	if useTLS {
		srv = httptest.NewTLSServer(h)
		pool := x509.NewCertPool()
		pool.AddCert(srv.Certificate())
		cfg.TLSConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	} else {
		srv = httptest.NewServer(h)
	}
	t.Cleanup(srv.Close)
	cfg.Address = srv.URL

	tr, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return ws, tr
}

// onlyCall returns the single request the server received.
func onlyCall(t *testing.T, ws *wireServer, method, path string) wireCall {
	t.Helper()
	calls := ws.recorded()
	if len(calls) != 1 {
		t.Fatalf("requests reaching the server: got %d, want 1", len(calls))
	}
	if calls[0].method != method || calls[0].path != path {
		t.Fatalf("request: got %s %s, want %s %s", calls[0].method, calls[0].path, method, path)
	}
	return calls[0]
}

// wantBody asserts the decoded request body equals want exactly: every field
// present with its wire value, and nothing more.
func wantBody(t *testing.T, got wireCall, want map[string]any) {
	t.Helper()
	if !reflect.DeepEqual(got.body, want) {
		t.Fatalf("request body on the wire:\n got %#v\nwant %#v", got.body, want)
	}
}

const (
	testCSRPEM  = "-----BEGIN CERTIFICATE REQUEST-----\nMIIBCSRbody2538\n-----END CERTIFICATE REQUEST-----\n"
	testCertPEM = "-----BEGIN CERTIFICATE-----\nMIIBCertbody2538\n-----END CERTIFICATE-----\n"
)

// TestBarrierClient_OfficerRegisterWire checks the officer registration
// request carries the CSR as plain PEM text (the upstream csr_pem contract,
// never base64) with every other field, and the response's certificate and
// share index come back as the server sent them.
func TestBarrierClient_OfficerRegisterWire(t *testing.T) {
	cases := []struct {
		name string
		path string
		call func(*BarrierClient, *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error)
	}{
		{"OfficerRegister", "/v1/barrier/officers", func(b *BarrierClient, r *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
			return b.OfficerRegister(context.Background(), r)
		}},
		{"RecoveryOfficerRegister", "/v1/barrier/recovery/officers", func(b *BarrierClient, r *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
			return b.RecoveryOfficerRegister(context.Background(), r)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reply, err := json.Marshal(map[string]any{"cert_pem": testCertPEM, "share_index": 3})
			if err != nil {
				t.Fatal(err)
			}
			ws, tr := newWireServer(t, false, string(reply))

			got, err := tc.call(tr.Barrier(), &qrdbtransport.OfficerRegisterRequest{
				Username:          "officer-a",
				CSRPEM:            []byte(testCSRPEM),
				AuthenticatorType: "piv",
				AuthenticatorRef:  "slot-9a",
			})
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			wantBody(t, onlyCall(t, ws, http.MethodPost, tc.path), map[string]any{
				"username":           "officer-a",
				"csr_pem":            testCSRPEM,
				"authenticator_type": "piv",
				"authenticator_ref":  "slot-9a",
			})
			want := &qrdbtransport.OfficerRegisterResponse{CertPEM: []byte(testCertPEM), ShareIndex: 3}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s response: got %+v, want %+v", tc.name, got, want)
			}
		})
	}
}

// TestBarrierClient_OfficerListWire checks a server list of two fully
// populated officers decodes into equal OfficerStatus values.
func TestBarrierClient_OfficerListWire(t *testing.T) {
	enrolledA := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	claimedA := time.Date(2026, 1, 3, 4, 5, 6, 0, time.UTC)
	enrolledB := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	claimedB := time.Date(2026, 2, 4, 5, 6, 7, 0, time.UTC)
	want := []*qrdbtransport.OfficerStatus{
		{Username: "officer-a", CertThumbprint: "ab12", ShareIndex: 1, AuthFactor: "piv",
			EnrolledAt: enrolledA, ClaimedAt: claimedA, SharePending: false},
		{Username: "officer-b", CertThumbprint: "cd34", ShareIndex: 2, AuthFactor: "fido2",
			EnrolledAt: enrolledB, ClaimedAt: claimedB, SharePending: true},
	}
	reply := `{"officers":[` +
		`{"username":"officer-a","cert_thumbprint":"ab12","share_index":1,"auth_factor":"piv",` +
		`"enrolled_at":"2026-01-02T03:04:05Z","claimed_at":"2026-01-03T04:05:06Z","share_pending":false},` +
		`{"username":"officer-b","cert_thumbprint":"cd34","share_index":2,"auth_factor":"fido2",` +
		`"enrolled_at":"2026-02-03T04:05:06Z","claimed_at":"2026-02-04T05:06:07Z","share_pending":true}]}`

	cases := []struct {
		name string
		path string
		call func(*BarrierClient) ([]*qrdbtransport.OfficerStatus, error)
	}{
		{"OfficerList", "/v1/barrier/officers", func(b *BarrierClient) ([]*qrdbtransport.OfficerStatus, error) {
			return b.OfficerList(context.Background())
		}},
		{"OfficerClaimStatus", "/v1/barrier/officers/claim-status", func(b *BarrierClient) ([]*qrdbtransport.OfficerStatus, error) {
			return b.OfficerClaimStatus(context.Background())
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, tr := newWireServer(t, false, reply)
			got, err := tc.call(tr.Barrier())
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			call := onlyCall(t, ws, http.MethodGet, tc.path)
			if call.body != nil {
				t.Fatalf("%s sent a body: %#v", tc.name, call.body)
			}
			if len(got) != len(want) {
				t.Fatalf("%s: got %d officers, want %d", tc.name, len(got), len(want))
			}
			for i := range want {
				g, w := *got[i], *want[i]
				if !g.EnrolledAt.Equal(w.EnrolledAt) || !g.ClaimedAt.Equal(w.ClaimedAt) {
					t.Fatalf("%s officer %d times: got %v/%v, want %v/%v", tc.name, i,
						g.EnrolledAt, g.ClaimedAt, w.EnrolledAt, w.ClaimedAt)
				}
				g.EnrolledAt, g.ClaimedAt, w.EnrolledAt, w.ClaimedAt = time.Time{}, time.Time{}, time.Time{}, time.Time{}
				if g != w {
					t.Fatalf("%s officer %d: got %+v, want %+v", tc.name, i, g, w)
				}
			}
		})
	}
}

// TestBarrierClient_ShareClaimWire checks the claim handshake's request
// fields reach the wire in the upstream encoding (signature as base64 of its
// bytes, the encoding/json form the upstream client sends) and the response
// fields come back as the server sent them, for the system and recovery
// barriers.
func TestBarrierClient_ShareClaimWire(t *testing.T) {
	expires := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	signature := []byte{0x01, 0x02, 0xfe, 0xff, 0x25, 0x38}
	share := []byte{0xde, 0xad, 0xbe, 0xef, 0x00, 0x7f}

	for _, prefix := range []string{"/v1/barrier/officers/", "/v1/barrier/recovery/officers/"} {
		recovery := prefix != "/v1/barrier/officers/"
		name := "system"
		if recovery {
			name = "recovery"
		}
		t.Run(name+"/begin", func(t *testing.T) {
			ws, tr := newWireServer(t, false, `{"nonce_hex":"a1b2c3","expires_at":"2026-03-04T05:06:07Z"}`)
			req := &qrdbtransport.ShareClaimBeginRequest{Username: "officer-a"}
			var got *qrdbtransport.ShareClaimBeginResponse
			var err error
			if recovery {
				got, err = tr.Barrier().RecoveryShareClaimBegin(context.Background(), req)
			} else {
				got, err = tr.Barrier().ShareClaimBegin(context.Background(), req)
			}
			if err != nil {
				t.Fatalf("claim begin: %v", err)
			}
			wantBody(t, onlyCall(t, ws, http.MethodPost, prefix+"officer-a/claim/begin"),
				map[string]any{"username": "officer-a"})
			if got.NonceHex != "a1b2c3" || !got.ExpiresAt.Equal(expires) {
				t.Fatalf("claim begin response: got %+v, want nonce a1b2c3 expiring %v", got, expires)
			}
		})
		t.Run(name+"/complete", func(t *testing.T) {
			reply, err := json.Marshal(map[string]any{"share_index": 4, "share_bytes": share})
			if err != nil {
				t.Fatal(err)
			}
			// TLS: a claim completion returns a share, so it needs https.
			ws, tr := newWireServer(t, true, string(reply))
			req := &qrdbtransport.ShareClaimCompleteRequest{Username: "officer-a", NonceHex: "a1b2c3", Signature: signature}
			var got *qrdbtransport.ShareClaimCompleteResponse
			if recovery {
				got, err = tr.Barrier().RecoveryShareClaimComplete(context.Background(), req)
			} else {
				got, err = tr.Barrier().ShareClaimComplete(context.Background(), req)
			}
			if err != nil {
				t.Fatalf("claim complete: %v", err)
			}
			wantBody(t, onlyCall(t, ws, http.MethodPost, prefix+"officer-a/claim/complete"), map[string]any{
				"username":  "officer-a",
				"nonce_hex": "a1b2c3",
				"signature": base64.StdEncoding.EncodeToString(signature),
			})
			want := &qrdbtransport.ShareClaimCompleteResponse{ShareIndex: 4, ShareBytes: share}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("claim complete response: got %+v, want %+v", got, want)
			}
		})
	}
}

// TestBarrierClient_SOPINInBodyWire checks each SO PIN-carrying barrier call
// over TLS puts the bound PIN in the body's so_pin field next to the
// request's own fields, and that a request PIN conflicting with the bound one
// is refused with the upstream *SOPINConflictError before anything is sent.
func TestBarrierClient_SOPINInBodyWire(t *testing.T) {
	const boundPIN = "so-pin-2538"
	secret := []byte("barrier-secret")
	cases := []struct {
		name  string
		path  string
		reply string
		// call runs the method with requestPIN in the request (where it has
		// one) and returns the decoded response for comparison.
		call     func(ctx context.Context, b *BarrierClient, requestPIN string) (any, error)
		wantBody map[string]any
		wantResp any
	}{
		{
			name: "BarrierSeal", path: "/v1/barrier/seal", reply: `{}`,
			call: func(ctx context.Context, b *BarrierClient, _ string) (any, error) {
				return nil, b.BarrierSeal(ctx)
			},
			wantBody: map[string]any{"so_pin": boundPIN},
		},
		{
			name: "BarrierInitializeShamir", path: "/v1/barrier/shamir/initialize",
			reply: `{"threshold":2,"total_shares":3,"share_count":3}`,
			call: func(ctx context.Context, b *BarrierClient, pin string) (any, error) {
				return b.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{
					Secret: secret, Threshold: 2, TotalShares: 3, SOPin: pin, UserPin: "user-pin",
				})
			},
			wantBody: map[string]any{
				"secret": base64.StdEncoding.EncodeToString(secret), "threshold": float64(2),
				"total_shares": float64(3), "so_pin": boundPIN, "user_pin": "user-pin",
			},
			wantResp: &qrdbtransport.BarrierInitializeShamirResponse{Threshold: 2, TotalShares: 3, ShareCount: 3},
		},
		{
			name: "BarrierRekey", path: "/v1/barrier/rekey",
			reply: `{"threshold":3,"total_shares":5,"share_count":5}`,
			call: func(ctx context.Context, b *BarrierClient, pin string) (any, error) {
				return b.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{SOPin: pin, Threshold: 3, Total: 5})
			},
			wantBody: map[string]any{"so_pin": boundPIN, "threshold": float64(3), "total": float64(5)},
			wantResp: &qrdbtransport.BarrierRekeyResponse{Threshold: 3, TotalShares: 5, ShareCount: 5},
		},
		{
			name: "BarrierGenerateRecoveryKeys", path: "/v1/barrier/recovery/generate",
			reply: `{"threshold":2,"total":4,"share_count":4}`,
			call: func(ctx context.Context, b *BarrierClient, pin string) (any, error) {
				return b.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{Threshold: 2, Total: 4, SOPin: pin})
			},
			wantBody: map[string]any{"threshold": float64(2), "total": float64(4), "so_pin": boundPIN},
			wantResp: &qrdbtransport.BarrierRecoveryKeysResponse{Threshold: 2, Total: 4, ShareCount: 4},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/bound", func(t *testing.T) {
			ws, tr := newWireServer(t, true, tc.reply)
			got, err := tc.call(bindSOPIN(t), tr.Barrier(), "")
			if err != nil {
				t.Fatalf("%s with a bound PIN over TLS: %v", tc.name, err)
			}
			wantBody(t, onlyCall(t, ws, http.MethodPost, tc.path), tc.wantBody)
			if tc.wantResp != nil && !reflect.DeepEqual(got, tc.wantResp) {
				t.Fatalf("%s response: got %+v, want %+v", tc.name, got, tc.wantResp)
			}
		})
		if tc.name == "BarrierSeal" {
			continue // takes no request, so no request PIN can conflict
		}
		t.Run(tc.name+"/conflict", func(t *testing.T) {
			ws, tr := newWireServer(t, true, tc.reply)
			_, err := tc.call(bindSOPIN(t), tr.Barrier(), "another-pin")
			var conflict *qrdbtransport.SOPINConflictError
			if !errors.As(err, &conflict) {
				t.Fatalf("%s with a conflicting request PIN: got %v, want *SOPINConflictError", tc.name, err)
			}
			if n := len(ws.recorded()); n != 0 {
				t.Fatalf("requests reaching the server: got %d, want 0", n)
			}
		})
	}
}
