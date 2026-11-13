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
import type { SpendLimits } from './spend.js';
import type { AgentRef, AgentRole, AgentAttempt } from './agents.js';

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
  /** lead is the Agent or pool the job's lead runs as; absent for a job that names none. */
  lead?: AgentRef;
  /** lead_backend and lead_model are what lead resolved to when the job started. */
  lead_backend?: string;
  lead_model?: string;
  project_id: string;
  poll_interval: number;
  max_iterations: number;
  workflow_name: string;
  /** task_id names the task this job is an attempt at; empty for a job started directly. */
  task_id?: string;
  /** attempt is the 1-based ordinal of this job among the task's attempts. */
  attempt?: number;
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
  /** task_id and attempt bind the job to the task it is an attempt at. */
  task_id?: string;
  attempt?: number;
  /** lead_session_id is the lead agent's session; absent until it is spawned. */
  lead_session_id?: string;
  /** session_ids lists every session the job has spawned and not yet closed. */
  session_ids?: string[];
  /** failure_class classifies a failed job's cause: the environment or the work. */
  failure_class?: string;
  /** stage is the last lifecycle moment the job passed through (prepare, reviewable, complete, discard, blocked). */
  stage?: string;
  /** runner names the JobRunner that drove the job's states; absent means the "workflow" runner. */
  runner?: string;
  /** prompt_append is the run-time prompt material the job appended to every session it spawned. */
  prompt_append?: string;
  /** node names the daemon running the job's control loop. */
  node?: string;
  /** heartbeat_at is the nanosecond Unix instant the loop last proved alive. */
  heartbeat_at?: number;

  // --- Provenance: who ran the attempt, and what it left behind -----------
  //
  // Mirrors pkg/orchestration/job_provenance.go. The first two are recorded
  // by the run; the rest are the task's facts, laid over the status when the
  // server reads it.

  /** agent_id is the fleet node the work was dispatched to; absent when it ran in-process. */
  agent_id?: string;
  /** stages is every lifecycle moment the job entered, in order. */
  stages?: JobStageRecord[];
  /** attempts is every attempt at the job's task, oldest first. */
  attempts?: JobAttemptRecord[];
  /** attempt_budget is the number of charged attempts the task allows. */
  attempt_budget?: number;
  /** terminal_reason is the typed reason the task stopped. */
  terminal_reason?: TaskConclusionReason;
  /** readmit_allowed says whether a person may return the task to the queue. */
  readmit_allowed?: boolean;
  /** delivery is the git side of the attempt: branch, head, pull request, reviews. */
  delivery?: JobDelivery | null;
  /** verification is the last validator pipeline's results: the build, the tests. */
  verification?: JobVerificationResult[];
  /**
   * source is where the job's TASK came from — the forge issue it was adopted
   * from. It is the task's own TaskSource, so `issueRef` reads a job exactly
   * as it reads a task. Unlike the rest of this block it is on the LISTING as
   * well as the detail read: a paged jobs view holds no tasks to resolve it
   * from (go-quicbaas#976).
   */
  source?: TaskSource | null;
}

/** JobVerificationResult is one verification command's outcome. */
export interface JobVerificationResult {
  name: string;
  passed: boolean;
  message?: string;
  details?: string;
}

/** JobStageRecord is one lifecycle moment the job entered. */
export interface JobStageRecord {
  stage: string;
  /** entered_at is the Unix-nanosecond instant the moment was entered. */
  entered_at: number;
}

/** JobAttemptRecord is one attempt at the job's task. */
export interface JobAttemptRecord {
  attempt: number;
  job_id: string;
  /** current marks the attempt the status being read belongs to. */
  current?: boolean;
}

/**
 * TaskConclusionReason is the closed vocabulary naming why a task stopped.
 * Mirrors pkg/orchestration/task_conclusion.go; a reason added there and not
 * here is a compile error at every render site rather than a blank badge.
 */
export type TaskConclusionReason =
  | 'completed'
  | 'budget_exhausted'
  | 'abandoned'
  | 'fault_bounded'
  | 'fault_held'
  | 'parked_needs_info'
  | 'parked_blocked'
  | 'parked_escalated'
  | 'readmitted';

/** JobReviewRound is one reviewer's verdict on one head of the job's branch. */
export interface JobReviewRound {
  round: number;
  reviewer: string;
  /** verdict is the reviewing module's own vocabulary: approve, request_changes, error. */
  verdict: string;
  head_sha?: string;
  findings?: string;
  /** submitted_at is an RFC 3339 instant. */
  submitted_at?: string;
}

/** JobDelivery is the git side of the job's attempt. */
export interface JobDelivery {
  branch?: string;
  head_sha?: string;
  pull_request_number?: number;
  pull_request_url?: string;
  pull_request_state?: string;
  reviews?: JobReviewRound[];
}

// JobEventType names the events a running control loop emits.
export type JobEventType = 'state_change';

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
  /** lead is the Agent or pool the lead runs as; absent takes the project's default lead. */
  lead?: AgentRef;
  project_id: string;
  max_iterations: number;
  poll_interval?: string;
  workflow_file?: string;
  /** runner names the JobRunner that drives the job's states; omit for the "workflow" runner. */
  runner?: string;
  /**
   * prompt_append is run-time prompt material appended to every session the
   * job spawns, the lead's included. At most 64 KiB.
   */
  prompt_append?: string;
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

// ListJobsRequest lists a tenant's jobs.
// Mirrors orchestrator.ListJobsRequest.
export interface ListJobsRequest {
  tenant_id: string;

  // task_id narrows the listing to the attempts at one task. Omit to list
  // every job the tenant owns. The SERVER applies it, as it does every field
  // below.
  task_id?: string;

  /** runner narrows the listing to the jobs one runner drove. */
  runner?: string;

  /** project_id narrows the listing to one project's jobs. */
  project_id?: string;

  /** statuses narrows the listing to jobs in any of these states. */
  statuses?: JobState[];

  /**
   * job_ids narrows the listing to the named jobs — how a caller holding a
   * page of something else resolves the jobs its rows name in ONE call.
   */
  job_ids?: string[];

  /** agent_id narrows the listing to the jobs dispatched to one fleet node. */
  agent_id?: string;

  /** The shared paging parameters; see PageParams in page.ts. */
  limit?: number;
  offset?: number;
  sort?: string;
  q?: string;
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
  /** project_id restricts the listing to one project. */
  project_id?: string;
  /** statuses restricts the listing to tasks in any of these statuses; composes with status. */
  statuses?: TaskStatus[];

  /** created_by restricts the listing to one creator. */
  created_by?: string;

  /**
   * auto_spawn restricts the listing to the tasks that asked for immediate
   * dispatch (true) or the ones queued for a claim (false). Absent does not
   * narrow.
   */
  auto_spawn?: boolean;

  /**
   * agent_id narrows the listing to the tasks whose LATEST ATTEMPT ran on one
   * fleet node. The agent is the attempt's job's, not the task's; the server
   * resolves it for the listing.
   */
  agent_id?: string;

  /** job_id narrows the listing to the task whose latest attempt is that job. */
  job_id?: string;

  /**
   * source_kind and source_ref, set together, narrow the listing to the task
   * holding that source reservation in project_id's scope. A source nobody
   * holds lists nothing; it is not an error.
   */
  source_kind?: string;
  source_ref?: string;

  /** The shared paging parameters; see PageParams in page.ts. */
  limit?: number;
  offset?: number;
  sort?: string;
  q?: string;
}

// ListTasksResponse is the response from listing tasks.
// Mirrors orchestrator.ListTasksResponse.
export interface ListTasksResponse {
  tasks: Task[];
  count: number;
}

// =============================================================================
// Agent Registry Types
// Mirrors pkg/orchestration/fleet_registry_types.go
// =============================================================================

// FleetNodeStatus represents the lifecycle state of a registered fleet node.
export type FleetNodeStatus =
  | 'registered'
  | 'running'
  | 'idle'
  | 'offline'
  | 'error'
  | 'disabled';

// FleetNode represents a node registered in the fleet registry.
// Mirrors orchestration.FleetNode.
export interface FleetNode {
  id: string;
  tenant_id: string;
  name: string;
  host: string;
  backend: string;
  model: string;
  node_type: string;
  status: FleetNodeStatus;
  capabilities: string[];
  /**
   * Every backend name this node is matched on: the primary backend first,
   * then each capability, each once. Never null (go-quicbaas#700).
   */
  backends: string[];
  work_dir: string;
  last_seen: number;
  registered_at: number;
  disabled_at?: number;
  version: number;
  metadata: Record<string, string>;
  // advertised_address is the address the node asked the control plane to
  // dial (`fleet serve --advertise`), when it named one.
  advertised_address?: string;
  // reachability is what registration learned about dialing the node:
  // "reachable" when the control plane probed it, "unverified" otherwise.
  reachability?: FleetNodeReachability;
}

// FleetNodeReachability is what registration learned about dialing a node.
export type FleetNodeReachability = 'reachable' | 'unverified';

// DisableFleetNodeRequest identifies a fleet node to disable globally.
export interface DisableFleetNodeRequest {
  tenant_id: string;
  node_id: string;
}

// DisableFleetNodeResponse is returned after a successful fleet node disable.
export interface DisableFleetNodeResponse {
  node_id: string;
  disabled: boolean;
  disabled_at: string;
  timestamp: string;
}

// EnableFleetNodeRequest identifies a disabled fleet node to re-enable.
export interface EnableFleetNodeRequest {
  tenant_id: string;
  node_id: string;
}

// EnableFleetNodeResponse is returned after a successful fleet node enable.
export interface EnableFleetNodeResponse {
  node_id: string;
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
  | 'cancelled'
  | 'blocked';

// TaskResult contains the output of a completed task.
// Mirrors orchestration.TaskResult.
export interface TaskResult {
  output: string;
  files: string[];
  metadata: Record<string, string>;
}

// TaskSourceKind names the place a task originated from. The engine's own
// kinds are listed; a registered task source declares any other kind it emits.
// Mirrors orchestration.TaskSourceKind.
export type TaskSourceKind = 'scheduler' | 'operator' | 'job' | (string & {});

// TaskSource records the provenance of a task: the kind of origin, a
// kind-specific reference (an issue number or `owner/repo#N` for a forge
// issue), and, when the origin has one, its URL.
// Mirrors orchestration.TaskSource.
export interface TaskSource {
  kind: TaskSourceKind;
  ref: string;
  url?: string;
}

// TaskBlockKind classifies why a task is parked on a person.
// Mirrors orchestration.TaskBlockKind.
export type TaskBlockKind = 'needs_info' | 'blocked' | 'escalated';

// TaskFaultOrigin says whose failure stopped a task: the environment (the
// fleet, an account pool, a forge), the work itself, or an attempt whose node
// stopped with nothing to diagnose it. '' for a task that did not stop on a
// failure. Mirrors orchestration.TaskFaultOrigin.
export type TaskFaultOrigin = 'environment' | 'work' | 'undiagnosed' | '';

// ArtifactKind classifies how an artifact's content is stored and referenced.
// Mirrors orchestration.ArtifactKind.
export type ArtifactKind = 'inline' | 'file' | 'commit' | 'kv_key' | 'url';

// ArtifactRef locates an artifact that is not inline.
// Mirrors orchestration.ArtifactRef.
export interface ArtifactRef {
  agent_id?: string;
  path?: string;
  sha?: string;
  branch?: string;
}

// Artifact is a named input or output of a task.
// Mirrors orchestration.Artifact.
export interface Artifact {
  kind: ArtifactKind | string;
  name: string;
  mime_type?: string;
  size?: number;
  /** content carries inline bytes, base64-encoded on the wire. */
  content?: string;
  ref?: ArtifactRef;
  metadata?: Record<string, string>;
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
  /** project_id scopes the task to a Project; empty for unscoped tasks. */
  project_id?: string;
  /**
   * agent_id is the fleet node the task's LATEST ATTEMPT ran on, resolved by
   * the server when the listing is paged. Absent for a task that has had no
   * attempt, or whose attempt ran in-process.
   */
  agent_id?: string;
  /** output_artifacts are what the task produced (pull requests, commits, files). */
  output_artifacts?: Artifact[];
  /** input_artifacts are what the task was given to work from. */
  input_artifacts?: Artifact[];
  /** source records where the task came from; absent for hand-created tasks. */
  source?: TaskSource;
  /** priority orders dispatch; higher runs first. */
  priority?: number;
  /** attempts counts every job dispatched for this task, refunded or not. */
  attempts?: number;
  /** attempt_budget is the number of charged attempts allowed before the task blocks. */
  attempt_budget?: number;
  /** environment_faults counts attempts refunded because the environment, not the work, failed. */
  environment_faults?: number;
  /** consecutive_environment_faults is the current run of refunded attempts. */
  consecutive_environment_faults?: number;
  /** retry_not_before is a nanosecond Unix timestamp before which the task is not redispatched; 0 when not deferred. */
  retry_not_before?: number;
  /** blocked_reason says, in words, what a person must resolve. */
  blocked_reason?: string;
  /** block_kind classifies the block; absent when the task is not blocked. */
  block_kind?: TaskBlockKind;
  /** status_changed_at is a nanosecond Unix timestamp of the last status transition. */
  status_changed_at?: number;
  /** runner names the JobRunner that drives an attempt; absent means the lead-driven 'workflow' runner. */
  runner?: string;
  /** prompt_append is run-time prompt material every attempt carries to the sessions it spawns. */
  prompt_append?: string;
  /** routing are the placement hints a dispatch reads, kept so a requeue keeps them. */
  routing?: TaskRouting;
  /** dispatch_reason says why the last dispatch produced no job; cleared when one does. */
  dispatch_reason?: string;
  /** question_attempts counts attempts refunded because the worker stopped to ask a person. */
  question_attempts?: number;
  /** block_fault_class names the environment fault class a block came from; '' for a hold or a question. */
  block_fault_class?: string;
  /**
   * conclusion_reason is the typed reason the task stopped; '' while it is
   * open. Open to strings because the server's vocabulary is wider than the
   * reasons TaskConclusionReason renders (runner_parked, already_satisfied,
   * superseded).
   */
  conclusion_reason?: TaskConclusionReason | string;
  /** charged_attempts is attempts less the refunded ones. Derived by the server. */
  charged_attempts?: number;
  /** effective_budget is attempt_budget, or the engine default when none is declared. Derived by the server. */
  effective_budget?: number;
  /** fault_class names the fault behind the latest concluded attempt; for a blocked task, its block's class. Derived by the server. */
  fault_class?: string;
  /** fault_origin says whose failure stopped the task. Derived by the server. */
  fault_origin?: TaskFaultOrigin;
}

// TaskRouting are the placement hints a dispatch reads when it decides where
// an attempt at a task runs. Mirrors orchestration.TaskRouting.
export interface TaskRouting {
  /** backend restricts the attempt to agents running this backend. */
  backend?: string;
  /** host_affinity pins the attempt to an agent by name or host. */
  host_affinity?: string;
  /** gpu_required restricts the attempt to agents advertising "gpu". */
  gpu_required?: boolean;
}

// =============================================================================
// Attention inbox — everything only an operator can clear
// =============================================================================
//
// Mirrors sdlc.AttentionItem / sdlc.AttentionKind / sdlc.AttentionAction.

// AttentionKind classifies one attention item.
export type AttentionKind = 'parked_task' | 'agent_offline' | 'account_error' | 'barrier_sealed' | 'project_idle';

// AttentionAction is one thing an operator may do to resolve an item.
export type AttentionAction =
  | 'approve_recommendation'
  | 'answer'
  | 'allow_another_round'
  | 'close_as_done'
  | 'readmit'
  | 'note';

// AttentionItem is one thing the operator, and only the operator, can clear:
// a parked task, or an environmental alert.
export interface AttentionItem {
  id: string;
  kind: AttentionKind;
  project_id?: string;
  project_name?: string;
  subject: string;
  issue_ref?: string;
  issue_url?: string;
  block_kind?: TaskBlockKind;
  /** condition is the typed unblock condition the park names — see
   * sdlc.UnblockKind: pipeline_green, forge_issue_state, release_tag,
   * owner_ruling, operator_repair. */
  condition?: string;
  question?: string;
  recommendation?: string;
  /** truncated is true when the underlying reply was capped before it was
   * stored, so recommendation may end mid-sentence. */
  truncated?: boolean;
  /** ruling_posted is true when the parked task's issue carries an
   * authorized ruler's comment newer than the park — the owner has already
   * answered, and the unblock sweep readmits the task. */
  ruling_posted?: boolean;
  /** message is an environmental alert's own explanation. */
  message?: string;
  /** severity is "warning" or "critical", set only on environmental alerts. */
  severity?: string;
  created_at?: number;
  status_changed_at?: number;
  attempts?: number;
  actions?: AttentionAction[];
}

// AttentionResolveRequest applies an operator's decision to one blocked task.
// Mirrors orchestrator.AttentionResolveRequest.
export interface AttentionResolveRequest {
  tenant_id: string;
  task_id: string;
  action: AttentionAction;
  /** answer carries the operator's free text. Required for "answer";
   * optional context for "close_as_done"; ignored otherwise. */
  answer?: string;
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
// Job runner types
// Mirrors pkg/orchestration/job_runner.go
// =============================================================================

// JobStateSpec declares one state a runner drives a job through.
// Mirrors orchestration.JobStateSpec (IdleThreshold is the engine's and is
// not published).
export interface JobStateSpec {
  /** state is the state itself, as it appears on JobStatus.state. */
  state: JobState;
  /** label is how a consumer names the state to a person. */
  label: string;
  /** session_backed says an agent session is doing the work in this state. */
  session_backed: boolean;
}

// JobRunnerInfo is one registered runner and the ordered, non-terminal states
// it drives a job through. Mirrors orchestration.JobRunner.
export interface JobRunnerInfo {
  /** name is what JobStatus.runner names the runner by. */
  name: string;
  /** states are the runner's non-terminal states in order. */
  states: JobStateSpec[];
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
  /** prompt_append is run-time prompt material appended to every session of this role. At most 64 KiB. */
  prompt_append?: string;
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
  /** phase is the control-loop state the criterion is judged in: validating (default) or reviewing. */
  phase?: CriterionPhase;
}

// CriterionPhase names the control-loop state a review criterion is judged in.
// Mirrors orchestration.CriterionPhase.
export type CriterionPhase = 'validating' | 'reviewing';

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
  /** hooks names the registered lifecycle hook set every job of the workflow runs; empty runs none. */
  hooks?: string;
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

// =============================================================================
// Spend reporting — the consumption ledger's read side
// Mirrors pkg/orchestration/spend_report.go
// =============================================================================

// SpendLevel names one level of the ledger's attribution chain.
export type SpendLevel = 'tenant' | 'project' | 'task' | 'job' | 'session' | 'round';

// SPEND_UNATTRIBUTED addresses the subtree of entries that have NO value at a
// chain level — a job that belongs to no task, a turn charged to no project.
//
// Supply it as a scope level to open the row a report marks `unattributed`,
// and map it back to "no project" / "no task" when rendering a scope. It is
// unambiguous because the server refuses it as an entity id at the write: no
// real project, task, job or session can bear this identifier.
// Mirrors orchestration.SpendUnattributed.
export const SPEND_UNATTRIBUTED = '-';

// SpendPolicyStatus is what the server could establish about one account's
// allowance.
//
// THE THREE ARE DIFFERENT FACTS AND MUST NOT BE COLLAPSED. 'none' says the
// account was read and meters nothing — the honest "no spend policy", not a
// budget of zero. 'unknown' says nothing could read it, which a renderer must
// say rather than implying the account is uncapped.
export type SpendPolicyStatus = 'unknown' | 'none' | 'metered';

// SpendWindowStatus is one sliding quota window and what has been drawn
// against it.
export interface SpendWindowStatus {
  name: string;
  // size is the window's length in NANOSECONDS, as Go's time.Duration is
  // encoded by encoding/json.
  size: number;
  // max_tokens is 0 when the window does not meter tokens.
  max_tokens: number;
  used_tokens: number;
  remaining_tokens: number;
  // The dollar axis, in millionths of a US dollar at list price over the
  // window's priced turns; absent or 0 when the window does not meter dollars.
  max_equivalent_micros?: number;
  used_equivalent_micros?: number;
  remaining_equivalent_micros?: number;
  // exhausted is true when either metered axis has nothing left.
  exhausted: boolean;
  // frees_at is RFC 3339, absent when the window holds nothing.
  frees_at?: string;
}

// SpendBreakerVerdict is an account's derived circuit-breaker state and the
// evidence it was derived from.
export interface SpendBreakerVerdict {
  state: 'closed' | 'open' | 'half_open';
  failures: number;
  successes: number;
  last_failure_at?: string;
  last_success_at?: string;
  // open_until is when an open breaker becomes half-open.
  open_until?: string;
}

// SpendTotals is what a set of consumption entries sums to: the shape of a
// report's own totals and of every breakdown row under it. Mirrors
// orchestration.ConsumptionTotals; its fields sit flat on each row.
export interface SpendTotals {
  entries?: number;
  // input_tokens is the fresh prompt; the two cache dimensions are prompt
  // tokens served from and written into the provider's cache.
  input_tokens?: number;
  output_tokens?: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  total_tokens: number;
  // equivalent_micros is the list-price equivalent of the PRICED entries in
  // millionths of the report's currency: what the tokens would have cost on
  // the provider's metered API. CONSULT priced_entries BEFORE RENDERING IT —
  // zero priced entries is a dash, never a zero; unpriced_entries > 0 beside
  // priced ones makes the figure a floor.
  equivalent_micros?: number;
  priced_entries?: number;
  unpriced_entries?: number;
  // billed_micros is what the turns added to the bill actually paid: zero
  // per turn on a subscription, the list price on a metered key. Consult
  // billed_entries the same way.
  billed_micros?: number;
  billed_entries?: number;
  unbilled_entries?: number;
  duration_ms?: number;
  // energy_mwh is the energy drawn by the turns costed from electricity, in
  // milliwatt-hours (a kWh is 1e6); electricity_entries counts them. Consult
  // electricity_entries before rendering the energy: none is a dash.
  energy_mwh?: number;
  electricity_entries?: number;
  // measured_energy_entries counts the electricity entries whose energy came
  // from fleet device telemetry (measured or derived) rather than from a
  // configured draw; the rest of electricity_entries were estimated.
  measured_energy_entries?: number;
  successes?: number;
  failures?: number;
}

// SpendAccountBilling is how a pooled account is billed.
export interface SpendAccountBilling {
  // mode is 'subscription' or 'api'; absent when not configured.
  mode?: string;
  // monthly_fee_micros is a subscription's flat monthly fee.
  monthly_fee_micros?: number;
}

// SpendAccount is one pooled account's contribution to a scope, and the
// allowance it draws against.
export interface SpendAccount extends SpendTotals {
  account_id: string;
  display_name?: string;
  provider?: string;
  billing?: SpendAccountBilling;
  policy_status: SpendPolicyStatus;
  windows?: SpendWindowStatus[];
  // breaker is absent when the account carries no breaker rule, which is not
  // the same as a closed breaker.
  breaker?: SpendBreakerVerdict;
  // subscription_fee_micros is the part of a subscription's monthly fee that
  // falls in the report's range. Present only on a tenant-level report and
  // only for a subscription whose fee is configured; absent renders as a dash.
  subscription_fee_micros?: number;
}

// SpendModel is one engine/model pair's share of a scope's tokens.
export interface SpendModel extends SpendTotals {
  engine: string;
  model: string;
}

// SpendChild is one drill-down row: the next chain level below the scope.
export interface SpendChild extends SpendTotals {
  // id is the child's identifier, absent when unattributed.
  id?: string;
  level: SpendLevel;
  // unattributed marks the subtree the ledger files under its no-value
  // sentinel — a job with no task. Real spend with no parent at this level,
  // to be labelled rather than shown as an entity named '-'.
  unattributed?: boolean;
  // name is the child's human name where the level has one (a project),
  // absent when unattributed or when the entity no longer exists.
  name?: string;
}

// GetSpendReportRequest addresses one scope of the attribution chain.
//
// The levels are a PATH: each may be set only if every level above it is. A
// scope that skips one is refused by the server.
export interface GetSpendReportRequest {
  tenant_id?: string;
  project_id?: string;
  task_id?: string;
  job_id?: string;
  session_id?: string;
  // round selects one round below the session, applied only when round_set is
  // true so round 0 — a real level — is distinguishable from "every round".
  round?: number;
  round_set?: boolean;
  // from and to (RFC 3339) narrow the report to the turns that occurred in
  // [from, to). from alone ranges up to now; to alone is refused.
  from?: string;
  to?: string;
  // host narrows the report to the turns that ran on one host, as the
  // report's hosts breakdown names it; SPEND_UNATTRIBUTED selects the turns
  // that name none. Absent reports every host.
  host?: string;
  // agent narrows the report to the turns of the sessions one tenant Agent
  // ran; SPEND_UNATTRIBUTED selects raw-backend sessions. The report then
  // carries that Agent's own limits.
  agent?: string;
}

// SpendAgent is one tenant Agent's share of a scope's spend.
export interface SpendAgent extends SpendTotals {
  // agent_id is the Agent's catalog ID, absent when unattributed.
  agent_id?: string;
  // name is the Agent's name when it still exists.
  name?: string;
  // unattributed marks the turns of raw-backend sessions.
  unattributed?: boolean;
}

// SpendHost is one host's share of a scope's spend.
export interface SpendHost extends SpendTotals {
  // host is the host the turns ran on, absent when unattributed.
  host?: string;
  // unattributed marks the turns that name no host.
  unattributed?: boolean;
}

// GetSpendReportResponse is one scope's consumption and its drill-down.
export interface GetSpendReportResponse extends SpendTotals {
  level: SpendLevel;
  tenant_id: string;
  project_id?: string;
  task_id?: string;
  job_id?: string;
  session_id?: string;
  round?: number;

  // project_name is the name of the project the scope addresses, when it
  // still exists.
  project_name?: string;

  // currency is what every money figure on the report is denominated in.
  currency?: string;

  first_at?: string;
  last_at?: string;

  // from and to echo the range the report was narrowed to; absent for a
  // report over the scope's whole history.
  from?: string;
  to?: string;

  // subscription_fee_micros sums the accounts' fee shares: what the tenant's
  // subscriptions cost over the range, beside the per-turn billed figure.
  // Absent below the tenant level or when no subscription carries a fee.
  subscription_fee_micros?: number;

  accounts?: SpendAccount[];
  models?: SpendModel[];

  // child_level names the level children are at, absent at the deepest level.
  child_level?: SpendLevel;
  children?: SpendChild[];

  // scope_policy_status and scope_windows are the scope's OWN spend limits —
  // a project's at the project level, the tenant's at the tenant level — with
  // live usage against each window. A spent one defers admission. Absent at
  // the deeper levels.
  scope_policy_status?: SpendPolicyStatus;
  scope_windows?: SpendWindowStatus[];

  // host echoes the host the report was narrowed to, absent for every host;
  // hosts breaks the totals down by the host each turn ran on.
  host?: string;
  hosts?: SpendHost[];

  // agent echoes the Agent the report was narrowed to; agents breaks the
  // totals down by the Agent each turn's session ran as.
  agent?: string;
  agents?: SpendAgent[];

  // agent_policy_status and agent_windows are the narrowed-to Agent's own
  // spend limits with live usage — what a spawn of the Agent is checked
  // against. Absent when the report is not narrowed to one Agent.
  agent_policy_status?: SpendPolicyStatus;
  agent_windows?: SpendWindowStatus[];
}

// GetTenantSpendPolicyRequest names the tenant whose limits to read.
export interface GetTenantSpendPolicyRequest {
  tenant_id: string;
}

// SetTenantSpendPolicyRequest replaces a tenant's limits; no windows makes the
// tenant unmetered.
export interface SetTenantSpendPolicyRequest {
  tenant_id: string;
  limits: SpendLimits;
}

// TenantSpendPolicy is a tenant's own limits and its live usage against each
// window, over every turn charged to it (go-quicbaas#1058).
export interface TenantSpendPolicy {
  tenant_id: string;
  limits?: SpendLimits;
  // updated_at is absent for a tenant that never set limits.
  updated_at?: string;
  windows?: SpendWindowStatus[];
}

// SessionTurnRole names who produced a turn.
// Mirrors orchestration.SessionTurnRole.
export type SessionTurnRole = 'user' | 'assistant' | 'system';

// SessionTokenUsage is a turn's token accounting as the backend reported it.
// Mirrors orchestration.TokenUsage.
export interface SessionTokenUsage {
  input_tokens?: number;
  output_tokens?: number;
  cache_creation_input_tokens?: number;
  cache_read_input_tokens?: number;
}

// SessionToolUse summarises one tool call inside an assistant turn. The
// arguments and the output stay on the event stream: a conversation page
// indexes the work rather than carrying it.
// Mirrors orchestration.SessionToolUse.
export interface SessionToolUse {
  invocation_id?: string;
  name: string;
  is_error?: boolean;
  result_bytes?: number;
}

// SessionTurn is one record in a session's conversation.
// Mirrors orchestration.SessionTurn.
export interface SessionTurn {
  // sequence is the log sequence of the turn's first event, and the cursor
  // that addresses it.
  sequence: number;
  session_id: string;
  tenant_id?: string;
  role: SessionTurnRole;
  // timestamp is RFC 3339, as Go's encoding/json emits it.
  timestamp: string;
  text: string;
  // truncated_bytes is how much text the server's storage cap removed.
  // Non-zero means text is not the whole of what was said, and the stored
  // text carries a visible marker where it was cut.
  truncated_bytes?: number;
  model?: string;
  stop_reason?: string;
  tools?: SessionToolUse[];
  usage?: SessionTokenUsage;
}

// GetSessionConversationRequest pages through a session's conversation.
// Mirrors orchestrator.GetSessionConversationRequest.
export interface GetSessionConversationRequest {
  tenant_id?: string;
  session_id: string;
  // cursor is a previous page's next_cursor. 0 or omitted starts at the
  // beginning of the RETAINED conversation, which retention may have moved.
  cursor?: number;
  // limit is the maximum turns to return; 0 or omitted selects the server
  // default (100), and the server caps it at 500.
  limit?: number;
}

// GetSessionConversationResponse carries one page of a session's conversation.
// Mirrors orchestrator.GetSessionConversationResponse.
export interface GetSessionConversationResponse {
  session_id: string;
  tenant_id?: string;
  turns: SessionTurn[];
  // next_cursor is the cursor for the following page, 0 at the end.
  next_cursor?: number;
  // oldest_sequence is the lowest sequence the server still holds. Above 1
  // means retention has dropped earlier turns.
  oldest_sequence?: number;
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

// MCPServerConfig declares an MCP server made available to a spawned agent.
// Mirrors orchestration.MCPServerConfig. An HTTP server (such as the QuicBaaS
// MCP endpoint) is described by url + env; a stdio server by command + args.
// env carries connection material — e.g. the agent's mTLS cert/key file paths
// for the QuicBaaS endpoint — and round-trips losslessly to the spawned agent.
export interface MCPServerConfig {
  name: string;
  url?: string;
  command?: string;
  args?: string[];
  env?: Record<string, string>;
}

// SpawnSessionRequest creates a new agent session.
// Mirrors orchestrator.SpawnSessionRequest.
export interface SpawnSessionRequest {
  // tenantID threads the orchestrator tenant into the request. Required —
  // the server rejects spawns without a tenant context.
  tenantID: string;
  // agent_id or pool_id names what to spawn from the tenant's catalog; a pool
  // fails over along its members. With neither, backend (and model) are raw.
  agent_id?: string;
  pool_id?: string;
  // role is the slot the session fills; a spawn by Agent checks the Agent
  // declares it.
  role?: AgentRole;
  backend?: string;
  project_id?: string;
  model?: string;
  work_dir?: string;
  env?: Record<string, string>;
  // sandbox_mode selects the sandboxing policy for the spawned session.
  sandbox_mode?: SandboxModeValue;
  // sandboxed is deprecated in favour of sandbox_mode; kept for server
  // compatibility with older daemons.
  sandboxed?: boolean;
  // host_affinity pins the session to a specific fleet node daemon (by Name or Host).
  host_affinity?: string;
  // host_affinity_strict, when true, forces hard host_affinity semantics:
  // the server returns no_matching_agent (HTTP 400) if the named host is
  // offline rather than falling back to any available daemon. Defers to the
  // operator-level orchestration.host_affinity.mode default when false.
  host_affinity_strict?: boolean;
  // mcp_servers preconfigures MCP servers for the spawned agent. The QuicBaaS
  // MCP endpoint preset gives the agent native, cert-authenticated access to
  // platform tools; arbitrary stdio/HTTP servers may also be declared.
  mcp_servers?: MCPServerConfig[];
  // account_id pins the session to one pooled provider account. Leaving both
  // account_id and account_auto unset preserves the pre-account behavior
  // exactly: the backend authenticates however its CLI already does.
  account_id?: string;
  // account_auto requests strategy-based account selection when account_id
  // is empty.
  account_auto?: boolean;
  // account_strategy names the registered selection strategy to use
  // ("static" | "least_usage"). Empty selects the registry default. Ignored
  // unless account_auto is true.
  account_strategy?: string;
  // account_allow optionally names the accounts this session may use and
  // rotate through — "these seats, nothing else". It is an additive filter,
  // never a source: an ID the tenant does not own is simply not a candidate.
  //
  // Together with account_auto it is what authorizes rotation. A session with
  // neither is treated as a deliberate pin and is never moved off its account.
  account_allow?: string[];
  // prompt_append is run-time prompt material appended after the session's
  // resolved prompt: the Agent's own, else its role's built-in charter. At
  // most 64 KiB.
  prompt_append?: string;
  // keep marks a session bound to no job as one the operator means to keep:
  // reconciliation concludes every other job-less session once it has been
  // quiet past the idle bound. It changes nothing for a session bound to a
  // job, which still ends with its job. Persisted on the session record.
  keep?: boolean;
}

// SpawnSessionResponse is the response from creating a new session.
// Mirrors orchestrator.SpawnSessionResponse.
export interface SpawnSessionResponse {
  session_id: string;
  backend: string;
  container_id?: string;
  // resolved_agent_id is the catalog Agent the session runs as, for a spawn
  // by Agent or pool.
  resolved_agent_id?: string;
  // failovers are the pool members passed over before resolved_agent_id, and why.
  failovers?: AgentAttempt[];
}

// PromptSessionRequest sends a prompt to an existing session.
// Mirrors orchestrator.PromptSessionRequest.
export interface PromptSessionRequest {
  // tenant_id is the session's tenant. The MCP transport requires it; the
  // REST route scopes by the caller and ignores it.
  tenant_id?: string;
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
  // tenant_id is the session's tenant. The MCP transport requires it; the
  // REST route scopes by the caller and ignores it.
  tenant_id?: string;
  session_id: string;
}

// CloseSessionResponse is the response from closing a session.
// Mirrors orchestrator.CloseSessionResponse.
export interface CloseSessionResponse {
  session_id: string;
  closed: boolean;
}

// AccountRotationMode reports which mechanism moved a session onto its
// replacement account. Mirrors orchestration.AccountRotationMode.
export type AccountRotationMode = 'in_place' | 'resume';

// RotateSessionAccountRequest asks the orchestrator to move a session onto a
// different pooled provider account.
// Mirrors orchestration.RotateSessionAccountRequest. session_id is carried on
// the struct for the non-REST transports; REST treats the path segment as
// authoritative and ignores a mismatched body value.
export interface RotateSessionAccountRequest {
  // tenant_id is the session's tenant. The MCP transport requires it; the
  // REST route scopes by the caller.
  tenant_id?: string;
  session_id: string;
  // reason is recorded on the closed account segment and in the session
  // transcript. Optional — an empty rotation carries no explanation.
  reason?: string;
  // hold_outgoing marks the account being left as limited until available_at,
  // keeping the next selection from walking straight back into whatever
  // prompted the rotation. Leave false for a plain rebalance.
  hold_outgoing?: boolean;
  // available_at is when a held outgoing account rejoins the pool (RFC 3339).
  // Ignored unless hold_outgoing is set; omitted applies the service's
  // default backoff.
  available_at?: string;
}

// RotateSessionAccountResponse reports the outcome of a rotation.
// Mirrors orchestration.RotateSessionAccountResult.
export interface RotateSessionAccountResponse {
  session_id: string;
  // previous_account_id is the account the session was moved off.
  previous_account_id?: string;
  // account_id is the replacement now in force.
  account_id: string;
  // strategy names the policy that selected the replacement.
  strategy?: string;
  // mode reports which mechanism was used. "resume" means the agent process
  // was closed and reconstituted — the transcript is replayed, not lost, but
  // the process itself restarts.
  mode: AccountRotationMode;
}

// ListSessionsRequest filters the session list.
// Mirrors orchestrator.ListSessionsRequest.
export interface ListSessionsRequest {
  tenantID: string;
  backend?: string;
  status?: string;
  /** project_id narrows the listing to one project's sessions. */
  project_id?: string;
  /** job_id narrows the listing to the sessions one job spawned. */
  job_id?: string;
  /**
   * runner narrows the listing to the sessions whose job one runner drove —
   * which is the ROLE the session played.
   */
  runner?: string;
  /** agent_id narrows the listing to the sessions on one fleet node. */
  agent_id?: string;

  /** The shared paging parameters; see PageParams in page.ts. */
  limit?: number;
  offset?: number;
  sort?: string;
  q?: string;
}

// SessionStatusResponse carries the status of a single session.
// Mirrors orchestrator.SessionStatusResponse.
export interface SessionStatusResponse {
  session_id: string;
  backend: string;
  /** model is the model the session runs on, when the service reported one. */
  model?: string;
  status: string;
  container_id?: string;
  created_at: number;
  last_active: number;
  /** job_id names the orchestration job this session works for, if any. */
  job_id?: string;
  /**
   * runner is the job runner that drove that job — `sdlc`, `review`,
   * `triage`, `workflow` — and so which ROLE the session played. The service
   * resolves it per page, so a consumer holding one page of sessions never
   * has to read the job list to find out. Absent when the session belongs to
   * no job, or to one no longer on record.
   */
  runner?: string;
  /**
   * agent_id is the fleet node the session's job was dispatched to, resolved
   * beside `runner` and from the same record. Absent when the session belongs
   * to no job or the work ran in-process.
   */
  agent_id?: string;
}

// ListSessionsResponse is the response from listing sessions.
// Mirrors orchestrator.ListSessionsResponse.
export interface ListSessionsResponse {
  sessions: SessionStatusResponse[];
  count: number;
}

// GetFleetNodeRequest retrieves a single fleet node by ID.
// Mirrors orchestrator.GetFleetNodeRequest.
export interface GetFleetNodeRequest {
  tenant_id: string;
  node_id: string;
}

// ListFleetNodesRequest filters the fleet listing.
// Mirrors orchestrator.ListFleetNodesRequest.
export interface ListFleetNodesRequest {
  tenant_id: string;
  host?: string;
  status?: string;
  node_type?: string;
  backend?: string;
  model?: string;

  /** The shared paging parameters; see PageParams in page.ts. */
  limit?: number;
  offset?: number;
  sort?: string;
  q?: string;
}

// ListFleetNodesResponse is the response from listing fleet nodes.
// Mirrors orchestrator.ListFleetNodesResponse.
export interface ListFleetNodesResponse {
  nodes: FleetNode[];
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

  // runner names the JobRunner that drives attempts at this task. Empty
  // resolves to the engine's own "workflow" runner — a lead agent
  // decomposing the task into work items. A name nothing registered is
  // refused.
  runner?: string;

  // prompt_append is run-time prompt material every attempt at the task
  // carries to the sessions it spawns (the worker's, for a module runner).
  // At most 64 KiB.
  prompt_append?: string;
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

// BlockTaskRequest parks a task until a person readmits it.
// Maps to POST /api/v1/orchestrator/tasks/:taskID/block
export interface BlockTaskRequest {
  tenant_id: string;
  task_id: string;
  kind: TaskBlockKind;
  reason: string;
}

// ReadmitTaskRequest returns a blocked task to the pending queue.
// Maps to POST /api/v1/orchestrator/tasks/:taskID/readmit
export interface ReadmitTaskRequest {
  tenant_id: string;
  task_id: string;
}

// DeleteTaskRequest removes a task and releases its source reservation, so
// the issue or ticket the task was adopted from can be adopted again.
// Maps to DELETE /api/v1/orchestrator/tasks/:taskID
export interface DeleteTaskRequest {
  tenant_id: string;
  task_id: string;
}

// DeleteTaskResponse confirms the task was removed.
export interface DeleteTaskResponse {
  task_id: string;
  deleted: boolean;
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

// RegisterFleetNodeRequest registers a new node with the fleet.
// Mirrors orchestrator.RegisterFleetNodeRequest.
export interface RegisterFleetNodeRequest {
  tenant_id: string;
  name: string;
  host: string;
  backend: string;
  node_type: string;
  model?: string;
  metadata?: Record<string, string>;
}

// RegisterFleetNodeResponse contains the registered fleet node.
export interface RegisterFleetNodeResponse {
  node: FleetNode;
}

// RemoveFleetNodeRequest removes a registered fleet node by ID.
// Mirrors orchestrator.RemoveFleetNodeRequest.
export interface RemoveFleetNodeRequest {
  tenant_id: string;
  node_id: string;
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
// Mirrors pkg/fleet/deploy/spec.go — fields are snake_case to match the
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
  /** The pinned host key as an authorized_keys-format line. */
  public_key?: string;
}

// AddSSHPinRequest is the body for POST /api/v1/ssh/known-hosts.
// Mirrors xkmsssh.AddKnownHostRequest. The pin is the full host key; the
// server derives its fingerprint and algorithm from it.
export interface AddSSHPinRequest {
  /** Scope that owns this pin. */
  scope?: SSHScope;
  /** Owner within the scope. */
  owner?: string;
  /** Hostname or IP address to pin. */
  host: string;
  /** Host key as an authorized_keys-format line, e.g. "ssh-ed25519 AAAA…". */
  public_key: string;
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

// DeployFleetNodeSpec describes how to deploy a QuicBaaS fleet node to a remote host.
// Required fields: host, ssh_user, server, tenant, node_bind, backends,
// binary. The mTLS identity for the daemon is minted by the server-side
// deploy flow — no bootstrap token is required. Optional fields fall back
// to server-side defaults.
export interface DeployFleetNodeSpec {
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
  node_bind: string;
  backends: string[];

  binary: string;
  install_dir?: string;
  data_dir?: string;

  /** Tri-state: undefined=auto-detect, true=systemd, false=nohup. */
  systemd?: boolean;

  /** Nanoseconds (Go time.Duration). Zero disables the online wait. */
  wait_online?: number;
}

// DeployFleetNodeResult is returned by the deploy workflow on success.
export interface DeployFleetNodeResult {
  host: string;
  installed_path: string;
  service_status: string;
  node_id?: string;
  online_at?: string;
  /** Path to the mTLS client certificate installed on the remote host. */
  node_cert_path?: string;
  /** Hex-encoded serial number of the issued fleet node certificate. */
  node_cert_serial?: string;
}

// =============================================================================
// IAM Types
// Mirrors /api/v1/tenants and /api/v1/iam REST routes.
// =============================================================================

// UserLifecycle is an account's state. It is the only account-state field:
// only `active` may authenticate.
export type UserLifecycle = 'pending' | 'active' | 'suspended' | 'locked' | 'disabled' | 'deleting';

// IAMUser represents a user identity managed by the IAM service. An account
// carries no group list: quicbaas keeps no groups, and the OIDC `groups` claim
// carries the names of the roles the account is bound to.
export interface IAMUser {
  id: string;
  tenant_id: string;
  username: string;
  email: string;
  lifecycle?: UserLifecycle;
  roles: string[];
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
  /**
   * Non-fatal irregularity behind an otherwise successful activation —
   * currently, an activation write that reported an error on a user the server
   * then found active. The ceremony succeeded and the invite was consumed, so
   * this is not a failure, but it should be surfaced rather than dropped.
   */
  warning?: string;
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
  // retired_permissions are permissions the role still carries that the running
  // server build no longer defines. Derived on read from the live permission
  // registry and never stored: it names the drift between what the tenant was
  // seeded with and what the server grants today. Reconciling the tenant's
  // built-in roles clears it.
  retired_permissions?: string[];
  created_at: string;
  updated_at: string;
}

// ListRolesResponse is the response from listing roles.
export interface ListRolesResponse {
  roles: IAMRole[];
  count: number;
}

// RoleReconcileResult describes what reconciliation changed on a single role.
export interface RoleReconcileResult {
  role_id: string;
  name: string;
  created?: boolean;
  added?: string[];
  removed?: string[];
}

// ReconcileRolesResponse is the response from reconciling a tenant's built-in
// roles onto the definitions the running server build carries.
export interface ReconcileRolesResponse {
  tenant_id: string;
  roles?: RoleReconcileResult[];
  changed: boolean;
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

// Wire note: a barrier credential is BYTES, not text. go-qrdb types every
// barrier secret as []byte (so the caller can wipe it), and encoding/json
// renders a []byte as base64 — so `secret` / `passphrase` below are base64
// strings on the wire, exactly like every other byte-slice field in this SDK
// (see the wire note in crypto.ts). Callers encode at the UI boundary:
//
//   secret: btoa(String.fromCharCode(...new TextEncoder().encode(passphrase)))
//
// Always encode. The server rejects MALFORMED base64 with 400, and nothing
// else: a raw passphrase that happens to be valid base64 (any run of the
// base64 alphabet whose length is a multiple of four, e.g. "password") decodes
// into different bytes and is used as the credential, silently
// (go-quicbaas#988).

// BarrierInitRequest holds the secret used to initialize the barrier with
// a single master key. Mirrors transport.BarrierInitializeRequest.
export interface BarrierInitRequest {
  /** Passphrase used to derive the master key. The REST client base64-encodes it. */
  secret: Uint8Array;
}

// BarrierUnsealRequest holds the secret used to unseal the barrier.
// Mirrors transport.BarrierUnsealRequest.
export interface BarrierUnsealRequest {
  /** Passphrase used to unseal the barrier. The REST client base64-encodes it. */
  secret: Uint8Array;
}

// BarrierSealRequest carries the Security Officer PIN that authorises a seal.
// Mirrors transport.BarrierSealRequest.
export interface BarrierSealRequest {
  /** Security Officer PIN. Required when the server has a PIN backend with an SO PIN set. */
  so_pin?: string;
}

// BarrierShamirInitRequest initialises the barrier with Shamir secret
// sharing. Mirrors transport.BarrierInitializeShamirRequest.
export interface BarrierShamirInitRequest {
  /** Optional passphrase used to derive the master key. The REST client base64-encodes it. */
  secret: Uint8Array;
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
  /** Passphrase used to derive the master key (passphrase strategy). The REST client base64-encodes it. */
  passphrase?: Uint8Array;
  /** Minimum shares required to reconstruct the master key (shamir strategy). */
  threshold?: number;
  /** Total shares to generate (shamir strategy). */
  shares?: number;
}

// TenantBarrierInitResponse is returned for the shamir strategy. It is
// share-free (mirrors go-qrdb transport.TenantBarrierInitResponse): the shares
// are sealed to officer enclaves and claimed individually. The "none" and "passphrase" strategies return
// 204 No Content, so callers receive undefined for those.
export interface TenantBarrierInitResponse {
  /** Tenant whose barrier was initialized. */
  tenant_id: string;
  /** Sealing strategy that was provisioned. */
  strategy: TenantBarrierStrategy;
  /** Threshold required for reconstruction. */
  threshold: number;
  /** Total shares generated. */
  total_shares: number;
  /**
   * Shares sealed to Security Officer enclaves. Share material is never
   * returned: each officer claims an individual share through the tenant
   * share-claim flow.
   */
  share_count: number;
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
// Mirrors transport.BarrierHasRecoveryKeysResponse, whose field is tagged
// `has_keys` — the REST wire name served by GET /api/v1/barrier/recovery/status
// (go-quicbaas#991). The MCP and gRPC surfaces spell the same bit
// `has_recovery_keys` and their clients map it onto this field.
export interface BarrierHasRecoveryKeysResponse {
  /** True when recovery keys have been generated. */
  has_keys: boolean;
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

/**
 * BarrierEnableEncryptionRequest migrates a previously unencrypted barrier to a
 * keyed strategy. Mirrors go-qrdb transport.BarrierEnableEncryptionRequest.
 */
export interface BarrierEnableEncryptionRequest {
  /** Tenant whose dedicated barrier to migrate. Omit for the system barrier. */
  tenant_id?: string;
  /** Keyed strategy to migrate to ("passphrase" or "shamir"). Required. */
  strategy: string;
  /** Re-encryption approach ("swap" or "inplace"). The server defaults to "swap". */
  mechanism?: string;
  /** Write availability during migration ("online" or "quiesced"). The server defaults to "online". */
  availability?: string;
  /** New passphrase for a passphrase target. The REST client base64-encodes it. */
  passphrase?: Uint8Array;
  /** Shamir threshold (M) for a Shamir target. */
  shamir_threshold?: number;
  /** Shamir share count (N) for a Shamir target. */
  shamir_total?: number;
}

/**
 * BarrierEnableEncryptionResponse acknowledges that the migration has started.
 * Poll status() to observe completion. Mirrors go-qrdb
 * transport.BarrierEnableEncryptionResponse.
 */
export interface BarrierEnableEncryptionResponse {
  /** Migration state; "migration_started" when accepted. */
  status: string;
  /** Barrier the migration runs on: "system" or "tenant". */
  scope: string;
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

// CeremonyClaimShareBeginRequest starts the Shamir share claim handshake.
// Mirrors transport.BeginClaimShareRequest. The caller's certificate
// thumbprint is bound server-side from the mTLS peer; it never travels here.
export interface CeremonyClaimShareBeginRequest {
  /** Officer username. */
  username: string;
}

// CeremonyClaimShareBeginResponse carries the nonce the officer must sign.
// Mirrors transport.BeginClaimShareResponse.
export interface CeremonyClaimShareBeginResponse {
  /** Hex-encoded random nonce the officer must sign. */
  nonce_hex: string;
  /** RFC3339 expiration timestamp for the nonce. */
  expires_at: string;
}

// CeremonyClaimShareCompleteRequest proves the officer's key over the nonce.
// Mirrors transport.CompleteClaimShareRequest.
export interface CeremonyClaimShareCompleteRequest {
  /** Officer username. */
  username: string;
  /** Hex-encoded nonce from claimShareBegin. */
  nonce_hex: string;
  /** Base64-encoded signature over the nonce. */
  signature: string;
}

// CeremonyClaimShareCompleteResponse returns the officer's Shamir share.
// Mirrors transport.CompleteClaimShareResponse.
export interface CeremonyClaimShareCompleteResponse {
  /** Index of the officer's share in the M-of-N split. */
  share_index: number;
  /** The officer's Shamir share, JSON-encoded. */
  share_json: string;
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

// BootstrapBundleRequest is the payload for each POST /api/v1/bootstrap/{method}
// call. server_url is required by every mode (except Noise when noise_addr is
// supplied explicitly). The per-mode material fields map directly onto the
// service.BootstrapRequest JSON contract the daemon decodes.
export interface BootstrapBundleRequest {
  /** Canonical server URL to bootstrap trust against (e.g. "https://kms.example.com:8443"). */
  server_url?: string;
  /** Filter by store type: "root", "intermediate", "leaf", "end-entity", or empty. */
  store_type?: string;
  /** Filter by key algorithm: "RSA", "ECDSA", "Ed25519", or empty. */
  algorithm?: string;
  /**
   * DANE: DNSSEC zone under which TLSA records are published
   * (e.g. "_443._tcp.kms.example.com" or the bare hostname).
   * Required for dane; optional knob for auto.
   */
  hostname?: string;
  /**
   * DANE: optional recursive resolver for the TLSA lookup
   * (e.g. "8.8.8.8:53"). Defaults to the system resolver when omitted.
   */
  dns_server?: string;
  /**
   * Noise: explicit Noise bootstrap address ("host:port").
   * When empty the address is derived from server_url.
   */
  noise_addr?: string;
  /**
   * Noise: hex-encoded 32-byte Curve25519 server static public key.
   * Required for noise; optional knob for auto.
   */
  noise_static_key?: string;
  /**
   * SPKI: hex-encoded SHA-256 pin of the server certificate's
   * SubjectPublicKeyInfo (64 hex characters).
   * Required for spki; optional knob for auto.
   */
  spki_pin?: string;
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
// Cluster
// =============================================================================

// ClusterShard describes one Raft shard's leadership and state. Mirrors the
// shard entries in the daemon's GET /api/v1/cluster/status response.
export interface ClusterShard {
  shard_id: number;
  leader_id: number;
  state: string;
}

// ClusterStatusResponse is the wire shape returned by
// GET /api/v1/cluster/status. In standalone (non-Raft) mode `shards` is empty
// and `total_nodes` is 1. Mirrors the Go SDK cluster.ClusterStatus.
export interface ClusterStatusResponse {
  node_id: number;
  raft_address?: string;
  leader_id?: string;
  total_nodes?: number;
  shards?: ClusterShard[];
  healthy: boolean;
  timestamp: string;
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
  // host_affinity pins the call to a specific fleet node daemon by Name or Host.
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

// CredentialAssertionJSON is the go-webauthn login challenge envelope returned
// by the server's webauthn/login/begin endpoint. The challenge options are
// nested under `publicKey` (W3C CredentialRequestOptions shape); binary members
// (challenge, allowCredentials[].id) are base64url strings. Callers MUST unwrap
// the envelope and base64url-decode the binary members before passing them to
// navigator.credentials.get() — the frontend webauthn helper does this.
export interface CredentialAssertionJSON {
  publicKey: PublicKeyCredentialRequestOptionsJSON;
  mediation?: string;
}

// PublicKeyCredentialRequestOptionsJSON is the inner (unwrapped) login challenge
// options. Binary members are base64url strings, not BufferSource.
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
// navigator.credentials.get() after the user approves the challenge. The shape
// matches go-webauthn's CredentialAssertionResponse: binary members are
// base64url strings (NOT number[] arrays) under camelCase keys, which is what
// the server's ParseCredentialRequestResponseBytes parser requires.
export interface PublicKeyCredentialJSON {
  id: string;
  rawId: string;
  type: string;
  authenticatorAttachment?: string;
  response: AuthenticatorAssertionResponseJSON;
  clientExtensionResults?: AuthenticationExtensionsClientOutputs;
}

// AuthenticatorAssertionResponseJSON holds the base64url-encoded byte members
// of an AuthenticatorAssertionResponse, matching go-webauthn's wire types.
export interface AuthenticatorAssertionResponseJSON {
  authenticatorData: string;
  clientDataJSON: string;
  signature: string;
  userHandle?: string;
}

// UserVerificationRequirement mirrors the W3C enum for the userVerification
// field in PublicKeyCredentialRequestOptions.
export type UserVerificationRequirement = 'required' | 'preferred' | 'discouraged';

// AuthenticatorTransport mirrors the W3C enum for the transports field.
export type AuthenticatorTransport = 'usb' | 'nfc' | 'ble' | 'internal' | 'hybrid';

// AuthTokenResponse is the token bundle returned on successful login
// (WebAuthn finish or PIV). Matches the JSON the server writes. The WebAuthn
// path rides the unified login ceremony (/api/v1/auth/login/finish), whose
// bearer + rotating refresh credential map onto access_token/refresh_token;
// id_token is present only on flows that mint one (e.g. PIV OIDC issuance).
export interface AuthTokenResponse {
  access_token: string;
  id_token?: string;
  refresh_token: string;
  token_type: string;
  expires_in: number;
  user?: IAMUser;
}

// WebAuthnLoginChallenge carries the state of an in-flight unified-ceremony
// WebAuthn login between webAuthnBeginLogin and webAuthnFinishLogin:
// the ceremony session (session_id), the relying-party session
// (rp_session_id), and the go-webauthn challenge envelope the caller feeds
// to navigator.credentials.get().
export interface WebAuthnLoginChallenge {
  // session_id is the unified login-ceremony session identifier
  // (POST /api/v1/auth/login/begin response session_id). It MUST be echoed
  // back on finish.
  session_id: string;

  // rp_session_id is the relying-party (WebAuthn) ceremony session id the
  // server issued alongside the challenge options. It MUST be echoed back
  // on finish as method metadata.
  rp_session_id: string;

  // options is the go-webauthn `{ publicKey: { ... } }` challenge envelope.
  // Callers unwrap + base64url-decode it (see the frontend webauthn helper)
  // before navigator.credentials.get().
  options: CredentialAssertionJSON;
}


// =============================================================================
// Cost settings (go-quicbaas#1058)
// =============================================================================
//
// Local inference is costed from electricity:
//   power_watts × turn seconds / 3.6e6 kWh × electricity_price_per_kwh
// These are the two inputs a tenant can set at runtime. Each beats the
// tenant's configured override, which beats the deployment's; the rate
// defaults to $0.12/kWh.

// GetCostSettingsRequest names the tenant whose cost settings to read.
export interface GetCostSettingsRequest {
  tenant_id?: string;
}

// SetCostSettingsRequest replaces a tenant's cost settings. An absent rate and
// an engine not named inherit the configuration; an engine named with 0 watts
// is not costed from electricity for the tenant.
export interface SetCostSettingsRequest {
  tenant_id?: string;
  electricity_price_per_kwh?: number;
  engine_power_watts?: Record<string, number>;
}

// ElectricityRateSource names where the rate in force came from.
export type ElectricityRateSource = 'tenant_settings' | 'tenant_config' | 'deployment_config' | 'default';

// CostSettings is a tenant's runtime settings and the rates in force once
// layered over the configuration.
export interface CostSettings {
  settings: {
    tenant_id: string;
    electricity_price_per_kwh?: number;
    engine_power_watts?: Record<string, number>;
    updated_at?: string;
  };
  electricity: { price_per_kwh: number; source: ElectricityRateSource };
  // engine_power_watts is every per-engine draw in force, in watts.
  engine_power_watts?: Record<string, number>;
}

// =============================================================================
// The Software Factory worker image refresh (docs/agent-fleet/worker-image.md)
// =============================================================================

// WorkerImageAgents are the two agent CLI versions an image carries.
export interface WorkerImageAgents {
  claude_code: string;
  codex: string;
}

// WorkerImageCheck is one probe or smoke the refresh gate ran.
export interface WorkerImageCheck {
  name: string;
  passed: boolean;
  detail?: string;
}

// WorkerImageBound is one managed image bindings name, as a pass found it.
export interface WorkerImageBound {
  image: string;
  // agents are the CLI versions its labels declare; empty when unreadable.
  agents: WorkerImageAgents;
  // uses counts the Agents naming it that a binding slot (worker, reviewer, triage) references.
  uses: number;
  stale: boolean;
}

// WorkerImagePromotion is one Agent a pass repointed at the new image, or tried to.
export interface WorkerImagePromotion {
  tenant_id: string;
  agent_id: string;
  from: string[] | null;
  to: string;
  // error is why the Agent was NOT updated; absent on success.
  error?: string;
}

// WorkerImageOutcome is how a pass ended.
export type WorkerImageOutcome = 'current' | 'promoted' | 'partial' | 'failed';

// WorkerImageReport is what one refresh pass did.
export interface WorkerImageReport {
  // trigger is "schedule" (the daily pass) or "manual".
  trigger: string;
  outcome: WorkerImageOutcome;
  started_at: string;
  finished_at: string;
  // latest are the npm registry versions the pass read.
  latest: WorkerImageAgents;
  bound?: WorkerImageBound[];
  stale?: string[];
  skipped?: string[];
  candidate?: string;
  image_id?: string;
  // reference is what bindings were rebound to.
  reference?: string;
  digest?: string;
  probes?: WorkerImageCheck[];
  smokes?: WorkerImageCheck[];
  promotions?: WorkerImagePromotion[];
  error?: string;
}

// WorkerImageStatus is the refresh loop's state as an operator reads it.
export interface WorkerImageStatus {
  serving: boolean;
  running: boolean;
  running_since?: string;
  queued: boolean;
  // last is the most recent pass that finished; absent before the first.
  last?: WorkerImageReport;
}
