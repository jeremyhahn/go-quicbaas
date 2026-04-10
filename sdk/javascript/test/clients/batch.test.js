/**
 * QuicBaaS JavaScript SDK - Batch Client Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import BatchClient from '../../src/clients/batch.js';
import { ValidationError } from '../../src/errors/index.js';

describe('BatchClient', () => {
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
    client = new BatchClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const batchClient = new BatchClient(mockTransport);
      expect(batchClient).toBeInstanceOf(BatchClient);
      expect(batchClient.transport).toBe(mockTransport);
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new BatchClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport is undefined', () => {
      expect(() => new BatchClient(undefined)).toThrow(ValidationError);
    });
  });

  describe('batchPut', () => {
    it('should execute batch put with single item', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revision: 42,
        results: [{ success: true }],
      });

      const result = await client.batchPut([
        { entityType: 'users', key: 'user:1', value: { name: 'Alice' } },
      ]);

      expect(result).toEqual({
        revision: 42,
        results: [{ success: true }],
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/put',
        {
          operations: [
            {
              type: 'PUT',
              entityType: 'users',
              key: 'user:1',
              value: { name: 'Alice' },
              ttl: undefined,
            },
          ],
        }
      );
    });

    it('should execute batch put with multiple items', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revision: 50,
        results: [{ success: true }, { success: true }, { success: true }],
      });

      const result = await client.batchPut([
        { entityType: 'users', key: 'user:1', value: { name: 'Alice' } },
        { entityType: 'users', key: 'user:2', value: { name: 'Bob' } },
        { entityType: 'config', key: 'setting:1', value: 'enabled' },
      ]);

      expect(result).toEqual({
        revision: 50,
        results: [{ success: true }, { success: true }, { success: true }],
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/put',
        {
          operations: [
            { type: 'PUT', entityType: 'users', key: 'user:1', value: { name: 'Alice' }, ttl: undefined },
            { type: 'PUT', entityType: 'users', key: 'user:2', value: { name: 'Bob' }, ttl: undefined },
            { type: 'PUT', entityType: 'config', key: 'setting:1', value: 'enabled', ttl: undefined },
          ],
        }
      );
    });

    it('should execute batch put with TTL', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revision: 10,
        results: [{ success: true }],
      });

      const result = await client.batchPut([
        { entityType: 'sessions', key: 'session:abc', value: { token: 'xyz' }, ttl: 3600 },
      ]);

      expect(result).toEqual({
        revision: 10,
        results: [{ success: true }],
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/put',
        {
          operations: [
            { type: 'PUT', entityType: 'sessions', key: 'session:abc', value: { token: 'xyz' }, ttl: 3600 },
          ],
        }
      );
    });

    it('should handle various value types', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revision: 100,
        results: [{ success: true }, { success: true }, { success: true }, { success: true }],
      });

      await client.batchPut([
        { entityType: 'test', key: 'string', value: 'hello' },
        { entityType: 'test', key: 'number', value: 42 },
        { entityType: 'test', key: 'array', value: [1, 2, 3] },
        { entityType: 'test', key: 'object', value: { nested: { deep: true } } },
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/put',
        {
          operations: [
            { type: 'PUT', entityType: 'test', key: 'string', value: 'hello', ttl: undefined },
            { type: 'PUT', entityType: 'test', key: 'number', value: 42, ttl: undefined },
            { type: 'PUT', entityType: 'test', key: 'array', value: [1, 2, 3], ttl: undefined },
            { type: 'PUT', entityType: 'test', key: 'object', value: { nested: { deep: true } }, ttl: undefined },
          ],
        }
      );
    });

    it('should throw ValidationError when items is empty array', async () => {
      await expect(client.batchPut([])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is null', async () => {
      await expect(client.batchPut(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is undefined', async () => {
      await expect(client.batchPut(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is not an array', async () => {
      await expect(client.batchPut('not-an-array')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is not an object', async () => {
      await expect(client.batchPut(['not-an-object'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is an array', async () => {
      await expect(client.batchPut([['nested', 'array']])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is null', async () => {
      await expect(client.batchPut([null])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is missing', async () => {
      await expect(
        client.batchPut([{ key: 'key1', value: 'value1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(
        client.batchPut([{ entityType: '', key: 'key1', value: 'value1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is not a string', async () => {
      await expect(
        client.batchPut([{ entityType: 123, key: 'key1', value: 'value1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(
        client.batchPut([{ entityType: 'users', value: 'value1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', async () => {
      await expect(
        client.batchPut([{ entityType: 'users', key: '', value: 'value1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is not a string', async () => {
      await expect(
        client.batchPut([{ entityType: 'users', key: 123, value: 'value1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is missing', async () => {
      await expect(
        client.batchPut([{ entityType: 'users', key: 'key1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is null', async () => {
      await expect(
        client.batchPut([{ entityType: 'users', key: 'key1', value: null }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when value is undefined', async () => {
      await expect(
        client.batchPut([{ entityType: 'users', key: 'key1', value: undefined }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ttl is negative', async () => {
      await expect(
        client.batchPut([{ entityType: 'users', key: 'key1', value: 'test', ttl: -1 }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ttl is not an integer', async () => {
      await expect(
        client.batchPut([{ entityType: 'users', key: 'key1', value: 'test', ttl: 1.5 }])
      ).rejects.toThrow(ValidationError);
    });

    it('should allow ttl of 0', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revision: 1,
        results: [{ success: true }],
      });

      await client.batchPut([
        { entityType: 'users', key: 'key1', value: 'test', ttl: 0 },
      ]);

      expect(mockTransport.post).toHaveBeenCalled();
    });

    it('should include specific item index in validation error message', async () => {
      try {
        await client.batchPut([
          { entityType: 'users', key: 'key1', value: 'valid' },
          { entityType: '', key: 'key2', value: 'invalid' },
        ]);
        fail('Expected ValidationError');
      } catch (error) {
        expect(error).toBeInstanceOf(ValidationError);
        expect(error.details.field).toContain('items[1]');
      }
    });
  });

  describe('batchGet', () => {
    it('should execute batch get with single item', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [{ success: true, value: { name: 'Alice' } }],
      });

      const result = await client.batchGet([
        { entityType: 'users', key: 'user:1' },
      ]);

      expect(result).toEqual({
        results: [{ success: true, value: { name: 'Alice' } }],
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/get',
        {
          operations: [
            { type: 'GET', entityType: 'users', key: 'user:1' },
          ],
        }
      );
    });

    it('should execute batch get with multiple items', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [
          { success: true, value: { name: 'Alice' } },
          { success: true, value: { name: 'Bob' } },
          { success: false, error: 'Not found' },
        ],
      });

      const result = await client.batchGet([
        { entityType: 'users', key: 'user:1' },
        { entityType: 'users', key: 'user:2' },
        { entityType: 'users', key: 'user:999' },
      ]);

      expect(result).toEqual({
        results: [
          { success: true, value: { name: 'Alice' } },
          { success: true, value: { name: 'Bob' } },
          { success: false, error: 'Not found' },
        ],
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/get',
        {
          operations: [
            { type: 'GET', entityType: 'users', key: 'user:1' },
            { type: 'GET', entityType: 'users', key: 'user:2' },
            { type: 'GET', entityType: 'users', key: 'user:999' },
          ],
        }
      );
    });

    it('should execute batch get across different entity types', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [
          { success: true, value: { name: 'Alice' } },
          { success: true, value: 'enabled' },
        ],
      });

      const result = await client.batchGet([
        { entityType: 'users', key: 'user:1' },
        { entityType: 'config', key: 'feature:flag' },
      ]);

      expect(result.results).toHaveLength(2);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/get',
        {
          operations: [
            { type: 'GET', entityType: 'users', key: 'user:1' },
            { type: 'GET', entityType: 'config', key: 'feature:flag' },
          ],
        }
      );
    });

    it('should throw ValidationError when items is empty array', async () => {
      await expect(client.batchGet([])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is null', async () => {
      await expect(client.batchGet(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is undefined', async () => {
      await expect(client.batchGet(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is not an array', async () => {
      await expect(client.batchGet({ entityType: 'users', key: 'key1' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is not an object', async () => {
      await expect(client.batchGet(['not-an-object'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is null', async () => {
      await expect(client.batchGet([null])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is missing', async () => {
      await expect(
        client.batchGet([{ key: 'key1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(
        client.batchGet([{ entityType: '', key: 'key1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is whitespace only', async () => {
      await expect(
        client.batchGet([{ entityType: '   ', key: 'key1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(
        client.batchGet([{ entityType: 'users' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', async () => {
      await expect(
        client.batchGet([{ entityType: 'users', key: '' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is whitespace only', async () => {
      await expect(
        client.batchGet([{ entityType: 'users', key: '   ' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should include specific item index in validation error message', async () => {
      try {
        await client.batchGet([
          { entityType: 'users', key: 'key1' },
          { entityType: 'users', key: 'key2' },
          { entityType: 'users', key: '' },
        ]);
        fail('Expected ValidationError');
      } catch (error) {
        expect(error).toBeInstanceOf(ValidationError);
        expect(error.details.field).toContain('items[2]');
      }
    });
  });

  describe('batchDelete', () => {
    it('should execute batch delete with single item', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revision: 43,
        results: [{ success: true }],
      });

      const result = await client.batchDelete([
        { entityType: 'users', key: 'user:1' },
      ]);

      expect(result).toEqual({
        revision: 43,
        results: [{ success: true }],
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/delete',
        {
          operations: [
            { type: 'DELETE', entityType: 'users', key: 'user:1' },
          ],
        }
      );
    });

    it('should execute batch delete with multiple items', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revision: 60,
        results: [{ success: true }, { success: true }, { success: false }],
      });

      const result = await client.batchDelete([
        { entityType: 'users', key: 'user:1' },
        { entityType: 'users', key: 'user:2' },
        { entityType: 'config', key: 'old:setting' },
      ]);

      expect(result).toEqual({
        revision: 60,
        results: [{ success: true }, { success: true }, { success: false }],
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/delete',
        {
          operations: [
            { type: 'DELETE', entityType: 'users', key: 'user:1' },
            { type: 'DELETE', entityType: 'users', key: 'user:2' },
            { type: 'DELETE', entityType: 'config', key: 'old:setting' },
          ],
        }
      );
    });

    it('should execute batch delete across different entity types', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revision: 70,
        results: [{ success: true }, { success: true }],
      });

      await client.batchDelete([
        { entityType: 'users', key: 'user:1' },
        { entityType: 'sessions', key: 'session:abc' },
      ]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/delete',
        {
          operations: [
            { type: 'DELETE', entityType: 'users', key: 'user:1' },
            { type: 'DELETE', entityType: 'sessions', key: 'session:abc' },
          ],
        }
      );
    });

    it('should throw ValidationError when items is empty array', async () => {
      await expect(client.batchDelete([])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is null', async () => {
      await expect(client.batchDelete(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is undefined', async () => {
      await expect(client.batchDelete(undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is not an array', async () => {
      await expect(client.batchDelete('not-an-array')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is not an object', async () => {
      await expect(client.batchDelete([123])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is null', async () => {
      await expect(client.batchDelete([null])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is missing', async () => {
      await expect(
        client.batchDelete([{ key: 'key1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(
        client.batchDelete([{ entityType: '', key: 'key1' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is missing', async () => {
      await expect(
        client.batchDelete([{ entityType: 'users' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', async () => {
      await expect(
        client.batchDelete([{ entityType: 'users', key: '' }])
      ).rejects.toThrow(ValidationError);
    });

    it('should include specific item index in validation error message', async () => {
      try {
        await client.batchDelete([
          { entityType: 'users', key: 'key1' },
          { key: 'key2' },
        ]);
        fail('Expected ValidationError');
      } catch (error) {
        expect(error).toBeInstanceOf(ValidationError);
        expect(error.details.field).toContain('items[1]');
      }
    });
  });

  describe('_executeOperation', () => {
    it('should call transport.post with correct path and body', async () => {
      mockTransport.post.mockResolvedValueOnce({ success: true });

      const operations = [
        { type: 'PUT', entityType: 'test', key: 'key1', value: 'value1' },
      ];

      await client._executeOperation('put', operations);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/batch/put',
        { operations }
      );
    });

    it('should handle transport errors', async () => {
      const networkError = new Error('Network failure');
      mockTransport.post.mockRejectedValueOnce(networkError);

      await expect(
        client._executeOperation('get', [{ type: 'GET', entityType: 'test', key: 'key1' }])
      ).rejects.toThrow('Network failure');
    });
  });

  describe('_validateItemBase', () => {
    it('should validate valid item', () => {
      expect(() => {
        client._validateItemBase({ entityType: 'users', key: 'key1' }, 0, 'test');
      }).not.toThrow();
    });

    it('should throw for non-object item', () => {
      expect(() => {
        client._validateItemBase('string', 0, 'test');
      }).toThrow(ValidationError);
    });

    it('should throw for array item', () => {
      expect(() => {
        client._validateItemBase(['array'], 0, 'test');
      }).toThrow(ValidationError);
    });

    it('should throw for null item', () => {
      expect(() => {
        client._validateItemBase(null, 0, 'test');
      }).toThrow(ValidationError);
    });
  });
});
