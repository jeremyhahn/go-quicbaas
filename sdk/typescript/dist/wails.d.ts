import type { AuthClient, Client, ConnectionClient, OrchestratorClient, SSHKeysClient, SSHClient } from './interfaces.js';
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
    readonly projects: any;
    readonly iam: any;
    readonly kms: any;
    readonly secrets: any;
    readonly barrier: any;
    readonly ceremony: any;
    readonly bootstrap: any;
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