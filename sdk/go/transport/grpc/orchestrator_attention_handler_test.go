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
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
)

// attentionServer answers the two inbox RPCs from a script.
type attentionServer struct {
	pb.UnimplementedSDLCServiceServer
	lastList    *pb.ListAttentionRequest
	lastResolve *pb.ResolveAttentionRequest
}

func (s *attentionServer) ListAttention(_ context.Context, req *pb.ListAttentionRequest) (*pb.ListAttentionResponse, error) {
	s.lastList = req
	return &pb.ListAttentionResponse{
		Items: []*pb.AttentionItem{{Id: "task-1", Kind: "parked_task", Attempts: 2, Actions: []string{"readmit", "note"}}},
		Total: 3, Limit: 1, Offset: 0, NextOffset: 1,
		Facets: map[string]*pb.FacetCounts{"kind": {Counts: map[string]int32{"parked_task": 3}}},
	}, nil
}

func (s *attentionServer) ResolveAttention(_ context.Context, req *pb.ResolveAttentionRequest) (*pb.ResolveAttentionResponse, error) {
	if req.GetTaskId() == "gone" {
		return nil, status.Error(codes.NotFound, "no such task")
	}
	s.lastResolve = req
	return &pb.ResolveAttentionResponse{Item: &pb.AttentionItem{Id: req.GetTaskId(), Kind: "parked_task"}}, nil
}

func TestOrchestratorGRPC_AttentionVerbsReachTheSDLCService(t *testing.T) {
	fake := &attentionServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
	h := newOrchestratorHandler(conn)
	ctx := context.Background()

	resp, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "AttentionList", &orchestrator.AttentionListRequest{
		TenantID: "t1", ProjectID: "p1", Kind: "parked_task", Params: page.Params{Limit: 1, Sort: "created_at:desc"},
	})
	if err != nil {
		t.Fatalf("AttentionList: %v", err)
	}
	if fake.lastList.GetProjectId() != "p1" || fake.lastList.GetLimit() != 1 || fake.lastList.GetSort() != "created_at:desc" {
		t.Fatalf("sent %+v", fake.lastList)
	}
	list := resp.(*orchestrator.AttentionListResponse)
	if len(list.Items) != 1 || list.Items[0].Attempts != 2 || list.Total != 3 || list.NextOffset != 1 ||
		list.Facets["kind"]["parked_task"] != 3 || len(list.Items[0].Actions) != 2 {
		t.Fatalf("list = %+v", list)
	}

	item, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "AttentionResolve", &orchestrator.AttentionResolveRequest{
		TenantID: "t1", TaskID: "task-1", Action: "note", Answer: "Decided.",
	})
	if err != nil {
		t.Fatalf("AttentionResolve: %v", err)
	}
	if fake.lastResolve.GetAction() != "note" || fake.lastResolve.GetAnswer() != "Decided." || item.(*orchestrator.AttentionItem).ID != "task-1" {
		t.Fatalf("sent %+v, got %+v", fake.lastResolve, item)
	}
}

func TestOrchestratorGRPC_AttentionVerbsRefuseWrongTypesAndSurfaceErrors(t *testing.T) {
	fake := &attentionServer{}
	conn := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterSDLCServiceServer(s, fake) }).dial(t)
	h := newOrchestratorHandler(conn)
	ctx := context.Background()
	for _, method := range []string{"AttentionList", "AttentionResolve"} {
		if _, err := h.handleRequest(ctx, "quicbaas.Orchestrator", method, "wrong"); err == nil {
			t.Fatalf("%s accepted the wrong request type", method)
		}
	}
	if _, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "AttentionResolve",
		&orchestrator.AttentionResolveRequest{TaskID: "gone", Action: "readmit"}); err == nil {
		t.Fatal("a NotFound was not surfaced")
	}
	if attentionItemFromProto(nil) != nil || facetsFromProto(nil) != nil {
		t.Fatal("nil must stay nil")
	}
}
