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
 * workerImage.ts — how the worker image refresh reads to an operator.
 *
 * The daemon keeps the Software Factory worker image current by itself: once
 * a day it reads the latest Claude Code and Codex CLIs from npm and, when the
 * bound image is behind, rebuilds, gates and rebinds (go-quicbaas#1021). This
 * file turns the loop's status into the few sentences and badges the panel
 * shows. Pure functions only, so every rule here is tested directly.
 */

import type { WorkerImageAgents, WorkerImageReport, WorkerImageStatus } from '@quicbaas/sdk';
import type { StatusVariant } from '../components/core/StatusBadge.svelte';
import { hasInstant, toMillis } from './time.ts';
import { httpStatus, errorText } from './tasks.ts';

/** A badge: the words and the tone. */
export interface Badge {
  text: string;
  variant: StatusVariant;
}

/**
 * loopBadge says what the refresh loop is doing now. A node without the loop
 * (no container engine, the refresh disabled, the SDLC module off) is said
 * plainly rather than shown as idle.
 */
export function loopBadge(status: WorkerImageStatus | null): Badge {
  if (!status || !status.serving) return { text: 'Not running on this node', variant: 'neutral' };
  if (status.running) return { text: 'Checking for updates', variant: 'primary' };
  if (status.queued) return { text: 'Check queued', variant: 'warning' };
  return { text: 'Idle', variant: 'info' };
}

// OUTCOMES maps each pass outcome to its sentence-case label and tone.
const OUTCOMES: Readonly<Record<string, Badge>> = {
  current: { text: 'Up to date', variant: 'success' },
  promoted: { text: 'Updated', variant: 'success' },
  partial: { text: 'Partly updated', variant: 'warning' },
  failed: { text: 'Failed', variant: 'error' },
};

/** outcomeBadge renders a pass outcome; an unknown one stays visible. */
export function outcomeBadge(outcome: string | undefined): Badge {
  if (!outcome) return { text: 'No pass yet', variant: 'neutral' };
  return OUTCOMES[outcome] ?? { text: outcome, variant: 'info' };
}

/** triggerLabel says what started a pass. */
export function triggerLabel(trigger: string | undefined): string {
  switch (trigger) {
    case 'schedule':
      return 'daily check';
    case 'manual':
      return 'requested';
    default:
      return trigger ?? '';
  }
}

/**
 * agentsLabel renders the two CLI versions an image carries. An image whose
 * versions could not be read says so — an unknown version is exactly why a
 * refresh treats an image as stale.
 */
export function agentsLabel(agents: WorkerImageAgents | undefined): string {
  if (!agents || (!agents.claude_code && !agents.codex)) return 'Versions unknown';
  return `Claude Code ${agents.claude_code || '—'} · Codex ${agents.codex || '—'}`;
}

/**
 * passStartedAfter reports whether report is a pass that started at or after
 * requestedAt (milliseconds) — the pass a "Check for updates now" asked for.
 * Server timestamps carry whole seconds, so the request is compared at that
 * resolution.
 */
export function passStartedAfter(report: WorkerImageReport | undefined, requestedAt: number): boolean {
  if (!report || !hasInstant(report.started_at)) return false;
  const started = toMillis(report.started_at);
  return started !== null && started >= Math.floor(requestedAt / 1000) * 1000;
}

/**
 * requestedPassFinished reports whether status shows the pass requested at
 * requestedAt has finished: nothing running or queued, and the last report is
 * that pass.
 */
export function requestedPassFinished(status: WorkerImageStatus, requestedAt: number): boolean {
  return !status.running && !status.queued && passStartedAfter(status.last, requestedAt);
}

/**
 * refreshErrorMessage turns a refused "Check for updates now" into a sentence
 * that says what the operator can do about it.
 */
export function refreshErrorMessage(err: unknown): string {
  switch (httpStatus(err)) {
    case 409:
      return 'A check is already running or queued. Its result will appear here.';
    case 503:
      return 'The worker image refresh is not running on this node.';
    case 403:
      return 'Your account cannot request a check (it needs worker_images:refresh as a system operator).';
    default:
      return errorText(err);
  }
}
