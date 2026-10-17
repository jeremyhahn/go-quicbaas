/**
 * QuicBaaS JavaScript SDK - IAM Client
 *
 * @module @quicbaas/client/clients/iam
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  requiredString,
  required,
  requiredArray,
  positiveInteger,
  nonNegativeInteger,
  requiredObject,
} from '../utils/validation.js';

/**
 * Tenant status values.
 * @readonly
 * @enum {string}
 */
export const TenantStatus = Object.freeze({
  UNSPECIFIED: 'UNSPECIFIED',
  ACTIVE: 'ACTIVE',
  SUSPENDED: 'SUSPENDED',
  DELETED: 'DELETED',
});

/**
 * User status values.
 * @readonly
 * @enum {string}
 */
export const UserStatus = Object.freeze({
  UNSPECIFIED: 'UNSPECIFIED',
  ACTIVE: 'ACTIVE',
  SUSPENDED: 'SUSPENDED',
  PENDING_VERIFICATION: 'PENDING_VERIFICATION',
  DELETED: 'DELETED',
});

/**
 * Export format types.
 * @readonly
 * @enum {string}
 */
export const ExportFormat = Object.freeze({
  JSON: 'json',
  CSV: 'csv',
  XML: 'xml',
});

/**
 * Audit event types.
 * @readonly
 * @enum {string}
 */
export const AuditEventType = Object.freeze({
  AUTHENTICATION: 'authentication',
  AUTHORIZATION: 'authorization',
  USER_MANAGEMENT: 'user_management',
  ROLE_MANAGEMENT: 'role_management',
  POLICY_MANAGEMENT: 'policy_management',
  DATA_ACCESS: 'data_access',
  CONFIGURATION: 'configuration',
  SYSTEM: 'system',
  GDPR: 'gdpr',
  ADMIN: 'admin',
});

/**
 * @typedef {Object} Tenant
 * @property {string} id - Unique identifier
 * @property {string} name - Unique tenant name
 * @property {string} [displayName] - Human-readable display name
 * @property {string} [description] - Optional description
 * @property {string} status - Tenant status
 * @property {Object} [settings] - Tenant-specific settings
 * @property {Object} [quotas] - Resource limits
 * @property {Object} [metadata] - Additional metadata
 * @property {string} createdAt - Creation timestamp
 * @property {string} updatedAt - Last update timestamp
 */

/**
 * @typedef {Object} User
 * @property {string} id - Unique identifier
 * @property {string} tenantId - Tenant ID
 * @property {string} username - Unique username within tenant
 * @property {string} email - User email address
 * @property {string} [displayName] - Human-readable display name
 * @property {string} status - User status
 * @property {Array<string>} [roles] - Assigned role IDs
 * @property {Object} [attributes] - Custom user attributes for ABAC
 * @property {Object} [metadata] - Additional metadata
 * @property {boolean} mfaEnabled - Whether MFA is enabled
 * @property {boolean} emailVerified - Whether email is verified
 * @property {string} [lastLoginAt] - Last login timestamp
 * @property {string} createdAt - Creation timestamp
 * @property {string} updatedAt - Last update timestamp
 */

/**
 * @typedef {Object} Role
 * @property {string} id - Unique identifier
 * @property {string} tenantId - Tenant ID
 * @property {string} name - Unique name within tenant
 * @property {string} [displayName] - Human-readable display name
 * @property {string} [description] - Optional description
 * @property {Array<string>} permissions - Permissions granted by this role
 * @property {boolean} isSystem - Whether this is a system-defined role
 * @property {Object} [metadata] - Additional metadata
 * @property {string} createdAt - Creation timestamp
 * @property {string} updatedAt - Last update timestamp
 */

/**
 * @typedef {Object} Application
 * @property {string} clientId - Unique client identifier
 * @property {string} tenantId - Tenant ID
 * @property {string} name - Application display name
 * @property {string} [description] - Optional description
 * @property {string} type - Application type (web, native, spa, service)
 * @property {string} [clientSecret] - Client secret (only returned on creation/rotation)
 * @property {Array<string>} [redirectUris] - Allowed redirect URIs
 * @property {Array<string>} [grantTypes] - Allowed OAuth2 grant types
 * @property {Array<string>} [scopes] - Allowed scopes
 * @property {boolean} enabled - Whether the application is active
 * @property {Object} [metadata] - Additional metadata
 * @property {string} createdAt - Creation timestamp
 * @property {string} updatedAt - Last update timestamp
 */

/**
 * @typedef {Object} Session
 * @property {string} id - Session identifier
 * @property {string} userId - User ID
 * @property {string} tenantId - Tenant ID
 * @property {string} createdAt - Creation timestamp
 * @property {string} lastActive - Last activity timestamp
 * @property {string} expiresAt - Expiration timestamp
 * @property {boolean} mfaVerified - Whether MFA was completed
 * @property {string} ipAddress - IP address
 * @property {string} userAgent - User agent string
 * @property {string} authMethod - Authentication method
 */

/**
 * @typedef {Object} AuditEvent
 * @property {string} id - Event identifier
 * @property {string} tenantId - Tenant ID
 * @property {string} timestamp - Event timestamp
 * @property {string} eventType - Type of event
 * @property {string} action - Specific action performed
 * @property {string} outcome - Action outcome (success, failure, denied, error)
 * @property {string} severity - Severity level
 * @property {Object} [actor] - Who performed the action
 * @property {Object} [target] - What was acted upon
 * @property {Object} [resource] - Resource involved
 * @property {string} [correlationId] - Links related events
 * @property {string} [sessionId] - Session ID
 * @property {string} [message] - Human-readable description
 * @property {Object} [details] - Additional event-specific data
 */

/**
 * @typedef {Object} PaginationOptions
 * @property {number} [limit] - Maximum number of items to return
 * @property {number} [offset] - Number of items to skip
 */

/**
 * @typedef {Object} PaginatedResponse
 * @property {number} limit - Limit the server applied
 * @property {number} offset - Offset the server applied
 */

/**
 * IAM client for QuicBaaS.
 * Provides comprehensive Identity and Access Management operations including
 * tenant, user, group, role, policy, application, session, audit, and GDPR management.
 */
class IAMClient {
  /**
   * Path of the tenant collection; every tenant-scoped IAM route hangs off it.
   * @type {string}
   * @private
   */
  static TENANTS_PATH = '/api/v1/tenants';

  /**
   * Path of the audit log route.
   * @type {string}
   * @private
   */
  static AUDIT_PATH = '/api/v1/iam/audit';

  /**
   * Create a new IAM client.
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    required(transport, 'transport');
    this.#transport = transport;
  }

  /** @type {import('../transport/http.js').HttpTransport} */
  #transport;

  // ==========================================================================
  // Tenant Management
  // ==========================================================================

  /**
   * Create a new tenant.
   * @param {string} tenantId - Unique tenant ID
   * @param {string} name - Tenant name
   * @param {Object} [options={}] - Additional options
   * @param {Object} [options.settings] - Tenant settings
   * @param {Object} [options.quotas] - Tenant quotas
   * @param {string} [options.isolationType] - Engine isolation type, fixed at creation
   * @returns {Promise<Tenant>} The created tenant
   * @throws {ValidationError} If tenantId or name is not provided
   */
  async createTenant(tenantId, name, options = {}) {
    requiredString(tenantId, 'tenantId');
    requiredString(name, 'name');

    const body = {
      id: tenantId,
      name,
      settings: options.settings,
      quotas: options.quotas,
      isolation_type: options.isolationType,
    };

    return this.#transport.post(`${IAMClient.TENANTS_PATH}/`, body);
  }

  /**
   * Get a tenant by ID.
   * @param {string} tenantId - The tenant ID
   * @returns {Promise<Tenant>} The tenant
   * @throws {ValidationError} If tenantId is not provided
   */
  async getTenant(tenantId) {
    return this.#transport.get(this.#tenantPath(tenantId));
  }

  /**
   * Update a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {Object} updates - Fields to update
   * @param {string} [updates.name] - New name
   * @param {Object} [updates.settings] - Updated settings
   * @param {Object} [updates.quotas] - Updated quotas
   * @returns {Promise<Tenant>} The updated tenant
   * @throws {ValidationError} If tenantId is not provided
   */
  async updateTenant(tenantId, updates) {
    const path = this.#tenantPath(tenantId);
    requiredObject(updates, 'updates');

    const body = {
      name: updates.name,
      settings: updates.settings,
      quotas: updates.quotas,
    };

    return this.#transport.put(path, body);
  }

  /**
   * Delete a tenant.
   * @param {string} tenantId - The tenant ID
   * @returns {Promise<void>}
   * @throws {ValidationError} If tenantId is not provided
   */
  async deleteTenant(tenantId) {
    await this.#transport.delete(this.#tenantPath(tenantId));
  }

  /**
   * List tenants.
   * @param {PaginationOptions} [options={}] - Pagination options
   * @returns {Promise<{tenants: Array<Tenant>} & PaginatedResponse>} List of tenants
   */
  async listTenants(options = {}) {
    const query = this.#buildPaginationQuery(options);
    return this.#transport.get(`${IAMClient.TENANTS_PATH}/`, { query });
  }

  // ==========================================================================
  // User Management
  // ==========================================================================

  /**
   * Create a new user in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {string} email - User email address
   * @param {Object} [options={}] - Additional options
   * @param {string} [options.username] - Username, unique within the tenant
   * @param {string} [options.name] - Display name
   * @param {Object<string, string>} [options.attributes] - ABAC attributes
   * @param {boolean} [options.enabled] - Whether the user is enabled
   * @param {Array<string>} [options.authMethods] - Permitted authentication methods
   * @param {Array<string>} [options.roles] - Initial role IDs
   * @param {string} [options.lifecycle] - Initial lifecycle state
   * @param {number} [options.inviteExpiryHours] - Invite token lifetime in hours
   * @returns {Promise<User>} The created user, with any invite token
   * @throws {ValidationError} If required parameters are missing
   */
  async createUser(tenantId, email, options = {}) {
    const path = `${this.#tenantPath(tenantId)}/users/`;
    requiredString(email, 'email');

    const body = {
      email,
      username: options.username,
      name: options.name,
      attributes: options.attributes,
      enabled: options.enabled,
      auth_methods: options.authMethods,
      roles: options.roles,
      lifecycle: options.lifecycle,
      invite_expiry_hours: options.inviteExpiryHours,
    };

    return this.#transport.post(path, body);
  }

  /**
   * Get a user by ID.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @returns {Promise<User>} The user
   * @throws {ValidationError} If required parameters are missing
   */
  async getUser(tenantId, userId) {
    return this.#transport.get(this.#userPath(tenantId, userId));
  }

  /**
   * Update a user.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {Object} updates - Fields to update
   * @param {string} [updates.email] - New email
   * @param {string} [updates.username] - New username
   * @param {string} [updates.name] - New display name
   * @param {Object<string, string>} [updates.attributes] - Replacement ABAC attributes
   * @param {boolean} [updates.enabled] - Whether the user is enabled
   * @param {Array<string>} [updates.roles] - Replacement role IDs
   * @param {Array<string>} [updates.authMethods] - Replacement authentication methods
   * @param {string} [updates.lifecycle] - New lifecycle state
   * @returns {Promise<User>} The updated user
   * @throws {ValidationError} If required parameters are missing
   */
  async updateUser(tenantId, userId, updates) {
    const path = this.#userPath(tenantId, userId);
    requiredObject(updates, 'updates');

    const body = {
      email: updates.email,
      username: updates.username,
      name: updates.name,
      attributes: updates.attributes,
      enabled: updates.enabled,
      roles: updates.roles,
      auth_methods: updates.authMethods,
      lifecycle: updates.lifecycle,
    };

    return this.#transport.put(path, body);
  }

  /**
   * Delete a user.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @returns {Promise<void>}
   * @throws {ValidationError} If required parameters are missing
   */
  async deleteUser(tenantId, userId) {
    await this.#transport.delete(this.#userPath(tenantId, userId));
  }

  /**
   * List users in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {PaginationOptions} [options={}] - Pagination options
   * @returns {Promise<{users: Array<User>} & PaginatedResponse>} List of users
   * @throws {ValidationError} If tenantId is not provided
   */
  async listUsers(tenantId, options = {}) {
    const path = `${this.#tenantPath(tenantId)}/users/`;
    const query = this.#buildPaginationQuery(options);
    return this.#transport.get(path, { query });
  }

  // ==========================================================================
  // Role Management
  // ==========================================================================

  /**
   * Create a new role in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {string} name - Unique role name within the tenant
   * @param {Array<string>} permissions - Permissions granted by this role
   * @param {Object} [options={}] - Additional options
   * @param {string} [options.id] - Role ID (the server assigns one when omitted)
   * @param {string} [options.description] - Optional description
   * @param {Object} [options.attributes] - Role attributes
   * @returns {Promise<Role>} The created role
   * @throws {ValidationError} If required parameters are missing
   */
  async createRole(tenantId, name, permissions, options = {}) {
    const path = `${this.#tenantPath(tenantId)}/roles/`;
    requiredString(name, 'name');
    requiredArray(permissions, 'permissions');

    if (permissions.length === 0) {
      throw new ValidationError('At least one permission is required', 'permissions');
    }

    const body = {
      id: options.id,
      name,
      description: options.description,
      permissions,
      attributes: options.attributes,
    };

    return this.#transport.post(path, body);
  }

  /**
   * Update a role.
   * @param {string} tenantId - The tenant ID
   * @param {string} roleId - The role ID
   * @param {Object} updates - Fields to update
   * @param {string} [updates.name] - New name
   * @param {string} [updates.description] - New description
   * @param {Array<string>} [updates.permissions] - New permissions (replaces existing)
   * @param {Object} [updates.attributes] - New attributes (replaces existing)
   * @returns {Promise<Role>} The updated role
   * @throws {ValidationError} If required parameters are missing
   */
  async updateRole(tenantId, roleId, updates) {
    const tenantPath = this.#tenantPath(tenantId);
    requiredString(roleId, 'roleId');
    requiredObject(updates, 'updates');

    const body = {
      name: updates.name,
      description: updates.description,
      permissions: updates.permissions,
      attributes: updates.attributes,
    };

    return this.#transport.put(`${tenantPath}/roles/${encodeURIComponent(roleId)}`, body);
  }

  /**
   * List roles in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {PaginationOptions} [options={}] - Pagination options
   * @returns {Promise<{roles: Array<Role>} & PaginatedResponse>} List of roles
   * @throws {ValidationError} If tenantId is not provided
   */
  async listRoles(tenantId, options = {}) {
    const path = `${this.#tenantPath(tenantId)}/roles/`;
    const query = this.#buildPaginationQuery(options);
    return this.#transport.get(path, { query });
  }

  /**
   * Assign a role to a user.
   *
   * quicbaasd serves user-role assignment under `/api/v1/tenants`, not under
   * {@link IAMClient.API_PREFIX}.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {string} roleId - The role ID to assign
   * @returns {Promise<void>}
   * @throws {ValidationError} If required parameters are missing
   */
  async assignRole(tenantId, userId, roleId) {
    requiredString(tenantId, 'tenantId');
    requiredString(userId, 'userId');
    requiredString(roleId, 'roleId');
    await this.#transport.post(
      `/api/v1/tenants/${encodeURIComponent(tenantId)}/users/${encodeURIComponent(userId)}/roles`,
      { role_id: roleId },
    );
  }

  /**
   * Revoke a role from a user.
   *
   * quicbaasd serves user-role revocation under `/api/v1/tenants`, not under
   * {@link IAMClient.API_PREFIX}.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {string} roleId - The role ID to revoke
   * @returns {Promise<void>}
   * @throws {ValidationError} If required parameters are missing
   */
  async revokeRole(tenantId, userId, roleId) {
    requiredString(tenantId, 'tenantId');
    requiredString(userId, 'userId');
    requiredString(roleId, 'roleId');
    await this.#transport.delete(
      `/api/v1/tenants/${encodeURIComponent(tenantId)}/users/${encodeURIComponent(userId)}/roles/${encodeURIComponent(roleId)}`,
    );
  }

  // ==========================================================================
  // Policy Management
  // ==========================================================================

  /**
   * Create an access policy in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {Object} policy - The policy definition, sent as is
   * @param {string} policy.name - Unique policy name
   * @param {Array<Object>} policy.rules - Policy rules
   * @param {string} [policy.description] - Policy description
   * @param {boolean} [policy.enabled] - Whether the policy is active
   * @param {number} [policy.priority] - Evaluation priority
   * @param {Object} [policy.labels] - Arbitrary metadata labels
   * @returns {Promise<Object>} The created policy
   * @throws {ValidationError} If required parameters are missing
   */
  async setPolicy(tenantId, policy) {
    const path = `${this.#tenantPath(tenantId)}/policies/`;
    requiredObject(policy, 'policy');
    requiredString(policy.name, 'policy.name');
    requiredArray(policy.rules, 'policy.rules');

    if (policy.rules.length === 0) {
      throw new ValidationError('At least one rule is required', 'policy.rules');
    }

    return this.#transport.post(path, policy);
  }

  /**
   * Get one access policy, or list the tenant's policies.
   * @param {string} tenantId - The tenant ID
   * @param {string} [policyName] - Policy name (lists all policies if not provided)
   * @returns {Promise<Object>} The policy, or `{policies, limit, offset}`
   * @throws {ValidationError} If tenantId is not provided
   */
  async getPolicy(tenantId, policyName) {
    const path = `${this.#tenantPath(tenantId)}/policies/`;
    if (policyName) {
      return this.#transport.get(`${path}${encodeURIComponent(policyName)}`);
    }
    return this.#transport.get(path);
  }

  // ==========================================================================
  // Application Management
  // ==========================================================================

  /**
   * Register a new OAuth2 application.
   * @param {string} tenantId - The tenant ID
   * @param {string} name - Application name
   * @param {Array<string>} redirectUris - Allowed redirect URIs (at least one)
   * @param {Object} [options={}] - Additional options
   * @param {string} [options.resourcePrefix] - Resource prefix the app is scoped to
   * @param {Array<string>} [options.grantTypes] - Allowed OAuth2 grant types
   * @param {Array<string>} [options.responseTypes] - Allowed OAuth2 response types
   * @param {boolean} [options.pkceRequired] - Whether PKCE is required
   * @returns {Promise<Application>} The created application with client secret
   * @throws {ValidationError} If required parameters are missing
   */
  async createApp(tenantId, name, redirectUris, options = {}) {
    const path = `${this.#tenantPath(tenantId)}/apps/`;
    requiredString(name, 'name');
    requiredArray(redirectUris, 'redirectUris');

    if (redirectUris.length === 0) {
      throw new ValidationError('At least one redirect URI is required', 'redirectUris');
    }

    const body = {
      name,
      redirect_uris: redirectUris,
      resource_prefix: options.resourcePrefix,
      grant_types: options.grantTypes,
      response_types: options.responseTypes,
      pkce_required: options.pkceRequired ?? false,
    };

    return this.#transport.post(path, body);
  }

  /**
   * Get an application by client ID.
   * @param {string} tenantId - The tenant ID
   * @param {string} appId - The application client ID
   * @returns {Promise<Application>} The application
   * @throws {ValidationError} If required parameters are missing
   */
  async getApp(tenantId, appId) {
    return this.#transport.get(this.#appPath(tenantId, appId));
  }

  /**
   * Delete an application.
   * @param {string} tenantId - The tenant ID
   * @param {string} appId - The application client ID
   * @returns {Promise<void>}
   * @throws {ValidationError} If required parameters are missing
   */
  async deleteApp(tenantId, appId) {
    await this.#transport.delete(this.#appPath(tenantId, appId));
  }

  /**
   * List applications in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {PaginationOptions} [options={}] - Pagination options
   * @returns {Promise<{applications: Array<Application>} & PaginatedResponse>} List of applications
   * @throws {ValidationError} If tenantId is not provided
   */
  async listApps(tenantId, options = {}) {
    const path = `${this.#tenantPath(tenantId)}/apps/`;
    const query = this.#buildPaginationQuery(options);
    return this.#transport.get(path, { query });
  }

  // ==========================================================================
  // Session Management
  // ==========================================================================

  /**
   * Create a session for a user.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {string} tokenHash - Hash of the token the session is bound to
   * @param {Object} [options={}] - Session options
   * @param {string} [options.id] - Session ID (the server assigns one when omitted)
   * @param {boolean} [options.mfaVerified] - Whether MFA was verified
   * @param {string} [options.ipAddress] - Client IP address
   * @param {string} [options.userAgent] - Client user agent
   * @param {Object} [options.deviceInfo] - Device id, name, type, fingerprint, trusted
   * @param {string} [options.expiresAt] - Expiry (RFC 3339); the server defaults to 24 hours
   * @returns {Promise<Session>} The created session
   * @throws {ValidationError} If required parameters are missing
   */
  async createSession(tenantId, userId, tokenHash, options = {}) {
    const path = `${this.#userPath(tenantId, userId)}/sessions/`;
    requiredString(tokenHash, 'tokenHash');

    const body = {
      id: options.id,
      token_hash: tokenHash,
      mfa_verified: options.mfaVerified ?? false,
      ip_address: options.ipAddress,
      user_agent: options.userAgent,
      device_info: options.deviceInfo,
      expires_at: options.expiresAt,
    };

    return this.#transport.post(path, body);
  }

  /**
   * List a user's sessions.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {Object} [options={}] - List options
   * @param {number} [options.limit] - Maximum number of sessions to return
   * @returns {Promise<{sessions: Array<Session>}>} List of sessions
   * @throws {ValidationError} If required parameters are missing
   */
  async listSessions(tenantId, userId, options = {}) {
    const path = `${this.#userPath(tenantId, userId)}/sessions/`;

    const query = {};
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
      query.limit = options.limit;
    }

    return this.#transport.get(path, { query });
  }

  // ==========================================================================
  // Audit Log
  // ==========================================================================

  /**
   * Get audit log entries for a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {Object} [options={}] - Query options
   * @param {string} [options.eventType] - Filter by event type
   * @param {string} [options.category] - Filter by category
   * @param {string} [options.outcome] - Filter by outcome
   * @param {string} [options.actorId] - Filter by actor ID
   * @param {string} [options.resourceId] - Filter by resource ID
   * @param {string} [options.startTime] - Start of time range (RFC 3339)
   * @param {string} [options.endTime] - End of time range (RFC 3339)
   * @param {number} [options.limit] - Maximum number of events to return
   * @param {number} [options.offset] - Number of events to skip
   * @returns {Promise<{events: Array<AuditEvent>, total: number} & PaginatedResponse>} Audit events
   * @throws {ValidationError} If tenantId is not provided
   */
  async getAuditLog(tenantId, options = {}) {
    requiredString(tenantId, 'tenantId');

    const query = { tenant_id: tenantId, ...this.#buildPaginationQuery(options) };
    const filters = {
      event_type: options.eventType,
      category: options.category,
      outcome: options.outcome,
      actor_id: options.actorId,
      resource_id: options.resourceId,
      start_time: options.startTime,
      end_time: options.endTime,
    };
    for (const [key, value] of Object.entries(filters)) {
      if (value) {
        query[key] = value;
      }
    }

    return this.#transport.get(IAMClient.AUDIT_PATH, { query });
  }

  // ==========================================================================
  // GDPR Operations
  // ==========================================================================

  /**
   * Request an export of a user's data (GDPR right of access).
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {Object} [options={}] - Export options
   * @param {string} [options.format] - Export format (json, csv, xml); defaults to json
   * @param {string} [options.requestedBy] - Who requested the export
   * @param {Array<string>} [options.includeCategories] - Data categories to include
   * @param {Array<string>} [options.excludeCategories] - Data categories to exclude
   * @returns {Promise<Object>} The export request
   * @throws {ValidationError} If required parameters are missing
   */
  async exportUserData(tenantId, userId, options = {}) {
    const path = `${this.#userPath(tenantId, userId)}/gdpr/export`;

    const body = {
      format: options.format || ExportFormat.JSON,
      requested_by: options.requestedBy,
      include_categories: options.includeCategories,
      exclude_categories: options.excludeCategories,
    };

    return this.#transport.post(path, body);
  }

  /**
   * Request deletion of a user's data (GDPR right to erasure).
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {Object} [options={}] - Deletion options
   * @param {string} [options.requestedBy] - Who requested the deletion
   * @param {string} [options.reason] - Reason for deletion
   * @param {Array<string>} [options.deleteCategories] - Data categories to delete
   * @param {Array<string>} [options.preserveCategories] - Data categories to keep (legal holds)
   * @param {number} [options.gracePeriodDays] - Days before the deletion runs
   * @returns {Promise<Object>} The deletion request
   * @throws {ValidationError} If required parameters are missing
   */
  async deleteUserData(tenantId, userId, options = {}) {
    const path = `${this.#userPath(tenantId, userId)}/gdpr/deletion`;

    const body = {
      requested_by: options.requestedBy,
      reason: options.reason,
      delete_categories: options.deleteCategories,
      preserve_categories: options.preserveCategories,
      grace_period_days: options.gracePeriodDays,
    };

    return this.#transport.post(path, body);
  }

  // ==========================================================================
  // Private Helper Methods
  // ==========================================================================

  /**
   * Build the path of one tenant.
   * @param {string} tenantId - The tenant ID
   * @returns {string} The tenant path
   * @throws {ValidationError} If tenantId is not provided
   * @private
   */
  #tenantPath(tenantId) {
    requiredString(tenantId, 'tenantId');
    return `${IAMClient.TENANTS_PATH}/${encodeURIComponent(tenantId)}`;
  }

  /**
   * Build the path of one user in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @returns {string} The user path
   * @throws {ValidationError} If tenantId or userId is not provided
   * @private
   */
  #userPath(tenantId, userId) {
    const tenantPath = this.#tenantPath(tenantId);
    requiredString(userId, 'userId');
    return `${tenantPath}/users/${encodeURIComponent(userId)}`;
  }

  /**
   * Build the path of one application in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {string} appId - The application client ID
   * @returns {string} The application path
   * @throws {ValidationError} If tenantId or appId is not provided
   * @private
   */
  #appPath(tenantId, appId) {
    const tenantPath = this.#tenantPath(tenantId);
    requiredString(appId, 'appId');
    return `${tenantPath}/apps/${encodeURIComponent(appId)}`;
  }

  /**
   * Build pagination query parameters.
   * @param {PaginationOptions} options - Pagination options
   * @returns {Object} Query parameters object
   * @private
   */
  #buildPaginationQuery(options) {
    const query = {};

    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
      query.offset = options.offset;
    }

    return query;
  }
}

export default IAMClient;
