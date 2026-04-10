/**
 * QuicBaaS JavaScript SDK - RBAC Client
 *
 * Provides Role-Based Access Control operations including user management,
 * role management, and role assignments.
 *
 * @module @quicbaas/client/clients/rbac
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  requiredString,
  required,
  nonNegativeInteger,
} from '../utils/validation.js';

/**
 * API base path for RBAC endpoints.
 * @constant {string}
 */
const RBAC_API_PATH = '/api/v1/rbac';

/**
 * @typedef {Object} UserInfo
 * @property {string} userId - Unique user identifier
 * @property {string} username - Username
 * @property {string[]} roles - Assigned roles
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 * @property {number} lastLoginAt - Last login timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} RoleInfo
 * @property {string} roleId - Unique role identifier
 * @property {string} roleName - Role name
 * @property {string[]} permissions - Permissions granted by this role
 * @property {number} createdAt - Creation timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} CreateUserOptions
 * @property {string[]} [roles] - Initial roles to assign to the user
 */

/**
 * @typedef {Object} CreateUserResponse
 * @property {string} userId - Unique identifier for the created user
 * @property {number} timestamp - Creation timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} DeleteUserResponse
 * @property {boolean} success - Whether the deletion was successful
 * @property {string} message - Additional information about the operation
 */

/**
 * @typedef {Object} ListUsersOptions
 * @property {number} [limit] - Maximum number of users to return
 * @property {number} [offset] - Number of users to skip
 */

/**
 * @typedef {Object} ListUsersResponse
 * @property {UserInfo[]} users - List of users
 * @property {number} totalCount - Total number of users available
 */

/**
 * @typedef {Object} CreateRoleOptions
 * @property {string} [description] - Optional description for the role
 */

/**
 * @typedef {Object} CreateRoleResponse
 * @property {string} roleId - Unique identifier for the created role
 * @property {number} timestamp - Creation timestamp (Unix nanoseconds)
 */

/**
 * @typedef {Object} DeleteRoleResponse
 * @property {boolean} success - Whether the deletion was successful
 * @property {string} message - Additional information about the operation
 */

/**
 * @typedef {Object} ListRolesOptions
 * @property {number} [limit] - Maximum number of roles to return
 * @property {number} [offset] - Number of roles to skip
 */

/**
 * @typedef {Object} ListRolesResponse
 * @property {RoleInfo[]} roles - List of roles
 * @property {number} totalCount - Total number of roles available
 */

/**
 * @typedef {Object} AssignRoleResponse
 * @property {boolean} success - Whether the assignment was successful
 * @property {string} message - Additional information about the operation
 */

/**
 * @typedef {Object} RevokeRoleResponse
 * @property {boolean} success - Whether the revocation was successful
 * @property {string} message - Additional information about the operation
 */

/**
 * RBAC (Role-Based Access Control) client for QuicBaaS.
 *
 * Provides methods for managing users, roles, and role assignments.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import RBACClient from '@quicbaas/client/clients/rbac';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const rbac = new RBACClient(transport);
 *
 * // Create a new user
 * const { userId } = await rbac.createUser('john.doe', 'securePassword123', {
 *   roles: ['viewer']
 * });
 *
 * // Create a role
 * const { roleId } = await rbac.createRole('editor', ['read', 'write']);
 *
 * // Assign role to user
 * await rbac.assignRole('john.doe', 'editor');
 *
 * // List users with pagination
 * const { users, totalCount } = await rbac.listUsers({ limit: 10, offset: 0 });
 */
export class RBACClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new RBACClient instance.
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
   * Create a new user with optional initial roles.
   *
   * @param {string} username - Unique username for the new user
   * @param {string} password - User password
   * @param {CreateUserOptions} [options={}] - Additional options
   * @returns {Promise<CreateUserResponse>} Response with userId and timestamp
   * @throws {ValidationError} If username or password is empty
   * @throws {AlreadyExistsError} If username already exists
   *
   * @example
   * // Create user without roles
   * const { userId } = await rbac.createUser('alice', 'password123');
   *
   * // Create user with initial roles
   * const { userId } = await rbac.createUser('bob', 'password456', {
   *   roles: ['admin', 'editor']
   * });
   */
  async createUser(username, password, options = {}) {
    requiredString(username, 'username');
    requiredString(password, 'password');

    this.#validateCreateUserOptions(options);

    const requestBody = {
      username,
      password,
    };

    if (options.roles !== undefined) {
      requestBody.roles = options.roles;
    }

    const response = await this.#transport.post(
      `${RBAC_API_PATH}/users`,
      requestBody
    );

    return this.#transformCreateUserResponse(response);
  }

  /**
   * Delete a user from the system.
   *
   * @param {string} username - Username of the user to delete
   * @returns {Promise<DeleteUserResponse>} Response indicating success
   * @throws {ValidationError} If username is empty
   * @throws {NotFoundError} If user does not exist
   *
   * @example
   * const { success, message } = await rbac.deleteUser('john.doe');
   * if (success) {
   *   console.log('User deleted successfully');
   * }
   */
  async deleteUser(username) {
    requiredString(username, 'username');

    const response = await this.#transport.delete(
      `${RBAC_API_PATH}/users/${encodeURIComponent(username)}`
    );

    return this.#transformDeleteUserResponse(response);
  }

  /**
   * List all users with optional pagination.
   *
   * @param {ListUsersOptions} [options={}] - Pagination options
   * @returns {Promise<ListUsersResponse>} Response with users and total count
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * // List first 10 users
   * const { users, totalCount } = await rbac.listUsers({ limit: 10 });
   *
   * // List next page
   * const { users } = await rbac.listUsers({ limit: 10, offset: 10 });
   */
  async listUsers(options = {}) {
    this.#validateListOptions(options, 'listUsers');

    const query = {};
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      query.offset = options.offset;
    }

    const response = await this.#transport.get(
      `${RBAC_API_PATH}/users`,
      { query }
    );

    return this.#transformListUsersResponse(response);
  }

  /**
   * Create a new role with specified permissions.
   *
   * @param {string} name - Unique name for the new role
   * @param {string[]} permissions - List of permissions granted by this role
   * @param {CreateRoleOptions} [options={}] - Additional options
   * @returns {Promise<CreateRoleResponse>} Response with roleId and timestamp
   * @throws {ValidationError} If name is empty or permissions is not an array
   * @throws {AlreadyExistsError} If role name already exists
   *
   * @example
   * const { roleId } = await rbac.createRole('editor', ['read', 'write', 'delete']);
   */
  async createRole(name, permissions, options = {}) {
    requiredString(name, 'name');
    this.#validatePermissions(permissions);
    this.#validateCreateRoleOptions(options);

    const requestBody = {
      role_name: name,
      permissions,
    };

    if (options.description !== undefined) {
      requestBody.description = options.description;
    }

    const response = await this.#transport.post(
      `${RBAC_API_PATH}/roles`,
      requestBody
    );

    return this.#transformCreateRoleResponse(response);
  }

  /**
   * Delete a role from the system.
   *
   * @param {string} name - Name of the role to delete
   * @returns {Promise<DeleteRoleResponse>} Response indicating success
   * @throws {ValidationError} If name is empty
   * @throws {NotFoundError} If role does not exist
   *
   * @example
   * const { success, message } = await rbac.deleteRole('editor');
   * if (success) {
   *   console.log('Role deleted successfully');
   * }
   */
  async deleteRole(name) {
    requiredString(name, 'name');

    const response = await this.#transport.delete(
      `${RBAC_API_PATH}/roles/${encodeURIComponent(name)}`
    );

    return this.#transformDeleteRoleResponse(response);
  }

  /**
   * List all roles with optional pagination.
   *
   * @param {ListRolesOptions} [options={}] - Pagination options
   * @returns {Promise<ListRolesResponse>} Response with roles and total count
   * @throws {ValidationError} If options are invalid
   *
   * @example
   * // List first 10 roles
   * const { roles, totalCount } = await rbac.listRoles({ limit: 10 });
   *
   * // List all roles (no pagination)
   * const { roles } = await rbac.listRoles();
   */
  async listRoles(options = {}) {
    this.#validateListOptions(options, 'listRoles');

    const query = {};
    if (options.limit !== undefined) {
      query.limit = options.limit;
    }
    if (options.offset !== undefined) {
      query.offset = options.offset;
    }

    const response = await this.#transport.get(
      `${RBAC_API_PATH}/roles`,
      { query }
    );

    return this.#transformListRolesResponse(response);
  }

  /**
   * Assign a role to a user.
   *
   * @param {string} username - Username of the user
   * @param {string} roleName - Name of the role to assign
   * @returns {Promise<AssignRoleResponse>} Response indicating success
   * @throws {ValidationError} If username or roleName is empty
   * @throws {NotFoundError} If user or role does not exist
   *
   * @example
   * const { success } = await rbac.assignRole('john.doe', 'editor');
   * if (success) {
   *   console.log('Role assigned successfully');
   * }
   */
  async assignRole(username, roleName) {
    requiredString(username, 'username');
    requiredString(roleName, 'roleName');

    const requestBody = {
      username,
      role_name: roleName,
    };

    const response = await this.#transport.post(
      `${RBAC_API_PATH}/assignments`,
      requestBody
    );

    return this.#transformAssignRoleResponse(response);
  }

  /**
   * Revoke a role from a user.
   *
   * @param {string} username - Username of the user
   * @param {string} roleName - Name of the role to revoke
   * @returns {Promise<RevokeRoleResponse>} Response indicating success
   * @throws {ValidationError} If username or roleName is empty
   * @throws {NotFoundError} If user or role does not exist
   *
   * @example
   * const { success } = await rbac.revokeRole('john.doe', 'editor');
   * if (success) {
   *   console.log('Role revoked successfully');
   * }
   */
  async revokeRole(username, roleName) {
    requiredString(username, 'username');
    requiredString(roleName, 'roleName');

    const response = await this.#transport.delete(
      `${RBAC_API_PATH}/assignments/${encodeURIComponent(username)}/${encodeURIComponent(roleName)}`
    );

    return this.#transformRevokeRoleResponse(response);
  }

  /**
   * Validate create user options.
   *
   * @param {CreateUserOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateCreateUserOptions(options) {
    if (options.roles !== undefined) {
      if (!Array.isArray(options.roles)) {
        throw new ValidationError('options.roles must be an array', 'options.roles');
      }
      for (let i = 0; i < options.roles.length; i++) {
        if (typeof options.roles[i] !== 'string' || options.roles[i].trim() === '') {
          throw new ValidationError(
            `options.roles[${i}] must be a non-empty string`,
            `options.roles[${i}]`
          );
        }
      }
    }
  }

  /**
   * Validate permissions array.
   *
   * @param {string[]} permissions - Permissions to validate
   * @throws {ValidationError} If permissions are invalid
   * @private
   */
  #validatePermissions(permissions) {
    required(permissions, 'permissions');
    if (!Array.isArray(permissions)) {
      throw new ValidationError('permissions must be an array', 'permissions');
    }
    for (let i = 0; i < permissions.length; i++) {
      if (typeof permissions[i] !== 'string' || permissions[i].trim() === '') {
        throw new ValidationError(
          `permissions[${i}] must be a non-empty string`,
          `permissions[${i}]`
        );
      }
    }
  }

  /**
   * Validate create role options.
   *
   * @param {CreateRoleOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateCreateRoleOptions(options) {
    if (options.description !== undefined && typeof options.description !== 'string') {
      throw new ValidationError('options.description must be a string', 'options.description');
    }
  }

  /**
   * Validate list options (pagination).
   *
   * @param {ListUsersOptions|ListRolesOptions} options - Options to validate
   * @param {string} operation - Operation name for error messages
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateListOptions(options, operation) {
    if (options.limit !== undefined) {
      nonNegativeInteger(options.limit, 'options.limit');
    }
    if (options.offset !== undefined) {
      nonNegativeInteger(options.offset, 'options.offset');
    }
  }

  /**
   * Transform server create user response.
   *
   * @param {Object} response - Raw server response
   * @returns {CreateUserResponse}
   * @private
   */
  #transformCreateUserResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid response from server', 'response');
    }

    return {
      userId: response.user_id || response.userId || '',
      timestamp: response.timestamp || 0,
    };
  }

  /**
   * Transform server delete user response.
   *
   * @param {Object} response - Raw server response
   * @returns {DeleteUserResponse}
   * @private
   */
  #transformDeleteUserResponse(response) {
    return {
      success: response?.success ?? true,
      message: response?.message || '',
    };
  }

  /**
   * Transform server list users response.
   *
   * @param {Object} response - Raw server response
   * @returns {ListUsersResponse}
   * @private
   */
  #transformListUsersResponse(response) {
    const users = (response?.users || []).map(user => this.#transformUserInfo(user));

    return {
      users,
      totalCount: response?.total_count || response?.totalCount || users.length,
    };
  }

  /**
   * Transform server user info.
   *
   * @param {Object} user - Raw user object from server
   * @returns {UserInfo}
   * @private
   */
  #transformUserInfo(user) {
    return {
      userId: user.user_id || user.userId || '',
      username: user.username || '',
      roles: user.roles || [],
      createdAt: user.created_at || user.createdAt || 0,
      lastLoginAt: user.last_login_at || user.lastLoginAt || 0,
    };
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
      roleId: response.role_id || response.roleId || '',
      timestamp: response.timestamp || 0,
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
      success: response?.success ?? true,
      message: response?.message || '',
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
    const roles = (response?.roles || []).map(role => this.#transformRoleInfo(role));

    return {
      roles,
      totalCount: response?.total_count || response?.totalCount || roles.length,
    };
  }

  /**
   * Transform server role info.
   *
   * @param {Object} role - Raw role object from server
   * @returns {RoleInfo}
   * @private
   */
  #transformRoleInfo(role) {
    return {
      roleId: role.role_id || role.roleId || '',
      roleName: role.role_name || role.roleName || '',
      permissions: role.permissions || [],
      createdAt: role.created_at || role.createdAt || 0,
    };
  }

  /**
   * Transform server assign role response.
   *
   * @param {Object} response - Raw server response
   * @returns {AssignRoleResponse}
   * @private
   */
  #transformAssignRoleResponse(response) {
    return {
      success: response?.success ?? true,
      message: response?.message || '',
    };
  }

  /**
   * Transform server revoke role response.
   *
   * @param {Object} response - Raw server response
   * @returns {RevokeRoleResponse}
   * @private
   */
  #transformRevokeRoleResponse(response) {
    return {
      success: response?.success ?? true,
      message: response?.message || '',
    };
  }
}

export default RBACClient;
