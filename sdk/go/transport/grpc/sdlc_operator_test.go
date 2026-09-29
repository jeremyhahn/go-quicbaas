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
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// operatorServer answers the two operator verbs and records what it was sent.
type operatorServer struct {
	pb.UnimplementedSDLCServiceServer
	lastFile *pb.FileIssueRequest
}

func (s *operatorServer) FileIssue(_ context.Context, req *pb.FileIssueRequest) (*pb.FileIssueResponse, error) {
	if req.GetTitle() == "refused" {
		return nil, status.Error(codes.InvalidArgument, "refused")
	}
	s.lastFile = req
	return &pb.FileIssueResponse{Issue: &pb.ForgeIssue{Number: 88, Title: req.GetTitle(), Labels: req.GetLabels(), Url: "https://f/88"}}, nil
}

func (s *operatorServer) ListCollaborators(_ context.Context, req *pb.ListCollaboratorsRequest) (*pb.ListCollaboratorsResponse, error) {
	if req.GetProjectId() == "missing" {
		return nil, status.Error(codes.NotFound, "no binding")
	}
	return &pb.ListCollaboratorsResponse{Users: []*pb.ForgeUser{{Login: "acme"}, {Login: "lead", FullName: "Lead"}}}, nil
}

func TestSDLCGRPC_OperatorVerbs(t *testing.T) {
	fake := &operatorServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
	h := newSDLCHandler(conn)
	ctx := context.Background()

	resp, err := h.handleRequest(ctx, "quicbaas.SDLC", "FileIssue", &sdlc.FileIssueRequest{
		TenantID: "t", ProjectID: "p1", Title: "Follow-up", Labels: []string{"task:ready"}, Parent: 12,
	})
	if err != nil {
		t.Fatalf("FileIssue: %v", err)
	}
	if issue := resp.(*sdlc.ForgeIssue); issue.Number != 88 || issue.URL != "https://f/88" || !reflect.DeepEqual(issue.Labels, []string{"task:ready"}) {
		t.Fatalf("issue = %+v", issue)
	}
	if fake.lastFile.GetParent() != 12 || fake.lastFile.GetTenantId() != "t" {
		t.Fatalf("the server saw %+v", fake.lastFile)
	}

	resp, err = h.handleRequest(ctx, "quicbaas.SDLC", "Collaborators", &sdlc.CollaboratorsRequest{TenantID: "t", ProjectID: "p1"})
	if err != nil {
		t.Fatalf("Collaborators: %v", err)
	}
	if users := resp.(*sdlc.CollaboratorsResponse); users.Count != 2 || users.Users[1].FullName != "Lead" {
		t.Fatalf("users = %+v", users)
	}
}

func TestSDLCGRPC_OperatorVerbsSurfaceRefusals(t *testing.T) {
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, &operatorServer{}) }).dial(t)
	h := newSDLCHandler(conn)
	ctx := context.Background()
	if _, err := h.handleRequest(ctx, "quicbaas.SDLC", "FileIssue", &sdlc.FileIssueRequest{ProjectID: "p1", Title: "refused"}); err == nil {
		t.Fatal("a refused filing came back as a success")
	}
	if _, err := h.handleRequest(ctx, "quicbaas.SDLC", "Collaborators", &sdlc.CollaboratorsRequest{ProjectID: "missing"}); err == nil {
		t.Fatal("an unbound project came back as a success")
	}
	for _, method := range []string{"FileIssue", "Collaborators"} {
		if _, err := h.handleRequest(ctx, "quicbaas.SDLC", method, "wrong"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
}

func TestSDLCGRPC_RulersSurviveTheWire(t *testing.T) {
	binding := profiledSDKBinding()
	binding.Rulers = []string{"lead", "ops"}
	back := sdlcBindingFromProto(sdlcBindingToProto(&binding))
	if !reflect.DeepEqual(back.Rulers, binding.Rulers) {
		t.Fatalf("rulers = %v, want %v", back.Rulers, binding.Rulers)
	}
}
