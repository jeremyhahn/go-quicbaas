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

//go:build integration

package placement

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestFleetPlacementSpecsMultiProtocol drives the fleet placement specs
// (SPEC row declarative-placement, go-quicbaas#1681) against the cluster's
// real server on every transport: a spec is declared, read back, listed,
// refused on a stale generation, replaced on the generation read, reported
// on for drift and withdrawn, each transport in its own spec name.
func TestFleetPlacementSpecsMultiProtocol(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	stamp := strconv.FormatInt(time.Now().UnixNano(), 36)
	// Declaring and withdrawing a spec needs placement:manage, which only
	// the tenant's Admin role holds, so the test acts as the tenant principal.
	bearer := helpers.AuthTenantPrincipal(t, restEndpoint(t))
	for name, build := range transports(t, bearer) {
		t.Run(name, func(t *testing.T) {
			client, err := build()
			if err != nil {
				t.Fatalf("%s client: %v", name, err)
			}
			defer func() {
				if err := client.Close(); err != nil {
					t.Errorf("close %s client: %v", name, err)
				}
			}()
			fleet := client.Orchestrator()
			specName := "it-" + stamp + "-" + name
			named := &orchestrator.FleetPlacementSpecRequest{Name: specName}

			created, err := fleet.DeclareFleetPlacementSpec(ctx, &orchestrator.DeclareFleetPlacementSpecRequest{
				Spec: &orchestrator.FleetPlacementSpec{Name: specName, Role: "agent", Hosts: []string{"it-host-a"}},
			})
			if err != nil {
				t.Fatalf("DeclareFleetPlacementSpec: %v", err)
			}
			if created.Generation != 1 || created.Role != "agent" || len(created.Hosts) != 1 {
				t.Fatalf("declared %+v", created)
			}

			got, err := fleet.GetFleetPlacementSpec(ctx, named)
			if err != nil || got.Name != specName || got.Generation != 1 || got.Hosts[0] != "it-host-a" {
				t.Fatalf("GetFleetPlacementSpec: %+v, %v", got, err)
			}

			list, err := fleet.ListFleetPlacementSpecs(ctx, &orchestrator.ListFleetPlacementSpecsRequest{})
			if err != nil {
				t.Fatalf("ListFleetPlacementSpecs: %v", err)
			}
			listed := false
			for _, s := range list.Specs {
				listed = listed || s.Name == specName
			}
			if !listed || list.Count != len(list.Specs) {
				t.Fatalf("list lacks %s or miscounts: %+v", specName, list)
			}

			// Generation 0 creates, so it is refused for an existing spec.
			if _, err := fleet.DeclareFleetPlacementSpec(ctx, &orchestrator.DeclareFleetPlacementSpecRequest{
				Spec: &orchestrator.FleetPlacementSpec{Name: specName, Role: "agent", HostCount: 2},
			}); err == nil {
				t.Fatal("a create of an existing spec: want a conflict")
			}
			replaced, err := fleet.DeclareFleetPlacementSpec(ctx, &orchestrator.DeclareFleetPlacementSpecRequest{
				Spec: &orchestrator.FleetPlacementSpec{Name: specName, Role: "agent", HostCount: 2, Generation: 1},
			})
			if err != nil || replaced.Generation != 2 || replaced.HostCount != 2 {
				t.Fatalf("replace on generation 1: %+v, %v", replaced, err)
			}

			drift, err := fleet.GetFleetPlacementDrift(ctx, named)
			if err != nil || drift.Spec != specName || drift.Role != "agent" || drift.Generation != 2 {
				t.Fatalf("GetFleetPlacementDrift: %+v, %v", drift, err)
			}

			withdrawn, err := fleet.WithdrawFleetPlacementSpec(ctx, named)
			if err != nil || !withdrawn.Withdrawn {
				t.Fatalf("WithdrawFleetPlacementSpec: %+v, %v", withdrawn, err)
			}
			if _, err := fleet.GetFleetPlacementSpec(ctx, named); err == nil {
				t.Fatal("a withdrawn spec is still readable")
			}
		})
	}
}
