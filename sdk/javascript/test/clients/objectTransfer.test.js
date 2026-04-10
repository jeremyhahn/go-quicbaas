/**
 * ObjectTransferClient Tests
 *
 * Comprehensive tests for the ObjectTransferClient covering all public methods,
 * success cases, error handling, and edge cases.
 */

import ObjectTransferClient from '../../src/clients/objectTransfer.js';
import { ValidationError } from '../../src/errors/index.js';

describe('ObjectTransferClient', () => {
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
    client = new ObjectTransferClient(mockTransport);
  });

  // ==========================================================================
  // Constructor Tests
  // ==========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const newClient = new ObjectTransferClient(mockTransport);
      expect(newClient).toBeInstanceOf(ObjectTransferClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new ObjectTransferClient()).toThrow(ValidationError);
      expect(() => new ObjectTransferClient(null)).toThrow(ValidationError);
      expect(() => new ObjectTransferClient(undefined)).toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // fetchObject Tests
  // ==========================================================================

  describe('fetchObject', () => {
    it('should fetch a single object with minimal parameters', async () => {
      const mockResponse = {
        objectRef: 'obj-123',
        data: 'SGVsbG8gV29ybGQ=', // "Hello World" in base64
        checksum: 'abc123checksum',
        size: 11,
        compressed: false,
        compressionAlgo: '',
        found: true,
        errorMessage: '',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.fetchObject(1, 'obj-123');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/1/objects/obj-123',
        { query: {} }
      );
      expect(result.objectRef).toBe('obj-123');
      expect(result.data).toBe('SGVsbG8gV29ybGQ=');
      expect(result.checksum).toBe('abc123checksum');
      expect(result.size).toBe(11);
      expect(result.compressed).toBe(false);
      expect(result.compressionAlgo).toBe('');
      expect(result.found).toBe(true);
      expect(result.errorMessage).toBe('');
    });

    it('should fetch object with checksum option', async () => {
      const mockResponse = {
        objectRef: 'obj-456',
        data: 'dGVzdCBkYXRh',
        checksum: 'expected-checksum',
        size: 9,
        found: true,
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.fetchObject(2, 'obj-456', {
        checksum: 'expected-checksum',
      });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/2/objects/obj-456',
        { query: { checksum: 'expected-checksum' } }
      );
      expect(result.objectRef).toBe('obj-456');
      expect(result.found).toBe(true);
    });

    it('should fetch object with requestId option', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: true });

      await client.fetchObject(3, 'obj-789', {
        requestId: 'req-abc-123',
      });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/3/objects/obj-789',
        { query: { request_id: 'req-abc-123' } }
      );
    });

    it('should fetch object with all options', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: true });

      await client.fetchObject(4, 'obj-full', {
        checksum: 'my-checksum',
        requestId: 'req-full-123',
      });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/4/objects/obj-full',
        {
          query: {
            checksum: 'my-checksum',
            request_id: 'req-full-123',
          },
        }
      );
    });

    it('should handle compressed object response', async () => {
      const mockResponse = {
        objectRef: 'compressed-obj',
        data: 'H4sIAAAAAAAA/8tIzcnJVyjPL8pJAQCFEUoNCwAAAA==',
        checksum: 'compressed-checksum',
        size: 50,
        compressed: true,
        compressionAlgo: 'gzip',
        found: true,
        errorMessage: '',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.fetchObject(5, 'compressed-obj');

      expect(result.compressed).toBe(true);
      expect(result.compressionAlgo).toBe('gzip');
    });

    it('should handle not found response', async () => {
      const mockResponse = {
        objectRef: 'missing-obj',
        data: '',
        checksum: '',
        size: 0,
        compressed: false,
        compressionAlgo: '',
        found: false,
        errorMessage: 'Object not found',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.fetchObject(6, 'missing-obj');

      expect(result.found).toBe(false);
      expect(result.errorMessage).toBe('Object not found');
      expect(result.data).toBe('');
    });

    it('should throw ValidationError when shardId is missing', async () => {
      await expect(client.fetchObject(null, 'obj-123')).rejects.toThrow(ValidationError);
      await expect(client.fetchObject(undefined, 'obj-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is not a number', async () => {
      await expect(client.fetchObject('invalid', 'obj-123')).rejects.toThrow(ValidationError);
      await expect(client.fetchObject({}, 'obj-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is negative', async () => {
      await expect(client.fetchObject(-1, 'obj-123')).rejects.toThrow(ValidationError);
      await expect(client.fetchObject(-100, 'obj-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is not an integer', async () => {
      await expect(client.fetchObject(1.5, 'obj-123')).rejects.toThrow(ValidationError);
      await expect(client.fetchObject(3.14, 'obj-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when objectRef is missing', async () => {
      await expect(client.fetchObject(1, '')).rejects.toThrow(ValidationError);
      await expect(client.fetchObject(1, null)).rejects.toThrow(ValidationError);
      await expect(client.fetchObject(1, undefined)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when objectRef is whitespace only', async () => {
      await expect(client.fetchObject(1, '   ')).rejects.toThrow(ValidationError);
      await expect(client.fetchObject(1, '\t\n')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.fetchObject(7, 'test-obj');

      expect(result.objectRef).toBe('test-obj');
      expect(result.data).toBe('');
      expect(result.checksum).toBe('');
      expect(result.size).toBe(0);
      expect(result.compressed).toBe(false);
      expect(result.compressionAlgo).toBe('');
      expect(result.found).toBe(false);
      expect(result.errorMessage).toBe('');
    });

    it('should properly encode special characters in objectRef', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: true });

      await client.fetchObject(8, 'path/to/object:with:colons');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/8/objects/path%2Fto%2Fobject%3Awith%3Acolons',
        expect.any(Object)
      );
    });

    it('should accept shardId of 0', async () => {
      mockTransport.get.mockResolvedValueOnce({ found: true });

      await client.fetchObject(0, 'obj-zero');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/0/objects/obj-zero',
        { query: {} }
      );
    });
  });

  // ==========================================================================
  // streamFetchObjects Tests
  // ==========================================================================

  describe('streamFetchObjects', () => {
    it('should fetch multiple objects with minimal parameters', async () => {
      const mockResponse = {
        results: [
          {
            objectRef: 'obj-1',
            data: 'ZGF0YTE=',
            checksum: 'checksum1',
            size: 5,
            compressed: false,
            compressionAlgo: '',
            found: true,
            errorMessage: '',
          },
          {
            objectRef: 'obj-2',
            data: 'ZGF0YTI=',
            checksum: 'checksum2',
            size: 5,
            compressed: false,
            compressionAlgo: '',
            found: true,
            errorMessage: '',
          },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const items = [
        { objectRef: 'obj-1' },
        { objectRef: 'obj-2' },
      ];

      const result = await client.streamFetchObjects(1, items);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/1/objects/batch',
        {
          items: [
            { objectRef: 'obj-1', checksum: null },
            { objectRef: 'obj-2', checksum: null },
          ],
        }
      );
      expect(result).toHaveLength(2);
      expect(result[0].objectRef).toBe('obj-1');
      expect(result[0].found).toBe(true);
      expect(result[1].objectRef).toBe('obj-2');
      expect(result[1].found).toBe(true);
    });

    it('should fetch objects with checksums', async () => {
      const mockResponse = {
        results: [
          { objectRef: 'obj-a', found: true },
          { objectRef: 'obj-b', found: true },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const items = [
        { objectRef: 'obj-a', checksum: 'checksum-a' },
        { objectRef: 'obj-b', checksum: 'checksum-b' },
      ];

      await client.streamFetchObjects(2, items);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/2/objects/batch',
        {
          items: [
            { objectRef: 'obj-a', checksum: 'checksum-a' },
            { objectRef: 'obj-b', checksum: 'checksum-b' },
          ],
        }
      );
    });

    it('should fetch objects with requestId', async () => {
      const mockResponse = {
        results: [{ objectRef: 'obj-x', found: true }],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const items = [{ objectRef: 'obj-x' }];

      await client.streamFetchObjects(3, items, { requestId: 'batch-req-123' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/3/objects/batch',
        {
          items: [{ objectRef: 'obj-x', checksum: null }],
          requestId: 'batch-req-123',
        }
      );
    });

    it('should handle mixed found and not found results', async () => {
      const mockResponse = {
        results: [
          { objectRef: 'found-obj', data: 'ZGF0YQ==', found: true },
          { objectRef: 'missing-obj', data: '', found: false, errorMessage: 'Not found' },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const items = [
        { objectRef: 'found-obj' },
        { objectRef: 'missing-obj' },
      ];

      const result = await client.streamFetchObjects(4, items);

      expect(result).toHaveLength(2);
      expect(result[0].found).toBe(true);
      expect(result[1].found).toBe(false);
      expect(result[1].errorMessage).toBe('Not found');
    });

    it('should handle compressed objects in batch', async () => {
      const mockResponse = {
        results: [
          {
            objectRef: 'compressed',
            data: 'H4sIAAAAAAAA/w==',
            compressed: true,
            compressionAlgo: 'zstd',
            found: true,
          },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const items = [{ objectRef: 'compressed' }];

      const result = await client.streamFetchObjects(5, items);

      expect(result[0].compressed).toBe(true);
      expect(result[0].compressionAlgo).toBe('zstd');
    });

    it('should throw ValidationError when shardId is missing', async () => {
      const items = [{ objectRef: 'obj-1' }];
      await expect(client.streamFetchObjects(null, items)).rejects.toThrow(ValidationError);
      await expect(client.streamFetchObjects(undefined, items)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is negative', async () => {
      const items = [{ objectRef: 'obj-1' }];
      await expect(client.streamFetchObjects(-1, items)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when shardId is not an integer', async () => {
      const items = [{ objectRef: 'obj-1' }];
      await expect(client.streamFetchObjects(1.5, items)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is empty', async () => {
      await expect(client.streamFetchObjects(1, [])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is null', async () => {
      await expect(client.streamFetchObjects(1, null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when items is not an array', async () => {
      await expect(client.streamFetchObjects(1, 'not-array')).rejects.toThrow(ValidationError);
      await expect(client.streamFetchObjects(1, { objectRef: 'obj' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item is not an object', async () => {
      await expect(client.streamFetchObjects(1, ['string-item'])).rejects.toThrow(ValidationError);
      await expect(client.streamFetchObjects(1, [null])).rejects.toThrow(ValidationError);
      await expect(client.streamFetchObjects(1, [123])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.objectRef is missing', async () => {
      await expect(client.streamFetchObjects(1, [{}])).rejects.toThrow(ValidationError);
      await expect(client.streamFetchObjects(1, [{ checksum: 'abc' }])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.objectRef is empty', async () => {
      await expect(client.streamFetchObjects(1, [{ objectRef: '' }])).rejects.toThrow(ValidationError);
      await expect(client.streamFetchObjects(1, [{ objectRef: '   ' }])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.objectRef is not a string', async () => {
      await expect(client.streamFetchObjects(1, [{ objectRef: 123 }])).rejects.toThrow(ValidationError);
      await expect(client.streamFetchObjects(1, [{ objectRef: {} }])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when item.checksum is not a string', async () => {
      await expect(client.streamFetchObjects(1, [{ objectRef: 'obj', checksum: 123 }])).rejects.toThrow(ValidationError);
      await expect(client.streamFetchObjects(1, [{ objectRef: 'obj', checksum: {} }])).rejects.toThrow(ValidationError);
    });

    it('should allow null checksum in items', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [{ found: true }] });

      await client.streamFetchObjects(6, [{ objectRef: 'obj', checksum: null }]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          items: [{ objectRef: 'obj', checksum: null }],
        })
      );
    });

    it('should allow undefined checksum in items', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [{ found: true }] });

      await client.streamFetchObjects(7, [{ objectRef: 'obj', checksum: undefined }]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          items: [{ objectRef: 'obj', checksum: null }],
        })
      );
    });

    it('should handle response with missing results array', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const items = [{ objectRef: 'obj-1' }, { objectRef: 'obj-2' }];
      const result = await client.streamFetchObjects(8, items);

      expect(result).toHaveLength(2);
      expect(result[0].objectRef).toBe('obj-1');
      expect(result[0].found).toBe(false);
      expect(result[1].objectRef).toBe('obj-2');
      expect(result[1].found).toBe(false);
    });

    it('should handle response with null results', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: null });

      const items = [{ objectRef: 'obj-1' }];
      const result = await client.streamFetchObjects(9, items);

      expect(result).toHaveLength(1);
      expect(result[0].objectRef).toBe('obj-1');
      expect(result[0].found).toBe(false);
    });

    it('should handle results with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({
        results: [{}],
      });

      const items = [{ objectRef: 'obj-1' }];
      const result = await client.streamFetchObjects(10, items);

      expect(result).toHaveLength(1);
      expect(result[0].objectRef).toBe('obj-1');
      expect(result[0].data).toBe('');
      expect(result[0].checksum).toBe('');
      expect(result[0].size).toBe(0);
      expect(result[0].compressed).toBe(false);
      expect(result[0].compressionAlgo).toBe('');
      expect(result[0].found).toBe(false);
      expect(result[0].errorMessage).toBe('');
    });

    it('should accept shardId of 0', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [{ found: true }] });

      await client.streamFetchObjects(0, [{ objectRef: 'obj' }]);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/objects/transfer/shards/0/objects/batch',
        expect.any(Object)
      );
    });

    it('should handle large batch of items', async () => {
      const largeItems = Array.from({ length: 100 }, (_, i) => ({
        objectRef: `obj-${i}`,
        checksum: `checksum-${i}`,
      }));

      const largeResults = Array.from({ length: 100 }, (_, i) => ({
        objectRef: `obj-${i}`,
        found: true,
      }));

      mockTransport.post.mockResolvedValueOnce({ results: largeResults });

      const result = await client.streamFetchObjects(11, largeItems);

      expect(result).toHaveLength(100);
      expect(mockTransport.post).toHaveBeenCalledWith(
        expect.any(String),
        expect.objectContaining({
          items: expect.arrayContaining([
            { objectRef: 'obj-0', checksum: 'checksum-0' },
            { objectRef: 'obj-99', checksum: 'checksum-99' },
          ]),
        })
      );
    });

    it('should validate all items in array', async () => {
      const items = [
        { objectRef: 'valid-obj' },
        { objectRef: '' }, // Invalid
      ];

      await expect(client.streamFetchObjects(12, items)).rejects.toThrow(ValidationError);
    });
  });
});
