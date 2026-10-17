/**
 * SecretsClient Tests
 *
 * Comprehensive tests for the Secrets management client.
 */

import { SecretsClient, SecretStatus, RotationInterval } from '../../src/clients/secrets.js';
import { ValidationError, QuicBaaSError } from '../../src/errors/index.js';

describe('SecretsClient', () => {
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
      baseUrl: 'https://api.example.com',
      authToken: 'test-token',
    };
    client = new SecretsClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const c = new SecretsClient(mockTransport);
      expect(c).toBeInstanceOf(SecretsClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new SecretsClient()).toThrow(ValidationError);
      expect(() => new SecretsClient()).toThrow('transport is required');
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new SecretsClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport has no request method', () => {
      expect(() => new SecretsClient({})).toThrow(ValidationError);
      expect(() => new SecretsClient({})).toThrow('transport must have a request method');
    });
  });

  describe('storeSecret', () => {
    it('should store a secret with required parameters', async () => {
      mockTransport.put.mockResolvedValueOnce({ version: 1 });

      const result = await client.storeSecret('app/db/credentials', {
        username: 'admin',
        password: 'secret123',
      });

      expect(result.version).toBe(1);
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/secrets/app/db/credentials',
        { data: { username: 'admin', password: 'secret123' } },
      );
    });

    it('should store a secret with metadata', async () => {
      mockTransport.put.mockResolvedValueOnce({ version: 2 });

      const result = await client.storeSecret(
        'app/api-key',
        { key: 'abc123' },
        { metadata: { owner: 'team-a', env: 'prod' } },
      );

      expect(result.version).toBe(2);
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/secrets/app/api-key',
        {
          data: { key: 'abc123' },
          metadata: { owner: 'team-a', env: 'prod' },
        },
      );
    });

    it('should URL encode path segments', async () => {
      mockTransport.put.mockResolvedValueOnce({ version: 1 });

      await client.storeSecret('app/special chars/key', { value: 'test' });

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/secrets/app/special%20chars/key',
        expect.any(Object),
      );
    });

    it('should throw ValidationError when path is missing', async () => {
      await expect(client.storeSecret()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.storeSecret('', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is missing', async () => {
      await expect(client.storeSecret('app/secret')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is not an object', async () => {
      await expect(client.storeSecret('app/secret', 'string')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      await expect(client.storeSecret('app/secret', { key: 'value' }, { metadata: 'invalid' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is an array', async () => {
      await expect(client.storeSecret('app/secret', { key: 'value' }, { metadata: [] }))
        .rejects.toThrow(ValidationError);
    });

    it('should handle response with missing version', async () => {
      mockTransport.put.mockResolvedValueOnce({});

      const result = await client.storeSecret('app/secret', { key: 'value' });

      expect(result.version).toBe(1);
    });
  });

  describe('getSecret', () => {
    it('should get the latest version of a secret', async () => {
      mockTransport.get.mockResolvedValueOnce({
        path: 'app/db/credentials',
        data: { username: 'admin', password: 'secret123' },
        metadata: { env: 'prod' },
        version: 3,
        status: 'active',
        created_at: '2024-01-01T00:00:00Z',
        updated_at: '2024-01-02T00:00:00Z',
      });

      const result = await client.getSecret('app/db/credentials');

      expect(result.path).toBe('app/db/credentials');
      expect(result.data).toEqual({ username: 'admin', password: 'secret123' });
      expect(result.version).toBe(3);
      expect(result.status).toBe('active');
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/secrets/app/db/credentials');
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.getSecret('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when path is missing', async () => {
      await expect(client.getSecret()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      await expect(client.getSecret('app/secret')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getSecret('app/secret');

      expect(result.path).toBe('');
      expect(result.data).toEqual({});
      expect(result.version).toBe(0);
      expect(result.status).toBe('active');
    });
  });

  describe('getSecretVersion', () => {
    it('should get a specific version of a secret', async () => {
      mockTransport.get.mockResolvedValueOnce({
        path: 'app/db/credentials',
        data: { username: 'admin', password: 'oldpassword' },
        version: 2,
        status: 'active',
        created_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.getSecretVersion('app/db/credentials', 2);

      expect(result.version).toBe(2);
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/secrets/app/db/credentials',
        { query: { version: 2 } },
      );
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.getSecretVersion('', 1)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when version is missing', async () => {
      await expect(client.getSecretVersion('app/secret')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when version is zero', async () => {
      await expect(client.getSecretVersion('app/secret', 0)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when version is negative', async () => {
      await expect(client.getSecretVersion('app/secret', -1)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when version is not an integer', async () => {
      await expect(client.getSecretVersion('app/secret', 1.5)).rejects.toThrow(ValidationError);
    });
  });

  describe('listSecrets', () => {
    it('should list secrets with prefix', async () => {
      mockTransport.get.mockResolvedValueOnce({
        paths: ['app/db/credentials', 'app/api-key'],
        has_more: true,
        next_cursor: 'cursor123',
      });

      const result = await client.listSecrets('app/');

      expect(result.paths).toEqual(['app/db/credentials', 'app/api-key']);
      expect(result.hasMore).toBe(true);
      expect(result.nextCursor).toBe('cursor123');
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/secrets', { query: { prefix: 'app/' } });
    });

    it('should list secrets without prefix', async () => {
      mockTransport.get.mockResolvedValueOnce({
        paths: ['secret1', 'secret2'],
        has_more: false,
      });

      const result = await client.listSecrets('');

      expect(result.paths).toEqual(['secret1', 'secret2']);
      expect(result.hasMore).toBe(false);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/secrets', { query: {} });
    });

    it('should list secrets with pagination options', async () => {
      mockTransport.get.mockResolvedValueOnce({
        paths: ['secret3'],
        has_more: false,
      });

      await client.listSecrets('', { limit: 10, cursor: 'prev_cursor' });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/secrets', {
        query: { limit: 10, cursor: 'prev_cursor' },
      });
    });

    it('should throw ValidationError when limit is not positive', async () => {
      await expect(client.listSecrets('', { limit: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is negative', async () => {
      await expect(client.listSecrets('', { limit: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when cursor is not a string', async () => {
      await expect(client.listSecrets('', { cursor: 123 })).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listSecrets('');

      expect(result.paths).toEqual([]);
      expect(result.hasMore).toBe(false);
      expect(result.nextCursor).toBeNull();
    });
  });

  describe('deleteSecret', () => {
    it('should delete a secret', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      await client.deleteSecret('app/old-secret');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/secrets/app/old-secret');
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.deleteSecret('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when path is missing', async () => {
      await expect(client.deleteSecret()).rejects.toThrow(ValidationError);
    });
  });

  describe('listSecretVersions', () => {
    it('should list all versions of a secret', async () => {
      mockTransport.get.mockResolvedValueOnce({
        versions: [
          { version: 1, created_at: '2024-01-01T00:00:00Z', destroyed: true },
          { version: 2, created_at: '2024-01-02T00:00:00Z', destroyed: false },
          { version: 3, created_at: '2024-01-03T00:00:00Z', destroyed: false },
        ],
        current_version: 3,
      });

      const result = await client.listSecretVersions('app/db/credentials');

      expect(result.versions).toHaveLength(3);
      expect(result.versions[0].version).toBe(1);
      expect(result.versions[0].destroyed).toBe(true);
      expect(result.currentVersion).toBe(3);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/secrets/app/db/credentials/versions');
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.listSecretVersions('')).rejects.toThrow(ValidationError);
    });

    it('should handle empty versions', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listSecretVersions('app/secret');

      expect(result.versions).toEqual([]);
      expect(result.currentVersion).toBe(0);
    });

    it('should handle versions with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({
        versions: [
          {}, // Version with all missing fields
          { createdAt: '2024-01-02T00:00:00Z' }, // Version with camelCase createdAt
        ],
        currentVersion: 2,
      });

      const result = await client.listSecretVersions('app/secret');

      expect(result.versions).toHaveLength(2);
      expect(result.versions[0].version).toBe(0);
      expect(result.versions[0].createdAt).toBeNull();
      expect(result.versions[0].destroyed).toBe(false);
      expect(result.versions[1].createdAt).toBe('2024-01-02T00:00:00Z');
      expect(result.currentVersion).toBe(2);
    });
  });

  describe('rollbackSecret', () => {
    it('should rollback to a previous version', async () => {
      mockTransport.post.mockResolvedValueOnce({ version: 4 });

      const result = await client.rollbackSecret('app/db/credentials', 2);

      expect(result.version).toBe(4);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/secrets/app/db/credentials/rollback',
        { version: 2 },
      );
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.rollbackSecret('', 1)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when version is missing', async () => {
      await expect(client.rollbackSecret('app/secret')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when version is zero', async () => {
      await expect(client.rollbackSecret('app/secret', 0)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when version is negative', async () => {
      await expect(client.rollbackSecret('app/secret', -1)).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing version', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.rollbackSecret('app/secret', 2);

      expect(result.version).toBe(0);
    });
  });

  describe('rotateSecret', () => {
    it('should rotate a secret without new data', async () => {
      mockTransport.post.mockResolvedValueOnce({
        version: 5,
        rotated_at: '2024-01-05T00:00:00Z',
      });

      const result = await client.rotateSecret('app/db/credentials');

      expect(result.version).toBe(5);
      expect(result.rotatedAt).toBe('2024-01-05T00:00:00Z');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/secrets/app/db/credentials/rotate',
        {},
      );
    });

    it('should rotate a secret with new data', async () => {
      mockTransport.post.mockResolvedValueOnce({
        version: 6,
        rotated_at: '2024-01-06T00:00:00Z',
      });

      const result = await client.rotateSecret('app/db/credentials', {
        newData: { username: 'new-admin', password: 'newpass123' },
      });

      expect(result.version).toBe(6);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/secrets/app/db/credentials/rotate',
        { data: { username: 'new-admin', password: 'newpass123' } },
      );
    });

    it('should rotate a secret with metadata', async () => {
      mockTransport.post.mockResolvedValueOnce({
        version: 7,
        rotated_at: '2024-01-07T00:00:00Z',
      });

      await client.rotateSecret('app/db/credentials', {
        metadata: { rotated_by: 'system' },
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/secrets/app/db/credentials/rotate',
        { metadata: { rotated_by: 'system' } },
      );
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.rotateSecret('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when newData is not an object', async () => {
      await expect(client.rotateSecret('app/secret', { newData: 'invalid' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when newData is an array', async () => {
      await expect(client.rotateSecret('app/secret', { newData: [] }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      await expect(client.rotateSecret('app/secret', { metadata: 'invalid' }))
        .rejects.toThrow(ValidationError);
    });

    it('should handle missing rotatedAt in response', async () => {
      mockTransport.post.mockResolvedValueOnce({ version: 8 });

      const result = await client.rotateSecret('app/secret');

      expect(result.version).toBe(8);
      expect(result.rotatedAt).toBeDefined();
    });

    it('should handle response with missing version', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.rotateSecret('app/secret');

      expect(result.version).toBe(0);
      expect(result.rotatedAt).toBeDefined();
    });
  });

  describe('setRotationPolicy', () => {
    it('should set monthly rotation policy', async () => {
      mockTransport.put.mockResolvedValueOnce({
        enabled: true,
        interval: 'monthly',
        next_rotation: '2024-02-01T00:00:00Z',
      });

      const result = await client.setRotationPolicy('app/db/credentials', {
        interval: RotationInterval.MONTHLY,
      });

      expect(result.enabled).toBe(true);
      expect(result.interval).toBe('monthly');
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/secrets/app/db/credentials/rotation-policy',
        { enabled: true, interval: 'monthly' },
      );
    });

    it('should set custom rotation policy with intervalSeconds', async () => {
      mockTransport.put.mockResolvedValueOnce({
        enabled: true,
        interval: 'custom',
        interval_seconds: 86400 * 45,
      });

      const result = await client.setRotationPolicy('app/secret', {
        interval: RotationInterval.CUSTOM,
        intervalSeconds: 86400 * 45,
      });

      expect(result.enabled).toBe(true);
      expect(result.intervalSeconds).toBe(86400 * 45);
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/secrets/app/secret/rotation-policy',
        { enabled: true, interval: 'custom', interval_seconds: 86400 * 45 },
      );
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.setRotationPolicy('', { interval: RotationInterval.DAILY }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy is missing', async () => {
      await expect(client.setRotationPolicy('app/secret')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when interval is missing', async () => {
      await expect(client.setRotationPolicy('app/secret', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when interval is invalid', async () => {
      await expect(client.setRotationPolicy('app/secret', { interval: 'invalid' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when custom interval is missing intervalSeconds', async () => {
      await expect(client.setRotationPolicy('app/secret', { interval: RotationInterval.CUSTOM }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when intervalSeconds is not positive', async () => {
      await expect(client.setRotationPolicy('app/secret', {
        interval: RotationInterval.CUSTOM,
        intervalSeconds: 0,
      })).rejects.toThrow(ValidationError);
    });
  });

  describe('getRotationPolicy', () => {
    it('should get rotation policy', async () => {
      mockTransport.get.mockResolvedValueOnce({
        enabled: true,
        interval: 'weekly',
        last_rotated: '2024-01-01T00:00:00Z',
        next_rotation: '2024-01-08T00:00:00Z',
      });

      const result = await client.getRotationPolicy('app/db/credentials');

      expect(result.enabled).toBe(true);
      expect(result.interval).toBe('weekly');
      expect(result.lastRotated).toBe('2024-01-01T00:00:00Z');
      expect(result.nextRotation).toBe('2024-01-08T00:00:00Z');
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/secrets/app/db/credentials/rotation-policy');
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.getRotationPolicy('')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getRotationPolicy('app/secret');

      expect(result.enabled).toBe(false);
      expect(result.interval).toBeNull();
      expect(result.lastRotated).toBeNull();
      expect(result.nextRotation).toBeNull();
    });
  });

  describe('updateSecretStatus', () => {
    it('should disable a secret', async () => {
      mockTransport.patch.mockResolvedValueOnce({
        success: true,
        status: 'disabled',
      });

      const result = await client.updateSecretStatus('app/secret', SecretStatus.DISABLED);

      expect(result.success).toBe(true);
      expect(result.status).toBe('disabled');
      expect(mockTransport.patch).toHaveBeenCalledWith(
        '/api/v1/secrets/app/secret/status',
        { status: 'disabled' },
      );
    });

    it('should enable a secret', async () => {
      mockTransport.patch.mockResolvedValueOnce({
        success: true,
        status: 'active',
      });

      const result = await client.updateSecretStatus('app/secret', SecretStatus.ACTIVE);

      expect(result.status).toBe('active');
    });

    it('should throw ValidationError when path is empty', async () => {
      await expect(client.updateSecretStatus('', SecretStatus.DISABLED)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when status is empty', async () => {
      await expect(client.updateSecretStatus('app/secret', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when status is invalid', async () => {
      await expect(client.updateSecretStatus('app/secret', 'invalid')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.patch.mockResolvedValueOnce({});

      const result = await client.updateSecretStatus('app/secret', SecretStatus.DISABLED);

      expect(result.success).toBe(true);
      expect(result.status).toBe('disabled');
    });
  });

  describe('getSecretsByPrefix', () => {
    it('should batch get secrets by prefix', async () => {
      mockTransport.get.mockResolvedValueOnce({
        secrets: {
          'app/db/user1': {
            path: 'app/db/user1',
            data: { username: 'user1' },
            version: 1,
            status: 'active',
            created_at: '2024-01-01T00:00:00Z',
          },
          'app/db/user2': {
            path: 'app/db/user2',
            data: { username: 'user2' },
            version: 2,
            status: 'active',
            created_at: '2024-01-02T00:00:00Z',
          },
        },
        errors: [],
      });

      const result = await client.getSecretsByPrefix('app/db/');

      expect(Object.keys(result.secrets)).toHaveLength(2);
      expect(result.secrets['app/db/user1'].data.username).toBe('user1');
      expect(result.errors).toEqual([]);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/secrets/batch', {
        query: { prefix: 'app/db/' },
      });
    });

    it('should handle errors in batch get', async () => {
      mockTransport.get.mockResolvedValueOnce({
        secrets: {},
        errors: ['app/db/missing'],
      });

      const result = await client.getSecretsByPrefix('app/db/');

      expect(Object.keys(result.secrets)).toHaveLength(0);
      expect(result.errors).toEqual(['app/db/missing']);
    });

    it('should throw ValidationError when prefix is empty', async () => {
      await expect(client.getSecretsByPrefix('')).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getSecretsByPrefix('app/');

      expect(result.secrets).toEqual({});
      expect(result.errors).toEqual([]);
    });
  });

  describe('watchSecretsByPrefix', () => {
    let originalEventSource;

    beforeEach(() => {
      originalEventSource = global.EventSource;
      global.EventSource = jest.fn().mockImplementation(() => ({
        onmessage: null,
        onerror: null,
        close: jest.fn(),
      }));
    });

    afterEach(() => {
      global.EventSource = originalEventSource;
    });

    it('should create a watcher for secrets', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();

      const watcher = await client.watchSecretsByPrefix('app/', { onEvent, onError });

      expect(watcher).toBeDefined();
      expect(watcher.closed).toBe(false);
      expect(typeof watcher.close).toBe('function');
      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/secrets/watch?prefix=app%2F',
      );
    });

    it('should call onEvent when message is received', async () => {
      const onEvent = jest.fn();
      let messageHandler;

      global.EventSource = jest.fn().mockImplementation(() => {
        const es = {
          onmessage: null,
          onerror: null,
          close: jest.fn(),
        };
        setTimeout(() => {
          if (es.onmessage) {
            messageHandler = es.onmessage;
            messageHandler({ data: JSON.stringify({ type: 'PUT', path: 'app/secret', version: 1 }) });
          }
        }, 0);
        return es;
      });

      await client.watchSecretsByPrefix('app/', { onEvent });
      await new Promise(resolve => setTimeout(resolve, 10));

      expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({
        type: 'PUT',
        path: 'app/secret',
        version: 1,
      }));
    });

    it('should call onError when parse error occurs', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();
      let messageHandler;

      global.EventSource = jest.fn().mockImplementation(() => {
        const es = {
          onmessage: null,
          onerror: null,
          close: jest.fn(),
        };
        setTimeout(() => {
          if (es.onmessage) {
            messageHandler = es.onmessage;
            messageHandler({ data: 'invalid json' });
          }
        }, 0);
        return es;
      });

      await client.watchSecretsByPrefix('app/', { onEvent, onError });
      await new Promise(resolve => setTimeout(resolve, 10));

      expect(onError).toHaveBeenCalledWith(expect.any(QuicBaaSError));
    });

    it('should call onError when connection error occurs', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();

      global.EventSource = jest.fn().mockImplementation(() => {
        const es = {
          onmessage: null,
          onerror: null,
          close: jest.fn(),
        };
        setTimeout(() => {
          if (es.onerror) {
            es.onerror(new Error('Connection lost'));
          }
        }, 0);
        return es;
      });

      await client.watchSecretsByPrefix('app/', { onEvent, onError });
      await new Promise(resolve => setTimeout(resolve, 10));

      expect(onError).toHaveBeenCalledWith(expect.any(QuicBaaSError));
    });

    it('should close the watcher', async () => {
      const mockClose = jest.fn();
      global.EventSource = jest.fn().mockImplementation(() => ({
        onmessage: null,
        onerror: null,
        close: mockClose,
      }));

      const onEvent = jest.fn();
      const watcher = await client.watchSecretsByPrefix('app/', { onEvent });

      watcher.close();

      expect(watcher.closed).toBe(true);
      expect(mockClose).toHaveBeenCalled();
    });

    it('should not call handlers after close', async () => {
      const onEvent = jest.fn();
      let messageHandler;

      global.EventSource = jest.fn().mockImplementation(() => ({
        onerror: null,
        close: jest.fn(),
        set onmessage(handler) {
          messageHandler = handler;
        },
        get onmessage() {
          return messageHandler;
        },
      }));

      const watcher = await client.watchSecretsByPrefix('app/', { onEvent });
      watcher.close();

      if (messageHandler) {
        messageHandler({ data: JSON.stringify({ type: 'PUT', path: 'app/secret' }) });
      }

      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should throw ValidationError when prefix is empty', async () => {
      await expect(client.watchSecretsByPrefix('', { onEvent: jest.fn() }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when options is missing', async () => {
      await expect(client.watchSecretsByPrefix('app/'))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is missing', async () => {
      await expect(client.watchSecretsByPrefix('app/', {}))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is not a function', async () => {
      await expect(client.watchSecretsByPrefix('app/', { onEvent: 'invalid' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onError is not a function', async () => {
      await expect(client.watchSecretsByPrefix('app/', { onEvent: jest.fn(), onError: 'invalid' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw QuicBaaSError when EventSource creation fails', async () => {
      global.EventSource = jest.fn().mockImplementation(() => {
        throw new Error('EventSource not supported');
      });

      await expect(client.watchSecretsByPrefix('app/', { onEvent: jest.fn() }))
        .rejects.toThrow(QuicBaaSError);
    });

    it('should work without authToken in transport', async () => {
      const transportWithoutAuth = {
        request: jest.fn(),
        get: jest.fn(),
        post: jest.fn(),
        put: jest.fn(),
        patch: jest.fn(),
        delete: jest.fn(),
        baseUrl: 'https://api.example.com',
        authToken: null,
      };
      const clientNoAuth = new SecretsClient(transportWithoutAuth);

      const onEvent = jest.fn();
      const watcher = await clientNoAuth.watchSecretsByPrefix('app/', { onEvent });

      expect(watcher).toBeDefined();
      expect(watcher.closed).toBe(false);
    });

    it('should not process messages after watcher is closed', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();
      let storedMessageHandler;
      let storedErrorHandler;

      global.EventSource = jest.fn().mockImplementation(() => {
        const es = {
          _onmessage: null,
          _onerror: null,
          close: jest.fn(),
          set onmessage(handler) {
            this._onmessage = handler;
            storedMessageHandler = handler;
          },
          get onmessage() {
            return this._onmessage;
          },
          set onerror(handler) {
            this._onerror = handler;
            storedErrorHandler = handler;
          },
          get onerror() {
            return this._onerror;
          },
        };
        return es;
      });

      const watcher = await client.watchSecretsByPrefix('app/', { onEvent, onError });
      watcher.close();

      // Try to trigger message and error after close
      if (storedMessageHandler) {
        storedMessageHandler({ data: JSON.stringify({ type: 'PUT', path: 'app/secret' }) });
      }
      if (storedErrorHandler) {
        storedErrorHandler(new Error('Connection lost'));
      }

      expect(onEvent).not.toHaveBeenCalled();
      expect(onError).not.toHaveBeenCalled();
    });

    it('should handle message with missing type defaulting to PUT', async () => {
      const onEvent = jest.fn();
      let messageHandler;

      global.EventSource = jest.fn().mockImplementation(() => {
        const es = {
          onmessage: null,
          onerror: null,
          close: jest.fn(),
        };
        setTimeout(() => {
          if (es.onmessage) {
            messageHandler = es.onmessage;
            messageHandler({ data: JSON.stringify({ path: 'app/secret', version: 1 }) });
          }
        }, 0);
        return es;
      });

      await client.watchSecretsByPrefix('app/', { onEvent });
      await new Promise(resolve => setTimeout(resolve, 10));

      expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({
        type: 'PUT',
        path: 'app/secret',
        version: 1,
      }));
    });

    it('should silently ignore parse errors when no onError handler', async () => {
      const onEvent = jest.fn();
      let messageHandler;

      global.EventSource = jest.fn().mockImplementation(() => {
        const es = {
          onmessage: null,
          onerror: null,
          close: jest.fn(),
        };
        setTimeout(() => {
          if (es.onmessage) {
            messageHandler = es.onmessage;
            messageHandler({ data: 'invalid json' });
          }
        }, 0);
        return es;
      });

      // No onError handler provided
      await client.watchSecretsByPrefix('app/', { onEvent });
      await new Promise(resolve => setTimeout(resolve, 10));

      // Should not throw, just silently ignore
      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should silently ignore connection errors when no onError handler', async () => {
      const onEvent = jest.fn();

      global.EventSource = jest.fn().mockImplementation(() => {
        const es = {
          onmessage: null,
          onerror: null,
          close: jest.fn(),
        };
        setTimeout(() => {
          if (es.onerror) {
            es.onerror(new Error('Connection lost'));
          }
        }, 0);
        return es;
      });

      // No onError handler provided
      await client.watchSecretsByPrefix('app/', { onEvent });
      await new Promise(resolve => setTimeout(resolve, 10));

      // Should not throw, just silently ignore
      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should close watcher only once even if close is called multiple times', async () => {
      const mockClose = jest.fn();
      global.EventSource = jest.fn().mockImplementation(() => ({
        onmessage: null,
        onerror: null,
        close: mockClose,
      }));

      const onEvent = jest.fn();
      const watcher = await client.watchSecretsByPrefix('app/', { onEvent });

      watcher.close();
      watcher.close();
      watcher.close();

      expect(watcher.closed).toBe(true);
      expect(mockClose).toHaveBeenCalledTimes(1);
    });

  });

  describe('enum exports', () => {
    it('should export SecretStatus enum with all values', () => {
      expect(SecretStatus.ACTIVE).toBe('active');
      expect(SecretStatus.DISABLED).toBe('disabled');
    });

    it('should export RotationInterval enum with all values', () => {
      expect(RotationInterval.HOURLY).toBe('hourly');
      expect(RotationInterval.DAILY).toBe('daily');
      expect(RotationInterval.WEEKLY).toBe('weekly');
      expect(RotationInterval.MONTHLY).toBe('monthly');
      expect(RotationInterval.QUARTERLY).toBe('quarterly');
      expect(RotationInterval.YEARLY).toBe('yearly');
      expect(RotationInterval.CUSTOM).toBe('custom');
    });

    it('should have frozen enums', () => {
      expect(Object.isFrozen(SecretStatus)).toBe(true);
      expect(Object.isFrozen(RotationInterval)).toBe(true);
    });
  });
});
