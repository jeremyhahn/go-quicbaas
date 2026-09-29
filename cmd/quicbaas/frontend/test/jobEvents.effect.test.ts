/**
 * jobEvents.effect.test.ts
 *
 * Regression: pinning a job's stream from a component $effect (JobDetailView)
 * must settle. startEventStream READ the store's reactive `activeJobID` and
 * then WROTE it; called from an effect, that read made the effect depend on
 * the value it had just written, and the effect's own cleanup
 * (stopEventStream) reset it — so every run scheduled another, until Svelte
 * aborted with effect_update_depth_exceeded and the whole app stopped
 * responding to navigation.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import { flushSync, tick } from 'svelte';

const { subscribeJobEvents } = vi.hoisted(() => ({
  subscribeJobEvents: vi.fn((_tenantID: string, _jobID: string, _onEvent: unknown, _onError?: unknown) => vi.fn()),
}));

vi.mock('../src/stores/sdkClient.ts', () => ({
  getSDKClient: vi.fn(async () => ({ orchestrator: { subscribeJobEvents, listJobs: vi.fn(async () => []) } })),
}));

import JobStreamHarness from './stubs/JobStreamHarness.svelte';
import {
  eventSubStore,
  retryEventStream,
  stopAllJobStreams,
  syncActiveJobStreams,
} from '../src/stores/job-events.svelte.ts';
import type { JobStatus } from '../src/stores/orchestrator.svelte.ts';

let errors: unknown[] = [];
const onError = (e: ErrorEvent): void => { errors.push(e.error ?? e.message); };

beforeEach(() => {
  errors = [];
  subscribeJobEvents.mockClear();
  window.addEventListener('error', onError);
});

afterEach(() => {
  window.removeEventListener('error', onError);
  stopAllJobStreams();
});

describe('pinning a job stream from an effect', () => {
  it('opens one stream and settles instead of looping', async () => {
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});
    expect(() => {
      render(JobStreamHarness, { props: { tenantID: 'acme', jobID: 'job-1' } });
      flushSync();
    }).not.toThrow();
    await vi.waitFor(() => { expect(subscribeJobEvents).toHaveBeenCalledTimes(1); });
    await tick();

    expect(eventSubStore.activeJobID).toBe('job-1');
    const loops = consoleError.mock.calls.filter((c) => String(c[0]).includes('effect_update_depth_exceeded'));
    expect(loops).toEqual([]);
    expect(errors).toEqual([]);
    consoleError.mockRestore();
  });

  it('moves the pin when the view switches jobs, and releases it on unmount', async () => {
    const view = render(JobStreamHarness, { props: { tenantID: 'acme', jobID: 'job-1' } });
    await vi.waitFor(() => { expect(subscribeJobEvents).toHaveBeenCalledTimes(1); });

    await view.rerender({ tenantID: 'acme', jobID: 'job-2' });
    await vi.waitFor(() => { expect(subscribeJobEvents).toHaveBeenCalledTimes(2); });
    expect(eventSubStore.activeJobID).toBe('job-2');

    view.unmount();
    expect(eventSubStore.activeJobID).toBe('');
  });
});

describe('a job stream that errors', () => {
  function failLatest(): void {
    const call = subscribeJobEvents.mock.calls.at(-1);
    const onStreamError = call?.[3] as (() => void) | undefined;
    if (onStreamError === undefined) throw new Error('subscribeJobEvents was not given an onError callback');
    onStreamError();
  }

  it('settles as "live updates unavailable" and is not reopened by the effect or a sync', async () => {
    render(JobStreamHarness, { props: { tenantID: 'acme', jobID: 'job-1' } });
    await vi.waitFor(() => { expect(subscribeJobEvents).toHaveBeenCalledTimes(1); });

    failLatest();
    flushSync();
    expect(eventSubStore.liveUpdatesUnavailable('job-1')).toBe(true);

    // The shell's poll keeps asking for the running job; a failed stream is
    // a tombstone, so neither the sync nor the pinned view dials it again.
    syncActiveJobStreams('acme', [{ job_id: 'job-1', state: 'executing' } as JobStatus]);
    syncActiveJobStreams('acme', [{ job_id: 'job-1', state: 'executing' } as JobStatus]);
    await tick();
    expect(subscribeJobEvents).toHaveBeenCalledTimes(1);
  });

  it('reopens only when the operator retries, and clears the notice', async () => {
    render(JobStreamHarness, { props: { tenantID: 'acme', jobID: 'job-1' } });
    await vi.waitFor(() => { expect(subscribeJobEvents).toHaveBeenCalledTimes(1); });
    failLatest();
    expect(eventSubStore.liveUpdatesUnavailable('job-1')).toBe(true);

    retryEventStream('acme', 'job-1');
    expect(eventSubStore.liveUpdatesUnavailable('job-1')).toBe(false);
    await vi.waitFor(() => { expect(subscribeJobEvents).toHaveBeenCalledTimes(2); });
  });

  it('forgets the failure once the view lets the job go', async () => {
    const view = render(JobStreamHarness, { props: { tenantID: 'acme', jobID: 'job-1' } });
    await vi.waitFor(() => { expect(subscribeJobEvents).toHaveBeenCalledTimes(1); });
    failLatest();

    view.unmount();
    expect(eventSubStore.liveUpdatesUnavailable('job-1')).toBe(false);
  });

  it('ignores a retry for a stream that did not fail', async () => {
    render(JobStreamHarness, { props: { tenantID: 'acme', jobID: 'job-1' } });
    await vi.waitFor(() => { expect(subscribeJobEvents).toHaveBeenCalledTimes(1); });

    retryEventStream('acme', 'job-1');
    await tick();
    expect(subscribeJobEvents).toHaveBeenCalledTimes(1);
  });
});
