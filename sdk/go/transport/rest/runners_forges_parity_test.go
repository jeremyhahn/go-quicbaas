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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// go-quicbaas#606 — ListRunners / ListForges on the REST transport.
//
// This is also the QUIC transport: sdk/go/transport/quic embeds *rest.Transport
// and swaps only the RoundTripper for http3, so every request-building and
// response-decoding path below is the one QUIC takes. sdk/go/transport/quic
// pins that embedding in its own package.
// =============================================================================

// TestRunnersForgesReachTheServersPaths drives both verbs through the real
// transport and pins the route each one takes. Neither carries a tenant: the
// runner registry is the daemon's and the forge adapter set is the build's.
func TestRunnersForgesReachTheServersPaths(t *testing.T) {
	var gotMethod, gotPath, gotQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotQuery = r.Method, r.URL.EscapedPath(), r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/sdlc/forges" {
			_, _ = w.Write([]byte(`{"forges":["gitea","github"],"count":2}`))
			return
		}
		_, _ = w.Write([]byte(
			`{"runners":[{"name":"sdlc","states":[{"state":"implementing","label":"Implementing","session_backed":true}]}],"count":1}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	runnersResp, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListRunners", nil)
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/orchestrator/runners" || gotQuery != "" {
		t.Fatalf("ListRunners hit %s %s?%s, want GET /api/v1/orchestrator/runners with no query",
			gotMethod, gotPath, gotQuery)
	}
	runners, ok := runnersResp.(*orchestrator.ListJobRunnersResponse)
	if !ok {
		t.Fatalf("ListRunners returned %T, want *orchestrator.ListJobRunnersResponse", runnersResp)
	}
	if len(runners.Runners) != 1 || runners.Count != 1 || runners.Runners[0].Name != "sdlc" {
		t.Fatalf("ListRunners decoded %+v", runners)
	}
	if state := runners.Runners[0].States[0]; state.State != "implementing" ||
		state.Label != "Implementing" || !state.SessionBacked {
		t.Errorf("a runner's state lost detail in decoding: %+v", state)
	}

	forgesResp, err := tr.Request(ctx, "quicbaas.SDLC", "ListForges", &sdlc.ListForgesRequest{})
	if err != nil {
		t.Fatalf("ListForges: %v", err)
	}
	if gotMethod != http.MethodGet || gotPath != "/api/v1/sdlc/forges" || gotQuery != "" {
		t.Fatalf("ListForges hit %s %s?%s, want GET /api/v1/sdlc/forges with no query", gotMethod, gotPath, gotQuery)
	}
	forges, ok := forgesResp.(*sdlc.ListForgesResponse)
	if !ok {
		t.Fatalf("ListForges returned %T, want *sdlc.ListForgesResponse", forgesResp)
	}
	if len(forges.Forges) != 2 || forges.Count != 2 || forges.Forges[0] != "gitea" || forges.Forges[1] != "github" {
		t.Fatalf("ListForges decoded %+v; the server's sort order must survive", forges)
	}
}

// TestRunnersForgesSurfaceServerErrors proves a refusal reaches the caller as
// an error rather than an empty list. An empty list would read as "this daemon
// drives no jobs" or "this build carries no forge adapters", which is a
// different and materially misleading answer.
func TestRunnersForgesSurfaceServerErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"orchestrator service not available"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "quicbaas.Orchestrator", "ListRunners", nil); err == nil {
		t.Error("ListRunners answered on a node that refused")
	}
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "ListForges", &sdlc.ListForgesRequest{}); err == nil {
		t.Error("ListForges answered on a node that refused")
	}
}
