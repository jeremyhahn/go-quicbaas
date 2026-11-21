// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package deploy

import (
	"context"
	"crypto/fips140"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// TestApplyFIPSAlgorithms checks that the helper constrains the client's key
// exchanges, ciphers and MACs to the FIPS-approved sets when — and only when —
// the process runs in FIPS mode. fips140.Enabled() is fixed at process start by
// GODEBUG, so the assertion is selected to match the ambient mode: the FIPS job
// exercises the constrained branch, the ordinary test job the default branch.
func TestApplyFIPSAlgorithms(t *testing.T) {
	cfg := &ssh.ClientConfig{User: "u"}
	applyFIPSAlgorithms(cfg)

	if fips140.Enabled() {
		assertExactList(t, "KeyExchanges", cfg.KeyExchanges, fipsApprovedKeyExchanges)
		assertExactList(t, "Ciphers", cfg.Ciphers, fipsApprovedCiphers)
		assertExactList(t, "MACs", cfg.MACs, fipsApprovedMACs)

		// Guard the specific unapproved algorithms this fix removes: if any
		// were still offered the handshake would reach them and the module
		// would refuse (#114 classes: X25519, AES-GCM arbitrary IV, ChaCha).
		forbidden := map[string]bool{
			ssh.KeyExchangeCurve25519:  true,
			ssh.CipherAES256GCM:        true,
			ssh.CipherAES128GCM:        true,
			ssh.CipherChaCha20Poly1305: true,
			ssh.HMACSHA1:               true,
		}
		for _, group := range [][]string{cfg.KeyExchanges, cfg.Ciphers, cfg.MACs} {
			for _, algo := range group {
				if forbidden[algo] {
					t.Fatalf("FIPS mode: unapproved algorithm %q must never be offered", algo)
				}
			}
		}
		return
	}

	if cfg.KeyExchanges != nil || cfg.Ciphers != nil || cfg.MACs != nil {
		t.Fatalf("non-FIPS mode: algorithms must stay nil (x/crypto defaults), got kex=%v ciphers=%v macs=%v",
			cfg.KeyExchanges, cfg.Ciphers, cfg.MACs)
	}
}

// assertExactList fails unless got equals want element-for-element and in
// order, so a reordering or a dropped algorithm is caught, not just a length
// change.
func assertExactList(t *testing.T, field string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", field, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %q, want %q", field, i, got[i], want[i])
		}
	}
}

// TestApplyFIPSAlgorithms_NilConfig documents that the helper is a no-op on a
// nil config rather than panicking.
func TestApplyFIPSAlgorithms_NilConfig(t *testing.T) {
	applyFIPSAlgorithms(nil) // must not panic
}

// TestDialSSH_NegotiatesApprovedKex proves the client can complete a handshake
// against a peer that offers ONLY a FIPS-approved key exchange. Because the
// server advertises nothing but ecdh-sha2-nistp256, a successful dial means
// that algorithm was negotiated — the assertion is on the negotiated kex by
// construction, not merely on the absence of an error. This holds in both
// modes: under FIPS it shows the constrained client still offers P-256; in the
// ordinary build it shows the default client is a superset.
func TestDialSSH_NegotiatesApprovedKex(t *testing.T) {
	srv := newTestSSHServerKex(t, "alice", "s3cret", []string{ssh.KeyExchangeECDHP256})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := &Spec{
		Host:            srv.addr(),
		SSHUser:         "alice",
		SSHPassword:     "s3cret",
		InsecureHostKey: true,
	}
	client, err := DialSSH(ctx, spec)
	if err != nil {
		t.Fatalf("DialSSH against a P-256-only server: %v", err)
	}
	defer client.Close()
	if client.UnderlyingClient() == nil {
		t.Fatalf("UnderlyingClient() nil after successful handshake")
	}
}

// TestDialSSH_X25519OnlyPeer pins the fail-closed contract. Against a peer that
// offers ONLY X25519, the client must refuse to connect under FIPS (X25519 is
// unapproved and never offered, so there is no common key exchange), surfacing
// a typed SSHDialError; and it must still connect on a non-FIPS host, proving
// the default preference order is left untouched off the FIPS path.
func TestDialSSH_X25519OnlyPeer(t *testing.T) {
	srv := newTestSSHServerKex(t, "alice", "s3cret", []string{ssh.KeyExchangeCurve25519})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	spec := &Spec{
		Host:            srv.addr(),
		SSHUser:         "alice",
		SSHPassword:     "s3cret",
		InsecureHostKey: true,
	}
	client, err := DialSSH(ctx, spec)

	if fips140.Enabled() {
		if err == nil {
			_ = client.Close()
			t.Fatalf("FIPS mode: DialSSH to an X25519-only peer must fail closed, got success")
		}
		var de *SSHDialError
		if !errors.As(err, &de) {
			t.Fatalf("FIPS mode: want *SSHDialError, got %T: %v", err, err)
		}
		return
	}

	if err != nil {
		t.Fatalf("non-FIPS mode: DialSSH to an X25519-only peer should succeed, got %v", err)
	}
	defer client.Close()
}
