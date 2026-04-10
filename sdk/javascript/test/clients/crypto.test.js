/**
 * CryptoClient Tests
 *
 * Comprehensive tests for the Cryptographic operations client.
 */

import { CryptoClient, HashAlgorithm, SignatureAlgorithm, KeyDerivationFunction } from '../../src/clients/crypto.js';
import { ValidationError } from '../../src/errors/index.js';

describe('CryptoClient', () => {
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
    client = new CryptoClient(mockTransport);

    // Mock btoa and atob for Node.js environment
    if (typeof btoa === 'undefined') {
      global.btoa = (str) => Buffer.from(str, 'binary').toString('base64');
    }
    if (typeof atob === 'undefined') {
      global.atob = (str) => Buffer.from(str, 'base64').toString('binary');
    }
  });

  describe('constructor', () => {
    it('should create client with valid transport', () => {
      const c = new CryptoClient(mockTransport);
      expect(c).toBeInstanceOf(CryptoClient);
    });

    it('should throw ValidationError when transport is missing', () => {
      expect(() => new CryptoClient()).toThrow(ValidationError);
      expect(() => new CryptoClient()).toThrow('transport is required');
    });

    it('should throw ValidationError when transport is null', () => {
      expect(() => new CryptoClient(null)).toThrow(ValidationError);
    });

    it('should throw ValidationError when transport has no request method', () => {
      expect(() => new CryptoClient({})).toThrow(ValidationError);
      expect(() => new CryptoClient({})).toThrow('transport must have a request method');
    });
  });

  describe('encrypt', () => {
    it('should encrypt data with string plaintext', async () => {
      mockTransport.post.mockResolvedValueOnce({
        ciphertext: 'YWJjZGVm',
        key_version: 1,
      });

      const result = await client.encrypt('key-123', 'Hello, World!');

      expect(result.ciphertext).toBe('YWJjZGVm');
      expect(result.keyVersion).toBe(1);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/encrypt',
        expect.objectContaining({
          key_id: 'key-123',
          plaintext: expect.any(String),
        })
      );
    });

    it('should encrypt data with Uint8Array plaintext', async () => {
      mockTransport.post.mockResolvedValueOnce({
        ciphertext: 'YWJjZGVm',
        key_version: 2,
      });

      const plaintext = new Uint8Array([72, 101, 108, 108, 111]);
      const result = await client.encrypt('key-123', plaintext);

      expect(result.ciphertext).toBe('YWJjZGVm');
      expect(result.keyVersion).toBe(2);
    });

    it('should encrypt data with context (AAD)', async () => {
      mockTransport.post.mockResolvedValueOnce({
        ciphertext: 'encrypted',
        key_version: 1,
      });

      await client.encrypt('key-123', 'data', { context: 'user-456' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/encrypt',
        expect.objectContaining({
          key_id: 'key-123',
          plaintext: expect.any(String),
          context: expect.any(String),
        })
      );
    });

    it('should encrypt data with Uint8Array context', async () => {
      mockTransport.post.mockResolvedValueOnce({
        ciphertext: 'encrypted',
        key_version: 1,
      });

      const context = new Uint8Array([1, 2, 3, 4]);
      await client.encrypt('key-123', 'data', { context });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/encrypt',
        expect.objectContaining({
          context: expect.any(String),
        })
      );
    });

    it('should encrypt data with specific key version', async () => {
      mockTransport.post.mockResolvedValueOnce({
        ciphertext: 'encrypted',
        key_version: 3,
      });

      await client.encrypt('key-123', 'data', { keyVersion: 3 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/encrypt',
        expect.objectContaining({
          key_version: 3,
        })
      );
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.encrypt('', 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keyId is missing', async () => {
      await expect(client.encrypt()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when plaintext is missing', async () => {
      await expect(client.encrypt('key-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when plaintext is empty string', async () => {
      await expect(client.encrypt('key-123', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when plaintext is empty Uint8Array', async () => {
      await expect(client.encrypt('key-123', new Uint8Array(0))).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when plaintext is invalid type', async () => {
      await expect(client.encrypt('key-123', 123)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when context is invalid type', async () => {
      await expect(client.encrypt('key-123', 'data', { context: 123 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keyVersion is not positive', async () => {
      await expect(client.encrypt('key-123', 'data', { keyVersion: 0 })).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keyVersion is negative', async () => {
      await expect(client.encrypt('key-123', 'data', { keyVersion: -1 })).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.encrypt('key-123', 'data');

      expect(result.ciphertext).toBe('');
      expect(result.keyVersion).toBe(0);
    });
  });

  describe('decrypt', () => {
    it('should decrypt ciphertext', async () => {
      mockTransport.post.mockResolvedValueOnce({
        plaintext: 'SGVsbG8=', // "Hello" in base64
      });

      const result = await client.decrypt('key-123', 'YWJjZGVm');

      expect(result.plaintext).toBeInstanceOf(Uint8Array);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/decrypt',
        expect.objectContaining({
          key_id: 'key-123',
          ciphertext: 'YWJjZGVm',
        })
      );
    });

    it('should decrypt with context (AAD)', async () => {
      mockTransport.post.mockResolvedValueOnce({
        plaintext: 'SGVsbG8=',
      });

      await client.decrypt('key-123', 'ciphertext', { context: 'user-456' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/decrypt',
        expect.objectContaining({
          context: expect.any(String),
        })
      );
    });

    it('should decrypt with Uint8Array context', async () => {
      mockTransport.post.mockResolvedValueOnce({
        plaintext: 'SGVsbG8=',
      });

      const context = new Uint8Array([1, 2, 3, 4]);
      await client.decrypt('key-123', 'ciphertext', { context });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/decrypt',
        expect.objectContaining({
          context: expect.any(String),
        })
      );
    });

    it('should decrypt with specific key version', async () => {
      mockTransport.post.mockResolvedValueOnce({
        plaintext: 'SGVsbG8=',
      });

      await client.decrypt('key-123', 'ciphertext', { keyVersion: 2 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/decrypt',
        expect.objectContaining({
          key_version: 2,
        })
      );
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.decrypt('', 'ciphertext')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ciphertext is empty', async () => {
      await expect(client.decrypt('key-123', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when ciphertext is missing', async () => {
      await expect(client.decrypt('key-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when context is invalid type', async () => {
      await expect(client.decrypt('key-123', 'ciphertext', { context: 123 }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keyVersion is not positive', async () => {
      await expect(client.decrypt('key-123', 'ciphertext', { keyVersion: 0 }))
        .rejects.toThrow(ValidationError);
    });

    it('should handle empty plaintext response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.decrypt('key-123', 'ciphertext');

      expect(result.plaintext).toBeInstanceOf(Uint8Array);
      expect(result.plaintext.length).toBe(0);
    });

    it('should throw ValidationError for invalid Base64 in plaintext response', async () => {
      // Save original atob
      const originalAtob = global.atob;
      // Mock atob to throw on invalid base64
      global.atob = (str) => {
        if (str === '!!!invalid-base64!!!') {
          throw new Error('Invalid character');
        }
        return Buffer.from(str, 'base64').toString('binary');
      };

      mockTransport.post.mockResolvedValueOnce({
        plaintext: '!!!invalid-base64!!!',
      });

      try {
        await expect(client.decrypt('key-123', 'ciphertext')).rejects.toThrow(ValidationError);

        mockTransport.post.mockResolvedValueOnce({
          plaintext: '!!!invalid-base64!!!',
        });
        await expect(client.decrypt('key-123', 'ciphertext')).rejects.toThrow('Invalid Base64 encoding');
      } finally {
        global.atob = originalAtob;
      }
    });
  });

  describe('sign', () => {
    it('should sign data with string', async () => {
      mockTransport.post.mockResolvedValueOnce({
        signature: 'c2lnbmF0dXJl',
        key_version: 1,
      });

      const result = await client.sign('key-123', 'Document content');

      expect(result.signature).toBe('c2lnbmF0dXJl');
      expect(result.keyVersion).toBe(1);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/sign',
        expect.objectContaining({
          key_id: 'key-123',
          data: expect.any(String),
        })
      );
    });

    it('should sign data with Uint8Array', async () => {
      mockTransport.post.mockResolvedValueOnce({
        signature: 'c2lnbmF0dXJl',
        key_version: 1,
      });

      const data = new Uint8Array([1, 2, 3, 4, 5]);
      const result = await client.sign('key-123', data);

      expect(result.signature).toBeDefined();
    });

    it('should sign with hash algorithm option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        signature: 'sig',
        key_version: 1,
      });

      await client.sign('key-123', 'data', { hashAlgorithm: HashAlgorithm.SHA512 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/sign',
        expect.objectContaining({
          hash_algorithm: 'SHA-512',
        })
      );
    });

    it('should sign with signature algorithm option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        signature: 'sig',
        key_version: 1,
      });

      await client.sign('key-123', 'data', { signatureAlgorithm: SignatureAlgorithm.ED25519 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/sign',
        expect.objectContaining({
          signature_algorithm: 'ED25519',
        })
      );
    });

    it('should sign with key version option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        signature: 'sig',
        key_version: 3,
      });

      await client.sign('key-123', 'data', { keyVersion: 3 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/sign',
        expect.objectContaining({
          key_version: 3,
        })
      );
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.sign('', 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is missing', async () => {
      await expect(client.sign('key-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is empty string', async () => {
      await expect(client.sign('key-123', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is empty Uint8Array', async () => {
      await expect(client.sign('key-123', new Uint8Array(0))).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is invalid type', async () => {
      await expect(client.sign('key-123', 123)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when hashAlgorithm is invalid', async () => {
      await expect(client.sign('key-123', 'data', { hashAlgorithm: 'MD5' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when signatureAlgorithm is invalid', async () => {
      await expect(client.sign('key-123', 'data', { signatureAlgorithm: 'INVALID' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keyVersion is not positive', async () => {
      await expect(client.sign('key-123', 'data', { keyVersion: 0 }))
        .rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.sign('key-123', 'data');

      expect(result.signature).toBe('');
      expect(result.keyVersion).toBe(0);
    });
  });

  describe('verify', () => {
    it('should verify a valid signature', async () => {
      mockTransport.post.mockResolvedValueOnce({
        valid: true,
      });

      const result = await client.verify('key-123', 'Document content', 'c2lnbmF0dXJl');

      expect(result.valid).toBe(true);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/verify',
        expect.objectContaining({
          key_id: 'key-123',
          data: expect.any(String),
          signature: 'c2lnbmF0dXJl',
        })
      );
    });

    it('should verify with Uint8Array data', async () => {
      mockTransport.post.mockResolvedValueOnce({
        valid: true,
      });

      const data = new Uint8Array([1, 2, 3, 4, 5]);
      const result = await client.verify('key-123', data, 'signature');

      expect(result.valid).toBe(true);
    });

    it('should verify with hash algorithm option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        valid: true,
      });

      await client.verify('key-123', 'data', 'sig', { hashAlgorithm: HashAlgorithm.SHA384 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/verify',
        expect.objectContaining({
          hash_algorithm: 'SHA-384',
        })
      );
    });

    it('should verify with signature algorithm option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        valid: true,
      });

      await client.verify('key-123', 'data', 'sig', { signatureAlgorithm: SignatureAlgorithm.RSA_PSS });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/verify',
        expect.objectContaining({
          signature_algorithm: 'RSA_PSS',
        })
      );
    });

    it('should verify with key version option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        valid: true,
      });

      await client.verify('key-123', 'data', 'sig', { keyVersion: 2 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/verify',
        expect.objectContaining({
          key_version: 2,
        })
      );
    });

    it('should return invalid for bad signature', async () => {
      mockTransport.post.mockResolvedValueOnce({
        valid: false,
      });

      const result = await client.verify('key-123', 'data', 'bad-signature');

      expect(result.valid).toBe(false);
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.verify('', 'data', 'sig')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is empty', async () => {
      await expect(client.verify('key-123', '', 'sig')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when signature is empty', async () => {
      await expect(client.verify('key-123', 'data', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when signature is missing', async () => {
      await expect(client.verify('key-123', 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when hashAlgorithm is invalid', async () => {
      await expect(client.verify('key-123', 'data', 'sig', { hashAlgorithm: 'INVALID' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when signatureAlgorithm is invalid', async () => {
      await expect(client.verify('key-123', 'data', 'sig', { signatureAlgorithm: 'INVALID' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when keyVersion is not positive', async () => {
      await expect(client.verify('key-123', 'data', 'sig', { keyVersion: 0 }))
        .rejects.toThrow(ValidationError);
    });

    it('should handle missing valid field in response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.verify('key-123', 'data', 'sig');

      expect(result.valid).toBe(false);
    });
  });

  describe('hash', () => {
    it('should hash data with SHA-256', async () => {
      mockTransport.post.mockResolvedValueOnce({
        hash: 'aGFzaGVkZGF0YQ==',
        algorithm: 'SHA-256',
      });

      const result = await client.hash(HashAlgorithm.SHA256, 'Hello, World!');

      expect(result.hash).toBe('aGFzaGVkZGF0YQ==');
      expect(result.algorithm).toBe('SHA-256');
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/hash',
        expect.objectContaining({
          algorithm: 'SHA-256',
          data: expect.any(String),
        })
      );
    });

    it('should hash data with SHA-384', async () => {
      mockTransport.post.mockResolvedValueOnce({
        hash: 'aGFzaA==',
        algorithm: 'SHA-384',
      });

      const result = await client.hash(HashAlgorithm.SHA384, 'data');

      expect(result.algorithm).toBe('SHA-384');
    });

    it('should hash data with SHA-512', async () => {
      mockTransport.post.mockResolvedValueOnce({
        hash: 'aGFzaA==',
        algorithm: 'SHA-512',
      });

      const result = await client.hash(HashAlgorithm.SHA512, 'data');

      expect(result.algorithm).toBe('SHA-512');
    });

    it('should hash Uint8Array data', async () => {
      mockTransport.post.mockResolvedValueOnce({
        hash: 'aGFzaA==',
        algorithm: 'SHA-256',
      });

      const data = new Uint8Array([1, 2, 3, 4, 5]);
      const result = await client.hash(HashAlgorithm.SHA256, data);

      expect(result.hash).toBeDefined();
    });

    it('should throw ValidationError when algorithm is empty', async () => {
      await expect(client.hash('', 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when algorithm is invalid', async () => {
      await expect(client.hash('MD5', 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is empty', async () => {
      await expect(client.hash(HashAlgorithm.SHA256, '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is missing', async () => {
      await expect(client.hash(HashAlgorithm.SHA256)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is invalid type', async () => {
      await expect(client.hash(HashAlgorithm.SHA256, 123)).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.hash(HashAlgorithm.SHA256, 'data');

      expect(result.hash).toBe('');
      expect(result.algorithm).toBe('SHA-256');
    });
  });

  describe('hmac', () => {
    it('should compute HMAC', async () => {
      mockTransport.post.mockResolvedValueOnce({
        hmac: 'aG1hY3Jlc3VsdA==',
        key_version: 1,
      });

      const result = await client.hmac('key-123', 'Message to authenticate');

      expect(result.hmac).toBe('aG1hY3Jlc3VsdA==');
      expect(result.keyVersion).toBe(1);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/hmac',
        expect.objectContaining({
          key_id: 'key-123',
          data: expect.any(String),
        })
      );
    });

    it('should compute HMAC with Uint8Array data', async () => {
      mockTransport.post.mockResolvedValueOnce({
        hmac: 'aG1hYw==',
        key_version: 2,
      });

      const data = new Uint8Array([1, 2, 3, 4, 5]);
      const result = await client.hmac('key-123', data);

      expect(result.hmac).toBeDefined();
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.hmac('', 'data')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is empty', async () => {
      await expect(client.hmac('key-123', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is missing', async () => {
      await expect(client.hmac('key-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when data is invalid type', async () => {
      await expect(client.hmac('key-123', 123)).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.hmac('key-123', 'data');

      expect(result.hmac).toBe('');
      expect(result.keyVersion).toBe(0);
    });
  });

  describe('deriveKey', () => {
    it('should derive a key using HKDF', async () => {
      mockTransport.post.mockResolvedValueOnce({
        derived_key: 'ZGVyaXZlZGtleQ==',
        key_version: 1,
      });

      const result = await client.deriveKey('master-key', 'user-123:encryption', {
        kdf: KeyDerivationFunction.HKDF,
      });

      expect(result.derivedKey).toBe('ZGVyaXZlZGtleQ==');
      expect(result.keyVersion).toBe(1);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/derive-key',
        expect.objectContaining({
          key_id: 'master-key',
          context: expect.any(String),
          kdf: 'HKDF',
        })
      );
    });

    it('should derive a key using PBKDF2', async () => {
      mockTransport.post.mockResolvedValueOnce({
        derived_key: 'ZGVyaXZlZA==',
        key_version: 1,
      });

      await client.deriveKey('master-key', 'context', {
        kdf: KeyDerivationFunction.PBKDF2,
        iterations: 100000,
      });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/derive-key',
        expect.objectContaining({
          kdf: 'PBKDF2',
          iterations: 100000,
        })
      );
    });

    it('should derive a key with Uint8Array context', async () => {
      mockTransport.post.mockResolvedValueOnce({
        derived_key: 'ZGVyaXZlZA==',
        key_version: 1,
      });

      const context = new Uint8Array([1, 2, 3, 4, 5]);
      const result = await client.deriveKey('master-key', context);

      expect(result.derivedKey).toBeDefined();
    });

    it('should derive a key with length option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        derived_key: 'a2V5',
        key_version: 1,
      });

      await client.deriveKey('master-key', 'context', { length: 64 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/derive-key',
        expect.objectContaining({
          length: 64,
        })
      );
    });

    it('should derive a key with hash algorithm option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        derived_key: 'a2V5',
        key_version: 1,
      });

      await client.deriveKey('master-key', 'context', { hashAlgorithm: HashAlgorithm.SHA512 });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/derive-key',
        expect.objectContaining({
          hash_algorithm: 'SHA-512',
        })
      );
    });

    it('should derive a key with salt option', async () => {
      mockTransport.post.mockResolvedValueOnce({
        derived_key: 'a2V5',
        key_version: 1,
      });

      await client.deriveKey('master-key', 'context', { salt: 'random-salt' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/derive-key',
        expect.objectContaining({
          salt: expect.any(String),
        })
      );
    });

    it('should derive a key with Uint8Array salt', async () => {
      mockTransport.post.mockResolvedValueOnce({
        derived_key: 'a2V5',
        key_version: 1,
      });

      const salt = new Uint8Array([1, 2, 3, 4]);
      await client.deriveKey('master-key', 'context', { salt });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/derive-key',
        expect.objectContaining({
          salt: expect.any(String),
        })
      );
    });

    it('should derive a key with info option (HKDF)', async () => {
      mockTransport.post.mockResolvedValueOnce({
        derived_key: 'a2V5',
        key_version: 1,
      });

      await client.deriveKey('master-key', 'context', { info: 'additional-info' });

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/derive-key',
        expect.objectContaining({
          info: expect.any(String),
        })
      );
    });

    it('should throw ValidationError when keyId is empty', async () => {
      await expect(client.deriveKey('', 'context')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when context is empty', async () => {
      await expect(client.deriveKey('key-123', '')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when context is missing', async () => {
      await expect(client.deriveKey('key-123')).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when kdf is invalid', async () => {
      await expect(client.deriveKey('key-123', 'context', { kdf: 'INVALID' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when length is not positive', async () => {
      await expect(client.deriveKey('key-123', 'context', { length: 0 }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when iterations is not positive', async () => {
      await expect(client.deriveKey('key-123', 'context', { iterations: 0 }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when hashAlgorithm is invalid', async () => {
      await expect(client.deriveKey('key-123', 'context', { hashAlgorithm: 'MD5' }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when salt is invalid type', async () => {
      await expect(client.deriveKey('key-123', 'context', { salt: 123 }))
        .rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when info is invalid type', async () => {
      await expect(client.deriveKey('key-123', 'context', { info: 123 }))
        .rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.deriveKey('key-123', 'context');

      expect(result.derivedKey).toBe('');
      expect(result.keyVersion).toBe(0);
    });
  });

  describe('generateRandom', () => {
    it('should generate random bytes', async () => {
      mockTransport.post.mockResolvedValueOnce({
        data: 'cmFuZG9tYnl0ZXM=',
      });

      const result = await client.generateRandom(32);

      expect(result.data).toBe('cmFuZG9tYnl0ZXM=');
      expect(result.bytes).toBeInstanceOf(Uint8Array);
      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/random',
        { length: 32 }
      );
    });

    it('should generate different lengths', async () => {
      mockTransport.post.mockResolvedValueOnce({
        data: 'YWI=',
      });

      await client.generateRandom(16);

      expect(mockTransport.post).toHaveBeenCalledWith(
        '/api/v1/crypto/random',
        { length: 16 }
      );
    });

    it('should throw ValidationError when length is missing', async () => {
      await expect(client.generateRandom()).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when length is zero', async () => {
      await expect(client.generateRandom(0)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when length is negative', async () => {
      await expect(client.generateRandom(-1)).rejects.toThrow(ValidationError);
    });

    it('should throw ValidationError when length is not an integer', async () => {
      await expect(client.generateRandom(1.5)).rejects.toThrow(ValidationError);
    });

    it('should handle empty response', async () => {
      mockTransport.post.mockResolvedValueOnce({});

      const result = await client.generateRandom(32);

      expect(result.data).toBe('');
      expect(result.bytes).toBeInstanceOf(Uint8Array);
      expect(result.bytes.length).toBe(0);
    });
  });

  describe('enum exports', () => {
    it('should export HashAlgorithm enum with all values', () => {
      expect(HashAlgorithm.SHA256).toBe('SHA-256');
      expect(HashAlgorithm.SHA384).toBe('SHA-384');
      expect(HashAlgorithm.SHA512).toBe('SHA-512');
    });

    it('should export SignatureAlgorithm enum with all values', () => {
      expect(SignatureAlgorithm.ECDSA_P256).toBe('ECDSA_P256');
      expect(SignatureAlgorithm.ECDSA_P384).toBe('ECDSA_P384');
      expect(SignatureAlgorithm.ECDSA_P521).toBe('ECDSA_P521');
      expect(SignatureAlgorithm.RSA_PSS).toBe('RSA_PSS');
      expect(SignatureAlgorithm.RSA_PKCS1).toBe('RSA_PKCS1');
      expect(SignatureAlgorithm.ED25519).toBe('ED25519');
    });

    it('should export KeyDerivationFunction enum with all values', () => {
      expect(KeyDerivationFunction.HKDF).toBe('HKDF');
      expect(KeyDerivationFunction.PBKDF2).toBe('PBKDF2');
    });

    it('should have frozen enums', () => {
      expect(Object.isFrozen(HashAlgorithm)).toBe(true);
      expect(Object.isFrozen(SignatureAlgorithm)).toBe(true);
      expect(Object.isFrozen(KeyDerivationFunction)).toBe(true);
    });
  });
});
