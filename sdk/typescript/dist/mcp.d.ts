import type { AuthClient, BarrierClient, CeremonyClient, Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
import type { BarrierGenerateRecoveryKeysRequest, BarrierGenerateRootTokenRequest, BarrierEnableEncryptionRequest, BarrierEnableEncryptionResponse, BarrierHasRecoveryKeysResponse, BarrierInitRequest, BarrierRecoverWithKeysRequest, BarrierRecoveryKeysResponse, BarrierRekeyRequest, BarrierRekeyResponse, BarrierRootTokenResponse, BarrierShamirInitRequest, BarrierShamirInitResponse, BarrierStatusResponse, BarrierUnsealRequest, BarrierSealRequest, BarrierUnsealShareRequest, BarrierUnsealShareResponse, BarrierUnsealSharesRequest, CeremonyClaimCertBeginRequest, CeremonyClaimCertBeginResponse, CeremonyClaimCertCompleteRequest, CeremonyClaimCertCompleteResponse, CeremonyClaimShareBeginRequest, CeremonyClaimShareBeginResponse, CeremonyClaimShareCompleteRequest, CeremonyClaimShareCompleteResponse, CeremonyEnrollExternalOfficerRequest, CeremonyEnrollExternalOfficerResponse, CeremonyInitializeRequest, CeremonyInitializeResponse, CeremonySignCSRRequest, CeremonySignCSRResponse, CeremonyStatusResponse, ClientConfig } from './types.js';
import type { Account, AccountsClient, CreateAccountRequest, CreateAccountResponse, DeleteAccountResponse, GetAccountResponse, GetAccountUsageResponse, ListAccountsRequest, ListAccountsResponse, RevokeAccountRequest, RevokeAccountResponse, RotateAccountCredentialRequest, RotateAccountCredentialResponse, SetDefaultAccountResponse, UpdateAccountRequest, UpdateAccountResponse } from './accounts.js';
import type { CreateProjectRequest, CreateProjectResponse, DeleteProjectResponse, GetProjectResponse, GrantFleetNodeRoleRequest, GrantFleetNodeRoleResponse, ListProjectsResponse, Project, ProjectsClient, RevokeFleetNodeRoleRequest, RevokeFleetNodeRoleResponse, UpdateProjectRequest, UpdateProjectResponse } from './projects.js';
import type { SdlcClient, ProjectBinding, Delivery, OnboardingReport, ConformanceReport, OpenWork, ReviewerSelection, FileIssueRequest, ForgeIssue, CollaboratorsResponse, AdoptIssueRequest, Adoption } from './sdlc.js';
import { McpSession } from './mcp_rpc.js';
import { type ProjectPageParams, type AccountPageParams, type PageResult } from './page.js';
import { QuicBaaSError } from './errors.js';
export declare class McpNotImplementedError extends QuicBaaSError {
    constructor(method: string);
}
export declare class McpBarrierClient implements BarrierClient {
    private readonly session;
    constructor(session: McpSession);
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
export declare class McpCeremonyClient implements CeremonyClient {
    private readonly session;
    constructor(session: McpSession);
    initialize(req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse>;
    status(): Promise<CeremonyStatusResponse>;
    claimCertBegin(req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse>;
    claimCertComplete(req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse>;
    claimShareBegin(req: CeremonyClaimShareBeginRequest): Promise<CeremonyClaimShareBeginResponse>;
    claimShareComplete(req: CeremonyClaimShareCompleteRequest): Promise<CeremonyClaimShareCompleteResponse>;
    signCSR(req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;
    enrollExternalOfficer(req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse>;
}
export declare class McpProjectsClient implements ProjectsClient {
    private readonly session;
    constructor(session: McpSession);
    createProject(req: CreateProjectRequest): Promise<CreateProjectResponse>;
    getProject(tenantId: string, projectId: string): Promise<GetProjectResponse>;
    updateProject(req: UpdateProjectRequest): Promise<UpdateProjectResponse>;
    deleteProject(tenantId: string, projectId: string): Promise<DeleteProjectResponse>;
    listProjects(tenantId: string): Promise<ListProjectsResponse>;
    listProjectsPage(tenantId: string, page?: ProjectPageParams): Promise<PageResult<Project>>;
    grantFleetNodeRole(req: GrantFleetNodeRoleRequest): Promise<GrantFleetNodeRoleResponse>;
    revokeFleetNodeRole(req: RevokeFleetNodeRoleRequest): Promise<RevokeFleetNodeRoleResponse>;
}
export declare class McpSdlcClient implements SdlcClient {
    private readonly session;
    constructor(session: McpSession);
    forges(): Promise<string[]>;
    bind(tenantId: string, projectId: string, binding: ProjectBinding, paused?: boolean): Promise<ProjectBinding>;
    unbind(tenantId: string, projectId: string): Promise<void>;
    binding(tenantId: string, projectId: string): Promise<ProjectBinding>;
    delivery(tenantId: string, taskId: string): Promise<Delivery>;
    onboard(tenantId: string, projectId: string): Promise<OnboardingReport>;
    conformance(tenantId: string, projectId: string): Promise<ConformanceReport>;
    openWork(tenantId: string, projectId: string): Promise<OpenWork>;
    reviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection>;
    setReviewer(tenantId: string, projectId: string, pullRequest: number, profile: string): Promise<ReviewerSelection>;
    clearReviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection>;
    fileIssue(tenantId: string, projectId: string, req: FileIssueRequest): Promise<ForgeIssue>;
    collaborators(tenantId: string, projectId: string): Promise<CollaboratorsResponse>;
    adoptIssue(tenantId: string, projectId: string, request: AdoptIssueRequest): Promise<Adoption>;
}
export declare class McpAccountsClient implements AccountsClient {
    private readonly session;
    constructor(session: McpSession);
    createAccount(req: CreateAccountRequest): Promise<CreateAccountResponse>;
    getAccount(tenantId: string, accountId: string): Promise<GetAccountResponse>;
    listAccounts(req: ListAccountsRequest): Promise<ListAccountsResponse>;
    listAccountsPage(req: AccountPageParams & {
        tenant_id: string;
    }): Promise<PageResult<Account>>;
    updateAccount(req: UpdateAccountRequest): Promise<UpdateAccountResponse>;
    deleteAccount(tenantId: string, accountId: string): Promise<DeleteAccountResponse>;
    rotateAccountCredential(req: RotateAccountCredentialRequest): Promise<RotateAccountCredentialResponse>;
    revokeAccount(req: RevokeAccountRequest): Promise<RevokeAccountResponse>;
    setDefaultAccount(tenantId: string, accountId: string): Promise<SetDefaultAccountResponse>;
    getAccountUsage(tenantId: string, accountId: string): Promise<GetAccountUsageResponse>;
}
export declare class McpClient implements Client {
    readonly connection: ConnectionClient;
    readonly orchestrator: OrchestratorClient;
    readonly auth: AuthClient;
    readonly data: any;
    readonly vectors: any;
    readonly timeseries: any;
    readonly objects: any;
    readonly rag: any;
    readonly embeddings: any;
    readonly messaging: any;
    readonly scheduler: any;
    readonly projects: ProjectsClient;
    readonly sdlc: SdlcClient;
    readonly accounts: AccountsClient;
    readonly agents: any;
    readonly iam: any;
    readonly kms: any;
    readonly secrets: any;
    readonly ca: any;
    readonly trust: any;
    readonly crypto: any;
    readonly dek: any;
    readonly attestation: any;
    readonly dynamicSecrets: any;
    readonly barrier: BarrierClient;
    readonly tenantBarrier: any;
    readonly ceremony: CeremonyClient;
    readonly cluster: any;
    readonly seal: any;
    readonly sshKeys: any;
    readonly ssh: any;
    constructor(endpoint: string, config?: ClientConfig);
    logout(_token?: string): Promise<void>;
}
//# sourceMappingURL=mcp.d.ts.map