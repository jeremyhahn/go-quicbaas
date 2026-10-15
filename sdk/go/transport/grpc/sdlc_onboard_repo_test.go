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
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// repoOnboardServer answers OnboardRepo with a fixed report and keeps the
// request it was sent.
type repoOnboardServer struct {
	pb.UnimplementedSDLCServiceServer
	mu   sync.Mutex
	last *pb.OnboardRepoRequest
}

func (s *repoOnboardServer) OnboardRepo(_ context.Context, req *pb.OnboardRepoRequest) (*pb.OnboardRepoResponse, error) {
	s.mu.Lock()
	s.last = req
	s.mu.Unlock()
	if req.GetLike() == "nope" {
		return nil, status.Error(codes.NotFound, "project nope not found")
	}
	return &pb.OnboardRepoResponse{Report: &pb.RepoOnboardingReport{
		TenantId: req.GetTenantId(), ProjectId: "p2", Repo: "acme/" + req.GetRepo(),
		Steps: []*pb.RepoOnboardStep{
			{Step: "project", Outcome: sdlc.OnboardCreated, Detail: "created p2"},
			{Step: "protection", Outcome: sdlc.OnboardFinding, Detail: "missing", Remedy: "require it"},
		},
		Conformance: &pb.ConformanceReport{ProjectId: "p2"},
	}}, nil
}

func TestSDLCGRPC_OnboardRepoRoundTrips(t *testing.T) {
	fake := &repoOnboardServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)

	resp, err := handleSDLCOnboardRepo(context.Background(), conn,
		&sdlc.RepoOnboardRequest{TenantID: "t", Repo: "web", Like: "p1", MaxConcurrency: 3, Paused: true, Preflight: true})
	if err != nil {
		t.Fatalf("OnboardRepo: %v", err)
	}

	fake.mu.Lock()
	sent := fake.last
	fake.mu.Unlock()
	if sent.GetRepo() != "web" || sent.GetLike() != "p1" || sent.GetMaxConcurrency() != 3 || !sent.GetPaused() || !sent.GetPreflight() {
		t.Fatalf("server saw %+v", sent)
	}
	report, ok := resp.(*sdlc.RepoOnboardingReport)
	if !ok {
		t.Fatalf("response = %T", resp)
	}
	if report.ProjectID != "p2" || report.Repo != "acme/web" || len(report.Steps) != 2 {
		t.Fatalf("report = %+v", report)
	}
	if report.Steps[1].Outcome != sdlc.OnboardFinding || report.Steps[1].Remedy != "require it" {
		t.Fatalf("finding step = %+v", report.Steps[1])
	}
	if report.Conformance == nil || report.Conformance.ProjectID != "p2" {
		t.Fatalf("conformance = %+v", report.Conformance)
	}
}

func TestSDLCGRPC_OnboardRepoRefusals(t *testing.T) {
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, &repoOnboardServer{}) }).dial(t)
	ctx := context.Background()

	if _, err := handleSDLCOnboardRepo(ctx, conn, &sdlc.OnboardRequest{}); !errors.IsInvalidInput(err) {
		t.Errorf("wrong request type: %v", err)
	}
	if _, err := handleSDLCOnboardRepo(ctx, conn, &sdlc.RepoOnboardRequest{Repo: "web", Like: "nope"}); !errors.IsNotFound(err) {
		t.Errorf("unknown template: %v", err)
	}
	if report := sdlcRepoOnboardingReportFromProto(nil); report == nil || len(report.Steps) != 0 {
		t.Errorf("a nil wire report converted to %+v", report)
	}
}
