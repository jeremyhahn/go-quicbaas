// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// The paged listings, on every transport that has one.
//
// Each transport is held to the same three properties:
//
//   1. it sends what the caller asked for — the four shared parameters and
//      the endpoint's own narrowings, spelled the way the server reads them;
//   2. it hands back the envelope, so `total` is the size of the collection
//      and not the size of the page;
//   3. it refuses an answer that is not a listing, rather than reporting an
//      empty page — "the server sent nothing usable" and "there are none" are
//      different answers and a table renders the second as a fact.
//
// A transport that cannot page (gRPC-Web carries no paging fields; quic, unix
// and mcp are not wired to anything yet) must still satisfy the interface
// honestly: it reports the whole set as ONE page, or refuses through its own
// unpaged sibling. It must never invent a page it did not fetch.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { RestClient } from '../src/rest.js';
import { WailsClient } from '../src/wails.js';
import { GrpcWebClient, GrpcWebNotImplementedError } from '../src/grpc_web.js';
import { QuicClient, QuicNotImplementedError } from '../src/quic.js';
import { UnixClient, UnixNotImplementedError } from '../src/unix.js';
import { McpClient, McpNotImplementedError } from '../src/mcp.js';
import { MalformedPageError, adoptPage, wholeSetPage } from '../src/page.js';

// mockOK is the fetch Response-alike the other REST tests use: both .json()
// and .text() answer, since fetchJSON reads the body as text.
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

function mockError(status: number, message: string): {
  ok: boolean;
  status: number;
  statusText: string;
  json: () => Promise<unknown>;
  text: () => Promise<string>;
} {
  const body = JSON.stringify({ error: message });
  return {
    ok: false,
    status,
    statusText: 'Bad Request',
    json: async () => JSON.parse(body) as unknown,
    text: async () => body,
  };
}

const JOB = {
  job_id: 'job-1',
  tenant_id: 'acme',
  state: 'executing',
  iteration: 1,
  task_count: 2,
  completed: 1,
  failed: 0,
  description: 'a job',
  started_at: 1,
  updated_at: 2,
};

// =============================================================================
// The envelope itself
// =============================================================================

describe('adoptPage', () => {
  it('reads a paged envelope as the page the server cut', () => {
    const page = adoptPage<typeof JOB>(
      { jobs: [JOB], total: 1987, limit: 25, offset: 50, next_offset: 75 },
      'jobs',
    );
    expect(page.items).toHaveLength(1);
    expect(page.total).toBe(1987);
    expect(page.limit).toBe(25);
    expect(page.offset).toBe(50);
    expect(page.next_offset).toBe(75);
  });

  it('omits next_offset on the last page rather than computing one', () => {
    const page = adoptPage<typeof JOB>({ jobs: [JOB], total: 1, limit: 25, offset: 0 }, 'jobs');
    expect(page.next_offset).toBeUndefined();
  });

  it('reports an unpaged answer as one whole page', () => {
    // A daemon older than this SDK: the records, no envelope.
    const wrapped = adoptPage<typeof JOB>({ jobs: [JOB, JOB] }, 'jobs');
    expect(wrapped.total).toBe(2);
    expect(wrapped.next_offset).toBeUndefined();

    const bare = adoptPage<typeof JOB>([JOB], 'jobs');
    expect(bare.total).toBe(1);
  });

  it('converts each record when the wire shape differs from the SDK\'s', () => {
    const page = adoptPage<{ id: string }>(
      { sessions: [{ session_id: 's-1' }], total: 1, limit: 25, offset: 0 },
      'sessions',
      (wire: never) => ({ id: (wire as { session_id: string }).session_id }),
    );
    expect(page.items[0]).toEqual({ id: 's-1' });
  });

  it('refuses an answer that is not a listing', () => {
    expect(() => adoptPage(null, 'jobs')).toThrow(MalformedPageError);
    expect(() => adoptPage('nope', 'jobs')).toThrow(MalformedPageError);
    expect(() => adoptPage({ error: 'boom' }, 'jobs')).toThrow(MalformedPageError);
    expect(() => adoptPage({ jobs: 'not an array' }, 'jobs')).toThrow(MalformedPageError);
  });

  it('names the key it looked under, so the refusal is actionable', () => {
    try {
      adoptPage({ error: 'boom' }, 'tasks');
      expect.unreachable('adoptPage accepted an answer with no records');
    } catch (err: unknown) {
      expect(err).toBeInstanceOf(MalformedPageError);
      expect((err as MalformedPageError).itemsKey).toBe('tasks');
      expect((err as Error).message).toContain('tasks');
    }
  });
});

describe('wholeSetPage', () => {
  it('reports a complete collection as one page with no next', () => {
    const page = wholeSetPage([JOB, JOB, JOB]);
    expect(page).toEqual({ items: [JOB, JOB, JOB], total: 3, limit: 3, offset: 0 });
  });

  it('reports an empty collection as an empty page, not as a missing one', () => {
    expect(wholeSetPage([])).toEqual({ items: [], total: 0, limit: 0, offset: 0 });
  });
});

// =============================================================================
// REST — the transport that actually pages
// =============================================================================

describe('RestOrchestratorClient paged listings', () => {
  const BASE = 'http://test-server';
  let client: RestClient;
  let fetchMock: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    client = new RestClient(BASE);
    fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof fetch;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  function calledParams(): URLSearchParams {
    return new URL(fetchMock.mock.calls[0][0] as string).searchParams;
  }

  it('sends the four shared parameters and returns the envelope', async () => {
    fetchMock.mockResolvedValueOnce(
      mockOK({ jobs: [JOB], total: 1987, limit: 25, offset: 25, next_offset: 50 }),
    );

    const page = await client.orchestrator.listJobsPage('acme', {
      limit: 25,
      offset: 25,
      sort: 'started_at:desc',
      q: 'wire',
    });

    const params = calledParams();
    expect(params.get('tenant_id')).toBe('acme');
    expect(params.get('limit')).toBe('25');
    expect(params.get('offset')).toBe('25');
    expect(params.get('sort')).toBe('started_at:desc');
    expect(params.get('q')).toBe('wire');
    expect(page.total).toBe(1987);
    expect(page.next_offset).toBe(50);
    expect(page.items).toHaveLength(1);
  });

  it('omits the parameters the caller did not ask for', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ jobs: [], total: 0, limit: 25, offset: 0 }));

    await client.orchestrator.listJobsPage('acme');

    const params = calledParams();
    expect(params.has('limit')).toBe(false);
    expect(params.has('offset')).toBe(false);
    expect(params.has('sort')).toBe(false);
    expect(params.has('q')).toBe(false);
  });

  it('sends the jobs listing own narrowings, repeated ones comma-separated', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ jobs: [], total: 0, limit: 25, offset: 0 }));

    await client.orchestrator.listJobsPage('acme', {
      task_id: 'task-7',
      runner: 'sdlc',
      project_id: 'proj-a',
      statuses: ['executing', 'failed'],
      job_ids: ['job-1', 'job-2'],
    });

    const params = calledParams();
    expect(params.get('task_id')).toBe('task-7');
    expect(params.get('runner')).toBe('sdlc');
    expect(params.get('project_id')).toBe('proj-a');
    expect(params.get('statuses')).toBe('executing,failed');
    expect(params.get('job_ids')).toBe('job-1,job-2');
  });

  it('omits an empty narrowing rather than sending an empty value', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ jobs: [], total: 0, limit: 25, offset: 0 }));

    await client.orchestrator.listJobsPage('acme', { statuses: [], runner: '' });

    const params = calledParams();
    expect(params.has('statuses')).toBe(false);
    expect(params.has('runner')).toBe(false);
  });

  it('sends the tasks dispatch-mode filter as a boolean the server can read', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ tasks: [], total: 0, limit: 25, offset: 0 }));
    await client.orchestrator.listTasksPage('acme', { auto_spawn: false, project_id: 'proj-a' });

    const params = calledParams();
    expect(params.get('auto_spawn')).toBe('false');
    expect(params.get('project_id')).toBe('proj-a');
  });

  it('renames the session wire fields on a paged answer, role included', async () => {
    fetchMock.mockResolvedValueOnce(
      mockOK({
        sessions: [
          {
            session_id: 's-1',
            backend: 'claude',
            model: 'opus',
            state: 'active',
            created_at: 1,
            last_activity: 2,
            job_id: 'job-1',
            runner: 'review',
          },
        ],
        total: 54,
        limit: 25,
        offset: 0,
        next_offset: 25,
      }),
    );

    const page = await client.orchestrator.listSessionsPage('acme', { backend: 'claude' });

    expect(calledParams().get('backend')).toBe('claude');
    expect(page.total).toBe(54);
    expect(page.items[0]).toEqual({
      session_id: 's-1',
      backend: 'claude',
      model: 'opus',
      status: 'active',
      created_at: 1,
      last_active: 2,
      job_id: 'job-1',
      runner: 'review',
    });
  });

  it('requires a tenant for the sessions page rather than listing every tenant', async () => {
    await expect(client.orchestrator.listSessionsPage('')).rejects.toThrow(/tenantID is required/);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it('pages agents, workflows, projects and accounts through the same envelope', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ agents: [{ id: 'a-1' }], total: 3, limit: 1, offset: 0, next_offset: 1 }));
    const agents = await client.orchestrator.listAgentsPage('acme', { limit: 1, backend: 'codex' });
    expect(agents.total).toBe(3);
    expect(new URL(fetchMock.mock.calls[0][0] as string).searchParams.get('backend')).toBe('codex');

    fetchMock.mockResolvedValueOnce(mockOK({ workflows: [{ name: 'w' }], total: 1, limit: 25, offset: 0 }));
    expect((await client.orchestrator.listWorkflowsPage('acme')).total).toBe(1);

    fetchMock.mockResolvedValueOnce(mockOK({ projects: [{ id: 'p' }], total: 9, limit: 25, offset: 0 }));
    const projects = await client.projects.listProjectsPage('acme', { sort: 'name' });
    expect(projects.total).toBe(9);
    expect(new URL(fetchMock.mock.calls[2][0] as string).searchParams.get('sort')).toBe('name');

    fetchMock.mockResolvedValueOnce(mockOK({ accounts: [{ id: 'acct' }], total: 4, limit: 25, offset: 0 }));
    const accounts = await client.accounts.listAccountsPage({ tenant_id: 'acme', provider: 'claude' });
    expect(accounts.total).toBe(4);
    expect(new URL(fetchMock.mock.calls[3][0] as string).searchParams.get('provider')).toBe('claude');
  });

  it('surfaces a refused request instead of an empty page', async () => {
    fetchMock.mockResolvedValueOnce(mockError(400, 'page: unknown sort field: subject'));

    await expect(
      client.orchestrator.listJobsPage('acme', { sort: 'subject' }),
    ).rejects.toThrow(/unknown sort field/);
  });

  it('refuses a 200 whose body is not a listing', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ message: 'ok' }));

    await expect(client.orchestrator.listJobsPage('acme')).rejects.toBeInstanceOf(
      MalformedPageError,
    );
  });

  it('reads a daemon that does not page yet as one whole page', async () => {
    fetchMock.mockResolvedValueOnce(mockOK({ jobs: [JOB, JOB] }));

    const page = await client.orchestrator.listJobsPage('acme');
    expect(page.total).toBe(2);
    expect(page.next_offset).toBeUndefined();
  });
});

// =============================================================================
// Wails — parameters go over, the envelope does not come back
// =============================================================================

describe('WailsOrchestratorClient paged listings', () => {
  let bindings: Record<string, ReturnType<typeof vi.fn>>;
  let client: WailsClient;

  beforeEach(() => {
    bindings = {
      ListJobs: vi.fn().mockResolvedValue([JOB]),
      ListTasks: vi.fn().mockResolvedValue([{ id: 't-1' }]),
      ListSessions: vi.fn().mockResolvedValue([
        { session_id: 's-1', backend: 'claude', status: 'active', runner: 'sdlc' },
      ]),
      ListAgents: vi.fn().mockResolvedValue({ agents: [{ id: 'a-1' }], count: 1 }),
      ListWorkflows: vi.fn().mockResolvedValue({ workflows: [{ name: 'w' }], count: 1 }),
    };
    Object.defineProperty(globalThis, 'window', {
      value: { go: { main: { OrchestratorBinding: bindings } } },
      writable: true,
      configurable: true,
    });
    client = new WailsClient();
  });

  afterEach(() => {
    Object.defineProperty(globalThis, 'window', {
      value: undefined,
      writable: true,
      configurable: true,
    });
    vi.restoreAllMocks();
  });

  it('puts the paging parameters on the request the binding forwards', async () => {
    await client.orchestrator.listJobsPage('acme', {
      limit: 50,
      offset: 50,
      sort: 'state',
      q: 'sdlc',
      runner: 'sdlc',
      statuses: ['failed'],
    });

    expect(bindings.ListJobs).toHaveBeenCalledWith({
      tenant_id: 'acme',
      limit: 50,
      offset: 50,
      sort: 'state',
      q: 'sdlc',
      runner: 'sdlc',
      statuses: ['failed'],
    });
  });

  it('reports the projected array as one whole page, since the binding drops the envelope', async () => {
    const page = await client.orchestrator.listJobsPage('acme');
    expect(page.total).toBe(1);
    expect(page.next_offset).toBeUndefined();
  });

  it('reads an envelope from the binding when it sends one', async () => {
    bindings.ListTasks.mockResolvedValueOnce({
      tasks: [{ id: 't-1' }],
      total: 300,
      limit: 25,
      offset: 0,
      next_offset: 25,
    });

    const page = await client.orchestrator.listTasksPage('acme', { limit: 25 });
    expect(page.total).toBe(300);
    expect(page.next_offset).toBe(25);
  });

  it('pages sessions, agents and workflows through the same envelope', async () => {
    const sessions = await client.orchestrator.listSessionsPage('acme', { status: 'active' });
    expect(sessions.items[0]?.runner).toBe('sdlc');
    expect(bindings.ListSessions).toHaveBeenCalledWith({ tenantID: 'acme', status: 'active' });

    expect((await client.orchestrator.listAgentsPage('acme')).total).toBe(1);
    expect((await client.orchestrator.listWorkflowsPage('acme')).total).toBe(1);
  });

  it('requires a tenant for the sessions page', async () => {
    await expect(client.orchestrator.listSessionsPage('')).rejects.toThrow(/tenantID is required/);
    expect(bindings.ListSessions).not.toHaveBeenCalled();
  });

  it('refuses an answer from the binding that is not a listing', async () => {
    bindings.ListJobs.mockResolvedValueOnce({ message: 'ok' });
    await expect(client.orchestrator.listJobsPage('acme')).rejects.toBeInstanceOf(
      MalformedPageError,
    );
  });
});

// =============================================================================
// The transports that cannot page
//
// Each answers through its own unpaged sibling, so the caller gets that
// transport's own typed refusal — not a page it never fetched, and not a
// "paging not supported" that would hide which half is missing.
// =============================================================================

describe('transports that carry no paging', () => {
  const cases: Array<{
    name: string;
    make: () => { orchestrator: { listJobsPage: (t: string) => Promise<unknown> } };
    error: new (...args: never[]) => Error;
  }> = [
    { name: 'grpc-web', make: () => new GrpcWebClient('http://x'), error: GrpcWebNotImplementedError },
    { name: 'quic', make: () => new QuicClient('http://x'), error: QuicNotImplementedError },
    { name: 'unix', make: () => new UnixClient('/tmp/x.sock'), error: UnixNotImplementedError },
    { name: 'mcp', make: () => new McpClient('http://x'), error: McpNotImplementedError },
  ];

  for (const tc of cases) {
    it(`${tc.name} refuses through its unpaged sibling rather than inventing a page`, async () => {
      await expect(tc.make().orchestrator.listJobsPage('acme')).rejects.toBeInstanceOf(tc.error);
    });
  }
});
