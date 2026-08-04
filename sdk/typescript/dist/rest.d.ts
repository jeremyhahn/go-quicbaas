import type { AuthClient, Client, ConnectionClient, DataClient, IAMClient, KMSClient, ObjectsClient, BarrierClient, TenantBarrierClient, CeremonyClient, BootstrapClient, ClusterClient, SealClient, SecretsClient, SSHKeysClient, SSHKnownHostsClient, SSHAuthorizedKeysClient, SSHClient, TimeSeriesClient, VectorsClient, OrchestratorClient } from './interfaces.js';
import type { RAGClient } from './rag.js';
import type { EmbeddingsClient } from './embedding_sdk.js';
import type { MessagingClient } from './messaging_sdk.js';
import type { SchedulerClient } from './scheduler_sdk.js';
import type { ProjectsClient } from './projects.js';
import type { ClientConfig, BarrierStatusResponse, BarrierInitRequest, BarrierUnsealRequest, BarrierShamirInitRequest, BarrierShamirInitResponse, BarrierUnsealShareRequest, BarrierUnsealShareResponse, BarrierUnsealSharesRequest, BarrierShamirSharesResponse, BarrierShamirDeleteShareRequest, BarrierRekeyRequest, BarrierRekeyResponse, BarrierGenerateRecoveryKeysRequest, BarrierRecoveryKeysResponse, BarrierRecoverWithKeysRequest, BarrierHasRecoveryKeysResponse, BarrierGenerateRootTokenRequest, BarrierRootTokenResponse, TenantBarrierInitRequest, TenantBarrierInitResponse, TenantBarrierUnsealShareResponse, TenantBarrierStatusResponse, CeremonyInitializeRequest, CeremonyInitializeResponse, CeremonyStatusResponse, CeremonyClaimCertBeginRequest, CeremonyClaimCertBeginResponse, CeremonyClaimCertCompleteRequest, CeremonyClaimCertCompleteResponse, CeremonyClaimShareRequest, CeremonyClaimShareResponse, CeremonySignCSRRequest, CeremonySignCSRResponse, CeremonyEnrollExternalOfficerRequest, CeremonyEnrollExternalOfficerResponse, BootstrapBundleRequest, BootstrapBundleResponse, ClusterStatusResponse, SessionInfo, SSHScope, SSHKey, SSHPublicKey, SignRequest, SignResponse, CreateSSHKeyRequest, ImportSSHKeyRequest, ListSSHKeysQuery, ListSSHKeysResponse, AddSSHPinRequest, ListSSHPinsResponse, VerifySSHPinRequest, EmitAuthorizedRequest, EmitAuthorizedResponse, VerifyAuthorizedRequest, VerifyAuthorizedResponse } from './types.js';
import { APIError, ConnectionError, NotConnectedError, SSHError, SSHNotFoundError, SSHDuplicateError, SSHInvalidError, SSHUnavailableError, SSHServerError, SSHHostMismatchError } from './errors.js';
export declare class RestBarrierClient implements BarrierClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
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
export declare class RestTenantBarrierClient implements TenantBarrierClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    private base;
    tenantBarrierInit(tenantId: string, req: TenantBarrierInitRequest): Promise<TenantBarrierInitResponse | undefined>;
    tenantBarrierUnseal(tenantId: string, key: string): Promise<void>;
    tenantBarrierUnsealShare(tenantId: string, share: string): Promise<TenantBarrierUnsealShareResponse>;
    tenantBarrierSeal(tenantId: string): Promise<void>;
    tenantBarrierStatus(tenantId: string): Promise<TenantBarrierStatusResponse>;
}
export declare class RestCeremonyClient implements CeremonyClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    initialize(req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse>;
    status(): Promise<CeremonyStatusResponse>;
    claimCertBegin(req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse>;
    claimCertComplete(req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse>;
    claimShare(req: CeremonyClaimShareRequest): Promise<CeremonyClaimShareResponse>;
    signCSR(req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;
    enrollExternalOfficer(req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse>;
}
export declare class RestClusterClient implements ClusterClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    status(): Promise<ClusterStatusResponse>;
}
export declare class RestBootstrapClient implements BootstrapClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    private fetchMethod;
    auto(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    dane(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    noise(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    spki(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
}
export declare class RestSSHKeysClient implements SSHKeysClient {
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    private authHeaders;
    private fetchSSH;
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
export declare class RestKnownHostsClient implements SSHKnownHostsClient {
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    private authHeaders;
    private fetchSSH;
    add(req: AddSSHPinRequest): Promise<void>;
    list(query?: ListSSHKeysQuery): Promise<ListSSHPinsResponse>;
    remove(host: string, query?: ListSSHKeysQuery): Promise<void>;
    verify(host: string, req: VerifySSHPinRequest): Promise<void>;
}
export declare class RestAuthorizedKeysClient implements SSHAuthorizedKeysClient {
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    private authHeaders;
    private fetchSSH;
    emit(req: EmitAuthorizedRequest): Promise<EmitAuthorizedResponse>;
    verify(req: VerifyAuthorizedRequest): Promise<VerifyAuthorizedResponse>;
}
export declare class RestClient implements Client {
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
    readonly barrier: BarrierClient;
    readonly tenantBarrier: TenantBarrierClient;
    readonly ceremony: CeremonyClient;
    readonly bootstrap: BootstrapClient;
    readonly cluster: ClusterClient;
    /** @deprecated Use barrier. The legacy seal accessor is kept for one release. */
    readonly seal: SealClient;
    /**
     * sshKeys is the legacy backward-compat accessor.
     * Points to the same RestSSHKeysClient instance as ssh.keys.
     */
    readonly sshKeys: SSHKeysClient;
    /**
     * ssh exposes the full scoped SSH surface (keys, knownHosts, authorized).
     * client.ssh.keys is the same instance as client.sshKeys.
     */
    readonly ssh: SSHClient;
    constructor(baseUrl: string, config?: ClientConfig);
}
export { APIError, ConnectionError, NotConnectedError, SSHError, SSHNotFoundError, SSHDuplicateError, SSHInvalidError, SSHUnavailableError, SSHServerError, SSHHostMismatchError, };
export type { SessionInfo };
//# sourceMappingURL=rest.d.ts.map