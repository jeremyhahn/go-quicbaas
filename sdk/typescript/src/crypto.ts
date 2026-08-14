// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// ---------------------------------------------------------------------------
// Crypto primitives domain types.
//
// Mirrors pkg/api/rest/handlers_crypto.go (/api/v1/crypto/*) and the
// service.{Encrypt,Decrypt,Rewrap,Sign,Verify,Hash,GenerateRandom}Request /
// Response shapes in pkg/service/kms_types.go. This is the operator "crypto
// console" surface for the primitives the MCP tool set names
// crypto_encrypt/decrypt/rewrap/sign/verify/hash/random/datakey.
//
// Wire note: every byte-slice field ([]byte in Go) is base64-encoded on the
// wire by encoding/json. All string fields below that represent binary data
// (plaintext, ciphertext, message, signature, hash, random_bytes, nonce,
// context) are base64 strings — callers encode/decode at the UI boundary.
//
// HMAC and batch encrypt/decrypt exist on the MCP transport (see
// pkg/api/mcp/tools.go) but have no REST route in handlers_crypto.go /
// server.go, so they are intentionally not exposed here — the browser only
// ever talks REST.
// ---------------------------------------------------------------------------

/** HashAlgorithm specifies a cryptographic hash function. */
export type HashAlgorithm =
  | 'SHA256'
  | 'SHA384'
  | 'SHA512'
  | 'SHA3-256'
  | 'SHA3-384'
  | 'SHA3-512'
  | 'BLAKE2B-256'
  | 'BLAKE2B-512';

/** CryptoEncryptRequest requests data encryption under a named key. */
export interface CryptoEncryptRequest {
  key_name: string;
  /** plaintext is base64-encoded. */
  plaintext: string;
  /** context is optional base64-encoded additional authenticated data. */
  context?: string;
  /** nonce optionally specifies a base64-encoded, algorithm-dependent nonce. */
  nonce?: string;
}

/** CryptoEncryptResponse returns encrypted data. */
export interface CryptoEncryptResponse {
  key_id: string;
  key_version: number;
  /** ciphertext is base64-encoded. */
  ciphertext: string;
  nonce?: string;
  timestamp: string;
}

/** CryptoDecryptRequest requests data decryption under a named key. */
export interface CryptoDecryptRequest {
  key_name: string;
  /** ciphertext is base64-encoded. */
  ciphertext: string;
  context?: string;
  nonce?: string;
  key_version?: number;
}

/** CryptoDecryptResponse returns decrypted data. */
export interface CryptoDecryptResponse {
  key_id: string;
  key_version: number;
  /** plaintext is base64-encoded. */
  plaintext: string;
  timestamp: string;
}

/** CryptoRewrapRequest re-encrypts ciphertext with the key's latest version. */
export interface CryptoRewrapRequest {
  key_name: string;
  ciphertext: string;
  context?: string;
  nonce?: string;
}

/** CryptoRewrapResponse returns re-encrypted data. */
export interface CryptoRewrapResponse {
  key_id: string;
  old_key_version: number;
  new_key_version: number;
  ciphertext: string;
  nonce?: string;
  timestamp: string;
}

/** CryptoSignRequest requests data signing. */
export interface CryptoSignRequest {
  key_name: string;
  /** message is base64-encoded. */
  message: string;
  hash_algorithm?: HashAlgorithm;
  /** pre_hashed indicates message is already a hash digest. */
  pre_hashed?: boolean;
}

/** CryptoSignResponse returns the signature. */
export interface CryptoSignResponse {
  key_id: string;
  key_version: number;
  /** signature is base64-encoded. */
  signature: string;
  hash_algorithm: HashAlgorithm;
  timestamp: string;
}

/** CryptoVerifyRequest requests signature verification. */
export interface CryptoVerifyRequest {
  key_name: string;
  message: string;
  signature: string;
  hash_algorithm?: HashAlgorithm;
  key_version?: number;
}

/** CryptoVerifyResponse returns the verification result. */
export interface CryptoVerifyResponse {
  key_id: string;
  key_version: number;
  valid: boolean;
  timestamp: string;
}

/** CryptoHashRequest requests hashing of arbitrary data. */
export interface CryptoHashRequest {
  /** data is base64-encoded. */
  data: string;
  algorithm: HashAlgorithm;
}

/** CryptoHashResponse returns the computed hash. */
export interface CryptoHashResponse {
  algorithm: HashAlgorithm;
  /** hash is base64-encoded. */
  hash: string;
  timestamp: string;
}

/** CryptoRandomResponse returns cryptographically secure random bytes. */
export interface CryptoRandomResponse {
  /** random_bytes is base64-encoded. */
  random_bytes: string;
  timestamp: string;
}

/** CryptoDataKeyRequest requests generation of a data encryption key wrapped by a named KEK. */
export interface CryptoDataKeyRequest {
  key_name: string;
  key_size_bits?: number;
  algorithm?: string;
  /** context is optional base64-encoded additional authenticated data. */
  context?: string;
}

/**
 * CryptoDataKeyResponse returns both the plaintext data key and its wrapped
 * form. plaintext is minted fresh per call and never persisted or re-served
 * by the server — display it once, like issueCertificate's private key.
 */
export interface CryptoDataKeyResponse {
  key_id: string;
  /** plaintext is the base64-encoded plaintext data key. Handle with care. */
  plaintext: string;
  /** ciphertext_blob is the base64-encoded wrapped data key. */
  ciphertext_blob: string;
  timestamp: string;
}

// ---------------------------------------------------------------------------
// CryptoClient interface
// ---------------------------------------------------------------------------

/**
 * CryptoClient exposes low-level cryptographic primitives backed by KMS-held
 * keys. Maps to /api/v1/crypto/* REST endpoints.
 */
export interface CryptoClient {
  /** encrypt encrypts plaintext under a named key. POST /api/v1/crypto/encrypt/{name} */
  encrypt(req: CryptoEncryptRequest): Promise<CryptoEncryptResponse>;

  /** decrypt decrypts ciphertext under a named key. POST /api/v1/crypto/decrypt/{name} */
  decrypt(req: CryptoDecryptRequest): Promise<CryptoDecryptResponse>;

  /** rewrap re-encrypts ciphertext with the key's latest version. POST /api/v1/crypto/rewrap/{name} */
  rewrap(req: CryptoRewrapRequest): Promise<CryptoRewrapResponse>;

  /** sign signs a message with a named key. POST /api/v1/crypto/sign/{name} */
  sign(req: CryptoSignRequest): Promise<CryptoSignResponse>;

  /** verify verifies a signature with a named key. POST /api/v1/crypto/verify/{name} */
  verify(req: CryptoVerifyRequest): Promise<CryptoVerifyResponse>;

  /** hash computes a hash digest. POST /api/v1/crypto/hash */
  hash(req: CryptoHashRequest): Promise<CryptoHashResponse>;

  /** random generates cryptographically secure random bytes. POST /api/v1/crypto/random */
  random(numBytes?: number): Promise<CryptoRandomResponse>;

  /** dataKey generates a new data encryption key wrapped by a named KEK. POST /api/v1/crypto/datakey/{name} */
  dataKey(req: CryptoDataKeyRequest): Promise<CryptoDataKeyResponse>;
}
