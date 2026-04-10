/**
 * OperationsClient Tests
 *
 * Comprehensive tests for the QuicBaaS operations management client.
 */

import OperationsClient, {
  MigrationStatus,
  UpgradeStatus,
  ReplaceNodeStatus,
} from '../../src/clients/operations.js';
import { ValidationError } from '../../src/errors/index.js';

describe('OperationsClient', () => {
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
    client = new OperationsClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const c = new OperationsClient(mockTransport);
      expect(c).toBeInstanceOf(OperationsClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new OperationsClient()).toThrow(ValidationError);
      expect(() => new OperationsClient(null)).toThrow(ValidationError);
      expect(() => new OperationsClient(undefined)).toThrow(ValidationError);
    });
  });

  describe('migrateData', () => {
    it('should start migration with minimum required parameters', async () => {
      const mockResponse = {
        migrationId: 'mig-123',
        status: 'pending',
        estimatedDuration: 3600,
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.migrateData({
        sourceShard: 1,
        targetShard: 2,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/operations/migrate', {
        sourceShard: 1,
        targetShard: 2,
      });
      expect(result.migrationId).toBe('mig-123');
      expect(result.status).toBe('pending');
      expect(result.estimatedDuration).toBe(3600);
    });

    it('should start migration with entityTypes', async () => {
      mockTransport.post.mockResolvedValueOnce({ migrationId: 'mig-123' });

      await client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        entityTypes: ['users', 'orders'],
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/operations/migrate', {
        sourceShard: 1,
        targetShard: 2,
        entityTypes: ['users', 'orders'],
      });
    });

    it('should start migration with concurrency', async () => {
      mockTransport.post.mockResolvedValueOnce({ migrationId: 'mig-123' });

      await client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        concurrency: 4,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/operations/migrate', {
        sourceShard: 1,
        targetShard: 2,
        concurrency: 4,
      });
    });

    it('should start migration with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ migrationId: 'mig-123' });

      await client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        entityTypes: ['users', 'orders'],
        concurrency: 4,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/operations/migrate', {
        sourceShard: 1,
        targetShard: 2,
        entityTypes: ['users', 'orders'],
        concurrency: 4,
      });
    });

    it('should not include empty entityTypes', async () => {
      mockTransport.post.mockResolvedValueOnce({ migrationId: 'mig-123' });

      await client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        entityTypes: [],
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/operations/migrate', {
        sourceShard: 1,
        targetShard: 2,
      });
    });

    it('should not include zero concurrency', async () => {
      mockTransport.post.mockResolvedValueOnce({ migrationId: 'mig-123' });

      await client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        concurrency: 0,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/operations/migrate', {
        sourceShard: 1,
        targetShard: 2,
      });
    });

    it('should throw ValidationError when request is missing', async () => {
      await expect(client.migrateData()).rejects.toThrow(ValidationError);
      await expect(client.migrateData(null)).rejects.toThrow(ValidationError);
      await expect(client.migrateData(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is not an object', async () => {
      await expect(client.migrateData('invalid')).rejects.toThrow(ValidationError);
      await expect(client.migrateData(123)).rejects.toThrow(ValidationError);
      await expect(client.migrateData([])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when sourceShard is missing', async () => {
      await expect(client.migrateData({ targetShard: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when sourceShard is not positive', async () => {
      await expect(client.migrateData({ sourceShard: 0, targetShard: 2 })).rejects.toThrow(ValidationError);
      await expect(client.migrateData({ sourceShard: -1, targetShard: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when sourceShard is not an integer', async () => {
      await expect(client.migrateData({ sourceShard: 1.5, targetShard: 2 })).rejects.toThrow(ValidationError);
      await expect(client.migrateData({ sourceShard: '1', targetShard: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when targetShard is missing', async () => {
      await expect(client.migrateData({ sourceShard: 1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when targetShard is not positive', async () => {
      await expect(client.migrateData({ sourceShard: 1, targetShard: 0 })).rejects.toThrow(ValidationError);
      await expect(client.migrateData({ sourceShard: 1, targetShard: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when source and target shards are the same', async () => {
      await expect(client.migrateData({ sourceShard: 1, targetShard: 1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityTypes is not an array', async () => {
      await expect(client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        entityTypes: 'users',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is not a non-empty string', async () => {
      await expect(client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        entityTypes: [''],
      })).rejects.toThrow(ValidationError);

      await expect(client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        entityTypes: ['users', ''],
      })).rejects.toThrow(ValidationError);

      await expect(client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        entityTypes: [123],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType contains only whitespace', async () => {
      await expect(client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        entityTypes: ['   '],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when concurrency is negative', async () => {
      await expect(client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        concurrency: -1,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when concurrency is not an integer', async () => {
      await expect(client.migrateData({
        sourceShard: 1,
        targetShard: 2,
        concurrency: 1.5,
      })).rejects.toThrow(ValidationError);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.migrateData({
        sourceShard: 1,
        targetShard: 2,
      });

      expect(result.migrationId).toBe('');
      expect(result.status).toBe(MigrationStatus.PENDING);
      expect(result.estimatedDuration).toBe(0);
    });
  });

  describe('getMigrationProgress', () => {
    it('should return migration progress', async () => {
      const mockResponse = {
        migrationId: 'mig-123',
        progress: 50,
        status: 'running',
        itemsMigrated: 500,
        totalItems: 1000,
        eta: 1705300000,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getMigrationProgress('mig-123');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/operations/migrate/mig-123');
      expect(result.migrationId).toBe('mig-123');
      expect(result.progress).toBe(50);
      expect(result.status).toBe('running');
      expect(result.itemsMigrated).toBe(500);
      expect(result.totalItems).toBe(1000);
      expect(result.eta).toBe(1705300000);
    });

    it('should URL encode migrationId with special characters', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getMigrationProgress('mig/123&456');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/operations/migrate/mig%2F123%26456');
    });

    it('should throw ValidationError when migrationId is missing', async () => {
      await expect(client.getMigrationProgress()).rejects.toThrow(ValidationError);
      await expect(client.getMigrationProgress('')).rejects.toThrow(ValidationError);
      await expect(client.getMigrationProgress(null)).rejects.toThrow(ValidationError);
    });

    it('should handle completed migration', async () => {
      const mockResponse = {
        migrationId: 'mig-123',
        progress: 100,
        status: 'completed',
        itemsMigrated: 1000,
        totalItems: 1000,
        eta: 0,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getMigrationProgress('mig-123');

      expect(result.status).toBe('completed');
      expect(result.progress).toBe(100);
    });

    it('should handle failed migration', async () => {
      const mockResponse = {
        migrationId: 'mig-123',
        progress: 30,
        status: 'failed',
        itemsMigrated: 300,
        totalItems: 1000,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getMigrationProgress('mig-123');

      expect(result.status).toBe('failed');
      expect(result.progress).toBe(30);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getMigrationProgress('mig-123');

      expect(result.migrationId).toBe('mig-123');
      expect(result.progress).toBe(0);
      expect(result.status).toBe(MigrationStatus.PENDING);
      expect(result.itemsMigrated).toBe(0);
      expect(result.totalItems).toBe(0);
      expect(result.eta).toBe(0);
    });
  });

  describe('replaceNode', () => {
    it('should replace node successfully', async () => {
      const mockResponse = {
        jobId: 'job-123',
        status: 'pending',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.replaceNode(1, 4, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/operations/nodes/replace', {
        shardId: 1,
        oldNodeId: 1,
        newNodeId: 4,
        newAddress: '192.168.1.4:5000',
      });
      expect(result.jobId).toBe('job-123');
      expect(result.status).toBe('pending');
    });

    it('should throw ValidationError when oldNodeId is missing', async () => {
      await expect(client.replaceNode(undefined, 4, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when oldNodeId is not positive', async () => {
      await expect(client.replaceNode(0, 4, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);

      await expect(client.replaceNode(-1, 4, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when oldNodeId is not an integer', async () => {
      await expect(client.replaceNode(1.5, 4, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when newNodeId is missing', async () => {
      await expect(client.replaceNode(1, undefined, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when newNodeId is not positive', async () => {
      await expect(client.replaceNode(1, 0, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);

      await expect(client.replaceNode(1, -1, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when old and new nodeIds are the same', async () => {
      await expect(client.replaceNode(1, 1, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options is missing', async () => {
      await expect(client.replaceNode(1, 4)).rejects.toThrow(ValidationError);
      await expect(client.replaceNode(1, 4, null)).rejects.toThrow(ValidationError);
      await expect(client.replaceNode(1, 4, undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options is not an object', async () => {
      await expect(client.replaceNode(1, 4, 'invalid')).rejects.toThrow(ValidationError);
      await expect(client.replaceNode(1, 4, [])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is missing', async () => {
      await expect(client.replaceNode(1, 4, {
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is not positive', async () => {
      await expect(client.replaceNode(1, 4, {
        shardId: 0,
        newAddress: '192.168.1.4:5000',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when newAddress is missing', async () => {
      await expect(client.replaceNode(1, 4, {
        shardId: 1,
      })).rejects.toThrow(ValidationError);

      await expect(client.replaceNode(1, 4, {
        shardId: 1,
        newAddress: '',
      })).rejects.toThrow(ValidationError);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.replaceNode(1, 4, {
        shardId: 1,
        newAddress: '192.168.1.4:5000',
      });

      expect(result.jobId).toBe('');
      expect(result.status).toBe(ReplaceNodeStatus.PENDING);
    });
  });

  describe('upgradeNode', () => {
    it('should start node upgrade successfully', async () => {
      const mockResponse = {
        jobId: 'upg-123',
        status: 'pending',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.upgradeNode(1, {
        targetVersion: '2.0.0',
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/operations/nodes/upgrade', {
        nodeId: 1,
        targetVersion: '2.0.0',
      });
      expect(result.jobId).toBe('upg-123');
      expect(result.status).toBe('pending');
    });

    it('should throw ValidationError when nodeId is missing', async () => {
      await expect(client.upgradeNode(undefined, {
        targetVersion: '2.0.0',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is not positive', async () => {
      await expect(client.upgradeNode(0, {
        targetVersion: '2.0.0',
      })).rejects.toThrow(ValidationError);

      await expect(client.upgradeNode(-1, {
        targetVersion: '2.0.0',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nodeId is not an integer', async () => {
      await expect(client.upgradeNode(1.5, {
        targetVersion: '2.0.0',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options is missing', async () => {
      await expect(client.upgradeNode(1)).rejects.toThrow(ValidationError);
      await expect(client.upgradeNode(1, null)).rejects.toThrow(ValidationError);
      await expect(client.upgradeNode(1, undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options is not an object', async () => {
      await expect(client.upgradeNode(1, 'invalid')).rejects.toThrow(ValidationError);
      await expect(client.upgradeNode(1, [])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when targetVersion is missing', async () => {
      await expect(client.upgradeNode(1, {})).rejects.toThrow(ValidationError);
      await expect(client.upgradeNode(1, { targetVersion: '' })).rejects.toThrow(ValidationError);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.upgradeNode(1, {
        targetVersion: '2.0.0',
      });

      expect(result.jobId).toBe('');
      expect(result.status).toBe(UpgradeStatus.PENDING);
    });
  });

  describe('getUpgradeStatus', () => {
    it('should return upgrade status', async () => {
      const mockResponse = {
        jobId: 'upg-123',
        progress: 50,
        status: 'running',
        currentVersion: '1.0.0',
        targetVersion: '2.0.0',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getUpgradeStatus('upg-123');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/operations/nodes/upgrade/upg-123');
      expect(result.jobId).toBe('upg-123');
      expect(result.progress).toBe(50);
      expect(result.status).toBe('running');
      expect(result.currentVersion).toBe('1.0.0');
      expect(result.targetVersion).toBe('2.0.0');
    });

    it('should URL encode upgradeId with special characters', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getUpgradeStatus('upg/123&456');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/operations/nodes/upgrade/upg%2F123%26456');
    });

    it('should throw ValidationError when upgradeId is missing', async () => {
      await expect(client.getUpgradeStatus()).rejects.toThrow(ValidationError);
      await expect(client.getUpgradeStatus('')).rejects.toThrow(ValidationError);
      await expect(client.getUpgradeStatus(null)).rejects.toThrow(ValidationError);
    });

    it('should handle completed upgrade', async () => {
      const mockResponse = {
        jobId: 'upg-123',
        progress: 100,
        status: 'completed',
        currentVersion: '2.0.0',
        targetVersion: '2.0.0',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getUpgradeStatus('upg-123');

      expect(result.status).toBe('completed');
      expect(result.progress).toBe(100);
    });

    it('should handle failed upgrade', async () => {
      const mockResponse = {
        jobId: 'upg-123',
        progress: 30,
        status: 'failed',
        currentVersion: '1.0.0',
        targetVersion: '2.0.0',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getUpgradeStatus('upg-123');

      expect(result.status).toBe('failed');
      expect(result.progress).toBe(30);
    });

    it('should handle rolling back upgrade', async () => {
      const mockResponse = {
        jobId: 'upg-123',
        progress: 80,
        status: 'rolling_back',
        currentVersion: '1.5.0',
        targetVersion: '2.0.0',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getUpgradeStatus('upg-123');

      expect(result.status).toBe('rolling_back');
    });

    it('should handle null response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getUpgradeStatus('upg-123');

      expect(result.jobId).toBe('upg-123');
      expect(result.progress).toBe(0);
      expect(result.status).toBe(UpgradeStatus.PENDING);
      expect(result.currentVersion).toBe('');
      expect(result.targetVersion).toBe('');
    });
  });

  describe('Exported enums', () => {
    it('should export MigrationStatus enum', () => {
      expect(MigrationStatus.PENDING).toBe('pending');
      expect(MigrationStatus.RUNNING).toBe('running');
      expect(MigrationStatus.COMPLETED).toBe('completed');
      expect(MigrationStatus.FAILED).toBe('failed');
      expect(MigrationStatus.CANCELLED).toBe('cancelled');
      expect(MigrationStatus.PAUSED).toBe('paused');
    });

    it('should export UpgradeStatus enum', () => {
      expect(UpgradeStatus.PENDING).toBe('pending');
      expect(UpgradeStatus.RUNNING).toBe('running');
      expect(UpgradeStatus.COMPLETED).toBe('completed');
      expect(UpgradeStatus.FAILED).toBe('failed');
      expect(UpgradeStatus.CANCELLED).toBe('cancelled');
      expect(UpgradeStatus.ROLLING_BACK).toBe('rolling_back');
    });

    it('should export ReplaceNodeStatus enum', () => {
      expect(ReplaceNodeStatus.PENDING).toBe('pending');
      expect(ReplaceNodeStatus.RUNNING).toBe('running');
      expect(ReplaceNodeStatus.COMPLETED).toBe('completed');
      expect(ReplaceNodeStatus.FAILED).toBe('failed');
    });

    it('should have frozen MigrationStatus enum', () => {
      expect(Object.isFrozen(MigrationStatus)).toBe(true);
    });

    it('should have frozen UpgradeStatus enum', () => {
      expect(Object.isFrozen(UpgradeStatus)).toBe(true);
    });

    it('should have frozen ReplaceNodeStatus enum', () => {
      expect(Object.isFrozen(ReplaceNodeStatus)).toBe(true);
    });
  });
});
