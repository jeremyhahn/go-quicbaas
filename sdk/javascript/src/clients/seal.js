/**
 * QuicBaaS JavaScript SDK - Seal Client
 *
 * Provides seal/unseal operations for managing database encryption state.
 * Implements Shamir secret sharing for secure key management with support
 * for initialization, unsealing, sealing, rekeying, and recovery operations.
 *
 * @module @quicbaas/client/clients/seal
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  positiveInteger,
} from '../utils/validation.js';

/**
 * API base path for seal endpoints.
 * @constant {string}
 */
const SEAL_API_PATH = '/api/v1/seal';

/**
 * @typedef {Object} SealStatus
 * @property {boolean} sealed - Whether the database is currently sealed
 * @property {boolean} initialized - Whether the database has been initialized
 * @property {number} progress - Number of unseal keys provided so far
 * @property {number} required - Number of unseal keys required to unseal
 * @property {string} [clusterName] - Name of the cluster
 * @property {string} [clusterVersion] - Version of the cluster
 */

/**
 * @typedef {Object} InitializeRequest
 * @property {number} secretShares - Number of key shares to create
 * @property {number} secretThreshold - Minimum shares required to unseal
 * @property {number} [recoveryShares] - Number of recovery key shares
 * @property {number} [recoveryThreshold] - Minimum recovery shares required
 */

/**
 * @typedef {Object} InitializeResponse
 * @property {string[]} keys - The unseal key shares (base64 encoded)
 * @property {string[]} [keysBase64] - Keys in base64 format
 * @property {string} rootToken - The initial root token for authentication
 * @property {string[]} [recoveryKeys] - Recovery key shares if recovery was configured
 * @property {string[]} [recoveryKeysBase64] - Recovery keys in base64 format
 */

/**
 * @typedef {Object} UnsealResponse
 * @property {boolean} sealed - Whether the database is still sealed
 * @property {number} progress - Number of unseal keys provided so far
 * @property {number} required - Number of unseal keys required to unseal
 */

/**
 * @typedef {Object} RekeyRequest
 * @property {number} secretShares - Number of new key shares to create
 * @property {number} secretThreshold - Minimum new shares required to unseal
 * @property {string[]} [existingKeys] - Existing unseal keys to authorize rekey
 */

/**
 * @typedef {Object} RekeyResponse
 * @property {boolean} complete - Whether the rekey operation is complete
 * @property {string[]} [keys] - New unseal key shares (only when complete)
 * @property {string[]} [keysBase64] - New keys in base64 format
 * @property {number} progress - Number of keys provided so far
 * @property {number} required - Number of keys required
 */

/**
 * @typedef {Object} GenerateRecoveryKeyRequest
 * @property {number} recoveryShares - Number of recovery key shares to create
 * @property {number} recoveryThreshold - Minimum recovery shares required
 */

/**
 * @typedef {Object} GenerateRecoveryKeyResponse
 * @property {string[]} recoveryKeys - The recovery key shares
 * @property {string[]} [recoveryKeysBase64] - Recovery keys in base64 format
 */

/**
 * @typedef {Object} ValidateRecoveryKeyResponse
 * @property {boolean} valid - Whether the recovery key is valid
 */

/**
 * Seal client for QuicBaaS.
 *
 * Provides operations for managing the database seal state including
 * initialization with Shamir secret sharing, unsealing, sealing,
 * key rotation, and recovery key management.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import { SealClient } from '@quicbaas/client/clients/seal';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const seal = new SealClient(transport);
 *
 * // Check seal status
 * const status = await seal.status();
 * if (!status.initialized) {
 *   // Initialize with 5 shares, requiring 3 to unseal
 *   const init = await seal.initialize({
 *     secretShares: 5,
 *     secretThreshold: 3
 *   });
 *   console.log('Root token:', init.rootToken);
 *   console.log('Unseal keys:', init.keys);
 * }
 *
 * // Unseal the database
 * if (status.sealed) {
 *   await seal.unseal(key1);
 *   await seal.unseal(key2);
 *   const result = await seal.unseal(key3);
 *   console.log('Sealed:', result.sealed); // false
 * }
 */
export class SealClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new SealClient instance.
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
   * Get the current seal status.
   *
   * This is a non-privileged operation that can be called without authentication
   * to determine if the database is sealed, initialized, and the unseal progress.
   *
   * @returns {Promise<SealStatus>} The current seal status
   *
   * @example
   * const status = await seal.status();
   * console.log('Sealed:', status.sealed);
   * console.log('Initialized:', status.initialized);
   * console.log('Progress:', `${status.progress}/${status.required}`);
   */
  async status() {
    const response = await this.#transport.get(`${SEAL_API_PATH}/status`);

    return this.#transformSealStatus(response);
  }

  /**
   * Initialize the database with Shamir secret sharing.
   *
   * This operation can only be performed once on a new database.
   * It generates the master encryption key, splits it using Shamir's
   * secret sharing algorithm, and returns the key shares along with
   * the initial root token for authentication.
   *
   * IMPORTANT: Store the unseal keys securely and distribute them to
   * trusted operators. The root token should be used to create initial
   * policies and authentication methods, then revoked.
   *
   * @param {InitializeRequest} request - Initialization parameters
   * @returns {Promise<InitializeResponse>} The initialization result with keys and root token
   * @throws {ValidationError} If request parameters are invalid
   * @throws {ConflictError} If the database is already initialized
   *
   * @example
   * const result = await seal.initialize({
   *   secretShares: 5,
   *   secretThreshold: 3,
   *   recoveryShares: 3,
   *   recoveryThreshold: 2
   * });
   *
   * // Securely distribute the unseal keys
   * result.keys.forEach((key, i) => {
   *   console.log(`Key ${i + 1}: ${key}`);
   * });
   *
   * // Use root token for initial setup
   * transport.setAuthToken(result.rootToken);
   */
  async initialize(request) {
    this.#validateInitializeRequest(request);

    const body = {
      secret_shares: request.secretShares,
      secret_threshold: request.secretThreshold,
    };

    if (request.recoveryShares !== undefined) {
      body.recovery_shares = request.recoveryShares;
    }

    if (request.recoveryThreshold !== undefined) {
      body.recovery_threshold = request.recoveryThreshold;
    }

    const response = await this.#transport.post(`${SEAL_API_PATH}/init`, body);

    return this.#transformInitializeResponse(response);
  }

  /**
   * Provide an unseal key share to progress toward unsealing.
   *
   * Each call with a valid key share increments the unseal progress.
   * Once the threshold number of shares have been provided, the database
   * will be unsealed and available for operations.
   *
   * @param {string} keyShare - A single unseal key share (base64 encoded)
   * @returns {Promise<UnsealResponse>} The unseal progress
   * @throws {ValidationError} If keyShare is empty
   *
   * @example
   * // Provide unseal keys one at a time
   * let result = await seal.unseal(key1);
   * console.log(`Progress: ${result.progress}/${result.required}`);
   *
   * result = await seal.unseal(key2);
   * console.log(`Progress: ${result.progress}/${result.required}`);
   *
   * result = await seal.unseal(key3);
   * if (!result.sealed) {
   *   console.log('Database is now unsealed!');
   * }
   */
  async unseal(keyShare) {
    requiredString(keyShare, 'keyShare');

    const body = {
      key: keyShare,
    };

    const response = await this.#transport.post(`${SEAL_API_PATH}/unseal`, body);

    return this.#transformUnsealResponse(response);
  }

  /**
   * Immediately seal the database.
   *
   * This operation seals the database, requiring the threshold number
   * of unseal keys to be provided again before the database can be
   * accessed. Use this for emergency situations or planned maintenance.
   *
   * WARNING: This will immediately make the database unavailable until
   * it is unsealed again.
   *
   * @returns {Promise<void>}
   * @throws {AuthorizationError} If not authorized to seal
   *
   * @example
   * // Emergency seal
   * await seal.seal();
   * console.log('Database is now sealed');
   *
   * // Verify seal status
   * const status = await seal.status();
   * console.log('Sealed:', status.sealed); // true
   */
  async seal() {
    await this.#transport.post(`${SEAL_API_PATH}/seal`, {});
  }

  /**
   * Rotate the master encryption key.
   *
   * This operation generates a new master key and splits it into new
   * shares using the specified parameters. The existing unseal keys
   * must be provided to authorize the rekey operation.
   *
   * @param {RekeyRequest} request - Rekey parameters
   * @returns {Promise<RekeyResponse>} The rekey result
   * @throws {ValidationError} If request parameters are invalid
   * @throws {AuthorizationError} If not authorized to rekey
   *
   * @example
   * const result = await seal.rekey({
   *   secretShares: 5,
   *   secretThreshold: 3,
   *   existingKeys: [key1, key2, key3]
   * });
   *
   * if (result.complete) {
   *   console.log('New unseal keys:', result.keys);
   * }
   */
  async rekey(request) {
    this.#validateRekeyRequest(request);

    const body = {
      secret_shares: request.secretShares,
      secret_threshold: request.secretThreshold,
    };

    if (request.existingKeys !== undefined) {
      body.existing_keys = request.existingKeys;
    }

    const response = await this.#transport.post(`${SEAL_API_PATH}/rekey`, body);

    return this.#transformRekeyResponse(response);
  }

  /**
   * Generate new recovery keys.
   *
   * Recovery keys can be used as a backup mechanism for unsealing
   * if the primary unseal keys are lost. This operation generates
   * new recovery key shares.
   *
   * @param {GenerateRecoveryKeyRequest} request - Recovery key generation parameters
   * @returns {Promise<GenerateRecoveryKeyResponse>} The generated recovery keys
   * @throws {ValidationError} If request parameters are invalid
   * @throws {AuthorizationError} If not authorized to generate recovery keys
   *
   * @example
   * const result = await seal.generateRecoveryKey({
   *   recoveryShares: 3,
   *   recoveryThreshold: 2
   * });
   *
   * // Securely store recovery keys
   * result.recoveryKeys.forEach((key, i) => {
   *   console.log(`Recovery key ${i + 1}: ${key}`);
   * });
   */
  async generateRecoveryKey(request) {
    this.#validateGenerateRecoveryKeyRequest(request);

    const body = {
      recovery_shares: request.recoveryShares,
      recovery_threshold: request.recoveryThreshold,
    };

    const response = await this.#transport.post(`${SEAL_API_PATH}/recovery/generate`, body);

    return this.#transformGenerateRecoveryKeyResponse(response);
  }

  /**
   * Validate a recovery key.
   *
   * Checks if a recovery key is valid without using it.
   *
   * @param {string} key - The recovery key to validate
   * @returns {Promise<ValidateRecoveryKeyResponse>} The validation result
   * @throws {ValidationError} If key is empty
   *
   * @example
   * const result = await seal.validateRecoveryKey(recoveryKey);
   * if (result.valid) {
   *   console.log('Recovery key is valid');
   * } else {
   *   console.log('Recovery key is invalid');
   * }
   */
  async validateRecoveryKey(key) {
    requiredString(key, 'key');

    const body = {
      recovery_key: key,
    };

    const response = await this.#transport.post(`${SEAL_API_PATH}/recovery/validate`, body);

    return {
      valid: response?.valid ?? false,
    };
  }

  /**
   * Validate initialize request parameters.
   *
   * @param {InitializeRequest} request - Request to validate
   * @throws {ValidationError} If parameters are invalid
   * @private
   */
  #validateInitializeRequest(request) {
    required(request, 'request');

    positiveInteger(request.secretShares, 'request.secretShares');
    positiveInteger(request.secretThreshold, 'request.secretThreshold');

    if (request.secretShares === undefined || request.secretShares === null) {
      throw new ValidationError('secretShares is required', 'request.secretShares');
    }

    if (request.secretThreshold === undefined || request.secretThreshold === null) {
      throw new ValidationError('secretThreshold is required', 'request.secretThreshold');
    }

    if (request.secretThreshold > request.secretShares) {
      throw new ValidationError(
        'secretThreshold cannot exceed secretShares',
        'request.secretThreshold'
      );
    }

    if (request.recoveryShares !== undefined) {
      positiveInteger(request.recoveryShares, 'request.recoveryShares');
    }

    if (request.recoveryThreshold !== undefined) {
      positiveInteger(request.recoveryThreshold, 'request.recoveryThreshold');

      if (request.recoveryShares === undefined) {
        throw new ValidationError(
          'recoveryShares is required when recoveryThreshold is specified',
          'request.recoveryShares'
        );
      }

      if (request.recoveryThreshold > request.recoveryShares) {
        throw new ValidationError(
          'recoveryThreshold cannot exceed recoveryShares',
          'request.recoveryThreshold'
        );
      }
    }
  }

  /**
   * Validate rekey request parameters.
   *
   * @param {RekeyRequest} request - Request to validate
   * @throws {ValidationError} If parameters are invalid
   * @private
   */
  #validateRekeyRequest(request) {
    required(request, 'request');

    positiveInteger(request.secretShares, 'request.secretShares');
    positiveInteger(request.secretThreshold, 'request.secretThreshold');

    if (request.secretShares === undefined || request.secretShares === null) {
      throw new ValidationError('secretShares is required', 'request.secretShares');
    }

    if (request.secretThreshold === undefined || request.secretThreshold === null) {
      throw new ValidationError('secretThreshold is required', 'request.secretThreshold');
    }

    if (request.secretThreshold > request.secretShares) {
      throw new ValidationError(
        'secretThreshold cannot exceed secretShares',
        'request.secretThreshold'
      );
    }

    if (request.existingKeys !== undefined) {
      if (!Array.isArray(request.existingKeys)) {
        throw new ValidationError('existingKeys must be an array', 'request.existingKeys');
      }
      for (let i = 0; i < request.existingKeys.length; i++) {
        if (typeof request.existingKeys[i] !== 'string' || request.existingKeys[i].trim() === '') {
          throw new ValidationError(
            `existingKeys[${i}] must be a non-empty string`,
            `request.existingKeys[${i}]`
          );
        }
      }
    }
  }

  /**
   * Validate generate recovery key request parameters.
   *
   * @param {GenerateRecoveryKeyRequest} request - Request to validate
   * @throws {ValidationError} If parameters are invalid
   * @private
   */
  #validateGenerateRecoveryKeyRequest(request) {
    required(request, 'request');

    positiveInteger(request.recoveryShares, 'request.recoveryShares');
    positiveInteger(request.recoveryThreshold, 'request.recoveryThreshold');

    if (request.recoveryShares === undefined || request.recoveryShares === null) {
      throw new ValidationError('recoveryShares is required', 'request.recoveryShares');
    }

    if (request.recoveryThreshold === undefined || request.recoveryThreshold === null) {
      throw new ValidationError('recoveryThreshold is required', 'request.recoveryThreshold');
    }

    if (request.recoveryThreshold > request.recoveryShares) {
      throw new ValidationError(
        'recoveryThreshold cannot exceed recoveryShares',
        'request.recoveryThreshold'
      );
    }
  }

  /**
   * Transform server response to SealStatus format.
   *
   * @param {Object} response - Raw server response
   * @returns {SealStatus}
   * @private
   */
  #transformSealStatus(response) {
    return {
      sealed: response?.sealed ?? true,
      initialized: response?.initialized ?? false,
      progress: response?.progress ?? 0,
      required: response?.required ?? 0,
      clusterName: response?.cluster_name ?? response?.clusterName ?? null,
      clusterVersion: response?.cluster_version ?? response?.clusterVersion ?? null,
    };
  }

  /**
   * Transform server response to InitializeResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {InitializeResponse}
   * @private
   */
  #transformInitializeResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid initialize response from server', 'response');
    }

    const result = {
      keys: response.keys ?? [],
      keysBase64: response.keys_base64 ?? response.keysBase64 ?? null,
      rootToken: response.root_token ?? response.rootToken,
    };

    if (response.recovery_keys ?? response.recoveryKeys) {
      result.recoveryKeys = response.recovery_keys ?? response.recoveryKeys;
    }

    if (response.recovery_keys_base64 ?? response.recoveryKeysBase64) {
      result.recoveryKeysBase64 = response.recovery_keys_base64 ?? response.recoveryKeysBase64;
    }

    return result;
  }

  /**
   * Transform server response to UnsealResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {UnsealResponse}
   * @private
   */
  #transformUnsealResponse(response) {
    return {
      sealed: response?.sealed ?? true,
      progress: response?.progress ?? 0,
      required: response?.required ?? 0,
    };
  }

  /**
   * Transform server response to RekeyResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {RekeyResponse}
   * @private
   */
  #transformRekeyResponse(response) {
    const result = {
      complete: response?.complete ?? false,
      progress: response?.progress ?? 0,
      required: response?.required ?? 0,
    };

    if (response?.keys) {
      result.keys = response.keys;
    }

    if (response?.keys_base64 ?? response?.keysBase64) {
      result.keysBase64 = response.keys_base64 ?? response.keysBase64;
    }

    return result;
  }

  /**
   * Transform server response to GenerateRecoveryKeyResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {GenerateRecoveryKeyResponse}
   * @private
   */
  #transformGenerateRecoveryKeyResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid recovery key response from server', 'response');
    }

    const result = {
      recoveryKeys: response.recovery_keys ?? response.recoveryKeys ?? [],
    };

    if (response.recovery_keys_base64 ?? response.recoveryKeysBase64) {
      result.recoveryKeysBase64 = response.recovery_keys_base64 ?? response.recoveryKeysBase64;
    }

    return result;
  }
}

export default SealClient;
