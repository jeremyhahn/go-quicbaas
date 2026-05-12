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
 *
 * Polling design
 * ──────────────
 * A single managed poller per resource type lives here. Components subscribe
 * to the store's reactive getters; they never start their own intervals.
 *
 * • POLL_INTERVAL_MS  – how often the background loop fires (30 s).
 * • STALE_AFTER_MS    – minimum age before a load actually hits the network
 *                       (15 s). Calls arriving before this threshold return
 *                       the cached value immediately (stale-while-revalidate).
 * • The poller is paused when the browser tab is hidden and resumed on focus.
 * • startOrchestratorPolling() / stopOrchestratorPolling() scope fetches to
 *   orchestrator views only. Non-orchestrator views (Secrets, KMS, …) never
 *   trigger fetches here.
 * • refresh(resource?) triggers an immediate unconditional refetch, bypassing
 *   the staleness check. Used by Refresh buttons.
 */

import { getSDKClient } from './sdkClient.ts';

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
// Polling constants
// ---------------------------------------------------------------------------

/** How often the background poller fires when the tab is visible. */
export const POLL_INTERVAL_MS = 30_000;

/**
 * Minimum age (in ms) of cached data before a load triggers a real network
 * fetch. Calls arriving while data is fresher than this return immediately
 * without a network round-trip (stale-while-revalidate).
 */
export const STALE_AFTER_MS = 15_000;

// ---------------------------------------------------------------------------
// Resource type
// ---------------------------------------------------------------------------

export type OrchestratorResource = 'jobs' | 'agents' | 'sessions' | 'tasks';

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

// Per-resource timestamps of the last successful fetch (ms since epoch).
// 0 means never fetched.
let _jobsFetchedAt = 0;
let _agentsFetchedAt = 0;
let _sessionsFetchedAt = 0;
let _tasksFetchedAt = 0;
let _healthFetchedAt = 0;

// Poller internals — one interval shared across all resources.
let _pollInterval: ReturnType<typeof setInterval> | null = null;
let _pollActive = false;
let _tabVisible = true;

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

// dedupeBy returns a new array containing only the first occurrence of each
// item whose key (extracted by keyFn) is unique. This guards all store
// mutations against server-side pagination overlap and Raft-replay duplicates
// that would otherwise surface as an each_key_duplicate Svelte reconciler error.
function dedupeBy<T>(arr: T[], keyFn: (item: T) => string): T[] {
  const seen = new Set<string>();
  const result: T[] = [];
  for (const item of arr) {
    const key = keyFn(item);
    if (!seen.has(key)) {
      seen.add(key);
      result.push(item);
    }
  }
  return result;
}

export function setJobs(j: JobStatus[] | null | undefined): void {
  jobs = Array.isArray(j) ? dedupeBy(j, (x) => x.job_id) : [];
  _jobsFetchedAt = Date.now();
}
export function selectJob(j: JobStatus | null): void { selectedJob = j; }
export function setAgents(a: AgentRegistration[] | null | undefined): void {
  agents = Array.isArray(a) ? dedupeBy(a, (x) => x.id) : [];
  _agentsFetchedAt = Date.now();
}
export function setSessions(s: SessionInfo[] | null | undefined): void {
  sessions = Array.isArray(s) ? dedupeBy(s, (x) => x.session_id) : [];
  _sessionsFetchedAt = Date.now();
}
export function setTasks(t: Task[] | null | undefined): void {
  tasks = Array.isArray(t) ? dedupeBy(t, (x) => x.id) : [];
  _tasksFetchedAt = Date.now();
}
export function setBackends(b: AgentBackendInfo[] | null | undefined): void {
  backends = Array.isArray(b) ? dedupeBy(b, (x) => x.name) : [];
}
export function setHealth(h: HealthCheckReport): void {
  health = h;
  _healthFetchedAt = Date.now();
}
export function setActiveTenant(id: string): void { activeTenantID = id; }
export function setJobsLoading(v: boolean): void { jobsLoading = v; }
export function setAgentsLoading(v: boolean): void { agentsLoading = v; }
export function setSessionsLoading(v: boolean): void { sessionsLoading = v; }
export function setTasksLoading(v: boolean): void { tasksLoading = v; }

export function appendEvent(e: JobEvent): void {
  const key = `${e.timestamp}:${e.job_id}:${e.type}:${e.iteration}`;
  // Guard against duplicate events (e.g. SSE stream reconnect replay).
  if (events.some((ev) => `${ev.timestamp}:${ev.job_id}:${ev.type}:${ev.iteration}` === key)) {
    return;
  }
  events = [...events.slice(-(MAX_EVENTS - 1)), e];
}

export function clearEvents(): void { events = []; }

// ---------------------------------------------------------------------------
// Fetch helpers (internal — each updates its timestamp on success)
// ---------------------------------------------------------------------------

async function _fetchJobs(): Promise<void> {
  jobsLoading = true;
  try {
    const client = await getSDKClient();
    const result = await client.orchestrator.listJobs(activeTenantID);
    setJobs(result);
  } catch {
    // Silently ignore transient errors; the next poll will retry.
  } finally {
    jobsLoading = false;
  }
}

async function _fetchAgents(): Promise<void> {
  agentsLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listAgents({ tenant_id: activeTenantID });
    setAgents(resp.agents ?? []);
  } catch {
    // Silently ignore.
  } finally {
    agentsLoading = false;
  }
}

async function _fetchSessions(): Promise<void> {
  sessionsLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listSessions({ tenantID: activeTenantID });
    setSessions(resp.sessions ?? []);
  } catch {
    // Silently ignore.
  } finally {
    sessionsLoading = false;
  }
}

async function _fetchTasks(): Promise<void> {
  tasksLoading = true;
  try {
    const client = await getSDKClient();
    const result = await client.orchestrator.listTasks(activeTenantID);
    setTasks(result);
  } catch {
    // Silently ignore.
  } finally {
    tasksLoading = false;
  }
}

async function _fetchHealth(): Promise<void> {
  try {
    const client = await getSDKClient();
    const data = await client.orchestrator.healthCheck();
    setHealth(data);
  } catch {
    // Silently ignore.
  }
}

// ---------------------------------------------------------------------------
// Stale-while-revalidate loaders (public)
// ---------------------------------------------------------------------------

/**
 * loadJobs fetches jobs only when cached data is older than STALE_AFTER_MS.
 * Returns immediately if the cache is fresh.
 */
export async function loadJobs(): Promise<void> {
  if (Date.now() - _jobsFetchedAt < STALE_AFTER_MS) return;
  await _fetchJobs();
}

/** loadAgents fetches agents only when cached data is stale. */
export async function loadAgents(): Promise<void> {
  if (Date.now() - _agentsFetchedAt < STALE_AFTER_MS) return;
  await _fetchAgents();
}

/** loadSessions fetches sessions only when cached data is stale. */
export async function loadSessions(): Promise<void> {
  if (Date.now() - _sessionsFetchedAt < STALE_AFTER_MS) return;
  await _fetchSessions();
}

/** loadTasks fetches tasks only when cached data is stale. */
export async function loadTasks(): Promise<void> {
  if (Date.now() - _tasksFetchedAt < STALE_AFTER_MS) return;
  await _fetchTasks();
}

/** loadHealth fetches the health report only when cached data is stale. */
export async function loadHealth(): Promise<void> {
  if (Date.now() - _healthFetchedAt < STALE_AFTER_MS) return;
  await _fetchHealth();
}

// ---------------------------------------------------------------------------
// Unconditional refresh (bypasses staleness check)
// ---------------------------------------------------------------------------

/**
 * refresh triggers an immediate unconditional refetch of the given resource,
 * or all resources when called without arguments. Used by Refresh buttons.
 */
export async function refresh(resource?: OrchestratorResource): Promise<void> {
  switch (resource) {
    case 'jobs':     return _fetchJobs();
    case 'agents':   return _fetchAgents();
    case 'sessions': return _fetchSessions();
    case 'tasks':    return _fetchTasks();
    default:
      await Promise.allSettled([_fetchJobs(), _fetchAgents(), _fetchSessions(), _fetchTasks()]);
  }
}

// ---------------------------------------------------------------------------
// Background poller
// ---------------------------------------------------------------------------

// _pollTick fires on the interval. We skip the network round-trip when the
// tab is hidden EXCEPT on the very first call after startup, so a UI loaded
// in a background tab still gets a populated initial state. Without this
// initial-load exemption, opening the app in a background tab leaves the
// orchestrator views permanently empty until the visibilitychange listener
// fires on focus — which never happens if the user clicks around in the
// (hidden) tab without ever switching foreground.
let _initialPollDone = false;
function _pollTick(): void {
  if (!_tabVisible && _initialPollDone) return;
  _initialPollDone = true;
  void Promise.allSettled([loadJobs(), loadAgents(), loadSessions(), loadTasks()]);
}

function _onVisibilityChange(): void {
  _tabVisible = !document.hidden;
  if (_tabVisible && _pollActive) {
    // Resume: fire immediately so data is fresh on tab return.
    _pollTick();
  }
}

/**
 * startOrchestratorPolling starts the background refresh loop and registers
 * the tab-visibility listener. Call this from the layout component that hosts
 * orchestrator views. Returns a cleanup function.
 *
 * Only one poller is ever active: repeated calls from additional subscribers
 * are no-ops until the last cleanup function is invoked.
 */
export function startOrchestratorPolling(): () => void {
  if (_pollActive) {
    // Already running — return a cleanup that is a no-op so the caller can
    // still call it safely from an onDestroy / $effect cleanup.
    return () => { /* no-op: poller started elsewhere */ };
  }

  _pollActive = true;
  _tabVisible = typeof document !== 'undefined' ? !document.hidden : true;

  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', _onVisibilityChange);
  }

  // Kick off an immediate load (respects staleness, avoids duplicate fetches
  // when data was already loaded during the current session).
  _pollTick();

  _pollInterval = setInterval(_pollTick, POLL_INTERVAL_MS);

  return () => {
    _pollActive = false;
    if (_pollInterval !== null) {
      clearInterval(_pollInterval);
      _pollInterval = null;
    }
    if (typeof document !== 'undefined') {
      document.removeEventListener('visibilitychange', _onVisibilityChange);
    }
  };
}

/**
 * stopOrchestratorPolling is a convenience wrapper to stop polling without
 * holding a reference to the cleanup function. Useful for imperative teardown.
 */
export function stopOrchestratorPolling(): void {
  _pollActive = false;
  if (_pollInterval !== null) {
    clearInterval(_pollInterval);
    _pollInterval = null;
  }
  if (typeof document !== 'undefined') {
    document.removeEventListener('visibilitychange', _onVisibilityChange);
  }
}

// ---------------------------------------------------------------------------
// Internal test helpers (exported only for unit-test introspection)
// ---------------------------------------------------------------------------

/** Returns the timestamp of the last successful jobs fetch. Test use only. */
export function _getJobsFetchedAt(): number { return _jobsFetchedAt; }
/** Returns the timestamp of the last successful sessions fetch. Test use only. */
export function _getSessionsFetchedAt(): number { return _sessionsFetchedAt; }
/** Returns whether the background poller is currently active. Test use only. */
export function _isPollActive(): boolean { return _pollActive; }
/** Resets all fetch timestamps. Test use only — allows staleness to be re-tested. */
export function _resetFetchTimestamps(): void {
  _jobsFetchedAt = 0;
  _agentsFetchedAt = 0;
  _sessionsFetchedAt = 0;
  _tasksFetchedAt = 0;
  _healthFetchedAt = 0;
}
