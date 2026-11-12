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
  RAGDocumentID,
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
  RegisterNodeRequest,
  PlacementNodeInfo,
  PlacementRequest,
  PlaceResult,
  Placement,
  PlacementFilter,
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
  GrantFleetNodeRoleRequest,
  GrantFleetNodeRoleResponse,
  RevokeFleetNodeRoleRequest,
  RevokeFleetNodeRoleResponse,
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
import type {
  CAClient,
  IssueCertificateRequest,
  IssueCertificateResponse,
  SignCSRRequest,
  SignCSRResponse,
  RevokeCertificateResponse,
  VerifyCertificateResponse,
  GetCACertificateResponse,
  CAInfoResponse,
  CABundleResponse,
  CertificateSummary,
  ListCertificatesRequest,
  ListCertificatesResponse,
  GenerateCRLResponse,
  IsRevokedResponse,
} from './ca.js';
import type {
  TrustClient,
  CrossSignedRootPoolResponse,
  AutoImportAIARequest,
  AutoImportAIAResponse,
  PublishDANERequest,
  PublishDANEResponse,
} from './trust.js';
import type {
  CryptoClient,
  CryptoEncryptRequest,
  CryptoEncryptResponse,
  CryptoDecryptRequest,
  CryptoDecryptResponse,
  CryptoRewrapRequest,
  CryptoRewrapResponse,
  CryptoSignRequest,
  CryptoSignResponse,
  CryptoVerifyRequest,
  CryptoVerifyResponse,
  CryptoHashRequest,
  CryptoHashResponse,
  CryptoRandomResponse,
  CryptoDataKeyRequest,
  CryptoDataKeyResponse,
} from './crypto.js';
import type {
  DEKClient,
  CreateDEKRequest,
  CreateDEKResponse,
  GetDEKResponse,
  ListDEKsRequest,
  ListDEKsResponse,
  RotateDEKRequest,
  RotateDEKResponse,
  DeleteDEKRequest,
  DeleteDEKResponse,
  GetDEKStatusResponse,
  SetDefaultDEKResponse,
  SetDEKSafetyRequest,
  SetDEKSafetyResponse,
  ClearDEKSafetyResponse,
  StartReencryptionRequest,
  StartReencryptionResponse,
  GetReencryptionProgressResponse,
} from './dek.js';
import type {
  RegistryClient,
  RegistryServiceInstance,
  RegistryRegisterRequest,
  RegistryLeaseResponse,
  RegistryResolveResponse,
} from './registry.js';
import type {
  DynamicSecretsClient,
  DynamicSecretRole,
  CreateRoleRequest as DynamicSecretCreateRoleRequest,
  ListRolesResponse as ListDynamicSecretRolesResponse,
  DeleteRoleResponse as DeleteDynamicSecretRoleResponse,
  GenerateCredentialRequest,
  GenerateCredentialResponse,
  RenewLeaseRequest,
  RenewLeaseResponse,
  RevokeLeaseResponse as RevokeDynamicSecretLeaseResponse,
  ListLeasesResponse,
} from './dynamicsecrets.js';
import type {
  AccountsClient,
  Account,
  CreateAccountRequest,
  CreateAccountResponse,
  GetAccountResponse,
  ListAccountsRequest,
  ListAccountsResponse,
  UpdateAccountRequest,
  UpdateAccountResponse,
  DeleteAccountResponse,
  RotateAccountCredentialRequest,
  RotateAccountCredentialResponse,
  RevokeAccountRequest,
  RevokeAccountResponse,
  SetDefaultAccountResponse,
  GetAccountUsageResponse,
} from './accounts.js';
import type { AgentsClient, Agent, AgentPool, AgentUsage } from './agents.js';
import { parseJSONPreservingLargeIntegers } from './json.js';
import type {
  AgentBackendInfo,
  JobRunnerInfo,
  AuthTokenResponse,
  CreateKeyRequest,
  CreateKeyResponse,
  CreateTenantRequest,
  CreateTenantResponse,
  CreateUserRequest,
  CreateUserResponse,
  DeployFleetNodeResult,
  DeployFleetNodeSpec,
  InferenceRequest,
  InferenceResponse,
  InferenceChunk,
  FleetNode,
  ClientConfig,
  CredentialAssertionJSON,
  PublicKeyCredentialJSON,
  WebAuthnLoginChallenge,
  CloseSessionRequest,
  CloseSessionResponse,
  ClaimTaskRequest,
  CompleteTaskRequest,
  DeleteTaskRequest,
  DeleteTaskResponse,
  CreateTaskRequest,
  CreateTaskResponse,
  CreateWorkflowRequest,
  CreateWorkflowResponse,
  DecomposeTaskRequest,
  DecomposeTaskResponse,
  GetFleetNodeRequest,
  GetSecretResponse,
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
  ListFleetNodesRequest,
  ListFleetNodesResponse,
  ListKeysResponse,
  ListPoliciesResponse,
  ListRolesResponse,
  ListSecretsResponse,
  ListSessionsRequest,
  ListSessionsResponse,
  SessionStatusResponse,
  ListTenantsResponse,
  ListUsersResponse,
  ListWorkflowsResponse,
  PromptSessionRequest,
  PromptSessionResponse,
  ReconcileRolesResponse,
  RegisterFleetNodeRequest,
  RegisterFleetNodeResponse,
  RemoveFleetNodeRequest,
  DisableFleetNodeRequest,
  DisableFleetNodeResponse,
  EnableFleetNodeRequest,
  EnableFleetNodeResponse,
  RekeyRequest,
  RekeyResponse,
  RoleReconcileResult,
  RotateKeyResponse,
  RotateRootKeyResponse,
  RotateSecretResponse,
  RotateSessionAccountRequest,
  RotateSessionAccountResponse,
  ListSecretVersionsResponse,
  SetDefaultKeyResponse,
  SealInitRequest,
  SealInitResponse,
  SealStatus,
  BarrierStatusResponse,
  BarrierInitRequest,
  BarrierUnsealRequest,
  BarrierSealRequest,
  BarrierShamirInitRequest,
  BarrierShamirInitResponse,
  BarrierUnsealShareRequest,
  BarrierUnsealShareResponse,
  BarrierUnsealSharesRequest,
  BarrierRekeyRequest,
  BarrierRekeyResponse,
  BarrierGenerateRecoveryKeysRequest,
  BarrierRecoveryKeysResponse,
  BarrierRecoverWithKeysRequest,
  BarrierHasRecoveryKeysResponse,
  BarrierGenerateRootTokenRequest,
  BarrierEnableEncryptionRequest,
  BarrierEnableEncryptionResponse,
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
  CeremonyClaimShareBeginRequest,
  CeremonyClaimShareBeginResponse,
  CeremonyClaimShareCompleteRequest,
  CeremonyClaimShareCompleteResponse,
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
  AttentionItem,
  AttentionResolveRequest,
} from './types.js';
import {
  adoptPage,
  appendCSVParam,
  appendListParam,
  appendPageParams,
  type AccountPageParams,
  type FleetNodePageParams,
  type AttentionPageParams,
  type JobPageParams,
  type PageParams,
  type PageResult,
  type ProjectPageParams,
  type SessionPageParams,
  type TaskPageParams,
  type WorkflowPageParams,
} from './page.js';
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
import { requireTLSForSOPIN } from './so_pin.js';

// API base path for all orchestrator endpoints.
const API_PREFIX = '/api/v1/orchestrator';

// FLEET_PREFIX is the fleet registry's own REST surface, outside the
// orchestrator prefix.
const FLEET_PREFIX = '/api/v1/fleet';

// WORKER_IMAGE_PATH is the worker image refresh's route: a host verb of the
// SDLC module, served beside the orchestrator surface.
const WORKER_IMAGE_PATH = '/api/v1/sdlc/worker-image';

// sleep resolves after `ms` milliseconds. Used only by the fetch-based SSE
// reconnect loops (subscribeJobEvents) to back off between attempts.
function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

// SSE_RECONNECT_DELAY_MS mirrors the browser's own default EventSource retry
// delay, for the fetch-based SSE stream(s) that replace it.
const SSE_RECONNECT_DELAY_MS = 3000;

// resolveToken resolves a bearer token from either a static string or an
// async factory function. Returns an empty string when no token is configured.
export async function resolveToken(
  token: string | (() => Promise<string>) | undefined,
): Promise<string> {
  if (!token) return '';
  if (typeof token === 'function') return token();
  return token;
}

// utf8Bytes encodes a string to its UTF-8 byte sequence.
function utf8Bytes(s: string): Uint8Array {
  return new TextEncoder().encode(s);
}

// bytesToBase64 encodes bytes as standard base64 — the wire form Go []byte
// fields decode from JSON strings.
function bytesToBase64(bytes: Uint8Array): string {
  let binary = '';
  for (let i = 0; i < bytes.length; i++) {
    binary += String.fromCharCode(bytes[i]);
  }
  return btoa(binary);
}

// parseResponseBody reads the body of a 2xx fetch Response and returns the
// parsed JSON, tolerating 204 No Content and empty bodies (returns undefined
// as T). All fetchJSON and fetchSSH success tails delegate here to prevent
// "Unexpected end of JSON input" when servers return 204 or a body-less 2xx.
// The parse keeps 64-bit identities exact: an id past 2^53 arrives as its
// decimal string, while timestamps and other quantities stay numbers (see
// json.ts for the key rule).
async function parseResponseBody<T>(resp: Response): Promise<T> {
  if (resp.status === 204) return undefined as T;
  const text = await resp.text();
  return (text ? parseJSONPreservingLargeIntegers<T>(text) : (undefined as T));
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

// =============================================================================
// The session shape the server writes, and the one the SDK promises
// =============================================================================
//
// THESE ARE NOT THE SAME SHAPE, and the SDK owes the translation. The service
// writes `orchestration.SessionInfo` verbatim (pkg/api/rest/handlers_orchestrator.go
// serves it with RespondJSON), which names the lifecycle field `state` and the
// activity instant `last_activity`. Every SDK — this one included — exposes
// them as `status` and `last_active`; the Go client does the rename in
// SessionStatusWire.Response() (sdk/go/client/orchestrator/session_wire.go)
// and the Wails binding does it in OrchestratorBinding.ListSessions.
//
// This client did not, and returned the raw body under a type that claimed
// otherwise. So `session.last_active` was `undefined` for every one of the
// tenant's sessions and the Last Active column rendered an em-dash forever —
// while the identical Svelte component, driven through Wails, rendered it
// correctly. A type that lies is worse than a missing one: nothing could
// detect the drift, because the compiler had been told the field was there
// (go-quicbaas#855).

// SessionWire is the session object as the REST service writes it.
interface SessionWire {
  session_id: string;
  project_id?: string;
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
  /** The runner of the session's job — what ROLE the session played. */
  runner?: string;
}

// sessionFromWire renames the two fields the service and the SDK disagree
// about. It mirrors SessionStatusWire.Response() in the Go client, field for
// field, so the two SDKs hand a caller the same object for the same session.
//
// A field the service does not send stays ABSENT rather than becoming an
// empty string or a zero instant: "the server did not say" and "the server
// said nothing was there" are different answers, and the views render them
// differently.
function sessionFromWire(wire: SessionWire): SessionStatusResponse {
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
  // The ROLE the session played, resolved server-side for the page. It was
  // dropped here while the wire carried it, which left the Role badge blank
  // on every row (go-quicbaas#969).
  if (wire.runner !== undefined) session.runner = wire.runner;
  return session;
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
    return this.fetchAPI<T>(`${API_PREFIX}${path}`, init);
  }

  private async fetchAllListPages<T>(path: string, itemsKey: string): Promise<T[]> {
    const items: T[] = [];
    let offset = 0;
    for (;;) {
      const params = new URLSearchParams();
      params.set('limit', '100');
      if (offset > 0) params.set('offset', String(offset));
      const pagePath = `${path}${path.includes('?') ? '&' : '?'}${params.toString()}`;
      const raw = await this.fetchJSON<unknown>(pagePath);
      const page = adoptPage<T>(raw, itemsKey);
      items.push(...page.items);
      if (page.next_offset === undefined) return items;
      if (page.next_offset <= page.offset) {
        throw new Error(`${itemsKey} list next_offset did not advance`);
      }
      offset = page.next_offset;
    }
  }

  // fetchAPI is fetchJSON for a path outside the orchestrator prefix — the
  // worker image refresh lives under /api/v1/sdlc and the fleet registry
  // under /api/v1/fleet.
  private async fetchAPI<T>(apiPath: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${apiPath}`;
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

  // =========================================================================
  // The paged listings
  //
  // Each one sends the four shared parameters plus its endpoint's own
  // narrowings, and hands back the envelope the server answered with. The
  // unpaged methods below are unchanged and still useful — they are just no
  // longer the whole collection, because the server pages every listing and
  // defaults to 25 records. See docs/software-factory/api.md.
  // =========================================================================

  // listJobsPage maps to GET /api/v1/orchestrator/jobs with limit/offset/
  // sort/q and the jobs listing's own filters, all applied by the SERVER.
  async listJobsPage(tenantID: string, page?: JobPageParams): Promise<PageResult<JobStatus>> {
    const params = new URLSearchParams({ tenant_id: tenantID });
    appendListParam(params, 'task_id', page?.task_id);
    appendListParam(params, 'runner', page?.runner);
    appendListParam(params, 'project_id', page?.project_id);
    appendListParam(params, 'agent_id', page?.agent_id);
    appendCSVParam(params, 'statuses', page?.statuses);
    appendCSVParam(params, 'job_ids', page?.job_ids);
    appendPageParams(params, page);
    const raw = await this.fetchJSON<unknown>(`/jobs?${params.toString()}`);
    return adoptPage<JobStatus>(raw, 'jobs');
  }

  // listTasksPage maps to GET /api/v1/orchestrator/tasks with the shared
  // paging parameters and the tasks listing's own filters.
  async listTasksPage(tenantID: string, page?: TaskPageParams): Promise<PageResult<Task>> {
    const params = new URLSearchParams({ tenant_id: tenantID });
    appendListParam(params, 'status', page?.status);
    appendListParam(params, 'project_id', page?.project_id);
    appendListParam(params, 'team_name', page?.team_name);
    appendListParam(params, 'assigned_to', page?.assigned_to);
    appendListParam(params, 'created_by', page?.created_by);
    appendListParam(params, 'auto_spawn', page?.auto_spawn);
    appendListParam(params, 'agent_id', page?.agent_id);
    appendListParam(params, 'job_id', page?.job_id);
    appendListParam(params, 'source_kind', page?.source_kind);
    appendListParam(params, 'source_ref', page?.source_ref);
    appendCSVParam(params, 'statuses', page?.statuses);
    appendPageParams(params, page);
    const raw = await this.fetchJSON<unknown>(`/tasks?${params.toString()}`);
    return adoptPage<Task>(raw, 'tasks');
  }

  // listSessionsPage maps to GET /api/v1/orchestrator/sessions. The records
  // are renamed through sessionFromWire, exactly as listSessions does, so a
  // paged caller and an unpaged one hold the same session object.
  async listSessionsPage(
    tenantID: string,
    page?: SessionPageParams,
  ): Promise<PageResult<SessionStatusResponse>> {
    if (!tenantID) {
      throw new Error('listSessionsPage: tenantID is required');
    }
    const params = new URLSearchParams({ tenant_id: tenantID });
    appendListParam(params, 'backend', page?.backend);
    appendListParam(params, 'status', page?.status);
    appendListParam(params, 'project_id', page?.project_id);
    appendListParam(params, 'job_id', page?.job_id);
    appendListParam(params, 'runner', page?.runner);
    appendListParam(params, 'agent_id', page?.agent_id);
    appendPageParams(params, page);
    const raw = await this.fetchJSON<unknown>(`/sessions?${params.toString()}`);
    return adoptPage<SessionStatusResponse>(raw, 'sessions', sessionFromWire);
  }

  // listFleetNodesPage maps to GET /api/v1/fleet with the shared
  // paging parameters and the fleet listing's own filters.
  async listFleetNodesPage(
    tenantID: string,
    page?: FleetNodePageParams,
  ): Promise<PageResult<FleetNode>> {
    const params = new URLSearchParams({ tenant_id: tenantID });
    appendListParam(params, 'host', page?.host);
    appendListParam(params, 'status', page?.status);
    appendListParam(params, 'node_type', page?.node_type);
    appendListParam(params, 'backend', page?.backend);
    appendListParam(params, 'model', page?.model);
    appendPageParams(params, page);
    const raw = await this.fetchAPI<unknown>(`${FLEET_PREFIX}?${params.toString()}`);
    return adoptPage<FleetNode>(raw, 'nodes');
  }

  // listWorkflowsPage maps to GET /api/v1/orchestrator/workflows with the
  // shared paging parameters and the workflow listing's own filters.
  async listWorkflowsPage(
    tenantID: string,
    page?: WorkflowPageParams,
  ): Promise<PageResult<WorkflowDefinition>> {
    const params = new URLSearchParams({ tenant_id: tenantID });
    appendListParam(params, 'project_id', page?.project_id);
    appendListParam(params, 'mode', page?.mode);
    appendPageParams(params, page);
    const raw = await this.fetchJSON<unknown>(`/workflows?${params.toString()}`);
    return adoptPage<WorkflowDefinition>(raw, 'workflows');
  }

  // listJobs maps to GET /api/v1/orchestrator/jobs?tenant_id=<tenantID>
  // with an optional &task_id=<taskID>, which the SERVER applies.
  // Server returns {jobs: JobStatus[] | null}; unwrap to bare array.
  async listJobs(tenantID: string, taskID?: string): Promise<JobStatus[]> {
    let path = `/jobs?tenant_id=${encodeURIComponent(tenantID)}`;
    if (taskID) path += `&task_id=${encodeURIComponent(taskID)}`;
    const raw = await this.fetchJSON<{ jobs?: JobStatus[] | null } | JobStatus[]>(path);
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

  // listFleetNodes maps to GET /api/v1/fleet with query parameters.
  async listFleetNodes(req: ListFleetNodesRequest): Promise<ListFleetNodesResponse> {
    const params = new URLSearchParams({ tenant_id: req.tenant_id });
    if (req.host) params.set('host', req.host);
    if (req.status) params.set('status', req.status);
    if (req.node_type) params.set('node_type', req.node_type);
    if (req.backend) params.set('backend', req.backend);
    if (req.model) params.set('model', req.model);
    return this.fetchAPI<ListFleetNodesResponse>(`${FLEET_PREFIX}?${params.toString()}`);
  }

  // getFleetNode maps to GET /api/v1/fleet/:nodeID?tenant_id=<tenantID>
  async getFleetNode(req: GetFleetNodeRequest): Promise<FleetNode> {
    return this.fetchAPI<FleetNode>(
      `${FLEET_PREFIX}/${encodeURIComponent(req.node_id)}?tenant_id=${encodeURIComponent(req.tenant_id)}`,
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
    const raw = await this.fetchJSON<{ sessions?: SessionWire[]; count?: number }>(
      `/sessions?${params.toString()}`,
    );
    const sessions = (raw.sessions ?? []).map(sessionFromWire);
    return {
      sessions,
      count: raw.count ?? sessions.length,
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
    return this.fetchAllListPages<AgentBackendInfo>('/backends', 'backends');
  }

  // listRunners maps to GET /api/v1/orchestrator/runners
  // The REST endpoint returns {"runners": [...]}; unwrap to match the Go SDK
  // contract, which exposes a bare slice.
  async listRunners(): Promise<JobRunnerInfo[]> {
    return this.fetchAllListPages<JobRunnerInfo>('/runners', 'runners');
  }

  // listBackendModels maps to
  // GET /api/v1/fleet/{nodeID}/backends/{backendName}/models
  // Returns the model names available on the named backend as seen from the
  // specified fleet node. Pass "any" as nodeID to query without host affinity.
  async listBackendModels(nodeID: string, backendName: string): Promise<string[]> {
    const nid = encodeURIComponent(nodeID || 'any');
    const bn = encodeURIComponent(backendName);
    const resp = await this.fetchAPI<{ models?: string[] }>(`${FLEET_PREFIX}/${nid}/backends/${bn}/models`);
    return resp.models ?? [];
  }

  // healthCheck maps to GET /api/v1/orchestrator/health
  async healthCheck(): Promise<HealthCheckReport> {
    return this.fetchJSON<HealthCheckReport>('/health');
  }

  // subscribeJobEvents opens a Server-Sent Events stream for a running job.
  // Maps to GET /api/v1/orchestrator/jobs/:jobID/events?tenant_id=<tenantID>
  // Returns a cleanup function that stops the stream and any pending
  // reconnect.
  //
  // Native EventSource is deliberately NOT used here, for the same reason
  // subscribeSession does its own fetch-based SSE parsing: it cannot carry a
  // custom Authorization header, so a bearer-token session has no way to
  // authenticate the connection at all. Every request this stream sends
  // therefore arrived at the daemon with no Authorization header and was
  // refused — indistinguishable, from the browser's side, from an expired or
  // rejected session, which is what made "renewed but rejected" reproduce for
  // an operator with running jobs open (their dashboard held an open job-
  // events stream) and NOT for one without any (go-quicbaas UI-login-refused
  // investigation, 2026-09-25). The bearer is resolved fresh on every
  // (re)connect attempt, exactly like every other authenticated call on this
  // client, so a token rotated mid-stream is picked up on the next reconnect
  // rather than the stream being pinned to whatever was valid when it first
  // opened.
  subscribeJobEvents(
    tenantID: string,
    jobID: string,
    onEvent: (event: JobEvent) => void,
    onError?: () => void,
  ): () => void {
    const url = [
      this.baseUrl,
      API_PREFIX,
      `/jobs/${encodeURIComponent(jobID)}/events`,
      `?tenant_id=${encodeURIComponent(tenantID)}`,
    ].join('');

    const controller = new AbortController();
    let closedByCaller = false;

    const run = async (): Promise<void> => {
      while (!closedByCaller) {
        let resp: Response;
        try {
          const auth = await this.authHeaders();
          resp = await fetch(url, {
            headers: { Accept: 'text/event-stream', ...auth },
            signal: controller.signal,
          });
        } catch {
          // Aborted by the caller, or a network failure. A network failure is
          // a transient drop — the same case EventSource reconnects from on
          // its own — so back off and retry rather than giving up.
          if (closedByCaller) return;
          await sleep(SSE_RECONNECT_DELAY_MS);
          continue;
        }

        if (!resp.ok || !resp.body) {
          // A refusal (403) or a response that is not an event stream at all
          // is not transient — reported once, never retried, matching
          // EventSource's CLOSED/give-up semantics.
          if (!closedByCaller) onError?.();
          return;
        }

        const reader = resp.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        let currentData = '';
        try {
          while (true) {
            const { value, done } = await reader.read();
            if (done) break;
            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop() ?? '';
            for (const line of lines) {
              if (line.startsWith('data: ')) {
                currentData = line.slice(6);
              } else if (line === '' && currentData !== '') {
                try {
                  onEvent(JSON.parse(currentData) as JobEvent);
                } catch {
                  // Discard unparseable frames; the server may send
                  // keep-alive comments.
                }
                currentData = '';
              }
            }
          }
        } catch {
          // Aborted mid-read, or a stream read error — fall through to the
          // reconnect decision below either way.
        } finally {
          reader.cancel().catch(() => { /* ignore cancel errors on teardown */ });
        }

        if (closedByCaller) return;
        // The stream ended — the server closed it, or the connection dropped
        // mid-read. Reconnect the same way EventSource does for a transient
        // drop, rather than treating every disconnect as a give-up.
        await sleep(SSE_RECONNECT_DELAY_MS);
      }
    };

    void run();

    return () => {
      closedByCaller = true;
      controller.abort();
    };
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

  // rotateSessionAccount maps to
  // POST /api/v1/orchestrator/sessions/:sessionID/rotate-account. The path
  // segment is authoritative on the server, so session_id is dropped from the
  // body rather than sent twice.
  async rotateSessionAccount(req: RotateSessionAccountRequest): Promise<RotateSessionAccountResponse> {
    if (!req.session_id) {
      throw new Error('rotateSessionAccount: session_id is required');
    }
    const { session_id, ...body } = req;
    return this.fetchJSON<RotateSessionAccountResponse>(
      `/sessions/${encodeURIComponent(session_id)}/rotate-account`,
      { method: 'POST', body: JSON.stringify(body) },
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

  // readmitTask maps to POST /api/v1/orchestrator/tasks/:taskID/readmit
  async readmitTask(tenantID: string, taskID: string): Promise<void> {
    await this.fetchJSON<void>(
      `/tasks/${encodeURIComponent(taskID)}/readmit?tenant_id=${encodeURIComponent(tenantID)}`,
      { method: 'POST' },
    );
  }

  // deleteTask maps to DELETE /api/v1/orchestrator/tasks/:taskID
  async deleteTask(req: DeleteTaskRequest): Promise<DeleteTaskResponse> {
    return this.fetchJSON<DeleteTaskResponse>(
      `/tasks/${encodeURIComponent(req.task_id)}?tenant_id=${encodeURIComponent(req.tenant_id)}`,
      { method: 'DELETE' },
    );
  }

  // listAttention maps to GET /api/v1/orchestrator/attention with the
  // shared paging parameters and the inbox's own project/kind narrowings.
  async listAttention(tenantID: string, page?: AttentionPageParams): Promise<PageResult<AttentionItem>> {
    const params = new URLSearchParams({ tenant_id: tenantID });
    appendListParam(params, 'project_id', page?.project_id);
    appendListParam(params, 'kind', page?.kind);
    appendPageParams(params, page);
    const raw = await this.fetchJSON<unknown>(`/attention?${params.toString()}`);
    return adoptPage<AttentionItem>(raw, 'items');
  }

  // resolveAttention maps to POST /api/v1/orchestrator/attention/:taskID/resolve
  async resolveAttention(req: AttentionResolveRequest): Promise<AttentionItem> {
    return this.fetchJSON<AttentionItem>(
      `/attention/${encodeURIComponent(req.task_id)}/resolve?tenant_id=${encodeURIComponent(req.tenant_id)}`,
      { method: 'POST', body: JSON.stringify({ action: req.action, answer: req.answer }) },
    );
  }

  // decomposeTask maps to POST /api/v1/orchestrator/tasks/:taskID/decompose
  async decomposeTask(req: DecomposeTaskRequest): Promise<DecomposeTaskResponse> {
    return this.fetchJSON<DecomposeTaskResponse>(
      `/tasks/${encodeURIComponent(req.task_id)}/decompose?tenant_id=${encodeURIComponent(req.tenant_id)}`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  // registerFleetNode maps to POST /api/v1/fleet
  async registerFleetNode(req: RegisterFleetNodeRequest): Promise<RegisterFleetNodeResponse> {
    return this.fetchAPI<RegisterFleetNodeResponse>(FLEET_PREFIX, {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // removeFleetNode maps to DELETE /api/v1/fleet/:nodeID?tenant_id=<tenantID>
  async removeFleetNode(req: RemoveFleetNodeRequest): Promise<void> {
    await this.fetchAPI<void>(
      `${FLEET_PREFIX}/${encodeURIComponent(req.node_id)}?tenant_id=${encodeURIComponent(req.tenant_id)}`,
      { method: 'DELETE' },
    );
  }

  // disableFleetNode maps to POST /api/v1/fleet/:nodeID/disable?tenant_id=<tenantID>
  async disableFleetNode(req: DisableFleetNodeRequest): Promise<DisableFleetNodeResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchAPI<DisableFleetNodeResponse>(
      `${FLEET_PREFIX}/${encodeURIComponent(req.node_id)}/disable?${query}`,
      { method: 'POST' },
    );
  }

  // enableFleetNode maps to POST /api/v1/fleet/:nodeID/enable?tenant_id=<tenantID>
  async enableFleetNode(req: EnableFleetNodeRequest): Promise<EnableFleetNodeResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchAPI<EnableFleetNodeResponse>(
      `${FLEET_PREFIX}/${encodeURIComponent(req.node_id)}/enable?${query}`,
      { method: 'POST' },
    );
  }

  // deployFleetNode is a desktop-only operation. REST callers must use the
  // CLI (`quicbaas fleet deploy`) or the desktop GUI.
  async deployFleetNode(_spec: DeployFleetNodeSpec): Promise<DeployFleetNodeResult> {
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

  // getSessionConversation maps to
  // GET /api/v1/orchestrator/sessions/:sessionID/history, the paged
  // conversation view.
  //
  // The route says "history" and the method says "conversation" because the
  // SDK already has a getSessionHistory that pages the raw EVENT log: the
  // path names the resource an operator reaches for, the method names the
  // shape it returns.
  async getSessionConversation(req: GetSessionConversationRequest): Promise<GetSessionConversationResponse> {
    const params = new URLSearchParams();
    if (req.tenant_id) params.set('tenant_id', req.tenant_id);
    if (req.cursor && req.cursor > 0) params.set('cursor', String(req.cursor));
    if (req.limit && req.limit > 0) params.set('limit', String(req.limit));
    const query = params.toString();
    return this.fetchJSON<GetSessionConversationResponse>(
      `/sessions/${encodeURIComponent(req.session_id)}/history${query ? `?${query}` : ''}`,
    );
  }

  // getSpendReport maps to GET /api/v1/orchestrator/cost.
  //
  // ONE ROUTE, SCOPED BY QUERY, because the ledger's attribution chain is a
  // PATH: a project's spend and a job's spend are the same prefix scan at two
  // depths, and four routes over one scan would be four places for the
  // drill-down's arithmetic to stop agreeing with itself.
  // getWorkerImageStatus maps to GET /api/v1/sdlc/worker-image.
  async getWorkerImageStatus(): Promise<WorkerImageStatus> {
    return this.fetchAPI<WorkerImageStatus>(WORKER_IMAGE_PATH);
  }

  // refreshWorkerImage maps to POST /api/v1/sdlc/worker-image/refresh, which
  // answers 202 with the status the request left behind, 409 while a pass is
  // running or queued, 503 on a node without the refresh loop.
  async refreshWorkerImage(): Promise<WorkerImageStatus> {
    return this.fetchAPI<WorkerImageStatus>(`${WORKER_IMAGE_PATH}/refresh`, { method: 'POST' });
  }

  async getSpendReport(req: GetSpendReportRequest): Promise<GetSpendReportResponse> {
    const params = new URLSearchParams();
    if (req.tenant_id) params.set('tenant_id', req.tenant_id);
    if (req.project_id) params.set('project_id', req.project_id);
    if (req.task_id) params.set('task_id', req.task_id);
    if (req.job_id) params.set('job_id', req.job_id);
    if (req.session_id) params.set('session_id', req.session_id);
    // Presence, not value: round 0 is a real level, so an unset round_set
    // must leave the parameter off entirely rather than send '0'.
    if (req.round_set) params.set('round', String(req.round ?? 0));
    if (req.from) params.set('from', req.from);
    if (req.to) params.set('to', req.to);
    if (req.host) params.set('host', req.host);
    if (req.agent) params.set('agent', req.agent);
    const query = params.toString();
    return this.fetchJSON<GetSpendReportResponse>(`/cost${query ? `?${query}` : ''}`);
  }

  // getTenantSpendPolicy maps to GET /api/v1/orchestrator/spend-policy.
  async getTenantSpendPolicy(req: GetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<TenantSpendPolicy>(`/spend-policy?${query}`);
  }

  // setTenantSpendPolicy maps to PUT /api/v1/orchestrator/spend-policy.
  async setTenantSpendPolicy(req: SetTenantSpendPolicyRequest): Promise<TenantSpendPolicy> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<TenantSpendPolicy>(`/spend-policy?${query}`, {
      method: 'PUT',
      body: JSON.stringify({ limits: req.limits }),
    });
  }

  // getCostSettings maps to GET /api/v1/orchestrator/cost-settings.
  async getCostSettings(req: GetCostSettingsRequest): Promise<CostSettings> {
    return this.fetchJSON<CostSettings>(`/cost-settings${costSettingsQuery(req.tenant_id)}`);
  }

  // setCostSettings maps to PUT /api/v1/orchestrator/cost-settings. The
  // settings are replaced: an absent rate or engine inherits the
  // configuration.
  async setCostSettings(req: SetCostSettingsRequest): Promise<CostSettings> {
    const body = {
      electricity_price_per_kwh: req.electricity_price_per_kwh,
      engine_power_watts: req.engine_power_watts,
    };
    return this.fetchJSON<CostSettings>(`/cost-settings${costSettingsQuery(req.tenant_id)}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    });
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

  // authValidate POSTs to the session-validate verb with a bearer token. When
  // no token is supplied, skip the network call entirely and return false —
  // the server requires the Authorization header and 400s without it, which is
  // not an error condition, it's just "unauthenticated". Returns false on
  // 400/401 (missing or invalid token); throws APIError on any other non-2xx
  // and on a 2xx whose body does not affirmatively answer the question.
  //
  // NOT /api/v1/auth/validate. On a composed node the xkms module owns the
  // /api/v1/auth/* session plane and registers GET (not POST) at that path, so
  // this POST matched no route at all — and go-qrdb's auth middleware wraps the
  // whole router, so an unregistered method+pattern has no module auth fallback
  // and fell through to mTLS, answering 401 "qrdb/auth: no_peer_cert" for a
  // bearer the daemon had just minted (go-quicbaas#256). quicbaas's own verb is
  // IAM-namespaced to clear that collision, the same move that put renewal at
  // /api/v1/iam/refresh (#221) and PIV login at /api/v1/iam/login/piv.
  //
  // A 2xx is NOT on its own a valid session: ValidateToken answers 200 with
  // {"valid":false} when the service rejects the token (pkg/api/rest/
  // iam_login.go), so reading only the status code would report a dead session
  // as live.
  //
  // Only `valid === true` — the literal boolean — authenticates. This is an
  // authentication boundary, so it fails CLOSED: every other shape is a
  // protocol violation and throws rather than resolving either way. The
  // tempting `body.valid !== false` reads almost the same and is not: it
  // authenticates `{}`, `{"valid":"false"}` (the string, which is truthy),
  // `{"valid":null}`, and a JSON `null` body, and pairing it with a catch that
  // returns true also authenticates an empty 204, an HTML error page from an
  // intermediary, and any truncated response. Each of those is something OTHER
  // than the server affirming this session, and none of them should be
  // indistinguishable from a "yes".
  async authValidate(token?: string): Promise<boolean> {
    if (!token) return false;

    const resp = await fetch(`${this.baseUrl}/api/v1/iam/validate`, {
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

    let body: unknown;
    try {
      body = await resp.json();
    } catch {
      throw new APIError(
        resp.status,
        'session validation returned an unreadable body; expected JSON with a boolean "valid"',
      );
    }

    if (typeof body !== 'object' || body === null) {
      throw new APIError(
        resp.status,
        'session validation returned a non-object body; expected JSON with a boolean "valid"',
      );
    }

    // Own property only. Plain destructuring would walk the prototype chain,
    // so a page that had suffered prototype pollution (Object.prototype.valid
    // = true) would turn a 200 {} into an authenticated session — the response
    // affirming nothing while the client reads a "yes" off the environment.
    if (!Object.prototype.hasOwnProperty.call(body, 'valid')) {
      throw new APIError(
        resp.status,
        'session validation response has no own "valid" field',
      );
    }
    const { valid } = body as { valid?: unknown };
    if (typeof valid !== 'boolean') {
      throw new APIError(
        resp.status,
        'session validation response has no boolean "valid" field',
      );
    }
    return valid;
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

  // webAuthnBeginLogin opens a unified-ceremony FIDO2 login and returns the
  // in-flight challenge (ceremony session + relying-party session + the
  // go-webauthn options envelope).
  // Maps to POST /api/v1/auth/login/begin (type=fido2). The server answers
  // every identity-resolution failure with a uniform 401 so callers cannot
  // enumerate tenants or users.
  async webAuthnBeginLogin(tenantID: string, email: string): Promise<WebAuthnLoginChallenge> {
    const resp = await fetch(`${this.baseUrl}/api/v1/auth/login/begin`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ type: 'fido2', hint: email, tenant_id: tenantID }),
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
    const begin = (await resp.json()) as {
      session_id: string;
      challenge_metadata?: Record<string, string>;
    };
    const optionsJSON = begin.challenge_metadata?.['options'];
    if (!optionsJSON) {
      throw new APIError(500, 'login/begin returned no WebAuthn challenge options');
    }
    return {
      session_id: begin.session_id,
      rp_session_id: begin.challenge_metadata?.['session_id'] ?? '',
      options: JSON.parse(optionsJSON) as CredentialAssertionJSON,
    };
  }

  // webAuthnFinishLogin submits the WebAuthn assertion for an in-flight
  // challenge and returns tokens (ceremony bearer + rotating refresh
  // credential, renewable via /api/v1/iam/refresh).
  // Maps to POST /api/v1/auth/login/finish (type=fido2). The proof wire
  // mirrors the Go CLI's proveCanonicalFIDO2: the lookup key is the raw
  // bytes of the base64url credential id, the signature carries the
  // assertion JSON, and the method metadata echoes the relying-party
  // session id and credential id.
  async webAuthnFinishLogin(
    challenge: WebAuthnLoginChallenge,
    credential: PublicKeyCredentialJSON,
  ): Promise<AuthTokenResponse> {
    const assertionJSON = JSON.stringify(credential);
    const resp = await fetch(`${this.baseUrl}/api/v1/auth/login/finish`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        session_id: challenge.session_id,
        type: 'fido2',
        // Go []byte fields decode standard-base64 JSON strings.
        lookup_key: bytesToBase64(utf8Bytes(credential.rawId)),
        signature: bytesToBase64(utf8Bytes(assertionJSON)),
        method_metadata: {
          session_id: challenge.rp_session_id,
          credential_id: credential.rawId,
        },
      }),
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
    const finish = (await resp.json()) as {
      bearer: string;
      expires_at?: string;
      refresh_token?: string;
      refresh_expires_at?: string;
    };
    let expiresIn = 0;
    if (finish.expires_at) {
      const deadline = Date.parse(finish.expires_at);
      if (!Number.isNaN(deadline)) {
        expiresIn = Math.max(0, Math.floor((deadline - Date.now()) / 1000));
      }
    }
    return {
      access_token: finish.bearer,
      refresh_token: finish.refresh_token ?? '',
      token_type: 'Bearer',
      expires_in: expiresIn,
    };
  }

  // loginPIV performs mTLS PIV smartcard login. credentials:'include' and
  // mode:'cors' ensure the browser presents the client certificate during
  // the TLS handshake so the server can read r.TLS.PeerCertificates[0].
  // Maps to POST /api/v1/iam/login/piv (P3.3 de-collision: moved off
  // /api/v1/auth/login/piv, which xkms's module now owns on a composed
  // node as part of its own session-login plane).
  async loginPIV(): Promise<AuthTokenResponse> {
    const resp = await fetch(`${this.baseUrl}/api/v1/iam/login/piv`, {
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

  // logout POSTs to the session-revoke verb to revoke the active session token
  // on the server. IAM-namespaced for the same reason authValidate is: xkms's
  // module owns POST /api/v1/auth/logout on a composed node and redeems against
  // its OWN token store, so a quicbaas-minted session sent there was never
  // actually revoked (go-quicbaas#256). Because this method swallows every
  // failure, that miss was silent — sign-out looked clean and the bearer stayed
  // live until its TTL. Caller must clear localStorage tokens after this
  // resolves;
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
      await fetch(`${this.baseUrl}/api/v1/iam/logout`, {
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
  async getDocument(id: RAGDocumentID): Promise<RAGDocument> {
    return this.fetchJSON<RAGDocument>(`/api/v1/rag/documents/${id}`);
  }

  // deleteDocument maps to DELETE /api/v1/rag/documents/{id}
  async deleteDocument(id: RAGDocumentID): Promise<void> {
    await this.fetchJSON<void>(`/api/v1/rag/documents/${id}`, { method: 'DELETE' });
  }

  // reindexDocument maps to POST /api/v1/rag/documents/{id}/reindex
  async reindexDocument(id: RAGDocumentID, req?: RAGReindexRequest): Promise<RAGDocument> {
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

  // registerNode maps to POST /api/v1/scheduler/nodes
  async registerNode(req: RegisterNodeRequest): Promise<PlacementNodeInfo> {
    return this.fetchJSON<PlacementNodeInfo>('/api/v1/scheduler/nodes', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // renewNode maps to POST /api/v1/scheduler/nodes/:id/renew
  async renewNode(nodeId: string): Promise<void> {
    await this.fetchJSON<unknown>(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}/renew`, { method: 'POST' });
  }

  // deregisterNode maps to DELETE /api/v1/scheduler/nodes/:id
  async deregisterNode(nodeId: string): Promise<void> {
    await this.fetchJSON<unknown>(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}`, { method: 'DELETE' });
  }

  // getNode maps to GET /api/v1/scheduler/nodes/:id
  async getNode(nodeId: string): Promise<PlacementNodeInfo> {
    return this.fetchJSON<PlacementNodeInfo>(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}`);
  }

  // listNodes maps to GET /api/v1/scheduler/nodes
  async listNodes(): Promise<PlacementNodeInfo[]> {
    const body = await this.fetchJSON<{ nodes: PlacementNodeInfo[] | null }>('/api/v1/scheduler/nodes');
    return body.nodes ?? [];
  }

  // cordonNode maps to POST /api/v1/scheduler/nodes/:id/cordon
  async cordonNode(nodeId: string): Promise<PlacementNodeInfo> {
    return this.fetchJSON<PlacementNodeInfo>(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}/cordon`, {
      method: 'POST',
    });
  }

  // uncordonNode maps to POST /api/v1/scheduler/nodes/:id/uncordon
  async uncordonNode(nodeId: string): Promise<PlacementNodeInfo> {
    return this.fetchJSON<PlacementNodeInfo>(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}/uncordon`, {
      method: 'POST',
    });
  }

  // drainNode maps to POST /api/v1/scheduler/nodes/:id/drain
  async drainNode(nodeId: string): Promise<Placement[]> {
    const body = await this.fetchJSON<{ placements: Placement[] | null }>(
      `/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}/drain`,
      { method: 'POST' },
    );
    return body.placements ?? [];
  }

  // place maps to POST /api/v1/scheduler/placements
  async place(req: PlacementRequest): Promise<PlaceResult> {
    return this.fetchJSON<PlaceResult>('/api/v1/scheduler/placements', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // release maps to DELETE /api/v1/scheduler/placements/:id
  async release(workloadId: string): Promise<void> {
    await this.fetchJSON<unknown>(`/api/v1/scheduler/placements/${encodeURIComponent(workloadId)}`, {
      method: 'DELETE',
    });
  }

  // getPlacement maps to GET /api/v1/scheduler/placements/:id
  async getPlacement(workloadId: string): Promise<Placement> {
    return this.fetchJSON<Placement>(`/api/v1/scheduler/placements/${encodeURIComponent(workloadId)}`);
  }

  // listPlacements maps to GET /api/v1/scheduler/placements
  async listPlacements(filter: PlacementFilter = {}): Promise<Placement[]> {
    const params = new URLSearchParams();
    if (filter.node_id) params.set('node_id', filter.node_id);
    if (filter.group) params.set('group', filter.group);
    const query = params.toString() ? `?${params.toString()}` : '';
    const body = await this.fetchJSON<{ placements: Placement[] | null }>(`/api/v1/scheduler/placements${query}`);
    return body.placements ?? [];
  }
}

// RestProjectsClient implements ProjectsClient over the QuicBaaS REST project endpoints.
// All project routes live under /api/v1/projects.
export class RestProjectsClient implements ProjectsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async fetchJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    // resolveToken, not the raw config value: the SPA supplies a token FACTORY
    // so every request re-reads the live access token. Interpolating the config
    // value directly stringified the function itself into the header, and the
    // 401 it earned drove the auth store into a session-ending renewal.
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
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

  // updateProject maps to PUT /api/v1/projects/:projectId?tenant_id=...
  //
  // The tenant goes in the QUERY, not only in the body: the handler derives
  // the tenant and refuses before it reads the body at all
  // (pkg/api/rest/handlers_project.go). Without it every save from the SPA's
  // edit dialog came back 400 — the project edit flow was simply broken.
  async updateProject(req: UpdateProjectRequest): Promise<UpdateProjectResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<UpdateProjectResponse>(
      `/api/v1/projects/${encodeURIComponent(req.project_id)}?${query}`,
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

  // listProjectsPage maps to GET /api/v1/projects with the shared paging
  // parameters, and hands back the envelope the server answered with.
  async listProjectsPage(tenantId: string, page?: ProjectPageParams): Promise<PageResult<Project>> {
    const params = new URLSearchParams({ tenant_id: tenantId });
    appendListParam(params, 'autonomous', page?.autonomous);
    appendPageParams(params, page);
    const raw = await this.fetchJSON<unknown>(`/api/v1/projects?${params.toString()}`);
    return adoptPage<Project>(raw, 'projects');
  }

  // grantFleetNodeRole maps to PUT /api/v1/projects/:projectId/permissions/:agentId
  async grantFleetNodeRole(req: GrantFleetNodeRoleRequest): Promise<GrantFleetNodeRoleResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<GrantFleetNodeRoleResponse>(
      `/api/v1/projects/${encodeURIComponent(req.project_id)}/permissions/${encodeURIComponent(req.node_id)}?${query}`,
      { method: 'PUT', body: JSON.stringify({ role: req.role }) },
    );
  }

  // revokeFleetNodeRole maps to DELETE /api/v1/projects/:projectId/permissions/:agentId
  async revokeFleetNodeRole(req: RevokeFleetNodeRoleRequest): Promise<RevokeFleetNodeRoleResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<RevokeFleetNodeRoleResponse>(
      `/api/v1/projects/${encodeURIComponent(req.project_id)}/permissions/${encodeURIComponent(req.node_id)}?${query}`,
      { method: 'DELETE' },
    );
  }
}

// RestSdlcClient implements SdlcClient over the SDLC module's REST endpoints:
// a project's forge binding and a task's delivery.
export class RestSdlcClient implements SdlcClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async fetchJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    // resolveToken, not the raw config value: the SPA supplies a token FACTORY
    // so every request re-reads the live access token. Interpolating the config
    // value directly stringified the function itself into the header, and the
    // 401 it earned drove the auth store into a session-ending renewal.
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
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

  private bindingPath(tenantId: string, projectId: string): string {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return `/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/binding?${query}`;
  }

  // forges maps to GET /api/v1/sdlc/forges
  // The endpoint returns {"forges": [...]}; unwrap to a bare slice.
  async forges(): Promise<string[]> {
    const resp = await this.fetchJSON<{ forges?: string[] } | string[]>('/api/v1/sdlc/forges');
    if (Array.isArray(resp)) {
      return resp;
    }
    return resp.forges ?? [];
  }

  // bind maps to PUT /api/v1/sdlc/projects/:projectId/binding?tenant_id=...[&paused=true]
  async bind(tenantId: string, projectId: string, binding: ProjectBinding, paused?: boolean): Promise<ProjectBinding> {
    const path = this.bindingPath(tenantId, projectId) + (paused ? '&paused=true' : '');
    return this.fetchJSON<ProjectBinding>(path, {
      method: 'PUT',
      body: JSON.stringify({ ...binding, project_id: binding.project_id || projectId }),
    });
  }

  // unbind maps to DELETE /api/v1/sdlc/projects/:projectId/binding?tenant_id=...
  async unbind(tenantId: string, projectId: string): Promise<void> {
    await this.fetchJSON<void>(this.bindingPath(tenantId, projectId), { method: 'DELETE' });
  }

  // binding maps to GET /api/v1/sdlc/projects/:projectId/binding?tenant_id=...
  async binding(tenantId: string, projectId: string): Promise<ProjectBinding> {
    return this.fetchJSON<ProjectBinding>(this.bindingPath(tenantId, projectId));
  }

  // delivery maps to GET /api/v1/sdlc/tasks/:taskId/delivery?tenant_id=...
  async delivery(tenantId: string, taskId: string): Promise<Delivery> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<Delivery>(`/api/v1/sdlc/tasks/${encodeURIComponent(taskId)}/delivery?${query}`);
  }

  // onboard maps to POST /api/v1/sdlc/projects/:projectId/onboarding
  // A POST, because it writes to the forge. The read is a different route so
  // that asking what state a repository is in cannot change it.
  async onboard(tenantId: string, projectId: string): Promise<OnboardingReport> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<OnboardingReport>(
      `/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/onboarding?${query}`,
      { method: 'POST' },
    );
  }

  // conformance maps to GET /api/v1/sdlc/projects/:projectId/conformance
  async conformance(tenantId: string, projectId: string): Promise<ConformanceReport> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<ConformanceReport>(
      `/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/conformance?${query}`,
    );
  }

  // openWork maps to GET /api/v1/sdlc/projects/:projectId/open-work
  async openWork(tenantId: string, projectId: string): Promise<OpenWork> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<OpenWork>(
      `/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/open-work?${query}`,
    );
  }

  private reviewerPath(tenantId: string, projectId: string, pullRequest: number): string {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return `/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/pulls/${encodeURIComponent(String(pullRequest))}/reviewer?${query}`;
  }

  // reviewer maps to GET /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer
  async reviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection> {
    return this.fetchJSON<ReviewerSelection>(this.reviewerPath(tenantId, projectId, pullRequest));
  }

  // setReviewer maps to PUT /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer
  // The body carries the profile alone: the path names the project and the
  // pull request.
  async setReviewer(tenantId: string, projectId: string, pullRequest: number, profile: string): Promise<ReviewerSelection> {
    return this.fetchJSON<ReviewerSelection>(this.reviewerPath(tenantId, projectId, pullRequest), {
      method: 'PUT',
      body: JSON.stringify({ profile }),
    });
  }

  // clearReviewer maps to DELETE /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer
  // The server answers the selection that now holds, not 204.
  async clearReviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection> {
    return this.fetchJSON<ReviewerSelection>(this.reviewerPath(tenantId, projectId, pullRequest), {
      method: 'DELETE',
    });
  }

  // fileIssue maps to POST /api/v1/sdlc/projects/:projectId/issues
  // The body carries the filing alone: the path names the project.
  async fileIssue(tenantId: string, projectId: string, req: FileIssueRequest): Promise<ForgeIssue> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<ForgeIssue>(`/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/issues?${query}`, {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // collaborators maps to GET /api/v1/sdlc/projects/:projectId/collaborators
  async collaborators(tenantId: string, projectId: string): Promise<CollaboratorsResponse> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<CollaboratorsResponse>(
      `/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/collaborators?${query}`,
    );
  }

  // adoptIssue maps to POST /api/v1/sdlc/projects/:projectId/adopt
  // The path names the project; the body carries the issue, the pull request
  // and the ruling.
  async adoptIssue(tenantId: string, projectId: string, request: AdoptIssueRequest): Promise<Adoption> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<Adoption>(`/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/adopt?${query}`, {
      method: 'POST',
      body: JSON.stringify(request),
    });
  }
}

// RestAccountsClient implements AccountsClient over the QuicBaaS REST pooled
// provider account endpoints. All account routes live under
// /api/v1/agent-accounts. See handlers_account.go for the server-side shapes
// this mirrors — AccountRESTResponse never carries credential material, so
// no method here can either.
export class RestAccountsClient implements AccountsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async fetchJSON<T>(path: string, init: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    // resolveToken, not the raw config value: the SPA supplies a token FACTORY
    // so every request re-reads the live access token. Interpolating the config
    // value directly stringified the function itself into the header, and the
    // 401 it earned drove the auth store into a session-ending renewal.
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
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

  // createAccount maps to POST /api/v1/agent-accounts
  async createAccount(req: CreateAccountRequest): Promise<CreateAccountResponse> {
    return this.fetchJSON<CreateAccountResponse>('/api/v1/agent-accounts', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // getAccount maps to GET /api/v1/agent-accounts/:accountId?tenant_id=...
  async getAccount(tenantId: string, accountId: string): Promise<GetAccountResponse> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<GetAccountResponse>(`/api/v1/agent-accounts/${encodeURIComponent(accountId)}?${query}`);
  }

  // listAccounts maps to GET /api/v1/agent-accounts?tenant_id=...&provider=...&project_id=...
  async listAccounts(req: ListAccountsRequest): Promise<ListAccountsResponse> {
    const params: Record<string, string> = { tenant_id: req.tenant_id };
    if (req.provider) params.provider = req.provider;
    if (req.project_id) params.project_id = req.project_id;
    const query = new URLSearchParams(params).toString();
    return this.fetchJSON<ListAccountsResponse>(`/api/v1/agent-accounts?${query}`);
  }

  // listAccountsPage maps to GET /api/v1/agent-accounts with the shared
  // paging parameters and the accounts listing's own filters.
  async listAccountsPage(
    req: AccountPageParams & { tenant_id: string },
  ): Promise<PageResult<Account>> {
    const params = new URLSearchParams({ tenant_id: req.tenant_id });
    appendListParam(params, 'provider', req.provider);
    appendListParam(params, 'project_id', req.project_id);
    appendListParam(params, 'status', req.status);
    appendPageParams(params, req);
    const raw = await this.fetchJSON<unknown>(`/api/v1/agent-accounts?${params.toString()}`);
    return adoptPage<Account>(raw, 'accounts');
  }

  // updateAccount maps to PUT /api/v1/agent-accounts/:accountId?tenant_id=...
  async updateAccount(req: UpdateAccountRequest): Promise<UpdateAccountResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    const { tenant_id: _tenantId, account_id: accountId, ...body } = req;
    return this.fetchJSON<UpdateAccountResponse>(
      `/api/v1/agent-accounts/${encodeURIComponent(accountId)}?${query}`,
      { method: 'PUT', body: JSON.stringify(body) },
    );
  }

  // deleteAccount maps to DELETE /api/v1/agent-accounts/:accountId?tenant_id=...
  async deleteAccount(tenantId: string, accountId: string): Promise<DeleteAccountResponse> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<DeleteAccountResponse>(
      `/api/v1/agent-accounts/${encodeURIComponent(accountId)}?${query}`,
      { method: 'DELETE' },
    );
  }

  // rotateAccountCredential maps to POST /api/v1/agent-accounts/:accountId/rotate?tenant_id=...
  async rotateAccountCredential(req: RotateAccountCredentialRequest): Promise<RotateAccountCredentialResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<RotateAccountCredentialResponse>(
      `/api/v1/agent-accounts/${encodeURIComponent(req.account_id)}/rotate?${query}`,
      { method: 'POST', body: JSON.stringify({ credential: req.credential }) },
    );
  }

  // revokeAccount maps to POST /api/v1/agent-accounts/:accountId/revoke?tenant_id=...
  async revokeAccount(req: RevokeAccountRequest): Promise<RevokeAccountResponse> {
    const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
    return this.fetchJSON<RevokeAccountResponse>(
      `/api/v1/agent-accounts/${encodeURIComponent(req.account_id)}/revoke?${query}`,
      { method: 'POST', body: JSON.stringify({ reason: req.reason }) },
    );
  }

  // setDefaultAccount maps to POST /api/v1/agent-accounts/:accountId/default?tenant_id=...
  async setDefaultAccount(tenantId: string, accountId: string): Promise<SetDefaultAccountResponse> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<SetDefaultAccountResponse>(
      `/api/v1/agent-accounts/${encodeURIComponent(accountId)}/default?${query}`,
      { method: 'POST' },
    );
  }

  // getAccountUsage maps to GET /api/v1/agent-accounts/:accountId/usage?tenant_id=...
  async getAccountUsage(tenantId: string, accountId: string): Promise<GetAccountUsageResponse> {
    const query = new URLSearchParams({ tenant_id: tenantId }).toString();
    return this.fetchJSON<GetAccountUsageResponse>(
      `/api/v1/agent-accounts/${encodeURIComponent(accountId)}/usage?${query}`,
    );
  }
}

// RestAgentsClient implements AgentsClient over the tenant AI agent catalog:
// /api/v1/agents and /api/v1/agent-pools. See handlers_agents.go for the
// server-side shapes this mirrors. Every route takes ?tenant_id=; without it
// the server answers for the caller's own tenant.
class RestAgentsClient implements AgentsClient {
  private readonly baseUrl: string;
  private readonly config: ClientConfig;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl.replace(/\/$/, '');
    this.config = config;
  }

  private async fetchJSON<T>(path: string, tenantId: string | undefined, init: RequestInit = {}): Promise<T> {
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    const tok = await resolveToken(this.config.token);
    if (tok) headers['Authorization'] = `Bearer ${tok}`;
    const query = tenantId ? `?${new URLSearchParams({ tenant_id: tenantId }).toString()}` : '';
    const resp = await fetch(`${this.baseUrl}${path}${query}`, {
      ...init,
      headers: { ...headers, ...(init.headers as Record<string, string> ?? {}) },
    });
    if (!resp.ok) {
      const text = await resp.text().catch(() => resp.statusText);
      throw new APIError(resp.status, text);
    }
    return parseResponseBody<T>(resp);
  }

  async listAgents(tenantId?: string): Promise<Agent[]> {
    const body = await this.fetchJSON<{ agents?: Agent[] }>('/api/v1/agents', tenantId);
    return body.agents ?? [];
  }

  async getAgent(agentId: string, tenantId?: string): Promise<Agent> {
    return this.fetchJSON<Agent>(`/api/v1/agents/${encodeURIComponent(agentId)}`, tenantId);
  }

  async createAgent(agent: Agent, tenantId?: string): Promise<Agent> {
    return this.fetchJSON<Agent>('/api/v1/agents', tenantId, { method: 'POST', body: JSON.stringify(agent) });
  }

  async updateAgent(agentId: string, agent: Agent, tenantId?: string): Promise<Agent> {
    return this.fetchJSON<Agent>(`/api/v1/agents/${encodeURIComponent(agentId)}`, tenantId, {
      method: 'PUT',
      body: JSON.stringify(agent),
    });
  }

  async deleteAgent(agentId: string, tenantId?: string): Promise<void> {
    await this.fetchJSON<unknown>(`/api/v1/agents/${encodeURIComponent(agentId)}`, tenantId, { method: 'DELETE' });
  }

  async agentUsedBy(agentId: string, tenantId?: string): Promise<AgentUsage[]> {
    const body = await this.fetchJSON<{ used_by?: AgentUsage[] }>(
      `/api/v1/agents/${encodeURIComponent(agentId)}/used-by`,
      tenantId,
    );
    return body.used_by ?? [];
  }

  async listPools(tenantId?: string): Promise<AgentPool[]> {
    const body = await this.fetchJSON<{ pools?: AgentPool[] }>('/api/v1/agent-pools', tenantId);
    return body.pools ?? [];
  }

  async getPool(poolId: string, tenantId?: string): Promise<AgentPool> {
    return this.fetchJSON<AgentPool>(`/api/v1/agent-pools/${encodeURIComponent(poolId)}`, tenantId);
  }

  async createPool(pool: AgentPool, tenantId?: string): Promise<AgentPool> {
    return this.fetchJSON<AgentPool>('/api/v1/agent-pools', tenantId, { method: 'POST', body: JSON.stringify(pool) });
  }

  async updatePool(poolId: string, pool: AgentPool, tenantId?: string): Promise<AgentPool> {
    return this.fetchJSON<AgentPool>(`/api/v1/agent-pools/${encodeURIComponent(poolId)}`, tenantId, {
      method: 'PUT',
      body: JSON.stringify(pool),
    });
  }

  async deletePool(poolId: string, tenantId?: string): Promise<void> {
    await this.fetchJSON<unknown>(`/api/v1/agent-pools/${encodeURIComponent(poolId)}`, tenantId, { method: 'DELETE' });
  }

  async poolUsedBy(poolId: string, tenantId?: string): Promise<AgentUsage[]> {
    const body = await this.fetchJSON<{ used_by?: AgentUsage[] }>(
      `/api/v1/agent-pools/${encodeURIComponent(poolId)}/used-by`,
      tenantId,
    );
    return body.used_by ?? [];
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
        lifecycle?: import('./types.js').UserLifecycle;
        role?: string;
        roles?: string[];
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
      lifecycle: u.lifecycle,
      roles: u.roles ?? (u.role ? [u.role] : []),
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

  async reconcileRoles(tenantID: string): Promise<ReconcileRolesResponse> {
    // Server returns {tenant_id, roles:[...], changed} — roles is omitted when
    // nothing drifted; normalize it to an array so callers can iterate freely.
    const raw = await this.fetchJSON<{ tenant_id?: string; roles?: RoleReconcileResult[]; changed?: boolean }>(
      `/api/v1/tenants/${encodeURIComponent(tenantID)}/roles/reconcile`,
      { method: 'POST' },
    );
    const roles = raw.roles ?? [];
    return { tenant_id: raw.tenant_id ?? tenantID, roles, changed: raw.changed ?? roles.length > 0 };
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

  // THE TENANT IS A PATH SEGMENT, and it is not optional. Invitations live in
  // their tenant's own store, so a token issued for one tenant is simply absent
  // from another — isolation is structural rather than a check that could be
  // forgotten. These methods called tenant-less paths that the server never
  // registered, so every one of them 404'd; on a server whose authenticator
  // answers before routing resolves, that surfaced as an auth error and the
  // browser invite flow had never worked at all.
  async getInvite(tenantID: string, token: string): Promise<import('./types.js').InviteMetadata> {
    return this.fetchJSON<import('./types.js').InviteMetadata>(
      `/api/v1/iam/${encodeURIComponent(tenantID)}/invites/${encodeURIComponent(token)}`,
      { method: 'GET' },
    );
  }

  async activateFIDO2Begin(tenantID: string, token: string): Promise<Record<string, unknown>> {
    return this.fetchJSON<Record<string, unknown>>(
      `/api/v1/iam/${encodeURIComponent(tenantID)}/invites/${encodeURIComponent(token)}/activate/fido2/begin`,
      { method: 'POST' },
    );
  }

  async activateFIDO2Finish(tenantID: string, token: string, credential: Record<string, unknown>): Promise<import('./types.js').ActivationResponse> {
    return this.fetchJSON<import('./types.js').ActivationResponse>(
      `/api/v1/iam/${encodeURIComponent(tenantID)}/invites/${encodeURIComponent(token)}/activate/fido2/finish`,
      { method: 'POST', body: JSON.stringify({ credential }) },
    );
  }

  async activatePIV(tenantID: string, token: string, certPEM: string): Promise<import('./types.js').ActivationResponse> {
    return this.fetchJSON<import('./types.js').ActivationResponse>(
      `/api/v1/iam/${encodeURIComponent(tenantID)}/invites/${encodeURIComponent(token)}/activate/piv`,
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

// unixToISO converts a Go unix-second int64 timestamp to an ISO-8601 string.
// Shared by RestCAClient, RestTrustClient, and RestCryptoClient, all of
// which receive int64 unix timestamps on the wire (Go's time.Time.Unix()).
function unixToISO(t: number | undefined): string {
  if (t === undefined || t === null || !Number.isFinite(t) || t <= 0) return '';
  return new Date(t * 1000).toISOString();
}

// ---------------------------------------------------------------------------
// RestCAClient — /api/v1/certificate/* and /api/v1/xkms/ca/* surface.
// ---------------------------------------------------------------------------

// RestCAClient implements CAClient over the QuicBaaS certificate authority
// REST endpoints. Mirrors sdk/go/transport/rest/certificate_handler.go,
// including its GenerateCRL/IsRevoked routing quirk: those two verbs live
// under /api/v1/xkms/ca/* (the underlying xkms CA implementation) rather
// than /api/v1/certificate/*, because no equivalent route exists on the
// quicbaas-native Certificate service.
export class RestCAClient implements CAClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  async issueCertificate(req: IssueCertificateRequest): Promise<IssueCertificateResponse> {
    const wire = await this.http.fetchJSON<{
      certificate_pem: string;
      private_key_pem: string;
      ca_certificate_pem: string;
      chain_pem: string;
      serial_number: string;
      not_before: number;
      not_after: number;
      timestamp: number;
    }>('/api/v1/certificate/issue', { method: 'POST', body: JSON.stringify(req) });
    return {
      certificate_pem: wire.certificate_pem,
      private_key_pem: wire.private_key_pem,
      ca_certificate_pem: wire.ca_certificate_pem,
      chain_pem: wire.chain_pem,
      serial_number: wire.serial_number,
      not_before: unixToISO(wire.not_before),
      not_after: unixToISO(wire.not_after),
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async signCSR(req: SignCSRRequest): Promise<SignCSRResponse> {
    const wire = await this.http.fetchJSON<{
      certificate_pem: string;
      ca_certificate_pem: string;
      chain_pem: string;
      serial_number: string;
      not_before: number;
      not_after: number;
      timestamp: number;
    }>('/api/v1/certificate/sign-csr', { method: 'POST', body: JSON.stringify(req) });
    return {
      certificate_pem: wire.certificate_pem,
      ca_certificate_pem: wire.ca_certificate_pem,
      chain_pem: wire.chain_pem,
      serial_number: wire.serial_number,
      not_before: unixToISO(wire.not_before),
      not_after: unixToISO(wire.not_after),
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async revokeCertificate(serialNumber: string): Promise<RevokeCertificateResponse> {
    const wire = await this.http.fetchJSON<{ success: boolean; timestamp: number }>(
      '/api/v1/certificate/revoke',
      { method: 'POST', body: JSON.stringify({ serial_number: serialNumber }) },
    );
    return { success: wire.success, timestamp: unixToISO(wire.timestamp) };
  }

  async verifyCertificate(certificatePEM: string): Promise<VerifyCertificateResponse> {
    const wire = await this.http.fetchJSON<{ valid: boolean; error_message?: string; timestamp: number }>(
      '/api/v1/certificate/verify',
      { method: 'POST', body: JSON.stringify({ certificate_pem: certificatePEM }) },
    );
    return { valid: wire.valid, error_message: wire.error_message, timestamp: unixToISO(wire.timestamp) };
  }

  async getCACertificate(): Promise<GetCACertificateResponse> {
    const wire = await this.http.fetchJSON<{
      ca_certificate_pem: string;
      ca_bundle_pem: string;
      identity: string;
      timestamp: number;
    }>('/api/v1/certificate/ca');
    return {
      ca_certificate_pem: wire.ca_certificate_pem,
      ca_bundle_pem: wire.ca_bundle_pem,
      identity: wire.identity,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async caInfo(): Promise<CAInfoResponse> {
    const wire = await this.http.fetchJSON<{
      common_name: string;
      subject: string;
      issuer: string;
      serial_number: string;
      not_before: number;
      not_after: number;
      is_ca: boolean;
      key_type: string;
      key_bits?: number;
      signature_algorithm: string;
      certificate_pem: string;
      ca_bundle_pem: string;
      timestamp: number;
    }>('/api/v1/certificate/ca/info');
    return {
      common_name: wire.common_name,
      subject: wire.subject,
      issuer: wire.issuer,
      serial_number: wire.serial_number,
      not_before: unixToISO(wire.not_before),
      not_after: unixToISO(wire.not_after),
      is_ca: wire.is_ca,
      key_type: wire.key_type,
      key_bits: wire.key_bits,
      signature_algorithm: wire.signature_algorithm,
      certificate_pem: wire.certificate_pem,
      ca_bundle_pem: wire.ca_bundle_pem,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async getCABundle(): Promise<CABundleResponse> {
    const wire = await this.http.fetchJSON<{ bundle_pem: string; timestamp: number }>(
      '/api/v1/certificate/ca/bundle',
    );
    return { bundle_pem: wire.bundle_pem, timestamp: unixToISO(wire.timestamp) };
  }

  async listCertificates(req: ListCertificatesRequest = {}): Promise<ListCertificatesResponse> {
    const params: Record<string, string> = {};
    if (req.common_name) params.common_name = req.common_name;
    if (req.include_revoked) params.include_revoked = 'true';
    if (req.include_expired) params.include_expired = 'true';
    if (req.limit !== undefined) params.limit = String(req.limit);
    const query = new URLSearchParams(params).toString();
    const wire = await this.http.fetchJSON<{
      certificates?: Array<{
        common_name: string;
        subject: string;
        issuer: string;
        serial_number: string;
        not_before: number;
        not_after: number;
        is_ca: boolean;
        key_usage?: string;
        dns_names?: string[];
      }>;
      total_count?: number;
      timestamp: number;
    }>(`/api/v1/certificate/ca/list${query ? `?${query}` : ''}`);
    const certificates: CertificateSummary[] = (wire.certificates ?? []).map((c) => ({
      common_name: c.common_name,
      subject: c.subject,
      issuer: c.issuer,
      serial_number: c.serial_number,
      not_before: unixToISO(c.not_before),
      not_after: unixToISO(c.not_after),
      is_ca: c.is_ca,
      key_usage: c.key_usage,
      dns_names: c.dns_names,
    }));
    return { certificates, total_count: wire.total_count ?? certificates.length, timestamp: unixToISO(wire.timestamp) };
  }

  async generateCRL(): Promise<GenerateCRLResponse> {
    // The CRL route lives under the xkms-native CA (see class doc comment).
    // The wire field is base64 of the already PEM-encoded CRL text.
    const wire = await this.http.fetchJSON<{ crl_pem: string }>(
      '/api/v1/xkms/ca/crl',
      { method: 'POST', body: JSON.stringify({}) },
    );
    let decoded = '';
    try {
      decoded = atob(wire.crl_pem);
    } catch {
      decoded = wire.crl_pem;
    }
    if (!decoded.includes('-----BEGIN')) {
      // Defensive fallback: wrap raw DER bytes in PEM headers ourselves,
      // matching the Go SDK's rest.certificate_handler.go behaviour.
      const rewrapped = wire.crl_pem.replace(/(.{64})/g, '$1\n');
      decoded = `-----BEGIN X509 CRL-----\n${rewrapped}\n-----END X509 CRL-----\n`;
    }
    return { crl_pem: decoded };
  }

  async isRevoked(serialNumber: string): Promise<IsRevokedResponse> {
    return this.http.fetchJSON<IsRevokedResponse>(
      `/api/v1/xkms/ca/revoked/${encodeURIComponent(serialNumber)}`,
    );
  }
}

// ---------------------------------------------------------------------------
// RestTrustClient — /api/v1/trust/* surface.
// ---------------------------------------------------------------------------

// pemToDERBase64 extracts the base64 payload from a single PEM block,
// stripping the header/footer lines and all whitespace. The result is
// exactly the base64 encoding of the DER bytes the block contains — no
// re-encoding is required because that's what PEM already is.
function pemToDERBase64(pem: string): string {
  return pem
    .replace(/-----BEGIN [^-]+-----/g, '')
    .replace(/-----END [^-]+-----/g, '')
    .replace(/\s+/g, '');
}

// RestTrustClient implements TrustClient over the QuicBaaS trust composition
// REST endpoints (/api/v1/trust/*). See handlers_trust.go.
export class RestTrustClient implements TrustClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  async crossSignedRootPool(): Promise<CrossSignedRootPoolResponse> {
    const wire = await this.http.fetchJSON<{ root_pems?: string[] }>(
      '/api/v1/trust/cross-signed-roots',
    );
    const root_pems = (wire.root_pems ?? []).map((b64) => {
      try {
        return atob(b64);
      } catch {
        return b64;
      }
    });
    return { root_pems };
  }

  async autoImportAIA(req: AutoImportAIARequest): Promise<AutoImportAIAResponse> {
    const chain_der = req.chain_pems.map(pemToDERBase64);
    return this.http.fetchJSON<AutoImportAIAResponse>(
      '/api/v1/trust/auto-import-aia',
      { method: 'POST', body: JSON.stringify({ chain_der }) },
    );
  }

  async publishDANE(req: PublishDANERequest): Promise<PublishDANEResponse> {
    const cert_der = pemToDERBase64(req.cert_pem);
    return this.http.fetchJSON<PublishDANEResponse>(
      '/api/v1/trust/publish-dane',
      { method: 'POST', body: JSON.stringify({ name: req.name, port: req.port, cert_der }) },
    );
  }
}

// ---------------------------------------------------------------------------
// RestCryptoClient — /api/v1/crypto/* surface.
// ---------------------------------------------------------------------------

// RestCryptoClient implements CryptoClient over the QuicBaaS crypto
// primitives REST endpoints (/api/v1/crypto/*). See handlers_crypto.go.
// All binary fields are base64 strings on the wire; this client passes them
// through unchanged (no re-encoding), except for unix timestamps which are
// converted to ISO-8601 strings for consistency with the rest of the SDK.
export class RestCryptoClient implements CryptoClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  async encrypt(req: CryptoEncryptRequest): Promise<CryptoEncryptResponse> {
    const wire = await this.http.fetchJSON<{
      key_id: string;
      key_version: number;
      ciphertext: string;
      nonce?: string;
      timestamp: number;
    }>(`/api/v1/crypto/encrypt/${encodeURIComponent(req.key_name)}`, {
      method: 'POST',
      body: JSON.stringify({ plaintext: req.plaintext, context: req.context, nonce: req.nonce }),
    });
    return {
      key_id: wire.key_id,
      key_version: wire.key_version,
      ciphertext: wire.ciphertext,
      nonce: wire.nonce,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async decrypt(req: CryptoDecryptRequest): Promise<CryptoDecryptResponse> {
    const wire = await this.http.fetchJSON<{
      key_id: string;
      key_version: number;
      plaintext: string;
      timestamp: number;
    }>(`/api/v1/crypto/decrypt/${encodeURIComponent(req.key_name)}`, {
      method: 'POST',
      body: JSON.stringify({
        ciphertext: req.ciphertext,
        context: req.context,
        nonce: req.nonce,
        key_version: req.key_version,
      }),
    });
    return {
      key_id: wire.key_id,
      key_version: wire.key_version,
      plaintext: wire.plaintext,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async rewrap(req: CryptoRewrapRequest): Promise<CryptoRewrapResponse> {
    const wire = await this.http.fetchJSON<{
      key_id: string;
      old_key_version: number;
      new_key_version: number;
      ciphertext: string;
      nonce?: string;
      timestamp: number;
    }>(`/api/v1/crypto/rewrap/${encodeURIComponent(req.key_name)}`, {
      method: 'POST',
      body: JSON.stringify({ ciphertext: req.ciphertext, context: req.context, nonce: req.nonce }),
    });
    return {
      key_id: wire.key_id,
      old_key_version: wire.old_key_version,
      new_key_version: wire.new_key_version,
      ciphertext: wire.ciphertext,
      nonce: wire.nonce,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async sign(req: CryptoSignRequest): Promise<CryptoSignResponse> {
    const wire = await this.http.fetchJSON<{
      key_id: string;
      key_version: number;
      signature: string;
      hash_algorithm: CryptoSignResponse['hash_algorithm'];
      timestamp: number;
    }>(`/api/v1/crypto/sign/${encodeURIComponent(req.key_name)}`, {
      method: 'POST',
      body: JSON.stringify({
        message: req.message,
        hash_algorithm: req.hash_algorithm,
        pre_hashed: req.pre_hashed,
      }),
    });
    return {
      key_id: wire.key_id,
      key_version: wire.key_version,
      signature: wire.signature,
      hash_algorithm: wire.hash_algorithm,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async verify(req: CryptoVerifyRequest): Promise<CryptoVerifyResponse> {
    const wire = await this.http.fetchJSON<{
      key_id: string;
      key_version: number;
      valid: boolean;
      timestamp: number;
    }>(`/api/v1/crypto/verify/${encodeURIComponent(req.key_name)}`, {
      method: 'POST',
      body: JSON.stringify({
        message: req.message,
        signature: req.signature,
        hash_algorithm: req.hash_algorithm,
        key_version: req.key_version,
      }),
    });
    return {
      key_id: wire.key_id,
      key_version: wire.key_version,
      valid: wire.valid,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async hash(req: CryptoHashRequest): Promise<CryptoHashResponse> {
    const wire = await this.http.fetchJSON<{
      algorithm: CryptoHashResponse['algorithm'];
      hash: string;
      timestamp: number;
    }>('/api/v1/crypto/hash', {
      method: 'POST',
      body: JSON.stringify({ data: req.data, algorithm: req.algorithm }),
    });
    return { algorithm: wire.algorithm, hash: wire.hash, timestamp: unixToISO(wire.timestamp) };
  }

  async random(numBytes = 32): Promise<CryptoRandomResponse> {
    const wire = await this.http.fetchJSON<{ random_bytes: string; timestamp: number }>(
      '/api/v1/crypto/random',
      { method: 'POST', body: JSON.stringify({ num_bytes: numBytes }) },
    );
    return { random_bytes: wire.random_bytes, timestamp: unixToISO(wire.timestamp) };
  }

  async dataKey(req: CryptoDataKeyRequest): Promise<CryptoDataKeyResponse> {
    const wire = await this.http.fetchJSON<{
      key_id: string;
      plaintext: string;
      ciphertext_blob: string;
      timestamp: number;
    }>(`/api/v1/crypto/datakey/${encodeURIComponent(req.key_name)}`, {
      method: 'POST',
      body: JSON.stringify({
        key_size_bits: req.key_size_bits,
        algorithm: req.algorithm,
        context: req.context,
      }),
    });
    return {
      key_id: wire.key_id,
      plaintext: wire.plaintext,
      ciphertext_blob: wire.ciphertext_blob,
      timestamp: unixToISO(wire.timestamp),
    };
  }
}

// ---------------------------------------------------------------------------
// RestDEKClient — /api/v1/kms/deks/* surface.
// ---------------------------------------------------------------------------

// RestDEKClient implements DEKClient over the QuicBaaS Data Encryption Key
// REST endpoints (/api/v1/kms/deks/*). See handlers_kms_dek.go. DEK path
// identifiers may contain '/' (e.g. "tenant/app/key"); every method encodes
// the path with encodeURIComponent, matching RestSecretsClient's convention
// — chi's wildcard route decodes it back to the original path server-side.
// Unlike RestCAClient/RestTrustClient/RestCryptoClient, DEK response
// timestamps arrive pre-formatted as RFC3339 strings (json.Marshal of a Go
// time.Time), so no unix-to-ISO conversion is applied here.
export class RestDEKClient implements DEKClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  async createDEK(req: CreateDEKRequest): Promise<CreateDEKResponse> {
    return this.http.fetchJSON<CreateDEKResponse>('/api/v1/kms/deks', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  async getDEK(path: string, version?: number): Promise<GetDEKResponse> {
    const query = version !== undefined ? `?version=${encodeURIComponent(String(version))}` : '';
    return this.http.fetchJSON<GetDEKResponse>(`/api/v1/kms/deks/${encodeURIComponent(path)}${query}`);
  }

  async listDEKs(req: ListDEKsRequest = {}): Promise<ListDEKsResponse> {
    const params: Record<string, string> = {};
    if (req.prefix) params.prefix = req.prefix;
    if (req.recursive) params.recursive = 'true';
    if (req.limit !== undefined) params.limit = String(req.limit);
    if (req.cursor) params.cursor = req.cursor;
    const query = new URLSearchParams(params).toString();
    const wire = await this.http.fetchJSON<ListDEKsResponse>(
      `/api/v1/kms/deks${query ? `?${query}` : ''}`,
    );
    return { ...wire, deks: wire.deks ?? [] };
  }

  async rotateDEK(path: string, req: RotateDEKRequest = {}): Promise<RotateDEKResponse> {
    return this.http.fetchJSON<RotateDEKResponse>(
      `/api/v1/kms/deks/${encodeURIComponent(path)}/rotate`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  async deleteDEK(path: string, req: DeleteDEKRequest = {}): Promise<DeleteDEKResponse> {
    return this.http.fetchJSON<DeleteDEKResponse>(
      `/api/v1/kms/deks/${encodeURIComponent(path)}`,
      { method: 'DELETE', body: JSON.stringify(req) },
    );
  }

  async getDEKStatus(path: string): Promise<GetDEKStatusResponse> {
    return this.http.fetchJSON<GetDEKStatusResponse>(`/api/v1/kms/deks/${encodeURIComponent(path)}/status`);
  }

  async setDefaultDEK(path: string): Promise<SetDefaultDEKResponse> {
    return this.http.fetchJSON<SetDefaultDEKResponse>(
      `/api/v1/kms/deks/${encodeURIComponent(path)}/set-default`,
      { method: 'POST' },
    );
  }

  async setDEKSafety(path: string, req: SetDEKSafetyRequest): Promise<SetDEKSafetyResponse> {
    return this.http.fetchJSON<SetDEKSafetyResponse>(
      `/api/v1/kms/deks/${encodeURIComponent(path)}/safety`,
      { method: 'PUT', body: JSON.stringify(req) },
    );
  }

  async clearDEKSafety(path: string): Promise<ClearDEKSafetyResponse> {
    return this.http.fetchJSON<ClearDEKSafetyResponse>(
      `/api/v1/kms/deks/${encodeURIComponent(path)}/safety`,
      { method: 'DELETE' },
    );
  }

  async startReencryption(path: string, req: StartReencryptionRequest = {}): Promise<StartReencryptionResponse> {
    return this.http.fetchJSON<StartReencryptionResponse>(
      `/api/v1/kms/deks/${encodeURIComponent(path)}/reencrypt`,
      { method: 'POST', body: JSON.stringify(req) },
    );
  }

  async getReencryptionProgress(
    path: string,
    operationId?: string,
    verbose?: boolean,
  ): Promise<GetReencryptionProgressResponse> {
    const params: Record<string, string> = {};
    if (operationId) params.operation_id = operationId;
    if (verbose) params.verbose = 'true';
    const query = new URLSearchParams(params).toString();
    return this.http.fetchJSON<GetReencryptionProgressResponse>(
      `/api/v1/kms/deks/${encodeURIComponent(path)}/reencrypt${query ? `?${query}` : ''}`,
    );
  }
}

// ---------------------------------------------------------------------------
// RestRegistryClient
// ---------------------------------------------------------------------------

export class RestRegistryClient implements RegistryClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  // Maps to POST /api/v1/registry/instances (201).
  async register(instance: RegistryServiceInstance, ttlSeconds: number): Promise<RegistryLeaseResponse> {
    const req: RegistryRegisterRequest = { instance, ttl_seconds: ttlSeconds };
    return this.http.fetchJSON<RegistryLeaseResponse>('/api/v1/registry/instances', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // Maps to POST /api/v1/registry/leases/{lease_id}/renew.
  async renew(leaseId: string): Promise<RegistryLeaseResponse> {
    return this.http.fetchJSON<RegistryLeaseResponse>(
      `/api/v1/registry/leases/${encodeURIComponent(leaseId)}/renew`,
      { method: 'POST' },
    );
  }

  // Maps to DELETE /api/v1/registry/leases/{lease_id} (204).
  async deregister(leaseId: string): Promise<void> {
    return this.http.fetchVoid(`/api/v1/registry/leases/${encodeURIComponent(leaseId)}`, {
      method: 'DELETE',
    });
  }

  // Maps to GET /api/v1/registry/services/{service}.
  async resolve(service: string): Promise<RegistryResolveResponse> {
    const wire = await this.http.fetchJSON<RegistryResolveResponse>(
      `/api/v1/registry/services/${encodeURIComponent(service)}`,
    );
    return { ...wire, instances: wire.instances ?? [] };
  }
}

// ---------------------------------------------------------------------------
// RestDynamicSecretsClient — /api/v1/secrets/dynamic/* surface.
// ---------------------------------------------------------------------------

// RestDynamicSecretsClient implements DynamicSecretsClient over the
// QuicBaaS dynamic secrets REST endpoints (/api/v1/secrets/dynamic/*). See
// handlers_dynamic_secrets.go. Like RestCAClient/RestTrustClient, the wire
// responses carry Go int64 unix-second timestamps; every method converts
// them to ISO-8601 strings before returning to the caller.
export class RestDynamicSecretsClient implements DynamicSecretsClient {
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.http = newFetchHelper(baseUrl, config);
  }

  async createRole(req: DynamicSecretCreateRoleRequest): Promise<DynamicSecretRole> {
    const wire = await this.http.fetchJSON<{
      name: string;
      generator_type: string;
      config?: Record<string, string>;
      default_ttl: string;
      max_ttl: string;
      created_at?: number;
      timestamp: number;
    }>('/api/v1/secrets/dynamic/roles', { method: 'POST', body: JSON.stringify(req) });
    return {
      name: wire.name,
      generator_type: wire.generator_type,
      config: wire.config,
      default_ttl: wire.default_ttl,
      max_ttl: wire.max_ttl,
      created_at: wire.created_at !== undefined ? unixToISO(wire.created_at) : undefined,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async getRole(name: string): Promise<DynamicSecretRole> {
    const wire = await this.http.fetchJSON<{
      name: string;
      generator_type: string;
      config?: Record<string, string>;
      default_ttl: string;
      max_ttl: string;
      timestamp: number;
    }>(`/api/v1/secrets/dynamic/roles/${encodeURIComponent(name)}`);
    return {
      name: wire.name,
      generator_type: wire.generator_type,
      config: wire.config,
      default_ttl: wire.default_ttl,
      max_ttl: wire.max_ttl,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async listRoles(prefix?: string): Promise<ListDynamicSecretRolesResponse> {
    const query = prefix ? `?prefix=${encodeURIComponent(prefix)}` : '';
    const wire = await this.http.fetchJSON<{
      roles?: Array<{
        name: string;
        generator_type: string;
        config?: Record<string, string>;
        default_ttl: string;
        max_ttl: string;
        timestamp: number;
      }>;
      count?: number;
      timestamp: number;
    }>(`/api/v1/secrets/dynamic/roles${query}`);
    const roles: DynamicSecretRole[] = (wire.roles ?? []).map((r) => ({
      name: r.name,
      generator_type: r.generator_type,
      config: r.config,
      default_ttl: r.default_ttl,
      max_ttl: r.max_ttl,
      timestamp: unixToISO(r.timestamp),
    }));
    return { roles, count: wire.count ?? roles.length, timestamp: unixToISO(wire.timestamp) };
  }

  async deleteRole(name: string): Promise<DeleteDynamicSecretRoleResponse> {
    const wire = await this.http.fetchJSON<{ success: boolean; deleted_at: number; timestamp: number }>(
      `/api/v1/secrets/dynamic/roles/${encodeURIComponent(name)}`,
      { method: 'DELETE' },
    );
    return { success: wire.success, deleted_at: unixToISO(wire.deleted_at), timestamp: unixToISO(wire.timestamp) };
  }

  async generateCredential(
    roleName: string,
    req: GenerateCredentialRequest = {},
  ): Promise<GenerateCredentialResponse> {
    const wire = await this.http.fetchJSON<{
      credential_id: string;
      username?: string;
      password?: string;
      secret?: Record<string, string>;
      lease_id: string;
      lease_ttl: string;
      lease_expiry: number;
      renewable: boolean;
      timestamp: number;
    }>(`/api/v1/secrets/dynamic/credentials/${encodeURIComponent(roleName)}`, {
      method: 'POST',
      body: JSON.stringify(req),
    });
    return {
      credential_id: wire.credential_id,
      username: wire.username,
      password: wire.password,
      secret: wire.secret,
      lease_id: wire.lease_id,
      lease_ttl: wire.lease_ttl,
      lease_expiry: unixToISO(wire.lease_expiry),
      renewable: wire.renewable,
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async renewLease(leaseId: string, req: RenewLeaseRequest = {}): Promise<RenewLeaseResponse> {
    const wire = await this.http.fetchJSON<{
      lease_id: string;
      lease_ttl: string;
      lease_expiry: number;
      renew_count: number;
      renewed_at: number;
      timestamp: number;
    }>(`/api/v1/secrets/dynamic/leases/${encodeURIComponent(leaseId)}/renew`, {
      method: 'POST',
      body: JSON.stringify(req),
    });
    return {
      lease_id: wire.lease_id,
      lease_ttl: wire.lease_ttl,
      lease_expiry: unixToISO(wire.lease_expiry),
      renew_count: wire.renew_count,
      renewed_at: unixToISO(wire.renewed_at),
      timestamp: unixToISO(wire.timestamp),
    };
  }

  async revokeLease(leaseId: string): Promise<RevokeDynamicSecretLeaseResponse> {
    const wire = await this.http.fetchJSON<{ success: boolean; revoked_at: number; timestamp: number }>(
      `/api/v1/secrets/dynamic/leases/${encodeURIComponent(leaseId)}`,
      { method: 'DELETE' },
    );
    return { success: wire.success, revoked_at: unixToISO(wire.revoked_at), timestamp: unixToISO(wire.timestamp) };
  }

  async listLeases(prefix?: string): Promise<ListLeasesResponse> {
    const query = prefix ? `?prefix=${encodeURIComponent(prefix)}` : '';
    const wire = await this.http.fetchJSON<{
      leases?: Array<{
        lease_id: string;
        path: string;
        ttl: string;
        expiry: number;
        renewable: boolean;
        renew_count: number;
        metadata?: Record<string, string>;
      }>;
      count?: number;
      timestamp: number;
    }>(`/api/v1/secrets/dynamic/leases${query}`);
    const leases = (wire.leases ?? []).map((l) => ({
      lease_id: l.lease_id,
      path: l.path,
      ttl: l.ttl,
      expiry: unixToISO(l.expiry),
      renewable: l.renewable,
      renew_count: l.renew_count,
      metadata: l.metadata,
    }));
    return { leases, count: wire.count ?? leases.length, timestamp: unixToISO(wire.timestamp) };
  }
}

// ---------------------------------------------------------------------------
// RestBarrierClient — canonical /api/v1/barrier surface.
// ---------------------------------------------------------------------------

// RestBarrierClient implements BarrierClient by routing to the QuicBaaS
// barrier REST endpoints. The shape mirrors go-qrdb's BarrierClient surface.
export class RestBarrierClient implements BarrierClient {
  private readonly baseUrl: string;
  private readonly http: FetchHelper;

  constructor(baseUrl: string, config: ClientConfig = {}) {
    this.baseUrl = baseUrl;
    this.http = newFetchHelper(baseUrl, config);
  }

  async status(): Promise<BarrierStatusResponse> {
    return this.http.fetchJSON<BarrierStatusResponse>('/api/v1/barrier/status');
  }

  async initialize(req: BarrierInitRequest): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/initialize', {
      method: 'POST',
      body: JSON.stringify({ secret: bytesToBase64(req.secret) }),
    });
  }

  async unseal(req: BarrierUnsealRequest): Promise<void> {
    await this.http.fetchVoid('/api/v1/barrier/unseal', {
      method: 'POST',
      body: JSON.stringify({ secret: bytesToBase64(req.secret) }),
    });
  }

  async seal(req?: BarrierSealRequest): Promise<void> {
    requireTLSForSOPIN('rest', this.baseUrl, req?.so_pin);
    await this.http.fetchVoid('/api/v1/barrier/seal', {
      method: 'POST',
      body: JSON.stringify({ so_pin: req?.so_pin }),
    });
  }

  async initializeShamir(req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse> {
    return this.http.fetchJSON<BarrierShamirInitResponse>('/api/v1/barrier/shamir/initialize', {
      method: 'POST',
      body: JSON.stringify({ ...req, secret: bytesToBase64(req.secret) }),
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

  // enableEncryption posts to the go-qrdb wire mount, whose body is the
  // canonical transport.BarrierEnableEncryptionRequest: tenant_id there
  // selects the tenant barrier, and the reply acknowledges the started
  // migration.
  async enableEncryption(
    req: BarrierEnableEncryptionRequest,
  ): Promise<BarrierEnableEncryptionResponse> {
    const { passphrase, ...rest } = req;
    const body = passphrase === undefined ? rest : { ...rest, passphrase: bytesToBase64(passphrase) };
    return this.http.fetchJSON<BarrierEnableEncryptionResponse>('/v1/barrier/enable-encryption', {
      method: 'POST',
      body: JSON.stringify(body),
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
    const { passphrase, ...rest } = req;
    const body = passphrase === undefined ? rest : { ...rest, passphrase: bytesToBase64(passphrase) };
    // The shamir strategy returns 200 with the minted shares; the none and
    // passphrase strategies return 204 No Content. fetchJSON yields undefined
    // on a 204, so the union return type already covers both cases.
    return this.http.fetchJSON<TenantBarrierInitResponse | undefined>(
      `${this.base(tenantId)}/init`,
      {
        method: 'POST',
        body: JSON.stringify(body),
      },
    );
  }

  async tenantBarrierUnseal(tenantId: string, key: Uint8Array): Promise<void> {
    await this.http.fetchVoid(`${this.base(tenantId)}/unseal`, {
      method: 'POST',
      body: JSON.stringify({ key: bytesToBase64(key) }),
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

  async claimShareBegin(
    req: CeremonyClaimShareBeginRequest,
  ): Promise<CeremonyClaimShareBeginResponse> {
    return this.http.fetchJSON<CeremonyClaimShareBeginResponse>(
      '/api/v1/init/officer-share/begin',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
    );
  }

  async claimShareComplete(
    req: CeremonyClaimShareCompleteRequest,
  ): Promise<CeremonyClaimShareCompleteResponse> {
    return this.http.fetchJSON<CeremonyClaimShareCompleteResponse>(
      '/api/v1/init/officer-share/complete',
      {
        method: 'POST',
        body: JSON.stringify(req),
      },
    );
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
  readonly sdlc: SdlcClient;
  readonly accounts: AccountsClient;
  readonly agents: AgentsClient;
  readonly iam: IAMClient;
  readonly kms: KMSClient;
  readonly secrets: SecretsClient;
  readonly ca: CAClient;
  readonly trust: TrustClient;
  readonly crypto: CryptoClient;
  readonly dek: DEKClient;
  readonly registry: RegistryClient;
  readonly dynamicSecrets: DynamicSecretsClient;
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
    this.sdlc = new RestSdlcClient(baseUrl, config);
    this.accounts = new RestAccountsClient(baseUrl, config);
    this.agents = new RestAgentsClient(baseUrl, config);
    this.iam = new RestIAMClient(baseUrl, config);
    this.kms = new RestKMSClient(baseUrl, config);
    this.secrets = new RestSecretsClient(baseUrl, config);
    this.ca = new RestCAClient(baseUrl, config);
    this.trust = new RestTrustClient(baseUrl, config);
    this.crypto = new RestCryptoClient(baseUrl, config);
    this.dek = new RestDEKClient(baseUrl, config);
    this.registry = new RestRegistryClient(baseUrl, config);
    this.dynamicSecrets = new RestDynamicSecretsClient(baseUrl, config);
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

// costSettingsQuery scopes a cost-settings request to a tenant when one is
// named; the server otherwise answers for the caller's own tenant.
function costSettingsQuery(tenantID: string | undefined): string {
  return tenantID ? `?${new URLSearchParams({ tenant_id: tenantID }).toString()}` : '';
}
