import type { AuthClient, BarrierClient, BootstrapClient, CeremonyClient, Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
import type { BarrierGenerateRecoveryKeysRequest, BarrierGenerateRootTokenRequest, BarrierHasRecoveryKeysResponse, BarrierInitRequest, BarrierRecoverWithKeysRequest, BarrierRecoveryKeysResponse, BarrierRekeyRequest, BarrierRekeyResponse, BarrierRootTokenResponse, BarrierShamirDeleteShareRequest, BarrierShamirInitRequest, BarrierShamirInitResponse, BarrierShamirSharesResponse, BarrierStatusResponse, BarrierUnsealRequest, BarrierUnsealShareRequest, BarrierUnsealShareResponse, BarrierUnsealSharesRequest, BootstrapBundleRequest, BootstrapBundleResponse, CeremonyClaimCertBeginRequest, CeremonyClaimCertBeginResponse, CeremonyClaimCertCompleteRequest, CeremonyClaimCertCompleteResponse, CeremonyClaimShareRequest, CeremonyClaimShareResponse, CeremonyEnrollExternalOfficerRequest, CeremonyEnrollExternalOfficerResponse, CeremonyInitializeRequest, CeremonyInitializeResponse, CeremonySignCSRRequest, CeremonySignCSRResponse, CeremonyStatusResponse, ClientConfig } from './types.js';
import { QuicBaaSError } from './errors.js';
export declare class McpNotImplementedError extends QuicBaaSError {
    constructor(method: string);
}
export declare class McpBarrierClient implements BarrierClient {
    status(): Promise<BarrierStatusResponse>;
    initialize(_req: BarrierInitRequest): Promise<void>;
    unseal(_req: BarrierUnsealRequest): Promise<void>;
    seal(): Promise<void>;
    initializeShamir(_req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse>;
    unsealWithShare(_req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse>;
    unsealWithShares(_req: BarrierUnsealSharesRequest): Promise<void>;
    shamirListShares(): Promise<BarrierShamirSharesResponse>;
    shamirDeleteShare(_req: BarrierShamirDeleteShareRequest): Promise<void>;
    shamirDeleteAllShares(): Promise<void>;
    shamirVerify(): Promise<void>;
    rekey(_req: BarrierRekeyRequest): Promise<BarrierRekeyResponse>;
    generateRecoveryKeys(_req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse>;
    recoverWithKeys(_req: BarrierRecoverWithKeysRequest): Promise<void>;
    deleteRecoveryKeys(): Promise<void>;
    recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse>;
    generateRootToken(_req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse>;
}
export declare class McpCeremonyClient implements CeremonyClient {
    initialize(_req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse>;
    status(): Promise<CeremonyStatusResponse>;
    claimCertBegin(_req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse>;
    claimCertComplete(_req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse>;
    claimShare(_req: CeremonyClaimShareRequest): Promise<CeremonyClaimShareResponse>;
    signCSR(_req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;
    enrollExternalOfficer(_req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse>;
}
export declare class McpBootstrapClient implements BootstrapClient {
    auto(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    dane(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    noise(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
    spki(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
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
    readonly projects: any;
    readonly sdlc: any;
    readonly accounts: any;
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
    readonly bootstrap: BootstrapClient;
    readonly cluster: any;
    readonly seal: any;
    readonly sshKeys: any;
    readonly ssh: any;
    constructor(endpoint: string, _config?: ClientConfig);
    logout(_token?: string): Promise<void>;
}
//# sourceMappingURL=mcp.d.ts.map