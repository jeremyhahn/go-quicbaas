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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"net"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestDialSSHWithHostKey_AcceptsTheVerifiedHost is the secure deploy
// path: password credentials from the spec, host identity from a
// policy the service resolved out of the known-hosts store. No
// InsecureHostKey anywhere.
func TestDialSSHWithHostKey_AcceptsTheVerifiedHost(t *testing.T) {
	srv := newTestSSHServer(t, "deploy", "pw")
	ctx := context.Background()

	client, err := DialSSHWithHostKey(ctx, &Spec{
		Host:        srv.addr(),
		SSHUser:     "deploy",
		SSHPassword: "pw",
	}, &HostKeyPolicy{Callback: ssh.FixedHostKey(srv.hostKey.PublicKey())})
	if err != nil {
		t.Fatalf("DialSSHWithHostKey: %v", err)
	}
	defer func() { _ = client.Close() }()

	if client.Host() == "" {
		t.Fatal("Host() empty")
	}
}

// TestDialSSHWithHostKey_RejectsAnUnverifiedHost covers the rotated /
// impersonated host: the callback refuses the presented key and the
// dial fails rather than uploading an agent identity to a stranger.
func TestDialSSHWithHostKey_RejectsAnUnverifiedHost(t *testing.T) {
	srv := newTestSSHServer(t, "deploy", "pw")
	ctx := context.Background()
	imposter := generateTestHostKey(t)

	_, err := DialSSHWithHostKey(ctx, &Spec{
		Host:        srv.addr(),
		SSHUser:     "deploy",
		SSHPassword: "pw",
	}, &HostKeyPolicy{Callback: ssh.FixedHostKey(imposter.PublicKey())})
	if err == nil {
		t.Fatal("expected the dial to fail on an unverified host key")
	}
	var dialErr *SSHDialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("want SSHDialError, got %T: %v", err, err)
	}
}

// TestDialSSHWithHostKey_InvalidSpec covers the inputs the primitive
// must refuse before it opens a socket. The nil-policy case is the
// one the CLI defect turned on: no pin, no opt-out, no connection.
func TestDialSSHWithHostKey_InvalidSpec(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		spec    *Spec
		hostKey *HostKeyPolicy
		field   string
	}{
		{"nil spec", nil, nil, "spec"},
		{
			name:  "no credentials",
			spec:  &Spec{Host: "127.0.0.1:22", SSHUser: "deploy"},
			field: "ssh_password",
		},
		{
			name:  "no policy and no opt-out",
			spec:  &Spec{Host: "127.0.0.1:22", SSHUser: "deploy", SSHPassword: "pw"},
			field: "ssh_host_key",
		},
		{
			// A policy without a callback is a caller bug; it must not
			// quietly fall back to the spec's policy and drop the
			// algorithms it asked for.
			name:    "policy without a callback",
			spec:    &Spec{Host: "127.0.0.1:22", SSHUser: "deploy", SSHPassword: "pw", InsecureHostKey: true},
			hostKey: &HostKeyPolicy{Algorithms: []string{ssh.KeyAlgoED25519}},
			field:   "ssh_host_key",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DialSSHWithHostKey(ctx, tc.spec, tc.hostKey)
			var specErr *InvalidSpecError
			if !errors.As(err, &specErr) {
				t.Fatalf("want InvalidSpecError, got %T: %v", err, err)
			}
			if specErr.Field != tc.field {
				t.Fatalf("field = %q, want %q", specErr.Field, tc.field)
			}
		})
	}
}

// TestDialSSHWithHostKey_NilPolicyHonoursTheInsecureOptOut keeps the
// documented escape hatch working: with InsecureHostKey the spec's own
// policy applies and no host-key policy is needed.
func TestDialSSHWithHostKey_NilPolicyHonoursTheInsecureOptOut(t *testing.T) {
	srv := newTestSSHServer(t, "deploy", "pw")

	client, err := DialSSHWithHostKey(context.Background(), &Spec{
		Host:            srv.addr(),
		SSHUser:         "deploy",
		SSHPassword:     "pw",
		InsecureHostKey: true,
	}, nil)
	if err != nil {
		t.Fatalf("DialSSHWithHostKey: %v", err)
	}
	_ = client.Close()
}

// TestDialSSHWithHostKey_NegotiatesThePolicyAlgorithms is the remote fleet node's failure
// in miniature: the host serves an ECDSA key and an ed25519 key, and the
// pin is for ed25519. The policy's Algorithms decide which key the host
// presents — asked for ed25519 it presents the pinned key and the dial
// succeeds; asked for ECDSA it presents a key the pin does not cover and
// the callback refuses it.
func TestDialSSHWithHostKey_NegotiatesThePolicyAlgorithms(t *testing.T) {
	ecdsaKey := generateTestECDSAHostKey(t)
	srv := newTestSSHServerKex(t, "deploy", "pw", nil, ecdsaKey)
	spec := &Spec{Host: srv.addr(), SSHUser: "deploy", SSHPassword: "pw"}
	pinned := ssh.FixedHostKey(srv.hostKey.PublicKey())

	client, err := DialSSHWithHostKey(context.Background(), spec, &HostKeyPolicy{
		Callback:   pinned,
		Algorithms: []string{ssh.KeyAlgoED25519},
	})
	if err != nil {
		t.Fatalf("dial asking for the pinned ed25519 key: %v", err)
	}
	_ = client.Close()

	_, err = DialSSHWithHostKey(context.Background(), spec, &HostKeyPolicy{
		Callback:   pinned,
		Algorithms: []string{ssh.KeyAlgoECDSA256},
	})
	var dialErr *SSHDialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("asking for the unpinned ECDSA key must fail the dial, got %T: %v", err, err)
	}
}

// TestNewClientConfig_HostKeyAlgorithms checks the policy's Algorithms land
// on the client configuration verbatim, and that an empty list or no policy
// leaves x/crypto/ssh's default negotiation in place.
func TestNewClientConfig_HostKeyAlgorithms(t *testing.T) {
	accept := func(string, net.Addr, ssh.PublicKey) error { return nil }
	spec := &Spec{Host: "h", SSHUser: "u", InsecureHostKey: true}
	auth := []ssh.AuthMethod{ssh.Password("p")}
	cases := []struct {
		name   string
		policy *HostKeyPolicy
		want   []string
	}{
		{"no policy", nil, nil},
		{"no algorithms", &HostKeyPolicy{Callback: accept}, nil},
		{
			name:   "pinned algorithms",
			policy: &HostKeyPolicy{Callback: accept, Algorithms: []string{ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA512}},
			want:   []string{ssh.KeyAlgoED25519, ssh.KeyAlgoRSASHA512},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := newClientConfig(spec, auth, tc.policy)
			if err != nil {
				t.Fatalf("newClientConfig: %v", err)
			}
			if cfg.HostKeyCallback == nil {
				t.Fatal("HostKeyCallback must always be set")
			}
			if tc.want == nil {
				if cfg.HostKeyAlgorithms != nil {
					t.Fatalf("HostKeyAlgorithms = %v, want the library default (nil)", cfg.HostKeyAlgorithms)
				}
				return
			}
			assertExactList(t, "HostKeyAlgorithms", cfg.HostKeyAlgorithms, tc.want)
		})
	}
}

// generateTestECDSAHostKey returns an ephemeral P-256 host key, the key
// type x/crypto/ssh negotiates ahead of ed25519 by default.
func generateTestECDSAHostKey(t *testing.T) ssh.Signer {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen ecdsa key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("new ecdsa signer: %v", err)
	}
	return signer
}
