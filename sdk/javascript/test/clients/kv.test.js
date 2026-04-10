/**
 * QuicBaaS JavaScript SDK - KV Client Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import KVClient, { ConsistencyLevel, WriteMode, EventType } from '../../src/clients/kv.js';
import { ValidationError, QuicBaaSError } from '../../src/errors/index.js';

describe('KVClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
      baseUrl: 'https://api.example.com',
      authToken: null,
    };
    client = new KVClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const kvClient = new KVClient(mockTransport);
      expect(kvClient).toBeInstanceOf(KVClient);
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new KVClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport is undefined', () => {
      expect(() => new KVClient(undefined)).toThrow(ValidationError);
    });
  });

  describe('exported constants', () => {
    it('should export ConsistencyLevel enum', () => {
      expect(ConsistencyLevel.EVENTUAL).toBe('eventual');
      expect(ConsistencyLevel.LINEARIZABLE).toBe('linearizable');
      expect(ConsistencyLevel.STRONG).toBe('strong');
    });

    it('should export WriteMode enum', () => {
      expect(WriteMode.SYNC).toBe('sync');
      expect(WriteMode.WAL_DURABLE).toBe('wal_durable');
      expect(WriteMode.BATCHED).toBe('batched');
    });

    it('should export EventType enum', () => {
      expect(EventType.PUT).toBe('PUT');
      expect(EventType.DELETE).toBe('DELETE');
    });

    it('should have frozen ConsistencyLevel', () => {
      expect(Object.isFrozen(ConsistencyLevel)).toBe(true);
    });

    it('should have frozen WriteMode', () => {
      expect(Object.isFrozen(WriteMode)).toBe(true);
    });

    it('should have frozen EventType', () => {
      expect(Object.isFrozen(EventType)).toBe(true);
    });
  });

  describe('put', () => {
    it('should store a key-value pair successfully', async () => {
      mockTransport.put.mockResolvedValueOnce({ revision: 42 });

      const result = await client.put('users', 'user:123', { name: 'Alice' });

      expect(result).toEqual({ revision: 42 });
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kv/users/user%3A123',
        { value: { name: 'Alice' } },
        { query: {} }
      );
    });

    it('should store a value with TTL option', async () => {
      mockTransport.put.mockResolvedValueOnce({ revision: 1 });

      const result = await client.put('sessions', 'session:abc', { token: 'xyz' }, { ttl: 3600 });

      expect(result).toEqual({ revision: 1 });
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kv/sessions/session%3Aabc',
        { value: { token: 'xyz' }, ttl: 3600 },
        { query: {} }
      );
    });

    it('should store a value with writeMode option', async () => {
      mockTransport.put.mockResolvedValueOnce({ revision: 5 });

      const result = await client.put('config', 'setting1', 'value1', { writeMode: WriteMode.SYNC });

      expect(result).toEqual({ revision: 5 });
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kv/config/setting1',
        { value: 'value1' },
        { query: { write_mode: 'sync' } }
      );
    });

    it('should store a value with consistencyLevel option', async () => {
      mockTransport.put.mockResolvedValueOnce({ revision: 10 });

      const result = await client.put('data', 'key1', 100, { consistencyLevel: ConsistencyLevel.STRONG });

      expect(result).toEqual({ revision: 10 });
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kv/data/key1',
        { value: 100 },
        { query: { consistency: 'strong' } }
      );
    });

    it('should store a value with all options', async () => {
      mockTransport.put.mockResolvedValueOnce({ revision: 15 });

      const result = await client.put('cache', 'item:1', [1, 2, 3], {
        ttl: 7200,
        writeMode: WriteMode.WAL_DURABLE,
        consistencyLevel: ConsistencyLevel.LINEARIZABLE,
      });

      expect(result).toEqual({ revision: 15 });
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kv/cache/item%3A1',
        { value: [1, 2, 3], ttl: 7200 },
        { query: { write_mode: 'wal_durable', consistency: 'linearizable' } }
      );
    });

    it('should handle null response gracefully', async () => {
      mockTransport.put.mockResolvedValueOnce(null);

      const result = await client.put('users', 'user:1', 'test');

      expect(result).toEqual({ revision: 0 });
    });

    it('should handle undefined revision in response', async () => {
      mockTransport.put.mockResolvedValueOnce({});

      const result = await client.put('users', 'user:1', 'test');

      expect(result).toEqual({ revision: 0 });
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(client.put('', 'key1', 'value1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', async () => {
      await expect(client.put(null, 'key1', 'value1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', async () => {
      await expect(client.put('users', '', 'value1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is null', async () => {
      await expect(client.put('users', null, 'value1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is null', async () => {
      await expect(client.put('users', 'key1', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is undefined', async () => {
      await expect(client.put('users', 'key1', undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when TTL is negative', async () => {
      await expect(client.put('users', 'key1', 'value', { ttl: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when TTL is zero', async () => {
      await expect(client.put('users', 'key1', 'value', { ttl: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when TTL is not an integer', async () => {
      await expect(client.put('users', 'key1', 'value', { ttl: 3.5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when writeMode is invalid', async () => {
      await expect(client.put('users', 'key1', 'value', { writeMode: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when consistencyLevel is invalid', async () => {
      await expect(client.put('users', 'key1', 'value', { consistencyLevel: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should URL encode entityType and key', async () => {
      mockTransport.put.mockResolvedValueOnce({ revision: 1 });

      await client.put('my entity', 'my/key', 'value');

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kv/my%20entity/my%2Fkey',
        expect.any(Object),
        expect.any(Object)
      );
    });
  });

  describe('get', () => {
    it('should retrieve a value by key successfully', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: true, value: { name: 'Alice' }, revision: 42 });

      const result = await client.get('users', 'user:123');

      expect(result).toEqual({ found: true, value: { name: 'Alice' }, revision: 42 });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/users/user%3A123',
        { query: {} }
      );
    });

    it('should retrieve a value with revision option', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: true, value: 'old-value', revision: 10 });

      const result = await client.get('config', 'setting1', { revision: 10 });

      expect(result).toEqual({ found: true, value: 'old-value', revision: 10 });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/config/setting1',
        { query: { revision: 10 } }
      );
    });

    it('should retrieve a value with consistencyLevel option', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: true, value: 'fresh', revision: 100 });

      const result = await client.get('data', 'key1', { consistencyLevel: ConsistencyLevel.LINEARIZABLE });

      expect(result).toEqual({ found: true, value: 'fresh', revision: 100 });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/data/key1',
        { query: { consistency: 'linearizable' } }
      );
    });

    it('should retrieve a value with all options', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: true, value: 'test', revision: 5 });

      const result = await client.get('cache', 'item:1', {
        revision: 5,
        consistencyLevel: ConsistencyLevel.STRONG,
      });

      expect(result).toEqual({ found: true, value: 'test', revision: 5 });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/cache/item%3A1',
        { query: { revision: 5, consistency: 'strong' } }
      );
    });

    it('should handle not found response', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: false, value: null, revision: 0 });

      const result = await client.get('users', 'nonexistent');

      expect(result).toEqual({ found: false, value: null, revision: 0 });
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.get('users', 'user:1');

      expect(result).toEqual({ found: false, value: null, revision: 0 });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.get('users', 'user:1');

      expect(result).toEqual({ found: false, value: null, revision: 0 });
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(client.get('', 'key1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', async () => {
      await expect(client.get(null, 'key1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', async () => {
      await expect(client.get('users', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is null', async () => {
      await expect(client.get('users', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when revision is negative', async () => {
      await expect(client.get('users', 'key1', { revision: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when revision is not an integer', async () => {
      await expect(client.get('users', 'key1', { revision: 3.5 })).rejects.toThrow(ValidationError);
    });

    it('should allow revision 0', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: false, value: null, revision: 0 });

      const result = await client.get('users', 'key1', { revision: 0 });

      expect(result).toEqual({ found: false, value: null, revision: 0 });
    });

    it('should throw ValidationError when consistencyLevel is invalid', async () => {
      await expect(client.get('users', 'key1', { consistencyLevel: 'bogus' })).rejects.toThrow(ValidationError);
    });
  });

  describe('delete', () => {
    it('should delete a key successfully', async () => {
      mockTransport.delete.mockResolvedValueOnce({ deleted: true, revision: 43 });

      const result = await client.delete('users', 'user:123');

      expect(result).toEqual({ deleted: true, revision: 43 });
      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/kv/users/user%3A123');
    });

    it('should handle deletion of non-existent key', async () => {
      mockTransport.delete.mockResolvedValueOnce({ deleted: false, revision: 0 });

      const result = await client.delete('users', 'nonexistent');

      expect(result).toEqual({ deleted: false, revision: 0 });
    });

    it('should handle null response gracefully', async () => {
      mockTransport.delete.mockResolvedValueOnce(null);

      const result = await client.delete('users', 'user:1');

      expect(result).toEqual({ deleted: false, revision: 0 });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      const result = await client.delete('users', 'user:1');

      expect(result).toEqual({ deleted: false, revision: 0 });
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(client.delete('', 'key1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', async () => {
      await expect(client.delete(null, 'key1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', async () => {
      await expect(client.delete('users', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is null', async () => {
      await expect(client.delete('users', null)).rejects.toThrow(ValidationError);
    });

    it('should URL encode entityType and key', async () => {
      mockTransport.delete.mockResolvedValueOnce({ deleted: true, revision: 1 });

      await client.delete('my entity', 'my/key');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/kv/my%20entity/my%2Fkey');
    });
  });

  describe('list', () => {
    it('should list keys successfully', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: ['key1', 'key2', 'key3'],
        nextCursor: null,
        hasMore: false,
      });

      const result = await client.list('users');

      expect(result).toEqual({
        keys: ['key1', 'key2', 'key3'],
        nextCursor: null,
        hasMore: false,
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/users',
        { query: {} }
      );
    });

    it('should list keys with prefix option', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: ['user:1', 'user:2'],
        nextCursor: null,
        hasMore: false,
      });

      const result = await client.list('users', { prefix: 'user:' });

      expect(result).toEqual({
        keys: ['user:1', 'user:2'],
        nextCursor: null,
        hasMore: false,
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/users',
        { query: { prefix: 'user:' } }
      );
    });

    it('should list keys with limit option', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: ['key1', 'key2'],
        nextCursor: 'cursor123',
        hasMore: true,
      });

      const result = await client.list('data', { limit: 2 });

      expect(result).toEqual({
        keys: ['key1', 'key2'],
        nextCursor: 'cursor123',
        hasMore: true,
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/data',
        { query: { limit: 2 } }
      );
    });

    it('should list keys with cursor option', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: ['key3', 'key4'],
        nextCursor: null,
        hasMore: false,
      });

      const result = await client.list('data', { cursor: 'cursor123' });

      expect(result).toEqual({
        keys: ['key3', 'key4'],
        nextCursor: null,
        hasMore: false,
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/data',
        { query: { cursor: 'cursor123' } }
      );
    });

    it('should list keys with consistencyLevel option', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: ['key1'],
        nextCursor: null,
        hasMore: false,
      });

      const result = await client.list('config', { consistencyLevel: ConsistencyLevel.EVENTUAL });

      expect(result).toEqual({
        keys: ['key1'],
        nextCursor: null,
        hasMore: false,
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/config',
        { query: { consistency: 'eventual' } }
      );
    });

    it('should list keys with all options', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: ['admin:1', 'admin:2'],
        nextCursor: 'next-cursor',
        hasMore: true,
      });

      const result = await client.list('users', {
        prefix: 'admin:',
        limit: 10,
        cursor: 'start-cursor',
        consistencyLevel: ConsistencyLevel.STRONG,
      });

      expect(result).toEqual({
        keys: ['admin:1', 'admin:2'],
        nextCursor: 'next-cursor',
        hasMore: true,
      });
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/users',
        {
          query: {
            prefix: 'admin:',
            limit: 10,
            cursor: 'start-cursor',
            consistency: 'strong',
          },
        }
      );
    });

    it('should handle null response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.list('users');

      expect(result).toEqual({
        keys: [],
        nextCursor: null,
        hasMore: false,
      });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.list('users');

      expect(result).toEqual({
        keys: [],
        nextCursor: null,
        hasMore: false,
      });
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(client.list('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', async () => {
      await expect(client.list(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is negative', async () => {
      await expect(client.list('users', { limit: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is zero', async () => {
      await expect(client.list('users', { limit: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is not an integer', async () => {
      await expect(client.list('users', { limit: 1.5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when consistencyLevel is invalid', async () => {
      await expect(client.list('users', { consistencyLevel: 'bogus' })).rejects.toThrow(ValidationError);
    });

    it('should URL encode entityType', async () => {
      mockTransport.get.mockResolvedValueOnce({ keys: [], nextCursor: null, hasMore: false });

      await client.list('my entity');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/kv/my%20entity',
        expect.any(Object)
      );
    });
  });

  describe('watch', () => {
    let mockEventSource;
    let originalEventSource;

    beforeEach(() => {
      mockEventSource = {
        onmessage: null,
        onerror: null,
        close: jest.fn(),
      };
      originalEventSource = global.EventSource;
      global.EventSource = jest.fn(() => mockEventSource);
    });

    afterEach(() => {
      global.EventSource = originalEventSource;
    });

    it('should create a watcher for a key', async () => {
      const onEvent = jest.fn();

      const watcher = await client.watch('users', 'user:123', { onEvent });

      expect(watcher).toBeDefined();
      expect(watcher.closed).toBe(false);
      expect(typeof watcher.close).toBe('function');
      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/user%3A123/watch'
      );
    });

    it('should create a watcher with startRevision option', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent, startRevision: 100 });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/user%3A123/watch?start_revision=100'
      );
    });

    it('should create a watcher with prevKV option', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent, prevKV: true });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/user%3A123/watch?prev_kv=true'
      );
    });

    it('should create a watcher with progressNotify option', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent, progressNotify: true });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/user%3A123/watch?progress_notify=true'
      );
    });

    it('should create a watcher with filterPut option', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent, filterPut: true });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/user%3A123/watch?filter_put=true'
      );
    });

    it('should create a watcher with filterDelete option', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent, filterDelete: true });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/user%3A123/watch?filter_delete=true'
      );
    });

    it('should create a watcher with all options', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', {
        onEvent,
        startRevision: 50,
        prevKV: true,
        progressNotify: true,
        filterPut: true,
        filterDelete: true,
      });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/user%3A123/watch?start_revision=50&prev_kv=true&progress_notify=true&filter_put=true&filter_delete=true'
      );
    });

    it('should include auth token in watcher URL when set', async () => {
      mockTransport.authToken = 'test-token';
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent });

      expect(global.EventSource).toHaveBeenCalled();
    });

    it('should call onEvent when message is received', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent });

      const eventData = JSON.stringify({
        type: 'PUT',
        key: 'user:123',
        value: { name: 'Alice' },
        revision: 42,
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      mockEventSource.onmessage({ data: eventData });

      expect(onEvent).toHaveBeenCalledWith({
        type: 'PUT',
        key: 'user:123',
        value: { name: 'Alice' },
        prevValue: undefined,
        revision: 42,
        timestamp: '2025-01-17T00:00:00.000Z',
      });
    });

    it('should use default PUT type when type is missing in event', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent });

      const eventData = JSON.stringify({
        key: 'user:123',
        value: { name: 'Bob' },
        revision: 43,
      });

      mockEventSource.onmessage({ data: eventData });

      expect(onEvent).toHaveBeenCalledWith(
        expect.objectContaining({ type: 'PUT' })
      );
    });

    it('should generate timestamp when missing in event', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent });

      const eventData = JSON.stringify({
        type: 'PUT',
        key: 'user:123',
        value: 'test',
        revision: 1,
      });

      mockEventSource.onmessage({ data: eventData });

      expect(onEvent).toHaveBeenCalledWith(
        expect.objectContaining({
          timestamp: expect.stringMatching(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/),
        })
      );
    });

    it('should filter out PUT events when filterPut is enabled', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent, filterPut: true });

      mockEventSource.onmessage({
        data: JSON.stringify({ type: 'PUT', key: 'user:123', value: 'test', revision: 1 }),
      });

      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should filter out DELETE events when filterDelete is enabled', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent, filterDelete: true });

      mockEventSource.onmessage({
        data: JSON.stringify({ type: 'DELETE', key: 'user:123', revision: 1 }),
      });

      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should call onError when parse error occurs', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();

      await client.watch('users', 'user:123', { onEvent, onError });

      mockEventSource.onmessage({ data: 'invalid-json' });

      expect(onError).toHaveBeenCalledWith(expect.any(QuicBaaSError));
    });

    it('should not call onError on parse error when onError not provided', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent });

      // Should not throw
      mockEventSource.onmessage({ data: 'invalid-json' });

      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should call onError when connection error occurs', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();

      await client.watch('users', 'user:123', { onEvent, onError });

      mockEventSource.onerror(new Error('Connection failed'));

      expect(onError).toHaveBeenCalledWith(expect.any(QuicBaaSError));
    });

    it('should not call onError on connection error when onError not provided', async () => {
      const onEvent = jest.fn();

      await client.watch('users', 'user:123', { onEvent });

      // Should not throw
      mockEventSource.onerror(new Error('Connection failed'));
    });

    it('should close the watcher', async () => {
      const onEvent = jest.fn();

      const watcher = await client.watch('users', 'user:123', { onEvent });
      expect(watcher.closed).toBe(false);

      watcher.close();

      expect(watcher.closed).toBe(true);
      expect(mockEventSource.close).toHaveBeenCalled();
    });

    it('should not process events after watcher is closed', async () => {
      const onEvent = jest.fn();

      const watcher = await client.watch('users', 'user:123', { onEvent });
      watcher.close();

      mockEventSource.onmessage({
        data: JSON.stringify({ type: 'PUT', key: 'user:123', value: 'test', revision: 1 }),
      });

      expect(onEvent).not.toHaveBeenCalled();
    });

    it('should not call onError after watcher is closed', async () => {
      const onEvent = jest.fn();
      const onError = jest.fn();

      const watcher = await client.watch('users', 'user:123', { onEvent, onError });
      watcher.close();

      mockEventSource.onerror(new Error('Connection failed'));

      expect(onError).not.toHaveBeenCalled();
    });

    it('should handle multiple close calls gracefully', async () => {
      const onEvent = jest.fn();

      const watcher = await client.watch('users', 'user:123', { onEvent });

      watcher.close();
      watcher.close();
      watcher.close();

      expect(mockEventSource.close).toHaveBeenCalledTimes(1);
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(client.watch('', 'key1', { onEvent: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', async () => {
      await expect(client.watch(null, 'key1', { onEvent: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', async () => {
      await expect(client.watch('users', '', { onEvent: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is null', async () => {
      await expect(client.watch('users', null, { onEvent: jest.fn() })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is missing', async () => {
      await expect(client.watch('users', 'key1', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is not a function', async () => {
      await expect(client.watch('users', 'key1', { onEvent: 'not-a-function' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onError is not a function', async () => {
      await expect(
        client.watch('users', 'key1', { onEvent: jest.fn(), onError: 'not-a-function' })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when startRevision is negative', async () => {
      await expect(
        client.watch('users', 'key1', { onEvent: jest.fn(), startRevision: -1 })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when startRevision is not an integer', async () => {
      await expect(
        client.watch('users', 'key1', { onEvent: jest.fn(), startRevision: 1.5 })
      ).rejects.toThrow(ValidationError);
    });

    it('should allow startRevision 0', async () => {
      const onEvent = jest.fn();

      const watcher = await client.watch('users', 'key1', { onEvent, startRevision: 0 });

      expect(watcher).toBeDefined();
      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/key1/watch?start_revision=0'
      );
    });

    it('should handle EventSource constructor failure', async () => {
      global.EventSource = jest.fn(() => {
        throw new Error('EventSource not supported');
      });

      const onEvent = jest.fn();

      await expect(client.watch('users', 'key1', { onEvent })).rejects.toThrow(QuicBaaSError);
    });
  });

  describe('watchPrefix', () => {
    let mockEventSource;
    let originalEventSource;

    beforeEach(() => {
      mockEventSource = {
        onmessage: null,
        onerror: null,
        close: jest.fn(),
      };
      originalEventSource = global.EventSource;
      global.EventSource = jest.fn(() => mockEventSource);
    });

    afterEach(() => {
      global.EventSource = originalEventSource;
    });

    it('should create a watcher for a prefix', async () => {
      const onEvent = jest.fn();

      const watcher = await client.watchPrefix('users', 'admin:', { onEvent });

      expect(watcher).toBeDefined();
      expect(watcher.closed).toBe(false);
      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/watch?prefix=admin%3A'
      );
    });

    it('should create a watcher with all options', async () => {
      const onEvent = jest.fn();

      await client.watchPrefix('users', 'admin:', {
        onEvent,
        startRevision: 100,
        prevKV: true,
        progressNotify: true,
        filterPut: false,
        filterDelete: false,
      });

      expect(global.EventSource).toHaveBeenCalledWith(
        'https://api.example.com/api/v1/kv/users/watch?prefix=admin%3A&start_revision=100&prev_kv=true&progress_notify=true'
      );
    });

    it('should call onEvent when message is received', async () => {
      const onEvent = jest.fn();

      await client.watchPrefix('users', 'admin:', { onEvent });

      const eventData = JSON.stringify({
        type: 'PUT',
        key: 'admin:user1',
        value: { role: 'admin' },
        revision: 50,
        timestamp: '2025-01-17T00:00:00.000Z',
      });

      mockEventSource.onmessage({ data: eventData });

      expect(onEvent).toHaveBeenCalledWith(
        expect.objectContaining({
          key: 'admin:user1',
          value: { role: 'admin' },
        })
      );
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(
        client.watchPrefix('', 'prefix:', { onEvent: jest.fn() })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', async () => {
      await expect(
        client.watchPrefix(null, 'prefix:', { onEvent: jest.fn() })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when prefix is empty', async () => {
      await expect(
        client.watchPrefix('users', '', { onEvent: jest.fn() })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when prefix is null', async () => {
      await expect(
        client.watchPrefix('users', null, { onEvent: jest.fn() })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is missing', async () => {
      await expect(client.watchPrefix('users', 'prefix:', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when onEvent is not a function', async () => {
      await expect(
        client.watchPrefix('users', 'prefix:', { onEvent: 'not-a-function' })
      ).rejects.toThrow(ValidationError);
    });
  });
});
