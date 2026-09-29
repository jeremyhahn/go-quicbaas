/**
 * workerImageLib.test.ts — the rules the worker image panel renders by
 * (go-quicbaas#1021).
 */

import { describe, it, expect } from 'vitest';
import type { WorkerImageStatus } from '@quicbaas/sdk';
import {
  agentsLabel,
  loopBadge,
  outcomeBadge,
  passStartedAfter,
  refreshErrorMessage,
  requestedPassFinished,
  triggerLabel,
} from '../src/lib/workerImage.ts';

const requestedAt = Date.parse('2026-09-22T10:00:00.500Z');

function status(over: Partial<WorkerImageStatus> = {}): WorkerImageStatus {
  return { serving: true, running: false, queued: false, ...over };
}

describe('loopBadge', () => {
  it('says plainly when the loop is not on this node', () => {
    expect(loopBadge(null)).toEqual({ text: 'Not running on this node', variant: 'neutral' });
    expect(loopBadge(status({ serving: false }))).toEqual({ text: 'Not running on this node', variant: 'neutral' });
  });
  it('reads running before queued before idle', () => {
    expect(loopBadge(status({ running: true, queued: true })).text).toBe('Checking for updates');
    expect(loopBadge(status({ queued: true })).text).toBe('Check queued');
    expect(loopBadge(status()).text).toBe('Idle');
  });
});

describe('outcomeBadge', () => {
  it('maps every outcome to a sentence-case label and tone', () => {
    expect(outcomeBadge('current')).toEqual({ text: 'Up to date', variant: 'success' });
    expect(outcomeBadge('promoted')).toEqual({ text: 'Updated', variant: 'success' });
    expect(outcomeBadge('partial')).toEqual({ text: 'Partly updated', variant: 'warning' });
    expect(outcomeBadge('failed')).toEqual({ text: 'Failed', variant: 'error' });
  });
  it('keeps an unknown outcome visible and says when there is none', () => {
    expect(outcomeBadge('mystery')).toEqual({ text: 'mystery', variant: 'info' });
    expect(outcomeBadge(undefined).text).toBe('No pass yet');
  });
});

describe('labels', () => {
  it('names both CLIs, and says when versions are unknown', () => {
    expect(agentsLabel({ claude_code: '2.1.280', codex: '0.156.0' })).toBe('Claude Code 2.1.280 · Codex 0.156.0');
    expect(agentsLabel({ claude_code: '', codex: '' })).toBe('Versions unknown');
    expect(agentsLabel(undefined)).toBe('Versions unknown');
    expect(agentsLabel({ claude_code: '2.1.280', codex: '' })).toBe('Claude Code 2.1.280 · Codex —');
  });
  it('names the triggers in words', () => {
    expect(triggerLabel('schedule')).toBe('daily check');
    expect(triggerLabel('manual')).toBe('requested');
    expect(triggerLabel(undefined)).toBe('');
  });
});

describe('following a requested pass', () => {
  const report = (started: string) => ({
    trigger: 'manual',
    outcome: 'current' as const,
    started_at: started,
    finished_at: started,
    latest: { claude_code: '1', codex: '1' },
  });

  it('recognises the pass that started in the second of the request', () => {
    expect(passStartedAfter(report('2026-09-22T10:00:00Z'), requestedAt)).toBe(true);
    expect(passStartedAfter(report('2026-09-22T09:59:59Z'), requestedAt)).toBe(false);
    expect(passStartedAfter(report('0001-01-01T00:00:00Z'), requestedAt)).toBe(false);
    expect(passStartedAfter(undefined, requestedAt)).toBe(false);
  });

  it('is finished only when nothing runs or waits and the last pass is ours', () => {
    const ours = report('2026-09-22T10:00:05Z');
    expect(requestedPassFinished(status({ last: ours }), requestedAt)).toBe(true);
    expect(requestedPassFinished(status({ last: ours, queued: true }), requestedAt)).toBe(false);
    expect(requestedPassFinished(status({ last: ours, running: true }), requestedAt)).toBe(false);
    expect(requestedPassFinished(status({ last: report('2026-09-21T10:00:00Z') }), requestedAt)).toBe(false);
  });
});

describe('refreshErrorMessage', () => {
  it('turns each refusal into what the operator can do', () => {
    expect(refreshErrorMessage({ status: 409 })).toMatch(/already running or queued/);
    expect(refreshErrorMessage({ status: 503 })).toMatch(/not running on this node/);
    expect(refreshErrorMessage({ status: 403 })).toMatch(/worker_images:refresh/);
    expect(refreshErrorMessage(new Error('network down'))).toBe('network down');
  });
});
