// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// The SDLC operator verbs on the wire: filing an issue on a bound project's
// repository and listing the collaborators a ruler is picked from
// (pkg/api/rest/handlers_sdlc_operator.go), and the binding's rulers.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest';
import type { ProjectBinding } from '../src/sdlc';

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

describe('RestSdlcClient operator verbs', () => {
  it('files an issue with a POST carrying the filing alone', async () => {
    fetchMock.mockResolvedValue(makeResponse({ number: 88, title: 'Follow-up', labels: ['task:ready'] }, 201));
    const issue = await new RestClient('http://localhost').sdlc.fileIssue('t1', 'p1', {
      title: 'Follow-up',
      labels: ['task:ready'],
      parent: 12,
    });
    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('http://localhost/api/v1/sdlc/projects/p1/issues?tenant_id=t1');
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ title: 'Follow-up', labels: ['task:ready'], parent: 12 });
    expect(issue.number).toBe(88);
  });

  it('surfaces a refused filing as an error', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'sdlc: invalid labels[0]: repository defines no label named nope' }, 400));
    await expect(
      new RestClient('http://localhost').sdlc.fileIssue('t1', 'p1', { title: 't', labels: ['nope'] }),
    ).rejects.toThrow();
  });

  it('lists the collaborators with a GET', async () => {
    fetchMock.mockResolvedValue(makeResponse({ users: [{ login: 'acme' }, { login: 'lead' }], count: 2 }));
    const users = await new RestClient('http://localhost').sdlc.collaborators('t1', 'p/1');
    expect(fetchMock.mock.calls[0][0]).toBe('http://localhost/api/v1/sdlc/projects/p%2F1/collaborators?tenant_id=t1');
    expect(users.users.map((u) => u.login)).toEqual(['acme', 'lead']);
  });

  it('surfaces an unbound project as an error', async () => {
    fetchMock.mockResolvedValue(makeResponse({ error: 'sdlc: record not found' }, 404));
    await expect(new RestClient('http://localhost').sdlc.collaborators('t1', 'p1')).rejects.toThrow();
  });

  it('writes the binding rulers back on a rebind', async () => {
    const binding = { project_id: 'p1', rulers: ['lead'] } as unknown as ProjectBinding;
    fetchMock.mockResolvedValue(makeResponse(binding));
    await new RestClient('http://localhost').sdlc.bind('t1', 'p1', binding);
    expect(JSON.parse(fetchMock.mock.calls[0][1].body).rulers).toEqual(['lead']);
  });
});
