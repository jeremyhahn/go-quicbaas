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

// transport_barrier_secret_tls_test.go pins that every barrier verb carrying
// a secret other than the SO PIN (an initialize or unseal secret, a decrypted
// key share, an enable-encryption passphrase) refuses an http:// endpoint
// before any request is sent, and sends the secret over https://.

package mcp

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"strings"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// barrierSecret is the secret every case carries; no error text may hold it.
const barrierSecret = "correct horse battery staple"

// secretVerbCase is one barrier verb that carries a secret: call sends
// secret, and arg is the tool argument the secret travels in.
type secretVerbCase struct {
	name string
	tool string
	arg  string
	call func(ctx context.Context, tr *Transport, secret []byte) error
}

var secretVerbCases = []secretVerbCase{
	{"BarrierInitialize", "qrdb_barrier_initialize", "secret", func(ctx context.Context, tr *Transport, secret []byte) error {
		return tr.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{Secret: secret})
	}},
	{"BarrierUnseal", "qrdb_barrier_unseal", "secret", func(ctx context.Context, tr *Transport, secret []byte) error {
		return tr.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{Secret: secret})
	}},
	{"BarrierQuorumSubmit", "qrdb_barrier_quorum_submit", "share", func(ctx context.Context, tr *Transport, secret []byte) error {
		_, err := tr.BarrierQuorumSubmit(ctx, &qrdbtransport.QuorumSubmitRequest{
			Domain: "unseal", Op: "unseal", Username: "alice", ChallengeID: "ch-1", Factor: "mtls", Share: secret})
		return err
	}},
	{"BarrierEnableEncryption", "qrdb_barrier_enable_encryption", "passphrase", func(ctx context.Context, tr *Transport, secret []byte) error {
		_, err := tr.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{Strategy: "passphrase", Passphrase: secret})
		return err
	}},
	{"BarrierEnableEncryption tenant", "qrdb_tenant_barrier_enable_encryption", "passphrase", func(ctx context.Context, tr *Transport, secret []byte) error {
		_, err := tr.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{TenantID: "acme", Strategy: "passphrase", Passphrase: secret})
		return err
	}},
}

func TestBarrierSecretVerbsRefusedOverPlaintext(t *testing.T) {
	for _, tc := range secretVerbCases {
		t.Run(tc.name, func(t *testing.T) {
			tr, called, cleanup := newMultiToolMCPServer(t, []string{tc.tool}, nil)
			defer cleanup()

			err := tc.call(context.Background(), tr, []byte(barrierSecret))
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !stderrors.As(err, &tlsErr) {
				t.Fatalf("expected SOPINRequiresTLSError, got %v", err)
			}
			if len(*called) != 0 {
				t.Fatalf("the secret must not be sent over http://, server saw %v", *called)
			}
			encoded := base64.StdEncoding.EncodeToString([]byte(barrierSecret))
			if msg := err.Error(); strings.Contains(msg, barrierSecret) || strings.Contains(msg, encoded) {
				t.Fatalf("error text %q carries the secret", msg)
			}
		})
	}
}

func TestBarrierSecretVerbsSendSecretOverTLS(t *testing.T) {
	for _, tc := range secretVerbCases {
		t.Run(tc.name, func(t *testing.T) {
			var received interface{}
			tr, called := newMultiToolMCPServerTLS(t, []string{tc.tool}, func(_ *testing.T, _ string, args map[string]interface{}) map[string]interface{} {
				received = args[tc.arg]
				return map[string]interface{}{"ok": true}
			})

			if err := tc.call(context.Background(), tr, []byte(barrierSecret)); err != nil {
				t.Fatalf("%s over https://: %v", tc.name, err)
			}
			if len(*called) != 1 {
				t.Fatalf("server saw %v, want one call to %s", *called, tc.tool)
			}
			// The share travels inside a remarshalled request, so JSON
			// renders its bytes as base64; the other secrets are strings.
			want := []string{barrierSecret, base64.StdEncoding.EncodeToString([]byte(barrierSecret))}
			if received != want[0] && received != want[1] {
				t.Fatalf("%s argument = %#v, want the secret", tc.arg, received)
			}
		})
	}
}

// TestBarrierVerbsWithoutSecretWorkOverPlaintext pins that the refusal is
// about the secret, not the verb: every guarded verb carrying no secret
// still reaches an http:// server.
func TestBarrierVerbsWithoutSecretWorkOverPlaintext(t *testing.T) {
	tests := []struct {
		name string
		tool string
		call func(ctx context.Context, tr *Transport) error
	}{
		{"BarrierInitialize", "qrdb_barrier_initialize", func(ctx context.Context, tr *Transport) error {
			return tr.BarrierInitialize(ctx, &qrdbtransport.BarrierInitializeRequest{})
		}},
		{"BarrierUnseal", "qrdb_barrier_unseal", func(ctx context.Context, tr *Transport) error {
			return tr.BarrierUnseal(ctx, &qrdbtransport.BarrierUnsealRequest{})
		}},
		{"BarrierSeal", "qrdb_barrier_seal", func(ctx context.Context, tr *Transport) error {
			return tr.BarrierSeal(ctx)
		}},
		{"BarrierInitializeShamir", "qrdb_barrier_shamir_initialize", func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{Threshold: 2, TotalShares: 3})
			return err
		}},
		{"BarrierGenerateRecoveryKeys", "qrdb_barrier_recovery_generate", func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{Threshold: 2})
			return err
		}},
		{"BarrierQuorumSubmit factor only", "qrdb_barrier_quorum_submit", func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierQuorumSubmit(ctx, &qrdbtransport.QuorumSubmitRequest{
				Domain: "unseal", Op: "unseal", Username: "alice", ChallengeID: "ch-1", Factor: "mtls"})
			return err
		}},
		{"BarrierRekeyStart", "qrdb_barrier_rekey_start", func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierRekeyStart(ctx, &qrdbtransport.RekeyStartRequest{})
			return err
		}},
		{"BarrierOfficerRevoke", "qrdb_barrier_officer_revoke", func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierOfficerRevoke(ctx, &qrdbtransport.OfficerRevokeRequest{})
			return err
		}},
		{"BarrierEnableEncryption shamir", "qrdb_barrier_enable_encryption", func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{
				Strategy: "shamir", ShamirThreshold: 2, ShamirTotal: 3})
			return err
		}},
		{"BarrierEnableEncryption tenant shamir", "qrdb_tenant_barrier_enable_encryption", func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierEnableEncryption(ctx, &qrdbtransport.BarrierEnableEncryptionRequest{
				TenantID: "acme", Strategy: "shamir", ShamirThreshold: 2, ShamirTotal: 3})
			return err
		}},
		{"CeremonyInitialize", "qrdb_ceremony_initialize", func(ctx context.Context, tr *Transport) error {
			_, err := tr.CeremonyInitialize(ctx, &qrdbtransport.CeremonyInitializeRequest{Threshold: 1})
			return err
		}},
		{"CeremonySignCSR", "qrdb_ceremony_sign_csr", func(ctx context.Context, tr *Transport) error {
			_, err := tr.CeremonySignCSR(ctx, &qrdbtransport.SignCSRInitRequest{Username: "officer"})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, called, cleanup := newMultiToolMCPServer(t, []string{tc.tool}, func(*testing.T, string, map[string]interface{}) map[string]interface{} {
				return map[string]interface{}{"ok": true}
			})
			defer cleanup()

			if err := tc.call(context.Background(), tr); err != nil {
				t.Fatalf("%s over http://: %v", tc.name, err)
			}
			if len(*called) != 1 || (*called)[0] != tc.tool {
				t.Fatalf("server saw %v, want one call to %s", *called, tc.tool)
			}
		})
	}
}

// TestUserPINVerbsRefusedOverPlaintext pins that a user PIN is guarded like
// the SO PIN: a verb carrying only a user PIN refuses an http:// endpoint
// before any tool call reaches the server.
func TestUserPINVerbsRefusedOverPlaintext(t *testing.T) {
	const pin = "user-pin-5678"
	tests := []struct {
		name string
		tool string
		call func(ctx context.Context, tr *Transport) error
	}{
		{"BarrierInitializeShamir", "qrdb_barrier_shamir_initialize", func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{Threshold: 2, TotalShares: 3, UserPin: pin})
			return err
		}},
		{"CeremonyInitialize", "qrdb_ceremony_initialize", func(ctx context.Context, tr *Transport) error {
			_, err := tr.CeremonyInitialize(ctx, &qrdbtransport.CeremonyInitializeRequest{UserPin: pin, Threshold: 1})
			return err
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tr, called, cleanup := newMultiToolMCPServer(t, []string{tc.tool}, func(*testing.T, string, map[string]interface{}) map[string]interface{} {
				return map[string]interface{}{"ok": true}
			})
			defer cleanup()

			err := tc.call(context.Background(), tr)
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !stderrors.As(err, &tlsErr) {
				t.Fatalf("expected SOPINRequiresTLSError, got %v", err)
			}
			if len(*called) != 0 {
				t.Fatalf("the user PIN must not be sent over http://, server saw %v", *called)
			}
			if strings.Contains(err.Error(), pin) {
				t.Fatalf("error text %q carries the user PIN", err.Error())
			}
		})
	}
}
