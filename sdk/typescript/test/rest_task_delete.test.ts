// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestOrchestratorClient.deleteTask (go-quicbaas#612).
//
// The verb removes a task and, with it, the task's source reservation, so the
// forge issue it was adopted from can be adopted again. A task a running job
// holds a lease on answers 409, which must surface as an APIError rather than
// a silent success.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function makeOK(payload: unknown = {}): Response {
  return new Response(JSON.stringify(payload), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
}

function makeError(status: number, message = 'error'): Response {
  return new Response(JSON.stringify({ error: message }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

describe('RestOrchestratorClient.deleteTask', () => {
  it('DELETEs /api/v1/orchestrator/tasks/:taskID and returns the response', async () => {
    fetchMock.mockResolvedValue(makeOK({ task_id: 'task-1', deleted: true }));

    const client = new RestClient('http://localhost:8080');
    const resp = await client.orchestrator.deleteTask({
      tenant_id: 'automatethethings',
      task_id: 'task-1',
    });

    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe(
      'http://localhost:8080/api/v1/orchestrator/tasks/task-1?tenant_id=automatethethings',
    );
    expect(init.method).toBe('DELETE');
    expect(resp.deleted).toBe(true);
    expect(resp.task_id).toBe('task-1');
  });

  it('URL-encodes the task ID and the tenant', async () => {
    fetchMock.mockResolvedValue(makeOK({ task_id: 'a/b', deleted: true }));

    const client = new RestClient('http://localhost:8080');
    await client.orchestrator.deleteTask({ tenant_id: 'my tenant', task_id: 'a/b' });

    const [url] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toContain('/tasks/a%2Fb');
    expect(new URL(url).searchParams.get('tenant_id')).toBe('my tenant');
  });

  it('throws APIError on 404 (no such task)', async () => {
    const { APIError } = await import('../src/errors');
    fetchMock.mockResolvedValue(makeError(404, 'task not found'));

    const client = new RestClient('http://localhost:8080');
    await expect(
      client.orchestrator.deleteTask({ tenant_id: 't1', task_id: 'missing' }),
    ).rejects.toBeInstanceOf(APIError);
  });

  it('throws APIError on 409 (a running job holds the task)', async () => {
    const { APIError } = await import('../src/errors');
    fetchMock.mockResolvedValue(makeError(409, 'task is leased by a running job'));

    const client = new RestClient('http://localhost:8080');
    await expect(
      client.orchestrator.deleteTask({ tenant_id: 't1', task_id: 'task-1' }),
    ).rejects.toBeInstanceOf(APIError);
  });
});
