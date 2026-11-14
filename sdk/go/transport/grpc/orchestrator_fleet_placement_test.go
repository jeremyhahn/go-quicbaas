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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

type recordingFleetPlacementServer struct {
	pb.UnimplementedOrchestratorServiceServer
	declared *pb.DeclareFleetPlacementSpecRequest
	named    []*pb.FleetPlacementSpecRequest
	listed   *pb.ListFleetPlacementSpecsRequest
}

func (s *recordingFleetPlacementServer) DeclareFleetPlacementSpec(_ context.Context, req *pb.DeclareFleetPlacementSpecRequest) (*pb.FleetPlacementSpec, error) {
	s.declared = req
	out := proto.Clone(req.GetSpec()).(*pb.FleetPlacementSpec)
	out.TenantId, out.Generation = "acme", 1
	return out, nil
}

func (s *recordingFleetPlacementServer) GetFleetPlacementSpec(_ context.Context, req *pb.FleetPlacementSpecRequest) (*pb.FleetPlacementSpec, error) {
	s.named = append(s.named, req)
	if req.GetName() == "absent" {
		return nil, status.Error(codes.NotFound, "no spec absent")
	}
	return &pb.FleetPlacementSpec{TenantId: "acme", Name: req.GetName(), Role: "agent", Generation: 1}, nil
}

func (s *recordingFleetPlacementServer) ListFleetPlacementSpecs(_ context.Context, req *pb.ListFleetPlacementSpecsRequest) (*pb.ListFleetPlacementSpecsResponse, error) {
	s.listed = req
	return &pb.ListFleetPlacementSpecsResponse{Specs: []*pb.FleetPlacementSpec{{Name: "agents"}}, Count: 1}, nil
}

func (s *recordingFleetPlacementServer) WithdrawFleetPlacementSpec(_ context.Context, req *pb.FleetPlacementSpecRequest) (*pb.WithdrawFleetPlacementSpecResponse, error) {
	s.named = append(s.named, req)
	return &pb.WithdrawFleetPlacementSpecResponse{Name: req.GetName(), Withdrawn: true}, nil
}

func (s *recordingFleetPlacementServer) GetFleetPlacementDrift(_ context.Context, req *pb.FleetPlacementSpecRequest) (*pb.FleetPlacementDrift, error) {
	s.named = append(s.named, req)
	return &pb.FleetPlacementDrift{
		Spec: req.GetName(), ObservedWork: 2,
		Findings: []*pb.FleetPlacementFinding{{Kind: "host_not_admitted", Host: "h2", Remedy: &pb.FleetPlacementRemedy{Action: "move", Destinations: []string{"h1"}}}},
	}, nil
}

func TestOrchestratorFleetPlacement_OverGRPC(t *testing.T) {
	fake := &recordingFleetPlacementServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) { pb.RegisterOrchestratorServiceServer(s, fake) })
	h := newOrchestratorHandler(srv.dial(t))
	ctx := context.Background()
	call := func(method string, req interface{}) (interface{}, error) {
		return h.handleRequest(ctx, "quicbaas.Orchestrator", method, req)
	}

	resp, err := call("DeclareFleetPlacementSpec", &orchestrator.DeclareFleetPlacementSpecRequest{
		TenantID: "acme",
		Spec:     &orchestrator.FleetPlacementSpec{Name: "agents", Role: "agent", Hosts: []string{"h1"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "acme", fake.declared.GetTenantId())
	assert.Equal(t, []string{"h1"}, fake.declared.GetSpec().GetHosts())
	assert.Equal(t, uint64(1), resp.(*orchestrator.FleetPlacementSpec).Generation)

	resp, err = call("GetFleetPlacementSpec", &orchestrator.FleetPlacementSpecRequest{Name: "agents"})
	require.NoError(t, err)
	assert.Equal(t, "agent", resp.(*orchestrator.FleetPlacementSpec).Role)

	resp, err = call("ListFleetPlacementSpecs", &orchestrator.ListFleetPlacementSpecsRequest{TenantID: "acme"})
	require.NoError(t, err)
	assert.Equal(t, "acme", fake.listed.GetTenantId())
	assert.Equal(t, 1, resp.(*orchestrator.ListFleetPlacementSpecsResponse).Count)

	resp, err = call("GetFleetPlacementDrift", &orchestrator.FleetPlacementSpecRequest{Name: "agents"})
	require.NoError(t, err)
	report := resp.(*orchestrator.FleetPlacementDrift)
	assert.Equal(t, 2, report.ObservedWork)
	require.Len(t, report.Findings, 1)
	assert.Equal(t, "move", report.Findings[0].Remedy.Action)
	assert.Equal(t, []string{"h1"}, report.Findings[0].Remedy.Destinations)

	resp, err = call("WithdrawFleetPlacementSpec", &orchestrator.FleetPlacementSpecRequest{Name: "agents"})
	require.NoError(t, err)
	assert.True(t, resp.(*orchestrator.WithdrawFleetPlacementSpecResponse).Withdrawn)

	_, err = call("GetFleetPlacementSpec", &orchestrator.FleetPlacementSpecRequest{Name: "absent"})
	assert.True(t, sdkerrors.IsNotFound(err), "absent = %v, want not found", err)

	_, err = call("GetFleetPlacementDrift", &orchestrator.ListFleetPlacementSpecsRequest{})
	assert.True(t, sdkerrors.IsInvalidInput(err), "wrong request type = %v, want invalid input", err)
}

// Every field of a spec survives the trip to proto and back, so a field
// added to the SDK type and not the message fails here.
func TestOrchestratorFleetPlacement_SpecProtoRoundTrip(t *testing.T) {
	spec := &orchestrator.FleetPlacementSpec{
		TenantID: "acme", Name: "n", Role: "r", HostCount: 2, Hosts: []string{"h"},
		Profile: &orchestrator.FleetPlacementProfile{CPUCores: 1.5, MemoryBytes: 3, Measured: true, SampleSize: 4,
			UpdatedAt: time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)},
		RequiredCapabilities: []string{"gpu"}, Actuate: true, Generation: 7,
	}
	if back := fleetPlacementSpecFromProto(fleetPlacementSpecToProto(spec)); !reflect.DeepEqual(spec, back) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", back, spec)
	}
	assert.Equal(t, 9, reflect.TypeOf(orchestrator.FleetPlacementSpec{}).NumField(), "map the new field here")
	assert.Equal(t, 8, reflect.TypeOf(orchestrator.FleetPlacementDrift{}).NumField(), "map the new field here")
}
