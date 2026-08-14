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

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestTransport_RotateSessionAccount_Delegates proves the Unix transport routes
// the operator-initiated rotation verb through the gRPC orchestrator handler.
//
// Unix is gRPC on a domain socket, so the server registers ONE
// OrchestratorService for both. With no server listening and a short deadline
// the call fails on connection — but crucially NOT with "unknown orchestrator
// method" or "not supported", which is the Phase 8 regression this guards.
func TestTransport_RotateSessionAccount_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestratorclient.RotateSessionAccountRequest{
			SessionID:    "s1",
			Reason:       "seat retired",
			HoldOutgoing: true,
		})
	if err == nil {
		t.Fatal("expected a connection error: no server is listening")
	}

	msg := err.Error()
	if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
		t.Fatalf("rotation was rejected as unsupported, dispatch regression: %v", err)
	}
	if strings.Contains(msg, "invalid request type") {
		t.Fatalf("the gRPC handler rejected the SDK request type: %v", err)
	}
}

// TestTransport_RotateSessionAccount_WrongRequestType proves the delegated
// handler's type assertion is reached over Unix too: a mistyped request fails
// locally rather than being written to the socket.
func TestTransport_RotateSessionAccount_WrongRequestType(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestratorclient.CloseSessionRequest{SessionID: "s1"})
	if err == nil {
		t.Fatal("a mistyped request was accepted")
	}
	if !strings.Contains(err.Error(), "invalid request type") {
		t.Fatalf("err = %v, want the handler's invalid-request-type rejection", err)
	}
}

// TestTransport_SpawnSession_AccountAllowDelegates keeps gap 2 closed on Unix:
// the request must reach the gRPC spawn handler with the field intact rather
// than being rejected before it is mapped onto the proto message.
func TestTransport_SpawnSession_AccountAllowDelegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.Orchestrator", "SpawnSession",
		&orchestratorclient.SpawnSessionRequest{
			Backend:      "claude",
			AccountID:    "acct-a",
			AccountAllow: []string{"acct-a", "acct-b"},
		})
	if err == nil {
		t.Fatal("expected a connection error: no server is listening")
	}
	if strings.Contains(err.Error(), "invalid request type") {
		t.Fatalf("the gRPC spawn handler rejected the SDK request type: %v", err)
	}
}
