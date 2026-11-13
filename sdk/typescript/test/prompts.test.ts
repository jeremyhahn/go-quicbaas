// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Tests for RestPromptsClient (client.prompts): each method's URL, HTTP
// method, query and body against /api/v1/prompts/*.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

function jsonResponse(status: number, body: unknown) {
  const text = JSON.stringify(body);
  return {
    ok: status >= 200 && status < 300,
    status,
    statusText: status === 409 ? 'Conflict' : 'OK',
    headers: new Headers({ 'content-type': 'application/json' }),
    json: async () => JSON.parse(text),
    text: async () => text,
  };
}

beforeEach(() => {
  fetchMock = vi.fn(async () => jsonResponse(200, { set: 'agents', name: 'x' }));
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

function lastCall(): { url: string; method: string; body: unknown } {
  const [url, init] = fetchMock.mock.calls[fetchMock.mock.calls.length - 1] as [string, RequestInit | undefined];
  return {
    url: String(url),
    method: (init?.method ?? 'GET').toUpperCase(),
    body: init?.body === undefined ? undefined : JSON.parse(String(init.body)),
  };
}

const B = 'http://localhost/api/v1/prompts';

describe('RestPromptsClient', () => {
  const client = () => new RestClient('http://localhost');

  it('listPrompts GETs the set with no query when no option is set', async () => {
    await client().prompts.listPrompts('agents');
    const c = lastCall();
    expect(c.url).toBe(`${B}/agents`);
    expect(c.method).toBe('GET');
    expect(c.body).toBeUndefined();
  });

  it('a job status carries the prompt versions the job ran with', async () => {
    const ref = { set: 'sdlc', name: 'worker_charter', scope: 'project', scope_id: 'p1', version: 1, hash: 'abcdef0123' };
    fetchMock.mockImplementationOnce(async () => jsonResponse(200, { job_id: 'j1', prompts: [ref] }));
    const job = await client().orchestrator.getJobStatus('t1', 'j1');
    expect(job.prompts).toEqual([ref]);
  });

  it('listPrompts carries scope and project_id', async () => {
    await client().prompts.listPrompts('agents', { scope: 'project', project_id: 'p1' });
    expect(lastCall().url).toBe(`${B}/agents?scope=project&project_id=p1`);
  });

  it('getPrompt encodes a name containing a slash and carries version', async () => {
    await client().prompts.getPrompt('agents', 'a/b', { tenant_id: 't1', version: 3 });
    const c = lastCall();
    expect(c.url).toBe(`${B}/agents/a%2Fb?tenant_id=t1&version=3`);
    expect(c.method).toBe('GET');
  });

  it('getPrompt omits the query when no option is set', async () => {
    await client().prompts.getPrompt('agents', 'planner');
    expect(lastCall().url).toBe(`${B}/agents/planner`);
  });

  it('putPrompt PUTs the body', async () => {
    await client().prompts.putPrompt(
      'agents',
      'a/b',
      { content: 'hi', note: 'n', expected_version: 2 },
      { scope: 'tenant' },
    );
    const c = lastCall();
    expect(c.url).toBe(`${B}/agents/a%2Fb?scope=tenant`);
    expect(c.method).toBe('PUT');
    expect(c.body).toEqual({ content: 'hi', note: 'n', expected_version: 2 });
  });

  it('resetPrompt DELETEs with expected_version and note in the query', async () => {
    await client().prompts.resetPrompt('agents', 'a/b', { scope: 'project', project_id: 'p1', expected_version: 4, note: 'back' });
    const c = lastCall();
    expect(c.url).toBe(`${B}/agents/a%2Fb?scope=project&project_id=p1&expected_version=4&note=back`);
    expect(c.method).toBe('DELETE');
    expect(c.body).toBeUndefined();
  });

  it('getEffectivePrompt GETs /effective', async () => {
    await client().prompts.getEffectivePrompt('agents', 'a/b');
    const c = lastCall();
    expect(c.url).toBe(`${B}/agents/a%2Fb/effective`);
    expect(c.method).toBe('GET');
  });

  it('diffPrompt GETs /diff', async () => {
    await client().prompts.diffPrompt('agents', 'planner', { scope: 'system' });
    const c = lastCall();
    expect(c.url).toBe(`${B}/agents/planner/diff?scope=system`);
    expect(c.method).toBe('GET');
  });

  it('getPromptHistory GETs /history', async () => {
    await client().prompts.getPromptHistory('agents', 'a/b');
    const c = lastCall();
    expect(c.url).toBe(`${B}/agents/a%2Fb/history`);
    expect(c.method).toBe('GET');
  });

  it('rollbackPrompt POSTs the body to /rollback', async () => {
    await client().prompts.rollbackPrompt('agents', 'a/b', { to_version: 1, note: 'r', expected_version: 5 });
    const c = lastCall();
    expect(c.url).toBe(`${B}/agents/a%2Fb/rollback`);
    expect(c.method).toBe('POST');
    expect(c.body).toEqual({ to_version: 1, note: 'r', expected_version: 5 });
  });

  it('a 409 rejects', async () => {
    fetchMock.mockResolvedValueOnce(jsonResponse(409, { error: 'version conflict' }));
    await expect(
      client().prompts.putPrompt('agents', 'planner', { content: 'x', expected_version: 1 }),
    ).rejects.toThrow();
  });
});
