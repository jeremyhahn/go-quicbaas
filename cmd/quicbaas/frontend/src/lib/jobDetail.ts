// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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
 * The job detail page's reading of a job record.
 *
 * A JOB PAGE IS NOT ONE PAGE. The workflow runner decomposes a description
 * into a DAG and iterates over it, so its page is iterations, tasks and a
 * graph. The sdlc runner makes one attempt at one issue in one container, so
 * its page is the attempt, the agent, the stage timeline, the pull request and
 * the reason it stopped. Rendering the first for both is what go-quicbaas#796
 * reported: an sdlc job showing `Agent —`, `Workflow —`, `0/10 iterations` and
 * `Tasks 0%`, none of which it has or could have.
 *
 * Everything here is a PURE FUNCTION of the record so the panel decisions are
 * testable without mounting anything, and so the component stays a rendering
 * of them rather than a second copy.
 */

import type {
  JobAttemptRecord,
  JobDelivery,
  JobReviewRound,
  JobStageRecord,
  JobVerificationResult,
  TaskConclusionReason,
} from '@quicbaas/sdk';

export type {
  JobAttemptRecord,
  JobDelivery,
  JobReviewRound,
  JobStageRecord,
  JobVerificationResult,
  TaskConclusionReason,
};

/**
 * JobProvenance is the narrowest shape these helpers read.
 *
 * Structural, not the SDK's `JobStatus`, for the reason tasks.ts gives: the
 * orchestrator store keeps its own slightly-looser copy of the job record,
 * and both it and the SDK type have to satisfy these functions — as does a
 * plain literal in a test.
 */
export interface JobProvenance {
  runner?: string;
  /** state is the job's current state; a terminal one closes the last span. */
  state?: string;
  attempt?: number;
  attempts?: JobAttemptRecord[];
  attempt_budget?: number;
  stages?: JobStageRecord[];
  updated_at: number;
  terminal_reason?: TaskConclusionReason | string;
  readmit_allowed?: boolean;
  delivery?: JobDelivery | null;
  verification?: JobVerificationResult[];
}

/** JobAuditTrail is the events half of a job record. */
export interface JobAuditTrail {
  events?: { type: string; details?: string }[] | null;
}

/** WORKFLOW_RUNNER is the engine's own runner: a lead agent driving a DAG. */
export const WORKFLOW_RUNNER = 'workflow';

/** SDLC_RUNNER is the software-factory runner: one attempt at one issue. */
export const SDLC_RUNNER = 'sdlc';

/**
 * runnerOf is the runner that drove a job. A record written before runners
 * existed names none and is a workflow job, which is what the server's own
 * JobStatus.Runner comment says.
 */
export function runnerOf(job: Pick<JobProvenance, 'runner'>): string {
  const runner = (job.runner ?? '').trim();
  return runner === '' ? WORKFLOW_RUNNER : runner;
}

/** isWorkflowJob reports whether the iteration/DAG panels apply. */
export function isWorkflowJob(job: Pick<JobProvenance, 'runner'>): boolean {
  return runnerOf(job) === WORKFLOW_RUNNER;
}

// ---------------------------------------------------------------------------
// Attempts
// ---------------------------------------------------------------------------

/** currentAttempt is the attempt the record belongs to, null when unknown. */
export function currentAttempt(job: JobProvenance): JobAttemptRecord | null {
  const attempts = job.attempts ?? [];
  return attempts.find((a) => a.current === true) ?? null;
}

/**
 * attemptLabel renders the attempt accounting as `3 of 3` — the ordinal this
 * job is, over how many the task has actually dispatched. The BUDGET is a
 * separate number and rendered separately: "attempt 3 of 3" answers "is this
 * the last one that ran", which is the question the page is read with.
 *
 * '' when the job is not an attempt at a task at all.
 */
export function attemptLabel(job: JobProvenance): string {
  const attempts = job.attempts ?? [];
  const ordinal = currentAttempt(job)?.attempt ?? job.attempt ?? 0;
  if (ordinal <= 0) return '';
  const total = Math.max(attempts.length, ordinal);
  return `${ordinal} of ${total}`;
}

// ---------------------------------------------------------------------------
// Stage timeline
// ---------------------------------------------------------------------------

/** StageSpan is one lifecycle moment and how long the job spent in it. */
export interface StageSpan {
  stage: string;
  /** enteredAt is the Unix-nanosecond instant the moment was entered. */
  enteredAt: number;
  /** durationMs is the wall time until the next moment, or until the job's last update. */
  durationMs: number;
  /** open is true for a moment the job is still in — never for a stopped job. */
  open: boolean;
}

/** NANOS_PER_MS converts the server's Unix nanoseconds to milliseconds. */
const NANOS_PER_MS = 1_000_000;

/** TERMINAL_JOB_STATES are the states a job never leaves. */
const TERMINAL_JOB_STATES = new Set(['completed', 'failed']);

/** isTerminalJob reports whether the job has stopped for good. */
export function isTerminalJob(job: Pick<JobProvenance, 'state'>): boolean {
  return TERMINAL_JOB_STATES.has(job.state ?? '');
}

/**
 * stageTimeline turns the recorded moments into spans. The last moment runs
 * until the job's last update — for a finished job that is when it finished,
 * and for a running one it is as much as the record knows.
 *
 * BEING LAST IS NOT BEING OPEN. A completed or failed job's final moment has
 * nothing after it precisely because the job stopped there, so keying `open`
 * on "nothing follows" rendered every finished job's last stage as "(still
 * open)" — the page saying a job that ended hours ago is mid-discard. Only a
 * job that has not reached a terminal state can still be inside a moment.
 */
export function stageTimeline(job: JobProvenance): StageSpan[] {
  const stages = job.stages ?? [];
  const running = !isTerminalJob(job);
  return stages.map((stage, i) => {
    const next = stages[i + 1];
    const endNanos = next !== undefined ? next.entered_at : job.updated_at;
    const durationMs = Math.max(0, Math.round((endNanos - stage.entered_at) / NANOS_PER_MS));
    return {
      stage: stage.stage,
      enteredAt: stage.entered_at,
      durationMs,
      open: next === undefined && running,
    };
  });
}

/** formatDuration renders a span as `1.4s`, `2m 10s` or `1h 05m`. */
export function formatDuration(durationMs: number): string {
  if (durationMs < 1000) return `${Math.max(0, Math.round(durationMs))}ms`;
  const secs = Math.floor(durationMs / 1000);
  if (secs < 60) return `${(durationMs / 1000).toFixed(1)}s`;
  if (secs < 3600) return `${Math.floor(secs / 60)}m ${String(secs % 60).padStart(2, '0')}s`;
  return `${Math.floor(secs / 3600)}h ${String(Math.floor((secs % 3600) / 60)).padStart(2, '0')}m`;
}

// ---------------------------------------------------------------------------
// The typed terminal reason
// ---------------------------------------------------------------------------

/** TerminalReasonView is how one conclusion reason is shown and acted on. */
export interface TerminalReasonView {
  /** label is the short badge text. */
  label: string;
  /** detail is the sentence under it: what happened and what to do. */
  detail: string;
  /** tone selects the badge colour. */
  tone: 'success' | 'error' | 'warning' | 'info';
}

/**
 * TERMINAL_REASONS renders the whole closed vocabulary of
 * pkg/orchestration/task_conclusion.go. It is a map, not a switch, so a reason
 * the server adds and the UI forgets is a missing key the tests catch rather
 * than a fall-through that renders the raw wire string.
 */
export const TERMINAL_REASONS: Record<TaskConclusionReason, TerminalReasonView> = {
  completed: {
    label: 'Completed',
    detail: 'The attempt was accepted and the task is done.',
    tone: 'success',
  },
  budget_exhausted: {
    label: 'Budget exhausted',
    detail:
      'Every charged attempt was spent. The task is failed, and failed is terminal — raise the attempt budget and adopt it again rather than readmitting it.',
    tone: 'error',
  },
  abandoned: {
    label: 'Abandoned',
    detail:
      'The daemon running the job stopped and no attempts were left. Nothing diagnosed the condition, so the task is parked rather than failed.',
    tone: 'warning',
  },
  fault_bounded: {
    label: 'Environment faults bounded',
    detail:
      'The environment failed on three consecutive attempts. Repair the condition, then readmit the task.',
    tone: 'warning',
  },
  fault_held: {
    label: 'Environment fault held',
    detail:
      'A classified environment fault that retrying will not clear. Repair the condition, then readmit the task.',
    tone: 'warning',
  },
  parked_needs_info: {
    label: 'Needs information',
    detail: 'The run stopped on something only a person can settle.',
    tone: 'info',
  },
  parked_blocked: {
    label: 'Blocked',
    detail: 'An operator parked the task. Readmit it when the block is cleared.',
    tone: 'warning',
  },
  parked_escalated: {
    label: 'Escalated',
    detail: 'The engine gave up and parked the task for a person.',
    tone: 'warning',
  },
  readmitted: {
    label: 'Readmitted',
    detail: 'The task was returned to the queue and will be attempted again.',
    tone: 'info',
  },
};

/**
 * terminalReason is how the job's conclusion is shown, or null when the task
 * has not concluded. A reason the server sent and this build does not know is
 * shown verbatim rather than dropped: an unrecognised reason is still the
 * answer to "why did it stop".
 */
export function terminalReason(job: Pick<JobProvenance, 'terminal_reason'>): TerminalReasonView | null {
  const reason = job.terminal_reason;
  if (reason === undefined || reason === null || String(reason).trim() === '') return null;
  const known = TERMINAL_REASONS[reason as TaskConclusionReason];
  if (known !== undefined) return known;
  return {
    label: String(reason),
    detail: 'This build does not recognise the reason the server reported.',
    tone: 'info',
  };
}

/**
 * readmitAllowed says whether the Retry / Readmit action may be offered. The
 * SERVER decides: it is the one that knows whether the task is parked (which a
 * person may requeue) or failed (which is terminal). The UI never re-derives
 * that from the reason, because the two disagree by design — abandoned parks
 * and budget_exhausted fails, and both are "the attempts ran out".
 */
export function readmitAllowed(job: Pick<JobProvenance, 'readmit_allowed'>): boolean {
  return job.readmit_allowed === true;
}

// ---------------------------------------------------------------------------
// The worker's last turn
// ---------------------------------------------------------------------------

/**
 * AGENT_TURN_EVENT is the audit event an sdlc worker's turn is recorded
 * under. The producer is `pkg/orchestration/sdlc`'s runner:
 *
 *     s.job.Audit(auditAgentTurn, turn.Describe())   // auditAgentTurn = "sdlc:agent_turn"
 *
 * It is read OPPORTUNISTICALLY. A daemon that does not emit it has no turn to
 * show, which is not an error and not a blank panel — the panel is simply
 * absent.
 */
export const AGENT_TURN_EVENT = 'sdlc:agent_turn';

/**
 * TURN_SILENT_MARKER is what `AgentTurn.Describe` writes in place of a reply
 * when the worker answered with no text at all. A silent turn is the single
 * most important thing the page can say — it is the condition three attempts
 * at go-truststrap#26 died of — so it is rendered as itself rather than as an
 * empty box.
 */
export const TURN_SILENT_MARKER = '(the worker answered with no text)';

/**
 * AgentTurnView is one recorded worker turn, split into the header of counts
 * `Describe` writes and the reply underneath it.
 *
 * THE TWO ARE SEPARATED because they are not the same evidence. The header is
 * the daemon's measurement of the turn (how long, how many tool uses, what
 * the backend said it stopped for); the reply is the worker's own words.
 * Rendering the header inside a box labelled "the worker's reply" would
 * attribute `tokens_in=1843` to the agent.
 */
export interface AgentTurnView {
  /** metrics are the `key=value` pairs of the header, in the order written. */
  metrics: { key: string; value: string }[];
  /** reply is the worker's own text, '' when it said nothing. */
  reply: string;
  /** silent is true when the worker answered with no text at all. */
  silent: boolean;
  /** truncated is true when the reply was cut at the producer's cap. */
  truncated: boolean;
}

/** TURN_HEADER_SEPARATOR is the blank line `Describe` puts before the reply. */
const TURN_HEADER_SEPARATOR = '\n\n';

/**
 * parseAgentTurn reads one `sdlc:agent_turn` event body.
 *
 * The shape is `Describe`'s: a single line of space-separated `key=value`
 * counts, a blank line, then the reply — which may itself contain blank
 * lines, so only the FIRST separator splits.
 *
 * A body that does not carry a header at all is treated as all reply. This is
 * deliberate defensiveness, not speculation: the producer is on an unmerged
 * branch (go-quicbaas#787), and a page that threw away the worker's words
 * because a header it expected was missing would be worse than one that shows
 * them unlabelled.
 */
export function parseAgentTurn(details: string): AgentTurnView {
  const body = (details ?? '').trim();
  const split = body.indexOf(TURN_HEADER_SEPARATOR);
  const header = split === -1 ? '' : body.slice(0, split);
  const rest = (split === -1 ? body : body.slice(split + TURN_HEADER_SEPARATOR.length)).trim();
  const metrics = parseTurnMetrics(header);
  const silent = rest === TURN_SILENT_MARKER;
  return {
    metrics,
    reply: silent ? '' : rest,
    silent,
    truncated: metrics.some((m) => m.key === 'truncated' && m.value === 'true'),
  };
}

/**
 * parseTurnMetrics splits the header into its pairs. A token carrying no `=`
 * is dropped rather than shown as a key with no value — the header is machine
 * written, so an unpaired token is a producer this reader does not understand
 * and guessing at it would put noise on the page.
 */
function parseTurnMetrics(header: string): { key: string; value: string }[] {
  if (header.trim() === '') return [];
  const pairs: { key: string; value: string }[] = [];
  for (const token of header.trim().split(/\s+/)) {
    const eq = token.indexOf('=');
    if (eq <= 0) continue;
    pairs.push({ key: token.slice(0, eq), value: token.slice(eq + 1) });
  }
  return pairs;
}

/**
 * lastAgentTurn is the most recent worker turn the job recorded, or null when
 * it recorded none.
 */
export function lastAgentTurn(job: JobAuditTrail): AgentTurnView | null {
  const events = Array.isArray(job.events) ? job.events : [];
  for (let i = events.length - 1; i >= 0; i--) {
    const event = events[i];
    if (event === undefined || event.type !== AGENT_TURN_EVENT) continue;
    const details = (event.details ?? '').trim();
    if (details === '') continue;
    return parseAgentTurn(details);
  }
  return null;
}

// ---------------------------------------------------------------------------
// Review rounds
// ---------------------------------------------------------------------------

/** REVIEW_VERDICT_TONES colours a verdict badge; an unknown verdict is neutral. */
export const REVIEW_VERDICT_TONES: Record<string, string> = {
  approve: 'badge-success',
  request_changes: 'badge-warning',
  error: 'badge-error',
};

/** reviewVerdictTone is the badge class for a verdict. */
export function reviewVerdictTone(verdict: string): string {
  return REVIEW_VERDICT_TONES[verdict] ?? 'badge-ghost';
}
