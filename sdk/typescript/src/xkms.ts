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
// XKMS passthrough domain types.
//
// Mirrors pkg/api/rest/handlers_xkms.go (/api/v1/xkms/*) and the wire types
// of go-xkms pkg/api/transport. XKMSClient reads the xkms backends and their
// health, seals and unseals data, and manages the certificates stored
// beside xkms keys. It is distinct from KMSClient (/api/v1/kms).
//
// Wire note: Go []byte fields travel as base64 strings, so every data,
// ciphertext, nonce, tag, aad, TPM blob and wrapped DEK field here is a
// base64 string. XKMSBackendCapabilities keys are PascalCase on the wire.
// ---------------------------------------------------------------------------

// XKMSBackendCapabilities lists what an xkms backend can do.
export interface XKMSBackendCapabilities {
  Keys: boolean;
  HardwareBacked: boolean;
  Signing: boolean;
  Decryption: boolean;
  KeyRotation: boolean;
  SymmetricEncryption: boolean;
  Sealing: boolean;
  Import: boolean;
  Export: boolean;
  KeyAgreement: boolean;
  ECIES: boolean;
  Attestation: boolean;
  QuantumSigning: boolean;
  KeyEncapsulation: boolean;
}

// XKMSBackendInfo describes one xkms backend.
export interface XKMSBackendInfo {
  id: string;
  type: string;
  hardware_backed: boolean;
  capabilities: XKMSBackendCapabilities;
}

// XKMSListBackendsResponse is the GET /api/v1/xkms/backends body.
export interface XKMSListBackendsResponse {
  backends: XKMSBackendInfo[];
  page?: number;
  page_size?: number;
  total?: number;
  has_more?: boolean;
}

// XKMSHealthResponse is the GET /api/v1/xkms/health body.
export interface XKMSHealthResponse {
  status: string;
  version?: string;
  message?: string;
}

// XKMSSealCapabilityResponse is the GET /api/v1/xkms/seal/capability body.
export interface XKMSSealCapabilityResponse {
  can_seal: boolean;
  backend: string;
  message?: string;
}

// XKMSSealRequest is the POST /api/v1/xkms/seal body (base64 byte fields).
export interface XKMSSealRequest {
  backend: string;
  key_id: string;
  data: string;
  aad?: string;
  tenant_id?: string;
  tpm_policy?: string;
  password?: string;
}

// XKMSSealResponse is the POST /api/v1/xkms/seal response (base64 byte fields).
export interface XKMSSealResponse {
  backend: string;
  ciphertext: string;
  nonce?: string;
  tag?: string;
  tpm_public?: string;
  tpm_private?: string;
  wrapped_dek?: string;
  key_id?: string;
  metadata?: Record<string, string>;
  key_version?: number;
}

// XKMSUnsealRequest is the POST /api/v1/xkms/unseal body (base64 byte fields).
export interface XKMSUnsealRequest {
  backend: string;
  key_id: string;
  ciphertext: string;
  nonce?: string;
  tag?: string;
  aad?: string;
  tenant_id?: string;
  tpm_public?: string;
  tpm_private?: string;
  wrapped_dek?: string;
  metadata?: Record<string, string>;
  password?: string;
  key_version?: number;
}

// XKMSUnsealResponse is the POST /api/v1/xkms/unseal response.
export interface XKMSUnsealResponse {
  // plaintext is base64.
  plaintext: string;
}

// XKMSCertificateInfo describes one stored certificate.
export interface XKMSCertificateInfo {
  key_id: string;
  subject?: string;
  issuer?: string;
  not_before?: string;
  not_after?: string;
  serial_number?: string;
  certificate_pem?: string;
}

// XKMSListCertificatesResponse is the GET /api/v1/xkms/certs body.
export interface XKMSListCertificatesResponse {
  certificates: XKMSCertificateInfo[];
  page?: number;
  page_size?: number;
  total?: number;
  has_more?: boolean;
}

// XKMSCertificateResponse is the GET /api/v1/xkms/certs/{id} body.
export interface XKMSCertificateResponse {
  key_id: string;
  certificate_pem: string;
}

// ---------------------------------------------------------------------------
// XKMSClient interface
// ---------------------------------------------------------------------------

// XKMSClient drives the xkms passthrough routes under /api/v1/xkms.
export interface XKMSClient {
  // listBackends maps to GET /api/v1/xkms/backends.
  listBackends(): Promise<XKMSListBackendsResponse>;
  // getBackend maps to GET /api/v1/xkms/backends/{id}.
  getBackend(id: string): Promise<XKMSBackendInfo>;
  // health maps to GET /api/v1/xkms/health.
  health(): Promise<XKMSHealthResponse>;
  // canSeal maps to GET /api/v1/xkms/seal/capability[?backend=].
  canSeal(backend?: string): Promise<XKMSSealCapabilityResponse>;
  // seal maps to POST /api/v1/xkms/seal.
  seal(req: XKMSSealRequest): Promise<XKMSSealResponse>;
  // unseal maps to POST /api/v1/xkms/unseal.
  unseal(req: XKMSUnsealRequest): Promise<XKMSUnsealResponse>;
  // listCertificates maps to GET /api/v1/xkms/certs[?backend=].
  listCertificates(backend?: string): Promise<XKMSListCertificatesResponse>;
  // getCertificate maps to GET /api/v1/xkms/certs/{id}.
  getCertificate(keyID: string): Promise<XKMSCertificateResponse>;
  // saveCertificate maps to POST /api/v1/xkms/certs?key_id=.
  saveCertificate(keyID: string, certificatePEM: string): Promise<void>;
  // deleteCertificate maps to DELETE /api/v1/xkms/certs/{id}.
  deleteCertificate(keyID: string): Promise<void>;
}
