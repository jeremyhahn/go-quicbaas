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

package orchestrator

import (
	"context"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

func TestFleetPlacement_VerbsReachTheTransport(t *testing.T) {
	ctx := context.Background()
	named := &FleetPlacementSpecRequest{Name: "agents"}

	if _, err := NewClient(okTransport(t, "DeclareFleetPlacementSpec", &FleetPlacementSpec{Name: "agents"})).
		DeclareFleetPlacementSpec(ctx, &DeclareFleetPlacementSpecRequest{Spec: &FleetPlacementSpec{Name: "agents"}}); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := NewClient(okTransport(t, "GetFleetPlacementSpec", &FleetPlacementSpec{})).GetFleetPlacementSpec(ctx, named); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if _, err := NewClient(okTransport(t, "ListFleetPlacementSpecs", &ListFleetPlacementSpecsResponse{})).ListFleetPlacementSpecs(ctx, nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := NewClient(okTransport(t, "WithdrawFleetPlacementSpec", &WithdrawFleetPlacementSpecResponse{})).WithdrawFleetPlacementSpec(ctx, named); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if _, err := NewClient(okTransport(t, "GetFleetPlacementDrift", &FleetPlacementDrift{})).GetFleetPlacementDrift(ctx, named); err != nil {
		t.Fatalf("Drift: %v", err)
	}
}

func TestFleetPlacement_RefusesBeforeTheTransport(t *testing.T) {
	ctx := context.Background()
	c := NewClient(failTransport())

	cases := map[string]error{}
	_, cases["declare nil"] = c.DeclareFleetPlacementSpec(ctx, nil)
	_, cases["declare no spec"] = c.DeclareFleetPlacementSpec(ctx, &DeclareFleetPlacementSpecRequest{})
	_, cases["declare no name"] = c.DeclareFleetPlacementSpec(ctx, &DeclareFleetPlacementSpecRequest{Spec: &FleetPlacementSpec{Role: "agent"}})
	_, cases["get no name"] = c.GetFleetPlacementSpec(ctx, &FleetPlacementSpecRequest{})
	_, cases["withdraw nil"] = c.WithdrawFleetPlacementSpec(ctx, nil)
	_, cases["drift no name"] = c.GetFleetPlacementDrift(ctx, &FleetPlacementSpecRequest{})
	//nolint:staticcheck // a nil context is the case under test
	_, cases["nil context"] = c.ListFleetPlacementSpecs(nil, nil)
	for name, err := range cases {
		if !errors.IsInvalidInput(err) {
			t.Errorf("%s = %v, want invalid input", name, err)
		}
	}

	// A transport answering the wrong type is an internal error, not a panic.
	_, err := NewClient(okTransport(t, "GetFleetPlacementDrift", &FleetPlacementSpec{})).
		GetFleetPlacementDrift(ctx, &FleetPlacementSpecRequest{Name: "agents"})
	if !errors.IsServerError(err) {
		t.Fatalf("wrong response type = %v, want an internal error", err)
	}
}
