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
// PIV (Personal Identity Verification) slot domain types.
//
// Mirrors pkg/api/rest/handlers_xkms.go (/api/v1/xkms/piv/slots*). A PIV
// slot (e.g. "9a", "9c") is addressed by a path segment, which this client
// encodes with encodeURIComponent; the optional `backend` selects the xkms
// backend and travels as a query parameter on GET/DELETE and in the body on
// PUT/POST. Byte fields (certificates, keys, CSRs) travel as strings: PEM
// text, or base64 when the format is DER.
//
// The xkms PIN routes are not exposed: the server answers 501 for them.
// ---------------------------------------------------------------------------

/** PIVKeyAlgorithm names the key types a PIV slot can generate. */
export type PIVKeyAlgorithm = 'rsa2048' | 'rsa4096' | 'ecdsap256' | 'ecdsap384' | 'ed25519';

/** PIVCertFormat is the encoding of a slot certificate on the wire. */
export type PIVCertFormat = 'pem' | 'der';

/** PIVSlotStatus describes one PIV slot and what it holds. */
export interface PIVSlotStatus {
  slot: string;
  name: string;
  description: string;
  has_cert: boolean;
  has_key: boolean;
  subject?: string;
  issuer?: string;
  algorithm?: string;
  key_size?: number;
  not_after?: string;
  fingerprint?: string;
  backend?: string;
}

/** ListPIVSlotsResponse is the answer of GET /api/v1/xkms/piv/slots. */
export interface ListPIVSlotsResponse {
  slots: PIVSlotStatus[];
  total_count?: number;
  next_cursor?: string;
}

/** GetPIVCertificateResponse carries a slot's certificate. */
export interface GetPIVCertificateResponse {
  slot: string;
  certificate: string;
  format: PIVCertFormat;
}

/** StorePIVCertificateRequest stores a certificate in a slot. */
export interface StorePIVCertificateRequest {
  backend?: string;
  certificate: string;
  format: PIVCertFormat;
}

/** GeneratePIVKeyRequest generates a key (and a self-signed certificate) in a slot. */
export interface GeneratePIVKeyRequest {
  backend?: string;
  algorithm: PIVKeyAlgorithm;
  subject: string;
  organization?: string;
  country?: string;
  output_format?: string;
  validity_days?: number;
}

/** GeneratePIVKeyResponse carries the generated key's certificate and public key. */
export interface GeneratePIVKeyResponse {
  slot: string;
  certificate: string;
  public_key: string;
  csr?: string;
  chain_pem?: string;
}

/** GeneratePIVCSRRequest asks for a CSR signed by the key in a slot. */
export interface GeneratePIVCSRRequest {
  backend?: string;
  subject: string;
  organization?: string;
  organizational_unit?: string;
  country?: string;
  state?: string;
  locality?: string;
}

/** GeneratePIVCSRResponse carries the CSR. */
export interface GeneratePIVCSRResponse {
  slot: string;
  csr: string;
}

/** Options of PIVClient.getCertificate. */
export interface GetPIVCertificateOptions {
  backend?: string;
  format?: PIVCertFormat;
}

/** PIVClient manages the PIV slots of an xkms backend. */
export interface PIVClient {
  /** Lists the slots and what each holds. */
  listSlots(backend?: string): Promise<ListPIVSlotsResponse>;
  /** Reads the certificate in a slot. */
  getCertificate(slot: string, opts?: GetPIVCertificateOptions): Promise<GetPIVCertificateResponse>;
  /** Stores a certificate in a slot. */
  storeCertificate(slot: string, req: StorePIVCertificateRequest): Promise<void>;
  /** Deletes the certificate in a slot. */
  deleteCertificate(slot: string, backend?: string): Promise<void>;
  /** Generates a key in a slot. */
  generateKey(slot: string, req: GeneratePIVKeyRequest): Promise<GeneratePIVKeyResponse>;
  /** Generates a CSR with the key in a slot. */
  generateCSR(slot: string, req: GeneratePIVCSRRequest): Promise<GeneratePIVCSRResponse>;
}
