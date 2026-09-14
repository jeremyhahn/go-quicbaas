/**
 * jobProvenanceLoad.test.ts
 *
 * The job detail page must be populated from `GetJobStatus`, not from the
 * list.
 *
 * `ListJobs` returns the RAW persisted statuses; only `GetJobStatus` applies
 * `withTaskProvenance`, which lays the task's attempt history, attempt
 * budget, typed terminal reason, readmit state and delivery over the record.
 * Selecting the list row and stopping there rendered every panel
 * go-quicbaas#796 adds empty — on the most common path into the page.
 */

import { describe, it, expect, beforeEach, vi } from 'vitest';

const { getJobStatusMock, listJobsMock } = vi.hoisted(() => ({
  getJobStatusMock: vi.fn(),
  listJobsMock: vi.fn(),
}));

vi.mock('../../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn().mockResolvedValue({
    orchestrator: {
      getJobStatus: getJobStatusMock,
      listJobs: listJobsMock,
    },
  }),
}));

import {
  applyJobEvent,
  loadSelectedJob,
  orchestratorStore,
  selectJob,
  setActiveTenant,
} from '../../src/stores/orchestrator.svelte.ts';

/** listRecord is a job as ListJobs returns it: no provenance at all. */
function listRecord(): Record<string, unknown> {
  return {
    job_id: 'job-3',
    tenant_id: 'acme',
    state: 'failed',
    iteration: 0,
    task_count: 0,
    completed: 0,
    failed: 0,
    description: 'go-truststrap#26',
    started_at: 1,
    updated_at: 2,
    runner: 'sdlc',
    task_id: 'task-26',
    attempt: 3,
  };
}

/** enrichedRecord is the same job as GetJobStatus returns it. */
function enrichedRecord(): Record<string, unknown> {
  return {
    ...listRecord(),
    attempts: [
      { attempt: 1, job_id: 'job-1' },
      { attempt: 2, job_id: 'job-2' },
      { attempt: 3, job_id: 'job-3', current: true },
    ],
    attempt_budget: 3,
    terminal_reason: 'budget_exhausted',
    readmit_allowed: false,
    delivery: { branch: 'issue/26', head_sha: 'abc', pull_request_number: 57 },
    verification: [{ name: 'make test', passed: false }],
    // GetJobStatus serializes the optional members as null, not absent.
    config: null,
    memory: null,
  };
}

describe('loadSelectedJob', () => {
  beforeEach(() => {
    getJobStatusMock.mockReset();
    listJobsMock.mockReset();
    setActiveTenant('acme');
    selectJob(null);
  });

  it('replaces a list-shaped selection with the enriched record', async () => {
    // The page paints from the list row, then fills in. What it fills in with
    // is the whole of the feature.
    selectJob(listRecord() as never);
    expect(orchestratorStore.selectedJob?.terminal_reason).toBeUndefined();

    getJobStatusMock.mockResolvedValue(enrichedRecord());
    await loadSelectedJob('job-3');

    expect(getJobStatusMock).toHaveBeenCalledWith('acme', 'job-3');
    const job = orchestratorStore.selectedJob;
    expect(job?.terminal_reason).toBe('budget_exhausted');
    expect(job?.attempt_budget).toBe(3);
    expect(job?.attempts).toHaveLength(3);
    expect(job?.delivery?.pull_request_number).toBe(57);
    expect(job?.verification).toHaveLength(1);
    expect(job?.readmit_allowed).toBe(false);
  });

  it('normalises the SDK\'s nulls to the store\'s absent members', () => {
    // The SDK types config/memory as `T | null`; the store as `T | undefined`.
    // A null leaking through would have `job.config?.max_iterations` read off
    // null rather than short-circuiting.
    getJobStatusMock.mockResolvedValue(enrichedRecord());
    return loadSelectedJob('job-3').then(() => {
      expect(orchestratorStore.selectedJob?.config).toBeUndefined();
      expect(orchestratorStore.selectedJob?.memory).toBeUndefined();
    });
  });

  it('leaves the placeholder selection alone when the read fails', async () => {
    // The list record without provenance is worse than the enriched one and
    // better than a blank page.
    selectJob(listRecord() as never);
    getJobStatusMock.mockRejectedValue(new Error('503'));
    await loadSelectedJob('job-3');
    expect(orchestratorStore.selectedJob?.job_id).toBe('job-3');
  });

  it('ignores an answer about a different job', async () => {
    // A late answer from a previous route must not select the wrong job.
    selectJob(listRecord() as never);
    getJobStatusMock.mockResolvedValue({ ...enrichedRecord(), job_id: 'job-other' });
    await loadSelectedJob('job-3');
    expect(orchestratorStore.selectedJob?.job_id).toBe('job-3');
    expect(orchestratorStore.selectedJob?.terminal_reason).toBeUndefined();
  });

  it('reads nothing for an empty job id', async () => {
    await loadSelectedJob('');
    expect(getJobStatusMock).not.toHaveBeenCalled();
  });

  it('drops a slow answer for a job the operator has already left', async () => {
    // Two job pages in ONE tenant share a scope, so the tenant check cannot
    // reject this. The route effect reads on every #/jobs/:id, so job A's
    // read can still be in flight when job B's page opens — and landing it
    // would put A's terminal reason, delivery, verification and Readmit
    // control under B's URL.
    let releaseA: (value: unknown) => void = () => {};
    const slowA = new Promise((resolve) => { releaseA = resolve; });
    getJobStatusMock.mockImplementation((_tenant: string, id: string) =>
      id === 'job-a'
        ? slowA
        : Promise.resolve({ ...enrichedRecord(), job_id: 'job-b', terminal_reason: 'completed' }),
    );

    const pendingA = loadSelectedJob('job-a');
    await loadSelectedJob('job-b');
    expect(orchestratorStore.selectedJob?.job_id).toBe('job-b');

    releaseA({ ...enrichedRecord(), job_id: 'job-a', terminal_reason: 'budget_exhausted' });
    await pendingA;

    expect(orchestratorStore.selectedJob?.job_id).toBe('job-b');
    expect(orchestratorStore.selectedJob?.terminal_reason).toBe('completed');
  });

  it('drops an answer whose selection has since been replaced directly', async () => {
    // The row click selects; the read that was already running for the page
    // just left must not undo it.
    let release: (value: unknown) => void = () => {};
    getJobStatusMock.mockImplementation(
      () => new Promise((resolve) => { release = resolve; }),
    );

    const pending = loadSelectedJob('job-3');
    // The read is only in flight once the client has been resolved.
    await vi.waitFor(() => expect(getJobStatusMock).toHaveBeenCalledWith('acme', 'job-3'));
    selectJob({ ...listRecord(), job_id: 'job-9' } as never);

    release(enrichedRecord());
    await pending;

    expect(orchestratorStore.selectedJob?.job_id).toBe('job-9');
    expect(orchestratorStore.selectedJob?.terminal_reason).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// A job that finishes while the operator is watching it
// ---------------------------------------------------------------------------
//
// The event stream carries STATE. jobEventPatch applies the state, the
// iteration and the stage; the delivery, the review rounds, the typed
// terminal reason, the readmit permission, the attempt history and the
// verification exist only on GetJobStatus. The detail view also releases the
// stream the moment the job reads terminal — so without a re-read at that
// transition, an operator watched the job go `failed` and every panel this
// change adds stayed empty until the page was navigated again.

describe('applyJobEvent — the transition into a terminal state', () => {
  beforeEach(() => {
    getJobStatusMock.mockReset();
    listJobsMock.mockReset();
    setActiveTenant('acme');
    selectJob(null);
  });

  /** stateEvent is what the stream sends when a job changes state. */
  function stateEvent(state: string, timestamp: number): Record<string, unknown> {
    return { job_id: 'job-3', type: 'state_change', state, timestamp, iteration: 0 };
  }

  it('re-reads the provenance when the stream says the job stopped', async () => {
    selectJob({ ...listRecord(), state: 'executing' } as never);
    getJobStatusMock.mockResolvedValue(enrichedRecord());

    applyJobEvent(stateEvent('failed', 1) as never);

    await vi.waitFor(() => expect(getJobStatusMock).toHaveBeenCalledWith('acme', 'job-3'));
    await vi.waitFor(() => {
      expect(orchestratorStore.selectedJob?.terminal_reason).toBe('budget_exhausted');
    });
    expect(orchestratorStore.selectedJob?.delivery?.pull_request_number).toBe(57);
    expect(orchestratorStore.selectedJob?.verification).toHaveLength(1);
  });

  it('reads nothing while the job is still running', () => {
    selectJob({ ...listRecord(), state: 'executing' } as never);
    applyJobEvent(stateEvent('reviewing', 1) as never);
    expect(orchestratorStore.selectedJob?.state).toBe('reviewing');
    expect(getJobStatusMock).not.toHaveBeenCalled();
  });

  it('reads once at the transition, not on every event after it', async () => {
    selectJob({ ...listRecord(), state: 'executing' } as never);
    getJobStatusMock.mockResolvedValue(enrichedRecord());

    applyJobEvent(stateEvent('failed', 1) as never);
    await vi.waitFor(() => expect(getJobStatusMock).toHaveBeenCalledTimes(1));

    // A terminal job still emits trailing events; none of them is a new
    // transition, and each re-read would be a request for nothing.
    applyJobEvent(stateEvent('failed', 2) as never);
    await Promise.resolve();
    expect(getJobStatusMock).toHaveBeenCalledTimes(1);
  });

  it('reads nothing for an event about a job that is not the selection', () => {
    selectJob({ ...listRecord(), state: 'executing' } as never);
    applyJobEvent({ ...stateEvent('failed', 1), job_id: 'job-other' } as never);
    expect(orchestratorStore.selectedJob?.state).toBe('executing');
    expect(getJobStatusMock).not.toHaveBeenCalled();
  });
});
