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

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// PlacementClient is the client half of the SchedulerService placement
// RPCs, in service-layer shape: it satisfies service.PlacementService, so a
// caller holding a connection uses it exactly as the in-process service.
type PlacementClient struct {
	rpc pb.SchedulerServiceClient
}

// Compile-time proof PlacementClient is a service.PlacementService.
var _ service.PlacementService = (*PlacementClient)(nil)

// Placement returns the placement client over this connection.
func (c *Client) Placement() *PlacementClient {
	return &PlacementClient{rpc: pb.NewSchedulerServiceClient(c.conn)}
}

// RegisterNode registers or refreshes a node's facts under a lease.
func (p *PlacementClient) RegisterNode(ctx context.Context, req *service.RegisterPlacementNodeRequest) (*placement.NodeInfo, error) {
	if req == nil {
		return nil, service.ErrPlacementNilRequest
	}
	resp, err := p.rpc.RegisterNode(ctx, &pb.RegisterPlacementNodeRequest{
		Facts:      &pb.PlacementNodeFacts{NodeId: req.Facts.NodeID, Capacity: req.Facts.Capacity, Labels: req.Facts.Labels},
		TtlSeconds: req.TTLSeconds,
	})
	if err != nil {
		return nil, convertError(err)
	}
	return PlacementNodeInfoFromProto(resp.Node), nil
}

// RenewNode heartbeats a node's lease.
func (p *PlacementClient) RenewNode(ctx context.Context, nodeID string) error {
	if _, err := p.rpc.RenewNode(ctx, &pb.PlacementNodeRequest{NodeId: nodeID}); err != nil {
		return convertError(err)
	}
	return nil
}

// DeregisterNode revokes a node's lease and clears its status.
func (p *PlacementClient) DeregisterNode(ctx context.Context, nodeID string) error {
	if _, err := p.rpc.DeregisterNode(ctx, &pb.PlacementNodeRequest{NodeId: nodeID}); err != nil {
		return convertError(err)
	}
	return nil
}

// nodeRPC is a node-keyed placement RPC returning the node.
type nodeRPC func(ctx context.Context, in *pb.PlacementNodeRequest) (*pb.PlacementNodeResponse, error)

// callNode runs a node-keyed RPC and converts the node.
func callNode(ctx context.Context, rpc nodeRPC, nodeID string) (*placement.NodeInfo, error) {
	resp, err := rpc(ctx, &pb.PlacementNodeRequest{NodeId: nodeID})
	if err != nil {
		return nil, convertError(err)
	}
	return PlacementNodeInfoFromProto(resp.Node), nil
}

// GetNode returns a live node.
func (p *PlacementClient) GetNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	return callNode(ctx, func(ctx context.Context, in *pb.PlacementNodeRequest) (*pb.PlacementNodeResponse, error) {
		return p.rpc.GetNode(ctx, in)
	}, nodeID)
}

// CordonNode excludes a node from new placements.
func (p *PlacementClient) CordonNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	return callNode(ctx, func(ctx context.Context, in *pb.PlacementNodeRequest) (*pb.PlacementNodeResponse, error) {
		return p.rpc.CordonNode(ctx, in)
	}, nodeID)
}

// UncordonNode returns a node to service.
func (p *PlacementClient) UncordonNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	return callNode(ctx, func(ctx context.Context, in *pb.PlacementNodeRequest) (*pb.PlacementNodeResponse, error) {
		return p.rpc.UncordonNode(ctx, in)
	}, nodeID)
}

// ListNodes returns every live node.
func (p *PlacementClient) ListNodes(ctx context.Context) ([]*placement.NodeInfo, error) {
	resp, err := p.rpc.ListNodes(ctx, &pb.ListPlacementNodesRequest{})
	if err != nil {
		return nil, convertError(err)
	}
	out := make([]*placement.NodeInfo, 0, len(resp.Nodes))
	for _, n := range resp.Nodes {
		out = append(out, PlacementNodeInfoFromProto(n))
	}
	return out, nil
}

// DrainNode marks a node draining and returns the placements to move.
func (p *PlacementClient) DrainNode(ctx context.Context, nodeID string) ([]*placement.Placement, error) {
	resp, err := p.rpc.DrainNode(ctx, &pb.PlacementNodeRequest{NodeId: nodeID})
	if err != nil {
		return nil, convertError(err)
	}
	return PlacementRecordsFromProto(resp.Placements), nil
}

// Place decides and records the node a workload runs on.
func (p *PlacementClient) Place(ctx context.Context, req *placement.PlacementRequest) (*placement.PlaceResult, error) {
	if req == nil {
		return nil, service.ErrPlacementNilRequest
	}
	resp, err := p.rpc.Place(ctx, PlacementRequestToProto(req))
	if err != nil {
		return nil, convertError(err)
	}
	return &placement.PlaceResult{Placement: PlacementRecordFromProto(resp.Placement), Existing: resp.Existing}, nil
}

// Release removes a workload's placement.
func (p *PlacementClient) Release(ctx context.Context, workloadID string) error {
	if _, err := p.rpc.Release(ctx, &pb.ReleaseWorkloadRequest{WorkloadId: workloadID}); err != nil {
		return convertError(err)
	}
	return nil
}

// GetPlacement returns a workload's placement.
func (p *PlacementClient) GetPlacement(ctx context.Context, workloadID string) (*placement.Placement, error) {
	resp, err := p.rpc.GetPlacement(ctx, &pb.GetPlacementRequest{WorkloadId: workloadID})
	if err != nil {
		return nil, convertError(err)
	}
	return PlacementRecordFromProto(resp.Placement), nil
}

// ListPlacements returns placements, optionally filtered.
func (p *PlacementClient) ListPlacements(ctx context.Context, filter *placement.PlacementFilter) ([]*placement.Placement, error) {
	in := &pb.ListPlacementsRequest{}
	if filter != nil {
		in.NodeId, in.Group = filter.NodeID, filter.Group
	}
	resp, err := p.rpc.ListPlacements(ctx, in)
	if err != nil {
		return nil, convertError(err)
	}
	return PlacementRecordsFromProto(resp.Placements), nil
}

// PlacementNodeInfoFromProto converts a proto node.
func PlacementNodeInfoFromProto(n *pb.PlacementNodeInfo) *placement.NodeInfo {
	if n == nil {
		return nil
	}
	info := &placement.NodeInfo{
		Facts:        PlacementFactsFromProto(n.Facts),
		LeaseID:      n.LeaseId,
		TTLSeconds:   n.TtlSeconds,
		Allocated:    n.Allocated,
		Placements:   n.Placements,
		RegisteredAt: fromUnixNanos(n.RegisteredAtUnixNanos),
	}
	if n.Status != nil {
		info.Status = placement.NodeStatus{Unschedulable: n.Status.Unschedulable, Draining: n.Status.Draining}
	}
	return info
}

// PlacementRecordFromProto converts a proto placement.
func PlacementRecordFromProto(p *pb.PlacementRecord) *placement.Placement {
	if p == nil {
		return nil
	}
	return &placement.Placement{
		WorkloadID: p.WorkloadId,
		Group:      p.Group,
		NodeID:     p.NodeId,
		Resources:  p.Resources,
		Labels:     p.Labels,
		Score:      p.Score,
		CreatedAt:  fromUnixNanos(p.CreatedAtUnixNanos),
		Orphaned:   p.Orphaned,
	}
}

// PlacementRecordsFromProto converts a proto placement list.
func PlacementRecordsFromProto(in []*pb.PlacementRecord) []*placement.Placement {
	out := make([]*placement.Placement, 0, len(in))
	for _, p := range in {
		out = append(out, PlacementRecordFromProto(p))
	}
	return out
}

// PlacementRequestToProto converts a placement request to proto.
func PlacementRequestToProto(req *placement.PlacementRequest) *pb.PlaceWorkloadRequest {
	out := &pb.PlaceWorkloadRequest{
		WorkloadId:   req.WorkloadID,
		Group:        req.Group,
		Labels:       req.Labels,
		Resources:    req.Resources,
		Strategy:     string(req.Strategy),
		Affinity:     affinityTermsToProto(req.Affinity),
		AntiAffinity: affinityTermsToProto(req.AntiAffinity),
	}
	for _, c := range req.Constraints {
		out.Constraints = append(out.Constraints, &pb.PlacementConstraint{Key: c.Key, Operator: string(c.Operator), Values: c.Values})
	}
	if req.Spread != nil {
		out.Spread = &pb.PlacementSpreadConstraint{
			TopologyKey: req.Spread.TopologyKey,
			MaxSkew:     int32(req.Spread.MaxSkew),
			Required:    req.Spread.Required,
		}
	}
	return out
}

// affinityTermsToProto converts affinity terms to proto.
func affinityTermsToProto(in []placement.AffinityTerm) []*pb.PlacementAffinityTerm {
	if len(in) == 0 {
		return nil
	}
	out := make([]*pb.PlacementAffinityTerm, 0, len(in))
	for _, t := range in {
		out = append(out, &pb.PlacementAffinityTerm{Group: t.Group, Required: t.Required, Weight: int32(t.Weight)})
	}
	return out
}

// fromUnixNanos is the inverse of unixNanos: zero stays the zero time.
func fromUnixNanos(ns int64) time.Time {
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}
