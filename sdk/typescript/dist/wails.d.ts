import type { AuthClient, BarrierClient, CeremonyClient, Client, ClusterClient, ConnectionClient, DataClient, IAMClient, KMSClient, ObjectsClient, OrchestratorClient, SecretsClient, SSHKeysClient, SSHClient, TenantBarrierClient, TimeSeriesClient, VectorsClient } from './interfaces.js';
import type { AgentsClient } from './agents.js';
import type { CAClient } from './ca.js';
import type { CryptoClient } from './crypto.js';
import type { DEKClient } from './dek.js';
import type { DynamicSecretsClient } from './dynamicsecrets.js';
import type { EmbeddingsClient } from './embedding_sdk.js';
import type { MessagingClient } from './messaging_sdk.js';
import type { RAGClient } from './rag.js';
import type { SchedulerClient } from './scheduler_sdk.js';
import type { TrustClient } from './trust.js';
import type { AccountsClient } from './accounts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
export declare function wailsError(reason: unknown): unknown;
export declare class WailsClient implements Client {
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
    readonly agents: AgentsClient;
    readonly iam: IAMClient;
    readonly kms: KMSClient;
    readonly secrets: SecretsClient;
    readonly ca: CAClient;
    readonly trust: TrustClient;
    readonly crypto: CryptoClient;
    readonly dek: DEKClient;
    readonly dynamicSecrets: DynamicSecretsClient;
    readonly barrier: BarrierClient;
    readonly tenantBarrier: TenantBarrierClient;
    readonly ceremony: CeremonyClient;
    readonly cluster: ClusterClient;
    readonly seal: import("./interfaces.js").SealClient;
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