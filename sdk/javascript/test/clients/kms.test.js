/**
 * KMSClient Tests
 *
 * Comprehensive tests for the Key Management Service client.
 */

import { KMSClient, KeyAlgorithm, KeyStatus, KeyUsage } from '../../src/clients/kms.js';
import { ValidationError } from '../../src/errors/index.js';

describe('KMSClient', () => {
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
    client = new KMSClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const c = new KMSClient(mockTransport);
      expect(c).toBeInstanceOf(KMSClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new KMSClient()).toThrow(ValidationError);
      expect(() => new KMSClient()).toThrow('transport is required');
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new KMSClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport has no request method', () => {
      expect(() => new KMSClient({})).toThrow(ValidationError);
      expect(() => new KMSClient({})).toThrow('transport must have a request method');
    });
  });

  describe('generateKey', () => {
    it('should generate a new key with required parameters', async () => {
      mockTransport.post.mockResolvedValueOnce({
        key_id: 'key-123',
        version: 1,
        algorithm: 'AES-256-GCM',
        created_at: 1234567890,
      });

      const result = await client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
      });

      expect(result.keyId).toBe('key-123');
      expect(result.version).toBe(1);
      expect(result.algorithm).toBe('AES-256-GCM');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/kms/keys',
        { name: 'my-key', algorithm: 'AES-256-GCM' }
      );
    });

    it('should generate a key with all optional parameters', async () => {
      mockTransport.post.mockResolvedValueOnce({
        key_id: 'key-456',
        version: 1,
        algorithm: 'RSA-4096',
        created_at: 1234567890,
      });

      const result = await client.generateKey({
        name: 'signing-key',
        algorithm: KeyAlgorithm.RSA_4096,
        usage: [KeyUsage.SIGN, KeyUsage.VERIFY],
        description: 'A signing key',
        metadata: { team: 'security' },
        rotatable: true,
        rotationPeriodDays: 90,
      });

      expect(result.keyId).toBe('key-456');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/kms/keys',
        {
          name: 'signing-key',
          algorithm: 'RSA-4096',
          usage: ['SIGN', 'VERIFY'],
          description: 'A signing key',
          metadata: { team: 'security' },
          rotatable: true,
          rotation_period_days: 90,
        }
      );
    });

    it('should throw ValidationError when request is missing', async () => {
      await expect(client.generateKey()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is missing', async () => {
      await expect(client.generateKey({
        algorithm: KeyAlgorithm.AES_256_GCM,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is empty', async () => {
      await expect(client.generateKey({
        name: '',
        algorithm: KeyAlgorithm.AES_256_GCM,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when algorithm is missing', async () => {
      await expect(client.generateKey({
        name: 'my-key',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when algorithm is invalid', async () => {
      await expect(client.generateKey({
        name: 'my-key',
        algorithm: 'INVALID-ALGO',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when usage is not an array', async () => {
      await expect(client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        usage: 'ENCRYPT',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when usage contains invalid values', async () => {
      await expect(client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        usage: ['INVALID'],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when description is not a string', async () => {
      await expect(client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        description: 123,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      await expect(client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        metadata: 'invalid',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is an array', async () => {
      await expect(client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        metadata: [],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when rotatable is not a boolean', async () => {
      await expect(client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        rotatable: 'yes',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when rotationPeriodDays is not a positive integer', async () => {
      await expect(client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        rotationPeriodDays: -1,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.generateKey({
        name: 'my-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
      })).rejects.toThrow(ValidationError);
    });
  });

  describe('importKey', () => {
    it('should import a key with required parameters', async () => {
      mockTransport.post.mockResolvedValueOnce({
        key_id: 'imported-key-123',
        version: 1,
        algorithm: 'AES-256-GCM',
        created_at: 1234567890,
      });

      const result = await client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64encodedkey==',
      });

      expect(result.keyId).toBe('imported-key-123');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/kms/keys/import',
        {
          name: 'imported-key',
          algorithm: 'AES-256-GCM',
          key_material: 'base64encodedkey==',
        }
      );
    });

    it('should import a key with optional parameters', async () => {
      mockTransport.post.mockResolvedValueOnce({
        key_id: 'imported-key-456',
        version: 1,
        algorithm: 'AES-256-GCM',
        created_at: 1234567890,
      });

      const result = await client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64encodedkey==',
        usage: [KeyUsage.ENCRYPT, KeyUsage.DECRYPT],
        description: 'Imported from HSM',
        metadata: { source: 'hsm' },
      });

      expect(result.keyId).toBe('imported-key-456');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/kms/keys/import',
        {
          name: 'imported-key',
          algorithm: 'AES-256-GCM',
          key_material: 'base64encodedkey==',
          usage: ['ENCRYPT', 'DECRYPT'],
          description: 'Imported from HSM',
          metadata: { source: 'hsm' },
        }
      );
    });

    it('should throw ValidationError when keyMaterial is missing', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keyMaterial is empty', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: '',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is missing', async () => {
      await expect(client.importKey({
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64key==',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when algorithm is invalid', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: 'INVALID',
        keyMaterial: 'base64key==',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64key==',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when description is not a string', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64key==',
        description: 123,
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64key==',
        metadata: 'not-an-object',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is an array', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64key==',
        metadata: ['item1', 'item2'],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when usage is not an array', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64key==',
        usage: 'ENCRYPT',
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when usage array contains non-string element', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64key==',
        usage: [KeyUsage.ENCRYPT, 123, KeyUsage.DECRYPT],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when usage contains invalid values', async () => {
      await expect(client.importKey({
        name: 'imported-key',
        algorithm: KeyAlgorithm.AES_256_GCM,
        keyMaterial: 'base64key==',
        usage: ['INVALID_USAGE'],
      })).rejects.toThrow(ValidationError);
    });
  });

  describe('getKey', () => {
    it('should get key information', async () => {
      mockTransport.get.mockResolvedValueOnce({
        key_id: 'key-123',
        name: 'my-key',
        algorithm: 'AES-256-GCM',
        version: 2,
        status: 'ACTIVE',
        usage: ['ENCRYPT', 'DECRYPT'],
        created_at: 1234567890,
        rotated_at: 1234567900,
        description: 'Test key',
      });

      const result = await client.getKey('key-123');

      expect(result.keyId).toBe('key-123');
      expect(result.name).toBe('my-key');
      expect(result.algorithm).toBe('AES-256-GCM');
      expect(result.version).toBe(2);
      expect(result.status).toBe('ACTIVE');
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys/key-123');
    });

    it('should URL encode keyId with special characters', async () => {
      mockTransport.get.mockResolvedValueOnce({
        key_id: 'key/with/slashes',
        name: 'test',
        algorithm: 'AES-256-GCM',
        version: 1,
        status: 'ACTIVE',
        usage: [],
        created_at: 0,
        rotated_at: 0,
      });

      await client.getKey('key/with/slashes');

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys/key%2Fwith%2Fslashes');
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.getKey('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keyId is missing', async () => {
      await expect(client.getKey()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      await expect(client.getKey('key-123')).rejects.toThrow(ValidationError);
    });
  });

  describe('listKeys', () => {
    it('should list keys with no options', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: [
          { key_id: 'key-1', name: 'key1', algorithm: 'AES-256-GCM', version: 1, status: 'ACTIVE', usage: [], created_at: 0, rotated_at: 0 },
          { key_id: 'key-2', name: 'key2', algorithm: 'RSA-4096', version: 2, status: 'ACTIVE', usage: [], created_at: 0, rotated_at: 0 },
        ],
        total_count: 2,
      });

      const result = await client.listKeys();

      expect(result.keys).toHaveLength(2);
      expect(result.keys[0].keyId).toBe('key-1');
      expect(result.totalCount).toBe(2);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys', { query: {} });
    });

    it('should list keys with pagination options', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: [{ key_id: 'key-3', name: 'key3', algorithm: 'AES-256-GCM', version: 1, status: 'ACTIVE', usage: [], created_at: 0, rotated_at: 0 }],
        total_count: 10,
      });

      const result = await client.listKeys({ limit: 5, offset: 5 });

      expect(result.keys).toHaveLength(1);
      expect(result.totalCount).toBe(10);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys', { query: { limit: 5, offset: 5 } });
    });

    it('should list keys with status filter', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: [],
        total_count: 0,
      });

      await client.listKeys({ status: KeyStatus.DISABLED });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys', { query: { status: 'DISABLED' } });
    });

    it('should list keys with algorithm filter', async () => {
      mockTransport.get.mockResolvedValueOnce({
        keys: [],
        total_count: 0,
      });

      await client.listKeys({ algorithm: KeyAlgorithm.ED25519 });

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys', { query: { algorithm: 'ED25519' } });
    });

    it('should throw ValidationError when limit is not a positive integer', async () => {
      await expect(client.listKeys({ limit: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when limit is zero', async () => {
      await expect(client.listKeys({ limit: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when offset is negative', async () => {
      await expect(client.listKeys({ offset: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when status is invalid', async () => {
      await expect(client.listKeys({ status: 'INVALID' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when algorithm is invalid', async () => {
      await expect(client.listKeys({ algorithm: 'INVALID' })).rejects.toThrow(ValidationError);
    });

    it('should handle empty response gracefully', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.listKeys();

      expect(result.keys).toEqual([]);
      expect(result.totalCount).toBe(0);
    });
  });

  describe('getKeyMetadata', () => {
    it('should get key metadata', async () => {
      mockTransport.get.mockResolvedValueOnce({
        key_id: 'key-123',
        name: 'my-key',
        algorithm: 'AES-256-GCM',
        version: 1,
        status: 'ACTIVE',
        usage: ['ENCRYPT'],
        description: 'Test',
        custom_metadata: { env: 'prod' },
        rotatable: true,
        rotation_period_days: 30,
        created_at: 1234567890,
        updated_at: 1234567900,
        rotated_at: 1234567890,
      });

      const result = await client.getKeyMetadata('key-123');

      expect(result.keyId).toBe('key-123');
      expect(result.customMetadata).toEqual({ env: 'prod' });
      expect(result.rotatable).toBe(true);
      expect(result.rotationPeriodDays).toBe(30);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys/key-123/metadata');
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.getKeyMetadata('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      await expect(client.getKeyMetadata('key-123')).rejects.toThrow(ValidationError);
    });
  });

  describe('listKeyVersions', () => {
    it('should list key versions', async () => {
      mockTransport.get.mockResolvedValueOnce({
        key_id: 'key-123',
        versions: [
          { version: 1, status: 'DESTROYED', created_at: 1234567890, destroyed_at: 1234567900 },
          { version: 2, status: 'ACTIVE', created_at: 1234567900 },
        ],
        total_count: 2,
      });

      const result = await client.listKeyVersions('key-123');

      expect(result.keyId).toBe('key-123');
      expect(result.versions).toHaveLength(2);
      expect(result.versions[0].version).toBe(1);
      expect(result.versions[0].destroyedAt).toBe(1234567900);
      expect(result.totalCount).toBe(2);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys/key-123/versions');
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.listKeyVersions('')).rejects.toThrow(ValidationError);
    });

    it('should use keyId from parameter when not in response', async () => {
      mockTransport.get.mockResolvedValueOnce({
        versions: [],
        total_count: 0,
      });

      const result = await client.listKeyVersions('key-456');

      expect(result.keyId).toBe('key-456');
    });
  });

  describe('rotateKey', () => {
    it('should rotate a key', async () => {
      mockTransport.post.mockResolvedValueOnce({
        key_id: 'key-123',
        new_version: 3,
        rotated_at: 1234567890,
      });

      const result = await client.rotateKey('key-123');

      expect(result.keyId).toBe('key-123');
      expect(result.newVersion).toBe(3);
      expect(result.rotatedAt).toBe(1234567890);
      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/kms/keys/key-123/rotate', {});
    });

    it('should rotate a key with destroyPrevious option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        key_id: 'key-123',
        new_version: 4,
        rotated_at: 1234567890,
      });

      const result = await client.rotateKey('key-123', { destroyPrevious: true });

      expect(result.newVersion).toBe(4);
      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/kms/keys/key-123/rotate', { destroy_previous: true });
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.rotateKey('')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when destroyPrevious is not a boolean', async () => {
      await expect(client.rotateKey('key-123', { destroyPrevious: 'yes' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.rotateKey('key-123')).rejects.toThrow(ValidationError);
    });
  });

  describe('revokeKey', () => {
    it('should revoke a key', async () => {
      mockTransport.post.mockResolvedValueOnce({
        success: true,
        message: 'Key revoked successfully',
      });

      const result = await client.revokeKey('key-123');

      expect(result.success).toBe(true);
      expect(result.message).toBe('Key revoked successfully');
      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/kms/keys/key-123/revoke', {});
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.revokeKey('')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.revokeKey('key-123');

      expect(result.success).toBe(true);
      expect(result.message).toBe('');
    });
  });

  describe('deleteKey', () => {
    it('should delete a key', async () => {
      mockTransport.delete.mockResolvedValueOnce({
        success: true,
        message: 'Key scheduled for deletion',
      });

      const result = await client.deleteKey('key-123');

      expect(result.success).toBe(true);
      expect(result.message).toBe('Key scheduled for deletion');
      expect(mockTransport.delete).toHaveBeenCalledWith('/api/v1/kms/keys/key-123');
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.deleteKey('')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.delete.mockResolvedValueOnce({});

      const result = await client.deleteKey('key-123');

      expect(result.success).toBe(true);
      expect(result.message).toBe('');
    });
  });

  describe('updateKeyMetadata', () => {
    it('should update key metadata', async () => {
      mockTransport.patch.mockResolvedValueOnce({
        key_id: 'key-123',
        name: 'updated-name',
        algorithm: 'AES-256-GCM',
        version: 1,
        status: 'ACTIVE',
        usage: [],
        description: 'Updated description',
        custom_metadata: { env: 'staging' },
        rotatable: false,
        created_at: 0,
        updated_at: 1234567890,
        rotated_at: 0,
      });

      const result = await client.updateKeyMetadata('key-123', {
        name: 'updated-name',
        description: 'Updated description',
        customMetadata: { env: 'staging' },
      });

      expect(result.name).toBe('updated-name');
      expect(result.description).toBe('Updated description');
      expect(mockTransport.patch).toHaveBeenCalledWith(
        '/api/v1/kms/keys/key-123/metadata',
        {
          name: 'updated-name',
          description: 'Updated description',
          custom_metadata: { env: 'staging' },
        }
      );
    });

    it('should update key with rotatable and rotationPeriodDays', async () => {
      mockTransport.patch.mockResolvedValueOnce({
        key_id: 'key-123',
        name: 'test',
        algorithm: 'AES-256-GCM',
        version: 1,
        status: 'ACTIVE',
        usage: [],
        rotatable: true,
        rotation_period_days: 60,
        created_at: 0,
        updated_at: 0,
        rotated_at: 0,
      });

      await client.updateKeyMetadata('key-123', {
        rotatable: true,
        rotationPeriodDays: 60,
      });

      expect(mockTransport.patch).toHaveBeenCalledWith(
        '/api/v1/kms/keys/key-123/metadata',
        {
          rotatable: true,
          rotation_period_days: 60,
        }
      );
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.updateKeyMetadata('', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is missing', async () => {
      await expect(client.updateKeyMetadata('key-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when metadata is not an object', async () => {
      await expect(client.updateKeyMetadata('key-123', 'invalid')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is empty string', async () => {
      await expect(client.updateKeyMetadata('key-123', { name: '' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when name is whitespace only', async () => {
      await expect(client.updateKeyMetadata('key-123', { name: '   ' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when description is not a string', async () => {
      await expect(client.updateKeyMetadata('key-123', { description: 123 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when customMetadata is not an object', async () => {
      await expect(client.updateKeyMetadata('key-123', { customMetadata: 'invalid' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when customMetadata is an array', async () => {
      await expect(client.updateKeyMetadata('key-123', { customMetadata: [] })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when rotatable is not a boolean', async () => {
      await expect(client.updateKeyMetadata('key-123', { rotatable: 'yes' })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when rotationPeriodDays is not positive', async () => {
      await expect(client.updateKeyMetadata('key-123', { rotationPeriodDays: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when server returns null response', async () => {
      mockTransport.patch.mockResolvedValueOnce(null);

      await expect(client.updateKeyMetadata('key-123', { name: 'test' })).rejects.toThrow(ValidationError);
    });
  });

  describe('updateKeyStatus', () => {
    it('should update key status to DISABLED', async () => {
      mockTransport.put.mockResolvedValueOnce({
        success: true,
        previous_status: 'ACTIVE',
        new_status: 'DISABLED',
      });

      const result = await client.updateKeyStatus('key-123', KeyStatus.DISABLED);

      expect(result.success).toBe(true);
      expect(result.previousStatus).toBe('ACTIVE');
      expect(result.newStatus).toBe('DISABLED');
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kms/keys/key-123/status',
        { status: 'DISABLED' }
      );
    });

    it('should update key status to ACTIVE', async () => {
      mockTransport.put.mockResolvedValueOnce({
        success: true,
        previous_status: 'DISABLED',
        new_status: 'ACTIVE',
      });

      const result = await client.updateKeyStatus('key-123', KeyStatus.ACTIVE);

      expect(result.newStatus).toBe('ACTIVE');
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.updateKeyStatus('', KeyStatus.DISABLED)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when status is empty', async () => {
      await expect(client.updateKeyStatus('key-123', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when status is invalid', async () => {
      await expect(client.updateKeyStatus('key-123', 'INVALID')).rejects.toThrow(ValidationError);
    });

    it('should handle response with missing fields', async () => {
      mockTransport.put.mockResolvedValueOnce({});

      const result = await client.updateKeyStatus('key-123', KeyStatus.DISABLED);

      expect(result.success).toBe(true);
      expect(result.previousStatus).toBe('');
      expect(result.newStatus).toBe('DISABLED');
    });
  });

  describe('setKeyPolicy', () => {
    it('should set key policy with valid statements', async () => {
      mockTransport.put.mockResolvedValueOnce({
        key_id: 'key-123',
        statements: [
          { sid: 'Allow', effect: 'ALLOW', principals: ['user:admin'], actions: ['kms:*'] },
        ],
        version: 1,
        updated_at: 1234567890,
      });

      const result = await client.setKeyPolicy('key-123', {
        statements: [
          {
            sid: 'Allow',
            effect: 'ALLOW',
            principals: ['user:admin'],
            actions: ['kms:*'],
          },
        ],
      });

      expect(result.keyId).toBe('key-123');
      expect(result.statements).toHaveLength(1);
      expect(result.statements[0].sid).toBe('Allow');
      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kms/keys/key-123/policy',
        {
          statements: [
            {
              sid: 'Allow',
              effect: 'ALLOW',
              principals: ['user:admin'],
              actions: ['kms:*'],
              conditions: undefined,
            },
          ],
        }
      );
    });

    it('should set key policy with conditions', async () => {
      mockTransport.put.mockResolvedValueOnce({
        key_id: 'key-123',
        statements: [],
        version: 1,
        updated_at: 0,
      });

      await client.setKeyPolicy('key-123', {
        statements: [
          {
            sid: 'Conditional',
            effect: 'DENY',
            principals: ['user:*'],
            actions: ['kms:delete'],
            conditions: { ip: { eq: '192.168.1.1' } },
          },
        ],
      });

      expect(mockTransport.put).toHaveBeenCalledWith(
        '/api/v1/kms/keys/key-123/policy',
        expect.objectContaining({
          statements: expect.arrayContaining([
            expect.objectContaining({
              conditions: { ip: { eq: '192.168.1.1' } },
            }),
          ]),
        })
      );
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.setKeyPolicy('', { statements: [] })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when policy is missing', async () => {
      await expect(client.setKeyPolicy('key-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when statements is missing', async () => {
      await expect(client.setKeyPolicy('key-123', {})).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when statements is not an array', async () => {
      await expect(client.setKeyPolicy('key-123', { statements: {} })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when statement is not an object', async () => {
      await expect(client.setKeyPolicy('key-123', { statements: ['invalid'] })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when statement.sid is missing', async () => {
      await expect(client.setKeyPolicy('key-123', {
        statements: [{ effect: 'ALLOW', principals: ['user:*'], actions: ['kms:*'] }],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when statement.effect is invalid', async () => {
      await expect(client.setKeyPolicy('key-123', {
        statements: [{ sid: 'Test', effect: 'INVALID', principals: ['user:*'], actions: ['kms:*'] }],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when statement.principals is not an array', async () => {
      await expect(client.setKeyPolicy('key-123', {
        statements: [{ sid: 'Test', effect: 'ALLOW', principals: 'user:*', actions: ['kms:*'] }],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when statement.actions is not an array', async () => {
      await expect(client.setKeyPolicy('key-123', {
        statements: [{ sid: 'Test', effect: 'ALLOW', principals: ['user:*'], actions: 'kms:*' }],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when principal is empty', async () => {
      await expect(client.setKeyPolicy('key-123', {
        statements: [{ sid: 'Test', effect: 'ALLOW', principals: [''], actions: ['kms:*'] }],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when action is empty', async () => {
      await expect(client.setKeyPolicy('key-123', {
        statements: [{ sid: 'Test', effect: 'ALLOW', principals: ['user:*'], actions: [''] }],
      })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when conditions is not an object', async () => {
      await expect(client.setKeyPolicy('key-123', {
        statements: [{ sid: 'Test', effect: 'ALLOW', principals: ['user:*'], actions: ['kms:*'], conditions: 'invalid' }],
      })).rejects.toThrow(ValidationError);
    });
  });

  describe('getKeyPolicy', () => {
    it('should get key policy', async () => {
      mockTransport.get.mockResolvedValueOnce({
        key_id: 'key-123',
        statements: [
          { sid: 'Default', effect: 'ALLOW', principals: ['user:admin'], actions: ['kms:*'] },
        ],
        version: 2,
        updated_at: 1234567890,
      });

      const result = await client.getKeyPolicy('key-123');

      expect(result.keyId).toBe('key-123');
      expect(result.statements).toHaveLength(1);
      expect(result.version).toBe(2);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/keys/key-123/policy');
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.getKeyPolicy('')).rejects.toThrow(ValidationError);
    });

    it('should handle empty statements', async () => {
      mockTransport.get.mockResolvedValueOnce({
        key_id: 'key-123',
        version: 1,
        updated_at: 0,
      });

      const result = await client.getKeyPolicy('key-123');

      expect(result.statements).toEqual([]);
    });
  });

  describe('getSupportedAlgorithms', () => {
    it('should get supported algorithms', async () => {
      mockTransport.get.mockResolvedValueOnce({
        algorithms: [
          {
            algorithm: 'AES-256-GCM',
            type: 'symmetric',
            supported_usage: ['ENCRYPT', 'DECRYPT'],
            key_size: 256,
            description: 'AES-256 in GCM mode',
          },
          {
            algorithm: 'RSA-4096',
            type: 'asymmetric',
            supportedUsage: ['SIGN', 'VERIFY'],
            keySize: 4096,
            description: 'RSA with 4096-bit key',
          },
        ],
      });

      const result = await client.getSupportedAlgorithms();

      expect(result.algorithms).toHaveLength(2);
      expect(result.algorithms[0].algorithm).toBe('AES-256-GCM');
      expect(result.algorithms[0].type).toBe('symmetric');
      expect(result.algorithms[0].supportedUsage).toEqual(['ENCRYPT', 'DECRYPT']);
      expect(result.algorithms[0].keySize).toBe(256);
      expect(result.algorithms[1].keySize).toBe(4096);
      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/kms/algorithms');
    });

    it('should handle empty algorithms list', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.getSupportedAlgorithms();

      expect(result.algorithms).toEqual([]);
    });
  });

  describe('enum exports', () => {
    it('should export KeyAlgorithm enum with all values', () => {
      expect(KeyAlgorithm.AES_256_GCM).toBe('AES-256-GCM');
      expect(KeyAlgorithm.CHACHA20_POLY1305).toBe('CHACHA20-POLY1305');
      expect(KeyAlgorithm.RSA_2048).toBe('RSA-2048');
      expect(KeyAlgorithm.RSA_4096).toBe('RSA-4096');
      expect(KeyAlgorithm.ECDSA_P256).toBe('ECDSA-P256');
      expect(KeyAlgorithm.ECDSA_P384).toBe('ECDSA-P384');
      expect(KeyAlgorithm.ED25519).toBe('ED25519');
    });

    it('should export KeyStatus enum with all values', () => {
      expect(KeyStatus.ACTIVE).toBe('ACTIVE');
      expect(KeyStatus.DISABLED).toBe('DISABLED');
      expect(KeyStatus.PENDING_DELETION).toBe('PENDING_DELETION');
      expect(KeyStatus.DESTROYED).toBe('DESTROYED');
    });

    it('should export KeyUsage enum with all values', () => {
      expect(KeyUsage.ENCRYPT).toBe('ENCRYPT');
      expect(KeyUsage.DECRYPT).toBe('DECRYPT');
      expect(KeyUsage.SIGN).toBe('SIGN');
      expect(KeyUsage.VERIFY).toBe('VERIFY');
      expect(KeyUsage.WRAP).toBe('WRAP');
      expect(KeyUsage.UNWRAP).toBe('UNWRAP');
    });

    it('should have frozen enums', () => {
      expect(Object.isFrozen(KeyAlgorithm)).toBe(true);
      expect(Object.isFrozen(KeyStatus)).toBe(true);
      expect(Object.isFrozen(KeyUsage)).toBe(true);
    });
  });
});
