/** BackupInfo describes one backup. */
export interface BackupInfo {
    backup_id: string;
    /** The backup's type ('full' or 'incremental') as the server reports it; the server keeps no user label. */
    name?: string;
    /** Text the server generates from the creation time. */
    description?: string;
    timestamp: number;
    size: number;
    status: string;
    shards?: number[];
}
/** BackupListOptions bounds the backup listing. */
export interface BackupListOptions {
    /** The server returns at most `limit` backups (default 100). */
    limit?: number;
    /**
     * Passed to the server, but the backup manager applies only `limit` today,
     * so a caller that needs every backup asks again with `limit` set to
     * `total_count`.
     */
    offset?: number;
}
/** BackupListResponse enumerates backups. */
export interface BackupListResponse {
    backups: BackupInfo[];
    /** Number of backups stored; `backups` can hold fewer when a record's metadata is unreadable. */
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
/**
 * BackupsClient lists, restores, verifies and deletes backups. Maps to
 * /api/v1/backups/* REST endpoints. It has no create until the daemon's
 * backup service captures the cluster's data.
 */
export interface BackupsClient {
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
//# sourceMappingURL=backups.d.ts.map