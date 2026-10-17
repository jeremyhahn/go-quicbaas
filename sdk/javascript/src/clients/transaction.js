/**
 * QuicBaaS JavaScript SDK - Transaction Client
 *
 * Provides compare-and-swap transaction operations for atomic multi-key updates.
 *
 * @module @quicbaas/client/clients/transaction
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  requiredObject,
  nonNegativeInteger,
} from '../utils/validation.js';

/**
 * Compare target types for transaction conditions.
 * @readonly
 * @enum {string}
 */
export const CompareTarget = Object.freeze({
  VERSION: 'VERSION',
  VALUE: 'VALUE',
  MOD_REVISION: 'MOD_REVISION',
  CREATE_REVISION: 'CREATE_REVISION',
});

/**
 * Compare result operators for transaction conditions.
 * @readonly
 * @enum {string}
 */
export const CompareResult = Object.freeze({
  EQUAL: 'EQUAL',
  NOT_EQUAL: 'NOT_EQUAL',
  LESS: 'LESS',
  GREATER: 'GREATER',
});

/**
 * Transaction operation types.
 * @readonly
 * @enum {string}
 */
export const TxnOpType = Object.freeze({
  PUT: 'PUT',
  GET: 'GET',
  DELETE: 'DELETE',
});

/**
 * @typedef {Object} Compare
 * @property {string} key - The key to compare
 * @property {CompareTarget} target - What to compare (VERSION, VALUE, MOD_REVISION, CREATE_REVISION)
 * @property {CompareResult} result - Comparison operator (EQUAL, NOT_EQUAL, LESS, GREATER)
 * @property {*} [value] - Value for VALUE comparison
 * @property {number} [revision] - Revision number for revision comparisons
 */

/**
 * @typedef {Object} TxnOp
 * @property {TxnOpType} type - Operation type (PUT, GET, DELETE)
 * @property {string} entityType - Entity type for the operation
 * @property {string} key - Key for the operation
 * @property {*} [value] - Value for PUT operations
 */

/**
 * @typedef {Object} TxnRequest
 * @property {Compare[]} [conditions] - Array of conditions to check
 * @property {TxnOp[]} [success] - Operations to execute if all conditions pass
 * @property {TxnOp[]} [failure] - Operations to execute if any condition fails
 */

/**
 * @typedef {Object} TxnOpResponse
 * @property {TxnOpType} type - Operation type that was executed
 * @property {*} [value] - Value returned from GET operations
 */

/**
 * @typedef {Object} TxnResponse
 * @property {boolean} succeeded - Whether all conditions passed
 * @property {number} revision - Revision number after transaction
 * @property {TxnOpResponse[]} responses - Responses from executed operations
 */

/**
 * Client for QuicBaaS transaction operations.
 *
 * Provides atomic compare-and-swap transactions that allow multiple operations
 * to be executed conditionally based on the current state of keys.
 *
 * @example
 * const transport = new HttpTransport('http://localhost:8080');
 * const txnClient = new TransactionClient(transport);
 *
 * // Create a conditional transaction
 * const result = await txnClient.transaction({
 *   conditions: [
 *     txnClient.compare('users', 'user:123', 5)
 *   ],
 *   success: [
 *     txnClient.putOp('users', 'user:123', { name: 'Updated Name' })
 *   ],
 *   failure: [
 *     txnClient.getOp('users', 'user:123')
 *   ]
 * });
 */
class TransactionClient {
  /**
   * API endpoint for transaction operations.
   * @private
   * @type {string}
   */
  static API_PATH = '/api/v1/transaction';

  /**
   * Create a new TransactionClient.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.transport = transport;
  }

  /**
   * Execute an atomic compare-and-swap transaction.
   *
   * The transaction evaluates all conditions. If all conditions pass,
   * the success operations are executed atomically. If any condition fails,
   * the failure operations are executed instead.
   *
   * @param {TxnRequest} request - Transaction request
   * @param {Compare[]} [request.conditions] - Conditions to evaluate
   * @param {TxnOp[]} [request.success] - Operations if conditions pass
   * @param {TxnOp[]} [request.failure] - Operations if conditions fail
   * @returns {Promise<TxnResponse>} Transaction result
   * @throws {ValidationError} If request is invalid
   *
   * @example
   * // Optimistic locking: update only if revision matches
   * const result = await client.transaction({
   *   conditions: [
   *     client.compare('users', 'user:123', 5)
   *   ],
   *   success: [
   *     client.putOp('users', 'user:123', { name: 'New Name', email: 'new@example.com' })
   *   ]
   * });
   *
   * if (result.succeeded) {
   *   console.log('Update successful at revision:', result.revision);
   * } else {
   *   console.log('Conflict detected, please retry');
   * }
   */
  async transaction(request) {
    this._validateRequest(request);

    const payload = this._buildPayload(request);
    const response = await this.transport.post(TransactionClient.API_PATH, payload);

    return this._parseResponse(response);
  }

  /**
   * Create a compare condition for revision checking.
   *
   * Creates a condition that checks if the key's modification revision
   * equals the specified value. Use revision 0 to check that a key does not exist.
   *
   * @param {string} entityType - Entity type (e.g., 'users', 'config')
   * @param {string} key - Key to check
   * @param {number} revision - Expected revision (0 means key should not exist)
   * @returns {Compare} Compare condition object
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * // Check that key has specific revision
   * const condition = client.compare('users', 'user:123', 5);
   *
   * // Check that key does not exist
   * const notExists = client.compare('users', 'user:new', 0);
   */
  compare(entityType, key, revision) {
    requiredString(entityType, 'entityType');
    requiredString(key, 'key');
    nonNegativeInteger(revision, 'revision');

    if (revision === undefined || revision === null) {
      throw new ValidationError('revision is required', 'revision');
    }

    return {
      entityType,
      key,
      target: CompareTarget.MOD_REVISION,
      result: CompareResult.EQUAL,
      revision,
    };
  }

  /**
   * Create an advanced compare condition with custom target and operator.
   *
   * @param {Object} options - Compare options
   * @param {string} options.entityType - Entity type
   * @param {string} options.key - Key to compare
   * @param {CompareTarget} options.target - What to compare
   * @param {CompareResult} options.result - Comparison operator
   * @param {*} [options.value] - Value for VALUE comparison
   * @param {number} [options.revision] - Revision for revision comparisons
   * @returns {Compare} Compare condition object
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * // Check if value equals specific content
   * const valueCheck = client.compareAdvanced({
   *   entityType: 'config',
   *   key: 'settings',
   *   target: CompareTarget.VALUE,
   *   result: CompareResult.EQUAL,
   *   value: { enabled: true }
   * });
   *
   * // Check if version is greater than 0 (key exists)
   * const existsCheck = client.compareAdvanced({
   *   entityType: 'users',
   *   key: 'user:123',
   *   target: CompareTarget.VERSION,
   *   result: CompareResult.GREATER,
   *   revision: 0
   * });
   */
  compareAdvanced(options) {
    requiredObject(options, 'options');
    requiredString(options.entityType, 'options.entityType');
    requiredString(options.key, 'options.key');
    required(options.target, 'options.target');
    required(options.result, 'options.result');

    this._validateCompareTarget(options.target);
    this._validateCompareResult(options.result);

    if (options.target === CompareTarget.VALUE && options.value === undefined) {
      throw new ValidationError('value is required for VALUE comparison', 'value');
    }

    if (
      (options.target === CompareTarget.VERSION ||
        options.target === CompareTarget.MOD_REVISION ||
        options.target === CompareTarget.CREATE_REVISION) &&
      options.revision === undefined
    ) {
      throw new ValidationError('revision is required for revision comparisons', 'revision');
    }

    if (options.revision !== undefined) {
      nonNegativeInteger(options.revision, 'revision');
    }

    return {
      entityType: options.entityType,
      key: options.key,
      target: options.target,
      result: options.result,
      value: options.value,
      revision: options.revision,
    };
  }

  /**
   * Create a PUT operation for use in transactions.
   *
   * @param {string} entityType - Entity type
   * @param {string} key - Key to write
   * @param {*} value - Value to store
   * @returns {TxnOp} Transaction operation object
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * const putOp = client.putOp('users', 'user:123', {
   *   name: 'John Doe',
   *   email: 'john@example.com'
   * });
   */
  putOp(entityType, key, value) {
    requiredString(entityType, 'entityType');
    requiredString(key, 'key');
    required(value, 'value');

    return {
      type: TxnOpType.PUT,
      entityType,
      key,
      value,
    };
  }

  /**
   * Create a GET operation for use in transactions.
   *
   * @param {string} entityType - Entity type
   * @param {string} key - Key to read
   * @returns {TxnOp} Transaction operation object
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * const getOp = client.getOp('users', 'user:123');
   */
  getOp(entityType, key) {
    requiredString(entityType, 'entityType');
    requiredString(key, 'key');

    return {
      type: TxnOpType.GET,
      entityType,
      key,
    };
  }

  /**
   * Create a DELETE operation for use in transactions.
   *
   * @param {string} entityType - Entity type
   * @param {string} key - Key to delete
   * @returns {TxnOp} Transaction operation object
   * @throws {ValidationError} If parameters are invalid
   *
   * @example
   * const deleteOp = client.deleteOp('users', 'user:123');
   */
  deleteOp(entityType, key) {
    requiredString(entityType, 'entityType');
    requiredString(key, 'key');

    return {
      type: TxnOpType.DELETE,
      entityType,
      key,
    };
  }

  /**
   * Validate the transaction request.
   * @private
   * @param {TxnRequest} request - Request to validate
   * @throws {ValidationError} If validation fails
   */
  _validateRequest(request) {
    requiredObject(request, 'request');

    const conditions = request.conditions ?? [];
    const success = request.success ?? [];
    const failure = request.failure ?? [];

    if (!Array.isArray(conditions)) {
      throw new ValidationError('conditions must be an array', 'conditions');
    }
    if (!Array.isArray(success)) {
      throw new ValidationError('success must be an array', 'success');
    }
    if (!Array.isArray(failure)) {
      throw new ValidationError('failure must be an array', 'failure');
    }

    if (conditions.length === 0 && success.length === 0 && failure.length === 0) {
      throw new ValidationError(
        'transaction must have at least one condition or operation',
        'request',
      );
    }

    conditions.forEach((condition, index) => {
      this._validateCondition(condition, index);
    });

    success.forEach((op, index) => {
      this._validateOperation(op, 'success', index);
    });

    failure.forEach((op, index) => {
      this._validateOperation(op, 'failure', index);
    });
  }

  /**
   * Validate a compare condition.
   * @private
   * @param {Compare} condition - Condition to validate
   * @param {number} index - Index in conditions array
   * @throws {ValidationError} If validation fails
   */
  _validateCondition(condition, index) {
    if (!condition || typeof condition !== 'object') {
      throw new ValidationError(
        `condition at index ${index} must be an object`,
        `conditions[${index}]`,
      );
    }

    if (!condition.key || typeof condition.key !== 'string' || condition.key.trim() === '') {
      throw new ValidationError(
        `condition at index ${index} requires a key`,
        `conditions[${index}].key`,
      );
    }

    if (!condition.target) {
      throw new ValidationError(
        `condition at index ${index} requires a target`,
        `conditions[${index}].target`,
      );
    }

    this._validateCompareTarget(condition.target, index);

    if (!condition.result) {
      throw new ValidationError(
        `condition at index ${index} requires a result operator`,
        `conditions[${index}].result`,
      );
    }

    this._validateCompareResult(condition.result, index);
  }

  /**
   * Validate a compare target.
   * @private
   * @param {string} target - Target to validate
   * @param {number} [index] - Optional index for error context
   * @throws {ValidationError} If target is invalid
   */
  _validateCompareTarget(target, index) {
    const validTargets = Object.values(CompareTarget);
    if (!validTargets.includes(target)) {
      const field = index !== undefined ? `conditions[${index}].target` : 'target';
      throw new ValidationError(
        `invalid compare target: ${target}. Must be one of: ${validTargets.join(', ')}`,
        field,
      );
    }
  }

  /**
   * Validate a compare result operator.
   * @private
   * @param {string} result - Result operator to validate
   * @param {number} [index] - Optional index for error context
   * @throws {ValidationError} If result is invalid
   */
  _validateCompareResult(result, index) {
    const validResults = Object.values(CompareResult);
    if (!validResults.includes(result)) {
      const field = index !== undefined ? `conditions[${index}].result` : 'result';
      throw new ValidationError(
        `invalid compare result: ${result}. Must be one of: ${validResults.join(', ')}`,
        field,
      );
    }
  }

  /**
   * Validate a transaction operation.
   * @private
   * @param {TxnOp} op - Operation to validate
   * @param {string} branch - Branch name ('success' or 'failure')
   * @param {number} index - Index in operations array
   * @throws {ValidationError} If validation fails
   */
  _validateOperation(op, branch, index) {
    if (!op || typeof op !== 'object') {
      throw new ValidationError(
        `${branch} operation at index ${index} must be an object`,
        `${branch}[${index}]`,
      );
    }

    if (!op.entityType || typeof op.entityType !== 'string' || op.entityType.trim() === '') {
      throw new ValidationError(
        `${branch} operation at index ${index} requires an entityType`,
        `${branch}[${index}].entityType`,
      );
    }

    if (!op.key || typeof op.key !== 'string' || op.key.trim() === '') {
      throw new ValidationError(
        `${branch} operation at index ${index} requires a key`,
        `${branch}[${index}].key`,
      );
    }

    if (!op.type) {
      throw new ValidationError(
        `${branch} operation at index ${index} requires a type`,
        `${branch}[${index}].type`,
      );
    }

    const validTypes = Object.values(TxnOpType);
    if (!validTypes.includes(op.type)) {
      throw new ValidationError(
        `invalid operation type at ${branch}[${index}]: ${op.type}. Must be one of: ${validTypes.join(', ')}`,
        `${branch}[${index}].type`,
      );
    }

    if (op.type === TxnOpType.PUT && op.value === undefined) {
      throw new ValidationError(
        `${branch} PUT operation at index ${index} requires a value`,
        `${branch}[${index}].value`,
      );
    }
  }

  /**
   * Build the request payload for the API.
   * @private
   * @param {TxnRequest} request - Transaction request
   * @returns {Object} API payload
   */
  _buildPayload(request) {
    const payload = {
      compare: (request.conditions ?? []).map((condition) => this._serializeCondition(condition)),
      success: (request.success ?? []).map((op) => this._serializeOperation(op)),
      failure: (request.failure ?? []).map((op) => this._serializeOperation(op)),
    };

    return payload;
  }

  /**
   * Serialize a condition for the API.
   * @private
   * @param {Compare} condition - Condition to serialize
   * @returns {Object} Serialized condition
   */
  _serializeCondition(condition) {
    const serialized = {
      key: condition.entityType ? `${condition.entityType}:${condition.key}` : condition.key,
      target: condition.target,
      result: condition.result,
    };

    if (condition.value !== undefined) {
      serialized.value = this._encodeValue(condition.value);
    }

    if (condition.revision !== undefined) {
      serialized.revision = condition.revision;
    }

    return serialized;
  }

  /**
   * Serialize an operation for the API.
   * @private
   * @param {TxnOp} op - Operation to serialize
   * @returns {Object} Serialized operation
   */
  _serializeOperation(op) {
    const serialized = {
      type: op.type,
      entityType: op.entityType,
      key: op.key,
    };

    if (op.value !== undefined) {
      serialized.value = this._encodeValue(op.value);
    }

    return serialized;
  }

  /**
   * Encode a value for transmission.
   * @private
   * @param {*} value - Value to encode
   * @returns {string} Base64 encoded value
   */
  _encodeValue(value) {
    if (value instanceof Uint8Array) {
      return this._uint8ArrayToBase64(value);
    }

    if (typeof value === 'string') {
      return btoa(value);
    }

    const jsonString = JSON.stringify(value);
    return btoa(jsonString);
  }

  /**
   * Convert Uint8Array to base64 string.
   * @private
   * @param {Uint8Array} bytes - Bytes to convert
   * @returns {string} Base64 string
   */
  _uint8ArrayToBase64(bytes) {
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
  }

  /**
   * Parse the API response.
   * @private
   * @param {Object} response - API response
   * @returns {TxnResponse} Parsed response
   */
  _parseResponse(response) {
    if (!response) {
      return {
        succeeded: false,
        revision: 0,
        responses: [],
      };
    }

    return {
      succeeded: Boolean(response.succeeded),
      revision: response.revision ?? 0,
      responses: (response.responses ?? []).map((opResponse) => this._parseOpResponse(opResponse)),
    };
  }

  /**
   * Parse an operation response.
   * @private
   * @param {Object} opResponse - Operation response from API
   * @returns {TxnOpResponse} Parsed operation response
   */
  _parseOpResponse(opResponse) {
    const parsed = {
      type: opResponse.type,
    };

    if (opResponse.value !== undefined && opResponse.value !== null) {
      parsed.value = this._decodeValue(opResponse.value);
    }

    return parsed;
  }

  /**
   * Decode a value from the API response.
   * @private
   * @param {string} encoded - Base64 encoded value
   * @returns {*} Decoded value
   */
  _decodeValue(encoded) {
    if (typeof encoded !== 'string') {
      return encoded;
    }

    try {
      const decoded = atob(encoded);
      try {
        return JSON.parse(decoded);
      } catch {
        return decoded;
      }
    } catch {
      return encoded;
    }
  }
}

export default TransactionClient;
export { TransactionClient };
