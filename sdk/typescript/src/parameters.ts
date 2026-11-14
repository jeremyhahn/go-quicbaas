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
// Parameter store domain types.
//
// Mirrors pkg/api/rest/handlers_parameters.go (/api/v1/parameters/*) and
// pkg/interfaces/parameter_store.go. Every `value` field is a base64 string
// and every time is an RFC 3339 string.
//
// Routing note: a parameter path such as "/app/db/host" goes in the URL
// without its leading '/', each segment encodeURIComponent'd and joined by
// '/' (/api/v1/parameters/app/db/host); the server URL-decodes the wildcard
// and adds the leading '/' back. The batch and search verbs are POSTs to
// fixed sub-routes (/batch/get, /batch/set, /batch/delete, /search).
// ---------------------------------------------------------------------------

/** ParameterValue is one parameter store entry. */
export interface ParameterValue {
  path: string;
  /** Base64-encoded value; absent when listed without values. */
  value?: string;
  type: string;
  description?: string;
  tags?: Record<string, string>;
  is_encrypted: boolean;
  kms_key_id?: string;
  expires_at?: string;
  audit_enabled: boolean;
  created_at: string;
  updated_at: string;
  created_by?: string;
  updated_by?: string;
}

/** ParameterGetOptions tunes a single-parameter read. */
export interface ParameterGetOptions {
  /** Set false to receive an encrypted value undecrypted. */
  withDecryption?: boolean;
}

/** GetParameterResponse returns one parameter. */
export interface GetParameterResponse {
  parameter: ParameterValue;
  timestamp: string;
}

/** SetParameterRequest creates or updates a parameter. */
export interface SetParameterRequest {
  /** Base64-encoded value. */
  value: string;
  type?: string;
  description?: string;
  kms_key_id?: string;
  tags?: Record<string, string>;
  ttl_seconds?: number;
  audit_enabled?: boolean;
  overwrite?: boolean;
}

/** SetParameterResponse confirms a write. */
export interface SetParameterResponse {
  path: string;
  created: boolean;
  timestamp: string;
}

/** DeleteParameterResponse confirms a delete. */
export interface DeleteParameterResponse {
  path: string;
  deleted: boolean;
  timestamp: string;
}

/** ParameterListOptions filters and paginates a listing; only those given are sent. */
export interface ParameterListOptions {
  prefix?: string;
  recursive?: boolean;
  withValues?: boolean;
  withDecryption?: boolean;
  cursor?: string;
  limit?: number;
}

/** ListParametersResponse enumerates parameters. */
export interface ListParametersResponse {
  parameters: ParameterValue[];
  next_cursor?: string;
  has_more: boolean;
  total_count?: number;
  timestamp: string;
}

/** GetParametersRequest reads several parameters at once. */
export interface GetParametersRequest {
  paths: string[];
  with_decryption?: boolean;
}

/** GetParametersResponse returns the parameters found and a per-path error map. */
export interface GetParametersResponse {
  parameters: ParameterValue[];
  errors?: Record<string, string>;
  timestamp: string;
}

/** ParameterBatchSetEntry is one write in a batch set. */
export interface ParameterBatchSetEntry {
  path: string;
  /** Base64-encoded value. */
  value: string;
  type?: string;
  description?: string;
  kms_key_id?: string;
  tags?: Record<string, string>;
  ttl_seconds?: number;
  audit_enabled?: boolean;
}

/** SetParametersRequest writes several parameters at once. */
export interface SetParametersRequest {
  parameters: ParameterBatchSetEntry[];
}

/** ParameterSetResult is the outcome of one batch write. */
export interface ParameterSetResult {
  path: string;
  created: boolean;
  success: boolean;
  error?: string;
}

/** SetParametersResponse reports every batch write. */
export interface SetParametersResponse {
  results: ParameterSetResult[];
  timestamp: string;
}

/** ParameterDeleteResult is the outcome of one batch delete. */
export interface ParameterDeleteResult {
  path: string;
  deleted: boolean;
  error?: string;
}

/** DeleteParametersResponse reports every batch delete. */
export interface DeleteParametersResponse {
  results: ParameterDeleteResult[];
  deleted_count: number;
  timestamp: string;
}

/** ParameterSearchByTagsRequest finds parameters carrying every given tag. */
export interface ParameterSearchByTagsRequest {
  tags: Record<string, string>;
  prefix?: string;
  with_values?: boolean;
  with_decryption?: boolean;
  limit?: number;
  cursor?: string;
}

/** ParameterSearchByTagsResponse returns the matching parameters. */
export interface ParameterSearchByTagsResponse {
  parameters: ParameterValue[];
  next_cursor?: string;
  has_more: boolean;
  timestamp: string;
}

// ---------------------------------------------------------------------------
// ParametersClient interface
// ---------------------------------------------------------------------------

/**
 * ParametersClient reads, writes, lists, searches and batches parameter
 * store entries. Maps to /api/v1/parameters/* REST endpoints.
 */
export interface ParametersClient {
  /** get reads one parameter. GET /api/v1/parameters/{path} */
  get(path: string, opts?: ParameterGetOptions): Promise<GetParameterResponse>;

  /** set creates or updates one parameter. PUT /api/v1/parameters/{path} */
  set(path: string, req: SetParameterRequest): Promise<SetParameterResponse>;

  /** delete removes one parameter. DELETE /api/v1/parameters/{path} */
  delete(path: string): Promise<DeleteParameterResponse>;

  /** list enumerates parameters. GET /api/v1/parameters/ */
  list(opts?: ParameterListOptions): Promise<ListParametersResponse>;

  /** getBatch reads several parameters. POST /api/v1/parameters/batch/get */
  getBatch(req: GetParametersRequest): Promise<GetParametersResponse>;

  /** setBatch writes several parameters. POST /api/v1/parameters/batch/set */
  setBatch(req: SetParametersRequest): Promise<SetParametersResponse>;

  /** deleteBatch removes several parameters. POST /api/v1/parameters/batch/delete */
  deleteBatch(paths: string[]): Promise<DeleteParametersResponse>;

  /** searchByTags finds parameters by tag. POST /api/v1/parameters/search */
  searchByTags(req: ParameterSearchByTagsRequest): Promise<ParameterSearchByTagsResponse>;
}
