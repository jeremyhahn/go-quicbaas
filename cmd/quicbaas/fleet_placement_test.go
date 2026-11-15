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

package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// fakePlacementClient records the placement verbs; the embedded nil
// OrchestratorClient panics if a command reaches past them.
type fakePlacementClient struct {
	OrchestratorClient
	declared  *orchestratorSDK.DeclareFleetPlacementSpecRequest
	named     []string
	tenant    string
	closed    bool
	withdrawn bool
}

func (f *fakePlacementClient) Close() error { f.closed = true; return nil }

func (f *fakePlacementClient) DeclareFleetPlacementSpec(_ context.Context, req *orchestratorSDK.DeclareFleetPlacementSpecRequest) (*orchestratorSDK.FleetPlacementSpec, error) {
	f.declared, f.tenant = req, req.TenantID
	out := *req.Spec
	out.Generation++
	return &out, nil
}

func (f *fakePlacementClient) GetFleetPlacementSpec(_ context.Context, req *orchestratorSDK.FleetPlacementSpecRequest) (*orchestratorSDK.FleetPlacementSpec, error) {
	f.named = append(f.named, req.Name)
	return &orchestratorSDK.FleetPlacementSpec{Name: req.Name, Role: "agent", Hosts: []string{"h1"}, Generation: 1}, nil
}

func (f *fakePlacementClient) ListFleetPlacementSpecs(_ context.Context, req *orchestratorSDK.ListFleetPlacementSpecsRequest) (*orchestratorSDK.ListFleetPlacementSpecsResponse, error) {
	f.tenant = req.TenantID
	return &orchestratorSDK.ListFleetPlacementSpecsResponse{Specs: []*orchestratorSDK.FleetPlacementSpec{{Name: "agents"}}, Count: 1}, nil
}

func (f *fakePlacementClient) WithdrawFleetPlacementSpec(_ context.Context, req *orchestratorSDK.FleetPlacementSpecRequest) (*orchestratorSDK.WithdrawFleetPlacementSpecResponse, error) {
	f.named, f.withdrawn = append(f.named, req.Name), true
	return &orchestratorSDK.WithdrawFleetPlacementSpecResponse{Name: req.Name, Withdrawn: true}, nil
}

func (f *fakePlacementClient) GetFleetPlacementDrift(_ context.Context, req *orchestratorSDK.FleetPlacementSpecRequest) (*orchestratorSDK.FleetPlacementDrift, error) {
	f.named = append(f.named, req.Name)
	return &orchestratorSDK.FleetPlacementDrift{Spec: req.Name, ObservedWork: 1}, nil
}

func withFakePlacement(t *testing.T, fake OrchestratorClient) {
	t.Helper()
	origFactory, origTimeout, origTenant, origFmt := orchestratorClientFactory, globalTimeout, globalTenantID, fleetOutputFmt
	orchestratorClientFactory = func() (OrchestratorClient, error) { return fake, nil }
	globalTimeout, globalTenantID, fleetOutputFmt = 5*time.Second, "acme", "json"
	t.Cleanup(func() {
		orchestratorClientFactory, globalTimeout, globalTenantID, fleetOutputFmt = origFactory, origTimeout, origTenant, origFmt
	})
}

// runPlacement runs `fleet placement <args>` on a fresh command tree, so no
// flag value leaks between cases.
func runPlacement(t *testing.T, args ...string) error {
	t.Helper()
	placementRole, placementHosts, placementHostCount, placementCapabilities = "", nil, 0, nil
	placementActuate, placementGeneration, placementFile = false, 0, ""
	cmd := newFleetPlacementCommand()
	cmd.SetArgs(args)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd.Execute()
}

func TestFleetPlacementCommands(t *testing.T) {
	fake := &fakePlacementClient{}
	withFakePlacement(t, fake)

	if err := runPlacement(t, "declare", "ci", "--role", "ci", "--host-count", "2", "--capability", "gpu", "--generation", "3"); err != nil {
		t.Fatalf("declare: %v", err)
	}
	spec := fake.declared.Spec
	if fake.tenant != "acme" || spec.Name != "ci" || spec.Role != "ci" || spec.HostCount != 2 ||
		len(spec.RequiredCapabilities) != 1 || spec.RequiredCapabilities[0] != "gpu" || spec.Generation != 3 || len(spec.Hosts) != 0 {
		t.Fatalf("declared %+v for tenant %q", spec, fake.tenant)
	}
	if !fake.closed {
		t.Fatal("the client was not closed")
	}

	for _, verb := range []string{"get", "drift", "withdraw"} {
		if err := runPlacement(t, verb, "agents"); err != nil {
			t.Fatalf("%s: %v", verb, err)
		}
	}
	if strings.Join(fake.named, ",") != "agents,agents,agents" || !fake.withdrawn {
		t.Fatalf("named %v, withdrawn %v", fake.named, fake.withdrawn)
	}
	fake.tenant = ""
	if err := runPlacement(t, "list"); err != nil || fake.tenant != "acme" {
		t.Fatalf("list: %v, tenant %q", err, fake.tenant)
	}
	if err := runPlacement(t, "get"); err == nil {
		t.Fatal("get with no name: want a usage error")
	}
}

// --file gives the spec and the flags the caller set override it.
func TestFleetPlacementDeclareFromFile(t *testing.T) {
	fake := &fakePlacementClient{}
	withFakePlacement(t, fake)
	path := filepath.Join(t.TempDir(), "agents.yaml")
	if err := os.WriteFile(path, []byte("role: agent\nhosts: [h1, h2]\nrequired_capabilities: [gpu]\ngeneration: 4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runPlacement(t, "declare", "agents", "--file", path, "--hosts", "h3"); err != nil {
		t.Fatalf("declare --file: %v", err)
	}
	spec := fake.declared.Spec
	if spec.Name != "agents" || spec.Role != "agent" || strings.Join(spec.Hosts, ",") != "h3" ||
		spec.RequiredCapabilities[0] != "gpu" || spec.Generation != 4 {
		t.Fatalf("declared %+v", spec)
	}

	other := filepath.Join(t.TempDir(), "other.json")
	if err := os.WriteFile(other, []byte(`{"name":"other","role":"agent"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runPlacement(t, "declare", "agents", "--file", other); err == nil {
		t.Fatal("a file naming another spec: want an error")
	}
	if err := runPlacement(t, "declare", "agents", "--file", filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("an absent file: want an error")
	}
}

// A client without the placement verbs is refused, not a panic.
func TestFleetPlacementClientWithoutVerbs(t *testing.T) {
	withFakePlacement(t, &closeOnlyClient{})
	if err := runPlacement(t, "list"); err == nil {
		t.Fatal("want an error from a client with no placement verbs")
	}
}

type closeOnlyClient struct{ OrchestratorClient }

func (closeOnlyClient) Close() error { return nil }

func TestFormatFleetPlacementTable(t *testing.T) {
	cases := []struct {
		v    interface{}
		want []string
	}{
		{&orchestratorSDK.FleetPlacementSpec{Name: "ci", Role: "ci", HostCount: 2, Generation: 3}, []string{"NAME", "ci", "2 hosts", "3"}},
		{&orchestratorSDK.ListFleetPlacementSpecsResponse{Specs: []*orchestratorSDK.FleetPlacementSpec{{Name: "agents", Hosts: []string{"h1", "h2"}}}}, []string{"agents", "h1,h2"}},
		{&orchestratorSDK.WithdrawFleetPlacementSpecResponse{Name: "agents", Withdrawn: true}, []string{"Withdrawn:", "agents"}},
		{&orchestratorSDK.FleetPlacementDrift{Spec: "agents", Role: "agent", ObservedWork: 1, AdmissibleHosts: []string{"h1"},
			Findings: []orchestratorSDK.FleetPlacementFinding{{Kind: "host_not_admitted", Host: "h2", WorkID: "s1",
				Remedy: orchestratorSDK.FleetPlacementRemedy{Action: "move", Destinations: []string{"h1"}}}}},
			[]string{"Observed work:", "host_not_admitted", "s1", "move"}},
	}
	for _, tc := range cases {
		var out bytes.Buffer
		if err := formatFleetPlacement(tc.v, "table", &out); err != nil {
			t.Fatalf("%T: %v", tc.v, err)
		}
		for _, want := range tc.want {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%T table lacks %q:\n%s", tc.v, want, out.String())
			}
		}
	}
	var out bytes.Buffer
	if err := formatFleetPlacement(&orchestratorSDK.FleetPlacementSpec{Name: "ci"}, "json", &out); err != nil ||
		!strings.Contains(out.String(), `"name": "ci"`) {
		t.Fatalf("json = %q, %v", out.String(), err)
	}
	if err := formatFleetPlacement(42, "table", &out); err == nil {
		t.Fatal("an unknown result: want an error")
	}
}
