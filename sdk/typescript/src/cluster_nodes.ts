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

// ---------------------------------------------------------------------------
// Cluster node membership domain types.
//
// Mirrors pkg/api/rest/handlers_cluster.go (/api/v1/cluster/nodes/*) and the
// request/response types in pkg/api/rest/types.go. ClusterNodesClient lists
// the Raft nodes of the cluster and adds, removes and replaces them; it is
// distinct from ClusterClient (client.cluster), which reports cluster status.
//
// Wire note: node and shard ids are numbers; timestamps are RFC 3339 strings.
//
// Routing note: the list and add routes carry a trailing slash
// (GET|POST /api/v1/cluster/nodes/); removing a node needs the shard_id query.
// ---------------------------------------------------------------------------

/** ClusterNodeInfo describes one node of the cluster. */
export interface ClusterNodeInfo {
  node_id: number;
  address: string;
  raft_address: string;
  is_leader: boolean;
  health_status: string;
  version: string;
  uptime_seconds: number;
}

/** ListClusterNodesResponse enumerates the cluster's nodes. */
export interface ListClusterNodesResponse {
  nodes: ClusterNodeInfo[];
  total_count: number;
  timestamp: string;
}

/** AddNodeRequest adds a node to a shard. */
export interface AddNodeRequest {
  shard_id: number;
  node_id: number;
  address: string;
  identity?: string;
}

/** AddNodeResponse confirms a node was added. */
export interface AddNodeResponse {
  success: boolean;
  message: string;
  shard_id: number;
  node_id: number;
  timestamp: string;
}

/** RemoveNodeResponse confirms a node was removed from a shard. */
export interface RemoveNodeResponse {
  success: boolean;
  message: string;
  shard_id: number;
  node_id: number;
  timestamp: string;
}

/** ReplaceNodeRequest names the node that replaces an existing one. */
export interface ReplaceNodeRequest {
  replacement_node_id: number;
  replacement_address: string;
  replacement_identity?: string;
}

/** ReplaceNodeResponse confirms a node replacement and the shards it touched. */
export interface ReplaceNodeResponse {
  success: boolean;
  message: string;
  affected_shards: number[];
  timestamp: string;
}

// ---------------------------------------------------------------------------
// ClusterNodesClient interface
// ---------------------------------------------------------------------------

/**
 * ClusterNodesClient manages cluster node membership. Maps to
 * /api/v1/cluster/nodes/* REST endpoints.
 */
export interface ClusterNodesClient {
  /** listNodes lists every node of the cluster. GET /api/v1/cluster/nodes/ */
  listNodes(): Promise<ListClusterNodesResponse>;

  /** addNode adds a node to a shard. POST /api/v1/cluster/nodes/ */
  addNode(req: AddNodeRequest): Promise<AddNodeResponse>;

  /** listNonLeaderNodes lists the nodes that lead no shard. GET /api/v1/cluster/nodes/non-leader */
  listNonLeaderNodes(): Promise<ListClusterNodesResponse>;

  /** removeNode removes a node from a shard. DELETE /api/v1/cluster/nodes/{nodeId}?shard_id= */
  removeNode(nodeId: number, shardId: number): Promise<RemoveNodeResponse>;

  /** replaceNode replaces a node with another. PUT /api/v1/cluster/nodes/{nodeId}/replace */
  replaceNode(nodeId: number, req: ReplaceNodeRequest): Promise<ReplaceNodeResponse>;
}
