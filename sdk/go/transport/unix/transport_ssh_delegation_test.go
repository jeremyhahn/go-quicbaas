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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/test/sshfixture"
)

// TestTransport_SSH_Delegates proves the Unix transport routes the SSH
// surface through the gRPC SSH handler.
//
// Unix is gRPC on a domain socket, so the server registers ONE SSHService for
// both. With no server listening and a short deadline every call fails on
// connection — but crucially NOT with "not supported" or "unknown ssh
// method", which would mean the dispatch table never saw the request. The
// gRPC handler tests assert the full proto round-trip; this pins the
// delegation contract.
func TestTransport_SSH_Delegates(t *testing.T) {
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"generate", sshclient.MethodGenerateKey, &sshclient.GenerateRequest{Scope: "user", Owner: "alice", Name: "laptop"}},
		{"import", sshclient.MethodImportKey, &sshclient.ImportKeyRequest{Name: "laptop", PEM: []byte("pem")}},
		{"get", sshclient.MethodGetKey, &sshclient.GetKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"}},
		{"list", sshclient.MethodListKeys, &sshclient.ListKeysRequest{Scope: "user", Owner: "alice"}},
		{"delete", sshclient.MethodDeleteKey, &sshclient.DeleteKeyRequest{Scope: "user", Owner: "alice", Name: "laptop"}},
		{"export", sshclient.MethodExportPublicKey, &sshclient.ExportRequest{Scope: "user", Owner: "alice", Name: "laptop"}},
		{"sign", sshclient.MethodSignDigest, &sshclient.SignRequest{Name: "laptop", Digest: []byte("d")}},
		{"known-host-add", sshclient.MethodAddKnownHost, &sshclient.AddKnownHostRequest{
			Host: "h", Fingerprint: sshfixture.HostFingerprint}},
		{"known-host-list", sshclient.MethodListKnownHosts, &sshclient.ListKnownHostsRequest{}},
		{"known-host-remove", sshclient.MethodRemoveKnownHost, &sshclient.RemoveKnownHostRequest{Host: "h"}},
		{"authorized-add", sshclient.MethodAddAuthorizedKey, &sshclient.AddAuthorizedKeyRequest{Name: "n", Line: "l"}},
		{"authorized-list", sshclient.MethodListAuthorizedKeys, &sshclient.ListAuthorizedKeysRequest{}},
		{"authorized-remove", sshclient.MethodRemoveAuthorizedKey, &sshclient.RemoveAuthorizedKeyRequest{Name: "n"}},
		{"authorized-emit", sshclient.MethodEmitAuthorizedKey, &sshclient.EmitRequest{PublicKey: "ssh-ed25519 AAAA"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, sshclient.ServiceName, tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("ssh request was rejected as unsupported, dispatch regression: %v", err)
			}
			if strings.Contains(msg, "unknown ssh method") {
				t.Fatalf("ssh method %q is not in the gRPC dispatch table: %v", tc.method, err)
			}
			if strings.Contains(msg, "invalid request type") {
				t.Fatalf("ssh method %q rejected its own SDK request type: %v", tc.method, err)
			}
		})
	}
}

// TestTransport_SSH_ClosedTransport proves a closed Unix transport rejects
// SSH calls instead of dialing a dead socket.
func TestTransport_SSH_ClosedTransport(t *testing.T) {
	tr := newWiredTransport(t)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err := tr.Request(context.Background(), sshclient.ServiceName,
		sshclient.MethodListKeys, &sshclient.ListKeysRequest{})
	if err == nil {
		t.Fatal("expected a closed-transport error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected a closed-transport error, got: %v", err)
	}
}
