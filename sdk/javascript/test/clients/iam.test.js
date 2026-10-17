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
    it('should create a tenant with name only', async () => {
      const mockResponse = { id: 'tenant-1', name: 'acme', status: 'ACTIVE' };
      mockTransport.post.mockResolvedValueOnce(mockResponse);

      const result = await client.createTenant('acme');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants', {
        name: 'acme',
        display_name: undefined,
        description: undefined,
        settings: undefined,
        metadata: undefined,
      });
      expect(result.id).toBe('tenant-1');
    });

    it('should create a tenant with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'tenant-1', name: 'acme' });

      await client.createTenant('acme', {
        displayName: 'Acme Corp',
        description: 'Test tenant',
        settings: { maxUsers: 100 },
        metadata: { tier: 'premium' },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants', {
        name: 'acme',
        display_name: 'Acme Corp',
        description: 'Test tenant',
        settings: { maxUsers: 100 },
        metadata: { tier: 'premium' },
      });
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.createTenant('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is not a string', async () => {
      await expect(client.createTenant(123)).rejects.toThrow(ValidationError);
      await expect(client.createTenant(null)).rejects.toThrow(ValidationError);
    });
  });

  describe('getTenant', () => {
    it('should get a tenant by ID', async () => {
      const mockResponse = { id: 'tenant-1', name: 'acme', status: 'ACTIVE' };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.getTenant('tenant-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1');
      expect(result.name).toBe('acme');
    });

    it('should URL-encode tenant ID', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getTenant('tenant/1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant%2F1');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.getTenant('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when tenantId is not a string', async () => {
      await expect(client.getTenant(123)).rejects.toThrow(ValidationError);
    });
  });

  describe('updateTenant', () => {
    it('should update a tenant', async () => {
      mockTransport.put.mockResolvedValueOnce({ id: 'tenant-1', name: 'acme' });

      await client.updateTenant('tenant-1', {
        displayName: 'Updated Acme',
        description: 'Updated description',
        settings: { maxUsers: 200 },
        metadata: { tier: 'enterprise' },
        status: TenantStatus.SUSPENDED,
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1', {
        display_name: 'Updated Acme',
        description: 'Updated description',
        settings: { maxUsers: 200 },
        metadata: { tier: 'enterprise' },
        status: 'SUSPENDED',
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.updateTenant('', { displayName: 'X' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when updates is null', async () => {
      await expect(client.updateTenant('tenant-1', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when updates is not an object', async () => {
      await expect(client.updateTenant('tenant-1', 'invalid')).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteTenant', () => {
    it('should delete a tenant', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      await client.deleteTenant('tenant-1');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.deleteTenant('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when tenantId is not a string', async () => {
      await expect(client.deleteTenant(null)).rejects.toThrow(ValidationError);
    });
  });

  describe('listTenants', () => {
    it('should list all tenants', async () => {
      const mockResponse = {
        tenants: [{ id: 'tenant-1' }, { id: 'tenant-2' }],
        pagination: { totalCount: 2 },
      };
      mockTransport.get.mockResolvedValueOnce(mockResponse);

      const result = await client.listTenants();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants', { query: {} });
      expect(result.tenants).toHaveLength(2);
    });

    it('should list tenants with pagination', async () => {
      mockTransport.get.mockResolvedValueOnce({ tenants: [] });

      await client.listTenants({ pageSize: 10, pageToken: 'abc123', filter: 'status=ACTIVE' });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants', {
        query: { page_size: 10, page_token: 'abc123', filter: 'status=ACTIVE' },
      });
    });

    it('should throw ValidationError when pageSize is not positive', async () => {
      await expect(client.listTenants({ pageSize: 0 })).rejects.toThrow(ValidationError);
      await expect(client.listTenants({ pageSize: -1 })).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // User Management Tests
  // ==========================================================================

  describe('createUser', () => {
    it('should create a user with required options', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'user-1', username: 'alice' });

      await client.createUser('tenant-1', 'alice', {
        email: 'alice@example.com',
        password: 'secret123',
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/users', {
        username: 'alice',
        email: 'alice@example.com',
        password: 'secret123',
        display_name: undefined,
        roles: undefined,
        groups: undefined,
        metadata: undefined,
        require_email_verification: undefined,
      });
    });

    it('should create a user with all options', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'user-1' });

      await client.createUser('tenant-1', 'alice', {
        email: 'alice@example.com',
        password: 'secret123',
        displayName: 'Alice Smith',
        roles: ['admin'],
        groups: ['developers'],
        metadata: { department: 'engineering' },
        requireEmailVerification: true,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/users', {
        username: 'alice',
        email: 'alice@example.com',
        password: 'secret123',
        display_name: 'Alice Smith',
        roles: ['admin'],
        groups: ['developers'],
        metadata: { department: 'engineering' },
        require_email_verification: true,
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(
        client.createUser('', 'alice', { email: 'a@b.com', password: 'x' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when username is empty', async () => {
      await expect(
        client.createUser('tenant-1', '', { email: 'a@b.com', password: 'x' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when email is missing', async () => {
      await expect(
        client.createUser('tenant-1', 'alice', { password: 'x' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when password is missing', async () => {
      await expect(
        client.createUser('tenant-1', 'alice', { email: 'a@b.com' }),
      ).rejects.toThrow(ValidationError);
    });
  });

  describe('getUser', () => {
    it('should get a user by ID', async () => {
      mockTransport.get.mockResolvedValueOnce({ id: 'user-1', username: 'alice' });

      const result = await client.getUser('tenant-1', 'user-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/users/user-1');
      expect(result.username).toBe('alice');
    });

    it('should URL-encode user ID', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getUser('tenant-1', 'user/1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/users/user%2F1');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.getUser('', 'user-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when userId is empty', async () => {
      await expect(client.getUser('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('updateUser', () => {
    it('should update a user', async () => {
      mockTransport.put.mockResolvedValueOnce({ id: 'user-1' });

      await client.updateUser('tenant-1', 'user-1', {
        username: 'alice-updated',
        email: 'alice.updated@example.com',
        displayName: 'Alice Updated',
        password: 'newpassword',
        metadata: { updated: true },
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/users/user-1', {
        username: 'alice-updated',
        email: 'alice.updated@example.com',
        display_name: 'Alice Updated',
        password: 'newpassword',
        metadata: { updated: true },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.updateUser('', 'user-1', { email: 'x@y.com' })).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when userId is empty', async () => {
      await expect(client.updateUser('tenant-1', '', { email: 'x@y.com' })).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when updates is null', async () => {
      await expect(client.updateUser('tenant-1', 'user-1', null)).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteUser', () => {
    it('should delete a user', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      await client.deleteUser('tenant-1', 'user-1');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/users/user-1');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.deleteUser('', 'user-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when userId is empty', async () => {
      await expect(client.deleteUser('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('listUsers', () => {
    it('should list users in a tenant', async () => {
      mockTransport.get.mockResolvedValueOnce({ users: [{ id: 'user-1' }] });

      const result = await client.listUsers('tenant-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/users', {
        query: {},
      });
      expect(result.users).toHaveLength(1);
    });

    it('should list users with filters', async () => {
      mockTransport.get.mockResolvedValueOnce({ users: [] });

      await client.listUsers('tenant-1', {
        pageSize: 20,
        pageToken: 'token',
        filter: 'email=*@example.com',
        roleId: 'admin',
        groupId: 'developers',
        status: UserStatus.ACTIVE,
      });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/users', {
        query: {
          page_size: 20,
          page_token: 'token',
          filter: 'email=*@example.com',
          role_id: 'admin',
          group_id: 'developers',
          status: 'ACTIVE',
        },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.listUsers('')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Group Management Tests
  // ==========================================================================

  describe('createGroup', () => {
    it('should create a group', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'group-1', name: 'developers' });

      await client.createGroup('tenant-1', 'developers', {
        displayName: 'Developers Team',
        description: 'All developers',
        roles: ['developer-role'],
        metadata: { type: 'team' },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/groups', {
        name: 'developers',
        display_name: 'Developers Team',
        description: 'All developers',
        roles: ['developer-role'],
        metadata: { type: 'team' },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.createGroup('', 'developers')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.createGroup('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('getGroup', () => {
    it('should get a group by ID', async () => {
      mockTransport.get.mockResolvedValueOnce({ id: 'group-1', name: 'developers' });

      const result = await client.getGroup('tenant-1', 'group-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/groups/group-1');
      expect(result.name).toBe('developers');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.getGroup('', 'group-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when groupId is empty', async () => {
      await expect(client.getGroup('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('updateGroup', () => {
    it('should update a group', async () => {
      mockTransport.put.mockResolvedValueOnce({ id: 'group-1' });

      await client.updateGroup('tenant-1', 'group-1', {
        name: 'developers-updated',
        displayName: 'Updated Developers',
        description: 'Updated description',
        roles: ['new-role'],
        metadata: { updated: true },
      });

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/iam/tenants/tenant-1/groups/group-1',
        {
          name: 'developers-updated',
          display_name: 'Updated Developers',
          description: 'Updated description',
          roles: ['new-role'],
          metadata: { updated: true },
        },
      );
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.updateGroup('', 'group-1', { name: 'x' })).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when groupId is empty', async () => {
      await expect(client.updateGroup('tenant-1', '', { name: 'x' })).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when updates is null', async () => {
      await expect(client.updateGroup('tenant-1', 'group-1', null)).rejects.toThrow(
        ValidationError,
      );
    });
  });

  describe('deleteGroup', () => {
    it('should delete a group', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      await client.deleteGroup('tenant-1', 'group-1');

      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/iam/tenants/tenant-1/groups/group-1',
      );
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.deleteGroup('', 'group-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when groupId is empty', async () => {
      await expect(client.deleteGroup('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('listGroups', () => {
    it('should list groups in a tenant', async () => {
      mockTransport.get.mockResolvedValueOnce({ groups: [{ id: 'group-1' }] });

      const result = await client.listGroups('tenant-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/groups', {
        query: {},
      });
      expect(result.groups).toHaveLength(1);
    });

    it('should list groups with filters', async () => {
      mockTransport.get.mockResolvedValueOnce({ groups: [] });

      await client.listGroups('tenant-1', {
        pageSize: 20,
        pageToken: 'token',
        filter: 'name=*dev*',
        userId: 'user-1',
      });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/groups', {
        query: { page_size: 20, page_token: 'token', filter: 'name=*dev*', user_id: 'user-1' },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.listGroups('')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Role Management Tests
  // ==========================================================================

  describe('createRole', () => {
    it('should create a role', async () => {
      mockTransport.post.mockResolvedValueOnce({ id: 'role-1', name: 'admin' });

      await client.createRole('tenant-1', 'admin', ['read', 'write', 'delete'], {
        displayName: 'Administrator',
        description: 'Full access',
        metadata: { priority: 1 },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/roles', {
        name: 'admin',
        permissions: ['read', 'write', 'delete'],
        display_name: 'Administrator',
        description: 'Full access',
        metadata: { priority: 1 },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.createRole('', 'admin', ['read'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.createRole('tenant-1', '', ['read'])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when permissions is not an array', async () => {
      await expect(client.createRole('tenant-1', 'admin', 'read')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when permissions is empty', async () => {
      await expect(client.createRole('tenant-1', 'admin', [])).rejects.toThrow(ValidationError);
    });
  });

  describe('updateRole', () => {
    it('should update a role', async () => {
      mockTransport.put.mockResolvedValueOnce({ id: 'role-1' });

      await client.updateRole('tenant-1', 'role-1', {
        name: 'super-admin',
        displayName: 'Super Administrator',
        description: 'Updated description',
        permissions: ['read', 'write', 'delete', 'admin'],
        metadata: { updated: true },
      });

      expect(mockTransport.put).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/roles/role-1', {
        name: 'super-admin',
        display_name: 'Super Administrator',
        description: 'Updated description',
        permissions: ['read', 'write', 'delete', 'admin'],
        metadata: { updated: true },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.updateRole('', 'role-1', { name: 'x' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when roleId is empty', async () => {
      await expect(client.updateRole('tenant-1', '', { name: 'x' })).rejects.toThrow(
        ValidationError,
      );
    });

    it('should throw ValidationError when updates is null', async () => {
      await expect(client.updateRole('tenant-1', 'role-1', null)).rejects.toThrow(ValidationError);
    });
  });

  describe('listRoles', () => {
    it('should list roles in a tenant', async () => {
      mockTransport.get.mockResolvedValueOnce({ roles: [{ id: 'role-1' }] });

      const result = await client.listRoles('tenant-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/roles', {
        query: {},
      });
      expect(result.roles).toHaveLength(1);
    });

    it('should list roles with includeSystem', async () => {
      mockTransport.get.mockResolvedValueOnce({ roles: [] });

      await client.listRoles('tenant-1', { includeSystem: true });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/roles', {
        query: { include_system: true },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.listRoles('')).rejects.toThrow(ValidationError);
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
    it('should set a policy', async () => {
      mockTransport.post.mockResolvedValueOnce({ name: 'access-policy' });

      await client.setPolicy('tenant-1', {
        name: 'access-policy',
        rules: [{ resource: '*', action: 'read', effect: 'ALLOW' }],
        description: 'Basic access policy',
        enabled: true,
        priority: 100,
        effect: 'DENY',
        labels: { env: 'prod' },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/policies', {
        name: 'access-policy',
        rules: [{ resource: '*', action: 'read', effect: 'ALLOW' }],
        description: 'Basic access policy',
        enabled: true,
        priority: 100,
        effect: 'DENY',
        labels: { env: 'prod' },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(
        client.setPolicy('', { name: 'p', rules: [{ r: 1 }] }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy is null', async () => {
      await expect(client.setPolicy('tenant-1', null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy.name is empty', async () => {
      await expect(
        client.setPolicy('tenant-1', { name: '', rules: [{ r: 1 }] }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy.rules is not an array', async () => {
      await expect(
        client.setPolicy('tenant-1', { name: 'p', rules: 'invalid' }),
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy.rules is empty', async () => {
      await expect(
        client.setPolicy('tenant-1', { name: 'p', rules: [] }),
      ).rejects.toThrow(ValidationError);
    });
  });

  describe('getPolicy', () => {
    it('should get all policies for a tenant', async () => {
      mockTransport.get.mockResolvedValueOnce({ policies: [{ name: 'policy-1' }] });

      const result = await client.getPolicy('tenant-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/policies');
      expect(result.policies).toHaveLength(1);
    });

    it('should get a specific policy by name', async () => {
      mockTransport.get.mockResolvedValueOnce({ name: 'access-policy' });

      const result = await client.getPolicy('tenant-1', 'access-policy');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/iam/tenants/tenant-1/policies/access-policy',
      );
      expect(result.name).toBe('access-policy');
    });

    it('should URL-encode policy name', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      await client.getPolicy('tenant-1', 'policy/name');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/iam/tenants/tenant-1/policies/policy%2Fname',
      );
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.getPolicy('')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Application Management Tests
  // ==========================================================================

  describe('createApp', () => {
    it('should create an application', async () => {
      mockTransport.post.mockResolvedValueOnce({
        clientId: 'app-123',
        name: 'MyApp',
        clientSecret: 'secret-xyz',
      });

      const result = await client.createApp('tenant-1', 'MyApp', {
        description: 'My application',
        type: 'web',
        redirectUris: ['https://example.com/callback'],
        grantTypes: ['authorization_code'],
        scopes: ['openid', 'profile'],
        metadata: { version: '1.0' },
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/apps', {
        name: 'MyApp',
        description: 'My application',
        type: 'web',
        redirect_uris: ['https://example.com/callback'],
        grant_types: ['authorization_code'],
        scopes: ['openid', 'profile'],
        metadata: { version: '1.0' },
      });
      expect(result.clientSecret).toBe('secret-xyz');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.createApp('', 'MyApp')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.createApp('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('getApp', () => {
    it('should get an application by ID', async () => {
      mockTransport.get.mockResolvedValueOnce({ clientId: 'app-123', name: 'MyApp' });

      const result = await client.getApp('tenant-1', 'app-123');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/apps/app-123');
      expect(result.name).toBe('MyApp');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.getApp('', 'app-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when appId is empty', async () => {
      await expect(client.getApp('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteApp', () => {
    it('should delete an application', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      await client.deleteApp('tenant-1', 'app-123');

      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/apps/app-123');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.deleteApp('', 'app-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when appId is empty', async () => {
      await expect(client.deleteApp('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('listApps', () => {
    it('should list applications in a tenant', async () => {
      mockTransport.get.mockResolvedValueOnce({
        applications: [{ clientId: 'app-1' }],
        totalCount: 1,
      });

      const result = await client.listApps('tenant-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/apps', {
        query: {},
      });
      expect(result.applications).toHaveLength(1);
    });

    it('should list applications with pagination', async () => {
      mockTransport.get.mockResolvedValueOnce({ applications: [] });

      await client.listApps('tenant-1', { pageSize: 10, pageToken: 'abc' });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/apps', {
        query: { page_size: 10, page_token: 'abc' },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.listApps('')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Session Management Tests
  // ==========================================================================

  describe('createSession', () => {
    it('should create a session', async () => {
      mockTransport.post.mockResolvedValueOnce({
        id: 'session-1',
        userId: 'user-1',
      });

      const result = await client.createSession('tenant-1', 'user-1', {
        ipAddress: '192.168.1.1',
        userAgent: 'Mozilla/5.0',
        authMethod: 'password',
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/sessions', {
        user_id: 'user-1',
        ip_address: '192.168.1.1',
        user_agent: 'Mozilla/5.0',
        auth_method: 'password',
      });
      expect(result.id).toBe('session-1');
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.createSession('', 'user-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when userId is empty', async () => {
      await expect(client.createSession('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('listSessions', () => {
    it('should list sessions in a tenant', async () => {
      mockTransport.get.mockResolvedValueOnce({
        sessions: [{ id: 'session-1' }],
        totalCount: 1,
        hasMore: false,
      });

      const result = await client.listSessions('tenant-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/sessions', {
        query: {},
      });
      expect(result.sessions).toHaveLength(1);
    });

    it('should list sessions with filters', async () => {
      mockTransport.get.mockResolvedValueOnce({ sessions: [] });

      await client.listSessions('tenant-1', {
        userId: 'user-1',
        includeExpired: true,
        limit: 10,
        offset: 5,
      });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/tenants/tenant-1/sessions', {
        query: { user_id: 'user-1', include_expired: true, limit: 10, offset: 5 },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.listSessions('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is not positive', async () => {
      await expect(client.listSessions('tenant-1', { limit: 0 })).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Audit Log Tests
  // ==========================================================================

  describe('getAuditLog', () => {
    it('should get audit log entries', async () => {
      mockTransport.get.mockResolvedValueOnce({
        events: [{ id: 'event-1', eventType: 'authentication' }],
        totalCount: 1,
        hasMore: false,
      });

      const result = await client.getAuditLog('tenant-1');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/audit', {
        query: { tenant_id: 'tenant-1' },
      });
      expect(result.events).toHaveLength(1);
    });

    it('should get audit log with all filters', async () => {
      mockTransport.get.mockResolvedValueOnce({ events: [] });

      await client.getAuditLog('tenant-1', {
        actorId: 'user-1',
        targetId: 'resource-1',
        eventType: AuditEventType.AUTHENTICATION,
        action: 'login',
        outcome: 'success',
        startTime: '2025-01-01T00:00:00Z',
        endTime: '2025-01-31T23:59:59Z',
        ipAddress: '192.168.1.1',
        sessionId: 'session-1',
        limit: 100,
        offset: 0,
        orderBy: 'timestamp',
        orderDesc: true,
      });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/iam/audit', {
        query: {
          tenant_id: 'tenant-1',
          actor_id: 'user-1',
          target_id: 'resource-1',
          event_type: 'authentication',
          action: 'login',
          outcome: 'success',
          start_time: '2025-01-01T00:00:00Z',
          end_time: '2025-01-31T23:59:59Z',
          ip_address: '192.168.1.1',
          session_id: 'session-1',
          limit: 100,
          offset: 0,
          order_by: 'timestamp',
          order_desc: true,
        },
      });
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.getAuditLog('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is not positive', async () => {
      await expect(client.getAuditLog('tenant-1', { limit: 0 })).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // GDPR Operations Tests
  // ==========================================================================

  describe('exportUserData', () => {
    it('should export user data', async () => {
      mockTransport.post.mockResolvedValueOnce({
        requestId: 'export-1',
        status: 'pending',
        format: 'json',
      });

      const result = await client.exportUserData('tenant-1', 'user-1');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/iam/tenants/tenant-1/users/user-1/export',
        {
          format: 'json',
          include_categories: undefined,
          exclude_categories: undefined,
          notify_email: undefined,
        },
      );
      expect(result.requestId).toBe('export-1');
    });

    it('should export user data with options', async () => {
      mockTransport.post.mockResolvedValueOnce({ requestId: 'export-1' });

      await client.exportUserData('tenant-1', 'user-1', {
        format: ExportFormat.CSV,
        includeCategories: ['profile', 'activity'],
        excludeCategories: ['logs'],
        notifyEmail: 'user@example.com',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/iam/tenants/tenant-1/users/user-1/export',
        {
          format: 'csv',
          include_categories: ['profile', 'activity'],
          exclude_categories: ['logs'],
          notify_email: 'user@example.com',
        },
      );
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.exportUserData('', 'user-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when userId is empty', async () => {
      await expect(client.exportUserData('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  describe('deleteUserData', () => {
    it('should delete user data', async () => {
      mockTransport.post.mockResolvedValueOnce({
        requestId: 'delete-1',
        status: 'pending',
      });

      const result = await client.deleteUserData('tenant-1', 'user-1');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/iam/tenants/tenant-1/users/user-1/delete-data',
        {
          reason: undefined,
          confirm_email: undefined,
          retain_categories: undefined,
          scheduled_for: undefined,
          notify_email: undefined,
        },
      );
      expect(result.requestId).toBe('delete-1');
    });

    it('should delete user data with options', async () => {
      mockTransport.post.mockResolvedValueOnce({ requestId: 'delete-1' });

      await client.deleteUserData('tenant-1', 'user-1', {
        reason: 'User requested deletion',
        confirmEmail: 'user@example.com',
        retainCategories: ['legal'],
        scheduledFor: '2025-02-01T00:00:00Z',
        notifyEmail: 'admin@example.com',
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/iam/tenants/tenant-1/users/user-1/delete-data',
        {
          reason: 'User requested deletion',
          confirm_email: 'user@example.com',
          retain_categories: ['legal'],
          scheduled_for: '2025-02-01T00:00:00Z',
          notify_email: 'admin@example.com',
        },
      );
    });

    it('should throw ValidationError when tenantId is empty', async () => {
      await expect(client.deleteUserData('', 'user-1')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when userId is empty', async () => {
      await expect(client.deleteUserData('tenant-1', '')).rejects.toThrow(ValidationError);
    });
  });

  // ==========================================================================
  // Enum Exports Tests
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
