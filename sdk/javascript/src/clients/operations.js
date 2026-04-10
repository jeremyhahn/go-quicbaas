/**
 * QuicBaaS JavaScript SDK - Operations Client
 *
 * Provides operations management functionality for data migration,
 * node replacement, and rolling upgrades.
 *
 * @module @quicbaas/client/clients/operations
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  positiveInteger,
  nonNegativeInteger,
} from '../utils/validation.js';

/**
 * Migration status values.
 * @readonly
 * @enum {string}
 */
export const MigrationStatus = Object.freeze({
  /** Migration has been queued and is waiting to start */
  PENDING: 'pending',
  /** Migration is currently in progress */
  RUNNING: 'running',
  /** Migration completed successfully */
  COMPLETED: 'completed',
  /** Migration failed with an error */
  FAILED: 'failed',
  /** Migration was cancelled by the user */
  CANCELLED: 'cancelled',
  /** Migration is paused */
  PAUSED: 'paused',
});

/**
 * Upgrade status values.
 * @readonly
 * @enum {string}
 */
export const UpgradeStatus = Object.freeze({
  /** Upgrade has been queued and is waiting to start */
  PENDING: 'pending',
  /** Upgrade is currently in progress */
  RUNNING: 'running',
  /** Upgrade completed successfully */
  COMPLETED: 'completed',
  /** Upgrade failed with an error */
  FAILED: 'failed',
  /** Upgrade was cancelled by the user */
  CANCELLED: 'cancelled',
  /** Upgrade is rolling back due to failure */
  ROLLING_BACK: 'rolling_back',
});

/**
 * Node replacement status values.
 * @readonly
 * @enum {string}
 */
export const ReplaceNodeStatus = Object.freeze({
  /** Replacement has been queued and is waiting to start */
  PENDING: 'pending',
  /** Replacement is currently in progress */
  RUNNING: 'running',
  /** Replacement completed successfully */
  COMPLETED: 'completed',
  /** Replacement failed with an error */
  FAILED: 'failed',
});

/**
 * @typedef {Object} MigrateDataRequest
 * @property {number} sourceShard - The shard ID to migrate data from
 * @property {number} targetShard - The shard ID to migrate data to
 * @property {string[]} [entityTypes] - Entity types to migrate (empty means all)
 * @property {number} [concurrency] - Number of concurrent migration workers
 */

/**
 * @typedef {Object} MigrateDataResponse
 * @property {string} migrationId - Unique identifier for the migration job
 * @property {string} status - Current status of the migration
 * @property {number} estimatedDuration - Estimated completion time in seconds
 */

/**
 * @typedef {Object} MigrationProgress
 * @property {string} migrationId - Unique identifier for the migration
 * @property {number} progress - Completion percentage (0-100)
 * @property {string} status - Current status of the migration
 * @property {number} itemsMigrated - Number of items migrated so far
 * @property {number} totalItems - Total number of items to migrate
 * @property {number} eta - Estimated time of completion as Unix timestamp
 */

/**
 * @typedef {Object} ReplaceNodeOptions
 * @property {number} shardId - The shard ID containing the node to replace
 * @property {string} newAddress - Network address of the replacement node
 */

/**
 * @typedef {Object} ReplaceNodeResponse
 * @property {string} jobId - Unique identifier for the replacement job
 * @property {string} status - Current status of the replacement operation
 */

/**
 * @typedef {Object} UpgradeNodeOptions
 * @property {string} targetVersion - The version to upgrade to
 */

/**
 * @typedef {Object} UpgradeNodeResponse
 * @property {string} jobId - Unique identifier for the upgrade job
 * @property {string} status - Current status of the upgrade
 */

/**
 * @typedef {Object} UpgradeStatusResponse
 * @property {string} jobId - Unique identifier for the upgrade
 * @property {number} progress - Completion percentage (0-100)
 * @property {string} status - Current status of the upgrade
 * @property {string} currentVersion - The node's current version
 * @property {string} targetVersion - The version being upgraded to
 */

/**
 * API base path for operations.
 * @constant {string}
 */
const API_BASE_PATH = '/api/v1/operations';

/**
 * OperationsClient provides operations management functionality for QuicBaaS.
 *
 * This client handles administrative operations including:
 * - Data migration between shards/nodes
 * - Node replacement for failed nodes
 * - Rolling upgrades of cluster nodes
 *
 * @example
 * const transport = new HttpTransport('http://localhost:8080');
 * const opsClient = new OperationsClient(transport);
 *
 * // Start data migration
 * const migration = await opsClient.migrateData({
 *   sourceShard: 1,
 *   targetShard: 2,
 *   entityTypes: ['users', 'orders'],
 *   concurrency: 4
 * });
 *
 * // Check migration progress
 * const progress = await opsClient.getMigrationProgress(migration.migrationId);
 * console.log(`Migration ${progress.progress}% complete`);
 */
class OperationsClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   */
  #transport;

  /**
   * Create a new OperationsClient instance.
   *
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} When transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /**
   * Start data migration between shards or nodes.
   *
   * Initiates an asynchronous data migration operation. The migration runs
   * in the background and can be monitored using getMigrationProgress().
   *
   * @param {MigrateDataRequest} request - Migration request parameters
   * @returns {Promise<MigrateDataResponse>} Response containing migration job details
   * @throws {ValidationError} When request validation fails
   *
   * @example
   * const result = await opsClient.migrateData({
   *   sourceShard: 1,
   *   targetShard: 2,
   *   entityTypes: ['users'],
   *   concurrency: 2
   * });
   * console.log(`Migration started: ${result.migrationId}`);
   */
  async migrateData(request) {
    this.#validateMigrateDataRequest(request);

    const body = {
      sourceShard: request.sourceShard,
      targetShard: request.targetShard,
    };

    if (request.entityTypes !== undefined && request.entityTypes.length > 0) {
      body.entityTypes = request.entityTypes;
    }

    if (request.concurrency !== undefined && request.concurrency > 0) {
      body.concurrency = request.concurrency;
    }

    const response = await this.#transport.post(`${API_BASE_PATH}/migrate`, body);

    return {
      migrationId: response?.migrationId ?? '',
      status: response?.status ?? MigrationStatus.PENDING,
      estimatedDuration: response?.estimatedDuration ?? 0,
    };
  }

  /**
   * Get the progress of an ongoing migration.
   *
   * Retrieves the current status and progress of a migration job
   * previously started with migrateData().
   *
   * @param {string} migrationId - The unique identifier of the migration to query
   * @returns {Promise<MigrationProgress>} Migration progress information
   * @throws {ValidationError} When migrationId is not provided
   *
   * @example
   * const progress = await opsClient.getMigrationProgress('mig-123');
   * console.log(`Progress: ${progress.progress}%`);
   * console.log(`Items: ${progress.itemsMigrated}/${progress.totalItems}`);
   */
  async getMigrationProgress(migrationId) {
    requiredString(migrationId, 'migrationId');

    const encodedId = encodeURIComponent(migrationId);
    const response = await this.#transport.get(`${API_BASE_PATH}/migrate/${encodedId}`);

    return {
      migrationId: response?.migrationId ?? migrationId,
      progress: response?.progress ?? 0,
      status: response?.status ?? MigrationStatus.PENDING,
      itemsMigrated: response?.itemsMigrated ?? 0,
      totalItems: response?.totalItems ?? 0,
      eta: response?.eta ?? 0,
    };
  }

  /**
   * Replace a failed node with a new node.
   *
   * Initiates a node replacement operation. This is used when a node
   * has failed and needs to be replaced with a new node in the cluster.
   *
   * @param {number} oldNodeId - The ID of the node to be replaced
   * @param {number} newNodeId - The ID of the replacement node
   * @param {ReplaceNodeOptions} options - Additional options for node replacement
   * @returns {Promise<ReplaceNodeResponse>} Response containing replacement job details
   * @throws {ValidationError} When required parameters are missing or invalid
   *
   * @example
   * const result = await opsClient.replaceNode(1, 4, {
   *   shardId: 1,
   *   newAddress: '192.168.1.100:5000'
   * });
   * console.log(`Replacement job: ${result.jobId}`);
   */
  async replaceNode(oldNodeId, newNodeId, options) {
    this.#validateReplaceNodeRequest(oldNodeId, newNodeId, options);

    const body = {
      shardId: options.shardId,
      oldNodeId,
      newNodeId,
      newAddress: options.newAddress,
    };

    const response = await this.#transport.post(`${API_BASE_PATH}/nodes/replace`, body);

    return {
      jobId: response?.jobId ?? '',
      status: response?.status ?? ReplaceNodeStatus.PENDING,
    };
  }

  /**
   * Start a rolling upgrade of a node.
   *
   * Initiates an upgrade operation for a specific node. The upgrade
   * runs in the background and can be monitored using getUpgradeStatus().
   *
   * @param {number} nodeId - The ID of the node to upgrade
   * @param {UpgradeNodeOptions} options - Upgrade options including target version
   * @returns {Promise<UpgradeNodeResponse>} Response containing upgrade job details
   * @throws {ValidationError} When required parameters are missing or invalid
   *
   * @example
   * const result = await opsClient.upgradeNode(1, {
   *   targetVersion: '2.0.0'
   * });
   * console.log(`Upgrade job: ${result.jobId}`);
   */
  async upgradeNode(nodeId, options) {
    this.#validateUpgradeNodeRequest(nodeId, options);

    const body = {
      nodeId,
      targetVersion: options.targetVersion,
    };

    const response = await this.#transport.post(`${API_BASE_PATH}/nodes/upgrade`, body);

    return {
      jobId: response?.jobId ?? '',
      status: response?.status ?? UpgradeStatus.PENDING,
    };
  }

  /**
   * Get the status of an ongoing upgrade.
   *
   * Retrieves the current status and progress of an upgrade job
   * previously started with upgradeNode().
   *
   * @param {string} upgradeId - The unique identifier of the upgrade job to query
   * @returns {Promise<UpgradeStatusResponse>} Upgrade status information
   * @throws {ValidationError} When upgradeId is not provided
   *
   * @example
   * const status = await opsClient.getUpgradeStatus('upg-123');
   * console.log(`Status: ${status.status}`);
   * console.log(`Progress: ${status.progress}%`);
   * console.log(`Version: ${status.currentVersion} -> ${status.targetVersion}`);
   */
  async getUpgradeStatus(upgradeId) {
    requiredString(upgradeId, 'upgradeId');

    const encodedId = encodeURIComponent(upgradeId);
    const response = await this.#transport.get(`${API_BASE_PATH}/nodes/upgrade/${encodedId}`);

    return {
      jobId: response?.jobId ?? upgradeId,
      progress: response?.progress ?? 0,
      status: response?.status ?? UpgradeStatus.PENDING,
      currentVersion: response?.currentVersion ?? '',
      targetVersion: response?.targetVersion ?? '',
    };
  }

  /**
   * Validate migrate data request parameters.
   *
   * @param {MigrateDataRequest} request - Request to validate
   * @throws {ValidationError} When validation fails
   * @private
   */
  #validateMigrateDataRequest(request) {
    required(request, 'request');

    if (typeof request !== 'object' || Array.isArray(request)) {
      throw new ValidationError('request must be an object', 'request');
    }

    positiveInteger(request.sourceShard, 'sourceShard');
    required(request.sourceShard, 'sourceShard');

    positiveInteger(request.targetShard, 'targetShard');
    required(request.targetShard, 'targetShard');

    if (request.sourceShard === request.targetShard) {
      throw new ValidationError(
        'source and target shards must be different',
        'targetShard'
      );
    }

    if (request.entityTypes !== undefined) {
      if (!Array.isArray(request.entityTypes)) {
        throw new ValidationError('entityTypes must be an array', 'entityTypes');
      }
      for (let i = 0; i < request.entityTypes.length; i++) {
        if (typeof request.entityTypes[i] !== 'string' || request.entityTypes[i].trim() === '') {
          throw new ValidationError(
            `entityTypes[${i}] must be a non-empty string`,
            `entityTypes[${i}]`
          );
        }
      }
    }

    if (request.concurrency !== undefined) {
      nonNegativeInteger(request.concurrency, 'concurrency');
    }
  }

  /**
   * Validate replace node request parameters.
   *
   * @param {number} oldNodeId - Old node ID to validate
   * @param {number} newNodeId - New node ID to validate
   * @param {ReplaceNodeOptions} options - Options to validate
   * @throws {ValidationError} When validation fails
   * @private
   */
  #validateReplaceNodeRequest(oldNodeId, newNodeId, options) {
    positiveInteger(oldNodeId, 'oldNodeId');
    required(oldNodeId, 'oldNodeId');

    positiveInteger(newNodeId, 'newNodeId');
    required(newNodeId, 'newNodeId');

    if (oldNodeId === newNodeId) {
      throw new ValidationError(
        'old and new node IDs must be different',
        'newNodeId'
      );
    }

    required(options, 'options');

    if (typeof options !== 'object' || Array.isArray(options)) {
      throw new ValidationError('options must be an object', 'options');
    }

    positiveInteger(options.shardId, 'options.shardId');
    required(options.shardId, 'options.shardId');

    requiredString(options.newAddress, 'options.newAddress');
  }

  /**
   * Validate upgrade node request parameters.
   *
   * @param {number} nodeId - Node ID to validate
   * @param {UpgradeNodeOptions} options - Options to validate
   * @throws {ValidationError} When validation fails
   * @private
   */
  #validateUpgradeNodeRequest(nodeId, options) {
    positiveInteger(nodeId, 'nodeId');
    required(nodeId, 'nodeId');

    required(options, 'options');

    if (typeof options !== 'object' || Array.isArray(options)) {
      throw new ValidationError('options must be an object', 'options');
    }

    requiredString(options.targetVersion, 'options.targetVersion');
  }
}

export default OperationsClient;
