/**
 * ParameterStoreClient Tests
 *
 * Comprehensive tests for the ParameterStoreClient covering all public methods,
 * success cases, error handling, and edge cases.
 */

import ParameterStoreClient, { ParameterType } from '../../src/clients/parameterStore.js';
import { ValidationError } from '../../src/errors/index.js';

describe('ParameterStoreClient', () => {
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
    client = new ParameterStoreClient(mockTransport);
  });

  // ==========================================================================
  // Constructor Tests
  // ==========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const newClient = new ParameterStoreClient(mockTransport);
      expect(newClient).toBeInstanceOf(ParameterStoreClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new ParameterStoreClient()).toThrow(ValidationError);
      expect(() => new ParameterStoreClient(null)).toThrow(ValidationError);
      expect(() => new ParameterStoreClient(undefined)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport lacks request method', () => {
      expect(() => new ParameterStoreClient({})).toThrow(ValidationError);
      expect(() => new ParameterStoreClient({ get: jest.fn() })).toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // getParameter Tests
  // ==========================================================================

  describe('getParameter', () => {
    it('should get a parameter by path', async () => {
      const mockResponse = {
        parameter: {
          path: '/app/config/db/host',
          value: 'localhost',
          type: ParameterType.STRING,
          description: 'Database host',
          tags: { env: 'dev' },
          is_encrypted: false,
          audit_enabled: true,
          created_at: '2024-01-01T00:00:00.000Z',
          updated_at: '2024-01-02T00:00:00.000Z',
        },
        timestamp: '2024-01-03T00:00:00.000Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getParameter('/app/config/db/host');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/parameters/app/config/db/host',
        { query: {} }
      );
      expect(result.parameter.path).toBe('/app/config/db/host');
      expect(result.parameter.value).toBe('localhost');
      expect(result.parameter.type).toBe(ParameterType.STRING);
      expect(result.parameter.auditEnabled).toBe(true);
    });

    it('should get parameter with decryption option', async () => {
      mockTransport.get.mockResolvedValueOnce({ parameter: { path: '/secret', value: 'decrypted' } });

      await client.getParameter('/secret', { withDecryption: true });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/parameters/secret',
        { query: { with_decryption: true } }
      );
    });

    it('should get parameter without decryption', async () => {
      mockTransport.get.mockResolvedValueOnce({ parameter: { path: '/secret', value: '***' } });

      await client.getParameter('/secret', { withDecryption: false });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/parameters/secret',
        { query: { with_decryption: false } }
      );
    });

    it('should throw ValidationError when path is missing', async () => {
      await expect(client.getParameter('')).rejects.toThrow(ValidationError);
      await expect(client.getParameter(null)).rejects.toThrow(ValidationError);
      await expect(client.getParameter(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when path does not start with /', async () => {
      await expect(client.getParameter('app/config')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid path characters', async () => {
      await expect(client.getParameter('/app/config@host')).rejects.toThrow(ValidationError);
      await expect(client.getParameter('/app/config$var')).rejects.toThrow(ValidationError);
      await expect(client.getParameter('/app/config host')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for consecutive slashes', async () => {
      await expect(client.getParameter('/app//config')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for trailing slash', async () => {
      await expect(client.getParameter('/app/config/')).rejects.toThrow(ValidationError);
    });

    it('should handle response without parameter wrapper', async () => {
      mockTransport.get.mockResolvedValueOnce({
        path: '/app/config',
        value: 'direct-response',
      });

      const result = await client.getParameter('/app/config');

      expect(result.parameter.path).toBe('/app/config');
      expect(result.parameter.value).toBe('direct-response');
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getParameter('/app/config');

      expect(result.parameter.path).toBe('');
      expect(result.parameter.value).toBe('');
      expect(result.parameter.type).toBe(ParameterType.STRING);
      expect(result.parameter.isEncrypted).toBe(false);
      expect(result.timestamp).toBeDefined();
    });

    it('should properly encode path segments', async () => {
      mockTransport.get.mockResolvedValueOnce({ parameter: {} });

      await client.getParameter('/app/my-config_v2');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/parameters/app/my-config_v2',
        expect.any(Object)
      );
    });
  });

  // ==========================================================================
  // setParameter Tests
  // ==========================================================================

  describe('setParameter', () => {
    it('should set a parameter with minimal options', async () => {
      const mockResponse = {
        path: '/app/config/db/host',
        created: true,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.put.mockResolvedValueOnce(mockResponse);

      const result = await client.setParameter('/app/config/db/host', 'localhost');

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/parameters/app/config/db/host',
        { value: 'localhost' }
      );
      expect(result.path).toBe('/app/config/db/host');
      expect(result.created).toBe(true);
    });

    it('should set a parameter with all options', async () => {
      mockTransport.put.mockResolvedValueOnce({ created: true });

      await client.setParameter('/app/secrets/api-key', 'sk_live_xxx', {
        type: ParameterType.SECURE_STRING,
        description: 'Production API key',
        tags: { environment: 'production', team: 'platform' },
        overwrite: true,
        ttl: 3600,
        kmsKeyId: 'alias/my-key',
        auditEnabled: true,
      });

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/parameters/app/secrets/api-key',
        {
          value: 'sk_live_xxx',
          type: ParameterType.SECURE_STRING,
          description: 'Production API key',
          tags: { environment: 'production', team: 'platform' },
          overwrite: true,
          ttl_seconds: 3600,
          kms_key_id: 'alias/my-key',
          audit_enabled: true,
        }
      );
    });

    it('should set a string list parameter', async () => {
      mockTransport.put.mockResolvedValueOnce({ created: true });

      await client.setParameter('/app/config/origins', 'https://a.com,https://b.com', {
        type: ParameterType.STRING_LIST,
      });

      expect(mockTransport.put).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          type: ParameterType.STRING_LIST,
        })
      );
    });

    it('should throw ValidationError when path is missing', async () => {
      await expect(client.setParameter('', 'value')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is missing', async () => {
      await expect(client.setParameter('/app/config', '')).rejects.toThrow(ValidationError);
      await expect(client.setParameter('/app/config', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid type', async () => {
      await expect(
        client.setParameter('/app/config', 'value', { type: 'invalidType' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when secureString lacks kmsKeyId', async () => {
      await expect(
        client.setParameter('/app/secret', 'value', { type: ParameterType.SECURE_STRING })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string description', async () => {
      await expect(
        client.setParameter('/app/config', 'value', { description: 123 })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid tags', async () => {
      await expect(
        client.setParameter('/app/config', 'value', { tags: 'not-an-object' })
      ).rejects.toThrow(ValidationError);

      await expect(
        client.setParameter('/app/config', 'value', { tags: ['array'] })
      ).rejects.toThrow(ValidationError);

      await expect(
        client.setParameter('/app/config', 'value', { tags: { key: 123 } })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-boolean overwrite', async () => {
      await expect(
        client.setParameter('/app/config', 'value', { overwrite: 'true' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid ttl', async () => {
      await expect(
        client.setParameter('/app/config', 'value', { ttl: -1 })
      ).rejects.toThrow(ValidationError);

      await expect(
        client.setParameter('/app/config', 'value', { ttl: 'invalid' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string kmsKeyId', async () => {
      await expect(
        client.setParameter('/app/config', 'value', { kmsKeyId: 123 })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-boolean auditEnabled', async () => {
      await expect(
        client.setParameter('/app/config', 'value', { auditEnabled: 'true' })
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.put.mockResolvedValueOnce({});

      const result = await client.setParameter('/app/config', 'value');

      expect(result.path).toBe('/app/config');
      expect(result.created).toBe(false);
      expect(result.timestamp).toBeDefined();
    });

    it('should allow ttl of 0 (no expiration)', async () => {
      mockTransport.put.mockResolvedValueOnce({ created: true });

      await client.setParameter('/app/config', 'value', { ttl: 0 });

      expect(mockTransport.put).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({ ttl_seconds: 0 })
      );
    });
  });

  // ==========================================================================
  // deleteParameter Tests
  // ==========================================================================

  describe('deleteParameter', () => {
    it('should delete a parameter by path', async () => {
      const mockResponse = {
        path: '/app/config/deprecated',
        deleted: true,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.delete.mockResolvedValueOnce(mockResponse);

      const result = await client.deleteParameter('/app/config/deprecated');

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/parameters/app/config/deprecated'
      );
      expect(result.path).toBe('/app/config/deprecated');
      expect(result.deleted).toBe(true);
    });

    it('should throw ValidationError when path is missing', async () => {
      await expect(client.deleteParameter('')).rejects.toThrow(ValidationError);
      await expect(client.deleteParameter(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid path', async () => {
      await expect(client.deleteParameter('invalid-path')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      const result = await client.deleteParameter('/app/config');

      expect(result.path).toBe('/app/config');
      expect(result.deleted).toBe(true);
      expect(result.timestamp).toBeDefined();
    });
  });

  // ==========================================================================
  // getParameters (batch) Tests
  // ==========================================================================

  describe('getParameters', () => {
    it('should get multiple parameters by paths', async () => {
      const mockResponse = {
        parameters: [
          { path: '/app/config/db/host', value: 'localhost' },
          { path: '/app/config/db/port', value: '5432' },
        ],
        errors: {},
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.getParameters([
        '/app/config/db/host',
        '/app/config/db/port',
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/batch/get',
        { paths: ['/app/config/db/host', '/app/config/db/port'] },
        { query: {} }
      );
      expect(result.parameters).toHaveLength(2);
      expect(result.errors).toEqual({});
    });

    it('should get parameters with decryption option', async () => {
      mockTransport.post.mockResolvedValueOnce({ parameters: [], errors: {} });

      await client.getParameters(['/secret/key'], { withDecryption: true });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/batch/get',
        { paths: ['/secret/key'] },
        { query: { with_decryption: true } }
      );
    });

    it('should handle partial failures', async () => {
      const mockResponse = {
        parameters: [{ path: '/app/config/db/host', value: 'localhost' }],
        errors: {
          '/app/config/missing': 'Parameter not found',
        },
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.getParameters([
        '/app/config/db/host',
        '/app/config/missing',
      ]);

      expect(result.parameters).toHaveLength(1);
      expect(result.errors['/app/config/missing']).toBe('Parameter not found');
    });

    it('should throw ValidationError when paths is empty', async () => {
      await expect(client.getParameters([])).rejects.toThrow(ValidationError);
      await expect(client.getParameters(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid paths', async () => {
      await expect(
        client.getParameters(['/valid/path', 'invalid-path'])
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.getParameters(['/app/config']);

      expect(result.parameters).toEqual([]);
      expect(result.errors).toEqual({});
      expect(result.timestamp).toBeDefined();
    });
  });

  // ==========================================================================
  // setParameters (batch) Tests
  // ==========================================================================

  describe('setParameters', () => {
    it('should set multiple parameters atomically', async () => {
      const mockResponse = {
        results: [
          { path: '/app/config/db/host', created: true, success: true },
          { path: '/app/config/db/port', created: false, success: true },
        ],
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.setParameters([
        { path: '/app/config/db/host', value: 'localhost' },
        { path: '/app/config/db/port', value: '5432' },
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/batch/set',
        {
          parameters: [
            { path: '/app/config/db/host', value: 'localhost' },
            { path: '/app/config/db/port', value: '5432' },
          ],
        }
      );
      expect(result.results).toHaveLength(2);
      expect(result.results[0].success).toBe(true);
    });

    it('should set parameters with options', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [] });

      await client.setParameters([
        {
          path: '/app/secrets/key',
          value: 'secret',
          type: ParameterType.SECURE_STRING,
          kmsKeyId: 'alias/key',
          description: 'Secret key',
          tags: { env: 'prod' },
          ttl: 3600,
          auditEnabled: true,
        },
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/batch/set',
        {
          parameters: [
            {
              path: '/app/secrets/key',
              value: 'secret',
              type: ParameterType.SECURE_STRING,
              kms_key_id: 'alias/key',
              description: 'Secret key',
              tags: { env: 'prod' },
              ttl_seconds: 3600,
              audit_enabled: true,
            },
          ],
        }
      );
    });

    it('should set parameters with overwrite option', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [] });

      await client.setParameters([
        {
          path: '/app/config/setting',
          value: 'updated-value',
          overwrite: true,
        },
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/batch/set',
        {
          parameters: [
            {
              path: '/app/config/setting',
              value: 'updated-value',
              overwrite: true,
            },
          ],
        }
      );
    });

    it('should handle partial failures', async () => {
      const mockResponse = {
        results: [
          { path: '/app/config/a', created: true, success: true },
          { path: '/app/config/b', success: false, error: 'Validation failed' },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.setParameters([
        { path: '/app/config/a', value: 'a' },
        { path: '/app/config/b', value: 'b' },
      ]);

      expect(result.results[0].success).toBe(true);
      expect(result.results[1].success).toBe(false);
      expect(result.results[1].error).toBe('Validation failed');
    });

    it('should throw ValidationError when parameters is empty', async () => {
      await expect(client.setParameters([])).rejects.toThrow(ValidationError);
      await expect(client.setParameters(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid parameter object', async () => {
      await expect(
        client.setParameters(['not-an-object'])
      ).rejects.toThrow(ValidationError);

      await expect(
        client.setParameters([null])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for missing path in parameter', async () => {
      await expect(
        client.setParameters([{ value: 'test' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for missing value in parameter', async () => {
      await expect(
        client.setParameters([{ path: '/app/config' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid options in batch parameter', async () => {
      await expect(
        client.setParameters([
          { path: '/app/config', value: 'test', type: 'invalid' },
        ])
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.setParameters([
        { path: '/app/config', value: 'test' },
      ]);

      expect(result.results).toEqual([]);
      expect(result.timestamp).toBeDefined();
    });
  });

  // ==========================================================================
  // deleteParameters (batch) Tests
  // ==========================================================================

  describe('deleteParameters', () => {
    it('should delete multiple parameters', async () => {
      const mockResponse = {
        results: [
          { path: '/app/config/a', deleted: true },
          { path: '/app/config/b', deleted: true },
        ],
        deleted_count: 2,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.deleteParameters([
        '/app/config/a',
        '/app/config/b',
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/batch/delete',
        { paths: ['/app/config/a', '/app/config/b'] }
      );
      expect(result.results).toHaveLength(2);
      expect(result.deletedCount).toBe(2);
    });

    it('should handle partial failures', async () => {
      const mockResponse = {
        results: [
          { path: '/app/config/a', deleted: true },
          { path: '/app/config/b', deleted: false, error: 'Not found' },
        ],
        deleted_count: 1,
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.deleteParameters([
        '/app/config/a',
        '/app/config/b',
      ]);

      expect(result.results[0].deleted).toBe(true);
      expect(result.results[1].deleted).toBe(false);
      expect(result.results[1].error).toBe('Not found');
      expect(result.deletedCount).toBe(1);
    });

    it('should throw ValidationError when paths is empty', async () => {
      await expect(client.deleteParameters([])).rejects.toThrow(ValidationError);
      await expect(client.deleteParameters(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid paths', async () => {
      await expect(
        client.deleteParameters(['/valid/path', 'invalid-path'])
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.deleteParameters(['/app/config']);

      expect(result.results).toEqual([]);
      expect(result.deletedCount).toBe(0);
      expect(result.timestamp).toBeDefined();
    });

    it('should handle deletedCount in camelCase response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        deletedCount: 5,
      });

      const result = await client.deleteParameters(['/app/config']);

      expect(result.deletedCount).toBe(5);
    });
  });

  // ==========================================================================
  // listParameters Tests
  // ==========================================================================

  describe('listParameters', () => {
    it('should list parameters by prefix', async () => {
      const mockResponse = {
        parameters: [
          { path: '/app/config/db/host', value: 'localhost' },
          { path: '/app/config/db/port', value: '5432' },
        ],
        next_cursor: 'cursor123',
        has_more: true,
        total_count: 100,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listParameters('/app/config/');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/parameters',
        { query: { prefix: '/app/config/' } }
      );
      expect(result.parameters).toHaveLength(2);
      expect(result.nextCursor).toBe('cursor123');
      expect(result.hasMore).toBe(true);
      expect(result.totalCount).toBe(100);
    });

    it('should list parameters with all options', async () => {
      mockTransport.get.mockResolvedValueOnce({ parameters: [] });

      await client.listParameters('/app/', {
        recursive: true,
        withDecryption: true,
        withValues: true,
        limit: 50,
        cursor: 'prev-cursor',
      });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/parameters',
        {
          query: {
            prefix: '/app/',
            recursive: true,
            with_decryption: true,
            with_values: true,
            limit: 50,
            cursor: 'prev-cursor',
          },
        }
      );
    });

    it('should list all parameters without prefix', async () => {
      mockTransport.get.mockResolvedValueOnce({ parameters: [] });

      await client.listParameters('');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/parameters',
        { query: {} }
      );
    });

    it('should throw ValidationError for invalid recursive option', async () => {
      await expect(
        client.listParameters('/app/', { recursive: 'true' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid withDecryption option', async () => {
      await expect(
        client.listParameters('/app/', { withDecryption: 'true' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid withValues option', async () => {
      await expect(
        client.listParameters('/app/', { withValues: 'true' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid limit', async () => {
      await expect(
        client.listParameters('/app/', { limit: 0 })
      ).rejects.toThrow(ValidationError);

      await expect(
        client.listParameters('/app/', { limit: -1 })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid cursor', async () => {
      await expect(
        client.listParameters('/app/', { cursor: 123 })
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listParameters('/app/');

      expect(result.parameters).toEqual([]);
      expect(result.nextCursor).toBeNull();
      expect(result.hasMore).toBe(false);
      expect(result.totalCount).toBeNull();
      expect(result.timestamp).toBeDefined();
    });

    it('should handle camelCase response fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        parameters: [],
        nextCursor: 'next',
        hasMore: true,
        totalCount: 50,
      });

      const result = await client.listParameters('/app/');

      expect(result.nextCursor).toBe('next');
      expect(result.hasMore).toBe(true);
      expect(result.totalCount).toBe(50);
    });
  });

  // ==========================================================================
  // searchByTags Tests
  // ==========================================================================

  describe('searchByTags', () => {
    it('should search parameters by tags', async () => {
      const mockResponse = {
        parameters: [
          { path: '/app/config/a', value: 'a', tags: { env: 'prod' } },
          { path: '/app/config/b', value: 'b', tags: { env: 'prod' } },
        ],
        has_more: false,
        timestamp: '2024-01-01T00:00:00.000Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.searchByTags({ env: 'prod' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/search/tags',
        { tags: { env: 'prod' } },
        { query: {} }
      );
      expect(result.parameters).toHaveLength(2);
    });

    it('should search with multiple tags', async () => {
      mockTransport.post.mockResolvedValueOnce({ parameters: [] });

      await client.searchByTags({ env: 'prod', team: 'platform' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/search/tags',
        { tags: { env: 'prod', team: 'platform' } },
        { query: {} }
      );
    });

    it('should search with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ parameters: [] });

      await client.searchByTags({ env: 'prod' }, {
        prefix: '/app/',
        withValues: true,
        withDecryption: true,
        limit: 25,
        cursor: 'abc123',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/parameters/search/tags',
        { tags: { env: 'prod' } },
        {
          query: {
            prefix: '/app/',
            with_values: true,
            with_decryption: true,
            limit: 25,
            cursor: 'abc123',
          },
        }
      );
    });

    it('should throw ValidationError when tags is not an object', async () => {
      await expect(client.searchByTags('invalid')).rejects.toThrow(ValidationError);
      await expect(client.searchByTags(null)).rejects.toThrow(ValidationError);
      await expect(client.searchByTags(['array'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when tags is empty', async () => {
      await expect(client.searchByTags({})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid prefix option', async () => {
      await expect(
        client.searchByTags({ env: 'prod' }, { prefix: 123 })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid withValues option', async () => {
      await expect(
        client.searchByTags({ env: 'prod' }, { withValues: 'true' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid withDecryption option', async () => {
      await expect(
        client.searchByTags({ env: 'prod' }, { withDecryption: 'true' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid limit', async () => {
      await expect(
        client.searchByTags({ env: 'prod' }, { limit: 0 })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid cursor', async () => {
      await expect(
        client.searchByTags({ env: 'prod' }, { cursor: 123 })
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.searchByTags({ env: 'prod' });

      expect(result.parameters).toEqual([]);
      expect(result.nextCursor).toBeNull();
      expect(result.hasMore).toBe(false);
      expect(result.timestamp).toBeDefined();
    });

    it('should handle camelCase response fields', async () => {
      mockTransport.post.mockResolvedValueOnce({
        parameters: [],
        nextCursor: 'next',
        hasMore: true,
      });

      const result = await client.searchByTags({ env: 'prod' });

      expect(result.nextCursor).toBe('next');
      expect(result.hasMore).toBe(true);
    });
  });

  // ==========================================================================
  // Enum Exports Tests
  // ==========================================================================

  describe('ParameterType enum', () => {
    it('should export STRING type', () => {
      expect(ParameterType.STRING).toBe('string');
    });

    it('should export SECURE_STRING type', () => {
      expect(ParameterType.SECURE_STRING).toBe('secureString');
    });

    it('should export STRING_LIST type', () => {
      expect(ParameterType.STRING_LIST).toBe('stringList');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(ParameterType)).toBe(true);
    });
  });

  // ==========================================================================
  // Path Validation Edge Cases
  // ==========================================================================

  describe('Path validation edge cases', () => {
    it('should accept valid paths with hyphens and underscores', async () => {
      mockTransport.get.mockResolvedValueOnce({ parameter: {} });

      await client.getParameter('/my-app/config_v2/db-host_primary');

      expect(mockTransport.get).toHaveBeenCalled();
    });

    it('should reject root path / as invalid', async () => {
      // The root path "/" by itself is not valid per the PATH_REGEX
      // which requires at least one alphanumeric character after the leading slash
      await expect(client.getParameter('/')).rejects.toThrow(ValidationError);
    });

    it('should accept numeric path segments', async () => {
      mockTransport.get.mockResolvedValueOnce({ parameter: {} });

      await client.getParameter('/app/v2/config123');

      expect(mockTransport.get).toHaveBeenCalled();
    });
  });

  // ==========================================================================
  // Response Transformation Tests
  // ==========================================================================

  describe('Response transformation', () => {
    it('should transform snake_case response to camelCase', async () => {
      mockTransport.get.mockResolvedValueOnce({
        parameter: {
          path: '/test',
          value: 'value',
          is_encrypted: true,
          kms_key_id: 'key-123',
          expires_at: '2025-01-01T00:00:00.000Z',
          audit_enabled: true,
          created_at: '2024-01-01T00:00:00.000Z',
          updated_at: '2024-01-02T00:00:00.000Z',
          created_by: 'user1',
          updated_by: 'user2',
        },
      });

      const result = await client.getParameter('/test');

      expect(result.parameter.isEncrypted).toBe(true);
      expect(result.parameter.kmsKeyId).toBe('key-123');
      expect(result.parameter.expiresAt).toBe('2025-01-01T00:00:00.000Z');
      expect(result.parameter.auditEnabled).toBe(true);
      expect(result.parameter.createdAt).toBe('2024-01-01T00:00:00.000Z');
      expect(result.parameter.updatedAt).toBe('2024-01-02T00:00:00.000Z');
      expect(result.parameter.createdBy).toBe('user1');
      expect(result.parameter.updatedBy).toBe('user2');
    });

    it('should handle camelCase response directly', async () => {
      mockTransport.get.mockResolvedValueOnce({
        parameter: {
          path: '/test',
          value: 'value',
          isEncrypted: true,
          kmsKeyId: 'key-123',
          auditEnabled: true,
        },
      });

      const result = await client.getParameter('/test');

      expect(result.parameter.isEncrypted).toBe(true);
      expect(result.parameter.kmsKeyId).toBe('key-123');
      expect(result.parameter.auditEnabled).toBe(true);
    });

    it('should handle null parameter response', async () => {
      // When the server returns { parameter: null }, the expression
      // response?.parameter ?? response evaluates to the full response object
      // (because null ?? response returns response when left side is null)
      // This results in transformParameter receiving the response object
      mockTransport.get.mockResolvedValueOnce({ parameter: null });

      const result = await client.getParameter('/test');

      // The result will be a default parameter object since transformParameter
      // processes the response object which has no matching fields
      expect(result.parameter).toBeDefined();
      expect(result.parameter.path).toBe('');
      expect(result.parameter.value).toBe('');
    });

    it('should handle undefined response', async () => {
      mockTransport.get.mockResolvedValueOnce(undefined);

      const result = await client.getParameter('/test');

      // When response is undefined, response?.parameter is undefined,
      // and undefined ?? response returns undefined, so transformParameter
      // receives undefined and returns null
      expect(result.parameter).toBeNull();
    });

    it('should handle null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getParameter('/test');

      // When response is null, response?.parameter is undefined,
      // and undefined ?? null returns null, so transformParameter
      // receives null and returns null
      expect(result.parameter).toBeNull();
    });
  });
});
