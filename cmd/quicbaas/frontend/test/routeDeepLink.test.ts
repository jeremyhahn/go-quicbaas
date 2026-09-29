/**
 * routeDeepLink.test.ts
 *
 * Regression: opening `#/jobs/:id` directly showed "No job selected".
 *
 * The shell issued GetJobStatus as soon as it mounted, and start-up validation
 * then moved the session scope (the session was adopted and its generation
 * moved) while that read was in flight. loadSelectedJob correctly dropped the
 * answer asked under the old scope — and nothing asked again, because the
 * route effect depended on the route alone. The effect now also reads
 * readyScope(), so the scope change re-runs it under the scope in effect.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { render } from '@testing-library/svelte';

const { getJobStatus, pending } = vi.hoisted(() => {
  const pending: Array<{ jobID: string; resolve: (v: unknown) => void }> = [];
  return {
    pending,
    getJobStatus: vi.fn((_tenantID: string, jobID: string) =>
      new Promise((resolve) => { pending.push({ jobID, resolve }); })),
  };
});

// Every other orchestrator read (the refresh a scope change triggers) answers
// empty; only GetJobStatus is held so the test can land it late.
vi.mock('../src/stores/sdkClient.ts', () => {
  const empty = vi.fn(async () => []);
  const orchestrator = new Proxy({ getJobStatus } as Record<string, unknown>, {
    get: (target, key: string) => target[key] ?? empty,
  });
  const client = new Proxy({ orchestrator } as Record<string, unknown>, {
    get: (target, key: string) =>
      target[key] ?? new Proxy({}, { get: () => empty }),
  });
  return { getSDKClient: vi.fn(async () => client), resetSDKClient: vi.fn() };
});

import JobRouteHarness from './stubs/JobRouteHarness.svelte';
import { setAuthenticated, setUnauthenticated } from '../src/stores/auth.svelte.ts';
import { orchestratorStore, readyScope, resetOrchestratorStore } from '../src/stores/orchestrator.svelte.ts';

let loginSerial = 0;

/** A fresh login for the same principal in the same tenant: the scope moves. */
function signIn(tenantID = 'acme'): void {
  const claims = {
    sub: 'operator@example.test',
    uid: 'usr_1',
    tenant_id: tenantID,
    roles: ['operator'],
    exp: Math.floor(Date.now() / 1000) + 900,
    sid: `sess-${(loginSerial += 1)}`,
  };
  const token = `${btoa('{"alg":"none"}')}.${btoa(JSON.stringify(claims))}.sig`;
  setAuthenticated({ access_token: token, refresh_token: 'r' });
}

function job(jobID: string): Record<string, unknown> {
  return { job_id: jobID, tenant_id: 'acme', state: 'executing', description: jobID, events: [], work_items: [] };
}

async function settle(): Promise<void> {
  for (let i = 0; i < 5; i++) await Promise.resolve();
}

beforeEach(() => {
  pending.length = 0;
  getJobStatus.mockClear();
  resetOrchestratorStore();
});

afterEach(() => {
  setUnauthenticated();
  resetOrchestratorStore();
});

describe('opening #/jobs/:id directly', () => {
  it('asks again when the scope moves under the first read, and shows the job', async () => {
    signIn();
    render(JobRouteHarness, { props: { jobID: 'job-1' } });
    await vi.waitFor(() => { expect(getJobStatus).toHaveBeenCalledTimes(1); });

    // Start-up validation adopts the session: same person, new scope.
    signIn();
    await vi.waitFor(() => { expect(getJobStatus).toHaveBeenCalledTimes(2); });

    // The first answer lands late, under the old scope, and is dropped...
    pending[0]!.resolve(job('job-1'));
    await settle();
    expect(orchestratorStore.selectedJob).toBeNull();

    // ...and the re-asked read is the one that selects the job.
    pending[1]!.resolve(job('job-1'));
    await vi.waitFor(() => { expect(orchestratorStore.selectedJob?.job_id).toBe('job-1'); });
  });

  it('asks nothing before there is a session, then asks once there is', async () => {
    render(JobRouteHarness, { props: { jobID: 'job-1' } });
    await settle();
    expect(readyScope()).toBe('');
    expect(getJobStatus).not.toHaveBeenCalled();

    signIn();
    await vi.waitFor(() => { expect(getJobStatus).toHaveBeenCalledTimes(1); });
    pending[0]!.resolve(job('job-1'));
    await vi.waitFor(() => { expect(orchestratorStore.selectedJob?.job_id).toBe('job-1'); });
  });
});
