/**
 * QuicBaaS JavaScript SDK - Vector Client
 *
 * Provides vector similarity search operations including namespace management,
 * vector CRUD operations, k-NN search, and index management.
 *
 * @module @quicbaas/client/clients/vector
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  requiredArray,
  nonEmptyArray,
  positiveInteger,
  nonNegativeInteger,
  oneOf,
  requiredObject,
} from '../utils/validation.js';

/**
 * Distance metrics for vector similarity calculations.
 * @readonly
 * @enum {string}
 */
export const DistanceMetric = Object.freeze({
  /** Cosine similarity (normalized dot product) */
  COSINE: 'cosine',
  /** Euclidean distance (L2 norm) */
  EUCLIDEAN: 'euclidean',
  /** Dot product similarity */
  DOT_PRODUCT: 'dot_product',
});

/**
 * Index types for vector storage and search.
 * @readonly
 * @enum {string}
 */
export const IndexType = Object.freeze({
  /** Hierarchical Navigable Small World graph (recommended for most use cases) */
  HNSW: 'hnsw',
  /** Flat index with exact search (best for small datasets) */
  FLAT: 'flat',
  /** Inverted File index (good for large datasets with clustering) */
  IVF: 'ivf',
});

/**
 * @typedef {Object} NamespaceOptions
 * @property {string} [distanceMetric='cosine'] - Distance metric (cosine, euclidean, dot_product)
 * @property {string} [indexType='hnsw'] - Index type (hnsw, flat, ivf)
 * @property {Object} [indexParams] - Index-specific parameters
 * @property {number} [indexParams.m] - HNSW: Number of connections per element
 * @property {number} [indexParams.efConstruction] - HNSW: Size of dynamic candidate list for construction
 * @property {number} [indexParams.nlist] - IVF: Number of clusters
 * @property {number} [indexParams.nprobe] - IVF: Number of clusters to search
 */

/**
 * @typedef {Object} NamespaceInfo
 * @property {string} name - Namespace name
 * @property {number} dimensions - Vector dimensions
 * @property {string} distanceMetric - Distance metric
 * @property {string} indexType - Index type
 * @property {number} vectorCount - Number of vectors stored
 * @property {string} createdAt - Creation timestamp
 * @property {string} updatedAt - Last update timestamp
 */

/**
 * @typedef {Object} UpsertOptions
 * @property {Object} [metadata] - Key-value metadata to store with the vector
 */

/**
 * @typedef {Object} VectorData
 * @property {string} id - Vector identifier
 * @property {number[]} vector - Vector values
 * @property {Object} [metadata] - Associated metadata
 */

/**
 * @typedef {Object} BatchVectorItem
 * @property {string} id - Vector identifier
 * @property {number[]} vector - Vector values
 * @property {Object} [metadata] - Associated metadata
 */

/**
 * @typedef {Object} SearchOptions
 * @property {number} [k=10] - Number of nearest neighbors to return
 * @property {Object} [filter] - Metadata filter for pre-filtering vectors
 * @property {boolean} [includeMetadata=true] - Include metadata in results
 * @property {boolean} [includeVectors=false] - Include vector values in results
 * @property {number} [efSearch] - HNSW: Size of dynamic candidate list for search
 * @property {number} [nprobe] - IVF: Number of clusters to search
 */

/**
 * @typedef {Object} SearchResult
 * @property {string} id - Vector identifier
 * @property {number} score - Similarity score
 * @property {number[]} [vector] - Vector values (if includeVectors is true)
 * @property {Object} [metadata] - Associated metadata (if includeMetadata is true)
 */

/**
 * @typedef {Object} SearchResponse
 * @property {SearchResult[]} results - Search results ordered by similarity
 */

/**
 * @typedef {Object} MetadataSearchOptions
 * @property {number} [limit=100] - Maximum number of results to return
 * @property {number} [offset=0] - Number of results to skip
 * @property {boolean} [includeVectors=false] - Include vector values in results
 */

/**
 * @typedef {Object} HybridSearchOptions
 * @property {number} [k=10] - Number of nearest neighbors to return
 * @property {boolean} [includeMetadata=true] - Include metadata in results
 * @property {boolean} [includeVectors=false] - Include vector values in results
 * @property {number} [alpha=0.5] - Weight for vector similarity (0-1), metadata weight is 1-alpha
 */

/**
 * @typedef {Object} RebuildOptions
 * @property {string} [indexType] - New index type (if changing)
 * @property {Object} [indexParams] - New index parameters
 * @property {boolean} [async=true] - Run rebuild asynchronously
 */

/**
 * @typedef {Object} RebuildProgress
 * @property {string} status - Rebuild status (pending, running, completed, failed)
 * @property {number} progress - Progress percentage (0-100)
 * @property {number} vectorsProcessed - Number of vectors processed
 * @property {number} totalVectors - Total vectors to process
 * @property {string} [error] - Error message if failed
 * @property {string} startedAt - Start timestamp
 * @property {string} [completedAt] - Completion timestamp
 */

/**
 * API base path for vector operations.
 * @constant {string}
 */
const API_BASE_PATH = '/api/v1/vector';

/**
 * VectorClient provides vector similarity search operations for QuicBaaS.
 *
 * Supports namespace management, vector CRUD operations, k-NN similarity search,
 * metadata filtering, hybrid search, and index management.
 *
 * @example
 * const transport = new HttpTransport('http://localhost:8080');
 * const vectorClient = new VectorClient(transport);
 *
 * // Create a namespace
 * await vectorClient.createNamespace('embeddings', 768, {
 *   distanceMetric: DistanceMetric.COSINE,
 *   indexType: IndexType.HNSW
 * });
 *
 * // Upsert a vector with metadata
 * await vectorClient.upsertVector('embeddings', 'doc1', [0.1, 0.2, ...], {
 *   metadata: { category: 'science', author: 'Alice' }
 * });
 *
 * // Search for similar vectors
 * const results = await vectorClient.searchSimilar('embeddings', queryVector, {
 *   k: 10,
 *   filter: { category: 'science' }
 * });
 */
class VectorClient {
  /**
   * Create a new VectorClient instance.
   *
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} When transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  // ============================================================
  // Namespace Management
  // ============================================================

  /**
   * Create a new vector namespace.
   *
   * A namespace is a container for vectors with a fixed dimensionality
   * and configured distance metric and index type.
   *
   * @param {string} name - Unique namespace name
   * @param {number} dimensions - Vector dimensions (must be positive)
   * @param {NamespaceOptions} [options={}] - Namespace configuration options
   * @returns {Promise<NamespaceInfo>} Created namespace information
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const ns = await vectorClient.createNamespace('embeddings', 768, {
   *   distanceMetric: DistanceMetric.COSINE,
   *   indexType: IndexType.HNSW,
   *   indexParams: { m: 16, efConstruction: 200 }
   * });
   */
  async createNamespace(name, dimensions, options = {}) {
    requiredString(name, 'name');
    positiveInteger(dimensions, 'dimensions');
    this.#validateNamespaceOptions(options);

    const path = `${API_BASE_PATH}/namespaces`;
    const body = {
      name,
      dimensions,
      distanceMetric: options.distanceMetric || DistanceMetric.COSINE,
      indexType: options.indexType || IndexType.HNSW,
    };

    if (options.indexParams) {
      body.indexParams = options.indexParams;
    }

    const response = await this.#transport.post(path, body);
    return this.#formatNamespaceInfo(response);
  }

  /**
   * Delete a vector namespace.
   *
   * This permanently removes the namespace and all vectors within it.
   * This operation cannot be undone.
   *
   * @param {string} name - Namespace name to delete
   * @returns {Promise<void>}
   * @throws {ValidationError} When name is invalid
   *
   * @example
   * await vectorClient.deleteNamespace('old-embeddings');
   */
  async deleteNamespace(name) {
    requiredString(name, 'name');

    const path = `${API_BASE_PATH}/namespaces/${encodeURIComponent(name)}`;
    await this.#transport.delete(path);
  }

  /**
   * List all vector namespaces.
   *
   * @returns {Promise<NamespaceInfo[]>} Array of namespace information
   *
   * @example
   * const namespaces = await vectorClient.listNamespaces();
   * namespaces.forEach(ns => {
   *   console.log(`${ns.name}: ${ns.vectorCount} vectors`);
   * });
   */
  async listNamespaces() {
    const path = `${API_BASE_PATH}/namespaces`;
    const response = await this.#transport.get(path);

    const namespaces = response?.namespaces || [];
    return namespaces.map(ns => this.#formatNamespaceInfo(ns));
  }

  /**
   * Get detailed information about a namespace.
   *
   * @param {string} name - Namespace name
   * @returns {Promise<NamespaceInfo>} Namespace information
   * @throws {ValidationError} When name is invalid
   *
   * @example
   * const info = await vectorClient.getNamespaceInfo('embeddings');
   * console.log(`Dimensions: ${info.dimensions}, Count: ${info.vectorCount}`);
   */
  async getNamespaceInfo(name) {
    requiredString(name, 'name');

    const path = `${API_BASE_PATH}/namespaces/${encodeURIComponent(name)}`;
    const response = await this.#transport.get(path);

    return this.#formatNamespaceInfo(response);
  }

  // ============================================================
  // Vector Operations
  // ============================================================

  /**
   * Insert or update a vector with optional metadata.
   *
   * If a vector with the given ID already exists, it will be replaced.
   * The vector dimensions must match the namespace configuration.
   *
   * @param {string} namespace - Namespace name
   * @param {string} id - Unique vector identifier
   * @param {number[]} vector - Vector values (must match namespace dimensions)
   * @param {UpsertOptions} [options={}] - Upsert options including metadata
   * @returns {Promise<{id: string}>} The upserted vector ID
   * @throws {ValidationError} When parameters are invalid or dimensions mismatch
   *
   * @example
   * await vectorClient.upsertVector('embeddings', 'doc-123', embedding, {
   *   metadata: {
   *     title: 'Introduction to ML',
   *     category: 'technology',
   *     timestamp: Date.now()
   *   }
   * });
   */
  async upsertVector(namespace, id, vector, options = {}) {
    requiredString(namespace, 'namespace');
    requiredString(id, 'id');
    this.#validateVector(vector, 'vector');

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/vectors`;
    const body = {
      id,
      vector,
    };

    if (options.metadata) {
      body.metadata = options.metadata;
    }

    const response = await this.#transport.post(path, body);
    return {
      id: response?.id || id,
    };
  }

  /**
   * Get a vector by ID.
   *
   * @param {string} namespace - Namespace name
   * @param {string} id - Vector identifier
   * @returns {Promise<VectorData>} Vector data including values and metadata
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const vector = await vectorClient.getVector('embeddings', 'doc-123');
   * console.log(`Vector dimensions: ${vector.vector.length}`);
   * console.log(`Metadata: ${JSON.stringify(vector.metadata)}`);
   */
  async getVector(namespace, id) {
    requiredString(namespace, 'namespace');
    requiredString(id, 'id');

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/vectors/${encodeURIComponent(id)}`;
    const response = await this.#transport.get(path);

    return {
      id: response?.id || id,
      vector: response?.vector || [],
      metadata: response?.metadata || null,
    };
  }

  /**
   * Delete a vector by ID.
   *
   * @param {string} namespace - Namespace name
   * @param {string} id - Vector identifier to delete
   * @returns {Promise<{deleted: boolean}>} Whether the vector was deleted
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const result = await vectorClient.deleteVector('embeddings', 'doc-123');
   * if (result.deleted) {
   *   console.log('Vector deleted successfully');
   * }
   */
  async deleteVector(namespace, id) {
    requiredString(namespace, 'namespace');
    requiredString(id, 'id');

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/vectors/${encodeURIComponent(id)}`;
    const response = await this.#transport.delete(path);

    return {
      deleted: response?.deleted ?? true,
    };
  }

  /**
   * Batch insert or update multiple vectors.
   *
   * More efficient than individual upserts for bulk operations.
   * All vectors must have the same dimensions matching the namespace.
   *
   * @param {string} namespace - Namespace name
   * @param {BatchVectorItem[]} vectors - Array of vectors to upsert
   * @returns {Promise<{count: number, ids: string[]}>} Number of vectors upserted and their IDs
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const result = await vectorClient.batchUpsert('embeddings', [
   *   { id: 'doc1', vector: [...], metadata: { category: 'A' } },
   *   { id: 'doc2', vector: [...], metadata: { category: 'B' } },
   *   { id: 'doc3', vector: [...], metadata: { category: 'A' } }
   * ]);
   * console.log(`Upserted ${result.count} vectors`);
   */
  async batchUpsert(namespace, vectors) {
    requiredString(namespace, 'namespace');
    this.#validateBatchVectors(vectors);

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/vectors/batch`;
    const body = {
      vectors: vectors.map(v => ({
        id: v.id,
        vector: v.vector,
        metadata: v.metadata,
      })),
    };

    const response = await this.#transport.post(path, body);

    return {
      count: response?.count ?? vectors.length,
      ids: response?.ids ?? vectors.map(v => v.id),
    };
  }

  // ============================================================
  // Search Operations
  // ============================================================

  /**
   * Search for similar vectors using k-NN.
   *
   * Finds the k nearest neighbors to the query vector based on
   * the namespace's configured distance metric.
   *
   * @param {string} namespace - Namespace name
   * @param {number[]} vector - Query vector (must match namespace dimensions)
   * @param {SearchOptions} [options={}] - Search options
   * @returns {Promise<SearchResponse>} Search results ordered by similarity
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const results = await vectorClient.searchSimilar('embeddings', queryVector, {
   *   k: 10,
   *   filter: { category: 'science' },
   *   includeMetadata: true
   * });
   * results.results.forEach(r => {
   *   console.log(`${r.id}: score=${r.score}`);
   * });
   */
  async searchSimilar(namespace, vector, options = {}) {
    requiredString(namespace, 'namespace');
    this.#validateVector(vector, 'vector');
    this.#validateSearchOptions(options);

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/search`;
    const body = {
      vector,
      k: options.k ?? 10,
      includeMetadata: options.includeMetadata ?? true,
      includeVectors: options.includeVectors ?? false,
    };

    if (options.filter) {
      body.filter = options.filter;
    }
    if (options.efSearch !== undefined) {
      body.efSearch = options.efSearch;
    }
    if (options.nprobe !== undefined) {
      body.nprobe = options.nprobe;
    }

    const response = await this.#transport.post(path, body);

    return {
      results: (response?.results || []).map(r => this.#formatSearchResult(r)),
    };
  }

  /**
   * Search vectors by metadata filter only.
   *
   * Returns vectors that match the metadata filter criteria
   * without performing vector similarity search.
   *
   * @param {string} namespace - Namespace name
   * @param {Object} filter - Metadata filter criteria
   * @param {MetadataSearchOptions} [options={}] - Search options
   * @returns {Promise<SearchResponse>} Matching vectors
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const results = await vectorClient.searchByMetadata('embeddings', {
   *   category: 'science',
   *   year: { $gte: 2020 }
   * }, { limit: 50 });
   */
  async searchByMetadata(namespace, filter, options = {}) {
    requiredString(namespace, 'namespace');
    requiredObject(filter, 'filter');
    this.#validateMetadataSearchOptions(options);

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/search/metadata`;
    const body = {
      filter,
      limit: options.limit ?? 100,
      offset: options.offset ?? 0,
      includeVectors: options.includeVectors ?? false,
    };

    const response = await this.#transport.post(path, body);

    return {
      results: (response?.results || []).map(r => this.#formatSearchResult(r)),
    };
  }

  /**
   * Perform hybrid search combining vector similarity and metadata filtering.
   *
   * Combines vector similarity scores with metadata matching
   * using a weighted approach controlled by the alpha parameter.
   *
   * @param {string} namespace - Namespace name
   * @param {number[]} vector - Query vector
   * @param {Object} filter - Metadata filter criteria
   * @param {HybridSearchOptions} [options={}] - Hybrid search options
   * @returns {Promise<SearchResponse>} Search results with combined scoring
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const results = await vectorClient.hybridSearch('embeddings', queryVector, {
   *   category: 'science'
   * }, {
   *   k: 10,
   *   alpha: 0.7  // 70% vector similarity, 30% metadata
   * });
   */
  async hybridSearch(namespace, vector, filter, options = {}) {
    requiredString(namespace, 'namespace');
    this.#validateVector(vector, 'vector');
    requiredObject(filter, 'filter');
    this.#validateHybridSearchOptions(options);

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/search/hybrid`;
    const body = {
      vector,
      filter,
      k: options.k ?? 10,
      alpha: options.alpha ?? 0.5,
      includeMetadata: options.includeMetadata ?? true,
      includeVectors: options.includeVectors ?? false,
    };

    const response = await this.#transport.post(path, body);

    return {
      results: (response?.results || []).map(r => this.#formatSearchResult(r)),
    };
  }

  // ============================================================
  // Index Management
  // ============================================================

  /**
   * Rebuild the vector index for a namespace.
   *
   * Useful after bulk insertions or to change index parameters.
   * By default runs asynchronously - use getRebuildProgress to monitor.
   *
   * @param {string} namespace - Namespace name
   * @param {RebuildOptions} [options={}] - Rebuild options
   * @returns {Promise<{jobId: string, status: string}>} Rebuild job information
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const job = await vectorClient.rebuildIndex('embeddings', {
   *   indexType: IndexType.HNSW,
   *   indexParams: { m: 32, efConstruction: 400 }
   * });
   * console.log(`Rebuild job started: ${job.jobId}`);
   */
  async rebuildIndex(namespace, options = {}) {
    requiredString(namespace, 'namespace');
    this.#validateRebuildOptions(options);

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/index/rebuild`;
    const body = {
      async: options.async ?? true,
    };

    if (options.indexType) {
      body.indexType = options.indexType;
    }
    if (options.indexParams) {
      body.indexParams = options.indexParams;
    }

    const response = await this.#transport.post(path, body);

    return {
      jobId: response?.jobId || '',
      status: response?.status || 'pending',
    };
  }

  /**
   * Get the progress of an index rebuild operation.
   *
   * @param {string} namespace - Namespace name
   * @returns {Promise<RebuildProgress>} Rebuild progress information
   * @throws {ValidationError} When namespace is invalid
   *
   * @example
   * const progress = await vectorClient.getRebuildProgress('embeddings');
   * console.log(`Rebuild: ${progress.progress}% complete`);
   * if (progress.status === 'completed') {
   *   console.log('Index rebuild finished');
   * }
   */
  async getRebuildProgress(namespace) {
    requiredString(namespace, 'namespace');

    const path = `${API_BASE_PATH}/${encodeURIComponent(namespace)}/index/rebuild/progress`;
    const response = await this.#transport.get(path);

    return {
      status: response?.status || 'unknown',
      progress: response?.progress ?? 0,
      vectorsProcessed: response?.vectorsProcessed ?? 0,
      totalVectors: response?.totalVectors ?? 0,
      error: response?.error || null,
      startedAt: response?.startedAt || null,
      completedAt: response?.completedAt || null,
    };
  }

  // ============================================================
  // Private Validation Methods
  // ============================================================

  /**
   * Validate namespace options.
   * @param {NamespaceOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateNamespaceOptions(options) {
    if (options.distanceMetric !== undefined) {
      oneOf(
        options.distanceMetric,
        Object.values(DistanceMetric),
        'options.distanceMetric'
      );
    }
    if (options.indexType !== undefined) {
      oneOf(
        options.indexType,
        Object.values(IndexType),
        'options.indexType'
      );
    }
    if (options.indexParams !== undefined && options.indexParams !== null) {
      if (typeof options.indexParams !== 'object' || Array.isArray(options.indexParams)) {
        throw new ValidationError('options.indexParams must be an object', 'options.indexParams');
      }
      this.#validateIndexParams(options.indexParams);
    }
  }

  /**
   * Validate index parameters.
   * @param {Object} params - Index parameters to validate
   * @throws {ValidationError} If parameters are invalid
   * @private
   */
  #validateIndexParams(params) {
    if (params.m !== undefined) {
      positiveInteger(params.m, 'indexParams.m');
    }
    if (params.efConstruction !== undefined) {
      positiveInteger(params.efConstruction, 'indexParams.efConstruction');
    }
    if (params.nlist !== undefined) {
      positiveInteger(params.nlist, 'indexParams.nlist');
    }
    if (params.nprobe !== undefined) {
      positiveInteger(params.nprobe, 'indexParams.nprobe');
    }
  }

  /**
   * Validate a vector array.
   * @param {number[]} vector - Vector to validate
   * @param {string} name - Parameter name for error messages
   * @throws {ValidationError} If vector is invalid
   * @private
   */
  #validateVector(vector, name) {
    requiredArray(vector, name);
    if (vector.length === 0) {
      throw new ValidationError(`${name} cannot be empty`, name);
    }
    for (let i = 0; i < vector.length; i++) {
      if (typeof vector[i] !== 'number' || !Number.isFinite(vector[i])) {
        throw new ValidationError(
          `${name}[${i}] must be a finite number`,
          `${name}[${i}]`
        );
      }
    }
  }

  /**
   * Validate batch vectors array.
   * @param {BatchVectorItem[]} vectors - Vectors to validate
   * @throws {ValidationError} If vectors are invalid
   * @private
   */
  #validateBatchVectors(vectors) {
    nonEmptyArray(vectors, 'vectors');

    let expectedDimensions = null;

    for (let i = 0; i < vectors.length; i++) {
      const item = vectors[i];

      if (!item || typeof item !== 'object' || Array.isArray(item)) {
        throw new ValidationError(`vectors[${i}] must be an object`, `vectors[${i}]`);
      }

      requiredString(item.id, `vectors[${i}].id`);
      this.#validateVector(item.vector, `vectors[${i}].vector`);

      // Ensure all vectors have the same dimensions
      if (expectedDimensions === null) {
        expectedDimensions = item.vector.length;
      } else if (item.vector.length !== expectedDimensions) {
        throw new ValidationError(
          `vectors[${i}].vector has ${item.vector.length} dimensions, expected ${expectedDimensions}`,
          `vectors[${i}].vector`
        );
      }

      if (item.metadata !== undefined && item.metadata !== null) {
        if (typeof item.metadata !== 'object' || Array.isArray(item.metadata)) {
          throw new ValidationError(`vectors[${i}].metadata must be an object`, `vectors[${i}].metadata`);
        }
      }
    }
  }

  /**
   * Validate search options.
   * @param {SearchOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateSearchOptions(options) {
    if (options.k !== undefined) {
      positiveInteger(options.k, 'options.k');
    }
    if (options.efSearch !== undefined) {
      positiveInteger(options.efSearch, 'options.efSearch');
    }
    if (options.nprobe !== undefined) {
      positiveInteger(options.nprobe, 'options.nprobe');
    }
    if (options.filter !== undefined && options.filter !== null) {
      if (typeof options.filter !== 'object' || Array.isArray(options.filter)) {
        throw new ValidationError('options.filter must be an object', 'options.filter');
      }
    }
  }

  /**
   * Validate metadata search options.
   * @param {MetadataSearchOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateMetadataSearchOptions(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
    }
  }

  /**
   * Validate hybrid search options.
   * @param {HybridSearchOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateHybridSearchOptions(options) {
    if (options.k !== undefined) {
      positiveInteger(options.k, 'options.k');
    }
    if (options.alpha !== undefined) {
      if (typeof options.alpha !== 'number' || options.alpha < 0 || options.alpha > 1) {
        throw new ValidationError('options.alpha must be a number between 0 and 1', 'options.alpha');
      }
    }
  }

  /**
   * Validate rebuild options.
   * @param {RebuildOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateRebuildOptions(options) {
    if (options.indexType !== undefined) {
      oneOf(
        options.indexType,
        Object.values(IndexType),
        'options.indexType'
      );
    }
    if (options.indexParams !== undefined && options.indexParams !== null) {
      if (typeof options.indexParams !== 'object' || Array.isArray(options.indexParams)) {
        throw new ValidationError('options.indexParams must be an object', 'options.indexParams');
      }
      this.#validateIndexParams(options.indexParams);
    }
  }

  // ============================================================
  // Private Helper Methods
  // ============================================================

  /**
   * Format namespace info response.
   * @param {Object} response - Raw response
   * @returns {NamespaceInfo} Formatted namespace info
   * @private
   */
  #formatNamespaceInfo(response) {
    return {
      name: response?.name || '',
      dimensions: response?.dimensions ?? 0,
      distanceMetric: response?.distanceMetric || DistanceMetric.COSINE,
      indexType: response?.indexType || IndexType.HNSW,
      vectorCount: response?.vectorCount ?? 0,
      createdAt: response?.createdAt || null,
      updatedAt: response?.updatedAt || null,
    };
  }

  /**
   * Format search result response.
   * @param {Object} result - Raw result
   * @returns {SearchResult} Formatted search result
   * @private
   */
  #formatSearchResult(result) {
    const formatted = {
      id: result?.id || '',
      score: result?.score ?? 0,
    };

    if (result?.vector !== undefined) {
      formatted.vector = result.vector;
    }
    if (result?.metadata !== undefined) {
      formatted.metadata = result.metadata;
    }

    return formatted;
  }
}

export default VectorClient;
