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
export interface CreatePolicyRequest {
    name: string;
    description?: string;
    spec_yaml?: string;
}
export interface UpdatePolicyRequest {
    description?: string;
    spec_yaml?: string;
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
    listTenants(): Promise<ListTenantsResponse>;
    createTenant(req: CreateTenantRequest): Promise<CreateTenantResponse>;
    updateTenant(tenantID: string, req: UpdateTenantRequest): Promise<IAMTenant>;
    deleteTenant(tenantID: string): Promise<void>;
    getTenant(tenantID: string): Promise<IAMTenant>;
    getUser(tenantID: string, userID: string): Promise<IAMUser>;
    listUserSessions(tenantID: string, userID: string, limit?: number): Promise<ListIAMSessionsResponse>;
    revokeAllUserSessions(tenantID: string, userID: string): Promise<void>;
    listRoles(tenantID: string): Promise<ListRolesResponse>;
    createRole(tenantID: string, req: CreateRoleRequest): Promise<IAMRole>;
    updateRole(tenantID: string, roleID: string, req: UpdateRoleRequest): Promise<IAMRole>;
    deleteRole(tenantID: string, roleID: string): Promise<void>;
    reconcileRoles(tenantID: string): Promise<ReconcileRolesResponse>;
    listPolicies(tenantID: string): Promise<ListPoliciesResponse>;
    createPolicy(tenantID: string, req: CreatePolicyRequest): Promise<IAMPolicy>;
    updatePolicy(tenantID: string, policyName: string, req: UpdatePolicyRequest): Promise<IAMPolicy>;
    deletePolicy(tenantID: string, policyName: string): Promise<void>;
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