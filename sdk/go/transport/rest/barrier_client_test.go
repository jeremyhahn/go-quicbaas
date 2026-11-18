package rest

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

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
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

			tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0}, WithAuthToken(token))
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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/health" {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(status)
		}))
		t.Cleanup(srv.Close)

		tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0})
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
