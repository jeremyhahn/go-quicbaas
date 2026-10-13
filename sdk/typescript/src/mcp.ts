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
// The projects, sdlc, accounts and orchestrator families are wired: they call
// the server's project_*, sdlc_*, quicbaas_agent_account_*, quicbaas_fleet_*
// and quicbaas_orchestrator_* tools through one shared McpSession
// (mcp_rpc.ts). A request argument a tool does not read, or a verb the server
// has no tool for, is refused with TransportNotSupportedError rather than
// silently dropped. The remaining families still throw McpNotImplementedError.
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
  type WorkflowPageParams,
} from './page.js';
import { QuicBaaSError, DeployNotSupportedError, McpError, TransportNotSupportedError } from './errors.js';


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

// McpSessionWire is a session as the list_sessions tool answers it: the
// orchestrator's own session record, the same shape the REST route writes.
interface McpSessionWire {
  session_id: string;
  backend?: string;
  model?: string;
  /** The lifecycle state; the SDK exposes it as `status`. */
  state?: string;
  /** Unix nanoseconds. */
  created_at?: number;
  /** Unix nanoseconds; the SDK exposes it as `last_active`. */
  last_activity?: number;
  container_id?: string;
  job_id?: string;
  runner?: string;
  agent_id?: string;
}

function sessionFromMcpWire(wire: McpSessionWire): SessionStatusResponse {
  const session: SessionStatusResponse = {
    session_id: wire.session_id,
    backend: wire.backend ?? '',
    status: wire.state ?? '',
    created_at: wire.created_at ?? 0,
    last_active: wire.last_activity ?? 0,
  };
  if (wire.model !== undefined) session.model = wire.model;
  if (wire.container_id !== undefined) session.container_id = wire.container_id;
  if (wire.job_id !== undefined) session.job_id = wire.job_id;
  if (wire.runner !== undefined) session.runner = wire.runner;
  if (wire.agent_id !== undefined) session.agent_id = wire.agent_id;
  return session;
}

// addPageArgs copies the shared page parameters a caller set into a tool's
// arguments, the MCP counterpart of appendPageParams.
function addPageArgs(args: Record<string, unknown>, page?: PageParams): void {
  if (page?.limit !== undefined) args.limit = page.limit;
  if (page?.offset !== undefined) args.offset = page.offset;
  if (page?.sort !== undefined) args.sort = page.sort;
  if (page?.q !== undefined) args.q = page.q;
}

// asksForOnePage reports whether a caller of an unpaged verb named a page; one
// that did gets that page, one that did not gets the whole set.
function asksForOnePage(page: PageParams): boolean {
  return page.limit !== undefined || page.offset !== undefined;
}

// atOffset is a listing request moved to the page starting at `offset`; the
// first page sends no offset, as a caller that never paged would.
function atOffset<P extends PageParams>(page: P, offset: number): P {
  return offset === 0 ? page : { ...page, offset };
}

// walkPages fetches the page at each `next_offset` until a page names none,
// returning every record. A page that does not advance the offset, or that is
// empty, ends the walk rather than looping on a server that miscounts.
async function walkPages<T>(
  fetchPage: (offset: number) => Promise<{ items: T[]; next_offset?: number }>,
): Promise<T[]> {
  const items: T[] = [];
  let offset = 0;
  for (;;) {
    const page = await fetchPage(offset);
    items.push(...page.items);
    const next = page.next_offset;
    if (next === undefined || next <= offset || page.items.length === 0) return items;
    offset = next;
  }
}

// McpTaskCreateReply is what the task_create tool answers.
interface McpTaskCreateReply {
  task_id: string;
  subject: string;
  status: string;
  auto_spawn: boolean;
  spawned_job_id?: string;
  runner?: string;
  dispatch_reason?: string;
  // error is a task persisted but not dispatched (no matching agent).
  error?: string;
}

// unixNanos converts the RFC 3339 instant a tool answers into the Unix
// nanoseconds the REST response carries; precision below a millisecond is
// lost. An instant that does not parse is 0.
function unixNanos(rfc3339: string): number {
  const ms = Date.parse(rfc3339);
  return Number.isFinite(ms) ? ms * 1e6 : 0;
}

class McpOrchestratorClient implements OrchestratorClient {
  constructor(private readonly session: McpSession) {}

  // =========================================================================
  // The paged listings
  //
  // Each forwards its filters and the shared page parameters to the tool and
  // keeps the server's page envelope (`total`, `limit`, `offset`,
  // `next_offset`), exactly as the REST transport's page methods do, so a
  // caller sees the same page whichever transport it rides.
  // =========================================================================
  async listJobsPage(tenantID: string, page?: JobPageParams): Promise<PageResult<JobStatus>> {
    const args: Record<string, unknown> = { tenant_id: tenantID };
    if (page?.task_id !== undefined) args.task_id = page.task_id;
    if (page?.runner !== undefined) args.runner = page.runner;
    if (page?.project_id !== undefined) args.project_id = page.project_id;
    if (page?.agent_id !== undefined) args.agent_id = page.agent_id;
    if (page?.statuses !== undefined) args.statuses = page.statuses;
    if (page?.job_ids !== undefined) args.job_ids = page.job_ids;
    addPageArgs(args, page);
    const raw = await this.session.callTool<unknown>('quicbaas_orchestrator_list_jobs', args);
    return adoptPage<JobStatus>(raw, 'jobs');
  }

  async listTasksPage(tenantID: string, _page?: TaskPageParams): Promise<PageResult<Task>> {
    return wholeSetPage(await this.listTasks(tenantID));
  }

  async listSessionsPage(
    tenantID: string,
    page?: SessionPageParams,
  ): Promise<PageResult<SessionStatusResponse>> {
    return this.sessionsPage({ ...page, tenantID });
  }

  async listFleetNodesPage(
    tenantID: string,
    page?: FleetNodePageParams,
  ): Promise<PageResult<FleetNode>> {
    return this.fleetNodesPage({ ...page, tenant_id: tenantID });
  }

  async listWorkflowsPage(
    tenantID: string,
    page?: WorkflowPageParams,
  ): Promise<PageResult<WorkflowDefinition>> {
    const args: Record<string, unknown> = { tenant_id: tenantID };
    if (page?.project_id !== undefined) args.project_id = page.project_id;
    if (page?.mode !== undefined) args.mode = page.mode;
    addPageArgs(args, page);
    const raw = await this.session.callTool<unknown>('quicbaas_orchestrator_list_workflows', args);
    return adoptPage<WorkflowDefinition>(raw, 'workflows');
  }

  // =========================================================================
  // Jobs
  // =========================================================================
  async listJobs(tenantID: string, taskID?: string): Promise<JobStatus[]> {
    const filter: JobPageParams = taskID === undefined ? {} : { task_id: taskID };
    return walkPages((offset) => this.listJobsPage(tenantID, atOffset(filter, offset)));
  }

  async getJobStatus(tenantID: string, jobID: string): Promise<JobStatus> {
    return this.session.callTool<JobStatus>('quicbaas_orchestrator_get_job_status', {
      tenant_id: tenantID,
      job_id: jobID,
    });
  }

  // startJob runs under the caller's own tenant: the start_job tool takes no
  // tenant_id, so req.tenant_id is not sent.
  async startJob(req: StartJobRequest): Promise<StartJobResponse> {
    if (req.workflow_name) {
      throw mcpRefusal('orchestrator', 'startJob', 'the start_job tool takes no workflow_name');
    }
    if (req.workflow_file) {
      throw mcpRefusal('orchestrator', 'startJob', 'the start_job tool takes no workflow_file');
    }
    if (req.poll_interval) {
      throw mcpRefusal('orchestrator', 'startJob', 'the start_job tool takes no poll_interval');
    }
    const args: Record<string, unknown> = {
      description: req.description,
      project_id: req.project_id,
      max_iterations: req.max_iterations,
    };
    if (req.lead !== undefined) args.lead = req.lead;
    if (req.runner !== undefined) args.runner = req.runner;
    if (req.prompt_append !== undefined) args.prompt_append = req.prompt_append;
    const resp = await this.session.callTool<{ job_id: string; timestamp: string }>(
      'quicbaas_orchestrator_start_job',
      args,
    );
    return { job_id: resp.job_id, timestamp: unixNanos(resp.timestamp) };
  }

  async cancelJob(tenantID: string, jobID: string): Promise<void> {
    await this.session.callTool('quicbaas_orchestrator_cancel_job', {
      tenant_id: tenantID,
      job_id: jobID,
    });
  }

  subscribeJobEvents(_tenantID: string, _jobID: string, _onEvent: (event: JobEvent) => void): () => void {
    throw mcpRefusal('orchestrator', 'subscribeJobEvents', 'the server has no streaming job-event tool');
  }

  // =========================================================================
  // Fleet
  // =========================================================================
  async listFleetNodes(req: ListFleetNodesRequest): Promise<ListFleetNodesResponse> {
    const nodes = asksForOnePage(req)
      ? (await this.fleetNodesPage(req)).items
      : await walkPages((offset) => this.fleetNodesPage(atOffset(req, offset)));
    return { nodes, count: nodes.length };
  }

  private async fleetNodesPage(req: ListFleetNodesRequest): Promise<PageResult<FleetNode>> {
    if (req.model) {
      throw mcpRefusal('orchestrator', 'listFleetNodes', 'the fleet_list tool has no model filter');
    }
    const args: Record<string, unknown> = { tenant_id: req.tenant_id };
    if (req.host !== undefined) args.host = req.host;
    if (req.status !== undefined) args.status = req.status;
    if (req.node_type !== undefined) args.node_type = req.node_type;
    if (req.backend !== undefined) args.backend = req.backend;
    addPageArgs(args, req);
    const raw = await this.session.callTool<unknown>('quicbaas_fleet_list', args);
    return adoptPage<FleetNode>(raw, 'nodes');
  }

  async getFleetNode(req: GetFleetNodeRequest): Promise<FleetNode> {
    const resp = await this.session.callTool<{ node: FleetNode }>('quicbaas_fleet_status', {
      tenant_id: req.tenant_id,
      node_id: req.node_id,
    });
    return resp.node;
  }

  async registerFleetNode(req: RegisterFleetNodeRequest): Promise<RegisterFleetNodeResponse> {
    if (req.model) {
      throw mcpRefusal('orchestrator', 'registerFleetNode', 'the fleet_register tool takes no model');
    }
    if (hasEntries(req.metadata)) {
      throw mcpRefusal('orchestrator', 'registerFleetNode', 'the fleet_register tool takes no metadata');
    }
    const resp = await this.session.callTool<{ node: FleetNode }>('quicbaas_fleet_register', {
      tenant_id: req.tenant_id,
      name: req.name,
      host: req.host,
      backend: req.backend,
      node_type: req.node_type,
    });
    return { node: resp.node };
  }

  async removeFleetNode(req: RemoveFleetNodeRequest): Promise<void> {
    await this.session.callTool('quicbaas_fleet_remove', {
      tenant_id: req.tenant_id,
      node_id: req.node_id,
    });
  }

  async disableFleetNode(
    req: import('./types.js').DisableFleetNodeRequest,
  ): Promise<import('./types.js').DisableFleetNodeResponse> {
    return this.session.callTool('quicbaas_fleet_disable', {
      tenant_id: req.tenant_id,
      node_id: req.node_id,
    });
  }

  async enableFleetNode(
    req: import('./types.js').EnableFleetNodeRequest,
  ): Promise<import('./types.js').EnableFleetNodeResponse> {
    return this.session.callTool('quicbaas_fleet_enable', {
      tenant_id: req.tenant_id,
      node_id: req.node_id,
    });
  }

  async deployFleetNode(_spec: DeployFleetNodeSpec): Promise<DeployFleetNodeResult> { throw new DeployNotSupportedError('mcp'); }

  // =========================================================================
  // Backends, runners and health
  // =========================================================================
  async listBackends(): Promise<AgentBackendInfo[]> {
    return this.allPages<AgentBackendInfo>('quicbaas_orchestrator_list_backends', 'backends');
  }

  async listRunners(): Promise<JobRunnerInfo[]> {
    return this.allPages<JobRunnerInfo>('quicbaas_orchestrator_list_runners', 'runners');
  }

  async listBackendModels(_agentID: string, _backendName: string): Promise<string[]> {
    throw mcpRefusal('orchestrator', 'listBackendModels', 'the server has no tool listing a fleet node backend\'s models');
  }

  async healthCheck(): Promise<HealthCheckReport> {
    const resp = await this.session.callTool<Omit<HealthCheckReport, 'timestamp'> & { timestamp: string }>(
      'quicbaas_orchestrator_health',
      {},
    );
    return { ...resp, timestamp: unixNanos(resp.timestamp) };
  }

  // =========================================================================
  // Sessions
  // =========================================================================
  async listSessions(req: ListSessionsRequest): Promise<ListSessionsResponse> {
    const sessions = asksForOnePage(req)
      ? (await this.sessionsPage(req)).items
      : await walkPages((offset) => this.sessionsPage(atOffset(req, offset)));
    return { sessions, count: sessions.length };
  }

  private async sessionsPage(req: ListSessionsRequest): Promise<PageResult<SessionStatusResponse>> {
    const args: Record<string, unknown> = { tenant_id: req.tenantID };
    if (req.backend !== undefined) args.backend = req.backend;
    if (req.status !== undefined) args.status = req.status;
    if (req.project_id !== undefined) args.project_id = req.project_id;
    if (req.job_id !== undefined) args.job_id = req.job_id;
    if (req.runner !== undefined) args.runner = req.runner;
    if (req.agent_id !== undefined) args.agent_id = req.agent_id;
    addPageArgs(args, req);
    const raw = await this.session.callTool<unknown>('quicbaas_orchestrator_list_sessions', args);
    return adoptPage<SessionStatusResponse>(raw, 'sessions', sessionFromMcpWire);
  }

  async spawnSession(req: SpawnSessionRequest): Promise<SpawnSessionResponse> {
    if (hasEntries(req.env)) {
      throw mcpRefusal('orchestrator', 'spawnSession', 'the spawn tool takes no env');
    }
    if (req.sandboxed) {
      throw mcpRefusal('orchestrator', 'spawnSession', 'the spawn tool takes sandbox_mode, not the deprecated sandboxed');
    }
    const { tenantID, env: _env, sandboxed: _sandboxed, ...fields } = req;
    return this.session.callTool<SpawnSessionResponse>('quicbaas_orchestrator_spawn', {
      ...fields,
      tenant_id: tenantID,
    });
  }

  // promptSession answers the server's asynchronous turn acknowledgement, as
  // the REST route's 202 does; the turn's output arrives on the session's
  // event history.
  async promptSession(req: PromptSessionRequest): Promise<PromptSessionResponse> {
    return this.session.callTool<PromptSessionResponse>('quicbaas_orchestrator_prompt', {
      tenant_id: req.tenant_id ?? '',
      session_id: req.session_id,
      prompt: req.prompt,
    });
  }

  async closeSession(req: CloseSessionRequest): Promise<CloseSessionResponse> {
    const resp = await this.session.callTool<{ session_id: string }>('quicbaas_orchestrator_close', {
      tenant_id: req.tenant_id ?? '',
      session_id: req.session_id,
    });
    return { session_id: resp.session_id, closed: true };
  }

  async rotateSessionAccount(req: RotateSessionAccountRequest): Promise<RotateSessionAccountResponse> {
    const args: Record<string, unknown> = {
      tenant_id: req.tenant_id ?? '',
      session_id: req.session_id,
    };
    if (req.reason !== undefined) args.reason = req.reason;
    if (req.hold_outgoing !== undefined) args.hold_outgoing = req.hold_outgoing;
    if (req.available_at !== undefined) args.available_at = req.available_at;
    return this.session.callTool<RotateSessionAccountResponse>('quicbaas_orchestrator_rotate_account', args);
  }

  async getSessionHistory(req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> {
    const args: Record<string, unknown> = {
      tenant_id: req.tenant_id ?? '',
      session_id: req.session_id,
      from_sequence: req.from_sequence ?? 0,
    };
    if (req.limit && req.limit > 0) args.limit = req.limit;
    return this.session.callTool<GetSessionHistoryResponse>('quicbaas_orchestrator_session_history', args);
  }

  async getSessionConversation(req: GetSessionConversationRequest): Promise<GetSessionConversationResponse> {
    const args: Record<string, unknown> = {
      tenant_id: req.tenant_id ?? '',
      session_id: req.session_id,
    };
    if (req.cursor !== undefined) args.cursor = req.cursor;
    if (req.limit !== undefined) args.limit = req.limit;
    return this.session.callTool<GetSessionConversationResponse>(
      'quicbaas_orchestrator_session_conversation',
      args,
    );
  }

  // eslint-disable-next-line @typescript-eslint/require-await, require-yield
  async *subscribeSession(_req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope> {
    throw mcpRefusal('orchestrator', 'subscribeSession', 'the server has no streaming session-event tool');
  }

  // =========================================================================
  // Spend and cost settings
  // =========================================================================
  async getSpendReport(req: GetSpendReportRequest): Promise<GetSpendReportResponse> {
    const args: Record<string, unknown> = { tenant_id: req.tenant_id ?? '' };
    for (const key of ['project_id', 'task_id', 'job_id', 'session_id', 'from', 'to', 'host', 'agent'] as const) {
      if (req[key]) args[key] = req[key];
    }
    // Presence, not value: round 0 is a real level, so an unset round_set
    // must leave the argument off entirely.
    if (req.round_set) args.round = req.round ?? 0;
    return this.session.callTool<GetSpendReportResponse>('quicbaas_orchestrator_cost', args);
  }

  async getTenantSpendPolicy(req: GetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> {
    return this.session.callTool<TenantSpendPolicy>('quicbaas_orchestrator_spend_policy_get', {
      tenant_id: req.tenant_id,
    });
  }

  async setTenantSpendPolicy(req: SetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> {
    return this.session.callTool<TenantSpendPolicy>('quicbaas_orchestrator_spend_policy_set', {
      tenant_id: req.tenant_id,
      limits: req.limits,
    });
  }

  async getCostSettings(req: GetCostSettingsRequest): Promise<CostSettings> {
    return this.session.callTool<CostSettings>('quicbaas_orchestrator_cost_settings_get', {
      tenant_id: req.tenant_id ?? '',
    });
  }

  async setCostSettings(req: SetCostSettingsRequest): Promise<CostSettings> {
    const args: Record<string, unknown> = { tenant_id: req.tenant_id ?? '' };
    if (req.electricity_price_per_kwh !== undefined) {
      args.electricity_price_per_kwh = req.electricity_price_per_kwh;
    }
    if (req.engine_power_watts !== undefined) args.engine_power_watts = req.engine_power_watts;
    return this.session.callTool<CostSettings>('quicbaas_orchestrator_cost_settings_set', args);
  }

  // =========================================================================
  // Worker image
  // =========================================================================
  async getWorkerImageStatus(): Promise<WorkerImageStatus> {
    return this.session.callTool<WorkerImageStatus>('quicbaas_sdlc_worker_image', {});
  }

  async refreshWorkerImage(): Promise<WorkerImageStatus> {
    return this.session.callTool<WorkerImageStatus>('quicbaas_sdlc_worker_image_refresh', {});
  }

  // =========================================================================
  // Inference
  // =========================================================================
  async inference(req: InferenceRequest): Promise<InferenceResponse> {
    if (req.host_affinity_strict) {
      throw mcpRefusal('orchestrator', 'inference', 'the inference tool takes no host_affinity_strict');
    }
    const args: Record<string, unknown> = { backend: req.backend, prompt: req.prompt };
    if (req.model !== undefined) args.model = req.model;
    if (req.system_prompt !== undefined) args.system_prompt = req.system_prompt;
    if (req.host_affinity !== undefined) args.host_affinity = req.host_affinity;
    if (req.metadata !== undefined) args.metadata = req.metadata;
    const resp = await this.session.callTool<InferenceResponse>('quicbaas_orchestrator_inference', args);
    const out: InferenceResponse = { content: resp.content ?? [] };
    if (resp.model !== undefined) out.model = resp.model;
    if (resp.stop_reason !== undefined) out.stop_reason = resp.stop_reason;
    return out;
  }

  // eslint-disable-next-line @typescript-eslint/require-await, require-yield
  async *inferenceStream(_req: InferenceRequest, _signal?: AbortSignal): AsyncIterable<InferenceChunk> {
    throw mcpRefusal('orchestrator', 'inferenceStream', 'the server has no streaming inference tool');
  }

  // =========================================================================
  // Workflows
  // =========================================================================
  async listWorkflows(tenantID: string): Promise<ListWorkflowsResponse> {
    const workflows = await walkPages((offset) => this.listWorkflowsPage(tenantID, atOffset({}, offset)));
    return { workflows, count: workflows.length };
  }

  async createWorkflow(_req: CreateWorkflowRequest): Promise<CreateWorkflowResponse> {
    throw mcpRefusal('orchestrator', 'createWorkflow', 'the server has no workflow-create tool');
  }

  async getWorkflow(_tenantID: string, _name: string): Promise<WorkflowDefinition> {
    throw mcpRefusal('orchestrator', 'getWorkflow', 'the server has no workflow-get tool');
  }

  async updateWorkflow(_name: string, _def: WorkflowDefinition): Promise<WorkflowDefinition> {
    throw mcpRefusal('orchestrator', 'updateWorkflow', 'the server has no workflow-update tool');
  }

  async deleteWorkflow(_tenantID: string, _name: string): Promise<void> {
    throw mcpRefusal('orchestrator', 'deleteWorkflow', 'the server has no workflow-delete tool');
  }

  // =========================================================================
  // Tasks and attention
  // =========================================================================
  async listTasks(_tenantID: string): Promise<Task[]> {
    throw mcpRefusal('orchestrator', 'listTasks', 'the server has no task-list tool');
  }

  async getTask(_tenantID: string, _taskID: string): Promise<Task> {
    throw mcpRefusal('orchestrator', 'getTask', 'the server has no task-get tool');
  }

  // createTask builds the Task from the request and the fields the task_create
  // tool answers (id, status, runner, spawned job, dispatch reason). The tool
  // answers no server-stamped times or version, so those are zero.
  async createTask(req: CreateTaskRequest): Promise<CreateTaskResponse> {
    const tool = 'quicbaas_orchestrator_task_create';
    if (req.host_affinity_strict) {
      throw mcpRefusal('orchestrator', 'createTask', 'the task_create tool takes no host_affinity_strict');
    }
    const resp = await this.session.callTool<McpTaskCreateReply>(tool, { ...req });
    // The tool reports a task it persisted but could not dispatch as a soft
    // error; the REST route answers that as a failure, and so does this.
    if (resp.error) throw new McpError(tool, resp.error);
    const task: Task = {
      id: resp.task_id,
      tenant_id: req.tenant_id,
      team_name: req.team_name,
      subject: resp.subject,
      description: req.description ?? '',
      status: resp.status as Task['status'],
      assigned_to: '',
      created_by: '',
      dependencies: [],
      blocks: [],
      result: null,
      metadata: req.metadata ?? {},
      created_at: 0,
      updated_at: 0,
      claimed_at: 0,
      completed_at: 0,
      version: 0,
      auto_spawn: resp.auto_spawn,
    };
    if (resp.spawned_job_id) task.spawned_job_id = resp.spawned_job_id;
    if (resp.runner) task.runner = resp.runner;
    if (resp.dispatch_reason) task.dispatch_reason = resp.dispatch_reason;
    if (req.prompt_append) task.prompt_append = req.prompt_append;
    return { task };
  }

  async claimTask(_req: ClaimTaskRequest): Promise<void> {
    throw mcpRefusal('orchestrator', 'claimTask', 'the server has no task-claim tool');
  }

  async completeTask(_req: CompleteTaskRequest): Promise<void> {
    throw mcpRefusal('orchestrator', 'completeTask', 'the server has no task-complete tool');
  }

  async readmitTask(tenantID: string, taskID: string): Promise<void> {
    await this.session.callTool('quicbaas_orchestrator_task_readmit', {
      tenant_id: tenantID,
      task_id: taskID,
    });
  }

  async deleteTask(
    req: import('./types.js').DeleteTaskRequest,
  ): Promise<import('./types.js').DeleteTaskResponse> {
    const resp = await this.session.callTool<{ task_id: string; deleted: boolean }>(
      'quicbaas_orchestrator_task_delete',
      { tenant_id: req.tenant_id, task_id: req.task_id },
    );
    return { task_id: resp.task_id, deleted: resp.deleted };
  }

  // decomposeTask is refused: quicbaas_orchestrator_decompose splits free
  // text into work items, which is not decomposing a stored task.
  async decomposeTask(_req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> {
    throw mcpRefusal('orchestrator', 'decomposeTask', 'the server has no tool that decomposes a stored task');
  }

  async listAttention(
    tenantID: string,
    page?: import('./page.js').AttentionPageParams,
  ): Promise<import('./page.js').PageResult<import('./types.js').AttentionItem>> {
    const args: Record<string, unknown> = { tenant_id: tenantID };
    if (page?.project_id !== undefined) args.project_id = page.project_id;
    if (page?.kind !== undefined) args.kind = page.kind;
    if (page?.limit !== undefined) args.limit = page.limit;
    if (page?.offset !== undefined) args.offset = page.offset;
    if (page?.sort !== undefined) args.sort = page.sort;
    if (page?.q !== undefined) args.q = page.q;
    const raw = await this.session.callTool<unknown>('quicbaas_orchestrator_attention_list', args);
    return adoptPage<import('./types.js').AttentionItem>(raw, 'items');
  }

  async resolveAttention(
    req: import('./types.js').AttentionResolveRequest,
  ): Promise<import('./types.js').AttentionItem> {
    const args: Record<string, unknown> = {
      tenant_id: req.tenant_id,
      task_id: req.task_id,
      action: req.action,
    };
    if (req.answer !== undefined) args.answer = req.answer;
    return this.session.callTool('quicbaas_orchestrator_attention_resolve', args);
  }

  // allPages walks a paged listing tool to its end, as the REST transport's
  // unpaged listings do, so the unpaged verbs answer the whole set.
  private async allPages<T>(tool: string, key: string): Promise<T[]> {
    return walkPages<T>(async (offset) => {
      const resp = await this.session.callTool<Record<string, unknown>>(tool, { offset });
      return {
        items: (resp[key] as T[] | null | undefined) ?? [],
        next_offset: resp.next_offset as number | undefined,
      };
    });
  }
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
    this.orchestrator = new McpOrchestratorClient(session);
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
