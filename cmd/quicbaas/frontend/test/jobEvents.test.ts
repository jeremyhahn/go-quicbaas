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
 * Live job events (go-quicbaas#580).
 *
 * The job event stream existed and nothing imported it: a running job's state,
 * iteration and stage only moved when the 30 s poll came round, and the Live
 * Events panel stayed empty. These cover the two halves of the wiring:
 *
 *  - `applyJobEvent` — an event patches the job it belongs to, in the list AND
 *    in the open detail view, and invents nothing for a job that is not there;
 *  - `syncActiveJobStreams` — the shell holds one stream per running job,
 *    capped, and lets a stream go when the job stops or leaves the list,
 *    except the one a detail view has pinned.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

// ---------------------------------------------------------------------------
// Mocks. The SDK client is the only edge here — the orchestrator store is the
// real one, because the point is that the two modules agree.
// ---------------------------------------------------------------------------

const { subscribeJobEvents, unsubscribes, listJobs } = vi.hoisted(() => {
  const unsubscribes = new Map<string, ReturnType<typeof vi.fn>>();
  return {
    unsubscribes,
    listJobs: vi.fn(async () => [] as unknown[]),
    subscribeJobEvents: vi.fn((_tenantID: string, jobID: string, _onEvent: unknown) => {
      const stop = vi.fn();
      unsubscribes.set(jobID, stop);
      return stop;
    }),
  };
});

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(async () => ({
    orchestrator: { subscribeJobEvents, listJobs },
  })),
}));

import {
  appendEvent,
  applyJobEvent,
  isTerminalJobState,
  loadJobs,
  orchestratorStore,
  resetOrchestratorStore,
  selectJob,
  type JobEvent,
  type JobState,
  type JobStatus,
} from '../src/stores/orchestrator.svelte.ts';
import {
  MAX_ACTIVE_JOB_STREAMS,
  eventSubStore,
  startEventStream,
  stopAllJobStreams,
  stopEventStream,
  syncActiveJobStreams,
} from '../src/stores/job-events.svelte.ts';

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

function job(jobID: string, state: JobState, overrides: Partial<JobStatus> = {}): JobStatus {
  return {
    job_id: jobID,
    tenant_id: 'acme',
    state,
    iteration: 1,
    task_count: 1,
    completed: 0,
    failed: 0,
    description: jobID,
    started_at: 1700000000,
    updated_at: 1700000000,
    ...overrides,
  } as JobStatus;
}

function event(overrides: Partial<JobEvent> = {}): JobEvent {
  return {
    job_id: 'job-1',
    type: 'state_change',
    state: 'executing',
    message: '',
    iteration: 1,
    timestamp: 1700000001,
    ...overrides,
  } as JobEvent;
}

/**
 * Puts the given jobs in the store through the real load path, so the cache is
 * in scope and the getters report it. Writing the array directly would leave
 * the store's scope key unset and every getter would read empty.
 */
async function seedJobs(jobs: JobStatus[]): Promise<void> {
  resetOrchestratorStore();
  listJobs.mockResolvedValueOnce(jobs);
  await loadJobs();
}

/** Resolves once every stream the last call asked for has opened. */
async function streamsSettled(count: number): Promise<void> {
  await vi.waitFor(() => {
    expect(subscribeJobEvents).toHaveBeenCalledTimes(count);
  });
}

/** The job ids streams are currently open against, in call order. */
function subscribedJobIDs(): string[] {
  return subscribeJobEvents.mock.calls.map((c) => c[1] as string);
}

beforeEach(() => {
  stopAllJobStreams();
  resetOrchestratorStore();
  subscribeJobEvents.mockClear();
  unsubscribes.clear();
  listJobs.mockReset();
  listJobs.mockResolvedValue([]);
});

afterEach(() => {
  stopAllJobStreams();
  resetOrchestratorStore();
});

// ---------------------------------------------------------------------------
// applyJobEvent
// ---------------------------------------------------------------------------

describe('applyJobEvent', () => {
  it('moves the job in the list to the state the event reports', async () => {
    await seedJobs([job('job-1', 'executing'), job('job-2', 'assigning')]);

    applyJobEvent(event({ job_id: 'job-1', state: 'reviewing', iteration: 4 }));

    const patched = orchestratorStore.jobs.find((j) => j.job_id === 'job-1');
    expect(patched?.state).toBe('reviewing');
    expect(patched?.iteration).toBe(4);
    // The event named one job; the other is untouched.
    expect(orchestratorStore.jobs.find((j) => j.job_id === 'job-2')?.state).toBe('assigning');
  });

  it('records the lifecycle stage the event carries in its metadata', async () => {
    await seedJobs([job('job-1', 'executing')]);

    applyJobEvent(event({ job_id: 'job-1', type: 'stage', metadata: { stage: 'validate' } }));

    expect(orchestratorStore.jobs[0].stage).toBe('validate');
  });

  it('patches the open detail view as well as the row', async () => {
    await seedJobs([job('job-1', 'executing')]);
    // A poll replaces the list under an open detail view, so the two are
    // distinct objects: patching only the row would freeze the detail page.
    selectJob({ ...job('job-1', 'executing') });

    applyJobEvent(event({ job_id: 'job-1', state: 'completed', iteration: 7 }));

    expect(orchestratorStore.selectedJob?.state).toBe('completed');
    expect(orchestratorStore.selectedJob?.iteration).toBe(7);
  });

  it('invents no job for an event about one the list does not hold', async () => {
    await seedJobs([job('job-1', 'executing')]);

    applyJobEvent(event({ job_id: 'job-unknown', state: 'failed' }));

    expect(orchestratorStore.jobs).toHaveLength(1);
    expect(orchestratorStore.jobs[0].job_id).toBe('job-1');
  });

  it('leaves the job alone when the event says nothing about it', async () => {
    await seedJobs([job('job-1', 'executing')]);
    const before = orchestratorStore.jobs[0];

    applyJobEvent({
      job_id: 'job-1',
      type: 'log',
      state: '' as JobState,
      message: 'a line of output',
      iteration: Number.NaN,
      timestamp: 1700000002,
    } as JobEvent);

    expect(orchestratorStore.jobs[0]).toBe(before);
  });

  it('ignores an event with no job id', async () => {
    await seedJobs([job('job-1', 'executing')]);

    applyJobEvent(event({ job_id: '' }));

    expect(orchestratorStore.jobs[0].state).toBe('executing');
  });
});

// ---------------------------------------------------------------------------
// appendEvent's type fallback
// ---------------------------------------------------------------------------

describe('appendEvent', () => {
  it('labels an event that carries only a state with that state', async () => {
    await seedJobs([job('job-1', 'executing')]);

    appendEvent(event({ type: '', state: 'reviewing' }));

    expect(orchestratorStore.events[0].type).toBe('reviewing');
  });

  it('keeps two untyped events of the same instant apart', async () => {
    await seedJobs([job('job-1', 'executing')]);

    appendEvent(event({ type: '', state: 'executing', timestamp: 1700000009 }));
    appendEvent(event({ type: '', state: 'reviewing', timestamp: 1700000009 }));

    // Both are real transitions. Before the fallback they shared an empty type
    // and the second was discarded as a duplicate.
    expect(orchestratorStore.events.map((e) => e.type)).toEqual(['executing', 'reviewing']);
  });

  it('still discards a genuine replay of the same event', async () => {
    await seedJobs([job('job-1', 'executing')]);

    appendEvent(event({ type: '', state: 'executing', timestamp: 1700000009 }));
    appendEvent(event({ type: '', state: 'executing', timestamp: 1700000009 }));

    expect(orchestratorStore.events).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// isTerminalJobState
// ---------------------------------------------------------------------------

describe('isTerminalJobState', () => {
  it('is true only for the states nothing more is emitted from', () => {
    expect(isTerminalJobState('completed')).toBe(true);
    expect(isTerminalJobState('failed')).toBe(true);
    expect(isTerminalJobState('executing')).toBe(false);
    expect(isTerminalJobState('cleanup')).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// syncActiveJobStreams
// ---------------------------------------------------------------------------

describe('syncActiveJobStreams', () => {
  it('opens one stream per running job and none for the stopped ones', async () => {
    syncActiveJobStreams('acme', [
      job('job-1', 'executing'),
      job('job-2', 'completed'),
      job('job-3', 'reviewing'),
      job('job-4', 'failed'),
    ]);

    await streamsSettled(2);
    expect(subscribedJobIDs()).toEqual(['job-1', 'job-3']);
    expect(eventSubStore.subscriptionCount).toBe(2);
  });

  it('opens nothing new when the same list is synced again', async () => {
    const jobs = [job('job-1', 'executing'), job('job-2', 'assigning')];
    syncActiveJobStreams('acme', jobs);
    await streamsSettled(2);

    syncActiveJobStreams('acme', jobs);
    syncActiveJobStreams('acme', jobs);

    await streamsSettled(2);
    expect(eventSubStore.subscriptionCount).toBe(2);
  });

  it('drops the stream for a job that has finished', async () => {
    syncActiveJobStreams('acme', [job('job-1', 'executing'), job('job-2', 'executing')]);
    await streamsSettled(2);

    syncActiveJobStreams('acme', [job('job-1', 'completed'), job('job-2', 'executing')]);

    await vi.waitFor(() => {
      expect(unsubscribes.get('job-1')).toHaveBeenCalledTimes(1);
    });
    expect(unsubscribes.get('job-2')).not.toHaveBeenCalled();
    expect(eventSubStore.subscriptionCount).toBe(1);
  });

  it('drops the stream for a job that has left the list', async () => {
    syncActiveJobStreams('acme', [job('job-1', 'executing'), job('job-2', 'executing')]);
    await streamsSettled(2);

    syncActiveJobStreams('acme', [job('job-2', 'executing')]);

    await vi.waitFor(() => {
      expect(unsubscribes.get('job-1')).toHaveBeenCalledTimes(1);
    });
    expect(eventSubStore.subscriptionCount).toBe(1);
  });

  it('holds no more than the cap, however many jobs are running', async () => {
    const many = Array.from({ length: MAX_ACTIVE_JOB_STREAMS + 5 }, (_, i) =>
      job(`job-${i}`, 'executing'),
    );

    syncActiveJobStreams('acme', many);

    await streamsSettled(MAX_ACTIVE_JOB_STREAMS);
    expect(eventSubStore.subscriptionCount).toBe(MAX_ACTIVE_JOB_STREAMS);
  });

  it('tolerates a job list that is absent or holds an unusable row', async () => {
    syncActiveJobStreams('acme', null);
    syncActiveJobStreams('acme', undefined);
    syncActiveJobStreams('acme', [job('', 'executing')]);

    await vi.waitFor(() => {
      expect(eventSubStore.subscriptionCount).toBe(0);
    });
    expect(subscribeJobEvents).not.toHaveBeenCalled();
  });

  it('routes a received event into the log and onto the job', async () => {
    await seedJobs([job('job-1', 'executing')]);
    syncActiveJobStreams('acme', [job('job-1', 'executing')]);
    await streamsSettled(1);

    const onEvent = subscribeJobEvents.mock.calls[0][2] as (e: JobEvent) => void;
    onEvent(event({ job_id: 'job-1', state: 'reviewing', iteration: 5 }));

    expect(orchestratorStore.events).toHaveLength(1);
    expect(orchestratorStore.jobs[0].state).toBe('reviewing');
    expect(orchestratorStore.jobs[0].iteration).toBe(5);
  });
});

// ---------------------------------------------------------------------------
// The detail view's pinned stream
// ---------------------------------------------------------------------------

describe('startEventStream / stopEventStream', () => {
  it('pins the job the detail view shows', async () => {
    startEventStream('acme', 'job-9');

    await streamsSettled(1);
    expect(eventSubStore.activeJobID).toBe('job-9');
    expect(subscribedJobIDs()).toEqual(['job-9']);
  });

  it('opens one connection when the shell already streams the same job', async () => {
    syncActiveJobStreams('acme', [job('job-9', 'executing')]);
    await streamsSettled(1);

    startEventStream('acme', 'job-9');

    await streamsSettled(1);
    expect(eventSubStore.subscriptionCount).toBe(1);
  });

  it('never lets a sync close the job the detail view is showing', async () => {
    startEventStream('acme', 'job-9');
    await streamsSettled(1);

    // The job finished, so the shell no longer wants a stream for it — the
    // operator is still looking at it.
    syncActiveJobStreams('acme', [job('job-9', 'completed')]);

    await vi.waitFor(() => {
      expect(eventSubStore.subscriptionCount).toBe(1);
    });
    expect(unsubscribes.get('job-9')).not.toHaveBeenCalled();
  });

  it('releases the pinned stream when the view leaves', async () => {
    startEventStream('acme', 'job-9');
    await streamsSettled(1);

    stopEventStream();

    expect(unsubscribes.get('job-9')).toHaveBeenCalledTimes(1);
    expect(eventSubStore.activeJobID).toBe('');
    expect(eventSubStore.subscriptionActive).toBe(false);
  });

  it('keeps the stream open on unpin when the shell still wants it', async () => {
    syncActiveJobStreams('acme', [job('job-9', 'executing')]);
    await streamsSettled(1);
    startEventStream('acme', 'job-9');

    stopEventStream();

    expect(unsubscribes.get('job-9')).not.toHaveBeenCalled();
    expect(eventSubStore.subscriptionCount).toBe(1);
  });

  it('releases the previous job when the view moves to another', async () => {
    startEventStream('acme', 'job-9');
    await streamsSettled(1);

    startEventStream('acme', 'job-10');

    await streamsSettled(2);
    expect(unsubscribes.get('job-9')).toHaveBeenCalledTimes(1);
    expect(eventSubStore.activeJobID).toBe('job-10');
  });

  it('closes every stream when the shell goes away', async () => {
    syncActiveJobStreams('acme', [job('job-1', 'executing'), job('job-2', 'executing')]);
    await streamsSettled(2);
    startEventStream('acme', 'job-3');
    await streamsSettled(3);

    stopAllJobStreams();

    expect(unsubscribes.get('job-1')).toHaveBeenCalledTimes(1);
    expect(unsubscribes.get('job-2')).toHaveBeenCalledTimes(1);
    expect(unsubscribes.get('job-3')).toHaveBeenCalledTimes(1);
    expect(eventSubStore.subscriptionCount).toBe(0);
    expect(eventSubStore.activeJobID).toBe('');
  });
});
