import type { AuthClient, Client, ConnectionClient, DataClient, IAMClient, KMSClient, ObjectsClient, BarrierClient, TenantBarrierClient, CeremonyClient, BootstrapClient, ClusterClient, SealClient, SecretsClient, SSHKeysClient, SSHKnownHostsClient, SSHAuthorizedKeysClient, SSHClient, TimeSeriesClient, VectorsClient, OrchestratorClient } from './interfaces.js';
import type { RAGClient } from './rag.js';
import type { EmbeddingsClient } from './embedding_sdk.js';
import type { MessagingClient } from './messaging_sdk.js';
import type { SchedulerClient } from './scheduler_sdk.js';
import type { ProjectsClient, Project, CreateProjectRequest, CreateProjectResponse, GetProjectResponse, UpdateProjectRequest, UpdateProjectResponse, DeleteProjectResponse, ListProjectsResponse, GrantFleetNodeRoleRequest, GrantFleetNodeRoleResponse, RevokeFleetNodeRoleRequest, RevokeFleetNodeRoleResponse } from './projects.js';
import type { SdlcClient, ProjectBinding, Delivery, OnboardingReport, ConformanceReport, OpenWork, ReviewerSelection, FileIssueRequest, ForgeIssue, CollaboratorsResponse, AdoptIssueRequest, Adoption } from './sdlc.js';
import type { CAClient, IssueCertificateRequest, IssueCertificateResponse, SignCSRRequest, SignCSRResponse, RevokeCertificateResponse, VerifyCertificateResponse, GetCACertificateResponse, CAInfoResponse, CABundleResponse, ListCertificatesRequest, ListCertificatesResponse, GenerateCRLResponse, IsRevokedResponse, XKMSCABundleOptions, XKMSCABundle, XKMSCACertificate, XKMSCASignCSRRequest, XKMSCASignCSRResponse, XKMSCAIssueRequest, XKMSCAIssueResponse, XKMSCARevokeResponse } from './ca.js';
import type { TrustClient, CrossSignedRootPoolResponse, AutoImportAIARequest, AutoImportAIAResponse, PublishDANERequest, PublishDANEResponse } from './trust.js';
import type { CryptoClient, CryptoEncryptRequest, CryptoEncryptResponse, CryptoDecryptRequest, CryptoDecryptResponse, CryptoRewrapRequest, CryptoRewrapResponse, CryptoSignRequest, CryptoSignResponse, CryptoVerifyRequest, CryptoVerifyResponse, CryptoHashRequest, CryptoHashResponse, CryptoRandomResponse, CryptoDataKeyRequest, CryptoDataKeyResponse } from './crypto.js';
import type { DEKClient, CreateDEKRequest, CreateDEKResponse, GetDEKResponse, ListDEKsRequest, ListDEKsResponse, RotateDEKRequest, RotateDEKResponse, DeleteDEKRequest, DeleteDEKResponse, GetDEKStatusResponse, SetDefaultDEKResponse, SetDEKSafetyRequest, SetDEKSafetyResponse, ClearDEKSafetyResponse, StartReencryptionRequest, StartReencryptionResponse, GetReencryptionProgressResponse } from './dek.js';
import type { AttestationClient, Endorsement, EndorsementKind, ListEndorsementsResponse, ListReferenceValuesResponse, PutEndorsementRequest, ReferenceValues } from './attestation.js';
import type { XKMSKeysClient, XKMSKeyInfo, XKMSListKeysResponse, XKMSGenerateKeyRequest, XKMSGenerateKeyResponse, XKMSDeleteKeyResponse, XKMSRotateKeyResponse, XKMSEncryptRequest, XKMSEncryptResponse, XKMSDecryptRequest, XKMSDecryptResponse, XKMSSignRequest, XKMSSignResponse, XKMSVerifyRequest, XKMSVerifyResponse, XKMSListKeyVersionsResponse, XKMSBulkKeyVersionsResponse, XKMSKeyVersionStatusResponse } from './xkms_keys.js';
import type { PIVClient, ListPIVSlotsResponse, GetPIVCertificateOptions, GetPIVCertificateResponse, StorePIVCertificateRequest, GeneratePIVKeyRequest, GeneratePIVKeyResponse, GeneratePIVCSRRequest, GeneratePIVCSRResponse } from './piv.js';
import type { XKMSClient, XKMSBackendInfo, XKMSListBackendsResponse, XKMSHealthResponse, XKMSSealCapabilityResponse, XKMSSealRequest, XKMSSealResponse, XKMSUnsealRequest, XKMSUnsealResponse, XKMSListCertificatesResponse, XKMSCertificateResponse } from './xkms.js';
import type { BackupsClient, BackupCreateRequest, BackupCreateResponse, BackupListOptions, BackupListResponse, BackupRestoreRequest, BackupRestoreResponse, BackupDeleteResponse, BackupVerifyResponse, BackupStatusResponse } from './backups.js';
import type { ClusterNodesClient, ListClusterNodesResponse, AddNodeRequest, AddNodeResponse, RemoveNodeResponse, ReplaceNodeRequest, ReplaceNodeResponse } from './cluster_nodes.js';
import type { EntitiesClient, EntityList, EntityListOptions, EntityGetOptions, EntityWriteOptions, EntityRecord, PutEntityResponse, DeleteEntityResponse } from './entities.js';
import type { KMSAdminClient, KMSKeyMetadataResponse, UpdateKMSKeyMetadataRequest, GetKMSKeyPolicyResponse, KMSKeyPolicyInput, SetKMSKeyPolicyResponse, GetKMSRotationPolicyResponse, KMSRotationPolicyInput, SetKMSRotationPolicyResponse, DeleteKMSRotationPolicyResponse } from './kms_admin.js';
import type { ObjectStorageAdminClient, ObjectStorageAdminCreateBackendRequest, ObjectStorageAdminCreateBackendResponse, ObjectStorageAdminUpdateBackendRequest, ObjectStorageAdminUpdateBackendResponse, ObjectStorageAdminDeleteBackendRequest, ObjectStorageAdminDeleteBackendResponse, ObjectStorageAdminBackendStatusResponse, ObjectStorageAdminCompactBackendRequest, ObjectStorageAdminCompactBackendResponse, ObjectStorageAdminStatsOptions, ObjectStorageAdminStatsResponse, ObjectStorageAdminGCRequest, ObjectStorageAdminGCResponse, ObjectStorageAdminGCProgressResponse, ObjectStorageAdminRepairRequest, ObjectStorageAdminRepairResponse } from './objectstorage_admin.js';
import type { ParametersClient, ParameterGetOptions, GetParameterResponse, SetParameterRequest, SetParameterResponse, DeleteParameterResponse, ParameterListOptions, ListParametersResponse, GetParametersRequest, GetParametersResponse, SetParametersRequest, SetParametersResponse, DeleteParametersResponse, ParameterSearchByTagsRequest, ParameterSearchByTagsResponse } from './parameters.js';
import type { VectorAdminClient, ClusterStatsResponse, CompactNamespaceResponse, CompactionProgress, ExportVectorsRequest, VectorExportEntry, ImportVectorsResponse } from './vector_admin.js';
import type { XKMSFIDO2Client, XKMSFIDO2BeginRegistrationRequest, XKMSFIDO2Challenge, XKMSFIDO2FinishRegistrationRequest, XKMSFIDO2FinishRegistrationResponse, XKMSFIDO2BeginAuthenticationRequest, XKMSFIDO2FinishAuthenticationRequest, XKMSFIDO2FinishAuthenticationResponse } from './xkms_fido2.js';
import type { RegistryClient, RegistryServiceInstance, RegistryLeaseResponse, RegistryResolveResponse } from './registry.js';
import type { CacheClient, CacheStatsResponse, CacheClearResponse, CacheConfigResponse, CacheConfigUpdateRequest, CacheConfigUpdateResponse } from './cache.js';
import type { DynamicSecretsClient, DynamicSecretRole, CreateRoleRequest as DynamicSecretCreateRoleRequest, ListRolesResponse as ListDynamicSecretRolesResponse, DeleteRoleResponse as DeleteDynamicSecretRoleResponse, GenerateCredentialRequest, GenerateCredentialResponse, RenewLeaseRequest, RenewLeaseResponse, RevokeLeaseResponse as RevokeDynamicSecretLeaseResponse, ListLeasesResponse } from './dynamicsecrets.js';
import type { AccountsClient, Account, CreateAccountRequest, CreateAccountResponse, GetAccountResponse, ListAccountsRequest, ListAccountsResponse, UpdateAccountRequest, UpdateAccountResponse, DeleteAccountResponse, RotateAccountCredentialRequest, RotateAccountCredentialResponse, RevokeAccountRequest, RevokeAccountResponse, SetDefaultAccountResponse, GetAccountUsageResponse } from './accounts.js';
import type { EffectivePrompt, GetPromptRequest, ListPromptsRequest, ListPromptsResponse, PromptAddress, PromptDiff, PromptHistory, PromptRecord, PromptsClient, PutPromptRequest, ResetPromptRequest, RollbackPromptRequest } from './prompts.js';
import type { AgentsClient } from './agents.js';
import type { ClientConfig, BarrierStatusResponse, BarrierInitRequest, BarrierUnsealRequest, BarrierSealRequest, BarrierShamirInitRequest, BarrierShamirInitResponse, BarrierUnsealShareRequest, BarrierUnsealShareResponse, BarrierUnsealSharesRequest, BarrierRekeyRequest, BarrierRekeyResponse, BarrierGenerateRecoveryKeysRequest, BarrierRecoveryKeysResponse, BarrierRecoverWithKeysRequest, BarrierHasRecoveryKeysResponse, BarrierGenerateRootTokenRequest, BarrierEnableEncryptionRequest, BarrierEnableEncryptionResponse, BarrierRootTokenResponse, TenantBarrierInitRequest, TenantBarrierInitResponse, TenantBarrierUnsealShareResponse, TenantBarrierStatusResponse, CeremonyInitializeRequest, CeremonyInitializeResponse, CeremonyStatusResponse, CeremonyClaimCertBeginRequest, CeremonyClaimCertBeginResponse, CeremonyClaimCertCompleteRequest, CeremonyClaimCertCompleteResponse, CeremonyClaimShareBeginRequest, CeremonyClaimShareBeginResponse, CeremonyClaimShareCompleteRequest, CeremonyClaimShareCompleteResponse, CeremonySignCSRRequest, CeremonySignCSRResponse, CeremonyEnrollExternalOfficerRequest, CeremonyEnrollExternalOfficerResponse, BootstrapBundleRequest, BootstrapBundleResponse, ClusterStatusResponse, ClusterLeaderResponse, ShardLeaderResponse, RebalanceRequest, RebalanceResponse, RebalanceStatusResponse, RebalancePlanResponse, MigrationRequest, MigrationResponse, MigrationStatusResponse, CancelMigrationResponse, SessionInfo, SSHScope, SSHKey, SSHPublicKey, SignRequest, SignResponse, SSHAuthorizedEntry, CreateSSHKeyRequest, ImportSSHKeyRequest, ListSSHKeysQuery, ListSSHKeysResponse, AddSSHPinRequest, ListSSHPinsResponse, VerifySSHPinRequest, EmitAuthorizedRequest, EmitAuthorizedResponse, VerifyAuthorizedRequest, VerifyAuthorizedResponse, AddAuthorizedKeyRequest, ListAuthorizedKeysRequest } from './types.js';
import { type AccountPageParams, type PageResult, type ProjectPageParams } from './page.js';
import { APIError, ConnectionError, NotConnectedError, SSHError, SSHNotFoundError, SSHDuplicateError, SSHInvalidError, SSHUnavailableError, SSHServerError, SSHHostMismatchError } from './errors.js';
export declare function resolveToken(token: string | (() => Promise<string>) | undefined): Promise<string>;
export declare class RestProjectsClient implements ProjectsClient {
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    private fetchJSON;
    createProject(req: CreateProjectRequest): Promise<CreateProjectResponse>;
    getProject(tenantId: string, projectId: string): Promise<GetProjectResponse>;
    updateProject(req: UpdateProjectRequest): Promise<UpdateProjectResponse>;
    deleteProject(tenantId: string, projectId: string): Promise<DeleteProjectResponse>;
    listProjects(tenantId: string): Promise<ListProjectsResponse>;
    listProjectsPage(tenantId: string, page?: ProjectPageParams): Promise<PageResult<Project>>;
    grantFleetNodeRole(req: GrantFleetNodeRoleRequest): Promise<GrantFleetNodeRoleResponse>;
    revokeFleetNodeRole(req: RevokeFleetNodeRoleRequest): Promise<RevokeFleetNodeRoleResponse>;
}
export declare class RestSdlcClient implements SdlcClient {
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    private fetchJSON;
    private bindingPath;
    forges(): Promise<string[]>;
    bind(tenantId: string, projectId: string, binding: ProjectBinding, paused?: boolean): Promise<ProjectBinding>;
    unbind(tenantId: string, projectId: string): Promise<void>;
    binding(tenantId: string, projectId: string): Promise<ProjectBinding>;
    delivery(tenantId: string, taskId: string): Promise<Delivery>;
    onboard(tenantId: string, projectId: string): Promise<OnboardingReport>;
    conformance(tenantId: string, projectId: string): Promise<ConformanceReport>;
    openWork(tenantId: string, projectId: string): Promise<OpenWork>;
    private reviewerPath;
    reviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection>;
    setReviewer(tenantId: string, projectId: string, pullRequest: number, profile: string): Promise<ReviewerSelection>;
    clearReviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection>;
    fileIssue(tenantId: string, projectId: string, req: FileIssueRequest): Promise<ForgeIssue>;
    collaborators(tenantId: string, projectId: string): Promise<CollaboratorsResponse>;
    adoptIssue(tenantId: string, projectId: string, request: AdoptIssueRequest): Promise<Adoption>;
}
export declare class RestAccountsClient implements AccountsClient {
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    private fetchJSON;
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
export declare class RestPromptsClient implements PromptsClient {
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    private fetchJSON;
    listPrompts(req: ListPromptsRequest): Promise<ListPromptsResponse>;
    getPrompt(req: GetPromptRequest): Promise<PromptRecord>;
    getEffectivePrompt(addr: PromptAddress): Promise<EffectivePrompt>;
    diffPrompt(addr: PromptAddress): Promise<PromptDiff>;
    getPromptHistory(addr: PromptAddress): Promise<PromptHistory>;
    putPrompt(req: PutPromptRequest): Promise<PromptRecord>;
    rollbackPrompt(req: RollbackPromptRequest): Promise<PromptRecord>;
    resetPrompt(req: ResetPromptRequest): Promise<PromptRecord>;
}
/** RestXKMSKeysClient implements XKMSKeysClient over /api/v1/xkms/keys/*. */
export declare class RestXKMSKeysClient implements XKMSKeysClient {
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    private authHeaders;
    private fetchJSON;
    private keyPath;
    private static backendQuery;
    listKeys(backend?: string): Promise<XKMSListKeysResponse>;
    generateKey(req: XKMSGenerateKeyRequest): Promise<XKMSGenerateKeyResponse>;
    getKey(id: string, backend?: string): Promise<XKMSKeyInfo>;
    deleteKey(id: string): Promise<XKMSDeleteKeyResponse>;
    rotateKey(id: string): Promise<XKMSRotateKeyResponse>;
    encrypt(id: string, req: XKMSEncryptRequest): Promise<XKMSEncryptResponse>;
    decrypt(id: string, req: XKMSDecryptRequest): Promise<XKMSDecryptResponse>;
    sign(id: string, req: XKMSSignRequest): Promise<XKMSSignResponse>;
    verify(id: string, req: XKMSVerifyRequest): Promise<XKMSVerifyResponse>;
    listKeyVersions(id: string): Promise<XKMSListKeyVersionsResponse>;
    enableAllKeyVersions(id: string): Promise<XKMSBulkKeyVersionsResponse>;
    disableAllKeyVersions(id: string): Promise<XKMSBulkKeyVersionsResponse>;
    enableKeyVersion(id: string, version: number): Promise<XKMSKeyVersionStatusResponse>;
    disableKeyVersion(id: string, version: number): Promise<XKMSKeyVersionStatusResponse>;
}
export declare class RestXKMSClient implements XKMSClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    listBackends(): Promise<XKMSListBackendsResponse>;
    getBackend(id: string): Promise<XKMSBackendInfo>;
    health(): Promise<XKMSHealthResponse>;
    canSeal(backend?: string): Promise<XKMSSealCapabilityResponse>;
    seal(req: XKMSSealRequest): Promise<XKMSSealResponse>;
    unseal(req: XKMSUnsealRequest): Promise<XKMSUnsealResponse>;
    listCertificates(backend?: string): Promise<XKMSListCertificatesResponse>;
    getCertificate(keyID: string): Promise<XKMSCertificateResponse>;
    saveCertificate(keyID: string, certificatePEM: string): Promise<void>;
    deleteCertificate(keyID: string): Promise<void>;
}
export declare class RestCAClient implements CAClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    issueCertificate(req: IssueCertificateRequest): Promise<IssueCertificateResponse>;
    signCSR(req: SignCSRRequest): Promise<SignCSRResponse>;
    revokeCertificate(serialNumber: string): Promise<RevokeCertificateResponse>;
    verifyCertificate(certificatePEM: string): Promise<VerifyCertificateResponse>;
    getCACertificate(): Promise<GetCACertificateResponse>;
    caInfo(): Promise<CAInfoResponse>;
    getCABundle(): Promise<CABundleResponse>;
    listCertificates(req?: ListCertificatesRequest): Promise<ListCertificatesResponse>;
    generateCRL(): Promise<GenerateCRLResponse>;
    isRevoked(serialNumber: string): Promise<IsRevokedResponse>;
    private static xkmsPEM;
    xkmsGetCABundle(opts?: XKMSCABundleOptions): Promise<XKMSCABundle>;
    xkmsGetCACertificate(identity?: string): Promise<XKMSCACertificate>;
    xkmsSignCSR(req: XKMSCASignCSRRequest): Promise<XKMSCASignCSRResponse>;
    xkmsIssueCertificate(req: XKMSCAIssueRequest): Promise<XKMSCAIssueResponse>;
    xkmsRevokeCertificate(serialNumber: string, reason: number, tenantId?: string): Promise<XKMSCARevokeResponse>;
}
export declare class RestTrustClient implements TrustClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    crossSignedRootPool(): Promise<CrossSignedRootPoolResponse>;
    autoImportAIA(req: AutoImportAIARequest): Promise<AutoImportAIAResponse>;
    publishDANE(req: PublishDANERequest): Promise<PublishDANEResponse>;
}
export declare class RestCryptoClient implements CryptoClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    encrypt(req: CryptoEncryptRequest): Promise<CryptoEncryptResponse>;
    decrypt(req: CryptoDecryptRequest): Promise<CryptoDecryptResponse>;
    rewrap(req: CryptoRewrapRequest): Promise<CryptoRewrapResponse>;
    sign(req: CryptoSignRequest): Promise<CryptoSignResponse>;
    verify(req: CryptoVerifyRequest): Promise<CryptoVerifyResponse>;
    hash(req: CryptoHashRequest): Promise<CryptoHashResponse>;
    random(numBytes?: number): Promise<CryptoRandomResponse>;
    dataKey(req: CryptoDataKeyRequest): Promise<CryptoDataKeyResponse>;
}
export declare class RestDEKClient implements DEKClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    createDEK(req: CreateDEKRequest): Promise<CreateDEKResponse>;
    getDEK(path: string, version?: number): Promise<GetDEKResponse>;
    listDEKs(req?: ListDEKsRequest): Promise<ListDEKsResponse>;
    rotateDEK(path: string, req?: RotateDEKRequest): Promise<RotateDEKResponse>;
    deleteDEK(path: string, req?: DeleteDEKRequest): Promise<DeleteDEKResponse>;
    getDEKStatus(path: string): Promise<GetDEKStatusResponse>;
    setDefaultDEK(path: string): Promise<SetDefaultDEKResponse>;
    setDEKSafety(path: string, req: SetDEKSafetyRequest): Promise<SetDEKSafetyResponse>;
    clearDEKSafety(path: string): Promise<ClearDEKSafetyResponse>;
    startReencryption(path: string, req?: StartReencryptionRequest): Promise<StartReencryptionResponse>;
    getReencryptionProgress(path: string, operationId?: string, verbose?: boolean): Promise<GetReencryptionProgressResponse>;
}
/**
 * RestAttestationClient manages the attestation verifier's endorsements and
 * reference values over REST (/api/v1/attestation/*).
 */
/** pemTextToBase64 encodes PEM text as the base64 of its UTF-8 bytes (a Go []byte on the wire). */
export declare function pemTextToBase64(pem: string): string;
/** base64ToPEMText decodes a Go []byte field back to the PEM text it carries. */
export declare function base64ToPEMText(b64: string): string;
export declare class RestAttestationClient implements AttestationClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    putEndorsement(name: string, req: PutEndorsementRequest): Promise<Endorsement>;
    listEndorsements(kind?: EndorsementKind): Promise<ListEndorsementsResponse>;
    deleteEndorsement(name: string): Promise<void>;
    putReferenceValues(name: string, signedCoRIM: string): Promise<ReferenceValues>;
    getReferenceValues(name: string): Promise<ReferenceValues>;
    listReferenceValues(): Promise<ListReferenceValuesResponse>;
    deleteReferenceValues(name: string): Promise<void>;
}
/**
 * RestPIVClient manages xkms PIV slots over REST
 * (/api/v1/xkms/piv/slots*, pkg/api/rest/handlers_xkms.go).
 *
 * The server's certificate, key, CSR and chain fields are Go []byte, so JSON
 * carries them as base64. This client base64-encodes PEM text on writes and
 * decodes the PEM fields of responses; a DER certificate is already base64
 * and passes through unchanged.
 */
export declare class RestPIVClient implements PIVClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    private slotPath;
    listSlots(backend?: string): Promise<ListPIVSlotsResponse>;
    getCertificate(slot: string, opts?: GetPIVCertificateOptions): Promise<GetPIVCertificateResponse>;
    storeCertificate(slot: string, req: StorePIVCertificateRequest): Promise<void>;
    deleteCertificate(slot: string, backend?: string): Promise<void>;
    generateKey(slot: string, req: GeneratePIVKeyRequest): Promise<GeneratePIVKeyResponse>;
    generateCSR(slot: string, req: GeneratePIVCSRRequest): Promise<GeneratePIVCSRResponse>;
}
export declare class RestBackupsClient implements BackupsClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    create(req?: BackupCreateRequest): Promise<BackupCreateResponse>;
    list(opts?: BackupListOptions): Promise<BackupListResponse>;
    restore(backupID: string, req?: BackupRestoreRequest): Promise<BackupRestoreResponse>;
    delete(backupID: string): Promise<BackupDeleteResponse>;
    verify(backupID: string): Promise<BackupVerifyResponse>;
    status(backupID: string): Promise<BackupStatusResponse>;
}
/**
 * RestClusterNodesClient manages cluster node membership over REST.
 * Distinct from RestClusterClient (client.cluster).
 */
export declare class RestClusterNodesClient implements ClusterNodesClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    listNodes(): Promise<ListClusterNodesResponse>;
    addNode(req: AddNodeRequest): Promise<AddNodeResponse>;
    listNonLeaderNodes(): Promise<ListClusterNodesResponse>;
    removeNode(nodeId: number, shardId: number): Promise<RemoveNodeResponse>;
    replaceNode(nodeId: number, req: ReplaceNodeRequest): Promise<ReplaceNodeResponse>;
}
/** RestEntitiesClient implements EntitiesClient over the generic entity REST routes. */
export declare class RestEntitiesClient implements EntitiesClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    list(type: string, opts?: EntityListOptions): Promise<EntityList>;
    create(type: string, data: unknown, opts?: EntityWriteOptions): Promise<PutEntityResponse>;
    put(type: string, id: number | string, data: unknown, opts?: EntityWriteOptions): Promise<PutEntityResponse>;
    get(type: string, id: number | string, opts?: EntityGetOptions): Promise<EntityRecord>;
    delete(type: string, id: number | string, opts?: EntityWriteOptions): Promise<DeleteEntityResponse>;
}
/**
 * RestKMSAdminClient administers named KMS keys over REST: metadata, access
 * policy and rotation policy under /api/v1/kms/keys/{name}/*.
 */
export declare class RestKMSAdminClient implements KMSAdminClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    private keyPath;
    getKeyMetadata(name: string): Promise<KMSKeyMetadataResponse>;
    updateKeyMetadata(name: string, req: UpdateKMSKeyMetadataRequest): Promise<KMSKeyMetadataResponse>;
    getKeyPolicy(name: string): Promise<GetKMSKeyPolicyResponse>;
    setKeyPolicy(name: string, policy: KMSKeyPolicyInput): Promise<SetKMSKeyPolicyResponse>;
    getKeyRotationPolicy(name: string): Promise<GetKMSRotationPolicyResponse>;
    setKeyRotationPolicy(name: string, req: KMSRotationPolicyInput): Promise<SetKMSRotationPolicyResponse>;
    deleteKeyRotationPolicy(name: string): Promise<DeleteKMSRotationPolicyResponse>;
}
export declare class RestObjectStorageAdminClient implements ObjectStorageAdminClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    createBackend(req: ObjectStorageAdminCreateBackendRequest): Promise<ObjectStorageAdminCreateBackendResponse>;
    updateBackend(name: string, req: ObjectStorageAdminUpdateBackendRequest): Promise<ObjectStorageAdminUpdateBackendResponse>;
    deleteBackend(name: string, req?: ObjectStorageAdminDeleteBackendRequest): Promise<ObjectStorageAdminDeleteBackendResponse>;
    setBackendStatus(name: string, enabled: boolean): Promise<ObjectStorageAdminBackendStatusResponse>;
    compactBackend(name: string, req?: ObjectStorageAdminCompactBackendRequest): Promise<ObjectStorageAdminCompactBackendResponse>;
    storageStats(opts?: ObjectStorageAdminStatsOptions): Promise<ObjectStorageAdminStatsResponse>;
    runGarbageCollection(req?: ObjectStorageAdminGCRequest): Promise<ObjectStorageAdminGCResponse>;
    garbageCollectionProgress(operationID: string): Promise<ObjectStorageAdminGCProgressResponse>;
    repairObject(req: ObjectStorageAdminRepairRequest): Promise<ObjectStorageAdminRepairResponse>;
}
export declare class RestParametersClient implements ParametersClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    get(path: string, opts?: ParameterGetOptions): Promise<GetParameterResponse>;
    set(path: string, req: SetParameterRequest): Promise<SetParameterResponse>;
    delete(path: string): Promise<DeleteParameterResponse>;
    list(opts?: ParameterListOptions): Promise<ListParametersResponse>;
    getBatch(req: GetParametersRequest): Promise<GetParametersResponse>;
    setBatch(req: SetParametersRequest): Promise<SetParametersResponse>;
    deleteBatch(paths: string[]): Promise<DeleteParametersResponse>;
    searchByTags(req: ParameterSearchByTagsRequest): Promise<ParameterSearchByTagsResponse>;
}
export declare class RestVectorAdminClient implements VectorAdminClient {
    private readonly http;
    private readonly baseUrl;
    private readonly config;
    constructor(baseUrl: string, config?: ClientConfig);
    getGlobalStats(): Promise<ClusterStatsResponse>;
    compactNamespace(namespace: string): Promise<CompactNamespaceResponse>;
    getCompactionProgress(operationId: string): Promise<CompactionProgress>;
    exportVectors(namespace: string, req?: ExportVectorsRequest): Promise<VectorExportEntry[]>;
    importVectors(namespace: string, entries: VectorExportEntry[]): Promise<ImportVectorsResponse>;
    private sendText;
}
export declare class RestXKMSFIDO2Client implements XKMSFIDO2Client {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    beginRegistration(req: XKMSFIDO2BeginRegistrationRequest, tenantId?: string): Promise<XKMSFIDO2Challenge>;
    finishRegistration(req: XKMSFIDO2FinishRegistrationRequest, tenantId?: string): Promise<XKMSFIDO2FinishRegistrationResponse>;
    beginAddCredential(): Promise<XKMSFIDO2Challenge>;
    beginAuthentication(req: XKMSFIDO2BeginAuthenticationRequest, tenantId?: string): Promise<XKMSFIDO2Challenge>;
    finishAuthentication(req: XKMSFIDO2FinishAuthenticationRequest, tenantId?: string): Promise<XKMSFIDO2FinishAuthenticationResponse>;
}
export declare class RestRegistryClient implements RegistryClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    register(instance: RegistryServiceInstance, ttlSeconds: number): Promise<RegistryLeaseResponse>;
    renew(leaseId: string): Promise<RegistryLeaseResponse>;
    deregister(leaseId: string): Promise<void>;
    resolve(service: string): Promise<RegistryResolveResponse>;
}
/** RestCacheClient reads and manages the server's cache over REST. */
export declare class RestCacheClient implements CacheClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    stats(): Promise<CacheStatsResponse>;
    clear(): Promise<CacheClearResponse>;
    getConfig(): Promise<CacheConfigResponse>;
    updateConfig(req: CacheConfigUpdateRequest): Promise<CacheConfigUpdateResponse>;
}
export declare class RestDynamicSecretsClient implements DynamicSecretsClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    createRole(req: DynamicSecretCreateRoleRequest): Promise<DynamicSecretRole>;
    getRole(name: string): Promise<DynamicSecretRole>;
    listRoles(prefix?: string): Promise<ListDynamicSecretRolesResponse>;
    deleteRole(name: string): Promise<DeleteDynamicSecretRoleResponse>;
    generateCredential(roleName: string, req?: GenerateCredentialRequest): Promise<GenerateCredentialResponse>;
    renewLease(leaseId: string, req?: RenewLeaseRequest): Promise<RenewLeaseResponse>;
    revokeLease(leaseId: string): Promise<RevokeDynamicSecretLeaseResponse>;
    listLeases(prefix?: string): Promise<ListLeasesResponse>;
}
export declare class RestBarrierClient implements BarrierClient {
    private readonly baseUrl;
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
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
export declare class RestTenantBarrierClient implements TenantBarrierClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    private base;
    tenantBarrierInit(tenantId: string, req: TenantBarrierInitRequest): Promise<TenantBarrierInitResponse | undefined>;
    tenantBarrierUnseal(tenantId: string, key: Uint8Array): Promise<void>;
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
    claimShareBegin(req: CeremonyClaimShareBeginRequest): Promise<CeremonyClaimShareBeginResponse>;
    claimShareComplete(req: CeremonyClaimShareCompleteRequest): Promise<CeremonyClaimShareCompleteResponse>;
    signCSR(req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;
    enrollExternalOfficer(req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse>;
}
export declare class RestClusterClient implements ClusterClient {
    private readonly http;
    constructor(baseUrl: string, config?: ClientConfig);
    status(): Promise<ClusterStatusResponse>;
    leader(): Promise<ClusterLeaderResponse>;
    shardLeader(shardID: number): Promise<ShardLeaderResponse>;
    startRebalance(req?: RebalanceRequest): Promise<RebalanceResponse>;
    rebalanceStatus(operationID?: string): Promise<RebalanceStatusResponse>;
    rebalancePlan(): Promise<RebalancePlanResponse>;
    startMigration(req: MigrationRequest): Promise<MigrationResponse>;
    migrationStatus(operationID: string): Promise<MigrationStatusResponse>;
    cancelMigration(operationID: string): Promise<CancelMigrationResponse>;
    listNodes(): Promise<ListClusterNodesResponse>;
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
    add(req: AddAuthorizedKeyRequest): Promise<void>;
    list(req?: ListAuthorizedKeysRequest): Promise<SSHAuthorizedEntry[]>;
    get(scope: SSHScope, owner: string, name: string): Promise<SSHAuthorizedEntry>;
    remove(scope: SSHScope, owner: string, name: string): Promise<void>;
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
    readonly sdlc: SdlcClient;
    readonly accounts: AccountsClient;
    readonly prompts: PromptsClient;
    readonly agents: AgentsClient;
    readonly iam: IAMClient;
    readonly kms: KMSClient;
    readonly secrets: SecretsClient;
    readonly ca: CAClient;
    readonly trust: TrustClient;
    readonly crypto: CryptoClient;
    readonly dek: DEKClient;
    readonly attestation: AttestationClient;
    readonly xkmsKeys: XKMSKeysClient;
    readonly piv: PIVClient;
    readonly xkms: XKMSClient;
    readonly backups: BackupsClient;
    readonly clusterNodes: ClusterNodesClient;
    readonly entities: EntitiesClient;
    readonly kmsAdmin: KMSAdminClient;
    readonly objectStorageAdmin: ObjectStorageAdminClient;
    readonly parameters: ParametersClient;
    readonly vectorAdmin: VectorAdminClient;
    readonly xkmsFido2: XKMSFIDO2Client;
    readonly registry: RegistryClient;
    readonly cache: CacheClient;
    readonly dynamicSecrets: DynamicSecretsClient;
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