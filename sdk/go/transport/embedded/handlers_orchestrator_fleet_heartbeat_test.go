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

package embedded

import (
	"context"
	"testing"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport/internal/fleetparity"
)

func heartbeatEmbedded(t *testing.T, req *orchestratorSDK.FleetNodeHeartbeatRequest) *fleetparity.Recorder {
	t.Helper()
	recorder := fleetparity.NewRecorder()
	resp, err := handleFleetHeartbeat(context.Background(), recorder, req)
	if err != nil {
		t.Fatalf("FleetNodeHeartbeat: %v", err)
	}
	if ack, ok := resp.(*orchestratorSDK.FleetNodeHeartbeatResponse); !ok || !ack.Heartbeat {
		t.Fatalf("FleetNodeHeartbeat answered %#v, want an acknowledgement", resp)
	}
	return recorder
}

func TestFleetHeartbeat_ObservationSurvivesEmbedded(t *testing.T) {
	recorder := heartbeatEmbedded(t, &orchestratorSDK.FleetNodeHeartbeatRequest{
		TenantID:    "tenant-1",
		NodeID:      "node-1",
		Observation: fleetparity.Observation(),
	})
	fleetparity.RequireParity(t, recorder.Observed(t))
}

func TestFleetHeartbeat_BareHeartbeatEmbeddedObservesNothing(t *testing.T) {
	recorder := heartbeatEmbedded(t, &orchestratorSDK.FleetNodeHeartbeatRequest{TenantID: "tenant-1", NodeID: "node-1"})
	if observed := recorder.Observed(t); observed != nil {
		t.Fatalf("a bare heartbeat observed %#v, want nothing", observed)
	}
}
