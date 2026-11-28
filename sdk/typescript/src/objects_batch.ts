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
// Batch object storage domain types.
//
// Mirrors the batch routes of pkg/api/rest/handlers_objectstorage.go
// (POST /api/v1/objects/batch, /batch/get and /batch/delete). Every reply is
// 200 JSON carrying per-key results plus success/failed counts.
//
// Wire note: `value` is the object's content as a plain string, not base64,
// because the server stores []byte(value). `timestamp` is a unix number.
// ---------------------------------------------------------------------------

// ObjectsBatchPutOperation is one object to store in a batch put.
export interface ObjectsBatchPutOperation {
  key: string;
  value: string;
  content_type?: string;
  tags?: Record<string, string>;
  target_backend?: string;
}

// ObjectsBatchPutResult is the outcome of one batch put operation.
export interface ObjectsBatchPutResult {
  key: string;
  object_id?: number;
  success: boolean;
  error?: string;
  version?: number;
  size?: number;
}

// ObjectsBatchPutResponse is the reply of POST /api/v1/objects/batch.
export interface ObjectsBatchPutResponse {
  results: ObjectsBatchPutResult[];
  success: number;
  failed: number;
  timestamp: number;
}

// ObjectsBatchGetOptions tunes a batch get.
export interface ObjectsBatchGetOptions {
  includeMetadata?: boolean;
}

// ObjectsBatchGetResult is the outcome of one key in a batch get.
export interface ObjectsBatchGetResult {
  key: string;
  value?: string;
  content_type?: string;
  size?: number;
  version?: number;
  tags?: Record<string, string>;
  success: boolean;
  error?: string;
}

// ObjectsBatchGetResponse is the reply of POST /api/v1/objects/batch/get.
export interface ObjectsBatchGetResponse {
  results: ObjectsBatchGetResult[];
  success: number;
  failed: number;
  timestamp: number;
}

// ObjectsBatchDeleteResult is the outcome of one key in a batch delete.
export interface ObjectsBatchDeleteResult {
  key: string;
  success: boolean;
  error?: string;
  size?: number;
}

// ObjectsBatchDeleteResponse is the reply of POST /api/v1/objects/batch/delete.
export interface ObjectsBatchDeleteResponse {
  results: ObjectsBatchDeleteResult[];
  success: number;
  failed: number;
  timestamp: number;
}
