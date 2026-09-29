// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// Adopting an issue on the wire: POST /api/v1/sdlc/projects/:id/adopt
// (pkg/api/rest/handlers_sdlc_adopt.go).

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';

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

describe('RestSdlcClient adoptIssue', () => {
  it('posts the issue, the pull request and the ruling to the project adopt route', async () => {
    fetchMock.mockResolvedValue(makeResponse({ task_id: 'task-1', issue: 141, pull_request: 194, branch: 'issue/141-x' }, 201));
    const got = await new RestClient('http://localhost').sdlc.adoptIssue('t1', 'go-luks2', { issue: 141, pull_request: 194, ruling: 'Land it.' });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/sdlc/projects/go-luks2/adopt?tenant_id=t1');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ issue: 141, pull_request: 194, ruling: 'Land it.' });
    expect(got.task_id).toBe('task-1');
    expect(got.branch).toBe('issue/141-x');
  });

  it('rejects when the server refuses the adoption', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'the issue is closed' }, 409));
    await expect(new RestClient('http://localhost').sdlc.adoptIssue('t1', 'p1', { issue: 1 })).rejects.toThrow();
  });
});
