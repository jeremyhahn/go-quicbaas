/**
 * QuicBaaS JavaScript SDK - Backup Client
 *
 * Provides backup and restore operations for QuicBaaS including
 * creating backups, restoring from backups, and monitoring progress.
 *
 * @module @quicbaas/client/clients/backup
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  positiveInteger,
  nonNegativeInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * Backup type enumeration.
 * @readonly
 * @enum {string}
 */
export const BackupType = Object.freeze({
  /** Full backup of all data */
  FULL: 'FULL',
  /** Incremental backup since last backup */
  INCREMENTAL: 'INCREMENTAL',
});

/**
 * Backup status enumeration.
 * @readonly
 * @enum {string}
 */
export const BackupStatus = Object.freeze({
  /** Backup is pending execution */
  PENDING: 'PENDING',
  /** Backup is currently in progress */
  IN_PROGRESS: 'IN_PROGRESS',
  /** Backup completed successfully */
  COMPLETED: 'COMPLETED',
  /** Backup failed */
  FAILED: 'FAILED',
});

/**
 * Restore status enumeration.
 * @readonly
 * @enum {string}
 */
export const RestoreStatus = Object.freeze({
  /** Restore is pending execution */
  PENDING: 'PENDING',
  /** Restore is currently in progress */
  IN_PROGRESS: 'IN_PROGRESS',
  /** Restore completed successfully */
  COMPLETED: 'COMPLETED',
  /** Restore failed */
  FAILED: 'FAILED',
});

/**
 * @typedef {Object} CreateBackupOptions
 * @property {string} name - Human-readable name for the backup
 * @property {string} [description] - Optional description of the backup
 * @property {BackupType} [type] - Type of backup (FULL or INCREMENTAL)
 * @property {string} destination - Destination path or URI for the backup
 * @property {boolean} [compress] - Whether to compress the backup
 * @property {boolean} [encrypt] - Whether to encrypt the backup
 */

/**
 * @typedef {Object} CreateBackupResponse
 * @property {string} backupId - Unique identifier for the backup
 * @property {BackupStatus} status - Current status of the backup
 * @property {string} createdAt - ISO timestamp when backup was created
 */

/**
 * @typedef {Object} ListBackupsOptions
 * @property {number} [limit] - Maximum number of backups to return
 * @property {number} [offset] - Number of backups to skip for pagination
 * @property {BackupType} [type] - Filter by backup type
 * @property {BackupStatus} [status] - Filter by backup status
 */

/**
 * @typedef {Object} BackupInfo
 * @property {string} backupId - Unique identifier for the backup
 * @property {string} name - Human-readable name
 * @property {string} [description] - Optional description
 * @property {BackupType} type - Type of backup
 * @property {BackupStatus} status - Current status
 * @property {string} destination - Backup destination path/URI
 * @property {number} size - Size of the backup in bytes
 * @property {boolean} compressed - Whether backup is compressed
 * @property {boolean} encrypted - Whether backup is encrypted
 * @property {string} createdAt - ISO timestamp when created
 * @property {string} [completedAt] - ISO timestamp when completed
 * @property {number} [durationMs] - Duration of backup operation in milliseconds
 * @property {string} [error] - Error message if failed
 */

/**
 * @typedef {Object} ListBackupsResponse
 * @property {BackupInfo[]} backups - List of backup information
 * @property {number} total - Total number of backups matching criteria
 * @property {boolean} hasMore - Whether more backups are available
 */

/**
 * @typedef {Object} RestoreOptions
 * @property {string} [targetPath] - Optional target path for restore
 * @property {boolean} [overwrite] - Whether to overwrite existing data
 * @property {boolean} [validateFirst] - Validate backup integrity before restore
 */

/**
 * @typedef {Object} RestoreResponse
 * @property {string} restoreId - Unique identifier for the restore operation
 * @property {string} backupId - ID of the backup being restored
 * @property {RestoreStatus} status - Current status of the restore
 * @property {string} startedAt - ISO timestamp when restore started
 */

/**
 * @typedef {Object} RestoreProgressResponse
 * @property {string} restoreId - Unique identifier for the restore operation
 * @property {string} backupId - ID of the backup being restored
 * @property {RestoreStatus} status - Current status
 * @property {number} progress - Progress percentage (0-100)
 * @property {number} bytesRestored - Number of bytes restored
 * @property {number} totalBytes - Total bytes to restore
 * @property {string} startedAt - ISO timestamp when started
 * @property {string} [completedAt] - ISO timestamp when completed
 * @property {number} [durationMs] - Duration of restore operation in milliseconds
 * @property {string} [error] - Error message if failed
 */

/**
 * @typedef {Object} VerifyBackupResponse
 * @property {string} backupId - ID of the verified backup
 * @property {boolean} valid - Whether the backup is valid
 * @property {string} [checksum] - Backup checksum if available
 * @property {string} verifiedAt - ISO timestamp when verification completed
 * @property {string[]} [issues] - List of issues found during verification
 */

/**
 * @typedef {Object} DeleteBackupResponse
 * @property {string} backupId - ID of the deleted backup
 * @property {boolean} deleted - Whether the backup was deleted
 * @property {string} deletedAt - ISO timestamp when deleted
 */

/**
 * Client for QuicBaaS backup and restore operations.
 *
 * Provides operations for creating backups, restoring from backups,
 * verifying backup integrity, and monitoring backup/restore progress.
 *
 * @example
 * const transport = new HttpTransport('http://localhost:8080');
 * const backupClient = new BackupClient(transport);
 *
 * // Create a full backup
 * const backup = await backupClient.createBackup({
 *   name: 'daily-backup',
 *   description: 'Daily production backup',
 *   type: BackupType.FULL,
 *   destination: '/backups/daily/2025-01-16'
 * });
 * console.log('Backup ID:', backup.backupId);
 *
 * // List all backups
 * const backups = await backupClient.listBackups({ limit: 10 });
 * console.log('Total backups:', backups.total);
 */
class BackupClient {
  /**
   * API endpoint prefix for backup operations.
   * @private
   * @type {string}
   */
  static API_PREFIX = '/api/v1/backup';

  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new BackupClient.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /**
   * Create a new backup.
   *
   * Initiates a backup operation with the specified configuration.
   * Returns immediately with a backup ID that can be used to track progress.
   *
   * @param {CreateBackupOptions} options - Backup creation options
   * @param {string} options.name - Human-readable name for the backup
   * @param {string} [options.description] - Optional description
   * @param {BackupType} [options.type=BackupType.FULL] - Backup type
   * @param {string} options.destination - Destination path or URI
   * @param {boolean} [options.compress=false] - Whether to compress
   * @param {boolean} [options.encrypt=false] - Whether to encrypt
   * @returns {Promise<CreateBackupResponse>} Backup creation response
   * @throws {ValidationError} If required options are missing or invalid
   *
   * @example
   * const result = await backupClient.createBackup({
   *   name: 'weekly-backup',
   *   type: BackupType.FULL,
   *   destination: 's3://my-bucket/backups/weekly',
   *   compress: true,
   *   encrypt: true
   * });
   * console.log('Backup started:', result.backupId);
   */
  async createBackup(options) {
    this.#validateCreateBackupOptions(options);

    const path = `${BackupClient.API_PREFIX}/create`;
    const body = {
      name: options.name,
      destination: options.destination,
    };

    if (options.description !== undefined) {
      body.description = options.description;
    }
    if (options.type !== undefined) {
      body.type = options.type;
    }
    if (options.compress !== undefined) {
      body.compress = options.compress;
    }
    if (options.encrypt !== undefined) {
      body.encrypt = options.encrypt;
    }

    const response = await this.#transport.post(path, body);

    return {
      backupId: response?.backupId ?? '',
      status: this.#parseBackupStatus(response?.status),
      createdAt: response?.createdAt ?? new Date().toISOString(),
    };
  }

  /**
   * List all backups with optional filtering and pagination.
   *
   * Returns a paginated list of backups matching the specified criteria.
   *
   * @param {ListBackupsOptions} [options={}] - List options
   * @param {number} [options.limit] - Maximum number of backups to return
   * @param {number} [options.offset] - Number of backups to skip
   * @param {BackupType} [options.type] - Filter by backup type
   * @param {BackupStatus} [options.status] - Filter by backup status
   * @returns {Promise<ListBackupsResponse>} List of backups with pagination info
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * const response = await backupClient.listBackups({
   *   limit: 20,
   *   offset: 0,
   *   type: BackupType.FULL,
   *   status: BackupStatus.COMPLETED
   * });
   * console.log('Found', response.total, 'backups');
   * for (const backup of response.backups) {
   *   console.log(backup.name, backup.status);
   * }
   */
  async listBackups(options = {}) {
    this.#validateListBackupsOptions(options);

    const path = `${BackupClient.API_PREFIX}/list`;
    const query = {};

    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      query.offset = options.offset;
    }
    if (options.type !== undefined) {
      query.type = options.type;
    }
    if (options.status !== undefined) {
      query.status = options.status;
    }

    const response = await this.#transport.get(path, { query });

    return {
      backups: (response?.backups ?? []).map((backup) => this.#parseBackupInfo(backup)),
      total: response?.total ?? 0,
      hasMore: response?.hasMore ?? false,
    };
  }

  /**
   * Get detailed information about a specific backup.
   *
   * Returns comprehensive information about the backup including
   * its status, size, timestamps, and any error information.
   *
   * @param {string} backupId - The backup ID to retrieve
   * @returns {Promise<BackupInfo>} Detailed backup information
   * @throws {ValidationError} If backupId is invalid
   *
   * @example
   * const backup = await backupClient.getBackup('backup-123');
   * console.log('Backup name:', backup.name);
   * console.log('Status:', backup.status);
   * console.log('Size:', backup.size, 'bytes');
   */
  async getBackup(backupId) {
    requiredString(backupId, 'backupId');

    const path = `${BackupClient.API_PREFIX}/${encodeURIComponent(backupId)}`;
    const response = await this.#transport.get(path);

    return this.#parseBackupInfo(response);
  }

  /**
   * Restore data from a backup.
   *
   * Initiates a restore operation from the specified backup.
   * Returns immediately with a restore ID that can be used to track progress.
   *
   * @param {string} backupId - The backup ID to restore from
   * @param {RestoreOptions} [options={}] - Restore options
   * @param {string} [options.targetPath] - Optional target path for restore
   * @param {boolean} [options.overwrite=false] - Whether to overwrite existing data
   * @param {boolean} [options.validateFirst=true] - Validate backup before restore
   * @returns {Promise<RestoreResponse>} Restore operation response
   * @throws {ValidationError} If backupId is invalid
   *
   * @example
   * const result = await backupClient.restoreBackup('backup-123', {
   *   overwrite: true,
   *   validateFirst: true
   * });
   * console.log('Restore started:', result.restoreId);
   */
  async restoreBackup(backupId, options = {}) {
    requiredString(backupId, 'backupId');
    this.#validateRestoreOptions(options);

    const path = `${BackupClient.API_PREFIX}/${encodeURIComponent(backupId)}/restore`;
    const body = {};

    if (options.targetPath !== undefined) {
      body.targetPath = options.targetPath;
    }
    if (options.overwrite !== undefined) {
      body.overwrite = options.overwrite;
    }
    if (options.validateFirst !== undefined) {
      body.validateFirst = options.validateFirst;
    }

    const response = await this.#transport.post(path, body);

    return {
      restoreId: response?.restoreId ?? '',
      backupId: response?.backupId ?? backupId,
      status: this.#parseRestoreStatus(response?.status),
      startedAt: response?.startedAt ?? new Date().toISOString(),
    };
  }

  /**
   * Delete a backup.
   *
   * Permanently removes a backup from storage. This operation cannot be undone.
   *
   * @param {string} backupId - The backup ID to delete
   * @returns {Promise<DeleteBackupResponse>} Delete operation response
   * @throws {ValidationError} If backupId is invalid
   *
   * @example
   * const result = await backupClient.deleteBackup('backup-123');
   * if (result.deleted) {
   *   console.log('Backup deleted at:', result.deletedAt);
   * }
   */
  async deleteBackup(backupId) {
    requiredString(backupId, 'backupId');

    const path = `${BackupClient.API_PREFIX}/${encodeURIComponent(backupId)}`;
    const response = await this.#transport.delete(path);

    return {
      backupId: response?.backupId ?? backupId,
      deleted: response?.deleted ?? true,
      deletedAt: response?.deletedAt ?? new Date().toISOString(),
    };
  }

  /**
   * Verify backup integrity.
   *
   * Performs integrity verification on a backup to ensure it can be
   * successfully restored. This includes checksum validation and
   * structural integrity checks.
   *
   * @param {string} backupId - The backup ID to verify
   * @returns {Promise<VerifyBackupResponse>} Verification result
   * @throws {ValidationError} If backupId is invalid
   *
   * @example
   * const result = await backupClient.verifyBackup('backup-123');
   * if (result.valid) {
   *   console.log('Backup is valid, checksum:', result.checksum);
   * } else {
   *   console.log('Backup has issues:', result.issues);
   * }
   */
  async verifyBackup(backupId) {
    requiredString(backupId, 'backupId');

    const path = `${BackupClient.API_PREFIX}/${encodeURIComponent(backupId)}/verify`;
    const response = await this.#transport.post(path, {});

    return {
      backupId: response?.backupId ?? backupId,
      valid: response?.valid ?? false,
      checksum: response?.checksum ?? null,
      verifiedAt: response?.verifiedAt ?? new Date().toISOString(),
      issues: response?.issues ?? [],
    };
  }

  /**
   * Get the progress of a restore operation.
   *
   * Returns the current status and progress of an ongoing or
   * completed restore operation.
   *
   * @param {string} restoreId - The restore operation ID
   * @returns {Promise<RestoreProgressResponse>} Restore progress information
   * @throws {ValidationError} If restoreId is invalid
   *
   * @example
   * const progress = await backupClient.getRestoreProgress('restore-456');
   * console.log('Status:', progress.status);
   * console.log('Progress:', progress.progress, '%');
   * console.log('Bytes restored:', progress.bytesRestored, '/', progress.totalBytes);
   */
  async getRestoreProgress(restoreId) {
    requiredString(restoreId, 'restoreId');

    const path = `${BackupClient.API_PREFIX}/restore/${encodeURIComponent(restoreId)}`;
    const response = await this.#transport.get(path);

    return {
      restoreId: response?.restoreId ?? restoreId,
      backupId: response?.backupId ?? '',
      status: this.#parseRestoreStatus(response?.status),
      progress: response?.progress ?? 0,
      bytesRestored: response?.bytesRestored ?? 0,
      totalBytes: response?.totalBytes ?? 0,
      startedAt: response?.startedAt ?? null,
      completedAt: response?.completedAt ?? null,
      durationMs: response?.durationMs ?? null,
      error: response?.error ?? null,
    };
  }

  /**
   * Validate create backup options.
   * @private
   * @param {CreateBackupOptions} options - Options to validate
   * @throws {ValidationError} If validation fails
   */
  #validateCreateBackupOptions(options) {
    required(options, 'options');

    if (typeof options !== 'object' || Array.isArray(options)) {
      throw new ValidationError('options must be an object', 'options');
    }

    requiredString(options.name, 'options.name');
    requiredString(options.destination, 'options.destination');

    if (options.description !== undefined) {
      if (typeof options.description !== 'string') {
        throw new ValidationError('options.description must be a string', 'options.description');
      }
    }

    if (options.type !== undefined) {
      oneOf(options.type, Object.values(BackupType), 'options.type');
    }

    if (options.compress !== undefined && typeof options.compress !== 'boolean') {
      throw new ValidationError('options.compress must be a boolean', 'options.compress');
    }

    if (options.encrypt !== undefined && typeof options.encrypt !== 'boolean') {
      throw new ValidationError('options.encrypt must be a boolean', 'options.encrypt');
    }
  }

  /**
   * Validate list backups options.
   * @private
   * @param {ListBackupsOptions} options - Options to validate
   * @throws {ValidationError} If validation fails
   */
  #validateListBackupsOptions(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }

    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
    }

    if (options.type !== undefined) {
      oneOf(options.type, Object.values(BackupType), 'options.type');
    }

    if (options.status !== undefined) {
      oneOf(options.status, Object.values(BackupStatus), 'options.status');
    }
  }

  /**
   * Validate restore options.
   * @private
   * @param {RestoreOptions} options - Options to validate
   * @throws {ValidationError} If validation fails
   */
  #validateRestoreOptions(options) {
    if (options.targetPath !== undefined) {
      if (typeof options.targetPath !== 'string') {
        throw new ValidationError('options.targetPath must be a string', 'options.targetPath');
      }
    }

    if (options.overwrite !== undefined && typeof options.overwrite !== 'boolean') {
      throw new ValidationError('options.overwrite must be a boolean', 'options.overwrite');
    }

    if (options.validateFirst !== undefined && typeof options.validateFirst !== 'boolean') {
      throw new ValidationError('options.validateFirst must be a boolean', 'options.validateFirst');
    }
  }

  /**
   * Parse backup info from response.
   * @private
   * @param {Object} data - Raw backup data
   * @returns {BackupInfo} Parsed backup info
   */
  #parseBackupInfo(data) {
    if (!data) {
      return {
        backupId: '',
        name: '',
        description: null,
        type: BackupType.FULL,
        status: BackupStatus.PENDING,
        destination: '',
        size: 0,
        compressed: false,
        encrypted: false,
        createdAt: new Date().toISOString(),
        completedAt: null,
        durationMs: null,
        error: null,
      };
    }

    return {
      backupId: data.backupId ?? data.id ?? '',
      name: data.name ?? '',
      description: data.description ?? null,
      type: this.#parseBackupType(data.type),
      status: this.#parseBackupStatus(data.status),
      destination: data.destination ?? '',
      size: data.size ?? 0,
      compressed: Boolean(data.compressed ?? data.compress),
      encrypted: Boolean(data.encrypted ?? data.encrypt),
      createdAt: data.createdAt ?? new Date().toISOString(),
      completedAt: data.completedAt ?? null,
      durationMs: data.durationMs ?? data.duration ?? null,
      error: data.error ?? null,
    };
  }

  /**
   * Parse backup type string to enum value.
   * @private
   * @param {string|number} type - Raw type value
   * @returns {BackupType} Parsed backup type
   */
  #parseBackupType(type) {
    if (typeof type === 'string') {
      const normalized = type.toUpperCase();
      if (Object.values(BackupType).includes(normalized)) {
        return normalized;
      }
    }

    if (typeof type === 'number') {
      const typeMap = {
        0: BackupType.FULL,
        1: BackupType.INCREMENTAL,
      };
      return typeMap[type] ?? BackupType.FULL;
    }

    return BackupType.FULL;
  }

  /**
   * Parse backup status string to enum value.
   * @private
   * @param {string|number} status - Raw status value
   * @returns {BackupStatus} Parsed backup status
   */
  #parseBackupStatus(status) {
    if (typeof status === 'string') {
      const normalized = status.toUpperCase();
      if (Object.values(BackupStatus).includes(normalized)) {
        return normalized;
      }
    }

    if (typeof status === 'number') {
      const statusMap = {
        0: BackupStatus.PENDING,
        1: BackupStatus.IN_PROGRESS,
        2: BackupStatus.COMPLETED,
        3: BackupStatus.FAILED,
      };
      return statusMap[status] ?? BackupStatus.PENDING;
    }

    return BackupStatus.PENDING;
  }

  /**
   * Parse restore status string to enum value.
   * @private
   * @param {string|number} status - Raw status value
   * @returns {RestoreStatus} Parsed restore status
   */
  #parseRestoreStatus(status) {
    if (typeof status === 'string') {
      const normalized = status.toUpperCase();
      if (Object.values(RestoreStatus).includes(normalized)) {
        return normalized;
      }
    }

    if (typeof status === 'number') {
      const statusMap = {
        0: RestoreStatus.PENDING,
        1: RestoreStatus.IN_PROGRESS,
        2: RestoreStatus.COMPLETED,
        3: RestoreStatus.FAILED,
      };
      return statusMap[status] ?? RestoreStatus.PENDING;
    }

    return RestoreStatus.PENDING;
  }
}

export default BackupClient;
export { BackupClient };
