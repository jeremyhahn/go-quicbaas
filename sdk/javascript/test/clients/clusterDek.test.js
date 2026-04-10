/**
 * ClusterDEKClient Tests
 *
 * Comprehensive tests for the QuicBaaS Cluster DEK Client covering
 * initialization, status, rotation, node synchronization, re-encryption, and recovery.
 *
 * @module test/clients/clusterDek
 */

import ClusterDEKClient, {
  NodeDEKSyncStatus,
  ReencryptionState,
  RecoveryMethod,
} from '../../src/clients/clusterDek.js';
import { ValidationError } from '../../src/errors/index.js';

describe('ClusterDEKClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      request: jest.fn(),
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
    };
    client = new ClusterDEKClient(mockTransport);
  });

  // ===========================================================================
  // Constructor Tests
  // ===========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const transport = { request: jest.fn() };
      const dekClient = new ClusterDEKClient(transport);
      expect(dekClient).toBeInstanceOf(ClusterDEKClient);
    });

    it('should throw ValidationError when transport is not provided', () => {
      expect(() => new ClusterDEKClient()).toThrow(ValidationError);
      expect(() => new ClusterDEKClient()).toThrow('transport is required');
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new ClusterDEKClient(null)).toThrow(ValidationError);
      expect(() => new ClusterDEKClient(null)).toThrow('transport is required');
    });

    it('should throw ValidationError when transport has no request method', () => {
      expect(() => new ClusterDEKClient({})).toThrow(ValidationError);
      expect(() => new ClusterDEKClient({})).toThrow('transport must have a request method');
    });

    it('should throw ValidationError when transport.request is not a function', () => {
      expect(() => new ClusterDEKClient({ request: 'not-a-function' })).toThrow(ValidationError);
    });
  });

  // ===========================================================================
  // Enums Tests
  // ===========================================================================

  describe('NodeDEKSyncStatus enum', () => {
    it('should have correct values', () => {
      expect(NodeDEKSyncStatus.SYNCED).toBe('synced');
      expect(NodeDEKSyncStatus.OUT_OF_DATE).toBe('out_of_date');
      expect(NodeDEKSyncStatus.MISSING).toBe('missing');
      expect(NodeDEKSyncStatus.SYNC_FAILED).toBe('sync_failed');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(NodeDEKSyncStatus)).toBe(true);
    });
  });

  describe('ReencryptionState enum', () => {
    it('should have correct values', () => {
      expect(ReencryptionState.RUNNING).toBe('running');
      expect(ReencryptionState.PAUSED).toBe('paused');
      expect(ReencryptionState.COMPLETED).toBe('completed');
      expect(ReencryptionState.FAILED).toBe('failed');
      expect(ReencryptionState.CANCELLED).toBe('cancelled');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(ReencryptionState)).toBe(true);
    });
  });

  describe('RecoveryMethod enum', () => {
    it('should have correct values', () => {
      expect(RecoveryMethod.RECOVERY_KEYS).toBe('recovery_keys');
      expect(RecoveryMethod.BACKUP).toBe('backup');
      expect(RecoveryMethod.MANUAL).toBe('manual');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(RecoveryMethod)).toBe(true);
    });
  });

  // ===========================================================================
  // initializeClusterDEK Tests
  // ===========================================================================

  describe('initializeClusterDEK', () => {
    const validRequest = {
      algorithm: 'AES-256-GCM',
      keySize: 256,
    };

    const mockResponse = {
      version: 1,
      created_at: '2025-01-15T00:00:00Z',
      nodes_synced: 3,
      algorithm: 'AES-256-GCM',
      key_size: 256,
    };

    it('should initialize cluster DEK with minimal request', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.initializeClusterDEK(validRequest);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/initialize',
        {
          algorithm: 'AES-256-GCM',
          key_size: 256,
        }
      );
      expect(result.version).toBe(1);
      expect(result.createdAt).toBe(mockResponse.created_at);
      expect(result.nodesSynced).toBe(3);
      expect(result.algorithm).toBe('AES-256-GCM');
      expect(result.keySize).toBe(256);
    });

    it('should initialize cluster DEK with all options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const fullRequest = {
        algorithm: 'AES-256-GCM',
        keySize: 256,
        metadata: { environment: 'production' },
        rotationPolicy: {
          enabled: true,
          rotationInterval: 7776000000, // 90 days in ms
          autoReencrypt: true,
        },
      };

      await client.initializeClusterDEK(fullRequest);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/initialize',
        expect.objectContaining({
          algorithm: 'AES-256-GCM',
          key_size: 256,
          metadata: { environment: 'production' },
          rotation_policy: {
            enabled: true,
            rotation_interval: 7776000000,
            auto_reencrypt: true,
          },
        })
      );
    });

    it('should throw ValidationError when request is missing', async () => {
      await expect(client.initializeClusterDEK()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.initializeClusterDEK(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when algorithm is missing', async () => {
      await expect(client.initializeClusterDEK({ keySize: 256 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when algorithm is empty', async () => {
      await expect(client.initializeClusterDEK({ algorithm: '', keySize: 256 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keySize is missing', async () => {
      await expect(client.initializeClusterDEK({ algorithm: 'AES-256-GCM' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid keySize values', async () => {
      await expect(client.initializeClusterDEK({ algorithm: 'AES-256-GCM', keySize: 64 })).rejects.toThrow(ValidationError);
      await expect(client.initializeClusterDEK({ algorithm: 'AES-256-GCM', keySize: 64 })).rejects.toThrow(/must be one of/);
    });

    it('should throw ValidationError for non-positive keySize', async () => {
      await expect(client.initializeClusterDEK({ algorithm: 'AES-256-GCM', keySize: 0 })).rejects.toThrow(ValidationError);
      await expect(client.initializeClusterDEK({ algorithm: 'AES-256-GCM', keySize: -256 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-integer keySize', async () => {
      await expect(client.initializeClusterDEK({ algorithm: 'AES-256-GCM', keySize: 256.5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      const request = { ...validRequest, metadata: 'not-an-object' };
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(/must be an object/);
    });

    it('should throw ValidationError when metadata is an array', async () => {
      const request = { ...validRequest, metadata: ['key', 'value'] };
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when rotationPolicy is not an object', async () => {
      const request = { ...validRequest, rotationPolicy: 'not-an-object' };
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(/must be an object/);
    });

    it('should throw ValidationError when rotationPolicy.enabled is not boolean', async () => {
      const request = { ...validRequest, rotationPolicy: { enabled: 'true' } };
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(/must be a boolean/);
    });

    it('should throw ValidationError when rotation enabled but rotationInterval missing', async () => {
      const request = { ...validRequest, rotationPolicy: { enabled: true } };
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(/rotationInterval is required/);
    });

    it('should throw ValidationError for non-positive rotationInterval', async () => {
      const request = { ...validRequest, rotationPolicy: { enabled: true, rotationInterval: 0 } };
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when autoReencrypt is not boolean', async () => {
      const request = {
        ...validRequest,
        rotationPolicy: { enabled: true, rotationInterval: 1000, autoReencrypt: 'true' },
      };
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
      await expect(client.initializeClusterDEK(request)).rejects.toThrow(/autoReencrypt must be a boolean/);
    });

    it('should accept valid keySize values', async () => {
      const validKeySizes = [128, 192, 256, 384, 512];
      for (const keySize of validKeySizes) {
        mockTransport.post.mockResolvedValueOnce({ ...mockResponse, key_size: keySize });
        const result = await client.initializeClusterDEK({ algorithm: 'AES', keySize });
        expect(result.keySize).toBe(keySize);
      }
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);
      await expect(client.initializeClusterDEK(validRequest)).rejects.toThrow(ValidationError);
      await expect(client.initializeClusterDEK(validRequest)).rejects.toThrow(/Invalid response from server/);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        version: 1,
        createdAt: '2025-01-15T00:00:00Z',
        nodesSynced: 3,
        algorithm: 'AES-256-GCM',
        keySize: 256,
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.initializeClusterDEK(validRequest);

      expect(result.createdAt).toBe(camelCaseResponse.createdAt);
      expect(result.nodesSynced).toBe(camelCaseResponse.nodesSynced);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.initializeClusterDEK(validRequest)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // getClusterDEKStatus Tests
  // ===========================================================================

  describe('getClusterDEKStatus', () => {
    const mockResponse = {
      initialized: true,
      current_version: 2,
      created_at: '2025-01-01T00:00:00Z',
      rotated_at: '2025-01-15T00:00:00Z',
      next_rotation: '2025-04-15T00:00:00Z',
      nodes_synced: 3,
      total_nodes: 3,
      algorithm: 'AES-256-GCM',
      key_size: 256,
      reencryption_in_progress: false,
    };

    it('should get cluster DEK status successfully', async () => {
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getClusterDEKStatus();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster-dek/status');
      expect(result.initialized).toBe(true);
      expect(result.currentVersion).toBe(2);
      expect(result.createdAt).toBe(mockResponse.created_at);
      expect(result.rotatedAt).toBe(mockResponse.rotated_at);
      expect(result.nextRotation).toBe(mockResponse.next_rotation);
      expect(result.nodesSynced).toBe(3);
      expect(result.totalNodes).toBe(3);
      expect(result.algorithm).toBe('AES-256-GCM');
      expect(result.keySize).toBe(256);
      expect(result.reencryptionInProgress).toBe(false);
    });

    it('should include reencryption progress when in progress', async () => {
      const responseWithProgress = {
        ...mockResponse,
        reencryption_in_progress: true,
        reencryption_progress: 45,
      };
      mockTransport.get.mockResolvedValueOnce(responseWithProgress);

      const result = await client.getClusterDEKStatus();

      expect(result.reencryptionInProgress).toBe(true);
      expect(result.reencryptionProgress).toBe(45);
    });

    it('should include nodes out of sync when present', async () => {
      const responseWithNodes = {
        ...mockResponse,
        nodes_out_of_sync: [
          { node_id: 1, node_name: 'node-1', current_version: 1, last_synced_at: '2025-01-10T00:00:00Z' },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(responseWithNodes);

      const result = await client.getClusterDEKStatus();

      expect(result.nodesOutOfSync).toHaveLength(1);
      expect(result.nodesOutOfSync[0].nodeId).toBe(1);
      expect(result.nodesOutOfSync[0].nodeName).toBe('node-1');
      expect(result.nodesOutOfSync[0].currentVersion).toBe(1);
    });

    it('should include previous versions when present', async () => {
      const responseWithVersions = {
        ...mockResponse,
        previous_versions: [
          { version: 1, created_at: '2025-01-01T00:00:00Z', deprecated_at: '2025-01-15T00:00:00Z', entities_encrypted: 100, can_delete: false },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(responseWithVersions);

      const result = await client.getClusterDEKStatus();

      expect(result.previousVersions).toHaveLength(1);
      expect(result.previousVersions[0].version).toBe(1);
      expect(result.previousVersions[0].deprecatedAt).toBe('2025-01-15T00:00:00Z');
      expect(result.previousVersions[0].entitiesEncrypted).toBe(100);
      expect(result.previousVersions[0].canDelete).toBe(false);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);
      await expect(client.getClusterDEKStatus()).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        initialized: true,
        currentVersion: 2,
        createdAt: '2025-01-01T00:00:00Z',
        rotatedAt: '2025-01-15T00:00:00Z',
        nextRotation: '2025-04-15T00:00:00Z',
        nodesSynced: 3,
        totalNodes: 3,
        algorithm: 'AES-256-GCM',
        keySize: 256,
        reencryptionInProgress: false,
      };
      mockTransport.get.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.getClusterDEKStatus();

      expect(result.currentVersion).toBe(camelCaseResponse.currentVersion);
      expect(result.rotatedAt).toBe(camelCaseResponse.rotatedAt);
    });

    it('should handle empty response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getClusterDEKStatus();

      expect(result.initialized).toBe(false);
      expect(result.currentVersion).toBe(0);
      expect(result.nodesSynced).toBe(0);
      expect(result.totalNodes).toBe(0);
      expect(result.reencryptionInProgress).toBe(false);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getClusterDEKStatus()).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // rotateClusterDEK Tests
  // ===========================================================================

  describe('rotateClusterDEK', () => {
    const validRequest = {
      reason: 'Scheduled quarterly rotation',
    };

    const mockResponse = {
      new_version: 3,
      old_version: 2,
      rotated_at: '2025-01-17T00:00:00Z',
      rotation_id: 'rot-12345',
      nodes_synced: 3,
      reencryption_started: false,
    };

    it('should rotate cluster DEK with minimal request', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.rotateClusterDEK(validRequest);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/rotate',
        { reason: 'Scheduled quarterly rotation' }
      );
      expect(result.newVersion).toBe(3);
      expect(result.oldVersion).toBe(2);
      expect(result.rotatedAt).toBe(mockResponse.rotated_at);
      expect(result.rotationId).toBe('rot-12345');
      expect(result.nodesSynced).toBe(3);
      expect(result.reencryptionStarted).toBe(false);
    });

    it('should rotate cluster DEK with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        ...mockResponse,
        reencryption_started: true,
        estimated_reencryption_time: 3600000,
      });

      const fullRequest = {
        reason: 'Security incident response',
        scheduledTime: '2025-01-18T00:00:00Z',
        forceRotation: true,
        autoStartReencryption: true,
        metadata: { incident_id: 'INC-12345' },
      };

      const result = await client.rotateClusterDEK(fullRequest);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/rotate',
        expect.objectContaining({
          reason: 'Security incident response',
          scheduled_time: '2025-01-18T00:00:00Z',
          force_rotation: true,
          auto_start_reencryption: true,
          metadata: { incident_id: 'INC-12345' },
        })
      );
      expect(result.reencryptionStarted).toBe(true);
      expect(result.estimatedReencryptionTime).toBe(3600000);
    });

    it('should throw ValidationError when request is missing', async () => {
      await expect(client.rotateClusterDEK()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.rotateClusterDEK(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when reason is missing', async () => {
      await expect(client.rotateClusterDEK({})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when reason is empty', async () => {
      await expect(client.rotateClusterDEK({ reason: '' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when scheduledTime is not a string', async () => {
      const request = { reason: 'test', scheduledTime: 12345 };
      await expect(client.rotateClusterDEK(request)).rejects.toThrow(ValidationError);
      await expect(client.rotateClusterDEK(request)).rejects.toThrow(/must be a string/);
    });

    it('should throw ValidationError when forceRotation is not a boolean', async () => {
      const request = { reason: 'test', forceRotation: 'true' };
      await expect(client.rotateClusterDEK(request)).rejects.toThrow(ValidationError);
      await expect(client.rotateClusterDEK(request)).rejects.toThrow(/must be a boolean/);
    });

    it('should throw ValidationError when autoStartReencryption is not a boolean', async () => {
      const request = { reason: 'test', autoStartReencryption: 'true' };
      await expect(client.rotateClusterDEK(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      const request = { reason: 'test', metadata: 'not-an-object' };
      await expect(client.rotateClusterDEK(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);
      await expect(client.rotateClusterDEK(validRequest)).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        newVersion: 3,
        oldVersion: 2,
        rotatedAt: '2025-01-17T00:00:00Z',
        rotationId: 'rot-12345',
        nodesSynced: 3,
        reencryptionStarted: true,
        estimatedReencryptionTime: 3600000,
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.rotateClusterDEK(validRequest);

      expect(result.newVersion).toBe(3);
      expect(result.rotatedAt).toBe(camelCaseResponse.rotatedAt);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.rotateClusterDEK(validRequest)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // syncNodeDEK Tests
  // ===========================================================================

  describe('syncNodeDEK', () => {
    const validNodeId = 1;

    const mockResponse = {
      node_id: 1,
      versions_synced: [1, 2, 3],
      current_version: 3,
      synced_at: '2025-01-17T00:00:00Z',
    };

    it('should sync node DEK with default options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.syncNodeDEK(validNodeId);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/nodes/1/sync',
        { node_id: 1 }
      );
      expect(result.nodeId).toBe(1);
      expect(result.versionsSynced).toEqual([1, 2, 3]);
      expect(result.currentVersion).toBe(3);
      expect(result.syncedAt).toBe(mockResponse.synced_at);
    });

    it('should sync node DEK with custom options', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const options = {
        currentVersion: 2,
        forceResync: true,
      };

      await client.syncNodeDEK(validNodeId, options);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/nodes/1/sync',
        {
          node_id: 1,
          current_version: 2,
          force_resync: true,
        }
      );
    });

    it('should throw ValidationError when nodeId is missing', async () => {
      await expect(client.syncNodeDEK()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is null', async () => {
      await expect(client.syncNodeDEK(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is not a number', async () => {
      await expect(client.syncNodeDEK('1')).rejects.toThrow(ValidationError);
      await expect(client.syncNodeDEK('1')).rejects.toThrow(/must be a positive integer/);
    });

    it('should throw ValidationError when nodeId is zero', async () => {
      await expect(client.syncNodeDEK(0)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is negative', async () => {
      await expect(client.syncNodeDEK(-1)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is not an integer', async () => {
      await expect(client.syncNodeDEK(1.5)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative currentVersion option', async () => {
      await expect(client.syncNodeDEK(1, { currentVersion: -1 })).rejects.toThrow(ValidationError);
    });

    it('should allow zero currentVersion option', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);
      await client.syncNodeDEK(1, { currentVersion: 0 });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/nodes/1/sync',
        expect.objectContaining({ current_version: 0 })
      );
    });

    it('should throw ValidationError when forceResync is not a boolean', async () => {
      await expect(client.syncNodeDEK(1, { forceResync: 'true' })).rejects.toThrow(ValidationError);
      await expect(client.syncNodeDEK(1, { forceResync: 'true' })).rejects.toThrow(/must be a boolean/);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);
      await expect(client.syncNodeDEK(validNodeId)).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        nodeId: 1,
        versionsSynced: [1, 2, 3],
        currentVersion: 3,
        syncedAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.syncNodeDEK(validNodeId);

      expect(result.versionsSynced).toEqual([1, 2, 3]);
      expect(result.syncedAt).toBe(camelCaseResponse.syncedAt);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.syncNodeDEK(validNodeId)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // getNodeDEKStatus Tests
  // ===========================================================================

  describe('getNodeDEKStatus', () => {
    const validNodeId = 1;

    const mockResponse = {
      node_id: 1,
      node_name: 'node-1',
      current_version: 3,
      last_synced_at: '2025-01-17T00:00:00Z',
      status: 'synced',
      versions_available: [1, 2, 3],
    };

    it('should get node DEK status successfully', async () => {
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getNodeDEKStatus(validNodeId);

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster-dek/nodes/1/status');
      expect(result.nodeId).toBe(1);
      expect(result.nodeName).toBe('node-1');
      expect(result.currentVersion).toBe(3);
      expect(result.lastSyncedAt).toBe(mockResponse.last_synced_at);
      expect(result.status).toBe('synced');
      expect(result.versionsAvailable).toEqual([1, 2, 3]);
    });

    it('should include error when present', async () => {
      const responseWithError = {
        ...mockResponse,
        status: 'sync_failed',
        error: 'Connection refused',
      };
      mockTransport.get.mockResolvedValueOnce(responseWithError);

      const result = await client.getNodeDEKStatus(validNodeId);

      expect(result.status).toBe('sync_failed');
      expect(result.error).toBe('Connection refused');
    });

    it('should throw ValidationError when nodeId is missing', async () => {
      await expect(client.getNodeDEKStatus()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is not a positive integer', async () => {
      await expect(client.getNodeDEKStatus(0)).rejects.toThrow(ValidationError);
      await expect(client.getNodeDEKStatus(-1)).rejects.toThrow(ValidationError);
      await expect(client.getNodeDEKStatus('1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);
      await expect(client.getNodeDEKStatus(validNodeId)).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        nodeId: 1,
        nodeName: 'node-1',
        currentVersion: 3,
        lastSyncedAt: '2025-01-17T00:00:00Z',
        status: 'synced',
        versionsAvailable: [1, 2, 3],
      };
      mockTransport.get.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.getNodeDEKStatus(validNodeId);

      expect(result.nodeName).toBe('node-1');
      expect(result.lastSyncedAt).toBe(camelCaseResponse.lastSyncedAt);
    });

    it('should handle response without nodeName', async () => {
      const responseWithoutName = { ...mockResponse };
      delete responseWithoutName.node_name;
      mockTransport.get.mockResolvedValueOnce(responseWithoutName);

      const result = await client.getNodeDEKStatus(validNodeId);

      expect(result.nodeName).toBeUndefined();
    });

    it('should handle empty response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getNodeDEKStatus(validNodeId);

      expect(result.nodeId).toBe(0);
      expect(result.currentVersion).toBe(0);
      expect(result.status).toBe(NodeDEKSyncStatus.MISSING);
      expect(result.versionsAvailable).toEqual([]);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getNodeDEKStatus(validNodeId)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // startReencryption Tests
  // ===========================================================================

  describe('startReencryption', () => {
    const validRequest = {
      fromVersion: 1,
      toVersion: 2,
    };

    const mockResponse = {
      operation_id: 'reenc-12345',
      started: true,
      estimated_items: 10000,
      timestamp: '2025-01-17T00:00:00Z',
      started_at: '2025-01-17T00:00:00Z',
      total_entities: 10000,
      estimated_completion: '2025-01-17T01:00:00Z',
    };

    it('should start re-encryption with minimal request', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.startReencryption(validRequest);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/reencryption/start',
        {
          from_version: 1,
          to_version: 2,
        }
      );
      expect(result.operationId).toBe('reenc-12345');
      expect(result.started).toBe(true);
      expect(result.estimatedItems).toBe(10000);
      expect(result.totalEntities).toBe(10000);
    });

    it('should start re-encryption with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ ...mockResponse, path: '/data/users' });

      const fullRequest = {
        fromVersion: 1,
        toVersion: 2,
        path: '/data/users',
        batchSize: 1000,
        concurrency: 4,
        throttleRate: 5000,
        force: true,
      };

      const result = await client.startReencryption(fullRequest);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/reencryption/start',
        expect.objectContaining({
          from_version: 1,
          to_version: 2,
          path: '/data/users',
          batch_size: 1000,
          concurrency: 4,
          throttle_rate: 5000,
          force: true,
        })
      );
      expect(result.path).toBe('/data/users');
    });

    it('should throw ValidationError when request is missing', async () => {
      await expect(client.startReencryption()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.startReencryption(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when fromVersion is missing', async () => {
      await expect(client.startReencryption({ toVersion: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when toVersion is missing', async () => {
      await expect(client.startReencryption({ fromVersion: 1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when fromVersion is not positive', async () => {
      await expect(client.startReencryption({ fromVersion: 0, toVersion: 2 })).rejects.toThrow(ValidationError);
      await expect(client.startReencryption({ fromVersion: -1, toVersion: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when toVersion is not positive', async () => {
      await expect(client.startReencryption({ fromVersion: 1, toVersion: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when toVersion is not greater than fromVersion', async () => {
      await expect(client.startReencryption({ fromVersion: 2, toVersion: 1 })).rejects.toThrow(ValidationError);
      await expect(client.startReencryption({ fromVersion: 2, toVersion: 1 })).rejects.toThrow(/must be greater than/);

      await expect(client.startReencryption({ fromVersion: 2, toVersion: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when path is not a string', async () => {
      const request = { ...validRequest, path: 123 };
      await expect(client.startReencryption(request)).rejects.toThrow(ValidationError);
      await expect(client.startReencryption(request)).rejects.toThrow(/must be a string/);
    });

    it('should throw ValidationError for negative batchSize', async () => {
      const request = { ...validRequest, batchSize: -1 };
      await expect(client.startReencryption(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-integer batchSize', async () => {
      const request = { ...validRequest, batchSize: 1.5 };
      await expect(client.startReencryption(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative concurrency', async () => {
      const request = { ...validRequest, concurrency: -1 };
      await expect(client.startReencryption(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for negative throttleRate', async () => {
      const request = { ...validRequest, throttleRate: -1 };
      await expect(client.startReencryption(request)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when force is not a boolean', async () => {
      const request = { ...validRequest, force: 'true' };
      await expect(client.startReencryption(request)).rejects.toThrow(ValidationError);
      await expect(client.startReencryption(request)).rejects.toThrow(/must be a boolean/);
    });

    it('should allow zero values for batchSize, concurrency, throttleRate', async () => {
      mockTransport.post.mockResolvedValueOnce(mockResponse);
      const request = { ...validRequest, batchSize: 0, concurrency: 0, throttleRate: 0 };
      await client.startReencryption(request);
      expect(mockTransport.post).toHaveBeenCalled();
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);
      await expect(client.startReencryption(validRequest)).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        operationId: 'reenc-12345',
        started: true,
        estimatedItems: 10000,
        timestamp: '2025-01-17T00:00:00Z',
        startedAt: '2025-01-17T00:00:00Z',
        totalEntities: 10000,
        estimatedCompletion: '2025-01-17T01:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.startReencryption(validRequest);

      expect(result.operationId).toBe('reenc-12345');
      expect(result.startedAt).toBe(camelCaseResponse.startedAt);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.startReencryption(validRequest)).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // getReencryptionStatus Tests
  // ===========================================================================

  describe('getReencryptionStatus', () => {
    const mockResponse = {
      operation_id: 'reenc-12345',
      state: 'running',
      started_at: '2025-01-17T00:00:00Z',
      progress: 45,
      total_entities: 10000,
      processed_entities: 4500,
      failed_entities: 5,
      throughput_rate: 1000,
    };

    it('should get re-encryption status successfully', async () => {
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getReencryptionStatus();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/cluster-dek/reencryption/status');
      expect(result.operationId).toBe('reenc-12345');
      expect(result.state).toBe('running');
      expect(result.startedAt).toBe(mockResponse.started_at);
      expect(result.progress).toBe(45);
      expect(result.totalEntities).toBe(10000);
      expect(result.processedEntities).toBe(4500);
      expect(result.failedEntities).toBe(5);
      expect(result.throughputRate).toBe(1000);
    });

    it('should include completed status fields', async () => {
      const completedResponse = {
        ...mockResponse,
        state: 'completed',
        completed_at: '2025-01-17T01:00:00Z',
        progress: 100,
      };
      mockTransport.get.mockResolvedValueOnce(completedResponse);

      const result = await client.getReencryptionStatus();

      expect(result.state).toBe('completed');
      expect(result.completedAt).toBe('2025-01-17T01:00:00Z');
      expect(result.progress).toBe(100);
    });

    it('should include estimated completion', async () => {
      const responseWithEstimate = {
        ...mockResponse,
        estimated_completion: '2025-01-17T01:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(responseWithEstimate);

      const result = await client.getReencryptionStatus();

      expect(result.estimatedCompletion).toBe('2025-01-17T01:00:00Z');
    });

    it('should include node progress when present', async () => {
      const responseWithNodeProgress = {
        ...mockResponse,
        node_progress: [
          {
            node_id: 1,
            node_name: 'node-1',
            progress: 50,
            processed_entities: 500,
            total_entities: 1000,
            throughput_rate: 500,
            last_updated_at: '2025-01-17T00:30:00Z',
          },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(responseWithNodeProgress);

      const result = await client.getReencryptionStatus();

      expect(result.nodeProgress).toHaveLength(1);
      expect(result.nodeProgress[0].nodeId).toBe(1);
      expect(result.nodeProgress[0].nodeName).toBe('node-1');
      expect(result.nodeProgress[0].progress).toBe(50);
    });

    it('should include errors when present', async () => {
      const responseWithErrors = {
        ...mockResponse,
        errors: [
          {
            timestamp: '2025-01-17T00:30:00Z',
            node_id: 1,
            entity_id: 'entity-123',
            error: 'Decryption failed',
            retried: true,
          },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(responseWithErrors);

      const result = await client.getReencryptionStatus();

      expect(result.errors).toHaveLength(1);
      expect(result.errors[0].nodeId).toBe(1);
      expect(result.errors[0].entityId).toBe('entity-123');
      expect(result.errors[0].error).toBe('Decryption failed');
      expect(result.errors[0].retried).toBe(true);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);
      await expect(client.getReencryptionStatus()).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        operationId: 'reenc-12345',
        state: 'running',
        startedAt: '2025-01-17T00:00:00Z',
        progress: 45,
        totalEntities: 10000,
        processedEntities: 4500,
        failedEntities: 5,
        throughputRate: 1000,
      };
      mockTransport.get.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.getReencryptionStatus();

      expect(result.startedAt).toBe(camelCaseResponse.startedAt);
      expect(result.processedEntities).toBe(4500);
    });

    it('should handle empty response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getReencryptionStatus();

      expect(result.operationId).toBe('');
      expect(result.state).toBe(ReencryptionState.RUNNING);
      expect(result.progress).toBe(0);
      expect(result.totalEntities).toBe(0);
      expect(result.processedEntities).toBe(0);
      expect(result.failedEntities).toBe(0);
      expect(result.throughputRate).toBe(0);
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.get.mockRejectedValueOnce(error);

      await expect(client.getReencryptionStatus()).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // cancelReencryption Tests
  // ===========================================================================

  describe('cancelReencryption', () => {
    it('should cancel re-encryption successfully', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      await client.cancelReencryption();

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/cluster-dek/reencryption/cancel',
        {}
      );
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.cancelReencryption()).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // recoverDEK Tests
  // ===========================================================================

  describe('recoverDEK', () => {
    const mockResponse = {
      success: true,
      recovered_version: 3,
      recovered_at: '2025-01-17T00:00:00Z',
      nodes_synced: 3,
      audit_id: 'audit-12345',
    };

    describe('with RECOVERY_KEYS method', () => {
      const validRequest = {
        recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
        reason: 'Disaster recovery',
        recoveryKeys: ['key1', 'key2', 'key3'],
      };

      it('should recover DEK with recovery keys', async () => {
        mockTransport.post.mockResolvedValueOnce(mockResponse);

        const result = await client.recoverDEK(validRequest);

        expect(mockTransport.post).toHaveBeenCalledWith(
          '/api/v1/cluster-dek/recover',
          {
            recovery_method: 'recovery_keys',
            reason: 'Disaster recovery',
            recovery_keys: ['key1', 'key2', 'key3'],
          }
        );
        expect(result.success).toBe(true);
        expect(result.recoveredVersion).toBe(3);
        expect(result.recoveredAt).toBe(mockResponse.recovered_at);
        expect(result.nodesSynced).toBe(3);
        expect(result.auditId).toBe('audit-12345');
      });

      it('should throw ValidationError when recoveryKeys is missing', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
          reason: 'Disaster recovery',
        };
        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
      });

      it('should throw ValidationError when recoveryKeys is empty', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
          reason: 'Disaster recovery',
          recoveryKeys: [],
        };
        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
        await expect(client.recoverDEK(request)).rejects.toThrow(/cannot be empty/);
      });

      it('should throw ValidationError when recoveryKeys contains empty strings', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
          reason: 'Disaster recovery',
          recoveryKeys: ['key1', '', 'key3'],
        };
        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
        await expect(client.recoverDEK(request)).rejects.toThrow(/non-empty string/);
      });

      it('should throw ValidationError when recoveryKeys contains non-strings', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
          reason: 'Disaster recovery',
          recoveryKeys: ['key1', 123, 'key3'],
        };
        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
      });
    });

    describe('with BACKUP method', () => {
      const validRequest = {
        recoveryMethod: RecoveryMethod.BACKUP,
        reason: 'Restore from backup',
        backupLocation: 's3://backups/dek-backup.enc',
      };

      it('should recover DEK from backup', async () => {
        mockTransport.post.mockResolvedValueOnce(mockResponse);

        const result = await client.recoverDEK(validRequest);

        expect(mockTransport.post).toHaveBeenCalledWith(
          '/api/v1/cluster-dek/recover',
          {
            recovery_method: 'backup',
            reason: 'Restore from backup',
            backup_location: 's3://backups/dek-backup.enc',
          }
        );
        expect(result.success).toBe(true);
      });

      it('should throw ValidationError when backupLocation is missing', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.BACKUP,
          reason: 'Restore from backup',
        };
        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
      });

      it('should throw ValidationError when backupLocation is empty', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.BACKUP,
          reason: 'Restore from backup',
          backupLocation: '',
        };
        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
      });
    });

    describe('with MANUAL method', () => {
      const validRequest = {
        recoveryMethod: RecoveryMethod.MANUAL,
        reason: 'Break-glass recovery',
        manualKey: 'base64-encoded-key-data',
      };

      it('should recover DEK with manual key', async () => {
        mockTransport.post.mockResolvedValueOnce(mockResponse);

        const result = await client.recoverDEK(validRequest);

        expect(mockTransport.post).toHaveBeenCalledWith(
          '/api/v1/cluster-dek/recover',
          {
            recovery_method: 'manual',
            reason: 'Break-glass recovery',
            manual_key: 'base64-encoded-key-data',
          }
        );
        expect(result.success).toBe(true);
      });

      it('should throw ValidationError when manualKey is missing', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.MANUAL,
          reason: 'Break-glass recovery',
        };
        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
      });

      it('should throw ValidationError when manualKey is empty', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.MANUAL,
          reason: 'Break-glass recovery',
          manualKey: '',
        };
        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
      });
    });

    it('should throw ValidationError when request is missing', async () => {
      await expect(client.recoverDEK()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.recoverDEK(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when reason is missing', async () => {
      await expect(client.recoverDEK({ recoveryMethod: RecoveryMethod.BACKUP })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when reason is empty', async () => {
      await expect(client.recoverDEK({ recoveryMethod: RecoveryMethod.BACKUP, reason: '' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when recoveryMethod is missing', async () => {
      await expect(client.recoverDEK({ reason: 'test' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when recoveryMethod is invalid', async () => {
      await expect(client.recoverDEK({ reason: 'test', recoveryMethod: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should include warnings when present', async () => {
      const responseWithWarnings = {
        ...mockResponse,
        warnings: ['Node 2 could not be synced', 'Node 3 required retry'],
      };
      mockTransport.post.mockResolvedValueOnce(responseWithWarnings);

      const result = await client.recoverDEK({
        recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
        reason: 'test',
        recoveryKeys: ['key1', 'key2', 'key3'],
      });

      expect(result.warnings).toEqual(['Node 2 could not be synced', 'Node 3 required retry']);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);
      await expect(client.recoverDEK({
        recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
        reason: 'test',
        recoveryKeys: ['key1', 'key2', 'key3'],
      })).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        success: true,
        recoveredVersion: 3,
        recoveredAt: '2025-01-17T00:00:00Z',
        nodesSynced: 3,
        auditId: 'audit-12345',
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.recoverDEK({
        recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
        reason: 'test',
        recoveryKeys: ['key1', 'key2', 'key3'],
      });

      expect(result.recoveredAt).toBe(camelCaseResponse.recoveredAt);
      expect(result.auditId).toBe(camelCaseResponse.auditId);
    });

    it('should handle empty response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.recoverDEK({
        recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
        reason: 'test',
        recoveryKeys: ['key1', 'key2', 'key3'],
      });

      expect(result.success).toBe(false);
      expect(result.recoveredVersion).toBe(0);
      expect(result.nodesSynced).toBe(0);
      expect(result.auditId).toBe('');
    });

    it('should propagate transport errors', async () => {
      const error = new Error('Network error');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.recoverDEK({
        recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
        reason: 'test',
        recoveryKeys: ['key1', 'key2', 'key3'],
      })).rejects.toThrow('Network error');
    });
  });

  // ===========================================================================
  // Additional Branch Coverage Tests
  // ===========================================================================

  describe('branch coverage - response transformations', () => {
    describe('initializeClusterDEK additional branches', () => {
      it('should handle rotationPolicy with enabled=false', async () => {
        mockTransport.post.mockResolvedValueOnce({
          version: 1,
          created_at: '2025-01-15T00:00:00Z',
        });

        const request = {
          algorithm: 'AES-256-GCM',
          keySize: 256,
          rotationPolicy: { enabled: false },
        };

        await client.initializeClusterDEK(request);

        expect(mockTransport.post).toHaveBeenCalledWith(
          '/api/v1/cluster-dek/initialize',
          expect.objectContaining({
            rotation_policy: expect.objectContaining({ enabled: false }),
          })
        );
      });

      it('should handle rotationPolicy.rotationInterval = null when enabled', async () => {
        const request = {
          algorithm: 'AES-256-GCM',
          keySize: 256,
          rotationPolicy: { enabled: true, rotationInterval: null },
        };

        await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
        await expect(client.initializeClusterDEK(request)).rejects.toThrow(/rotationInterval is required/);
      });

      it('should handle rotationPolicy as array (invalid)', async () => {
        const request = {
          algorithm: 'AES-256-GCM',
          keySize: 256,
          rotationPolicy: [true, 1000],
        };

        await expect(client.initializeClusterDEK(request)).rejects.toThrow(ValidationError);
      });
    });

    describe('getClusterDEKStatus additional branches', () => {
      it('should handle camelCase nodesOutOfSync response', async () => {
        const response = {
          initialized: true,
          currentVersion: 2,
          nodesOutOfSync: [
            { nodeId: 1, nodeName: 'node-1', currentVersion: 1, lastSyncedAt: '2025-01-10T00:00:00Z' },
          ],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.nodesOutOfSync).toHaveLength(1);
        expect(result.nodesOutOfSync[0].nodeId).toBe(1);
      });

      it('should handle camelCase previousVersions response', async () => {
        const response = {
          initialized: true,
          currentVersion: 2,
          previousVersions: [
            { version: 1, createdAt: '2025-01-01T00:00:00Z', deprecatedAt: '2025-01-15T00:00:00Z', entitiesEncrypted: 100, canDelete: false },
          ],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.previousVersions).toHaveLength(1);
        expect(result.previousVersions[0].deprecatedAt).toBe('2025-01-15T00:00:00Z');
      });

      it('should handle camelCase reencryptionProgress response', async () => {
        const response = {
          initialized: true,
          reencryptionInProgress: true,
          reencryptionProgress: 75,
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.reencryptionProgress).toBe(75);
      });

      it('should handle nodes without nodeName', async () => {
        const response = {
          initialized: true,
          nodes_out_of_sync: [
            { node_id: 1, current_version: 1, last_synced_at: '2025-01-10T00:00:00Z' },
          ],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.nodesOutOfSync[0].nodeName).toBeUndefined();
      });

      it('should handle previous versions without deprecatedAt', async () => {
        const response = {
          initialized: true,
          previous_versions: [
            { version: 1, created_at: '2025-01-01T00:00:00Z', entities_encrypted: 100, can_delete: true },
          ],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.previousVersions[0].deprecatedAt).toBeUndefined();
      });
    });

    describe('rotateClusterDEK additional branches', () => {
      it('should include null scheduledTime in request when explicitly set', async () => {
        mockTransport.post.mockResolvedValueOnce({
          new_version: 3,
          old_version: 2,
          rotated_at: '2025-01-17T00:00:00Z',
          rotation_id: 'rot-12345',
        });

        const request = {
          reason: 'test',
          scheduledTime: null,
        };

        await client.rotateClusterDEK(request);

        // When scheduledTime is explicitly null, it's included (indicates immediate rotation)
        expect(mockTransport.post).toHaveBeenCalledWith(
          '/api/v1/cluster-dek/rotate',
          { reason: 'test', scheduled_time: null }
        );
      });

      it('should not include scheduledTime when undefined', async () => {
        mockTransport.post.mockResolvedValueOnce({
          new_version: 3,
          old_version: 2,
          rotated_at: '2025-01-17T00:00:00Z',
          rotation_id: 'rot-12345',
        });

        await client.rotateClusterDEK({ reason: 'test' });

        expect(mockTransport.post).toHaveBeenCalledWith(
          '/api/v1/cluster-dek/rotate',
          { reason: 'test' }
        );
      });

      it('should handle response without estimatedReencryptionTime', async () => {
        mockTransport.post.mockResolvedValueOnce({
          new_version: 3,
          old_version: 2,
          rotated_at: '2025-01-17T00:00:00Z',
          rotation_id: 'rot-12345',
          reencryption_started: false,
        });

        const result = await client.rotateClusterDEK({ reason: 'test' });

        expect(result.estimatedReencryptionTime).toBeUndefined();
      });

      it('should handle camelCase estimatedReencryptionTime', async () => {
        mockTransport.post.mockResolvedValueOnce({
          newVersion: 3,
          oldVersion: 2,
          rotatedAt: '2025-01-17T00:00:00Z',
          rotationId: 'rot-12345',
          reencryptionStarted: true,
          estimatedReencryptionTime: 7200000,
        });

        const result = await client.rotateClusterDEK({ reason: 'test' });

        expect(result.estimatedReencryptionTime).toBe(7200000);
      });

      it('should handle metadata as array (invalid)', async () => {
        const request = {
          reason: 'test',
          metadata: ['key', 'value'],
        };

        await expect(client.rotateClusterDEK(request)).rejects.toThrow(ValidationError);
      });
    });

    describe('getReencryptionStatus additional branches', () => {
      it('should handle camelCase completedAt response', async () => {
        const response = {
          operationId: 'reenc-12345',
          state: 'completed',
          completedAt: '2025-01-17T01:00:00Z',
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getReencryptionStatus();

        expect(result.completedAt).toBe('2025-01-17T01:00:00Z');
      });

      it('should handle camelCase estimatedCompletion response', async () => {
        const response = {
          operationId: 'reenc-12345',
          state: 'running',
          estimatedCompletion: '2025-01-17T02:00:00Z',
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getReencryptionStatus();

        expect(result.estimatedCompletion).toBe('2025-01-17T02:00:00Z');
      });

      it('should handle camelCase nodeProgress response', async () => {
        const response = {
          operationId: 'reenc-12345',
          state: 'running',
          nodeProgress: [
            {
              nodeId: 1,
              nodeName: 'node-1',
              progress: 50,
              processedEntities: 500,
              totalEntities: 1000,
              throughputRate: 500,
              lastUpdatedAt: '2025-01-17T00:30:00Z',
            },
          ],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getReencryptionStatus();

        expect(result.nodeProgress[0].lastUpdatedAt).toBe('2025-01-17T00:30:00Z');
      });

      it('should handle node progress without nodeName', async () => {
        const response = {
          operationId: 'reenc-12345',
          node_progress: [
            { node_id: 1, progress: 50, processed_entities: 500, total_entities: 1000, throughput_rate: 500 },
          ],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getReencryptionStatus();

        expect(result.nodeProgress[0].nodeName).toBeUndefined();
      });

      it('should handle camelCase error entityId', async () => {
        const response = {
          operationId: 'reenc-12345',
          errors: [
            { timestamp: '2025-01-17T00:30:00Z', nodeId: 1, entityId: 'entity-456', error: 'Failed', retried: false },
          ],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getReencryptionStatus();

        expect(result.errors[0].entityId).toBe('entity-456');
      });

      it('should handle errors without entityId', async () => {
        const response = {
          operationId: 'reenc-12345',
          errors: [
            { timestamp: '2025-01-17T00:30:00Z', node_id: 1, error: 'General failure', retried: true },
          ],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getReencryptionStatus();

        expect(result.errors[0].entityId).toBeUndefined();
      });
    });

    describe('startReencryption additional branches', () => {
      it('should handle response without path', async () => {
        mockTransport.post.mockResolvedValueOnce({
          operation_id: 'reenc-12345',
          started: true,
        });

        const result = await client.startReencryption({ fromVersion: 1, toVersion: 2 });

        expect(result.path).toBeUndefined();
      });
    });

    describe('recoverDEK additional branches', () => {
      it('should handle response without warnings', async () => {
        mockTransport.post.mockResolvedValueOnce({
          success: true,
          recovered_version: 3,
          recovered_at: '2025-01-17T00:00:00Z',
          nodes_synced: 3,
          audit_id: 'audit-12345',
        });

        const result = await client.recoverDEK({
          recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
          reason: 'test',
          recoveryKeys: ['key1', 'key2', 'key3'],
        });

        expect(result.warnings).toBeUndefined();
      });

      it('should handle recoveryKeys with only whitespace (invalid)', async () => {
        const request = {
          recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
          reason: 'test',
          recoveryKeys: ['key1', '   ', 'key3'],
        };

        await expect(client.recoverDEK(request)).rejects.toThrow(ValidationError);
      });
    });

    describe('getNodeDEKStatus additional branches', () => {
      it('should handle camelCase nodeName in response', async () => {
        const response = {
          nodeId: 1,
          nodeName: 'node-alpha',
          currentVersion: 3,
          lastSyncedAt: '2025-01-17T00:00:00Z',
          status: 'synced',
          versionsAvailable: [1, 2, 3],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getNodeDEKStatus(1);

        expect(result.nodeName).toBe('node-alpha');
      });
    });

    describe('initializeClusterDEK default response values', () => {
      it('should default version to 1 when missing from response', async () => {
        mockTransport.post.mockResolvedValueOnce({
          created_at: '2025-01-15T00:00:00Z',
          nodes_synced: 3,
          algorithm: 'AES-256-GCM',
          key_size: 256,
        });

        const result = await client.initializeClusterDEK({
          algorithm: 'AES-256-GCM',
          keySize: 256,
        });

        expect(result.version).toBe(1);
      });

      it('should default createdAt to empty string when missing from response', async () => {
        mockTransport.post.mockResolvedValueOnce({
          version: 1,
          nodes_synced: 3,
          algorithm: 'AES-256-GCM',
          key_size: 256,
        });

        const result = await client.initializeClusterDEK({
          algorithm: 'AES-256-GCM',
          keySize: 256,
        });

        expect(result.createdAt).toBe('');
      });

      it('should handle empty response with all defaults', async () => {
        mockTransport.post.mockResolvedValueOnce({});

        const result = await client.initializeClusterDEK({
          algorithm: 'AES-256-GCM',
          keySize: 256,
        });

        expect(result.version).toBe(1);
        expect(result.createdAt).toBe('');
        expect(result.nodesSynced).toBe(0);
        expect(result.algorithm).toBe('');
        expect(result.keySize).toBe(0);
      });
    });

    describe('getClusterDEKStatus reencryptionProgress branch', () => {
      it('should not include reencryptionProgress when reencryptionInProgress is false', async () => {
        const response = {
          initialized: true,
          currentVersion: 2,
          reencryption_in_progress: false,
          reencryption_progress: 50,
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.reencryptionInProgress).toBe(false);
        expect(result.reencryptionProgress).toBeUndefined();
      });

      it('should default reencryptionProgress to 0 when missing but reencryptionInProgress is true', async () => {
        const response = {
          initialized: true,
          currentVersion: 2,
          reencryption_in_progress: true,
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.reencryptionInProgress).toBe(true);
        expect(result.reencryptionProgress).toBe(0);
      });
    });

    describe('transformNodeDEKInfo default values', () => {
      it('should handle nodes with all default values in nodesOutOfSync', async () => {
        const response = {
          initialized: true,
          nodes_out_of_sync: [{}],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.nodesOutOfSync[0].nodeId).toBe(0);
        expect(result.nodesOutOfSync[0].currentVersion).toBe(0);
        expect(result.nodesOutOfSync[0].lastSyncedAt).toBe('');
        expect(result.nodesOutOfSync[0].nodeName).toBeUndefined();
      });
    });

    describe('transformDEKVersionInfo default values', () => {
      it('should handle previous versions with all default values', async () => {
        const response = {
          initialized: true,
          previous_versions: [{}],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getClusterDEKStatus();

        expect(result.previousVersions[0].version).toBe(0);
        expect(result.previousVersions[0].createdAt).toBe('');
        expect(result.previousVersions[0].entitiesEncrypted).toBe(0);
        expect(result.previousVersions[0].canDelete).toBe(false);
        expect(result.previousVersions[0].deprecatedAt).toBeUndefined();
      });
    });

    describe('rotateClusterDEK default response values', () => {
      it('should handle response with all default values', async () => {
        mockTransport.post.mockResolvedValueOnce({});

        const result = await client.rotateClusterDEK({ reason: 'test' });

        expect(result.newVersion).toBe(0);
        expect(result.oldVersion).toBe(0);
        expect(result.rotatedAt).toBe('');
        expect(result.rotationId).toBe('');
        expect(result.nodesSynced).toBe(0);
        expect(result.reencryptionStarted).toBe(false);
        expect(result.estimatedReencryptionTime).toBeUndefined();
      });
    });

    describe('syncNodeDEK default response values', () => {
      it('should handle response with all default values', async () => {
        mockTransport.post.mockResolvedValueOnce({});

        const result = await client.syncNodeDEK(1);

        expect(result.nodeId).toBe(0);
        expect(result.versionsSynced).toEqual([]);
        expect(result.currentVersion).toBe(0);
        expect(result.syncedAt).toBe('');
      });
    });

    describe('startReencryption default response values', () => {
      it('should handle response with all default values', async () => {
        mockTransport.post.mockResolvedValueOnce({});

        const result = await client.startReencryption({ fromVersion: 1, toVersion: 2 });

        expect(result.operationId).toBe('');
        expect(result.started).toBe(false);
        expect(result.estimatedItems).toBe(0);
        expect(result.timestamp).toBe('');
        expect(result.startedAt).toBe('');
        expect(result.totalEntities).toBe(0);
        expect(result.estimatedCompletion).toBe('');
        expect(result.path).toBeUndefined();
      });
    });

    describe('transformNodeReencryptionProgress default values', () => {
      it('should handle node progress with all default values', async () => {
        const response = {
          operation_id: 'reenc-12345',
          state: 'running',
          node_progress: [{}],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getReencryptionStatus();

        expect(result.nodeProgress[0].nodeId).toBe(0);
        expect(result.nodeProgress[0].progress).toBe(0);
        expect(result.nodeProgress[0].processedEntities).toBe(0);
        expect(result.nodeProgress[0].totalEntities).toBe(0);
        expect(result.nodeProgress[0].throughputRate).toBe(0);
        expect(result.nodeProgress[0].lastUpdatedAt).toBe('');
        expect(result.nodeProgress[0].nodeName).toBeUndefined();
      });
    });

    describe('transformReencryptionError default values', () => {
      it('should handle errors with all default values', async () => {
        const response = {
          operation_id: 'reenc-12345',
          state: 'failed',
          errors: [{}],
        };
        mockTransport.get.mockResolvedValueOnce(response);

        const result = await client.getReencryptionStatus();

        expect(result.errors[0].timestamp).toBe('');
        expect(result.errors[0].nodeId).toBe(0);
        expect(result.errors[0].error).toBe('');
        expect(result.errors[0].retried).toBe(false);
        expect(result.errors[0].entityId).toBeUndefined();
      });
    });
  });
});
