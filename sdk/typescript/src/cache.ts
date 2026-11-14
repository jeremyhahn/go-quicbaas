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
// Cache domain types.
//
// Mirrors pkg/api/rest/handlers_cache.go (/api/v1/cache/*): the server's L1
// cache statistics, a clear of every entry, and its configuration. Every
// route answers 200 with a JSON body.
// ---------------------------------------------------------------------------

/** CacheStatsResponse is the L1 cache's counters (GET /api/v1/cache/stats). */
export interface CacheStatsResponse {
  enabled: boolean;
  l1_size: number;
  l1_capacity: number;
  l1_hits: number;
  l1_misses: number;
  l1_hit_rate: number;
  l1_evictions: number;
}

/** CacheClearResponse reports a clear (POST /api/v1/cache/clear). */
export interface CacheClearResponse {
  success: boolean;
  entries_cleared: number;
}

/** CacheConfigResponse is the cache's configuration (GET /api/v1/cache/config). */
export interface CacheConfigResponse {
  enabled: boolean;
  l1_size: number;
  l1_ttl_seconds: number;
  cleanup_interval_seconds: number;
}

/**
 * CacheConfigUpdateRequest updates the cache's configuration
 * (PUT /api/v1/cache/config). Only the fields given are sent.
 */
export interface CacheConfigUpdateRequest {
  l1_size?: number;
  l1_ttl_seconds?: number;
}

/** CacheConfigUpdateResponse reports a configuration update. */
export interface CacheConfigUpdateResponse {
  success: boolean;
  message: string;
}

/** CacheClient reads and manages the server's cache (/api/v1/cache/*). */
export interface CacheClient {
  /** stats returns the L1 cache's counters. */
  stats(): Promise<CacheStatsResponse>;
  /** clear removes every cache entry. */
  clear(): Promise<CacheClearResponse>;
  /** getConfig returns the cache's configuration. */
  getConfig(): Promise<CacheConfigResponse>;
  /** updateConfig changes the fields given in req. */
  updateConfig(req: CacheConfigUpdateRequest): Promise<CacheConfigUpdateResponse>;
}
