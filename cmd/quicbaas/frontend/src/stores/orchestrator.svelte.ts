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

/**
 * Orchestrator state store.
 *
 * Central reactive state for the orchestration subsystem: jobs, agents,
 * sessions, tasks, backends, health, and the live event stream.
 */

// ---------------------------------------------------------------------------
// Domain Types (mirror go-quicbaas/pkg/orchestration)
// ---------------------------------------------------------------------------

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

export type AgentStatus = 'registered' | 'running' | 'idle' | 'offline' | 'error';
export type TaskStatus = 'pending' | 'claimed' | 'in_progress' | 'completed' | 'failed' | 'cancelled';
export type SessionState = string;

export interface JobAuditEvent {
  timestamp: number;
  type: string;
  details: string;
}

export interface WorkItem {
  id: string;
  project_id: string;
  title: string;
  description: string;
  priority: number;
  dependencies?: string[];
  backend?: string;
  required_capabilities?: string[];
  required_role?: string;
  secret_refs?: string[];
  max_retries?: number;
  parallelizable?: boolean;
  fallback_backend?: string;
}

export interface JobMemory {
  [key: string]: unknown;
}

export interface JobConfig {
  tenant_id: string;
  description: string;
  lead_backend?: string;
  lead_model?: string;
  project_id?: string;
  max_iterations?: number;
  workflow_name?: string;
}

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
  error?: string;
  events?: JobAuditEvent[];
  work_items?: WorkItem[];
  config?: JobConfig;
  memory?: JobMemory;
  workflow_name?: string;
}

export interface JobEvent {
  job_id: string;
  type: string;
  state: JobState;
  message: string;
  iteration: number;
  metadata?: Record<string, string>;
  timestamp: number;
}

export interface AgentRegistration {
  id: string;
  tenant_id: string;
  name: string;
  host: string;
  backend: string;
  model?: string;
  agent_type: string;
  status: AgentStatus;
  capabilities?: string[];
  container_id?: string;
  work_dir?: string;
  last_seen: number;
  registered_at: number;
  version: number;
  metadata?: Record<string, string>;
}

export interface SessionInfo {
  session_id: string;
  project_id: string;
  backend: string;
  model?: string;
  state: SessionState;
  created_at: number;
  last_activity: number;
}

export interface TaskResult {
  success: boolean;
  output?: string;
  error?: string;
}

export interface Task {
  id: string;
  tenant_id: string;
  team_name: string;
  subject: string;
  description: string;
  status: TaskStatus;
  assigned_to?: string;
  created_by?: string;
  dependencies?: string[];
  blocks?: string[];
  result?: TaskResult;
  metadata?: Record<string, string>;
  created_at: number;
  updated_at: number;
  claimed_at?: number;
}

export interface BackendCapabilities {
  [key: string]: unknown;
}

export interface AgentBackendInfo {
  name: string;
  type: string;
  capabilities: BackendCapabilities;
  default_model?: string;
}

export interface HealthStatus {
  status: string;
  message?: string;
}

export interface HealthCheckReport {
  status: string;
  timestamp: number;
  checks?: Record<string, HealthStatus>;
}

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

const MAX_EVENTS = 500;

let jobs: JobStatus[] = $state([]);
let selectedJob: JobStatus | null = $state(null);
let jobsLoading = $state(false);

let agents: AgentRegistration[] = $state([]);
let agentsLoading = $state(false);

let sessions: SessionInfo[] = $state([]);
let sessionsLoading = $state(false);

let tasks: Task[] = $state([]);
let tasksLoading = $state(false);

let backends: AgentBackendInfo[] = $state([]);
let health: HealthCheckReport | null = $state(null);
let events: JobEvent[] = $state([]);
let activeTenantID = $state('default');

// ---------------------------------------------------------------------------
// Store (read-only view)
// ---------------------------------------------------------------------------

export const orchestratorStore = {
  get jobs() { return jobs; },
  get selectedJob() { return selectedJob; },
  get jobsLoading() { return jobsLoading; },
  get agents() { return agents; },
  get agentsLoading() { return agentsLoading; },
  get sessions() { return sessions; },
  get sessionsLoading() { return sessionsLoading; },
  get tasks() { return tasks; },
  get tasksLoading() { return tasksLoading; },
  get backends() { return backends; },
  get health() { return health; },
  get events() { return events; },
  get activeTenantID() { return activeTenantID; },
};

// ---------------------------------------------------------------------------
// Mutations
// ---------------------------------------------------------------------------

export function setJobs(j: JobStatus[]): void { jobs = j; }
export function selectJob(j: JobStatus | null): void { selectedJob = j; }
export function setAgents(a: AgentRegistration[]): void { agents = a; }
export function setSessions(s: SessionInfo[]): void { sessions = s; }
export function setTasks(t: Task[]): void { tasks = t; }
export function setBackends(b: AgentBackendInfo[]): void { backends = b; }
export function setHealth(h: HealthCheckReport): void { health = h; }
export function setActiveTenant(id: string): void { activeTenantID = id; }
export function setJobsLoading(v: boolean): void { jobsLoading = v; }
export function setAgentsLoading(v: boolean): void { agentsLoading = v; }
export function setSessionsLoading(v: boolean): void { sessionsLoading = v; }
export function setTasksLoading(v: boolean): void { tasksLoading = v; }

export function appendEvent(e: JobEvent): void {
  events = [...events.slice(-(MAX_EVENTS - 1)), e];
}

export function clearEvents(): void { events = []; }
