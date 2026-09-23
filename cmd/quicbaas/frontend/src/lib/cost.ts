/**
 * cost.ts — the rules the Cost views render by.
 *
 * THE ONE RULE THIS FILE EXISTS FOR: a number the system does not have is
 * ABSENT AND LABELLED, never a zero. go-quicbaas#794 was filed because the
 * Cost page showed "0.0% Iterations" and "0/10" for jobs whose runner does not
 * iterate — figures that are structurally always zero, rendered as if they
 * were measurements. Three separate absences have to stay distinguishable:
 *
 *   - NO ITERATION BUDGET. Only the "workflow" runner iterates. A job driven
 *     by any other runner has no such budget, and a gauge reading 0% is a
 *     claim about a budget that does not exist.
 *   - NO SPEND POLICY. An account nobody configured a cap for is UNMETERED.
 *     That is a fact, and it is not "0% of budget used"; it is also not the
 *     same as 'unknown', which is what the server says when it could not read
 *     the account at all.
 *   - NO PRICE. Nothing in the stack prices a turn (go-quicbaas#791), so the
 *     ledger's cost is UNMEASURED, not free. It renders as a dash.
 *
 * Pure functions only: no stores, no fetch. Everything here is directly
 * testable, which is what keeps the rules from being re-derived inline in a
 * Svelte template where nothing checks them.
 */

import type {
  GetSpendReportResponse,
  SpendAccount,
  SpendChild,
  SpendLevel,
  SpendWindowStatus,
} from '@quicbaas/sdk';
import { SPEND_UNATTRIBUTED } from '@quicbaas/sdk';
import { httpStatus, errorText } from './tasks.ts';

// UNMEASURED is what a figure the system does not have renders as. An em
// dash, not '0' and not 'N/A': it reads as "nothing here" to a person and is
// never mistaken for a measurement.
export const UNMEASURED = '—';

// SPEND_UNATTRIBUTED_LEVEL re-exports the ledger's no-value sentinel under a
// name that says what it is FOR at a call site: the value a scope supplies
// for a chain level the record has no value at. Re-exported here rather than
// imported from the SDK at every call site so the meaning travels with the
// rules that use it.
export const SPEND_UNATTRIBUTED_LEVEL = SPEND_UNATTRIBUTED;

/**
 * noIterationBudgetNotice is what a non-iterating job's panel says in place of
 * a gauge. It names the runner, because "this runner does not iterate" is only
 * actionable if the reader knows which runner ran.
 *
 * WHICH runner iterates is NOT decided here. `isWorkflowJob` in
 * lib/jobDetail.ts already owns that question for the whole job page, and a
 * second copy of the rule is the copy that goes stale. This file only supplies
 * the sentence for the case that one answers no.
 */
export function noIterationBudgetNotice(runner: string | undefined | null): string {
  const name = (runner ?? '').trim();
  return name === ''
    ? 'This job has no iteration budget.'
    : `The ${name} runner does not iterate, so this job has no iteration budget.`;
}

/**
 * formatTokens renders a token count for a person. A count the report does
 * not carry is UNMEASURED, so an absent value is a dash and a real zero is
 * still "0" — a scope that genuinely spent nothing is a measurement.
 */
export function formatTokens(tokens: number | undefined | null): string {
  if (typeof tokens !== 'number' || !Number.isFinite(tokens)) return UNMEASURED;
  return tokens.toLocaleString();
}

/**
 * formatCost renders the ledger's cost ONLY when the server says it was
 * measured. Nothing prices a turn today, so this returns a dash on every
 * production report — which is the point: a rendered 0 would claim the work
 * was free. go-quicbaas#791.
 */
export function formatCost(report: Pick<GetSpendReportResponse, 'cost_micros' | 'cost_measured'> | null): string {
  if (report === null || !report.cost_measured) return UNMEASURED;
  return `${(report.cost_micros / 1_000_000).toFixed(2)}`;
}

/** costIsMeasured says whether a cost figure may be shown at all. */
export function costIsMeasured(report: Pick<GetSpendReportResponse, 'cost_measured'> | null): boolean {
  return report !== null && report.cost_measured === true;
}

/**
 * policyLabel is what an account's spend policy reads as.
 *
 * The three answers are deliberately distinct sentences. Collapsing 'unknown'
 * into 'No spend policy' would let a deployment that wires no account store
 * report every account as uncapped.
 */
export function policyLabel(account: Pick<SpendAccount, 'policy_status'>): string {
  switch (account.policy_status) {
    case 'metered':
      return 'Metered';
    case 'none':
      return 'No spend policy';
    default:
      return 'Spend policy unknown';
  }
}

/**
 * policyVariant maps a policy status onto a badge variant. Map dispatch, so a
 * status this file does not know about renders neutrally rather than as the
 * reassuring one.
 */
const POLICY_VARIANTS: Record<string, 'success' | 'info' | 'warning'> = {
  metered: 'success',
  none: 'info',
  unknown: 'warning',
};

export function policyVariant(account: Pick<SpendAccount, 'policy_status'>): 'success' | 'info' | 'warning' {
  return POLICY_VARIANTS[account.policy_status] ?? 'warning';
}

/**
 * windowPercent is how much of a quota window has been drawn, 0-100.
 *
 * A window with no allowance cannot be expressed as a percentage, and the
 * server refuses to store one; if one ever arrives, this returns null rather
 * than dividing by zero into Infinity or claiming 0%.
 */
export function windowPercent(window: Pick<SpendWindowStatus, 'max_tokens' | 'used_tokens'>): number | null {
  if (!Number.isFinite(window.max_tokens) || window.max_tokens <= 0) return null;
  return Math.min(100, (window.used_tokens / window.max_tokens) * 100);
}

/**
 * windowSummary renders one window as a sentence: what is spent, out of what,
 * over how long.
 */
export function windowSummary(window: SpendWindowStatus): string {
  return `${formatTokens(window.used_tokens)} / ${formatTokens(window.max_tokens)} tokens per ${formatDurationNanos(window.size)}`;
}

/**
 * formatDurationNanos renders a Go time.Duration (nanoseconds on the wire) as
 * the coarsest unit that divides it evenly, so "5h" does not read as "300m".
 *
 * NOTHING IS ROUNDED AWAY. A quota window is any positive time.Duration the
 * policy validator accepted, and the wire carries nanoseconds so that a window
 * shorter than a second survives the trip. Rounding to whole seconds first
 * rendered a 100ms window as "0s" -- a real limit that reads as no limit -- and
 * a 1.5s window as "2s".
 */
export function formatDurationNanos(nanos: number | undefined | null): string {
  if (typeof nanos !== 'number' || !Number.isFinite(nanos) || nanos <= 0) return UNMEASURED;
  if (nanos < 1_000_000_000) return `${trimFloat(nanos / 1_000_000)}ms`;

  const seconds = nanos / 1_000_000_000;
  const units: [number, string][] = [
    [86_400, 'd'],
    [3_600, 'h'],
    [60, 'm'],
  ];
  for (const [size, suffix] of units) {
    if (seconds % size === 0 && seconds >= size) return `${seconds / size}${suffix}`;
  }
  return `${trimFloat(seconds)}s`;
}

/**
 * trimFloat renders a number without the trailing digits binary floating point
 * adds to an exact decimal, so 1.5 prints as "1.5" and not "1.5000000000000002".
 */
function trimFloat(value: number): string {
  return String(Number(value.toPrecision(12)));
}

/**
 * childLabel names a drill-down row.
 *
 * The ledger files spend with no parent at a level — a job started outside any
 * task — under a sentinel. It must read as an absence, never as an entity
 * whose identifier is a dash.
 *
 * A row the server could NAME reads as its name: the By Project card listed
 * bare project ids, which nobody reading the page can map back to a
 * repository (go-quicbaas#1058). A project that no longer exists keeps its
 * spend under its id, so the id is the fallback.
 */
export function childLabel(child: SpendChild): string {
  if (child.unattributed === true) return `No ${child.level}`;
  const name = (child.name ?? '').trim();
  return name !== '' ? name : (child.id ?? '');
}

/** childIsNamed says whether a row renders a name rather than an identifier. */
export function childIsNamed(child: SpendChild): boolean {
  return child.unattributed !== true && (child.name ?? '').trim() !== '';
}

/**
 * childScopeValue is what a drill step supplies for the child's level.
 *
 * An unattributed row is ADDRESSABLE even though it has no id: the ledger's
 * sentinel names that subtree, and the server refuses it as a real entity id,
 * so the two can never be confused. A row a reader can see is a row a reader
 * can open.
 */
export function childScopeValue(child: SpendChild): string {
  return child.unattributed === true ? SPEND_UNATTRIBUTED : (child.id ?? '');
}

/** childIsDrillable says whether a row addresses something that can be opened. */
export function childIsDrillable(child: SpendChild): boolean {
  return childScopeValue(child) !== '';
}

/**
 * scopeLabel renders a chain level's value for a person, mapping the ledger's
 * no-value sentinel to the absence it stands for. Printing the raw '-' as a
 * project name is the exact lie this file exists to prevent.
 */
export function scopeLabel(level: SpendLevel | string, value: string, name = ''): string {
  if (value === SPEND_UNATTRIBUTED) return `No ${level}`;
  const trimmed = name.trim();
  return trimmed !== '' ? trimmed : value;
}

/**
 * LEVEL_LABELS names each chain level for a person. Map dispatch rather than
 * a switch, and a level this build does not know renders as itself rather
 * than as a blank.
 */
const LEVEL_LABELS: Record<string, string> = {
  tenant: 'Tenant',
  project: 'Project',
  task: 'Task',
  job: 'Job',
  session: 'Session',
  round: 'Round',
};

export function levelLabel(level: SpendLevel | string | undefined | null): string {
  const name = (level ?? '').trim();
  return LEVEL_LABELS[name] ?? name;
}

/**
 * spendErrorMessage turns a failed spend read into a sentence that says what
 * the reader can do about it.
 *
 * IT ADDS THE ONE STATUS lib/tasks.ts HAS NO OPINION ABOUT: 503, which the
 * server returns when the deployment records no consumption at all. That is
 * the opposite of "nothing has been spent", and rendering it as an empty
 * report is precisely the reassuring lie this feature exists to remove.
 */
export function spendErrorMessage(err: unknown, subject: string): string {
  if (httpStatus(err) === 503) {
    return `This deployment is not recording provider consumption, so ${subject} cannot be reported.`;
  }
  switch (httpStatus(err)) {
    case 401:
      return `Your session is no longer valid. Sign in again to view ${subject}.`;
    case 403:
      return `You do not have permission to view ${subject}: this account lacks orchestrator:status.`;
    case 400:
      return `Could not read ${subject}: ${errorText(err)}`;
    default:
      return `Could not load ${subject}: ${errorText(err)}`;
  }
}

/**
 * reportProjectName is the breadcrumb name an accepted spend report carries for
 * the project in scope: the report's own `project_name`, or empty when it has
 * none. Every accepted report replaces the previous name, so a report that
 * carries no name (a tenant-level scope, another tenant, a deleted project)
 * clears it rather than leaving the last project's name on screen.
 */
export function reportProjectName(report: Pick<GetSpendReportResponse, 'project_name'>): string {
  return report.project_name ?? '';
}
