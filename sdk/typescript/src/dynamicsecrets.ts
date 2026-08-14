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
// Dynamic Secrets domain types.
//
// Mirrors pkg/api/rest/handlers_dynamic_secrets.go (/api/v1/secrets/dynamic/*)
// and sdk/go/client/dynamicsecrets. DynamicSecretsClient manages role
// templates that generate short-lived, leased credentials on demand
// (database users, API keys, etc.) rather than static long-lived secrets.
//
// Wire note: like CAClient/TrustClient/CryptoClient, the REST responses here
// carry Go int64 unix-second timestamps (created_at, deleted_at, lease
// expiry/renewal/revocation, timestamp). RestDynamicSecretsClient converts
// all of them to ISO-8601 strings before returning to callers.
//
// SECURITY NOTE: generateCredential's response contains the only copy of
// the generated secret material (username/password or provider-specific
// secret map) the server will ever return — it is not retrievable again.
// The DynamicSecretsView must render it once, in a dismissible dialog, and
// must not persist it in component state beyond that dialog's lifetime.
// ---------------------------------------------------------------------------

/** DynamicSecretRole describes a credential-generation role template. */
export interface DynamicSecretRole {
  name: string;
  generator_type: string;
  config?: Record<string, string>;
  /** default_ttl is a Go duration string, e.g. "1h0m0s". */
  default_ttl: string;
  /** max_ttl is a Go duration string, e.g. "24h0m0s". */
  max_ttl: string;
  created_at?: string;
  timestamp: string;
}

/** CreateRoleRequest defines a new credential-generation role. */
export interface CreateRoleRequest {
  name: string;
  generator_type: string;
  config?: Record<string, string>;
  /** default_ttl must be a Go duration string, e.g. "1h". */
  default_ttl: string;
  /** max_ttl must be a Go duration string, e.g. "24h". */
  max_ttl: string;
}

/** ListRolesResponse enumerates credential-generation roles. */
export interface ListRolesResponse {
  roles: DynamicSecretRole[];
  count: number;
  timestamp: string;
}

/** DeleteRoleResponse confirms role deletion. */
export interface DeleteRoleResponse {
  success: boolean;
  deleted_at: string;
  timestamp: string;
}

/** GenerateCredentialRequest optionally overrides the role's default lease TTL. */
export interface GenerateCredentialRequest {
  /** ttl is a Go duration string, e.g. "30m". */
  ttl?: string;
}

/**
 * GenerateCredentialResponse is the one-time mint response for a generated
 * credential. This is the only time the secret material is returned by the
 * server — display it once and discard.
 */
export interface GenerateCredentialResponse {
  credential_id: string;
  username?: string;
  password?: string;
  secret?: Record<string, string>;
  lease_id: string;
  /** lease_ttl is a Go duration string, e.g. "1h0m0s". */
  lease_ttl: string;
  lease_expiry: string;
  renewable: boolean;
  timestamp: string;
}

/** RenewLeaseRequest optionally overrides the lease's default renewal increment. */
export interface RenewLeaseRequest {
  /** increment is a Go duration string, e.g. "30m". */
  increment?: string;
}

/** RenewLeaseResponse confirms a lease renewal. */
export interface RenewLeaseResponse {
  lease_id: string;
  lease_ttl: string;
  lease_expiry: string;
  renew_count: number;
  renewed_at: string;
  timestamp: string;
}

/** RevokeLeaseResponse confirms immediate lease revocation. */
export interface RevokeLeaseResponse {
  success: boolean;
  revoked_at: string;
  timestamp: string;
}

/** LeaseInfo is one row in a ListLeases response. */
export interface LeaseInfo {
  lease_id: string;
  path: string;
  ttl: string;
  expiry: string;
  renewable: boolean;
  renew_count: number;
  metadata?: Record<string, string>;
}

/** ListLeasesResponse enumerates active leases. */
export interface ListLeasesResponse {
  leases: LeaseInfo[];
  count: number;
  timestamp: string;
}

// ---------------------------------------------------------------------------
// DynamicSecretsClient interface
// ---------------------------------------------------------------------------

/**
 * DynamicSecretsClient exposes dynamic secrets role administration,
 * on-demand credential generation, and lease lifecycle management. Maps to
 * /api/v1/secrets/dynamic/* REST endpoints.
 */
export interface DynamicSecretsClient {
  /** createRole defines a new credential-generation role. POST /api/v1/secrets/dynamic/roles */
  createRole(req: CreateRoleRequest): Promise<DynamicSecretRole>;

  /** getRole retrieves a role by name. GET /api/v1/secrets/dynamic/roles/{name} */
  getRole(name: string): Promise<DynamicSecretRole>;

  /** listRoles enumerates roles, optionally filtered by name prefix. GET /api/v1/secrets/dynamic/roles */
  listRoles(prefix?: string): Promise<ListRolesResponse>;

  /** deleteRole removes a role. DELETE /api/v1/secrets/dynamic/roles/{name} */
  deleteRole(name: string): Promise<DeleteRoleResponse>;

  /**
   * generateCredential mints a new short-lived credential from a role. The
   * secret material in the response is never retrievable again.
   * POST /api/v1/secrets/dynamic/credentials/{role}
   */
  generateCredential(roleName: string, req?: GenerateCredentialRequest): Promise<GenerateCredentialResponse>;

  /** renewLease extends a credential lease's expiration. POST /api/v1/secrets/dynamic/leases/{id}/renew */
  renewLease(leaseId: string, req?: RenewLeaseRequest): Promise<RenewLeaseResponse>;

  /** revokeLease revokes a credential lease immediately. DELETE /api/v1/secrets/dynamic/leases/{id} */
  revokeLease(leaseId: string): Promise<RevokeLeaseResponse>;

  /** listLeases enumerates active leases, optionally filtered by path prefix. GET /api/v1/secrets/dynamic/leases */
  listLeases(prefix?: string): Promise<ListLeasesResponse>;
}
