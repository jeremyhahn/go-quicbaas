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
// Certificate Authority (CA) domain types.
//
// Mirrors pkg/api/rest/handlers_certificate.go (/api/v1/certificate/*) and
// the xkms-native CA routes registered at /api/v1/xkms/ca/* (CRL generation
// and revocation lookup — see routes_xkms.go). CAClient is the browser-facing
// equivalent of the CLI's `quicbaas ca info|bundle|list|issue|revoke
// |export-bundle` command group plus sign-csr / verify / CRL, which the CLI
// does not expose but the underlying SDK (sdk/go/client/certificate) does.
//
// SECURITY NOTE: issueCertificate returns a freshly generated private key in
// private_key_pem. This is a mint-once operation — the server does not
// persist or ever re-serve this value — so it is safe for the UI to render it
// exactly once in the issuance response, the same way the CLI writes it to a
// file on disk. No other method in this file returns key material.
// ---------------------------------------------------------------------------

/** CertificateUsage selects the intended TLS role for an issued/signed certificate. */
export type CertificateUsage = 'client' | 'server' | 'both';

/** CertificateKeyAlgorithm selects the private key algorithm for certificate issuance. */
export type CertificateKeyAlgorithm = 'ecdsa' | 'rsa';

/** IssueCertificateRequest requests a brand-new leaf (or sub-CA) certificate. */
export interface IssueCertificateRequest {
  common_name: string;
  organization?: string;
  organizational_unit?: string;
  country?: string;
  province?: string;
  locality?: string;
  dns_names?: string[];
  ip_addresses?: string[];
  email_addresses?: string[];
  uris?: string[];
  validity_days?: number;
  usage?: CertificateUsage;
  is_ca?: boolean;
  max_path_len?: number;
  key_algorithm?: CertificateKeyAlgorithm;
  key_size?: number;
  metadata?: Record<string, string>;
}

/**
 * IssueCertificateResponse is the one-time mint response. private_key_pem is
 * generated fresh per call and never retrievable again — display it once and
 * discard.
 */
export interface IssueCertificateResponse {
  certificate_pem: string;
  private_key_pem: string;
  ca_certificate_pem: string;
  chain_pem: string;
  serial_number: string;
  not_before: string;
  not_after: string;
  timestamp: string;
}

/** SignCSRRequest signs a caller-supplied Certificate Signing Request. */
export interface SignCSRRequest {
  csr_pem: string;
  validity_days?: number;
  usage?: CertificateUsage;
}

/** SignCSRResponse contains the signed certificate. The private key never leaves the caller. */
export interface SignCSRResponse {
  certificate_pem: string;
  ca_certificate_pem: string;
  chain_pem: string;
  serial_number: string;
  not_before: string;
  not_after: string;
  timestamp: string;
}

/** RevokeCertificateResponse confirms a revocation. */
export interface RevokeCertificateResponse {
  success: boolean;
  timestamp: string;
}

/** VerifyCertificateResponse reports whether a certificate PEM is valid against the CA trust chain. */
export interface VerifyCertificateResponse {
  valid: boolean;
  error_message?: string;
  timestamp: string;
}

/** GetCACertificateResponse returns the leaf CA certificate and bundle. */
export interface GetCACertificateResponse {
  ca_certificate_pem: string;
  ca_bundle_pem: string;
  identity: string;
  timestamp: string;
}

/** CAInfoResponse returns parsed metadata for the active issuing CA. */
export interface CAInfoResponse {
  common_name: string;
  subject: string;
  issuer: string;
  serial_number: string;
  not_before: string;
  not_after: string;
  is_ca: boolean;
  key_type: string;
  key_bits?: number;
  signature_algorithm: string;
  certificate_pem: string;
  ca_bundle_pem: string;
  timestamp: string;
}

/** CABundleResponse returns the CA trust-chain PEM (equivalent to `quicbaas ca bundle`). */
export interface CABundleResponse {
  bundle_pem: string;
  timestamp: string;
}

/** CertificateSummary is one row in a ListCertificates response. */
export interface CertificateSummary {
  common_name: string;
  subject: string;
  issuer: string;
  serial_number: string;
  not_before: string;
  not_after: string;
  is_ca: boolean;
  key_usage?: string;
  dns_names?: string[];
}

/** ListCertificatesRequest filters the CA's certificate store. */
export interface ListCertificatesRequest {
  common_name?: string;
  include_revoked?: boolean;
  include_expired?: boolean;
  limit?: number;
}

/** ListCertificatesResponse enumerates certificates matching the filter. */
export interface ListCertificatesResponse {
  certificates: CertificateSummary[];
  total_count: number;
  timestamp: string;
}

/** GenerateCRLResponse contains the freshly generated Certificate Revocation List. */
export interface GenerateCRLResponse {
  crl_pem: string;
}

/** IsRevokedResponse reports whether a serial number has been revoked. */
export interface IsRevokedResponse {
  revoked: boolean;
  reason?: number;
  message?: string;
}

// ---------------------------------------------------------------------------
// CAClient interface
// ---------------------------------------------------------------------------

/**
 * CAClient exposes certificate authority administration operations.
 * Maps to /api/v1/certificate/* and /api/v1/xkms/ca/* REST endpoints.
 */
export interface CAClient {
  /** issueCertificate mints a new certificate. POST /api/v1/certificate/issue */
  issueCertificate(req: IssueCertificateRequest): Promise<IssueCertificateResponse>;

  /** signCSR signs a caller-supplied CSR. POST /api/v1/certificate/sign-csr */
  signCSR(req: SignCSRRequest): Promise<SignCSRResponse>;

  /** revokeCertificate revokes a certificate by serial number. POST /api/v1/certificate/revoke */
  revokeCertificate(serialNumber: string): Promise<RevokeCertificateResponse>;

  /** verifyCertificate checks a certificate PEM against the CA trust chain. POST /api/v1/certificate/verify */
  verifyCertificate(certificatePEM: string): Promise<VerifyCertificateResponse>;

  /** getCACertificate returns the leaf CA certificate and bundle. GET /api/v1/certificate/ca */
  getCACertificate(): Promise<GetCACertificateResponse>;

  /** caInfo returns parsed CA metadata. GET /api/v1/certificate/ca/info */
  caInfo(): Promise<CAInfoResponse>;

  /** getCABundle returns the CA trust-chain PEM. GET /api/v1/certificate/ca/bundle */
  getCABundle(): Promise<CABundleResponse>;

  /** listCertificates enumerates certificates in the CA store. GET /api/v1/certificate/ca/list */
  listCertificates(req?: ListCertificatesRequest): Promise<ListCertificatesResponse>;

  /** generateCRL mints a fresh CRL. POST /api/v1/xkms/ca/crl */
  generateCRL(): Promise<GenerateCRLResponse>;

  /** isRevoked checks a single serial number's revocation status. GET /api/v1/xkms/ca/revoked/{serial} */
  isRevoked(serialNumber: string): Promise<IsRevokedResponse>;
}
