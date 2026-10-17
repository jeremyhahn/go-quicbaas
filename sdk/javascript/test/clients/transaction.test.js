/**
 * QuicBaaS JavaScript SDK - Transaction Client Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import TransactionClient, {
  CompareTarget,
  CompareResult,
  TxnOpType,
} from '../../src/clients/transaction.js';
import { ValidationError } from '../../src/errors/index.js';

describe('TransactionClient', () => {
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
    client = new TransactionClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const txnClient = new TransactionClient(mockTransport);
      expect(txnClient).toBeInstanceOf(TransactionClient);
      expect(txnClient.transport).toBe(mockTransport);
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new TransactionClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport is undefined', () => {
      expect(() => new TransactionClient(undefined)).toThrow(ValidationError);
    });
  });

  describe('exported constants', () => {
    it('should export CompareTarget enum', () => {
      expect(CompareTarget.VERSION).toBe('VERSION');
      expect(CompareTarget.VALUE).toBe('VALUE');
      expect(CompareTarget.MOD_REVISION).toBe('MOD_REVISION');
      expect(CompareTarget.CREATE_REVISION).toBe('CREATE_REVISION');
    });

    it('should export CompareResult enum', () => {
      expect(CompareResult.EQUAL).toBe('EQUAL');
      expect(CompareResult.NOT_EQUAL).toBe('NOT_EQUAL');
      expect(CompareResult.LESS).toBe('LESS');
      expect(CompareResult.GREATER).toBe('GREATER');
    });

    it('should export TxnOpType enum', () => {
      expect(TxnOpType.PUT).toBe('PUT');
      expect(TxnOpType.GET).toBe('GET');
      expect(TxnOpType.DELETE).toBe('DELETE');
    });

    it('should have frozen CompareTarget', () => {
      expect(Object.isFrozen(CompareTarget)).toBe(true);
    });

    it('should have frozen CompareResult', () => {
      expect(Object.isFrozen(CompareResult)).toBe(true);
    });

    it('should have frozen TxnOpType', () => {
      expect(Object.isFrozen(TxnOpType)).toBe(true);
    });
  });

  describe('compare', () => {
    it('should create a revision compare condition', () => {
      const condition = client.compare('users', 'user:123', 5);

      expect(condition).toEqual({
        entityType: 'users',
        key: 'user:123',
        target: CompareTarget.MOD_REVISION,
        result: CompareResult.EQUAL,
        revision: 5,
      });
    });

    it('should create a condition to check key does not exist (revision 0)', () => {
      const condition = client.compare('users', 'user:new', 0);

      expect(condition).toEqual({
        entityType: 'users',
        key: 'user:new',
        target: CompareTarget.MOD_REVISION,
        result: CompareResult.EQUAL,
        revision: 0,
      });
    });

    it('should throw ValidationError when entityType is empty', () => {
      expect(() => client.compare('', 'key1', 5)).toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', () => {
      expect(() => client.compare(null, 'key1', 5)).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', () => {
      expect(() => client.compare('users', '', 5)).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is null', () => {
      expect(() => client.compare('users', null, 5)).toThrow(ValidationError);
    });

    it('should throw ValidationError when revision is negative', () => {
      expect(() => client.compare('users', 'key1', -1)).toThrow(ValidationError);
    });

    it('should throw ValidationError when revision is not an integer', () => {
      expect(() => client.compare('users', 'key1', 1.5)).toThrow(ValidationError);
    });

    it('should throw ValidationError when revision is undefined', () => {
      expect(() => client.compare('users', 'key1', undefined)).toThrow(ValidationError);
      expect(() => client.compare('users', 'key1', undefined)).toThrow('revision is required');
    });

    it('should throw ValidationError when revision is null', () => {
      expect(() => client.compare('users', 'key1', null)).toThrow(ValidationError);
      expect(() => client.compare('users', 'key1', null)).toThrow('revision is required');
    });
  });

  describe('compareAdvanced', () => {
    it('should create a VALUE compare condition', () => {
      const condition = client.compareAdvanced({
        entityType: 'config',
        key: 'settings',
        target: CompareTarget.VALUE,
        result: CompareResult.EQUAL,
        value: { enabled: true },
      });

      expect(condition).toEqual({
        entityType: 'config',
        key: 'settings',
        target: CompareTarget.VALUE,
        result: CompareResult.EQUAL,
        value: { enabled: true },
        revision: undefined,
      });
    });

    it('should create a VERSION compare condition', () => {
      const condition = client.compareAdvanced({
        entityType: 'users',
        key: 'user:123',
        target: CompareTarget.VERSION,
        result: CompareResult.GREATER,
        revision: 0,
      });

      expect(condition).toEqual({
        entityType: 'users',
        key: 'user:123',
        target: CompareTarget.VERSION,
        result: CompareResult.GREATER,
        value: undefined,
        revision: 0,
      });
    });

    it('should create a MOD_REVISION compare condition', () => {
      const condition = client.compareAdvanced({
        entityType: 'data',
        key: 'key1',
        target: CompareTarget.MOD_REVISION,
        result: CompareResult.LESS,
        revision: 100,
      });

      expect(condition).toEqual({
        entityType: 'data',
        key: 'key1',
        target: CompareTarget.MOD_REVISION,
        result: CompareResult.LESS,
        value: undefined,
        revision: 100,
      });
    });

    it('should create a CREATE_REVISION compare condition', () => {
      const condition = client.compareAdvanced({
        entityType: 'data',
        key: 'key1',
        target: CompareTarget.CREATE_REVISION,
        result: CompareResult.NOT_EQUAL,
        revision: 50,
      });

      expect(condition).toEqual({
        entityType: 'data',
        key: 'key1',
        target: CompareTarget.CREATE_REVISION,
        result: CompareResult.NOT_EQUAL,
        value: undefined,
        revision: 50,
      });
    });

    it('should throw ValidationError when options is null', () => {
      expect(() => client.compareAdvanced(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when options is not an object', () => {
      expect(() => client.compareAdvanced('invalid')).toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is missing', () => {
      expect(() =>
        client.compareAdvanced({
          key: 'key1',
          target: CompareTarget.VALUE,
          result: CompareResult.EQUAL,
          value: 'test',
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is missing', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          target: CompareTarget.VALUE,
          result: CompareResult.EQUAL,
          value: 'test',
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when target is missing', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          result: CompareResult.EQUAL,
          value: 'test',
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when result is missing', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          target: CompareTarget.VALUE,
          value: 'test',
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when target is invalid', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          target: 'INVALID_TARGET',
          result: CompareResult.EQUAL,
          value: 'test',
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when result is invalid', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          target: CompareTarget.VALUE,
          result: 'INVALID_RESULT',
          value: 'test',
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when VALUE comparison is missing value', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          target: CompareTarget.VALUE,
          result: CompareResult.EQUAL,
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when VERSION comparison is missing revision', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          target: CompareTarget.VERSION,
          result: CompareResult.GREATER,
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when MOD_REVISION comparison is missing revision', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          target: CompareTarget.MOD_REVISION,
          result: CompareResult.EQUAL,
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when CREATE_REVISION comparison is missing revision', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          target: CompareTarget.CREATE_REVISION,
          result: CompareResult.EQUAL,
        }),
      ).toThrow(ValidationError);
    });

    it('should throw ValidationError when revision is negative', () => {
      expect(() =>
        client.compareAdvanced({
          entityType: 'users',
          key: 'key1',
          target: CompareTarget.VERSION,
          result: CompareResult.EQUAL,
          revision: -1,
        }),
      ).toThrow(ValidationError);
    });
  });

  describe('putOp', () => {
    it('should create a PUT operation', () => {
      const op = client.putOp('users', 'user:123', { name: 'Alice', email: 'alice@example.com' });

      expect(op).toEqual({
        type: TxnOpType.PUT,
        entityType: 'users',
        key: 'user:123',
        value: { name: 'Alice', email: 'alice@example.com' },
      });
    });

    it('should create a PUT operation with string value', () => {
      const op = client.putOp('config', 'setting1', 'value1');

      expect(op).toEqual({
        type: TxnOpType.PUT,
        entityType: 'config',
        key: 'setting1',
        value: 'value1',
      });
    });

    it('should create a PUT operation with number value', () => {
      const op = client.putOp('counters', 'count:1', 42);

      expect(op).toEqual({
        type: TxnOpType.PUT,
        entityType: 'counters',
        key: 'count:1',
        value: 42,
      });
    });

    it('should create a PUT operation with array value', () => {
      const op = client.putOp('lists', 'items', [1, 2, 3]);

      expect(op).toEqual({
        type: TxnOpType.PUT,
        entityType: 'lists',
        key: 'items',
        value: [1, 2, 3],
      });
    });

    it('should throw ValidationError when entityType is empty', () => {
      expect(() => client.putOp('', 'key1', 'value')).toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', () => {
      expect(() => client.putOp(null, 'key1', 'value')).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', () => {
      expect(() => client.putOp('users', '', 'value')).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is null', () => {
      expect(() => client.putOp('users', null, 'value')).toThrow(ValidationError);
    });

    it('should throw ValidationError when value is null', () => {
      expect(() => client.putOp('users', 'key1', null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when value is undefined', () => {
      expect(() => client.putOp('users', 'key1', undefined)).toThrow(ValidationError);
    });
  });

  describe('getOp', () => {
    it('should create a GET operation', () => {
      const op = client.getOp('users', 'user:123');

      expect(op).toEqual({
        type: TxnOpType.GET,
        entityType: 'users',
        key: 'user:123',
      });
    });

    it('should throw ValidationError when entityType is empty', () => {
      expect(() => client.getOp('', 'key1')).toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', () => {
      expect(() => client.getOp(null, 'key1')).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', () => {
      expect(() => client.getOp('users', '')).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is null', () => {
      expect(() => client.getOp('users', null)).toThrow(ValidationError);
    });
  });

  describe('deleteOp', () => {
    it('should create a DELETE operation', () => {
      const op = client.deleteOp('users', 'user:123');

      expect(op).toEqual({
        type: TxnOpType.DELETE,
        entityType: 'users',
        key: 'user:123',
      });
    });

    it('should throw ValidationError when entityType is empty', () => {
      expect(() => client.deleteOp('', 'key1')).toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is null', () => {
      expect(() => client.deleteOp(null, 'key1')).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is empty', () => {
      expect(() => client.deleteOp('users', '')).toThrow(ValidationError);
    });

    it('should throw ValidationError when key is null', () => {
      expect(() => client.deleteOp('users', null)).toThrow(ValidationError);
    });
  });

  describe('transaction', () => {
    it('should execute a transaction with conditions and success operations', async () => {
      mockTransport.post.mockResolvedValueOnce({
        succeeded: true,
        revision: 100,
        responses: [{ type: 'PUT' }],
      });

      const result = await client.transaction({
        conditions: [client.compare('users', 'user:123', 5)],
        success: [client.putOp('users', 'user:123', { name: 'Updated' })],
      });

      expect(result).toEqual({
        succeeded: true,
        revision: 100,
        responses: [{ type: 'PUT' }],
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/transaction',
        expect.objectContaining({
          compare: expect.any(Array),
          success: expect.any(Array),
          failure: [],
        }),
      );
    });

    it('should execute a transaction with failure operations', async () => {
      mockTransport.post.mockResolvedValueOnce({
        succeeded: false,
        revision: 50,
        responses: [{ type: 'GET', value: btoa(JSON.stringify({ name: 'Original' })) }],
      });

      const result = await client.transaction({
        conditions: [client.compare('users', 'user:123', 999)],
        success: [client.putOp('users', 'user:123', { name: 'Updated' })],
        failure: [client.getOp('users', 'user:123')],
      });

      expect(result.succeeded).toBe(false);
      expect(result.revision).toBe(50);
      expect(result.responses[0].value).toEqual({ name: 'Original' });
    });

    it('should execute a transaction with only success operations (no conditions)', async () => {
      mockTransport.post.mockResolvedValueOnce({
        succeeded: true,
        revision: 10,
        responses: [{ type: 'PUT' }, { type: 'PUT' }],
      });

      const result = await client.transaction({
        success: [
          client.putOp('users', 'user:1', { name: 'Alice' }),
          client.putOp('users', 'user:2', { name: 'Bob' }),
        ],
      });

      expect(result.succeeded).toBe(true);
      expect(result.responses).toHaveLength(2);
    });

    it('should execute a transaction with multiple conditions', async () => {
      mockTransport.post.mockResolvedValueOnce({
        succeeded: true,
        revision: 200,
        responses: [],
      });

      await client.transaction({
        conditions: [
          client.compare('users', 'user:1', 5),
          client.compare('users', 'user:2', 10),
          client.compare('config', 'version', 1),
        ],
        success: [client.putOp('config', 'version', 2)],
      });

      const call = mockTransport.post.mock.calls[0];
      expect(call[1].compare).toHaveLength(3);
    });

    it('should execute a transaction with DELETE operations', async () => {
      mockTransport.post.mockResolvedValueOnce({
        succeeded: true,
        revision: 150,
        responses: [{ type: 'DELETE' }],
      });

      await client.transaction({
        conditions: [client.compare('users', 'user:123', 5)],
        success: [client.deleteOp('users', 'user:123')],
      });

      const call = mockTransport.post.mock.calls[0];
      expect(call[1].success[0].type).toBe('DELETE');
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.transaction({
        success: [client.putOp('users', 'user:1', 'test')],
      });

      expect(result).toEqual({
        succeeded: false,
        revision: 0,
        responses: [],
      });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.transaction({
        success: [client.putOp('users', 'user:1', 'test')],
      });

      expect(result).toEqual({
        succeeded: false,
        revision: 0,
        responses: [],
      });
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.transaction(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is not an object', async () => {
      await expect(client.transaction('invalid')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when conditions is not an array', async () => {
      await expect(
        client.transaction({
          conditions: 'not-an-array',
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when success is not an array', async () => {
      await expect(
        client.transaction({
          conditions: [client.compare('users', 'key1', 5)],
          success: 'not-an-array',
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when failure is not an array', async () => {
      await expect(
        client.transaction({
          conditions: [client.compare('users', 'key1', 5)],
          success: [],
          failure: 'not-an-array',
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when transaction has no conditions or operations', async () => {
      await expect(client.transaction({})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when transaction has empty conditions, success, and failure', async () => {
      await expect(
        client.transaction({
          conditions: [],
          success: [],
          failure: [],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition is not an object', async () => {
      await expect(
        client.transaction({
          conditions: ['not-an-object'],
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition is missing key', async () => {
      await expect(
        client.transaction({
          conditions: [{ target: CompareTarget.VERSION, result: CompareResult.EQUAL, revision: 1 }],
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition has empty key', async () => {
      await expect(
        client.transaction({
          conditions: [{ key: '', target: CompareTarget.VERSION, result: CompareResult.EQUAL, revision: 1 }],
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition has whitespace-only key', async () => {
      await expect(
        client.transaction({
          conditions: [{ key: '   ', target: CompareTarget.VERSION, result: CompareResult.EQUAL, revision: 1 }],
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition is missing target', async () => {
      await expect(
        client.transaction({
          conditions: [{ key: 'key1', result: CompareResult.EQUAL, revision: 1 }],
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition is missing result', async () => {
      await expect(
        client.transaction({
          conditions: [{ key: 'key1', target: CompareTarget.VERSION, revision: 1 }],
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition has invalid target', async () => {
      await expect(
        client.transaction({
          conditions: [{ key: 'key1', target: 'INVALID', result: CompareResult.EQUAL, revision: 1 }],
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition has invalid result', async () => {
      await expect(
        client.transaction({
          conditions: [{ key: 'key1', target: CompareTarget.VERSION, result: 'INVALID', revision: 1 }],
          success: [client.putOp('users', 'key1', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when success operation is not an object', async () => {
      await expect(
        client.transaction({
          success: ['not-an-object'],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when success operation is missing entityType', async () => {
      await expect(
        client.transaction({
          success: [{ type: TxnOpType.PUT, key: 'key1', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when success operation has empty entityType', async () => {
      await expect(
        client.transaction({
          success: [{ type: TxnOpType.PUT, entityType: '', key: 'key1', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when success operation is missing key', async () => {
      await expect(
        client.transaction({
          success: [{ type: TxnOpType.PUT, entityType: 'users', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when success operation has empty key', async () => {
      await expect(
        client.transaction({
          success: [{ type: TxnOpType.PUT, entityType: 'users', key: '', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when success operation is missing type', async () => {
      await expect(
        client.transaction({
          success: [{ entityType: 'users', key: 'key1', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when success operation has invalid type', async () => {
      await expect(
        client.transaction({
          success: [{ type: 'INVALID', entityType: 'users', key: 'key1', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when PUT operation is missing value', async () => {
      await expect(
        client.transaction({
          success: [{ type: TxnOpType.PUT, entityType: 'users', key: 'key1' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when failure operation is invalid', async () => {
      await expect(
        client.transaction({
          conditions: [client.compare('users', 'key1', 5)],
          success: [client.putOp('users', 'key1', 'test')],
          failure: [{ type: 'INVALID', entityType: 'users', key: 'key1' }],
        }),
      ).rejects.toThrow(ValidationError);
    });
  });

  describe('_buildPayload', () => {
    it('should build payload with conditions and operations', () => {
      const request = {
        conditions: [client.compare('users', 'user:123', 5)],
        success: [client.putOp('users', 'user:123', { name: 'Test' })],
        failure: [client.getOp('users', 'user:123')],
      };

      const payload = client._buildPayload(request);

      expect(payload.compare).toHaveLength(1);
      expect(payload.compare[0]).toEqual({
        key: 'users:user:123',
        target: CompareTarget.MOD_REVISION,
        result: CompareResult.EQUAL,
        revision: 5,
      });
      expect(payload.success).toHaveLength(1);
      expect(payload.success[0].type).toBe('PUT');
      expect(payload.failure).toHaveLength(1);
      expect(payload.failure[0].type).toBe('GET');
    });

    it('should handle empty arrays', () => {
      const request = {
        conditions: [],
        success: [client.putOp('users', 'key1', 'test')],
        failure: [],
      };

      const payload = client._buildPayload(request);

      expect(payload.compare).toEqual([]);
      expect(payload.success).toHaveLength(1);
      expect(payload.failure).toEqual([]);
    });

    it('should handle undefined arrays', () => {
      const request = {
        success: [client.putOp('users', 'key1', 'test')],
      };

      const payload = client._buildPayload(request);

      expect(payload.compare).toEqual([]);
      expect(payload.failure).toEqual([]);
    });
  });

  describe('_serializeCondition', () => {
    it('should serialize condition with entityType', () => {
      const condition = {
        entityType: 'users',
        key: 'user:123',
        target: CompareTarget.MOD_REVISION,
        result: CompareResult.EQUAL,
        revision: 10,
      };

      const serialized = client._serializeCondition(condition);

      expect(serialized).toEqual({
        key: 'users:user:123',
        target: CompareTarget.MOD_REVISION,
        result: CompareResult.EQUAL,
        revision: 10,
      });
    });

    it('should serialize condition without entityType', () => {
      const condition = {
        key: 'standalone:key',
        target: CompareTarget.VERSION,
        result: CompareResult.GREATER,
        revision: 0,
      };

      const serialized = client._serializeCondition(condition);

      expect(serialized.key).toBe('standalone:key');
    });

    it('should serialize condition with value', () => {
      const condition = {
        entityType: 'config',
        key: 'setting',
        target: CompareTarget.VALUE,
        result: CompareResult.EQUAL,
        value: { enabled: true },
      };

      const serialized = client._serializeCondition(condition);

      expect(serialized.value).toBeDefined();
    });
  });

  describe('_serializeOperation', () => {
    it('should serialize PUT operation', () => {
      const op = {
        type: TxnOpType.PUT,
        entityType: 'users',
        key: 'user:1',
        value: { name: 'Test' },
      };

      const serialized = client._serializeOperation(op);

      expect(serialized).toEqual({
        type: 'PUT',
        entityType: 'users',
        key: 'user:1',
        value: expect.any(String),
      });
    });

    it('should serialize GET operation', () => {
      const op = {
        type: TxnOpType.GET,
        entityType: 'users',
        key: 'user:1',
      };

      const serialized = client._serializeOperation(op);

      expect(serialized).toEqual({
        type: 'GET',
        entityType: 'users',
        key: 'user:1',
      });
    });

    it('should serialize DELETE operation', () => {
      const op = {
        type: TxnOpType.DELETE,
        entityType: 'users',
        key: 'user:1',
      };

      const serialized = client._serializeOperation(op);

      expect(serialized).toEqual({
        type: 'DELETE',
        entityType: 'users',
        key: 'user:1',
      });
    });
  });

  describe('_encodeValue', () => {
    it('should encode string value', () => {
      const encoded = client._encodeValue('hello');

      expect(encoded).toBe(btoa('hello'));
    });

    it('should encode object value as JSON', () => {
      const value = { name: 'Test', count: 42 };
      const encoded = client._encodeValue(value);

      expect(atob(encoded)).toBe(JSON.stringify(value));
    });

    it('should encode array value as JSON', () => {
      const value = [1, 2, 3];
      const encoded = client._encodeValue(value);

      expect(atob(encoded)).toBe(JSON.stringify(value));
    });

    it('should encode Uint8Array value', () => {
      const value = new Uint8Array([72, 101, 108, 108, 111]);
      const encoded = client._encodeValue(value);

      expect(encoded).toBe(btoa('Hello'));
    });

    it('should encode number value as JSON', () => {
      const encoded = client._encodeValue(42);

      expect(atob(encoded)).toBe('42');
    });

    it('should encode boolean value as JSON', () => {
      const encoded = client._encodeValue(true);

      expect(atob(encoded)).toBe('true');
    });
  });

  describe('_parseResponse', () => {
    it('should parse successful response', () => {
      const response = {
        succeeded: true,
        revision: 100,
        responses: [
          { type: 'PUT' },
          { type: 'GET', value: btoa(JSON.stringify({ name: 'Test' })) },
        ],
      };

      const parsed = client._parseResponse(response);

      expect(parsed).toEqual({
        succeeded: true,
        revision: 100,
        responses: [
          { type: 'PUT' },
          { type: 'GET', value: { name: 'Test' } },
        ],
      });
    });

    it('should parse failed response', () => {
      const response = {
        succeeded: false,
        revision: 50,
        responses: [],
      };

      const parsed = client._parseResponse(response);

      expect(parsed).toEqual({
        succeeded: false,
        revision: 50,
        responses: [],
      });
    });

    it('should handle null response', () => {
      const parsed = client._parseResponse(null);

      expect(parsed).toEqual({
        succeeded: false,
        revision: 0,
        responses: [],
      });
    });

    it('should handle undefined fields', () => {
      const parsed = client._parseResponse({});

      expect(parsed).toEqual({
        succeeded: false,
        revision: 0,
        responses: [],
      });
    });
  });

  describe('_parseOpResponse', () => {
    it('should parse operation response with value', () => {
      const opResponse = {
        type: 'GET',
        value: btoa(JSON.stringify({ name: 'Test' })),
      };

      const parsed = client._parseOpResponse(opResponse);

      expect(parsed).toEqual({
        type: 'GET',
        value: { name: 'Test' },
      });
    });

    it('should parse operation response without value', () => {
      const opResponse = {
        type: 'PUT',
      };

      const parsed = client._parseOpResponse(opResponse);

      expect(parsed).toEqual({
        type: 'PUT',
      });
    });

    it('should handle null value', () => {
      const opResponse = {
        type: 'GET',
        value: null,
      };

      const parsed = client._parseOpResponse(opResponse);

      expect(parsed).toEqual({
        type: 'GET',
      });
    });
  });

  describe('_decodeValue', () => {
    it('should decode base64 JSON string', () => {
      const encoded = btoa(JSON.stringify({ name: 'Test' }));
      const decoded = client._decodeValue(encoded);

      expect(decoded).toEqual({ name: 'Test' });
    });

    it('should decode base64 plain string', () => {
      const encoded = btoa('hello world');
      const decoded = client._decodeValue(encoded);

      expect(decoded).toBe('hello world');
    });

    it('should return non-string value as-is', () => {
      const decoded = client._decodeValue(42);

      expect(decoded).toBe(42);
    });

    it('should return invalid base64 string as-is', () => {
      const decoded = client._decodeValue('not-valid-base64!!!');

      expect(decoded).toBe('not-valid-base64!!!');
    });

    it('should decode array', () => {
      const encoded = btoa(JSON.stringify([1, 2, 3]));
      const decoded = client._decodeValue(encoded);

      expect(decoded).toEqual([1, 2, 3]);
    });
  });

  describe('_uint8ArrayToBase64', () => {
    it('should convert Uint8Array to base64', () => {
      const bytes = new Uint8Array([72, 101, 108, 108, 111]);
      const result = client._uint8ArrayToBase64(bytes);

      expect(result).toBe(btoa('Hello'));
    });

    it('should handle empty Uint8Array', () => {
      const bytes = new Uint8Array([]);
      const result = client._uint8ArrayToBase64(bytes);

      expect(result).toBe('');
    });
  });
});
