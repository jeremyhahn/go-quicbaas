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
 * loadTaskJobs — one task's attempt history, read from the server.
 *
 * go-quicbaas#822: the task page sieved the tenant-wide job list for its own
 * task's attempts. Measured on the burn-in node, a task with three charged
 * attempts rendered "JOBS (0)", because none of its three jobs was among the
 * seven the tenant list happened to hold. These assert the REQUEST — the
 * narrowing has to travel to the server, which is the only place it can be
 * applied honestly — and the answer's landing.
 */

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';

import { setAuthenticated, setUnauthenticated } from '../../src/stores/auth.svelte.ts';
import { resetSDKClient } from '../../src/stores/sdkClient.ts';
import {
  loadTaskJobs,
  orchestratorStore,
  resetOrchestratorStore,
} from '../../src/stores/orchestrator.svelte.ts';

/** An unsigned JWT carrying the claims a live session bearer carries. */
function sessionToken(tenantID: string): string {
  const b64url = (o: unknown): string =>
    btoa(JSON.stringify(o)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  const claims = {
    sub: 'usr_1',
    tenant_id: tenantID,
    roles: ['operator'],
    sid: 'sid_1',
    exp: Math.floor(Date.now() / 1000) + 3600,
  };
  return `${b64url({ alg: 'none' })}.${b64url(claims)}.sig`;
}

/** Every request URL the SDK issued. */
let requested: string[] = [];

/** The jobs the stubbed server answers with; keyed by the task_id asked for. */
let jobsByTask: Record<string, unknown[]> = {};

/** When set, the stubbed server refuses the listing. */
let refuse = false;

function stubFetch(): void {
  const spy = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url =
      typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    requested.push(url);
    if (refuse) {
      return new Response(JSON.stringify({ error: 'denied' }), {
        status: 403,
        headers: { 'Content-Type': 'application/json' },
      });
    }
    const taskID = new URL(url, window.location.origin).searchParams.get('task_id') ?? '';
    return new Response(JSON.stringify({ jobs: jobsByTask[taskID] ?? [] }), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
    });
  });
  Object.defineProperty(window, 'fetch', { writable: true, configurable: true, value: spy });
}

/** One concluded attempt at `taskID`. */
function attempt(jobID: string, taskID: string, ordinal: number): Record<string, unknown> {
  return {
    job_id: jobID,
    tenant_id: 'burnin',
    task_id: taskID,
    attempt: ordinal,
    state: 'failed',
    started_at: ordinal,
  };
}

/** The job listings the SDK issued, newest last. */
function jobListings(): string[] {
  return requested.filter((u) => u.includes('/api/v1/orchestrator/jobs'));
}

describe('loadTaskJobs', () => {
  beforeEach(() => {
    requested = [];
    jobsByTask = {};
    refuse = false;
    resetOrchestratorStore();
    resetSDKClient();
    setUnauthenticated();
    stubFetch();
    setAuthenticated({ access_token: sessionToken('burnin') });
  });

  afterEach(() => {
    resetOrchestratorStore();
    setUnauthenticated();
    vi.restoreAllMocks();
  });

  it('asks the server for one task, and holds what it answers', async () => {
    jobsByTask['task-a'] = [attempt('job-1', 'task-a', 1), attempt('job-2', 'task-a', 2)];

    await loadTaskJobs('task-a');

    const listings = jobListings();
    expect(listings.length).toBeGreaterThan(0);
    const url = new URL(listings[listings.length - 1]!, window.location.origin);
    expect(url.searchParams.get('task_id')).toBe('task-a');
    expect(url.searchParams.get('tenant_id')).toBe('burnin');
    expect(orchestratorStore.taskJobs.map((j) => j.job_id)).toEqual(['job-1', 'job-2']);
    expect(orchestratorStore.taskJobsTaskID).toBe('task-a');
  });

  it('re-reads for a different task rather than serving the previous answer', async () => {
    jobsByTask['task-a'] = [attempt('job-1', 'task-a', 1)];
    jobsByTask['task-b'] = [attempt('job-9', 'task-b', 1)];

    await loadTaskJobs('task-a');
    await loadTaskJobs('task-b');

    expect(orchestratorStore.taskJobsTaskID).toBe('task-b');
    expect(orchestratorStore.taskJobs.map((j) => j.job_id)).toEqual(['job-9']);
  });

  it('records the task even when the read is refused, so the page can say so', async () => {
    refuse = true;

    await loadTaskJobs('task-a');

    expect(orchestratorStore.taskJobs).toEqual([]);
    expect(orchestratorStore.taskJobsTaskID).toBe('task-a');
  });

  it('answers an empty task id without issuing a request', async () => {
    const before = jobListings().length;

    await loadTaskJobs('');

    expect(jobListings().length).toBe(before);
    expect(orchestratorStore.taskJobsTaskID).toBe('');
  });

  it('drops a departed session’s attempt history', async () => {
    jobsByTask['task-a'] = [attempt('job-1', 'task-a', 1)];
    await loadTaskJobs('task-a');
    expect(orchestratorStore.taskJobs.length).toBe(1);

    // A new principal must not see the previous one's attempts for even one
    // render: the getter gates on scope, so there is no window at all.
    setUnauthenticated();
    expect(orchestratorStore.taskJobs).toEqual([]);
    expect(orchestratorStore.taskJobsTaskID).toBe('');
  });
});
