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
//# sourceMappingURL=timeseries_admin.d.ts.map