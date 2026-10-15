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

package quic

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	orchestratorclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// TestRequest_ListFleetNodesCarriesBackendsOverHTTP3 proves the served
// "backends" of a fleet listing (go-quicbaas#700) survives the HTTP/3 round
// trip into the SDK type.
func TestRequest_ListFleetNodesCarriesBackendsOverHTTP3(t *testing.T) {
	gotPath := make(chan string, 1)

	addr, shutdown := startTestHTTP3Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"nodes":[` +
			`{"id":"agent-04","backend":"claude","capabilities":["claude","codex"],"backends":["claude","codex"]},` +
			`{"id":"unknown","backend":"","backends":[]}],"count":2}`))
	}))
	defer shutdown()

	tr, err := New(testTransportConfig(t, addr))
	require.NoError(t, err)
	defer tr.Close()

	resp, err := tr.Request(context.Background(), "quicbaas.Orchestrator", "ListFleetNodes",
		&orchestratorclient.ListFleetNodesRequest{TenantID: "t1"})
	require.NoError(t, err)

	out, ok := resp.(*orchestratorclient.ListFleetNodesResponse)
	require.True(t, ok, "transport returned %T", resp)
	require.Len(t, out.Nodes, 2)
	assert.Equal(t, []string{"claude", "codex"}, out.Nodes[0].Backends)
	assert.NotNil(t, out.Nodes[1].Backends, "a node serving nothing must decode to [], not nil")
	assert.Empty(t, out.Nodes[1].Backends)
	assert.Equal(t, "/api/v1/fleet", <-gotPath)
}
