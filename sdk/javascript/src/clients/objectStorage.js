/**
 * QuicBaaS JavaScript SDK - Object Storage Client
 *
 * @module @quicbaas/client/clients/objectStorage
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  nonEmptyArray,
  positiveInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * Replication modes for object replication.
 * @readonly
 * @enum {string}
 */
export const ReplicationMode = Object.freeze({
  /** Waits for all replicas before returning success */
  SYNC: 'sync',
  /** Returns immediately, replicates in background */
  ASYNC: 'async',
  /** Waits for majority of replicas */
  QUORUM: 'quorum',
});

/**
 * Object status values.
 * @readonly
 * @enum {string}
 */
export const ObjectStatus = Object.freeze({
  /** Object is active and accessible */
  ACTIVE: 'active',
  /** Object has been archived */
  ARCHIVED: 'archived',
  /** Object is being migrated */
  MIGRATING: 'migrating',
  /** Object is pending deletion */
  PENDING_DELETE: 'pending_delete',
});

/**
 * Storage tier values.
 * @readonly
 * @enum {string}
 */
export const StorageTier = Object.freeze({
  /** Hot storage for frequently accessed data */
  HOT: 'hot',
  /** Warm storage for less frequently accessed data */
  WARM: 'warm',
  /** Cold storage for rarely accessed data */
  COLD: 'cold',
});

/**
 * @typedef {Object} PutObjectOptions
 * @property {string} [contentType] - MIME type of the object
 * @property {string} [tenantId] - Tenant that owns the object
 * @property {Object.<string, string>} [tags] - User-defined key-value pairs
 * @property {ReplicationPolicy} [replicationPolicy] - Replication policy override
 * @property {ArchivePolicy} [archivePolicy] - Archive policy override
 * @property {RetentionPolicy} [retentionPolicy] - Retention policy override
 * @property {string} [targetBackend] - Explicit storage backend
 * @property {Uint8Array|Array<number>} [checksum] - Client-provided checksum for verification
 */

/**
 * @typedef {Object} ReplicationPolicy
 * @property {boolean} enabled - Whether replication is active
 * @property {number} minReplicas - Minimum number of replicas required
 * @property {Array<string>} [targetStores] - Backend names to replicate to
 * @property {string} [mode] - Replication mode (sync, async, quorum)
 * @property {number} [priority] - Replication priority (1-10)
 */

/**
 * @typedef {Object} ArchivePolicy
 * @property {boolean} enabled - Whether archival is active
 * @property {number} [archiveAfterDays] - Days since last access before archiving
 * @property {string} [targetStore] - Archive backend name
 * @property {boolean} [deleteFromPrimary] - Remove from primary after archiving
 * @property {number} [compressionLevel] - Compression level (0-9)
 */

/**
 * @typedef {Object} RetentionPolicy
 * @property {boolean} enabled - Whether retention enforcement is active
 * @property {number} [retentionDays] - Auto-delete after N days (0 = never)
 * @property {number} [maxVersions] - Keep last N versions (0 = unlimited)
 * @property {number} [deleteAfter] - Unix timestamp for deletion
 */

/**
 * @typedef {Object} PutObjectResponse
 * @property {string} key - The stored object key
 * @property {number} objectId - Internal object identifier
 * @property {string} checksum - SHA-256 checksum of stored data (base64)
 * @property {number} size - Size in bytes
 * @property {string} backend - Storage backend used
 * @property {number} version - Object version
 * @property {string} createdAt - Creation timestamp (ISO format)
 */

/**
 * @typedef {Object} GetObjectResponse
 * @property {string} key - Object key
 * @property {*} value - Object data
 * @property {string} contentType - MIME type
 * @property {number} size - Size in bytes
 * @property {string} checksum - SHA-256 checksum (base64)
 * @property {number} version - Object version
 * @property {Object.<string, string>} tags - User-defined tags
 * @property {string} tenantId - Owning tenant
 * @property {string} createdAt - Creation timestamp (ISO format)
 * @property {string} modifiedAt - Last modification timestamp (ISO format)
 * @property {string} backend - Storage backend serving the object
 */

/**
 * @typedef {Object} DeleteObjectResponse
 * @property {string} key - Deleted object key
 * @property {boolean} success - Whether deletion succeeded
 * @property {number} size - Size of deleted object
 * @property {string} timestamp - Deletion timestamp (ISO format)
 */

/**
 * @typedef {Object} HeadObjectResponse
 * @property {string} key - Object key
 * @property {boolean} exists - Whether object exists
 * @property {string} contentType - MIME type
 * @property {number} size - Size in bytes
 * @property {string} checksum - SHA-256 checksum (base64)
 * @property {number} version - Object version
 * @property {Object.<string, string>} tags - User-defined tags
 * @property {string} tenantId - Owning tenant
 * @property {string} status - Object status
 * @property {string} createdAt - Creation timestamp (ISO format)
 * @property {string} modifiedAt - Last modification timestamp (ISO format)
 * @property {string} lastAccessAt - Last access timestamp (ISO format)
 * @property {string} backend - Primary storage backend
 * @property {number} replicaCount - Number of replicas
 */

/**
 * @typedef {Object} ListObjectsOptions
 * @property {string} [prefix] - Filter objects by key prefix
 * @property {string} [tenantId] - Filter by tenant
 * @property {number} [limit] - Maximum number of results
 * @property {string} [cursor] - Pagination cursor
 * @property {boolean} [includeTags] - Include tags in results
 */

/**
 * @typedef {Object} ObjectSummary
 * @property {string} key - Object key
 * @property {number} size - Size in bytes
 * @property {string} contentType - MIME type
 * @property {number} version - Object version
 * @property {string} status - Object status
 * @property {Object.<string, string>} [tags] - User-defined tags
 * @property {string} createdAt - Creation timestamp (ISO format)
 * @property {string} modifiedAt - Last modification timestamp (ISO format)
 */

/**
 * @typedef {Object} ListObjectsResponse
 * @property {Array<ObjectSummary>} objects - Matching objects
 * @property {string|null} nextCursor - Cursor for next page
 * @property {boolean} hasMore - Whether more results are available
 * @property {number} totalCount - Approximate total count
 * @property {string} timestamp - Query timestamp (ISO format)
 */

/**
 * @typedef {Object} ListByTagOptions
 * @property {string} [tenantId] - Filter by tenant
 * @property {number} [limit] - Maximum number of results
 * @property {string} [cursor] - Pagination cursor
 */

/**
 * @typedef {Object} ListByTenantOptions
 * @property {number} [limit] - Maximum number of results
 * @property {string} [cursor] - Pagination cursor
 * @property {boolean} [includeTags] - Include tags in results
 */

/**
 * @typedef {Object} CopyObjectOptions
 * @property {string} [tenantId] - Destination tenant
 * @property {boolean} [overwriteExisting] - Allow overwriting existing object
 * @property {string} [targetBackend] - Backend for the copy
 */

/**
 * @typedef {Object} CopyObjectResponse
 * @property {string} key - Destination object key
 * @property {number} objectId - New object identifier
 * @property {number} size - Copied object size
 * @property {string} checksum - Checksum of copied object (base64)
 * @property {number} version - New object version
 * @property {string} backend - Storage backend used
 * @property {string} createdAt - Creation timestamp (ISO format)
 */

/**
 * @typedef {Object} ReplicateObjectOptions
 * @property {string} [tenantId] - For access control
 * @property {number} [priority] - Replication priority (1-10)
 * @property {boolean} [synchronous] - Whether replication should be synchronous
 */

/**
 * @typedef {Object} ReplicationResult
 * @property {string} target - Backend name
 * @property {boolean} success - Whether replication succeeded
 * @property {string} [error] - Error message if failed
 * @property {number} duration - Replication duration in milliseconds
 */

/**
 * @typedef {Object} ReplicateObjectResponse
 * @property {string} key - Replicated object key
 * @property {string} status - Overall replication status
 * @property {Object.<string, ReplicationResult>} results - Per-target results
 * @property {string} timestamp - Operation timestamp (ISO format)
 */

/**
 * @typedef {Object} MigrateObjectOptions
 * @property {string} [tenantId] - For access control
 * @property {boolean} [deleteFromSource] - Delete from source after migration
 */

/**
 * @typedef {Object} MigrateObjectResponse
 * @property {string} key - Migrated object key
 * @property {string} sourceBackend - Original backend
 * @property {string} targetBackend - New backend
 * @property {boolean} success - Whether migration succeeded
 * @property {boolean} deletedFromSource - Whether deleted from source
 * @property {string} timestamp - Operation timestamp (ISO format)
 */

/**
 * @typedef {Object} ArchiveObjectOptions
 * @property {string} [tenantId] - For access control
 * @property {boolean} [deleteFromPrimary] - Delete from primary after archiving
 */

/**
 * @typedef {Object} ArchiveObjectResponse
 * @property {string} key - Archived object key
 * @property {string} archiveStore - Archive backend name
 * @property {boolean} success - Whether archival succeeded
 * @property {boolean} deletedFromPrimary - Whether deleted from primary
 * @property {string} archivedAt - Archival timestamp (ISO format)
 */

/**
 * @typedef {Object} RestoreObjectOptions
 * @property {string} [tenantId] - For access control
 * @property {string} [targetBackend] - Destination backend for restore
 */

/**
 * @typedef {Object} RestoreObjectResponse
 * @property {string} key - Restored object key
 * @property {string} backend - Backend where object was restored
 * @property {boolean} success - Whether restore succeeded
 * @property {string} restoredAt - Restore timestamp (ISO format)
 */

/**
 * @typedef {Object} VerifyObjectOptions
 * @property {Array<string>} [stores] - Backends to verify (empty = all)
 * @property {string} [tenantId] - For access control
 * @property {boolean} [repairMismatches] - Repair any mismatches found
 */

/**
 * @typedef {Object} VerificationResult
 * @property {string} store - Backend name
 * @property {boolean} valid - Whether the copy is valid
 * @property {boolean} checksumMatch - Whether checksums match
 * @property {boolean} sizeMatch - Whether sizes match
 * @property {string} [error] - Error message if verification failed
 */

/**
 * @typedef {Object} VerifyObjectResponse
 * @property {string} key - Verified object key
 * @property {boolean} valid - Whether object is valid across all locations
 * @property {Object.<string, VerificationResult>} results - Per-store results
 * @property {boolean} repaired - Whether any mismatches were repaired
 * @property {string} timestamp - Verification timestamp (ISO format)
 */

/**
 * @typedef {Object} ObjectPolicy
 * @property {ReplicationPolicy} [replicationPolicy] - Replication policy
 * @property {ArchivePolicy} [archivePolicy] - Archive policy
 * @property {RetentionPolicy} [retentionPolicy] - Retention policy
 */

/**
 * @typedef {Object} SetObjectPolicyResponse
 * @property {string} key - Object key
 * @property {boolean} success - Whether policies were set
 * @property {number} version - New object version
 * @property {string} timestamp - Operation timestamp (ISO format)
 */

/**
 * @typedef {Object} GetObjectPolicyResponse
 * @property {string} key - Object key
 * @property {ReplicationPolicy} [replicationPolicy] - Current replication policy
 * @property {ArchivePolicy} [archivePolicy] - Current archive policy
 * @property {RetentionPolicy} [retentionPolicy] - Current retention policy
 * @property {string} timestamp - Query timestamp (ISO format)
 */

/**
 * @typedef {Object} ListBackendsOptions
 * @property {boolean} [includeHealth] - Include health status
 * @property {string} [filterByType] - Filter by backend type
 */

/**
 * @typedef {Object} BackendHealthInfo
 * @property {boolean} healthy - Whether backend is healthy
 * @property {number} latency - Last health check latency in ms
 * @property {string} lastCheck - Last check timestamp (ISO format)
 * @property {string} [error] - Error message if unhealthy
 */

/**
 * @typedef {Object} BackendInfo
 * @property {string} name - Backend name
 * @property {string} type - Backend type (s3, gcs, local, etc.)
 * @property {boolean} enabled - Whether backend is enabled
 * @property {boolean} default - Whether this is the default backend
 * @property {string} tier - Storage tier (hot, warm, cold)
 * @property {string} region - Geographic region
 * @property {number} objectCount - Number of objects
 * @property {number} totalSize - Total size in bytes
 * @property {BackendHealthInfo} [health] - Health status
 */

/**
 * @typedef {Object} ListBackendsResponse
 * @property {Array<BackendInfo>} backends - Backend information
 * @property {string} timestamp - Query timestamp (ISO format)
 */

/**
 * @typedef {Object} GetBackendHealthResponse
 * @property {string} name - Backend name
 * @property {boolean} healthy - Whether backend is healthy
 * @property {number} latency - Last health check latency in ms
 * @property {string} lastCheck - Last check timestamp (ISO format)
 * @property {string} status - Additional status information
 * @property {string} [error] - Error message if unhealthy
 */

/**
 * @typedef {Object} TenantUsageResponse
 * @property {string} tenantId - Tenant ID
 * @property {number} objectCount - Total number of objects
 * @property {number} totalSize - Total size in bytes
 * @property {number} activeCount - Number of active objects
 * @property {number} archivedCount - Number of archived objects
 * @property {string} timestamp - Query timestamp (ISO format)
 */

/**
 * @typedef {Object} TenantQuotasResponse
 * @property {string} tenantId - Tenant ID
 * @property {number} maxStorageBytes - Maximum storage quota (0 = unlimited)
 * @property {number} maxObjectSize - Maximum object size (0 = unlimited)
 * @property {number} maxObjectCount - Maximum object count (0 = unlimited)
 * @property {number} usedStorageBytes - Current storage usage
 * @property {number} usedObjectCount - Current object count
 * @property {string} timestamp - Query timestamp (ISO format)
 */

/**
 * Object Storage client for QuicBaaS.
 * Provides operations for storing, retrieving, and managing objects with
 * support for multi-backend storage, replication, archival, and tenant isolation.
 */
class ObjectStorageClient {
  /**
   * API path prefix for object storage operations.
   * @type {string}
   * @private
   */
  static API_PREFIX = '/api/v1/objects';

  /**
   * API path prefix for backend operations.
   * @type {string}
   * @private
   */
  static BACKENDS_PREFIX = '/api/v1/objects/backends';

  /**
   * API path prefix for tenant operations.
   * @type {string}
   * @private
   */
  static TENANTS_PREFIX = '/api/v1/objects/tenants';

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  /**
   * Create a new Object Storage client.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  // ==========================================================================
  // Object CRUD Operations
  // ==========================================================================

  /**
   * Store an object with specified metadata and policies.
   * @param {string} key - Object key in "namespace:path" format
   * @param {*} data - Object data to store
   * @param {PutObjectOptions} [options={}] - Put options
   * @returns {Promise<PutObjectResponse>} The put response
   * @throws {ValidationError} If required parameters are missing
   */
  async putObject(key, data, options = {}) {
    requiredString(key, 'key');
    required(data, 'data');

    this.#validatePutOptions(options);

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}`;
    const body = {
      value: data,
    };

    if (options.contentType) {
      body.contentType = options.contentType;
    }
    if (options.tenantId) {
      body.tenantId = options.tenantId;
    }
    if (options.tags) {
      body.tags = options.tags;
    }
    if (options.replicationPolicy) {
      body.replicationPolicy = options.replicationPolicy;
    }
    if (options.archivePolicy) {
      body.archivePolicy = options.archivePolicy;
    }
    if (options.retentionPolicy) {
      body.retentionPolicy = options.retentionPolicy;
    }
    if (options.targetBackend) {
      body.targetBackend = options.targetBackend;
    }
    if (options.checksum) {
      body.checksum = this.#encodeChecksum(options.checksum);
    }

    const response = await this.#transport.put(path, body);

    return {
      key: response?.key ?? key,
      objectId: response?.objectId ?? 0,
      checksum: response?.checksum ?? '',
      size: response?.size ?? 0,
      backend: response?.backend ?? '',
      version: response?.version ?? 1,
      createdAt: this.#formatTimestamp(response?.createdAt),
    };
  }

  /**
   * Retrieve an object by key.
   * @param {string} key - Object key to retrieve
   * @param {Object} [options={}] - Get options
   * @param {string} [options.tenantId] - Filter by tenant for access control
   * @param {boolean} [options.includeMetadata=true] - Include metadata in response
   * @returns {Promise<GetObjectResponse>} The object with data and metadata
   * @throws {ValidationError} If key is not provided
   */
  async getObject(key, options = {}) {
    requiredString(key, 'key');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}`;
    const query = {};

    if (options.tenantId) {
      query.tenant_id = options.tenantId;
    }
    if (options.includeMetadata !== undefined) {
      query.include_metadata = options.includeMetadata;
    }

    const response = await this.#transport.get(path, { query });

    return {
      key: response?.key ?? key,
      value: response?.value ?? null,
      contentType: response?.contentType ?? '',
      size: response?.size ?? 0,
      checksum: response?.checksum ?? '',
      version: response?.version ?? 0,
      tags: response?.tags ?? {},
      tenantId: response?.tenantId ?? '',
      createdAt: this.#formatTimestamp(response?.createdAt),
      modifiedAt: this.#formatTimestamp(response?.modifiedAt),
      backend: response?.backend ?? '',
    };
  }

  /**
   * Remove an object from storage.
   * @param {string} key - Object key to delete
   * @param {Object} [options={}] - Delete options
   * @param {string} [options.tenantId] - Filter by tenant for access control
   * @returns {Promise<DeleteObjectResponse>} The delete response
   * @throws {ValidationError} If key is not provided
   */
  async deleteObject(key, options = {}) {
    requiredString(key, 'key');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}`;
    const query = {};

    if (options.tenantId) {
      query.tenant_id = options.tenantId;
    }

    const response = await this.#transport.delete(path, { query });

    return {
      key: response?.key ?? key,
      success: response?.success ?? true,
      size: response?.size ?? 0,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  /**
   * Get object metadata without retrieving the data.
   * @param {string} key - Object key
   * @param {Object} [options={}] - Head options
   * @param {string} [options.tenantId] - Filter by tenant for access control
   * @returns {Promise<HeadObjectResponse>} Object metadata
   * @throws {ValidationError} If key is not provided
   */
  async headObject(key, options = {}) {
    requiredString(key, 'key');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}/head`;
    const query = {};

    if (options.tenantId) {
      query.tenant_id = options.tenantId;
    }

    const response = await this.#transport.get(path, { query });

    return {
      key: response?.key ?? key,
      exists: response?.exists ?? false,
      contentType: response?.contentType ?? '',
      size: response?.size ?? 0,
      checksum: response?.checksum ?? '',
      version: response?.version ?? 0,
      tags: response?.tags ?? {},
      tenantId: response?.tenantId ?? '',
      status: response?.status ?? ObjectStatus.ACTIVE,
      createdAt: this.#formatTimestamp(response?.createdAt),
      modifiedAt: this.#formatTimestamp(response?.modifiedAt),
      lastAccessAt: this.#formatTimestamp(response?.lastAccessAt),
      backend: response?.backend ?? '',
      replicaCount: response?.replicaCount ?? 0,
    };
  }

  // ==========================================================================
  // Listing Operations
  // ==========================================================================

  /**
   * List objects matching specified criteria.
   * @param {ListObjectsOptions} [options={}] - List options
   * @returns {Promise<ListObjectsResponse>} List of matching objects
   */
  async listObjects(options = {}) {
    this.#validateListOptions(options);

    const path = ObjectStorageClient.API_PREFIX;
    const query = {};

    if (options.prefix) {
      query.prefix = options.prefix;
    }
    if (options.tenantId) {
      query.tenant_id = options.tenantId;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor) {
      query.cursor = options.cursor;
    }
    if (options.includeTags !== undefined) {
      query.include_tags = options.includeTags;
    }

    const response = await this.#transport.get(path, { query });

    return {
      objects: this.#mapObjectSummaries(response?.objects),
      nextCursor: response?.nextCursor ?? null,
      hasMore: response?.hasMore ?? false,
      totalCount: response?.totalCount ?? 0,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  /**
   * List objects with a specific tag.
   * @param {string} tagName - Tag name to filter by
   * @param {string} tagValue - Tag value to match
   * @param {ListByTagOptions} [options={}] - List options
   * @returns {Promise<ListObjectsResponse>} List of matching objects
   * @throws {ValidationError} If tagName is not provided
   */
  async listByTag(tagName, tagValue, options = {}) {
    requiredString(tagName, 'tagName');

    this.#validateListOptions(options);

    const path = `${ObjectStorageClient.API_PREFIX}/by-tag`;
    const query = {
      tag_name: tagName,
    };

    if (tagValue !== undefined && tagValue !== null) {
      query.tag_value = tagValue;
    }
    if (options.tenantId) {
      query.tenant_id = options.tenantId;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor) {
      query.cursor = options.cursor;
    }

    const response = await this.#transport.get(path, { query });

    return {
      objects: this.#mapObjectSummaries(response?.objects),
      nextCursor: response?.nextCursor ?? null,
      hasMore: response?.hasMore ?? false,
      totalCount: response?.totalCount ?? 0,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  /**
   * List objects owned by a specific tenant.
   * @param {string} tenantId - Tenant ID to list objects for
   * @param {ListByTenantOptions} [options={}] - List options
   * @returns {Promise<ListObjectsResponse>} List of tenant's objects
   * @throws {ValidationError} If tenantId is not provided
   */
  async listByTenant(tenantId, options = {}) {
    requiredString(tenantId, 'tenantId');

    this.#validateListOptions(options);

    const path = `${ObjectStorageClient.API_PREFIX}/by-tenant/${encodeURIComponent(tenantId)}`;
    const query = {};

    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor) {
      query.cursor = options.cursor;
    }
    if (options.includeTags !== undefined) {
      query.include_tags = options.includeTags;
    }

    const response = await this.#transport.get(path, { query });

    return {
      objects: this.#mapObjectSummaries(response?.objects),
      nextCursor: response?.nextCursor ?? null,
      hasMore: response?.hasMore ?? false,
      totalCount: response?.totalCount ?? 0,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  // ==========================================================================
  // Copy Operations
  // ==========================================================================

  /**
   * Copy an object to a new location.
   * @param {string} sourceKey - Source object key
   * @param {string} destKey - Destination object key
   * @param {CopyObjectOptions} [options={}] - Copy options
   * @returns {Promise<CopyObjectResponse>} The copy response
   * @throws {ValidationError} If required parameters are missing
   */
  async copyObject(sourceKey, destKey, options = {}) {
    requiredString(sourceKey, 'sourceKey');
    requiredString(destKey, 'destKey');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(sourceKey)}/copy`;
    const body = {
      destinationKey: destKey,
    };

    if (options.tenantId) {
      body.tenantId = options.tenantId;
    }
    if (options.overwriteExisting !== undefined) {
      body.overwriteExisting = options.overwriteExisting;
    }
    if (options.targetBackend) {
      body.targetBackend = options.targetBackend;
    }

    const response = await this.#transport.post(path, body);

    return {
      key: response?.key ?? destKey,
      objectId: response?.objectId ?? 0,
      size: response?.size ?? 0,
      checksum: response?.checksum ?? '',
      version: response?.version ?? 1,
      backend: response?.backend ?? '',
      createdAt: this.#formatTimestamp(response?.createdAt),
    };
  }

  // ==========================================================================
  // Replication & Migration Operations
  // ==========================================================================

  /**
   * Replicate an object to target backends.
   * @param {string} key - Object key to replicate
   * @param {Array<string>} targets - Target backend names
   * @param {ReplicateObjectOptions} [options={}] - Replication options
   * @returns {Promise<ReplicateObjectResponse>} The replication response
   * @throws {ValidationError} If required parameters are missing
   */
  async replicateObject(key, targets, options = {}) {
    requiredString(key, 'key');
    nonEmptyArray(targets, 'targets');

    this.#validateReplicateOptions(options);

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}/replicate`;
    const body = {
      targets,
    };

    if (options.tenantId) {
      body.tenantId = options.tenantId;
    }
    if (options.priority !== undefined) {
      body.priority = options.priority;
    }
    if (options.synchronous !== undefined) {
      body.synchronous = options.synchronous;
    }

    const response = await this.#transport.post(path, body);

    return {
      key: response?.key ?? key,
      status: response?.status ?? '',
      results: this.#mapReplicationResults(response?.results),
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  /**
   * Migrate an object to a new backend.
   * @param {string} key - Object key to migrate
   * @param {string} targetBackend - Destination backend
   * @param {MigrateObjectOptions} [options={}] - Migration options
   * @returns {Promise<MigrateObjectResponse>} The migration response
   * @throws {ValidationError} If required parameters are missing
   */
  async migrateObject(key, targetBackend, options = {}) {
    requiredString(key, 'key');
    requiredString(targetBackend, 'targetBackend');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}/migrate`;
    const body = {
      targetBackend,
    };

    if (options.tenantId) {
      body.tenantId = options.tenantId;
    }
    if (options.deleteFromSource !== undefined) {
      body.deleteFromSource = options.deleteFromSource;
    }

    const response = await this.#transport.post(path, body);

    return {
      key: response?.key ?? key,
      sourceBackend: response?.sourceBackend ?? '',
      targetBackend: response?.targetBackend ?? targetBackend,
      success: response?.success ?? true,
      deletedFromSource: response?.deletedFromSource ?? false,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  // ==========================================================================
  // Archive & Restore Operations
  // ==========================================================================

  /**
   * Move an object to archive storage.
   * @param {string} key - Object key to archive
   * @param {string} archiveStore - Archive backend name
   * @param {ArchiveObjectOptions} [options={}] - Archive options
   * @returns {Promise<ArchiveObjectResponse>} The archive response
   * @throws {ValidationError} If required parameters are missing
   */
  async archiveObject(key, archiveStore, options = {}) {
    requiredString(key, 'key');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}/archive`;
    const body = {};

    if (archiveStore) {
      body.targetStore = archiveStore;
    }
    if (options.tenantId) {
      body.tenantId = options.tenantId;
    }
    if (options.deleteFromPrimary !== undefined) {
      body.deleteFromPrimary = options.deleteFromPrimary;
    }

    const response = await this.#transport.post(path, body);

    return {
      key: response?.key ?? key,
      archiveStore: response?.archiveStore ?? archiveStore ?? '',
      success: response?.success ?? true,
      deletedFromPrimary: response?.deletedFromPrimary ?? false,
      archivedAt: this.#formatTimestamp(response?.archivedAt),
    };
  }

  /**
   * Restore an archived object to active storage.
   * @param {string} key - Object key to restore
   * @param {RestoreObjectOptions} [options={}] - Restore options
   * @returns {Promise<RestoreObjectResponse>} The restore response
   * @throws {ValidationError} If key is not provided
   */
  async restoreObject(key, options = {}) {
    requiredString(key, 'key');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}/restore`;
    const body = {};

    if (options.tenantId) {
      body.tenantId = options.tenantId;
    }
    if (options.targetBackend) {
      body.targetBackend = options.targetBackend;
    }

    const response = await this.#transport.post(path, body);

    return {
      key: response?.key ?? key,
      backend: response?.backend ?? '',
      success: response?.success ?? true,
      restoredAt: this.#formatTimestamp(response?.restoredAt),
    };
  }

  // ==========================================================================
  // Verification Operations
  // ==========================================================================

  /**
   * Verify object integrity across storage backends.
   * @param {string} key - Object key to verify
   * @param {VerifyObjectOptions} [options={}] - Verification options
   * @returns {Promise<VerifyObjectResponse>} The verification response
   * @throws {ValidationError} If key is not provided
   */
  async verifyObject(key, options = {}) {
    requiredString(key, 'key');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}/verify`;
    const body = {};

    if (options.stores && options.stores.length > 0) {
      body.stores = options.stores;
    }
    if (options.tenantId) {
      body.tenantId = options.tenantId;
    }
    if (options.repairMismatches !== undefined) {
      body.repairMismatches = options.repairMismatches;
    }

    const response = await this.#transport.post(path, body);

    return {
      key: response?.key ?? key,
      valid: response?.valid ?? false,
      results: this.#mapVerificationResults(response?.results),
      repaired: response?.repaired ?? false,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  // ==========================================================================
  // Policy Management Operations
  // ==========================================================================

  /**
   * Set lifecycle/replication policies for an object.
   * @param {string} key - Object key
   * @param {ObjectPolicy} policy - Policy settings
   * @param {Object} [options={}] - Options
   * @param {string} [options.tenantId] - For access control
   * @returns {Promise<SetObjectPolicyResponse>} The response
   * @throws {ValidationError} If key is not provided
   */
  async setObjectPolicy(key, policy, options = {}) {
    requiredString(key, 'key');
    required(policy, 'policy');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}/policy`;
    const body = { ...policy };

    if (options.tenantId) {
      body.tenantId = options.tenantId;
    }

    const response = await this.#transport.put(path, body);

    return {
      key: response?.key ?? key,
      success: response?.success ?? true,
      version: response?.version ?? 0,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  /**
   * Get policies for an object.
   * @param {string} key - Object key
   * @param {Object} [options={}] - Options
   * @param {string} [options.tenantId] - For access control
   * @returns {Promise<GetObjectPolicyResponse>} The object policies
   * @throws {ValidationError} If key is not provided
   */
  async getObjectPolicy(key, options = {}) {
    requiredString(key, 'key');

    const path = `${ObjectStorageClient.API_PREFIX}/${encodeURIComponent(key)}/policy`;
    const query = {};

    if (options.tenantId) {
      query.tenant_id = options.tenantId;
    }

    const response = await this.#transport.get(path, { query });

    return {
      key: response?.key ?? key,
      replicationPolicy: response?.replicationPolicy ?? null,
      archivePolicy: response?.archivePolicy ?? null,
      retentionPolicy: response?.retentionPolicy ?? null,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  // ==========================================================================
  // Backend Management Operations
  // ==========================================================================

  /**
   * List available storage backends.
   * @param {ListBackendsOptions} [options={}] - List options
   * @returns {Promise<ListBackendsResponse>} List of backends
   */
  async listBackends(options = {}) {
    const path = ObjectStorageClient.BACKENDS_PREFIX;
    const query = {};

    if (options.includeHealth !== undefined) {
      query.include_health = options.includeHealth;
    }
    if (options.filterByType) {
      query.filter_by_type = options.filterByType;
    }

    const response = await this.#transport.get(path, { query });

    return {
      backends: this.#mapBackendInfos(response?.backends),
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  /**
   * Get health status for a storage backend.
   * @param {string} backendName - Backend name
   * @returns {Promise<GetBackendHealthResponse>} Backend health status
   * @throws {ValidationError} If backendName is not provided
   */
  async getBackendHealth(backendName) {
    requiredString(backendName, 'backendName');

    const path = `${ObjectStorageClient.BACKENDS_PREFIX}/${encodeURIComponent(backendName)}/health`;

    const response = await this.#transport.get(path);

    return {
      name: response?.name ?? backendName,
      healthy: response?.healthy ?? false,
      latency: response?.latency ?? 0,
      lastCheck: this.#formatTimestamp(response?.lastCheck),
      status: response?.status ?? '',
      error: response?.error ?? null,
    };
  }

  // ==========================================================================
  // Tenant Management Operations
  // ==========================================================================

  /**
   * Get storage usage statistics for a tenant.
   * @param {string} tenantId - Tenant ID
   * @returns {Promise<TenantUsageResponse>} Tenant usage statistics
   * @throws {ValidationError} If tenantId is not provided
   */
  async getTenantUsage(tenantId) {
    requiredString(tenantId, 'tenantId');

    const path = `${ObjectStorageClient.TENANTS_PREFIX}/${encodeURIComponent(tenantId)}/usage`;

    const response = await this.#transport.get(path);

    return {
      tenantId: response?.tenantId ?? tenantId,
      objectCount: response?.objectCount ?? 0,
      totalSize: response?.totalSize ?? 0,
      activeCount: response?.activeCount ?? 0,
      archivedCount: response?.archivedCount ?? 0,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  /**
   * Get quota settings for a tenant.
   * @param {string} tenantId - Tenant ID
   * @returns {Promise<TenantQuotasResponse>} Tenant quota settings
   * @throws {ValidationError} If tenantId is not provided
   */
  async getTenantQuotas(tenantId) {
    requiredString(tenantId, 'tenantId');

    const path = `${ObjectStorageClient.TENANTS_PREFIX}/${encodeURIComponent(tenantId)}/quotas`;

    const response = await this.#transport.get(path);

    return {
      tenantId: response?.tenantId ?? tenantId,
      maxStorageBytes: response?.maxStorageBytes ?? 0,
      maxObjectSize: response?.maxObjectSize ?? 0,
      maxObjectCount: response?.maxObjectCount ?? 0,
      usedStorageBytes: response?.usedStorageBytes ?? 0,
      usedObjectCount: response?.usedObjectCount ?? 0,
      timestamp: this.#formatTimestamp(response?.timestamp),
    };
  }

  // ==========================================================================
  // Private Helper Methods
  // ==========================================================================

  /**
   * Validate put options.
   * @param {PutObjectOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validatePutOptions(options) {
    if (options.tags !== undefined && options.tags !== null) {
      if (typeof options.tags !== 'object' || Array.isArray(options.tags)) {
        throw new ValidationError('options.tags must be an object', 'options.tags');
      }
    }
    if (options.replicationPolicy) {
      this.#validateReplicationPolicy(options.replicationPolicy);
    }
  }

  /**
   * Validate replication policy.
   * @param {ReplicationPolicy} policy - Policy to validate
   * @throws {ValidationError} If policy is invalid
   * @private
   */
  #validateReplicationPolicy(policy) {
    if (policy.mode !== undefined) {
      oneOf(policy.mode, Object.values(ReplicationMode), 'replicationPolicy.mode');
    }
    if (policy.priority !== undefined) {
      if (typeof policy.priority !== 'number' || policy.priority < 1 || policy.priority > 10) {
        throw new ValidationError(
          'replicationPolicy.priority must be between 1 and 10',
          'replicationPolicy.priority',
        );
      }
    }
  }

  /**
   * Validate list options.
   * @param {Object} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListOptions(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
  }

  /**
   * Validate replicate options.
   * @param {ReplicateObjectOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateReplicateOptions(options) {
    if (options.priority !== undefined) {
      if (typeof options.priority !== 'number' || options.priority < 1 || options.priority > 10) {
        throw new ValidationError(
          'options.priority must be between 1 and 10',
          'options.priority',
        );
      }
    }
  }

  /**
   * Encode a checksum to base64.
   * @param {Uint8Array|Array<number>} checksum - Checksum bytes
   * @returns {string} Base64 encoded checksum
   * @private
   */
  #encodeChecksum(checksum) {
    if (checksum instanceof Uint8Array) {
      return btoa(String.fromCharCode.apply(null, checksum));
    }
    if (Array.isArray(checksum)) {
      return btoa(String.fromCharCode.apply(null, checksum));
    }
    return checksum;
  }

  /**
   * Format a Unix nanosecond timestamp to ISO string.
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
   * Map object summaries from response.
   * @param {Array} objects - Raw object summaries
   * @returns {Array<ObjectSummary>} Mapped object summaries
   * @private
   */
  #mapObjectSummaries(objects) {
    if (!objects || !Array.isArray(objects)) {
      return [];
    }
    return objects.map(obj => ({
      key: obj.key ?? '',
      size: obj.size ?? 0,
      contentType: obj.contentType ?? '',
      version: obj.version ?? 0,
      status: obj.status ?? ObjectStatus.ACTIVE,
      tags: obj.tags ?? undefined,
      createdAt: this.#formatTimestamp(obj.createdAt),
      modifiedAt: this.#formatTimestamp(obj.modifiedAt),
    }));
  }

  /**
   * Map replication results from response.
   * @param {Object} results - Raw replication results
   * @returns {Object.<string, ReplicationResult>} Mapped results
   * @private
   */
  #mapReplicationResults(results) {
    if (!results || typeof results !== 'object') {
      return {};
    }
    const mapped = {};
    for (const [target, result] of Object.entries(results)) {
      mapped[target] = {
        target: result.target ?? target,
        success: result.success ?? false,
        error: result.error ?? null,
        duration: result.duration ?? 0,
      };
    }
    return mapped;
  }

  /**
   * Map verification results from response.
   * @param {Object} results - Raw verification results
   * @returns {Object.<string, VerificationResult>} Mapped results
   * @private
   */
  #mapVerificationResults(results) {
    if (!results || typeof results !== 'object') {
      return {};
    }
    const mapped = {};
    for (const [store, result] of Object.entries(results)) {
      mapped[store] = {
        store: result.store ?? store,
        valid: result.valid ?? false,
        checksumMatch: result.checksumMatch ?? false,
        sizeMatch: result.sizeMatch ?? false,
        error: result.error ?? null,
      };
    }
    return mapped;
  }

  /**
   * Map backend info array from response.
   * @param {Array} backends - Raw backend info array
   * @returns {Array<BackendInfo>} Mapped backend infos
   * @private
   */
  #mapBackendInfos(backends) {
    if (!backends || !Array.isArray(backends)) {
      return [];
    }
    return backends.map(backend => ({
      name: backend.name ?? '',
      type: backend.type ?? '',
      enabled: backend.enabled ?? false,
      default: backend.default ?? false,
      tier: backend.tier ?? StorageTier.HOT,
      region: backend.region ?? '',
      objectCount: backend.objectCount ?? 0,
      totalSize: backend.totalSize ?? 0,
      health: backend.health ? {
        healthy: backend.health.healthy ?? false,
        latency: backend.health.latency ?? 0,
        lastCheck: this.#formatTimestamp(backend.health.lastCheck),
        error: backend.health.error ?? null,
      } : undefined,
    }));
  }
}

export default ObjectStorageClient;
