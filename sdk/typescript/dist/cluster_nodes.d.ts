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
//# sourceMappingURL=cluster_nodes.d.ts.map