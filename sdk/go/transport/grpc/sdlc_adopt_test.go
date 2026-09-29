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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

type adoptServer struct {
	pb.UnimplementedSDLCServiceServer
	last *pb.AdoptIssueRequest
}

func (s *adoptServer) AdoptIssue(_ context.Context, req *pb.AdoptIssueRequest) (*pb.AdoptIssueResponse, error) {
	if req.GetIssue() == 404 {
		return nil, status.Error(codes.NotFound, "no such issue")
	}
	s.last = req
	return &pb.AdoptIssueResponse{TaskId: "task-1", Issue: req.GetIssue(), PullRequest: req.GetPullRequest(),
		Branch: "issue/141-x", Superseded: "task-0"}, nil
}

func TestSDLCGRPC_AdoptIssue(t *testing.T) {
	fake := &adoptServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
	h := newSDLCHandler(conn)
	ctx := context.Background()

	resp, err := h.handleRequest(ctx, "quicbaas.SDLC", "AdoptIssue",
		&sdlc.AdoptIssueRequest{TenantID: "t", ProjectID: "p1", Issue: 141, PullRequest: 194, Ruling: "go"})
	if err != nil {
		t.Fatalf("AdoptIssue: %v", err)
	}
	if fake.last.GetRuling() != "go" || fake.last.GetPullRequest() != 194 || fake.last.GetTenantId() != "t" {
		t.Fatalf("sent %+v", fake.last)
	}
	if got := resp.(*sdlc.Adoption); got.TaskID != "task-1" || got.Superseded != "task-0" || got.Branch != "issue/141-x" {
		t.Fatalf("adoption = %+v", got)
	}
	if _, err := h.handleRequest(ctx, "quicbaas.SDLC", "AdoptIssue", &sdlc.AdoptIssueRequest{ProjectID: "p1", Issue: 404}); err == nil {
		t.Fatal("a NotFound was not surfaced")
	}
	if _, err := h.handleRequest(ctx, "quicbaas.SDLC", "AdoptIssue", &sdlc.ReviewerRequest{}); err == nil {
		t.Fatal("a wrong request type was sent")
	}
}
