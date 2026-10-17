/**
 * QuicBaaS JavaScript SDK - Object Transfer Client
 *
 * @module @quicbaas/client/clients/objectTransfer
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  nonEmptyArray,
} from '../utils/validation.js';

/**
 * @typedef {Object} FetchObjectOptions
 * @property {string} [checksum] - Expected checksum (base64) for verification
 * @property {string} [requestId] - Client-provided request identifier for tracking
 */

/**
 * @typedef {Object} FetchObjectResponse
 * @property {string} objectRef - Object reference identifier
 * @property {string} data - Object data (base64 encoded)
 * @property {string} checksum - SHA-256 checksum of the data (base64)
 * @property {number} size - Size of the object in bytes
 * @property {boolean} compressed - Whether the data is compressed
 * @property {string} compressionAlgo - Compression algorithm used (if compressed)
 * @property {boolean} found - Whether the object was found
 * @property {string} errorMessage - Error message if fetch failed
 */

/**
 * @typedef {Object} FetchItem
 * @property {string} objectRef - Object reference identifier
 * @property {string} [checksum] - Expected checksum (base64) for verification
 */

/**
 * @typedef {Object} StreamFetchOptions
 * @property {string} [requestId] - Client-provided request identifier for tracking
 */

/**
 * Object Transfer client for QuicBaaS.
 * Provides operations for fetching objects and streaming multiple object fetches.
 */
class ObjectTransferClient {
  /**
   * API path prefix for object transfer operations.
   * @type {string}
   * @private
   */
  static API_PREFIX = '/api/v1/objects/transfer';

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  /**
   * Create a new Object Transfer client.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  // ==========================================================================
  // Object Fetch Operations
  // ==========================================================================

  /**
   * Fetch a single object from a shard.
   * @param {number} shardId - The shard identifier
   * @param {string} objectRef - Object reference identifier
   * @param {FetchObjectOptions} [options={}] - Fetch options
   * @returns {Promise<FetchObjectResponse>} The fetched object response
   * @throws {ValidationError} If required parameters are missing or invalid
   */
  async fetchObject(shardId, objectRef, options = {}) {
    this.#validateShardId(shardId);
    requiredString(objectRef, 'objectRef');

    const path = `${ObjectTransferClient.API_PREFIX}/shards/${shardId}/objects/${encodeURIComponent(objectRef)}`;
    const query = {};

    if (options.checksum) {
      query.checksum = options.checksum;
    }
    if (options.requestId) {
      query.request_id = options.requestId;
    }

    const response = await this.#transport.get(path, { query });

    return this.#mapFetchObjectResponse(response, objectRef);
  }

  /**
   * Fetch multiple objects from a shard in a streaming fashion.
   * @param {number} shardId - The shard identifier
   * @param {Array<FetchItem>} items - Array of objects to fetch
   * @param {StreamFetchOptions} [options={}] - Stream fetch options
   * @returns {Promise<Array<FetchObjectResponse>>} Array of fetched object responses
   * @throws {ValidationError} If required parameters are missing or invalid
   */
  async streamFetchObjects(shardId, items, options = {}) {
    this.#validateShardId(shardId);
    nonEmptyArray(items, 'items');
    this.#validateFetchItems(items);

    const path = `${ObjectTransferClient.API_PREFIX}/shards/${shardId}/objects/batch`;
    const body = {
      items: items.map(item => ({
        objectRef: item.objectRef,
        checksum: item.checksum ?? null,
      })),
    };

    if (options.requestId) {
      body.requestId = options.requestId;
    }

    const response = await this.#transport.post(path, body);

    return this.#mapStreamFetchResponse(response, items);
  }

  // ==========================================================================
  // Private Helper Methods
  // ==========================================================================

  /**
   * Validate shard ID.
   * @param {number} shardId - Shard ID to validate
   * @throws {ValidationError} If shardId is invalid
   * @private
   */
  #validateShardId(shardId) {
    required(shardId, 'shardId');
    if (typeof shardId !== 'number' || !Number.isInteger(shardId) || shardId < 0) {
      throw new ValidationError('shardId must be a non-negative integer', 'shardId');
    }
  }

  /**
   * Validate fetch items array.
   * @param {Array<FetchItem>} items - Items to validate
   * @throws {ValidationError} If items are invalid
   * @private
   */
  #validateFetchItems(items) {
    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      if (!item || typeof item !== 'object') {
        throw new ValidationError(`items[${i}] must be an object`, `items[${i}]`);
      }
      if (!item.objectRef || typeof item.objectRef !== 'string' || item.objectRef.trim() === '') {
        throw new ValidationError(`items[${i}].objectRef is required and must be a non-empty string`, `items[${i}].objectRef`);
      }
      if (item.checksum !== undefined && item.checksum !== null && typeof item.checksum !== 'string') {
        throw new ValidationError(`items[${i}].checksum must be a string`, `items[${i}].checksum`);
      }
    }
  }

  /**
   * Map a single fetch object response.
   * @param {Object} response - Raw response from transport
   * @param {string} objectRef - Original object reference
   * @returns {FetchObjectResponse} Mapped response
   * @private
   */
  #mapFetchObjectResponse(response, objectRef) {
    return {
      objectRef: response?.objectRef ?? objectRef,
      data: response?.data ?? '',
      checksum: response?.checksum ?? '',
      size: response?.size ?? 0,
      compressed: response?.compressed ?? false,
      compressionAlgo: response?.compressionAlgo ?? '',
      found: response?.found ?? false,
      errorMessage: response?.errorMessage ?? '',
    };
  }

  /**
   * Map stream fetch response.
   * @param {Object} response - Raw response from transport
   * @param {Array<FetchItem>} items - Original fetch items
   * @returns {Array<FetchObjectResponse>} Mapped responses
   * @private
   */
  #mapStreamFetchResponse(response, items) {
    if (!response || !response.results || !Array.isArray(response.results)) {
      // Return default responses for all items if no results
      return items.map(item => this.#mapFetchObjectResponse(null, item.objectRef));
    }

    return response.results.map((result, index) => {
      const originalObjectRef = items[index]?.objectRef ?? '';
      return this.#mapFetchObjectResponse(result, originalObjectRef);
    });
  }
}

export default ObjectTransferClient;
