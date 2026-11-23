export interface ObjectsBatchPutOperation {
    key: string;
    value: string;
    content_type?: string;
    tags?: Record<string, string>;
    target_backend?: string;
}
export interface ObjectsBatchPutResult {
    key: string;
    object_id?: number;
    success: boolean;
    error?: string;
    version?: number;
    size?: number;
}
export interface ObjectsBatchPutResponse {
    results: ObjectsBatchPutResult[];
    success: number;
    failed: number;
    timestamp: number;
}
export interface ObjectsBatchGetOptions {
    includeMetadata?: boolean;
}
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
export interface ObjectsBatchGetResponse {
    results: ObjectsBatchGetResult[];
    success: number;
    failed: number;
    timestamp: number;
}
export interface ObjectsBatchDeleteResult {
    key: string;
    success: boolean;
    error?: string;
    size?: number;
}
export interface ObjectsBatchDeleteResponse {
    results: ObjectsBatchDeleteResult[];
    success: number;
    failed: number;
    timestamp: number;
}
//# sourceMappingURL=objects_batch.d.ts.map