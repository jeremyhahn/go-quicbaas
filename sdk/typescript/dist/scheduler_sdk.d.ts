export interface SchedulerJob {
    id: number;
    name: string;
    description?: string;
    type: string;
    schedule?: string;
    handler: string;
    payload?: unknown;
    priority: number;
    timeout: number;
    max_retries: number;
    status: string;
    last_run?: string;
    next_run?: string;
    run_count: number;
    fail_count: number;
    last_error?: string;
    labels?: Record<string, string>;
    created_at: string;
    updated_at: string;
    owner_type?: string;
    owner_id?: string;
    target_tenant_id?: string;
}
export interface ListJobsRequest {
    status?: string;
    handler?: string;
    limit?: number;
    cursor?: string;
    owner_type?: string;
    owner_id?: string;
}
export interface ListJobsResponse {
    jobs: SchedulerJob[];
    next_cursor: string;
    has_more: boolean;
    total_count: number;
    timestamp: string;
}
export interface CreateJobRequest {
    name: string;
    description?: string;
    type: string;
    schedule?: string;
    handler: string;
    payload?: unknown;
    priority?: number;
    timeout_seconds?: number;
    max_retries?: number;
    labels?: Record<string, string>;
    owner_type?: string;
    owner_id?: string;
    target_tenant_id?: string;
}
export interface CreateJobResponse {
    job: SchedulerJob;
    timestamp: string;
}
export interface DeleteJobResponse {
    job_id: number;
    deleted: boolean;
    timestamp: string;
}
export interface TriggerJobResponse {
    job_id: number;
    status: string;
    timestamp: string;
}
export interface SchedulerStatus {
    running: boolean;
    stats?: {
        job_count: number;
        running_jobs: number;
        success_count: number;
        failure_count: number;
        started_at: string;
    };
    timestamp: string;
}
export interface SchedulerClient {
    listJobs(req?: ListJobsRequest): Promise<ListJobsResponse>;
    createJob(req: CreateJobRequest): Promise<CreateJobResponse>;
    deleteJob(id: number): Promise<DeleteJobResponse>;
    triggerJob(id: number): Promise<TriggerJobResponse>;
    pauseJob(id: number): Promise<void>;
    resumeJob(id: number): Promise<void>;
    updateJob(id: number, req: Partial<CreateJobRequest>): Promise<SchedulerJob>;
    getStatus(): Promise<SchedulerStatus>;
}
//# sourceMappingURL=scheduler_sdk.d.ts.map