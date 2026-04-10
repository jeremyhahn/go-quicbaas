/**
 * QuicBaaS JavaScript SDK - Cluster DEK (Data Encryption Key) Client
 *
 * Provides cluster-wide data encryption key management operations including
 * initialization, rotation, node synchronization, re-encryption, and recovery.
 *
 * @module @quicbaas/client/clients/clusterDek
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  requiredObject,
  positiveInteger,
  nonNegativeInteger,
  oneOf,
  requiredArray,
} from '../utils/validation.js';

/**
 * API base path for cluster DEK endpoints.
 * @constant {string}
 */
const CLUSTER_DEK_API_PATH = '/api/v1/cluster-dek';

/**
 * Valid key sizes for DEK initialization.
 * @constant {number[]}
 */
const VALID_KEY_SIZES = [128, 192, 256, 384, 512];

/**
 * Node DEK synchronization status values.
 * @readonly
 * @enum {string}
 */
export const NodeDEKSyncStatus = Object.freeze({
  /** Node has the current DEK version */
  SYNCED: 'synced',
  /** Node has an older DEK version */
  OUT_OF_DATE: 'out_of_date',
  /** Node has no DEK */
  MISSING: 'missing',
  /** DEK sync attempt failed */
  SYNC_FAILED: 'sync_failed',
});

/**
 * Re-encryption operation state values.
 * @readonly
 * @enum {string}
 */
export const ReencryptionState = Object.freeze({
  /** Re-encryption is in progress */
  RUNNING: 'running',
  /** Re-encryption is paused */
  PAUSED: 'paused',
  /** Re-encryption completed successfully */
  COMPLETED: 'completed',
  /** Re-encryption failed */
  FAILED: 'failed',
  /** Re-encryption was cancelled */
  CANCELLED: 'cancelled',
});

/**
 * DEK recovery method values.
 * @readonly
 * @enum {string}
 */
export const RecoveryMethod = Object.freeze({
  /** Recover using Shamir-split recovery keys */
  RECOVERY_KEYS: 'recovery_keys',
  /** Recover from encrypted backup */
  BACKUP: 'backup',
  /** Recover using manually injected key (break-glass) */
  MANUAL: 'manual',
});

/**
 * @typedef {Object} DEKRotationPolicy
 * @property {boolean} enabled - Whether automatic rotation is enabled
 * @property {number} rotationInterval - Time between rotations in milliseconds
 * @property {boolean} autoReencrypt - Automatically start re-encryption after rotation
 */

/**
 * @typedef {Object} InitClusterDEKRequest
 * @property {string} algorithm - Encryption algorithm (e.g., 'AES-256-GCM')
 * @property {number} keySize - Key size in bits (128, 192, 256, 384, or 512)
 * @property {Object} [metadata] - Metadata for audit and tracking
 * @property {DEKRotationPolicy} [rotationPolicy] - Automatic rotation settings
 */

/**
 * @typedef {Object} InitClusterDEKResponse
 * @property {number} version - Initial DEK version (typically 1)
 * @property {string} createdAt - ISO timestamp when DEK was created
 * @property {number} nodesSynced - Count of nodes that have the DEK
 * @property {string} algorithm - Algorithm used for the DEK
 * @property {number} keySize - Key size in bits
 */

/**
 * @typedef {Object} NodeDEKInfo
 * @property {number} nodeId - Node identifier
 * @property {string} [nodeName] - Human-readable node name
 * @property {number} currentVersion - DEK version on this node
 * @property {string} lastSyncedAt - ISO timestamp of last sync
 */

/**
 * @typedef {Object} DEKVersionInfo
 * @property {number} version - Version number
 * @property {string} createdAt - ISO timestamp when version was created
 * @property {string} [deprecatedAt] - ISO timestamp when version was superseded
 * @property {number} entitiesEncrypted - Count of entities using this version
 * @property {boolean} canDelete - Whether this version can be safely removed
 */

/**
 * @typedef {Object} ClusterDEKStatusResponse
 * @property {boolean} initialized - Whether cluster DEK has been set up
 * @property {number} currentVersion - Active DEK version
 * @property {string} createdAt - ISO timestamp when current DEK was created
 * @property {string} [rotatedAt] - ISO timestamp of last rotation
 * @property {string} [nextRotation] - Scheduled rotation time
 * @property {number} nodesSynced - Count of nodes with current DEK
 * @property {number} totalNodes - Total nodes in cluster
 * @property {NodeDEKInfo[]} [nodesOutOfSync] - Nodes missing current DEK
 * @property {string} algorithm - Algorithm for current DEK
 * @property {number} keySize - Key size in bits
 * @property {DEKVersionInfo[]} [previousVersions] - Historical DEK versions still in use
 * @property {boolean} reencryptionInProgress - Whether re-encryption is running
 * @property {number} [reencryptionProgress] - Completion percentage (0-100)
 */

/**
 * @typedef {Object} RotateClusterDEKRequest
 * @property {string} reason - Reason for rotation (audit trail)
 * @property {string} [scheduledTime] - ISO timestamp for scheduled rotation (null = immediate)
 * @property {boolean} [forceRotation] - Skip safety checks
 * @property {boolean} [autoStartReencryption] - Start re-encryption automatically
 * @property {Object} [metadata] - Metadata for audit and tracking
 */

/**
 * @typedef {Object} RotateClusterDEKResponse
 * @property {number} newVersion - New DEK version
 * @property {number} oldVersion - Previous DEK version
 * @property {string} rotatedAt - ISO timestamp of rotation
 * @property {string} rotationId - Tracking ID for this rotation
 * @property {number} nodesSynced - Count of nodes with new DEK
 * @property {boolean} reencryptionStarted - Whether re-encryption was initiated
 * @property {number} [estimatedReencryptionTime] - Estimated completion duration in milliseconds
 */

/**
 * @typedef {Object} SyncNodeDEKOptions
 * @property {number} [currentVersion] - DEK version node currently has (0 if none)
 * @property {boolean} [forceResync] - Force sync even if version matches
 */

/**
 * @typedef {Object} SyncNodeDEKResponse
 * @property {number} nodeId - Node that was synced
 * @property {number[]} versionsSynced - All DEK versions provided to node
 * @property {number} currentVersion - Active DEK version
 * @property {string} syncedAt - ISO timestamp of sync completion
 */

/**
 * @typedef {Object} NodeDEKStatusResponse
 * @property {number} nodeId - Node identifier
 * @property {string} [nodeName] - Human-readable node name
 * @property {number} currentVersion - DEK version on this node
 * @property {string} lastSyncedAt - ISO timestamp of last sync
 * @property {string} status - Sync state (from NodeDEKSyncStatus)
 * @property {string} [error] - Error message if sync failed
 * @property {number[]} versionsAvailable - All DEK versions node has
 */

/**
 * @typedef {Object} StartReencryptionRequest
 * @property {number} fromVersion - Old DEK version to re-encrypt from
 * @property {number} toVersion - New DEK version to re-encrypt to
 * @property {string} [path] - DEK path to re-encrypt (empty = all DEKs)
 * @property {number} [batchSize] - Entities to process per batch
 * @property {number} [concurrency] - Parallel re-encryption workers
 * @property {number} [throttleRate] - Rate limit (entities/sec, 0 = unlimited)
 * @property {boolean} [force] - Force re-encryption even if already in progress
 */

/**
 * @typedef {Object} StartReencryptionResponse
 * @property {string} operationId - Tracking ID for this re-encryption
 * @property {string} [path] - DEK path being re-encrypted
 * @property {boolean} started - Whether re-encryption was started
 * @property {number} estimatedItems - Estimated items to process
 * @property {string} timestamp - ISO timestamp when operation started
 * @property {string} startedAt - ISO timestamp when re-encryption began
 * @property {number} totalEntities - Estimated total entities to re-encrypt
 * @property {string} estimatedCompletion - Estimated completion time
 */

/**
 * @typedef {Object} NodeReencryptionProgress
 * @property {number} nodeId - Node identifier
 * @property {string} [nodeName] - Human-readable node name
 * @property {number} progress - Completion percentage (0-100)
 * @property {number} processedEntities - Entities processed on this node
 * @property {number} totalEntities - Total entities on this node
 * @property {number} throughputRate - Entities/second for this node
 * @property {string} lastUpdatedAt - ISO timestamp of last progress update
 */

/**
 * @typedef {Object} ReencryptionError
 * @property {string} timestamp - ISO timestamp when error occurred
 * @property {number} nodeId - Node where error occurred
 * @property {string} [entityId] - Entity that failed
 * @property {string} error - Error message
 * @property {boolean} retried - Whether operation was retried
 */

/**
 * @typedef {Object} ReencryptionStatusResponse
 * @property {string} operationId - Re-encryption operation ID
 * @property {string} state - Operation state (from ReencryptionState)
 * @property {string} startedAt - ISO timestamp when operation started
 * @property {string} [completedAt] - ISO timestamp when operation finished
 * @property {string} [estimatedCompletion] - Estimated finish time
 * @property {number} progress - Completion percentage (0-100)
 * @property {number} totalEntities - Total entities to re-encrypt
 * @property {number} processedEntities - Entities already re-encrypted
 * @property {number} failedEntities - Count of failures
 * @property {number} throughputRate - Current entities/second
 * @property {NodeReencryptionProgress[]} [nodeProgress] - Per-node progress
 * @property {ReencryptionError[]} [errors] - Recent errors (limited to last 100)
 */

/**
 * @typedef {Object} RecoverDEKRequest
 * @property {string} recoveryMethod - Recovery method (from RecoveryMethod)
 * @property {string} reason - Reason for recovery (audit trail)
 * @property {string[]} [recoveryKeys] - Base64-encoded Shamir recovery keys
 * @property {string} [backupLocation] - Path/URL to encrypted DEK backup
 * @property {string} [manualKey] - Base64-encoded manually injected key
 */

/**
 * @typedef {Object} RecoverDEKResponse
 * @property {boolean} success - Whether recovery succeeded
 * @property {number} recoveredVersion - DEK version recovered
 * @property {string} recoveredAt - ISO timestamp of recovery
 * @property {number} nodesSynced - Count of nodes that received recovered DEK
 * @property {string} auditId - Tracking ID for this recovery operation
 * @property {string[]} [warnings] - Issues encountered during recovery
 */

/**
 * Cluster DEK (Data Encryption Key) client for QuicBaaS.
 *
 * Provides cluster-wide data encryption key management including initialization,
 * rotation, node synchronization, re-encryption, and recovery operations.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import ClusterDEKClient, { RecoveryMethod } from '@quicbaas/client/clients/clusterDek';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * transport.setAuthToken('your-auth-token');
 * const clusterDek = new ClusterDEKClient(transport);
 *
 * // Initialize cluster DEK
 * const init = await clusterDek.initializeClusterDEK({
 *   algorithm: 'AES-256-GCM',
 *   keySize: 256
 * });
 * console.log('DEK version:', init.version);
 *
 * // Check cluster DEK status
 * const status = await clusterDek.getClusterDEKStatus();
 * console.log('Nodes synced:', status.nodesSynced);
 *
 * // Rotate to new DEK version
 * const rotation = await clusterDek.rotateClusterDEK({
 *   reason: 'Scheduled quarterly rotation',
 *   autoStartReencryption: true
 * });
 * console.log('New version:', rotation.newVersion);
 */
export class ClusterDEKClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new ClusterDEKClient instance.
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
   * Initialize the cluster DEK.
   *
   * Creates and distributes the initial cluster data encryption key.
   * This should be called once when setting up a new cluster.
   *
   * @param {InitClusterDEKRequest} request - Initialization request
   * @returns {Promise<InitClusterDEKResponse>} Initialization result
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const result = await clusterDek.initializeClusterDEK({
   *   algorithm: 'AES-256-GCM',
   *   keySize: 256,
   *   metadata: { environment: 'production' },
   *   rotationPolicy: {
   *     enabled: true,
   *     rotationInterval: 90 * 24 * 60 * 60 * 1000, // 90 days
   *     autoReencrypt: true
   *   }
   * });
   */
  async initializeClusterDEK(request) {
    this.#validateInitClusterDEKRequest(request);

    const body = {
      algorithm: request.algorithm,
      key_size: request.keySize,
    };

    if (request.metadata !== undefined) {
      body.metadata = request.metadata;
    }

    if (request.rotationPolicy !== undefined) {
      body.rotation_policy = {
        enabled: request.rotationPolicy.enabled,
        rotation_interval: request.rotationPolicy.rotationInterval,
        auto_reencrypt: request.rotationPolicy.autoReencrypt,
      };
    }

    const response = await this.#transport.post(
      `${CLUSTER_DEK_API_PATH}/initialize`,
      body
    );

    return this.#transformInitClusterDEKResponse(response);
  }

  /**
   * Get the current cluster DEK status.
   *
   * Retrieves comprehensive status information about the cluster DEK
   * including version, node sync status, and re-encryption progress.
   *
   * @returns {Promise<ClusterDEKStatusResponse>} Current DEK status
   *
   * @example
   * const status = await clusterDek.getClusterDEKStatus();
   * if (status.initialized) {
   *   console.log(`DEK version: ${status.currentVersion}`);
   *   console.log(`Nodes synced: ${status.nodesSynced}/${status.totalNodes}`);
   *   if (status.reencryptionInProgress) {
   *     console.log(`Re-encryption: ${status.reencryptionProgress}%`);
   *   }
   * }
   */
  async getClusterDEKStatus() {
    const response = await this.#transport.get(`${CLUSTER_DEK_API_PATH}/status`);

    return this.#transformClusterDEKStatusResponse(response);
  }

  /**
   * Rotate the cluster DEK to a new version.
   *
   * Generates a new cluster DEK version and optionally initiates
   * re-encryption of existing data.
   *
   * @param {RotateClusterDEKRequest} request - Rotation request
   * @returns {Promise<RotateClusterDEKResponse>} Rotation result
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * // Immediate rotation with auto re-encryption
   * const result = await clusterDek.rotateClusterDEK({
   *   reason: 'Security incident response',
   *   autoStartReencryption: true,
   *   metadata: { incident_id: 'INC-12345' }
   * });
   *
   * // Scheduled rotation
   * const scheduledResult = await clusterDek.rotateClusterDEK({
   *   reason: 'Quarterly rotation',
   *   scheduledTime: new Date(Date.now() + 24 * 60 * 60 * 1000).toISOString()
   * });
   */
  async rotateClusterDEK(request) {
    this.#validateRotateClusterDEKRequest(request);

    const body = {
      reason: request.reason,
    };

    if (request.scheduledTime !== undefined) {
      body.scheduled_time = request.scheduledTime;
    }

    if (request.forceRotation !== undefined) {
      body.force_rotation = request.forceRotation;
    }

    if (request.autoStartReencryption !== undefined) {
      body.auto_start_reencryption = request.autoStartReencryption;
    }

    if (request.metadata !== undefined) {
      body.metadata = request.metadata;
    }

    const response = await this.#transport.post(
      `${CLUSTER_DEK_API_PATH}/rotate`,
      body
    );

    return this.#transformRotateClusterDEKResponse(response);
  }

  /**
   * Synchronize the cluster DEK to a specific node.
   *
   * Ensures a node has the current DEK version and any required
   * historical versions for data access.
   *
   * @param {number} nodeId - Node identifier to sync
   * @param {SyncNodeDEKOptions} [options={}] - Sync options
   * @returns {Promise<SyncNodeDEKResponse>} Sync result
   * @throws {ValidationError} If nodeId is invalid
   *
   * @example
   * // Sync DEK to a new node
   * const result = await clusterDek.syncNodeDEK(3, {
   *   currentVersion: 0 // Node has no DEK
   * });
   * console.log(`Synced versions: ${result.versionsSynced.join(', ')}`);
   *
   * // Force resync
   * const forceResult = await clusterDek.syncNodeDEK(3, {
   *   forceResync: true
   * });
   */
  async syncNodeDEK(nodeId, options = {}) {
    this.#validateNodeId(nodeId, 'nodeId');
    this.#validateSyncNodeDEKOptions(options);

    const body = {
      node_id: nodeId,
    };

    if (options.currentVersion !== undefined) {
      body.current_version = options.currentVersion;
    }

    if (options.forceResync !== undefined) {
      body.force_resync = options.forceResync;
    }

    const response = await this.#transport.post(
      `${CLUSTER_DEK_API_PATH}/nodes/${nodeId}/sync`,
      body
    );

    return this.#transformSyncNodeDEKResponse(response);
  }

  /**
   * Get the DEK status for a specific node.
   *
   * Retrieves DEK synchronization status and version information
   * for a specific cluster node.
   *
   * @param {number} nodeId - Node identifier
   * @returns {Promise<NodeDEKStatusResponse>} Node DEK status
   * @throws {ValidationError} If nodeId is invalid
   * @throws {NotFoundError} If node is not found
   *
   * @example
   * const nodeStatus = await clusterDek.getNodeDEKStatus(3);
   * if (nodeStatus.status === NodeDEKSyncStatus.OUT_OF_DATE) {
   *   console.log(`Node ${nodeStatus.nodeId} needs sync`);
   *   await clusterDek.syncNodeDEK(nodeStatus.nodeId);
   * }
   */
  async getNodeDEKStatus(nodeId) {
    this.#validateNodeId(nodeId, 'nodeId');

    const response = await this.#transport.get(
      `${CLUSTER_DEK_API_PATH}/nodes/${nodeId}/status`
    );

    return this.#transformNodeDEKStatusResponse(response);
  }

  /**
   * Start re-encryption of data with a new DEK.
   *
   * Manually initiates the re-encryption process to migrate data
   * from an old DEK version to a new version.
   *
   * @param {StartReencryptionRequest} request - Re-encryption request
   * @returns {Promise<StartReencryptionResponse>} Re-encryption start result
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const result = await clusterDek.startReencryption({
   *   fromVersion: 1,
   *   toVersion: 2,
   *   batchSize: 1000,
   *   concurrency: 4,
   *   throttleRate: 5000 // 5000 entities/sec
   * });
   * console.log(`Operation ${result.operationId} started`);
   * console.log(`Estimated entities: ${result.totalEntities}`);
   */
  async startReencryption(request) {
    this.#validateStartReencryptionRequest(request);

    const body = {
      from_version: request.fromVersion,
      to_version: request.toVersion,
    };

    if (request.path !== undefined) {
      body.path = request.path;
    }

    if (request.batchSize !== undefined) {
      body.batch_size = request.batchSize;
    }

    if (request.concurrency !== undefined) {
      body.concurrency = request.concurrency;
    }

    if (request.throttleRate !== undefined) {
      body.throttle_rate = request.throttleRate;
    }

    if (request.force !== undefined) {
      body.force = request.force;
    }

    const response = await this.#transport.post(
      `${CLUSTER_DEK_API_PATH}/reencryption/start`,
      body
    );

    return this.#transformStartReencryptionResponse(response);
  }

  /**
   * Get the current re-encryption status.
   *
   * Retrieves progress information for any ongoing re-encryption
   * operation including per-node progress and error details.
   *
   * @returns {Promise<ReencryptionStatusResponse>} Re-encryption status
   * @throws {NotFoundError} If no re-encryption is in progress
   *
   * @example
   * const status = await clusterDek.getReencryptionStatus();
   * console.log(`State: ${status.state}`);
   * console.log(`Progress: ${status.progress}%`);
   * console.log(`Processed: ${status.processedEntities}/${status.totalEntities}`);
   * console.log(`Throughput: ${status.throughputRate} entities/sec`);
   */
  async getReencryptionStatus() {
    const response = await this.#transport.get(
      `${CLUSTER_DEK_API_PATH}/reencryption/status`
    );

    return this.#transformReencryptionStatusResponse(response);
  }

  /**
   * Cancel an ongoing re-encryption operation.
   *
   * Stops the current re-encryption process. Data that has already
   * been re-encrypted will remain with the new DEK version.
   *
   * @returns {Promise<void>}
   *
   * @example
   * await clusterDek.cancelReencryption();
   * console.log('Re-encryption cancelled');
   *
   * // Check final status
   * const status = await clusterDek.getReencryptionStatus();
   * console.log(`Final state: ${status.state}`);
   */
  async cancelReencryption() {
    await this.#transport.post(
      `${CLUSTER_DEK_API_PATH}/reencryption/cancel`,
      {}
    );
  }

  /**
   * Recover the cluster DEK using recovery keys or backup.
   *
   * Restores the cluster DEK in disaster recovery scenarios using
   * one of the supported recovery methods.
   *
   * @param {RecoverDEKRequest} request - Recovery request
   * @returns {Promise<RecoverDEKResponse>} Recovery result
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * // Recover using Shamir recovery keys
   * const result = await clusterDek.recoverDEK({
   *   recoveryMethod: RecoveryMethod.RECOVERY_KEYS,
   *   reason: 'Disaster recovery after cluster failure',
   *   recoveryKeys: [
   *     'base64-encoded-key-share-1',
   *     'base64-encoded-key-share-2',
   *     'base64-encoded-key-share-3'
   *   ]
   * });
   *
   * // Recover from backup
   * const backupResult = await clusterDek.recoverDEK({
   *   recoveryMethod: RecoveryMethod.BACKUP,
   *   reason: 'Restore from backup',
   *   backupLocation: 's3://backups/dek-backup-20250115.enc'
   * });
   */
  async recoverDEK(request) {
    this.#validateRecoverDEKRequest(request);

    const body = {
      recovery_method: request.recoveryMethod,
      reason: request.reason,
    };

    if (request.recoveryKeys !== undefined) {
      body.recovery_keys = request.recoveryKeys;
    }

    if (request.backupLocation !== undefined) {
      body.backup_location = request.backupLocation;
    }

    if (request.manualKey !== undefined) {
      body.manual_key = request.manualKey;
    }

    const response = await this.#transport.post(
      `${CLUSTER_DEK_API_PATH}/recover`,
      body
    );

    return this.#transformRecoverDEKResponse(response);
  }

  // =========================================================================
  // Validation Methods
  // =========================================================================

  /**
   * Validate initialize cluster DEK request.
   *
   * @param {InitClusterDEKRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateInitClusterDEKRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.algorithm, 'request.algorithm');
    required(request.keySize, 'request.keySize');
    positiveInteger(request.keySize, 'request.keySize');

    if (!VALID_KEY_SIZES.includes(request.keySize)) {
      throw new ValidationError(
        `request.keySize must be one of: ${VALID_KEY_SIZES.join(', ')}`,
        'request.keySize'
      );
    }

    if (request.metadata !== undefined) {
      if (typeof request.metadata !== 'object' || Array.isArray(request.metadata)) {
        throw new ValidationError(
          'request.metadata must be an object',
          'request.metadata'
        );
      }
    }

    if (request.rotationPolicy !== undefined) {
      this.#validateRotationPolicy(request.rotationPolicy);
    }
  }

  /**
   * Validate rotation policy.
   *
   * @param {DEKRotationPolicy} policy - Policy to validate
   * @throws {ValidationError} If policy is invalid
   * @private
   */
  #validateRotationPolicy(policy) {
    if (typeof policy !== 'object' || Array.isArray(policy)) {
      throw new ValidationError(
        'request.rotationPolicy must be an object',
        'request.rotationPolicy'
      );
    }

    if (typeof policy.enabled !== 'boolean') {
      throw new ValidationError(
        'request.rotationPolicy.enabled must be a boolean',
        'request.rotationPolicy.enabled'
      );
    }

    if (policy.enabled) {
      if (policy.rotationInterval === undefined || policy.rotationInterval === null) {
        throw new ValidationError(
          'request.rotationPolicy.rotationInterval is required when rotation is enabled',
          'request.rotationPolicy.rotationInterval'
        );
      }
      positiveInteger(policy.rotationInterval, 'request.rotationPolicy.rotationInterval');
    }

    if (policy.autoReencrypt !== undefined && typeof policy.autoReencrypt !== 'boolean') {
      throw new ValidationError(
        'request.rotationPolicy.autoReencrypt must be a boolean',
        'request.rotationPolicy.autoReencrypt'
      );
    }
  }

  /**
   * Validate rotate cluster DEK request.
   *
   * @param {RotateClusterDEKRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateRotateClusterDEKRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.reason, 'request.reason');

    if (request.scheduledTime !== undefined && request.scheduledTime !== null) {
      if (typeof request.scheduledTime !== 'string') {
        throw new ValidationError(
          'request.scheduledTime must be a string (ISO timestamp)',
          'request.scheduledTime'
        );
      }
    }

    if (request.forceRotation !== undefined && typeof request.forceRotation !== 'boolean') {
      throw new ValidationError(
        'request.forceRotation must be a boolean',
        'request.forceRotation'
      );
    }

    if (request.autoStartReencryption !== undefined && typeof request.autoStartReencryption !== 'boolean') {
      throw new ValidationError(
        'request.autoStartReencryption must be a boolean',
        'request.autoStartReencryption'
      );
    }

    if (request.metadata !== undefined) {
      if (typeof request.metadata !== 'object' || Array.isArray(request.metadata)) {
        throw new ValidationError(
          'request.metadata must be an object',
          'request.metadata'
        );
      }
    }
  }

  /**
   * Validate node ID.
   *
   * @param {number} nodeId - Node ID to validate
   * @param {string} fieldName - Field name for error messages
   * @throws {ValidationError} If nodeId is invalid
   * @private
   */
  #validateNodeId(nodeId, fieldName) {
    required(nodeId, fieldName);
    if (typeof nodeId !== 'number' || !Number.isInteger(nodeId) || nodeId <= 0) {
      throw new ValidationError(
        `${fieldName} must be a positive integer`,
        fieldName
      );
    }
  }

  /**
   * Validate sync node DEK options.
   *
   * @param {SyncNodeDEKOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateSyncNodeDEKOptions(options) {
    if (options.currentVersion !== undefined) {
      nonNegativeInteger(options.currentVersion, 'options.currentVersion');
    }

    if (options.forceResync !== undefined && typeof options.forceResync !== 'boolean') {
      throw new ValidationError(
        'options.forceResync must be a boolean',
        'options.forceResync'
      );
    }
  }

  /**
   * Validate start re-encryption request.
   *
   * @param {StartReencryptionRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateStartReencryptionRequest(request) {
    requiredObject(request, 'request');
    required(request.fromVersion, 'request.fromVersion');
    required(request.toVersion, 'request.toVersion');
    positiveInteger(request.fromVersion, 'request.fromVersion');
    positiveInteger(request.toVersion, 'request.toVersion');

    if (request.fromVersion >= request.toVersion) {
      throw new ValidationError(
        'request.toVersion must be greater than request.fromVersion',
        'request.toVersion'
      );
    }

    if (request.path !== undefined && typeof request.path !== 'string') {
      throw new ValidationError(
        'request.path must be a string',
        'request.path'
      );
    }

    if (request.batchSize !== undefined) {
      nonNegativeInteger(request.batchSize, 'request.batchSize');
    }

    if (request.concurrency !== undefined) {
      nonNegativeInteger(request.concurrency, 'request.concurrency');
    }

    if (request.throttleRate !== undefined) {
      nonNegativeInteger(request.throttleRate, 'request.throttleRate');
    }

    if (request.force !== undefined && typeof request.force !== 'boolean') {
      throw new ValidationError(
        'request.force must be a boolean',
        'request.force'
      );
    }
  }

  /**
   * Validate recover DEK request.
   *
   * @param {RecoverDEKRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateRecoverDEKRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.reason, 'request.reason');
    requiredString(request.recoveryMethod, 'request.recoveryMethod');
    oneOf(request.recoveryMethod, Object.values(RecoveryMethod), 'request.recoveryMethod');

    switch (request.recoveryMethod) {
      case RecoveryMethod.RECOVERY_KEYS:
        requiredArray(request.recoveryKeys, 'request.recoveryKeys');
        if (request.recoveryKeys.length === 0) {
          throw new ValidationError(
            'request.recoveryKeys cannot be empty for recovery_keys method',
            'request.recoveryKeys'
          );
        }
        for (let i = 0; i < request.recoveryKeys.length; i++) {
          if (typeof request.recoveryKeys[i] !== 'string' || request.recoveryKeys[i].trim() === '') {
            throw new ValidationError(
              `request.recoveryKeys[${i}] must be a non-empty string`,
              `request.recoveryKeys[${i}]`
            );
          }
        }
        break;

      case RecoveryMethod.BACKUP:
        requiredString(request.backupLocation, 'request.backupLocation');
        break;

      case RecoveryMethod.MANUAL:
        requiredString(request.manualKey, 'request.manualKey');
        break;
    }
  }

  // =========================================================================
  // Response Transformation Methods
  // =========================================================================

  /**
   * Transform initialize cluster DEK response.
   *
   * @param {Object} response - Raw server response
   * @returns {InitClusterDEKResponse}
   * @private
   */
  #transformInitClusterDEKResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      version: response.version ?? 1,
      createdAt: response.created_at ?? response.createdAt ?? '',
      nodesSynced: response.nodes_synced ?? response.nodesSynced ?? 0,
      algorithm: response.algorithm ?? '',
      keySize: response.key_size ?? response.keySize ?? 0,
    };
  }

  /**
   * Transform cluster DEK status response.
   *
   * @param {Object} response - Raw server response
   * @returns {ClusterDEKStatusResponse}
   * @private
   */
  #transformClusterDEKStatusResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    const result = {
      initialized: response.initialized ?? false,
      currentVersion: response.current_version ?? response.currentVersion ?? 0,
      createdAt: response.created_at ?? response.createdAt ?? '',
      nodesSynced: response.nodes_synced ?? response.nodesSynced ?? 0,
      totalNodes: response.total_nodes ?? response.totalNodes ?? 0,
      algorithm: response.algorithm ?? '',
      keySize: response.key_size ?? response.keySize ?? 0,
      reencryptionInProgress: response.reencryption_in_progress ?? response.reencryptionInProgress ?? false,
    };

    if (response.rotated_at ?? response.rotatedAt) {
      result.rotatedAt = response.rotated_at ?? response.rotatedAt;
    }

    if (response.next_rotation ?? response.nextRotation) {
      result.nextRotation = response.next_rotation ?? response.nextRotation;
    }

    if (response.nodes_out_of_sync ?? response.nodesOutOfSync) {
      result.nodesOutOfSync = (response.nodes_out_of_sync ?? response.nodesOutOfSync).map(
        node => this.#transformNodeDEKInfo(node)
      );
    }

    if (response.previous_versions ?? response.previousVersions) {
      result.previousVersions = (response.previous_versions ?? response.previousVersions).map(
        version => this.#transformDEKVersionInfo(version)
      );
    }

    if (result.reencryptionInProgress) {
      result.reencryptionProgress = response.reencryption_progress ?? response.reencryptionProgress ?? 0;
    }

    return result;
  }

  /**
   * Transform node DEK info.
   *
   * @param {Object} node - Raw node info
   * @returns {NodeDEKInfo}
   * @private
   */
  #transformNodeDEKInfo(node) {
    const result = {
      nodeId: node.node_id ?? node.nodeId ?? 0,
      currentVersion: node.current_version ?? node.currentVersion ?? 0,
      lastSyncedAt: node.last_synced_at ?? node.lastSyncedAt ?? '',
    };

    if (node.node_name ?? node.nodeName) {
      result.nodeName = node.node_name ?? node.nodeName;
    }

    return result;
  }

  /**
   * Transform DEK version info.
   *
   * @param {Object} version - Raw version info
   * @returns {DEKVersionInfo}
   * @private
   */
  #transformDEKVersionInfo(version) {
    const result = {
      version: version.version ?? 0,
      createdAt: version.created_at ?? version.createdAt ?? '',
      entitiesEncrypted: version.entities_encrypted ?? version.entitiesEncrypted ?? 0,
      canDelete: version.can_delete ?? version.canDelete ?? false,
    };

    if (version.deprecated_at ?? version.deprecatedAt) {
      result.deprecatedAt = version.deprecated_at ?? version.deprecatedAt;
    }

    return result;
  }

  /**
   * Transform rotate cluster DEK response.
   *
   * @param {Object} response - Raw server response
   * @returns {RotateClusterDEKResponse}
   * @private
   */
  #transformRotateClusterDEKResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    const result = {
      newVersion: response.new_version ?? response.newVersion ?? 0,
      oldVersion: response.old_version ?? response.oldVersion ?? 0,
      rotatedAt: response.rotated_at ?? response.rotatedAt ?? '',
      rotationId: response.rotation_id ?? response.rotationId ?? '',
      nodesSynced: response.nodes_synced ?? response.nodesSynced ?? 0,
      reencryptionStarted: response.reencryption_started ?? response.reencryptionStarted ?? false,
    };

    if (response.estimated_reencryption_time ?? response.estimatedReencryptionTime) {
      result.estimatedReencryptionTime =
        response.estimated_reencryption_time ?? response.estimatedReencryptionTime;
    }

    return result;
  }

  /**
   * Transform sync node DEK response.
   *
   * @param {Object} response - Raw server response
   * @returns {SyncNodeDEKResponse}
   * @private
   */
  #transformSyncNodeDEKResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      nodeId: response.node_id ?? response.nodeId ?? 0,
      versionsSynced: response.versions_synced ?? response.versionsSynced ?? [],
      currentVersion: response.current_version ?? response.currentVersion ?? 0,
      syncedAt: response.synced_at ?? response.syncedAt ?? '',
    };
  }

  /**
   * Transform node DEK status response.
   *
   * @param {Object} response - Raw server response
   * @returns {NodeDEKStatusResponse}
   * @private
   */
  #transformNodeDEKStatusResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    const result = {
      nodeId: response.node_id ?? response.nodeId ?? 0,
      currentVersion: response.current_version ?? response.currentVersion ?? 0,
      lastSyncedAt: response.last_synced_at ?? response.lastSyncedAt ?? '',
      status: response.status ?? NodeDEKSyncStatus.MISSING,
      versionsAvailable: response.versions_available ?? response.versionsAvailable ?? [],
    };

    if (response.node_name ?? response.nodeName) {
      result.nodeName = response.node_name ?? response.nodeName;
    }

    if (response.error) {
      result.error = response.error;
    }

    return result;
  }

  /**
   * Transform start re-encryption response.
   *
   * @param {Object} response - Raw server response
   * @returns {StartReencryptionResponse}
   * @private
   */
  #transformStartReencryptionResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    const result = {
      operationId: response.operation_id ?? response.operationId ?? '',
      started: response.started ?? false,
      estimatedItems: response.estimated_items ?? response.estimatedItems ?? 0,
      timestamp: response.timestamp ?? '',
      startedAt: response.started_at ?? response.startedAt ?? '',
      totalEntities: response.total_entities ?? response.totalEntities ?? 0,
      estimatedCompletion: response.estimated_completion ?? response.estimatedCompletion ?? '',
    };

    if (response.path) {
      result.path = response.path;
    }

    return result;
  }

  /**
   * Transform re-encryption status response.
   *
   * @param {Object} response - Raw server response
   * @returns {ReencryptionStatusResponse}
   * @private
   */
  #transformReencryptionStatusResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    const result = {
      operationId: response.operation_id ?? response.operationId ?? '',
      state: response.state ?? ReencryptionState.RUNNING,
      startedAt: response.started_at ?? response.startedAt ?? '',
      progress: response.progress ?? 0,
      totalEntities: response.total_entities ?? response.totalEntities ?? 0,
      processedEntities: response.processed_entities ?? response.processedEntities ?? 0,
      failedEntities: response.failed_entities ?? response.failedEntities ?? 0,
      throughputRate: response.throughput_rate ?? response.throughputRate ?? 0,
    };

    if (response.completed_at ?? response.completedAt) {
      result.completedAt = response.completed_at ?? response.completedAt;
    }

    if (response.estimated_completion ?? response.estimatedCompletion) {
      result.estimatedCompletion = response.estimated_completion ?? response.estimatedCompletion;
    }

    if (response.node_progress ?? response.nodeProgress) {
      result.nodeProgress = (response.node_progress ?? response.nodeProgress).map(
        node => this.#transformNodeReencryptionProgress(node)
      );
    }

    if (response.errors) {
      result.errors = response.errors.map(err => this.#transformReencryptionError(err));
    }

    return result;
  }

  /**
   * Transform node re-encryption progress.
   *
   * @param {Object} node - Raw node progress
   * @returns {NodeReencryptionProgress}
   * @private
   */
  #transformNodeReencryptionProgress(node) {
    const result = {
      nodeId: node.node_id ?? node.nodeId ?? 0,
      progress: node.progress ?? 0,
      processedEntities: node.processed_entities ?? node.processedEntities ?? 0,
      totalEntities: node.total_entities ?? node.totalEntities ?? 0,
      throughputRate: node.throughput_rate ?? node.throughputRate ?? 0,
      lastUpdatedAt: node.last_updated_at ?? node.lastUpdatedAt ?? '',
    };

    if (node.node_name ?? node.nodeName) {
      result.nodeName = node.node_name ?? node.nodeName;
    }

    return result;
  }

  /**
   * Transform re-encryption error.
   *
   * @param {Object} error - Raw error object
   * @returns {ReencryptionError}
   * @private
   */
  #transformReencryptionError(error) {
    const result = {
      timestamp: error.timestamp ?? '',
      nodeId: error.node_id ?? error.nodeId ?? 0,
      error: error.error ?? '',
      retried: error.retried ?? false,
    };

    if (error.entity_id ?? error.entityId) {
      result.entityId = error.entity_id ?? error.entityId;
    }

    return result;
  }

  /**
   * Transform recover DEK response.
   *
   * @param {Object} response - Raw server response
   * @returns {RecoverDEKResponse}
   * @private
   */
  #transformRecoverDEKResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    const result = {
      success: response.success ?? false,
      recoveredVersion: response.recovered_version ?? response.recoveredVersion ?? 0,
      recoveredAt: response.recovered_at ?? response.recoveredAt ?? '',
      nodesSynced: response.nodes_synced ?? response.nodesSynced ?? 0,
      auditId: response.audit_id ?? response.auditId ?? '',
    };

    if (response.warnings) {
      result.warnings = response.warnings;
    }

    return result;
  }
}

export default ClusterDEKClient;
