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
 * Pure helpers for rendering the generic Task model (attempt accounting,
 * provenance, and the parked-on-a-person state) in the Tasks views.
 *
 * Every function takes the narrowest structural type it needs so the store's
 * Task and the SDK's Task both satisfy it, and so tests can pass literals.
 */

import type { Artifact, TaskSource } from '@quicbaas/sdk';

/**
 * The attempt-accounting fields of a Task. `charged_attempts` and
 * `effective_budget` are derived by the server from the others
 * (go-quicbaas#947); they are preferred here so the page never re-derives the
 * engine's refund rules and disagrees with it.
 */
export interface TaskAttemptFields {
  attempts?: number;
  attempt_budget?: number;
  environment_faults?: number;
  question_attempts?: number;
  charged_attempts?: number;
  effective_budget?: number;
}

/** The fields that say whether, and why, a Task is waiting on a person. */
export interface TaskBlockFields {
  status: string;
  blocked_reason?: string;
}

/** A resolved link to the forge issue a Task was adopted from. */
export interface IssueRef {
  /** The issue number as written on the forge, without the `#`. */
  number: string;
  /** `#N`, the label the views render. */
  label: string;
  /** The issue's URL when the source carries one; '' otherwise. */
  href: string;
}

/**
 * issueRef returns the forge issue a task was adopted from, or null for any
 * other provenance. The source `ref` is either the bare number ("42") or
 * `owner/repo#42`; the label is always `#42`. The href is the URL the source
 * records — the browser has no forge base of its own to build one from.
 */
export function issueRef(task: { source?: TaskSource | null }): IssueRef | null {
  const src = task.source;
  if (!src || src.kind !== 'forge_issue') return null;
  const ref = (src.ref ?? '').trim();
  if (ref === '') return null;
  const hashAt = ref.lastIndexOf('#');
  const number = hashAt === -1 ? ref : ref.slice(hashAt + 1);
  if (number === '') return null;
  return { number, label: `#${number}`, href: (src.url ?? '').trim() };
}

/** dispatchedAttempts is every job ever started for the task, refunded or not. */
export function dispatchedAttempts(task: TaskAttemptFields): number {
  return Math.max(0, task.attempts ?? 0);
}

/**
 * chargedAttempts is what counts against the budget: dispatched attempts less
 * the ones refunded because the environment failed or the worker stopped to
 * ask a person. The server's own figure wins when it sent one.
 */
export function chargedAttempts(task: TaskAttemptFields): number {
  if (task.charged_attempts !== undefined) return Math.max(0, task.charged_attempts);
  const refunded = Math.max(0, task.environment_faults ?? 0) + Math.max(0, task.question_attempts ?? 0);
  return Math.max(0, dispatchedAttempts(task) - refunded);
}

/**
 * attemptBudget is the budget the task runs under: the server's effective
 * budget (its own, or the engine default) when sent, else the declared one.
 * 0 means nothing says.
 */
export function attemptBudget(task: TaskAttemptFields): number {
  return Math.max(0, task.effective_budget ?? task.attempt_budget ?? 0);
}

/**
 * budgetLabel renders attempt accounting as `charged / budget`, adding
 * `(N dispatched)` only when refunds make the two differ — "2 / 5 (3
 * dispatched)" says three jobs ran and one was refunded. A task with no
 * accounting at all renders as an em-dash rather than an invented "0 / 0".
 */
export function budgetLabel(task: TaskAttemptFields): string {
  if (task.attempts === undefined && task.attempt_budget === undefined && task.effective_budget === undefined) {
    return '—';
  }
  const charged = chargedAttempts(task);
  const dispatched = dispatchedAttempts(task);
  const budget = attemptBudget(task);
  const base = budget > 0 ? `${charged} / ${budget}` : `${charged}`;
  return dispatched === charged ? base : `${base} (${dispatched} dispatched)`;
}

/** blockedReason is the recorded reason, trimmed; '' when nothing was recorded. */
export function blockedReason(task: { blocked_reason?: string }): string {
  return (task.blocked_reason ?? '').trim();
}

/**
 * isWaitingOnPerson is true when no job will move the task on its own: it is
 * parked (`blocked`) or it ended in `failed`. Both need a person to act.
 */
export function isWaitingOnPerson(task: TaskBlockFields): boolean {
  return task.status === 'blocked' || task.status === 'failed';
}

// =============================================================================
// Why a task stopped
// =============================================================================
//
// `failed, attempts: 3` SAID NOTHING ABOUT WHOSE FAILURE IT WAS. Nine tasks a
// deploy killed and two that genuinely failed read identically, and a task
// parked on a question for the owner read the same as one waiting for an
// operator to mount a workspace (go-quicbaas#947). The server classifies the
// stop — `fault_origin` from its own fault-class tables, `block_kind` from the
// park — and this reads those fields. It never classifies `result.output`:
// that prose is the detail behind the typed reason, not the reason.

/** The fields a stop is classified from. */
export interface TaskStopFields {
  status: string;
  block_kind?: string;
  fault_class?: string;
  fault_origin?: string;
  conclusion_reason?: string;
}

/**
 * The distinct ways a task can be stopped, as a reader must tell them apart.
 * `none` is a task that is not stopped on anything: open, or completed.
 */
export type TaskStopKind =
  /** The environment failed — the fleet, an account pool, a forge. An operator repairs it. */
  | 'environment'
  /** The task failed on its own merits. */
  | 'work'
  /** Its node stopped and nothing diagnosed why; a person readmits it. */
  | 'undiagnosed'
  /** Parked on a question or a decision only the owner may make. */
  | 'owner'
  /** Parked on a condition or an operator's hold no fault classified. */
  | 'held'
  /** Escalated to a person for a reason no fault classified. */
  | 'escalated'
  | 'none';

/** taskStopKind classifies why a task is stopped. */
export function taskStopKind(task: TaskStopFields): TaskStopKind {
  switch (task.fault_origin) {
    case 'environment':
      return 'environment';
    case 'work':
      return 'work';
    case 'undiagnosed':
      return 'undiagnosed';
  }
  if (task.status !== 'blocked') return 'none';
  switch (task.block_kind) {
    case 'needs_info':
      return 'owner';
    case 'escalated':
      return 'escalated';
    default:
      return 'held';
  }
}

/** How one stop kind is presented: a short label, a badge tone, and a sentence. */
export interface TaskStopPresentation {
  label: string;
  variant: 'error' | 'warning' | 'info' | 'neutral';
  detail: string;
}

/** The presentation of every stop kind, keyed for O(1) lookup. */
const TASK_STOP_PRESENTATION: Record<Exclude<TaskStopKind, 'none'>, TaskStopPresentation> = {
  environment: {
    label: 'Environment fault',
    variant: 'warning',
    detail: 'The environment failed, not the work; awaiting operator repair. Refunded attempts are not charged.',
  },
  work: {
    label: 'Failed on the work',
    variant: 'error',
    detail: 'Every charged attempt failed on the work itself.',
  },
  undiagnosed: {
    label: 'Node lost',
    variant: 'neutral',
    detail: 'The node running the attempt stopped and nothing diagnosed why; readmit once the cause is known.',
  },
  owner: {
    label: 'Awaiting owner ruling',
    variant: 'info',
    detail: 'The worker stopped on a question or a decision only the owner may make.',
  },
  held: {
    label: 'Held',
    variant: 'neutral',
    detail: 'Parked on a condition or an operator’s hold.',
  },
  escalated: {
    label: 'Escalated',
    variant: 'warning',
    detail: 'Escalated to a person.',
  },
};

/**
 * taskStopPresentation is how a stopped task's reason is shown, or null for a
 * task that is not stopped on anything. An environment fault names its class,
 * so the badge reads e.g. "Environment fault: forge_unreachable".
 */
export function taskStopPresentation(task: TaskStopFields): TaskStopPresentation | null {
  const kind = taskStopKind(task);
  if (kind === 'none') return null;
  const base = TASK_STOP_PRESENTATION[kind];
  const cls = (task.fault_class ?? '').trim();
  if (kind === 'environment' && cls !== '') return { ...base, label: `${base.label}: ${cls}` };
  return base;
}

/** The text shown when a task is waiting on a person and nothing says why. */
export const NO_REASON_RECORDED = 'No reason recorded.';

/**
 * The artifact NAME under which a delivered pull request is recorded.
 *
 * The name, not the kind: the runner publishes it as
 * `{kind: 'url', name: 'pull_request'}` — see pkg/orchestration's
 * ArtifactNamePullRequest — and the engine's own validator refuses any kind
 * outside inline/file/commit/kv_key/url, so an artifact whose KIND was
 * `pull_request` could never have existed. Filtering on the kind matched
 * nothing, which is why a merged run's page linked no pull request
 * (go-quicbaas#796).
 */
export const PULL_REQUEST_ARTIFACT_NAME = 'pull_request';

/** The artifact name under which the pushed head is recorded. */
export const HEAD_ARTIFACT_NAME = 'head';

/**
 * ARTIFACT_ATTEMPT_SUFFIX separates a delivery artifact's base name from the
 * attempt that published it: `pull_request@a3`. It mirrors
 * pkg/orchestration's ArtifactAttemptSuffix, and it exists because the task
 * store overwrites artifacts BY NAME while a retried task keeps one record —
 * so without the scope, attempt 3's pull request replaced attempt 1's.
 */
export const ARTIFACT_ATTEMPT_SUFFIX = '@a';

/** artifactBaseName strips the attempt scope from a published artifact name. */
export function artifactBaseName(name: string): string {
  const at = name.lastIndexOf(ARTIFACT_ATTEMPT_SUFFIX);
  return at > 0 ? name.slice(0, at) : name;
}

/**
 * pullRequestArtifacts returns EVERY attempt's delivered pull request, in the
 * order the task recorded them.
 *
 * The task page is per-task, so it shows them all — its heading already says
 * "Pull requests (N)". A per-attempt view reads the job's own `delivery`
 * instead, which the server scopes for it.
 */
export function pullRequestArtifacts(task: { output_artifacts?: Artifact[] | null }): Artifact[] {
  return (task.output_artifacts ?? []).filter(
    (a) => artifactBaseName(a.name) === PULL_REQUEST_ARTIFACT_NAME,
  );
}

/**
 * artifactUrl is the link an artifact carries: the `url` entry of its
 * metadata, or a ref path that is itself a URL. '' when it has neither.
 */
export function artifactUrl(artifact: Artifact): string {
  const fromMetadata = (artifact.metadata?.['url'] ?? '').trim();
  if (fromMetadata !== '') return fromMetadata;
  const path = (artifact.ref?.path ?? '').trim();
  return /^https?:\/\//.test(path) ? path : '';
}

/**
 * httpStatus reads the status a failed SDK call carried, or null when it
 * carried none. Exported so every reader maps a refusal the same way — see
 * lib/cost.ts, which adds the one status this file has no opinion about.
 */
export function httpStatus(err: unknown): number | null {
  if (typeof err !== 'object' || err === null) return null;
  const status = (err as { status?: unknown }).status;
  return typeof status === 'number' ? status : null;
}

/** errorText renders an unknown thrown value as a sentence. */
export function errorText(err: unknown): string {
  if (err instanceof Error) return err.message;
  return String(err);
}

/**
 * loadErrorMessage turns a failed read into a sentence that says what the
 * reader can do about it. A 403 names the permission the account lacks, a
 * 404 says the subject is gone, a 401 says the session is; anything else
 * carries the server's own message.
 */
export function loadErrorMessage(err: unknown, subject: string, permission: string): string {
  switch (httpStatus(err)) {
    case 401:
      return `Your session is no longer valid. Sign in again to view ${subject}.`;
    case 403:
      return `You do not have permission to view ${subject}: this account lacks ${permission}.`;
    case 404:
      return `${subject} not found on the server.`;
    default:
      return `Could not load ${subject}: ${errorText(err)}`;
  }
}

// =============================================================================
// What an empty panel is allowed to say
// =============================================================================
//
// AN EMPTY PANEL IS A CLAIM, AND IT HAS TO BE TRUE. The task page used to
// render one sentence for every empty Jobs panel — "Dispatch requested; no
// agent has matched this task yet." — which is a statement about a task that
// has never dispatched. go-quicbaas#822 measured it on a task that had
// dispatched three times, run three jobs and ended `failed`: every word of it
// was false, and an operator reading it would conclude the scheduler had
// never picked the work up.
//
// The facts a reader needs separated are:
//
//   - NOTHING HAS RUN. No attempt was ever charged. Nothing to show, and
//     saying so is honest.
//   - SOMETHING RAN AND CANNOT BE SHOWN. Attempts were charged but no job
//     record answered for them. That is a gap in what the page can read, NOT
//     a statement about the work, and it must never be phrased as one.
//   - IT IS STILL LOADING. Not yet an answer at all.
//
// Same for the pull-request panel: a task that failed before it ever reached
// publish and a task that finished and published nothing are different
// outcomes, and "No pull request recorded for this task" flattens them.

// =============================================================================
// Which job ran the task
// =============================================================================
//
// `spawned_job_id` IS THE OPEN ATTEMPT, NOT THE JOB THAT RAN. It is the one
// field on the task that says "this attempt is still mine", and
// recordAttemptHistory (pkg/orchestration/task_attempt.go) moves it into
// metadata.attempt_history and CLEARS it the moment an attempt concludes —
// whether the attempt succeeded, failed, or was refunded.
//
// So every settled task has an empty `spawned_job_id`, and the Job column,
// reading only that, fell through to `auto_spawn` and printed the italic
// "no agent matched" for tasks that had demonstrably run jobs to completion.
// An operator could not get from a finished task to the job that finished it,
// which is the first hop of the only path that answers "what actually
// happened here" (go-quicbaas#856).
//
// The history is the record of the CONCLUDED attempts, oldest first; the open
// attempt, when there is one, comes after them. That is the same ordering
// JobAttemptsOfTask builds server-side (pkg/orchestration/job_provenance.go),
// and it is built here rather than fetched because the task record already
// carries it — the Job column must not need a request per row.

/**
 * The task metadata key holding every job concluded against the task, oldest
 * first, comma-separated. Mirrors `attemptHistoryKey` in
 * pkg/orchestration/task_attempt.go.
 */
export const TASK_ATTEMPT_HISTORY_KEY = 'attempt_history';

/** The fields that name the jobs a task has run. */
export interface TaskJobFields {
  /** The OPEN attempt, empty once every attempt has concluded. */
  spawned_job_id?: string;
  /** Carries `attempt_history`: the concluded attempts, comma-separated. */
  metadata?: Record<string, string> | null;
}

/**
 * taskAttemptJobIDs is every job the task has run, oldest first: the
 * concluded attempts from `metadata.attempt_history`, then the attempt still
 * open. Empty for a task that has never dispatched.
 */
export function taskAttemptJobIDs(task: TaskJobFields): string[] {
  const history = (task.metadata?.[TASK_ATTEMPT_HISTORY_KEY] ?? '')
    .split(',')
    .map((id) => id.trim())
    .filter((id) => id !== '');
  const open = (task.spawned_job_id ?? '').trim();
  // A job id appearing in both — a record read mid-conclusion — is one
  // attempt, not two.
  if (open !== '' && !history.includes(open)) history.push(open);
  return history;
}

/**
 * latestTaskJobID is the job of the task's most recent attempt: the open one
 * when an attempt is open, otherwise the newest concluded. '' when the task
 * has never dispatched.
 */
export function latestTaskJobID(task: TaskJobFields): string {
  const ids = taskAttemptJobIDs(task);
  return ids.length === 0 ? '' : ids[ids.length - 1] ?? '';
}

/**
 * taskHasDispatched is true when a job has ever been started for the task —
 * by its own history, by an open attempt, or by the attempt accounting. Only
 * a task for which all three say nothing has genuinely never dispatched, and
 * only such a task may be described as having matched no agent.
 */
export function taskHasDispatched(task: TaskJobFields & TaskAttemptFields): boolean {
  return taskAttemptJobIDs(task).length > 0 || dispatchedAttempts(task) > 0;
}

// =============================================================================
// Which agent ran the task
// =============================================================================
//
// `assigned_to` IS THE CLAIMANT, AND AN AUTO-SPAWNED TASK HAS NONE. The field
// is written by ClaimTask (pkg/orchestration/task_store.go) and cleared again
// by requeue and block (pkg/orchestration/task_attempt.go). The dispatcher's
// own path — ClaimTaskSpawn, which is how every SDLC task is picked up —
// never writes it at all.
//
// So for the whole life of an auto-spawned task the field is legitimately
// empty, and the Assigned column rendered an em-dash that reads as "nobody
// ran this" for work that ran on a named fleet node. The node is recorded
// on the JOB, as `agent_id` (pkg/orchestration/orchestrator_types.go), which
// is where this looks when the task names no claimant.

/** The job fields that name the agent an attempt ran on. */
export interface TaskAgentJob {
  job_id: string;
  agent_id?: string;
}

/**
 * taskAgentID is the agent the task is being worked by: its claimant when it
 * has one, otherwise the fleet node its most recent attempt ran on. '' when
 * neither is known — which is an honest "not recorded", not "nobody".
 *
 * The attempt's agent comes from the task's own `agent_id` when the server
 * resolved it — every task listing does, from the latest attempt's job — and
 * otherwise from `jobs`, whatever job records the caller already holds. Only
 * the latest attempt's record is consulted, so an older attempt's agent is
 * never presented as the current one.
 *
 * READING THE LISTING'S OWN FIELD IS WHAT SPARES A PAGE ITS SECOND REQUEST.
 * The Tasks page used to follow every page of tasks with a job listing for
 * their attempts, only to re-derive the agent the task rows already carried.
 */
export function taskAgentID(
  task: { assigned_to?: string; agent_id?: string } & TaskJobFields,
  jobs: readonly TaskAgentJob[] = [],
): string {
  const claimant = (task.assigned_to ?? '').trim();
  if (claimant !== '') return claimant;
  const resolved = (task.agent_id ?? '').trim();
  if (resolved !== '') return resolved;
  const latest = latestTaskJobID(task);
  if (latest === '') return '';
  for (const job of jobs) {
    if (job.job_id === latest) return (job.agent_id ?? '').trim();
  }
  return '';
}

/** The fields the empty-state wording is derived from. */
export interface TaskDispatchFields extends TaskAttemptFields, TaskJobFields {
  status: string;
  auto_spawn?: boolean;
}

/** The statuses in which no further job will run without a person acting. */
const SETTLED_TASK_STATUSES = new Set(['completed', 'failed', 'blocked', 'cancelled']);

/** taskHasSettled is true when the task will not advance on its own. */
export function taskHasSettled(task: { status: string }): boolean {
  return SETTLED_TASK_STATUSES.has(task.status);
}

/**
 * What the Jobs panel is reporting when it has no rows to draw. Each value is
 * a different fact, and they are not interchangeable.
 */
export type TaskJobsEmptyState =
  /** The listing has not answered yet. */
  | 'loading'
  /** No attempt has ever been charged, and the task is waiting for one. */
  | 'awaiting-dispatch'
  /** No attempt has ever been charged, and none ever will be. */
  | 'never-dispatched'
  /** Attempts ran; their job records could not be read. */
  | 'records-unavailable';

/**
 * taskJobsEmptyState classifies an empty Jobs panel. `jobCount` is the number
 * of the task's jobs the page actually holds; callers pass 0 to ask what the
 * emptiness means.
 */
export function taskJobsEmptyState(
  task: TaskDispatchFields,
  jobCount: number,
  loading: boolean,
): TaskJobsEmptyState {
  if (jobCount > 0) return 'loading'; // not an empty panel; callers do not render this
  if (loading) return 'loading';
  // An attempt was charged, or the task names a job — open or concluded:
  // something ran, and the page cannot show it. That is a read gap, not a
  // dispatch outcome.
  if (taskHasDispatched(task)) {
    return 'records-unavailable';
  }
  return taskHasSettled(task) ? 'never-dispatched' : 'awaiting-dispatch';
}

/** The sentence each empty Jobs state renders, keyed for O(1) lookup. */
const TASK_JOBS_EMPTY_TEXT: Record<TaskJobsEmptyState, (task: TaskDispatchFields) => string> = {
  'loading': () => 'Loading this task’s jobs…',
  'awaiting-dispatch': (task) =>
    task.auto_spawn === true
      ? 'Dispatch requested; no agent has matched this task yet.'
      : 'No job has been dispatched for this task.',
  'never-dispatched': (task) =>
    `This task ended ${task.status} without a job ever being dispatched.`,
  'records-unavailable': (task) => {
    const attempts = dispatchedAttempts(task);
    const ran = attempts === 1 ? '1 attempt ran' : `${attempts} attempts ran`;
    return `${ran} for this task, but no job record for them could be read. The attempts are counted above; their jobs are not available.`;
  },
};

/** taskJobsEmptyMessage is the sentence an empty Jobs panel renders. */
export function taskJobsEmptyMessage(
  task: TaskDispatchFields,
  jobCount: number,
  loading: boolean,
): string {
  return TASK_JOBS_EMPTY_TEXT[taskJobsEmptyState(task, jobCount, loading)](task);
}

/**
 * What the Pull requests panel is reporting when it has nothing to list.
 */
export type TaskPullRequestEmptyState =
  /** No attempt has run, so nothing could have been published. */
  | 'nothing-ran'
  /** The task settled and published nothing. */
  | 'settled-without-publishing'
  /** Attempts are still in flight. */
  | 'not-yet-published';

/** taskPullRequestEmptyState classifies an empty Pull requests panel. */
export function taskPullRequestEmptyState(
  task: TaskDispatchFields,
  pullRequestCount: number,
): TaskPullRequestEmptyState {
  if (pullRequestCount > 0) return 'not-yet-published'; // not empty; callers do not render this
  if (dispatchedAttempts(task) === 0) return 'nothing-ran';
  return taskHasSettled(task) ? 'settled-without-publishing' : 'not-yet-published';
}

/** The sentence each empty Pull requests state renders. */
const TASK_PR_EMPTY_TEXT: Record<TaskPullRequestEmptyState, (task: TaskDispatchFields) => string> = {
  'nothing-ran': () => 'No attempt has run, so nothing has been published.',
  'settled-without-publishing': (task) => {
    const attempts = dispatchedAttempts(task);
    const ran = attempts === 1 ? '1 attempt ran' : `${attempts} attempts ran`;
    return task.status === 'completed'
      ? `This task completed without publishing a pull request (${ran}).`
      : `This task ended ${task.status} before publishing a pull request (${ran}).`;
  },
  'not-yet-published': () => 'No pull request has been published yet.',
};

/** taskPullRequestEmptyMessage is the sentence an empty PR panel renders. */
export function taskPullRequestEmptyMessage(
  task: TaskDispatchFields,
  pullRequestCount: number,
): string {
  return TASK_PR_EMPTY_TEXT[taskPullRequestEmptyState(task, pullRequestCount)](task);
}
