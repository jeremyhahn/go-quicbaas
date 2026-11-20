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

// TestTransport_FleetPlacement_Delegates proves the Unix transport routes the
// fleet placement verbs (go-quicbaas#1681) through the gRPC orchestrator
// handler, whose round trip is pinned in sdk/go/transport/grpc
// (TestOrchestratorFleetPlacement_OverGRPC).
func TestTransport_FleetPlacement_Delegates(t *testing.T) {
	tr := newWiredTransport(t)
	defer func() { _ = tr.Close() }()

	named := &orchestratorclient.FleetPlacementSpecRequest{TenantID: "t1", Name: "agents"}
	for method, req := range map[string]interface{}{
		"DeclareFleetPlacementSpec": &orchestratorclient.DeclareFleetPlacementSpecRequest{
			TenantID: "t1", Spec: &orchestratorclient.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}},
		},
		"GetFleetPlacementSpec":      named,
		"ListFleetPlacementSpecs":    &orchestratorclient.ListFleetPlacementSpecsRequest{TenantID: "t1"},
		"WithdrawFleetPlacementSpec": named,
		"GetFleetPlacementDrift":     named,
		"ListFleetPlacementConsents": &orchestratorclient.ListFleetPlacementConsentsRequest{TenantID: "t1", State: "pending"},
		"AnswerFleetPlacementConsent": &orchestratorclient.AnswerFleetPlacementConsentRequest{
			TenantID: "t1", ID: "c1", State: "granted",
		},
	} {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := tr.Request(ctx, "quicbaas.Orchestrator", method, req)
		cancel()
		if err == nil {
			t.Fatalf("%s: expected a connection error: no server is listening", method)
		}
		if refusedAsUnsupported(err) {
			t.Fatalf("%s was refused before the socket, dispatch regression: %v", method, err)
		}
	}
}
