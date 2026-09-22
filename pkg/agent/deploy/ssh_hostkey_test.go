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
	"errors"
	"testing"

	"golang.org/x/crypto/ssh"
)

// TestDialSSHWithHostKey_AcceptsTheVerifiedHost is the secure deploy
// path: password credentials from the spec, host identity from a
// callback the service resolved out of the known-hosts store. No
// InsecureHostKey anywhere.
func TestDialSSHWithHostKey_AcceptsTheVerifiedHost(t *testing.T) {
	srv := newTestSSHServer(t, "deploy", "pw")
	ctx := context.Background()

	client, err := DialSSHWithHostKey(ctx, &Spec{
		Host:        srv.addr(),
		SSHUser:     "deploy",
		SSHPassword: "pw",
	}, ssh.FixedHostKey(srv.hostKey.PublicKey()))
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
	}, ssh.FixedHostKey(imposter.PublicKey()))
	if err == nil {
		t.Fatal("expected the dial to fail on an unverified host key")
	}
	var dialErr *SSHDialError
	if !errors.As(err, &dialErr) {
		t.Fatalf("want SSHDialError, got %T: %v", err, err)
	}
}

// TestDialSSHWithHostKey_InvalidSpec covers the inputs the primitive
// must refuse before it opens a socket. The nil-callback case is the
// one the CLI defect turned on: no pin, no opt-out, no connection.
func TestDialSSHWithHostKey_InvalidSpec(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name    string
		spec    *Spec
		hostKey ssh.HostKeyCallback
		field   string
	}{
		{"nil spec", nil, nil, "spec"},
		{
			name:  "no credentials",
			spec:  &Spec{Host: "127.0.0.1:22", SSHUser: "deploy"},
			field: "ssh_password",
		},
		{
			name:  "no callback and no opt-out",
			spec:  &Spec{Host: "127.0.0.1:22", SSHUser: "deploy", SSHPassword: "pw"},
			field: "ssh_host_key",
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

// TestDialSSHWithHostKey_NilCallbackHonoursTheInsecureOptOut keeps the
// documented escape hatch working: with InsecureHostKey the spec's own
// policy applies and no callback is needed.
func TestDialSSHWithHostKey_NilCallbackHonoursTheInsecureOptOut(t *testing.T) {
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
