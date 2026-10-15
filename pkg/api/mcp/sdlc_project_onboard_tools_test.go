//go:build mcp

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

package mcp

import (
	"context"
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func (p *mcpSDLCProjects) GetProjectByName(_ context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, project := range p.projects {
		if project.TenantID == req.TenantID && project.Name == req.Name {
			return &service.GetProjectResponse{Project: project}, nil
		}
	}
	return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, Name: req.Name}
}

// The rig's node reads no SSH keys, so the onboarding stops at the signing
// key with a named finding — and the tool still answers every step.
func TestSDLCProjectOnboardTool_ReportsEveryStep(t *testing.T) {
	registry, forge, _ := newMCPSDLCRig(t)
	bindMCPProject(t, registry, profiledToolBinding(t, forge))

	result, err := registry.executeSDLCTool(context.Background(), "sdlc_project_onboard", map[string]interface{}{
		"tenant_id": "tenant-a", "repo": "web", "like": "p1", "max_concurrency": float64(2), "paused": true,
	})
	if err != nil {
		t.Fatalf("sdlc_project_onboard: %v", err)
	}
	report, ok := result.(*sdlc.RepoOnboardingReport)
	if !ok {
		t.Fatalf("result = %T, want *sdlc.RepoOnboardingReport", result)
	}
	if report.Repo != "acme/web" {
		t.Fatalf("Repo = %q, want acme/web", report.Repo)
	}
	if step, ok := report.Step(sdlc.OnboardStepSigningKey); !ok || step.Outcome != sdlc.OnboardFinding || step.Remedy == "" {
		t.Fatalf("signing key step = %+v, %v; want a finding with a remedy", step, ok)
	}
}

func TestSDLCProjectOnboardTool_CarriesPreflight(t *testing.T) {
	registry, forge, _ := newMCPSDLCRig(t)
	bindMCPProject(t, registry, profiledToolBinding(t, forge))

	result, err := registry.executeSDLCTool(context.Background(), "sdlc_project_onboard", map[string]interface{}{
		"tenant_id": "tenant-a", "repo": "web", "like": "p1", "preflight": true,
	})
	if err != nil {
		t.Fatalf("sdlc_project_onboard: %v", err)
	}
	report, ok := result.(*sdlc.RepoOnboardingReport)
	if !ok {
		t.Fatalf("result = %T, want *sdlc.RepoOnboardingReport", result)
	}
	if step, ok := report.Step(sdlc.OnboardStepPreflight); !ok || step.Outcome != sdlc.OnboardFinding {
		t.Fatalf("preflight step = %+v, %v; want the flag to reach the service", step, ok)
	}
}

func TestSDLCProjectOnboardTool_RefusesMistypedArguments(t *testing.T) {
	registry, _, _ := newMCPSDLCRig(t)

	for name, args := range map[string]map[string]interface{}{
		"string concurrency": {"tenant_id": "tenant-a", "repo": "web", "like": "p1", "max_concurrency": "two"},
		"string paused":      {"tenant_id": "tenant-a", "repo": "web", "like": "p1", "paused": "yes"},
		"string preflight":   {"tenant_id": "tenant-a", "repo": "web", "like": "p1", "preflight": "yes"},
		"no repo":            {"tenant_id": "tenant-a", "like": "p1"},
	} {
		t.Run(name, func(t *testing.T) {
			var invalid *sdlc.InvalidInputError
			if _, err := registry.executeSDLCTool(context.Background(), "sdlc_project_onboard", args); !errors.As(err, &invalid) {
				t.Fatalf("err = %v, want an InvalidInputError", err)
			}
		})
	}
}
