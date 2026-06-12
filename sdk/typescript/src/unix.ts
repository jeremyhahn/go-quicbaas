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
// OPERATOR NOTE: This transport is only available in Node.js environments.
// It requires a Node.js net.Socket adapter that speaks the same framing
// protocol as the go-quicbaas UNIX transport server. The implementation
// must be added here before this transport can be used. Browser environments
// will always throw UnixNotImplementedError.

import type { AuthClient, Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
import type {
  AgentBackendInfo,
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
  GetSessionHistoryResponse,
  HealthCheckReport,
  JobEvent,
  JobStatus,
  ListAgentsRequest,
  ListAgentsResponse,
  ListSessionsRequest,
  ListSessionsResponse,
  ListWorkflowsResponse,
  PromptSessionRequest,
  PromptSessionResponse,
  RegisterAgentRequest,
  RegisterAgentResponse,
  RemoveAgentRequest,
  SessionEventEnvelope,
  SpawnSessionRequest,
  SpawnSessionResponse,
  StartJobRequest,
  StartJobResponse,
  SubscribeSessionEventsRequest,
  Task,
  WorkflowDefinition,
} from './types.js';
import { QuicBaaSError, DeployNotSupportedError } from './errors.js';

// UnixNotImplementedError is thrown by all methods until the Unix socket
// adapter is wired into this file.
export class UnixNotImplementedError extends QuicBaaSError {
  constructor(method: string) {
    super(
      `unix transport: ${method} not implemented. ` +
      'Add a Node.js net.Socket adapter that speaks the go-quicbaas framing protocol.',
    );
    this.name = 'UnixNotImplementedError';
  }
}

function notImpl(method: string): never {
  throw new UnixNotImplementedError(method);
}

class UnixConnectionClient implements ConnectionClient {
  private readonly _socketPath: string;
  private readonly _transport = 'unix';

  constructor(socketPath: string) {
    this._socketPath = socketPath;
  }

  async connect(_address: string): Promise<void> { notImpl('connect'); }
  async disconnect(): Promise<void> { notImpl('disconnect'); }
  isConnected(): boolean { return false; }
  getAddress(): string { return this._socketPath; }
  getTransport(): string { return this._transport; }
}

class UnixOrchestratorClient implements OrchestratorClient {
  async listJobs(_tenantID: string): Promise<JobStatus[]> { return notImpl('listJobs'); }
  async getJobStatus(_tenantID: string, _jobID: string): Promise<JobStatus> { return notImpl('getJobStatus'); }
  async startJob(_req: StartJobRequest): Promise<StartJobResponse> { return notImpl('startJob'); }
  async cancelJob(_tenantID: string, _jobID: string): Promise<void> { return notImpl('cancelJob'); }
  async listAgents(_req: ListAgentsRequest): Promise<ListAgentsResponse> { return notImpl('listAgents'); }
  async getAgentStatus(_req: GetAgentRequest): Promise<AgentRegistration> { return notImpl('getAgentStatus'); }
  async listSessions(_req: ListSessionsRequest): Promise<ListSessionsResponse> { return notImpl('listSessions'); }
  async listTasks(_tenantID: string): Promise<Task[]> { return notImpl('listTasks'); }
  async getTask(_tenantID: string, _taskID: string): Promise<Task> { return notImpl('getTask'); }
  async listBackends(): Promise<AgentBackendInfo[]> { return notImpl('listBackends'); }
  async listBackendModels(_agentID: string, _backendName: string): Promise<string[]> { return notImpl('listBackendModels'); }
  async healthCheck(): Promise<HealthCheckReport> { return notImpl('healthCheck'); }
  subscribeJobEvents(_tenantID: string, _jobID: string, _onEvent: (event: JobEvent) => void): () => void { return notImpl('subscribeJobEvents'); }
  async spawnSession(_req: SpawnSessionRequest): Promise<SpawnSessionResponse> { return notImpl('spawnSession'); }
  async promptSession(_req: PromptSessionRequest): Promise<PromptSessionResponse> { return notImpl('promptSession'); }
  async closeSession(_req: CloseSessionRequest): Promise<CloseSessionResponse> { return notImpl('closeSession'); }
  async getSessionHistory(_req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> { return notImpl('getSessionHistory'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *subscribeSession(_req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope> { return notImpl('subscribeSession'); }
  async deployAgent(_spec: DeployAgentSpec): Promise<DeployAgentResult> { throw new DeployNotSupportedError('unix'); }
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
  async decomposeTask(_req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> { return notImpl('decomposeTask'); }
  async registerAgent(_req: RegisterAgentRequest): Promise<RegisterAgentResponse> { return notImpl('registerAgent'); }
  async removeAgent(_req: RemoveAgentRequest): Promise<void> { return notImpl('removeAgent'); }
  async disableAgent(_req: import('./types.js').DisableAgentRequest): Promise<import('./types.js').DisableAgentResponse> { return notImpl('disableAgent'); }
  async enableAgent(_req: import('./types.js').EnableAgentRequest): Promise<import('./types.js').EnableAgentResponse> { return notImpl('enableAgent'); }
}

class UnixAuthClient implements AuthClient {
  async authValidate(_token?: string): Promise<boolean> { return notImpl('authValidate'); }
  async authLogin(_credentials: { username: string; password: string }): Promise<void> { return notImpl('authLogin'); }
  async webAuthnBeginLogin(_tenantID: string, _email: string): Promise<import('./types.js').CredentialAssertionJSON> { return notImpl('webAuthnBeginLogin'); }
  async webAuthnFinishLogin(_tenantID: string, _email: string, _credential: import('./types.js').PublicKeyCredentialJSON): Promise<import('./types.js').AuthTokenResponse> { return notImpl('webAuthnFinishLogin'); }
  async loginPIV(): Promise<import('./types.js').AuthTokenResponse> { return notImpl('loginPIV'); }
  async listPublicTenants(): Promise<import('./types.js').PublicTenant[]> { return notImpl('listPublicTenants'); }
  async logout(_token?: string): Promise<void> { return notImpl('logout'); }
  async registerBegin(_req: import('./interfaces.js').SelfRegisterBeginRequest): Promise<import('./interfaces.js').SelfRegisterBeginResponse> { return notImpl('registerBegin'); }
  async registerFinish(_req: import('./interfaces.js').SelfRegisterFinishRequest): Promise<import('./interfaces.js').SelfRegisterFinishResponse> { return notImpl('registerFinish'); }
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
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly projects: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly iam: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly kms: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly secrets: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly barrier: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly tenantBarrier: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly ceremony: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly bootstrap: any = null;
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
    // Non-REST transports don't manage bearer tokens via /api/v1/auth/logout.
    // The caller is responsible for clearing local credentials; this is a no-op
    // so the unified AuthClient surface compiles across every transport.
  }
}
