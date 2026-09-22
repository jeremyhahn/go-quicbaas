/*
 * Copyright (c) 2025 Jeremy Hahn
 * Copyright (c) 2025 Automate The Things, LLC
 *
 * Canonical status → variant mapping shared across every view.
 *
 * The mapping must be the single source of truth for translating arbitrary
 * status strings (e.g. "active", "queued", "failed", role labels) into a
 * `StatusVariant` understood by the StatusBadge component or the raw
 * `badge-*` daisyUI class. Drift between views was the root cause of the
 * faded-badge regression — every view used to roll its own switch.
 */

import type { StatusVariant } from './StatusBadge.svelte';

/**
 * BadgeClass is the raw daisyUI class consumed by views that render a
 * `<span class="badge {badgeClass}">` directly without using StatusBadge.
 */
export type BadgeClass =
  | 'badge-success'
  | 'badge-error'
  | 'badge-warning'
  | 'badge-info'
  | 'badge-primary'
  | 'badge-neutral'
  | 'badge-ghost';

// ---------------------------------------------------------------------------
// Canonical mapping table.
//
// Tokens are matched case-insensitively. Unknown tokens fall back to
// `'info'` so user-defined values remain visible.
// ---------------------------------------------------------------------------
const STATUS_TO_VARIANT: Readonly<Record<string, StatusVariant>> = {
  // success — running, healthy, available, ok
  active: 'success',
  enabled: 'success',
  available: 'success',
  ok: 'success',
  healthy: 'success',
  online: 'success',
  running: 'success',
  ready: 'success',
  succeeded: 'success',
  completed: 'success',

  // warning — pending, queued, paused, degraded
  pending: 'warning',
  queued: 'warning',
  paused: 'warning',
  degraded: 'warning',
  inactive: 'warning',
  truncated: 'warning',
  // a claimed task is held but not yet worked; a blocked one waits on a person
  claimed: 'warning',
  blocked: 'warning',
  // a job sending work back, or asking for a person, needs attention but has
  // not failed
  're-delegating': 'warning',
  escalating: 'warning',

  // primary — work is actively happening
  in_progress: 'primary',
  // orchestration job states where the job is doing the work
  assigning: 'primary',
  executing: 'primary',
  validating: 'primary',
  reviewing: 'primary',

  // error — failure modes
  error: 'error',
  failed: 'error',
  revoked: 'error',
  destroyed: 'error',
  cancelled: 'error',
  unavailable: 'error',
  down: 'error',
  unhealthy: 'error',

  // neutral — disabled / paused (operator intent, not a failure)
  disabled: 'neutral',
  closed: 'neutral',
  terminated: 'neutral',
  offline: 'neutral',

  // info — informational role labels and idle states
  info: 'info',
  idle: 'info',
  observer: 'info',
  worker: 'info',
  lead: 'info',
  unknown: 'info',
  // job lifecycle phases that are setup or wind-down rather than either
  // "actively working" (primary) or "waiting on something" (warning)
  initializing: 'info',
  decomposing: 'info',
  scheduling: 'info',
  completing: 'info',
  cleanup: 'info',
};

/**
 * statusToVariant maps a raw status string to the canonical StatusVariant.
 * Unknown tokens degrade to `'info'` so they remain visible.
 */
export function statusToVariant(status: string): StatusVariant {
  const key = (status ?? '').trim().toLowerCase();
  return STATUS_TO_VARIANT[key] ?? 'info';
}

const VARIANT_TO_BADGE_CLASS: Readonly<Record<StatusVariant, BadgeClass>> = {
  success: 'badge-success',
  error: 'badge-error',
  warning: 'badge-warning',
  info: 'badge-info',
  primary: 'badge-primary',
  neutral: 'badge-neutral',
  ghost: 'badge-ghost',
};

/**
 * statusBadgeClass returns the raw daisyUI badge class for a status string.
 * Use this from views that render `<span class="badge {cls}">` directly.
 */
export function statusBadgeClass(status: string): BadgeClass {
  return VARIANT_TO_BADGE_CLASS[statusToVariant(status)];
}
