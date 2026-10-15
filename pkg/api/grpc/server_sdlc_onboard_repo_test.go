//go:build codec_json

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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func (p *bindProjects) GetProjectByName(_ context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
	if req.Name != p.project.Name {
		return nil, &service.ProjectNotFoundError{TenantID: req.TenantID, Name: req.Name}
	}
	return &service.GetProjectResponse{Project: p.project}, nil
}

// The rig's node reads no SSH keys, so onboarding stops at the signing key
// with a named finding — and every step still crosses the wire.
func TestSDLCServer_OnboardRepoReturnsEveryStep(t *testing.T) {
	server, _, binding := newBindRig(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	_, err := server.BindProject(ctx, &pb.BindProjectRequest{TenantId: "tenant-a", Binding: binding})
	require.NoError(t, err)

	resp, err := server.OnboardRepo(ctx, &pb.OnboardRepoRequest{TenantId: "tenant-a", Repo: "web", Like: "p1", Paused: true})
	require.NoError(t, err)

	report := resp.GetReport()
	require.NotNil(t, report)
	assert.Equal(t, "acme/web", report.Repo)
	require.Len(t, report.Steps, 8)
	assert.Equal(t, sdlc.OnboardStepSigningKey, report.Steps[0].Step)
	assert.Equal(t, string(sdlc.OnboardFinding), report.Steps[0].Outcome)
	assert.NotEmpty(t, report.Steps[0].Remedy, "a finding crossed the wire without its remedy")
	assert.Nil(t, report.Conformance, "an unbound repository reported conformance")
}

// preflight reaches the service: the report gains the preflight step, which
// is absent when the flag is not set.
func TestSDLCServer_OnboardRepoCarriesPreflight(t *testing.T) {
	server, _, binding := newBindRig(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")
	_, err := server.BindProject(ctx, &pb.BindProjectRequest{TenantId: "tenant-a", Binding: binding})
	require.NoError(t, err)

	resp, err := server.OnboardRepo(ctx, &pb.OnboardRepoRequest{TenantId: "tenant-a", Repo: "web", Like: "p1", Preflight: true})
	require.NoError(t, err)

	steps := resp.GetReport().GetSteps()
	require.Len(t, steps, 9)
	assert.Equal(t, sdlc.OnboardStepPreflight, steps[8].Step)
	assert.Equal(t, string(sdlc.OnboardFinding), steps[8].Outcome)
}

func TestSDLCServer_OnboardRepoMapsItsErrors(t *testing.T) {
	server, _, _ := newBindRig(t)
	ctx := tenantctx.WithTenant(context.Background(), "tenant-a")

	_, err := server.OnboardRepo(ctx, &pb.OnboardRepoRequest{TenantId: "tenant-a", Repo: "web", Like: "nope"})
	assert.Equal(t, codes.NotFound, status.Code(err), "unknown template: %v", err)

	_, err = server.OnboardRepo(ctx, &pb.OnboardRepoRequest{TenantId: "tenant-a", Like: "p1"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "no repo: %v", err)

	_, err = server.OnboardRepo(ctx, nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "nil request: %v", err)
}

func TestSDLCServer_OnboardRepoAnswersUnavailableWithoutTheModule(t *testing.T) {
	srv := NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t))
	_, err := srv.OnboardRepo(tenantctx.WithSystem(context.Background()), &pb.OnboardRepoRequest{Repo: "web", Like: "p1"})
	assert.Equal(t, codes.Unavailable, status.Code(err))
}

func TestRepoOnboardingReportToProto(t *testing.T) {
	report := &sdlc.RepoOnboardingReport{
		TenantID: "t", ProjectID: "p2", Repo: "acme/web",
		Steps: []sdlc.RepoOnboardStep{
			{Step: sdlc.OnboardStepProject, Outcome: sdlc.OnboardCreated, Detail: "created"},
			{Step: sdlc.OnboardStepProtection, Outcome: sdlc.OnboardFinding, Detail: "missing", Remedy: "require it"},
		},
		Conformance: &sdlc.ConformanceReport{ProjectID: "p2"},
	}

	wire := repoOnboardingReportToProto(report)

	require.NotNil(t, wire)
	assert.Equal(t, "p2", wire.ProjectId)
	require.Len(t, wire.Steps, 2)
	assert.Equal(t, "finding", wire.Steps[1].Outcome)
	assert.Equal(t, "require it", wire.Steps[1].Remedy)
	assert.Equal(t, "p2", wire.Conformance.GetProjectId())
	assert.Nil(t, repoOnboardingReportToProto(nil))
}
