// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// The FACETED half of the list contract: the narrowings a filter bar sends,
// and the counts it renders beside each choice.
//
// Each is held to two properties:
//
//   1. the client sends the filter under the name the SERVER reads — a
//      parameter spelled `agentId` here is a filter that silently does
//      nothing, and an empty table is how you would find out;
//   2. the counts survive the round trip as numbers, and an answer that
//      carries none leaves `facets` undefined rather than inventing an empty
//      block a bar would render as "0 of everything".

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';
import { adoptPage, wholeSetPage } from '../src/page.js';

// mockOK is the fetch Response-alike the REST tests use: both .json() and
// .text() answer, since fetchJSON reads the body as text.
function mockOK(payload: unknown): {
  ok: boolean;
  status: number;
  json: () => Promise<unknown>;
  text: () => Promise<string>;
} {
  const body = JSON.stringify(payload);
  return {
    ok: true,
    status: 200,
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

// queryOf returns the query string the client sent on its last call.
function queryOf(fetchMock: ReturnType<typeof vi.fn>): URLSearchParams {
  const url = String(fetchMock.mock.calls[0]?.[0] ?? '');
  const query = url.includes('?') ? url.slice(url.indexOf('?') + 1) : '';
  return new URLSearchParams(query);
}

describe('adoptPage facets', () => {
  it('reads the counts block as numbers', () => {
    const page = adoptPage<{ id: string }>(
      {
        tasks: [{ id: 't1' }],
        total: 3,
        limit: 25,
        offset: 0,
        facets: {
          status: { pending: 12, completed: 30 },
          agent_id: { 'agent-1': 2 },
        },
      },
      'tasks',
    );
    expect(page.facets?.['status']).toEqual({ pending: 12, completed: 30 });
    expect(page.facets?.['agent_id']?.['agent-1']).toBe(2);
  });

  it('leaves facets undefined when the server sent none', () => {
    const page = adoptPage<{ id: string }>({ tasks: [], total: 0, limit: 25, offset: 0 }, 'tasks');
    expect(page.facets).toBeUndefined();
  });

  it('drops a count that is not a number rather than rendering undefined', () => {
    const page = adoptPage<{ id: string }>(
      {
        tasks: [],
        total: 0,
        limit: 25,
        offset: 0,
        facets: { status: { pending: 4, blocked: 'lots', ignored: null }, junk: 'not a map' },
      },
      'tasks',
    );
    expect(page.facets?.['status']).toEqual({ pending: 4 });
    expect(page.facets?.['junk']).toBeUndefined();
  });

  it('reports a transport that cannot page as carrying no counts', () => {
    expect(wholeSetPage([{ id: 't1' }]).facets).toBeUndefined();
  });
});

describe('RestClient faceted filters', () => {
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('sends the tasks listing every narrowing under the server\'s spelling', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ tasks: [], total: 0, limit: 25, offset: 0 }));
    const client = new RestClient('https://q');

    await client.orchestrator.listTasksPage('acme', {
      statuses: ['blocked', 'pending'],
      project_id: 'proj-a',
      agent_id: 'agent-1',
      job_id: 'job-1',
      auto_spawn: true,
      q: 'filter bar',
      sort: 'attempts:desc',
      limit: 10,
    });

    const query = queryOf(fetchMock);
    expect(query.get('statuses')).toBe('blocked,pending');
    expect(query.get('project_id')).toBe('proj-a');
    expect(query.get('agent_id')).toBe('agent-1');
    expect(query.get('job_id')).toBe('job-1');
    expect(query.get('auto_spawn')).toBe('true');
    expect(query.get('q')).toBe('filter bar');
    expect(query.get('sort')).toBe('attempts:desc');
    expect(query.get('limit')).toBe('10');
  });

  it('sends the jobs listing its agent narrowing beside the others', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ jobs: [], total: 0, limit: 25, offset: 0 }));
    const client = new RestClient('https://q');

    await client.orchestrator.listJobsPage('acme', {
      agent_id: 'agent-2',
      runner: 'sdlc',
      statuses: ['failed'],
      project_id: 'proj-b',
    });

    const query = queryOf(fetchMock);
    expect(query.get('agent_id')).toBe('agent-2');
    expect(query.get('runner')).toBe('sdlc');
    expect(query.get('statuses')).toBe('failed');
    expect(query.get('project_id')).toBe('proj-b');
  });

  it('sends the sessions listing the four narrowings it gained', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ sessions: [], total: 0, limit: 25, offset: 0 }));
    const client = new RestClient('https://q');

    await client.orchestrator.listSessionsPage('acme', {
      backend: 'claude',
      status: 'active',
      project_id: 'proj-a',
      job_id: 'job-1',
      runner: 'review',
      agent_id: 'agent-1',
    });

    const query = queryOf(fetchMock);
    expect(query.get('backend')).toBe('claude');
    expect(query.get('status')).toBe('active');
    expect(query.get('project_id')).toBe('proj-a');
    expect(query.get('job_id')).toBe('job-1');
    expect(query.get('runner')).toBe('review');
    expect(query.get('agent_id')).toBe('agent-1');
  });

  it('sends the workflows and projects narrowings', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ workflows: [], total: 0, limit: 25, offset: 0 }));
    const client = new RestClient('https://q');
    await client.orchestrator.listWorkflowsPage('acme', { project_id: 'proj-a', mode: 'canned' });
    let query = queryOf(fetchMock);
    expect(query.get('project_id')).toBe('proj-a');
    expect(query.get('mode')).toBe('canned');

    fetchMock.mockReset();
    fetchMock.mockResolvedValueOnce(mockOK({ projects: [], total: 0, limit: 25, offset: 0 }));
    await client.projects.listProjectsPage('acme', { autonomous: false });
    query = queryOf(fetchMock);
    // FALSE IS SENT, not omitted: `autonomous` is tri-state, and dropping the
    // false case would turn "show me the supervised projects" into "show me
    // every project".
    expect(query.get('autonomous')).toBe('false');
  });

  it('sends the accounts status narrowing beside the provider', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ accounts: [], total: 0, limit: 25, offset: 0 }));
    const client = new RestClient('https://q');

    await client.accounts.listAccountsPage({ tenant_id: 'acme', provider: 'claude', status: 'revoked' });

    const query = queryOf(fetchMock);
    expect(query.get('provider')).toBe('claude');
    expect(query.get('status')).toBe('revoked');
  });

  it('hands the counts back to the caller', async () => {
    fetchMock.mockResolvedValueOnce(
      mockOK({
        tasks: [],
        total: 0,
        limit: 25,
        offset: 0,
        facets: { status: { blocked: 7 }, agent_id: { 'agent-1': 3 } },
      }),
    );
    const client = new RestClient('https://q');

    const page = await client.orchestrator.listTasksPage('acme', { statuses: ['blocked'] });

    expect(page.facets?.['status']?.['blocked']).toBe(7);
    expect(page.facets?.['agent_id']?.['agent-1']).toBe(3);
  });

  it('omits a filter the caller did not set', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ tasks: [], total: 0, limit: 25, offset: 0 }));
    const client = new RestClient('https://q');

    await client.orchestrator.listTasksPage('acme');

    const query = queryOf(fetchMock);
    for (const name of ['statuses', 'project_id', 'agent_id', 'job_id', 'auto_spawn', 'limit']) {
      expect(query.has(name)).toBe(false);
    }
  });
});
