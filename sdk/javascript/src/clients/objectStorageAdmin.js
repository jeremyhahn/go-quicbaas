/**
 * QuicBaaS JavaScript SDK - Object Storage Admin Client
 *
 * Provides administrative operations for object storage including
 * backend management, storage statistics, garbage collection, and maintenance.
 *
 * @module @quicbaas/client/clients/objectStorageAdmin
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  requiredObject,
  nonNegativeInteger,
  positiveInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * API base path for object storage admin endpoints.
 * @constant {string}
 */
const ADMIN_API_PATH = '/api/v1/storage/admin';

/**
 * Backend type values indicating the type of storage backend.
 * @readonly
 * @enum {string}
 */
export const BackendType = Object.freeze({
  /** Unspecified backend type */
  UNSPECIFIED: 'unspecified',
  /** Local filesystem backend */
  LOCAL: 'local',
  /** Amazon S3 or compatible backend */
  S3: 's3',
  /** Google Cloud Storage backend */
  GCS: 'gcs',
  /** Azure Blob Storage backend */
  AZURE: 'azure',
  /** MinIO object storage backend */
  MINIO: 'minio',
  /** Embedded PebbleDB backend */
  PEBBLE: 'pebble',
  /** Raft-replicated storage backend */
  RAFT: 'raft',
});

/**
 * Storage tier values for cost/performance optimization.
 * @readonly
 * @enum {string}
 */
export const AdminStorageTier = Object.freeze({
  /** Unspecified storage tier */
  UNSPECIFIED: 'unspecified',
  /** Hot storage - frequently accessed, highest performance */
  HOT: 'hot',
  /** Warm storage - infrequently accessed, balanced */
  WARM: 'warm',
  /** Cold storage - rarely accessed, low cost */
  COLD: 'cold',
  /** Archive storage - long-term retention, lowest cost */
  ARCHIVE: 'archive',
});

/**
 * Garbage collection status values.
 * @readonly
 * @enum {string}
 */
export const GarbageCollectionStatus = Object.freeze({
  /** GC operation is running */
  RUNNING: 'running',
  /** GC operation completed successfully */
  COMPLETED: 'completed',
  /** GC operation failed */
  FAILED: 'failed',
  /** GC operation was canceled */
  CANCELED: 'canceled',
});

/**
 * @typedef {Object} CreateBackendRequest
 * @property {string} name - Unique backend name
 * @property {string} type - Backend type (from BackendType enum)
 * @property {string} tier - Storage tier (from AdminStorageTier enum)
 * @property {Object.<string, string>} config - Backend-specific configuration
 * @property {boolean} [enabled=true] - Whether the backend should be enabled
 */

/**
 * @typedef {Object} CreateBackendResponse
 * @property {string} name - Created backend name
 * @property {string} createdAt - Creation timestamp (ISO format)
 */

/**
 * @typedef {Object} UpdateBackendRequest
 * @property {string} name - Backend name to update
 * @property {Object.<string, string>} [config] - Updated configuration (only specified keys are updated)
 * @property {string} [tier] - Updated storage tier
 */

/**
 * @typedef {Object} UpdateBackendResponse
 * @property {string} name - Updated backend name
 * @property {string} updatedAt - Update timestamp (ISO format)
 */

/**
 * @typedef {Object} DeleteBackendRequest
 * @property {string} name - Backend name to delete
 * @property {string} [migrateTo] - Where to migrate objects before deletion
 * @property {boolean} [force=false] - Force delete even if objects exist (data loss)
 */

/**
 * @typedef {Object} DeleteBackendResponse
 * @property {string} name - Deleted backend name
 * @property {boolean} deleted - Whether the backend was deleted
 * @property {number} migratedObjects - Count of migrated objects
 */

/**
 * @typedef {Object} SetBackendStatusRequest
 * @property {string} name - Backend name
 * @property {boolean} enabled - New enabled status
 */

/**
 * @typedef {Object} SetBackendStatusResponse
 * @property {string} name - Backend name
 * @property {boolean} enabled - New status
 * @property {string} updatedAt - Update timestamp (ISO format)
 */

/**
 * @typedef {Object} GetStorageStatsRequest
 * @property {boolean} [includeBackends=false] - Include per-backend breakdown
 * @property {boolean} [includeTenants=false] - Include per-tenant breakdown
 */

/**
 * @typedef {Object} BackendInfo
 * @property {string} name - Unique backend name
 * @property {string} type - Backend type
 * @property {boolean} enabled - Whether backend is enabled
 * @property {boolean} healthy - Whether backend is healthy
 * @property {string} tier - Storage tier
 * @property {string} [region] - Geographic region
 * @property {string} [endpoint] - Backend endpoint URL
 * @property {string} [bucket] - Bucket/container name (for cloud storage)
 * @property {number} capacityBytes - Total capacity (if known)
 * @property {number} usedBytes - Current usage
 * @property {number} objectCount - Number of objects stored
 * @property {string} [lastHealthCheck] - Last health check timestamp (ISO format)
 * @property {Object.<string, string>} [metadata] - Additional backend-specific metadata
 */

/**
 * @typedef {Object} TenantUsage
 * @property {string} tenantId - Tenant identifier
 * @property {number} totalBytes - Total storage used
 * @property {number} objectCount - Total number of objects
 * @property {number} hotBytes - Storage in hot tier
 * @property {number} warmBytes - Storage in warm tier
 * @property {number} coldBytes - Storage in cold tier
 * @property {number} archiveBytes - Storage in archive tier
 * @property {string} calculatedAt - When usage was calculated (ISO format)
 */

/**
 * @typedef {Object} GetStorageStatsResponse
 * @property {number} totalBytes - Total storage used
 * @property {number} totalObjects - Total object count
 * @property {number} totalBackends - Number of backends
 * @property {number} healthyBackends - Number of healthy backends
 * @property {BackendInfo[]} [backendStats] - Per-backend statistics (if requested)
 * @property {TenantUsage[]} [tenantStats] - Per-tenant statistics (if requested)
 * @property {string} calculatedAt - When stats were calculated (ISO format)
 */

/**
 * @typedef {Object} RunGarbageCollectionRequest
 * @property {string[]} [backends] - Which backends to GC (empty means all)
 * @property {boolean} [dryRun=false] - Report what would be deleted without deleting
 * @property {boolean} [aggressive=false] - Use more aggressive GC settings
 */

/**
 * @typedef {Object} RunGarbageCollectionResponse
 * @property {string} operationId - Async operation ID for tracking
 * @property {string} startedAt - When GC started (ISO format)
 */

/**
 * @typedef {Object} GetGarbageCollectionProgressRequest
 * @property {string} operationId - GC operation ID
 */

/**
 * @typedef {Object} GetGarbageCollectionProgressResponse
 * @property {string} operationId - GC operation ID
 * @property {string} status - GC status (running, completed, failed, canceled)
 * @property {number} objectsScanned - Count of scanned objects
 * @property {number} objectsDeleted - Count of deleted objects
 * @property {number} bytesReclaimed - Storage reclaimed
 * @property {number} percentComplete - Completion percentage (0-100)
 * @property {string} [error] - Error message if failed
 * @property {string} [completedAt] - When GC completed (ISO format, if finished)
 */

/**
 * @typedef {Object} RepairObjectRequest
 * @property {string} key - Object key to repair
 * @property {string} [sourceLocation] - Which location has the correct data
 * @property {number} [timeoutMs] - Maximum time to wait (milliseconds)
 */

/**
 * @typedef {Object} RepairObjectResponse
 * @property {string} key - Repaired object key
 * @property {boolean} success - Whether repair succeeded
 * @property {string[]} repairedLocations - Locations that were repaired
 * @property {string} [error] - Error message if failed
 */

/**
 * @typedef {Object} CompactBackendRequest
 * @property {string} name - Backend name
 * @property {boolean} [aggressive=false] - Use more aggressive compaction settings
 */

/**
 * @typedef {Object} CompactBackendResponse
 * @property {string} name - Backend name
 * @property {string} operationId - Async operation ID
 * @property {string} startedAt - When compaction started (ISO format)
 */

/**
 * Object Storage Admin client for QuicBaaS.
 *
 * Provides administrative operations for managing storage backends,
 * retrieving storage statistics, running garbage collection, and
 * performing maintenance operations.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import ObjectStorageAdminClient, { BackendType, AdminStorageTier } from '@quicbaas/client/clients/objectStorageAdmin';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const admin = new ObjectStorageAdminClient(transport);
 *
 * // Create a new S3 backend
 * const { name, createdAt } = await admin.createBackend({
 *   name: 'my-s3-backend',
 *   type: BackendType.S3,
 *   tier: AdminStorageTier.HOT,
 *   config: {
 *     bucket: 'my-bucket',
 *     region: 'us-east-1',
 *   }
 * });
 *
 * // Get storage statistics
 * const stats = await admin.getStorageStats({ includeBackends: true });
 * console.log(`Total storage: ${stats.totalBytes} bytes`);
 */
export class ObjectStorageAdminClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new ObjectStorageAdminClient instance.
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

  // ==========================================================================
  // Backend Management Operations
  // ==========================================================================

  /**
   * Create a new storage backend.
   *
   * @param {CreateBackendRequest} request - Backend creation request
   * @returns {Promise<CreateBackendResponse>} Response with backend name and creation time
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const { name, createdAt } = await admin.createBackend({
   *   name: 'local-storage',
   *   type: BackendType.LOCAL,
   *   tier: AdminStorageTier.HOT,
   *   config: {
   *     path: '/data/objects',
   *   },
   *   enabled: true
   * });
   */
  async createBackend(request) {
    this.#validateCreateBackendRequest(request);

    const requestBody = {
      name: request.name,
      type: request.type,
      tier: request.tier,
      config: request.config,
    };

    if (request.enabled !== undefined) {
      requestBody.enabled = request.enabled;
    }

    const response = await this.#transport.post(
      `${ADMIN_API_PATH}/backends`,
      requestBody
    );

    return this.#transformCreateBackendResponse(response, request.name);
  }

  /**
   * Update backend configuration.
   *
   * @param {UpdateBackendRequest} request - Backend update request
   * @returns {Promise<UpdateBackendResponse>} Response with backend name and update time
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const { name, updatedAt } = await admin.updateBackend({
   *   name: 'my-backend',
   *   tier: AdminStorageTier.WARM,
   *   config: {
   *     maxConnections: '100'
   *   }
   * });
   */
  async updateBackend(request) {
    this.#validateUpdateBackendRequest(request);

    const requestBody = {};

    if (request.config !== undefined) {
      requestBody.config = request.config;
    }
    if (request.tier !== undefined) {
      requestBody.tier = request.tier;
    }

    const response = await this.#transport.put(
      `${ADMIN_API_PATH}/backends/${encodeURIComponent(request.name)}`,
      requestBody
    );

    return this.#transformUpdateBackendResponse(response, request.name);
  }

  /**
   * Delete a storage backend.
   *
   * @param {DeleteBackendRequest} request - Backend deletion request
   * @returns {Promise<DeleteBackendResponse>} Response with deletion details
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * // Delete with migration
   * const { deleted, migratedObjects } = await admin.deleteBackend({
   *   name: 'old-backend',
   *   migrateTo: 'new-backend'
   * });
   *
   * // Force delete (data loss)
   * const { deleted } = await admin.deleteBackend({
   *   name: 'unused-backend',
   *   force: true
   * });
   */
  async deleteBackend(request) {
    this.#validateDeleteBackendRequest(request);

    const query = {};

    if (request.migrateTo !== undefined) {
      query.migrate_to = request.migrateTo;
    }
    if (request.force !== undefined) {
      query.force = request.force;
    }

    const response = await this.#transport.delete(
      `${ADMIN_API_PATH}/backends/${encodeURIComponent(request.name)}`,
      { query }
    );

    return this.#transformDeleteBackendResponse(response, request.name);
  }

  /**
   * Enable or disable a storage backend.
   *
   * @param {SetBackendStatusRequest} request - Backend status request
   * @returns {Promise<SetBackendStatusResponse>} Response with new status
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * // Disable a backend for maintenance
   * const { enabled, updatedAt } = await admin.setBackendStatus({
   *   name: 'my-backend',
   *   enabled: false
   * });
   *
   * // Re-enable after maintenance
   * await admin.setBackendStatus({
   *   name: 'my-backend',
   *   enabled: true
   * });
   */
  async setBackendStatus(request) {
    this.#validateSetBackendStatusRequest(request);

    const requestBody = {
      enabled: request.enabled,
    };

    const response = await this.#transport.post(
      `${ADMIN_API_PATH}/backends/${encodeURIComponent(request.name)}/status`,
      requestBody
    );

    return this.#transformSetBackendStatusResponse(response, request.name, request.enabled);
  }

  // ==========================================================================
  // Storage Statistics Operations
  // ==========================================================================

  /**
   * Get cluster-wide storage statistics.
   *
   * @param {GetStorageStatsRequest} [request={}] - Statistics request options
   * @returns {Promise<GetStorageStatsResponse>} Storage statistics
   *
   * @example
   * // Get basic stats
   * const stats = await admin.getStorageStats();
   * console.log(`Total objects: ${stats.totalObjects}`);
   *
   * // Get detailed stats with backend breakdown
   * const detailedStats = await admin.getStorageStats({
   *   includeBackends: true,
   *   includeTenants: true
   * });
   */
  async getStorageStats(request = {}) {
    const query = {};

    if (request.includeBackends !== undefined) {
      query.include_backends = request.includeBackends;
    }
    if (request.includeTenants !== undefined) {
      query.include_tenants = request.includeTenants;
    }

    const response = await this.#transport.get(
      `${ADMIN_API_PATH}/stats`,
      { query }
    );

    return this.#transformGetStorageStatsResponse(response);
  }

  // ==========================================================================
  // Garbage Collection Operations
  // ==========================================================================

  /**
   * Initiate garbage collection on specified backends.
   *
   * @param {RunGarbageCollectionRequest} [request={}] - GC request options
   * @returns {Promise<RunGarbageCollectionResponse>} Response with operation ID
   *
   * @example
   * // Run GC on all backends
   * const { operationId } = await admin.runGarbageCollection();
   *
   * // Dry run on specific backends
   * const { operationId } = await admin.runGarbageCollection({
   *   backends: ['backend-1', 'backend-2'],
   *   dryRun: true
   * });
   *
   * // Aggressive GC
   * const { operationId } = await admin.runGarbageCollection({
   *   aggressive: true
   * });
   */
  async runGarbageCollection(request = {}) {
    this.#validateRunGarbageCollectionRequest(request);

    const requestBody = {};

    if (request.backends !== undefined && request.backends.length > 0) {
      requestBody.backends = request.backends;
    }
    if (request.dryRun !== undefined) {
      requestBody.dry_run = request.dryRun;
    }
    if (request.aggressive !== undefined) {
      requestBody.aggressive = request.aggressive;
    }

    const response = await this.#transport.post(
      `${ADMIN_API_PATH}/gc`,
      requestBody
    );

    return this.#transformRunGarbageCollectionResponse(response);
  }

  /**
   * Get garbage collection progress.
   *
   * @param {GetGarbageCollectionProgressRequest} request - Progress request
   * @returns {Promise<GetGarbageCollectionProgressResponse>} GC progress information
   * @throws {ValidationError} If operationId is not provided
   *
   * @example
   * const progress = await admin.getGarbageCollectionProgress({
   *   operationId: 'gc-op-12345'
   * });
   *
   * console.log(`Status: ${progress.status}`);
   * console.log(`Progress: ${progress.percentComplete}%`);
   * console.log(`Reclaimed: ${progress.bytesReclaimed} bytes`);
   */
  async getGarbageCollectionProgress(request) {
    this.#validateGetGarbageCollectionProgressRequest(request);

    const response = await this.#transport.get(
      `${ADMIN_API_PATH}/gc/progress`,
      { query: { operation_id: request.operationId } }
    );

    return this.#transformGetGarbageCollectionProgressResponse(response, request.operationId);
  }

  // ==========================================================================
  // Repair Operations
  // ==========================================================================

  /**
   * Repair an object with inconsistent replicas.
   *
   * @param {RepairObjectRequest} request - Repair request
   * @returns {Promise<RepairObjectResponse>} Repair result
   * @throws {ValidationError} If key is not provided
   *
   * @example
   * const { success, repairedLocations } = await admin.repairObject({
   *   key: 'namespace:path/to/object',
   *   sourceLocation: 'primary-backend'
   * });
   *
   * if (success) {
   *   console.log(`Repaired locations: ${repairedLocations.join(', ')}`);
   * }
   */
  async repairObject(request) {
    this.#validateRepairObjectRequest(request);

    const requestBody = {
      key: request.key,
    };

    if (request.sourceLocation !== undefined) {
      requestBody.source_location = request.sourceLocation;
    }
    if (request.timeoutMs !== undefined) {
      requestBody.timeout_ms = request.timeoutMs;
    }

    const response = await this.#transport.post(
      `${ADMIN_API_PATH}/repair`,
      requestBody
    );

    return this.#transformRepairObjectResponse(response, request.key);
  }

  // ==========================================================================
  // Compaction Operations
  // ==========================================================================

  /**
   * Trigger compaction on a backend.
   *
   * @param {CompactBackendRequest} request - Compaction request
   * @returns {Promise<CompactBackendResponse>} Response with operation ID
   * @throws {ValidationError} If name is not provided
   *
   * @example
   * const { operationId, startedAt } = await admin.compactBackend({
   *   name: 'my-backend',
   *   aggressive: true
   * });
   *
   * console.log(`Compaction started at ${startedAt}`);
   */
  async compactBackend(request) {
    this.#validateCompactBackendRequest(request);

    const requestBody = {};

    if (request.aggressive !== undefined) {
      requestBody.aggressive = request.aggressive;
    }

    const response = await this.#transport.post(
      `${ADMIN_API_PATH}/backends/${encodeURIComponent(request.name)}/compact`,
      requestBody
    );

    return this.#transformCompactBackendResponse(response, request.name);
  }

  // ==========================================================================
  // Validation Methods
  // ==========================================================================

  /**
   * Validate create backend request.
   *
   * @param {CreateBackendRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateCreateBackendRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');
    requiredString(request.type, 'request.type');
    requiredString(request.tier, 'request.tier');
    requiredObject(request.config, 'request.config');

    oneOf(request.type, Object.values(BackendType), 'request.type');
    oneOf(request.tier, Object.values(AdminStorageTier), 'request.tier');

    if (request.type === BackendType.UNSPECIFIED) {
      throw new ValidationError('request.type cannot be unspecified', 'request.type');
    }
    if (request.tier === AdminStorageTier.UNSPECIFIED) {
      throw new ValidationError('request.tier cannot be unspecified', 'request.tier');
    }

    if (Object.keys(request.config).length === 0) {
      throw new ValidationError('request.config cannot be empty', 'request.config');
    }

    if (request.enabled !== undefined && typeof request.enabled !== 'boolean') {
      throw new ValidationError('request.enabled must be a boolean', 'request.enabled');
    }
  }

  /**
   * Validate update backend request.
   *
   * @param {UpdateBackendRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateUpdateBackendRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');

    if (request.config === undefined && request.tier === undefined) {
      throw new ValidationError(
        'at least one of request.config or request.tier must be provided',
        'request'
      );
    }

    if (request.config !== undefined) {
      if (typeof request.config !== 'object' || Array.isArray(request.config)) {
        throw new ValidationError('request.config must be an object', 'request.config');
      }
    }

    if (request.tier !== undefined) {
      oneOf(request.tier, Object.values(AdminStorageTier), 'request.tier');
    }
  }

  /**
   * Validate delete backend request.
   *
   * @param {DeleteBackendRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateDeleteBackendRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');

    if (request.migrateTo !== undefined && typeof request.migrateTo !== 'string') {
      throw new ValidationError('request.migrateTo must be a string', 'request.migrateTo');
    }

    if (request.force !== undefined && typeof request.force !== 'boolean') {
      throw new ValidationError('request.force must be a boolean', 'request.force');
    }
  }

  /**
   * Validate set backend status request.
   *
   * @param {SetBackendStatusRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateSetBackendStatusRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');
    required(request.enabled, 'request.enabled');

    if (typeof request.enabled !== 'boolean') {
      throw new ValidationError('request.enabled must be a boolean', 'request.enabled');
    }
  }

  /**
   * Validate run garbage collection request.
   *
   * @param {RunGarbageCollectionRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateRunGarbageCollectionRequest(request) {
    if (request.backends !== undefined) {
      if (!Array.isArray(request.backends)) {
        throw new ValidationError('request.backends must be an array', 'request.backends');
      }

      for (let i = 0; i < request.backends.length; i++) {
        if (typeof request.backends[i] !== 'string' || request.backends[i].trim() === '') {
          throw new ValidationError(
            `request.backends[${i}] must be a non-empty string`,
            `request.backends[${i}]`
          );
        }
      }
    }

    if (request.dryRun !== undefined && typeof request.dryRun !== 'boolean') {
      throw new ValidationError('request.dryRun must be a boolean', 'request.dryRun');
    }

    if (request.aggressive !== undefined && typeof request.aggressive !== 'boolean') {
      throw new ValidationError('request.aggressive must be a boolean', 'request.aggressive');
    }
  }

  /**
   * Validate get garbage collection progress request.
   *
   * @param {GetGarbageCollectionProgressRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateGetGarbageCollectionProgressRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.operationId, 'request.operationId');
  }

  /**
   * Validate repair object request.
   *
   * @param {RepairObjectRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateRepairObjectRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.key, 'request.key');

    if (request.sourceLocation !== undefined && typeof request.sourceLocation !== 'string') {
      throw new ValidationError('request.sourceLocation must be a string', 'request.sourceLocation');
    }

    if (request.timeoutMs !== undefined) {
      positiveInteger(request.timeoutMs, 'request.timeoutMs');
    }
  }

  /**
   * Validate compact backend request.
   *
   * @param {CompactBackendRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateCompactBackendRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');

    if (request.aggressive !== undefined && typeof request.aggressive !== 'boolean') {
      throw new ValidationError('request.aggressive must be a boolean', 'request.aggressive');
    }
  }

  // ==========================================================================
  // Response Transformation Methods
  // ==========================================================================

  /**
   * Format a Unix nanosecond timestamp to ISO string.
   *
   * @param {number|string} timestamp - Unix nanoseconds or ISO string
   * @returns {string} ISO timestamp string
   * @private
   */
  #formatTimestamp(timestamp) {
    if (!timestamp) {
      return new Date().toISOString();
    }
    if (typeof timestamp === 'string') {
      return timestamp;
    }
    // Convert nanoseconds to milliseconds
    return new Date(timestamp / 1000000).toISOString();
  }

  /**
   * Transform create backend response.
   *
   * @param {Object} response - Raw server response
   * @param {string} requestName - Requested backend name
   * @returns {CreateBackendResponse}
   * @private
   */
  #transformCreateBackendResponse(response, requestName) {
    return {
      name: response?.name ?? requestName,
      createdAt: this.#formatTimestamp(response?.created_at ?? response?.createdAt),
    };
  }

  /**
   * Transform update backend response.
   *
   * @param {Object} response - Raw server response
   * @param {string} requestName - Requested backend name
   * @returns {UpdateBackendResponse}
   * @private
   */
  #transformUpdateBackendResponse(response, requestName) {
    return {
      name: response?.name ?? requestName,
      updatedAt: this.#formatTimestamp(response?.updated_at ?? response?.updatedAt),
    };
  }

  /**
   * Transform delete backend response.
   *
   * @param {Object} response - Raw server response
   * @param {string} requestName - Requested backend name
   * @returns {DeleteBackendResponse}
   * @private
   */
  #transformDeleteBackendResponse(response, requestName) {
    return {
      name: response?.name ?? requestName,
      deleted: response?.deleted ?? true,
      migratedObjects: response?.migrated_objects ?? response?.migratedObjects ?? 0,
    };
  }

  /**
   * Transform set backend status response.
   *
   * @param {Object} response - Raw server response
   * @param {string} requestName - Requested backend name
   * @param {boolean} requestEnabled - Requested enabled status
   * @returns {SetBackendStatusResponse}
   * @private
   */
  #transformSetBackendStatusResponse(response, requestName, requestEnabled) {
    return {
      name: response?.name ?? requestName,
      enabled: response?.enabled ?? requestEnabled,
      updatedAt: this.#formatTimestamp(response?.updated_at ?? response?.updatedAt),
    };
  }

  /**
   * Transform backend info from server response.
   *
   * @param {Object} backend - Raw backend object from server
   * @returns {BackendInfo}
   * @private
   */
  #transformBackendInfo(backend) {
    return {
      name: backend.name ?? '',
      type: backend.type ?? BackendType.UNSPECIFIED,
      enabled: backend.enabled ?? false,
      healthy: backend.healthy ?? false,
      tier: backend.tier ?? AdminStorageTier.UNSPECIFIED,
      region: backend.region,
      endpoint: backend.endpoint,
      bucket: backend.bucket,
      capacityBytes: backend.capacity_bytes ?? backend.capacityBytes ?? 0,
      usedBytes: backend.used_bytes ?? backend.usedBytes ?? 0,
      objectCount: backend.object_count ?? backend.objectCount ?? 0,
      lastHealthCheck: backend.last_health_check ?? backend.lastHealthCheck
        ? this.#formatTimestamp(backend.last_health_check ?? backend.lastHealthCheck)
        : undefined,
      metadata: backend.metadata,
    };
  }

  /**
   * Transform tenant usage from server response.
   *
   * @param {Object} tenant - Raw tenant usage object from server
   * @returns {TenantUsage}
   * @private
   */
  #transformTenantUsage(tenant) {
    return {
      tenantId: tenant.tenant_id ?? tenant.tenantId ?? '',
      totalBytes: tenant.total_bytes ?? tenant.totalBytes ?? 0,
      objectCount: tenant.object_count ?? tenant.objectCount ?? 0,
      hotBytes: tenant.hot_bytes ?? tenant.hotBytes ?? 0,
      warmBytes: tenant.warm_bytes ?? tenant.warmBytes ?? 0,
      coldBytes: tenant.cold_bytes ?? tenant.coldBytes ?? 0,
      archiveBytes: tenant.archive_bytes ?? tenant.archiveBytes ?? 0,
      calculatedAt: this.#formatTimestamp(tenant.calculated_at ?? tenant.calculatedAt),
    };
  }

  /**
   * Transform get storage stats response.
   *
   * @param {Object} response - Raw server response
   * @returns {GetStorageStatsResponse}
   * @private
   */
  #transformGetStorageStatsResponse(response) {
    const result = {
      totalBytes: response?.total_bytes ?? response?.totalBytes ?? 0,
      totalObjects: response?.total_objects ?? response?.totalObjects ?? 0,
      totalBackends: response?.total_backends ?? response?.totalBackends ?? 0,
      healthyBackends: response?.healthy_backends ?? response?.healthyBackends ?? 0,
      calculatedAt: this.#formatTimestamp(response?.calculated_at ?? response?.calculatedAt),
    };

    if (response?.backend_stats ?? response?.backendStats) {
      const backendStats = response?.backend_stats ?? response?.backendStats ?? [];
      result.backendStats = backendStats.map(backend => this.#transformBackendInfo(backend));
    }

    if (response?.tenant_stats ?? response?.tenantStats) {
      const tenantStats = response?.tenant_stats ?? response?.tenantStats ?? [];
      result.tenantStats = tenantStats.map(tenant => this.#transformTenantUsage(tenant));
    }

    return result;
  }

  /**
   * Transform run garbage collection response.
   *
   * @param {Object} response - Raw server response
   * @returns {RunGarbageCollectionResponse}
   * @private
   */
  #transformRunGarbageCollectionResponse(response) {
    return {
      operationId: response?.operation_id ?? response?.operationId ?? '',
      startedAt: this.#formatTimestamp(response?.started_at ?? response?.startedAt),
    };
  }

  /**
   * Transform get garbage collection progress response.
   *
   * @param {Object} response - Raw server response
   * @param {string} requestOperationId - Requested operation ID
   * @returns {GetGarbageCollectionProgressResponse}
   * @private
   */
  #transformGetGarbageCollectionProgressResponse(response, requestOperationId) {
    const result = {
      operationId: response?.operation_id ?? response?.operationId ?? requestOperationId,
      status: response?.status ?? GarbageCollectionStatus.RUNNING,
      objectsScanned: response?.objects_scanned ?? response?.objectsScanned ?? 0,
      objectsDeleted: response?.objects_deleted ?? response?.objectsDeleted ?? 0,
      bytesReclaimed: response?.bytes_reclaimed ?? response?.bytesReclaimed ?? 0,
      percentComplete: response?.percent_complete ?? response?.percentComplete ?? 0,
    };

    if (response?.error) {
      result.error = response.error;
    }

    if (response?.completed_at ?? response?.completedAt) {
      result.completedAt = this.#formatTimestamp(response?.completed_at ?? response?.completedAt);
    }

    return result;
  }

  /**
   * Transform repair object response.
   *
   * @param {Object} response - Raw server response
   * @param {string} requestKey - Requested object key
   * @returns {RepairObjectResponse}
   * @private
   */
  #transformRepairObjectResponse(response, requestKey) {
    const result = {
      key: response?.key ?? requestKey,
      success: response?.success ?? false,
      repairedLocations: response?.repaired_locations ?? response?.repairedLocations ?? [],
    };

    if (response?.error) {
      result.error = response.error;
    }

    return result;
  }

  /**
   * Transform compact backend response.
   *
   * @param {Object} response - Raw server response
   * @param {string} requestName - Requested backend name
   * @returns {CompactBackendResponse}
   * @private
   */
  #transformCompactBackendResponse(response, requestName) {
    return {
      name: response?.name ?? requestName,
      operationId: response?.operation_id ?? response?.operationId ?? '',
      startedAt: this.#formatTimestamp(response?.started_at ?? response?.startedAt),
    };
  }
}

export default ObjectStorageAdminClient;
