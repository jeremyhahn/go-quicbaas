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

// McpClient is the transport implementation for the Model Context Protocol.
//
// The projects, sdlc and accounts families are wired: they call the server's
// project_*, sdlc_* and quicbaas_agent_account_* tools through one shared
// McpSession (mcp_rpc.ts). A request argument a tool does not read is refused
// with TransportNotSupportedError rather than silently dropped. The remaining
// families still throw McpNotImplementedError.
//
// MCP endpoint convention: config.mcp.endpoint (e.g. "http://localhost:8081/mcp")

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
import type {
  Account,
  AccountsClient,
  CreateAccountRequest,
  CreateAccountResponse,
  DeleteAccountResponse,
  GetAccountResponse,
  GetAccountUsageResponse,
  ListAccountsRequest,
  ListAccountsResponse,
  RevokeAccountRequest,
  RevokeAccountResponse,
  RotateAccountCredentialRequest,
  RotateAccountCredentialResponse,
  SetDefaultAccountResponse,
  UpdateAccountRequest,
  UpdateAccountResponse,
} from './accounts.js';
import type {
  CreateProjectRequest,
  CreateProjectResponse,
  DeleteProjectResponse,
  GetProjectResponse,
  GrantFleetNodeRoleRequest,
  GrantFleetNodeRoleResponse,
  ListProjectsResponse,
  Project,
  ProjectRole,
  ProjectsClient,
  RevokeFleetNodeRoleRequest,
  RevokeFleetNodeRoleResponse,
  UpdateProjectRequest,
  UpdateProjectResponse,
} from './projects.js';
import type {
  SdlcClient,
  ProjectBinding,
  Delivery,
  OnboardingReport,
  ConformanceReport,
  OpenWork,
  ReviewerSelection,
  FileIssueRequest,
  ForgeIssue,
  CollaboratorsResponse,
  AdoptIssueRequest,
  Adoption,
} from './sdlc.js';
import { McpSession } from './mcp_rpc.js';
import {
  adoptPage,
  type ProjectPageParams,
  wholeSetPage,
  type AccountPageParams,
  type FleetNodePageParams,
  type JobPageParams,
  type PageParams,
  type PageResult,
  type SessionPageParams,
  type TaskPageParams,
} from './page.js';
import { QuicBaaSError, DeployNotSupportedError, TransportNotSupportedError } from './errors.js';


// McpNotImplementedError is thrown by all methods until the MCP protocol
// client is wired into this file.
export class McpNotImplementedError extends QuicBaaSError {
  constructor(method: string) {
    super(
      `mcp transport: ${method} not implemented. ` +
      'Wire an MCP client library and the go-quicbaas MCP tool definitions.',
    );
    this.name = 'McpNotImplementedError';
  }
}

function notImpl(method: string): never {
  throw new McpNotImplementedError(method);
}

class McpConnectionClient implements ConnectionClient {
  private readonly _endpoint: string;
  private readonly _transport = 'mcp';

  constructor(endpoint: string) {
    this._endpoint = endpoint;
  }

  async connect(_address: string): Promise<void> { notImpl('connect'); }
  async disconnect(): Promise<void> { notImpl('disconnect'); }
  isConnected(): boolean { return false; }
  getAddress(): string { return this._endpoint; }
  getTransport(): string { return this._transport; }
}

class McpOrchestratorClient implements OrchestratorClient {

  // =========================================================================
  // The paged listings
  //
  // This transport is not wired to an MCP client yet, so every listing
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

  async listJobs(_tenantID: string, _taskID?: string): Promise<JobStatus[]> { return notImpl('listJobs'); }
  async getJobStatus(_tenantID: string, _jobID: string): Promise<JobStatus> { return notImpl('getJobStatus'); }
  async startJob(_req: StartJobRequest): Promise<StartJobResponse> { return notImpl('startJob'); }
  async cancelJob(_tenantID: string, _jobID: string): Promise<void> { return notImpl('cancelJob'); }
  async listFleetNodes(_req: ListFleetNodesRequest): Promise<ListFleetNodesResponse> { return notImpl('listFleetNodes'); }
  async getFleetNode(_req: GetFleetNodeRequest): Promise<FleetNode> { return notImpl('getFleetNode'); }
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
  async getTenantSpendPolicy(_req: GetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> { return notImpl('getTenantSpendPolicy'); }
  async setTenantSpendPolicy(_req: SetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> { return notImpl('setTenantSpendPolicy'); }
  async getCostSettings(_req: GetCostSettingsRequest): Promise<CostSettings> { return notImpl('getCostSettings'); }
  async setCostSettings(_req: SetCostSettingsRequest): Promise<CostSettings> { return notImpl('setCostSettings'); }
  async getWorkerImageStatus(): Promise<WorkerImageStatus> { return notImpl('getWorkerImageStatus'); }
  async refreshWorkerImage(): Promise<WorkerImageStatus> { return notImpl('refreshWorkerImage'); }
  // eslint-disable-next-line @typescript-eslint/require-await
  async *subscribeSession(_req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope> { return notImpl('subscribeSession'); }
  async deployFleetNode(_spec: DeployFleetNodeSpec): Promise<DeployFleetNodeResult> { throw new DeployNotSupportedError('mcp'); }
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
  async listAttention(_tenantID: string, _page?: import('./page.js').AttentionPageParams): Promise<import('./page.js').PageResult<import('./types.js').AttentionItem>> { return notImpl('listAttention'); }
  async resolveAttention(_req: import('./types.js').AttentionResolveRequest): Promise<import('./types.js').AttentionItem> { return notImpl('resolveAttention'); }
  async decomposeTask(_req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> { return notImpl('decomposeTask'); }
  async registerFleetNode(_req: RegisterFleetNodeRequest): Promise<RegisterFleetNodeResponse> { return notImpl('registerFleetNode'); }
  async removeFleetNode(_req: RemoveFleetNodeRequest): Promise<void> { return notImpl('removeFleetNode'); }
  async disableFleetNode(_req: import('./types.js').DisableFleetNodeRequest): Promise<import('./types.js').DisableFleetNodeResponse> { return notImpl('disableFleetNode'); }
  async enableFleetNode(_req: import('./types.js').EnableFleetNodeRequest): Promise<import('./types.js').EnableFleetNodeResponse> { return notImpl('enableFleetNode'); }
}

class McpAuthClient implements AuthClient {
  async authValidate(_token?: string): Promise<boolean> { return notImpl('authValidate'); }
  async authLogin(_credentials: { username: string; password: string }): Promise<void> { return notImpl('authLogin'); }
  async webAuthnBeginLogin(_tenantID: string, _email: string): Promise<import('./types.js').WebAuthnLoginChallenge> { return notImpl('webAuthnBeginLogin'); }
  async webAuthnFinishLogin(_challenge: import('./types.js').WebAuthnLoginChallenge, _credential: import('./types.js').PublicKeyCredentialJSON): Promise<import('./types.js').AuthTokenResponse> { return notImpl('webAuthnFinishLogin'); }
  async loginPIV(): Promise<import('./types.js').AuthTokenResponse> { return notImpl('loginPIV'); }
  async logout(_token?: string): Promise<void> { return notImpl('logout'); }
  async registerBegin(_req: import('./interfaces.js').SelfRegisterBeginRequest): Promise<import('./interfaces.js').SelfRegisterBeginResponse> { return notImpl('registerBegin'); }
  async registerFinish(_req: import('./interfaces.js').SelfRegisterFinishRequest): Promise<import('./interfaces.js').SelfRegisterFinishResponse> { return notImpl('registerFinish'); }
}

// McpBarrierClient is a stub BarrierClient that throws McpNotImplementedError
// on every call. Real MCP tool wiring will replace this stub.
export class McpBarrierClient implements BarrierClient {
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

// McpCeremonyClient is a stub CeremonyClient that throws
// McpNotImplementedError on every call.
export class McpCeremonyClient implements CeremonyClient {
  async initialize(_req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse> { return notImpl('ceremony.initialize'); }
  async status(): Promise<CeremonyStatusResponse> { return notImpl('ceremony.status'); }
  async claimCertBegin(_req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse> { return notImpl('ceremony.claimCertBegin'); }
  async claimCertComplete(_req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse> { return notImpl('ceremony.claimCertComplete'); }
  async claimShare(_req: CeremonyClaimShareRequest): Promise<CeremonyClaimShareResponse> { return notImpl('ceremony.claimShare'); }
  async signCSR(_req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse> { return notImpl('ceremony.signCSR'); }
  async enrollExternalOfficer(_req: CeremonyEnrollExternalOfficerRequest): Promise<CeremonyEnrollExternalOfficerResponse> { return notImpl('ceremony.enrollExternalOfficer'); }
}

// McpBootstrapClient is a stub BootstrapClient that throws
// McpNotImplementedError on every call.
export class McpBootstrapClient implements BootstrapClient {
  async auto(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> { return notImpl('bootstrap.auto'); }
  async dane(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> { return notImpl('bootstrap.dane'); }
  async noise(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> { return notImpl('bootstrap.noise'); }
  async spki(_req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> { return notImpl('bootstrap.spki'); }
}

// McpClient is the stub entry point for the MCP transport.
// McpAccountsClient carries the accounts family over the quicbaas_agent_account_*
// tools (pkg/api/mcp/account_tools.go). Each tool takes the REST request's
// fields flat and answers with the same JSON the REST handler writes, so the
// requests pass through and the responses are the shared accounts types.
// mcpRefusal is the error for a request argument a tool does not read:
// dropping it would answer as if the server had applied it.
function mcpRefusal(family: string, method: string, reason: string): TransportNotSupportedError {
  return new TransportNotSupportedError('mcp', family, `${reason}; use the rest transport`, method);
}

function hasEntries(value: Record<string, string> | undefined): boolean {
  return value !== undefined && Object.keys(value).length > 0;
}

// The project tools answer the service's Go response structs, which carry no
// json tags, so their keys are the Go field names. The project inside them is
// snake_case like every other wire project; only the envelope is translated.
interface WireProjectResponse {
  Project: Project;
}

interface WireDeleteProjectResponse {
  Deleted: boolean;
}

interface WireGrantResponse {
  ProjectID: string;
  FleetNodeID: string;
  Role: ProjectRole;
  Timestamp: string;
}

interface WireRevokeResponse {
  ProjectID: string;
  FleetNodeID: string;
  Timestamp: string;
}

export class McpProjectsClient implements ProjectsClient {
  private readonly session: McpSession;

  constructor(session: McpSession) {
    this.session = session;
  }

  // createProject refuses metadata: project_create does not read it.
  async createProject(req: CreateProjectRequest): Promise<CreateProjectResponse> {
    if (hasEntries(req.metadata)) {
      throw mcpRefusal('projects', 'createProject', 'the project_create tool does not read metadata');
    }
    const resp = await this.session.callTool<WireProjectResponse>('project_create', req);
    return { project: resp.Project };
  }

  async getProject(tenantId: string, projectId: string): Promise<GetProjectResponse> {
    const resp = await this.session.callTool<WireProjectResponse>('project_get', { tenant_id: tenantId, project_id: projectId });
    return { project: resp.Project };
  }

  // updateProject refuses metadata and clear_description: project_update
  // reads neither. Any defined metadata is refused, {} included: on an update
  // {} means "clear the metadata", and dropping it would report a success that
  // changed nothing.
  async updateProject(req: UpdateProjectRequest): Promise<UpdateProjectResponse> {
    if (req.metadata !== undefined) {
      throw mcpRefusal('projects', 'updateProject', 'the project_update tool does not read metadata');
    }
    if (req.clear_description) {
      throw mcpRefusal('projects', 'updateProject', 'the project_update tool does not read clear_description');
    }
    const resp = await this.session.callTool<WireProjectResponse>('project_update', req);
    return { project: resp.Project };
  }

  async deleteProject(tenantId: string, projectId: string): Promise<DeleteProjectResponse> {
    const resp = await this.session.callTool<WireDeleteProjectResponse>('project_delete', { tenant_id: tenantId, project_id: projectId });
    return { deleted: resp.Deleted };
  }

  async listProjects(tenantId: string): Promise<ListProjectsResponse> {
    const page = await this.listProjectsPage(tenantId);
    return { projects: page.items, total: page.total };
  }

  // listProjectsPage refuses the autonomous filter: project_list takes the
  // paging arguments but no autonomous switch.
  async listProjectsPage(tenantId: string, page?: ProjectPageParams): Promise<PageResult<Project>> {
    if (page?.autonomous !== undefined) {
      throw mcpRefusal('projects', 'listProjectsPage', 'the project_list tool has no autonomous filter');
    }
    const raw = await this.session.callTool<unknown>('project_list', {
      tenant_id: tenantId,
      limit: page?.limit,
      offset: page?.offset,
      sort: page?.sort,
      q: page?.q,
    });
    return adoptPage<Project>(raw, 'projects');
  }

  async grantFleetNodeRole(req: GrantFleetNodeRoleRequest): Promise<GrantFleetNodeRoleResponse> {
    const resp = await this.session.callTool<WireGrantResponse>('project_grant_fleet_node', req);
    return { project_id: resp.ProjectID, node_id: resp.FleetNodeID, role: resp.Role, timestamp: resp.Timestamp };
  }

  async revokeFleetNodeRole(req: RevokeFleetNodeRoleRequest): Promise<RevokeFleetNodeRoleResponse> {
    const resp = await this.session.callTool<WireRevokeResponse>('project_revoke_fleet_node', req);
    return { project_id: resp.ProjectID, node_id: resp.FleetNodeID, timestamp: resp.Timestamp };
  }
}

// McpSdlcClient calls the sdlc_* tools. They answer the same service values
// the REST handlers do, so every result but forges passes through unchanged.
export class McpSdlcClient implements SdlcClient {
  private readonly session: McpSession;

  constructor(session: McpSession) {
    this.session = session;
  }

  async forges(): Promise<string[]> {
    const resp = await this.session.callTool<{ forges?: string[] }>('sdlc_forges', {});
    return resp.forges ?? [];
  }

  async bind(tenantId: string, projectId: string, binding: ProjectBinding): Promise<ProjectBinding> {
    return this.session.callTool('sdlc_bind', {
      tenant_id: tenantId,
      project_id: projectId,
      binding: { ...binding, project_id: binding.project_id || projectId },
    });
  }

  async unbind(tenantId: string, projectId: string): Promise<void> {
    await this.session.callTool('sdlc_unbind', { tenant_id: tenantId, project_id: projectId });
  }

  async binding(tenantId: string, projectId: string): Promise<ProjectBinding> {
    return this.session.callTool('sdlc_binding', { tenant_id: tenantId, project_id: projectId });
  }

  async delivery(tenantId: string, taskId: string): Promise<Delivery> {
    return this.session.callTool('sdlc_delivery', { tenant_id: tenantId, task_id: taskId });
  }

  async onboard(tenantId: string, projectId: string): Promise<OnboardingReport> {
    return this.session.callTool('sdlc_onboard', { tenant_id: tenantId, project_id: projectId });
  }

  async conformance(tenantId: string, projectId: string): Promise<ConformanceReport> {
    return this.session.callTool('sdlc_conformance', { tenant_id: tenantId, project_id: projectId });
  }

  async openWork(tenantId: string, projectId: string): Promise<OpenWork> {
    return this.session.callTool('sdlc_open_work', { tenant_id: tenantId, project_id: projectId });
  }

  async reviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection> {
    return this.session.callTool('sdlc_reviewer', { tenant_id: tenantId, project_id: projectId, pull_request: pullRequest });
  }

  async setReviewer(tenantId: string, projectId: string, pullRequest: number, profile: string): Promise<ReviewerSelection> {
    return this.session.callTool('sdlc_reviewer_set', {
      tenant_id: tenantId,
      project_id: projectId,
      pull_request: pullRequest,
      profile,
    });
  }

  async clearReviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection> {
    return this.session.callTool('sdlc_reviewer_clear', { tenant_id: tenantId, project_id: projectId, pull_request: pullRequest });
  }

  async fileIssue(tenantId: string, projectId: string, req: FileIssueRequest): Promise<ForgeIssue> {
    return this.session.callTool('sdlc_issue_create', { ...req, tenant_id: tenantId, project_id: projectId });
  }

  async collaborators(tenantId: string, projectId: string): Promise<CollaboratorsResponse> {
    return this.session.callTool('sdlc_collaborators', { tenant_id: tenantId, project_id: projectId });
  }

  async adoptIssue(tenantId: string, projectId: string, request: AdoptIssueRequest): Promise<Adoption> {
    return this.session.callTool('sdlc_adopt_issue', { ...request, tenant_id: tenantId, project_id: projectId });
  }
}

export class McpAccountsClient implements AccountsClient {
  private readonly session: McpSession;

  constructor(session: McpSession) {
    this.session = session;
  }

  async createAccount(req: CreateAccountRequest): Promise<CreateAccountResponse> {
    return this.session.callTool('quicbaas_agent_account_create', req);
  }

  async getAccount(tenantId: string, accountId: string): Promise<GetAccountResponse> {
    return this.session.callTool('quicbaas_agent_account_get', { tenant_id: tenantId, account_id: accountId });
  }

  async listAccounts(req: ListAccountsRequest): Promise<ListAccountsResponse> {
    return this.session.callTool('quicbaas_agent_account_list', req);
  }

  // listAccountsPage refuses a status filter: the list tool takes provider,
  // project_id and the paging arguments but no status, and answering the
  // unfiltered page as if it were filtered would be a lie.
  async listAccountsPage(req: AccountPageParams & { tenant_id: string }): Promise<PageResult<Account>> {
    if (req.status !== undefined && req.status !== '') {
      throw new TransportNotSupportedError(
        'mcp',
        'accounts',
        'the quicbaas_agent_account_list tool has no status filter; use the rest transport',
        'listAccountsPage',
      );
    }
    const raw = await this.session.callTool<unknown>('quicbaas_agent_account_list', req);
    return adoptPage<Account>(raw, 'accounts');
  }

  async updateAccount(req: UpdateAccountRequest): Promise<UpdateAccountResponse> {
    return this.session.callTool('quicbaas_agent_account_update', req);
  }

  async deleteAccount(tenantId: string, accountId: string): Promise<DeleteAccountResponse> {
    return this.session.callTool('quicbaas_agent_account_delete', { tenant_id: tenantId, account_id: accountId });
  }

  async rotateAccountCredential(req: RotateAccountCredentialRequest): Promise<RotateAccountCredentialResponse> {
    return this.session.callTool('quicbaas_agent_account_rotate', req);
  }

  async revokeAccount(req: RevokeAccountRequest): Promise<RevokeAccountResponse> {
    return this.session.callTool('quicbaas_agent_account_revoke', req);
  }

  async setDefaultAccount(tenantId: string, accountId: string): Promise<SetDefaultAccountResponse> {
    return this.session.callTool('quicbaas_agent_account_set_default', { tenant_id: tenantId, account_id: accountId });
  }

  async getAccountUsage(tenantId: string, accountId: string): Promise<GetAccountUsageResponse> {
    return this.session.callTool('quicbaas_agent_account_usage', { tenant_id: tenantId, account_id: accountId });
  }
}

export class McpClient implements Client {
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
  readonly projects: ProjectsClient;
  readonly sdlc: SdlcClient;
  readonly accounts: AccountsClient;
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

  constructor(endpoint: string, config: ClientConfig = {}) {
    this.connection = new McpConnectionClient(endpoint);
    const session = new McpSession(endpoint, config);
    this.projects = new McpProjectsClient(session);
    this.sdlc = new McpSdlcClient(session);
    this.accounts = new McpAccountsClient(session);
    this.orchestrator = new McpOrchestratorClient();
    this.auth = new McpAuthClient();
    this.barrier = new McpBarrierClient();
    this.ceremony = new McpCeremonyClient();
    this.bootstrap = new McpBootstrapClient();
  }
  async logout(_token?: string): Promise<void> {
    // Non-REST transports don't manage bearer tokens via the REST logout verb.
    // The caller is responsible for clearing local credentials; this is a no-op
    // so the unified AuthClient surface compiles across every transport.
  }
}
