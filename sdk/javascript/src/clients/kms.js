/**
 * QuicBaaS JavaScript SDK - KMS (Key Management Service) Client
 *
 * Provides key management operations including key generation, import,
 * rotation, revocation, and policy management.
 *
 * @module @quicbaas/client/clients/kms
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  requiredString,
  requiredObject,
  nonNegativeInteger,
  positiveInteger,
  oneOf,
  requiredArray,
} from '../utils/validation.js';

/**
 * API base path for KMS endpoints.
 * @constant {string}
 */
const KMS_API_PATH = '/api/v1/kms';

/**
 * Supported cryptographic key algorithms.
 * @readonly
 * @enum {string}
 */
export const KeyAlgorithm = Object.freeze({
  /** AES-256 in GCM mode for symmetric encryption */
  AES_256_GCM: 'AES-256-GCM',
  /** ChaCha20-Poly1305 symmetric encryption */
  CHACHA20_POLY1305: 'CHACHA20-POLY1305',
  /** RSA with 2048-bit key */
  RSA_2048: 'RSA-2048',
  /** RSA with 4096-bit key */
  RSA_4096: 'RSA-4096',
  /** ECDSA with P-256 curve */
  ECDSA_P256: 'ECDSA-P256',
  /** ECDSA with P-384 curve */
  ECDSA_P384: 'ECDSA-P384',
  /** Ed25519 signature algorithm */
  ED25519: 'ED25519',
});

/**
 * Key status values.
 * @readonly
 * @enum {string}
 */
export const KeyStatus = Object.freeze({
  /** Key is active and can be used */
  ACTIVE: 'ACTIVE',
  /** Key is disabled and cannot be used */
  DISABLED: 'DISABLED',
  /** Key is scheduled for deletion */
  PENDING_DELETION: 'PENDING_DELETION',
  /** Key has been destroyed */
  DESTROYED: 'DESTROYED',
});

/**
 * Key usage types indicating how a key can be used.
 * @readonly
 * @enum {string}
 */
export const KeyUsage = Object.freeze({
  /** Key can be used for encryption */
  ENCRYPT: 'ENCRYPT',
  /** Key can be used for decryption */
  DECRYPT: 'DECRYPT',
  /** Key can be used for signing */
  SIGN: 'SIGN',
  /** Key can be used for signature verification */
  VERIFY: 'VERIFY',
  /** Key can be used for key wrapping */
  WRAP: 'WRAP',
  /** Key can be used for key unwrapping */
  UNWRAP: 'UNWRAP',
});

/**
 * @typedef {Object} GenerateKeyRequest
 * @property {string} name - Name for the key
 * @property {string} algorithm - Key algorithm (from KeyAlgorithm enum)
 * @property {string[]} [usage] - Allowed key usages (from KeyUsage enum)
 * @property {string} [description] - Optional description
 * @property {Object} [metadata] - Custom metadata key-value pairs
 * @property {boolean} [rotatable] - Whether automatic rotation is allowed
 * @property {number} [rotationPeriodDays] - Days between automatic rotations
 */

/**
 * @typedef {Object} GenerateKeyResponse
 * @property {string} keyId - Unique identifier for the generated key
 * @property {number} version - Key version (starts at 1)
 * @property {string} algorithm - Algorithm used
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} ImportKeyRequest
 * @property {string} name - Name for the key
 * @property {string} algorithm - Key algorithm (from KeyAlgorithm enum)
 * @property {string} keyMaterial - Base64-encoded key material
 * @property {string[]} [usage] - Allowed key usages (from KeyUsage enum)
 * @property {string} [description] - Optional description
 * @property {Object} [metadata] - Custom metadata key-value pairs
 */

/**
 * @typedef {Object} ImportKeyResponse
 * @property {string} keyId - Unique identifier for the imported key
 * @property {number} version - Key version (starts at 1)
 * @property {string} algorithm - Algorithm used
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} KeyInfo
 * @property {string} keyId - Unique key identifier
 * @property {string} name - Key name
 * @property {string} algorithm - Key algorithm
 * @property {number} version - Current key version
 * @property {string} status - Key status (from KeyStatus enum)
 * @property {string[]} usage - Allowed key usages
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 * @property {number} rotatedAt - Last rotation timestamp (Unix nanoseconds)
 * @property {string} [description] - Key description
 */

/**
 * @typedef {Object} KeyMetadata
 * @property {string} keyId - Unique key identifier
 * @property {string} name - Key name
 * @property {string} algorithm - Key algorithm
 * @property {number} version - Current key version
 * @property {string} status - Key status
 * @property {string[]} usage - Allowed key usages
 * @property {string} [description] - Key description
 * @property {Object} [customMetadata] - Custom metadata key-value pairs
 * @property {boolean} rotatable - Whether automatic rotation is allowed
 * @property {number} [rotationPeriodDays] - Days between automatic rotations
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 * @property {number} updatedAt - Last update timestamp (Unix nanoseconds)
 * @property {number} rotatedAt - Last rotation timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} KeyVersion
 * @property {number} version - Version number
 * @property {string} status - Version status
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 * @property {number} [destroyedAt] - Destruction timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} ListKeysOptions
 * @property {number} [limit] - Maximum number of keys to return
 * @property {number} [offset] - Number of keys to skip
 * @property {string} [status] - Filter by key status
 * @property {string} [algorithm] - Filter by algorithm
 */

/**
 * @typedef {Object} ListKeysResponse
 * @property {KeyInfo[]} keys - List of keys
 * @property {number} totalCount - Total number of keys available
 */

/**
 * @typedef {Object} ListKeyVersionsResponse
 * @property {string} keyId - Key identifier
 * @property {KeyVersion[]} versions - List of key versions
 * @property {number} totalCount - Total number of versions
 */

/**
 * @typedef {Object} RotateKeyOptions
 * @property {boolean} [destroyPrevious] - Whether to destroy the previous version
 */

/**
 * @typedef {Object} RotateKeyResponse
 * @property {string} keyId - Key identifier
 * @property {number} newVersion - New version number after rotation
 * @property {number} rotatedAt - Rotation timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} RevokeKeyResponse
 * @property {boolean} success - Whether the revocation was successful
 * @property {string} message - Additional information
 */

/**
 * @typedef {Object} DeleteKeyResponse
 * @property {boolean} success - Whether the deletion was successful
 * @property {string} message - Additional information
 */

/**
 * @typedef {Object} UpdateMetadataRequest
 * @property {string} [name] - New key name
 * @property {string} [description] - New description
 * @property {Object} [customMetadata] - Custom metadata to merge
 * @property {boolean} [rotatable] - Whether automatic rotation is allowed
 * @property {number} [rotationPeriodDays] - Days between automatic rotations
 */

/**
 * @typedef {Object} UpdateStatusResponse
 * @property {boolean} success - Whether the update was successful
 * @property {string} previousStatus - Previous status
 * @property {string} newStatus - New status
 */

/**
 * @typedef {Object} KeyPolicy
 * @property {string} keyId - Key identifier
 * @property {PolicyStatement[]} statements - Policy statements
 * @property {number} version - Policy version
 * @property {number} updatedAt - Last update timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} PolicyStatement
 * @property {string} sid - Statement identifier
 * @property {string} effect - ALLOW or DENY
 * @property {string[]} principals - Principal identifiers
 * @property {string[]} actions - Allowed or denied actions
 * @property {Object} [conditions] - Optional conditions
 */

/**
 * @typedef {Object} SetPolicyRequest
 * @property {PolicyStatement[]} statements - Policy statements
 */

/**
 * @typedef {Object} SupportedAlgorithm
 * @property {string} algorithm - Algorithm identifier
 * @property {string} type - Algorithm type (symmetric, asymmetric)
 * @property {string[]} supportedUsage - Supported key usages
 * @property {number} keySize - Key size in bits
 * @property {string} description - Algorithm description
 */

/**
 * @typedef {Object} SupportedAlgorithmsResponse
 * @property {SupportedAlgorithm[]} algorithms - List of supported algorithms
 */

/**
 * KMS (Key Management Service) client for QuicBaaS.
 *
 * Provides methods for managing cryptographic keys including generation,
 * import, rotation, revocation, and access policy management.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import KMSClient, { KeyAlgorithm, KeyUsage } from '@quicbaas/client/clients/kms';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const kms = new KMSClient(transport);
 *
 * // Generate a new encryption key
 * const { keyId, version } = await kms.generateKey({
 *   name: 'my-encryption-key',
 *   algorithm: KeyAlgorithm.AES_256_GCM,
 *   usage: [KeyUsage.ENCRYPT, KeyUsage.DECRYPT]
 * });
 *
 * // Rotate the key to a new version
 * const { newVersion } = await kms.rotateKey(keyId);
 *
 * // List all active keys
 * const { keys } = await kms.listKeys({ status: KeyStatus.ACTIVE });
 */
export class KMSClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new KMSClient instance.
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
   * Generate a new cryptographic key.
   *
   * @param {GenerateKeyRequest} request - Key generation request
   * @returns {Promise<GenerateKeyResponse>} Response with keyId, version, and algorithm
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const { keyId } = await kms.generateKey({
   *   name: 'database-encryption-key',
   *   algorithm: KeyAlgorithm.AES_256_GCM,
   *   usage: [KeyUsage.ENCRYPT, KeyUsage.DECRYPT],
   *   description: 'Key for encrypting sensitive data'
   * });
   */
  async generateKey(request) {
    this.#validateGenerateKeyRequest(request);

    const requestBody = {
      name: request.name,
      algorithm: request.algorithm,
    };

    if (request.usage !== undefined) {
      requestBody.usage = request.usage;
    }
    if (request.description !== undefined) {
      requestBody.description = request.description;
    }
    if (request.metadata !== undefined) {
      requestBody.metadata = request.metadata;
    }
    if (request.rotatable !== undefined) {
      requestBody.rotatable = request.rotatable;
    }
    if (request.rotationPeriodDays !== undefined) {
      requestBody.rotation_period_days = request.rotationPeriodDays;
    }

    const response = await this.#transport.post(
      `${KMS_API_PATH}/keys`,
      requestBody,
    );

    return this.#transformGenerateKeyResponse(response);
  }

  /**
   * Import an existing cryptographic key.
   *
   * @param {ImportKeyRequest} request - Key import request
   * @returns {Promise<ImportKeyResponse>} Response with keyId, version, and algorithm
   * @throws {ValidationError} If request parameters are invalid
   *
   * @example
   * const { keyId } = await kms.importKey({
   *   name: 'imported-key',
   *   algorithm: KeyAlgorithm.AES_256_GCM,
   *   keyMaterial: base64EncodedKeyMaterial,
   *   usage: [KeyUsage.ENCRYPT, KeyUsage.DECRYPT]
   * });
   */
  async importKey(request) {
    this.#validateImportKeyRequest(request);

    const requestBody = {
      name: request.name,
      algorithm: request.algorithm,
      key_material: request.keyMaterial,
    };

    if (request.usage !== undefined) {
      requestBody.usage = request.usage;
    }
    if (request.description !== undefined) {
      requestBody.description = request.description;
    }
    if (request.metadata !== undefined) {
      requestBody.metadata = request.metadata;
    }

    const response = await this.#transport.post(
      `${KMS_API_PATH}/keys/import`,
      requestBody,
    );

    return this.#transformImportKeyResponse(response);
  }

  /**
   * Get information about a key.
   *
   * @param {string} keyId - Unique key identifier
   * @returns {Promise<KeyInfo>} Key information
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const keyInfo = await kms.getKey('key-12345');
   * console.log(`Key ${keyInfo.name} is ${keyInfo.status}`);
   */
  async getKey(keyId) {
    requiredString(keyId, 'keyId');

    const response = await this.#transport.get(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}`,
    );

    return this.#transformKeyInfo(response);
  }

  /**
   * List all keys with optional filtering and pagination.
   *
   * @param {ListKeysOptions} [options={}] - List options
   * @returns {Promise<ListKeysResponse>} Response with keys and total count
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * // List all active keys
   * const { keys, totalCount } = await kms.listKeys({
   *   status: KeyStatus.ACTIVE,
   *   limit: 10
   * });
   *
   * // List keys with pagination
   * const { keys } = await kms.listKeys({ limit: 10, offset: 20 });
   */
  async listKeys(options = {}) {
    this.#validateListKeysOptions(options);

    const query = {};
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      query.offset = options.offset;
    }
    if (options.status !== undefined) {
      query.status = options.status;
    }
    if (options.algorithm !== undefined) {
      query.algorithm = options.algorithm;
    }

    const response = await this.#transport.get(
      `${KMS_API_PATH}/keys`,
      { query },
    );

    return this.#transformListKeysResponse(response);
  }

  /**
   * Get detailed metadata for a key.
   *
   * @param {string} keyId - Unique key identifier
   * @returns {Promise<KeyMetadata>} Key metadata
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const metadata = await kms.getKeyMetadata('key-12345');
   * console.log(`Custom metadata: ${JSON.stringify(metadata.customMetadata)}`);
   */
  async getKeyMetadata(keyId) {
    requiredString(keyId, 'keyId');

    const response = await this.#transport.get(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/metadata`,
    );

    return this.#transformKeyMetadata(response);
  }

  /**
   * List all versions of a key.
   *
   * @param {string} keyId - Unique key identifier
   * @returns {Promise<ListKeyVersionsResponse>} Response with key versions
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const { versions } = await kms.listKeyVersions('key-12345');
   * versions.forEach(v => {
   *   console.log(`Version ${v.version}: ${v.status}`);
   * });
   */
  async listKeyVersions(keyId) {
    requiredString(keyId, 'keyId');

    const response = await this.#transport.get(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/versions`,
    );

    return this.#transformListKeyVersionsResponse(response, keyId);
  }

  /**
   * Rotate a key to a new version.
   *
   * @param {string} keyId - Unique key identifier
   * @param {RotateKeyOptions} [options={}] - Rotation options
   * @returns {Promise<RotateKeyResponse>} Response with new version number
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const { newVersion, rotatedAt } = await kms.rotateKey('key-12345');
   * console.log(`Key rotated to version ${newVersion}`);
   */
  async rotateKey(keyId, options = {}) {
    requiredString(keyId, 'keyId');
    this.#validateRotateKeyOptions(options);

    const requestBody = {};
    if (options.destroyPrevious !== undefined) {
      requestBody.destroy_previous = options.destroyPrevious;
    }

    const response = await this.#transport.post(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/rotate`,
      requestBody,
    );

    return this.#transformRotateKeyResponse(response, keyId);
  }

  /**
   * Revoke a key, making it unusable for cryptographic operations.
   *
   * @param {string} keyId - Unique key identifier
   * @returns {Promise<RevokeKeyResponse>} Response indicating success
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const { success } = await kms.revokeKey('key-12345');
   * if (success) {
   *   console.log('Key has been revoked');
   * }
   */
  async revokeKey(keyId) {
    requiredString(keyId, 'keyId');

    const response = await this.#transport.post(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/revoke`,
      {},
    );

    return this.#transformRevokeKeyResponse(response);
  }

  /**
   * Delete a key permanently.
   *
   * Note: Keys may have a scheduled deletion period before permanent removal.
   *
   * @param {string} keyId - Unique key identifier
   * @returns {Promise<DeleteKeyResponse>} Response indicating success
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const { success, message } = await kms.deleteKey('key-12345');
   * console.log(message); // May indicate scheduled deletion date
   */
  async deleteKey(keyId) {
    requiredString(keyId, 'keyId');

    const response = await this.#transport.delete(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}`,
    );

    return this.#transformDeleteKeyResponse(response);
  }

  /**
   * Update key metadata.
   *
   * @param {string} keyId - Unique key identifier
   * @param {UpdateMetadataRequest} metadata - Metadata to update
   * @returns {Promise<KeyMetadata>} Updated key metadata
   * @throws {ValidationError} If keyId is empty or metadata is invalid
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const updated = await kms.updateKeyMetadata('key-12345', {
   *   description: 'Updated description',
   *   customMetadata: { environment: 'production' }
   * });
   */
  async updateKeyMetadata(keyId, metadata) {
    requiredString(keyId, 'keyId');
    requiredObject(metadata, 'metadata');
    this.#validateUpdateMetadataRequest(metadata);

    const requestBody = {};
    if (metadata.name !== undefined) {
      requestBody.name = metadata.name;
    }
    if (metadata.description !== undefined) {
      requestBody.description = metadata.description;
    }
    if (metadata.customMetadata !== undefined) {
      requestBody.custom_metadata = metadata.customMetadata;
    }
    if (metadata.rotatable !== undefined) {
      requestBody.rotatable = metadata.rotatable;
    }
    if (metadata.rotationPeriodDays !== undefined) {
      requestBody.rotation_period_days = metadata.rotationPeriodDays;
    }

    const response = await this.#transport.patch(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/metadata`,
      requestBody,
    );

    return this.#transformKeyMetadata(response);
  }

  /**
   * Update key status (enable/disable).
   *
   * @param {string} keyId - Unique key identifier
   * @param {string} status - New status (from KeyStatus enum)
   * @returns {Promise<UpdateStatusResponse>} Response with previous and new status
   * @throws {ValidationError} If keyId is empty or status is invalid
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * // Disable a key
   * const { previousStatus, newStatus } = await kms.updateKeyStatus(
   *   'key-12345',
   *   KeyStatus.DISABLED
   * );
   *
   * // Re-enable a key
   * await kms.updateKeyStatus('key-12345', KeyStatus.ACTIVE);
   */
  async updateKeyStatus(keyId, status) {
    requiredString(keyId, 'keyId');
    requiredString(status, 'status');
    oneOf(status, Object.values(KeyStatus), 'status');

    const requestBody = {
      status,
    };

    const response = await this.#transport.put(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/status`,
      requestBody,
    );

    return this.#transformUpdateStatusResponse(response, status);
  }

  /**
   * Set access policy for a key.
   *
   * @param {string} keyId - Unique key identifier
   * @param {SetPolicyRequest} policy - Policy to set
   * @returns {Promise<KeyPolicy>} Updated key policy
   * @throws {ValidationError} If keyId is empty or policy is invalid
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const policy = await kms.setKeyPolicy('key-12345', {
   *   statements: [{
   *     sid: 'AllowAdminAccess',
   *     effect: 'ALLOW',
   *     principals: ['user:admin'],
   *     actions: ['kms:*']
   *   }]
   * });
   */
  async setKeyPolicy(keyId, policy) {
    requiredString(keyId, 'keyId');
    requiredObject(policy, 'policy');
    this.#validateSetPolicyRequest(policy);

    const requestBody = {
      statements: policy.statements.map(stmt => ({
        sid: stmt.sid,
        effect: stmt.effect,
        principals: stmt.principals,
        actions: stmt.actions,
        conditions: stmt.conditions,
      })),
    };

    const response = await this.#transport.put(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/policy`,
      requestBody,
    );

    return this.#transformKeyPolicy(response, keyId);
  }

  /**
   * Get access policy for a key.
   *
   * @param {string} keyId - Unique key identifier
   * @returns {Promise<KeyPolicy>} Key policy
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const policy = await kms.getKeyPolicy('key-12345');
   * policy.statements.forEach(stmt => {
   *   console.log(`${stmt.sid}: ${stmt.effect} ${stmt.actions.join(', ')}`);
   * });
   */
  async getKeyPolicy(keyId) {
    requiredString(keyId, 'keyId');

    const response = await this.#transport.get(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/policy`,
    );

    return this.#transformKeyPolicy(response, keyId);
  }

  /**
   * Get list of supported cryptographic algorithms.
   *
   * @returns {Promise<SupportedAlgorithmsResponse>} Response with supported algorithms
   *
   * @example
   * const { algorithms } = await kms.getSupportedAlgorithms();
   * algorithms.forEach(algo => {
   *   console.log(`${algo.algorithm}: ${algo.description}`);
   * });
   */
  async getSupportedAlgorithms() {
    const response = await this.#transport.get(
      `${KMS_API_PATH}/algorithms`,
    );

    return this.#transformSupportedAlgorithmsResponse(response);
  }

  // Validation methods

  /**
   * Validate generate key request.
   *
   * @param {GenerateKeyRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateGenerateKeyRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');
    requiredString(request.algorithm, 'request.algorithm');
    oneOf(request.algorithm, Object.values(KeyAlgorithm), 'request.algorithm');

    if (request.usage !== undefined) {
      this.#validateKeyUsageArray(request.usage, 'request.usage');
    }
    if (request.description !== undefined && typeof request.description !== 'string') {
      throw new ValidationError('request.description must be a string', 'request.description');
    }
    if (request.metadata !== undefined && (typeof request.metadata !== 'object' || Array.isArray(request.metadata))) {
      throw new ValidationError('request.metadata must be an object', 'request.metadata');
    }
    if (request.rotatable !== undefined && typeof request.rotatable !== 'boolean') {
      throw new ValidationError('request.rotatable must be a boolean', 'request.rotatable');
    }
    if (request.rotationPeriodDays !== undefined) {
      positiveInteger(request.rotationPeriodDays, 'request.rotationPeriodDays');
    }
  }

  /**
   * Validate import key request.
   *
   * @param {ImportKeyRequest} request - Request to validate
   * @throws {ValidationError} If request is invalid
   * @private
   */
  #validateImportKeyRequest(request) {
    requiredObject(request, 'request');
    requiredString(request.name, 'request.name');
    requiredString(request.algorithm, 'request.algorithm');
    requiredString(request.keyMaterial, 'request.keyMaterial');
    oneOf(request.algorithm, Object.values(KeyAlgorithm), 'request.algorithm');

    if (request.usage !== undefined) {
      this.#validateKeyUsageArray(request.usage, 'request.usage');
    }
    if (request.description !== undefined && typeof request.description !== 'string') {
      throw new ValidationError('request.description must be a string', 'request.description');
    }
    if (request.metadata !== undefined && (typeof request.metadata !== 'object' || Array.isArray(request.metadata))) {
      throw new ValidationError('request.metadata must be an object', 'request.metadata');
    }
  }

  /**
   * Validate key usage array.
   *
   * @param {string[]} usage - Usage array to validate
   * @param {string} fieldName - Field name for error messages
   * @throws {ValidationError} If usage is invalid
   * @private
   */
  #validateKeyUsageArray(usage, fieldName) {
    if (!Array.isArray(usage)) {
      throw new ValidationError(`${fieldName} must be an array`, fieldName);
    }

    const validUsages = Object.values(KeyUsage);
    for (let i = 0; i < usage.length; i++) {
      if (typeof usage[i] !== 'string') {
        throw new ValidationError(`${fieldName}[${i}] must be a string`, `${fieldName}[${i}]`);
      }
      if (!validUsages.includes(usage[i])) {
        throw new ValidationError(
          `${fieldName}[${i}] must be one of: ${validUsages.join(', ')}`,
          `${fieldName}[${i}]`,
        );
      }
    }
  }

  /**
   * Validate list keys options.
   *
   * @param {ListKeysOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListKeysOptions(options) {
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
    }
    if (options.status !== undefined) {
      oneOf(options.status, Object.values(KeyStatus), 'options.status');
    }
    if (options.algorithm !== undefined) {
      oneOf(options.algorithm, Object.values(KeyAlgorithm), 'options.algorithm');
    }
  }

  /**
   * Validate rotate key options.
   *
   * @param {RotateKeyOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateRotateKeyOptions(options) {
    if (options.destroyPrevious !== undefined && typeof options.destroyPrevious !== 'boolean') {
      throw new ValidationError('options.destroyPrevious must be a boolean', 'options.destroyPrevious');
    }
  }

  /**
   * Validate update metadata request.
   *
   * @param {UpdateMetadataRequest} metadata - Metadata to validate
   * @throws {ValidationError} If metadata is invalid
   * @private
   */
  #validateUpdateMetadataRequest(metadata) {
    if (metadata.name !== undefined && (typeof metadata.name !== 'string' || metadata.name.trim() === '')) {
      throw new ValidationError('metadata.name must be a non-empty string', 'metadata.name');
    }
    if (metadata.description !== undefined && typeof metadata.description !== 'string') {
      throw new ValidationError('metadata.description must be a string', 'metadata.description');
    }
    if (metadata.customMetadata !== undefined && (typeof metadata.customMetadata !== 'object' || Array.isArray(metadata.customMetadata))) {
      throw new ValidationError('metadata.customMetadata must be an object', 'metadata.customMetadata');
    }
    if (metadata.rotatable !== undefined && typeof metadata.rotatable !== 'boolean') {
      throw new ValidationError('metadata.rotatable must be a boolean', 'metadata.rotatable');
    }
    if (metadata.rotationPeriodDays !== undefined) {
      positiveInteger(metadata.rotationPeriodDays, 'metadata.rotationPeriodDays');
    }
  }

  /**
   * Validate set policy request.
   *
   * @param {SetPolicyRequest} policy - Policy to validate
   * @throws {ValidationError} If policy is invalid
   * @private
   */
  #validateSetPolicyRequest(policy) {
    requiredArray(policy.statements, 'policy.statements');

    for (let i = 0; i < policy.statements.length; i++) {
      const stmt = policy.statements[i];
      const prefix = `policy.statements[${i}]`;

      if (!stmt || typeof stmt !== 'object') {
        throw new ValidationError(`${prefix} must be an object`, prefix);
      }
      requiredString(stmt.sid, `${prefix}.sid`);
      requiredString(stmt.effect, `${prefix}.effect`);
      oneOf(stmt.effect, ['ALLOW', 'DENY'], `${prefix}.effect`);
      requiredArray(stmt.principals, `${prefix}.principals`);
      requiredArray(stmt.actions, `${prefix}.actions`);

      for (let j = 0; j < stmt.principals.length; j++) {
        if (typeof stmt.principals[j] !== 'string' || stmt.principals[j].trim() === '') {
          throw new ValidationError(
            `${prefix}.principals[${j}] must be a non-empty string`,
            `${prefix}.principals[${j}]`,
          );
        }
      }

      for (let j = 0; j < stmt.actions.length; j++) {
        if (typeof stmt.actions[j] !== 'string' || stmt.actions[j].trim() === '') {
          throw new ValidationError(
            `${prefix}.actions[${j}] must be a non-empty string`,
            `${prefix}.actions[${j}]`,
          );
        }
      }

      if (stmt.conditions !== undefined && (typeof stmt.conditions !== 'object' || Array.isArray(stmt.conditions))) {
        throw new ValidationError(`${prefix}.conditions must be an object`, `${prefix}.conditions`);
      }
    }
  }

  // Response transformation methods

  /**
   * Transform generate key response.
   *
   * @param {Object} response - Raw server response
   * @returns {GenerateKeyResponse}
   * @private
   */
  #transformGenerateKeyResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      keyId: response.key_id || response.keyId || '',
      version: response.version || 1,
      algorithm: response.algorithm || '',
      createdAt: response.created_at || response.createdAt || 0,
    };
  }

  /**
   * Transform import key response.
   *
   * @param {Object} response - Raw server response
   * @returns {ImportKeyResponse}
   * @private
   */
  #transformImportKeyResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      keyId: response.key_id || response.keyId || '',
      version: response.version || 1,
      algorithm: response.algorithm || '',
      createdAt: response.created_at || response.createdAt || 0,
    };
  }

  /**
   * Transform key info from server response.
   *
   * @param {Object} key - Raw key object from server
   * @returns {KeyInfo}
   * @private
   */
  #transformKeyInfo(key) {
    if (!key) {
      throw new ValidationError('Invalid key data from server', 'key');
    }

    return {
      keyId: key.key_id || key.keyId || '',
      name: key.name || '',
      algorithm: key.algorithm || '',
      version: key.version || 1,
      status: key.status || KeyStatus.ACTIVE,
      usage: key.usage || [],
      createdAt: key.created_at || key.createdAt || 0,
      rotatedAt: key.rotated_at || key.rotatedAt || 0,
      description: key.description,
    };
  }

  /**
   * Transform list keys response.
   *
   * @param {Object} response - Raw server response
   * @returns {ListKeysResponse}
   * @private
   */
  #transformListKeysResponse(response) {
    const keys = (response?.keys || []).map(key => this.#transformKeyInfo(key));

    return {
      keys,
      totalCount: response?.total_count || response?.totalCount || keys.length,
    };
  }

  /**
   * Transform key metadata from server response.
   *
   * @param {Object} response - Raw server response
   * @returns {KeyMetadata}
   * @private
   */
  #transformKeyMetadata(response) {
    if (!response) {
      throw new ValidationError('Invalid metadata from server', 'response');
    }

    return {
      keyId: response.key_id || response.keyId || '',
      name: response.name || '',
      algorithm: response.algorithm || '',
      version: response.version || 1,
      status: response.status || KeyStatus.ACTIVE,
      usage: response.usage || [],
      description: response.description,
      customMetadata: response.custom_metadata || response.customMetadata || {},
      rotatable: response.rotatable || false,
      rotationPeriodDays: response.rotation_period_days || response.rotationPeriodDays,
      createdAt: response.created_at || response.createdAt || 0,
      updatedAt: response.updated_at || response.updatedAt || 0,
      rotatedAt: response.rotated_at || response.rotatedAt || 0,
    };
  }

  /**
   * Transform list key versions response.
   *
   * @param {Object} response - Raw server response
   * @param {string} keyId - Key identifier
   * @returns {ListKeyVersionsResponse}
   * @private
   */
  #transformListKeyVersionsResponse(response, keyId) {
    const versions = (response?.versions || []).map(v => ({
      version: v.version || 0,
      status: v.status || KeyStatus.ACTIVE,
      createdAt: v.created_at || v.createdAt || 0,
      destroyedAt: v.destroyed_at || v.destroyedAt,
    }));

    return {
      keyId: response?.key_id || response?.keyId || keyId,
      versions,
      totalCount: response?.total_count || response?.totalCount || versions.length,
    };
  }

  /**
   * Transform rotate key response.
   *
   * @param {Object} response - Raw server response
   * @param {string} keyId - Key identifier
   * @returns {RotateKeyResponse}
   * @private
   */
  #transformRotateKeyResponse(response, keyId) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      keyId: response.key_id || response.keyId || keyId,
      newVersion: response.new_version || response.newVersion || 0,
      rotatedAt: response.rotated_at || response.rotatedAt || 0,
    };
  }

  /**
   * Transform revoke key response.
   *
   * @param {Object} response - Raw server response
   * @returns {RevokeKeyResponse}
   * @private
   */
  #transformRevokeKeyResponse(response) {
    return {
      success: response?.success ?? true,
      message: response?.message || '',
    };
  }

  /**
   * Transform delete key response.
   *
   * @param {Object} response - Raw server response
   * @returns {DeleteKeyResponse}
   * @private
   */
  #transformDeleteKeyResponse(response) {
    return {
      success: response?.success ?? true,
      message: response?.message || '',
    };
  }

  /**
   * Transform update status response.
   *
   * @param {Object} response - Raw server response
   * @param {string} newStatus - New status that was set
   * @returns {UpdateStatusResponse}
   * @private
   */
  #transformUpdateStatusResponse(response, newStatus) {
    return {
      success: response?.success ?? true,
      previousStatus: response?.previous_status || response?.previousStatus || '',
      newStatus: response?.new_status || response?.newStatus || newStatus,
    };
  }

  /**
   * Transform key policy from server response.
   *
   * @param {Object} response - Raw server response
   * @param {string} keyId - Key identifier
   * @returns {KeyPolicy}
   * @private
   */
  #transformKeyPolicy(response, keyId) {
    const statements = (response?.statements || []).map(stmt => ({
      sid: stmt.sid || '',
      effect: stmt.effect || 'DENY',
      principals: stmt.principals || [],
      actions: stmt.actions || [],
      conditions: stmt.conditions,
    }));

    return {
      keyId: response?.key_id || response?.keyId || keyId,
      statements,
      version: response?.version || 1,
      updatedAt: response?.updated_at || response?.updatedAt || 0,
    };
  }

  // ===========================================================================
  // Rotation Policy Methods
  // ===========================================================================

  /**
   * Set or replace the automatic cryptoperiod rotation policy for a key.
   *
   * When autoRotate is true, intervalSeconds must be positive. The server
   * computes nextRotationAt as now + intervalSeconds if not supplied.
   *
   * @param {string} keyId - Unique key identifier
   * @param {Object} policy - Rotation policy configuration
   * @param {boolean} policy.autoRotate - Enable automatic rotation
   * @param {number} policy.intervalSeconds - Rotation interval in seconds (required when autoRotate=true)
   * @param {number} [policy.retainVersions=0] - Number of old versions to retain
   * @param {boolean} [policy.enabled=true] - Mark the policy as enabled
   * @returns {Promise<KeyRotationPolicyResponse>} The stored policy with computed nextRotationAt
   * @throws {ValidationError} If keyId is empty or autoRotate=true with intervalSeconds <= 0
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const policy = await kms.setKeyRotationPolicy('key-12345', {
   *   autoRotate: true,
   *   intervalSeconds: 90 * 24 * 60 * 60, // 90 days
   *   retainVersions: 2,
   * });
   * console.log('Next rotation at:', new Date(policy.nextRotationAt * 1000));
   */
  async setKeyRotationPolicy(keyId, policy = {}) {
    requiredString(keyId, 'keyId');
    if (policy.autoRotate && !(policy.intervalSeconds > 0)) {
      throw new Error('intervalSeconds must be positive when autoRotate is true');
    }

    const requestBody = {
      enabled: policy.enabled !== undefined ? policy.enabled : true,
      auto_rotate: Boolean(policy.autoRotate),
      interval_seconds: policy.intervalSeconds || 0,
      retain_versions: policy.retainVersions || 0,
    };

    const response = await this.#transport.put(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/rotation-policy`,
      requestBody,
    );

    return this.#transformRotationPolicyResponse(response, keyId);
  }

  /**
   * Retrieve the current automatic rotation policy for a key.
   *
   * @param {string} keyId - Unique key identifier
   * @returns {Promise<KeyRotationPolicyResponse|null>} The current policy, or null when none is configured
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * const policy = await kms.getKeyRotationPolicy('key-12345');
   * if (policy) {
   *   console.log('Auto-rotate:', policy.autoRotate);
   *   console.log('Interval:', policy.intervalSeconds, 'seconds');
   * }
   */
  async getKeyRotationPolicy(keyId) {
    requiredString(keyId, 'keyId');

    const response = await this.#transport.get(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/rotation-policy`,
    );

    if (!response || !response.policy) {
      return null;
    }

    return this.#transformRotationPolicyResponse(response, keyId);
  }

  /**
   * Remove the automatic rotation policy for a key, disabling auto-rotation.
   *
   * @param {string} keyId - Unique key identifier
   * @returns {Promise<{keyId: string, timestamp: number}>} Confirmation with timestamp
   * @throws {ValidationError} If keyId is empty
   * @throws {NotFoundError} If key does not exist
   *
   * @example
   * await kms.deleteKeyRotationPolicy('key-12345');
   * console.log('Auto-rotation disabled');
   */
  async deleteKeyRotationPolicy(keyId) {
    requiredString(keyId, 'keyId');

    const response = await this.#transport.delete(
      `${KMS_API_PATH}/keys/${encodeURIComponent(keyId)}/rotation-policy`,
    );

    return {
      keyId: response?.key_id || response?.keyId || keyId,
      timestamp: response?.timestamp || 0,
    };
  }

  /**
   * Transform a rotation policy response.
   *
   * @param {Object} response - Raw server response
   * @param {string} keyId - Key identifier for fallback
   * @returns {KeyRotationPolicyResponse}
   * @private
   */
  #transformRotationPolicyResponse(response, keyId) {
    const p = response?.policy || response;
    return {
      keyId: response?.key_id || response?.keyId || keyId,
      policy: p ? {
        enabled: Boolean(p.enabled),
        autoRotate: Boolean(p.auto_rotate),
        intervalSeconds: p.interval_seconds || 0,
        nextRotationAt: p.next_rotation_unix || 0,
        retainVersions: p.retain_versions || 0,
      } : null,
      timestamp: response?.timestamp || 0,
    };
  }

  /**
   * Transform supported algorithms response.
   *
   * @param {Object} response - Raw server response
   * @returns {SupportedAlgorithmsResponse}
   * @private
   */
  #transformSupportedAlgorithmsResponse(response) {
    const algorithms = (response?.algorithms || []).map(algo => ({
      algorithm: algo.algorithm || '',
      type: algo.type || '',
      supportedUsage: algo.supported_usage || algo.supportedUsage || [],
      keySize: algo.key_size || algo.keySize || 0,
      description: algo.description || '',
    }));

    return {
      algorithms,
    };
  }
}

export default KMSClient;
