// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// A project's open work on the wire: GET
// /api/v1/sdlc/projects/{projectID}/open-work (pkg/api/rest/handlers_sdlc.go).

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

describe('RestSdlcClient openWork', () => {
  it('reads the project open work with a GET in the caller tenant', async () => {
    fetchMock.mockResolvedValue(makeResponse({
      project_id: 'go-luks2',
      repo: 'jeremyhahn/go-luks2',
      agent: 'agent-luks2',
      pull_requests: [{ number: 210, title: 'fix', head_ref: 'issue/209-fix', head_sha: 'abc', ci_state: 'success',
        review_state: 'approved', created: '2026-09-22T00:00:00Z', age_seconds: 3600 }],
      issues: [{ number: 141, title: 'coverage gate', labels: ['factory:escalated'], created: '2026-09-20T00:00:00Z', age_seconds: 7200 }],
      read_at: '2026-09-25T00:00:00Z',
    }));
    const work = await new RestClient('http://localhost').sdlc.openWork('t1', 'go luks2');
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/sdlc/projects/go%20luks2/open-work?tenant_id=t1');
    expect(init?.method ?? 'GET').toBe('GET');
    expect(work.agent).toBe('agent-luks2');
    expect(work.pull_requests[0].review_state).toBe('approved');
    expect(work.issues[0].labels).toEqual(['factory:escalated']);
  });

  it('surfaces an unbound project as an error rather than empty work', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'not found' }, 404));
    await expect(new RestClient('http://localhost').sdlc.openWork('t1', 'p1')).rejects.toThrow();
  });
});
