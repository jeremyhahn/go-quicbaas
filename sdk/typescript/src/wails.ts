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
  DeployAgentResult,
  DeployAgentSpec,
  InferenceRequest,
  InferenceResponse,
  InferenceChunk,
  AgentRegistration,
  CancelJobRequest,
  CloseSessionRequest,
  CloseSessionResponse,
  CreateTaskRequest,
  CreateTaskResponse,
  ClaimTaskRequest,
  CompleteTaskRequest,
  CreateWorkflowRequest,
  CreateWorkflowResponse,
  DecomposeTaskRequest,
  DecomposeTaskResponse,
  GetAgentRequest,
  GetJobStatusRequest,
  GetSessionHistoryRequest,
  GetSessionHistoryResponse,
  GetTaskRequest,
  HealthCheckReport,
  JobEvent,
  JobStatus,
  ListAgentsRequest,
  ListAgentsResponse,
  ListJobsRequest,
  ListJobsResponse,
  ListSessionsRequest,
  ListSessionsResponse,
  ListTasksRequest,
  ListTasksResponse,
  ListWorkflowsResponse,
  PromptSessionRequest,
  PromptSessionResponse,
  RegisterAgentRequest,
  RegisterAgentResponse,
  RemoveAgentRequest,
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
import { ConnectionError } from './errors.js';

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

// connectionBinding returns the Wails-generated ConnectionBinding proxy.
function connectionBinding(): WailsBinding {
  return wailsGo().main.ConnectionBinding as unknown as WailsBinding;
}

// orchestratorBinding returns the Wails-generated OrchestratorBinding proxy.
function orchestratorBinding(): WailsBinding {
  return wailsGo().main.OrchestratorBinding as unknown as WailsBinding;
}

// sessionBinding returns the Wails-generated SessionBinding proxy.
// Phase 6 Go side: window.go.main.SessionBinding.
function sessionBinding(): WailsBinding {
  return wailsGo().main.SessionBinding as unknown as WailsBinding;
}

// agentDeployBinding returns the Wails-generated AgentDeployBinding proxy.
// Wails exposes the Go struct at window.go.main.AgentDeployBinding.
function agentDeployBinding(): WailsBinding {
  return wailsGo().main.AgentDeployBinding as unknown as WailsBinding;
}

// sshKeysBinding returns the Wails-generated SSHKeysBinding proxy. The
// Go struct lives at window.go.main.SSHKeysBinding and mirrors the
// sshkey.Client interface.
function sshKeysBinding(): WailsBinding {
  return wailsGo().main.SSHKeysBinding as unknown as WailsBinding;
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

// WailsOrchestratorClient implements OrchestratorClient over Wails IPC.
//
// Every Wails Go binding accepts the corresponding SDK request struct
// verbatim — Wails serialises the TS object as JSON and Go unmarshals it
// into the typed request. Methods on the OrchestratorClient interface that
// expose flat argument lists (e.g. `listJobs(tenantID)`) construct the
// request object here so the interface contract is unchanged while the
// wire shape matches what Go expects.
class WailsOrchestratorClient implements OrchestratorClient {
  async listJobs(tenantID: string): Promise<JobStatus[]> {
    const req: ListJobsRequest = { tenant_id: tenantID };
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

  async listAgents(req: ListAgentsRequest): Promise<ListAgentsResponse> {
    return orchestratorBinding().ListAgents(req) as Promise<ListAgentsResponse>;
  }

  async getAgentStatus(req: GetAgentRequest): Promise<AgentRegistration> {
    return orchestratorBinding().GetAgent(req) as Promise<AgentRegistration>;
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

  async listBackendModels(agentID: string, backendName: string): Promise<string[]> {
    return orchestratorBinding().ListBackendModels({ agent_id: agentID, backend_name: backendName }) as Promise<string[]>;
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

  // deployAgent forwards the spec to the Wails AgentDeployBinding.Deploy
  // method. The Go-side binding orchestrates the SSH+SFTP+systemd workflow
  // and returns a structured result.
  async deployAgent(spec: DeployAgentSpec): Promise<DeployAgentResult> {
    return agentDeployBinding().Deploy(spec) as Promise<DeployAgentResult>;
  }

  // getSessionHistory fetches a page of historical session events.
  // The Go binding forwards the request struct verbatim to the SDK.
  async getSessionHistory(req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> {
    return sessionBinding().GetHistory(req) as Promise<GetSessionHistoryResponse>;
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

  // decomposeTask delegates to OrchestratorBinding.DecomposeTask.
  async decomposeTask(req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> {
    const b = orchestratorBinding();
    if (typeof b.DecomposeTask !== 'function') {
      throw new Error('wails: DecomposeTask not implemented');
    }
    return b.DecomposeTask(req) as Promise<DecomposeTaskResponse>;
  }

  // registerAgent delegates to OrchestratorBinding.RegisterAgent.
  async registerAgent(req: RegisterAgentRequest): Promise<RegisterAgentResponse> {
    const b = orchestratorBinding();
    if (typeof b.RegisterAgent !== 'function') {
      throw new Error('wails: RegisterAgent not implemented');
    }
    return b.RegisterAgent(req) as Promise<RegisterAgentResponse>;
  }

  // removeAgent delegates to OrchestratorBinding.RemoveAgent.
  async removeAgent(req: RemoveAgentRequest): Promise<void> {
    const b = orchestratorBinding();
    if (typeof b.RemoveAgent !== 'function') {
      throw new Error('wails: RemoveAgent not implemented');
    }
    await b.RemoveAgent(req);
  }

  // disableAgent delegates to OrchestratorBinding.DisableAgent.
  async disableAgent(req: import('./types.js').DisableAgentRequest): Promise<import('./types.js').DisableAgentResponse> {
    const b = orchestratorBinding();
    if (typeof b.DisableAgent !== 'function') {
      throw new Error('wails: DisableAgent not implemented');
    }
    return b.DisableAgent(req) as Promise<import('./types.js').DisableAgentResponse>;
  }

  // enableAgent delegates to OrchestratorBinding.EnableAgent.
  async enableAgent(req: import('./types.js').EnableAgentRequest): Promise<import('./types.js').EnableAgentResponse> {
    const b = orchestratorBinding();
    if (typeof b.EnableAgent !== 'function') {
      throw new Error('wails: EnableAgent not implemented');
    }
    return b.EnableAgent(req) as Promise<import('./types.js').EnableAgentResponse>;
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
  async webAuthnBeginLogin(_tenantID: string, _email: string): Promise<import('./types.js').CredentialAssertionJSON> {
    // Wails desktop mode delegates to the embedded REST transport for WebAuthn.
    // Use the REST transport client when WebAuthn is needed in desktop mode.
    throw new Error('webAuthnBeginLogin: use REST transport for WebAuthn in desktop mode');
  }
  async webAuthnFinishLogin(
    _tenantID: string,
    _email: string,
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
