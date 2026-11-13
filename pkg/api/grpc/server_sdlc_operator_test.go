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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

func TestSDLCConverters_RulersSurviveTheWire(t *testing.T) {
	binding := profiledProtoBinding()
	binding.Rulers = []string{"lead-agent", "ops"}
	back := bindingFromProto(bindingToProto(binding))
	if !reflect.DeepEqual(back.Rulers, binding.Rulers) {
		t.Fatalf("rulers = %v, want %v: a rebind through gRPC would erase them", back.Rulers, binding.Rulers)
	}
	none := bindingFromProto(bindingToProto(&sdlc.ProjectBinding{ProjectID: "p"}))
	if len(none.Rulers) != 0 {
		t.Fatalf("absent rulers became %v", none.Rulers)
	}
}

func TestSDLCConverters_ForgeIssue(t *testing.T) {
	if forgeIssueToProto(nil) != nil {
		t.Fatal("a nil issue became a message")
	}
	got := forgeIssueToProto(&sdlc.ForgeIssue{
		Number: 88, Title: "Follow-up", Body: "b", State: "open", Labels: []string{"task:ready"}, Author: "agent", URL: "https://f/88",
	})
	if got.Number != 88 || got.Title != "Follow-up" || got.Url != "https://f/88" || got.Author != "agent" ||
		len(got.Labels) != 1 || got.Labels[0] != "task:ready" {
		t.Fatalf("issue = %+v", got)
	}
}

func TestSDLCServer_OperatorVerbsRefuseWithoutTheModuleOrAProject(t *testing.T) {
	ctx := context.Background()
	unavailable := NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t))
	if _, err := unavailable.FileIssue(ctx, &pb.FileIssueRequest{ProjectId: "p", Title: "t"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("FileIssue: %v", err)
	}
	if _, err := unavailable.ListCollaborators(ctx, &pb.ListCollaboratorsRequest{ProjectId: "p"}); status.Code(err) != codes.Unavailable {
		t.Fatalf("ListCollaborators: %v", err)
	}
	running := NewSDLCServer(fixedSDLCProvider{svc: &service.SDLCService{}}, sdlcTestLogger(t))
	if _, err := running.FileIssue(ctx, &pb.FileIssueRequest{TenantId: "t", Title: "t"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("FileIssue with no project: %v", err)
	}
	if _, err := running.ListCollaborators(ctx, &pb.ListCollaboratorsRequest{TenantId: "t"}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("ListCollaborators with no project: %v", err)
	}
}

func TestSDLCServer_ErrorMappingForTheOperatorVerbs(t *testing.T) {
	if code := status.Code(convertSDLCError(&sdlc.RulerNotCollaboratorError{Login: "x"})); code != codes.InvalidArgument {
		t.Fatalf("a refused ruler = %v, want InvalidArgument", code)
	}
	if code := status.Code(convertSDLCError(&sdlc.AuditUnavailableError{Operation: "file issue"})); code != codes.Unavailable {
		t.Fatalf("an unaudited filing = %v, want Unavailable", code)
	}
}
