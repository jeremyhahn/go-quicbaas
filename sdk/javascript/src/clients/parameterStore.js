/**
 * QuicBaaS JavaScript SDK - Parameter Store Client
 *
 * Provides parameter store operations for storing, retrieving, and managing
 * hierarchical configuration parameters with support for encryption, TTL,
 * and tag-based search.
 *
 * @module @quicbaas/client/clients/parameterStore
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError, QuicBaaSError } from '../errors/index.js';
import {
  requiredString,
  required,
  requiredObject,
  nonEmptyArray,
  positiveInteger,
  nonNegativeInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * API base path for parameter store endpoints.
 * @constant {string}
 */
const PARAMETERS_API_PATH = '/api/v1/parameters';

/**
 * Parameter type values.
 * @readonly
 * @enum {string}
 */
export const ParameterType = Object.freeze({
  /** Plain text string value */
  STRING: 'string',
  /** Encrypted string value (using KMS) */
  SECURE_STRING: 'secureString',
  /** Comma-separated list of strings */
  STRING_LIST: 'stringList',
});

/**
 * @typedef {Object} Parameter
 * @property {string} path - The parameter path (hierarchical, e.g., /app/config/db/host)
 * @property {string} value - The parameter value (decrypted if requested)
 * @property {string} type - The parameter type (string, secureString, stringList)
 * @property {string} [description] - Optional parameter description
 * @property {Object<string, string>} [tags] - Key-value tags for the parameter
 * @property {boolean} isEncrypted - Whether the value is encrypted
 * @property {string} [kmsKeyId] - KMS key ID used for encryption
 * @property {string} [expiresAt] - ISO timestamp when the parameter expires
 * @property {boolean} auditEnabled - Whether audit logging is enabled
 * @property {string} createdAt - ISO timestamp of creation
 * @property {string} updatedAt - ISO timestamp of last update
 * @property {string} [createdBy] - User who created the parameter
 * @property {string} [updatedBy] - User who last updated the parameter
 */

/**
 * @typedef {Object} GetParameterOptions
 * @property {boolean} [withDecryption=true] - Decrypt the value if encrypted
 */

/**
 * @typedef {Object} GetParameterResponse
 * @property {Parameter} parameter - The retrieved parameter
 * @property {string} timestamp - ISO timestamp when the response was generated
 */

/**
 * @typedef {Object} SetParameterOptions
 * @property {string} [type='string'] - Parameter type (string, secureString, stringList)
 * @property {string} [description] - Optional description
 * @property {Object<string, string>} [tags] - Key-value tags
 * @property {boolean} [overwrite=true] - Allow overwriting existing parameters
 * @property {number} [ttl] - Time-to-live in seconds (0 = no expiration)
 * @property {string} [kmsKeyId] - KMS key ID for encryption (required for secureString)
 * @property {boolean} [auditEnabled] - Enable audit logging for this parameter
 */

/**
 * @typedef {Object} SetParameterResponse
 * @property {string} path - The parameter path
 * @property {boolean} created - Whether a new parameter was created
 * @property {string} timestamp - ISO timestamp when the operation completed
 */

/**
 * @typedef {Object} DeleteParameterResponse
 * @property {string} path - The deleted parameter path
 * @property {boolean} deleted - Whether deletion succeeded
 * @property {string} timestamp - ISO timestamp when deletion occurred
 */

/**
 * @typedef {Object} GetParametersOptions
 * @property {boolean} [withDecryption=true] - Decrypt values if encrypted
 */

/**
 * @typedef {Object} GetParametersResponse
 * @property {Parameter[]} parameters - Successfully retrieved parameters
 * @property {Object<string, string>} errors - Per-path errors for failed retrievals
 * @property {string} timestamp - ISO timestamp when the response was generated
 */

/**
 * @typedef {Object} SetParameterInput
 * @property {string} path - The parameter path (required)
 * @property {string} value - The parameter value (required)
 * @property {string} [type='string'] - Parameter type
 * @property {string} [description] - Optional description
 * @property {Object<string, string>} [tags] - Key-value tags
 * @property {boolean} [overwrite=true] - Allow overwriting
 * @property {number} [ttl] - Time-to-live in seconds
 * @property {string} [kmsKeyId] - KMS key ID for encryption
 * @property {boolean} [auditEnabled] - Enable audit logging
 */

/**
 * @typedef {Object} SetParameterResult
 * @property {string} path - The parameter path
 * @property {boolean} created - Whether a new parameter was created
 * @property {boolean} success - Whether the operation succeeded
 * @property {string} [error] - Error message if failed
 */

/**
 * @typedef {Object} SetParametersResponse
 * @property {SetParameterResult[]} results - Per-path results
 * @property {string} timestamp - ISO timestamp when the operation completed
 */

/**
 * @typedef {Object} DeleteParameterResult
 * @property {string} path - The parameter path
 * @property {boolean} deleted - Whether deletion succeeded
 * @property {string} [error] - Error message if failed
 */

/**
 * @typedef {Object} DeleteParametersResponse
 * @property {DeleteParameterResult[]} results - Per-path deletion results
 * @property {number} deletedCount - Number of parameters successfully deleted
 * @property {string} timestamp - ISO timestamp when the operation completed
 */

/**
 * @typedef {Object} ListParametersOptions
 * @property {boolean} [recursive=true] - Include nested paths
 * @property {boolean} [withDecryption=false] - Decrypt values if encrypted
 * @property {boolean} [withValues=false] - Include parameter values
 * @property {number} [limit=100] - Maximum number of results
 * @property {string} [cursor] - Pagination cursor
 */

/**
 * @typedef {Object} ListParametersResponse
 * @property {Parameter[]} parameters - Listed parameters
 * @property {string} [nextCursor] - Cursor for the next page
 * @property {boolean} hasMore - Whether there are more results
 * @property {number} [totalCount] - Total number of matching parameters
 * @property {string} timestamp - ISO timestamp when the response was generated
 */

/**
 * @typedef {Object} SearchByTagsOptions
 * @property {string} [prefix] - Optional path prefix filter
 * @property {boolean} [withValues=false] - Include parameter values
 * @property {boolean} [withDecryption=false] - Decrypt values if encrypted
 * @property {number} [limit=100] - Maximum number of results
 * @property {string} [cursor] - Pagination cursor
 */

/**
 * @typedef {Object} SearchByTagsResponse
 * @property {Parameter[]} parameters - Matching parameters
 * @property {string} [nextCursor] - Cursor for the next page
 * @property {boolean} hasMore - Whether there are more results
 * @property {string} timestamp - ISO timestamp when the response was generated
 */

/**
 * Regular expression for validating hierarchical parameter paths.
 * Paths must start with / and contain only alphanumeric, hyphens, underscores, and forward slashes.
 * @constant {RegExp}
 */
const PATH_REGEX = /^\/[a-zA-Z0-9\-_\/]+$/;

/**
 * Parameter Store client for QuicBaaS.
 *
 * Provides comprehensive parameter management including storing, retrieving,
 * batch operations, listing by prefix, and searching by tags.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import ParameterStoreClient, { ParameterType } from '@quicbaas/client/clients/parameterStore';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const params = new ParameterStoreClient(transport);
 *
 * // Set a parameter
 * await params.setParameter('/app/config/db/host', 'localhost', {
 *   type: ParameterType.STRING,
 *   description: 'Database hostname',
 *   tags: { environment: 'production' }
 * });
 *
 * // Get a parameter
 * const { parameter } = await params.getParameter('/app/config/db/host');
 * console.log(parameter.value);
 *
 * // List parameters by prefix
 * const { parameters } = await params.listParameters('/app/config/', {
 *   recursive: true,
 *   withValues: true
 * });
 *
 * // Search by tags
 * const results = await params.searchByTags({ environment: 'production' });
 */
class ParameterStoreClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new ParameterStoreClient instance.
   *
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided or invalid
   */
  constructor(transport) {
    if (!transport) {
      throw new ValidationError('transport is required', 'transport');
    }
    if (typeof transport.request !== 'function') {
      throw new ValidationError('transport must have a request method', 'transport');
    }

    this.#transport = transport;
  }

  /**
   * Get a parameter by path.
   *
   * Retrieves a single parameter from the parameter store. By default,
   * encrypted values are decrypted.
   *
   * @param {string} path - The parameter path (e.g., /app/config/db/host)
   * @param {GetParameterOptions} [options={}] - Get options
   * @returns {Promise<GetParameterResponse>} The parameter with metadata
   * @throws {ValidationError} If path is invalid
   * @throws {NotFoundError} If the parameter does not exist
   *
   * @example
   * const { parameter } = await params.getParameter('/app/config/db/host');
   * console.log('Host:', parameter.value);
   * console.log('Type:', parameter.type);
   */
  async getParameter(path, options = {}) {
    this.#validatePath(path);

    const query = {};
    if (options.withDecryption !== undefined) {
      query.with_decryption = options.withDecryption;
    }

    const encodedPath = this.#encodePath(path);
    const response = await this.#transport.get(
      `${PARAMETERS_API_PATH}/${encodedPath}`,
      { query }
    );

    return {
      parameter: this.#transformParameter(response?.parameter ?? response),
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Set a parameter value.
   *
   * Creates a new parameter or updates an existing one. Use the type option
   * to specify string, secureString, or stringList types.
   *
   * @param {string} path - The parameter path (e.g., /app/config/db/host)
   * @param {string} value - The parameter value
   * @param {SetParameterOptions} [options={}] - Set options
   * @returns {Promise<SetParameterResponse>} Response with creation status
   * @throws {ValidationError} If path or value is invalid
   * @throws {ConflictError} If overwrite is false and parameter exists
   *
   * @example
   * // Set a plain string parameter
   * const { created } = await params.setParameter('/app/config/db/host', 'localhost');
   *
   * // Set a secure string with encryption
   * await params.setParameter('/app/secrets/api-key', 'sk_live_xxx', {
   *   type: ParameterType.SECURE_STRING,
   *   kmsKeyId: 'alias/my-key',
   *   description: 'Production API key'
   * });
   *
   * // Set a string list
   * await params.setParameter('/app/config/allowed-origins', 'https://a.com,https://b.com', {
   *   type: ParameterType.STRING_LIST
   * });
   */
  async setParameter(path, value, options = {}) {
    this.#validatePath(path);
    requiredString(value, 'value');
    this.#validateSetOptions(options);

    const encodedPath = this.#encodePath(path);
    const body = {
      value,
    };

    if (options.type !== undefined) {
      body.type = options.type;
    }
    if (options.description !== undefined) {
      body.description = options.description;
    }
    if (options.tags !== undefined) {
      body.tags = options.tags;
    }
    if (options.overwrite !== undefined) {
      body.overwrite = options.overwrite;
    }
    if (options.ttl !== undefined) {
      body.ttl_seconds = options.ttl;
    }
    if (options.kmsKeyId !== undefined) {
      body.kms_key_id = options.kmsKeyId;
    }
    if (options.auditEnabled !== undefined) {
      body.audit_enabled = options.auditEnabled;
    }

    const response = await this.#transport.put(
      `${PARAMETERS_API_PATH}/${encodedPath}`,
      body
    );

    return {
      path: response?.path ?? path,
      created: response?.created ?? false,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Delete a parameter by path.
   *
   * Permanently removes a parameter from the store.
   *
   * @param {string} path - The parameter path to delete
   * @returns {Promise<DeleteParameterResponse>} Response with deletion status
   * @throws {ValidationError} If path is invalid
   * @throws {NotFoundError} If the parameter does not exist
   *
   * @example
   * const { deleted } = await params.deleteParameter('/app/config/deprecated');
   * console.log('Deleted:', deleted);
   */
  async deleteParameter(path) {
    this.#validatePath(path);

    const encodedPath = this.#encodePath(path);
    const response = await this.#transport.delete(
      `${PARAMETERS_API_PATH}/${encodedPath}`
    );

    return {
      path: response?.path ?? path,
      deleted: response?.deleted ?? true,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Get multiple parameters by paths.
   *
   * Retrieves multiple parameters in a single request. Parameters that
   * cannot be retrieved will have their errors reported separately.
   *
   * @param {string[]} paths - Array of parameter paths to retrieve
   * @param {GetParametersOptions} [options={}] - Get options
   * @returns {Promise<GetParametersResponse>} Retrieved parameters and any errors
   * @throws {ValidationError} If paths array is empty or invalid
   *
   * @example
   * const { parameters, errors } = await params.getParameters([
   *   '/app/config/db/host',
   *   '/app/config/db/port',
   *   '/app/config/db/name'
   * ]);
   *
   * parameters.forEach(p => console.log(`${p.path}: ${p.value}`));
   *
   * if (Object.keys(errors).length > 0) {
   *   console.error('Failed to retrieve:', errors);
   * }
   */
  async getParameters(paths, options = {}) {
    nonEmptyArray(paths, 'paths');
    paths.forEach((p, i) => this.#validatePath(p, `paths[${i}]`));

    const query = {};
    if (options.withDecryption !== undefined) {
      query.with_decryption = options.withDecryption;
    }

    const response = await this.#transport.post(
      `${PARAMETERS_API_PATH}/batch/get`,
      { paths },
      { query }
    );

    return {
      parameters: (response?.parameters ?? []).map(p => this.#transformParameter(p)),
      errors: response?.errors ?? {},
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Set multiple parameters atomically.
   *
   * Creates or updates multiple parameters in a single atomic operation.
   * All parameters are validated before any are written.
   *
   * @param {SetParameterInput[]} parameters - Array of parameters to set
   * @returns {Promise<SetParametersResponse>} Results for each parameter
   * @throws {ValidationError} If parameters array is empty or any parameter is invalid
   *
   * @example
   * const { results } = await params.setParameters([
   *   { path: '/app/config/db/host', value: 'localhost' },
   *   { path: '/app/config/db/port', value: '5432' },
   *   {
   *     path: '/app/config/db/password',
   *     value: 'secret123',
   *     type: ParameterType.SECURE_STRING,
   *     kmsKeyId: 'alias/my-key'
   *   }
   * ]);
   *
   * results.forEach(r => {
   *   if (r.success) {
   *     console.log(`${r.path}: ${r.created ? 'created' : 'updated'}`);
   *   } else {
   *     console.error(`${r.path}: ${r.error}`);
   *   }
   * });
   */
  async setParameters(parameters) {
    nonEmptyArray(parameters, 'parameters');
    this.#validateParameterInputs(parameters);

    const body = {
      parameters: parameters.map(p => this.#transformSetParameterInput(p)),
    };

    const response = await this.#transport.post(
      `${PARAMETERS_API_PATH}/batch/set`,
      body
    );

    return {
      results: (response?.results ?? []).map(r => ({
        path: r.path,
        created: r.created ?? false,
        success: r.success ?? true,
        error: r.error ?? null,
      })),
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Delete multiple parameters.
   *
   * Removes multiple parameters in a single request.
   *
   * @param {string[]} paths - Array of parameter paths to delete
   * @returns {Promise<DeleteParametersResponse>} Results for each deletion
   * @throws {ValidationError} If paths array is empty or invalid
   *
   * @example
   * const { results, deletedCount } = await params.deleteParameters([
   *   '/app/config/deprecated/a',
   *   '/app/config/deprecated/b'
   * ]);
   *
   * console.log(`Deleted ${deletedCount} parameters`);
   * results.forEach(r => {
   *   if (!r.deleted) {
   *     console.error(`Failed to delete ${r.path}: ${r.error}`);
   *   }
   * });
   */
  async deleteParameters(paths) {
    nonEmptyArray(paths, 'paths');
    paths.forEach((p, i) => this.#validatePath(p, `paths[${i}]`));

    const response = await this.#transport.post(
      `${PARAMETERS_API_PATH}/batch/delete`,
      { paths }
    );

    return {
      results: (response?.results ?? []).map(r => ({
        path: r.path,
        deleted: r.deleted ?? false,
        error: r.error ?? null,
      })),
      deletedCount: response?.deleted_count ?? response?.deletedCount ?? 0,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * List parameters by path prefix.
   *
   * Returns all parameters matching the given prefix. Use recursive option
   * to include nested paths.
   *
   * @param {string} prefix - Path prefix to filter by (e.g., /app/config/)
   * @param {ListParametersOptions} [options={}] - List options
   * @returns {Promise<ListParametersResponse>} Matching parameters with pagination
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * // List all parameters under /app/config/
   * const { parameters, hasMore, nextCursor } = await params.listParameters('/app/config/', {
   *   recursive: true,
   *   withValues: true,
   *   limit: 50
   * });
   *
   * parameters.forEach(p => console.log(`${p.path}: ${p.value}`));
   *
   * // Fetch next page if available
   * if (hasMore) {
   *   const nextPage = await params.listParameters('/app/config/', {
   *     cursor: nextCursor
   *   });
   * }
   */
  async listParameters(prefix, options = {}) {
    this.#validateListOptions(options);

    const query = {};
    if (prefix) {
      query.prefix = prefix;
    }
    if (options.recursive !== undefined) {
      query.recursive = options.recursive;
    }
    if (options.withDecryption !== undefined) {
      query.with_decryption = options.withDecryption;
    }
    if (options.withValues !== undefined) {
      query.with_values = options.withValues;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor !== undefined) {
      query.cursor = options.cursor;
    }

    const response = await this.#transport.get(PARAMETERS_API_PATH, { query });

    return {
      parameters: (response?.parameters ?? []).map(p => this.#transformParameter(p)),
      nextCursor: response?.next_cursor ?? response?.nextCursor ?? null,
      hasMore: response?.has_more ?? response?.hasMore ?? false,
      totalCount: response?.total_count ?? response?.totalCount ?? null,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Search parameters by tags.
   *
   * Finds all parameters matching the specified tag key-value pairs.
   * Multiple tags are ANDed together.
   *
   * @param {Object<string, string>} tags - Required tag key-value pairs
   * @param {SearchByTagsOptions} [options={}] - Search options
   * @returns {Promise<SearchByTagsResponse>} Matching parameters with pagination
   * @throws {ValidationError} If tags object is empty or options are invalid
   *
   * @example
   * // Find all production database parameters
   * const { parameters } = await params.searchByTags(
   *   { environment: 'production', service: 'database' },
   *   { withValues: true }
   * );
   *
   * // Find with prefix filter
   * const { parameters: appParams } = await params.searchByTags(
   *   { team: 'platform' },
   *   { prefix: '/app/', limit: 100 }
   * );
   */
  async searchByTags(tags, options = {}) {
    requiredObject(tags, 'tags');
    if (Object.keys(tags).length === 0) {
      throw new ValidationError('tags cannot be empty', 'tags');
    }
    this.#validateSearchOptions(options);

    const query = {};
    if (options.prefix !== undefined) {
      query.prefix = options.prefix;
    }
    if (options.withValues !== undefined) {
      query.with_values = options.withValues;
    }
    if (options.withDecryption !== undefined) {
      query.with_decryption = options.withDecryption;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor !== undefined) {
      query.cursor = options.cursor;
    }

    const response = await this.#transport.post(
      `${PARAMETERS_API_PATH}/search/tags`,
      { tags },
      { query }
    );

    return {
      parameters: (response?.parameters ?? []).map(p => this.#transformParameter(p)),
      nextCursor: response?.next_cursor ?? response?.nextCursor ?? null,
      hasMore: response?.has_more ?? response?.hasMore ?? false,
      timestamp: response?.timestamp ?? new Date().toISOString(),
    };
  }

  /**
   * Validate a hierarchical parameter path.
   *
   * @param {string} path - The path to validate
   * @param {string} [name='path'] - Parameter name for error messages
   * @throws {ValidationError} If path is invalid
   * @private
   */
  #validatePath(path, name = 'path') {
    requiredString(path, name);

    if (!path.startsWith('/')) {
      throw new ValidationError(
        `${name} must start with / (e.g., /app/config/db/host)`,
        name
      );
    }

    if (!PATH_REGEX.test(path)) {
      throw new ValidationError(
        `${name} must be a valid hierarchical path containing only alphanumeric characters, hyphens, underscores, and forward slashes`,
        name
      );
    }

    if (path.includes('//')) {
      throw new ValidationError(
        `${name} cannot contain consecutive slashes`,
        name
      );
    }

    if (path.endsWith('/') && path.length > 1) {
      throw new ValidationError(
        `${name} cannot end with a trailing slash`,
        name
      );
    }
  }

  /**
   * Encode a parameter path for use in URLs.
   *
   * @param {string} path - The parameter path
   * @returns {string} URL-encoded path (without leading slash)
   * @private
   */
  #encodePath(path) {
    // Remove leading slash and encode each segment
    const segments = path.substring(1).split('/');
    return segments.map(segment => encodeURIComponent(segment)).join('/');
  }

  /**
   * Validate set parameter options.
   *
   * @param {SetParameterOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateSetOptions(options) {
    if (options.type !== undefined) {
      oneOf(options.type, Object.values(ParameterType), 'options.type');
    }

    if (options.description !== undefined && typeof options.description !== 'string') {
      throw new ValidationError('options.description must be a string', 'options.description');
    }

    if (options.tags !== undefined) {
      if (typeof options.tags !== 'object' || Array.isArray(options.tags)) {
        throw new ValidationError('options.tags must be an object', 'options.tags');
      }
      for (const [key, value] of Object.entries(options.tags)) {
        if (typeof key !== 'string' || typeof value !== 'string') {
          throw new ValidationError('options.tags must have string keys and values', 'options.tags');
        }
      }
    }

    if (options.overwrite !== undefined && typeof options.overwrite !== 'boolean') {
      throw new ValidationError('options.overwrite must be a boolean', 'options.overwrite');
    }

    if (options.ttl !== undefined) {
      nonNegativeInteger(options.ttl, 'options.ttl');
    }

    if (options.kmsKeyId !== undefined && typeof options.kmsKeyId !== 'string') {
      throw new ValidationError('options.kmsKeyId must be a string', 'options.kmsKeyId');
    }

    if (options.auditEnabled !== undefined && typeof options.auditEnabled !== 'boolean') {
      throw new ValidationError('options.auditEnabled must be a boolean', 'options.auditEnabled');
    }

    // Validate that secureString type has kmsKeyId
    if (options.type === ParameterType.SECURE_STRING && !options.kmsKeyId) {
      throw new ValidationError(
        'kmsKeyId is required when type is secureString',
        'options.kmsKeyId'
      );
    }
  }

  /**
   * Validate list parameters options.
   *
   * @param {ListParametersOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListOptions(options) {
    if (options.recursive !== undefined && typeof options.recursive !== 'boolean') {
      throw new ValidationError('options.recursive must be a boolean', 'options.recursive');
    }

    if (options.withDecryption !== undefined && typeof options.withDecryption !== 'boolean') {
      throw new ValidationError('options.withDecryption must be a boolean', 'options.withDecryption');
    }

    if (options.withValues !== undefined && typeof options.withValues !== 'boolean') {
      throw new ValidationError('options.withValues must be a boolean', 'options.withValues');
    }

    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }

    if (options.cursor !== undefined && typeof options.cursor !== 'string') {
      throw new ValidationError('options.cursor must be a string', 'options.cursor');
    }
  }

  /**
   * Validate search by tags options.
   *
   * @param {SearchByTagsOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateSearchOptions(options) {
    if (options.prefix !== undefined && typeof options.prefix !== 'string') {
      throw new ValidationError('options.prefix must be a string', 'options.prefix');
    }

    if (options.withValues !== undefined && typeof options.withValues !== 'boolean') {
      throw new ValidationError('options.withValues must be a boolean', 'options.withValues');
    }

    if (options.withDecryption !== undefined && typeof options.withDecryption !== 'boolean') {
      throw new ValidationError('options.withDecryption must be a boolean', 'options.withDecryption');
    }

    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }

    if (options.cursor !== undefined && typeof options.cursor !== 'string') {
      throw new ValidationError('options.cursor must be a string', 'options.cursor');
    }
  }

  /**
   * Validate an array of parameter inputs for batch set operation.
   *
   * @param {SetParameterInput[]} parameters - Parameters to validate
   * @throws {ValidationError} If any parameter is invalid
   * @private
   */
  #validateParameterInputs(parameters) {
    parameters.forEach((param, index) => {
      if (!param || typeof param !== 'object') {
        throw new ValidationError(
          `parameters[${index}] must be an object`,
          `parameters[${index}]`
        );
      }

      this.#validatePath(param.path, `parameters[${index}].path`);
      requiredString(param.value, `parameters[${index}].value`);

      // Validate optional fields using the same rules as setParameter
      const options = {
        type: param.type,
        description: param.description,
        tags: param.tags,
        overwrite: param.overwrite,
        ttl: param.ttl,
        kmsKeyId: param.kmsKeyId,
        auditEnabled: param.auditEnabled,
      };

      // Remove undefined values to avoid unnecessary validation
      Object.keys(options).forEach(key => {
        if (options[key] === undefined) {
          delete options[key];
        }
      });

      try {
        this.#validateSetOptions(options);
      } catch (error) {
        // Re-throw with parameter index context
        throw new ValidationError(
          error.message.replace('options.', `parameters[${index}].`),
          error.details?.field?.replace('options.', `parameters[${index}].`)
        );
      }
    });
  }

  /**
   * Transform a SetParameterInput to the API request format.
   *
   * @param {SetParameterInput} input - The input to transform
   * @returns {Object} Transformed request body
   * @private
   */
  #transformSetParameterInput(input) {
    const result = {
      path: input.path,
      value: input.value,
    };

    if (input.type !== undefined) {
      result.type = input.type;
    }
    if (input.description !== undefined) {
      result.description = input.description;
    }
    if (input.tags !== undefined) {
      result.tags = input.tags;
    }
    if (input.overwrite !== undefined) {
      result.overwrite = input.overwrite;
    }
    if (input.ttl !== undefined) {
      result.ttl_seconds = input.ttl;
    }
    if (input.kmsKeyId !== undefined) {
      result.kms_key_id = input.kmsKeyId;
    }
    if (input.auditEnabled !== undefined) {
      result.audit_enabled = input.auditEnabled;
    }

    return result;
  }

  /**
   * Transform server response to Parameter format.
   *
   * @param {Object} response - Raw server response
   * @returns {Parameter}
   * @private
   */
  #transformParameter(response) {
    if (!response) {
      return null;
    }

    return {
      path: response.path ?? '',
      value: response.value ?? '',
      type: response.type ?? ParameterType.STRING,
      description: response.description ?? null,
      tags: response.tags ?? {},
      isEncrypted: response.is_encrypted ?? response.isEncrypted ?? false,
      kmsKeyId: response.kms_key_id ?? response.kmsKeyId ?? null,
      expiresAt: response.expires_at ?? response.expiresAt ?? null,
      auditEnabled: response.audit_enabled ?? response.auditEnabled ?? false,
      createdAt: response.created_at ?? response.createdAt ?? null,
      updatedAt: response.updated_at ?? response.updatedAt ?? null,
      createdBy: response.created_by ?? response.createdBy ?? null,
      updatedBy: response.updated_by ?? response.updatedBy ?? null,
    };
  }
}

export default ParameterStoreClient;
