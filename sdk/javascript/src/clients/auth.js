/**
 * QuicBaaS JavaScript SDK - Authentication Client
 *
 * Provides authentication and authorization operations including
 * login, logout, token management, and user information retrieval.
 *
 * @module @quicbaas/client/clients/auth
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import { requiredString, required } from '../utils/validation.js';

/**
 * API base path for authentication endpoints.
 * @constant {string}
 */
const AUTH_API_PATH = '/api/v1/auth';

/**
 * MFA type enumeration.
 * @readonly
 * @enum {string}
 */
export const MFAType = {
  TOTP: 'TOTP',
  WEBAUTHN: 'WEBAUTHN',
  SMS: 'SMS',
  EMAIL: 'EMAIL',
  BACKUP_CODE: 'BACKUP_CODE',
};

/**
 * @typedef {Object} User
 * @property {string} userId - Unique user identifier
 * @property {string} username - Username
 * @property {string} [email] - User email address
 * @property {string[]} roles - Assigned roles
 * @property {string[]} permissions - Granted permissions
 * @property {string[]} [groups] - Group memberships
 * @property {string} [tenantId] - Tenant identifier
 */

/**
 * @typedef {Object} MFAChallenge
 * @property {string} challengeId - Unique challenge identifier
 * @property {string} type - MFA type (TOTP, WEBAUTHN, SMS, EMAIL, BACKUP_CODE)
 * @property {string} expiresAt - ISO timestamp when challenge expires
 */

/**
 * @typedef {Object} LoginResponse
 * @property {string} accessToken - JWT access token for API authentication
 * @property {string} refreshToken - Token for obtaining new access tokens
 * @property {number} expiresIn - Token expiration time in seconds
 * @property {string} tokenType - Token type (typically "Bearer")
 * @property {User} user - Authenticated user information
 * @property {boolean} [mfaRequired] - Whether MFA verification is needed
 * @property {MFAChallenge} [mfaChallenge] - MFA challenge details if required
 */

/**
 * @typedef {Object} RefreshTokenResponse
 * @property {string} accessToken - New JWT access token
 * @property {number} expiresIn - Token expiration time in seconds
 * @property {string} tokenType - Token type (typically "Bearer")
 * @property {string} [refreshToken] - New refresh token (if rotation enabled)
 */

/**
 * @typedef {Object} TokenClaims
 * @property {boolean} valid - Whether the token is valid
 * @property {string} userId - User identifier
 * @property {string} username - Username
 * @property {string} [email] - User email
 * @property {string} [tenantId] - Tenant identifier
 * @property {string[]} roles - User roles
 * @property {string[]} [groups] - User groups
 * @property {string[]} permissions - User permissions
 * @property {string} issuedAt - ISO timestamp when token was issued
 * @property {string} expiresAt - ISO timestamp when token expires
 * @property {string} issuer - Token issuer
 * @property {string[]} audience - Intended audience
 * @property {string} subject - Token subject
 * @property {string} [sessionId] - Associated session ID
 * @property {Object} [customClaims] - Additional custom claims
 */

/**
 * @typedef {Object} LoginOptions
 * @property {string} [mfaCode] - Multi-factor authentication code
 * @property {string} [tenantId] - Tenant ID for multi-tenant deployments
 */

/**
 * Authentication client for QuicBaaS.
 *
 * Provides methods for user authentication, token management,
 * and session handling. Automatically updates the transport's
 * auth token on successful login or token refresh.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import { AuthClient } from '@quicbaas/client/clients/auth';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * const auth = new AuthClient(transport);
 *
 * // Login and automatically set auth token
 * const { user, accessToken } = await auth.login('username', 'password');
 * console.log(`Logged in as ${user.username}`);
 *
 * // Later: refresh the token
 * const refreshed = await auth.refreshToken(refreshToken);
 *
 * // Logout
 * await auth.logout();
 */
export class AuthClient {
  /**
   * Current access token.
   * @type {string|null}
   * @private
   */
  #accessToken = null;

  /**
   * Current refresh token.
   * @type {string|null}
   * @private
   */
  #refreshToken = null;

  /**
   * Cached current user information.
   * @type {User|null}
   * @private
   */
  #currentUser = null;

  /**
   * Create a new AuthClient instance.
   *
   * @param {import('../transport/http.js').HttpTransport} transport - HTTP transport instance
   * @throws {ValidationError} If transport is not provided
   */
  constructor(transport) {
    if (!transport) {
      throw new ValidationError('transport is required', 'transport');
    }
    if (typeof transport.request !== 'function') {
      throw new ValidationError('transport must have a request method', 'transport');
    }

    /**
     * HTTP transport instance.
     * @type {import('../transport/http.js').HttpTransport}
     * @private
     */
    this._transport = transport;
  }

  /**
   * Authenticate a user with username and password.
   *
   * On successful authentication, automatically updates the transport's
   * auth token for subsequent requests.
   *
   * @param {string} username - Username or email
   * @param {string} password - User password
   * @param {LoginOptions} [options] - Additional login options
   * @returns {Promise<LoginResponse>} Authentication result with tokens and user info
   * @throws {ValidationError} If username or password is empty
   * @throws {AuthenticationError} If credentials are invalid
   *
   * @example
   * // Basic login
   * const result = await auth.login('admin', 'secretpassword');
   *
   * // Login with MFA code
   * const result = await auth.login('admin', 'secretpassword', {
   *   mfaCode: '123456'
   * });
   *
   * // Multi-tenant login
   * const result = await auth.login('admin', 'secretpassword', {
   *   tenantId: 'tenant-123'
   * });
   */
  async login(username, password, options = {}) {
    requiredString(username, 'username');
    requiredString(password, 'password');

    const requestBody = {
      username,
      password,
    };

    if (options.mfaCode) {
      if (typeof options.mfaCode !== 'string') {
        throw new ValidationError('mfaCode must be a string', 'mfaCode');
      }
      requestBody.mfa_code = options.mfaCode;
    }

    if (options.tenantId) {
      if (typeof options.tenantId !== 'string') {
        throw new ValidationError('tenantId must be a string', 'tenantId');
      }
      requestBody.tenant_id = options.tenantId;
    }

    const response = await this._transport.post(
      `${AUTH_API_PATH}/login`,
      requestBody
    );

    const result = this._transformLoginResponse(response);

    // Update transport auth token on successful login (if no MFA required)
    if (!result.mfaRequired && result.accessToken) {
      this.#accessToken = result.accessToken;
      this.#refreshToken = result.refreshToken;
      this.#currentUser = result.user;
      this._transport.setAuthToken(result.accessToken, result.refreshToken);
    }

    return result;
  }

  /**
   * Invalidate the current session and clear tokens.
   *
   * Sends a logout request to the server and clears all local
   * authentication state including the transport's auth token.
   *
   * @returns {Promise<void>}
   * @throws {AuthenticationError} If not currently authenticated
   *
   * @example
   * await auth.logout();
   * // Transport auth token is now cleared
   */
  async logout() {
    const token = this.#accessToken;

    if (token) {
      await this._transport.post(`${AUTH_API_PATH}/logout`, {
        access_token: token,
      });
    }

    // Clear local state regardless of server response
    this.#accessToken = null;
    this.#refreshToken = null;
    this.#currentUser = null;
    this._transport.clearAuthToken();
  }

  /**
   * Exchange a refresh token for a new access token.
   *
   * On successful refresh, automatically updates the transport's
   * auth token for subsequent requests.
   *
   * @param {string} refreshToken - The refresh token to exchange
   * @returns {Promise<RefreshTokenResponse>} New token information
   * @throws {ValidationError} If refreshToken is empty
   * @throws {AuthenticationError} If refresh token is invalid or expired
   *
   * @example
   * const { accessToken, expiresIn } = await auth.refreshToken(storedRefreshToken);
   * console.log(`New token expires in ${expiresIn} seconds`);
   */
  async refreshToken(refreshToken) {
    requiredString(refreshToken, 'refreshToken');

    const response = await this._transport.post(
      `${AUTH_API_PATH}/refresh`,
      { refresh_token: refreshToken }
    );

    const result = this._transformRefreshResponse(response);

    // Update transport auth token on successful refresh
    if (result.accessToken) {
      this.#accessToken = result.accessToken;
      if (result.refreshToken) {
        this.#refreshToken = result.refreshToken;
      }
      this._transport.setAuthToken(
        result.accessToken,
        result.refreshToken || this.#refreshToken
      );
    }

    return result;
  }

  /**
   * Validate an access token and retrieve its claims.
   *
   * Use this method to check if a token is valid and to extract
   * user information and permissions from the token.
   *
   * @param {string} token - The access token to validate
   * @returns {Promise<TokenClaims>} Token claims if valid
   * @throws {ValidationError} If token is empty
   * @throws {AuthenticationError} If token is invalid
   *
   * @example
   * const claims = await auth.validateToken(accessToken);
   * if (claims.valid) {
   *   console.log(`Token belongs to user: ${claims.username}`);
   *   console.log(`Roles: ${claims.roles.join(', ')}`);
   * }
   */
  async validateToken(token) {
    requiredString(token, 'token');

    const response = await this._transport.post(
      `${AUTH_API_PATH}/validate`,
      { access_token: token }
    );

    return this._transformTokenClaims(response);
  }

  /**
   * Get the currently authenticated user's information.
   *
   * Returns cached user information from the last successful login,
   * or fetches fresh data from the server if not cached.
   *
   * @returns {Promise<User>} Current user information
   * @throws {AuthenticationError} If not authenticated
   *
   * @example
   * const user = await auth.getCurrentUser();
   * console.log(`Current user: ${user.username}`);
   * console.log(`Roles: ${user.roles.join(', ')}`);
   */
  async getCurrentUser() {
    // Return cached user if available
    if (this.#currentUser) {
      return this.#currentUser;
    }

    // Fetch from server using current token
    const response = await this._transport.get(`${AUTH_API_PATH}/me`);

    const user = this._transformUser(response);
    this.#currentUser = user;

    return user;
  }

  /**
   * Check if the client is currently authenticated.
   *
   * Note: This only checks for the presence of a local token,
   * not whether the token is still valid on the server.
   *
   * @returns {boolean} True if an access token is present
   *
   * @example
   * if (auth.isAuthenticated()) {
   *   const user = await auth.getCurrentUser();
   * }
   */
  isAuthenticated() {
    return this.#accessToken !== null;
  }

  /**
   * Get the current access token.
   *
   * @returns {string|null} Current access token or null if not authenticated
   */
  getAccessToken() {
    return this.#accessToken;
  }

  /**
   * Get the current refresh token.
   *
   * @returns {string|null} Current refresh token or null if not available
   */
  getRefreshToken() {
    return this.#refreshToken;
  }

  /**
   * Set tokens manually (e.g., from stored session).
   *
   * Use this method to restore authentication state from
   * persisted tokens (localStorage, cookies, etc.).
   *
   * @param {string} accessToken - Access token to set
   * @param {string} [refreshToken] - Optional refresh token
   * @throws {ValidationError} If accessToken is empty
   *
   * @example
   * // Restore session from localStorage
   * const storedToken = localStorage.getItem('accessToken');
   * const storedRefresh = localStorage.getItem('refreshToken');
   * auth.setTokens(storedToken, storedRefresh);
   */
  setTokens(accessToken, refreshToken = null) {
    requiredString(accessToken, 'accessToken');

    if (refreshToken !== null && typeof refreshToken !== 'string') {
      throw new ValidationError('refreshToken must be a string or null', 'refreshToken');
    }

    this.#accessToken = accessToken;
    this.#refreshToken = refreshToken;
    this.#currentUser = null; // Clear cached user
    this._transport.setAuthToken(accessToken, refreshToken);
  }

  /**
   * Transform server login response to LoginResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {LoginResponse}
   * @private
   */
  _transformLoginResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid login response from server', 'response');
    }

    const result = {
      accessToken: response.access_token || response.accessToken,
      refreshToken: response.refresh_token || response.refreshToken,
      expiresIn: this._parseExpiresIn(response),
      tokenType: response.token_type || response.tokenType || 'Bearer',
      user: this._extractUserFromResponse(response),
    };

    // Handle MFA requirement
    if (response.mfa_required || response.mfaRequired) {
      result.mfaRequired = true;
      if (response.mfa_challenge || response.mfaChallenge) {
        const challenge = response.mfa_challenge || response.mfaChallenge;
        result.mfaChallenge = {
          challengeId: challenge.challenge_id || challenge.challengeId,
          type: challenge.type,
          expiresAt: challenge.expires_at || challenge.expiresAt,
        };
      }
    }

    return result;
  }

  /**
   * Transform server refresh response to RefreshTokenResponse format.
   *
   * @param {Object} response - Raw server response
   * @returns {RefreshTokenResponse}
   * @private
   */
  _transformRefreshResponse(response) {
    if (!response) {
      throw new ValidationError('Invalid refresh response from server', 'response');
    }

    return {
      accessToken: response.access_token || response.accessToken,
      expiresIn: this._parseExpiresIn(response),
      tokenType: response.token_type || response.tokenType || 'Bearer',
      refreshToken: response.refresh_token || response.refreshToken || null,
    };
  }

  /**
   * Transform server token claims response to TokenClaims format.
   *
   * @param {Object} response - Raw server response
   * @returns {TokenClaims}
   * @private
   */
  _transformTokenClaims(response) {
    if (!response) {
      throw new ValidationError('Invalid token claims response from server', 'response');
    }

    return {
      valid: response.valid !== false,
      userId: response.user_id || response.userId,
      username: response.username,
      email: response.email || null,
      tenantId: response.tenant_id || response.tenantId || null,
      roles: response.roles || [],
      groups: response.groups || [],
      permissions: response.permissions || [],
      issuedAt: response.issued_at || response.issuedAt,
      expiresAt: response.expires_at || response.expiresAt,
      issuer: response.issuer || response.iss,
      audience: this._normalizeAudience(response.audience || response.aud),
      subject: response.subject || response.sub,
      sessionId: response.session_id || response.sessionId || null,
      customClaims: response.custom_claims || response.customClaims || null,
    };
  }

  /**
   * Transform server user response to User format.
   *
   * @param {Object} response - Raw server response
   * @returns {User}
   * @private
   */
  _transformUser(response) {
    if (!response) {
      throw new ValidationError('Invalid user response from server', 'response');
    }

    return {
      userId: response.user_id || response.userId || response.id,
      username: response.username,
      email: response.email || null,
      roles: response.roles || [],
      permissions: response.permissions || [],
      groups: response.groups || [],
      tenantId: response.tenant_id || response.tenantId || null,
    };
  }

  /**
   * Extract user information from login response.
   *
   * @param {Object} response - Raw server response
   * @returns {User}
   * @private
   */
  _extractUserFromResponse(response) {
    // User info might be nested under 'user' key or at top level
    const userData = response.user || response;

    return {
      userId: userData.user_id || userData.userId || userData.id,
      username: userData.username,
      email: userData.email || null,
      roles: userData.roles || [],
      permissions: userData.permissions || [],
      groups: userData.groups || [],
      tenantId: userData.tenant_id || userData.tenantId || null,
    };
  }

  /**
   * Parse expires_in or calculate from expires_at.
   *
   * @param {Object} response - Server response
   * @returns {number} Expiration time in seconds
   * @private
   */
  _parseExpiresIn(response) {
    if (response.expires_in !== undefined) {
      return response.expires_in;
    }
    if (response.expiresIn !== undefined) {
      return response.expiresIn;
    }

    // Calculate from expires_at if available
    const expiresAt = response.expires_at || response.expiresAt;
    if (expiresAt) {
      const expiresDate = new Date(expiresAt);
      const nowMs = Date.now();
      return Math.max(0, Math.floor((expiresDate.getTime() - nowMs) / 1000));
    }

    // Default expiration (1 hour)
    return 3600;
  }

  /**
   * Normalize audience to array format.
   *
   * @param {string|string[]} audience - Audience claim
   * @returns {string[]}
   * @private
   */
  _normalizeAudience(audience) {
    if (!audience) {
      return [];
    }
    if (Array.isArray(audience)) {
      return audience;
    }
    return [audience];
  }
}

export default AuthClient;
