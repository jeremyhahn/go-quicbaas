/** DEKAlgorithm restricts DEK creation to symmetric AEAD ciphers. */
export type DEKAlgorithm = 'AES-128-GCM' | 'AES-192-GCM' | 'AES-256-GCM' | 'CHACHA20-POLY1305' | 'XCHACHA20-POLY1305';
/** DEKMetadata describes a single Data Encryption Key. */
export interface DEKMetadata {
    path: string;
    algorithm: string;
    version: number;
    status: string;
    is_default: boolean;
    created_at: string;
    rotated_at?: string;
    usage_count: number;
    bytes_used: number;
    max_usage_count?: number;
    max_bytes_used?: number;
    warning_percent?: number;
    description?: string;
    backend_type?: string;
}
/** CreateDEKRequest requests creation of a new Data Encryption Key. */
export interface CreateDEKRequest {
    path: string;
    algorithm?: DEKAlgorithm;
    description?: string;
    max_operations?: number;
    max_bytes?: number;
    warning_percent?: number;
    tags?: Record<string, string>;
}
/** CreateDEKResponse confirms DEK creation. */
export interface CreateDEKResponse {
    dek: DEKMetadata;
    timestamp: string;
}
/** GetDEKResponse returns a single DEK's metadata. */
export interface GetDEKResponse {
    dek: DEKMetadata;
    timestamp: string;
}
/** ListDEKsRequest filters/paginates the DEK listing. */
export interface ListDEKsRequest {
    prefix?: string;
    recursive?: boolean;
    limit?: number;
    cursor?: string;
}
/** ListDEKsResponse enumerates DEKs matching the filter. */
export interface ListDEKsResponse {
    deks: DEKMetadata[];
    next_cursor?: string;
    has_more: boolean;
    total_count: number;
    timestamp: string;
}
/** RotateDEKRequest requests a new DEK version. */
export interface RotateDEKRequest {
    background?: boolean;
    retain_old_version?: boolean;
}
/** RotateDEKResponse confirms DEK rotation. */
export interface RotateDEKResponse {
    path: string;
    old_version: number;
    new_version: number;
    timestamp: string;
}
/** DeleteDEKRequest requests DEK deletion, optionally migrating data first. */
export interface DeleteDEKRequest {
    migrate_to?: string;
    force?: boolean;
}
/** DeleteDEKResponse confirms DEK deletion. */
export interface DeleteDEKResponse {
    path: string;
    deleted: boolean;
    migrated_count?: number;
    timestamp: string;
}
/** ReencryptionProgress tracks a background re-encryption operation. */
export interface ReencryptionProgress {
    status: string;
    in_progress: boolean;
    total_keys: number;
    processed_keys: number;
    failed_keys: number;
    percent_complete: number;
    started_at?: string;
    estimated_completion?: string;
    error_message?: string;
}
/** GetDEKStatusResponse returns DEK metadata plus any in-flight re-encryption progress. */
export interface GetDEKStatusResponse {
    dek: DEKMetadata;
    reencryption_progress?: ReencryptionProgress;
    timestamp: string;
}
/** SetDefaultDEKResponse confirms a default DEK change. */
export interface SetDefaultDEKResponse {
    path: string;
    previous_default?: string;
    timestamp: string;
}
/** SetDEKSafetyRequest configures AEAD safety limits for a DEK. */
export interface SetDEKSafetyRequest {
    max_operations?: number;
    max_bytes?: number;
    warning_percent?: number;
}
/** DEKSafetyConfig is the safety configuration currently applied to a DEK. */
export interface DEKSafetyConfig {
    max_operations: number;
    max_bytes: number;
    warning_percent: number;
}
/** SetDEKSafetyResponse confirms safety limits were applied. */
export interface SetDEKSafetyResponse {
    path: string;
    config: DEKSafetyConfig;
    timestamp: string;
}
/** ClearDEKSafetyResponse confirms custom safety limits were removed. */
export interface ClearDEKSafetyResponse {
    path: string;
    cleared: boolean;
    timestamp: string;
}
/** StartReencryptionRequest begins background re-encryption of data protected by a DEK. */
export interface StartReencryptionRequest {
    from_version?: number;
    to_version?: number;
    batch_size?: number;
    concurrency?: number;
    throttle_rate?: number;
    force?: boolean;
}
/** StartReencryptionResponse confirms a re-encryption operation was started. */
export interface StartReencryptionResponse {
    operation_id: string;
    path?: string;
    started: boolean;
    estimated_items: number;
    timestamp: string;
    started_at?: string;
    total_entities?: number;
    estimated_completion?: string;
}
/** GetReencryptionProgressResponse reports the current state of a re-encryption operation. */
export interface GetReencryptionProgressResponse {
    path?: string;
    operation_id?: string;
    progress?: ReencryptionProgress;
    status: string;
    error?: string;
    timestamp: string;
}
/**
 * DEKClient exposes Data Encryption Key lifecycle management: creation,
 * lookup, listing, rotation, deletion, default selection, AEAD safety
 * limits, and background re-encryption. Maps to /api/v1/kms/deks/* REST
 * endpoints.
 */
export interface DEKClient {
    /** createDEK creates a new DEK at the given path. POST /api/v1/kms/deks */
    createDEK(req: CreateDEKRequest): Promise<CreateDEKResponse>;
    /** getDEK retrieves DEK metadata by path. GET /api/v1/kms/deks/{path} */
    getDEK(path: string, version?: number): Promise<GetDEKResponse>;
    /** listDEKs enumerates DEKs matching an optional prefix filter. GET /api/v1/kms/deks */
    listDEKs(req?: ListDEKsRequest): Promise<ListDEKsResponse>;
    /** rotateDEK creates a new version of an existing DEK. POST /api/v1/kms/deks/{path}/rotate */
    rotateDEK(path: string, req?: RotateDEKRequest): Promise<RotateDEKResponse>;
    /** deleteDEK removes a DEK (requires force or migration). DELETE /api/v1/kms/deks/{path} */
    deleteDEK(path: string, req?: DeleteDEKRequest): Promise<DeleteDEKResponse>;
    /** getDEKStatus retrieves detailed DEK status including AEAD usage. GET /api/v1/kms/deks/{path}/status */
    getDEKStatus(path: string): Promise<GetDEKStatusResponse>;
    /** setDefaultDEK marks a DEK as the default for its path level. POST /api/v1/kms/deks/{path}/set-default */
    setDefaultDEK(path: string): Promise<SetDefaultDEKResponse>;
    /** setDEKSafety configures AEAD safety limits for a DEK. PUT /api/v1/kms/deks/{path}/safety */
    setDEKSafety(path: string, req: SetDEKSafetyRequest): Promise<SetDEKSafetyResponse>;
    /** clearDEKSafety removes custom AEAD safety limits from a DEK. DELETE /api/v1/kms/deks/{path}/safety */
    clearDEKSafety(path: string): Promise<ClearDEKSafetyResponse>;
    /**
     * startReencryption begins background re-encryption of data protected by
     * this DEK onto a newer version. Long-running and destructive-adjacent —
     * callers must confirm before invoking. POST /api/v1/kms/deks/{path}/reencrypt
     */
    startReencryption(path: string, req?: StartReencryptionRequest): Promise<StartReencryptionResponse>;
    /** getReencryptionProgress returns the current progress of a re-encryption operation. GET /api/v1/kms/deks/{path}/reencrypt */
    getReencryptionProgress(path: string, operationId?: string, verbose?: boolean): Promise<GetReencryptionProgressResponse>;
}
//# sourceMappingURL=dek.d.ts.map