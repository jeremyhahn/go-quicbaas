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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestTransport_GetSessionConversation_Delegates proves the Unix transport
// routes the session-conversation verb through the gRPC orchestrator handler.
//
// Unix is gRPC on a domain socket, so the server registers ONE
// OrchestratorService for both. With no server listening and a short deadline
// the call fails on connection — but crucially NOT with "unknown orchestrator
// method" or "not supported", which is the Phase 8 regression this guards.
func TestTransport_GetSessionConversation_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.Orchestrator", "GetSessionConversation",
		&orchestratorclient.GetSessionConversationRequest{SessionID: "s1", Limit: 10})
	if err == nil {
		t.Fatal("expected a connection error: no server is listening")
	}

	msg := err.Error()
	if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
		t.Fatalf("session conversation was rejected as unsupported, dispatch regression: %v", err)
	}
	if strings.Contains(msg, "invalid request type") {
		t.Fatalf("the gRPC handler rejected the SDK request type: %v", err)
	}
}

// TestTransport_GetSessionConversation_WrongRequestType proves the delegated
// handler's type assertion is reached over Unix too: a mistyped request fails
// locally rather than being written to the socket.
func TestTransport_GetSessionConversation_WrongRequestType(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.Orchestrator", "GetSessionConversation",
		&orchestratorclient.CloseSessionRequest{SessionID: "s1"})
	if err == nil {
		t.Fatal("a mistyped request was accepted")
	}
	if !strings.Contains(err.Error(), "invalid request type") {
		t.Fatalf("err = %v, want the handler's invalid-request-type rejection", err)
	}
}
