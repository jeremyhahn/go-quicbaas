// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// QuicClient is the transport implementation for QUIC (pkg/server/baas_transport.go).
//
// OPERATOR NOTE: WebTransport (the W3C standard that exposes QUIC streams to
// browsers) is available in Chrome 97+ and Node.js 21+ via the
// @fails-components/webtransport or native WebTransport API. Server-side Node
// usage can additionally use the quic module from @node/quic or the
// webtransport-ponyfill package.
//
// Every family rejects every verb with TransportNotSupportedError: projects,
// sdlc and accounts directly, the rest through its QuicNotImplementedError
// subclass (see quicUnsupported and quicNoOrchestratorRoute).
// The go-quicbaas QUIC server endpoint is configured in config.quic.endpoint.

import type {
  AuthClient,
  BarrierClient,
  CeremonyClient,
  Client,
  ConnectionClient,
  OrchestratorClient,
} from './interfaces.js';
import type { AccountsClient } from './accounts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
import { unsupportedClient } from './unsupported.js';
import type {
  AgentBackendInfo,
  JobRunnerInfo,
  BarrierGenerateRecoveryKeysRequest,
  BarrierGenerateRootTokenRequest,
  BarrierEnableEncryptionRequest,
  BarrierEnableEncryptionResponse,
  BarrierHasRecoveryKeysResponse,
  BarrierInitRequest,
  BarrierRecoverWithKeysRequest,
  BarrierRecoveryKeysResponse,
  BarrierRekeyRequest,
  BarrierRekeyResponse,
  BarrierRootTokenResponse,
  BarrierShamirInitRequest,
  BarrierShamirInitResponse,
  BarrierStatusResponse,
  BarrierUnsealRequest,
  BarrierSealRequest,
  BarrierUnsealShareRequest,
  BarrierUnsealShareResponse,
  BarrierUnsealSharesRequest,
  CeremonyClaimCertBeginRequest,
  CeremonyClaimCertBeginResponse,
  CeremonyClaimCertCompleteRequest,
  CeremonyClaimCertCompleteResponse,
  CeremonyClaimShareBeginRequest,
  CeremonyClaimShareBeginResponse,
  CeremonyClaimShareCompleteRequest,
  CeremonyClaimShareCompleteResponse,
  CeremonyEnrollExternalOfficerRequest,
  CeremonyEnrollExternalOfficerResponse,
  CeremonyInitializeRequest,
  CeremonyInitializeResponse,
  CeremonySignCSRRequest,
  CeremonySignCSRResponse,
  CeremonyStatusResponse,
  DeployFleetNodeResult,
  DeployFleetNodeSpec,
  InferenceRequest,
  InferenceResponse,
  InferenceChunk,
  FleetNode,
  ClientConfig,
  CloseSessionRequest,
  CloseSessionResponse,
  ClaimTaskRequest,
  CompleteTaskRequest,
  CreateTaskRequest,
  CreateTaskResponse,
  CreateWorkflowRequest,
  CreateWorkflowResponse,
  DecomposeTaskRequest,
  DecomposeTaskResponse,
  GetFleetNodeRequest,
  GetSessionHistoryRequest,
  GetSessionConversationRequest,
  GetSessionConversationResponse,
  GetSpendReportRequest,
  GetSpendReportResponse,
  GetTenantSpendPolicyRequest,
  SetTenantSpendPolicyRequest,
  TenantSpendPolicy,
  GetCostSettingsRequest,
  SetCostSettingsRequest,
  CostSettings,
  WorkerImageStatus,
  GetSessionHistoryResponse,
  HealthCheckReport,
  JobEvent,
  JobStatus,
  ListFleetNodesRequest,
  ListFleetNodesResponse,
  ListSessionsRequest,
  ListSessionsResponse,
  SessionStatusResponse,
  ListWorkflowsResponse,
  PromptSessionRequest,
  PromptSessionResponse,
  RegisterFleetNodeRequest,
  RegisterFleetNodeResponse,
  RemoveFleetNodeRequest,
  RotateSessionAccountRequest,
  RotateSessionAccountResponse,
  SessionEventEnvelope,
  SpawnSessionRequest,
  SpawnSessionResponse,
  StartJobRequest,
  StartJobResponse,
  SubscribeSessionEventsRequest,
  Task,
  WorkflowDefinition,
} from './types.js';
import {
  wholeSetPage,
  type FleetNodePageParams,
  type JobPageParams,
  type PageParams,
  type PageResult,
  type SessionPageParams,
  type TaskPageParams,
} from './page.js';
import { DeployNotSupportedError, TransportNotSupportedError } from './errors.js';

// QuicNotImplementedError is the refusal every QUIC verb rejects with. It is a
// TransportNotSupportedError, so it names the transport, family, method and
// reason, and a refusal never reaches the network.
export class QuicNotImplementedError extends TransportNotSupportedError {
  constructor(family: string, method: string, reason: string) {
    super('quic', family, reason, method);
    this.name = 'QuicNotImplementedError';
  }
}

// quicUnsupported is why most families refuse here. The server's QUIC listener
// (pkg/server/transport_factory.go) speaks qrdb's frame protocol, not REST over
// HTTP/3, and Node's fetch does not negotiate HTTP/3 anyway: a REST client
// aimed at the QUIC endpoint would either fail against the real listener or
// quietly fall back to TCP HTTP while claiming QUIC. Implementing the frame
// protocol in TypeScript waits on an open owner decision on whether and how a
// QUIC client listener is offered to SDK callers.
const quicUnsupported =
  "the server's QUIC listener speaks qrdb's frame protocol, which this SDK " +
  'does not implement; use the rest transport';

// quicNoOrchestratorRoute is why the orchestrator family refuses: the server's
// QUIC listener serves every unary orchestration, cost and worker-image method
// (go-quicbaas#819), but only over qrdb's frame protocol, which this SDK does
// not implement yet.
const quicNoOrchestratorRoute =
  "the server's QUIC listener serves the orchestrator's unary methods over " +
  "qrdb's frame protocol, which this SDK does not implement yet; use the rest transport";

function frameOnly(family: string, method: string): never {
  throw new QuicNotImplementedError(family, method, quicUnsupported);
}

function noQuicRoute(method: string): never {
  throw new QuicNotImplementedError('orchestrator', method, quicNoOrchestratorRoute);
}

class QuicConnectionClient implements ConnectionClient {
  private readonly _endpoint: string;
  private readonly _transport = 'quic';

  constructor(endpoint: string) {
    this._endpoint = endpoint;
  }

  async connect(_address: string): Promise<void> { frameOnly('connection', 'connect'); }
  async disconnect(): Promise<void> { frameOnly('connection', 'disconnect'); }
  isConnected(): boolean { return false; }
  getAddress(): string { return this._endpoint; }
  getTransport(): string { return this._transport; }
}

class QuicOrchestratorClient implements OrchestratorClient {

  // =========================================================================
  // The paged listings
  //
  // This transport is not wired to a QUIC/WebTransport client yet, so
  // every listing here refuses through its unpaged sibling rather than
  // answering a page it never fetched.
  //
  // Each delegates to its unpaged sibling and reports the whole answer as ONE
  // page (wholeSetPage): `total` is its length and there is no `next_offset`,
  // which is true of what this transport can actually fetch. The day it can
  // page, these methods are where the parameters go.
  // =========================================================================
  async listJobsPage(tenantID: string, page?: JobPageParams): Promise<PageResult<JobStatus>> {
    return wholeSetPage(await this.listJobs(tenantID, page?.task_id));
  }

  async listTasksPage(tenantID: string, _page?: TaskPageParams): Promise<PageResult<Task>> {
    return wholeSetPage(await this.listTasks(tenantID));
  }

  async listSessionsPage(
    tenantID: string,
    page?: SessionPageParams,
  ): Promise<PageResult<SessionStatusResponse>> {
    const req: ListSessionsRequest = { tenantID };
    if (page?.backend !== undefined) req.backend = page.backend;
    if (page?.status !== undefined) req.status = page.status;
    const resp = await this.listSessions(req);
    return wholeSetPage(resp.sessions ?? []);
  }

  async listFleetNodesPage(
    tenantID: string,
    page?: FleetNodePageParams,
  ): Promise<PageResult<FleetNode>> {
    const req: ListFleetNodesRequest = { tenant_id: tenantID };
    if (page?.host !== undefined) req.host = page.host;
    if (page?.status !== undefined) req.status = page.status;
    if (page?.node_type !== undefined) req.node_type = page.node_type;
    if (page?.backend !== undefined) req.backend = page.backend;
    if (page?.model !== undefined) req.model = page.model;
    const resp = await this.listFleetNodes(req);
    return wholeSetPage(resp.nodes ?? []);
  }

  async listWorkflowsPage(
    tenantID: string,
    _page?: PageParams,
  ): Promise<PageResult<WorkflowDefinition>> {
    const resp = await this.listWorkflows(tenantID);
    return wholeSetPage(resp.workflows ?? []);
  }

  async listJobs(_tenantID: string, _taskID?: string): Promise<JobStatus[]> { return noQuicRoute('listJobs'); }
  async getJobStatus(_tenantID: string, _jobID: string): Promise<JobStatus> { return noQuicRoute('getJobStatus'); }
  async startJob(_req: StartJobRequest): Promise<StartJobResponse> { return noQuicRoute('startJob'); }
  async cancelJob(_tenantID: string, _jobID: string): Promise<void> { return noQuicRoute('cancelJob'); }
  async listFleetNodes(_req: ListFleetNodesRequest): Promise<ListFleetNodesResponse> { return noQuicRoute('listFleetNodes'); }
  async getFleetNode(_req: GetFleetNodeRequest): Promise<FleetNode> { return noQuicRoute('getFleetNode'); }
  async listSessions(_req: ListSessionsRequest): Promise<ListSessionsResponse> { return noQuicRoute('listSessions'); }
  async listTasks(_tenantID: string): Promise<Task[]> { return noQuicRoute('listTasks'); }
  async getTask(_tenantID: string, _taskID: string): Promise<Task> { return noQuicRoute('getTask'); }
  async listBackends(): Promise<AgentBackendInfo[]> { return noQuicRoute('listBackends'); }

  async listRunners(): Promise<JobRunnerInfo[]> { return noQuicRoute('listRunners'); }
  async listBackendModels(_agentID: string, _backendName: string): Promise<string[]> { return noQuicRoute('listBackendModels'); }
  async healthCheck(): Promise<HealthCheckReport> { return noQuicRoute('healthCheck'); }
  subscribeJobEvents(_tenantID: string, _jobID: string, _onEvent: (event: JobEvent) => void): () => void { return noQuicRoute('subscribeJobEvents'); }
  async spawnSession(_req: SpawnSessionRequest): Promise<SpawnSessionResponse> { return noQuicRoute('spawnSession'); }
  async promptSession(_req: PromptSessionRequest): Promise<PromptSessionResponse> { return noQuicRoute('promptSession'); }
  async closeSession(_req: CloseSessionRequest): Promise<CloseSessionResponse> { return noQuicRoute('closeSession'); }
  async rotateSessionAccount(_req: RotateSessionAccountRequest): Promise<RotateSessionAccountResponse> { return noQuicRoute('rotateSessionAccount'); }
  async getSessionHistory(_req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> { return noQuicRoute('getSessionHistory'); }
  async getSessionConversation(_req: GetSessionConversationRequest): Promise<GetSessionConversationResponse> { return noQuicRoute('getSessionConversation'); }
  async getSpendReport(_req: GetSpendReportRequest): Promise<GetSpendReportResponse> { return noQuicRoute('getSpendReport'); }
  async getTenantSpendPolicy(_req: GetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> { return noQuicRoute('getTenantSpendPolicy'); }
  async setTenantSpendPolicy(_req: SetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> { return noQuicRoute('setTenantSpendPolicy'); }
  async getCostSettings(_req: GetCostSettingsRequest): Promise<CostSettings> { return noQuicRoute('getCostSettings'); }
  async setCostSettings(_req: SetCostSettingsRequest): Promise<CostSettings> { return noQuicRoute('setCostSettings'); }
  async getWorkerImageStatus(): Promise<WorkerImageStatus> { return noQuicRoute('getWorkerImageStatus'); }
  async refreshWorkerImage(): Promise<WorkerImageStatus> { return noQuicRoute('refreshWorkerImage'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *subscribeSession(_req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope> { return noQuicRoute('subscribeSession'); }
  async deployFleetNode(_spec: DeployFleetNodeSpec): Promise<DeployFleetNodeResult> { throw new DeployNotSupportedError('quic'); }
  async inference(_req: InferenceRequest): Promise<InferenceResponse> { return noQuicRoute('inference'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *inferenceStream(_req: InferenceRequest, _signal?: AbortSignal): AsyncIterable<InferenceChunk> { return noQuicRoute('inferenceStream'); }
  async createWorkflow(_req: CreateWorkflowRequest): Promise<CreateWorkflowResponse> { return noQuicRoute('createWorkflow'); }
  async listWorkflows(_tenantID: string): Promise<ListWorkflowsResponse> { return noQuicRoute('listWorkflows'); }
  async getWorkflow(_tenantID: string, _name: string): Promise<WorkflowDefinition> { return noQuicRoute('getWorkflow'); }
  async updateWorkflow(_name: string, _def: WorkflowDefinition): Promise<WorkflowDefinition> { return noQuicRoute('updateWorkflow'); }
  async deleteWorkflow(_tenantID: string, _name: string): Promise<void> { return noQuicRoute('deleteWorkflow'); }
  async createTask(_req: CreateTaskRequest): Promise<CreateTaskResponse> { return noQuicRoute('createTask'); }
  async claimTask(_req: ClaimTaskRequest): Promise<void> { return noQuicRoute('claimTask'); }
  async completeTask(_req: CompleteTaskRequest): Promise<void> { return noQuicRoute('completeTask'); }
  async readmitTask(_tenantID: string, _taskID: string): Promise<void> { return noQuicRoute('readmitTask'); }
  async deleteTask(_req: import('./types.js').DeleteTaskRequest): Promise<import('./types.js').DeleteTaskResponse> { return noQuicRoute('deleteTask'); }
  async listAttention(_tenantID: string, _page?: import('./page.js').AttentionPageParams): Promise<import('./page.js').PageResult<import('./types.js').AttentionItem>> { return noQuicRoute('listAttention'); }
  async resolveAttention(_req: import('./types.js').AttentionResolveRequest): Promise<import('./types.js').AttentionItem> { return noQuicRoute('resolveAttention'); }
  async decomposeTask(_req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> { return noQuicRoute('decomposeTask'); }
  async registerFleetNode(_req: RegisterFleetNodeRequest): Promise<RegisterFleetNodeResponse> { return noQuicRoute('registerFleetNode'); }
  async removeFleetNode(_req: RemoveFleetNodeRequest): Promise<void> { return noQuicRoute('removeFleetNode'); }
  async disableFleetNode(_req: import('./types.js').DisableFleetNodeRequest): Promise<import('./types.js').DisableFleetNodeResponse> { return noQuicRoute('disableFleetNode'); }
  async enableFleetNode(_req: import('./types.js').EnableFleetNodeRequest): Promise<import('./types.js').EnableFleetNodeResponse> { return noQuicRoute('enableFleetNode'); }
}

class QuicAuthClient implements AuthClient {
  async authValidate(_token?: string): Promise<boolean> { return frameOnly('auth', 'authValidate'); }
  async authLogin(_credentials: { username: string; password: string }): Promise<void> { return frameOnly('auth', 'authLogin'); }
  async webAuthnBeginLogin(_tenantID: string, _email: string): Promise<import('./types.js').WebAuthnLoginChallenge> { return frameOnly('auth', 'webAuthnBeginLogin'); }
  async webAuthnFinishLogin(_challenge: import('./types.js').WebAuthnLoginChallenge, _credential: import('./types.js').PublicKeyCredentialJSON): Promise<import('./types.js').AuthTokenResponse> { return frameOnly('auth', 'webAuthnFinishLogin'); }
  async loginPIV(): Promise<import('./types.js').AuthTokenResponse> { return frameOnly('auth', 'loginPIV'); }
  async logout(_token?: string): Promise<void> { return frameOnly('auth', 'logout'); }
  async registerBegin(_req: import('./interfaces.js').SelfRegisterBeginRequest): Promise<import('./interfaces.js').SelfRegisterBeginResponse> { return frameOnly('auth', 'registerBegin'); }
  async registerFinish(_req: import('./interfaces.js').SelfRegisterFinishRequest): Promise<import('./interfaces.js').SelfRegisterFinishResponse> { return frameOnly('auth', 'registerFinish'); }
}

// QuicBarrierClient refuses every verb: go-qrdb serves barrier.* on the QUIC
// listener's frame protocol, which this SDK does not speak.
export class QuicBarrierClient implements BarrierClient {
  async status(): Promise<BarrierStatusResponse> { return frameOnly('barrier', 'status'); }
  async initialize(_req: BarrierInitRequest): Promise<void> { return frameOnly('barrier', 'initialize'); }
  async unseal(_req: BarrierUnsealRequest): Promise<void> { return frameOnly('barrier', 'unseal'); }
  async seal(_req?: BarrierSealRequest): Promise<void> { return frameOnly('barrier', 'seal'); }
  async initializeShamir(_req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse> { return frameOnly('barrier', 'initializeShamir'); }
  async unsealWithShare(_req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse> { return frameOnly('barrier', 'unsealWithShare'); }
  async unsealWithShares(_req: BarrierUnsealSharesRequest): Promise<void> { return frameOnly('barrier', 'unsealWithShares'); }
  async rekey(_req: BarrierRekeyRequest): Promise<BarrierRekeyResponse> { return frameOnly('barrier', 'rekey'); }
  async generateRecoveryKeys(_req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse> { return frameOnly('barrier', 'generateRecoveryKeys'); }
  async recoverWithKeys(_req: BarrierRecoverWithKeysRequest): Promise<void> { return frameOnly('barrier', 'recoverWithKeys'); }
  async deleteRecoveryKeys(): Promise<void> { return frameOnly('barrier', 'deleteRecoveryKeys'); }
  async recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse> { return frameOnly('barrier', 'recoveryStatus'); }
  async generateRootToken(_req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse> { return frameOnly('barrier', 'generateRootToken'); }
  async enableEncryption(_req: BarrierEnableEncryptionRequest): Promise<BarrierEnableEncryptionResponse> { return frameOnly('barrier', 'enableEncryption'); }
}

// QuicCeremonyClient refuses every verb: go-qrdb serves ceremony.* on the QUIC
// listener's frame protocol, which this SDK does not speak.
export class QuicCeremonyClient implements CeremonyClient {
  async initialize(_req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse> { return frameOnly('ceremony', 'initialize'); }
  async status(): Promise<CeremonyStatusResponse> { return frameOnly('ceremony', 'status'); }
  async claimCertBegin(_req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse> { return frameOnly('ceremony', 'claimCertBegin'); }
  async claimCertComplete(_req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse> { return frameOnly('ceremony', 'claimCertComplete'); }
  async claimShareBegin(_req: CeremonyClaimShareBeginRequest): Promise<CeremonyClaimShareBeginResponse> { return frameOnly('ceremony', 'claimShareBegin'); }
  async claimShareComplete(_req: CeremonyClaimShareCompleteRequest): Promise<CeremonyClaimShareCompleteResponse> { return frameOnly('ceremony', 'claimShareComplete'); }
  async signCSR(_req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse> { return frameOnly('ceremony', 'signCSR'); }
  async enrollExternalOfficer(_req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse> { return frameOnly('ceremony', 'enrollExternalOfficer'); }
}

// QuicClient is the entry point for the QUIC transport. It sends nothing: every
// verb refuses with a typed reason (see quicUnsupported).
export class QuicClient implements Client {
  readonly connection: ConnectionClient;
  readonly orchestrator: OrchestratorClient;
  readonly auth: AuthClient;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly data: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly vectors: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly timeseries: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly objects: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly rag: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly embeddings: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly messaging: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly scheduler: any = null;
  readonly projects = unsupportedClient<ProjectsClient>('quic', 'projects', quicUnsupported);
  readonly sdlc = unsupportedClient<SdlcClient>('quic', 'sdlc', quicUnsupported);
  readonly accounts = unsupportedClient<AccountsClient>('quic', 'accounts', quicUnsupported);
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly agents: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly iam: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly kms: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly secrets: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly ca: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly trust: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly crypto: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly dek: any = null;
  readonly vectorAdmin: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly dynamicSecrets: any = null;
  readonly barrier: BarrierClient;
  // Per-tenant barrier is a REST-only surface; non-REST transports stub it.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly tenantBarrier: any = null;
  readonly ceremony: CeremonyClient;
  // cluster status/management is exposed over the REST transport; not wired here.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly cluster: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly seal: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly sshKeys: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly ssh: any = null;

  constructor(endpoint: string, _config: ClientConfig = {}) {
    this.connection = new QuicConnectionClient(endpoint);
    this.orchestrator = new QuicOrchestratorClient();
    this.auth = new QuicAuthClient();
    this.barrier = new QuicBarrierClient();
    this.ceremony = new QuicCeremonyClient();
  }
  async logout(_token?: string): Promise<void> {
    // Non-REST transports don't manage bearer tokens via the REST logout verb.
    // The caller is responsible for clearing local credentials; this is a no-op
    // so the unified AuthClient surface compiles across every transport.
  }
}
