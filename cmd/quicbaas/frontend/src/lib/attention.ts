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
 * Pure helpers for the attention inbox — every blocker the factory has
 * stopped on and cannot start again by itself.
 *
 * Every function takes the narrowest structural type it needs so the SDK's
 * AttentionItem and a test literal both satisfy it.
 */

import { formatRelative, toMillis } from './time.ts';

/** The fields an item's age is read from. */
export interface AttentionAgeFields {
  status_changed_at?: number;
  created_at?: number;
}

/**
 * itemAgeInstant is the instant a card's age is measured from:
 * status_changed_at (when the item started waiting) falling back to
 * created_at for an item that carries no status change — an environmental
 * alert observed just now.
 */
export function itemAgeInstant(item: AttentionAgeFields): number | string | undefined {
  return item.status_changed_at || item.created_at;
}

/** itemAgeLabel renders how long an item has been waiting, e.g. "2h ago". */
export function itemAgeLabel(item: AttentionAgeFields): string {
  return formatRelative(itemAgeInstant(item));
}

/**
 * oldestAgeInstant returns the age instant of the oldest item in a list, or
 * null for an empty list — what the dashboard card's "oldest item's age"
 * reads.
 */
export function oldestAgeInstant<T extends AttentionAgeFields>(items: readonly T[]): number | string | undefined {
  let oldest: T | undefined;
  let oldestMs = Number.POSITIVE_INFINITY;
  for (const item of items) {
    const ms = toMillis(itemAgeInstant(item));
    if (ms !== null && ms < oldestMs) {
      oldestMs = ms;
      oldest = item;
    }
  }
  return oldest === undefined ? undefined : itemAgeInstant(oldest);
}

/** The fields that say whether an item is an environmental alert. */
export interface AttentionKindFields {
  kind: string;
}

/** ENVIRONMENTAL_ALERT_KINDS is every kind that is not a parked task. */
export const ENVIRONMENTAL_ALERT_KINDS = new Set(['agent_offline', 'account_error', 'barrier_sealed', 'project_idle']);

/** isEnvironmentalAlert is true for a fact about the factory, not the work. */
export function isEnvironmentalAlert(item: AttentionKindFields): boolean {
  return ENVIRONMENTAL_ALERT_KINDS.has(item.kind);
}

/** isParkedTask is true for a blocked task the runner parked. */
export function isParkedTask(item: AttentionKindFields): boolean {
  return item.kind === 'parked_task';
}

/** kindLabel renders an attention kind as the plain-English word an operator reads. */
const KIND_LABELS: Record<string, string> = {
  parked_task: 'Parked task',
  agent_offline: 'Agent offline',
  account_error: 'Credential error',
  barrier_sealed: 'Barrier sealed',
  project_idle: 'Project idle',
};

export function kindLabel(kind: string): string {
  return KIND_LABELS[kind] ?? kind;
}

/** blockKindLabel renders an orchestration.TaskBlockKind for a person. */
const BLOCK_KIND_LABELS: Record<string, string> = {
  needs_info: 'Needs info',
  blocked: 'Blocked',
  escalated: 'Escalated',
};

export function blockKindLabel(blockKind: string | undefined): string {
  if (!blockKind) return '';
  return BLOCK_KIND_LABELS[blockKind] ?? blockKind;
}

/** conditionLabel renders an sdlc.UnblockKind for a person. */
const CONDITION_LABELS: Record<string, string> = {
  pipeline_green: 'Waiting on CI to go green',
  forge_issue_state: 'Waiting on a linked issue',
  release_tag: 'Waiting on a release tag',
  owner_ruling: 'Waiting on an owner ruling',
  operator_repair: 'Waiting on an operator repair',
};

export function conditionLabel(condition: string | undefined): string {
  if (!condition) return '';
  return CONDITION_LABELS[condition] ?? condition;
}

/** severityVariant maps an alert's severity onto the app's StatusBadge variant. */
export function severityVariant(severity: string | undefined): 'error' | 'warning' | 'info' {
  if (severity === 'critical') return 'error';
  if (severity === 'warning') return 'warning';
  return 'info';
}

// =============================================================================
// Actions
// =============================================================================

export type AttentionAction = 'approve_recommendation' | 'answer' | 'allow_another_round' | 'close_as_done' | 'readmit' | 'note';

const ACTION_LABELS: Record<AttentionAction, string> = {
  approve_recommendation: 'Approve recommendation',
  answer: 'Answer',
  allow_another_round: 'Allow another round',
  close_as_done: 'Close as done',
  readmit: 'Readmit',
  note: 'Add note',
};

export function actionLabel(action: string): string {
  return ACTION_LABELS[action as AttentionAction] ?? action;
}

/**
 * TEXT_ACTIONS are the actions that carry the operator's own words: an
 * answer readmits the task with them, a note posts them and leaves the task
 * parked. Each opens the card's text box instead of acting at once.
 */
const TEXT_ACTIONS: Record<string, string> = {
  answer: 'Post answer',
  note: 'Post note',
};

/** takesText is true for an action that needs the operator's words first. */
export function takesText(action: string): boolean {
  return action in TEXT_ACTIONS;
}

/** submitLabel names the button that posts a text action's words. */
export function submitLabel(action: string): string {
  return TEXT_ACTIONS[action] ?? actionLabel(action);
}

/** The fields a card's action list is read from. */
export interface AttentionActionFields {
  actions?: string[];
  recommendation?: string;
}

/**
 * primaryAction is the action a card's primary button performs:
 * approve_recommendation when the item offers one, otherwise the first
 * action the item carries, or null when it carries none (an environmental
 * alert, which is observed rather than resolved).
 */
export function primaryAction(item: AttentionActionFields): AttentionAction | null {
  const actions = item.actions ?? [];
  if (actions.includes('approve_recommendation')) return 'approve_recommendation';
  return (actions[0] as AttentionAction) ?? null;
}

/** secondaryActions is every action other than the primary one. */
export function secondaryActions(item: AttentionActionFields): AttentionAction[] {
  const primary = primaryAction(item);
  return (item.actions ?? []).filter((a) => a !== primary) as AttentionAction[];
}

// =============================================================================
// Issue linking
// =============================================================================

/** The fields a card's issue link is read from. */
export interface AttentionIssueFields {
  issue_ref?: string;
  issue_url?: string;
}

/** hasIssueLink is true when the item names a browsable forge issue. */
export function hasIssueLink(item: AttentionIssueFields): boolean {
  return Boolean(item.issue_url && item.issue_url.trim() !== '');
}

// =============================================================================
// Facet / project grouping
// =============================================================================

/** The fields a project health strip groups by. */
export interface AttentionProjectFields {
  project_id?: string;
  project_name?: string;
}

/**
 * parkedCountByProject counts parked-task items per project id, for the
 * dashboard's project health strip. Environmental alerts (which name no
 * project, or name one incidentally) are excluded — the strip's "parked"
 * column is specifically about work waiting on a person.
 */
export function parkedCountByProject<T extends AttentionKindFields & AttentionProjectFields>(
  items: readonly T[],
): Map<string, { name: string; count: number }> {
  const counts = new Map<string, { name: string; count: number }>();
  for (const item of items) {
    if (!isParkedTask(item) || !item.project_id) continue;
    const existing = counts.get(item.project_id);
    if (existing) {
      existing.count += 1;
    } else {
      counts.set(item.project_id, { name: item.project_name || item.project_id, count: 1 });
    }
  }
  return counts;
}

// =============================================================================
// Banner dismissal — per session, per alert id
// =============================================================================

const DISMISSED_STORAGE_KEY = 'quicbaas.attention.dismissedAlerts';

/** readDismissedAlerts returns the set of alert ids dismissed this session. */
export function readDismissedAlerts(storage: Pick<Storage, 'getItem'> | undefined): Set<string> {
  if (!storage) return new Set();
  try {
    const raw = storage.getItem(DISMISSED_STORAGE_KEY);
    if (!raw) return new Set();
    const parsed: unknown = JSON.parse(raw);
    return Array.isArray(parsed) ? new Set(parsed.filter((v): v is string => typeof v === 'string')) : new Set();
  } catch {
    return new Set();
  }
}

/** writeDismissedAlerts persists the set of alert ids dismissed this session. */
export function writeDismissedAlerts(storage: Pick<Storage, 'setItem'> | undefined, dismissed: ReadonlySet<string>): void {
  if (!storage) return;
  try {
    storage.setItem(DISMISSED_STORAGE_KEY, JSON.stringify([...dismissed]));
  } catch {
    // Storage may be full or disabled (private browsing); the banner simply
    // reappears next render, which is the safe direction to fail in.
  }
}

/** visibleAlerts filters environmental alerts down to the ones not dismissed. */
export function visibleAlerts<T extends AttentionKindFields & { id: string }>(
  items: readonly T[],
  dismissed: ReadonlySet<string>,
): T[] {
  return items.filter((item) => isEnvironmentalAlert(item) && !dismissed.has(item.id));
}

// =============================================================================
// The complete set, and the inbox's filtered view of it
// =============================================================================

/** The narrowing the inbox's two dropdowns select. Empty means "all". */
export interface AttentionViewFilter {
  projectID?: string;
  kind?: string;
}

/**
 * filterAttention narrows the complete set to what the inbox's dropdowns
 * select. It is a VIEW over the store's items, never a second fetch: the
 * badge, the dashboard card and the alert banner read the same complete set,
 * and a filtered fetch written into it hid every alert the filter excluded.
 */
export function filterAttention<T extends AttentionKindFields & AttentionProjectFields>(
  items: readonly T[],
  filter: AttentionViewFilter,
): T[] {
  return items.filter(
    (item) =>
      (!filter.projectID || item.project_id === filter.projectID) &&
      (!filter.kind || item.kind === filter.kind),
  );
}

/** One page of a listing, as far as fetchAllPages needs to read it. */
export interface AttentionPage<T> {
  items?: T[];
  total?: number;
  offset?: number;
  next_offset?: number;
  facets?: Record<string, Record<string, number>>;
}

/** The complete set fetchAllPages assembles. */
export interface AttentionSet<T> {
  items: T[];
  total: number;
  facets: Record<string, Record<string, number>>;
}

/**
 * ATTENTION_PAGE_LIMIT is the page size the inbox asks for: the server's
 * maximum, so a normal inbox is one round trip.
 */
export const ATTENTION_PAGE_LIMIT = 100;

/**
 * ATTENTION_MAX_PAGES bounds how many pages one refresh follows — a server
 * that kept answering with a next_offset would otherwise hold the refresh
 * open forever. 50 pages of 100 is far past any inbox a person can clear.
 */
export const ATTENTION_MAX_PAGES = 50;

/**
 * fetchAllPages follows next_offset until the listing ends, so every item
 * is reachable — one page of 100 left the 101st parked task nowhere in the
 * UI. total and facets are the first page's: the server counts them over the
 * whole matched set before it cuts a page. A next_offset that does not
 * advance ends the walk rather than looping on the same page.
 */
export async function fetchAllPages<T>(
  fetchPage: (offset: number) => Promise<AttentionPage<T>>,
  maxPages: number = ATTENTION_MAX_PAGES,
): Promise<AttentionSet<T>> {
  const first = await fetchPage(0);
  const items: T[] = [...(first.items ?? [])];
  let next = first.next_offset;
  let current = first.offset ?? 0;
  for (let page = 1; page < maxPages && next !== undefined && next > current; page++) {
    const resp = await fetchPage(next);
    items.push(...(resp.items ?? []));
    current = next;
    next = resp.next_offset;
  }
  return { items, total: first.total ?? items.length, facets: first.facets ?? {} };
}
