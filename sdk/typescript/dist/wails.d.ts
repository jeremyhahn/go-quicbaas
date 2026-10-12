import type { AuthClient, Client, ConnectionClient, OrchestratorClient, SSHKeysClient, SSHClient } from './interfaces.js';
import type { AccountsClient } from './accounts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
export declare function wailsError(reason: unknown): unknown;
export declare class WailsClient implements Client {
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
    /**
     * sshKeys is the legacy backward-compat accessor.
     * Points to ssh.keys — the same WailsSSHKeysClient instance.
     */
    readonly sshKeys: SSHKeysClient;
    /**
     * ssh exposes the full scoped SSH interface.
     * In Wails mode, only ssh.keys (List/Create/Get/Delete) is backed by the
     * Go binding. ssh.knownHosts and ssh.authorized throw in Wails mode;
     * use the REST transport for those.
     */
    readonly ssh: SSHClient;
    constructor();
}
//# sourceMappingURL=wails.d.ts.map