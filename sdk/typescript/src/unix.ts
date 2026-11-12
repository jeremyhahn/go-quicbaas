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

// UnixClient is the transport implementation for Unix domain sockets.
//
// The server's Unix listener serves native gRPC over AF_UNIX and nothing
// else. This SDK carries no gRPC client or generated stubs, so every verb
// that needs the daemon rejects with TransportNotSupportedError naming the
// family, the verb and the reason (see unixUnsupported). Browsers cannot open
// a socket at all.

import type { AuthClient, Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
import type { AccountsClient } from './accounts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
import { unsupportedClient } from './unsupported.js';
import type {
  AgentBackendInfo,
  JobRunnerInfo,
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

// refuse rejects one verb of a family this transport cannot carry yet.
function refuse(family: string, method: string): never {
  throw new TransportNotSupportedError('unix', family, unixUnsupported, method);
}

// unixUnsupported is why every family refuses here. The Unix wire is gRPC
// over AF_UNIX, and the server does serve the orchestrator, auth, projects,
// sdlc and accounts services on it; but speaking gRPC needs an HTTP/2
// gRPC client and generated stubs, which this SDK (no runtime dependencies)
// does not carry, and a browser cannot open a socket at all.
const unixUnsupported =
  'the Unix wire is gRPC over AF_UNIX and the SDK has no Node gRPC client or ' +
  'generated stubs; use the rest transport';

class UnixConnectionClient implements ConnectionClient {
  private readonly _socketPath: string;
  private readonly _transport = 'unix';

  constructor(socketPath: string) {
    this._socketPath = socketPath;
  }

  async connect(_address: string): Promise<void> { refuse('connection', 'connect'); }
  async disconnect(): Promise<void> { refuse('connection', 'disconnect'); }
  isConnected(): boolean { return false; }
  getAddress(): string { return this._socketPath; }
  getTransport(): string { return this._transport; }
}

class UnixOrchestratorClient implements OrchestratorClient {

  // =========================================================================
  // The paged listings
  //
  // This transport cannot speak the Unix socket's gRPC yet, so every listing
  // here refuses through its unpaged sibling rather than answering a page it
  // never fetched.
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

  async listJobs(_tenantID: string, _taskID?: string): Promise<JobStatus[]> { return refuse('orchestrator', 'listJobs'); }
  async getJobStatus(_tenantID: string, _jobID: string): Promise<JobStatus> { return refuse('orchestrator', 'getJobStatus'); }
  async startJob(_req: StartJobRequest): Promise<StartJobResponse> { return refuse('orchestrator', 'startJob'); }
  async cancelJob(_tenantID: string, _jobID: string): Promise<void> { return refuse('orchestrator', 'cancelJob'); }
  async listFleetNodes(_req: ListFleetNodesRequest): Promise<ListFleetNodesResponse> { return refuse('orchestrator', 'listFleetNodes'); }
  async getFleetNode(_req: GetFleetNodeRequest): Promise<FleetNode> { return refuse('orchestrator', 'getFleetNode'); }
  async listSessions(_req: ListSessionsRequest): Promise<ListSessionsResponse> { return refuse('orchestrator', 'listSessions'); }
  async listTasks(_tenantID: string): Promise<Task[]> { return refuse('orchestrator', 'listTasks'); }
  async getTask(_tenantID: string, _taskID: string): Promise<Task> { return refuse('orchestrator', 'getTask'); }
  async listBackends(): Promise<AgentBackendInfo[]> { return refuse('orchestrator', 'listBackends'); }

  async listRunners(): Promise<JobRunnerInfo[]> { return refuse('orchestrator', 'listRunners'); }
  async listBackendModels(_agentID: string, _backendName: string): Promise<string[]> { return refuse('orchestrator', 'listBackendModels'); }
  async healthCheck(): Promise<HealthCheckReport> { return refuse('orchestrator', 'healthCheck'); }
  subscribeJobEvents(_tenantID: string, _jobID: string, _onEvent: (event: JobEvent) => void): () => void { return refuse('orchestrator', 'subscribeJobEvents'); }
  async spawnSession(_req: SpawnSessionRequest): Promise<SpawnSessionResponse> { return refuse('orchestrator', 'spawnSession'); }
  async promptSession(_req: PromptSessionRequest): Promise<PromptSessionResponse> { return refuse('orchestrator', 'promptSession'); }
  async closeSession(_req: CloseSessionRequest): Promise<CloseSessionResponse> { return refuse('orchestrator', 'closeSession'); }
  async rotateSessionAccount(_req: RotateSessionAccountRequest): Promise<RotateSessionAccountResponse> { return refuse('orchestrator', 'rotateSessionAccount'); }
  async getSessionHistory(_req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> { return refuse('orchestrator', 'getSessionHistory'); }
  async getSessionConversation(_req: GetSessionConversationRequest): Promise<GetSessionConversationResponse> { return refuse('orchestrator', 'getSessionConversation'); }
  async getSpendReport(_req: GetSpendReportRequest): Promise<GetSpendReportResponse> { return refuse('orchestrator', 'getSpendReport'); }
  async getTenantSpendPolicy(_req: GetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> { return refuse('orchestrator', 'getTenantSpendPolicy'); }
  async setTenantSpendPolicy(_req: SetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> { return refuse('orchestrator', 'setTenantSpendPolicy'); }
  async getCostSettings(_req: GetCostSettingsRequest): Promise<CostSettings> { return refuse('orchestrator', 'getCostSettings'); }
  async setCostSettings(_req: SetCostSettingsRequest): Promise<CostSettings> { return refuse('orchestrator', 'setCostSettings'); }
  async getWorkerImageStatus(): Promise<WorkerImageStatus> { return refuse('orchestrator', 'getWorkerImageStatus'); }
  async refreshWorkerImage(): Promise<WorkerImageStatus> { return refuse('orchestrator', 'refreshWorkerImage'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *subscribeSession(_req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope> { return refuse('orchestrator', 'subscribeSession'); }
  async deployFleetNode(_spec: DeployFleetNodeSpec): Promise<DeployFleetNodeResult> { throw new DeployNotSupportedError('unix'); }
  async inference(_req: InferenceRequest): Promise<InferenceResponse> { return refuse('orchestrator', 'inference'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *inferenceStream(_req: InferenceRequest, _signal?: AbortSignal): AsyncIterable<InferenceChunk> { return refuse('orchestrator', 'inferenceStream'); }
  async createWorkflow(_req: CreateWorkflowRequest): Promise<CreateWorkflowResponse> { return refuse('orchestrator', 'createWorkflow'); }
  async listWorkflows(_tenantID: string): Promise<ListWorkflowsResponse> { return refuse('orchestrator', 'listWorkflows'); }
  async getWorkflow(_tenantID: string, _name: string): Promise<WorkflowDefinition> { return refuse('orchestrator', 'getWorkflow'); }
  async updateWorkflow(_name: string, _def: WorkflowDefinition): Promise<WorkflowDefinition> { return refuse('orchestrator', 'updateWorkflow'); }
  async deleteWorkflow(_tenantID: string, _name: string): Promise<void> { return refuse('orchestrator', 'deleteWorkflow'); }
  async createTask(_req: CreateTaskRequest): Promise<CreateTaskResponse> { return refuse('orchestrator', 'createTask'); }
  async claimTask(_req: ClaimTaskRequest): Promise<void> { return refuse('orchestrator', 'claimTask'); }
  async completeTask(_req: CompleteTaskRequest): Promise<void> { return refuse('orchestrator', 'completeTask'); }
  async readmitTask(_tenantID: string, _taskID: string): Promise<void> { return refuse('orchestrator', 'readmitTask'); }
  async deleteTask(_req: import('./types.js').DeleteTaskRequest): Promise<import('./types.js').DeleteTaskResponse> { return refuse('orchestrator', 'deleteTask'); }
  async listAttention(_tenantID: string, _page?: import('./page.js').AttentionPageParams): Promise<import('./page.js').PageResult<import('./types.js').AttentionItem>> { return refuse('orchestrator', 'listAttention'); }
  async resolveAttention(_req: import('./types.js').AttentionResolveRequest): Promise<import('./types.js').AttentionItem> { return refuse('orchestrator', 'resolveAttention'); }
  async decomposeTask(_req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> { return refuse('orchestrator', 'decomposeTask'); }
  async registerFleetNode(_req: RegisterFleetNodeRequest): Promise<RegisterFleetNodeResponse> { return refuse('orchestrator', 'registerFleetNode'); }
  async removeFleetNode(_req: RemoveFleetNodeRequest): Promise<void> { return refuse('orchestrator', 'removeFleetNode'); }
  async disableFleetNode(_req: import('./types.js').DisableFleetNodeRequest): Promise<import('./types.js').DisableFleetNodeResponse> { return refuse('orchestrator', 'disableFleetNode'); }
  async enableFleetNode(_req: import('./types.js').EnableFleetNodeRequest): Promise<import('./types.js').EnableFleetNodeResponse> { return refuse('orchestrator', 'enableFleetNode'); }
}

class UnixAuthClient implements AuthClient {
  async authValidate(_token?: string): Promise<boolean> { return refuse('auth', 'authValidate'); }
  async authLogin(_credentials: { username: string; password: string }): Promise<void> { return refuse('auth', 'authLogin'); }
  async webAuthnBeginLogin(_tenantID: string, _email: string): Promise<import('./types.js').WebAuthnLoginChallenge> { return refuse('auth', 'webAuthnBeginLogin'); }
  async webAuthnFinishLogin(_challenge: import('./types.js').WebAuthnLoginChallenge, _credential: import('./types.js').PublicKeyCredentialJSON): Promise<import('./types.js').AuthTokenResponse> { return refuse('auth', 'webAuthnFinishLogin'); }
  async loginPIV(): Promise<import('./types.js').AuthTokenResponse> { return refuse('auth', 'loginPIV'); }
  async logout(_token?: string): Promise<void> { return refuse('auth', 'logout'); }
  async registerBegin(_req: import('./interfaces.js').SelfRegisterBeginRequest): Promise<import('./interfaces.js').SelfRegisterBeginResponse> { return refuse('auth', 'registerBegin'); }
  async registerFinish(_req: import('./interfaces.js').SelfRegisterFinishRequest): Promise<import('./interfaces.js').SelfRegisterFinishResponse> { return refuse('auth', 'registerFinish'); }
}

// UnixClient is the stub entry point for the Unix domain socket transport.
export class UnixClient implements Client {
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
  readonly projects = unsupportedClient<ProjectsClient>('unix', 'projects', unixUnsupported);
  readonly sdlc = unsupportedClient<SdlcClient>('unix', 'sdlc', unixUnsupported);
  readonly accounts = unsupportedClient<AccountsClient>('unix', 'accounts', unixUnsupported);
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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly cache: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly dynamicSecrets: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly barrier: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly tenantBarrier: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly ceremony: any = null;
  // cluster status/management is exposed over the REST transport; not wired here.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly cluster: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly seal: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly sshKeys: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly ssh: any = null;

  constructor(socketPath: string, _config: ClientConfig = {}) {
    this.connection = new UnixConnectionClient(socketPath);
    this.orchestrator = new UnixOrchestratorClient();
    this.auth = new UnixAuthClient();
  }
  async logout(_token?: string): Promise<void> {
    // Non-REST transports don't manage bearer tokens via the REST logout verb.
    // The caller is responsible for clearing local credentials; this is a no-op
    // so the unified AuthClient surface compiles across every transport.
  }
}
