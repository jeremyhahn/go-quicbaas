//go:build rest

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
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func (p *sdlcRESTProjects) GetProjectByName(_ context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
	for _, project := range p.projects {
		if project.TenantID == req.TenantID && project.Name == req.Name {
			return &service.GetProjectResponse{Project: project}, nil
		}
	}
	return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, Name: req.Name}
}

// The rig's node reads no SSH keys, so the onboarding stops at the signing
// key with a named finding — and the report still answers 200 with every step.
func TestSDLCRepoOnboarding_ReportsEveryStep(t *testing.T) {
	server, svc := setupSDLCServer(t)
	sdlcRESTProjectRecords["p1"] = &orchestration.Project{ID: "p1", TenantID: "tenant-a", Name: "p1"}
	require.NoError(t, svc.Store().PutBinding(tenantctx.WithSystem(context.Background()), "tenant-a", ptr(restBinding("p1"))))

	rec := sdlcCall(t, server, "tenant-a", http.MethodPost, "/api/v1/sdlc/repos/web/onboarding",
		sdlc.RepoOnboardRequest{Like: "p1", Paused: true})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var report sdlc.RepoOnboardingReport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	require.Equal(t, "acme/web", report.Repo)
	step, ok := report.Step(sdlc.OnboardStepSigningKey)
	require.True(t, ok, "the signing key step did not run")
	require.Equal(t, sdlc.OnboardFinding, step.Outcome)
	require.NotEmpty(t, step.Remedy)
}

func TestSDLCRepoOnboarding_CarriesPreflight(t *testing.T) {
	server, svc := setupSDLCServer(t)
	sdlcRESTProjectRecords["p1"] = &orchestration.Project{ID: "p1", TenantID: "tenant-a", Name: "p1"}
	require.NoError(t, svc.Store().PutBinding(tenantctx.WithSystem(context.Background()), "tenant-a", ptr(restBinding("p1"))))

	rec := sdlcCall(t, server, "tenant-a", http.MethodPost, "/api/v1/sdlc/repos/web/onboarding",
		sdlc.RepoOnboardRequest{Like: "p1", Preflight: true})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var report sdlc.RepoOnboardingReport
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &report))
	step, ok := report.Step(sdlc.OnboardStepPreflight)
	require.True(t, ok, "preflight did not reach the service")
	require.Equal(t, sdlc.OnboardFinding, step.Outcome)
}

func TestSDLCRepoOnboarding_Refusals(t *testing.T) {
	server, _ := setupSDLCServer(t)

	for name, tc := range map[string]struct {
		path string
		body any
		want int
	}{
		"repo differs from the path": {"/api/v1/sdlc/repos/web/onboarding", sdlc.RepoOnboardRequest{Repo: "api", Like: "p1"}, http.StatusBadRequest},
		"malformed body":             {"/api/v1/sdlc/repos/web/onboarding", "not an object", http.StatusBadRequest},
		"no template":                {"/api/v1/sdlc/repos/web/onboarding", sdlc.RepoOnboardRequest{}, http.StatusBadRequest},
		"unknown template":           {"/api/v1/sdlc/repos/web/onboarding", sdlc.RepoOnboardRequest{Like: "nope"}, http.StatusNotFound},
	} {
		t.Run(name, func(t *testing.T) {
			rec := sdlcCall(t, server, "tenant-a", http.MethodPost, tc.path, tc.body)
			require.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}

func TestSDLCRepoOnboarding_AnsweredUnavailableWithoutTheModule(t *testing.T) {
	server, mock, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	mock.SetSDLCService(nil)

	rec := sdlcCall(t, server, "tenant-a", http.MethodPost, "/api/v1/sdlc/repos/web/onboarding", sdlc.RepoOnboardRequest{Like: "p1"})
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
