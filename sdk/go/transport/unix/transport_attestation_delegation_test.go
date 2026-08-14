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

	attestationclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/attestation"
)

// TestTransport_Attestation_Delegates proves the Unix transport routes the
// Attestation surface through the gRPC attestation handler.
//
// Unix is gRPC on a domain socket, so the server registers ONE
// AttestationService for both. With no server listening and a short deadline
// every call fails on connection — but crucially NOT with "unimplemented" or
// "invalid request type", which would mean the dispatch table never saw the
// request or rejected the SDK's own types. The gRPC handler tests assert the
// proto conversion; this pins the delegation contract.
func TestTransport_Attestation_Delegates(t *testing.T) {
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{
			name:   "verify",
			method: "Verify",
			req: &attestationclient.VerifyRequest{
				Format:         "tpm2",
				ChainPEM:       [][]byte{[]byte("cert")},
				AttestedKeyDER: []byte("key"),
				Signature:      []byte("sig"),
			},
		},
		{
			name:   "verify-chain",
			method: "VerifyChain",
			req: &attestationclient.VerifyChainRequest{
				Format:   "tpm2",
				ChainPEM: [][]byte{[]byte("cert")},
			},
		},
		{
			name:   "attest-container",
			method: "AttestContainer",
			req: &attestationclient.AttestContainerRequest{
				Image: "alpine@sha256:abc",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, attestationclient.ServiceName, tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("attestation request was rejected as unsupported, dispatch regression: %v", err)
			}
			if strings.Contains(msg, "not exposed by AttestationService") {
				t.Fatalf("attestation method %q is not in the gRPC dispatch table: %v", tc.method, err)
			}
			if strings.Contains(msg, "invalid request type") {
				t.Fatalf("attestation method %q rejected its own SDK request type: %v", tc.method, err)
			}
		})
	}
}

// TestTransport_Attestation_ClosedTransport proves a closed Unix transport
// rejects attestation calls instead of dialing a dead socket.
func TestTransport_Attestation_ClosedTransport(t *testing.T) {
	tr := newWiredTransport(t)
	if err := tr.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err := tr.Request(context.Background(), attestationclient.ServiceName, "Verify",
		&attestationclient.VerifyRequest{
			Format:    "tpm2",
			ChainPEM:  [][]byte{[]byte("cert")},
			Signature: []byte("sig"),
		})
	if err == nil {
		t.Fatal("expected a closed-transport error")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Fatalf("expected a closed-transport error, got: %v", err)
	}
}
