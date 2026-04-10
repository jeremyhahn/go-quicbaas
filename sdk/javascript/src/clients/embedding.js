/**
 * QuicBaaS JavaScript SDK - Embedding Client
 *
 * Provides embedding generation operations including single and batch
 * embedding generation, provider management, and cache management.
 *
 * @module @quicbaas/client/clients/embedding
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  nonEmptyArray,
  oneOf,
} from '../utils/validation.js';

/**
 * Embedding provider types.
 * @readonly
 * @enum {string}
 */
export const Provider = Object.freeze({
  /** Voyage AI embedding models */
  VOYAGE: 'voyage',
  /** OpenAI embedding models */
  OPENAI: 'openai',
  /** Local Ollama embedding models */
  OLLAMA: 'ollama',
  /** Local LM Studio embedding models */
  LMSTUDIO: 'lmstudio',
});

/**
 * Input types for embedding generation.
 * @readonly
 * @enum {string}
 */
export const InputType = Object.freeze({
  /** Document or long-form text */
  DOCUMENT: 'document',
  /** Search query text */
  QUERY: 'query',
  /** Source code */
  CODE: 'code',
});

/**
 * Provider availability status.
 * @readonly
 * @enum {string}
 */
export const Status = Object.freeze({
  /** Provider is ready and available */
  AVAILABLE: 'available',
  /** Provider is currently unavailable */
  UNAVAILABLE: 'unavailable',
  /** Provider has hit rate limits */
  RATE_LIMITED: 'rate_limited',
  /** Provider has errors */
  ERROR: 'error',
});

/**
 * @typedef {Object} GenerateEmbeddingOptions
 * @property {string} [provider] - Provider to use (uses default if not specified)
 * @property {string} [inputType] - Type of input text (document, query, code)
 * @property {Object} [metadata] - Key-value metadata to attach
 * @property {string} [source] - Source identifier (file path, URL, etc.)
 * @property {string} [language] - Programming or natural language
 * @property {boolean} [cacheBypass=false] - Force generation even if cached
 */

/**
 * @typedef {Object} EmbeddingMetadata
 * @property {string} model - Embedding model name
 * @property {string} provider - Provider that generated the embedding
 * @property {number} dimensions - Embedding vector dimensions
 * @property {number} tokens - Number of tokens processed
 * @property {string} [source] - Source identifier
 * @property {string} [language] - Programming or natural language
 * @property {string} [inputType] - Type of input text
 * @property {Object} [tags] - Custom user-defined tags
 * @property {boolean} cacheHit - Whether result was served from cache
 * @property {number} processingTimeMs - Time taken to generate in milliseconds
 */

/**
 * @typedef {Object} GenerateEmbeddingResponse
 * @property {number[]} embedding - The embedding vector
 * @property {EmbeddingMetadata} metadata - Embedding metadata
 * @property {string} timestamp - When the embedding was generated
 */

/**
 * @typedef {Object} BatchEmbeddingItem
 * @property {string} id - Client-provided identifier for this item
 * @property {string} text - Input text to embed
 * @property {string} [inputType] - Type of input text
 * @property {Object} [metadata] - Optional metadata
 * @property {string} [source] - Source identifier
 * @property {string} [language] - Programming or natural language
 */

/**
 * @typedef {Object} BatchGenerateOptions
 * @property {string} [provider] - Provider to use (uses default if not specified)
 * @property {boolean} [cacheBypass=false] - Force generation even if cached
 */

/**
 * @typedef {Object} BatchEmbeddingResult
 * @property {string} id - Client-provided identifier
 * @property {number[]} embedding - The embedding vector
 * @property {EmbeddingMetadata} metadata - Embedding metadata
 * @property {string} [error] - Error message for this item if failed
 */

/**
 * @typedef {Object} BatchGenerateResponse
 * @property {BatchEmbeddingResult[]} results - Per-item results in request order
 * @property {number} successCount - Number of successful embeddings
 * @property {number} errorCount - Number of failed embeddings
 * @property {number} totalTokens - Total tokens processed
 * @property {number} processingTimeMs - Total processing time in milliseconds
 * @property {string} timestamp - When the batch was processed
 */

/**
 * @typedef {Object} ListProvidersOptions
 * @property {boolean} [includeUnavailable=false] - Include unavailable providers
 */

/**
 * @typedef {Object} ProviderInfo
 * @property {string} name - Provider name
 * @property {string} type - Provider type
 * @property {string} model - Model name being used
 * @property {number} dimensions - Embedding vector dimensions
 * @property {number} maxTokens - Maximum tokens per request
 * @property {number} maxBatchSize - Maximum batch size supported
 * @property {string} status - Current provider status
 * @property {string} apiUrl - Provider API endpoint
 * @property {number} rateLimitPerMinute - Rate limit (0 = unlimited)
 * @property {number} timeoutMs - Request timeout in milliseconds
 * @property {boolean} isDefault - Whether this is the default provider
 * @property {string} [lastError] - Last error message if any
 * @property {string} [lastErrorTime] - When the last error occurred
 * @property {number} successCount - Total successful requests
 * @property {number} errorCount - Total failed requests
 * @property {number} avgLatencyMs - Average request latency in milliseconds
 */

/**
 * @typedef {Object} ListProvidersResponse
 * @property {ProviderInfo[]} providers - List of available providers
 * @property {string} defaultProvider - Name of the default provider
 * @property {string} timestamp - When the list was generated
 */

/**
 * @typedef {Object} GetProviderInfoResponse
 * @property {ProviderInfo} info - Provider information
 * @property {string} timestamp - When the info was retrieved
 */

/**
 * @typedef {Object} SetDefaultProviderResponse
 * @property {string} provider - New default provider
 * @property {string} previousProvider - Previous default provider
 * @property {string} timestamp - When the change was made
 */

/**
 * @typedef {Object} CacheStats
 * @property {boolean} enabled - Whether caching is enabled
 * @property {number} size - Current number of cached embeddings
 * @property {number} capacity - Maximum cache capacity
 * @property {number} hits - Total cache hits
 * @property {number} misses - Total cache misses
 * @property {number} hitRate - Cache hit rate (0.0 to 1.0)
 * @property {number} evictions - Total number of evictions
 * @property {number} memoryBytes - Approximate memory used by cache
 * @property {boolean} compressionEnabled - Whether compression is enabled
 * @property {number} compressionRatio - Average compression ratio
 * @property {string} timestamp - When stats were collected
 */

/**
 * @typedef {Object} ClearCacheOptions
 * @property {string} [provider] - Clear cache only for specific provider
 * @property {boolean} [all=false] - Clear entire cache regardless of provider
 */

/**
 * @typedef {Object} ClearCacheResponse
 * @property {number} entriesCleared - Number of cache entries removed
 * @property {string} [provider] - Provider whose cache was cleared
 * @property {string} timestamp - When the cache was cleared
 */

/**
 * API base path for embedding operations.
 * @constant {string}
 */
const API_BASE_PATH = '/api/v1/embedding';

/**
 * EmbeddingClient provides embedding generation operations for QuicBaaS.
 *
 * Supports single and batch embedding generation, provider management,
 * and cache management with multiple embedding providers.
 *
 * @example
 * const transport = new HttpTransport('http://localhost:8080');
 * const embeddingClient = new EmbeddingClient(transport);
 *
 * // Generate a single embedding
 * const result = await embeddingClient.generateEmbedding('Hello, world!', {
 *   provider: Provider.VOYAGE,
 *   inputType: InputType.QUERY
 * });
 *
 * // Batch generate embeddings
 * const batchResult = await embeddingClient.batchGenerateEmbeddings([
 *   { id: 'doc1', text: 'First document' },
 *   { id: 'doc2', text: 'Second document' }
 * ]);
 *
 * // List available providers
 * const providers = await embeddingClient.listProviders();
 */
class EmbeddingClient {
  /**
   * Create a new EmbeddingClient instance.
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
  // Embedding Generation
  // ============================================================

  /**
   * Generate an embedding for a single text.
   *
   * Generates a vector embedding for the provided text using the specified
   * or default embedding provider.
   *
   * @param {string} text - Input text to embed
   * @param {GenerateEmbeddingOptions} [options={}] - Generation options
   * @returns {Promise<GenerateEmbeddingResponse>} Generated embedding with metadata
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const result = await embeddingClient.generateEmbedding(
   *   'What is machine learning?',
   *   {
   *     provider: Provider.VOYAGE,
   *     inputType: InputType.QUERY,
   *     metadata: { category: 'question' }
   *   }
   * );
   * console.log(`Embedding dimensions: ${result.embedding.length}`);
   * console.log(`Cache hit: ${result.metadata.cacheHit}`);
   */
  async generateEmbedding(text, options = {}) {
    requiredString(text, 'text');
    this.#validateGenerateOptions(options);

    const path = `${API_BASE_PATH}/generate`;
    const body = {
      text,
    };

    if (options.provider !== undefined) {
      body.provider = options.provider;
    }
    if (options.inputType !== undefined) {
      body.inputType = options.inputType;
    }
    if (options.metadata !== undefined) {
      body.metadata = options.metadata;
    }
    if (options.source !== undefined) {
      body.source = options.source;
    }
    if (options.language !== undefined) {
      body.language = options.language;
    }
    if (options.cacheBypass !== undefined) {
      body.cacheBypass = options.cacheBypass;
    }

    const response = await this.#transport.post(path, body);
    return this.#formatGenerateResponse(response);
  }

  /**
   * Generate embeddings for multiple texts in a single request.
   *
   * More efficient than individual calls for bulk embedding operations.
   * Each item can have its own input type and metadata.
   *
   * @param {BatchEmbeddingItem[]} items - Array of items to embed
   * @param {BatchGenerateOptions} [options={}] - Batch generation options
   * @returns {Promise<BatchGenerateResponse>} Batch results with per-item embeddings
   * @throws {ValidationError} When parameters are invalid
   *
   * @example
   * const result = await embeddingClient.batchGenerateEmbeddings([
   *   { id: 'doc1', text: 'First document', inputType: InputType.DOCUMENT },
   *   { id: 'doc2', text: 'Second document', metadata: { author: 'Alice' } },
   *   { id: 'code1', text: 'function hello() {}', inputType: InputType.CODE }
   * ], { provider: Provider.OPENAI });
   *
   * console.log(`Success: ${result.successCount}, Errors: ${result.errorCount}`);
   * result.results.forEach(r => {
   *   if (r.error) {
   *     console.error(`${r.id} failed: ${r.error}`);
   *   } else {
   *     console.log(`${r.id}: ${r.embedding.length} dimensions`);
   *   }
   * });
   */
  async batchGenerateEmbeddings(items, options = {}) {
    this.#validateBatchItems(items);
    this.#validateBatchOptions(options);

    const path = `${API_BASE_PATH}/batch`;
    const body = {
      items: items.map(item => ({
        id: item.id,
        text: item.text,
        inputType: item.inputType,
        metadata: item.metadata,
        source: item.source,
        language: item.language,
      })),
    };

    if (options.provider !== undefined) {
      body.provider = options.provider;
    }
    if (options.cacheBypass !== undefined) {
      body.cacheBypass = options.cacheBypass;
    }

    const response = await this.#transport.post(path, body);
    return this.#formatBatchResponse(response);
  }

  // ============================================================
  // Provider Management
  // ============================================================

  /**
   * List all available embedding providers.
   *
   * Returns information about all registered providers including their
   * capabilities, status, and statistics.
   *
   * @param {ListProvidersOptions} [options={}] - List options
   * @returns {Promise<ListProvidersResponse>} List of providers
   *
   * @example
   * const result = await embeddingClient.listProviders({ includeUnavailable: true });
   * result.providers.forEach(p => {
   *   console.log(`${p.name}: ${p.status} (${p.dimensions} dims)`);
   * });
   * console.log(`Default provider: ${result.defaultProvider}`);
   */
  async listProviders(options = {}) {
    const path = `${API_BASE_PATH}/providers`;
    const queryParams = new URLSearchParams();

    if (options.includeUnavailable) {
      queryParams.set('includeUnavailable', 'true');
    }

    const queryString = queryParams.toString();
    const fullPath = queryString ? `${path}?${queryString}` : path;

    const response = await this.#transport.get(fullPath);
    return this.#formatListProvidersResponse(response);
  }

  /**
   * Get detailed information about a specific provider.
   *
   * @param {string} provider - Provider name to get info for
   * @returns {Promise<GetProviderInfoResponse>} Provider information
   * @throws {ValidationError} When provider is not specified
   *
   * @example
   * const result = await embeddingClient.getProviderInfo(Provider.VOYAGE);
   * console.log(`Model: ${result.info.model}`);
   * console.log(`Max tokens: ${result.info.maxTokens}`);
   * console.log(`Success rate: ${result.info.successCount / (result.info.successCount + result.info.errorCount)}`);
   */
  async getProviderInfo(provider) {
    requiredString(provider, 'provider');
    oneOf(provider, Object.values(Provider), 'provider');

    const path = `${API_BASE_PATH}/providers/${encodeURIComponent(provider)}`;
    const response = await this.#transport.get(path);

    return {
      info: this.#formatProviderInfo(response?.info || response),
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Set the default embedding provider.
   *
   * The default provider is used when no provider is specified in
   * embedding generation requests.
   *
   * @param {string} provider - Provider to set as default
   * @returns {Promise<SetDefaultProviderResponse>} Confirmation with previous default
   * @throws {ValidationError} When provider is not specified
   *
   * @example
   * const result = await embeddingClient.setDefaultProvider(Provider.OLLAMA);
   * console.log(`Changed from ${result.previousProvider} to ${result.provider}`);
   */
  async setDefaultProvider(provider) {
    requiredString(provider, 'provider');
    oneOf(provider, Object.values(Provider), 'provider');

    const path = `${API_BASE_PATH}/providers/default`;
    const body = { provider };

    const response = await this.#transport.post(path, body);

    return {
      provider: response?.provider || provider,
      previousProvider: response?.previousProvider || '',
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  // ============================================================
  // Cache Management
  // ============================================================

  /**
   * Get embedding cache statistics.
   *
   * Returns statistics about the embedding cache including hit rate,
   * size, and memory usage.
   *
   * @returns {Promise<CacheStats>} Cache statistics
   *
   * @example
   * const stats = await embeddingClient.getCacheStats();
   * console.log(`Cache size: ${stats.size} / ${stats.capacity}`);
   * console.log(`Hit rate: ${(stats.hitRate * 100).toFixed(1)}%`);
   * console.log(`Memory: ${(stats.memoryBytes / 1024 / 1024).toFixed(2)} MB`);
   */
  async getCacheStats() {
    const path = `${API_BASE_PATH}/cache/stats`;
    const response = await this.#transport.get(path);

    return {
      enabled: response?.enabled ?? false,
      size: response?.size ?? 0,
      capacity: response?.capacity ?? 0,
      hits: response?.hits ?? 0,
      misses: response?.misses ?? 0,
      hitRate: response?.hitRate ?? 0,
      evictions: response?.evictions ?? 0,
      memoryBytes: response?.memoryBytes ?? 0,
      compressionEnabled: response?.compressionEnabled ?? false,
      compressionRatio: response?.compressionRatio ?? 0,
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Clear embedding cache entries.
   *
   * Can clear all cache entries or only entries for a specific provider.
   *
   * @param {ClearCacheOptions} [options={}] - Clear options
   * @returns {Promise<ClearCacheResponse>} Confirmation with entries cleared count
   *
   * @example
   * // Clear all cache entries
   * const result = await embeddingClient.clearCache({ all: true });
   * console.log(`Cleared ${result.entriesCleared} entries`);
   *
   * // Clear cache for specific provider
   * const result2 = await embeddingClient.clearCache({ provider: Provider.VOYAGE });
   * console.log(`Cleared ${result2.entriesCleared} Voyage entries`);
   */
  async clearCache(options = {}) {
    this.#validateClearCacheOptions(options);

    const path = `${API_BASE_PATH}/cache/clear`;
    const body = {};

    if (options.provider !== undefined) {
      body.provider = options.provider;
    }
    if (options.all !== undefined) {
      body.all = options.all;
    }

    const response = await this.#transport.post(path, body);

    return {
      entriesCleared: response?.entriesCleared ?? 0,
      provider: response?.provider || options.provider || null,
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  // ============================================================
  // Private Validation Methods
  // ============================================================

  /**
   * Validate generate embedding options.
   * @param {GenerateEmbeddingOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateGenerateOptions(options) {
    if (options.provider !== undefined) {
      oneOf(options.provider, Object.values(Provider), 'options.provider');
    }
    if (options.inputType !== undefined) {
      oneOf(options.inputType, Object.values(InputType), 'options.inputType');
    }
    if (options.metadata !== undefined && options.metadata !== null) {
      if (typeof options.metadata !== 'object' || Array.isArray(options.metadata)) {
        throw new ValidationError('options.metadata must be an object', 'options.metadata');
      }
    }
    if (options.source !== undefined && options.source !== null) {
      if (typeof options.source !== 'string') {
        throw new ValidationError('options.source must be a string', 'options.source');
      }
    }
    if (options.language !== undefined && options.language !== null) {
      if (typeof options.language !== 'string') {
        throw new ValidationError('options.language must be a string', 'options.language');
      }
    }
    if (options.cacheBypass !== undefined && options.cacheBypass !== null) {
      if (typeof options.cacheBypass !== 'boolean') {
        throw new ValidationError('options.cacheBypass must be a boolean', 'options.cacheBypass');
      }
    }
  }

  /**
   * Validate batch embedding items.
   * @param {BatchEmbeddingItem[]} items - Items to validate
   * @throws {ValidationError} If items are invalid
   * @private
   */
  #validateBatchItems(items) {
    nonEmptyArray(items, 'items');

    for (let i = 0; i < items.length; i++) {
      const item = items[i];

      if (!item || typeof item !== 'object' || Array.isArray(item)) {
        throw new ValidationError(`items[${i}] must be an object`, `items[${i}]`);
      }

      requiredString(item.id, `items[${i}].id`);
      requiredString(item.text, `items[${i}].text`);

      if (item.inputType !== undefined) {
        oneOf(item.inputType, Object.values(InputType), `items[${i}].inputType`);
      }

      if (item.metadata !== undefined && item.metadata !== null) {
        if (typeof item.metadata !== 'object' || Array.isArray(item.metadata)) {
          throw new ValidationError(`items[${i}].metadata must be an object`, `items[${i}].metadata`);
        }
      }

      if (item.source !== undefined && item.source !== null) {
        if (typeof item.source !== 'string') {
          throw new ValidationError(`items[${i}].source must be a string`, `items[${i}].source`);
        }
      }

      if (item.language !== undefined && item.language !== null) {
        if (typeof item.language !== 'string') {
          throw new ValidationError(`items[${i}].language must be a string`, `items[${i}].language`);
        }
      }
    }
  }

  /**
   * Validate batch generation options.
   * @param {BatchGenerateOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateBatchOptions(options) {
    if (options.provider !== undefined) {
      oneOf(options.provider, Object.values(Provider), 'options.provider');
    }
    if (options.cacheBypass !== undefined && options.cacheBypass !== null) {
      if (typeof options.cacheBypass !== 'boolean') {
        throw new ValidationError('options.cacheBypass must be a boolean', 'options.cacheBypass');
      }
    }
  }

  /**
   * Validate clear cache options.
   * @param {ClearCacheOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateClearCacheOptions(options) {
    if (options.provider !== undefined && options.provider !== null) {
      oneOf(options.provider, Object.values(Provider), 'options.provider');
    }
    if (options.all !== undefined && options.all !== null) {
      if (typeof options.all !== 'boolean') {
        throw new ValidationError('options.all must be a boolean', 'options.all');
      }
    }
  }

  // ============================================================
  // Private Formatting Methods
  // ============================================================

  /**
   * Format generate embedding response.
   * @param {Object} response - Raw response
   * @returns {GenerateEmbeddingResponse} Formatted response
   * @private
   */
  #formatGenerateResponse(response) {
    return {
      embedding: response?.embedding || [],
      metadata: this.#formatEmbeddingMetadata(response?.metadata),
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Format embedding metadata.
   * @param {Object} metadata - Raw metadata
   * @returns {EmbeddingMetadata} Formatted metadata
   * @private
   */
  #formatEmbeddingMetadata(metadata) {
    return {
      model: metadata?.model || '',
      provider: metadata?.provider || '',
      dimensions: metadata?.dimensions ?? 0,
      tokens: metadata?.tokens ?? 0,
      source: metadata?.source || null,
      language: metadata?.language || null,
      inputType: metadata?.inputType || null,
      tags: metadata?.tags || null,
      cacheHit: metadata?.cacheHit ?? false,
      processingTimeMs: this.#formatDurationToMs(metadata?.processingTime),
    };
  }

  /**
   * Format batch generate response.
   * @param {Object} response - Raw response
   * @returns {BatchGenerateResponse} Formatted response
   * @private
   */
  #formatBatchResponse(response) {
    return {
      results: (response?.results || []).map(r => this.#formatBatchResult(r)),
      successCount: response?.successCount ?? 0,
      errorCount: response?.errorCount ?? 0,
      totalTokens: response?.totalTokens ?? 0,
      processingTimeMs: this.#formatDurationToMs(response?.processingTime),
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Format batch embedding result.
   * @param {Object} result - Raw result
   * @returns {BatchEmbeddingResult} Formatted result
   * @private
   */
  #formatBatchResult(result) {
    const formatted = {
      id: result?.id || '',
      embedding: result?.embedding || [],
      metadata: this.#formatEmbeddingMetadata(result?.metadata),
    };

    if (result?.error) {
      formatted.error = result.error;
    }

    return formatted;
  }

  /**
   * Format list providers response.
   * @param {Object} response - Raw response
   * @returns {ListProvidersResponse} Formatted response
   * @private
   */
  #formatListProvidersResponse(response) {
    return {
      providers: (response?.providers || []).map(p => this.#formatProviderInfo(p)),
      defaultProvider: response?.defaultProvider || '',
      timestamp: response?.timestamp || new Date().toISOString(),
    };
  }

  /**
   * Format provider info.
   * @param {Object} info - Raw provider info
   * @returns {ProviderInfo} Formatted provider info
   * @private
   */
  #formatProviderInfo(info) {
    return {
      name: info?.name || '',
      type: info?.type || '',
      model: info?.model || '',
      dimensions: info?.dimensions ?? 0,
      maxTokens: info?.maxTokens ?? 0,
      maxBatchSize: info?.maxBatchSize ?? 0,
      status: info?.status || Status.UNAVAILABLE,
      apiUrl: info?.apiUrl || info?.apiURL || '',
      rateLimitPerMinute: info?.rateLimitPerMinute ?? 0,
      timeoutMs: this.#formatDurationToMs(info?.timeout),
      isDefault: info?.isDefault ?? false,
      lastError: info?.lastError || null,
      lastErrorTime: info?.lastErrorTime || null,
      successCount: info?.successCount ?? 0,
      errorCount: info?.errorCount ?? 0,
      avgLatencyMs: this.#formatDurationToMs(info?.avgLatency),
    };
  }

  /**
   * Convert Go duration (nanoseconds) or string to milliseconds.
   * @param {number|string} duration - Duration value
   * @returns {number} Duration in milliseconds
   * @private
   */
  #formatDurationToMs(duration) {
    if (duration === undefined || duration === null) {
      return 0;
    }

    // If already a number, assume nanoseconds from Go time.Duration
    if (typeof duration === 'number') {
      return Math.round(duration / 1000000);
    }

    // If string, try to parse as milliseconds
    if (typeof duration === 'string') {
      const parsed = parseFloat(duration);
      return Number.isFinite(parsed) ? parsed : 0;
    }

    return 0;
  }
}

export default EmbeddingClient;
