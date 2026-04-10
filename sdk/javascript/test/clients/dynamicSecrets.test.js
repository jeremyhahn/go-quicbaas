/**
 * DynamicSecretsClient Tests
 *
 * Comprehensive tests for the Dynamic Secrets management client.
 */

import DynamicSecretsClient, { CredentialType } from '../../src/clients/dynamicSecrets.js';
import { ValidationError } from '../../src/errors/index.js';

describe('DynamicSecretsClient', () => {
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
    };
    client = new DynamicSecretsClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const c = new DynamicSecretsClient(mockTransport);
      expect(c).toBeInstanceOf(DynamicSecretsClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new DynamicSecretsClient()).toThrow(ValidationError);
      expect(() => new DynamicSecretsClient()).toThrow('transport is required');
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new DynamicSecretsClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport has no request method', () => {
      expect(() => new DynamicSecretsClient({})).toThrow(ValidationError);
      expect(() => new DynamicSecretsClient({})).toThrow('transport must have a request method');
    });
  });

  describe('createRole', () => {
    it('should create a database role', async () => {
      mockTransport.post.mockResolvedValueOnce({
        role: {
          name: 'postgres-readonly',
          generator_type: 'database',
          config: { connection_string: 'postgres://localhost/db' },
          default_ttl: 3600000,
          max_ttl: 86400000,
        },
        created_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.createRole('postgres-readonly', {
        generatorType: CredentialType.DATABASE,
        config: { connection_string: 'postgres://localhost/db' },
        defaultTtl: 3600000,
        maxTtl: 86400000,
      });

      expect(result.role.name).toBe('postgres-readonly');
      expect(result.role.generatorType).toBe('database');
      expect(result.createdAt).toBe('2024-01-01T00:00:00Z');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/roles',
        {
          name: 'postgres-readonly',
          generator_type: 'database',
          config: { connection_string: 'postgres://localhost/db' },
          default_ttl: 3600000,
          max_ttl: 86400000,
        }
      );
    });

    it('should create an AWS role', async () => {
      mockTransport.post.mockResolvedValueOnce({
        role: {
          name: 'aws-admin',
          generator_type: 'aws',
          config: { role_arn: 'arn:aws:iam::123456789:role/admin' },
          default_ttl: 3600000,
          max_ttl: 43200000,
        },
        created_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.createRole('aws-admin', {
        generatorType: CredentialType.AWS,
        config: { role_arn: 'arn:aws:iam::123456789:role/admin' },
        defaultTtl: 3600000,
        maxTtl: 43200000,
      });

      expect(result.role.generatorType).toBe('aws');
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.createRole('', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is missing', async () => {
      await expect(client.createRole()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when config is missing', async () => {
      await expect(client.createRole('role')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when config is not an object', async () => {
      await expect(client.createRole('role', 'invalid')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when config is an array', async () => {
      await expect(client.createRole('role', [])).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when generatorType is missing', async () => {
      await expect(client.createRole('role', {
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when generatorType is invalid', async () => {
      await expect(client.createRole('role', {
        generatorType: 'invalid',
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when config.config is missing', async () => {
      await expect(client.createRole('role', {
        generatorType: CredentialType.DATABASE,
        defaultTtl: 1000,
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when config.config is not an object', async () => {
      await expect(client.createRole('role', {
        generatorType: CredentialType.DATABASE,
        config: 'invalid',
        defaultTtl: 1000,
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when defaultTtl is missing', async () => {
      await expect(client.createRole('role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when defaultTtl is not positive', async () => {
      await expect(client.createRole('role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 0,
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when maxTtl is missing', async () => {
      await expect(client.createRole('role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when maxTtl is not positive', async () => {
      await expect(client.createRole('role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 0,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when defaultTtl exceeds maxTtl', async () => {
      await expect(client.createRole('role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 5000,
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.createRole('role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      })).rejects.toThrow(ValidationError);
    });

    it('should handle null role in response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        role: null,
        created_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.createRole('test-role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      });

      expect(result.role).toBeNull();
      expect(result.createdAt).toBe('2024-01-01T00:00:00Z');
    });

    it('should handle undefined role in response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        created_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.createRole('test-role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      });

      expect(result.role).toBeNull();
      expect(result.createdAt).toBe('2024-01-01T00:00:00Z');
    });

    it('should handle camelCase createdAt in response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        role: {
          name: 'test-role',
          generator_type: 'database',
          config: {},
          default_ttl: 1000,
          max_ttl: 2000,
        },
        createdAt: '2024-02-15T12:00:00Z',
      });

      const result = await client.createRole('test-role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      });

      expect(result.createdAt).toBe('2024-02-15T12:00:00Z');
    });

    it('should handle role with camelCase field names', async () => {
      mockTransport.post.mockResolvedValueOnce({
        role: {
          name: 'camel-role',
          generatorType: 'aws',
          config: { region: 'us-east-1' },
          defaultTtl: 7200000,
          maxTtl: 14400000,
        },
        created_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.createRole('camel-role', {
        generatorType: CredentialType.AWS,
        config: { region: 'us-east-1' },
        defaultTtl: 7200000,
        maxTtl: 14400000,
      });

      expect(result.role.name).toBe('camel-role');
      expect(result.role.generatorType).toBe('aws');
      expect(result.role.defaultTtl).toBe(7200000);
      expect(result.role.maxTtl).toBe(14400000);
    });

    it('should handle role with missing optional fields defaulting to empty values', async () => {
      mockTransport.post.mockResolvedValueOnce({
        role: {
          name: 'minimal-role',
        },
        created_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.createRole('minimal-role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      });

      expect(result.role.name).toBe('minimal-role');
      expect(result.role.generatorType).toBe('');
      expect(result.role.config).toEqual({});
      expect(result.role.defaultTtl).toBe(0);
      expect(result.role.maxTtl).toBe(0);
    });

    it('should fallback to current date when createdAt is missing in response', async () => {
      const beforeTest = new Date();
      mockTransport.post.mockResolvedValueOnce({
        role: {
          name: 'test-role',
          generator_type: 'database',
          config: {},
          default_ttl: 1000,
          max_ttl: 2000,
        },
      });

      const result = await client.createRole('test-role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      });
      const afterTest = new Date();

      expect(result.role.name).toBe('test-role');
      const parsedDate = new Date(result.createdAt);
      expect(parsedDate.getTime()).toBeGreaterThanOrEqual(beforeTest.getTime());
      expect(parsedDate.getTime()).toBeLessThanOrEqual(afterTest.getTime());
    });

    it('should handle role with empty string name', async () => {
      mockTransport.post.mockResolvedValueOnce({
        role: {
          name: '',
          generator_type: 'database',
          config: {},
          default_ttl: 1000,
          max_ttl: 2000,
        },
        created_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.createRole('test-role', {
        generatorType: CredentialType.DATABASE,
        config: {},
        defaultTtl: 1000,
        maxTtl: 2000,
      });

      expect(result.role.name).toBe('');
    });
  });

  describe('getRole', () => {
    it('should get a role by name', async () => {
      mockTransport.get.mockResolvedValueOnce({
        role: {
          name: 'postgres-readonly',
          generator_type: 'database',
          config: { connection_string: 'postgres://localhost/db' },
          default_ttl: 3600000,
          max_ttl: 86400000,
        },
      });

      const result = await client.getRole('postgres-readonly');

      expect(result.role.name).toBe('postgres-readonly');
      expect(result.role.generatorType).toBe('database');
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/roles/postgres-readonly'
      );
    });

    it('should handle role object at root level', async () => {
      mockTransport.get.mockResolvedValueOnce({
        name: 'aws-role',
        generator_type: 'aws',
        config: {},
        default_ttl: 1000,
        max_ttl: 2000,
      });

      const result = await client.getRole('aws-role');

      expect(result.role.name).toBe('aws-role');
    });

    it('should URL encode role name with special characters', async () => {
      mockTransport.get.mockResolvedValueOnce({
        role: { name: 'role/with/slashes', generator_type: 'database', config: {}, default_ttl: 0, max_ttl: 0 },
      });

      await client.getRole('role/with/slashes');

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/roles/role%2Fwith%2Fslashes'
      );
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.getRole('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is missing', async () => {
      await expect(client.getRole()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      await expect(client.getRole('role')).rejects.toThrow(ValidationError);
    });
  });

  describe('listRoles', () => {
    it('should list all roles', async () => {
      mockTransport.get.mockResolvedValueOnce({
        roles: [
          { name: 'role-1', generator_type: 'database', config: {}, default_ttl: 1000, max_ttl: 2000 },
          { name: 'role-2', generator_type: 'aws', config: {}, default_ttl: 3000, max_ttl: 4000 },
        ],
        count: 2,
      });

      const result = await client.listRoles();

      expect(result.roles).toHaveLength(2);
      expect(result.roles[0].name).toBe('role-1');
      expect(result.roles[1].generatorType).toBe('aws');
      expect(result.count).toBe(2);
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/roles',
        { query: {} }
      );
    });

    it('should list roles with prefix filter', async () => {
      mockTransport.get.mockResolvedValueOnce({
        roles: [{ name: 'postgres-readonly', generator_type: 'database', config: {}, default_ttl: 0, max_ttl: 0 }],
        count: 1,
      });

      await client.listRoles({ prefix: 'postgres-' });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/roles',
        { query: { prefix: 'postgres-' } }
      );
    });

    it('should list roles with pagination', async () => {
      mockTransport.get.mockResolvedValueOnce({
        roles: [],
        count: 10,
      });

      await client.listRoles({ limit: 5, offset: 5 });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/roles',
        { query: { limit: 5, offset: 5 } }
      );
    });

    it('should throw ValidationError when prefix is not a string', async () => {
      await expect(client.listRoles({ prefix: 123 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is not positive', async () => {
      await expect(client.listRoles({ limit: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is negative', async () => {
      await expect(client.listRoles({ limit: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when offset is negative', async () => {
      await expect(client.listRoles({ offset: -1 })).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listRoles();

      expect(result.roles).toEqual([]);
      expect(result.count).toBe(0);
    });
  });

  describe('deleteRole', () => {
    it('should delete a role', async () => {
      mockTransport.delete.mockResolvedValueOnce({
        deleted_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.deleteRole('old-role');

      expect(result.deletedAt).toBe('2024-01-01T00:00:00Z');
      expect(mockTransport.delete).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/roles/old-role'
      );
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.deleteRole('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is missing', async () => {
      await expect(client.deleteRole()).rejects.toThrow(ValidationError);
    });

    it('should handle missing deletedAt in response', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      const result = await client.deleteRole('role');

      expect(result.deletedAt).toBeDefined();
    });
  });

  describe('generateCredential', () => {
    it('should generate a credential with default TTL', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-123',
          type: 'database',
          data: { username: 'user_abc', password: 'secret123' },
          lease_id: 'lease-123',
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease: {
          lease_id: 'lease-123',
          secret_path: 'postgres-readonly',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        lease_id: 'lease-123',
      });

      const result = await client.generateCredential('postgres-readonly');

      expect(result.credential.credentialId).toBe('cred-123');
      expect(result.credential.data.username).toBe('user_abc');
      expect(result.lease.leaseId).toBe('lease-123');
      expect(result.lease.renewable).toBe(true);
      expect(result.leaseId).toBe('lease-123');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/credentials',
        { role_name: 'postgres-readonly' }
      );
    });

    it('should generate a credential with custom TTL', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-456',
          type: 'database',
          data: {},
          lease_id: 'lease-456',
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T02:00:00Z',
        },
        lease: {
          lease_id: 'lease-456',
          secret_path: 'role',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T02:00:00Z',
          ttl: 7200000,
          renew_count: 0,
          max_renewals: 3,
          renewable: true,
        },
        lease_id: 'lease-456',
      });

      await client.generateCredential('role', { ttl: 7200000 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/credentials',
        { role_name: 'role', ttl: 7200000 }
      );
    });

    it('should throw ValidationError when roleName is empty', async () => {
      await expect(client.generateCredential('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when roleName is missing', async () => {
      await expect(client.generateCredential()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ttl is negative', async () => {
      await expect(client.generateCredential('role', { ttl: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ttl is not a number', async () => {
      await expect(client.generateCredential('role', { ttl: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.generateCredential('role')).rejects.toThrow(ValidationError);
    });

    it('should handle null credential in response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        lease: {
          lease_id: 'lease-789',
        },
        lease_id: 'lease-789',
      });

      const result = await client.generateCredential('role');

      expect(result.credential).toBeNull();
    });

    it('should handle null lease in response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-123',
          type: 'database',
          data: { username: 'user_abc', password: 'secret123' },
          lease_id: 'lease-123',
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease: null,
        lease_id: 'lease-123',
      });

      const result = await client.generateCredential('role');

      expect(result.credential).not.toBeNull();
      expect(result.credential.credentialId).toBe('cred-123');
      expect(result.lease).toBeNull();
      expect(result.leaseId).toBe('lease-123');
    });

    it('should handle undefined lease in response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-456',
          type: 'aws',
          data: { access_key: 'AKIAIOSFODNN7EXAMPLE' },
          lease_id: 'lease-456',
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease_id: 'lease-456',
      });

      const result = await client.generateCredential('role');

      expect(result.credential).not.toBeNull();
      expect(result.lease).toBeNull();
      expect(result.leaseId).toBe('lease-456');
    });

    it('should handle credential with camelCase field names', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credentialId: 'cred-camel',
          type: 'database',
          data: { username: 'user_camel' },
          leaseId: 'lease-camel',
          createdAt: '2024-03-01T00:00:00Z',
          expiresAt: '2024-03-01T01:00:00Z',
        },
        lease: {
          lease_id: 'lease-camel',
          secret_path: 'role',
          issued_at: '2024-03-01T00:00:00Z',
          expires_at: '2024-03-01T01:00:00Z',
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        lease_id: 'lease-camel',
      });

      const result = await client.generateCredential('role');

      expect(result.credential.credentialId).toBe('cred-camel');
      expect(result.credential.leaseId).toBe('lease-camel');
      expect(result.credential.createdAt).toBe('2024-03-01T00:00:00Z');
      expect(result.credential.expiresAt).toBe('2024-03-01T01:00:00Z');
    });

    it('should handle credential with missing optional fields', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-minimal',
        },
        lease: {
          lease_id: 'lease-minimal',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease_id: 'lease-minimal',
      });

      const result = await client.generateCredential('role');

      expect(result.credential.credentialId).toBe('cred-minimal');
      expect(result.credential.type).toBe('');
      expect(result.credential.data).toEqual({});
      expect(result.credential.leaseId).toBe('');
    });

    it('should handle leaseId from camelCase response field', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-123',
          type: 'database',
          data: {},
          lease_id: 'lease-123',
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease: {
          lease_id: 'lease-123',
          secret_path: 'role',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        leaseId: 'lease-from-camel',
      });

      const result = await client.generateCredential('role');

      expect(result.leaseId).toBe('lease-from-camel');
    });

    it('should extract leaseId from lease object when top-level is missing', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-123',
          type: 'database',
          data: {},
          lease_id: 'lease-123',
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease: {
          lease_id: 'extracted-lease-id',
          secret_path: 'role',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
      });

      const result = await client.generateCredential('role');

      expect(result.leaseId).toBe('extracted-lease-id');
    });

    it('should fallback to empty string when all leaseId sources are missing', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-no-lease',
          type: 'database',
          data: {},
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease: {
          secret_path: 'role',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
      });

      const result = await client.generateCredential('role');

      expect(result.leaseId).toBe('');
    });

    it('should handle credential with empty string credentialId', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: '',
          type: 'database',
          data: {},
          lease_id: 'lease-123',
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease: {
          lease_id: 'lease-123',
          secret_path: 'role',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        lease_id: 'lease-123',
      });

      const result = await client.generateCredential('role');

      expect(result.credential.credentialId).toBe('');
    });
  });

  describe('renewLease', () => {
    it('should renew a lease with default increment', async () => {
      mockTransport.post.mockResolvedValueOnce({
        lease: {
          lease_id: 'lease-123',
          secret_path: 'role',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T02:00:00Z',
          ttl: 7200000,
          renew_count: 1,
          max_renewals: 5,
          renewable: true,
        },
        renewed_at: '2024-01-01T01:00:00Z',
      });

      const result = await client.renewLease('lease-123');

      expect(result.lease.leaseId).toBe('lease-123');
      expect(result.lease.renewCount).toBe(1);
      expect(result.renewedAt).toBe('2024-01-01T01:00:00Z');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/leases/lease-123/renew',
        { lease_id: 'lease-123' }
      );
    });

    it('should renew a lease with specific increment', async () => {
      mockTransport.post.mockResolvedValueOnce({
        lease: {
          lease_id: 'lease-123',
          secret_path: 'role',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:30:00Z',
          ttl: 5400000,
          renew_count: 2,
          max_renewals: 5,
          renewable: true,
        },
        renewed_at: '2024-01-01T01:00:00Z',
      });

      await client.renewLease('lease-123', { increment: 1800000 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/leases/lease-123/renew',
        { lease_id: 'lease-123', increment: 1800000 }
      );
    });

    it('should URL encode leaseId with special characters', async () => {
      mockTransport.post.mockResolvedValueOnce({
        lease: { lease_id: 'lease/with/slashes' },
        renewed_at: '2024-01-01T00:00:00Z',
      });

      await client.renewLease('lease/with/slashes');

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/leases/lease%2Fwith%2Fslashes/renew',
        expect.any(Object)
      );
    });

    it('should throw ValidationError when leaseId is empty', async () => {
      await expect(client.renewLease('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when leaseId is missing', async () => {
      await expect(client.renewLease()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when increment is negative', async () => {
      await expect(client.renewLease('lease-123', { increment: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when increment is not a number', async () => {
      await expect(client.renewLease('lease-123', { increment: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.renewLease('lease-123')).rejects.toThrow(ValidationError);
    });

    it('should handle missing renewedAt in response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        lease: { lease_id: 'lease-123' },
      });

      const result = await client.renewLease('lease-123');

      expect(result.renewedAt).toBeDefined();
    });

    it('should handle null lease in renew response', async () => {
      mockTransport.post.mockResolvedValueOnce({
        lease: null,
        renewed_at: '2024-01-01T01:00:00Z',
      });

      const result = await client.renewLease('lease-123');

      expect(result.lease).toBeNull();
      expect(result.renewedAt).toBe('2024-01-01T01:00:00Z');
    });
  });

  describe('revokeLease', () => {
    it('should revoke a lease', async () => {
      mockTransport.post.mockResolvedValueOnce({
        revoked_at: '2024-01-01T00:00:00Z',
      });

      const result = await client.revokeLease('lease-123');

      expect(result.revokedAt).toBe('2024-01-01T00:00:00Z');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/leases/lease-123/revoke',
        { lease_id: 'lease-123' }
      );
    });

    it('should throw ValidationError when leaseId is empty', async () => {
      await expect(client.revokeLease('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when leaseId is missing', async () => {
      await expect(client.revokeLease()).rejects.toThrow(ValidationError);
    });

    it('should handle missing revokedAt in response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.revokeLease('lease-123');

      expect(result.revokedAt).toBeDefined();
    });
  });

  describe('listLeases', () => {
    it('should list all leases', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [
          {
            lease_id: 'lease-1',
            secret_path: 'role-1',
            issued_at: '2024-01-01T00:00:00Z',
            expires_at: '2024-01-01T01:00:00Z',
            ttl: 3600000,
            renew_count: 0,
            max_renewals: 5,
            renewable: true,
          },
          {
            lease_id: 'lease-2',
            secret_path: 'role-2',
            issued_at: '2024-01-01T00:00:00Z',
            expires_at: '2024-01-01T02:00:00Z',
            ttl: 7200000,
            renew_count: 1,
            max_renewals: 3,
            renewable: false,
          },
        ],
        count: 2,
      });

      const result = await client.listLeases();

      expect(result.leases).toHaveLength(2);
      expect(result.leases[0].leaseId).toBe('lease-1');
      expect(result.leases[1].renewable).toBe(false);
      expect(result.count).toBe(2);
      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/leases',
        { query: {} }
      );
    });

    it('should list leases with prefix filter', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [],
        count: 0,
      });

      await client.listLeases({ prefix: 'postgres/' });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/leases',
        { query: { prefix: 'postgres/' } }
      );
    });

    it('should list leases with pagination', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [],
        count: 10,
      });

      await client.listLeases({ limit: 5, offset: 5 });

      expect(mockTransport.get).toHaveBeenCalledWith(
        '/api/v1/dynamic-secrets/leases',
        { query: { limit: 5, offset: 5 } }
      );
    });

    it('should throw ValidationError when prefix is not a string', async () => {
      await expect(client.listLeases({ prefix: 123 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is not positive', async () => {
      await expect(client.listLeases({ limit: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when offset is negative', async () => {
      await expect(client.listLeases({ offset: -1 })).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listLeases();

      expect(result.leases).toEqual([]);
      expect(result.count).toBe(0);
    });

    it('should handle lease with revokedAt', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [
          {
            lease_id: 'lease-1',
            secret_path: 'role-1',
            issued_at: '2024-01-01T00:00:00Z',
            expires_at: '2024-01-01T01:00:00Z',
            ttl: 3600000,
            renew_count: 0,
            max_renewals: 5,
            renewable: true,
            revoked_at: '2024-01-01T00:30:00Z',
          },
        ],
        count: 1,
      });

      const result = await client.listLeases();

      expect(result.leases[0].revokedAt).toBe('2024-01-01T00:30:00Z');
    });

    it('should handle lease with metadata', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [
          {
            lease_id: 'lease-1',
            secret_path: 'role-1',
            issued_at: '2024-01-01T00:00:00Z',
            expires_at: '2024-01-01T01:00:00Z',
            ttl: 3600000,
            renew_count: 0,
            max_renewals: 5,
            renewable: true,
            metadata: { client_id: 'app-123' },
          },
        ],
        count: 1,
      });

      const result = await client.listLeases();

      expect(result.leases[0].metadata).toEqual({ client_id: 'app-123' });
    });

    it('should handle lease with camelCase field names', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [
          {
            leaseId: 'lease-camel',
            secretPath: 'role-camel',
            issuedAt: '2024-03-01T00:00:00Z',
            expiresAt: '2024-03-01T01:00:00Z',
            ttl: 3600000,
            renewCount: 2,
            maxRenewals: 10,
            renewable: false,
          },
        ],
        count: 1,
      });

      const result = await client.listLeases();

      expect(result.leases[0].leaseId).toBe('lease-camel');
      expect(result.leases[0].secretPath).toBe('role-camel');
      expect(result.leases[0].renewCount).toBe(2);
      expect(result.leases[0].maxRenewals).toBe(10);
    });

    it('should handle lease with camelCase revokedAt', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [
          {
            lease_id: 'lease-revoked',
            secret_path: 'role-1',
            issued_at: '2024-01-01T00:00:00Z',
            expires_at: '2024-01-01T01:00:00Z',
            ttl: 3600000,
            renew_count: 0,
            max_renewals: 5,
            renewable: true,
            revokedAt: '2024-01-01T00:30:00Z',
          },
        ],
        count: 1,
      });

      const result = await client.listLeases();

      expect(result.leases[0].revokedAt).toBe('2024-01-01T00:30:00Z');
    });

    it('should handle lease with missing optional fields defaulting to empty values', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [
          {
            lease_id: 'lease-minimal',
            issued_at: '2024-01-01T00:00:00Z',
            expires_at: '2024-01-01T01:00:00Z',
          },
        ],
        count: 1,
      });

      const result = await client.listLeases();

      expect(result.leases[0].leaseId).toBe('lease-minimal');
      expect(result.leases[0].secretPath).toBe('');
      expect(result.leases[0].ttl).toBe(0);
      expect(result.leases[0].renewCount).toBe(0);
      expect(result.leases[0].maxRenewals).toBe(0);
      expect(result.leases[0].renewable).toBe(true);
      expect(result.leases[0].revokedAt).toBeNull();
      expect(result.leases[0].metadata).toEqual({});
    });

    it('should handle lease with empty string leaseId', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [
          {
            lease_id: '',
            secret_path: 'role-1',
            issued_at: '2024-01-01T00:00:00Z',
            expires_at: '2024-01-01T01:00:00Z',
            ttl: 3600000,
            renew_count: 0,
            max_renewals: 5,
            renewable: true,
          },
        ],
        count: 1,
      });

      const result = await client.listLeases();

      expect(result.leases[0].leaseId).toBe('');
    });

    it('should fallback to empty string for leaseId when both formats are missing', async () => {
      mockTransport.get.mockResolvedValueOnce({
        leases: [
          {
            secret_path: 'role-1',
            issued_at: '2024-01-01T00:00:00Z',
            expires_at: '2024-01-01T01:00:00Z',
            ttl: 3600000,
            renew_count: 0,
            max_renewals: 5,
            renewable: true,
          },
        ],
        count: 1,
      });

      const result = await client.listLeases();

      expect(result.leases[0].leaseId).toBe('');
    });
  });

  describe('timestamp transformation', () => {
    it('should handle string timestamps', async () => {
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-1',
          type: 'database',
          data: {},
          lease_id: 'lease-1',
          created_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
        },
        lease: {
          lease_id: 'lease-1',
          secret_path: 'role',
          issued_at: '2024-01-01T00:00:00Z',
          expires_at: '2024-01-01T01:00:00Z',
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        lease_id: 'lease-1',
      });

      const result = await client.generateCredential('role');

      expect(result.credential.createdAt).toBe('2024-01-01T00:00:00Z');
      expect(result.lease.issuedAt).toBe('2024-01-01T00:00:00Z');
    });

    it('should handle numeric timestamps (milliseconds)', async () => {
      const timestamp = 1704067200000; // 2024-01-01T00:00:00.000Z
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-1',
          type: 'database',
          data: {},
          lease_id: 'lease-1',
          created_at: timestamp,
          expires_at: timestamp + 3600000,
        },
        lease: {
          lease_id: 'lease-1',
          secret_path: 'role',
          issued_at: timestamp,
          expires_at: timestamp + 3600000,
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        lease_id: 'lease-1',
      });

      const result = await client.generateCredential('role');

      expect(result.credential.createdAt).toBeDefined();
      expect(typeof result.credential.createdAt).toBe('string');
    });

    it('should handle nanosecond timestamps (Go time.Time)', async () => {
      const nsTimestamp = 1704067200000000000; // Nanoseconds
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-1',
          type: 'database',
          data: {},
          lease_id: 'lease-1',
          created_at: nsTimestamp,
          expires_at: nsTimestamp,
        },
        lease: {
          lease_id: 'lease-1',
          secret_path: 'role',
          issued_at: nsTimestamp,
          expires_at: nsTimestamp,
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        lease_id: 'lease-1',
      });

      const result = await client.generateCredential('role');

      expect(result.credential.createdAt).toBeDefined();
      expect(typeof result.credential.createdAt).toBe('string');
    });

    it('should handle Date instance timestamps', async () => {
      const dateInstance = new Date('2024-01-01T00:00:00Z');
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-1',
          type: 'database',
          data: {},
          lease_id: 'lease-1',
          created_at: dateInstance,
          expires_at: dateInstance,
        },
        lease: {
          lease_id: 'lease-1',
          secret_path: 'role',
          issued_at: dateInstance,
          expires_at: dateInstance,
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        lease_id: 'lease-1',
      });

      const result = await client.generateCredential('role');

      expect(result.credential.createdAt).toBe('2024-01-01T00:00:00.000Z');
      expect(result.lease.issuedAt).toBe('2024-01-01T00:00:00.000Z');
    });

    it('should fallback to current date for unknown timestamp types', async () => {
      const beforeTest = new Date();
      // Use an object that is not a string, number, or Date
      const unknownType = { invalid: 'timestamp' };
      mockTransport.post.mockResolvedValueOnce({
        credential: {
          credential_id: 'cred-1',
          type: 'database',
          data: {},
          lease_id: 'lease-1',
          created_at: unknownType,
          expires_at: unknownType,
        },
        lease: {
          lease_id: 'lease-1',
          secret_path: 'role',
          issued_at: unknownType,
          expires_at: unknownType,
          ttl: 3600000,
          renew_count: 0,
          max_renewals: 5,
          renewable: true,
        },
        lease_id: 'lease-1',
      });

      const result = await client.generateCredential('role');
      const afterTest = new Date();

      // Verify the timestamp is a valid ISO string within test execution time
      expect(typeof result.credential.createdAt).toBe('string');
      const parsedDate = new Date(result.credential.createdAt);
      expect(parsedDate.getTime()).toBeGreaterThanOrEqual(beforeTest.getTime());
      expect(parsedDate.getTime()).toBeLessThanOrEqual(afterTest.getTime());
    });
  });

  describe('enum exports', () => {
    it('should export CredentialType enum with all values', () => {
      expect(CredentialType.DATABASE).toBe('database');
      expect(CredentialType.AWS).toBe('aws');
      expect(CredentialType.AZURE).toBe('azure');
      expect(CredentialType.GCP).toBe('gcp');
      expect(CredentialType.SSH).toBe('ssh');
      expect(CredentialType.PKI).toBe('pki');
      expect(CredentialType.LDAP).toBe('ldap');
    });

    it('should have frozen enum', () => {
      expect(Object.isFrozen(CredentialType)).toBe(true);
    });
  });
});
