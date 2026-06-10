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

// =============================================================================
// Job Orchestration Types
// Mirrors pkg/orchestration/orchestrator_types.go
// =============================================================================

// JobState represents the lifecycle state of an orchestration job.
// Wails auto-converts Go PascalCase JSON tags to camelCase; all fields use
// the snake_case JSON tags from Go, received as-is from the REST API.
export type JobState =
  | 'initializing'
  | 'decomposing'
  | 'scheduling'
  | 'assigning'
  | 'executing'
  | 'validating'
  | 'reviewing'
  | 're-delegating'
  | 'completing'
  | 'cleanup'
  | 'escalating'
  | 'completed'
  | 'failed';

// JobAuditEvent records a single event in a job's complete audit trail.
// Mirrors orchestration.JobAuditEvent.
export interface JobAuditEvent {
  timestamp: number;
  type: string;
  details: string;
}

// FailedApproach records a failed work item with context for why it failed.
// Mirrors orchestration.FailedApproach.
export interface FailedApproach {
  work_item_title: string;
  approach: string;
  reason: string;
  iteration: number;
}

// JobMemory accumulates context across iterations.
// Mirrors orchestration.JobMemory.
export interface JobMemory {
  successful_approaches: string[];
  failed_approaches: FailedApproach[];
  constraints: string[];
  review_feedback: string[];
}

// WorkItem represents a decomposed unit of work.
// Mirrors orchestration.WorkItem.
export interface WorkItem {
  id: string;
  project_id: string;
  title: string;
  description: string;
  priority: number;
  dependencies: string[];
  backend: string;
  required_capabilities: string[];
  required_role: string;
  secret_refs: string[];
  max_retries: number;
  parallelizable: boolean;
  fallback_backend: string;
}

// JobConfig configures an orchestration job.
// Mirrors orchestration.JobConfig.
export interface JobConfig {
  tenant_id: string;
  description: string;
  lead_backend: string;
  lead_model: string;
  project_id: string;
  poll_interval: number;
  max_iterations: number;
  workflow_name: string;
}

// JobStatus reports the current state of an orchestration job.
// Mirrors orchestration.JobStatus.
export interface JobStatus {
  job_id: string;
  tenant_id: string;
  state: JobState;
  iteration: number;
  task_count: number;
  completed: number;
  failed: number;
  description: string;
  started_at: number;
  updated_at: number;
  error: string;
  events: JobAuditEvent[];
  work_items: WorkItem[];
  config: JobConfig | null;
  memory: JobMemory | null;
  workflow_name: string;
}

// JobEvent represents a real-time event from a running orchestration job.
// Mirrors orchestration.JobEvent.
export interface JobEvent {
  job_id: string;
  type: string;
  state: JobState;
  message: string;
  iteration: number;
  metadata: Record<string, string>;
  timestamp: number;
}

// StartJobRequest starts an autonomous orchestration job.
// Mirrors orchestration.StartJobRequest.
export interface StartJobRequest {
  tenant_id: string;
  description: string;
  workflow_name: string;
  lead_backend: string;
  lead_model: string;
  project_id: string;
  max_iterations: number;
  poll_interval?: string;
  workflow_file?: string;
}

// StartJobResponse contains the result of starting an orchestration job.
// Mirrors orchestration.StartJobResponse.
export interface StartJobResponse {
  job_id: string;
  timestamp: number;
}

// GetJobStatusRequest retrieves the current status of a running job.
// Mirrors orchestrator.GetJobStatusRequest.
export interface GetJobStatusRequest {
  tenant_id: string;
  job_id: string;
}

// ListJobsRequest lists all jobs for a tenant.
// Mirrors orchestrator.ListJobsRequest.
export interface ListJobsRequest {
  tenant_id: string;
}

// ListJobsResponse is the response from listing jobs.
// Mirrors orchestrator.ListJobsResponse.
export interface ListJobsResponse {
  jobs: JobStatus[];
}

// CancelJobRequest cancels a running orchestration job.
// Mirrors orchestrator.CancelJobRequest.
export interface CancelJobRequest {
  tenant_id: string;
  job_id: string;
}

// SubscribeJobEventsRequest subscribes to real-time events for a job.
// Mirrors orchestrator.SubscribeJobEventsRequest.
export interface SubscribeJobEventsRequest {
  tenant_id: string;
  job_id: string;
}

// GetTaskRequest retrieves a single task by ID.
// Mirrors orchestrator.GetTaskRequest.
export interface GetTaskRequest {
  tenant_id: string;
  task_id: string;
}

// ListTasksRequest lists tasks for a tenant.
// Mirrors orchestrator.ListTasksRequest.
export interface ListTasksRequest {
  tenant_id: string;
  status?: string;
  team_name?: string;
  assigned_to?: string;
}

// ListTasksResponse is the response from listing tasks.
// Mirrors orchestrator.ListTasksResponse.
export interface ListTasksResponse {
  tasks: Task[];
  count: number;
}

// =============================================================================
// Agent Registry Types
// Mirrors pkg/orchestration/agent_registry_types.go
// =============================================================================

// AgentStatus represents the lifecycle state of a registered agent.
export type AgentStatus =
  | 'registered'
  | 'running'
  | 'idle'
  | 'offline'
  | 'error'
  | 'disabled';

// AgentRegistration represents a registered agent in the fleet registry.
// Mirrors orchestration.AgentRegistration.
export interface AgentRegistration {
  id: string;
  tenant_id: string;
  name: string;
  host: string;
  backend: string;
  model: string;
  agent_type: string;
  status: AgentStatus;
  capabilities: string[];
  container_id: string;
  work_dir: string;
  last_seen: number;
  registered_at: number;
  disabled_at?: number;
  version: number;
  metadata: Record<string, string>;
}

// DisableAgentRequest identifies an agent to disable globally.
export interface DisableAgentRequest {
  tenant_id: string;
  agent_id: string;
}

// DisableAgentResponse is returned after a successful agent disable.
export interface DisableAgentResponse {
  agent_id: string;
  disabled: boolean;
  disabled_at: string;
  timestamp: string;
}

// EnableAgentRequest identifies a disabled agent to re-enable.
export interface EnableAgentRequest {
  tenant_id: string;
  agent_id: string;
}

// EnableAgentResponse is returned after a successful agent enable.
export interface EnableAgentResponse {
  agent_id: string;
  enabled: boolean;
  timestamp: string;
}

// =============================================================================
// Session Types
// Mirrors pkg/orchestration/orchestrator_types.go (SessionInfo)
// and pkg/orchestration/backend.go (SessionState)
// =============================================================================

// SessionState represents the state of an agent session.
export type SessionState = 'active' | 'idle' | 'closed' | 'error';

// SessionInfo contains summary information about an active agent session.
// Mirrors orchestration.SessionInfo.
export interface SessionInfo {
  session_id: string;
  project_id: string;
  backend: string;
  model: string;
  state: SessionState;
  created_at: number;
  last_activity: number;
}

// =============================================================================
// Task Types
// Mirrors pkg/orchestration/task_types.go
// =============================================================================

// TaskStatus represents the lifecycle state of a task.
export type TaskStatus =
  | 'pending'
  | 'claimed'
  | 'in_progress'
  | 'completed'
  | 'failed'
  | 'cancelled';

// TaskResult contains the output of a completed task.
// Mirrors orchestration.TaskResult.
export interface TaskResult {
  output: string;
  files: string[];
  metadata: Record<string, string>;
}

// Task represents an orchestration work item managed by the task store.
// Mirrors orchestration.Task.
export interface Task {
  id: string;
  tenant_id: string;
  team_name: string;
  subject: string;
  description: string;
  status: TaskStatus;
  assigned_to: string;
  created_by: string;
  dependencies: string[];
  blocks: string[];
  result: TaskResult | null;
  metadata: Record<string, string>;
  created_at: number;
  updated_at: number;
  claimed_at: number;
  completed_at: number;
  version: number;
  /** auto_spawn is true when the task was created with immediate dispatch. */
  auto_spawn?: boolean;
  /** spawned_job_id is the ID of the auto-spawned job, empty when not spawned. */
  spawned_job_id?: string;
  /** spawned_at is a nanosecond Unix timestamp of when the job was dispatched. */
  spawned_at?: number;
}

// =============================================================================
// Backend Types
// Mirrors pkg/orchestration/backend.go
// =============================================================================

// BackendKind classifies a backend as an agent or model server.
export type BackendKind = 'agent' | 'model_server' | 'api_inference';

// BackendCapabilities describes what a backend supports.
// Mirrors orchestration.BackendCapabilities.
export interface BackendCapabilities {
  kind: BackendKind;
  persistent_sessions: boolean;
  tool_use: boolean;
  file_access: boolean;
  streaming: boolean;
  acp: boolean;
  team_management: boolean;
}

// AgentBackendInfo contains information about a registered agent backend.
// Mirrors orchestration.AgentBackendInfo.
export interface AgentBackendInfo {
  name: string;
  type: string;
  capabilities: BackendCapabilities;
  default_model: string;
}

// =============================================================================
// Health Types
// Mirrors pkg/orchestration/health.go
// =============================================================================

// SessionHealthResult contains the health check result for a single session.
// Mirrors orchestration.SessionHealthResult.
export interface SessionHealthResult {
  session_id: string;
  backend: string;
  state: SessionState;
  healthy: boolean;
  error: string;
}

// HealthCheckReport contains the results of checking all active sessions.
// Mirrors orchestration.HealthCheckReport.
export interface HealthCheckReport {
  timestamp: number;
  total_sessions: number;
  healthy: number;
  unhealthy: number;
  closed: number;
  results: SessionHealthResult[];
}

// =============================================================================
// Cost Tracker Types
// Mirrors pkg/orchestration/cost_tracker.go
// =============================================================================

// BudgetStatus represents the current budget state.
export type BudgetStatus = 'ok' | 'warning' | 'exceeded';

// UsageReport contains token usage details for a job or agent.
// Mirrors orchestration.UsageReport.
export interface UsageReport {
  total_tokens: number;
  budget_limit: number;
  percent_used: number;
  by_agent: Record<string, number>;
  last_updated: number;
}

// =============================================================================
// Peer Messaging Types
// Mirrors pkg/orchestration/peer_messaging.go
// =============================================================================

// PeerMessage represents a message in an agent's inbox.
// Mirrors orchestration.PeerMessage.
export interface PeerMessage {
  id: string;
  from: string;
  to: string;
  action: string;
  channel: string;
  content: string;
  metadata: Record<string, string>;
  timestamp: number;
  ttl: number;
}

// =============================================================================
// File Lock Types
// Mirrors pkg/orchestration/file_lock.go
// =============================================================================

// FileLock represents an active lock on a file path.
// Mirrors orchestration.FileLock.
export interface FileLock {
  lock_key: string;
  agent_id: string;
  tenant_id: string;
  project_id: string;
  file_path: string;
  lease_id: string;
  fencing_token: number;
  acquired_at: number;
  isolation: string;
}

// =============================================================================
// Workflow Types
// Mirrors pkg/orchestration/workflow_types.go
// =============================================================================

// CommunicationTopology defines the inter-agent messaging topology.
export type CommunicationTopology = 'hub_and_spoke' | 'hybrid' | 'peer_to_peer';

// WorkspaceIsolation defines how agents share or isolate workspaces.
export type WorkspaceIsolation = 'shared' | 'worktree';

// WorkflowMode defines the operational mode for an orchestration workflow.
export type WorkflowMode = 'canned' | 'constrained' | 'autonomous';

// AgentRoleSpec defines a single agent role within a workflow.
// Mirrors orchestration.AgentRoleSpec.
export interface AgentRoleSpec {
  role: string;
  backend: string;
  model: string;
  capabilities: string[];
  mcp_scopes: string[];
  count: number;
  workspace_path: string;
  permission_mode: string;
  skip_permissions: boolean;
  env: Record<string, string>;
  sandboxed: boolean;
  container_image: string;
  num_ctx: number;
}

// PeerMessagingSettings controls peer-to-peer messaging behavior.
// Mirrors orchestration.PeerMessagingSettings.
export interface PeerMessagingSettings {
  max_message_size: number;
  max_inbox_size: number;
  overflow_action: string;
  inbox_ttl: string;
}

// FileLockSettings controls distributed file lock behavior.
// Mirrors orchestration.FileLockSettings.
export interface FileLockSettings {
  lease_ttl: string;
  heartbeat_interval: string;
  max_renewals: number;
}

// VectorDBSettings controls auto-indexing vector database behavior.
// Mirrors orchestration.VectorDBSettings.
export interface VectorDBSettings {
  enabled: boolean;
  index_type: string;
  auto_index: boolean;
  triggers: string[];
  gpu_accelerated: boolean;
}

// CostControlSettings controls token budget limits.
// Mirrors orchestration.CostControlSettings.
export interface CostControlSettings {
  max_tokens_per_job: number;
  max_tokens_per_agent: number;
  budget_action: string;
}

// RecoverySettings controls crash recovery behavior.
// Mirrors orchestration.RecoverySettings.
export interface RecoverySettings {
  auto_restart: boolean;
  max_restarts: number;
  restart_backoff: string;
}

// ReviewCriterion defines a single validation check applied during review.
// Mirrors orchestration.ReviewCriterion.
export interface ReviewCriterion {
  name: string;
  validator: string;
  config: string;
  required: boolean;
}

// WorkflowSettings contains operational parameters for workflow execution.
// Mirrors orchestration.WorkflowSettings.
export interface WorkflowSettings {
  max_iterations: number;
  max_parallelism: number;
  poll_interval: string;
  timeout_per_item: string;
  total_timeout: string;
  review_criteria: ReviewCriterion[];
  topology: CommunicationTopology;
  isolation: WorkspaceIsolation;
  peer_messaging: PeerMessagingSettings;
  file_locking: FileLockSettings;
  vector_db: VectorDBSettings;
  cost_controls: CostControlSettings;
  recovery: RecoverySettings;
  verbosity: string;
  num_ctx: number;
}

// WorkflowDefinition describes a complete orchestration workflow.
// Mirrors orchestration.WorkflowDefinition.
export interface WorkflowDefinition {
  name: string;
  description: string;
  mode: WorkflowMode;
  agents: AgentRoleSpec[];
  settings: WorkflowSettings;
}

// =============================================================================
// Validation Types
// =============================================================================

// ValidationResult represents the outcome of a single validation check.
export interface ValidationResult {
  name: string;
  passed: boolean;
  message: string;
  details: string;
}

// =============================================================================
// Session Event Types (Phase 4)
// Mirrors sdk/go/client/orchestrator/client.go
// =============================================================================

// SessionEventEnvelope wraps one event delivered over a session event stream.
// PayloadJSON is the raw orchestration.SessionEvent encoded as JSON — callers
// parse the record themselves or pass it to a downstream dispatcher. Keeping
// the payload opaque allows new event kinds to be added server-side without
// breaking this SDK.
export interface SessionEventEnvelope {
  session_id: string;
  tenant_id?: string;
  sequence: number;
  // payload_json is the decoded JSON payload; the Go side sends []byte but the
  // REST/SSE layer transmits it as a JSON object embedded in the frame data.
  payload_json: Record<string, unknown>;
  // timestamp is an ISO-8601 string as serialised by time.Time in Go's
  // encoding/json package (RFC 3339 with nanosecond precision).
  timestamp: string;
  from_replay?: boolean;
}

// GetSessionHistoryRequest pages through historical session events.
// Mirrors orchestrator.GetSessionHistoryRequest.
export interface GetSessionHistoryRequest {
  tenant_id?: string;
  session_id: string;
  // from_sequence is the inclusive lower-bound sequence number; 0 = from start.
  from_sequence?: number;
  // limit controls the maximum number of events returned; server enforces its
  // own ceiling when this is 0 or omitted.
  limit?: number;
}

// GetSessionHistoryResponse carries one page of session events plus the cursor
// for the next page.
// Mirrors orchestrator.GetSessionHistoryResponse.
export interface GetSessionHistoryResponse {
  events: SessionEventEnvelope[];
  // next_seq is 0 when there are no more events (end of history).
  next_seq: number;
}

// SubscribeSessionEventsRequest opens a live stream over a session's event log.
// Mirrors orchestrator.SubscribeSessionEventsRequest.
export interface SubscribeSessionEventsRequest {
  tenant_id?: string;
  session_id: string;
  // from_sequence = 0 means only new events; >= 1 triggers a replay of all
  // stored events from that sequence before transitioning to live tail.
  from_sequence?: number;
  // follow = true continues streaming after the replay is exhausted.
  follow?: boolean;
  // signal allows the caller to abort a streaming subscription.
  signal?: AbortSignal;
}

// =============================================================================
// Session Spawn Types (Phase 1 additions)
// Mirrors sdk/go/client/orchestrator/client.go SpawnSessionRequest
// =============================================================================

// SandboxMode mirrors orchestration.SandboxMode.
// Use SandboxModeDefault to defer to the server's configured policy.
export const SandboxMode = {
  // Default defers to the server's configured sandbox policy.
  Default: 0,
  // Required forces container-sandboxed execution.
  Required: 1,
  // None forces raw process execution, subject to server policy.
  None: 2,
} as const;

// SandboxModeValue is the union of valid SandboxMode values.
export type SandboxModeValue = (typeof SandboxMode)[keyof typeof SandboxMode];

// SpawnSessionRequest creates a new agent session.
// Mirrors orchestrator.SpawnSessionRequest.
export interface SpawnSessionRequest {
  // tenantID threads the orchestrator tenant into the request. Required —
  // the server rejects spawns without a tenant context.
  tenantID: string;
  backend: string;
  project_id?: string;
  model?: string;
  work_dir?: string;
  env?: Record<string, string>;
  // sandbox_mode selects the sandboxing policy for the spawned session.
  sandbox_mode?: SandboxModeValue;
  // sandboxed is deprecated in favour of sandbox_mode; kept for server
  // compatibility with older daemons.
  sandboxed?: boolean;
  // host_affinity pins the session to a specific agent daemon (by Name or Host).
  host_affinity?: string;
  // host_affinity_strict, when true, forces hard host_affinity semantics:
  // the server returns no_matching_agent (HTTP 400) if the named host is
  // offline rather than falling back to any available daemon. Defers to the
  // operator-level orchestration.host_affinity.mode default when false.
  host_affinity_strict?: boolean;
}

// SpawnSessionResponse is the response from creating a new session.
// Mirrors orchestrator.SpawnSessionResponse.
export interface SpawnSessionResponse {
  session_id: string;
  backend: string;
  container_id?: string;
}

// PromptSessionRequest sends a prompt to an existing session.
// Mirrors orchestrator.PromptSessionRequest.
export interface PromptSessionRequest {
  session_id: string;
  prompt: string;
}

// PromptSessionResponse is the response from prompting a session.
// Mirrors orchestrator.PromptSessionResponse.
export interface PromptSessionResponse {
  session_id: string;
  response: string;
  done: boolean;
}

// CloseSessionRequest terminates a session.
// Mirrors orchestrator.CloseSessionRequest.
export interface CloseSessionRequest {
  session_id: string;
}

// CloseSessionResponse is the response from closing a session.
// Mirrors orchestrator.CloseSessionResponse.
export interface CloseSessionResponse {
  session_id: string;
  closed: boolean;
}

// ListSessionsRequest filters the session list.
// Mirrors orchestrator.ListSessionsRequest.
export interface ListSessionsRequest {
  tenantID: string;
  backend?: string;
  status?: string;
}

// SessionStatusResponse carries the status of a single session.
// Mirrors orchestrator.SessionStatusResponse.
export interface SessionStatusResponse {
  session_id: string;
  backend: string;
  status: string;
  container_id?: string;
  created_at: number;
  last_active: number;
}

// ListSessionsResponse is the response from listing sessions.
// Mirrors orchestrator.ListSessionsResponse.
export interface ListSessionsResponse {
  sessions: SessionStatusResponse[];
  count: number;
}

// GetAgentRequest retrieves a single agent by ID.
// Mirrors orchestrator.GetAgentRequest.
export interface GetAgentRequest {
  tenant_id: string;
  agent_id: string;
}

// ListAgentsRequest filters the agent list.
// Mirrors orchestrator.ListAgentsRequest.
export interface ListAgentsRequest {
  tenant_id: string;
  host?: string;
  status?: string;
  agent_type?: string;
  backend?: string;
  model?: string;
}

// ListAgentsResponse is the response from listing agents.
// Mirrors orchestrator.ListAgentsResponse.
export interface ListAgentsResponse {
  agents: AgentRegistration[];
  count: number;
}

// =============================================================================
// Workflow Management Types
// Mirrors pkg/orchestration/workflow_types.go REST layer
// =============================================================================

// CreateWorkflowRequest creates a named workflow from a YAML or JSON definition.
export interface CreateWorkflowRequest {
  tenant_id: string;
  name: string;
  definition: WorkflowDefinition;
}

// CreateWorkflowResponse contains the server-assigned workflow name on success.
export interface CreateWorkflowResponse {
  name: string;
  created_at: number;
}

// ListWorkflowsResponse contains the set of registered workflow definitions.
export interface ListWorkflowsResponse {
  workflows: WorkflowDefinition[];
  count: number;
}

// GetWorkflowResponse wraps a single workflow definition.
export interface GetWorkflowResponse {
  workflow: WorkflowDefinition;
}

// =============================================================================
// Task Mutation Types
// =============================================================================

// CreateTaskRequest creates a new orchestration task.
// Mirrors orchestrator.CreateTaskRequest.
export interface CreateTaskRequest {
  tenant_id: string;
  subject: string;
  team_name: string;
  description?: string;
  metadata?: Record<string, string>;

  // host_affinity pins the task to a specific agent name or hostname.
  // Empty means "any available agent".
  host_affinity?: string;

  // host_affinity_strict, when true, forces hard host_affinity semantics:
  // the server returns no_matching_agent (HTTP 400) if the named host is
  // offline rather than falling back to any available agent.
  host_affinity_strict?: boolean;

  // gpu_required indicates the task must execute on an agent that advertises
  // "gpu" in its capabilities.
  gpu_required?: boolean;

  // auto_spawn, when true, dispatches a one-shot job on the best-matching
  // agent immediately. If no agent matches the routing constraints the server
  // returns an error and the task is persisted with empty spawned_job_id.
  auto_spawn?: boolean;

  // backend restricts agent selection during auto-spawn to agents whose
  // backend field matches this value. Ignored when auto_spawn is false.
  backend?: string;
}

// ListBackendModelsResponse is the response from listing models for a backend.
export interface ListBackendModelsResponse {
  backend: string;
  models: string[];
  count: number;
}

// CreateTaskResponse contains the newly created task.
export interface CreateTaskResponse {
  task: Task;
}

// DecomposeTaskRequest asks the orchestrator to break a task into sub-tasks.
// Mirrors orchestrator.DecomposeTaskRequest.
export interface DecomposeTaskRequest {
  tenant_id: string;
  task_id: string;
}

// DecomposeTaskResponse contains sub-tasks generated by decomposition.
export interface DecomposeTaskResponse {
  tasks: Task[];
}

// ClaimTaskRequest claims a pending task for execution by an agent.
export interface ClaimTaskRequest {
  tenant_id: string;
  task_id: string;
  agent_id: string;
}

// CompleteTaskRequest marks a task as completed with an optional result.
export interface CompleteTaskRequest {
  tenant_id: string;
  task_id: string;
  result?: {
    output?: string;
    metadata?: Record<string, string>;
  };
}

// =============================================================================
// Agent Registration Mutation Types
// =============================================================================

// RegisterAgentRequest registers a new agent daemon with the fleet.
// Mirrors orchestrator.RegisterAgentRequest.
export interface RegisterAgentRequest {
  tenant_id: string;
  name: string;
  host: string;
  backend: string;
  agent_type: string;
  model?: string;
  metadata?: Record<string, string>;
}

// RegisterAgentResponse contains the registered agent.
export interface RegisterAgentResponse {
  agent: AgentRegistration;
}

// RemoveAgentRequest removes a registered agent by ID.
// Mirrors orchestrator.RemoveAgentRequest.
export interface RemoveAgentRequest {
  tenant_id: string;
  agent_id: string;
}

// =============================================================================
// Client Configuration
// =============================================================================

// Transport selects which wire protocol the client uses to reach the server.
export type Transport = 'wails' | 'rest' | 'grpc-web' | 'unix' | 'mcp' | 'quic';

// ClientConfig holds configuration for the SDK client factory.
export interface ClientConfig {
  // baseURL is used by the rest and grpc-web transports.
  baseURL?: string;
  /** @deprecated use baseURL */
  baseUrl?: string;
  // transport selects the wire protocol explicitly; omit to auto-detect.
  transport?: Transport;
  // token is a bearer token string or a factory that resolves one asynchronously.
  token?: string | (() => Promise<string>);
  // tenantID is forwarded as a default tenant for operations that require one.
  tenantID?: string;
  // grpcWeb contains options for the grpc-web transport.
  grpcWeb?: { format: 'binary' | 'text' };
  // unix contains options for the unix-socket transport.
  unix?: { socketPath: string };
  // mcp contains options for the MCP transport.
  mcp?: { endpoint: string };
  // quic contains options for the QUIC transport.
  quic?: { endpoint: string };
}


// =============================================================================
// Agent Deploy Types
// Mirrors pkg/agent/deploy/spec.go — fields are snake_case to match the
// JSON tags the Go-side primitives use.
// =============================================================================

// =============================================================================
// SSH Types
// Mirrors pkg/api/rest/handlers_ssh.go and go-xkms/sdk/go/client/ssh/types.go.
// The new scoped model (scope/owner/name) is the preferred identity triple.
// Legacy callers that only use name continue to work via server-side defaults.
// =============================================================================

// SSHScope enumerates the three key visibility scopes supported by xkms.
export type SSHScope = 'tenant' | 'user' | 'service';

// SSHKey represents a stored SSH key entry returned by the keys service.
// The server serialises xkmsssh.Key directly; the public_key field uses the
// JSON tag "public_key" (not "public_key_openssh").
export interface SSHKey {
  /** Scope of this key. Populated by the scoped REST surface. */
  scope?: SSHScope;
  /** Owner identifier within the scope. */
  owner?: string;
  /** Human-visible key name, unique within (scope, owner). */
  name: string;
  /** Canonical algorithm identifier: 'ed25519', 'rsa', 'ecdsa-p256', etc. */
  algorithm: string;
  /** OpenSSH SHA256 fingerprint ('SHA256:<base64>'). */
  fingerprint: string;
  /**
   * OpenSSH authorized_keys-format public key line.
   * The server JSON tag is "public_key"; the legacy field name
   * "public_key_openssh" is kept as an optional alias for backward compat
   * with Wails callers that read SSHKeyDTO.
   */
  public_key?: string;
  /** @deprecated Use public_key. Wails SSHKeyDTO still populates this field. */
  public_key_openssh?: string;
  /** ISO8601 creation timestamp. */
  created_at: string;
  /** Optional free-form comment. */
  comment?: string;
}

// CreateSSHKeyRequest is the payload for POST /api/v1/ssh/keys.
// scope and owner are optional; the server defaults to user/$USER when absent.
export interface CreateSSHKeyRequest {
  /** Human-visible key name, unique within (scope, owner). */
  name: string;
  /** Canonical algorithm: 'ed25519', 'rsa', 'ecdsa-p256', etc. */
  algorithm: string;
  /** Key scope. Defaults to 'user' server-side when omitted. */
  scope?: SSHScope;
  /** Owner identifier. Defaults to the authenticated OS user when omitted. */
  owner?: string;
  /** Optional RSA key size in bits. Only consulted for 'rsa' algorithm. */
  rsa_bits?: number;
  /** Optional free-form comment. */
  comment?: string;
}

// ImportSSHKeyRequest is the payload for POST /api/v1/ssh/keys/import.
export interface ImportSSHKeyRequest {
  /** Human-visible key name. */
  name: string;
  /** PEM-encoded private key bytes. The server base64-decodes this field. */
  pem: string;
  /**
   * Base64-encoded passphrase bytes. The server decodes the base64 directly
   * into a mlocked buffer — the plaintext never materialises as a Go string.
   */
  passphrase_b64?: string;
  /** Key scope. Defaults to 'user' server-side when omitted. */
  scope?: SSHScope;
  /** Owner identifier. Defaults to the authenticated OS user when omitted. */
  owner?: string;
  /** Optional free-form comment. */
  comment?: string;
}

// ListSSHKeysQuery carries optional filters for GET /api/v1/ssh/keys.
export interface ListSSHKeysQuery {
  /** Filter by scope. */
  scope?: SSHScope;
  /** Filter by owner. */
  owner?: string;
}

// ListSSHKeysResponse is the response from GET /api/v1/ssh/keys.
// The server wraps the array in {"items": [...]} to match the xkms REST envelope.
// The legacy "keys" field is kept for backward compatibility with Wails callers.
export interface ListSSHKeysResponse {
  /** Scoped REST surface wraps keys in "items". */
  items?: SSHKey[];
  /** Legacy Wails binding wraps keys in "keys". */
  keys?: SSHKey[];
}

// SSHPublicKey is the response from GET /api/v1/ssh/keys/{scope}/{owner}/{name}/public.
// Mirrors xkmsssh.PublicKey.
export interface SSHPublicKey {
  /** Canonical algorithm identifier. */
  algorithm: string;
  /** OpenSSH authorized_keys-format line. */
  line: string;
  /** OpenSSH SHA256 fingerprint. */
  fingerprint: string;
}

// SignRequest is the body for POST /api/v1/ssh/keys/{scope}/{owner}/{name}/sign.
// The digest field is raw bytes; JSON serialisation base64-encodes it automatically.
export interface SignRequest {
  /** Raw digest bytes to sign. JSON base64-encoded automatically. */
  digest: Uint8Array | number[];
}

// SignResponse mirrors sshSignResponse from handlers_ssh.go.
// The server returns the golang.org/x/crypto/ssh.Signature fields.
export interface SignResponse {
  /** Signature format string (e.g. 'ssh-ed25519'). */
  format: string;
  /** Raw signature blob. JSON base64-decoded by the receiver. */
  blob: number[];
  /** Trailing bytes, if any. */
  rest?: number[];
}

// =============================================================================
// SSH Known Hosts types
// =============================================================================

// SSHPin is the transport-safe representation of a host-key pin.
// Mirrors xkmsssh.Pin.
export interface SSHPin {
  /** Scope that owns this pin. */
  scope: SSHScope;
  /** Owner within the scope. */
  owner: string;
  /** Hostname or IP address that was pinned. */
  host: string;
  /** OpenSSH SHA256 fingerprint of the pinned host key. */
  fingerprint: string;
  /** Algorithm of the pinned host key. */
  algorithm?: string;
  /** ISO8601 timestamp when the pin was added. */
  added_at: string;
  /** Provenance tag: 'manual', 'imported:<path>', 'first-contact', etc. */
  source?: string;
}

// AddSSHPinRequest is the body for POST /api/v1/ssh/known-hosts.
// Mirrors xkmsssh.AddKnownHostRequest.
export interface AddSSHPinRequest {
  /** Scope that owns this pin. */
  scope?: SSHScope;
  /** Owner within the scope. */
  owner?: string;
  /** Hostname or IP address to pin. */
  host: string;
  /** OpenSSH SHA256 fingerprint. */
  fingerprint: string;
  /** Algorithm of the host key. */
  algorithm?: string;
  /** Provenance tag. */
  source?: string;
}

// ListSSHPinsResponse is the response from GET /api/v1/ssh/known-hosts.
// The server wraps the array in {"items": [...]}.
export interface ListSSHPinsResponse {
  /** Pinned host keys. */
  items: SSHPin[];
}

// VerifySSHPinRequest is the body for POST /api/v1/ssh/known-hosts/{host}/verify.
export interface VerifySSHPinRequest {
  /** Scope whose pin store to query. */
  scope?: SSHScope;
  /** Owner whose pin store to query. */
  owner?: string;
  /** Fingerprint presented by the connecting host. */
  fingerprint: string;
  /** Algorithm of the presented key (optional hint). */
  algorithm?: string;
}

// =============================================================================
// SSH Authorized Keys types
// =============================================================================

// SSHAuthorizedEntry is the transport-safe representation of an authorized_keys entry.
// Mirrors xkmsssh.AuthorizedEntry.
export interface SSHAuthorizedEntry {
  /** Scope that owns this entry. */
  scope: SSHScope;
  /** Owner within the scope. */
  owner: string;
  /** Key name. */
  name: string;
  /** Full authorized_keys line (algorithm + base64 + comment). */
  line: string;
  /** OpenSSH restrict/permit directives. */
  options?: string[];
  /** Free-form comment. */
  comment?: string;
}

// EmitAuthorizedRequest is the body for POST /api/v1/ssh/authorized.
// Mirrors xkmsssh.EmitRequest.
export interface EmitAuthorizedRequest {
  /** OpenSSH authorized_keys-format public key. */
  public_key: string;
  /** Optional OpenSSH directives. */
  options?: string[];
  /** Optional comment. */
  comment?: string;
}

// EmitAuthorizedResponse is the response from POST /api/v1/ssh/authorized.
// The server returns sshEmitResponse{Line: line}.
export interface EmitAuthorizedResponse {
  /** The formatted authorized_keys line. */
  line: string;
}

// VerifyAuthorizedRequest is the body for POST /api/v1/ssh/authorized/verify.
export interface VerifyAuthorizedRequest {
  /** Scope to search for a matching authorized entry. */
  scope?: SSHScope;
  /** Owner to search for a matching authorized entry. */
  owner?: string;
  /** SHA256 fingerprint of the key to verify. */
  fingerprint: string;
  /** Algorithm hint (optional). */
  algorithm?: string;
}

// VerifyAuthorizedResponse is the response from POST /api/v1/ssh/authorized/verify.
// The server returns the matching AuthorizedEntry directly.
export interface VerifyAuthorizedResponse {
  /** The matching authorized entry. */
  entry: SSHAuthorizedEntry;
}

// DeployAgentSpec describes how to deploy a QuicBaaS agent to a remote host.
// Required fields: host, ssh_user, server, tenant, agent_bind, backends,
// binary. The mTLS identity for the daemon is minted by the server-side
// deploy flow — no bootstrap token is required. Optional fields fall back
// to server-side defaults.
export interface DeployAgentSpec {
  host: string;
  ssh_user: string;
  ssh_key_name?: string;
  ssh_key_path?: string;
  ssh_key_passphrase?: string;
  ssh_password?: string;
  ssh_host_key?: string;
  insecure_host_key?: boolean;

  server: string;
  tenant: string;
  agent_bind: string;
  backends: string[];

  binary: string;
  install_dir?: string;
  data_dir?: string;

  /** Tri-state: undefined=auto-detect, true=systemd, false=nohup. */
  systemd?: boolean;

  /** Nanoseconds (Go time.Duration). Zero disables the online wait. */
  wait_online?: number;
}

// DeployAgentResult is returned by the deploy workflow on success.
export interface DeployAgentResult {
  host: string;
  installed_path: string;
  service_status: string;
  agent_id?: string;
  online_at?: string;
  /** Path to the mTLS client certificate installed on the remote host. */
  agent_cert_path?: string;
  /** Hex-encoded serial number of the issued agent certificate. */
  agent_cert_serial?: string;
}

// =============================================================================
// IAM Types
// Mirrors /api/v1/tenants and /api/v1/iam REST routes.
// =============================================================================

// IAMUser represents a user identity managed by the IAM service.
export interface IAMUser {
  id: string;
  tenant_id: string;
  username: string;
  email: string;
  enabled?: boolean;
  roles: string[];
  groups: string[];
  created_at: string;
  updated_at: string;
}

// ListUsersResponse is the response from listing tenant users.
export interface ListUsersResponse {
  users: IAMUser[];
  count: number;
}

// CreateUserRequest creates a new user in a tenant via the passwordless invite flow.
export interface CreateUserRequest {
  tenant_id: string;
  username: string;
  email: string;
  display_name?: string;
  auth_methods?: string[];
  roles?: string[];
  invite_expiry_hours?: number;
}

// CreateUserResponse contains the created user and the one-time invite details.
export interface CreateUserResponse {
  user_id: string;
  email: string;
  invite_token: string;
  invite_url: string;
  expires_at: string;
}

// InviteMetadata contains publicly visible info about a pending invite.
export interface InviteMetadata {
  user_id: string;
  email?: string;
  name?: string;
  auth_methods: string[];
  expires_at: string;
}

// ActivationResponse is returned on successful FIDO2 or PIV activation.
export interface ActivationResponse {
  user_id: string;
  message: string;
}

// IAMTenant represents a tenant in the IAM service.
export interface IAMTenant {
  id: string;
  name: string;
  description: string;
  created_at: string;
  updated_at: string;
}

// ListTenantsResponse is the response from listing tenants.
export interface ListTenantsResponse {
  tenants: IAMTenant[];
  count: number;
}

// CreateTenantRequest creates a new tenant.
export interface CreateTenantRequest {
  name: string;
  description?: string;
}

// CreateTenantResponse contains the created tenant.
export interface CreateTenantResponse {
  tenant: IAMTenant;
}

// IAMRole represents a role definition within a tenant.
export interface IAMRole {
  id: string;
  tenant_id: string;
  name: string;
  description: string;
  permissions: string[];
  created_at: string;
  updated_at: string;
}

// ListRolesResponse is the response from listing roles.
export interface ListRolesResponse {
  roles: IAMRole[];
  count: number;
}

// IAMPolicy represents an ABAC policy definition.
export interface IAMPolicy {
  name: string;
  tenant_id: string;
  description: string;
  rules: unknown[];
  spec_yaml?: string;
  created_at: string;
  updated_at: string;
}

// ListPoliciesResponse is the response from listing policies.
export interface ListPoliciesResponse {
  policies: IAMPolicy[];
  count: number;
}

// =============================================================================
// KMS Types
// Mirrors /api/v1/kms REST routes.
// =============================================================================

// KMSKeyStatus represents the operational status of a cryptographic key.
export type KMSKeyStatus = 'active' | 'inactive' | 'pending_deletion' | 'destroyed';

// KMSKey represents a cryptographic key managed by the KMS.
export interface KMSKey {
  id: string;
  name: string;
  description: string;
  algorithm: string;
  key_size: number;
  status: KMSKeyStatus;
  created_at: string;
  updated_at: string;
  exportable: boolean;
  tags: Record<string, string>;
}

// ListKeysResponse is the response from listing KMS keys.
export interface ListKeysResponse {
  keys: KMSKey[];
  count: number;
}

// CreateKeyRequest creates a new cryptographic key.
export interface CreateKeyRequest {
  name?: string;
  description?: string;
  key_attributes: {
    algorithm: string;
    key_usage: string[];
    exportable?: boolean;
  };
  tags?: Record<string, string>;
}

// CreateKeyResponse contains the created key handle.
export interface CreateKeyResponse {
  key: KMSKey;
  timestamp: number;
}

// RotateKeyResponse is returned when a key is rotated.
export interface RotateKeyResponse {
  key: KMSKey;
  old_key_id: string;
  timestamp: number;
}

// KMSEncryptRequest encrypts plaintext using a named key.
export interface KMSEncryptRequest {
  key_id: string;
  plaintext: string;
}

// KMSEncryptResponse contains the encrypted ciphertext.
export interface KMSEncryptResponse {
  ciphertext: string;
  key_id: string;
  algorithm: string;
}

// KMSDecryptRequest decrypts ciphertext using a named key.
export interface KMSDecryptRequest {
  key_id: string;
  ciphertext: string;
}

// KMSDecryptResponse contains the decrypted plaintext.
export interface KMSDecryptResponse {
  plaintext: string;
  key_id: string;
}

// =============================================================================
// Secrets Types
// Mirrors /api/v1/secrets REST routes.
// =============================================================================

// SecretEntry represents a secret stored in the secrets engine.
export interface SecretEntry {
  path: string;
  version: number;
  created_at: string;
  updated_at: string;
}

// ListSecretsResponse is the response from listing secrets.
export interface ListSecretsResponse {
  secrets: SecretEntry[];
  count: number;
}

// StoreSecretRequest stores a secret at the given path.
export interface StoreSecretRequest {
  path: string;
  data: Record<string, unknown>;
}

// GetSecretResponse contains the retrieved secret value.
export interface GetSecretResponse {
  path: string;
  data: Record<string, unknown>;
  version: number;
  created_at: string;
  updated_at: string;
}

// RotateSecretResponse is returned after a secret is rotated.
export interface RotateSecretResponse {
  path: string;
  version: number;
  rotated_at: string;
}

// SecretVersionEntry represents a single version of a secret.
export interface SecretVersionEntry {
  secret_id: string;
  version: number;
  created_at: string;
  created_by?: string;
  deleted_at?: string;
  expires_at?: string;
  metadata?: Record<string, string>;
}

// ListSecretVersionsResponse is returned by the list-versions endpoint.
export interface ListSecretVersionsResponse {
  versions: SecretVersionEntry[];
  count: number;
}

// SetDefaultKeyResponse confirms a key was set as the system default.
export interface SetDefaultKeyResponse {
  name: string;
  is_default: boolean;
  previous_default?: string;
  timestamp: string;
}

// RotateRootKeyResponse confirms root key rotation.
export interface RotateRootKeyResponse {
  success: boolean;
  message: string;
  timestamp: string;
}

// RekeyRequest initiates a Shamir secret sharing rekey operation.
export interface RekeyRequest {
  new_threshold: number;
  new_total_shares: number;
}

// RekeyResponse contains the new Shamir shares after a rekey.
export interface RekeyResponse {
  // shares are base64-encoded Shamir share bytes.
  shares: string[];
  version: number;
  timestamp: string;
}

// =============================================================================
// Seal Types
// Mirrors /api/v1/seal REST routes.
// =============================================================================

// SealStatus represents the current sealed/unsealed state of the database.
// Field names match the daemon's SealStatusResponse JSON shape.
export interface SealStatus {
  sealed: boolean;
  initialized: boolean;
  mode?: string;
  threshold: number;
  total_shares: number;
  shares_provided: number;
  shares_received?: number;
  tpm_available?: boolean;
  cluster_nodes?: number;
  version: number;
  timestamp?: string;
  cluster_name?: string;
}

// SealInitRequest initialises the seal with Shamir secret sharing parameters.
export interface SealInitRequest {
  secret_shares: number;
  secret_threshold: number;
}

// SealInitResponse contains the generated key shares and root token.
export interface SealInitResponse {
  keys: string[];
  root_token: string;
}

// UnsealRequest submits one key share toward unsealing.
export interface UnsealRequest {
  key: string;
}

// UnsealResponse reports the updated seal state after submitting a share.
export interface UnsealResponse {
  sealed: boolean;
  progress: number;
  threshold: number;
}

// =============================================================================
// Barrier Types
// Mirrors /api/v1/barrier REST routes and go-qrdb transport/barrier_types.go.
// Replaces the legacy Seal* surface; Seal* aliases below are kept for one
// release as deprecated re-exports.
// =============================================================================

// BarrierStatusResponse describes the current state of the cryptographic
// barrier. Mirrors transport.BarrierStatusResponse.
export interface BarrierStatusResponse {
  /** True when the barrier is in sealed state. */
  sealed: boolean;
  /** Active sealing strategy identifier (e.g. "passphrase", "shamir"). */
  strategy: string;
  /** True when the active strategy uses dedicated cryptographic hardware. */
  hardware_backed: boolean;
  /** RFC3339 timestamp recorded when the barrier was first initialized. */
  initialized_at?: string;
}

// BarrierInitRequest holds the secret used to initialize the barrier with
// a single master key. Mirrors transport.BarrierInitializeRequest.
export interface BarrierInitRequest {
  /** Passphrase used to derive the master key. */
  secret: string;
}

// BarrierUnsealRequest holds the secret used to unseal the barrier.
// Mirrors transport.BarrierUnsealRequest.
export interface BarrierUnsealRequest {
  /** Passphrase used to unseal the barrier. */
  secret: string;
}

// BarrierShamirInitRequest initialises the barrier with Shamir secret
// sharing. Mirrors transport.BarrierInitializeShamirRequest.
export interface BarrierShamirInitRequest {
  /** Optional passphrase used to derive the master key. */
  secret: string;
  /** Minimum shares required to reconstruct the master key. */
  threshold: number;
  /** Total shares to generate. */
  total_shares: number;
}

// BarrierShamirInitResponse contains the generated Shamir shares.
// Mirrors transport.BarrierInitializeShamirResponse.
export interface BarrierShamirInitResponse {
  /** Shamir shares for distribution. */
  shares: string[];
  /** Threshold required for reconstruction. */
  threshold: number;
  /** Total shares generated. */
  total_shares: number;
}

// BarrierUnsealShareRequest submits a single Shamir share toward the unseal
// quorum. Mirrors transport.BarrierUnsealShareRequest.
export interface BarrierUnsealShareRequest {
  /** Single Shamir share. */
  share: string;
}

// BarrierUnsealShareResponse reports progress toward the unseal threshold.
// Mirrors transport.BarrierUnsealShareResponse.
export interface BarrierUnsealShareResponse {
  /** Number of shares required to unseal. */
  required: number;
  /** Number of shares submitted so far. */
  submitted: number;
  /** True when the quorum threshold has been met. */
  complete: boolean;
}

// BarrierUnsealSharesRequest submits multiple shares in a single call.
// Mirrors transport.BarrierUnsealSharesRequest.
export interface BarrierUnsealSharesRequest {
  /** Complete set of Shamir shares. */
  shares: string[];
}

// BarrierShamirSharesResponse describes persisted Shamir share metadata.
// Mirrors transport.BarrierShamirSharesResponse.
export interface BarrierShamirSharesResponse {
  /** Number of shares currently in storage. */
  count: number;
  /** Minimum shares required for reconstruction. */
  threshold: number;
  /** Total shares generated. */
  total: number;
}

// BarrierRekeyRequest holds parameters for rekeying the Shamir shares.
// Mirrors transport.BarrierRekeyRequest.
export interface BarrierRekeyRequest {
  /** New threshold value. */
  threshold: number;
  /** New total share count. */
  total: number;
}

// BarrierRekeyResponse returns the new key shares after a rekey operation.
// Mirrors transport.BarrierRekeyResponse.
export interface BarrierRekeyResponse {
  /** New Shamir shares. */
  shares: string[];
  /** New threshold value. */
  threshold: number;
  /** New total share count. */
  total_shares: number;
}

// ---------------------------------------------------------------------------
// Per-tenant barrier
//
// The broker exposes a dedicated per-tenant barrier under
// /api/v1/tenants/{id}/barrier/*. Unlike the system barrier (a single
// process-wide cryptographic boundary), each tenant that selects the
// "barrier" isolation mode owns an independent barrier whose strategy,
// sealed state, and Shamir quorum are scoped to that tenant. The request and
// response shapes below mirror the broker's REST contract exactly (snake_case
// wire fields) so the SDK round-trips the JSON without transformation.
// ---------------------------------------------------------------------------

/** TenantBarrierStrategy identifies how a tenant's barrier is sealed. */
export type TenantBarrierStrategy = 'none' | 'passphrase' | 'shamir';

// TenantBarrierInitRequest initializes a tenant's barrier with the chosen
// strategy. passphrase is required for the "passphrase" strategy; threshold
// and shares are required for the "shamir" strategy and ignored otherwise.
export interface TenantBarrierInitRequest {
  /** Sealing strategy to provision for the tenant. */
  strategy: TenantBarrierStrategy;
  /** Passphrase used to derive the master key (passphrase strategy). */
  passphrase?: string;
  /** Minimum shares required to reconstruct the master key (shamir strategy). */
  threshold?: number;
  /** Total shares to generate (shamir strategy). */
  shares?: number;
}

// TenantBarrierInitResponse is returned for the shamir strategy and carries
// the freshly minted shares. The "none" and "passphrase" strategies return
// 204 No Content, so callers receive undefined for those.
export interface TenantBarrierInitResponse {
  /** Tenant whose barrier was initialized. */
  tenant_id: string;
  /** Sealing strategy that was provisioned. */
  strategy: TenantBarrierStrategy;
  /** Shamir shares for distribution — returned exactly once. */
  shares: string[];
  /** Threshold required for reconstruction. */
  threshold: number;
  /** Total shares generated. */
  total_shares: number;
}

// TenantBarrierUnsealShareResponse reports progress toward the tenant's unseal
// quorum after submitting a single Shamir share.
export interface TenantBarrierUnsealShareResponse {
  /** Tenant whose barrier received the share. */
  tenant_id: string;
  /** Number of shares required to unseal. */
  required: number;
  /** Number of shares submitted so far. */
  submitted: number;
  /** True when the quorum threshold has been met. */
  complete: boolean;
  /** True while the tenant barrier remains sealed. */
  sealed: boolean;
}

// TenantBarrierStatusResponse describes the current state of a tenant's
// dedicated barrier.
export interface TenantBarrierStatusResponse {
  /** Tenant the status describes. */
  tenant_id: string;
  /** True when the tenant barrier is sealed. */
  sealed: boolean;
  /** True once the tenant barrier has been initialized. */
  initialized: boolean;
  /** Active sealing strategy ("none", "passphrase", "shamir"). */
  strategy: TenantBarrierStrategy;
  /** True when the active strategy uses dedicated cryptographic hardware. */
  hardware_backed: boolean;
  /** Minimum shares required to unseal (shamir strategy). */
  shamir_threshold: number;
  /** Total shares minted by the last init (shamir strategy). */
  shamir_total: number;
}

// BarrierGenerateRecoveryKeysRequest holds parameters for generating
// recovery keys. Mirrors transport.BarrierGenerateRecoveryKeysRequest.
export interface BarrierGenerateRecoveryKeysRequest {
  /** Threshold required for recovery. */
  threshold: number;
  /** Total recovery keys to generate. */
  total: number;
}

// BarrierRecoveryKeysResponse returns generated recovery keys.
// Mirrors transport.BarrierRecoveryKeysResponse.
export interface BarrierRecoveryKeysResponse {
  /** Recovery key values for distribution. */
  keys: string[];
  /** Threshold required for recovery. */
  threshold: number;
  /** Total recovery keys generated. */
  total: number;
}

// BarrierRecoverWithKeysRequest holds recovery keys for disaster recovery.
// Mirrors transport.BarrierRecoverWithKeysRequest.
export interface BarrierRecoverWithKeysRequest {
  /** Recovery keys used to unseal. */
  keys: string[];
}

// BarrierHasRecoveryKeysResponse indicates whether recovery keys exist.
// Mirrors transport.BarrierHasRecoveryKeysResponse.
export interface BarrierHasRecoveryKeysResponse {
  /** True when recovery keys have been generated. */
  has_recovery_keys: boolean;
}

// BarrierGenerateRootTokenRequest holds Shamir shares used to authorise a
// new root token. Mirrors transport.BarrierGenerateRootTokenRequest.
export interface BarrierGenerateRootTokenRequest {
  /** Shamir shares proving knowledge of the master key. */
  shares: string[];
}

// BarrierRootTokenResponse returns a newly generated root token.
// Mirrors transport.BarrierRootTokenResponse.
export interface BarrierRootTokenResponse {
  /** Hex-encoded root token. */
  token: string;
  /** RFC3339 timestamp when the token was generated. */
  created_at: string;
}

// BarrierShamirDeleteShareRequest identifies a share by index.
// Mirrors transport.BarrierShamirDeleteShareRequest.
export interface BarrierShamirDeleteShareRequest {
  /** Zero-based share index. */
  index: number;
}

// =============================================================================
// Ceremony Types
// Mirrors /api/v1/init REST routes and go-qrdb transport/ceremony_types.go.
// =============================================================================

// CeremonyOfficer describes a single officer participating in the M-of-N
// initialization ceremony. Mirrors transport.CeremonyOfficerRequest.
export interface CeremonyOfficer {
  /** Unique username for the officer. */
  username: string;
  /** PEM-encoded CSR submitted by the officer. */
  csr_pem: string;
}

// CeremonyInitializeRequest holds the parameters for initializing the
// security ceremony. Mirrors transport.CeremonyInitializeRequest.
export interface CeremonyInitializeRequest {
  /** Security Officer PIN. */
  so_pin: string;
  /** User PIN. */
  user_pin: string;
  /** Optional credential seal strategy override. */
  credential_seal_strategy?: string;
  /** Threshold for M-of-N enrollment. Zero means single-admin mode. */
  threshold: number;
  /** Officers participating in the ceremony. */
  officers?: CeremonyOfficer[];
  /** Optional hostname for the issued TLS certificate. */
  hostname?: string;
  /** Optional Subject Alternative Names for the issued TLS certificate. */
  sans?: string[];
}

// CeremonyInitializeResponse returns the result of a successful ceremony
// initialization. Mirrors transport.CeremonyInitializeResponse.
export interface CeremonyInitializeResponse {
  /** Base64-encoded SHA-256 hash of the CA certificate's SPKI. */
  spki_pin: string;
  /** PEM-encoded CA certificate. */
  ca_cert_pem: string;
  /** PEM-encoded TLS certificate issued for the server. */
  tls_cert_pem: string;
  /** PEM-encoded Security Officer certificate. */
  so_cert_pem?: string;
  /** PEM-encoded Security Officer private key. */
  so_key_pem?: string;
  /** Ceremony state after initialization. */
  state: string;
  /** Number of Shamir shares generated. */
  share_count: number;
}

// CeremonyStatusResponse describes the current ceremony state.
// Mirrors transport.CeremonyStatusResponse.
export interface CeremonyStatusResponse {
  /** Current ceremony state. */
  state: string;
  /** Optional base64-encoded SPKI pin once the CA is initialized. */
  spki_pin?: string;
}

// CeremonyClaimCertBeginRequest initiates a certificate claim challenge.
// Mirrors transport.ClaimCertBeginRequest.
export interface CeremonyClaimCertBeginRequest {
  /** Officer username. */
  username: string;
}

// CeremonyClaimCertBeginResponse returns the challenge nonce.
// Mirrors transport.ClaimCertBeginResponse.
export interface CeremonyClaimCertBeginResponse {
  /** Hex-encoded random nonce the officer must sign. */
  nonce_hex: string;
  /** RFC3339 expiration timestamp for the nonce. */
  expires_at: string;
}

// CeremonyClaimCertCompleteRequest completes the certificate claim.
// Mirrors transport.ClaimCertCompleteRequest.
export interface CeremonyClaimCertCompleteRequest {
  /** Officer username. */
  username: string;
  /** Hex-encoded nonce from CeremonyClaimCertBegin. */
  nonce_hex: string;
  /** Base64-encoded signature over the nonce. */
  signature: string;
}

// CeremonyClaimCertCompleteResponse returns the issued certificate.
// Mirrors transport.ClaimCertCompleteResponse.
export interface CeremonyClaimCertCompleteResponse {
  /** PEM-encoded officer certificate. */
  cert_pem: string;
  /** PEM-encoded CA certificate for trust chain verification. */
  ca_cert_pem: string;
}

// CeremonyClaimShareRequest requests the Shamir share for an officer.
// Mirrors transport.ClaimShareRequest.
export interface CeremonyClaimShareRequest {
  /** Officer username. */
  username: string;
}

// CeremonyClaimShareResponse returns the officer's Shamir share.
// Mirrors transport.ClaimShareResponse.
export interface CeremonyClaimShareResponse {
  /** Base64-encoded Shamir share material. */
  share: string;
}

// CeremonySignCSRRequest submits a CSR for signing during the ceremony.
// Mirrors transport.SignCSRInitRequest.
export interface CeremonySignCSRRequest {
  /** Subject username for the issued certificate. */
  username: string;
  /** Security Officer PIN. */
  so_pin: string;
  /** PEM-encoded CSR. */
  csr_pem: string;
  /** Role identifier for the issued certificate. */
  role: string;
}

// CeremonySignCSRResponse returns the signed certificate.
// Mirrors transport.SignCSRInitResponse.
export interface CeremonySignCSRResponse {
  /** PEM-encoded certificate signed from the CSR. */
  cert_pem: string;
}

// CeremonyEnrollExternalOfficerRequest enrols an officer whose key material
// lives outside the daemon. Exactly one of cert_pem and pubkey_pem must be
// supplied. Mirrors transport.EnrollExternalOfficerRequest.
export interface CeremonyEnrollExternalOfficerRequest {
  /** Officer username. */
  username: string;
  /** PEM-encoded X.509 certificate. Mutually exclusive with pubkey_pem. */
  cert_pem?: string;
  /** PEM-encoded SPKI public key. Mutually exclusive with cert_pem. */
  pubkey_pem?: string;
  /** One-based Shamir share index assigned to this officer. */
  share_index: number;
  /** Auth factor identifier: "mtls", "webauthn", or "piv". */
  auth_factor: string;
  /** Optional plaintext share to seal to the officer's public key. */
  share_bytes?: string;
}

// CeremonyEnrollExternalOfficerResponse returns the sealed share envelope.
// Mirrors transport.EnrollExternalOfficerResponse.
export interface CeremonyEnrollExternalOfficerResponse {
  /** Base64-encoded sealed share envelope. Empty when no share was sealed. */
  sealed_share?: string;
}

// =============================================================================
// Bootstrap Types
// Mirrors /api/v1/bootstrap REST routes for trust bootstrap (CA bundle
// retrieval before TLS is available). Each method advertises a different
// trust verification mechanism (auto, dane, noise, spki).
// =============================================================================

// BootstrapMethod enumerates the supported trust bootstrap mechanisms.
export type BootstrapMethod = 'auto' | 'dane' | 'noise' | 'spki';

// BootstrapBundleRequest filters the CA bundle returned by a bootstrap
// endpoint. A request with all fields empty fetches every certificate.
export interface BootstrapBundleRequest {
  /** Filter by store type: "root", "intermediate", "leaf", "end-entity", or empty. */
  store_type?: string;
  /** Filter by key algorithm: "RSA", "ECDSA", "Ed25519", or empty. */
  algorithm?: string;
}

// BootstrapBundleResponse contains the retrieved CA certificate bundle.
// Mirrors truststrap.CABundleResponse.
export interface BootstrapBundleResponse {
  /** Trust bootstrap method used to retrieve this bundle. */
  method: BootstrapMethod;
  /** PEM-encoded certificate chain. */
  bundle_pem: string;
  /** Optional base64-encoded individual DER-encoded certificates. */
  certificates?: string[];
  /** MIME type of the bundle ("application/pem-certificate-chain"). */
  content_type: string;
  /** Optional metadata describing how the bundle was verified. */
  metadata?: Record<string, string>;
}

// =============================================================================
// Seal* Deprecation Aliases
// Re-export Seal* names that point at the new Barrier* shapes so existing
// callers continue to compile. New code should use Barrier* directly. These
// aliases will be removed one release after the unification ships.
// =============================================================================

/** @deprecated Use BarrierStatusResponse. */
export type SealStatusResponse = BarrierStatusResponse;
/** @deprecated Use BarrierInitRequest. */
export type SealBarrierInitRequest = BarrierInitRequest;
/** @deprecated Use BarrierUnsealRequest. */
export type SealBarrierUnsealRequest = BarrierUnsealRequest;
/** @deprecated Use BarrierShamirInitRequest. */
export type SealShamirInitRequest = BarrierShamirInitRequest;
/** @deprecated Use BarrierShamirInitResponse. */
export type SealShamirInitResponse = BarrierShamirInitResponse;
/** @deprecated Use BarrierUnsealShareRequest. */
export type SealUnsealShareRequest = BarrierUnsealShareRequest;
/** @deprecated Use BarrierUnsealShareResponse. */
export type SealUnsealShareResponse = BarrierUnsealShareResponse;
/** @deprecated Use BarrierRekeyRequest. */
export type SealRekeyRequest = BarrierRekeyRequest;
/** @deprecated Use BarrierRekeyResponse. */
export type SealRekeyResponse = BarrierRekeyResponse;
/** @deprecated Use BarrierGenerateRecoveryKeysRequest. */
export type SealGenerateRecoveryKeysRequest = BarrierGenerateRecoveryKeysRequest;
/** @deprecated Use BarrierRecoveryKeysResponse. */
export type SealRecoveryKeysResponse = BarrierRecoveryKeysResponse;
/** @deprecated Use BarrierRootTokenResponse. */
export type SealRootTokenResponse = BarrierRootTokenResponse;

// =============================================================================
// Inference Types
// Mirrors pkg/orchestration/orchestrator_inference.go and
// sdk/go/client/orchestrator/client.go
// =============================================================================

// InferenceRequest is the request for a stateless fleet-routed inference call.
// Maps to POST /api/v1/orchestrator/inference.
export interface InferenceRequest {
  // backend is the named backend to route to (e.g., "ollama"). Required.
  backend: string;
  // model is the model to use. When empty the backend's default is used.
  model?: string;
  // prompt is the user prompt text. Required.
  prompt: string;
  // system_prompt is an optional system message prepended to the conversation.
  system_prompt?: string;
  // host_affinity pins the call to a specific agent daemon by Name or Host.
  host_affinity?: string;
  // host_affinity_strict, when true, forces hard host_affinity semantics:
  // the server returns no_matching_agent (HTTP 400) if the named host is
  // offline rather than falling back to any available daemon. Defers to the
  // operator-level orchestration.host_affinity.mode default when false.
  host_affinity_strict?: boolean;
  // metadata is an opaque string map forwarded to the backend unchanged.
  metadata?: Record<string, string>;
  // stream, when true, requests chunked NDJSON streaming from the server.
  stream?: boolean;
}

// InferenceContentBlock is one content fragment from a model response.
export interface InferenceContentBlock {
  // type is the content type ("text", "tool_use", etc.).
  type: string;
  // text holds the text content when type is "text".
  text?: string;
}

// InferenceResponse is the aggregated response from a non-streaming call.
export interface InferenceResponse {
  // content holds all content blocks from the response.
  content: InferenceContentBlock[];
  // model is the model that produced the response.
  model?: string;
  // stop_reason is the reason the model stopped generating.
  stop_reason?: string;
}

// InferenceChunk is one delta frame from a streaming inference call.
export interface InferenceChunk {
  // delta is the content emitted in this chunk.
  delta: InferenceContentBlock[];
  // model is set on the final chunk.
  model?: string;
  // stop_reason is set on the final chunk.
  stop_reason?: string;
  // final is true on the last chunk of the stream.
  final?: boolean;
}

// =============================================================================
// WebAuthn / FIDO2 Types
// Mirror the JSON shapes the server and navigator.credentials.get/create accept
// and emit. Field names match the W3C WebAuthn Level 3 specification.
// =============================================================================

// PublicKeyCredentialRequestOptionsJSON is the JSON representation of the
// challenge options returned by the server's webauthn/login/begin endpoint.
// These are passed verbatim to navigator.credentials.get({ publicKey: ... }).
export interface PublicKeyCredentialRequestOptionsJSON {
  challenge: string;
  timeout?: number;
  rpId?: string;
  allowCredentials?: PublicKeyCredentialDescriptorJSON[];
  userVerification?: UserVerificationRequirement;
  extensions?: Record<string, unknown>;
}

// PublicKeyCredentialDescriptorJSON is the JSON form of a WebAuthn credential
// descriptor returned inside challenge options.
export interface PublicKeyCredentialDescriptorJSON {
  id: string;
  type: 'public-key';
  transports?: AuthenticatorTransport[];
}

// PublicKeyCredentialJSON is the JSON-serialized assertion produced by
// navigator.credentials.get() after the user approves the challenge.
export interface PublicKeyCredentialJSON {
  id: string;
  rawId: number[];
  type: string;
  response: AuthenticatorAssertionResponseJSON;
  clientExtensionResults?: Record<string, unknown>;
}

// AuthenticatorAssertionResponseJSON holds the base64url-decoded byte arrays
// from an AuthenticatorAssertionResponse.
export interface AuthenticatorAssertionResponseJSON {
  authenticatorData: number[];
  clientDataJSON: number[];
  signature: number[];
  userHandle: number[] | null;
}

// UserVerificationRequirement mirrors the W3C enum for the userVerification
// field in PublicKeyCredentialRequestOptions.
export type UserVerificationRequirement = 'required' | 'preferred' | 'discouraged';

// AuthenticatorTransport mirrors the W3C enum for the transports field.
export type AuthenticatorTransport = 'usb' | 'nfc' | 'ble' | 'internal' | 'hybrid';

// AuthTokenResponse is the token bundle returned on successful login
// (WebAuthn finish or PIV). Matches the JSON the server writes.
export interface AuthTokenResponse {
  access_token: string;
  id_token: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user?: IAMUser;
}

// PublicTenant is the subset of tenant info exposed by the unauthenticated
// GET /api/v1/iam/tenants/public endpoint.
export interface PublicTenant {
  id: string;
  name: string;
  display_name: string;
}
