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
 * Central reactive state for the orchestration subsystem: jobs, fleet nodes,
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

import { untrack } from 'svelte';
import type {
  Artifact,
  HealthCheckReport,
  JobAttemptRecord,
  JobDelivery,
  JobMemory,
  JobStageRecord,
  JobVerificationResult,
  TaskBlockKind,
  TaskConclusionReason,
  TaskFaultOrigin,
  Task,
  TaskResult,
  TaskSource,
} from '@quicbaas/sdk';
import { getSDKClient } from './sdkClient.ts';
import { toSessionInfo } from '../lib/sessions.ts';
import {
  announceSessionScopeChange,
  authStore,
  onSessionScopeChange,
} from './auth.svelte.ts';

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

/**
 * ACTIVE_JOB_STATES is every state a job can still leave — the complement of
 * isTerminalJobState, spelled out because a SERVER filter narrows by naming
 * states and cannot be asked for "not terminal".
 *
 * It is declared beside JobState on purpose: a state added to the union above
 * and forgotten here would silently drop that job from the Active filter, and
 * two lists in two files drift the moment one of them is edited alone.
 */
export const ACTIVE_JOB_STATES: readonly JobState[] = [
  'initializing',
  'decomposing',
  'scheduling',
  'assigning',
  'executing',
  'validating',
  'reviewing',
  're-delegating',
  'completing',
  'cleanup',
  'escalating',
];

export type FleetNodeStatus = 'registered' | 'running' | 'idle' | 'offline' | 'error' | 'disabled';
export type TaskStatus = 'pending' | 'claimed' | 'in_progress' | 'completed' | 'failed' | 'cancelled' | 'blocked';
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

// JobMemory is the SDK's: one declaration of the job's memory, not a looser
// copy that the SDK's records will not assign into.
export type { JobMemory };

export interface JobConfig {
  tenant_id: string;
  description: string;
  /** lead is the Agent or pool the job's lead runs as; absent for a job that names none. */
  lead?: { agent_id?: string; pool_id?: string };
  /** lead_backend and lead_model are what lead resolved to when the job started. */
  lead_backend?: string;
  lead_model?: string;
  project_id?: string;
  max_iterations?: number;
  workflow_name?: string;
  /** task_id is the Task this job was spawned for; absent for lead-driven jobs. */
  task_id?: string;
  /** attempt is the 1-based attempt number of task_id this job represents. */
  attempt?: number;
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
  config?: JobConfig | null;
  memory?: JobMemory | null;
  workflow_name?: string;
  /** task_id is the Task this job is an attempt at; absent for lead-driven jobs. */
  task_id?: string;
  /** attempt is the 1-based ordinal of this job among the task's attempts. */
  attempt?: number;
  /** lead_session_id is the lead agent's session; absent until it is spawned. */
  lead_session_id?: string;
  /** session_ids lists every session the job has spawned and not yet closed. */
  session_ids?: string[];
  /** failure_class classifies a failed job's cause: the environment or the work. */
  failure_class?: string;
  /** stage is the last lifecycle moment the job passed through. */
  stage?: string;
  /** runner names the JobRunner that drove the job's states; absent means the "workflow" runner. */
  runner?: string;
  /** node names the daemon that ran the job's control loop. */
  node?: string;
  /** prompt_append is the run-time prompt material the job appended to every session it spawned. */
  prompt_append?: string;

  // Provenance — mirrors pkg/orchestration/job_provenance.go. The nested
  // shapes come from the SDK: one declaration, not a second copy that drifts.

  /** agent_id is the fleet node the work was dispatched to. */
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
  /** delivery is the git side of the attempt. */
  delivery?: JobDelivery | null;
  /** verification is the last validator pipeline's results. */
  verification?: JobVerificationResult[];
  /**
   * source is the forge issue behind the job's task, laid over the record by
   * the server — on the PAGED LISTING as well as the detail read, because a
   * paged view holds no tasks to resolve it from (go-quicbaas#976).
   */
  source?: TaskSource | null;
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

export interface FleetNode {
  id: string;
  tenant_id: string;
  name: string;
  host: string;
  backend: string;
  model?: string;
  node_type: string;
  status: FleetNodeStatus;
  capabilities?: string[];
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
  /** job_id names the orchestration job this session works for, if any. */
  job_id?: string;
}

// The store's task types are the SDK's, as the SDK client returns them.
export type { Task, TaskResult };

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

export type { HealthCheckReport };

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

export type OrchestratorResource = 'jobs' | 'fleet' | 'sessions' | 'tasks';

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

const MAX_EVENTS = 500;

let jobs: JobStatus[] = $state([]);
let selectedJob: JobStatus | null = $state(null);
let jobsLoading = $state(false);

// ONE TASK'S ATTEMPT HISTORY, read from the server rather than sieved out of
// the tenant-wide list.
//
// The tenant list is the recent jobs an operator browses; a task page needs
// EVERY attempt at ONE task, including the ones that concluded days ago and
// left memory. Filtering the tenant list for them is the mistake
// go-quicbaas#822 measured on the burn-in node: a task with three charged
// attempts rendered "JOBS (0)", because none of its three jobs was among the
// seven the list happened to hold. The server applies the narrowing now, so
// the page asks for what it needs.
let taskJobs: JobStatus[] = $state([]);
let taskJobsTaskID = $state('');
let taskJobsLoading = $state(false);

let fleetNodes: FleetNode[] = $state([]);
let fleetNodesLoading = $state(false);

let sessions: SessionInfo[] = $state([]);
let sessionsLoading = $state(false);

let tasks: Task[] = $state([]);
let tasksLoading = $state(false);
// The last task fetch failure, kept so the Tasks views can say why the list
// is empty (a 403 names the missing permission) instead of showing nothing.
// Cleared by the next successful fetch and by every scope change.
let tasksError: unknown = $state(null);

let backends: AgentBackendInfo[] = $state([]);
let health: HealthCheckReport | null = $state(null);
let events: JobEvent[] = $state([]);

/**
 * jobEventRevision counts the job events that said something about a job's
 * own record. A paged jobs list watches it and re-reads its page; see the
 * getter on orchestratorStore for why that is not a patch.
 */
let jobEventRevision = $state(0);
/**
 * qrdb's canonical control-plane tenant (qrdbsdk.SystemTenantID), always
 * provisioned at first boot. It is the LAST RESORT only — the tenant to address
 * when there is no session to read one from (Wails desktop, where auth is
 * OS-level and no bearer exists, and the pre-login window in the browser).
 */
export const SYSTEM_TENANT_ID = 'system';

/**
 * Tenant the operator explicitly selected in the header switcher, and the
 * session identity that selection was made under.
 *
 * The pair is what scopes an override to one session. An override is a
 * deliberate act by someone permitted to see other tenants; it is NOT a stored
 * preference, and it must not outlive the session it was made in. When a
 * different session takes over, the recorded identity stops matching and the
 * override lapses on its own — no logout hook to forget, and no chance of a
 * stale selection sending the next operator's queries at a tenant they cannot
 * see.
 *
 * The identity is the principal's `uid` AND their tenant, not the tenant alone.
 * Two people can share a tenant: a system admin who selects `burnin`, signs out,
 * and hands the tab to a second system admin would otherwise pass their
 * selection along, because the tenant claim alone cannot tell the two apart.
 */
let tenantOverride = $state('');
let tenantOverrideBase = $state('');

/** Identity an override belongs to: this principal, in this tenant. */
function sessionIdentity(): string {
  return authStore.sessionKey;
}

/**
 * Who the cached data belongs to: the session that asked for it, and the tenant
 * it was asked about.
 *
 * Both halves are needed. The tenant alone cannot separate two principals who
 * share one, and the session alone cannot separate the tenants a system admin
 * switches between. Everything below — the cached arrays, the freshness
 * timestamps, and every in-flight request — is tagged with this and is only
 * valid while it still matches.
 */
function currentScope(): string {
  return `${sessionIdentity()}\u0000${currentTenantID()}`;
}

/**
 * The scope key anything tenant-scoped should be tagged with.
 *
 * Exported because the two sibling stores (fleetStore, sessionStore) keep their
 * own SDK-typed caches of the same data and read their tenant from here. They
 * need the SAME key, not a second definition of it: a store that computed its
 * own notion of "whose data is this" would drift from this one, and the whole
 * point is that every cache in the app agrees on when it has gone stale.
 */
export function tenantScope(): string {
  return currentScope();
}

/**
 * readyScope is the scope a route-driven read may be issued under, or '' while
 * no session has been validated.
 *
 * A detail page opened from a deep link (`#/jobs/:id`, `#/sessions/:id`,
 * `#/tasks/:id`) issues its read as soon as the shell mounts, and start-up
 * validation can move the session scope while that read is in flight — the
 * session is adopted and its generation moves, although nobody signed in
 * again. Every such read correctly DROPS an answer asked under a scope that is
 * no longer in effect, so the page stayed empty ("No job selected") because
 * the effect that asked was keyed on the route alone and never asked again.
 *
 * The effect that follows a route reads this, so it re-runs on every scope
 * change and asks again under the scope now in effect — and asks nothing at
 * all until there is a session to ask for.
 */
export function readyScope(): string {
  return authStore.status === 'authenticated' ? currentScope() : '';
}

/**
 * Scope the cached arrays and freshness timestamps currently belong to.
 *
 * Empty at startup: nothing has been fetched, so nothing is in scope, and the
 * getters correctly report empty rather than claiming the initial [] belongs to
 * whoever signs in first.
 */
let _dataScope = $state('');

/** Whether the cached data still belongs to the session and tenant in effect. */
function _inScope(): boolean {
  return _dataScope === currentScope();
}

// Any scope change — a session beginning or ending, or the operator switching
// tenant — releases everything tenant-scoped immediately rather than leaving it
// to the next load, and then asks for the replacement. The scope key already
// makes the old data UNREACHABLE; this is what makes "signing out clears
// protected data" and "switching tenant shows the new tenant" things the code
// does.
//
// The refetch is skipped when there is no session to fetch for: a sign-out would
// otherwise fire four requests nobody is waiting for, against a credential that
// has just been thrown away.
onSessionScopeChange(() => {
  _enforceScope();
  if (authStore.status === 'authenticated') void refresh();
});

/**
 * Stable empty arrays handed out when the cache is out of scope.
 *
 * Module constants rather than fresh `[]` literals: a getter that allocated a
 * new array on every read would change identity on every read, and any $derived
 * or $effect comparing it would re-run forever.
 */
const NO_JOBS: JobStatus[] = [];
const NO_FLEET_NODES: FleetNode[] = [];
const NO_SESSIONS: SessionInfo[] = [];
const NO_TASKS: Task[] = [];
const NO_EVENTS: JobEvent[] = [];

/**
 * Drop everything belonging to a session or tenant that is no longer in effect.
 *
 * Called before every load, so the corrective fetch after a change is never
 * suppressed by the previous scope's freshness timestamps: tenant A loads jobs,
 * signs out, tenant B signs in, and B's first load must go to the network rather
 * than being told the 15-second-old data is fresh.
 *
 * The getters gate on scope independently, so B never RENDERS A's rows even in
 * the window before this runs. This is what releases them.
 */
function _enforceScope(): void {
  const scope = currentScope();
  if (scope === _dataScope) return;
  _dataScope = scope;
  jobs = [];
  selectedJob = null;
  taskJobs = [];
  taskJobsTaskID = '';
  fleetNodes = [];
  sessions = [];
  tasks = [];
  tasksError = null;
  events = [];
  _jobsFetchedAt = 0;
  _fleetFetchedAt = 0;
  _sessionsFetchedAt = 0;
  _tasksFetchedAt = 0;
}

/**
 * The tenant every orchestrator query is scoped to.
 *
 * The session's `tenant_id` claim is the source of truth, re-derived from the
 * bearer on every load rather than remembered (go-quicbaas#309: this was pinned
 * to the literal 'system', so an operator authenticated into any other tenant
 * queried a tenant they are not in and got 403s and empty Fleet, Sessions,
 * Tasks, Workflows and Jobs views).
 */
function currentTenantID(): string {
  const claim = authStore.tenantID;
  if (tenantOverride !== '' && tenantOverrideBase === sessionIdentity()) {
    return tenantOverride;
  }
  return claim !== '' ? claim : SYSTEM_TENANT_ID;
}

// Per-resource timestamps of the last successful fetch (ms since epoch).
// 0 means never fetched.
let _jobsFetchedAt = 0;
let _fleetFetchedAt = 0;
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
  // Every tenant-scoped getter reports empty the instant the cache stops
  // belonging to the session and tenant in effect. That is what stops a new
  // operator's shell from rendering the previous one's jobs, agents, sessions
  // and tasks in the window before the corrective fetch lands — the check is on
  // the READ, so there is no window at all.
  //
  // backends and health are node-level, not tenant-scoped, and are not gated.
  get jobs() { return _inScope() ? jobs : NO_JOBS; },
  get selectedJob() { return _inScope() ? selectedJob : null; },
  get jobsLoading() { return jobsLoading; },
  /** The attempts at the task named by taskJobsTaskID, newest first. */
  get taskJobs() { return _inScope() ? taskJobs : NO_JOBS; },
  /** The task taskJobs belongs to; '' when none has been read. */
  get taskJobsTaskID() { return _inScope() ? taskJobsTaskID : ''; },
  get taskJobsLoading() { return taskJobsLoading; },
  get fleetNodes() { return _inScope() ? fleetNodes : NO_FLEET_NODES; },
  get fleetNodesLoading() { return fleetNodesLoading; },
  get sessions() { return _inScope() ? sessions : NO_SESSIONS; },
  get sessionsLoading() { return sessionsLoading; },
  get tasks() { return _inScope() ? tasks : NO_TASKS; },
  get tasksLoading() { return tasksLoading; },
  get tasksError() { return _inScope() ? tasksError : null; },
  get backends() { return backends; },
  get health() { return health; },
  get events() { return _inScope() ? events : NO_EVENTS; },
  get activeTenantID() { return currentTenantID(); },
  /**
   * jobEventRevision counts the job events that changed a job's record.
   *
   * IT IS THE SIGNAL A PAGED LIST READS. A view that renders one page of jobs
   * cannot be patched in place by an event — the job the event is about may
   * not be on the page, and the page's ORDER and CONTENTS are the server's
   * answer, not this store's. So the list watches this number and re-asks for
   * its page instead of editing rows by hand. The detail views, which do hold
   * one whole record, keep being patched (see applyJobEvent).
   */
  get jobEventRevision() { return jobEventRevision; },
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
  // The job records carry their own audit trail, and it is the only history the
  // event log can have: the live stream only delivers what happens while the
  // page is open. Seeding here — the one place job records enter the store —
  // means every reader of `events` is populated on first paint.
  seedEventsFromJobs(jobs);
}
/**
 * _selectedJobRequest is the job the selection is currently FOR.
 *
 * Scope alone is not enough to reject a late answer. Two job detail pages in
 * one tenant share a scope, and the route effect fires a read on every
 * `#/jobs/:id`, so a slow `GetJobStatus(job-a)` can land after the operator
 * has already navigated to job-b and overwrite the selection with job A's
 * terminal reason, delivery, verification and readmit state — under job B's
 * URL, with job B's Cancel and Readmit buttons. Every read and every direct
 * selection claims this, and an answer whose claim has been superseded is
 * dropped.
 */
let _selectedJobRequest = '';

export function selectJob(j: JobStatus | null): void {
  _selectedJobRequest = j?.job_id ?? '';
  selectedJob = j;
}

/**
 * followJobRoute makes the job a `#/jobs/:id` route names the selection.
 *
 * TWO READS, and the second is the one that matters. The list record
 * (ListJobs) is the placeholder: it paints the page immediately and, on a
 * fresh load at #/jobs/:id where the poller has not answered yet, may not
 * exist at all. The AUTHORITATIVE record is GetJobStatus, because only that
 * path lays the task's provenance over the status — attempts, budget,
 * terminal reason, readmit state, delivery and review rounds.
 *
 * Call it from the route effect after reading `readyScope()`, so a scope
 * change re-runs it (see readyScope). It reads the store untracked: the
 * effect must depend on the route and the scope, never on the selection it
 * writes, or it would re-run on its own write forever.
 */
export function followJobRoute(jobID: string): void {
  if (jobID === '') return;
  untrack(() => {
    const found = orchestratorStore.jobs.find((j) => j.job_id === jobID) ?? null;
    if (found !== null) {
      selectJob(found);
    } else {
      // Nothing to paint yet; refresh the list so the row exists for the
      // back navigation, bounded by network latency not the poll cadence.
      void loadJobs();
    }
    void loadSelectedJob(jobID);
  });
}
export function setFleetNodes(a: FleetNode[] | null | undefined): void {
  fleetNodes = Array.isArray(a) ? dedupeBy(a, (x) => x.id) : [];
  _fleetFetchedAt = Date.now();
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
/**
 * Point the orchestrator views at `id` for the remainder of THIS session.
 *
 * Only reachable from the header switcher, whose options the server decides
 * (see MainApp): a principal who cannot enumerate tenants is offered only their
 * own. Passing the session's own tenant clears the override rather than pinning
 * it, so returning to your own tenant leaves nothing behind to expire.
 */
export function setActiveTenant(id: string): void {
  if (id === '' || id === authStore.tenantID) {
    tenantOverride = '';
    tenantOverrideBase = '';
  } else {
    tenantOverride = id;
    tenantOverrideBase = sessionIdentity();
  }
  // Switching tenant changes the scope every cached collection is keyed on, in
  // this store and in the sibling stores that hold their own. Announcing it on
  // the shared bus is what lets the switcher stay one line: each store releases
  // and refetches itself, and a store added later is served by registering
  // rather than by someone remembering to extend a list here.
  announceSessionScopeChange();
}
export function setJobsLoading(v: boolean): void { jobsLoading = v; }
export function setFleetNodesLoading(v: boolean): void { fleetNodesLoading = v; }
export function setSessionsLoading(v: boolean): void { sessionsLoading = v; }
export function setTasksLoading(v: boolean): void { tasksLoading = v; }

/**
 * eventKey identifies one event in the log — the same shape EventLog keys its
 * `{#each}` on. Two events sharing a key are one entry to the Svelte
 * reconciler, so every writer into `events` owes the log uniqueness under this
 * key or the render dies with each_key_duplicate; having one definition of it
 * here is what lets both writers check.
 */
function eventKey(e: JobEvent): string {
  return `${e.timestamp}:${e.job_id}:${e.type}:${e.iteration}`;
}

export function appendEvent(e: JobEvent): void {
  // An event that only announces a state carries no `type` of its own. The
  // log renders `type` as the event's label and the dedupe key reads it, so
  // the fallback is applied once here rather than at every reader — an
  // untyped event used to show as a blank chip and collide with every other
  // untyped event of the same instant.
  const event: JobEvent = e.type ? e : { ...e, type: e.state };
  const key = eventKey(event);
  // Guard against duplicate events (e.g. SSE stream reconnect replay).
  if (events.some((ev) => eventKey(ev) === key)) {
    return;
  }
  events = [...events.slice(-(MAX_EVENTS - 1)), event];
}

/**
 * auditContentKey identifies an audit line by WHAT IT SAYS rather than by when
 * it was recorded: the job it belongs to, the type it announced, and its detail.
 *
 * The two paths an audit line reaches the UI by disagree about its timestamp.
 * The persisted line carries the instant `JobRun.Audit` recorded it; the live
 * fan-out of the same line (AuditFn -> publishEvent, pkg/orchestration/
 * job_envelope.go) stamps `time.Now()` again when it pushes to subscribers. So
 * the render key cannot recognise a seeded line as the one already streamed —
 * the two differ by microseconds — and seeding would double every event the
 * page had already watched arrive. Content identity is what the two paths do
 * agree on.
 */
function auditContentKey(e: JobEvent): string {
  return `${e.job_id}\u0000${e.type}\u0000${e.message}`;
}

/**
 * seedEventsFromJobs fills the event log from the audit trail the job records
 * already carry.
 *
 * Without this the log held only what the SSE stream delivered while the page
 * happened to be open, so a dashboard opened after the work ran said "No events
 * yet" about a job with a full history behind it (go-quicbaas#850).
 *
 * Seeded lines go BEFORE the live ones and the overflow is taken off the front,
 * so a job with a long trail can never evict an event that arrived live: the
 * seed is history, the stream is the present, and when only one of them fits it
 * is the present that must survive.
 *
 * Private on purpose: setJobs is the one door job records come through, so
 * there is nowhere else a seed could be needed, and no way to seed the log from
 * records the store is not holding.
 */
function seedEventsFromJobs(list: readonly JobStatus[]): void {
  const seeded: JobEvent[] = [];
  for (const job of list) {
    if (!job.job_id || !Array.isArray(job.events)) continue;
    for (const line of job.events) {
      if (typeof line?.type !== 'string' || line.type === '') continue;
      if (typeof line.timestamp !== 'number' || !Number.isFinite(line.timestamp)) continue;
      seeded.push({
        job_id: job.job_id,
        type: line.type,
        // An audit line announces a moment, not a state. The log renders the
        // state badge only when there is one, so the empty state is the honest
        // value rather than a guess at the job's state when the line was cut.
        state: '' as JobState,
        message: line.details ?? '',
        iteration: 0,
        timestamp: line.timestamp,
      });
    }
  }
  if (seeded.length === 0) return;

  // Two guards, because they catch different things: content identity rejects a
  // line already delivered by the stream (and a re-seed on the next poll), and
  // the render key rejects a pair that would collide in the reconciler.
  const byContent = new Set(events.map(auditContentKey));
  const byRenderKey = new Set(events.map(eventKey));
  const fresh: JobEvent[] = [];
  for (const event of seeded) {
    const content = auditContentKey(event);
    const render = eventKey(event);
    if (byContent.has(content) || byRenderKey.has(render)) continue;
    byContent.add(content);
    byRenderKey.add(render);
    fresh.push(event);
  }
  if (fresh.length === 0) return;

  fresh.sort((a, b) => a.timestamp - b.timestamp);
  events = [...fresh, ...events].slice(-MAX_EVENTS);
}

/**
 * jobEventPatch is what an event says about the job's own record: its state,
 * how far it has iterated, and the lifecycle moment it is in (carried in the
 * event metadata, since a stage is not a state). Returns null when the event
 * says nothing about any of the three, so a caller can skip the write.
 */
function jobEventPatch(e: JobEvent): Partial<JobStatus> | null {
  const patch: Partial<JobStatus> = {};
  // The wire is untyped, and an event that only announces a stage carries no
  // state at all. `JobState` has no empty member, so the emptiness check is
  // made on the string before it is read back as a state.
  const state: string = e.state ?? '';
  if (state !== '') patch.state = state as JobState;
  if (typeof e.iteration === 'number' && Number.isFinite(e.iteration)) patch.iteration = e.iteration;
  const stage = e.metadata?.['stage'];
  if (typeof stage === 'string' && stage !== '') patch.stage = stage;
  return Object.keys(patch).length === 0 ? null : patch;
}

/**
 * applyJobEvent patches the job the event belongs to so the views move with
 * the stream instead of waiting up to 30 s for the next poll. It updates the
 * row in the list AND the selected job, which are distinct objects once a
 * poll has replaced the list under an open detail view.
 *
 * It never creates a job: an event for a job this tenant's list does not hold
 * is recorded in the event log and changes nothing else.
 */
export function applyJobEvent(e: JobEvent): void {
  if (typeof e?.job_id !== 'string' || e.job_id === '') return;
  const patch = jobEventPatch(e);
  if (patch === null) return;

  // Announced BEFORE the patch is applied and regardless of whether this
  // store holds the job: a paged list reads this to re-ask for its page, and
  // the job that just changed state may well be one this store never saw.
  jobEventRevision += 1;

  const idx = jobs.findIndex((j) => j.job_id === e.job_id);
  if (idx !== -1) {
    const patched = { ...jobs[idx], ...patch } as JobStatus;
    jobs = [...jobs.slice(0, idx), patched, ...jobs.slice(idx + 1)];
  }
  if (selectedJob !== null && selectedJob.job_id === e.job_id) {
    // THE PATCH IS STATE, AND THE OUTCOME IS NOT STATE. jobEventPatch carries
    // the state, the iteration and the stage; the delivery, the review
    // rounds, the typed terminal reason, the readmit permission, the attempt
    // history and the verification exist only on GetJobStatus (see
    // loadSelectedJob). So an operator WATCHING an sdlc job finish saw it go
    // `failed` with every one of those panels still empty, and the stream was
    // released at the same moment — nothing would have filled them in until
    // the page was navigated again. The transition into a terminal state is
    // exactly when the provenance becomes final, so it is re-read there.
    const wasRunning = !isTerminalJobState(selectedJob.state);
    selectedJob = { ...selectedJob, ...patch };
    if (wasRunning && isTerminalJobState(selectedJob.state)) {
      void loadSelectedJob(selectedJob.job_id);
    }
  }
}

/**
 * isTerminalJobState says the job has stopped for good: nothing more will be
 * emitted for it, so pollers, filters and event streams can let it go.
 */
export function isTerminalJobState(state: JobState): boolean {
  return state === 'completed' || state === 'failed';
}

export function clearEvents(): void { events = []; }

// ---------------------------------------------------------------------------
// Fetch helpers (internal — each updates its timestamp on success)
// ---------------------------------------------------------------------------

async function _fetchJobs(): Promise<void> {
  // Pin BOTH the tenant asked about and the scope it is asked for, before any
  // await. Reading the tenant after the await would send the request for
  // whoever is signed in by then while the guard still judged it against the
  // scope at entry — the URL and the verdict have to describe the same request.
  const scope = currentScope();
  const tenantID = currentTenantID();
  jobsLoading = true;
  try {
    const client = await getSDKClient();
    const result = await client.orchestrator.listJobs(tenantID);
    if (scope !== currentScope()) return;
    setJobs(result);
  } catch {
    // Silently ignore transient errors; the next poll will retry.
  } finally {
    jobsLoading = false;
  }
}

async function _fetchFleet(): Promise<void> {
  const scope = currentScope();
  const tenantID = currentTenantID();
  fleetNodesLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listFleetNodes({ tenant_id: tenantID });
    if (scope !== currentScope()) return;
    setFleetNodes(resp.nodes ?? []);
  } catch {
    // Silently ignore.
  } finally {
    fleetNodesLoading = false;
  }
}

async function _fetchSessions(): Promise<void> {
  const scope = currentScope();
  const tenantID = currentTenantID();
  sessionsLoading = true;
  try {
    const client = await getSDKClient();
    const resp = await client.orchestrator.listSessions({ tenantID });
    if (scope !== currentScope()) return;
    setSessions((resp.sessions ?? []).map(toSessionInfo));
  } catch {
    // Silently ignore.
  } finally {
    sessionsLoading = false;
  }
}

async function _fetchTasks(): Promise<void> {
  const scope = currentScope();
  const tenantID = currentTenantID();
  tasksLoading = true;
  try {
    const client = await getSDKClient();
    const result = await client.orchestrator.listTasks(tenantID);
    if (scope !== currentScope()) return;
    setTasks(result);
    tasksError = null;
  } catch (err: unknown) {
    if (scope === currentScope()) tasksError = err;
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
  _enforceScope();
  if (Date.now() - _jobsFetchedAt < STALE_AFTER_MS) return;
  await _fetchJobs();
}

/**
 * loadSelectedJob reads ONE job through `GetJobStatus` and makes it the
 * selection.
 *
 * THE LIST IS NOT ENOUGH, and this is not an optimisation. `ListJobs` returns
 * the raw persisted statuses (pkg/orchestration/orchestrator.go); only
 * `GetJobStatus` lays the task's provenance over them — the attempt history,
 * the attempt budget, the typed terminal reason, whether the task may be
 * readmitted, and the delivery with its pull request and review rounds. A
 * detail page populated from the list therefore renders every one of those
 * panels empty, which is the whole feature missing (go-quicbaas#796).
 *
 * The list record is still used as an immediate placeholder by the caller, so
 * the page paints at once and fills in when this returns.
 *
 * A read that fails leaves whatever is already selected in place: the list
 * record without provenance is worse than the enriched one and better than a
 * blank page.
 */
export async function loadSelectedJob(jobID: string): Promise<void> {
  if (jobID === '') return;
  const scope = currentScope();
  const tenantID = currentTenantID();
  _selectedJobRequest = jobID;
  try {
    const client = await getSDKClient();
    const job = await client.orchestrator.getJobStatus(tenantID, jobID);
    // The operator may have switched tenant, or moved to ANOTHER JOB, during
    // the round trip. A late answer must not select a job for a scope that is
    // no longer the one asking, nor for a page the operator has already left:
    // both would put one job's record under another job's URL and controls.
    if (scope !== currentScope() || _selectedJobRequest !== jobID) return;
    if (job !== null && job !== undefined && job.job_id === jobID) {
      // The SDK types the optional members as `T | null`; the store's own
      // copy types them as `T | undefined`. Normalising here is narrower —
      // and more honest — than casting the whole record through `unknown`.
      selectedJob = {
        ...job,
        config: job.config ?? undefined,
        memory: job.memory ?? undefined,
      };
    }
  } catch {
    // Leave the placeholder selection alone; the next route visit retries.
  }
}

/** loadFleet fetches the fleet only when cached data is stale. */
export async function loadFleet(): Promise<void> {
  _enforceScope();
  if (Date.now() - _fleetFetchedAt < STALE_AFTER_MS) return;
  await _fetchFleet();
}

/** loadSessions fetches sessions only when cached data is stale. */
export async function loadSessions(): Promise<void> {
  _enforceScope();
  if (Date.now() - _sessionsFetchedAt < STALE_AFTER_MS) return;
  await _fetchSessions();
}

/** loadTasks fetches tasks only when cached data is stale. */
export async function loadTasks(): Promise<void> {
  _enforceScope();
  if (Date.now() - _tasksFetchedAt < STALE_AFTER_MS) return;
  await _fetchTasks();
}

/**
 * loadTaskJobs reads every job that ran one task — the task's attempt
 * history — and makes it the task-scoped selection.
 *
 * UNCONDITIONAL, no staleness window: the caller is a task page that has just
 * been opened or reloaded, and each call is for a different task. A window
 * keyed on time rather than on the task asked for would answer the second
 * task with the first one's attempts.
 *
 * A read that fails leaves the previous answer in place and records the task
 * anyway, so the page reports "no records could be read" rather than
 * asserting the task never dispatched.
 */
export async function loadTaskJobs(taskID: string): Promise<void> {
  if (taskID === '') return;
  _enforceScope();
  const scope = currentScope();
  const tenantID = currentTenantID();
  taskJobsLoading = true;
  try {
    const client = await getSDKClient();
    const result = await client.orchestrator.listJobs(tenantID, taskID);
    // The operator may have navigated to another task during the round trip;
    // a late answer must not become the attempt history of the task now open.
    if (scope !== currentScope()) return;
    taskJobs = Array.isArray(result) ? dedupeBy(result, (x) => x.job_id) : [];
    taskJobsTaskID = taskID;
  } catch {
    taskJobs = [];
    taskJobsTaskID = taskID;
  } finally {
    taskJobsLoading = false;
  }
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
  _enforceScope();
  switch (resource) {
    case 'jobs':     return _fetchJobs();
    case 'fleet':    return _fetchFleet();
    case 'sessions': return _fetchSessions();
    case 'tasks':    return _fetchTasks();
    default:
      await Promise.allSettled([_fetchJobs(), _fetchFleet(), _fetchSessions(), _fetchTasks()]);
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
  void Promise.allSettled([loadJobs(), loadFleet(), loadSessions(), loadTasks()]);
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

/**
 * resetOrchestratorStore stops polling and clears all cached data and the
 * stale-while-revalidate timestamps. Call it on logout / tenant switch so the
 * next session does not see the previous user's cached jobs, agents, sessions
 * or tasks and so the next poll tick performs a real fetch rather than serving
 * stale data. (Tests also use it to isolate the module-level cache between
 * cases — without the timestamp reset the second mount considers data fresh and
 * skips the fetch.)
 */
export function resetOrchestratorStore(): void {
  stopOrchestratorPolling();
  tenantOverride = '';
  tenantOverrideBase = '';
  // Back to "nothing here belongs to anyone", the startup state. Leaving the
  // old scope behind would let the next session's first read match a cache that
  // has just been emptied, which is harmless today but only by luck.
  _dataScope = '';
  jobs = [];
  selectedJob = null;
  taskJobs = [];
  taskJobsTaskID = '';
  fleetNodes = [];
  sessions = [];
  tasks = [];
  tasksError = null;
  backends = [];
  health = null;
  events = [];
  _jobsFetchedAt = 0;
  _fleetFetchedAt = 0;
  _sessionsFetchedAt = 0;
  _tasksFetchedAt = 0;
  _healthFetchedAt = 0;
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
  _fleetFetchedAt = 0;
  _sessionsFetchedAt = 0;
  _tasksFetchedAt = 0;
  _healthFetchedAt = 0;
}
