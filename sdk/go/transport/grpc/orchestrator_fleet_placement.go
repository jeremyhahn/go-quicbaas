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

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Fleet placement specs over gRPC (go-quicbaas#1681). An empty tenant is
// sent empty: the server reads it as the caller's tenant.

func fleetPlacementSpecRequestOf(req interface{}, op string) (*pb.FleetPlacementSpecRequest, error) {
	r, ok := req.(*orchestrator.FleetPlacementSpecRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	return &pb.FleetPlacementSpecRequest{TenantId: r.TenantID, Name: r.Name}, nil
}

// handleOrchestratorDeclareFleetPlacementSpec calls
// OrchestratorService.DeclareFleetPlacementSpec.
func handleOrchestratorDeclareFleetPlacementSpec(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.DeclareFleetPlacementSpec"
	r, ok := req.(*orchestrator.DeclareFleetPlacementSpecRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).DeclareFleetPlacementSpec(ctx, &pb.DeclareFleetPlacementSpecRequest{
		TenantId: r.TenantID,
		Spec:     fleetPlacementSpecToProto(r.Spec),
	})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return fleetPlacementSpecFromProto(resp), nil
}

// handleOrchestratorGetFleetPlacementSpec calls
// OrchestratorService.GetFleetPlacementSpec.
func handleOrchestratorGetFleetPlacementSpec(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.GetFleetPlacementSpec"
	in, err := fleetPlacementSpecRequestOf(req, op)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).GetFleetPlacementSpec(ctx, in)
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return fleetPlacementSpecFromProto(resp), nil
}

// handleOrchestratorListFleetPlacementSpecs calls
// OrchestratorService.ListFleetPlacementSpecs.
func handleOrchestratorListFleetPlacementSpecs(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.ListFleetPlacementSpecs"
	r, ok := req.(*orchestrator.ListFleetPlacementSpecsRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).ListFleetPlacementSpecs(ctx, &pb.ListFleetPlacementSpecsRequest{TenantId: r.TenantID})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &orchestrator.ListFleetPlacementSpecsResponse{Specs: make([]*orchestrator.FleetPlacementSpec, 0, len(resp.GetSpecs()))}
	for _, spec := range resp.GetSpecs() {
		out.Specs = append(out.Specs, fleetPlacementSpecFromProto(spec))
	}
	out.Count = len(out.Specs)
	return out, nil
}

// handleOrchestratorWithdrawFleetPlacementSpec calls
// OrchestratorService.WithdrawFleetPlacementSpec.
func handleOrchestratorWithdrawFleetPlacementSpec(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.WithdrawFleetPlacementSpec"
	in, err := fleetPlacementSpecRequestOf(req, op)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).WithdrawFleetPlacementSpec(ctx, in)
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return &orchestrator.WithdrawFleetPlacementSpecResponse{Name: resp.GetName(), Withdrawn: resp.GetWithdrawn()}, nil
}

// handleOrchestratorGetFleetPlacementDrift calls
// OrchestratorService.GetFleetPlacementDrift.
func handleOrchestratorGetFleetPlacementDrift(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.GetFleetPlacementDrift"
	in, err := fleetPlacementSpecRequestOf(req, op)
	if err != nil {
		return nil, err
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).GetFleetPlacementDrift(ctx, in)
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return fleetPlacementDriftFromProto(resp), nil
}

func handleOrchestratorListFleetPlacementConsents(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.ListFleetPlacementConsents"
	r, ok := req.(*orchestrator.ListFleetPlacementConsentsRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).ListFleetPlacementConsents(ctx,
		&pb.ListFleetPlacementConsentsRequest{TenantId: r.TenantID, State: r.State})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	out := &orchestrator.ListFleetPlacementConsentsResponse{
		Requests: make([]*orchestrator.FleetPlacementConsentRequest, 0, len(resp.GetRequests())),
		Count:    int(resp.GetCount()),
	}
	for _, c := range resp.GetRequests() {
		out.Requests = append(out.Requests, fleetPlacementConsentFromProto(c))
	}
	return out, nil
}

func handleOrchestratorAnswerFleetPlacementConsent(ctx context.Context, conn *grpc.ClientConn, req interface{}) (interface{}, error) {
	const op = "grpc.orchestrator.AnswerFleetPlacementConsent"
	r, ok := req.(*orchestrator.AnswerFleetPlacementConsentRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type")
	}
	resp, err := pb.NewOrchestratorServiceClient(conn).AnswerFleetPlacementConsent(ctx,
		&pb.AnswerFleetPlacementConsentRequest{TenantId: r.TenantID, Id: r.ID, State: r.State, Reason: r.Reason})
	if err != nil {
		return nil, convertGRPCError(err, op)
	}
	return fleetPlacementConsentFromProto(resp), nil
}

func fleetPlacementSpecToProto(in *orchestrator.FleetPlacementSpec) *pb.FleetPlacementSpec {
	if in == nil {
		return nil
	}
	out := &pb.FleetPlacementSpec{
		TenantId:             in.TenantID,
		Name:                 in.Name,
		Role:                 in.Role,
		HostCount:            int64(in.HostCount),
		Hosts:                in.Hosts,
		RequiredCapabilities: in.RequiredCapabilities,
		Actuate:              in.Actuate,
		Generation:           in.Generation,
	}
	if p := in.Profile; p != nil {
		out.Profile = &pb.FleetPlacementProfile{
			CpuCores:    p.CPUCores,
			MemoryBytes: p.MemoryBytes,
			Measured:    p.Measured,
			SampleSize:  int64(p.SampleSize),
		}
		if !p.UpdatedAt.IsZero() {
			out.Profile.UpdatedAt = timestamppb.New(p.UpdatedAt)
		}
	}
	return out
}

func fleetPlacementSpecFromProto(in *pb.FleetPlacementSpec) *orchestrator.FleetPlacementSpec {
	if in == nil {
		return nil
	}
	out := &orchestrator.FleetPlacementSpec{
		TenantID:             in.GetTenantId(),
		Name:                 in.GetName(),
		Role:                 in.GetRole(),
		HostCount:            int(in.GetHostCount()),
		Hosts:                in.GetHosts(),
		RequiredCapabilities: in.GetRequiredCapabilities(),
		Actuate:              in.GetActuate(),
		Generation:           in.GetGeneration(),
	}
	if p := in.GetProfile(); p != nil {
		out.Profile = &orchestrator.FleetPlacementProfile{
			CPUCores:    p.GetCpuCores(),
			MemoryBytes: p.GetMemoryBytes(),
			Measured:    p.GetMeasured(),
			SampleSize:  int(p.GetSampleSize()),
		}
		if p.GetUpdatedAt() != nil {
			out.Profile.UpdatedAt = p.GetUpdatedAt().AsTime()
		}
	}
	return out
}

func fleetPlacementDriftFromProto(in *pb.FleetPlacementDrift) *orchestrator.FleetPlacementDrift {
	out := &orchestrator.FleetPlacementDrift{
		TenantID:        in.GetTenantId(),
		Spec:            in.GetSpec(),
		Role:            in.GetRole(),
		Generation:      in.GetGeneration(),
		Disposition:     in.GetDisposition(),
		ObservedWork:    int(in.GetObservedWork()),
		AdmissibleHosts: in.GetAdmissibleHosts(),
		Findings:        make([]orchestrator.FleetPlacementFinding, 0, len(in.GetFindings())),
	}
	for _, f := range in.GetFindings() {
		out.Findings = append(out.Findings, orchestrator.FleetPlacementFinding{
			Kind:    f.GetKind(),
			Field:   f.GetField(),
			Host:    f.GetHost(),
			WorkID:  f.GetWorkId(),
			Missing: f.GetMissing(),
			Remedy: orchestrator.FleetPlacementRemedy{
				Action:       f.GetRemedy().GetAction(),
				From:         f.GetRemedy().GetFrom(),
				Destinations: f.GetRemedy().GetDestinations(),
			},
		})
	}
	return out
}

// fleetPlacementConsentFromProto maps a consent request off the wire. An
// unset timestamp stays the zero time.
func fleetPlacementConsentFromProto(in *pb.FleetPlacementConsentRequest) *orchestrator.FleetPlacementConsentRequest {
	out := &orchestrator.FleetPlacementConsentRequest{
		ID:       in.GetId(),
		TenantID: in.GetTenantId(),
		Move: orchestrator.FleetPlacementConsentMove{
			SpecName:       in.GetMove().GetSpecName(),
			SpecGeneration: in.GetMove().GetSpecGeneration(),
			Class:          in.GetMove().GetClass(),
			WorkID:         in.GetMove().GetWorkId(),
			SourceHost:     in.GetMove().GetSourceHost(),
		},
		State:  in.GetState(),
		By:     in.GetBy(),
		Reason: in.GetReason(),
	}
	if in.GetRaisedAt() != nil {
		out.RaisedAt = in.GetRaisedAt().AsTime()
	}
	if in.GetAnsweredAt() != nil {
		out.AnsweredAt = in.GetAnsweredAt().AsTime()
	}
	return out
}
