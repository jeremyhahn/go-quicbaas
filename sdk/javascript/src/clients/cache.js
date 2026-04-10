/**
 * QuicBaaS JavaScript SDK - Cache Client
 *
 * @module @quicbaas/client/clients/cache
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import { required, nonNegativeInteger, positiveInteger } from '../utils/validation.js';

/**
 * @typedef {Object} CacheStatsOptions
 * @property {string} [entityType] - Filter statistics to a specific entity type
 */

/**
 * @typedef {Object} CacheStatsResponse
 * @property {number} hitRate - Cache hit ratio (0.0 to 1.0)
 * @property {number} size - Current number of entries in the cache
 * @property {number} capacity - Maximum number of entries the cache can hold
 * @property {number} hits - Total number of cache hits
 * @property {number} misses - Total number of cache misses
 * @property {number} evictions - Total number of entries evicted
 */

/**
 * @typedef {Object} ClearCacheOptions
 * @property {string} [entityType] - Clear only entries for a specific entity type
 * @property {boolean} [all] - Clear all cache entries when true
 */

/**
 * @typedef {Object} ClearCacheResponse
 * @property {number} entriesCleared - Number of entries removed from the cache
 */

/**
 * @typedef {Object} CacheConfigResponse
 * @property {number} l1Size - Maximum number of entries in the L1 cache
 * @property {number} l1TTL - Time-to-live for L1 cache entries in milliseconds
 * @property {number} cleanupInterval - Interval between cache cleanup cycles in milliseconds
 * @property {boolean} enabled - Whether caching is enabled
 */

/**
 * @typedef {Object} UpdateCacheConfigRequest
 * @property {number} [l1Size] - Maximum number of entries in the L1 cache
 * @property {number} [l1TTL] - Time-to-live for L1 cache entries in milliseconds
 * @property {number} [cleanupInterval] - Interval between cache cleanup cycles in milliseconds
 * @property {boolean} [enabled] - Whether caching is enabled
 */

/**
 * @typedef {Object} UpdateCacheConfigResponse
 * @property {boolean} updated - Whether the configuration was successfully updated
 * @property {CacheConfigResponse} [newConfig] - New cache configuration after the update
 */

/**
 * Cache client for QuicBaaS.
 * Provides cache management operations for monitoring and configuring
 * the database caching layer.
 */
class CacheClient {
  /**
   * API path prefix for cache operations.
   * @type {string}
   * @private
   */
  static API_PREFIX = '/api/v1/cache';

  /**
   * Create a new Cache client.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  /**
   * Get cache statistics with optional filtering by entity type.
   * Returns metrics about cache performance including hit rate, size, and evictions.
   *
   * @param {CacheStatsOptions} [options={}] - Cache statistics options
   * @returns {Promise<CacheStatsResponse>} Cache statistics
   * @throws {ValidationError} If options contain invalid values
   *
   * @example
   * // Get all cache statistics
   * const stats = await cacheClient.getCacheStats();
   * console.log(`Hit rate: ${(stats.hitRate * 100).toFixed(2)}%`);
   * console.log(`Size: ${stats.size}/${stats.capacity}`);
   *
   * @example
   * // Get statistics for a specific entity type
   * const stats = await cacheClient.getCacheStats({ entityType: 'user' });
   * console.log(`User cache hits: ${stats.hits}`);
   */
  async getCacheStats(options = {}) {
    this.#validateCacheStatsOptions(options);

    const query = {};

    if (options.entityType !== undefined && options.entityType !== null) {
      query.entityType = options.entityType;
    }

    const path = `${CacheClient.API_PREFIX}/stats`;
    const response = await this.#transport.get(path, { query });

    return this.#normalizeCacheStatsResponse(response);
  }

  /**
   * Clear cache entries with optional filtering.
   * Can clear all entries or only entries for a specific entity type.
   *
   * @param {ClearCacheOptions} [options={}] - Clear cache options
   * @returns {Promise<ClearCacheResponse>} Clear cache result
   * @throws {ValidationError} If neither entityType nor all is specified
   *
   * @example
   * // Clear all cache entries
   * const result = await cacheClient.clearCache({ all: true });
   * console.log(`Cleared ${result.entriesCleared} entries`);
   *
   * @example
   * // Clear cache for a specific entity type
   * const result = await cacheClient.clearCache({ entityType: 'session' });
   * console.log(`Cleared ${result.entriesCleared} session entries`);
   */
  async clearCache(options = {}) {
    this.#validateClearCacheOptions(options);

    const body = {};

    if (options.entityType !== undefined && options.entityType !== null) {
      body.entityType = options.entityType;
    }

    if (options.all === true) {
      body.all = true;
    }

    const path = `${CacheClient.API_PREFIX}/clear`;
    const response = await this.#transport.post(path, body);

    return this.#normalizeClearCacheResponse(response);
  }

  /**
   * Get the current cache configuration.
   * Returns settings for cache size, TTL, and cleanup intervals.
   *
   * @returns {Promise<CacheConfigResponse>} Current cache configuration
   *
   * @example
   * const config = await cacheClient.getCacheConfig();
   * console.log(`L1 Cache Size: ${config.l1Size}`);
   * console.log(`L1 TTL: ${config.l1TTL}ms`);
   * console.log(`Cleanup Interval: ${config.cleanupInterval}ms`);
   * console.log(`Enabled: ${config.enabled}`);
   */
  async getCacheConfig() {
    const path = `${CacheClient.API_PREFIX}/config`;
    const response = await this.#transport.get(path);

    return this.#normalizeCacheConfigResponse(response);
  }

  /**
   * Update the cache configuration.
   * Allows modifying cache size, TTL, cleanup interval, and enable/disable state.
   * Only fields that are provided will be updated.
   *
   * @param {UpdateCacheConfigRequest} config - Configuration updates
   * @returns {Promise<UpdateCacheConfigResponse>} Update result with new configuration
   * @throws {ValidationError} If no configuration fields are provided
   * @throws {ValidationError} If any configuration values are invalid
   *
   * @example
   * // Update L1 cache size
   * const result = await cacheClient.updateCacheConfig({ l1Size: 10000 });
   * console.log(`Updated: ${result.updated}`);
   * console.log(`New size: ${result.newConfig.l1Size}`);
   *
   * @example
   * // Disable caching
   * const result = await cacheClient.updateCacheConfig({ enabled: false });
   *
   * @example
   * // Update multiple settings
   * const result = await cacheClient.updateCacheConfig({
   *   l1Size: 5000,
   *   l1TTL: 60000,
   *   cleanupInterval: 30000
   * });
   */
  async updateCacheConfig(config) {
    this.#validateUpdateCacheConfigRequest(config);

    const body = {};

    if (config.l1Size !== undefined) {
      body.l1Size = config.l1Size;
    }

    if (config.l1TTL !== undefined) {
      body.l1TTL = config.l1TTL;
    }

    if (config.cleanupInterval !== undefined) {
      body.cleanupInterval = config.cleanupInterval;
    }

    if (config.enabled !== undefined) {
      body.enabled = config.enabled;
    }

    const path = `${CacheClient.API_PREFIX}/config`;
    const response = await this.#transport.put(path, body);

    return this.#normalizeUpdateCacheConfigResponse(response);
  }

  /**
   * Validate cache statistics options.
   * @param {CacheStatsOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateCacheStatsOptions(options) {
    if (options === null || options === undefined) {
      throw new ValidationError('options must be an object', 'options');
    }

    if (typeof options !== 'object' || Array.isArray(options)) {
      throw new ValidationError('options must be an object', 'options');
    }

    if (options.entityType !== undefined && options.entityType !== null) {
      if (typeof options.entityType !== 'string') {
        throw new ValidationError('entityType must be a string', 'entityType');
      }
      if (options.entityType.trim() === '') {
        throw new ValidationError('entityType cannot be empty', 'entityType');
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
    if (options === null || options === undefined) {
      throw new ValidationError('options must be an object', 'options');
    }

    if (typeof options !== 'object' || Array.isArray(options)) {
      throw new ValidationError('options must be an object', 'options');
    }

    const hasEntityType = options.entityType !== undefined && options.entityType !== null;
    const hasAll = options.all === true;

    if (!hasEntityType && !hasAll) {
      throw new ValidationError(
        'either entityType or all flag must be specified',
        'options'
      );
    }

    if (hasEntityType) {
      if (typeof options.entityType !== 'string') {
        throw new ValidationError('entityType must be a string', 'entityType');
      }
      if (options.entityType.trim() === '') {
        throw new ValidationError('entityType cannot be empty', 'entityType');
      }
    }

    if (options.all !== undefined && typeof options.all !== 'boolean') {
      throw new ValidationError('all must be a boolean', 'all');
    }
  }

  /**
   * Validate update cache config request.
   * @param {UpdateCacheConfigRequest} config - Configuration to validate
   * @throws {ValidationError} If configuration is invalid
   * @private
   */
  #validateUpdateCacheConfigRequest(config) {
    required(config, 'config');

    if (typeof config !== 'object' || Array.isArray(config)) {
      throw new ValidationError('config must be an object', 'config');
    }

    const hasL1Size = config.l1Size !== undefined;
    const hasL1TTL = config.l1TTL !== undefined;
    const hasCleanupInterval = config.cleanupInterval !== undefined;
    const hasEnabled = config.enabled !== undefined;

    if (!hasL1Size && !hasL1TTL && !hasCleanupInterval && !hasEnabled) {
      throw new ValidationError(
        'at least one configuration field must be specified',
        'config'
      );
    }

    if (hasL1Size) {
      if (typeof config.l1Size !== 'number' || !Number.isInteger(config.l1Size)) {
        throw new ValidationError('l1Size must be an integer', 'l1Size');
      }
      if (config.l1Size < 0) {
        throw new ValidationError('l1Size cannot be negative', 'l1Size');
      }
    }

    if (hasL1TTL) {
      if (typeof config.l1TTL !== 'number' || !Number.isInteger(config.l1TTL)) {
        throw new ValidationError('l1TTL must be an integer', 'l1TTL');
      }
      if (config.l1TTL < 0) {
        throw new ValidationError('l1TTL cannot be negative', 'l1TTL');
      }
    }

    if (hasCleanupInterval) {
      if (typeof config.cleanupInterval !== 'number' || !Number.isInteger(config.cleanupInterval)) {
        throw new ValidationError('cleanupInterval must be an integer', 'cleanupInterval');
      }
      if (config.cleanupInterval < 0) {
        throw new ValidationError('cleanupInterval cannot be negative', 'cleanupInterval');
      }
    }

    if (hasEnabled && typeof config.enabled !== 'boolean') {
      throw new ValidationError('enabled must be a boolean', 'enabled');
    }
  }

  /**
   * Normalize cache stats response to ensure consistent structure.
   * @param {Object} response - Raw response from the server
   * @returns {CacheStatsResponse} Normalized cache stats response
   * @private
   */
  #normalizeCacheStatsResponse(response) {
    return {
      hitRate: response?.hitRate ?? 0,
      size: response?.size ?? 0,
      capacity: response?.maxSize ?? response?.capacity ?? 0,
      hits: response?.hits ?? 0,
      misses: response?.misses ?? 0,
      evictions: response?.evictions ?? 0,
    };
  }

  /**
   * Normalize clear cache response to ensure consistent structure.
   * @param {Object} response - Raw response from the server
   * @returns {ClearCacheResponse} Normalized clear cache response
   * @private
   */
  #normalizeClearCacheResponse(response) {
    return {
      entriesCleared: response?.entriesCleared ?? 0,
    };
  }

  /**
   * Normalize cache config response to ensure consistent structure.
   * @param {Object} response - Raw response from the server
   * @returns {CacheConfigResponse} Normalized cache config response
   * @private
   */
  #normalizeCacheConfigResponse(response) {
    return {
      l1Size: response?.l1Size ?? 0,
      l1TTL: response?.l1TTL ?? 0,
      cleanupInterval: response?.cleanupInterval ?? 0,
      enabled: response?.enabled ?? false,
    };
  }

  /**
   * Normalize update cache config response to ensure consistent structure.
   * @param {Object} response - Raw response from the server
   * @returns {UpdateCacheConfigResponse} Normalized update config response
   * @private
   */
  #normalizeUpdateCacheConfigResponse(response) {
    const result = {
      updated: response?.updated ?? false,
    };

    if (response?.newConfig) {
      result.newConfig = this.#normalizeCacheConfigResponse(response.newConfig);
    }

    return result;
  }
}

export default CacheClient;
