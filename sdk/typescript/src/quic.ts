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
// Until this transport is wired in, all method calls throw QuicNotImplementedError.
// The go-quicbaas QUIC server endpoint is configured in config.quic.endpoint.

import type {
  AuthClient,
  BarrierClient,
  BootstrapClient,
  CeremonyClient,
  Client,
  ConnectionClient,
  OrchestratorClient,
} from './interfaces.js';
import type {
  AgentBackendInfo,
  JobRunnerInfo,
  BarrierGenerateRecoveryKeysRequest,
  BarrierGenerateRootTokenRequest,
  BarrierHasRecoveryKeysResponse,
  BarrierInitRequest,
  BarrierRecoverWithKeysRequest,
  BarrierRecoveryKeysResponse,
  BarrierRekeyRequest,
  BarrierRekeyResponse,
  BarrierRootTokenResponse,
  BarrierShamirDeleteShareRequest,
  BarrierShamirInitRequest,
  BarrierShamirInitResponse,
  BarrierShamirSharesResponse,
  BarrierStatusResponse,
  BarrierUnsealRequest,
  BarrierUnsealShareRequest,
  BarrierUnsealShareResponse,
  BarrierUnsealSharesRequest,
  BootstrapBundleRequest,
  BootstrapBundleResponse,
  CeremonyClaimCertBeginRequest,
  CeremonyClaimCertBeginResponse,
  CeremonyClaimCertCompleteRequest,
  CeremonyClaimCertCompleteResponse,
  CeremonyClaimShareRequest,
  CeremonyClaimShareResponse,
  CeremonyEnrollExternalOfficerRequest,
  CeremonyEnrollExternalOfficerResponse,
  CeremonyInitializeRequest,
  CeremonyInitializeResponse,
  CeremonySignCSRRequest,
  CeremonySignCSRResponse,
  CeremonyStatusResponse,
  DeployAgentResult,
  DeployAgentSpec,
  InferenceRequest,
  InferenceResponse,
  InferenceChunk,
  AgentRegistration,
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
  GetAgentRequest,
  GetSessionHistoryRequest,
  GetSessionConversationRequest,
  GetSessionConversationResponse,
  GetSpendReportRequest,
  GetSpendReportResponse,
  GetSessionHistoryResponse,
  HealthCheckReport,
  JobEvent,
  JobStatus,
  ListAgentsRequest,
  ListAgentsResponse,
  ListSessionsRequest,
  ListSessionsResponse,
  SessionStatusResponse,
  ListWorkflowsResponse,
  PromptSessionRequest,
  PromptSessionResponse,
  RegisterAgentRequest,
  RegisterAgentResponse,
  RemoveAgentRequest,
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
  type AgentPageParams,
  type JobPageParams,
  type PageParams,
  type PageResult,
  type SessionPageParams,
  type TaskPageParams,
} from './page.js';
import { QuicBaaSError, DeployNotSupportedError } from './errors.js';

// QuicNotImplementedError is thrown by all methods until a QUIC/WebTransport
// library is wired into this file.
export class QuicNotImplementedError extends QuicBaaSError {
  constructor(method: string) {
    super(
      `quic transport: ${method} not implemented. ` +
      'Wire a WebTransport or QUIC client library for the go-quicbaas QUIC endpoint.',
    );
    this.name = 'QuicNotImplementedError';
  }
}

function notImpl(method: string): never {
  throw new QuicNotImplementedError(method);
}

class QuicConnectionClient implements ConnectionClient {
  private readonly _endpoint: string;
  private readonly _transport = 'quic';

  constructor(endpoint: string) {
    this._endpoint = endpoint;
  }

  async connect(_address: string): Promise<void> { notImpl('connect'); }
  async disconnect(): Promise<void> { notImpl('disconnect'); }
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

  async listAgentsPage(
    tenantID: string,
    page?: AgentPageParams,
  ): Promise<PageResult<AgentRegistration>> {
    const req: ListAgentsRequest = { tenant_id: tenantID };
    if (page?.host !== undefined) req.host = page.host;
    if (page?.status !== undefined) req.status = page.status;
    if (page?.agent_type !== undefined) req.agent_type = page.agent_type;
    if (page?.backend !== undefined) req.backend = page.backend;
    if (page?.model !== undefined) req.model = page.model;
    const resp = await this.listAgents(req);
    return wholeSetPage(resp.agents ?? []);
  }

  async listWorkflowsPage(
    tenantID: string,
    _page?: PageParams,
  ): Promise<PageResult<WorkflowDefinition>> {
    const resp = await this.listWorkflows(tenantID);
    return wholeSetPage(resp.workflows ?? []);
  }

  async listJobs(_tenantID: string, _taskID?: string): Promise<JobStatus[]> { return notImpl('listJobs'); }
  async getJobStatus(_tenantID: string, _jobID: string): Promise<JobStatus> { return notImpl('getJobStatus'); }
  async startJob(_req: StartJobRequest): Promise<StartJobResponse> { return notImpl('startJob'); }
  async cancelJob(_tenantID: string, _jobID: string): Promise<void> { return notImpl('cancelJob'); }
  async listAgents(_req: ListAgentsRequest): Promise<ListAgentsResponse> { return notImpl('listAgents'); }
  async getAgentStatus(_req: GetAgentRequest): Promise<AgentRegistration> { return notImpl('getAgentStatus'); }
  async listSessions(_req: ListSessionsRequest): Promise<ListSessionsResponse> { return notImpl('listSessions'); }
  async listTasks(_tenantID: string): Promise<Task[]> { return notImpl('listTasks'); }
  async getTask(_tenantID: string, _taskID: string): Promise<Task> { return notImpl('getTask'); }
  async listBackends(): Promise<AgentBackendInfo[]> { return notImpl('listBackends'); }

  async listRunners(): Promise<JobRunnerInfo[]> { return notImpl('listRunners'); }
  async listBackendModels(_agentID: string, _backendName: string): Promise<string[]> { return notImpl('listBackendModels'); }
  async healthCheck(): Promise<HealthCheckReport> { return notImpl('healthCheck'); }
  subscribeJobEvents(_tenantID: string, _jobID: string, _onEvent: (event: JobEvent) => void): () => void { return notImpl('subscribeJobEvents'); }
  async spawnSession(_req: SpawnSessionRequest): Promise<SpawnSessionResponse> { return notImpl('spawnSession'); }
  async promptSession(_req: PromptSessionRequest): Promise<PromptSessionResponse> { return notImpl('promptSession'); }
  async closeSession(_req: CloseSessionRequest): Promise<CloseSessionResponse> { return notImpl('closeSession'); }
  async rotateSessionAccount(_req: RotateSessionAccountRequest): Promise<RotateSessionAccountResponse> { return notImpl('rotateSessionAccount'); }
  async getSessionHistory(_req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> { return notImpl('getSessionHistory'); }
  async getSessionConversation(_req: GetSessionConversationRequest): Promise<GetSessionConversationResponse> { return notImpl('getSessionConversation'); }
  async getSpendReport(_req: GetSpendReportRequest): Promise<GetSpendReportResponse> { return notImpl('getSpendReport'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *subscribeSession(_req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope> { return notImpl('subscribeSession'); }
  async deployAgent(_spec: DeployAgentSpec): Promise<DeployAgentResult> { throw new DeployNotSupportedError('quic'); }
  async inference(_req: InferenceRequest): Promise<InferenceResponse> { return notImpl('inference'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *inferenceStream(_req: InferenceRequest, _signal?: AbortSignal): AsyncIterable<InferenceChunk> { return notImpl('inferenceStream'); }
  async createWorkflow(_req: CreateWorkflowRequest): Promise<CreateWorkflowResponse> { return notImpl('createWorkflow'); }
  async listWorkflows(_tenantID: string): Promise<ListWorkflowsResponse> { return notImpl('listWorkflows'); }
  async getWorkflow(_tenantID: string, _name: string): Promise<WorkflowDefinition> { return notImpl('getWorkflow'); }
  async updateWorkflow(_name: string, _def: WorkflowDefinition): Promise<WorkflowDefinition> { return notImpl('updateWorkflow'); }
  async deleteWorkflow(_tenantID: string, _name: string): Promise<void> { return notImpl('deleteWorkflow'); }
  async createTask(_req: CreateTaskRequest): Promise<CreateTaskResponse> { return notImpl('createTask'); }
  async claimTask(_req: ClaimTaskRequest): Promise<void> { return notImpl('claimTask'); }
  async completeTask(_req: CompleteTaskRequest): Promise<void> { return notImpl('completeTask'); }
  async readmitTask(_tenantID: string, _taskID: string): Promise<void> { return notImpl('readmitTask'); }
  async deleteTask(_req: import('./types.js').DeleteTaskRequest): Promise<import('./types.js').DeleteTaskResponse> { return notImpl('deleteTask'); }
  async decomposeTask(_req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> { return notImpl('decomposeTask'); }
  async registerAgent(_req: RegisterAgentRequest): Promise<RegisterAgentResponse> { return notImpl('registerAgent'); }
  async removeAgent(_req: RemoveAgentRequest): Promise<void> { return notImpl('removeAgent'); }
  async disableAgent(_req: import('./types.js').DisableAgentRequest): Promise<import('./types.js').DisableAgentResponse> { return notImpl('disableAgent'); }
  async enableAgent(_req: import('./types.js').EnableAgentRequest): Promise<import('./types.js').EnableAgentResponse> { return notImpl('enableAgent'); }
}

class QuicAuthClient implements AuthClient {
  async authValidate(_token?: string): Promise<boolean> { return notImpl('authValidate'); }
  async authLogin(_credentials: { username: string; password: string }): Promise<void> { return notImpl('authLogin'); }
  async webAuthnBeginLogin(_tenantID: string, _email: string): Promise<import('./types.js').WebAuthnLoginChallenge> { return notImpl('webAuthnBeginLogin'); }
  async webAuthnFinishLogin(_challenge: import('./types.js').WebAuthnLoginChallenge, _credential: import('./types.js').PublicKeyCredentialJSON): Promise<import('./types.js').AuthTokenResponse> { return notImpl('webAuthnFinishLogin'); }
  async loginPIV(): Promise<import('./types.js').AuthTokenResponse> { return notImpl('loginPIV'); }
  async logout(_token?: string): Promise<void> { return notImpl('logout'); }
  async registerBegin(_req: import('./interfaces.js').SelfRegisterBeginRequest): Promise<import('./interfaces.js').SelfRegisterBeginResponse> { return notImpl('registerBegin'); }
  async registerFinish(_req: import('./interfaces.js').SelfRegisterFinishRequest): Promise<import('./interfaces.js').SelfRegisterFinishResponse> { return notImpl('registerFinish'); }
}

// QuicBarrierClient is a stub BarrierClient that throws QuicNotImplementedError
// on every call. Real QUIC streaming wiring will replace this stub.
export class QuicBarrierClient implements BarrierClient {
  async status(): Promise<BarrierStatusResponse> { return notImpl('barrier.status'); }
  async initialize(_req: BarrierInitRequest): Promise<void> { return notImpl('barrier.initialize'); }
  async unseal(_req: BarrierUnsealRequest): Promise<void> { return notImpl('barrier.unseal'); }
  async seal(): Promise<void> { return notImpl('barrier.seal'); }
  async initializeShamir(_req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse> { return notImpl('barrier.initializeShamir'); }
  async unsealWithShare(_req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse> { return notImpl('barrier.unsealWithShare'); }
  async unsealWithShares(_req: BarrierUnsealSharesRequest): Promise<void> { return notImpl('barrier.unsealWithShares'); }
  async shamirListShares(): Promise<BarrierShamirSharesResponse> { return notImpl('barrier.shamirListShares'); }
  async shamirDeleteShare(_req: BarrierShamirDeleteShareRequest): Promise<void> { return notImpl('barrier.shamirDeleteShare'); }
  async shamirDeleteAllShares(): Promise<void> { return notImpl('barrier.shamirDeleteAllShares'); }
  async shamirVerify(): Promise<void> { return notImpl('barrier.shamirVerify'); }
  async rekey(_req: BarrierRekeyRequest): Promise<BarrierRekeyResponse> { return notImpl('barrier.rekey'); }
  async generateRecoveryKeys(_req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse> { return notImpl('barrier.generateRecoveryKeys'); }
  async recoverWithKeys(_req: BarrierRecoverWithKeysRequest): Promise<void> { return notImpl('barrier.recoverWithKeys'); }
  async deleteRecoveryKeys(): Promise<void> { return notImpl('barrier.deleteRecoveryKeys'); }
  async recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse> { return notImpl('barrier.recoveryStatus'); }
  async generateRootToken(_req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse> { return notImpl('barrier.generateRootToken'); }
}

// QuicCeremonyClient is a stub CeremonyClient that throws
// QuicNotImplementedError on every call.
export class QuicCeremonyClient implements CeremonyClient {
  async initialize(_req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse> { return notImpl('ceremony.initialize'); }
  async status(): Promise<CeremonyStatusResponse> { return notImpl('ceremony.status'); }
  async claimCertBegin(_req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse> { return notImpl('ceremony.claimCertBegin'); }
  async claimCertComplete(_req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse> { return notImpl('ceremony.claimCertComplete'); }
  async claimShare(_req: CeremonyClaimShareRequest): Promise<CeremonyClaimShareResponse> { return notImpl('ceremony.claimShare'); }
  async signCSR(_req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse> { return notImpl('ceremony.signCSR'); }
  async enrollExternalOfficer(_req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse> { return notImpl('ceremony.enrollExternalOfficer'); }
}

// QuicBootstrapClient is a stub BootstrapClient that throws
// QuicNotImplementedError on every call.
export class QuicBootstrapClient implements BootstrapClient {
  async auto(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> { return notImpl('bootstrap.auto'); }
  async dane(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> { return notImpl('bootstrap.dane'); }
  async noise(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> { return notImpl('bootstrap.noise'); }
  async spki(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> { return notImpl('bootstrap.spki'); }
}

// QuicClient is the stub entry point for the QUIC transport.
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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly projects: any = null;
  readonly sdlc: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly accounts: any = null;
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
  readonly bootstrap: BootstrapClient;
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
    this.bootstrap = new QuicBootstrapClient();
  }
  async logout(_token?: string): Promise<void> {
    // Non-REST transports don't manage bearer tokens via the REST logout verb.
    // The caller is responsible for clearing local credentials; this is a no-op
    // so the unified AuthClient surface compiles across every transport.
  }
}
