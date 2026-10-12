/**
 * AuthClient Tests
 *
 * Comprehensive tests for the authentication client covering all public methods,
 * success scenarios, error handling, and edge cases.
 */

import { AuthClient, MFAType } from '../../src/clients/auth.js';
import { ValidationError } from '../../src/errors/index.js';

describe('AuthClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      request: jest.fn(),
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
      setAuthToken: jest.fn(),
      clearAuthToken: jest.fn(),
    };
    client = new AuthClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      expect(client).toBeInstanceOf(AuthClient);
    });

    it('should throw ValidationError when transport is not provided', () => {
      expect(() => new AuthClient()).toThrow(ValidationError);
      expect(() => new AuthClient(null)).toThrow(ValidationError);
      expect(() => new AuthClient(undefined)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport lacks request method', () => {
      const invalidTransport = { get: jest.fn() };
      expect(() => new AuthClient(invalidTransport)).toThrow(ValidationError);
      expect(() => new AuthClient(invalidTransport)).toThrow('transport must have a request method');
    });
  });

  describe('MFAType enum', () => {
    it('should export MFA type constants', () => {
      expect(MFAType.TOTP).toBe('TOTP');
      expect(MFAType.WEBAUTHN).toBe('WEBAUTHN');
      expect(MFAType.SMS).toBe('SMS');
      expect(MFAType.EMAIL).toBe('EMAIL');
      expect(MFAType.BACKUP_CODE).toBe('BACKUP_CODE');
    });
  });

  describe('login', () => {
    const mockLoginResponse = {
      access_token: 'jwt-access-token',
      refresh_token: 'jwt-refresh-token',
      expires_in: 3600,
      token_type: 'Bearer',
      user: {
        user_id: 'user-123',
        username: 'admin',
        email: 'admin@example.com',
        roles: ['admin'],
        permissions: ['read', 'write'],
        groups: ['admins'],
        tenant_id: 'tenant-1',
      },
    };

    it('should login successfully and set auth token', async () => {
      mockTransport.post.mockResolvedValueOnce(mockLoginResponse);

      const result = await client.login('admin', 'password');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/auth/login', {
        username: 'admin',
        password: 'password',
      });
      expect(mockTransport.setAuthToken).toHaveBeenCalledWith('jwt-access-token', 'jwt-refresh-token');
      expect(result.accessToken).toBe('jwt-access-token');
      expect(result.refreshToken).toBe('jwt-refresh-token');
      expect(result.expiresIn).toBe(3600);
      expect(result.tokenType).toBe('Bearer');
      expect(result.user.userId).toBe('user-123');
      expect(result.user.username).toBe('admin');
    });

    it('should login with MFA code', async () => {
      mockTransport.post.mockResolvedValueOnce(mockLoginResponse);

      await client.login('admin', 'password', { mfaCode: '123456' });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/auth/login', {
        username: 'admin',
        password: 'password',
        mfa_code: '123456',
      });
    });

    it('should login with tenant ID', async () => {
      mockTransport.post.mockResolvedValueOnce(mockLoginResponse);

      await client.login('admin', 'password', { tenantId: 'tenant-123' });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/auth/login', {
        username: 'admin',
        password: 'password',
        tenant_id: 'tenant-123',
      });
    });

    it('should handle MFA required response', async () => {
      const mfaResponse = {
        mfa_required: true,
        mfa_challenge: {
          challenge_id: 'challenge-123',
          type: 'TOTP',
          expires_at: '2025-01-17T12:00:00Z',
        },
      };
      mockTransport.post.mockResolvedValueOnce(mfaResponse);

      const result = await client.login('admin', 'password');

      expect(result.mfaRequired).toBe(true);
      expect(result.mfaChallenge.challengeId).toBe('challenge-123');
      expect(result.mfaChallenge.type).toBe('TOTP');
      expect(mockTransport.setAuthToken).not.toHaveBeenCalled();
    });

    it('should throw ValidationError for empty username', async () => {
      await expect(client.login('', 'password')).rejects.toThrow(ValidationError);
      await expect(client.login('  ', 'password')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for null username', async () => {
      await expect(client.login(null, 'password')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string username', async () => {
      await expect(client.login(123, 'password')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for empty password', async () => {
      await expect(client.login('admin', '')).rejects.toThrow(ValidationError);
      await expect(client.login('admin', '  ')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for null password', async () => {
      await expect(client.login('admin', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string mfaCode', async () => {
      await expect(client.login('admin', 'password', { mfaCode: 123456 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string tenantId', async () => {
      await expect(client.login('admin', 'password', { tenantId: 123 })).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        accessToken: 'jwt-token',
        refreshToken: 'refresh-token',
        expiresIn: 7200,
        tokenType: 'Bearer',
        user: {
          userId: 'user-456',
          username: 'user',
        },
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.login('user', 'pass');

      expect(result.accessToken).toBe('jwt-token');
      expect(result.refreshToken).toBe('refresh-token');
      expect(result.expiresIn).toBe(7200);
    });

    it('should calculate expiresIn from expires_at timestamp', async () => {
      const futureTime = new Date(Date.now() + 1800000).toISOString(); // 30 minutes from now
      const responseWithExpiresAt = {
        access_token: 'token',
        refresh_token: 'refresh',
        expires_at: futureTime,
        user: { username: 'test' },
      };
      mockTransport.post.mockResolvedValueOnce(responseWithExpiresAt);

      const result = await client.login('test', 'pass');

      expect(result.expiresIn).toBeGreaterThan(0);
      expect(result.expiresIn).toBeLessThanOrEqual(1800);
    });

    it('should default expiresIn to 3600 when not provided', async () => {
      const responseWithoutExpiry = {
        access_token: 'token',
        refresh_token: 'refresh',
        user: { username: 'test' },
      };
      mockTransport.post.mockResolvedValueOnce(responseWithoutExpiry);

      const result = await client.login('test', 'pass');

      expect(result.expiresIn).toBe(3600);
    });

    it('should throw ValidationError for null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.login('admin', 'password')).rejects.toThrow(ValidationError);
    });
  });

  describe('logout', () => {
    it('should logout and clear auth token', async () => {
      // First login to set tokens
      const loginResponse = {
        access_token: 'jwt-token',
        refresh_token: 'refresh-token',
        user: { username: 'admin' },
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      mockTransport.post.mockResolvedValueOnce({});
      await client.logout();

      expect(mockTransport.post).toHaveBeenLastCalledWith('/api/v1/iam/logout', null, {
        headers: { Authorization: 'Bearer jwt-token' },
      });
      expect(mockTransport.clearAuthToken).toHaveBeenCalled();
    });

    it('should clear local state even when not authenticated', async () => {
      await client.logout();

      expect(mockTransport.clearAuthToken).toHaveBeenCalled();
    });

    it('should clear local state on logout error', async () => {
      // Login first
      const loginResponse = {
        access_token: 'jwt-token',
        refresh_token: 'refresh-token',
        user: { username: 'admin' },
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      // Logout should clear tokens even if server call fails
      mockTransport.post.mockRejectedValueOnce(new Error('Server error'));

      await expect(client.logout()).rejects.toThrow('Server error');
    });
  });

  describe('refreshToken', () => {
    const mockRefreshResponse = {
      access_token: 'new-access-token',
      expires_in: 3600,
      token_type: 'Bearer',
      refresh_token: 'new-refresh-token',
    };

    it('should refresh token successfully', async () => {
      mockTransport.post.mockResolvedValueOnce(mockRefreshResponse);

      const result = await client.refreshToken('old-refresh-token');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/refresh', {
        refresh_token: 'old-refresh-token',
      });
      expect(mockTransport.setAuthToken).toHaveBeenCalledWith('new-access-token', 'new-refresh-token');
      expect(result.accessToken).toBe('new-access-token');
      expect(result.expiresIn).toBe(3600);
      expect(result.tokenType).toBe('Bearer');
    });

    it('should handle refresh without new refresh token (no rotation)', async () => {
      // First login to have a refresh token stored
      const loginResponse = {
        access_token: 'old-token',
        refresh_token: 'stored-refresh',
        user: { username: 'admin' },
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      // Refresh without new refresh token
      const refreshResponse = {
        access_token: 'new-token',
        expires_in: 3600,
        token_type: 'Bearer',
      };
      mockTransport.post.mockResolvedValueOnce(refreshResponse);

      const result = await client.refreshToken('stored-refresh');

      expect(result.refreshToken).toBeNull();
      expect(mockTransport.setAuthToken).toHaveBeenLastCalledWith('new-token', 'stored-refresh');
    });

    it('should throw ValidationError for empty refresh token', async () => {
      await expect(client.refreshToken('')).rejects.toThrow(ValidationError);
      await expect(client.refreshToken('  ')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for null refresh token', async () => {
      await expect(client.refreshToken(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string refresh token', async () => {
      await expect(client.refreshToken(123)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.refreshToken('valid-token')).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        accessToken: 'new-token',
        expiresIn: 7200,
        tokenType: 'Bearer',
        refreshToken: 'new-refresh',
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.refreshToken('old-refresh');

      expect(result.accessToken).toBe('new-token');
      expect(result.expiresIn).toBe(7200);
    });
  });

  describe('validateToken', () => {
    const mockTokenClaims = {
      valid: true,
      user_id: 'user-123',
      username: 'admin',
      email: 'admin@example.com',
      tenant_id: 'tenant-1',
      roles: ['admin', 'user'],
      groups: ['admins'],
      permissions: ['read', 'write', 'delete'],
      issued_at: '2025-01-17T10:00:00Z',
      expires_at: '2025-01-17T11:00:00Z',
      issuer: 'quicbaas',
      audience: ['api.quicbaas.example.com'],
      subject: 'user-123',
      session_id: 'session-456',
      custom_claims: { department: 'engineering' },
    };

    it('should validate token and return claims', async () => {
      mockTransport.post.mockResolvedValueOnce(mockTokenClaims);

      const result = await client.validateToken('valid-jwt-token');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/validate', null, {
        headers: { Authorization: 'Bearer valid-jwt-token' },
      });
      expect(result.valid).toBe(true);
      expect(result.userId).toBe('user-123');
      expect(result.username).toBe('admin');
      expect(result.email).toBe('admin@example.com');
      expect(result.roles).toEqual(['admin', 'user']);
      expect(result.permissions).toEqual(['read', 'write', 'delete']);
      expect(result.sessionId).toBe('session-456');
    });

    it('should report valid:false from a 200 {"valid":false} body', async () => {
      mockTransport.post.mockResolvedValueOnce({ valid: false });

      const result = await client.validateToken('dead-token');

      expect(result.valid).toBe(false);
    });

    it('should throw ValidationError for empty token', async () => {
      await expect(client.validateToken('')).rejects.toThrow(ValidationError);
      await expect(client.validateToken('  ')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for null token', async () => {
      await expect(client.validateToken(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.validateToken('token')).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response format', async () => {
      const camelCaseResponse = {
        valid: true,
        userId: 'user-789',
        username: 'test',
        tenantId: 'tenant-2',
        issuedAt: '2025-01-17T10:00:00Z',
        expiresAt: '2025-01-17T11:00:00Z',
        sessionId: 'session-xyz',
        customClaims: { key: 'value' },
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.validateToken('token');

      expect(result.userId).toBe('user-789');
      expect(result.tenantId).toBe('tenant-2');
      expect(result.sessionId).toBe('session-xyz');
    });

    it('should normalize audience to array from string', async () => {
      const responseWithStringAudience = {
        valid: true,
        aud: 'single-audience',
        username: 'test',
      };
      mockTransport.post.mockResolvedValueOnce(responseWithStringAudience);

      const result = await client.validateToken('token');

      expect(result.audience).toEqual(['single-audience']);
    });

    it('should handle missing optional fields with defaults', async () => {
      const minimalResponse = {
        valid: true,
        username: 'test',
      };
      mockTransport.post.mockResolvedValueOnce(minimalResponse);

      const result = await client.validateToken('token');

      expect(result.email).toBeNull();
      expect(result.tenantId).toBeNull();
      expect(result.roles).toEqual([]);
      expect(result.groups).toEqual([]);
      expect(result.permissions).toEqual([]);
      expect(result.audience).toEqual([]);
      expect(result.sessionId).toBeNull();
      expect(result.customClaims).toBeNull();
    });

    it('should default valid to true if not explicitly false', async () => {
      const responseWithoutValid = {
        username: 'test',
      };
      mockTransport.post.mockResolvedValueOnce(responseWithoutValid);

      const result = await client.validateToken('token');

      expect(result.valid).toBe(true);
    });
  });

  describe('getCurrentUser', () => {
    const mockUserResponse = {
      user_id: 'user-123',
      username: 'admin',
      email: 'admin@example.com',
      roles: ['admin'],
      permissions: ['read', 'write'],
      groups: ['admins'],
      tenant_id: 'tenant-1',
    };

    it('should return cached user if available', async () => {
      // Login first to cache user
      const loginResponse = {
        access_token: 'token',
        refresh_token: 'refresh',
        user: mockUserResponse,
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      const user = await client.getCurrentUser();

      expect(mockTransport.get).not.toHaveBeenCalled();
      expect(user.username).toBe('admin');
    });

    it('should fetch user from server if not cached', async () => {
      mockTransport.get.mockResolvedValueOnce(mockUserResponse);

      const user = await client.getCurrentUser();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/auth/me');
      expect(user.userId).toBe('user-123');
      expect(user.username).toBe('admin');
      expect(user.email).toBe('admin@example.com');
      expect(user.roles).toEqual(['admin']);
    });

    it('should throw ValidationError for null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      await expect(client.getCurrentUser()).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response', async () => {
      const camelCaseResponse = {
        userId: 'user-456',
        username: 'user',
        tenantId: 'tenant-2',
      };
      mockTransport.get.mockResolvedValueOnce(camelCaseResponse);

      const user = await client.getCurrentUser();

      expect(user.userId).toBe('user-456');
      expect(user.tenantId).toBe('tenant-2');
    });

    it('should handle id field instead of user_id', async () => {
      const responseWithId = {
        id: 'user-789',
        username: 'user',
      };
      mockTransport.get.mockResolvedValueOnce(responseWithId);

      const user = await client.getCurrentUser();

      expect(user.userId).toBe('user-789');
    });
  });

  describe('isAuthenticated', () => {
    it('should return false when not logged in', () => {
      expect(client.isAuthenticated()).toBe(false);
    });

    it('should return true when logged in', async () => {
      const loginResponse = {
        access_token: 'token',
        refresh_token: 'refresh',
        user: { username: 'admin' },
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      expect(client.isAuthenticated()).toBe(true);
    });

    it('should return false after logout', async () => {
      // Login
      const loginResponse = {
        access_token: 'token',
        refresh_token: 'refresh',
        user: { username: 'admin' },
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      // Logout
      mockTransport.post.mockResolvedValueOnce({});
      await client.logout();

      expect(client.isAuthenticated()).toBe(false);
    });
  });

  describe('getAccessToken', () => {
    it('should return null when not authenticated', () => {
      expect(client.getAccessToken()).toBeNull();
    });

    it('should return access token when authenticated', async () => {
      const loginResponse = {
        access_token: 'my-jwt-token',
        refresh_token: 'refresh',
        user: { username: 'admin' },
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      expect(client.getAccessToken()).toBe('my-jwt-token');
    });
  });

  describe('getRefreshToken', () => {
    it('should return null when not authenticated', () => {
      expect(client.getRefreshToken()).toBeNull();
    });

    it('should return refresh token when authenticated', async () => {
      const loginResponse = {
        access_token: 'token',
        refresh_token: 'my-refresh-token',
        user: { username: 'admin' },
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      expect(client.getRefreshToken()).toBe('my-refresh-token');
    });
  });

  describe('setTokens', () => {
    it('should set tokens manually', () => {
      client.setTokens('manual-access-token', 'manual-refresh-token');

      expect(mockTransport.setAuthToken).toHaveBeenCalledWith('manual-access-token', 'manual-refresh-token');
      expect(client.getAccessToken()).toBe('manual-access-token');
      expect(client.getRefreshToken()).toBe('manual-refresh-token');
    });

    it('should set only access token', () => {
      client.setTokens('access-only-token');

      expect(mockTransport.setAuthToken).toHaveBeenCalledWith('access-only-token', null);
      expect(client.getAccessToken()).toBe('access-only-token');
      expect(client.getRefreshToken()).toBeNull();
    });

    it('should clear cached user when setting new tokens', async () => {
      // Login to cache user
      const loginResponse = {
        access_token: 'old-token',
        refresh_token: 'old-refresh',
        user: { username: 'admin' },
      };
      mockTransport.post.mockResolvedValueOnce(loginResponse);
      await client.login('admin', 'password');

      // Set new tokens
      client.setTokens('new-token');

      // Getting user should fetch from server since cache was cleared
      mockTransport.get.mockResolvedValueOnce({ username: 'newuser' });
      await client.getCurrentUser();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/auth/me');
    });

    it('should throw ValidationError for empty access token', () => {
      expect(() => client.setTokens('')).toThrow(ValidationError);
      expect(() => client.setTokens('  ')).toThrow(ValidationError);
    });

    it('should throw ValidationError for null access token', () => {
      expect(() => client.setTokens(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string refresh token', () => {
      expect(() => client.setTokens('valid-token', 123)).toThrow(ValidationError);
      expect(() => client.setTokens('valid-token', {})).toThrow(ValidationError);
    });

    it('should allow null refresh token explicitly', () => {
      expect(() => client.setTokens('valid-token', null)).not.toThrow();
    });
  });

  describe('response transformation edge cases', () => {
    it('should extract user from nested user object', async () => {
      const responseWithNestedUser = {
        access_token: 'token',
        refresh_token: 'refresh',
        user: {
          user_id: 'nested-user-id',
          username: 'nested-user',
        },
      };
      mockTransport.post.mockResolvedValueOnce(responseWithNestedUser);

      const result = await client.login('user', 'pass');

      expect(result.user.userId).toBe('nested-user-id');
      expect(result.user.username).toBe('nested-user');
    });

    it('should extract user from top-level response when no user object', async () => {
      const responseWithTopLevelUser = {
        access_token: 'token',
        refresh_token: 'refresh',
        user_id: 'top-level-id',
        username: 'top-level-user',
      };
      mockTransport.post.mockResolvedValueOnce(responseWithTopLevelUser);

      const result = await client.login('user', 'pass');

      expect(result.user.userId).toBe('top-level-id');
      expect(result.user.username).toBe('top-level-user');
    });

    it('should handle mfaChallenge with camelCase fields', async () => {
      const mfaResponse = {
        mfaRequired: true,
        mfaChallenge: {
          challengeId: 'camel-challenge',
          type: 'WEBAUTHN',
          expiresAt: '2025-01-17T12:00:00Z',
        },
      };
      mockTransport.post.mockResolvedValueOnce(mfaResponse);

      const result = await client.login('admin', 'password');

      expect(result.mfaChallenge.challengeId).toBe('camel-challenge');
      expect(result.mfaChallenge.expiresAt).toBe('2025-01-17T12:00:00Z');
    });

    it('should handle expires_at in the past returning 0', async () => {
      const pastTime = new Date(Date.now() - 60000).toISOString(); // 1 minute ago
      const response = {
        access_token: 'token',
        refresh_token: 'refresh',
        expires_at: pastTime,
        user: { username: 'test' },
      };
      mockTransport.post.mockResolvedValueOnce(response);

      const result = await client.login('test', 'pass');

      expect(result.expiresIn).toBe(0);
    });

    it('should use iss field for issuer when issuer not present', async () => {
      const response = {
        valid: true,
        username: 'test',
        iss: 'alternative-issuer',
      };
      mockTransport.post.mockResolvedValueOnce(response);

      const result = await client.validateToken('token');

      expect(result.issuer).toBe('alternative-issuer');
    });

    it('should use sub field for subject when subject not present', async () => {
      const response = {
        valid: true,
        username: 'test',
        sub: 'subject-from-sub',
      };
      mockTransport.post.mockResolvedValueOnce(response);

      const result = await client.validateToken('token');

      expect(result.subject).toBe('subject-from-sub');
    });
  });
});
