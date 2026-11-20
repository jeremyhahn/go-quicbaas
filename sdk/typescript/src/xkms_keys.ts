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
// XKMS key domain types.
//
// Mirrors the xkms passthrough's key routes (pkg/api/rest/handlers_xkms.go,
// /api/v1/xkms/keys/*) and go-xkms pkg/api/transport/types.go. Every type
// carries the XKMS prefix: the SDK's own KMS types (KMSClient,
// /api/v1/kms/*) are a different surface.
//
// Wire note: Go []byte fields (plaintext, ciphertext, nonce, tag, iv,
// additional_data, data, signature) travel as base64 strings.
//
// Routing note: {id} and {version} are one path segment each; the client
// encodes every segment with encodeURIComponent, so an id containing '/'
// stays one segment.
// ---------------------------------------------------------------------------

/** XKMSKeyInfo describes one key held by an xkms backend. */
export interface XKMSKeyInfo {
  key_id: string;
  key_type: string;
  algorithm?: string;
  backend: string;
  public_key_pem?: string;
  key_size?: number;
  curve?: string;
  partition?: string;
  key_version?: number;
}

/** XKMSListKeysResponse enumerates keys, optionally filtered by backend. */
export interface XKMSListKeysResponse {
  keys: XKMSKeyInfo[];
  page?: number;
  page_size?: number;
  total?: number;
  has_more?: boolean;
}

/** XKMSGenerateKeyRequest requests a new key on a backend. */
export interface XKMSGenerateKeyRequest {
  key_id: string;
  backend: string;
  key_type: string;
  key_size?: number;
  curve?: string;
  hash?: string;
  algorithm?: string;
  exportable?: boolean;
  sensitive?: boolean;
  partition?: string;
  tpm_policy?: string;
  tenant_id?: string;
}

/** XKMSGenerateKeyResponse confirms key generation (HTTP 201). */
export interface XKMSGenerateKeyResponse {
  key_id: string;
  key_type: string;
  public_key_pem?: string;
  message?: string;
}

/** XKMSDeleteKeyResponse confirms key deletion. */
export interface XKMSDeleteKeyResponse {
  success: boolean;
  message?: string;
}

/** XKMSRotateKeyResponse confirms key rotation. */
export interface XKMSRotateKeyResponse {
  success: boolean;
  key_id: string;
  key_type?: string;
  message?: string;
  public_key_pem?: string;
  key_version?: number;
  certificate_reissue_required?: boolean;
}

/** XKMSEncryptRequest carries base64 plaintext and optional AAD. */
export interface XKMSEncryptRequest {
  plaintext: string;
  additional_data?: string;
}

/** XKMSEncryptResponse carries base64 ciphertext and AEAD parameters. */
export interface XKMSEncryptResponse {
  ciphertext: string;
  nonce?: string;
  tag?: string;
  iv?: string;
  gcm_nonce_mode?: string;
  key_version?: number;
}

/** XKMSDecryptRequest carries base64 ciphertext and AEAD parameters. */
export interface XKMSDecryptRequest {
  ciphertext: string;
  nonce?: string;
  tag?: string;
  additional_data?: string;
}

/** XKMSDecryptResponse carries the base64 plaintext. */
export interface XKMSDecryptResponse {
  plaintext: string;
  key_version?: number;
}

/** XKMSSignRequest carries base64 data and an optional hash name. */
export interface XKMSSignRequest {
  data: string;
  hash?: string;
}

/** XKMSSignResponse carries the base64 signature. */
export interface XKMSSignResponse {
  signature: string;
  algorithm?: string;
  key_version?: number;
}

/** XKMSVerifyRequest carries base64 data and signature. */
export interface XKMSVerifyRequest {
  data: string;
  signature: string;
  hash?: string;
}

/** XKMSVerifyResponse reports whether the signature is valid. */
export interface XKMSVerifyResponse {
  valid: boolean;
  message?: string;
  key_version?: number;
}

/** XKMSKeyVersion describes one version of a key. */
export interface XKMSKeyVersion {
  version: number;
  status: string;
  created_at: string;
}

/** XKMSListKeyVersionsResponse enumerates a key's versions. */
export interface XKMSListKeyVersionsResponse {
  key_id: string;
  versions: XKMSKeyVersion[];
  total: number;
}

/** XKMSBulkKeyVersionsResponse confirms an enable-all or disable-all. */
export interface XKMSBulkKeyVersionsResponse {
  key_id: string;
  count: number;
  message: string;
}

/** XKMSKeyVersionStatusResponse confirms one version's enable or disable. */
export interface XKMSKeyVersionStatusResponse {
  key_id: string;
  version: number;
  status: string;
}

// ---------------------------------------------------------------------------
// XKMSKeysClient interface
// ---------------------------------------------------------------------------

/**
 * XKMSKeysClient exposes the xkms passthrough's key routes: generation,
 * listing, lookup, deletion, rotation, encrypt/decrypt, sign/verify, and
 * key version management. Maps to /api/v1/xkms/keys/* REST endpoints.
 */
export interface XKMSKeysClient {
  /** listKeys lists keys, optionally on one backend. GET /api/v1/xkms/keys[?backend=] */
  listKeys(backend?: string): Promise<XKMSListKeysResponse>;

  /** generateKey creates a key. POST /api/v1/xkms/keys */
  generateKey(req: XKMSGenerateKeyRequest): Promise<XKMSGenerateKeyResponse>;

  /** getKey retrieves one key. GET /api/v1/xkms/keys/{id}[?backend=] */
  getKey(id: string, backend?: string): Promise<XKMSKeyInfo>;

  /** deleteKey removes a key. DELETE /api/v1/xkms/keys/{id} */
  deleteKey(id: string): Promise<XKMSDeleteKeyResponse>;

  /** rotateKey creates a new key version. POST /api/v1/xkms/keys/{id}/rotate */
  rotateKey(id: string): Promise<XKMSRotateKeyResponse>;

  /** encrypt encrypts base64 plaintext. POST /api/v1/xkms/keys/{id}/encrypt */
  encrypt(id: string, req: XKMSEncryptRequest): Promise<XKMSEncryptResponse>;

  /** decrypt decrypts base64 ciphertext. POST /api/v1/xkms/keys/{id}/decrypt */
  decrypt(id: string, req: XKMSDecryptRequest): Promise<XKMSDecryptResponse>;

  /** sign signs base64 data. POST /api/v1/xkms/keys/{id}/sign */
  sign(id: string, req: XKMSSignRequest): Promise<XKMSSignResponse>;

  /** verify checks a base64 signature. POST /api/v1/xkms/keys/{id}/verify */
  verify(id: string, req: XKMSVerifyRequest): Promise<XKMSVerifyResponse>;

  /** listKeyVersions lists a key's versions. GET /api/v1/xkms/keys/{id}/versions */
  listKeyVersions(id: string): Promise<XKMSListKeyVersionsResponse>;

  /** enableAllKeyVersions enables every version. POST /api/v1/xkms/keys/{id}/versions/enable-all */
  enableAllKeyVersions(id: string): Promise<XKMSBulkKeyVersionsResponse>;

  /** disableAllKeyVersions disables every version. POST /api/v1/xkms/keys/{id}/versions/disable-all */
  disableAllKeyVersions(id: string): Promise<XKMSBulkKeyVersionsResponse>;

  /** enableKeyVersion enables one version. POST /api/v1/xkms/keys/{id}/versions/{version}/enable */
  enableKeyVersion(id: string, version: number): Promise<XKMSKeyVersionStatusResponse>;

  /** disableKeyVersion disables one version. POST /api/v1/xkms/keys/{id}/versions/{version}/disable */
  disableKeyVersion(id: string, version: number): Promise<XKMSKeyVersionStatusResponse>;
}
