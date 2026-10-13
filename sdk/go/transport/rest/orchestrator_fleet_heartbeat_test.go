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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/fleetparity"
)

// heartbeatBodyOverREST sends req through the SDK REST transport and returns
// the body the server received. The server decodes that body as a
// FleetHeartbeatReport (pkg/api/rest heartbeatBody embeds it), which is the
// parity reference, so the body decoding to the reference is the whole check.
func heartbeatBodyOverREST(t *testing.T, req *orchestrator.FleetNodeHeartbeatRequest) []byte {
	t.Helper()
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/fleet/node-1/heartbeat" || r.URL.RawQuery != "tenant_id=tenant-1" {
			t.Errorf("heartbeat sent to %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"node_id":"node-1","heartbeat":true}`)); err != nil {
			t.Errorf("write: %v", err)
		}
	}))
	defer server.Close()

	resp, err := newTestTransport(t, server).Request(context.Background(), "Orchestrator", "FleetNodeHeartbeat", req)
	if err != nil {
		t.Fatalf("FleetNodeHeartbeat: %v", err)
	}
	if ack, ok := resp.(*orchestrator.FleetNodeHeartbeatResponse); !ok || !ack.Heartbeat {
		t.Fatalf("FleetNodeHeartbeat answered %#v, want an acknowledgement", resp)
	}
	return <-bodies
}

func TestFleetHeartbeat_ObservationSurvivesREST(t *testing.T) {
	body := heartbeatBodyOverREST(t, &orchestrator.FleetNodeHeartbeatRequest{
		TenantID:    "tenant-1",
		NodeID:      "node-1",
		Observation: fleetparity.Observation(),
	})
	observed, err := orchestration.ParseFleetHeartbeatReport(body)
	if err != nil {
		t.Fatalf("the server could not decode the heartbeat body %s: %v", body, err)
	}
	fleetparity.RequireParity(t, observed)
}

func TestFleetHeartbeat_BareHeartbeatOverRESTSendsNoBody(t *testing.T) {
	body := heartbeatBodyOverREST(t, &orchestrator.FleetNodeHeartbeatRequest{TenantID: "tenant-1", NodeID: "node-1"})
	if len(body) != 0 {
		t.Fatalf("a bare heartbeat sent the body %s, want none", body)
	}
}
