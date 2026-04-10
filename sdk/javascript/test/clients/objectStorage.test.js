/**
 * ObjectStorageClient Tests
 *
 * Comprehensive tests for the ObjectStorageClient covering all public methods,
 * success cases, error handling, and edge cases.
 */

import ObjectStorageClient, {
  ReplicationMode,
  ObjectStatus,
  StorageTier,
} from '../../src/clients/objectStorage.js';
import { ValidationError } from '../../src/errors/index.js';

describe('ObjectStorageClient', () => {
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
    client = new ObjectStorageClient(mockTransport);
  });

  // ==========================================================================
  // Constructor Tests
  // ==========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const newClient = new ObjectStorageClient(mockTransport);
      expect(newClient).toBeInstanceOf(ObjectStorageClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new ObjectStorageClient()).toThrow(ValidationError);
      expect(() => new ObjectStorageClient(null)).toThrow(ValidationError);
      expect(() => new ObjectStorageClient(undefined)).toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // putObject Tests
  // ==========================================================================

  describe('putObject', () => {
    it('should store an object with minimal parameters', async () => {
      const mockResponse = {
        key: 'my-namespace:file.txt',
        objectId: 12345,
        checksum: 'abc123',
        size: 1024,
        backend: 'primary',
        version: 1,
        createdAt: 1704067200000000000, // Unix nanoseconds
      };
      mockTransport.put.mockResolvedValueOnce(mockResponse);

      const result = await client.putObject('my-namespace:file.txt', 'file content');

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/objects/my-namespace%3Afile.txt',
        { value: 'file content' }
      );
      expect(result.key).toBe('my-namespace:file.txt');
      expect(result.objectId).toBe(12345);
      expect(result.checksum).toBe('abc123');
      expect(result.size).toBe(1024);
      expect(result.backend).toBe('primary');
      expect(result.version).toBe(1);
    });

    it('should store an object with all options', async () => {
      const mockResponse = {
        key: 'my-bucket:data.json',
        objectId: 99999,
        checksum: 'xyz789',
        size: 2048,
        backend: 's3-primary',
        version: 1,
        createdAt: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.put.mockResolvedValueOnce(mockResponse);

      const options = {
        contentType: 'application/json',
        tenantId: 'tenant-123',
        tags: { environment: 'production', team: 'platform' },
        replicationPolicy: {
          enabled: true,
          minReplicas: 3,
          targetStores: ['s3-backup', 'gcs-archive'],
          mode: ReplicationMode.SYNC,
          priority: 5,
        },
        archivePolicy: {
          enabled: true,
          archiveAfterDays: 30,
          targetStore: 'glacier',
          deleteFromPrimary: false,
          compressionLevel: 6,
        },
        retentionPolicy: {
          enabled: true,
          retentionDays: 365,
          maxVersions: 5,
        },
        targetBackend: 's3-primary',
        checksum: new Uint8Array([1, 2, 3, 4]),
      };

      const result = await client.putObject('my-bucket:data.json', { foo: 'bar' }, options);

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/objects/my-bucket%3Adata.json',
        expect.objectContaining({
          value: { foo: 'bar' },
          contentType: 'application/json',
          tenantId: 'tenant-123',
          tags: { environment: 'production', team: 'platform' },
          replicationPolicy: options.replicationPolicy,
          archivePolicy: options.archivePolicy,
          retentionPolicy: options.retentionPolicy,
          targetBackend: 's3-primary',
        })
      );
      expect(result.key).toBe('my-bucket:data.json');
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.putObject('', 'data')).rejects.toThrow(ValidationError);
      await expect(client.putObject(null, 'data')).rejects.toThrow(ValidationError);
      await expect(client.putObject(undefined, 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is missing', async () => {
      await expect(client.putObject('key', null)).rejects.toThrow(ValidationError);
      await expect(client.putObject('key', undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when tags is not an object', async () => {
      await expect(
        client.putObject('key', 'data', { tags: 'invalid' })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.putObject('key', 'data', { tags: ['array'] })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid replication mode', async () => {
      await expect(
        client.putObject('key', 'data', {
          replicationPolicy: { mode: 'invalid-mode' },
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid replication priority', async () => {
      await expect(
        client.putObject('key', 'data', {
          replicationPolicy: { priority: 0 },
        })
      ).rejects.toThrow(ValidationError);

      await expect(
        client.putObject('key', 'data', {
          replicationPolicy: { priority: 11 },
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should handle checksum as array of numbers', async () => {
      mockTransport.put.mockResolvedValueOnce({ key: 'test' });

      await client.putObject('test', 'data', { checksum: [1, 2, 3, 4] });

      expect(mockTransport.put).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          checksum: expect.any(String),
        })
      );
    });

    it('should handle checksum as string (already encoded)', async () => {
      mockTransport.put.mockResolvedValueOnce({ key: 'test' });

      await client.putObject('test', 'data', { checksum: 'already-base64-encoded-string' });

      expect(mockTransport.put).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          checksum: 'already-base64-encoded-string',
        })
      );
    });

    it('should handle response with missing fields', async () => {
      mockTransport.put.mockResolvedValueOnce({});

      const result = await client.putObject('test-key', 'data');

      expect(result.key).toBe('test-key');
      expect(result.objectId).toBe(0);
      expect(result.checksum).toBe('');
      expect(result.size).toBe(0);
      expect(result.backend).toBe('');
      expect(result.version).toBe(1);
      expect(result.createdAt).toBeDefined();
    });
  });

  // ==========================================================================
  // getObject Tests
  // ==========================================================================

  describe('getObject', () => {
    it('should retrieve an object by key', async () => {
      const mockResponse = {
        key: 'my-key',
        value: { data: 'content' },
        contentType: 'application/json',
        size: 512,
        checksum: 'sha256hash',
        version: 3,
        tags: { env: 'test' },
        tenantId: 'tenant-1',
        createdAt: '2024-01-01T00:00:00.000Z',
        modifiedAt: '2024-01-02T00:00:00.000Z',
        backend: 's3-primary',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getObject('my-key');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/my-key',
        { query: {} }
      );
      expect(result.key).toBe('my-key');
      expect(result.value).toEqual({ data: 'content' });
      expect(result.contentType).toBe('application/json');
      expect(result.size).toBe(512);
      expect(result.version).toBe(3);
      expect(result.tags).toEqual({ env: 'test' });
    });

    it('should retrieve object with tenant filter', async () => {
      mockTransport.get.mockResolvedValueOnce({ key: 'my-key', value: 'data' });

      await client.getObject('my-key', { tenantId: 'tenant-123' });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/my-key',
        { query: { tenant_id: 'tenant-123' } }
      );
    });

    it('should retrieve object without metadata', async () => {
      mockTransport.get.mockResolvedValueOnce({ key: 'my-key', value: 'data' });

      await client.getObject('my-key', { includeMetadata: false });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/my-key',
        { query: { include_metadata: false } }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.getObject('')).rejects.toThrow(ValidationError);
      await expect(client.getObject(null)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getObject('test-key');

      expect(result.key).toBe('test-key');
      expect(result.value).toBeNull();
      expect(result.contentType).toBe('');
      expect(result.size).toBe(0);
      expect(result.checksum).toBe('');
      expect(result.version).toBe(0);
      expect(result.tags).toEqual({});
      expect(result.tenantId).toBe('');
    });

    it('should properly encode special characters in key', async () => {
      mockTransport.get.mockResolvedValueOnce({ key: 'bucket:path/to/file.txt' });

      await client.getObject('bucket:path/to/file.txt');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/bucket%3Apath%2Fto%2Ffile.txt',
        expect.any(Object)
      );
    });
  });

  // ==========================================================================
  // deleteObject Tests
  // ==========================================================================

  describe('deleteObject', () => {
    it('should delete an object by key', async () => {
      const mockResponse = {
        key: 'my-key',
        success: true,
        size: 1024,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.delete.mockResolvedValueOnce(mockResponse);

      const result = await client.deleteObject('my-key');

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/objects/my-key',
        { query: {} }
      );
      expect(result.key).toBe('my-key');
      expect(result.success).toBe(true);
      expect(result.size).toBe(1024);
    });

    it('should delete object with tenant filter', async () => {
      mockTransport.delete.mockResolvedValueOnce({ success: true });

      await client.deleteObject('my-key', { tenantId: 'tenant-123' });

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/objects/my-key',
        { query: { tenant_id: 'tenant-123' } }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.deleteObject('')).rejects.toThrow(ValidationError);
      await expect(client.deleteObject(null)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      const result = await client.deleteObject('test-key');

      expect(result.key).toBe('test-key');
      expect(result.success).toBe(true);
      expect(result.size).toBe(0);
      expect(result.timestamp).toBeDefined();
    });
  });

  // ==========================================================================
  // headObject Tests
  // ==========================================================================

  describe('headObject', () => {
    it('should get object metadata', async () => {
      const mockResponse = {
        key: 'my-key',
        exists: true,
        contentType: 'text/plain',
        size: 256,
        checksum: 'abc123',
        version: 2,
        tags: { category: 'logs' },
        tenantId: 'tenant-1',
        status: ObjectStatus.ACTIVE,
        createdAt: '2024-01-01T00:00:00.000Z',
        modifiedAt: '2024-01-02T00:00:00.000Z',
        lastAccessAt: '2024-01-03T00:00:00.000Z',
        backend: 'primary',
        replicaCount: 3,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.headObject('my-key');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/head',
        { query: {} }
      );
      expect(result.key).toBe('my-key');
      expect(result.exists).toBe(true);
      expect(result.contentType).toBe('text/plain');
      expect(result.size).toBe(256);
      expect(result.version).toBe(2);
      expect(result.status).toBe(ObjectStatus.ACTIVE);
      expect(result.replicaCount).toBe(3);
    });

    it('should get object metadata with tenant filter', async () => {
      mockTransport.get.mockResolvedValueOnce({ exists: true });

      await client.headObject('my-key', { tenantId: 'tenant-123' });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/head',
        { query: { tenant_id: 'tenant-123' } }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.headObject('')).rejects.toThrow(ValidationError);
      await expect(client.headObject(null)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.headObject('test-key');

      expect(result.key).toBe('test-key');
      expect(result.exists).toBe(false);
      expect(result.contentType).toBe('');
      expect(result.size).toBe(0);
      expect(result.version).toBe(0);
      expect(result.tags).toEqual({});
      expect(result.status).toBe(ObjectStatus.ACTIVE);
      expect(result.replicaCount).toBe(0);
    });
  });

  // ==========================================================================
  // listObjects Tests
  // ==========================================================================

  describe('listObjects', () => {
    it('should list objects with default options', async () => {
      const mockResponse = {
        objects: [
          { key: 'obj1', size: 100, contentType: 'text/plain', version: 1, status: 'active' },
          { key: 'obj2', size: 200, contentType: 'application/json', version: 2, status: 'active' },
        ],
        nextCursor: 'cursor123',
        hasMore: true,
        totalCount: 100,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listObjects();

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects',
        { query: {} }
      );
      expect(result.objects).toHaveLength(2);
      expect(result.objects[0].key).toBe('obj1');
      expect(result.nextCursor).toBe('cursor123');
      expect(result.hasMore).toBe(true);
      expect(result.totalCount).toBe(100);
    });

    it('should list objects with all options', async () => {
      mockTransport.get.mockResolvedValueOnce({ objects: [] });

      await client.listObjects({
        prefix: 'logs/',
        tenantId: 'tenant-123',
        limit: 50,
        cursor: 'prev-cursor',
        includeTags: true,
      });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects',
        {
          query: {
            prefix: 'logs/',
            tenant_id: 'tenant-123',
            limit: 50,
            cursor: 'prev-cursor',
            include_tags: true,
          },
        }
      );
    });

    it('should throw ValidationError for invalid limit', async () => {
      await expect(client.listObjects({ limit: 0 })).rejects.toThrow(ValidationError);
      await expect(client.listObjects({ limit: -1 })).rejects.toThrow(ValidationError);
      await expect(client.listObjects({ limit: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listObjects();

      expect(result.objects).toEqual([]);
      expect(result.nextCursor).toBeNull();
      expect(result.hasMore).toBe(false);
      expect(result.totalCount).toBe(0);
    });

    it('should handle response with null objects array', async () => {
      mockTransport.get.mockResolvedValueOnce({ objects: null });

      const result = await client.listObjects();

      expect(result.objects).toEqual([]);
    });
  });

  // ==========================================================================
  // listByTag Tests
  // ==========================================================================

  describe('listByTag', () => {
    it('should list objects by tag', async () => {
      const mockResponse = {
        objects: [{ key: 'tagged-obj', size: 100 }],
        nextCursor: null,
        hasMore: false,
        totalCount: 1,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listByTag('environment', 'production');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/by-tag',
        {
          query: {
            tag_name: 'environment',
            tag_value: 'production',
          },
        }
      );
      expect(result.objects).toHaveLength(1);
    });

    it('should list objects by tag with options', async () => {
      mockTransport.get.mockResolvedValueOnce({ objects: [] });

      await client.listByTag('team', 'platform', {
        tenantId: 'tenant-123',
        limit: 25,
        cursor: 'abc',
      });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/by-tag',
        {
          query: {
            tag_name: 'team',
            tag_value: 'platform',
            tenant_id: 'tenant-123',
            limit: 25,
            cursor: 'abc',
          },
        }
      );
    });

    it('should throw ValidationError when tagName is missing', async () => {
      await expect(client.listByTag('', 'value')).rejects.toThrow(ValidationError);
      await expect(client.listByTag(null, 'value')).rejects.toThrow(ValidationError);
    });

    it('should handle null tag value', async () => {
      mockTransport.get.mockResolvedValueOnce({ objects: [] });

      await client.listByTag('exists', null);

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/by-tag',
        { query: { tag_name: 'exists' } }
      );
    });

    it('should handle undefined tag value', async () => {
      mockTransport.get.mockResolvedValueOnce({ objects: [] });

      await client.listByTag('exists', undefined);

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/by-tag',
        { query: { tag_name: 'exists' } }
      );
    });
  });

  // ==========================================================================
  // listByTenant Tests
  // ==========================================================================

  describe('listByTenant', () => {
    it('should list objects by tenant', async () => {
      const mockResponse = {
        objects: [{ key: 'tenant-obj', size: 100 }],
        nextCursor: null,
        hasMore: false,
        totalCount: 1,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listByTenant('tenant-123');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/by-tenant/tenant-123',
        { query: {} }
      );
      expect(result.objects).toHaveLength(1);
    });

    it('should list objects by tenant with options', async () => {
      mockTransport.get.mockResolvedValueOnce({ objects: [] });

      await client.listByTenant('tenant-123', {
        limit: 100,
        cursor: 'next-page',
        includeTags: true,
      });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/by-tenant/tenant-123',
        {
          query: {
            limit: 100,
            cursor: 'next-page',
            include_tags: true,
          },
        }
      );
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.listByTenant('')).rejects.toThrow(ValidationError);
      await expect(client.listByTenant(null)).rejects.toThrow(ValidationError);
    });

    it('should properly encode special characters in tenantId', async () => {
      mockTransport.get.mockResolvedValueOnce({ objects: [] });

      await client.listByTenant('org/team/user');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/by-tenant/org%2Fteam%2Fuser',
        expect.any(Object)
      );
    });
  });

  // ==========================================================================
  // copyObject Tests
  // ==========================================================================

  describe('copyObject', () => {
    it('should copy an object to new location', async () => {
      const mockResponse = {
        key: 'dest-key',
        objectId: 54321,
        size: 1024,
        checksum: 'newchecksum',
        version: 1,
        backend: 'primary',
        createdAt: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.copyObject('source-key', 'dest-key');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/source-key/copy',
        { destinationKey: 'dest-key' }
      );
      expect(result.key).toBe('dest-key');
      expect(result.objectId).toBe(54321);
    });

    it('should copy object with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ key: 'dest-key' });

      await client.copyObject('source-key', 'dest-key', {
        tenantId: 'tenant-123',
        overwriteExisting: true,
        targetBackend: 'backup-store',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/source-key/copy',
        {
          destinationKey: 'dest-key',
          tenantId: 'tenant-123',
          overwriteExisting: true,
          targetBackend: 'backup-store',
        }
      );
    });

    it('should throw ValidationError when sourceKey is missing', async () => {
      await expect(client.copyObject('', 'dest')).rejects.toThrow(ValidationError);
      await expect(client.copyObject(null, 'dest')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when destKey is missing', async () => {
      await expect(client.copyObject('source', '')).rejects.toThrow(ValidationError);
      await expect(client.copyObject('source', null)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.copyObject('source', 'dest');

      expect(result.key).toBe('dest');
      expect(result.objectId).toBe(0);
      expect(result.size).toBe(0);
      expect(result.checksum).toBe('');
      expect(result.version).toBe(1);
    });
  });

  // ==========================================================================
  // replicateObject Tests
  // ==========================================================================

  describe('replicateObject', () => {
    it('should replicate object to targets', async () => {
      const mockResponse = {
        key: 'my-key',
        status: 'completed',
        results: {
          's3-backup': { target: 's3-backup', success: true, duration: 150 },
          'gcs-archive': { target: 'gcs-archive', success: true, duration: 200 },
        },
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.replicateObject('my-key', ['s3-backup', 'gcs-archive']);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/replicate',
        { targets: ['s3-backup', 'gcs-archive'] }
      );
      expect(result.key).toBe('my-key');
      expect(result.status).toBe('completed');
      expect(result.results['s3-backup'].success).toBe(true);
    });

    it('should replicate object with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ key: 'my-key', results: {} });

      await client.replicateObject('my-key', ['target1'], {
        tenantId: 'tenant-123',
        priority: 7,
        synchronous: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/replicate',
        {
          targets: ['target1'],
          tenantId: 'tenant-123',
          priority: 7,
          synchronous: true,
        }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.replicateObject('', ['target'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when targets is empty', async () => {
      await expect(client.replicateObject('key', [])).rejects.toThrow(ValidationError);
      await expect(client.replicateObject('key', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid priority', async () => {
      await expect(
        client.replicateObject('key', ['target'], { priority: 0 })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.replicateObject('key', ['target'], { priority: 11 })
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.replicateObject('key', ['target']);

      expect(result.key).toBe('key');
      expect(result.status).toBe('');
      expect(result.results).toEqual({});
    });

    it('should handle failed replication results', async () => {
      const mockResponse = {
        key: 'my-key',
        status: 'partial',
        results: {
          'target1': { success: false, error: 'Connection timeout', duration: 5000 },
        },
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.replicateObject('my-key', ['target1']);

      expect(result.results['target1'].success).toBe(false);
      expect(result.results['target1'].error).toBe('Connection timeout');
    });
  });

  // ==========================================================================
  // migrateObject Tests
  // ==========================================================================

  describe('migrateObject', () => {
    it('should migrate object to target backend', async () => {
      const mockResponse = {
        key: 'my-key',
        sourceBackend: 'primary',
        targetBackend: 'archive',
        success: true,
        deletedFromSource: true,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.migrateObject('my-key', 'archive');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/migrate',
        { targetBackend: 'archive' }
      );
      expect(result.key).toBe('my-key');
      expect(result.sourceBackend).toBe('primary');
      expect(result.targetBackend).toBe('archive');
      expect(result.success).toBe(true);
    });

    it('should migrate object with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      await client.migrateObject('my-key', 'archive', {
        tenantId: 'tenant-123',
        deleteFromSource: false,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/migrate',
        {
          targetBackend: 'archive',
          tenantId: 'tenant-123',
          deleteFromSource: false,
        }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.migrateObject('', 'archive')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when targetBackend is missing', async () => {
      await expect(client.migrateObject('key', '')).rejects.toThrow(ValidationError);
      await expect(client.migrateObject('key', null)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.migrateObject('key', 'target');

      expect(result.key).toBe('key');
      expect(result.sourceBackend).toBe('');
      expect(result.targetBackend).toBe('target');
      expect(result.success).toBe(true);
      expect(result.deletedFromSource).toBe(false);
    });
  });

  // ==========================================================================
  // archiveObject Tests
  // ==========================================================================

  describe('archiveObject', () => {
    it('should archive object to archive store', async () => {
      const mockResponse = {
        key: 'my-key',
        archiveStore: 'glacier',
        success: true,
        deletedFromPrimary: false,
        archivedAt: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.archiveObject('my-key', 'glacier');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/archive',
        { targetStore: 'glacier' }
      );
      expect(result.key).toBe('my-key');
      expect(result.archiveStore).toBe('glacier');
      expect(result.success).toBe(true);
    });

    it('should archive object with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      await client.archiveObject('my-key', 'glacier', {
        tenantId: 'tenant-123',
        deleteFromPrimary: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/archive',
        {
          targetStore: 'glacier',
          tenantId: 'tenant-123',
          deleteFromPrimary: true,
        }
      );
    });

    it('should archive object without specifying archive store', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true, archiveStore: 'default-archive' });

      const result = await client.archiveObject('my-key', '');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/archive',
        {}
      );
      expect(result.archiveStore).toBe('default-archive');
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.archiveObject('', 'glacier')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.archiveObject('key', '');

      expect(result.key).toBe('key');
      expect(result.archiveStore).toBe('');
      expect(result.success).toBe(true);
      expect(result.deletedFromPrimary).toBe(false);
    });
  });

  // ==========================================================================
  // restoreObject Tests
  // ==========================================================================

  describe('restoreObject', () => {
    it('should restore archived object', async () => {
      const mockResponse = {
        key: 'my-key',
        backend: 'primary',
        success: true,
        restoredAt: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.restoreObject('my-key');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/restore',
        {}
      );
      expect(result.key).toBe('my-key');
      expect(result.backend).toBe('primary');
      expect(result.success).toBe(true);
    });

    it('should restore object with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      await client.restoreObject('my-key', {
        tenantId: 'tenant-123',
        targetBackend: 'hot-storage',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/restore',
        {
          tenantId: 'tenant-123',
          targetBackend: 'hot-storage',
        }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.restoreObject('')).rejects.toThrow(ValidationError);
      await expect(client.restoreObject(null)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.restoreObject('key');

      expect(result.key).toBe('key');
      expect(result.backend).toBe('');
      expect(result.success).toBe(true);
    });
  });

  // ==========================================================================
  // verifyObject Tests
  // ==========================================================================

  describe('verifyObject', () => {
    it('should verify object integrity', async () => {
      const mockResponse = {
        key: 'my-key',
        valid: true,
        results: {
          'primary': { store: 'primary', valid: true, checksumMatch: true, sizeMatch: true },
          'backup': { store: 'backup', valid: true, checksumMatch: true, sizeMatch: true },
        },
        repaired: false,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.verifyObject('my-key');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/verify',
        {}
      );
      expect(result.key).toBe('my-key');
      expect(result.valid).toBe(true);
      expect(result.results['primary'].checksumMatch).toBe(true);
    });

    it('should verify object with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ valid: true, results: {} });

      await client.verifyObject('my-key', {
        stores: ['primary', 'backup'],
        tenantId: 'tenant-123',
        repairMismatches: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/verify',
        {
          stores: ['primary', 'backup'],
          tenantId: 'tenant-123',
          repairMismatches: true,
        }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.verifyObject('')).rejects.toThrow(ValidationError);
    });

    it('should handle verification failure results', async () => {
      const mockResponse = {
        key: 'my-key',
        valid: false,
        results: {
          'primary': { valid: true, checksumMatch: true, sizeMatch: true },
          'backup': { valid: false, checksumMatch: false, sizeMatch: true, error: 'Checksum mismatch' },
        },
        repaired: true,
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.verifyObject('my-key', { repairMismatches: true });

      expect(result.valid).toBe(false);
      expect(result.results['backup'].error).toBe('Checksum mismatch');
      expect(result.repaired).toBe(true);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.verifyObject('key');

      expect(result.key).toBe('key');
      expect(result.valid).toBe(false);
      expect(result.results).toEqual({});
      expect(result.repaired).toBe(false);
    });

    it('should not include empty stores array in request', async () => {
      mockTransport.post.mockResolvedValueOnce({ valid: true, results: {} });

      await client.verifyObject('my-key', { stores: [] });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/verify',
        {}
      );
    });
  });

  // ==========================================================================
  // setObjectPolicy Tests
  // ==========================================================================

  describe('setObjectPolicy', () => {
    it('should set object policy', async () => {
      const mockResponse = {
        key: 'my-key',
        success: true,
        version: 2,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.put.mockResolvedValueOnce(mockResponse);

      const policy = {
        replicationPolicy: { enabled: true, minReplicas: 3 },
        archivePolicy: { enabled: false },
      };

      const result = await client.setObjectPolicy('my-key', policy);

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/policy',
        {
          replicationPolicy: { enabled: true, minReplicas: 3 },
          archivePolicy: { enabled: false },
        }
      );
      expect(result.success).toBe(true);
      expect(result.version).toBe(2);
    });

    it('should set object policy with tenantId', async () => {
      mockTransport.put.mockResolvedValueOnce({ success: true });

      await client.setObjectPolicy('my-key', { retentionPolicy: { enabled: true } }, { tenantId: 'tenant-123' });

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/policy',
        {
          retentionPolicy: { enabled: true },
          tenantId: 'tenant-123',
        }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.setObjectPolicy('', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy is missing', async () => {
      await expect(client.setObjectPolicy('key', null)).rejects.toThrow(ValidationError);
      await expect(client.setObjectPolicy('key', undefined)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.put.mockResolvedValueOnce({});

      const result = await client.setObjectPolicy('key', {});

      expect(result.key).toBe('key');
      expect(result.success).toBe(true);
      expect(result.version).toBe(0);
    });
  });

  // ==========================================================================
  // getObjectPolicy Tests
  // ==========================================================================

  describe('getObjectPolicy', () => {
    it('should get object policy', async () => {
      const mockResponse = {
        key: 'my-key',
        replicationPolicy: { enabled: true, minReplicas: 3 },
        archivePolicy: { enabled: true, archiveAfterDays: 30 },
        retentionPolicy: null,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getObjectPolicy('my-key');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/policy',
        { query: {} }
      );
      expect(result.key).toBe('my-key');
      expect(result.replicationPolicy).toEqual({ enabled: true, minReplicas: 3 });
      expect(result.archivePolicy).toEqual({ enabled: true, archiveAfterDays: 30 });
    });

    it('should get object policy with tenantId', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getObjectPolicy('my-key', { tenantId: 'tenant-123' });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/my-key/policy',
        { query: { tenant_id: 'tenant-123' } }
      );
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(client.getObjectPolicy('')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getObjectPolicy('key');

      expect(result.key).toBe('key');
      expect(result.replicationPolicy).toBeNull();
      expect(result.archivePolicy).toBeNull();
      expect(result.retentionPolicy).toBeNull();
    });
  });

  // ==========================================================================
  // listBackends Tests
  // ==========================================================================

  describe('listBackends', () => {
    it('should list storage backends', async () => {
      const mockResponse = {
        backends: [
          {
            name: 's3-primary',
            type: 's3',
            enabled: true,
            default: true,
            tier: 'hot',
            region: 'us-east-1',
            objectCount: 1000,
            totalSize: 1048576,
          },
          {
            name: 'gcs-backup',
            type: 'gcs',
            enabled: true,
            default: false,
            tier: 'warm',
            region: 'us-central1',
            objectCount: 500,
            totalSize: 524288,
          },
        ],
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listBackends();

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/backends',
        { query: {} }
      );
      expect(result.backends).toHaveLength(2);
      expect(result.backends[0].name).toBe('s3-primary');
      expect(result.backends[0].default).toBe(true);
    });

    it('should list backends with health info', async () => {
      const mockResponse = {
        backends: [
          {
            name: 's3-primary',
            type: 's3',
            enabled: true,
            health: {
              healthy: true,
              latency: 50,
              lastCheck: '2024-01-01T00:00:00.000Z',
            },
          },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listBackends({ includeHealth: true });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/backends',
        { query: { include_health: true } }
      );
      expect(result.backends[0].health.healthy).toBe(true);
    });

    it('should list backends filtered by type', async () => {
      mockTransport.get.mockResolvedValueOnce({ backends: [] });

      await client.listBackends({ filterByType: 's3' });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/backends',
        { query: { filter_by_type: 's3' } }
      );
    });

    it('should handle empty backends response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listBackends();

      expect(result.backends).toEqual([]);
    });

    it('should handle backends with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        backends: [{}],
      });

      const result = await client.listBackends();

      expect(result.backends[0].name).toBe('');
      expect(result.backends[0].type).toBe('');
      expect(result.backends[0].enabled).toBe(false);
      expect(result.backends[0].default).toBe(false);
      expect(result.backends[0].tier).toBe(StorageTier.HOT);
    });
  });

  // ==========================================================================
  // getBackendHealth Tests
  // ==========================================================================

  describe('getBackendHealth', () => {
    it('should get backend health status', async () => {
      const mockResponse = {
        name: 's3-primary',
        healthy: true,
        latency: 45,
        lastCheck: '2024-01-01T00:00:00.000Z',
        status: 'operational',
        error: null,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getBackendHealth('s3-primary');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/backends/s3-primary/health'
      );
      expect(result.name).toBe('s3-primary');
      expect(result.healthy).toBe(true);
      expect(result.latency).toBe(45);
      expect(result.status).toBe('operational');
    });

    it('should throw ValidationError when backendName is missing', async () => {
      await expect(client.getBackendHealth('')).rejects.toThrow(ValidationError);
      await expect(client.getBackendHealth(null)).rejects.toThrow(ValidationError);
    });

    it('should handle unhealthy backend response', async () => {
      const mockResponse = {
        name: 's3-primary',
        healthy: false,
        latency: 0,
        lastCheck: '2024-01-01T00:00:00.000Z',
        status: 'degraded',
        error: 'Connection timeout',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getBackendHealth('s3-primary');

      expect(result.healthy).toBe(false);
      expect(result.error).toBe('Connection timeout');
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getBackendHealth('test-backend');

      expect(result.name).toBe('test-backend');
      expect(result.healthy).toBe(false);
      expect(result.latency).toBe(0);
      expect(result.status).toBe('');
      expect(result.error).toBeNull();
    });
  });

  // ==========================================================================
  // getTenantUsage Tests
  // ==========================================================================

  describe('getTenantUsage', () => {
    it('should get tenant usage statistics', async () => {
      const mockResponse = {
        tenantId: 'tenant-123',
        objectCount: 500,
        totalSize: 10485760,
        activeCount: 450,
        archivedCount: 50,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getTenantUsage('tenant-123');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/tenants/tenant-123/usage'
      );
      expect(result.tenantId).toBe('tenant-123');
      expect(result.objectCount).toBe(500);
      expect(result.totalSize).toBe(10485760);
      expect(result.activeCount).toBe(450);
      expect(result.archivedCount).toBe(50);
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.getTenantUsage('')).rejects.toThrow(ValidationError);
      await expect(client.getTenantUsage(null)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getTenantUsage('tenant-123');

      expect(result.tenantId).toBe('tenant-123');
      expect(result.objectCount).toBe(0);
      expect(result.totalSize).toBe(0);
      expect(result.activeCount).toBe(0);
      expect(result.archivedCount).toBe(0);
    });
  });

  // ==========================================================================
  // getTenantQuotas Tests
  // ==========================================================================

  describe('getTenantQuotas', () => {
    it('should get tenant quota settings', async () => {
      const mockResponse = {
        tenantId: 'tenant-123',
        maxStorageBytes: 1073741824,
        maxObjectSize: 104857600,
        maxObjectCount: 10000,
        usedStorageBytes: 536870912,
        usedObjectCount: 5000,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getTenantQuotas('tenant-123');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/tenants/tenant-123/quotas'
      );
      expect(result.tenantId).toBe('tenant-123');
      expect(result.maxStorageBytes).toBe(1073741824);
      expect(result.maxObjectSize).toBe(104857600);
      expect(result.maxObjectCount).toBe(10000);
      expect(result.usedStorageBytes).toBe(536870912);
      expect(result.usedObjectCount).toBe(5000);
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.getTenantQuotas('')).rejects.toThrow(ValidationError);
      await expect(client.getTenantQuotas(null)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getTenantQuotas('tenant-123');

      expect(result.tenantId).toBe('tenant-123');
      expect(result.maxStorageBytes).toBe(0);
      expect(result.maxObjectSize).toBe(0);
      expect(result.maxObjectCount).toBe(0);
      expect(result.usedStorageBytes).toBe(0);
      expect(result.usedObjectCount).toBe(0);
    });
  });

  // ==========================================================================
  // Enum Exports Tests
  // ==========================================================================

  describe('Enum exports', () => {
    it('should export ReplicationMode enum', () => {
      expect(ReplicationMode.SYNC).toBe('sync');
      expect(ReplicationMode.ASYNC).toBe('async');
      expect(ReplicationMode.QUORUM).toBe('quorum');
      expect(Object.isFrozen(ReplicationMode)).toBe(true);
    });

    it('should export ObjectStatus enum', () => {
      expect(ObjectStatus.ACTIVE).toBe('active');
      expect(ObjectStatus.ARCHIVED).toBe('archived');
      expect(ObjectStatus.MIGRATING).toBe('migrating');
      expect(ObjectStatus.PENDING_DELETE).toBe('pending_delete');
      expect(Object.isFrozen(ObjectStatus)).toBe(true);
    });

    it('should export StorageTier enum', () => {
      expect(StorageTier.HOT).toBe('hot');
      expect(StorageTier.WARM).toBe('warm');
      expect(StorageTier.COLD).toBe('cold');
      expect(Object.isFrozen(StorageTier)).toBe(true);
    });
  });
});
