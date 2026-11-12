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
// Backup domain types.
//
// Mirrors pkg/api/rest/handlers_backup.go (/api/v1/backups/*). BackupsClient
// creates, lists, restores, verifies and deletes cluster backups.
//
// Wire note: `timestamp` is the Go int64 the server sends (unix seconds or
// nanoseconds), passed through unchanged as a number. Shard IDs are numbers.
// The backup ID is encoded with encodeURIComponent in every path.
// ---------------------------------------------------------------------------

/** BackupCreateRequest requests a new backup. */
export interface BackupCreateRequest {
  name?: string;
  description?: string;
  compression?: string;
  encryption?: string;
  include_shards?: number[];
}

/** BackupCreateResponse confirms a backup was started. */
export interface BackupCreateResponse {
  backup_id: string;
  timestamp: number;
  size: number;
  status: string;
}

/** BackupInfo describes one backup. */
export interface BackupInfo {
  backup_id: string;
  name?: string;
  description?: string;
  timestamp: number;
  size: number;
  status: string;
  shards?: number[];
}

/** BackupListOptions paginates the backup listing. */
export interface BackupListOptions {
  limit?: number;
  offset?: number;
}

/** BackupListResponse enumerates backups. */
export interface BackupListResponse {
  backups: BackupInfo[];
  total_count: number;
}

/** BackupRestoreRequest selects the shards a restore targets. */
export interface BackupRestoreRequest {
  target_shards?: number[];
}

/** BackupRestoreResponse confirms a restore job was started. */
export interface BackupRestoreResponse {
  job_id: string;
  status: string;
  progress: number;
}

/** BackupDeleteResponse confirms a backup was deleted. */
export interface BackupDeleteResponse {
  success: boolean;
}

/** BackupVerifyResponse reports a backup's integrity check. */
export interface BackupVerifyResponse {
  valid: boolean;
  checksum_match: boolean;
  issues?: string[];
}

/** BackupStatusResponse reports a backup's current integrity status. */
export interface BackupStatusResponse {
  backup_id: string;
  valid: boolean;
  checksum_match: boolean;
  issues?: string[];
}

// ---------------------------------------------------------------------------
// BackupsClient interface
// ---------------------------------------------------------------------------

/**
 * BackupsClient exposes backup management. Maps to /api/v1/backups/* REST
 * endpoints.
 */
export interface BackupsClient {
  /** create starts a new backup. POST /api/v1/backups */
  create(req?: BackupCreateRequest): Promise<BackupCreateResponse>;

  /** list enumerates backups. GET /api/v1/backups */
  list(opts?: BackupListOptions): Promise<BackupListResponse>;

  /** restore restores a backup. POST /api/v1/backups/{id}/restore */
  restore(backupID: string, req?: BackupRestoreRequest): Promise<BackupRestoreResponse>;

  /** delete removes a backup. DELETE /api/v1/backups/{id} */
  delete(backupID: string): Promise<BackupDeleteResponse>;

  /** verify checks a backup's integrity. POST /api/v1/backups/{id}/verify */
  verify(backupID: string): Promise<BackupVerifyResponse>;

  /** status reports a backup's integrity status. GET /api/v1/backups/{id}/status */
  status(backupID: string): Promise<BackupStatusResponse>;
}
