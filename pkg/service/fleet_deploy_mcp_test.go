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

package service

import (
	"context"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/fleet/deploy"
)

// capturingRenderer records the spec it was asked to render so tests can
// assert what the service stamped onto it before rendering.
type capturingRenderer struct {
	spec *deploy.Spec
}

func (c *capturingRenderer) RenderUnit(spec *deploy.Spec) string {
	c.spec = spec
	return "unit"
}

func (c *capturingRenderer) RenderStartScript(spec *deploy.Spec) string {
	c.spec = spec
	return "script"
}

// newMCPTestService builds a service with the test doubles and a configured
// MCP endpoint, using a capturing renderer so the rendered spec is observable.
func newMCPTestService(mcpEndpoint string, rec *capturingRenderer) *fleetDeployService {
	return &fleetDeployService{
		ssh:         &fakeDialer{},
		upload:      &fakeUploader{},
		files:       newFakeFileUploader(),
		systemd:     &fakeSystemd{},
		nohup:       &fakeNohup{},
		renderer:    rec,
		node:        &fakeNode{},
		certIssuer:  &fakeCertIssuer{},
		now:         func() time.Time { return time.Unix(0, 0) },
		sleep:       func(ctx context.Context, d time.Duration) error { return nil },
		resolveHome: func(ctx context.Context, sess sshSession) (string, error) { return "/home/deploy", nil },
		mcpEndpoint: mcpEndpoint,
	}
}

// TestNewAgentDeployService_StoresMCPEndpoint verifies the deps field is
// carried onto the constructed service.
func TestNewAgentDeployService_StoresMCPEndpoint(t *testing.T) {
	const endpoint = "https://cp.example.com:11443/mcp"
	svc := NewFleetDeployService(FleetDeployServiceDeps{
		CertIssuer:  &fakeCertIssuer{},
		MCPEndpoint: endpoint,
	}).(*fleetDeployService)
	if svc.mcpEndpoint != endpoint {
		t.Fatalf("mcpEndpoint=%q want %q", svc.mcpEndpoint, endpoint)
	}
}

// TestDeploy_StampsMCPEndpoint verifies the service stamps its configured MCP
// endpoint onto the spec before rendering the daemon command line.
func TestDeploy_StampsMCPEndpoint(t *testing.T) {
	const endpoint = "https://cp.example.com:11443/mcp"
	on := true
	spec := validSpec()
	spec.Systemd = &on // force the systemd render path

	rec := &capturingRenderer{}
	svc := newMCPTestService(endpoint, rec)

	if _, err := svc.Deploy(acmeCtx, spec); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if rec.spec == nil {
		t.Fatal("renderer was not invoked")
	}
	if rec.spec.MCPEndpoint != endpoint {
		t.Fatalf("rendered spec MCPEndpoint=%q want %q", rec.spec.MCPEndpoint, endpoint)
	}
}

// TestDeploy_MCPEndpointEmptyWhenUnconfigured verifies that an empty service
// endpoint leaves the spec untouched (no behavior change).
func TestDeploy_MCPEndpointEmptyWhenUnconfigured(t *testing.T) {
	on := true
	spec := validSpec()
	spec.Systemd = &on

	rec := &capturingRenderer{}
	svc := newMCPTestService("", rec)

	if _, err := svc.Deploy(acmeCtx, spec); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if rec.spec == nil {
		t.Fatal("renderer was not invoked")
	}
	if rec.spec.MCPEndpoint != "" {
		t.Fatalf("rendered spec MCPEndpoint=%q want empty", rec.spec.MCPEndpoint)
	}
}

// TestDeploy_SpecMCPEndpointWins verifies a caller-supplied spec endpoint is
// preserved over the service's configured endpoint.
func TestDeploy_SpecMCPEndpointWins(t *testing.T) {
	const svcEndpoint = "https://service.example.com:11443/mcp"
	const specEndpoint = "https://caller.example.com:9999/mcp"
	on := true
	spec := validSpec()
	spec.Systemd = &on
	spec.MCPEndpoint = specEndpoint

	rec := &capturingRenderer{}
	svc := newMCPTestService(svcEndpoint, rec)

	if _, err := svc.Deploy(acmeCtx, spec); err != nil {
		t.Fatalf("Deploy: %v", err)
	}
	if rec.spec.MCPEndpoint != specEndpoint {
		t.Fatalf("rendered spec MCPEndpoint=%q want %q (caller wins)", rec.spec.MCPEndpoint, specEndpoint)
	}
}
