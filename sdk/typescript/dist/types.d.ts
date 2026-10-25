import type { SpendLimits } from './spend.js';
import type { AgentRef, AgentRole, AgentAttempt } from './agents.js';
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
    /** lead is the Agent or pool the job's lead runs as; absent for a job that names none. */
    lead?: AgentRef;
    /** lead_backend and lead_model are what lead resolved to when the job started. */
    lead_backend?: string;
    lead_model?: string;
    project_id: string;
    poll_interval: number;
    max_iterations: number;
    workflow_name: string;
    /** task_id names the task this job is an attempt at; empty for a job started directly. */
    task_id?: string;
    /** attempt is the 1-based ordinal of this job among the task's attempts. */
    attempt?: number;
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
    /** task_id and attempt bind the job to the task it is an attempt at. */
    task_id?: string;
    attempt?: number;
    /** lead_session_id is the lead agent's session; absent until it is spawned. */
    lead_session_id?: string;
    /** session_ids lists every session the job has spawned and not yet closed. */
    session_ids?: string[];
    /** failure_class classifies a failed job's cause: the environment or the work. */
    failure_class?: string;
    /** stage is the last lifecycle moment the job passed through (prepare, reviewable, complete, discard, blocked). */
    stage?: string;
    /** runner names the JobRunner that drove the job's states; absent means the "workflow" runner. */
    runner?: string;
    /** prompt_append is the run-time prompt material the job appended to every session it spawned. */
    prompt_append?: string;
    /** node names the daemon running the job's control loop. */
    node?: string;
    /** heartbeat_at is the nanosecond Unix instant the loop last proved alive. */
    heartbeat_at?: number;
    /** agent_id is the fleet node the work was dispatched to; absent when it ran in-process. */
    agent_id?: string;
    /** stages is every lifecycle moment the job entered, in order. */
    stages?: JobStageRecord[];
    /** attempts is every attempt at the job's task, oldest first. */
    attempts?: JobAttemptRecord[];
    /** attempt_budget is the number of charged attempts the task allows. */
    attempt_budget?: number;
    /** terminal_reason is the typed reason the task stopped. */
    terminal_reason?: TaskConclusionReason;
    /** readmit_allowed says whether a person may return the task to the queue. */
    readmit_allowed?: boolean;
    /** delivery is the git side of the attempt: branch, head, pull request, reviews. */
    delivery?: JobDelivery | null;
    /** verification is the last validator pipeline's results: the build, the tests. */
    verification?: JobVerificationResult[];
    /**
     * source is where the job's TASK came from — the forge issue it was adopted
     * from. It is the task's own TaskSource, so `issueRef` reads a job exactly
     * as it reads a task. Unlike the rest of this block it is on the LISTING as
     * well as the detail read: a paged jobs view holds no tasks to resolve it
     * from (go-quicbaas#976).
     */
    source?: TaskSource | null;
}
/** JobVerificationResult is one verification command's outcome. */
export interface JobVerificationResult {
    name: string;
    passed: boolean;
    message?: string;
    details?: string;
}
/** JobStageRecord is one lifecycle moment the job entered. */
export interface JobStageRecord {
    stage: string;
    /** entered_at is the Unix-nanosecond instant the moment was entered. */
    entered_at: number;
}
/** JobAttemptRecord is one attempt at the job's task. */
export interface JobAttemptRecord {
    attempt: number;
    job_id: string;
    /** current marks the attempt the status being read belongs to. */
    current?: boolean;
}
/**
 * TaskConclusionReason is the closed vocabulary naming why a task stopped.
 * Mirrors pkg/orchestration/task_conclusion.go; a reason added there and not
 * here is a compile error at every render site rather than a blank badge.
 */
export type TaskConclusionReason = 'completed' | 'budget_exhausted' | 'abandoned' | 'fault_bounded' | 'fault_held' | 'parked_needs_info' | 'parked_blocked' | 'parked_escalated' | 'readmitted';
/** JobReviewRound is one reviewer's verdict on one head of the job's branch. */
export interface JobReviewRound {
    round: number;
    reviewer: string;
    /** verdict is the reviewing module's own vocabulary: approve, request_changes, error. */
    verdict: string;
    head_sha?: string;
    findings?: string;
    /** submitted_at is an RFC 3339 instant. */
    submitted_at?: string;
}
/** JobDelivery is the git side of the job's attempt. */
export interface JobDelivery {
    branch?: string;
    head_sha?: string;
    pull_request_number?: number;
    pull_request_url?: string;
    pull_request_state?: string;
    reviews?: JobReviewRound[];
}
export type JobEventType = 'state_change';
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
    /** lead is the Agent or pool the lead runs as; absent takes the project's default lead. */
    lead?: AgentRef;
    project_id: string;
    max_iterations: number;
    poll_interval?: string;
    workflow_file?: string;
    /** runner names the JobRunner that drives the job's states; omit for the "workflow" runner. */
    runner?: string;
    /**
     * prompt_append is run-time prompt material appended to every session the
     * job spawns, the lead's included. At most 64 KiB.
     */
    prompt_append?: string;
}
export interface StartJobResponse {
    job_id: string;
    timestamp: number;
}
export interface GetJobStatusRequest {
    tenant_id: string;
    job_id: string;
}
export interface ListJobsRequest {
    tenant_id: string;
    task_id?: string;
    /** runner narrows the listing to the jobs one runner drove. */
    runner?: string;
    /** project_id narrows the listing to one project's jobs. */
    project_id?: string;
    /** statuses narrows the listing to jobs in any of these states. */
    statuses?: JobState[];
    /**
     * job_ids narrows the listing to the named jobs — how a caller holding a
     * page of something else resolves the jobs its rows name in ONE call.
     */
    job_ids?: string[];
    /** agent_id narrows the listing to the jobs dispatched to one fleet node. */
    agent_id?: string;
    /** The shared paging parameters; see PageParams in page.ts. */
    limit?: number;
    offset?: number;
    sort?: string;
    q?: string;
}
export interface ListJobsResponse {
    jobs: JobStatus[];
}
export interface CancelJobRequest {
    tenant_id: string;
    job_id: string;
}
export interface SubscribeJobEventsRequest {
    tenant_id: string;
    job_id: string;
}
export interface GetTaskRequest {
    tenant_id: string;
    task_id: string;
}
export interface ListTasksRequest {
    tenant_id: string;
    status?: string;
    team_name?: string;
    assigned_to?: string;
    /** project_id restricts the listing to one project. */
    project_id?: string;
    /** statuses restricts the listing to tasks in any of these statuses; composes with status. */
    statuses?: TaskStatus[];
    /** created_by restricts the listing to one creator. */
    created_by?: string;
    /**
     * auto_spawn restricts the listing to the tasks that asked for immediate
     * dispatch (true) or the ones queued for a claim (false). Absent does not
     * narrow.
     */
    auto_spawn?: boolean;
    /**
     * agent_id narrows the listing to the tasks whose LATEST ATTEMPT ran on one
     * fleet node. The agent is the attempt's job's, not the task's; the server
     * resolves it for the listing.
     */
    agent_id?: string;
    /** job_id narrows the listing to the task whose latest attempt is that job. */
    job_id?: string;
    /**
     * source_kind and source_ref, set together, narrow the listing to the task
     * holding that source reservation in project_id's scope. A source nobody
     * holds lists nothing; it is not an error.
     */
    source_kind?: string;
    source_ref?: string;
    /** The shared paging parameters; see PageParams in page.ts. */
    limit?: number;
    offset?: number;
    sort?: string;
    q?: string;
}
export interface ListTasksResponse {
    tasks: Task[];
    count: number;
}
export type FleetNodeStatus = 'registered' | 'running' | 'idle' | 'offline' | 'error' | 'disabled';
export interface FleetNode {
    id: string;
    tenant_id: string;
    name: string;
    host: string;
    backend: string;
    model: string;
    node_type: string;
    status: FleetNodeStatus;
    capabilities: string[];
    /**
     * Every backend name this node is matched on: the primary backend first,
     * then each capability, each once. Never null (go-quicbaas#700).
     */
    backends: string[];
    work_dir: string;
    last_seen: number;
    registered_at: number;
    disabled_at?: number;
    version: number;
    metadata: Record<string, string>;
    advertised_address?: string;
    reachability?: FleetNodeReachability;
}
export type FleetNodeReachability = 'reachable' | 'unverified';
export interface DisableFleetNodeRequest {
    tenant_id: string;
    node_id: string;
}
export interface DisableFleetNodeResponse {
    node_id: string;
    disabled: boolean;
    disabled_at: string;
    timestamp: string;
}
export interface EnableFleetNodeRequest {
    tenant_id: string;
    node_id: string;
}
export interface EnableFleetNodeResponse {
    node_id: string;
    enabled: boolean;
    timestamp: string;
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
export type TaskStatus = 'pending' | 'claimed' | 'in_progress' | 'completed' | 'failed' | 'cancelled' | 'blocked';
export interface TaskResult {
    output: string;
    files: string[];
    metadata: Record<string, string>;
}
export type TaskSourceKind = 'scheduler' | 'operator' | 'job' | (string & {});
export interface TaskSource {
    kind: TaskSourceKind;
    ref: string;
    url?: string;
}
export type TaskBlockKind = 'needs_info' | 'blocked' | 'escalated';
export type TaskFaultOrigin = 'environment' | 'work' | 'undiagnosed' | '';
export type ArtifactKind = 'inline' | 'file' | 'commit' | 'kv_key' | 'url';
export interface ArtifactRef {
    agent_id?: string;
    path?: string;
    sha?: string;
    branch?: string;
}
export interface Artifact {
    kind: ArtifactKind | string;
    name: string;
    mime_type?: string;
    size?: number;
    /** content carries inline bytes, base64-encoded on the wire. */
    content?: string;
    ref?: ArtifactRef;
    metadata?: Record<string, string>;
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
    /** auto_spawn is true when the task was created with immediate dispatch. */
    auto_spawn?: boolean;
    /** spawned_job_id is the ID of the auto-spawned job, empty when not spawned. */
    spawned_job_id?: string;
    /** spawned_at is a nanosecond Unix timestamp of when the job was dispatched. */
    spawned_at?: number;
    /** project_id scopes the task to a Project; empty for unscoped tasks. */
    project_id?: string;
    /**
     * agent_id is the fleet node the task's LATEST ATTEMPT ran on, resolved by
     * the server when the listing is paged. Absent for a task that has had no
     * attempt, or whose attempt ran in-process.
     */
    agent_id?: string;
    /** output_artifacts are what the task produced (pull requests, commits, files). */
    output_artifacts?: Artifact[];
    /** input_artifacts are what the task was given to work from. */
    input_artifacts?: Artifact[];
    /** source records where the task came from; absent for hand-created tasks. */
    source?: TaskSource;
    /** priority orders dispatch; higher runs first. */
    priority?: number;
    /** attempts counts every job dispatched for this task, refunded or not. */
    attempts?: number;
    /** attempt_budget is the number of charged attempts allowed before the task blocks. */
    attempt_budget?: number;
    /** environment_faults counts attempts refunded because the environment, not the work, failed. */
    environment_faults?: number;
    /** consecutive_environment_faults is the current run of refunded attempts. */
    consecutive_environment_faults?: number;
    /** retry_not_before is a nanosecond Unix timestamp before which the task is not redispatched; 0 when not deferred. */
    retry_not_before?: number;
    /** blocked_reason says, in words, what a person must resolve. */
    blocked_reason?: string;
    /** block_kind classifies the block; absent when the task is not blocked. */
    block_kind?: TaskBlockKind;
    /** status_changed_at is a nanosecond Unix timestamp of the last status transition. */
    status_changed_at?: number;
    /** runner names the JobRunner that drives an attempt; absent means the lead-driven 'workflow' runner. */
    runner?: string;
    /** prompt_append is run-time prompt material every attempt carries to the sessions it spawns. */
    prompt_append?: string;
    /** routing are the placement hints a dispatch reads, kept so a requeue keeps them. */
    routing?: TaskRouting;
    /** dispatch_reason says why the last dispatch produced no job; cleared when one does. */
    dispatch_reason?: string;
    /** question_attempts counts attempts refunded because the worker stopped to ask a person. */
    question_attempts?: number;
    /** block_fault_class names the environment fault class a block came from; '' for a hold or a question. */
    block_fault_class?: string;
    /**
     * conclusion_reason is the typed reason the task stopped; '' while it is
     * open. Open to strings because the server's vocabulary is wider than the
     * reasons TaskConclusionReason renders (runner_parked, already_satisfied,
     * superseded).
     */
    conclusion_reason?: TaskConclusionReason | string;
    /** charged_attempts is attempts less the refunded ones. Derived by the server. */
    charged_attempts?: number;
    /** effective_budget is attempt_budget, or the engine default when none is declared. Derived by the server. */
    effective_budget?: number;
    /** fault_class names the fault behind the latest concluded attempt; for a blocked task, its block's class. Derived by the server. */
    fault_class?: string;
    /** fault_origin says whose failure stopped the task. Derived by the server. */
    fault_origin?: TaskFaultOrigin;
}
export interface TaskRouting {
    /** backend restricts the attempt to agents running this backend. */
    backend?: string;
    /** host_affinity pins the attempt to an agent by name or host. */
    host_affinity?: string;
    /** gpu_required restricts the attempt to agents advertising "gpu". */
    gpu_required?: boolean;
}
export type AttentionKind = 'parked_task' | 'agent_offline' | 'account_error' | 'barrier_sealed' | 'project_idle';
export type AttentionAction = 'approve_recommendation' | 'answer' | 'allow_another_round' | 'close_as_done' | 'readmit' | 'note';
export interface AttentionItem {
    id: string;
    kind: AttentionKind;
    project_id?: string;
    project_name?: string;
    subject: string;
    issue_ref?: string;
    issue_url?: string;
    block_kind?: TaskBlockKind;
    /** condition is the typed unblock condition the park names — see
     * sdlc.UnblockKind: pipeline_green, forge_issue_state, release_tag,
     * owner_ruling, operator_repair. */
    condition?: string;
    question?: string;
    recommendation?: string;
    /** truncated is true when the underlying reply was capped before it was
     * stored, so recommendation may end mid-sentence. */
    truncated?: boolean;
    /** ruling_posted is true when the parked task's issue carries an
     * authorized ruler's comment newer than the park — the owner has already
     * answered, and the unblock sweep readmits the task. */
    ruling_posted?: boolean;
    /** message is an environmental alert's own explanation. */
    message?: string;
    /** severity is "warning" or "critical", set only on environmental alerts. */
    severity?: string;
    created_at?: number;
    status_changed_at?: number;
    attempts?: number;
    actions?: AttentionAction[];
}
export interface AttentionResolveRequest {
    tenant_id: string;
    task_id: string;
    action: AttentionAction;
    /** answer carries the operator's free text. Required for "answer";
     * optional context for "close_as_done"; ignored otherwise. */
    answer?: string;
}
export type BackendKind = 'agent' | 'model_server' | 'api_inference';
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
export interface JobStateSpec {
    /** state is the state itself, as it appears on JobStatus.state. */
    state: JobState;
    /** label is how a consumer names the state to a person. */
    label: string;
    /** session_backed says an agent session is doing the work in this state. */
    session_backed: boolean;
}
export interface JobRunnerInfo {
    /** name is what JobStatus.runner names the runner by. */
    name: string;
    /** states are the runner's non-terminal states in order. */
    states: JobStateSpec[];
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
    /** prompt_append is run-time prompt material appended to every session of this role. At most 64 KiB. */
    prompt_append?: string;
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
    /** phase is the control-loop state the criterion is judged in: validating (default) or reviewing. */
    phase?: CriterionPhase;
}
export type CriterionPhase = 'validating' | 'reviewing';
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
    /** hooks names the registered lifecycle hook set every job of the workflow runs; empty runs none. */
    hooks?: string;
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
export interface SessionEventEnvelope {
    session_id: string;
    tenant_id?: string;
    sequence: number;
    payload_json: Record<string, unknown>;
    timestamp: string;
    from_replay?: boolean;
}
export interface GetSessionHistoryRequest {
    tenant_id?: string;
    session_id: string;
    from_sequence?: number;
    limit?: number;
}
export interface GetSessionHistoryResponse {
    events: SessionEventEnvelope[];
    next_seq: number;
}
export type SpendLevel = 'tenant' | 'project' | 'task' | 'job' | 'session' | 'round';
export declare const SPEND_UNATTRIBUTED = "-";
export type SpendPolicyStatus = 'unknown' | 'none' | 'metered';
export interface SpendWindowStatus {
    name: string;
    size: number;
    max_tokens: number;
    used_tokens: number;
    remaining_tokens: number;
    max_equivalent_micros?: number;
    used_equivalent_micros?: number;
    remaining_equivalent_micros?: number;
    exhausted: boolean;
    frees_at?: string;
}
export interface SpendBreakerVerdict {
    state: 'closed' | 'open' | 'half_open';
    failures: number;
    successes: number;
    last_failure_at?: string;
    last_success_at?: string;
    open_until?: string;
}
export interface SpendTotals {
    entries?: number;
    input_tokens?: number;
    output_tokens?: number;
    cache_read_tokens?: number;
    cache_write_tokens?: number;
    total_tokens: number;
    equivalent_micros?: number;
    priced_entries?: number;
    unpriced_entries?: number;
    billed_micros?: number;
    billed_entries?: number;
    unbilled_entries?: number;
    duration_ms?: number;
    energy_mwh?: number;
    electricity_entries?: number;
    measured_energy_entries?: number;
    successes?: number;
    failures?: number;
}
export interface SpendAccountBilling {
    mode?: string;
    monthly_fee_micros?: number;
}
export interface SpendAccount extends SpendTotals {
    account_id: string;
    display_name?: string;
    provider?: string;
    billing?: SpendAccountBilling;
    policy_status: SpendPolicyStatus;
    windows?: SpendWindowStatus[];
    breaker?: SpendBreakerVerdict;
    subscription_fee_micros?: number;
}
export interface SpendModel extends SpendTotals {
    engine: string;
    model: string;
}
export interface SpendChild extends SpendTotals {
    id?: string;
    level: SpendLevel;
    unattributed?: boolean;
    name?: string;
}
export interface GetSpendReportRequest {
    tenant_id?: string;
    project_id?: string;
    task_id?: string;
    job_id?: string;
    session_id?: string;
    round?: number;
    round_set?: boolean;
    from?: string;
    to?: string;
    host?: string;
    agent?: string;
}
export interface SpendAgent extends SpendTotals {
    agent_id?: string;
    name?: string;
    unattributed?: boolean;
}
export interface SpendHost extends SpendTotals {
    host?: string;
    unattributed?: boolean;
}
export interface GetSpendReportResponse extends SpendTotals {
    level: SpendLevel;
    tenant_id: string;
    project_id?: string;
    task_id?: string;
    job_id?: string;
    session_id?: string;
    round?: number;
    project_name?: string;
    currency?: string;
    first_at?: string;
    last_at?: string;
    from?: string;
    to?: string;
    subscription_fee_micros?: number;
    accounts?: SpendAccount[];
    models?: SpendModel[];
    child_level?: SpendLevel;
    children?: SpendChild[];
    scope_policy_status?: SpendPolicyStatus;
    scope_windows?: SpendWindowStatus[];
    host?: string;
    hosts?: SpendHost[];
    agent?: string;
    agents?: SpendAgent[];
    agent_policy_status?: SpendPolicyStatus;
    agent_windows?: SpendWindowStatus[];
}
export interface GetTenantSpendPolicyRequest {
    tenant_id: string;
}
export interface SetTenantSpendPolicyRequest {
    tenant_id: string;
    limits: SpendLimits;
}
export interface TenantSpendPolicy {
    tenant_id: string;
    limits?: SpendLimits;
    updated_at?: string;
    windows?: SpendWindowStatus[];
}
export type SessionTurnRole = 'user' | 'assistant' | 'system';
export interface SessionTokenUsage {
    input_tokens?: number;
    output_tokens?: number;
    cache_creation_input_tokens?: number;
    cache_read_input_tokens?: number;
}
export interface SessionToolUse {
    invocation_id?: string;
    name: string;
    is_error?: boolean;
    result_bytes?: number;
}
export interface SessionTurn {
    sequence: number;
    session_id: string;
    tenant_id?: string;
    role: SessionTurnRole;
    timestamp: string;
    text: string;
    truncated_bytes?: number;
    model?: string;
    stop_reason?: string;
    tools?: SessionToolUse[];
    usage?: SessionTokenUsage;
}
export interface GetSessionConversationRequest {
    tenant_id?: string;
    session_id: string;
    cursor?: number;
    limit?: number;
}
export interface GetSessionConversationResponse {
    session_id: string;
    tenant_id?: string;
    turns: SessionTurn[];
    next_cursor?: number;
    oldest_sequence?: number;
}
export interface SubscribeSessionEventsRequest {
    tenant_id?: string;
    session_id: string;
    from_sequence?: number;
    follow?: boolean;
    signal?: AbortSignal;
}
export declare const SandboxMode: {
    readonly Default: 0;
    readonly Required: 1;
    readonly None: 2;
};
export type SandboxModeValue = (typeof SandboxMode)[keyof typeof SandboxMode];
export interface MCPServerConfig {
    name: string;
    url?: string;
    command?: string;
    args?: string[];
    env?: Record<string, string>;
}
export interface SpawnSessionRequest {
    tenantID: string;
    agent_id?: string;
    pool_id?: string;
    role?: AgentRole;
    backend?: string;
    project_id?: string;
    model?: string;
    work_dir?: string;
    env?: Record<string, string>;
    sandbox_mode?: SandboxModeValue;
    sandboxed?: boolean;
    host_affinity?: string;
    host_affinity_strict?: boolean;
    mcp_servers?: MCPServerConfig[];
    account_id?: string;
    account_auto?: boolean;
    account_strategy?: string;
    account_allow?: string[];
    prompt_append?: string;
    keep?: boolean;
}
export interface SpawnSessionResponse {
    session_id: string;
    backend: string;
    container_id?: string;
    resolved_agent_id?: string;
    failovers?: AgentAttempt[];
}
export interface PromptSessionRequest {
    tenant_id?: string;
    session_id: string;
    prompt: string;
}
export interface PromptSessionResponse {
    session_id: string;
    response: string;
    done: boolean;
}
export interface CloseSessionRequest {
    tenant_id?: string;
    session_id: string;
}
export interface CloseSessionResponse {
    session_id: string;
    closed: boolean;
}
export type AccountRotationMode = 'in_place' | 'resume';
export interface RotateSessionAccountRequest {
    tenant_id?: string;
    session_id: string;
    reason?: string;
    hold_outgoing?: boolean;
    available_at?: string;
}
export interface RotateSessionAccountResponse {
    session_id: string;
    previous_account_id?: string;
    account_id: string;
    strategy?: string;
    mode: AccountRotationMode;
}
export interface ListSessionsRequest {
    tenantID: string;
    backend?: string;
    status?: string;
    /** project_id narrows the listing to one project's sessions. */
    project_id?: string;
    /** job_id narrows the listing to the sessions one job spawned. */
    job_id?: string;
    /**
     * runner narrows the listing to the sessions whose job one runner drove —
     * which is the ROLE the session played.
     */
    runner?: string;
    /** agent_id narrows the listing to the sessions on one fleet node. */
    agent_id?: string;
    /** The shared paging parameters; see PageParams in page.ts. */
    limit?: number;
    offset?: number;
    sort?: string;
    q?: string;
}
export interface SessionStatusResponse {
    session_id: string;
    backend: string;
    /** model is the model the session runs on, when the service reported one. */
    model?: string;
    status: string;
    container_id?: string;
    created_at: number;
    last_active: number;
    /** job_id names the orchestration job this session works for, if any. */
    job_id?: string;
    /**
     * runner is the job runner that drove that job — `sdlc`, `review`,
     * `triage`, `workflow` — and so which ROLE the session played. The service
     * resolves it per page, so a consumer holding one page of sessions never
     * has to read the job list to find out. Absent when the session belongs to
     * no job, or to one no longer on record.
     */
    runner?: string;
    /**
     * agent_id is the fleet node the session's job was dispatched to, resolved
     * beside `runner` and from the same record. Absent when the session belongs
     * to no job or the work ran in-process.
     */
    agent_id?: string;
}
export interface ListSessionsResponse {
    sessions: SessionStatusResponse[];
    count: number;
}
export interface GetFleetNodeRequest {
    tenant_id: string;
    node_id: string;
}
export interface ListFleetNodesRequest {
    tenant_id: string;
    host?: string;
    status?: string;
    node_type?: string;
    backend?: string;
    model?: string;
    /** The shared paging parameters; see PageParams in page.ts. */
    limit?: number;
    offset?: number;
    sort?: string;
    q?: string;
}
export interface ListFleetNodesResponse {
    nodes: FleetNode[];
    count: number;
}
export interface CreateWorkflowRequest {
    tenant_id: string;
    name: string;
    definition: WorkflowDefinition;
}
export interface CreateWorkflowResponse {
    name: string;
    created_at: number;
}
export interface ListWorkflowsResponse {
    workflows: WorkflowDefinition[];
    count: number;
}
export interface GetWorkflowResponse {
    workflow: WorkflowDefinition;
}
export interface CreateTaskRequest {
    tenant_id: string;
    subject: string;
    team_name: string;
    description?: string;
    metadata?: Record<string, string>;
    host_affinity?: string;
    host_affinity_strict?: boolean;
    gpu_required?: boolean;
    auto_spawn?: boolean;
    backend?: string;
    runner?: string;
    prompt_append?: string;
}
export interface ListBackendModelsResponse {
    backend: string;
    models: string[];
    count: number;
}
export interface CreateTaskResponse {
    task: Task;
}
export interface DecomposeTaskRequest {
    tenant_id: string;
    task_id: string;
}
export interface DecomposeTaskResponse {
    tasks: Task[];
}
export interface ClaimTaskRequest {
    tenant_id: string;
    task_id: string;
    agent_id: string;
}
export interface BlockTaskRequest {
    tenant_id: string;
    task_id: string;
    kind: TaskBlockKind;
    reason: string;
}
export interface ReadmitTaskRequest {
    tenant_id: string;
    task_id: string;
}
export interface DeleteTaskRequest {
    tenant_id: string;
    task_id: string;
}
export interface DeleteTaskResponse {
    task_id: string;
    deleted: boolean;
}
export interface CompleteTaskRequest {
    tenant_id: string;
    task_id: string;
    result?: {
        output?: string;
        metadata?: Record<string, string>;
    };
}
export interface RegisterFleetNodeRequest {
    tenant_id: string;
    name: string;
    host: string;
    backend: string;
    node_type: string;
    model?: string;
    metadata?: Record<string, string>;
}
export interface RegisterFleetNodeResponse {
    node: FleetNode;
}
export interface RemoveFleetNodeRequest {
    tenant_id: string;
    node_id: string;
}
export type Transport = 'wails' | 'rest' | 'grpc-web' | 'unix' | 'mcp' | 'quic';
export interface ClientConfig {
    baseURL?: string;
    /** @deprecated use baseURL */
    baseUrl?: string;
    transport?: Transport;
    token?: string | (() => Promise<string>);
    tenantID?: string;
    grpcWeb?: {
        format: 'binary' | 'text';
    };
    unix?: {
        socketPath: string;
    };
    mcp?: {
        endpoint: string;
    };
    quic?: {
        endpoint: string;
    };
}
export type SSHScope = 'tenant' | 'user' | 'service';
export interface SSHKey {
    /** Scope of this key. Populated by the scoped REST surface. */
    scope?: SSHScope;
    /** Owner identifier within the scope. */
    owner?: string;
    /** Human-visible key name, unique within (scope, owner). */
    name: string;
    /** Canonical algorithm identifier: 'ed25519', 'rsa', 'ecdsa-p256', etc. */
    algorithm: string;
    /** OpenSSH SHA256 fingerprint ('SHA256:<base64>'). */
    fingerprint: string;
    /**
     * OpenSSH authorized_keys-format public key line.
     * The server JSON tag is "public_key"; the legacy field name
     * "public_key_openssh" is kept as an optional alias for backward compat
     * with Wails callers that read SSHKeyDTO.
     */
    public_key?: string;
    /** @deprecated Use public_key. Wails SSHKeyDTO still populates this field. */
    public_key_openssh?: string;
    /** ISO8601 creation timestamp. */
    created_at: string;
    /** Optional free-form comment. */
    comment?: string;
}
export interface CreateSSHKeyRequest {
    /** Human-visible key name, unique within (scope, owner). */
    name: string;
    /** Canonical algorithm: 'ed25519', 'rsa', 'ecdsa-p256', etc. */
    algorithm: string;
    /** Key scope. Defaults to 'user' server-side when omitted. */
    scope?: SSHScope;
    /** Owner identifier. Defaults to the authenticated OS user when omitted. */
    owner?: string;
    /** Optional RSA key size in bits. Only consulted for 'rsa' algorithm. */
    rsa_bits?: number;
    /** Optional free-form comment. */
    comment?: string;
}
export interface ImportSSHKeyRequest {
    /** Human-visible key name. */
    name: string;
    /** PEM-encoded private key bytes. The server base64-decodes this field. */
    pem: string;
    /**
     * Base64-encoded passphrase bytes. The server decodes the base64 directly
     * into a mlocked buffer — the plaintext never materialises as a Go string.
     */
    passphrase_b64?: string;
    /** Key scope. Defaults to 'user' server-side when omitted. */
    scope?: SSHScope;
    /** Owner identifier. Defaults to the authenticated OS user when omitted. */
    owner?: string;
    /** Optional free-form comment. */
    comment?: string;
}
export interface ListSSHKeysQuery {
    /** Filter by scope. */
    scope?: SSHScope;
    /** Filter by owner. */
    owner?: string;
}
export interface ListSSHKeysResponse {
    /** Scoped REST surface wraps keys in "items". */
    items?: SSHKey[];
    /** Legacy Wails binding wraps keys in "keys". */
    keys?: SSHKey[];
}
export interface SSHPublicKey {
    /** Canonical algorithm identifier. */
    algorithm: string;
    /** OpenSSH authorized_keys-format line. */
    line: string;
    /** OpenSSH SHA256 fingerprint. */
    fingerprint: string;
}
export interface SignRequest {
    /** Raw digest bytes to sign. JSON base64-encoded automatically. */
    digest: Uint8Array | number[];
}
export interface SignResponse {
    /** Signature format string (e.g. 'ssh-ed25519'). */
    format: string;
    /** Raw signature blob. JSON base64-decoded by the receiver. */
    blob: number[];
    /** Trailing bytes, if any. */
    rest?: number[];
}
export interface SSHPin {
    /** Scope that owns this pin. */
    scope: SSHScope;
    /** Owner within the scope. */
    owner: string;
    /** Hostname or IP address that was pinned. */
    host: string;
    /** OpenSSH SHA256 fingerprint of the pinned host key. */
    fingerprint: string;
    /** Algorithm of the pinned host key. */
    algorithm?: string;
    /** ISO8601 timestamp when the pin was added. */
    added_at: string;
    /** Provenance tag: 'manual', 'imported:<path>', 'first-contact', etc. */
    source?: string;
    /** The pinned host key as an authorized_keys-format line. */
    public_key?: string;
}
export interface AddSSHPinRequest {
    /** Scope that owns this pin. */
    scope?: SSHScope;
    /** Owner within the scope. */
    owner?: string;
    /** Hostname or IP address to pin. */
    host: string;
    /** Host key as an authorized_keys-format line, e.g. "ssh-ed25519 AAAA…". */
    public_key: string;
    /** Provenance tag. */
    source?: string;
}
export interface ListSSHPinsResponse {
    /** Pinned host keys. */
    items: SSHPin[];
}
export interface VerifySSHPinRequest {
    /** Scope whose pin store to query. */
    scope?: SSHScope;
    /** Owner whose pin store to query. */
    owner?: string;
    /** Fingerprint presented by the connecting host. */
    fingerprint: string;
    /** Algorithm of the presented key (optional hint). */
    algorithm?: string;
}
export interface SSHAuthorizedEntry {
    /** Scope that owns this entry. */
    scope: SSHScope;
    /** Owner within the scope. */
    owner: string;
    /** Key name. */
    name: string;
    /** Full authorized_keys line (algorithm + base64 + comment). */
    line: string;
    /** OpenSSH restrict/permit directives. */
    options?: string[];
    /** Free-form comment. */
    comment?: string;
}
export interface EmitAuthorizedRequest {
    /** OpenSSH authorized_keys-format public key. */
    public_key: string;
    /** Optional OpenSSH directives. */
    options?: string[];
    /** Optional comment. */
    comment?: string;
}
export interface EmitAuthorizedResponse {
    /** The formatted authorized_keys line. */
    line: string;
}
export interface VerifyAuthorizedRequest {
    /** Scope to search for a matching authorized entry. */
    scope?: SSHScope;
    /** Owner to search for a matching authorized entry. */
    owner?: string;
    /** SHA256 fingerprint of the key to verify. */
    fingerprint: string;
    /** Algorithm hint (optional). */
    algorithm?: string;
}
export interface VerifyAuthorizedResponse {
    /** The matching authorized entry. */
    entry: SSHAuthorizedEntry;
}
export interface DeployFleetNodeSpec {
    host: string;
    ssh_user: string;
    ssh_key_name?: string;
    ssh_key_path?: string;
    ssh_key_passphrase?: string;
    ssh_password?: string;
    ssh_host_key?: string;
    insecure_host_key?: boolean;
    server: string;
    tenant: string;
    node_bind: string;
    backends: string[];
    binary: string;
    install_dir?: string;
    data_dir?: string;
    /** Tri-state: undefined=auto-detect, true=systemd, false=nohup. */
    systemd?: boolean;
    /** Nanoseconds (Go time.Duration). Zero disables the online wait. */
    wait_online?: number;
}
export interface DeployFleetNodeResult {
    host: string;
    installed_path: string;
    service_status: string;
    node_id?: string;
    online_at?: string;
    /** Path to the mTLS client certificate installed on the remote host. */
    node_cert_path?: string;
    /** Hex-encoded serial number of the issued fleet node certificate. */
    node_cert_serial?: string;
}
export type UserLifecycle = 'pending' | 'active' | 'suspended' | 'locked' | 'disabled' | 'deleting';
export interface IAMUser {
    id: string;
    tenant_id: string;
    username: string;
    email: string;
    lifecycle?: UserLifecycle;
    roles: string[];
    created_at: string;
    updated_at: string;
}
export interface ListUsersResponse {
    users: IAMUser[];
    count: number;
}
export interface CreateUserRequest {
    tenant_id: string;
    username: string;
    email: string;
    display_name?: string;
    auth_methods?: string[];
    roles?: string[];
    invite_expiry_hours?: number;
}
export interface CreateUserResponse {
    user_id: string;
    email: string;
    invite_token: string;
    invite_url: string;
    expires_at: string;
}
export interface InviteMetadata {
    user_id: string;
    email?: string;
    name?: string;
    auth_methods: string[];
    expires_at: string;
}
export interface ActivationResponse {
    user_id: string;
    message: string;
    /**
     * Non-fatal irregularity behind an otherwise successful activation —
     * currently, an activation write that reported an error on a user the server
     * then found active. The ceremony succeeded and the invite was consumed, so
     * this is not a failure, but it should be surfaced rather than dropped.
     */
    warning?: string;
}
export interface IAMTenant {
    id: string;
    name: string;
    description: string;
    created_at: string;
    updated_at: string;
}
export interface ListTenantsResponse {
    tenants: IAMTenant[];
    count: number;
}
export interface CreateTenantRequest {
    name: string;
    description?: string;
}
export interface CreateTenantResponse {
    tenant: IAMTenant;
}
export interface IAMRole {
    id: string;
    tenant_id: string;
    name: string;
    description: string;
    permissions: string[];
    retired_permissions?: string[];
    created_at: string;
    updated_at: string;
}
export interface ListRolesResponse {
    roles: IAMRole[];
    count: number;
}
export interface RoleReconcileResult {
    role_id: string;
    name: string;
    created?: boolean;
    added?: string[];
    removed?: string[];
}
export interface ReconcileRolesResponse {
    tenant_id: string;
    roles?: RoleReconcileResult[];
    changed: boolean;
}
export interface IAMPolicy {
    name: string;
    tenant_id: string;
    description: string;
    rules: unknown[];
    spec_yaml?: string;
    created_at: string;
    updated_at: string;
}
export interface ListPoliciesResponse {
    policies: IAMPolicy[];
    count: number;
}
export type KMSKeyStatus = 'active' | 'inactive' | 'pending_deletion' | 'destroyed';
export interface KMSKey {
    id: string;
    name: string;
    description: string;
    algorithm: string;
    key_size: number;
    status: KMSKeyStatus;
    created_at: string;
    updated_at: string;
    exportable: boolean;
    tags: Record<string, string>;
}
export interface ListKeysResponse {
    keys: KMSKey[];
    count: number;
}
export interface CreateKeyRequest {
    name?: string;
    description?: string;
    key_attributes: {
        algorithm: string;
        key_usage: string[];
        exportable?: boolean;
    };
    tags?: Record<string, string>;
}
export interface CreateKeyResponse {
    key: KMSKey;
    timestamp: number;
}
export interface RotateKeyResponse {
    key: KMSKey;
    old_key_id: string;
    timestamp: number;
}
export interface KMSEncryptRequest {
    key_id: string;
    plaintext: string;
}
export interface KMSEncryptResponse {
    ciphertext: string;
    key_id: string;
    algorithm: string;
}
export interface KMSDecryptRequest {
    key_id: string;
    ciphertext: string;
}
export interface KMSDecryptResponse {
    plaintext: string;
    key_id: string;
}
export interface SecretEntry {
    path: string;
    version: number;
    created_at: string;
    updated_at: string;
}
export interface ListSecretsResponse {
    secrets: SecretEntry[];
    count: number;
}
export interface StoreSecretRequest {
    path: string;
    data: Record<string, unknown>;
}
export interface GetSecretResponse {
    path: string;
    data: Record<string, unknown>;
    version: number;
    created_at: string;
    updated_at: string;
}
export interface RotateSecretResponse {
    path: string;
    version: number;
    rotated_at: string;
}
export interface SecretVersionEntry {
    secret_id: string;
    version: number;
    created_at: string;
    created_by?: string;
    deleted_at?: string;
    expires_at?: string;
    metadata?: Record<string, string>;
}
export interface ListSecretVersionsResponse {
    versions: SecretVersionEntry[];
    count: number;
}
export interface SetDefaultKeyResponse {
    name: string;
    is_default: boolean;
    previous_default?: string;
    timestamp: string;
}
export interface RotateRootKeyResponse {
    success: boolean;
    message: string;
    timestamp: string;
}
export interface RekeyRequest {
    new_threshold: number;
    new_total_shares: number;
}
export interface RekeyResponse {
    shares: string[];
    version: number;
    timestamp: string;
}
export interface SealStatus {
    sealed: boolean;
    initialized: boolean;
    mode?: string;
    threshold: number;
    total_shares: number;
    shares_provided: number;
    shares_received?: number;
    tpm_available?: boolean;
    cluster_nodes?: number;
    version: number;
    timestamp?: string;
    cluster_name?: string;
}
export interface SealInitRequest {
    secret_shares: number;
    secret_threshold: number;
}
export interface SealInitResponse {
    keys: string[];
    root_token: string;
}
export interface UnsealRequest {
    key: string;
}
export interface UnsealResponse {
    sealed: boolean;
    progress: number;
    threshold: number;
}
export interface BarrierStatusResponse {
    /** True when the barrier is in sealed state. */
    sealed: boolean;
    /** Active sealing strategy identifier (e.g. "passphrase", "shamir"). */
    strategy: string;
    /** True when the active strategy uses dedicated cryptographic hardware. */
    hardware_backed: boolean;
    /** RFC3339 timestamp recorded when the barrier was first initialized. */
    initialized_at?: string;
}
export interface BarrierInitRequest {
    /** Passphrase used to derive the master key. The REST client base64-encodes it. */
    secret: Uint8Array;
}
export interface BarrierUnsealRequest {
    /** Passphrase used to unseal the barrier. The REST client base64-encodes it. */
    secret: Uint8Array;
}
export interface BarrierSealRequest {
    /** Security Officer PIN. Required when the server has a PIN backend with an SO PIN set. */
    so_pin?: string;
}
export interface BarrierShamirInitRequest {
    /** Optional passphrase used to derive the master key. The REST client base64-encodes it. */
    secret: Uint8Array;
    /** Minimum shares required to reconstruct the master key. */
    threshold: number;
    /** Total shares to generate. */
    total_shares: number;
}
export interface BarrierShamirInitResponse {
    /** Shamir shares for distribution. */
    shares: string[];
    /** Threshold required for reconstruction. */
    threshold: number;
    /** Total shares generated. */
    total_shares: number;
}
/** BarrierDomain names a barrier's key-holder set: its unseal officers or its recovery officers. */
export type BarrierDomain = 'unseal' | 'recovery';
/**
 * BarrierQuorumOp names the operation a quorum round authorises. unseal, seal
 * and root-token run in the unseal domain, recover in the recovery domain and
 * rekey in either.
 */
export type BarrierQuorumOp = 'unseal' | 'seal' | 'rekey' | 'recover' | 'root-token';
export interface BarrierHolderFactor {
    /** Factor name, e.g. "mtls". */
    name: string;
    /** Factor-specific enrolment parameters. */
    params?: Record<string, unknown>;
}
export interface BarrierOfficerRegisterRequest {
    /** Officer username. */
    username: string;
    /** Role bound to the issued certificate. The server defaults it. */
    role?: string;
    /** PEM-encoded CSR the officer's certificate is issued from. */
    csr_pem: string;
    /** KEM that seals the officer's share envelope. */
    encryption_kem?: string;
    /** Base64-encoded public key for encryption_kem. */
    encryption_key?: string;
    /** Factors the officer answers quorum challenges with. */
    factors?: BarrierHolderFactor[];
    /** Designated holder the officer stands in for. */
    designated?: string;
}
export interface BarrierOfficerRegisterResponse {
    /** PEM-encoded officer certificate. */
    cert_pem: string;
    /** Role bound to the certificate. */
    role: string;
    /** Designated holder the officer stands in for. */
    designated?: string;
    /** KEM that seals the officer's share envelope. */
    encryption_kem?: string;
    /** Names of the enrolled factors. */
    factors?: string[];
}
export interface BarrierOfficer {
    /** Officer username. */
    username: string;
    /** Role bound to the officer's certificate. */
    role: string;
    /** SPKI SHA-256 thumbprint of the officer's certificate. */
    cert_thumbprint?: string;
    /** One-based index of the officer's share. */
    share_index: number;
    /** Designated holder the officer stands in for. */
    designated?: string;
    /** Names of the enrolled factors. */
    factors?: string[];
    /** KEM that seals the officer's share envelope. */
    encryption_kem?: string;
    /** RFC3339 enrolment time. */
    enrolled_at: string;
    /** RFC3339 time the officer claimed its share. */
    claimed_at?: string;
    /** True while the officer's share awaits its claim. */
    share_pending: boolean;
}
export interface BarrierShareClaimBeginResponse {
    /** Hex-encoded nonce to sign. */
    nonce_hex: string;
    /** RFC3339 expiration of the nonce. */
    expires_at: string;
}
export interface BarrierShareClaimCompleteRequest {
    /** Officer username. */
    username: string;
    /** Hex-encoded nonce from shareClaimBegin. */
    nonce_hex: string;
    /** Base64-encoded signature over the nonce. */
    signature: string;
}
export interface BarrierShareClaimCompleteResponse {
    /** One-based index of the share. */
    share_index: number;
    /** Base64-encoded share envelope sealed to the officer. */
    envelope: string;
}
export interface BarrierQuorumBeginRequest {
    /** Holder set the round runs in. */
    domain: BarrierDomain;
    /** Operation the round authorises. */
    op: BarrierQuorumOp;
    /** Holder answering the challenge. */
    username: string;
    /** Factor to answer with. The server picks the holder's default. */
    factor?: string;
}
export interface BarrierQuorumChallenge {
    /** Challenge identifier echoed on submit. */
    challenge_id: string;
    /** Holder set the round runs in. */
    domain: string;
    /** Operation the round authorises. */
    op: string;
    /** Holder the challenge is for. */
    username: string;
    /** Factor the challenge must be answered with. */
    factor: string;
    /** Base64-encoded nonce to sign. */
    nonce: string;
    /** Factor-specific challenge data. */
    method_data?: Record<string, string>;
    /** RFC3339 expiration of the challenge. */
    expires_at: string;
}
export interface BarrierQuorumSubmitRequest {
    /** Holder set the round runs in. */
    domain: BarrierDomain;
    /** Operation the round authorises. */
    op: BarrierQuorumOp;
    /** Holder answering the challenge. */
    username: string;
    /** Challenge identifier from quorumBegin. */
    challenge_id: string;
    /** Factor the response was made with. */
    factor: string;
    /** Base64-encoded signed challenge response. */
    response: string;
    /** Factor-specific response data. */
    response_metadata?: Record<string, string>;
    /** Base64-encoded share the holder contributes. */
    share: string;
}
export interface BarrierQuorumOutcome {
    /** Holder set the round runs in. */
    domain: string;
    /** Operation the round authorises. */
    op: string;
    /** Shares the round needs. */
    required: number;
    /** Shares submitted so far. */
    submitted: number;
    /** True once the operation has run. */
    complete: boolean;
    /** Root token minted by a completed root-token round. */
    root_token?: string;
    /** RFC3339 time the root token was minted. */
    root_token_created_at?: string;
    /** New threshold set by a completed rekey round. */
    threshold?: number;
    /** New share total set by a completed rekey round. */
    total_shares?: number;
    /** Shares minted by a completed rekey round. */
    share_count?: number;
}
export interface BarrierRekeyStartRequest {
    /** Holder set to rekey. */
    domain: BarrierDomain;
    /** Threshold of the new split. */
    threshold: number;
    /** Quorum policy of the new split. */
    policy?: string;
    /** Security Officer PIN, when the server requires one. */
    so_pin?: string;
}
export interface BarrierQuorumRound {
    /** Holder set the round runs in. */
    domain: string;
    /** Operation the round authorises. */
    op: string;
    /** Shares the round needs. */
    required: number;
    /** Shares submitted so far. */
    submitted: number;
    /** RFC3339 expiration of the round. */
    expires_at: string;
    /** Threshold of the new split, for a rekey round. */
    new_threshold?: number;
    /** Quorum policy of the new split, for a rekey round. */
    policy?: string;
    /** Holders a rekey round removes. */
    remove?: string[];
}
export type TenantBarrierQuorumBeginRequest = Omit<BarrierQuorumBeginRequest, 'domain'>;
export type TenantBarrierQuorumSubmitRequest = Omit<BarrierQuorumSubmitRequest, 'domain'>;
/** TenantBarrierStrategy identifies how a tenant's barrier is sealed. */
export type TenantBarrierStrategy = 'none' | 'passphrase' | 'shamir';
export interface TenantBarrierInitRequest {
    /** Sealing strategy to provision for the tenant. */
    strategy: TenantBarrierStrategy;
    /** Passphrase used to derive the master key (passphrase strategy). The REST client base64-encodes it. */
    passphrase?: Uint8Array;
    /** Minimum shares required to reconstruct the master key (shamir strategy). */
    threshold?: number;
    /** Total shares to generate (shamir strategy). */
    shares?: number;
}
export interface TenantBarrierInitResponse {
    /** Tenant whose barrier was initialized. */
    tenant_id: string;
    /** Sealing strategy that was provisioned. */
    strategy: TenantBarrierStrategy;
    /** Shamir shares for distribution — returned exactly once. */
    shares: string[];
    /** Threshold required for reconstruction. */
    threshold: number;
    /** Total shares generated. */
    total_shares: number;
}
export interface TenantBarrierStatusResponse {
    /** Tenant the status describes. */
    tenant_id: string;
    /** True when the tenant barrier is sealed. */
    sealed: boolean;
    /** True once the tenant barrier has been initialized. */
    initialized: boolean;
    /** Active sealing strategy ("none", "passphrase", "shamir"). */
    strategy: TenantBarrierStrategy;
    /** True when the active strategy uses dedicated cryptographic hardware. */
    hardware_backed: boolean;
    /** Minimum shares required to unseal (shamir strategy). */
    shamir_threshold: number;
    /** Total shares minted by the last init (shamir strategy). */
    shamir_total: number;
}
export interface BarrierGenerateRecoveryKeysRequest {
    /** Threshold required for recovery. */
    threshold: number;
    /** Total recovery keys to generate. */
    total: number;
}
export interface BarrierRecoveryKeysResponse {
    /** Recovery key values for distribution. */
    keys: string[];
    /** Threshold required for recovery. */
    threshold: number;
    /** Total recovery keys generated. */
    total: number;
}
export interface BarrierHasRecoveryKeysResponse {
    /** True when recovery keys have been generated. */
    has_keys: boolean;
}
/**
 * BarrierEnableEncryptionRequest migrates a previously unencrypted barrier to a
 * keyed strategy. Mirrors go-qrdb transport.BarrierEnableEncryptionRequest.
 */
export interface BarrierEnableEncryptionRequest {
    /** Tenant whose dedicated barrier to migrate. Omit for the system barrier. */
    tenant_id?: string;
    /** Keyed strategy to migrate to ("passphrase" or "shamir"). Required. */
    strategy: string;
    /** Re-encryption approach ("swap" or "inplace"). The server defaults to "swap". */
    mechanism?: string;
    /** Write availability during migration ("online" or "quiesced"). The server defaults to "online". */
    availability?: string;
    /** New passphrase for a passphrase target. The REST client base64-encodes it. */
    passphrase?: Uint8Array;
    /** Shamir threshold (M) for a Shamir target. */
    shamir_threshold?: number;
    /** Shamir share count (N) for a Shamir target. */
    shamir_total?: number;
}
/**
 * BarrierEnableEncryptionResponse acknowledges that the migration has started.
 * Poll status() to observe completion. Mirrors go-qrdb
 * transport.BarrierEnableEncryptionResponse.
 */
export interface BarrierEnableEncryptionResponse {
    /** Migration state; "migration_started" when accepted. */
    status: string;
    /** Barrier the migration runs on: "system" or "tenant". */
    scope: string;
}
export interface CeremonyOfficer {
    /** Unique username for the officer. */
    username: string;
    /** PEM-encoded CSR submitted by the officer. */
    csr_pem: string;
}
export interface CeremonyInitializeRequest {
    /** Security Officer PIN. */
    so_pin: string;
    /** User PIN. */
    user_pin: string;
    /** Optional credential seal strategy override. */
    credential_seal_strategy?: string;
    /** Threshold for M-of-N enrollment. Zero means single-admin mode. */
    threshold: number;
    /** Officers participating in the ceremony. */
    officers?: CeremonyOfficer[];
    /** Optional hostname for the issued TLS certificate. */
    hostname?: string;
    /** Optional Subject Alternative Names for the issued TLS certificate. */
    sans?: string[];
}
export interface CeremonyInitializeResponse {
    /** Base64-encoded SHA-256 hash of the CA certificate's SPKI. */
    spki_pin: string;
    /** PEM-encoded CA certificate. */
    ca_cert_pem: string;
    /** PEM-encoded TLS certificate issued for the server. */
    tls_cert_pem: string;
    /** Ceremony state after initialization. */
    state: string;
    /** Number of Shamir shares generated. */
    share_count: number;
}
export interface CeremonyStatusResponse {
    /** Current ceremony state. */
    state: string;
    /** Optional base64-encoded SPKI pin once the CA is initialized. */
    spki_pin?: string;
}
export interface CeremonySignCSRRequest {
    /** Subject username for the issued certificate. */
    username: string;
    /** Security Officer PIN. */
    so_pin: string;
    /** PEM-encoded CSR. */
    csr_pem: string;
    /** Role identifier for the issued certificate. */
    role: string;
}
export interface CeremonySignCSRResponse {
    /** PEM-encoded certificate signed from the CSR. */
    cert_pem: string;
}
export type BootstrapMethod = 'auto' | 'dane' | 'noise' | 'spki';
export interface BootstrapBundleRequest {
    /** Canonical server URL to bootstrap trust against (e.g. "https://kms.example.com:8443"). */
    server_url?: string;
    /** Filter by store type: "root", "intermediate", "leaf", "end-entity", or empty. */
    store_type?: string;
    /** Filter by key algorithm: "RSA", "ECDSA", "Ed25519", or empty. */
    algorithm?: string;
    /**
     * DANE: DNSSEC zone under which TLSA records are published
     * (e.g. "_443._tcp.kms.example.com" or the bare hostname).
     * Required for dane; optional knob for auto.
     */
    hostname?: string;
    /**
     * DANE: optional recursive resolver for the TLSA lookup
     * (e.g. "8.8.8.8:53"). Defaults to the system resolver when omitted.
     */
    dns_server?: string;
    /**
     * Noise: explicit Noise bootstrap address ("host:port").
     * When empty the address is derived from server_url.
     */
    noise_addr?: string;
    /**
     * Noise: hex-encoded 32-byte Curve25519 server static public key.
     * Required for noise; optional knob for auto.
     */
    noise_static_key?: string;
    /**
     * SPKI: hex-encoded SHA-256 pin of the server certificate's
     * SubjectPublicKeyInfo (64 hex characters).
     * Required for spki; optional knob for auto.
     */
    spki_pin?: string;
}
export interface BootstrapBundleResponse {
    /** Trust bootstrap method used to retrieve this bundle. */
    method: BootstrapMethod;
    /** PEM-encoded certificate chain. */
    bundle_pem: string;
    /** Optional base64-encoded individual DER-encoded certificates. */
    certificates?: string[];
    /** MIME type of the bundle ("application/pem-certificate-chain"). */
    content_type: string;
    /** Optional metadata describing how the bundle was verified. */
    metadata?: Record<string, string>;
}
export interface ClusterShard {
    shard_id: number;
    leader_id: number;
    state: string;
}
export interface ClusterStatusResponse {
    node_id: number;
    raft_address?: string;
    leader_id?: string;
    total_nodes?: number;
    shards?: ClusterShard[];
    healthy: boolean;
    timestamp: string;
}
/** @deprecated Use BarrierStatusResponse. */
export type SealStatusResponse = BarrierStatusResponse;
/** @deprecated Use BarrierInitRequest. */
export type SealBarrierInitRequest = BarrierInitRequest;
/** @deprecated Use BarrierUnsealRequest. */
export type SealBarrierUnsealRequest = BarrierUnsealRequest;
/** @deprecated Use BarrierShamirInitRequest. */
export type SealShamirInitRequest = BarrierShamirInitRequest;
/** @deprecated Use BarrierShamirInitResponse. */
export type SealShamirInitResponse = BarrierShamirInitResponse;
/** @deprecated Use BarrierGenerateRecoveryKeysRequest. */
export type SealGenerateRecoveryKeysRequest = BarrierGenerateRecoveryKeysRequest;
/** @deprecated Use BarrierRecoveryKeysResponse. */
export type SealRecoveryKeysResponse = BarrierRecoveryKeysResponse;
export interface InferenceRequest {
    backend: string;
    model?: string;
    prompt: string;
    system_prompt?: string;
    host_affinity?: string;
    host_affinity_strict?: boolean;
    metadata?: Record<string, string>;
    stream?: boolean;
}
export interface InferenceContentBlock {
    type: string;
    text?: string;
}
export interface InferenceResponse {
    content: InferenceContentBlock[];
    model?: string;
    stop_reason?: string;
}
export interface InferenceChunk {
    delta: InferenceContentBlock[];
    model?: string;
    stop_reason?: string;
    final?: boolean;
}
export interface CredentialAssertionJSON {
    publicKey: PublicKeyCredentialRequestOptionsJSON;
    mediation?: string;
}
export interface PublicKeyCredentialRequestOptionsJSON {
    challenge: string;
    timeout?: number;
    rpId?: string;
    allowCredentials?: PublicKeyCredentialDescriptorJSON[];
    userVerification?: UserVerificationRequirement;
    extensions?: Record<string, unknown>;
}
export interface PublicKeyCredentialDescriptorJSON {
    id: string;
    type: 'public-key';
    transports?: AuthenticatorTransport[];
}
export interface PublicKeyCredentialJSON {
    id: string;
    rawId: string;
    type: string;
    authenticatorAttachment?: string;
    response: AuthenticatorAssertionResponseJSON;
    clientExtensionResults?: AuthenticationExtensionsClientOutputs;
}
export interface AuthenticatorAssertionResponseJSON {
    authenticatorData: string;
    clientDataJSON: string;
    signature: string;
    userHandle?: string;
}
export type UserVerificationRequirement = 'required' | 'preferred' | 'discouraged';
export type AuthenticatorTransport = 'usb' | 'nfc' | 'ble' | 'internal' | 'hybrid';
export interface AuthTokenResponse {
    access_token: string;
    id_token?: string;
    refresh_token: string;
    token_type: string;
    expires_in: number;
    user?: IAMUser;
}
export interface WebAuthnLoginChallenge {
    session_id: string;
    rp_session_id: string;
    options: CredentialAssertionJSON;
}
export interface GetCostSettingsRequest {
    tenant_id?: string;
}
export interface SetCostSettingsRequest {
    tenant_id?: string;
    electricity_price_per_kwh?: number;
    engine_power_watts?: Record<string, number>;
}
export type ElectricityRateSource = 'tenant_settings' | 'tenant_config' | 'deployment_config' | 'default';
export interface CostSettings {
    settings: {
        tenant_id: string;
        electricity_price_per_kwh?: number;
        engine_power_watts?: Record<string, number>;
        updated_at?: string;
    };
    electricity: {
        price_per_kwh: number;
        source: ElectricityRateSource;
    };
    engine_power_watts?: Record<string, number>;
}
export interface WorkerImageAgents {
    claude_code: string;
    codex: string;
}
export interface WorkerImageCheck {
    name: string;
    passed: boolean;
    detail?: string;
}
export interface WorkerImageBound {
    image: string;
    agents: WorkerImageAgents;
    uses: number;
    stale: boolean;
}
export interface WorkerImagePromotion {
    tenant_id: string;
    agent_id: string;
    from: string[] | null;
    to: string;
    error?: string;
}
export type WorkerImageOutcome = 'current' | 'promoted' | 'partial' | 'failed';
export interface WorkerImageReport {
    trigger: string;
    outcome: WorkerImageOutcome;
    started_at: string;
    finished_at: string;
    latest: WorkerImageAgents;
    bound?: WorkerImageBound[];
    stale?: string[];
    skipped?: string[];
    candidate?: string;
    image_id?: string;
    reference?: string;
    digest?: string;
    probes?: WorkerImageCheck[];
    smokes?: WorkerImageCheck[];
    promotions?: WorkerImagePromotion[];
    error?: string;
}
export interface WorkerImageStatus {
    serving: boolean;
    running: boolean;
    running_since?: string;
    queued: boolean;
    last?: WorkerImageReport;
}
//# sourceMappingURL=types.d.ts.map