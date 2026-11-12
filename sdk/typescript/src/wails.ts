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

import {
  adoptPage,
  type FleetNodePageParams,
  type AttentionPageParams,
  type JobPageParams,
  type PageParams,
  type PageResult,
  type SessionPageParams,
  type TaskPageParams,
  type WorkflowPageParams,
} from './page.js';
import type {
  AuthClient,
  Client,
  ConnectionClient,
  OrchestratorClient,
  SSHKeysClient,
  SSHClient,
  SSHKnownHostsClient,
  SSHAuthorizedKeysClient,
} from './interfaces.js';
import type {
  AgentBackendInfo,
  JobRunnerInfo,
  DeployFleetNodeResult,
  DeployFleetNodeSpec,
  InferenceRequest,
  InferenceResponse,
  InferenceChunk,
  FleetNode,
  CancelJobRequest,
  CloseSessionRequest,
  CloseSessionResponse,
  RotateSessionAccountRequest,
  RotateSessionAccountResponse,
  CreateTaskRequest,
  CreateTaskResponse,
  ClaimTaskRequest,
  CompleteTaskRequest,
  DeleteTaskRequest,
  DeleteTaskResponse,
  CreateWorkflowRequest,
  CreateWorkflowResponse,
  DecomposeTaskRequest,
  DecomposeTaskResponse,
  GetFleetNodeRequest,
  GetJobStatusRequest,
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
  GetTaskRequest,
  HealthCheckReport,
  JobEvent,
  JobStatus,
  ListFleetNodesRequest,
  ListFleetNodesResponse,
  ListJobsRequest,
  ListJobsResponse,
  ListSessionsRequest,
  ListSessionsResponse,
  SessionStatusResponse,
  ListTasksRequest,
  ListTasksResponse,
  ListWorkflowsResponse,
  AttentionItem,
  AttentionResolveRequest,
  PromptSessionRequest,
  PromptSessionResponse,
  RegisterFleetNodeRequest,
  RegisterFleetNodeResponse,
  RemoveFleetNodeRequest,
  SessionEventEnvelope,
  SpawnSessionRequest,
  SpawnSessionResponse,
  SSHKey,
  SSHScope,
  SSHPublicKey,
  SSHPin,
  SSHAuthorizedEntry,
  ListSSHKeysResponse,
  ListSSHKeysQuery,
  ListSSHPinsResponse,
  AddSSHPinRequest,
  VerifySSHPinRequest,
  EmitAuthorizedRequest,
  EmitAuthorizedResponse,
  VerifyAuthorizedRequest,
  VerifyAuthorizedResponse,
  AddAuthorizedKeyRequest,
  ListAuthorizedKeysRequest,
  CreateSSHKeyRequest,
  ImportSSHKeyRequest,
  SignRequest,
  SignResponse,
  StartJobRequest,
  StartJobResponse,
  SubscribeJobEventsRequest,
  SubscribeSessionEventsRequest,
  Task,
  WorkflowDefinition,
} from './types.js';
import { APIError, ConnectionError } from './errors.js';
import type { AccountsClient } from './accounts.js';
import type { ProjectsClient } from './projects.js';
import type { SdlcClient } from './sdlc.js';
import { unsupportedClient } from './unsupported.js';

// wailsUnsupported is why projects, sdlc and accounts refuse here: the
// desktop app binds no Go struct for them (cmd/quicbaas/bindings_*.go binds
// orchestrator, sessions, fleet, SSH, agents and cost settings only), so there
// is no window.go method to call. The desktop SPA reaches them over REST
// against the embedded server.
const wailsUnsupported =
  'the desktop app exposes no Wails binding for this family; ' +
  'use the rest transport against the embedded server';

// WailsBinding is the shape of a Wails-generated Go struct binding.
// Wails exposes each exported Go method as a property whose value is a
// function that returns a Promise. The exact method names are determined
// at Go compile time, so we use an index signature here.
type WailsBinding = {
  [method: string]: (...args: unknown[]) => Promise<unknown>;
};

// wailsGo returns the window.go namespace cast to a two-level map.
// Wails generates bindings at compile time; we keep the index signature
// here and cast return values at each call site.
function wailsGo(): Record<string, Record<string, WailsBinding[string]>> {
  if (typeof window === 'undefined' || !window.go) {
    throw new ConnectionError('Wails runtime not available');
  }
  return window.go as Record<string, Record<string, WailsBinding[string]>>;
}

// WailsErrorPayload is the value a failed binding call rejects with: the Go
// ErrorFormatter (formatGUIError in cmd/quicbaas/errors_gui.go) sends the
// classified error code, its HTTP status and the message.
interface WailsErrorPayload {
  code: string;
  status: number;
  message: string;
}

function isWailsErrorPayload(reason: unknown): reason is WailsErrorPayload {
  if (typeof reason !== 'object' || reason === null) return false;
  const payload = reason as Partial<WailsErrorPayload>;
  return typeof payload.status === 'number' && typeof payload.message === 'string';
}

// wailsError converts a binding rejection into the error the REST client
// throws for the same failure: a structured payload becomes an APIError
// carrying its status, so callers classify a denial the same way on both
// transports. Any other rejection is passed through unchanged.
export function wailsError(reason: unknown): unknown {
  if (isWailsErrorPayload(reason)) {
    return new APIError(reason.status, reason.message);
  }
  return reason;
}

// normalizedBinding wraps every method of a Wails binding so its rejection
// goes through wailsError.
function normalizedBinding(binding: WailsBinding): WailsBinding {
  return new Proxy(binding, {
    get(target, prop, receiver) {
      const value: unknown = Reflect.get(target, prop, receiver);
      if (typeof value !== 'function') return value;
      return (...args: unknown[]) =>
        Promise.resolve(value.apply(target, args)).catch((reason: unknown) => {
          throw wailsError(reason);
        });
    },
  });
}

// connectionBinding returns the Wails-generated ConnectionBinding proxy.
function connectionBinding(): WailsBinding {
  return normalizedBinding(wailsGo().main.ConnectionBinding as unknown as WailsBinding);
}

// orchestratorBinding returns the Wails-generated OrchestratorBinding proxy.
function orchestratorBinding(): WailsBinding {
  return normalizedBinding(wailsGo().main.OrchestratorBinding as unknown as WailsBinding);
}

// sessionBinding returns the Wails-generated SessionBinding proxy.
// Phase 6 Go side: window.go.main.SessionBinding.
function sessionBinding(): WailsBinding {
  return normalizedBinding(wailsGo().main.SessionBinding as unknown as WailsBinding);
}

// fleetDeployBinding returns the Wails-generated FleetDeployBinding proxy.
// Wails exposes the Go struct at window.go.main.FleetDeployBinding.
function fleetDeployBinding(): WailsBinding {
  return normalizedBinding(wailsGo().main.FleetDeployBinding as unknown as WailsBinding);
}

// sshKeysBinding returns the Wails-generated SSHKeysBinding proxy. The
// Go struct lives at window.go.main.SSHKeysBinding and mirrors the
// sshkey.Client interface.
function sshKeysBinding(): WailsBinding {
  return normalizedBinding(wailsGo().main.SSHKeysBinding as unknown as WailsBinding);
}

// WailsSSHKeysClient implements SSHKeysClient over the Wails IPC. Each
// call forwards to the Go binding, which dispatches through the shared
// SDK client owned by the ConnectionBinding.
//
// The Go binding (cmd/quicbaas/bindings_ssh.go) only exposes the legacy
// flat surface: List, Create, Get, Delete with server-side scope/owner
// defaults. The full scope/owner/name methods are available via REST only
// (use RestClient.ssh.keys for those).
class WailsSSHKeysClient implements SSHKeysClient {
  async list(): Promise<ListSSHKeysResponse> {
    return sshKeysBinding().List() as Promise<ListSSHKeysResponse>;
  }
  async create(req: CreateSSHKeyRequest): Promise<SSHKey> {
    return sshKeysBinding().Create(req) as Promise<SSHKey>;
  }
  async get(name: string): Promise<SSHKey> {
    return sshKeysBinding().Get(name) as Promise<SSHKey>;
  }
  async remove(name: string): Promise<void> {
    await sshKeysBinding().Delete(name);
  }

  // ---- Scoped methods not available via Wails binding ----
  // Use the REST transport (RestClient.ssh.keys) for these operations.

  async listScoped(_query?: ListSSHKeysQuery): Promise<ListSSHKeysResponse> {
    throw new Error('wails: listScoped not available — use REST transport');
  }
  async getScoped(_scope: SSHScope, _owner: string, _name: string): Promise<SSHKey> {
    throw new Error('wails: getScoped not available — use REST transport');
  }
  async deleteScoped(_scope: SSHScope, _owner: string, _name: string): Promise<void> {
    throw new Error('wails: deleteScoped not available — use REST transport');
  }
  async exportPublic(_scope: SSHScope, _owner: string, _name: string): Promise<SSHPublicKey> {
    throw new Error('wails: exportPublic not available — use REST transport');
  }
  async sign(_scope: SSHScope, _owner: string, _name: string, _req: SignRequest): Promise<SignResponse> {
    throw new Error('wails: sign not available — use REST transport');
  }
  async import_(_req: ImportSSHKeyRequest): Promise<SSHKey> {
    throw new Error('wails: import_ not available — use REST transport');
  }
}

// WailsKnownHostsClient is a stub that surfaces the interface contract for
// Wails mode. Known-hosts operations are not exposed via the Go Wails binding;
// use the REST transport (RestClient.ssh.knownHosts) for production use.
class WailsKnownHostsClient implements SSHKnownHostsClient {
  async add(_req: AddSSHPinRequest): Promise<void> {
    throw new Error('wails: ssh.knownHosts not available — use REST transport');
  }
  async list(_query?: ListSSHKeysQuery): Promise<ListSSHPinsResponse> {
    throw new Error('wails: ssh.knownHosts not available — use REST transport');
  }
  async remove(_host: string, _query?: ListSSHKeysQuery): Promise<void> {
    throw new Error('wails: ssh.knownHosts not available — use REST transport');
  }
  async verify(_host: string, _req: VerifySSHPinRequest): Promise<void> {
    throw new Error('wails: ssh.knownHosts not available — use REST transport');
  }
}

// WailsAuthorizedKeysClient is a stub that surfaces the interface contract for
// Wails mode. Authorized-keys operations are not exposed via the Go Wails
// binding; use the REST transport (RestClient.ssh.authorized) for production use.
class WailsAuthorizedKeysClient implements SSHAuthorizedKeysClient {
  async emit(_req: EmitAuthorizedRequest): Promise<EmitAuthorizedResponse> {
    throw new Error('wails: ssh.authorized not available — use REST transport');
  }
  async verify(_req: VerifyAuthorizedRequest): Promise<VerifyAuthorizedResponse> {
    throw new Error('wails: ssh.authorized not available — use REST transport');
  }
  async add(_req: AddAuthorizedKeyRequest): Promise<void> {
    throw new Error('wails: ssh.authorized not available — use REST transport');
  }
  async list(_req?: ListAuthorizedKeysRequest): Promise<SSHAuthorizedEntry[]> {
    throw new Error('wails: ssh.authorized not available — use REST transport');
  }
  async get(_scope: SSHScope, _owner: string, _name: string): Promise<SSHAuthorizedEntry> {
    throw new Error('wails: ssh.authorized not available — use REST transport');
  }
  async remove(_scope: SSHScope, _owner: string, _name: string): Promise<void> {
    throw new Error('wails: ssh.authorized not available — use REST transport');
  }
}

// WailsSSHClient groups the three Wails SSH sub-client stubs.
class WailsSSHClient implements SSHClient {
  readonly keys: WailsSSHKeysClient;
  readonly knownHosts: WailsKnownHostsClient;
  readonly authorized: WailsAuthorizedKeysClient;

  constructor(keys: WailsSSHKeysClient) {
    this.keys = keys;
    this.knownHosts = new WailsKnownHostsClient();
    this.authorized = new WailsAuthorizedKeysClient();
  }
}

// WailsConnectionClient implements ConnectionClient over Wails IPC.
class WailsConnectionClient implements ConnectionClient {
  private _connected = false;
  private _address = '';
  private _transport = 'wails';

  async connect(address: string, _transport?: string, _tenantID?: string): Promise<void> {
    await connectionBinding().Connect(address);
    this._address = address;
    this._connected = true;
  }

  async disconnect(): Promise<void> {
    await connectionBinding().Disconnect();
    this._connected = false;
  }

  isConnected(): boolean {
    return this._connected;
  }

  getAddress(): string {
    return this._address;
  }

  getTransport(): string {
    return this._transport;
  }
}

// pageFields renders the shared paging parameters as the members a Go SDK
// request struct carries (page.Params: limit/offset/sort/q). An absent field
// is omitted so the request says only what the caller asked for.
function pageFields(page?: PageParams): Record<string, string | number> {
  const fields: Record<string, string | number> = {};
  if (page === undefined) return fields;
  if (typeof page.limit === 'number' && page.limit > 0) fields['limit'] = page.limit;
  if (typeof page.offset === 'number' && page.offset > 0) fields['offset'] = page.offset;
  if (page.sort !== undefined && page.sort !== '') fields['sort'] = page.sort;
  if (page.q !== undefined && page.q !== '') fields['q'] = page.q;
  return fields;
}

// WailsOrchestratorClient implements OrchestratorClient over Wails IPC.
//
// Every Wails Go binding accepts the corresponding SDK request struct
// verbatim — Wails serialises the TS object as JSON and Go unmarshals it
// into the typed request. Methods on the OrchestratorClient interface that
// expose flat argument lists (e.g. `listJobs(tenantID)`) construct the
// request object here so the interface contract is unchanged while the
// wire shape matches what Go expects.
class WailsOrchestratorClient implements OrchestratorClient {
  // =========================================================================
  // The paged listings
  //
  // THE PARAMETERS GO OVER THE BINDING; THE ENVELOPE DOES NOT COME BACK. A
  // Wails binding takes the SDK request struct verbatim, and those structs
  // embed the paging parameters, so limit/offset/sort/q reach the daemon and
  // the SERVER does the paging, ordering and searching. What the bindings
  // return, though, is a projected ARRAY — the envelope's `total` is dropped
  // on the Go side (cmd/quicbaas/bindings_orchestrator.go) — so an answer
  // without one is reported as a single whole page rather than as a first
  // page of an unknown number.
  //
  // The consequence, written down rather than discovered: on the desktop a
  // table shows one page and no pager until those bindings return the
  // envelope. adoptPage already handles both shapes, so the day they do,
  // these methods page for real without another edit here.
  // =========================================================================
  async listJobsPage(tenantID: string, page?: JobPageParams): Promise<PageResult<JobStatus>> {
    const req: ListJobsRequest = { tenant_id: tenantID, ...pageFields(page) };
    if (page?.task_id !== undefined) req.task_id = page.task_id;
    if (page?.runner !== undefined) req.runner = page.runner;
    if (page?.project_id !== undefined) req.project_id = page.project_id;
    if (page?.statuses !== undefined) req.statuses = page.statuses;
    if (page?.job_ids !== undefined) req.job_ids = page.job_ids;
    if (page?.agent_id !== undefined) req.agent_id = page.agent_id;
    return adoptPage<JobStatus>(await orchestratorBinding().ListJobs(req), 'jobs');
  }

  async listTasksPage(tenantID: string, page?: TaskPageParams): Promise<PageResult<Task>> {
    const req: ListTasksRequest = { tenant_id: tenantID, ...pageFields(page) };
    if (page?.status !== undefined) req.status = page.status;
    if (page?.statuses !== undefined) req.statuses = page.statuses;
    if (page?.project_id !== undefined) req.project_id = page.project_id;
    if (page?.team_name !== undefined) req.team_name = page.team_name;
    if (page?.assigned_to !== undefined) req.assigned_to = page.assigned_to;
    if (page?.created_by !== undefined) req.created_by = page.created_by;
    if (page?.auto_spawn !== undefined) req.auto_spawn = page.auto_spawn;
    if (page?.agent_id !== undefined) req.agent_id = page.agent_id;
    if (page?.job_id !== undefined) req.job_id = page.job_id;
    if (page?.source_kind !== undefined) req.source_kind = page.source_kind;
    if (page?.source_ref !== undefined) req.source_ref = page.source_ref;
    return adoptPage<Task>(await orchestratorBinding().ListTasks(req), 'tasks');
  }

  async listSessionsPage(
    tenantID: string,
    page?: SessionPageParams,
  ): Promise<PageResult<SessionStatusResponse>> {
    if (!tenantID) {
      throw new Error('listSessionsPage: tenantID is required');
    }
    const req: ListSessionsRequest = { tenantID, ...pageFields(page) };
    if (page?.backend !== undefined) req.backend = page.backend;
    if (page?.status !== undefined) req.status = page.status;
    if (page?.project_id !== undefined) req.project_id = page.project_id;
    if (page?.job_id !== undefined) req.job_id = page.job_id;
    if (page?.runner !== undefined) req.runner = page.runner;
    if (page?.agent_id !== undefined) req.agent_id = page.agent_id;
    return adoptPage<SessionStatusResponse>(
      await orchestratorBinding().ListSessions(req),
      'sessions',
    );
  }

  async listFleetNodesPage(
    tenantID: string,
    page?: FleetNodePageParams,
  ): Promise<PageResult<FleetNode>> {
    const req: ListFleetNodesRequest = { tenant_id: tenantID, ...pageFields(page) };
    if (page?.host !== undefined) req.host = page.host;
    if (page?.status !== undefined) req.status = page.status;
    if (page?.node_type !== undefined) req.node_type = page.node_type;
    if (page?.backend !== undefined) req.backend = page.backend;
    if (page?.model !== undefined) req.model = page.model;
    return adoptPage<FleetNode>(await orchestratorBinding().ListFleetNodes(req), 'nodes');
  }

  async listWorkflowsPage(
    tenantID: string,
    page?: WorkflowPageParams,
  ): Promise<PageResult<WorkflowDefinition>> {
    const b = orchestratorBinding();
    if (typeof b.ListWorkflows !== 'function') {
      throw new Error('wails: ListWorkflows not implemented');
    }
    const workflowFilters: Record<string, string> = {};
    if (page?.project_id !== undefined) workflowFilters['project_id'] = page.project_id;
    if (page?.mode !== undefined) workflowFilters['mode'] = page.mode;
    const answered = await b.ListWorkflows({
      tenant_id: tenantID,
      ...workflowFilters,
      ...pageFields(page),
    });
    return adoptPage<WorkflowDefinition>(answered, 'workflows');
  }

  async listJobs(tenantID: string, taskID?: string): Promise<JobStatus[]> {
    const req: ListJobsRequest = { tenant_id: tenantID, task_id: taskID };
    const resp = (await orchestratorBinding().ListJobs(req)) as ListJobsResponse | JobStatus[];
    // The Go binding returns the projected job array; older shims may
    // have returned a ListJobsResponse. Normalise to JobStatus[].
    if (Array.isArray(resp)) return resp;
    return resp.jobs ?? [];
  }

  async getJobStatus(tenantID: string, jobID: string): Promise<JobStatus> {
    const req: GetJobStatusRequest = { tenant_id: tenantID, job_id: jobID };
    return orchestratorBinding().GetJobStatus(req) as Promise<JobStatus>;
  }

  async startJob(req: StartJobRequest): Promise<StartJobResponse> {
    return orchestratorBinding().StartJob(req) as Promise<StartJobResponse>;
  }

  async cancelJob(tenantID: string, jobID: string): Promise<void> {
    const req: CancelJobRequest = { tenant_id: tenantID, job_id: jobID };
    await orchestratorBinding().CancelJob(req);
  }

  async listFleetNodes(req: ListFleetNodesRequest): Promise<ListFleetNodesResponse> {
    return orchestratorBinding().ListFleetNodes(req) as Promise<ListFleetNodesResponse>;
  }

  async getFleetNode(req: GetFleetNodeRequest): Promise<FleetNode> {
    return orchestratorBinding().GetFleetNode(req) as Promise<FleetNode>;
  }

  async listSessions(req: ListSessionsRequest): Promise<ListSessionsResponse> {
    if (!req.tenantID) {
      throw new Error('listSessions: tenantID is required');
    }
    return orchestratorBinding().ListSessions(req) as Promise<ListSessionsResponse>;
  }

  async listTasks(tenantID: string): Promise<Task[]> {
    const req: ListTasksRequest = { tenant_id: tenantID };
    const resp = (await orchestratorBinding().ListTasks(req)) as ListTasksResponse | Task[];
    if (Array.isArray(resp)) return resp;
    return resp.tasks ?? [];
  }

  async getTask(tenantID: string, taskID: string): Promise<Task> {
    const req: GetTaskRequest = { tenant_id: tenantID, task_id: taskID };
    return orchestratorBinding().GetTask(req) as Promise<Task>;
  }

  async listBackends(): Promise<AgentBackendInfo[]> {
    return orchestratorBinding().ListBackends() as Promise<AgentBackendInfo[]>;
  }

  async listRunners(): Promise<JobRunnerInfo[]> {
    return orchestratorBinding().ListRunners() as Promise<JobRunnerInfo[]>;
  }

  async listBackendModels(nodeID: string, backendName: string): Promise<string[]> {
    return orchestratorBinding().ListBackendModels({ node_id: nodeID, backend_name: backendName }) as Promise<string[]>;
  }

  async healthCheck(): Promise<HealthCheckReport> {
    return orchestratorBinding().HealthCheck() as Promise<HealthCheckReport>;
  }

  // subscribeJobEvents tells the Go binding to start emitting job events and
  // listens for them on the Wails runtime event bus. Returns a cleanup function
  // that unregisters the listener and tells Go to stop streaming.
  subscribeJobEvents(
    tenantID: string,
    jobID: string,
    onEvent: (event: JobEvent) => void,
  ): () => void {
    const req: SubscribeJobEventsRequest = { tenant_id: tenantID, job_id: jobID };
    void orchestratorBinding().SubscribeJobEvents(req);

    const eventName = `job-event:${jobID}`;

    if (!window.runtime) {
      return () => { /* no-op */ };
    }

    const cleanup = window.runtime.EventsOn(eventName, (...args: unknown[]) => {
      if (args.length > 0) {
        onEvent(args[0] as JobEvent);
      }
    });

    return () => {
      if (typeof cleanup === 'function') {
        cleanup();
      }
      const binding = orchestratorBinding();
      if (typeof binding.UnsubscribeJobEvents === 'function') {
        void binding.UnsubscribeJobEvents(req);
      }
    };
  }

  // spawnSession creates a new agent session via the Wails SessionBinding.
  async spawnSession(req: SpawnSessionRequest): Promise<SpawnSessionResponse> {
    return sessionBinding().Spawn(req) as Promise<SpawnSessionResponse>;
  }

  // promptSession sends a prompt to an existing session via the Wails SessionBinding.
  async promptSession(req: PromptSessionRequest): Promise<PromptSessionResponse> {
    return sessionBinding().Prompt(req) as Promise<PromptSessionResponse>;
  }

  // closeSession terminates a session via the Wails SessionBinding.
  async closeSession(req: CloseSessionRequest): Promise<CloseSessionResponse> {
    return sessionBinding().Close(req) as Promise<CloseSessionResponse>;
  }

  // rotateSessionAccount is not yet exposed by the Wails SessionBinding.
  // Use the REST transport for account rotation in desktop mode until the
  // Go-side binding grows a RotateAccount method.
  async rotateSessionAccount(_req: RotateSessionAccountRequest): Promise<RotateSessionAccountResponse> {
    throw new Error('rotateSessionAccount: use REST transport for account rotation in desktop mode');
  }

  // deployFleetNode forwards the spec to the Wails FleetDeployBinding.Deploy
  // method. The Go-side binding orchestrates the SSH+SFTP+systemd workflow
  // and returns a structured result.
  async deployFleetNode(spec: DeployFleetNodeSpec): Promise<DeployFleetNodeResult> {
    return fleetDeployBinding().Deploy(spec) as Promise<DeployFleetNodeResult>;
  }

  // getSessionHistory fetches a page of historical session events.
  // The Go binding forwards the request struct verbatim to the SDK.
  async getSessionHistory(req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> {
    return sessionBinding().GetHistory(req) as Promise<GetSessionHistoryResponse>;
  }

  // getSessionConversation fetches a page of a session's conversation.
  // The Go binding forwards the request struct verbatim to the SDK.
  async getSessionConversation(req: GetSessionConversationRequest): Promise<GetSessionConversationResponse> {
    return sessionBinding().GetConversation(req) as Promise<GetSessionConversationResponse>;
  }

  // getSpendReport delegates to OrchestratorBinding.GetSpendReport. The Go
  // binding forwards the request struct verbatim and returns the SDK type
  // unreshaped, so priced_entries — what says whether equivalent_micros is a
  // measured figure — survives the IPC hop.
  async getSpendReport(req: GetSpendReportRequest): Promise<GetSpendReportResponse> {
    return orchestratorBinding().GetSpendReport(req) as Promise<GetSpendReportResponse>;
  }

  // getTenantSpendPolicy / setTenantSpendPolicy delegate to the binding verbs
  // of the same name, which forward the request struct verbatim.
  async getTenantSpendPolicy(req: GetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> {
    return orchestratorBinding().GetTenantSpendPolicy(req) as Promise<TenantSpendPolicy>;
  }

  async setTenantSpendPolicy(req: SetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> {
    return orchestratorBinding().SetTenantSpendPolicy(req) as Promise<TenantSpendPolicy>;
  }

  // getCostSettings / setCostSettings delegate to the binding verbs of the
  // same names, which pass the SDK types straight through.
  async getCostSettings(req: GetCostSettingsRequest): Promise<CostSettings> {
    return orchestratorBinding().GetCostSettings(req) as Promise<CostSettings>;
  }

  async setCostSettings(req: SetCostSettingsRequest): Promise<CostSettings> {
    return orchestratorBinding().SetCostSettings(req) as Promise<CostSettings>;
  }

  // getWorkerImageStatus delegates to OrchestratorBinding.GetWorkerImageStatus,
  // which reaches the Go SDK's SDLC client and returns its type unreshaped.
  async getWorkerImageStatus(): Promise<WorkerImageStatus> {
    return orchestratorBinding().GetWorkerImageStatus() as Promise<WorkerImageStatus>;
  }

  // refreshWorkerImage delegates to OrchestratorBinding.RefreshWorkerImage.
  async refreshWorkerImage(): Promise<WorkerImageStatus> {
    return orchestratorBinding().RefreshWorkerImage() as Promise<WorkerImageStatus>;
  }

  // createWorkflow delegates to OrchestratorBinding.CreateWorkflow if available.
  async createWorkflow(req: CreateWorkflowRequest): Promise<CreateWorkflowResponse> {
    const b = orchestratorBinding();
    if (typeof b.CreateWorkflow !== 'function') {
      throw new Error('wails: CreateWorkflow not implemented');
    }
    return b.CreateWorkflow(req) as Promise<CreateWorkflowResponse>;
  }

  // listWorkflows delegates to OrchestratorBinding.ListWorkflows if available.
  async listWorkflows(tenantID: string): Promise<ListWorkflowsResponse> {
    const b = orchestratorBinding();
    if (typeof b.ListWorkflows !== 'function') {
      throw new Error('wails: ListWorkflows not implemented');
    }
    return b.ListWorkflows({ tenant_id: tenantID }) as Promise<ListWorkflowsResponse>;
  }

  // getWorkflow delegates to OrchestratorBinding.GetWorkflow if available.
  async getWorkflow(tenantID: string, name: string): Promise<WorkflowDefinition> {
    const b = orchestratorBinding();
    if (typeof b.GetWorkflow !== 'function') {
      throw new Error('wails: GetWorkflow not implemented');
    }
    return b.GetWorkflow({ tenant_id: tenantID, name }) as Promise<WorkflowDefinition>;
  }

  // updateWorkflow delegates to OrchestratorBinding.UpdateWorkflow if available.
  async updateWorkflow(name: string, def: WorkflowDefinition): Promise<WorkflowDefinition> {
    const b = orchestratorBinding();
    if (typeof b.UpdateWorkflow !== 'function') {
      throw new Error('wails: UpdateWorkflow not implemented');
    }
    return b.UpdateWorkflow({ name, definition: def }) as Promise<WorkflowDefinition>;
  }

  // deleteWorkflow delegates to OrchestratorBinding.DeleteWorkflow if available.
  async deleteWorkflow(tenantID: string, name: string): Promise<void> {
    const b = orchestratorBinding();
    if (typeof b.DeleteWorkflow !== 'function') {
      throw new Error('wails: DeleteWorkflow not implemented');
    }
    await b.DeleteWorkflow({ tenant_id: tenantID, name });
  }

  // createTask delegates to OrchestratorBinding.CreateTask.
  async createTask(req: CreateTaskRequest): Promise<CreateTaskResponse> {
    const b = orchestratorBinding();
    if (typeof b.CreateTask !== 'function') {
      throw new Error('wails: CreateTask not implemented');
    }
    return b.CreateTask(req) as Promise<CreateTaskResponse>;
  }

  // claimTask delegates to OrchestratorBinding.ClaimTask.
  async claimTask(req: ClaimTaskRequest): Promise<void> {
    const b = orchestratorBinding();
    if (typeof b.ClaimTask !== 'function') {
      throw new Error('wails: ClaimTask not implemented');
    }
    await b.ClaimTask(req);
  }

  // completeTask delegates to OrchestratorBinding.CompleteTask.
  async completeTask(req: CompleteTaskRequest): Promise<void> {
    const b = orchestratorBinding();
    if (typeof b.CompleteTask !== 'function') {
      throw new Error('wails: CompleteTask not implemented');
    }
    await b.CompleteTask(req);
  }

  // readmitTask delegates to OrchestratorBinding.ReadmitTask.
  //
  // The binding takes the SDK request struct, like every other task verb on
  // it; passing the two ids positionally reached a Go method that does not
  // exist in that shape and threw on every click in desktop mode.
  async readmitTask(tenantID: string, taskID: string): Promise<void> {
    const b = orchestratorBinding();
    if (typeof b.ReadmitTask !== 'function') {
      throw new Error('wails: ReadmitTask not implemented');
    }
    await b.ReadmitTask({ tenant_id: tenantID, task_id: taskID });
  }

  // deleteTask delegates to OrchestratorBinding.DeleteTask.
  async deleteTask(req: DeleteTaskRequest): Promise<DeleteTaskResponse> {
    const b = orchestratorBinding();
    if (typeof b.DeleteTask !== 'function') {
      throw new Error('wails: DeleteTask not implemented');
    }
    return b.DeleteTask(req) as Promise<DeleteTaskResponse>;
  }

  // listAttention delegates to OrchestratorBinding.AttentionList, which
  // returns the full paged envelope (items/total/facets), not a bare array —
  // adoptPage reads it either way.
  async listAttention(tenantID: string, page?: AttentionPageParams): Promise<PageResult<AttentionItem>> {
    const b = orchestratorBinding();
    if (typeof b.AttentionList !== 'function') {
      throw new Error('wails: AttentionList not implemented');
    }
    const req: Record<string, unknown> = { tenant_id: tenantID, ...pageFields(page) };
    if (page?.project_id !== undefined) req.project_id = page.project_id;
    if (page?.kind !== undefined) req.kind = page.kind;
    return adoptPage<AttentionItem>(await b.AttentionList(req), 'items');
  }

  // resolveAttention delegates to OrchestratorBinding.AttentionResolve.
  async resolveAttention(req: AttentionResolveRequest): Promise<AttentionItem> {
    const b = orchestratorBinding();
    if (typeof b.AttentionResolve !== 'function') {
      throw new Error('wails: AttentionResolve not implemented');
    }
    return b.AttentionResolve(req) as Promise<AttentionItem>;
  }

  // decomposeTask delegates to OrchestratorBinding.DecomposeTask.
  async decomposeTask(req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> {
    const b = orchestratorBinding();
    if (typeof b.DecomposeTask !== 'function') {
      throw new Error('wails: DecomposeTask not implemented');
    }
    return b.DecomposeTask(req) as Promise<DecomposeTaskResponse>;
  }

  // registerFleetNode delegates to OrchestratorBinding.RegisterFleetNode.
  async registerFleetNode(req: RegisterFleetNodeRequest): Promise<RegisterFleetNodeResponse> {
    const b = orchestratorBinding();
    if (typeof b.RegisterFleetNode !== 'function') {
      throw new Error('wails: RegisterFleetNode not implemented');
    }
    return b.RegisterFleetNode(req) as Promise<RegisterFleetNodeResponse>;
  }

  // removeFleetNode delegates to OrchestratorBinding.RemoveFleetNode.
  async removeFleetNode(req: RemoveFleetNodeRequest): Promise<void> {
    const b = orchestratorBinding();
    if (typeof b.RemoveFleetNode !== 'function') {
      throw new Error('wails: RemoveFleetNode not implemented');
    }
    await b.RemoveFleetNode(req);
  }

  // disableFleetNode delegates to OrchestratorBinding.DisableFleetNode.
  async disableFleetNode(req: import('./types.js').DisableFleetNodeRequest): Promise<import('./types.js').DisableFleetNodeResponse> {
    const b = orchestratorBinding();
    if (typeof b.DisableFleetNode !== 'function') {
      throw new Error('wails: DisableFleetNode not implemented');
    }
    return b.DisableFleetNode(req) as Promise<import('./types.js').DisableFleetNodeResponse>;
  }

  // enableFleetNode delegates to OrchestratorBinding.EnableFleetNode.
  async enableFleetNode(req: import('./types.js').EnableFleetNodeRequest): Promise<import('./types.js').EnableFleetNodeResponse> {
    const b = orchestratorBinding();
    if (typeof b.EnableFleetNode !== 'function') {
      throw new Error('wails: EnableFleetNode not implemented');
    }
    return b.EnableFleetNode(req) as Promise<import('./types.js').EnableFleetNodeResponse>;
  }

  // subscribeSession returns an async iterable of session events via Wails IPC.
  //
  // The Go-side binding (Phase 6) is registered as SessionBinding.Subscribe.
  // It returns the historical replay slice first, then emits new events via
  // the Wails event bus under the topic "session-event:<sessionID>".
  async *subscribeSession(
    req: SubscribeSessionEventsRequest,
  ): AsyncIterable<SessionEventEnvelope> {
    // Fetch historical events from the Go binding.
    const history = (await sessionBinding().Subscribe(req)) as SessionEventEnvelope[];
    for (const h of history) {
      yield h;
    }

    if (!req.follow) return;

    // Queue to buffer events that arrive while the generator is suspended.
    const queue: SessionEventEnvelope[] = [];
    // resolve holds the pending Promise resolver so we can wake the generator
    // when a new event arrives.
    let wakeResolve: ((v: void) => void) | null = null;

    const eventTopic = `session-event:${req.session_id}`;

    if (!window.runtime) {
      // No Wails runtime available (e.g. test environment without the stub).
      return;
    }

    const unsub = window.runtime.EventsOn(
      eventTopic,
      (...args: unknown[]) => {
        if (args.length > 0) {
          queue.push(args[0] as SessionEventEnvelope);
          const r = wakeResolve;
          wakeResolve = null;
          r?.();
        }
      },
    );

    try {
      while (true) {
        if (req.signal?.aborted) return;

        if (queue.length > 0) {
          yield queue.shift()!;
          continue;
        }

        // Wait until the next event arrives or the signal fires.
        await new Promise<void>((resolve) => {
          wakeResolve = resolve;
          // If the signal fires while we are waiting, wake immediately.
          req.signal?.addEventListener('abort', () => {
            const r = wakeResolve;
            wakeResolve = null;
            r?.();
          }, { once: true });
        });

        if (req.signal?.aborted) return;
      }
    } finally {
      unsub();
    }
  }

  // inference delegates to OrchestratorBinding.Inference via Wails IPC.
  // The Go-side binding calls into the orchestrator service directly (in-process).
  async inference(req: InferenceRequest): Promise<InferenceResponse> {
    const b = orchestratorBinding();
    if (typeof b.Inference !== 'function') {
      throw new Error('wails: Inference not implemented — update the Go Wails binding');
    }
    return b.Inference(req) as Promise<InferenceResponse>;
  }

  // inferenceStream is not natively streamable over Wails IPC (no chunked HTTP).
  // Falls back to aggregate inference and yields a single chunk so callers
  // using the streaming API work transparently in desktop mode.
  async *inferenceStream(req: InferenceRequest, _signal?: AbortSignal): AsyncIterable<InferenceChunk> {
    const resp = await this.inference(req);
    yield {
      delta: resp.content,
      model: resp.model,
      stop_reason: resp.stop_reason,
      final: true,
    };
  }
}

// WailsAuthClient is a no-op AuthClient for desktop mode.
// In Wails, auth is managed by the OS session; validate always returns true.
// WebAuthn / PIV flows require a browser TLS handshake and are not applicable
// to the embedded desktop transport — those methods delegate to notImpl.
class WailsAuthClient implements AuthClient {
  async authValidate(_token?: string): Promise<boolean> {
    return true;
  }
  async authLogin(_credentials: { username: string; password: string }): Promise<void> {
    // Wails desktop mode runs embedded — no login required.
  }
  async webAuthnBeginLogin(_tenantID: string, _email: string): Promise<import('./types.js').WebAuthnLoginChallenge> {
    // Wails desktop mode delegates to the embedded REST transport for WebAuthn.
    // Use the REST transport client when WebAuthn is needed in desktop mode.
    throw new Error('webAuthnBeginLogin: use REST transport for WebAuthn in desktop mode');
  }
  async webAuthnFinishLogin(
    _challenge: import('./types.js').WebAuthnLoginChallenge,
    _credential: import('./types.js').PublicKeyCredentialJSON,
  ): Promise<import('./types.js').AuthTokenResponse> {
    throw new Error('webAuthnFinishLogin: use REST transport for WebAuthn in desktop mode');
  }
  async loginPIV(): Promise<import('./types.js').AuthTokenResponse> {
    throw new Error('loginPIV: PIV requires a browser TLS handshake; not supported in desktop mode');
  }
  async logout(_token?: string): Promise<void> {
    // Wails desktop mode runs embedded — there is no remote bearer token to
    // revoke. The host application owns the OS-level session; this is a no-op
    // so the caller can use a single logout() method across transports.
  }
  async registerBegin(_req: import('./interfaces.js').SelfRegisterBeginRequest): Promise<import('./interfaces.js').SelfRegisterBeginResponse> {
    throw new Error('registerBegin: use REST transport for self-service registration in desktop mode');
  }
  async registerFinish(_req: import('./interfaces.js').SelfRegisterFinishRequest): Promise<import('./interfaces.js').SelfRegisterFinishResponse> {
    throw new Error('registerFinish: use REST transport for self-service registration in desktop mode');
  }
}

// WailsClient is the top-level Client implementation for Wails desktop mode.
export class WailsClient implements Client {
  readonly connection: ConnectionClient;
  readonly orchestrator: OrchestratorClient;
  readonly auth: AuthClient;
  // Data sub-clients delegate to REST over the embedded server. The Wails
  // desktop mode runs the full QuicBaaS server in-process so REST calls
  // resolve against localhost without a network round-trip.
  // These are intentionally null until Wails Go bindings are generated;
  // desktop users can also access data surfaces through the REST transport.
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
  readonly projects = unsupportedClient<ProjectsClient>('wails', 'projects', wailsUnsupported);
  readonly sdlc = unsupportedClient<SdlcClient>('wails', 'sdlc', wailsUnsupported);
  readonly accounts = unsupportedClient<AccountsClient>('wails', 'accounts', wailsUnsupported);
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
  readonly entities: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly attestation: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly prompts: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly xkmsKeys: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly piv: any = null;
  readonly xkms: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly backups: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly clusterNodes: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly kmsAdmin: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly objectStorageAdmin: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly parameters: any = null;
  readonly vectorAdmin: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly xkmsFido2: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly dynamicSecrets: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly barrier: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly tenantBarrier: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly ceremony: any = null;
  // cluster status/management resolves through the REST transport in desktop
  // mode (the embedded server runs in-process); no Wails Go binding yet.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly cluster: any = null;
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  readonly seal: any = null;
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

  constructor() {
    this.connection = new WailsConnectionClient();
    this.orchestrator = new WailsOrchestratorClient();
    this.auth = new WailsAuthClient();
    const keysClient = new WailsSSHKeysClient();
    this.ssh = new WailsSSHClient(keysClient);
    this.sshKeys = this.ssh.keys;
  }
}
