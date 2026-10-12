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
	"testing"
	"time"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestTransport_ListFleetNodes_Delegates proves the Unix transport routes the
// fleet listing through the gRPC orchestrator handler, whose round trip of the
// served "backends" (go-quicbaas#700) is pinned in sdk/go/transport/grpc
// (TestHandleFleetList_CarriesTheServedBackends).
func TestTransport_ListFleetNodes_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListFleetNodes", &orchestratorclient.ListFleetNodesRequest{TenantID: "t1"})
	if err == nil {
		t.Fatal("expected a connection error: no server is listening")
	}
	if refusedAsUnsupported(err) {
		t.Fatalf("ListFleetNodes was refused before the socket, dispatch regression: %v", err)
	}
}
