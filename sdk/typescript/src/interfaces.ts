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

import type { RAGClient } from './rag.js';
import type { EmbeddingsClient } from './embedding_sdk.js';
import type { MessagingClient } from './messaging_sdk.js';
import type { SchedulerClient } from './scheduler_sdk.js';
import type { ProjectsClient } from './projects.js';

import type {
  AgentBackendInfo,
  AuthTokenResponse,
  DeployAgentResult,
  DeployAgentSpec,
  InferenceRequest,
  InferenceResponse,
  InferenceChunk,
  CredentialAssertionJSON,
  PublicKeyCredentialJSON,
  PublicTenant,
  SSHKey,
  SSHScope,
  CreateSSHKeyRequest,
  ImportSSHKeyRequest,
  ListSSHKeysQuery,
  ListSSHKeysResponse,
  SSHPublicKey,
  SignRequest,
  SignResponse,
  SSHPin,
  AddSSHPinRequest,
  ListSSHPinsResponse,
  VerifySSHPinRequest,
  SSHAuthorizedEntry,
  EmitAuthorizedRequest,
  EmitAuthorizedResponse,
  VerifyAuthorizedRequest,
  VerifyAuthorizedResponse,
  AgentRegistration,
  CloseSessionRequest,
  CloseSessionResponse,
  CreateKeyRequest,
  CreateKeyResponse,
  CreateTenantRequest,
  CreateTenantResponse,
  CreateTaskRequest,
  CreateTaskResponse,
  CreateUserRequest,
  CreateUserResponse,
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
  SessionEventEnvelope,
  SessionInfo,
  SpawnSessionRequest,
  SpawnSessionResponse,
  ClaimTaskRequest,
  CompleteTaskRequest,
  StartJobRequest,
  StartJobResponse,
  StoreSecretRequest,
  SubscribeSessionEventsRequest,
  Task,
  UnsealRequest,
  UnsealResponse,
  WorkflowDefinition,
} from './types.js';

// ConnectionClient manages the transport-level connection to the QuicBaaS server.
// In Wails desktop mode this is a no-op since the Go process is embedded.
// In REST mode this tracks the base URL and validates connectivity.
export interface ConnectionClient {
  // connect establishes a connection to the given address.
  // transport is optional and defaults to the implementation's native transport.
  connect(address: string, transport?: string, tenantID?: string): Promise<void>;

  // disconnect terminates the connection.
  disconnect(): Promise<void>;

  // isConnected returns true if a connection is currently active.
  isConnected(): boolean;

  // getAddress returns the address of the connected server.
  getAddress(): string;

  // getTransport returns the name of the active transport (e.g. "rest", "wails").
  getTransport(): string;
}

// OrchestratorClient provides all orchestration operations.
// Implementations must delegate to the appropriate transport.
export interface OrchestratorClient {
  // listJobs returns all jobs for a tenant, sorted by start time descending.
  listJobs(tenantID: string): Promise<JobStatus[]>;

  // getJobStatus returns the current status of a specific job.
  getJobStatus(tenantID: string, jobID: string): Promise<JobStatus>;

  // startJob starts an autonomous orchestration control loop.
  startJob(req: StartJobRequest): Promise<StartJobResponse>;

  // cancelJob cancels a running orchestration job.
  cancelJob(tenantID: string, jobID: string): Promise<void>;

  // listAgents returns all registered agents matching the filter criteria.
  listAgents(req: ListAgentsRequest): Promise<ListAgentsResponse>;

  // getAgentStatus retrieves a single registered agent by ID.
  getAgentStatus(req: GetAgentRequest): Promise<AgentRegistration>;

  // listSessions returns all active sessions, optionally filtered.
  listSessions(req: ListSessionsRequest): Promise<ListSessionsResponse>;

  // listTasks returns all tasks for a tenant.
  listTasks(tenantID: string): Promise<Task[]>;

  // getTask retrieves a single task by ID within a tenant.
  getTask(tenantID: string, taskID: string): Promise<Task>;

  // listBackends returns all registered agent backends.
  listBackends(): Promise<AgentBackendInfo[]>;

  // listBackendModels returns model names available on the named backend as
  // seen from the specified agent. For Ollama this enumerates locally-pulled
  // models; for ACP backends it returns a curated catalog.
  // Maps to GET /api/v1/orchestrator/agents/{agentID}/backends/{backendName}/models
  listBackendModels(agentID: string, backendName: string): Promise<string[]>;

  // healthCheck performs a health check on all active agent sessions.
  healthCheck(): Promise<HealthCheckReport>;

  // subscribeJobEvents subscribes to real-time events for a running job.
  // Returns a cleanup function that unsubscribes and stops the event stream.
  subscribeJobEvents(
    tenantID: string,
    jobID: string,
    onEvent: (event: JobEvent) => void,
  ): () => void;

  // spawnSession creates a new agent session.
  spawnSession(req: SpawnSessionRequest): Promise<SpawnSessionResponse>;

  // promptSession sends a prompt to an existing agent session.
  promptSession(req: PromptSessionRequest): Promise<PromptSessionResponse>;

  // closeSession terminates an agent session.
  closeSession(req: CloseSessionRequest): Promise<CloseSessionResponse>;

  // getSessionHistory fetches a page of historical session events.
  // Used by attach commands and scrollback pagers to browse a session transcript.
  getSessionHistory(req: GetSessionHistoryRequest): Promise<GetSessionHistoryResponse>;

  // subscribeSession returns an async iterable of session events.
  // When req.fromSequence > 0, the stream begins with a historical replay
  // from that sequence, then transitions to live tail when req.follow is true.
  // Use AbortSignal (req.signal) to cancel.
  subscribeSession(req: SubscribeSessionEventsRequest): AsyncIterable<SessionEventEnvelope>;

  // createWorkflow registers a new named workflow definition.
  // Maps to POST /api/v1/orchestrator/workflows
  createWorkflow(req: CreateWorkflowRequest): Promise<CreateWorkflowResponse>;

  // listWorkflows returns all registered workflow definitions for a tenant.
  // Maps to GET /api/v1/orchestrator/workflows?tenant_id=<tenantID>
  listWorkflows(tenantID: string): Promise<ListWorkflowsResponse>;

  // getWorkflow returns a single workflow definition by name.
  // Maps to GET /api/v1/orchestrator/workflows/:name?tenant_id=<tenantID>
  getWorkflow(tenantID: string, name: string): Promise<WorkflowDefinition>;

  // updateWorkflow replaces an existing workflow definition by name.
  // Maps to PUT /api/v1/orchestrator/workflows/:name
  updateWorkflow(name: string, def: WorkflowDefinition): Promise<WorkflowDefinition>;

  // deleteWorkflow removes a workflow definition by name.
  // Maps to DELETE /api/v1/orchestrator/workflows/:name
  deleteWorkflow(tenantID: string, name: string): Promise<void>;

  // createTask creates a new orchestration task.
  // Maps to POST /api/v1/orchestrator/tasks
  createTask(req: CreateTaskRequest): Promise<CreateTaskResponse>;

  // claimTask claims a pending task for execution by an agent.
  // Maps to POST /api/v1/orchestrator/tasks/:taskID/claim
  claimTask(req: ClaimTaskRequest): Promise<void>;

  // completeTask marks a task as completed with an optional result.
  // Maps to POST /api/v1/orchestrator/tasks/:taskID/complete
  completeTask(req: CompleteTaskRequest): Promise<void>;

  // decomposeTask asks the orchestrator to decompose a task into sub-tasks.
  // Maps to POST /api/v1/orchestrator/tasks/:taskID/decompose
  decomposeTask(req: DecomposeTaskRequest): Promise<DecomposeTaskResponse>;

  // registerAgent registers a new agent daemon with the fleet.
  // Maps to POST /api/v1/orchestrator/agents
  registerAgent(req: RegisterAgentRequest): Promise<RegisterAgentResponse>;

  // removeAgent removes a registered agent from the fleet.
  // Maps to DELETE /api/v1/orchestrator/agents/:agentID?tenant_id=<tenantID>
  removeAgent(req: RemoveAgentRequest): Promise<void>;

  // disableAgent administratively disables an agent globally.
  // Maps to POST /api/v1/agents/:agentID/disable?tenant_id=<tenantID>
  disableAgent(req: DisableAgentRequest): Promise<DisableAgentResponse>;

  // enableAgent re-activates a previously disabled agent.
  // Maps to POST /api/v1/agents/:agentID/enable?tenant_id=<tenantID>
  enableAgent(req: EnableAgentRequest): Promise<EnableAgentResponse>;

  // deployAgent provisions a QuicBaaS agent daemon on a remote host over SSH.
  //
  // This operation is only supported by the Wails (desktop) transport. Every
  // other transport rejects the call with DeployNotSupportedError so
  // operators know to invoke the CLI or the desktop app instead.
  deployAgent(spec: DeployAgentSpec): Promise<DeployAgentResult>;

  // inference runs a stateless fleet-routed prompt against a named backend
  // without requiring a prior session spawn. Returns the aggregated response.
  // Maps to POST /api/v1/orchestrator/inference (stream=false).
  inference(req: InferenceRequest): Promise<InferenceResponse>;

  // inferenceStream runs a stateless fleet-routed prompt and returns an async
  // iterable of InferenceChunk values streamed from the backend via chunked
  // NDJSON. The iterable completes when the backend sends a chunk with final=true
  // or the AbortSignal fires.
  // Maps to POST /api/v1/orchestrator/inference (stream=true).
  inferenceStream(req: InferenceRequest, signal?: AbortSignal): AsyncIterable<InferenceChunk>;
}

// SelfRegisterBeginRequest is the payload for POST /api/v1/auth/register/begin.
export interface SelfRegisterBeginRequest {
  // tenant_name is the human-readable name for the new tenant.
  tenant_name: string;
  // email is the registrant's email address.
  email: string;
  // display_name is the registrant's display name.
  display_name: string;
  // isolation_mode selects the tenant isolation strategy: "keyspace" or "barrier".
  isolation_mode?: string;
}

// SelfRegisterBeginResponse is returned by POST /api/v1/auth/register/begin.
export interface SelfRegisterBeginResponse {
  // public_key_credential_creation_options is the WebAuthn challenge for the browser.
  public_key_credential_creation_options: Record<string, unknown>;
  // session_id must be echoed back in the finish request.
  session_id: string;
}

// SelfRegisterFinishRequest is the payload for POST /api/v1/auth/register/finish.
export interface SelfRegisterFinishRequest {
  // session_id returned from the begin call.
  session_id: string;
  // credential is the WebAuthn attestation response from the browser.
  credential: Record<string, unknown>;
}

// SelfRegisterFinishResponse is returned by POST /api/v1/auth/register/finish.
export interface SelfRegisterFinishResponse {
  // status is "active", "pending", or "verification_required".
  status: 'active' | 'pending' | 'verification_required';
  // tenant_id of the newly created tenant (always present).
  tenant_id: string;
  // user_id of the newly created user (always present).
  user_id: string;
  // access_token is present only when status === "active".
  access_token?: string;
  // refresh_token is present only when status === "active".
  refresh_token?: string;
  // message carries a human-readable explanation when status !== "active".
  message?: string;
}

// AuthClient provides authentication operations.
export interface AuthClient {
  // authValidate validates the current session or a provided bearer token.
  // POSTs to /api/v1/auth/validate. Resolves to true when authenticated,
  // false on 400/401, and throws on network or server errors.
  authValidate(token?: string): Promise<boolean>;

  // authLogin authenticates with username+password, returning a session
  // cookie-backed result. On failure throws with the server-reported error
  // message. Reading the cookie is the browser's job; this method just
  // confirms the server accepted the credentials.
  authLogin(credentials: { username: string; password: string }): Promise<void>;

  // webAuthnBeginLogin fetches a WebAuthn challenge from the server for the
  // given tenant and email. Returns the go-webauthn `{ publicKey: { ... } }`
  // envelope verbatim; callers unwrap + base64url-decode it (see the frontend
  // webauthn helper) before navigator.credentials.get().
  // Maps to POST /api/v1/tenants/{tenantID}/webauthn/login/begin.
  webAuthnBeginLogin(tenantID: string, email: string): Promise<CredentialAssertionJSON>;

  // webAuthnFinishLogin submits the signed WebAuthn assertion to the server and
  // returns a token bundle on success.
  // Maps to POST /api/v1/tenants/{tenantID}/webauthn/login/finish.
  webAuthnFinishLogin(
    tenantID: string,
    email: string,
    credential: PublicKeyCredentialJSON,
  ): Promise<AuthTokenResponse>;

  // loginPIV performs PIV smartcard mTLS login. The browser must send a TLS
  // client certificate (credentials: 'include', mode: 'cors') so the server
  // can read r.TLS.PeerCertificates[0] during the handshake.
  // Maps to POST /api/v1/auth/login/piv.
  // Only implemented on the REST transport; non-browser transports return notImpl.
  loginPIV(): Promise<AuthTokenResponse>;

  // listPublicTenants returns the tenants that allow self-service login.
  // Maps to GET /api/v1/iam/tenants/public. No auth required.
  listPublicTenants(): Promise<PublicTenant[]>;

  // registerBegin initiates the self-service tenant registration ceremony.
  // Returns the WebAuthn creation options challenge and a session_id that
  // must be forwarded to registerFinish. Maps to POST /api/v1/auth/register/begin.
  registerBegin(req: SelfRegisterBeginRequest): Promise<SelfRegisterBeginResponse>;

  // registerFinish completes the self-service registration ceremony with the
  // WebAuthn attestation from the browser. On success, the response status
  // is "active" (with tokens), "pending" (awaiting admin approval), or
  // "verification_required" (email verification needed).
  // Maps to POST /api/v1/auth/register/finish.
  registerFinish(req: SelfRegisterFinishRequest): Promise<SelfRegisterFinishResponse>;

  // logout invalidates the current bearer token on the server and clears any
  // client-side session material the SDK is responsible for. Maps to
  // POST /api/v1/auth/logout. Idempotent: succeeds even if the token was
  // already revoked or expired so the UI can rely on it as a clean reset.
  logout(token?: string): Promise<void>;
}

// ---------------------------------------------------------------------------
// Data sub-clients
// ---------------------------------------------------------------------------

// KVEntry represents a single key-value record returned by the store.
export interface KVEntry {
  key: string;
  value: unknown;
}

// KVListResponse is the paginated response from a KV list call.
export interface KVListResponse {
  entries: KVEntry[];
  cursor?: string;
}

// DataClient provides KV store CRUD operations.
// Paths: /api/v1/kv/{type}[/{key}]
export interface DataClient {
  // kvList returns all key-value pairs for the given entity type.
  // Maps to GET /api/v1/kv/{type}
  kvList(entityType: string): Promise<KVListResponse>;

  // kvGet retrieves the value for a single key within an entity type.
  // Maps to GET /api/v1/kv/{type}/{key}
  kvGet(entityType: string, key: string): Promise<KVEntry>;

  // kvPut stores a value under the given key within an entity type.
  // Maps to PUT /api/v1/kv/{type}/{key}
  kvPut(entityType: string, key: string, value: unknown): Promise<void>;

  // kvDelete removes a key from the given entity type.
  // Maps to DELETE /api/v1/kv/{type}/{key}
  kvDelete(entityType: string, key: string): Promise<void>;
}

// ---------------------------------------------------------------------------
// Vector sub-client
// ---------------------------------------------------------------------------

// VectorNamespace is a collection / namespace returned by list operations.
export interface VectorNamespace {
  name: string;
  dimension?: number;
  metric?: string;
  vector_count?: number;
  created_at?: string;
}

// VectorSearchResult is one result from a similarity search.
export interface VectorSearchResult {
  id: string;
  score: number;
  metadata?: Record<string, unknown>;
}

// VectorsClient provides vector-store operations.
// Paths: /api/v1/vector/namespaces[/{namespace}/…]
export interface VectorsClient {
  // listNamespaces returns all vector namespaces (collections).
  // Maps to GET /api/v1/vector/namespaces
  listNamespaces(): Promise<VectorNamespace[]>;

  // createNamespace creates a new vector namespace.
  // Maps to POST /api/v1/vector/namespaces
  createNamespace(req: { name: string; dimension: number; metric?: string }): Promise<VectorNamespace>;

  // deleteNamespace deletes a vector namespace by name.
  // Maps to DELETE /api/v1/vector/namespaces/{namespace}
  deleteNamespace(namespace: string): Promise<void>;

  // searchSimilar performs a similarity search in the given namespace.
  // Maps to POST /api/v1/vector/namespaces/{namespace}/search/similar
  searchSimilar(namespace: string, req: {
    vector?: number[];
    text?: string;
    top_k?: number;
    filter?: Record<string, unknown>;
  }): Promise<VectorSearchResult[]>;
}

// ---------------------------------------------------------------------------
// Time-series sub-client
// ---------------------------------------------------------------------------

// TSPoint is a single time-series data point.
export interface TSPoint {
  metric: string;
  timestamp: string;
  value: number;
  tags?: Record<string, string>;
}

// TSQueryResponse holds the results of a time-series query.
export interface TSQueryResponse {
  points: TSPoint[];
  count: number;
}

// TimeSeriesClient provides time-series write and query operations.
// Paths: /api/v1/timeseries/…
export interface TimeSeriesClient {
  // write writes a single data point.
  // Maps to POST /api/v1/timeseries/write
  write(point: TSPoint): Promise<void>;

  // query queries time-series data for a metric over a time range.
  // Maps to POST /api/v1/timeseries/query
  query(req: {
    metric: string;
    start?: string;
    end?: string;
    tags?: Record<string, string>;
    limit?: number;
  }): Promise<TSQueryResponse>;

  // listMetrics returns all known metric names.
  // Maps to GET /api/v1/timeseries/metrics
  listMetrics(): Promise<string[]>;

  // deleteSeries deletes all data for a metric.
  // Maps to DELETE /api/v1/timeseries/series
  deleteSeries(metric: string): Promise<void>;
}

// ---------------------------------------------------------------------------
// Object-storage sub-client
// ---------------------------------------------------------------------------

// ObjectEntry is a single object in the store.
export interface ObjectEntry {
  key: string;
  size?: number;
  content_type?: string;
  created_at?: string;
  tags?: Record<string, string>;
}

// ObjectListResponse is the paginated response from an object list call.
export interface ObjectListResponse {
  objects: ObjectEntry[];
  cursor?: string;
}

// ObjectsClient provides object-storage CRUD operations.
// Paths: /api/v1/objects[/{key}]
export interface ObjectsClient {
  // listObjects returns objects, optionally filtered by a key prefix.
  // Maps to GET /api/v1/objects
  listObjects(prefix?: string): Promise<ObjectListResponse>;

  // getObject downloads the raw binary content of an object.
  // Maps to GET /api/v1/objects/{key}
  getObject(key: string): Promise<Blob>;

  // putObject uploads binary content to the given key.
  // Maps to PUT /api/v1/objects/{key}
  putObject(key: string, body: Blob | ArrayBuffer, contentType?: string): Promise<void>;

  // deleteObject removes an object by key.
  // Maps to DELETE /api/v1/objects/{key}
  deleteObject(key: string): Promise<void>;
}

// ---------------------------------------------------------------------------
// IAM sub-client
// ---------------------------------------------------------------------------

// UpdateUserRequest holds the fields that may be changed on an existing user.
export interface UpdateUserRequest {
  email?: string;
  display_name?: string;
  enabled?: boolean;
  auth_methods?: string[];
}

// UpdateTenantRequest holds the fields that may be changed on an existing tenant.
export interface UpdateTenantRequest {
  name?: string;
  description?: string;
}

// CreateRoleRequest defines a new role.
export interface CreateRoleRequest {
  name: string;
  description?: string;
  permissions?: string[];
}

// UpdateRoleRequest holds updatable role fields.
export interface UpdateRoleRequest {
  description?: string;
  permissions?: string[];
}

// CreatePolicyRequest defines a new ABAC policy.
export interface CreatePolicyRequest {
  name: string;
  description?: string;
  spec_yaml?: string;
}

// UpdatePolicyRequest holds updatable policy fields.
export interface UpdatePolicyRequest {
  description?: string;
  spec_yaml?: string;
}

// IAMClient provides identity and access management operations.
// Paths: /api/v1/tenants and /api/v1/iam
export interface IAMClient {
  // listUsers returns all users within a tenant.
  // Maps to GET /api/v1/tenants/{tenantID}/users
  listUsers(tenantID: string): Promise<ListUsersResponse>;

  // createUser creates a new user within a tenant.
  // Maps to POST /api/v1/tenants/{tenantID}/users
  createUser(tenantID: string, req: CreateUserRequest): Promise<CreateUserResponse>;

  // updateUser updates a user's mutable fields.
  // Maps to PUT /api/v1/tenants/{tenantID}/users/{userID}
  updateUser(tenantID: string, userID: string, req: UpdateUserRequest): Promise<IAMUser>;

  // deleteUser removes a user from a tenant.
  // Maps to DELETE /api/v1/tenants/{tenantID}/users/{userID}
  deleteUser(tenantID: string, userID: string): Promise<void>;

  // assignRoleToUser grants a role to a user.
  // Maps to POST /api/v1/tenants/{tenantID}/users/{userID}/roles
  assignRoleToUser(tenantID: string, userID: string, role: string): Promise<void>;

  // revokeRoleFromUser removes a role from a user.
  // Maps to DELETE /api/v1/tenants/{tenantID}/users/{userID}/roles/{role}
  revokeRoleFromUser(tenantID: string, userID: string, role: string): Promise<void>;

  // listTenants returns all tenants.
  // Maps to GET /api/v1/tenants
  listTenants(): Promise<ListTenantsResponse>;

  // createTenant creates a new tenant.
  // Maps to POST /api/v1/tenants
  createTenant(req: CreateTenantRequest): Promise<CreateTenantResponse>;

  // updateTenant updates a tenant's mutable fields.
  // Maps to PUT /api/v1/tenants/{tenantID}
  updateTenant(tenantID: string, req: UpdateTenantRequest): Promise<IAMTenant>;

  // deleteTenant removes a tenant.
  // Maps to DELETE /api/v1/tenants/{tenantID}
  deleteTenant(tenantID: string): Promise<void>;

  // listRoles returns all roles within a tenant.
  // Maps to GET /api/v1/tenants/{tenantID}/roles
  listRoles(tenantID: string): Promise<ListRolesResponse>;

  // createRole creates a new role within a tenant.
  // Maps to POST /api/v1/tenants/{tenantID}/roles
  createRole(tenantID: string, req: CreateRoleRequest): Promise<IAMRole>;

  // updateRole updates a role's mutable fields.
  // Maps to PUT /api/v1/tenants/{tenantID}/roles/{roleID}
  updateRole(tenantID: string, roleID: string, req: UpdateRoleRequest): Promise<IAMRole>;

  // deleteRole removes a role from a tenant.
  // Maps to DELETE /api/v1/tenants/{tenantID}/roles/{roleID}
  deleteRole(tenantID: string, roleID: string): Promise<void>;

  // listPolicies returns all ABAC policies within a tenant.
  // Maps to GET /api/v1/tenants/{tenantID}/policies
  listPolicies(tenantID: string): Promise<ListPoliciesResponse>;

  // createPolicy creates a new ABAC policy.
  // Maps to POST /api/v1/tenants/{tenantID}/policies
  createPolicy(tenantID: string, req: CreatePolicyRequest): Promise<IAMPolicy>;

  // updatePolicy updates an existing ABAC policy.
  // Maps to PUT /api/v1/tenants/{tenantID}/policies/{policyName}
  updatePolicy(tenantID: string, policyName: string, req: UpdatePolicyRequest): Promise<IAMPolicy>;

  // deletePolicy removes an ABAC policy.
  // Maps to DELETE /api/v1/tenants/{tenantID}/policies/{policyName}
  deletePolicy(tenantID: string, policyName: string): Promise<void>;

  // getInvite returns publicly visible metadata for a pending invite.
  // Unauthenticated — use to populate the activation UI.
  // Maps to GET /api/v1/iam/invites/{token}
  getInvite(token: string): Promise<import('./types.js').InviteMetadata>;

  // activateFIDO2Begin starts a WebAuthn credential creation ceremony.
  // Maps to POST /api/v1/iam/invites/{token}/activate/fido2/begin
  activateFIDO2Begin(token: string): Promise<Record<string, unknown>>;

  // activateFIDO2Finish completes the WebAuthn registration.
  // Maps to POST /api/v1/iam/invites/{token}/activate/fido2/finish
  activateFIDO2Finish(token: string, credential: Record<string, unknown>): Promise<import('./types.js').ActivationResponse>;

  // activatePIV activates the user with a PIV X.509 certificate PEM block.
  // Maps to POST /api/v1/iam/invites/{token}/activate/piv
  activatePIV(token: string, certPEM: string): Promise<import('./types.js').ActivationResponse>;
}

// ---------------------------------------------------------------------------
// KMS sub-client
// ---------------------------------------------------------------------------

// KMSClient provides key management service operations.
// Paths: /api/v1/kms
export interface KMSClient {
  // listKeys returns all cryptographic keys, up to the given limit.
  // Maps to GET /api/v1/kms/keys?limit={limit}
  listKeys(limit?: number): Promise<ListKeysResponse>;

  // createKey generates a new cryptographic key.
  // Maps to POST /api/v1/kms/keys
  createKey(req: CreateKeyRequest): Promise<CreateKeyResponse>;

  // rotateKey generates a new version of an existing key.
  // Maps to POST /api/v1/kms/keys/{name}/rotate
  rotateKey(name: string): Promise<RotateKeyResponse>;

  // deleteKey schedules a key for deletion.
  // Maps to DELETE /api/v1/kms/keys/{name}
  deleteKey(name: string): Promise<void>;

  // encrypt encrypts plaintext using a named key.
  // Maps to POST /api/v1/kms/encrypt/{name}
  encrypt(req: KMSEncryptRequest): Promise<KMSEncryptResponse>;

  // decrypt decrypts ciphertext using a named key.
  // Maps to POST /api/v1/kms/decrypt/{name}
  decrypt(req: KMSDecryptRequest): Promise<KMSDecryptResponse>;

  // listKeyVersions returns all versions for a key.
  // Maps to GET /api/v1/kms/keys/{name}/versions
  listKeyVersions(name: string): Promise<KMSKeyVersion[]>;

  // updateKeyStatus enables or disables a key.
  // Maps to PUT /api/v1/kms/keys/{name}/status
  updateKeyStatus(name: string, status: 'enabled' | 'disabled'): Promise<void>;

  // setDefaultKey marks the named key as the system default.
  // Maps to PUT /api/v1/kms/keys/{name}/default
  setDefaultKey(name: string): Promise<SetDefaultKeyResponse>;
}

// KMSKeyVersion represents a single version of a cryptographic key.
export interface KMSKeyVersion {
  version: number;
  status: string;
  created_at?: string;
  destroyed_at?: string;
}

// ---------------------------------------------------------------------------
// Secrets sub-client
// ---------------------------------------------------------------------------

// SecretsClient provides secrets engine operations.
// Paths: /api/v1/secrets
export interface SecretsClient {
  // listSecrets returns all secret paths up to the given limit.
  // Maps to GET /api/v1/secrets?limit={limit}
  listSecrets(limit?: number): Promise<ListSecretsResponse>;

  // storeSecret stores a secret at the given path.
  // Maps to POST /api/v1/secrets/{path}
  storeSecret(req: StoreSecretRequest): Promise<void>;

  // getSecret retrieves a secret by path.
  // Maps to GET /api/v1/secrets/{path}
  getSecret(path: string): Promise<GetSecretResponse>;

  // deleteSecret removes a secret by path.
  // Maps to DELETE /api/v1/secrets/{path}
  deleteSecret(path: string): Promise<void>;

  // rotateSecret rotates a secret, creating a new version.
  // Maps to POST /api/v1/secrets/{path}?action=rotate
  rotateSecret(path: string): Promise<RotateSecretResponse>;

  // listVersions returns all versions of a secret by path.
  // Maps to GET /api/v1/secrets-versions/{path}
  listVersions(path: string): Promise<ListSecretVersionsResponse>;
}

// ---------------------------------------------------------------------------
// Barrier sub-client (replaces SealClient)
// ---------------------------------------------------------------------------

// BarrierClient manages the cryptographic barrier that protects data at
// rest. Paths: /api/v1/barrier. Mirrors go-qrdb's transport.BarrierClient
// surface. Replaces the legacy SealClient interface; existing
// SealClient-typed callers continue to work via the deprecation alias below.
export interface BarrierClient {
  // status returns the current sealed/unsealed state of the barrier.
  // Maps to GET /api/v1/barrier/status.
  status(): Promise<BarrierStatusResponse>;

  // initialize initialises the barrier with a single master key (passphrase).
  // Maps to POST /api/v1/barrier/initialize.
  initialize(req: BarrierInitRequest): Promise<void>;

  // unseal unseals the barrier using the master key (passphrase strategy).
  // Maps to POST /api/v1/barrier/unseal.
  unseal(req: BarrierUnsealRequest): Promise<void>;

  // seal transitions the barrier to the sealed state.
  // Maps to POST /api/v1/barrier/seal.
  seal(): Promise<void>;

  // initializeShamir initialises the barrier with Shamir secret sharing.
  // Maps to POST /api/v1/barrier/shamir/initialize.
  initializeShamir(req: BarrierShamirInitRequest): Promise<BarrierShamirInitResponse>;

  // unsealWithShare submits a single Shamir share toward the unseal threshold.
  // Maps to POST /api/v1/barrier/shamir/unseal-share.
  unsealWithShare(req: BarrierUnsealShareRequest): Promise<BarrierUnsealShareResponse>;

  // unsealWithShares submits multiple Shamir shares in a single call.
  // Maps to POST /api/v1/barrier/shamir/unseal-shares.
  unsealWithShares(req: BarrierUnsealSharesRequest): Promise<void>;

  // shamirListShares returns metadata about persisted Shamir shares.
  // Maps to GET /api/v1/barrier/shamir/shares.
  shamirListShares(): Promise<BarrierShamirSharesResponse>;

  // shamirDeleteShare removes a single Shamir share by index.
  // Maps to DELETE /api/v1/barrier/shamir/shares/{index}.
  shamirDeleteShare(req: BarrierShamirDeleteShareRequest): Promise<void>;

  // shamirDeleteAllShares removes all persisted Shamir shares.
  // Maps to DELETE /api/v1/barrier/shamir/shares.
  shamirDeleteAllShares(): Promise<void>;

  // shamirVerify validates the consistency of persisted shares.
  // Maps to POST /api/v1/barrier/shamir/verify.
  shamirVerify(): Promise<void>;

  // rekey generates new Shamir shares for the existing root key.
  // Maps to POST /api/v1/barrier/rekey.
  rekey(req: BarrierRekeyRequest): Promise<BarrierRekeyResponse>;

  // generateRecoveryKeys generates Shamir recovery shares for disaster recovery.
  // Maps to POST /api/v1/barrier/recovery/generate.
  generateRecoveryKeys(req: BarrierGenerateRecoveryKeysRequest): Promise<BarrierRecoveryKeysResponse>;

  // recoverWithKeys unseals the barrier using recovery shares.
  // Maps to POST /api/v1/barrier/recovery/recover.
  recoverWithKeys(req: BarrierRecoverWithKeysRequest): Promise<void>;

  // deleteRecoveryKeys removes recovery key metadata from storage.
  // Maps to DELETE /api/v1/barrier/recovery.
  deleteRecoveryKeys(): Promise<void>;

  // recoveryStatus reports whether recovery key metadata exists.
  // Maps to GET /api/v1/barrier/recovery/status.
  recoveryStatus(): Promise<BarrierHasRecoveryKeysResponse>;

  // generateRootToken creates a new root token by proving knowledge of the
  // master key through Shamir shares.
  // Maps to POST /api/v1/barrier/root-token.
  generateRootToken(req: BarrierGenerateRootTokenRequest): Promise<BarrierRootTokenResponse>;
}

// ---------------------------------------------------------------------------
// Tenant-barrier sub-client
// ---------------------------------------------------------------------------

// TenantBarrierClient manages the dedicated cryptographic barrier owned by a
// single tenant. It is distinct from BarrierClient, which operates the
// process-wide system barrier. Each method is scoped to a tenant ID and maps
// to the broker's /api/v1/tenants/{id}/barrier/* REST surface.
export interface TenantBarrierClient {
  // tenantBarrierInit initializes the tenant's barrier with the chosen
  // strategy. The "shamir" strategy returns the minted shares; the "none" and
  // "passphrase" strategies return 204 No Content (undefined).
  // Maps to POST /api/v1/tenants/{id}/barrier/init.
  tenantBarrierInit(
    tenantId: string,
    req: TenantBarrierInitRequest,
  ): Promise<TenantBarrierInitResponse | undefined>;

  // tenantBarrierUnseal unseals the tenant's barrier with a single master key
  // (passphrase strategy). The key is the base64-encoded master-key bytes.
  // Maps to POST /api/v1/tenants/{id}/barrier/unseal.
  tenantBarrierUnseal(tenantId: string, key: string): Promise<void>;

  // tenantBarrierUnsealShare submits a single Shamir share toward the tenant's
  // unseal quorum and reports progress.
  // Maps to POST /api/v1/tenants/{id}/barrier/unseal-share.
  tenantBarrierUnsealShare(
    tenantId: string,
    share: string,
  ): Promise<TenantBarrierUnsealShareResponse>;

  // tenantBarrierSeal transitions the tenant's barrier to the sealed state.
  // Maps to POST /api/v1/tenants/{id}/barrier/seal.
  tenantBarrierSeal(tenantId: string): Promise<void>;

  // tenantBarrierStatus returns the current state of the tenant's barrier.
  // Maps to GET /api/v1/tenants/{id}/barrier/status.
  tenantBarrierStatus(tenantId: string): Promise<TenantBarrierStatusResponse>;
}

/**
 * SealClient is the legacy alias for BarrierClient. Its surface is preserved
 * for one release so existing callers continue to compile. New code must use
 * BarrierClient directly.
 *
 * The runtime RestSealClient wraps RestBarrierClient and emits a single
 * deprecation warning per construction. See rest.ts for the implementation.
 *
 * @deprecated Use BarrierClient. The Seal* surface will be removed one
 *   release after the Barrier unification ships.
 */
export interface SealClient {
  status(): Promise<SealStatus>;
  unseal(req: UnsealRequest): Promise<UnsealResponse>;
  seal(): Promise<void>;
  rekey(req: RekeyRequest): Promise<RekeyResponse>;
  rotateRootKey(): Promise<RotateRootKeyResponse>;
}

// ---------------------------------------------------------------------------
// Ceremony sub-client
// ---------------------------------------------------------------------------

// CeremonyClient drives the initialization ceremony lifecycle including
// M-of-N officer enrollment, certificate claims, share distribution, and
// CSR signing. Paths: /api/v1/init.
export interface CeremonyClient {
  // initialize starts the initialization ceremony.
  // Maps to POST /api/v1/init/initialize.
  initialize(req: CeremonyInitializeRequest): Promise<CeremonyInitializeResponse>;

  // status returns the current ceremony state and SPKI pin.
  // Maps to GET /api/v1/init/status.
  status(): Promise<CeremonyStatusResponse>;

  // claimCertBegin starts a certificate claim challenge for an officer.
  // Maps to POST /api/v1/init/claim-cert/begin.
  claimCertBegin(req: CeremonyClaimCertBeginRequest): Promise<CeremonyClaimCertBeginResponse>;

  // claimCertComplete completes the certificate claim by submitting the
  // signed nonce. Maps to POST /api/v1/init/claim-cert/complete.
  claimCertComplete(req: CeremonyClaimCertCompleteRequest): Promise<CeremonyClaimCertCompleteResponse>;

  // claimShare retrieves the Shamir share for an enrolled officer who has
  // already claimed their certificate.
  // Maps to POST /api/v1/init/claim-share.
  claimShare(req: CeremonyClaimShareRequest): Promise<CeremonyClaimShareResponse>;

  // signCSR signs a CSR during or after the ceremony.
  // Maps to POST /api/v1/init/sign-csr.
  signCSR(req: CeremonySignCSRRequest): Promise<CeremonySignCSRResponse>;

  // enrollExternalOfficer enrols an officer whose key material lives outside
  // the daemon. Either cert_pem or pubkey_pem must be supplied (not both).
  // Maps to POST /api/v1/init/enroll-external-officer.
  enrollExternalOfficer(
    req: CeremonyEnrollExternalOfficerRequest,
  ): Promise<CeremonyEnrollExternalOfficerResponse>;
}

// ---------------------------------------------------------------------------
// Bootstrap sub-client
// ---------------------------------------------------------------------------

// BootstrapClient retrieves the CA certificate bundle through one of the
// supported trust bootstrap methods (auto, dane, noise, spki). The methods
// are intended for first-contact use before TLS is available; each one
// advertises a different verification mechanism. Paths: /api/v1/bootstrap.
export interface BootstrapClient {
  // auto attempts every configured bootstrap method in priority order and
  // returns the first successful CA bundle. Maps to POST /api/v1/bootstrap/auto.
  auto(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;

  // dane retrieves the CA bundle using DANE/TLSA DNS verification.
  // Maps to POST /api/v1/bootstrap/dane.
  dane(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;

  // noise retrieves the CA bundle through the Noise_NK protocol.
  // Maps to POST /api/v1/bootstrap/noise.
  noise(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;

  // spki retrieves the CA bundle using SPKI-pinned TLS.
  // Maps to POST /api/v1/bootstrap/spki.
  spki(req?: BootstrapBundleRequest): Promise<BootstrapBundleResponse>;
}

// SSHKeysClient provides access to stored SSH key identities.
// All keys are generated server-side and the private material never
// leaves xkms. The client only manages the public half and the
// operator-facing metadata.
//
// Legacy flat methods (list/create/get/remove) are backward-compatible
// with Wails desktop callers and hit the new /api/v1/ssh/keys surface
// with server-side scope/owner defaults.
//
// Scoped methods (listScoped, getScoped, deleteScoped, etc.) expose the
// full scope/owner/name triple for Phase 3 Svelte views and CLI callers.
export interface SSHKeysClient {
  // list returns all stored SSH key entries using server-side scope/owner
  // defaults. Maps to GET /api/v1/ssh/keys.
  list(): Promise<ListSSHKeysResponse>;

  // create generates a new SSH key inside xkms and returns the public
  // half in OpenSSH authorized_keys format. Maps to
  // POST /api/v1/ssh/keys with a JSON body containing name and algorithm.
  create(req: CreateSSHKeyRequest): Promise<SSHKey>;

  // get fetches a single stored SSH key identity by name using server-side
  // scope/owner defaults. Maps to GET /api/v1/ssh/keys/{scope}/{owner}/{name}.
  get(name: string): Promise<SSHKey>;

  // remove deletes the stored identity and its underlying xkms key using
  // server-side scope/owner defaults. Named remove because delete is a
  // reserved word in JavaScript.
  remove(name: string): Promise<void>;

  // listScoped returns keys filtered by the supplied scope/owner pair.
  // Maps to GET /api/v1/ssh/keys?scope=&owner=
  listScoped(query?: ListSSHKeysQuery): Promise<ListSSHKeysResponse>;

  // getScoped fetches a single key by the full scope/owner/name triple.
  // Maps to GET /api/v1/ssh/keys/{scope}/{owner}/{name}.
  getScoped(scope: SSHScope, owner: string, name: string): Promise<SSHKey>;

  // deleteScoped removes a key by the full scope/owner/name triple.
  // Maps to DELETE /api/v1/ssh/keys/{scope}/{owner}/{name}.
  deleteScoped(scope: SSHScope, owner: string, name: string): Promise<void>;

  // exportPublic returns the authorized_keys-format public key for the given key.
  // Maps to GET /api/v1/ssh/keys/{scope}/{owner}/{name}/public.
  exportPublic(scope: SSHScope, owner: string, name: string): Promise<SSHPublicKey>;

  // sign signs a raw digest with the named key.
  // Maps to POST /api/v1/ssh/keys/{scope}/{owner}/{name}/sign.
  sign(scope: SSHScope, owner: string, name: string, req: SignRequest): Promise<SignResponse>;

  // import_ imports a PEM-encoded private key. Trailing underscore because
  // 'import' is a reserved word in TypeScript/JavaScript.
  // Maps to POST /api/v1/ssh/keys/import.
  import_(req: ImportSSHKeyRequest): Promise<SSHKey>;
}

// SSHKnownHostsClient manages the host-key pin store that prevents MITM
// attacks during SSH connections. Pins are scoped to (scope, owner) pairs.
// Paths: /api/v1/ssh/known-hosts
export interface SSHKnownHostsClient {
  // add pins a host-key fingerprint. Maps to POST /api/v1/ssh/known-hosts.
  add(req: AddSSHPinRequest): Promise<void>;

  // list enumerates pinned host keys, optionally filtered by scope/owner.
  // Maps to GET /api/v1/ssh/known-hosts?scope=&owner=
  list(query?: ListSSHKeysQuery): Promise<ListSSHPinsResponse>;

  // remove removes a pinned host key.
  // Maps to DELETE /api/v1/ssh/known-hosts/{host}?scope=&owner=
  remove(host: string, query?: ListSSHKeysQuery): Promise<void>;

  // verify checks the presented fingerprint against the stored pin.
  // Throws SSHHostMismatchError when the fingerprints differ,
  // SSHNotFoundError when no pin exists for the host.
  // Maps to POST /api/v1/ssh/known-hosts/{host}/verify.
  verify(host: string, req: VerifySSHPinRequest): Promise<void>;
}

// SSHAuthorizedKeysClient manages authorized_keys entries and line formatting.
// Paths: /api/v1/ssh/authorized
export interface SSHAuthorizedKeysClient {
  // emit formats a public key into an authorized_keys line.
  // Maps to POST /api/v1/ssh/authorized.
  emit(req: EmitAuthorizedRequest): Promise<EmitAuthorizedResponse>;

  // verify checks whether a fingerprint matches an authorized entry for
  // the given scope/owner. Returns the matching entry.
  // Maps to POST /api/v1/ssh/authorized/verify.
  verify(req: VerifyAuthorizedRequest): Promise<VerifyAuthorizedResponse>;
}

// SSHClient is the top-level SSH namespace exposed on the RestClient as
// client.ssh. It groups the three SSH sub-clients behind a single property.
export interface SSHClient {
  /** SSH key management. */
  readonly keys: SSHKeysClient;
  /** Host-key pin store (MITM-safe SSH). */
  readonly knownHosts: SSHKnownHostsClient;
  /** Authorized-keys entry management. */
  readonly authorized: SSHAuthorizedKeysClient;
}

// Client is the top-level SDK entry point, combining connection management
// with all orchestration operations.
export interface Client {
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
  /**
   * barrier is the canonical surface for sealed-storage operations.
   * Replaces the legacy seal accessor; new code must use barrier.
   */
  readonly barrier: BarrierClient;
  /**
   * tenantBarrier operates the per-tenant cryptographic barrier for tenants
   * that select the "barrier" isolation mode. It is scoped per tenant ID and
   * is independent of the process-wide system barrier exposed by `barrier`.
   */
  readonly tenantBarrier: TenantBarrierClient;
  /**
   * ceremony drives the initialization ceremony lifecycle (M-of-N officer
   * enrollment, certificate claims, share distribution, CSR signing).
   */
  readonly ceremony: CeremonyClient;
  /**
   * bootstrap retrieves the CA certificate bundle through one of the
   * supported trust bootstrap methods (auto, dane, noise, spki).
   */
  readonly bootstrap: BootstrapClient;
  /**
   * @deprecated Use barrier. Kept for one release so existing callers
   * continue to compile. The seal accessor proxies to barrier.
   */
  readonly seal: SealClient;
  /**
   * sshKeys is the legacy flat accessor for backward compatibility.
   * Existing callers using client.sshKeys.list()/create()/get()/remove()
   * continue to work unchanged.
   */
  readonly sshKeys: SSHKeysClient;
  /**
   * ssh exposes the full scoped SSH surface introduced in Phase 2b.
   * client.ssh.keys  → SSHKeysClient (same instance as sshKeys)
   * client.ssh.knownHosts → SSHKnownHostsClient
   * client.ssh.authorized → SSHAuthorizedKeysClient
   */
  readonly ssh: SSHClient;
}

// SessionInfo re-exported for convenience — used by listSessions in the legacy
// Wails binding which returns the older flat array form.
export type { SessionInfo };
