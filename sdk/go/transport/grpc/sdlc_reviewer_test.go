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
	"reflect"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// bindingStoreServer is an SDLCService that STORES what it is bound with and
// answers it back, so a test can drive a real read-modify-write across the
// wire and see what the server ended up holding.
type bindingStoreServer struct {
	pb.UnimplementedSDLCServiceServer
	mu        sync.Mutex
	held      *pb.ProjectBinding
	lastBind  *pb.BindProjectRequest
	lastSet   *pb.SetReviewerOverrideRequest
	lastClear *pb.ClearReviewerOverrideRequest
}

func (s *bindingStoreServer) BindProject(_ context.Context, req *pb.BindProjectRequest) (*pb.BindProjectResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastBind = req
	s.held = req.GetBinding()
	return &pb.BindProjectResponse{Binding: s.held}, nil
}

func (s *bindingStoreServer) GetBinding(context.Context, *pb.GetBindingRequest) (*pb.GetBindingResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return &pb.GetBindingResponse{Binding: s.held}, nil
}

func (s *bindingStoreServer) GetReviewerSelection(_ context.Context, req *pb.GetReviewerSelectionRequest) (*pb.ReviewerSelectionResponse, error) {
	return &pb.ReviewerSelectionResponse{Selection: &pb.ReviewerSelection{
		ProjectId: req.GetProjectId(), PullRequest: req.GetPullRequest(), Profile: "default", CountsTowardMerge: true,
		StatusContext: "quicbaas/review",
	}}, nil
}

func (s *bindingStoreServer) SetReviewerOverride(_ context.Context, req *pb.SetReviewerOverrideRequest) (*pb.ReviewerSelectionResponse, error) {
	if req.GetProfile() == "gone" {
		return nil, status.Error(codes.InvalidArgument, "no such profile")
	}
	s.mu.Lock()
	s.lastSet = req
	s.mu.Unlock()
	return &pb.ReviewerSelectionResponse{Selection: &pb.ReviewerSelection{
		ProjectId: req.GetProjectId(), PullRequest: req.GetPullRequest(), Profile: req.GetProfile(), Overridden: true,
		StatusContext: "quicbaas/review/" + req.GetProfile(), UpdatedAtUnix: 1_700_000_000,
	}}, nil
}

func (s *bindingStoreServer) ClearReviewerOverride(_ context.Context, req *pb.ClearReviewerOverrideRequest) (*pb.ReviewerSelectionResponse, error) {
	s.mu.Lock()
	s.lastClear = req
	s.mu.Unlock()
	return &pb.ReviewerSelectionResponse{Selection: &pb.ReviewerSelection{
		ProjectId: req.GetProjectId(), PullRequest: req.GetPullRequest(), Profile: "default", CountsTowardMerge: true,
	}}, nil
}

func profiledSDKBinding() sdlc.ProjectBinding {
	advisory, counted := false, true
	return sdlc.ProjectBinding{
		ProjectID: "p1",
		Reviewer: sdlc.ReviewerConfig{
			Agent: "codex-reviewer", Login: "codex", TokenSecretRef: "codex-tok"},
		Reviewers: []sdlc.ReviewerConfig{
			{Name: "qwen", Agent: "qwen-reviewer", Login: "qwen-reviewer", TokenSecretRef: "qwen-tok", CountsTowardMerge: &advisory},
			{Name: "gpt", Agent: "codex-reviewer", Login: "gpt-reviewer", TokenSecretRef: "gpt-tok", CountsTowardMerge: &counted},
			{Name: "unset", Agent: "codex-reviewer", Login: "unset-reviewer", TokenSecretRef: "unset-tok"},
		},
	}
}

// TestSDLCGRPC_ARebindKeepsTheReviewerProfiles is the gRPC regression for the
// bug class reviewer_not_on_the_wire records: bind, read back, bind what was
// read — and the server must still hold every alternate, every field of each,
// and the PRESENCE of counts_toward_merge.
func TestSDLCGRPC_ARebindKeepsTheReviewerProfiles(t *testing.T) {
	fake := &bindingStoreServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
	ctx := context.Background()
	want := profiledSDKBinding()

	if _, err := handleSDLCBind(ctx, conn, &sdlc.BindRequest{TenantID: "t", ProjectID: "p1", Binding: want}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	read, err := handleSDLCBinding(ctx, conn, &sdlc.GetBindingRequest{TenantID: "t", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Binding: %v", err)
	}
	rebound, err := handleSDLCBind(ctx, conn, &sdlc.BindRequest{TenantID: "t", ProjectID: "p1", Binding: *read.(*sdlc.ProjectBinding)})
	if err != nil {
		t.Fatalf("rebind: %v", err)
	}
	got := rebound.(*sdlc.ProjectBinding)
	if !reflect.DeepEqual(got.Reviewer, want.Reviewer) {
		t.Fatalf("default reviewer after a rebind: %+v, want %+v", got.Reviewer, want.Reviewer)
	}
	if !reflect.DeepEqual(got.Reviewers, want.Reviewers) {
		t.Fatalf("alternates after a rebind: %+v, want %+v", got.Reviewers, want.Reviewers)
	}
	if got.Reviewers[2].CountsTowardMerge != nil {
		t.Fatal("an unset counts_toward_merge came back set; a default would flip to advisory")
	}
	fake.mu.Lock()
	held := len(fake.held.GetReviewers())
	fake.mu.Unlock()
	if held != 3 {
		t.Fatalf("the server holds %d alternates after the rebind, want 3", held)
	}
}

func TestSDLCGRPC_ReviewerVerbs(t *testing.T) {
	fake := &bindingStoreServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
	h := newSDLCHandler(conn)
	ctx := context.Background()

	resp, err := h.handleRequest(ctx, "quicbaas.SDLC", "Reviewer", &sdlc.ReviewerRequest{TenantID: "t", ProjectID: "p1", PullRequest: 41})
	if err != nil || resp.(*sdlc.ReviewerSelection).Profile != "default" || !resp.(*sdlc.ReviewerSelection).CountsTowardMerge {
		t.Fatalf("Reviewer: %+v %v", resp, err)
	}
	resp, err = h.handleRequest(ctx, "quicbaas.SDLC", "SetReviewer", &sdlc.SetReviewerRequest{TenantID: "t", ProjectID: "p1", PullRequest: 41, Profile: "qwen"})
	if err != nil {
		t.Fatalf("SetReviewer: %v", err)
	}
	set := resp.(*sdlc.ReviewerSelection)
	if !set.Overridden || set.Profile != "qwen" || set.StatusContext != "quicbaas/review/qwen" || set.UpdatedAt.Unix() != 1_700_000_000 {
		t.Fatalf("SetReviewer selection: %+v", set)
	}
	if fake.lastSet.GetTenantId() != "t" || fake.lastSet.GetPullRequest() != 41 {
		t.Fatalf("the server saw %+v", fake.lastSet)
	}
	if _, err := h.handleRequest(ctx, "quicbaas.SDLC", "ClearReviewer", &sdlc.ReviewerRequest{ProjectID: "p1", PullRequest: 41}); err != nil {
		t.Fatalf("ClearReviewer: %v", err)
	}
	if fake.lastClear.GetProjectId() != "p1" {
		t.Fatalf("the server saw %+v", fake.lastClear)
	}

	// A refusal comes back as an error, not as an empty selection.
	if _, err := h.handleRequest(ctx, "quicbaas.SDLC", "SetReviewer", &sdlc.SetReviewerRequest{ProjectID: "p1", PullRequest: 41, Profile: "gone"}); err == nil {
		t.Fatal("a refused override came back as a success")
	}
	for _, method := range []string{"Reviewer", "SetReviewer", "ClearReviewer"} {
		if _, err := h.handleRequest(ctx, "quicbaas.SDLC", method, "wrong"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
	if sdlcReviewerSelectionFromProto(nil) != nil {
		t.Fatal("a nil selection became a value")
	}
}
