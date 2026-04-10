/**
 * QuicBaaS JavaScript SDK - Crypto Client
 *
 * Provides cryptographic operations including encryption, decryption,
 * signing, verification, hashing, HMAC, key derivation, and secure
 * random number generation.
 *
 * @module @quicbaas/client/clients/crypto
 * @copyright 2025 Jeremy Hahn, Automate The Things, LLC
 * @license AGPL-3.0-or-later
 */

import { ValidationError } from '../errors/index.js';
import {
  required,
  requiredString,
  positiveInteger,
  oneOf,
} from '../utils/validation.js';

/**
 * API base path for crypto endpoints.
 * @constant {string}
 */
const CRYPTO_API_PATH = '/api/v1/crypto';

/**
 * Hash algorithms supported by the crypto service.
 * @readonly
 * @enum {string}
 */
export const HashAlgorithm = Object.freeze({
  /** SHA-256 hash algorithm (256-bit output) */
  SHA256: 'SHA-256',
  /** SHA-384 hash algorithm (384-bit output) */
  SHA384: 'SHA-384',
  /** SHA-512 hash algorithm (512-bit output) */
  SHA512: 'SHA-512',
});

/**
 * Signature algorithms supported by the crypto service.
 * @readonly
 * @enum {string}
 */
export const SignatureAlgorithm = Object.freeze({
  /** ECDSA with P-256 curve */
  ECDSA_P256: 'ECDSA_P256',
  /** ECDSA with P-384 curve */
  ECDSA_P384: 'ECDSA_P384',
  /** ECDSA with P-521 curve */
  ECDSA_P521: 'ECDSA_P521',
  /** RSA-PSS signature scheme */
  RSA_PSS: 'RSA_PSS',
  /** RSA PKCS#1 v1.5 signature scheme */
  RSA_PKCS1: 'RSA_PKCS1',
  /** EdDSA with Ed25519 curve */
  ED25519: 'ED25519',
});

/**
 * Key derivation functions supported by the crypto service.
 * @readonly
 * @enum {string}
 */
export const KeyDerivationFunction = Object.freeze({
  /** HKDF (HMAC-based Key Derivation Function) */
  HKDF: 'HKDF',
  /** PBKDF2 (Password-Based Key Derivation Function 2) */
  PBKDF2: 'PBKDF2',
});

/**
 * @typedef {Object} EncryptOptions
 * @property {string|Uint8Array} [context] - Additional authenticated data (AAD)
 * @property {number} [keyVersion] - Specific key version to use
 */

/**
 * @typedef {Object} EncryptResponse
 * @property {string} ciphertext - Base64-encoded ciphertext
 * @property {number} keyVersion - Key version used for encryption
 */

/**
 * @typedef {Object} DecryptOptions
 * @property {string|Uint8Array} [context] - Additional authenticated data (AAD) used during encryption
 * @property {number} [keyVersion] - Key version used for encryption
 */

/**
 * @typedef {Object} DecryptResponse
 * @property {Uint8Array} plaintext - Decrypted data
 */

/**
 * @typedef {Object} SignOptions
 * @property {string} [hashAlgorithm] - Hash algorithm for signing (default: SHA-256)
 * @property {string} [signatureAlgorithm] - Signature algorithm to use
 * @property {number} [keyVersion] - Specific key version to use
 */

/**
 * @typedef {Object} SignResponse
 * @property {string} signature - Base64-encoded signature
 * @property {number} keyVersion - Key version used for signing
 */

/**
 * @typedef {Object} VerifyOptions
 * @property {string} [hashAlgorithm] - Hash algorithm used during signing
 * @property {string} [signatureAlgorithm] - Signature algorithm used
 * @property {number} [keyVersion] - Key version used for signing
 */

/**
 * @typedef {Object} VerifyResponse
 * @property {boolean} valid - Whether the signature is valid
 */

/**
 * @typedef {Object} HashResponse
 * @property {string} hash - Base64-encoded hash digest
 * @property {string} algorithm - Hash algorithm used
 */

/**
 * @typedef {Object} HmacResponse
 * @property {string} hmac - Base64-encoded HMAC
 * @property {number} keyVersion - Key version used
 */

/**
 * @typedef {Object} DeriveKeyOptions
 * @property {string} [kdf] - Key derivation function (HKDF, PBKDF2)
 * @property {number} [length] - Derived key length in bytes (default: 32)
 * @property {number} [iterations] - Iterations for PBKDF2 (default: 100000)
 * @property {string} [hashAlgorithm] - Hash algorithm for KDF (default: SHA-256)
 * @property {string|Uint8Array} [salt] - Salt for key derivation
 * @property {string|Uint8Array} [info] - Additional info for HKDF
 */

/**
 * @typedef {Object} DeriveKeyResponse
 * @property {string} derivedKey - Base64-encoded derived key
 * @property {number} keyVersion - Master key version used
 */

/**
 * @typedef {Object} GenerateRandomResponse
 * @property {string} data - Base64-encoded random bytes
 * @property {Uint8Array} bytes - Random bytes as Uint8Array
 */

/**
 * Crypto client for QuicBaaS.
 *
 * Provides cryptographic operations as a service, including encryption,
 * decryption, signing, verification, hashing, HMAC computation, key
 * derivation, and secure random number generation.
 *
 * All binary data is automatically Base64 encoded/decoded for transport.
 *
 * @example
 * import { HttpTransport } from '@quicbaas/client/transport';
 * import CryptoClient, { HashAlgorithm } from '@quicbaas/client/clients/crypto';
 *
 * const transport = new HttpTransport('https://api.quicbaas.example.com');
 * transport.setAuthToken('your-auth-token');
 * const crypto = new CryptoClient(transport);
 *
 * // Encrypt data
 * const plaintext = new TextEncoder().encode('Hello, World!');
 * const encrypted = await crypto.encrypt('my-key-id', plaintext);
 * console.log('Ciphertext:', encrypted.ciphertext);
 *
 * // Decrypt data
 * const decrypted = await crypto.decrypt('my-key-id', encrypted.ciphertext);
 * const text = new TextDecoder().decode(decrypted.plaintext);
 * console.log('Plaintext:', text);
 *
 * // Hash data
 * const hashResult = await crypto.hash(HashAlgorithm.SHA256, plaintext);
 * console.log('Hash:', hashResult.hash);
 */
export class CryptoClient {
  /**
   * HTTP transport instance.
   * @type {import('../transport/http.js').HttpTransport}
   * @private
   */
  #transport;

  /**
   * Create a new CryptoClient instance.
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
   * Encrypt data using the specified key.
   *
   * Uses authenticated encryption (AES-GCM) to encrypt the plaintext.
   * Optional context/AAD can be provided for additional authentication.
   *
   * @param {string} keyId - The ID of the encryption key
   * @param {string|Uint8Array} plaintext - Data to encrypt
   * @param {EncryptOptions} [options={}] - Encryption options
   * @returns {Promise<EncryptResponse>} The encryption result
   * @throws {ValidationError} If keyId or plaintext is invalid
   * @throws {NotFoundError} If the key does not exist
   * @throws {AuthorizationError} If not authorized to use the key
   *
   * @example
   * const plaintext = new TextEncoder().encode('Sensitive data');
   * const result = await crypto.encrypt('my-key', plaintext, {
   *   context: 'user-123'
   * });
   * console.log('Ciphertext:', result.ciphertext);
   * console.log('Key version:', result.keyVersion);
   */
  async encrypt(keyId, plaintext, options = {}) {
    requiredString(keyId, 'keyId');
    this.#validateBinaryData(plaintext, 'plaintext');
    this.#validateEncryptOptions(options);

    const body = {
      key_id: keyId,
      plaintext: this.#toBase64(plaintext),
    };

    if (options.context !== undefined) {
      body.context = this.#toBase64(options.context);
    }

    if (options.keyVersion !== undefined) {
      body.key_version = options.keyVersion;
    }

    const response = await this.#transport.post(`${CRYPTO_API_PATH}/encrypt`, body);

    return {
      ciphertext: response?.ciphertext ?? '',
      keyVersion: response?.key_version ?? response?.keyVersion ?? 0,
    };
  }

  /**
   * Decrypt data using the specified key.
   *
   * Decrypts ciphertext that was encrypted using the encrypt method.
   * If context/AAD was used during encryption, the same context must be provided.
   *
   * @param {string} keyId - The ID of the encryption key
   * @param {string} ciphertext - Base64-encoded ciphertext to decrypt
   * @param {DecryptOptions} [options={}] - Decryption options
   * @returns {Promise<DecryptResponse>} The decryption result
   * @throws {ValidationError} If keyId or ciphertext is invalid
   * @throws {NotFoundError} If the key does not exist
   * @throws {AuthorizationError} If not authorized to use the key
   *
   * @example
   * const result = await crypto.decrypt('my-key', ciphertext, {
   *   context: 'user-123'
   * });
   * const text = new TextDecoder().decode(result.plaintext);
   * console.log('Decrypted:', text);
   */
  async decrypt(keyId, ciphertext, options = {}) {
    requiredString(keyId, 'keyId');
    requiredString(ciphertext, 'ciphertext');
    this.#validateDecryptOptions(options);

    const body = {
      key_id: keyId,
      ciphertext: ciphertext,
    };

    if (options.context !== undefined) {
      body.context = this.#toBase64(options.context);
    }

    if (options.keyVersion !== undefined) {
      body.key_version = options.keyVersion;
    }

    const response = await this.#transport.post(`${CRYPTO_API_PATH}/decrypt`, body);

    return {
      plaintext: this.#fromBase64(response?.plaintext ?? ''),
    };
  }

  /**
   * Create a digital signature for data.
   *
   * Signs the provided data using the specified key. The data is hashed
   * before signing using the specified hash algorithm.
   *
   * @param {string} keyId - The ID of the signing key
   * @param {string|Uint8Array} data - Data to sign
   * @param {SignOptions} [options={}] - Signing options
   * @returns {Promise<SignResponse>} The signature result
   * @throws {ValidationError} If keyId or data is invalid
   * @throws {NotFoundError} If the key does not exist
   * @throws {AuthorizationError} If not authorized to use the key
   *
   * @example
   * const data = new TextEncoder().encode('Document content');
   * const result = await crypto.sign('signing-key', data, {
   *   hashAlgorithm: HashAlgorithm.SHA256
   * });
   * console.log('Signature:', result.signature);
   */
  async sign(keyId, data, options = {}) {
    requiredString(keyId, 'keyId');
    this.#validateBinaryData(data, 'data');
    this.#validateSignOptions(options);

    const body = {
      key_id: keyId,
      data: this.#toBase64(data),
    };

    if (options.hashAlgorithm !== undefined) {
      body.hash_algorithm = options.hashAlgorithm;
    }

    if (options.signatureAlgorithm !== undefined) {
      body.signature_algorithm = options.signatureAlgorithm;
    }

    if (options.keyVersion !== undefined) {
      body.key_version = options.keyVersion;
    }

    const response = await this.#transport.post(`${CRYPTO_API_PATH}/sign`, body);

    return {
      signature: response?.signature ?? '',
      keyVersion: response?.key_version ?? response?.keyVersion ?? 0,
    };
  }

  /**
   * Verify a digital signature.
   *
   * Verifies that the provided signature is valid for the given data
   * and was created using the specified key.
   *
   * @param {string} keyId - The ID of the signing key
   * @param {string|Uint8Array} data - Original data that was signed
   * @param {string} signature - Base64-encoded signature to verify
   * @param {VerifyOptions} [options={}] - Verification options
   * @returns {Promise<VerifyResponse>} The verification result
   * @throws {ValidationError} If keyId, data, or signature is invalid
   * @throws {NotFoundError} If the key does not exist
   * @throws {AuthorizationError} If not authorized to use the key
   *
   * @example
   * const data = new TextEncoder().encode('Document content');
   * const result = await crypto.verify('signing-key', data, signature, {
   *   hashAlgorithm: HashAlgorithm.SHA256
   * });
   * if (result.valid) {
   *   console.log('Signature is valid');
   * } else {
   *   console.log('Signature is invalid');
   * }
   */
  async verify(keyId, data, signature, options = {}) {
    requiredString(keyId, 'keyId');
    this.#validateBinaryData(data, 'data');
    requiredString(signature, 'signature');
    this.#validateVerifyOptions(options);

    const body = {
      key_id: keyId,
      data: this.#toBase64(data),
      signature: signature,
    };

    if (options.hashAlgorithm !== undefined) {
      body.hash_algorithm = options.hashAlgorithm;
    }

    if (options.signatureAlgorithm !== undefined) {
      body.signature_algorithm = options.signatureAlgorithm;
    }

    if (options.keyVersion !== undefined) {
      body.key_version = options.keyVersion;
    }

    const response = await this.#transport.post(`${CRYPTO_API_PATH}/verify`, body);

    return {
      valid: response?.valid ?? false,
    };
  }

  /**
   * Compute a cryptographic hash of data.
   *
   * Computes a hash digest of the provided data using the specified
   * hash algorithm. This is a keyless operation.
   *
   * @param {string} algorithm - Hash algorithm to use (from HashAlgorithm enum)
   * @param {string|Uint8Array} data - Data to hash
   * @returns {Promise<HashResponse>} The hash result
   * @throws {ValidationError} If algorithm or data is invalid
   *
   * @example
   * const data = new TextEncoder().encode('Hello, World!');
   * const result = await crypto.hash(HashAlgorithm.SHA256, data);
   * console.log('Hash:', result.hash);
   * console.log('Algorithm:', result.algorithm);
   */
  async hash(algorithm, data) {
    requiredString(algorithm, 'algorithm');
    oneOf(algorithm, Object.values(HashAlgorithm), 'algorithm');
    this.#validateBinaryData(data, 'data');

    const body = {
      algorithm: algorithm,
      data: this.#toBase64(data),
    };

    const response = await this.#transport.post(`${CRYPTO_API_PATH}/hash`, body);

    return {
      hash: response?.hash ?? '',
      algorithm: response?.algorithm ?? algorithm,
    };
  }

  /**
   * Compute an HMAC of data.
   *
   * Computes an HMAC (Hash-based Message Authentication Code) of the
   * provided data using the specified key.
   *
   * @param {string} keyId - The ID of the HMAC key
   * @param {string|Uint8Array} data - Data to authenticate
   * @returns {Promise<HmacResponse>} The HMAC result
   * @throws {ValidationError} If keyId or data is invalid
   * @throws {NotFoundError} If the key does not exist
   * @throws {AuthorizationError} If not authorized to use the key
   *
   * @example
   * const data = new TextEncoder().encode('Message to authenticate');
   * const result = await crypto.hmac('hmac-key', data);
   * console.log('HMAC:', result.hmac);
   */
  async hmac(keyId, data) {
    requiredString(keyId, 'keyId');
    this.#validateBinaryData(data, 'data');

    const body = {
      key_id: keyId,
      data: this.#toBase64(data),
    };

    const response = await this.#transport.post(`${CRYPTO_API_PATH}/hmac`, body);

    return {
      hmac: response?.hmac ?? '',
      keyVersion: response?.key_version ?? response?.keyVersion ?? 0,
    };
  }

  /**
   * Derive a key from a master key.
   *
   * Uses a key derivation function (KDF) to derive a new key from the
   * specified master key and context. Supports HKDF and PBKDF2.
   *
   * @param {string} keyId - The ID of the master key
   * @param {string|Uint8Array} context - Context/label for key derivation
   * @param {DeriveKeyOptions} [options={}] - Key derivation options
   * @returns {Promise<DeriveKeyResponse>} The derived key result
   * @throws {ValidationError} If keyId or context is invalid
   * @throws {NotFoundError} If the key does not exist
   * @throws {AuthorizationError} If not authorized to use the key
   *
   * @example
   * const context = 'user-123:encryption';
   * const result = await crypto.deriveKey('master-key', context, {
   *   kdf: KeyDerivationFunction.HKDF,
   *   length: 32,
   *   hashAlgorithm: HashAlgorithm.SHA256
   * });
   * console.log('Derived key:', result.derivedKey);
   */
  async deriveKey(keyId, context, options = {}) {
    requiredString(keyId, 'keyId');
    this.#validateBinaryData(context, 'context');
    this.#validateDeriveKeyOptions(options);

    const body = {
      key_id: keyId,
      context: this.#toBase64(context),
    };

    if (options.kdf !== undefined) {
      body.kdf = options.kdf;
    }

    if (options.length !== undefined) {
      body.length = options.length;
    }

    if (options.iterations !== undefined) {
      body.iterations = options.iterations;
    }

    if (options.hashAlgorithm !== undefined) {
      body.hash_algorithm = options.hashAlgorithm;
    }

    if (options.salt !== undefined) {
      body.salt = this.#toBase64(options.salt);
    }

    if (options.info !== undefined) {
      body.info = this.#toBase64(options.info);
    }

    const response = await this.#transport.post(`${CRYPTO_API_PATH}/derive-key`, body);

    return {
      derivedKey: response?.derived_key ?? response?.derivedKey ?? '',
      keyVersion: response?.key_version ?? response?.keyVersion ?? 0,
    };
  }

  /**
   * Generate cryptographically secure random bytes.
   *
   * Generates random bytes using a cryptographically secure random
   * number generator (CSPRNG).
   *
   * @param {number} length - Number of random bytes to generate
   * @returns {Promise<GenerateRandomResponse>} The random bytes result
   * @throws {ValidationError} If length is invalid (must be positive integer)
   *
   * @example
   * const result = await crypto.generateRandom(32);
   * console.log('Random (base64):', result.data);
   * console.log('Random bytes:', result.bytes);
   */
  async generateRandom(length) {
    positiveInteger(length, 'length');
    required(length, 'length');

    const body = {
      length: length,
    };

    const response = await this.#transport.post(`${CRYPTO_API_PATH}/random`, body);

    const data = response?.data ?? '';

    return {
      data: data,
      bytes: this.#fromBase64(data),
    };
  }

  /**
   * Validate binary data input (string or Uint8Array).
   *
   * @param {string|Uint8Array} value - Value to validate
   * @param {string} name - Parameter name for error message
   * @throws {ValidationError} If value is invalid
   * @private
   */
  #validateBinaryData(value, name) {
    required(value, name);
    if (typeof value !== 'string' && !(value instanceof Uint8Array)) {
      throw new ValidationError(
        `${name} must be a string or Uint8Array`,
        name
      );
    }
    if (typeof value === 'string' && value.length === 0) {
      throw new ValidationError(`${name} cannot be empty`, name);
    }
    if (value instanceof Uint8Array && value.length === 0) {
      throw new ValidationError(`${name} cannot be empty`, name);
    }
  }

  /**
   * Validate encrypt options.
   *
   * @param {EncryptOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateEncryptOptions(options) {
    if (options.context !== undefined) {
      if (typeof options.context !== 'string' && !(options.context instanceof Uint8Array)) {
        throw new ValidationError(
          'options.context must be a string or Uint8Array',
          'options.context'
        );
      }
    }

    if (options.keyVersion !== undefined) {
      positiveInteger(options.keyVersion, 'options.keyVersion');
    }
  }

  /**
   * Validate decrypt options.
   *
   * @param {DecryptOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateDecryptOptions(options) {
    if (options.context !== undefined) {
      if (typeof options.context !== 'string' && !(options.context instanceof Uint8Array)) {
        throw new ValidationError(
          'options.context must be a string or Uint8Array',
          'options.context'
        );
      }
    }

    if (options.keyVersion !== undefined) {
      positiveInteger(options.keyVersion, 'options.keyVersion');
    }
  }

  /**
   * Validate sign options.
   *
   * @param {SignOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateSignOptions(options) {
    if (options.hashAlgorithm !== undefined) {
      oneOf(options.hashAlgorithm, Object.values(HashAlgorithm), 'options.hashAlgorithm');
    }

    if (options.signatureAlgorithm !== undefined) {
      oneOf(
        options.signatureAlgorithm,
        Object.values(SignatureAlgorithm),
        'options.signatureAlgorithm'
      );
    }

    if (options.keyVersion !== undefined) {
      positiveInteger(options.keyVersion, 'options.keyVersion');
    }
  }

  /**
   * Validate verify options.
   *
   * @param {VerifyOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateVerifyOptions(options) {
    if (options.hashAlgorithm !== undefined) {
      oneOf(options.hashAlgorithm, Object.values(HashAlgorithm), 'options.hashAlgorithm');
    }

    if (options.signatureAlgorithm !== undefined) {
      oneOf(
        options.signatureAlgorithm,
        Object.values(SignatureAlgorithm),
        'options.signatureAlgorithm'
      );
    }

    if (options.keyVersion !== undefined) {
      positiveInteger(options.keyVersion, 'options.keyVersion');
    }
  }

  /**
   * Validate derive key options.
   *
   * @param {DeriveKeyOptions} options - Options to validate
   * @throws {ValidationError} If options are invalid
   * @private
   */
  #validateDeriveKeyOptions(options) {
    if (options.kdf !== undefined) {
      oneOf(options.kdf, Object.values(KeyDerivationFunction), 'options.kdf');
    }

    if (options.length !== undefined) {
      positiveInteger(options.length, 'options.length');
    }

    if (options.iterations !== undefined) {
      positiveInteger(options.iterations, 'options.iterations');
    }

    if (options.hashAlgorithm !== undefined) {
      oneOf(options.hashAlgorithm, Object.values(HashAlgorithm), 'options.hashAlgorithm');
    }

    if (options.salt !== undefined) {
      if (typeof options.salt !== 'string' && !(options.salt instanceof Uint8Array)) {
        throw new ValidationError(
          'options.salt must be a string or Uint8Array',
          'options.salt'
        );
      }
    }

    if (options.info !== undefined) {
      if (typeof options.info !== 'string' && !(options.info instanceof Uint8Array)) {
        throw new ValidationError(
          'options.info must be a string or Uint8Array',
          'options.info'
        );
      }
    }
  }

  /**
   * Convert string or Uint8Array to Base64.
   *
   * @param {string|Uint8Array} data - Data to encode
   * @returns {string} Base64-encoded string
   * @private
   */
  #toBase64(data) {
    if (typeof data === 'string') {
      // Encode string as UTF-8 bytes first, then to Base64
      const encoder = new TextEncoder();
      const bytes = encoder.encode(data);
      return this.#uint8ArrayToBase64(bytes);
    }
    return this.#uint8ArrayToBase64(data);
  }

  /**
   * Convert Uint8Array to Base64 string.
   *
   * @param {Uint8Array} bytes - Bytes to encode
   * @returns {string} Base64-encoded string
   * @private
   */
  #uint8ArrayToBase64(bytes) {
    // Use btoa for browser compatibility, with proper binary handling
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
  }

  /**
   * Convert Base64 string to Uint8Array.
   *
   * @param {string} base64 - Base64-encoded string
   * @returns {Uint8Array} Decoded bytes
   * @private
   */
  #fromBase64(base64) {
    if (!base64 || base64.length === 0) {
      return new Uint8Array(0);
    }

    try {
      const binary = atob(base64);
      const bytes = new Uint8Array(binary.length);
      for (let i = 0; i < binary.length; i++) {
        bytes[i] = binary.charCodeAt(i);
      }
      return bytes;
    } catch {
      throw new ValidationError('Invalid Base64 encoding', 'base64');
    }
  }
}

export default CryptoClient;
