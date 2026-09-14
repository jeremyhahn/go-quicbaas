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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/sdlc"
)

// =============================================================================
// go-quicbaas#606 — ListRunners / ListForges on the gRPC transport.
//
// This is the transport the Unix socket delegates to verbatim
// (sdk/go/transport/unix.Transport.Request), so a verb missing here is missing
// on two wires, not one. sdk/go/transport/unix pins that delegation in its own
// package.
// =============================================================================

// TestRunnersForgesAreDispatchedOverGRPC pins both verbs into the two dispatch
// maps. A verb absent from a map is answered "unknown method" by the SDK before
// the wire is ever touched, which is precisely how go-quicbaas#606 presented.
func TestRunnersForgesAreDispatchedOverGRPC(t *testing.T) {
	if _, ok := orchestratorMethodMap["ListRunners"]; !ok {
		t.Error("ListRunners is not in orchestratorMethodMap: the SDK refuses the verb before dialling")
	}
	if _, ok := sdlcMethodMap["ListForges"]; !ok {
		t.Error("ListForges is not in sdlcMethodMap: the SDK refuses the verb before dialling")
	}
}

// runnersForgesServer serves the two RPCs over the in-memory listener.
type runnersForgesServer struct {
	pb.UnimplementedOrchestratorServiceServer
	pb.UnimplementedSDLCServiceServer
	fail bool
}

func (f *runnersForgesServer) ListRunners(context.Context, *pb.OrchestratorListRunnersRequest) (*pb.OrchestratorListRunnersResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.OrchestratorListRunnersResponse{
		Runners: []*pb.OrchestratorJobRunner{
			{Name: "sdlc", States: []*pb.OrchestratorJobStateSpec{
				{State: "implementing", Label: "Implementing", SessionBacked: true},
				{State: "verifying", Label: "Verifying"},
			}},
			{Name: "workflow"},
		},
		Count: 2,
	}, nil
}

func (f *runnersForgesServer) ListForges(context.Context, *pb.ListForgesRequest) (*pb.ListForgesResponse, error) {
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.ListForgesResponse{Forges: []string{"gitea", "github"}, Count: 2}, nil
}

func runnersForgesConn(t *testing.T, fail bool) *grpc.ClientConn {
	t.Helper()
	fake := &runnersForgesServer{fail: fail}
	return newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
		pb.RegisterSDLCServiceServer(s, fake)
	}).dial(t)
}

// TestHandleOrchestratorListRunners_RoundTrip proves the handler reaches the
// real RPC and renders the SDK envelope the REST transport renders: runners in
// registry order, each carrying its ordered states.
func TestHandleOrchestratorListRunners_RoundTrip(t *testing.T) {
	resp, err := handleOrchestratorListRunners(context.Background(), runnersForgesConn(t, false), nil)
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}
	runners, ok := resp.(*orchestrator.ListJobRunnersResponse)
	if !ok {
		t.Fatalf("got %T, want *orchestrator.ListJobRunnersResponse", resp)
	}
	if len(runners.Runners) != 2 || runners.Count != 2 {
		t.Fatalf("got %d runners (count %d), want 2", len(runners.Runners), runners.Count)
	}
	if runners.Runners[0].Name != "sdlc" || runners.Runners[1].Name != "workflow" {
		t.Fatalf("registry order lost: %q, %q", runners.Runners[0].Name, runners.Runners[1].Name)
	}
	states := runners.Runners[0].States
	if len(states) != 2 {
		t.Fatalf("got %d states, want 2", len(states))
	}
	if states[0].State != "implementing" || states[0].Label != "Implementing" || !states[0].SessionBacked {
		t.Errorf("first state lost detail: %+v", states[0])
	}
	if states[1].SessionBacked {
		t.Errorf("second state is not session backed but arrived as though it were: %+v", states[1])
	}
	if len(runners.Runners[1].States) != 0 {
		t.Errorf("a runner declaring no states arrived with %d", len(runners.Runners[1].States))
	}
}

// TestHandleOrchestratorListRunners_ServerError surfaces a server failure as a
// typed SDK error rather than an empty list, which would read as "this daemon
// drives no jobs".
func TestHandleOrchestratorListRunners_ServerError(t *testing.T) {
	if _, err := handleOrchestratorListRunners(context.Background(), runnersForgesConn(t, true), nil); err == nil {
		t.Fatal("a failing server produced no error")
	}
}

// TestHandleSDLCListForges_RoundTrip proves the handler reaches the real RPC and
// renders the same {forges, count} envelope the REST route serves.
func TestHandleSDLCListForges_RoundTrip(t *testing.T) {
	resp, err := handleSDLCForges(context.Background(), runnersForgesConn(t, false), &sdlc.ListForgesRequest{})
	if err != nil {
		t.Fatalf("ListForges: %v", err)
	}
	forges, ok := resp.(*sdlc.ListForgesResponse)
	if !ok {
		t.Fatalf("got %T, want *sdlc.ListForgesResponse", resp)
	}
	if len(forges.Forges) != 2 || forges.Count != 2 {
		t.Fatalf("got %v (count %d), want two forges", forges.Forges, forges.Count)
	}
	if forges.Forges[0] != "gitea" || forges.Forges[1] != "github" {
		t.Errorf("the registry's sort order was not preserved: %v", forges.Forges)
	}
}

// TestHandleSDLCListForges_ServerError surfaces a server failure as a typed SDK
// error rather than an empty list, which would read as "this build carries no
// forge adapters".
func TestHandleSDLCListForges_ServerError(t *testing.T) {
	if _, err := handleSDLCForges(context.Background(), runnersForgesConn(t, true), &sdlc.ListForgesRequest{}); err == nil {
		t.Fatal("a failing server produced no error")
	}
}
