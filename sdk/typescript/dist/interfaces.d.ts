import type { FleetNodePageParams, AttentionPageParams, JobPageParams, PageResult, SessionPageParams, TaskPageParams, WorkflowPageParams } from './page.js';
import type { RAGClient } from './rag.js';
import type { EmbeddingsClient } from './embedding_sdk.js';
import type { MessagingClient } from './messaging_sdk.js';
import type { SchedulerClient } from './scheduler_sdk.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
import type { AccountsClient } from './accounts.js';
import type { AgentsClient } from './agents.js';
import type { CAClient } from './ca.js';
import type { TrustClient } from './trust.js';
import type { CryptoClient } from './crypto.js';
import type { DEKClient } from './dek.js';
import type { AttestationClient } from './attestation.js';
import type { DynamicSecretsClient } from './dynamicsecrets.js';
import type { AgentBackendInfo, JobRunnerInfo, AuthTokenResponse, DeployFleetNodeResult, DeployFleetNodeSpec, InferenceRequest, InferenceResponse, InferenceChunk, PublicKeyCredentialJSON, WebAuthnLoginChallenge, SSHKey, SSHScope, CreateSSHKeyRequest, ImportSSHKeyRequest, ListSSHKeysQuery, ListSSHKeysResponse, SSHPublicKey, SignRequest, SignResponse, AddSSHPinRequest, ListSSHPinsResponse, VerifySSHPinRequest, EmitAuthorizedRequest, EmitAuthorizedResponse, VerifyAuthorizedRequest, VerifyAuthorizedResponse, FleetNode, SessionStatusResponse, CloseSessionRequest, CloseSessionResponse, CreateKeyRequest, CreateKeyResponse, CreateTenantRequest, CreateTenantResponse, CreateTaskRequest, CreateTaskResponse, CreateUserRequest, CreateUserResponse, CreateWorkflowRequest, CreateWorkflowResponse, DecomposeTaskRequest, DecomposeTaskResponse, GetFleetNodeRequest, GetSecretResponse, GetSessionHistoryRequest, GetSessionConversationRequest, GetSessionConversationResponse, GetSpendReportRequest, GetSpendReportResponse, GetTenantSpendPolicyRequest, SetTenantSpendPolicyRequest, TenantSpendPolicy, GetCostSettingsRequest, SetCostSettingsRequest, CostSettings, WorkerImageStatus, GetSessionHistoryResponse, HealthCheckReport, IAMRole, IAMPolicy, IAMTenant, IAMUser, JobEvent, JobStatus, KMSDecryptRequest, KMSDecryptResponse, KMSEncryptRequest, KMSEncryptResponse, ListFleetNodesRequest, ListFleetNodesResponse, ListKeysResponse, ListPoliciesResponse, ListRolesResponse, ListSecretsResponse, ListSessionsRequest, ListSessionsResponse, ListTenantsResponse, ListUsersResponse, ListWorkflowsResponse, PromptSessionRequest, PromptSessionResponse, ReconcileRolesResponse, RegisterFleetNodeRequest, RegisterFleetNodeResponse, RemoveFleetNodeRequest, DisableFleetNodeRequest, DisableFleetNodeResponse, EnableFleetNodeRequest, EnableFleetNodeResponse, RekeyRequest, RekeyResponse, RotateKeyResponse, RotateRootKeyResponse, RotateSecretResponse, RotateSessionAccountRequest, RotateSessionAccountResponse, ListSecretVersionsResponse, SetDefaultKeyResponse, SealStatus, BarrierStatusResponse, BarrierInitRequest, BarrierUnsealRequest, BarrierSealRequest, BarrierShamirInitRequest, BarrierShamirInitResponse, BarrierUnsealShareRequest, BarrierUnsealShareResponse, BarrierUnsealSharesRequest, BarrierRekeyRequest, BarrierRekeyResponse, BarrierGenerateRecoveryKeysRequest, BarrierRecoveryKeysResponse, BarrierRecoverWithKeysRequest, BarrierHasRecoveryKeysResponse, BarrierGenerateRootTokenRequest, BarrierEnableEncryptionRequest, BarrierEnableEncryptionResponse, BarrierRootTokenResponse, TenantBarrierInitRequest, TenantBarrierInitResponse, TenantBarrierUnsealShareResponse, TenantBarrierStatusResponse, CeremonyInitializeRequest, CeremonyInitializeResponse, CeremonyStatusResponse, CeremonyClaimCertBeginRequest, CeremonyClaimCertBeginResponse, CeremonyClaimCertCompleteRequest, CeremonyClaimCertCompleteResponse, CeremonyClaimShareBeginRequest, CeremonyClaimShareBeginResponse, CeremonyClaimShareCompleteRequest, CeremonyClaimShareCompleteResponse, CeremonySignCSRRequest, CeremonySignCSRResponse, CeremonyEnrollExternalOfficerRequest, CeremonyEnrollExternalOfficerResponse, BootstrapBundleRequest, BootstrapBundleResponse, ClusterStatusResponse, SessionEventEnvelope, SessionInfo, SpawnSessionRequest, SpawnSessionResponse, ClaimTaskRequest, CompleteTaskRequest, DeleteTaskRequest, DeleteTaskResponse, StartJobRequest, StartJobResponse, StoreSecretRequest, SubscribeSessionEventsRequest, Task, UnsealRequest, UnsealResponse, WorkflowDefinition, AttentionItem, AttentionResolveRequest } from './types.js';
export interface ConnectionClient {
    connect(address: string, transport?: string, tenantID?: string): Promise<void>;
    disconnect(): Promise<void>;
    isConnected(): boolean;
    getAddress(): string;
    getTransport(): string;
}
export interface OrchestratorClient {
    listJobsPage(tenantID: string, page?: JobPageParams): Promise<PageResult<JobStatus>>;
    listTasksPage(tenantID: string, page?: TaskPageParams): Promise<PageResult<Task>>;
    listSessionsPage(tenantID: string, page?: SessionPageParams): Promise<PageResult<SessionStatusResponse>>;
    listFleetNodesPage(tenantID: string, page?: FleetNodePageParams): Promise<PageResult<FleetNode>>;
    listWorkflowsPage(tenantID: string, page?: WorkflowPageParams): Promise<PageResult<WorkflowDefinition>>;
    listJobs(tenantID: string, taskID?: string): Promise<JobStatus[]>;
    getJobStatus(tenantID: string, jobID: string): Promise<JobStatus>;
    startJob(req: StartJobRequest): Promise<StartJobResponse>;
    cancelJob(tenantID: string, jobID: string): Promise<void>;
    listFleetNodes(req: ListFleetNodesRequest): Promise<ListFleetNodesResponse>;
    getFleetNode(req: GetFleetNodeRequest): Promise<FleetNode>;
    listSessions(req: ListSessionsRequest): Promise<ListSessionsResponse>;
    listTasks(tenantID: string): Promise<Task[]>;
    getTask(tenantID: string, taskID: string): Promise<Task>;
    listBackends(): Promise<AgentBackendInfo[]>;
    listRunners(): Promise<JobRunnerInfo[]>;
    listBackendModels(nodeID: string, backendName: string): Promise<string[]>;
    healthCheck(): Promise<HealthCheckReport>;
    subscribeJobEvents(tenantID: string, jobID: string, onEvent: (event: JobEvent) => void, onError?: () => void): () => void;
    spawnSession(req: SpawnSessionRequest): Promise<SpawnSessionResponse>;
    promptSession(req: PromptSessionRequest): Promise<PromptSessionResponse>;
    closeSession(req: CloseSessionRequest): Promise<CloseSessionResponse>;
    rotateSessionAccount(req: RotateSessionAccountRequest): Promise<RotateSessionAccountResponse>;
    getSessionHistory(req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse>;
    getSessionConversation(req: GetSessionConversationRequest): Promise<GetSessionConversationResponse>;
    getSpendReport(req: GetSpendReportRequest): Promise<GetSpendReportResponse>;
    getTenantSpendPolicy(req: GetTenantSpendPolicyRequest): Promise<TenantSpendPolicy>;
    setTenantSpendPolicy(req: SetTenantSpendPolicyRequest): Promise<TenantSpendPolicy>;
    getCostSettings(req: GetCostSettingsRequest): Promise<CostSettings>;
    setCostSettings(req: SetCostSettingsRequest): Promise<CostSettings>;
    getWorkerImageStatus(): Promise<WorkerImageStatus>;
    refreshWorkerImage(): Promise<WorkerImageStatus>;
    subscribeSession(req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope>;
    createWorkflow(req: CreateWorkflowRequest): Promise<CreateWorkflowResponse>;
    listWorkflows(tenantID: string): Promise<ListWorkflowsResponse>;
    getWorkflow(tenantID: string, name: string): Promise<WorkflowDefinition>;
    updateWorkflow(name: string, def: WorkflowDefinition): Promise<WorkflowDefinition>;
    deleteWorkflow(tenantID: string, name: string): Promise<void>;
    createTask(req: CreateTaskRequest): Promise<CreateTaskResponse>;
    claimTask(req: ClaimTaskRequest): Promise<void>;
    completeTask(req: CompleteTaskRequest): Promise<void>;
    readmitTask(tenantID: string, taskID: string): Promise<void>;
    deleteTask(req: DeleteTaskRequest): Promise<DeleteTaskResponse>;
    listAttention(tenantID: string, page?: AttentionPageParams): Promise<PageResult<AttentionItem>>;
    resolveAttention(req: AttentionResolveRequest): Promise<AttentionItem>;
    decomposeTask(req: DecomposeTaskRequest): Promise<DecomposeTaskResponse>;
    registerFleetNode(req: RegisterFleetNodeRequest): Promise<RegisterFleetNodeResponse>;
    removeFleetNode(req: RemoveFleetNodeRequest): Promise<void>;
    disableFleetNode(req: DisableFleetNodeRequest): Promise<DisableFleetNodeResponse>;
    enableFleetNode(req: EnableFleetNodeRequest): Promise<EnableFleetNodeResponse>;
    deployFleetNode(spec: DeployFleetNodeSpec): Promise<DeployFleetNodeResult>;
    inference(req: InferenceRequest): Promise<InferenceResponse>;
    inferenceStream(req: InferenceRequest, signal?: AbortSignal): AsyncIterable<InferenceChunk>;
}
export interface SelfRegisterBeginRequest {
    tenant_name: string;
    email: string;
    display_name: string;
    isolation_mode?: string;
}
export interface SelfRegisterBeginResponse {
    public_key_credential_creation_options: Record<string, unknown>;
    session_id: string;
}
export interface SelfRegisterFinishRequest {
    session_id: string;
    credential: Record<string, unknown>;
}
export interface SelfRegisterFinishResponse {
    status: 'active' | 'pending' | 'verification_required';
    tenant_id: string;
    user_id: string;
    access_token?: string;
    refresh_token?: string;
    message?: string;
}
export interface AuthClient {
    authValidate(token?: string): Promise<boolean>;
    authLogin(credentials: {
        username: string;
        password: string;
    }): Promise<void>;
    webAuthnBeginLogin(tenantID: string, email: string): Promise<WebAuthnLoginChallenge>;
    webAuthnFinishLogin(challenge: WebAuthnLoginChallenge, credential: PublicKeyCredentialJSON): Promise<AuthTokenResponse>;
    loginPIV(): Promise<AuthTokenResponse>;
    registerBegin(req: SelfRegisterBeginRequest): Promise<SelfRegisterBeginResponse>;
    registerFinish(req: SelfRegisterFinishRequest): Promise<SelfRegisterFinishResponse>;
    logout(token?: string): Promise<void>;
}
export interface KVEntry {
    key: string;
    value: unknown;
}
export interface KVListResponse {
    entries: KVEntry[];
    cursor?: string;
}
export interface DataClient {
    kvList(entityType: string): Promise<KVListResponse>;
    kvGet(entityType: string, key: string): Promise<KVEntry>;
    kvPut(entityType: string, key: string, value: unknown): Promise<void>;
    kvDelete(entityType: string, key: string): Promise<void>;
}
export interface VectorNamespace {
    name: string;
    dimension?: number;
    metric?: string;
    vector_count?: number;
    created_at?: string;
}
export interface VectorSearchResult {
    id: string;
    score: number;
    metadata?: Record<string, unknown>;
}
export interface VectorsClient {
    listNamespaces(): Promise<VectorNamespace[]>;
    createNamespace(req: {
        name: string;
        dimension: number;
        metric?: string;
    }): Promise<VectorNamespace>;
    deleteNamespace(namespace: string): Promise<void>;
    searchSimilar(namespace: string, req: {
        vector?: number[];
        text?: string;
        top_k?: number;
        filter?: Record<string, unknown>;
    }): Promise<VectorSearchResult[]>;
}
export interface TSPoint {
    metric: string;
    timestamp: string;
    value: number;
    tags?: Record<string, string>;
}
export interface TSQueryResponse {
    points: TSPoint[];
    count: number;
}
export interface TimeSeriesClient {
    write(point: TSPoint): Promise<void>;
    query(req: {
        metric: string;
        start?: string;
        end?: string;
        tags?: Record<string, string>;
        limit?: number;
    }): Promise<TSQueryResponse>;
    listMetrics(): Promise<string[]>;
    deleteSeries(metric: string): Promise<void>;
}
export interface ObjectEntry {
    key: string;
    size?: number;
    content_type?: string;
    created_at?: string;
    tags?: Record<string, string>;
}
export interface ObjectListResponse {
    objects: ObjectEntry[];
    cursor?: string;
}
export interface ObjectsClient {
    listObjects(prefix?: string): Promise<ObjectListResponse>;
    getObject(key: string): Promise<Blob>;
    putObject(key: string, body: Blob | ArrayBuffer, contentType?: string): Promise<void>;
    deleteObject(key: string): Promise<void>;
}
export interface UpdateUserRequest {
    email?: string;
    display_name?: string;
    /** lifecycle transitions the account; the server refuses an illegal move. */
    lifecycle?: import('./types.js').UserLifecycle;
    auth_methods?: string[];
}
export interface UpdateTenantRequest {
    name?: string;
    description?: string;
}
export interface CreateRoleRequest {
    name: string;
    description?: string;
    permissions?: string[];
}
export interface UpdateRoleRequest {
    description?: string;
    permissions?: string[];
}
export interface IAMAuditActor {
    user_id?: string;
    username?: string;
    email?: string;
    session_id?: string;
    client_id?: string;
    impersonating_user_id?: string;
    impersonating_username?: string;
    service_identity?: string;
    jti?: string;
    scopes?: string[];
    roles?: string[];
}
export interface IAMAuditResource {
    type: string;
    id: string;
    name?: string;
}
export interface IAMAuditEvent {
    id: string;
    tenant_id: string;
    timestamp: string;
    event_type: string;
    category: string;
    actor?: IAMAuditActor;
    resource?: IAMAuditResource;
    action: string;
    outcome: string;
    details?: Record<string, unknown>;
    ip_address?: string;
    user_agent?: string;
    impersonation_session_id?: string;
    correlation_id?: string;
}
export interface ListIAMAuditEventsOptions {
    tenantID?: string;
    eventType?: string;
    category?: string;
    outcome?: string;
    actorID?: string;
    resourceID?: string;
    startTime?: string;
    endTime?: string;
    limit?: number;
    offset?: number;
}
export interface ListIAMAuditEventsResponse {
    events: IAMAuditEvent[];
    total: number;
    limit: number;
    offset: number;
}
export interface RecordIAMAuditEventRequest {
    tenant_id?: string;
    event: IAMAuditEvent;
    /** When true the server records the event asynchronously and returns an empty event_id. */
    async?: boolean;
}
export interface RecordIAMAuditEventResponse {
    event_id: string;
}
export interface DeleteIAMAuditEventsResponse {
    deleted_count: number;
    before: string;
}
export type IAMProviderType = 'oidc' | 'saml' | 'oauth2' | 'ldap';
export interface RegisterIAMProviderRequest {
    /** Optional provider ID; the server generates one when omitted. */
    id?: string;
    name: string;
    type: IAMProviderType;
    enabled: boolean;
    client_id: string;
    client_secret: string;
    /** Issuer URL (the response spells it `issuer_url`). */
    issuer?: string;
    auth_url?: string;
    token_url?: string;
    userinfo_url?: string;
    jwks_url?: string;
    scopes?: string[];
    /** Claim mapping (the response spells it `claim_mappings`). */
    claim_mapping?: Record<string, string>;
    metadata?: Record<string, unknown>;
}
export interface UpdateIAMProviderRequest {
    name?: string;
    enabled?: boolean;
    client_id?: string;
    client_secret?: string;
    /** Issuer URL (the response spells it `issuer_url`). */
    issuer?: string;
    auth_url?: string;
    token_url?: string;
    userinfo_url?: string;
    jwks_url?: string;
    scopes?: string[];
    /** Claim mapping (the response spells it `claim_mappings`). */
    claim_mapping?: Record<string, string>;
    metadata?: Record<string, unknown>;
}
export interface IAMExternalProvider {
    id: string;
    tenant_id: string;
    name: string;
    type: IAMProviderType;
    enabled: boolean;
    client_id: string;
    /** Always sent empty by the server; the secret is never returned. */
    client_secret: string;
    /** Issuer URL (requests spell it `issuer`). */
    issuer_url: string;
    auth_url?: string;
    token_url?: string;
    userinfo_url?: string;
    jwks_url?: string;
    scopes: string[];
    /** Claim mappings (requests spell it `claim_mapping`). */
    claim_mappings?: Record<string, unknown>;
    auto_provision: boolean;
    allow_linking: boolean;
    default_roles?: string[];
    default_scopes?: string[];
    allowed_email_domains?: string[];
    require_explicit_role_mapping?: boolean;
    created: string;
    updated: string;
}
export interface ListIAMProvidersResponse {
    providers: IAMExternalProvider[];
    limit: number;
    offset: number;
}
export interface ListIAMProvidersOptions {
    limit?: number;
    offset?: number;
    type?: IAMProviderType;
}
export interface CreatePolicyRequest {
    name: string;
    description?: string;
    spec_yaml?: string;
}
export interface UpdatePolicyRequest {
    description?: string;
    spec_yaml?: string;
}
export interface IAMSubject {
    id: string;
    name?: string;
    email?: string;
    tenant?: string;
    tenant_id: string;
    roles?: string[];
    groups?: string[];
    authenticated: boolean;
    mfa_verified?: boolean;
    mfa_timestamp?: string;
    last_login?: string;
}
export interface IAMCheckRequest {
    subject: IAMSubject;
    action: string;
    resource: string;
}
export interface IAMCheckResponse {
    allowed: boolean;
    matched_policy?: string;
    matched_rule?: string;
    reason?: string;
}
export interface IAMPolicyDocument {
    apiVersion: string;
    kind: string;
    metadata: {
        name: string;
        tenant: string;
        description?: string;
        labels?: Record<string, string>;
        priority?: number;
        created?: string;
        updated?: string;
    };
    spec: {
        subjects: Record<string, unknown>;
        variables?: Record<string, string>;
        rules: unknown[];
    };
    load_error?: {
        reason: string;
        unknown_keys?: string[];
    };
}
export interface IAMPolicyValidationResult {
    valid: boolean;
    errors?: string[];
}
export interface IAMPolicySimulationRequest {
    policy: IAMPolicyDocument;
    check_request: IAMCheckRequest;
    include_trace?: boolean;
}
export interface IAMEvaluationStep {
    step_number: number;
    policy_name: string;
    rule_name?: string;
    action: string;
    result: boolean;
    details?: string;
}
export interface IAMPolicySimulationResult {
    allowed: boolean;
    matched_policy?: string;
    matched_rule?: string;
    evaluation_steps?: IAMEvaluationStep[];
    reason?: string;
}
export interface IAMEffectivePermissionsRequest {
    subject: IAMSubject;
    resource_patterns: string[];
    actions?: string[];
    include_deny_details?: boolean;
}
export interface IAMPermissionSet {
    allowed: string[];
    denied?: string[];
    sources?: Record<string, string[]>;
}
export interface IAMEffectivePermissionsResult {
    subject: IAMSubject;
    permissions: Record<string, IAMPermissionSet>;
    total_policies_evaluated: number;
}
export type ConsentPurpose = 'marketing' | 'analytics' | 'personalization' | 'third_party' | 'data_processing' | 'data_retention' | 'functional';
export type ConsentStatus = 'granted' | 'denied' | 'withdrawn' | 'pending';
export interface UserConsent {
    id: string;
    user_id: string;
    tenant_id: string;
    purpose: ConsentPurpose;
    status: ConsentStatus;
    version: string;
    granted_at?: string;
    withdrawn_at?: string;
    expires_at?: string;
    ip_address?: string;
    user_agent?: string;
    metadata?: Record<string, string>;
    created: string;
    updated: string;
}
export interface GrantConsentRequest {
    purpose: ConsentPurpose;
    version: string;
    expires_at?: string;
    ip_address?: string;
    user_agent?: string;
}
export interface WithdrawConsentRequest {
    ip_address?: string;
    user_agent?: string;
}
export type DataExportFormat = 'json' | 'csv' | 'xml';
export type DataExportStatus = 'pending' | 'processing' | 'completed' | 'failed' | 'expired';
export type DeletionStatus = 'pending' | 'processing' | 'completed' | 'failed' | 'cancelled';
export interface RequestDataExportRequest {
    format?: DataExportFormat;
    requested_by?: string;
    include_categories?: string[];
    exclude_categories?: string[];
}
export interface DataExportRequest {
    id: string;
    user_id: string;
    tenant_id: string;
    requested_by: string;
    format: DataExportFormat;
    status: DataExportStatus;
    include_categories?: string[];
    exclude_categories?: string[];
    requested_at: string;
    started_at?: string;
    completed_at?: string;
    expires_at?: string;
    download_url?: string;
    file_size_bytes?: number;
    error_message?: string;
    metadata?: Record<string, string>;
}
export interface ExportMetadata {
    export_id: string;
    user_id: string;
    tenant_id: string;
    exported_at: string;
    format: DataExportFormat;
    categories: string[];
    schema_version: string;
}
export interface UserDataExport {
    export_metadata: ExportMetadata;
    profile?: {
        id: string;
        email: string;
        name: string;
        attributes?: Record<string, string>;
        created: string;
        updated: string;
        last_auth?: string;
    };
    sessions?: Array<{
        id: string;
        started_at: string;
        expires_at: string;
        last_active: string;
        ip_address?: string;
        user_agent?: string;
        auth_method: string;
        ended_at?: string;
    }>;
    consents?: Array<{
        purpose: ConsentPurpose;
        status: ConsentStatus;
        version: string;
        granted_at?: string;
        withdrawn_at?: string;
    }>;
    roles?: Array<{
        role_id: string;
        role_name: string;
        assigned_at: string;
    }>;
    audit_logs?: Array<{
        timestamp: string;
        action: string;
        resource: string;
        ip_address?: string;
        success: boolean;
    }>;
    credentials?: Array<{
        id: string;
        type: string;
        name?: string;
        created_at: string;
        last_used_at: string;
        attestation?: string;
        transports?: string[];
    }>;
    mfa_info?: {
        enabled: boolean;
        method: string;
        enrolled_at?: string;
        backup_codes_remaining: number;
    };
}
export interface RequestDeletionRequest {
    requested_by?: string;
    reason?: string;
    delete_categories?: string[];
    preserve_categories?: string[];
    grace_period_days?: number;
}
export interface DeletionSummary {
    profile_deleted: boolean;
    sessions_deleted: number;
    audit_logs_anonymized: number;
    consents_deleted: number;
    credentials_deleted: number;
    role_assignments_removed: number;
    tokens_revoked: number;
    additional_data?: Record<string, number>;
}
export interface DeletionRequest {
    id: string;
    user_id: string;
    tenant_id: string;
    requested_by: string;
    reason?: string;
    status: DeletionStatus;
    delete_categories?: string[];
    preserve_categories?: string[];
    requested_at: string;
    grace_period_ends_at?: string;
    started_at?: string;
    completed_at?: string;
    cancelled_at?: string;
    cancelled_by?: string;
    deleted_data_summary?: DeletionSummary;
    error_message?: string;
    metadata?: Record<string, string>;
}
export interface IAMSessionDeviceInfo {
    id: string;
    name: string;
    type: string;
    fingerprint: string;
    trusted: boolean;
}
export interface IAMSession {
    id: string;
    created: string;
    last_active: string;
    expires_at: string;
    mfa_verified: boolean;
    ip_address: string;
    user_agent: string;
    device_info: IAMSessionDeviceInfo | null;
}
export interface IAMApplication {
    client_id: string;
    name: string;
    tenant_id: string;
    client_secret?: string;
    redirect_uris: string[];
    resource_prefix?: string;
    scopes?: string[];
    default_audience?: string;
    grant_types: string[];
    response_types: string[];
    token_endpoint_auth_method?: string;
    mtls_cert_thumbprint?: string;
    mtls_subject_dn?: string;
    mtls_san_dns?: string;
    created: string;
    updated: string;
}
export interface RegisterIAMAppRequest {
    name: string;
    redirect_uris: string[];
    resource_prefix?: string;
    grant_types?: string[];
    response_types?: string[];
    pkce_required: boolean;
    token_endpoint_auth_method?: string;
}
export interface UpdateIAMAppRequest {
    name?: string;
    redirect_uris?: string[];
    grant_types?: string[];
    response_types?: string[];
    resource_prefix?: string;
    default_audience?: string;
    scopes?: string[];
    token_endpoint_auth_method?: string;
}
export interface ListIAMAppsOptions {
    limit?: number;
    offset?: number;
}
export interface ListIAMAppsResponse {
    applications: IAMApplication[];
    apps: IAMApplication[];
    limit: number;
    offset: number;
}
export interface WebAuthnCredentialSummary {
    id: string;
    aaguid?: string;
    attestation_type?: string;
    transports?: string[];
    sign_count: number;
    backup_eligible: boolean;
    backup_state: boolean;
    clone_warning: boolean;
}
export interface ListWebAuthnCredentialsResponse {
    credentials: WebAuthnCredentialSummary[];
    total: number;
}
export interface DeleteWebAuthnCredentialsResponse {
    deleted: number;
}
export interface CreateAccountLinkRequest {
    provider_id: string;
    external_id: string;
    access_token?: string;
}
export interface AccountLink {
    id: string;
    user_id: string;
    provider_id: string;
    provider_name: string;
    external_id: string;
    email?: string;
    metadata?: Record<string, unknown>;
    created_at: string;
    updated_at: string;
}
export interface FederatedIdentity {
    id: string;
    user_id: string;
    tenant_id: string;
    provider_id: string;
    external_id: string;
    external_email?: string;
    external_name?: string;
    claims?: Record<string, unknown>;
    last_login: string;
    created: string;
    updated: string;
}
export interface ListAccountLinksOptions {
    limit?: number;
    offset?: number;
}
export interface ListAccountLinksResponse {
    links: FederatedIdentity[];
    limit: number;
    offset: number;
}
export interface ListSessionsOptions {
    user_id?: string;
    tenant_id?: string;
}
export interface ListIAMSessionsResponse {
    sessions: IAMSession[];
}
export interface RevokeAllSessionsOptions {
    except?: string;
    user_id?: string;
    tenant_id?: string;
}
export interface RevokeOtherSessionsResponse {
    message: string;
    kept_session_id: string;
}
export interface MFAStatusResponse {
    enabled: boolean;
    method?: string;
    enrolled_at?: string;
    last_verified?: string;
    backup_codes_total?: number;
    backup_codes_remaining?: number;
}
export interface MFAEnrollResponse {
    enrollment_id: string;
    provisioning_uri: string;
    secret: string;
    expires_at: string;
}
export interface MFACompleteResponse {
    success: boolean;
}
export interface MFAChallengeResponse {
    challenge_id: string;
    method: string;
    expires_at: string;
}
export interface MFAVerifyResponse {
    success: boolean;
    token?: string;
    backup_codes_remaining?: number;
}
export interface MFABackupCodesResponse {
    codes: string[];
}
export interface MFABackupCodeStatusResponse {
    total_codes: number;
    remaining_codes: number;
    generated_at?: string;
}
export interface TenantQuotas {
    max_databases: number;
    max_raft_groups: number;
    max_replicas_per_group: number;
    max_total_replicas: number;
    max_collections: number;
    max_keys: number;
    max_storage_bytes: number;
    max_database_size_bytes: number;
    max_users: number;
    max_roles: number;
    max_certificates: number;
    max_backups: number;
    max_vector_dims: number;
    max_ts_metrics: number;
    max_dedicated_wal_groups: number;
    system_barrier_quota: number;
    current_databases?: number;
    current_raft_groups?: number;
    current_total_replicas?: number;
    current_collections?: number;
    current_keys?: number;
    current_storage_bytes?: number;
    current_database_size_bytes?: number;
    current_users?: number;
    current_roles?: number;
    current_certificates?: number;
    current_backups?: number;
    current_vector_dims?: number;
    current_ts_metrics?: number;
    current_dedicated_wal_groups?: number;
    current_system_barrier_bytes?: number;
}
export interface TenantEntitlements {
    allowed_isolation_modes?: string[];
    allow_dedicated_raft?: boolean;
    allow_volume_storage?: boolean;
    allow_system_managed_barrier?: boolean;
    allow_dedicated_wal?: boolean;
    allowed_barrier_modes?: string[];
}
export type TenantAdminEnrollment = 'cert' | 'invite';
export interface ProvisionTenantRequest {
    tenant_id: string;
    name?: string;
    isolation_type?: string;
    admin_username?: string;
    admin_enrollment?: TenantAdminEnrollment;
    admin_csr_pem?: string;
    invite_expiry_hours?: number;
}
export interface ProvisionTenantResponse {
    tenant_id: string;
    admin_user_id: string;
    admin_username: string;
    admin_role_id: string;
    admin_pending: boolean;
    admin_cert_pem?: string;
    admin_cert_thumbprint?: string;
    invite_token?: string;
    invite_expires_at?: string;
    timestamp: string;
}
export interface IAMClient {
    listUsers(tenantID: string): Promise<ListUsersResponse>;
    createUser(tenantID: string, req: CreateUserRequest): Promise<CreateUserResponse>;
    updateUser(tenantID: string, userID: string, req: UpdateUserRequest): Promise<IAMUser>;
    deleteUser(tenantID: string, userID: string): Promise<void>;
    assignRoleToUser(tenantID: string, userID: string, role: string): Promise<void>;
    revokeRoleFromUser(tenantID: string, userID: string, role: string): Promise<void>;
    listSessions(opts?: ListSessionsOptions): Promise<ListIAMSessionsResponse>;
    getSession(sessionID: string): Promise<IAMSession>;
    revokeSession(sessionID: string): Promise<void>;
    revokeAllSessions(opts?: RevokeAllSessionsOptions): Promise<void>;
    revokeOtherSessions(keepSessionID: string): Promise<RevokeOtherSessionsResponse>;
    listWebAuthnCredentials(tenantID: string, userID: string): Promise<ListWebAuthnCredentialsResponse>;
    deleteAllWebAuthnCredentials(tenantID: string, userID: string): Promise<DeleteWebAuthnCredentialsResponse>;
    deleteWebAuthnCredential(tenantID: string, userID: string, credentialID: string): Promise<void>;
    createAccountLink(tenantID: string, userID: string, req: CreateAccountLinkRequest): Promise<AccountLink>;
    listAccountLinks(tenantID: string, userID: string, opts?: ListAccountLinksOptions): Promise<ListAccountLinksResponse>;
    deleteAccountLink(tenantID: string, userID: string, providerID: string): Promise<void>;
    getMFAStatus(tenantID: string, userID: string): Promise<MFAStatusResponse>;
    disableMFA(tenantID: string, userID: string): Promise<void>;
    beginMFAEnrollment(tenantID: string, userID: string, method?: string): Promise<MFAEnrollResponse>;
    completeMFAEnrollment(tenantID: string, userID: string, enrollmentID: string, code: string): Promise<MFACompleteResponse>;
    cancelMFAEnrollment(tenantID: string, userID: string, enrollmentID: string): Promise<void>;
    beginMFAChallenge(tenantID: string, userID: string, sessionToken: string): Promise<MFAChallengeResponse>;
    verifyMFAChallenge(tenantID: string, userID: string, challengeID: string, code: string): Promise<MFAVerifyResponse>;
    getBackupCodeStatus(tenantID: string, userID: string): Promise<MFABackupCodeStatusResponse>;
    generateBackupCodes(tenantID: string, userID: string): Promise<MFABackupCodesResponse>;
    listTenants(): Promise<ListTenantsResponse>;
    createTenant(req: CreateTenantRequest): Promise<CreateTenantResponse>;
    updateTenant(tenantID: string, req: UpdateTenantRequest): Promise<IAMTenant>;
    deleteTenant(tenantID: string): Promise<void>;
    getTenant(tenantID: string): Promise<IAMTenant>;
    getUser(tenantID: string, userID: string): Promise<IAMUser>;
    listUserSessions(tenantID: string, userID: string, limit?: number): Promise<ListIAMSessionsResponse>;
    revokeAllUserSessions(tenantID: string, userID: string): Promise<void>;
    listApps(tenantID: string, opts?: ListIAMAppsOptions): Promise<ListIAMAppsResponse>;
    registerApp(tenantID: string, req: RegisterIAMAppRequest): Promise<IAMApplication>;
    getApp(tenantID: string, clientID: string): Promise<IAMApplication>;
    updateApp(tenantID: string, clientID: string, req: UpdateIAMAppRequest): Promise<IAMApplication>;
    deleteApp(tenantID: string, clientID: string): Promise<void>;
    rotateAppSecret(tenantID: string, clientID: string): Promise<IAMApplication>;
    updateTenantQuotas(tenantID: string, quotas: TenantQuotas): Promise<void>;
    updateTenantEntitlements(tenantID: string, entitlements: TenantEntitlements): Promise<void>;
    provisionTenant(req: ProvisionTenantRequest): Promise<ProvisionTenantResponse>;
    listRoles(tenantID: string): Promise<ListRolesResponse>;
    createRole(tenantID: string, req: CreateRoleRequest): Promise<IAMRole>;
    updateRole(tenantID: string, roleID: string, req: UpdateRoleRequest): Promise<IAMRole>;
    /** listAuditEvents returns one page of IAM audit events matching the filters. */
    listAuditEvents(opts?: ListIAMAuditEventsOptions): Promise<ListIAMAuditEventsResponse>;
    /** getAuditEvent returns one IAM audit event. */
    getAuditEvent(eventID: string, tenantID?: string): Promise<IAMAuditEvent>;
    /** recordAuditEvent records one IAM audit event (201); event_id is empty when async. */
    recordAuditEvent(req: RecordIAMAuditEventRequest): Promise<RecordIAMAuditEventResponse>;
    /** deleteAuditEventsBefore prunes IAM audit events older than `before` (RFC 3339). */
    deleteAuditEventsBefore(before: string, tenantID?: string): Promise<DeleteIAMAuditEventsResponse>;
    getRole(tenantID: string, roleID: string): Promise<IAMRole>;
    registerProvider(tenantID: string, req: RegisterIAMProviderRequest): Promise<IAMExternalProvider>;
    listProviders(tenantID: string, opts?: ListIAMProvidersOptions): Promise<ListIAMProvidersResponse>;
    getProvider(tenantID: string, providerID: string): Promise<IAMExternalProvider>;
    updateProvider(tenantID: string, providerID: string, req: UpdateIAMProviderRequest): Promise<IAMExternalProvider>;
    deleteProvider(tenantID: string, providerID: string): Promise<void>;
    deleteRole(tenantID: string, roleID: string): Promise<void>;
    reconcileRoles(tenantID: string): Promise<ReconcileRolesResponse>;
    listPolicies(tenantID: string): Promise<ListPoliciesResponse>;
    createPolicy(tenantID: string, req: CreatePolicyRequest): Promise<IAMPolicy>;
    updatePolicy(tenantID: string, policyName: string, req: UpdatePolicyRequest): Promise<IAMPolicy>;
    deletePolicy(tenantID: string, policyName: string): Promise<void>;
    getPolicy(tenantID: string, policyName: string): Promise<IAMPolicyDocument>;
    validatePolicy(tenantID: string, req: CreatePolicyRequest, name?: string): Promise<IAMPolicyValidationResult>;
    testPolicy(tenantID: string, req: IAMCheckRequest): Promise<IAMCheckResponse>;
    simulatePolicy(tenantID: string, req: IAMPolicySimulationRequest): Promise<IAMPolicySimulationResult>;
    getEffectivePermissions(tenantID: string, req: IAMEffectivePermissionsRequest): Promise<IAMEffectivePermissionsResult>;
    getInvite(tenantID: string, token: string): Promise<import('./types.js').InviteMetadata>;
    activateFIDO2Begin(tenantID: string, token: string): Promise<Record<string, unknown>>;
    activateFIDO2Finish(tenantID: string, token: string, credential: Record<string, unknown>): Promise<import('./types.js').ActivationResponse>;
    activatePIV(tenantID: string, token: string, certPEM: string): Promise<import('./types.js').ActivationResponse>;
    grantConsent(tenantID: string, userID: string, req: GrantConsentRequest): Promise<UserConsent>;
    listUserConsents(tenantID: string, userID: string): Promise<UserConsent[]>;
    getConsent(tenantID: string, userID: string, purpose: ConsentPurpose): Promise<UserConsent>;
    withdrawConsent(tenantID: string, userID: string, purpose: ConsentPurpose, req?: WithdrawConsentRequest): Promise<UserConsent>;
    requestDataExport(tenantID: string, userID: string, req?: RequestDataExportRequest): Promise<DataExportRequest>;
    getExportStatus(tenantID: string, userID: string, exportID: string): Promise<DataExportRequest>;
    downloadExport(tenantID: string, userID: string, exportID: string): Promise<UserDataExport>;
    requestDeletion(tenantID: string, userID: string, req?: RequestDeletionRequest): Promise<DeletionRequest>;
    getDeletionStatus(tenantID: string, userID: string, deletionID: string): Promise<DeletionRequest>;
    cancelDeletion(tenantID: string, userID: string, deletionID: string, cancelledBy?: string): Promise<DeletionRequest>;
}
export interface KMSClient {
    listKeys(limit?: number): Promise<ListKeysResponse>;
    createKey(req: CreateKeyRequest): Promise<CreateKeyResponse>;
    rotateKey(name: string): Promise<RotateKeyResponse>;
    deleteKey(name: string): Promise<void>;
    encrypt(req: KMSEncryptRequest): Promise<KMSEncryptResponse>;
    decrypt(req: KMSDecryptRequest): Promise<KMSDecryptResponse>;
    listKeyVersions(name: string): Promise<KMSKeyVersion[]>;
    updateKeyStatus(name: string, status: 'enabled' | 'disabled'): Promise<void>;
    setDefaultKey(name: string): Promise<SetDefaultKeyResponse>;
}
export interface KMSKeyVersion {
    version: number;
    status: string;
    created_at?: string;
    destroyed_at?: string;
}
export interface SecretsClient {
    listSecrets(limit?: number): Promise<ListSecretsResponse>;
    storeSecret(req: StoreSecretRequest): Promise<void>;
    getSecret(path: string): Promise<GetSecretResponse>;
    deleteSecret(path: string): Promise<void>;
    rotateSecret(path: string): Promise<RotateSecretResponse>;
    listVersions(path: string): Promise<ListSecretVersionsResponse>;
}
export interface BarrierClient {
    status(): Promise<BarrierStatusResponse>;
    initialize(req: BarrierInitRequest): Promise<void>;
    unseal(req: BarrierUnsealRequest): Promise<void>;
    seal(req?: BarrierSealRequest): Promise<void>;
    initializeShamir(req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse>;
    unsealWithShare(req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse>;
    unsealWithShares(req: BarrierUnsealSharesRequest): Promise<void>;
    rekey(req: BarrierRekeyRequest): Promise<BarrierRekeyResponse>;
    generateRecoveryKeys(req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse>;
    recoverWithKeys(req: BarrierRecoverWithKeysRequest): Promise<void>;
    deleteRecoveryKeys(): Promise<void>;
    recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse>;
    generateRootToken(req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse>;
    enableEncryption(req: BarrierEnableEncryptionRequest): Promise<BarrierEnableEncryptionResponse>;
}
export interface TenantBarrierClient {
    tenantBarrierInit(tenantId: string, req: TenantBarrierInitRequest): Promise<TenantBarrierInitResponse | undefined>;
    tenantBarrierUnseal(tenantId: string, key: Uint8Array): Promise<void>;
    tenantBarrierUnsealShare(tenantId: string, share: string): Promise<TenantBarrierUnsealShareResponse>;
    tenantBarrierSeal(tenantId: string): Promise<void>;
    tenantBarrierStatus(tenantId: string): Promise<TenantBarrierStatusResponse>;
}
/**
 * SealClient is the legacy alias for BarrierClient. Its surface is preserved
 * for one release so existing callers continue to compile. New code must use
 * BarrierClient directly.
 *
 * The runtime RestSealClient wraps RestBarrierClient and emits a single
 * deprecation warning per construction. See rest.ts for the implementation.
 *
 * @deprecated Use BarrierClient. The Seal* surface will be removed one
 *   release after the Barrier unification ships.
 */
export interface SealClient {
    status(): Promise<SealStatus>;
    unseal(req: UnsealRequest): Promise<UnsealResponse>;
    seal(): Promise<void>;
    rekey(req: RekeyRequest): Promise<RekeyResponse>;
    rotateRootKey(): Promise<RotateRootKeyResponse>;
}
export interface CeremonyClient {
    initialize(req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse>;
    status(): Promise<CeremonyStatusResponse>;
    claimCertBegin(req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse>;
    claimCertComplete(req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse>;
    claimShareBegin(req: CeremonyClaimShareBeginRequest): Promise<CeremonyClaimShareBeginResponse>;
    claimShareComplete(req: CeremonyClaimShareCompleteRequest): Promise<CeremonyClaimShareCompleteResponse>;
    signCSR(req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;
    enrollExternalOfficer(req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse>;
}
export interface ClusterClient {
    status(): Promise<ClusterStatusResponse>;
}
export interface BootstrapClient {
    auto(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    dane(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    noise(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    spki(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
}
export interface SSHKeysClient {
    list(): Promise<ListSSHKeysResponse>;
    create(req: CreateSSHKeyRequest): Promise<SSHKey>;
    get(name: string): Promise<SSHKey>;
    remove(name: string): Promise<void>;
    listScoped(query?: ListSSHKeysQuery): Promise<ListSSHKeysResponse>;
    getScoped(scope: SSHScope, owner: string, name: string): Promise<SSHKey>;
    deleteScoped(scope: SSHScope, owner: string, name: string): Promise<void>;
    exportPublic(scope: SSHScope, owner: string, name: string): Promise<SSHPublicKey>;
    sign(scope: SSHScope, owner: string, name: string, req: SignRequest): Promise<SignResponse>;
    import_(req: ImportSSHKeyRequest): Promise<SSHKey>;
}
export interface SSHKnownHostsClient {
    add(req: AddSSHPinRequest): Promise<void>;
    list(query?: ListSSHKeysQuery): Promise<ListSSHPinsResponse>;
    remove(host: string, query?: ListSSHKeysQuery): Promise<void>;
    verify(host: string, req: VerifySSHPinRequest): Promise<void>;
}
export interface SSHAuthorizedKeysClient {
    emit(req: EmitAuthorizedRequest): Promise<EmitAuthorizedResponse>;
    verify(req: VerifyAuthorizedRequest): Promise<VerifyAuthorizedResponse>;
}
export interface SSHClient {
    /** SSH key management. */
    readonly keys: SSHKeysClient;
    /** Host-key pin store (MITM-safe SSH). */
    readonly knownHosts: SSHKnownHostsClient;
    /** Authorized-keys entry management. */
    readonly authorized: SSHAuthorizedKeysClient;
}
export interface Client {
    readonly connection: ConnectionClient;
    readonly orchestrator: OrchestratorClient;
    readonly auth: AuthClient;
    readonly data: DataClient;
    readonly vectors: VectorsClient;
    readonly timeseries: TimeSeriesClient;
    readonly objects: ObjectsClient;
    readonly rag: RAGClient;
    readonly embeddings: EmbeddingsClient;
    readonly messaging: MessagingClient;
    readonly scheduler: SchedulerClient;
    readonly projects: ProjectsClient;
    /**
     * sdlc is the SDLC module of the Software Factory: a project's forge
     * binding and a task's delivery.
     */
    readonly sdlc: SdlcClient;
    /**
     * accounts manages the pooled provider account pool (Anthropic/Claude,
     * OpenAI/Codex, OpenCode) an agent session can be spawned against.
     */
    readonly accounts: AccountsClient;
    /**
     * agents manages the tenant's AI Agents — named runtime + model + provider
     * that role slots reference — and the pools slots fail over along.
     */
    readonly agents: AgentsClient;
    readonly iam: IAMClient;
    readonly kms: KMSClient;
    readonly secrets: SecretsClient;
    /**
     * ca exposes certificate authority administration (issue, sign-csr,
     * revoke, verify, CA info/bundle/list, CRL generation). Mirrors the CLI's
     * `quicbaas ca` command group plus sign-csr/verify/CRL.
     */
    readonly ca: CAClient;
    /**
     * trust exposes cross-sign root pool retrieval, AIA auto-import, and DANE
     * publication — a pure composition surface over the xkms TrustedCA.
     */
    readonly trust: TrustClient;
    /**
     * crypto exposes low-level cryptographic primitives (encrypt, decrypt,
     * rewrap, sign, verify, hash, random, data-key generation) backed by
     * KMS-held keys.
     */
    readonly crypto: CryptoClient;
    /**
     * dek manages envelope-encryption Data Encryption Keys: creation,
     * rotation, deletion, default selection, AEAD safety limits, and
     * background re-encryption. Paths: /api/v1/kms/deks/*.
     */
    readonly dek: DEKClient;
    /**
     * Attestation verifier: endorsements (reference-value signers and AK
     * roots) and signed CoRIM reference values.
     * Paths: /api/v1/attestation/endorsements/* and
     * /api/v1/attestation/reference-values/*.
     */
    readonly attestation: AttestationClient;
    /**
     * dynamicSecrets manages credential-generation roles, on-demand
     * short-lived credential minting, and lease renewal/revocation.
     * Paths: /api/v1/secrets/dynamic/*.
     */
    readonly dynamicSecrets: DynamicSecretsClient;
    /**
     * barrier is the canonical surface for sealed-storage operations.
     * Replaces the legacy seal accessor; new code must use barrier.
     */
    readonly barrier: BarrierClient;
    /**
     * tenantBarrier operates the per-tenant cryptographic barrier for tenants
     * that select the "barrier" isolation mode. It is scoped per tenant ID and
     * is independent of the process-wide system barrier exposed by `barrier`.
     */
    readonly tenantBarrier: TenantBarrierClient;
    /**
     * ceremony drives the initialization ceremony lifecycle (M-of-N officer
     * enrollment, certificate claims, share distribution, CSR signing).
     */
    readonly ceremony: CeremonyClient;
    /**
     * cluster exposes the Raft cluster control plane (status today; membership
     * and rebalance writes when a caller needs them). Path: /api/v1/cluster.
     */
    readonly cluster: ClusterClient;
    /**
     * @deprecated Use barrier. Kept for one release so existing callers
     * continue to compile. The seal accessor proxies to barrier.
     */
    readonly seal: SealClient;
    /**
     * sshKeys is the legacy flat accessor for backward compatibility.
     * Existing callers using client.sshKeys.list()/create()/get()/remove()
     * continue to work unchanged.
     */
    readonly sshKeys: SSHKeysClient;
    /**
     * ssh exposes the full scoped SSH surface introduced in Phase 2b.
     * client.ssh.keys  → SSHKeysClient (same instance as sshKeys)
     * client.ssh.knownHosts → SSHKnownHostsClient
     * client.ssh.authorized → SSHAuthorizedKeysClient
     */
    readonly ssh: SSHClient;
}
export type { SessionInfo };
//# sourceMappingURL=interfaces.d.ts.map