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

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Fleet placement specs over gRPC (SPEC row declarative-placement,
// go-quicbaas#1681). The service enforces the tenant and validates the spec;
// these methods convert and map its errors with ToGRPCError: invalid is
// InvalidArgument, another tenant PermissionDenied, absent NotFound, a stale
// generation (or a create of an existing spec) AlreadyExists, an incomplete report FailedPrecondition and an unbound
// service Unavailable.

// WithFleetPlacement serves the fleet placement verbs from provider, asked on
// every call so a service bound after construction is seen. It returns s.
func (s *OrchestratorServer) WithFleetPlacement(provider service.FleetPlacementProvider) *OrchestratorServer {
	s.fleetPlacement = provider
	return s
}

func (s *OrchestratorServer) fleetPlacementSvc() (service.FleetPlacementService, error) {
	if s.fleetPlacement == nil || s.fleetPlacement.FleetPlacement() == nil {
		return nil, status.Error(codes.Unavailable, "fleet placement service not available")
	}
	return s.fleetPlacement.FleetPlacement(), nil
}

// DeclareFleetPlacementSpec creates or replaces a fleet placement spec.
func (s *OrchestratorServer) DeclareFleetPlacementSpec(ctx context.Context, req *pb.DeclareFleetPlacementSpecRequest) (*pb.FleetPlacementSpec, error) {
	if req == nil || req.GetSpec() == nil {
		return nil, status.Error(codes.InvalidArgument, "spec is required")
	}
	svc, err := s.fleetPlacementSvc()
	if err != nil {
		return nil, err
	}
	stored, err := svc.DeclarePlacementSpec(ctx, req.GetTenantId(), fleetPlacementSpecFromProto(req.GetSpec()))
	if err != nil {
		return nil, ToGRPCError(err)
	}
	return fleetPlacementSpecToProto(stored), nil
}

// GetFleetPlacementSpec returns one fleet placement spec.
func (s *OrchestratorServer) GetFleetPlacementSpec(ctx context.Context, req *pb.FleetPlacementSpecRequest) (*pb.FleetPlacementSpec, error) {
	svc, err := s.fleetPlacementSvc()
	if err != nil {
		return nil, err
	}
	spec, err := svc.GetPlacementSpec(ctx, req.GetTenantId(), req.GetName())
	if err != nil {
		return nil, ToGRPCError(err)
	}
	return fleetPlacementSpecToProto(spec), nil
}

// ListFleetPlacementSpecs returns a tenant's fleet placement specs.
func (s *OrchestratorServer) ListFleetPlacementSpecs(ctx context.Context, req *pb.ListFleetPlacementSpecsRequest) (*pb.ListFleetPlacementSpecsResponse, error) {
	svc, err := s.fleetPlacementSvc()
	if err != nil {
		return nil, err
	}
	specs, err := svc.ListPlacementSpecs(ctx, req.GetTenantId())
	if err != nil {
		return nil, ToGRPCError(err)
	}
	out := &pb.ListFleetPlacementSpecsResponse{Specs: make([]*pb.FleetPlacementSpec, 0, len(specs))}
	for _, spec := range specs {
		out.Specs = append(out.Specs, fleetPlacementSpecToProto(spec))
	}
	out.Count = pageInt32(len(out.Specs))
	return out, nil
}

// WithdrawFleetPlacementSpec removes a fleet placement spec.
func (s *OrchestratorServer) WithdrawFleetPlacementSpec(ctx context.Context, req *pb.FleetPlacementSpecRequest) (*pb.WithdrawFleetPlacementSpecResponse, error) {
	svc, err := s.fleetPlacementSvc()
	if err != nil {
		return nil, err
	}
	if err := svc.WithdrawPlacementSpec(ctx, req.GetTenantId(), req.GetName()); err != nil {
		return nil, ToGRPCError(err)
	}
	return &pb.WithdrawFleetPlacementSpecResponse{Name: req.GetName(), Withdrawn: true}, nil
}

// GetFleetPlacementDrift reports a spec's role work against the hosts it
// admits.
func (s *OrchestratorServer) GetFleetPlacementDrift(ctx context.Context, req *pb.FleetPlacementSpecRequest) (*pb.FleetPlacementDrift, error) {
	svc, err := s.fleetPlacementSvc()
	if err != nil {
		return nil, err
	}
	report, err := svc.GetPlacementDrift(ctx, req.GetTenantId(), req.GetName())
	if err != nil {
		return nil, ToGRPCError(err)
	}
	return fleetPlacementDriftToProto(report), nil
}

// ListFleetPlacementConsents lists the tenant's per-move placement consent
// requests, all of them or those in the request's state.
func (s *OrchestratorServer) ListFleetPlacementConsents(ctx context.Context, req *pb.ListFleetPlacementConsentsRequest) (*pb.ListFleetPlacementConsentsResponse, error) {
	svc, err := s.fleetPlacementSvc()
	if err != nil {
		return nil, err
	}
	requests, err := svc.ListConsentRequests(ctx, req.GetTenantId(), orchestration.ConsentState(req.GetState()))
	if err != nil {
		return nil, ToGRPCError(err)
	}
	out := &pb.ListFleetPlacementConsentsResponse{
		Requests: make([]*pb.FleetPlacementConsentRequest, 0, len(requests)),
	}
	for _, r := range requests {
		out.Requests = append(out.Requests, fleetPlacementConsentToProto(r))
	}
	out.Count = pageInt32(len(out.Requests))
	return out, nil
}

// AnswerFleetPlacementConsent grants or refuses one pending consent request
// as the authenticated subject. The request names no answerer.
func (s *OrchestratorServer) AnswerFleetPlacementConsent(ctx context.Context, req *pb.AnswerFleetPlacementConsentRequest) (*pb.FleetPlacementConsentRequest, error) {
	svc, err := s.fleetPlacementSvc()
	if err != nil {
		return nil, err
	}
	answered, err := svc.AnswerConsent(ctx, req.GetTenantId(), req.GetId(), orchestration.ConsentAnswer{
		State: orchestration.ConsentState(req.GetState()), Reason: req.GetReason(),
	})
	if err != nil {
		return nil, ToGRPCError(err)
	}
	return fleetPlacementConsentToProto(answered), nil
}

// fleetPlacementConsentToProto maps a consent request to its message. A zero
// time is left unset.
func fleetPlacementConsentToProto(in *orchestration.PlacementConsentRequest) *pb.FleetPlacementConsentRequest {
	if in == nil {
		return nil
	}
	out := &pb.FleetPlacementConsentRequest{
		Id:       in.ID,
		TenantId: in.TenantID,
		Move: &pb.FleetPlacementConsentMove{
			SpecName:       in.Move.SpecName,
			SpecGeneration: in.Move.SpecGeneration,
			Class:          in.Move.Class,
			WorkId:         in.Move.WorkID,
			SourceHost:     in.Move.SourceHost,
		},
		State:  string(in.State),
		By:     in.By,
		Reason: in.Reason,
	}
	if !in.RaisedAt.IsZero() {
		out.RaisedAt = timestamppb.New(in.RaisedAt)
	}
	if !in.AnsweredAt.IsZero() {
		out.AnsweredAt = timestamppb.New(in.AnsweredAt)
	}
	return out
}

func fleetPlacementSpecFromProto(in *pb.FleetPlacementSpec) *orchestration.PlacementSpec {
	out := &orchestration.PlacementSpec{
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
		out.Profile = &orchestration.FleetJobProfile{
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

func fleetPlacementSpecToProto(in *orchestration.PlacementSpec) *pb.FleetPlacementSpec {
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

func fleetPlacementDriftToProto(in *orchestration.DriftReport) *pb.FleetPlacementDrift {
	if in == nil {
		return nil
	}
	out := &pb.FleetPlacementDrift{
		TenantId:        in.TenantID,
		Spec:            in.Spec,
		Role:            in.Role,
		Generation:      in.Generation,
		Disposition:     string(in.Disposition),
		ObservedWork:    int64(in.ObservedWork),
		AdmissibleHosts: in.AdmissibleHosts,
		Findings:        make([]*pb.FleetPlacementFinding, 0, len(in.Findings)),
	}
	for _, f := range in.Findings {
		out.Findings = append(out.Findings, &pb.FleetPlacementFinding{
			Kind:    string(f.Kind),
			Field:   f.Field,
			Host:    f.Host,
			WorkId:  f.WorkID,
			Missing: f.Missing,
			Remedy: &pb.FleetPlacementRemedy{
				Action:       string(f.Remedy.Action),
				From:         f.Remedy.From,
				Destinations: f.Remedy.Destinations,
			},
		})
	}
	return out
}
