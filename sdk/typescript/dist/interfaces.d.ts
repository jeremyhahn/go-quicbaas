import type { RAGClient } from './rag.js';
import type { EmbeddingsClient } from './embedding_sdk.js';
import type { MessagingClient } from './messaging_sdk.js';
import type { SchedulerClient } from './scheduler_sdk.js';
import type { ProjectsClient } from './projects.js';
import type { AgentBackendInfo, AuthTokenResponse, DeployAgentResult, DeployAgentSpec, InferenceRequest, InferenceResponse, InferenceChunk, PublicKeyCredentialRequestOptionsJSON, PublicKeyCredentialJSON, PublicTenant, SSHKey, SSHScope, CreateSSHKeyRequest, ImportSSHKeyRequest, ListSSHKeysQuery, ListSSHKeysResponse, SSHPublicKey, SignRequest, SignResponse, AddSSHPinRequest, ListSSHPinsResponse, VerifySSHPinRequest, EmitAuthorizedRequest, EmitAuthorizedResponse, VerifyAuthorizedRequest, VerifyAuthorizedResponse, AgentRegistration, CloseSessionRequest, CloseSessionResponse, CreateKeyRequest, CreateKeyResponse, CreateTenantRequest, CreateTenantResponse, CreateTaskRequest, CreateTaskResponse, CreateUserRequest, CreateUserResponse, CreateWorkflowRequest, CreateWorkflowResponse, DecomposeTaskRequest, DecomposeTaskResponse, GetAgentRequest, GetSecretResponse, GetSessionHistoryRequest, GetSessionHistoryResponse, HealthCheckReport, IAMRole, IAMPolicy, IAMTenant, IAMUser, JobEvent, JobStatus, KMSDecryptRequest, KMSDecryptResponse, KMSEncryptRequest, KMSEncryptResponse, ListAgentsRequest, ListAgentsResponse, ListKeysResponse, ListPoliciesResponse, ListRolesResponse, ListSecretsResponse, ListSessionsRequest, ListSessionsResponse, ListTenantsResponse, ListUsersResponse, ListWorkflowsResponse, PromptSessionRequest, PromptSessionResponse, RegisterAgentRequest, RegisterAgentResponse, RemoveAgentRequest, DisableAgentRequest, DisableAgentResponse, EnableAgentRequest, EnableAgentResponse, RekeyRequest, RekeyResponse, RotateKeyResponse, RotateRootKeyResponse, RotateSecretResponse, ListSecretVersionsResponse, SetDefaultKeyResponse, SealStatus, BarrierStatusResponse, BarrierInitRequest, BarrierUnsealRequest, BarrierShamirInitRequest, BarrierShamirInitResponse, BarrierUnsealShareRequest, BarrierUnsealShareResponse, BarrierUnsealSharesRequest, BarrierShamirSharesResponse, BarrierShamirDeleteShareRequest, BarrierRekeyRequest, BarrierRekeyResponse, BarrierGenerateRecoveryKeysRequest, BarrierRecoveryKeysResponse, BarrierRecoverWithKeysRequest, BarrierHasRecoveryKeysResponse, BarrierGenerateRootTokenRequest, BarrierRootTokenResponse, CeremonyInitializeRequest, CeremonyInitializeResponse, CeremonyStatusResponse, CeremonyClaimCertBeginRequest, CeremonyClaimCertBeginResponse, CeremonyClaimCertCompleteRequest, CeremonyClaimCertCompleteResponse, CeremonyClaimShareRequest, CeremonyClaimShareResponse, CeremonySignCSRRequest, CeremonySignCSRResponse, CeremonyEnrollExternalOfficerRequest, CeremonyEnrollExternalOfficerResponse, BootstrapBundleRequest, BootstrapBundleResponse, SessionEventEnvelope, SessionInfo, SpawnSessionRequest, SpawnSessionResponse, ClaimTaskRequest, CompleteTaskRequest, StartJobRequest, StartJobResponse, StoreSecretRequest, SubscribeSessionEventsRequest, Task, UnsealRequest, UnsealResponse, WorkflowDefinition } from './types.js';
export interface ConnectionClient {
    connect(address: string, transport?: string, tenantID?: string): Promise<void>;
    disconnect(): Promise<void>;
    isConnected(): boolean;
    getAddress(): string;
    getTransport(): string;
}
export interface OrchestratorClient {
    listJobs(tenantID: string): Promise<JobStatus[]>;
    getJobStatus(tenantID: string, jobID: string): Promise<JobStatus>;
    startJob(req: StartJobRequest): Promise<StartJobResponse>;
    cancelJob(tenantID: string, jobID: string): Promise<void>;
    listAgents(req: ListAgentsRequest): Promise<ListAgentsResponse>;
    getAgentStatus(req: GetAgentRequest): Promise<AgentRegistration>;
    listSessions(req: ListSessionsRequest): Promise<ListSessionsResponse>;
    listTasks(tenantID: string): Promise<Task[]>;
    getTask(tenantID: string, taskID: string): Promise<Task>;
    listBackends(): Promise<AgentBackendInfo[]>;
    listBackendModels(agentID: string, backendName: string): Promise<string[]>;
    healthCheck(): Promise<HealthCheckReport>;
    subscribeJobEvents(tenantID: string, jobID: string, onEvent: (event: JobEvent) => void): () => void;
    spawnSession(req: SpawnSessionRequest): Promise<SpawnSessionResponse>;
    promptSession(req: PromptSessionRequest): Promise<PromptSessionResponse>;
    closeSession(req: CloseSessionRequest): Promise<CloseSessionResponse>;
    getSessionHistory(req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse>;
    subscribeSession(req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope>;
    createWorkflow(req: CreateWorkflowRequest): Promise<CreateWorkflowResponse>;
    listWorkflows(tenantID: string): Promise<ListWorkflowsResponse>;
    getWorkflow(tenantID: string, name: string): Promise<WorkflowDefinition>;
    updateWorkflow(name: string, def: WorkflowDefinition): Promise<WorkflowDefinition>;
    deleteWorkflow(tenantID: string, name: string): Promise<void>;
    createTask(req: CreateTaskRequest): Promise<CreateTaskResponse>;
    claimTask(req: ClaimTaskRequest): Promise<void>;
    completeTask(req: CompleteTaskRequest): Promise<void>;
    decomposeTask(req: DecomposeTaskRequest): Promise<DecomposeTaskResponse>;
    registerAgent(req: RegisterAgentRequest): Promise<RegisterAgentResponse>;
    removeAgent(req: RemoveAgentRequest): Promise<void>;
    disableAgent(req: DisableAgentRequest): Promise<DisableAgentResponse>;
    enableAgent(req: EnableAgentRequest): Promise<EnableAgentResponse>;
    deployAgent(spec: DeployAgentSpec): Promise<DeployAgentResult>;
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
    webAuthnBeginLogin(tenantID: string, email: string): Promise<PublicKeyCredentialRequestOptionsJSON>;
    webAuthnFinishLogin(tenantID: string, email: string, credential: PublicKeyCredentialJSON): Promise<AuthTokenResponse>;
    loginPIV(): Promise<AuthTokenResponse>;
    listPublicTenants(): Promise<PublicTenant[]>;
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
    enabled?: boolean;
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
export interface IAMClient {
    listUsers(tenantID: string): Promise<ListUsersResponse>;
    createUser(tenantID: string, req: CreateUserRequest): Promise<CreateUserResponse>;
    updateUser(tenantID: string, userID: string, req: UpdateUserRequest): Promise<IAMUser>;
    deleteUser(tenantID: string, userID: string): Promise<void>;
    assignRoleToUser(tenantID: string, userID: string, role: string): Promise<void>;
    revokeRoleFromUser(tenantID: string, userID: string, role: string): Promise<void>;
    listTenants(): Promise<ListTenantsResponse>;
    createTenant(req: CreateTenantRequest): Promise<CreateTenantResponse>;
    updateTenant(tenantID: string, req: UpdateTenantRequest): Promise<IAMTenant>;
    deleteTenant(tenantID: string): Promise<void>;
    listRoles(tenantID: string): Promise<ListRolesResponse>;
    createRole(tenantID: string, req: CreateRoleRequest): Promise<IAMRole>;
    updateRole(tenantID: string, roleID: string, req: UpdateRoleRequest): Promise<IAMRole>;
    deleteRole(tenantID: string, roleID: string): Promise<void>;
    listPolicies(tenantID: string): Promise<ListPoliciesResponse>;
    createPolicy(tenantID: string, req: CreatePolicyRequest): Promise<IAMPolicy>;
    updatePolicy(tenantID: string, policyName: string, req: UpdatePolicyRequest): Promise<IAMPolicy>;
    deletePolicy(tenantID: string, policyName: string): Promise<void>;
    getInvite(token: string): Promise<import('./types.js').InviteMetadata>;
    activateFIDO2Begin(token: string): Promise<Record<string, unknown>>;
    activateFIDO2Finish(token: string, credential: Record<string, unknown>): Promise<import('./types.js').ActivationResponse>;
    activatePIV(token: string, certPEM: string): Promise<import('./types.js').ActivationResponse>;
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
    seal(): Promise<void>;
    initializeShamir(req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse>;
    unsealWithShare(req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse>;
    unsealWithShares(req: BarrierUnsealSharesRequest): Promise<void>;
    shamirListShares(): Promise<BarrierShamirSharesResponse>;
    shamirDeleteShare(req: BarrierShamirDeleteShareRequest): Promise<void>;
    shamirDeleteAllShares(): Promise<void>;
    shamirVerify(): Promise<void>;
    rekey(req: BarrierRekeyRequest): Promise<BarrierRekeyResponse>;
    generateRecoveryKeys(req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse>;
    recoverWithKeys(req: BarrierRecoverWithKeysRequest): Promise<void>;
    deleteRecoveryKeys(): Promise<void>;
    recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse>;
    generateRootToken(req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse>;
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
    claimShare(req: CeremonyClaimShareRequest): Promise<CeremonyClaimShareResponse>;
    signCSR(req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;
    enrollExternalOfficer(req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse>;
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
    readonly iam: IAMClient;
    readonly kms: KMSClient;
    readonly secrets: SecretsClient;
    /**
     * barrier is the canonical surface for sealed-storage operations.
     * Replaces the legacy seal accessor; new code must use barrier.
     */
    readonly barrier: BarrierClient;
    /**
     * ceremony drives the initialization ceremony lifecycle (M-of-N officer
     * enrollment, certificate claims, share distribution, CSR signing).
     */
    readonly ceremony: CeremonyClient;
    /**
     * bootstrap retrieves the CA certificate bundle through one of the
     * supported trust bootstrap methods (auto, dane, noise, spki).
     */
    readonly bootstrap: BootstrapClient;
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