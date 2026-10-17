/**
 * QuicBaaS JavaScript SDK - Query Client Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import QueryClient, {
  Operator,
  eq,
  neq,
  gt,
  gte,
  lt,
  lte,
  between,
  prefix,
} from '../../src/clients/query.js';
import { ValidationError } from '../../src/errors/index.js';

describe('QueryClient', () => {
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
    client = new QueryClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const queryClient = new QueryClient(mockTransport);
      expect(queryClient).toBeInstanceOf(QueryClient);
      expect(queryClient.transport).toBe(mockTransport);
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new QueryClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport is undefined', () => {
      expect(() => new QueryClient(undefined)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport is falsy', () => {
      expect(() => new QueryClient(0)).toThrow(ValidationError);
      expect(() => new QueryClient('')).toThrow(ValidationError);
      expect(() => new QueryClient(false)).toThrow(ValidationError);
    });
  });

  describe('exported Operator enum', () => {
    it('should export all operator values', () => {
      expect(Operator.EQUAL).toBe('EQUAL');
      expect(Operator.NOT_EQUAL).toBe('NOT_EQUAL');
      expect(Operator.LESS_THAN).toBe('LESS_THAN');
      expect(Operator.LESS_THAN_OR_EQUAL).toBe('LESS_THAN_OR_EQUAL');
      expect(Operator.GREATER_THAN).toBe('GREATER_THAN');
      expect(Operator.GREATER_THAN_OR_EQUAL).toBe('GREATER_THAN_OR_EQUAL');
      expect(Operator.PREFIX).toBe('PREFIX');
      expect(Operator.RANGE).toBe('RANGE');
    });
  });

  describe('condition helper functions', () => {
    describe('eq', () => {
      it('should create an EQUAL condition', () => {
        const condition = eq('status', 'active');

        expect(condition).toEqual({
          field: 'status',
          operator: Operator.EQUAL,
          value: 'active',
        });
      });

      it('should work with numeric value', () => {
        const condition = eq('age', 25);

        expect(condition).toEqual({
          field: 'age',
          operator: Operator.EQUAL,
          value: 25,
        });
      });

      it('should work with boolean value', () => {
        const condition = eq('enabled', true);

        expect(condition).toEqual({
          field: 'enabled',
          operator: Operator.EQUAL,
          value: true,
        });
      });

      it('should work with object value', () => {
        const condition = eq('data', { nested: 'value' });

        expect(condition).toEqual({
          field: 'data',
          operator: Operator.EQUAL,
          value: { nested: 'value' },
        });
      });

      it('should throw ValidationError when field is empty', () => {
        expect(() => eq('', 'value')).toThrow(ValidationError);
      });

      it('should throw ValidationError when field is null', () => {
        expect(() => eq(null, 'value')).toThrow(ValidationError);
      });

      it('should throw ValidationError when value is null', () => {
        expect(() => eq('field', null)).toThrow(ValidationError);
      });

      it('should throw ValidationError when value is undefined', () => {
        expect(() => eq('field', undefined)).toThrow(ValidationError);
      });
    });

    describe('neq', () => {
      it('should create a NOT_EQUAL condition', () => {
        const condition = neq('status', 'deleted');

        expect(condition).toEqual({
          field: 'status',
          operator: Operator.NOT_EQUAL,
          value: 'deleted',
        });
      });

      it('should throw ValidationError when field is empty', () => {
        expect(() => neq('', 'value')).toThrow(ValidationError);
      });

      it('should throw ValidationError when value is null', () => {
        expect(() => neq('field', null)).toThrow(ValidationError);
      });
    });

    describe('gt', () => {
      it('should create a GREATER_THAN condition', () => {
        const condition = gt('age', 18);

        expect(condition).toEqual({
          field: 'age',
          operator: Operator.GREATER_THAN,
          value: 18,
        });
      });

      it('should work with string comparison', () => {
        const condition = gt('name', 'A');

        expect(condition).toEqual({
          field: 'name',
          operator: Operator.GREATER_THAN,
          value: 'A',
        });
      });

      it('should throw ValidationError when field is empty', () => {
        expect(() => gt('', 10)).toThrow(ValidationError);
      });

      it('should throw ValidationError when value is null', () => {
        expect(() => gt('field', null)).toThrow(ValidationError);
      });
    });

    describe('gte', () => {
      it('should create a GREATER_THAN_OR_EQUAL condition', () => {
        const condition = gte('score', 100);

        expect(condition).toEqual({
          field: 'score',
          operator: Operator.GREATER_THAN_OR_EQUAL,
          value: 100,
        });
      });

      it('should throw ValidationError when field is empty', () => {
        expect(() => gte('', 10)).toThrow(ValidationError);
      });

      it('should throw ValidationError when value is null', () => {
        expect(() => gte('field', null)).toThrow(ValidationError);
      });
    });

    describe('lt', () => {
      it('should create a LESS_THAN condition', () => {
        const condition = lt('price', 50);

        expect(condition).toEqual({
          field: 'price',
          operator: Operator.LESS_THAN,
          value: 50,
        });
      });

      it('should throw ValidationError when field is empty', () => {
        expect(() => lt('', 10)).toThrow(ValidationError);
      });

      it('should throw ValidationError when value is null', () => {
        expect(() => lt('field', null)).toThrow(ValidationError);
      });
    });

    describe('lte', () => {
      it('should create a LESS_THAN_OR_EQUAL condition', () => {
        const condition = lte('quantity', 0);

        expect(condition).toEqual({
          field: 'quantity',
          operator: Operator.LESS_THAN_OR_EQUAL,
          value: 0,
        });
      });

      it('should throw ValidationError when field is empty', () => {
        expect(() => lte('', 10)).toThrow(ValidationError);
      });

      it('should throw ValidationError when value is null', () => {
        expect(() => lte('field', null)).toThrow(ValidationError);
      });
    });

    describe('between', () => {
      it('should create a RANGE condition', () => {
        const condition = between('age', 18, 65);

        expect(condition).toEqual({
          field: 'age',
          operator: Operator.RANGE,
          value: 18,
          maxValue: 65,
        });
      });

      it('should work with string range', () => {
        const condition = between('name', 'A', 'M');

        expect(condition).toEqual({
          field: 'name',
          operator: Operator.RANGE,
          value: 'A',
          maxValue: 'M',
        });
      });

      it('should work with date strings', () => {
        const condition = between('createdAt', '2025-01-01', '2025-12-31');

        expect(condition).toEqual({
          field: 'createdAt',
          operator: Operator.RANGE,
          value: '2025-01-01',
          maxValue: '2025-12-31',
        });
      });

      it('should throw ValidationError when field is empty', () => {
        expect(() => between('', 1, 10)).toThrow(ValidationError);
      });

      it('should throw ValidationError when min is null', () => {
        expect(() => between('field', null, 10)).toThrow(ValidationError);
      });

      it('should throw ValidationError when max is null', () => {
        expect(() => between('field', 1, null)).toThrow(ValidationError);
      });

      it('should throw ValidationError when min is undefined', () => {
        expect(() => between('field', undefined, 10)).toThrow(ValidationError);
      });

      it('should throw ValidationError when max is undefined', () => {
        expect(() => between('field', 1, undefined)).toThrow(ValidationError);
      });
    });

    describe('prefix', () => {
      it('should create a PREFIX condition', () => {
        const condition = prefix('email', 'admin@');

        expect(condition).toEqual({
          field: 'email',
          operator: Operator.PREFIX,
          value: 'admin@',
        });
      });

      it('should throw ValidationError when field is empty', () => {
        expect(() => prefix('', 'test')).toThrow(ValidationError);
      });

      it('should throw ValidationError when field is null', () => {
        expect(() => prefix(null, 'test')).toThrow(ValidationError);
      });

      it('should throw ValidationError when prefix is empty', () => {
        expect(() => prefix('field', '')).toThrow(ValidationError);
      });

      it('should throw ValidationError when prefix is null', () => {
        expect(() => prefix('field', null)).toThrow(ValidationError);
      });
    });
  });

  describe('query', () => {
    it('should execute a query with single condition', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [
          { key: 'user:1', value: { name: 'Alice', status: 'active' } },
          { key: 'user:2', value: { name: 'Bob', status: 'active' } },
        ],
        nextCursor: null,
        hasMore: false,
      });

      const result = await client.query({
        index: 'users_by_status',
        entityType: 'user',
        conditions: [eq('status', 'active')],
      });

      expect(result).toEqual({
        results: [
          { key: 'user:1', value: { name: 'Alice', status: 'active' } },
          { key: 'user:2', value: { name: 'Bob', status: 'active' } },
        ],
        nextCursor: null,
        hasMore: false,
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query',
        {
          index: 'users_by_status',
          entityType: 'user',
          conditions: [{ field: 'status', operator: 'EQUAL', value: 'active' }],
        },
      );
    });

    it('should execute a query with multiple conditions', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [{ key: 'product:1', value: { price: 50, category: 'electronics' } }],
        nextCursor: null,
        hasMore: false,
      });

      const result = await client.query({
        index: 'products_by_category_price',
        entityType: 'product',
        conditions: [
          eq('category', 'electronics'),
          between('price', 25, 100),
        ],
      });

      expect(result.results).toHaveLength(1);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query',
        {
          index: 'products_by_category_price',
          entityType: 'product',
          conditions: [
            { field: 'category', operator: 'EQUAL', value: 'electronics' },
            { field: 'price', operator: 'RANGE', value: 25, maxValue: 100 },
          ],
        },
      );
    });

    it('should execute a query with limit', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [{ key: 'item:1', value: 'test' }],
        nextCursor: 'cursor123',
        hasMore: true,
      });

      const result = await client.query({
        index: 'items_index',
        entityType: 'item',
        conditions: [eq('type', 'test')],
        limit: 10,
      });

      expect(result.hasMore).toBe(true);
      expect(result.nextCursor).toBe('cursor123');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query',
        expect.objectContaining({ limit: 10 }),
      );
    });

    it('should execute a query with offset', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        nextCursor: null,
        hasMore: false,
      });

      await client.query({
        index: 'items_index',
        entityType: 'item',
        conditions: [eq('type', 'test')],
        offset: 50,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query',
        expect.objectContaining({ offset: 50 }),
      );
    });

    it('should execute a query with cursor', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [{ key: 'item:11', value: 'test' }],
        nextCursor: null,
        hasMore: false,
      });

      await client.query({
        index: 'items_index',
        entityType: 'item',
        conditions: [eq('type', 'test')],
        cursor: 'cursor123',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query',
        expect.objectContaining({ cursor: 'cursor123' }),
      );
    });

    it('should execute a query with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        nextCursor: 'next',
        hasMore: true,
      });

      await client.query({
        index: 'complex_index',
        entityType: 'entity',
        conditions: [eq('field', 'value')],
        limit: 25,
        offset: 10,
        cursor: 'prev',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query',
        {
          index: 'complex_index',
          entityType: 'entity',
          conditions: [{ field: 'field', operator: 'EQUAL', value: 'value' }],
          limit: 25,
          offset: 10,
          cursor: 'prev',
        },
      );
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.query({
        index: 'test_index',
        entityType: 'test',
        conditions: [eq('field', 'value')],
      });

      expect(result).toEqual({
        results: [],
        nextCursor: null,
        hasMore: false,
      });
    });

    it('should handle undefined fields in response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.query({
        index: 'test_index',
        entityType: 'test',
        conditions: [eq('field', 'value')],
      });

      expect(result).toEqual({
        results: [],
        nextCursor: null,
        hasMore: false,
      });
    });

    it('should handle next_cursor snake_case in response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        next_cursor: 'snake_cursor',
        has_more: true,
      });

      const result = await client.query({
        index: 'test_index',
        entityType: 'test',
        conditions: [eq('field', 'value')],
      });

      expect(result.nextCursor).toBe('snake_cursor');
      expect(result.hasMore).toBe(true);
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.query(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is not an object', async () => {
      await expect(client.query('invalid')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when index is missing', async () => {
      await expect(
        client.query({
          entityType: 'test',
          conditions: [eq('field', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when index is empty', async () => {
      await expect(
        client.query({
          index: '',
          entityType: 'test',
          conditions: [eq('field', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is missing', async () => {
      await expect(
        client.query({
          index: 'test_index',
          conditions: [eq('field', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when entityType is empty', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: '',
          conditions: [eq('field', 'value')],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when conditions is missing', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when conditions is not an array', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: 'not-an-array',
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when conditions is empty', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition is not an object', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: ['not-an-object'],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition.field is missing', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [{ operator: 'EQUAL', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition.field is empty', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [{ field: '', operator: 'EQUAL', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition.operator is missing', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [{ field: 'test', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition.operator is invalid', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [{ field: 'test', operator: 'INVALID', value: 'test' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when condition.value is missing', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [{ field: 'test', operator: 'EQUAL' }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when RANGE condition is missing maxValue', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [{ field: 'age', operator: 'RANGE', value: 18 }],
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is negative', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [eq('field', 'value')],
          limit: -1,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is not an integer', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [eq('field', 'value')],
          limit: 1.5,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should allow limit of 0', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [], nextCursor: null, hasMore: false });

      await client.query({
        index: 'test_index',
        entityType: 'test',
        conditions: [eq('field', 'value')],
        limit: 0,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query',
        expect.objectContaining({ limit: 0 }),
      );
    });

    it('should throw ValidationError when offset is negative', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [eq('field', 'value')],
          offset: -1,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when offset is not an integer', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [eq('field', 'value')],
          offset: 1.5,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should allow offset of 0', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [], nextCursor: null, hasMore: false });

      await client.query({
        index: 'test_index',
        entityType: 'test',
        conditions: [eq('field', 'value')],
        offset: 0,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query',
        expect.objectContaining({ offset: 0 }),
      );
    });

    it('should throw ValidationError when cursor is not a string', async () => {
      await expect(
        client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [eq('field', 'value')],
          cursor: 123,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should include condition index in validation error', async () => {
      try {
        await client.query({
          index: 'test_index',
          entityType: 'test',
          conditions: [
            eq('field1', 'value1'),
            { field: 'field2', operator: 'INVALID', value: 'value2' },
          ],
        });
        throw new Error('Expected ValidationError');
      } catch (error) {
        expect(error).toBeInstanceOf(ValidationError);
        expect(error.details.field).toContain('conditions[1]');
      }
    });
  });

  describe('scanIndex', () => {
    it('should scan an index without options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [
          { key: 'item:1', value: { name: 'Item 1' } },
          { key: 'item:2', value: { name: 'Item 2' } },
        ],
        nextCursor: null,
        hasMore: false,
      });

      const result = await client.scanIndex({
        index: 'items_index',
      });

      expect(result).toEqual({
        results: [
          { key: 'item:1', value: { name: 'Item 1' } },
          { key: 'item:2', value: { name: 'Item 2' } },
        ],
        nextCursor: null,
        hasMore: false,
      });
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query/scan',
        { index: 'items_index' },
      );
    });

    it('should scan an index with prefix', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [{ key: 'admin:1', value: { role: 'admin' } }],
        nextCursor: null,
        hasMore: false,
      });

      const result = await client.scanIndex({
        index: 'users_index',
        prefix: 'admin:',
      });

      expect(result.results).toHaveLength(1);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query/scan',
        { index: 'users_index', prefix: 'admin:' },
      );
    });

    it('should scan an index with limit', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [{ key: 'item:1', value: 'test' }],
        nextCursor: 'cursor',
        hasMore: true,
      });

      await client.scanIndex({
        index: 'items_index',
        limit: 100,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query/scan',
        { index: 'items_index', limit: 100 },
      );
    });

    it('should scan an index with offset', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        nextCursor: null,
        hasMore: false,
      });

      await client.scanIndex({
        index: 'items_index',
        offset: 50,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query/scan',
        { index: 'items_index', offset: 50 },
      );
    });

    it('should scan an index with cursor', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        nextCursor: null,
        hasMore: false,
      });

      await client.scanIndex({
        index: 'items_index',
        cursor: 'prev_cursor',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query/scan',
        { index: 'items_index', cursor: 'prev_cursor' },
      );
    });

    it('should scan an index with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [],
        nextCursor: 'next',
        hasMore: true,
      });

      await client.scanIndex({
        index: 'complex_index',
        prefix: 'test:',
        limit: 50,
        offset: 10,
        cursor: 'start',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query/scan',
        {
          index: 'complex_index',
          prefix: 'test:',
          limit: 50,
          offset: 10,
          cursor: 'start',
        },
      );
    });

    it('should handle null response gracefully', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.scanIndex({
        index: 'test_index',
      });

      expect(result).toEqual({
        results: [],
        nextCursor: null,
        hasMore: false,
      });
    });

    it('should throw ValidationError when request is null', async () => {
      await expect(client.scanIndex(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when request is not an object', async () => {
      await expect(client.scanIndex('invalid')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when index is missing', async () => {
      await expect(client.scanIndex({})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when index is empty', async () => {
      await expect(client.scanIndex({ index: '' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when index is whitespace only', async () => {
      await expect(client.scanIndex({ index: '   ' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when prefix is not a string', async () => {
      await expect(
        client.scanIndex({
          index: 'test_index',
          prefix: 123,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should allow empty string prefix (becomes falsy, not included)', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [], nextCursor: null, hasMore: false });

      await client.scanIndex({
        index: 'test_index',
        prefix: '',
      });

      // Empty string is falsy, so prefix should not be included
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/query/scan',
        { index: 'test_index' },
      );
    });

    it('should throw ValidationError when limit is negative', async () => {
      await expect(
        client.scanIndex({
          index: 'test_index',
          limit: -1,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is not an integer', async () => {
      await expect(
        client.scanIndex({
          index: 'test_index',
          limit: 1.5,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when offset is negative', async () => {
      await expect(
        client.scanIndex({
          index: 'test_index',
          offset: -1,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when offset is not an integer', async () => {
      await expect(
        client.scanIndex({
          index: 'test_index',
          offset: 1.5,
        }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when cursor is not a string', async () => {
      await expect(
        client.scanIndex({
          index: 'test_index',
          cursor: 123,
        }),
      ).rejects.toThrow(ValidationError);
    });
  });

  describe('_buildQueryRequestBody', () => {
    it('should build request body with required fields', () => {
      const request = {
        index: 'test_index',
        entityType: 'test',
        conditions: [eq('field', 'value')],
      };

      const body = client._buildQueryRequestBody(request);

      expect(body).toEqual({
        index: 'test_index',
        entityType: 'test',
        conditions: [{ field: 'field', operator: 'EQUAL', value: 'value' }],
      });
    });

    it('should include optional fields when present', () => {
      const request = {
        index: 'test_index',
        entityType: 'test',
        conditions: [eq('field', 'value')],
        limit: 10,
        offset: 5,
        cursor: 'abc',
      };

      const body = client._buildQueryRequestBody(request);

      expect(body).toEqual({
        index: 'test_index',
        entityType: 'test',
        conditions: [{ field: 'field', operator: 'EQUAL', value: 'value' }],
        limit: 10,
        offset: 5,
        cursor: 'abc',
      });
    });

    it('should not include null/undefined optional fields', () => {
      const request = {
        index: 'test_index',
        entityType: 'test',
        conditions: [eq('field', 'value')],
        limit: null,
        offset: undefined,
      };

      const body = client._buildQueryRequestBody(request);

      expect(body).not.toHaveProperty('limit');
      expect(body).not.toHaveProperty('offset');
    });
  });

  describe('_buildScanRequestBody', () => {
    it('should build request body with required fields', () => {
      const request = {
        index: 'test_index',
      };

      const body = client._buildScanRequestBody(request);

      expect(body).toEqual({
        index: 'test_index',
      });
    });

    it('should include optional fields when present', () => {
      const request = {
        index: 'test_index',
        prefix: 'test:',
        limit: 100,
        offset: 50,
        cursor: 'xyz',
      };

      const body = client._buildScanRequestBody(request);

      expect(body).toEqual({
        index: 'test_index',
        prefix: 'test:',
        limit: 100,
        offset: 50,
        cursor: 'xyz',
      });
    });

    it('should not include falsy prefix', () => {
      const request = {
        index: 'test_index',
        prefix: '',
      };

      const body = client._buildScanRequestBody(request);

      expect(body).not.toHaveProperty('prefix');
    });
  });

  describe('_serializeCondition', () => {
    it('should serialize condition without maxValue', () => {
      const condition = eq('field', 'value');
      const serialized = client._serializeCondition(condition);

      expect(serialized).toEqual({
        field: 'field',
        operator: 'EQUAL',
        value: 'value',
      });
    });

    it('should serialize condition with maxValue', () => {
      const condition = between('age', 18, 65);
      const serialized = client._serializeCondition(condition);

      expect(serialized).toEqual({
        field: 'age',
        operator: 'RANGE',
        value: 18,
        maxValue: 65,
      });
    });
  });

  describe('_normalizeQueryResponse', () => {
    it('should normalize complete response', () => {
      const response = {
        results: [{ key: 'k1', value: 'v1' }],
        nextCursor: 'cursor',
        hasMore: true,
      };

      const normalized = client._normalizeQueryResponse(response);

      expect(normalized).toEqual({
        results: [{ key: 'k1', value: 'v1' }],
        nextCursor: 'cursor',
        hasMore: true,
      });
    });

    it('should normalize response with snake_case fields', () => {
      const response = {
        results: [],
        next_cursor: 'snake',
        has_more: true,
      };

      const normalized = client._normalizeQueryResponse(response);

      expect(normalized).toEqual({
        results: [],
        nextCursor: 'snake',
        hasMore: true,
      });
    });

    it('should prioritize camelCase over snake_case', () => {
      const response = {
        results: [],
        nextCursor: 'camel',
        next_cursor: 'snake',
        hasMore: true,
        has_more: false,
      };

      const normalized = client._normalizeQueryResponse(response);

      expect(normalized.nextCursor).toBe('camel');
      expect(normalized.hasMore).toBe(true);
    });

    it('should handle null response', () => {
      const normalized = client._normalizeQueryResponse(null);

      expect(normalized).toEqual({
        results: [],
        nextCursor: null,
        hasMore: false,
      });
    });

    it('should handle undefined response', () => {
      const normalized = client._normalizeQueryResponse(undefined);

      expect(normalized).toEqual({
        results: [],
        nextCursor: null,
        hasMore: false,
      });
    });

    it('should handle non-array results', () => {
      const response = {
        results: 'not-an-array',
      };

      const normalized = client._normalizeQueryResponse(response);

      expect(normalized.results).toEqual([]);
    });
  });
});
