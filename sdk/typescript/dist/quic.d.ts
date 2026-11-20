import type { AuthClient, BarrierClient, CeremonyClient, Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
import type { AccountsClient } from './accounts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
import type { BarrierGenerateRecoveryKeysRequest, BarrierGenerateRootTokenRequest, BarrierEnableEncryptionRequest, BarrierEnableEncryptionResponse, BarrierHasRecoveryKeysResponse, BarrierInitRequest, BarrierRecoverWithKeysRequest, BarrierRecoveryKeysResponse, BarrierRekeyRequest, BarrierRekeyResponse, BarrierRootTokenResponse, BarrierShamirInitRequest, BarrierShamirInitResponse, BarrierStatusResponse, BarrierUnsealRequest, BarrierSealRequest, BarrierUnsealShareRequest, BarrierUnsealShareResponse, BarrierUnsealSharesRequest, CeremonyClaimCertBeginRequest, CeremonyClaimCertBeginResponse, CeremonyClaimCertCompleteRequest, CeremonyClaimCertCompleteResponse, CeremonyClaimShareBeginRequest, CeremonyClaimShareBeginResponse, CeremonyClaimShareCompleteRequest, CeremonyClaimShareCompleteResponse, CeremonyEnrollExternalOfficerRequest, CeremonyEnrollExternalOfficerResponse, CeremonyInitializeRequest, CeremonyInitializeResponse, CeremonySignCSRRequest, CeremonySignCSRResponse, CeremonyStatusResponse, ClientConfig } from './types.js';
import { TransportNotSupportedError } from './errors.js';
export declare class QuicNotImplementedError extends TransportNotSupportedError {
    constructor(family: string, method: string, reason: string);
}
export declare class QuicBarrierClient implements BarrierClient {
    status(): Promise<BarrierStatusResponse>;
    initialize(_req: BarrierInitRequest): Promise<void>;
    unseal(_req: BarrierUnsealRequest): Promise<void>;
    seal(_req?: BarrierSealRequest): Promise<void>;
    initializeShamir(_req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse>;
    unsealWithShare(_req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse>;
    unsealWithShares(_req: BarrierUnsealSharesRequest): Promise<void>;
    rekey(_req: BarrierRekeyRequest): Promise<BarrierRekeyResponse>;
    generateRecoveryKeys(_req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse>;
    recoverWithKeys(_req: BarrierRecoverWithKeysRequest): Promise<void>;
    deleteRecoveryKeys(): Promise<void>;
    recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse>;
    generateRootToken(_req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse>;
    enableEncryption(_req: BarrierEnableEncryptionRequest): Promise<BarrierEnableEncryptionResponse>;
}
export declare class QuicCeremonyClient implements CeremonyClient {
    initialize(_req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse>;
    status(): Promise<CeremonyStatusResponse>;
    claimCertBegin(_req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse>;
    claimCertComplete(_req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse>;
    claimShareBegin(_req: CeremonyClaimShareBeginRequest): Promise<CeremonyClaimShareBeginResponse>;
    claimShareComplete(_req: CeremonyClaimShareCompleteRequest): Promise<CeremonyClaimShareCompleteResponse>;
    signCSR(_req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;
    enrollExternalOfficer(_req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse>;
}
export declare class QuicClient implements Client {
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
    constructor(endpoint: string, _config?: ClientConfig);
    logout(_token?: string): Promise<void>;
}
//# sourceMappingURL=quic.d.ts.map