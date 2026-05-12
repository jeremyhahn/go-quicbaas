// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

/**
 * Local type definitions for the Barrier / Ceremony / Bootstrap security
 * components. These mirror the types exposed by `@quicbaas/sdk` so the
 * components compile cleanly even before the SDK rename has landed.
 *
 * When the SDK rename completes the consumer can pass any object that
 * structurally matches these interfaces — the components remain unchanged.
 */

// ---------------------------------------------------------------------------
// Barrier
// ---------------------------------------------------------------------------

/** BarrierStrategy identifies how the encryption barrier protects the master key. */
export type BarrierStrategy =
  | 'disabled'
  | 'software'
  | 'shamir'
  | 'tpm2'
  | 'pkcs11'
  | 'aws-kms'
  | 'gcp-kms'
  | 'azure-kms'
  | 'vault-kms';

/** BarrierStatus is the strategy-aware status payload returned by the daemon. */
export interface BarrierStatus {
  /** strategy is the configured barrier strategy. */
  strategy: BarrierStrategy;
  /** sealed is true when the master key is not unsealed. */
  sealed: boolean;
  /** initialized is true once the barrier has been initialized at least once. */
  initialized: boolean;
  /** threshold is the number of shares required to unseal (Shamir only). */
  threshold: number;
  /** total_shares is the total shares minted by the last init (Shamir only). */
  total_shares: number;
  /** shares_provided is how many shares have been submitted toward unseal. */
  shares_provided: number;
  /** hardware_backed is true for tpm2/pkcs11/*-kms strategies. */
  hardware_backed: boolean;
  /** auto_unseal is true when the strategy unseals automatically at boot. */
  auto_unseal: boolean;
  /** version is an opaque generation counter that bumps on init/rekey. */
  version: number;
  /** cluster_name is the human-readable cluster identity, when known. */
  cluster_name?: string;
}

/** BarrierInitRequest initializes a new Shamir barrier. */
export interface BarrierInitRequest {
  threshold: number;
  total_shares: number;
}

/** BarrierInitResponse contains the freshly minted Shamir shares. */
export interface BarrierInitResponse {
  shares: string[];
}

/** BarrierUnsealRequest submits one share toward unsealing. */
export interface BarrierUnsealRequest {
  share: string;
}

/** BarrierUnsealResponse reports the new state after submitting a share. */
export interface BarrierUnsealResponse {
  sealed: boolean;
  shares_provided: number;
  threshold: number;
}

/**
 * BarrierClient is the structural contract the components use to talk to the
 * daemon. The frontend can pass either the SDK BarrierClient (post Phase 8)
 * or a thin adapter around the legacy SealClient — both satisfy this shape.
 */
export interface BarrierClient {
  status(): Promise<BarrierStatus>;
  init(req: BarrierInitRequest): Promise<BarrierInitResponse>;
  unseal(req: BarrierUnsealRequest): Promise<BarrierUnsealResponse>;
}

// ---------------------------------------------------------------------------
// Ceremony
// ---------------------------------------------------------------------------

/** OfficerKind identifies how an external officer authenticates. */
export type OfficerKind = 'cert' | 'pubkey';

/** OfficerClaimStatus tracks where an officer is in the claim flow. */
export type OfficerClaimStatus =
  | 'pending'
  | 'challenged'
  | 'claimed'
  | 'enrolled'
  | 'revoked';

/** CeremonyOfficer represents a participant in a key ceremony. */
export interface CeremonyOfficer {
  /** username is the officer's stable identifier. */
  username: string;
  /** kind identifies the proof type the officer must supply. */
  kind: OfficerKind;
  /** claim_status is the current state of the officer's claim flow. */
  claim_status: OfficerClaimStatus;
  /** share_index is the Shamir share assigned to this officer (1-based). */
  share_index: number;
  /** auth_factor is a free-form description (e.g. "yubikey", "passphrase"). */
  auth_factor: string;
}

/** ClaimCertificateRequest signs the ceremony challenge with a CSR private key. */
export interface ClaimCertificateRequest {
  username: string;
  challenge: string;
  signature_b64: string;
}

/** ClaimShareRequest delivers an officer's share contribution. */
export interface ClaimShareRequest {
  username: string;
  share: string;
}

/** ClaimResponse reports the outcome of a claim flow. */
export interface ClaimResponse {
  username: string;
  status: OfficerClaimStatus;
}

/** EnrollOfficerRequest creates an external officer record. */
export interface EnrollOfficerRequest {
  username: string;
  kind: OfficerKind;
  /** material is the PEM-encoded cert (kind=cert) or SSH/SPKI pubkey (kind=pubkey). */
  material: string;
  share_index: number;
  auth_factor: string;
}

/** CeremonyClient is the structural API the ceremony components consume. */
export interface CeremonyClient {
  listOfficers(): Promise<CeremonyOfficer[]>;
  claimCertificate(req: ClaimCertificateRequest): Promise<ClaimResponse>;
  claimShare(req: ClaimShareRequest): Promise<ClaimResponse>;
  enrollOfficer(req: EnrollOfficerRequest): Promise<CeremonyOfficer>;
}

// ---------------------------------------------------------------------------
// Bootstrap
// ---------------------------------------------------------------------------

/** BootstrapMethod identifies how the client trusts the daemon at first contact. */
export type BootstrapMethod = 'spki-pin' | 'dane' | 'noise' | 'direct';

/** BootstrapRequest is the user's chosen method + transport-specific input. */
export interface BootstrapRequest {
  method: BootstrapMethod;
  /** server_url is the URL the operator wants to bootstrap against. */
  server_url: string;
  /**
   * material is the method-specific input:
   *   - spki-pin: SHA-256 SPKI hash, hex
   *   - dane:     TLSA record, presentation form
   *   - noise:    Noise static public key, hex
   *   - direct:   ignored (transport trust is implicit)
   */
  material: string;
}

/** BootstrapResponse confirms a successful bootstrap. */
export interface BootstrapResponse {
  method: BootstrapMethod;
  server_url: string;
  fingerprint?: string;
}
