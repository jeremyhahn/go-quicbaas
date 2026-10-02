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
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// placementService returns the placement half, or Unavailable when this
// node has no placement engine (no qrdb client).
func (s *SchedulerServer) placementService() (service.PlacementService, error) {
	if s.placementSvc == nil {
		return nil, status.Error(codes.Unavailable, "scheduler placement service not available")
	}
	return s.placementSvc, nil
}

// RegisterNode registers or refreshes a node's facts under a lease.
func (s *SchedulerServer) RegisterNode(ctx context.Context, req *pb.RegisterPlacementNodeRequest) (*pb.PlacementNodeResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	info, err := svc.RegisterNode(ctx, &service.RegisterPlacementNodeRequest{
		Facts:      PlacementFactsFromProto(req.Facts),
		TTLSeconds: req.TtlSeconds,
	})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.PlacementNodeResponse{Node: PlacementNodeInfoToProto(info)}, nil
}

// RenewNode heartbeats a node's lease.
func (s *SchedulerServer) RenewNode(ctx context.Context, req *pb.PlacementNodeRequest) (*pb.RenewPlacementNodeResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	if err := svc.RenewNode(ctx, req.GetNodeId()); err != nil {
		return nil, toProtoError(err)
	}
	return &pb.RenewPlacementNodeResponse{NodeId: req.GetNodeId(), Renewed: true}, nil
}

// DeregisterNode revokes a node's lease and clears its status.
func (s *SchedulerServer) DeregisterNode(ctx context.Context, req *pb.PlacementNodeRequest) (*pb.DeregisterPlacementNodeResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	if err := svc.DeregisterNode(ctx, req.GetNodeId()); err != nil {
		return nil, toProtoError(err)
	}
	return &pb.DeregisterPlacementNodeResponse{NodeId: req.GetNodeId(), Deregistered: true}, nil
}

// nodeOp is a placement call keyed by a node ID that returns the node.
type nodeOp func(svc service.PlacementService, ctx context.Context, nodeID string) (*placement.NodeInfo, error)

// serveNodeOp runs a node-returning placement call.
func (s *SchedulerServer) serveNodeOp(ctx context.Context, req *pb.PlacementNodeRequest, op nodeOp) (*pb.PlacementNodeResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	info, err := op(svc, ctx, req.GetNodeId())
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.PlacementNodeResponse{Node: PlacementNodeInfoToProto(info)}, nil
}

// GetNode returns a live node.
func (s *SchedulerServer) GetNode(ctx context.Context, req *pb.PlacementNodeRequest) (*pb.PlacementNodeResponse, error) {
	return s.serveNodeOp(ctx, req, service.PlacementService.GetNode)
}

// CordonNode excludes a node from new placements.
func (s *SchedulerServer) CordonNode(ctx context.Context, req *pb.PlacementNodeRequest) (*pb.PlacementNodeResponse, error) {
	return s.serveNodeOp(ctx, req, service.PlacementService.CordonNode)
}

// UncordonNode returns a node to service.
func (s *SchedulerServer) UncordonNode(ctx context.Context, req *pb.PlacementNodeRequest) (*pb.PlacementNodeResponse, error) {
	return s.serveNodeOp(ctx, req, service.PlacementService.UncordonNode)
}

// ListNodes returns every live node.
func (s *SchedulerServer) ListNodes(ctx context.Context, _ *pb.ListPlacementNodesRequest) (*pb.ListPlacementNodesResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	nodes, err := svc.ListNodes(ctx)
	if err != nil {
		return nil, toProtoError(err)
	}
	out := &pb.ListPlacementNodesResponse{Nodes: make([]*pb.PlacementNodeInfo, 0, len(nodes))}
	for _, n := range nodes {
		out.Nodes = append(out.Nodes, PlacementNodeInfoToProto(n))
	}
	return out, nil
}

// DrainNode marks a node draining and returns the placements to move.
func (s *SchedulerServer) DrainNode(ctx context.Context, req *pb.PlacementNodeRequest) (*pb.DrainPlacementNodeResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	moved, err := svc.DrainNode(ctx, req.GetNodeId())
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.DrainPlacementNodeResponse{NodeId: req.GetNodeId(), Placements: PlacementRecordsToProto(moved)}, nil
}

// Place decides and records the node a workload runs on.
func (s *SchedulerServer) Place(ctx context.Context, req *pb.PlaceWorkloadRequest) (*pb.PlaceWorkloadResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	res, err := svc.Place(ctx, PlacementRequestFromProto(req))
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.PlaceWorkloadResponse{Placement: PlacementRecordToProto(res.Placement), Existing: res.Existing}, nil
}

// Release removes a workload's placement.
func (s *SchedulerServer) Release(ctx context.Context, req *pb.ReleaseWorkloadRequest) (*pb.ReleaseWorkloadResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	if err := svc.Release(ctx, req.GetWorkloadId()); err != nil {
		return nil, toProtoError(err)
	}
	return &pb.ReleaseWorkloadResponse{WorkloadId: req.GetWorkloadId(), Released: true}, nil
}

// GetPlacement returns a workload's placement.
func (s *SchedulerServer) GetPlacement(ctx context.Context, req *pb.GetPlacementRequest) (*pb.GetPlacementResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	pl, err := svc.GetPlacement(ctx, req.GetWorkloadId())
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.GetPlacementResponse{Placement: PlacementRecordToProto(pl)}, nil
}

// ListPlacements returns placements, optionally filtered.
func (s *SchedulerServer) ListPlacements(ctx context.Context, req *pb.ListPlacementsRequest) (*pb.ListPlacementsResponse, error) {
	svc, err := s.placementService()
	if err != nil {
		return nil, err
	}
	out, err := svc.ListPlacements(ctx, &placement.PlacementFilter{NodeID: req.GetNodeId(), Group: req.GetGroup()})
	if err != nil {
		return nil, toProtoError(err)
	}
	return &pb.ListPlacementsResponse{Placements: PlacementRecordsToProto(out)}, nil
}

// =============================================================================
// Conversions
// =============================================================================

// PlacementFactsFromProto converts proto node facts.
func PlacementFactsFromProto(f *pb.PlacementNodeFacts) placement.NodeFacts {
	if f == nil {
		return placement.NodeFacts{}
	}
	return placement.NodeFacts{NodeID: f.NodeId, Capacity: f.Capacity, Labels: f.Labels}
}

// PlacementNodeInfoToProto converts a node to proto.
func PlacementNodeInfoToProto(n *placement.NodeInfo) *pb.PlacementNodeInfo {
	if n == nil {
		return nil
	}
	return &pb.PlacementNodeInfo{
		Facts:                 &pb.PlacementNodeFacts{NodeId: n.Facts.NodeID, Capacity: n.Facts.Capacity, Labels: n.Facts.Labels},
		Status:                &pb.PlacementNodeStatus{Unschedulable: n.Status.Unschedulable, Draining: n.Status.Draining},
		LeaseId:               n.LeaseID,
		TtlSeconds:            n.TTLSeconds,
		Allocated:             n.Allocated,
		Placements:            n.Placements,
		RegisteredAtUnixNanos: unixNanos(n.RegisteredAt),
	}
}

// PlacementRecordToProto converts a placement to proto.
func PlacementRecordToProto(p *placement.Placement) *pb.PlacementRecord {
	if p == nil {
		return nil
	}
	return &pb.PlacementRecord{
		WorkloadId:         p.WorkloadID,
		Group:              p.Group,
		NodeId:             p.NodeID,
		Resources:          p.Resources,
		Labels:             p.Labels,
		Score:              p.Score,
		CreatedAtUnixNanos: unixNanos(p.CreatedAt),
		Orphaned:           p.Orphaned,
	}
}

// PlacementRecordsToProto converts a placement list to proto.
func PlacementRecordsToProto(in []*placement.Placement) []*pb.PlacementRecord {
	out := make([]*pb.PlacementRecord, 0, len(in))
	for _, p := range in {
		out = append(out, PlacementRecordToProto(p))
	}
	return out
}

// PlacementRequestFromProto converts a proto placement request.
func PlacementRequestFromProto(req *pb.PlaceWorkloadRequest) *placement.PlacementRequest {
	out := &placement.PlacementRequest{
		WorkloadID: req.WorkloadId,
		Group:      req.Group,
		Labels:     req.Labels,
		Resources:  req.Resources,
		Strategy:   placement.Strategy(req.Strategy),
	}
	for _, c := range req.Constraints {
		out.Constraints = append(out.Constraints, placement.Constraint{Key: c.Key, Operator: placement.Operator(c.Operator), Values: c.Values})
	}
	out.Affinity = affinityTermsFromProto(req.Affinity)
	out.AntiAffinity = affinityTermsFromProto(req.AntiAffinity)
	if req.Spread != nil {
		out.Spread = &placement.SpreadConstraint{
			TopologyKey: req.Spread.TopologyKey,
			MaxSkew:     int(req.Spread.MaxSkew),
			Required:    req.Spread.Required,
		}
	}
	return out
}

// affinityTermsFromProto converts proto affinity terms.
func affinityTermsFromProto(in []*pb.PlacementAffinityTerm) []placement.AffinityTerm {
	if len(in) == 0 {
		return nil
	}
	out := make([]placement.AffinityTerm, 0, len(in))
	for _, t := range in {
		out = append(out, placement.AffinityTerm{Group: t.Group, Required: t.Required, Weight: int(t.Weight)})
	}
	return out
}

// unixNanos renders a time as Unix nanoseconds, zero for the zero time.
func unixNanos(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixNano()
}
