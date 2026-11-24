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

// GrpcWebClient is the transport implementation for gRPC-Web.
//
// Until go-qrdb's gRPC-Web option is in the pinned release and this SDK has
// an @connectrpc/connect-web client (docs/SPEC.md, multiprotocol-api), every
// verb refuses with GrpcWebNotImplementedError and nothing reaches the network.
//
// Proto definitions: api/proto/orchestrator.proto
// gRPC service: OrchestratorService
//   - SubscribeSessionEvents(SubscribeSessionRequest) returns (stream SessionEventEnvelope)
//   - GetSessionHistory(GetSessionHistoryRequest) returns (GetSessionHistoryResponse)

import type {
  AuthClient,
  BarrierClient,
  CeremonyClient,
  Client,
  ConnectionClient,
  OrchestratorClient,
} from './interfaces.js';
import type { AccountsClient } from './accounts.js';
import type { PromptsClient } from './prompts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
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

// grpcWebNotImplemented is why every gRPC-Web verb refuses. The server's
// gRPC-Web endpoint comes from go-qrdb, whose pinned release has no sdk/go
// option to turn it on, and the client docs/SPEC.md (multiprotocol-api) names,
// @connectrpc/connect-web, is not wired into this SDK.
const grpcWebNotImplemented =
  'go-qrdb serves no gRPC-Web endpoint yet: the pinned go-qrdb release has no sdk/go option ' +
  'that turns it on, and this SDK has no @connectrpc/connect-web client yet; use the rest transport';

// grpcWebRefusalTarget returns the family and method a GrpcWebNotImplementedError
// names. `method` is `family.verb` or a bare verb: it is split at the first
// `.`, and a bare verb becomes the family with no method.
function grpcWebRefusalTarget(method: string): [string, string | undefined] {
  const dot = method.indexOf('.');
  if (dot < 0) {
    return [method, undefined];
  }
  return [method.slice(0, dot), method.slice(dot + 1)];
}

// GrpcWebNotImplementedError is the gRPC-Web transport's refusal of one verb.
// It is a TransportNotSupportedError naming `family.verb`, like
// QuicNotImplementedError, so a caller that catches the base class handles it.
// The constructor takes `family.verb` or a bare verb.
export class GrpcWebNotImplementedError extends TransportNotSupportedError {
  /**
   * `method` is `family.verb` (for example `prompts.list`) or a bare verb
   * (for example `listJobs`).
   */
  constructor(method: string) {
    super(
      'grpc-web',
      grpcWebRefusalTarget(method)[0],
      grpcWebNotImplemented,
      grpcWebRefusalTarget(method)[1],
    );
    this.name = 'GrpcWebNotImplementedError';
  }
}

// notImpl throws the gRPC-Web refusal for family.method.
function notImpl(family: string, method: string): never {
  throw new GrpcWebNotImplementedError(`${family}.${method}`);
}

// grpcWebNotImplementedClient returns a client of type T whose every verb
// rejects with GrpcWebNotImplementedError naming `family.verb`. It is the
// gRPC-Web refusal docs/SPEC.md (multiprotocol-api) requires until the
// go-qrdb option lands. `then` and symbol keys read as undefined, so
// awaiting or inspecting the client does not call a verb.
function grpcWebNotImplementedClient<T extends object>(family: string): T {
  return new Proxy({} as T, {
    get(_target, prop) {
      if (typeof prop !== 'string' || prop === 'then') return undefined;
      return async () => {
        throw new GrpcWebNotImplementedError(`${family}.${prop}`);
      };
    },
  });
}

class GrpcWebConnectionClient implements ConnectionClient {
  private readonly _transport = 'grpc-web';
  private readonly _address: string;

  constructor(address: string) {
    this._address = address;
  }

  async connect(_address: string): Promise<void> { notImpl('connection', 'connect'); }
  async disconnect(): Promise<void> { notImpl('connection', 'disconnect'); }
  isConnected(): boolean { return false; }
  getAddress(): string { return this._address; }
  getTransport(): string { return this._transport; }
}

class GrpcWebOrchestratorClient implements OrchestratorClient {
  // All methods delegate to notImpl until the gRPC-Web library is wired in.

  // =========================================================================
  // The paged listings
  //
  // gRPC CARRIES NO PAGING FIELDS. Its request messages are generated
  // from api/proto/orchestrator.proto, which has none, so there is no page to
  // ask for until the proto grows them (go-quicbaas#968 follow-up).
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

  async listJobs(_tenantID: string, _taskID?: string): Promise<JobStatus[]> { return notImpl('orchestrator', 'listJobs'); }
  async getJobStatus(_tenantID: string, _jobID: string): Promise<JobStatus> { return notImpl('orchestrator', 'getJobStatus'); }
  async startJob(_req: StartJobRequest): Promise<StartJobResponse> { return notImpl('orchestrator', 'startJob'); }
  async cancelJob(_tenantID: string, _jobID: string): Promise<void> { return notImpl('orchestrator', 'cancelJob'); }
  async listFleetNodes(_req: ListFleetNodesRequest): Promise<ListFleetNodesResponse> { return notImpl('orchestrator', 'listFleetNodes'); }
  async getFleetNode(_req: GetFleetNodeRequest): Promise<FleetNode> { return notImpl('orchestrator', 'getFleetNode'); }
  async listSessions(_req: ListSessionsRequest): Promise<ListSessionsResponse> { return notImpl('orchestrator', 'listSessions'); }
  async listTasks(_tenantID: string): Promise<Task[]> { return notImpl('orchestrator', 'listTasks'); }
  async getTask(_tenantID: string, _taskID: string): Promise<Task> { return notImpl('orchestrator', 'getTask'); }
  async listBackends(): Promise<AgentBackendInfo[]> { return notImpl('orchestrator', 'listBackends'); }

  async listRunners(): Promise<JobRunnerInfo[]> { return notImpl('orchestrator', 'listRunners'); }
  async listBackendModels(_agentID: string, _backendName: string): Promise<string[]> { return notImpl('orchestrator', 'listBackendModels'); }
  async healthCheck(): Promise<HealthCheckReport> { return notImpl('orchestrator', 'healthCheck'); }
  subscribeJobEvents(_tenantID: string, _jobID: string, _onEvent: (event: JobEvent) => void): () => void { return notImpl('orchestrator', 'subscribeJobEvents'); }
  async spawnSession(_req: SpawnSessionRequest): Promise<SpawnSessionResponse> { return notImpl('orchestrator', 'spawnSession'); }
  async promptSession(_req: PromptSessionRequest): Promise<PromptSessionResponse> { return notImpl('orchestrator', 'promptSession'); }
  async closeSession(_req: CloseSessionRequest): Promise<CloseSessionResponse> { return notImpl('orchestrator', 'closeSession'); }
  async rotateSessionAccount(_req: RotateSessionAccountRequest): Promise<RotateSessionAccountResponse> { return notImpl('orchestrator', 'rotateSessionAccount'); }
  async getSessionHistory(_req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> { return notImpl('orchestrator', 'getSessionHistory'); }
  async getSessionConversation(_req: GetSessionConversationRequest): Promise<GetSessionConversationResponse> { return notImpl('orchestrator', 'getSessionConversation'); }
  async getSpendReport(_req: GetSpendReportRequest): Promise<GetSpendReportResponse> { return notImpl('orchestrator', 'getSpendReport'); }
  async getTenantSpendPolicy(_req: GetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> { return notImpl('orchestrator', 'getTenantSpendPolicy'); }
  async setTenantSpendPolicy(_req: SetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> { return notImpl('orchestrator', 'setTenantSpendPolicy'); }
  async getCostSettings(_req: GetCostSettingsRequest): Promise<CostSettings> { return notImpl('orchestrator', 'getCostSettings'); }
  async setCostSettings(_req: SetCostSettingsRequest): Promise<CostSettings> { return notImpl('orchestrator', 'setCostSettings'); }
  async getWorkerImageStatus(): Promise<WorkerImageStatus> { return notImpl('orchestrator', 'getWorkerImageStatus'); }
  async refreshWorkerImage(): Promise<WorkerImageStatus> { return notImpl('orchestrator', 'refreshWorkerImage'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *subscribeSession(_req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope> { return notImpl('orchestrator', 'subscribeSession'); }
  async deployFleetNode(_spec: DeployFleetNodeSpec): Promise<DeployFleetNodeResult> { throw new DeployNotSupportedError('grpc-web'); }
  async inference(_req: InferenceRequest): Promise<InferenceResponse> { return notImpl('orchestrator', 'inference'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *inferenceStream(_req: InferenceRequest, _signal?: AbortSignal): AsyncIterable<InferenceChunk> { return notImpl('orchestrator', 'inferenceStream'); }
  async createWorkflow(_req: CreateWorkflowRequest): Promise<CreateWorkflowResponse> { return notImpl('orchestrator', 'createWorkflow'); }
  async listWorkflows(_tenantID: string): Promise<ListWorkflowsResponse> { return notImpl('orchestrator', 'listWorkflows'); }
  async getWorkflow(_tenantID: string, _name: string): Promise<WorkflowDefinition> { return notImpl('orchestrator', 'getWorkflow'); }
  async updateWorkflow(_name: string, _def: WorkflowDefinition): Promise<WorkflowDefinition> { return notImpl('orchestrator', 'updateWorkflow'); }
  async deleteWorkflow(_tenantID: string, _name: string): Promise<void> { return notImpl('orchestrator', 'deleteWorkflow'); }
  async createTask(_req: CreateTaskRequest): Promise<CreateTaskResponse> { return notImpl('orchestrator', 'createTask'); }
  async claimTask(_req: ClaimTaskRequest): Promise<void> { return notImpl('orchestrator', 'claimTask'); }
  async completeTask(_req: CompleteTaskRequest): Promise<void> { return notImpl('orchestrator', 'completeTask'); }
  async readmitTask(_tenantID: string, _taskID: string): Promise<void> { return notImpl('orchestrator', 'readmitTask'); }
  async deleteTask(_req: import('./types.js').DeleteTaskRequest): Promise<import('./types.js').DeleteTaskResponse> { return notImpl('orchestrator', 'deleteTask'); }
  async listAttention(_tenantID: string, _page?: import('./page.js').AttentionPageParams): Promise<import('./page.js').PageResult<import('./types.js').AttentionItem>> { return notImpl('orchestrator', 'listAttention'); }
  async resolveAttention(_req: import('./types.js').AttentionResolveRequest): Promise<import('./types.js').AttentionItem> { return notImpl('orchestrator', 'resolveAttention'); }
  async decomposeTask(_req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> { return notImpl('orchestrator', 'decomposeTask'); }
  async registerFleetNode(_req: RegisterFleetNodeRequest): Promise<RegisterFleetNodeResponse> { return notImpl('orchestrator', 'registerFleetNode'); }
  async removeFleetNode(_req: RemoveFleetNodeRequest): Promise<void> { return notImpl('orchestrator', 'removeFleetNode'); }
  async disableFleetNode(_req: import('./types.js').DisableFleetNodeRequest): Promise<import('./types.js').DisableFleetNodeResponse> { return notImpl('orchestrator', 'disableFleetNode'); }
  async enableFleetNode(_req: import('./types.js').EnableFleetNodeRequest): Promise<import('./types.js').EnableFleetNodeResponse> { return notImpl('orchestrator', 'enableFleetNode'); }
}

class GrpcWebAuthClient implements AuthClient {
  async authValidate(_token?: string): Promise<boolean> { return notImpl('auth', 'authValidate'); }
  async authLogin(_credentials: { username: string; password: string }): Promise<void> { return notImpl('auth', 'authLogin'); }
  async webAuthnBeginLogin(_tenantID: string, _email: string): Promise<import('./types.js').WebAuthnLoginChallenge> { return notImpl('auth', 'webAuthnBeginLogin'); }
  async webAuthnFinishLogin(_challenge: import('./types.js').WebAuthnLoginChallenge, _credential: import('./types.js').PublicKeyCredentialJSON): Promise<import('./types.js').AuthTokenResponse> { return notImpl('auth', 'webAuthnFinishLogin'); }
  async loginPIV(): Promise<import('./types.js').AuthTokenResponse> { return notImpl('auth', 'loginPIV'); }
  async logout(_token?: string): Promise<void> { return notImpl('auth', 'logout'); }
  async registerBegin(_req: import('./interfaces.js').SelfRegisterBeginRequest): Promise<import('./interfaces.js').SelfRegisterBeginResponse> { return notImpl('auth', 'registerBegin'); }
  async registerFinish(_req: import('./interfaces.js').SelfRegisterFinishRequest): Promise<import('./interfaces.js').SelfRegisterFinishResponse> { return notImpl('auth', 'registerFinish'); }
}

// GrpcWebBarrierClient is a stub BarrierClient that throws
// GrpcWebNotImplementedError on every call. The barrier surface lives on
// REST today; future gRPC-Web wiring can replace this stub.
export class GrpcWebBarrierClient implements BarrierClient {
  async status(): Promise<BarrierStatusResponse> { return notImpl('barrier', 'status'); }
  async initialize(_req: BarrierInitRequest): Promise<void> { return notImpl('barrier', 'initialize'); }
  async unseal(_req: BarrierUnsealRequest): Promise<void> { return notImpl('barrier', 'unseal'); }
  async seal(_req?: BarrierSealRequest): Promise<void> { return notImpl('barrier', 'seal'); }
  async initializeShamir(_req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse> { return notImpl('barrier', 'initializeShamir'); }
  async unsealWithShare(_req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse> { return notImpl('barrier', 'unsealWithShare'); }
  async unsealWithShares(_req: BarrierUnsealSharesRequest): Promise<void> { return notImpl('barrier', 'unsealWithShares'); }
  async rekey(_req: BarrierRekeyRequest): Promise<BarrierRekeyResponse> { return notImpl('barrier', 'rekey'); }
  async generateRecoveryKeys(_req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse> { return notImpl('barrier', 'generateRecoveryKeys'); }
  async recoverWithKeys(_req: BarrierRecoverWithKeysRequest): Promise<void> { return notImpl('barrier', 'recoverWithKeys'); }
  async deleteRecoveryKeys(): Promise<void> { return notImpl('barrier', 'deleteRecoveryKeys'); }
  async recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse> { return notImpl('barrier', 'recoveryStatus'); }
  async generateRootToken(_req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse> { return notImpl('barrier', 'generateRootToken'); }
  async enableEncryption(_req: BarrierEnableEncryptionRequest): Promise<BarrierEnableEncryptionResponse> { return notImpl('barrier', 'enableEncryption'); }
}

// GrpcWebCeremonyClient is a stub CeremonyClient that throws
// GrpcWebNotImplementedError on every call.
export class GrpcWebCeremonyClient implements CeremonyClient {
  async initialize(_req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse> { return notImpl('ceremony', 'initialize'); }
  async status(): Promise<CeremonyStatusResponse> { return notImpl('ceremony', 'status'); }
  async claimCertBegin(_req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse> { return notImpl('ceremony', 'claimCertBegin'); }
  async claimCertComplete(_req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse> { return notImpl('ceremony', 'claimCertComplete'); }
  async claimShareBegin(_req: CeremonyClaimShareBeginRequest): Promise<CeremonyClaimShareBeginResponse> { return notImpl('ceremony', 'claimShareBegin'); }
  async claimShareComplete(_req: CeremonyClaimShareCompleteRequest): Promise<CeremonyClaimShareCompleteResponse> { return notImpl('ceremony', 'claimShareComplete'); }
  async signCSR(_req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse> { return notImpl('ceremony', 'signCSR'); }
  async enrollExternalOfficer(_req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse> { return notImpl('ceremony', 'enrollExternalOfficer'); }
}

// GrpcWebClient is the stub entry point for the gRPC-Web transport.
// Replace the stub implementations above with real gRPC-Web calls once the
// proto client library is installed.
export class GrpcWebClient implements Client {
  readonly connection: ConnectionClient;
  readonly orchestrator: OrchestratorClient;
  readonly auth: AuthClient;
  // Data sub-clients are not implemented for gRPC-Web — all methods throw
  // GrpcWebNotImplementedError until the transport is wired in.
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
  readonly projects = grpcWebNotImplementedClient<ProjectsClient>('projects');
  readonly sdlc = grpcWebNotImplementedClient<SdlcClient>('sdlc');
  readonly accounts = grpcWebNotImplementedClient<AccountsClient>('accounts');
  readonly prompts = grpcWebNotImplementedClient<PromptsClient>('prompts');
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

  constructor(baseUrl: string, _config: ClientConfig = {}) {
    this.connection = new GrpcWebConnectionClient(baseUrl);
    this.orchestrator = new GrpcWebOrchestratorClient();
    this.auth = new GrpcWebAuthClient();
    this.barrier = new GrpcWebBarrierClient();
    this.ceremony = new GrpcWebCeremonyClient();
  }
  async logout(_token?: string): Promise<void> {
    // Non-REST transports don't manage bearer tokens via the REST logout verb.
    // The caller is responsible for clearing local credentials; this is a no-op
    // so the unified AuthClient surface compiles across every transport.
  }
}
