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

	sdlcclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// go-quicbaas#606 — ListRunners / ListForges over the Unix socket.
//
// Unix is gRPC on a domain socket: the daemon registers ONE OrchestratorService
// and ONE SDLCService for both wires, and this transport hands every request to
// the gRPC transport verbatim. So the only thing that can be missing here is
// the dispatch entry, which is exactly what went missing in go-quicbaas#606.
//
// With no server listening and a short deadline the calls fail on connection —
// but crucially NOT with "unknown method" or "not supported", which is the
// regression these guard.
// =============================================================================

// refusedAsUnsupported reports whether the error is the SDK refusing the verb
// locally rather than the socket refusing the connection.
func refusedAsUnsupported(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "not supported") ||
		strings.Contains(msg, "not yet supported") ||
		strings.Contains(msg, "unknown method") ||
		strings.Contains(msg, "unknown sdlc method") ||
		strings.Contains(msg, "unsupported orchestrator method")
}

// TestTransport_ListRunners_Delegates proves the Unix transport routes
// ListRunners through the gRPC orchestrator handler.
func TestTransport_ListRunners_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListRunners", nil)
	if err == nil {
		t.Fatal("expected a connection error: no server is listening")
	}
	if refusedAsUnsupported(err) {
		t.Fatalf("ListRunners was refused before the socket, dispatch regression: %v", err)
	}
}

// TestTransport_ListForges_Delegates proves the Unix transport routes the SDLC
// forges verb through the gRPC SDLC handler.
func TestTransport_ListForges_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.SDLC", "ListForges", &sdlcclient.ListForgesRequest{})
	if err == nil {
		t.Fatal("expected a connection error: no server is listening")
	}
	if refusedAsUnsupported(err) {
		t.Fatalf("ListForges was refused before the socket, dispatch regression: %v", err)
	}
}

// TestTransport_ListForgesOnAClosedTransport refuses locally once the transport is
// closed, rather than dialling a socket it no longer owns.
func TestTransport_ListForgesOnAClosedTransport(t *testing.T) {
	tr := newWiredTransport(t)
	if err := tr.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	_, err := tr.Request(context.Background(), "quicbaas.SDLC", "ListForges", &sdlcclient.ListForgesRequest{})
	if err == nil {
		t.Fatal("a closed transport answered ListForges")
	}
	if !strings.Contains(err.Error(), "transport is closed") {
		t.Fatalf("got %v, want a closed-transport refusal", err)
	}
}
