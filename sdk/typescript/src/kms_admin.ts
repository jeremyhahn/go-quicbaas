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
// KMS key administration domain types.
//
// Mirrors pkg/api/rest/handlers_kms_admin.go and handlers_kms.go: the key
// metadata, key policy and rotation policy routes under
// /api/v1/kms/keys/{name}/*. KMSAdminClient is distinct from KMSClient, which
// covers key creation and the cryptographic operations.
//
// Wire note: `timestamp`, `created_at`, `updated_at`, `last_used_at` and
// `next_rotation_unix` arrive as Unix seconds (numbers). A key policy's
// `expires_at` is an RFC 3339 string or null.
// ---------------------------------------------------------------------------

/** Metadata of a named KMS key. */
export interface KMSKeyMetadata {
  key_id: string;
  name?: string;
  description?: string;
  algorithm: string;
  status: string;
  usage?: string[];
  version: number;
  exportable: boolean;
  deletion_protection: boolean;
  /** Unix seconds. */
  created_at: number;
  /** Unix seconds. */
  updated_at: number;
  /** Unix seconds. */
  last_used_at?: number;
  tags?: Record<string, string>;
  owner?: string;
}

/** Body of PUT /api/v1/kms/keys/{name}/metadata. */
export interface UpdateKMSKeyMetadataRequest {
  name?: string;
  description?: string;
  tags?: Record<string, string>;
  remove_tags?: string[];
}

/** Response of GET and PUT /api/v1/kms/keys/{name}/metadata. */
export interface KMSKeyMetadataResponse {
  metadata: KMSKeyMetadata;
  /** Unix seconds. */
  timestamp: number;
}

/** Access policy of a named KMS key, as the server reports it. */
export interface KMSKeyPolicy {
  key_id: string;
  version: number;
  allowed_operations: string[];
  allowed_principals: string[];
  denied_principals: string[];
  /** RFC 3339, or null when the policy never expires. */
  expires_at: string | null;
  rotation_required: boolean;
  max_usage_count: number | null;
  require_mfa: boolean;
  /** Unix seconds. */
  updated_at: number;
}

/** Access policy to set on a named KMS key. */
export interface KMSKeyPolicyInput {
  version: number;
  allowed_operations: string[];
  allowed_principals: string[];
  denied_principals: string[];
  /** RFC 3339, or null when the policy never expires. */
  expires_at: string | null;
  rotation_required: boolean;
  max_usage_count: number | null;
  require_mfa: boolean;
}

/** Response of GET /api/v1/kms/keys/{name}/policy. */
export interface GetKMSKeyPolicyResponse {
  policy: KMSKeyPolicy;
  /** Unix seconds. */
  timestamp: number;
}

/** Response of PUT /api/v1/kms/keys/{name}/policy. */
export interface SetKMSKeyPolicyResponse {
  key_id: string;
  policy_version: number;
  /** Unix seconds. */
  timestamp: number;
}

/** Rotation policy of a named KMS key. */
export interface KMSRotationPolicy {
  enabled: boolean;
  interval_seconds: number;
  /** Unix seconds. */
  next_rotation_unix?: number;
  retain_versions?: number;
  auto_rotate: boolean;
}

/** Body of PUT /api/v1/kms/keys/{name}/rotation-policy. */
export interface KMSRotationPolicyInput {
  enabled: boolean;
  interval_seconds: number;
  retain_versions?: number;
  auto_rotate: boolean;
}

/** Response of GET /api/v1/kms/keys/{name}/rotation-policy. */
export interface GetKMSRotationPolicyResponse {
  key_id: string;
  /** Absent when the key has no rotation policy. */
  policy?: KMSRotationPolicy;
  /** Unix seconds. */
  timestamp: number;
}

/** Response of PUT /api/v1/kms/keys/{name}/rotation-policy. */
export interface SetKMSRotationPolicyResponse {
  key_id: string;
  policy: KMSRotationPolicy;
  /** Unix seconds. */
  timestamp: number;
}

/** Response of DELETE /api/v1/kms/keys/{name}/rotation-policy. */
export interface DeleteKMSRotationPolicyResponse {
  key_id: string;
  /** Unix seconds. */
  timestamp: number;
}

/**
 * Administers named KMS keys: metadata, access policy and rotation policy.
 * Paths: /api/v1/kms/keys/{name}/{metadata,policy,rotation-policy}.
 */
export interface KMSAdminClient {
  getKeyMetadata(name: string): Promise<KMSKeyMetadataResponse>;
  updateKeyMetadata(name: string, req: UpdateKMSKeyMetadataRequest): Promise<KMSKeyMetadataResponse>;
  getKeyPolicy(name: string): Promise<GetKMSKeyPolicyResponse>;
  setKeyPolicy(name: string, policy: KMSKeyPolicyInput): Promise<SetKMSKeyPolicyResponse>;
  getKeyRotationPolicy(name: string): Promise<GetKMSRotationPolicyResponse>;
  setKeyRotationPolicy(name: string, req: KMSRotationPolicyInput): Promise<SetKMSRotationPolicyResponse>;
  deleteKeyRotationPolicy(name: string): Promise<DeleteKMSRotationPolicyResponse>;
}
