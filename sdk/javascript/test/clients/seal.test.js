/**
 * SealClient Tests
 *
 * Comprehensive tests for the Seal client covering all public methods,
 * success scenarios, error handling, and edge cases for seal/unseal operations
 * and Shamir secret sharing functionality.
 */

import { SealClient } from '../../src/clients/seal.js';
import { ValidationError } from '../../src/errors/index.js';

describe('SealClient', () => {
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
    client = new SealClient(mockTransport);
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      expect(client).toBeInstanceOf(SealClient);
    });

    it('should throw ValidationError when transport is not provided', () => {
      expect(() => new SealClient()).toThrow(ValidationError);
      expect(() => new SealClient(null)).toThrow(ValidationError);
      expect(() => new SealClient(undefined)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport lacks request method', () => {
      const invalidTransport = { get: jest.fn() };
      expect(() => new SealClient(invalidTransport)).toThrow(ValidationError);
      expect(() => new SealClient(invalidTransport)).toThrow('transport must have a request method');
    });
  });

  describe('status', () => {
    const mockStatusResponse = {
      sealed: true,
      initialized: true,
      progress: 1,
      required: 3,
      cluster_name: 'quicbaas-cluster',
      cluster_version: '1.0.0',
    };

    it('should return seal status successfully', async () => {
      mockTransport.get.mockResolvedValueOnce(mockStatusResponse);

      const result = await client.status();

      expect(mockTransport.get).toHaveBeenCalledWith('/api/v1/seal/status');
      expect(result.sealed).toBe(true);
      expect(result.initialized).toBe(true);
      expect(result.progress).toBe(1);
      expect(result.required).toBe(3);
      expect(result.clusterName).toBe('quicbaas-cluster');
      expect(result.clusterVersion).toBe('1.0.0');
    });

    it('should handle unsealed status', async () => {
      const unsealedResponse = {
        sealed: false,
        initialized: true,
        progress: 0,
        required: 3,
      };
      mockTransport.get.mockResolvedValueOnce(unsealedResponse);

      const result = await client.status();

      expect(result.sealed).toBe(false);
    });

    it('should handle uninitialized status', async () => {
      const uninitializedResponse = {
        sealed: true,
        initialized: false,
        progress: 0,
        required: 0,
      };
      mockTransport.get.mockResolvedValueOnce(uninitializedResponse);

      const result = await client.status();

      expect(result.initialized).toBe(false);
      expect(result.required).toBe(0);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.get.mockResolvedValueOnce(null);

      const result = await client.status();

      expect(result.sealed).toBe(true);
      expect(result.initialized).toBe(false);
      expect(result.progress).toBe(0);
      expect(result.required).toBe(0);
      expect(result.clusterName).toBeNull();
      expect(result.clusterVersion).toBeNull();
    });

    it('should handle camelCase response', async () => {
      const camelCaseResponse = {
        sealed: false,
        initialized: true,
        progress: 2,
        required: 3,
        clusterName: 'camel-cluster',
        clusterVersion: '2.0.0',
      };
      mockTransport.get.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.status();

      expect(result.clusterName).toBe('camel-cluster');
      expect(result.clusterVersion).toBe('2.0.0');
    });

    it('should handle empty response object', async () => {
      mockTransport.get.mockResolvedValueOnce({});

      const result = await client.status();

      expect(result.sealed).toBe(true);
      expect(result.initialized).toBe(false);
    });
  });

  describe('initialize', () => {
    const mockInitResponse = {
      keys: ['key1-base64', 'key2-base64', 'key3-base64', 'key4-base64', 'key5-base64'],
      keys_base64: ['key1-b64', 'key2-b64', 'key3-b64', 'key4-b64', 'key5-b64'],
      root_token: 'hvs.root-token-123',
    };

    it('should initialize with basic parameters', async () => {
      mockTransport.post.mockResolvedValueOnce(mockInitResponse);

      const result = await client.initialize({
        secretShares: 5,
        secretThreshold: 3,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/seal/init', {
        secret_shares: 5,
        secret_threshold: 3,
      });
      expect(result.keys).toHaveLength(5);
      expect(result.rootToken).toBe('hvs.root-token-123');
    });

    it('should initialize with recovery keys', async () => {
      const responseWithRecovery = {
        ...mockInitResponse,
        recovery_keys: ['recovery-key1', 'recovery-key2', 'recovery-key3'],
        recovery_keys_base64: ['r-key1-b64', 'r-key2-b64', 'r-key3-b64'],
      };
      mockTransport.post.mockResolvedValueOnce(responseWithRecovery);

      const result = await client.initialize({
        secretShares: 5,
        secretThreshold: 3,
        recoveryShares: 3,
        recoveryThreshold: 2,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/seal/init', {
        secret_shares: 5,
        secret_threshold: 3,
        recovery_shares: 3,
        recovery_threshold: 2,
      });
      expect(result.recoveryKeys).toHaveLength(3);
      expect(result.recoveryKeysBase64).toHaveLength(3);
    });

    it('should throw ValidationError for missing request', async () => {
      await expect(client.initialize()).rejects.toThrow(ValidationError);
      await expect(client.initialize(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for missing secretShares', async () => {
      await expect(client.initialize({ secretThreshold: 3 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for missing secretThreshold', async () => {
      await expect(client.initialize({ secretShares: 5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-positive secretShares', async () => {
      await expect(client.initialize({ secretShares: 0, secretThreshold: 3 })).rejects.toThrow(ValidationError);
      await expect(client.initialize({ secretShares: -1, secretThreshold: 3 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-positive secretThreshold', async () => {
      await expect(client.initialize({ secretShares: 5, secretThreshold: 0 })).rejects.toThrow(ValidationError);
      await expect(client.initialize({ secretShares: 5, secretThreshold: -1 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-integer secretShares', async () => {
      await expect(client.initialize({ secretShares: 5.5, secretThreshold: 3 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-integer secretThreshold', async () => {
      await expect(client.initialize({ secretShares: 5, secretThreshold: 3.5 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when secretThreshold exceeds secretShares', async () => {
      await expect(client.initialize({ secretShares: 3, secretThreshold: 5 })).rejects.toThrow(ValidationError);
      await expect(client.initialize({ secretShares: 3, secretThreshold: 5 })).rejects.toThrow(
        'secretThreshold cannot exceed secretShares'
      );
    });

    it('should throw ValidationError for non-positive recoveryShares', async () => {
      await expect(
        client.initialize({
          secretShares: 5,
          secretThreshold: 3,
          recoveryShares: 0,
          recoveryThreshold: 2,
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-positive recoveryThreshold', async () => {
      await expect(
        client.initialize({
          secretShares: 5,
          secretThreshold: 3,
          recoveryShares: 3,
          recoveryThreshold: 0,
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when recoveryThreshold exceeds recoveryShares', async () => {
      await expect(
        client.initialize({
          secretShares: 5,
          secretThreshold: 3,
          recoveryShares: 2,
          recoveryThreshold: 3,
        })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.initialize({
          secretShares: 5,
          secretThreshold: 3,
          recoveryShares: 2,
          recoveryThreshold: 3,
        })
      ).rejects.toThrow('recoveryThreshold cannot exceed recoveryShares');
    });

    it('should throw ValidationError when recoveryThreshold is set without recoveryShares', async () => {
      await expect(
        client.initialize({
          secretShares: 5,
          secretThreshold: 3,
          recoveryThreshold: 2,
        })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.initialize({
          secretShares: 5,
          secretThreshold: 3,
          recoveryThreshold: 2,
        })
      ).rejects.toThrow('recoveryShares is required when recoveryThreshold is specified');
    });

    it('should throw ValidationError for null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.initialize({ secretShares: 5, secretThreshold: 3 })).rejects.toThrow(ValidationError);
    });

    it('should handle camelCase response', async () => {
      const camelCaseResponse = {
        keys: ['key1', 'key2'],
        keysBase64: ['k1-b64', 'k2-b64'],
        rootToken: 'root-token-camel',
        recoveryKeys: ['r1', 'r2'],
        recoveryKeysBase64: ['r1-b64', 'r2-b64'],
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.initialize({ secretShares: 2, secretThreshold: 2 });

      expect(result.keysBase64).toEqual(['k1-b64', 'k2-b64']);
      expect(result.rootToken).toBe('root-token-camel');
      expect(result.recoveryKeys).toEqual(['r1', 'r2']);
    });

    it('should handle response without optional fields', async () => {
      const minimalResponse = {
        keys: ['key1'],
        root_token: 'token',
      };
      mockTransport.post.mockResolvedValueOnce(minimalResponse);

      const result = await client.initialize({ secretShares: 1, secretThreshold: 1 });

      expect(result.keys).toEqual(['key1']);
      expect(result.keysBase64).toBeNull();
      expect(result.recoveryKeys).toBeUndefined();
      expect(result.recoveryKeysBase64).toBeUndefined();
    });
  });

  describe('unseal', () => {
    const mockUnsealResponse = {
      sealed: true,
      progress: 1,
      required: 3,
    };

    it('should provide unseal key share successfully', async () => {
      mockTransport.post.mockResolvedValueOnce(mockUnsealResponse);

      const result = await client.unseal('base64-key-share');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/seal/unseal', {
        key: 'base64-key-share',
      });
      expect(result.sealed).toBe(true);
      expect(result.progress).toBe(1);
      expect(result.required).toBe(3);
    });

    it('should return unsealed state when threshold met', async () => {
      const unsealedResponse = {
        sealed: false,
        progress: 3,
        required: 3,
      };
      mockTransport.post.mockResolvedValueOnce(unsealedResponse);

      const result = await client.unseal('final-key-share');

      expect(result.sealed).toBe(false);
      expect(result.progress).toBe(3);
    });

    it('should throw ValidationError for empty key share', async () => {
      await expect(client.unseal('')).rejects.toThrow(ValidationError);
      await expect(client.unseal('  ')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for null key share', async () => {
      await expect(client.unseal(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string key share', async () => {
      await expect(client.unseal(123)).rejects.toThrow(ValidationError);
      await expect(client.unseal([])).rejects.toThrow(ValidationError);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.unseal('key');

      expect(result.sealed).toBe(true);
      expect(result.progress).toBe(0);
      expect(result.required).toBe(0);
    });

    it('should handle empty response object', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.unseal('key');

      expect(result.sealed).toBe(true);
      expect(result.progress).toBe(0);
    });
  });

  describe('seal', () => {
    it('should seal the database successfully', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      await client.seal();

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/seal/seal', {});
    });

    it('should handle server returning data on seal', async () => {
      mockTransport.post.mockResolvedValueOnce({ sealed: true });

      await expect(client.seal()).resolves.toBeUndefined();
    });

    it('should propagate server errors', async () => {
      const error = new Error('Not authorized');
      mockTransport.post.mockRejectedValueOnce(error);

      await expect(client.seal()).rejects.toThrow('Not authorized');
    });
  });

  describe('rekey', () => {
    const mockRekeyResponse = {
      complete: true,
      keys: ['new-key1', 'new-key2', 'new-key3'],
      keys_base64: ['nk1-b64', 'nk2-b64', 'nk3-b64'],
      progress: 3,
      required: 3,
    };

    it('should rekey with new shares configuration', async () => {
      mockTransport.post.mockResolvedValueOnce(mockRekeyResponse);

      const result = await client.rekey({
        secretShares: 3,
        secretThreshold: 2,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/seal/rekey', {
        secret_shares: 3,
        secret_threshold: 2,
      });
      expect(result.complete).toBe(true);
      expect(result.keys).toHaveLength(3);
    });

    it('should rekey with existing keys provided', async () => {
      mockTransport.post.mockResolvedValueOnce(mockRekeyResponse);

      await client.rekey({
        secretShares: 3,
        secretThreshold: 2,
        existingKeys: ['old-key1', 'old-key2'],
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/seal/rekey', {
        secret_shares: 3,
        secret_threshold: 2,
        existing_keys: ['old-key1', 'old-key2'],
      });
    });

    it('should handle incomplete rekey progress', async () => {
      const incompleteResponse = {
        complete: false,
        progress: 1,
        required: 3,
      };
      mockTransport.post.mockResolvedValueOnce(incompleteResponse);

      const result = await client.rekey({
        secretShares: 5,
        secretThreshold: 3,
      });

      expect(result.complete).toBe(false);
      expect(result.keys).toBeUndefined();
      expect(result.progress).toBe(1);
    });

    it('should throw ValidationError for missing request', async () => {
      await expect(client.rekey()).rejects.toThrow(ValidationError);
      await expect(client.rekey(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for missing secretShares', async () => {
      await expect(client.rekey({ secretThreshold: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for missing secretThreshold', async () => {
      await expect(client.rekey({ secretShares: 3 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-positive secretShares', async () => {
      await expect(client.rekey({ secretShares: 0, secretThreshold: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-positive secretThreshold', async () => {
      await expect(client.rekey({ secretShares: 3, secretThreshold: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when secretThreshold exceeds secretShares', async () => {
      await expect(client.rekey({ secretShares: 2, secretThreshold: 5 })).rejects.toThrow(ValidationError);
      await expect(client.rekey({ secretShares: 2, secretThreshold: 5 })).rejects.toThrow(
        'secretThreshold cannot exceed secretShares'
      );
    });

    it('should throw ValidationError when existingKeys is not an array', async () => {
      await expect(
        client.rekey({
          secretShares: 3,
          secretThreshold: 2,
          existingKeys: 'not-array',
        })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.rekey({
          secretShares: 3,
          secretThreshold: 2,
          existingKeys: {},
        })
      ).rejects.toThrow('existingKeys must be an array');
    });

    it('should throw ValidationError when existingKeys contains empty strings', async () => {
      await expect(
        client.rekey({
          secretShares: 3,
          secretThreshold: 2,
          existingKeys: ['key1', ''],
        })
      ).rejects.toThrow(ValidationError);
      await expect(
        client.rekey({
          secretShares: 3,
          secretThreshold: 2,
          existingKeys: ['key1', '  '],
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when existingKeys contains non-strings', async () => {
      await expect(
        client.rekey({
          secretShares: 3,
          secretThreshold: 2,
          existingKeys: ['key1', 123],
        })
      ).rejects.toThrow(ValidationError);
    });

    it('should handle null response with defaults', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.rekey({ secretShares: 3, secretThreshold: 2 });

      expect(result.complete).toBe(false);
      expect(result.progress).toBe(0);
      expect(result.required).toBe(0);
    });

    it('should handle camelCase response', async () => {
      const camelCaseResponse = {
        complete: true,
        keys: ['k1', 'k2'],
        keysBase64: ['k1-b64', 'k2-b64'],
        progress: 2,
        required: 2,
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.rekey({ secretShares: 2, secretThreshold: 2 });

      expect(result.keysBase64).toEqual(['k1-b64', 'k2-b64']);
    });
  });

  describe('generateRecoveryKey', () => {
    const mockRecoveryResponse = {
      recovery_keys: ['recovery-key1', 'recovery-key2', 'recovery-key3'],
      recovery_keys_base64: ['rk1-b64', 'rk2-b64', 'rk3-b64'],
    };

    it('should generate recovery keys successfully', async () => {
      mockTransport.post.mockResolvedValueOnce(mockRecoveryResponse);

      const result = await client.generateRecoveryKey({
        recoveryShares: 3,
        recoveryThreshold: 2,
      });

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/seal/recovery/generate', {
        recovery_shares: 3,
        recovery_threshold: 2,
      });
      expect(result.recoveryKeys).toHaveLength(3);
      expect(result.recoveryKeysBase64).toHaveLength(3);
    });

    it('should throw ValidationError for missing request', async () => {
      await expect(client.generateRecoveryKey()).rejects.toThrow(ValidationError);
      await expect(client.generateRecoveryKey(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for missing recoveryShares', async () => {
      await expect(client.generateRecoveryKey({ recoveryThreshold: 2 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for missing recoveryThreshold', async () => {
      await expect(client.generateRecoveryKey({ recoveryShares: 3 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-positive recoveryShares', async () => {
      await expect(client.generateRecoveryKey({ recoveryShares: 0, recoveryThreshold: 2 })).rejects.toThrow(
        ValidationError
      );
      await expect(client.generateRecoveryKey({ recoveryShares: -1, recoveryThreshold: 2 })).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError for non-positive recoveryThreshold', async () => {
      await expect(client.generateRecoveryKey({ recoveryShares: 3, recoveryThreshold: 0 })).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError for non-integer recoveryShares', async () => {
      await expect(client.generateRecoveryKey({ recoveryShares: 3.5, recoveryThreshold: 2 })).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError for non-integer recoveryThreshold', async () => {
      await expect(client.generateRecoveryKey({ recoveryShares: 3, recoveryThreshold: 2.5 })).rejects.toThrow(
        ValidationError
      );
    });

    it('should throw ValidationError when recoveryThreshold exceeds recoveryShares', async () => {
      await expect(client.generateRecoveryKey({ recoveryShares: 2, recoveryThreshold: 3 })).rejects.toThrow(
        ValidationError
      );
      await expect(client.generateRecoveryKey({ recoveryShares: 2, recoveryThreshold: 3 })).rejects.toThrow(
        'recoveryThreshold cannot exceed recoveryShares'
      );
    });

    it('should throw ValidationError for null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      await expect(client.generateRecoveryKey({ recoveryShares: 3, recoveryThreshold: 2 })).rejects.toThrow(
        ValidationError
      );
    });

    it('should handle camelCase response', async () => {
      const camelCaseResponse = {
        recoveryKeys: ['r1', 'r2'],
        recoveryKeysBase64: ['r1-b64', 'r2-b64'],
      };
      mockTransport.post.mockResolvedValueOnce(camelCaseResponse);

      const result = await client.generateRecoveryKey({ recoveryShares: 2, recoveryThreshold: 2 });

      expect(result.recoveryKeys).toEqual(['r1', 'r2']);
      expect(result.recoveryKeysBase64).toEqual(['r1-b64', 'r2-b64']);
    });

    it('should handle response without base64 keys', async () => {
      const responseWithoutBase64 = {
        recovery_keys: ['key1', 'key2'],
      };
      mockTransport.post.mockResolvedValueOnce(responseWithoutBase64);

      const result = await client.generateRecoveryKey({ recoveryShares: 2, recoveryThreshold: 2 });

      expect(result.recoveryKeys).toEqual(['key1', 'key2']);
      expect(result.recoveryKeysBase64).toBeUndefined();
    });
  });

  describe('validateRecoveryKey', () => {
    it('should validate recovery key successfully', async () => {
      mockTransport.post.mockResolvedValueOnce({ valid: true });

      const result = await client.validateRecoveryKey('valid-recovery-key');

      expect(mockTransport.post).toHaveBeenCalledWith('/api/v1/seal/recovery/validate', {
        recovery_key: 'valid-recovery-key',
      });
      expect(result.valid).toBe(true);
    });

    it('should return invalid for invalid recovery key', async () => {
      mockTransport.post.mockResolvedValueOnce({ valid: false });

      const result = await client.validateRecoveryKey('invalid-key');

      expect(result.valid).toBe(false);
    });

    it('should throw ValidationError for empty key', async () => {
      await expect(client.validateRecoveryKey('')).rejects.toThrow(ValidationError);
      await expect(client.validateRecoveryKey('  ')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for null key', async () => {
      await expect(client.validateRecoveryKey(null)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError for non-string key', async () => {
      await expect(client.validateRecoveryKey(123)).rejects.toThrow(ValidationError);
      await expect(client.validateRecoveryKey([])).rejects.toThrow(ValidationError);
    });

    it('should default valid to false for null response', async () => {
      mockTransport.post.mockResolvedValueOnce(null);

      const result = await client.validateRecoveryKey('key');

      expect(result.valid).toBe(false);
    });

    it('should default valid to false for empty response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.validateRecoveryKey('key');

      expect(result.valid).toBe(false);
    });
  });

  describe('response transformation edge cases', () => {
    it('should handle initialize response with empty keys array', async () => {
      const responseWithEmptyKeys = {
        keys: [],
        root_token: 'token',
      };
      mockTransport.post.mockResolvedValueOnce(responseWithEmptyKeys);

      const result = await client.initialize({ secretShares: 1, secretThreshold: 1 });

      expect(result.keys).toEqual([]);
    });

    it('should handle rekey response with keys only when complete', async () => {
      // First test: incomplete - no keys
      const incompleteResponse = {
        complete: false,
        progress: 1,
        required: 3,
      };
      mockTransport.post.mockResolvedValueOnce(incompleteResponse);

      let result = await client.rekey({ secretShares: 5, secretThreshold: 3 });
      expect(result.keys).toBeUndefined();
      expect(result.keysBase64).toBeUndefined();

      // Second test: complete - has keys
      const completeResponse = {
        complete: true,
        keys: ['k1', 'k2'],
        keys_base64: ['k1-b64', 'k2-b64'],
        progress: 3,
        required: 3,
      };
      mockTransport.post.mockResolvedValueOnce(completeResponse);

      result = await client.rekey({ secretShares: 2, secretThreshold: 2 });
      expect(result.keys).toEqual(['k1', 'k2']);
      expect(result.keysBase64).toEqual(['k1-b64', 'k2-b64']);
    });

    it('should handle status with both snake_case and camelCase (snake_case takes precedence)', async () => {
      const mixedResponse = {
        cluster_name: 'snake-cluster',
        clusterName: 'camel-cluster',
        cluster_version: '1.0',
        clusterVersion: '2.0',
        sealed: true,
        initialized: true,
        progress: 0,
        required: 3,
      };
      mockTransport.get.mockResolvedValueOnce(mixedResponse);

      const result = await client.status();

      // snake_case should take precedence via nullish coalescing
      expect(result.clusterName).toBe('snake-cluster');
      expect(result.clusterVersion).toBe('1.0');
    });

    it('should handle initialize with recovery_keys and recoveryKeys mixed', async () => {
      const mixedResponse = {
        keys: ['k1'],
        root_token: 'token',
        recovery_keys: ['snake-recovery'],
        recoveryKeys: ['camel-recovery'],
        recovery_keys_base64: ['snake-b64'],
        recoveryKeysBase64: ['camel-b64'],
      };
      mockTransport.post.mockResolvedValueOnce(mixedResponse);

      const result = await client.initialize({ secretShares: 1, secretThreshold: 1 });

      // snake_case should take precedence
      expect(result.recoveryKeys).toEqual(['snake-recovery']);
      expect(result.recoveryKeysBase64).toEqual(['snake-b64']);
    });

    it('should handle generateRecoveryKey with empty recovery_keys array', async () => {
      const emptyResponse = {
        recovery_keys: [],
      };
      mockTransport.post.mockResolvedValueOnce(emptyResponse);

      const result = await client.generateRecoveryKey({ recoveryShares: 1, recoveryThreshold: 1 });

      expect(result.recoveryKeys).toEqual([]);
    });
  });

  describe('threshold validation edge cases', () => {
    it('should allow threshold equal to shares in initialize', async () => {
      mockTransport.post.mockResolvedValueOnce({ keys: ['k'], root_token: 't' });

      await expect(client.initialize({ secretShares: 3, secretThreshold: 3 })).resolves.toBeDefined();
    });

    it('should allow threshold equal to shares in rekey', async () => {
      mockTransport.post.mockResolvedValueOnce({ complete: true, keys: ['k'] });

      await expect(client.rekey({ secretShares: 5, secretThreshold: 5 })).resolves.toBeDefined();
    });

    it('should allow threshold equal to shares in generateRecoveryKey', async () => {
      mockTransport.post.mockResolvedValueOnce({ recovery_keys: ['k'] });

      await expect(
        client.generateRecoveryKey({ recoveryShares: 2, recoveryThreshold: 2 })
      ).resolves.toBeDefined();
    });

    it('should allow single share and single threshold', async () => {
      mockTransport.post.mockResolvedValueOnce({ keys: ['k'], root_token: 't' });

      await expect(client.initialize({ secretShares: 1, secretThreshold: 1 })).resolves.toBeDefined();
    });

    it('should allow recoveryShares without recoveryThreshold in initialize', async () => {
      mockTransport.post.mockResolvedValueOnce({ keys: ['k'], root_token: 't' });

      await expect(
        client.initialize({
          secretShares: 5,
          secretThreshold: 3,
          recoveryShares: 3,
        })
      ).resolves.toBeDefined();
    });
  });
});
