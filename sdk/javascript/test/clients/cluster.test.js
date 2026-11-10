/**
 * ClusterClient Tests
 *
 * Comprehensive tests for the QuicBaaS cluster management client.
 */

import {
  ClusterClient,
  NodeState,
  RebalanceStrategy,
  RebalanceStatus,
} from '../../src/clients/cluster.js';
import { ValidationError } from '../../src/errors/index.js';

describe('ClusterClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
    };
    client = new ClusterClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const c = new ClusterClient(mockTransport);
      expect(c).toBeInstanceOf(ClusterClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new ClusterClient()).toThrow(ValidationError);
      expect(() => new ClusterClient(null)).toThrow(ValidationError);
      expect(() => new ClusterClient(undefined)).toThrow(ValidationError);
    });
  });

  describe('clusterStatus', () => {
    it('should return cluster status with healthy cluster', async () => {
      const mockResponse = {
        clusterId: 'cluster-1',
        leaderId: 'node-1',
        members: [
          { id: 'node-1', name: 'Node 1', address: '192.0.2.1:5000', isLeader: true, healthy: true },
          { id: 'node-2', name: 'Node 2', address: '192.0.2.2:5000', isLeader: false, healthy: true },
          { id: 'node-3', name: 'Node 3', address: '192.0.2.3:5000', isLeader: false, healthy: true },
        ],
        healthy: true,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.clusterStatus();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster/status');
      expect(result.clusterId).toBe('cluster-1');
      expect(result.leaderId).toBe('node-1');
      expect(result.members).toHaveLength(3);
      expect(result.healthy).toBe(true);
      expect(result.members[0].isLeader).toBe(true);
      expect(result.members[0].healthy).toBe(true);
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.clusterStatus();

      expect(result).toEqual({
        clusterId: '',
        leaderId: '',
        members: [],
        healthy: false,
      });
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.clusterStatus();

      expect(result.clusterId).toBe('');
      expect(result.leaderId).toBe('');
      expect(result.members).toEqual([]);
      expect(result.healthy).toBe(false);
    });

    it('should handle partial member data', async () => {
      mockTransport.get.mockResolvedValueOnce({
        members: [{ id: 'node-1' }],
      });

      const result = await client.clusterStatus();

      expect(result.members[0]).toEqual({
        id: 'node-1',
        name: '',
        address: '',
        isLeader: false,
        healthy: false,
      });
    });

    it('should handle member with explicit null fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        members: [{ id: null, name: null, address: null, isLeader: null, healthy: null }],
      });

      const result = await client.clusterStatus();

      expect(result.members[0]).toEqual({
        id: '',
        name: '',
        address: '',
        isLeader: false,
        healthy: false,
      });
    });
  });

  describe('getLeader', () => {
    it('should return leader information for valid shard', async () => {
      const mockResponse = {
        shardId: 1,
        leaderId: 'node-1',
        leaderAddress: '192.0.2.1:5000',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getLeader(1);

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster/shards/1/leader');
      expect(result.shardId).toBe(1);
      expect(result.leaderId).toBe('node-1');
      expect(result.leaderAddress).toBe('192.0.2.1:5000');
    });

    it('should throw ValidationError when shardId is missing', async () => {
      await expect(client.getLeader()).rejects.toThrow(ValidationError);
      await expect(client.getLeader(null)).rejects.toThrow(ValidationError);
      await expect(client.getLeader(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is negative', async () => {
      await expect(client.getLeader(-1)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is not an integer', async () => {
      await expect(client.getLeader(1.5)).rejects.toThrow(ValidationError);
      await expect(client.getLeader('1')).rejects.toThrow(ValidationError);
    });

    it('should accept shardId of 0', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getLeader(0);

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster/shards/0/leader');
    });

    it('should handle null response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getLeader(1);

      expect(result.shardId).toBe(1);
      expect(result.leaderId).toBe('');
      expect(result.leaderAddress).toBe('');
    });
  });

  describe('addNode', () => {
    it('should add node to shard successfully', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      await client.addNode(1, 4, '192.0.2.4:5000');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster/shards/1/nodes',
        { nodeId: 4, address: '192.0.2.4:5000' },
      );
    });

    it('should throw ValidationError when shardId is missing', async () => {
      await expect(client.addNode(undefined, 4, '192.0.2.4:5000')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is negative', async () => {
      await expect(client.addNode(-1, 4, '192.0.2.4:5000')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is missing', async () => {
      await expect(client.addNode(1, undefined, '192.0.2.4:5000')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is not positive', async () => {
      await expect(client.addNode(1, 0, '192.0.2.4:5000')).rejects.toThrow(ValidationError);
      await expect(client.addNode(1, -1, '192.0.2.4:5000')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when address is missing', async () => {
      await expect(client.addNode(1, 4, undefined)).rejects.toThrow(ValidationError);
      await expect(client.addNode(1, 4, '')).rejects.toThrow(ValidationError);
      await expect(client.addNode(1, 4, null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when address is not in host:port format', async () => {
      await expect(client.addNode(1, 4, 'invalid')).rejects.toThrow(ValidationError);
      await expect(client.addNode(1, 4, '192.0.2.4')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when address has empty host', async () => {
      await expect(client.addNode(1, 4, ':5000')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when address port is invalid', async () => {
      await expect(client.addNode(1, 4, '192.0.2.4:0')).rejects.toThrow(ValidationError);
      await expect(client.addNode(1, 4, '192.0.2.4:70000')).rejects.toThrow(ValidationError);
      await expect(client.addNode(1, 4, '192.0.2.4:abc')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when address has multiple colons', async () => {
      await expect(client.addNode(1, 4, '192.0.2.4:5000:extra')).rejects.toThrow(ValidationError);
    });
  });

  describe('removeNode', () => {
    it('should remove node from shard successfully', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      await client.removeNode(1, 4);

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/cluster/shards/1/nodes/4');
    });

    it('should throw ValidationError when shardId is missing', async () => {
      await expect(client.removeNode(undefined, 4)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is negative', async () => {
      await expect(client.removeNode(-1, 4)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is missing', async () => {
      await expect(client.removeNode(1, undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is not positive', async () => {
      await expect(client.removeNode(1, 0)).rejects.toThrow(ValidationError);
      await expect(client.removeNode(1, -1)).rejects.toThrow(ValidationError);
    });
  });

  describe('createShard', () => {
    it('should create shard with valid replicas', async () => {
      const replicas = [
        { nodeId: 1, address: '192.0.2.1:5000' },
        { nodeId: 2, address: '192.0.2.2:5000' },
        { nodeId: 3, address: '192.0.2.3:5000' },
      ];
      mockTransport.post.mockResolvedValueOnce({ shardId: 2, created: true });

      const result = await client.createShard(2, replicas);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cluster/shards', {
        shardId: 2,
        replicas: [
          { nodeId: 1, address: '192.0.2.1:5000' },
          { nodeId: 2, address: '192.0.2.2:5000' },
          { nodeId: 3, address: '192.0.2.3:5000' },
        ],
      });
      expect(result.shardId).toBe(2);
      expect(result.created).toBe(true);
    });

    it('should throw ValidationError when shardId is missing', async () => {
      const replicas = [{ nodeId: 1, address: '192.0.2.1:5000' }];
      await expect(client.createShard(undefined, replicas)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is negative', async () => {
      const replicas = [{ nodeId: 1, address: '192.0.2.1:5000' }];
      await expect(client.createShard(-1, replicas)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when replicas is empty', async () => {
      await expect(client.createShard(1, [])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when replicas is not an array', async () => {
      await expect(client.createShard(1, 'invalid')).rejects.toThrow(ValidationError);
      await expect(client.createShard(1, null)).rejects.toThrow(ValidationError);
      await expect(client.createShard(1, undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when replica is not an object', async () => {
      await expect(client.createShard(1, ['invalid'])).rejects.toThrow(ValidationError);
      await expect(client.createShard(1, [null])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when replica nodeId is invalid', async () => {
      await expect(client.createShard(1, [{ nodeId: 0, address: '192.0.2.1:5000' }])).rejects.toThrow(ValidationError);
      await expect(client.createShard(1, [{ nodeId: -1, address: '192.0.2.1:5000' }])).rejects.toThrow(ValidationError);
      await expect(client.createShard(1, [{ address: '192.0.2.1:5000' }])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when replica address is missing', async () => {
      await expect(client.createShard(1, [{ nodeId: 1 }])).rejects.toThrow(ValidationError);
      await expect(client.createShard(1, [{ nodeId: 1, address: '' }])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when replica address is invalid format', async () => {
      await expect(client.createShard(1, [{ nodeId: 1, address: 'invalid' }])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when duplicate nodeId in replicas', async () => {
      const replicas = [
        { nodeId: 1, address: '192.0.2.1:5000' },
        { nodeId: 1, address: '192.0.2.2:5000' },
      ];
      await expect(client.createShard(1, replicas)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when duplicate address in replicas', async () => {
      const replicas = [
        { nodeId: 1, address: '192.0.2.1:5000' },
        { nodeId: 2, address: '192.0.2.1:5000' },
      ];
      await expect(client.createShard(1, replicas)).rejects.toThrow(ValidationError);
    });

    it('should handle null response with defaults', async () => {
      const replicas = [{ nodeId: 1, address: '192.0.2.1:5000' }];
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.createShard(1, replicas);

      expect(result.shardId).toBe(1);
      expect(result.created).toBe(true);
    });
  });

  describe('rebalanceCluster', () => {
    it('should start rebalance with default options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        operationId: 'op-123',
        status: 'PENDING',
        startedAt: '2025-01-15T00:00:00Z',
      });

      const result = await client.rebalanceCluster();

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cluster/rebalance', {});
      expect(result.operationId).toBe('op-123');
      expect(result.status).toBe('PENDING');
      expect(result.startedAt).toBe('2025-01-15T00:00:00Z');
    });

    it('should start rebalance with EVEN strategy', async () => {
      mockTransport.post.mockResolvedValueOnce({ operationId: 'op-123' });

      await client.rebalanceCluster({ strategy: RebalanceStrategy.EVEN });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cluster/rebalance', {
        strategy: 'EVEN',
      });
    });

    it('should start rebalance with DATA_SIZE strategy', async () => {
      mockTransport.post.mockResolvedValueOnce({ operationId: 'op-123' });

      await client.rebalanceCluster({ strategy: RebalanceStrategy.DATA_SIZE });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cluster/rebalance', {
        strategy: 'DATA_SIZE',
      });
    });

    it('should start rebalance with LOAD strategy', async () => {
      mockTransport.post.mockResolvedValueOnce({ operationId: 'op-123' });

      await client.rebalanceCluster({ strategy: RebalanceStrategy.LOAD });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cluster/rebalance', {
        strategy: 'LOAD',
      });
    });

    it('should start rebalance with maxConcurrentMoves', async () => {
      mockTransport.post.mockResolvedValueOnce({ operationId: 'op-123' });

      await client.rebalanceCluster({ maxConcurrentMoves: 2 });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cluster/rebalance', {
        maxConcurrentMoves: 2,
      });
    });

    it('should start rebalance with dryRun', async () => {
      mockTransport.post.mockResolvedValueOnce({ operationId: 'op-123' });

      await client.rebalanceCluster({ dryRun: true });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cluster/rebalance', {
        dryRun: true,
      });
    });

    it('should start rebalance with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ operationId: 'op-123' });

      await client.rebalanceCluster({
        strategy: RebalanceStrategy.EVEN,
        maxConcurrentMoves: 3,
        dryRun: false,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/cluster/rebalance', {
        strategy: 'EVEN',
        maxConcurrentMoves: 3,
        dryRun: false,
      });
    });

    it('should throw ValidationError for invalid strategy', async () => {
      await expect(client.rebalanceCluster({ strategy: 'INVALID' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-positive maxConcurrentMoves', async () => {
      await expect(client.rebalanceCluster({ maxConcurrentMoves: 0 })).rejects.toThrow(ValidationError);
      await expect(client.rebalanceCluster({ maxConcurrentMoves: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-integer maxConcurrentMoves', async () => {
      await expect(client.rebalanceCluster({ maxConcurrentMoves: 1.5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-boolean dryRun', async () => {
      await expect(client.rebalanceCluster({ dryRun: 'true' })).rejects.toThrow(ValidationError);
      await expect(client.rebalanceCluster({ dryRun: 1 })).rejects.toThrow(ValidationError);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.rebalanceCluster();

      expect(result.operationId).toBe('');
      expect(result.status).toBe(RebalanceStatus.PENDING);
      expect(result.startedAt).toBeDefined();
    });
  });

  describe('getRebalanceProgress', () => {
    it('should return rebalance progress', async () => {
      const mockResponse = {
        operationId: 'op-123',
        status: 'IN_PROGRESS',
        progress: 50,
        shardsMovedCount: 5,
        totalShardsToMove: 10,
        startedAt: '2025-01-15T00:00:00Z',
        completedAt: null,
        error: null,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRebalanceProgress('op-123');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster/rebalance/op-123');
      expect(result.operationId).toBe('op-123');
      expect(result.status).toBe('IN_PROGRESS');
      expect(result.progress).toBe(50);
      expect(result.shardsMovedCount).toBe(5);
      expect(result.totalShardsToMove).toBe(10);
    });

    it('should URL encode operationId with special characters', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getRebalanceProgress('op/123&456');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster/rebalance/op%2F123%26456');
    });

    it('should throw ValidationError when operationId is missing', async () => {
      await expect(client.getRebalanceProgress()).rejects.toThrow(ValidationError);
      await expect(client.getRebalanceProgress('')).rejects.toThrow(ValidationError);
      await expect(client.getRebalanceProgress(null)).rejects.toThrow(ValidationError);
    });

    it('should handle completed rebalance', async () => {
      const mockResponse = {
        operationId: 'op-123',
        status: 'COMPLETED',
        progress: 100,
        shardsMovedCount: 10,
        totalShardsToMove: 10,
        startedAt: '2025-01-15T00:00:00Z',
        completedAt: '2025-01-15T01:00:00Z',
        error: null,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRebalanceProgress('op-123');

      expect(result.status).toBe('COMPLETED');
      expect(result.progress).toBe(100);
      expect(result.completedAt).toBe('2025-01-15T01:00:00Z');
    });

    it('should handle failed rebalance', async () => {
      const mockResponse = {
        operationId: 'op-123',
        status: 'FAILED',
        progress: 30,
        error: 'Node failure during rebalance',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRebalanceProgress('op-123');

      expect(result.status).toBe('FAILED');
      expect(result.error).toBe('Node failure during rebalance');
    });

    it('should handle null response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getRebalanceProgress('op-123');

      expect(result.operationId).toBe('op-123');
      expect(result.status).toBe(RebalanceStatus.PENDING);
      expect(result.progress).toBe(0);
      expect(result.shardsMovedCount).toBe(0);
      expect(result.totalShardsToMove).toBe(0);
      expect(result.startedAt).toBeNull();
      expect(result.completedAt).toBeNull();
      expect(result.error).toBeNull();
    });
  });

  describe('getNodes', () => {
    it('should return list of nodes', async () => {
      const mockResponse = {
        nodes: [
          { id: 'node-1', name: 'Node 1', address: '192.0.2.1:5000', state: 'HEALTHY' },
          { id: 'node-2', name: 'Node 2', address: '192.0.2.2:5000', state: 'HEALTHY' },
          { id: 'node-3', name: 'Node 3', address: '192.0.2.3:5000', state: 'UNHEALTHY' },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getNodes();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster/nodes');
      expect(result.nodes).toHaveLength(3);
      expect(result.nodes[0].id).toBe('node-1');
      expect(result.nodes[0].state).toBe(NodeState.HEALTHY);
      expect(result.nodes[2].state).toBe(NodeState.UNHEALTHY);
    });

    it('should parse numeric node states', async () => {
      const mockResponse = {
        nodes: [
          { id: 'node-1', state: 0 },
          { id: 'node-2', state: 1 },
          { id: 'node-3', state: 2 },
          { id: 'node-4', state: 3 },
          { id: 'node-5', state: 99 },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getNodes();

      expect(result.nodes[0].state).toBe(NodeState.UNKNOWN);
      expect(result.nodes[1].state).toBe(NodeState.HEALTHY);
      expect(result.nodes[2].state).toBe(NodeState.UNHEALTHY);
      expect(result.nodes[3].state).toBe(NodeState.LEAVING);
      expect(result.nodes[4].state).toBe(NodeState.UNKNOWN);
    });

    it('should handle string state values', async () => {
      const mockResponse = {
        nodes: [
          { id: 'node-1', state: 'healthy' },
          { id: 'node-2', state: 'UNHEALTHY' },
          { id: 'node-3', state: 'leaving' },
          { id: 'node-4', state: 'invalid' },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getNodes();

      expect(result.nodes[0].state).toBe(NodeState.HEALTHY);
      expect(result.nodes[1].state).toBe(NodeState.UNHEALTHY);
      expect(result.nodes[2].state).toBe(NodeState.LEAVING);
      expect(result.nodes[3].state).toBe(NodeState.UNKNOWN);
    });

    it('should handle null response with empty nodes', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getNodes();

      expect(result.nodes).toEqual([]);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        nodes: [{ id: 'node-1' }],
      });

      const result = await client.getNodes();

      expect(result.nodes[0]).toEqual({
        id: 'node-1',
        name: '',
        address: '',
        state: NodeState.UNKNOWN,
      });
    });

    it('should handle node with explicit null fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        nodes: [{ id: null, name: null, address: null, state: null }],
      });

      const result = await client.getNodes();

      expect(result.nodes[0]).toEqual({
        id: '',
        name: '',
        address: '',
        state: NodeState.UNKNOWN,
      });
    });
  });

  describe('getShards', () => {
    it('should return list of shards', async () => {
      const mockResponse = {
        shards: [
          { shardId: 1, leaderId: 'node-1', replicas: ['node-1', 'node-2', 'node-3'], size: 1024 },
          { shardId: 2, leaderId: 'node-2', replicas: ['node-2', 'node-3', 'node-1'], size: 2048 },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getShards();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster/shards');
      expect(result.shards).toHaveLength(2);
      expect(result.shards[0].shardId).toBe(1);
      expect(result.shards[0].leaderId).toBe('node-1');
      expect(result.shards[0].replicas).toEqual(['node-1', 'node-2', 'node-3']);
      expect(result.shards[0].size).toBe(1024);
    });

    it('should handle null response with empty shards', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getShards();

      expect(result.shards).toEqual([]);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        shards: [{ shardId: 1 }],
      });

      const result = await client.getShards();

      expect(result.shards[0]).toEqual({
        shardId: 1,
        leaderId: '',
        replicas: [],
        size: 0,
      });
    });

    it('should handle shard with explicit null fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        shards: [{ shardId: null, leaderId: null, replicas: null, size: null }],
      });

      const result = await client.getShards();

      expect(result.shards[0]).toEqual({
        shardId: 0,
        leaderId: '',
        replicas: [],
        size: 0,
      });
    });
  });

  describe('Exported enums', () => {
    it('should export NodeState enum', () => {
      expect(NodeState.UNKNOWN).toBe('UNKNOWN');
      expect(NodeState.HEALTHY).toBe('HEALTHY');
      expect(NodeState.UNHEALTHY).toBe('UNHEALTHY');
      expect(NodeState.LEAVING).toBe('LEAVING');
    });

    it('should export RebalanceStrategy enum', () => {
      expect(RebalanceStrategy.EVEN).toBe('EVEN');
      expect(RebalanceStrategy.DATA_SIZE).toBe('DATA_SIZE');
      expect(RebalanceStrategy.LOAD).toBe('LOAD');
    });

    it('should export RebalanceStatus enum', () => {
      expect(RebalanceStatus.PENDING).toBe('PENDING');
      expect(RebalanceStatus.IN_PROGRESS).toBe('IN_PROGRESS');
      expect(RebalanceStatus.COMPLETED).toBe('COMPLETED');
      expect(RebalanceStatus.FAILED).toBe('FAILED');
      expect(RebalanceStatus.CANCELLED).toBe('CANCELLED');
    });

    it('should have frozen NodeState enum', () => {
      expect(Object.isFrozen(NodeState)).toBe(true);
    });

    it('should have frozen RebalanceStrategy enum', () => {
      expect(Object.isFrozen(RebalanceStrategy)).toBe(true);
    });

    it('should have frozen RebalanceStatus enum', () => {
      expect(Object.isFrozen(RebalanceStatus)).toBe(true);
    });
  });
});
