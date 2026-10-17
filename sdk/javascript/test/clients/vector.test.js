/**
 * VectorClient Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import VectorClient, { DistanceMetric, IndexType } from '../../src/clients/vector.js';
import { ValidationError } from '../../src/errors/index.js';

describe('VectorClient', () => {
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
    client = new VectorClient(mockTransport);
  });

  // ==========================================================================
  // Constructor Tests
  // ==========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      expect(client).toBeInstanceOf(VectorClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new VectorClient()).toThrow(ValidationError);
      expect(() => new VectorClient(null)).toThrow(ValidationError);
      expect(() => new VectorClient(undefined)).toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Namespace Management Tests
  // ==========================================================================

  describe('createNamespace', () => {
    it('should create a namespace with name and dimensions', async () => {
      const mockResponse = {
        name: 'embeddings',
        dimensions: 768,
        distanceMetric: 'cosine',
        indexType: 'hnsw',
        vectorCount: 0,
        createdAt: '2025-01-17T00:00:00Z',
        updatedAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.createNamespace('embeddings', 768);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/namespaces', {
        name: 'embeddings',
        dimensions: 768,
        distanceMetric: 'cosine',
        indexType: 'hnsw',
      });
      expect(result.name).toBe('embeddings');
      expect(result.dimensions).toBe(768);
    });

    it('should create a namespace with custom options', async () => {
      const mockResponse = {
        name: 'embeddings',
        dimensions: 1536,
        distanceMetric: 'euclidean',
        indexType: 'ivf',
        vectorCount: 0,
        createdAt: '2025-01-17T00:00:00Z',
        updatedAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.createNamespace('embeddings', 1536, {
        distanceMetric: DistanceMetric.EUCLIDEAN,
        indexType: IndexType.IVF,
        indexParams: { nlist: 100, nprobe: 10 },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/namespaces', {
        name: 'embeddings',
        dimensions: 1536,
        distanceMetric: 'euclidean',
        indexType: 'ivf',
        indexParams: { nlist: 100, nprobe: 10 },
      });
      expect(result.distanceMetric).toBe('euclidean');
      expect(result.indexType).toBe('ivf');
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.createNamespace('', 768)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is not a string', async () => {
      await expect(client.createNamespace(123, 768)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when dimensions is not positive', async () => {
      await expect(client.createNamespace('embeddings', 0)).rejects.toThrow(ValidationError);
      await expect(client.createNamespace('embeddings', -1)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when dimensions is not an integer', async () => {
      await expect(client.createNamespace('embeddings', 768.5)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid distanceMetric', async () => {
      await expect(
        client.createNamespace('embeddings', 768, { distanceMetric: 'invalid' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid indexType', async () => {
      await expect(
        client.createNamespace('embeddings', 768, { indexType: 'invalid' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when indexParams is not an object', async () => {
      await expect(
        client.createNamespace('embeddings', 768, { indexParams: 'invalid' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when indexParams is an array', async () => {
      await expect(
        client.createNamespace('embeddings', 768, { indexParams: [1, 2, 3] }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid indexParams.m', async () => {
      await expect(
        client.createNamespace('embeddings', 768, { indexParams: { m: -1 } }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid indexParams.efConstruction', async () => {
      await expect(
        client.createNamespace('embeddings', 768, { indexParams: { efConstruction: 0 } }),
      ).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteNamespace', () => {
    it('should delete a namespace', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      await client.deleteNamespace('embeddings');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/vector/namespaces/embeddings');
    });

    it('should URL-encode namespace name', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      await client.deleteNamespace('my namespace');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/vector/namespaces/my%20namespace');
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.deleteNamespace('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is not a string', async () => {
      await expect(client.deleteNamespace(null)).rejects.toThrow(ValidationError);
    });
  });

  describe('listNamespaces', () => {
    it('should list all namespaces', async () => {
      const mockResponse = {
        namespaces: [
          { name: 'ns1', dimensions: 768, vectorCount: 100 },
          { name: 'ns2', dimensions: 1536, vectorCount: 200 },
        ],
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listNamespaces();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/vector/namespaces');
      expect(result).toHaveLength(2);
      expect(result[0].name).toBe('ns1');
      expect(result[1].name).toBe('ns2');
    });

    it('should return empty array when no namespaces exist', async () => {
      mockTransport.get.mockResolvedValueOnce({ namespaces: [] });

      const result = await client.listNamespaces();

      expect(result).toEqual([]);
    });

    it('should handle null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.listNamespaces();

      expect(result).toEqual([]);
    });
  });

  describe('getNamespaceInfo', () => {
    it('should get namespace information', async () => {
      const mockResponse = {
        name: 'embeddings',
        dimensions: 768,
        distanceMetric: 'cosine',
        indexType: 'hnsw',
        vectorCount: 1000,
        createdAt: '2025-01-17T00:00:00Z',
        updatedAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getNamespaceInfo('embeddings');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/vector/namespaces/embeddings');
      expect(result.name).toBe('embeddings');
      expect(result.dimensions).toBe(768);
      expect(result.vectorCount).toBe(1000);
    });

    it('should apply defaults for missing namespace fields', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getNamespaceInfo('embeddings');

      expect(result.name).toBe('');
      expect(result.dimensions).toBe(0);
      expect(result.distanceMetric).toBe('cosine');
      expect(result.indexType).toBe('hnsw');
      expect(result.vectorCount).toBe(0);
      expect(result.createdAt).toBeNull();
      expect(result.updatedAt).toBeNull();
    });

    it('should handle null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getNamespaceInfo('embeddings');

      expect(result.name).toBe('');
      expect(result.dimensions).toBe(0);
      expect(result.distanceMetric).toBe('cosine');
      expect(result.indexType).toBe('hnsw');
      expect(result.vectorCount).toBe(0);
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.getNamespaceInfo('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is not a string', async () => {
      await expect(client.getNamespaceInfo(123)).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Vector Operations Tests
  // ==========================================================================

  describe('upsertVector', () => {
    const validVector = [0.1, 0.2, 0.3, 0.4, 0.5];

    it('should upsert a vector', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'vec-1' });

      const result = await client.upsertVector('embeddings', 'vec-1', validVector);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/embeddings/vectors', {
        id: 'vec-1',
        vector: validVector,
      });
      expect(result.id).toBe('vec-1');
    });

    it('should upsert a vector with metadata', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'vec-1' });

      const result = await client.upsertVector('embeddings', 'vec-1', validVector, {
        metadata: { category: 'science', author: 'Alice' },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/embeddings/vectors', {
        id: 'vec-1',
        vector: validVector,
        metadata: { category: 'science', author: 'Alice' },
      });
      expect(result.id).toBe('vec-1');
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.upsertVector('', 'vec-1', validVector)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when id is empty', async () => {
      await expect(client.upsertVector('embeddings', '', validVector)).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when vector is empty array', async () => {
      await expect(client.upsertVector('embeddings', 'vec-1', [])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vector is not an array', async () => {
      await expect(client.upsertVector('embeddings', 'vec-1', 'not-array')).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when vector contains non-finite numbers', async () => {
      await expect(
        client.upsertVector('embeddings', 'vec-1', [0.1, NaN, 0.3]),
      ).rejects.toThrow(ValidationError);
      await expect(
        client.upsertVector('embeddings', 'vec-1', [0.1, Infinity, 0.3]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vector contains non-numbers', async () => {
      await expect(
        client.upsertVector('embeddings', 'vec-1', [0.1, 'string', 0.3]),
      ).rejects.toThrow(ValidationError);
    });

    it('should fallback to provided id when response id is missing', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.upsertVector('embeddings', 'vec-1', validVector);

      expect(result.id).toBe('vec-1');
    });

    it('should handle null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.upsertVector('embeddings', 'vec-1', validVector);

      expect(result.id).toBe('vec-1');
    });
  });

  describe('getVector', () => {
    it('should get a vector by id', async () => {
      const mockResponse = {
        id: 'vec-1',
        vector: [0.1, 0.2, 0.3],
        metadata: { category: 'science' },
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getVector('embeddings', 'vec-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/vector/embeddings/vectors/vec-1');
      expect(result.id).toBe('vec-1');
      expect(result.vector).toEqual([0.1, 0.2, 0.3]);
      expect(result.metadata).toEqual({ category: 'science' });
    });

    it('should handle missing metadata', async () => {
      mockTransport.get.mockResolvedValueOnce({ id: 'vec-1', vector: [0.1, 0.2, 0.3] });

      const result = await client.getVector('embeddings', 'vec-1');

      expect(result.metadata).toBeNull();
    });

    it('should fallback to provided id when response id is missing', async () => {
      mockTransport.get.mockResolvedValueOnce({ vector: [0.1, 0.2, 0.3] });

      const result = await client.getVector('embeddings', 'vec-1');

      expect(result.id).toBe('vec-1');
    });

    it('should fallback to empty vector when response vector is missing', async () => {
      mockTransport.get.mockResolvedValueOnce({ id: 'vec-1' });

      const result = await client.getVector('embeddings', 'vec-1');

      expect(result.vector).toEqual([]);
    });

    it('should handle null response with all fallbacks', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getVector('embeddings', 'vec-1');

      expect(result.id).toBe('vec-1');
      expect(result.vector).toEqual([]);
      expect(result.metadata).toBeNull();
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.getVector('', 'vec-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when id is empty', async () => {
      await expect(client.getVector('embeddings', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteVector', () => {
    it('should delete a vector', async () => {
      mockTransport.delete.mockResolvedValueOnce({ deleted: true });

      const result = await client.deleteVector('embeddings', 'vec-1');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/vector/embeddings/vectors/vec-1');
      expect(result.deleted).toBe(true);
    });

    it('should default deleted to true when not in response', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      const result = await client.deleteVector('embeddings', 'vec-1');

      expect(result.deleted).toBe(true);
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.deleteVector('', 'vec-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when id is empty', async () => {
      await expect(client.deleteVector('embeddings', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('batchUpsert', () => {
    const validVectors = [
      { id: 'vec-1', vector: [0.1, 0.2, 0.3] },
      { id: 'vec-2', vector: [0.4, 0.5, 0.6] },
    ];

    it('should batch upsert vectors', async () => {
      mockTransport.post.mockResolvedValueOnce({
        count: 2,
        ids: ['vec-1', 'vec-2'],
      });

      const result = await client.batchUpsert('embeddings', validVectors);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/embeddings/vectors/batch', {
        vectors: [
          { id: 'vec-1', vector: [0.1, 0.2, 0.3], metadata: undefined },
          { id: 'vec-2', vector: [0.4, 0.5, 0.6], metadata: undefined },
        ],
      });
      expect(result.count).toBe(2);
      expect(result.ids).toEqual(['vec-1', 'vec-2']);
    });

    it('should batch upsert vectors with metadata', async () => {
      const vectorsWithMetadata = [
        { id: 'vec-1', vector: [0.1, 0.2, 0.3], metadata: { cat: 'A' } },
        { id: 'vec-2', vector: [0.4, 0.5, 0.6], metadata: { cat: 'B' } },
      ];
      mockTransport.post.mockResolvedValueOnce({ count: 2, ids: ['vec-1', 'vec-2'] });

      const result = await client.batchUpsert('embeddings', vectorsWithMetadata);

      expect(result.count).toBe(2);
    });

    it('should fallback to vector ids when response does not include ids', async () => {
      const vectors = [
        { id: 'vec-1', vector: [0.1, 0.2, 0.3] },
        { id: 'vec-2', vector: [0.4, 0.5, 0.6] },
      ];
      mockTransport.post.mockResolvedValueOnce({ count: 2 });

      const result = await client.batchUpsert('embeddings', vectors);

      expect(result.count).toBe(2);
      expect(result.ids).toEqual(['vec-1', 'vec-2']);
    });

    it('should fallback to vector count when response does not include count', async () => {
      const vectors = [
        { id: 'vec-1', vector: [0.1, 0.2, 0.3] },
        { id: 'vec-2', vector: [0.4, 0.5, 0.6] },
      ];
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.batchUpsert('embeddings', vectors);

      expect(result.count).toBe(2);
      expect(result.ids).toEqual(['vec-1', 'vec-2']);
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.batchUpsert('', validVectors)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vectors is empty array', async () => {
      await expect(client.batchUpsert('embeddings', [])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vectors is not an array', async () => {
      await expect(client.batchUpsert('embeddings', 'not-array')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vector item is not an object', async () => {
      await expect(client.batchUpsert('embeddings', ['not-object'])).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when vector item is an array', async () => {
      await expect(client.batchUpsert('embeddings', [['array']])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vector id is missing', async () => {
      await expect(
        client.batchUpsert('embeddings', [{ vector: [0.1, 0.2, 0.3] }]),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vectors have different dimensions', async () => {
      const mismatchedVectors = [
        { id: 'vec-1', vector: [0.1, 0.2, 0.3] },
        { id: 'vec-2', vector: [0.4, 0.5] },
      ];
      await expect(client.batchUpsert('embeddings', mismatchedVectors)).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      const invalidMetadata = [{ id: 'vec-1', vector: [0.1, 0.2, 0.3], metadata: 'invalid' }];
      await expect(client.batchUpsert('embeddings', invalidMetadata)).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when metadata is an array', async () => {
      const invalidMetadata = [{ id: 'vec-1', vector: [0.1, 0.2, 0.3], metadata: [1, 2] }];
      await expect(client.batchUpsert('embeddings', invalidMetadata)).rejects.toThrow(
        ValidationError,
      );
    });
  });

  // ==========================================================================
  // Search Operations Tests
  // ==========================================================================

  describe('searchSimilar', () => {
    const queryVector = [0.1, 0.2, 0.3];

    it('should search for similar vectors', async () => {
      const mockResponse = {
        results: [
          { id: 'vec-1', score: 0.95, metadata: { cat: 'A' } },
          { id: 'vec-2', score: 0.87, metadata: { cat: 'B' } },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.searchSimilar('embeddings', queryVector);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/embeddings/search', {
        vector: queryVector,
        k: 10,
        includeMetadata: true,
        includeVectors: false,
      });
      expect(result.results).toHaveLength(2);
      expect(result.results[0].score).toBe(0.95);
    });

    it('should search with custom options', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [] });

      await client.searchSimilar('embeddings', queryVector, {
        k: 5,
        filter: { category: 'science' },
        includeMetadata: false,
        includeVectors: true,
        efSearch: 100,
        nprobe: 10,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/embeddings/search', {
        vector: queryVector,
        k: 5,
        includeMetadata: false,
        includeVectors: true,
        filter: { category: 'science' },
        efSearch: 100,
        nprobe: 10,
      });
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.searchSimilar('', queryVector)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vector is empty', async () => {
      await expect(client.searchSimilar('embeddings', [])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when k is not positive', async () => {
      await expect(
        client.searchSimilar('embeddings', queryVector, { k: 0 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when efSearch is not positive', async () => {
      await expect(
        client.searchSimilar('embeddings', queryVector, { efSearch: -1 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when nprobe is not positive', async () => {
      await expect(
        client.searchSimilar('embeddings', queryVector, { nprobe: 0 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when filter is not an object', async () => {
      await expect(
        client.searchSimilar('embeddings', queryVector, { filter: 'invalid' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when filter is an array', async () => {
      await expect(
        client.searchSimilar('embeddings', queryVector, { filter: [1, 2] }),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle empty results', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [] });

      const result = await client.searchSimilar('embeddings', queryVector);

      expect(result.results).toEqual([]);
    });

    it('should handle null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.searchSimilar('embeddings', queryVector);

      expect(result.results).toEqual([]);
    });

    it('should include vectors in results when includeVectors is true', async () => {
      const mockResponse = {
        results: [
          { id: 'vec-1', score: 0.95, vector: [0.1, 0.2, 0.3], metadata: { cat: 'A' } },
          { id: 'vec-2', score: 0.87, vector: [0.4, 0.5, 0.6], metadata: { cat: 'B' } },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.searchSimilar('embeddings', queryVector, {
        includeVectors: true,
      });

      expect(result.results).toHaveLength(2);
      expect(result.results[0].vector).toEqual([0.1, 0.2, 0.3]);
      expect(result.results[1].vector).toEqual([0.4, 0.5, 0.6]);
    });

    it('should not include vector property when not in response', async () => {
      const mockResponse = {
        results: [
          { id: 'vec-1', score: 0.95, metadata: { cat: 'A' } },
        ],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.searchSimilar('embeddings', queryVector);

      expect(result.results[0]).not.toHaveProperty('vector');
      expect(result.results[0].metadata).toEqual({ cat: 'A' });
    });

    it('should apply defaults for missing search result fields', async () => {
      const mockResponse = {
        results: [{}],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.searchSimilar('embeddings', queryVector);

      expect(result.results[0].id).toBe('');
      expect(result.results[0].score).toBe(0);
      expect(result.results[0]).not.toHaveProperty('vector');
      expect(result.results[0]).not.toHaveProperty('metadata');
    });

    it('should handle null result items gracefully', async () => {
      const mockResponse = {
        results: [null],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.searchSimilar('embeddings', queryVector);

      expect(result.results[0].id).toBe('');
      expect(result.results[0].score).toBe(0);
    });
  });

  describe('searchByMetadata', () => {
    it('should search by metadata filter', async () => {
      const mockResponse = {
        results: [{ id: 'vec-1', score: 1, metadata: { category: 'science' } }],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.searchByMetadata('embeddings', { category: 'science' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/vector/embeddings/search/metadata',
        {
          filter: { category: 'science' },
          limit: 100,
          offset: 0,
          includeVectors: false,
        },
      );
      expect(result.results).toHaveLength(1);
    });

    it('should search with custom options', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [] });

      await client.searchByMetadata('embeddings', { category: 'science' }, {
        limit: 50,
        offset: 10,
        includeVectors: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/vector/embeddings/search/metadata',
        {
          filter: { category: 'science' },
          limit: 50,
          offset: 10,
          includeVectors: true,
        },
      );
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.searchByMetadata('', { category: 'science' })).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when filter is null', async () => {
      await expect(client.searchByMetadata('embeddings', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when filter is not an object', async () => {
      await expect(client.searchByMetadata('embeddings', 'invalid')).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when limit is not positive', async () => {
      await expect(
        client.searchByMetadata('embeddings', { cat: 'A' }, { limit: 0 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when offset is negative', async () => {
      await expect(
        client.searchByMetadata('embeddings', { cat: 'A' }, { offset: -1 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.searchByMetadata('embeddings', { category: 'science' });

      expect(result.results).toEqual([]);
    });
  });

  describe('hybridSearch', () => {
    const queryVector = [0.1, 0.2, 0.3];
    const filter = { category: 'science' };

    it('should perform hybrid search', async () => {
      const mockResponse = {
        results: [{ id: 'vec-1', score: 0.9 }],
      };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.hybridSearch('embeddings', queryVector, filter);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/embeddings/search/hybrid', {
        vector: queryVector,
        filter,
        k: 10,
        alpha: 0.5,
        includeMetadata: true,
        includeVectors: false,
      });
      expect(result.results).toHaveLength(1);
    });

    it('should perform hybrid search with custom options', async () => {
      mockTransport.post.mockResolvedValueOnce({ results: [] });

      await client.hybridSearch('embeddings', queryVector, filter, {
        k: 20,
        alpha: 0.7,
        includeMetadata: false,
        includeVectors: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/vector/embeddings/search/hybrid', {
        vector: queryVector,
        filter,
        k: 20,
        alpha: 0.7,
        includeMetadata: false,
        includeVectors: true,
      });
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.hybridSearch('', queryVector, filter)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when vector is empty', async () => {
      await expect(client.hybridSearch('embeddings', [], filter)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when filter is null', async () => {
      await expect(client.hybridSearch('embeddings', queryVector, null)).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when k is not positive', async () => {
      await expect(
        client.hybridSearch('embeddings', queryVector, filter, { k: 0 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when alpha is less than 0', async () => {
      await expect(
        client.hybridSearch('embeddings', queryVector, filter, { alpha: -0.1 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when alpha is greater than 1', async () => {
      await expect(
        client.hybridSearch('embeddings', queryVector, filter, { alpha: 1.5 }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when alpha is not a number', async () => {
      await expect(
        client.hybridSearch('embeddings', queryVector, filter, { alpha: 'high' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.hybridSearch('embeddings', queryVector, filter);

      expect(result.results).toEqual([]);
    });
  });

  // ==========================================================================
  // Index Management Tests
  // ==========================================================================

  describe('rebuildIndex', () => {
    it('should start index rebuild', async () => {
      mockTransport.post.mockResolvedValueOnce({
        jobId: 'job-123',
        status: 'pending',
      });

      const result = await client.rebuildIndex('embeddings');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/vector/embeddings/index/rebuild',
        { async: true },
      );
      expect(result.jobId).toBe('job-123');
      expect(result.status).toBe('pending');
    });

    it('should rebuild with custom options', async () => {
      mockTransport.post.mockResolvedValueOnce({ jobId: 'job-123', status: 'pending' });

      await client.rebuildIndex('embeddings', {
        indexType: IndexType.HNSW,
        indexParams: { m: 32, efConstruction: 400 },
        async: false,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/vector/embeddings/index/rebuild',
        {
          async: false,
          indexType: 'hnsw',
          indexParams: { m: 32, efConstruction: 400 },
        },
      );
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.rebuildIndex('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for invalid indexType', async () => {
      await expect(
        client.rebuildIndex('embeddings', { indexType: 'invalid' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when indexParams is not an object', async () => {
      await expect(
        client.rebuildIndex('embeddings', { indexParams: 'invalid' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when indexParams is an array', async () => {
      await expect(
        client.rebuildIndex('embeddings', { indexParams: [1, 2] }),
      ).rejects.toThrow(ValidationError);
    });

    it('should handle response without jobId', async () => {
      mockTransport.post.mockResolvedValueOnce({ status: 'pending' });

      const result = await client.rebuildIndex('embeddings');

      expect(result.jobId).toBe('');
      expect(result.status).toBe('pending');
    });

    it('should handle response without status', async () => {
      mockTransport.post.mockResolvedValueOnce({ jobId: 'job-123' });

      const result = await client.rebuildIndex('embeddings');

      expect(result.jobId).toBe('job-123');
      expect(result.status).toBe('pending');
    });

    it('should handle null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.rebuildIndex('embeddings');

      expect(result.jobId).toBe('');
      expect(result.status).toBe('pending');
    });
  });

  describe('getRebuildProgress', () => {
    it('should get rebuild progress', async () => {
      const mockResponse = {
        status: 'running',
        progress: 45,
        vectorsProcessed: 4500,
        totalVectors: 10000,
        startedAt: '2025-01-17T00:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRebuildProgress('embeddings');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/vector/embeddings/index/rebuild/progress',
      );
      expect(result.status).toBe('running');
      expect(result.progress).toBe(45);
      expect(result.vectorsProcessed).toBe(4500);
      expect(result.totalVectors).toBe(10000);
    });

    it('should handle completed rebuild', async () => {
      const mockResponse = {
        status: 'completed',
        progress: 100,
        vectorsProcessed: 10000,
        totalVectors: 10000,
        startedAt: '2025-01-17T00:00:00Z',
        completedAt: '2025-01-17T01:00:00Z',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRebuildProgress('embeddings');

      expect(result.status).toBe('completed');
      expect(result.completedAt).toBe('2025-01-17T01:00:00Z');
    });

    it('should handle failed rebuild', async () => {
      const mockResponse = {
        status: 'failed',
        progress: 30,
        error: 'Out of memory',
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getRebuildProgress('embeddings');

      expect(result.status).toBe('failed');
      expect(result.error).toBe('Out of memory');
    });

    it('should throw ValidationError when namespace is empty', async () => {
      await expect(client.getRebuildProgress('')).rejects.toThrow(ValidationError);
    });

    it('should handle null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.getRebuildProgress('embeddings');

      expect(result.status).toBe('unknown');
      expect(result.progress).toBe(0);
    });
  });

  // ==========================================================================
  // Enum Exports Tests
  // ==========================================================================

  describe('DistanceMetric enum', () => {
    it('should export COSINE', () => {
      expect(DistanceMetric.COSINE).toBe('cosine');
    });

    it('should export EUCLIDEAN', () => {
      expect(DistanceMetric.EUCLIDEAN).toBe('euclidean');
    });

    it('should export DOT_PRODUCT', () => {
      expect(DistanceMetric.DOT_PRODUCT).toBe('dot_product');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(DistanceMetric)).toBe(true);
    });
  });

  describe('IndexType enum', () => {
    it('should export HNSW', () => {
      expect(IndexType.HNSW).toBe('hnsw');
    });

    it('should export FLAT', () => {
      expect(IndexType.FLAT).toBe('flat');
    });

    it('should export IVF', () => {
      expect(IndexType.IVF).toBe('ivf');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(IndexType)).toBe(true);
    });
  });
});
