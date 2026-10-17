/**
 * QuicBaaS JavaScript SDK - Secrets Client
 *
 * Provides secrets management operations including storing, retrieving,
 * versioning, rotation, and watching for secret changes.
 *
 * @module @quicbaas/client/clients/secrets
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError, QuicBaaSError } from '../errors/index.js';
import {
  requiredString,
  required,
  requiredObject,
  positiveInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * API base path for secrets endpoints.
 * @constant {string}
 */
const SECRETS_API_PATH = '/api/v1/secrets';

/**
 * Secret status values.
 * @readonly
 * @enum {string}
 */
export const SecretStatus = Object.freeze({
  /** Secret is active and accessible */
  ACTIVE: 'active',
  /** Secret is disabled and cannot be accessed */
  DISABLED: 'disabled',
});

/**
 * Rotation interval presets.
 * @readonly
 * @enum {string}
 */
export const RotationInterval = Object.freeze({
  /** Rotate every hour */
  HOURLY: 'hourly',
  /** Rotate every day */
  DAILY: 'daily',
  /** Rotate every week */
  WEEKLY: 'weekly',
  /** Rotate every month */
  MONTHLY: 'monthly',
  /** Rotate every quarter (90 days) */
  QUARTERLY: 'quarterly',
  /** Rotate every year */
  YEARLY: 'yearly',
  /** Custom interval (requires intervalSeconds) */
  CUSTOM: 'custom',
});

/**
 * @typedef {Object} Secret
 * @property {string} path - The secret path
 * @property {Object} data - The secret data (key-value pairs)
 * @property {Object} [metadata] - Optional metadata associated with the secret
 * @property {number} version - The version number
 * @property {string} status - The secret status (active or disabled)
 * @property {string} createdAt - ISO timestamp of creation
 * @property {string} [updatedAt] - ISO timestamp of last update
 */

/**
 * @typedef {Object} StoreSecretOptions
 * @property {Object} [metadata] - Optional metadata to store with the secret
 */

/**
 * @typedef {Object} StoreSecretResponse
 * @property {number} version - The version number after storing
 */

/**
 * @typedef {Object} ListSecretsOptions
 * @property {number} [limit] - Maximum number of secrets to return
 * @property {string} [cursor] - Pagination cursor for fetching next page
 */

/**
 * @typedef {Object} ListSecretsResponse
 * @property {string[]} paths - List of secret paths
 * @property {string} [nextCursor] - Cursor for next page (if more results)
 * @property {boolean} hasMore - Whether more results are available
 */

/**
 * @typedef {Object} SecretVersion
 * @property {number} version - The version number
 * @property {string} createdAt - ISO timestamp when this version was created
 * @property {boolean} destroyed - Whether this version has been destroyed
 */

/**
 * @typedef {Object} ListVersionsResponse
 * @property {SecretVersion[]} versions - List of secret versions
 * @property {number} currentVersion - The current/latest version number
 */

/**
 * @typedef {Object} RotateSecretOptions
 * @property {Object} [newData] - New secret data (if not using auto-generation)
 * @property {Object} [metadata] - Metadata to store with the rotated secret
 */

/**
 * @typedef {Object} RotateSecretResponse
 * @property {number} version - The new version number after rotation
 * @property {string} rotatedAt - ISO timestamp of rotation
 */

/**
 * @typedef {Object} RotationPolicy
 * @property {boolean} enabled - Whether automatic rotation is enabled
 * @property {string} interval - Rotation interval (hourly, daily, weekly, monthly, quarterly, yearly, custom)
 * @property {number} [intervalSeconds] - Custom interval in seconds (required if interval is 'custom')
 * @property {string} [lastRotated] - ISO timestamp of last rotation
 * @property {string} [nextRotation] - ISO timestamp of next scheduled rotation
 */

/**
 * @typedef {Object} SetRotationPolicyOptions
 * @property {string} interval - Rotation interval
 * @property {number} [intervalSeconds] - Custom interval in seconds
 */

/**
 * @typedef {Object} UpdateStatusResponse
 * @property {boolean} success - Whether the status update was successful
 * @property {string} status - The new status
 */

/**
 * @typedef {Object} BatchGetResponse
 * @property {Object<string, Secret>} secrets - Map of path to secret
 * @property {string[]} [errors] - Paths that failed to retrieve
 */

/**
 * @typedef {Object} WatchEvent
 * @property {string} type - Event type (PUT, DELETE, STATUS_CHANGE, ROTATED)
 * @property {string} path - The secret path that changed
 * @property {number} [version] - The new version (for PUT events)
 * @property {string} [status] - The new status (for STATUS_CHANGE events)
 * @property {string} timestamp - ISO timestamp of the event
 */

/**
 * @typedef {Object} Watcher
 * @property {Function} close - Close the watcher and release resources
 * @property {boolean} closed - Whether the watcher is closed
 */

/**
 * @typedef {Object} WatchOptions
 * @property {Function} onEvent - Callback for watch events
 * @property {Function} [onError] - Callback for errors
 */

/**
 * Secrets client for QuicBaaS.
 *
 * Provides comprehensive secrets management including storing, retrieving,
 * versioning, rotation policies, and real-time change watching.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import SecretsClient, { SecretStatus, RotationInterval } from '@quicbaas/client/clients/secrets';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const secrets = new SecretsClient(transport);
 *
 * // Store a secret
 * const { version } = await secrets.storeSecret('app/database/credentials', {
 *   username: 'admin',
 *   password: 'secretpass123'
 * });
 *
 * // Get the secret
 * const secret = await secrets.getSecret('app/database/credentials');
 * console.log(secret.data.username);
 *
 * // Set up automatic rotation
 * await secrets.setRotationPolicy('app/database/credentials', {
 *   interval: RotationInterval.MONTHLY
 * });
 *
 * // Watch for changes
 * const watcher = await secrets.watchSecretsByPrefix('app/', {
 *   onEvent: (event) => console.log('Secret changed:', event.path),
 *   onError: (err) => console.error('Watch error:', err)
 * });
 */
export class SecretsClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new SecretsClient instance.
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
   * Store a secret at the given path.
   *
   * Creates a new secret or updates an existing one, creating a new version.
   * The secret data is encrypted at rest using the database's master key.
   *
   * @param {string} path - The path where the secret will be stored
   * @param {Object} value - The secret data as key-value pairs
   * @param {StoreSecretOptions} [options={}] - Additional options
   * @returns {Promise<StoreSecretResponse>} Response with the new version number
   * @throws {ValidationError} If path or value is invalid
   *
   * @example
   * const { version } = await secrets.storeSecret('app/api-keys/stripe', {
   *   publicKey: 'pk_live_xxx',
   *   secretKey: 'sk_live_xxx'
   * }, {
   *   metadata: { environment: 'production', owner: 'payments-team' }
   * });
   */
  async storeSecret(path, value, options = {}) {
    requiredString(path, 'path');
    requiredObject(value, 'value');

    this.#validateStoreOptions(options);

    const encodedPath = this.#encodePath(path);
    const body = {
      data: value,
    };

    if (options.metadata !== undefined) {
      body.metadata = options.metadata;
    }

    const response = await this.#transport.put(
      `${SECRETS_API_PATH}/${encodedPath}`,
      body,
    );

    return {
      version: response?.version ?? 1,
    };
  }

  /**
   * Get the latest version of a secret.
   *
   * @param {string} path - The path of the secret to retrieve
   * @returns {Promise<Secret>} The secret data and metadata
   * @throws {ValidationError} If path is invalid
   * @throws {NotFoundError} If the secret does not exist
   *
   * @example
   * const secret = await secrets.getSecret('app/database/credentials');
   * console.log('Username:', secret.data.username);
   * console.log('Version:', secret.version);
   */
  async getSecret(path) {
    requiredString(path, 'path');

    const encodedPath = this.#encodePath(path);
    const response = await this.#transport.get(
      `${SECRETS_API_PATH}/${encodedPath}`,
    );

    return this.#transformSecret(response);
  }

  /**
   * Get a specific version of a secret.
   *
   * @param {string} path - The path of the secret
   * @param {number} version - The version number to retrieve
   * @returns {Promise<Secret>} The secret data at the specified version
   * @throws {ValidationError} If path or version is invalid
   * @throws {NotFoundError} If the secret or version does not exist
   *
   * @example
   * // Get version 2 of the secret
   * const secret = await secrets.getSecretVersion('app/database/credentials', 2);
   * console.log('Version 2 data:', secret.data);
   */
  async getSecretVersion(path, version) {
    requiredString(path, 'path');
    positiveInteger(version, 'version');

    if (version === undefined || version === null) {
      throw new ValidationError('version is required', 'version');
    }

    const encodedPath = this.#encodePath(path);
    const response = await this.#transport.get(
      `${SECRETS_API_PATH}/${encodedPath}`,
      { query: { version } },
    );

    return this.#transformSecret(response);
  }

  /**
   * List secrets under a path prefix.
   *
   * @param {string} prefix - The path prefix to list secrets under
   * @param {ListSecretsOptions} [options={}] - Pagination options
   * @returns {Promise<ListSecretsResponse>} List of secret paths
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * // List all secrets under 'app/'
   * const { paths, hasMore } = await secrets.listSecrets('app/', { limit: 100 });
   * paths.forEach(path => console.log(path));
   */
  async listSecrets(prefix, options = {}) {
    this.#validateListOptions(options);

    const query = {};
    if (prefix) {
      query.prefix = prefix;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.cursor) {
      query.cursor = options.cursor;
    }

    const response = await this.#transport.get(SECRETS_API_PATH, { query });

    return {
      paths: response?.paths ?? [],
      nextCursor: response?.next_cursor ?? response?.nextCursor ?? null,
      hasMore: response?.has_more ?? response?.hasMore ?? false,
    };
  }

  /**
   * Delete a secret and all its versions.
   *
   * This operation is destructive and cannot be undone. All versions
   * of the secret will be permanently removed.
   *
   * @param {string} path - The path of the secret to delete
   * @returns {Promise<void>}
   * @throws {ValidationError} If path is invalid
   * @throws {NotFoundError} If the secret does not exist
   *
   * @example
   * await secrets.deleteSecret('app/deprecated/old-api-key');
   */
  async deleteSecret(path) {
    requiredString(path, 'path');

    const encodedPath = this.#encodePath(path);
    await this.#transport.delete(`${SECRETS_API_PATH}/${encodedPath}`);
  }

  /**
   * List all versions of a secret.
   *
   * @param {string} path - The path of the secret
   * @returns {Promise<ListVersionsResponse>} List of versions with metadata
   * @throws {ValidationError} If path is invalid
   * @throws {NotFoundError} If the secret does not exist
   *
   * @example
   * const { versions, currentVersion } = await secrets.listSecretVersions('app/database/credentials');
   * console.log('Current version:', currentVersion);
   * versions.forEach(v => {
   *   console.log(`Version ${v.version}: created at ${v.createdAt}`);
   * });
   */
  async listSecretVersions(path) {
    requiredString(path, 'path');

    const encodedPath = this.#encodePath(path);
    const response = await this.#transport.get(
      `${SECRETS_API_PATH}/${encodedPath}/versions`,
    );

    return {
      versions: (response?.versions ?? []).map(v => this.#transformVersion(v)),
      currentVersion: response?.current_version ?? response?.currentVersion ?? 0,
    };
  }

  /**
   * Rollback a secret to a previous version.
   *
   * Creates a new version with the data from the specified previous version.
   * The previous version remains intact.
   *
   * @param {string} path - The path of the secret
   * @param {number} version - The version number to rollback to
   * @returns {Promise<StoreSecretResponse>} Response with the new version number
   * @throws {ValidationError} If path or version is invalid
   * @throws {NotFoundError} If the secret or version does not exist
   *
   * @example
   * // Rollback to version 3
   * const { version } = await secrets.rollbackSecret('app/database/credentials', 3);
   * console.log('New version after rollback:', version);
   */
  async rollbackSecret(path, version) {
    requiredString(path, 'path');
    positiveInteger(version, 'version');

    if (version === undefined || version === null) {
      throw new ValidationError('version is required', 'version');
    }

    const encodedPath = this.#encodePath(path);
    const response = await this.#transport.post(
      `${SECRETS_API_PATH}/${encodedPath}/rollback`,
      { version },
    );

    return {
      version: response?.version ?? 0,
    };
  }

  /**
   * Manually rotate a secret.
   *
   * Creates a new version with either provided data or auto-generated
   * data (if the secret supports auto-generation).
   *
   * @param {string} path - The path of the secret to rotate
   * @param {RotateSecretOptions} [options={}] - Rotation options
   * @returns {Promise<RotateSecretResponse>} Response with new version and rotation timestamp
   * @throws {ValidationError} If path or options are invalid
   * @throws {NotFoundError} If the secret does not exist
   *
   * @example
   * // Manual rotation with new data
   * const { version, rotatedAt } = await secrets.rotateSecret('app/api-keys/stripe', {
   *   newData: { publicKey: 'pk_live_new', secretKey: 'sk_live_new' }
   * });
   */
  async rotateSecret(path, options = {}) {
    requiredString(path, 'path');

    this.#validateRotateOptions(options);

    const encodedPath = this.#encodePath(path);
    const body = {};

    if (options.newData !== undefined) {
      body.data = options.newData;
    }
    if (options.metadata !== undefined) {
      body.metadata = options.metadata;
    }

    const response = await this.#transport.post(
      `${SECRETS_API_PATH}/${encodedPath}/rotate`,
      body,
    );

    return {
      version: response?.version ?? 0,
      rotatedAt: response?.rotated_at ?? response?.rotatedAt ?? new Date().toISOString(),
    };
  }

  /**
   * Set the automatic rotation policy for a secret.
   *
   * @param {string} path - The path of the secret
   * @param {SetRotationPolicyOptions} policy - Rotation policy configuration
   * @returns {Promise<RotationPolicy>} The configured rotation policy
   * @throws {ValidationError} If path or policy is invalid
   * @throws {NotFoundError} If the secret does not exist
   *
   * @example
   * // Set monthly rotation
   * const policy = await secrets.setRotationPolicy('app/database/credentials', {
   *   interval: RotationInterval.MONTHLY
   * });
   *
   * // Set custom 45-day rotation
   * const customPolicy = await secrets.setRotationPolicy('app/api-keys/stripe', {
   *   interval: RotationInterval.CUSTOM,
   *   intervalSeconds: 45 * 24 * 60 * 60
   * });
   */
  async setRotationPolicy(path, policy) {
    requiredString(path, 'path');
    requiredObject(policy, 'policy');

    this.#validateRotationPolicy(policy);

    const encodedPath = this.#encodePath(path);
    const body = {
      enabled: true,
      interval: policy.interval,
    };

    if (policy.intervalSeconds !== undefined) {
      body.interval_seconds = policy.intervalSeconds;
    }

    const response = await this.#transport.put(
      `${SECRETS_API_PATH}/${encodedPath}/rotation-policy`,
      body,
    );

    return this.#transformRotationPolicy(response);
  }

  /**
   * Get the rotation policy for a secret.
   *
   * @param {string} path - The path of the secret
   * @returns {Promise<RotationPolicy>} The current rotation policy
   * @throws {ValidationError} If path is invalid
   * @throws {NotFoundError} If the secret does not exist
   *
   * @example
   * const policy = await secrets.getRotationPolicy('app/database/credentials');
   * if (policy.enabled) {
   *   console.log('Next rotation:', policy.nextRotation);
   * }
   */
  async getRotationPolicy(path) {
    requiredString(path, 'path');

    const encodedPath = this.#encodePath(path);
    const response = await this.#transport.get(
      `${SECRETS_API_PATH}/${encodedPath}/rotation-policy`,
    );

    return this.#transformRotationPolicy(response);
  }

  /**
   * Update the status of a secret (enable or disable).
   *
   * Disabled secrets cannot be accessed until re-enabled.
   *
   * @param {string} path - The path of the secret
   * @param {string} status - The new status (active or disabled)
   * @returns {Promise<UpdateStatusResponse>} Response with success status
   * @throws {ValidationError} If path or status is invalid
   * @throws {NotFoundError} If the secret does not exist
   *
   * @example
   * // Disable a secret
   * await secrets.updateSecretStatus('app/deprecated/old-key', SecretStatus.DISABLED);
   *
   * // Re-enable a secret
   * await secrets.updateSecretStatus('app/deprecated/old-key', SecretStatus.ACTIVE);
   */
  async updateSecretStatus(path, status) {
    requiredString(path, 'path');
    requiredString(status, 'status');
    oneOf(status, Object.values(SecretStatus), 'status');

    const encodedPath = this.#encodePath(path);
    const response = await this.#transport.patch(
      `${SECRETS_API_PATH}/${encodedPath}/status`,
      { status },
    );

    return {
      success: response?.success ?? true,
      status: response?.status ?? status,
    };
  }

  /**
   * Batch retrieve secrets by prefix.
   *
   * Retrieves all secrets under a given prefix in a single request.
   * More efficient than making individual requests for each secret.
   *
   * @param {string} prefix - The path prefix
   * @returns {Promise<BatchGetResponse>} Map of paths to secrets
   * @throws {ValidationError} If prefix is invalid
   *
   * @example
   * const { secrets, errors } = await secrets.getSecretsByPrefix('app/database/');
   * for (const [path, secret] of Object.entries(secrets)) {
   *   console.log(`${path}: version ${secret.version}`);
   * }
   */
  async getSecretsByPrefix(prefix) {
    requiredString(prefix, 'prefix');

    const response = await this.#transport.get(`${SECRETS_API_PATH}/batch`, {
      query: { prefix },
    });

    const secrets = {};
    const rawSecrets = response?.secrets ?? {};

    for (const [path, secretData] of Object.entries(rawSecrets)) {
      secrets[path] = this.#transformSecret(secretData);
    }

    return {
      secrets,
      errors: response?.errors ?? [],
    };
  }

  /**
   * Watch secrets by prefix for real-time changes.
   *
   * Establishes a Server-Sent Events (SSE) connection to receive
   * notifications when secrets under the given prefix are created,
   * updated, deleted, or have their status changed.
   *
   * @param {string} prefix - The path prefix to watch
   * @param {WatchOptions} options - Watch options with callbacks
   * @returns {Promise<Watcher>} A watcher object with close() method
   * @throws {ValidationError} If prefix or options are invalid
   *
   * @example
   * const watcher = await secrets.watchSecretsByPrefix('app/', {
   *   onEvent: (event) => {
   *     console.log(`Secret ${event.path} ${event.type}`);
   *     if (event.type === 'PUT') {
   *       console.log(`New version: ${event.version}`);
   *     }
   *   },
   *   onError: (err) => {
   *     console.error('Watch error:', err);
   *   }
   * });
   *
   * // Later, when done watching
   * watcher.close();
   */
  async watchSecretsByPrefix(prefix, options) {
    requiredString(prefix, 'prefix');
    required(options, 'options');
    required(options.onEvent, 'options.onEvent');

    if (typeof options.onEvent !== 'function') {
      throw new ValidationError('options.onEvent must be a function', 'options.onEvent');
    }
    if (options.onError !== undefined && typeof options.onError !== 'function') {
      throw new ValidationError('options.onError must be a function', 'options.onError');
    }

    const queryParams = new URLSearchParams();
    queryParams.set('prefix', prefix);

    const url = `${this.#transport.baseUrl}${SECRETS_API_PATH}/watch?${queryParams.toString()}`;

    const headers = {};
    if (this.#transport.authToken) {
      headers['Authorization'] = `Bearer ${this.#transport.authToken}`;
    }

    let eventSource;
    let closed = false;

    const watcher = {
      get closed() {
        return closed;
      },
      close: () => {
        if (!closed) {
          closed = true;
          if (eventSource) {
            eventSource.close();
          }
        }
      },
    };

    try {
      eventSource = new EventSource(url);

      eventSource.onmessage = (event) => {
        if (closed) {
          return;
        }

        try {
          const data = JSON.parse(event.data);
          const watchEvent = {
            type: data.type ?? 'PUT',
            path: data.path,
            version: data.version,
            status: data.status,
            timestamp: data.timestamp ?? new Date().toISOString(),
          };

          options.onEvent(watchEvent);
        } catch (parseError) {
          if (options.onError) {
            options.onError(new QuicBaaSError(
              'Failed to parse watch event',
              'PARSE_ERROR',
              { cause: parseError.message },
            ));
          }
        }
      };

      eventSource.onerror = (error) => {
        if (closed) {
          return;
        }

        if (options.onError) {
          options.onError(new QuicBaaSError(
            'Watch connection error',
            'WATCH_ERROR',
            { cause: error },
          ));
        }
      };

      return watcher;
    } catch (error) {
      throw new QuicBaaSError(
        'Failed to create watch connection',
        'WATCH_ERROR',
        { cause: error.message },
      );
    }
  }

  /**
   * Encode a secret path for use in URLs.
   *
   * @param {string} path - The secret path
   * @returns {string} URL-encoded path
   * @private
   */
  #encodePath(path) {
    return path.split('/').map(segment => encodeURIComponent(segment)).join('/');
  }

  /**
   * Validate store secret options.
   *
   * @param {StoreSecretOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateStoreOptions(options) {
    if (options.metadata !== undefined) {
      if (typeof options.metadata !== 'object' || Array.isArray(options.metadata)) {
        throw new ValidationError('options.metadata must be an object', 'options.metadata');
      }
    }
  }

  /**
   * Validate list secrets options.
   *
   * @param {ListSecretsOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListOptions(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
    if (options.cursor !== undefined && typeof options.cursor !== 'string') {
      throw new ValidationError('options.cursor must be a string', 'options.cursor');
    }
  }

  /**
   * Validate rotate secret options.
   *
   * @param {RotateSecretOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateRotateOptions(options) {
    if (options.newData !== undefined) {
      if (typeof options.newData !== 'object' || Array.isArray(options.newData)) {
        throw new ValidationError('options.newData must be an object', 'options.newData');
      }
    }
    if (options.metadata !== undefined) {
      if (typeof options.metadata !== 'object' || Array.isArray(options.metadata)) {
        throw new ValidationError('options.metadata must be an object', 'options.metadata');
      }
    }
  }

  /**
   * Validate rotation policy configuration.
   *
   * @param {SetRotationPolicyOptions} policy - Policy to validate
   * @throws {ValidationError} If policy is invalid
   * @private
   */
  #validateRotationPolicy(policy) {
    requiredString(policy.interval, 'policy.interval');
    oneOf(policy.interval, Object.values(RotationInterval), 'policy.interval');

    if (policy.interval === RotationInterval.CUSTOM) {
      if (policy.intervalSeconds === undefined || policy.intervalSeconds === null) {
        throw new ValidationError(
          'intervalSeconds is required when interval is custom',
          'policy.intervalSeconds',
        );
      }
      positiveInteger(policy.intervalSeconds, 'policy.intervalSeconds');
    }
  }

  /**
   * Transform server response to Secret format.
   *
   * @param {Object} response - Raw server response
   * @returns {Secret}
   * @private
   */
  #transformSecret(response) {
    if (!response) {
      throw new ValidationError('Invalid secret response from server', 'response');
    }

    return {
      path: response.path ?? '',
      data: response.data ?? {},
      metadata: response.metadata ?? {},
      version: response.version ?? 0,
      status: response.status ?? SecretStatus.ACTIVE,
      createdAt: response.created_at ?? response.createdAt ?? null,
      updatedAt: response.updated_at ?? response.updatedAt ?? null,
    };
  }

  /**
   * Transform server response to SecretVersion format.
   *
   * @param {Object} version - Raw version object
   * @returns {SecretVersion}
   * @private
   */
  #transformVersion(version) {
    return {
      version: version.version ?? 0,
      createdAt: version.created_at ?? version.createdAt ?? null,
      destroyed: version.destroyed ?? false,
    };
  }

  /**
   * Transform server response to RotationPolicy format.
   *
   * @param {Object} response - Raw server response
   * @returns {RotationPolicy}
   * @private
   */
  #transformRotationPolicy(response) {
    return {
      enabled: response?.enabled ?? false,
      interval: response?.interval ?? null,
      intervalSeconds: response?.interval_seconds ?? response?.intervalSeconds ?? null,
      lastRotated: response?.last_rotated ?? response?.lastRotated ?? null,
      nextRotation: response?.next_rotation ?? response?.nextRotation ?? null,
    };
  }
}

export default SecretsClient;
