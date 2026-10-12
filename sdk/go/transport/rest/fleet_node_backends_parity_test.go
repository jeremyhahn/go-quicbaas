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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestListFleetNodes_DecodesTheServedBackends proves the "backends" the
// server renders on a fleet listing (go-quicbaas#700) reaches the SDK type.
// QUIC embeds this transport, so this is its decoding path too.
func TestListFleetNodes_DecodesTheServedBackends(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nodes":[` +
			`{"id":"agent-04","backend":"claude","capabilities":["claude","codex","opencode"],"backends":["claude","codex","opencode"]},` +
			`{"id":"unknown","backend":"","backends":[]}],"count":2,"total":2}`))
	}))
	defer server.Close()

	resp, err := newTestTransport(t, server).Request(context.Background(), "quicbaas.Orchestrator", "ListFleetNodes",
		&orchestrator.ListFleetNodesRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("ListFleetNodes: %v", err)
	}
	if gotPath != "/api/v1/fleet" {
		t.Fatalf("ListFleetNodes hit %s, want /api/v1/fleet", gotPath)
	}
	out, ok := resp.(*orchestrator.ListFleetNodesResponse)
	if !ok {
		t.Fatalf("got %T, want *orchestrator.ListFleetNodesResponse", resp)
	}
	if len(out.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(out.Nodes))
	}
	if want := []string{"claude", "codex", "opencode"}; !reflect.DeepEqual(out.Nodes[0].Backends, want) {
		t.Fatalf("Backends = %v, want %v", out.Nodes[0].Backends, want)
	}
	if out.Nodes[1].Backends == nil || len(out.Nodes[1].Backends) != 0 {
		t.Fatalf("a node serving nothing decoded Backends = %#v, want []", out.Nodes[1].Backends)
	}
}

// TestListFleetNodes_SurfacesARefusal proves a failing registry reaches the
// caller as an error, not as an empty fleet.
func TestListFleetNodes_SurfacesARefusal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"fleet registry unavailable"}`))
	}))
	defer server.Close()

	if _, err := newTestTransport(t, server).Request(context.Background(), "quicbaas.Orchestrator", "ListFleetNodes",
		&orchestrator.ListFleetNodesRequest{TenantID: "t1"}); err == nil {
		t.Fatal("a refusing server produced a listing")
	}
}
