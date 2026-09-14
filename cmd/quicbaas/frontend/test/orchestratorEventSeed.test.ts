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
 * Event-log seeding tests (go-quicbaas#850).
 *
 * The defect these guard: the event log was appended to ONLY by the live SSE
 * handler, so a dashboard opened after the work ran said "No events yet" about
 * jobs whose records carried a full audit trail. The trail is already on the
 * wire — every job record from ListJobs carries `events` — and it was being
 * thrown away at the UI.
 *
 * The fetch path is exercised for real (a stub SDK client, the store's own
 * refresh) rather than by calling the seeder directly, because the property
 * being defended is that a job POLL populates the log.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import {
  appendEvent,
  orchestratorStore,
  refresh,
  resetOrchestratorStore,
  type JobStatus,
} from '../src/stores/orchestrator.svelte.ts';

// ---------------------------------------------------------------------------
// Stub SDK client — the one call the jobs refresh makes.
// ---------------------------------------------------------------------------

const { mockListJobs } = vi.hoisted(() => ({ mockListJobs: vi.fn() }));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: { listJobs: mockListJobs },
  }),
}));

/** A job record shaped like the wire's, carrying an audit trail. */
function jobWithEvents(
  jobID: string,
  events: Array<{ timestamp: number; type: string; details: string }>,
): JobStatus {
  return {
    job_id: jobID,
    tenant_id: 'system',
    state: 'executing',
    iteration: 0,
    task_count: 0,
    completed: 0,
    failed: 0,
    description: 'a job',
    started_at: 1,
    updated_at: 2,
    events,
  };
}

describe('orchestrator store — the event log is seeded from the job records', () => {
  beforeEach(() => {
    resetOrchestratorStore();
    mockListJobs.mockReset();
  });

  afterEach(() => {
    resetOrchestratorStore();
  });

  it('holds the audit trail after a jobs refresh, with no stream activity at all', async () => {
    mockListJobs.mockResolvedValue([
      jobWithEvents('job-1', [
        { timestamp: 300, type: 'sdlc:agent_turn', details: 'attempt=1 round=0' },
        { timestamp: 100, type: 'orchestration:stage_passed', details: 'stage=prepare hooks=' },
      ]),
      jobWithEvents('job-2', [
        { timestamp: 200, type: 'sdlc:workspace_checkout', details: '$ git rev-parse --git-dir' },
      ]),
    ]);

    await refresh('jobs');

    const log = orchestratorStore.events;
    expect(log).toHaveLength(3);
    // Oldest first, which is the order the log renders and the order the live
    // stream appends in.
    expect(log.map((e) => e.type)).toEqual([
      'orchestration:stage_passed',
      'sdlc:workspace_checkout',
      'sdlc:agent_turn',
    ]);
    // The detail is what the operator came to read.
    expect(log[0].message).toBe('stage=prepare hooks=');
    expect(log[0].job_id).toBe('job-1');
    expect(log[1].job_id).toBe('job-2');
    // An audit line announces a moment, not a state.
    expect(log[0].state).toBe('');
  });

  it('does not duplicate an audit line across repeated polls', async () => {
    const job = jobWithEvents('job-1', [
      { timestamp: 100, type: 'sdlc:parked', details: 'needs_info' },
    ]);
    mockListJobs.mockResolvedValue([job]);

    await refresh('jobs');
    await refresh('jobs');
    await refresh('jobs');

    expect(orchestratorStore.events).toHaveLength(1);
  });

  it('does not duplicate a line the live stream already delivered', async () => {
    // The live fan-out of an audit line re-stamps the timestamp (AuditFn ->
    // publishEvent), so the streamed twin and the persisted line disagree about
    // when it happened. Identity has to be its content, or every event the
    // operator watched arrive would be seeded again beside itself.
    mockListJobs.mockResolvedValue([]);
    await refresh('jobs'); // claims the scope with nothing to seed

    appendEvent({
      job_id: 'job-1',
      type: 'sdlc:agent_turn',
      state: '' as JobStatus['state'],
      message: 'attempt=1 round=0',
      iteration: 0,
      timestamp: 999, // the fan-out's clock, not the audit line's
    });
    expect(orchestratorStore.events).toHaveLength(1);

    mockListJobs.mockResolvedValue([
      jobWithEvents('job-1', [
        { timestamp: 100, type: 'sdlc:agent_turn', details: 'attempt=1 round=0' },
      ]),
    ]);
    await refresh('jobs');

    expect(orchestratorStore.events).toHaveLength(1);
    expect(orchestratorStore.events[0].timestamp).toBe(999);
  });

  it('keeps the live events when a job trail is longer than the log bound', async () => {
    // MAX_EVENTS is 500. The seed is history and the stream is the present, so
    // when only one of them fits it is the present that must survive.
    mockListJobs.mockResolvedValue([]);
    await refresh('jobs');

    appendEvent({
      job_id: 'job-live',
      type: 'state_change',
      state: 'executing',
      message: 'entering state executing',
      iteration: 1,
      timestamp: 10_000,
    });

    const trail = Array.from({ length: 600 }, (_, i) => ({
      timestamp: i + 1,
      type: 'sdlc:agent_turn',
      details: `line ${i}`,
    }));
    mockListJobs.mockResolvedValue([jobWithEvents('job-1', trail)]);
    await refresh('jobs');

    const log = orchestratorStore.events;
    expect(log).toHaveLength(500);
    // The live event is still there, and still last.
    expect(log[log.length - 1].job_id).toBe('job-live');
    // The overflow came off the seed's own oldest end.
    expect(log.some((e) => e.message === 'line 599')).toBe(true);
    expect(log.some((e) => e.message === 'line 0')).toBe(false);
  });

  it('ignores job records with no trail, and malformed lines within one', async () => {
    mockListJobs.mockResolvedValue([
      jobWithEvents('job-1', []),
      { ...jobWithEvents('job-2', []), events: undefined },
      { ...jobWithEvents('job-3', []), events: 'nonsense' as unknown as [] },
      jobWithEvents('job-4', [
        { timestamp: 100, type: '', details: 'no type' },
        { timestamp: Number.NaN, type: 'sdlc:parked', details: 'no timestamp' },
        { timestamp: 200, type: 'sdlc:parked', details: 'kept' },
      ]),
    ]);

    await refresh('jobs');

    expect(orchestratorStore.events.map((e) => e.message)).toEqual(['kept']);
  });
});
