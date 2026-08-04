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
  DataClient,
  IAMClient,
  KMSClient,
  KVEntry,
  KVListResponse,
  ObjectEntry,
  ObjectListResponse,
  ObjectsClient,
  BarrierClient,
  TenantBarrierClient,
  CeremonyClient,
  BootstrapClient,
  ClusterClient,
  SealClient,
  SecretsClient,
  SSHKeysClient,
  SSHKnownHostsClient,
  SSHAuthorizedKeysClient,
  SSHClient,
  TSPoint,
  TSQueryResponse,
  TimeSeriesClient,
  VectorNamespace,
  VectorSearchResult,
  VectorsClient,
  OrchestratorClient,
} from './interfaces.js';
import type {
  RAGClient,
  RAGQueryRequest,
  RAGQueryResponse,
  RAGBuildContextRequest,
  RAGBuildContextResponse,
  RAGDocument,
  RAGListDocumentsResponse,
  RAGReindexRequest,
} from './rag.js';
import type {
  EmbeddingsClient,
  GenerateEmbeddingRequest,
  GenerateEmbeddingResponse,
  ListProvidersRequest,
  ListProvidersResponse,
  SetProviderStatusResponse,
} from './embedding_sdk.js';
import type {
  MessagingClient,
  ListChannelsRequest,
  ListChannelsResponse,
  CreateChannelRequest,
  CreateChannelResponse,
  DeleteChannelResponse,
  PublishRequest,
  PublishResponse,
} from './messaging_sdk.js';
import type {
  SchedulerClient,
  SchedulerJob,
  ListJobsRequest,
  ListJobsResponse,
  CreateJobRequest,
  CreateJobResponse,
  DeleteJobResponse,
  TriggerJobResponse,
  SchedulerStatus,
} from './scheduler_sdk.js';
import type {
  ProjectsClient,
  Project,
  CreateProjectRequest,
  CreateProjectResponse,
  GetProjectResponse,
  UpdateProjectRequest,
  UpdateProjectResponse,
  DeleteProjectResponse,
  ListProjectsResponse,
  GrantAgentRoleRequest,
  GrantAgentRoleResponse,
  RevokeAgentRoleRequest,
  RevokeAgentRoleResponse,
} from './projects.js';
import type {
  AgentBackendInfo,
  AuthTokenResponse,
  CreateKeyRequest,
  CreateKeyResponse,
  CreateTenantRequest,
  CreateTenantResponse,
  CreateUserRequest,
  CreateUserResponse,
  DeployAgentResult,
  DeployAgentSpec,
  InferenceRequest,
  InferenceResponse,
  InferenceChunk,
  AgentRegistration,
  ClientConfig,
  CredentialAssertionJSON,
  PublicKeyCredentialJSON,
  PublicTenant,
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
  GetSecretResponse,
  GetSessionHistoryRequest,
  GetSessionHistoryResponse,
  GetWorkflowResponse,
  HealthCheckReport,
  IAMRole,
  IAMPolicy,
  IAMTenant,
  IAMUser,
  JobEvent,
  JobStatus,
  KMSDecryptRequest,
  KMSDecryptResponse,
  KMSEncryptRequest,
  KMSEncryptResponse,
  KMSKey,
  KMSKeyStatus,
  ListAgentsRequest,
  ListAgentsResponse,
  ListKeysResponse,
  ListPoliciesResponse,
  ListRolesResponse,
  ListSecretsResponse,
  ListSessionsRequest,
  ListSessionsResponse,
  ListTenantsResponse,
  ListUsersResponse,
  ListWorkflowsResponse,
  PromptSessionRequest,
  PromptSessionResponse,
  RegisterAgentRequest,
  RegisterAgentResponse,
  RemoveAgentRequest,
  DisableAgentRequest,
  DisableAgentResponse,
  EnableAgentRequest,
  EnableAgentResponse,
  RekeyRequest,
  RekeyResponse,
  RotateKeyResponse,
  RotateRootKeyResponse,
  RotateSecretResponse,
  ListSecretVersionsResponse,
  SetDefaultKeyResponse,
  SealInitRequest,
  SealInitResponse,
  SealStatus,
  BarrierStatusResponse,
  BarrierInitRequest,
  BarrierUnsealRequest,
  BarrierShamirInitRequest,
  BarrierShamirInitResponse,
  BarrierUnsealShareRequest,
  BarrierUnsealShareResponse,
  BarrierUnsealSharesRequest,
  BarrierShamirSharesResponse,
  BarrierShamirDeleteShareRequest,
  BarrierRekeyRequest,
  BarrierRekeyResponse,
  BarrierGenerateRecoveryKeysRequest,
  BarrierRecoveryKeysResponse,
  BarrierRecoverWithKeysRequest,
  BarrierHasRecoveryKeysResponse,
  BarrierGenerateRootTokenRequest,
  BarrierRootTokenResponse,
  TenantBarrierInitRequest,
  TenantBarrierInitResponse,
  TenantBarrierUnsealShareResponse,
  TenantBarrierStatusResponse,
  CeremonyInitializeRequest,
  CeremonyInitializeResponse,
  CeremonyStatusResponse,
  CeremonyClaimCertBeginRequest,
  CeremonyClaimCertBeginResponse,
  CeremonyClaimCertCompleteRequest,
  CeremonyClaimCertCompleteResponse,
  CeremonyClaimShareRequest,
  CeremonyClaimShareResponse,
  CeremonySignCSRRequest,
  CeremonySignCSRResponse,
  CeremonyEnrollExternalOfficerRequest,
  CeremonyEnrollExternalOfficerResponse,
  BootstrapMethod,
  BootstrapBundleRequest,
  BootstrapBundleResponse,
  ClusterStatusResponse,
  SecretEntry,
  SessionEventEnvelope,
  SessionInfo,
  SpawnSessionRequest,
  SpawnSessionResponse,
  StartJobRequest,
  StartJobResponse,
  StoreSecretRequest,
  SubscribeSessionEventsRequest,
  Task,
  UnsealRequest,
  UnsealResponse,
  WorkflowDefinition,
  SSHScope,
  SSHKey,
  SSHPublicKey,
  SignRequest,
  SignResponse,
  SSHPin,
  SSHAuthorizedEntry,
  CreateSSHKeyRequest,
  ImportSSHKeyRequest,
  ListSSHKeysQuery,
  ListSSHKeysResponse,
  AddSSHPinRequest,
  ListSSHPinsResponse,
  VerifySSHPinRequest,
  EmitAuthorizedRequest,
  EmitAuthorizedResponse,
  VerifyAuthorizedRequest,
  VerifyAuthorizedResponse,
} from './types.js';
import {
  APIError,
  ConnectionError,
  NotConnectedError,
  DeployNotSupportedError,
  SSHError,
  SSHNotFoundError,
  SSHDuplicateError,
  SSHInvalidError,
  SSHUnavailableError,
  SSHServerError,
  SSHHostMismatchError,
} from './errors.js';

// API base path for all orchestrator endpoints.
const API_PREFIX = '/api/v1/orchestrator';

// resolveToken resolves a bearer token from either a static string or an
// async factory function. Returns an empty string when no token is configured.
async function resolveToken(
  token: string | (() => Promise<string>) | undefined,
): Promise<string> {
  if (!token) return '';
  if (typeof token === 'function') return token();
  return token;
}

// parseResponseBody reads the body of a 2xx fetch Response and returns the
// parsed JSON, tolerating 204 No Content and empty bodies (returns undefined
// as T). All fetchJSON and fetchSSH success tails delegate here to prevent
// "Unexpected end of JSON input" when servers return 204 or a body-less 2xx.
async function parseResponseBody<T>(resp: Response): Promise<T> {
  if (resp.status === 204) return undefined as T;
  const text = await resp.text();
  return (text ? (JSON.parse(text) as T) : (undefined as T));
}

// RestConnectionClient manages the server address and validates connectivity
// by performing a health check against the orchestrator endpoint.
class RestConnectionClient implements ConnectionClient {
  private _connected = false;
  private _address = '';
  private readonly _transport = 'rest';

  constructor(address: string) {
    this._address = address;
  }

  // connect validates that the server is reachable by calling the health endpoint.
  async connect(address: string, _transport?: string, _tenantID?: string): Promise<void> {
    this._address = address;
    const resp = await fetch(`${address}${API_PREFIX}/health`);
    if (!resp.ok) {
      throw new ConnectionError(
        `Failed to connect to QuicBaaS at ${address}: ${resp.statusText}`,
      );
    }
    this._connected = true;
  }

  async disconnect(): Promise<void> {
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

// RestOrchestratorClient implements OrchestratorClient using the QuicBaaS REST API.
// All endpoints are under /api/v1/orchestrator and accept/return JSON.
// Error responses are surfaced as APIError with the HTTP status code.
class RestOrchestratorClient implements OrchestratorClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    // Trim trailing slash to allow consistent path concatenation.
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  // authHeaders builds Authorization and tenant headers from config.
  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  // fetchJSON performs an authenticated fetch and deserialises the response body.
  // Non-2xx responses are converted to APIError.
  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${API_PREFIX}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });

    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText if the body is not valid JSON.
      }
      throw new APIError(resp.status, message);
    }

    return parseResponseBody<T>(resp);
  }

  // listJobs maps to GET /api/v1/orchestrator/jobs?tenant_id=<tenantID>
  // Server returns {jobs: JobStatus[] | null}; unwrap to bare array.
  async listJobs(tenantID: string): Promise<JobStatus[]> {
    const raw = await this.fetchJSON<{ jobs?: JobStatus[] | null } | JobStatus[]>(
      `/jobs?tenant_id=${encodeURIComponent(tenantID)}`,
    );
    if (Array.isArray(raw)) return raw;
    return (raw as { jobs?: JobStatus[] | null }).jobs ?? [];
  }

  // getJobStatus maps to GET /api/v1/orchestrator/jobs/:jobID?tenant_id=<tenantID>
  async getJobStatus(tenantID: string, jobID: string): Promise<JobStatus> {
    return this.fetchJSON<JobStatus>(
      `/jobs/${encodeURIComponent(jobID)}?tenant_id=${encodeURIComponent(tenantID)}`,
    );
  }

  // startJob maps to POST /api/v1/orchestrator/jobs
  async startJob(req: StartJobRequest): Promise<StartJobResponse> {
    return this.fetchJSON<StartJobResponse>('/jobs', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // cancelJob maps to DELETE /api/v1/orchestrator/jobs/:jobID?tenant_id=<tenantID>
  async cancelJob(tenantID: string, jobID: string): Promise<void> {
    await this.fetchJSON<void>(
      `/jobs/${encodeURIComponent(jobID)}?tenant_id=${encodeURIComponent(tenantID)}`,
      { method: 'DELETE' },
    );
  }

  // listAgents maps to GET /api/v1/orchestrator/agents with query parameters.
  async listAgents(req: ListAgentsRequest): Promise<ListAgentsResponse> {
    const params = new URLSearchParams({ tenant_id: req.tenant_id });
    if (req.host) params.set('host', req.host);
    if (req.status) params.set('status', req.status);
    if (req.agent_type) params.set('agent_type', req.agent_type);
    if (req.backend) params.set('backend', req.backend);
    if (req.model) params.set('model', req.model);
    return this.fetchJSON<ListAgentsResponse>(`/agents?${params.toString()}`);
  }

  // getAgentStatus maps to GET /api/v1/orchestrator/agents/:agentID?tenant_id=<tenantID>
  async getAgentStatus(req: GetAgentRequest): Promise<AgentRegistration> {
    return this.fetchJSON<AgentRegistration>(
      `/agents/${encodeURIComponent(req.agent_id)}?tenant_id=${encodeURIComponent(req.tenant_id)}`,
    );
  }

  // listSessions maps to GET /api/v1/orchestrator/sessions?tenant_id=<tenantID>
  // Server returns {sessions: [...], timestamp}; normalise missing count field.
  async listSessions(req: ListSessionsRequest): Promise<ListSessionsResponse> {
    if (!req.tenantID) {
      throw new Error('listSessions: tenantID is required');
    }
    const params = new URLSearchParams();
    params.set('tenant_id', req.tenantID);
    if (req.backend) params.set('backend', req.backend);
    if (req.status) params.set('status', req.status);
    const raw = await this.fetchJSON<{ sessions?: ListSessionsResponse['sessions']; count?: number }>(
      `/sessions?${params.toString()}`,
    );
    return {
      sessions: raw.sessions ?? [],
      count: raw.count ?? (raw.sessions ?? []).length,
    };
  }

  // listTasks maps to GET /api/v1/orchestrator/tasks?tenant_id=<tenantID>
  // Server returns {tasks: Task[]}; unwrap to bare array.
  async listTasks(tenantID: string): Promise<Task[]> {
    const raw = await this.fetchJSON<{ tasks?: Task[] } | Task[]>(
      `/tasks?tenant_id=${encodeURIComponent(tenantID)}`,
    );
    if (Array.isArray(raw)) return raw;
    return (raw as { tasks?: Task[] }).tasks ?? [];
  }

  // getTask maps to GET /api/v1/orchestrator/tasks/:taskID?tenant_id=<tenantID>
  async getTask(tenantID: string, taskID: string): Promise<Task> {
    return this.fetchJSON<Task>(
      `/tasks/${encodeURIComponent(taskID)}?tenant_id=${encodeURIComponent(tenantID)}`,
    );
  }

  // listBackends maps to GET /api/v1/orchestrator/backends
  // The REST endpoint returns {"backends": [...]}; unwrap to match the
  // Go SDK contract which exposes a bare slice.
  async listBackends(): Promise<AgentBackendInfo[]> {
    const resp = await this.fetchJSON<{ backends?: AgentBackendInfo[] } | AgentBackendInfo[]>(
      '/backends',
    );
    if (Array.isArray(resp)) {
      return resp;
    }
    return resp.backends ?? [];
  }

  // listBackendModels maps to
  // GET /api/v1/orchestrator/agents/{agentID}/backends/{backendName}/models
  // Returns the model names available on the named backend as seen from the
  // specified agent. Pass "any" as agentID to query without host affinity.
  async listBackendModels(agentID: string, backendName: string): Promise<string[]> {
    const aid = encodeURIComponent(agentID || 'any');
    const bn = encodeURIComponent(backendName);
    const resp = await this.fetchJSON<{ models?: string[] }>(`/agents/${aid}/backends/${bn}/models`);
    return resp.models ?? [];
  }

  // healthCheck maps to GET /api/v1/orchestrator/health
  async healthCheck(): Promise<HealthCheckReport> {
    return this.fetchJSON<HealthCheckReport>('/health');
  }

  // subscribeJobEvents opens a Server-Sent Events stream for a running job.
  // Maps to GET /api/v1/orchestrator/jobs/:jobID/events?tenant_id=<tenantID>
  // Returns a cleanup function that closes the EventSource connection.
  subscribeJobEvents(
    tenantID: string,
    jobID: string,
    onEvent: (event: JobEvent) => void,
  ): () => void {
    const url = [
      this.baseUrl,
      API_PREFIX,
      `/jobs/${encodeURIComponent(jobID)}/events`,
      `?tenant_id=${encodeURIComponent(tenantID)}`,
    ].join('');

    const source = new EventSource(url);

    source.onmessage = (e: MessageEvent<string>) => {
      try {
        const event = JSON.parse(e.data) as JobEvent;
        onEvent(event);
      } catch {
        // Discard unparseable frames; the server may send keep-alive comments.
      }
    };

    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) {
        source.close();
      }
    };

    return () => source.close();
  }

  // spawnSession maps to POST /api/v1/orchestrator/sessions?tenant_id=<t>.
  // tenantID is threaded via query string (server expectation) and not in
  // the JSON body — the server-side SpawnSessionRequest struct has no
  // tenant_id field.
  async spawnSession(req: SpawnSessionRequest): Promise<SpawnSessionResponse> {
    if (!req.tenantID) {
      throw new Error('spawnSession: tenantID is required');
    }
    const { tenantID, ...body } = req;
    return this.fetchJSON<SpawnSessionResponse>(
      `/sessions?tenant_id=${encodeURIComponent(tenantID)}`,
      { method: 'POST', body: JSON.stringify(body) },
    );
  }

  // promptSession maps to POST /api/v1/orchestrator/sessions/:sessionID/prompt
  async promptSession(req: PromptSessionRequest): Promise<PromptSessionResponse> {
    return this.fetchJSON<PromptSessionResponse>(
      `/sessions/${encodeURIComponent(req.session_id)}/prompt`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  // closeSession maps to DELETE /api/v1/orchestrator/sessions/:sessionID
  async closeSession(req: CloseSessionRequest): Promise<CloseSessionResponse> {
    return this.fetchJSON<CloseSessionResponse>(
      `/sessions/${encodeURIComponent(req.session_id)}`,
      { method: 'DELETE' },
    );
  }

  // createWorkflow maps to POST /api/v1/orchestrator/workflows
  async createWorkflow(req: CreateWorkflowRequest): Promise<CreateWorkflowResponse> {
    return this.fetchJSON<CreateWorkflowResponse>('/workflows', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // listWorkflows maps to GET /api/v1/orchestrator/workflows?tenant_id=<tenantID>
  // Server returns {workflows:[...]}; count field may be absent — derive it.
  async listWorkflows(tenantID: string): Promise<ListWorkflowsResponse> {
    const raw = await this.fetchJSON<{ workflows?: ListWorkflowsResponse['workflows']; count?: number }>(
      `/workflows?tenant_id=${encodeURIComponent(tenantID)}`,
    );
    return {
      workflows: raw.workflows ?? [],
      count: raw.count ?? (raw.workflows ?? []).length,
    };
  }

  // getWorkflow maps to GET /api/v1/orchestrator/workflows/:name?tenant_id=<tenantID>
  async getWorkflow(tenantID: string, name: string): Promise<WorkflowDefinition> {
    const resp = await this.fetchJSON<GetWorkflowResponse>(
      `/workflows/${encodeURIComponent(name)}?tenant_id=${encodeURIComponent(tenantID)}`,
    );
    return resp.workflow;
  }

  // updateWorkflow maps to PUT /api/v1/orchestrator/workflows/:name
  async updateWorkflow(name: string, def: WorkflowDefinition): Promise<WorkflowDefinition> {
    const resp = await this.fetchJSON<{ workflow: WorkflowDefinition }>(
      `/workflows/${encodeURIComponent(name)}`,
      { method: 'PUT', body: JSON.stringify(def) },
    );
    return resp.workflow;
  }

  // deleteWorkflow maps to DELETE /api/v1/orchestrator/workflows/:name
  async deleteWorkflow(tenantID: string, name: string): Promise<void> {
    await this.fetchJSON<void>(
      `/workflows/${encodeURIComponent(name)}?tenant_id=${encodeURIComponent(tenantID)}`,
      { method: 'DELETE' },
    );
  }

  // createTask maps to POST /api/v1/orchestrator/tasks
  async createTask(req: CreateTaskRequest): Promise<CreateTaskResponse> {
    return this.fetchJSON<CreateTaskResponse>('/tasks', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // claimTask maps to POST /api/v1/orchestrator/tasks/:taskID/claim
  async claimTask(req: ClaimTaskRequest): Promise<void> {
    await this.fetchJSON<void>(
      `/tasks/${encodeURIComponent(req.task_id)}/claim?tenant_id=${encodeURIComponent(req.tenant_id)}`,
      { method: 'POST', body: JSON.stringify({ agent_id: req.agent_id }) },
    );
  }

  // completeTask maps to POST /api/v1/orchestrator/tasks/:taskID/complete
  async completeTask(req: CompleteTaskRequest): Promise<void> {
    await this.fetchJSON<void>(
      `/tasks/${encodeURIComponent(req.task_id)}/complete?tenant_id=${encodeURIComponent(req.tenant_id)}`,
      { method: 'POST', body: JSON.stringify({ result: req.result }) },
    );
  }

  // decomposeTask maps to POST /api/v1/orchestrator/tasks/:taskID/decompose
  async decomposeTask(req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> {
    return this.fetchJSON<DecomposeTaskResponse>(
      `/tasks/${encodeURIComponent(req.task_id)}/decompose?tenant_id=${encodeURIComponent(req.tenant_id)}`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  // registerAgent maps to POST /api/v1/orchestrator/agents
  async registerAgent(req: RegisterAgentRequest): Promise<RegisterAgentResponse> {
    return this.fetchJSON<RegisterAgentResponse>('/agents', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // removeAgent maps to DELETE /api/v1/orchestrator/agents/:agentID?tenant_id=<tenantID>
  async removeAgent(req: RemoveAgentRequest): Promise<void> {
    await this.fetchJSON<void>(
      `/agents/${encodeURIComponent(req.agent_id)}?tenant_id=${encodeURIComponent(req.tenant_id)}`,
      { method: 'DELETE' },
    );
  }

  // disableAgent maps to POST /api/v1/orchestrator/agents/:agentID/disable?tenant_id=<tenantID>
  async disableAgent(req: DisableAgentRequest): Promise<DisableAgentResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<DisableAgentResponse>(
      `/agents/${encodeURIComponent(req.agent_id)}/disable?${query}`,
      { method: 'POST' },
    );
  }

  // enableAgent maps to POST /api/v1/orchestrator/agents/:agentID/enable?tenant_id=<tenantID>
  async enableAgent(req: EnableAgentRequest): Promise<EnableAgentResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<EnableAgentResponse>(
      `/agents/${encodeURIComponent(req.agent_id)}/enable?${query}`,
      { method: 'POST' },
    );
  }

  // deployAgent is a desktop-only operation. REST callers must use the
  // CLI (`quicbaas agent deploy`) or the desktop GUI.
  async deployAgent(_spec: DeployAgentSpec): Promise<DeployAgentResult> {
    throw new DeployNotSupportedError('rest');
  }

  // getSessionHistory maps to GET /api/v1/orchestrator/sessions/:sessionID/events
  // with Accept: application/json and follow=0, which returns a paginated JSON body.
  async getSessionHistory(req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse> {
    const params = new URLSearchParams({
      from: String(req.from_sequence ?? 0),
      follow: '0',
    });
    if (req.limit && req.limit > 0) params.set('limit', String(req.limit));
    if (req.tenant_id) params.set('tenant_id', req.tenant_id);
    return this.fetchJSON<GetSessionHistoryResponse>(
      `/sessions/${encodeURIComponent(req.session_id)}/events?${params.toString()}`,
    );
  }

  // subscribeSession opens a Server-Sent Events stream for a session.
  // Maps to GET /api/v1/orchestrator/sessions/:sessionID/events?from=N&follow=1
  //
  // The SSE stream emits frames in the standard format:
  //   id: <sequence>
  //   event: <kind>
  //   data: <json>
  //
  // Native EventSource is intentionally avoided here because it does not support
  // custom request headers (needed for bearer tokens), AbortSignal, or reliable
  // reconnect with Last-Event-ID across all target browsers. Manual fetch-based
  // SSE parsing gives us all of those.
  async *subscribeSession(
    req: SubscribeSessionEventsRequest,
  ): AsyncIterable<SessionEventEnvelope> {
    const params = new URLSearchParams({
      from: String(req.from_sequence ?? 0),
      follow: req.follow ? '1' : '0',
    });
    if (req.tenant_id) params.set('tenant_id', req.tenant_id);

    const url = `${this.baseUrl}${API_PREFIX}/sessions/${encodeURIComponent(req.session_id)}/events?${params.toString()}`;
    const auth = await this.authHeaders();

    const res = await fetch(url, {
      headers: { Accept: 'text/event-stream', ...auth },
      signal: req.signal,
    });

    if (!res.ok) {
      let message = res.statusText;
      try {
        const body = (await res.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(res.status, message);
    }

    if (!res.body) {
      throw new APIError(0, 'subscribeSession: response body is null');
    }

    const reader = res.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';
    let currentID = 0;
    let currentData = '';

    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) return;

        buffer += decoder.decode(value, { stream: true });
        // SSE lines are separated by \n; a blank line dispatches the event.
        const lines = buffer.split('\n');
        // The last element is an incomplete line — keep it in the buffer.
        buffer = lines.pop() ?? '';

        for (const line of lines) {
          if (line.startsWith('id: ')) {
            currentID = parseInt(line.slice(4), 10);
          } else if (line.startsWith('data: ')) {
            currentData = line.slice(6);
          } else if (line === '' && currentData !== '') {
            // Blank line after data dispatches the event.
            const parsed = JSON.parse(currentData) as Record<string, unknown>;
            // Merge the SSE sequence into the envelope so that callers have a
            // single source of truth regardless of whether sequence is also in
            // the JSON body.
            yield { sequence: currentID, ...parsed } as SessionEventEnvelope;
            currentData = '';
          }
        }
      }
    } finally {
      reader.cancel().catch(() => { /* ignore cancel errors on teardown */ });
    }
  }

  // inference maps to POST /api/v1/orchestrator/inference (stream=false).
  // Returns the aggregated response after the backend finishes generating.
  async inference(req: InferenceRequest): Promise<InferenceResponse> {
    return this.fetchJSON<InferenceResponse>('/inference', {
      method: 'POST',
      body: JSON.stringify({ ...req, stream: false }),
    });
  }

  // inferenceStream maps to POST /api/v1/orchestrator/inference (stream=true).
  // Reads a chunked NDJSON body and yields InferenceChunk objects as they arrive.
  // Each newline-delimited JSON object carries a delta array and a final flag.
  // The iterable completes when the server sends a chunk with final=true or the
  // response body is exhausted. Callers may abort early via the optional signal.
  async *inferenceStream(req: InferenceRequest, signal?: AbortSignal): AsyncIterable<InferenceChunk> {
    const url = `${this.baseUrl}${API_PREFIX}/inference`;
    const auth = await this.authHeaders();

    const resp = await fetch(url, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/x-ndjson',
        ...auth,
      },
      body: JSON.stringify({ ...req, stream: true }),
      signal,
    });

    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }

    if (!resp.body) {
      throw new APIError(0, 'inferenceStream: response body is null');
    }

    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    try {
      while (true) {
        const { value, done } = await reader.read();
        if (done) return;

        buffer += decoder.decode(value, { stream: true });
        // NDJSON: each complete JSON object is separated by a newline.
        const lines = buffer.split('\n');
        // Keep the last (potentially incomplete) line in the buffer.
        buffer = lines.pop() ?? '';

        for (const line of lines) {
          const trimmed = line.trim();
          if (trimmed === '') continue;
          const chunk = JSON.parse(trimmed) as InferenceChunk;
          yield chunk;
          if (chunk.final === true) return;
        }
      }
    } finally {
      reader.cancel().catch(() => { /* ignore cancel errors on teardown */ });
    }
  }
}

// RestDataClient implements DataClient over the QuicBaaS REST API.
// All endpoints are under /api/v1/kv/{type}[/{key}].
class RestDataClient implements DataClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  // kvList maps to GET /api/v1/kv/{type}
  // Server response shape: {entity_type, items: [{key, value, timestamp}], total, has_more}
  async kvList(entityType: string): Promise<KVListResponse> {
    const raw = await this.fetchJSON<{
      items?: Array<{ key?: string; value?: unknown }>;
      // Legacy shape — kept as a fallback so older daemons still work.
      entries?: Array<{ key?: string; string_key?: string; value?: unknown; data?: unknown }>;
    }>(`/api/v1/kv/${encodeURIComponent(entityType)}`);
    const source = raw.items ?? raw.entries ?? [];
    const entries: KVEntry[] = source.map((e) => ({
      key: (e.key ?? (e as { string_key?: string }).string_key) ?? '',
      value: e.value ?? (e as { data?: unknown }).data,
    }));
    return { entries };
  }

  // kvGet maps to GET /api/v1/kv/{type}/{key}
  // Server response shape: {entity_type, key, value, found, timestamp}
  async kvGet(entityType: string, key: string): Promise<KVEntry> {
    const raw = await this.fetchJSON<{
      key?: string;
      value?: unknown;
      // Legacy shape fallback.
      string_key?: string;
      data?: unknown;
    }>(`/api/v1/kv/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}`);
    return {
      key: raw.key ?? raw.string_key ?? key,
      value: raw.value ?? raw.data,
    };
  }

  // kvPut maps to PUT /api/v1/kv/{type}/{key}.
  // The server expects a {"value": <JSON>} envelope. Sending the raw value
  // directly causes the server to (mis)interpret a top-level "value" field
  // inside the user's payload as the actual stored value, silently dropping
  // the rest of the JSON. Always wrap.
  async kvPut(entityType: string, key: string, value: unknown): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/kv/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}`,
      { method: 'PUT', body: JSON.stringify({ value }) },
    );
  }

  // kvDelete maps to DELETE /api/v1/kv/{type}/{key}
  async kvDelete(entityType: string, key: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/kv/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}`,
      { method: 'DELETE' },
    );
  }
}

// RestVectorsClient implements VectorsClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/vector/namespaces.
class RestVectorsClient implements VectorsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  // listNamespaces maps to GET /api/v1/vector/namespaces.
  // Server returns {namespaces: [{namespace, dimensions, distance_metric, ...}]}
  // — translate to the SDK's {name, dimension, metric} surface.
  async listNamespaces(): Promise<VectorNamespace[]> {
    const raw = await this.fetchJSON<{
      namespaces?: Array<{
        namespace?: string;
        name?: string;
        dimensions?: number;
        dimension?: number;
        distance_metric?: string;
        metric?: string;
        vector_count?: number;
        created_at?: string;
      }>;
    }>('/api/v1/vector/namespaces');
    return (raw.namespaces ?? []).map((ns) => ({
      name: ns.name ?? ns.namespace ?? '',
      dimension: ns.dimension ?? ns.dimensions,
      metric: ns.metric ?? ns.distance_metric,
      vector_count: ns.vector_count,
      created_at: ns.created_at,
    }));
  }

  // createNamespace maps to POST /api/v1/vector/namespaces.
  // Server expects {namespace, dimensions, distance_metric} — translate from
  // the SDK's {name, dimension, metric} surface so callers don't need to
  // know about the wire-format quirks.
  async createNamespace(req: { name: string; dimension: number; metric?: string }): Promise<VectorNamespace> {
    return this.fetchJSON<VectorNamespace>('/api/v1/vector/namespaces', {
      method: 'POST',
      body: JSON.stringify({
        namespace: req.name,
        dimensions: req.dimension,
        distance_metric: req.metric ?? 'cosine',
      }),
    });
  }

  // deleteNamespace maps to DELETE /api/v1/vector/namespaces/{namespace}
  async deleteNamespace(namespace: string): Promise<void> {
    await this.fetchJSON<void>(`/api/v1/vector/namespaces/${encodeURIComponent(namespace)}`, {
      method: 'DELETE',
    });
  }

  // searchSimilar maps to POST /api/v1/vector/namespaces/{namespace}/search/similar
  async searchSimilar(
    namespace: string,
    req: { vector?: number[]; text?: string; top_k?: number; filter?: Record<string, unknown> },
  ): Promise<VectorSearchResult[]> {
    const raw = await this.fetchJSON<{ results?: VectorSearchResult[] }>(
      `/api/v1/vector/namespaces/${encodeURIComponent(namespace)}/search/similar`,
      { method: 'POST', body: JSON.stringify(req) },
    );
    return raw.results ?? [];
  }
}

// RestTimeSeriesClient implements TimeSeriesClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/timeseries.
class RestTimeSeriesClient implements TimeSeriesClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  // write maps to POST /api/v1/timeseries/write
  async write(point: TSPoint): Promise<void> {
    await this.fetchJSON<void>('/api/v1/timeseries/write', {
      method: 'POST',
      body: JSON.stringify({
        metric: point.metric,
        timestamp: point.timestamp,
        value: point.value,
        tags: point.tags ?? {},
      }),
    });
  }

  // query maps to POST /api/v1/timeseries/query
  async query(req: {
    metric: string;
    start?: string;
    end?: string;
    tags?: Record<string, string>;
    limit?: number;
  }): Promise<TSQueryResponse> {
    const raw = await this.fetchJSON<{ points?: TSPoint[]; count?: number }>(
      '/api/v1/timeseries/query',
      { method: 'POST', body: JSON.stringify(req) },
    );
    return { points: raw.points ?? [], count: raw.count ?? 0 };
  }

  // listMetrics maps to GET /api/v1/timeseries/metrics
  async listMetrics(): Promise<string[]> {
    const raw = await this.fetchJSON<{ metrics?: string[] }>('/api/v1/timeseries/metrics');
    return raw.metrics ?? [];
  }

  // deleteSeries maps to DELETE /api/v1/timeseries/series
  async deleteSeries(metric: string): Promise<void> {
    await this.fetchJSON<void>(`/api/v1/timeseries/series?metric=${encodeURIComponent(metric)}`, {
      method: 'DELETE',
    });
  }
}

// RestObjectsClient implements ObjectsClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/objects.
class RestObjectsClient implements ObjectsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  // listObjects maps to GET /api/v1/objects (with optional prefix query param)
  async listObjects(prefix?: string): Promise<ObjectListResponse> {
    const params = new URLSearchParams();
    if (prefix) params.set('prefix', prefix);
    const query = params.toString() ? `?${params.toString()}` : '';
    const raw = await this.fetchJSON<{ objects?: ObjectEntry[]; cursor?: string }>(
      `/api/v1/objects${query}`,
    );
    return { objects: raw.objects ?? [], cursor: raw.cursor };
  }

  // getObject maps to GET /api/v1/objects/{key} returning binary content.
  async getObject(key: string): Promise<Blob> {
    const url = `${this.baseUrl}/api/v1/objects/${encodeURIComponent(key)}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, { headers: { ...auth } });
    if (!resp.ok) {
      throw new APIError(resp.status, resp.statusText);
    }
    return resp.blob();
  }

  // putObject maps to PUT /api/v1/objects/{key} with binary body.
  async putObject(key: string, body: Blob | ArrayBuffer, contentType = 'application/octet-stream'): Promise<void> {
    const url = `${this.baseUrl}/api/v1/objects/${encodeURIComponent(key)}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      method: 'PUT',
      headers: { 'Content-Type': contentType, ...auth },
      body,
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const errBody = (await resp.json()) as { error?: string; message?: string };
        message = errBody.error ?? errBody.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
  }

  // deleteObject maps to DELETE /api/v1/objects/{key}
  async deleteObject(key: string): Promise<void> {
    const url = `${this.baseUrl}/api/v1/objects/${encodeURIComponent(key)}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, { method: 'DELETE', headers: { ...auth } });
    if (!resp.ok) {
      throw new APIError(resp.status, resp.statusText);
    }
  }
}

// RestAuthClient implements AuthClient using the QuicBaaS REST auth endpoints.
class RestAuthClient implements AuthClient {
  private readonly baseUrl: string;

  constructor(baseUrl: string) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
  }

  // authValidate POSTs to /api/v1/auth/validate with a bearer token. When no
  // token is supplied, skip the network call entirely and return false — the
  // server requires the Authorization header and 400s without it, which is
  // not an error condition, it's just "unauthenticated". Returns true on 2xx,
  // false on 400/401 (missing or invalid token), throws on other errors.
  async authValidate(token?: string): Promise<boolean> {
    if (!token) return false;

    const resp = await fetch(`${this.baseUrl}/api/v1/auth/validate`, {
      method: 'POST',
      credentials: 'include',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${token}`,
      },
    });

    if (resp.status === 400 || resp.status === 401) return false;

    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }

    return true;
  }

  async authLogin(credentials: { username: string; password: string }): Promise<void> {
    // The server's LoginRequest.Password is a *secure.Buffer populated via a
    // json.Unmarshaler that base64-decodes directly into an mlocked enclave
    // without ever materializing a Go string. Encode the password bytes as
    // base64 so the wire format matches the server decoder.
    const passwordBytes = new TextEncoder().encode(credentials.password);
    let binary = '';
    for (let i = 0; i < passwordBytes.length; i++) {
      binary += String.fromCharCode(passwordBytes[i]);
    }
    const body = {
      username: credentials.username,
      password: btoa(binary),
    };
    const resp = await fetch(`${this.baseUrl}/api/v1/auth/login`, {
      method: 'POST',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
  }

  // webAuthnBeginLogin fetches the WebAuthn challenge from the server.
  // Maps to POST /api/v1/tenants/{tenantID}/webauthn/login/begin.
  async webAuthnBeginLogin(tenantID: string, email: string): Promise<CredentialAssertionJSON> {
    const resp = await fetch(
      `${this.baseUrl}/api/v1/tenants/${encodeURIComponent(tenantID)}/webauthn/login/begin`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email }),
      },
    );
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return resp.json() as Promise<CredentialAssertionJSON>;
  }

  // webAuthnFinishLogin submits the WebAuthn assertion and returns tokens.
  // Maps to POST /api/v1/tenants/{tenantID}/webauthn/login/finish.
  async webAuthnFinishLogin(
    tenantID: string,
    email: string,
    credential: PublicKeyCredentialJSON,
  ): Promise<AuthTokenResponse> {
    const resp = await fetch(
      `${this.baseUrl}/api/v1/tenants/${encodeURIComponent(tenantID)}/webauthn/login/finish`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, credential }),
      },
    );
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return resp.json() as Promise<AuthTokenResponse>;
  }

  // loginPIV performs mTLS PIV smartcard login. credentials:'include' and
  // mode:'cors' ensure the browser presents the client certificate during
  // the TLS handshake so the server can read r.TLS.PeerCertificates[0].
  // Maps to POST /api/v1/auth/login/piv.
  async loginPIV(): Promise<AuthTokenResponse> {
    const resp = await fetch(`${this.baseUrl}/api/v1/auth/login/piv`, {
      method: 'POST',
      credentials: 'include',
      mode: 'cors',
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return resp.json() as Promise<AuthTokenResponse>;
  }

  // listPublicTenants returns tenants that allow self-service login.
  // Maps to GET /api/v1/iam/tenants/public. No auth required.
  async listPublicTenants(): Promise<PublicTenant[]> {
    const resp = await fetch(`${this.baseUrl}/api/v1/iam/tenants/public`, {
      method: 'GET',
      headers: { Accept: 'application/json' },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    const data = (await resp.json()) as { tenants?: PublicTenant[] } | PublicTenant[];
    return Array.isArray(data) ? data : (data.tenants ?? []);
  }

  // logout POSTs to /api/v1/auth/logout to revoke the active session token on
  // the server. Caller must clear localStorage tokens after this resolves;
  // the SDK doesn't own that storage. Idempotent — server-side errors and
  // missing tokens are swallowed so the UI can use this as a clean-state
  // primitive without branching on the network outcome.
  async logout(token?: string): Promise<void> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      Accept: 'application/json',
    };
    if (token) headers.Authorization = `Bearer ${token}`;
    try {
      await fetch(`${this.baseUrl}/api/v1/auth/logout`, {
        method: 'POST',
        credentials: 'include',
        headers,
        body: JSON.stringify({}),
      });
    } catch {
      // Network errors during logout shouldn't block the UI from clearing
      // local session state. The token is already a one-shot credential —
      // worst case it stays valid until its TTL expires server-side.
    }
  }

  // registerBegin initiates the self-service registration WebAuthn ceremony.
  // Maps to POST /api/v1/auth/register/begin.
  async registerBegin(req: import('./interfaces.js').SelfRegisterBeginRequest): Promise<import('./interfaces.js').SelfRegisterBeginResponse> {
    const resp = await fetch(`${this.baseUrl}/api/v1/auth/register/begin`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(req),
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return resp.json() as Promise<import('./interfaces.js').SelfRegisterBeginResponse>;
  }

  // registerFinish completes the self-service registration WebAuthn ceremony.
  // Maps to POST /api/v1/auth/register/finish.
  async registerFinish(req: import('./interfaces.js').SelfRegisterFinishRequest): Promise<import('./interfaces.js').SelfRegisterFinishResponse> {
    const resp = await fetch(`${this.baseUrl}/api/v1/auth/register/finish`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
      body: JSON.stringify(req),
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return resp.json() as Promise<import('./interfaces.js').SelfRegisterFinishResponse>;
  }
}

// RestRAGClient implements RAGClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/rag.
class RestRAGClient implements RAGClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  // query maps to POST /api/v1/rag/query
  async query(req: RAGQueryRequest): Promise<RAGQueryResponse> {
    return this.fetchJSON<RAGQueryResponse>('/api/v1/rag/query', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // buildContext maps to POST /api/v1/rag/context
  async buildContext(req: RAGBuildContextRequest): Promise<RAGBuildContextResponse> {
    return this.fetchJSON<RAGBuildContextResponse>('/api/v1/rag/context', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // listDocuments maps to GET /api/v1/rag/documents?namespace={namespace}
  async listDocuments(namespace: string, limit?: number, cursor?: string): Promise<RAGListDocumentsResponse> {
    const params = new URLSearchParams({ namespace });
    if (limit !== undefined) params.set('limit', String(limit));
    if (cursor) params.set('cursor', cursor);
    const raw = await this.fetchJSON<{ documents?: RAGDocument[]; next_cursor?: string; total?: number }>(
      `/api/v1/rag/documents?${params.toString()}`,
    );
    return { documents: raw.documents ?? [], next_cursor: raw.next_cursor, total: raw.total ?? 0 };
  }

  // getDocument maps to GET /api/v1/rag/documents/{id}
  async getDocument(id: number): Promise<RAGDocument> {
    return this.fetchJSON<RAGDocument>(`/api/v1/rag/documents/${id}`);
  }

  // deleteDocument maps to DELETE /api/v1/rag/documents/{id}
  async deleteDocument(id: number): Promise<void> {
    await this.fetchJSON<void>(`/api/v1/rag/documents/${id}`, { method: 'DELETE' });
  }

  // reindexDocument maps to POST /api/v1/rag/documents/{id}/reindex
  async reindexDocument(id: number, req?: RAGReindexRequest): Promise<RAGDocument> {
    return this.fetchJSON<RAGDocument>(`/api/v1/rag/documents/${id}/reindex`, {
      method: 'POST',
      body: JSON.stringify(req ?? {}),
    });
  }
}

// RestEmbeddingsClient implements EmbeddingsClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/embedding.
class RestEmbeddingsClient implements EmbeddingsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  // generate maps to POST /api/v1/embedding/generate
  async generate(req: GenerateEmbeddingRequest): Promise<GenerateEmbeddingResponse> {
    return this.fetchJSON<GenerateEmbeddingResponse>('/api/v1/embedding/generate', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // listProviders maps to GET /api/v1/embedding/providers
  async listProviders(req?: ListProvidersRequest): Promise<ListProvidersResponse> {
    const params = new URLSearchParams();
    if (req?.include_unavailable) params.set('include_unavailable', 'true');
    const query = params.toString() ? `?${params.toString()}` : '';
    return this.fetchJSON<ListProvidersResponse>(`/api/v1/embedding/providers${query}`);
  }

  // setDefaultProvider maps to PUT /api/v1/embedding/providers/default
  async setDefaultProvider(providerName: string): Promise<void> {
    await this.fetchJSON<void>('/api/v1/embedding/providers/default', {
      method: 'PUT',
      body: JSON.stringify({ provider: providerName }),
    });
  }

  // setProviderStatus maps to PUT /api/v1/embedding/providers/{name}/status
  async setProviderStatus(name: string, enabled: boolean): Promise<SetProviderStatusResponse> {
    return this.fetchJSON<SetProviderStatusResponse>(
      `/api/v1/embedding/providers/${encodeURIComponent(name)}/status`,
      { method: 'PUT', body: JSON.stringify({ enabled }) },
    );
  }
}

// RestMessagingClient implements MessagingClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/messaging.
class RestMessagingClient implements MessagingClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  // listChannels maps to GET /api/v1/messaging/channels
  // Server returns {channels:[], total, timestamp}; map to SDK's ListChannelsResponse shape:
  //   total_count ← total ?? total_count
  //   has_more    ← has_more (may be absent; default false)
  //   next_cursor ← next_cursor ?? cursor
  async listChannels(req?: ListChannelsRequest): Promise<ListChannelsResponse> {
    const params = new URLSearchParams();
    if (req?.limit) params.set('limit', String(req.limit));
    if (req?.cursor) params.set('cursor', req.cursor);
    if (req?.name_prefix) params.set('name_prefix', req.name_prefix);
    const query = params.toString() ? `?${params.toString()}` : '';
    const raw = await this.fetchJSON<{
      channels?: ListChannelsResponse['channels'];
      total?: number;
      total_count?: number;
      has_more?: boolean;
      next_cursor?: string;
      cursor?: string;
    }>(`/api/v1/messaging/channels${query}`);
    return {
      channels: raw.channels ?? [],
      total_count: raw.total_count ?? raw.total ?? 0,
      has_more: raw.has_more ?? false,
      next_cursor: raw.next_cursor ?? raw.cursor ?? '',
    };
  }

  // createChannel maps to POST /api/v1/messaging/channels
  async createChannel(req: CreateChannelRequest): Promise<CreateChannelResponse> {
    return this.fetchJSON<CreateChannelResponse>('/api/v1/messaging/channels', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // deleteChannel maps to DELETE /api/v1/messaging/channels/:name
  async deleteChannel(name: string): Promise<DeleteChannelResponse> {
    return this.fetchJSON<DeleteChannelResponse>(
      `/api/v1/messaging/channels/${encodeURIComponent(name)}`,
      { method: 'DELETE' },
    );
  }

  // publish maps to POST /api/v1/messaging/publish/:channel
  async publish(req: PublishRequest): Promise<PublishResponse> {
    const { channel, ...body } = req;
    return this.fetchJSON<PublishResponse>(
      `/api/v1/messaging/publish/${encodeURIComponent(channel)}`,
      { method: 'POST', body: JSON.stringify(body) },
    );
  }
}

// RestSchedulerClient implements SchedulerClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/scheduler.
class RestSchedulerClient implements SchedulerClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  // listJobs maps to GET /api/v1/scheduler/jobs
  // Server returns {jobs:[], has_more, timestamp}; map to full SDK ListJobsResponse shape:
  //   total_count ← total_count (absent in current daemon; derived from jobs.length)
  //   next_cursor ← next_cursor ?? cursor
  //   timestamp   ← ISO string from server
  async listJobs(req?: ListJobsRequest): Promise<ListJobsResponse> {
    const params = new URLSearchParams();
    if (req?.status) params.set('status', req.status);
    if (req?.handler) params.set('handler', req.handler);
    if (req?.limit) params.set('limit', String(req.limit));
    if (req?.cursor) params.set('cursor', req.cursor);
    if (req?.owner_type) params.set('owner_type', req.owner_type);
    if (req?.owner_id) params.set('owner_id', req.owner_id);
    const query = params.toString() ? `?${params.toString()}` : '';
    const raw = await this.fetchJSON<{
      jobs?: ListJobsResponse['jobs'];
      has_more?: boolean;
      total_count?: number;
      next_cursor?: string;
      cursor?: string;
      timestamp?: string;
    }>(`/api/v1/scheduler/jobs${query}`);
    const jobs = raw.jobs ?? [];
    return {
      jobs,
      has_more: raw.has_more ?? false,
      total_count: raw.total_count ?? jobs.length,
      next_cursor: raw.next_cursor ?? raw.cursor ?? '',
      timestamp: raw.timestamp ?? '',
    };
  }

  // createJob maps to POST /api/v1/scheduler/jobs
  async createJob(req: CreateJobRequest): Promise<CreateJobResponse> {
    return this.fetchJSON<CreateJobResponse>('/api/v1/scheduler/jobs', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // deleteJob maps to DELETE /api/v1/scheduler/jobs/:id
  async deleteJob(id: number): Promise<DeleteJobResponse> {
    return this.fetchJSON<DeleteJobResponse>(`/api/v1/scheduler/jobs/${id}`, {
      method: 'DELETE',
    });
  }

  // triggerJob maps to POST /api/v1/scheduler/jobs/:id/trigger
  async triggerJob(id: number): Promise<TriggerJobResponse> {
    return this.fetchJSON<TriggerJobResponse>(`/api/v1/scheduler/jobs/${id}/trigger`, {
      method: 'POST',
    });
  }

  // pauseJob maps to POST /api/v1/scheduler/jobs/:id/pause
  async pauseJob(id: number): Promise<void> {
    await this.fetchJSON<void>(`/api/v1/scheduler/jobs/${id}/pause`, { method: 'POST' });
  }

  // resumeJob maps to POST /api/v1/scheduler/jobs/:id/resume
  async resumeJob(id: number): Promise<void> {
    await this.fetchJSON<void>(`/api/v1/scheduler/jobs/${id}/resume`, { method: 'POST' });
  }

  // updateJob maps to PUT /api/v1/scheduler/jobs/:id
  async updateJob(id: number, req: Partial<CreateJobRequest>): Promise<SchedulerJob> {
    return this.fetchJSON<SchedulerJob>(`/api/v1/scheduler/jobs/${id}`, {
      method: 'PUT',
      body: JSON.stringify(req),
    });
  }

  // getStatus maps to GET /api/v1/scheduler/status
  async getStatus(): Promise<SchedulerStatus> {
    return this.fetchJSON<SchedulerStatus>('/api/v1/scheduler/status');
  }
}

// RestProjectsClient implements ProjectsClient over the QuicBaaS REST project endpoints.
// All project routes live under /api/v1/projects.
class RestProjectsClient implements ProjectsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async fetchJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (this.config.token) {
      headers['Authorization'] = `Bearer ${this.config.token}`;
    }
    const resp = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: { ...headers, ...(init.headers as Record<string, string> ?? {}) },
    });
    if (!resp.ok) {
      const text = await resp.text().catch(() => resp.statusText);
      throw new APIError(resp.status, text);
    }
    return parseResponseBody<T>(resp);
  }

  // createProject maps to POST /api/v1/projects
  async createProject(req: CreateProjectRequest): Promise<CreateProjectResponse> {
    return this.fetchJSON<CreateProjectResponse>('/api/v1/projects', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // getProject maps to GET /api/v1/projects/:projectId?tenant_id=...
  async getProject(tenantId: string, projectId: string): Promise<GetProjectResponse> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<GetProjectResponse>(`/api/v1/projects/${encodeURIComponent(projectId)}?${query}`);
  }

  // updateProject maps to PUT /api/v1/projects/:projectId
  async updateProject(req: UpdateProjectRequest): Promise<UpdateProjectResponse> {
    return this.fetchJSON<UpdateProjectResponse>(
      `/api/v1/projects/${encodeURIComponent(req.project_id)}`,
      { method: 'PUT', body: JSON.stringify(req) },
    );
  }

  // deleteProject maps to DELETE /api/v1/projects/:projectId?tenant_id=...
  async deleteProject(tenantId: string, projectId: string): Promise<DeleteProjectResponse> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<DeleteProjectResponse>(
      `/api/v1/projects/${encodeURIComponent(projectId)}?${query}`,
      { method: 'DELETE' },
    );
  }

  // listProjects maps to GET /api/v1/projects?tenant_id=...
  async listProjects(tenantId: string): Promise<ListProjectsResponse> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<ListProjectsResponse>(`/api/v1/projects?${query}`);
  }

  // grantAgentRole maps to PUT /api/v1/projects/:projectId/permissions/:agentId
  async grantAgentRole(req: GrantAgentRoleRequest): Promise<GrantAgentRoleResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<GrantAgentRoleResponse>(
      `/api/v1/projects/${encodeURIComponent(req.project_id)}/permissions/${encodeURIComponent(req.agent_id)}?${query}`,
      { method: 'PUT', body: JSON.stringify({ role: req.role }) },
    );
  }

  // revokeAgentRole maps to DELETE /api/v1/projects/:projectId/permissions/:agentId
  async revokeAgentRole(req: RevokeAgentRoleRequest): Promise<RevokeAgentRoleResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<RevokeAgentRoleResponse>(
      `/api/v1/projects/${encodeURIComponent(req.project_id)}/permissions/${encodeURIComponent(req.agent_id)}?${query}`,
      { method: 'DELETE' },
    );
  }
}

// RestIAMClient implements IAMClient over the QuicBaaS REST IAM endpoints.
// Paths: /api/v1/tenants/{tenantID}/users|roles|policies and /api/v1/tenants
class RestIAMClient implements IAMClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  async listUsers(tenantID: string): Promise<ListUsersResponse> {
    // Server returns {limit, offset, users:[{id, email, name, tenant_id, created, updated, ...}]}.
    // The wire fields differ from the IAMUser SDK type:
    //   name      → username (display name; used as the human-readable identifier)
    //   created   → created_at
    //   updated   → updated_at
    //   role      → roles[] (some responses send a single "role" string)
    //   state     → maps to enabled (any state that is not "disabled" is treated as enabled)
    // Absent fields are normalised to safe defaults so the UI never renders undefined.
    const raw = await this.fetchJSON<{
      users?: Array<{
        id?: string;
        email?: string;
        name?: string;
        username?: string;
        tenant_id?: string;
        created?: string;
        created_at?: string;
        updated?: string;
        updated_at?: string;
        enabled?: boolean;
        state?: string;
        role?: string;
        roles?: string[];
        groups?: string[];
      }>;
      count?: number;
      limit?: number;
      offset?: number;
    }>(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users`);

    const users: IAMUser[] = (raw.users ?? []).map((u) => ({
      id: u.id ?? '',
      tenant_id: u.tenant_id ?? tenantID,
      // Prefer an explicit username field; fall back to the display name field.
      username: u.username ?? u.name ?? u.email ?? '',
      email: u.email ?? '',
      enabled: u.enabled ?? (u.state !== 'disabled'),
      roles: u.roles ?? (u.role ? [u.role] : []),
      groups: u.groups ?? [],
      created_at: u.created_at ?? u.created ?? '',
      updated_at: u.updated_at ?? u.updated ?? '',
    }));

    return { users, count: raw.count ?? users.length };
  }

  async createUser(tenantID: string, req: CreateUserRequest): Promise<CreateUserResponse> {
    return this.fetchJSON<CreateUserResponse>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/users`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  async deleteUser(tenantID: string, userID: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}`,
      { method: 'DELETE' },
    );
  }

  async listTenants(): Promise<ListTenantsResponse> {
    // Server returns {limit, offset, tenants:[{id, name, created, updated, ...}]}.
    // Wire fields differ from IAMTenant:
    //   created → created_at
    //   updated → updated_at
    const raw = await this.fetchJSON<{
      tenants?: Array<{
        id?: string;
        name?: string;
        description?: string;
        created?: string;
        created_at?: string;
        updated?: string;
        updated_at?: string;
      }>;
      count?: number;
      limit?: number;
      offset?: number;
    }>('/api/v1/tenants');

    const tenants: IAMTenant[] = (raw.tenants ?? []).map((t) => ({
      id: t.id ?? '',
      name: t.name ?? '',
      description: t.description ?? '',
      created_at: t.created_at ?? t.created ?? '',
      updated_at: t.updated_at ?? t.updated ?? '',
    }));

    return { tenants, count: raw.count ?? tenants.length };
  }

  async createTenant(req: CreateTenantRequest): Promise<CreateTenantResponse> {
    // The server's POST /api/v1/tenants validates an `id` field as the
    // immutable tenant identifier. The SDK surface keeps a single `name`
    // (the human-friendly label) so callers don't have to think about both.
    // We derive a kebab-case slug from `name` for the wire `id` when the
    // caller doesn't specify one explicitly.
    const reqWithID = req as CreateTenantRequest & { id?: string };
    const id = reqWithID.id ?? req.name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    return this.fetchJSON<CreateTenantResponse>('/api/v1/tenants', {
      method: 'POST',
      body: JSON.stringify({ id, name: req.name, description: req.description }),
    });
  }

  async updateUser(tenantID: string, userID: string, req: import('./interfaces.js').UpdateUserRequest): Promise<IAMUser> {
    return this.fetchJSON<IAMUser>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}`,
      { method: 'PUT', body: JSON.stringify(req) },
    );
  }

  async assignRoleToUser(tenantID: string, userID: string, role: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}/roles`,
      // Wire field is "role_id" — the handler decodes this and passes it directly
      // to IdentityStore.AssignRole which keys on the role UUID, not the name.
      { method: 'POST', body: JSON.stringify({ role_id: role }) },
    );
  }

  async revokeRoleFromUser(tenantID: string, userID: string, role: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}/roles/${encodeURIComponent(role)}`,
      { method: 'DELETE' },
    );
  }

  async updateTenant(tenantID: string, req: import('./interfaces.js').UpdateTenantRequest): Promise<IAMTenant> {
    return this.fetchJSON<IAMTenant>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}`,
      { method: 'PUT', body: JSON.stringify(req) },
    );
  }

  async deleteTenant(tenantID: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}`,
      { method: 'DELETE' },
    );
  }

  async listRoles(tenantID: string): Promise<ListRolesResponse> {
    // Server returns {limit, offset, roles:[...]} — count field absent; derive it.
    const raw = await this.fetchJSON<{ roles?: IAMRole[]; count?: number; limit?: number; offset?: number }>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/roles`,
    );
    const roles = raw.roles ?? [];
    return { roles, count: raw.count ?? roles.length };
  }

  async createRole(tenantID: string, req: import('./interfaces.js').CreateRoleRequest): Promise<IAMRole> {
    return this.fetchJSON<IAMRole>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/roles`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  async updateRole(tenantID: string, roleID: string, req: import('./interfaces.js').UpdateRoleRequest): Promise<IAMRole> {
    return this.fetchJSON<IAMRole>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/roles/${encodeURIComponent(roleID)}`,
      { method: 'PUT', body: JSON.stringify(req) },
    );
  }

  async deleteRole(tenantID: string, roleID: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/roles/${encodeURIComponent(roleID)}`,
      { method: 'DELETE' },
    );
  }

  async listPolicies(tenantID: string): Promise<ListPoliciesResponse> {
    // Server returns {limit, offset, policies:[...]} — count field absent; derive it.
    const raw = await this.fetchJSON<{ policies?: IAMPolicy[]; count?: number; limit?: number; offset?: number }>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/policies`,
    );
    const policies = raw.policies ?? [];
    return { policies, count: raw.count ?? policies.length };
  }

  async createPolicy(tenantID: string, req: import('./interfaces.js').CreatePolicyRequest): Promise<IAMPolicy> {
    return this.fetchJSON<IAMPolicy>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/policies`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  async updatePolicy(tenantID: string, policyName: string, req: import('./interfaces.js').UpdatePolicyRequest): Promise<IAMPolicy> {
    return this.fetchJSON<IAMPolicy>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/${encodeURIComponent(policyName)}`,
      { method: 'PUT', body: JSON.stringify(req) },
    );
  }

  async deletePolicy(tenantID: string, policyName: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/${encodeURIComponent(policyName)}`,
      { method: 'DELETE' },
    );
  }

  async getInvite(token: string): Promise<import('./types.js').InviteMetadata> {
    return this.fetchJSON<import('./types.js').InviteMetadata>(
      `/api/v1/iam/invites/${encodeURIComponent(token)}`,
      { method: 'GET' },
    );
  }

  async activateFIDO2Begin(token: string): Promise<Record<string, unknown>> {
    return this.fetchJSON<Record<string, unknown>>(
      `/api/v1/iam/invites/${encodeURIComponent(token)}/activate/fido2/begin`,
      { method: 'POST' },
    );
  }

  async activateFIDO2Finish(token: string, credential: Record<string, unknown>): Promise<import('./types.js').ActivationResponse> {
    return this.fetchJSON<import('./types.js').ActivationResponse>(
      `/api/v1/iam/invites/${encodeURIComponent(token)}/activate/fido2/finish`,
      { method: 'POST', body: JSON.stringify({ credential }) },
    );
  }

  async activatePIV(token: string, certPEM: string): Promise<import('./types.js').ActivationResponse> {
    return this.fetchJSON<import('./types.js').ActivationResponse>(
      `/api/v1/iam/invites/${encodeURIComponent(token)}/activate/piv`,
      { method: 'POST', body: JSON.stringify({ cert_pem: certPEM }) },
    );
  }
}

// RestKMSClient implements KMSClient over the QuicBaaS REST KMS endpoints.
// All endpoints are under /api/v1/kms.
class RestKMSClient implements KMSClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  async listKeys(limit = 100): Promise<ListKeysResponse> {
    // Server returns {keys: [{key_id, version, algorithm, status, created_at}]}.
    // Map key_id → both id and name (UI uses .name primarily, .id as fallback)
    // and translate the unix-second created_at timestamp to an ISO string for
    // consistent rendering across views.
    const raw = await this.fetchJSON<{
      keys?: Array<{
        key_id?: string;
        id?: string;
        name?: string;
        description?: string;
        algorithm?: string;
        key_size?: number;
        version?: number;
        status?: KMSKeyStatus;
        created_at?: string | number;
        updated_at?: string | number;
        exportable?: boolean;
        tags?: Record<string, string>;
      }>;
      count?: number;
    }>(`/api/v1/kms/keys?limit=${limit}`);
    const toISO = (t: string | number | undefined): string => {
      if (typeof t === 'string') return t;
      if (typeof t === 'number') {
        // Skip Go zero-time sentinel (-62135596800 = year 1)
        if (t < 0 || !Number.isFinite(t)) return '';
        return new Date(t * 1000).toISOString();
      }
      return '';
    };
    const keys: KMSKey[] = (raw.keys ?? []).map((k) => ({
      id: k.id ?? k.key_id ?? '',
      name: k.name ?? k.key_id ?? '',
      description: k.description ?? '',
      algorithm: k.algorithm ?? '',
      key_size: k.key_size ?? 0,
      status: k.status ?? ('enabled' as KMSKeyStatus),
      created_at: toISO(k.created_at),
      updated_at: toISO(k.updated_at),
      exportable: k.exportable ?? false,
      tags: k.tags ?? {},
    }));
    return { keys, count: raw.count ?? keys.length };
  }

  async createKey(req: CreateKeyRequest): Promise<CreateKeyResponse> {
    return this.fetchJSON<CreateKeyResponse>('/api/v1/kms/keys', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async rotateKey(name: string): Promise<RotateKeyResponse> {
    return this.fetchJSON<RotateKeyResponse>(
      `/api/v1/kms/keys/${encodeURIComponent(name)}/rotate`,
      { method: 'POST' },
    );
  }

  async deleteKey(name: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/kms/keys/${encodeURIComponent(name)}`,
      { method: 'DELETE' },
    );
  }

  async encrypt(req: KMSEncryptRequest): Promise<KMSEncryptResponse> {
    return this.fetchJSON<KMSEncryptResponse>(
      `/api/v1/kms/encrypt/${encodeURIComponent(req.key_id)}`,
      { method: 'POST', body: JSON.stringify({ plaintext: req.plaintext }) },
    );
  }

  async decrypt(req: KMSDecryptRequest): Promise<KMSDecryptResponse> {
    return this.fetchJSON<KMSDecryptResponse>(
      `/api/v1/kms/decrypt/${encodeURIComponent(req.key_id)}`,
      { method: 'POST', body: JSON.stringify({ ciphertext: req.ciphertext }) },
    );
  }

  async listKeyVersions(name: string): Promise<import('./interfaces.js').KMSKeyVersion[]> {
    // Server returns created_at / destroyed_at as Unix-second int64 values.
    // Convert to ISO strings so callers can pass them directly to new Date().
    const toISO = (t: string | number | undefined): string | undefined => {
      if (t === undefined || t === null) return undefined;
      if (typeof t === 'string') return t || undefined;
      if (typeof t === 'number') {
        if (t <= 0 || !Number.isFinite(t)) return undefined;
        return new Date(t * 1000).toISOString();
      }
      return undefined;
    };
    const raw = await this.fetchJSON<{
      versions?: Array<{
        version?: number;
        status?: string;
        created_at?: string | number;
        destroyed_at?: string | number;
      }>;
    }>(`/api/v1/kms/keys/${encodeURIComponent(name)}/versions`);
    return (raw.versions ?? []).map((v) => ({
      version: v.version ?? 0,
      status: v.status ?? '',
      created_at: toISO(v.created_at),
      destroyed_at: toISO(v.destroyed_at),
    }));
  }

  async updateKeyStatus(name: string, status: 'enabled' | 'disabled'): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/kms/keys/${encodeURIComponent(name)}/status`,
      { method: 'PUT', body: JSON.stringify({ status }) },
    );
  }

  // setDefaultKey marks the named key as the system default.
  async setDefaultKey(name: string): Promise<SetDefaultKeyResponse> {
    return this.fetchJSON<SetDefaultKeyResponse>(
      `/api/v1/kms/keys/${encodeURIComponent(name)}/default`,
      { method: 'PUT' },
    );
  }
}

// RestSecretsClient implements SecretsClient over the QuicBaaS REST secrets endpoints.
// All endpoints are under /api/v1/secrets.
class RestSecretsClient implements SecretsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchJSON<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${path}`;
    const auth = await this.authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText when body is not parseable.
      }
      throw new APIError(resp.status, message);
    }
    return parseResponseBody<T>(resp);
  }

  async listSecrets(limit = 100): Promise<ListSecretsResponse> {
    // Server returns {secrets:[{secret_id, name, current_version, created_at,
    // updated_at}], has_more, timestamp, total_count}. The SDK normalises this
    // into {secrets:[{path, version, created_at, updated_at}], count}: callers
    // (notably the SecretsView) bind to .path and .version, so the wire-shape
    // mismatch silently produces blank rows when not adapted.
    interface WireSecret {
      secret_id?: string;
      name?: string;
      path?: string;
      current_version?: number;
      version?: number;
      created_at?: string;
      updated_at?: string;
    }
    interface WireResp {
      secrets?: WireSecret[];
      count?: number;
      total_count?: number;
      has_more?: boolean;
    }
    const raw = await this.fetchJSON<WireResp>(`/api/v1/secrets?limit=${limit}`);
    const wire = raw.secrets ?? [];
    const secrets: SecretEntry[] = wire.map((w) => ({
      path: w.path ?? w.secret_id ?? w.name ?? '',
      version: w.version ?? w.current_version ?? 0,
      created_at: w.created_at ?? '',
      updated_at: w.updated_at ?? '',
    }));
    return { secrets, count: raw.count ?? raw.total_count ?? secrets.length };
  }

  async storeSecret(req: StoreSecretRequest): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/secrets/${encodeURIComponent(req.path)}`,
      { method: 'POST', body: JSON.stringify({ data: req.data }) },
    );
  }

  async getSecret(path: string): Promise<GetSecretResponse> {
    const raw = await this.fetchJSON<{
      secret_id?: string;
      path?: string;
      data?: string | Record<string, unknown>;
      version?: number;
      current_version?: number;
      created_at?: string;
      updated_at?: string;
    }>(
      `/api/v1/secrets/${encodeURIComponent(path)}`,
    );
    let data: Record<string, unknown> = {};
    if (typeof raw.data === 'string') {
      try {
        const decoded = atob(raw.data);
        const parsed = JSON.parse(decoded) as unknown;
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          data = parsed as Record<string, unknown>;
        } else {
          data = { value: parsed };
        }
      } catch {
        data = { value: raw.data };
      }
    } else if (raw.data && typeof raw.data === 'object') {
      data = raw.data;
    }
    return {
      path: raw.path ?? raw.secret_id ?? path,
      data,
      version: raw.version ?? raw.current_version ?? 0,
      created_at: raw.created_at ?? '',
      updated_at: raw.updated_at ?? '',
    };
  }

  async deleteSecret(path: string): Promise<void> {
    await this.fetchJSON<void>(
      `/api/v1/secrets/${encodeURIComponent(path)}`,
      { method: 'DELETE' },
    );
  }

  async rotateSecret(path: string): Promise<RotateSecretResponse> {
    return this.fetchJSON<RotateSecretResponse>(
      `/api/v1/secrets/${encodeURIComponent(path)}?action=rotate`,
      { method: 'POST' },
    );
  }

  // listVersions returns all versions of a secret at the given path.
  async listVersions(path: string): Promise<ListSecretVersionsResponse> {
    const raw = await this.fetchJSON<{ versions?: ListSecretVersionsResponse['versions']; count?: number }>(
      `/api/v1/secrets-versions/${encodeURIComponent(path)}`,
    );
    return { versions: raw.versions ?? [], count: raw.count ?? 0 };
  }
}

// ---------------------------------------------------------------------------
// Shared HTTP helper for barrier/ceremony/bootstrap clients.
// All three sub-clients share the same auth header injection and JSON
// envelope handling. Centralising the helper avoids duplicating ~30 lines
// per client.
// ---------------------------------------------------------------------------

// fetchHelper hides the auth + envelope plumbing behind a small surface so
// each client can focus on routing. Empty server bodies (204 No Content)
// resolve to undefined — callers expecting void receive the empty payload.
type FetchInit = RequestInit;

interface FetchHelper {
  fetchJSON<T>(path: string, init?: FetchInit): Promise<T>;
  fetchVoid(path: string, init?: FetchInit): Promise<void>;
}

function newFetchHelper(baseUrl: string, config: ClientConfig): FetchHelper {
  const trimmed = baseUrl.replace(/\/$/, '');

  const authHeaders = async (): Promise<Record<string, string>> => {
    const headers: Record<string, string> = {};
    const tok = await resolveToken(config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    if (config.tenantID) headers['X-Tenant-ID'] = config.tenantID;
    return headers;
  };

  const send = async (path: string, init?: FetchInit): Promise<Response> => {
    const url = `${trimmed}${path}`;
    const auth = await authHeaders();
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Body not JSON: fall back to statusText.
      }
      throw new APIError(resp.status, message);
    }
    return resp;
  };

  return {
    async fetchJSON<T>(path: string, init?: FetchInit): Promise<T> {
      const resp = await send(path, init);
      return parseResponseBody<T>(resp);
    },
    async fetchVoid(path: string, init?: FetchInit): Promise<void> {
      await send(path, init);
    },
  };
}

// ---------------------------------------------------------------------------
// RestBarrierClient — canonical /api/v1/barrier surface.
// ---------------------------------------------------------------------------

// RestBarrierClient implements BarrierClient by routing to the QuicBaaS
// barrier REST endpoints. The shape mirrors go-qrdb's BarrierClient surface.
export class RestBarrierClient implements BarrierClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  async status(): Promise<BarrierStatusResponse> {
    return this.http.fetchJSON<BarrierStatusResponse>('/api/v1/barrier/status');
  }

  async initialize(req: BarrierInitRequest): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/initialize', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async unseal(req: BarrierUnsealRequest): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/unseal', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async seal(): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/seal', { method: 'POST' });
  }

  async initializeShamir(req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse> {
    return this.http.fetchJSON<BarrierShamirInitResponse>('/api/v1/barrier/shamir/initialize', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async unsealWithShare(req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse> {
    return this.http.fetchJSON<BarrierUnsealShareResponse>(
      '/api/v1/barrier/shamir/unseal-share',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
    );
  }

  async unsealWithShares(req: BarrierUnsealSharesRequest): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/shamir/unseal-shares', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async shamirListShares(): Promise<BarrierShamirSharesResponse> {
    return this.http.fetchJSON<BarrierShamirSharesResponse>('/api/v1/barrier/shamir/shares');
  }

  async shamirDeleteShare(req: BarrierShamirDeleteShareRequest): Promise<void> {
    await this.http.fetchVoid(`/api/v1/barrier/shamir/shares/${encodeURIComponent(String(req.index))}`, {
      method: 'DELETE',
    });
  }

  async shamirDeleteAllShares(): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/shamir/shares', { method: 'DELETE' });
  }

  async shamirVerify(): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/shamir/verify', { method: 'POST' });
  }

  async rekey(req: BarrierRekeyRequest): Promise<BarrierRekeyResponse> {
    return this.http.fetchJSON<BarrierRekeyResponse>('/api/v1/barrier/rekey', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async generateRecoveryKeys(
    req: BarrierGenerateRecoveryKeysRequest,
  ): Promise<BarrierRecoveryKeysResponse> {
    return this.http.fetchJSON<BarrierRecoveryKeysResponse>('/api/v1/barrier/recovery/generate', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async recoverWithKeys(req: BarrierRecoverWithKeysRequest): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/recovery/recover', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async deleteRecoveryKeys(): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/recovery', { method: 'DELETE' });
  }

  async recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse> {
    return this.http.fetchJSON<BarrierHasRecoveryKeysResponse>(
      '/api/v1/barrier/recovery/status',
    );
  }

  async generateRootToken(req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse> {
    return this.http.fetchJSON<BarrierRootTokenResponse>('/api/v1/barrier/root-token', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }
}

// ---------------------------------------------------------------------------
// RestTenantBarrierClient — /api/v1/tenants/{id}/barrier surface.
// ---------------------------------------------------------------------------

// RestTenantBarrierClient implements TenantBarrierClient by routing to the
// broker's per-tenant barrier REST endpoints. Each call is scoped to a tenant
// ID; the surface is distinct from RestBarrierClient, which drives the
// process-wide system barrier.
export class RestTenantBarrierClient implements TenantBarrierClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  private base(tenantId: string): string {
    return `/api/v1/tenants/${encodeURIComponent(tenantId)}/barrier`;
  }

  async tenantBarrierInit(
    tenantId: string,
    req: TenantBarrierInitRequest,
  ): Promise<TenantBarrierInitResponse | undefined> {
    // The shamir strategy returns 200 with the minted shares; the none and
    // passphrase strategies return 204 No Content. fetchJSON yields undefined
    // on a 204, so the union return type already covers both cases.
    return this.http.fetchJSON<TenantBarrierInitResponse | undefined>(
      `${this.base(tenantId)}/init`,
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
    );
  }

  async tenantBarrierUnseal(tenantId: string, key: string): Promise<void> {
    await this.http.fetchVoid(`${this.base(tenantId)}/unseal`, {
      method: 'POST',
      body: JSON.stringify({ key }),
    });
  }

  async tenantBarrierUnsealShare(
    tenantId: string,
    share: string,
  ): Promise<TenantBarrierUnsealShareResponse> {
    return this.http.fetchJSON<TenantBarrierUnsealShareResponse>(
      `${this.base(tenantId)}/unseal-share`,
      {
        method: 'POST',
        body: JSON.stringify({ share }),
      },
    );
  }

  async tenantBarrierSeal(tenantId: string): Promise<void> {
    await this.http.fetchVoid(`${this.base(tenantId)}/seal`, { method: 'POST' });
  }

  async tenantBarrierStatus(tenantId: string): Promise<TenantBarrierStatusResponse> {
    return this.http.fetchJSON<TenantBarrierStatusResponse>(`${this.base(tenantId)}/status`);
  }
}

// ---------------------------------------------------------------------------
// RestCeremonyClient — /api/v1/init surface.
// ---------------------------------------------------------------------------

// RestCeremonyClient implements CeremonyClient against the QuicBaaS
// ceremony REST endpoints (mounted under /api/v1/init).
export class RestCeremonyClient implements CeremonyClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  async initialize(req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse> {
    return this.http.fetchJSON<CeremonyInitializeResponse>('/api/v1/init/initialize', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async status(): Promise<CeremonyStatusResponse> {
    return this.http.fetchJSON<CeremonyStatusResponse>('/api/v1/init/status');
  }

  async claimCertBegin(
    req: CeremonyClaimCertBeginRequest,
  ): Promise<CeremonyClaimCertBeginResponse> {
    return this.http.fetchJSON<CeremonyClaimCertBeginResponse>(
      '/api/v1/init/claim-cert/begin',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
    );
  }

  async claimCertComplete(
    req: CeremonyClaimCertCompleteRequest,
  ): Promise<CeremonyClaimCertCompleteResponse> {
    return this.http.fetchJSON<CeremonyClaimCertCompleteResponse>(
      '/api/v1/init/claim-cert/complete',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
    );
  }

  async claimShare(req: CeremonyClaimShareRequest): Promise<CeremonyClaimShareResponse> {
    return this.http.fetchJSON<CeremonyClaimShareResponse>('/api/v1/init/claim-share', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async signCSR(req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse> {
    return this.http.fetchJSON<CeremonySignCSRResponse>('/api/v1/init/sign-csr', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async enrollExternalOfficer(
    req: CeremonyEnrollExternalOfficerRequest,
  ): Promise<CeremonyEnrollExternalOfficerResponse> {
    return this.http.fetchJSON<CeremonyEnrollExternalOfficerResponse>(
      '/api/v1/init/enroll-external-officer',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
    );
  }
}

// ---------------------------------------------------------------------------
// RestClusterClient — /api/v1/cluster surface.
// ---------------------------------------------------------------------------

// RestClusterClient implements ClusterClient against the QuicBaaS Raft cluster
// REST endpoints. status() issues an authenticated GET so the bearer token is
// attached via the shared FetchHelper (the same auth path every other
// authenticated client uses) — a raw fetch() would omit the token and 401.
export class RestClusterClient implements ClusterClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  async status(): Promise<ClusterStatusResponse> {
    return this.http.fetchJSON<ClusterStatusResponse>('/api/v1/cluster/status', {
      method: 'GET',
    });
  }
}

// ---------------------------------------------------------------------------
// RestBootstrapClient — /api/v1/bootstrap surface.
// ---------------------------------------------------------------------------

// RestBootstrapClient implements BootstrapClient against the QuicBaaS
// trust bootstrap REST endpoints. Each method is a thin wrapper around the
// matching POST /api/v1/bootstrap/{method} route. A nil request fetches
// the full CA bundle without filters.
export class RestBootstrapClient implements BootstrapClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  private async fetchMethod(
    method: BootstrapMethod,
    req?: BootstrapBundleRequest,
  ): Promise<BootstrapBundleResponse> {
    const body = req ? JSON.stringify(req) : '{}';
    return this.http.fetchJSON<BootstrapBundleResponse>(`/api/v1/bootstrap/${method}`, {
      method: 'POST',
      body,
    });
  }

  async auto(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> {
    return this.fetchMethod('auto', req);
  }

  async dane(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> {
    return this.fetchMethod('dane', req);
  }

  async noise(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> {
    return this.fetchMethod('noise', req);
  }

  async spki(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse> {
    return this.fetchMethod('spki', req);
  }
}

// ---------------------------------------------------------------------------
// RestSealClient — deprecated alias preserved for one release.
//
// This class warns once per construction and delegates directly to the new
// RestBarrierClient. The legacy /api/v1/seal endpoints remain available on
// the daemon (with Deprecation/Sunset headers) so existing callers that
// instantiate RestSealClient out-of-band continue to work, but new code
// should migrate to client.barrier directly.
// ---------------------------------------------------------------------------

let sealDeprecationWarned = false;

// emitSealDeprecationWarning prints a single console warning describing the
// rename. Repeated constructions are no-ops to avoid log spam.
function emitSealDeprecationWarning(): void {
  if (sealDeprecationWarned) {
    return;
  }
  sealDeprecationWarned = true;
  // eslint-disable-next-line no-console
  console.warn(
    'SealClient is deprecated, use BarrierClient (client.barrier). ' +
      'The Seal* surface will be removed one release after the unification ships.',
  );
}

class RestSealClient implements SealClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    emitSealDeprecationWarning();
    this.http = newFetchHelper(baseUrl, config);
  }

  async status(): Promise<SealStatus> {
    return this.http.fetchJSON<SealStatus>('/api/v1/seal/status');
  }

  async unseal(req: UnsealRequest): Promise<UnsealResponse> {
    return this.http.fetchJSON<UnsealResponse>('/api/v1/seal/unseal', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async seal(): Promise<void> {
    await this.http.fetchVoid('/api/v1/seal/seal', { method: 'POST' });
  }

  async rekey(req: RekeyRequest): Promise<RekeyResponse> {
    return this.http.fetchJSON<RekeyResponse>('/api/v1/seal/rekey', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async rotateRootKey(): Promise<RotateRootKeyResponse> {
    return this.http.fetchJSON<RotateRootKeyResponse>('/api/v1/seal/rotate-root', {
      method: 'POST',
    });
  }
}

// =============================================================================
// SSH error helpers
// =============================================================================

// sshErrorBody is the shape of error envelopes returned by the SSH REST handlers.
interface sshErrorBody {
  error?: string;
  resource?: string;
  host?: string;
  expected?: string;
  received?: string;
}

// throwSSHError maps an HTTP response onto a typed SSH error and throws it.
// The caller must ensure resp.ok is false before calling this.
async function throwSSHError(resp: Response): Promise<never> {
  let body: sshErrorBody = {};
  try {
    body = (await resp.json()) as sshErrorBody;
  } catch {
    // Fall through with empty body — use statusText.
  }
  const message = body.error ?? resp.statusText;
  const resource = body.resource;

  switch (resp.status) {
    case 404:
      throw new SSHNotFoundError(message, resource);
    case 400:
      throw new SSHInvalidError(message, resource);
    case 503:
      throw new SSHUnavailableError(message);
    case 409:
      // Distinguish host-key mismatch (has expected/received) from generic conflict.
      if (body.host !== undefined && body.expected !== undefined && body.received !== undefined) {
        throw new SSHHostMismatchError(body.host, body.expected, body.received, message);
      }
      throw new SSHDuplicateError(message, resource);
    default:
      if (resp.status >= 500) {
        throw new SSHServerError(resp.status, message, resource);
      }
      throw new SSHError(resp.status, message, resource);
  }
}

// =============================================================================
// RestSSHKeysClient
// =============================================================================

// RestSSHKeysClient implements SSHKeysClient using the QuicBaaS REST API.
// Legacy flat methods hit /api/v1/ssh/keys with server-side scope/owner defaults.
// Scoped methods use the full /api/v1/ssh/keys/{scope}/{owner}/{name} surface.
export class RestSSHKeysClient implements SSHKeysClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const token = await resolveToken(this.config.token);
    if (token) headers.Authorization = `Bearer ${token}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchSSH<T>(path: string, init?: RequestInit): Promise<T> {
    const auth = await this.authHeaders();
    const resp = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      await throwSSHError(resp);
    }
    return parseResponseBody<T>(resp);
  }

  // list returns SSH keys using server-side scope/owner defaults.
  // Backward-compatible: returns {keys: SSHKey[]} for Wails callers.
  // The new REST surface returns {items: SSHKey[]} — both fields are populated.
  async list(): Promise<ListSSHKeysResponse> {
    try {
      const raw = await this.fetchSSH<{ items?: SSHKey[] }>('/api/v1/ssh/keys');
      return { items: raw.items ?? [], keys: raw.items ?? [] };
    } catch {
      // Silent fallback preserves old behaviour for Wails/Svelte list views.
      return { keys: [], items: [] };
    }
  }

  // create generates a new SSH key and returns its metadata.
  async create(req: CreateSSHKeyRequest): Promise<SSHKey> {
    return this.fetchSSH<SSHKey>('/api/v1/ssh/keys', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // get fetches a single key. For backward compat, uses the first
  // matching key from the list when scope/owner are not provided.
  async get(name: string): Promise<SSHKey> {
    // The new surface requires scope/owner/name. Without them we query
    // the list and find by name, matching the legacy flat behaviour.
    const resp = await this.fetchSSH<{ items?: SSHKey[] }>('/api/v1/ssh/keys');
    const found = (resp.items ?? []).find((k) => k.name === name);
    if (!found) {
      throw new SSHNotFoundError(`ssh key not found: ${name}`);
    }
    return found;
  }

  // remove deletes a key. Without scope/owner, mirrors the legacy behaviour
  // by finding the key via list first, then deleting by triple.
  async remove(name: string): Promise<void> {
    const key = await this.get(name);
    const scope = key.scope ?? 'user';
    const owner = key.owner ?? '';
    await this.fetchSSH<void>(
      `/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`,
      { method: 'DELETE' },
    );
  }

  // listScoped enumerates keys filtered by scope/owner.
  async listScoped(query?: ListSSHKeysQuery): Promise<ListSSHKeysResponse> {
    const params = new URLSearchParams();
    if (query?.scope) params.set('scope', query.scope);
    if (query?.owner) params.set('owner', query.owner);
    const qs = params.toString() ? `?${params.toString()}` : '';
    const raw = await this.fetchSSH<{ items?: SSHKey[] }>(`/api/v1/ssh/keys${qs}`);
    return { items: raw.items ?? [], keys: raw.items ?? [] };
  }

  // getScoped fetches a single key by the full scope/owner/name triple.
  async getScoped(scope: SSHScope, owner: string, name: string): Promise<SSHKey> {
    return this.fetchSSH<SSHKey>(
      `/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`,
    );
  }

  // deleteScoped removes a key by the full scope/owner/name triple.
  async deleteScoped(scope: SSHScope, owner: string, name: string): Promise<void> {
    await this.fetchSSH<void>(
      `/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`,
      { method: 'DELETE' },
    );
  }

  // exportPublic returns the authorized_keys-format public key for the given key.
  async exportPublic(scope: SSHScope, owner: string, name: string): Promise<SSHPublicKey> {
    return this.fetchSSH<SSHPublicKey>(
      `/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/public`,
    );
  }

  // sign signs a raw digest with the named key.
  async sign(scope: SSHScope, owner: string, name: string, req: SignRequest): Promise<SignResponse> {
    return this.fetchSSH<SignResponse>(
      `/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/sign`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  // import_ imports a PEM-encoded private key. Trailing underscore avoids
  // the reserved word 'import'.
  async import_(req: ImportSSHKeyRequest): Promise<SSHKey> {
    return this.fetchSSH<SSHKey>('/api/v1/ssh/keys/import', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }
}

// =============================================================================
// RestKnownHostsClient
// =============================================================================

// RestKnownHostsClient implements SSHKnownHostsClient over the QuicBaaS REST API.
// Paths: /api/v1/ssh/known-hosts
export class RestKnownHostsClient implements SSHKnownHostsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const token = await resolveToken(this.config.token);
    if (token) headers.Authorization = `Bearer ${token}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchSSH<T>(path: string, init?: RequestInit): Promise<T> {
    const auth = await this.authHeaders();
    const resp = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      await throwSSHError(resp);
    }
    return parseResponseBody<T>(resp);
  }

  // add pins a host-key fingerprint.
  async add(req: AddSSHPinRequest): Promise<void> {
    await this.fetchSSH<void>('/api/v1/ssh/known-hosts', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // list enumerates pinned host keys, optionally filtered by scope/owner.
  async list(query?: ListSSHKeysQuery): Promise<ListSSHPinsResponse> {
    const params = new URLSearchParams();
    if (query?.scope) params.set('scope', query.scope);
    if (query?.owner) params.set('owner', query.owner);
    const qs = params.toString() ? `?${params.toString()}` : '';
    return this.fetchSSH<ListSSHPinsResponse>(`/api/v1/ssh/known-hosts${qs}`);
  }

  // remove removes a pinned host key.
  async remove(host: string, query?: ListSSHKeysQuery): Promise<void> {
    const params = new URLSearchParams();
    if (query?.scope) params.set('scope', query.scope);
    if (query?.owner) params.set('owner', query.owner);
    const qs = params.toString() ? `?${params.toString()}` : '';
    await this.fetchSSH<void>(
      `/api/v1/ssh/known-hosts/${encodeURIComponent(host)}${qs}`,
      { method: 'DELETE' },
    );
  }

  // verify checks the presented fingerprint against the stored pin.
  // Throws SSHHostMismatchError on mismatch, SSHNotFoundError when not pinned.
  async verify(host: string, req: VerifySSHPinRequest): Promise<void> {
    await this.fetchSSH<void>(
      `/api/v1/ssh/known-hosts/${encodeURIComponent(host)}/verify`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }
}

// =============================================================================
// RestAuthorizedKeysClient
// =============================================================================

// RestAuthorizedKeysClient implements SSHAuthorizedKeysClient over the QuicBaaS REST API.
// Paths: /api/v1/ssh/authorized
export class RestAuthorizedKeysClient implements SSHAuthorizedKeysClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async authHeaders(): Promise<Record<string, string>> {
    const headers: Record<string, string> = {};
    const token = await resolveToken(this.config.token);
    if (token) headers.Authorization = `Bearer ${token}`;
    if (this.config.tenantID) headers['X-Tenant-ID'] = this.config.tenantID;
    return headers;
  }

  private async fetchSSH<T>(path: string, init?: RequestInit): Promise<T> {
    const auth = await this.authHeaders();
    const resp = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...auth,
        ...(init?.headers ?? {}),
      },
    });
    if (!resp.ok) {
      await throwSSHError(resp);
    }
    return parseResponseBody<T>(resp);
  }

  // emit formats a public key into an authorized_keys line.
  async emit(req: EmitAuthorizedRequest): Promise<EmitAuthorizedResponse> {
    return this.fetchSSH<EmitAuthorizedResponse>('/api/v1/ssh/authorized', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // verify checks whether a fingerprint matches an authorized entry.
  // Returns the matching SSHAuthorizedEntry on success.
  async verify(req: VerifyAuthorizedRequest): Promise<VerifyAuthorizedResponse> {
    const entry = await this.fetchSSH<SSHAuthorizedEntry>('/api/v1/ssh/authorized/verify', {
      method: 'POST',
      body: JSON.stringify(req),
    });
    return { entry };
  }
}

// =============================================================================
// RestSSHClient — top-level SSH namespace
// =============================================================================

// RestSSHClient groups the three SSH sub-clients behind a single property.
// Exposed as client.ssh on RestClient.
class RestSSHClient implements SSHClient {
  readonly keys: RestSSHKeysClient;
  readonly knownHosts: RestKnownHostsClient;
  readonly authorized: RestAuthorizedKeysClient;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.keys = new RestSSHKeysClient(baseUrl, config);
    this.knownHosts = new RestKnownHostsClient(baseUrl, config);
    this.authorized = new RestAuthorizedKeysClient(baseUrl, config);
  }
}

// readErrorMessage attempts to extract a structured error message
// from the response body, falling back to the HTTP status text.
async function readErrorMessage(resp: Response): Promise<string> {
  try {
    const data = (await resp.json()) as { error?: string; message?: string };
    return data.error ?? data.message ?? resp.statusText;
  } catch {
    return resp.statusText;
  }
}

// RestClient is the top-level Client implementation for REST / browser mode.
// It composes all sub-clients and shares the base URL and config between them.
export class RestClient implements Client {
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
  readonly iam: IAMClient;
  readonly kms: KMSClient;
  readonly secrets: SecretsClient;
  readonly barrier: BarrierClient;
  readonly tenantBarrier: TenantBarrierClient;
  readonly ceremony: CeremonyClient;
  readonly bootstrap: BootstrapClient;
  readonly cluster: ClusterClient;
  /** @deprecated Use barrier. The legacy seal accessor is kept for one release. */
  readonly seal!: SealClient;
  /**
   * sshKeys is the legacy backward-compat accessor.
   * Points to the same RestSSHKeysClient instance as ssh.keys.
   */
  readonly sshKeys: SSHKeysClient;
  /**
   * ssh exposes the full scoped SSH surface (keys, knownHosts, authorized).
   * client.ssh.keys is the same instance as client.sshKeys.
   */
  readonly ssh: SSHClient;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.connection = new RestConnectionClient(baseUrl);
    this.orchestrator = new RestOrchestratorClient(baseUrl, config);
    this.auth = new RestAuthClient(baseUrl);
    this.data = new RestDataClient(baseUrl, config);
    this.vectors = new RestVectorsClient(baseUrl, config);
    this.timeseries = new RestTimeSeriesClient(baseUrl, config);
    this.objects = new RestObjectsClient(baseUrl, config);
    this.rag = new RestRAGClient(baseUrl, config);
    this.embeddings = new RestEmbeddingsClient(baseUrl, config);
    this.messaging = new RestMessagingClient(baseUrl, config);
    this.scheduler = new RestSchedulerClient(baseUrl, config);
    this.projects = new RestProjectsClient(baseUrl, config);
    this.iam = new RestIAMClient(baseUrl, config);
    this.kms = new RestKMSClient(baseUrl, config);
    this.secrets = new RestSecretsClient(baseUrl, config);
    this.barrier = new RestBarrierClient(baseUrl, config);
    this.tenantBarrier = new RestTenantBarrierClient(baseUrl, config);
    this.ceremony = new RestCeremonyClient(baseUrl, config);
    this.bootstrap = new RestBootstrapClient(baseUrl, config);
    this.cluster = new RestClusterClient(baseUrl, config);
    // RestSealClient is a deprecated alias retained for one release. Construction
    // is lazy via a getter so apps that never touch `client.seal` (which is the
    // expected case after the unification) don't pay for the deprecation
    // warning on every client instantiation. New code must use client.barrier;
    // this accessor will be removed one release post-cutover.
    let _seal: SealClient | null = null;
    Object.defineProperty(this, 'seal', {
      get: (): SealClient => {
        if (_seal === null) _seal = new RestSealClient(baseUrl, config);
        return _seal;
      },
      enumerable: true,
    });
    // Build the scoped SSH client group first, then alias sshKeys to ssh.keys
    // so both accessors point at the same RestSSHKeysClient instance.
    this.ssh = new RestSSHClient(baseUrl, config);
    this.sshKeys = this.ssh.keys;
  }
}

// Re-export errors used in this module so callers can catch typed errors
// without importing from the errors module directly.
export {
  APIError,
  ConnectionError,
  NotConnectedError,
  SSHError,
  SSHNotFoundError,
  SSHDuplicateError,
  SSHInvalidError,
  SSHUnavailableError,
  SSHServerError,
  SSHHostMismatchError,
};

// Re-export SessionInfo for any callers referencing it through this module.
export type { SessionInfo };
