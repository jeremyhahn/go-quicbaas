// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// The MCP list tools cut a page (25 records by default) and name the next one
// with `next_offset`. An unpaged verb must walk those pages to the end, as the
// REST transport does, or a tenant past one page silently loses records. A
// paged call keeps its single page.

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { McpClient } from '../src/mcp';
import { jsonReply, scriptServer, toolCalls, toolText, type RpcMessage } from './mcp_server';

const endpoint = 'http://node.example:8081/mcp';

const originalFetch = globalThis.fetch;
let fetchMock: ReturnType<typeof vi.fn>;

beforeEach(() => {
  fetchMock = vi.fn();
  globalThis.fetch = fetchMock as unknown as typeof fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

type Orchestrator = McpClient['orchestrator'];

// twoPages answers a list tool with `first` at offset 0 and `second` at the
// offset the first page names, each in the server's page envelope.
function twoPages(key: string, first: unknown, second: unknown): (msg: RpcMessage) => Response {
  return (msg) => {
    const offset = (msg.params?.arguments?.offset as number | undefined) ?? 0;
    const body =
      offset === 0
        ? { [key]: [first], total: 2, limit: 1, offset: 0, next_offset: 1 }
        : { [key]: [second], total: 2, limit: 1, offset: 1 };
    return jsonReply(toolText(msg.id as number, body));
  };
}

const session = (id: string) => ({ session_id: id, backend: 'claude', state: 'active', created_at: 1, last_activity: 2 });
const sdkSession = (id: string) => ({ session_id: id, backend: 'claude', status: 'active', created_at: 1, last_active: 2 });

describe('McpClient — unpaged listings walk every page', () => {
  it.each([
    {
      name: 'listJobs',
      call: (o: Orchestrator) => o.listJobs('t1', 'task-1'),
      tool: 'quicbaas_orchestrator_list_jobs',
      key: 'jobs',
      first: { job_id: 'job-1' },
      second: { job_id: 'job-2' },
      want: [{ job_id: 'job-1' }, { job_id: 'job-2' }],
      filter: { task_id: 'task-1' },
    },
    {
      name: 'listSessions',
      call: (o: Orchestrator) => o.listSessions({ tenantID: 't1', status: 'active', sort: 'created_at' }),
      tool: 'quicbaas_orchestrator_list_sessions',
      key: 'sessions',
      first: session('s1'),
      second: session('s2'),
      want: { sessions: [sdkSession('s1'), sdkSession('s2')], count: 2 },
      filter: { status: 'active', sort: 'created_at' },
    },
    {
      name: 'listFleetNodes',
      call: (o: Orchestrator) => o.listFleetNodes({ tenant_id: 't1', backend: 'ollama', q: 'gpu' }),
      tool: 'quicbaas_fleet_list',
      key: 'nodes',
      first: { id: 'node-1' },
      second: { id: 'node-2' },
      want: { nodes: [{ id: 'node-1' }, { id: 'node-2' }], count: 2 },
      filter: { backend: 'ollama', q: 'gpu' },
    },
    {
      name: 'listWorkflows',
      call: (o: Orchestrator) => o.listWorkflows('t1'),
      tool: 'quicbaas_orchestrator_list_workflows',
      key: 'workflows',
      first: { name: 'wf-1' },
      second: { name: 'wf-2' },
      want: { workflows: [{ name: 'wf-1' }, { name: 'wf-2' }], count: 2 },
      filter: {},
    },
  ])('$name returns the records of every page', async ({ call, tool, key, first, second, want, filter }) => {
    scriptServer(fetchMock, twoPages(key, first, second));
    const client = new McpClient(endpoint);

    await expect(call(client.orchestrator)).resolves.toEqual(want);

    const calls = toolCalls(fetchMock);
    expect(calls.map((c) => c.params?.name)).toEqual([tool, tool]);
    expect(calls[1]?.params?.arguments).toEqual({ tenant_id: 't1', ...filter, offset: 1 });
  });

  it('a paged listSessions keeps its single page', async () => {
    scriptServer(fetchMock, twoPages('sessions', session('s1'), session('s2')));
    const client = new McpClient(endpoint);

    const resp = await client.orchestrator.listSessions({ tenantID: 't1', limit: 1 });

    expect(resp).toEqual({ sessions: [sdkSession('s1')], count: 1 });
    expect(toolCalls(fetchMock)).toHaveLength(1);
  });

  it('a paged listFleetNodes keeps its single page', async () => {
    scriptServer(fetchMock, twoPages('nodes', { id: 'node-1' }, { id: 'node-2' }));
    const client = new McpClient(endpoint);

    const resp = await client.orchestrator.listFleetNodes({ tenant_id: 't1', offset: 0 });

    expect(resp).toEqual({ nodes: [{ id: 'node-1' }], count: 1 });
    expect(toolCalls(fetchMock)).toHaveLength(1);
  });

  it('stops when a page does not advance the offset', async () => {
    scriptServer(fetchMock, (msg) =>
      jsonReply(toolText(msg.id as number, { jobs: [{ job_id: 'job-1' }], total: 9, limit: 1, offset: 0, next_offset: 0 })),
    );
    const client = new McpClient(endpoint);

    await expect(client.orchestrator.listJobs('t1')).resolves.toEqual([{ job_id: 'job-1' }]);
    expect(toolCalls(fetchMock)).toHaveLength(1);
  });
});

describe('McpClient — listing filters and fields', () => {
  it('listWorkflowsPage forwards project_id and mode', async () => {
    scriptServer(fetchMock, (msg) =>
      jsonReply(toolText(msg.id as number, { workflows: [], total: 0, limit: 25, offset: 0 })),
    );
    const client = new McpClient(endpoint);

    await client.orchestrator.listWorkflowsPage('t1', { project_id: 'proj-a', mode: 'dag', limit: 5 });

    expect(toolCalls(fetchMock)[0]?.params?.arguments).toEqual({
      tenant_id: 't1',
      project_id: 'proj-a',
      mode: 'dag',
      limit: 5,
    });
  });

  it.each([
    { name: 'listSessions', call: async (o: Orchestrator) => (await o.listSessions({ tenantID: 't1' })).sessions },
    { name: 'listSessionsPage', call: async (o: Orchestrator) => (await o.listSessionsPage('t1')).items },
  ])('$name keeps the session agent_id', async ({ call }) => {
    scriptServer(fetchMock, (msg) =>
      jsonReply(toolText(msg.id as number, { sessions: [{ ...session('s1'), agent_id: 'agent-7' }], total: 1 })),
    );
    const client = new McpClient(endpoint);

    const sessions = await call(client.orchestrator);

    expect(sessions).toEqual([{ ...sdkSession('s1'), agent_id: 'agent-7' }]);
  });
});
