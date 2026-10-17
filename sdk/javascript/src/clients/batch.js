/**
 * QuicBaaS JavaScript SDK - Batch Client
 *
 * Provides atomic batch operations for multiple key-value operations.
 *
 * @module @quicbaas/client/clients/batch
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  nonEmptyArray,
  nonNegativeInteger,
} from '../utils/validation.js';

/**
 * @typedef {Object} BatchPutItem
 * @property {string} entityType - The entity type for the key-value pair
 * @property {string} key - The key to store
 * @property {*} value - The value to store (will be JSON serialized)
 * @property {number} [ttl] - Optional time-to-live in seconds
 */

/**
 * @typedef {Object} BatchGetItem
 * @property {string} entityType - The entity type to retrieve from
 * @property {string} key - The key to retrieve
 */

/**
 * @typedef {Object} BatchDeleteItem
 * @property {string} entityType - The entity type to delete from
 * @property {string} key - The key to delete
 */

/**
 * @typedef {Object} BatchResult
 * @property {boolean} success - Whether the operation succeeded
 * @property {string} [error] - Error message if operation failed
 * @property {*} [value] - Retrieved value for get operations
 */

/**
 * @typedef {Object} BatchResponse
 * @property {number} revision - The revision number after the operation
 * @property {BatchResult[]} results - Results for each operation
 */

/**
 * @typedef {Object} BatchGetResponse
 * @property {BatchResult[]} results - Results containing retrieved values
 */

/**
 * API base path for batch operations.
 * @constant {string}
 */
const API_BASE_PATH = '/api/v1/batch';

/**
 * Operation types for batch requests.
 * @constant {Object}
 */
const OpType = {
  PUT: 'PUT',
  GET: 'GET',
  DELETE: 'DELETE',
};

/**
 * BatchClient provides atomic batch operations for QuicBaaS.
 *
 * Batch operations allow multiple key-value operations to be executed
 * atomically in a single request, improving performance and ensuring
 * consistency across related operations.
 *
 * @example
 * const transport = new HttpTransport('http://localhost:8080');
 * const batchClient = new BatchClient(transport);
 *
 * // Batch put multiple items
 * const putResult = await batchClient.batchPut([
 *   { entityType: 'users', key: 'user1', value: { name: 'Alice' } },
 *   { entityType: 'users', key: 'user2', value: { name: 'Bob' }, ttl: 3600 }
 * ]);
 *
 * // Batch get multiple items
 * const getResult = await batchClient.batchGet([
 *   { entityType: 'users', key: 'user1' },
 *   { entityType: 'users', key: 'user2' }
 * ]);
 *
 * // Batch delete multiple items
 * const deleteResult = await batchClient.batchDelete([
 *   { entityType: 'users', key: 'user1' },
 *   { entityType: 'users', key: 'user2' }
 * ]);
 */
class BatchClient {
  /**
   * Create a new BatchClient instance.
   *
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} When transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.transport = transport;
  }

  /**
   * Execute multiple PUT operations atomically.
   *
   * All items are stored atomically - either all succeed or all fail.
   * This is useful for maintaining consistency when storing related data.
   *
   * @param {BatchPutItem[]} items - Array of items to put
   * @returns {Promise<BatchResponse>} Response containing revision and results
   * @throws {ValidationError} When items validation fails
   *
   * @example
   * const result = await batchClient.batchPut([
   *   { entityType: 'config', key: 'setting1', value: 'value1' },
   *   { entityType: 'config', key: 'setting2', value: 'value2', ttl: 7200 }
   * ]);
   * console.log(`Revision: ${result.revision}`);
   */
  async batchPut(items) {
    this._validatePutItems(items);

    const operations = items.map(item => ({
      type: OpType.PUT,
      entityType: item.entityType,
      key: item.key,
      value: item.value,
      ttl: item.ttl,
    }));

    return this._executeOperation('put', operations);
  }

  /**
   * Execute multiple GET operations in a single request.
   *
   * Retrieves multiple values efficiently in one network round-trip.
   * Each result will contain the value if found, or an error if not.
   *
   * @param {BatchGetItem[]} items - Array of items to get
   * @returns {Promise<BatchGetResponse>} Response containing results with values
   * @throws {ValidationError} When items validation fails
   *
   * @example
   * const result = await batchClient.batchGet([
   *   { entityType: 'users', key: 'user1' },
   *   { entityType: 'users', key: 'user2' }
   * ]);
   * result.results.forEach((r, i) => {
   *   if (r.success) {
   *     console.log(`Value: ${JSON.stringify(r.value)}`);
   *   }
   * });
   */
  async batchGet(items) {
    this._validateGetDeleteItems(items, 'batchGet');

    const operations = items.map(item => ({
      type: OpType.GET,
      entityType: item.entityType,
      key: item.key,
    }));

    return this._executeOperation('get', operations);
  }

  /**
   * Execute multiple DELETE operations atomically.
   *
   * All items are deleted atomically - either all succeed or all fail.
   * This is useful for maintaining consistency when removing related data.
   *
   * @param {BatchDeleteItem[]} items - Array of items to delete
   * @returns {Promise<BatchResponse>} Response containing revision and results
   * @throws {ValidationError} When items validation fails
   *
   * @example
   * const result = await batchClient.batchDelete([
   *   { entityType: 'sessions', key: 'session1' },
   *   { entityType: 'sessions', key: 'session2' }
   * ]);
   * console.log(`Deleted with revision: ${result.revision}`);
   */
  async batchDelete(items) {
    this._validateGetDeleteItems(items, 'batchDelete');

    const operations = items.map(item => ({
      type: OpType.DELETE,
      entityType: item.entityType,
      key: item.key,
    }));

    return this._executeOperation('delete', operations);
  }

  /**
   * Execute a batch operation against the API.
   *
   * @param {string} operation - Operation name (put, get, delete)
   * @param {Object[]} operations - Array of operation objects
   * @returns {Promise<Object>} API response
   * @private
   */
  async _executeOperation(operation, operations) {
    const path = `${API_BASE_PATH}/${operation}`;
    const body = { operations };

    return this.transport.post(path, body);
  }

  /**
   * Validate batch PUT items.
   *
   * @param {BatchPutItem[]} items - Items to validate
   * @throws {ValidationError} When validation fails
   * @private
   */
  _validatePutItems(items) {
    nonEmptyArray(items, 'items');

    items.forEach((item, index) => {
      this._validateItemBase(item, index, 'batchPut');

      // Value is required for PUT operations
      required(item.value, `items[${index}].value`);

      // TTL must be a non-negative integer if provided
      if (item.ttl !== undefined && item.ttl !== null) {
        nonNegativeInteger(item.ttl, `items[${index}].ttl`);
      }
    });
  }

  /**
   * Validate batch GET or DELETE items.
   *
   * @param {BatchGetItem[]|BatchDeleteItem[]} items - Items to validate
   * @param {string} operationName - Name of the operation for error messages
   * @throws {ValidationError} When validation fails
   * @private
   */
  _validateGetDeleteItems(items, operationName) {
    nonEmptyArray(items, 'items');

    items.forEach((item, index) => {
      this._validateItemBase(item, index, operationName);
    });
  }

  /**
   * Validate base item properties (entityType and key).
   *
   * @param {Object} item - Item to validate
   * @param {number} index - Item index for error messages
   * @param {string} _operationName - Operation name for error context (unused)
   * @throws {ValidationError} When validation fails
   * @private
   */
  _validateItemBase(item, index, _operationName) {
    if (!item || typeof item !== 'object' || Array.isArray(item)) {
      throw new ValidationError(
        `items[${index}] must be an object`,
        `items[${index}]`,
      );
    }

    requiredString(item.entityType, `items[${index}].entityType`);
    requiredString(item.key, `items[${index}].key`);
  }
}

export default BatchClient;
