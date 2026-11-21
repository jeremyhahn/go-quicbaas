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

	pkgsdlc "github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

func (p *sdlcBindProjects) GetProjectByName(_ context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if req.Name != p.project.Name {
		return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, Name: req.Name}
	}
	return &service.GetProjectResponse{Project: p.project}, nil
}

// The rig's node reads no SSH keys, so the onboarding stops at the signing
// key with a named finding — and the embedded transport still converts every
// step into the SDK's types.
func TestEmbeddedSDLCOnboardRepo_ReportsEveryStep(t *testing.T) {
	tr, _, binding := newSDLCBindRig(t)
	ctx := context.Background()
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "Bind",
		&sdlc.BindRequest{TenantID: "tenant-a", ProjectID: "p1", Binding: binding, Paused: true}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	resp, err := tr.Request(ctx, "quicbaas.SDLC", "OnboardRepo",
		&sdlc.RepoOnboardRequest{TenantID: "tenant-a", Repo: "web", Like: "p1", Paused: true})
	if err != nil {
		t.Fatalf("OnboardRepo: %v", err)
	}
	report, ok := resp.(*sdlc.RepoOnboardingReport)
	if !ok {
		t.Fatalf("response = %T, want *sdlc.RepoOnboardingReport", resp)
	}
	if report.Repo != "acme/web" || len(report.Steps) == 0 {
		t.Fatalf("report = %+v", report)
	}
	first := report.Steps[0]
	if first.Step != pkgsdlc.OnboardStepSigningKey || first.Outcome != sdlc.OnboardFinding || first.Remedy == "" {
		t.Fatalf("first step = %+v, want a signing key finding with a remedy", first)
	}
}

// TestEmbeddedSDLCOnboardRepo_CarriesPreflight: RepoOnboardRequest.Preflight
// reaches the service, which adds the preflight step to the report.
func TestEmbeddedSDLCOnboardRepo_CarriesPreflight(t *testing.T) {
	tr, _, binding := newSDLCBindRig(t)
	ctx := context.Background()
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "Bind",
		&sdlc.BindRequest{TenantID: "tenant-a", ProjectID: "p1", Binding: binding, Paused: true}); err != nil {
		t.Fatalf("Bind: %v", err)
	}

	resp, err := tr.Request(ctx, "quicbaas.SDLC", "OnboardRepo",
		&sdlc.RepoOnboardRequest{TenantID: "tenant-a", Repo: "web", Like: "p1", Preflight: true})
	if err != nil {
		t.Fatalf("OnboardRepo: %v", err)
	}
	report, ok := resp.(*sdlc.RepoOnboardingReport)
	if !ok {
		t.Fatalf("response = %T, want *sdlc.RepoOnboardingReport", resp)
	}
	last := report.Steps[len(report.Steps)-1]
	if last.Step != pkgsdlc.OnboardStepPreflight || last.Outcome != sdlc.OnboardFinding {
		t.Fatalf("last step = %+v, want the preflight step", last)
	}
}

func TestEmbeddedSDLCOnboardRepo_Refusals(t *testing.T) {
	tr, _, _ := newSDLCBindRig(t)
	ctx := context.Background()

	if _, err := tr.Request(ctx, "quicbaas.SDLC", "OnboardRepo", &sdlc.OnboardRequest{ProjectID: "p1"}); err == nil {
		t.Error("OnboardRepo accepted the wrong request type")
	}
	if _, err := tr.Request(ctx, "quicbaas.SDLC", "OnboardRepo",
		&sdlc.RepoOnboardRequest{TenantID: "tenant-a", Repo: "web", Like: "nope"}); err == nil {
		t.Error("an unknown template was onboarded")
	}
}
