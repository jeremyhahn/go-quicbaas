/**
 * IAMClient Tests
 *
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import IAMClient, {
  TenantStatus,
  UserStatus,
  ExportFormat,
  AuditEventType,
} from '../../src/clients/iam.js';
import { NotFoundError, ValidationError } from '../../src/errors/index.js';

describe('IAMClient', () => {
  let client;
  let mockTransport;

  beforeEach(() => {
    mockTransport = {
      get: jest.fn(),
      post: jest.fn(),
      put: jest.fn(),
      patch: jest.fn(),
      delete: jest.fn(),
    };
    client = new IAMClient(mockTransport);
  });

  // ==========================================================================
  // Constructor Tests
  // ==========================================================================

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      expect(client).toBeInstanceOf(IAMClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new IAMClient()).toThrow(ValidationError);
      expect(() => new IAMClient(null)).toThrow(ValidationError);
      expect(() => new IAMClient(undefined)).toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Tenant Management Tests
  // ==========================================================================

  describe('createTenant', () => {
    it('should POST the tenant collection with id and name', async () => {
      mockTransport.post.mockResolvedValue({ id: 't1', name: 'Acme' });

      const result = await client.createTenant('t1', 'Acme', {
        settings: { mfa_required: true },
        quotas: { max_users: 10 },
        isolationType: 'namespace',
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/', {
        id: 't1',
        name: 'Acme',
        settings: { mfa_required: true },
        quotas: { max_users: 10 },
        isolation_type: 'namespace',
      });
      expect(result).toEqual({ id: 't1', name: 'Acme' });
    });

    it('should throw ValidationError when id or name is missing', async () => {
      await expect(client.createTenant('', 'Acme')).rejects.toThrow(ValidationError);
      await expect(client.createTenant('t1', '')).rejects.toThrow(ValidationError);
      expect(mockTransport.post).not.toHaveBeenCalled();
    });
  });

  describe('getTenant', () => {
    it('should GET the tenant path', async () => {
      mockTransport.get.mockResolvedValue({ id: 't/1' });

      await client.getTenant('t/1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t%2F1');
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.getTenant('')).rejects.toThrow(ValidationError);
    });
  });

  describe('updateTenant', () => {
    it('should PUT name, settings and quotas to the tenant path', async () => {
      mockTransport.put.mockResolvedValue({ id: 't1' });

      await client.updateTenant('t1', { name: 'New', settings: { a: 1 }, quotas: { b: 2 } });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/tenants/t1', {
        name: 'New',
        settings: { a: 1 },
        quotas: { b: 2 },
      });
    });

    it('should throw ValidationError when updates are missing', async () => {
      await expect(client.updateTenant('t1')).rejects.toThrow(ValidationError);
      await expect(client.updateTenant('', {})).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteTenant', () => {
    it('should DELETE the tenant path', async () => {
      mockTransport.delete.mockResolvedValue(undefined);

      await client.deleteTenant('t1');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/tenants/t1');
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.deleteTenant()).rejects.toThrow(ValidationError);
    });
  });

  describe('listTenants', () => {
    it('should GET the tenant collection with limit and offset', async () => {
      mockTransport.get.mockResolvedValue({ tenants: [], limit: 5, offset: 10 });

      await client.listTenants({ limit: 5, offset: 10 });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/', {
        query: { limit: 5, offset: 10 },
      });
    });

    it('should send an empty query by default', async () => {
      await client.listTenants();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/', { query: {} });
    });

    it('should throw ValidationError for an invalid limit or offset', async () => {
      await expect(client.listTenants({ limit: 0 })).rejects.toThrow(ValidationError);
      await expect(client.listTenants({ offset: -1 })).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // User Management Tests
  // ==========================================================================

  describe('createUser', () => {
    it('should POST the user collection with the handler body', async () => {
      mockTransport.post.mockResolvedValue({ id: '7' });

      await client.createUser('t1', 'a@example.com', {
        username: 'alice',
        name: 'Alice',
        attributes: { dept: 'eng' },
        enabled: true,
        authMethods: ['piv'],
        roles: ['admin'],
        lifecycle: 'pending',
        inviteExpiryHours: 48,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/t1/users/', {
        email: 'a@example.com',
        username: 'alice',
        name: 'Alice',
        attributes: { dept: 'eng' },
        enabled: true,
        auth_methods: ['piv'],
        roles: ['admin'],
        lifecycle: 'pending',
        invite_expiry_hours: 48,
      });
    });

    it('should throw ValidationError when tenantId or email is missing', async () => {
      await expect(client.createUser('', 'a@example.com')).rejects.toThrow(ValidationError);
      await expect(client.createUser('t1', '')).rejects.toThrow(ValidationError);
      expect(mockTransport.post).not.toHaveBeenCalled();
    });
  });

  describe('getUser', () => {
    it('should GET the user path', async () => {
      await client.getUser('t1', '7');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t1/users/7');
    });

    it('should throw ValidationError when userId is missing', async () => {
      await expect(client.getUser('t1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('updateUser', () => {
    it('should PUT the handler body to the user path', async () => {
      await client.updateUser('t1', '7', {
        email: 'b@example.com',
        username: 'bob',
        name: 'Bob',
        attributes: { dept: 'ops' },
        enabled: false,
        roles: ['viewer'],
        authMethods: ['fido2'],
        lifecycle: 'active',
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/tenants/t1/users/7', {
        email: 'b@example.com',
        username: 'bob',
        name: 'Bob',
        attributes: { dept: 'ops' },
        enabled: false,
        roles: ['viewer'],
        auth_methods: ['fido2'],
        lifecycle: 'active',
      });
    });

    it('should throw ValidationError when updates are missing', async () => {
      await expect(client.updateUser('t1', '7')).rejects.toThrow(ValidationError);
      await expect(client.updateUser('t1', '', {})).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteUser', () => {
    it('should DELETE the user path', async () => {
      await client.deleteUser('t1', '7');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/tenants/t1/users/7');
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.deleteUser('', '7')).rejects.toThrow(ValidationError);
    });
  });

  describe('listUsers', () => {
    it('should GET the user collection with limit and offset', async () => {
      await client.listUsers('t1', { limit: 20, offset: 40 });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t1/users/', {
        query: { limit: 20, offset: 40 },
      });
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.listUsers('')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Role Management Tests
  // ==========================================================================

  describe('createRole', () => {
    it('should POST the role collection with the handler body', async () => {
      await client.createRole('t1', 'editor', ['kv:write'], {
        id: 'r1',
        description: 'Edits',
        attributes: { level: 2 },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/t1/roles/', {
        id: 'r1',
        name: 'editor',
        description: 'Edits',
        permissions: ['kv:write'],
        attributes: { level: 2 },
      });
    });

    it('should throw ValidationError for missing name or empty permissions', async () => {
      await expect(client.createRole('t1', '', ['p'])).rejects.toThrow(ValidationError);
      await expect(client.createRole('t1', 'editor', [])).rejects.toThrow(ValidationError);
      await expect(client.createRole('t1', 'editor')).rejects.toThrow(ValidationError);
    });
  });

  describe('updateRole', () => {
    it('should PUT the handler body to the role path', async () => {
      await client.updateRole('t1', 'r 1', {
        name: 'writer',
        description: 'Writes',
        permissions: ['kv:write'],
        attributes: { level: 3 },
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/tenants/t1/roles/r%201', {
        name: 'writer',
        description: 'Writes',
        permissions: ['kv:write'],
        attributes: { level: 3 },
      });
    });

    it('should throw ValidationError when roleId or updates are missing', async () => {
      await expect(client.updateRole('t1', '', {})).rejects.toThrow(ValidationError);
      await expect(client.updateRole('t1', 'r1')).rejects.toThrow(ValidationError);
    });
  });

  describe('listRoles', () => {
    it('should GET the role collection with limit and offset', async () => {
      await client.listRoles('t1', { limit: 3 });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t1/roles/', {
        query: { limit: 3 },
      });
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.listRoles()).rejects.toThrow(ValidationError);
    });
  });

  describe('assignRole', () => {
    it('should POST the role_id to the tenant user roles route', async () => {
      mockTransport.post.mockResolvedValueOnce(undefined);

      await client.assignRole('tenant-1', 'user-1', 'role-1');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/tenant-1/users/user-1/roles', {
        role_id: 'role-1',
      });
    });

    it('should URL-encode the path segments', async () => {
      mockTransport.post.mockResolvedValueOnce(undefined);

      await client.assignRole('t/1', 'u 1', 'role-1');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/t%2F1/users/u%201/roles', {
        role_id: 'role-1',
      });
    });

    it('should surface a server error as the SDK error type', async () => {
      mockTransport.post.mockRejectedValueOnce(new NotFoundError('user not found'));

      await expect(client.assignRole('tenant-1', 'user-1', 'role-1')).rejects.toThrow(NotFoundError);
    });

    it.each([
      ['tenantId', '', 'user-1', 'role-1'],
      ['userId', 'tenant-1', '', 'role-1'],
      ['roleId', 'tenant-1', 'user-1', ''],
    ])('should throw ValidationError without a request when %s is empty', async (_name, tenantId, userId, roleId) => {
      await expect(client.assignRole(tenantId, userId, roleId)).rejects.toThrow(ValidationError);
      expect(mockTransport.post).not.toHaveBeenCalled();
    });
  });

  describe('revokeRole', () => {
    it('should DELETE the role from the tenant user roles route', async () => {
      mockTransport.delete.mockResolvedValueOnce(undefined);

      await client.revokeRole('tenant-1', 'user-1', 'role-1');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/tenants/tenant-1/users/user-1/roles/role-1');
    });

    it('should URL-encode the path segments', async () => {
      mockTransport.delete.mockResolvedValueOnce(undefined);

      await client.revokeRole('t/1', 'u 1', 'r/1');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/tenants/t%2F1/users/u%201/roles/r%2F1');
    });

    it('should surface a server error as the SDK error type', async () => {
      mockTransport.delete.mockRejectedValueOnce(new NotFoundError('role not assigned'));

      await expect(client.revokeRole('tenant-1', 'user-1', 'role-1')).rejects.toThrow(NotFoundError);
    });

    it.each([
      ['tenantId', '', 'user-1', 'role-1'],
      ['userId', 'tenant-1', '', 'role-1'],
      ['roleId', 'tenant-1', 'user-1', ''],
    ])('should throw ValidationError without a request when %s is empty', async (_name, tenantId, userId, roleId) => {
      await expect(client.revokeRole(tenantId, userId, roleId)).rejects.toThrow(ValidationError);
      expect(mockTransport.delete).not.toHaveBeenCalled();
    });
  });

  // ==========================================================================
  // Policy Management Tests
  // ==========================================================================

  describe('setPolicy', () => {
    const policy = { name: 'p1', rules: [{ effect: 'allow', actions: ['read'] }] };

    it('should POST the policy as is to the policy collection', async () => {
      await client.setPolicy('t1', policy);

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/t1/policies/', policy);
    });

    it('should throw ValidationError for a missing name or empty rules', async () => {
      await expect(client.setPolicy('t1', { rules: policy.rules })).rejects.toThrow(
        ValidationError,
      );
      await expect(client.setPolicy('t1', { name: 'p1', rules: [] })).rejects.toThrow(
        ValidationError,
      );
      await expect(client.setPolicy('', policy)).rejects.toThrow(ValidationError);
    });
  });

  describe('getPolicy', () => {
    it('should GET one policy by name', async () => {
      await client.getPolicy('t1', 'p/1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t1/policies/p%2F1');
    });

    it('should GET the policy collection without a name', async () => {
      await client.getPolicy('t1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t1/policies/');
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.getPolicy('')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Application Management Tests
  // ==========================================================================

  describe('createApp', () => {
    it('should POST the app collection with the handler body', async () => {
      await client.createApp('t1', 'portal', ['https://app.example.com/cb'], {
        resourcePrefix: 'portal/',
        grantTypes: ['authorization_code'],
        responseTypes: ['code'],
        pkceRequired: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/t1/apps/', {
        name: 'portal',
        redirect_uris: ['https://app.example.com/cb'],
        resource_prefix: 'portal/',
        grant_types: ['authorization_code'],
        response_types: ['code'],
        pkce_required: true,
      });
    });

    it('should default pkce_required to false', async () => {
      await client.createApp('t1', 'portal', ['https://app.example.com/cb']);

      expect(mockTransport.post.mock.calls[0][1].pkce_required).toBe(false);
    });

    it('should throw ValidationError for a missing name or no redirect URIs', async () => {
      await expect(client.createApp('t1', '', ['https://x'])).rejects.toThrow(ValidationError);
      await expect(client.createApp('t1', 'portal', [])).rejects.toThrow(ValidationError);
      await expect(client.createApp('t1', 'portal')).rejects.toThrow(ValidationError);
    });
  });

  describe('getApp', () => {
    it('should GET the app path', async () => {
      await client.getApp('t1', 'client-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t1/apps/client-1');
    });

    it('should throw ValidationError when appId is missing', async () => {
      await expect(client.getApp('t1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteApp', () => {
    it('should DELETE the app path', async () => {
      await client.deleteApp('t1', 'client-1');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/tenants/t1/apps/client-1');
    });

    it('should throw ValidationError when appId is missing', async () => {
      await expect(client.deleteApp('t1')).rejects.toThrow(ValidationError);
    });
  });

  describe('listApps', () => {
    it('should GET the app collection with limit and offset', async () => {
      await client.listApps('t1', { limit: 10, offset: 0 });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t1/apps/', {
        query: { limit: 10, offset: 0 },
      });
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.listApps('')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Session Management Tests
  // ==========================================================================

  describe('createSession', () => {
    it('should POST the user session collection with the handler body', async () => {
      const deviceInfo = { id: 'd1', trusted: true };

      await client.createSession('t1', '7', 'hash-1', {
        id: 's1',
        mfaVerified: true,
        ipAddress: '192.0.2.1',
        userAgent: 'jest',
        deviceInfo,
        expiresAt: '2026-01-01T00:00:00Z',
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/t1/users/7/sessions/', {
        id: 's1',
        token_hash: 'hash-1',
        mfa_verified: true,
        ip_address: '192.0.2.1',
        user_agent: 'jest',
        device_info: deviceInfo,
        expires_at: '2026-01-01T00:00:00Z',
      });
    });

    it('should throw ValidationError when tokenHash or userId is missing', async () => {
      await expect(client.createSession('t1', '7', '')).rejects.toThrow(ValidationError);
      await expect(client.createSession('t1', '', 'hash-1')).rejects.toThrow(ValidationError);
      expect(mockTransport.post).not.toHaveBeenCalled();
    });
  });

  describe('listSessions', () => {
    it('should GET the user session collection with a limit', async () => {
      await client.listSessions('t1', '7', { limit: 5 });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/tenants/t1/users/7/sessions/', {
        query: { limit: 5 },
      });
    });

    it('should throw ValidationError for a missing userId or invalid limit', async () => {
      await expect(client.listSessions('t1', '')).rejects.toThrow(ValidationError);
      await expect(client.listSessions('t1', '7', { limit: 0 })).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Audit Log Tests
  // ==========================================================================

  describe('getAuditLog', () => {
    it('should GET the audit route with every handler filter', async () => {
      await client.getAuditLog('t1', {
        eventType: 'authentication',
        category: 'security',
        outcome: 'success',
        actorId: 'u1',
        resourceId: 'r1',
        startTime: '2026-01-01T00:00:00Z',
        endTime: '2026-01-02T00:00:00Z',
        limit: 50,
        offset: 100,
      });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/audit', {
        query: {
          tenant_id: 't1',
          event_type: 'authentication',
          category: 'security',
          outcome: 'success',
          actor_id: 'u1',
          resource_id: 'r1',
          start_time: '2026-01-01T00:00:00Z',
          end_time: '2026-01-02T00:00:00Z',
          limit: 50,
          offset: 100,
        },
      });
    });

    it('should send only the tenant when no filter is set', async () => {
      await client.getAuditLog('t1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/audit', {
        query: { tenant_id: 't1' },
      });
    });

    it('should throw ValidationError for a missing tenantId or invalid limit', async () => {
      await expect(client.getAuditLog('')).rejects.toThrow(ValidationError);
      await expect(client.getAuditLog('t1', { limit: -1 })).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // GDPR Tests
  // ==========================================================================

  describe('exportUserData', () => {
    it('should POST the export route with the handler body', async () => {
      await client.exportUserData('t1', '7', {
        format: ExportFormat.CSV,
        requestedBy: 'dpo',
        includeCategories: ['profile'],
        excludeCategories: ['audit'],
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/t1/users/7/gdpr/export', {
        format: 'csv',
        requested_by: 'dpo',
        include_categories: ['profile'],
        exclude_categories: ['audit'],
      });
    });

    it('should default the format to json', async () => {
      await client.exportUserData('t1', '7');

      expect(mockTransport.post.mock.calls[0][1].format).toBe('json');
    });

    it('should throw ValidationError when userId is missing', async () => {
      await expect(client.exportUserData('t1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteUserData', () => {
    it('should POST the deletion route with the handler body', async () => {
      await client.deleteUserData('t1', '7', {
        requestedBy: 'dpo',
        reason: 'user request',
        deleteCategories: ['profile'],
        preserveCategories: ['billing'],
        gracePeriodDays: 30,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/tenants/t1/users/7/gdpr/deletion', {
        requested_by: 'dpo',
        reason: 'user request',
        delete_categories: ['profile'],
        preserve_categories: ['billing'],
        grace_period_days: 30,
      });
    });

    it('should throw ValidationError when tenantId is missing', async () => {
      await expect(client.deleteUserData('', '7')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Enum Tests
  // ==========================================================================

  describe('TenantStatus enum', () => {
    it('should export all tenant status values', () => {
      expect(TenantStatus.UNSPECIFIED).toBe('UNSPECIFIED');
      expect(TenantStatus.ACTIVE).toBe('ACTIVE');
      expect(TenantStatus.SUSPENDED).toBe('SUSPENDED');
      expect(TenantStatus.DELETED).toBe('DELETED');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(TenantStatus)).toBe(true);
    });
  });

  describe('UserStatus enum', () => {
    it('should export all user status values', () => {
      expect(UserStatus.UNSPECIFIED).toBe('UNSPECIFIED');
      expect(UserStatus.ACTIVE).toBe('ACTIVE');
      expect(UserStatus.SUSPENDED).toBe('SUSPENDED');
      expect(UserStatus.PENDING_VERIFICATION).toBe('PENDING_VERIFICATION');
      expect(UserStatus.DELETED).toBe('DELETED');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(UserStatus)).toBe(true);
    });
  });

  describe('ExportFormat enum', () => {
    it('should export all export format values', () => {
      expect(ExportFormat.JSON).toBe('json');
      expect(ExportFormat.CSV).toBe('csv');
      expect(ExportFormat.XML).toBe('xml');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(ExportFormat)).toBe(true);
    });
  });

  describe('AuditEventType enum', () => {
    it('should export all audit event type values', () => {
      expect(AuditEventType.AUTHENTICATION).toBe('authentication');
      expect(AuditEventType.AUTHORIZATION).toBe('authorization');
      expect(AuditEventType.USER_MANAGEMENT).toBe('user_management');
      expect(AuditEventType.ROLE_MANAGEMENT).toBe('role_management');
      expect(AuditEventType.POLICY_MANAGEMENT).toBe('policy_management');
      expect(AuditEventType.DATA_ACCESS).toBe('data_access');
      expect(AuditEventType.CONFIGURATION).toBe('configuration');
      expect(AuditEventType.SYSTEM).toBe('system');
      expect(AuditEventType.GDPR).toBe('gdpr');
      expect(AuditEventType.ADMIN).toBe('admin');
    });

    it('should be frozen', () => {
      expect(Object.isFrozen(AuditEventType)).toBe(true);
    });
  });
});
