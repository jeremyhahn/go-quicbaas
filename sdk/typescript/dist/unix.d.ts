import type { AuthClient, Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
import type { AccountsClient } from './accounts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
import type { ClientConfig } from './types.js';
import { QuicBaaSError } from './errors.js';
export declare class UnixNotImplementedError extends QuicBaaSError {
    constructor(method: string);
}
export declare class UnixClient implements Client {
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
    readonly barrier: any;
    readonly tenantBarrier: any;
    readonly ceremony: any;
    readonly bootstrap: any;
    readonly cluster: any;
    readonly seal: any;
    readonly sshKeys: any;
    readonly ssh: any;
    constructor(socketPath: string, _config?: ClientConfig);
    logout(_token?: string): Promise<void>;
}
//# sourceMappingURL=unix.d.ts.map