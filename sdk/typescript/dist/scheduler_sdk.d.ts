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
export interface PlacementNodeFacts {
    node_id: string;
    capacity?: Record<string, number>;
    labels?: Record<string, string>;
}
export interface PlacementNodeStatus {
    unschedulable: boolean;
    draining: boolean;
}
export interface PlacementNodeInfo {
    facts: PlacementNodeFacts;
    status: PlacementNodeStatus;
    lease_id: number | string;
    ttl_seconds: number;
    allocated?: Record<string, number>;
    placements: number;
    registered_at: string;
}
export interface RegisterNodeRequest {
    facts: PlacementNodeFacts;
    ttl_seconds: number;
}
export interface PlacementConstraint {
    key: string;
    operator: 'In' | 'NotIn' | 'Exists' | 'DoesNotExist';
    values?: string[];
}
export interface PlacementAffinityTerm {
    group: string;
    required?: boolean;
    weight?: number;
}
export interface PlacementSpreadConstraint {
    topology_key: string;
    max_skew: number;
    required?: boolean;
}
export interface PlacementRequest {
    workload_id: string;
    group?: string;
    labels?: Record<string, string>;
    resources?: Record<string, number>;
    constraints?: PlacementConstraint[];
    affinity?: PlacementAffinityTerm[];
    anti_affinity?: PlacementAffinityTerm[];
    spread?: PlacementSpreadConstraint;
    strategy?: 'binpack' | 'spread';
}
export interface Placement {
    workload_id: string;
    group?: string;
    node_id: string;
    resources?: Record<string, number>;
    labels?: Record<string, string>;
    score: number;
    created_at: string;
    orphaned?: boolean;
}
export interface PlaceResult {
    placement: Placement;
    existing: boolean;
}
export interface PlacementFilter {
    node_id?: string;
    group?: string;
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
    registerNode(req: RegisterNodeRequest): Promise<PlacementNodeInfo>;
    renewNode(nodeId: string): Promise<void>;
    deregisterNode(nodeId: string): Promise<void>;
    getNode(nodeId: string): Promise<PlacementNodeInfo>;
    listNodes(): Promise<PlacementNodeInfo[]>;
    cordonNode(nodeId: string): Promise<PlacementNodeInfo>;
    uncordonNode(nodeId: string): Promise<PlacementNodeInfo>;
    drainNode(nodeId: string): Promise<Placement[]>;
    place(req: PlacementRequest): Promise<PlaceResult>;
    release(workloadId: string): Promise<void>;
    getPlacement(workloadId: string): Promise<Placement>;
    listPlacements(filter?: PlacementFilter): Promise<Placement[]>;
}
//# sourceMappingURL=scheduler_sdk.d.ts.map