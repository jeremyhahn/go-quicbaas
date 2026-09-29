// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Shape audit for the generic Task model (stage 1a, #552): the fields the
// server writes on `orchestration.Task` and `JobStatus`/`JobConfig` must
// reach the caller of the REST client verbatim, typed. A field the SDK
// silently dropped would render as an em-dash in every view and read as
// "the server did not say", which is not what happened.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import type { JobStatus, ListSessionsResponse, Task, TaskFaultOrigin, TaskStatus } from '../src/types';

function makeResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

// The wire shape as `RespondJSON(w, r, 200, task)` writes it from
// pkg/orchestration/task_types.go — every json tag, nothing renamed.
const wireTask = {
  id: 'task-1',
  tenant_id: 'tenant-a',
  team_name: '',
  subject: 'Add the widget',
  description: 'The issue body.',
  status: 'blocked',
  assigned_to: 'agent-7',
  created_at: 1778017609000000000,
  updated_at: 1778017700000000000,
  version: 3,
  project_id: 'proj-1',
  output_artifacts: [
    { kind: 'pull_request', name: 'PR #7', metadata: { url: 'https://forge.example/o/r/pulls/7' }, ref: { branch: 'issue/42' } },
  ],
  auto_spawn: true,
  spawned_job_id: 'job-1',
  spawned_at: 1778017650000000000,
  source: { kind: 'forge_issue', ref: 'owner/repo#42', url: 'https://forge.example/o/r/issues/42' },
  priority: 3,
  attempts: 3,
  attempt_budget: 5,
  environment_faults: 1,
  consecutive_environment_faults: 1,
  retry_not_before: 1778017800000000000,
  blocked_reason: 'budget',
  block_kind: 'escalated',
  status_changed_at: 1778017700000000000,
  runner: 'sdlc',
  routing: { backend: 'claude', host_affinity: 'gpu-box', gpu_required: true },
  dispatch_reason: 'no online agents satisfy backend/gpu/host_affinity constraints',
  // go-quicbaas#947: why the task stopped, and the budget it ran under.
  question_attempts: 0,
  block_fault_class: '',
  conclusion_reason: 'abandoned',
  charged_attempts: 2,
  effective_budget: 5,
  fault_class: '',
  fault_origin: 'undiagnosed',
};

describe('RestOrchestratorClient.getTask — generic Task fields', () => {
  it('passes every stage-1a field through verbatim', async () => {
    fetchMock.mockResolvedValue(makeResponse(wireTask));
    const client = new RestClient('http://localhost');
    const task: Task = await client.orchestrator.getTask('tenant-a', 'task-1');

    expect(task.source).toEqual({ kind: 'forge_issue', ref: 'owner/repo#42', url: 'https://forge.example/o/r/issues/42' });
    expect(task.priority).toBe(3);
    expect(task.attempts).toBe(3);
    expect(task.attempt_budget).toBe(5);
    expect(task.environment_faults).toBe(1);
    expect(task.consecutive_environment_faults).toBe(1);
    expect(task.retry_not_before).toBe(1778017800000000000);
    expect(task.blocked_reason).toBe('budget');
    expect(task.block_kind).toBe('escalated');
    expect(task.spawned_job_id).toBe('job-1');
    expect(task.project_id).toBe('proj-1');
    expect(task.status_changed_at).toBe(1778017700000000000);
    expect(task.output_artifacts).toHaveLength(1);
    expect(task.output_artifacts?.[0]?.kind).toBe('pull_request');
    expect(task.output_artifacts?.[0]?.metadata?.['url']).toBe('https://forge.example/o/r/pulls/7');
    expect(task.output_artifacts?.[0]?.ref?.branch).toBe('issue/42');

    expect(task.runner).toBe('sdlc');
    expect(task.routing).toEqual({ backend: 'claude', host_affinity: 'gpu-box', gpu_required: true });
    expect(task.dispatch_reason).toBe('no online agents satisfy backend/gpu/host_affinity constraints');

    expect(task.question_attempts).toBe(0);
    expect(task.block_fault_class).toBe('');
    expect(task.conclusion_reason).toBe('abandoned');
    expect(task.charged_attempts).toBe(2);
    expect(task.effective_budget).toBe(5);
    expect(task.fault_class).toBe('');
    const origin: TaskFaultOrigin | undefined = task.fault_origin;
    expect(origin).toBe('undiagnosed');

    const blocked: TaskStatus = 'blocked';
    expect(task.status).toBe(blocked);
  });

  it('leaves the fields absent — not zeroed — when the server omits them', async () => {
    const { source: _s, priority: _p, attempts: _a, attempt_budget: _b, environment_faults: _e,
      consecutive_environment_faults: _c, retry_not_before: _r, blocked_reason: _br, block_kind: _bk,
      output_artifacts: _oa, status_changed_at: _sc, runner: _rn, routing: _rt,
      dispatch_reason: _dr, ...bare } = wireTask;
    fetchMock.mockResolvedValue(makeResponse({ ...bare, status: 'pending' }));
    const client = new RestClient('http://localhost');
    const task = await client.orchestrator.getTask('tenant-a', 'task-1');

    expect(task.source).toBeUndefined();
    expect(task.attempts).toBeUndefined();
    expect(task.attempt_budget).toBeUndefined();
    expect(task.retry_not_before).toBeUndefined();
    expect(task.blocked_reason).toBeUndefined();
    expect(task.output_artifacts).toBeUndefined();
    expect(task.runner).toBeUndefined();
    expect(task.routing).toBeUndefined();
    expect(task.dispatch_reason).toBeUndefined();
  });

  it('keeps the fields through the listTasks envelope', async () => {
    fetchMock.mockResolvedValue(makeResponse({ tasks: [wireTask] }));
    const client = new RestClient('http://localhost');
    const tasks = await client.orchestrator.listTasks('tenant-a');
    expect(tasks).toHaveLength(1);
    expect(tasks[0]?.source?.ref).toBe('owner/repo#42');
    expect(tasks[0]?.attempt_budget).toBe(5);
    expect(tasks[0]?.spawned_job_id).toBe('job-1');
    expect(tasks[0]?.runner).toBe('sdlc');
    expect(tasks[0]?.routing?.host_affinity).toBe('gpu-box');
  });
});

describe('RestOrchestratorClient.getJobStatus — task binding', () => {
  it('passes task_id and attempt through on the status and its config', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      job_id: 'job-1',
      tenant_id: 'tenant-a',
      state: 'executing',
      iteration: 2,
      task_id: 'task-1',
      attempt: 2,
      config: { tenant_id: 'tenant-a', description: 'Add the widget', task_id: 'task-1', attempt: 2 },
    }));
    const client = new RestClient('http://localhost');
    const job: JobStatus = await client.orchestrator.getJobStatus('tenant-a', 'job-1');
    expect(job.task_id).toBe('task-1');
    expect(job.attempt).toBe(2);
    expect(job.config?.task_id).toBe('task-1');
    expect(job.config?.attempt).toBe(2);
  });

  it('leaves task_id and attempt absent for a lead-driven job', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      job_id: 'job-2',
      tenant_id: 'tenant-a',
      state: 'executing',
      iteration: 1,
      config: { tenant_id: 'tenant-a', description: 'lead-driven' },
    }));
    const client = new RestClient('http://localhost');
    const job = await client.orchestrator.getJobStatus('tenant-a', 'job-2');
    expect(job.task_id).toBeUndefined();
    expect(job.config?.task_id).toBeUndefined();
  });
});

// The sessions a job holds, the stage it is in, and why it failed. The Job
// detail view chips each session and links it; a dropped field renders as
// "this job spawned nothing", which is not what the server said.
describe('RestOrchestratorClient.getJobStatus — sessions, stage and failure class', () => {
  it('passes the session bindings, stage and failure class through verbatim', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      job_id: 'job-1',
      tenant_id: 'tenant-a',
      state: 'failed',
      iteration: 3,
      lead_session_id: 'sess-lead-1',
      session_ids: ['sess-lead-1', 'sess-worker-2', 'sess-reviewer-3'],
      stage: 'validate',
      failure_class: 'environment',
    }));
    const client = new RestClient('http://localhost');
    const job: JobStatus = await client.orchestrator.getJobStatus('tenant-a', 'job-1');

    expect(job.lead_session_id).toBe('sess-lead-1');
    expect(job.session_ids).toEqual(['sess-lead-1', 'sess-worker-2', 'sess-reviewer-3']);
    expect(job.stage).toBe('validate');
    expect(job.failure_class).toBe('environment');
  });

  it('leaves them absent — not zeroed — for a job that has spawned nothing', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      job_id: 'job-2',
      tenant_id: 'tenant-a',
      state: 'assigning',
      iteration: 0,
    }));
    const client = new RestClient('http://localhost');
    const job = await client.orchestrator.getJobStatus('tenant-a', 'job-2');

    expect(job.lead_session_id).toBeUndefined();
    expect(job.session_ids).toBeUndefined();
    expect(job.stage).toBeUndefined();
    expect(job.failure_class).toBeUndefined();
  });
});

// The session shape the REST service actually writes: orchestration.SessionInfo,
// which names the lifecycle field `state` and the activity instant
// `last_activity`. This mock previously used the SDK's own field names, so it
// asserted the client against itself and could never have caught the rename
// the client was not doing (go-quicbaas#855).
describe('RestOrchestratorClient.listSessions — the server\'s field names, renamed once', () => {
  it('renames state → status and last_activity → last_active', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      sessions: [
        {
          session_id: 'sess-1',
          project_id: 'proj-1',
          backend: 'claude',
          model: 'opus',
          state: 'active',
          created_at: 1789185149_000000000,
          last_activity: 1789185263_664211875,
        },
      ],
    }));
    const client = new RestClient('http://localhost');
    const resp: ListSessionsResponse = await client.orchestrator.listSessions({ tenantID: 'tenant-a' });

    expect(resp.sessions).toHaveLength(1);
    expect(resp.sessions[0]?.status).toBe('active');
    expect(resp.sessions[0]?.last_active).toBe(1789185263_664211875);
    expect(resp.sessions[0]?.created_at).toBe(1789185149_000000000);
    expect(resp.count).toBe(1);
  });

  // THE UNIT, pinned on the client side too. The live record that opened #855
  // carried created_at in seconds and last_activity in nanoseconds, and the
  // browser's magnitude-detecting formatter rendered both plausibly. Both
  // instants of one session describe moments a couple of minutes apart, so
  // read in the one unit the server now promises they must be that far apart
  // — not five decades.
  it('hands both instants over in one unit', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      sessions: [{
        session_id: 'sess-1',
        backend: 'claude',
        state: 'active',
        created_at: 1789185149_000000000,
        last_activity: 1789185263_664211875,
      }],
    }));
    const client = new RestClient('http://localhost');
    const resp = await client.orchestrator.listSessions({ tenantID: 'tenant-a' });

    const created = resp.sessions[0]?.created_at ?? 0;
    const active = resp.sessions[0]?.last_active ?? 0;
    const elapsedSeconds = (active - created) / 1e9;
    expect(elapsedSeconds).toBeGreaterThan(0);
    expect(elapsedSeconds).toBeLessThan(600);
  });

  it('passes job_id through, and leaves it absent for a session no job spawned', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      sessions: [
        { session_id: 'sess-1', backend: 'claude', state: 'active', created_at: 1, last_activity: 2, job_id: 'job-1' },
        { session_id: 'sess-2', backend: 'claude', state: 'active', created_at: 1, last_activity: 2 },
      ],
    }));
    const client = new RestClient('http://localhost');
    const resp: ListSessionsResponse = await client.orchestrator.listSessions({ tenantID: 'tenant-a' });

    expect(resp.sessions[0]?.job_id).toBe('job-1');
    // A session an operator opened directly belongs to no job, and says so by
    // omission rather than by an empty string.
    expect(resp.sessions[1]?.job_id).toBeUndefined();
  });

  it('answers an empty listing with no sessions rather than throwing', async () => {
    fetchMock.mockResolvedValue(makeResponse({ sessions: [] }));
    const client = new RestClient('http://localhost');
    const resp = await client.orchestrator.listSessions({ tenantID: 'tenant-a' });
    expect(resp.sessions).toEqual([]);
    expect(resp.count).toBe(0);
  });

  it('refuses a listing with no tenant rather than asking for every tenant\'s sessions', async () => {
    const client = new RestClient('http://localhost');
    await expect(client.orchestrator.listSessions({ tenantID: '' })).rejects.toThrow(/tenantID is required/);
  });
});
