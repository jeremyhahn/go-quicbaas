package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
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
