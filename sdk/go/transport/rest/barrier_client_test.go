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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// TestBarrierClient_MethodAndPath pins each barrier verb to the HTTP method
// and path go-qrdb's REST transport sends (go-quicbaas#2533).
func TestBarrierClient_MethodAndPath(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		call   func(ctx context.Context, b *BarrierClient) error
	}{
		{"BarrierInitialize", http.MethodPost, "/v1/barrier/initialize", func(ctx context.Context, b *BarrierClient) error {
			return b.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{})
		}},
		{"BarrierUnseal", http.MethodPost, "/v1/barrier/unseal", func(ctx context.Context, b *BarrierClient) error {
			return b.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{})
		}},
		{"BarrierSeal", http.MethodPost, "/v1/barrier/seal", func(ctx context.Context, b *BarrierClient) error {
			return b.BarrierSeal(ctx)
		}},
		{"BarrierStatus", http.MethodGet, "/v1/barrier/status", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.BarrierStatus(ctx)
			return err
		}},
		{"BarrierInitializeShamir", http.MethodPost, "/v1/barrier/shamir/initialize", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{})
			return err
		}},
		{"BarrierUnsealWithShare", http.MethodPost, "/v1/barrier/shamir/unseal-share", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.BarrierUnsealWithShare(ctx, &qrdbtransport.BarrierUnsealShareRequest{})
			return err
		}},
		{"BarrierUnsealWithShares", http.MethodPost, "/v1/barrier/shamir/unseal-shares", func(ctx context.Context, b *BarrierClient) error {
			return b.BarrierUnsealWithShares(ctx, &qrdbtransport.BarrierUnsealSharesRequest{})
		}},
		{"BarrierRekey", http.MethodPost, "/v1/barrier/rekey", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{})
			return err
		}},
		{"BarrierEnableEncryption", http.MethodPost, "/v1/barrier/enable-encryption", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{})
			return err
		}},
		{"BarrierGenerateRecoveryKeys", http.MethodPost, "/v1/barrier/recovery/generate", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{})
			return err
		}},
		{"BarrierRecoverWithKeys", http.MethodPost, "/v1/barrier/recovery/recover", func(ctx context.Context, b *BarrierClient) error {
			return b.BarrierRecoverWithKeys(ctx, &qrdbtransport.BarrierRecoverWithKeysRequest{})
		}},
		{"BarrierDeleteRecoveryKeys", http.MethodDelete, "/v1/barrier/recovery", func(ctx context.Context, b *BarrierClient) error {
			return b.BarrierDeleteRecoveryKeys(ctx)
		}},
		{"BarrierHasRecoveryKeys", http.MethodGet, "/v1/barrier/recovery/status", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.BarrierHasRecoveryKeys(ctx)
			return err
		}},
		{"BarrierGenerateRootToken", http.MethodPost, "/v1/barrier/root-token", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.BarrierGenerateRootToken(ctx, &qrdbtransport.BarrierGenerateRootTokenRequest{})
			return err
		}},
		{"OfficerRegister", http.MethodPost, "/v1/barrier/officers", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.OfficerRegister(ctx, &qrdbtransport.OfficerRegisterRequest{Username: "so1"})
			return err
		}},
		{"OfficerList", http.MethodGet, "/v1/barrier/officers", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.OfficerList(ctx)
			return err
		}},
		{"OfficerClaimStatus", http.MethodGet, "/v1/barrier/officers/claim-status", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.OfficerClaimStatus(ctx)
			return err
		}},
		{"ShareClaimBegin", http.MethodPost, "/v1/barrier/officers/so1/claim/begin", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.ShareClaimBegin(ctx, &qrdbtransport.ShareClaimBeginRequest{Username: "so1"})
			return err
		}},
		{"ShareClaimComplete", http.MethodPost, "/v1/barrier/officers/so1/claim/complete", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.ShareClaimComplete(ctx, &qrdbtransport.ShareClaimCompleteRequest{Username: "so1"})
			return err
		}},
		{"ShamirSealVote", http.MethodPost, "/v1/barrier/shamir/seal-vote", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.ShamirSealVote(ctx, &qrdbtransport.ShamirSealVoteRequest{})
			return err
		}},
		{"RecoveryOfficerRegister", http.MethodPost, "/v1/barrier/recovery/officers", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.RecoveryOfficerRegister(ctx, &qrdbtransport.OfficerRegisterRequest{Username: "so1"})
			return err
		}},
		{"RecoveryShareClaimBegin", http.MethodPost, "/v1/barrier/recovery/officers/so1/claim/begin", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.RecoveryShareClaimBegin(ctx, &qrdbtransport.ShareClaimBeginRequest{Username: "so1"})
			return err
		}},
		{"RecoveryShareClaimComplete", http.MethodPost, "/v1/barrier/recovery/officers/so1/claim/complete", func(ctx context.Context, b *BarrierClient) error {
			_, err := b.RecoveryShareClaimComplete(ctx, &qrdbtransport.ShareClaimCompleteRequest{Username: "so1"})
			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotMethod, gotPath string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotPath = r.Method, r.URL.Path
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{}`))
			}))
			defer srv.Close()

			b := newTestTransport(t, srv).Barrier()
			if err := tc.call(context.Background(), b); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if gotMethod != tc.method || gotPath != tc.path {
				t.Errorf("%s: got %s %s, want %s %s", tc.name, gotMethod, gotPath, tc.method, tc.path)
			}
		})
	}
}
