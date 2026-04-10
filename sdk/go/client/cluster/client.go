// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package cluster

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Cluster"
)

// Client provides cluster management operations.
type Client interface {
	// Status returns the current cluster status.
	Status(ctx context.Context) (*ClusterStatus, error)

	// Nodes returns information about all nodes in the cluster.
	Nodes(ctx context.Context) (*NodesResponse, error)

	// AddNode adds a new node to the cluster.
	AddNode(ctx context.Context, req *AddNodeRequest) error

	// RemoveNode removes a node from the cluster.
	RemoveNode(ctx context.Context, req *RemoveNodeRequest) error

	// RebalanceCluster initiates cluster rebalancing to redistribute data across shards.
	RebalanceCluster(ctx context.Context, req *RebalanceClusterRequest) (*RebalanceClusterResponse, error)

	// GetRebalanceProgress returns the status of an ongoing rebalance operation.
	GetRebalanceProgress(ctx context.Context, req *GetRebalanceProgressRequest) (*GetRebalanceProgressResponse, error)
}

// ClusterStatus represents the cluster status.
type ClusterStatus struct {
	ClusterID string
	LeaderID  string
	Members   []ClusterMember
	Healthy   bool
	Shards    []ClusterShard
}

// ClusterMember represents a cluster member.
type ClusterMember struct {
	ID       string
	Name     string
	Address  string
	IsLeader bool
	Healthy  bool
}

// ClusterShard represents per-shard status information.
type ClusterShard struct {
	ShardID  uint64
	LeaderID uint64
	Replicas []uint64
}

// NodesResponse is the response from listing nodes.
type NodesResponse struct {
	Nodes []NodeInfo
}

// NodeInfo contains information about a node.
type NodeInfo struct {
	ID      string
	Name    string
	Address string
	State   NodeState
}

// NodeState represents the state of a node.
type NodeState int

const (
	NodeStateUnknown NodeState = iota
	NodeStateHealthy
	NodeStateUnhealthy
	NodeStateLeaving
)

// String returns the string representation of the node state.
func (s NodeState) String() string {
	switch s {
	case NodeStateHealthy:
		return "HEALTHY"
	case NodeStateUnhealthy:
		return "UNHEALTHY"
	case NodeStateLeaving:
		return "LEAVING"
	default:
		return "UNKNOWN"
	}
}

// AddNodeRequest is a request to add a node.
type AddNodeRequest struct {
	Address string
	Name    string
}

// RemoveNodeRequest is a request to remove a node.
type RemoveNodeRequest struct {
	NodeID string
}

// RebalanceClusterRequest is a request to start cluster rebalancing.
type RebalanceClusterRequest struct {
	Strategy string
}

// RebalanceClusterResponse is the response from starting a rebalance operation.
type RebalanceClusterResponse struct {
	OperationID string
	Started     int64
}

// GetRebalanceProgressRequest is a request to check rebalance progress.
type GetRebalanceProgressRequest struct {
	OperationID string
}

// GetRebalanceProgressResponse is the response for rebalance progress.
type GetRebalanceProgressResponse struct {
	OperationID     string
	Status          string
	PercentComplete float64
	ShardsMoved     int32
	TotalShards     int32
	ElapsedTime     int64
}

// statusRequest is the internal status request.
type statusRequest struct{}

// nodesRequest is the internal nodes request.
type nodesRequest struct{}

// addNodeResponse is the internal add node response.
type addNodeResponse struct{}

// removeNodeResponse is the internal remove node response.
type removeNodeResponse struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new cluster client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Status returns the current cluster status.
func (c *client) Status(ctx context.Context) (*ClusterStatus, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("cluster.Status", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Status", &statusRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("cluster.Status", "nil response from server")
	}

	status, ok := resp.(*ClusterStatus)
	if !ok {
		return nil, errors.NewInternal("cluster.Status", "invalid response type")
	}

	return status, nil
}

// Nodes returns information about all nodes in the cluster.
func (c *client) Nodes(ctx context.Context) (*NodesResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("cluster.Nodes", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Nodes", &nodesRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return &NodesResponse{Nodes: []NodeInfo{}}, nil
	}

	nodes, ok := resp.(*NodesResponse)
	if !ok {
		return nil, errors.NewInternal("cluster.Nodes", "invalid response type")
	}

	return nodes, nil
}

// AddNode adds a new node to the cluster.
func (c *client) AddNode(ctx context.Context, req *AddNodeRequest) error {
	if err := validateAddNodeRequest(req); err != nil {
		return err
	}

	if ctx == nil {
		return errors.NewInvalidInput("cluster.AddNode", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "AddNode", req)
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*addNodeResponse); !ok {
		return errors.NewInternal("cluster.AddNode", "invalid response type")
	}

	return nil
}

// RemoveNode removes a node from the cluster.
func (c *client) RemoveNode(ctx context.Context, req *RemoveNodeRequest) error {
	if err := validateRemoveNodeRequest(req); err != nil {
		return err
	}

	if ctx == nil {
		return errors.NewInvalidInput("cluster.RemoveNode", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "RemoveNode", req)
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*removeNodeResponse); !ok {
		return errors.NewInternal("cluster.RemoveNode", "invalid response type")
	}

	return nil
}

// RebalanceCluster initiates cluster rebalancing.
func (c *client) RebalanceCluster(ctx context.Context, req *RebalanceClusterRequest) (*RebalanceClusterResponse, error) {
	if err := validateRebalanceClusterRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("cluster.RebalanceCluster", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Rebalance", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("cluster.RebalanceCluster", "nil response from server")
	}

	rebalanceResp, ok := resp.(*RebalanceClusterResponse)
	if !ok {
		return nil, errors.NewInternal("cluster.RebalanceCluster", "invalid response type")
	}

	return rebalanceResp, nil
}

// GetRebalanceProgress returns the status of an ongoing rebalance operation.
func (c *client) GetRebalanceProgress(ctx context.Context, req *GetRebalanceProgressRequest) (*GetRebalanceProgressResponse, error) {
	if err := validateGetRebalanceProgressRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("cluster.GetRebalanceProgress", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetRebalanceStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("cluster.GetRebalanceProgress", "nil response from server")
	}

	progressResp, ok := resp.(*GetRebalanceProgressResponse)
	if !ok {
		return nil, errors.NewInternal("cluster.GetRebalanceProgress", "invalid response type")
	}

	return progressResp, nil
}

// Validation functions

func validateAddNodeRequest(req *AddNodeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("cluster.AddNode", "request is required")
	}
	if req.Address == "" {
		return errors.NewInvalidInput("cluster.AddNode", "address is required")
	}
	return nil
}

func validateRemoveNodeRequest(req *RemoveNodeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("cluster.RemoveNode", "request is required")
	}
	if req.NodeID == "" {
		return errors.NewInvalidInput("cluster.RemoveNode", "node ID is required")
	}
	return nil
}

func validateRebalanceClusterRequest(req *RebalanceClusterRequest) error {
	if req == nil {
		return errors.NewInvalidInput("cluster.RebalanceCluster", "request is required")
	}
	return nil
}

func validateGetRebalanceProgressRequest(req *GetRebalanceProgressRequest) error {
	if req == nil {
		return errors.NewInvalidInput("cluster.GetRebalanceProgress", "request is required")
	}
	if req.OperationID == "" {
		return errors.NewInvalidInput("cluster.GetRebalanceProgress", "operation ID is required")
	}
	return nil
}
