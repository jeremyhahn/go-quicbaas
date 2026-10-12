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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// =============================================================================
// go-quicbaas#700 — the served "backends" of a fleet listing on the gRPC
// transport. The Unix socket delegates here verbatim, so this pins two wires.
// =============================================================================

// fleetBackendsServer answers ListFleetNodes with one node serving three
// backends and one that reports none.
type fleetBackendsServer struct {
	pb.UnimplementedOrchestratorServiceServer
}

func (fleetBackendsServer) ListFleetNodes(context.Context, *pb.ListFleetNodesRequest) (*pb.ListFleetNodesResponse, error) {
	return &pb.ListFleetNodesResponse{
		Nodes: []*pb.FleetNode{
			{Id: "agent-04", Backend: "claude", Capabilities: []string{"claude", "codex", "opencode"},
				Backends: []string{"claude", "codex", "opencode"}},
			{Id: "unknown"},
		},
		Count: 2,
	}, nil
}

func TestHandleFleetList_CarriesTheServedBackends(t *testing.T) {
	conn := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fleetBackendsServer{})
	}).dial(t)

	resp, err := handleFleetList(context.Background(), conn, &orchestrator.ListFleetNodesRequest{TenantID: "t1"})
	if err != nil {
		t.Fatalf("ListFleetNodes: %v", err)
	}
	out, ok := resp.(*orchestrator.ListFleetNodesResponse)
	if !ok {
		t.Fatalf("got %T, want *orchestrator.ListFleetNodesResponse", resp)
	}
	if len(out.Nodes) != 2 {
		t.Fatalf("got %d nodes, want 2", len(out.Nodes))
	}
	if want := []string{"claude", "codex", "opencode"}; !reflect.DeepEqual(out.Nodes[0].Backends, want) {
		t.Fatalf("Backends = %v, want %v", out.Nodes[0].Backends, want)
	}
	if out.Nodes[1].Backends == nil || len(out.Nodes[1].Backends) != 0 {
		t.Fatalf("a node serving nothing decoded Backends = %#v, want [] as REST renders it", out.Nodes[1].Backends)
	}
}

func TestFleetNodeFromProto_NilIsNil(t *testing.T) {
	if got := fleetNodeFromProto(nil); got != nil {
		t.Fatalf("fleetNodeFromProto(nil) = %+v, want nil", got)
	}
}
