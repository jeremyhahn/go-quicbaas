/**
 * QuicBaaS JavaScript SDK - Dynamic Secrets Client
 *
 * Provides dynamic secrets management operations including credential generation,
 * role management, and lease lifecycle operations.
 *
 * @module @quicbaas/client/clients/dynamicSecrets
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  requiredString,
  required,
  requiredObject,
  nonNegativeInteger,
  positiveInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * API base path for dynamic secrets endpoints.
 * @constant {string}
 */
const DYNAMIC_SECRETS_API_PATH = '/api/v1/dynamic-secrets';

/**
 * Credential types supported by the dynamic secrets engine.
 * @readonly
 * @enum {string}
 */
export const CredentialType = Object.freeze({
  /** Database credentials (PostgreSQL, MySQL, MongoDB, etc.) */
  DATABASE: 'database',
  /** AWS IAM credentials */
  AWS: 'aws',
  /** Azure service principal credentials */
  AZURE: 'azure',
  /** Google Cloud Platform service account credentials */
  GCP: 'gcp',
  /** SSH key pairs or certificates */
  SSH: 'ssh',
  /** PKI certificates */
  PKI: 'pki',
  /** LDAP credentials */
  LDAP: 'ldap',
});

/**
 * @typedef {Object} RoleConfig
 * @property {string} generatorType - The credential generator type (from CredentialType)
 * @property {Object<string, string>} config - Generator-specific configuration
 * @property {number} defaultTtl - Default TTL in milliseconds for generated credentials
 * @property {number} maxTtl - Maximum TTL in milliseconds for credentials
 */

/**
 * @typedef {Object} Role
 * @property {string} name - Unique role identifier
 * @property {string} generatorType - The credential generator type
 * @property {Object<string, string>} config - Generator-specific configuration
 * @property {number} defaultTtl - Default TTL in milliseconds
 * @property {number} maxTtl - Maximum TTL in milliseconds
 */

/**
 * @typedef {Object} CreateRoleResponse
 * @property {Role} role - The created role
 * @property {string} createdAt - ISO timestamp when the role was created
 */

/**
 * @typedef {Object} GetRoleResponse
 * @property {Role} role - The retrieved role
 */

/**
 * @typedef {Object} ListRolesOptions
 * @property {string} [prefix] - Filter roles by name prefix
 * @property {number} [limit] - Maximum number of roles to return
 * @property {number} [offset] - Number of roles to skip
 */

/**
 * @typedef {Object} ListRolesResponse
 * @property {Role[]} roles - List of roles
 * @property {number} count - Total number of roles matching the filter
 */

/**
 * @typedef {Object} DeleteRoleResponse
 * @property {string} deletedAt - ISO timestamp when the role was deleted
 */

/**
 * @typedef {Object} Credential
 * @property {string} credentialId - Unique credential identifier
 * @property {string} type - Credential type
 * @property {Object<string, string>} data - The actual credential data
 * @property {string} leaseId - Associated lease identifier
 * @property {string} createdAt - ISO timestamp when the credential was created
 * @property {string} expiresAt - ISO timestamp when the credential expires
 */

/**
 * @typedef {Object} Lease
 * @property {string} leaseId - Unique lease identifier
 * @property {string} secretPath - Path to the associated secret or resource
 * @property {string} issuedAt - ISO timestamp when the lease was issued
 * @property {string} expiresAt - ISO timestamp when the lease expires
 * @property {number} ttl - Time-to-live in milliseconds
 * @property {number} renewCount - Number of times this lease has been renewed
 * @property {number} maxRenewals - Maximum allowed renewals
 * @property {boolean} renewable - Whether the lease can be renewed
 * @property {string|null} revokedAt - ISO timestamp when revoked, or null
 * @property {Object<string, string>} [metadata] - Additional metadata
 */

/**
 * @typedef {Object} GenerateCredentialOptions
 * @property {number} [ttl] - Requested TTL in milliseconds for the credential
 */

/**
 * @typedef {Object} GenerateCredentialResponse
 * @property {Credential} credential - The generated credential
 * @property {Lease} lease - The associated lease
 * @property {string} leaseId - The lease identifier
 */

/**
 * @typedef {Object} RenewLeaseOptions
 * @property {number} [increment] - Duration in milliseconds to add to expiration
 */

/**
 * @typedef {Object} RenewLeaseResponse
 * @property {Lease} lease - The renewed lease
 * @property {string} renewedAt - ISO timestamp when the lease was renewed
 */

/**
 * @typedef {Object} RevokeLeaseResponse
 * @property {string} revokedAt - ISO timestamp when the lease was revoked
 */

/**
 * @typedef {Object} ListLeasesOptions
 * @property {string} [prefix] - Filter leases by path prefix
 * @property {number} [limit] - Maximum number of leases to return
 * @property {number} [offset] - Number of leases to skip
 */

/**
 * @typedef {Object} ListLeasesResponse
 * @property {Lease[]} leases - List of active leases
 * @property {number} count - Total number of leases matching the filter
 */

/**
 * Dynamic Secrets client for QuicBaaS.
 *
 * Provides methods for managing credential generation roles and dynamic
 * credential lifecycle including generation, renewal, and revocation.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import DynamicSecretsClient, { CredentialType } from '@quicbaas/client/clients/dynamicSecrets';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const dynamicSecrets = new DynamicSecretsClient(transport);
 *
 * // Create a database credential role
 * const { role } = await dynamicSecrets.createRole('postgres-readonly', {
 *   generatorType: CredentialType.DATABASE,
 *   config: {
 *     connection_string: 'postgres://admin:secret@localhost:5432/mydb',
 *     creation_statement: 'CREATE ROLE "{{name}}" WITH LOGIN PASSWORD \'{{password}}\' VALID UNTIL \'{{expiration}}\';',
 *   },
 *   defaultTtl: 3600000, // 1 hour
 *   maxTtl: 86400000,    // 24 hours
 * });
 *
 * // Generate a credential
 * const { credential, lease } = await dynamicSecrets.generateCredential('postgres-readonly');
 * console.log('Username:', credential.data.username);
 * console.log('Password:', credential.data.password);
 *
 * // Renew the lease before expiration
 * await dynamicSecrets.renewLease(lease.leaseId, { increment: 3600000 });
 *
 * // Revoke when done
 * await dynamicSecrets.revokeLease(lease.leaseId);
 */
class DynamicSecretsClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new DynamicSecretsClient instance.
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
   * Create a new credential generation role.
   *
   * @param {string} name - Unique name for the role
   * @param {RoleConfig} config - Role configuration
   * @returns {Promise<CreateRoleResponse>} Response with the created role
   * @throws {ValidationError} If required parameters are missing or invalid
   * @throws {AlreadyExistsError} If role name already exists
   *
   * @example
   * const { role, createdAt } = await dynamicSecrets.createRole('aws-readonly', {
   *   generatorType: CredentialType.AWS,
   *   config: {
   *     role_arn: 'arn:aws:iam::123456789:role/readonly',
   *   },
   *   defaultTtl: 3600000,
   *   maxTtl: 43200000,
   * });
   */
  async createRole(name, config) {
    requiredString(name, 'name');
    this.#validateRoleConfig(config);

    const requestBody = {
      name,
      generator_type: config.generatorType,
      config: config.config,
      default_ttl: config.defaultTtl,
      max_ttl: config.maxTtl,
    };

    const response = await this.#transport.post(
      `${DYNAMIC_SECRETS_API_PATH}/roles`,
      requestBody
    );

    return this.#transformCreateRoleResponse(response);
  }

  /**
   * Get a credential generation role by name.
   *
   * @param {string} name - Name of the role to retrieve
   * @returns {Promise<GetRoleResponse>} Response with the role
   * @throws {ValidationError} If name is empty
   * @throws {NotFoundError} If role does not exist
   *
   * @example
   * const { role } = await dynamicSecrets.getRole('postgres-readonly');
   * console.log('Generator type:', role.generatorType);
   */
  async getRole(name) {
    requiredString(name, 'name');

    const response = await this.#transport.get(
      `${DYNAMIC_SECRETS_API_PATH}/roles/${encodeURIComponent(name)}`
    );

    return this.#transformGetRoleResponse(response);
  }

  /**
   * List all credential generation roles.
   *
   * @param {ListRolesOptions} [options={}] - Filter and pagination options
   * @returns {Promise<ListRolesResponse>} Response with roles and count
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * // List all roles
   * const { roles, count } = await dynamicSecrets.listRoles();
   *
   * // List roles with prefix filter
   * const { roles } = await dynamicSecrets.listRoles({ prefix: 'postgres-' });
   */
  async listRoles(options = {}) {
    this.#validateListRolesOptions(options);

    const query = {};
    if (options.prefix !== undefined) {
      query.prefix = options.prefix;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      query.offset = options.offset;
    }

    const response = await this.#transport.get(
      `${DYNAMIC_SECRETS_API_PATH}/roles`,
      { query }
    );

    return this.#transformListRolesResponse(response);
  }

  /**
   * Delete a credential generation role.
   *
   * @param {string} name - Name of the role to delete
   * @returns {Promise<DeleteRoleResponse>} Response with deletion timestamp
   * @throws {ValidationError} If name is empty
   * @throws {NotFoundError} If role does not exist
   *
   * @example
   * const { deletedAt } = await dynamicSecrets.deleteRole('old-role');
   * console.log('Role deleted at:', deletedAt);
   */
  async deleteRole(name) {
    requiredString(name, 'name');

    const response = await this.#transport.delete(
      `${DYNAMIC_SECRETS_API_PATH}/roles/${encodeURIComponent(name)}`
    );

    return this.#transformDeleteRoleResponse(response);
  }

  /**
   * Generate a dynamic credential based on a role.
   *
   * @param {string} roleName - Name of the role to use for generation
   * @param {GenerateCredentialOptions} [options={}] - Generation options
   * @returns {Promise<GenerateCredentialResponse>} Response with credential and lease
   * @throws {ValidationError} If roleName is empty or options are invalid
   * @throws {NotFoundError} If role does not exist
   *
   * @example
   * // Generate with default TTL
   * const { credential, lease } = await dynamicSecrets.generateCredential('postgres-readonly');
   *
   * // Generate with custom TTL
   * const { credential } = await dynamicSecrets.generateCredential('postgres-readonly', {
   *   ttl: 7200000 // 2 hours
   * });
   */
  async generateCredential(roleName, options = {}) {
    requiredString(roleName, 'roleName');
    this.#validateGenerateCredentialOptions(options);

    const requestBody = {
      role_name: roleName,
    };

    if (options.ttl !== undefined) {
      requestBody.ttl = options.ttl;
    }

    const response = await this.#transport.post(
      `${DYNAMIC_SECRETS_API_PATH}/credentials`,
      requestBody
    );

    return this.#transformGenerateCredentialResponse(response);
  }

  /**
   * Renew a credential lease to extend its expiration time.
   *
   * @param {string} leaseId - ID of the lease to renew
   * @param {RenewLeaseOptions} [options={}] - Renewal options
   * @returns {Promise<RenewLeaseResponse>} Response with renewed lease
   * @throws {ValidationError} If leaseId is empty or options are invalid
   * @throws {NotFoundError} If lease does not exist
   *
   * @example
   * // Renew with default increment
   * const { lease, renewedAt } = await dynamicSecrets.renewLease('lease-123');
   *
   * // Renew with specific increment
   * const { lease } = await dynamicSecrets.renewLease('lease-123', {
   *   increment: 1800000 // 30 minutes
   * });
   */
  async renewLease(leaseId, options = {}) {
    requiredString(leaseId, 'leaseId');
    this.#validateRenewLeaseOptions(options);

    const requestBody = {
      lease_id: leaseId,
    };

    if (options.increment !== undefined) {
      requestBody.increment = options.increment;
    }

    const response = await this.#transport.post(
      `${DYNAMIC_SECRETS_API_PATH}/leases/${encodeURIComponent(leaseId)}/renew`,
      requestBody
    );

    return this.#transformRenewLeaseResponse(response);
  }

  /**
   * Revoke a credential lease immediately.
   *
   * This will invalidate the credential and release associated resources.
   *
   * @param {string} leaseId - ID of the lease to revoke
   * @returns {Promise<RevokeLeaseResponse>} Response with revocation timestamp
   * @throws {ValidationError} If leaseId is empty
   * @throws {NotFoundError} If lease does not exist
   *
   * @example
   * const { revokedAt } = await dynamicSecrets.revokeLease('lease-123');
   * console.log('Lease revoked at:', revokedAt);
   */
  async revokeLease(leaseId) {
    requiredString(leaseId, 'leaseId');

    const response = await this.#transport.post(
      `${DYNAMIC_SECRETS_API_PATH}/leases/${encodeURIComponent(leaseId)}/revoke`,
      { lease_id: leaseId }
    );

    return this.#transformRevokeLeaseResponse(response);
  }

  /**
   * List all active leases.
   *
   * @param {ListLeasesOptions} [options={}] - Filter and pagination options
   * @returns {Promise<ListLeasesResponse>} Response with leases and count
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * // List all leases
   * const { leases, count } = await dynamicSecrets.listLeases();
   *
   * // List leases with prefix filter
   * const { leases } = await dynamicSecrets.listLeases({ prefix: 'postgres/' });
   */
  async listLeases(options = {}) {
    this.#validateListLeasesOptions(options);

    const query = {};
    if (options.prefix !== undefined) {
      query.prefix = options.prefix;
    }
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      query.offset = options.offset;
    }

    const response = await this.#transport.get(
      `${DYNAMIC_SECRETS_API_PATH}/leases`,
      { query }
    );

    return this.#transformListLeasesResponse(response);
  }

  /**
   * Validate role configuration.
   *
   * @param {RoleConfig} config - Configuration to validate
   * @throws {ValidationError} If configuration is invalid
   * @private
   */
  #validateRoleConfig(config) {
    required(config, 'config');
    if (typeof config !== 'object' || Array.isArray(config)) {
      throw new ValidationError('config must be an object', 'config');
    }

    requiredString(config.generatorType, 'config.generatorType');
    oneOf(
      config.generatorType,
      Object.values(CredentialType),
      'config.generatorType'
    );

    requiredObject(config.config, 'config.config');

    required(config.defaultTtl, 'config.defaultTtl');
    positiveInteger(config.defaultTtl, 'config.defaultTtl');

    required(config.maxTtl, 'config.maxTtl');
    positiveInteger(config.maxTtl, 'config.maxTtl');

    if (config.defaultTtl > config.maxTtl) {
      throw new ValidationError(
        'config.defaultTtl cannot exceed config.maxTtl',
        'config.defaultTtl'
      );
    }
  }

  /**
   * Validate list roles options.
   *
   * @param {ListRolesOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListRolesOptions(options) {
    if (options.prefix !== undefined && typeof options.prefix !== 'string') {
      throw new ValidationError('options.prefix must be a string', 'options.prefix');
    }
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
    }
  }

  /**
   * Validate generate credential options.
   *
   * @param {GenerateCredentialOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateGenerateCredentialOptions(options) {
    if (options.ttl !== undefined) {
      if (typeof options.ttl !== 'number' || options.ttl < 0) {
        throw new ValidationError('options.ttl must be a non-negative number', 'options.ttl');
      }
    }
  }

  /**
   * Validate renew lease options.
   *
   * @param {RenewLeaseOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateRenewLeaseOptions(options) {
    if (options.increment !== undefined) {
      if (typeof options.increment !== 'number' || options.increment < 0) {
        throw new ValidationError(
          'options.increment must be a non-negative number',
          'options.increment'
        );
      }
    }
  }

  /**
   * Validate list leases options.
   *
   * @param {ListLeasesOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListLeasesOptions(options) {
    if (options.prefix !== undefined && typeof options.prefix !== 'string') {
      throw new ValidationError('options.prefix must be a string', 'options.prefix');
    }
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
    }
    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
    }
  }

  /**
   * Transform server create role response.
   *
   * @param {Object} response - Raw server response
   * @returns {CreateRoleResponse}
   * @private
   */
  #transformCreateRoleResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      role: this.#transformRole(response.role),
      createdAt: response.created_at || response.createdAt || new Date().toISOString(),
    };
  }

  /**
   * Transform server get role response.
   *
   * @param {Object} response - Raw server response
   * @returns {GetRoleResponse}
   * @private
   */
  #transformGetRoleResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      role: this.#transformRole(response.role || response),
    };
  }

  /**
   * Transform server list roles response.
   *
   * @param {Object} response - Raw server response
   * @returns {ListRolesResponse}
   * @private
   */
  #transformListRolesResponse(response) {
    const roles = (response?.roles || []).map(role => this.#transformRole(role));

    return {
      roles,
      count: response?.count ?? roles.length,
    };
  }

  /**
   * Transform server delete role response.
   *
   * @param {Object} response - Raw server response
   * @returns {DeleteRoleResponse}
   * @private
   */
  #transformDeleteRoleResponse(response) {
    return {
      deletedAt: response?.deleted_at || response?.deletedAt || new Date().toISOString(),
    };
  }

  /**
   * Transform a role object from server format.
   *
   * @param {Object} role - Raw role from server
   * @returns {Role}
   * @private
   */
  #transformRole(role) {
    if (!role) {
      return null;
    }

    return {
      name: role.name || '',
      generatorType: role.generator_type || role.generatorType || '',
      config: role.config || {},
      defaultTtl: role.default_ttl || role.defaultTtl || 0,
      maxTtl: role.max_ttl || role.maxTtl || 0,
    };
  }

  /**
   * Transform server generate credential response.
   *
   * @param {Object} response - Raw server response
   * @returns {GenerateCredentialResponse}
   * @private
   */
  #transformGenerateCredentialResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      credential: this.#transformCredential(response.credential),
      lease: this.#transformLease(response.lease),
      leaseId: response.lease_id || response.leaseId || response.lease?.lease_id || '',
    };
  }

  /**
   * Transform a credential object from server format.
   *
   * @param {Object} credential - Raw credential from server
   * @returns {Credential}
   * @private
   */
  #transformCredential(credential) {
    if (!credential) {
      return null;
    }

    return {
      credentialId: credential.credential_id || credential.credentialId || '',
      type: credential.type || '',
      data: credential.data || {},
      leaseId: credential.lease_id || credential.leaseId || '',
      createdAt: this.#transformTimestamp(credential.created_at || credential.createdAt),
      expiresAt: this.#transformTimestamp(credential.expires_at || credential.expiresAt),
    };
  }

  /**
   * Transform server renew lease response.
   *
   * @param {Object} response - Raw server response
   * @returns {RenewLeaseResponse}
   * @private
   */
  #transformRenewLeaseResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      lease: this.#transformLease(response.lease),
      renewedAt: response.renewed_at || response.renewedAt || new Date().toISOString(),
    };
  }

  /**
   * Transform server revoke lease response.
   *
   * @param {Object} response - Raw server response
   * @returns {RevokeLeaseResponse}
   * @private
   */
  #transformRevokeLeaseResponse(response) {
    return {
      revokedAt: response?.revoked_at || response?.revokedAt || new Date().toISOString(),
    };
  }

  /**
   * Transform server list leases response.
   *
   * @param {Object} response - Raw server response
   * @returns {ListLeasesResponse}
   * @private
   */
  #transformListLeasesResponse(response) {
    const leases = (response?.leases || []).map(lease => this.#transformLease(lease));

    return {
      leases,
      count: response?.count ?? leases.length,
    };
  }

  /**
   * Transform a lease object from server format.
   *
   * @param {Object} lease - Raw lease from server
   * @returns {Lease}
   * @private
   */
  #transformLease(lease) {
    if (!lease) {
      return null;
    }

    return {
      leaseId: lease.lease_id || lease.leaseId || '',
      secretPath: lease.secret_path || lease.secretPath || '',
      issuedAt: this.#transformTimestamp(lease.issued_at || lease.issuedAt),
      expiresAt: this.#transformTimestamp(lease.expires_at || lease.expiresAt),
      ttl: lease.ttl || 0,
      renewCount: lease.renew_count || lease.renewCount || 0,
      maxRenewals: lease.max_renewals || lease.maxRenewals || 0,
      renewable: lease.renewable ?? true,
      revokedAt: lease.revoked_at || lease.revokedAt
        ? this.#transformTimestamp(lease.revoked_at || lease.revokedAt)
        : null,
      metadata: lease.metadata || {},
    };
  }

  /**
   * Transform a timestamp to ISO string format.
   *
   * @param {string|number|Date} timestamp - Timestamp to transform
   * @returns {string} ISO formatted timestamp
   * @private
   */
  #transformTimestamp(timestamp) {
    if (!timestamp) {
      return new Date().toISOString();
    }
    if (typeof timestamp === 'string') {
      return timestamp;
    }
    if (typeof timestamp === 'number') {
      // Handle nanoseconds (Go time.Time) or milliseconds
      const ms = timestamp > 1e15 ? Math.floor(timestamp / 1e6) : timestamp;
      return new Date(ms).toISOString();
    }
    if (timestamp instanceof Date) {
      return timestamp.toISOString();
    }
    return new Date().toISOString();
  }
}

export default DynamicSecretsClient;
