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

package scheduler

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Workload placement types. They are the server engine's own types
// (pkg/scheduler/placement), aliased so a value crosses every transport
// without conversion and a field added to the engine reaches the SDK in the
// same change.
type (
	// NodeFacts are a node's caller-supplied facts: abstract resource
	// capacities and opaque labels.
	NodeFacts = placement.NodeFacts

	// NodeStatus is a node's operator-controlled cordon and drain state.
	NodeStatus = placement.NodeStatus

	// NodeInfo is a live node: facts, status, lease and allocation.
	NodeInfo = placement.NodeInfo

	// Operator is a label-constraint operator.
	Operator = placement.Operator

	// Constraint is a hard requirement on a node label.
	Constraint = placement.Constraint

	// AffinityTerm relates a workload to nodes hosting another group.
	AffinityTerm = placement.AffinityTerm

	// SpreadConstraint spreads a group across a topology label.
	SpreadConstraint = placement.SpreadConstraint

	// Strategy ranks feasible nodes.
	Strategy = placement.Strategy

	// PlacementRequest asks for one workload to be placed.
	PlacementRequest = placement.PlacementRequest

	// Placement is a recorded placement decision.
	Placement = placement.Placement

	// PlaceResult is the outcome of Place.
	PlaceResult = placement.PlaceResult

	// PlacementFilter narrows ListPlacements.
	PlacementFilter = placement.PlacementFilter
)

// Constraint operators and strategies.
const (
	OperatorIn           = placement.OperatorIn
	OperatorNotIn        = placement.OperatorNotIn
	OperatorExists       = placement.OperatorExists
	OperatorDoesNotExist = placement.OperatorDoesNotExist

	StrategyBinPack = placement.StrategyBinPack
	StrategySpread  = placement.StrategySpread
)

// RegisterNodeRequest registers a node's facts under a lease.
type RegisterNodeRequest struct {
	// Facts are the node's caller-supplied facts.
	Facts NodeFacts `json:"facts"`

	// TTLSeconds is the lease time-to-live. Heartbeat with RenewNode
	// within it, or the node drops out of placement.
	TTLSeconds int64 `json:"ttl_seconds"`
}

// NodeRequest is the transport request of a call keyed by a node ID.
type NodeRequest struct {
	NodeID string `json:"node_id"`
}

// WorkloadRequest is the transport request of a call keyed by a workload ID.
type WorkloadRequest struct {
	WorkloadID string `json:"workload_id"`
}

// ListNodesRequest is the transport request of ListNodes.
type ListNodesRequest struct{}

// RenewNodeResponse is the transport response of RenewNode.
type RenewNodeResponse struct {
	NodeID  string `json:"node_id"`
	Renewed bool   `json:"renewed"`
}

// DeregisterNodeResponse is the transport response of DeregisterNode.
type DeregisterNodeResponse struct {
	NodeID       string `json:"node_id"`
	Deregistered bool   `json:"deregistered"`
}

// ListNodesResponse lists the live nodes.
type ListNodesResponse struct {
	Nodes []*NodeInfo `json:"nodes"`
}

// DrainNodeResponse lists the placements on a drained node, which the
// caller moves: stop each workload, Release it, and Place it again.
type DrainNodeResponse struct {
	NodeID     string       `json:"node_id"`
	Placements []*Placement `json:"placements"`
}

// ReleaseResponse is the transport response of Release.
type ReleaseResponse struct {
	WorkloadID string `json:"workload_id"`
	Released   bool   `json:"released"`
}

// ListPlacementsResponse lists placements.
type ListPlacementsResponse struct {
	Placements []*Placement `json:"placements"`
}

// placementCall sends one placement request and asserts the response type.
func placementCall[T any](ctx context.Context, c *client, method string, req any) (*T, error) {
	op := "scheduler." + method
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, method, req)
	if err != nil {
		return nil, err
	}
	typed, ok := resp.(*T)
	if !ok || typed == nil {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return typed, nil
}

// requireID rejects an empty identifier before any transport call.
func requireID(op, field, id string) error {
	if id == "" {
		return errors.NewInvalidInput("scheduler."+op, field+" is required")
	}
	return nil
}

// RegisterNode registers or refreshes a node's facts under a lease.
func (c *client) RegisterNode(ctx context.Context, req *RegisterNodeRequest) (*NodeInfo, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("scheduler.RegisterNode", "request is required")
	}
	if err := requireID("RegisterNode", "node ID", req.Facts.NodeID); err != nil {
		return nil, err
	}
	if req.TTLSeconds < 1 {
		return nil, errors.NewInvalidInput("scheduler.RegisterNode", "ttl_seconds must be at least 1")
	}
	return placementCall[NodeInfo](ctx, c, "RegisterNode", req)
}

// RenewNode heartbeats a node's lease.
func (c *client) RenewNode(ctx context.Context, nodeID string) error {
	if err := requireID("RenewNode", "node ID", nodeID); err != nil {
		return err
	}
	_, err := placementCall[RenewNodeResponse](ctx, c, "RenewNode", &NodeRequest{NodeID: nodeID})
	return err
}

// DeregisterNode revokes a node's lease and clears its status.
func (c *client) DeregisterNode(ctx context.Context, nodeID string) error {
	if err := requireID("DeregisterNode", "node ID", nodeID); err != nil {
		return err
	}
	_, err := placementCall[DeregisterNodeResponse](ctx, c, "DeregisterNode", &NodeRequest{NodeID: nodeID})
	return err
}

// GetNode returns a live node.
func (c *client) GetNode(ctx context.Context, nodeID string) (*NodeInfo, error) {
	if err := requireID("GetNode", "node ID", nodeID); err != nil {
		return nil, err
	}
	return placementCall[NodeInfo](ctx, c, "GetNode", &NodeRequest{NodeID: nodeID})
}

// ListNodes returns every live node.
func (c *client) ListNodes(ctx context.Context) ([]*NodeInfo, error) {
	resp, err := placementCall[ListNodesResponse](ctx, c, "ListNodes", &ListNodesRequest{})
	if err != nil {
		return nil, err
	}
	return resp.Nodes, nil
}

// CordonNode excludes a node from new placements.
func (c *client) CordonNode(ctx context.Context, nodeID string) (*NodeInfo, error) {
	if err := requireID("CordonNode", "node ID", nodeID); err != nil {
		return nil, err
	}
	return placementCall[NodeInfo](ctx, c, "CordonNode", &NodeRequest{NodeID: nodeID})
}

// UncordonNode returns a node to service, clearing cordon and drain.
func (c *client) UncordonNode(ctx context.Context, nodeID string) (*NodeInfo, error) {
	if err := requireID("UncordonNode", "node ID", nodeID); err != nil {
		return nil, err
	}
	return placementCall[NodeInfo](ctx, c, "UncordonNode", &NodeRequest{NodeID: nodeID})
}

// DrainNode marks a node draining and returns the placements to move.
func (c *client) DrainNode(ctx context.Context, nodeID string) ([]*Placement, error) {
	if err := requireID("DrainNode", "node ID", nodeID); err != nil {
		return nil, err
	}
	resp, err := placementCall[DrainNodeResponse](ctx, c, "DrainNode", &NodeRequest{NodeID: nodeID})
	if err != nil {
		return nil, err
	}
	return resp.Placements, nil
}

// Place decides and records the node a workload runs on.
func (c *client) Place(ctx context.Context, req *PlacementRequest) (*PlaceResult, error) {
	if req == nil {
		return nil, errors.NewInvalidInput("scheduler.Place", "request is required")
	}
	if err := requireID("Place", "workload ID", req.WorkloadID); err != nil {
		return nil, err
	}
	return placementCall[PlaceResult](ctx, c, "Place", req)
}

// Release removes a workload's placement.
func (c *client) Release(ctx context.Context, workloadID string) error {
	if err := requireID("Release", "workload ID", workloadID); err != nil {
		return err
	}
	_, err := placementCall[ReleaseResponse](ctx, c, "Release", &WorkloadRequest{WorkloadID: workloadID})
	return err
}

// GetPlacement returns a workload's placement.
func (c *client) GetPlacement(ctx context.Context, workloadID string) (*Placement, error) {
	if err := requireID("GetPlacement", "workload ID", workloadID); err != nil {
		return nil, err
	}
	return placementCall[Placement](ctx, c, "GetPlacement", &WorkloadRequest{WorkloadID: workloadID})
}

// ListPlacements returns the placements matching filter (nil for all).
func (c *client) ListPlacements(ctx context.Context, filter *PlacementFilter) ([]*Placement, error) {
	if filter == nil {
		filter = &PlacementFilter{}
	}
	resp, err := placementCall[ListPlacementsResponse](ctx, c, "ListPlacements", filter)
	if err != nil {
		return nil, err
	}
	return resp.Placements, nil
}
