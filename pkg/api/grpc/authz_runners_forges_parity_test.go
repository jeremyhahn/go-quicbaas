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
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// go-quicbaas#606 — ListRunners / ListForges cross-transport parity
//
// Both verbs shipped on REST alone. ListRunners reached MCP and the embedded
// transport as well; ListForges reached nothing else at all. A caller on gRPC —
// and so on the Unix socket, which serves the same wire over a UDS — was told
// the verb did not exist, which is the Phase 8 regression this repository's
// own rule forbids.
//
// The permissions below are NOT invented here. They are the REST route table's,
// read off pkg/api/rest/routes_permissions_generated.go:
//
//	GET /api/v1/orchestrator/runners -> orchestrator:status
//	GET /api/v1/sdlc/forges          -> sdlc:read
//
// so the same principal calling the same verb is admitted or denied the same
// way whichever wire carried the call. pkg/api/rest and pkg/api/mcp pin the
// same two pairs in their own packages, since neither table is exported.
// =============================================================================

// runnersForgesGRPCGrants is the canonical gRPC gate for the two verbs.
var runnersForgesGRPCGrants = map[string]qrdbToolResourceLike{
	"/quicbaas.orchestrator.v1.OrchestratorService/ListRunners": {"orchestrator", "status"},
	"/quicbaas.sdlc.v1.SDLCService/ListForges":                  {"sdlc", "read"},
}

// TestRunnersForgesGRPCPermissions pins the gRPC gate for both verbs.
//
// TestGRPCPermissions_ProtoOptionMatchesAuthzMapping separately proves the
// (qrdb.v1.permission) annotation on each RPC agrees with this table, so the
// two gRPC enforcement points cannot drift from each other either.
func TestRunnersForgesGRPCPermissions(t *testing.T) {
	for method, want := range runnersForgesGRPCGrants {
		got, ok := grpcMethodPermissions[method]
		if !ok {
			t.Errorf("method %q has no permission mapping: under RequireAuthz it is default-denied, "+
				"so the verb would be unreachable over gRPC and the Unix socket", method)
			continue
		}
		if got.resource != want.resource || got.action != want.action {
			t.Errorf("method %q is gated %s:%s, but the REST route serving the same verb is gated %s:%s; "+
				"the grant must not depend on which transport carried the call",
				method, got.resource, got.action, want.resource, want.action)
		}
	}
}

// TestRunnersForgesRPCsExistOnTheWire proves both verbs are RPCs on the
// services the broker registers, not merely table entries. A table entry
// naming an RPC nobody serves gates nothing.
func TestRunnersForgesRPCsExistOnTheWire(t *testing.T) {
	cases := []struct {
		service string
		methods []string
		rpc     string
	}{
		{"OrchestratorService", methodNamesOf(pb.OrchestratorService_ServiceDesc), "ListRunners"},
		{"SDLCService", methodNamesOf(pb.SDLCService_ServiceDesc), "ListForges"},
	}
	for _, tc := range cases {
		t.Run(tc.service+"/"+tc.rpc, func(t *testing.T) {
			for _, name := range tc.methods {
				if name == tc.rpc {
					return
				}
			}
			t.Errorf("%s carries no %s RPC: the verb is unreachable over gRPC and the Unix socket", tc.service, tc.rpc)
		})
	}
}

// methodNamesOf lists the unary and streaming method names a descriptor serves.
func methodNamesOf(desc grpc.ServiceDesc) []string {
	names := make([]string, 0, len(desc.Methods)+len(desc.Streams))
	for _, m := range desc.Methods {
		names = append(names, m.MethodName)
	}
	for _, s := range desc.Streams {
		names = append(names, s.StreamName)
	}
	return names
}

// TestSDLCServiceDescIsGuarded proves the SDLC descriptor is in the exhaustive
// guard's slice. quicbaasd registers the service (pkg/server/lifecycle.go), so
// leaving it out of brokerServiceDescs meant TestGRPCPermissions_EveryMethodMapped
// never looked at it and an unmapped SDLC RPC would have shipped silently.
func TestSDLCServiceDescIsGuarded(t *testing.T) {
	for _, desc := range brokerServiceDescs {
		if desc.ServiceName == pb.SDLCService_ServiceDesc.ServiceName {
			return
		}
	}
	t.Fatalf("brokerServiceDescs omits %s, so its methods escape the exhaustive permission guard",
		pb.SDLCService_ServiceDesc.ServiceName)
}

// =============================================================================
// Behaviour: both handlers answer, and answer the same thing REST answers.
// =============================================================================

// runningSDLCProvider is a node whose SDLC module is up. ListForges reads the
// process-wide forge registry rather than the service, so the service only has
// to be present — which is exactly the distinction the handler gates on.
type runningSDLCProvider struct{}

func (runningSDLCProvider) SDLC() *service.SDLCService { return &service.SDLCService{} }

// TestOrchestratorServer_ListRunners renders the process-wide runner registry.
// The orchestration package registers the "workflow" runner in its own init, so
// the registry is never empty in a build that carries the engine.
func TestOrchestratorServer_ListRunners(t *testing.T) {
	srv := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())

	resp, err := srv.ListRunners(context.Background(), &pb.OrchestratorListRunnersRequest{})
	if err != nil {
		t.Fatalf("ListRunners: %v", err)
	}

	registered := orchestration.JobRunners()
	if len(resp.GetRunners()) != len(registered) {
		t.Fatalf("got %d runners, registry holds %d", len(resp.GetRunners()), len(registered))
	}
	if int(resp.GetCount()) != len(registered) {
		t.Errorf("count is %d but %d runners were returned", resp.GetCount(), len(resp.GetRunners()))
	}
	for i, want := range registered {
		got := resp.GetRunners()[i]
		if got.GetName() != want.Name() {
			t.Fatalf("runner %d: got %q, want %q (registry order must be preserved)", i, got.GetName(), want.Name())
		}
		specs := want.States()
		if len(got.GetStates()) != len(specs) {
			t.Fatalf("runner %q: got %d states, want %d", want.Name(), len(got.GetStates()), len(specs))
		}
		for j, spec := range specs {
			state := got.GetStates()[j]
			if state.GetState() != string(spec.State) || state.GetLabel() != spec.Label ||
				state.GetSessionBacked() != spec.SessionBacked {
				t.Errorf("runner %q state %d: got %+v, want %+v", want.Name(), j, state, spec)
			}
		}
	}
}

// TestOrchestratorServer_ListRunnersWithoutTheOrchestrator refuses on a node
// that drives no jobs, matching the REST handler's 503 rather than answering
// with an empty list that would read as "this node has no runners".
func TestOrchestratorServer_ListRunnersWithoutTheOrchestrator(t *testing.T) {
	srv := NewOrchestratorServer(nil, createOrchestratorGRPCTestLogger())

	_, err := srv.ListRunners(context.Background(), &pb.OrchestratorListRunnersRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("ListRunners without an orchestrator: got %v, want Unavailable", err)
	}
}

// TestSDLCServer_ListForges renders the build's registered forge adapters. The
// sdlc package registers the Gitea factory in its own init, so a build carrying
// the module always carries at least that one.
func TestSDLCServer_ListForges(t *testing.T) {
	srv := NewSDLCServer(runningSDLCProvider{}, sdlcTestLogger(t))

	resp, err := srv.ListForges(context.Background(), &pb.ListForgesRequest{})
	if err != nil {
		t.Fatalf("ListForges: %v", err)
	}

	registered := sdlc.RegisteredForgeTypes()
	if len(resp.GetForges()) != len(registered) {
		t.Fatalf("got %d forges, registry holds %d", len(resp.GetForges()), len(registered))
	}
	if int(resp.GetCount()) != len(registered) {
		t.Errorf("count is %d but %d forges were returned", resp.GetCount(), len(resp.GetForges()))
	}
	for i, want := range registered {
		if resp.GetForges()[i] != string(want) {
			t.Fatalf("forge %d: got %q, want %q (the registry's sort order must be preserved)",
				i, resp.GetForges()[i], want)
		}
	}
}

// TestSDLCServer_ListForgesWithoutTheModule refuses on a node that does not run
// the module, matching the REST handler's 503: "the module is off" and "this
// build carries no adapters" are different answers and must stay different.
func TestSDLCServer_ListForgesWithoutTheModule(t *testing.T) {
	for name, srv := range map[string]*SDLCServer{
		"nil provider":    NewSDLCServer(nil, sdlcTestLogger(t)),
		"disabled module": NewSDLCServer(nilSDLCProvider{}, sdlcTestLogger(t)),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := srv.ListForges(context.Background(), &pb.ListForgesRequest{})
			if status.Code(err) != codes.Unavailable {
				t.Fatalf("ListForges: got %v, want Unavailable", err)
			}
		})
	}
}
