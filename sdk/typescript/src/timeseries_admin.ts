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
// Time-series batch-write and retention-policy types.
//
// Mirrors pkg/api/rest/timeseries_handlers.go and timeseries_types.go
// (POST /api/v1/timeseries/write-batch and
// GET|PUT /api/v1/timeseries/retention/{metric}).
//
// Wire note: every reply is 200 JSON, and every time is an RFC 3339 string.
// The metric in a retention path is encoded with encodeURIComponent, so a
// metric name containing '/' stays one path segment.
// ---------------------------------------------------------------------------

/** TSBatchPoint is one point of a batch write; metric and timestamp are required. */
export interface TSBatchPoint {
  metric: string;
  tags?: Record<string, string>;
  value: number;
  /** RFC 3339 timestamp. */
  timestamp: string;
}

/** TSWriteBatchResponse is the reply to POST /api/v1/timeseries/write-batch. */
export interface TSWriteBatchResponse {
  success: boolean;
  points_written: number;
  error?: string;
  timestamp: string;
}

/** TSRetentionPolicy is the retention policy of one metric. */
export interface TSRetentionPolicy {
  duration_seconds: number;
  resolution_seconds?: number;
  aggregation?: string;
}

/** TSRetentionPolicyResponse is the reply to GET /api/v1/timeseries/retention/{metric}. */
export interface TSRetentionPolicyResponse {
  metric: string;
  policy: TSRetentionPolicy | null;
  error?: string;
  timestamp: string;
}

/** TSSetRetentionPolicyResponse is the reply to PUT /api/v1/timeseries/retention/{metric}. */
export interface TSSetRetentionPolicyResponse {
  success: boolean;
  error?: string;
  timestamp: string;
}
