/**
 * QuicBaaS JavaScript SDK - Cluster Client
 *
 * Provides cluster management operations for QuicBaaS including
 * status monitoring, node management, shard operations, and rebalancing.
 *
 * @module @quicbaas/client/clients/cluster
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  positiveInteger,
  nonEmptyArray,
} from '../utils/validation.js';

/**
 * Node state enumeration.
 * @readonly
 * @enum {string}
 */
export const NodeState = Object.freeze({
  /** Node state is unknown */
  UNKNOWN: 'UNKNOWN',
  /** Node is healthy and operational */
  HEALTHY: 'HEALTHY',
  /** Node is unhealthy or unresponsive */
  UNHEALTHY: 'UNHEALTHY',
  /** Node is in the process of leaving the cluster */
  LEAVING: 'LEAVING',
});

/**
 * Rebalance strategy enumeration.
 * @readonly
 * @enum {string}
 */
export const RebalanceStrategy = Object.freeze({
  /** Balance shards evenly across nodes */
  EVEN: 'EVEN',
  /** Balance based on data size */
  DATA_SIZE: 'DATA_SIZE',
  /** Balance based on request load */
  LOAD: 'LOAD',
});

/**
 * Rebalance operation status enumeration.
 * @readonly
 * @enum {string}
 */
export const RebalanceStatus = Object.freeze({
  /** Rebalance operation is pending */
  PENDING: 'PENDING',
  /** Rebalance operation is in progress */
  IN_PROGRESS: 'IN_PROGRESS',
  /** Rebalance operation completed successfully */
  COMPLETED: 'COMPLETED',
  /** Rebalance operation failed */
  FAILED: 'FAILED',
  /** Rebalance operation was cancelled */
  CANCELLED: 'CANCELLED',
});

/**
 * @typedef {Object} ClusterMember
 * @property {string} id - Unique identifier for the member
 * @property {string} name - Human-readable name
 * @property {string} address - Network address of the member
 * @property {boolean} isLeader - Whether this member is the cluster leader
 * @property {boolean} healthy - Whether the member is healthy
 */

/**
 * @typedef {Object} ClusterStatusResponse
 * @property {string} clusterId - Unique identifier for the cluster
 * @property {string} leaderId - ID of the current leader node
 * @property {ClusterMember[]} members - List of cluster members
 * @property {boolean} healthy - Overall cluster health status
 */

/**
 * @typedef {Object} NodeInfo
 * @property {string} id - Unique identifier for the node
 * @property {string} name - Human-readable name
 * @property {string} address - Network address of the node
 * @property {NodeState} state - Current state of the node
 */

/**
 * @typedef {Object} ShardInfo
 * @property {number} shardId - Unique identifier for the shard
 * @property {string} leaderId - ID of the shard leader node
 * @property {string[]} replicas - List of replica node IDs
 * @property {number} size - Size of the shard data in bytes
 */

/**
 * @typedef {Object} LeaderResponse
 * @property {number} shardId - The shard ID
 * @property {string} leaderId - ID of the leader node
 * @property {string} leaderAddress - Network address of the leader
 */

/**
 * @typedef {Object} AddNodeRequest
 * @property {number} shardId - Shard to add the node to
 * @property {number} nodeId - Unique identifier for the new node
 * @property {string} address - Network address of the new node
 */

/**
 * @typedef {Object} RemoveNodeRequest
 * @property {number} shardId - Shard to remove the node from
 * @property {number} nodeId - ID of the node to remove
 */

/**
 * @typedef {Object} CreateShardRequest
 * @property {number} shardId - Unique identifier for the new shard
 * @property {Array<{nodeId: number, address: string}>} replicas - Initial replica configuration
 */

/**
 * @typedef {Object} CreateShardResponse
 * @property {number} shardId - ID of the created shard
 * @property {boolean} created - Whether the shard was created
 */

/**
 * @typedef {Object} RebalanceOptions
 * @property {RebalanceStrategy} [strategy] - Rebalancing strategy to use
 * @property {number} [maxConcurrentMoves] - Maximum concurrent shard moves
 * @property {boolean} [dryRun] - If true, simulate without making changes
 */

/**
 * @typedef {Object} RebalanceResponse
 * @property {string} operationId - Unique identifier for the rebalance operation
 * @property {RebalanceStatus} status - Current status of the operation
 * @property {string} startedAt - ISO timestamp when operation started
 */

/**
 * @typedef {Object} RebalanceProgressResponse
 * @property {string} operationId - Unique identifier for the operation
 * @property {RebalanceStatus} status - Current status
 * @property {number} progress - Progress percentage (0-100)
 * @property {number} shardsMovedCount - Number of shards moved
 * @property {number} totalShardsToMove - Total shards to move
 * @property {string} [startedAt] - ISO timestamp when started
 * @property {string} [completedAt] - ISO timestamp when completed
 * @property {string} [error] - Error message if failed
 */

/**
 * Client for QuicBaaS cluster management operations.
 *
 * Provides operations for monitoring cluster status, managing nodes,
 * creating shards, and performing cluster rebalancing.
 *
 * @example
 * const transport = new HttpTransport('http://localhost:8080');
 * const clusterClient = new ClusterClient(transport);
 *
 * // Get cluster status
 * const status = await clusterClient.clusterStatus();
 * console.log('Cluster healthy:', status.healthy);
 *
 * // Get leader for a shard
 * const leader = await clusterClient.getLeader(1);
 * console.log('Shard 1 leader:', leader.leaderId);
 */
class ClusterClient {
  /**
   * API endpoint prefix for cluster operations.
   * @private
   * @type {string}
   */
  static API_PREFIX = '/api/v1/cluster';

  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new ClusterClient.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /**
   * Get the current cluster status including node and shard information.
   *
   * Returns comprehensive information about the cluster's health,
   * member nodes, and leadership status.
   *
   * @returns {Promise<ClusterStatusResponse>} Cluster status information
   *
   * @example
   * const status = await clusterClient.clusterStatus();
   * console.log('Cluster ID:', status.clusterId);
   * console.log('Leader ID:', status.leaderId);
   * console.log('Members:', status.members.length);
   * console.log('Healthy:', status.healthy);
   */
  async clusterStatus() {
    const path = `${ClusterClient.API_PREFIX}/status`;
    const response = await this.#transport.get(path);

    return this.#parseClusterStatus(response);
  }

  /**
   * Get the leader node for a specific shard.
   *
   * Returns the current leader information for the specified Raft shard,
   * including the leader's node ID and network address.
   *
   * @param {number} shardId - The shard ID to query
   * @returns {Promise<LeaderResponse>} Leader information for the shard
   * @throws {ValidationError} If shardId is invalid
   *
   * @example
   * const leader = await clusterClient.getLeader(1);
   * console.log('Leader node:', leader.leaderId);
   * console.log('Leader address:', leader.leaderAddress);
   */
  async getLeader(shardId) {
    this.#validateShardId(shardId, 'shardId');

    const path = `${ClusterClient.API_PREFIX}/shards/${shardId}/leader`;
    const response = await this.#transport.get(path);

    return {
      shardId: response?.shardId ?? shardId,
      leaderId: response?.leaderId ?? '',
      leaderAddress: response?.leaderAddress ?? '',
    };
  }

  /**
   * Add a node to a specific shard.
   *
   * Adds a new replica node to the specified Raft shard. The node
   * will begin syncing data and participate in consensus once added.
   *
   * @param {number} shardId - The shard to add the node to
   * @param {number} nodeId - Unique identifier for the new node
   * @param {string} address - Network address of the new node (host:port)
   * @returns {Promise<void>} Resolves when node is added
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * await clusterClient.addNode(1, 4, '192.168.1.104:6001');
   * console.log('Node added to shard 1');
   */
  async addNode(shardId, nodeId, address) {
    this.#validateShardId(shardId, 'shardId');
    this.#validateNodeId(nodeId, 'nodeId');
    requiredString(address, 'address');
    this.#validateAddress(address);

    const path = `${ClusterClient.API_PREFIX}/shards/${shardId}/nodes`;
    const body = {
      nodeId,
      address,
    };

    await this.#transport.post(path, body);
  }

  /**
   * Remove a node from a specific shard.
   *
   * Removes a replica node from the specified Raft shard. The node
   * will be removed from consensus and stop receiving updates.
   *
   * @param {number} shardId - The shard to remove the node from
   * @param {number} nodeId - ID of the node to remove
   * @returns {Promise<void>} Resolves when node is removed
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * await clusterClient.removeNode(1, 4);
   * console.log('Node 4 removed from shard 1');
   */
  async removeNode(shardId, nodeId) {
    this.#validateShardId(shardId, 'shardId');
    this.#validateNodeId(nodeId, 'nodeId');

    const path = `${ClusterClient.API_PREFIX}/shards/${shardId}/nodes/${nodeId}`;
    await this.#transport.delete(path);
  }

  /**
   * Create a new Raft shard with initial replicas.
   *
   * Creates a new Raft consensus group with the specified shard ID
   * and initial replica configuration. The shard will elect a leader
   * and begin accepting operations once created.
   *
   * @param {number} shardId - Unique identifier for the new shard
   * @param {Array<{nodeId: number, address: string}>} replicas - Initial replica nodes
   * @returns {Promise<CreateShardResponse>} Shard creation result
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * const result = await clusterClient.createShard(2, [
   *   { nodeId: 1, address: '192.168.1.101:6001' },
   *   { nodeId: 2, address: '192.168.1.102:6001' },
   *   { nodeId: 3, address: '192.168.1.103:6001' }
   * ]);
   * console.log('Shard created:', result.created);
   */
  async createShard(shardId, replicas) {
    this.#validateShardId(shardId, 'shardId');
    nonEmptyArray(replicas, 'replicas');
    this.#validateReplicas(replicas);

    const path = `${ClusterClient.API_PREFIX}/shards`;
    const body = {
      shardId,
      replicas: replicas.map((replica) => ({
        nodeId: replica.nodeId,
        address: replica.address,
      })),
    };

    const response = await this.#transport.post(path, body);

    return {
      shardId: response?.shardId ?? shardId,
      created: response?.created ?? true,
    };
  }

  /**
   * Start a cluster rebalancing operation.
   *
   * Initiates a rebalancing process to redistribute shards across
   * nodes according to the specified strategy. Returns an operation
   * ID that can be used to track progress.
   *
   * @param {RebalanceOptions} [options={}] - Rebalancing options
   * @param {RebalanceStrategy} [options.strategy] - Strategy to use for rebalancing
   * @param {number} [options.maxConcurrentMoves] - Maximum concurrent shard moves
   * @param {boolean} [options.dryRun] - If true, simulate without making changes
   * @returns {Promise<RebalanceResponse>} Rebalance operation information
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * const result = await clusterClient.rebalanceCluster({
   *   strategy: RebalanceStrategy.EVEN,
   *   maxConcurrentMoves: 2,
   *   dryRun: false
   * });
   * console.log('Rebalance operation ID:', result.operationId);
   */
  async rebalanceCluster(options = {}) {
    this.#validateRebalanceOptions(options);

    const path = `${ClusterClient.API_PREFIX}/rebalance`;
    const body = {};

    if (options.strategy !== undefined) {
      body.strategy = options.strategy;
    }
    if (options.maxConcurrentMoves !== undefined) {
      body.maxConcurrentMoves = options.maxConcurrentMoves;
    }
    if (options.dryRun !== undefined) {
      body.dryRun = options.dryRun;
    }

    const response = await this.#transport.post(path, body);

    return {
      operationId: response?.operationId ?? '',
      status: response?.status ?? RebalanceStatus.PENDING,
      startedAt: response?.startedAt ?? new Date().toISOString(),
    };
  }

  /**
   * Get the progress of a rebalance operation.
   *
   * Returns the current status and progress of an ongoing or
   * completed rebalance operation.
   *
   * @param {string} operationId - The rebalance operation ID
   * @returns {Promise<RebalanceProgressResponse>} Rebalance progress information
   * @throws {ValidationError} If operationId is invalid
   *
   * @example
   * const progress = await clusterClient.getRebalanceProgress('op-123');
   * console.log('Status:', progress.status);
   * console.log('Progress:', progress.progress, '%');
   * console.log('Shards moved:', progress.shardsMovedCount, '/', progress.totalShardsToMove);
   */
  async getRebalanceProgress(operationId) {
    requiredString(operationId, 'operationId');

    const path = `${ClusterClient.API_PREFIX}/rebalance/${encodeURIComponent(operationId)}`;
    const response = await this.#transport.get(path);

    return {
      operationId: response?.operationId ?? operationId,
      status: response?.status ?? RebalanceStatus.PENDING,
      progress: response?.progress ?? 0,
      shardsMovedCount: response?.shardsMovedCount ?? 0,
      totalShardsToMove: response?.totalShardsToMove ?? 0,
      startedAt: response?.startedAt ?? null,
      completedAt: response?.completedAt ?? null,
      error: response?.error ?? null,
    };
  }

  /**
   * Get information about all nodes in the cluster.
   *
   * Returns a list of all nodes with their current state and metadata.
   *
   * @returns {Promise<{nodes: NodeInfo[]}>} List of cluster nodes
   *
   * @example
   * const response = await clusterClient.getNodes();
   * for (const node of response.nodes) {
   *   console.log(`Node ${node.id}: ${node.state}`);
   * }
   */
  async getNodes() {
    const path = `${ClusterClient.API_PREFIX}/nodes`;
    const response = await this.#transport.get(path);

    return {
      nodes: (response?.nodes ?? []).map((node) => this.#parseNodeInfo(node)),
    };
  }

  /**
   * Get information about all shards in the cluster.
   *
   * Returns a list of all shards with their leader, replicas, and size.
   *
   * @returns {Promise<{shards: ShardInfo[]}>} List of cluster shards
   *
   * @example
   * const response = await clusterClient.getShards();
   * for (const shard of response.shards) {
   *   console.log(`Shard ${shard.shardId}: leader=${shard.leaderId}`);
   * }
   */
  async getShards() {
    const path = `${ClusterClient.API_PREFIX}/shards`;
    const response = await this.#transport.get(path);

    return {
      shards: (response?.shards ?? []).map((shard) => ({
        shardId: shard.shardId ?? 0,
        leaderId: shard.leaderId ?? '',
        replicas: shard.replicas ?? [],
        size: shard.size ?? 0,
      })),
    };
  }

  /**
   * Parse cluster status response.
   * @private
   * @param {Object} response - Raw API response
   * @returns {ClusterStatusResponse} Parsed cluster status
   */
  #parseClusterStatus(response) {
    if (!response) {
      return {
        clusterId: '',
        leaderId: '',
        members: [],
        healthy: false,
      };
    }

    return {
      clusterId: response.clusterId ?? '',
      leaderId: response.leaderId ?? '',
      members: (response.members ?? []).map((member) => ({
        id: member.id ?? '',
        name: member.name ?? '',
        address: member.address ?? '',
        isLeader: Boolean(member.isLeader),
        healthy: Boolean(member.healthy),
      })),
      healthy: Boolean(response.healthy),
    };
  }

  /**
   * Parse node info from response.
   * @private
   * @param {Object} node - Raw node data
   * @returns {NodeInfo} Parsed node info
   */
  #parseNodeInfo(node) {
    return {
      id: node.id ?? '',
      name: node.name ?? '',
      address: node.address ?? '',
      state: this.#parseNodeState(node.state),
    };
  }

  /**
   * Parse node state string to enum value.
   * @private
   * @param {string|number} state - Raw state value
   * @returns {NodeState} Parsed node state
   */
  #parseNodeState(state) {
    if (typeof state === 'number') {
      const stateMap = {
        0: NodeState.UNKNOWN,
        1: NodeState.HEALTHY,
        2: NodeState.UNHEALTHY,
        3: NodeState.LEAVING,
      };
      return stateMap[state] ?? NodeState.UNKNOWN;
    }

    if (typeof state === 'string') {
      const normalized = state.toUpperCase();
      if (Object.values(NodeState).includes(normalized)) {
        return normalized;
      }
    }

    return NodeState.UNKNOWN;
  }

  /**
   * Validate shard ID parameter.
   * @private
   * @param {number} shardId - Shard ID to validate
   * @param {string} name - Parameter name for error message
   * @throws {ValidationError} If validation fails
   */
  #validateShardId(shardId, name) {
    required(shardId, name);
    if (typeof shardId !== 'number' || !Number.isInteger(shardId) || shardId < 0) {
      throw new ValidationError(`${name} must be a non-negative integer`, name);
    }
  }

  /**
   * Validate node ID parameter.
   * @private
   * @param {number} nodeId - Node ID to validate
   * @param {string} name - Parameter name for error message
   * @throws {ValidationError} If validation fails
   */
  #validateNodeId(nodeId, name) {
    required(nodeId, name);
    if (typeof nodeId !== 'number' || !Number.isInteger(nodeId) || nodeId <= 0) {
      throw new ValidationError(`${name} must be a positive integer`, name);
    }
  }

  /**
   * Validate network address format.
   * @private
   * @param {string} address - Address to validate
   * @throws {ValidationError} If validation fails
   */
  #validateAddress(address) {
    if (!address.includes(':')) {
      throw new ValidationError('address must be in host:port format', 'address');
    }

    const parts = address.split(':');
    if (parts.length !== 2) {
      throw new ValidationError('address must be in host:port format', 'address');
    }

    const [host, portStr] = parts;
    if (!host || host.trim() === '') {
      throw new ValidationError('address host cannot be empty', 'address');
    }

    const port = parseInt(portStr, 10);
    if (isNaN(port) || port < 1 || port > 65535) {
      throw new ValidationError('address port must be between 1 and 65535', 'address');
    }
  }

  /**
   * Validate replicas array.
   * @private
   * @param {Array<{nodeId: number, address: string}>} replicas - Replicas to validate
   * @throws {ValidationError} If validation fails
   */
  #validateReplicas(replicas) {
    const seenNodeIds = new Set();
    const seenAddresses = new Set();

    replicas.forEach((replica, index) => {
      if (!replica || typeof replica !== 'object') {
        throw new ValidationError(
          `replica at index ${index} must be an object`,
          `replicas[${index}]`,
        );
      }

      this.#validateNodeId(replica.nodeId, `replicas[${index}].nodeId`);

      if (!replica.address || typeof replica.address !== 'string' || replica.address.trim() === '') {
        throw new ValidationError(
          `replica at index ${index} requires an address`,
          `replicas[${index}].address`,
        );
      }

      this.#validateAddress(replica.address);

      if (seenNodeIds.has(replica.nodeId)) {
        throw new ValidationError(
          `duplicate nodeId ${replica.nodeId} in replicas`,
          `replicas[${index}].nodeId`,
        );
      }
      seenNodeIds.add(replica.nodeId);

      if (seenAddresses.has(replica.address)) {
        throw new ValidationError(
          `duplicate address ${replica.address} in replicas`,
          `replicas[${index}].address`,
        );
      }
      seenAddresses.add(replica.address);
    });
  }

  /**
   * Validate rebalance options.
   * @private
   * @param {RebalanceOptions} options - Options to validate
   * @throws {ValidationError} If validation fails
   */
  #validateRebalanceOptions(options) {
    if (options.strategy !== undefined) {
      const validStrategies = Object.values(RebalanceStrategy);
      if (!validStrategies.includes(options.strategy)) {
        throw new ValidationError(
          `strategy must be one of: ${validStrategies.join(', ')}`,
          'options.strategy',
        );
      }
    }

    if (options.maxConcurrentMoves !== undefined) {
      positiveInteger(options.maxConcurrentMoves, 'options.maxConcurrentMoves');
    }

    if (options.dryRun !== undefined && typeof options.dryRun !== 'boolean') {
      throw new ValidationError('dryRun must be a boolean', 'options.dryRun');
    }
  }
}

export default ClusterClient;
export { ClusterClient };
