/**
 * QuicBaaS JavaScript SDK - Query Client
 *
 * Provides secondary index query operations for QuicBaaS.
 *
 * @module @quicbaas/client/clients/query
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  requiredObject,
  requiredArray,
  nonNegativeInteger,
  validateRequest,
} from '../utils/validation.js';

/**
 * Query comparison operators.
 * @readonly
 * @enum {string}
 */
export const Operator = {
  EQUAL: 'EQUAL',
  NOT_EQUAL: 'NOT_EQUAL',
  LESS_THAN: 'LESS_THAN',
  LESS_THAN_OR_EQUAL: 'LESS_THAN_OR_EQUAL',
  GREATER_THAN: 'GREATER_THAN',
  GREATER_THAN_OR_EQUAL: 'GREATER_THAN_OR_EQUAL',
  PREFIX: 'PREFIX',
  RANGE: 'RANGE',
};

/**
 * Valid operator values for validation.
 * @type {string[]}
 */
const VALID_OPERATORS = Object.values(Operator);

/**
 * @typedef {Object} QueryCondition
 * @property {string} field - Field name to query
 * @property {string} operator - Comparison operator
 * @property {*} value - Value to compare against
 * @property {*} [maxValue] - Maximum value for range queries
 */

/**
 * @typedef {Object} QueryRequest
 * @property {string} index - Index name to query
 * @property {string} entityType - Entity type to query
 * @property {QueryCondition[]} conditions - Query conditions
 * @property {number} [limit] - Maximum results to return
 * @property {number} [offset] - Number of results to skip
 * @property {string} [cursor] - Pagination cursor
 */

/**
 * @typedef {Object} ScanIndexRequest
 * @property {string} index - Index name to scan
 * @property {string} [prefix] - Optional prefix filter
 * @property {number} [limit] - Maximum results to return
 * @property {number} [offset] - Number of results to skip
 * @property {string} [cursor] - Pagination cursor
 */

/**
 * @typedef {Object} QueryResult
 * @property {string} key - Result key
 * @property {*} value - Result value
 */

/**
 * @typedef {Object} QueryResponse
 * @property {QueryResult[]} results - Query results
 * @property {string} [nextCursor] - Cursor for next page
 * @property {boolean} hasMore - Whether more results exist
 */

/**
 * Create an equals condition.
 * @param {string} field - Field name
 * @param {*} value - Value to match
 * @returns {QueryCondition}
 */
export function eq(field, value) {
  requiredString(field, 'field');
  required(value, 'value');
  return {
    field,
    operator: Operator.EQUAL,
    value,
  };
}

/**
 * Create a not-equals condition.
 * @param {string} field - Field name
 * @param {*} value - Value to not match
 * @returns {QueryCondition}
 */
export function neq(field, value) {
  requiredString(field, 'field');
  required(value, 'value');
  return {
    field,
    operator: Operator.NOT_EQUAL,
    value,
  };
}

/**
 * Create a greater-than condition.
 * @param {string} field - Field name
 * @param {*} value - Value to compare
 * @returns {QueryCondition}
 */
export function gt(field, value) {
  requiredString(field, 'field');
  required(value, 'value');
  return {
    field,
    operator: Operator.GREATER_THAN,
    value,
  };
}

/**
 * Create a greater-than-or-equal condition.
 * @param {string} field - Field name
 * @param {*} value - Value to compare
 * @returns {QueryCondition}
 */
export function gte(field, value) {
  requiredString(field, 'field');
  required(value, 'value');
  return {
    field,
    operator: Operator.GREATER_THAN_OR_EQUAL,
    value,
  };
}

/**
 * Create a less-than condition.
 * @param {string} field - Field name
 * @param {*} value - Value to compare
 * @returns {QueryCondition}
 */
export function lt(field, value) {
  requiredString(field, 'field');
  required(value, 'value');
  return {
    field,
    operator: Operator.LESS_THAN,
    value,
  };
}

/**
 * Create a less-than-or-equal condition.
 * @param {string} field - Field name
 * @param {*} value - Value to compare
 * @returns {QueryCondition}
 */
export function lte(field, value) {
  requiredString(field, 'field');
  required(value, 'value');
  return {
    field,
    operator: Operator.LESS_THAN_OR_EQUAL,
    value,
  };
}

/**
 * Create a range condition (between min and max, inclusive).
 * @param {string} field - Field name
 * @param {*} min - Minimum value
 * @param {*} max - Maximum value
 * @returns {QueryCondition}
 */
export function between(field, min, max) {
  requiredString(field, 'field');
  required(min, 'min');
  required(max, 'max');
  return {
    field,
    operator: Operator.RANGE,
    value: min,
    maxValue: max,
  };
}

/**
 * Create a prefix condition.
 * @param {string} field - Field name
 * @param {string} prefix - Prefix to match
 * @returns {QueryCondition}
 */
export function prefix(field, prefix) {
  requiredString(field, 'field');
  requiredString(prefix, 'prefix');
  return {
    field,
    operator: Operator.PREFIX,
    value: prefix,
  };
}

/**
 * Query Client for QuicBaaS secondary index operations.
 *
 * Provides methods to execute index-based queries and scans
 * against QuicBaaS's secondary index system.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import QueryClient, { eq, between } from '@quicbaas/client/clients/query';
 *
 * const transport = new HttpTransport('http://localhost:8080');
 * const queryClient = new QueryClient(transport);
 *
 * // Execute a query with conditions
 * const results = await queryClient.query({
 *   index: 'users_by_age',
 *   entityType: 'user',
 *   conditions: [between('age', 18, 65)],
 *   limit: 100,
 * });
 *
 * // Scan an entire index
 * const allUsers = await queryClient.scanIndex({
 *   index: 'users_by_name',
 *   prefix: 'John',
 *   limit: 50,
 * });
 */
export class QueryClient {
  /**
   * API endpoint paths.
   * @private
   * @readonly
   */
  static ENDPOINTS = {
    QUERY: '/api/v1/query',
    SCAN: '/api/v1/query/scan',
  };

  /**
   * Create a new QueryClient instance.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    if (!transport) {
      throw new ValidationError('transport is required', 'transport');
    }
    this.transport = transport;
  }

  /**
   * Execute an index-based query with conditions.
   *
   * Queries the specified secondary index using the provided conditions.
   * Results are returned as an array with pagination support.
   *
   * @param {QueryRequest} request - Query request parameters
   * @param {string} request.index - Name of the secondary index to query
   * @param {string} request.entityType - Type of entity to query
   * @param {QueryCondition[]} request.conditions - Array of query conditions
   * @param {number} [request.limit] - Maximum number of results to return
   * @param {number} [request.offset] - Number of results to skip
   * @param {string} [request.cursor] - Pagination cursor from previous response
   * @returns {Promise<QueryResponse>} Query results with pagination info
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const response = await queryClient.query({
   *   index: 'users_by_status',
   *   entityType: 'user',
   *   conditions: [eq('status', 'active')],
   *   limit: 50,
   * });
   *
   * console.log(response.results); // Array of matching results
   * console.log(response.hasMore); // true if more results exist
   * console.log(response.nextCursor); // cursor for next page
   */
  async query(request) {
    this._validateQueryRequest(request);

    const body = this._buildQueryRequestBody(request);
    const response = await this.transport.post(QueryClient.ENDPOINTS.QUERY, body);

    return this._normalizeQueryResponse(response);
  }

  /**
   * Scan an index with optional prefix filtering.
   *
   * Performs a full or partial scan of a secondary index.
   * Optionally filters results by key prefix.
   *
   * @param {ScanIndexRequest} request - Scan request parameters
   * @param {string} request.index - Name of the secondary index to scan
   * @param {string} [request.prefix] - Optional prefix to filter results
   * @param {number} [request.limit] - Maximum number of results to return
   * @param {number} [request.offset] - Number of results to skip
   * @param {string} [request.cursor] - Pagination cursor from previous response
   * @returns {Promise<QueryResponse>} Scan results with pagination info
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const response = await queryClient.scanIndex({
   *   index: 'products_by_category',
   *   prefix: 'electronics',
   *   limit: 100,
   * });
   *
   * console.log(response.results); // Array of matching results
   */
  async scanIndex(request) {
    this._validateScanRequest(request);

    const body = this._buildScanRequestBody(request);
    const response = await this.transport.post(QueryClient.ENDPOINTS.SCAN, body);

    return this._normalizeQueryResponse(response);
  }

  /**
   * Validate a query request.
   * @param {QueryRequest} request - Request to validate
   * @throws {ValidationError} If validation fails
   * @private
   */
  _validateQueryRequest(request) {
    validateRequest(request);
    requiredString(request.index, 'index');
    requiredString(request.entityType, 'entityType');
    requiredArray(request.conditions, 'conditions');

    if (request.conditions.length === 0) {
      throw new ValidationError('conditions cannot be empty', 'conditions');
    }

    for (let i = 0; i < request.conditions.length; i++) {
      this._validateCondition(request.conditions[i], `conditions[${i}]`);
    }

    nonNegativeInteger(request.limit, 'limit');
    nonNegativeInteger(request.offset, 'offset');

    if (request.cursor !== undefined && request.cursor !== null) {
      if (typeof request.cursor !== 'string') {
        throw new ValidationError('cursor must be a string', 'cursor');
      }
    }
  }

  /**
   * Validate a scan request.
   * @param {ScanIndexRequest} request - Request to validate
   * @throws {ValidationError} If validation fails
   * @private
   */
  _validateScanRequest(request) {
    validateRequest(request);
    requiredString(request.index, 'index');

    if (request.prefix !== undefined && request.prefix !== null) {
      if (typeof request.prefix !== 'string') {
        throw new ValidationError('prefix must be a string', 'prefix');
      }
    }

    nonNegativeInteger(request.limit, 'limit');
    nonNegativeInteger(request.offset, 'offset');

    if (request.cursor !== undefined && request.cursor !== null) {
      if (typeof request.cursor !== 'string') {
        throw new ValidationError('cursor must be a string', 'cursor');
      }
    }
  }

  /**
   * Validate a single query condition.
   * @param {QueryCondition} condition - Condition to validate
   * @param {string} path - Path for error messages
   * @throws {ValidationError} If validation fails
   * @private
   */
  _validateCondition(condition, path) {
    requiredObject(condition, path);
    requiredString(condition.field, `${path}.field`);
    requiredString(condition.operator, `${path}.operator`);

    if (!VALID_OPERATORS.includes(condition.operator)) {
      throw new ValidationError(
        `${path}.operator must be one of: ${VALID_OPERATORS.join(', ')}`,
        `${path}.operator`,
      );
    }

    required(condition.value, `${path}.value`);

    if (condition.operator === Operator.RANGE) {
      required(condition.maxValue, `${path}.maxValue`);
    }
  }

  /**
   * Build the request body for a query operation.
   * @param {QueryRequest} request - Query request
   * @returns {Object} Request body
   * @private
   */
  _buildQueryRequestBody(request) {
    const body = {
      index: request.index,
      entityType: request.entityType,
      conditions: request.conditions.map(this._serializeCondition),
    };

    if (request.limit !== undefined && request.limit !== null) {
      body.limit = request.limit;
    }

    if (request.offset !== undefined && request.offset !== null) {
      body.offset = request.offset;
    }

    if (request.cursor) {
      body.cursor = request.cursor;
    }

    return body;
  }

  /**
   * Build the request body for a scan operation.
   * @param {ScanIndexRequest} request - Scan request
   * @returns {Object} Request body
   * @private
   */
  _buildScanRequestBody(request) {
    const body = {
      index: request.index,
    };

    if (request.prefix) {
      body.prefix = request.prefix;
    }

    if (request.limit !== undefined && request.limit !== null) {
      body.limit = request.limit;
    }

    if (request.offset !== undefined && request.offset !== null) {
      body.offset = request.offset;
    }

    if (request.cursor) {
      body.cursor = request.cursor;
    }

    return body;
  }

  /**
   * Serialize a condition for API transmission.
   * @param {QueryCondition} condition - Condition to serialize
   * @returns {Object} Serialized condition
   * @private
   */
  _serializeCondition(condition) {
    const serialized = {
      field: condition.field,
      operator: condition.operator,
      value: condition.value,
    };

    if (condition.maxValue !== undefined) {
      serialized.maxValue = condition.maxValue;
    }

    return serialized;
  }

  /**
   * Normalize API response to QueryResponse format.
   * @param {Object} response - Raw API response
   * @returns {QueryResponse} Normalized response
   * @private
   */
  _normalizeQueryResponse(response) {
    if (!response) {
      return {
        results: [],
        nextCursor: null,
        hasMore: false,
      };
    }

    return {
      results: Array.isArray(response.results) ? response.results : [],
      nextCursor: response.nextCursor || response.next_cursor || null,
      hasMore: Boolean(response.hasMore || response.has_more),
    };
  }
}

export default QueryClient;
