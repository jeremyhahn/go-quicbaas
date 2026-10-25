import type { AuthClient, BarrierClient, CeremonyClient, Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
import type { AccountsClient } from './accounts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
import type { BarrierGenerateRecoveryKeysRequest, BarrierEnableEncryptionRequest, BarrierEnableEncryptionResponse, BarrierHasRecoveryKeysResponse, BarrierInitRequest, BarrierRecoveryKeysResponse, BarrierShamirInitRequest, BarrierShamirInitResponse, BarrierDomain, BarrierOfficer, BarrierOfficerRegisterRequest, BarrierOfficerRegisterResponse, BarrierShareClaimBeginResponse, BarrierShareClaimCompleteRequest, BarrierShareClaimCompleteResponse, BarrierQuorumBeginRequest, BarrierQuorumChallenge, BarrierQuorumSubmitRequest, BarrierQuorumOutcome, BarrierRekeyStartRequest, BarrierQuorumRound, BarrierStatusResponse, BarrierUnsealRequest, BarrierSealRequest, CeremonyInitializeRequest, CeremonyInitializeResponse, CeremonySignCSRRequest, CeremonySignCSRResponse, CeremonyStatusResponse, ClientConfig } from './types.js';
import { QuicBaaSError } from './errors.js';
export declare class GrpcWebNotImplementedError extends QuicBaaSError {
    constructor(method: string);
}
export declare class GrpcWebBarrierClient implements BarrierClient {
    status(): Promise<BarrierStatusResponse>;
    initialize(_req: BarrierInitRequest): Promise<void>;
    unseal(_req: BarrierUnsealRequest): Promise<void>;
    seal(_req?: BarrierSealRequest): Promise<void>;
    initializeShamir(_req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse>;
    officers(): Promise<BarrierOfficer[]>;
    registerOfficer(_req: BarrierOfficerRegisterRequest, _domain?: BarrierDomain): Promise<BarrierOfficerRegisterResponse>;
    shareClaimBegin(_username: string, _domain?: BarrierDomain): Promise<BarrierShareClaimBeginResponse>;
    shareClaimComplete(_req: BarrierShareClaimCompleteRequest, _domain?: BarrierDomain): Promise<BarrierShareClaimCompleteResponse>;
    quorumBegin(_req: BarrierQuorumBeginRequest): Promise<BarrierQuorumChallenge>;
    quorumSubmit(_req: BarrierQuorumSubmitRequest): Promise<BarrierQuorumOutcome>;
    rekeyStart(_req: BarrierRekeyStartRequest): Promise<BarrierQuorumRound>;
    generateRecoveryKeys(_req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse>;
    deleteRecoveryKeys(): Promise<void>;
    recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse>;
    enableEncryption(_req: BarrierEnableEncryptionRequest): Promise<BarrierEnableEncryptionResponse>;
}
export declare class GrpcWebCeremonyClient implements CeremonyClient {
    initialize(_req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse>;
    status(): Promise<CeremonyStatusResponse>;
    signCSR(_req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;
}
export declare class GrpcWebClient implements Client {
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
    readonly dynamicSecrets: any;
    readonly barrier: BarrierClient;
    readonly tenantBarrier: any;
    readonly ceremony: CeremonyClient;
    readonly cluster: any;
    readonly seal: any;
    readonly sshKeys: any;
    readonly ssh: any;
    constructor(baseUrl: string, _config?: ClientConfig);
    logout(_token?: string): Promise<void>;
}
//# sourceMappingURL=grpc_web.d.ts.map