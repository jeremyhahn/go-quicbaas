export type JobState = 'initializing' | 'decomposing' | 'scheduling' | 'assigning' | 'executing' | 'validating' | 'reviewing' | 're-delegating' | 'completing' | 'cleanup' | 'escalating' | 'completed' | 'failed';
export interface JobAuditEvent {
    timestamp: number;
    type: string;
    details: string;
}
export interface FailedApproach {
    work_item_title: string;
    approach: string;
    reason: string;
    iteration: number;
}
export interface JobMemory {
    successful_approaches: string[];
    failed_approaches: FailedApproach[];
    constraints: string[];
    review_feedback: string[];
}
export interface WorkItem {
    id: string;
    project_id: string;
    title: string;
    description: string;
    priority: number;
    dependencies: string[];
    backend: string;
    required_capabilities: string[];
    required_role: string;
    secret_refs: string[];
    max_retries: number;
    parallelizable: boolean;
    fallback_backend: string;
}
export interface JobConfig {
    tenant_id: string;
    description: string;
    lead_backend: string;
    lead_model: string;
    project_id: string;
    poll_interval: number;
    max_iterations: number;
    workflow_name: string;
}
export interface JobStatus {
    job_id: string;
    tenant_id: string;
    state: JobState;
    iteration: number;
    task_count: number;
    completed: number;
    failed: number;
    description: string;
    started_at: number;
    updated_at: number;
    error: string;
    events: JobAuditEvent[];
    work_items: WorkItem[];
    config: JobConfig | null;
    memory: JobMemory | null;
    workflow_name: string;
}
export interface JobEvent {
    job_id: string;
    type: string;
    state: JobState;
    message: string;
    iteration: number;
    metadata: Record<string, string>;
    timestamp: number;
}
export interface StartJobRequest {
    tenant_id: string;
    description: string;
    workflow_name: string;
    lead_backend: string;
    lead_model: string;
    project_id: string;
    max_iterations: number;
    poll_interval?: string;
    workflow_file?: string;
}
export interface StartJobResponse {
    job_id: string;
    timestamp: number;
}
export type AgentStatus = 'registered' | 'running' | 'idle' | 'offline' | 'error';
export interface AgentRegistration {
    id: string;
    tenant_id: string;
    name: string;
    host: string;
    backend: string;
    model: string;
    agent_type: string;
    status: AgentStatus;
    capabilities: string[];
    container_id: string;
    work_dir: string;
    last_seen: number;
    registered_at: number;
    version: number;
    metadata: Record<string, string>;
}
export type SessionState = 'active' | 'idle' | 'closed' | 'error';
export interface SessionInfo {
    session_id: string;
    project_id: string;
    backend: string;
    model: string;
    state: SessionState;
    created_at: number;
    last_activity: number;
}
export type TaskStatus = 'pending' | 'claimed' | 'in_progress' | 'completed' | 'failed' | 'cancelled';
export interface TaskResult {
    output: string;
    files: string[];
    metadata: Record<string, string>;
}
export interface Task {
    id: string;
    tenant_id: string;
    team_name: string;
    subject: string;
    description: string;
    status: TaskStatus;
    assigned_to: string;
    created_by: string;
    dependencies: string[];
    blocks: string[];
    result: TaskResult | null;
    metadata: Record<string, string>;
    created_at: number;
    updated_at: number;
    claimed_at: number;
    completed_at: number;
    version: number;
}
export type BackendKind = 'agent' | 'model_server';
export interface BackendCapabilities {
    kind: BackendKind;
    persistent_sessions: boolean;
    tool_use: boolean;
    file_access: boolean;
    streaming: boolean;
    acp: boolean;
    team_management: boolean;
}
export interface AgentBackendInfo {
    name: string;
    type: string;
    capabilities: BackendCapabilities;
    default_model: string;
}
export interface SessionHealthResult {
    session_id: string;
    backend: string;
    state: SessionState;
    healthy: boolean;
    error: string;
}
export interface HealthCheckReport {
    timestamp: number;
    total_sessions: number;
    healthy: number;
    unhealthy: number;
    closed: number;
    results: SessionHealthResult[];
}
export type BudgetStatus = 'ok' | 'warning' | 'exceeded';
export interface UsageReport {
    total_tokens: number;
    budget_limit: number;
    percent_used: number;
    by_agent: Record<string, number>;
    last_updated: number;
}
export interface PeerMessage {
    id: string;
    from: string;
    to: string;
    action: string;
    channel: string;
    content: string;
    metadata: Record<string, string>;
    timestamp: number;
    ttl: number;
}
export interface FileLock {
    lock_key: string;
    agent_id: string;
    tenant_id: string;
    project_id: string;
    file_path: string;
    lease_id: string;
    fencing_token: number;
    acquired_at: number;
    isolation: string;
}
export type CommunicationTopology = 'hub_and_spoke' | 'hybrid' | 'peer_to_peer';
export type WorkspaceIsolation = 'shared' | 'worktree';
export type WorkflowMode = 'canned' | 'constrained' | 'autonomous';
export interface AgentRoleSpec {
    role: string;
    backend: string;
    model: string;
    capabilities: string[];
    mcp_scopes: string[];
    count: number;
    workspace_path: string;
    permission_mode: string;
    skip_permissions: boolean;
    env: Record<string, string>;
    sandboxed: boolean;
    container_image: string;
    num_ctx: number;
}
export interface PeerMessagingSettings {
    max_message_size: number;
    max_inbox_size: number;
    overflow_action: string;
    inbox_ttl: string;
}
export interface FileLockSettings {
    lease_ttl: string;
    heartbeat_interval: string;
    max_renewals: number;
}
export interface VectorDBSettings {
    enabled: boolean;
    index_type: string;
    auto_index: boolean;
    triggers: string[];
    gpu_accelerated: boolean;
}
export interface CostControlSettings {
    max_tokens_per_job: number;
    max_tokens_per_agent: number;
    budget_action: string;
}
export interface RecoverySettings {
    auto_restart: boolean;
    max_restarts: number;
    restart_backoff: string;
}
export interface ReviewCriterion {
    name: string;
    validator: string;
    config: string;
    required: boolean;
}
export interface WorkflowSettings {
    max_iterations: number;
    max_parallelism: number;
    poll_interval: string;
    timeout_per_item: string;
    total_timeout: string;
    review_criteria: ReviewCriterion[];
    topology: CommunicationTopology;
    isolation: WorkspaceIsolation;
    peer_messaging: PeerMessagingSettings;
    file_locking: FileLockSettings;
    vector_db: VectorDBSettings;
    cost_controls: CostControlSettings;
    recovery: RecoverySettings;
    verbosity: string;
    num_ctx: number;
}
export interface WorkflowDefinition {
    name: string;
    description: string;
    mode: WorkflowMode;
    agents: AgentRoleSpec[];
    settings: WorkflowSettings;
}
export interface ValidationResult {
    name: string;
    passed: boolean;
    message: string;
    details: string;
}
export interface ClientConfig {
    baseUrl?: string;
}
//# sourceMappingURL=types.d.ts.map