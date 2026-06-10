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
export interface GetJobStatusRequest {
    tenant_id: string;
    job_id: string;
}
export interface ListJobsRequest {
    tenant_id: string;
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
}
export interface ListTasksResponse {
    tasks: Task[];
    count: number;
}
export type AgentStatus = 'registered' | 'running' | 'idle' | 'offline' | 'error' | 'disabled';
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
    disabled_at?: number;
    version: number;
    metadata: Record<string, string>;
}
export interface DisableAgentRequest {
    tenant_id: string;
    agent_id: string;
}
export interface DisableAgentResponse {
    agent_id: string;
    disabled: boolean;
    disabled_at: string;
    timestamp: string;
}
export interface EnableAgentRequest {
    tenant_id: string;
    agent_id: string;
}
export interface EnableAgentResponse {
    agent_id: string;
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
    /** auto_spawn is true when the task was created with immediate dispatch. */
    auto_spawn?: boolean;
    /** spawned_job_id is the ID of the auto-spawned job, empty when not spawned. */
    spawned_job_id?: string;
    /** spawned_at is a nanosecond Unix timestamp of when the job was dispatched. */
    spawned_at?: number;
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
export interface SpawnSessionRequest {
    tenantID: string;
    backend: string;
    project_id?: string;
    model?: string;
    work_dir?: string;
    env?: Record<string, string>;
    sandbox_mode?: SandboxModeValue;
    sandboxed?: boolean;
    host_affinity?: string;
    host_affinity_strict?: boolean;
}
export interface SpawnSessionResponse {
    session_id: string;
    backend: string;
    container_id?: string;
}
export interface PromptSessionRequest {
    session_id: string;
    prompt: string;
}
export interface PromptSessionResponse {
    session_id: string;
    response: string;
    done: boolean;
}
export interface CloseSessionRequest {
    session_id: string;
}
export interface CloseSessionResponse {
    session_id: string;
    closed: boolean;
}
export interface ListSessionsRequest {
    tenantID: string;
    backend?: string;
    status?: string;
}
export interface SessionStatusResponse {
    session_id: string;
    backend: string;
    status: string;
    container_id?: string;
    created_at: number;
    last_active: number;
}
export interface ListSessionsResponse {
    sessions: SessionStatusResponse[];
    count: number;
}
export interface GetAgentRequest {
    tenant_id: string;
    agent_id: string;
}
export interface ListAgentsRequest {
    tenant_id: string;
    host?: string;
    status?: string;
    agent_type?: string;
    backend?: string;
    model?: string;
}
export interface ListAgentsResponse {
    agents: AgentRegistration[];
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
export interface CompleteTaskRequest {
    tenant_id: string;
    task_id: string;
    result?: {
        output?: string;
        metadata?: Record<string, string>;
    };
}
export interface RegisterAgentRequest {
    tenant_id: string;
    name: string;
    host: string;
    backend: string;
    agent_type: string;
    model?: string;
    metadata?: Record<string, string>;
}
export interface RegisterAgentResponse {
    agent: AgentRegistration;
}
export interface RemoveAgentRequest {
    tenant_id: string;
    agent_id: string;
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
}
export interface AddSSHPinRequest {
    /** Scope that owns this pin. */
    scope?: SSHScope;
    /** Owner within the scope. */
    owner?: string;
    /** Hostname or IP address to pin. */
    host: string;
    /** OpenSSH SHA256 fingerprint. */
    fingerprint: string;
    /** Algorithm of the host key. */
    algorithm?: string;
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
export interface DeployAgentSpec {
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
    agent_bind: string;
    backends: string[];
    binary: string;
    install_dir?: string;
    data_dir?: string;
    /** Tri-state: undefined=auto-detect, true=systemd, false=nohup. */
    systemd?: boolean;
    /** Nanoseconds (Go time.Duration). Zero disables the online wait. */
    wait_online?: number;
}
export interface DeployAgentResult {
    host: string;
    installed_path: string;
    service_status: string;
    agent_id?: string;
    online_at?: string;
    /** Path to the mTLS client certificate installed on the remote host. */
    agent_cert_path?: string;
    /** Hex-encoded serial number of the issued agent certificate. */
    agent_cert_serial?: string;
}
export interface IAMUser {
    id: string;
    tenant_id: string;
    username: string;
    email: string;
    enabled?: boolean;
    roles: string[];
    groups: string[];
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
    created_at: string;
    updated_at: string;
}
export interface ListRolesResponse {
    roles: IAMRole[];
    count: number;
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
    /** Passphrase used to derive the master key. */
    secret: string;
}
export interface BarrierUnsealRequest {
    /** Passphrase used to unseal the barrier. */
    secret: string;
}
export interface BarrierShamirInitRequest {
    /** Optional passphrase used to derive the master key. */
    secret: string;
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
export interface BarrierUnsealShareRequest {
    /** Single Shamir share. */
    share: string;
}
export interface BarrierUnsealShareResponse {
    /** Number of shares required to unseal. */
    required: number;
    /** Number of shares submitted so far. */
    submitted: number;
    /** True when the quorum threshold has been met. */
    complete: boolean;
}
export interface BarrierUnsealSharesRequest {
    /** Complete set of Shamir shares. */
    shares: string[];
}
export interface BarrierShamirSharesResponse {
    /** Number of shares currently in storage. */
    count: number;
    /** Minimum shares required for reconstruction. */
    threshold: number;
    /** Total shares generated. */
    total: number;
}
export interface BarrierRekeyRequest {
    /** New threshold value. */
    threshold: number;
    /** New total share count. */
    total: number;
}
export interface BarrierRekeyResponse {
    /** New Shamir shares. */
    shares: string[];
    /** New threshold value. */
    threshold: number;
    /** New total share count. */
    total_shares: number;
}
/** TenantBarrierStrategy identifies how a tenant's barrier is sealed. */
export type TenantBarrierStrategy = 'none' | 'passphrase' | 'shamir';
export interface TenantBarrierInitRequest {
    /** Sealing strategy to provision for the tenant. */
    strategy: TenantBarrierStrategy;
    /** Passphrase used to derive the master key (passphrase strategy). */
    passphrase?: string;
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
export interface TenantBarrierUnsealShareResponse {
    /** Tenant whose barrier received the share. */
    tenant_id: string;
    /** Number of shares required to unseal. */
    required: number;
    /** Number of shares submitted so far. */
    submitted: number;
    /** True when the quorum threshold has been met. */
    complete: boolean;
    /** True while the tenant barrier remains sealed. */
    sealed: boolean;
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
export interface BarrierRecoverWithKeysRequest {
    /** Recovery keys used to unseal. */
    keys: string[];
}
export interface BarrierHasRecoveryKeysResponse {
    /** True when recovery keys have been generated. */
    has_recovery_keys: boolean;
}
export interface BarrierGenerateRootTokenRequest {
    /** Shamir shares proving knowledge of the master key. */
    shares: string[];
}
export interface BarrierRootTokenResponse {
    /** Hex-encoded root token. */
    token: string;
    /** RFC3339 timestamp when the token was generated. */
    created_at: string;
}
export interface BarrierShamirDeleteShareRequest {
    /** Zero-based share index. */
    index: number;
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
    /** PEM-encoded Security Officer certificate. */
    so_cert_pem?: string;
    /** PEM-encoded Security Officer private key. */
    so_key_pem?: string;
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
export interface CeremonyClaimCertBeginRequest {
    /** Officer username. */
    username: string;
}
export interface CeremonyClaimCertBeginResponse {
    /** Hex-encoded random nonce the officer must sign. */
    nonce_hex: string;
    /** RFC3339 expiration timestamp for the nonce. */
    expires_at: string;
}
export interface CeremonyClaimCertCompleteRequest {
    /** Officer username. */
    username: string;
    /** Hex-encoded nonce from CeremonyClaimCertBegin. */
    nonce_hex: string;
    /** Base64-encoded signature over the nonce. */
    signature: string;
}
export interface CeremonyClaimCertCompleteResponse {
    /** PEM-encoded officer certificate. */
    cert_pem: string;
    /** PEM-encoded CA certificate for trust chain verification. */
    ca_cert_pem: string;
}
export interface CeremonyClaimShareRequest {
    /** Officer username. */
    username: string;
}
export interface CeremonyClaimShareResponse {
    /** Base64-encoded Shamir share material. */
    share: string;
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
export interface CeremonyEnrollExternalOfficerRequest {
    /** Officer username. */
    username: string;
    /** PEM-encoded X.509 certificate. Mutually exclusive with pubkey_pem. */
    cert_pem?: string;
    /** PEM-encoded SPKI public key. Mutually exclusive with cert_pem. */
    pubkey_pem?: string;
    /** One-based Shamir share index assigned to this officer. */
    share_index: number;
    /** Auth factor identifier: "mtls", "webauthn", or "piv". */
    auth_factor: string;
    /** Optional plaintext share to seal to the officer's public key. */
    share_bytes?: string;
}
export interface CeremonyEnrollExternalOfficerResponse {
    /** Base64-encoded sealed share envelope. Empty when no share was sealed. */
    sealed_share?: string;
}
export type BootstrapMethod = 'auto' | 'dane' | 'noise' | 'spki';
export interface BootstrapBundleRequest {
    /** Filter by store type: "root", "intermediate", "leaf", "end-entity", or empty. */
    store_type?: string;
    /** Filter by key algorithm: "RSA", "ECDSA", "Ed25519", or empty. */
    algorithm?: string;
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
/** @deprecated Use BarrierUnsealShareRequest. */
export type SealUnsealShareRequest = BarrierUnsealShareRequest;
/** @deprecated Use BarrierUnsealShareResponse. */
export type SealUnsealShareResponse = BarrierUnsealShareResponse;
/** @deprecated Use BarrierRekeyRequest. */
export type SealRekeyRequest = BarrierRekeyRequest;
/** @deprecated Use BarrierRekeyResponse. */
export type SealRekeyResponse = BarrierRekeyResponse;
/** @deprecated Use BarrierGenerateRecoveryKeysRequest. */
export type SealGenerateRecoveryKeysRequest = BarrierGenerateRecoveryKeysRequest;
/** @deprecated Use BarrierRecoveryKeysResponse. */
export type SealRecoveryKeysResponse = BarrierRecoveryKeysResponse;
/** @deprecated Use BarrierRootTokenResponse. */
export type SealRootTokenResponse = BarrierRootTokenResponse;
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
    rawId: number[];
    type: string;
    response: AuthenticatorAssertionResponseJSON;
    clientExtensionResults?: Record<string, unknown>;
}
export interface AuthenticatorAssertionResponseJSON {
    authenticatorData: number[];
    clientDataJSON: number[];
    signature: number[];
    userHandle: number[] | null;
}
export type UserVerificationRequirement = 'required' | 'preferred' | 'discouraged';
export type AuthenticatorTransport = 'usb' | 'nfc' | 'ble' | 'internal' | 'hybrid';
export interface AuthTokenResponse {
    access_token: string;
    id_token: string;
    refresh_token: string;
    token_type: string;
    expires_in: number;
    user?: IAMUser;
}
export interface PublicTenant {
    id: string;
    name: string;
    display_name: string;
}
//# sourceMappingURL=types.d.ts.map