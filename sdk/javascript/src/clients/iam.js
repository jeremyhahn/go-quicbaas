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
 * @property {Array<string>} [groups] - Group IDs user belongs to
 * @property {Object} [attributes] - Custom user attributes for ABAC
 * @property {Object} [metadata] - Additional metadata
 * @property {boolean} mfaEnabled - Whether MFA is enabled
 * @property {boolean} emailVerified - Whether email is verified
 * @property {string} [lastLoginAt] - Last login timestamp
 * @property {string} createdAt - Creation timestamp
 * @property {string} updatedAt - Last update timestamp
 */

/**
 * @typedef {Object} Group
 * @property {string} id - Unique identifier
 * @property {string} tenantId - Tenant ID
 * @property {string} name - Unique name within tenant
 * @property {string} [displayName] - Human-readable display name
 * @property {string} [description] - Optional description
 * @property {Array<string>} [roles] - Assigned role IDs
 * @property {number} memberCount - Number of members
 * @property {Object} [metadata] - Additional metadata
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
 * @property {number} [pageSize] - Maximum number of items to return
 * @property {string} [pageToken] - Token for the next page
 * @property {string} [filter] - Optional filter expression
 */

/**
 * @typedef {Object} PaginatedResponse
 * @property {Object} pagination - Pagination information
 * @property {number} pagination.totalCount - Total number of items
 * @property {number} pagination.pageSize - Items per page
 * @property {string} [pagination.nextPageToken] - Token for next page
 * @property {boolean} pagination.hasMore - Whether more results exist
 */

/**
 * IAM client for QuicBaaS.
 * Provides comprehensive Identity and Access Management operations including
 * tenant, user, group, role, policy, application, session, audit, and GDPR management.
 */
class IAMClient {
  /**
   * API path prefix for IAM operations.
   * @type {string}
   * @private
   */
  static API_PREFIX = '/api/v1/iam';

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
   * @param {string} name - Unique tenant name
   * @param {Object} [options={}] - Additional options
   * @param {string} [options.displayName] - Human-readable display name
   * @param {string} [options.description] - Optional description
   * @param {Object} [options.settings] - Tenant-specific settings
   * @param {Object} [options.metadata] - Additional metadata
   * @returns {Promise<Tenant>} The created tenant
   * @throws {ValidationError} If name is not provided
   */
  async createTenant(name, options = {}) {
    requiredString(name, 'name');

    const body = {
      name,
      display_name: options.displayName,
      description: options.description,
      settings: options.settings,
      metadata: options.metadata,
    };

    return this.#transport.post(`${IAMClient.API_PREFIX}/tenants`, body);
  }

  /**
   * Get a tenant by ID.
   * @param {string} tenantId - The tenant ID
   * @returns {Promise<Tenant>} The tenant
   * @throws {ValidationError} If tenantId is not provided
   */
  async getTenant(tenantId) {
    requiredString(tenantId, 'tenantId');
    return this.#transport.get(`${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}`);
  }

  /**
   * Update a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {Object} updates - Fields to update
   * @param {string} [updates.displayName] - New display name
   * @param {string} [updates.description] - New description
   * @param {Object} [updates.settings] - Updated settings
   * @param {Object} [updates.metadata] - Updated metadata
   * @param {string} [updates.status] - New status (for suspend/unsuspend)
   * @returns {Promise<Tenant>} The updated tenant
   * @throws {ValidationError} If tenantId is not provided
   */
  async updateTenant(tenantId, updates) {
    requiredString(tenantId, 'tenantId');
    requiredObject(updates, 'updates');

    const body = {
      display_name: updates.displayName,
      description: updates.description,
      settings: updates.settings,
      metadata: updates.metadata,
      status: updates.status,
    };

    return this.#transport.put(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}`,
      body
    );
  }

  /**
   * Delete a tenant.
   * @param {string} tenantId - The tenant ID
   * @returns {Promise<void>}
   * @throws {ValidationError} If tenantId is not provided
   */
  async deleteTenant(tenantId) {
    requiredString(tenantId, 'tenantId');
    await this.#transport.delete(`${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}`);
  }

  /**
   * List tenants with pagination.
   * @param {PaginationOptions} [options={}] - Pagination options
   * @returns {Promise<{tenants: Array<Tenant>} & PaginatedResponse>} List of tenants
   */
  async listTenants(options = {}) {
    const query = this.#buildPaginationQuery(options);
    return this.#transport.get(`${IAMClient.API_PREFIX}/tenants`, { query });
  }

  // ==========================================================================
  // User Management
  // ==========================================================================

  /**
   * Create a new user in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {string} username - Unique username within the tenant
   * @param {Object} [options={}] - Additional options
   * @param {string} options.email - User email address (required)
   * @param {string} options.password - User password (required)
   * @param {string} [options.displayName] - Human-readable display name
   * @param {Array<string>} [options.roles] - Initial role IDs to assign
   * @param {Array<string>} [options.groups] - Initial group IDs to add user to
   * @param {Object} [options.metadata] - Additional metadata
   * @param {boolean} [options.requireEmailVerification] - Require email verification
   * @returns {Promise<User>} The created user
   * @throws {ValidationError} If required parameters are missing
   */
  async createUser(tenantId, username, options = {}) {
    requiredString(tenantId, 'tenantId');
    requiredString(username, 'username');
    requiredString(options.email, 'options.email');
    requiredString(options.password, 'options.password');

    const body = {
      username,
      email: options.email,
      password: options.password,
      display_name: options.displayName,
      roles: options.roles,
      groups: options.groups,
      metadata: options.metadata,
      require_email_verification: options.requireEmailVerification,
    };

    return this.#transport.post(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/users`,
      body
    );
  }

  /**
   * Get a user by ID.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @returns {Promise<User>} The user
   * @throws {ValidationError} If required parameters are missing
   */
  async getUser(tenantId, userId) {
    requiredString(tenantId, 'tenantId');
    requiredString(userId, 'userId');
    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/users/${encodeURIComponent(userId)}`
    );
  }

  /**
   * Update a user.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {Object} updates - Fields to update
   * @param {string} [updates.username] - New username
   * @param {string} [updates.email] - New email
   * @param {string} [updates.displayName] - New display name
   * @param {string} [updates.password] - New password
   * @param {Object} [updates.metadata] - Updated metadata
   * @returns {Promise<User>} The updated user
   * @throws {ValidationError} If required parameters are missing
   */
  async updateUser(tenantId, userId, updates) {
    requiredString(tenantId, 'tenantId');
    requiredString(userId, 'userId');
    requiredObject(updates, 'updates');

    const body = {
      username: updates.username,
      email: updates.email,
      display_name: updates.displayName,
      password: updates.password,
      metadata: updates.metadata,
    };

    return this.#transport.put(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/users/${encodeURIComponent(userId)}`,
      body
    );
  }

  /**
   * Delete a user.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @returns {Promise<void>}
   * @throws {ValidationError} If required parameters are missing
   */
  async deleteUser(tenantId, userId) {
    requiredString(tenantId, 'tenantId');
    requiredString(userId, 'userId');
    await this.#transport.delete(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/users/${encodeURIComponent(userId)}`
    );
  }

  /**
   * List users in a tenant with pagination.
   * @param {string} tenantId - The tenant ID
   * @param {Object} [options={}] - List options
   * @param {number} [options.pageSize] - Maximum number of users to return
   * @param {string} [options.pageToken] - Token for the next page
   * @param {string} [options.filter] - Optional filter expression
   * @param {string} [options.roleId] - Filter users by role
   * @param {string} [options.groupId] - Filter users by group
   * @param {string} [options.status] - Filter users by status
   * @returns {Promise<{users: Array<User>} & PaginatedResponse>} List of users
   * @throws {ValidationError} If tenantId is not provided
   */
  async listUsers(tenantId, options = {}) {
    requiredString(tenantId, 'tenantId');

    const query = this.#buildPaginationQuery(options);
    if (options.roleId) query.role_id = options.roleId;
    if (options.groupId) query.group_id = options.groupId;
    if (options.status) query.status = options.status;

    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/users`,
      { query }
    );
  }

  // ==========================================================================
  // Group Management
  // ==========================================================================

  /**
   * Create a new group in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {string} name - Unique group name within the tenant
   * @param {Object} [options={}] - Additional options
   * @param {string} [options.displayName] - Human-readable display name
   * @param {string} [options.description] - Optional description
   * @param {Array<string>} [options.roles] - Initial role IDs to assign
   * @param {Object} [options.metadata] - Additional metadata
   * @returns {Promise<Group>} The created group
   * @throws {ValidationError} If required parameters are missing
   */
  async createGroup(tenantId, name, options = {}) {
    requiredString(tenantId, 'tenantId');
    requiredString(name, 'name');

    const body = {
      name,
      display_name: options.displayName,
      description: options.description,
      roles: options.roles,
      metadata: options.metadata,
    };

    return this.#transport.post(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/groups`,
      body
    );
  }

  /**
   * Get a group by ID.
   * @param {string} tenantId - The tenant ID
   * @param {string} groupId - The group ID
   * @returns {Promise<Group>} The group
   * @throws {ValidationError} If required parameters are missing
   */
  async getGroup(tenantId, groupId) {
    requiredString(tenantId, 'tenantId');
    requiredString(groupId, 'groupId');
    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/groups/${encodeURIComponent(groupId)}`
    );
  }

  /**
   * Update a group.
   * @param {string} tenantId - The tenant ID
   * @param {string} groupId - The group ID
   * @param {Object} updates - Fields to update
   * @param {string} [updates.name] - New name
   * @param {string} [updates.displayName] - New display name
   * @param {string} [updates.description] - New description
   * @param {Array<string>} [updates.roles] - New role IDs (replaces existing)
   * @param {Object} [updates.metadata] - Updated metadata
   * @returns {Promise<Group>} The updated group
   * @throws {ValidationError} If required parameters are missing
   */
  async updateGroup(tenantId, groupId, updates) {
    requiredString(tenantId, 'tenantId');
    requiredString(groupId, 'groupId');
    requiredObject(updates, 'updates');

    const body = {
      name: updates.name,
      display_name: updates.displayName,
      description: updates.description,
      roles: updates.roles,
      metadata: updates.metadata,
    };

    return this.#transport.put(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/groups/${encodeURIComponent(groupId)}`,
      body
    );
  }

  /**
   * Delete a group.
   * @param {string} tenantId - The tenant ID
   * @param {string} groupId - The group ID
   * @returns {Promise<void>}
   * @throws {ValidationError} If required parameters are missing
   */
  async deleteGroup(tenantId, groupId) {
    requiredString(tenantId, 'tenantId');
    requiredString(groupId, 'groupId');
    await this.#transport.delete(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/groups/${encodeURIComponent(groupId)}`
    );
  }

  /**
   * List groups in a tenant with pagination.
   * @param {string} tenantId - The tenant ID
   * @param {Object} [options={}] - List options
   * @param {number} [options.pageSize] - Maximum number of groups to return
   * @param {string} [options.pageToken] - Token for the next page
   * @param {string} [options.filter] - Optional filter expression
   * @param {string} [options.userId] - Filter groups containing this user
   * @returns {Promise<{groups: Array<Group>} & PaginatedResponse>} List of groups
   * @throws {ValidationError} If tenantId is not provided
   */
  async listGroups(tenantId, options = {}) {
    requiredString(tenantId, 'tenantId');

    const query = this.#buildPaginationQuery(options);
    if (options.userId) query.user_id = options.userId;

    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/groups`,
      { query }
    );
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
   * @param {string} [options.displayName] - Human-readable display name
   * @param {string} [options.description] - Optional description
   * @param {Object} [options.metadata] - Additional metadata
   * @returns {Promise<Role>} The created role
   * @throws {ValidationError} If required parameters are missing
   */
  async createRole(tenantId, name, permissions, options = {}) {
    requiredString(tenantId, 'tenantId');
    requiredString(name, 'name');
    requiredArray(permissions, 'permissions');

    if (permissions.length === 0) {
      throw new ValidationError('At least one permission is required', 'permissions');
    }

    const body = {
      name,
      permissions,
      display_name: options.displayName,
      description: options.description,
      metadata: options.metadata,
    };

    return this.#transport.post(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/roles`,
      body
    );
  }

  /**
   * Update a role.
   * @param {string} tenantId - The tenant ID
   * @param {string} roleId - The role ID
   * @param {Object} updates - Fields to update
   * @param {string} [updates.name] - New name
   * @param {string} [updates.displayName] - New display name
   * @param {string} [updates.description] - New description
   * @param {Array<string>} [updates.permissions] - New permissions (replaces existing)
   * @param {Object} [updates.metadata] - Updated metadata
   * @returns {Promise<Role>} The updated role
   * @throws {ValidationError} If required parameters are missing
   */
  async updateRole(tenantId, roleId, updates) {
    requiredString(tenantId, 'tenantId');
    requiredString(roleId, 'roleId');
    requiredObject(updates, 'updates');

    const body = {
      name: updates.name,
      display_name: updates.displayName,
      description: updates.description,
      permissions: updates.permissions,
      metadata: updates.metadata,
    };

    return this.#transport.put(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/roles/${encodeURIComponent(roleId)}`,
      body
    );
  }

  /**
   * List roles in a tenant with pagination.
   * @param {string} tenantId - The tenant ID
   * @param {Object} [options={}] - List options
   * @param {number} [options.pageSize] - Maximum number of roles to return
   * @param {string} [options.pageToken] - Token for the next page
   * @param {string} [options.filter] - Optional filter expression
   * @param {boolean} [options.includeSystem] - Include system-defined roles
   * @returns {Promise<{roles: Array<Role>} & PaginatedResponse>} List of roles
   * @throws {ValidationError} If tenantId is not provided
   */
  async listRoles(tenantId, options = {}) {
    requiredString(tenantId, 'tenantId');

    const query = this.#buildPaginationQuery(options);
    if (options.includeSystem !== undefined) {
      query.include_system = options.includeSystem;
    }

    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/roles`,
      { query }
    );
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
   * Set an access policy for a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {Object} policy - The policy definition
   * @param {string} policy.name - Unique policy name
   * @param {Array<Object>} policy.rules - Policy rules
   * @param {string} [policy.description] - Policy description
   * @param {boolean} [policy.enabled] - Whether the policy is active
   * @param {number} [policy.priority] - Evaluation priority
   * @param {string} [policy.effect] - Default effect (ALLOW or DENY)
   * @param {Object} [policy.labels] - Arbitrary metadata labels
   * @returns {Promise<Object>} The created/updated policy
   * @throws {ValidationError} If required parameters are missing
   */
  async setPolicy(tenantId, policy) {
    requiredString(tenantId, 'tenantId');
    requiredObject(policy, 'policy');
    requiredString(policy.name, 'policy.name');
    requiredArray(policy.rules, 'policy.rules');

    if (policy.rules.length === 0) {
      throw new ValidationError('At least one rule is required', 'policy.rules');
    }

    return this.#transport.post(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/policies`,
      policy
    );
  }

  /**
   * Get the access policy for a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {string} [policyName] - Optional policy name (gets all policies if not provided)
   * @returns {Promise<Object>} The policy or list of policies
   * @throws {ValidationError} If tenantId is not provided
   */
  async getPolicy(tenantId, policyName) {
    requiredString(tenantId, 'tenantId');

    if (policyName) {
      return this.#transport.get(
        `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/policies/${encodeURIComponent(policyName)}`
      );
    }

    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/policies`
    );
  }

  // ==========================================================================
  // Application Management
  // ==========================================================================

  /**
   * Create a new OAuth2 application.
   * @param {string} tenantId - The tenant ID
   * @param {string} name - Application display name
   * @param {Object} [options={}] - Additional options
   * @param {string} [options.description] - Application description
   * @param {string} [options.type] - Application type (web, native, spa, service)
   * @param {Array<string>} [options.redirectUris] - Allowed redirect URIs
   * @param {Array<string>} [options.grantTypes] - Allowed OAuth2 grant types
   * @param {Array<string>} [options.scopes] - Allowed scopes
   * @param {Object} [options.metadata] - Additional metadata
   * @returns {Promise<Application>} The created application with client secret
   * @throws {ValidationError} If required parameters are missing
   */
  async createApp(tenantId, name, options = {}) {
    requiredString(tenantId, 'tenantId');
    requiredString(name, 'name');

    const body = {
      name,
      description: options.description,
      type: options.type,
      redirect_uris: options.redirectUris,
      grant_types: options.grantTypes,
      scopes: options.scopes,
      metadata: options.metadata,
    };

    return this.#transport.post(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/apps`,
      body
    );
  }

  /**
   * Get an application by client ID.
   * @param {string} tenantId - The tenant ID
   * @param {string} appId - The application client ID
   * @returns {Promise<Application>} The application
   * @throws {ValidationError} If required parameters are missing
   */
  async getApp(tenantId, appId) {
    requiredString(tenantId, 'tenantId');
    requiredString(appId, 'appId');
    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/apps/${encodeURIComponent(appId)}`
    );
  }

  /**
   * Delete an application.
   * @param {string} tenantId - The tenant ID
   * @param {string} appId - The application client ID
   * @returns {Promise<void>}
   * @throws {ValidationError} If required parameters are missing
   */
  async deleteApp(tenantId, appId) {
    requiredString(tenantId, 'tenantId');
    requiredString(appId, 'appId');
    await this.#transport.delete(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/apps/${encodeURIComponent(appId)}`
    );
  }

  /**
   * List applications in a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {Object} [options={}] - List options
   * @param {number} [options.pageSize] - Maximum number of apps to return
   * @param {string} [options.pageToken] - Token for the next page
   * @returns {Promise<{applications: Array<Application>, totalCount: number}>} List of applications
   * @throws {ValidationError} If tenantId is not provided
   */
  async listApps(tenantId, options = {}) {
    requiredString(tenantId, 'tenantId');
    const query = this.#buildPaginationQuery(options);
    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/apps`,
      { query }
    );
  }

  // ==========================================================================
  // Session Management
  // ==========================================================================

  /**
   * Create a new session for a user.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {Object} [options={}] - Session options
   * @param {string} [options.ipAddress] - Client IP address
   * @param {string} [options.userAgent] - Client user agent
   * @param {string} [options.authMethod] - Authentication method used
   * @returns {Promise<Session>} The created session
   * @throws {ValidationError} If required parameters are missing
   */
  async createSession(tenantId, userId, options = {}) {
    requiredString(tenantId, 'tenantId');
    requiredString(userId, 'userId');

    const body = {
      user_id: userId,
      ip_address: options.ipAddress,
      user_agent: options.userAgent,
      auth_method: options.authMethod,
    };

    return this.#transport.post(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/sessions`,
      body
    );
  }

  /**
   * List sessions for a tenant or user.
   * @param {string} tenantId - The tenant ID
   * @param {Object} [options={}] - List options
   * @param {string} [options.userId] - Filter by user ID
   * @param {boolean} [options.includeExpired] - Include expired sessions
   * @param {number} [options.limit] - Maximum number of sessions to return
   * @param {number} [options.offset] - Pagination offset
   * @returns {Promise<{sessions: Array<Session>, totalCount: number, hasMore: boolean}>} List of sessions
   * @throws {ValidationError} If tenantId is not provided
   */
  async listSessions(tenantId, options = {}) {
    requiredString(tenantId, 'tenantId');

    const query = {};
    if (options.userId) query.user_id = options.userId;
    if (options.includeExpired !== undefined) query.include_expired = options.includeExpired;
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
      query.limit = options.limit;
    }
    if (options.offset !== undefined) query.offset = options.offset;

    return this.#transport.get(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/sessions`,
      { query }
    );
  }

  // ==========================================================================
  // Audit Log
  // ==========================================================================

  /**
   * Get audit log entries for a tenant.
   * @param {string} tenantId - The tenant ID
   * @param {Object} [options={}] - Query options
   * @param {string} [options.actorId] - Filter by actor ID
   * @param {string} [options.targetId] - Filter by target ID
   * @param {string} [options.eventType] - Filter by event type
   * @param {string} [options.action] - Filter by action
   * @param {string} [options.outcome] - Filter by outcome
   * @param {string} [options.startTime] - Start of time range (ISO 8601)
   * @param {string} [options.endTime] - End of time range (ISO 8601)
   * @param {string} [options.ipAddress] - Filter by IP address
   * @param {string} [options.sessionId] - Filter by session ID
   * @param {number} [options.limit] - Maximum number of events to return
   * @param {number} [options.offset] - Pagination offset
   * @param {string} [options.orderBy] - Sort field
   * @param {boolean} [options.orderDesc] - Sort descending
   * @returns {Promise<{events: Array<AuditEvent>, totalCount: number, hasMore: boolean}>} List of audit events
   * @throws {ValidationError} If tenantId is not provided
   */
  async getAuditLog(tenantId, options = {}) {
    requiredString(tenantId, 'tenantId');

    const query = {
      tenant_id: tenantId,
    };

    if (options.actorId) query.actor_id = options.actorId;
    if (options.targetId) query.target_id = options.targetId;
    if (options.eventType) query.event_type = options.eventType;
    if (options.action) query.action = options.action;
    if (options.outcome) query.outcome = options.outcome;
    if (options.startTime) query.start_time = options.startTime;
    if (options.endTime) query.end_time = options.endTime;
    if (options.ipAddress) query.ip_address = options.ipAddress;
    if (options.sessionId) query.session_id = options.sessionId;
    if (options.limit !== undefined) {
      positiveInteger(options.limit, 'options.limit');
      query.limit = options.limit;
    }
    if (options.offset !== undefined) query.offset = options.offset;
    if (options.orderBy) query.order_by = options.orderBy;
    if (options.orderDesc !== undefined) query.order_desc = options.orderDesc;

    return this.#transport.get(`${IAMClient.API_PREFIX}/audit`, { query });
  }

  // ==========================================================================
  // GDPR Operations
  // ==========================================================================

  /**
   * Export user data for GDPR compliance.
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {Object} [options={}] - Export options
   * @param {string} [options.format] - Export format (json, csv, xml)
   * @param {Array<string>} [options.includeCategories] - Data categories to include
   * @param {Array<string>} [options.excludeCategories] - Data categories to exclude
   * @param {string} [options.notifyEmail] - Email to notify when export is ready
   * @returns {Promise<{requestId: string, status: string, format: string}>} Export request info
   * @throws {ValidationError} If required parameters are missing
   */
  async exportUserData(tenantId, userId, options = {}) {
    requiredString(tenantId, 'tenantId');
    requiredString(userId, 'userId');

    const body = {
      format: options.format || 'json',
      include_categories: options.includeCategories,
      exclude_categories: options.excludeCategories,
      notify_email: options.notifyEmail,
    };

    return this.#transport.post(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/users/${encodeURIComponent(userId)}/export`,
      body
    );
  }

  /**
   * Delete all user data for GDPR compliance (right to be forgotten).
   * @param {string} tenantId - The tenant ID
   * @param {string} userId - The user ID
   * @param {Object} [options={}] - Deletion options
   * @param {string} [options.reason] - Reason for deletion
   * @param {string} [options.confirmEmail] - Email confirmation for verification
   * @param {Array<string>} [options.retainCategories] - Data categories to retain (legal requirements)
   * @param {string} [options.scheduledFor] - Schedule deletion for a future date (ISO 8601)
   * @param {string} [options.notifyEmail] - Email to notify when deletion is complete
   * @returns {Promise<{requestId: string, status: string, scheduledFor: string}>} Deletion request info
   * @throws {ValidationError} If required parameters are missing
   */
  async deleteUserData(tenantId, userId, options = {}) {
    requiredString(tenantId, 'tenantId');
    requiredString(userId, 'userId');

    const body = {
      reason: options.reason,
      confirm_email: options.confirmEmail,
      retain_categories: options.retainCategories,
      scheduled_for: options.scheduledFor,
      notify_email: options.notifyEmail,
    };

    return this.#transport.post(
      `${IAMClient.API_PREFIX}/tenants/${encodeURIComponent(tenantId)}/users/${encodeURIComponent(userId)}/delete-data`,
      body
    );
  }

  // ==========================================================================
  // Private Helper Methods
  // ==========================================================================

  /**
   * Build pagination query parameters.
   * @param {PaginationOptions} options - Pagination options
   * @returns {Object} Query parameters object
   * @private
   */
  #buildPaginationQuery(options) {
    const query = {};

    if (options.pageSize !== undefined) {
      positiveInteger(options.pageSize, 'options.pageSize');
      query.page_size = options.pageSize;
    }
    if (options.pageToken) {
      query.page_token = options.pageToken;
    }
    if (options.filter) {
      query.filter = options.filter;
    }

    return query;
  }
}

export default IAMClient;
