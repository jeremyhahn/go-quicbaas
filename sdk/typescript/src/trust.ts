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
// Trust composition domain types.
//
// Mirrors pkg/api/rest/handlers_trust.go (/api/v1/trust/*) and
// pkg/service/trust_service.go. TrustClient is a pure composition surface
// over the xkms TrustedCA — it does not reimplement cross-signing, AIA
// walking, or TLSA hashing.
//
// Wire note: the Go server types carry raw []byte / [][]byte fields
// (root_pems, chain_der, cert_der) that Go's encoding/json marshals as
// base64 strings. RestTrustClient decodes root_pems to plain PEM text for
// display; chain_der / cert_der are supplied by the caller as the base64
// payload extracted directly from a PEM block (which is exactly the base64
// encoding of the DER bytes — no re-encoding needed).
// ---------------------------------------------------------------------------

/** CrossSignedRootPoolResponse is the active CA's cross-sign-aware root set, PEM-decoded. */
export interface CrossSignedRootPoolResponse {
  root_pems: string[];
}

/** AutoImportAIARequest carries a chain of PEM certificates whose AIA extensions are walked. */
export interface AutoImportAIARequest {
  /** chain_pems is a list of PEM-encoded certificates (client-side convenience input). */
  chain_pems: string[];
}

/** AutoImportAIAResponse reports how many chain certificates had their AIA processed. */
export interface AutoImportAIAResponse {
  imported: number;
}

/** PublishDANERequest carries a PEM certificate plus the DNS name/port TLSA records are published for. */
export interface PublishDANERequest {
  name: string;
  port: number;
  /** cert_pem is a single PEM-encoded certificate (client-side convenience input). */
  cert_pem: string;
}

/** PublishDANEResponse reports how many TLSA records were published. */
export interface PublishDANEResponse {
  published: number;
}

// ---------------------------------------------------------------------------
// TrustClient interface
// ---------------------------------------------------------------------------

/**
 * TrustClient exposes trust composition operations (cross-sign root pool,
 * AIA auto-import, DANE publication). Maps to /api/v1/trust/* REST endpoints.
 */
export interface TrustClient {
  /** crossSignedRootPool returns the CA's cross-sign-aware root set. GET /api/v1/trust/cross-signed-roots */
  crossSignedRootPool(): Promise<CrossSignedRootPoolResponse>;

  /** autoImportAIA imports issuing CAs referenced by a chain's AIA extension. POST /api/v1/trust/auto-import-aia */
  autoImportAIA(req: AutoImportAIARequest): Promise<AutoImportAIAResponse>;

  /**
   * publishDANE derives and publishes DANE TLSA records for a certificate.
   * Returns a typed error when no DANE publisher is registered on the server.
   * POST /api/v1/trust/publish-dane
   */
  publishDANE(req: PublishDANERequest): Promise<PublishDANEResponse>;
}
