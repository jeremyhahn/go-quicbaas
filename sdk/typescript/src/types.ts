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
  | 'error';

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
  version: number;
  metadata: Record<string, string>;
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
}

// =============================================================================
// Backend Types
// Mirrors pkg/orchestration/backend.go
// =============================================================================

// BackendKind classifies a backend as an agent or model server.
export type BackendKind = 'agent' | 'model_server';

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
// Client Configuration
// =============================================================================

// ClientConfig holds configuration for the SDK client.
export interface ClientConfig {
  baseUrl?: string;
}
